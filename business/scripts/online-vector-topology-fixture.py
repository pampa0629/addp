#!/usr/bin/env python3
"""Owned Hosted linework and an independent coordinate oracle; no platform APIs."""
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import sys

SUITE = 'vector-topology-persistence'
RUNTIME = 'addp-topology-runtime'
TARGETS = ('faces.geojson', 'diagnostics.geojson')


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
        if (root / 'source.geojson').exists() or list((root / 'outputs').iterdir()):
            raise FixtureError('refusing existing data')
        (root / 'source.geojson').write_text(json.dumps(linework(), sort_keys=True))
        return {'source': digest(root / 'source.geojson')}
    if action != 'verify':
        raise FixtureError('unknown worker action')
    from osgeo import ogr, osr
    if sorted(path.name for path in root.iterdir()) != ['.addp-online-owner', 'outputs', 'source.geojson'] or sorted(path.name for path in (root / 'outputs').iterdir()) != sorted(TARGETS):
        raise FixtureError('unexpected source, output, sidecar or temporary file')
    crs = osr.SpatialReference()
    if crs.ImportFromEPSG(4326) != 0:
        raise FixtureError('cannot load the fixture CRS')
    crs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    results = {}
    for name in TARGETS:
        path = root / 'outputs' / name
        features = json.loads(path.read_text())['features']
        validate_features(name, features)
        dataset = ogr.Open(str(path), 0)
        if dataset is None or dataset.GetLayerCount() != 1:
            raise FixtureError('expected exactly one vector layer')
        layer = dataset.GetLayer(0)
        if not layer.GetSpatialRef() or not layer.GetSpatialRef().IsSame(crs):
            raise FixtureError('source CRS was not preserved')
        results[name] = {**digest(path), 'feature_count': 4, 'crs': 'EPSG:4326',
                         'fields': [] if name == 'faces.geojson' else ['issue_type']}
        dataset = None
    return {'source': digest(root / 'source.geojson'), 'targets': results, 'exact_inventory_verified': True}


def runtime_worker(action, root):
    info = json.loads(command(['docker', 'inspect', RUNTIME]))[0]
    labels = info['Config']['Labels']
    if labels.get('com.addp.online-runtime') != SUITE or labels.get('com.addp.online-run-id') != os.environ['ADDP_ONLINE_TEST_RUN_ID'] or info['Config']['Cmd'] != ['sh', 'container_entrypoint.sh']:
        raise FixtureError('foreign Runtime or non-product entry')
    return json.loads(command(['docker', 'exec', '-i', RUNTIME, 'python', '-', '--worker', action, str(root)], input=Path(__file__).read_text()))


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
    elif result['source'] != json.loads(baseline.read_text())['source']:
        raise FixtureError('source file changed')
    return result


if __name__ == '__main__':
    try:
        result = worker(sys.argv[2], Path(sys.argv[3])) if sys.argv[1:2] == ['--worker'] else main(sys.argv[1])
        print(json.dumps(result, sort_keys=True))
    except Exception as error:
        print(f'Topology fixture failed: {type(error).__name__}: {error}', file=sys.stderr)
        raise SystemExit(1)
