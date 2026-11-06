#!/usr/bin/env python3
"""Owned NFS DEM fixtures and an independent cell-intersection reference."""
from __future__ import annotations

import base64
import csv
from decimal import Decimal, localcontext
from fractions import Fraction
import hashlib
import io
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import tempfile

PREFIX = 'line-of-sight'
SUITE = 'raster-line-of-sight-persistence'
RUNTIME = 'addp-raster-line-of-sight-runtime'
COLUMNS = ('distance_m', 'status')
SCHEMA = [{'name': 'distance_m', 'type': 'float64'}, {'name': 'status', 'type': 'string'}]
NODATA = -9999.
VERTICAL = {'metre': 1., 'foot': .3048, 'us_survey_foot': 1200 / 3937}

CASES = {
    'flat.csv': 'visible',
    'ridge.csv': 'blocked',
    'touch.csv': 'blocked',
    'below.csv': 'visible',
    'endpoint.csv': 'visible',
    'on-ground.csv': 'blocked',
    'corner.csv': 'visible',
    'near-corner.csv': 'blocked',
    'horizontal-edge.csv': 'blocked',
    'vertical-edge.csv': 'indeterminate',
    'nodata.csv': 'indeterminate',
    'mask.csv': 'indeterminate',
    'alpha.csv': 'indeterminate',
    'gap-blocked.csv': 'blocked',
    'feet.csv': 'visible',
    'survey-cross-block.csv': 'blocked',
}
TARGETS = tuple(CASES)
SOURCES = (*[name.replace('.csv', '.tif') for name in TARGETS],
           'geographic.tif', 'rotated.tif', 'south-up.tif', 'scaled.tif')


class FixtureError(RuntimeError):
    pass


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import FixtureError, NFSExport, run_runtime_worker


def command(args, **kwargs):
    try:
        return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()
    except subprocess.CalledProcessError as error:
        detail = (error.stderr or 'no stderr diagnostic')[-1600:]
        raise FixtureError(f'fixture command failed ({error.returncode}): {detail}') from error


def guard(env=os.environ):
    return NFSExport(PREFIX, command).guard(env)


