#!/usr/bin/env python3
"""Owned focal percentiles, independent scalar/Fraction oracle and immutable COGs."""
from __future__ import annotations
import hashlib,io,json,os,re,subprocess,sys,tempfile,time
from contextlib import contextmanager
from functools import lru_cache
from pathlib import Path

SUITE='raster-focal-percentile-persistence'
RUNTIME='addp-raster-focal-percentile-runtime'
WIDTH,HEIGHT=519,515
CASES={
 'p0-3.cog.tif':('values.tif',3,'percentile',0,'direct'),
 'min-3.cog.tif':('values.tif',3,'min',None,'direct'),
 'p100-5.cog.tif':('values.tif',5,'percentile',100,'direct'),
 'max-5.cog.tif':('values.tif',5,'max',None,'direct'),
 'p50-3.cog.tif':('values.tif',3,'percentile',50,'direct'),
 'median-3.cog.tif':('values.tif',3,'median',None,'direct'),
 'p50-5.cog.tif':('values.tif',5,'percentile',50,'direct'),
 'median-5.cog.tif':('values.tif',5,'median',None,'direct'),
 'p25-3.cog.tif':('values.tif',3,'percentile',25,'direct'),
 'p75-5.cog.tif':('values.tif',5,'percentile',75,'direct'),
 'p33-5.cog.tif':('values.tif',5,'percentile',33.3,'direct'),
 'reclassified.cog.tif':('values.tif',3,'percentile',90,'reclassify'),
 'int64.cog.tif':('int64.tif',3,'percentile',25,'direct'),
 'uint64.cog.tif':('uint64.tif',5,'percentile',75,'direct'),
 'unlocated.cog.tif':('unlocated.tif',5,'percentile',25,'direct'),
 'invalid.cog.tif':('invalid.tif',3,'percentile',50,'direct'),
}
TARGETS=tuple(CASES)
SOURCES=('values.tif','unlocated.tif','invalid.tif','int64.tif','uint64.tif','complex.tif')
COMPARISONS=(('p0-3.cog.tif','min-3.cog.tif'),('p100-5.cog.tif','max-5.cog.tif'),
             ('p50-3.cog.tif','median-3.cog.tif'),('p50-5.cog.tif','median-5.cog.tif'))
RTOL=4e-15
ATOL=0.
class FixtureError(RuntimeError):pass

