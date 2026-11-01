#!/usr/bin/env python3
"""Owned focal proximity_rectangular, independent scalar oracle and immutable COG evidence."""
from __future__ import annotations
import hashlib,io,json,os,re,subprocess,sys,tempfile,time,math
from contextlib import contextmanager
from functools import lru_cache
from pathlib import Path

SUITE='raster-proximity-rectangular-persistence'
RUNTIME='addp-raster-proximity-rectangular-runtime'
WIDTH,HEIGHT=519,515
CASES={
 'wide.cog.tif':('wide.tif',2,[1]),'tall.cog.tif':('tall.tif',2,[1]),
 'fractional.cog.tif':('fractional.tif',2,[1]),'foot.cog.tif':('foot.tif',2,[1]),'survey.cog.tif':('survey.tif',2,[1]),
 'second-band.cog.tif':('second-band.tif',2,[1]),'nodata.cog.tif':('nodata.tif',2,[1]),
 'mask.cog.tif':('mask.tif',2,[1]),'alpha.cog.tif':('alpha.tif',2,[1]),
 'zero-negative.cog.tif':('zero-negative.tif',2,[0,-3]),'uint32.cog.tif':('uint32.tif',2,[0,4294967295]),
 'cross-block.cog.tif':('cross-block.tif',2,[1]),'row.cog.tif':('row.tif',2,[1]),'column.cog.tif':('column.tif',2,[1]),
 'square-metre.cog.tif':('square-metre.tif',2,[1]),'square-foot.cog.tif':('square-foot.tif',2,[1]),
}
FAILURES=('band_zero','band_high','band_boolean','alpha_band','float','complex','int64','uint64',
 'targets_empty','targets_duplicate','targets_boolean','targets_fraction','targets_object','targets_range','targets_nonlist',
 'no_target','invalid_targets','geographic','compound','missing_crs','missing_grid','unknown_unit','rotated_x','rotated_y','orientation','singular')
TARGETS=tuple(CASES)
SOURCES=tuple(v[0]for v in CASES.values())+tuple(n+'.tif'for n in ('float','complex','int64','uint64','no_target','invalid_targets','geographic','compound','missing_crs','missing_grid','unknown_unit','rotated_x','rotated_y','orientation','singular'))
class FixtureError(RuntimeError):pass

def command(args,**kwargs):
    try:return subprocess.run(args,check=True,capture_output=True,text=True,timeout=150,**kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:
        detail=(error.stderr or '').strip()[-4000:]or'no stderr diagnostic'
        raise FixtureError(f'fixture command failed (exit {error.returncode}): {detail}')from error

if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker
    import importlib
    archive_files=importlib.import_module('business.scripts.online-raster-zonal-percentile-fixture').archive_files

def guard(env=os.environ):return NFSExport('proximity-rectangular',command).guard(env)
def owned_root(secret):return NFSExport('proximity-rectangular',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def digest(path):
    if path.is_symlink()or not path.is_file()or path.stat().st_size<=0:raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}

def source_spec(name):
    if name not in SOURCES:raise FixtureError('unknown exact rectangular proximity source')
    grid=(100.,2.,0.,200.,0.,-5.)
    if name=='tall.tif':grid=(100.,5.,0.,200.,0.,-2.)
    if name=='fractional.tif':grid=(100.,.125,0.,200.,0.,-.5)
    if name.startswith('square-'):grid=(100.,2.5,0.,200.,0.,-2.5)
    if name=='rotated_x.tif':grid=(100.,2.,.25,200.,0.,-5.)
    if name=='rotated_y.tif':grid=(100.,2.,0.,200.,.25,-5.)
    if name=='orientation.tif':grid=(100.,-2.,0.,200.,0.,-5.)
    if name=='singular.tif':grid=(100.,2.,1.,200.,4.,2.)
    if name=='missing_grid.tif':grid=None
    dtype='UInt32'if name=='uint32.tif'else'Float64'if name=='float.tif'else'CFloat64'if name=='complex.tif'else'Int64'if name=='int64.tif'else'UInt64'if name=='uint64.tif'else'Int32'
    nodata=4294967294 if dtype=='UInt32'else 2**63-1 if dtype=='Int64'else 2**64-1 if dtype=='UInt64'else -9999
    return {'width':WIDTH if name=='cross-block.tif'else 1 if name=='column.tif'else 13,
        'height':HEIGHT if name=='cross-block.tif'else 1 if name=='row.tif'else 9,'grid':grid,'dtype':dtype,'nodata':nodata,
        'epsg':2222 if name=='foot.tif'else 2277 if name in('survey.tif','square-foot.tif')else 4326 if name=='geographic.tif'else 7415 if name=='compound.tif'else 0 if name=='missing_crs.tif'else 32650 if name=='unknown_unit.tif'else 3857}

def reference(name):
    from osgeo import osr
    spec=source_spec(name)
    if not spec['epsg']:return None
    crs=osr.SpatialReference();crs.ImportFromEPSG(spec['epsg'])
    if name=='unknown_unit.tif':crs.SetLinearUnits('unknown',.7)
    crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);return crs

