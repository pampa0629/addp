"""Observe three unaccelerated maintenance cycles in one real System process."""
from __future__ import annotations

from datetime import datetime, timezone
from importlib import import_module
import json
import os
import sys
import time
import uuid

EVENTS = import_module('scripts.test.execution-events-retention-online')
BASE = EVENTS.BASE
SUITE = 'execution-retention-hourly'
OBSERVATION_SECONDS = 7500
POLL_SECONDS = 30
require = BASE.require


def boundary():
    return BASE.boundary(SUITE)


def sql(statement):
    return BASE.sql(statement, SUITE)


def timestamp(value):
    parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
    require(parsed.tzinfo is not None, 'maintenance time requires an offset')
    return parsed


def fixture_rows(tenant, foreign):
    require(type(tenant) is int and type(foreign) is int and min(tenant,foreign)>1 and tenant!=foreign,
            'two nondefault Tenants required')
    return [dict(id=str(uuid.uuid4()),tenant=tenant if i<60 else foreign,
                 status='success' if i%2==0 else 'running',age=200 if i%2==0 else None,
                 parent=None,retry=None,recovered=None,lease=bool(i%2)) for i in range(120)]


def seed_statement(rows, marker):
    require(len(rows)==120, '120 histories required')
    targets=','.join("'"+str(uuid.UUID(rows[i]['id']))+"'" for i in (0,1,60,61))
    header='INSERT INTO common.execution_events (execution_id,tenant_id,module,task_type,attempt,occurred_at,kind,counters) '
    old=(header+"SELECT execution_id,tenant_id,module,task_type,1,CURRENT_TIMESTAMP-INTERVAL '31 days',"
         "'progress','{\"records_read\":1}'::jsonb FROM common.task_executions CROSS JOIN generate_series(1,251) "
         f"WHERE execution_id IN ({targets}); ")
    phases=''
    for minutes,counter in ((30,3),(90,4)):
        phases+=(header+"SELECT execution_id,tenant_id,module,task_type,1,CURRENT_TIMESTAMP-INTERVAL '30 days'+"
                 f"INTERVAL '{minutes} minutes','progress','{{\"records_read\":{counter}}}'::jsonb "
                 f"FROM common.task_executions WHERE execution_id IN ({targets}); ")
    return EVENTS.seed_history_statement(rows,marker)+" BEGIN; SET LOCAL statement_timeout='10s'; "+old+phases+'COMMIT;'


def records():
    result=[]
    for path in sorted((BASE.ROOT/'logs/runtime/system').glob('*/*.jsonl')):
        for line in path.read_text().splitlines():
            try:
                envelope=json.loads(line)
                payload=json.loads(envelope.get('message',''))
            except (ValueError,TypeError,AttributeError):
                continue
            if not isinstance(payload,dict) or envelope.get('module_name')!='system':
                continue
            require(payload.get('msg') not in {'执行过程事件保留清理失败','执行概览只读保留评估失败'},
                    'real maintenance reported failure')
            if payload.get('msg') not in {EVENTS.MESSAGE,BASE.MESSAGE}:
                continue
            require(not envelope.get('truncated') and envelope.get('instance_id')==path.parent.name,
                    'maintenance source identity mismatch')
            result.append(dict(instance=envelope['instance_id'],payload=payload))
    return sorted(result,key=lambda r:timestamp(r['payload']['time']))


