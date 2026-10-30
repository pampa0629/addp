#!/usr/bin/env python3
"""Owned read-only full-raster statistics sources and independent scalar oracle."""
from __future__ import annotations

from fractions import Fraction
import hashlib
import json
import math
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

SUITE = 'raster-statistics-percentile-workflow'
RUNTIME = 'addp-raster-statistics-percentile-runtime'
PREFIX = 'statistics-percentile'
WIDTH, HEIGHT = 1025, 513
GRID = (100., 2., .25, 200., .125, -3.)
CASES = {
    'original': {'source': 'values.tif', 'band': 2},
    'p0': {'source': 'values.tif', 'band': 2, 'percentile': 0},
    'p100': {'source': 'values.tif', 'band': 2, 'percentile': 100},
    'p50-even': {'source': 'even.tif', 'band': 2, 'percentile': 50},
    'p50-odd': {'source': 'odd.tif', 'band': 2, 'percentile': 50},
    'p25': {'source': 'values.tif', 'band': 2, 'percentile': 25},
    'p75': {'source': 'values.tif', 'band': 2, 'percentile': 75},
    'p33': {'source': 'values.tif', 'band': 2, 'percentile': 33.3},
    'p33-reversed': {'source': 'reversed.tif', 'band': 2, 'percentile': 33.3},
    'reclassified': {'source': 'values.tif', 'band': 2, 'percentile': 90, 'stage': 'reclassify'},
    'int64': {'source': 'int64.tif', 'band': 2, 'percentile': 50},
    'uint64': {'source': 'uint64.tif', 'band': 2, 'percentile': 50},
    'coverage': {'source': 'coverage.tif', 'band': 2, 'percentile': 50},
    'single': {'source': 'single.tif', 'band': 2, 'percentile': 33.3},
    'invalid': {'source': 'invalid.tif', 'band': 2, 'percentile': 50},
    'unlocated': {'source': 'unlocated.tif', 'band': 2, 'percentile': 25},
}
FAILURES = ('percentile_null', 'percentile_boolean', 'percentile_string', 'percentile_array',
            'percentile_object', 'percentile_low', 'percentile_high',
            'band_zero', 'band_high', 'band_boolean', 'band_string', 'complex')
SOURCES = tuple(dict.fromkeys([case['source'] for case in CASES.values()] + ['complex.tif']))


class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker


def guard(env=os.environ):
    return NFSExport(PREFIX, command).guard(env)


