"""Manager PostGIS FileGDB export and independent archive readback."""
from __future__ import annotations

import hashlib
import importlib
import json
from pathlib import Path, PurePosixPath
import stat
import subprocess
import sys
import tempfile
import time
import zipfile

kml = importlib.import_module('scripts.test.manager-kml-export-online')
require = kml.require
PERMISSIONS = kml.PERMISSIONS - {'meta.scan_task.execute', 'meta.scan_task.read'}
OUTSIDER_PERMISSIONS = {'manager.derived_artifact.read'}
FIXTURES = {
    'points': ('filegdb_export_points', 3857, ('POINT (12000 6000)', 'POINT (-8000 2300)')),
    'lines': ('filegdb_export_lines', 4326, (
        'MULTILINESTRING ((110 28,111 29,112 29.25),(114 30,115 30.5))',
        'MULTILINESTRING ((-121 45,-120.25 45.5))',
    )),
    'polygons': ('filegdb_export_polygons', 4326, (
        'MULTIPOLYGON (((110 28,114 28,114 32,110 32,110 28),(111 29,111 30,112 30,112 29,111 29)),'
        '((115 28,117 28,117 29,115 28)))',
        'MULTIPOLYGON (((-121 44,-120 44,-120 45,-121 45,-121 44)))',
    )),
}
ATTRIBUTES = {1: ('长沙', 1, 1.25), 2: ('', 0, 2.5), 3: (None, 1, None)}


def unpack_archive(archive_path, destination, root_name):
    with zipfile.ZipFile(archive_path) as archive:
        entries = archive.infolist()
        require(1 < len(entries) < 128 and sum(info.file_size for info in entries) < 16 * 1024 * 1024,
                'unexpected fixture FileGDB archive bounds')
        seen = set()
        for info in entries:
            path = PurePosixPath(info.filename)
            require(path.parts[:1] == (root_name,) and str(path) == info.filename.rstrip('/')
                    and not {'..', '.'}.intersection(path.parts) and '\\' not in info.filename
                    and path not in seen and not stat.S_ISLNK(info.external_attr >> 16)
                    and (len(path.parts) == 1 if info.is_dir() else len(path.parts) == 2),
                    'unexpected FileGDB archive member')
            seen.add(path)
        require(any(path.name.endswith('.gdbtable') for path in seen), 'FileGDB table files missing')
        archive.extractall(destination)
    return destination / root_name


def geometry_signature(geometry):
    require(geometry is not None and geometry.GetCoordinateDimension() == 2, 'FileGDB geometry must be XY')
    kind = geometry.GetGeometryName()
    if kind in {'POINT', 'LINESTRING', 'LINEARRING'}:
        xy = tuple((round(x, 8), round(y, 8)) for x, y, *_ in geometry.GetPoints())
        if kind == 'LINEARRING':
            require(len(xy) >= 4 and xy[0] == xy[-1], 'FileGDB polygon ring is open')
            vertices = xy[:-1]
            # GDAL can change the starting vertex and winding of a closed ring.
            xy = min(order[i:] + order[:i] for order in (vertices, vertices[::-1]) for i in range(len(order)))
        return kind, xy
    children = tuple(geometry_signature(geometry.GetGeometryRef(i)) for i in range(geometry.GetGeometryCount()))
    if kind == 'POLYGON':
        require(children and all(child[0] == 'LINEARRING' for child in children), 'invalid FileGDB polygon rings')
        return kind, children[0], tuple(sorted(children[1:]))
    require(kind in {'MULTILINESTRING', 'MULTIPOLYGON'} and children, 'unexpected FileGDB geometry')
    return kind, tuple(sorted(children))


