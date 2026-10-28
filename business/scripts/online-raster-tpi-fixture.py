#!/usr/bin/env python3
"""Owned NFS, independent Decimal TPI oracle and immutable original COG archive."""
from __future__ import annotations
from functools import lru_cache
import hashlib,io,json,math,os,re,subprocess,sys,tempfile,time
from contextlib import contextmanager
from pathlib import Path

SUITE='raster-tpi-persistence'
RUNTIME='addp-raster-tpi-runtime'
WIDTH,HEIGHT=519,515
FACTORS={'metre':1.,'foot':.3048,'us_survey_foot':1200/3937}
SOURCE_UNITS={'feet.tif':(3857,1.,'foot'),'survey-feet.tif':(2222,.3048,'us_survey_foot'),'us-grid.tif':(2277,1200/3937,'metre')}
CASES={'tpi.cog.tif':('dem.tif',2,'metre','direct'),
       'flat.cog.tif':('flat.tif',2,'metre','direct'),
       'plane.cog.tif':('plane.tif',2,'metre','direct'),
       'saddle.cog.tif':('saddle.tif',2,'metre','direct'),
       'feet.cog.tif':('feet.tif',2,'foot','direct'),
       'survey-feet.cog.tif':('survey-feet.tif',2,'us_survey_foot','direct'),
       'us-grid.cog.tif':('us-grid.tif',2,'metre','direct'),
       'filled.cog.tif':('bowl.tif',2,'metre','fill'),
       'reclassified.cog.tif':('dem.tif',2,'metre','reclassify'),
       'int64.cog.tif':('int64.tif',2,'metre','direct'),
       'uint64.cog.tif':('uint64.tif',2,'metre','direct'),
       'invalid.cog.tif':('invalid.tif',2,'metre','direct'),
       'sparse.cog.tif':('sparse.tif',2,'metre','direct')}
TARGETS=tuple(CASES)
SOURCES=tuple(dict.fromkeys([c[0] for c in CASES.values()]+['rotated.tif','south-up.tif','geographic.tif','no-crs.tif','no-grid.tif','scaled.tif','offset.tif','complex.tif','unknown-unit.tif']))
class FixtureError(RuntimeError):pass

