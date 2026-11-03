#!/usr/bin/env python3
"""HDFS System -> Meta -> Manager -> formal Develop/Spark -> real Console."""
from __future__ import annotations
import argparse
import importlib.util
import json
import math
import stat
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.parse
import uuid
import urllib.request
from types import SimpleNamespace

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
spec = importlib.util.spec_from_file_location('spark_table_online', Path(__file__).with_name('spark-table-consumer-online.py'))
TABLES = importlib.util.module_from_spec(spec)
spec.loader.exec_module(TABLES)
PERMISSIONS = {'system.engine_catalog.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read',
               'manager.data_item.read', 'manager.content.read', 'develop.task.read', 'develop.task.execute',
               'develop.data_read.execute', 'develop.data_write.execute', 'develop.data_ddl.execute',
               'meta.lineage.read', 'system.execution_authorization.create', 'develop.task.create', 'develop.task.update'}
FILES = {'csv': 'samples/orders.csv', 'json': 'samples/orders.json', 'parquet': 'samples/orders.parquet',
         'original': '订单 100%.csv'}


def table_host():
    return SimpleNamespace(support=support, SPARK=SPARK, submit_and_wait=submit_and_wait)


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


def worker_evidence(engine_id, before):
    try:
        return SPARK.worker_evidence(engine_id, 'hdfs-spark-consumer-flow', 'addp-hdfs-online-worker', 'http://127.0.0.1:18080', before)
    except ValueError as error:
        raise SuiteError(str(error)) from error


def run(client, tenant_id, engine_id, cluster_id, timeout, source_scan_id, expected_runtime_id, physical=worker_evidence):
    if engine_id == cluster_id:
        raise SuiteError('HDFS source and Spark cluster must be distinct Engines')
    principal = validate_identity(client, tenant_id)
    engines = support._array(client.request('GET', '/api/v1/system/engine-catalog/engines', (200,)).payload, 'catalog engines')
    matches = [engine for engine in engines if engine.get('id') == engine_id and engine.get('engine_type') == 'hdfs']
    if len(matches) != 1:
        raise SuiteError('System catalog must expose the disposable HDFS source')
    clusters = support._array(client.request('GET', '/api/v1/develop/spark-runtimes', (200,)).payload, 'Spark cluster selectors')
    try:
        SPARK.require_compute_cluster(cluster_id, engines, clusters)
    except ValueError as error:
        raise SuiteError(str(error)) from error
    runtimes = support._array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'runtimes')
    selected = [runtime for runtime in runtimes if runtime.get('engine_type') == 'spark_workflow' and runtime.get('connection_status') == 'online']
    if len(selected) != 1:
        raise SuiteError('exactly one online self-registered Spark Workflow Runtime is required')
    runtime_id = support.positive_int(selected[0]['id'], 'Runtime')
    if runtime_id != expected_runtime_id:
        raise SuiteError('prepared Runtime identity changed before consumption')
    deadline = time.monotonic() + timeout
    scan = support._object(client.request('GET', '/api/v1/meta/executions/' + urllib.parse.quote(source_scan_id), (200,)).payload, 'prepared source scan')
    if scan.get('status') != 'success':
        raise SuiteError('prepared HDFS source scan is no longer successful')
    locators, item_ids = {}, {}
    for name, filename in FILES.items():
        item = support.find_item(client, engine_id, filename, 'file')
        locators[name], item_ids[name] = locator(engine_id, item), item['id']
        _, rows = support.preview_rows(client, locators[name])
        validate_rows(rows)
    definition = workflow(locators)
    before = SPARK.application_ids(SPARK.application_snapshot('http://127.0.0.1:18080'))
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
            'item_ids': item_ids, 'scan_execution_id': source_scan_id, 'execution_id': execution_id,
            'runtime_execution_id': execution['metadata']['result']['runtime_execution_id'],
            'final_result': final, 'worker': physical(cluster_id, before),
            'runtime_status': runtime_status_evidence(execution['metadata']['result']['runtime_execution_id'], final)}


def persistence_workflow(locators, target_engine_id, target_type="table"):
    definition = workflow(locators)
    definition['tasks'].append({'id': 'save', 'operator': 'save', 'depends_on': ['verify_all_formats'], 'params': {
        'input_df': {'$ref': 'verify_all_formats', 'port': 'default'},
        'target_type': 'table',
        'target_parent_locator': f'addp://engine/{target_engine_id}/path/results?type=schema',
        'target_name': 'hdfs_totals', 'mode': 'overwrite',
    }})
    if target_type == 'file':
        definition['tasks'][-1]['params'].update(target_type='file',
            target_parent_locator=f'addp://engine/{target_engine_id}/path/result?type=bucket',
            target_name='results/hdfs_totals', format='parquet', mode='create')
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