def readback(archive_path, root_name, case):
    from osgeo import ogr, osr

    table, srid, geometries = FIXTURES[case]
    expected_geometries = {index: geometry_signature(ogr.CreateGeometryFromWkt(wkt))
                           for index, wkt in enumerate(geometries, 1)}
    with tempfile.TemporaryDirectory() as temporary:
        root = unpack_archive(archive_path, Path(temporary), root_name)
        dataset = ogr.Open(str(root), 0)
        require(dataset is not None and dataset.GetDriver().GetName() == 'OpenFileGDB'
                and dataset.GetLayerCount() == 1, 'expected exactly one OpenFileGDB layer')
        layer = dataset.GetLayer(0)
        expected_type = ogr.CreateGeometryFromWkt(geometries[0]).GetGeometryType()
        require(layer.GetName() == table and layer.GetGeomType() == expected_type
                and layer.GetFeatureCount() == len(ATTRIBUTES), 'FileGDB layer identity or shape changed')
        spatial = layer.GetSpatialRef()
        expected_spatial = osr.SpatialReference()
        require(expected_spatial.ImportFromEPSG(srid) == 0 and spatial is not None, 'source CRS missing')
        spatial = spatial.Clone()
        spatial.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        expected_spatial.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        require(spatial.IsSame(expected_spatial), 'FileGDB source CRS changed')
        schema = layer.GetLayerDefn()
        require([schema.GetFieldDefn(index).GetName() for index in range(schema.GetFieldCount())]
                == ['seq', '标题', 'flag', 'measure'], 'FileGDB source attributes changed')
        remaining = dict(ATTRIBUTES)
        for feature in layer:
            row_id = feature.GetField('seq')
            require(row_id in remaining, 'unexpected or duplicate FileGDB row')
            require((feature.GetField('标题'), feature.GetField('flag'), feature.GetField('measure'))
                    == remaining.pop(row_id), 'FileGDB scalar or null value changed')
            geometry = feature.GetGeometryRef()
            expected = expected_geometries.get(row_id)
            if expected is None:
                require(geometry is None, 'FileGDB null geometry changed')
            else:
                require(geometry_signature(geometry) == expected, 'FileGDB geometry parts, rings or coordinates changed')
        require(not remaining, 'FileGDB source rows missing')
    return {'driver': 'OpenFileGDB', 'source_srid': srid, 'features': len(ATTRIBUTES),
            'geometry': expected_geometries[1][0] + ' XY', 'coordinates_and_attributes_verified': True}


def source_snapshot(repository, environment, table):
    result = subprocess.run(['bash', 'business/scripts/online-metric-postgres-fixture.sh', 'snapshot-filegdb', table],
                            cwd=repository, env=environment, text=True, capture_output=True, timeout=30, check=True)
    snapshot = json.loads(result.stdout)
    require(snapshot.get('count') == len(ATTRIBUTES) and all(
        isinstance(snapshot.get(key), str) and len(snapshot[key]) == 32 for key in ('rows', 'columns')),
        'FileGDB source snapshot is incomplete')
    return snapshot


def scan_source(helpers, initializer, engine, table):
    locator = f'addp://engine/{engine}/path/public/{table}?type=table'
    execution = helpers.wait_for_meta_scan(
        initializer, engine, time.monotonic() + 180, scope={'targets': [locator]})
    scanned = initializer.request('GET', f'/api/v1/meta/executions/{execution}', (200,)).payload
    metadata = scanned.get('metadata', {})
    require(metadata.get('items_scanned') == 1 and metadata.get('catalog_nodes_scanned') == 0,
            'FileGDB requires one scanned source table')
    return locator, execution


def require_export_denied_without_source_grant(client, locator):
    denied = client.request('POST', '/api/v1/manager/exports', (403,), {
        'source_item_locator': locator, 'format': 'filegdb',
    })
    require(isinstance(denied.payload, dict) and not {
        'id', 'transfer_execution_id', 'download_url',
    }.intersection(denied.payload), 'source denial exposed an export session')
    return denied.status


