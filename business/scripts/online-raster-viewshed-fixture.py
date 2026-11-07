#!/usr/bin/env python3
"""Owned NFS, independent exact cell-clipping oracle and immutable COG archive."""
from __future__ import annotations
from functools import lru_cache
import hashlib,io,json,math,os,re,subprocess,sys,tempfile,time
from contextlib import contextmanager
from pathlib import Path

SUITE='raster-viewshed-persistence'
RUNTIME='addp-raster-viewshed-runtime'
WIDTH,HEIGHT=519,515
FACTORS={'metre':1.,'foot':.3048,'us_survey_foot':1200/3937}
SOURCE_UNITS={'feet.tif':(3857,1.,'foot'),'survey-feet.tif':(2222,.3048,'us_survey_foot'),'us-grid.tif':(2277,1200/3937,'metre')}
CASES={
 'flat.cog.tif':('flat.tif','direct'), 'ground.cog.tif':('ground.tif','direct'),
 'ridge.cog.tif':('ridge.tif','direct'), 'unknown.cog.tif':('unknown.tif','direct'),
 'priority.cog.tif':('priority.tif','direct'), 'coverage.cog.tif':('coverage.tif','direct'),
 'radius-equal.cog.tif':('radius.tif','direct'), 'radius-below.cog.tif':('radius.tif','direct'),
 'radius-above.cog.tif':('radius.tif','direct'), 'subpixel.cog.tif':('flat.tif','direct'),
 'empty.cog.tif':('flat.tif','direct'), 'feet.cog.tif':('feet.tif','direct'),
 'survey-feet.cog.tif':('survey-feet.tif','direct'), 'us-grid.cog.tif':('us-grid.tif','direct'),
 'filled.cog.tif':('bowl.tif','fill'), 'cross-block.cog.tif':('cross-block.tif','direct')}
TARGETS=tuple(CASES)
SOURCES=tuple(dict.fromkeys([c[0] for c in CASES.values()]+['rotated.tif','south-up.tif','geographic.tif','no-crs.tif','no-grid.tif','scaled.tif','offset.tif']))
NODATA=-9999.
class FixtureError(RuntimeError):pass

def command(args,**kwargs):
    try:return subprocess.run(args,check=True,capture_output=True,text=True,timeout=150,**kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:
        detail=(error.stderr or '').strip()[-4000:] or 'no stderr diagnostic'
        raise FixtureError(f'fixture command failed (exit {error.returncode}): {detail}') from error

if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker

def guard(env=os.environ):return NFSExport('viewshed',command).guard(env)
def owned_root(secret):return NFSExport('viewshed',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}
def grid_for(source):
    horizontal=SOURCE_UNITS.get(source,(3857,1.,'metre'))[1]
    dx,dy=(2.,3.) if source=='cross-block.tif' else (1.,1.)
    return (0.,dx/horizontal,0.,HEIGHT*dy/horizontal,0.,-dy/horizontal)
def nodata_for(source):return NODATA

def source_arrays(name):
    import numpy as np
    values=np.zeros((HEIGHT,WIDTH));alpha=np.full(values.shape,255.);mask=np.full(values.shape,255,np.uint8)
    if name=='ridge.tif':values[254:260,258]=3.
    if name=='unknown.tif':values[256,257]=NODATA
    if name=='priority.tif':values[256,257]=NODATA;values[256,258]=3.;values[257,256]=3.;values[258,256]=NODATA
    if name=='coverage.tif':
        values[256,257]=NODATA;alpha[257,256]=0.;alpha[255,256]=128.;mask[256,255]=0;values[255,257]=3.
    if name=='bowl.tif':values[:]=10.;values[1:-1,1:-1]=2.
    if name=='cross-block.tif':values[508:515,513]=3.
    return values,alpha,mask

def parameters(name):
    if name not in CASES:raise FixtureError('unknown exact viewshed target')
    source,stage=CASES[name];grid=grid_for(source);_,_,unit=SOURCE_UNITS.get(source,(3857,1.,'metre'))
    col,row=(511.5,511.5)if source=='cross-block.tif'else(256.5,256.5)
    if name in ('subpixel.cog.tif','empty.cog.tif'):col,row=256.25,256.75
    radius={'radius-equal.cog.tif':1.,'radius-below.cog.tif':math.nextafter(1.,0.),
            'radius-above.cog.tif':math.nextafter(1.,math.inf),'empty.cog.tif':.01}.get(name,6.)
    return {'band':2,'elevation_unit':unit,'observer_xy':[grid[0]+col*grid[1],grid[3]+row*grid[5]],
            'observer_height_m':0. if name=='ground.cog.tif'else 1.,
            'target_height_m':0. if name=='ground.cog.tif'else 1.,'radius_metres':radius}

def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/n).exists()for n in SOURCES)or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite fixture data')
    for name in SOURCES:
        values,alpha,mask=source_arrays(name)
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,4,gdal.GDT_Float64,['TILED=YES','COMPRESS=DEFLATE'])
        epsg,_,unit=SOURCE_UNITS.get(name,(3857,1.,'metre'));crs=osr.SpatialReference();crs.ImportFromEPSG(4326 if name=='geographic.tif'else epsg)
        if name!='no-crs.tif':ds.SetSpatialRef(crs)
        grid=list(grid_for(name))
        if name=='rotated.tif':grid[2]=.25
        if name=='south-up.tif':grid[5]=abs(grid[5])
        if name!='no-grid.tif':ds.SetGeoTransform(grid)
        for i,data in enumerate((np.full(values.shape,17.),values,np.full(values.shape,1.),alpha),1):
            band=ds.GetRasterBand(i);band.WriteArray(data);band.SetNoDataValue(NODATA);band=None
        ds.GetRasterBand(2).SetUnitType(unit);ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        if name=='scaled.tif':ds.GetRasterBand(2).SetScale(2.)
        if name=='offset.tif':ds.GetRasterBand(2).SetOffset(1.)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            if ds.CreateMaskBand(gdal.GMF_PER_DATASET)!=gdal.CE_None:raise FixtureError('cannot create internal source mask')
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
        ds=gdal.Open(str(root/name));band=ds.GetRasterBand(2)
        if ds.GetFileList()!=[str(root/name)]or not np.array_equal(band.ReadAsArray(),values)or not np.array_equal(band.GetMaskBand().ReadAsArray(),mask):raise FixtureError('stored DEM values or mask differ')
        if ds.GetRasterBand(4).GetColorInterpretation()!=gdal.GCI_AlphaBand or band.GetUnitType()!=unit:raise FixtureError('stored band roles or units differ')
        band=ds=None
    return {'sources':{name:digest(root/name)for name in SOURCES}}

