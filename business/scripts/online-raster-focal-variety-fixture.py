#!/usr/bin/env python3
"""Owned focal variety, independent scalar oracle and immutable COG evidence."""
from __future__ import annotations
import hashlib,io,json,os,re,subprocess,sys,tempfile,time,math,statistics
from collections import Counter
from fractions import Fraction
from contextlib import contextmanager
from functools import lru_cache
from pathlib import Path

SUITE='raster-focal-variety-persistence'
RUNTIME='addp-raster-focal-variety-runtime'
WIDTH,HEIGHT=519,515
CASES={
 'variety-3.cog.tif':('values.tif',3,'variety',None,'direct'),
 'variety-5.cog.tif':('values.tif',5,'variety',None,'direct'),
 'reclassified.cog.tif':('values.tif',3,'variety',None,'reclassify'),
 'zero.cog.tif':('zero.tif',5,'variety',None,'direct'),
 'int64.cog.tif':('int64.tif',3,'variety',None,'direct'),
 'uint64.cog.tif':('uint64.tif',5,'variety',None,'direct'),
 'unlocated-row.cog.tif':('unlocated.tif',5,'variety',None,'direct'),
 'invalid.cog.tif':('invalid.tif',3,'variety',None,'direct'),
 'control-sum.cog.tif':('values.tif',3,'sum',None,'direct'),
 'control-mean.cog.tif':('values.tif',3,'mean',None,'direct'),
 'control-min.cog.tif':('values.tif',3,'min',None,'direct'),
 'control-max.cog.tif':('values.tif',3,'max',None,'direct'),
 'control-stddev.cog.tif':('values.tif',3,'stddev',None,'direct'),
 'control-mode.cog.tif':('values.tif',3,'mode',None,'direct'),
 'control-median.cog.tif':('values.tif',3,'median',None,'direct'),
 'control-percentile.cog.tif':('values.tif',3,'percentile',50,'direct'),
}
FAILURES=('fractional','positive_unsafe','negative_unsafe','band_zero','band_high','band_boolean','band_string',
          'alpha','complex','window_value','window_boolean','window_string','method','unused')
TARGETS=tuple(CASES)
SOURCES=('values.tif','zero.tif','int64.tif','uint64.tif','unlocated.tif','invalid.tif','fractional.tif','positive-unsafe.tif','negative-unsafe.tif','complex.tif')
COMPARISONS=(('control-median.cog.tif','control-percentile.cog.tif'),)
RTOL=1e-12
ATOL=1e-12
class FixtureError(RuntimeError):pass

