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

SUITE = 'raster-zonal-class-frequency-persistence'
PREFIX = 'zonal-class-frequency'
RUNTIME = 'addp-raster-zonal-class-frequency-runtime'
WIDTH, HEIGHT = 519, 515
GRID = (100., 2., 0., 200., 0., -3.)
ORACLE = '.zonal-class-frequency-oracle.json'
BASE_COLUMNS = ('zone_value', 'total_count', 'valid_count', 'invalid_count', 'sum', 'min', 'max', 'mean', 'stddev')
FREQUENCY_COLUMNS = ('zone_value', 'class_value', 'count', 'proportion', 'total_count', 'valid_count', 'invalid_count')
CASES = {
    'original.csv': ('values.tif', 'zones.tif', 'summary-default', 2),
    'summary.csv': ('values.tif', 'zones.tif', 'summary', 2),
    'direct.csv': ('values.tif', 'zones.tif', 'frequency', 2),
    'single-class.csv': ('single.tif', 'zones.tif', 'frequency', 2),
    'second-band.csv': ('second.tif', 'zones.tif', 'frequency', 2),
    'cross-blocks.csv': ('cross-values.tif', 'cross-zones.tif', 'frequency', 2),
    'reclassified-values.csv': ('values.tif', 'zones.tif', 'reclass-values', 2),
    'reclassified-zones.csv': ('values.tif', 'zones.tif', 'reclass-zones', 2),
    'rotated.csv': ('rotated-values.tif', 'rotated-zones.tif', 'frequency', 2),
    'geographic.csv': ('geographic-values.tif', 'geographic-zones.tif', 'frequency', 2),
    'reversed.csv': ('reversed-values.tif', 'reversed-zones.tif', 'frequency', 2),
    'coverage.csv': ('coverage.tif', 'coverage-zones.tif', 'frequency', 2),
    'native.csv': ('native-values.tif', 'native-zones.tif', 'frequency', 2),
    'mixed-diagnostic.csv': ('mixed.tif', 'zones.tif', 'frequency', 2),
    'invalid-values.csv': ('invalid.tif', 'zones.tif', 'frequency', 2),
    'empty-zones.csv': ('values.tif', 'empty-zones.tif', 'frequency', 2),
}
FAILURES = ('method_unknown', 'method_empty', 'percentile_value', 'percentile_null', 'weights_raster', 'weights_band', 'weighted_stddev', 'stddev_string',
    'values_band_zero', 'values_band_high', 'values_band_boolean', 'values_band_string',
    'zones_band_zero', 'zones_band_high', 'zones_band_boolean', 'zones_band_string',
    'values_alpha', 'zones_alpha', 'values_complex', 'zones_complex',
    'fractional_class', 'unsafe_class', 'native_unsafe_class', 'fractional_zone', 'unsafe_zone', 'overflow_zone',
    'different_size', 'shifted_grid', 'different_crs', 'missing_crs', 'missing_grid', 'singular_grid')
