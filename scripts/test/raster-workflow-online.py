#!/usr/bin/env python3
"""Real Develop raster execution -> automatic Meta scan/lineage -> Monitor/Console."""
from __future__ import annotations

import importlib
import json
import math
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.parse

support = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')
GatewayClient, SuiteError = support.GatewayClient, support.SuiteError
obj, array, positive = support._object, support._array, support.positive_int
PERMISSIONS = {'develop.task.read', 'develop.task.execute', 'develop.data_read.execute',
               'develop.data_write.execute', 'system.execution_authorization.create',
               'meta.catalog.read', 'meta.scan_task.read', 'meta.scan_task.execute',
               'meta.lineage.read', 'monitor.execution.read'}
SCHEMA = 'addp.raster-workflow-online/v1'


def validate_identity(client, tenant_id):
    context = obj(client.request('GET', '/api/v1/system/auth/context', (200,)).payload, 'AuthContext')
    principal = obj(context.get('principal'), 'principal')
    tenant = obj(context.get('context'), 'context')
    token = obj(context.get('token'), 'token')
    if principal.get('type') != 'user' or tenant.get('type') != 'tenant' or tenant.get('tenant_id') != str(tenant_id):
        raise SuiteError('raster acceptance requires the configured non-default Tenant User')
    if token.get('type') not in {'first_party_access_token', 'oauth_access_token'}:
        raise SuiteError('raster acceptance requires a User Access Token')
    assignments = array(obj(context.get('authorization'), 'authorization').get('role_assignments'), 'assignments')
    roles = {obj(assignment, 'assignment').get('role_key') for assignment in assignments}
    permissions = {key for assignment in assignments for key in array(assignment.get('permissions'), 'permissions')}
    if roles & support.FORBIDDEN_ADMIN_ROLES or permissions != PERMISSIONS:
        raise SuiteError('raster User must have exactly the scene permissions and no administrator role')
    return {'principal_id': str(positive(principal.get('id'), 'principal.id')), 'tenant_id': str(tenant_id),
            'principal_type': 'user', 'permissions_verified': sorted(PERMISSIONS)}


def wait_execution(client, module, execution_id, timeout, expected='success'):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        execution = obj(client.request('GET', f'/api/v1/{module}/executions/{urllib.parse.quote(execution_id)}', (200,)).payload, 'execution')
        if execution.get('status') in support.TERMINAL_STATUSES:
            if execution['status'] != expected:
                raise SuiteError(f'{module} execution ended with status {execution["status"]}, expected {expected}')
            return execution
        time.sleep(1)
    raise SuiteError(f'{module} execution convergence timed out')


