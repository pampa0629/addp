#!/usr/bin/env python3
"""Owned Hosted linework/self-crossing faces and independent coordinate oracles."""
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import sys

SUITE = 'vector-topology-persistence'
RUNTIME = 'addp-topology-runtime'
TARGETS = ('faces.geojson', 'diagnostics.geojson', 'repaired.geojson', 'snapped.gpkg')
SOURCES = ('source.geojson', 'invalid.geojson', 'snap-input.gpkg', 'snap-reference.gpkg')
PORTS = ('faces', 'diagnostics', 'repaired', 'snapped')
# Explicit coordinates are independent of the production matching algorithm.
SNAP_INPUT = (((0,0),(10,0)), ((0,10),(10,10)), ((0,20),(10,20)), ((5,30),(5,35)), ((50,40),(52,40)))
SNAP_REFERENCE = (((.5,0),(1,0)), ((10,0),(11,0)), ((.75,10),(10,10)), ((-.5,20),(.5,20)), ((0,30),(10,30)), ((51,40),(51,45)))
SNAP_EXPECTED = (((.5,0),(10,0)), ((0,10),(10,10)), ((-.5,20),(10,20)), ((5,30),(5,35)), ((50,40),(52,40)))


class FixtureError(RuntimeError):
    pass


if sys.argv[1:2] != ['--worker']:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
    from scripts.utils.online_nfs_fixture import NFSExport, FixtureError


def command(args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, text=True, timeout=120, **kwargs).stdout.strip()


def digest(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size <= 0:
        raise FixtureError('expected a nonempty ordinary file')
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}


def linework():
    coordinates = [
        [[120,30],[124,30],[124,34],[120,34],[120,30]],
        [[119,32],[125,32]], [[122,29],[122,35]], [[120,30],[124,30]]
    ]
    return {'type': 'FeatureCollection', 'crs': {'type': 'name', 'properties': {'name': 'EPSG:4326'}},
        'features': [{'type': 'Feature', 'properties': {'source_name': f'line-{i}'},
                      'geometry': {'type': 'LineString', 'coordinates': points}} for i, points in enumerate(coordinates)]}


def invalid_polygons():
    return {'type': 'FeatureCollection', 'crs': {'type': 'name', 'properties': {'name': 'EPSG:4326'}},
        'features': [{'type': 'Feature', 'properties': {'source_name': f'bow-{i}', 'grade': 7+i},
            'geometry': {'type': 'Polygon', 'coordinates': [[[x,30],[x+2,32],[x,32],[x+2,30],[x,30]]]}}
            for i, x in enumerate((120,124))]}


def seed_snap(path, coordinates):
    from osgeo import ogr, osr
    crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    dataset = ogr.GetDriverByName('GPKG').CreateDataSource(str(path))
    layer = dataset.CreateLayer(path.stem, crs, ogr.wkbLineString)
    layer.CreateField(ogr.FieldDefn('source_name', ogr.OFTString))
    layer.CreateField(ogr.FieldDefn('grade', ogr.OFTInteger64))
    for index, points in enumerate(coordinates):
        feature = ogr.Feature(layer.GetLayerDefn())
        feature.SetField('source_name', f'snap-{index}'); feature.SetField('grade', 10+index)
        geometry = ogr.Geometry(ogr.wkbLineString)
        for x, y in points: geometry.AddPoint_2D(x, y)
        feature.SetGeometry(geometry)
        if layer.CreateFeature(feature) != 0: raise FixtureError('cannot seed projected snap source')
    dataset = None


def validate_snap_features(features, expected=SNAP_EXPECTED):
    if len(features) != len(expected): raise FixtureError('snap changed source row count')
    for index, (feature, points) in enumerate(zip(features, expected)):
        geometry = feature['geometry']
        if (feature['properties'] != {'source_name': f'snap-{index}', 'grade': 10+index}
            or geometry.get('type') != 'LineString'
            or geometry.get('coordinates') != [list(point) for point in points]):
            raise FixtureError('snap changed original-position coordinates, order or attributes')