def command(args,**kwargs):
    try:return subprocess.run(args,check=True,capture_output=True,text=True,timeout=150,**kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:
        detail=(error.stderr or '').strip()[-4000:] or 'no stderr diagnostic'
        raise FixtureError(f'fixture command failed (exit {error.returncode}): {detail}') from error

if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker
    import importlib
    archive_files=importlib.import_module('business.scripts.online-raster-zonal-percentile-fixture').archive_files

def guard(env=os.environ):return NFSExport('focal-variety',command).guard(env)
def owned_root(secret):return NFSExport('focal-variety',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}

def source_spec(name):
    if name not in SOURCES:raise FixtureError('unknown exact variety source')
    small=name in ('fractional.tif','positive-unsafe.tif','negative-unsafe.tif','complex.tif')
    return {'width':19 if small else WIDTH,'height':1 if name=='unlocated.tif' else 7 if small else HEIGHT,
            'grid':None if name=='unlocated.tif' else (500000.,2.,.25,3000000.,-.5,-3.),'epsg':0 if name=='unlocated.tif' else 3857,
            'nodata':-2**63 if name=='int64.tif' else 2**64-1 if name=='uint64.tif' else -9999.}

def case_shape(name):
    spec=source_spec(CASES[name][0]);return spec['height'],spec['width']

def expected_cache_bytes():return sum(h*w*8 for h,w in (case_shape(n) for n in TARGETS))

def source_arrays(name):
    import numpy as np
    spec=source_spec(name);y,x=np.indices((spec['height'],spec['width']))
    # Signed powers of two keep old sum control mathematically exact while
    # retaining 25 distinct safe classes and 9/25 complete-window counts.
    classes=np.array([-2**k for k in range(12,0,-1)]+[0]+[2**k for k in range(1,13)],dtype=float)
    values=classes[(x%5)+5*(y%5)]
    alpha=np.full(values.shape,255.);mask=np.full(values.shape,255,np.uint8)
    values[0,1]=spec['nodata'];alpha[0,2]=0.;alpha[0,3]=128.;mask[0,4]=0
    if spec['height']>500:
        values[40:60,40:60]=0.;values[200:207,200:207]=spec['nodata'];values[203,203]=0.
        values[510,511]=spec['nodata'];values[508,512]=np.inf;values[512,509]=np.nan
        alpha[509,510]=0.;alpha[511,513]=np.nan;alpha[513,510]=-1.;alpha[510,512]=np.inf;mask[513,512]=0
    if name=='zero.tif':values[:]=0.;values[200:207,200:207]=spec['nodata'];values[203,203]=0.
    if name=='invalid.tif':values[:]=spec['nodata']
    if name in ('int64.tif','uint64.tif'):
        dtype=np.int64 if name=='int64.tif' else np.uint64
        values=np.asarray(((x%3)+3*(y%3))-(3 if name=='int64.tif' else 0),dtype=dtype)
        values[0,1]=spec['nodata'];values[512,517]=spec['nodata'];alpha=np.full(values.shape,255,dtype=dtype);alpha[509,510]=0
    if name in ('fractional.tif','positive-unsafe.tif','negative-unsafe.tif'):
        values[0,0]={'fractional.tif':.5,'positive-unsafe.tif':2**53,'negative-unsafe.tif':-2**53}[name]
    if name=='complex.tif':values=values.astype(complex)+2j;alpha=alpha.astype(complex)
    return values,alpha,mask

def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/n).exists()for n in SOURCES)or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite fixture data')
    for name in SOURCES:
        spec=source_spec(name);values,alpha,mask=source_arrays(name)
        native=gdal.GDT_Int64 if name=='int64.tif'else gdal.GDT_UInt64 if name=='uint64.tif'else gdal.GDT_CFloat64 if name=='complex.tif'else gdal.GDT_Float64
        dtype=np.int64 if name=='int64.tif'else np.uint64 if name=='uint64.tif'else np.complex128 if name=='complex.tif'else np.float64
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),spec['width'],spec['height'],4,native,['TILED=YES','COMPRESS=DEFLATE'])
        if spec['grid'] is not None:
            ds.SetGeoTransform(spec['grid']);crs=osr.SpatialReference();crs.ImportFromEPSG(spec['epsg']);ds.SetSpatialRef(crs)
        # Unselected fractional data are deliberate; variety must validate only selected band2.
        for i,data in enumerate((np.full(values.shape,42 if name in('int64.tif','uint64.tif')else 42.25),values,np.full(values.shape,17),alpha),1):
            band=ds.GetRasterBand(i);band.WriteArray(np.asarray(data,dtype=dtype))
            if name=='int64.tif':band.SetNoDataValueAsInt64(spec['nodata'])
            elif name=='uint64.tif':band.SetNoDataValueAsUInt64(spec['nodata'])
            else:band.SetNoDataValue(spec['nodata'])
            band=None
        ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        if name=='values.tif':ds.GetRasterBand(2).SetScale(0.);ds.GetRasterBand(2).SetOffset(.5)
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

