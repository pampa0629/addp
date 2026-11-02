#!/usr/bin/env python3
"""Owned region labeling, independent pixel BFS and immutable COG/CSV evidence."""
from __future__ import annotations
import hashlib,io,json,os,re,subprocess,sys,tempfile,time,math,statistics
from collections import Counter
from fractions import Fraction
from contextlib import contextmanager
from functools import lru_cache
from pathlib import Path

SUITE='raster-region-group-persistence'
RUNTIME='addp-raster-region-group-runtime'
WIDTH,HEIGHT=131,129
# source, explicit connectivity, stage; CSV cases retain the labels as their frozen oracle.
CASES={
 'four.cog.tif':('values.tif',4,'direct'), 'eight.cog.tif':('values.tif',8,'direct'),
 'reclass-four.cog.tif':('values.tif',4,'reclassify'), 'reclass-eight.cog.tif':('values.tif',8,'reclassify'),
 'cross-four.cog.tif':('cross-block.tif',4,'direct'), 'cross-eight.cog.tif':('cross-block.tif',8,'direct'),
 'repeat-four.cog.tif':('values.tif',4,'direct'), 'late-merge.cog.tif':('late.tif',4,'direct'),
 'coverage.cog.tif':('coverage.tif',8,'direct'), 'rotated.cog.tif':('rotated.tif',4,'direct'),
 'geographic.cog.tif':('geographic.tif',8,'direct'), 'unlocated.cog.tif':('unlocated.tif',4,'direct'),
 'int64.cog.tif':('int64-safe.tif',4,'direct'), 'uint64.cog.tif':('uint64-safe.tif',8,'direct'),
 'invalid-four.cog.tif':('invalid.tif',4,'direct'), 'invalid-eight.cog.tif':('invalid.tif',8,'direct'),
 'stats-four.csv':('values.tif',4,'statistics'), 'stats-eight.csv':('values.tif',8,'statistics'),
}
FAILURES=('band_zero','band_high','band_bool','band_string','band_null','band_list',
 'connectivity_zero','connectivity_three','connectivity_bool','connectivity_string','connectivity_null','connectivity_list',
 'alpha','fractional','unsafe_positive','unsafe_negative','int64','uint64')
TARGETS=tuple(CASES)
SOURCES=('values.tif','cross-block.tif','late.tif','coverage.tif','rotated.tif','geographic.tif','unlocated.tif',
 'int64-safe.tif','uint64-safe.tif','invalid.tif','fractional.tif','unsafe-positive.tif','unsafe-negative.tif','int64.tif','uint64.tif')
COMPARISONS=(('four.cog.tif','repeat-four.cog.tif'),('invalid-four.cog.tif','invalid-eight.cog.tif'))
COLUMNS=('zone_value','total_count','valid_count','invalid_count','sum','min','max','mean','stddev')
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

def guard(env=os.environ):return NFSExport('region-group',command).guard(env)
def owned_root(secret):return NFSExport('region-group',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}

def source_spec(name):
    if name not in SOURCES:raise FixtureError('unknown exact region source')
    width,height=WIDTH,HEIGHT;grid=(500000.,2.,0.,3000000.,0.,-3.);epsg=3857
    if name=='cross-block.tif':width,height=519,515
    if name=='unlocated.tif':width,height=512,1;grid=None;epsg=0
    if name=='int64-safe.tif':width,height=1,512
    if name=='rotated.tif':grid=(500000.,2.,.25,3000000.,-.5,-3.)
    if name=='geographic.tif':grid=(110.,.01,0.,30.,0.,-.02);epsg=4326
    native='int64'if name.startswith('int64')else'uint64'if name.startswith('uint64')else'float64'
    nodata=-2**63 if native=='int64'else 2**64-1 if native=='uint64'else -2.**40
    return {'width':width,'height':height,'grid':grid,'epsg':epsg,'nodata':nodata,'dtype':native}

def reference(name):
    from osgeo import osr
    code=source_spec(name)['epsg']
    if not code:return None
    crs=osr.SpatialReference();crs.ImportFromEPSG(code);crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER);return crs

def case_shape(name):
    spec=source_spec(CASES[name][0]);return spec['height'],spec['width']

