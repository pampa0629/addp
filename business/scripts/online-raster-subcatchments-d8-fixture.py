#!/usr/bin/env python3
"""Owned NFS fixture and independent whole-pixel oracle for D8 incremental subcatchments."""
from __future__ import annotations
from functools import lru_cache
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-subcatchments-d8-persistence'
RUNTIME = 'addp-raster-subcatchments-d8-runtime'
CASES = {'subcatchments.cog.tif':('directions.tif','points.tif',2,2,'direct'),
         'full-reference.cog.tif':('directions.tif','points.tif',2,2,'reference'),
         'nested.cog.tif':('directions.tif','nested-points.tif',2,2,'nested'),
         'reclass.cog.tif':('directions.tif','nested-points.tif',2,2,'reclass'),
         'dem-subcatchments.cog.tif':('dem.tif','dem-points.tif',2,2,'direction'),
         'filled-subcatchments.cog.tif':('bowl.tif','dem-points.tif',2,2,'fill'),
         'snapped.cog.tif':('directions.tif','snap-points.tif',2,2,'snap')}
TARGETS=tuple(CASES)
SOURCES=('directions.tif','points.tif','dem.tif','bowl.tif','dem-points.tif','snap-points.tif',
         'nested-points.tif','duplicate-points.tif','empty-points.tif','missing-points.tif','shifted-points.tif',
         'invalid-points.tif','id-directions.tif','code.tif','code-points.tif','cycle.tif',
         'terminal.tif','outside.tif','invalid-target.tif','rotated-directions.tif','rotated-points.tif')
SNAP_RADIUS=3.1
TRANSFORM=(500000.,2.,0.,3000000.,0.,-3.)
WIDTH,HEIGHT=519,515
FIRST_ROW=508
NEIGHBOURS=((-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2))


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
    return NFSExport('subcatchments-d8', command).guard(env)


def owned_root(secret):
    return NFSExport('subcatchments-d8', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}


def point(x, y):
    a,b,c,d,e,f = TRANSFORM
    return a+b*x+c*y, d+e*x+f*y


def crs():
    from osgeo import osr
    result=osr.SpatialReference();result.ImportFromEPSG(3857);return result


def direction_arrays():
    import numpy as np
    values=np.full((HEIGHT,WIDTH),np.nan);values[FIRST_ROW:,:]=4.;values[-1,:]=0
    alpha=np.full(values.shape,255.);mask=np.full(values.shape,255,np.uint8)
    # Every removed centre's northern predecessor becomes a legal open zero.
    for row,col in ((510,100),(511,200),(512,300),(513,400),(514,510)):
        values[row-1,col]=0
    values[510,100]=np.nan;values[511,200]=np.inf
    values[512,300]=3;alpha[512,300]=0
    values[513,400]=3;alpha[513,400]=np.nan
    values[514,510]=3;mask[514,510]=0
    # Nested confluences straddle row/column 512. Northbound tributaries join
    # an eastbound trunk ending at the right edge, without long background paths.
    for (row,col),code in { (1,1):2,(1,3):8,(2,2):2,(5,1):128,(5,3):32,(4,2):128,
            (2,4):8,(3,3):1,(3,4):1,(3,5):1,(3,6):1,(3,7):1,(3,8):0,(2,6):4}.items():
        values[FIRST_ROW+row,510+col]=code
    alpha[511,515]=128
    return values,alpha,mask


def effective(values,alpha,mask):
    import numpy as np
    return np.where(np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0),values,np.nan)


def dem_values(filled=False):
    import numpy as np
    result=np.full((HEIGHT,WIDTH),np.nan)
    result[FIRST_ROW:,:]=np.arange(HEIGHT-FIRST_ROW,0,-1,dtype=float)[:,None]
    if filled:result[FIRST_ROW+1:-1,1:-1]=-2
    return result


