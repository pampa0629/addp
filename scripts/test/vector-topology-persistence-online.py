#!/usr/bin/env python3
"""Ordinary User topology/repair/snap/overlap -> exact scans/lineage -> Console."""
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
vector = importlib.import_module('scripts.utils.online_vector_persistence')
scan_ids = importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids


def definition(source, parent, invalid_source, snap_source, snap_reference, overlap_source):
    return {'tasks': [
        {'id': 'load', 'operator': 'load', 'params': {'locator': source}, 'depends_on': []},
        {'id': 'build', 'operator': 'polygonize', 'params': {'input_gdf': {'$ref': 'load'}}, 'depends_on': ['load']},
        *[{'id': 'save_' + port, 'operator': 'save', 'depends_on': ['build'], 'params': {
            'input_df': {'$ref': 'build'} if port == 'faces' else {'$ref': 'build', 'port': 'diagnostics'},
            'target_parent_locator': parent, 'target_name': name, 'mode': 'fail'}}
          for port, name in zip(('faces','diagnostics'), fixture.TARGETS[:2])],
        {'id': 'load_invalid', 'operator': 'load', 'params': {'locator': invalid_source}, 'depends_on': []},
        {'id': 'check', 'operator': 'check_geometry', 'params': {'input_gdf': {'$ref': 'load_invalid'}}, 'depends_on': ['load_invalid']},
        {'id': 'repair', 'operator': 'repair_geometry', 'params': {'input_gdf': {'$ref': 'check'}}, 'depends_on': ['check']},
        {'id': 'recheck', 'operator': 'check_geometry', 'params': {'input_gdf': {'$ref': 'repair'}, 'valid_field': 'after_valid', 'reason_field': 'after_reason'}, 'depends_on': ['repair']},
        {'id': 'save_repaired', 'operator': 'save', 'params': {'input_df': {'$ref': 'recheck'}, 'target_parent_locator': parent, 'target_name': 'repaired.geojson', 'mode': 'fail'}, 'depends_on': ['recheck']},
        *snap_definition(snap_source, snap_reference, parent)['tasks'],
        {'id': 'overlap_input', 'operator': 'load', 'params': {'locator': overlap_source}, 'depends_on': []},
        {'id': 'overlap', 'operator': 'check_polygon_overlap', 'params': {'input_gdf': {'$ref': 'overlap_input'}, 'id_field': 'parcel_id'}, 'depends_on': ['overlap_input']},
        {'id': 'save_overlaps', 'operator': 'save', 'params': {'input_df': {'$ref': 'overlap'}, 'target_parent_locator': parent, 'target_name': 'overlaps.gpkg', 'mode': 'fail'}, 'depends_on': ['overlap']},
    ]}


def snap_definition(source, reference, parent, *, failed=False):
    return {'tasks': [
        {'id': 'snap_input', 'operator': 'load', 'params': {'locator': source}, 'depends_on': []},
        {'id': 'snap_reference', 'operator': 'load', 'params': {'locator': reference}, 'depends_on': []},
        {'id': 'snap', 'operator': 'snap_geometry', 'params': {'input_gdf': {'$ref': 'snap_input'},
            'reference_gdf': {'$ref': 'snap_reference'}, 'tolerance': 1.25 if failed else .75},
            'depends_on': ['snap_input', 'snap_reference']},
        {'id': 'save_snapped', 'operator': 'save', 'params': {'input_df': {'$ref': 'snap'},
            'target_parent_locator': parent, 'target_name': 'failed-snap.gpkg' if failed else 'snapped.gpkg',
            'mode': 'fail'}, 'depends_on': ['snap']},
    ]}


def output_sources(port):
    return (4,) if port == 'overlaps' else (2, 3) if port == 'snapped' else (1,) if port == 'repaired' else (0,)