def scalar_value(samples,method,percentile,center):
    if method=='variety':return float(len(set(samples)))
    if method=='mode':
        counts=Counter(samples);modes=[v for v,n in counts.items() if n==max(counts.values())]
        return center if center in modes else min(modes)
    if method in ('median','percentile'):
        ordered=sorted(samples);position=(len(ordered)-1)*Fraction(float(50 if method=='median' else percentile))/100
        i=position.numerator//position.denominator;t=position-i
        return float(Fraction(ordered[i])*(1-t)+Fraction(ordered[i+bool(t)])*t)
    return {'sum':math.fsum,'mean':statistics.fmean,'min':min,'max':max,'stddev':statistics.pstdev}[method](samples)

@lru_cache(maxsize=16)
def expected_pixels(name):
    """Independent scalar set/count/math oracle; no product or NumPy reducer."""
    import numpy as np
    values,valid=expected_inputs(name);window,method,p=CASES[name][1:4];radius=window//2
    height,width=values.shape;answer=np.full(values.shape,np.nan);known={}
    for row,col in zip(*np.nonzero(valid)):
        samples=tuple(sorted(float(values[y,x]) for y in range(max(0,row-radius),min(height,row+radius+1))
                             for x in range(max(0,col-radius),min(width,col+radius+1)) if valid[y,x]))
        key=(samples,float(values[row,col]) if method=='mode' else None)
        if key not in known:known[key]=scalar_value(samples,method,p,float(values[row,col]))
        answer[row,col]=known[key]
    answer.flags.writeable=False;return answer

def assert_pixels(actual,expected,name):
    import numpy as np
    if not np.array_equal(np.isfinite(actual),np.isfinite(expected)):raise FixtureError('complete pixel mask differs')
    method=CASES[name][2]
    valid=np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True) if method in ('mean','stddev') else np.array_equal(actual,expected,equal_nan=True)
    if not valid:raise FixtureError('complete independent variety/control pixels differ')
    if method=='variety':
        data=actual[np.isfinite(actual)]
        if np.any((data<1)|(data>CASES[name][1]**2)|(data!=np.trunc(data))):raise FixtureError('variety must be exact positive neighbour counts')
    if not np.array_equal(actual==0,expected==0):raise FixtureError('legal zero changed')

CACHE_SCHEMA='addp.variety-oracle/v1'
CACHE_MARKER='.oracle-owner'
CONTROL_NAME='variety-oracle-control.json'
EXPECTED_EXPORT='.variety-expected'

@contextmanager
def elapsed(timings,key):
    start=time.monotonic()
    try:yield
    finally:timings[key]=timings.get(key,0)+round((time.monotonic()-start)*1000)

def encoded(value):return json.dumps(value,sort_keys=True,separators=(',',':')).encode()
def scope_hash(scope):return hashlib.sha256(encoded(scope)).hexdigest()
def oracle_path(scope):return Path(tempfile.gettempdir())/('addp-variety-oracle-'+scope_hash(scope))