def command(args,**kwargs):
    try:return subprocess.run(args,check=True,capture_output=True,text=True,timeout=150,**kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:
        detail=(error.stderr or '').strip()[-4000:] or 'no stderr diagnostic'
        raise FixtureError(f'fixture command failed (exit {error.returncode}): {detail}') from error

if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker

def guard(env=os.environ):return NFSExport('focal-percentile',command).guard(env)
def owned_root(secret):return NFSExport('focal-percentile',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}

def source_spec(name):
    if name not in SOURCES:raise FixtureError('unknown exact percentile source')
    return {'width':WIDTH,'height':HEIGHT,'grid':None if name=='unlocated.tif'else(500000.,2.,.25,3000000.,-.5,-3.),
            'epsg':0 if name=='unlocated.tif'else 3857,
            'nodata':-2**63 if name=='int64.tif'else 2**64-2 if name=='uint64.tif'else-2.**40}

def source_arrays(name):
    import numpy as np
    spec=source_spec(name);y,x=np.indices((HEIGHT,WIDTH))
    values=((x%17)*3+(y%13)*5-50).astype(float)/4
    alpha=np.full(values.shape,255.);mask=np.full(values.shape,255,np.uint8)
    # Non-power-of-two counts and distinct fractional order statistics, not a plane.
    values[0,:4]=[0.,1.,8.,9.];values[20:25,20:29]=spec['nodata']
    values[22,22:26]=[0.,1.,8.,9.]
    values[200:207,200:207]=spec['nodata'];values[203,203]=0.
    values[510,511]=spec['nodata'];values[508,512]=np.inf;values[512,509]=np.nan
    alpha[509,510]=0;alpha[511,513]=np.nan;alpha[513,510]=-1;alpha[510,512]=np.inf
    mask[513,512]=0;alpha[500,500]=128.
    if name=='invalid.tif':values[:]=spec['nodata']
    if name in ('int64.tif','uint64.tif'):
        values=np.full(values.shape,7,np.int64 if name=='int64.tif'else np.uint64)
        values[512,517]=spec['nodata'];values[512,518]=-2**63+1 if name=='int64.tif'else 2**64-3
        alpha=np.full(values.shape,255,dtype=values.dtype);alpha[509,510]=0
    return values,alpha,mask

def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/n).exists()for n in SOURCES)or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite fixture data')
    for name in SOURCES:
        spec=source_spec(name);values,alpha,mask=source_arrays(name)
        native=gdal.GDT_Int64 if name=='int64.tif'else gdal.GDT_UInt64 if name=='uint64.tif'else gdal.GDT_CFloat64 if name=='complex.tif'else gdal.GDT_Float64
        dtype=np.int64 if name=='int64.tif'else np.uint64 if name=='uint64.tif'else np.complex128 if name=='complex.tif'else np.float64
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),WIDTH,HEIGHT,4,native,['TILED=YES','COMPRESS=DEFLATE'])
        if spec['grid'] is not None:
            ds.SetGeoTransform(spec['grid']);crs=osr.SpatialReference();crs.ImportFromEPSG(spec['epsg']);ds.SetSpatialRef(crs)
        # Unselected fractional data are deliberate; percentile must validate only selected band2.
        for i,data in enumerate((np.full(values.shape,42 if name in('int64.tif','uint64.tif')else 42.25),values,np.full(values.shape,17),alpha),1):
            band=ds.GetRasterBand(i);band.WriteArray(np.asarray(data,dtype=dtype))
            if name=='int64.tif':band.SetNoDataValueAsInt64(spec['nodata'])
            elif name=='uint64.tif':band.SetNoDataValueAsUInt64(spec['nodata'])
            else:band.SetNoDataValue(spec['nodata'])
            band=None
        ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        if name=='values.tif':ds.GetRasterBand(2).SetScale(2.);ds.GetRasterBand(2).SetOffset(7.)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            if ds.CreateMaskBand(gdal.GMF_PER_DATASET)!=gdal.CE_None:raise FixtureError('cannot create internal source mask')
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
        ds=gdal.Open(str(root/name));band=ds.GetRasterBand(2)
        if ds.GetFileList()!=[str(root/name)]or not np.array_equal(band.ReadAsArray(),np.asarray(values,dtype=dtype),equal_nan=True)or not np.array_equal(band.GetMaskBand().ReadAsArray(),mask):raise FixtureError('native source bytes or coverage changed')
        if (ds.GetGeoTransform(can_return_null=True)or None)!=spec['grid']:raise FixtureError('source grid facts changed')
        band=ds=None
    return {'sources':{n:digest(root/n)for n in SOURCES}}

def class_rules():
    return [{'min':None,'max':0,'class':0},{'min':0,'max':10,'class':10},{'min':10,'max':None,'class':20}]

@lru_cache(maxsize=16)
def expected_inputs(name):
    import numpy as np
    source,window,method,p,stage=CASES[name];values,alpha,mask=source_arrays(source)
    # Native integer membership must precede the declared Float64 value precision.
    valid=np.isfinite(values)&(values!=source_spec(source)['nodata'])&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    values=values.astype(np.float64)
    if stage=='reclassify':values=np.where(values<0,0.,np.where(values<10,10.,20.))
    return values,valid

def scalar_value(samples,percentile):
    from fractions import Fraction
    ordered=sorted(samples);position=(len(ordered)-1)*Fraction(float(percentile))/100
    i=position.numerator//position.denominator;t=position-i
    return float(Fraction(ordered[i])*(1-t)+Fraction(ordered[i+bool(t)])*t)