SOURCES = tuple(dict.fromkeys([source for case in CASES.values() for source in case[:2]] +
    ['complex.tif', 'fractional-class.tif', 'unsafe-class.tif', 'native-unsafe-class.tif',
     'fractional-zone.tif', 'unsafe-zone.tif', 'overflow-zone.tif'] + [name+'.tif' for name in FAILURES[-6:]]))

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
    values = np.array([[-3, 0, 2, 2, 11, -9999, 0, 2], [0, 2, 11, -3, 0, 2, 11, -3], [2, 2, -3, 11, 0, 11, 2, 0]], dtype=float)
    zone = np.array([[0, 0, 0, 2, 2, 2, -3, -3], [0, 2, -3, 0, 2, -3, 0, 2], [-3, -3, 0, 0, 2, 2, -3, -3]], dtype=float)
    if 'cross-' in name:
        rows, cols = np.indices((HEIGHT, WIDTH)); values = np.array([-3., 0., 2., 11.])[(rows*7+cols)%4]; zone = ((rows*19+cols)%79-39).astype(float)
    if name in ('zones.tif', 'coverage-zones.tif', 'native-zones.tif', 'empty-zones.tif') or name.endswith('-zones.tif') or name in ('fractional-zone.tif', 'unsafe-zone.tif', 'overflow-zone.tif', *[n+'.tif' for n in FAILURES[-6:]]): values = zone.copy()
    first = np.full(values.shape, .5); mask = np.full(values.shape, 255, np.uint8); alpha = np.full(values.shape, 128.)
    if name == 'single.tif': values[:] = 0
    if name == 'second.tif': first[:] = 1.5
    if name == 'mixed.tif': values[zone == 2] = nodata
    if name == 'invalid.tif' or name == 'empty-zones.tif': values[:] = nodata
    if name.startswith('coverage'):
        values[0, 0] = nodata; mask[0, 1] = 0; alpha[0, 2] = 0; alpha[0, 3] = np.nan; alpha[1, 0] = 1; alpha[1, 1] = 255; values[2, 7] = np.inf
    if name.startswith('coverage'): alpha[1, 2] = nodata; alpha[1, 3] = -1
    if name == 'coverage.tif': values[0, 0] = .5
    if name.startswith('rotated-'): grid = (100., 2., .5, 200., .25, -3.)
    if name.startswith('geographic-'): crs, grid = 4326, (110., .01, 0., 30., 0., -.02)
    if name.startswith('reversed-'): grid = (100., -2., 0., 200., 0., 3.)
    if name == 'native-values.tif':
        dtype, nodata = 'UInt64', 2**64-1; values = np.array([[nodata, 2**53-1, 0, 2, 11, 0, 2, 11]]*3, dtype=np.uint64)
    if name == 'native-zones.tif':
        dtype, nodata = 'Int64', -2**63; values = np.array([[nodata, 2**63-1, -2**63+1, 0, 0, -3, 2**63-1, -2**63+1]]*3, dtype=np.int64)
    if name == 'fractional-class.tif' or name == 'fractional-zone.tif': values[0, 0] = .5
    if name == 'unsafe-class.tif' or name == 'unsafe-zone.tif': values[0, 0] = float(2**53)
    if name == 'native-unsafe-class.tif':
        dtype, nodata = 'UInt64', 2**64-1; values = np.full(values.shape, nodata-1, dtype=np.uint64); values[0, 1] = nodata
    if name == 'overflow-zone.tif':
        dtype, nodata = 'UInt64', 2**64-1; values = np.full(values.shape, 2**63, dtype=np.uint64); values[0, 1] = nodata
    if name == 'complex.tif': dtype = 'CFloat64'; values = np.full(values.shape, 1+2j, dtype=complex)
    if dtype in ('Int64', 'UInt64', 'CFloat64'): first = np.zeros(values.shape, dtype=values.dtype); alpha = np.full(values.shape, 128, dtype=values.dtype)
    if name == 'different_size.tif': values, first, mask, alpha = [a[:, :-1] for a in (values, first, mask, alpha)]
    if name == 'shifted_grid.tif': grid = (101., *GRID[1:])
    if name == 'different_crs.tif': crs = 4326
    if name == 'missing_crs.tif': crs = None
    if name == 'missing_grid.tif': grid = None
    if name == 'singular_grid.tif': grid = (100., 1., 2., 200., 2., 4.)
    return np.stack([first.astype(values.dtype), values, alpha.astype(values.dtype)]), mask, dtype, nodata, crs, grid

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

def class_rules(): return [{'value': value, 'class': 10 if value in (-3, 0) else 20} for value in (-3, 0, 2, 11)]
def columns(name): return list(BASE_COLUMNS if CASES[name][2].startswith('summary') else FREQUENCY_COLUMNS)
def schema(name): return [{'name': key, 'type': 'Int64' if key == 'class_value' else 'Float64' if key == 'proportion' or key in BASE_COLUMNS[4:] else 'int64'} for key in columns(name)]