def expected_pixels(name):
    """Independent closed-rectangle clipping, not the product crossing iterator."""
    import numpy as np
    from fractions import Fraction as F
    if name not in CASES:raise FixtureError('unknown exact viewshed target')
    source,stage=CASES[name];values,alpha,mask=source_arrays(source);valid=np.isfinite(values)&(values!=NODATA)&np.isfinite(alpha)&(alpha>0)&(mask>0)
    if stage=='fill':values=np.full(values.shape,10.)
    params=parameters(name);x0,dx,_,y0,_,dy=map(F,grid_for(source));ox,oy=map(F,params['observer_xy']);ox,oy=(ox-x0)/dx,(oy-y0)/dy
    horizontal=F(SOURCE_UNITS.get(source,(3857,1.,'metre'))[1]);vertical=F(FACTORS[params['elevation_unit']]);radius=F(params['radius_metres'])
    start=F(values[math.floor(oy),math.floor(ox)].item())*vertical+F(params['observer_height_m']);answer=np.full(values.shape,255,np.uint8)
    columns=range(max(0,math.ceil(ox-radius/abs(dx*horizontal)-F(1,2))),min(WIDTH,math.floor(ox+radius/abs(dx*horizontal)-F(1,2))+1))
    rows=range(max(0,math.ceil(oy-radius/abs(dy*horizontal)-F(1,2))),min(HEIGHT,math.floor(oy+radius/abs(dy*horizontal)-F(1,2))+1))
    for row in rows:
        for col in columns:
            tx,ty=F(2*col+1,2),F(2*row+1,2)
            if ((tx-ox)*dx*horizontal)**2+((ty-oy)*dy*horizontal)**2>radius**2 or not valid[row,col]:continue
            if (tx,ty)==(ox,oy):answer[row,col]=1;continue
            end=F(values[row,col].item())*vertical+F(params['target_height_m']);blocked=missing=False
            for y in range(max(0,math.floor(min(oy,ty))-1),min(HEIGHT,math.floor(max(oy,ty))+1)):
                for x in range(max(0,math.floor(min(ox,tx))-1),min(WIDTH,math.floor(max(ox,tx))+1)):
                    enter,leave=F(0),F(1)
                    for position,motion,low,high in ((ox,tx-ox,x,x+1),(oy,ty-oy,y,y+1)):
                        if motion==0:
                            if not low<=position<=high:break
                        else:
                            first,last=sorted(((low-position)/motion,(high-position)/motion));enter,leave=max(enter,first),min(leave,last)
                    else:
                        if enter>=leave:continue
                        if not valid[y,x]:missing=True;continue
                        ground=F(values[y,x].item())*vertical
                        left,right=start+(end-start)*enter,start+(end-start)*leave
                        if ground>min(left,right)or(ground==min(left,right)and(end==start or(0<enter<1 if end>=start else 0<leave<1))):blocked=True
            answer[row,col]=0 if blocked else 2 if missing else 1
    answer.flags.writeable=False
    return answer

