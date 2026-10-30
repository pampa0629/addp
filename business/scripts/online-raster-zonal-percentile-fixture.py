#!/usr/bin/env python3
"""Owned raster pairs and independent complete zonal CSV verification."""
from __future__ import annotations
import csv
from decimal import Decimal, InvalidOperation
from fractions import Fraction
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shutil
import statistics
import subprocess
import sys
import tempfile

SUITE = 'raster-zonal-percentile-persistence'
PREFIX = 'zonal-percentile'
RUNTIME = 'addp-raster-zonal-percentile-runtime'
WIDTH, HEIGHT = 1025, 513
GRID = (100., 2., .25, 200., .125, -3.)
ORACLE = '.zonal-percentile-oracle.json'
BASE_COLUMNS = ('zone_value', 'total_count', 'valid_count', 'invalid_count', 'sum', 'min', 'max', 'mean', 'stddev')
CASES = {
    'original.csv': ('values.tif', 'zones.tif', None, 2),
    'p0.csv': ('values.tif', 'zones.tif', 0, 2),
    'p100.csv': ('values.tif', 'zones.tif', 100, 2),
    'p50-odd.csv': ('odd.tif', 'small-zones.tif', 50, 2),
    'p50-even.csv': ('even.tif', 'small-zones.tif', 50, 2),
    'p25.csv': ('values.tif', 'zones.tif', 25, 2),
    'p75.csv': ('values.tif', 'zones.tif', 75, 2),
    'p33.csv': ('values.tif', 'zones.tif', 33.3, 2),
    'p33-reversed.csv': ('reversed.tif', 'zones.tif', 33.3, 2),
    'reclassified.csv': ('values.tif', 'zones.tif', 90, 2),
    'many-zones.csv': ('values.tif', 'zones.tif', 50, 1),
    'int64.csv': ('int64.tif', 'large-zones.tif', 50, 2),
    'uint64.csv': ('uint64.tif', 'large-zones.tif', 50, 2),
    'coverage.csv': ('coverage.tif', 'coverage-zones.tif', 50, 2),
    'invalid-values.csv': ('invalid.tif', 'small-zones.tif', 50, 2),
    'empty-zones.csv': ('even.tif', 'empty-zones.tif', 50, 2),
}
FAILURES = ('percentile_null', 'percentile_boolean', 'percentile_string', 'percentile_array', 'percentile_object', 'percentile_low', 'percentile_high',
            'values_band_zero', 'values_band_high', 'values_band_boolean', 'values_band_string',
            'zones_band_zero', 'zones_band_high', 'zones_band_boolean', 'zones_band_string',
            'values_complex', 'zones_complex', 'values_alpha', 'zones_alpha', 'fractional_zone', 'overflow_zone',
            'missing_crs', 'different_crs', 'missing_grid', 'singular_grid', 'different_size', 'shifted_grid', 'rotated_grid')
SOURCES = tuple(dict.fromkeys([source for case in CASES.values() for source in case[:2]] +
                ['complex.tif', 'fractional-zone.tif', 'overflow-zone.tif'] + [name+'.tif' for name in FAILURES[-7:]]))

class FixtureError(RuntimeError): pass

def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()

if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker

def guard(env=os.environ): return NFSExport(PREFIX, command).guard(env)
def owned_root(secret): return NFSExport(PREFIX, command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])
def encoded(value): return (json.dumps(value, allow_nan=False, sort_keys=True)+'\n').encode()
def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0: raise FixtureError('expected a positive ordinary file')
    return {'size_bytes': path.stat().st_size, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}