def point_values(name='points.tif'):
    import numpy as np
    values,alpha,mask=direction_arrays();valid=np.isfinite(effective(values,alpha,mask))
    if name=='dem-points.tif':valid=np.isfinite(dem_values());alpha[:]=255;mask[:]=255
    if name=='code-points.tif':valid[:]=False;valid[509,100]=True;alpha[:]=255;mask[:]=255
    result=np.where(valid,0.,np.nan)
    if name=='code-points.tif':result[509,100]=42
    elif name=='dem-points.tif':
        for row,col,label in ((512,512,2**53-1),(513,512,33),(514,512,9),(514,50,83),(510,200,7)):result[row,col]=label
    else:
        for row,col,label in ((510,512,91),(512,512,7),(512,50,2**53-1),(512,51,5)):result[row,col]=label
        if name=='nested-points.tif':result[511,516]=83;result[511,518]=9
        if name=='snap-points.tif':
            result[valid]=0
            for row,col,label in ((508,50,2**53-1),(511,50,83),(508,60,7)):result[row,col]=label
        if name=='duplicate-points.tif':result[512,512]=91
        if name=='empty-points.tif':result[valid]=0
        if name=='missing-points.tif':result[509,100]=np.inf
        # These invalid finite classes must be excluded by mask/alpha, not
        # interpreted as selected points or used to fill absent data.
        result[512,300]=result[513,400]=result[514,510]=-3
        result[511,200]=np.inf
    return result,alpha,mask


def seed(root):
    import numpy as np
    from osgeo import gdal
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    for name in SOURCES:
        values,alpha,mask=point_values(name) if 'points' in name else direction_arrays()
        if name in ('dem.tif','bowl.tif'):
            values=dem_values(name=='bowl.tif');alpha[:]=255;mask[:]=255
        elif name=='terminal.tif':values[511,50]=0
        elif name=='outside.tif':values[-1,-1]=4
        elif name=='invalid-target.tif':values[509,100]=4
        elif name=='cycle.tif':values[509,10]=1;values[509,11]=16
        elif name=='id-directions.tif':values[:]=np.nan;values[509,100]=0;alpha[:]=255;mask[:]=255
        if name in ('code.tif','invalid-points.tif'):
            dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,2,gdal.GDT_UInt64,['TILED=YES','COMPRESS=DEFLATE'])
            dataset.SetProjection(crs().ExportToWkt());dataset.SetGeoTransform(TRANSFORM)
            for index in (1,2):
                data=np.full((HEIGHT,WIDTH),2**53,dtype=np.uint64)
                if index==2:data[509,100]=2**53+1
                band=dataset.GetRasterBand(index);band.WriteArray(data);band.SetNoDataValueAsUInt64(2**53);band=None
            dataset=None;continue
        dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,4,gdal.GDT_Float64,['TILED=YES','COMPRESS=DEFLATE'])
        dataset.SetProjection(crs().ExportToWkt())
        dataset.SetGeoTransform((500001.,2.,0.,3000000.,0.,-3.) if name=='shifted-points.tif' else (500000.,2.,.1,3000000.,0.,-3.) if name.startswith('rotated-') else TRANSFORM)
        for index,data in enumerate((np.full(values.shape,.25),np.where(np.isnan(values),-9999,values),np.full(values.shape,-9999.),alpha),1):
            band=dataset.GetRasterBand(index);band.WriteArray(data);band.SetNoDataValue(-9999);band=None
        dataset.GetRasterBand(2).SetUnitType('metre' if name in ('dem.tif','bowl.tif') else 'classification')
        dataset.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        if dataset.GetFileList()!=[str(root/name)]:raise FixtureError('source must be self-contained')
        dataset=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}


