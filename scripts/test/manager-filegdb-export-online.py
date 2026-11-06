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
TABLE = 'filegdb_export_points'
EXPECTED_ROWS = {
    1: ('长沙', 1, 1.25, (12000.0, 6000.0)),
    2: ('', 0, 2.5, (-8000.0, 2300.0)),
    3: (None, 1, None, None),
}


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


def readback(archive_path, root_name):
    from osgeo import ogr, osr

    with tempfile.TemporaryDirectory() as temporary:
        root = unpack_archive(archive_path, Path(temporary), root_name)
        dataset = ogr.Open(str(root), 0)
        require(dataset is not None and dataset.GetDriver().GetName() == 'OpenFileGDB'
                and dataset.GetLayerCount() == 1, 'expected exactly one OpenFileGDB layer')
        layer = dataset.GetLayer(0)
        require(layer.GetName() == TABLE and layer.GetGeomType() == ogr.wkbPoint
                and layer.GetFeatureCount() == len(EXPECTED_ROWS), 'FileGDB layer identity or shape changed')
        spatial = layer.GetSpatialRef()
        expected_spatial = osr.SpatialReference()
        require(expected_spatial.ImportFromEPSG(3857) == 0 and spatial is not None, 'source CRS missing')
        spatial = spatial.Clone()
        spatial.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        expected_spatial.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        require(spatial.IsSame(expected_spatial), 'FileGDB source CRS changed')
        schema = layer.GetLayerDefn()
        require([schema.GetFieldDefn(index).GetName() for index in range(schema.GetFieldCount())]
                == ['seq', '标题', 'flag', 'measure'], 'FileGDB source attributes changed')
        remaining = dict(EXPECTED_ROWS)
        for feature in layer:
            row_id = feature.GetField('seq')
            require(row_id in remaining, 'unexpected or duplicate FileGDB row')
            title, flag, measure, xy = remaining.pop(row_id)
            require((feature.GetField('标题'), feature.GetField('flag'), feature.GetField('measure'))
                    == (title, flag, measure), 'FileGDB scalar or null value changed')
            geometry = feature.GetGeometryRef()
            if xy is None:
                require(geometry is None, 'FileGDB null geometry changed')
            else:
                require(geometry is not None and geometry.GetCoordinateDimension() == 2
                        and abs(geometry.GetX() - xy[0]) <= 1e-6 and abs(geometry.GetY() - xy[1]) <= 1e-6,
                        'FileGDB projected coordinates changed')
        require(not remaining, 'FileGDB source rows missing')
    return {'driver': 'OpenFileGDB', 'source_srid': 3857, 'features': len(EXPECTED_ROWS),
            'geometry': 'Point XY', 'coordinates_and_attributes_verified': True}


def source_snapshot(repository, environment):
    result = subprocess.run(['bash', 'business/scripts/online-metric-postgres-fixture.sh', 'snapshot-filegdb'],
                            cwd=repository, env=environment, text=True, capture_output=True, timeout=30, check=True)
    snapshot = json.loads(result.stdout)
    require(snapshot.get('count') == len(EXPECTED_ROWS) and all(
        isinstance(snapshot.get(key), str) and len(snapshot[key]) == 32 for key in ('rows', 'columns')),
        'FileGDB source snapshot is incomplete')
    return snapshot


def run(repository, environment, helpers):
    artifact_dir = Path(environment['ADDP_ONLINE_ARTIFACT_DIR'])
    report = {'schema_version': 'addp.manager-filegdb-export/v1', 'case': 'filegdb-export',
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
        report.update(identity=identity, outsider_identity=denied_identity, source_before=source_snapshot(repository, environment))
        report['scan_execution_id'] = helpers.wait_for_meta_scan(
            initializer, engine, time.monotonic() + 180, scope={'catalog_paths': ['public/' + TABLE]})
        registration = importlib.import_module('scripts.test.online-engine-registration')
        grant, = registration.initialize_exact_record_read_grants(
            initializer, client, tenant, [(engine, 'schema', 'public', TABLE)], scan_preparation=True)
        report['source_grant'] = {key: grant[key] for key in ('request_id', 'item_fingerprint', 'boundary_hash')}
        items = client.request('GET', f'/api/v1/meta/engines/{engine}/items?branch=public', (200,)).payload
        item, = [item for item in items if item.get('fingerprint') == grant['item_fingerprint']]
        locator = f'addp://engine/{engine}/path/public/{TABLE}?type=table&item_id={item["id"]}'
        source = {'source_case': 'mercator', 'source_srid': 3857, 'locator': locator,
                  'filename': 'FileGDB-成果-' + report['run_id'], 'download_file': 'manager-filegdb.gdb.zip',
                  'dialog_screenshot': 'manager-filegdb-export-dialog.png', 'screenshot': 'manager-filegdb-export.png'}
        context = {'run_id': report['run_id'], 'identity': identity, 'cases': [source]}
        browser_env = dict(environment, ADDP_ONLINE_MANAGER_FILEGDB_EXPORT_JSON=json.dumps(context, ensure_ascii=False))
        subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/manager-kml-export.spec.js',
                        '--config=playwright.online.config.js'], cwd=repository / 'console/frontend', env=browser_env, check=True)
        browser = json.loads((artifact_dir / 'manager-filegdb-export-browser.json').read_text())
        require(browser.get('schema_version') == 'addp.manager-filegdb-export-browser/v1'
                and browser.get('result') == 'passed' and browser.get('run_id') == report['run_id']
                and browser.get('identity') == identity and browser.get('warnings') == []
                and browser.get('failed_responses') == [], 'FileGDB real page evidence incomplete')
        outcome, = browser['cases']
        archive = artifact_dir / source['download_file']
        raw = archive.read_bytes()
        filename = source['filename'] + '.gdb.zip'
        require(outcome['locator'] == locator and outcome['actual_button_download'] and outcome['monitor_verified']
                and outcome['suggested_filename'] == filename and outcome['size_bytes'] == len(raw)
                and outcome['sha256'] == hashlib.sha256(raw).hexdigest()
                and all((artifact_dir / source[key]).is_file() for key in ('dialog_screenshot', 'screenshot')),
                'FileGDB page, session or download bytes are inconsistent')
        physical = subprocess.run(['/usr/bin/python3', '-m', 'scripts.test.manager-filegdb-export-online',
                                   '--readback', str(archive), filename.removesuffix('.zip')],
                                  cwd=repository, capture_output=True, text=True, timeout=30)
        require(physical.returncode == 0, 'independent FileGDB readback failed: ' + physical.stderr)
        report.update(download=outcome, physical=json.loads(physical.stdout))
        session = helpers.positive_int(outcome['session_id'], 'export session')
        session_path = f'/api/v1/manager/exports/{session}'
        outsider.request('GET', session_path, (404,))
        outsider.request('GET', session_path + '/file', (404,))
        report['same_tenant_non_owner_denied'] = True
        report['source_after'] = source_snapshot(repository, environment)
        require(report['source_after'] == report['source_before'], 'FileGDB export changed source data or structure')
        report.update(result='passed', source_unchanged=True)
        return report
    finally:
        (artifact_dir / 'manager-filegdb-export.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')


if __name__ == '__main__':
    require(len(sys.argv) == 4 and sys.argv[1] == '--readback', 'expected independent FileGDB readback arguments')
    print(json.dumps(readback(Path(sys.argv[2]), sys.argv[3])))