def source_arrays(name):
    import numpy as np
    if name not in SOURCES: raise FixtureError('unknown exact source')
    dtype, nodata, crs, grid = 'Float64', -9999., 3857, GRID
    if name in ('values.tif', 'reversed.tif', 'zones.tif'):
        indices = np.arange(WIDTH*HEIGHT, dtype=np.int64).reshape(HEIGHT, WIDTH)
        symmetric = np.minimum(indices, indices.size-1-indices)
        zone = np.array([-3., 0., 2., 7., 11.])[symmetric % 5]
        if name == 'zones.tif':
            values = zone; first = (symmetric % 79).astype(float)
            mask = np.where(symmetric % 173 == 1, 0, 255).astype(np.uint8)
            alpha = np.where(symmetric % 181 == 2, 0., 128.)
            values[symmetric % 211 == 3] = nodata
        else:
            values = ((indices*37) % 997-490).astype(float)/4
            values[zone == 7] = nodata
            values[indices % 257 == 4] = np.nan
            values[indices % 263 == 5] = np.inf
            mask = np.where(indices % 167 == 6, 0, 255).astype(np.uint8)
            alpha = np.where(indices % 179 == 7, 0., 128.)
            first = np.full(values.shape, nodata)
    else:
        values = np.tile(np.array([[0., -6., 8., -9999., 4., 12., np.nan, np.inf]]), (3, 1))
        first = np.full(values.shape, nodata); mask = np.full(values.shape, 255, np.uint8); alpha = np.full(values.shape, 128.)
        if name in ('odd.tif', 'even.tif'):
            values[:] = np.array([-8., 0., 5., 11., nodata, nodata, nodata, nodata])
            if name == 'odd.tif': values[:, 3] = nodata
            values[2] = nodata; values[2, 0] = 0.
        elif name == 'invalid.tif': values[:] = nodata
        elif name in ('int64.tif', 'uint64.tif'):
            dtype = 'Int64' if name == 'int64.tif' else 'UInt64'
            nodata = 2**63-1 if dtype == 'Int64' else 2**64-1
            values = np.tile(np.array([[nodata, nodata-1, 0, 12, 6, 2, 8, 4]], dtype=dtype.lower()), (3, 1))
            first = np.zeros(values.shape, dtype=values.dtype); alpha = np.full(values.shape, 128, dtype=values.dtype)
        elif name == 'complex.tif':
            dtype = 'CFloat64'; values = values.astype(complex); values[:] = 1+2j
            alpha = np.full(values.shape, 128., dtype=complex)
        elif name == 'coverage.tif':
            alpha[0, :4] = [128., 255., 0., np.nan]; mask[1, 4] = 0
        else:
            values = np.tile(np.array([[0., 0., 0., 0., -3., -3., 7., 7.]]), (3, 1))
            if name == 'small-zones.tif': values = np.repeat(np.array([[0.], [-3.], [7.]]), 8, axis=1)
            if name == 'large-zones.tif':
                dtype = 'Int64'; nodata = -9999
                values = np.tile(np.array([[2**63-1]*4+[2**63-2]*2+[0]*2], dtype=np.int64), (3, 1))
                first = np.zeros(values.shape, dtype=np.int64); alpha = np.full(values.shape, 128, dtype=np.int64)
            if name == 'empty-zones.tif': values[:] = nodata
            if name == 'fractional-zone.tif': values[:] = .5
            if name == 'overflow-zone.tif':
                dtype = 'UInt64'; nodata = 2**64-1
                values = np.full(values.shape, 2**63, dtype=np.uint64)
                first = np.zeros(values.shape, dtype=np.uint64); alpha = np.full(values.shape, 128, dtype=np.uint64)
            if name == 'coverage-zones.tif': alpha[1, 3] = 0.; mask[2, 5] = 0
            if name == 'missing_crs.tif': crs = None
            if name == 'different_crs.tif': crs = 4326
            if name == 'missing_grid.tif': grid = None
            if name == 'singular_grid.tif': grid = (100., 1., 2., 200., 2., 4.)
            if name == 'shifted_grid.tif': grid = (101., *GRID[1:])
            if name == 'rotated_grid.tif': grid = (*GRID[:2], .5, *GRID[3:])
            if name == 'different_size.tif': values, first, mask, alpha = [array[:, :-1] for array in (values, first, mask, alpha)]
    data = np.stack([first.astype(values.dtype), values, alpha.astype(values.dtype)])
    if name == 'reversed.tif': data, mask = data[:, ::-1, ::-1].copy(), mask[::-1, ::-1].copy()
    return data, mask, dtype, nodata, crs, grid