def command(args,**kwargs):
    try:return subprocess.run(args,check=True,capture_output=True,text=True,timeout=150,**kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:
        detail=(error.stderr or '').strip()[-4000:] or 'no stderr diagnostic'
        raise FixtureError(f'fixture command failed (exit {error.returncode}): {detail}') from error

if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker

def guard(env=os.environ):return NFSExport('tpi',command).guard(env)
def owned_root(secret):return NFSExport('tpi',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}
def grid_for(source):
    horizontal=SOURCE_UNITS.get(source,(3857,1.,'metre'))[1]
    return (500000.,2./horizontal,0.,3000000.,0.,-3./horizontal)
def nodata_for(source):return -2**63 if source=='int64.tif' else 2**64-1 if source=='uint64.tif' else -2.**40

def source_arrays(name):
    import numpy as np
    rows,cols=np.indices((HEIGHT,WIDTH));r=rows%35-17;c=cols%35-17
    values=r*r-2*c*c+(rows%7-3)*(cols%5-2)-100
    integer=name in ('int64.tif','uint64.tif')
    if integer:values=(values.astype(object)+(2**60 if name=='int64.tif' else 2**63)).astype(np.int64 if name=='int64.tif' else np.uint64)
    else:values=values.astype(float)/FACTORS[SOURCE_UNITS.get(name,(3857,1.,'metre'))[2]]
    alpha=np.full(values.shape,255.);mask=np.full(values.shape,255,np.uint8)
    if name=='flat.tif':values[:]=0.
    elif name=='plane.tif':values=2.*cols-3.*rows-100
    elif name=='saddle.tif':values=(cols-259.)**2-(rows-257.)**2
    elif name=='bowl.tif':values[:]=10.;values[1:-1,1:-1]=2.
    elif name=='invalid.tif':values[:]=nodata_for(name)
    elif name=='sparse.tif':values[:]=np.nan;values[::5,::5]=0.
    else:
        values[510,511]=nodata_for(name)
        values[508,512]=nodata_for(name) if integer else np.inf
        values[512,509]=nodata_for(name) if integer else np.nan
        alpha[509,510]=0.;alpha[511,513]=0. if integer else np.nan;alpha[500,500]=128.;mask[513,512]=0
    return values,alpha,mask

def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/n).exists() for n in SOURCES) or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite fixture data')
    for name in SOURCES:
        values,alpha,mask=source_arrays(name);integer=name in ('int64.tif','uint64.tif');dtype=np.uint64 if name=='uint64.tif' else np.int64 if integer else np.complex128 if name=='complex.tif' else np.float64
        native=gdal.GDT_UInt64 if name=='uint64.tif' else gdal.GDT_Int64 if integer else gdal.GDT_CFloat64 if name=='complex.tif' else gdal.GDT_Float64
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,4,native,['TILED=YES','COMPRESS=DEFLATE'])
        epsg,_,unit=SOURCE_UNITS.get(name,(3857,1.,'metre'));crs=osr.SpatialReference();crs.ImportFromEPSG(4326 if name=='geographic.tif' else epsg)
        if name!='no-crs.tif':ds.SetSpatialRef(crs)
        grid=list(grid_for(name))
        if name=='rotated.tif':grid[2]=.25
        if name=='south-up.tif':grid[5]=3.
        if name!='no-grid.tif':ds.SetGeoTransform(grid)
        for i,data in enumerate((np.full(values.shape,1),values,np.full(values.shape,17),alpha),1):
            band=ds.GetRasterBand(i);band.WriteArray(np.asarray(data,dtype=dtype));band.SetNoDataValue(nodata_for(name));band=None
        ds.GetRasterBand(2).SetUnitType('fathom' if name=='unknown-unit.tif' else unit);ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        if name=='scaled.tif':ds.GetRasterBand(2).SetScale(2.)
        if name=='offset.tif':ds.GetRasterBand(2).SetOffset(1.)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            if ds.CreateMaskBand(gdal.GMF_PER_DATASET)!=gdal.CE_None:raise FixtureError('cannot create internal source mask')
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
        ds=gdal.Open(str(root/name))
        if ds.GetFileList()!=[str(root/name)] or not np.array_equal(ds.GetRasterBand(2).ReadAsArray(),np.asarray(values,dtype=dtype),equal_nan=True):raise FixtureError('native source bytes or values changed')
        if not np.array_equal(ds.GetRasterBand(2).GetMaskBand().ReadAsArray(),mask):raise FixtureError('source mask changed')
        ds=None
    return {'sources':{name:digest(root/name) for name in SOURCES}}

@lru_cache(maxsize=13)
def expected_dem(name):
    import numpy as np
    if name not in CASES:raise FixtureError('unknown exact TPI target')
    source,_,_,stage=CASES[name];values,alpha,mask=source_arrays(source)
    valid=np.isfinite(values)&(values!=nodata_for(source))&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    if stage=='fill':values=np.full(values.shape,10.)
    if stage=='reclassify':values=np.where(values<0,0.,1.)
    values.flags.writeable=valid.flags.writeable=False
    return values,valid

@lru_cache(maxsize=13)
def expected_pixels(name):
    """Group identical stored-value neighbourhoods; evaluate an independent Decimal mean."""
    import numpy as np
    from decimal import Decimal,localcontext
    values,valid=expected_dem(name);answer=np.full(values.shape,np.nan)
    cells=np.lib.stride_tricks.sliding_window_view(values,(3,3));complete=np.lib.stride_tricks.sliding_window_view(valid,(3,3)).all(axis=(-2,-1))
    if complete.any():
        unique,inverse=np.unique(cells[complete].reshape(-1,9),axis=0,return_inverse=True);reduced=[]
        with localcontext() as context:
            context.prec=2500;factor=Decimal.from_float(FACTORS[CASES[name][2]])
            for row in unique.tolist():
                samples=[Decimal(v) if isinstance(v,int) else Decimal.from_float(v) for v in row];centre=samples.pop(4)
                reduced.append(float((centre-sum(samples)/Decimal(8))*factor))
        answer[1:-1,1:-1][complete]=np.asarray(reduced)[inverse]
    answer.flags.writeable=False;return answer

CACHE_SCHEMA='addp.tpi-oracle/v1'
CACHE_MARKER='.oracle-owner'
CONTROL_NAME='tpi-oracle-control.json'

@contextmanager
def elapsed(timings,key):
    start=time.monotonic()
    try:yield
    finally:timings[key]=timings.get(key,0)+round((time.monotonic()-start)*1000)

