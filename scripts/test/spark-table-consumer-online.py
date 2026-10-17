"""MySQL/Doris extension of the existing HDFS/Spark Hosted acceptance."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.parse

ROWS = [
    {'id': '-9007199254740993', 'label': None, 'value': '-100.123456789012345678'},
    {'id': '1', 'label': '', 'value': None},
    {'id': '2', 'label': '尾随😀  ', 'value': '0.000000000000000001'},
    {'id': '3', 'label': 'x' * 100, 'value': '999.987654321098765432'},
    {'id': '9007199254740993', 'label': '原始文本', 'value': '100.123456789012345678'},
]


def target_name():
    return 'spark_boundary_' + hashlib.sha256(os.environ['ADDP_ONLINE_TEST_RUN_ID'].encode()).hexdigest()[:16]


def physical(engine, table, cleanup=False):
    command = [sys.executable, 'business/scripts/online-spark-table-oracle.py', engine, table]
    if cleanup:
        command.append('--cleanup')
    return json.loads(subprocess.run(command, check=True, capture_output=True, text=True, timeout=180).stdout)


def validate_rows(rows, *, manager=False):
    if not isinstance(rows, list) or any(not isinstance(row, dict) or set(row) != {'id', 'label', 'value'} for row in rows):
        raise ValueError('exact boundary rows have unexpected fields')
    if manager and any(not isinstance(row['id'], str) for row in rows):
        raise ValueError('Manager bigint JSON must always be a decimal string')
    normalized = [dict(row, id=str(row['id'])) for row in rows]
    if sorted(normalized, key=lambda row: int(row['id'])) != ROWS:
        raise ValueError('bigint/Decimal/null/empty/trailing-space boundary changed')


def definition(engine_id, table):
    values = ["CAST(NULL AS STRING)", "''", "'尾随😀  '", "repeat('x',100)", "'原始文本'"]
    selects = []
    for row, label in zip(ROWS, values):
        decimal = 'NULL' if row['value'] is None else "'" + row['value'] + "'"
        selects.append(f"SELECT CAST({row['id']} AS BIGINT) AS id, {label} AS label, CAST({decimal} AS DECIMAL(38,18)) AS value")
    return {'tasks': [
        {'id': 'seed', 'operator': 'sql', 'depends_on': [], 'params': {'query': ' UNION ALL '.join(selects)}},
        {'id': 'save', 'operator': 'save', 'depends_on': ['seed'], 'params': {
            'input_df': {'$ref': 'seed', 'port': 'default'}, 'target_type': 'table',
            'target_parent_locator': f'addp://engine/{engine_id}/path/spark_results?type=database',
            'target_name': table, 'mode': 'overwrite'}},
    ]}


def wait_scan(host, client, execution, locator, deadline):
    result = execution['metadata']['result']
    runs = result.get('meta_scan_runs', [])
    if (len(runs) != 1 or runs[0].get('status') != 'submitted'
        or runs[0].get('target_locator') != locator or not runs[0].get('execution_id')):
        raise ValueError('table publication must submit its automatic Meta scan')
    identifier = runs[0]['execution_id']
    while time.monotonic() < deadline:
        scan = host.support._object(client.request('GET', '/api/v1/meta/executions/' + urllib.parse.quote(identifier), (200,)).payload, 'table automatic scan')
        if scan.get('status') == 'success':
            return identifier
        if scan.get('status') in host.support.TERMINAL_STATUSES:
            raise ValueError('table automatic Meta scan failed')
        time.sleep(1)
    raise ValueError('table automatic Meta scan timed out')


def save(host, client, runtime, cluster, engine, table, deadline):
    workflow = definition(engine, table)
    identifier, execution = host.submit_and_wait(client, workflow, runtime, cluster, deadline)
    locator = f'addp://engine/{engine}/path/spark_results/{table}?type=table'
    result = execution['metadata']['result']
    final = result.get('final_result')
    if final != {'status': 'success', 'rows': 5, 'target': 'spark_results.' + table}:
        raise ValueError('table publication omitted the exact five-row receipt')
    if execution.get('outputs', {}).get('save', {}).get('resource') != {'locator': locator, 'type': 'table', 'write_mode': 'replace'}:
        raise ValueError('table publication stable output differs')
    targets = result.get('produced_targets', [])
    if len(targets) != 1 or targets[0].get('locator') != locator or targets[0].get('task_id') != 'save':
        raise ValueError('table publication lost its produced target')
    def validate_nodes(nodes):
        if set(nodes) != {'seed', 'save'} or nodes['save'] != final:
            raise ValueError('table Runtime lost its save receipt')
        validate_rows(nodes['seed'].get('preview_rows'))
    runtime_evidence = host.SPARK.runtime_status_evidence(os.environ['ADDP_ONLINE_SPARK_RUNTIME_URL'],
        result['runtime_execution_id'], final, 2, validate_nodes, {'seed': 'spark_dataframe', 'save': 'save'})
    scan_id = wait_scan(host, client, execution, locator, deadline)
    item = host.support.find_item(client, engine, 'spark_results.' + table, 'table')
    fields = item.get('attributes', {}).get('type_info', {}).get('table', {}).get('fields', [])
    validate_fields(fields)
    return {'execution_id': identifier, 'runtime_status': runtime_evidence, 'scan_execution_id': scan_id,
            'target_locator': locator, 'preview_locator': locator + '&item_id=' + str(item['id']),
            'item_id': item['id'], 'fields': fields, 'final_result': final}


def validate_fields(fields):
    by_name = {field.get('name'): field for field in fields}
    if (set(by_name) != {'id', 'label', 'value'} or len(fields) != 3 or by_name['id'].get('type') != 'bigint'
        or by_name['label'].get('type') != 'string' or by_name['value'].get('type') != 'decimal'
        or by_name['value'].get('precision') != 38 or by_name['value'].get('scale') != 18):
        raise ValueError('Meta field types must retain bigint/string/Decimal(38,18)')


def cleanup():
    receipt, failures = {}, []
    for engine in ('mysql', 'doris'):
        try:
            receipt[engine] = physical(engine, target_name(), cleanup=True)
            if receipt[engine] != {'target_absent': True, 'staging_tables': 0}:
                raise ValueError('table cleanup receipt differs')
        except (OSError, ValueError, subprocess.SubprocessError):
            failures.append(engine)
    Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'], 'spark-table-cleanup.json').write_text(json.dumps(receipt))
    if failures:
        raise ValueError('table domain cleanup unverified: ' + ', '.join(failures))


def prepare(host, client, principal, tenant, runtime, cluster, timeout):
    prepared = {'run_id': os.environ['ADDP_ONLINE_TEST_RUN_ID'], 'principal_id': principal,
                'tenant_id': tenant, 'runtime_id': runtime, 'cluster_id': cluster, 'tables': {}}
    try:
        deadline = time.monotonic() + timeout
        for engine in ('mysql', 'doris'):
            engine_id = host.support.positive_int(os.environ['ADDP_ONLINE_' + engine.upper() + '_ENGINE_ID'], engine)
            created = save(host, client, runtime, cluster, engine_id, target_name(), deadline)
            native = physical(engine, target_name())
            validate_rows(native['rows'], manager=True)
            if native['staging_tables'] != 0:
                raise ValueError('initial table publication left staging tables')
            prepared['tables'][engine] = {'engine_id': engine_id, 'created': created, 'physical_before': native}
        path = Path(os.environ['ADDP_ONLINE_SECRET_DIR'], 'spark-table-prepared.json')
        with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as stream:
            json.dump(prepared, stream)
        return prepared
    except BaseException:
        cleanup()
        raise


def run(host, client, report, timeout):
    prepared = json.loads(Path(os.environ['ADDP_ONLINE_SECRET_DIR'], 'spark-table-prepared.json').read_text())
    if any(prepared.get(key) != value for key, value in {
        'run_id': os.environ['ADDP_ONLINE_TEST_RUN_ID'], 'principal_id': report['principal_id'],
        'tenant_id': report['tenant_id'], 'runtime_id': report['runtime_id'], 'cluster_id': report['cluster_id']}.items()):
        raise ValueError('table preparation belongs to another run or User')
    if set(prepared['tables']) != {'mysql', 'doris'}:
        raise ValueError('both table Engines are required')
    for engine, facts in prepared['tables'].items():
        deadline = time.monotonic() + timeout
        if facts['engine_id'] != int(os.environ['ADDP_ONLINE_' + engine.upper() + '_ENGINE_ID']):
            raise ValueError('prepared table Engine differs')
        facts['overwrite'] = save(host, client, report['runtime_id'], report['cluster_id'], facts['engine_id'], target_name(), deadline)
        if facts['overwrite']['item_id'] != facts['created']['item_id']:
            raise ValueError('overwrite changed the stable DataItem identity')
        native = physical(engine, target_name())
        validate_rows(native['rows'], manager=True)
        if native != facts['physical_before']:
            raise ValueError('overwrite changed exact rows, table identity, DDL or field types')
        facts['physical_after'] = native
        _, rows = host.support.preview_rows(client, facts['overwrite']['preview_locator'])
        validate_rows(rows, manager=True)
        reuse_definition = {'tasks': [{'id': 'load', 'operator': 'load', 'depends_on': [],
            'params': {'source_type': 'table', 'locator': facts['overwrite']['target_locator']}}]}
        reuse_id, execution = host.submit_and_wait(client, reuse_definition, report['runtime_id'], report['cluster_id'], deadline)
        final = execution['metadata']['result']['final_result']
        validate_rows(final.get('preview_rows'))
        if final.get('schema') != [{'name': 'id', 'type': 'bigint'}, {'name': 'label', 'type': 'string'}, {'name': 'value', 'type': 'decimal(38,18)'}]:
            raise ValueError('downstream Spark schema lost exact types')
        if execution.get('outputs') or execution['metadata']['result'].get('produced_targets') or execution['metadata']['result'].get('meta_scan_runs'):
            raise ValueError('read-only table reuse unexpectedly published data')
        facts['reuse'] = {'execution_id': reuse_id, 'final_result': final,
            'runtime_status': host.SPARK.runtime_status_evidence(os.environ['ADDP_ONLINE_SPARK_RUNTIME_URL'],
                execution['metadata']['result']['runtime_execution_id'], final, 1)}
    return prepared['tables']