def validate_scope(root,scope,*,sources):
    if (not isinstance(scope,dict) or set(scope)!={'schema','run_id','fixture_sha256','source_root','sources'}
        or scope['schema']!=CACHE_SCHEMA or not re.fullmatch('focal-variety-[0-9]+-[0-9]+',scope.get('run_id',''))
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
    control={'scope':scope,'arrays':arrays,'timing_ms':timings}
    with elapsed(timings,'expected_export_ms'):export_expected(root,control,directory)
    return control

def validate_expected_export(root,control):
    directory=root/EXPECTED_EXPORT
    if directory.is_symlink() or not directory.is_dir() or directory.stat().st_mode&0o777!=0o755 or {p.name for p in directory.iterdir()}!={n+'.npy' for n in TARGETS}:
        raise FixtureError('frozen expected export inventory changed')
    for name,fact in control['arrays'].items():
        path=directory/(name+'.npy')
        if path.is_symlink() or not path.is_file() or path.stat().st_mode&0o777!=0o444 or digest(path)!=fact:raise FixtureError('frozen expected export bytes changed')
    return directory

def export_expected(root,control,cache):
    directory=root/EXPECTED_EXPORT
    if directory.exists() or directory.is_symlink():return validate_expected_export(root,control)
    directory.mkdir(mode=0o755);directory.chmod(0o755)
    for name in TARGETS:
        path=directory/(name+'.npy')
        with path.open('xb') as out:out.write((cache/(name+'.npy')).read_bytes())
        path.chmod(0o444)
    return validate_expected_export(root,control)

def archive_expectations(root,artifacts,previous):
    return archive_files(root/EXPECTED_EXPORT,artifacts,[n+'.npy' for n in TARGETS],previous,'variety-expectations')

def validate_cache(control):
    scope=control['scope'];directory=cache_directory(scope);arrays=control.get('arrays')
    if (not isinstance(arrays,dict) or set(arrays)!=set(TARGETS)
        or {p.name for p in directory.iterdir()}!={CACHE_MARKER,*[n+'.npy'for n in TARGETS]}):
        raise FixtureError('oracle cache inventory changed')
    for name,fact in arrays.items():
        if (set(fact)!={'sha256','size_bytes'} or type(fact['size_bytes'])is not int
            or not math.prod(case_shape(name))*8<fact['size_bytes']<math.prod(case_shape(name))*8+1024
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
    if answer.shape!=case_shape(name) or answer.dtype!=np.dtype('float64'):raise FixtureError('oracle cache shape/type changed')
    answer.flags.writeable=False;return answer

def cleanup_oracle(root,control):
    scope=control['scope'];validate_scope(root,scope,sources=False);directory=oracle_path(scope)
    export=root/EXPECTED_EXPORT
    if export.exists() or export.is_symlink():
        if export.is_symlink() or not export.is_dir() or any(p.is_symlink() or not p.is_file() or p.name not in {n+'.npy' for n in TARGETS} for p in export.iterdir()):raise FixtureError('unsafe expected export cleanup inventory')
        for path in export.iterdir():path.unlink()
        export.rmdir()
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
        a,b,c,d,e,f=grid;corners=[(a+b*x+c*y,d+e*x+f*y)for x,y in ((0,0),(spec['width'],0),(0,spec['height']),(spec['width'],spec['height']))]
    return {'width':spec['width'],'height':spec['height'],'band_count':1,'source_crs':'EPSG:'+str(spec['epsg'])if spec['epsg']else'',
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
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('invalid exact VARIETY target inventory')
    if sorted(p.name for p in root.iterdir())!=sorted([*SOURCES,'outputs','.addp-online-owner',EXPECTED_EXPORT]) or sorted(p.name for p in (root/'outputs').iterdir())!=sorted(names):raise FixtureError('unexpected source/output sidecar or temporary file')
    verify_sources(root)
    timings={k:0 for k in ('source_hash_ms','cache_load_ms','target_hash_ms','pixels_mask_metadata_ms','cog_layout_ms','overviews_ms')}
    with elapsed(timings,'source_hash_ms'):
        validate_scope(root,control['scope'],sources=True)
        result={'sources':control['scope']['sources'],'targets':{},'full_source_pixels_verified':True}
    with elapsed(timings,'cache_load_ms'):
        directory=validate_cache(control);validate_expected_export(root,control)
    for name in names:
        with elapsed(timings,'target_hash_ms'):path=root/'outputs'/name;fact=digest(path)
        with elapsed(timings,'cache_load_ms'):expected=load_expected(control,directory,name)
        pixel_start=time.monotonic();ds=gdal.Open(str(path));spec=artifact_expectation(name)
        if ds is None or ds.GetDriver().ShortName!='GTiff' or ds.RasterCount!=1:raise FixtureError('expected one-band GeoTIFF')
        if (ds.RasterXSize,ds.RasterYSize,list(ds.GetGeoTransform(can_return_null=True) or ()))!=(spec['width'],spec['height'],spec['transform']):raise FixtureError('source grid changed')
        if spec['extent_srid']:
            crs=osr.SpatialReference();crs.ImportFromEPSG(spec['extent_srid'])
            if ds.GetSpatialRef() is None or not ds.GetSpatialRef().IsSame(crs):raise FixtureError('source CRS changed')
        elif ds.GetSpatialRef() is not None or ds.GetProjection():raise FixtureError('missing source CRS was fabricated')
        band=ds.GetRasterBand(1);actual=band.ReadAsArray()
        if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue()):raise FixtureError('Float64 or NaN NoData changed')
        assert_pixels(actual,expected,name)
        if not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected)):raise FixtureError('complete pixel mask differs')
        if band.GetUnitType()!='' or band.GetScale() is not None or band.GetOffset() is not None or band.GetColorTable() is not None:raise FixtureError('focal output unit or source encoding differs')
        timings['pixels_mask_metadata_ms']+=round((time.monotonic()-pixel_start)*1000)
        with elapsed(timings,'cog_layout_ms'):
            if validate(ds,full_check=True)[1]:raise FixtureError('COG full layout validation failed')
        with elapsed(timings,'overviews_ms'):nearest_overviews(ds,actual)
        zeros=int(np.count_nonzero(np.isfinite(expected)&(expected==0)))
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,'overview_count':band.GetOverviewCount(),
            'valid_pixels':int(np.isfinite(expected).sum()),'zero_pixels':zeros,'negative_pixels':int(np.count_nonzero(expected<0)),
            **{k:True for k in ('full_pixel_and_mask_verification','independent_scalar_oracle_verified','original_coverage_verified','distinct_classes_verified','count_encoding_verified','nearest_overviews_verified','explicit_selected_band_verified','legal_zero_verified')}}
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

