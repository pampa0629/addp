#!/usr/bin/env python3
"""Owned NFS fixture and independent whole-pixel oracle for pour-point snapping."""
from __future__ import annotations
from functools import lru_cache
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-snap-pour-points-persistence'
RUNTIME = 'addp-raster-snap-pour-points-runtime'
CASES = {
 'snap.cog.tif':('counts.tif','points.tif',2,2,'direct',5.),
 'foot.cog.tif':('foot-counts.tif','foot-points.tif',2,2,'direct',1.524001),
 'survey.cog.tif':('survey-counts.tif','survey-points.tif',2,2,'direct',1.524001),
 'zero.cog.tif':('counts.tif','points.tif',2,2,'direct',0.),
 'reclass.cog.tif':('counts.tif','points.tif',2,2,'reclass',4.),
 'dem-snap.cog.tif':('dem.tif','dem-points.tif',2,2,'direction',4.),
 'filled-snap.cog.tif':('bowl.tif','dem-points.tif',2,2,'fill',4.),
 'watershed.cog.tif':('directions.tif','points.tif',2,2,'watershed',4.),
}
TARGETS=tuple(CASES)
SOURCES=('counts.tif','points.tif','foot-counts.tif','foot-points.tif','survey-counts.tif','survey-points.tif',
 'directions.tif','dem.tif','bowl.tif','dem-points.tif','nested-points.tif','duplicate-points.tif','empty-points.tif',
 'missing-points.tif','shifted-points.tif','rotated-counts.tif','collision-points.tif',
 'invalid-counts.tif','invalid-points.tif','id-counts.tif','id-points.tif')
TRANSFORM=(500000.,2.,0.,3000000.,0.,-3.)
WIDTH,HEIGHT=519,515
FIRST_ROW=508


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
    return NFSExport('snap-pour-points', command).guard(env)


def owned_root(secret):
    return NFSExport('snap-pour-points', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}


def point(x, y):
    a,b,c,d,e,f = TRANSFORM
    return a+b*x+c*y, d+e*x+f*y


def crs(name='snap.cog.tif'):
    from osgeo import osr
    result=osr.SpatialReference();result.ImportFromEPSG(2222 if name.startswith('foot') else 2277 if name.startswith('survey') else 3857)
    return result


def direction_arrays():
    import numpy as np
    values=np.full((HEIGHT,WIDTH),np.nan);values[FIRST_ROW:,:]=4.;values[-1,:]=0
    alpha=np.full(values.shape,255.);mask=np.full(values.shape,255,np.uint8)
    for row,col in ((510,100),(511,200),(512,300),(513,400),(514,450),(510,52),(512,51),(512,53)):
        values[row-1,col]=0
        values[row,col]=np.nan
    values[511,200]=np.inf;values[512,300]=-3;alpha[512,300]=0
    values[513,400]=-3;alpha[513,400]=np.nan;values[514,450]=-3;mask[514,450]=0
    values[508,511]=values[509,511]=1
    # Isolated, equal north/south counts give an exact row-order tie at the centre.
    values[98,100]=4;values[99,100]=values[100,100]=values[101,100]=0;values[102,100]=64
    alpha[511,510]=128
    return values,alpha,mask


def effective(values,alpha,mask):
    import numpy as np
    return np.where(np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0),values,np.nan)


def dem_values(filled=False):
    import numpy as np
    result=np.full((HEIGHT,WIDTH),np.nan);result[FIRST_ROW:,:]=np.arange(HEIGHT-FIRST_ROW,0,-1,dtype=float)[:,None]
    if filled:result[FIRST_ROW+1:-1,1:-1]=-2
    return result