CACHE_SCHEMA='addp.viewshed-oracle/v1'
CACHE_MARKER='.oracle-owner'
CONTROL_NAME='viewshed-oracle-control.json'

@contextmanager
def elapsed(timings,key):
    start=time.monotonic()
    try:yield
    finally:timings[key]=timings.get(key,0)+round((time.monotonic()-start)*1000)

def encoded(value):return json.dumps(value,sort_keys=True,separators=(',',':')).encode()
def scope_hash(scope):return hashlib.sha256(encoded(scope)).hexdigest()
def oracle_path(scope):return Path(tempfile.gettempdir())/('addp-viewshed-oracle-'+scope_hash(scope))

def validate_scope(root,scope,*,sources):
    if (not isinstance(scope,dict) or set(scope)!={'schema','run_id','fixture_sha256','source_root','sources'}
        or scope['schema']!=CACHE_SCHEMA or not re.fullmatch('viewshed-[0-9]+-[0-9]+',scope.get('run_id',''))
        or not re.fullmatch('[0-9a-f]{64}',scope.get('fixture_sha256',''))
        or scope['source_root']!=str(root.resolve()) or (root/'.addp-online-owner').is_symlink()
        or (root/'.addp-online-owner').read_text()!=scope['run_id'] or set(scope['sources'])!=set(SOURCES)):
        raise FixtureError('oracle scope differs from current owned run/fixture/source')
    for fact in scope['sources'].values():
        if (set(fact)!={'sha256','size_bytes'} or type(fact['size_bytes'])is not int or fact['size_bytes']<=0
            or not isinstance(fact['sha256'],str) or not re.fullmatch('[0-9a-f]{64}',fact['sha256'])):
            raise FixtureError('invalid frozen oracle source hash')
    if sources and {n:digest(root/n)for n in SOURCES}!=scope['sources']:raise FixtureError('source file changed')

def cache_directory(scope):
    path=oracle_path(scope)
    if (path.is_symlink() or not path.is_dir() or path.stat().st_uid!=os.getuid()
        or path.stat().st_mode&0o777!=0o700):raise FixtureError('oracle cache directory is not private/owned')
    marker=path/CACHE_MARKER
    if marker.is_symlink() or not marker.is_file() or marker.stat().st_uid!=os.getuid()or marker.stat().st_mode&0o777!=0o400 or marker.read_bytes()!=encoded(scope):raise FixtureError('oracle cache owner changed')
    return path

def prepare_oracle(root,scope):
    import numpy as np
    validate_scope(root,scope,sources=True)
    if {p.name for p in root.iterdir()}!={*SOURCES,'outputs','.addp-online-owner'} or list((root/'outputs').iterdir()) or list(Path(tempfile.gettempdir()).glob('addp-raster-*')):raise FixtureError('preflight source/output or Runtime workspace residual')
    directory=oracle_path(scope);directory.mkdir(mode=0o700)
    marker=directory/CACHE_MARKER
    with marker.open('xb') as f:f.write(encoded(scope))
    marker.chmod(0o400);arrays={};timings={}
    for name in TARGETS:
        with elapsed(timings,'expected_build_ms'):expected=expected_pixels(name)
        with elapsed(timings,'cache_write_ms'):
            path=directory/(name+'.npy')
            with path.open('xb') as f:np.save(f,expected,allow_pickle=False)
            path.chmod(0o400);arrays[name]=digest(path)
    return {'scope':scope,'arrays':arrays,'timing_ms':timings,'runtime_workspaces_removed':True,'targets':{}, 'expectations':{name:{'artifact':artifact_expectation(name),'state_counts':{str(code):int((load_expected({'arrays':arrays},directory,name)==code).sum())for code in(0,1,2,255)}}for name in TARGETS}}

