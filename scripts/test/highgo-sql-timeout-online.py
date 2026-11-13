#!/usr/bin/env python3
"""HighGo's isolated query/preview timeout case; no business-scene substitution."""
from importlib import import_module
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

API = import_module('scripts.utils.online-api')
SUPPORT = import_module('scripts.test.security-transfer-protection-online')
REGISTRATION = import_module('scripts.test.online-engine-registration')
FIXTURE = import_module('business.scripts.online-highgo-consumer-fixture')
PERMISSIONS = {'system.engine_catalog.read', 'meta.catalog.read', 'manager.data_item.read', 'manager.content.read',
               'develop.task.read', 'develop.task.execute', 'develop.data_read.execute', 'system.execution_authorization.create'}
PROBE = 'addp_engine_probe'
NAMESPACE = 'addp_business'
SCHEMA = 'addp.highgo-sql-timeout/v1'


def require(ok, message):
    if not ok:
        raise SUPPORT.SuiteError(message)


def client(key):
    return SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment(key), 30)


def prepare():
    private = FIXTURE.secret_directory()
    consumer, preparer = client('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), client('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN')
    tenant = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_TEST_TENANT_ID'), 'Tenant')
    engine = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_HIGHGO_ENGINE_ID'), 'HighGo engine')
    require(tenant > 1, 'disposable Tenant required')
    API.validate_user_identity(consumer, tenant, PERMISSIONS)
    REGISTRATION.source_grant_participants(preparer, consumer, tenant, scan_preparation=True)
    locator = f'addp://engine/{engine}/path/{NAMESPACE}/{PROBE}?type=table'
    scan = SUPPORT.wait_for_scan(preparer, engine, time.monotonic() + 120, scan_depth='basic', targets=[locator])

    def pending(engine_id, request_id, target):
        FIXTURE.write_private(private / 'highgo-timeout-grant.json', json.dumps(
            dict(engine_id=engine_id, request_id=request_id, target=target)))

    REGISTRATION.initialize_exact_record_read_grants(preparer, consumer, tenant,
        [(engine, 'schema', NAMESPACE, PROBE)], scan_preparation=True, on_pending=pending)
    item = SUPPORT.find_item(consumer, engine, NAMESPACE + '.' + PROBE, 'table')
    context = consumer.request('GET', '/api/v1/system/auth/context', (200,)).payload
    FIXTURE.write_private(private / 'highgo-timeout-expectations.json', json.dumps(dict(
        schema_version=SCHEMA, case='sql-timeout', run_id=SUPPORT.required_environment('ADDP_ONLINE_TEST_RUN_ID'),
        tenant_id=str(tenant), principal_id=context['principal']['id'], permissions=sorted(PERMISSIONS),
        engine_id=engine, engine_name='Hosted HighGo', locator=SUPPORT.build_item_locator(engine, item),
        scan_execution_id=scan)))


def cleanup():
    private = FIXTURE.secret_directory()
    grant = json.loads((private / 'highgo-timeout-grant.json').read_text())
    preparer, consumer = client('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'), client('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN')
    preparer.request('POST', f"/api/v1/system/engines/{grant['engine_id']}/access_grants/{grant['request_id']}/revoke",
                     (200, 404), {'reason': 'Disposable HighGo SQL timeout acceptance finished'})
    consumer.request('POST', '/api/v1/system/engine-access/read-observations/current-source', (403,), {'targets': [grant['target']]})
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'), 'highgo-timeout-grant-cleanup.json').write_text(
        json.dumps(dict(run_id=SUPPORT.required_environment('ADDP_ONLINE_TEST_RUN_ID'),
                        request_id=grant['request_id'], engine_id=grant['engine_id'], read_denied=True)))