def encoded(value):return json.dumps(value,sort_keys=True,separators=(',',':')).encode()
def scope_hash(scope):return hashlib.sha256(encoded(scope)).hexdigest()
def oracle_path(scope):return Path(tempfile.gettempdir())/('addp-tpi-oracle-'+scope_hash(scope))

def validate_scope(root,scope,*,sources):
    if (not isinstance(scope,dict) or set(scope)!={'schema','run_id','fixture_sha256','source_root','sources'}
        or scope['schema']!=CACHE_SCHEMA or not re.fullmatch('tpi-[0-9]+-[0-9]+',scope.get('run_id',''))
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
    validate_scope(root,scope,sources=True);directory=oracle_path(scope);directory.mkdir(mode=0o700)
    marker=directory/CACHE_MARKER
    with marker.open('xb') as f:f.write(encoded(scope))
    marker.chmod(0o400);arrays={};timings={}
    for name in TARGETS:
        with elapsed(timings,'expected_build_ms'):expected=expected_pixels(name)
        with elapsed(timings,'cache_write_ms'):
            path=directory/(name+'.npy')
            with path.open('xb') as f:np.save(f,expected,allow_pickle=False)
            path.chmod(0o400);arrays[name]=digest(path)
    return {'scope':scope,'arrays':arrays,'timing_ms':timings}

def validate_cache(control):
    scope=control['scope'];directory=cache_directory(scope);arrays=control.get('arrays')
    if (not isinstance(arrays,dict) or set(arrays)!=set(TARGETS)
        or {p.name for p in directory.iterdir()}!={CACHE_MARKER,*[n+'.npy'for n in TARGETS]}):
        raise FixtureError('oracle cache inventory changed')
    for fact in arrays.values():
        if (set(fact)!={'sha256','size_bytes'} or type(fact['size_bytes'])is not int
            or not WIDTH*HEIGHT*8<fact['size_bytes']<WIDTH*HEIGHT*8+1024
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
    if answer.shape!=(HEIGHT,WIDTH) or answer.dtype!=np.dtype('float64'):raise FixtureError('oracle cache shape/type changed')
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
    corners=[(a+b*x+c*y,d+e*x+f*y) for x,y in ((0,0),(WIDTH,0),(0,HEIGHT),(WIDTH,HEIGHT))]
    epsg=SOURCE_UNITS.get(source,(3857,1.,'metre'))[0]
    return {'width':WIDTH,'height':HEIGHT,'band_count':1,'source_crs':'EPSG:'+str(epsg),'extent_srid':epsg,'transform':list(grid),'band_nodata':None,
            'extent':[min(p[0] for p in corners),min(p[1] for p in corners),max(p[0] for p in corners),max(p[1] for p in corners)]}

def nearest_overviews(dataset,expected):
    import numpy as np
    band=dataset.GetRasterBand(1);previous=expected;h,w=expected.shape;dimensions=[]
    while max(h,w)>128:w,h=max(1,w//2),max(1,h//2);dimensions.append((w,h))
    if band.GetOverviewCount()!=len(dimensions):raise FixtureError('incorrect exact overview count')
    for i,shape in enumerate(dimensions):
        level=band.GetOverview(i)
        if (level.XSize,level.YSize)!=shape:raise FixtureError('overview dimensions differ')
        ys=np.floor(.5+np.arange(shape[1])*previous.shape[0]/shape[1]).astype(int);xs=np.floor(.5+np.arange(shape[0])*previous.shape[1]/shape[0]).astype(int);wanted=previous[np.ix_(ys,xs)]
        if not np.array_equal(level.ReadAsArray(),wanted,equal_nan=True) or not np.array_equal(level.GetMaskBand().ReadAsArray()!=0,np.isfinite(wanted)):raise FixtureError('complete nearest overview pixels or mask differ')
        previous=wanted
    level=band=None

def verify(root,names,control):
    import numpy as np
    from osgeo import gdal,osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('invalid exact TPI target inventory')
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
        if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue()):raise FixtureError('Float64 or NaN NoData changed')
        if not np.array_equal(actual,expected,equal_nan=True):raise FixtureError('complete exact TPI pixels differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected)):raise FixtureError('complete pixel mask differs')
        if band.GetUnitType()!='metre' or band.GetScale() is not None or band.GetOffset() is not None or band.GetColorTable() is not None:raise FixtureError('metre unit or source encoding differs')
        timings['pixels_mask_metadata_ms']+=round((time.monotonic()-pixel_start)*1000)
        with elapsed(timings,'cog_layout_ms'):
            if validate(ds,full_check=True)[1]:raise FixtureError('COG full layout validation failed')
        with elapsed(timings,'overviews_ms'):nearest_overviews(ds,expected)
        zeros=int(np.count_nonzero(np.isfinite(expected)&(expected==0)))
        if name in ('flat.cog.tif','plane.cog.tif','saddle.cog.tif','filled.cog.tif') and zeros!=(HEIGHT-2)*(WIDTH-2):raise FixtureError('analytic valid zero differs')
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,'overview_count':band.GetOverviewCount(),
            'valid_pixels':int(np.isfinite(expected).sum()),'zero_pixels':zeros,
            **{k:True for k in ('full_pixel_and_mask_verification','independent_decimal_oracle_verified','complete_neighbourhood_verified','metre_unit_verified','nearest_overviews_verified','eight_equal_neighbours_verified','legal_zero_verified')}}
        band=ds=None
    result['verification_timing_ms']=timings
    result['oracle_cache']={'scope_sha256':scope_hash(control['scope']),'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}
    return result

def archive_cogs(root,artifacts,verified):
    """Host-side byte copy after Runtime verification; no image re-encoding."""
    names=set(verified['targets']);directory=artifacts/'tpi-cogs';manifest_path=artifacts/'tpi-cogs-manifest.json'
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
        manifest[name]={'path':'tpi-cogs/'+name,**digest(path)}
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
    expected=os.environ.get('ADDP_ONLINE_TPI_ORACLE_ANCHOR_SHA256')
    if expected is not None and digest(path)['sha256']!=expected:raise FixtureError('oracle anchor changed')
    return path,value

def main(action):
    secret=guard()
    if action in {'start','stop'}:return NFSExport('tpi',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'tpi-source.json').write_text(json.dumps(result));return result
    if action=='prepare-oracle':
        scope={'schema':CACHE_SCHEMA,'run_id':os.environ['ADDP_ONLINE_TEST_RUN_ID'],'source_root':str(root.resolve()),
               'fixture_sha256':digest(Path(__file__))['sha256'],'sources':json.loads((secret/'tpi-source.json').read_text())['sources']}
        path=secret/CONTROL_NAME
        fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb')as f:f.write(encoded({'scope':scope,'arrays':None}))
        result=runtime_worker(action,root,[json.dumps(scope)])
        if result.get('scope')!=scope or set(result.get('arrays',{}))!=set(TARGETS):raise FixtureError('oracle prepare response differs')
        temp=path.with_suffix('.tmp')
        with temp.open('xb')as f:f.write(encoded({'scope':scope,'arrays':result['arrays']}))
        temp.chmod(0o600);temp.replace(path)
        return {'scope_sha256':scope_hash(scope),'anchor_sha256':digest(path)['sha256'],'expected_array_count':len(TARGETS),
                'cache_bytes':sum(v['size_bytes']for v in result['arrays'].values()),'timing_ms':result['timing_ms']}
    if action=='cleanup-oracle':
        if not(secret/CONTROL_NAME).exists():return {'oracle_cache_removed':True,'not_started':True}
        path,control=private_control(secret);result=runtime_worker(action,root,[json.dumps(control)])
        if result.get('oracle_cache_removed')is not True:raise FixtureError('oracle cleanup proof missing')
        path.unlink();return {'oracle_cache_removed':True}
    if action=='verify':
        if not os.environ.get('ADDP_ONLINE_TPI_ORACLE_ANCHOR_SHA256'):raise FixtureError('oracle anchor is required')
        _,control=private_control(secret);timings={}
        with elapsed(timings,'worker_transport_ms'):result=runtime_worker('verify',root,[json.dumps(control),*sys.argv[2:]])
        with elapsed(timings,'source_receipt_ms'):
            if result['sources']!=json.loads((secret/'tpi-source.json').read_text())['sources']:raise FixtureError('source file changed')
        with elapsed(timings,'archive_ms'):result['archived_cogs']=archive_cogs(root,Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']),result)
        result['host_timing_ms']=timings;return result
    raise FixtureError('unknown fixture action')
if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1]);print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster TPI fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
