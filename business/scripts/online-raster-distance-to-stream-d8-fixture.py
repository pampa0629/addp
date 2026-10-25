#!/usr/bin/env python3
"""Owned NFS fixture and independent whole-pixel oracle for D8 distance to stream."""
from __future__ import annotations
from functools import lru_cache
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE='raster-distance-to-stream-d8-persistence'
RUNTIME='addp-raster-distance-to-stream-d8-runtime'
CASES={'distance.cog.tif':('directions.tif','streams.tif',2,2,'direct'),
       'feet.cog.tif':('feet-directions.tif','feet-streams.tif',2,2,'direct'),
       'survey-feet.cog.tif':('survey-directions.tif','survey-streams.tif',2,2,'direct'),
       'threshold.cog.tif':('directions.tif',None,2,1,'threshold'),
       'dem.cog.tif':('dem.tif',None,2,1,'direction'),
       'filled.cog.tif':('bowl.tif',None,2,1,'fill'),
       'empty.cog.tif':('directions.tif','empty-streams.tif',2,2,'empty'),
       'invalid.cog.tif':('directions.tif','streams.tif',3,3,'invalid')}
TARGETS=tuple(CASES)
SOURCES=('directions.tif','streams.tif','feet-directions.tif','feet-streams.tif','survey-directions.tif','survey-streams.tif',
         'empty-streams.tif','dem.tif','bowl.tif','broken-streams.tif','nonbinary-streams.tif','coverage-streams.tif',
         'shifted-streams.tif','code.tif','code-streams.tif','terminal.tif','outside.tif','invalid.tif','cycle.tif','rotated.tif')
SOURCE_UNITS={'feet-directions.tif':(2222,.3048),'feet-streams.tif':(2222,.3048),
              'survey-directions.tif':(2277,1200/3937),'survey-streams.tif':(2277,1200/3937)}
TRANSFORM=(500000.,2.,0.,3000000.,0.,-3.)
WIDTH,HEIGHT=519,515
FIRST_ROW=508
NEIGHBOURS=((-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2))
RTOL,ATOL=1e-12,1e-9

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
    return NFSExport('distance-to-stream-d8', command).guard(env)


def owned_root(secret):
    return NFSExport('distance-to-stream-d8', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}


def point(x, y):
    a,b,c,d,e,f = TRANSFORM
    return a+b*x+c*y, d+e*x+f*y


def source_crs(name):
    from osgeo import osr
    code, factor = SOURCE_UNITS.get(name, (3857, 1.))
    crs = osr.SpatialReference(); crs.ImportFromEPSG(code)
    if abs(crs.GetLinearUnits()-factor)>1e-15:
        raise FixtureError('fixture CRS linear unit differs from the independent metre factor')
    return crs


def direction_arrays():
    import numpy as np
    values=np.full((HEIGHT,WIDTH),np.nan);values[FIRST_ROW:,:]=1.;values[FIRST_ROW:,-1]=0
    alpha=np.full(values.shape,255.);mask=np.full(values.shape,255,np.uint8)
    # Every removed centre's western predecessor becomes a legal open zero.
    for row,col in ((510,100),(511,200),(512,300),(513,400),(514,510)):
        values[row,col-1]=0
    values[510,100]=np.nan;values[511,200]=np.inf
    values[512,300]=3;alpha[512,300]=0
    values[513,400]=3;alpha[513,400]=np.nan
    values[514,510]=3;mask[514,510]=0
    # Nested tributaries cross the 512 seam. the tree has one right outlet and several first-hit locations.
    for (row,col),code in { (1,1):2,(1,3):8,(2,2):2,(5,1):128,(5,3):32,(4,2):128,
            (2,4):8,(3,3):1,(3,4):1,(3,5):1,(3,6):1,(3,7):1,(3,8):0,(2,6):4}.items():
        values[FIRST_ROW+row,510+col]=code
    # Crossing diagonals have separate cell-centre routes and separate outlets.
    for cell,code in {(508,20):0,(508,21):0,(509,20):128,(509,21):32}.items():values[cell]=code
    alpha[511,515]=128
    return values,alpha,mask


def effective(values,alpha,mask):
    import numpy as np
    return np.where(np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0),values,np.nan)