def oracle():
    import numpy as np
    populations, results = {}, {}
    for name, case in CASES.items():
        v, z, mode, zone_band = case
        key = (v, z, mode if mode.startswith('reclass') else '', zone_band)
        if key not in populations:
            data, mask, _, nodata, _, _ = source_arrays(v); zones, zm, _, zn, _, _ = source_arrays(z)
            covered = (zones[zone_band-1] != zn) & np.isfinite(zones[zone_band-1]) & (zm != 0) & np.isfinite(zones[2]) & (zones[2] > 0)
            valid = (data[1] != nodata) & np.isfinite(data[1]) & (mask != 0) & np.isfinite(data[2]) & (data[2] > 0)
            totals, samples = {}, {}
            for raw, value, accepted in zip(zones[zone_band-1][covered], data[1][covered], valid[covered]):
                code = int(raw)
                if mode == 'reclass-zones': code = 10 if code in (-3, 0) else 20
                totals[code] = totals.get(code, 0)+1
                if accepted:
                    value = int(value)
                    if mode == 'reclass-values': value = 10 if value in (-3, 0) else 20
                    samples.setdefault(code, []).append(value)
            populations[key] = totals, samples
        totals, samples = populations[key]; rows = []
        for code in sorted(totals):
            values = samples.get(code, []); valid, total = len(values), totals[code]
            if mode.startswith('summary'):
                stats = [math.fsum(values), min(values), max(values), statistics.fmean(values), statistics.pstdev(values)] if valid else [None]*5
                rows.append(dict(zip(BASE_COLUMNS, [code, total, valid, total-valid, *stats])))
            else:
                counts = {}
                for value in values: counts[value] = counts.get(value, 0)+1
                if not counts: rows.append(dict(zip(FREQUENCY_COLUMNS, [code, None, 0, None, total, 0, total])))
                for value in sorted(counts): rows.append(dict(zip(FREQUENCY_COLUMNS, [code, value, counts[value], float(Fraction(counts[value], valid)), total, valid, total-valid])))
        results[name] = rows
    return results

def validate_records(rows, expected, name, *, preview=False):
    if len(rows) != len(expected): raise FixtureError('complete CSV row count differs')
    for actual, wanted in zip(rows, expected):
        if set(actual) != set(columns(name)): raise FixtureError('complete CSV columns differ')
        for key, target in wanted.items():
            value = actual[key]
            if target is None:
                if value is not None: raise FixtureError('null diagnostic was replaced')
            elif key in ('zone_value', 'class_value', 'count', 'total_count', 'valid_count', 'invalid_count'):
                if type(value) is not int or value != target: raise FixtureError('exact integer code or count differs')
            elif type(value) not in (int, float) or not math.isfinite(value): raise FixtureError('nonfinite or nonnumeric statistic')
            elif key == 'proportion':
                displayed = float(format(target, '.15f')) if preview else target
                if value != displayed: raise FixtureError('exact valid-class proportion differs')
            elif not math.isclose(value, target, rel_tol=1e-12, abs_tol=1e-12): raise FixtureError('original summary statistic differs')

def read_csv(path, name):
    with path.open(newline='', encoding='utf-8') as stream:
        reader = csv.DictReader(stream)
        if reader.fieldnames != columns(name): raise FixtureError('CSV schema differs')
        rows = []
        for record in reader:
            if set(record) != set(columns(name)) or any(value is None for value in record.values()): raise FixtureError('malformed CSV record')
            row = {}
            for key, text in record.items():
                if key in ('zone_value', 'class_value', 'count', 'total_count', 'valid_count', 'invalid_count'):
                    if text == '' and key == 'class_value': row[key] = None
                    else:
                        if re.fullmatch(r'-?(0|[1-9][0-9]*)', text) is None: raise FixtureError('CSV integer must be exact')
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
            if (band.DataType != getattr(gdal, 'GDT_'+dtype) or (band.GetNoDataValueAsInt64() if dtype == 'Int64' else band.GetNoDataValueAsUInt64() if dtype == 'UInt64' else band.GetNoDataValue()) != nodata or band.GetScale() != 17. or band.GetOffset() != 500.
                or band.GetColorInterpretation() != (gdal.GCI_AlphaBand if index == 3 else gdal.GCI_GrayIndex if index == 1 else gdal.GCI_Undefined)
                or not np.array_equal(band.ReadAsArray(), values, equal_nan=True) or not np.array_equal(band.GetMaskBand().ReadAsArray(), mask)):
                raise FixtureError('complete source pixels/mask/type/NoData/scale changed')
        band = ds = None; result['sources'][name] = fact
    records = {}
    for name in names:
        rows = read_csv(root/'outputs'/name, name); validate_records(rows, expectations[name], name); records[name] = rows
        result['targets'][name] = {**digest(root/'outputs'/name), 'row_count': len(rows), 'schema': columns(name), 'complete_table_verified': True}
    if 'original.csv' in records and 'summary.csv' in records and records['original.csv'] != records['summary.csv']: raise FixtureError('explicit summary changed original nine columns')

    return result

