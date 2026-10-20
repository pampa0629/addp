#!/usr/bin/env python3
"""Owned NFS sources and an independent full-domain OIF object oracle."""
from __future__ import annotations

import hashlib
import itertools
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-band-recommendation-workflow'
RUNTIME = 'addp-raster-band-recommendation-runtime'
SOURCES = ('candidates.tif', 'orthogonal.tif')
WIDTH, HEIGHT = 519, 515
TRANSFORM = (110., 1 / 1024, 1 / 8192, 30., 1 / 4096, -1 / 512)
CASES = {
    'full': {'source': SOURCES[0], 'bands': list(range(1, 17))},
    'full-reversed': {'source': SOURCES[0], 'bands': list(range(16, 0, -1))},
    'orthogonal': {'source': SOURCES[1], 'bands': [4, 2, 3, 1]},
}
FAILURES = {
    'band': {'source': SOURCES[0], 'bands': [1, 2, 99]},
    'duplicates': {'source': SOURCES[1], 'bands': [1, 2, 2]},
    'no-common': {'source': SOURCES[1], 'bands': [1, 2, 7]},
    'one-common': {'source': SOURCES[1], 'bands': [1, 2, 8]},
    'constants': {'source': SOURCES[1], 'bands': [5, 6, 9]},
}


class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker


def guard(env=os.environ):
    return NFSExport('band-recommendation', command).guard(env)


def owned_root(secret):
    return NFSExport('band-recommendation', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a positive-size ordinary source file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}


def source_arrays(name):
    import numpy as np
    if name == SOURCES[0]:
        rng = np.random.default_rng(1701)
        # Keep generated values within ±7500, distinct from explicit -9999 NoData.
        data = rng.integers(-500, 501, (17, HEIGHT, WIDTH)).astype(np.float64)
        data[:15] *= np.arange(1, 16)[:, None, None]
        data[15] = 4.
        data[16] = np.nan  # Unselected invalid data cannot restrict the common domain.
        for index, (row, column) in enumerate(((0, 1), (511, 513), (513, 511), (514, 518))):
            data[index, row, column] = [-9999., np.nan, np.inf, -np.inf][index]
        data[15, 512, 512] = -9999.  # A constant candidate still restricts the shared domain.
        data[0, 0, 0] = 0.
        alpha = np.full((HEIGHT, WIDTH), 128.)
        alpha[2, 0], alpha[2, 1], alpha[2, 2], alpha[2, 3] = 0., -1., np.nan, np.inf
        mask = np.full((HEIGHT, WIDTH), 255, np.uint8)
        mask[510, 511] = 0
        return data, alpha, mask
    if name == SOURCES[1]:
        first = np.array([1., 1., -1., -1., 1., 1., -1., -1.])
        second = np.array([1., -1., 1., -1., 1., -1., 1., -1.])
        third = first * second
        data = np.stack([first, second, third, first, np.full(8, 4.), np.full(8, -3.),
                         np.full(8, -9999.), np.array([1., *([-9999.] * 7)]), np.full(8, 9.)]).reshape(9, 2, 4)
        return data, np.full((2, 4), 128.), np.full((2, 4), 255, np.uint8)
    raise FixtureError('unknown exact source')


