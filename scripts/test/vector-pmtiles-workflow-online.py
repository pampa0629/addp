#!/usr/bin/env python3
"""PMTiles public Develop execution, immutable conflict, replacement and preview T4."""
from __future__ import annotations
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.parse

raster = importlib.import_module('scripts.test.raster-workflow-online')
support = raster.support
obj, array, positive, SuiteError = raster.obj, raster.array, raster.positive, raster.SuiteError
PERMISSIONS = raster.PERMISSIONS | {'manager.data_item.read', 'manager.derived_artifact.read'}
SUITE = 'vector-pmtiles-workflow'


def definition(source, target_engine, mode, polygons):
    return {'tasks': [{'id': 'convert', 'operator': 'vector_to_pmtiles', 'depends_on': [], 'params': {
        'locator': source, 'target_parent_locator': f'addp://engine/{target_engine}/path/pmtiles-target?type=bucket',
        'target_name': 'result.pmtiles', 'write_mode': mode, 'min_zoom': 0, 'max_zoom': 5,
        'layer_name': 'areas_after' if polygons else 'points_before', 'attributes': ['name', 'rank'] if polygons else []}}]}


def physical(repository, env, action):
    result = subprocess.run([sys.executable, '-m', 'business.scripts.online-vector-pmtiles-fixture', action],
                            cwd=repository, env=env, capture_output=True, text=True, timeout=300)
    if result.returncode: raise SuiteError(f'PMTiles physical {action} failed ({result.returncode})')
    return obj(json.loads(result.stdout), 'physical PMTiles evidence')


def browser(repository, env, evidence):
    path = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / f'pmtiles-{evidence["phase"]}-browser.json'
    path.unlink(missing_ok=True)
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/vector-pmtiles-workflow.spec.js',
        '--config=playwright.online.config.js'], cwd=repository/'console/frontend',
        env=dict(env, ADDP_ONLINE_PMTILES_EVIDENCE=json.dumps(evidence)), stdout=sys.stderr, stderr=sys.stderr, timeout=240)
    if result.returncode or not path.is_file(): raise SuiteError('PMTiles Console evidence missing or failed')
    report = obj(json.loads(path.read_text()), 'browser evidence')
    if report != {**evidence, 'schema_version': 'addp.pmtiles-browser/v1', 'result': 'passed'}:
        raise SuiteError('PMTiles browser evidence does not bind exact run, identity, target and content')
    return report


def graph(client, item_id):
    query = urllib.parse.urlencode({'subject_kind': 'data_item', 'item_id': item_id, 'direction': 'upstream'})
    return obj(client.request('GET', '/api/v1/meta/lineage/graph?'+query, (200,)).payload, 'current graph')


def wait_current_graph(client, source, target, execution, timeout):
    deadline = time.monotonic()+timeout
    while time.monotonic() < deadline:
        value = graph(client, target)
        edges = array(value.get('edges'), 'current edges')
        if (value.get('truncated') is False and len(value.get('nodes', [])) == 2 and len(edges) == 1
            and edges[0].get('source', {}).get('item_id') == source
            and edges[0].get('target', {}).get('item_id') == target
            and edges[0].get('evidence', {}).get('execution_id') == execution
            and edges[0].get('status') == 'active' and edges[0].get('relation_kind') == 'derive'):
            return value
        time.sleep(1)
    raise SuiteError('PMTiles exact current lineage did not converge')


def scan_ids(client):
    response = obj(client.request('GET', '/api/v1/meta/scan/runs?page=1&page_size=100', (200,)).payload, 'scans')
    items = array(response.get('items'), 'scan items')
    if response.get('total') != len(items): raise SuiteError('PMTiles scan snapshot is truncated')
    identifiers = [item.get('execution_id') for item in items]
    if any(not isinstance(value, str) or not value for value in identifiers) or len(set(identifiers)) != len(identifiers):
        raise SuiteError('PMTiles scan identities missing or duplicated')
    return sorted(identifiers)


def item(client, engine):
    target = support.find_fixture_item(client, engine, 'pmtiles-target/result.pmtiles', 'PMTiles target')
    return obj(client.request('GET', f'/api/v1/meta/items/{target["id"]}', (200,)).payload, 'target detail')