@lru_cache(maxsize=16)
def expected_directions(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact subcatchments target')
    if CASES[name][4] not in ('direction','fill'):
        result=effective(*direction_arrays());result.flags.writeable=False;return result
    values=dem_values()
    if name=='filled-subcatchments.cog.tif':values[FIRST_ROW+1:-1,1:-1]=1.
    result=np.full(values.shape,np.nan);result[FIRST_ROW:,:]=4.;result[-1,:]=0
    # Filled interior flats drain vertically to the bottom open edge. Their
    # north-up centre paths are strictly shorter than any diagonal alternative.
    if name=='filled-subcatchments.cog.tif':
        import math
        for row,col in np.argwhere(np.isfinite(values)):
            if row not in (FIRST_ROW,HEIGHT-1) and col not in (0,WIDTH-1):continue
            candidates=[]
            for dy,dx,code in NEIGHBOURS:
                y,x=row+dy,col+dx
                if 0<=y<HEIGHT and 0<=x<WIDTH and values[y,x]<values[row,col]:
                    candidates.append((-(values[row,col]-values[y,x])/math.hypot(dx*2,dy*3),y,x,code))
            result[row,col]=min(candidates)[-1] if candidates else 0
    result.flags.writeable=False;return result


def snap_oracle(directions, points):
    """Count every source route, then enumerate all candidate centres; no product graph/search."""
    import math
    import numpy as np
    valid=np.isfinite(directions);counts=np.where(valid,0.,np.nan)
    for row,col in np.argwhere(valid):
        y,x=int(row),int(col);seen={(y,x)}
        while directions[y,x]!=0:
            dy,dx=next((dy,dx) for dy,dx,code in NEIGHBOURS if code==directions[y,x]);y,x=y+dy,x+dx
            if (y,x) in seen or not (0<=y<HEIGHT and 0<=x<WIDTH and valid[y,x]):raise FixtureError('invalid snap oracle route')
            seen.add((y,x));counts[y,x]+=1
    result=np.where(valid,0.,np.nan)
    for row,col in np.argwhere(points>0):
        candidates=[]
        for y,x in np.argwhere(valid):
            distance=math.hypot((int(x)-col)*2,(int(y)-row)*3)
            if distance<=SNAP_RADIUS:candidates.append((-counts[y,x],distance,int(y),int(x)))
        if not candidates:raise FixtureError('snap oracle has no candidate')
        _,_,y,x=min(candidates)
        if result[y,x]!=0:raise FixtureError('snap oracle collision')
        result[y,x]=points[row,col]
    return result


@lru_cache(maxsize=16)
def expected_points(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact subcatchments target')
    result=effective(*point_values(CASES[name][1]))
    if CASES[name][4]=='reclass':
        original=result.copy()
        for value,label in ((91,901),(7,70),(5,55),(83,830),(9,90)):result[original==value]=label
    if CASES[name][4]=='snap':result=snap_oracle(expected_directions(name),result)
    result.flags.writeable=False;return result


def walk_oracle(directions,points,*,full=False):
    # Every source walks its entire route, including downstream of a selected
    # point. There is no production graph/topological order or early truncation.
    import numpy as np
    valid=np.isfinite(directions)
    if not np.array_equal(valid,np.isfinite(points)):raise FixtureError('oracle point coverage differs')
    selected=points[valid&(points>0)]
    if not selected.size or len(set(map(int,selected)))!=selected.size:raise FixtureError('oracle points absent or duplicate')
    result=np.where(valid,0.,np.nan)
    for start_row,start_col in np.argwhere(valid):
        row,col=int(start_row),int(start_col);visited=set();found=[]
        while True:
            if (row,col) in visited:raise FixtureError('oracle cycle')
            visited.add((row,col))
            if points[row,col]>0:found.append(points[row,col])
            if directions[row,col]==0:break
            step=next(((dy,dx) for dy,dx,code in NEIGHBOURS if code==directions[row,col]),None)
            if step is None:raise FixtureError('oracle illegal direction')
            row,col=row+step[0],col+step[1]
            if not (0<=row<HEIGHT and 0<=col<WIDTH and valid[row,col]):raise FixtureError('oracle invalid route')
        if full and len(found)>1:raise FixtureError('oracle nested points')
        if found:result[start_row,start_col]=found[0]
    result.flags.writeable=False;return result


@lru_cache(maxsize=16)
def expected_pixels(name):
    return walk_oracle(expected_directions(name),expected_points(name),full=CASES[name][4]=='reference')


def subcatchments_evidence(actual,name):
    import numpy as np
    expected=expected_pixels(name);directions=expected_directions(name);points=expected_points(name);valid=np.isfinite(directions)
    if not np.array_equal(actual,expected,equal_nan=True):raise FixtureError('independent complete subcatchments IDs differ')
    ids=sorted(map(int,points[valid&(points>0)]))
    if sorted(map(int,np.unique(actual[valid&(actual>0)])))!=ids:raise FixtureError('user point IDs were changed')
    if not np.array_equal(actual[points>0],points[points>0]):raise FixtureError('selected point itself was excluded')
    counts=[int(np.count_nonzero(actual==label)) for label in ids];zeros=int(np.count_nonzero(valid&(actual==0)))
    if sum(counts)+zeros!=int(valid.sum()):raise FixtureError('subcatchments partition count differs')
    nested=0
    for row,col in np.argwhere(points>0):
        y,x=int(row),int(col)
        while directions[y,x]!=0:
            dy,dx=next((dy,dx) for dy,dx,code in NEIGHBOURS if code==directions[y,x]);y,x=y+dy,x+dx
            if points[y,x]>0:nested+=1;break
    stage=CASES[name][4]
    if (stage in ('nested','reclass','direction','fill','snap')) != (nested>0):raise FixtureError('expected nested point routes differ')
    moved=0
    if stage=='snap':
        original=effective(*point_values('snap-points.tif'))
        for label in ids:
            moved+=int(not np.array_equal(np.argwhere(original==label),np.argwhere(points==label)))
        if moved<1:raise FixtureError('snapping did not move any point')
    return {'subcatchments_verified':True,'point_ids_verified':True,'point_self_verified':True,
            'nested_supported_verified':True,'nested_point_routes':nested,'snapped_points_moved':moved,
            'unselected_zero_verified':True,'first_downstream_point_verified':True,'disjoint_verified':True,'partition_conservation_verified':True,'point_ids':ids,
            'subcatchments_pixel_counts':counts,'zero_pixels':zeros}


def artifact_expectation(name):
    if name not in CASES:raise FixtureError('unknown exact incremental subcatchments target')
    corners=[point(x,y) for x,y in ((0,0),(WIDTH,0),(0,HEIGHT),(WIDTH,HEIGHT))]
    return {'width':WIDTH,'height':HEIGHT,'band_count':1,'source_crs':'EPSG:3857','extent_srid':3857,
        'transform':list(TRANSFORM),'band_nodata':None,
        'extent':[min(p[0] for p in corners),min(p[1] for p in corners),max(p[0] for p in corners),max(p[1] for p in corners)]}


def verify(root,names):
    import numpy as np
    from osgeo import gdal,osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(name not in TARGETS for name in names):raise FixtureError('invalid exact incremental subcatchments target inventory')
    if sorted(p.name for p in root.iterdir())!=sorted([*SOURCES,'outputs','.addp-online-owner']):raise FixtureError('unexpected source sidecar or temporary file')
    if sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):raise FixtureError('unexpected output, sidecar or temporary file')
    result={'sources':{name:digest(root/name) for name in SOURCES},'targets':{}}
    for name in names:
        path=root/'outputs'/name;fact=digest(path);dataset=gdal.Open(str(path))
        if dataset is None or dataset.GetDriver().ShortName!='GTiff' or dataset.RasterCount!=1:raise FixtureError('expected one-band GeoTIFF')
        if (dataset.RasterXSize,dataset.RasterYSize,dataset.GetGeoTransform())!=(WIDTH,HEIGHT,TRANSFORM):raise FixtureError('source grid changed')
        actual_crs=osr.SpatialReference(wkt=dataset.GetProjection())
        if not actual_crs.IsSame(crs()):raise FixtureError('source CRS changed')
        band=dataset.GetRasterBand(1);actual=band.ReadAsArray();expected=expected_pixels(name)
        if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue()):raise FixtureError('Float64 or NaN NoData changed')
        if not np.array_equal(actual,expected,equal_nan=True):raise FixtureError('complete pixel values differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray(),np.where(np.isfinite(expected),255,0)):raise FixtureError('complete pixel mask differs')
        if validate(dataset,full_check=True)[1]:raise FixtureError('COG full layout validation failed')
        if band.GetOverviewCount()<1:raise FixtureError('expected COG overviews for cross-block image')
        if band.GetColorTable() is not None or band.GetUnitType()!='' or band.GetScale() is not None or band.GetOffset() is not None:raise FixtureError('classification unit or output scale/offset/color differs')
        for index in range(band.GetOverviewCount()):
            overview=band.GetOverview(index).ReadAsArray();valid=np.isfinite(overview)
            if not set(np.unique(overview[valid]))<=set(np.unique(expected[np.isfinite(expected)])):raise FixtureError('overview invented a subcatchments ID')
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':True,'full_pixel_and_mask_verification':True,
            'valid_pixels':int(np.isfinite(expected).sum()),'unit_cleared_verified':True,'unit':'',**subcatchments_evidence(actual,name)}
        band=dataset=None
    if 'full-reference.cog.tif' in names:
        left=gdal.Open(str(root/'outputs'/'subcatchments.cog.tif'));right=gdal.Open(str(root/'outputs'/'full-reference.cog.tif'))
        if not np.array_equal(left.ReadAsArray(),right.ReadAsArray(),equal_nan=True):raise FixtureError('nonnested full reference differs')
        left=right=None;result['nonnested_reference_equal']=True
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
        return NFSExport('subcatchments-d8',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'subcatchments-d8-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'subcatchments-d8-source.json').read_text())['sources']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster D8 incremental subcatchments fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
