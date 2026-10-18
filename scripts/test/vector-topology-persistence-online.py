#!/usr/bin/env python3
"""Ordinary User -> GeoPython dual outputs -> exact scans/lineage -> Console."""
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time

raster = importlib.import_module('scripts.test.raster-workflow-online')
fixture = importlib.import_module('business.scripts.online-vector-topology-fixture')
support, SuiteError = raster.support, raster.SuiteError
obj, array, positive = raster.obj, raster.array, raster.positive
SUITE = fixture.SUITE
PERMISSIONS = raster.PERMISSIONS | {'manager.data_item.read'}


def definition(source, parent):
    return {'tasks': [
        {'id': 'load', 'operator': 'load', 'params': {'locator': source}, 'depends_on': []},
        {'id': 'build', 'operator': 'polygonize', 'params': {'input_gdf': {'$ref': 'load'}}, 'depends_on': ['load']},
        *[{'id': 'save_' + port, 'operator': 'save', 'depends_on': ['build'], 'params': {
            'input_df': {'$ref': 'build'} if port == 'faces' else {'$ref': 'build', 'port': 'diagnostics'},
            'target_parent_locator': parent, 'target_name': name, 'mode': 'fail'}}
          for port, name in zip(('faces','diagnostics'), fixture.TARGETS)]
    ]}


def validate_success(execution, source, targets):
    metadata = obj(execution.get('metadata'), 'execution metadata')
    outputs = obj(execution.get('outputs'), 'stable outputs')
    expected = {'save_' + key: {'resource': {'locator': target, 'type': 'file'}} for key, target in targets.items()}
    if outputs != expected:
        raise SuiteError('expected both exact named file outputs')
    facts = obj(metadata.get('lineage_facts'), 'lineage facts')
    if (facts.get('schema_version') != 'addp.lineage-facts/v1'
        or [entry.get('locator') for entry in array(facts.get('inputs'), 'inputs')] != [source]
        or sorted(entry.get('locator') for entry in array(facts.get('outputs'), 'outputs')) != sorted(targets.values())):
        raise SuiteError('dual-output resource lineage differs')
    scans = array(obj(metadata.get('result'), 'result').get('meta_scan_runs'), 'automatic scans')
    if (len(scans) != 2 or {scan.get('target_locator') for scan in scans} != set(targets.values())
        or any(scan.get('status') != 'submitted' or not isinstance(scan.get('execution_id'), str) or not scan['execution_id'] for scan in scans)
        or len({scan['execution_id'] for scan in scans}) != 2):
        raise SuiteError('expected exactly two distinct automatic precise file scans')
    return facts, scans


def physical(repository, env):
    result = subprocess.run([sys.executable, 'business/scripts/online-vector-topology-fixture.py', 'verify'],
        cwd=repository, env=dict(env), text=True, capture_output=True, timeout=150)
    if result.returncode:
        raise SuiteError('independent topology physical verification failed: ' + result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'physical evidence')


def validate_item(item, native, name):
    size = item.get('size_bytes')
    if type(size) is not int or size <= 0 or type(native.get('size_bytes')) is not int or size != native['size_bytes'] or not item.get('scanned_at'):
        raise SuiteError('physical and Meta sizes must be equal positive integers')
    if item.get('item_type') != 'file' or item.get('full_name') != 'outputs/' + name or obj(obj(item.get('attributes'), 'attributes').get('item'), 'item facts').get('format') != 'geojson':
        raise SuiteError('GeoJSON file identity differs')
    return {'physical_size_bytes': size, 'meta_size_bytes': size}


def browser(repository, env, evidence):
    path = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / 'topology-browser.json'
    path.unlink(missing_ok=True)
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/vector-topology-persistence.spec.js',
        '--config=playwright.online.config.js'], cwd=repository/'console/frontend',
        env=dict(env, ADDP_ONLINE_TOPOLOGY_EVIDENCE=json.dumps(evidence)), stdout=sys.stderr, stderr=sys.stderr, timeout=240)
    if result.returncode or not path.is_file():
        raise SuiteError('topology Console evidence missing or failed')
    report = obj(json.loads(path.read_text()), 'browser evidence')
    if report != {**evidence, 'schema_version': 'addp.topology-browser/v1', 'result': 'passed'}:
        raise SuiteError('browser report does not bind the exact run, User and outputs')
    return report


