#!/usr/bin/env python3
"""HDFS System -> Meta -> Manager -> formal Develop/Spark -> real Console."""
from __future__ import annotations
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import urllib.parse
import urllib.request

path = Path(__file__).with_name('security-transfer-protection-online.py')
spec = importlib.util.spec_from_file_location('hdfs_online_support', path)
support = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = support
spec.loader.exec_module(support)
SuiteError = support.SuiteError
PERMISSIONS = {'system.engine_catalog.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read',
               'manager.data_item.read', 'manager.content.read', 'develop.task.read', 'develop.task.execute',
               'develop.data_read.execute', 'system.execution_authorization.create'}
FILES = {'csv': 'samples/orders.csv', 'json': 'samples/orders.json', 'parquet': 'samples/orders.parquet',
         'original': '订单 100%.csv'}


def locator(engine_id, item):
    path = '/'.join(urllib.parse.quote(part, safe='') for part in item['full_name'].split('/'))
    query = urllib.parse.urlencode({'type': 'file', 'item_id': item['id']})
    return f'addp://engine/{engine_id}/path/{path}?{query}'


def validate_identity(client, tenant_id):
    context = support._object(client.request('GET', '/api/v1/system/auth/context', (200,)).payload, 'identity')
    principal, tenant = context['principal'], context['context']
    assignments = support._array(context['authorization']['role_assignments'], 'role assignments')
    permissions = {key for assignment in assignments for key in assignment['permissions']}
    if (tenant_id <= 1 or principal.get('type') != 'user' or tenant.get('type') != 'tenant'
        or tenant.get('tenant_id') != str(tenant_id) or permissions != PERMISSIONS
        or any(role.get('role_key') in support.FORBIDDEN_ADMIN_ROLES for role in assignments)):
        raise SuiteError('HDFS acceptance requires exactly the read workflow scene permissions of a dedicated Tenant User')
    return str(support.positive_int(principal['id'], 'principal'))


def validate_rows(rows):
    expected = [{'id': number, 'region': 'east' if number % 2 == 0 else 'west', 'amount': number * 10}
                for number in range(1, 21)]
    normalized = [{'id': int(row['id']), 'region': row['region'], 'amount': int(row['amount'])} for row in rows]
    if sorted(normalized, key=lambda row: row['id']) != expected:
        raise SuiteError('HDFS preview differs from the 20 row / 2100 amount Business fixture')


def workflow(locators):
    def reference(task): return {'$ref': task, 'port': 'default'}
    tasks = []
    for format_name in ('csv', 'json', 'parquet'):
        load, total = 'load_' + format_name, 'total_' + format_name
        tasks.extend([
            {'id': load, 'operator': 'load', 'depends_on': [],
             'params': {'source_type': 'file', 'locator': locators[format_name]}},
            {'id': total, 'operator': 'group_by', 'depends_on': [load], 'params': {
                'input_df': reference(load), 'group_columns': ['region'],
                'agg_exprs': {format_name + '_rows': 'count(*)', format_name + '_amount_sum': 'sum(cast(amount as bigint))'}}},
        ])
    for name, left, right in (('join_csv_json', 'total_csv', 'total_json'),
                              ('verify_all_formats', 'join_csv_json', 'total_parquet')):
        tasks.append({'id': name, 'operator': 'join', 'depends_on': [left, right], 'params': {
            'left_df': reference(left), 'right_df': reference(right), 'how': 'inner', 'on': 'region'}})
    return {'tasks': tasks}


def validate_result(execution):
    if execution.get('status') != 'success' or execution.get('outputs'):
        raise SuiteError('HDFS read workflow must succeed without persistent outputs')
    result = support._object(execution['metadata']['result'], 'Develop result')
    final = support._object(result['final_result'], 'final result')
    expected = []
    for region, amount in (('east', 1100), ('west', 1000)):
        row = {'region': region}
        for name in ('csv', 'json', 'parquet'):
            row[name + '_rows'], row[name + '_amount_sum'] = 10, amount
        expected.append(row)
    if (final.get('type') != 'spark_dataframe' or sorted(final.get('preview_rows', []), key=lambda row: row['region']) != expected
        or not result.get('runtime_execution_id') or result.get('produced_targets') or result.get('meta_scan_runs')):
        raise SuiteError('Develop must report all three formats as 20 rows / 2100 amount and preserve its Runtime reference')
    return final


def worker_evidence(engine_id):
    with urllib.request.urlopen('http://127.0.0.1:18080/json', timeout=10) as response:
        master = json.load(response)
    applications = [app for app in master.get('activeapps', []) if app.get('name') == f'ADDP-Workflow-Engine-{engine_id}']
    if len(applications) != 1 or applications[0].get('state') != 'RUNNING' or applications[0].get('cores', 0) < 1 or master.get('aliveworkers') != 1:
        raise SuiteError('Formal workflow must allocate cores on the disposable Standalone Worker')
    app_id = applications[0].get('id', '')
    if not re.fullmatch(r'app-[0-9]+-[0-9]+', app_id):
        raise SuiteError('invalid Standalone Application ID')
    container = 'addp-hdfs-online-worker'
    owned = subprocess.run(['docker', 'container', 'inspect', '--format', '{{index .Config.Labels "com.addp.online-fixture"}}', container],
                           check=True, capture_output=True, text=True)
    if owned.stdout.strip() != 'hdfs-spark-consumer-flow':
        raise SuiteError('Worker evidence must belong to this disposable fixture')
    # The exact Application directory binds task completion to the formal execution.
    logs = subprocess.run(['docker', 'exec', container, 'bash', '-c', 'cat /opt/spark/work/"$1"/*/stderr', '_', app_id],
                          check=True, capture_output=True, text=True)
    tasks = re.findall(r'Finished task ([0-9.]+) in stage ([0-9.]+)', logs.stdout)
    if not tasks:
        raise SuiteError('Standalone Application has no completed Executor tasks')
    return {'application_id': app_id, 'application_name': applications[0]['name'],
            'worker_container': container, 'completed_tasks': len(tasks), 'cores': applications[0]['cores']}