def validate_success(execution, source, target, mode, native):
    metadata = obj(execution.get('metadata'), 'execution metadata')
    expected = {'locator': target, 'type': 'object', 'write_mode': mode}
    if obj(obj(execution.get('outputs'), 'outputs').get('convert'), 'convert').get('resource') != expected:
        raise SuiteError('PMTiles stable output differs')
    facts = obj(metadata.get('lineage_facts'), 'lineage facts')
    if (facts.get('schema_version') != 'addp.lineage-facts/v1'
        or [value.get('locator') for value in facts.get('inputs', [])] != [source]
        or len(facts.get('outputs', [])) != 1
        or any(facts['outputs'][0].get(key) != value for key,value in expected.items() if key != 'type')):
        raise SuiteError('PMTiles execution lineage differs')
    result = obj(metadata.get('result'), 'result')
    artifact = obj(result.get('final_result'), 'PMTiles artifact')
    archive = obj(native.get('archive'), 'physical archive')
    if any(artifact.get(key) != value for key,value in {
        'archive_format': 'pmtiles', 'spec_version': 3, 'tile_format': 'mvt', 'tile_compression': 'gzip',
        'min_zoom': 0, 'max_zoom': 5, 'actual_max_zoom': 5, 'extent_srid': 4326,
        'header_hash': archive['header_hash'], 'archive_size_bytes': native['targets']['result.pmtiles']['size_bytes']}.items()):
        raise SuiteError('PMTiles public artifact differs from the physical archive')
    scans = array(result.get('meta_scan_runs'), 'automatic scans')
    if len(scans) != 1 or scans[0].get('status') != 'submitted' or scans[0].get('target_locator') != target or not scans[0].get('execution_id'):
        raise SuiteError('PMTiles output lacks exactly one automatic target scan')
    return facts, scans[0]['execution_id']



def validate_item(target, native):
    physical = native['targets']['result.pmtiles']
    if (type(target.get('size_bytes')) is not int or target['size_bytes'] <= 0
        or target['size_bytes'] != physical['size_bytes'] or not target.get('scanned_at')):
        raise SuiteError('PMTiles automatic scan size/timestamp missing')
    attrs = obj(target.get('attributes'), 'PMTiles attributes')
    info = obj(obj(attrs.get('format_info'), 'format info').get('pmtiles'), 'PMTiles header metadata')
    if (obj(attrs.get('item'), 'item facts').get('format') != 'pmtiles'
        or any(info.get(key) != value for key,value in {'spec_version': 3, 'tile_type': 'mvt',
            'tile_compression': 'gzip', 'min_zoom': 0, 'max_zoom': 5,
            'header_hash': native['archive']['header_hash']}.items())):
        raise SuiteError('PMTiles automatic Meta header facts differ from physical archive')


def validate_conflict(execution, before_native, after_native, before_item, after_item, before_scans, after_scans, before_graph, after_graph):
    metadata = obj(execution.get('metadata'), 'failed metadata')
    result = metadata.get('result') or {}
    encoded = json.dumps(execution.get('error_details', {}))
    if (execution.get('status') != 'failed' or execution.get('outputs') or metadata.get('lineage_facts')
        or result.get('meta_scan_runs') or result.get('produced_targets')
        or '"reason": "target_exists"' not in encoded or '"stage": "result"' not in encoded):
        raise SuiteError('PMTiles create conflict lacks controlled failure or published success facts')
    if before_native != after_native: raise SuiteError('PMTiles create conflict changed bytes, object timestamps or source content')
    if before_item != after_item or before_scans != after_scans: raise SuiteError('PMTiles create conflict changed catalog or scans')
    if before_graph != after_graph: raise SuiteError('PMTiles create conflict changed current lineage')


