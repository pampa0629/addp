#!/usr/bin/env python3
"""HighGo native facts and PG snapshot replace through real owner APIs and Console."""
from __future__ import annotations

from importlib import import_module
import json
import os
from pathlib import Path
import subprocess
import sys
import time
from urllib.parse import urlencode

API = import_module('scripts.utils.online-api')
SUPPORT = import_module('scripts.test.security-transfer-protection-online')
FLOW = import_module('scripts.test.relational-consumer-flow-online')
REGISTRATION = import_module('scripts.test.online-engine-registration')
NAMESPACE = 'addp_business'
TARGET = 'orders_replace_online'
PERMISSIONS = {
    'system.engine_catalog.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read',
    'manager.data_item.read', 'manager.content.read', 'transfer.task.create', 'transfer.task.delete',
    'transfer.task.execute', 'transfer.task.read', 'system.execution_authorization.create',
}
TABLES = {'customers': (3, 6), 'products': (3, 7), 'orders': (3, 7), 'order_items': (4, 5)}
ORDER_FIELDS = ['id', 'order_no', 'customer_id', 'order_status', 'total_amount', 'ordered_at', 'updated_at']


def require(ok, message):
    if not ok:
        raise API.SuiteError(message)


def expected_facts(table):
    constraints = [{'name': table + '_pkey', 'constraint_type': 'primary_key', 'fields': ['id']}]
    unique = {'customers': ['customer_code'], 'products': ['sku'], 'orders': ['order_no'],
              'order_items': ['order_id', 'product_id']}[table]
    constraints.append({'name': table + '_' + '_'.join(unique) + '_key', 'constraint_type': 'unique', 'fields': unique})
    for field, reference in {'orders': [('customer_id', 'customers')],
                             'order_items': [('order_id', 'orders'), ('product_id', 'products')]}.get(table, []):
        constraints.append({'name': table + '_' + field + '_fkey', 'constraint_type': 'foreign_key',
                            'fields': [field], 'referenced_fields': ['id'],
                            'referenced_namespace': NAMESPACE, 'referenced_table': reference})
    indexes = [{'name': row['name'], 'fields': row['fields'], 'index_type': 'btree', 'is_unique': True}
               for row in constraints if row['constraint_type'] != 'foreign_key']
    if table == 'orders':
        indexes += [{'name': 'idx_highgo_orders_customer_id', 'fields': ['customer_id'], 'index_type': 'btree', 'is_unique': False},
                    {'name': 'idx_highgo_orders_updated_at', 'fields': ['updated_at', 'id'], 'index_type': 'btree', 'is_unique': False}]
    return sorted(constraints, key=lambda r: r['name']), sorted(indexes, key=lambda r: r['name'])


def validate_item(item, table):
    require(item.get('full_name') == NAMESPACE + '.' + table and item.get('item_type') == 'table', 'persisted DataItem identity mismatch')
    SUPPORT.positive_int(item.get('id'), 'DataItem')
    SUPPORT.positive_int(item.get('node_id'), 'namespace node')
    attrs = item.get('attributes', {})
    caps = attrs.get('capabilities', {})
    constraints, indexes = expected_facts(table)
    require(caps.get('constraints', {}).get('constraints') == constraints, table + ' named constraints mismatch')
    require(caps.get('indexing', {}).get('indexes') == indexes, table + ' ordered indexes mismatch')
    require('partitioning' not in caps, 'ordinary Business tables must not invent partition facts')
    info = attrs.get('type_info', {}).get('table', {})
    fields = info.get('fields', [])
    require(info.get('primary_key') == ['id'] and [f.get('name') for f in fields if f.get('primary_key')] == ['id'], 'primary key projection mismatch')
    require((info.get('row_count'), len(fields)) == TABLES[table], table + ' schema or row count mismatch')
    return {'constraints': len(constraints), 'indexes': len(indexes), 'rows': TABLES[table][0], 'fields': len(fields)}


def items(client, engine):
    rows = client.request('GET', f'/api/v1/meta/engines/{engine}/items?branch={NAMESPACE}', (200,)).payload
    require(isinstance(rows, list), 'Meta item list required')
    result = {}
    for row in rows:
        name = row.get('full_name', '').removeprefix(NAMESPACE + '.')
        require(name not in result, 'duplicate persisted DataItem identity')
        result[name] = row
    return result


def preview(client, engine, item, table):
    target = SUPPORT.build_item_locator(engine, item)
    payload = client.request('GET', '/api/v1/manager/preview?' + urlencode({'locator': target, 'page': 1, 'page_size': 20}), (200,)).payload
    data = payload.get('data', {})
    expected_count, expected_fields = TABLES.get(table, (3, 7))
    require(payload.get('preview_type') == 'table' and data.get('total') == expected_count
            and len(data.get('columns', [])) == expected_fields and len(data.get('rows', [])) == expected_count,
            'Manager preview shape mismatch: ' + table)
    return data


def replace_task(name, source, target_engine, target_item):
    fields = [{'source': f, 'target': f, 'target_type': 'bigint' if f in ('id', 'customer_id') else
               'decimal' if f == 'total_amount' else 'timestamp' if f.endswith('_at') else 'string', 'nullable': False}
              for f in ORDER_FIELDS]
    fields[4].update(precision=14, scale=2)
    return {'name': name, 'task_type': 'sync', 'enabled': False, 'schedule': '', 'auto_scan_metadata': True,
            'config': {'runtime': {'boundary': 'bounded'}, 'load': {'mode': 'snapshot'},
                'source': {'locator': source, 'data_type': 'table', 'representation': 'native'},
                'target': {'parent_locator': f'addp://engine/{target_engine}/path/{NAMESPACE}?' + urlencode({'type': 'schema', 'node_id': target_item['node_id']}),
                           'name': TARGET, 'data_type': 'table', 'representation': 'native', 'policy': {'apply_mode': 'replace'}},
                'transforms': [{'type': 'field_mapping', 'version': 'v1', 'mode': 'project', 'fields': fields}], 'batch_size': 100}}


