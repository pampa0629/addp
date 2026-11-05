#!/usr/bin/env python3
"""Owned raster sources, frozen independent square-window oracle and raw COG evidence."""
from __future__ import annotations

from collections import Counter
from decimal import Decimal, localcontext
from fractions import Fraction
from functools import lru_cache
import hashlib
import io
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

PREFIX = 'raster-focal-square-seven'
SUITE = PREFIX + '-persistence'
RUNTIME = 'addp-' + PREFIX + '-runtime'
ORACLE = '.' + PREFIX + '-oracle.npz'
ORACLE_SCHEMA = 'addp.focal-square-seven-oracle/v1'
PERCENTILES = (0.0, 25.0, 50.0, 75.0, 100.0)
BLOCK_SIZE = 128
NODATA = -9999.0
GRID = (500000.0, 2.0, 0.25, 3000000.0, -0.5, -3.0)
EPSG = 3857

# The explicit mask and the NoData/alpha fields all belong to band 2's validity.
SOURCES = ('cross.tif', 'control.tif', 'invalid.tif', 'fractional.tif', 'unsafe.tif')
CASES = {
    # Source, window edge, method, optional kernel, optional reclassification.
    'sum.cog.tif': ('control.tif', 7, 'sum', None, False),
    'mean.cog.tif': ('control.tif', 7, 'mean', None, False),
    'stddev.cog.tif': ('control.tif', 7, 'stddev', None, False),
    'min.cog.tif': ('cross.tif', 7, 'min', None, False),
    'median.cog.tif': ('cross.tif', 7, 'median', None, False),
    'max.cog.tif': ('cross.tif', 7, 'max', None, False),
    'percentiles.cog.tif': ('cross.tif', 7, 'percentile', None, False),
    'mode.cog.tif': ('control.tif', 7, 'mode', None, True),
    'variety.cog.tif': ('control.tif', 7, 'variety', None, False),
    'asymmetric-mean.cog.tif': ('cross.tif', 7, 'weighted_mean', 'asymmetric', False),
    'asymmetric-stddev.cog.tif': ('cross.tif', 7, 'weighted_stddev', 'asymmetric', False),
    'asymmetric-sum.cog.tif': ('cross.tif', 7, 'weighted_sum', 'asymmetric', False),
    'ones-mean.cog.tif': ('control.tif', 7, 'weighted_mean', 'ones', False),
    'ones-stddev.cog.tif': ('control.tif', 7, 'weighted_stddev', 'ones', False),
    'ones-sum.cog.tif': ('control.tif', 7, 'weighted_sum', 'ones', False),
    'square3.cog.tif': ('control.tif', 3, 'mean', None, False),
    'square5.cog.tif': ('control.tif', 5, 'mean', None, False),
    'invalid.cog.tif': ('invalid.tif', 7, 'weighted_sum', 'asymmetric', False),
}
# All four outer sides contribute and the centre carries no weight.
ASYMMETRIC = (
    (0, 0, 0, 0, 0, 0, 2),
    (0, 0, 0, 0.5, 0, 0, 0),
    (0, 0, 0, 0, 0, 0, 0),
    (1, 0, 0, 0, 0, 3, 0),
    (0, 0, 0, 0, 0, 0, 0),
    (0, 0, 4, 0, 0, 0, 0),
    (0, 0, 0, 0, 1, 0, 0),
)
KERNELS = {'asymmetric': ASYMMETRIC, 'ones': ((1,) * 7,) * 7}
FAILURES = (
    'window_missing',
    'window_boolean',
    'window_string',
    'window_null',
    'window_even',
    'window_nine',
    'square_radius',
    'method',
    'list_missing',
    'list_null',
    'list_empty',
    'list_duplicate',
    'list_descending',
    'list_range',
    'unused_percentiles',
    'mean_weights_size',
    'stddev_weights_size',
    'sum_weights_size',
    'weights_missing',
    'weights_zero',
    'weights_negative',
    'weights_boolean',
    'weights_ragged',
    'unused_weights',
    'band_zero',
    'band_high',
    'band_boolean',
    'band_alpha',
    'mode_fractional',
    'variety_unsafe',
)
# Left COG/band, right COG/band, exact equality required.
COMPARISONS = (
    ('ones-mean.cog.tif', 0, 'mean.cog.tif', 0),
    ('ones-stddev.cog.tif', 0, 'stddev.cog.tif', 0),
    ('ones-sum.cog.tif', 0, 'sum.cog.tif', 0),
    ('percentiles.cog.tif', 0, 'min.cog.tif', 0),
    ('percentiles.cog.tif', 2, 'median.cog.tif', 0),
    ('percentiles.cog.tif', 4, 'max.cog.tif', 0),
)


