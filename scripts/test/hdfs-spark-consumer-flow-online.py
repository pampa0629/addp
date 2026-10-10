#!/usr/bin/env python3
"""HDFS System -> Meta -> Manager -> formal Develop/Spark -> real Console."""
from __future__ import annotations
import argparse
import importlib.util
import json
import os
from pathlib import Path
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
spec = importlib.util.spec_from_file_location('hdfs_spark_online_evidence', Path(__file__).with_name('spark-online-evidence.py'))
SPARK = importlib.util.module_from_spec(spec)
spec.loader.exec_module(SPARK)
spec = importlib.util.spec_from_file_location('hdfs_online_registration', Path(__file__).with_name('online-engine-registration.py'))
REGISTRATION = importlib.util.module_from_spec(spec)
spec.loader.exec_module(REGISTRATION)
PERMISSIONS = {'system.engine_catalog.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read',
               'manager.data_item.read', 'manager.content.read', 'develop.task.read', 'develop.task.execute',
               'develop.data_read.execute', 'develop.data_write.execute', 'develop.data_ddl.execute',
               'meta.lineage.read', 'system.execution_authorization.create'}
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
        raise SuiteError('HDFS acceptance requires exactly the persisted workflow scene permissions of a dedicated Tenant User')
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


def runtime_status_evidence(execution_id, final_result):
    try:
        return SPARK.runtime_status_evidence(support.required_environment('ADDP_ONLINE_SPARK_RUNTIME_URL'), execution_id, final_result, 8)
    except ValueError as error:
        raise SuiteError(str(error)) from error


def worker_evidence(engine_id):
    try:
        return SPARK.worker_evidence(engine_id, 'hdfs-spark-consumer-flow', 'addp-hdfs-online-worker', 'http://127.0.0.1:18080')
    except ValueError as error:
        raise SuiteError(str(error)) from error


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
            'final_result': final, 'worker': physical(cluster_id),
            'runtime_status': runtime_status_evidence(execution['metadata']['result']['runtime_execution_id'], final)}


def persistence_workflow(locators, target_engine_id):
    definition = workflow(locators)
    definition['tasks'].append({'id': 'save', 'operator': 'save', 'depends_on': ['verify_all_formats'], 'params': {
        'input_df': {'$ref': 'verify_all_formats', 'port': 'default'},
        'target_type': 'table',
        'target_parent_locator': f'addp://engine/{target_engine_id}/path/results?type=schema',
        'target_name': 'hdfs_totals', 'mode': 'overwrite',
    }})
    return definition


def submit_and_wait(client, definition, runtime_id, cluster_id, deadline):
    started = support._object(client.request('POST', '/api/v1/develop/executions', (200,), {
        'dev_type': 'workflow', 'trigger_type': 'manual', 'timeout': 300,
        'content': {'workflow_definition': definition},
        'execution_config': {'engine_id': runtime_id, 'engine_specific': {'spark_cluster_id': cluster_id}},
    }).payload, 'persisted workflow submission')
    identifier = started.get('execution_id')
    if not isinstance(identifier, str) or not identifier:
        raise SuiteError('persisted workflow omitted execution ID')
    while time.monotonic() < deadline:
        execution = support._object(client.request('GET', '/api/v1/develop/executions/' + urllib.parse.quote(identifier), (200,)).payload, 'persisted execution')
        if execution.get('status') in support.TERMINAL_STATUSES:
            if execution.get('status') != 'success':
                raise SuiteError('persisted workflow failed: ' + str(support.execution_failure_diagnostics(execution)))
            return identifier, execution
        time.sleep(1)
    raise SuiteError('persisted workflow did not converge')


def validate_persistence(execution, locators, target_locator):
    if execution.get('status') != 'success':
        raise SuiteError('Spark persistence execution did not succeed')
    metadata = support._object(execution.get('metadata'), 'persisted metadata')
    result = support._object(metadata.get('result'), 'persisted result')
    resource = execution.get('outputs', {}).get('save', {}).get('resource')
    if resource != {'locator': target_locator, 'type': 'table', 'write_mode': 'replace'}:
        raise SuiteError('Spark stable output must bind the exact table and replace mode')
    final = result.get('final_result')
    if final != {'status': 'success', 'rows': 2, 'target': 'results.hdfs_totals'}:
        raise SuiteError('Spark save must report exactly two committed rows')
    facts = metadata.get('lineage_facts', {})
    if (facts.get('schema_version') != 'addp.lineage-facts/v1'
        or {value.get('locator') for value in facts.get('inputs', [])} != {locators[name] for name in ('csv', 'json', 'parquet')}
        or len(facts.get('outputs', [])) != 1 or facts['outputs'][0].get('locator') != target_locator
        or facts['outputs'][0].get('write_mode') != 'replace'):
        raise SuiteError('Spark save lineage must bind the exact three sources and table target')
    targets = result.get('produced_targets', [])
    if len(targets) != 1 or targets[0].get('locator') != target_locator or targets[0].get('task_id') != 'save':
        raise SuiteError('Spark save omitted its produced target')
    runs = result.get('meta_scan_runs', [])
    if len(runs) != 1 or runs[0].get('status') != 'submitted' or runs[0].get('target_locator') != target_locator or not runs[0].get('execution_id'):
        raise SuiteError('Spark result automatic Meta scan was not submitted')
    return final, runs[0]['execution_id']