def validate_persistence(execution, locators, target_locator, target_type="table"):
    if execution.get('status') != 'success':
        raise SuiteError('Spark persistence execution did not succeed')
    metadata = support._object(execution.get('metadata'), 'persisted metadata')
    result = support._object(metadata.get('result'), 'persisted result')
    object_result = target_type == 'file'
    resource_type, write_mode = ('object', 'create') if object_result else ('table', 'replace')
    physical_target = ('s3a://result/' + urllib.parse.urlsplit(target_locator).path.split('/path/result/', 1)[1]) if object_result else 'results.hdfs_totals'
    resource = execution.get('outputs', {}).get('save', {}).get('resource')
    if resource != {'locator': target_locator, 'type': resource_type, 'write_mode': write_mode}:
        raise SuiteError('Spark stable output must bind the exact target and write mode')
    final = result.get('final_result')
    if final != {'status': 'success', 'rows': 2, 'target': physical_target}:
        raise SuiteError('Spark save must report exactly two committed rows')
    facts = metadata.get('lineage_facts', {})
    if (facts.get('schema_version') != 'addp.lineage-facts/v1'
        or {value.get('locator') for value in facts.get('inputs', [])} != {locators[name] for name in ('csv', 'json', 'parquet')}
        or len(facts.get('outputs', [])) != 1 or facts['outputs'][0].get('locator') != target_locator
        or facts['outputs'][0].get('write_mode') != write_mode):
        raise SuiteError('Spark save lineage must bind the exact three sources and result target')
    targets = result.get('produced_targets', [])
    if len(targets) != 1 or targets[0].get('locator') != target_locator or targets[0].get('task_id') != 'save':
        raise SuiteError('Spark save omitted its produced target')
    if object_result and targets[0].get('layout') != 'whole':
        raise SuiteError('MinIO result must declare the whole Parquet layout')
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


def publish_persistence(client, report, target_engine_id, timeout, target_type="table"):
    deadline = time.monotonic() + timeout
    if target_engine_id in (report['engine_id'], report['cluster_id'], report['runtime_id']):
        raise SuiteError('PostgreSQL target must be a distinct Engine')
    target_locator = f'addp://engine/{target_engine_id}/path/results/hdfs_totals?type=table'
    definition = persistence_workflow(report['locators'], target_engine_id, target_type)
    execution_id, execution = submit_and_wait(client, definition,
                                            report['runtime_id'], report['cluster_id'], deadline)
    if target_type == 'file':
        leaf = execution_id + '-' + str(uuid.uuid5(uuid.NAMESPACE_OID, 'save'))
        target_locator = f'addp://engine/{target_engine_id}/path/result/results/hdfs_totals/{leaf}?type=object'
        final, scan_id = validate_persistence(execution, report['locators'], target_locator, target_type)
    else:
        final, scan_id = validate_persistence(execution, report['locators'], target_locator)
    def validate_nodes(nodes):
        if set(nodes) != {task['id'] for task in definition['tasks']} or nodes['save'] != final:
            raise ValueError('persisted Runtime lost canonical node outputs')
        for name, value in nodes.items():
            if name != 'save' and value.get('type') != 'spark_dataframe':
                raise ValueError('persisted Runtime lost DataFrame summary')
    runtime = SPARK.runtime_status_evidence(support.required_environment('ADDP_ONLINE_SPARK_RUNTIME_URL'),
        execution['metadata']['result']['runtime_execution_id'], final, 9, validate_nodes=validate_nodes,
        node_types={task['id']: ('save' if task['id'] == 'save' else 'spark_dataframe')
                    for task in definition['tasks']})
    while time.monotonic() < deadline:
        scan = support._object(client.request('GET', '/api/v1/meta/executions/' + urllib.parse.quote(scan_id), (200,)).payload, 'automatic result scan')
        if scan.get('status') == 'success':
            break
        if scan.get('status') in support.TERMINAL_STATUSES:
            raise SuiteError('Spark result automatic scan failed')
        time.sleep(1)
    else:
        raise SuiteError('Spark result automatic scan did not converge')
    return {'execution_id': execution_id, 'target_locator': target_locator, 'scan_execution_id': scan_id,
            'runtime_execution_id': execution['metadata']['result']['runtime_execution_id'],
            'runtime_status': runtime, 'final_result': final, 'remaining_seconds': deadline - time.monotonic()}


def publication_item_path(publication, engine, target_type):
    execution_id = publication.get('execution_id')
    if not isinstance(execution_id, str) or not execution_id:
        raise SuiteError('prepared publication execution identity is missing')
    item_path = ('result/results/hdfs_totals/' + execution_id + '-' + str(uuid.uuid5(uuid.NAMESPACE_OID, 'save'))
                 if target_type == 'file' else 'results/hdfs_totals')
    expected = f'addp://engine/{engine}/path/{item_path}?type=' + ('object' if target_type == 'file' else 'table')
    if publication.get('target_locator') != expected:
        raise SuiteError('prepared publication target differs from its original execution and Engine')
    return item_path if target_type == 'file' else 'results.hdfs_totals'


