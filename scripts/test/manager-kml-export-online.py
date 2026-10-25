"""Manager's isolated, ordinary-user KML export acceptance case."""
from __future__ import annotations

import hashlib
import importlib
import json
import math
import os
import subprocess
import time
import xml.etree.ElementTree as ET
from pathlib import Path

PERMISSIONS = {
    'manager.data_item.read', 'manager.derived_artifact.create', 'manager.derived_artifact.read',
    'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read', 'monitor.execution.read',
}
NAMESPACE = 'http://www.opengis.net/kml/2.2'
NS = {'k': NAMESPACE}
SHAPES = [
    ('Point', [(120, 30)]), ('LineString', [(120, 30), (121, 31)]),
    ('Polygon', [[(0, 0), (4, 0), (4, 4), (0, 4), (0, 0)], [(1, 1), (1, 2), (2, 2), (2, 1), (1, 1)]]),
    ('MultiGeometry', [('Point', [(1, 2)]), ('Point', [(3, 4)])]),
    ('MultiGeometry', [('LineString', [(1, 2), (3, 4)]), ('LineString', [(5, 6), (7, 8)])]),
    ('MultiGeometry', [('Polygon', [[(0, 0), (1, 0), (1, 1), (0, 0)]])]),
    ('MultiGeometry', [('Point', [(1, 2)]), ('LineString', [(3, 4), (5, 6)])]), None,
]


def require(condition, message):
    if not condition:
        raise ValueError(message)


def coordinates(node):
    require(node is not None and node.text is not None, 'KML coordinates missing')
    values = []
    for token in node.text.split():
        pair = token.split(',')
        require(len(pair) == 2, 'KML must retain XY coordinates')
        point = tuple(float(v) for v in pair)
        require(all(math.isfinite(v) for v in point), 'KML coordinates must be finite')
        values.append(point)
    return values


def geometry(node):
    kind = node.tag.removeprefix('{' + NAMESPACE + '}')
    if kind in ('Point', 'LineString'):
        return kind, coordinates(node.find('k:coordinates', NS))
    if kind == 'Polygon':
        rings = [coordinates(node.find('k:outerBoundaryIs/k:LinearRing/k:coordinates', NS))]
        rings += [coordinates(r) for r in node.findall('k:innerBoundaryIs/k:LinearRing/k:coordinates', NS)]
        return kind, rings
    require(kind == 'MultiGeometry', 'unexpected KML geometry')
    return kind, [geometry(child) for child in node]


def compare_geometry(actual, expected):
    if isinstance(expected, (int, float)):
        require(isinstance(actual, (int, float)) and abs(actual - expected) <= 1e-8, 'KML WGS84 coordinate mismatch')
    elif isinstance(expected, str):
        require(actual == expected, 'KML geometry type mismatch')
    else:
        require(isinstance(actual, (tuple, list)) and len(actual) == len(expected), 'KML geometry structure mismatch')
        for value, wanted in zip(actual, expected):
            compare_geometry(value, wanted)


