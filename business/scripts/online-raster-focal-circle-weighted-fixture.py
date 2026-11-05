#!/usr/bin/env python3
"""Owned focal circle, independent scalar oracle and immutable COG evidence."""
from __future__ import annotations
import hashlib,io,json,os,re,subprocess,sys,tempfile,time,math
from contextlib import contextmanager
from functools import lru_cache
from pathlib import Path

SUITE='raster-focal-circle-weighted-persistence'
RUNTIME='addp-raster-focal-circle-weighted-runtime'
WIDTH,HEIGHT=131,129
# source, method, metre radius, weight matrix name, upstream stage
CASES = {
    'north-mean.cog.tif': ('north.tif', 'weighted_mean', 4., 'north', 'direct'),
    'north-stddev.cog.tif': ('north.tif', 'weighted_stddev', 4., 'north', 'direct'),
    'north-sum.cog.tif': ('north.tif', 'weighted_sum', 4., 'north', 'direct'),
    'rotated-mean.cog.tif': ('rotated-values.tif', 'weighted_mean', 10., 'rotated', 'direct'),
    'rotated-stddev.cog.tif': ('rotated-values.tif', 'weighted_stddev', 10., 'rotated', 'direct'),
    'rotated-sum.cog.tif': ('cross-block.tif', 'weighted_sum', 10., 'rotated', 'direct'),
    'ones-mean.cog.tif': ('control.tif', 'weighted_mean', 4., 'ones', 'direct'),
    'control-mean.cog.tif': ('control.tif', 'mean', 4., None, 'direct'),
    'ones-stddev.cog.tif': ('control.tif', 'weighted_stddev', 4., 'ones', 'direct'),
    'control-stddev.cog.tif': ('control.tif', 'stddev', 4., None, 'direct'),
    'ones-sum.cog.tif': ('control.tif', 'weighted_sum', 4., 'ones', 'direct'),
    'control-sum.cog.tif': ('control.tif', 'sum', 4., None, 'direct'),
    'foot.cog.tif': ('foot.tif', 'weighted_mean', .4, 'north', 'direct'),
    'survey.cog.tif': ('survey.tif', 'weighted_stddev', .4, 'north', 'direct'),
    'center.cog.tif': ('rotated-values.tif', 'weighted_sum', 1., 'center', 'direct'),
    'reclassified.cog.tif': ('north.tif', 'weighted_stddev', 4., 'north', 'reclassify'),
    'invalid.cog.tif': ('invalid.tif', 'weighted_sum', 10., 'rotated', 'direct'),
    'below.cog.tif': ('north.tif', 'weighted_mean', 3.9999999999999996, 'below', 'direct'),
}
KERNELS = {
    'north': [[0, 2, 0], [1, 0, 3], [0, 0, 0]],
    'rotated': [[0, 0, 0, 0, 0], [0, 0, .5, 0, 0], [2, 0, 0, 3, 1],
                [0, 0, 4, 0, 0], [0, 0, 0, 0, 0]],
    'ones': [[0, 1, 0], [1, 1, 1], [0, 1, 0]],
    'center': [[2]],
    'below': [[0, 0, 0], [1, 0, 3], [0, 0, 0]],
}
FAILURES = (
    'weight_missing', 'weight_null', 'weight_scalar', 'weight_flat', 'weight_ragged',
    'weight_size', 'weight_zero', 'weight_bool', 'weight_string', 'weight_null_element',
    'weight_object_element', 'weight_negative', 'weight_outside', 'weight_below',
    'method', 'unused_weights', 'unused_percentile', 'circle_window', 'circle_window_null',
    'radius_missing', 'radius_null', 'radius_zero', 'radius_negative', 'radius_bool',
    'geographic', 'compound', 'missing_crs', 'missing_grid', 'shear', 'mirror',
    'x_oversize', 'y_oversize', 'band_zero', 'band_high', 'band_fraction', 'alpha',
)
TARGETS = tuple(CASES)
SOURCES = (
    'north.tif', 'rotated-values.tif', 'cross-block.tif', 'control.tif', 'foot.tif',
    'survey.tif', 'invalid.tif', 'geographic.tif', 'compound.tif', 'missing_crs.tif',
    'missing_grid.tif', 'shear.tif', 'mirror.tif', 'x_oversize.tif', 'y_oversize.tif',
)
COMPARISONS = (
    ('ones-mean.cog.tif', 'control-mean.cog.tif'),
    ('ones-stddev.cog.tif', 'control-stddev.cog.tif'),
    ('ones-sum.cog.tif', 'control-sum.cog.tif'),
)
BOUNDARY_PAIR = ('north-mean.cog.tif', 'below.cog.tif')
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

