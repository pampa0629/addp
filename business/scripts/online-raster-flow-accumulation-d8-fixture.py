#!/usr/bin/env python3
"""Owned NFS fixture and independent whole-pixel oracle for D8 flow accumulation."""
from __future__ import annotations
from functools import lru_cache
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-flow-accumulation-d8-persistence'
RUNTIME = 'addp-raster-flow-accumulation-d8-runtime'
CASES = {'accumulation.cog.tif':('directions.tif',2,'direct'),
         'dem-accumulation.cog.tif':('direct.tif',2,'direction'),
         'filled-accumulation.cog.tif':('bowl.tif',2,'fill'),
         'empty-accumulation.cog.tif':('directions.tif',3,'direct')}
TARGETS = tuple(CASES)
SOURCES = ('directions.tif','direct.tif','bowl.tif','code.tif','terminal.tif','outside.tif','invalid.tif','cycle.tif','rotated.tif')
TRANSFORM = (500000., 2., 0., 3000000., 0., -3.)
WIDTH, HEIGHT = 519, 515



class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    try:
        return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:
        # Keep the worker's actual verification failure; stdout and stdin may
        # contain fixture state and are deliberately excluded from diagnostics.
        detail = (error.stderr or '').strip()[-4000:] or 'no stderr diagnostic'
        raise FixtureError(f'fixture command failed (exit {error.returncode}): {detail}') from error


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker


def guard(env=os.environ):
    return NFSExport('flow-accumulation-d8', command).guard(env)


def owned_root(secret):
    return NFSExport('flow-accumulation-d8', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}


def point(x, y):
    a,b,c,d,e,f = TRANSFORM
    return a+b*x+c*y, d+e*x+f*y


MAIN_WIDTH=514
NEIGHBOURS=tuple((y,x,c) for y,x,c in ((-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2)))


def source_arrays(name='direct.tif'):
    import numpy as np
    values=np.full((HEIGHT,WIDTH),-9999.)
    values[:,:MAIN_WIDTH]=10.
    values[[0,-1],:MAIN_WIDTH]=50.;values[:,[0,MAIN_WIDTH-1]]=50.
    values[0,1]=10.  # Only low open terminal for the large flat component.
    if name=='bowl.tif':values[1:-1,1:MAIN_WIDTH-1]=2.
    if name=='closed.tif':values[250,250]=0.
    values[510:513,516:519]=[[4,3,2],[1,0,-1],[-2,-3,-4]]
    values[511,516]=np.inf;values[512,516]=np.nan
    alpha=np.full(values.shape,255.);alpha[511,517]=128
    alpha[510,517]=0;alpha[510,518]=np.nan
    mask=np.full(values.shape,255,np.uint8);mask[510,516]=0
    return values,alpha,mask


def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    for name in SOURCES:
        values,alpha,mask=source_arrays(name)
        if name not in ('direct.tif','bowl.tif'):
            values=np.where(np.isfinite(expected_directions('accumulation.cog.tif')),expected_directions('accumulation.cog.tif'),-9999.)
            # Illegal codes hidden by alpha/mask must remain excluded.
            values[510,516:519]=3
            if name=='terminal.tif':values[250,250]=0
            elif name=='outside.tif':values[0,0]=64
            elif name=='invalid.tif':values[250,MAIN_WIDTH-1]=1
            elif name=='cycle.tif':values[250,250]=1;values[250,251]=16
        if name=='code.tif':
            dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,2,gdal.GDT_UInt64,['TILED=YES','COMPRESS=DEFLATE'])
            dataset.SetProjection(crs.ExportToWkt());dataset.SetGeoTransform(TRANSFORM)
            for index in (1,2):
                array=np.full((HEIGHT,WIDTH),2**53,dtype=np.uint64)
                if index==2:array[250,250]=2**53+1
                band=dataset.GetRasterBand(index);band.WriteArray(array);band.SetNoDataValueAsUInt64(2**53);band=None
            dataset=None;continue
        dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,4,gdal.GDT_Float64,['TILED=YES','COMPRESS=DEFLATE'])
        dataset.SetProjection(crs.ExportToWkt())
        dataset.SetGeoTransform((500000.,2.,.25,3000000.,0.,-3.) if name=='rotated.tif' else TRANSFORM)
        for index,array in enumerate((np.full(values.shape,.25),values,np.full(values.shape,-9999.),alpha),1):
            band=dataset.GetRasterBand(index);band.WriteArray(array);band.SetNoDataValue(-9999);band=None
        dataset.GetRasterBand(2).SetUnitType('metre' if name in ('direct.tif','bowl.tif') else 'classification')
        dataset.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        if dataset.GetFileList()!=[str(root/name)]:raise FixtureError('source must be self-contained')
        dataset=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}