def oracle_scope(root, script_sha):
    marker = root/'.addp-online-owner'
    if root.is_symlink() or root.resolve() != root or marker.is_symlink() or not marker.is_file() or not re.fullmatch(PREFIX+r'-[0-9]+-[0-9]+', marker.read_text()): raise FixtureError('invalid oracle owned root/run')
    return {'root': str(root), 'run_id': marker.read_text(), 'fixture_sha256': script_sha, 'source_files': {n:digest(root/n) for n in SOURCES}}

def worker(action, root, names):
    if action == 'seed' and not names: return seed(root)
    if action == 'cleanup-oracle':
        path = root/ORACLE
        if path.is_symlink(): raise FixtureError('oracle symlink')
        path.unlink(missing_ok=True); return {'oracle_cache_removed': not path.exists()}
    if action == 'oracle' and len(names) == 1 and re.fullmatch('[0-9a-f]{64}', names[0]):
        expectations = oracle(); raw = encoded({'schema': 'addp.zonal-class-frequency-oracle/v1', 'scope': oracle_scope(root, names[0]), 'expectations': expectations})
        with (root/ORACLE).open('xb') as stream: stream.write(raw)
        (root/ORACLE).chmod(0o444); anchor = hashlib.sha256(raw).hexdigest(); names = []
    elif action == 'verify' and len(names) >= 2 and all(re.fullmatch('[0-9a-f]{64}', v) for v in names[:2]):
        anchor, script_sha, names = names[0], names[1], names[2:]; path = root/ORACLE
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != anchor: raise FixtureError('oracle cache hash changed')
        envelope = json.loads(path.read_bytes())
        if envelope.get('schema') != 'addp.zonal-class-frequency-oracle/v1' or envelope.get('scope') != oracle_scope(root, script_sha): raise FixtureError('oracle root/run/source/script scope changed')
        if path.stat().st_mode & 0o222: raise FixtureError('oracle cache became writable')
        expectations = envelope['expectations']
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
    args = sys.argv[2:]; script_sha = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    if action == 'oracle': args = [script_sha]
    if action == 'verify': args = [os.environ['ADDP_ONLINE_ZONAL_CLASS_FREQUENCY_ORACLE_SHA256'], script_sha, *args]
    result = runtime_worker(action, root, args)
    if action == 'seed':
        with state.open('xb') as stream: stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']: raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret/(PREFIX+'-archives.json'); old = json.loads(history.read_bytes()) if history.exists() else {'sources': {}, 'targets': {}, 'oracle': {}}
        if action != 'oracle' and not history.is_file(): raise FixtureError('source archive history missing')
        result['archived_oracle'] = archive_files(root, artifacts, [ORACLE], old['oracle'], PREFIX+'-oracle')
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, old['sources'], 'zonal-class-frequency-sources')
        result['archived_csvs'] = archive_files(root/'outputs', artifacts, sys.argv[2:], old['targets'], 'zonal-class-frequency-csvs')
        history.write_bytes(encoded({'sources': result['archived_sources'], 'targets': result['archived_csvs'], 'oracle': result['archived_oracle']}))
    return result

if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'Zonal class frequency fixture failed: {type(error).__name__}: {error}', file=sys.stderr); raise SystemExit(1)
