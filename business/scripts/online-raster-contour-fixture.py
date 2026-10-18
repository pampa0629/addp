#!/usr/bin/env python3
"""Owned NFS export and independent GDAL oracle for contour persistence T4."""
from __future__ import annotations
import hashlib
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

SUITE = 'raster-contour-persistence'
RUNTIME = 'addp-raster-contour-runtime'
TARGETS = {'levels.gpkg': [200., 350., 500.], 'interval.gpkg': [200., 300., 400., 500., 600., 700.]}


class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker


def guard(env=os.environ):
    return NFSExport('contour', command).guard(env)


def owned_root(secret):
    return NFSExport('contour', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file():
        raise FixtureError('expected an ordinary owned file')
    size = path.stat().st_size
    if size <= 0:
        raise FixtureError('file is empty')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': size}


def seed(root):
    from osgeo import gdal, osr
    import numpy as np
    path = root / 'dem.tif'
    if path.exists() or list((root / 'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    rows, cols = np.indices((48, 64))
    values = 100 + 4 * (cols + .5) + 9 * (rows + .5)
    raster = gdal.GetDriverByName('GTiff').Create(str(path), 64, 48, 1, gdal.GDT_Float64)
    crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    raster.SetProjection(crs.ExportToWkt()); raster.SetGeoTransform((1000, 2, 0, 2000, 0, -3))
    raster.GetRasterBand(1).WriteArray(values); raster = None
    return {'source': digest(path), 'crs': 'EPSG:3857', 'width': 64, 'height': 48}


def verify(root, names):
    from osgeo import gdal, ogr, osr
    if not names or len(set(names)) != len(names) or any(name not in TARGETS for name in names):
        raise FixtureError('invalid exact contour target inventory')
    if sorted(p.name for p in (root / 'outputs').iterdir()) != sorted(names):
        raise FixtureError('unexpected output, sidecar or temporary file')
    raster = gdal.Open(str(root / 'dem.tif'))
    if (raster.RasterXSize, raster.RasterYSize, raster.RasterCount) != (64, 48, 1):
        raise FixtureError('source grid changed')
    result = {'source': digest(root / 'dem.tif'), 'targets': {}}
    expected_crs = osr.SpatialReference(); expected_crs.ImportFromEPSG(3857)
    for name in names:
        path = root / 'outputs' / name
        file = digest(path)
        data = ogr.Open(str(path), 0)
        if data is None or data.GetDriver().GetName() != 'GPKG' or data.GetLayerCount() != 1:
            raise FixtureError('expected exactly one GeoPackage vector layer')
        layer = data.GetLayer(0)
        if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(expected_crs):
            raise FixtureError('contour source projection was not preserved')
        schema = layer.GetLayerDefn()
        if schema.GetFieldCount() != 1 or schema.GetFieldDefn(0).GetName() != 'elevation' or schema.GetFieldDefn(0).GetType() != ogr.OFTReal:
            raise FixtureError('expected only Float64 elevation in metres')
        levels, points, count = set(), 0, 0
        for feature in layer:
            elevation = feature.GetField('elevation')
            geometry = feature.GetGeometryRef()
            if elevation not in TARGETS[name] or geometry is None or geometry.GetGeometryType() != ogr.wkbLineString or geometry.GetPointCount() < 2:
                raise FixtureError('invalid contour geometry or level')
            for i in range(geometry.GetPointCount()):
                x, y, _ = geometry.GetPoint(i)
                # GDAL extends contours to half-cell outer edges; evaluate the
                # independent plane on the source sample-centre domain.
                z = 100 + 2 * (min(1127., max(1001., x)) - 1000) + 3 * (2000 - min(1998.5, max(1857.5, y)))
                if not (math.isfinite(x) and math.isfinite(y) and 1000 <= x <= 1128 and 1856 <= y <= 2000 and math.isclose(z, elevation, rel_tol=0, abs_tol=2e-5)):
                    raise FixtureError('contour vertex differs from the independent elevation plane')
                points += 1
            levels.add(elevation); count += 1
        if levels != set(TARGETS[name]) or count != len(TARGETS[name]):
            raise FixtureError('missing, duplicated or unexpected contour lines')
        result['targets'][name] = {**file, 'layer_name': layer.GetName(), 'feature_count': count,
                                   'vertex_count': points, 'levels_metres': sorted(levels), 'crs': 'EPSG:3857'}
        data = None
    return result


def worker(action, root, names):
    if action not in {'seed', 'verify'}:
        raise FixtureError('unknown worker action')
    if action == 'verify' and list(Path(tempfile.gettempdir()).glob('addp-raster-*')):
        raise FixtureError('Runtime raster execution workspace has residuals')
    if action == 'seed':
        return seed(root)
    return {**verify(root, names), 'runtime_workspaces_removed': True}


def runtime_worker(action, root, names=()):
    return run_runtime_worker(command, RUNTIME, SUITE, Path(__file__), action, root, names)


def main(action):
    secret = guard()
    if action in {'start', 'stop'}:
        return NFSExport('contour', command).lifecycle(action, secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root = owned_root(secret)
    if action == 'seed':
        result = runtime_worker('seed', root)
        (secret / 'contour-source.json').write_text(json.dumps(result))
        return result
    if action == 'verify':
        result = runtime_worker('verify', root, sys.argv[2:])
        if result['source'] != json.loads((secret / 'contour-source.json').read_text())['source']:
            raise FixtureError('source file changed')
        return result
    raise FixtureError('unknown fixture action')


if __name__ == '__main__':
    try:
        if sys.argv[1:2] == ['--worker']:
            result = worker(sys.argv[2], Path(sys.argv[3]), sys.argv[4:])
        else:
            result = main(sys.argv[1])
        print(json.dumps(result, sort_keys=True))
    except Exception as error:
        print(f'Contour fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