def expected_overview_count(name):
    height,width=case_shape(name);count=0
    while max(height,width)>128:width,height=max(1,width//2),max(1,height//2);count+=1
    return count

def expected_cache_bytes():return sum(h*w*8 for h,w in (case_shape(n) for n in TARGETS))

def source_arrays(name):
    import numpy as np
    spec=source_spec(name);shape=(spec['height'],spec['width']);values=np.full(shape,7,dtype=spec['dtype'])
    if min(shape)>30:
        values[4:12,4:12]=0;values[16:24,4:12]=1
        for i in range(5):values[40+i,40+i]=0
        values[60:66,60:66]=spec['nodata'];values[62,62]=0
        # Two same-class provisional runs join below: union root need not be the first anchor.
        values[80:84,20:31]=1;values[80:83,23:28]=7
    else:values.flat[::3]=0
    if name=='cross-block.tif':values[510:514,508:516]=0;values[511,511]=7;values[512,512]=spec['nodata']
    alpha=np.full(shape,255,dtype=spec['dtype']);mask=np.full(shape,255,np.uint8)
    if values.size>10:values.flat[1]=spec['nodata'];alpha.flat[2]=0;alpha.flat[3]=128;mask.flat[4]=0
    if spec['dtype']=='float64':
        if min(shape)>30:values[30,3]=np.nan;values[30,4]=np.inf;alpha[30,5]=np.nan;alpha[30,6]=-1;alpha[30,7]=np.inf
        values.flat[-1]=-7
    if name=='invalid.tif':values[:]=spec['nodata']
    if name=='fractional.tif':values.flat[-1]=.5
    if name=='unsafe-positive.tif':values.flat[-1]=2**53
    if name=='unsafe-negative.tif':values.flat[-1]=-2**53
    if name=='int64-safe.tif':values.flat[-1]=-(2**53-1)
    if name=='uint64-safe.tif':values.flat[-1]=2**53-1
    if name=='int64.tif':values.flat[-1]=-2**63+1
    if name=='uint64.tif':values.flat[-1]=2**64-2
    return values,alpha,mask

def seed(root):
    import numpy as np
    from osgeo import gdal
    if any((root/n).exists()for n in SOURCES)or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite fixture data')
    for name in SOURCES:
        spec=source_spec(name);values,alpha,mask=source_arrays(name);dtype={'float64':gdal.GDT_Float64,'int64':gdal.GDT_Int64,'uint64':gdal.GDT_UInt64}[spec['dtype']]
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),spec['width'],spec['height'],4,dtype,['TILED=YES','COMPRESS=DEFLATE'])
        if spec['grid']is not None:ds.SetGeoTransform(spec['grid'])
        crs=reference(name)
        if crs is not None:ds.SetSpatialRef(crs)
        for i,data in enumerate((np.full(values.shape,42 if spec['dtype']!='float64'else 42.25),values,np.full(values.shape,17),alpha),1):
            band=ds.GetRasterBand(i);band.WriteArray(data)
            if spec['dtype']=='int64':band.SetNoDataValueAsInt64(spec['nodata'])
            elif spec['dtype']=='uint64':band.SetNoDataValueAsUInt64(spec['nodata'])
            else:band.SetNoDataValue(spec['nodata'])
        ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        if name=='values.tif':ds.GetRasterBand(2).SetScale(17.);ds.GetRasterBand(2).SetOffset(500.)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            if ds.CreateMaskBand(gdal.GMF_PER_DATASET)!=gdal.CE_None:raise FixtureError('cannot create internal source mask')
        band=ds=None;ds=gdal.Open(str(root/name),gdal.GA_Update);ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
    verify_sources(root)
    return {'sources':{n:digest(root/n)for n in SOURCES}}

def class_rules():
    return [{'min':None,'max':0,'class':0},{'min':0,'max':10,'class':10},{'min':10,'max':None,'class':20}]

@lru_cache(maxsize=18)
def expected_inputs(name):
    import numpy as np
    source,connectivity,stage=CASES[name];values,alpha,mask=source_arrays(source)
    valid=np.isfinite(values)&(values!=source_spec(source)['nodata'])&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    values=values.astype(np.float64)
    if stage=='reclassify':values=np.where(values<0,0.,np.where(values<10,10.,20.))
    return values,valid


