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
import time

SUITE = 'raster-zonal-weighted-stddev-persistence'
PREFIX = 'zonal-weighted-stddev'
RUNTIME = 'addp-raster-zonal-weighted-stddev-runtime'
WIDTH, HEIGHT = 513, 513
GRID = (100., 2., .25, 200., .125, -3.)
ORACLE = '.zonal-weighted-stddev-oracle.json'
BASE_COLUMNS = ('zone_value', 'total_count', 'valid_count', 'invalid_count', 'sum', 'min', 'max', 'mean', 'stddev')
WEIGHT_COLUMNS = ('weight_sum', 'weighted_sum', 'weighted_mean', 'weighted_stddev')
CASES = {
    'weighted.csv': ('values.tif', 'zones.tif', 'weights.tif'),
    'scaled.csv': ('values.tif', 'zones.tif', 'scaled-weights.tif'),
    'ones.csv': ('even.tif', 'small-zones.tif', 'ones.tif'),
    'control-default.csv': ('values.tif', 'zones.tif', 'weights.tif'),
    'control-false.csv': ('values.tif', 'zones.tif', 'weights.tif'),
    'control-original.csv': ('even.tif', 'small-zones.tif', None),
    'weights-reclassified.csv': ('values.tif', 'zones.tif', 'weight-classes.tif'),
    'zones-reclassified.csv': ('values.tif', 'zones.tif', 'weights.tif'),
    'zero.csv': ('even.tif', 'small-zones.tif', 'zero.tif'),
    'mixed-empty.csv': ('mixed-values.tif', 'mixed-zones.tif', 'mixed-weights.tif'),
    'invalid-values.csv': ('invalid.tif', 'small-zones.tif', 'ones.tif'),
    'empty-zones.csv': ('even.tif', 'empty-zones.tif', 'ones.tif'),
    'coverage.csv': ('coverage.tif', 'coverage-zones.tif', 'ignored-weights.tif'),
    'rotated.csv': ('rotated-values.tif', 'rotated-zones.tif', 'rotated-weights.tif'),
    'int64.csv': ('int64.tif', 'large-zones.tif', 'int64-weights.tif'),
    'uint64.csv': ('uint64.tif', 'large-zones.tif', 'uint64-weights.tif'),
}

def enabled(name): return not name.startswith('control-')
def all_one(name): return CASES[name][2] in ('ones.tif','rotated-weights.tif')

FAILURES = ('only_raster', 'only_band', 'raster_null', 'band_null', 'both_null', 'percentile_conflict',
    'negative_weight', 'nodata_weight', 'mask_weight', 'alpha_weight', 'nan_weight', 'infinite_weight',
    'values_band_zero', 'values_band_high', 'values_band_boolean', 'values_band_string', 'values_alpha', 'values_complex',
    'zones_band_zero', 'zones_band_high', 'zones_band_boolean', 'zones_band_string', 'zones_alpha', 'zones_complex',
    'weights_band_zero', 'weights_band_high', 'weights_band_boolean', 'weights_band_string', 'weights_alpha', 'weights_complex',
    'zones_shifted', 'weights_size', 'weights_crs', 'weights_shifted', 'weights_rotated', 'weights_missing', 'weights_singular',
    'fractional_zone', 'unsafe_zone', 'overflow_zone',
    'flag_null', 'flag_zero', 'flag_one', 'flag_fraction', 'flag_string_true', 'flag_string_false', 'flag_list', 'flag_object', 'enabled_without_weights')