def validate_cache(control):
    scope=control['scope'];directory=cache_directory(scope);arrays=control.get('arrays')
    if (not isinstance(arrays,dict) or set(arrays)!=set(TARGETS)
        or {p.name for p in directory.iterdir()}!={CACHE_MARKER,*[n+'.npy'for n in TARGETS]}):
        raise FixtureError('oracle cache inventory changed')
    for fact in arrays.values():
        if (set(fact)!={'sha256','size_bytes'} or type(fact['size_bytes'])is not int
            or not WIDTH*HEIGHT<fact['size_bytes']<WIDTH*HEIGHT+1024
            or not isinstance(fact['sha256'],str) or not re.fullmatch('[0-9a-f]{64}',fact['sha256'])):
            raise FixtureError('oracle cache receipt changed')
    return directory

def load_expected(control,directory,name):
    import numpy as np
    path=directory/(name+'.npy');fact=control['arrays'][name]
    if path.is_symlink() or not path.is_file() or path.stat().st_uid!=os.getuid()or path.stat().st_mode&0o777!=0o400 or path.stat().st_size!=fact['size_bytes']:raise FixtureError('oracle cache file changed')
    with path.open('rb') as f:raw=f.read(fact['size_bytes']+1)
    if len(raw)!=fact['size_bytes'] or hashlib.sha256(raw).hexdigest()!=fact['sha256']:raise FixtureError('oracle cache hash changed')
    try:answer=np.load(io.BytesIO(raw),allow_pickle=False)
    except Exception:raise FixtureError('oracle cache array is invalid')from None
    if answer.shape!=(HEIGHT,WIDTH) or answer.dtype!=np.dtype('uint8'):raise FixtureError('oracle cache shape/type changed')
    answer.flags.writeable=False;return answer

def cleanup_oracle(root,control):
    scope=control['scope'];validate_scope(root,scope,sources=False);directory=oracle_path(scope)
    if not directory.exists() and not directory.is_symlink():return {'oracle_cache_removed':True}
    directory=cache_directory(scope)
    if any(p.is_dir()and not p.is_symlink()for p in directory.iterdir()):raise FixtureError('unexpected oracle cache subdirectory')
    for path in directory.iterdir():path.unlink()
    directory.rmdir()
    if directory.exists():raise FixtureError('oracle cache cleanup residual')
    return {'oracle_cache_removed':True}

def artifact_expectation(name):
    source=CASES[name][0];grid=grid_for(source);a,b,c,d,e,f=grid
    epsg=SOURCE_UNITS.get(source,(3857,1.,'metre'))[0]
    return {'width':WIDTH,'height':HEIGHT,'band_count':1,'source_crs':'EPSG:'+str(epsg),'extent_srid':epsg,'transform':list(grid),'band_nodata':255,
        'extent':[a,d+f*HEIGHT,a+b*WIDTH,d]}