def validate_failed_snap(execution):
    metadata = obj(execution.get('metadata'), 'failed metadata')
    result = metadata.get('result')
    if result is not None: result = obj(result, 'failed result')
    if (execution.get('status') != 'failed' or execution.get('outputs') or metadata.get('lineage_facts')
        or (result is not None and result.get('meta_scan_runs'))):
        raise SuiteError('collapsed snap must fail without publication or automatic scan')


def collapsed_snap_diagnostic(repository, env, execution_id, timeout=30):
    # Public tenant observations intentionally omit raw Runtime failure text.
    # Match only this execution's fixed cause in the disposable owner's log.
    root = Path(env.get('ADDP_RUNTIME_LOG_ROOT', repository / 'logs/runtime'))
    if not root.is_absolute(): root = repository / root
    prefix = f'[DevExecutor] 工作流引擎调用失败: execution_id={execution_id} err='
    suffix = '任务 snap 执行失败: snapping produced invalid geometries; reduce tolerance or revise reference'
    deadline = time.monotonic() + timeout
    while True:
        for path in (root / 'develop').glob('*/*.jsonl'):
            for line in path.read_text().splitlines():
                try: event = json.loads(line)
                except json.JSONDecodeError: continue
                message = event.get('message', '')
                if (event.get('module_name') == 'develop' and event.get('role') == 'backend'
                    and prefix in message and message.endswith(suffix)):
                    return {'execution_id': execution_id, 'cause': 'invalid_snapped_geometry', 'source': 'develop_runtime_log'}
        if time.monotonic() >= deadline:
            raise SuiteError('collapsed snap lacks its exact owner diagnostic')
        time.sleep(.2)


def validate_success(execution, sources, targets):
    metadata = obj(execution.get('metadata'), 'execution metadata')
    outputs = obj(execution.get('outputs'), 'stable outputs')
    expected = {'save_' + key: {'resource': {'locator': target, 'type': 'file', 'write_mode': 'create'}} for key, target in targets.items()}
    if outputs != expected:
        raise SuiteError('expected five exact named file outputs')
    facts = obj(metadata.get('lineage_facts'), 'lineage facts')
    expected_inputs = [('input.'+node, source) for node, source in zip(
        ('load', 'load_invalid', 'snap_input', 'snap_reference', 'overlap_input'), sources)]
    actual_inputs = [(entry.get('port'), entry.get('locator')) for entry in array(facts.get('inputs'), 'inputs')]
    if (facts.get('schema_version') != 'addp.lineage-facts/v1'
        or len(sources) != len(fixture.SOURCES) or len(actual_inputs) != len(expected_inputs)
        or any(actual_inputs.count(binding) != 1 for binding in expected_inputs)
        or sorted(entry.get('locator') for entry in array(facts.get('outputs'), 'outputs')) != sorted(targets.values())
        or any(entry.get('write_mode') != 'create' for entry in facts['outputs'])):
        raise SuiteError('topology, repair, snap and overlap resource lineage differs')
    scans = array(obj(metadata.get('result'), 'result').get('meta_scan_runs'), 'automatic scans')
    if (len(scans) != len(fixture.TARGETS) or {scan.get('target_locator') for scan in scans} != set(targets.values())
        or any(scan.get('status') != 'submitted' or not isinstance(scan.get('execution_id'), str) or not scan['execution_id'] for scan in scans)
        or len({scan['execution_id'] for scan in scans}) != len(fixture.TARGETS)):
        raise SuiteError('expected exactly five distinct automatic precise file scans')
    return facts, scans


def physical(repository, env):
    result = subprocess.run([sys.executable, 'business/scripts/online-vector-topology-fixture.py', 'verify'],
        cwd=repository, env=dict(env), text=True, capture_output=True, timeout=150)
    if result.returncode:
        raise SuiteError('independent topology physical verification failed: ' + result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'physical evidence')


def validate_item(item, native, name):
    if name in ('snapped.gpkg', 'overlaps.gpkg'):
        return vector.validate_item(item, native, name)
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
    if report != {**evidence, 'schema_version': 'addp.topology-browser/v4', 'result': 'passed'}:
        raise SuiteError('browser report does not bind the exact run, User and outputs')
    return report


