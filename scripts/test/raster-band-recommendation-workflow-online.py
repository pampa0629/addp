#!/usr/bin/env python3
"""Registered sources -> read-only OIF object; no artifact, output scan or derive."""
from __future__ import annotations

import importlib
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import time

raster = importlib.import_module('scripts.test.raster-workflow-online')
rasterize = importlib.import_module('scripts.test.vector-rasterize-persistence-online')
fixture = importlib.import_module('business.scripts.online-raster-band-recommendation-fixture')
scan_ids = importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
support = raster.support
obj, array, positive, SuiteError = raster.obj, raster.array, raster.positive, raster.SuiteError
SUITE = fixture.SUITE


class DiagnosticClient(rasterize.DiagnosticClient):
    diagnostic_name = 'band-recommendation-submission-error.json'
    diagnostic_schema = 'addp.raster-band-recommendation-submission-diagnostic/v1'


def definition(sources, name, *, invalid=False):
    inventory = fixture.FAILURES if invalid else fixture.CASES
    if name not in inventory:
        raise SuiteError('unknown exact OIF case')
    case = inventory[name]
    return {'tasks': [
        {'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': sources[case['source']]}},
        {'id': 'recommend', 'operator': 'raster_recommend_band_combination', 'depends_on': ['source'],
         'params': {'input_raster': {'$ref': 'source'}, 'bands': case['bands'].copy()}}]}


def physical(repository, env):
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-band-recommendation-fixture.py', 'verify'],
                            cwd=repository, env=dict(env), text=True, capture_output=True, timeout=150)
    if result.returncode:
        raise SuiteError('independent OIF source/object oracle failed: ' + result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'OIF physical evidence')


def compare(actual, expected, path='result'):
    if isinstance(expected, dict):
        if not isinstance(actual, dict) or actual.keys() != expected.keys():
            raise SuiteError('OIF object keys differ at ' + path)
        for key, value in expected.items():
            compare(actual[key], value, path + '.' + key)
    elif isinstance(expected, list):
        if not isinstance(actual, list) or len(actual) != len(expected):
            raise SuiteError('OIF array length differs at ' + path)
        for index, value in enumerate(expected):
            compare(actual[index], value, path + '[' + str(index) + ']')
    elif isinstance(expected, float):
        if (type(actual) not in (float, int) or not math.isfinite(actual) or not math.isfinite(expected)
            or not math.isclose(actual, expected, rel_tol=1e-10, abs_tol=1e-11)):
            raise SuiteError('OIF numeric value differs at ' + path)
    elif type(actual) is not type(expected) or actual != expected:
        raise SuiteError('OIF exact value differs at ' + path)


def check_physical(native, previous=None):
    if native.get('no_outputs') is not True or native.get('runtime_workspaces_removed') is not True:
        raise SuiteError('read-only physical output or workspace residual')
    sources = obj(native.get('sources'), 'source digests')
    facts = obj(native.get('source_physical'), 'source physical facts')
    if set(sources) != set(fixture.SOURCES) or set(facts) != set(fixture.SOURCES):
        raise SuiteError('source inventory changed')
    for name in fixture.SOURCES:
        digest = obj(sources[name], 'source digest')
        fact = obj(facts[name], 'source physical fact')
        if (type(digest.get('size_bytes')) is not int or digest['size_bytes'] <= 0
            or not isinstance(digest.get('sha256'), str) or len(digest['sha256']) != 64
            or any(fact.get(key) != digest[key] for key in ('size_bytes', 'sha256'))
            or fact.get('full_pixels_and_mask_verified') is not True or fact.get('grid_crs_scale_verified') is not True):
            raise SuiteError('source size, digest or complete physical evidence is invalid')
    expected = obj(native.get('expectations'), 'oracle expectations')
    if set(expected) != set(fixture.CASES):
        raise SuiteError('independent object oracle inventory differs')
    if previous is not None and (sources != previous['sources'] or expected != previous['expectations']):
        raise SuiteError('source or independent oracle changed between executions')
    # Fail on NaN/Infinity even if a malformed oracle would otherwise compare equally.
    try:
        json.dumps(expected, allow_nan=False)
    except (ValueError, TypeError) as error:
        raise SuiteError('independent oracle contains a non-JSON numeric value') from error


