"""Observe event expiry in the real System; retain all execution facts."""
from __future__ import annotations

from datetime import datetime
import hashlib
from importlib import import_module
import json
import os
from pathlib import Path
import sys
import time
import uuid

BASE = import_module('scripts.test.execution-retention-readonly-online')
SUITE = 'execution-events-retention'
MESSAGE = '执行过程事件保留清理完成'
SUMMARY_FIELDS = {'deleted', 'retention_days', 'budget_exhausted'}
require = BASE.require


def boundary():
    return BASE.boundary(SUITE)


def sql(statement, expected_suite=SUITE):
    return BASE.sql(statement, expected_suite)


def fixture_rows(tenant, foreign):
    rows = BASE.fixture_rows(tenant, foreign)[:4]
    for index, row in enumerate(rows):
        row.update(tenant=tenant if index < 2 else foreign, parent=None, retry=None,
                   recovered=None, status='success' if index % 2 == 0 else 'running',
                   age=200 if index % 2 == 0 else None, lease=bool(index % 2))
    return rows


def seed_history_statement(rows, marker):
    # Shared immutable payload sentinels and two protected events per history.
    return BASE.seed_statement(rows, marker) + " BEGIN; SET LOCAL statement_timeout='10s'; " + (
        "UPDATE common.task_executions SET metadata=metadata || "
        "'{\"result\":{\"fixture\":true},\"step_results\":[{\"status\":\"success\"}],"
        "\"lineage_facts\":{\"pending\":true}}'::jsonb; "
        "INSERT INTO common.execution_events (execution_id,tenant_id,module,task_type,attempt,occurred_at,kind,counters) "
        "SELECT execution_id,tenant_id,module,task_type,1,CURRENT_TIMESTAMP-INTERVAL '29 days','progress','{\"records_read\":2}'::jsonb "
        "FROM common.task_executions; COMMIT;")


def seed_statement(rows, marker):
    require(len(rows) == 4 and len({r['tenant'] for r in rows}) == 2,
            'four histories across two Tenants required')
    return seed_history_statement(rows, marker) + " BEGIN; SET LOCAL statement_timeout='10s'; " + (
        "INSERT INTO common.execution_events (execution_id,tenant_id,module,task_type,attempt,occurred_at,kind,counters) "
        "SELECT execution_id,tenant_id,module,task_type,1,CURRENT_TIMESTAMP-INTERVAL '31 days','progress','{\"records_read\":1}'::jsonb "
        "FROM common.task_executions CROSS JOIN generate_series(1,251); COMMIT;")


def snapshot(cutoff=None, audit_max=None, expected_suite=SUITE):
    if cutoff is None:
        cutoff_sql = "(SELECT max(occurred_at)-INTERVAL '30 days' FROM common.execution_events)"
    else:
        cutoff_sql = "'" + datetime.fromisoformat(cutoff).isoformat() + "'::timestamptz"
    require(audit_max is None or type(audit_max) is int and audit_max > 0, 'invalid audit baseline')
    audit_bound = '(SELECT max(id) FROM system.audit_logs)' if audit_max is None else str(audit_max)
    # Whole payloads stay in PostgreSQL. A fixed audit high-water mark permits
    # legitimate startup/authentication appends, never mutation of earlier rows.
    return json.loads(sql("BEGIN READ ONLY; WITH boundary AS (SELECT " + cutoff_sql + " AS cutoff), "
        "audits AS (SELECT * FROM system.audit_logs WHERE id <= " + audit_bound + "), "
        "groups AS (SELECT execution_id,count(*) FILTER (WHERE occurred_at < cutoff) AS expired,"
        "count(*) FILTER (WHERE occurred_at >= cutoff) AS retained FROM common.execution_events, boundary GROUP BY execution_id) "
        "SELECT json_build_object('cutoff',(SELECT cutoff FROM boundary),"
        "'executions',(SELECT count(*) FROM common.task_executions),"
        "'history_hash',(SELECT md5(string_agg(row_to_json(e)::text,'' ORDER BY id)) FROM common.task_executions e),"
        "'events',(SELECT count(*) FROM common.execution_events),"
        "'expired',(SELECT count(*) FROM common.execution_events,boundary WHERE occurred_at < cutoff),"
        "'retained',(SELECT count(*) FROM common.execution_events,boundary WHERE occurred_at >= cutoff),"
        "'retained_hash',(SELECT md5(string_agg(row_to_json(e)::text,'' ORDER BY id)) FROM common.execution_events e,boundary WHERE occurred_at >= cutoff),"
        "'audit_max_id'," + audit_bound + ", 'audit_count',(SELECT count(*) FROM audits),"
        "'audit_hash',(SELECT md5(string_agg(row_to_json(a)::text,'' ORDER BY id)) FROM audits a),"
        "'groups',(SELECT json_agg(row_to_json(g) ORDER BY execution_id) FROM groups g)); COMMIT;", expected_suite))


