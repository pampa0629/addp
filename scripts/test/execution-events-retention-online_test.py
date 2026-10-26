import copy
import importlib
import json
from pathlib import Path
import tempfile
import shutil
import unittest
from unittest.mock import patch

M = importlib.import_module('scripts.test.execution-events-retention-online')


class ExecutionEventsRetentionTest(unittest.TestCase):
    def snapshot(self, after=False):
        return dict(executions=4, history_hash='whole-row', events=8 if after else 1012,
                    expired=0 if after else 1004, retained=8, retained_hash='retained',
                    audit_max_id=9, audit_count=9, audit_hash='audit',
                    groups=[dict(execution_id=str(i), expired=0 if after else 251,
                                 retained=2) for i in range(4)])

    def test_exact_deletion_and_complete_preservation(self):
        M.assert_snapshots(self.snapshot(), self.snapshot(True))
        for key in ('executions', 'history_hash', 'events', 'expired', 'retained',
                    'retained_hash', 'audit_count', 'audit_hash', 'audit_max_id', 'groups'):
            bad = self.snapshot(True);bad[key] = None
            with self.subTest(key=key), self.assertRaises(M.BASE.API.SuiteError):
                M.assert_snapshots(self.snapshot(), bad)
        bad = self.snapshot(True);bad['groups'][0]['retained'] = 1
        with self.assertRaises(M.BASE.API.SuiteError):
            M.assert_snapshots(self.snapshot(), bad)

    def test_maintenance_summary_is_closed_and_matches_real_deleted_count(self):
        record = dict(msg=M.MESSAGE, time='2026-10-26T00:00:00Z', level='INFO',
                      deleted=1004, retention_days=30, budget_exhausted=False)
        M.assert_summary(record)
        for key, value in [('deleted', 1000), ('deleted', True), ('retention_days', 180),
                           ('budget_exhausted', True), ('metadata', {}), ('tenant_id', 2)]:
            bad = dict(record);bad[key] = value
            with self.subTest(key=key), self.assertRaises(M.BASE.API.SuiteError):
                M.assert_summary(bad)

    def test_legal_history_has_two_tenants_and_old_and_active_executions(self):
        rows = M.fixture_rows(2, 3)
        self.assertEqual(len(rows), 4)
        self.assertEqual({r['tenant'] for r in rows}, {2, 3})
        self.assertEqual([r['status'] for r in rows], ['success', 'running', 'success', 'running'])
        statement = M.seed_statement(rows, 'a'*32)
        self.assertIn('generate_series(1,251)', statement)
        self.assertIn("INTERVAL '31 days'", statement)
        self.assertIn("INTERVAL '29 days'", statement)
        self.assertIn('lineage_facts', statement)
        self.assertNotIn('DELETE', statement)
        self.assertNotIn('system.audit_logs', statement)

    def test_process_logs_allow_append_but_reject_removal_or_rewrite(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp);path = root/'logs/runtime/system/old/one.jsonl'
            path.parent.mkdir(parents=True);path.write_bytes(b'original\n')
            with patch.object(M.BASE, 'ROOT', root):
                before = M.log_prefixes()
                path.write_bytes(b'original\nnew\n');M.assert_log_prefixes(before)
                path.write_bytes(b'changed\n')
                with self.assertRaises(M.BASE.API.SuiteError):
                    M.assert_log_prefixes(before)
                path.unlink()
                with self.assertRaises(M.BASE.API.SuiteError):
                    M.assert_log_prefixes(before)

    def test_failed_report_does_not_archive_private_state(self):
        with tempfile.TemporaryDirectory() as tmp:
            secret = Path(tmp)/'secret';secret.mkdir();artifact=Path(tmp)/'artifact';artifact.mkdir()
            (secret/'event-retention-state.json').write_text(json.dumps(dict(before=self.snapshot(),
                marker='private-fixture', cutoff='2026-09-01T00:00:00+00:00', previous_records=[])))
            record=dict(msg=M.MESSAGE, deleted=1004, retention_days=30, budget_exhausted=False,
                        execution_config={'secret': 'private-fixture'})
            with patch.object(M, 'boundary', return_value=(secret,artifact)), \
                    patch.object(M.BASE, 'user_identity', return_value={}), \
                    patch.object(M, 'maintenance_records', return_value=[record]), \
                    patch.dict(M.os.environ, ADDP_ONLINE_TEST_RUN_ID='test'), self.assertRaises(M.BASE.API.SuiteError):
                M.observe()
            raw=(artifact/f'{M.SUITE}.json').read_text()
            self.assertEqual(json.loads(raw)['result'], 'failed')
            for private in ('private-fixture', 'execution_config', 'whole-row', 'audit_hash'):
                self.assertNotIn(private, raw)

    def test_successful_observation_requires_all_baselines_and_no_overview_delete(self):
        with tempfile.TemporaryDirectory() as tmp:
            secret=Path(tmp)/'secret';secret.mkdir();artifact=Path(tmp)/'artifact';artifact.mkdir()
            state=dict(before=self.snapshot(),cutoff='2026-09-01T00:00:00+00:00',marker='private-fixture',
                       previous_records=[],previous_assessments=[],logs=[{'private':'baseline'}])
            (secret/'event-retention-state.json').write_text(json.dumps(state))
            record=dict(time='now',msg=M.MESSAGE,deleted=1004,retention_days=30,budget_exhausted=False)
            with patch.object(M,'boundary',return_value=(secret,artifact)), \
                    patch.object(M.BASE,'user_identity',return_value={}), \
                    patch.object(M,'maintenance_records',return_value=[record]), \
                    patch.object(M.BASE,'maintenance_records',return_value=[{'read_only':True}]), \
                    patch.object(M.BASE,'log_records',return_value=[]), \
                    patch.object(M,'snapshot',return_value=self.snapshot(True)), \
                    patch.object(M,'assert_log_prefixes') as preserve_logs, \
                    patch.dict(M.os.environ,ADDP_ONLINE_TEST_RUN_ID='test'),patch('builtins.print'):
                M.observe()
            report=json.loads((artifact/f'{M.SUITE}.json').read_text())
            self.assertEqual(report['result'],'passed');self.assertEqual(len(report['checks']),10)
            self.assertFalse(report['overview_physical_deletion_enabled'])
            self.assertFalse(report['professional_result_lifecycle_verified'])
            preserve_logs.assert_called_once_with(state['logs'])
            for private in ('private-fixture','history_hash','execution_id','audit_hash'):
                self.assertNotIn(private,json.dumps(report))

    def test_registration_rejects_missing_event_profile_and_regressions(self):
        check=importlib.import_module('scripts.ci.check-online-ci-registration')
        suites={'execution-retention-readonly','execution-events-retention'}
        check.validate_execution_retention_profile(M.BASE.ROOT,suites)
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            for name in ['.github/workflows/online-t4-gates.yml','Makefile',
                         'scripts/test/online-hosted-execution-retention-gate.sh',
                         'scripts/test/execution-retention-readonly-online_test.py',
                         'scripts/test/execution-events-retention-online_test.py',
                         'scripts/test/online-hosted-execution-retention-gate_test.py']:
                path=root/name;path.parent.mkdir(parents=True,exist_ok=True)
                shutil.copy2(M.BASE.ROOT/name,path)
            for name,marker in [('.github/workflows/online-t4-gates.yml',"inputs.suite != 'execution-events-retention'"),
                                ('.github/workflows/online-t4-gates.yml',"inputs.suite == 'execution-events-retention'"),
                                ('Makefile','scripts/test/execution-events-retention-online_test.py'),
                                ('scripts/test/online-hosted-execution-retention-gate.sh','scripts.test.execution-events-retention-online')]:
                path=root/name;original=path.read_text();path.write_text(original.replace(marker,'removed'))
                with self.subTest(marker=marker),self.assertRaises(check.RegistrationError):
                    check.validate_execution_retention_profile(root,suites)
                path.write_text(original)

    def test_no_direct_product_maintenance_or_runtime_api(self):
        source=Path(M.__file__).read_text()
        for forbidden in ('DELETE FROM', 'PruneEvents(', 'AssessRetention(', '/api/v1/meta', '/api/v1/system/platform'):
            self.assertNotIn(forbidden, source)


if __name__ == '__main__':
    unittest.main()
