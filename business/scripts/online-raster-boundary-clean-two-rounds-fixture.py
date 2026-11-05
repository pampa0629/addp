#!/usr/bin/env python3
"""Owned two-round boundary-clean sources, scalar oracle and immutable COG evidence."""
from __future__ import annotations
import hashlib,io,json,os,re,subprocess,sys,tempfile,time,math
from contextlib import contextmanager
from functools import lru_cache
from pathlib import Path

SUITE='raster-boundary-clean-two-rounds-persistence'
RUNTIME='addp-raster-boundary-clean-two-rounds-runtime'
# Source, optional explicit rounds (None means omitted), upstream stage.
CASES = {
    'change-default.cog.tif': ('change.tif', None, 'direct'),
    'change-one.cog.tif': ('change.tif', 1, 'direct'),
    'change-two.cog.tif': ('change.tif', 2, 'direct'),
    'frozen-default.cog.tif': ('frozen.tif', None, 'direct'),
    'frozen-one.cog.tif': ('frozen.tif', 1, 'direct'),
    'frozen-two.cog.tif': ('frozen.tif', 2, 'direct'),
    'seam-default.cog.tif': ('seam.tif', None, 'direct'),
    'seam-one.cog.tif': ('seam.tif', 1, 'direct'),
    'seam-two.cog.tif': ('seam.tif', 2, 'direct'),
    'reclassified-two.cog.tif': ('continuous.tif', 2, 'reclassify'),
    'empty-two.cog.tif': ('empty.tif', 2, 'direct'),
    'thin-two.cog.tif': ('thin.tif', 2, 'direct'),
}
ROUND_FAILURES = {
    'rounds_null': None, 'rounds_true': True, 'rounds_false': False, 'rounds_zero': 0,
    'rounds_negative': -1, 'rounds_high': 3, 'rounds_fraction': 1.5,
    'rounds_string': '2', 'rounds_list': [2], 'rounds_object': {},
}
BAND_FAILURES = {
    'band_zero': 0, 'band_high': 99, 'band_fraction': 1.5, 'band_alpha': 3,
    'class_fractional': 4, 'class_overflow': 5,
}
FAILURES = (
    'rounds_null', 'rounds_true', 'rounds_false', 'rounds_zero', 'rounds_negative',
    'rounds_high', 'rounds_fraction', 'rounds_string', 'rounds_list', 'rounds_object',
    'band_zero', 'band_high', 'band_fraction', 'band_alpha', 'class_fractional', 'class_overflow',
)
TARGETS = tuple(CASES)
SOURCES = ('change.tif', 'frozen.tif', 'seam.tif', 'continuous.tif', 'empty.tif', 'thin.tif')
COMPARISONS = (
    ('change-default.cog.tif', 'change-one.cog.tif'),
    ('frozen-default.cog.tif', 'frozen-one.cog.tif'),
    ('seam-default.cog.tif', 'seam-one.cog.tif'),
)
# (row, column, exact value) witnesses measured from actual COGs.
WITNESSES = {
    'change-one.cog.tif': (1, 1, 1.),
    'change-two.cog.tif': (1, 1, 0.),
    'frozen-one.cog.tif': (0, 1, 0.),
    'frozen-two.cog.tif': (0, 1, 0.),
    'seam-one.cog.tif': (510, 510, 1.),
    'seam-two.cog.tif': (510, 510, 0.),
}


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