def workflow(source_locator, target_engine_id, expression, mode):
    return {'tasks': [
        {'id': 'load', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': source_locator}},
        {'id': 'math', 'operator': 'raster_band_math', 'depends_on': ['load'],
         'params': {'input_raster': {'$ref': 'load', 'port': 'default'}, 'expression': expression}},
        {'id': 'save', 'operator': 'raster_save', 'depends_on': ['math'], 'params': {
            'input_raster': {'$ref': 'math', 'port': 'default'},
            'target_parent_locator': f'addp://engine/{target_engine_id}/path/raster-target?type=bucket',
            'target_name': 'result.cog.tif', 'write_mode': mode, 'profile': 'cog', 'blocksize': 128,
        }},
    ]}


def submit(client, engine_id, definition):
    response = obj(client.request('POST', '/api/v1/develop/executions', (200,), {
        'dev_type': 'workflow', 'trigger_type': 'manual', 'timeout': 180,
        'content': {'workflow_definition': definition}, 'execution_config': {'engine_id': engine_id},
    }).payload, 'execution submission')
    value = response.get('execution_id')
    if not isinstance(value, str) or not value:
        raise SuiteError('Develop did not return an execution_id')
    return value


def validate_success(execution, source_locator, target_locator, mode):
    metadata = obj(execution.get('metadata'), 'Develop metadata')
    resource = obj(obj(obj(metadata.get('outputs'), 'outputs').get('save'), 'save output').get('resource'), 'resource')
    if resource != {'locator': target_locator, 'type': 'object', 'write_mode': mode}:
        raise SuiteError('Develop stable output has the wrong locator/type/write_mode')
    facts = obj(metadata.get('lineage_facts'), 'lineage facts')
    if facts.get('schema_version') != 'addp.lineage-facts/v1':
        raise SuiteError('Develop did not persist canonical lineage facts')
    inputs, outputs = array(facts.get('inputs'), 'inputs'), array(facts.get('outputs'), 'outputs')
    if len(inputs) != 1 or inputs[0].get('locator') != source_locator:
        raise SuiteError('Develop lineage does not bind the exact source')
    if len(outputs) != 1 or outputs[0].get('locator') != target_locator or outputs[0].get('write_mode') != mode:
        raise SuiteError('Develop lineage does not bind the exact target/write mode')
    operations = array(facts.get('operations'), 'lineage operations')
    if len(operations) != 1 or operations[0] != {
        'kind': 'derive', 'operator': 'develop', 'input_ports': ['input'], 'output_ports': ['output'],
    }:
        raise SuiteError('Develop lineage operation does not match the raster derivation')
    result = obj(metadata.get('result'), 'result')
    artifact = obj(result.get('final_result'), 'raster artifact')
    if (artifact.get('artifact_type'), artifact.get('format'), artifact.get('profile')) != ('raster', 'tiff', 'cog'):
        raise SuiteError('Develop result is not a public raster COG artifact')
    if (artifact.get('width'), artifact.get('height'), artifact.get('band_count')) != (256, 256, 1):
        raise SuiteError('Develop raster artifact dimensions are invalid')
    if artifact.get('source_crs') != 'EPSG:4326' or artifact.get('extent_srid') != 4326:
        raise SuiteError('Develop raster artifact CRS is invalid')
    transform = array(artifact.get('transform'), 'artifact transform')
    if len(transform) != 6 or any(not math.isclose(actual, expected, abs_tol=1e-10) for actual, expected in
                                 zip(transform, (110, .01, 0, 20.32, 0, -.01))):
        raise SuiteError('Develop raster artifact transform is invalid')
    bands = array(artifact.get('bands'), 'artifact bands')
    if len(bands) != 1 or bands[0].get('dtype') != 'Float64' or bands[0].get('nodata_is_nan') is not True:
        raise SuiteError('Develop raster artifact did not preserve band dtype/NoData')
    forbidden = {'access_plan', 'connection_info', 'access_key', 'secret_key', 'password', 'path', 'workspace'}
    def check(value):
        if isinstance(value, dict):
            if forbidden & value.keys():
                raise SuiteError('public raster artifact exposes private runtime fields')
            for item in value.values(): check(item)
        elif isinstance(value, list):
            for item in value: check(item)
    check(artifact)
    runs = array(result.get('meta_scan_runs'), 'automatic scan runs')
    if len(runs) != 1 or runs[0].get('status') != 'submitted' or runs[0].get('target_locator') != target_locator:
        raise SuiteError('Develop did not automatically submit the target Meta scan')
    scan_id = runs[0].get('execution_id')
    if not isinstance(scan_id, str) or not scan_id:
        raise SuiteError('automatic scan execution_id is missing')
    return facts, scan_id


def validate_target_item(item):
    attributes = obj(item.get('attributes'), 'target attributes')
    if obj(attributes.get('item'), 'item facts').get('format') != 'tiff':
        raise SuiteError('Meta did not identify the target TIFF')
    media = obj(obj(attributes.get('type_info'), 'type info').get('media'), 'media facts')
    if (media.get('width'), media.get('height')) != (256, 256):
        raise SuiteError('Meta target dimensions are invalid')
    tiff = obj(obj(attributes.get('format_info'), 'format info').get('tiff'), 'TIFF facts')
    if tiff.get('profile') != 'cog' or tiff.get('is_tiled') is not True:
        raise SuiteError('Meta did not identify the COG profile hint')
    spatial = obj(obj(attributes.get('capabilities'), 'capabilities').get('spatial'), 'spatial facts')
    extent = array(spatial.get('extent'), 'spatial extent')
    if spatial.get('srid') != 4326 or len(extent) != 4 or any(
        not math.isclose(actual, expected, abs_tol=1e-10) for actual, expected in zip(extent, (110, 17.76, 112.56, 20.32))
    ):
        raise SuiteError('Meta target CRS/extent are invalid')


def wait_lineage(client, source_id, target_id, execution_id, timeout):
    query = urllib.parse.urlencode({'subject_kind': 'data_item', 'item_id': target_id, 'direction': 'upstream'})
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        graph = obj(client.request('GET', '/api/v1/meta/lineage/graph?' + query, (200,)).payload, 'lineage graph')
        if graph.get('truncated'):
            raise SuiteError('raster lineage graph is truncated')
        edges = array(graph.get('edges'), 'lineage edges')
        for edge in edges:
            if (edge.get('source', {}).get('item_id'), edge.get('target', {}).get('item_id'),
                edge.get('evidence', {}).get('execution_id'), edge.get('status')) == (source_id, target_id, execution_id, 'active'):
                return {'source_item_id': source_id, 'target_item_id': target_id, 'execution_id': execution_id}
        time.sleep(1)
    raise SuiteError('automatic raster lineage did not converge')


def physical(repository, env, action):
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-minio-fixture.py', action],
                            cwd=repository, env=env, capture_output=True, text=True, timeout=300)
    if result.returncode:
        raise SuiteError(f'physical raster {action} failed ({result.returncode})')
    payload = obj(json.loads(result.stdout), 'physical raster evidence')
    if payload.get('cog_valid') is not True or payload.get('source_unchanged') is not True or payload.get('valid_pixels') != 65535:
        raise SuiteError('physical raster verification is incomplete')
    return payload