def guard(env=os.environ):return NFSExport('focal-circle-weighted',command).guard(env)
def owned_root(secret):return NFSExport('focal-circle-weighted',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}

def source_spec(name):
    if name not in SOURCES:
        raise FixtureError('unknown weighted circle source')
    width, height = (519, 515) if name == 'cross-block.tif' else (WIDTH, HEIGHT)
    grid = (500000., 3., 0., 3000000., 0., -4.)
    epsg = {'foot.tif': 2222, 'survey.tif': 2277, 'geographic.tif': 4326,
            'compound.tif': 7415, 'missing_crs.tif': 0}.get(name, 3857)
    if name in ('rotated-values.tif', 'cross-block.tif', 'invalid.tif'):
        grid = (500000., 3., 8., 3000000., 4., -6.)
    elif name == 'control.tif':
        grid = (500000., 0., 4., 3000000., 3., 0.)
    elif name in ('foot.tif', 'survey.tif'):
        grid = (500000., 0., 1., 3000000., 1., 0.)
    elif name == 'missing_grid.tif':
        grid = None
    elif name == 'shear.tif':
        grid = (0., 1., 2.**-40, 0., 0., -1.)
    elif name == 'mirror.tif':
        grid = (0., 3., -4., 0., 4., 3.)
    elif name == 'x_oversize.tif':
        grid = (0., 0., 4., 0., 1., 0.)
    elif name == 'y_oversize.tif':
        grid = (0., 0., 1., 0., 4., 0.)
    return {'width': width, 'height': height, 'grid': grid, 'epsg': epsg, 'nodata': -9999.}


def reference(name):
    from osgeo import osr
    epsg = source_spec(name)['epsg']
    if not epsg:
        return None
    crs = osr.SpatialReference()
    crs.ImportFromEPSG(epsg)
    crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    return crs


def case_shape(name):
    spec=source_spec(CASES[name][0]);return spec['height'],spec['width']

