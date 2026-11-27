"""Quality's manual Gateway execution on the MySQL platform with an owned PostgreSQL source."""

from contextlib import contextmanager
import importlib
import json
import os
from pathlib import Path
import select
import subprocess
import sys
import time
import uuid

from scripts.utils.hosted_mysql_platform import command, login, mysql_read, owned_container, require

quality = importlib.import_module('scripts.test.quality-dynamic-binding-online')
recovery = importlib.import_module('scripts.test.quality-bounded-recovery-online')
registration = importlib.import_module('scripts.test.online-engine-registration')
raster = importlib.import_module('scripts.test.raster-workflow-online')

ACTOR_PERMISSIONS = {
    'quality.plan.execute', 'quality.plan.read', 'monitor.execution.read',
    'system.execution_authorization.create', 'develop.data_read.execute',
}
AUTHOR_PERMISSIONS = {'quality.rule.create', 'quality.rule.read', 'quality.plan.create'}
SOURCE_USER = 'quality_source_reader'


def psql(container):
    return ['docker', 'exec', '-i', container, 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
            '-U', 'postgres', '-d', 'quality_fixture']


@contextmanager
def hold_source(container):
    # The plan is already defined. Hold this table just long enough to observe
    # the real worker's claim and the subsequent blocked PostgreSQL read.
    with subprocess.Popen(psql(container), stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True) as lock:
        try:
            lock.stdin.write("BEGIN;\nSET LOCAL idle_in_transaction_session_timeout='60s';\n"
                             "LOCK TABLE public.quality_probe IN ACCESS EXCLUSIVE MODE;\nSELECT pg_backend_pid();\n")
            lock.stdin.flush()
            require(select.select([lock.stdout], [], [], 12)[0], 'business PostgreSQL lock did not start')
            pid = int(lock.stdout.readline())
            require(pid > 0, 'business PostgreSQL returned an invalid lock session')
            yield pid
        finally:
            if lock.poll() is None:
                try:
                    lock.stdin.write('ROLLBACK;\n\\q\n')
                    lock.stdin.flush()
                    lock.wait(timeout=8)
                except (BrokenPipeError, subprocess.TimeoutExpired):
                    lock.kill()
                    lock.wait()


def source_waiters(source, pid):
    return int(command(psql(source), input=f"""
SELECT count(*) FROM pg_stat_activity
WHERE datname='quality_fixture' AND usename='{SOURCE_USER}' AND wait_event_type='Lock'
  AND {int(pid)}=ANY(pg_blocking_pids(pid));
"""))


def execution_snapshot(mysql, tenant, execution):
    require(str(uuid.UUID(execution)) == execution, 'invalid Quality execution identity')
    raw = mysql_read(mysql, f"""
SELECT JSON_OBJECT('execution_id',e.execution_id,'status',e.status,'attempt',e.attempt,
 'source_task_id',e.source_task_id,'triggered_by',e.triggered_by,
 'lease_present',e.lease_token IS NOT NULL,'lease_live',COALESCE(e.lease_expires_at>UTC_TIMESTAMP(6),0),
 'worker_owner',e.lease_owner,'worker_instance',h.instance_id,
 'created_at',DATE_FORMAT(e.created_at,'%Y-%m-%dT%H:%i:%s.%fZ'),
 'started_at',DATE_FORMAT(e.started_at,'%Y-%m-%dT%H:%i:%s.%fZ'),
 'completed_at',DATE_FORMAT(e.completed_at,'%Y-%m-%dT%H:%i:%s.%fZ'),'execution_time_ms',e.execution_time_ms,
 'authorized',e.execution_authorization_id IS NOT NULL)
FROM common.task_executions e
LEFT JOIN common.background_runtime_heartbeats h
 ON h.instance_id=REGEXP_REPLACE(e.lease_owner,'-[0-9]+$','') AND h.module='quality'
 AND h.role='execution_worker' AND h.runtime_name='quality-bounded'
 AND h.stopped_at IS NULL AND h.expires_at>UTC_TIMESTAMP(6)
WHERE e.execution_id='{execution}' AND e.tenant_id={int(tenant)}
 AND e.module='quality' AND e.task_type='quality_plan' AND e.source='quality' AND e.trigger_type='manual';
""")
    require(bool(raw), 'original manual Quality execution is missing from MySQL')
    return json.loads(raw)