def owned_root(secret):
    return NFSExport(PREFIX, command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def source_spec(name):
    if name not in SOURCES:
        raise FixtureError('unexpected DEM source')
    key = name.removesuffix('.tif')
    epsg, unit, horizontal = {
        'feet': (2222, 'foot', .3048),
        'survey-cross-block': (2277, 'us_survey_foot', 1200 / 3937),
        'geographic': (4326, 'metre', 1.),
    }.get(key, (3857, 'metre', 1.))
    width = 520 if key == 'survey-cross-block' else 6
    dx, dy = (2., 3.) if unit != 'metre' else (1., 1.)
    grid = (0., dx, .25 if key == 'rotated' else 0., 4*dy, 0., dy if key == 'south-up' else -dy)
    elevations = {
        'ridge': {(1, 2): 3.},
        'touch': {(1, 2): 2.},
        'below': {(1, 2): math.nextafter(2., 0.)},
        'corner': {(0, 1): 9., (1, 0): NODATA},
        'near-corner': {(0, 1): 9.},
        'horizontal-edge': {(1, 2): 9.},
        'vertical-edge': {(1, 1): NODATA},
        'nodata': {(1, 2): NODATA},
        'mask': {(1, 2): 9.},
        'alpha': {(1, 2): 9.},
        'gap-blocked': {(1, 1): NODATA, (1, 3): 9.},
        'feet': {(1, 2): 6.},
        'survey-cross-block': {(1, 256): 6.},
    }.get(key, {})
    return {'width': width, 'height': 4, 'epsg': epsg, 'unit': unit, 'horizontal': horizontal,
            'grid': grid, 'elevations': elevations, 'masked': {(1, 2)} if key == 'mask' else set(),
            'transparent': {(1, 2)} if key == 'alpha' else set()}


def parameters(name):
    if name not in CASES:
        raise FixtureError('unknown target')
    spec = source_spec(name.replace('.csv', '.tif'))
    x0, dx, _, y0, _, dy = spec['grid']
    start, end = (.5, 1.5), (4.5, 1.5)
    heights = {
        'endpoint.csv': (0., 2.), 'on-ground.csv': (0., 0.),
        'survey-cross-block.csv': (1.5, 1.5),
    }.get(name, (2., 2.))
    if name in ('corner.csv', 'near-corner.csv'):
        start, end = (.5, .5), (3.5, 3.5)
    elif name == 'horizontal-edge.csv':
        start, end = (.5, 2), (4.5, 2)
    elif name == 'vertical-edge.csv':
        start, end = (2, .5), (2, 3.5)
    elif name == 'survey-cross-block.csv':
        end = (519.5, 2.5)
    observer = [x0 + start[0]*dx, y0 + start[1]*dy]
    target = [x0 + end[0]*dx, y0 + end[1]*dy]
    if name == 'near-corner.csv':
        target[1] = math.nextafter(target[1], math.inf)
    return {'band': 2, 'elevation_unit': spec['unit'], 'observer_xy': observer, 'target_xy': target,
            'observer_height_m': heights[0], 'target_height_m': heights[1]}


def reference(name, grid, horizontal):
    """Intersect the line with each cell independently by clipping both axes."""
    spec = source_spec(name.replace('.csv', '.tif'))
    params = parameters(name)
    start = tuple(map(Fraction, params['observer_xy']))
    end = tuple(map(Fraction, params['target_xy']))
    delta = tuple(b-a for a, b in zip(start, end))
    x0, dx, _, y0, _, dy = map(Fraction, grid)

    def elevation(row, col):
        raw = spec['elevations'].get((row, col), 0.)
        if raw == NODATA or (row, col) in spec['masked'] or (row, col) in spec['transparent']:
            return None
        return Fraction(raw) * Fraction(VERTICAL[spec['unit']])

    def anchor(point, height):
        col, row = (point[0]-x0)//dx, (point[1]-y0)//dy
        return elevation(row, col) + Fraction(height)

    observer = anchor(start, params['observer_height_m'])
    target = anchor(end, params['target_height_m'])
    blocked = missing = False
    for row in range(spec['height']):
        for col in range(spec['width']):
            enter, leave = Fraction(0), Fraction(1)
            edges = ((x0 + col*dx, x0 + (col+1)*dx), (y0 + (row+1)*dy, y0 + row*dy))
            for position, motion, (low, high) in zip(start, delta, edges):
                if not motion:
                    if not low <= position <= high:
                        break
                else:
                    first, second = sorted(((low-position)/motion, (high-position)/motion))
                    enter, leave = max(enter, first), min(leave, second)
            else:
                if enter >= leave:
                    continue
                ground = elevation(row, col)
                if ground is None:
                    missing = True
                    continue
                left = (1-enter)*observer + enter*target
                right = (1-leave)*observer + leave*target
                # Compare both ends and an interior point to exclude anchor-only contact.
                blocked |= (ground > min(left, right) or (enter > 0 and ground == left)
                            or (leave < 1 and ground == right) or ground == (left+right)/2)
    status = 'blocked' if blocked else 'indeterminate' if missing else 'visible'
    if status != CASES[name]:
        raise FixtureError(f'independent reference contradicts declared scenario {name}: {status}')
    with localcontext() as context:
        context.prec = 200
        squared = sum(value**2 for value in delta) * Fraction(horizontal)**2
        distance = float((Decimal(squared.numerator)/Decimal(squared.denominator)).sqrt())
    return {'distance_m': distance, 'status': status}


def read_file(path):
    if path.is_symlink() or not path.is_file():
        raise FixtureError('expected an ordinary owned file')
    payload = path.read_bytes()
    if not payload:
        raise FixtureError('expected a nonempty owned file')
    return payload


def fingerprint(payload):
    return {'sha256': hashlib.sha256(payload).hexdigest(), 'size_bytes': len(payload)}


def parse_csv(payload, expected):
    try:
        reader = csv.reader(io.StringIO(payload.decode('utf-8'), newline=''), strict=True)
        header, row = list(reader)
        if header != list(COLUMNS) or len(row) != 2:
            raise ValueError('incorrect CSV schema')
        distance = float(row[0])
    except (UnicodeError, ValueError, csv.Error) as error:
        raise FixtureError('expected one complete UTF-8 row in distance/status order') from error
    actual = {'distance_m': distance, 'status': row[1]}
    if not math.isfinite(distance) or distance <= 0 or actual != expected:
        raise FixtureError('exact Float64 distance or three-state result differs')
    return actual


def seed(root):
    import numpy as np
    import pandas as pd
    from osgeo import gdal, osr

    if any((root/name).exists() or (root/name).is_symlink() for name in SOURCES) or list((root/'outputs').iterdir()):
        raise FixtureError('refusing to overwrite DEM fixture data')
    expectations = {}
    for name in SOURCES:
        spec = source_spec(name)
        values = np.zeros((spec['height'], spec['width']), dtype=float)
        alpha = np.full_like(values, 255.)
        mask = np.full(values.shape, 255, np.uint8)
        for cell, value in spec['elevations'].items():
            values[cell] = value
        for cell in spec['masked']:
            mask[cell] = 0
        for cell in spec['transparent']:
            alpha[cell] = 0
        ds = gdal.GetDriverByName('GTiff').Create(str(root/name), spec['width'], spec['height'], 3,
                                                 gdal.GDT_Float64, ['TILED=YES', 'COMPRESS=DEFLATE'])
        crs = osr.SpatialReference()
        crs.ImportFromEPSG(spec['epsg'])
        ds.SetSpatialRef(crs)
        ds.SetGeoTransform(spec['grid'])
        for index, data in enumerate((np.zeros_like(values), values, alpha), 1):
            band = ds.GetRasterBand(index)
            band.WriteArray(data)
            band.SetNoDataValue(NODATA)
        ds.GetRasterBand(2).SetUnitType(spec['unit'])
        ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_AlphaBand)
        if name == 'scaled.tif':
            ds.GetRasterBand(2).SetScale(2.)
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
            if ds.CreateMaskBand(gdal.GMF_PER_DATASET) != gdal.CE_None:
                raise FixtureError('cannot create owned internal mask')
        ds.GetRasterBand(1).GetMaskBand().WriteArray(mask)
        band = ds = None
        ds = gdal.Open(str(root/name))
        source_crs = ds.GetSpatialRef()
        source_crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        band = ds.GetRasterBand(2)
        grid = ds.GetGeoTransform()
        checks = {
            'self-contained': ds.GetFileList() == [str(root/name)],
            'crs': source_crs.IsSame(crs),
            'grid': grid == spec['grid'],
            'nodata': band.GetNoDataValue() == NODATA,
            'unit': band.GetUnitType() == spec['unit'],
            'alpha-role': ds.GetRasterBand(3).GetColorInterpretation() == gdal.GCI_AlphaBand,
            'elevations': np.array_equal(band.ReadAsArray(), values),
            'mask': np.array_equal(band.GetMaskBand().ReadAsArray(), mask),
            'alpha-values': np.array_equal(ds.GetRasterBand(3).ReadAsArray(), alpha),
        }
        if invalid := [key for key, verified in checks.items() if not verified]:
            raise FixtureError(f'{name}: stored DEM facts differ: {invalid}')
        target = name.replace('.tif', '.csv')
        if target in CASES:
            horizontal = source_crs.GetLinearUnits()
            if not math.isclose(horizontal, spec['horizontal'], rel_tol=1e-15, abs_tol=0):
                raise FixtureError('stored CRS linear units differ')
            row = reference(target, grid, horizontal)
            preview = json.loads(pd.DataFrame([row]).to_json(orient='records', double_precision=15))[0]
            expectations[target] = {'row': row, 'preview_row': preview, 'measurement_grid': list(grid),
                                    'horizontal_metres': horizontal}
        band = ds = None
    return {'sources': verify(root, [])['sources'], 'expectations': expectations}