def run(client, tenant_id, engine_id, cluster_id, timeout, physical=worker_evidence):
    if engine_id == cluster_id:
        raise SuiteError('HDFS source and Spark cluster must be distinct Engines')
    principal = validate_identity(client, tenant_id)
    engines = support._array(client.request('GET', '/api/v1/system/engine-catalog/engines', (200,)).payload, 'catalog engines')
    for identifier, engine_type in ((engine_id, 'hdfs'), (cluster_id, 'spark')):
        matches = [engine for engine in engines if engine.get('id') == identifier and engine.get('engine_type') == engine_type]
        if len(matches) != 1:
            raise SuiteError('System catalog must expose the exact disposable HDFS and Spark Engines')
    runtimes = support._array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'runtimes')
    selected = [runtime for runtime in runtimes if runtime.get('engine_type') == 'spark_workflow' and runtime.get('connection_status') == 'online']
    if len(selected) != 1:
        raise SuiteError('exactly one online self-registered Spark Workflow Runtime is required')
    runtime_id = support.positive_int(selected[0]['id'], 'Runtime')
    deadline = time.monotonic() + timeout
    scan = support.wait_for_scan(client, engine_id, deadline)
    locators, item_ids = {}, {}
    for name, filename in FILES.items():
        item = support.find_item(client, engine_id, filename, 'file')
        locators[name], item_ids[name] = locator(engine_id, item), item['id']
        _, rows = support.preview_rows(client, locators[name])
        validate_rows(rows)
    definition = workflow(locators)
    started = support._object(client.request('POST', '/api/v1/develop/executions', (200,), {
        'dev_type': 'workflow', 'trigger_type': 'manual', 'timeout': 300,
        'content': {'workflow_definition': definition},
        'execution_config': {'engine_id': runtime_id, 'engine_specific': {'spark_cluster_id': cluster_id}},
    }).payload, 'Develop submission')
    execution_id = started.get('execution_id')
    if not isinstance(execution_id, str) or not execution_id:
        raise SuiteError('Develop omitted its execution ID')
    while time.monotonic() < deadline:
        execution = support._object(client.request('GET', '/api/v1/develop/executions/' + urllib.parse.quote(execution_id), (200,)).payload, 'execution')
        if execution.get('status') in support.TERMINAL_STATUSES:
            final = validate_result(execution)
            break
        time.sleep(1)
    else:
        raise SuiteError('HDFS workflow did not converge')
    return {'schema_version': 'addp.hdfs-spark-consumer-online/v1', 'engine_id': engine_id, 'cluster_id': cluster_id,
            'runtime_id': runtime_id, 'tenant_id': tenant_id, 'principal_id': principal, 'locators': locators,
            'item_ids': item_ids, 'scan_execution_id': scan, 'execution_id': execution_id,
            'runtime_execution_id': execution['metadata']['result']['runtime_execution_id'],
            'final_result': final, 'worker': physical(cluster_id)}


def run_browser(report):
    artifacts = Path(support.required_environment('ADDP_ONLINE_ARTIFACT_DIR')).resolve()
    output = artifacts / 'hdfs-console.json'
    output.unlink(missing_ok=True)
    environment = dict(os.environ, ADDP_ONLINE_HDFS_EXPECTATIONS=json.dumps(report), ADDP_ONLINE_HDFS_BROWSER_REPORT=str(output))
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/hdfs-spark-consumer-flow.spec.js',
                             '--config=playwright.online.config.js'], cwd=Path(__file__).resolve().parents[2] / 'console/frontend', env=environment)
    if result.returncode != 0 or not output.is_file():
        raise SuiteError('Real HDFS Console browser acceptance failed or omitted evidence')
    evidence = json.loads(output.read_text())
    expected = {'run_id': support.required_environment('ADDP_ONLINE_TEST_RUN_ID'), 'engine_id': report['engine_id'],
                'tenant_id': report['tenant_id'], 'principal_id': report['principal_id'], 'execution_id': report['execution_id'],
                'meta_ui_scan': True, 'previews': 4, 'develop_result': True}
    if evidence != expected:
        raise SuiteError('HDFS browser identity or execution evidence differs from API acceptance')
    for name in ('meta', 'csv', 'json', 'parquet', 'original', 'workflow'):
        if not (artifacts / ('hdfs-' + name + '-console.png')).is_file():
            raise SuiteError('HDFS Console screenshot evidence is missing')
    return evidence


def main():
    if os.environ.get('ADDP_ONLINE_TEST') != '1' or os.environ.get('ADDP_ONLINE_HOSTED') != '1':
        raise SuiteError('HDFS suite requires the Hosted Online entry')
    require = support.required_environment
    timeout = float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '900'))
    client = support.GatewayClient(require('GATEWAY_URL'), require('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), min(timeout, 30))
    report = run(client, support.positive_int(require('ADDP_ONLINE_TEST_TENANT_ID'), 'tenant'),
                 support.positive_int(require('ADDP_ONLINE_HDFS_ENGINE_ID'), 'HDFS'),
                 support.positive_int(require('ADDP_ONLINE_SPARK_ENGINE_ID'), 'Spark'), timeout)
    report['browser'] = run_browser(report)
    Path(require('ADDP_ONLINE_ARTIFACT_DIR'), 'hdfs-spark-consumer-flow.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (SuiteError, OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
