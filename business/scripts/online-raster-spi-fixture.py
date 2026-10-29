#!/usr/bin/env python3
"""Owned two-source SPI fixtures, independent continued-fraction oracle, immutable COGs."""
from __future__ import annotations
import hashlib,io,json,math,os,re,subprocess,sys,tempfile,time
from contextlib import contextmanager
from functools import lru_cache
from fractions import Fraction
from pathlib import Path

SUITE='raster-spi-persistence'
RUNTIME='addp-raster-spi-runtime'
WIDTH,HEIGHT=519,515
CASES={
 'degree.cog.tif':('area.tif','slope-degree.tif','degree','direct'),
 'percent.cog.tif':('area.tif','slope-percent.tif','percent','direct'),
 'foot.cog.tif':('area-foot.tif','slope-foot.tif','degree','direct'),
 'us-foot.cog.tif':('area-us.tif','slope-us.tif','percent','direct'),
 'tiny.cog.tif':('area.tif','slope-tiny.tif','degree','direct'),
 'classes.cog.tif':('area.tif','slope-classes.tif','percent','reclassify'),
 'dem-degree.cog.tif':('dem.tif',None,'degree','dem'),
 'dem-percent.cog.tif':('dem.tif',None,'percent','dem'),
 'filled.cog.tif':('bowl.tif',None,'degree','fill'),
 'int64.cog.tif':('area-int64.tif','slope-degree.tif','degree','direct'),
 'uint64.cog.tif':('area-uint64.tif','slope-degree.tif','degree','direct'),
 'invalid.cog.tif':('area-invalid.tif','slope-degree.tif','degree','direct'),
 'disjoint.cog.tif':('area-disjoint.tif','slope-disjoint.tif','degree','direct'),
 'zero-slope.cog.tif':('area.tif','slope-zero.tif','degree','direct'),
}
TARGETS=tuple(CASES)
EXTRA_SOURCES=('area-no-unit.tif','area-wrong-unit.tif','slope-conflict.tif','area-zero.tif','area-negative.tif','slope-negative.tif','slope-90.tif','slope-size.tif','slope-origin.tif','slope-crs.tif','area-rectangular.tif','area-rotated.tif','area-south-up.tif','area-geographic.tif','area-no-crs.tif','area-no-grid.tif','area-complex.tif','area-scale.tif','area-offset.tif','slope-scale.tif','slope-offset.tif')
SOURCES=tuple(dict.fromkeys([n for c in CASES.values()for n in c[:2]if n]+list(EXTRA_SOURCES)))
class FixtureError(RuntimeError):pass

