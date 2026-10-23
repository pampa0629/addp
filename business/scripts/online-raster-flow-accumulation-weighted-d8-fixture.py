#!/usr/bin/env python3
"""Owned NFS fixture and independent whole-pixel oracle for D8 weighted flow accumulation."""
from __future__ import annotations
from functools import lru_cache
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-flow-accumulation-weighted-d8-persistence'
RUNTIME = 'addp-raster-flow-accumulation-weighted-d8-runtime'
CASES = {'weighted.cog.tif':('directions.tif','weights.tif',2,2,'direct'),
         'ones.cog.tif':('directions.tif','ones-weights.tif',2,2,'ones'),
         'reclass.cog.tif':('directions.tif','weights.tif',2,2,'reclass'),
         'dem-weighted.cog.tif':('dem.tif','dem-weights.tif',2,2,'direction'),
         'filled-weighted.cog.tif':('bowl.tif','dem-weights.tif',2,2,'fill'),
         'invalid-weighted.cog.tif':('directions.tif','weights.tif',3,3,'invalid')}
TARGETS=tuple(CASES)
SOURCES=('directions.tif','weights.tif','ones-weights.tif','dem.tif','bowl.tif','dem-weights.tif',
         'negative-weights.tif','missing-weights.tif','shifted-weights.tif',
         'code.tif','code-weights.tif','cycle.tif','overflow-weights.tif')
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
    return NFSExport('flow-accumulation-weighted-d8', command).guard(env)


def owned_root(secret):
    return NFSExport('flow-accumulation-weighted-d8', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


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


def weight_values(name='weights.tif'):
    import numpy as np
    values,alpha,mask=direction_arrays()
    if name=='dem-weights.tif':
        valid=np.isfinite(dem_values());alpha[:]=255;mask[:]=255
    elif name=='code-weights.tif':
        valid=np.zeros(values.shape,bool);valid[509,100]=True;alpha[:]=255;mask[:]=255
    else:valid=np.isfinite(effective(values,alpha,mask))
    rows,cols=np.indices(values.shape)
    result=np.where(valid,((rows-FIRST_ROW+cols)%9)/8.,np.nan)
    if name in ('ones-weights.tif','code-weights.tif'):result[valid]=1.
    if name=='negative-weights.tif':result[509,100]=-1
    if name=='missing-weights.tif':result[509,100]=np.inf
    if name=='overflow-weights.tif':result[valid]=1.7e308
    # Invalid finite negative values must stay excluded by native coverage.
    if name not in ('dem-weights.tif','code-weights.tif'):
        result[512,300]=result[513,400]=result[514,510]=-3
        result[511,200]=np.inf
    return result,alpha,mask


def seed(root):
    import numpy as np
    from osgeo import gdal
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    for name in SOURCES:
        values,alpha,mask=weight_values(name) if 'weights' in name else direction_arrays()
        if name in ('dem.tif','bowl.tif'):
            values=dem_values(name=='bowl.tif');alpha[:]=255;mask[:]=255
        elif name=='cycle.tif':values[509,10]=1;values[509,11]=16
        if name=='code.tif':
            dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,2,gdal.GDT_UInt64,['TILED=YES','COMPRESS=DEFLATE'])
            dataset.SetProjection(crs().ExportToWkt());dataset.SetGeoTransform(TRANSFORM)
            for index in (1,2):
                data=np.full((HEIGHT,WIDTH),2**53,dtype=np.uint64)
                if index==2:data[509,100]=2**53+1
                band=dataset.GetRasterBand(index);band.WriteArray(data);band.SetNoDataValueAsUInt64(2**53);band=None
            dataset=None;continue
        dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,4,gdal.GDT_Float64,['TILED=YES','COMPRESS=DEFLATE'])
        dataset.SetProjection(crs().ExportToWkt())
        dataset.SetGeoTransform((500001.,2.,0.,3000000.,0.,-3.) if name=='shifted-weights.tif' else TRANSFORM)
        # Band 1 differs from the explicitly selected band 2. Band 3 is all
        # NoData for both roles in the all-invalid chain; alpha 128 remains valid.
        for index,data in enumerate((np.full(values.shape,.25),np.where(np.isnan(values),-9999,values),np.full(values.shape,-9999.),alpha),1):
            band=dataset.GetRasterBand(index);band.WriteArray(data);band.SetNoDataValue(-9999);band=None
        dataset.GetRasterBand(2).SetUnitType('metre' if name in ('dem.tif','bowl.tif') else
            'm3' if name=='dem-weights.tif' else '' if name=='ones-weights.tif' else 'kg' if 'weights' in name else 'classification')
        dataset.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        if dataset.GetFileList()!=[str(root/name)]:raise FixtureError('source must be self-contained')
        dataset=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}


