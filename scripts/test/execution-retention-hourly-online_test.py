import copy
from datetime import datetime, timedelta, timezone
import importlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

M = importlib.import_module('scripts.test.execution-retention-hourly-online')


class HourlyRetentionTest(unittest.TestCase):
    def record(self,index):
        count=20 if index==1 else 100
        when=datetime(2026,10,26,tzinfo=timezone.utc)+timedelta(hours=index)
        reasons=dict(owner_release_unverified=count,consumer_references_unverified=count,
                     active_execution=count//2,completion_missing=count//2,lease_active=count//2)
        event=dict(time=when.isoformat(),level='INFO',msg=M.EVENTS.MESSAGE,deleted=1004 if index==0 else 4,
                   retention_days=30,budget_exhausted=False)
        overview=dict(time=(when+timedelta(milliseconds=50)).isoformat(),level='INFO',msg=M.BASE.MESSAGE,
                      read_only=True,retention_days=180,seeds_evaluated=count,trees_observed=count,
                      complete_trees=count,age_eligible_trees=count//2,next_after_id=120 if index==1 else 100,
                      has_more=index!=1,budget_exhausted=False,reason_counts=reasons,rows_read=count+1,queries=count+2)
        return [dict(instance='one-process',payload=p) for p in (event,overview)]

    def state(self):
        return dict(cursors=[100,120,100],early_expiry='2026-10-26T00:30:00+00:00',
                    late_expiry='2026-10-26T01:30:00+00:00')

    def test_three_natural_cycles_and_100_20_100_cursor(self):
        cycles=[]
        for i in range(3):
            cycles.append(M.assert_cycle(self.record(i),i,self.state(),cycles))
        self.assertEqual([c['overview']['seeds_evaluated'] for c in cycles],[100,20,100])
        self.assertEqual([c['event']['deleted'] for c in cycles],[1004,4,4])
        self.assertEqual(cycles[-1]['elapsed_from_start_seconds'],7200)

    def test_rejects_early_or_missing_tick_wrong_cursor_extra_fields_and_process_restart(self):
        first=M.assert_cycle(self.record(0),0,self.state(),[])
        cases=[]
        early=self.record(1);early[0]['payload']['time']='2026-10-26T00:00:10+00:00';cases.append(early)
        restarted=self.record(1);restarted[0]['instance']='different';cases.append(restarted)
        wrong=self.record(1);wrong[1]['payload']['next_after_id']=100;cases.append(wrong)
        private=self.record(1);private[0]['payload']['execution_config']={'private':'never-archive'};cases.append(private)
        cases.extend([self.record(1)[:1],self.record(1)*2])
        for records in cases:
            with self.subTest(records=records),self.assertRaises(M.BASE.API.SuiteError):
                M.assert_cycle(records,1,self.state(),[first])

    def test_rejects_phase_expiry_order_and_wrong_owner_unknown_counts(self):
        wrong=self.state();wrong['early_expiry']='2026-10-25T23:59:00+00:00'
        with self.assertRaises(M.BASE.API.SuiteError):M.assert_cycle(self.record(0),0,wrong,[])
        wrong=self.record(0);wrong[1]['payload']['reason_counts']['consumer_references_unverified']=0
        with self.assertRaises(M.BASE.API.SuiteError):M.assert_cycle(wrong,0,self.state(),[])

    def test_history_and_event_fixture_bounded_and_without_direct_deletion(self):
        rows=M.fixture_rows(2,3)
        self.assertEqual(len(rows),120)
        for tenant in (2,3):
            self.assertEqual(sum(r['tenant']==tenant for r in rows),60)
        self.assertEqual(sum(r['status']=='running' for r in rows),60)
        statement=M.seed_statement(rows,'a'*32)
        for needed in ("INTERVAL '30 minutes'","INTERVAL '90 minutes'",'generate_series(1,251)',"INTERVAL '29 days'"):
            self.assertIn(needed,statement)
        self.assertNotIn('DELETE',statement)
        self.assertNotIn('system.audit_logs',statement)

    def test_each_snapshot_preserves_protected_rows_and_exact_pending_phase_counts(self):
        before=dict(executions=120,events=1252,expired=1012,retained=240,history_hash='all',retained_hash='kept',
                    audit_max_id=5,audit_count=5,audit_hash='audit',groups=[dict(execution_id=str(i),retained=2,expired=253 if i<4 else 0) for i in range(120)])
        for index in range(3):
            after=copy.deepcopy(before);remaining=(2,1,0)[index]
            after.update(events=240+remaining*4,expired=remaining*4)
            for g in after['groups']:g['expired']=remaining if g['expired'] else 0
            phase=dict(old=0,early=4 if index==0 else 0,late=4 if index<2 else 0)
            M.assert_snapshot(before,after,phase,index)
            for key in ('history_hash','retained_hash','audit_hash','events','groups'):
                bad=copy.deepcopy(after);bad[key]=None
                with self.subTest(index=index,key=key),self.assertRaises(M.BASE.API.SuiteError):
                    M.assert_snapshot(before,bad,phase,index)

    def test_observer_complete_success_and_midway_failure_keep_only_verified_safe_cycles(self):
        before=dict(executions=120,events=1252,expired=1012,retained=240,history_hash='all',retained_hash='kept',
                    audit_max_id=5,audit_count=5,audit_hash='audit',groups=[dict(execution_id=str(i),retained=2,expired=253 if i<4 else 0) for i in range(120)])
        for fail in (False,True):
            with self.subTest(fail=fail),tempfile.TemporaryDirectory() as tmp:
                secret=Path(tmp)/'secret';secret.mkdir();artifact=Path(tmp)/'artifact';artifact.mkdir()
                state=dict(self.state(),before=before,marker='private-fixture',cutoff='2026-09-27T00:00:00+00:00',previous_records=[],logs=[])
                (secret/'hourly-retention-state.json').write_text(json.dumps(state))
                pairs=[self.record(i) for i in range(3)]
                if fail:pairs[1][0]['payload']['execution_config']={'secret':'private-fixture'}
                accumulated=[sum(pairs[:i+1],[]) for i in range(3)]
                afters=[];phases=[]
                for index in range(3):
                    after=copy.deepcopy(before);remaining=(2,1,0)[index]
                    after.update(events=240+remaining*4,expired=remaining*4)
                    for group in after['groups']:group['expired']=remaining if group['expired'] else 0
                    afters.append(after);phases.append(dict(old=0,early=4 if index==0 else 0,late=4 if index<2 else 0))
                with patch.object(M,'boundary',return_value=(secret,artifact)), \
                        patch.object(M.BASE,'user_identity',return_value={}), \
                        patch.object(M,'records',side_effect=accumulated), \
                        patch.object(M.EVENTS,'snapshot',side_effect=afters), \
                        patch.object(M,'phase_counts',side_effect=phases), \
                        patch.object(M.EVENTS,'assert_log_prefixes'), \
                        patch.object(M.BASE,'log_records',return_value=[]), \
                        patch.object(M,'datetime',wraps=datetime) as wall, \
                        patch.object(M.time,'monotonic',side_effect=[0,0,3600,7200,7200,7200]), \
                        patch.dict(M.os.environ,ADDP_ONLINE_TEST_RUN_ID='test-hourly'),patch('builtins.print'):
                    wall.now.side_effect=[datetime(2026,10,26,i,0,1,tzinfo=timezone.utc) for i in range(3)]
                    if fail:
                        with self.assertRaises(M.BASE.API.SuiteError):M.observe()
                    else:M.observe()
                raw=(artifact/f'{M.SUITE}.json').read_text();report=json.loads(raw)
                self.assertEqual(report['result'],'failed' if fail else 'passed')
                self.assertEqual(report['cycles_verified'],1 if fail else 3)
                self.assertFalse(report['overview_physical_deletion_enabled'])
                for private in ('one-process','private-fixture','execution_config','history_hash'):
                    self.assertNotIn(private,raw)

    def test_hourly_registration_rejects_budget_drift_and_missing_dispatch(self):
        import shutil
        check=importlib.import_module('scripts.ci.check-online-ci-registration')
        suites={'execution-retention-readonly','execution-events-retention','execution-retention-hourly'}
        check.validate_execution_retention_profile(M.BASE.ROOT,suites)
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            for name in ['.github/workflows/online-t4-gates.yml','Makefile',
                         'scripts/test/online-hosted-execution-retention-gate.sh',
                         'scripts/test/execution-retention-readonly-online_test.py',
                         'scripts/test/execution-events-retention-online_test.py',
                         'scripts/test/execution-retention-hourly-online_test.py',
                         'scripts/test/online-hosted-execution-retention-gate_test.py']:
                path=root/name;path.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(M.BASE.ROOT/name,path)
            for name,fragment in [('.github/workflows/online-t4-gates.yml','180 || 60'),
                                  ('.github/workflows/online-t4-gates.yml',"inputs.suite != 'execution-retention-hourly'"),
                                  ('scripts/test/online-hosted-execution-retention-gate.sh','ADDP_ONLINE_TEST_TIMEOUT_SECONDS=8100'),
                                  ('Makefile','scripts/test/execution-retention-hourly-online_test.py')]:
                p=root/name;before=p.read_text();p.write_text(before.replace(fragment,'removed'))
                with self.subTest(fragment=fragment),self.assertRaises(check.RegistrationError):
                    check.validate_execution_retention_profile(root,suites)
                p.write_text(before)

    def test_observer_does_not_change_real_product_clock_or_add_cleanup_api(self):
        source=Path(M.__file__).read_text()
        for forbidden in ('PruneEvents(', 'AssessRetention(', 'DELETE FROM', 'UPDATE common.execution_events', '/api/v1/system/platform'):
            self.assertNotIn(forbidden,source)
        product=(M.BASE.ROOT/'system/backend/internal/service/execution_event_maintenance.go').read_text()
        self.assertIn('time.NewTicker(time.Hour)',product)
        self.assertEqual(M.OBSERVATION_SECONDS,7500)
        self.assertLessEqual(M.POLL_SECONDS,30)

    def test_runtime_envelope_binds_current_instance_and_closed_payload(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);p=root/'logs/runtime/system/one-process/one.jsonl';p.parent.mkdir(parents=True)
            payload=self.record(0)[0]['payload']
            p.write_text(json.dumps(dict(module_name='system',instance_id='one-process',message=json.dumps(payload)))+'\n')
            with patch.object(M.BASE,'ROOT',root):
                self.assertEqual(M.records(),[self.record(0)[0]])
                p.write_text(json.dumps(dict(module_name='system',instance_id='other',message=json.dumps(payload)))+'\n')
                with self.assertRaises(M.BASE.API.SuiteError):M.records()


if __name__=='__main__':unittest.main()