def validate_kml(raw, table_name):
    require(0 < len(raw) <= 1024 * 1024, 'unexpected KML export size')
    require(b'<!DOCTYPE' not in raw.upper() and b'<!ENTITY' not in raw.upper(), 'unexpected KML external XML declaration')
    root = ET.fromstring(raw)
    require(root.tag == '{' + NAMESPACE + '}kml', 'KML 2.2 namespace missing')
    folders = root.findall('k:Document/k:Folder', NS)
    require(len(folders) == 1 and folders[0].findtext('k:name', namespaces=NS) == table_name, 'KML source layer name changed')
    schema = root.findall('k:Document/k:Schema', NS)
    require(len(schema) == 1 and schema[0].get('id') == 'addp_schema', 'KML schema missing')
    fields = [(field.get('name'), field.get('type')) for field in schema[0]]
    require(fields == [('seq', 'int'), ('标题', 'string'), ('flag', 'bool'), ('measure', 'double')], 'KML scalar field names or types changed')
    marks = folders[0].findall('k:Placemark', NS)
    require(len(marks) == 8, 'KML Placemark count changed')
    seen = set()
    for mark in marks:
        data = mark.find('k:ExtendedData/k:SchemaData', NS)
        require(data is not None and data.get('schemaUrl') == '#addp_schema', 'KML schema reference missing')
        names = [value.get('name') for value in data]
        require(len(names) == len(set(names)), 'KML duplicated scalar data')
        values = {value.get('name'): value.text or '' for value in data}
        index = int(values['seq'])
        require(index in range(8) and index not in seen, 'KML source row identity changed')
        seen.add(index)
        require(values['flag'] == ('1' if index % 2 == 0 else '0') and values['measure'] == '1.25', 'KML bool or double value changed')
        require(set(values) == ({'seq', 'flag', 'measure'} if index == 2 else {'seq', '标题', 'flag', 'measure'}), 'KML null or field presence changed')
        if index != 2:
            require(values['标题'] == ('' if index == 1 else '中文 <&> "quote"'), 'KML Unicode, XML escaping or empty string changed')
        shapes = [child for child in mark if child.tag != '{' + NAMESPACE + '}ExtendedData']
        if index == 7:
            require(shapes == [], 'KML null geometry changed')
        else:
            require(len(shapes) == 1, 'KML geometry missing or duplicated')
            compare_geometry(geometry(shapes[0]), SHAPES[index])
    return {'placemarks': 8, 'nonempty_geometries': 7, 'null_geometry': 1,
            'scalar_fields_verified': True, 'wgs84_coordinates_verified': True,
            'null_and_empty_preserved': True, 'size_bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}


def source_snapshot(repository, environment):
    result = subprocess.run(['bash', 'business/scripts/online-metric-postgres-fixture.sh', 'snapshot-kml'],
                            cwd=repository, env=environment, text=True, capture_output=True, timeout=30, check=True)
    snapshot = json.loads(result.stdout)
    require(snapshot.get('counts') == [8, 8] and all(isinstance(snapshot.get(key), str) and len(snapshot[key]) == 32
                for key in ('wgs84', 'mercator', 'columns')), 'KML fixture snapshot is incomplete')
    return snapshot


def validate_browser_report(report, context, artifact_dir):
    require(report.get('schema_version') == 'addp.manager-kml-export-browser/v1' and report.get('result') == 'passed', 'KML browser did not pass')
    require(report.get('run_id') == context['run_id'] and report.get('identity') == context['identity'], 'KML browser identity mismatch')
    require(report.get('warnings') == [] and report.get('failed_responses') == [], 'KML browser had unexpected diagnostics')
    cases = report.get('cases')
    require(isinstance(cases, list) and len(cases) == 2, 'KML browser case count mismatch')
    results = []
    for source, case in zip(context['cases'], cases):
        require(case.get('source_case') == source['source_case'] and case.get('locator') == source['locator'], 'KML browser source mismatch')
        require(case.get('actual_button_download') is True and case.get('monitor_verified') is True, 'KML button or execution provenance not verified')
        require(case.get('suggested_filename') == source['filename'] + '.kml', 'KML downloaded filename changed')
        raw = (artifact_dir / source['download_file']).read_bytes()
        physical = validate_kml(raw, source['table'])
        require(case.get('sha256') == physical['sha256'] and case.get('size_bytes') == len(raw), 'KML saved browser bytes differ from proof')
        require((artifact_dir / source['screenshot']).is_file(), 'KML page screenshot missing')
        require((artifact_dir / source['dialog_screenshot']).is_file(), 'KML selected-format screenshot missing')
        results.append({**case, 'physical': physical})
    return results


def run(repository, environment, helpers):
    required = ('GATEWAY_URL', 'ADDP_ONLINE_TEST_TENANT_ID', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
                'ADDP_ONLINE_TEST_USER_USERNAME', 'ADDP_ONLINE_TEST_USER_PASSWORD',
                'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN', 'ADDP_ONLINE_MANAGER_PG_ENGINE_ID',
                'ADDP_ONLINE_ARTIFACT_DIR', 'ADDP_ONLINE_TEST_RUN_ID')
    require(all(environment.get(key) for key in required), 'KML acceptance environment is incomplete')
    require(environment.get('ADDP_ONLINE_MANAGER_CASE') == 'kml-export', 'KML requires its explicit acceptance case')
    artifact_dir = Path(environment['ADDP_ONLINE_ARTIFACT_DIR'])
    report = {'schema_version': 'addp.manager-kml-export/v1', 'suite': 'manager-internal-artifact-lineage',
              'case': 'kml-export', 'run_id': environment['ADDP_ONLINE_TEST_RUN_ID'], 'result': 'incomplete'}
    try:
        tenant = helpers.positive_int(environment['ADDP_ONLINE_TEST_TENANT_ID'], 'Tenant')
        engine = helpers.positive_int(environment['ADDP_ONLINE_MANAGER_PG_ENGINE_ID'], 'PostgreSQL engine')
        client = helpers.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
        identity = helpers.validate_user_identity(client, tenant, PERMISSIONS)
        report['identity'] = identity
        before = source_snapshot(repository, environment)
        report['source_before'] = before
        report['meta_scan_execution_id'] = helpers.wait_for_meta_scan(client, engine, time.monotonic() + 180)
        cases = []
        before_meta = {}
        for source_case, srid in [('wgs84', 4326), ('mercator', 3857)]:
            table = 'kml_export_' + source_case
            items = client.request('GET', f'/api/v1/meta/engines/{engine}/items?branch=public', (200,)).payload
            matches = [item for item in items if item.get('full_name') == 'public.' + table and item.get('item_type') == 'table']
            require(len(matches) == 1, 'KML source table is not uniquely discovered')
            item = matches[0]
            item_id = int(item['id'])
            before_meta[item_id] = client.request('GET', f'/api/v1/meta/items/{item_id}', (200,)).payload
            cases.append({'source_case': source_case, 'source_srid': srid, 'table': table, 'item_id': item_id,
                          'locator': f'addp://engine/{engine}/path/public/{table}?type=table&item_id={item_id}',
                          'filename': 'KML-成果-' + source_case + '-' + report['run_id'],
                          'download_file': 'manager-kml-' + source_case + '.kml',
                          'dialog_screenshot': 'manager-kml-' + source_case + '-dialog.png',
                          'screenshot': 'manager-kml-' + source_case + '.png'})
        initializer = helpers.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], 30)
        registration = importlib.import_module('scripts.test.online-engine-registration')
        grants = registration.initialize_exact_record_read_grants(initializer, client, tenant,
                     [(engine, 'schema', 'public', case['table']) for case in cases])
        report['source_grants'] = [{'request_id': grant['request_id'], 'item_fingerprint': grant['item_fingerprint'],
                                    'boundary_hash': grant['boundary_hash']} for grant in grants]
        context = {'run_id': report['run_id'], 'identity': identity, 'cases': cases}
        browser_env = dict(environment, ADDP_ONLINE_MANAGER_KML_JSON=json.dumps(context, ensure_ascii=False))
        subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/manager-kml-export.spec.js',
                        '--config=playwright.online.config.js'], cwd=repository / 'console/frontend', env=browser_env, check=True)
        browser = json.loads((artifact_dir / 'manager-kml-export-browser.json').read_text())
        report['cases'] = validate_browser_report(browser, context, artifact_dir)
        for item_id, previous in before_meta.items():
            require(client.request('GET', f'/api/v1/meta/items/{item_id}', (200,)).payload == previous, 'KML export modified source metadata')
        report['source_after'] = source_snapshot(repository, environment)
        require(report['source_after'] == before, 'KML export modified source rows or structure')
        report.update(result='passed', source_unchanged=True, source_metadata_unchanged=True,
                      cleanup_scope='export sessions and objects follow existing retention; disposable source/application/Infra removed by Hosted lifecycle')
        return report
    except BaseException:
        report['result'] = 'failed'
        raise
    finally:
        if 'source_before' in report and 'source_after' not in report:
            try:
                after = source_snapshot(repository, environment)
                report['source_after'] = after
                report['source_unchanged'] = after == report['source_before']
            except (OSError, ValueError, subprocess.SubprocessError) as failure:
                report['source_verification_error'] = type(failure).__name__
        (artifact_dir / 'manager-kml-export.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
