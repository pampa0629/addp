"""Acceptance-oracle and exact preparation regressions, without real services."""
from copy import deepcopy
from importlib import import_module
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

OWNER = import_module('scripts.test.highgo-sql-timeout-online')
GATE = import_module('scripts.test.online-gate')


def expected():
    return dict(schema_version=OWNER.SCHEMA, case='sql-timeout', run_id='owned', tenant_id='2',
                principal_id='7', engine_id=29, locator='exact-probe', permissions=sorted(OWNER.PERMISSIONS))


def report():
    result = expected()
    for index, phase in enumerate(('manager', 'develop')):
        holder = 100 + index
        snapshots = {}
        for key, stage, held in [('before', 'acquire', True), ('during', 'observe', True),
                                  ('after_timeout', 'observe', True), ('released', 'release', False)]:
            snapshots[key] = dict(run_id='owned', phase=phase, stage=stage, holder_id=holder, held=held,
                waiters=[dict(id=200, state='active', wait_event_type='Lock', operation='table_read')] if key == 'during' else [])
        result[phase] = dict(**snapshots, elapsed_ms=55_000 if phase == 'manager' else 120_000,
            code='preview_sql_execution_timeout' if phase == 'manager' else 'sql_execution_timeout',
            heading='数据库 SQL 执行超时。', recovery_rows=[dict(id=1, engine_name='HighGo V4.5 Business')],
            execution_id=None if phase == 'manager' else '11111111-1111-4111-8111-111111111111',
            recovery_execution_id=None if phase == 'manager' else '22222222-2222-4222-8222-222222222222')
    return result