@lru_cache(maxsize=18)
def expected_pixels(name):
    """Independent per-pixel BFS in original row/column order; no product runs or union table."""
    import numpy as np
    from collections import deque
    values,valid=expected_inputs(name);height,width=values.shape;answer=np.full(values.shape,np.nan);number=0
    steps=[(-1,0),(0,-1),(0,1),(1,0)]
    if CASES[name][1]==8:steps +=[(-1,-1),(-1,1),(1,-1),(1,1)]
    for row in range(height):
        for col in range(width):
            if not valid[row,col]or np.isfinite(answer[row,col]):continue
            number+=1;answer[row,col]=number;queue=deque([(row,col)])
            while queue:
                y,x=queue.popleft()
                for dy,dx in steps:
                    yy,xx=y+dy,x+dx
                    if 0<=yy<height and 0<=xx<width and valid[yy,xx]and not np.isfinite(answer[yy,xx])and values[yy,xx]==values[row,col]:
                        answer[yy,xx]=number;queue.append((yy,xx))
    answer.flags.writeable=False;return answer

def assert_pixels(actual,expected,name):
    import numpy as np
    if not np.array_equal(actual,expected,equal_nan=True):raise FixtureError('complete exact region labels or mask differ')
    labels=np.unique(actual[np.isfinite(actual)])
    if not np.array_equal(labels,np.arange(1,len(labels)+1)):raise FixtureError('region labels are not contiguous positive integers')

def expected_records(name,labels=None):
    import numpy as np
    if labels is None:labels=expected_pixels(name)
    values,valid=expected_inputs(name);records=[]
    for label in np.unique(labels[np.isfinite(labels)]):
        members=values[labels==label];category=float(members[0]);count=len(members)
        if not np.all(members==category):raise FixtureError('oracle component mixes source classes')
        records.append(dict(zip(COLUMNS,(int(label),count,count,0,category*count,category,category,category,0.))))
    if sum(v['valid_count']for v in records)!=int(valid.sum()):raise FixtureError('oracle pixel conservation failed')
    return records

def validate_records(records,name,labels=None):
    import math
    expected=expected_records(name,labels)
    if not isinstance(records,list)or len(records)!=len(expected):raise FixtureError('complete CSV row count differs')
    for actual,wanted in zip(records,expected):
        if set(actual)!=set(COLUMNS):raise FixtureError('complete CSV columns differ')
        for key,value in wanted.items():
            try:given=float(actual[key])
            except(TypeError,ValueError,OverflowError):raise FixtureError('CSV value is not finite numeric')from None
            if not math.isfinite(given)or given!=value:raise FixtureError('complete exact CSV statistic differs')
    return True

def verify_csv(path,name,labels):
    import csv,io
    raw=path.read_bytes()
    try:
        reader=csv.DictReader(io.StringIO(raw.decode('utf-8')));records=list(reader)
        if reader.fieldnames!=list(COLUMNS):raise FixtureError('CSV ordered schema differs')
        validate_records(records,name,labels)
    except(UnicodeError,csv.Error)as error:raise FixtureError('invalid CSV bytes')from error
    return {**digest(path),'schema':list(COLUMNS),'row_count':len(records),'preview_rows':expected_records(name,labels)[:50],'complete_table_verified':True,'independent_bfs_oracle_verified':True,'pixel_conservation_verified':True}

CACHE_SCHEMA='addp.region-oracle/v1'
CACHE_MARKER='.oracle-owner'
CONTROL_NAME='region-oracle-control.json'
EXPECTED_EXPORT='.region-expected'

@contextmanager
def elapsed(timings,key):
    start=time.monotonic()
    try:yield
    finally:timings[key]=timings.get(key,0)+round((time.monotonic()-start)*1000)

def encoded(value):return json.dumps(value,sort_keys=True,separators=(',',':')).encode()
def scope_hash(scope):return hashlib.sha256(encoded(scope)).hexdigest()
def oracle_path(scope):return Path(tempfile.gettempdir())/('addp-region-oracle-'+scope_hash(scope))