def verify_live_claim(snapshot, execution, task_id, principal):
    instance = snapshot['worker_instance']
    require(snapshot['execution_id'] == execution and snapshot['status'] == 'running' and snapshot['attempt'] == 1
            and snapshot['source_task_id'] == str(task_id) and snapshot['triggered_by'] == principal
            and snapshot['authorized'] == 1 and snapshot['lease_present'] == 1 and snapshot['lease_live'] == 1
            and snapshot['started_at'] and snapshot['completed_at'] is None and instance
            and snapshot['worker_owner'].startswith(instance + '-')
            and snapshot['worker_owner'][len(instance) + 1:].isdigit(),
            'original execution did not have a live first claim by a registered Quality Worker')


def run(artifacts, mysql, source, worker):
    network = os.environ['ADDP_DEPLOYMENT_NETWORK']
    infra = os.environ['ADDP_MYSQL_FIXTURE_PROJECT']
    owned_container(mysql, infra, 'mysql', network)
    postgres = owned_container(source, infra, 'quality-source', network)
    owned_container(worker, os.environ['ADDP_DEPLOYMENT_PROJECT'], 'quality-worker', network)
    require(not (postgres['HostConfig']['PortBindings'] or {}), 'business PostgreSQL has a published host port')
    privileges = command(psql(source), input=f"""
SELECT has_table_privilege('{SOURCE_USER}','public.quality_probe','SELECT') AND NOT
       has_table_privilege('{SOURCE_USER}','public.quality_probe','INSERT,UPDATE,DELETE,TRUNCATE');
""")
    require(privileges == 't', 'business source account must only read the fixture table')
    source_rows_sql = 'SELECT json_agg(id ORDER BY id) FROM public.quality_probe;'
    source_rows = json.loads(command(psql(source), input=source_rows_sql))
    require(source_rows == [1, 2, 3], 'business PostgreSQL fixture rows differ')

    facts = json.loads(Path(os.environ['ADDP_TEST_MYSQL_QUALITY_FACTS']).read_text())
    tenant = facts['tenant_id']
    actor, author, registrar = [login(f'quality-worker-{role.lower()}', os.environ[f'ADDP_TEST_MYSQL_QUALITY_{role}_PASSWORD'],
                                      facts[f'quality-worker-{role.lower()}'], tenant)
                               for role in ('ACTOR', 'AUTHOR', 'REGISTRAR')]
    actor_identity = raster.validate_identity(actor, tenant, required_permissions=ACTOR_PERMISSIONS)
    raster.validate_identity(author, tenant, required_permissions=AUTHOR_PERMISSIONS)
    roles = registrar.request('GET', '/api/v1/system/auth/context', (200,)).payload['authorization']['role_assignments']
    require([role['role_key'] for role in roles] == ['tenant.infrastructure_administrator'],
            'business Engine requires the independent Hosted registration identity')
    report = dict(source_commit=os.environ['GITHUB_SHA'], platform='mysql', business_source='postgresql',
                  source_version=command(psql(source), input='SHOW server_version;'),
                  source_select_only=True, identity=actor_identity, checks=[], result='incomplete')
    path = artifacts / 'quality-worker.json'

    def record(stage, **facts):
        report['checks'].append(dict(stage=stage, **facts))
        path.write_text(json.dumps(report, indent=2) + '\n')

    record('fixture_ready')
    engine = registration.register_engine(registrar.base_url, registrar.token, {
        'name': 'Hosted Quality PostgreSQL', 'description': 'Owned read-only business source',
        'engine_type': 'postgresql', 'engine_origin': 'general', 'connection_info': {
            'host': 'quality-source', 'port': 5432, 'database': 'quality_fixture',
            'user': SOURCE_USER, 'password': os.environ['ADDP_TEST_QUALITY_SOURCE_PASSWORD'],
        },
    })
    registrar.token = ''
    record('business_source_registered', engine_id=engine)
    locator = f'addp://engine/{engine}/path/public/quality_probe?type=table'
    rule = author.request('POST', quality.QUALITY + '/rules', (201,), {
        'code': 'hosted_quality_worker_rows', 'name': 'Hosted Quality source rows',
        'type': 'row_count', 'params': {'min': len(source_rows), 'max': len(source_rows)},
    }).payload
    rule_id = quality.API.require_positive_int(rule, 'id')
    quality.API.assert_tenant(rule, tenant, 'Quality rule')
    rule_key = str(uuid.uuid4())
    plan = author.request('POST', quality.QUALITY + '/plans', (201,), {
        'code': 'hosted_quality_worker_plan', 'name': 'Hosted Quality Worker plan',
        'table_bindings': [{'alias': 'target', 'locator': locator}],
        'check_items': [{'rule_key': rule_key, 'rule_id': rule_id, 'revision_no': 1,
                         'bindings': {'table': 'target'}, 'severity': 'error', 'disabled': False}],
    }).payload
    plan_id = quality.API.require_positive_int(plan, 'id')
    quality.API.assert_tenant(plan, tenant, 'Quality plan')
    plan_path = f'{quality.QUALITY}/plans/{plan_id}'
    record('quality_plan_ready', plan_id=plan_id)

    with hold_source(source) as lock_pid:
        record('gateway_submit_intent', plan_id=plan_id)
        receipt = actor.request('POST', plan_path + '/run', (202,), {}).payload
        execution = receipt['execution_id']
        require(str(uuid.UUID(execution)) == execution and receipt['status'] == 'pending',
                'Gateway did not accept the original Quality execution')
        record('gateway_accepted', execution_id=execution, http_status=202)
        deadline = time.monotonic() + 35
        while True:
            live = execution_snapshot(mysql, tenant, execution)
            require(live['status'] in {'pending', 'running'}, 'Quality did not reach the source while held')
            waiting = source_waiters(source, lock_pid)
            if live['status'] == 'running' and live['worker_instance'] and waiting == 1:
                break
            require(time.monotonic() < deadline, 'Quality Worker claim or real source query was not observed')
            time.sleep(1)
        verify_live_claim(live, execution, plan_id, facts['quality-worker-actor'])
        record('worker_claimed', mysql=live, postgres_waiters=waiting,
               gateway_views=recovery.assert_projection(actor, execution, live))

    deadline = time.monotonic() + 100
    while True:
        terminal = execution_snapshot(mysql, tenant, execution)
        if terminal['status'] in quality.TERMINAL:
            record('terminal_observed', mysql=terminal)
            break
        require(time.monotonic() < deadline, 'the original Quality execution did not reach terminal status')
        time.sleep(1)
    require(terminal['status'] == 'success' and terminal['attempt'] == 1 and terminal['lease_present'] == 0
            and terminal['started_at'] == live['started_at'] and terminal['completed_at'],
            'terminal execution did not complete the original first attempt')
    views = recovery.assert_projection(actor, execution, terminal)
    detail = actor.request('GET', f'{quality.QUALITY}/executions/{execution}', (200,)).payload
    quality.assert_quality(detail, {'locator': locator, 'row_count': len(source_rows)}, None, True, 1, plan['version'], rule_key)
    summary = actor.request('GET', plan_path, (200,)).payload
    require(summary['last_execution_id'] == execution and summary['last_execution_status'] == 'success',
            'Quality plan summary did not retain the same completed execution')
    event_path = f'/api/v1/monitor/executions/by-execution-id/{execution}/events?limit=100'
    events = quality.validate_quality_event_page(actor.request('GET', event_path, (200,)).payload, execution, True)
    count = int(mysql_read(mysql, f"""
SELECT COUNT(*) FROM common.task_executions WHERE tenant_id={int(tenant)} AND module='quality'
 AND task_type='quality_plan' AND source='quality' AND source_task_id='{int(plan_id)}';
"""))
    require(count == 1, 'the one Gateway submission did not produce exactly one Quality execution')
    require(json.loads(command(psql(source), input=source_rows_sql)) == source_rows,
            'Quality changed the business PostgreSQL source')
    report['result'] = 'passed'
    record('verified', execution_id=execution, gateway_views=views, events=events,
           plan_last_execution_id=summary['last_execution_id'], execution_count=count,
           observed_rows=len(source_rows), source_unchanged=True)


if __name__ == '__main__':
    require(os.environ.get('GITHUB_ACTIONS') == 'true' and os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted'
            and os.environ.get('RUNNER_OS') == 'Linux' and os.environ.get('RUNNER_ARCH') == 'X64'
            and os.environ.get('ADDP_MYSQL_ONLINE_SUITE') == 'quality-worker',
            'requires the owned GitHub Hosted Quality suite')
    run(Path(sys.argv[1]), *sys.argv[2:])