@lru_cache(maxsize=16)
def expected_pixels(name):
    """Enumerate every actual neighbour; cache only identical sorted multisets."""
    import numpy as np
    values,valid=expected_inputs(name);window,method,p=CASES[name][1:4];radius=window//2
    percentile=0 if method=='min'else 100 if method=='max'else 50 if method=='median'else p
    answer=np.full(values.shape,np.nan);known={}
    for row,col in zip(*np.nonzero(valid)):
        sample=tuple(sorted(float(values[y,x]) for y in range(max(0,row-radius),min(HEIGHT,row+radius+1))
                            for x in range(max(0,col-radius),min(WIDTH,col+radius+1)) if valid[y,x]))
        if sample not in known:known[sample]=scalar_value(sample,percentile)
        answer[row,col]=known[sample]
    answer.flags.writeable=False;return answer

def assert_pixels(actual,expected,name):
    import numpy as np
    # Dyadic percentiles and controls are exact for the fixed quantized fixtures.
    exact=CASES[name][2]!='percentile' or CASES[name][3] in (0,25,50,75,100)
    if not np.array_equal(np.isfinite(actual),np.isfinite(expected)):
        raise FixtureError('complete pixel mask differs')
    if exact:
        valid=np.array_equal(actual,expected,equal_nan=True)
    else:
        valid=np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)
    if not valid:raise FixtureError('complete independent linear percentile pixels differ')
    if not np.array_equal(actual==0,expected==0):raise FixtureError('legal zero or cancellation changed')

CACHE_SCHEMA='addp.percentile-oracle/v1'
CACHE_MARKER='.oracle-owner'
CONTROL_NAME='percentile-oracle-control.json'

@contextmanager
def elapsed(timings,key):
    start=time.monotonic()
    try:yield
    finally:timings[key]=timings.get(key,0)+round((time.monotonic()-start)*1000)

def encoded(value):return json.dumps(value,sort_keys=True,separators=(',',':')).encode()
def scope_hash(scope):return hashlib.sha256(encoded(scope)).hexdigest()
def oracle_path(scope):return Path(tempfile.gettempdir())/('addp-percentile-oracle-'+scope_hash(scope))