def count_oracle(directions):
    # Independent per-origin walks add one to each downstream cell, excluding self.
    import numpy as np
    steps={1:(0,1),2:(1,1),4:(1,0),8:(1,-1),16:(0,-1),32:(-1,-1),64:(-1,0),128:(-1,1)}
    result=np.where(np.isfinite(directions),0.,np.nan)
    for row,col in np.argwhere(np.isfinite(directions)):
        y,x=int(row),int(col);seen={(y,x)}
        while directions[y,x]!=0:
            dy,dx=steps[directions[y,x]];y,x=y+dy,x+dx
            if not (0<=y<HEIGHT and 0<=x<WIDTH and np.isfinite(directions[y,x])) or (y,x) in seen:raise FixtureError('oracle invalid route')
            seen.add((y,x));result[y,x]+=1
    return result


def point_values(name='points.tif'):
    import numpy as np
    values,alpha,mask=direction_arrays();valid=np.isfinite(effective(values,alpha,mask))
    if name=='dem-points.tif':valid=np.isfinite(dem_values());alpha[:]=255;mask[:]=255
    points=np.where(valid,0.,np.nan)
    positions=((510,20,2**53-1),(511,250,91),(511,510,7)) if name=='dem-points.tif' else ((509,20,2**53-1),(100,100,91),(511,52,88),(511,510,7))
    if name=='nested-points.tif':positions=((510,20,91),(512,20,7))
    if name=='collision-points.tif':positions=((510,511,91),(511,510,7))
    for row,col,label in positions:points[row,col]=label
    if name=='duplicate-points.tif':points[511,52]=91
    if name=='empty-points.tif':points[valid]=0
    if name=='missing-points.tif':points[509,100]=np.inf
    if name!='dem-points.tif':points[512,300]=points[513,400]=points[514,450]=-3
    return points,alpha,mask


def seed(root):
    import numpy as np
    from osgeo import gdal
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite existing fixture data')
    for name in SOURCES:
        values,alpha,mask=point_values(name) if 'points' in name else direction_arrays()
        if name in ('dem.tif','bowl.tif'):
            values=dem_values(name=='bowl.tif');alpha[:]=255;mask[:]=255
        elif 'counts' in name:values=count_oracle(effective(*direction_arrays()))
        if name in ('invalid-counts.tif','invalid-points.tif','id-counts.tif','id-points.tif'):
            ds=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,2,gdal.GDT_UInt64,['TILED=YES','COMPRESS=DEFLATE'])
            ds.SetProjection(crs().ExportToWkt());ds.SetGeoTransform(TRANSFORM)
            for index in (1,2):
                data=np.full((HEIGHT,WIDTH),2**53,dtype=np.uint64)
                if index==2:data[509,100]=2**53+1 if name.startswith('invalid') else 42 if 'points' in name else 0
                band=ds.GetRasterBand(index);band.WriteArray(data);band.SetNoDataValueAsUInt64(2**53);band=None
            ds=None;continue
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,4,gdal.GDT_Float64,['TILED=YES','COMPRESS=DEFLATE'])
        ds.SetProjection(crs(name).ExportToWkt());ds.SetGeoTransform((500001.,2.,0.,3000000.,0.,-3.) if name=='shifted-points.tif' else (500000.,2.,.1,3000000.,0.,-3.) if name=='rotated-counts.tif' else TRANSFORM)
        for index,data in enumerate((np.full(values.shape,.25),np.where(np.isnan(values),-9999,values),np.full(values.shape,-9999.),alpha),1):
            band=ds.GetRasterBand(index);band.WriteArray(data);band.SetNoDataValue(-9999);band=None
        ds.GetRasterBand(2).SetUnitType('metre' if name in ('dem.tif','bowl.tif') else 'count' if 'counts' in name else 'classification')
        ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        if ds.GetFileList()!=[str(root/name)]:raise FixtureError('source must be self-contained')
        ds=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}