def run_scenario(repository, env, client, physical_runner=physical, browser_runner=browser):
    tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
    if tenant <= 1:
        raise SuiteError('topology forbids the default Tenant')
    identity = raster.validate_identity(client, tenant, required_permissions=PERMISSIONS)
    timeout = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    engine = positive(env['ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID'], 'NFS engine')
    source_scope = f'addp://engine/{engine}/path/source.geojson?type=file'
    source_scan = support.wait_for_meta_scan(client, engine, time.monotonic()+timeout, scope={'targets': [source_scope]})
    source = support.find_fixture_item(client, engine, 'source.geojson', 'topology source')
    source_locator = source_scope + f'&item_id={source["id"]}'
    source_before = client.request('GET', f'/api/v1/meta/items/{source["id"]}', (200,)).payload
    runtimes = array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'runtimes')
    selected = [value for value in runtimes if value.get('engine_type') == 'geopython_workflow' and value.get('connection_status') == 'online']
    if len(selected) != 1:
        raise SuiteError('expected exactly one online product GeoPython Runtime')
    parent = f'addp://engine/{engine}/path/outputs?type=directory'
    targets = {key: f'addp://engine/{engine}/path/outputs/{name}?type=file' for key, name in zip(('faces','diagnostics'), fixture.TARGETS)}
    identifier = raster.submit(client, positive(selected[0]['id'], 'Runtime'), definition(source_locator, parent))
    execution = raster.wait_execution(client, 'develop', identifier, timeout)
    facts, scans = validate_success(execution, source_locator, targets)
    for scan in scans:
        scanned = raster.wait_execution(client, 'meta', scan['execution_id'], timeout)
        if (type(scanned.get('tenant_id')) is not int or scanned['tenant_id'] != tenant
            or type(scanned.get('triggered_by')) is not int or scanned['triggered_by'] != int(identity['principal_id'])):
            raise SuiteError('automatic scan User or Tenant differs')
    monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor')
    if monitor.get('status') != 'success' or monitor.get('module') != 'develop' or obj(monitor.get('metadata'), 'metadata').get('lineage_facts') != facts:
        raise SuiteError('Monitor status, owner or lineage differs')
    native = physical_runner(repository, env)
    if native.get('exact_inventory_verified') is not True or set(native.get('targets', {})) != set(fixture.TARGETS):
        raise SuiteError('missing exact physical target inventory')
    outputs = []
    for key, name in zip(('faces','diagnostics'), fixture.TARGETS):
        item = support.find_fixture_item(client, engine, 'outputs/' + name, 'topology output')
        item = obj(client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload, 'output item')
        sizes = validate_item(item, native['targets'][name], name)
        lineage = raster.wait_lineage(client, source['id'], item['id'], identifier, timeout)
        outputs.append({'port': key, 'name': name, 'locator': targets[key], 'item_id': item['id'], 'size_evidence': sizes, 'lineage': lineage})
    if client.request('GET', f'/api/v1/meta/items/{source["id"]}', (200,)).payload != source_before:
        raise SuiteError('source metadata changed')
    # Browser evidence deliberately contains only stable identities and expected content.
    evidence = {'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], **identity, 'execution_id': identifier,
                'outputs': [{key: output[key] for key in ('port','name','locator','item_id')} for output in outputs]}
    report = browser_runner(repository, env, evidence)
    if physical_runner(repository, env) != native:
        raise SuiteError('browser reads changed source or saved outputs')
    return {'schema_version': 'addp.vector-topology-persistence/v1', 'suite': SUITE, 'result': 'passed',
        'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'identity': identity, 'execution_id': identifier,
        'source_scan_execution_id': source_scan, 'source_unchanged': True, 'automatic_scans': scans,
        'outputs': outputs, 'physical': native, 'browser': report,
        'cleanup_boundary': 'owned NFS export/directory and disposable deployment; no business DELETE acceptance'}


if __name__ == '__main__':
    try:
        env = dict(os.environ)
        report = run_scenario(Path(__file__).resolve().parents[2], env,
            raster.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30))
        (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-persistence.json').write_text(json.dumps(report, indent=2)+'\n')
        print(json.dumps(report, sort_keys=True))
    except Exception as error:
        print('Topology acceptance failed: ' + (str(error) if isinstance(error, SuiteError) else type(error).__name__), file=sys.stderr)
        raise SystemExit(1)
