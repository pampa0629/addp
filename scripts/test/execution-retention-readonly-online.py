"""Observe the real System startup maintenance; never call AssessRetention here."""
from __future__ import annotations

from importlib import import_module
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import uuid

API = import_module('scripts.utils.online-api')
HOSTED = import_module('scripts.test.orchestrator-execution-faults')
ROOT = Path(__file__).resolve().parents[2]
SUITE = 'execution-retention-readonly'
MESSAGE = '执行概览只读保留评估完成'
EXPECTED_REASONS = {
    'owner_release_unverified': 14, 'consumer_references_unverified': 14,
    'not_expired': 1, 'active_execution': 1, 'completion_missing': 2,
    'lease_active': 2, 'status_unknown': 1, 'relationship_anomaly': 4,
    'execution_reference': 2,
}
SUMMARY_FIELDS = {'read_only', 'retention_days', 'seeds_evaluated', 'trees_observed',
                  'complete_trees', 'age_eligible_trees', 'rows_read', 'queries',
                  'reason_counts', 'next_after_id', 'has_more', 'budget_exhausted'}
ENVELOPE_FIELDS = {'time', 'level', 'msg', 'module', 'service', 'instance_id', 'stream',
                   'schema_version', 'timestamp', 'sequence', 'trace_id', 'span_id',
                   'host', 'pid', 'request_id', 'build_id', 'git_commit'}


def require(condition, message):
    if not condition:
        raise API.SuiteError(message)


def boundary():
    secret = HOSTED.deployment_secret()
    require(os.environ.get('ONLINE_SUITE') == SUITE and os.environ.get('ADDP_ONLINE_TEST') == '1',
            'dedicated read-only retention suite required')
    require(not os.environ.get('ADDP_ONLINE_OWNER_MANAGED') and not os.environ.get('ADDP_EXECUTION_TRACE_LOCAL'),
            'mixed deployment profiles forbidden')
    artifact = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']).resolve(strict=True)
    require(not artifact.is_relative_to(ROOT) and not secret.is_relative_to(artifact)
            and not artifact.is_relative_to(secret), 'private and evidence directories must be disjoint')
    return secret, artifact