def consume_persistence(client, report, target_engine_id, publication, timeout, target_type="table"):
    item_path = publication_item_path(publication, target_engine_id, target_type)
    remaining = publication.get('remaining_seconds')
    if (isinstance(remaining, bool) or not isinstance(remaining, (int, float))
            or not math.isfinite(remaining) or not 0 < remaining <= timeout):
        raise SuiteError('prepared publication has no valid remaining original budget')
    deadline = time.monotonic() + remaining
    execution_id, target_locator = publication['execution_id'], publication['target_locator']
    scan_id, runtime, final = publication['scan_execution_id'], publication['runtime_status'], publication['final_result']
    execution = support._object(client.request('GET', '/api/v1/develop/executions/' + urllib.parse.quote(execution_id), (200,)).payload,
                                'original publication execution')
    current_final, current_scan = validate_persistence(execution, report['locators'], target_locator, target_type)
    if (execution.get('status') != 'success' or current_final != final or current_scan != scan_id
            or execution['metadata']['result']['runtime_execution_id'] != publication['runtime_execution_id']):
        raise SuiteError('prepared publication differs from the original User execution')
    item = support.find_item(client, target_engine_id, item_path, 'object' if target_type == 'file' else 'table')
    if target_type == 'file':
        facts = support._object(item.get('attributes', {}).get('item'), 'Meta result item facts')
        if (facts.get('data_type') != 'table' or facts.get('format') != 'parquet' or facts.get('layout') != 'whole'):
            raise SuiteError('Meta did not resolve MinIO result as table/parquet/whole')
    preview_locator = target_locator + '&item_id=' + str(item['id'])
    _, rows = support.preview_rows(client, preview_locator)
    expected_rows = sorted(report['final_result']['preview_rows'], key=lambda row: row['region'])
    # All six count/sum columns in this fixture are bigint. Manager's HTTP
    # boundary uses decimal strings; Runtime DataFrame summaries stay numeric.
    expected_preview_rows = [{key: value if key == 'region' else str(value)
                              for key, value in row.items()} for row in expected_rows]
    if sorted(rows, key=lambda row: row['region']) != expected_preview_rows:
        observed_types = [{key: type(value).__name__ for key, value in row.items()} for row in rows]
        raise SuiteError('Manager persisted result differs from the computed totals / bigint JSON contract: '
                         + json.dumps(observed_types, sort_keys=True))
    baseline = publication.get('manager_preview_before_second')
    if baseline is not None and sorted(rows, key=lambda row: row['region']) != sorted(baseline, key=lambda row: row['region']):
        raise SuiteError('Second publication changed the original first-result Manager baseline')
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
                                   'params': {'locator': target_locator, 'source_type': target_type}}]}
    reuse_id, reused = submit_and_wait(client, reuse_definition, report['runtime_id'], report['cluster_id'], deadline)
    reused_final = reused['metadata']['result']['final_result']
    if reused_final.get('type') != 'spark_dataframe' or sorted(reused_final.get('preview_rows', []), key=lambda row: row['region']) != expected_rows:
        raise SuiteError('Downstream Spark could not reuse the stable table output')
    reuse_runtime = SPARK.runtime_status_evidence(support.required_environment('ADDP_ONLINE_SPARK_RUNTIME_URL'),
        reused['metadata']['result']['runtime_execution_id'], reused_final, 1)
    evidence = {'engine_id': target_engine_id, 'execution_id': execution_id, 'target_locator': target_locator,
            'preview_locator': preview_locator, 'manager_preview_rows': rows, 'item_id': item['id'], 'scan_execution_id': scan_id,
            'final_result': final, 'runtime_status': runtime, 'physical': minio_physical(item_path) if target_type == 'file' else persistence_physical(),
            'lineage_sources': sorted(observed), 'reuse_execution_id': reuse_id, 'reuse_final_result': reused_final,
            'reuse_runtime_status': reuse_runtime}
    if baseline is not None:
        evidence['manager_preview_before_second'] = baseline
    return evidence


def validate_minio_independent_results(client, first, second):
    if first['target_locator'] == second['target_locator']:
        raise SuiteError('Independent executions reused the same MinIO result directory')
    _, old_rows = support.preview_rows(client, first['preview_locator'])
    # Recheck the first publication at the same Manager HTTP boundary. Its
    # independently verified bigint strings must remain exact after the second.
    if sorted(old_rows, key=lambda row: row['region']) != sorted(first['manager_preview_rows'], key=lambda row: row['region']):
        raise SuiteError('Second publication changed the first MinIO result')


def minio_physical(path):
    command = ['docker', 'exec', 'addp-hdfs-online-minio', 'mc', 'ls', '--recursive', '--json', 'owned/' + path + '/']
    objects = [json.loads(line) for line in subprocess.run(command, check=True, capture_output=True, text=True).stdout.splitlines() if line]
    keys = [value.get('key', '') for value in objects]
    if not keys or not any(key.endswith('_SUCCESS') for key in keys) or not any(key.endswith('.parquet') for key in keys):
        raise SuiteError('MinIO result has no committed Parquet objects')
    if any('__magic' in key or '.pending' in key or '_temporary' in key for key in keys):
        raise SuiteError('MinIO result contains uncommitted objects')
    marker = json.loads(subprocess.run(['docker', 'exec', 'addp-hdfs-online-minio', 'mc', 'cat', 'owned/' + path + '/_SUCCESS'],
        check=True, capture_output=True, text=True).stdout)
    if marker.get('committer') != 'magic':
        raise SuiteError('MinIO result omitted its Magic Committer marker')
    return {'committer': 'magic', 'objects': len(keys)}


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
                'meta_ui_scan': True, 'previews': 6, 'develop_result': True,
                'persist_execution_id': report['persistence']['execution_id'],
                'reuse_execution_id': report['persistence']['reuse_execution_id'],
                'minio_execution_id': report['minio']['execution_id'],
                'minio_reuse_execution_id': report['minio']['reuse_execution_id']}
    expected['tables'] = {engine: {name: facts[name]['execution_id'] for name in ('created', 'overwrite', 'reuse')}
                          for engine, facts in report['tables'].items()}
    if evidence != expected:
        raise SuiteError('HDFS browser identity or execution evidence differs from API acceptance')
    for name in ('meta', 'csv', 'json', 'parquet', 'original', 'workflow', 'persisted', 'save', 'reuse', 'minio', 'minio-save', 'minio-reuse'):
        if not (artifacts / ('hdfs-' + name + '-console.png')).is_file():
            raise SuiteError('HDFS Console screenshot evidence is missing')
    for engine in ('mysql', 'doris'):
        for phase in ('created', 'overwrite', 'reuse', 'preview'):
            if not (artifacts / f'hdfs-{engine}-{phase}-console.png').is_file():
                raise SuiteError('table Console screenshot evidence is missing')
    return evidence