def assert_snapshots(before, after):
    require(before['executions'] == 4 and before['events'] == 1012 and before['expired'] == 1004
            and before['retained'] == 8 and len(before['groups']) == 4
            and type(before['audit_count']) is int and before['audit_count'] > 0,
            'fixture baseline is incomplete')
    for field in ('executions', 'history_hash', 'retained', 'retained_hash', 'audit_max_id', 'audit_count', 'audit_hash'):
        require(after[field] == before[field], 'maintenance changed a protected baseline')
    require(after['expired'] == 0 and after['events'] == 8, 'event expiry did not delete exactly the old events')
    require(all(g['expired'] == 251 and g['retained'] == 2 for g in before['groups']), 'unbalanced fixture coverage')
    expected = [dict(g, expired=0) for g in before['groups']]
    require(after['groups'] == expected, 'expiry did not preserve events for every execution and Tenant')


def log_prefixes():
    result = []
    root = BASE.ROOT / 'logs/runtime/system'
    for path in sorted(root.glob('*/*.jsonl')):
        require(path.is_file() and not path.is_symlink(), 'invalid owned process log')
        raw = path.read_bytes()
        result.append(dict(path=str(path.relative_to(root)), size=len(raw), digest=hashlib.sha256(raw).hexdigest()))
    require(bool(result), 'no original process logs to preserve')
    return result


def assert_log_prefixes(before):
    root = (BASE.ROOT / 'logs/runtime/system').resolve()
    for entry in before:
        path = root / entry['path']
        require(path.resolve().is_relative_to(root) and path.is_file() and not path.is_symlink(),
                'original process log was removed')
        with path.open('rb') as stream:
            prefix = stream.read(entry['size'])
        require(len(prefix) == entry['size'] and hashlib.sha256(prefix).hexdigest() == entry['digest'],
                'original process log was changed')


def maintenance_records():
    records = []
    for record in BASE.log_records():
        require(record.get('msg') not in {'执行过程事件保留清理失败', '执行概览只读保留评估失败'},
                'real maintenance failed')
        if record.get('msg') == MESSAGE:
            records.append(record)
    return sorted(records, key=lambda item: item['time'])


def assert_summary(record):
    require(set(record) <= SUMMARY_FIELDS | BASE.ENVELOPE_FIELDS and SUMMARY_FIELDS <= set(record),
            'event summary contains private or missing fields')
    expected = dict(deleted=1004, retention_days=30, budget_exhausted=False)
    require(all(type(record[k]) is type(v) and record[k] == v for k,v in expected.items()),
            'unexpected real event maintenance summary')
    return expected