class HighGoTimeoutTest(unittest.TestCase):
    def test_dispatch_selects_only_the_explicit_case_and_checks_query_services(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            repository = root / 'repository'
            repository.mkdir()
            environment = dict(ADDP_ONLINE_TEST='1', ADDP_ONLINE_TEST_TENANT_ID='2',
                POSTGRES_DB='addp_online', ADDP_ONLINE_TEST_RUN_ID='owned',
                ADDP_ONLINE_ARTIFACT_DIR=str(root / 'artifacts'))
            for variable in ('GATEWAY_URL', 'SYSTEM_URL', 'META_URL', 'MANAGER_URL', 'DEVELOP_URL', 'SECURITY_URL', 'TRANSFER_URL'):
                environment[variable] = 'http://127.0.0.1:8000'
            for case, module in [('sql-timeout', 'highgo-sql-timeout-online'), ('business', 'highgo-consumer-flow-online')]:
                with patch.object(GATE, 'run_stage', side_effect=[({}, 1), ({'case': case}, 1)]) as stage:
                    result = GATE.run_online('highgo-consumer-flow', repository, dict(environment,
                        ADDP_ONLINE_TEST_RUN_ID='owned-' + case, ADDP_ONLINE_HIGHGO_CASE=case), lock_root=root)
                self.assertEqual(result['result'], 'passed')
                self.assertEqual(stage.call_args_list[1].args[1][-1], 'scripts.test.' + module)
                preflight = stage.call_args_list[0].args[1]
                self.assertEqual('develop=http://127.0.0.1:8000' in preflight, case == 'sql-timeout')
                self.assertEqual('transfer=http://127.0.0.1:8000' in preflight, case == 'business')
            with patch.object(GATE, 'run_stage') as stage, self.assertRaisesRegex(RuntimeError, 'unsupported HighGo'):
                GATE.run_online('highgo-consumer-flow', repository, dict(environment, ADDP_ONLINE_HIGHGO_CASE='unknown'))
            stage.assert_not_called()

    def test_complete_browser_and_native_evidence_pass(self):
        self.assertEqual(OWNER.validate_browser(report(), expected()), report())

    def test_catalog_sql_wait_is_named_separately_from_table_content_read(self):
        value = report()
        value['manager']['during']['waiters'][0]['operation'] = 'table_catalog'
        self.assertEqual(OWNER.validate_browser(value, expected()), value)
        value['manager']['during']['waiters'][0]['operation'] = 'unknown'
        with self.assertRaisesRegex(RuntimeError, 'invalid native waiter'):
            OWNER.validate_browser(value, expected())

    def test_rejects_wrong_scope_missing_phase_generic_timeout_and_missing_recovery(self):
        for fault in ('identity', 'permissions', 'case', 'missing', 'generic', 'recovery', 'early', 'sql', 'id'):
            value = report()
            if fault == 'identity': value['principal_id'] = '8'
            if fault == 'permissions': value['permissions'].append('system.engine.create')
            if fault == 'case': value['case'] = 'business'
            if fault == 'missing': del value['develop']
            if fault == 'generic': value['develop']['code'] = 'timeout'
            if fault == 'recovery': value['manager']['recovery_rows'] = []
            if fault == 'early': value['manager']['elapsed_ms'] = 150
            if fault == 'sql': value['manager']['raw_sql'] = 'private source query'
            if fault == 'id': value['develop']['recovery_execution_id'] = value['develop']['execution_id']
            with self.subTest(fault=fault), self.assertRaises(RuntimeError):
                OWNER.validate_browser(value, expected())

    def test_rejects_unobserved_or_changed_holder_and_residual_native_sql(self):
        for fault in ('missing_waiter', 'changed_holder', 'residual', 'not_released', 'wrong_phase', 'private_query', 'same_holder'):
            value = report(); proof = value['develop']
            if fault == 'missing_waiter': proof['during']['waiters'] = []
            if fault == 'changed_holder': proof['after_timeout']['holder_id'] = 999
            if fault == 'residual': proof['after_timeout']['waiters'] = deepcopy(proof['during']['waiters'])
            if fault == 'not_released': proof['released']['held'] = True
            if fault == 'wrong_phase': proof['during']['phase'] = 'manager'
            if fault == 'private_query': proof['during']['waiters'][0]['query'] = 'source SQL'
            if fault == 'same_holder':
                for key in ('before', 'during', 'after_timeout', 'released'): proof[key]['holder_id'] = 100
            with self.subTest(fault=fault), self.assertRaises(RuntimeError):
                OWNER.validate_browser(value, expected())

    def test_prepare_scans_and_grants_only_probe_using_separate_identity(self):
        with tempfile.TemporaryDirectory() as temp:
            private = Path(temp)
            consumer, preparer = Mock(), Mock()
            consumer.request.return_value = SimpleNamespace(payload={'principal': {'id': '7'}})
            record = dict(id=17, node_id=9, item_type='table', full_name='addp_business.addp_engine_probe')
            env = dict(ADDP_ONLINE_TEST_TENANT_ID='2', ADDP_ONLINE_HIGHGO_ENGINE_ID='29', ADDP_ONLINE_TEST_RUN_ID='owned')
            def grant(*args, **kwargs):
                kwargs['on_pending'](29, 'request-id', {'engine_id': 29})
            with patch.dict(OWNER.os.environ, env), patch.object(OWNER.FIXTURE, 'secret_directory', return_value=private), \
                 patch.object(OWNER, 'client', side_effect=[consumer, preparer]), \
                 patch.object(OWNER.API, 'validate_user_identity'), \
                 patch.object(OWNER.REGISTRATION, 'source_grant_participants') as participants, \
                 patch.object(OWNER.SUPPORT, 'wait_for_scan', return_value='scan-id') as scan, \
                 patch.object(OWNER.REGISTRATION, 'initialize_exact_record_read_grants', side_effect=grant) as grants, \
                 patch.object(OWNER.SUPPORT, 'find_item', return_value=record), \
                 patch.object(OWNER.SUPPORT, 'build_item_locator', return_value='exact-probe'):
                OWNER.prepare()
            self.assertEqual(participants.call_args.kwargs, {'scan_preparation': True})
            self.assertEqual(scan.call_args.args[:2], (preparer, 29))
            self.assertEqual(scan.call_args.kwargs, dict(scan_depth='basic', targets=[
                'addp://engine/29/path/addp_business/addp_engine_probe?type=table']))
            self.assertEqual(grants.call_args.args[3], [(29, 'schema', 'addp_business', 'addp_engine_probe')])
            snapshot = json.loads((private / 'highgo-timeout-expectations.json').read_text())
            self.assertEqual(snapshot['permissions'], sorted(OWNER.PERMISSIONS))
            self.assertEqual(snapshot['locator'], 'exact-probe')
            self.assertEqual((private / 'highgo-timeout-grant.json').stat().st_mode & 0o777, 0o600)

    def test_cleanup_reuses_exact_journal_and_proves_current_denial(self):
        with tempfile.TemporaryDirectory() as temp:
            private = Path(temp)
            (private / 'highgo-timeout-grant.json').write_text(json.dumps(dict(engine_id=29, request_id='owned-grant', target={'exact': True})))
            preparer, consumer = Mock(), Mock()
            with patch.dict(OWNER.os.environ, ADDP_ONLINE_ARTIFACT_DIR=temp, ADDP_ONLINE_TEST_RUN_ID='owned'), \
                 patch.object(OWNER.FIXTURE, 'secret_directory', return_value=private), \
                 patch.object(OWNER, 'client', side_effect=[preparer, consumer]):
                OWNER.cleanup()
            self.assertEqual(preparer.request.call_args.args[1], '/api/v1/system/engines/29/access_grants/owned-grant/revoke')
            self.assertEqual(consumer.request.call_args.args[2:], ((403,), {'targets': [{'exact': True}]}))
            self.assertTrue(json.loads((private / 'highgo-timeout-grant-cleanup.json').read_text())['read_denied'])


if __name__ == '__main__':
    unittest.main()