def validate_scope(root,scope,*,sources):
    if (not isinstance(scope,dict) or set(scope)!={'schema','run_id','fixture_sha256','source_root','sources'}
        or scope['schema']!=CACHE_SCHEMA or not re.fullmatch('focal-percentile-[0-9]+-[0-9]+',scope.get('run_id',''))
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
    source=CASES[name][0];spec=source_spec(source);grid=spec['grid']
    corners=[]
    if grid is not None:
        a,b,c,d,e,f=grid;corners=[(a+b*x+c*y,d+e*x+f*y)for x,y in ((0,0),(WIDTH,0),(0,HEIGHT),(WIDTH,HEIGHT))]
    return {'width':WIDTH,'height':HEIGHT,'band_count':1,'source_crs':'EPSG:'+str(spec['epsg'])if spec['epsg']else'',
            'extent_srid':spec['epsg'],'transform':list(grid)if grid else[],'band_nodata':None,
            'extent':[min(p[0]for p in corners),min(p[1]for p in corners),max(p[0]for p in corners),max(p[1]for p in corners)]if corners else[]}


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
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('invalid exact PERCENTILE target inventory')
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
        if (ds.RasterXSize,ds.RasterYSize,list(ds.GetGeoTransform(can_return_null=True) or ()))!=(WIDTH,HEIGHT,spec['transform']):raise FixtureError('source grid changed')
        if spec['extent_srid']:
            crs=osr.SpatialReference();crs.ImportFromEPSG(spec['extent_srid'])
            if ds.GetSpatialRef() is None or not ds.GetSpatialRef().IsSame(crs):raise FixtureError('source CRS changed')
        elif ds.GetSpatialRef() is not None or ds.GetProjection():raise FixtureError('missing source CRS was fabricated')
        band=ds.GetRasterBand(1);actual=band.ReadAsArray()
        if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue()):raise FixtureError('Float64 or NaN NoData changed')
        assert_pixels(actual,expected,name)
        if not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected)):raise FixtureError('complete pixel mask differs')
        if band.GetUnitType()!='' or band.GetScale() is not None or band.GetOffset() is not None or band.GetColorTable() is not None:raise FixtureError('percentile unit or source encoding differs')
        timings['pixels_mask_metadata_ms']+=round((time.monotonic()-pixel_start)*1000)
        with elapsed(timings,'cog_layout_ms'):
            if validate(ds,full_check=True)[1]:raise FixtureError('COG full layout validation failed')
        with elapsed(timings,'overviews_ms'):nearest_overviews(ds,actual)
        zeros=int(np.count_nonzero(np.isfinite(expected)&(expected==0)))
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,'overview_count':band.GetOverviewCount(),
            'valid_pixels':int(np.isfinite(expected).sum()),'zero_pixels':zeros,'negative_pixels':int(np.count_nonzero(expected<0)),
            **{k:True for k in ('full_pixel_and_mask_verification','independent_fraction_oracle_verified','original_coverage_verified','linear_position_verified','percentile_encoding_verified','nearest_overviews_verified','explicit_selected_band_verified','legal_zero_verified')}}
        band=ds=None
    comparisons=[]
    for left,right in COMPARISONS:
        if left in names and right in names:
            a=gdal.Open(str(root/'outputs'/left));b=gdal.Open(str(root/'outputs'/right))
            if not np.array_equal(a.ReadAsArray(),b.ReadAsArray(),equal_nan=True):raise FixtureError('actual percentile endpoint or median control differs')
            a=b=None;comparisons.append([left,right])
    result['comparisons']=comparisons
    result['verification_timing_ms']=timings
    result['oracle_cache']={'scope_sha256':scope_hash(control['scope']),'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}
    return result

def archive_cogs(root,artifacts,verified):
    """Host-side byte copy after Runtime verification; no image re-encoding."""
    names=set(verified['targets']);directory=artifacts/'percentile-cogs';manifest_path=artifacts/'percentile-cogs-manifest.json'
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
        manifest[name]={'path':'percentile-cogs/'+name,**digest(path)}
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
    expected=os.environ.get('ADDP_ONLINE_FOCAL_PERCENTILE_ORACLE_ANCHOR_SHA256')
    if expected is not None and digest(path)['sha256']!=expected:raise FixtureError('oracle anchor changed')
    return path,value

def main(action):
    secret=guard()
    if action in {'start','stop'}:return NFSExport('focal-percentile',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'focal-percentile-source.json').write_text(json.dumps(result));return result
    if action=='prepare-oracle':
        scope={'schema':CACHE_SCHEMA,'run_id':os.environ['ADDP_ONLINE_TEST_RUN_ID'],'source_root':str(root.resolve()),
               'fixture_sha256':digest(Path(__file__))['sha256'],'sources':json.loads((secret/'focal-percentile-source.json').read_text())['sources']}
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
        if not os.environ.get('ADDP_ONLINE_FOCAL_PERCENTILE_ORACLE_ANCHOR_SHA256'):raise FixtureError('oracle anchor is required')
        _,control=private_control(secret);timings={}
        with elapsed(timings,'worker_transport_ms'):result=runtime_worker('verify',root,[json.dumps(control),*sys.argv[2:]])
        with elapsed(timings,'source_receipt_ms'):
            if result['sources']!=json.loads((secret/'focal-percentile-source.json').read_text())['sources']:raise FixtureError('source file changed')
        with elapsed(timings,'archive_ms'):result['archived_cogs']=archive_cogs(root,Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']),result)
        result['host_timing_ms']=timings;return result
    raise FixtureError('unknown fixture action')
if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1]);print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster PERCENTILE fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