def run(repository, environment, helpers):
    artifact_dir = Path(environment['ADDP_ONLINE_ARTIFACT_DIR'])
    report = {'schema_version': 'addp.manager-filegdb-export/v3', 'case': 'filegdb-export',
              'run_id': environment['ADDP_ONLINE_TEST_RUN_ID'], 'result': 'failed'}
    try:
        tenant = helpers.positive_int(environment['ADDP_ONLINE_TEST_TENANT_ID'], 'Tenant')
        engine = helpers.positive_int(environment['ADDP_ONLINE_MANAGER_PG_ENGINE_ID'], 'PostgreSQL engine')
        gateway = environment['GATEWAY_URL']
        client = helpers.GatewayClient(gateway, environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
        initializer = helpers.GatewayClient(gateway, environment['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], 30)
        outsider = helpers.GatewayClient(gateway, environment['ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN'], 30)
        identity = helpers.validate_user_identity(client, tenant, PERMISSIONS)
        denied_identity = helpers.validate_user_identity(outsider, tenant, OUTSIDER_PERMISSIONS)
        require(identity['principal_id'] != denied_identity['principal_id'], 'expected distinct same-tenant users')
        report.update(identity=identity, outsider_identity=denied_identity, cases=[])
        registration = importlib.import_module('scripts.test.online-engine-registration')
        sources = []
        for case, (table, srid, _) in FIXTURES.items():
            proof = {'source_case': case, 'source_before': source_snapshot(repository, environment, table)}
            report['cases'].append(proof)
            table_locator, proof['scan_execution_id'] = scan_source(helpers, initializer, engine, table)
            proof['without_source_grant_status'] = require_export_denied_without_source_grant(client, table_locator)
            grant, = registration.initialize_exact_record_read_grants(
                initializer, client, tenant, [(engine, 'schema', 'public', table)], scan_preparation=True)
            proof['source_grant'] = {key: grant[key] for key in ('request_id', 'item_fingerprint', 'boundary_hash')}
            items = client.request('GET', f'/api/v1/meta/engines/{engine}/items?branch=public', (200,)).payload
            item, = [item for item in items if item.get('fingerprint') == grant['item_fingerprint']]
            sources.append({'source_case': case, 'source_srid': srid, 'locator': f'{table_locator}&item_id={item["id"]}',
                            'filename': f'FileGDB-成果-{case}-' + report['run_id'],
                            'download_file': f'manager-filegdb-{case}.gdb.zip',
                            'dialog_screenshot': f'manager-filegdb-{case}-dialog.png',
                            'screenshot': f'manager-filegdb-{case}.png'})
        context = {'run_id': report['run_id'], 'identity': identity, 'cases': sources}
        browser_env = dict(environment, ADDP_ONLINE_MANAGER_FILEGDB_EXPORT_JSON=json.dumps(context, ensure_ascii=False))
        subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/manager-kml-export.spec.js',
                        '--config=playwright.online.config.js'], cwd=repository / 'console/frontend', env=browser_env, check=True)
        browser = json.loads((artifact_dir / 'manager-filegdb-export-browser.json').read_text())
        require(browser.get('schema_version') == 'addp.manager-filegdb-export-browser/v2'
                and browser.get('result') == 'passed' and browser.get('run_id') == report['run_id']
                and browser.get('identity') == identity and browser.get('warnings') == []
                and browser.get('failed_responses') == [], 'FileGDB real page evidence incomplete')
        for source, proof, outcome in zip(sources, report['cases'], browser['cases'], strict=True):
            case = source['source_case']
            archive = artifact_dir / source['download_file']
            raw = archive.read_bytes()
            filename = source['filename'] + '.gdb.zip'
            require(outcome['source_case'] == case and outcome['locator'] == source['locator']
                    and outcome['actual_button_download'] and outcome['monitor_verified']
                    and outcome['suggested_filename'] == filename and outcome['size_bytes'] == len(raw)
                    and outcome['sha256'] == hashlib.sha256(raw).hexdigest()
                    and all((artifact_dir / source[key]).is_file() for key in ('dialog_screenshot', 'screenshot')),
                    'FileGDB page, session or download bytes are inconsistent')
            physical = subprocess.run(['/usr/bin/python3', '-m', 'scripts.test.manager-filegdb-export-online',
                                       '--readback', str(archive), filename.removesuffix('.zip'), case],
                                      cwd=repository, capture_output=True, text=True, timeout=30)
            require(physical.returncode == 0, f'independent FileGDB {case} readback failed: ' + physical.stderr)
            proof.update(download=outcome, physical=json.loads(physical.stdout))
            session = helpers.positive_int(outcome['session_id'], 'export session')
            session_path = f'/api/v1/manager/exports/{session}'
            outsider.request('GET', session_path, (404,))
            outsider.request('GET', session_path + '/file', (404,))
            proof['same_tenant_non_owner_denied'] = True
            proof['source_after'] = source_snapshot(repository, environment, FIXTURES[case][0])
            require(proof['source_after'] == proof['source_before'], 'FileGDB export changed source data or structure')
        report.update(result='passed', source_unchanged=True)
        return report
    finally:
        (artifact_dir / 'manager-filegdb-export.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')


if __name__ == '__main__':
    require(len(sys.argv) == 5 and sys.argv[1] == '--readback', 'expected independent FileGDB readback arguments')
    print(json.dumps(readback(Path(sys.argv[2]), sys.argv[3], sys.argv[4])))