def verify_snap(path, expected):
    from osgeo import ogr, osr
    dataset = ogr.Open(str(path), 0)
    if dataset is None or dataset.GetLayerCount() != 1: raise FixtureError('expected one projected GeoPackage layer')
    layer = dataset.GetLayer(0); crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
    crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs): raise FixtureError('snap lost projected CRS')
    schema = layer.GetLayerDefn()
    if ([schema.GetFieldDefn(i).GetName() for i in range(schema.GetFieldCount())] != ['source_name','grade']
        or schema.GetFieldDefn(1).GetType() != ogr.OFTInteger64):
        raise FixtureError('snap changed the attribute schema')
    features = []
    for feature in layer:
        geometry = feature.GetGeometryRef()
        if geometry is None or geometry.GetCoordinateDimension() != 2 or not geometry.IsValid():
            raise FixtureError('snap persisted invalid or non-XY geometry')
        features.append({'properties': {key: feature.GetField(key) for key in ('source_name','grade')},
                         'geometry': json.loads(geometry.ExportToJson())})
    validate_snap_features(features, expected)
    result = {**digest(path), 'feature_count': len(features), 'crs': 'EPSG:3857',
              'layer_name': layer.GetName(), 'geometry_column': schema.GetGeomFieldDefn(0).GetName(),
              'fields': ['grade','source_name']}
    dataset = None
    return result


def validate_repaired(features):
    if len(features) != 2:
        raise FixtureError('repair must retain both source rows')
    for i, (feature, x) in enumerate(zip(features, (120,124))):
        props = feature['properties']; geometry = feature['geometry']
        expected_fields = {'source_name', 'grade', 'geometry_valid', 'geometry_reason', 'after_valid', 'after_reason'}
        if (set(props) != expected_fields or props['source_name'] != f'bow-{i}' or props['grade'] != 7+i
            or props['geometry_valid'] is not False or not props['geometry_reason'].startswith('Self-intersection')
            or props['after_valid'] is not True or props['after_reason'] != 'Valid Geometry'):
            raise FixtureError('repair lost source attributes or before/after validity')
        expected = {canonical([[x,30],[x+2,30],[x+1,31],[x,30]], True),
                    canonical([[x,32],[x+2,32],[x+1,31],[x,32]], True)}
        if geometry['type'] != 'MultiPolygon' or len(geometry['coordinates']) != 2 or any(len(part) != 1 for part in geometry['coordinates']):
            raise FixtureError('repair must retain two triangles per source feature')
        actual = [canonical(part[0], True) for part in geometry['coordinates']]
        if len(set(actual)) != 2 or set(actual) != expected:
            raise FixtureError('repair differs from independent triangle coordinates')


def canonical(points, ring=False):
    if any(len(point) != 2 or any(type(value) not in (int, float) or not math.isfinite(value) for value in point) for point in points):
        raise FixtureError('expected finite XY coordinates')
    vertices = [tuple(point) for point in points]
    if ring:
        if len(vertices) < 4 or vertices[0] != vertices[-1]:
            raise FixtureError('polygon must be closed')
        vertices = vertices[:-1]
        rotations = [tuple(sequence[i:] + sequence[:i]) for sequence in (vertices, list(reversed(vertices))) for i in range(len(vertices))]
        return min(rotations)
    return min(tuple(vertices), tuple(reversed(vertices)))


def expected_geometries(name):
    if name == 'faces.geojson':
        return {canonical([[x,y],[x+2,y],[x+2,y+2],[x,y+2],[x,y]], True)
                for x in (120,122) for y in (30,32)}
    if name == 'diagnostics.geojson':
        return {canonical(points) for points in (
            [[119,32],[120,32]], [[124,32],[125,32]], [[122,29],[122,30]], [[122,34],[122,35]])}
    raise FixtureError('unexpected target name')


def validate_features(name, features):
    if name == 'snapped.gpkg':
        return validate_snap_features(features)
    if name == 'repaired.geojson':
        return validate_repaired(features)
    actual = []
    for feature in features:
        geometry = feature['geometry']; coordinates = geometry['coordinates']
        if name == 'faces.geojson':
            if geometry['type'] != 'Polygon' or len(coordinates) != 1 or feature['properties'] != {}:
                raise FixtureError('faces must be attribute-free single-ring polygons')
            actual.append(canonical(coordinates[0], True))
        else:
            if geometry['type'] != 'LineString' or feature['properties'] != {'issue_type': 'dangle'}:
                raise FixtureError('diagnostics must contain only dangle classification')
            actual.append(canonical(coordinates))
    if len(actual) != 4 or len(set(actual)) != 4 or set(actual) != expected_geometries(name):
        raise FixtureError('output differs from the independent coordinate oracle')