def case_shape(name):
    spec=source_spec(CASES[name][0]);return spec['height'],spec['width']
def expected_cache_bytes():return sum(h*w*8 for h,w in(case_shape(n)for n in TARGETS))

def source_arrays(name):
    import numpy as np
    spec=source_spec(name);dtype={'CFloat64':'complex128','Float64':'float64'}.get(spec['dtype'],spec['dtype'].lower());shape=(spec['height'],spec['width'])
    values=np.full(shape,9,dtype=dtype);alpha=np.full(shape,255,dtype=dtype);mask=np.full(shape,255,np.uint8)
    targets=[(0,min(2,shape[1]-1)),(min(1,shape[0]-1),0),(shape[0]-1,shape[1]-1)]
    if name=='tall.tif':targets=[(0,1),(2,0),(shape[0]-1,shape[1]-1)]
    for row,col in targets:values[row,col]=1
    if name in('zero-negative.tif','uint32.tif'):
        values[targets[0]]=0;values[targets[1]]=4294967295 if name=='uint32.tif'else -3;values[targets[2]]=0
    if shape[0]>7 and shape[1]>8:
        values[3:5,3:5]=spec['nodata']
        if name=='nodata.tif':values[targets[0]]=spec['nodata']
        if name=='mask.tif':mask[targets[0]]=0;mask[6,:]=0
        if name=='alpha.tif':alpha[targets[0]]=0;alpha[6,:]=0;alpha[7,1]=-1;alpha[targets[1]]=128
    if name=='cross-block.tif':values[512,513]=1;values[200:207,200:207]=spec['nodata']
    if name=='complex.tif':values+=2j
    if name=='no_target.tif':values[:]=9
    if name=='invalid_targets.tif':alpha[:]=0
    return values,alpha,mask

def seed(root):
    import numpy as np
    from osgeo import gdal
    if any((root/n).exists()for n in SOURCES)or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite fixture data')
    for name in SOURCES:
        spec=source_spec(name);values,alpha,mask=source_arrays(name)
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),spec['width'],spec['height'],4,getattr(gdal,'GDT_'+spec['dtype']),['TILED=YES','COMPRESS=DEFLATE'])
        if spec['grid'] is not None:ds.SetGeoTransform(spec['grid'])
        crs=reference(name)
        if crs:ds.SetSpatialRef(crs)
        for i,data in enumerate((np.full_like(values,42),values,np.full_like(values,17),alpha),1):
            band=ds.GetRasterBand(i);band.WriteArray(data)
            if spec['dtype']=='Int64':band.SetNoDataValueAsInt64(spec['nodata'])
            elif spec['dtype']=='UInt64':band.SetNoDataValueAsUInt64(spec['nodata'])
            else:band.SetNoDataValue(spec['nodata'])
        ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        if name=='second-band.tif':ds.GetRasterBand(2).SetScale(17.);ds.GetRasterBand(2).SetOffset(500.)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            if ds.CreateMaskBand(gdal.GMF_PER_DATASET)!=gdal.CE_None:raise FixtureError('cannot create internal source mask')
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);band=ds=None
    verify_sources(root)
    return {'sources':{n:digest(root/n)for n in SOURCES}}

@lru_cache(maxsize=16)
def expected_inputs(name):
    import numpy as np
    source,_,_=CASES[name];values,alpha,mask=source_arrays(source)
    valid=np.isfinite(values)&(values!=source_spec(source)['nodata'])&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    return values,valid

@lru_cache(maxsize=16)
def expected_pixels(name):
    """Independent exhaustive target enumeration; Decimal final rounding, no EDT."""
    import numpy as np
    from decimal import Decimal,localcontext
    source,_,codes=CASES[name];spec=source_spec(source);values,valid=expected_inputs(name);targets=np.argwhere(valid&np.isin(values,codes));answer=np.full(values.shape,np.nan)
    with localcontext()as context:
        context.prec=200
        dx=Decimal.from_float(spec['grid'][1]);dy=Decimal.from_float(-spec['grid'][5]);units=Decimal.from_float(reference(source).GetLinearUnits())
        # Fixture offsets are bounded. Enumerating independent integer squared
        # offsets is exact for these binary-rational steps; Decimal rounds roots.
        from fractions import Fraction
        ratio=(Fraction(spec['grid'][1])/Fraction(-spec['grid'][5]))**2
        yy,xx=np.indices(values.shape);cost=None
        for row,col in targets:
            candidate=(xx-int(col))**2*ratio.numerator+(yy-int(row))**2*ratio.denominator
            cost=candidate if cost is None else np.minimum(cost,candidate)
        if cost is None:raise FixtureError('oracle requires a valid target')
        if source.startswith('square-'):
            answer=np.where(valid,np.sqrt(cost.astype(float))*float(spec['grid'][1]*reference(source).GetLinearUnits()),np.nan)
        else:
            factor=dy*dy/Decimal(ratio.denominator)*units*units
            rounded={int(v):float((Decimal(int(v))*factor).sqrt())for v in np.unique(cost[valid])}
            for value,distance in rounded.items():answer[valid&(cost==value)]=distance
    answer.flags.writeable=False;return answer