def run_browser(report):
    directory = Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'))
    evidence = directory / 'highgo-console.json'
    evidence.unlink(missing_ok=True)
    env = dict(os.environ, ADDP_ONLINE_HIGHGO_EXPECTATIONS=json.dumps(report), ADDP_ONLINE_HIGHGO_BROWSER_REPORT=str(evidence))
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/highgo-consumer-flow.spec.js',
                             '--config=playwright.online.config.js'], cwd=Path(__file__).resolve().parents[2] / 'console/frontend', env=env)
    require(result.returncode == 0 and evidence.is_file(), 'Console HighGo preview acceptance failed or omitted evidence')
    result = json.loads(evidence.read_text())
    require(result == {'run_id': report['run_id'], 'tenant_id': report['tenant_id'], 'principal_id': report['principal_id'],
                       'engine_id': report['engine_id'], 'tables': [*TABLES, TARGET]}, 'browser identity or table evidence mismatch')
    return result


def run(client, tenant, highgo, postgres, timeout):
    require(tenant > 1 and highgo != postgres, 'dedicated Tenant and distinct source/target Engines required')
    identity = API.validate_user_identity(client, tenant, PERMISSIONS)
    context = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    actual = {p for r in context['authorization']['role_assignments'] for p in r['permissions']}
    require(actual == PERMISSIONS, 'consumer has unexpected extra permissions')
    selectors = client.request('GET', '/api/v1/system/engine-catalog/engines', (200,)).payload
    engines = {r['id']: r for r in selectors}
    require(engines.get(highgo, {}).get('engine_type') == 'highgo' and engines.get(postgres, {}).get('engine_type') == 'postgresql', 'engine registration types changed')
    storage = engines[highgo].get('capabilities', {}).get('storage', {})
    require(all(storage.get('facts', {}).get(k) is True for k in ('indexes', 'constraints', 'partitioning'))
            and storage.get('store', {}).get('delete') is True, 'certified HighGo facts and replace capabilities missing')
    require('connection_info' not in engines[highgo], 'consumer catalog leaked credentials')
    deadline = time.monotonic() + timeout
    scans = [SUPPORT.wait_for_scan(client, e, deadline) for e in (highgo, postgres)]
    hg, pg = items(client, highgo), items(client, postgres)
    require(set(hg) == {*TABLES, 'addp_engine_probe', TARGET}, 'HighGo scan included unexpected system objects or omitted samples')
    samples = {t: dict(validate_item(hg[t], t), locator=SUPPORT.build_item_locator(highgo, hg[t])) for t in TABLES}
    initializer = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'), 30)
    REGISTRATION.initialize_exact_record_read_grants(initializer, client, tenant,
        [(postgres, 'schema', NAMESPACE, 'orders')])
    for table in TABLES:
        preview(client, highgo, hg[table], table)
    source = preview(client, postgres, pg['orders'], 'orders')
    require(source['columns'] == ORDER_FIELDS, 'source order fields mismatch')
    tasks, executions = [], []
    run_id = SUPPORT.required_environment('ADDP_ONLINE_TEST_RUN_ID')
    try:
        task = FLOW.create_task(client, replace_task('addp_online_highgo_' + run_id, SUPPORT.build_item_locator(postgres, pg['orders']), highgo, hg[TARGET]), tasks)
        for phase in ('replace_sentinel', 'repeat_replace'):
            executions.append(FLOW.assert_transfer_counts(FLOW.run_task(client, task, deadline), 3, phase))
            scans.append(SUPPORT.wait_for_scan(client, highgo, deadline))
            current = items(client, highgo)[TARGET]
            data = preview(client, highgo, current, TARGET)
            require(data['columns'] == source['columns'] and sorted(data['rows'], key=lambda r: int(r['id'])) == sorted(source['rows'], key=lambda r: int(r['id'])), 'seven-field target data differs after ' + phase)
        for table in TABLES:
            validate_item(items(client, highgo)[table], table)
        report = {'suite': 'highgo-consumer-flow', 'run_id': run_id, 'tenant_id': tenant,
                  'principal_id': str(context['principal']['id']), 'engine_id': highgo,
                  'identity': identity, 'samples': samples, 'target_locator': SUPPORT.build_item_locator(highgo, current),
                  'executions': executions, 'scans': scans, 'constraints': 11, 'indexes': 10}
        report['browser'] = run_browser(report)
        return report
    finally:
        SUPPORT.cleanup_tasks(client, tasks)


def main():
    require(os.environ.get('ADDP_ONLINE_HOSTED') == '1' and os.environ.get('ADDP_ONLINE_TEST') == '1', 'Hosted Online entry required')
    tenant = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_TEST_TENANT_ID'), 'Tenant')
    hg = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_HIGHGO_ENGINE_ID'), 'HighGo')
    pg = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_HIGHGO_PG_ENGINE_ID'), 'PostgreSQL')
    timeout = float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '900'))
    client = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), 30)
    report = run(client, tenant, hg, pg, timeout)
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'), 'highgo-consumer-flow.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (RuntimeError, OSError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