def seed(root):
    from osgeo import gdal, osr
    if any((root/name).exists() for name in (*SOURCES, ORACLE)) or list((root/'outputs').iterdir()): raise FixtureError('refusing to overwrite source or previous result')
    for name in SOURCES:
        data, mask, dtype, nodata, crs, grid = source_arrays(name)
        ds = gdal.GetDriverByName('GTiff').Create(str(root/name), data.shape[2], data.shape[1], 3, getattr(gdal, 'GDT_'+dtype), options=['COMPRESS=DEFLATE'])
        if crs:
            reference = osr.SpatialReference(); reference.ImportFromEPSG(crs); ds.SetProjection(reference.ExportToWkt())
        if grid: ds.SetGeoTransform(grid)
        for index, values in enumerate(data, 1):
            band = ds.GetRasterBand(index); band.WriteArray(values)
            if dtype == 'Int64': band.SetNoDataValueAsInt64(nodata)
            elif dtype == 'UInt64': band.SetNoDataValueAsUInt64(nodata)
            else: band.SetNoDataValue(nodata)
            band.SetScale(17.); band.SetOffset(500.)
        ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'): ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask); band = ds = None
    return {'sources': {name: digest(root/name) for name in SOURCES}}

def class_rules(): return [{'value': value, 'class': 10 if value in (-3, 0) else 20 if value == 2 else 30} for value in (-3, 0, 2, 7, 11)]
def columns(name): return list(BASE_COLUMNS) + ([] if CASES[name][2] is None else ['percentile', 'percentile_value'])

def groups(case, reclassified=False):
    import numpy as np
    value_source, zone_source, _, zone_band = case
    data, mask, _, nodata, _, _ = source_arrays(value_source)
    zones, zone_mask, _, zone_nodata, _, _ = source_arrays(zone_source)
    values, classes = data[1], zones[zone_band-1]
    covered = (classes != zone_nodata) & np.isfinite(classes) & (zone_mask != 0) & np.isfinite(zones[2]) & (zones[2] > 0)
    valid = (values != nodata) & np.isfinite(values) & (mask != 0) & np.isfinite(data[2]) & (data[2] > 0)
    counts, samples = {}, {}
    for raw, value, accepted in zip(classes[covered], values[covered], valid[covered]):
        code = int(raw)
        if reclassified: code = 10 if code in (-3, 0) else 20 if code == 2 else 30
        counts[code] = counts.get(code, 0)+1
        if accepted: samples.setdefault(code, []).append(float(value))
    return [(code, counts[code], sorted(samples.get(code, []))) for code in sorted(counts)]

def oracle():
    populations, results = {}, {}
    for name, case in CASES.items():
        key = (case[0], case[1], case[3], name == 'reclassified.csv')
        if key not in populations: populations[key] = groups(case, key[-1])
        rows = []
        for code, total, values in populations[key]:
            count = len(values)
            stats = [math.fsum(values), values[0], values[-1], statistics.fmean(values), statistics.pstdev(values)] if count else [None]*5
            row = dict(zip(BASE_COLUMNS, [code, total, count, total-count, *stats]))
            if case[2] is not None:
                p = float(case[2]); value = None
                if count:
                    position = (count-1)*Fraction(p)/100
                    low = position.numerator//position.denominator; weight = position-low
                    value = float(Fraction(values[low])*(1-weight) + Fraction(values[low+bool(weight)])*weight)
                row.update(percentile=p, percentile_value=value)
            rows.append(row)
        results[name] = rows
    return results

def validate_records(rows, expected, name):
    if len(rows) != len(expected): raise FixtureError('complete CSV row count differs')
    for actual, wanted in zip(rows, expected):
        if set(actual) != set(columns(name)): raise FixtureError('complete CSV columns differ')
        for key, target in wanted.items():
            value = actual[key]
            if target is None:
                if value is not None: raise FixtureError('null group was replaced')
            elif key in BASE_COLUMNS[:4]:
                if type(value) is not int or value != target: raise FixtureError('exact Int64 class or count differs')
            elif type(value) not in (int, float) or not math.isfinite(value): raise FixtureError('nonfinite or nonnumeric statistic')
            elif key in ('percentile', 'min', 'max') or (key == 'percentile_value' and (target == 0 or CASES[name][2] in (0, 50, 100))):
                if value != target: raise FixtureError('exact percentile or endpoint differs')
            elif not math.isclose(value, target, rel_tol=4e-15 if key == 'percentile_value' else 1e-12, abs_tol=0. if key == 'percentile_value' else 1e-12): raise FixtureError('independent complete statistic differs')