def multicluster_expectations(client, report):
    cluster_b = support.positive_int(support.required_environment('ADDP_ONLINE_SPARK_B_ENGINE_ID'), 'Spark B')
    if cluster_b == report['cluster_id']:
        raise SuiteError('two distinct registered compute clusters are required')
    catalog = support._array(client.request('GET', '/api/v1/system/engine-catalog/engines', (200,)).payload, 'catalog')
    clusters = support._array(client.request('GET', '/api/v1/develop/spark-runtimes', (200,)).payload, 'clusters')
    selected = {}
    for key, identifier, port, worker in [('a', report['cluster_id'], 18080, 'addp-hdfs-online-worker'),
                                          ('b', cluster_b, 18082, 'addp-hdfs-online-worker-b')]:
        cluster = SPARK.require_compute_cluster(identifier, catalog, clusters)
        selected[key] = {'id': identifier, 'name': cluster['name'], 'master_url': f'http://127.0.0.1:{port}', 'worker': worker}
    runtimes = support._array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'runtimes')
    matches = [item for item in runtimes if item['id'] == report['runtime_id'] and item['engine_type'] == 'spark_workflow']
    if len(matches) != 1:
        raise SuiteError('same built-in Runtime must serve both clusters')
    # Fixed synthetic source name belongs to this fixture, never a user's table.
    source = next(item for item in catalog if item['id'] == int(os.environ['ADDP_ONLINE_POSTGRES_ENGINE_ID']))
    if source['name'] != 'Hosted PostgreSQL':
        raise SuiteError('unexpected disposable PostgreSQL source identity')
    table = '`Hosted_PostgreSQL`.`results`.`hdfs_totals`'
    return {'clusters': selected, 'runtime_name': matches[0]['name'], 'permissions': sorted(PERMISSIONS),
            'query': f'SELECT region, CAST(csv_rows AS STRING) AS rows FROM {table} ORDER BY region',
            'failure_query': f'SELECT p65_missing_column FROM {table}',
            'query_rows': [{'region': 'east', 'rows': '10'}, {'region': 'west', 'rows': '10'}],
            'workflow_query': "SELECT 'dual-workflow' AS marker, count(*) AS rows FROM range(0,4,1,2)"}


def validate_multicluster_receipt(receipt, report):
    if (receipt.get('run_id') != support.required_environment('ADDP_ONLINE_TEST_RUN_ID')
            or receipt.get('tenant_id') != report['tenant_id'] or receipt.get('principal_id') != report['principal_id']
            or receipt.get('runtime_id') != report['runtime_id'] or receipt.get('manager_no_spark') is not True):
        raise SuiteError('dual-cluster receipt identity differs from the ordinary browser User')
    tasks = receipt.get('tasks', {})
    if set(tasks) != {'a', 'b'} or any(not isinstance(value, int) or isinstance(value, bool) or value <= 0 for value in tasks.values()) or tasks['a'] == tasks['b']:
        raise SuiteError('two independently saved workflow definitions are required')
    observations = receipt.get('observations', [])
    if len(observations) != 12:
        raise SuiteError('both page kinds require A/B/A, concurrency, failure isolation and recovery')
    seen = {'a': set(), 'b': set()}
    execution_ids = set()
    for index, observation in enumerate(observations):
        kind = 'workflow' if index < 6 else 'query'
        step = index % 6
        expected_pairs = [[('a', 'success')], [('b', 'success')], [('a', 'success')],
                          [('a', 'success'), ('b', 'success')], [('a', 'failed'), ('b', 'success')], [('a', 'success')]][step]
        operations = observation.get('operations', [])
        if (observation.get('kind') != kind or [(op.get('cluster'), op.get('status')) for op in operations] != expected_pairs
                or (step in (3, 4) and observation.get('overlap') is not True)):
            raise SuiteError('dual-cluster sequence, overlap or failure isolation evidence is incomplete')
        phase = f'sequence-{index}' if step < 3 else {3: 'concurrent', 4: 'failure-isolation', 5: 'recovery'}[step]
        if observation.get('phase') != phase:
            raise SuiteError('dual-cluster phase is missing or reordered')
        for key in ('a', 'b'):
            apps = observation.get('applications', {}).get(key, [])
            selected = [op for op in operations if op['cluster'] == key]
            if len(apps) != len(selected) or len(set(observation.get('before', {}).get(key, []))) != len(observation.get('before', {}).get(key, [])):
                raise SuiteError('unexpected Master Application count')
            for app, operation in zip(apps, selected):
                cluster = report['multicluster']['clusters'][key]
                expected_name = f"ADDP-Workflow-Engine-{cluster['id']}" if kind == 'workflow' else 'ADDP-federated-query'
                app_id = app.get('id')
                if (not isinstance(app_id, str) or app_id in seen[key] or app_id in observation['before'][key]
                        or app.get('name') != expected_name or app.get('state') != 'FINISHED'):
                    raise SuiteError('Application identity reused, unfinished or routed to the wrong Master')
                seen[key].add(app_id)
                if kind == 'workflow' and operation.get('task_id') != tasks[key]:
                    raise SuiteError('workflow page executed a different saved task')
                if kind == 'query' and operation.get('query') != report['multicluster']['failure_query' if operation['status'] == 'failed' else 'query']:
                    raise SuiteError('query page source or SQL differs from the fixture')
                identifier = operation.get('execution_id')
                if not isinstance(identifier, str) or not identifier or identifier in execution_ids:
                    raise SuiteError('page execution IDs must be distinct')
                execution_ids.add(identifier)
    return seen


