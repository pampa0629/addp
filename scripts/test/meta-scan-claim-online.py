"""Observe one real Meta Worker claim on the pinned Hosted product."""
import json
import os
import re
import select
import subprocess
import sys
import time
from contextlib import contextmanager
from datetime import datetime
from importlib import import_module
from pathlib import Path
from types import SimpleNamespace
from uuid import UUID


class ClaimError(RuntimeError):
    pass


def require(ok, message):
    if not ok:
        raise ClaimError(message)


def instant(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00'))


def assert_claim(row, execution, task_id, tenant, workers):
    require(row['execution_id'] == execution and row['tenant_id'] == tenant
            and row['source_task_id'] == str(task_id) and row['status'] == 'running'
            and row['attempt'] == 1 and row['lease_valid'] is True,
            'running lease does not match the submitted task')
    worker, slot = row['lease_owner'].rsplit('-', 1)
    require(worker in workers and slot.isdigit() and 1 <= int(slot) <= workers[worker]['capacity'],
            'lease owner does not belong to a ready Meta Worker slot')
    events = row['events']
    require(len(events) == 1 and events[0]['attempt'] == row['attempt']
            and events[0]['execution_id'] == execution and events[0]['id'] > 0
            and instant(events[0]['occurred_at']) >= instant(row['started_at']),
            'started event does not match the real claim')


def pg_observation(sql):
    result = subprocess.run(['docker', 'exec', '-i', '-e', 'PGOPTIONS=-c default_transaction_read_only=on',
                             'addp-postgres', 'psql', '-XqAt', '-v', 'ON_ERROR_STOP=1',
                             '-U', os.environ['POSTGRES_USER'], '-d', 'addp_online'],
                            input=sql, text=True, capture_output=True, check=True, timeout=6)
    return json.loads(result.stdout) if result.stdout.strip() else None


@contextmanager
def hold_source():
    # Only the disposable source table is locked. The Worker can commit its
    # lease, event and task summary before it requests relation sizes.
    with subprocess.Popen(['docker', 'exec', '-i', 'addp-metric-online-disposable', 'psql',
                           '-XqAt', '-v', 'ON_ERROR_STOP=1', '-U', 'postgres', '-d', 'metric_fixture'],
                          stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True) as lock:
        try:
            lock.stdin.write("BEGIN; SET LOCAL idle_in_transaction_session_timeout='30s'; "
                             "LOCK TABLE public.metric_people IN ACCESS EXCLUSIVE MODE; SELECT 'locked';\n")
            lock.stdin.flush()
            require(select.select([lock.stdout], [], [], 6)[0] and lock.stdout.readline().strip() == 'locked',
                    'source barrier did not become ready')
            yield
        finally:
            lock.stdin.close()
            require(lock.wait(timeout=8) == 0, 'source barrier release failed')


def wait_for(probe, timeout, description):
    until = time.monotonic() + timeout
    while time.monotonic() < until:
        value = probe()
        if value:
            return value
        time.sleep(.15)
    raise ClaimError(description)


def observe(report):
    root = Path.cwd()
    require(os.environ.get('ONLINE_SUITE') == 'meta-scan-claim' and os.environ.get('GITHUB_ACTIONS') == 'true'
            and os.environ.get('ADDP_ONLINE_HOSTED') == '1' and sys.platform == 'linux'
            and os.environ['POSTGRES_DB'] == 'addp_online'
            and root == Path(os.environ['RUNNER_TEMP']) / 'meta-scan-product', 'requires pinned Hosted product')
    sha = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    require(sha == os.environ['META_SCAN_SOURCE_COMMIT'], 'unexpected product SHA')
    sys.path.insert(0, str(root))
    pg = import_module('scripts.test.pg-source-authorization-online')
    preflight = pg.PREFLIGHT
    tenant = int(os.environ['ADDP_ONLINE_TEST_TENANT_ID'])
    engine = int(os.environ['ADDP_ONLINE_CONSUMER_ENGINE_ID'])
    run_id = os.environ['ADDP_ONLINE_TEST_RUN_ID']
    report.update(product_sha=sha, harness_sha=os.environ['GITHUB_SHA'], run_id=run_id, tenant_id=tenant, engine_id=engine)
    started = {}
    for module in ('system', 'meta'):
        service = preflight.parse_service(module + '=' + os.environ[module.upper() + '_URL'])
        ready = preflight.load_health(service, '/health/ready', 5)
        preflight.validate_health(service, ready, sha, 'ready')
        started[module] = ready['started_at']
    worker_rows = wait_for(lambda: pg_observation("""
        SELECT json_agg(json_build_object('instance',instance_id,'started_at',started_at,'capacity',capacity))
        FROM common.background_runtime_heartbeats WHERE module='meta' AND role='execution_worker'
        AND runtime_name='scan' AND stopped_at IS NULL AND expires_at>clock_timestamp()
    """), 30, 'no ready Meta worker')
    workers = {w['instance']: w for w in worker_rows}
    require(all(instant(started['system']) <= instant(started['meta']) <= instant(w['started_at'])
                for w in worker_rows), 'startup order differs')
    report.update(started_at=started, workers=worker_rows, phase='verify_scan_operator')
    operator = pg.API.GatewayClient(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], 10)
    issuer, _ = pg.identity(operator, tenant, pg.OPERATOR_PERMISSIONS)
    scenario = pg.MetaCollectionScenario(SimpleNamespace(operator=operator, tenant=tenant, engine=engine), issuer, run_id)
    try:
        report['phase'] = 'create_task'
        task_id = scenario.create_task('_single')['id']
        report['task_id'] = task_id
        with hold_source():
            report['phase'] = 'trigger'
            execution, attempt = scenario.start_execution(task_id, True)
            require(str(UUID(execution)) == execution, 'unexpected execution identity')
            report.update(execution_id=execution, phase='observe_claim')
            query = f"""
                SELECT json_build_object('execution_id',e.execution_id,'source_task_id',e.source_task_id,
                    'tenant_id',e.tenant_id,'status',e.status,'attempt',e.attempt,'lease_owner',e.lease_owner,
                    'started_at',e.started_at,'lease_valid',e.lease_token IS NOT NULL AND e.lease_expires_at>clock_timestamp(),
                    'events',(SELECT coalesce(json_agg(json_build_object('id',v.id,'execution_id',v.execution_id,
                         'attempt',v.attempt,'occurred_at',v.occurred_at)), '[]') FROM common.execution_events v
                         WHERE v.execution_id=e.execution_id AND v.tenant_id=e.tenant_id AND v.kind='started'))
                FROM common.task_executions e WHERE e.execution_id='{execution}' AND e.module='meta'
                AND e.task_type='scan' AND e.execution_boundary='bounded' AND e.status='running'
            """
            row = wait_for(lambda: pg_observation(query), 20, 'live claim observation timed out')
            assert_claim(row, execution, task_id, tenant, workers)
            report['claim'] = row
        report['phase'] = 'observe_terminal'
        terminal = scenario.observe_execution(task_id, execution, True, attempt)
        task, = [t for t in scenario.task_rows() if t['id'] == task_id]
        require(task['last_execution_id'] == execution and task['last_execution_status'] == terminal['status'] == 'success',
                'terminal summary differs')
        runs = scenario.scheduled_rows(task_id)
        require([r['execution_id'] for r in runs] == [execution], 'task did not have exactly one execution')
        run = operator.request('GET', '/api/v1/meta/executions/' + execution, (200,)).payload
        require(instant(run['started_at']) == instant(row['started_at']) and
                instant(run['completed_at']) >= instant(row['events'][0]['occurred_at']), 'terminal timeline differs')
        report.update(terminal=terminal | {'completed_at':run['completed_at']},
                      summary={'last_execution_id':execution,'last_execution_status':task['last_execution_status']})
    finally:
        scenario.cleanup()
        report['task_cleanup'] = 'passed'


if __name__ == '__main__':
    evidence = {'result': 'failed', 'phase': 'startup'}
    try:
        observe(evidence)
        evidence.update(result='passed', phase='complete')
    except Exception as exc:
        # Only publish known bounded diagnostics, never raw requests or credentials.
        evidence['error_type'] = type(exc).__name__
        message = str(exc)
        if isinstance(exc, ClaimError) or re.fullmatch(r'[^\s]* returned HTTP [1-5]\d\d \([\w.\-]+\)', message):
            evidence['error'] = message
        else:
            import traceback
            frame = traceback.extract_tb(exc.__traceback__)[-1]
            evidence['failure_site'] = f'{Path(frame.filename).name}:{frame.lineno}'
    path = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / 'meta-scan-claim.json'
    path.write_text(json.dumps(evidence, indent=2) + '\n')
    print(json.dumps(evidence))
    sys.exit(0 if evidence['result'] == 'passed' else 1)