def read_csv(path, name):
    with path.open(newline='', encoding='utf-8') as stream:
        reader = csv.DictReader(stream)
        if reader.fieldnames != columns(name): raise FixtureError('CSV schema differs')
        rows = []
        for record in reader:
            if set(record) != set(columns(name)) or any(value is None for value in record.values()): raise FixtureError('malformed CSV record')
            row = {}
            for key, text in record.items():
                if key in BASE_COLUMNS[:4]:
                    if re.fullmatch(r'-?(0|[1-9][0-9]*)', text) is None: raise FixtureError('CSV Int64 must be an exact integer')
                    row[key] = int(text)
                elif text == '': row[key] = None
                else:
                    try: number = Decimal(text)
                    except InvalidOperation: raise FixtureError('CSV number malformed') from None
                    if not number.is_finite(): raise FixtureError('CSV number nonfinite')
                    row[key] = float(number)
            rows.append(row)
    return rows

def verify(root, names, expectations):
    import numpy as np
    from osgeo import gdal, osr
    if len(set(names)) != len(names) or any(name not in CASES for name in names): raise FixtureError('unexpected target names')
    if set(p.name for p in root.iterdir()) != {*SOURCES, 'outputs', '.addp-online-owner', ORACLE}: raise FixtureError('unexpected source sidecar or private file')
    if set(p.name for p in (root/'outputs').iterdir()) != set(names): raise FixtureError('unexpected output or failed target')
    result = {'sources': {}, 'targets': {}, 'full_csv_checks': len(names), 'full_source_pixels_verified': True}
    for name in SOURCES:
        data, mask, dtype, nodata, crs, grid = source_arrays(name); ds = gdal.Open(str(root/name)); fact = digest(root/name)
        if (ds is None or ds.GetDriver().ShortName != 'GTiff' or ds.RasterCount != 3 or (ds.RasterYSize, ds.RasterXSize) != mask.shape
            or ds.GetFileList() != [str(root/name)] or ds.GetGeoTransform(can_return_null=True) != grid): raise FixtureError('source dimensions/grid/inventory changed')
        if crs:
            reference = osr.SpatialReference(); reference.ImportFromEPSG(crs)
            if not ds.GetProjection() or not osr.SpatialReference(wkt=ds.GetProjection()).IsSame(reference): raise FixtureError('source CRS changed')
        elif ds.GetProjection(): raise FixtureError('missing source CRS fabricated')
        for index, values in enumerate(data, 1):
            band = ds.GetRasterBand(index)
            if (band.DataType != getattr(gdal, 'GDT_'+dtype) or band.GetNoDataValue() != nodata or band.GetScale() != 17. or band.GetOffset() != 500.
                or band.GetColorInterpretation() != (gdal.GCI_AlphaBand if index == 3 else gdal.GCI_GrayIndex if index == 1 else gdal.GCI_Undefined)
                or not np.array_equal(band.ReadAsArray(), values, equal_nan=True) or not np.array_equal(band.GetMaskBand().ReadAsArray(), mask)):
                raise FixtureError('complete source pixels/mask/type/NoData/scale changed')
        band = ds = None; result['sources'][name] = fact
    records = {}
    for name in names:
        rows = read_csv(root/'outputs'/name, name); validate_records(rows, expectations[name], name); records[name] = rows
        result['targets'][name] = {**digest(root/'outputs'/name), 'row_count': len(rows), 'schema': columns(name), 'complete_table_verified': True}
    if 'original.csv' in records:
        for name in ('p0.csv', 'p100.csv', 'p25.csv', 'p75.csv', 'p33.csv'):
            if name in records and [{key: row[key] for key in BASE_COLUMNS} for row in records[name]] != records['original.csv']: raise FixtureError('requested percentile changed original nine columns')
    if 'p33-reversed.csv' in records and [r['percentile_value'] for r in records['p33-reversed.csv']] != [r['percentile_value'] for r in records['p33.csv']]: raise FixtureError('source order changed percentile')
    return result

