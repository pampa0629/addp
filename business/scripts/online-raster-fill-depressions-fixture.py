#!/usr/bin/env python3
"""Owned NFS fixture and independent whole-pixel oracle for raster DEM filling."""
from __future__ import annotations
from functools import lru_cache
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-fill-depressions-persistence'
RUNTIME = 'addp-raster-fill-depressions-runtime'
CASES = {'filled.cog.tif': ('dem.tif',2,'metre'), 'feet-filled.cog.tif': ('feet.tif',2,'foot'), 'empty-filled.cog.tif': ('dem.tif',3,'metre')}
TARGETS = tuple(CASES)
SOURCES = ('dem.tif','feet.tif','rotated.tif','scaled.tif')
TRANSFORM = (500000., 2., 0., 3000000., 0., -3.)
WIDTH, HEIGHT = 519, 515



class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker


def guard(env=os.environ):
    return NFSExport('fill-depressions', command).guard(env)


def owned_root(secret):
    return NFSExport('fill-depressions', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}


def point(x, y):
    a,b,c,d,e,f = TRANSFORM
    return a+b*x+c*y, d+e*x+f*y


def source_arrays():
    import numpy as np
    values=np.full((HEIGHT,WIDTH),12.)
    values[[0,-1],:]=30;values[:,[0,-1]]=30
    values[0,0]=4;values[1:40,1:40]=0
    values[0,100]=0
    values[60:170,60:170]=18;values[61:169,61:169]=1
    values[90:140,90:140]=24;values[91:139,91:139]=-5
    values[200:300,200:300]=20;values[201:299,201:299]=0
    values[200,230]=8;values[299,270]=16
    values[350:430,350:430]=16;values[351:429,351:429]=7
    values[360:365,410:415]=-9999
    values[480:HEIGHT-1,480:WIDTH-1]=22;values[481:HEIGHT-1,481:WIDTH-1]=-3
    values[3,250]=np.inf;values[320,190]=np.nan
    alpha=np.full(values.shape,255.);alpha[0,0]=128;alpha[250,350]=0;alpha[321,191]=np.nan
    mask=np.full(values.shape,255,np.uint8);mask[310,320]=0
    return values,alpha,mask


def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    values,alpha,mask=source_arrays()
    crs=osr.SpatialReference();crs.ImportFromEPSG(3857)
    for name in SOURCES:
        dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,4,gdal.GDT_Float64,['TILED=YES','COMPRESS=DEFLATE'])
        dataset.SetProjection(crs.ExportToWkt())
        dataset.SetGeoTransform((500000.,2.,.25,3000000.,0.,-3.) if name=='rotated.tif' else TRANSFORM)
        for index,array in enumerate((np.full(values.shape,.25),values,np.full(values.shape,-9999.),alpha),1):
            band=dataset.GetRasterBand(index);band.WriteArray(array);band.SetNoDataValue(-9999);band=None
        dataset.GetRasterBand(2).SetUnitType('foot' if name=='feet.tif' else 'metre')
        if name=='scaled.tif':dataset.GetRasterBand(2).SetScale(2.)
        dataset.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        if dataset.GetFileList()!=[str(root/name)]:raise FixtureError('source must be self-contained')
        dataset=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}


def original_pixels(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact DEM filling target')
    values,alpha,mask=source_arrays()
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    if name=='empty-filled.cog.tif':valid[:]=False
    return np.where(valid,values*(.3048 if CASES[name][2]=='foot' else 1.),np.nan)


def outlet_cells(values):
    import numpy as np
    valid=np.isfinite(values)
    exits=[]
    for row,col in np.argwhere(valid):
        if row in (0,HEIGHT-1) or col in (0,WIDTH-1) or any(
            not valid[y,x] for y in range(max(0,row-1),min(HEIGHT,row+2)) for x in range(max(0,col-1),min(WIDTH,col+2))):
            exits.append((int(row),int(col)))
    return exits


@lru_cache(maxsize=1)
def raw_filled_pixels():
    # Independent threshold-connectivity oracle: no product imports, heap,
    # min-max priority traversal or production scratch/block implementation.
    from collections import deque
    import numpy as np
    values=original_pixels('filled.cog.tif');valid=np.isfinite(values)
    exits=outlet_cells(values);expected=np.full(values.shape,np.nan)
    pending=int(valid.sum())
    for level in np.unique(values[valid]):
        allowed=valid&(values<=level)
        reached=np.zeros(values.shape,dtype=bool);queue=deque()
        for row,col in exits:
            if allowed[row,col]:reached[row,col]=True;queue.append((row,col))
        while queue:
            row,col=queue.popleft()
            for y in range(max(0,row-1),min(HEIGHT,row+2)):
                for x in range(max(0,col-1),min(WIDTH,col+2)):
                    if allowed[y,x] and not reached[y,x]:
                        reached[y,x]=True;queue.append((y,x))
        discovered=reached&np.isnan(expected)
        expected[discovered]=level;pending-=int(discovered.sum())
        if not pending:break
    if pending:raise FixtureError('independent oracle left valid cells without an open outlet')
    expected.flags.writeable=False
    return expected


@lru_cache(maxsize=3)
def expected_pixels(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact DEM filling target')
    expected=np.full((HEIGHT,WIDTH),np.nan) if name=='empty-filled.cog.tif' else raw_filled_pixels()*(.3048 if CASES[name][2]=='foot' else 1.)
    expected.flags.writeable=False
    return expected


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
        raise FixtureError('invalid exact raster DEM filling target inventory')
    if sorted(p.name for p in root.iterdir())!=sorted([*SOURCES,'outputs']):
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
        if band.GetUnitType()!='metre':raise FixtureError('output elevation unit must be metre')
        if band.GetScale() is not None or band.GetOffset() is not None:raise FixtureError('source scale/offset leaked into output')
        original=original_pixels(name);valid=np.isfinite(original);exits=outlet_cells(original)
        if np.any(actual[valid]<original[valid]):raise FixtureError('filling lowered a valid source elevation')
        if any(actual[row,col]!=original[row,col] for row,col in exits):raise FixtureError('open outlet elevation changed')
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,
            'valid_pixels':int(valid.sum()),'full_pixel_and_mask_verification':True,
            'depression_fill_verified':True,'unit_metres_verified':True,'open_outlets_verified':True,'never_lowered_verified':True,
            'raised_pixels':int(np.count_nonzero(actual[valid]>original[valid])),
            'maximum_raise_metres':float(np.max(actual[valid]-original[valid])) if valid.any() else 0.,
            'open_outlet_pixels':len(exits)}
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
        return NFSExport('fill-depressions',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'fill-depressions-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'fill-depressions-source.json').read_text())['sources']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster DEM filling fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