@lru_cache(maxsize=8)
def expected_directions(name):
    if name not in CASES:raise FixtureError('unknown exact snap target')
    if CASES[name][4] not in ('direction','fill'):return effective(*direction_arrays())
    # Frozen strip DEM: unfilled descent is vertical; filled interior levels are
    # one metre and drain along shortest vertical paths to the lower open edge.
    import math
    import numpy as np
    values=dem_values();result=np.full(values.shape,np.nan);result[FIRST_ROW:,:]=4.;result[-1,:]=0
    if CASES[name][4]=='fill':
        values[FIRST_ROW+1:-1,1:-1]=1.
        neighbours=((-1,-1,32),(-1,0,64),(-1,1,128),(0,-1,16),(0,1,1),(1,-1,8),(1,0,4),(1,1,2))
        for row,col in np.argwhere(np.isfinite(values)):
            if row not in (FIRST_ROW,HEIGHT-1) and col not in (0,WIDTH-1):continue
            candidates=[]
            for dy,dx,code in neighbours:
                y,x=row+dy,col+dx
                if 0<=y<HEIGHT and 0<=x<WIDTH and values[y,x]<values[row,col]:
                    candidates.append((-(values[row,col]-values[y,x])/math.hypot(dx*2,dy*3),y,x,code))
            result[row,col]=min(candidates)[-1] if candidates else 0
    result.flags.writeable=False;return result

@lru_cache(maxsize=8)
def expected_points(name):
    result=effective(*point_values(CASES[name][1]))
    if CASES[name][4]=='reclass':
        original=result.copy()
        for value,label in ((91,901),(7,70),(88,880)):result[original==value]=label
    result.flags.writeable=False;return result


def snap_oracle(counts,points,xstep,ystep,radius):
    import math
    import numpy as np
    valid=np.isfinite(counts);result=np.where(valid,0.,np.nan);cells=list(map(tuple,np.argwhere(valid)))
    for row,col in np.argwhere(points>0):
        choices=[]
        for y,x in cells:
            distance=math.hypot(int(x-col)*xstep,int(y-row)*ystep)
            if distance<=radius:choices.append((-counts[y,x],distance,int(y),int(x)))
        _,_,y,x=min(choices)
        if result[y,x]!=0:raise FixtureError('oracle snap collision')
        result[y,x]=points[row,col]
    return result


@lru_cache(maxsize=8)
def expected_snapped(name):
    unit=crs(name).GetLinearUnits()
    result=snap_oracle(count_oracle(expected_directions(name)),expected_points(name),2*unit,3*unit,CASES[name][5]);result.flags.writeable=False;return result


def watershed_oracle(directions,points):
    import numpy as np
    steps={1:(0,1),2:(1,1),4:(1,0),8:(1,-1),16:(0,-1),32:(-1,-1),64:(-1,0),128:(-1,1)}
    valid=np.isfinite(directions);result=np.where(valid,0.,np.nan)
    if not np.array_equal(valid,np.isfinite(points)):raise FixtureError('oracle point coverage differs')
    for row,col in np.argwhere(valid):
        y,x=int(row),int(col);seen=set();labels=[]
        while True:
            if (y,x) in seen or not (0<=y<HEIGHT and 0<=x<WIDTH and valid[y,x]):raise FixtureError('oracle invalid watershed route')
            seen.add((y,x))
            if points[y,x]>0:labels.append(points[y,x])
            if directions[y,x]==0:break
            dy,dx=steps[directions[y,x]];y,x=y+dy,x+dx
        if len(labels)>1:raise FixtureError('oracle nested points')
        if labels:result[row,col]=labels[0]
    result.flags.writeable=False;return result


@lru_cache(maxsize=8)
def expected_pixels(name):
    return watershed_oracle(expected_directions(name),expected_snapped(name)) if CASES[name][4]=='watershed' else expected_snapped(name)

def snap_evidence(actual,name):
    import numpy as np
    if not np.array_equal(actual,expected_pixels(name),equal_nan=True):raise FixtureError('independent complete snap IDs differ')
    ids=sorted(map(int,expected_points(name)[expected_points(name)>0]));observed=sorted(map(int,np.unique(actual[np.isfinite(actual)&(actual>0)])))
    if ids!=observed:raise FixtureError('user IDs were changed or dropped')
    return {'snap_verified':True,'point_ids_verified':True,'unselected_zero_verified':True,'radius_and_tie_verified':True,
        'point_ids':ids,'zero_pixels':int(np.count_nonzero(actual==0)),
        'snapped_positions':[[int(y),int(x),int(expected_snapped(name)[y,x])] for y,x in np.argwhere(expected_snapped(name)>0)],
        'watershed_verified':CASES[name][4]=='watershed'}