def expected_overview_count(name):
    height,width=case_shape(name);count=0
    while max(height,width)>128:width,height=max(1,width//2),max(1,height//2);count+=1
    return count

def expected_cache_bytes():return sum(h*w*8 for h,w in (case_shape(n) for n in TARGETS))

def source_arrays(name):
    import numpy as np
    spec = source_spec(name)
    y, x = np.indices((spec['height'], spec['width']))
    values = ((x + 2*y) % 7 - 3).astype(np.float64)
    alpha = np.full(values.shape, 255.)
    mask = np.full(values.shape, 255, np.uint8)
    values.flat[1] = spec['nodata']
    alpha.flat[2] = 0.
    alpha.flat[3] = 128.
    mask.flat[4] = 0
    values[10:17, 10:17] = 0.
    values[20:27, 20:27] = spec['nodata']
    values[23, 23] = 0.
    if name == 'north.tif':
        values[61, 62] = 8.
        values[62, 61:64] = [-4., 0., 4.]
    elif name == 'control.tif':
        # Isolated horizontal pairs have exactly representable mean and population stddev.
        values[:] = spec['nodata']
        for row in range(3, spec['height'], 5):
            values[row, 10:12] = [0., 4.]
            values[row, 20:22] = [-4., 4.]
            values[row, 30:32] = [2., 2.]
    elif name == 'invalid.tif':
        values[:] = spec['nodata']
    elif name == 'cross-block.tif':
        values[508, 508] = spec['nodata']
        alpha[509, 509] = 0.
        mask[510, 510] = 0
    return values, alpha, mask


def seed(root):
    import numpy as np
    from osgeo import gdal
    if any((root/n).exists()for n in SOURCES)or list((root/'outputs').iterdir()):raise FixtureError('refusing to overwrite fixture data')
    for name in SOURCES:
        spec=source_spec(name);values,alpha,mask=source_arrays(name)
        ds=gdal.GetDriverByName('GTiff').Create(str(root/name),spec['width'],spec['height'],4,gdal.GDT_Float64,['TILED=YES','COMPRESS=DEFLATE'])
        if spec['grid'] is not None:ds.SetGeoTransform(spec['grid'])
        crs=reference(name)
        if crs is not None:ds.SetSpatialRef(crs)
        for i,data in enumerate((np.full(values.shape,42.25),values,np.full(values.shape,17),alpha),1):
            band=ds.GetRasterBand(i);band.WriteArray(data);band.SetNoDataValue(spec['nodata'])
        ds.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        if name=='north.tif':ds.GetRasterBand(2).SetScale(17.);ds.GetRasterBand(2).SetOffset(500.)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
            if ds.CreateMaskBand(gdal.GMF_PER_DATASET)!=gdal.CE_None:raise FixtureError('cannot create internal source mask')
        band=ds=None
        ds=gdal.Open(str(root/name),gdal.GA_Update);ds.GetRasterBand(1).GetMaskBand().WriteArray(mask);ds=None
    verify_sources(root)
    return {'sources':{n:digest(root/n)for n in SOURCES}}

def class_rules():
    return [{'min':None,'max':0,'class':0},{'min':0,'max':10,'class':10},{'min':10,'max':None,'class':20}]

def expected_inputs(name):
    import numpy as np
    source, _, _, _, stage = CASES[name]
    values, alpha, mask = source_arrays(source)
    valid = np.isfinite(values) & (values != source_spec(source)['nodata']) & np.isfinite(alpha) & (alpha > 0) & (mask != 0)
    if stage == 'reclassify':
        values = np.where(values < 0, 0., np.where(values < 10, 10., 20.))
    return values, valid


def scalar_value(samples, method):
    from decimal import Decimal
    if not samples:
        return math.nan
    pairs = [(Decimal.from_float(value), weight) for value, weight in samples]
    total = sum(value * weight for value, weight in pairs)
    if method in ('sum', 'weighted_sum'):
        return float(total)
    mass = sum(weight for _, weight in pairs)
    mean = total / mass
    if method in ('mean', 'weighted_mean'):
        return float(mean)
    return float((sum(weight * (value - mean)**2 for value, weight in pairs) / mass).sqrt())


@lru_cache(maxsize=18)
def expected_pixels(name):
    # Independently enumerate grid centres with Decimal and reduce positional weights.
    from decimal import Decimal, localcontext
    import numpy as np
    source, method, radius, kernel, _ = CASES[name]
    values, valid = expected_inputs(name)
    height, width = values.shape
    answer = np.full(values.shape, np.nan)
    known = {}
    with localcontext() as context:
        context.prec = 2000
        d = Decimal.from_float
        _, column_x, row_x, _, column_y, row_y = map(d, source_spec(source)['grid'])
        units = d(reference(source).GetLinearUnits())
        radius_squared = d(radius)**2
        offsets = [(dr, dc) for dr in range(-2, 3) for dc in range(-2, 3)
                   if ((dc*column_x + dr*row_x)*units)**2
                   + ((dc*column_y + dr*row_y)*units)**2 <= radius_squared]
        if kernel is None:
            positions = [(dr, dc, Decimal(1)) for dr, dc in offsets]
        else:
            matrix = KERNELS[kernel]
            reach = max(max(abs(dr), abs(dc)) for dr, dc in offsets)
            if len(matrix) != 2*reach+1 or any(len(row) != len(matrix) for row in matrix):
                raise FixtureError('fixture weight matrix does not match independent derived square')
            members = set(offsets)
            if any(value and (i-reach, j-reach) not in members
                   for i, row in enumerate(matrix) for j, value in enumerate(row)):
                raise FixtureError('fixture includes positive weight outside the independent circle')
            positions = [(dr, dc, d(float(matrix[dr+reach][dc+reach])))
                         for dr, dc in offsets if matrix[dr+reach][dc+reach] > 0]
        for row, col in zip(*np.nonzero(valid)):
            samples = tuple((float(values[row+dr, col+dc]), weight) for dr, dc, weight in positions
                            if 0 <= row+dr < height and 0 <= col+dc < width and valid[row+dr, col+dc])
            if samples not in known:
                known[samples] = scalar_value(samples, method)
            answer[row, col] = known[samples]
    answer.flags.writeable = False
    return answer


def assert_pixels(actual, expected, name):
    import numpy as np
    if not np.array_equal(np.isfinite(actual), np.isfinite(expected)):
        raise FixtureError('complete pixel mask differs')
    if not np.array_equal(actual, expected, equal_nan=True):
        raise FixtureError(f'complete independent weighted circle/control pixels differ for {name}')


CACHE_SCHEMA='addp.circle-weighted-oracle/v1'
CACHE_MARKER='.oracle-owner'
CONTROL_NAME='circle-weighted-oracle-control.json'
EXPECTED_EXPORT='.circle-expected'

@contextmanager
def elapsed(timings,key):
    start=time.monotonic()
    try:yield
    finally:timings[key]=timings.get(key,0)+round((time.monotonic()-start)*1000)

def encoded(value):return json.dumps(value,sort_keys=True,separators=(',',':')).encode()
def scope_hash(scope):return hashlib.sha256(encoded(scope)).hexdigest()
def oracle_path(scope):return Path(tempfile.gettempdir())/('addp-circle-weighted-oracle-'+scope_hash(scope))

def validate_scope(root,scope,*,sources):
    if (not isinstance(scope,dict) or set(scope)!={'schema','run_id','fixture_sha256','source_root','sources'}
        or scope['schema']!=CACHE_SCHEMA or not re.fullmatch('focal-circle-weighted-[0-9]+-[0-9]+',scope.get('run_id',''))
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
    return archive_files(root/EXPECTED_EXPORT,artifacts,[n+'.npy' for n in TARGETS],previous,'circle-weighted-expectations')

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
        if not np.array_equal(level.ReadAsArray(),wanted,equal_nan=True) or not np.array_equal(level.GetMaskBand().ReadAsArray(),np.where(np.isfinite(wanted),255,0)):raise FixtureError('complete nearest overview pixels or mask differ')
        previous=wanted
    level=band=None

def verify(root,names,control):
    import numpy as np
    from osgeo import gdal,osr
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('invalid exact CIRCLE target inventory')
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
        if not np.array_equal(band.GetMaskBand().ReadAsArray(),np.where(np.isfinite(expected),255,0)):raise FixtureError('complete pixel mask differs')
        if band.GetUnitType()!='' or band.GetScale() is not None or band.GetOffset() is not None or band.GetColorTable() is not None:raise FixtureError('focal output unit or source encoding differs')
        timings['pixels_mask_metadata_ms']+=round((time.monotonic()-pixel_start)*1000)
        with elapsed(timings,'cog_layout_ms'):
            if validate(ds,full_check=True)[1]:raise FixtureError('COG full layout validation failed')
        with elapsed(timings,'overviews_ms'):nearest_overviews(ds,actual)
        zeros=int(np.count_nonzero(np.isfinite(expected)&(expected==0)))
        result['targets'][name]={**fact,'affine_transform':list(ds.GetGeoTransform()),'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,'overview_count':band.GetOverviewCount(),
            'valid_pixels':int(np.isfinite(expected).sum()),'zero_pixels':zeros,'negative_pixels':int(np.count_nonzero(expected<0)),
            **{k:True for k in ('full_pixel_and_mask_verification','independent_scalar_oracle_verified','original_coverage_verified','circle_membership_verified','circle_statistics_verified','nearest_overviews_verified','explicit_selected_band_verified','legal_zero_verified')}}
        band=ds=None
    comparisons = []
    for left, right in COMPARISONS:
        if left in names and right in names:
            a = gdal.Open(str(root/'outputs'/left))
            b = gdal.Open(str(root/'outputs'/right))
            if (not np.array_equal(a.ReadAsArray(), b.ReadAsArray(), equal_nan=True)
                    or not np.array_equal(a.GetRasterBand(1).GetMaskBand().ReadAsArray(),
                                          b.GetRasterBand(1).GetMaskBand().ReadAsArray())):
                raise FixtureError('actual all-one and unweighted COG pixels or masks differ')
            a = b = None
            comparisons.append([left, right])
    result['comparisons'] = comparisons
    boundary = None
    if all(name in names for name in BOUNDARY_PAIR):
        measurements = []
        for name in BOUNDARY_PAIR:
            ds = gdal.Open(str(root/'outputs'/name))
            measurements.append(float(ds.ReadAsArray(62, 62, 1, 1)[0, 0]))
            ds = None
        if measurements != [4., 2.]:
            raise FixtureError('actual exact/below boundary witness differs')
        boundary = {'equal': measurements[0], 'below': measurements[1]}
    result['boundary_witness'] = boundary
    result['verification_timing_ms']=timings
    result['oracle_cache']={'scope_sha256':scope_hash(control['scope']),'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}
    return result

def archive_cogs(root,artifacts,verified,previous):
    return archive_files(root/'outputs',artifacts,list(verified['targets']),previous,'circle-weighted-cogs')

def verify_sources(root):
    import numpy as np
    from osgeo import gdal
    for name in SOURCES:
        spec=source_spec(name);values,alpha,mask=source_arrays(name);ds=gdal.Open(str(root/name));crs=reference(name)
        if ds is None or ds.GetDriver().ShortName!='GTiff' or ds.RasterCount!=4 or (ds.RasterXSize,ds.RasterYSize)!=(spec['width'],spec['height']) or ds.GetGeoTransform(can_return_null=True)!=spec['grid'] or ds.GetFileList()!=[str(root/name)]:raise FixtureError('complete source dimensions/grid/inventory differ')
        if crs is not None:
            if ds.GetSpatialRef()is None or not ds.GetSpatialRef().IsSame(crs):raise FixtureError('source CRS changed')
        elif ds.GetProjection():raise FixtureError('missing source location fabricated')
        for index,wanted in enumerate((np.full(values.shape,42.25),values,np.full(values.shape,17),alpha),1):
            band=ds.GetRasterBand(index)
            if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue()!=spec['nodata'] or not np.array_equal(band.ReadAsArray(),wanted,equal_nan=True) or not np.array_equal(band.GetMaskBand().ReadAsArray(),mask):raise FixtureError('complete source pixels/mask/type/NoData differ')
            if band.GetColorInterpretation()!=(gdal.GCI_AlphaBand if index==4 else gdal.GCI_GrayIndex if index==1 else gdal.GCI_Undefined):raise FixtureError('source band interpretation changed')
            if (band.GetScale(),band.GetOffset())!=((17.,500.)if name=='north.tif'and index==2 else(None,None)):raise FixtureError('source scale/offset changed')
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
    expected=os.environ.get('ADDP_ONLINE_FOCAL_CIRCLE_WEIGHTED_ORACLE_ANCHOR_SHA256')
    if expected is not None and digest(path)['sha256']!=expected:raise FixtureError('oracle anchor changed')
    return path,value

def main(action):
    secret=guard()
    if action in {'start','stop'}:return NFSExport('focal-circle-weighted',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'focal-circle-weighted-source.json').write_text(json.dumps(result));return result
    if action=='prepare-oracle':
        scope={'schema':CACHE_SCHEMA,'run_id':os.environ['ADDP_ONLINE_TEST_RUN_ID'],'source_root':str(root.resolve()),
               'fixture_sha256':digest(Path(__file__))['sha256'],'sources':json.loads((secret/'focal-circle-weighted-source.json').read_text())['sources']}
        path=secret/CONTROL_NAME
        fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb')as f:f.write(encoded({'scope':scope,'arrays':None}))
        result=runtime_worker(action,root,[json.dumps(scope)])
        if result.get('scope')!=scope or set(result.get('arrays',{}))!=set(TARGETS):raise FixtureError('oracle prepare response differs')
        temp=path.with_suffix('.tmp')
        with temp.open('xb')as f:f.write(encoded({'scope':scope,'arrays':result['arrays']}))
        temp.chmod(0o600);temp.replace(path)
        baseline=runtime_worker('verify',root,[json.dumps({'scope':scope,'arrays':result['arrays']})])
        artifacts=Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']);sources=archive_files(root,artifacts,SOURCES,{},'circle-weighted-sources')
        expectations=archive_expectations(root,artifacts,{})
        history=secret/'circle-weighted-archive-history.json'
        with history.open('xb') as f:f.write(encoded({'sources':sources,'targets':{},'expected':expectations}))
        return {**baseline,'archived_sources':sources,'archived_cogs':{},'archived_expectations':expectations,'scope_sha256':scope_hash(scope),'anchor_sha256':digest(path)['sha256'],'expected_array_count':len(TARGETS),
                'cache_bytes':sum(v['size_bytes']for v in result['arrays'].values()),'timing_ms':result['timing_ms']}
    if action=='cleanup-oracle':
        if not(secret/CONTROL_NAME).exists():return {'oracle_cache_removed':True,'not_started':True}
        path,control=private_control(secret);result=runtime_worker(action,root,[json.dumps(control)])
        if result.get('oracle_cache_removed')is not True:raise FixtureError('oracle cleanup proof missing')
        path.unlink();return {'oracle_cache_removed':True}
    if action=='verify':
        if not os.environ.get('ADDP_ONLINE_FOCAL_CIRCLE_WEIGHTED_ORACLE_ANCHOR_SHA256'):raise FixtureError('oracle anchor is required')
        _,control=private_control(secret);timings={}
        with elapsed(timings,'worker_transport_ms'):result=runtime_worker('verify',root,[json.dumps(control),*sys.argv[2:]])
        with elapsed(timings,'source_receipt_ms'):
            if result['sources']!=json.loads((secret/'focal-circle-weighted-source.json').read_text())['sources']:raise FixtureError('source file changed')
        with elapsed(timings,'archive_ms'):
            history=secret/'circle-weighted-archive-history.json'
            if not history.is_file():raise FixtureError('archive history missing')
            old=json.loads(history.read_bytes());artifacts=Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
            result['archived_sources']=archive_files(root,artifacts,SOURCES,old['sources'],'circle-weighted-sources')
            result['archived_cogs']=archive_cogs(root,artifacts,result,old['targets'])
            result['archived_expectations']=archive_expectations(root,artifacts,old['expected'])
            history.write_bytes(encoded({'sources':result['archived_sources'],'targets':result['archived_cogs'],'expected':result['archived_expectations']}))
        result['host_timing_ms']=timings;return result
    raise FixtureError('unknown fixture action')
if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1]);print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Raster CIRCLE fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