def worker(action, root, names):
    if action == 'seed' and not names: return seed(root)
    if action == 'cleanup-oracle':
        path = root/ORACLE
        if path.is_symlink(): raise FixtureError('oracle symlink')
        path.unlink(missing_ok=True); return {'oracle_cache_removed': not path.exists()}
    if action == 'oracle' and not names:
        expectations = oracle(); raw = encoded(expectations)
        with (root/ORACLE).open('xb') as stream: stream.write(raw)
        (root/ORACLE).chmod(0o444); anchor = hashlib.sha256(raw).hexdigest()
    elif action == 'verify' and names and re.fullmatch('[0-9a-f]{64}', names[0]):
        anchor, names = names[0], names[1:]; path = root/ORACLE
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != anchor: raise FixtureError('oracle cache hash changed')
        expectations = json.loads(path.read_bytes())
        if set(expectations) != set(CASES): raise FixtureError('oracle scope changed')
    else: raise FixtureError('unknown oracle action or anchor')
    if list(Path(tempfile.gettempdir()).glob('addp-raster-*')): raise FixtureError('Runtime execution workspace residual')
    result = {**verify(root, names, expectations), 'runtime_workspaces_removed': True, 'oracle_sha256': anchor}
    if action == 'oracle': result['expectations'] = expectations
    return result

def runtime_worker(action, root, names=()): return run_runtime_worker(command, RUNTIME, SUITE, Path(__file__), action, root, names)

def archive_files(root, artifacts, names, previous, prefix):
    directory = artifacts/prefix; manifest = artifacts/(prefix+'-manifest.json')
    if manifest.exists() or directory.exists():
        if (manifest.is_symlink() or directory.is_symlink() or not manifest.is_file() or not directory.is_dir()
            or json.loads(manifest.read_bytes()) != previous or set(p.name for p in directory.iterdir()) != set(previous)):
            raise FixtureError('previous archive inventory changed')
        for name, receipt in previous.items():
            if digest(directory/name) != {key: receipt[key] for key in ('size_bytes', 'sha256')}: raise FixtureError('previous archived bytes changed')
    elif previous: raise FixtureError('previous archive was deleted')
    else: directory.mkdir()
    if not set(previous) <= set(names): raise FixtureError('archive scope shrank')
    receipts = dict(previous)
    for name in names:
        source = root/name; fact = digest(source)
        wanted = {'path': prefix+'/'+name, **fact}
        if name in previous:
            if previous[name] != wanted: raise FixtureError('source or previous output changed')
        else:
            with source.open('rb') as src, (directory/name).open('xb') as dst: shutil.copyfileobj(src, dst)
            (directory/name).chmod(0o444)
            if digest(directory/name) != fact: raise FixtureError('archived byte identity differs')
            receipts[name] = wanted
    temporary = manifest.with_suffix('.pending')
    with temporary.open('xb') as stream: stream.write(encoded(receipts))
    temporary.replace(manifest)
    return receipts

def main(action):
    secret = guard()
    if action in {'start', 'stop'}: return NFSExport(PREFIX, command).lifecycle(action, secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root = owned_root(secret); state = secret/(PREFIX+'-source.json')
    args = sys.argv[2:]
    if action == 'verify': args = [os.environ['ADDP_ONLINE_ZONAL_PERCENTILE_ORACLE_SHA256'], *args]
    result = runtime_worker(action, root, args)
    if action == 'seed':
        with state.open('x') as stream: stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']: raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret/(PREFIX+'-archives.json'); old = json.loads(history.read_bytes()) if history.exists() else {'sources': {}, 'targets': {}}
        if action != 'oracle' and not history.is_file(): raise FixtureError('source archive history missing')
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, old['sources'], 'zonal-percentile-sources')
        result['archived_csvs'] = archive_files(root/'outputs', artifacts, sys.argv[2:], old['targets'], 'zonal-percentile-csvs')
        history.write_bytes(encoded({'sources': result['archived_sources'], 'targets': result['archived_csvs']}))
    return result

if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'Zonal percentile fixture failed: {type(error).__name__}: {error}', file=sys.stderr); raise SystemExit(1)