@lru_cache(maxsize=6)
def expected_directions(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact weighted accumulation target')
    if name=='invalid-weighted.cog.tif':return np.full((HEIGHT,WIDTH),np.nan)
    if CASES[name][4] not in ('direction','fill'):
        result=effective(*direction_arrays());result.flags.writeable=False;return result
    values=dem_values()
    if name=='filled-weighted.cog.tif':values[FIRST_ROW+1:-1,1:-1]=1.
    result=np.full(values.shape,np.nan);result[FIRST_ROW:,:]=4.;result[-1,:]=0
    # Filled interior flats drain vertically to the bottom open edge. Their
    # north-up centre paths are strictly shorter than any diagonal alternative.
    if name=='filled-weighted.cog.tif':
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


@lru_cache(maxsize=6)
def expected_weights(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact weighted accumulation target')
    if CASES[name][4]=='invalid':return np.full((HEIGHT,WIDTH),np.nan)
    result=effective(*weight_values(CASES[name][1]))
    if CASES[name][4]=='reclass':result=np.where(np.isfinite(result),np.where(result<.5,0.,2.),np.nan)
    result.flags.writeable=False;return result


def walk_oracle(directions,weights):
    # Per-origin downstream walks independently distribute each original weight.
    # No production graph, indegree queue, topological pass or intermediate sum.
    import math
    import numpy as np
    valid=np.isfinite(directions)
    if not np.array_equal(valid,np.isfinite(weights)) or np.any(weights[valid]<0):raise FixtureError('oracle weights coverage/value differs')
    result=np.where(valid,0.,np.nan)
    for start_row,start_col in np.argwhere(valid):
        row,col=int(start_row),int(start_col);visited={(row,col)};weight=float(weights[row,col])
        while directions[row,col]!=0:
            step=next(((dy,dx) for dy,dx,code in NEIGHBOURS if code==directions[row,col]),None)
            if step is None:raise FixtureError('oracle illegal direction')
            row,col=row+step[0],col+step[1]
            if not (0<=row<HEIGHT and 0<=col<WIDTH and valid[row,col]) or (row,col) in visited:raise FixtureError('oracle invalid route')
            visited.add((row,col));result[row,col]+=weight
    outlets=directions==0
    if math.fsum(map(float,result[outlets]))+math.fsum(map(float,weights[outlets]))!=math.fsum(map(float,weights[valid])):raise FixtureError('oracle terminal mass balance differs')
    result.flags.writeable=False;return result


@lru_cache(maxsize=6)
def expected_pixels(name):
    return walk_oracle(expected_directions(name),expected_weights(name))


def expected_unit(name):
    return 'm3' if CASES[name][4] in ('direction','fill') else 'kg' if CASES[name][4]=='direct' else ''


def weighted_evidence(actual,name):
    import math
    import numpy as np
    expected=expected_pixels(name);directions=expected_directions(name);weights=expected_weights(name);valid=np.isfinite(directions)
    if not np.array_equal(actual,expected,equal_nan=True):raise FixtureError('independent complete weighted sums differ')
    targets=set()
    for row,col in np.argwhere(valid&(directions!=0)):
        dy,dx=next((dy,dx) for dy,dx,code in NEIGHBOURS if code==directions[row,col]);targets.add((row+dy,col+dx))
    if any(actual[row,col]!=0 for row,col in np.argwhere(valid) if (row,col) not in targets):raise FixtureError('source sum includes own weight')
    outlets=directions==0;total=math.fsum(map(float,weights[valid]));outlet_total=math.fsum(map(float,actual[outlets]))+math.fsum(map(float,weights[outlets]))
    if total!=outlet_total:raise FixtureError('actual terminal weight mass balance differs')
    fact={'weighted_sum_verified':True,'self_exclusion_verified':True,'mass_balance_verified':True,
          'valid_zero_verified':True,'weight_total':total,'outlet_total_with_self':outlet_total,
          'zero_pixels':int(np.count_nonzero(valid&(actual==0)))}
    if name=='ones.cog.tif':
        counts=walk_oracle(directions,np.where(valid,1.,np.nan))
        if not np.array_equal(actual,counts,equal_nan=True):raise FixtureError('all-one weights differ from unweighted counts')
        fact['all_ones_equivalent_verified']=True
    return fact


def artifact_expectation(name):
    if name not in CASES:raise FixtureError('unknown exact weighted accumulation target')
    corners=[point(x,y) for x,y in ((0,0),(WIDTH,0),(0,HEIGHT),(WIDTH,HEIGHT))]
    return {'width':WIDTH,'height':HEIGHT,'band_count':1,'source_crs':'EPSG:3857','extent_srid':3857,
        'transform':list(TRANSFORM),'band_nodata':None,
        'extent':[min(p[0] for p in corners),min(p[1] for p in corners),max(p[0] for p in corners),max(p[1] for p in corners)]}


def verify(root,names):
    import numpy as np
    from osgeo import gdal,osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(name not in TARGETS for name in names):raise FixtureError('invalid exact weighted accumulation target inventory')
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
        if band.GetColorTable() is not None or band.GetUnitType()!=expected_unit(name) or band.GetScale() is not None or band.GetOffset() is not None:raise FixtureError('weight unit or output scale/offset/color differs')
        for index in range(band.GetOverviewCount()):
            overview=band.GetOverview(index).ReadAsArray();valid=np.isfinite(overview)
            if not set(np.unique(overview[valid]))<=set(np.unique(expected[np.isfinite(expected)])):raise FixtureError('overview invented a weighted sum')
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':True,'full_pixel_and_mask_verification':True,
            'valid_pixels':int(np.isfinite(expected).sum()),'weight_unit_verified':True,'unit':expected_unit(name),**weighted_evidence(actual,name)}
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
        return NFSExport('flow-accumulation-weighted-d8',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'flow-accumulation-weighted-d8-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'flow-accumulation-weighted-d8-source.json').read_text())['sources']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster D8 weighted flow accumulation fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