def run_multicluster_browser(client, report):
    report['multicluster'] = multicluster_expectations(client, report)
    listeners = SPARK.loopback_listener_evidence([(False, Path('/proc/net/tcp').read_text()),
        (True, Path('/proc/net/tcp6').read_text())], (7077, 18080, 18081, 7078, 18082, 18084))
    artifacts = Path(support.required_environment('ADDP_ONLINE_ARTIFACT_DIR'))
    output = artifacts / 'spark-dual-console.json'
    output.unlink(missing_ok=True)
    environment = dict(os.environ, ADDP_ONLINE_HDFS_EXPECTATIONS=json.dumps(report),
                       ADDP_ONLINE_SPARK_DUAL_BROWSER_REPORT=str(output))
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/spark-multicluster.spec.js',
                             '--config=playwright.online.config.js'], cwd=Path(__file__).resolve().parents[2] / 'console/frontend', env=environment)
    if result.returncode or not output.is_file():
        raise SuiteError('dual-cluster Console acceptance failed or omitted its receipt')
    receipt = json.loads(output.read_text())
    seen = validate_multicluster_receipt(receipt, report)
    physical = []
    for key, cluster in report['multicluster']['clusters'].items():
        master = SPARK.application_snapshot(cluster['master_url'])
        apps = {app['id']: app for app in master.get('completedapps', [])}
        if master.get('activeapps') or not seen[key].issubset(apps):
            raise SuiteError('dual-cluster Driver cleanup or Master receipts are incomplete')
        for observation in receipt['observations']:
            for app, operation in zip(observation['applications'][key], [op for op in observation['operations'] if op['cluster'] == key]):
                if apps[app['id']]['name'] != app['name'] or apps[app['id']]['state'] != 'FINISHED':
                    raise SuiteError('archived page Application differs from the live Master')
                evidence = SPARK.worker_application_evidence(app['id'], 'hdfs-spark-consumer-flow', cluster['worker'], operation['status'] == 'success')
                physical.append(dict(evidence, cluster=key, kind=observation['kind'], phase=observation['phase']))
                if operation.get('execution_id'):
                    execution = support._object(client.request('GET', '/api/v1/develop/executions/' + operation['execution_id'], (200,)).payload, 'page execution')
                    if execution.get('status') != operation['status']:
                        raise SuiteError('page execution status differs from the formal owner')
                    config = execution.get('execution_config', {})
                    if observation['kind'] == 'workflow':
                        if (execution.get('source_task_id') != str(receipt['tasks'][key]) or config.get('engine_id') != report['runtime_id']
                                or config.get('engine_specific', {}).get('spark_cluster_id') != cluster['id']):
                            raise SuiteError('formal workflow target differs from saved page selection')
                    elif config.get('engine_id') != cluster['id']:
                        raise SuiteError('formal query target differs from page selection')
    evidence = {'browser': receipt, 'physical': physical, 'active_applications': 0, 'loopback_listeners': listeners,
                'runtime_cleanup': SPARK.runtime_cleanup_evidence('hdfs-spark-consumer-flow', 'addp-hdfs-online-runtime')}
    (artifacts / 'spark-dual-physical.json').write_text(json.dumps(evidence, indent=2, ensure_ascii=False))
    return evidence


def write_private(name, value, *, new=False):
    path = Path(support.required_environment('ADDP_ONLINE_SECRET_DIR'), name)
    if new and path.exists():
        raise SuiteError('preparation checkpoint must be new')
    temporary = path.with_name(path.name + '.' + uuid.uuid4().hex + '.tmp')
    try:
        with os.fdopen(os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
            json.dump(value, stream)
            stream.flush()
            os.fsync(stream.fileno())
        if new:
            os.link(temporary, path)
            temporary.unlink()
        else:
            os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def read_private(name):
    path = Path(support.required_environment('ADDP_ONLINE_SECRET_DIR'), name)
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'r') as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600 or info.st_uid != os.getuid():
            raise SuiteError('preparation checkpoint must be an owner-only regular file')
        value = json.load(stream)
    if not isinstance(value, dict):
        raise SuiteError('preparation checkpoint must be an object')
    return value