def graph(client, identifier):
    result = obj(client.request('GET', f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={identifier}&direction=both', (200,)).payload,
                 'source lineage graph')
    if result.get('truncated') is not False or array(result.get('edges'), 'source edges'):
        raise SuiteError('read-only source unexpectedly has derive lineage or a truncated graph')
    return result


def actor(execution, identity, module):
    if (execution.get('module') != module or type(execution.get('triggered_by')) is not int
        or execution['triggered_by'] != int(identity['principal_id'])):
        raise SuiteError('execution owner or triggering user differs')


def execution_snapshot(execution, expected, runtime):
    config = obj(execution.get('execution_config'), 'professional execution config')
    if type(config.get('engine_id')) is not int or config['engine_id'] != runtime:
        raise SuiteError('execution selected a different Runtime')
    content = obj(config.get('content'), 'professional content snapshot')
    if content.get('workflow_definition') != expected:
        raise SuiteError('current execution source or OIF parameters differ from the submitted public DAG')


def no_publication(execution):
    metadata = obj(execution.get('metadata'), 'execution metadata')
    result = metadata.get('result')
    result = {} if result is None else obj(result, 'execution result')
    if execution.get('outputs') or metadata.get('lineage_facts') or result.get('produced_targets') or result.get('meta_scan_runs'):
        raise SuiteError('read-only OIF execution published a resource, derive fact or scan')


def run_scenario(repository, env, client, physical_runner=physical):
    timeout = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
    if tenant <= 1:
        raise SuiteError('OIF acceptance forbids the default tenant')
    identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
    engine = positive(env['ADDP_ONLINE_BAND_RECOMMENDATION_NFS_ENGINE_ID'], 'NFS engine')
    scopes = {name: f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
    source_scan = support.wait_for_meta_scan(client, engine, time.monotonic() + timeout, scope={'targets': list(scopes.values())})
    items = {name: support.find_fixture_item(client, engine, name, 'OIF source') for name in fixture.SOURCES}
    sources = {name: scopes[name] + f'&item_id={items[name]["id"]}' for name in fixture.SOURCES}
    before = {name: client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload for name, item in items.items()}
    for value in before.values():
        if type(value.get('size_bytes')) is not int or value['size_bytes'] <= 0:
            raise SuiteError('source Meta size must be a positive integer')
    scanned = raster.wait_execution(client, 'meta', source_scan, timeout)
    if type(scanned.get('triggered_by')) is not int or scanned['triggered_by'] != int(identity['principal_id']):
        raise SuiteError('source scan changed the triggering user')
    runtimes = array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'workflow engines')
    selected = [runtime for runtime in runtimes if runtime.get('engine_type') == 'geopython_workflow' and runtime.get('connection_status') == 'online']
    if len(selected) != 1:
        raise SuiteError('expected exactly one online product GeoPython Runtime')
    runtime = positive(selected[0].get('id'), 'Runtime engine')
    original_graphs = {name: graph(client, item['id']) for name, item in items.items()}
    baseline = physical_runner(repository, env)
    check_physical(baseline)
    for name in fixture.SOURCES:
        if before[name]['size_bytes'] != baseline['sources'][name]['size_bytes']:
            raise SuiteError('source Meta size differs from the physical source')
    baseline_scans = scan_ids(client)
    if baseline_scans != [source_scan]:
        raise SuiteError('expected only the explicit exact source scan')
    cases, failures = [], []
    for name in fixture.CASES:
        dag = definition(sources, name)
        identifier = raster.submit(client, runtime, dag)
        execution = raster.wait_execution(client, 'develop', identifier, timeout)
        actor(execution, identity, 'develop')
        execution_snapshot(execution, dag, runtime)
        actual = raster.transient_result(execution)
        compare(actual, baseline['expectations'][name])
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor execution')
        actor(monitor, identity, 'develop')
        if monitor.get('status') != 'success':
            raise SuiteError('Monitor status differs')
        no_publication(monitor)
        native = physical_runner(repository, env)
        check_physical(native, baseline)
        if scan_ids(client) != baseline_scans:
            raise SuiteError('read-only recommendation added a scan')
        cases.append({'name': name, 'execution_id': identifier, 'source_locator': sources[fixture.CASES[name]['source']],
                      'json_result': actual, 'complete_object_verified': True, 'monitor_verified': True,
                      'current_execution_input_verified': True, 'no_publication_or_scan': True})
    if cases[0]['json_result'] != cases[1]['json_result']:
        raise SuiteError('candidate ordering changed the canonical recommendation')
    for name in fixture.FAILURES:
        dag = definition(sources, name, invalid=True)
        identifier = raster.submit(client, runtime, dag)
        execution = raster.wait_execution(client, 'develop', identifier, timeout, expected='failed')
        actor(execution, identity, 'develop')
        execution_snapshot(execution, dag, runtime)
        no_publication(execution)
        if scan_ids(client) != baseline_scans:
            raise SuiteError('invalid OIF parameters added a scan')
        native = physical_runner(repository, env)
        check_physical(native, baseline)
        failures.append({'problem': name, 'execution_id': identifier, 'status': 'failed', 'no_publication_or_scan': True})
    for name, item in items.items():
        if client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload != before[name]:
            raise SuiteError('source metadata changed')
        if graph(client, item['id']) != original_graphs[name]:
            raise SuiteError('read-only execution changed source lineage')
    return {'schema_version': 'addp.' + SUITE + '/v1', 'suite': SUITE, 'result': 'passed', 'run_id': env['ADDP_ONLINE_TEST_RUN_ID'],
            'identity': identity, 'sources': sources, 'source_scan_execution_id': source_scan, 'source_unchanged': True,
            'cases': cases, 'failed_parameters': failures, 'physical': native,
            'no_resource_outputs_or_derive': True, 'only_source_scan_verified': True,
            'cleanup_boundary': 'owned NFS export/directory and disposable deployment; no persisted OIF artifact'}


def main():
    env = dict(os.environ)
    report = run_scenario(Path(__file__).resolve().parents[2], env,
                          DiagnosticClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30, env['ADDP_ONLINE_ARTIFACT_DIR']))
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / 'band-recommendation-workflow.json').write_text(json.dumps(report, allow_nan=False, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps(report, allow_nan=False, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'OIF workflow acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr)
        raise SystemExit(1)
