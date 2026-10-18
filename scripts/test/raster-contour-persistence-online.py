#!/usr/bin/env python3
"""Contour -> existing GeoPython save -> exact automatic Meta scan and lineage."""
from __future__ import annotations
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time

raster = importlib.import_module('scripts.test.raster-workflow-online')
fixture = importlib.import_module('business.scripts.online-raster-contour-fixture')
support = raster.support
obj, array, positive, SuiteError = raster.obj, raster.array, raster.positive, raster.SuiteError
SUITE = fixture.SUITE


def definition(source, parent, name):
    if name not in fixture.TARGETS:
        raise SuiteError('unknown contour target')
    contour = {'input_raster': {'$ref': 'load'}, 'elevation_unit': 'metre', 'band': 1}
    contour.update({'levels': fixture.TARGETS[name]} if name == 'levels.gpkg' else {'interval': 100})
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source}},
        {'id': 'contour', 'operator': 'raster_contour', 'depends_on': ['load'], 'params': contour},
        {'id': 'save', 'operator': 'save', 'depends_on': ['contour'], 'params': {
            'input_df': {'$ref': 'contour'}, 'target_parent_locator': parent, 'target_name': name, 'mode': 'replace'}},
    ]}


def validate_success(execution, source, target):
    metadata = obj(execution.get('metadata'), 'execution metadata')
    resource = {'locator': target, 'type': 'file', 'write_mode': 'replace'}
    if obj(obj(execution.get('outputs'), 'outputs').get('save'), 'save output').get('resource') != resource:
        raise SuiteError('contour stable file output differs')
    facts = obj(metadata.get('lineage_facts'), 'lineage facts')
    if (facts.get('schema_version') != 'addp.lineage-facts/v1'
        or [value.get('locator') for value in array(facts.get('inputs'), 'lineage inputs')] != [source]
        or [value.get('locator') for value in array(facts.get('outputs'), 'lineage outputs')] != [target]
        or facts['outputs'][0].get('write_mode') != 'replace'):
        raise SuiteError('contour resource lineage differs')
    scans = array(obj(metadata.get('result'), 'result').get('meta_scan_runs'), 'automatic scans')
    if len(scans) != 1 or scans[0].get('status') != 'submitted' or scans[0].get('target_locator') != target or not isinstance(scans[0].get('execution_id'), str) or not scans[0]['execution_id']:
        raise SuiteError('expected exactly one automatic scan of the precise output file')
    return facts, scans[0]['execution_id']


def validate_item(item, physical, name):
    size = item.get('size_bytes')
    if type(size) is not int or size <= 0 or type(physical.get('size_bytes')) is not int or size != physical['size_bytes'] or not item.get('scanned_at'):
        raise SuiteError('physical file and Meta sizes must be equal positive integers with a scan timestamp')
    attrs = obj(item.get('attributes'), 'GeoPackage attributes')
    descriptor = obj(attrs.get('item'), 'item facts')
    if descriptor != {'layout': 'single', 'data_type': 'container', 'format': 'geopackage'}:
        raise SuiteError('GeoPackage must be registered as one container file')
    types = obj(attrs.get('type_info'), 'type facts')
    children = array(obj(types.get('container'), 'container facts').get('children'), 'container children')
    layers = [entry for entry in children if entry.get('name') == physical['layer_name']]
    if (len(children) != 1 or len(layers) != 1 or layers[0].get('child_kind') != 'layer'
        or obj(layers[0].get('native'), 'layer native locator').get('table') != physical['layer_name']
        or 'table' in types or 'spatial' in obj(attrs.get('capabilities', {}), 'capabilities')):
        raise SuiteError('layer facts must remain inside the GeoPackage container boundary')
    if item.get('item_type') != 'file' or item.get('full_name') != 'outputs/' + name:
        raise SuiteError('GeoPackage file identity differs')
    return {'physical_size_bytes': size, 'meta_size_bytes': size, 'layer_name': physical['layer_name']}


def physical(repository, env, names):
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-contour-fixture.py', 'verify', *names],
        cwd=repository, env=dict(env), text=True, capture_output=True, timeout=150)
    if result.returncode:
        raise SuiteError('independent contour physical verification failed: ' + result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'physical contour evidence')