def original_pixels(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact D8 target')
    values,alpha,mask=source_arrays(CASES[name][0])
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    if CASES[name][1]==3:valid[:]=False
    return np.where(valid,values,np.nan)


def local_direction(values,row,col):
    # Independent local maximum slope, with row/column tie ordering.
    import math
    neighbours=[]
    for dy,dx,code in NEIGHBOURS:
        y,x=row+dy,col+dx
        if 0<=y<HEIGHT and 0<=x<WIDTH and values[y,x]<values[row,col]:
            neighbours.append((-(values[row,col]-values[y,x])/math.hypot(dx*2,dy*3),y,x,code))
    return min(neighbours)[-1] if neighbours else 0


@lru_cache(maxsize=4)
def expected_directions(name):
    # Closed-form octile paths to (0,1), independent of product heap/graph.
    # All interior cells are flat after the explicitly requested filling step.
    import numpy as np
    values=original_pixels(name);expected=np.full(values.shape,np.nan)
    if name=='empty-accumulation.cog.tif':return expected
    if name=='filled-accumulation.cog.tif':values[1:-1,1:MAIN_WIDTH-1]=10.
    expected[:,:MAIN_WIDTH]=32.
    expected[1:-1,1]=64.;expected[1,3:MAIN_WIDTH-1]=16.
    for row,col in np.argwhere(np.isfinite(values)):
        if col>=MAIN_WIDTH or row in (0,HEIGHT-1) or col in (0,MAIN_WIDTH-1):
            expected[row,col]=local_direction(values,row,col)
    expected.flags.writeable=False
    return expected


@lru_cache(maxsize=4)
def expected_pixels(name):
    import numpy as np
    directions=expected_directions(name)
    valid=np.isfinite(directions);counts=np.zeros(directions.size,np.int64)
    destination=np.full(directions.shape,-1,np.int64)
    for dy,dx,code in NEIGHBOURS:
        rows,cols=np.nonzero(directions==code)
        destination[rows,cols]=(rows+dy)*WIDTH+cols+dx
    # Each original valid source walks its own route; repeated destinations
    # retain multiplicity. No indegree, topological ordering or product import.
    active=np.flatnonzero(valid)
    for _ in range(directions.size):
        active=destination.ravel()[active];active=active[active>=0]
        if not len(active):break
        counts+=np.bincount(active,minlength=directions.size)
    else:raise FixtureError('fixture direction cycle')
    result=np.where(valid,counts.reshape(directions.shape),np.nan)
    result.flags.writeable=False
    return result


def conservation_evidence(actual,name):
    import numpy as np
    directions=expected_directions(name);valid=np.isfinite(directions)
    if not np.array_equal(np.isfinite(actual),valid):raise FixtureError('count coverage differs')
    if np.any(actual[valid]<0) or np.any(actual[valid]!=np.floor(actual[valid])):raise FixtureError('invalid upstream count')
    terminals=directions==0
    total=sum(int(value)+1 for value in actual[terminals])
    if total!=int(valid.sum()):raise FixtureError('terminal totals do not conserve valid source pixels')
    return {'terminal_conservation_verified':True,'counts_exclude_self':True,
            'terminal_pixels':int(terminals.sum()),'terminal_total_with_self':total}


def artifact_expectation():
    corners=[point(x,y) for x,y in ((0,0),(WIDTH,0),(0,HEIGHT),(WIDTH,HEIGHT))]
    return {'width':WIDTH,'height':HEIGHT,'band_count':1,'source_crs':'EPSG:3857','extent_srid':3857,
        'transform':list(TRANSFORM),'band_nodata':None,
        'extent':[min(p[0] for p in corners),min(p[1] for p in corners),max(p[0] for p in corners),max(p[1] for p in corners)]}


def verify(root, names):
    import numpy as np
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(name not in TARGETS for name in names):
        raise FixtureError('invalid exact D8 flow accumulation target inventory')
    # NFSExport.owned_root validates this lifecycle marker against the run ID
    # before invoking the worker. It is required inventory, never a sidecar.
    if sorted(p.name for p in root.iterdir())!=sorted([*SOURCES,'outputs','.addp-online-owner']):
        raise FixtureError('unexpected source sidecar or temporary file')
    if sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):
        raise FixtureError('unexpected output, sidecar or temporary file')
    result={'sources':{name:digest(root/name) for name in SOURCES},'targets':{}}
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    for name in names:
        path=root/'outputs'/name;fact=digest(path);dataset=gdal.Open(str(path))
        if dataset is None or dataset.GetDriver().ShortName!='GTiff' or dataset.RasterCount!=1:
            raise FixtureError('expected one-band GeoTIFF')
        if (dataset.RasterXSize,dataset.RasterYSize,dataset.GetGeoTransform())!=(WIDTH,HEIGHT,TRANSFORM):
            raise FixtureError('source grid changed')
        if not dataset.GetProjection():raise FixtureError('source CRS changed')
        actual_crs=osr.SpatialReference();actual_crs.ImportFromWkt(dataset.GetProjection())
        if not actual_crs.IsSame(crs):raise FixtureError('source CRS changed')
        band=dataset.GetRasterBand(1)
        if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue()):
            raise FixtureError('Float64 or NaN NoData changed')
        expected=expected_pixels(name);actual=band.ReadAsArray()
        if not np.array_equal(actual,expected,equal_nan=True):raise FixtureError('complete pixel values differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray(),np.where(np.isfinite(expected),255,0)):
            raise FixtureError('complete pixel mask differs')
        _,errors,_=validate(dataset,full_check=True)
        if errors:raise FixtureError('COG full layout validation failed')
        if band.GetOverviewCount()<1:raise FixtureError('expected COG overviews for cross-block image')
        if band.GetColorTable() is not None:raise FixtureError('unexpected DEM color table')
        if band.GetUnitType()!='':raise FixtureError('D8 counts must not inherit source units')
        if band.GetScale() is not None or band.GetOffset() is not None:raise FixtureError('source scale/offset leaked into output')
        valid=np.isfinite(expected)
        routing=conservation_evidence(actual,name)
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,
            'valid_pixels':int(valid.sum()),'full_pixel_and_mask_verification':True,
            'count_unit_cleared':True,**routing}
        band=dataset=None
    return result


def worker(action, root, names):
    if action not in {'seed','verify'}:raise FixtureError('unknown worker action')
    if action=='verify' and list(Path(tempfile.gettempdir()).glob('addp-raster-*')):
        raise FixtureError('Runtime raster execution workspace has residuals')
    return seed(root) if action=='seed' else {**verify(root,names),'runtime_workspaces_removed':True}


def runtime_worker(action, root, names=()):
    return run_runtime_worker(command,RUNTIME,SUITE,Path(__file__),action,root,names)


def main(action):
    secret=guard()
    if action in {'start','stop'}:
        return NFSExport('flow-accumulation-d8',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'flow-accumulation-d8-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'flow-accumulation-d8-source.json').read_text())['sources']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster D8 flow accumulation fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