def artifact_expectation(name):
    if name not in CASES:raise FixtureError('unknown exact snap target')
    corners=[point(x,y) for x,y in ((0,0),(WIDTH,0),(0,HEIGHT),(WIDTH,HEIGHT))]
    # Resolve the authority without loading a GIS binding in the runner's host process.
    epsg=2222 if name.startswith('foot') else 2277 if name.startswith('survey') else 3857
    return {'width':WIDTH,'height':HEIGHT,'band_count':1,'source_crs':'EPSG:'+str(epsg),'extent_srid':epsg,'transform':list(TRANSFORM),'band_nodata':None,
        'extent':[min(p[0] for p in corners),min(p[1] for p in corners),max(p[0] for p in corners),max(p[1] for p in corners)]}


def verify(root,names):
    import numpy as np
    from osgeo import gdal,osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(name not in TARGETS for name in names):raise FixtureError('invalid exact pour-point snapping target inventory')
    if sorted(p.name for p in root.iterdir())!=sorted([*SOURCES,'outputs','.addp-online-owner']):raise FixtureError('unexpected source sidecar or temporary file')
    if sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):raise FixtureError('unexpected output, sidecar or temporary file')
    result={'sources':{name:digest(root/name) for name in SOURCES},'targets':{}}
    for name in names:
        path=root/'outputs'/name;fact=digest(path);dataset=gdal.Open(str(path))
        if dataset is None or dataset.GetDriver().ShortName!='GTiff' or dataset.RasterCount!=1:raise FixtureError('expected one-band GeoTIFF')
        if (dataset.RasterXSize,dataset.RasterYSize,dataset.GetGeoTransform())!=(WIDTH,HEIGHT,TRANSFORM):raise FixtureError('source grid changed')
        actual_crs=osr.SpatialReference(wkt=dataset.GetProjection())
        if not actual_crs.IsSame(crs(name)):raise FixtureError('source CRS changed')
        band=dataset.GetRasterBand(1);actual=band.ReadAsArray();expected=expected_pixels(name)
        if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue()):raise FixtureError('Float64 or NaN NoData changed')
        if not np.array_equal(actual,expected,equal_nan=True):raise FixtureError('complete pixel values differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray(),np.where(np.isfinite(expected),255,0)):raise FixtureError('complete pixel mask differs')
        if validate(dataset,full_check=True)[1]:raise FixtureError('COG full layout validation failed')
        if band.GetOverviewCount()<1:raise FixtureError('expected COG overviews for cross-block image')
        if band.GetColorTable() is not None or band.GetUnitType()!='' or band.GetScale() is not None or band.GetOffset() is not None:raise FixtureError('unit or output scale/offset/color differs')
        for index in range(band.GetOverviewCount()):
            overview=band.GetOverview(index).ReadAsArray();valid=np.isfinite(overview)
            if not set(np.unique(overview[valid]))<=set(np.unique(expected[np.isfinite(expected)])):raise FixtureError('overview invented a point ID')
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':True,'full_pixel_and_mask_verification':True,
            'valid_pixels':int(np.isfinite(expected).sum()),'unit_cleared_verified':True,'unit':'',**snap_evidence(actual,name)}
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
        return NFSExport('snap-pour-points',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'snap-pour-points-source.json').write_text(json.dumps(result));return result
    if action=='verify':
        result=runtime_worker('verify',root,sys.argv[2:])
        if result['sources']!=json.loads((secret/'snap-pour-points-source.json').read_text())['sources']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1])
        print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster pour-point snapping fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