def user_identity():
    tenant = int(os.environ['ADDP_ONLINE_TEST_TENANT_ID'])
    foreign = int(os.environ['ADDP_ONLINE_RETENTION_FOREIGN_TENANT_ID'])
    require(tenant > 1 and foreign > 1 and tenant != foreign, 'two distinct nondefault Tenants required')
    client = API.GatewayClient(os.environ['SYSTEM_URL'], os.environ['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 10)
    return API.validate_user_identity(client, tenant, {'monitor.execution.read'})


def validate_container(container, expected, environment):
    config = container.get('Config', {})
    labels = config.get('Labels') or {}
    values = dict(value.split('=', 1) for value in config.get('Env', []) if '=' in value)
    require(bool(expected) and container.get('Id') == expected and container.get('State', {}).get('Running')
            and labels.get('com.docker.compose.project') == 'addp-infra'
            and labels.get('com.docker.compose.service') == 'postgres'
            and labels.get('com.docker.compose.project.working_dir') == str(ROOT)
            and values.get('POSTGRES_DB') == 'addp_online'
            and bool(values.get('POSTGRES_PASSWORD')) and bool(values.get('POSTGRES_USER'))
            and all(values.get(key) == environment.get(key) for key in ('POSTGRES_PASSWORD', 'POSTGRES_USER')),
            'refusing PostgreSQL outside this exclusive deployment')
    return values['POSTGRES_USER']


def sql(statement):
    boundary()
    inspected = subprocess.run(['docker', 'inspect', 'addp-postgres'], check=True,
                               capture_output=True, text=True, timeout=10)
    container, = json.loads(inspected.stdout)
    user = validate_container(container, os.environ.get('ADDP_ONLINE_RETENTION_POSTGRES_ID'), os.environ)
    result = subprocess.run(['docker', 'exec', '-i', container['Id'], 'psql', '-X', '-qAt',
                             '-v', 'ON_ERROR_STOP=1', '-U', user, '-d', 'addp_online'],
                            input=statement, check=True, capture_output=True, text=True, timeout=20)
    return result.stdout.strip()


def private_write(path, value):
    temporary = path.with_suffix('.tmp')
    # Do not briefly create readable files before chmod.
    descriptor = os.open(temporary, os.O_CREAT | os.O_TRUNC | os.O_WRONLY, 0o600)
    with os.fdopen(descriptor, 'w') as output:
        json.dump(value, output, sort_keys=True)
    temporary.replace(path)


def log_records():
    # Local process output is read only by this test driver. It is never exposed
    # through tenant APIs, Loki or the retained acceptance artifact.
    for path in sorted((ROOT / 'logs/runtime/system').glob('*/*.jsonl')):
        for line in path.read_text().splitlines():
            try:
                record = json.loads(line)
            except ValueError:
                continue
            if isinstance(record, dict) and record.get('module_name') == 'system' and not record.get('truncated'):
                try:
                    payload = json.loads(record.get('message', ''))
                except (ValueError, TypeError):
                    continue
                if isinstance(payload, dict):
                    yield payload


def maintenance_records():
    records = []
    for record in log_records():
        if record.get('msg') == MESSAGE:
            records.append(record)
        require(record.get('msg') != '执行概览只读保留评估失败', 'real System maintenance reported failure')
    return sorted(records, key=lambda item: item['time'])


def fixture_rows(tenant, foreign):
    ids = [str(uuid.uuid4()) for _ in range(16)]
    # Database intervals, not the acceptance driver's clock, establish age.
    rows = [{'id': identity, 'tenant': tenant, 'status': 'success', 'age': 200,
             'parent': None, 'retry': None, 'recovered': None, 'lease': False}
            for identity in ids]
    rows[1]['parent'] = ids[0]                  # expired, complete two-node tree
    rows[3].update(parent=ids[2], age=1)        # young child retains its old parent
    rows[4].update(status='running', age=None, lease=True)
    rows[5]['lease'] = True                    # terminal but still leased
    rows[6]['status'] = 'unknown_fixture'
    rows[7]['age'] = None                      # missing completion
    rows[8]['parent'] = str(uuid.uuid4())      # absent parent
    rows[9]['tenant'] = foreign
    rows[10]['parent'] = ids[9]                # forbidden cross-Tenant tree edge
    rows[12]['retry'] = ids[11]                # references are not tree edges
    rows[14]['recovered'] = ids[13]
    rows[15]['parent'] = ids[15]               # self-cycle
    return rows


def seed_statement(rows, marker):
    require(re.fullmatch('[0-9a-f]{32}', marker), 'invalid fixture marker')
    values = []
    def quoted(value):
        return 'NULL' if value is None else "'" + str(uuid.UUID(value)) + "'"
    for row in rows:
        require(type(row['tenant']) is int and row['tenant'] > 1, 'invalid fixture Tenant')
        require(row['status'] in {'success', 'running', 'unknown_fixture'}, 'invalid fixture status')
        require(row['age'] in {None, 1, 200}, 'invalid fixture completion')
        completed = 'NULL' if row['age'] is None else f"CURRENT_TIMESTAMP - INTERVAL '{row['age']} days'"
        lease = "CURRENT_TIMESTAMP + INTERVAL '2 days'" if row['lease'] else 'NULL'
        metadata = {'fixture_marker': marker, 'outputs': {'sample': marker}}
        if row['recovered']:
            metadata['recovered_from_execution_id'] = str(uuid.UUID(row['recovered']))
        values.append(f"({quoted(row['id'])},{row['tenant']},'meta','scan','meta','{row['status']}',"
                      f"'manual',{quoted(row['parent'])},{quoted(row['retry'])},{completed},{lease},"
                      f"'{{\"secret_fixture\":\"{marker}\"}}'::jsonb,"
                      f"'{{\"private_error\":\"{marker}\"}}'::jsonb,'{json.dumps(metadata)}'::jsonb,"
                      "CURRENT_TIMESTAMP - INTERVAL '201 days',CURRENT_TIMESTAMP)")
    return ("BEGIN; SET LOCAL statement_timeout='10s'; "
            "DO $$ BEGIN IF EXISTS (SELECT 1 FROM common.task_executions) OR "
            "EXISTS (SELECT 1 FROM common.execution_events) THEN RAISE EXCEPTION 'history not empty'; END IF; END $$; "
            "INSERT INTO common.task_executions (execution_id,tenant_id,module,task_type,source,status,trigger_type,"
            "parent_execution_id,retry_of_execution_id,completed_at,lease_expires_at,execution_config,error_details,metadata,created_at,updated_at) VALUES "
            + ','.join(values) + "; INSERT INTO common.execution_events (execution_id,tenant_id,module,task_type,attempt,occurred_at,kind,counters) "
            "SELECT execution_id,tenant_id,module,task_type,1,CURRENT_TIMESTAMP,'started','{}'::jsonb FROM common.task_executions; COMMIT;")


def snapshot():
    # Whole rows include configuration, private errors, result metadata and lease
    # facts, but only hashes and counts leave PostgreSQL; no private payloads.
    return json.loads(sql("BEGIN READ ONLY; SELECT json_build_object("
        "'executions',(SELECT count(*) FROM common.task_executions),"
        "'events',(SELECT count(*) FROM common.execution_events),"
        "'max_id',(SELECT max(id) FROM common.task_executions),"
        "'history_hash',(SELECT md5(string_agg(row_to_json(e)::text,'' ORDER BY id)) FROM common.task_executions e),"
        "'events_hash',(SELECT md5(string_agg(row_to_json(e)::text,'' ORDER BY id)) FROM common.execution_events e)); COMMIT;"))


def authorize():
    secret, _ = boundary()
    identity = user_identity()
    # Readiness alone does not guarantee the startup maintenance goroutine has
    # finished or that its output receiver has flushed the first summary.
    deadline = time.monotonic() + 30
    while True:
        records = maintenance_records()
        if records:
            require(len(records) == 1 and records[0].get('seeds_evaluated') == 0,
                    'fresh deployment must start with empty execution history')
            break
        require(time.monotonic() < deadline, 'initial maintenance output did not become ready')
        time.sleep(.2)
    private_write(secret / 'retention-authorized.json', identity)


def prepare():
    secret, _ = boundary()
    proof = json.loads((secret / 'retention-authorized.json').read_text())
    require(proof.get('tenant_id') == os.environ['ADDP_ONLINE_TEST_TENANT_ID'], 'formal identity must be checked before fixture creation')
    require(not (ROOT / '.dev-pids/system.pid').exists(), 'System must be stopped by the standard exclusive lifecycle before seeding')
    require(not (secret / 'retention-state.json').exists(), 'fixture must not be replayed')
    tenant = int(os.environ['ADDP_ONLINE_TEST_TENANT_ID'])
    foreign = int(os.environ['ADDP_ONLINE_RETENTION_FOREIGN_TENANT_ID'])
    require(tenant > 1 and foreign > 1 and tenant != foreign, 'invalid fixture Tenants')
    rows = fixture_rows(tenant, foreign)
    marker = uuid.uuid4().hex
    sql(seed_statement(rows, marker))
    before = snapshot()
    require(before['executions'] == 16 and before['events'] == 16, 'fixture preparation incomplete')
    private_write(secret / 'retention-state.json', {'before': before, 'marker': marker,
                  'identities': [r['id'] for r in rows], 'previous_records': maintenance_records()})


def assert_summary(record, max_id):
    require(set(record) <= SUMMARY_FIELDS | ENVELOPE_FIELDS and SUMMARY_FIELDS <= set(record),
            'maintenance summary contains private or missing fields')
    expected = {'read_only': True, 'retention_days': 180, 'seeds_evaluated': 16,
                'trees_observed': 14, 'complete_trees': 14, 'age_eligible_trees': 5,
                'next_after_id': max_id, 'has_more': False, 'budget_exhausted': False,
                'reason_counts': EXPECTED_REASONS}
    require(all(type(record[k]) is type(v) and record[k] == v for k, v in expected.items()),
            'real maintenance retention facts differ from the declared fixtures')
    require(type(record['rows_read']) is int and 16 <= record['rows_read'] <= 10000
            and type(record['queries']) is int and 1 <= record['queries'] <= 500,
            'maintenance exceeded its read budget')
    return {key: record[key] for key in sorted(SUMMARY_FIELDS)}


def observe():
    secret, artifact = boundary()
    report = {'schema_version': 'addp.online-suite/v1', 'suite': SUITE,
              'run_id': os.environ['ADDP_ONLINE_TEST_RUN_ID'], 'result': 'failed',
              'physical_deletion_enabled': False, 'professional_result_lifecycle_verified': False,
              'cleanup': 'pending_deployment_destruction'}
    try:
        report['identity'] = user_identity()
        state = json.loads((secret / 'retention-state.json').read_text())
        deadline = time.monotonic() + 30
        while True:
            records = maintenance_records()
            require(records[:len(state['previous_records'])] == state['previous_records'],
                    'earlier maintenance evidence changed')
            fresh = records[len(state['previous_records']):]
            if fresh:
                require(len(fresh) == 1, 'ambiguous startup maintenance observations')
                break
            require(time.monotonic() < deadline, 'real System did not finish its startup assessment')
            time.sleep(.2)
        report['assessment'] = assert_summary(fresh[0], state['before']['max_id'])
        raw = json.dumps(list(log_records()), ensure_ascii=False)
        require(state['marker'] not in raw and not any(identity in json.dumps(fresh) for identity in state['identities']),
                'maintenance logs exposed private fixtures or execution identities')
        require(snapshot() == state['before'], 'read-only maintenance changed execution or event rows')
        report.update(result='passed', complete_history_unchanged=True, retained_events_unchanged=True,
                      checks=['real_system_startup_maintenance', 'expired_complete_tree', 'young_child_retains_parent',
                              'active_and_leased', 'unknown_and_missing_completion', 'cross_tenant_missing_self_parent',
                              'retry_recovery_references', 'owner_and_consumer_unknown', 'summary_closed_fields',
                              'full_history_and_events_unchanged'])
    except Exception:
        report['error_code'] = 'execution_retention_readonly_failed'
        raise
    finally:
        (artifact / f'{SUITE}.json').write_text(json.dumps(report, sort_keys=True) + '\n')
    print(json.dumps(report, sort_keys=True))


def main():
    try:
        action = sys.argv[1] if len(sys.argv) == 2 else 'observe' if len(sys.argv) == 1 else ''
        require(action in {'authorize', 'prepare', 'observe'}, 'invalid maintenance acceptance action')
        secret, _ = boundary()
        if action == 'authorize':
            authorize()
        elif action == 'prepare':
            prepare()
        else:
            observe()
    except Exception as error:
        # Never echo SQL, subprocess output, private paths or sensitive fixtures.
        print(f'execution_retention_readonly_failed:{action}:{type(error).__name__}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