def worker(action, root):
    if action == 'seed':
        if any((root / name).exists() for name in SOURCES) or list((root / 'outputs').iterdir()):
            raise FixtureError('refusing existing data')
        (root / 'source.geojson').write_text(json.dumps(linework(), sort_keys=True))
        (root / 'invalid.geojson').write_text(json.dumps(invalid_polygons(), sort_keys=True))
        seed_snap(root / 'snap-input.gpkg', SNAP_INPUT)
        seed_snap(root / 'snap-reference.gpkg', SNAP_REFERENCE)
        return {'sources': {name: digest(root / name) for name in SOURCES}}
    if action != 'verify':
        raise FixtureError('unknown worker action')
    from osgeo import ogr, osr
    if sorted(path.name for path in root.iterdir()) != sorted(['.addp-online-owner', 'outputs', *SOURCES]) or sorted(path.name for path in (root / 'outputs').iterdir()) != sorted(TARGETS):
        raise FixtureError('unexpected source, output, sidecar or temporary file')
    crs = osr.SpatialReference()
    if crs.ImportFromEPSG(4326) != 0:
        raise FixtureError('cannot load the fixture CRS')
    crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    results = {}
    verify_snap(root / 'snap-input.gpkg', SNAP_INPUT)
    verify_snap(root / 'snap-reference.gpkg', SNAP_REFERENCE)
    for name in TARGETS:
        path = root / 'outputs' / name
        if name == 'snapped.gpkg':
            results[name] = verify_snap(path, SNAP_EXPECTED)
            continue
        features = json.loads(path.read_text())['features']
        validate_features(name, features)
        dataset = ogr.Open(str(path), 0)
        if dataset is None or dataset.GetLayerCount() != 1:
            raise FixtureError('expected exactly one vector layer')
        layer = dataset.GetLayer(0)
        if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs):
            raise FixtureError('source CRS was not preserved')
        if layer.GetFeatureCount() != len(features) or any(not item.GetGeometryRef().IsValid() for item in layer):
            raise FixtureError('persisted geometries must all be valid')
        results[name] = {**digest(path), 'feature_count': len(features), 'crs': 'EPSG:4326',
                         'fields': sorted(features[0]['properties'])}
        dataset = None
    return {'sources': {name: digest(root / name) for name in SOURCES}, 'targets': results, 'exact_inventory_verified': True}


def runtime_worker(action, root):
    info = json.loads(command(['docker', 'inspect', RUNTIME]))[0]
    labels = info['Config']['Labels']
    if labels.get('com.addp.online-runtime') != SUITE or labels.get('com.addp.online-run-id') != os.environ['ADDP_ONLINE_TEST_RUN_ID'] or info['Config']['Cmd'] != ['sh', 'container_entrypoint.sh']:
        raise FixtureError('foreign Runtime or non-product entry')
    return json.loads(command(['docker', 'exec', '-i', RUNTIME, 'python', '-', '--worker', action, str(root)], input=Path(__file__).read_text()))


def archive_physical_evidence(root, directory, result):
    """Retain only this synthetic fixture's verified bytes for offline audit."""
    archive = directory / 'topology-physical'
    if archive.is_symlink() or (archive/'outputs').is_symlink():
        raise FixtureError('physical archive must not follow symlinks')
    (archive/'outputs').mkdir(parents=True, exist_ok=True)
    for relative, expected in [*((name, result['sources'][name]) for name in SOURCES),
                               *((f'outputs/{name}', result['targets'][name]) for name in TARGETS)]:
        source, target = root/relative, archive/relative
        actual = digest(source)
        if any(actual[key] != expected[key] for key in ('sha256','size_bytes')):
            raise FixtureError('physical source changed before evidence capture')
        if target.exists() or target.is_symlink():
            if digest(target) != actual: raise FixtureError('refusing to replace a different physical evidence snapshot')
        else:
            target.write_bytes(source.read_bytes())
        if digest(target) != actual: raise FixtureError('physical evidence capture differs')


def main(action):
    export = NFSExport('topology', command)
    secret = export.guard(os.environ)
    run_id = os.environ['ADDP_ONLINE_TEST_RUN_ID']
    if action in {'start','stop'}:
        return export.lifecycle(action, secret, run_id)
    root = export.owned_root(secret, run_id)
    result = runtime_worker(action, root)
    baseline = secret / 'topology-source.json'
    if action == 'seed':
        baseline.write_text(json.dumps(result))
    elif result['sources'] != json.loads(baseline.read_text())['sources']:
        raise FixtureError('source file changed')
    else:
        archive_physical_evidence(root, Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']), result)
    return result


if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3])) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, sort_keys=True))
    except Exception as error:
        print(f'Topology fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