def run_scenario(repository, env, client, physical_runner=physical, browser_runner=browser):
    tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
    if tenant <= 1: raise SuiteError('PMTiles forbids the default Tenant')
    identity = raster.validate_identity(client, tenant, required_permissions=PERMISSIONS)
    timeout = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '180'))
    source_engine = positive(env['ADDP_ONLINE_PMTILES_SOURCE_ENGINE_ID'], 'source engine')
    target_engine = positive(env['ADDP_ONLINE_PMTILES_TARGET_ENGINE_ID'], 'target engine')
    if source_engine == target_engine: raise SuiteError('PMTiles requires independent source and target Engines')
    engines = array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'engines')
    runtime = [engine for engine in engines if engine.get('engine_type') == 'geopython_workflow' and engine.get('connection_status') == 'online']
    if len(runtime) != 1: raise SuiteError('exactly one online product GeoPython Runtime is required')
    runtime_id = positive(runtime[0].get('id'), 'Runtime')
    support.wait_for_meta_scan(client, source_engine, time.monotonic()+timeout)
    baseline = json.loads((Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'pmtiles-fixture-baseline.json').read_text())
    target_locator = f'addp://engine/{target_engine}/path/pmtiles-target/result.pmtiles?type=object'
    phases = []
    previous, before_item = None, None
    for mode, polygons in [('create', False), ('replace', True)]:
        source = support.find_fixture_item(client, source_engine, 'pmtiles-source/'+('areas.shp' if polygons else 'points.geojson'), 'vector source')
        source_locator = support.build_item_locator(source_engine, source)
        execution_id = raster.submit(client, runtime_id, definition(source_locator, target_engine, mode, polygons))
        execution = raster.wait_execution(client, 'develop', execution_id, timeout)
        native = physical_runner(repository, env, 'verify-replace' if polygons else 'verify-create')
        if native['sources'] != baseline['sources'] or native['targets'].get('keep.txt') != baseline['targets']['keep.txt'] or set(native['targets']) != {'result.pmtiles', 'keep.txt'}:
            raise SuiteError('PMTiles changed sources or unrelated target content')
        facts, scan = validate_success(execution, source_locator, target_locator, mode, native)
        raster.wait_execution(client, 'meta', scan, timeout)
        target = item(client, target_engine)
        validate_item(target, native)
        current = wait_current_graph(client, source['id'], target['id'], execution_id, timeout)
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{execution_id}', (200,)).payload, 'Monitor')
        if monitor.get('status') != 'success' or monitor.get('module') != 'develop' or monitor.get('metadata', {}).get('lineage_facts') != facts:
            raise SuiteError('PMTiles Monitor and Develop facts differ')
        if polygons:
            if (target['id'] != before_item['id'] or target['fingerprint'] != before_item['fingerprint']
                or native['targets']['result.pmtiles']['sha256'] == previous['targets']['result.pmtiles']['sha256']
                or target['scanned_at'] == before_item['scanned_at']):
                raise SuiteError('PMTiles replace did not preserve identity and refresh content/scan')
        evidence = {'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], **identity, 'execution_id': execution_id,
            'phase': mode, 'source_item_id': source['id'], 'target_item_id': target['id'], 'target_locator': target_locator,
            'layer': native['archive']['layer'], 'feature_count': native['archive']['feature_count'],
            'tile_sha256': native['archive']['tile_sha256']}
        browser_report = browser_runner(repository, env, evidence)
        phases.append({'phase': mode, 'execution_id': execution_id, 'automatic_scan_execution_id': scan,
            'target': target, 'lineage': current, 'native': native, 'browser': browser_report})
        if not polygons:
            before_scans = scan_ids(client)
            conflict_id = raster.submit(client, runtime_id, definition(source_locator, target_engine, 'create', True))
            failed = raster.wait_execution(client, 'develop', conflict_id, timeout, 'failed')
            after_native = physical_runner(repository, env, 'verify-create')
            validate_conflict(failed, native, after_native, target, item(client, target_engine), before_scans, scan_ids(client), current, graph(client, target['id']))
            conflict = {'execution_id': conflict_id, 'file_unchanged': True, 'scan_unchanged': True, 'lineage_unchanged': True, 'diagnostic': failed['error_details']}
            previous, before_item = native, target
    return {'schema_version': 'addp.vector-pmtiles-workflow-online/v1', 'suite': SUITE, 'result': 'passed',
        'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'identity': identity, 'phases': phases, 'conflict': conflict,
        'cleanup': {'scope': 'disposable-hosted-deployment', 'owner': 'online-hosted-vector-pmtiles-gate.sh'}}


def main():
    env = dict(os.environ)
    client = raster.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
    print(json.dumps(run_scenario(Path(__file__).resolve().parents[2], env, client), ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try: main()
    except Exception as error:
        print('PMTiles Online acceptance failed: '+(str(error) if isinstance(error, SuiteError) else type(error).__name__), file=sys.stderr)
        raise SystemExit(1)