def validate_scope(root,scope,*,sources):
    if (not isinstance(scope,dict) or set(scope)!={'schema','run_id','fixture_sha256','source_root','sources'}
        or scope['schema']!=CACHE_SCHEMA or not re.fullmatch('region-group-[0-9]+-[0-9]+',scope.get('run_id',''))
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
    return archive_files(root/EXPECTED_EXPORT,artifacts,[n+'.npy' for n in TARGETS],previous,'region-expectations')

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
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('invalid exact REGION target inventory')
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
        if name.endswith('.csv'):
            result['targets'][name]=verify_csv(path,name,expected);continue
        pixel_start=time.monotonic();ds=gdal.Open(str(path));spec=artifact_expectation(name)
        if ds is None or ds.GetDriver().ShortName!='GTiff' or ds.RasterCount!=1:raise FixtureError('expected one-band GeoTIFF')
        if (ds.RasterXSize,ds.RasterYSize,list(ds.GetGeoTransform(can_return_null=True) or ()))!=(spec['width'],spec['height'],spec['transform']):raise FixtureError('source grid changed')
        if spec['extent_srid']:
            crs=reference(CASES[name][0])
            if ds.GetSpatialRef() is None or not ds.GetSpatialRef().IsSame(crs):raise FixtureError('source CRS changed')
        elif ds.GetSpatialRef() is not None or ds.GetProjection():raise FixtureError('missing source CRS was fabricated')
        band=ds.GetRasterBand(1);actual=band.ReadAsArray()
        if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue()):raise FixtureError('Float64 or NaN NoData changed')
        assert_pixels(actual,expected,name)
        if not np.array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected)):raise FixtureError('complete pixel mask differs')
        if band.GetUnitType()!='' or band.GetScale() is not None or band.GetOffset() is not None or band.GetColorTable() is not None:raise FixtureError('region output unit or source encoding differs')
        timings['pixels_mask_metadata_ms']+=round((time.monotonic()-pixel_start)*1000)
        with elapsed(timings,'cog_layout_ms'):
            if validate(ds,full_check=True)[1]:raise FixtureError('COG full layout validation failed')
        with elapsed(timings,'overviews_ms'):nearest_overviews(ds,actual)
        zeros=int(np.count_nonzero(np.isfinite(expected)&(expected==0)))
        result['targets'][name]={**fact,'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,'overview_count':band.GetOverviewCount(),
            'valid_pixels':int(np.isfinite(expected).sum()),'zero_pixels':zeros,'negative_pixels':int(np.count_nonzero(expected<0)),
            **{k:True for k in ('full_pixel_and_mask_verification','independent_bfs_oracle_verified','original_coverage_verified','stable_anchor_order_verified','same_class_components_verified','nearest_overviews_verified','explicit_selected_band_verified','legal_zero_verified')}}
        band=ds=None
    comparisons=[]
    for left,right in COMPARISONS:
        if left in names and right in names:
            a=gdal.Open(str(root/'outputs'/left));b=gdal.Open(str(root/'outputs'/right))
            if not np.array_equal(a.ReadAsArray(),b.ReadAsArray(),equal_nan=True):raise FixtureError('actual repeated region labels differ')
            a=b=None;comparisons.append([left,right])
    result['comparisons']=comparisons
    result['verification_timing_ms']=timings
    result['oracle_cache']={'scope_sha256':scope_hash(control['scope']),'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}
    return result

def archive_outputs(root,artifacts,verified,previous):
    return archive_files(root/'outputs',artifacts,list(verified['targets']),previous,'region-outputs')

def verify_sources(root):
    import numpy as np
    from osgeo import gdal
    for name in SOURCES:
        spec=source_spec(name);values,alpha,mask=source_arrays(name);ds=gdal.Open(str(root/name));crs=reference(name)
        if ds is None or ds.GetDriver().ShortName!='GTiff'or ds.RasterCount!=4 or(ds.RasterXSize,ds.RasterYSize)!=(spec['width'],spec['height'])or ds.GetGeoTransform(can_return_null=True)!=spec['grid']or ds.GetFileList()!=[str(root/name)]:raise FixtureError('complete source dimensions/grid/inventory differ')
        if crs is not None:
            if ds.GetSpatialRef()is None or not ds.GetSpatialRef().IsSame(crs):raise FixtureError('source CRS changed')
        elif ds.GetProjection():raise FixtureError('missing source location fabricated')
        for index,wanted in enumerate((np.full(values.shape,42 if spec['dtype']!='float64'else 42.25),values,np.full(values.shape,17),alpha),1):
            band=ds.GetRasterBand(index);nodata=band.GetNoDataValueAsInt64()if spec['dtype']=='int64'else band.GetNoDataValueAsUInt64()if spec['dtype']=='uint64'else band.GetNoDataValue()
            if band.DataType!={'float64':gdal.GDT_Float64,'int64':gdal.GDT_Int64,'uint64':gdal.GDT_UInt64}[spec['dtype']]or nodata!=spec['nodata']or not np.array_equal(band.ReadAsArray(),wanted,equal_nan=True)or not np.array_equal(band.GetMaskBand().ReadAsArray(),mask):raise FixtureError('complete native source pixels/mask/type/NoData differ')
            if band.GetColorInterpretation()!=(gdal.GCI_AlphaBand if index==4 else gdal.GCI_GrayIndex if index==1 else gdal.GCI_Undefined):raise FixtureError('source band interpretation changed')
            if(band.GetScale(),band.GetOffset())!=((17.,500.)if name=='values.tif'and index==2 else(None,None)):raise FixtureError('source scale/offset changed')
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
    expected=os.environ.get('ADDP_ONLINE_REGION_GROUP_ORACLE_ANCHOR_SHA256')
    if expected is not None and digest(path)['sha256']!=expected:raise FixtureError('oracle anchor changed')
    return path,value

def main(action):
    secret=guard()
    if action in {'start','stop'}:return NFSExport('region-group',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'region-group-source.json').write_text(json.dumps(result));return result
    if action=='prepare-oracle':
        scope={'schema':CACHE_SCHEMA,'run_id':os.environ['ADDP_ONLINE_TEST_RUN_ID'],'source_root':str(root.resolve()),
               'fixture_sha256':digest(Path(__file__))['sha256'],'sources':json.loads((secret/'region-group-source.json').read_text())['sources']}
        path=secret/CONTROL_NAME
        fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb')as f:f.write(encoded({'scope':scope,'arrays':None}))
        result=runtime_worker(action,root,[json.dumps(scope)])
        if result.get('scope')!=scope or set(result.get('arrays',{}))!=set(TARGETS):raise FixtureError('oracle prepare response differs')
        temp=path.with_suffix('.tmp')
        with temp.open('xb')as f:f.write(encoded({'scope':scope,'arrays':result['arrays']}))
        temp.chmod(0o600);temp.replace(path)
        baseline=runtime_worker('verify',root,[json.dumps({'scope':scope,'arrays':result['arrays']})])
        artifacts=Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']);sources=archive_files(root,artifacts,SOURCES,{},'region-sources')
        expectations=archive_expectations(root,artifacts,{})
        history=secret/'region-archive-history.json'
        with history.open('xb') as f:f.write(encoded({'sources':sources,'targets':{},'expected':expectations}))
        return {**baseline,'archived_sources':sources,'archived_outputs':{},'archived_expectations':expectations,'scope_sha256':scope_hash(scope),'anchor_sha256':digest(path)['sha256'],'expected_array_count':len(TARGETS),
                'cache_bytes':sum(v['size_bytes']for v in result['arrays'].values()),'timing_ms':result['timing_ms']}
    if action=='cleanup-oracle':
        if not(secret/CONTROL_NAME).exists():return {'oracle_cache_removed':True,'not_started':True}
        path,control=private_control(secret);result=runtime_worker(action,root,[json.dumps(control)])
        if result.get('oracle_cache_removed')is not True:raise FixtureError('oracle cleanup proof missing')
        path.unlink();return {'oracle_cache_removed':True}
    if action=='verify':
        if not os.environ.get('ADDP_ONLINE_REGION_GROUP_ORACLE_ANCHOR_SHA256'):raise FixtureError('oracle anchor is required')
        _,control=private_control(secret);timings={}
        with elapsed(timings,'worker_transport_ms'):result=runtime_worker('verify',root,[json.dumps(control),*sys.argv[2:]])
        with elapsed(timings,'source_receipt_ms'):
            if result['sources']!=json.loads((secret/'region-group-source.json').read_text())['sources']:raise FixtureError('source file changed')
        with elapsed(timings,'archive_ms'):
            history=secret/'region-archive-history.json'
            if not history.is_file():raise FixtureError('archive history missing')
            old=json.loads(history.read_bytes());artifacts=Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
            result['archived_sources']=archive_files(root,artifacts,SOURCES,old['sources'],'region-sources')
            result['archived_outputs']=archive_outputs(root,artifacts,result,old['targets'])
            result['archived_expectations']=archive_expectations(root,artifacts,old['expected'])
            history.write_bytes(encoded({'sources':result['archived_sources'],'targets':result['archived_outputs'],'expected':result['archived_expectations']}))
        result['host_timing_ms']=timings;return result
    raise FixtureError('unknown fixture action')
if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1]);print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster REGION fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