def seed(root):
    from osgeo import gdal, osr
    if any((root / name).exists() for name in SOURCES) or list((root / 'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing source or output')
    reference = osr.SpatialReference()
    reference.ImportFromEPSG(4326)
    for name in SOURCES:
        data, alpha, mask = source_arrays(name)
        height, width = alpha.shape
        dataset = gdal.GetDriverByName('GTiff').Create(str(root / name), width, height, len(data) + 1, gdal.GDT_Float64)
        dataset.SetProjection(reference.ExportToWkt())
        dataset.SetGeoTransform(TRANSFORM)
        for index, array in enumerate([*data, alpha], 1):
            band = dataset.GetRasterBand(index)
            band.WriteArray(array)
            band.SetNoDataValue(-9999.)
            band.SetScale(17.)
            band.SetOffset(500.)
        dataset.GetRasterBand(len(data) + 1).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
            dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
        dataset.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        band = dataset = None
    return {'sources': {name: digest(root / name) for name in SOURCES}}


def expected_result(name):
    """Full-array NumPy statistics, independent of product scaling and block passes."""
    import numpy as np
    case = CASES[name]
    bands = sorted(case['bands'])
    arrays, alpha, mask = source_arrays(case['source'])
    selected = arrays[np.asarray(bands) - 1]
    common = ((selected != -9999) & np.isfinite(selected)).all(axis=0) & np.isfinite(alpha) & (alpha > 0) & (mask > 0)
    values = np.ascontiguousarray(selected[:, common])
    count = int(common.sum())
    means = values.mean(axis=1)
    stddev = values.std(axis=1, ddof=0)
    eligible = np.flatnonzero(stddev > 0)
    matrix = [[None] * len(bands) for _ in bands]
    correlation = np.corrcoef(values[eligible])
    for i, left in enumerate(eligible):
        for j, right in enumerate(eligible):
            matrix[int(left)][int(right)] = float(correlation[i, j])
    ranked, excluded = [], []
    for indices in itertools.combinations(range(len(bands)), 3):
        combination = [bands[index] for index in indices]
        if any(stddev[index] == 0 for index in indices):
            excluded.append({'bands': combination, 'reason': 'zero_variance'})
        else:
            denominator = sum(abs(matrix[i][j]) for i, j in itertools.combinations(indices, 2))
            score = None if denominator == 0 else sum(float(stddev[index]) for index in indices) / denominator
            ranked.append({'bands': combination, 'score_status': 'unbounded' if score is None else 'finite', 'score': score})
    ranked.sort(key=lambda row: (row['score_status'] != 'unbounded', -(row['score'] or 0), row['bands']))
    for rank, row in enumerate(ranked, 1):
        row['rank'] = rank
    result = {'method': 'oif', 'strategy': 'full', 'value_domain': 'stored', 'candidate_bands': bands,
              'common_valid_count': count, 'common_invalid_count': int(common.size - count),
              'band_statistics': [{'band': band, 'mean': float(means[i]), 'stddev': float(stddev[i]),
                                   'score_status': 'eligible' if stddev[i] > 0 else 'zero_variance'} for i, band in enumerate(bands)],
              'correlation_matrix': matrix, 'rankings': ranked, 'excluded_combinations': excluded,
              'recommended_bands': ranked[0]['bands']}
    json.dumps(result, allow_nan=False)
    return result


def verify(root):
    import numpy as np
    from osgeo import gdal, osr
    if sorted(path.name for path in root.iterdir()) != sorted([*SOURCES, 'outputs', '.addp-online-owner']):
        raise FixtureError('unexpected source, sidecar or temporary file')
    if list((root / 'outputs').iterdir()):
        raise FixtureError('read-only workflow created a physical output')
    result = {'sources': {}, 'source_physical': {}, 'no_outputs': True}
    reference = osr.SpatialReference()
    reference.ImportFromEPSG(4326)
    for name in SOURCES:
        fact = digest(root / name)
        values, alpha, mask = source_arrays(name)
        dataset = gdal.Open(str(root / name))
        if dataset is None or dataset.GetDriver().ShortName != 'GTiff' or dataset.RasterCount != len(values) + 1:
            raise FixtureError('source driver or band count changed')
        if (dataset.RasterYSize, dataset.RasterXSize) != alpha.shape or dataset.GetGeoTransform() != TRANSFORM:
            raise FixtureError('source dimensions or rotated grid changed')
        actual_crs = osr.SpatialReference(wkt=dataset.GetProjection())
        if not actual_crs.IsSame(reference) or dataset.GetFileList() != [str(root / name)]:
            raise FixtureError('source CRS or self-contained file inventory changed')
        for index, expected in enumerate([*values, alpha], 1):
            band = dataset.GetRasterBand(index)
            if (band.DataType != gdal.GDT_Float64 or band.GetNoDataValue() != -9999.
                or band.GetScale() != 17. or band.GetOffset() != 500.
                or band.GetColorInterpretation() != (gdal.GCI_AlphaBand if index == len(values) + 1 else gdal.GCI_GrayIndex if index == 1 else gdal.GCI_Undefined)):
                raise FixtureError('source encoded numeric facts changed')
            if not np.array_equal(band.ReadAsArray(), expected, equal_nan=True):
                raise FixtureError('complete source pixels changed')
            if not np.array_equal(band.GetMaskBand().ReadAsArray(), mask):
                raise FixtureError('complete source mask changed')
        band = dataset = None
        result['sources'][name] = fact
        result['source_physical'][name] = {**fact, 'full_pixels_and_mask_verified': True, 'grid_crs_scale_verified': True}
    result['expectations'] = {name: expected_result(name) for name in CASES}
    return result


def worker(action, root, names):
    if names or action not in {'seed', 'verify'}:
        raise FixtureError('unknown fixture action or extra target names')
    if action == 'seed':
        return seed(root)
    if list(Path(tempfile.gettempdir()).glob('addp-raster-*')):
        raise FixtureError('Runtime raster execution workspace has residuals')
    return {**verify(root), 'runtime_workspaces_removed': True}


def runtime_worker(action, root, names=()):
    return run_runtime_worker(command, RUNTIME, SUITE, Path(__file__), action, root, names)


def main(action):
    secret = guard()
    if action in {'start', 'stop'}:
        return NFSExport('band-recommendation', command).lifecycle(action, secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root = owned_root(secret)
    state = secret / 'band-recommendation-source.json'
    if action == 'seed':
        result = runtime_worker('seed', root)
        state.write_text(json.dumps(result))
        return result
    if action == 'verify':
        result = runtime_worker('verify', root, sys.argv[2:])
        if result['sources'] != json.loads(state.read_text())['sources']:
            raise FixtureError('source bytes changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'OIF fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