class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=150, **kwargs).stdout.strip()


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker
    import importlib

    archive_files = importlib.import_module('business.scripts.online-raster-zonal-percentile-fixture').archive_files


def encoded(value):
    return json.dumps(value, allow_nan=False, sort_keys=True).encode()


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('positive ordinary file required')
    data = path.read_bytes()
    return {'size_bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest()}


def source_shape(source):
    if source not in SOURCES:
        raise FixtureError('unknown seven-pixel source')
    return (367, 369) if source == 'cross.tif' else (33, 37)


def source_arrays(source):
    import numpy as np

    h, w = source_shape(source)
    y, x = np.indices((h, w))
    values = np.full((h, w), NODATA)
    alpha = np.full((h, w), 255.0)
    mask = np.full((h, w), 255, np.uint8)
    if source == 'cross.tif':
        # Include both sides of each seam and the raster edges. A sparse
        # interior keeps the independent oracle affordable for this T4 matrix.
        support = (x >= 355) | (y >= 355) | (x < 8) | (y < 8) | ((x % 29 == 0) & (y % 23 == 0))
        values[support] = (((x * 3 + y * 5) % 17 - 8) / 4)[support]
        values[359, 360], values[360, 359], values[360, 360] = -12.0, 0.0, 24.0
        mask[359, 359] = 0
        alpha[361, 359] = 128.0
        alpha[360, 361] = 0.0
        values[361, 360] = NODATA
    elif source == 'control.tif':
        # Pairs remain isolated even in a 7×7 window and yield exact controls.
        for row in (3, 11, 27):
            values[row, 3:5] = [0.0, 4.0]
            values[row, 13:15] = [-4.0, 4.0]
            values[row, 23:25] = [2.0, 2.0]
            values[row, 32:34] = [1.0, 5.0]
        mask[27, 4] = 0
        alpha[11, 14] = 0.0
        alpha[3, 4] = 128.0
        # Two separated pairs distinguish 3 from 5 and 5 from 7.
        values[16, [4, 6, 18, 21]] = [0.0, 4.0, 0.0, 8.0]
    elif source in ('fractional.tif', 'unsafe.tif'):
        values[5, 5] = 0.25 if source == 'fractional.tif' else 2**53
    return values, alpha, mask


def source_bands(source):
    import numpy as np

    values, alpha, mask = source_arrays(source)
    return (np.full(values.shape, 42.25), values, np.full(values.shape, 17.0), alpha), mask


def crs():
    from osgeo import osr

    result = osr.SpatialReference()
    if result.ImportFromEPSG(EPSG) != 0:
        raise FixtureError('cannot initialize source CRS')
    result.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    return result


def same_crs(dataset):
    from osgeo import osr

    actual = dataset.GetSpatialRef()
    if actual is None:
        return False
    actual.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    return bool(actual.IsSame(crs()))


def seed(root):
    from osgeo import gdal

    if any((root / name).exists() or (root / name).is_symlink() for name in (*SOURCES, ORACLE)) or any(
        (root / 'outputs').iterdir()
    ):
        raise FixtureError('refusing to overwrite owned sources or results')
    for source in SOURCES:
        height, width = source_shape(source)
        datasets, mask = source_bands(source)
        dataset = gdal.GetDriverByName('GTiff').Create(
            str(root / source), width, height, 4, gdal.GDT_Float64, ['TILED=YES', 'COMPRESS=DEFLATE']
        )
        dataset.SetSpatialRef(crs())
        dataset.SetGeoTransform(GRID)
        for index, values in enumerate(datasets, 1):
            band = dataset.GetRasterBand(index)
            band.WriteArray(values)
            band.SetNoDataValue(NODATA)
        dataset.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        if source in ('cross.tif', 'control.tif'):
            dataset.GetRasterBand(2).SetScale(2.0)
            dataset.GetRasterBand(2).SetOffset(7.0)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
            if dataset.CreateMaskBand(gdal.GMF_PER_DATASET) != gdal.CE_None:
                raise FixtureError('cannot create internal source mask')
        dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        band = dataset = None
    return {'sources': verify_sources(root)}


def verify_sources(root):
    import numpy as np
    from osgeo import gdal

    facts = {}
    for source in SOURCES:
        path = root / source
        facts[source] = digest(path)
        dataset = gdal.Open(str(path))
        if (
            dataset is None
            or dataset.RasterCount != 4
            or (dataset.RasterYSize, dataset.RasterXSize) != source_shape(source)
            or dataset.GetGeoTransform(can_return_null=True) != GRID
            or not same_crs(dataset)
            or dataset.GetFileList() != [str(path)]
        ):
            raise FixtureError('source grid/CRS/inventory changed')
        wanted_bands, mask = source_bands(source)
        for index, wanted in enumerate(wanted_bands, 1):
            band = dataset.GetRasterBand(index)
            scaled = index == 2 and source in ('cross.tif', 'control.tif')
            if (
                band.DataType != gdal.GDT_Float64
                or band.GetNoDataValue() != NODATA
                or band.GetColorInterpretation()
                != (gdal.GCI_AlphaBand if index == 4 else gdal.GCI_GrayIndex if index == 1 else gdal.GCI_Undefined)
                or band.GetScale() != (2.0 if scaled else None)
                or band.GetOffset() != (7.0 if scaled else None)
                or band.GetUnitType()
                or not np.array_equal(band.ReadAsArray(), wanted)
                or not np.array_equal(band.GetMaskBand().ReadAsArray(), mask)
            ):
                raise FixtureError('complete source values/mask/alpha/NoData/scale changed')
        band = dataset = None
    return facts


def class_rules():
    return [
        {'min': None, 'max': 0, 'class': 0},
        {'min': 0, 'max': 10, 'class': 10},
        {'min': 10, 'max': None, 'class': 20},
    ]


def descriptions(name):
    return ['percentile=' + repr(value) for value in PERCENTILES] if CASES[name][2] == 'percentile' else ['']


def band_count(name):
    return len(descriptions(name))


def output_shape(name):
    return (band_count(name), *source_shape(CASES[name][0]))


def artifact_expectation(name):
    count, height, width = output_shape(name)
    a, b, c, d, e, f = GRID
    corners = [(a + b * x + c * y, d + e * x + f * y) for x in (0, width) for y in (0, height)]
    return {
        'width': width,
        'height': height,
        'band_count': count,
        'source_crs': f'EPSG:{EPSG}',
        'extent_srid': EPSG,
        'transform': list(GRID),
        'band_nodata': None,
        'extent': [
            min(x for x, _ in corners),
            min(y for _, y in corners),
            max(x for x, _ in corners),
            max(y for _, y in corners),
        ],
    }


def scalar_value(method, sample, centre):
    if not sample:
        return (math.nan,)
    values = [value for value, _ in sample]
    if method == 'mode':
        counts = Counter(values)
        most = max(counts.values())
        tied = [value for value, count in counts.items() if count == most]
        return (centre if centre in tied else min(tied),)
    if method == 'variety':
        return (len(set(values)),)
    if method == 'min':
        return (min(values),)
    if method == 'max':
        return (max(values),)
    if method in ('median', 'percentile'):
        values.sort()
        result = []
        for percentile in (50.0,) if method == 'median' else PERCENTILES:
            rank = Fraction(percentile) * (len(values) - 1) / 100
            lower = math.floor(rank)
            upper = math.ceil(rank)
            result.append(
                float((upper - rank) * Fraction(values[lower]) + (rank - lower) * Fraction(values[upper]))
                if upper != lower
                else values[lower]
            )
        return tuple(result)

    pairs = [(Fraction(value), Fraction(weight)) for value, weight in sample]
    total = sum(value * weight for value, weight in pairs)
    if method in ('sum', 'weighted_sum'):
        return (float(total),)
    mass = sum(weight for _, weight in pairs)
    mean = total / mass
    if method in ('mean', 'weighted_mean'):
        return (float(mean),)
    variance = sum(weight * (value - mean) ** 2 for value, weight in pairs) / mass
    with localcontext() as context:
        context.prec = 200
        return (float((Decimal(variance.numerator) / Decimal(variance.denominator)).sqrt()),)


@lru_cache(maxsize=len(CASES))
def expected_pixels(name, *, no_halo=False, flip_weights=False):
    """Independent scalar enumeration in image coordinates; memoize equal samples."""
    import numpy as np

    source, size, method, kernel, reclassify = CASES[name]
    values, alpha, mask = source_arrays(source)
    valid = np.isfinite(values) & (values != NODATA) & np.isfinite(alpha) & (alpha > 0) & (mask != 0)
    if reclassify:
        values = np.where(values < 0, 0.0, np.where(values < 10, 10.0, 20.0))
    weights = KERNELS[kernel] if kernel else ((1,) * size,) * size
    if flip_weights:
        weights = tuple(tuple(reversed(row)) for row in reversed(weights))
    radius = size // 2
    offsets = [
        (i - radius, j - radius, weight)
        for i, row in enumerate(weights)
        for j, weight in enumerate(row)
        if weight > 0
    ]
    answer = np.full(output_shape(name), np.nan)
    height, width = values.shape
    known = {}
    for y, x in zip(*np.nonzero(valid)):
        sample = tuple(
            (float(values[y + dy, x + dx]), weight)
            for dy, dx, weight in offsets
            if 0 <= y + dy < height
            and 0 <= x + dx < width
            and valid[y + dy, x + dx]
            and (not no_halo or ((y + dy) // 360 == y // 360 and (x + dx) // 360 == x // 360))
        )
        centre = float(values[y, x]) if method == 'mode' else None
        key = sample, centre
        if key not in known:
            known[key] = scalar_value(method, sample, centre)
        answer[:, y, x] = known[key]
    answer.flags.writeable = False
    return answer


def overview_sizes(height, width):
    sizes = []
    while max(height, width) > BLOCK_SIZE:
        height, width = max(1, height // 2), max(1, width // 2)
        sizes.append((width, height))
    return sizes


def overview_count(name):
    return len(overview_sizes(*output_shape(name)[1:]))


def assert_pixels(actual, expected, name):
    import numpy as np

    if not np.array_equal(actual, expected, equal_nan=True):
        raise FixtureError(f'complete independent pixel values differ for {name}')


def check_band(band, wanted, name, description):
    import numpy as np
    from osgeo import gdal

    nodata = band.GetNoDataValue()
    if (
        band.DataType != gdal.GDT_Float64
        or nodata is None
        or not math.isnan(nodata)
        or band.GetScale() is not None
        or band.GetOffset() is not None
        or band.GetUnitType()
        or band.GetColorInterpretation() == gdal.GCI_AlphaBand
        or band.GetDescription() != description
        or tuple(band.GetBlockSize()) != (BLOCK_SIZE, BLOCK_SIZE)
    ):
        raise FixtureError('COG type/NaN/description/metadata/block size differs')
    data = band.ReadAsArray()
    mask = band.GetMaskBand().ReadAsArray()
    assert_pixels(data, wanted, name)
    if not np.array_equal(mask, np.where(np.isfinite(wanted), 255, 0)):
        raise FixtureError('complete band mask differs')
    sizes = overview_sizes(*wanted.shape)
    if band.GetOverviewCount() != len(sizes):
        raise FixtureError('incorrect exact overview count')
    for index, (width, height) in enumerate(sizes):
        level = band.GetOverview(index)
        if (level.XSize, level.YSize) != (width, height):
            raise FixtureError('overview dimensions differ')
        ys = np.floor(0.5 + np.arange(height) * wanted.shape[0] / height).astype(int)
        xs = np.floor(0.5 + np.arange(width) * wanted.shape[1] / width).astype(int)
        wanted = wanted[ys[:, None], xs]
        assert_pixels(level.ReadAsArray(), wanted, name)
        if not np.array_equal(level.GetMaskBand().ReadAsArray(), np.where(np.isfinite(wanted), 255, 0)):
            raise FixtureError('complete overview mask differs')
    return data, mask


def actual_pairs(names):
    return [list(pair) for pair in COMPARISONS if pair[0] in names and pair[2] in names]


def verify(root, names, expectations, sources):
    import numpy as np
    from osgeo import gdal
    from osgeo_utils.samples.validate_cloud_optimized_geotiff import validate

    if len(set(names)) != len(names) or any(name not in CASES for name in names):
        raise FixtureError('unknown output inventory')
    if {p.name for p in root.iterdir()} != {*SOURCES, 'outputs', '.addp-online-owner', ORACLE} or {
        p.name for p in (root / 'outputs').iterdir()
    } != set(names):
        raise FixtureError('unexpected private file or output')
    result = {'sources': sources, 'targets': {}, 'full_cog_checks': len(names)}
    actual = {}
    for name in names:
        count, height, width = output_shape(name)
        path = root / 'outputs' / name
        facts = digest(path)
        expected = expectations[name]
        if expected.shape != (count, height, width) or expected.dtype != np.dtype('float64'):
            raise FixtureError('frozen expected array shape/type differs')
        dataset = gdal.Open(str(path))
        if (
            dataset is None
            or dataset.GetFileList() != [str(path)]
            or dataset.RasterCount != count
            or (dataset.RasterYSize, dataset.RasterXSize) != (height, width)
            or dataset.GetGeoTransform(can_return_null=True) != GRID
            or not same_crs(dataset)
            or dataset.GetMetadata('IMAGE_STRUCTURE').get('LAYOUT') != 'COG'
        ):
            raise FixtureError('complete COG/grid/CRS/band count differs')
        actual[name] = [
            check_band(dataset.GetRasterBand(index), wanted, name, description)
            for index, (wanted, description) in enumerate(zip(expected, descriptions(name)), 1)
        ]
        _, errors, _ = validate(dataset, full_check=True)
        if errors:
            raise FixtureError('COG layout invalid')
        dataset = None
        result['targets'][name] = {
            **facts,
            'cog_valid': True,
            'full_pixel_and_mask_verification': True,
            'independent_scalar_oracle_verified': True,
            'band_order_and_descriptions_verified': True,
            'nearest_overviews_verified': True,
            'band_count': count,
            'band_descriptions': descriptions(name),
            'overview_count': overview_count(name),
            'has_overviews': bool(overview_count(name)),
        }
    for left, left_band, right, right_band in actual_pairs(names):
        for a, b in zip(actual[left][left_band], actual[right][right_band]):
            if not np.array_equal(a, b, equal_nan=True):
                raise FixtureError('actual same-source COG control pixels or masks differ')
    result['actual_control_pairs_verified'] = actual_pairs(names)
    return result


def oracle_scope(root, script_sha, sources):
    marker = root / '.addp-online-owner'
    if (
        root.is_symlink()
        or root.resolve() != root
        or marker.is_symlink()
        or not marker.is_file()
        or not re.fullmatch(PREFIX + r'-[0-9]+-[0-9]+', marker.read_text())
    ):
        raise FixtureError('invalid owned oracle root/run')
    return {
        'schema': ORACLE_SCHEMA,
        'root': str(root),
        'run_id': marker.read_text(),
        'fixture_sha256': script_sha,
        'source_files': sources,
    }


def worker(action, root, names):
    import numpy as np

    path = root / ORACLE
    if action == 'seed' and not names:
        return seed(root)
    if action == 'cleanup-oracle' and not names:
        if path.is_symlink() or (path.exists() and not path.is_file()):
            raise FixtureError('unsafe oracle cleanup path')
        path.unlink(missing_ok=True)
        return {'oracle_cache_removed': not path.exists()}
    if action == 'oracle' and len(names) == 1 and re.fullmatch('[0-9a-f]{64}', names[0]):
        sources = verify_sources(root)
        scope = oracle_scope(root, names[0], sources)
        with path.open('xb') as stream:
            np.savez(
                stream,
                scope=np.frombuffer(encoded(scope), dtype=np.uint8),
                **{name: expected_pixels(name) for name in CASES},
            )
        path.chmod(0o444)
        anchor = digest(path)['sha256']
        names = []
    elif action == 'verify' and len(names) >= 2 and all(re.fullmatch('[0-9a-f]{64}', value) for value in names[:2]):
        anchor, script_sha, *names = names
        scope = oracle_scope(root, script_sha, {source: digest(root / source) for source in SOURCES})
        sources = scope['source_files']
    else:
        raise FixtureError('unknown oracle action or anchor')
    if path.is_symlink() or not path.is_file() or path.stat().st_mode & 0o222:
        raise FixtureError('frozen oracle must be an ordinary read-only file')
    data = path.read_bytes()
    if hashlib.sha256(data).hexdigest() != anchor:
        raise FixtureError('oracle cache hash changed')
    if list(Path(tempfile.gettempdir()).glob('addp-raster-*')):
        raise FixtureError('Runtime execution workspace residual')
    with np.load(io.BytesIO(data), allow_pickle=False) as expected:
        if (
            len(expected.files) != len(CASES) + 1
            or set(expected.files) != {*CASES, 'scope'}
            or expected['scope'].tobytes() != encoded(scope)
        ):
            raise FixtureError('frozen oracle inventory or scope differs')
        result = verify(root, names, expected, sources)
    return {
        **result,
        'runtime_workspaces_removed': True,
        'oracle_sha256': anchor,
        'full_source_pixels_verified_at_baseline': True,
    }


def runtime_worker(action, root, names=()):
    return run_runtime_worker(command, RUNTIME, SUITE, Path(__file__), action, root, names)


def main(action):
    nfs = NFSExport(PREFIX, command)
    secret = nfs.guard(os.environ)
    run = os.environ['ADDP_ONLINE_TEST_RUN_ID']
    if action in ('start', 'stop'):
        return nfs.lifecycle(action, secret, run)
    root = nfs.owned_root(secret, run)
    state = secret / (PREFIX + '-source.json')
    names = sys.argv[2:]
    if action == 'oracle':
        arguments = [digest(Path(__file__))['sha256']]
    elif action == 'verify':
        arguments = [
            os.environ['ADDP_ONLINE_FOCAL_SQUARE_SEVEN_ORACLE_SHA256'],
            digest(Path(__file__))['sha256'],
            *names,
        ]
    elif action in ('seed', 'cleanup-oracle') and not names:
        arguments = []
    else:
        raise FixtureError('unknown fixture action')
    result = runtime_worker(action, root, arguments)
    if action == 'seed':
        with state.open('xb') as stream:
            stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']:
            raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret / (PREFIX + '-archives.json')
        if action == 'verify' and not history.is_file():
            raise FixtureError('missing immutable archive history')
        previous = (
            json.loads(history.read_bytes()) if history.is_file() else {'sources': {}, 'targets': {}, 'oracle': {}}
        )
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, previous['sources'], PREFIX + '-sources')
        result['archived_oracle'] = archive_files(root, artifacts, [ORACLE], previous['oracle'], PREFIX + '-oracle')
        result['archived_cogs'] = archive_files(
            root / 'outputs', artifacts, names, previous['targets'], PREFIX + '-cogs'
        )
        history.write_bytes(
            encoded(
                {
                    'sources': result['archived_sources'],
                    'targets': result['archived_cogs'],
                    'oracle': result['archived_oracle'],
                }
            )
        )
    return result


if __name__ == '__main__':
    try:
        result = (
            worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:])
            if sys.argv[1:2] == ['--worker']
            else main(sys.argv[1])
        )
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'Seven-pixel square fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