def owned_root(secret):
    return NFSExport(PREFIX, command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a positive-size ordinary file')
    return {'size_bytes': path.stat().st_size, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}


def source_arrays(name):
    import numpy as np
    dtype, nodata, located = 'Float64', -9999., name != 'unlocated.tif'
    if name in ('values.tif', 'reversed.tif'):
        y, x = np.indices((HEIGHT, WIDTH))
        values = ((17*x + 31*y) % 2049 - 1024).astype(float)/4
        values[0, 0], values[-1, -1] = 0., 9.125
        values[511, 512], values[512, 511] = -9999., np.nan
        alpha = np.full(values.shape, 128.); alpha[1, :4] = [0., -1., np.nan, np.inf]
        mask = np.full(values.shape, 255, np.uint8); mask[510, 511] = 0
    elif name in ('int64.tif', 'uint64.tif'):
        dtype = 'Int64' if name == 'int64.tif' else 'UInt64'
        nodata = 2**63-1 if dtype == 'Int64' else 2**64-1
        values = np.array([[nodata, nodata-1, 0, 12]], dtype=np.int64 if dtype == 'Int64' else np.uint64)
        alpha = np.full(values.shape, 128, dtype=values.dtype); mask = np.full(values.shape, 255, np.uint8)
    else:
        samples = {'even.tif': [[-7., 0., 4., 12.]], 'odd.tif': [[-8., 0., 0., 5., 11.]],
                   'coverage.tif': [[0., -6., 8., -9999.], [4., 12., np.nan, np.inf]],
                   'single.tif': [[-9999., 0., -9999.]], 'invalid.tif': [[-9999., -9999.]],
                   'unlocated.tif': [[-4., 0., 8., 12.]], 'complex.tif': [[1+2j, 3+4j]]}
        if name not in samples:
            raise FixtureError('unknown exact source')
        values = np.array(samples[name], dtype=np.complex128 if name == 'complex.tif' else float)
        if name == 'complex.tif': dtype = 'CFloat64'
        alpha = np.full(values.shape, 128., dtype=values.dtype); mask = np.full(values.shape, 255, np.uint8)
        if name == 'coverage.tif':
            alpha[:] = [[128., 255., 0., 255.], [255., np.nan, 255., 255.]]
            mask[1, 0] = 0
    data = np.stack([np.full(values.shape, 7, dtype=values.dtype), values,
                     np.full(values.shape, nodata, dtype=values.dtype), alpha])
    if name == 'reversed.tif': data, mask = data[:, ::-1, ::-1].copy(), mask[::-1, ::-1].copy()
    return data, mask, dtype, nodata, located


def seed(root):
    from osgeo import gdal, osr
    if any((root/name).exists() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite sources or previous outputs')
    reference = osr.SpatialReference(); reference.ImportFromEPSG(3857)
    for name in SOURCES:
        data, mask, dtype, nodata, located = source_arrays(name)
        dataset = gdal.GetDriverByName('GTiff').Create(str(root/name), data.shape[2], data.shape[1], 4, getattr(gdal, 'GDT_'+dtype))
        if located: dataset.SetProjection(reference.ExportToWkt()); dataset.SetGeoTransform(GRID)
        for index, values in enumerate(data, 1):
            band = dataset.GetRasterBand(index); band.WriteArray(values)
            if dtype == 'Int64': band.SetNoDataValueAsInt64(nodata)
            elif dtype == 'UInt64': band.SetNoDataValueAsUInt64(nodata)
            else: band.SetNoDataValue(nodata)
            band.SetScale(17.); band.SetOffset(500.)
        dataset.GetRasterBand(4).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'): dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        band = dataset = None
    return {'sources': {name: digest(root/name) for name in SOURCES}}


def class_rules():
    return [{'min': None, 'max': 0, 'class': -2}, {'min': 0, 'max': None, 'class': 3}]


def expected_population(case):
    """Source values and validity independently enumerated before Float64 conversion."""
    import numpy as np
    data, mask, _, nodata, _ = source_arrays(case['source'])
    values, alpha = data[case['band']-1], data[3]
    valid = (values != nodata) & np.isfinite(values) & (mask != 0) & np.isfinite(alpha) & (alpha > 0)
    samples = [float(value) for value in values[valid]]
    if case.get('stage') == 'reclassify': samples = [-2. if value < 0 else 3. for value in samples]
    return sorted(samples), int(values.size)


def expected_result(case, ordered, total):
    count = len(ordered)
    mean = math.fsum(ordered)/count if count else None
    stddev = math.sqrt(math.fsum((value-mean)**2 for value in ordered)/count) if count else None
    result = {'band': 1 if case.get('stage') == 'reclassify' else case['band'], 'strategy': 'full',
              'valid_count': count, 'invalid_count': total-count, 'nodata_ratio': (total-count)/total,
              'min': ordered[0] if count else None, 'max': ordered[-1] if count else None, 'mean': mean, 'stddev': stddev}
    if 'percentile' in case:
        p = float(case['percentile']); value = None
        if count:
            position = (count-1)*Fraction(p)/100
            low = position.numerator//position.denominator; weight = position-low
            high = low + bool(weight)
            value = float(Fraction(ordered[low])*(1-weight) + Fraction(ordered[high])*weight)
        result.update(percentile=p, percentile_value=value)
    json.dumps(result, allow_nan=False)
    return result


def oracle():
    populations, results = {}, {}
    for name, case in CASES.items():
        key = (case['source'], case.get('stage'))
        if key not in populations: populations[key] = expected_population(case)
        results[name] = expected_result(case, *populations[key])
    return results


def verify(root):
    import numpy as np
    from osgeo import gdal, osr
    if sorted(path.name for path in root.iterdir()) != sorted([*SOURCES, 'outputs', '.addp-online-owner']):
        raise FixtureError('unexpected source, sidecar or temporary file')
    if list((root/'outputs').iterdir()): raise FixtureError('read-only statistics published a physical file')
    reference = osr.SpatialReference(); reference.ImportFromEPSG(3857)
    result = {'sources': {}, 'source_physical': {}, 'no_outputs': True}
    for name in SOURCES:
        data, mask, dtype, nodata, located = source_arrays(name)
        dataset = gdal.Open(str(root/name)); fact = digest(root/name)
        if (not dataset or dataset.GetDriver().ShortName != 'GTiff' or dataset.RasterCount != 4
            or (dataset.RasterYSize, dataset.RasterXSize) != mask.shape or dataset.GetFileList() != [str(root/name)]):
            raise FixtureError('source driver/dimensions/self-contained inventory changed')
        if located:
            if dataset.GetGeoTransform() != GRID or not osr.SpatialReference(wkt=dataset.GetProjection()).IsSame(reference):
                raise FixtureError('source rotated grid/CRS changed')
        elif dataset.GetProjection() or dataset.GetGeoTransform(can_return_null=True) is not None:
            raise FixtureError('missing source location was fabricated')
        for index, expected in enumerate(data, 1):
            band = dataset.GetRasterBand(index)
            if (band.DataType != getattr(gdal, 'GDT_'+dtype) or band.GetNoDataValue() != nodata
                or band.GetScale() != 17. or band.GetOffset() != 500.
                or band.GetColorInterpretation() != (gdal.GCI_AlphaBand if index == 4 else gdal.GCI_GrayIndex if index == 1 else gdal.GCI_Undefined)
                or not np.array_equal(band.ReadAsArray(), expected, equal_nan=True)
                or not np.array_equal(band.GetMaskBand().ReadAsArray(), mask)):
                raise FixtureError('complete source pixels/mask/type/NoData/scale/offset changed')
        band = dataset = None
        result['sources'][name] = fact
        result['source_physical'][name] = {**fact, 'full_pixels_and_mask_verified': True, 'grid_crs_scale_verified': True}
    return result


def worker(action, root, names):
    if names or action not in {'seed', 'verify', 'oracle'}: raise FixtureError('unknown action or extra names')
    if action == 'seed': return seed(root)
    if list(Path(tempfile.gettempdir()).glob('addp-raster-*')): raise FixtureError('Runtime execution workspace residual')
    result = {**verify(root), 'runtime_workspaces_removed': True}
    if action == 'oracle': result['expectations'] = oracle()
    return result


def runtime_worker(action, root, names=()):
    return run_runtime_worker(command, RUNTIME, SUITE, Path(__file__), action, root, names)


def archive_sources(root, artifact_dir, facts):
    directory = artifact_dir/'statistics-sources'; manifest = artifact_dir/'statistics-sources-manifest.json'
    expected = {name: {'path': 'statistics-sources/'+name, **facts[name]} for name in SOURCES}
    if directory.exists() or manifest.exists():
        if (directory.is_symlink() or manifest.is_symlink() or not directory.is_dir() or not manifest.is_file()
            or json.loads(manifest.read_text()) != expected or set(p.name for p in directory.iterdir()) != set(SOURCES)):
            raise FixtureError('previous source archive inventory changed')
    else:
        directory.mkdir()
        for name in SOURCES:
            with (root/name).open('rb') as source, (directory/name).open('xb') as target: shutil.copyfileobj(source, target)
            (directory/name).chmod(0o444)
        with manifest.open('x') as stream: json.dump(expected, stream, sort_keys=True)
        manifest.chmod(0o444)
    for name in SOURCES:
        if digest(directory/name) != facts[name]: raise FixtureError('previous archived source changed')
    return expected


def main(action):
    secret = guard()
    if action in {'start', 'stop'}: return NFSExport(PREFIX, command).lifecycle(action, secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root = owned_root(secret); state = secret/(PREFIX+'-source.json')
    result = runtime_worker(action, root, sys.argv[2:])
    if action == 'seed':
        with state.open('x') as stream: json.dump(result, stream)
    else:
        if result['sources'] != json.loads(state.read_text())['sources']: raise FixtureError('source file changed')
        result['archived_sources'] = archive_sources(root, Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']), result['sources'])
    return result


if __name__ == '__main__':
    try:
        value = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(value, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'statistics percentile fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