def assert_pixels(actual,expected,name):
    import numpy as np
    if not np.array_equal(actual,expected,equal_nan=True):raise FixtureError('complete independent rectangular proximity pixels differ')
    if not np.array_equal(actual==0,expected==0):raise FixtureError('legal zero changed')

CACHE_SCHEMA='addp.proximity_rectangular-oracle/v1'
CACHE_MARKER='.oracle-owner'
CONTROL_NAME='proximity_rectangular-oracle-control.json'
EXPECTED_EXPORT='.proximity_rectangular-expected'

@contextmanager
def elapsed(timings,key):
    start=time.monotonic()
    try:yield
    finally:timings[key]=timings.get(key,0)+round((time.monotonic()-start)*1000)

def encoded(value):return json.dumps(value,sort_keys=True,separators=(',',':')).encode()
def scope_hash(scope):return hashlib.sha256(encoded(scope)).hexdigest()
def oracle_path(scope):return Path(tempfile.gettempdir())/('addp-proximity_rectangular-oracle-'+scope_hash(scope))

def validate_scope(root,scope,*,sources):
    if (not isinstance(scope,dict) or set(scope)!={'schema','run_id','fixture_sha256','source_root','sources'}
        or scope['schema']!=CACHE_SCHEMA or not re.fullmatch('proximity-rectangular-[0-9]+-[0-9]+',scope.get('run_id',''))
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
    return archive_files(root/EXPECTED_EXPORT,artifacts,[n+'.npy' for n in TARGETS],previous,'proximity_rectangular-expectations')

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
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('invalid exact PROXIMITY_RECTANGULAR target inventory')
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
        if band.GetUnitType()!='metre' or band.GetScale() is not None or band.GetOffset() is not None or band.GetColorTable() is not None:raise FixtureError('focal output unit or source encoding differs')
        timings['pixels_mask_metadata_ms']+=round((time.monotonic()-pixel_start)*1000)
        with elapsed(timings,'cog_layout_ms'):
            if validate(ds,full_check=True)[1]:raise FixtureError('COG full layout validation failed')
        with elapsed(timings,'overviews_ms'):nearest_overviews(ds,actual)
        zeros=int(np.count_nonzero(np.isfinite(expected)&(expected==0)))
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,'overview_count':band.GetOverviewCount(),
            'distance_unit':band.GetUnitType(),'valid_pixels':int(np.isfinite(expected).sum()),'zero_pixels':zeros,'negative_pixels':int(np.count_nonzero(expected<0)),
            **{k:True for k in ('full_pixel_and_mask_verification','independent_scalar_oracle_verified','original_coverage_verified','actual_metre_nearest_verified','square_results_preserved','proximity_rectangular_verified','nearest_overviews_verified','explicit_selected_band_verified','legal_zero_verified')}}
        band=ds=None
    result['verification_timing_ms']=timings
    result['oracle_cache']={'scope_sha256':scope_hash(control['scope']),'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}
    return result

def archive_cogs(root,artifacts,verified,previous):
    return archive_files(root/'outputs',artifacts,list(verified['targets']),previous,'proximity_rectangular-cogs')

def verify_sources(root):
    import numpy as np
    from osgeo import gdal
    for name in SOURCES:
        spec=source_spec(name);values,alpha,mask=source_arrays(name);ds=gdal.Open(str(root/name));crs=reference(name)
        if ds is None or ds.GetDriver().ShortName!='GTiff'or ds.RasterCount!=4 or(ds.RasterXSize,ds.RasterYSize)!=(spec['width'],spec['height'])or ds.GetGeoTransform(can_return_null=True)!=spec['grid']or ds.GetFileList()!=[str(root/name)]:raise FixtureError('complete source dimensions/grid/inventory differ')
        if (crs is None)!=(ds.GetSpatialRef()is None)or(crs is not None and not ds.GetSpatialRef().IsSame(crs)):raise FixtureError('source CRS changed')
        for index,wanted in enumerate((np.full_like(values,42),values,np.full_like(values,17),alpha),1):
            band=ds.GetRasterBand(index)
            if band.DataType!=getattr(gdal,'GDT_'+spec['dtype'])or band.GetNoDataValue()!=spec['nodata']or not np.array_equal(band.ReadAsArray(),wanted,equal_nan=True)or not np.array_equal(band.GetMaskBand().ReadAsArray(),mask):raise FixtureError('complete source pixels/mask/type/NoData differ')
            if band.GetColorInterpretation()!=(gdal.GCI_AlphaBand if index==4 else gdal.GCI_GrayIndex if index==1 else gdal.GCI_Undefined):raise FixtureError('source band interpretation changed')
            if(band.GetScale(),band.GetOffset())!=((17.,500.)if name=='second-band.tif'and index==2 else(None,None)):raise FixtureError('source scale/offset changed')
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
    expected=os.environ.get('ADDP_ONLINE_PROXIMITY_RECTANGULAR_ORACLE_ANCHOR_SHA256')
    if expected is not None and digest(path)['sha256']!=expected:raise FixtureError('oracle anchor changed')
    return path,value

def main(action):
    secret=guard()
    if action in {'start','stop'}:return NFSExport('proximity-rectangular',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'proximity-rectangular-source.json').write_text(json.dumps(result));return result
    if action=='prepare-oracle':
        scope={'schema':CACHE_SCHEMA,'run_id':os.environ['ADDP_ONLINE_TEST_RUN_ID'],'source_root':str(root.resolve()),
               'fixture_sha256':digest(Path(__file__))['sha256'],'sources':json.loads((secret/'proximity-rectangular-source.json').read_text())['sources']}
        path=secret/CONTROL_NAME
        fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb')as f:f.write(encoded({'scope':scope,'arrays':None}))
        result=runtime_worker(action,root,[json.dumps(scope)])
        if result.get('scope')!=scope or set(result.get('arrays',{}))!=set(TARGETS):raise FixtureError('oracle prepare response differs')
        temp=path.with_suffix('.tmp')
        with temp.open('xb')as f:f.write(encoded({'scope':scope,'arrays':result['arrays']}))
        temp.chmod(0o600);temp.replace(path)
        baseline=runtime_worker('verify',root,[json.dumps({'scope':scope,'arrays':result['arrays']})])
        artifacts=Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']);sources=archive_files(root,artifacts,SOURCES,{},'proximity_rectangular-sources')
        expectations=archive_expectations(root,artifacts,{})
        history=secret/'proximity_rectangular-archive-history.json'
        with history.open('xb') as f:f.write(encoded({'sources':sources,'targets':{},'expected':expectations}))
        return {**baseline,'archived_sources':sources,'archived_cogs':{},'archived_expectations':expectations,'scope_sha256':scope_hash(scope),'anchor_sha256':digest(path)['sha256'],'expected_array_count':len(TARGETS),
                'cache_bytes':sum(v['size_bytes']for v in result['arrays'].values()),'timing_ms':result['timing_ms']}
    if action=='cleanup-oracle':
        if not(secret/CONTROL_NAME).exists():return {'oracle_cache_removed':True,'not_started':True}
        path,control=private_control(secret);result=runtime_worker(action,root,[json.dumps(control)])
        if result.get('oracle_cache_removed')is not True:raise FixtureError('oracle cleanup proof missing')
        path.unlink();return {'oracle_cache_removed':True}
    if action=='verify':
        if not os.environ.get('ADDP_ONLINE_PROXIMITY_RECTANGULAR_ORACLE_ANCHOR_SHA256'):raise FixtureError('oracle anchor is required')
        _,control=private_control(secret);timings={}
        with elapsed(timings,'worker_transport_ms'):result=runtime_worker('verify',root,[json.dumps(control),*sys.argv[2:]])
        with elapsed(timings,'source_receipt_ms'):
            if result['sources']!=json.loads((secret/'proximity-rectangular-source.json').read_text())['sources']:raise FixtureError('source file changed')
        with elapsed(timings,'archive_ms'):
            history=secret/'proximity_rectangular-archive-history.json'
            if not history.is_file():raise FixtureError('archive history missing')
            old=json.loads(history.read_bytes());artifacts=Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
            result['archived_sources']=archive_files(root,artifacts,SOURCES,old['sources'],'proximity_rectangular-sources')
            result['archived_cogs']=archive_cogs(root,artifacts,result,old['targets'])
            result['archived_expectations']=archive_expectations(root,artifacts,old['expected'])
            history.write_bytes(encoded({'sources':result['archived_sources'],'targets':result['archived_cogs'],'expected':result['archived_expectations']}))
        result['host_timing_ms']=timings;return result
    raise FixtureError('unknown fixture action')
if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1]);print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster PROXIMITY_RECTANGULAR fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