def binary_values():
    import numpy as np
    values,alpha,mask=direction_arrays();valid=np.isfinite(effective(values,alpha,mask))
    result=np.where(valid,0.,np.nan)
    for row,col in ((1,1),(1,3),(2,2),(5,1),(5,3),(4,2),(2,4),(3,3),(3,4),(3,5),(3,6),(3,7),(3,8),(2,6)):
        result[FIRST_ROW+row,510+col]=1
    for cell in ((508,20),(508,21),(509,20),(509,21)):result[cell]=1
    # Leave upstream branches unselected so first-hit distances include vertical
    # and diagonal edges; selected downstream routes remain closed.
    for cell in ((509,511),(509,513),(510,512),(513,511),(513,513),(510,514),(510,516),(509,20),(509,21)):result[cell]=0
    # Hidden nonbinary values are excluded by each source's native validity.
    result[511,200]=np.inf;result[512,300]=result[513,400]=result[514,510]=3
    return result,alpha,mask


def dem_values(filled=False):
    import numpy as np
    result=np.full((HEIGHT,WIDTH),np.nan)
    result[FIRST_ROW:,:]=np.arange(WIDTH,0,-1,dtype=float)
    if filled:result[FIRST_ROW+1:-1,1:-1]=-2
    return result


def seed(root):
    import numpy as np
    from osgeo import gdal
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    for name in SOURCES:
        values,alpha,mask=binary_values() if 'streams' in name else direction_arrays()
        if name in ('dem.tif','bowl.tif'):
            values=dem_values(name=='bowl.tif');alpha[:]=255;mask[:]=255
        elif name=='cycle.tif':values[511,514]=1;values[511,515]=16
        elif name=='terminal.tif':values[511,514]=0
        elif name=='outside.tif':values[514,100]=4
        elif name=='invalid.tif':values[509,100]=4
        elif name=='empty-streams.tif':values=np.where(np.isfinite(effective(values,alpha,mask)),0.,np.nan)
        elif name=='broken-streams.tif':values[511,514]=0
        elif name=='nonbinary-streams.tif':values[509,100]=2
        elif name=='coverage-streams.tif':values[509,100]=np.nan
        elif name=='code-streams.tif':
            values[:]=np.nan;values[509,100]=0;alpha[:]=255;mask[:]=255
        if name=='code.tif':
            dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,2,gdal.GDT_UInt64,['TILED=YES','COMPRESS=DEFLATE'])
            dataset.SetProjection(source_crs(name).ExportToWkt());dataset.SetGeoTransform(TRANSFORM)
            for index in (1,2):
                data=np.full((HEIGHT,WIDTH),2**53,dtype=np.uint64)
                if index==2:data[509,100]=2**53+1
                band=dataset.GetRasterBand(index);band.WriteArray(data);band.SetNoDataValueAsUInt64(2**53);band=None
            dataset=None;continue
        dataset=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,4,gdal.GDT_Float64,['TILED=YES','COMPRESS=DEFLATE'])
        dataset.SetProjection(source_crs(name).ExportToWkt())
        dataset.SetGeoTransform((500001.,2.,0.,3000000.,0.,-3.) if name=='shifted-streams.tif' else (500000.,2.,.25,3000000.,0.,-3.) if name=='rotated.tif' else TRANSFORM)
        # Band 1 is deliberately unsuitable as direction and differs from the
        # selected river mask. Band 3 is all NoData for the all-invalid chain.
        for index,data in enumerate((np.full(values.shape,.25),np.where(np.isnan(values),-9999,values),np.full(values.shape,-9999.),alpha),1):
            band=dataset.GetRasterBand(index);band.WriteArray(data);band.SetNoDataValue(-9999);band=None
        dataset.GetRasterBand(2).SetUnitType('metre' if name in ('dem.tif','bowl.tif') else 'classification')
        dataset.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        if dataset.GetFileList()!=[str(root/name)]:raise FixtureError('source must be self-contained')
        dataset=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}