def archive_cogs(root,artifacts,verified,previous):
    return archive_files(root/'outputs',artifacts,list(verified['targets']),previous,'variety-cogs')

def verify_sources(root):
    import numpy as np
    from osgeo import gdal,osr
    for name in SOURCES:
        spec=source_spec(name);values,alpha,mask=source_arrays(name);ds=gdal.Open(str(root/name))
        dtype=gdal.GDT_Int64 if name=='int64.tif' else gdal.GDT_UInt64 if name=='uint64.tif' else gdal.GDT_CFloat64 if name=='complex.tif' else gdal.GDT_Float64
        if ds is None or ds.GetDriver().ShortName!='GTiff' or ds.RasterCount!=4 or (ds.RasterXSize,ds.RasterYSize)!=(spec['width'],spec['height']) or ds.GetGeoTransform(can_return_null=True)!=spec['grid'] or ds.GetFileList()!=[str(root/name)]:raise FixtureError('complete source dimensions/grid/inventory differ')
        if spec['epsg']:
            reference=osr.SpatialReference();reference.ImportFromEPSG(spec['epsg'])
            if ds.GetSpatialRef() is None or not ds.GetSpatialRef().IsSame(reference):raise FixtureError('source CRS changed')
        elif ds.GetProjection():raise FixtureError('missing source location fabricated')
        arrays=(np.full(values.shape,42 if name in ('int64.tif','uint64.tif') else 42.25),values,np.full(values.shape,17),alpha)
        for index,wanted in enumerate(arrays,1):
            band=ds.GetRasterBand(index)
            if band.DataType!=dtype or band.GetNoDataValue()!=spec['nodata'] or not np.array_equal(band.ReadAsArray(),wanted.astype(values.dtype),equal_nan=True) or not np.array_equal(band.GetMaskBand().ReadAsArray(),mask):raise FixtureError('complete source pixels/mask/type/NoData differ')
            if band.GetColorInterpretation()!=(gdal.GCI_AlphaBand if index==4 else gdal.GCI_GrayIndex if index==1 else gdal.GCI_Undefined):raise FixtureError('source band interpretation changed')
            if (band.GetScale(),band.GetOffset())!=((0.,.5) if name=='values.tif' and index==2 else (None,None)):raise FixtureError('source scale/offset changed')
        band=ds=None

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
    expected=os.environ.get('ADDP_ONLINE_FOCAL_VARIETY_ORACLE_ANCHOR_SHA256')
    if expected is not None and digest(path)['sha256']!=expected:raise FixtureError('oracle anchor changed')
    return path,value