def assert_cycle(pair,index,state,previous):
    require(index in (0,1,2) and len(pair)==2 and len(previous)==index, 'missing or extra cycle records')
    event,overview=[r['payload'] for r in pair]
    instance=pair[0]['instance']
    require(instance and pair[1]['instance']==instance and (not previous or previous[0]['instance']==instance),
            'maintenance process changed')
    require(event.get('msg')==EVENTS.MESSAGE and overview.get('msg')==BASE.MESSAGE, 'maintenance cycle order changed')
    require(set(event)<=EVENTS.SUMMARY_FIELDS|BASE.ENVELOPE_FIELDS and EVENTS.SUMMARY_FIELDS<=set(event),
            'event summary fields not closed')
    expected=dict(deleted=1004 if index==0 else 4,retention_days=30,budget_exhausted=False)
    require(all(type(event[k]) is type(v) and event[k]==v for k,v in expected.items()), 'unexpected event cycle facts')
    require(set(overview)<=BASE.SUMMARY_FIELDS|BASE.ENVELOPE_FIELDS and BASE.SUMMARY_FIELDS<=set(overview),
            'overview summary fields not closed')
    count=20 if index==1 else 100
    wanted=dict(read_only=True,retention_days=180,seeds_evaluated=count,trees_observed=count,complete_trees=count,
                age_eligible_trees=count//2,next_after_id=state['cursors'][index],has_more=index!=1,budget_exhausted=False,
                reason_counts=dict(owner_release_unverified=count,consumer_references_unverified=count,
                                   active_execution=count//2,completion_missing=count//2,lease_active=count//2))
    require(all(type(overview[k]) is type(v) and overview[k]==v for k,v in wanted.items()), 'cursor or retention reasons differ')
    require(type(overview['rows_read']) is int and count<=overview['rows_read']<=10000
            and type(overview['queries']) is int and 1<=overview['queries']<=500, 'overview read budget exceeded')
    when=timestamp(event['time'])
    require(0<=(timestamp(overview['time'])-when).total_seconds()<=30, 'overview did not follow the same event cycle')
    early,late=timestamp(state['early_expiry']),timestamp(state['late_expiry'])
    require((when<early<late if index==0 else early<when<late if index==1 else early<late<when),
            'real expiry phase order differs')
    if previous:
        delta=(when-timestamp(previous[-1]['at'])).total_seconds()
        require(3590<=delta<=3690, 'hourly maintenance accelerated, delayed or missing')
    elapsed=0 if not previous else (when-timestamp(previous[0]['at'])).total_seconds()
    return dict(instance=instance,at=event['time'],elapsed_from_start_seconds=elapsed,event=expected,
                overview={k:overview[k] for k in sorted(BASE.SUMMARY_FIELDS)})


def phase_counts():
    return json.loads(sql("BEGIN READ ONLY; SELECT json_build_object("
        "'old',count(*) FILTER (WHERE counters='{\"records_read\":1}'::jsonb),"
        "'early',count(*) FILTER (WHERE counters='{\"records_read\":3}'::jsonb),"
        "'late',count(*) FILTER (WHERE counters='{\"records_read\":4}'::jsonb)) "
        "FROM common.execution_events; COMMIT;"))


def assert_snapshot(before,after,phases,index):
    require(before['executions']==120 and before['events']==1252 and before['retained']==240
            and before['expired']==1012 and len(before['groups'])==120 and before['audit_count']>0,
            'hourly fixture baseline incomplete')
    for key in ('executions','history_hash','retained','retained_hash','audit_max_id','audit_count','audit_hash'):
        require(after[key]==before[key], 'protected rows changed during a maintenance cycle')
    remaining=(2,1,0)[index]
    require(after['events']==240+4*remaining and after['expired']==4*remaining, 'unexpected remaining events')
    require(sum(g['expired']==253 for g in before['groups'])==4
            and all(g['retained']==2 and g['expired'] in (0,253) for g in before['groups']), 'baseline groups incomplete')
    require(after['groups']==[dict(g,expired=remaining if g['expired'] else 0) for g in before['groups']],
            'per-execution protected events changed')
    require(phases==dict(old=0,early=4 if index==0 else 0,late=4 if index<2 else 0), 'wrong natural expiry group deleted')


def prepare():
    secret,_=boundary()
    proof=json.loads((secret/'retention-authorized.json').read_text())
    require(proof.get('tenant_id')==os.environ['ADDP_ONLINE_TEST_TENANT_ID'], 'formal identity must precede histories')
    require(not (BASE.ROOT/'.dev-pids/system.pid').exists(), 'exclusive System must be stopped')
    require(not (secret/'hourly-retention-state.json').exists(), 'history fixture cannot be replayed')
    rows=fixture_rows(int(os.environ['ADDP_ONLINE_TEST_TENANT_ID']),int(os.environ['ADDP_ONLINE_RETENTION_FOREIGN_TENANT_ID']))
    marker=uuid.uuid4().hex
    sql(seed_statement(rows,marker))
    facts=json.loads(sql("BEGIN READ ONLY; SELECT json_build_object("
        "'cutoff',CURRENT_TIMESTAMP-INTERVAL '29 days 1 hour',"
        "'first_cursor',(SELECT id FROM common.task_executions ORDER BY id OFFSET 99 LIMIT 1),"
        "'last_cursor',(SELECT max(id) FROM common.task_executions),"
        "'early_expiry',(SELECT min(occurred_at)+INTERVAL '30 days' FROM common.execution_events WHERE counters='{\"records_read\":3}'::jsonb),"
        "'late_expiry',(SELECT min(occurred_at)+INTERVAL '30 days' FROM common.execution_events WHERE counters='{\"records_read\":4}'::jsonb)); COMMIT;"))
    before=EVENTS.snapshot(facts['cutoff'],expected_suite=SUITE)
    require(before['events']==1252 and before['executions']==120, 'incomplete hourly history')
    BASE.private_write(secret/'hourly-retention-state.json',dict(before=before,marker=marker,cutoff=facts['cutoff'],
        cursors=[facts['first_cursor'],facts['last_cursor'],facts['first_cursor']],early_expiry=facts['early_expiry'],
        late_expiry=facts['late_expiry'],previous_records=records(),logs=EVENTS.log_prefixes()))


def safe_cycles(cycles):
    # Process UUID and private fixture boundaries never leave the private state.
    return [{k:v for k,v in cycle.items() if k!='instance'} for cycle in cycles]


def observe():
    secret,artifact=boundary()
    path=artifact/f'{SUITE}.json'
    report=dict(schema_version='addp.online-suite/v1',suite=SUITE,run_id=os.environ['ADDP_ONLINE_TEST_RUN_ID'],
                result='failed',overview_physical_deletion_enabled=False,professional_result_lifecycle_verified=False,
                cleanup='pending_deployment_destruction',cycles=[])
    started=time.monotonic();deadline=started+OBSERVATION_SECONDS
    cycles=[]
    try:
        # Only initial identity is checked; no token refresh or authority reuse
        # is introduced to keep a User session alive for the observation period.
        report['identity']=BASE.user_identity()
        state=json.loads((secret/'hourly-retention-state.json').read_text())
        baseline=state['previous_records']
        accepted_records=baseline
        for index in range(3):
            while True:
                observed=records()
                require(observed[:len(accepted_records)]==accepted_records, 'earlier maintenance records changed')
                fresh=observed[len(baseline):]
                require(len(fresh)<=(index+1)*2, 'unexpected extra maintenance cycle')
                if len(fresh)==(index+1)*2:
                    cycle=assert_cycle(fresh[index*2:index*2+2],index,state,cycles)
                    break
                require(time.monotonic()<deadline,'real hourly cycle did not complete within its deadline')
                time.sleep(min(POLL_SECONDS,max(0,deadline-time.monotonic())))
            delay=(datetime.now(timezone.utc)-timestamp(cycle['at'])).total_seconds()
            require(0<=delay<=(900 if index==0 else 90), 'maintenance timestamp is not current')
            cycle['observed_delay_seconds']=round(delay,3)
            cycle['observer_elapsed_seconds']=round(time.monotonic()-started,3)
            after=EVENTS.snapshot(state['cutoff'],state['before']['audit_max_id'],expected_suite=SUITE)
            assert_snapshot(state['before'],after,phase_counts(),index)
            EVENTS.assert_log_prefixes(state['logs'])
            require(state['marker'] not in json.dumps(list(BASE.log_records())), 'private fixture leaked into runtime output')
            accepted_records=observed
            cycles.append(cycle)
            report['cycles']=safe_cycles(cycles)
            report['cycles_verified']=len(cycles)
            # Persist only validated, closed facts on failure or interruption.
            path.write_text(json.dumps(report,sort_keys=True)+'\n')
        require(cycles[-1]['elapsed_from_start_seconds']>=7190,'two genuine hours were not observed')
        real_elapsed=time.monotonic()-started-cycles[0]['observer_elapsed_seconds']+cycles[0]['observed_delay_seconds']
        require(real_elapsed>=7190, 'monotonic observation did not cover two real hourly intervals')
        report.update(result='passed',same_system_instance=True,protected_execution_count=120,protected_event_count=240,
                      elapsed_observer_seconds=round(time.monotonic()-started),checks=[
            'three_unaccelerated_real_cycles','same_process_source','100_20_100_overview_cursor_and_wrap',
            '1004_4_4_real_event_deletions','natural_30_day_phase_expiry','closed_safe_summaries',
            'all_execution_rows_and_inline_professional_facts_preserved_each_cycle',
            '240_protected_events_preserved_each_cycle','audit_baseline_preserved_each_cycle',
            'existing_runtime_log_prefixes_preserved_each_cycle','owner_release_and_consumers_still_unverified',
            'overview_assessment_stays_read_only'])
    except Exception:
        report['error_code']='execution_retention_hourly_failed'
        raise
    finally:
        path.write_text(json.dumps(report,sort_keys=True)+'\n')
    print(json.dumps(report,sort_keys=True))


def main():
    action=sys.argv[1] if len(sys.argv)==2 else 'observe' if len(sys.argv)==1 else ''
    try:
        boundary()
        require(action in {'authorize','prepare','observe'},'invalid hourly acceptance action')
        if action=='authorize':BASE.authorize(SUITE)
        elif action=='prepare':prepare()
        else:observe()
    except Exception as error:
        print(f'execution_retention_hourly_failed:{action}:{type(error).__name__}:'
              f'{error.sqlstate if isinstance(error,BASE.FixtureSQLFailure) else "unavailable"}',file=sys.stderr)
        return 1
    return 0


if __name__=='__main__':raise SystemExit(main())
