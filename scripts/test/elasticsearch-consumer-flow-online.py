#!/usr/bin/env python3
"""Verify an authenticated ES index through System, Meta, Manager and Develop."""
from __future__ import annotations
import importlib.util
import json
import os
from pathlib import Path
import sys
import subprocess
import time
import urllib.parse

path = Path(__file__).with_name('security-transfer-protection-online.py')
spec = importlib.util.spec_from_file_location('elasticsearch_online_support', path)
SUPPORT = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = SUPPORT
spec.loader.exec_module(SUPPORT)
SuiteError = SUPPORT.SuiteError


def locator(engine_id, item):
    # A concrete index name, including its dots, is one catalog path component.
    name = urllib.parse.quote(item['full_name'], safe='')
    query = urllib.parse.urlencode({'type': 'index', 'item_id': item['id']})
    return f'addp://engine/{engine_id}/path/{name}?{query}'


def validate_rows(rows, expected_count, projected=False, ordered=False):
    if len(rows) != expected_count:
        raise SuiteError('Elasticsearch result count differs from the sample fixture')
    expected_ids = [str(9007199254740993 + number) for number in range(expected_count)]
    actual_ids = []
    for row in rows:
        number = row.get('order_id')
        if not isinstance(number, str) or number not in expected_ids:
            raise SuiteError('Elasticsearch long precision was lost')
        actual_ids.append(number)
        sample_number = int(number) - 9007199254740993
        if row.get('customer') != {'name': 'customer-' + str(sample_number)}:
            raise SuiteError('Elasticsearch object shape was lost')
        if projected and set(row) - {'order_id', 'customer'}:
            raise SuiteError('Elasticsearch source projection exposed extra fields')
        if not projected and row.get('items') != [{'sku': 'SKU-001', 'quantity': sample_number + 1}]:
            raise SuiteError('Elasticsearch nested array shape was lost')
    if len(set(actual_ids)) != expected_count:
        raise SuiteError('Elasticsearch result repeats or omits sample records')
    if ordered and actual_ids != expected_ids:
        raise SuiteError('Elasticsearch sorting differs from fixture')


def execute_query(client, engine_id, target, query, deadline):
    started = SUPPORT._object(client.request('POST', '/api/v1/develop/executions', (200,), {
        'dev_type': 'query', 'trigger_type': 'manual',
        'content': {'query_type': 'es_dsl', 'query': json.dumps(query), 'target_locator': target, 'query_parameters': []},
        'execution_config': {'engine_id': engine_id}, 'parameters': {}, 'timeout': 120,
    }).payload, 'Develop execution')
    execution_id = started.get('execution_id')
    if not isinstance(execution_id, str) or not execution_id:
        raise SuiteError('Develop execution ID is missing')
    while time.monotonic() < deadline:
        execution = SUPPORT._object(client.request('GET', '/api/v1/develop/executions/' + urllib.parse.quote(execution_id), (200,)).payload, 'Develop execution')
        if execution.get('status') == 'success':
            result = SUPPORT._object(execution.get('metadata', {}).get('result'), 'Develop result')
            summary = SUPPORT._object(result.get('summary'), 'Develop summary')
            return execution_id, SUPPORT._array(summary.get('preview_rows'), 'Develop rows')
        if execution.get('status') in {'failed', 'timeout', 'cancelled'}:
            raise SuiteError('Develop Elasticsearch query did not succeed')
        time.sleep(1)
    raise SuiteError('Develop Elasticsearch query timed out')