@lru_cache(maxsize=8)
def expected_directions(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact distance target')
    if name=='invalid.cog.tif':return np.full((HEIGHT,WIDTH),np.nan)
    if CASES[name][4] not in ('direction','fill'):
        result=effective(*direction_arrays());result.flags.writeable=False;return result
    values=dem_values()
    if name=='filled.cog.tif':values[FIRST_ROW+1:-1,1:-1]=1.
    result=np.full(values.shape,np.nan);result[FIRST_ROW:,:]=1.;result[FIRST_ROW:,-1]=0
    # Interior filled flats take the horizontal shortest centre path to the
    # right open edge. Only the perimeter has a strict-downhill slope choice.
    if name=='filled.cog.tif':
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


def backward_oracle(directions):
    # Independent scalar reverse traversal with an explicit call stack and
    # memoized tributaries. No production graph, indegree queue or forward pass.
    import numpy as np
    valid=np.isfinite(directions);selected=valid
    incoming={int(row*WIDTH+col):[] for row,col in np.argwhere(selected)}
    for cell in incoming:
        row,col=divmod(cell,WIDTH);code=directions[row,col]
        if code==0:continue
        step=next(((dy,dx) for dy,dx,value in NEIGHBOURS if value==code),None)
        if step is None:raise FixtureError('oracle invalid direction')
        y,x=row+step[0],col+step[1];target=y*WIDTH+x
        if not (0<=y<HEIGHT and 0<=x<WIDTH and target in incoming):raise FixtureError('oracle stream break')
        incoming[target].append(cell)
    memo={};active=set()
    for origin in incoming:
        stack=[(origin,False)]
        while stack:
            cell,expanded=stack.pop()
            if cell in memo:continue
            if expanded:
                donors=[memo[d] for d in incoming[cell]]
                memo[cell]=sum(d+1 for d in donors)
                active.remove(cell)
            else:
                if cell in active:raise FixtureError('oracle direction cycle')
                active.add(cell);stack.append((cell,True));stack.extend((d,False) for d in incoming[cell] if d not in memo)
    result=np.where(valid,0.,np.nan)
    for cell,value in memo.items():result.flat[cell]=value
    result.flags.writeable=False;return result


@lru_cache(maxsize=8)
def expected_streams(name):
    import numpy as np
    directions=expected_directions(name);stage=CASES[name][4]
    if stage in ('threshold','direction','fill'):
        return np.where(np.isfinite(directions),np.where(backward_oracle(directions)>=2,1.,0.),np.nan)
    if stage=='invalid':return np.full(directions.shape,np.nan)
    values=effective(*binary_values())
    return np.where(np.isfinite(directions),0.,np.nan) if stage=='empty' else values


@lru_cache(maxsize=8)
def walked_paths(stage):
    """Track each original source independently; no indegree/topological propagation."""
    import numpy as np
    name=next(n for n,c in CASES.items() if c[4]==stage)
    directions,streams=expected_directions(name),expected_streams(name)
    valid=np.isfinite(directions);flat=directions.ravel();river=streams.ravel()==1
    destination=np.full(directions.size,-1,np.int64)
    horizontal=np.zeros(directions.size,np.int64);vertical=horizontal.copy();diagonal=horizontal.copy()
    for dy,dx,code in NEIGHBOURS:
        rows,cols=np.nonzero(directions==code);ys,xs=rows+dy,cols+dx
        if np.any((ys<0)|(ys>=HEIGHT)|(xs<0)|(xs>=WIDTH)) or np.any(~valid[ys,xs]):raise FixtureError('oracle invalid downstream')
        destination[rows*WIDTH+cols]=ys*WIDTH+xs
    donors=np.flatnonzero(river&(destination>=0))
    if np.any(~river[destination[donors]]):raise FixtureError('oracle stream break')
    origins=np.flatnonzero(valid);current=origins.copy();hit=np.zeros(directions.size,bool)
    for _ in range(int(valid.sum())+1):
        reached=river[current];hit[origins[reached]]=True
        keep=~reached&(destination[current]>=0);origins,current=origins[keep],current[keep]
        if not len(origins):break
        codes=flat[current]
        horizontal[origins]+=np.isin(codes,(1,16));vertical[origins]+=np.isin(codes,(4,64));diagonal[origins]+=np.isin(codes,(2,8,32,128))
        current=destination[current]
    else:raise FixtureError('oracle direction cycle')
    for a in (horizontal,vertical,diagonal,hit):a.flags.writeable=False
    return horizontal,vertical,diagonal,hit


@lru_cache(maxsize=8)
def expected_pixels(name):
    import math
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact distance target')
    horizontal,vertical,diagonal,hit=walked_paths(CASES[name][4])
    factor=SOURCE_UNITS.get(CASES[name][0],(3857,1.))[1];w,h=2*factor,3*factor
    result=np.full(WIDTH*HEIGHT,np.nan)
    for cell in np.flatnonzero(hit):result[cell]=math.fsum((int(horizontal[cell])*w,int(vertical[cell])*h,int(diagonal[cell])*math.hypot(w,h)))
    result=result.reshape(HEIGHT,WIDTH);result.flags.writeable=False;return result


def distance_evidence(actual,name):
    import numpy as np
    expected=expected_pixels(name);directions=expected_directions(name);streams=expected_streams(name)
    valid=np.isfinite(directions);reached=np.isfinite(expected);river=streams==1;missed=valid&~reached
    if not np.array_equal(np.isfinite(actual),reached):raise FixtureError('first-hit distance coverage differs')
    if np.any(actual[river]!=0):raise FixtureError('stream centres must be zero')
    if np.any(~np.isnan(actual[missed])):raise FixtureError('unreachable centres must be NoData')
    if np.any(actual[reached&~river]<=0):raise FixtureError('reached nonriver distance must be positive')
    if not np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True):raise FixtureError('complete independent path distances differ')
    return {'independent_distance_oracle_verified':True,'first_hit_verified':True,'river_zero_verified':True,
            'unreachable_nodata_verified':True,'stream_closure_verified':True,'metre_unit_verified':True,
            'source_valid_pixels':int(valid.sum()),'river_pixels':int(river.sum()),'unreachable_pixels':int(missed.sum()),
            'reached_nonriver_pixels':int((reached&~river).sum()),'metres_per_source_unit':SOURCE_UNITS.get(CASES[name][0],(3857,1.))[1]}