def command(args,**kwargs):
    try:return subprocess.run(args,check=True,capture_output=True,text=True,timeout=150,**kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:
        detail=(error.stderr or '').strip()[-4000:] or 'no stderr diagnostic'
        raise FixtureError(f'fixture command failed (exit {error.returncode}): {detail}') from error

if sys.argv[1:2]!=['--worker']:
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport,FixtureError,run_runtime_worker

def guard(env=os.environ):return NFSExport('spi',command).guard(env)
def owned_root(secret):return NFSExport('spi',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}

def source_spec(name):
    if name not in SOURCES:raise FixtureError('unknown exact SPI source')
    epsg,horizontal=(2222,.3048)if name in('area-foot.tif','slope-foot.tif')else(2277,1200/3937)if name in('area-us.tif','slope-us.tif')else(32650,1.)if name=='slope-crs.tif'else(4326,1.)if name=='area-geographic.tif'else(3857,1.)
    grid=[500000.,2./horizontal,0.,3000000.,0.,-2./horizontal]
    if name=='slope-origin.tif':grid[0]+=1
    if name=='area-rectangular.tif':grid[5]=-3.
    if name=='area-rotated.tif':grid[2]=.25
    if name=='area-south-up.tif':grid[5]=2.
    integer=name in('area-int64.tif','area-uint64.tif')
    unit='m2'if name.startswith('area')else'metre'if name in('dem.tif','bowl.tif')else'percent'if name in('slope-percent.tif','slope-us.tif','slope-conflict.tif')else''if name=='slope-classes.tif'else'degree'
    if name=='area-no-unit.tif':unit=''
    if name=='area-wrong-unit.tif':unit='foot'
    return {'width':WIDTH-1 if name=='slope-size.tif'else WIDTH,'height':HEIGHT,'grid':tuple(grid),'epsg':epsg,'horizontal':horizontal,'unit':unit,'integer':integer,
            'nodata':-2**63 if name=='area-int64.tif'else 2**64-1 if name=='area-uint64.tif'else-2.**40}

def grid_for(name):return source_spec(name)['grid']

def source_arrays(name):
    import numpy as np
    spec=source_spec(name);rows,cols=np.indices((spec['height'],spec['width']))
    if name.startswith('area'):
        values=np.power(2.,(rows+cols)%5-1)
        if spec['integer']:values=(((rows%7)*13+cols%13).astype(object)+(2**60 if name=='area-int64.tif'else 2**63)).astype(np.int64 if name=='area-int64.tif'else np.uint64)
    elif name in('dem.tif','bowl.tif'):
        values=2000.-2.*cols if name=='dem.tif'else np.where((rows==0)|(cols==0)|(rows==HEIGHT-1)|(cols==WIDTH-1),10.,2.)
    elif name=='slope-tiny.tif':values=np.asarray([1e-12,1.,30.,45.,60.,89.])[((rows+cols)%6)]
    elif name=='slope-classes.tif':values=((rows+cols)%3).astype(float)
    else:
        values=np.where((rows+cols)%7==0,0.,100. if spec['unit']=='percent'else 45.)
    alpha=np.full(values.shape,255.);mask=np.full(values.shape,255,np.uint8)
    if name not in('dem.tif','bowl.tif'):
        area=name.startswith('area');points=((510,511),(508,512),(512,509),(509,510),(511,513),(513,512))if area else((510,510),(508,511),(512,508),(509,509),(511,512),(513,511))
        values[points[0]]=spec['nodata'];values[points[1]]=spec['nodata']if spec['integer']else np.inf;values[points[2]]=spec['nodata']if spec['integer']else np.nan
        alpha[points[3]]=0;alpha[points[4]]=0 if spec['integer']else np.nan;mask[points[5]]=0;alpha[500,500]=128.
    if name=='area-invalid.tif':values[:]=spec['nodata']
    elif name=='area-disjoint.tif':values[:,WIDTH//2:]=spec['nodata']
    elif name=='slope-disjoint.tif':values[:,:WIDTH//2]=spec['nodata']
    elif name=='slope-zero.tif':values[:]=0.
    elif name=='area-zero.tif':values[5,6]=0.
    elif name=='area-negative.tif':values[510,510]=-1. # Other source invalid here.
    elif name=='slope-negative.tif':values[510,511]=-1. # Other source invalid here.
    elif name=='slope-90.tif':values[5,6]=90.
    return values,alpha,mask

def seed(root):
    import numpy as np
    from osgeo import gdal,osr
    if any((root/n).exists()for n in SOURCES)or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite fixture data')
    for name in SOURCES:
        spec=source_spec(name);values,alpha,mask=source_arrays(name)
        native=gdal.GDT_UInt64 if name=='area-uint64.tif'else gdal.GDT_Int64 if spec['integer']else gdal.GDT_CFloat64 if name=='area-complex.tif'else gdal.GDT_Float64
        dtype=np.uint64 if name=='area-uint64.tif'else np.int64 if spec['integer']else np.complex128 if name=='area-complex.tif'else np.float64
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),spec['width'],spec['height'],4,native,['TILED=YES','COMPRESS=DEFLATE'])
        crs=osr.SpatialReference();crs.ImportFromEPSG(spec['epsg'])
        if name!='area-no-crs.tif':ds.SetSpatialRef(crs)
        if name!='area-no-grid.tif':ds.SetGeoTransform(spec['grid'])
        for i,data in enumerate((np.full(values.shape,1),values,np.full(values.shape,17),alpha),1):
            b=ds.GetRasterBand(i);b.WriteArray(np.asarray(data,dtype=dtype))
            if name=='area-uint64.tif':b.SetNoDataValueAsUInt64(spec['nodata'])
            elif name=='area-int64.tif':b.SetNoDataValueAsInt64(spec['nodata'])
            else:b.SetNoDataValue(spec['nodata'])
            b=None
        ds.GetRasterBand(2).SetUnitType(spec['unit']);ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        if name in('area-scale.tif','slope-scale.tif'):ds.GetRasterBand(2).SetScale(2.)
        if name in('area-offset.tif','slope-offset.tif'):ds.GetRasterBand(2).SetOffset(1.)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            if ds.CreateMaskBand(gdal.GMF_PER_DATASET)!=gdal.CE_None:raise FixtureError('cannot create internal source mask')
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
        ds=gdal.Open(str(root/name));b=ds.GetRasterBand(2)
        if ds.GetFileList()!=[str(root/name)]or not np.array_equal(b.ReadAsArray(),np.asarray(values,dtype=dtype),equal_nan=True)or not np.array_equal(b.GetMaskBand().ReadAsArray(),mask):raise FixtureError('native source bytes or coverage changed')
        b=ds=None
    return {'sources':{n:digest(root/n)for n in SOURCES}}

@lru_cache(maxsize=14)
def expected_inputs(name):
    import numpy as np
    a,s,unit,stage=CASES[name]
    if stage in('dem','fill'):
        rows,cols=np.indices((HEIGHT,WIDTH));areas=4*(cols+1);slopes=np.full((HEIGHT,WIDTH),45. if unit=='degree'else 100.)
        valid=np.ones((HEIGHT,WIDTH),bool);valid[[0,-1],:]=False;valid[:,[0,-1]]=False
        if stage=='fill':slopes[:]=0.
        return areas,slopes,valid
    areas,alpha,mask=source_arrays(a);spec=source_spec(a)
    valid=np.isfinite(areas)&(areas!=spec['nodata'])&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    slopes,alpha,mask=source_arrays(s);spec=source_spec(s)
    valid&=np.isfinite(slopes)&(slopes!=spec['nodata'])&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    if stage=='reclassify':slopes=np.where(slopes==1,100.,np.where(slopes==2,200.,0.))
    return areas,slopes,valid

def measurement_width(root,name):
    from osgeo import gdal,osr
    source=CASES[name][0];spec=source_spec(source);ds=gdal.Open(str(root/source));grid=ds.GetGeoTransform();crs=ds.GetSpatialRef()
    nominal=osr.SpatialReference();nominal.ImportFromEPSG(spec['epsg'])
    if tuple(grid)!=spec['grid']or crs is None or not crs.IsSame(nominal):raise FixtureError('source measurement grid/CRS changed')
    width=Fraction(grid[1])*Fraction(crs.GetLinearUnits());ds=None
    return width


@lru_cache(maxsize=28)
def expected_pixels(name,width=Fraction(2)):
    """Independent 200-digit Lambert continued fraction, no product/GDAL terrain calls."""
    import numpy as np
    from decimal import Decimal,localcontext
    areas,slopes,valid=expected_inputs(name);complete=valid;answer=np.full((HEIGHT,WIDTH),np.nan)
    if not complete.any():return answer
    with localcontext()as context:
        context.prec=200;d=Decimal(width.numerator)/Decimal(width.denominator);tangents={};indices={};out=[]
        for a,b in zip(areas[complete].tolist(),slopes[complete].tolist()):
            if b not in tangents:
                if CASES[name][2]=='percent':tangents[b]=Decimal(b)/100
                else:
                    pi=Decimal('3.14159265358979323846264338327950288419716939937510582097494459230781640628620899862803482534211706798214808651328230664709384460955058223172535940812848111745028410270193852110555964462294895493038196')
                    x=Decimal(b)*pi/180;denominator=Decimal(601)
                    for odd in range(599,0,-2):denominator=Decimal(odd)-x*x/denominator
                    tangents[b]=x/denominator
                    if b==45:tangents[b]=Decimal(1) # Analytic identity; never a near-zero cutoff.
            key=(a,b)
            if key not in indices:indices[key]=float(Decimal(a)/d*tangents[b])
            out.append(indices[key])
        answer[complete]=out
    answer.flags.writeable=False;return answer

CACHE_SCHEMA='addp.spi-oracle/v1'
CACHE_MARKER='.oracle-owner'
CONTROL_NAME='spi-oracle-control.json'

@contextmanager
def elapsed(timings,key):
    start=time.monotonic()
    try:yield
    finally:timings[key]=timings.get(key,0)+round((time.monotonic()-start)*1000)

def encoded(value):return json.dumps(value,sort_keys=True,separators=(',',':')).encode()
def scope_hash(scope):return hashlib.sha256(encoded(scope)).hexdigest()
def oracle_path(scope):return Path(tempfile.gettempdir())/('addp-spi-oracle-'+scope_hash(scope))

def validate_scope(root,scope,*,sources):
    if (not isinstance(scope,dict) or set(scope)!={'schema','run_id','fixture_sha256','source_root','sources'}
        or scope['schema']!=CACHE_SCHEMA or not re.fullmatch('spi-[0-9]+-[0-9]+',scope.get('run_id',''))
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
        with elapsed(timings,'expected_build_ms'):expected=expected_pixels(name,measurement_width(root,name))
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
    source=CASES[name][0];spec=source_spec(source);grid=spec['grid'];a,b,c,d,e,f=grid
    corners=[(a+b*x+c*y,d+e*x+f*y)for x,y in ((0,0),(WIDTH,0),(0,HEIGHT),(WIDTH,HEIGHT))]
    return {'width':WIDTH,'height':HEIGHT,'band_count':1,'source_crs':'EPSG:'+str(spec['epsg']),'extent_srid':spec['epsg'],'transform':list(grid),'band_nodata':None,
            'extent':[min(p[0]for p in corners),min(p[1]for p in corners),max(p[0]for p in corners),max(p[1]for p in corners)]}


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
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('invalid exact SPI target inventory')
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
        if not np.array_equal(actual,expected,equal_nan=True):raise FixtureError('complete exact SPI pixels differ')
        if not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected)):raise FixtureError('complete pixel mask differs')
        if band.GetUnitType()!='metre' or band.GetScale() is not None or band.GetOffset() is not None or band.GetColorTable() is not None:raise FixtureError('metre unit or source encoding differs')
        timings['pixels_mask_metadata_ms']+=round((time.monotonic()-pixel_start)*1000)
        with elapsed(timings,'cog_layout_ms'):
            if validate(ds,full_check=True)[1]:raise FixtureError('COG full layout validation failed')
        with elapsed(timings,'overviews_ms'):nearest_overviews(ds,expected)
        zeros=int(np.count_nonzero(np.isfinite(expected)&(expected==0)))
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,'overview_count':band.GetOverviewCount(),
            'valid_pixels':int(np.isfinite(expected).sum()),'zero_pixels':zeros,'negative_pixels':int(np.count_nonzero(expected<0)),
            **{k:True for k in ('full_pixel_and_mask_verification','independent_decimal_oracle_verified','joint_coverage_verified','fixed_area_over_width_verified','metre_unit_verified','nearest_overviews_verified','no_slope_threshold_verified','legal_zero_verified')}}
        band=ds=None
    result['verification_timing_ms']=timings
    result['oracle_cache']={'scope_sha256':scope_hash(control['scope']),'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}
    return result

def archive_cogs(root,artifacts,verified):
    """Host-side byte copy after Runtime verification; no image re-encoding."""
    names=set(verified['targets']);directory=artifacts/'spi-cogs';manifest_path=artifacts/'spi-cogs-manifest.json'
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
        manifest[name]={'path':'spi-cogs/'+name,**digest(path)}
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
    expected=os.environ.get('ADDP_ONLINE_SPI_ORACLE_ANCHOR_SHA256')
    if expected is not None and digest(path)['sha256']!=expected:raise FixtureError('oracle anchor changed')
    return path,value

def main(action):
    secret=guard()
    if action in {'start','stop'}:return NFSExport('spi',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'spi-source.json').write_text(json.dumps(result));return result
    if action=='prepare-oracle':
        scope={'schema':CACHE_SCHEMA,'run_id':os.environ['ADDP_ONLINE_TEST_RUN_ID'],'source_root':str(root.resolve()),
               'fixture_sha256':digest(Path(__file__))['sha256'],'sources':json.loads((secret/'spi-source.json').read_text())['sources']}
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
        if not os.environ.get('ADDP_ONLINE_SPI_ORACLE_ANCHOR_SHA256'):raise FixtureError('oracle anchor is required')
        _,control=private_control(secret);timings={}
        with elapsed(timings,'worker_transport_ms'):result=runtime_worker('verify',root,[json.dumps(control),*sys.argv[2:]])
        with elapsed(timings,'source_receipt_ms'):
            if result['sources']!=json.loads((secret/'spi-source.json').read_text())['sources']:raise FixtureError('source file changed')
        with elapsed(timings,'archive_ms'):result['archived_cogs']=archive_cogs(root,Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']),result)
        result['host_timing_ms']=timings;return result
    raise FixtureError('unknown fixture action')
if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1]);print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster SPI fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