def guard(env=os.environ):return NFSExport('boundary-clean-two-rounds',command).guard(env)
def owned_root(secret):return NFSExport('boundary-clean-two-rounds',command).owned_root(secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size<=0:raise FixtureError('expected a positive-size ordinary owned file')
    return {'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}

def source_spec(name):
    if name not in SOURCES:
        raise FixtureError('unknown boundary-clean source')
    width, height = {
        'change.tif': (3, 3), 'frozen.tif': (3, 3), 'seam.tif': (519, 515),
        'continuous.tif': (131, 129), 'empty.tif': (131, 129), 'thin.tif': (512, 1),
    }[name]
    grid = (110., 1/1024, 1/8192, 30., 1/4096, -1/512)
    return {'width': width, 'height': height, 'grid': None if name == 'thin.tif' else grid,
            'epsg': 0 if name == 'thin.tif' else 4326, 'nodata': -9999.}


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
    rows, cols = np.indices((spec['height'], spec['width']))
    values = ((cols + 2*rows) % 7 - 3).astype(np.float64)
    alpha = np.full(values.shape, 255.)
    mask = np.full(values.shape, 255, np.uint8)
    nodata = spec['nodata']
    if name == 'change.tif':
        values[:] = [[0, 0, 0], [0, 1, 1], [0, 2, 1]]
    elif name == 'frozen.tif':
        values[:] = [[0, 0, 0], [1, 1, 1], [2, 2, 2]]
    elif name == 'seam.tif':
        values[:] = nodata
        values[:61, :61] = 0.
        values[10:16, 10:16] = -7.
        values[503:507, 509:515] = -7.
        values[509:512, 509:512] = [[0, 0, 0], [0, 1, 1], [0, 2, 1]]
        values[504, 510] = .5
        mask[504, 510] = 0
        values[505, 511] = 2.**53
        alpha[505, 511] = 0.
        alpha[504, 509] = 128.
    elif name == 'continuous.tif':
        values = values / 4.
        values[0, 1] = nodata
    elif name == 'empty.tif':
        values[:] = nodata
    elif name == 'thin.tif':
        values[0, 8:10] = nodata
    fractional, overflow = values.copy(), values.copy()
    fractional[1 if spec['height'] > 1 else 0, 1] = .5
    overflow[1 if spec['height'] > 1 else 0, 1] = 2.**53
    return (np.full(values.shape, .25), values, alpha, fractional, overflow), mask


def seed(root):
    from osgeo import gdal
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite fixture data')
    for name in SOURCES:
        spec = source_spec(name)
        bands, mask = source_arrays(name)
        ds = gdal.GetDriverByName('GTiff').Create(str(root/name), spec['width'], spec['height'],
                                                 len(bands), gdal.GDT_Float64, ['TILED=YES', 'COMPRESS=DEFLATE'])
        if spec['grid'] is not None:
            ds.SetGeoTransform(spec['grid'])
        crs = reference(name)
        if crs is not None:
            ds.SetSpatialRef(crs)
        for index, values in enumerate(bands, 1):
            band = ds.GetRasterBand(index)
            band.WriteArray(values)
            band.SetNoDataValue(spec['nodata'])
            if index == 2:
                band.SetScale(17.)
                band.SetOffset(500.)
        ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
            if ds.CreateMaskBand(gdal.GMF_PER_DATASET) != gdal.CE_None:
                raise FixtureError('cannot create internal source mask')
        band = ds = None
        ds = gdal.Open(str(root/name), gdal.GA_Update)
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        ds = None
    verify_sources(root)
    return {'sources': {name: digest(root/name) for name in SOURCES}}


def class_rules():
    return [{'min': None, 'max': 0, 'class': -7}, {'min': 0, 'max': .5, 'class': 0},
            {'min': .5, 'max': None, 'class': 2}]


@lru_cache(maxsize=6)
def expected_rounds(source, stage):
    from collections import Counter
    import numpy as np
    bands, mask = source_arrays(source)
    values, alpha = bands[1:3]
    valid = (np.isfinite(values) & (values != source_spec(source)['nodata'])
             & np.isfinite(alpha) & (alpha > 0) & (mask != 0))
    if stage == 'reclassify':
        values = np.where(values < 0, -7., np.where(values < .5, 0., 2.))
    # One independent global count on the original selected classification.
    counts = Counter(int(value) for value in values[valid])
    height, width = values.shape
    positions = [tuple(site) for site in np.argwhere(valid)]
    neighbours = {
        (row, col): [(y, x) for y in range(max(0, row-1), min(height, row+2))
                     for x in range(max(0, col-1), min(width, col+2)) if valid[y, x]]
        for row, col in positions
    }
    baseline = values
    results = []
    for _ in range(2):
        expanded = np.full(values.shape, np.nan)
        for cell in positions:
            expanded[cell] = max((baseline[near] for near in neighbours[cell]),
                                 key=lambda value: (counts[value], -value))
        shrunk = np.full(values.shape, np.nan)
        for cell in positions:
            category = baseline[cell]
            shrunk[cell] = category if any(expanded[near] == category for near in neighbours[cell]) else expanded[cell]
        shrunk.flags.writeable = False
        results.append(shrunk)
        baseline = shrunk
    return tuple(results)


def expected_pixels(name):
    source, rounds, stage = CASES[name]
    return expected_rounds(source, stage)[(rounds or 1)-1]


def assert_pixels(actual, expected, name):
    import numpy as np
    if not np.array_equal(np.isfinite(actual), np.isfinite(expected)):
        raise FixtureError('complete pixel mask differs')
    if not np.array_equal(actual, expected, equal_nan=True):
        raise FixtureError(f'complete independent boundary-clean pixels differ for {name}')


CACHE_SCHEMA='addp.boundary-two-rounds-oracle/v1'
CACHE_MARKER='.oracle-owner'
CONTROL_NAME='boundary-two-rounds-oracle-control.json'
EXPECTED_EXPORT='.boundary-expected'

@contextmanager
def elapsed(timings,key):
    start=time.monotonic()
    try:yield
    finally:timings[key]=timings.get(key,0)+round((time.monotonic()-start)*1000)

def encoded(value):return json.dumps(value,sort_keys=True,separators=(',',':')).encode()
def scope_hash(scope):return hashlib.sha256(encoded(scope)).hexdigest()
def oracle_path(scope):return Path(tempfile.gettempdir())/('addp-boundary-two-rounds-oracle-'+scope_hash(scope))

def validate_scope(root,scope,*,sources):
    if (not isinstance(scope,dict) or set(scope)!={'schema','run_id','fixture_sha256','source_root','sources'}
        or scope['schema']!=CACHE_SCHEMA or not re.fullmatch('boundary-clean-two-rounds-[0-9]+-[0-9]+',scope.get('run_id',''))
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
    return archive_files(root/EXPECTED_EXPORT,artifacts,[n+'.npy' for n in TARGETS],previous,'boundary-two-rounds-expectations')

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
    from osgeo import gdal
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate
    if len(set(names))!=len(names) or any(n not in TARGETS for n in names):raise FixtureError('invalid exact boundary-clean target inventory')
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
            crs = reference(CASES[name][0])
            if ds.GetSpatialRef() is None or not ds.GetSpatialRef().IsSame(crs):raise FixtureError('source CRS changed')
        elif ds.GetSpatialRef() is not None or ds.GetProjection():raise FixtureError('missing source CRS was fabricated')
        band=ds.GetRasterBand(1);actual=band.ReadAsArray()
        if band.DataType!=gdal.GDT_Float64 or band.GetNoDataValue() is None or not np.isnan(band.GetNoDataValue()):raise FixtureError('Float64 or NaN NoData changed')
        assert_pixels(actual,expected,name)
        if not np.array_equal(band.GetMaskBand().ReadAsArray(),np.where(np.isfinite(expected),255,0)):raise FixtureError('complete pixel mask differs')
        if band.GetUnitType()!='' or band.GetScale() is not None or band.GetOffset() is not None or band.GetColorTable() is not None:raise FixtureError('boundary-clean output unit or source encoding differs')
        timings['pixels_mask_metadata_ms']+=round((time.monotonic()-pixel_start)*1000)
        with elapsed(timings,'cog_layout_ms'):
            if validate(ds,full_check=True)[1]:raise FixtureError('COG full layout validation failed')
        with elapsed(timings,'overviews_ms'):nearest_overviews(ds,actual)
        zeros=int(np.count_nonzero(np.isfinite(expected)&(expected==0)))
        result['targets'][name]={**fact,'affine_transform':list(ds.GetGeoTransform(can_return_null=True) or ()),'cog_valid':True,'has_overviews':band.GetOverviewCount()>0,'overview_count':band.GetOverviewCount(),
            'valid_pixels':int(np.isfinite(expected).sum()),'zero_pixels':zeros,'negative_pixels':int(np.count_nonzero(expected<0)),
            **{k:True for k in ('full_pixel_and_mask_verification','independent_scalar_oracle_verified','original_coverage_verified','frozen_priorities_verified','complete_round_snapshots_verified','nearest_overviews_verified','explicit_selected_band_verified','legal_zero_verified')}}
        band=ds=None
    comparisons = []
    for left, right in COMPARISONS:
        if left in names and right in names:
            a = gdal.Open(str(root/'outputs'/left))
            b = gdal.Open(str(root/'outputs'/right))
            if (not np.array_equal(a.ReadAsArray(), b.ReadAsArray(), equal_nan=True)
                    or not np.array_equal(a.GetRasterBand(1).GetMaskBand().ReadAsArray(),
                                          b.GetRasterBand(1).GetMaskBand().ReadAsArray())):
                raise FixtureError('actual default and explicit one-round COG pixels or masks differ')
            a = b = None
            comparisons.append([left, right])
    result['comparisons'] = comparisons
    witnesses = {}
    for name, (row, col, wanted) in WITNESSES.items():
        if name in names:
            ds = gdal.Open(str(root/'outputs'/name))
            value = float(ds.ReadAsArray(col, row, 1, 1)[0, 0])
            ds = None
            if value != wanted:
                raise FixtureError('actual round or frozen-priority witness differs')
            witnesses[name] = value
    result['round_witnesses'] = witnesses
    result['verification_timing_ms']=timings
    result['oracle_cache']={'scope_sha256':scope_hash(control['scope']),'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}
    return result

def archive_cogs(root,artifacts,verified,previous):
    return archive_files(root/'outputs',artifacts,list(verified['targets']),previous,'boundary-two-rounds-cogs')

def verify_sources(root):
    import numpy as np
    from osgeo import gdal
    for name in SOURCES:
        spec = source_spec(name)
        bands, mask = source_arrays(name)
        ds = gdal.Open(str(root/name))
        crs = reference(name)
        if (ds is None or ds.GetDriver().ShortName != 'GTiff' or ds.RasterCount != len(bands)
                or (ds.RasterXSize, ds.RasterYSize) != (spec['width'], spec['height'])
                or ds.GetGeoTransform(can_return_null=True) != spec['grid'] or ds.GetFileList() != [str(root/name)]):
            raise FixtureError('complete source dimensions/grid/inventory differ')
        if crs is not None:
            if ds.GetSpatialRef() is None or not ds.GetSpatialRef().IsSame(crs):
                raise FixtureError('source CRS changed')
        elif ds.GetSpatialRef() is not None or ds.GetProjection():
            raise FixtureError('missing source CRS was fabricated')
        for index, wanted in enumerate(bands, 1):
            band = ds.GetRasterBand(index)
            if (band.DataType != gdal.GDT_Float64 or band.GetNoDataValue() != spec['nodata']
                    or not np.array_equal(band.ReadAsArray(), wanted, equal_nan=True)
                    or not np.array_equal(band.GetMaskBand().ReadAsArray(), mask)):
                raise FixtureError('complete source pixels/mask/type/NoData differ')
            color = gdal.GCI_AlphaBand if index == 3 else gdal.GCI_GrayIndex if index == 1 else gdal.GCI_Undefined
            if band.GetColorInterpretation() != color:
                raise FixtureError('source band interpretation changed')
            if (band.GetScale(), band.GetOffset()) != ((17., 500.) if index == 2 else (None, None)):
                raise FixtureError('source scale/offset changed')
        band = ds = None


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
    expected=os.environ.get('ADDP_ONLINE_BOUNDARY_CLEAN_TWO_ROUNDS_ORACLE_ANCHOR_SHA256')
    if expected is not None and digest(path)['sha256']!=expected:raise FixtureError('oracle anchor changed')
    return path,value

def main(action):
    secret=guard()
    if action in {'start','stop'}:return NFSExport('boundary-clean-two-rounds',command).lifecycle(action,secret,os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root=owned_root(secret)
    if action=='seed':
        result=runtime_worker('seed',root);(secret/'boundary-clean-two-rounds-source.json').write_text(json.dumps(result));return result
    if action=='prepare-oracle':
        scope={'schema':CACHE_SCHEMA,'run_id':os.environ['ADDP_ONLINE_TEST_RUN_ID'],'source_root':str(root.resolve()),
               'fixture_sha256':digest(Path(__file__))['sha256'],'sources':json.loads((secret/'boundary-clean-two-rounds-source.json').read_text())['sources']}
        path=secret/CONTROL_NAME
        fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb')as f:f.write(encoded({'scope':scope,'arrays':None}))
        result=runtime_worker(action,root,[json.dumps(scope)])
        if result.get('scope')!=scope or set(result.get('arrays',{}))!=set(TARGETS):raise FixtureError('oracle prepare response differs')
        temp=path.with_suffix('.tmp')
        with temp.open('xb')as f:f.write(encoded({'scope':scope,'arrays':result['arrays']}))
        temp.chmod(0o600);temp.replace(path)
        baseline=runtime_worker('verify',root,[json.dumps({'scope':scope,'arrays':result['arrays']})])
        artifacts=Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']);sources=archive_files(root,artifacts,SOURCES,{},'boundary-two-rounds-sources')
        expectations=archive_expectations(root,artifacts,{})
        history=secret/'boundary-two-rounds-archive-history.json'
        with history.open('xb') as f:f.write(encoded({'sources':sources,'targets':{},'expected':expectations}))
        return {**baseline,'archived_sources':sources,'archived_cogs':{},'archived_expectations':expectations,'scope_sha256':scope_hash(scope),'anchor_sha256':digest(path)['sha256'],'expected_array_count':len(TARGETS),
                'cache_bytes':sum(v['size_bytes']for v in result['arrays'].values()),'timing_ms':result['timing_ms']}
    if action=='cleanup-oracle':
        if not(secret/CONTROL_NAME).exists():return {'oracle_cache_removed':True,'not_started':True}
        path,control=private_control(secret);result=runtime_worker(action,root,[json.dumps(control)])
        if result.get('oracle_cache_removed')is not True:raise FixtureError('oracle cleanup proof missing')
        path.unlink();return {'oracle_cache_removed':True}
    if action=='verify':
        if not os.environ.get('ADDP_ONLINE_BOUNDARY_CLEAN_TWO_ROUNDS_ORACLE_ANCHOR_SHA256'):raise FixtureError('oracle anchor is required')
        _,control=private_control(secret);timings={}
        with elapsed(timings,'worker_transport_ms'):result=runtime_worker('verify',root,[json.dumps(control),*sys.argv[2:]])
        with elapsed(timings,'source_receipt_ms'):
            if result['sources']!=json.loads((secret/'boundary-clean-two-rounds-source.json').read_text())['sources']:raise FixtureError('source file changed')
        with elapsed(timings,'archive_ms'):
            history=secret/'boundary-two-rounds-archive-history.json'
            if not history.is_file():raise FixtureError('archive history missing')
            old=json.loads(history.read_bytes());artifacts=Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
            result['archived_sources']=archive_files(root,artifacts,SOURCES,old['sources'],'boundary-two-rounds-sources')
            result['archived_cogs']=archive_cogs(root,artifacts,result,old['targets'])
            result['archived_expectations']=archive_expectations(root,artifacts,old['expected'])
            history.write_bytes(encoded({'sources':result['archived_sources'],'targets':result['archived_cogs'],'expected':result['archived_expectations']}))
        result['host_timing_ms']=timings;return result
    raise FixtureError('unknown fixture action')
if __name__=='__main__':
    try:
        result=worker(sys.argv[2],Path(sys.argv[3]),sys.argv[4:]) if sys.argv[1:2]==['--worker'] else main(sys.argv[1]);print(json.dumps(result,sort_keys=True))
    except Exception as error:
        print(f'Boundary cleaning fixture failed: {type(error).__name__}: {error}',file=sys.stderr);raise SystemExit(1)