def verify(root, names):
    read_file(root/'.addp-online-owner')
    if len(set(names)) != len(names) or any(name not in CASES for name in names):
        raise FixtureError('unexpected requested CSV inventory')
    if {p.name for p in root.iterdir()} != {*SOURCES, 'outputs', '.addp-online-owner'}:
        raise FixtureError('unexpected DEM, sidecar or private file')
    output = root/'outputs'
    if output.is_symlink() or not output.is_dir() or {p.name for p in output.iterdir()} != set(names):
        raise FixtureError('unexpected output CSV inventory')
    targets = {}
    for name in names:
        payload = read_file(output/name)
        targets[name] = {**fingerprint(payload), 'csv_base64': base64.b64encode(payload).decode('ascii')}
    return {'sources': {name: fingerprint(read_file(root/name)) for name in SOURCES}, 'targets': targets}


def worker(action, root, names):
    if action == 'seed' and not names:
        return seed(root)
    if action != 'verify':
        raise FixtureError('unknown worker action')
    if list(Path(tempfile.gettempdir()).glob('addp-raster-*')):
        raise FixtureError('Runtime raster workspace residuals')
    return {**verify(root, names), 'runtime_workspaces_removed': True}


def main(action):
    secret = guard()
    run_id = os.environ['ADDP_ONLINE_TEST_RUN_ID']
    if action in ('start', 'stop'):
        return NFSExport(PREFIX, command).lifecycle(action, secret, run_id)
    root = owned_root(secret)
    snapshot = secret/(PREFIX+'-source.json')
    if action == 'seed':
        result = run_runtime_worker(command, RUNTIME, SUITE, Path(__file__), 'seed', root, ())
        with snapshot.open('x') as stream:
            json.dump(result, stream, allow_nan=False)
        return result
    if action not in ('verify', 'oracle'):
        raise FixtureError('unknown fixture action')
    frozen = json.loads(read_file(snapshot))
    result = run_runtime_worker(command, RUNTIME, SUITE, Path(__file__), 'verify', root, sys.argv[2:])
    if result['sources'] != frozen['sources']:
        raise FixtureError('source bytes changed')
    if action == 'oracle':
        if result['targets']:
            raise FixtureError('expected no CSV before business execution')
        result['expectations'] = frozen['expectations']
    return result


if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:]) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, sort_keys=True, allow_nan=False))
    except Exception as error:
        print(f'Line-of-sight fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