def validate_browser(result, expected):
    require(isinstance(result, dict) and set(result) == {'schema_version', 'case', 'run_id', 'tenant_id',
        'principal_id', 'engine_id', 'locator', 'permissions', 'manager', 'develop'}, 'incomplete HighGo browser report')
    for key in ('schema_version', 'case', 'run_id', 'tenant_id', 'principal_id', 'engine_id', 'locator', 'permissions'):
        require(result.get(key) == expected.get(key), 'HighGo browser identity or scope mismatch: ' + key)
    for phase in ('manager', 'develop'):
        proof = result[phase]
        require(isinstance(proof, dict) and set(proof) == {'before', 'during', 'after_timeout', 'released',
            'elapsed_ms', 'code', 'heading', 'recovery_rows', 'execution_id', 'recovery_execution_id'},
            'incomplete HighGo timeout phase: ' + phase)
        require(proof['code'] == ('preview_sql_execution_timeout' if phase == 'manager' else 'sql_execution_timeout')
                and proof['heading'] == '数据库 SQL 执行超时。'
                and proof['recovery_rows'] == [{'id': 1, 'engine_name': 'HighGo V4.5 Business'}],
                'HighGo timeout classification or recovery mismatch: ' + phase)
        elapsed = proof['elapsed_ms']
        require(type(elapsed) in (int, float) and (45_000 <= elapsed <= 75_000 if phase == 'manager'
                else 100_000 <= elapsed <= 155_000), 'HighGo timeout did not follow the original budget')
        holder = None
        for key, stage, held in [('before', 'acquire', True), ('during', 'observe', True),
                                 ('after_timeout', 'observe', True), ('released', 'release', False)]:
            snapshot = proof[key]
            require(isinstance(snapshot, dict) and set(snapshot) == {'run_id', 'phase', 'stage', 'holder_id', 'held', 'waiters'}
                    and snapshot['run_id'] == expected['run_id'] and snapshot['phase'] == phase
                    and snapshot['stage'] == stage and snapshot['held'] is held
                    and type(snapshot['holder_id']) is int and snapshot['holder_id'] > 0,
                    'HighGo native phase identity mismatch')
            holder = snapshot['holder_id'] if holder is None else holder
            require(holder == snapshot['holder_id'], 'HighGo native holder changed')
            waiters = snapshot['waiters']
            require(isinstance(waiters, list) and len(waiters) == (1 if key == 'during' else 0),
                    'HighGo real waiter or source cancellation evidence missing')
            if waiters:
                row = waiters[0]
                require(isinstance(row, dict) and set(row) == {'id', 'state', 'wait_event_type', 'operation'}
                        and type(row['id']) is int and row['id'] > 0 and row['id'] != holder
                        and row['state'] == 'active' and row['wait_event_type'] == 'Lock'
                        and row['operation'] in ('table_catalog', 'table_read'), 'invalid native waiter')
        for key in ('execution_id', 'recovery_execution_id'):
            value = proof[key]
            if phase == 'manager':
                require(value is None, 'Manager preview must not invent an execution ID')
            else:
                try:
                    valid = isinstance(value, str) and str(uuid.UUID(value)) == value
                except ValueError:
                    valid = False
                require(valid, 'Develop execution identity missing')
        if phase == 'develop':
            require(proof['execution_id'] != proof['recovery_execution_id'], 'recovery reused timed-out execution')
    require(result['manager']['before']['holder_id'] != result['develop']['before']['holder_id'],
            'independent phases reused a native holder')
    return result


def run():
    private = FIXTURE.secret_directory()
    expected = json.loads((private / 'highgo-timeout-expectations.json').read_text())
    consumer = client('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN')
    API.validate_user_identity(consumer, int(expected['tenant_id']), PERMISSIONS)
    require(not os.environ.get('ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN')
            and not os.environ.get('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'), 'preparation credentials leaked into page stage')
    artifacts = Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'))
    browser_path = artifacts / 'highgo-sql-timeout-browser.json'
    require(not browser_path.exists(), 'HighGo timeout refuses a previous browser result')
    env = dict(os.environ, ADDP_ONLINE_HIGHGO_TIMEOUT_EXPECTATIONS=json.dumps(expected),
               ADDP_ONLINE_HIGHGO_TIMEOUT_BROWSER_REPORT=str(browser_path))
    finished = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/highgo-sql-timeout.spec.js',
        '--config=playwright.online.config.js'], cwd=Path(__file__).resolve().parents[2] / 'console/frontend', env=env)
    require(finished.returncode == 0 and browser_path.is_file(), 'HighGo timeout Console verification failed')
    result = validate_browser(json.loads(browser_path.read_text()), expected)
    (artifacts / 'highgo-sql-timeout.json').write_text(json.dumps(dict(result=result, result_state='passed'), indent=2))
    print(json.dumps(dict(suite='highgo-consumer-flow', case='sql-timeout', run_id=expected['run_id'],
                         tenant_id=expected['tenant_id'], browser=result), sort_keys=True))


def main():
    FIXTURE.guard()
    require(os.environ.get('ADDP_ONLINE_HIGHGO_CASE') == 'sql-timeout', 'explicit HighGo sql-timeout case required')
    operation = sys.argv[1] if len(sys.argv) == 2 else 'run' if len(sys.argv) == 1 else ''
    require(operation in ('prepare', 'cleanup', 'run'), 'use prepare|cleanup|run')
    {'prepare': prepare, 'cleanup': cleanup, 'run': run}[operation]()


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        # API messages can include source details; artifacts retain only safe code/stage.
        print('HighGo sql-timeout case failed: ' + type(error).__name__, file=sys.stderr)
        raise SystemExit(1)