def nearest_overviews(dataset,expected):
    import numpy as np
    band=dataset.GetRasterBand(1);previous=expected;h,w=expected.shape;dimensions=[]
    while max(h,w)>128:w,h=max(1,w//2),max(1,h//2);dimensions.append((w,h))
    if band.GetOverviewCount()!=len(dimensions):raise FixtureError('incorrect exact overview count')
    for i,shape in enumerate(dimensions):
        level=band.GetOverview(i)
        if (level.XSize,level.YSize)!=shape:raise FixtureError('overview dimensions differ')
        ys=np.floor(.5+np.arange(shape[1])*previous.shape[0]/shape[1]).astype(int);xs=np.floor(.5+np.arange(shape[0])*previous.shape[1]/shape[0]).astype(int);wanted=previous[np.ix_(ys,xs)]
        if not np.array_equal(level.ReadAsArray(),wanted,equal_nan=True) or not np.array_equal(level.GetMaskBand().ReadAsArray()!=0,wanted!=255):raise FixtureError('complete nearest overview pixels or mask differ')
        previous=wanted
    level=band=None

def verify(root,names,control):
    import numpy as np
    from osgeo import gdal,osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('invalid exact Viewshed target inventory')
    if sorted(p.name for p in root.iterdir())!=sorted([*SOURCES,'outputs','.addp-online-owner']) or sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):raise FixtureError('unexpected source/output sidecar or temporary file')
    timings={k:0 for k in ('source_hash_ms','cache_load_ms','target_hash_ms','pixels_mask_metadata_ms','cog_layout_ms','overviews_ms')}
    with elapsed(timings,'source_hash_ms'):
        validate_scope(root,control['scope'],sources=True)
        result={'sources':control['scope']['sources'],'targets':{}}
    with elapsed(timings,'cache_load_ms'):directory=validate_cache(control)
    for name in names:
        with elapsed(timings,'target_hash_ms'):path=root/'outputs'/name;fact=digest(path)
        with elapsed(timings,'cache_load_ms'):expected=load_expected(control,directory,name)
        pixel_start=time.monotonic();ds=gdal.Open(str(path));spec=artifact_expectation(name)
        if ds is None or ds.GetDriver().ShortName!='GTiff' or ds.RasterCount!=1:raise FixtureError('expected one-band GeoTIFF')
        if (ds.RasterXSize,ds.RasterYSize,list(ds.GetGeoTransform()))!=(WIDTH,HEIGHT,spec['transform']):raise FixtureError('source grid changed')
        crs=osr.SpatialReference();crs.ImportFromEPSG(spec['extent_srid'])
        if ds.GetSpatialRef() is None or not ds.GetSpatialRef().IsSame(crs):raise FixtureError('source CRS changed')
        band=ds.GetRasterBand(1);actual=band.ReadAsArray()
        if band.DataType!=gdal.GDT_Byte or band.GetNoDataValue()!=255:raise FixtureError('Byte or NoData255 changed')
        if not np.array_equal(actual,expected):raise FixtureError('complete exact viewshed pixels differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,expected!=255):raise FixtureError('complete pixel mask differs')
        if band.GetUnitType()!='' or band.GetScale()is not None or band.GetOffset()is not None or band.GetColorTable()is not None:raise FixtureError('source encoding was inherited')
        timings['pixels_mask_metadata_ms']+=round((time.monotonic()-pixel_start)*1000)
        with elapsed(timings,'cog_layout_ms'):
            if validate(ds,full_check=True)[1]:raise FixtureError('COG full layout validation failed')
        with elapsed(timings,'overviews_ms'):nearest_overviews(ds,expected)
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,'overview_count':band.GetOverviewCount(),
            'valid_pixels':int((expected!=255).sum()),'state_counts':{str(code):int((expected==code).sum())for code in(0,1,2,255)},
            'full_pixel_and_mask_verification':True,'independent_cell_clipping_verified':True,'nearest_overviews_verified':True}
        band=ds=None
    result['verification_timing_ms']=timings
    result['oracle_cache']={'scope_sha256':scope_hash(control['scope']),'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}
    return result

def archive_cogs(root,artifacts,verified):
    """Host-side byte copy after Runtime verification; no image re-encoding."""
    names=set(verified['targets']);directory=artifacts/'viewshed-cogs';manifest_path=artifacts/'viewshed-cogs-manifest.json'
    if directory.is_symlink() or manifest_path.is_symlink():raise FixtureError('COG archive must not be symlink')
    directory.mkdir(parents=True,exist_ok=True)
    if any(p.name not in names or p.is_symlink() or not p.is_file() for p in directory.iterdir()):raise FixtureError('unexpected COG archive inventory')
    existing={p.name for p in directory.iterdir()};previous={}
    if manifest_path.exists():
        previous=json.loads(manifest_path.read_text())
        if not isinstance(previous,dict) or set(previous)!=existing:raise FixtureError('previous COG manifest inventory differs')
    elif existing:raise FixtureError('archived COG has no original manifest')
    manifest={}
    for name,fact in verified['targets'].items():
        if name not in TARGETS or digest(root/'outputs'/name)!={k:fact[k] for k in ('sha256','size_bytes')}:raise FixtureError('verified original COG changed before archive')
        path=directory/name
        if path.exists():
            if digest(path)!={k:fact[k] for k in ('sha256','size_bytes')}:raise FixtureError('previous archived COG changed')
        else:
            with (root/'outputs'/name).open('rb') as src,path.open('xb') as dest:
                while data:=src.read(1024*1024):dest.write(data)
        manifest[name]={'path':'viewshed-cogs/'+name,**digest(path)}
        if manifest[name]['sha256']!=fact['sha256'] or manifest[name]['size_bytes']!=fact['size_bytes']:raise FixtureError('archived COG byte identity differs')
    if any(k not in manifest or value!=manifest[k] for k,value in previous.items()):raise FixtureError('previous COG manifest changed')
    temporary=manifest_path.with_suffix('.tmp')
    if temporary.is_symlink():raise FixtureError('COG manifest temporary path must not be symlink')
    temporary.write_text(json.dumps(manifest,sort_keys=True,indent=2)+'\n');temporary.replace(manifest_path)
    return manifest

def worker(action,root,args):
    if action=='seed':return seed(root)
    if action not in {'verify','prepare-oracle','cleanup-oracle'} or not args:raise FixtureError('unknown worker action or missing oracle context')
    control=json.loads(args[0]);start=time.monotonic()
    if action=='prepare-oracle':result=prepare_oracle(root,control)
    elif action=='cleanup-oracle':result=cleanup_oracle(root,control)
    else:
        if list(Path(tempfile.gettempdir()).glob('addp-raster-*')):raise FixtureError('Runtime raster workspace residuals')
        result={**verify(root,args[1:],control),'runtime_workspaces_removed':True}
    result['worker_total_ms']=round((time.monotonic()-start)*1000);return result

def runtime_worker(action,root,names=()):return run_runtime_worker(command,RUNTIME,SUITE,Path(__file__),action,root,names)

def private_control(secret):
    path=secret/CONTROL_NAME
    if path.is_symlink() or not path.is_file() or path.stat().st_uid!=os.getuid()or path.stat().st_mode&0o777!=0o600:
        raise FixtureError('oracle anchor is not private/owned')
    value=json.loads(path.read_text())
    if value['scope']['fixture_sha256']!=digest(Path(__file__))['sha256']or value['scope']['run_id']!=os.environ['ADDP_ONLINE_TEST_RUN_ID']:
        raise FixtureError('oracle anchor fixture/run changed')
    expected=os.environ.get('ADDP_ONLINE_VIEWSHED_ORACLE_ANCHOR_SHA256')
    if expected is not None and digest(path)['sha256']!=expected:raise FixtureError('oracle anchor changed')
    return path,value

def main(action):
    secret=guard()
    if action in {'start','stop'}:return NFSExport('viewshed',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'viewshed-source.json').write_text(json.dumps(result));return result
    if action=='prepare-oracle':
        scope={'schema':CACHE_SCHEMA,'run_id':os.environ['ADDP_ONLINE_TEST_RUN_ID'],'source_root':str(root.resolve()),
               'fixture_sha256':digest(Path(__file__))['sha256'],'sources':json.loads((secret/'viewshed-source.json').read_text())['sources']}
        path=secret/CONTROL_NAME
        fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb')as f:f.write(encoded({'scope':scope,'arrays':None}))
        result=runtime_worker(action,root,[json.dumps(scope)])
        if result.get('scope')!=scope or set(result.get('arrays',{}))!=set(TARGETS):raise FixtureError('oracle prepare response differs')
        temp=path.with_suffix('.tmp')
        with temp.open('xb')as f:f.write(encoded({'scope':scope,'arrays':result['arrays']}))
        temp.chmod(0o600);temp.replace(path)
        return {'scope_sha256':scope_hash(scope),'anchor_sha256':digest(path)['sha256'],'expected_array_count':len(TARGETS),
                'cache_bytes':sum(v['size_bytes']for v in result['arrays'].values()),'timing_ms':result['timing_ms'], 'sources':scope['sources'],'expectations':result['expectations'],'runtime_workspaces_removed':result['runtime_workspaces_removed'],'targets':result['targets'],'archived_cogs':{}}
    if action=='cleanup-oracle':
        if not(secret/CONTROL_NAME).exists():return {'oracle_cache_removed':True,'not_started':True}
        path,control=private_control(secret);result=runtime_worker(action,root,[json.dumps(control)])
        if result.get('oracle_cache_removed')is not True:raise FixtureError('oracle cleanup proof missing')
        path.unlink();return {'oracle_cache_removed':True}
    if action=='verify':
        if not os.environ.get('ADDP_ONLINE_VIEWSHED_ORACLE_ANCHOR_SHA256'):raise FixtureError('oracle anchor is required')
        _,control=private_control(secret);timings={}
        with elapsed(timings,'worker_transport_ms'):result=runtime_worker('verify',root,[json.dumps(control),*sys.argv[2:]])
        with elapsed(timings,'source_receipt_ms'):
            if result['sources']!=json.loads((secret/'viewshed-source.json').read_text())['sources']:raise FixtureError('source file changed')
        with elapsed(timings,'archive_ms'):result['archived_cogs']=archive_cogs(root,Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']),result)
        result['host_timing_ms']=timings;return result
    raise FixtureError('unknown fixture action')
if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1]);print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster Viewshed fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