def run(client, tenant_id, engine_id, timeout):
    # The standard Online runner enforces a dedicated tenant and deployment.
    context = SUPPORT._object(client.request('GET', '/api/v1/system/auth/context', (200,)).payload, 'identity')
    principal = SUPPORT._object(context.get('principal'), 'principal')
    tenant = SUPPORT._object(context.get('context'), 'tenant context')
    authorization = SUPPORT._object(context.get('authorization'), 'authorization')
    if principal.get('type') != 'user' or tenant.get('type') != 'tenant' or tenant.get('tenant_id') != str(tenant_id):
        raise SuiteError('Elasticsearch Online user or tenant mismatch')
    assignments = SUPPORT._array(authorization.get('role_assignments'), 'role assignments')
    if any(role.get('role_key') in SUPPORT.FORBIDDEN_ADMIN_ROLES for role in assignments if isinstance(role, dict)):
        raise SuiteError('Online consumer must use a regular user')
    engines = SUPPORT._array(client.request('GET', '/api/v1/system/engine-catalog/engines', (200,)).payload, 'catalog engines')
    selected = [engine for engine in engines if int(engine.get('id', 0)) == engine_id]
    if len(selected) != 1 or selected[0].get('engine_type') != 'elasticsearch':
        raise SuiteError('Online catalog must contain the dedicated Elasticsearch engine')
    deadline = time.monotonic() + timeout
    scan = SUPPORT.wait_for_scan(client, engine_id, deadline)
    item = SUPPORT.find_item(client, engine_id, 'addp_orders.v1', 'index')
    empty = SUPPORT.find_item(client, engine_id, 'addp_empty.v1', 'index')
    target = locator(engine_id, item)
    _, rows = SUPPORT.preview_rows(client, target)
    validate_rows(rows, 25)
    _, empty_rows = SUPPORT.preview_rows(client, locator(engine_id, empty))
    if empty_rows:
        raise SuiteError('Empty index preview contains records')
    execution, queried = execute_query(client, engine_id, target, {
        'query': {'term': {'tags': 'sample'}}, 'size': 25,
        'sort': [{'order_id': 'asc'}], '_source': ['order_id', 'customer'],
    }, deadline)
    validate_rows(queried, 25, projected=True, ordered=True)
    return {'engine_id': engine_id, 'tenant_id': tenant_id, 'principal_id': str(principal['id']),
            'scan_execution_id': scan, 'query_execution_id': execution,
            'index_locator': target, 'empty_index_locator': locator(engine_id, empty),
            'index_item_id': item['id'], 'empty_item_id': empty['id'],
            'manager_rows': len(rows), 'develop_rows': len(queried), 'empty_index': True}


def run_browser(report):
    artifacts = Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR')).resolve()
    report_file = artifacts / 'elasticsearch-console.json'
    report_file.unlink(missing_ok=True)
    environment = dict(os.environ, ADDP_ONLINE_ELASTICSEARCH_EXPECTATIONS=json.dumps(report),
                       ADDP_ONLINE_ELASTICSEARCH_BROWSER_REPORT=str(report_file))
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/elasticsearch-consumer-flow.spec.js',
                             '--config=playwright.online.config.js'], cwd=Path(__file__).resolve().parents[2] / 'console/frontend', env=environment)
    if result.returncode != 0 or not report_file.is_file():
        raise SuiteError('Real Console Elasticsearch browser acceptance failed or omitted evidence')
    evidence = json.loads(report_file.read_text())
    expected = {'run_id': SUPPORT.required_environment('ADDP_ONLINE_TEST_RUN_ID'),
                'engine_id': report['engine_id'], 'tenant_id': report['tenant_id'], 'principal_id': report['principal_id'],
                'manager_rows': 25, 'develop_rows': 25, 'empty_index': True, 'meta_ui_scan': True, 'develop_ui_query': True}
    if evidence != expected:
        raise SuiteError('Console browser evidence does not match API identity and results')
    for name in ('meta', 'orders', 'empty', 'query'):
        if not (artifacts / f'elasticsearch-{name}-console.png').is_file():
            raise SuiteError('Console Elasticsearch screenshot evidence is missing')
    return evidence


def main():
    if os.environ.get('ADDP_ONLINE_TEST') != '1' or os.environ.get('ADDP_ONLINE_HOSTED') != '1':
        raise SuiteError('Elasticsearch suite requires the Hosted Online entry')
    tenant = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_TEST_TENANT_ID'), 'tenant')
    engine = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_ELASTICSEARCH_ENGINE_ID'), 'engine')
    timeout = float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '900'))
    client = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), min(timeout, 30))
    report = run(client, tenant, engine, timeout)
    report['browser'] = run_browser(report)
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'), 'elasticsearch-consumer-flow.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (SuiteError, OSError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