def browser(repository, env, evidence):
    report_path = Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / 'raster-workflow-browser.json'
    report_path.unlink(missing_ok=True)
    browser_env = dict(env, ADDP_ONLINE_RASTER_EVIDENCE=json.dumps(evidence))
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/raster-workflow.spec.js',
                             '--config=playwright.online.config.js'], cwd=repository / 'console/frontend',
                            env=browser_env, stdout=sys.stderr, stderr=sys.stderr, timeout=240)
    if result.returncode or not report_path.is_file():
        raise SuiteError('raster Console acceptance failed or report is missing')
    report = obj(json.loads(report_path.read_text()), 'browser report')
    expected = {**evidence, 'schema_version': 'addp.raster-workflow-browser/v1', 'result': 'passed'}
    if report != expected:
        raise SuiteError('raster Console report does not bind the exact run/User/execution/items')
    return report


def run_scenario(repository, env, client, physical_runner=physical, browser_runner=browser):
    tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
    if tenant <= 1:
        raise SuiteError('raster acceptance forbids the default Tenant')
    source_engine = positive(env['ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID'], 'source engine')
    target_engine = positive(env['ADDP_ONLINE_RASTER_TARGET_ENGINE_ID'], 'target engine')
    if source_engine == target_engine:
        raise SuiteError('raster fixture requires distinct source and target Engines')
    timeout = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '180'))
    identity = validate_identity(client, tenant)
    engines = array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'workflow engines')
    runtime = [item for item in engines if item.get('engine_type') == 'geopython_workflow' and item.get('connection_status') == 'online']
    if len(runtime) != 1:
        raise SuiteError('exactly one online GeoPython Workflow Runtime must be registered')
    engine_id = positive(runtime[0].get('id'), 'runtime engine')
    support.wait_for_meta_scan(client, source_engine, time.monotonic() + timeout)
    source = support.find_fixture_item(client, source_engine, 'raster-source/source.tif', 'raster source')
    source_locator = support.build_item_locator(source_engine, source)
    target_locator = f'addp://engine/{target_engine}/path/raster-target/result.cog.tif?type=object'
    executions = []
    physical_evidence = []
    last_scan = ''
    for mode, expression, status in [('create', 'b2-b1', 'success'), ('create', 'b2+b1', 'failed'), ('replace', 'b2+b1', 'success')]:
        identifier = submit(client, engine_id, workflow(source_locator, target_engine, expression, mode))
        execution = wait_execution(client, 'develop', identifier, timeout, status)
        metadata = obj(execution.get('metadata'), 'execution metadata')
        if status == 'success':
            facts, last_scan = validate_success(execution, source_locator, target_locator, mode)
            wait_execution(client, 'meta', last_scan, timeout)
        else:
            if metadata.get('outputs') or metadata.get('lineage_facts'):
                raise SuiteError('failed create published stable outputs or success lineage')
            if 'target object already exists' not in obj(execution.get('error_details'), 'failed create error').get('message', ''):
                raise SuiteError('duplicate create must fail because the target already exists')
        monitor = obj(client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{identifier}', (200,)).payload, 'Monitor execution')
        if monitor.get('status') != status or monitor.get('module') != 'develop':
            raise SuiteError('Monitor does not match the Develop execution status/owner')
        if status == 'success':
            if obj(monitor.get('metadata'), 'Monitor metadata').get('lineage_facts') != facts:
                raise SuiteError('Monitor lineage differs from Develop facts')
        native = physical_runner(repository, env, 'verify-replace' if mode == 'replace' else 'verify-create')
        if len(physical_evidence) == 1 and native.get('sha256') != physical_evidence[0].get('sha256'):
            raise SuiteError('failed create modified the existing target')
        if mode == 'replace' and native.get('sha256') == physical_evidence[0].get('sha256'):
            raise SuiteError('replace did not change the target pixels')
        physical_evidence.append(native)
        executions.append({'execution_id': identifier, 'write_mode': mode, 'status': status})
    target = support.find_fixture_item(client, target_engine, 'raster-target/result.cog.tif', 'COG target')
    # This DataItem must already exist after the automatic scan; no manual scan here.
    target = obj(client.request('GET', f'/api/v1/meta/items/{target["id"]}', (200,)).payload, 'target DataItem')
    validate_target_item(target)
    lineage = wait_lineage(client, source['id'], target['id'], executions[-1]['execution_id'], timeout)
    browser_evidence = browser_runner(repository, env, {
        'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'principal_id': identity['principal_id'],
        'tenant_id': str(tenant), 'execution_id': executions[-1]['execution_id'],
        'source_item_id': source['id'], 'target_item_id': target['id'],
        'source_locator': source_locator, 'target_locator': target_locator,
    })
    return {'schema_version': SCHEMA, 'suite': 'raster-workflow', 'result': 'passed',
            'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'identity': identity, 'executions': executions,
            'automatic_target_scan_execution_id': last_scan, 'lineage': lineage,
            'physical': physical_evidence, 'browser': browser_evidence,
            'cleanup': {'scope': 'disposable-hosted-deployment', 'owner': 'online-hosted-raster-gate.sh'}}


def main():
    env = dict(os.environ)
    required = ('ADDP_ONLINE_TEST_RUN_ID', 'ADDP_ONLINE_TEST_TENANT_ID', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
                'ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID', 'ADDP_ONLINE_RASTER_TARGET_ENGINE_ID',
                'ADDP_ONLINE_TEST_USER_USERNAME', 'ADDP_ONLINE_TEST_USER_PASSWORD',
                'ADDP_ONLINE_ARTIFACT_DIR', 'GATEWAY_URL', 'CONSOLE_URL')
    if any(not env.get(key) for key in required):
        raise SuiteError('required raster Online environment is missing')
    client = GatewayClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
    report = run_scenario(Path(__file__).resolve().parents[2], env, client)
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'Raster Online acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr)
        raise SystemExit(1)