def artifact_expectation(name):
    corners=[point(x,y) for x,y in ((0,0),(WIDTH,0),(0,HEIGHT),(WIDTH,HEIGHT))]
    return {'width':WIDTH,'height':HEIGHT,'band_count':1,'source_crs':'EPSG:'+str(SOURCE_UNITS.get(CASES[name][0],(3857,1.))[0]),'extent_srid':SOURCE_UNITS.get(CASES[name][0],(3857,1.))[0],
        'transform':list(TRANSFORM),'band_nodata':None,
        'extent':[min(p[0] for p in corners),min(p[1] for p in corners),max(p[0] for p in corners),max(p[1] for p in corners)]}


def nearest_overviews(dataset,expected,name):
    import numpy as np
    from osgeo import gdal
    band=dataset.GetRasterBand(1)
    # GDAL COG uses nearest half-up source indices and cascades from the
    # previous level. Independent NumPy oracle, no product/COG encoder call:
    # https://github.com/OSGeo/gdal/blob/v3.12.1/gcore/overview.cpp
    # GDALResampleChunk_NearT and GDALRegenerateOverviewsMultiBand.
    previous=expected
    for i in range(band.GetOverviewCount()):
        actual=band.GetOverview(i);width,height=actual.XSize,actual.YSize
        if width<1 or height<1 or width>=previous.shape[1] or height>=previous.shape[0]:raise FixtureError('invalid overview dimensions')
        rows=np.floor(.5+np.arange(height)*(previous.shape[0]/height)).astype(np.int64)
        cols=np.floor(.5+np.arange(width)*(previous.shape[1]/width)).astype(np.int64)
        wanted=previous[np.ix_(rows,cols)];values=actual.ReadAsArray()
        equal=np.allclose(values,wanted,rtol=RTOL,atol=ATOL,equal_nan=True)
        if not equal or not np.array_equal(actual.GetMaskBand().ReadAsArray()!=0,np.isfinite(wanted)):
            raise FixtureError('complete nearest overview pixels or mask differ')
        previous=wanted
    actual=band=None


def verify(root, names):
    import numpy as np
    from osgeo import gdal, osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(name not in TARGETS for name in names):
        raise FixtureError('invalid exact D8 distance to stream target inventory')
    # NFSExport.owned_root validates this lifecycle marker against the run ID
    # before invoking the worker. It is required inventory, never a sidecar.
    if sorted(p.name for p in root.iterdir())!=sorted([*SOURCES,'outputs','.addp-online-owner']):
        raise FixtureError('unexpected source sidecar or temporary file')
    if sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):
        raise FixtureError('unexpected output, sidecar or temporary file')
    result={'sources':{name:digest(root/name) for name in SOURCES},'targets':{}}
    for name in names:
        crs=source_crs(CASES[name][0])
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
        if not np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True):
            raise FixtureError('complete pixel values differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray(),np.where(np.isfinite(expected),255,0)):
            raise FixtureError('complete pixel mask differs')
        _,errors,_=validate(dataset,full_check=True)
        if errors:raise FixtureError('COG full layout validation failed')
        if band.GetOverviewCount()<1:raise FixtureError('expected COG overviews for cross-block image')
        if band.GetColorTable() is not None:raise FixtureError('unexpected DEM color table')
        if band.GetUnitType()!='metre':raise FixtureError('distance band must use metres')
        if band.GetScale() is not None or band.GetOffset() is not None:raise FixtureError('source scale/offset leaked into output')
        valid=np.isfinite(expected)
        routing=distance_evidence(actual,name)
        nearest_overviews(dataset,expected,name)
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,
            'valid_pixels':int(valid.sum()),'full_pixel_and_mask_verification':True,
            'nearest_overviews_verified':True,**routing}
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
        return NFSExport('distance-to-stream-d8',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'distance-to-stream-d8-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'distance-to-stream-d8-source.json').read_text())['sources']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster D8 distance to stream fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