def preparation_identity(client):
    require = support.required_environment
    tenant = support.positive_int(require('ADDP_ONLINE_TEST_TENANT_ID'), 'Tenant')
    return {'run_id': require('ADDP_ONLINE_TEST_RUN_ID'), 'tenant_id': tenant,
            'principal_id': validate_identity(client, tenant),
            'engine_id': support.positive_int(require('ADDP_ONLINE_HDFS_ENGINE_ID'), 'HDFS'),
            'cluster_id': support.positive_int(require('ADDP_ONLINE_SPARK_ENGINE_ID'), 'Spark'),
            'minio_engine_id': support.positive_int(require('ADDP_ONLINE_MINIO_ENGINE_ID'), 'MinIO')}


def validate_metadata_journal(journal, identity):
    if (journal.get('schema_version') != 'addp.hdfs-metadata-grants/v1'
            or any(journal.get(key) != value for key, value in identity.items())):
        raise SuiteError('metadata Grant checkpoint belongs to another run or identity')
    rows = journal.get('pending')
    issued = journal.get('issued_count')
    if (not isinstance(rows, list) or type(issued) is not int or not len(rows) <= issued <= 6):
        raise SuiteError('metadata Grant checkpoint exceeds the six approved targets')
    sources = [REGISTRATION.ExactFileMetadataGrants.target(identity['engine_id'], name, 'hdfs') for name in FILES.values()]
    requests, targets = set(), set()
    counts = {'hdfs': 0, 'minio': 0}
    try:
        for row in rows:
            if set(row) != {'engine', 'request_id', 'target', 'confirmed'} or type(row['confirmed']) is not bool:
                raise ValueError('invalid pending record')
            request = str(uuid.UUID(row['request_id']))
            if request != row['request_id'] or request in requests:
                raise ValueError('invalid request identity')
            requests.add(request)
            target = row['target']
            key = json.dumps(target, sort_keys=True)
            if key in targets:
                raise ValueError('duplicate target')
            targets.add(key)
            if row['engine'] == identity['engine_id'] and target in sources:
                counts['hdfs'] += 1
            elif row['engine'] == identity['minio_engine_id']:
                parts = [segment['name'] for segment in target['segments'][1:]]
                if len(parts) != 4 or parts[:3] != ['result', 'results', 'hdfs_totals']:
                    raise ValueError('not a run result')
                execution_id = parts[-1][:-37]
                if str(uuid.UUID(execution_id)) != execution_id or parts[-1] != execution_id + '-' + str(uuid.uuid5(uuid.NAMESPACE_OID, 'save')):
                    raise ValueError('not the original publication')
                expected = REGISTRATION.ExactFileMetadataGrants.target(row['engine'], '/'.join(parts), 'minio', layout='whole')
                if target != expected:
                    raise ValueError('not the whole DataItem root')
                counts['minio'] += 1
            else:
                raise ValueError('unapproved target')
        if counts['hdfs'] > 4 or counts['minio'] > 2:
            raise ValueError('target count differs')
    except (KeyError, ValueError, TypeError, AttributeError) as error:
        raise SuiteError('metadata Grant checkpoint contains an unapproved target') from error


def metadata_grants(authorizer, consumer, identity, journal):
    def persist(rows):
        journal['pending'] = rows
        journal['issued_count'] = max(journal['issued_count'], len(rows))
        validate_metadata_journal(journal, identity)
        write_private('hdfs-metadata-grants.json', journal)
    grants = REGISTRATION.ExactFileMetadataGrants(authorizer, consumer, identity['tenant_id'], on_change=persist)
    grants.pending = journal['pending']
    return grants


def prepare_file_metadata(consumer, authorizer, tenant, principal, runtime, cluster, timeout):
    identity = preparation_identity(consumer)
    if (identity['tenant_id'], identity['principal_id'], identity['cluster_id']) != (tenant, principal, cluster):
        raise SuiteError('file preparation identity differs from the verified User')
    journal = dict(identity, schema_version='addp.hdfs-metadata-grants/v1', pending=[], issued_count=0)
    write_private('hdfs-metadata-grants.json', journal, new=True)
    grants = metadata_grants(authorizer, consumer, identity, journal)
    deadline = time.monotonic() + timeout
    scan = support.wait_for_scan(consumer, identity['engine_id'], deadline)
    prepared = dict(identity, runtime_id=runtime, scan_execution_id=scan, locators={}, item_ids={})
    for key, name in FILES.items():
        grants.grant(identity['engine_id'], name, 'hdfs', data_type='table', format_name=name.rsplit('.', 1)[1], layout='single')
        item = support.find_item(consumer, identity['engine_id'], name, 'file')
        prepared['locators'][key], prepared['item_ids'][key] = locator(identity['engine_id'], item), item['id']
    prepared['source_remaining_seconds'] = deadline - time.monotonic()
    if prepared['source_remaining_seconds'] <= 0:
        raise SuiteError('source preparation exhausted the original HDFS budget')
    return prepared, grants


