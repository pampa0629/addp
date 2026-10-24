import copy
import importlib
import json
import os
import re
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

M = importlib.import_module('scripts.test.execution-retention-readonly-online')
CHECK = importlib.import_module('scripts.ci.check-online-ci-registration')


class ExecutionRetentionReadonlyTest(unittest.TestCase):
    def summary(self):
        return dict(time='2026-10-25T00:00:00Z', level='INFO', msg=M.MESSAGE,
                    read_only=True, retention_days=180, seeds_evaluated=16, trees_observed=14,
                    complete_trees=14, age_eligible_trees=5, next_after_id=16, has_more=False,
                    budget_exhausted=False, reason_counts=copy.deepcopy(M.EXPECTED_REASONS), rows_read=50, queries=30)

    def test_declared_facts_and_closed_summary(self):
        self.assertEqual(M.assert_summary(self.summary(), 16)['age_eligible_trees'], 5)
        for key, value in [('age_eligible_trees', 14), ('read_only', False), ('retention_days', 30),
                           ('has_more', True), ('next_after_id', 15), ('rows_read', 10001),
                           ('queries', 501), ('execution_config', {}), ('tenant_id', 2),
                           ('delete_allowed', True), ('trees', [])]:
            with self.subTest(key=key):
                record = self.summary(); record[key] = value
                with self.assertRaises(M.API.SuiteError):
                    M.assert_summary(record, 16)
        record = self.summary(); record['reason_counts'].pop('owner_release_unverified')
        with self.assertRaises(M.API.SuiteError):
            M.assert_summary(record, 16)

    def test_fixture_is_bounded_and_references_are_not_parent_edges(self):
        rows = M.fixture_rows(2, 3)
        self.assertEqual(len(rows), 16)
        self.assertEqual(rows[10]['parent'], rows[9]['id'])
        self.assertNotEqual(rows[10]['tenant'], rows[9]['tenant'])
        self.assertIsNone(rows[12]['parent']); self.assertIsNone(rows[14]['parent'])
        self.assertEqual(rows[12]['retry'], rows[11]['id'])
        self.assertEqual(rows[14]['recovered'], rows[13]['id'])
        sql = M.seed_statement(rows, 'a'*32)
        self.assertIn("INTERVAL '200 days'", sql)
        self.assertIn('history not empty', sql)
        self.assertNotIn('DELETE', sql); self.assertNotIn('TRUNCATE', sql)
        self.assertNotIn('system.', sql)
        for mutation in [dict(tenant=1), dict(status="x';DELETE"), dict(age=181), dict(id='bad')]:
            bad = copy.deepcopy(rows); bad[0].update(mutation)
            with self.assertRaises((ValueError, M.API.SuiteError)):
                M.seed_statement(bad, 'a'*32)

    def test_fixture_statuses_obey_current_store_constraint(self):
        migration = (M.ROOT/'common/execution/migrations/004_normalize_execution_status.sql').read_text()
        allowed = set(re.findall(r"'([^']+)'", re.search(r"CHECK \(status IN \((.*?)\)\)", migration).group(1)))
        self.assertLessEqual({row['status'] for row in M.fixture_rows(2, 3)}, allowed)
        rows = M.fixture_rows(2, 3);rows[0]['status'] = 'unknown_fixture'
        with self.assertRaises(M.API.SuiteError):
            M.seed_statement(rows, 'a'*32)

    def test_fixture_sql_failure_reports_only_sqlstate(self):
        inspected = type('Result', (), {'stdout': '[{}]'})()
        failed = type('Result', (), {'returncode': 1, 'stderr': 'ERROR: 23514 private-fixture-secret', 'stdout': ''})()
        with patch.object(M, 'boundary'), patch.object(M, 'validate_container', return_value='owned'), \
                patch.dict(os.environ, ADDP_ONLINE_RETENTION_POSTGRES_ID='owned'), \
                patch.object(M.subprocess, 'run', side_effect=[inspected, failed]):
            inspected.stdout = json.dumps([{'Id': 'owned'}])
            with self.assertRaises(M.FixtureSQLFailure) as error:
                M.sql('private SQL statement')
            self.assertEqual(error.exception.sqlstate, '23514')
            self.assertNotIn('private', str(error.exception))

    def test_container_identity_refuses_personal_replaced_and_nononline_database(self):
        environment = dict(POSTGRES_USER='owned', POSTGRES_PASSWORD='private')
        container = {'Id': 'container-id', 'State': {'Running': True}, 'Config': {
            'Labels': {'com.docker.compose.project': 'addp-infra', 'com.docker.compose.service': 'postgres',
                       'com.docker.compose.project.working_dir': str(M.ROOT)},
            'Env': ['POSTGRES_DB=addp_online', 'POSTGRES_USER=owned', 'POSTGRES_PASSWORD=private']}}
        self.assertEqual(M.validate_container(container, 'container-id', environment), 'owned')
        for invalid in ('', 'other-container'):
            with self.assertRaises(M.API.SuiteError):
                M.validate_container(container, invalid, environment)
        for key, value in [('POSTGRES_DB', 'addp'), ('POSTGRES_DB', 'addp_test'), ('POSTGRES_PASSWORD', 'other')]:
            bad = copy.deepcopy(container)
            bad['Config']['Env'] = [f'{key}={value}' if e.startswith(key+'=') else e for e in bad['Config']['Env']]
            with self.assertRaises(M.API.SuiteError):
                M.validate_container(bad, 'container-id', environment)
        container['Config']['Labels']['com.docker.compose.project.working_dir'] = '/personal'
        with self.assertRaises(M.API.SuiteError):
            M.validate_container(container, 'container-id', environment)

    def test_normalized_runtime_output_is_decoded_without_changing_platform_contract(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); log = root/'logs/runtime/system/instance/0001.jsonl';log.parent.mkdir(parents=True)
            log.write_text(json.dumps({'module_name': 'system', 'message': json.dumps(self.summary())})+'\n')
            with patch.object(M, 'ROOT', root):
                self.assertEqual(M.maintenance_records(), [self.summary()])
                bad = self.summary(); bad['msg'] = '执行概览只读保留评估失败'
                log.write_text(json.dumps({'module_name': 'system', 'message': json.dumps(bad)})+'\n')
                with self.assertRaises(M.API.SuiteError):
                    M.maintenance_records()

    def test_observation_rejects_changed_whole_rows_and_keeps_failure_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            secret = Path(tmp)/'secret'; secret.mkdir()
            artifact = Path(tmp)/'artifact';artifact.mkdir()
            before = dict(max_id=16, executions=16, events=16, history_hash='hash', events_hash='hash')
            (secret/'retention-state.json').write_text(json.dumps(dict(before=before, marker='private-marker', identities=[], previous_records=[])))
            with patch.object(M, 'boundary', return_value=(secret, artifact)), patch.object(M, 'user_identity', return_value={}), \
                    patch.object(M, 'maintenance_records', return_value=[self.summary()]), patch.object(M, 'log_records', return_value=[]), \
                    patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='test-run'):
                with patch.object(M, 'snapshot', return_value=before), patch('builtins.print'):
                    M.observe()
                self.assertEqual(json.loads((artifact/f'{M.SUITE}.json').read_text())['result'], 'passed')
                for field in ('history_hash', 'events_hash', 'events', 'executions'):
                    after = dict(before);after[field] = 'changed'
                    with patch.object(M, 'snapshot', return_value=after), self.assertRaises(M.API.SuiteError):
                        M.observe()
                    self.assertEqual(json.loads((artifact/f'{M.SUITE}.json').read_text())['result'], 'failed')

    def test_failed_summary_keeps_only_closed_observed_counters(self):
        with tempfile.TemporaryDirectory() as tmp:
            secret = Path(tmp)/'secret';secret.mkdir()
            artifact = Path(tmp)/'artifact';artifact.mkdir()
            before = dict(max_id=16)
            (secret/'retention-state.json').write_text(json.dumps(dict(before=before, marker='private-marker', identities=[], previous_records=[])))
            corrupt = self.summary();corrupt['tenant_id'] = 999;corrupt['execution_config'] = {'secret': 'never-archive'}
            corrupt['reason_counts']['private_reason'] = 'never-archive'
            with patch.object(M, 'boundary', return_value=(secret, artifact)), patch.object(M, 'user_identity', return_value={}), \
                    patch.object(M, 'maintenance_records', return_value=[corrupt]), patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='test-run'), \
                    self.assertRaises(M.API.SuiteError):
                M.observe()
            raw = (artifact/f'{M.SUITE}.json').read_text();report = json.loads(raw)
            self.assertEqual(report['unexpected_summary_field_count'], 2)
            self.assertEqual(report['observed_counts']['seeds_evaluated'], 16)
            for private in ('999', 'never-archive', 'private_reason', 'execution_config'):
                self.assertNotIn(private, raw)

    def test_no_runtime_api_or_owner_lifecycle_route(self):
        source = Path(M.__file__).read_text()
        self.assertNotIn('DELETE FROM', source)
        self.assertNotIn('AssessRetention(', source)
        self.assertNotIn('/api/v1/meta', source)
        self.assertNotIn('/api/v1/system/platform', source)

    def test_initial_summary_must_flush_before_seeding(self):
        with tempfile.TemporaryDirectory() as tmp:
            secret = Path(tmp)
            with patch.object(M, 'boundary', return_value=(secret, secret)), patch.object(M, 'user_identity', return_value={'tenant_id': '2'}), \
                    patch.object(M, 'maintenance_records', side_effect=[[], [{'seeds_evaluated': 0}]]), patch.object(M.time, 'sleep'):
                M.authorize()
                self.assertTrue((secret/'retention-authorized.json').exists())
            with patch.object(M, 'boundary', return_value=(secret, secret)), patch.object(M, 'user_identity', return_value={}), \
                    patch.object(M, 'maintenance_records', return_value=[{'seeds_evaluated': 1}]), self.assertRaises(M.API.SuiteError):
                M.authorize()

    def test_registration_requires_manual_profile_and_standard_regression_owner(self):
        CHECK.validate_execution_retention_profile(M.ROOT, {M.SUITE})
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            files = ['.github/workflows/online-t4-gates.yml', 'Makefile', 'scripts/test/online-hosted-execution-retention-gate.sh',
                     'scripts/test/execution-retention-readonly-online_test.py', 'scripts/test/online-hosted-execution-retention-gate_test.py']
            for name in files:
                path = root/name; path.parent.mkdir(parents=True, exist_ok=True);path.write_text((M.ROOT/name).read_text())
            for name, marker in [('Makefile', '$(MAKE) test-execution-retention-online-runner'),
                                 ('.github/workflows/online-t4-gates.yml', "inputs.suite != 'execution-retention-readonly'"),
                                 ('scripts/test/online-hosted-execution-retention-gate.sh', 'scripts/dev/stop-exact-process.sh -system')]:
                path = root/name; original = path.read_text();path.write_text(original.replace(marker, 'removed'))
                with self.assertRaises(CHECK.RegistrationError):
                    CHECK.validate_execution_retention_profile(root, {M.SUITE})
                path.write_text(original)


if __name__ == '__main__':
    unittest.main()