def run_scenario(repository, env, client, physical_runner=physical):
    timeout = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
    if tenant <= 1:
        raise SuiteError('contour acceptance forbids the default tenant')
    identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
    engine = positive(env['ADDP_ONLINE_CONTOUR_NFS_ENGINE_ID'], 'NFS engine')
    # File catalog paths are relative to the engine export, without its host path.
    source_name = 'dem.tif'
    source_scope = f'addp://engine/{engine}/path/{source_name}?type=file'
    source_scan = support.wait_for_meta_scan(client, engine, time.monotonic() + timeout,
                                            scope={'targets': [source_scope]})
    source = support.find_fixture_item(client, engine, source_name, 'contour DEM source')
    source_locator = source_scope + f'&item_id={source["id"]}'
    source_before = client.request('GET', f'/api/v1/meta/items/{source["id"]}', (200,)).payload
    runtimes = array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'workflow engines')
    selected = [entry for entry in runtimes if entry.get('engine_type') == 'geopython_workflow' and entry.get('connection_status') == 'online']
    if len(selected) != 1:
        raise SuiteError('expected exactly one online product GeoPython Runtime')
    runtime = positive(selected[0].get('id'), 'Runtime engine')
    parent = f'addp://engine/{engine}/path/outputs?type=directory'
    cases, names, previous, scans = [], [], {}, []
    for name in fixture.TARGETS:
        target = f'addp://engine/{engine}/path/outputs/{name}?type=file'
        identifier = raster.submit(client, runtime, definition(source_locator, parent, name))
        execution = raster.wait_execution(client, 'develop', identifier, timeout)
        facts, scan = validate_success(execution, source_locator, target)
        if scan in scans:
            raise SuiteError('different files must have distinct automatic scan executions')
        scans.append(scan)
        scanned = raster.wait_execution(client, 'meta', scan, timeout)
        if str(scanned.get('tenant_id')) != str(tenant) or str(scanned.get('actor_principal_id')) != identity['principal_id']:
            raise SuiteError('automatic scan did not retain the same tenant/user identity')
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor execution')
        if monitor.get('status') != 'success' or monitor.get('module') != 'develop' or obj(monitor.get('metadata'), 'Monitor metadata').get('lineage_facts') != facts:
            raise SuiteError('Monitor owner, terminal status or lineage differs')
        names.append(name)
        native = physical_runner(repository, env, names)
        if native.get('runtime_workspaces_removed') is not True:
            raise SuiteError('Runtime execution workspace cleanup was not verified')
        for prior, digest in previous.items():
            if native['targets'].get(prior) != digest:
                raise SuiteError('later contour execution changed an earlier GeoPackage')
        previous = native['targets'].copy()
        item = support.find_fixture_item(client, engine, 'outputs/' + name, 'contour GeoPackage')
        item = obj(client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload, 'GeoPackage detail')
        sizes = validate_item(item, native['targets'][name], name)
        lineage = raster.wait_lineage(client, source['id'], item['id'], identifier, timeout)
        cases.append({'name': name, 'execution_id': identifier, 'automatic_scan_execution_id': scan,
            'target_locator': target, 'target_item_id': item['id'], 'size_evidence': sizes,
            'physical': native['targets'][name], 'lineage': lineage, 'monitor_verified': True})
    if client.request('GET', f'/api/v1/meta/items/{source["id"]}', (200,)).payload != source_before:
        raise SuiteError('source metadata changed during contour persistence')
    return {'schema_version': 'addp.raster-contour-persistence/v1', 'suite': SUITE, 'result': 'passed',
        'run_id': env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity': identity, 'source_locator': source_locator, 'source_item_id': source['id'],
        'source_scan_execution_id': source_scan, 'source_unchanged': True,
        'cases': cases, 'physical': native,
        'cleanup_boundary': 'owned NFS export/directory and disposable deployment; no business DELETE acceptance'}


def main():
    env = dict(os.environ)
    report = run_scenario(Path(__file__).resolve().parents[2], env,
        raster.GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30))
    path = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / 'contour-persistence.json'
    path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'Contour persistence acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr)
        raise SystemExit(1)