def cleanup_metadata_access():
    REGISTRATION.require_external_environment(os.environ)
    require = support.required_environment
    consumer = support.GatewayClient(require('GATEWAY_URL'), require('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), 30)
    authorizer = support.GatewayClient(require('GATEWAY_URL'), require('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'), 30)
    identity = preparation_identity(consumer)
    journal = read_private('hdfs-metadata-grants.json')
    validate_metadata_journal(journal, identity)
    result = metadata_grants(authorizer, consumer, identity, journal).cleanup()
    result['targets'] = journal['issued_count']
    Path(require('ADDP_ONLINE_ARTIFACT_DIR'), 'hdfs-metadata-cleanup.json').write_text(json.dumps(result))


def observe_first_minio(client):
    expected = preparation_identity(client)
    snapshot = read_private('hdfs-first-minio.json')
    journal = read_private('hdfs-metadata-grants.json')
    validate_metadata_journal(journal, expected)
    if (any(snapshot.get(key) != value for key, value in expected.items())
            or journal['issued_count'] != 5 or len(journal['pending']) != 5
            or not all(row['confirmed'] for row in journal['pending'])):
        raise SuiteError('first-result observation requires this User and exactly five prepared metadata Grants')
    publication = snapshot['publication']
    name = publication_item_path(publication, expected['minio_engine_id'], 'file')
    item = support.find_item(client, expected['minio_engine_id'], name, 'object')
    _, rows = support.preview_rows(client, publication['target_locator'] + '&item_id=' + str(item['id']))
    if len(rows) != 2 or {row.get('region') for row in rows} != {'east', 'west'}:
        raise SuiteError('first-result Manager baseline does not contain the original two regions')
    snapshot['manager_preview_before_second'] = rows
    write_private('hdfs-first-minio.json', snapshot)
    return 0


def capture_first_minio(consumer, publication):
    started = time.monotonic()
    identity = preparation_identity(consumer)
    write_private('hdfs-first-minio.json', dict(identity, publication=publication), new=True)
    environment = dict(os.environ)
    for key in ('ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'):
        environment.pop(key, None)
    remaining = publication['remaining_seconds'] - (time.monotonic() - started)
    if remaining <= 0:
        raise SuiteError('first-result baseline exhausted the original publication budget')
    subprocess.run([sys.executable, str(Path(__file__).resolve()), '--observe-first-minio'], env=environment,
                   check=True, timeout=remaining)
    snapshot = read_private('hdfs-first-minio.json')
    if any(snapshot.get(key) != value for key, value in identity.items()) or snapshot.get('publication') != publication:
        raise SuiteError('first-result baseline changed the publication or User identity')
    publication['manager_preview_before_second'] = snapshot['manager_preview_before_second']
    publication['remaining_seconds'] -= time.monotonic() - started
    if publication['remaining_seconds'] <= 0:
        raise SuiteError('first-result baseline exhausted the original publication budget')


def main():
    parser = argparse.ArgumentParser()
    actions = parser.add_mutually_exclusive_group()
    actions.add_argument('--prepare-data-access', action='store_true')
    actions.add_argument('--cleanup-data-access', action='store_true')
    actions.add_argument('--observe-first-minio', action='store_true')
    arguments = parser.parse_args()
    if arguments.cleanup_data_access:
        if os.environ.get('ADDP_ONLINE_HOSTED') != '1' or os.environ.get('GITHUB_ACTIONS') != 'true':
            raise SuiteError('table cleanup requires the disposable Hosted profile')
        try:
            cleanup_metadata_access()
        finally:
            TABLES.cleanup()
        return 0
    if arguments.prepare_data_access:
        REGISTRATION.require_external_environment(os.environ)
        if os.environ.get('ADDP_ONLINE_HOSTED') != '1':
            raise SuiteError('HDFS source Grant preparation requires the disposable Hosted profile')
        require = support.required_environment
        tenant_id = support.positive_int(require('ADDP_ONLINE_TEST_TENANT_ID'), 'Tenant')
        consumer = support.GatewayClient(require('GATEWAY_URL'), require('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), 30)
        principal = validate_identity(consumer, tenant_id)
        authorizer = support.GatewayClient(require('GATEWAY_URL'), require('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'), 30)
        runtimes = support._array(consumer.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'runtimes')
        selected = [runtime for runtime in runtimes if runtime.get('engine_type') == 'spark_workflow' and runtime.get('connection_status') == 'online']
        if len(selected) != 1:
            raise SuiteError('table preparation requires exactly one online Spark Runtime')
        runtime = support.positive_int(selected[0]['id'], 'Runtime')
        cluster = support.positive_int(require('ADDP_ONLINE_SPARK_ENGINE_ID'), 'Spark')
        timeout = float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '900'))
        prepared, metadata_grants = prepare_file_metadata(consumer, authorizer, tenant_id, principal, runtime, cluster, timeout)
        postgres_id = support.positive_int(require('ADDP_ONLINE_POSTGRES_ENGINE_ID'), 'PostgreSQL')
        support.wait_for_scan(consumer, postgres_id, time.monotonic() + 180)
        REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, tenant_id, [
            (postgres_id, 'schema', 'results', 'hdfs_totals')])
        def grant_created_table(engine_id, table):
            REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, tenant_id,
                [(engine_id, 'database', 'spark_results', table)])
        TABLES.prepare(table_host(), consumer, principal, tenant_id, runtime, cluster, 300, grant_created_table)
        publications = []
        for _ in range(2):
            publication = publish_persistence(consumer, prepared, prepared['minio_engine_id'], timeout, 'file')
            name = publication_item_path(publication, prepared['minio_engine_id'], 'file')
            grant_started = time.monotonic()
            metadata_grants.grant(prepared['minio_engine_id'], name, 'minio', data_type='table', format_name='parquet', layout='whole')
            publication['remaining_seconds'] -= time.monotonic() - grant_started
            if publication['remaining_seconds'] <= 0:
                raise SuiteError('publication preparation exhausted its original budget')
            if not publications:
                capture_first_minio(consumer, publication)
            publications.append(publication)
        prepared['minio_publications'] = publications
        write_private('hdfs-file-prepared.json', prepared, new=True)
        print('Disposable table reads and six exact file metadata Grants are ready')
        return 0
    if os.environ.get('ADDP_ONLINE_TEST') != '1' or os.environ.get('ADDP_ONLINE_HOSTED') != '1':
        raise SuiteError('HDFS suite requires the Hosted Online entry')
    if os.environ.get('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN') or os.environ.get('ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'):
        raise SuiteError('preparation credentials must be removed before ordinary consumption')
    require = support.required_environment
    timeout = float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '900'))
    client = support.GatewayClient(require('GATEWAY_URL'), require('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), min(timeout, 30))
    if arguments.observe_first_minio:
        return observe_first_minio(client)
    try:
        return run_scenario(client, timeout)
    finally:
        TABLES.cleanup()


def run_scenario(client, timeout):
    require = support.required_environment
    prepared = read_private('hdfs-file-prepared.json')
    expected = preparation_identity(client)
    if any(prepared.get(key) != value for key, value in expected.items()):
        raise SuiteError('file preparation belongs to another run, Tenant, User or Engine')
    source_budget = prepared.get('source_remaining_seconds')
    if (isinstance(source_budget, bool) or not isinstance(source_budget, (int, float))
            or not math.isfinite(source_budget) or not 0 < source_budget <= timeout):
        raise SuiteError('source preparation has no valid remaining original budget')
    journal = read_private('hdfs-metadata-grants.json')
    validate_metadata_journal(journal, expected)
    if journal['issued_count'] != 6 or len(journal['pending']) != 6 or not all(row['confirmed'] for row in journal['pending']):
        raise SuiteError('six confirmed metadata Grant receipts are required before consumption')
    publications = prepared.get('minio_publications')
    if not isinstance(publications, list) or len(publications) != 2:
        raise SuiteError('exactly two original MinIO publications are required')
    paths = [publication_item_path(publication, expected['minio_engine_id'], 'file') for publication in publications]
    if len(set(paths)) != 2:
        raise SuiteError('prepared MinIO publications must be independent')
    if not isinstance(publications[0].get('manager_preview_before_second'), list):
        raise SuiteError('first publication must retain its ordinary User baseline from before the second write')
    report = run(client, expected['tenant_id'], expected['engine_id'], expected['cluster_id'], source_budget,
                 prepared['scan_execution_id'], prepared['runtime_id'])
    if any(prepared.get(key) != report.get(key) for key in ('runtime_id', 'locators', 'item_ids')):
        raise SuiteError('prepared source identities or Runtime changed before consumption')
    postgres = support.positive_int(require('ADDP_ONLINE_POSTGRES_ENGINE_ID'), 'PostgreSQL')
    publication = publish_persistence(client, report, postgres, timeout)
    report['persistence'] = consume_persistence(client, report, postgres, publication, timeout)
    minio_id = support.positive_int(require('ADDP_ONLINE_MINIO_ENGINE_ID'), 'MinIO')
    report['minio'] = consume_persistence(client, report, minio_id, publications[0], timeout, 'file')
    report['minio_second'] = consume_persistence(client, report, minio_id, publications[1], timeout, 'file')
    report['metadata_preparation'] = {'action': 'metadata_read', 'hdfs_sources': 4, 'minio_publications': 2,
        'source_scan_execution_id': prepared['scan_execution_id'],
        'publication_execution_ids': [publication['execution_id'] for publication in publications]}
    validate_minio_independent_results(client, report['minio'], report['minio_second'])
    report['tables'] = TABLES.run(table_host(), client, report, timeout)
    Path(require('ADDP_ONLINE_ARTIFACT_DIR'), 'spark-table-consumer-flow.json').write_text(json.dumps(report['tables'], ensure_ascii=False, indent=2))
    report['browser'] = run_browser(report)
    report['dual_cluster'] = run_multicluster_browser(client, report)
    Path(require('ADDP_ONLINE_ARTIFACT_DIR'), 'hdfs-spark-consumer-flow.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (SuiteError, REGISTRATION.RegistrationError, OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
