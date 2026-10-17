#!/usr/bin/env python3
"""Physical PMTiles oracle in an owned disposable deployment; no platform APIs."""
from __future__ import annotations
import gzip
import hashlib
import importlib
import io
import json
import math
import os
from pathlib import Path
import secrets
import sys
import tempfile

if len(sys.argv) > 1 and sys.argv[1].startswith('worker-'):
    import importlib.util
    spec = importlib.util.spec_from_file_location('shared_fixture', '/shared-fixture.py')
    shared = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(shared)
else:
    shared = importlib.import_module('business.scripts.online-raster-minio-fixture')
SUITE = 'vector-pmtiles-workflow'
CONTAINERS = ('addp-pmtiles-source', 'addp-pmtiles-target', 'addp-pmtiles-fixture-worker')
TARGET = 'result.pmtiles'


def features(polygons=False):
    result = []
    for index in range(50 if polygons else 2):
        x, y = (120 + (index % 10) * .16, 30 + (index // 10) * .16) if polygons else (120 + index, 30 + index)
        geometry = {'type': 'Polygon', 'coordinates': [[[x,y],[x+.08,y],[x+.08,y+.08],[x,y+.08],[x,y]]]} if polygons else {'type': 'Point', 'coordinates': [x,y]}
        result.append({'type': 'Feature', 'geometry': geometry, 'properties': {'name': f'区域{index:02}', 'rank': index, 'secret': 'excluded'}})
    return {'type': 'FeatureCollection', 'features': result}


def stop():
    errors = []
    for name in reversed(CONTAINERS):
        try:
            if shared.owned(name, SUITE): shared.command(['docker', 'rm', '-fv', name])
            if shared.owned(name, SUITE): raise shared.FixtureError('PMTiles cleanup has residuals')
        except Exception as error:
            errors.append(type(error).__name__)
    if errors: raise shared.FixtureError('PMTiles cleanup failed')


def start(root):
    for name in CONTAINERS:
        if shared.owned(name, SUITE): raise shared.FixtureError('refusing existing PMTiles fixture')
    config = {}
    for index, role in enumerate(('source', 'target')):
        config[role] = {'endpoint': f'127.0.0.1:{59022+index}', 'access_key': f'pmtiles-{role}',
                        'secret_key': secrets.token_hex(32), 'bucket': f'pmtiles-{role}'}
    shared.private_json(root / 'pmtiles-fixture.json', config)
    for index, role in enumerate(config):
        entry = config[role]
        shared.start_minio(root, entry, CONTAINERS[index], SUITE)
        shared.private_json(root / f'pmtiles-{role}-engine.json', {
            'name': f'Hosted PMTiles {role} {os.environ["ADDP_ONLINE_TEST_RUN_ID"]}',
            'description': 'Disposable PMTiles T4 fixture', 'engine_type': 'minio', 'engine_origin': 'general',
            'connection_info': {key: entry[key] for key in ('endpoint', 'access_key', 'secret_key')} | {'use_ssl': False}})


def objects(client, bucket):
    result = {}
    for item in client.list_objects(bucket, recursive=True):
        response = client.get_object(bucket, item.object_name)
        try: data = response.read()
        finally: response.close(); response.release_conn()
        result[item.object_name] = {'sha256': hashlib.sha256(data).hexdigest(), 'size_bytes': len(data),
            'etag': item.etag, 'last_modified': item.last_modified.isoformat()}
    return result


def inspect_archive(data, polygons):
    from osgeo import gdal
    from pmtiles.reader import Reader
    from pmtiles.tile import Compression, TileType
    reader = Reader(lambda offset, length: data[offset:offset+length])
    header, metadata = reader.header(), reader.metadata()
    layer = 'areas_after' if polygons else 'points_before'
    fields = {'name': 'String', 'rank': 'Number'} if polygons else {}
    if data[:8] != b'PMTiles\x03' or header['min_zoom'] != 0 or header['max_zoom'] != 5 or header['tile_type'] != TileType.MVT or header['tile_compression'] != Compression.GZIP:
        raise shared.FixtureError('PMTiles v3/header/compression/zoom differs')
    if metadata.get('vector_layers') != [{'id': layer, 'fields': fields}]:
        raise shared.FixtureError('PMTiles exact layer/attribute allowlist differs')
    # All sources lie strictly inside z5/x26/y13; decode that real MVT with GDAL.
    payload = reader.get(5, 26, 13)
    if not payload: raise shared.FixtureError('PMTiles expected tile is absent')
    with tempfile.TemporaryDirectory(prefix='pmtiles-oracle-') as temporary:
        path = Path(temporary) / '5/26/13.pbf'
        path.parent.mkdir(parents=True)
        path.write_bytes(gzip.decompress(payload))
        dataset = gdal.OpenEx(str(path), gdal.OF_VECTOR)
        if dataset is None or dataset.GetLayerCount() != 1: raise shared.FixtureError('MVT layer count differs')
        actual = dataset.GetLayerByIndex(0)
        if actual.GetName() != layer: raise shared.FixtureError('MVT layer differs')
        records = [json.loads(feature.ExportToJson()) for feature in actual]
    validate_records(records, polygons)
    return {'layer': layer, 'fields': fields, 'feature_count': len(records),
        'geometry_types': sorted({record['geometry']['type'] for record in records}),
        'min_zoom': header['min_zoom'], 'max_zoom': header['max_zoom'],
        'tile_sha256': hashlib.sha256(gzip.decompress(payload)).hexdigest(), 'tile': [5,26,13],
        'header_hash': hashlib.sha256(data[:127]).hexdigest()}


def validate_records(records, polygons):
    expected = features(polygons)['features']
    if len(records) != len(expected): raise shared.FixtureError('MVT feature count differs')
    if polygons:
        by_rank = {record['properties'].get('rank'): record for record in records}
        if set(by_rank) != set(range(50)): raise shared.FixtureError('MVT rank identity differs')
    else:
        records.sort(key=lambda record: record['geometry']['coordinates'][0])
    for index, source in enumerate(expected):
        record = by_rank[index] if polygons else records[index]
        props = {key: value for key, value in record['properties'].items() if key != 'mvt_id'}
        if props != ({'name': source['properties']['name'], 'rank': index} if polygons else {}):
            raise shared.FixtureError('MVT business attributes differ')
        if 'mvt_id' in record['properties'] and record['properties']['mvt_id'] is not None:
            raise shared.FixtureError('unexpected synthetic MVT identity')
        geometry = record['geometry']
        allowed = ('Polygon', 'MultiPolygon') if polygons else ('Point',)
        if geometry['type'] not in allowed: raise shared.FixtureError('MVT geometry type differs')
        def points(value):
            if isinstance(value[0], (int, float)): return [value]
            return [point for child in value for point in points(child)]
        if polygons:
            parts = geometry['coordinates'] if geometry['type'] == 'MultiPolygon' else [geometry['coordinates']]
            if len(parts) != 1 or len(parts[0]) != 1:
                raise shared.FixtureError('MVT polygon topology differs')
            ring = parts[0][0]
            if len(ring) != 5 or ring[0] != ring[-1]:
                raise shared.FixtureError('MVT polygon ring differs')
            area = abs(sum(a[0]*b[1]-b[0]*a[1] for a,b in zip(ring,ring[1:]))) / 2
            xs, ys = [point[0] for point in ring], [point[1] for point in ring]
            if not math.isclose(area, (max(xs)-min(xs))*(max(ys)-min(ys)), rel_tol=1e-6):
                raise shared.FixtureError('MVT polygon winding or area differs')
        actual_points, expected_points = points(geometry['coordinates']), points(source['geometry']['coordinates'])
        projected = [(x*math.pi*6378137/180, math.log(math.tan(math.pi/4+y*math.pi/360))*6378137) for x,y in expected_points]
        # z5/4096 quantization: one cell is about 306 m. Match every original
        # corner both ways, rejecting shifted, swapped or collapsed geometries.
        for first, second in ((actual_points, projected), (projected, actual_points)):
            if any(not any(abs(a[0]-b[0]) <= 310 and abs(a[1]-b[1]) <= 310 for b in second) for a in first):
                raise shared.FixtureError('MVT coordinates differ from independent source geometry')


def worker(action, path):
    from minio import Minio
    config = json.loads(Path(path).read_text())
    clients = {role: Minio(entry['endpoint'], access_key=entry['access_key'], secret_key=entry['secret_key'], secure=False)
               for role, entry in config.items()}
    if action == 'seed':
        from osgeo import gdal
        for role, client in clients.items(): client.make_bucket(config[role]['bucket'])
        source = clients['source']; bucket = config['source']['bucket']
        data = json.dumps(features(), ensure_ascii=False).encode()
        source.put_object(bucket, 'points.geojson', io.BytesIO(data), len(data), content_type='application/geo+json')
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root/'areas.geojson').write_text(json.dumps(features(True), ensure_ascii=False))
            dataset = gdal.VectorTranslate(str(root/'areas.shp'), str(root/'areas.geojson'), format='ESRI Shapefile', layerCreationOptions=['ENCODING=UTF-8'])
            if dataset is None: raise shared.FixtureError('Shapefile fixture failed')
            dataset = None
            for component in root.glob('areas.*'):
                if component.suffix != '.geojson': source.fput_object(bucket, component.name, str(component))
        keep = b'unrelated target; preserve through create conflict and replace\n'
        clients['target'].put_object(config['target']['bucket'], 'keep.txt', io.BytesIO(keep), len(keep))
        return {'sources': objects(source, bucket), 'targets': objects(clients['target'], config['target']['bucket'])}
    sources = objects(clients['source'], config['source']['bucket'])
    targets = objects(clients['target'], config['target']['bucket'])
    response = clients['target'].get_object(config['target']['bucket'], TARGET)
    try: data = response.read()
    finally: response.close(); response.release_conn()
    return {'sources': sources, 'targets': targets, 'archive': inspect_archive(data, action == 'verify-replace')}


def physical_worker(action, root):
    image = os.environ['ADDP_ONLINE_PMTILES_RUNTIME_IMAGE']
    return json.loads(shared.command(['docker', 'run', '--rm', '--name', CONTAINERS[2],
        '--label', f'com.addp.online-fixture={SUITE}', '--network', 'host', '--entrypoint', 'python',
        '-v', f'{Path(__file__).resolve()}:/fixture.py:ro',
        '-v', f'{Path(shared.__file__).resolve()}:/shared-fixture.py:ro',
        '-v', f'{root}:/secrets:ro', image, '/fixture.py', 'worker-'+action, '/secrets/pmtiles-fixture.json']))


def main():
    action = sys.argv[1]
    if action.startswith('worker-'):
        return worker(action.removeprefix('worker-'), sys.argv[2])
    root = shared.environment()
    if action == 'start': start(root); return {'result': 'started'}
    if action == 'stop': stop(); return {'result': 'removed', 'residual_resources': 0}
    if action not in ('seed', 'verify-create', 'verify-replace'): raise shared.FixtureError('unknown PMTiles action')
    result = physical_worker(action, root)
    if action == 'seed':
        (Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])/'pmtiles-fixture-baseline.json').write_text(json.dumps(result, ensure_ascii=False, sort_keys=True))
    return result


if __name__ == '__main__':
    try: print(json.dumps(main(), ensure_ascii=False, sort_keys=True))
    except Exception as error:
        print('PMTiles physical fixture failed: ' + type(error).__name__, file=sys.stderr)
        raise SystemExit(1)
