#!/usr/bin/env python3
"""Owned NFS export and independent GDAL oracle for polygonize persistence T4."""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SUITE = 'raster-polygonize-persistence'
RUNTIME = 'addp-raster-polygonize-runtime'
TARGETS = {'four.gpkg': 4, 'eight.gpkg': 8, 'reclassified.gpkg': 4}
TRANSFORM = (1000., 2., .25, 2000., .5, -3.)


class FixtureError(RuntimeError):
    pass


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError, run_runtime_worker


def guard(env=os.environ):
    return NFSExport('polygonize', command).guard(env)


def owned_root(secret):
    return NFSExport('polygonize', command).owned_root(secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])


def digest(path):
    if path.is_symlink() or not path.is_file():
        raise FixtureError('expected an ordinary owned file')
    size = path.stat().st_size
    if size <= 0:
        raise FixtureError('file is empty')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': size}


def source_arrays():
    import numpy as np
    values = np.array([[2,2,2,-1,-999,0,7,7], [2,0,2,-1,0,-1,7,7],
        [2,2,2,-1,-1,0,7,7], [-1,-1,-1,0,-1,-1,7,7],
        [0,-1,0,-1,0,-1,7,7], [-1,0,-1,0,-1,0,7,7]], dtype=np.int16)
    alpha = np.full(values.shape, 255, dtype=np.int16)
    alpha[0,6] = 0; alpha[0,5] = 128
    mask = np.full(values.shape, 255, dtype=np.uint8); mask[3,6] = 0
    return values, alpha, mask


def seed(root):
    from osgeo import gdal, osr
    path = root / 'classes.tif'
    if path.exists() or list((root / 'outputs').iterdir()):
        raise FixtureError('refusing to overwrite existing fixture data')
    values, alpha, mask = source_arrays()
    raster = gdal.GetDriverByName('GTiff').Create(str(path), 8, 6, 2, gdal.GDT_Int16)
    crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    raster.SetProjection(crs.ExportToWkt()); raster.SetGeoTransform(TRANSFORM)
    raster.GetRasterBand(1).WriteArray(values); raster.GetRasterBand(1).SetNoDataValue(-999)
    raster.GetRasterBand(2).WriteArray(alpha); raster.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    previous = gdal.GetConfigOption('GDAL_TIFF_INTERNAL_MASK')
    gdal.SetConfigOption('GDAL_TIFF_INTERNAL_MASK', 'YES')
    try:
        raster.CreateMaskBand(gdal.GMF_PER_DATASET)
        raster.GetRasterBand(1).GetMaskBand().WriteArray(mask)
    finally:
        gdal.SetConfigOption('GDAL_TIFF_INTERNAL_MASK', previous)
    raster = None
    return {'source': digest(path), 'crs': 'EPSG:3857', 'width': 8, 'height': 6}


def expected_regions(name):
    # Independent scalar flood-fill plus literal cell polygons; no product
    # polygonizer, reclassifier or GDAL Polygonize participates in this oracle.
    from shapely.geometry import Polygon
    from shapely.ops import unary_union
    values, alpha, mask = source_arrays()
    valid = (values != -999) & (alpha > 0) & (mask > 0)
    codes = values.copy()
    if name == 'reclassified.gpkg':
        for y in range(6):
            for x in range(8):
                if -2 <= int(values[y,x]) < 1: codes[y,x] = 10
                elif 1 <= int(values[y,x]) < 4: codes[y,x] = 20
    neighbors = [(0,1),(1,0),(0,-1),(-1,0)]
    if TARGETS[name] == 8: neighbors += [(1,1),(1,-1),(-1,1),(-1,-1)]
    seen, expected = set(), []
    def point(x,y):
        a,b,c,d,e,f = TRANSFORM
        return a+b*x+c*y, d+e*x+f*y
    for y in range(6):
        for x in range(8):
            if not valid[y,x] or (y,x) in seen: continue
            queue = [(y,x)]; seen.add((y,x)); cells = []
            for row,col in queue:
                cells.append(Polygon([point(col,row),point(col+1,row),point(col+1,row+1),point(col,row+1)]))
                for dy,dx in neighbors:
                    other = (row+dy,col+dx)
                    if (0 <= other[0] < 6 and 0 <= other[1] < 8 and other not in seen
                        and valid[other] and codes[other] == codes[y,x]):
                        seen.add(other); queue.append(other)
            expected.append((int(codes[y,x]), unary_union(cells)))
    return expected


def verify(root, names):
    from osgeo import ogr, osr
    from shapely import wkb
    if not names or len(set(names)) != len(names) or any(name not in TARGETS for name in names):
        raise FixtureError('invalid exact polygonize target inventory')
    if sorted(p.name for p in (root / 'outputs').iterdir()) != sorted(names):
        raise FixtureError('unexpected output, sidecar or temporary file')
    result = {'source': digest(root / 'classes.tif'), 'targets': {}}
    expected_crs = osr.SpatialReference(); expected_crs.ImportFromEPSG(3857)
    for name in names:
        path = root / 'outputs' / name
        file = digest(path); data = ogr.Open(str(path), 0)
        if data is None or data.GetDriver().GetName() != 'GPKG' or data.GetLayerCount() != 1:
            raise FixtureError('expected exactly one GeoPackage vector layer')
        layer = data.GetLayer(0)
        if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(expected_crs):
            raise FixtureError('classification source projection was not preserved')
        schema = layer.GetLayerDefn()
        if schema.GetFieldCount() != 1 or schema.GetFieldDefn(0).GetName() != 'class_value' or schema.GetFieldDefn(0).GetType() != ogr.OFTInteger64:
            raise FixtureError('expected only Int64 class_value')
        remaining = expected_regions(name); count, area, holes = 0, 0., 0
        for feature in layer:
            code = feature.GetField('class_value'); geometry = feature.GetGeometryRef()
            if geometry is None or geometry.GetGeometryType() != ogr.wkbMultiPolygon:
                raise FixtureError('expected valid two dimensional MultiPolygon')
            shape = wkb.loads(bytes(geometry.ExportToWkb()))
            if shape.is_empty or not shape.is_valid or shape.has_z:
                raise FixtureError('expected valid two dimensional MultiPolygon')
            matches = [i for i,(value,region) in enumerate(remaining) if value == code and region.equals(shape)]
            if len(matches) != 1:
                raise FixtureError('classification code, connectivity or exact cell coverage differs')
            remaining.pop(matches[0]); count += 1; area += shape.area
            holes += sum(len(part.interiors) for part in shape.geoms)
        if remaining: raise FixtureError('missing classified connected regions')
        result['targets'][name] = {**file, 'layer_name': layer.GetName(), 'feature_count': count,
            'area': area, 'hole_count': holes, 'connectivity': TARGETS[name], 'crs': 'EPSG:3857',
            'full_region_and_cell_verification': True}
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
        return NFSExport('polygonize', command).lifecycle(action, secret, os.environ['ADDP_ONLINE_TEST_RUN_ID'])
    root = owned_root(secret)
    if action == 'seed':
        result = runtime_worker('seed', root)
        (secret / 'polygonize-source.json').write_text(json.dumps(result))
        return result
    if action == 'verify':
        result = runtime_worker('verify', root, sys.argv[2:])
        if result['source'] != json.loads((secret / 'polygonize-source.json').read_text())['source']:
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
        print(f'Polygonize fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