def main(action):
    secret=guard()
    if action in {'start','stop'}:return NFSExport('focal-variety',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'focal-variety-source.json').write_text(json.dumps(result));return result
    if action=='prepare-oracle':
        scope={'schema':CACHE_SCHEMA,'run_id':os.environ['ADDP_ONLINE_TEST_RUN_ID'],'source_root':str(root.resolve()),
               'fixture_sha256':digest(Path(__file__))['sha256'],'sources':json.loads((secret/'focal-variety-source.json').read_text())['sources']}
        path=secret/CONTROL_NAME
        fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb')as f:f.write(encoded({'scope':scope,'arrays':None}))
        result=runtime_worker(action,root,[json.dumps(scope)])
        if result.get('scope')!=scope or set(result.get('arrays',{}))!=set(TARGETS):raise FixtureError('oracle prepare response differs')
        temp=path.with_suffix('.tmp')
        with temp.open('xb')as f:f.write(encoded({'scope':scope,'arrays':result['arrays']}))
        temp.chmod(0o600);temp.replace(path)
        baseline=runtime_worker('verify',root,[json.dumps({'scope':scope,'arrays':result['arrays']})])
        artifacts=Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']);sources=archive_files(root,artifacts,SOURCES,{},'variety-sources')
        expectations=archive_expectations(root,artifacts,{})
        history=secret/'variety-archive-history.json'
        with history.open('xb') as f:f.write(encoded({'sources':sources,'targets':{},'expected':expectations}))
        return {**baseline,'archived_sources':sources,'archived_cogs':{},'archived_expectations':expectations,'scope_sha256':scope_hash(scope),'anchor_sha256':digest(path)['sha256'],'expected_array_count':len(TARGETS),
                'cache_bytes':sum(v['size_bytes']for v in result['arrays'].values()),'timing_ms':result['timing_ms']}
    if action=='cleanup-oracle':
        if not(secret/CONTROL_NAME).exists():return {'oracle_cache_removed':True,'not_started':True}
        path,control=private_control(secret);result=runtime_worker(action,root,[json.dumps(control)])
        if result.get('oracle_cache_removed')is not True:raise FixtureError('oracle cleanup proof missing')
        path.unlink();return {'oracle_cache_removed':True}
    if action=='verify':
        if not os.environ.get('ADDP_ONLINE_FOCAL_VARIETY_ORACLE_ANCHOR_SHA256'):raise FixtureError('oracle anchor is required')
        _,control=private_control(secret);timings={}
        with elapsed(timings,'worker_transport_ms'):result=runtime_worker('verify',root,[json.dumps(control),*sys.argv[2:]])
        with elapsed(timings,'source_receipt_ms'):
            if result['sources']!=json.loads((secret/'focal-variety-source.json').read_text())['sources']:raise FixtureError('source file changed')
        with elapsed(timings,'archive_ms'):
            history=secret/'variety-archive-history.json'
            if not history.is_file():raise FixtureError('archive history missing')
            old=json.loads(history.read_bytes());artifacts=Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
            result['archived_sources']=archive_files(root,artifacts,SOURCES,old['sources'],'variety-sources')
            result['archived_cogs']=archive_cogs(root,artifacts,result,old['targets'])
            result['archived_expectations']=archive_expectations(root,artifacts,old['expected'])
            history.write_bytes(encoded({'sources':result['archived_sources'],'targets':result['archived_cogs'],'expected':result['archived_expectations']}))
        result['host_timing_ms']=timings;return result
    raise FixtureError('unknown fixture action')
if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1]);print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster VARIETY fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