def prepare():
    secret, _ = boundary()
    proof=json.loads((secret/'retention-authorized.json').read_text())
    require(proof.get('tenant_id') == os.environ['ADDP_ONLINE_TEST_TENANT_ID'], 'formal identity must precede fixtures')
    require(not (BASE.ROOT/'.dev-pids/system.pid').exists(), 'exclusive System must be stopped')
    require(not (secret/'event-retention-state.json').exists(), 'fixture cannot be replayed')
    rows=fixture_rows(int(os.environ['ADDP_ONLINE_TEST_TENANT_ID']), int(os.environ['ADDP_ONLINE_RETENTION_FOREIGN_TENANT_ID']))
    marker=uuid.uuid4().hex
    sql(seed_statement(rows,marker))
    before=snapshot()
    require(before['executions']==4 and before['events']==1012 and before['audit_count']>0, 'incomplete fixture')
    BASE.private_write(secret/'event-retention-state.json',dict(before=before,cutoff=before['cutoff'],marker=marker,
        previous_records=maintenance_records(), previous_assessments=BASE.maintenance_records(), logs=log_prefixes()))


def observe():
    secret, artifact=boundary()
    report=dict(schema_version='addp.online-suite/v1', suite=SUITE, run_id=os.environ['ADDP_ONLINE_TEST_RUN_ID'],
                result='failed', overview_physical_deletion_enabled=False, professional_result_lifecycle_verified=False,
                cleanup='pending_deployment_destruction')
    try:
        report['identity']=BASE.user_identity()
        state=json.loads((secret/'event-retention-state.json').read_text())
        deadline=time.monotonic()+30
        while True:
            records=maintenance_records()
            previous=state['previous_records']
            require(records[:len(previous)]==previous, 'earlier maintenance evidence changed')
            fresh=records[len(previous):]
            if fresh:
                require(len(fresh)==1,'ambiguous startup event maintenance')
                report['maintenance']=assert_summary(fresh[0])
                break
            require(time.monotonic()<deadline,'real startup event maintenance did not complete')
            time.sleep(.2)
        # Also wait for the same startup's subsequent read-only overview pass.
        while True:
            assessments=BASE.maintenance_records()
            previous=state['previous_assessments']
            require(assessments[:len(previous)]==previous,'earlier overview evidence changed')
            if len(assessments)>len(previous):
                require(len(assessments)==len(previous)+1 and assessments[-1].get('read_only') is True,
                        'overview maintenance must remain read-only')
                break
            require(time.monotonic()<deadline,'subsequent overview assessment did not complete')
            time.sleep(.2)
        after=snapshot(state['cutoff'],state['before']['audit_max_id'])
        assert_snapshots(state['before'],after)
        assert_log_prefixes(state['logs'])
        require(state['marker'] not in json.dumps(list(BASE.log_records())), 'private fixture leaked into logs')
        report.update(result='passed', counts=dict(executions=4, tenants=2, expired_before=1004, expired_after=0,
            retained_before=8, retained_after=8), checks=['real_system_startup_event_pruning','multiple_product_batches',
            'both_tenants_and_active_terminal_executions','retained_events_unchanged','complete_execution_rows_unchanged',
            'inline_professional_results_and_pending_facts_unchanged','existing_audit_rows_unchanged',
            'existing_process_log_prefixes_unchanged','overview_assessment_still_read_only','summary_closed_fields'])
    except Exception:
        report['error_code']='execution_events_retention_failed'
        raise
    finally:
        (artifact/f'{SUITE}.json').write_text(json.dumps(report,sort_keys=True)+'\n')
    print(json.dumps(report,sort_keys=True))


def main():
    action=sys.argv[1] if len(sys.argv)==2 else 'observe' if len(sys.argv)==1 else ''
    try:
        boundary()
        require(action in {'authorize','prepare','observe'},'invalid event acceptance action')
        if action=='authorize':
            BASE.authorize(SUITE)
        elif action=='prepare':
            prepare()
        else:
            observe()
    except Exception as error:
        print(f'execution_events_retention_failed:{action}:{type(error).__name__}:'
              f'{error.sqlstate if isinstance(error,BASE.FixtureSQLFailure) else "unavailable"}',file=sys.stderr)
        return 1
    return 0


if __name__=='__main__':
    raise SystemExit(main())