def run_scenario(repository, env, client, physical_runner=physical, browser_runner=browser,
                 diagnostic_runner=collapsed_snap_diagnostic):
    tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
    if tenant <= 1:
        raise SuiteError('topology forbids the default Tenant')
    identity = raster.validate_identity(client, tenant, required_permissions=PERMISSIONS)
    timeout = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    engine = positive(env['ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID'], 'NFS engine')
    sources = []
    for name in fixture.SOURCES:
        scope = f'addp://engine/{engine}/path/{name}?type=file'
        scan = support.wait_for_meta_scan(client, engine, time.monotonic()+timeout, scope={'targets': [scope]})
        item = support.find_fixture_item(client, engine, name, 'topology source')
        before = client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload
        sources.append({'name': name, 'id': item['id'], 'locator': scope+f'&item_id={item["id"]}', 'scan_execution_id': scan, 'before': before})
    runtimes = array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'runtimes')
    selected = [value for value in runtimes if value.get('engine_type') == 'geopython_workflow' and value.get('connection_status') == 'online']
    if len(selected) != 1:
        raise SuiteError('expected exactly one online product GeoPython Runtime')
    parent = f'addp://engine/{engine}/path/outputs?type=directory'
    targets = {key: f'addp://engine/{engine}/path/outputs/{name}?type=file' for key, name in zip(fixture.PORTS, fixture.TARGETS)}
    identifier = raster.submit(client, positive(selected[0]['id'], 'Runtime'), definition(sources[0]["locator"], parent, sources[1]["locator"], sources[2]["locator"], sources[3]["locator"], sources[4]["locator"]))
    execution = raster.wait_execution(client, 'develop', identifier, timeout)
    facts, scans = validate_success(execution, [source["locator"] for source in sources], targets)
    scan_observations = []
    for scan in scans:
        scanned = raster.wait_execution(client, 'meta', scan['execution_id'], timeout)
        if (type(scanned.get('tenant_id')) is not int or scanned['tenant_id'] != tenant
            or type(scanned.get('triggered_by')) is not int or scanned['triggered_by'] != int(identity['principal_id'])):
            raise SuiteError('automatic scan User or Tenant differs')
        scan_observations.append({'execution_id': scan['execution_id'], 'tenant_id': scanned['tenant_id'],
                                  'triggered_by': scanned['triggered_by'], 'status': scanned['status']})
    monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor')
    if monitor.get('status') != 'success' or monitor.get('module') != 'develop' or obj(monitor.get('metadata'), 'metadata').get('lineage_facts') != facts:
        raise SuiteError('Monitor status, owner or lineage differs')
    native = physical_runner(repository, env)
    if native.get('exact_inventory_verified') is not True or set(native.get('targets', {})) != set(fixture.TARGETS):
        raise SuiteError('missing exact physical target inventory')
    outputs = []
    for key, name in zip(fixture.PORTS, fixture.TARGETS):
        item = support.find_fixture_item(client, engine, 'outputs/' + name, 'topology output')
        item = obj(client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload, 'output item')
        sizes = validate_item(item, native['targets'][name], name)
        outputs.append({'port': key, 'name': name, 'locator': targets[key], 'item_id': item['id'], 'size_evidence': sizes,
                        **({'fid_column': native['targets'][name]['fid_column']} if key == 'overlaps' else {}),
                        **({'layer_name': native['targets'][name]['layer_name'], 'geometry_column': native['targets'][name]['geometry_column']} if name.endswith('.gpkg') else {})})
    # Persist the verified physical facts before any potentially long convergence
    # wait. This checkpoint cannot stand in for the final successful suite report.
    checkpoint = {'schema_version': 'addp.topology-checkpoint/v4', 'suite': SUITE, 'result': 'incomplete',
        'stage': 'before_lineage', 'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'identity': identity,
        'execution_id': identifier, 'sources': [{key: source[key] for key in ('name', 'id', 'locator', 'scan_execution_id')} for source in sources], 'lineage_facts': facts,
        'automatic_scans': scan_observations, 'outputs': outputs, 'stable_outputs': execution['outputs'], 'physical': native, 'monitor_lineage_verified': True, 'monitor': monitor}
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-before-lineage.json').write_text(json.dumps(checkpoint, indent=2)+'\n')
    for output in outputs:
        output['lineage'] = [raster.wait_lineage(client, sources[index]['id'], output['item_id'], identifier, timeout)
                             for index in output_sources(output['port'])]
    for source in sources:
        if client.request('GET', f'/api/v1/meta/items/{source["id"]}', (200,)).payload != source['before']:
            raise SuiteError('source metadata changed')
    before_scans = scan_ids(client)
    if before_scans != sorted([source['scan_execution_id'] for source in sources] + [scan['execution_id'] for scan in scans]):
        raise SuiteError('unexpected source or automatic scan inventory')
    failed_id = raster.submit(client, positive(selected[0]['id'], 'Runtime'),
        snap_definition(sources[2]['locator'], sources[3]['locator'], parent, failed=True))
    failed = raster.wait_execution(client, 'develop', failed_id, timeout, expected='failed')
    validate_failed_snap(failed)
    diagnostic = diagnostic_runner(repository, env, failed_id)
    failed_monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{failed_id}', (200,)).payload, 'failed Monitor')
    if failed_monitor.get('status') != 'failed' or failed_monitor.get('module') != 'develop':
        raise SuiteError('collapsed snap Monitor owner or terminal status differs')
    if scan_ids(client) != before_scans or physical_runner(repository, env) != native:
        raise SuiteError('collapsed snap changed scans, sources or saved outputs')
    failure = {'execution_id': failed_id, 'status': 'failed', 'no_publication_or_scan': True,
               'monitor': failed_monitor, 'diagnostic': diagnostic}
    failed_metadata = obj(failed.get('metadata'), 'failed metadata')
    failed_result = failed_metadata.get('result')
    failure['observation'] = {'status': failed['status'], 'outputs': failed.get('outputs'),
        'lineage_facts': failed_metadata.get('lineage_facts'),
        'meta_scan_runs': failed_result.get('meta_scan_runs') if isinstance(failed_result, dict) else None}
    # Preserve the already verified negative case even if a later map fails.
    # This remains an incomplete suite checkpoint, never a successful T4 report.
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-snap-failure.json').write_text(json.dumps({
        'schema_version': 'addp.topology-snap-failure/v1', 'result': 'incomplete',
        'stage': 'collapsed_snap_validated', 'run_id': env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity': identity, 'failed_snap': failure, 'scan_inventory': before_scans,
        'physical_unchanged': True}, indent=2)+'\n')
    # Browser evidence deliberately contains only stable identities and expected content.
    evidence = {'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], **identity, 'execution_id': identifier,
                'outputs': [{key: value for key, value in output.items() if key not in ('lineage', 'size_evidence')} for output in outputs],
                'failed_execution_id': failed_id,
                'overlap_expected': fixture.overlap_expected_features(),
                'sources': [{'id': source['id'], 'before': source['before']} for source in sources]}
    report = browser_runner(repository, env, evidence)
    final_native = physical_runner(repository, env)
    if final_native != native:
        raise SuiteError('browser reads changed source or saved outputs')
    return {'schema_version': 'addp.vector-topology-persistence/v4', 'suite': SUITE, 'result': 'passed',
        'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'identity': identity, 'execution_id': identifier,
        'sources': [{key: source[key] for key in ('name', 'id', 'locator', 'scan_execution_id')} for source in sources], 'source_unchanged': True, 'automatic_scans': scans, 'automatic_scan_observations': scan_observations, 'monitor': monitor,
        'outputs': outputs, 'stable_outputs': execution['outputs'], 'physical': native, 'post_browser_physical': final_native, 'browser': report, 'failed_snap': failure,
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