SOURCES = tuple(dict.fromkeys([source for case in CASES.values() for source in case if source] +
    ['complex.tif', 'fractional-zone.tif', 'unsafe-zone.tif', 'overflow-zone.tif', 'shifted-zones.tif'] +
    [name+'.tif' for name in FAILURES[6:12]] + [name+'.tif' for name in FAILURES[31:37]]))

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
    if name.startswith('mixed-'):
        values = np.array([[2.,99.,-30.,8.,4.,12.,0.,-6.], [4.]*8,
                           [10.,20.,999.,0.,0.,0.,0.,0.], [-9999.]*8, [0.,-6.,8.,4.,12.,0.,-2.,2.]])
        weights = np.array([[2.,0.,0.,0.,0.,0.,0.,0.], [2.]*8,
                            [1.,3.,0.,0.,0.,0.,0.,0.], [-1.,np.nan,-9999.,np.inf,0.,0.,0.,0.], [0.]*8])
        zones = np.repeat(np.array([[-3.],[0.],[7.],[11.],[19.]]),8,axis=1)
        data = {'mixed-values.tif':values,'mixed-zones.tif':zones,'mixed-weights.tif':weights}[name]
        return np.stack([np.full(data.shape,-9999.),data,np.full(data.shape,128.)]), np.full(data.shape,255,np.uint8), 'Float64', -9999., 3857, GRID
    if name.startswith('rotated-'):
        data,mask,dtype,nodata,crs,_=source_arrays({'rotated-values.tif':'even.tif','rotated-zones.tif':'small-zones.tif','rotated-weights.tif':'ones.tif'}[name])
        return data,mask,dtype,nodata,crs,(10.,3.,1.,20.,-1.,-4.)
    dtype, nodata, crs, grid = 'Float64', -9999., 3857, GRID
    large = name in ('values.tif', 'zones.tif', 'weights.tif', 'scaled-weights.tif', 'weight-classes.tif')
    if large:
        i = np.arange(WIDTH*HEIGHT, dtype=np.int64).reshape(HEIGHT, WIDTH)
        values = ((i*37)%997-490).astype(float)/4
        mask = np.where(i%167 == 6, 0, 255).astype(np.uint8)
        alpha = np.where(i%179 == 7, 0., 128.)
        if name == 'zones.tif':
            values = (i%79).astype(float); values[i%211 == 3] = nodata
            mask = np.where(i%173 == 1, 0, 255).astype(np.uint8); alpha = np.where(i%181 == 2, 0., 128.)
        elif 'weights' in name or name == 'weight-classes.tif':
            values = (i%4).astype(float)/2
            if name == 'scaled-weights.tif': values *= 8
            if name == 'weight-classes.tif': values = (i%4).astype(float)
            mask = np.full(i.shape, 255, np.uint8); alpha = np.full(i.shape, 128.)
        else:
            values[i%79 == 7] = nodata; values[i%257 == 4] = np.nan; values[i%263 == 5] = np.inf
    else:
        values = np.tile(np.array([[0., -6., 8., nodata, 4., 12., np.nan, np.inf]]), (3,1))
        mask = np.full(values.shape,255,np.uint8); alpha = np.full(values.shape,128.)
        if name in ('ones.tif','zero.tif','ignored-weights.tif') or name.endswith('_weight.tif') or name.startswith('weights_'):
            values[:] = 0. if name == 'zero.tif' else 1.
            if name == 'negative_weight.tif': values[0,0] = -1
            if name == 'nodata_weight.tif': values[0,0] = nodata
            if name == 'mask_weight.tif': mask[0,0] = 0
            if name == 'alpha_weight.tif': alpha[0,0] = 0
            if name == 'nan_weight.tif': values[0,0] = np.nan
            if name == 'infinite_weight.tif': values[0,0] = np.inf
            if name == 'ignored-weights.tif':
                values[:] = 2.
                # Invalid weights only outside the intersection of value and zone coverage.
                values[0,2] = -1; values[0,3] = np.nan; values[0,6] = nodata
                values[1,4] = np.inf; mask[1,4] = 0; alpha[2,5] = 0
            if name == 'weights_crs.tif': crs = 4326
            if name == 'weights_missing.tif': crs = None; grid = None
            if name == 'weights_singular.tif': grid = (100.,1.,2.,200.,2.,4.)
            if name == 'weights_shifted.tif': grid = (101.,*GRID[1:])
            if name == 'weights_rotated.tif': grid = (*GRID[:2],.5,*GRID[3:])
            if name == 'weights_size.tif': values,mask,alpha = [v[:,:-1] for v in (values,mask,alpha)]
        elif name == 'even.tif':
            values[:] = [0.,-6.,8.,12.,4.,0.,-2.,2.]
        elif name == 'mixed.tif': values[2] = nodata
        elif name == 'single.tif': values[:] = nodata; values[0,0] = 0.; values[1,0] = -6.
        elif name == 'invalid.tif': values[:] = nodata
        elif name == 'coverage.tif': alpha[0,:4] = [128.,255.,0.,np.nan]; mask[1,4] = 0
        elif name in ('int64.tif','uint64.tif','int64-weights.tif','uint64-weights.tif'):
            dtype = 'UInt64' if name.startswith('uint') else 'Int64'; nodata = 2**64-1 if dtype == 'UInt64' else 2**63-1
            values = np.tile(np.array([[nodata,nodata-1,0,12,6,2,8,4]],dtype=dtype.lower()),(3,1))
            alpha = np.full(values.shape,128,dtype=values.dtype)
        elif name == 'complex.tif': dtype = 'CFloat64'; values = np.full(values.shape,1+2j); alpha = alpha.astype(complex)
        else:
            values = np.repeat(np.array([[0.],[-3.],[7.]]),8,axis=1)
            if name == 'empty-zones.tif': values[:] = nodata
            if name == 'large-zones.tif':
                dtype = 'Int64'; nodata = -9999; values = np.tile(np.array([[2**63-1]*4+[2**63-2]*2+[0]*2],dtype=np.int64),(3,1)); alpha=alpha.astype(np.int64)
            if name == 'fractional-zone.tif': values[:] = .5
            if name == 'unsafe-zone.tif': values[:] = float(2**53)
            if name == 'overflow-zone.tif':
                dtype='UInt64';nodata=2**64-1;values=np.full(values.shape,2**63,dtype=np.uint64);alpha=alpha.astype(np.uint64)
            if name == 'coverage-zones.tif': alpha[1,3] = 0.; mask[2,5] = 0
            if name == 'shifted-zones.tif': grid=(101.,*GRID[1:])
    first = np.full(values.shape,0 if dtype in ('Int64','UInt64') else nodata,dtype=values.dtype)
    return np.stack([first,values,alpha.astype(values.dtype)]), mask, dtype, nodata, crs, grid

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