def persistence_physical():
    root = Path(support.required_environment('ADDP_ONLINE_SECRET_DIR'))
    before = json.loads((root / 'postgres-before.json').read_text())
    prefix = ['docker', 'exec', 'addp-hdfs-online-postgres', 'psql', '-XAt', '-p', '15435', '-U', 'fixture_admin', '-d', 'spark_results', '-c']
    def query(sql):
        return subprocess.run(prefix + [sql], check=True, capture_output=True, text=True).stdout.strip()
    after = json.loads(query("SELECT json_build_object('oid',oid::text,'acl',relacl::text,'comment',obj_description(oid)) FROM pg_class WHERE oid='results.hdfs_totals'::regclass"))
    if before != after or query("SELECT count(*) FROM pg_tables WHERE schemaname='results' AND tablename LIKE '__addp_spark_stage_%'") != '0':
        raise SuiteError('Spark overwrite replaced target structure/grants or left staging tables')
    return {'table_oid': before['oid'], 'structure_and_grants_preserved': True, 'staging_tables': 0}


def run_persistence(client, report, target_engine_id, timeout):
    deadline = time.monotonic() + timeout
    if target_engine_id in (report['engine_id'], report['cluster_id'], report['runtime_id']):
        raise SuiteError('PostgreSQL target must be a distinct Engine')
    target_locator = f'addp://engine/{target_engine_id}/path/results/hdfs_totals?type=table'
    execution_id, execution = submit_and_wait(client, persistence_workflow(report['locators'], target_engine_id),
                                            report['runtime_id'], report['cluster_id'], deadline)
    final, scan_id = validate_persistence(execution, report['locators'], target_locator)
    def validate_nodes(nodes):
        if set(nodes) != {task['id'] for task in persistence_workflow(report['locators'], target_engine_id)['tasks']} or nodes['save'] != final:
            raise ValueError('persisted Runtime lost canonical node outputs')
        for name, value in nodes.items():
            if name != 'save' and value.get('type') != 'spark_dataframe':
                raise ValueError('persisted Runtime lost DataFrame summary')
    runtime = SPARK.runtime_status_evidence(support.required_environment('ADDP_ONLINE_SPARK_RUNTIME_URL'),
        execution['metadata']['result']['runtime_execution_id'], final, 9, validate_nodes=validate_nodes,
        node_types={task['id']: ('save' if task['id'] == 'save' else 'spark_dataframe')
                    for task in persistence_workflow(report['locators'], target_engine_id)['tasks']})
    while time.monotonic() < deadline:
        scan = support._object(client.request('GET', '/api/v1/meta/executions/' + urllib.parse.quote(scan_id), (200,)).payload, 'automatic result scan')
        if scan.get('status') == 'success':
            break
        if scan.get('status') in support.TERMINAL_STATUSES:
            raise SuiteError('Spark result automatic scan failed')
        time.sleep(1)
    else:
        raise SuiteError('Spark result automatic scan did not converge')
    item = support.find_item(client, target_engine_id, 'results.hdfs_totals', 'table')
    preview_locator = target_locator + '&item_id=' + str(item['id'])
    _, rows = support.preview_rows(client, preview_locator)
    expected_rows = report['final_result']['preview_rows']
    if sorted(rows, key=lambda row: row['region']) != expected_rows:
        raise SuiteError('Manager persisted result differs from the computed totals')
    query = urllib.parse.urlencode({'subject_kind': 'data_item', 'item_id': item['id'], 'direction': 'upstream'})
    expected_sources = {report['item_ids'][name] for name in ('csv', 'json', 'parquet')}
    while time.monotonic() < deadline:
        graph = support._object(client.request('GET', '/api/v1/meta/lineage/graph?' + query, (200,)).payload, 'persisted lineage')
        if graph.get('truncated'):
            raise SuiteError('Spark persisted lineage graph is truncated')
        observed = {edge.get('source', {}).get('item_id') for edge in graph.get('edges', [])
            if edge.get('target', {}).get('item_id') == item['id'] and edge.get('evidence', {}).get('execution_id') == execution_id
            and edge.get('status') == 'active' and edge.get('relation_kind') == 'derive'}
        if observed == expected_sources:
            break
        time.sleep(1)
    else:
        raise SuiteError('Spark persisted lineage did not converge')
    reuse_definition = {'tasks': [{'id': 'load_result', 'operator': 'load', 'depends_on': [],
                                   'params': {'locator': target_locator, 'source_type': 'table'}}]}
    reuse_id, reused = submit_and_wait(client, reuse_definition, report['runtime_id'], report['cluster_id'], deadline)
    reused_final = reused['metadata']['result']['final_result']
    if reused_final.get('type') != 'spark_dataframe' or sorted(reused_final.get('preview_rows', []), key=lambda row: row['region']) != expected_rows:
        raise SuiteError('Downstream Spark could not reuse the stable table output')
    reuse_runtime = SPARK.runtime_status_evidence(support.required_environment('ADDP_ONLINE_SPARK_RUNTIME_URL'),
        reused['metadata']['result']['runtime_execution_id'], reused_final, 1)
    return {'engine_id': target_engine_id, 'execution_id': execution_id, 'target_locator': target_locator,
            'preview_locator': preview_locator, 'item_id': item['id'], 'scan_execution_id': scan_id,
            'final_result': final, 'runtime_status': runtime, 'physical': persistence_physical(),
            'lineage_sources': sorted(observed), 'reuse_execution_id': reuse_id, 'reuse_final_result': reused_final,
            'reuse_runtime_status': reuse_runtime}


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
                'meta_ui_scan': True, 'previews': 5, 'develop_result': True,
                'persist_execution_id': report['persistence']['execution_id'],
                'reuse_execution_id': report['persistence']['reuse_execution_id']}
    if evidence != expected:
        raise SuiteError('HDFS browser identity or execution evidence differs from API acceptance')
    for name in ('meta', 'csv', 'json', 'parquet', 'original', 'workflow', 'persisted', 'save', 'reuse'):
        if not (artifacts / ('hdfs-' + name + '-console.png')).is_file():
            raise SuiteError('HDFS Console screenshot evidence is missing')
    return evidence


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--initialize-table-read', action='store_true')
    arguments = parser.parse_args()
    if arguments.initialize_table_read:
        REGISTRATION.require_external_environment(os.environ)
        if os.environ.get('ADDP_ONLINE_HOSTED') != '1':
            raise SuiteError('HDFS source Grant preparation requires the disposable Hosted profile')
        require = support.required_environment
        tenant_id = support.positive_int(require('ADDP_ONLINE_TEST_TENANT_ID'), 'Tenant')
        consumer = support.GatewayClient(require('GATEWAY_URL'), require('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), 30)
        validate_identity(consumer, tenant_id)
        authorizer = support.GatewayClient(require('GATEWAY_URL'), require('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'), 30)
        REGISTRATION.initialize_exact_table_read_grants(authorizer, consumer, tenant_id, [
            (support.positive_int(require('ADDP_ONLINE_POSTGRES_ENGINE_ID'), 'PostgreSQL'), 'schema', 'results', 'hdfs_totals')])
        print('Disposable PostgreSQL result exact table read Grant is ready')
        return 0
    if os.environ.get('ADDP_ONLINE_TEST') != '1' or os.environ.get('ADDP_ONLINE_HOSTED') != '1':
        raise SuiteError('HDFS suite requires the Hosted Online entry')
    require = support.required_environment
    timeout = float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '900'))
    client = support.GatewayClient(require('GATEWAY_URL'), require('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), min(timeout, 30))
    report = run(client, support.positive_int(require('ADDP_ONLINE_TEST_TENANT_ID'), 'tenant'),
                 support.positive_int(require('ADDP_ONLINE_HDFS_ENGINE_ID'), 'HDFS'),
                 support.positive_int(require('ADDP_ONLINE_SPARK_ENGINE_ID'), 'Spark'), timeout)
    report['persistence'] = run_persistence(client, report, support.positive_int(require('ADDP_ONLINE_POSTGRES_ENGINE_ID'), 'PostgreSQL'), timeout)
    report['worker'] = worker_evidence(report['cluster_id'])
    report['browser'] = run_browser(report)
    Path(require('ADDP_ONLINE_ARTIFACT_DIR'), 'hdfs-spark-consumer-flow.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (SuiteError, REGISTRATION.RegistrationError, OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