def class_rules(): return [{'value': value, 'class': value//10} for value in range(79)]
def weight_rules(): return [{'value': value, 'class': value/2} for value in range(4)]
def columns(name): return list(BASE_COLUMNS) + (list(WEIGHT_COLUMNS if enabled(name) else WEIGHT_COLUMNS[:3]) if CASES[name][2] is not None else [])

def population(case, reclassified=False, include_stddev=True):
    import numpy as np
    from decimal import localcontext
    value_source, zone_source, weight_source = case
    data, mask, _, nodata, _, _ = source_arrays(value_source)
    zones, zone_mask, _, zone_nodata, _, _ = source_arrays(zone_source)
    values, classes = data[1], zones[1]
    covered = (classes != zone_nodata) & np.isfinite(classes) & (zone_mask != 0) & np.isfinite(zones[2]) & (zones[2] > 0)
    valid = (values != nodata) & np.isfinite(values) & (mask != 0) & np.isfinite(data[2]) & (data[2] > 0)
    weights = source_arrays(weight_source)[0][1] if weight_source else np.ones(values.shape)
    if weight_source == 'weight-classes.tif': weights = weights/2
    counts, samples, masses, totals, origins, first_moments, second_moments = {}, {}, {}, {}, {}, {}, {}
    with localcontext() as context:
        context.prec = 2000
        for raw, value, accepted, mass in zip(classes[covered], values[covered], valid[covered], weights[covered]):
            code = int(raw)//10 if reclassified else int(raw)
            counts[code] = counts.get(code,0)+1
            if accepted:
                v, w = float(value), float(mass)
                samples.setdefault(code,[]).append(v)
                dw = Decimal.from_float(w)
                if include_stddev and w:
                    # A positive-weight source value is an exact finite reference. Each
                    # observation contributes once; never subtract a repeating
                    # 2000-digit mean and square it for every pixel.
                    origin = origins.setdefault(code, Decimal.from_float(v))
                    delta = Decimal.from_float(v)-origin
                    first_moments[code] = first_moments.get(code,Decimal(0))+dw*delta
                    second_moments[code] = second_moments.get(code,Decimal(0))+dw*delta*delta
                masses[code] = masses.get(code,Decimal(0))+dw
                totals[code] = totals.get(code,Decimal(0))+Decimal.from_float(v)*dw
        rows = []
        for code in sorted(counts):
            group = samples.get(code,[]); count = len(group)
            stats = [math.fsum(group), min(group), max(group), statistics.fmean(group), statistics.pstdev(group)] if count else [None]*5
            row = dict(zip(BASE_COLUMNS,[code,counts[code],count,counts[code]-count,*stats]))
            if weight_source:
                mass,total=masses.get(code,Decimal(0)),totals.get(code,Decimal(0))
                weighted = [float(mass),float(total),float(total/mass) if mass else None] if count else [None]*3
                # The all-one contract preserves the existing ordinary reduction exactly.
                if weight_source == 'ones.tif' and count: weighted=[float(count),stats[0],stats[3]]
                if include_stddev:
                    if not mass: deviation=None
                    else:
                        center=total/mass
                        shift=center-origins[code]
                        # Exact centered-moment identity, at the same 2000-digit
                        # precision: sum(w*(delta-shift)^2). No clipping or epsilon.
                        variance=(second_moments[code]-2*shift*first_moments[code]+shift*shift*mass)/mass
                        deviation=float(variance.sqrt())
                    weighted.append(deviation)
                row.update(zip(WEIGHT_COLUMNS,weighted))
            rows.append(row)
    return rows

def stage(name, started=None):
    # Owned fixture phases only: no paths, credentials, identities or pixel data.
    suffix = '' if started is None else ' elapsed_ms='+str(round((time.monotonic()-started)*1000))
    print('zonal-weighted-stddev stage='+name+suffix, file=sys.stderr, flush=True)


def oracle():
    populations, results = {}, {}
    for name,case in CASES.items():
        key=(*case,name == 'zones-reclassified.csv',enabled(name))
        if key not in populations:
            started=time.monotonic();stage('population:'+name+':start')
            populations[key] = population(case,key[-2],key[-1])
            stage('population:'+name+':done',started)
        results[name]=populations[key]
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
            elif all_one(name) and key in ('weighted_sum','weighted_mean','weighted_stddev'):
                if value != actual[{'weighted_sum':'sum','weighted_mean':'mean','weighted_stddev':'stddev'}[key]]: raise FixtureError('all-one weights changed ordinary statistics')
            elif key in WEIGHT_COLUMNS or key in ('min','max'):
                if value != target: raise FixtureError('exact weighted statistic differs')
            elif not math.isclose(value,target,rel_tol=1e-12,abs_tol=1e-12): raise FixtureError('independent complete statistic differs')

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
    if root.is_symlink() or (root/'outputs').is_symlink() or not (root/'outputs').is_dir(): raise FixtureError('physical directory symlink or invalid output directory')
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
    for left,right in (('ones.csv','control-original.csv'),('weighted.csv','control-default.csv'),('weighted.csv','control-false.csv')):
        if left in records and right in records:
            keys = BASE_COLUMNS if right == 'control-original.csv' else (*BASE_COLUMNS,*WEIGHT_COLUMNS[:3])
            if [{key:row[key] for key in keys} for row in records[left]] != records[right]: raise FixtureError('weights changed original nine columns or disabled weighted statistics')
    if 'ones.csv' in records:
        for row in records['ones.csv']:
            if (row['weighted_sum'],row['weighted_mean'],row['weighted_stddev']) != (row['sum'],row['mean'],row['stddev']): raise FixtureError('all-one weights changed ordinary sum mean or stddev')
    if 'scaled.csv' in records:
        for a,b in zip(records['weighted.csv'],records['scaled.csv']):
            if any(b[key] != (a[key]*8 if a[key] is not None else None) for key in ('weight_sum','weighted_sum')) or a['weighted_mean'] != b['weighted_mean'] or a['weighted_stddev'] != b['weighted_stddev']:
                raise FixtureError('absolute weight scale or mean invariance changed')
    return result

def worker(action, root, names):
    if action == 'seed' and not names: return seed(root)
    if action == 'cleanup-oracle':
        path = root/ORACLE
        if path.is_symlink(): raise FixtureError('oracle symlink')
        path.unlink(missing_ok=True); return {'oracle_cache_removed': not path.exists()}
    if action == 'oracle' and not names:
        started=time.monotonic();stage('oracle:start')
        expectations = oracle();stage('oracle:computed',started)
        raw = encoded(expectations);stage('oracle:encoded')
        with (root/ORACLE).open('xb') as stream: stream.write(raw)
        (root/ORACLE).chmod(0o444); anchor = hashlib.sha256(raw).hexdigest();stage('oracle:frozen')
    elif action == 'verify' and names and re.fullmatch('[0-9a-f]{64}', names[0]):
        anchor, names = names[0], names[1:]; path = root/ORACLE
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != anchor: raise FixtureError('oracle cache hash changed')
        expectations = json.loads(path.read_bytes())
        if set(expectations) != set(CASES): raise FixtureError('oracle scope changed')
    else: raise FixtureError('unknown oracle action or anchor')
    if list(Path(tempfile.gettempdir()).glob('addp-raster-*')): raise FixtureError('Runtime execution workspace residual')
    started=time.monotonic();stage('verify:start')
    physical=verify(root,names,expectations);stage('verify:done',started)
    result = {**physical, 'runtime_workspaces_removed': True, 'oracle_sha256': anchor}
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
    if action == 'verify': args = [os.environ['ADDP_ONLINE_ZONAL_WEIGHTED_STDDEV_ORACLE_SHA256'], *args]
    result = runtime_worker(action, root, args)
    if action == 'seed':
        with state.open('x') as stream: stream.write(encoded(result))
    elif action != 'cleanup-oracle':
        if result['sources'] != json.loads(state.read_bytes())['sources']: raise FixtureError('source file changed')
        artifacts = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        history = secret/(PREFIX+'-archives.json'); old = json.loads(history.read_bytes()) if history.exists() else {'sources': {}, 'targets': {}}
        if action != 'oracle' and not history.is_file(): raise FixtureError('source archive history missing')
        result['archived_sources'] = archive_files(root, artifacts, SOURCES, old['sources'], 'zonal-weighted-stddev-sources')
        result['archived_csvs'] = archive_files(root/'outputs', artifacts, sys.argv[2:], old['targets'], 'zonal-weighted-stddev-csvs')
        history.write_bytes(encoded({'sources': result['archived_sources'], 'targets': result['archived_csvs']}))
    return result

if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, allow_nan=False, sort_keys=True))
    except Exception as error:
        print(f'Zonal weighted stddev fixture failed: {type(error).__name__}: {error}', file=sys.stderr); raise SystemExit(1)
