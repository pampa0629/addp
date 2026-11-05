"""Native runner refusal/orchestration contracts, not Hosted success evidence."""
import copy
import importlib
import json
import sys
import tempfile
import unittest
import uuid
from pathlib import Path
from types import SimpleNamespace
from urllib.error import HTTPError
from unittest.mock import patch
from contextlib import contextmanager

M = importlib.import_module('scripts.test.cleanup-native-effects-online')
S = M.S
ID = str(uuid.uuid4())


class NativeCleanupRunnerTest(unittest.TestCase):
    def test_owner_cause_contract_matches_all_runtime_declarations(self):
        import re
        declared = {'engine.deleting': [], 'tenant.deleted': []}
        constants = {'CleanupCauseEngineDeleting': 'engine.deleting', 'CleanupCauseTenantDeleted': 'tenant.deleted'}
        for path in sorted(S.ROOT.glob('*/backend/cmd/server/main.go')):
            owner, text = path.relative_to(S.ROOT).parts[0], path.read_text()
            blocks = re.findall(r'"cleanup_executor":\s*map\[string\]interface\{\}\{([^}]+?"causes":\s*\[\]string\{[^}]+\})', text)
            if not blocks:
                self.assertNotIn('"cleanup_executor"', text, owner)
                continue
            self.assertEqual(len(blocks), 1, owner)
            self.assertRegex(blocks[0], r'"enabled":\s*true')
            names = re.findall(r'\w+\.(CleanupCause\w+)', blocks[0])
            self.assertTrue(names, owner)
            self.assertEqual(len(set(names)), len(names), owner)
            for name in names:
                declared[constants[name]].append(owner)
        self.assertEqual(S.CONTRACT['cleanup_owners_by_cause'], declared)
        self.assertEqual(tuple(declared['tenant.deleted']), S.OWNERS)

    def test_public_cleanup_permission_matches_every_engine_owner_declaration(self):
        import re
        for owner in S.CONTRACT['cleanup_owners_by_cause']['engine.deleting']:
            text = (S.ROOT / owner / 'backend/internal/api/execution_read_scope.go').read_text()
            self.assertRegex(text, r'(?:"cleanup_executor"|execution\.TaskTypeCleanupExecutor):\s*"system\.cleanup\.read"', owner)

    def test_engine_execute_and_diagnostic_require_exact_cause_members(self):
        expected = list(S.CONTRACT['cleanup_owners_by_cause']['engine.deleting'])
        for owners in (expected, expected[::-1], expected[:-1], expected + ['asset'],
                       expected + [expected[0]], list(S.OWNERS), tuple(expected), None, [True] + expected[1:]):
            accepted = type(owners) is list and len(owners) == len(expected) and owners in (expected, expected[::-1])
            parent = dict(task_id=ID, status='timeout', task=dict(task_id=ID, execution_id=ID,
                context={'engine_id': 9}, requested_by=17, action='scan', expected_modules=owners),
                module_executions=[])
            with self.subTest(owners=owners), patch.object(S, 'user_identity', return_value='17'), \
                    patch.object(M.PROTOCOL, 'task', return_value=parent):
                observation = M.engine_task_observation(parent, ID, 9, '17', action='scan')
                self.assertEqual(observation['bindings']['owners'], accepted)
                self.assertEqual(observation['status'], 'timeout')
                self.assertEqual('children' in observation, accepted)
                parent['task']['action'] = 'execute'
                workflow = M.EngineWorkflow(object(), 2, {}, lambda: None)
                workflow.engine = lambda _: {'deletion_execute_task_id': ID}
                if accepted:
                    self.assertIs(workflow.execution(9, deadline=0), parent)
                else:
                    with self.assertRaises(S.SuiteError): workflow.execution(9, deadline=0)

    def test_assessment_diagnostic_projects_only_bound_authorized_closed_states(self):
        parent, child = str(uuid.uuid4()), str(uuid.uuid4())
        value = dict(task_id=ID, status='failed', task=dict(task_id=ID, execution_id=parent,
            context={'engine_id': 9}, requested_by=17, action='scan', expected_modules=list(S.CONTRACT['cleanup_owners_by_cause']['engine.deleting'])),
            module_executions=[dict(module='transfer', execution_id=child, parent_execution_id=parent,
                task_type='cleanup_executor', status='failed', attempt=0, started_at=None,
                error_details={'code': 'cleanup_authorization_denied', 'error': 'secret'},
                cleanup_admission={'state': 'closed_rejected', 'observed_at': 'secret'},
                receipt={'status': 'failed', 'reason': 'authorization_denied', 'statistics': {'secret': 1}},
                metadata={'token': 'secret'})], error='secret', metadata={'secret': True})
        observed = M.engine_task_observation(value, ID, 9, '17', action='scan')
        self.assertEqual(observed, dict(request_id=ID, observation='observed', status='failed',
            bindings=dict(request=True, engine=True, actor=True, action=True, owners=True),
            children_observation='observed', children=[dict(module='transfer', status='failed', attempt=0,
                started=False, receipt_status='failed', receipt_reason='authorization_denied',
                admission_state='closed_rejected', authorization_denied=True, authorization_unavailable=False)]))
        self.assertNotIn('secret', json.dumps(observed)); self.assertNotIn('private', json.dumps(observed))
        for mutation in ('request', 'engine', 'actor', 'action', 'owners', 'hidden', 'duplicate', 'parent', 'unsafe'):
            candidate = copy.deepcopy(value)
            if mutation == 'request': candidate['task_id'] = str(uuid.uuid4())
            elif mutation == 'engine': candidate['task']['context']['engine_id'] = True
            elif mutation == 'actor': candidate['task']['requested_by'] = 99
            elif mutation == 'action': candidate['task']['action'] = 'execute'
            elif mutation == 'owners': candidate['task']['expected_modules'].append('transfer')
            elif mutation == 'hidden': candidate['module_executions'][0]['module'] = 'asset'
            elif mutation == 'duplicate': candidate['module_executions'] *= 2
            elif mutation == 'parent': candidate['module_executions'][0]['parent_execution_id'] = str(uuid.uuid4())
            else:
                row = candidate['module_executions'][0]
                row.update(status='secret', attempt=True, receipt={'status': 'secret', 'reason': 'secret'},
                    cleanup_admission={'state': 'secret'})
                candidate['status'] = 'secret'
            with self.subTest(mutation=mutation):
                result = M.engine_task_observation(candidate, ID, 9, '17', action='scan')
                self.assertNotIn('secret', json.dumps(result))
                if mutation in {'request', 'engine', 'actor', 'action', 'owners'}:
                    self.assertEqual(result['observation'], 'identity_mismatch'); self.assertNotIn('children', result)
                    self.assertFalse(result['bindings'][mutation])
                elif mutation in {'hidden', 'duplicate', 'parent'}:
                    self.assertEqual(result['children_observation'], 'invalid_response'); self.assertEqual(result['children'], [])
                else:
                    self.assertEqual(result['status'], 'unrecognized')
                    self.assertEqual(result['children'][0]['attempt'], 'unrecognized')
        self.assertEqual(M.engine_task_observation(None, ID, 9, '17', action='scan'),
                         dict(request_id=ID, observation='invalid_response'))

    def test_public_owner_scopes_are_distinct_from_two_professional_permissions(self):
        parent = str(uuid.uuid4())
        owners = S.CONTRACT['cleanup_owners_by_cause']['engine.deleting']
        value = dict(task_id=ID, status='timeout', task=dict(task_id=ID, execution_id=parent,
            context={'engine_id': 9}, requested_by=17, action='scan', expected_modules=owners),
            module_executions=[dict(module=owner, execution_id=str(uuid.uuid4()), parent_execution_id=parent,
                task_type='cleanup_executor', status='pending', attempt=0,
                metadata={'private': 'secret'}, error_details={'private': 'secret'}) for owner in owners])
        observation = M.engine_task_observation(value, ID, 9, '17', action='scan')
        self.assertEqual(observation['children_observation'], 'observed')
        self.assertEqual([r['module'] for r in observation['children']], ['manager', 'transfer'])
        self.assertNotIn('secret', json.dumps(observation))
        for mutation in ('foreign', 'duplicate', 'parent', 'identity', 'task_type'):
            bad = copy.deepcopy(value); row = next(r for r in bad['module_executions'] if r['module'] == 'meta')
            if mutation == 'foreign': row['module'] = 'asset'
            elif mutation == 'duplicate': bad['module_executions'].append(copy.deepcopy(row))
            elif mutation == 'parent': row['parent_execution_id'] = str(uuid.uuid4())
            elif mutation == 'identity': row['execution_id'] = 'secret'
            else: row['task_type'] = 'scan'
            with self.subTest(mutation=mutation):
                result = M.engine_task_observation(bad, ID, 9, '17', action='scan')
                self.assertEqual(result['children_observation'], 'invalid_response')
                self.assertEqual(result['children'], []); self.assertNotIn('secret', json.dumps(result))

    def test_invalid_public_children_report_only_closed_guard_reasons(self):
        parent, child = str(uuid.uuid4()), str(uuid.uuid4())
        value = dict(task_id=ID, status='timeout', task=dict(task_id=ID, execution_id=parent,
            context={'engine_id': 9}, requested_by=17, action='scan',
            expected_modules=S.CONTRACT['cleanup_owners_by_cause']['engine.deleting']),
            module_executions=[dict(module='manager', execution_id=child, parent_execution_id=parent,
                task_type='cleanup_executor', status='pending', attempt=0)])
        for reason in ('invalid_parent_identity', 'invalid_collection', 'invalid_child_shape', 'unexpected_owner',
                       'wrong_task_type', 'foreign_parent', 'invalid_execution_identity', 'duplicate_owner', 'duplicate_execution'):
            bad = copy.deepcopy(value); row = bad['module_executions'][0]
            if reason == 'invalid_parent_identity': bad['task']['execution_id'] = 'secret'
            elif reason == 'invalid_collection': bad['module_executions'] = {'secret': True}
            elif reason == 'invalid_child_shape': bad['module_executions'] = ['secret']
            elif reason == 'unexpected_owner': row['module'] = 'secret'
            elif reason == 'wrong_task_type': row['task_type'] = 'secret'
            elif reason == 'foreign_parent': row['parent_execution_id'] = 'secret'
            elif reason == 'invalid_execution_identity': row['execution_id'] = 'secret'
            elif reason == 'duplicate_owner': bad['module_executions'].append({**row, 'execution_id': str(uuid.uuid4())})
            else: bad['module_executions'].append({**row, 'module': 'transfer'})
            with self.subTest(reason=reason):
                result = M.engine_task_observation(bad, ID, 9, '17', action='scan')
                self.assertEqual(result['children_failure_kind'], reason)
                self.assertEqual(result['children'], [])
                self.assertNotIn('secret', json.dumps(result))

    def test_child_authorization_reason_uses_canonical_public_code(self):
        parent, child = str(uuid.uuid4()), str(uuid.uuid4())
        for code in ('cleanup_authorization_denied', 'cleanup_authorization_unavailable', 'secret', None):
            value = dict(task_id=ID, status='timeout', task=dict(task_id=ID, execution_id=parent,
                context={'engine_id': 9}, requested_by=17, action='scan',
                expected_modules=S.CONTRACT['cleanup_owners_by_cause']['engine.deleting']),
                module_executions=[dict(module='manager', execution_id=child, parent_execution_id=parent,
                    task_type='cleanup_executor', status='failed', attempt=1, started_at='present',
                    error_details={'code': code})])
            row = M.engine_task_observation(value, ID, 9, '17', action='scan')['children'][0]
            self.assertEqual(row['authorization_denied'], code == 'cleanup_authorization_denied')
            self.assertEqual(row['authorization_unavailable'], code == 'cleanup_authorization_unavailable')
            self.assertNotIn('secret', json.dumps(row))

    def test_manager_failure_stage_requires_failed_bound_observation_and_closed_step(self):
        parent, child = str(uuid.uuid4()), str(uuid.uuid4())
        value = dict(task_id=ID, status='timeout', task=dict(task_id=ID, execution_id=parent,
            context={'engine_id': 9}, requested_by=17, action='scan',
            expected_modules=S.CONTRACT['cleanup_owners_by_cause']['engine.deleting']),
            module_executions=[dict(module='manager', execution_id=child, parent_execution_id=parent,
                task_type='cleanup_executor', status='failed', attempt=1, started_at='present')])
        for stage in ('plan', 'inventory', 'engine_facts', 'confirmation', 'commit', 'artifact', 'index_effect', 'finalize'):
            candidate = copy.deepcopy(value); row = candidate['module_executions'][0]
            row.update(current_step='manager_cleanup_'+stage,
                steps=[dict(id='manager_cleanup_'+stage, status='failed', phase='terminal', error_code='secret')])
            result = M.engine_task_observation(candidate, ID, 9, '17', action='scan')
            self.assertEqual(result['children'][0]['failure_step'], 'manager_cleanup_'+stage)
            self.assertNotIn('secret', json.dumps(result))
        for bad in ('unknown', 'missing_step', 'duplicate', 'wrong_status', 'wrong_phase', 'not_failed', 'not_started', 'not_attempt', 'transfer'):
            candidate = copy.deepcopy(value); row = candidate['module_executions'][0]
            row.update(current_step='manager_cleanup_inventory', steps=[dict(id='manager_cleanup_inventory', status='failed', phase='terminal')])
            if bad == 'unknown': row['current_step'] = row['steps'][0]['id'] = 'secret'
            elif bad == 'missing_step': row['steps'] = []
            elif bad == 'duplicate': row['steps'] *= 2
            elif bad == 'wrong_status': row['steps'][0]['status'] = 'success'
            elif bad == 'wrong_phase': row['steps'][0]['phase'] = 'waiting'
            elif bad == 'not_failed': row['status'] = 'pending'
            elif bad == 'not_started': row.pop('started_at')
            elif bad == 'not_attempt': row['attempt'] = True
            else: row['module'] = 'transfer'
            with self.subTest(bad=bad):
                result = M.engine_task_observation(candidate, ID, 9, '17', action='scan')
                self.assertNotIn('failure_step', result['children'][0]); self.assertNotIn('secret', json.dumps(result))

    def test_transfer_failure_stage_uses_only_exact_failed_owner_step(self):
        parent, child = str(uuid.uuid4()), str(uuid.uuid4())
        value = dict(task_id=ID, status='timeout', task=dict(task_id=ID, execution_id=parent,
            context={'engine_id': 9}, requested_by=17, action='execute',
            expected_modules=S.CONTRACT['cleanup_owners_by_cause']['engine.deleting']),
            module_executions=[dict(module='transfer', execution_id=child, parent_execution_id=parent,
                task_type='cleanup_executor', status='failed', attempt=1, started_at='present')])
        for stage in ('plan', 'stop', 'capture_plan', 'capture_connector', 'capture_source',
                      'capture_group', 'capture_topic', 'capture_acl', 'capture_history_topic',
                      'capture_history_acl', 'dlq', 'commit'):
            candidate = copy.deepcopy(value); row = candidate['module_executions'][0]
            step = 'transfer_cleanup_'+stage
            row.update(current_step=step, steps=[dict(id=step, status='failed', phase='terminal', error_code='private-metadata-sentinel')])
            result = M.engine_task_observation(candidate, ID, 9, '17', action='execute')
            self.assertEqual(result['children'][0]['failure_step'], step)
            self.assertNotIn('private-metadata-sentinel', json.dumps(result))
        for bad in ('manager_step', 'unknown', 'missing', 'duplicate', 'wrong_status', 'wrong_phase',
                    'not_failed', 'not_started', 'old_attempt', 'bool_attempt'):
            candidate = copy.deepcopy(value); row = candidate['module_executions'][0]
            step = 'transfer_cleanup_capture_plan'
            row.update(current_step=step, steps=[dict(id=step, status='failed', phase='terminal')])
            if bad == 'manager_step': row['current_step'] = row['steps'][0]['id'] = 'manager_cleanup_inventory'
            elif bad == 'unknown': row['current_step'] = row['steps'][0]['id'] = 'private-metadata-sentinel'
            elif bad == 'missing': row['steps'] = []
            elif bad == 'duplicate': row['steps'] *= 2
            elif bad == 'wrong_status': row['steps'][0]['status'] = 'success'
            elif bad == 'wrong_phase': row['steps'][0]['phase'] = 'waiting'
            elif bad == 'not_failed': row['status'] = 'running'
            elif bad == 'not_started': row.pop('started_at')
            elif bad == 'old_attempt': row['attempt'] = 2
            else: row['attempt'] = True
            result = M.engine_task_observation(candidate, ID, 9, '17', action='execute')
            self.assertNotIn('failure_step', result['children'][0])
            self.assertNotIn('private-metadata-sentinel', json.dumps(result))

    def test_professional_read_selects_only_its_existing_two_owner_permissions(self):
        parent, calls = str(uuid.uuid4()), []
        completed = dict(task_id=ID, task={'execution_id': parent}, module_executions=[])
        for owner in S.CONTRACT['cleanup_owners_by_cause']['engine.deleting']:
            execution_id = str(uuid.uuid4())
            receipt = dict(schema='addp.cleanup-receipt/v1', tenant_id=2, parent_execution_id=parent,
                request_id=ID, execution_id=execution_id, module=owner, attempt=1,
                action='scan', cleanup_mode='', status='success', summary={}, reason='')
            completed['module_executions'].append(dict(module=owner, execution_id=execution_id,
                parent_execution_id=parent, task_type='cleanup_executor', status='success', attempt=1, receipt=receipt))
        class User:
            def request(self, method, path, expected):
                calls.append((method, path, expected))
                row, = [r for r in completed['module_executions'] if r['execution_id'] in path]
                if row['module'] not in {'manager', 'transfer'}: raise AssertionError('extra professional read')
                result = {k: v for k, v in row['receipt'].items() if k in M.PROTOCOL.RESULT_KEYS}
                return SimpleNamespace(payload={**result, 'schema': 'addp.cleanup-professional-result/v1', 'statistics': {}})
        for owner in ('manager', 'transfer'):
            result = M.professional(User(), completed, owner, 2)
            self.assertEqual(result['module'], owner)
        self.assertEqual(len(calls), 2)
        for mutation in ('missing_selected', 'duplicate_other', 'foreign_parent'):
            bad = copy.deepcopy(completed)
            if mutation == 'missing_selected': bad['module_executions'] = [r for r in bad['module_executions'] if r['module'] != 'transfer']
            elif mutation == 'duplicate_other': bad['module_executions'].append(copy.deepcopy(bad['module_executions'][0]))
            else: bad['module_executions'][0]['parent_execution_id'] = str(uuid.uuid4())
            with self.subTest(mutation=mutation), self.assertRaises(S.SuiteError): M.professional(User(), bad, 'manager', 2)
        self.assertEqual(len(calls), 2)

    def test_assessment_failure_keeps_last_observation_without_extra_reads_or_changed_budget(self):
        for failed in ('first_read', 'after_pending', 'terminal', 'identity', 'success'):
            report, calls, checkpoints = {'requests': []}, [], []
            class User:
                def request(self, method, path, expected, body=None):
                    calls.append((method, path, expected))
                    if method == 'POST':
                        self.request_id = body['request_id']; return SimpleNamespace(payload={'assessment_id': self.request_id})
                    if '/deletion-assessments/' in path:
                        if failed == 'first_read' or (failed == 'after_pending' and len(calls) == 4):
                            raise S.SuiteError('GET /private returned HTTP 503 (secret)')
                        return SimpleNamespace(payload=dict(task_id=self.request_id,
                            status='pending' if failed == 'after_pending' else 'failed' if failed == 'terminal' else 'success',
                            task=dict(task_id=self.request_id, execution_id=ID, context={'engine_id': 9},
                                requested_by=99 if failed == 'identity' else 17, action='scan', expected_modules=list(S.CONTRACT['cleanup_owners_by_cause']['engine.deleting'])),
                            module_executions=[]))
                    engine = dict(id=9, tenant_id=2, lifecycle_state='active', version=3, name='secret')
                    if method == 'DELETE': engine['lifecycle_state'] = 'deleting'; return SimpleNamespace(payload={'engine': engine})
                    return SimpleNamespace(payload=engine)
            def wait(read, predicate, message, **kwargs):
                self.assertEqual(kwargs, {})  # Original canonical 120s default; no extended budget.
                value = read()
                if failed == 'after_pending': self.assertFalse(predicate(value)); read()
                self.assertTrue(predicate(value)); return value
            with self.subTest(failed=failed), patch.object(S, 'user_identity', return_value='17'), patch.object(S, 'wait_for', side_effect=wait):
                workflow = M.EngineWorkflow(User(), 2, report, lambda: checkpoints.append(copy.deepcopy(report)))
                workflow.owned.add(9)
                if failed == 'success':
                    workflow.begin(9); self.assertNotIn('engine_assessment_diagnostic', report)
                    self.assertTrue(all('engine_assessment_diagnostic' not in c for c in checkpoints)); self.assertEqual(len(calls), 5)
                else:
                    with self.assertRaises(S.SuiteError): workflow.begin(9)
                    diagnostic = report['engine_assessment_diagnostic']
                    self.assertEqual(diagnostic['observation'], 'not_observed' if failed == 'first_read' else
                        'identity_mismatch' if failed == 'identity' else 'observed')
                    if failed in {'after_pending', 'terminal'}:
                        self.assertEqual(diagnostic['status'], 'pending' if failed == 'after_pending' else 'failed')
                    self.assertEqual(len(calls), 4 if failed == 'after_pending' else 3)
                    self.assertFalse(any(c[0] == 'DELETE' for c in calls))
                    self.assertEqual(report['stage'], 'engine_assessment_read')
                    self.assertEqual(checkpoints[-1]['engine_assessment_diagnostic'], diagnostic)
                    self.assertNotIn('secret', json.dumps(report))

    def test_cdc_readiness_uses_engine_user_reads_and_same_scan_deadline(self):
        for case in ('transfer-engine', 'transfer-capture-unknown'):
            calls, engines = [], iter((9, 10))
            workflow = SimpleNamespace(register=lambda _: next(engines))
            controls = SimpleNamespace(run_id='native-1', descriptor=lambda _: {})
            def scan(user, engine, deadline): calls.append(('scan', engine, deadline))
            def ready(owner, engines, deadline, report, checkpoint):
                self.assertIs(owner, workflow)
                calls.append(('ready', engines, deadline))
            with self.subTest(case=case), patch.object(M.time, 'monotonic', return_value=20), \
                    patch.object(M.MANAGER, 'wait_for_meta_scan', side_effect=scan), \
                    patch.object(M, 'wait_native_engines', side_effect=ready, create=True), \
                    patch.object(M, 'prepare_task', side_effect=S.SuiteError('stop before effects')):
                with self.assertRaises(S.SuiteError):
                    M.run_transfer(object(), workflow, controls, case, {'checks': []}, lambda: None)
                self.assertEqual(calls, [('scan', 9, 200), ('ready', [(9, 'postgresql'), (10, 'kafka')], 200)])

    def test_engine_readiness_waits_for_both_current_observations_without_mutation(self):
        reads, report = [], {}
        class Owner:
            def engine(self, engine, **scope):
                reads.append(engine)
                return dict(engine_type='postgresql' if engine == 9 else 'kafka', lifecycle_state='active',
                            connection_status='unknown' if len(reads) <= 2 else 'online',
                            connection_info={'password': 'private'}, error='secret')
        with patch.object(M.time, 'monotonic', side_effect=[1, 2]), patch.object(M.time, 'sleep') as sleep:
            M.wait_native_engines(Owner(), [(9, 'postgresql'), (10, 'kafka')], 180, report, lambda: self.fail('no failure diagnostics on success'))
        self.assertEqual(reads, [9, 10, 9, 10])
        sleep.assert_called_once_with(1)
        self.assertEqual(report, {})

    def test_engine_readiness_deadline_and_invalid_state_fail_closed_with_safe_last_observation(self):
        for state in ('unknown', 'offline', 'checking', 'secret status'):
            report, reads = {}, []
            def engine(identity, **scope):
                reads.append(identity)
                return dict(engine_type='postgresql' if identity == 9 else 'kafka', lifecycle_state='active',
                            connection_status=state, password='secret password')
            with self.subTest(state=state), patch.object(M.time, 'monotonic', side_effect=[1, 180]), \
                    patch.object(M.time, 'sleep') as sleep:
                with self.assertRaises(S.SuiteError):
                    M.wait_native_engines(SimpleNamespace(engine=engine), [(9, 'postgresql'), (10, 'kafka')], 180, report, lambda: None)
                self.assertEqual(reads, [9, 10])
                self.assertEqual([r['connection_status'] for r in report['engine_readiness_diagnostic']['engines']],
                                 [state if state != 'secret status' else 'unrecognized'] * 2)
                self.assertNotIn('secret', json.dumps(report))
                sleep.assert_called_once_with(1)
        report = {}
        with patch.object(M.time, 'monotonic', return_value=180):
            with self.assertRaises(S.SuiteError):
                M.wait_native_engines(SimpleNamespace(engine=lambda _, **scope: self.fail('expired deadline must not read')),
                                        [(9, 'postgresql'), (10, 'kafka')], 180, report, lambda: None)
        self.assertEqual([r['observation'] for r in report['engine_readiness_diagnostic']['engines']],
                         ['not_observed', 'not_observed'])

    def test_engine_readiness_rejects_changed_type_lifecycle_and_read_errors(self):
        for bad in ('type', 'lifecycle', 'read'):
            calls, report = [], {}
            def engine(identity, **scope):
                calls.append(identity)
                if bad == 'read': raise S.SuiteError('GET /private returned HTTP 403 (secret)')
                return dict(engine_type='minio' if bad == 'type' else 'postgresql',
                            lifecycle_state='deleting' if bad == 'lifecycle' else 'active', connection_status='online')
            with self.subTest(bad=bad), patch.object(M.time, 'monotonic', return_value=1), \
                    patch.object(M.time, 'sleep') as sleep:
                with self.assertRaises(S.SuiteError):
                    M.wait_native_engines(SimpleNamespace(engine=engine), [(9, 'postgresql'), (10, 'kafka')], 180, report, lambda: None)
                self.assertEqual(calls, [9])
                sleep.assert_not_called()
                self.assertNotIn('secret', json.dumps(report))

    def test_task_create_400_records_bound_closed_reason_without_retry_or_raw_error(self):
        cases = [
            ('invalid transfer task config: resolve database CDC source engine: engine 9 is unavailable', 'source_engine_unavailable'),
            ('invalid transfer task config: resolve database CDC target engine: engine 9 is unavailable', 'target_engine_unavailable'),
            ('invalid transfer task config: resolve database CDC source engine: private token/SQL', 'source_engine_resolution_failed'),
            ('invalid transfer task config: resolve database CDC target engine: private token/SQL', 'target_engine_resolution_failed'),
            ('invalid transfer task config: database CDC target engine "postgresql" does not declare atomic monotonic upsert/delete partitioned_table_change_apply', 'target_apply_unsupported'),
            ('invalid transfer task config: continuous_kafka=private; database_cdc=private', 'configuration_rejected'),
            ('private connection password', 'unrecognized'),
            ({'nested': 'private'}, 'unrecognized'),
        ]
        for error, reason in cases:
            calls, report = [], {'tasks': [], 'executions': []}
            class User:
                def request(self, method, path, expected, body):
                    calls.append((method, path, expected))
                    return SimpleNamespace(status=400, payload={'error': error, 'error_code': 'private', 'token': 'secret'})
            with self.subTest(reason=reason), self.assertRaises(S.SuiteError) as caught:
                M.prepare_task(User(), object(), 9, 10, 'cdc', 'native-1', report, lambda: None)
            self.assertEqual(M.failure_diagnostics(caught.exception), {'failure_kind': 'http_unexpected_status', 'failure_http_status': 400})
            self.assertEqual(len(calls), 1)
            self.assertEqual(calls[0], ('POST', M.TASKS, (201, 400)))
            self.assertEqual(report['tasks'], [])
            self.assertEqual(report['task_create_diagnostic'], dict(task_kind='cdc', source_engine_id=9,
                target_engine_id=9, http_status=400, reason=reason))
            self.assertNotIn('private', json.dumps(report)); self.assertNotIn('secret', json.dumps(report))

    def test_readiness_failure_prevents_cdc_creation_and_does_not_restart_scan_budget(self):
        for case in ('transfer-engine', 'transfer-capture-unknown'):
            report, engines = {}, iter((9, 10))
            workflow = SimpleNamespace(register=lambda _: next(engines), engine=lambda _: self.fail('scan consumed budget'))
            controls = SimpleNamespace(run_id='native-1', descriptor=lambda _: {})
            with self.subTest(case=case), patch.object(M.time, 'monotonic', side_effect=[20, 200]), \
                    patch.object(M.MANAGER, 'wait_for_meta_scan') as scan, patch.object(M, 'prepare_task') as prepare:
                with self.assertRaises(S.SuiteError):
                    M.run_transfer(object(), workflow, controls, case, report, lambda: None)
                self.assertEqual(scan.call_args.args[2], 200)
                self.assertEqual(report['stage'], 'transfer_engine_readiness')
                prepare.assert_not_called()

    def test_non_cdc_create_rejection_keeps_exact_bindings_and_never_claims_cdc_reason(self):
        diagnostic = M.task_create_rejection({'error': 'invalid transfer task config: resolve database CDC source engine: engine 10 is unavailable'},
                                             'kafka', 9, 10)
        self.assertEqual(diagnostic, dict(task_kind='kafka', source_engine_id=10, target_engine_id=9,
                                         http_status=400, reason='unrecognized'))

    def test_cdc_cases_use_task_execution_without_manager_grant_preparation(self):
        for case in ('transfer-engine', 'transfer-capture-unknown'):
            engines = iter((9, 10))
            workflow = SimpleNamespace(register=lambda descriptor: next(engines),
                engine=lambda identity, **scope: dict(engine_type='postgresql' if identity == 9 else 'kafka',
                                             lifecycle_state='active', connection_status='online'))
            controls = SimpleNamespace(tenant=2, run_id='native-1', descriptor=lambda kind: {})
            user, report = object(), {'checks': []}
            with self.subTest(case=case), patch.object(M.MANAGER, 'wait_for_meta_scan'), \
                    patch.object(M.REGISTRATION, 'initialize_exact_record_read_grants',
                                 side_effect=AssertionError('CDC must not prepare an unused Manager Grant')) as grant, \
                    patch.object(M, 'prepare_task', side_effect=S.SuiteError('reached CDC execution')) as prepare:
                with self.assertRaisesRegex(S.SuiteError, 'reached CDC execution'):
                    M.run_transfer(user, workflow, controls, case, report, lambda: None)
                grant.assert_not_called()
                self.assertEqual(prepare.call_args.args[:6], (user, controls, 9, 10, 'cdc', 'native-1'))
                self.assertNotEqual(report['stage'], 'transfer_source_grant')
                config = M.TRANSFER.task_config('cdc', 9, 10)
                self.assertEqual(config['source']['locator'], 'addp://engine/9/path/public/continuous_source?type=table')
                self.assertEqual(config['load']['change_detection'], {'type': 'cdc', 'bootstrap': 'initial_snapshot'})

    def test_transfer_entry_does_not_construct_or_require_source_authorizer_client(self):
        for case in ('transfer-engine', 'transfer-capture-unknown', 'transfer-dlq-unknown'):
            with self.subTest(case=case), tempfile.TemporaryDirectory() as directory:
                environment = dict(ADDP_ONLINE_ARTIFACT_DIR=directory, ADDP_ONLINE_NATIVE_CASE=case,
                    ADDP_ONLINE_TEST_TENANT_ID='2', GATEWAY_URL='http://127.0.0.1:8000',
                    ADDP_ONLINE_CLEANUP_NATIVE_MANAGER_ACCESS_TOKEN='manager-token',
                    ADDP_ONLINE_CLEANUP_NATIVE_TRANSFER_ACCESS_TOKEN='transfer-token',
                    ADDP_ONLINE_CLEANUP_NATIVE_ENGINE_ACCESS_TOKEN='engine-token')
                deployment, controls = SimpleNamespace(run_id='native-1', close=lambda: None), SimpleNamespace(close=lambda: None)
                clients = [object(), object(), object()]
                with patch.dict(M.os.environ, environment, clear=True), \
                        patch.object(S, 'OwnedDeployment', return_value=deployment), \
                        patch.object(S, 'local_client', side_effect=clients) as local, \
                        patch.object(S, 'user_identity', side_effect=('33', '34', '35')), \
                        patch.object(M, 'EngineWorkflow'), patch.object(M, 'NativeControls', return_value=controls), \
                        patch.object(M, 'run_transfer', side_effect=S.SuiteError('stop at Transfer entry')) as run, \
                        patch('builtins.print'):
                    self.assertEqual(M.main(), 1)
                    run.assert_called_once()
                    self.assertEqual(local.call_count, 3)
                    self.assertEqual([c.args[1] for c in local.call_args_list], ['manager-token', 'transfer-token', 'engine-token'])
                    self.assertIs(run.call_args.args[0], clients[1])

    def test_manager_entry_keeps_its_separate_source_authorizer_client(self):
        for case in ('manager-success', 'manager-late-writer'):
            with self.subTest(case=case), tempfile.TemporaryDirectory() as directory:
                environment = dict(ADDP_ONLINE_ARTIFACT_DIR=directory, ADDP_ONLINE_NATIVE_CASE=case,
                    ADDP_ONLINE_TEST_TENANT_ID='2', GATEWAY_URL='http://127.0.0.1:8000',
                    ADDP_ONLINE_CLEANUP_NATIVE_MANAGER_ACCESS_TOKEN='manager-token',
                    ADDP_ONLINE_CLEANUP_NATIVE_TRANSFER_ACCESS_TOKEN='transfer-token',
                    ADDP_ONLINE_CLEANUP_NATIVE_ENGINE_ACCESS_TOKEN='engine-token',
                    ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='source-token')
                deployment, controls = SimpleNamespace(run_id='native-1', close=lambda: None), SimpleNamespace(close=lambda: None)
                clients = [SimpleNamespace(base_url='http://127.0.0.1:8000', token='manager-token', timeout=60),
                           object(), object(), object()]
                with patch.dict(M.os.environ, environment, clear=True), \
                        patch.object(S, 'OwnedDeployment', return_value=deployment), \
                        patch.object(S, 'local_client', side_effect=clients) as local, \
                        patch.object(S, 'user_identity', side_effect=('33', '34', '35')), \
                        patch.object(M, 'EngineWorkflow'), patch.object(M, 'NativeControls', return_value=controls), \
                        patch.object(M, 'run_manager', side_effect=S.SuiteError('stop at Manager entry')) as manager, \
                        patch.object(M, 'run_late_writer', side_effect=S.SuiteError('stop at Manager entry')) as late, \
                        patch('builtins.print'):
                    self.assertEqual(M.main(), 1)
                    run = late if case == 'manager-late-writer' else manager
                    run.assert_called_once()
                    domain = run.call_args.args[0]
                    self.assertIsInstance(domain, M.MANAGER.GatewayClient)
                    self.assertEqual((domain.base_url, domain.token, domain.timeout),
                                     ('http://127.0.0.1:8000', 'manager-token', 60))
                    self.assertIs(run.call_args.args[3], clients[3])
                    self.assertEqual(local.call_count, 4)
                    self.assertEqual(local.call_args.args[1:], ('source-token', 30))

    def test_manager_entries_preserve_binary_transport_and_source_refusals(self):
        import io
        import re
        import subprocess
        import threading
        import zipfile
        from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

        facts = M.MANAGER.MESH_SOURCE_FACTS['obj']['download_members']
        fixture = S.ROOT / 'business/fixtures/manager'
        script = (S.ROOT / 'business/scripts/online-manager-minio-fixture.sh').read_text()
        program = re.search(r"generate_fixtures\(\) \{\n  python3 [^\n]+ <<'PY'\n(.*?)\nPY", script, re.S).group(1)
        with tempfile.TemporaryDirectory() as generated:
            subprocess.run([sys.executable, '-', generated, str(fixture / 'addp_online_exchange_texture.png'), '1'],
                           input=program, text=True, capture_output=True, check=True, timeout=10)
            members = {name: (Path(generated) / 'obj' / name).read_bytes() for name in facts}
        glb = b'glTF\x02\x00\x00\x00\xff\x80'  # Transport bytes, not a GLB geometry oracle.
        observed, completed = [], []
        mode = 'good'

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                observed.append((self.path.split('?')[0], self.headers.get('Authorization')))
                status, content_type, raw = 200, 'application/json', b'{"items":[],"total":0}'
                if self.path.startswith('/api/v1/manager/downloads/file?'):
                    if mode == 'denied':
                        status, raw = 403, b'{"error_code":"manager.source.read_denied"}'
                    else:
                        archive = io.BytesIO()
                        with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as zipped:
                            for name, data in members.items():
                                zipped.writestr(name, data + b'x' if mode == 'changed_member' and name == 'model.mtl' else data)
                            if mode == 'unexpected_member': zipped.writestr('ungranted.txt', b'not allowed')
                        content_type, raw = 'application/zip', archive.getvalue()
                elif self.path.endswith('/content'):
                    content_type, raw = 'model/gltf-binary', glb
                elif mode == 'invalid_json':
                    raw = b'not JSON'
                self.send_response(status)
                self.send_header('Content-Type', content_type)
                self.send_header('Content-Length', str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)

            def log_message(self, *args): pass

        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            for case in ('manager-success', 'manager-object-unknown', 'manager-index-unknown', 'manager-late-writer'):
                for mode in ('good', 'changed_member', 'unexpected_member', 'denied', 'invalid_json'):
                    with self.subTest(case=case, mode=mode), tempfile.TemporaryDirectory() as directory:
                        observed.clear(); completed.clear()
                        environment = dict(ADDP_ONLINE_ARTIFACT_DIR=directory, ADDP_ONLINE_NATIVE_CASE=case,
                            ADDP_ONLINE_TEST_TENANT_ID='2', GATEWAY_URL=f'http://127.0.0.1:{server.server_port}',
                            ADDP_ONLINE_CLEANUP_NATIVE_MANAGER_ACCESS_TOKEN='manager-token',
                            ADDP_ONLINE_CLEANUP_NATIVE_TRANSFER_ACCESS_TOKEN='transfer-token',
                            ADDP_ONLINE_CLEANUP_NATIVE_ENGINE_ACCESS_TOKEN='engine-token',
                            ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='source-token')
                        def probe(client, *args):
                            args[-2]['stage'] = 'manager_model_prepare'
                            M.MANAGER.verify_source_download(client, dict(format='obj', locator='owned-source', download_members=facts))
                            response = client.request('GET', '/api/v1/manager/model_3d_glb/7/content', (200,))
                            self.assertEqual(response.raw, glb)
                            self.assertEqual(response.headers['Content-Type'], 'model/gltf-binary')
                            self.assertEqual(client.request('GET', '/api/v1/manager/tasks?page=1', (200,)).payload,
                                             {'items': [], 'total': 0})
                            self.assertEqual(client.timeout, 60)
                            completed.append(True)
                            raise S.SuiteError('bounded transport probe complete')
                        with patch.dict(M.os.environ, environment, clear=True), \
                                patch.object(S, 'OwnedDeployment', return_value=SimpleNamespace(run_id='native-1', close=lambda: None)), \
                                patch.object(S, 'user_identity', side_effect=('33', '34', '35')), \
                                patch.object(M, 'EngineWorkflow'), \
                                patch.object(M, 'NativeControls', return_value=SimpleNamespace(close=lambda: None)), \
                                patch.object(M, 'run_manager', side_effect=probe), \
                                patch.object(M, 'run_late_writer', side_effect=probe), patch('builtins.print'):
                            self.assertEqual(M.main(), 1)
                        report = json.loads((Path(directory) / 'cleanup-native-effects.json').read_text())
                        self.assertEqual(report['control_cleanup'], 'passed')
                        self.assertEqual(report['checks'], [])  # This transport probe is never Hosted acceptance.
                        self.assertEqual(report['error_type'], 'SuiteError')
                        self.assertEqual(report['failure_kind'], 'http_unexpected_status' if mode == 'denied'
                                         else 'http_invalid_json' if mode == 'invalid_json' else 'assertion_failed')
                        self.assertEqual(completed, [True] if mode == 'good' else [])
                        self.assertEqual(len(observed), 3 if mode in ('good', 'invalid_json') else 1)
                        self.assertTrue(all(auth == 'Bearer manager-token' for _, auth in observed))
                        if mode == 'denied':
                            self.assertEqual(report['failure_http_status'], 403)
                            self.assertEqual(report['model_prepare_diagnostic'],
                                dict(operation='manager_source_download', error_code='unrecognized'))
                        else:
                            self.assertNotIn('model_prepare_diagnostic', report)
        finally:
            server.shutdown(); server.server_close(); thread.join(timeout=5)
        self.assertFalse(thread.is_alive())

    def test_runtime_wait_retains_only_last_scoped_observation_with_original_budget(self):
        for status in ('pending', 'failed', 'timeout', 'cancelled', 'success'):
            report, snapshots, calls = {}, [], []
            values = iter(({'execution_id': ID, 'task_id': 7, 'status': 'pending'},
                           {'execution_id': ID, 'task_id': 7, 'status': status,
                            'error_details': {'category': 'connection_failed', 'code': 'lease_expired',
                                              'message': 'secret connection/SQL', 'raw_response': 'private'},
                            'metadata': {'stop_reason': 'worker_shutdown', 'recovery_reason': 'execution_failed',
                                         'metadata_scan': {'status': 'failed', 'error_code': 'scan_submission_failed',
                                                           'message': 'secret source'},
                                         'execution_config': {'password': 'private'}}}))
            class User:
                def request(self, method, path, expected):
                    calls.append((method, path, expected))
                    return SimpleNamespace(payload=next(values))
            with self.subTest(status=status), patch.object(S.time, 'monotonic', side_effect=[0, 0, 1, 120]), \
                    patch.object(S.time, 'sleep') as sleep:
                with self.assertRaisesRegex(S.SuiteError, 'native Transfer runtime did not start'):
                    M.wait_transfer_runtime(User(), 7, ID, report, lambda: snapshots.append(copy.deepcopy(report)))
                self.assertEqual(calls, [('GET', f'/api/v1/transfer/executions/{ID}', (200,))] * 2)
                self.assertEqual(sleep.call_args_list, [unittest.mock.call(.2)] * 2)
                self.assertEqual(report['runtime_start_diagnostic'], {
                    'execution_id': ID, 'task_id': 7, 'observation': 'observed', 'status': status,
                    'error_details': {'category': 'connection_failed', 'code': 'lease_expired'},
                    'metadata': {'stop_reason': 'worker_shutdown', 'recovery_reason': 'execution_failed',
                                 'metadata_scan': {'status': 'failed', 'error_code': 'scan_submission_failed'}}})
                self.assertEqual(snapshots, [report])
                self.assertNotIn('secret', json.dumps(report))
                self.assertNotIn('private', json.dumps(report))

    def test_runtime_wait_running_succeeds_without_failure_fields_or_extra_requests(self):
        reads = []
        class User:
            def request(self, method, path, expected):
                reads.append(path)
                return SimpleNamespace(payload={'execution_id': ID, 'task_id': 7,
                                                 'status': 'pending' if len(reads) == 1 else 'running'})
        report = {}
        with patch.object(S.time, 'monotonic', side_effect=[0, 0, 1]), patch.object(S.time, 'sleep'):
            M.wait_transfer_runtime(User(), 7, ID, report, lambda: self.fail('success must not publish failure diagnostics'))
        self.assertEqual(len(reads), 2)
        self.assertEqual(report, {})

    def test_runtime_wait_read_failure_preserves_prior_observation_without_an_extra_get(self):
        for observed in (False, True):
            calls, report = [], {}
            class User:
                def request(self, method, path, expected):
                    calls.append(path)
                    if observed and len(calls) == 1:
                        return SimpleNamespace(payload={'execution_id': ID, 'task_id': 7, 'status': 'pending'})
                    raise S.SuiteError('GET /private returned HTTP 503 (secret)')
            with self.subTest(observed=observed), patch.object(S.time, 'monotonic', return_value=0), \
                    patch.object(S.time, 'sleep'):
                with self.assertRaises(S.SuiteError) as caught:
                    M.wait_transfer_runtime(User(), 7, ID, report, lambda: None)
                self.assertEqual(M.failure_diagnostics(caught.exception),
                                 {'failure_kind': 'http_unexpected_status', 'failure_http_status': 503})
                self.assertEqual(len(calls), 2 if observed else 1)
                diagnostic = report['runtime_start_diagnostic']
                self.assertEqual(diagnostic['observation'], 'observed' if observed else 'not_observed')
                self.assertEqual(diagnostic.get('status'), 'pending' if observed else None)
                self.assertNotIn('secret', json.dumps(report))

    def test_runtime_observation_uses_closed_enums_and_rejects_foreign_or_malformed_identity(self):
        payload = {'execution_id': ID, 'task_id': 7, 'status': 'secret',
                   'error_details': {'category': ['private'], 'code': {'password': 'secret'},
                                     'error_code': 'private.driver.detail', 'message': 'private'},
                   'metadata': {'stop_reason': 'private', 'recovery_reason': 'private',
                                'metadata_scan': {'status': 'private', 'error_code': 'private'},
                                'continuous': {'diagnostics': {'error': 'private'}}}}
        safe = M.transfer_runtime_observation(payload, 7, ID)
        self.assertEqual(safe, {'execution_id': ID, 'task_id': 7, 'observation': 'observed',
                               'status': 'unrecognized',
                               'error_details': {'category': 'unrecognized', 'code': 'unrecognized',
                                                 'error_code': 'unrecognized'},
                               'metadata': {'stop_reason': 'unrecognized', 'recovery_reason': 'unrecognized',
                                            'metadata_scan': {'status': 'unrecognized', 'error_code': 'unrecognized'}}})
        for invalid in (None, [], {'execution_id': str(uuid.uuid4()), 'task_id': 7, 'status': 'running'},
                        {'execution_id': ID, 'task_id': True, 'status': 'running'},
                        {'execution_id': ID, 'task_id': 8, 'status': 'running'}):
            with self.subTest(invalid=invalid):
                diagnostic = M.transfer_runtime_observation(invalid, 7, ID)
                self.assertIn(diagnostic['observation'], ('invalid_response', 'identity_mismatch'))
                self.assertNotIn('status', diagnostic)
                self.assertEqual(set(diagnostic), {'execution_id', 'task_id', 'observation'})

    def test_initial_physical_probe_observation_preserves_boolean_and_never_exports_output(self):
        for code, outcome in [(0, 'matched'), (1, 'command_failed'), (2, 'query_failed'),
                              (3, 'rows_mismatch'), (127, 'command_failed')]:
            observed = []
            with self.subTest(code=code), patch.object(M.TRANSFER.subprocess, 'run',
                    return_value=SimpleNamespace(returncode=code, stdout=b'private row', stderr=b'private connection')) as run:
                self.assertEqual(M.TRANSFER.physical('verify-cdc-initial', observe=observed.append), code == 0)
                self.assertEqual(observed, [outcome])
                self.assertEqual(run.call_args.kwargs['timeout'], 20)
                self.assertTrue(run.call_args.kwargs['capture_output'])
                self.assertEqual(M.TRANSFER.physical('verify-cdc-mutated'), code == 0)
                self.assertNotIn('private', json.dumps(observed))
        with patch.object(M.TRANSFER.subprocess, 'run') as run, self.assertRaises(M.TRANSFER.SuiteError):
            M.TRANSFER.physical('mutate-cdc', observe=lambda _: None)
        run.assert_not_called()

    def test_initial_rows_keep_120s_predicate_and_last_bound_runtime_within_remaining_budget(self):
        for kind in ('cdc', 'kafka'):
            report, snapshots, reads, timeouts = {}, [], [], []
            values = iter(('pending', 'failed'))
            class Reader:
                def request(self, method, path, expected):
                    reads.append((method, path, expected))
                    return SimpleNamespace(payload=dict(execution_id=ID, task_id=7, status=next(values),
                        error_details={'category': 'connection_failed', 'code': 'lease_expired', 'message': 'private'},
                        metadata={'stop_reason': 'execution_failed', 'password': 'private'}, token='private'))
            def reader(url, token, timeout):
                self.assertEqual((url, token), ('http://127.0.0.1:8000', 'private-token'))
                timeouts.append(timeout); return Reader()
            user = SimpleNamespace(base_url='http://127.0.0.1:8000', token='private-token', timeout=60)
            with self.subTest(kind=kind), patch.object(S.time, 'monotonic', side_effect=[0, 0, 0, 5, 70, 90, 120]), \
                    patch.object(S.time, 'sleep') as sleep, patch.object(S, 'local_client', side_effect=reader), \
                    patch.object(M.TRANSFER, 'physical', side_effect=lambda _, observe: observe('rows_mismatch') or False) as physical:
                with self.assertRaisesRegex(S.SuiteError, 'target rows were not committed'):
                    M.wait_initial_records(user, 7, ID, kind, report, lambda: snapshots.append(copy.deepcopy(report)))
                self.assertEqual([call.args for call in physical.call_args_list], [('verify-' + kind + '-initial',)] * 2)
                self.assertEqual(reads, [('GET', f'/api/v1/transfer/executions/{ID}', (200,))] * 2)
                self.assertEqual(timeouts, [60, 30])
                self.assertEqual(sleep.call_args_list, [unittest.mock.call(.2)] * 2)
                self.assertEqual(report['initial_records_diagnostic'], dict(task_kind=kind, physical_probe='rows_mismatch',
                    runtime=dict(execution_id=ID, task_id=7, observation='observed', status='failed',
                        error_details={'category': 'connection_failed', 'code': 'lease_expired'},
                        metadata={'stop_reason': 'execution_failed'})))
                self.assertEqual(snapshots, [report]); self.assertNotIn('private', json.dumps(report))

    def test_initial_rows_success_has_no_failure_fields_and_never_reads_after_match(self):
        for first in ('matched', 'failed_execution', 'read_failed', 'foreign_execution'):
            report = {}
            class Reader:
                def request(self, *_):
                    if first == 'read_failed': raise S.SuiteError('GET /private returned HTTP 503 (secret)')
                    return SimpleNamespace(payload=dict(execution_id=str(uuid.uuid4()) if first == 'foreign_execution' else ID,
                        task_id=7, status='failed', error_details={'message': 'private'}))
            user = SimpleNamespace(base_url='http://127.0.0.1:8000', token='private-token', timeout=60)
            values = iter([True] if first == 'matched' else [False, True])
            def physical(_, *, observe):
                value = next(values); observe('matched' if value else 'rows_mismatch'); return value
            with self.subTest(first=first), patch.object(S.time, 'monotonic', return_value=0), \
                    patch.object(S.time, 'sleep'), patch.object(S, 'local_client', return_value=Reader()) as client, \
                    patch.object(M.TRANSFER, 'physical', side_effect=physical):
                M.wait_initial_records(user, 7, ID, 'cdc', report, lambda: self.fail('success must not publish diagnostics'))
                self.assertEqual(client.call_count, 0 if first == 'matched' else 1)
                self.assertEqual(report, {})

    def test_initial_rows_expired_window_does_not_add_a_final_diagnostic_read(self):
        report = {}
        self.assertEqual(S.wait_for.__defaults__, (120,))
        with patch.object(S.time, 'monotonic', side_effect=[0, 0, 0, 120, 120]), \
                patch.object(S.time, 'sleep'), patch.object(S, 'local_client') as client, \
                patch.object(M.TRANSFER, 'physical', side_effect=lambda _, observe: observe('rows_mismatch') or False):
            with self.assertRaises(S.SuiteError): M.wait_initial_records(object(), 7, ID, 'cdc', report, lambda: None)
            client.assert_not_called()
            self.assertEqual(report['initial_records_diagnostic'], dict(task_kind='cdc', physical_probe='rows_mismatch',
                runtime=dict(execution_id=ID, task_id=7, observation='not_observed')))

    def test_initial_rows_retains_observed_state_when_later_read_fails_or_window_expires(self):
        for ending in ('read_failed', 'expired'):
            report, reads = {}, []
            class Reader:
                def request(self, *_):
                    reads.append(True)
                    if len(reads) == 2: raise S.SuiteError('GET /private returned HTTP 503 (secret)')
                    return SimpleNamespace(payload=dict(execution_id=ID, task_id=7, status='failed', error_details={}, metadata={}))
            def wait(read, predicate, message, **kwargs):
                self.assertEqual(kwargs, {})
                self.assertFalse(predicate(read())); self.assertFalse(predicate(read())); raise S.SuiteError(message)
            user = SimpleNamespace(base_url='http://127.0.0.1:8000', token='private-token', timeout=60)
            with self.subTest(ending=ending), patch.object(S.time, 'monotonic', side_effect=[0, 1, 120 if ending == 'expired' else 2]), \
                    patch.object(S, 'wait_for', side_effect=wait), patch.object(S, 'local_client', return_value=Reader()), \
                    patch.object(M.TRANSFER, 'physical', side_effect=lambda _, observe: observe('rows_mismatch') or False):
                with self.assertRaises(S.SuiteError): M.wait_initial_records(user, 7, ID, 'cdc', report, lambda: None)
                diagnostic = report['initial_records_diagnostic']
                self.assertEqual(diagnostic['runtime']['status'], 'failed')
                self.assertEqual(len(reads), 1 if ending == 'expired' else 2)
                self.assertEqual('runtime_read_failure' in diagnostic, ending == 'read_failed')
                self.assertNotIn('private', json.dumps(report)); self.assertNotIn('secret', json.dumps(report))

    def test_initial_rows_failure_keeps_safe_read_errors_or_rejects_foreign_identity(self):
        for failure in ('read', 'identity', 'shape', 'physical'):
            report, calls = {}, []
            class Reader:
                def request(self, method, path, expected):
                    calls.append((method, path, expected))
                    if failure == 'read': raise S.SuiteError('GET /private returned HTTP 503 (secret)')
                    return SimpleNamespace(payload=[] if failure == 'shape' else
                        dict(execution_id=str(uuid.uuid4()), task_id=7, status='failed', password='private'))
            def wait(read, predicate, message, **kwargs):
                self.assertEqual(kwargs, {}); self.assertIs(predicate, bool)
                self.assertFalse(predicate(read())); raise S.SuiteError(message)
            def physical(_, *, observe):
                if failure == 'physical': raise RuntimeError('private connection details')
                observe('rows_mismatch'); return False
            user = SimpleNamespace(base_url='http://127.0.0.1:8000', token='private-token', timeout=60)
            with self.subTest(failure=failure), patch.object(S.time, 'monotonic', return_value=0), \
                    patch.object(S, 'wait_for', side_effect=wait), patch.object(S, 'local_client', return_value=Reader()), \
                    patch.object(M.TRANSFER, 'physical', side_effect=physical):
                with self.assertRaises(Exception): M.wait_initial_records(user, 7, ID, 'cdc', report, lambda: None)
                diagnostic = report['initial_records_diagnostic']; runtime = diagnostic['runtime']
                self.assertEqual(calls, [] if failure == 'physical' else [('GET', f'/api/v1/transfer/executions/{ID}', (200,))])
                self.assertEqual(runtime['observation'], {'read': 'not_observed', 'identity': 'identity_mismatch',
                    'shape': 'invalid_response', 'physical': 'not_observed'}[failure])
                self.assertNotIn('status', runtime)
                if failure == 'read': self.assertEqual(diagnostic['runtime_read_failure'],
                    {'failure_kind': 'http_unexpected_status', 'failure_http_status': 503})
                self.assertNotIn('private', json.dumps(report)); self.assertNotIn('secret', json.dumps(report))

    def test_failure_diagnostics_publish_only_closed_categories_and_http_status(self):
        cases = [
            (S.SuiteError('POST /private?token=secret returned HTTP 403 (private_code)'),
             {'failure_kind': 'http_unexpected_status', 'failure_http_status': 403}),
            (S.SuiteError('DELETE /private returned HTTP 503 (secret)'),
             {'failure_kind': 'http_unexpected_status', 'failure_http_status': 503}),
            (S.SuiteError('GET /private returned invalid JSON'), {'failure_kind': 'http_invalid_json'}),
            (S.SuiteError('GET /private transport failed: secret connection'), {'failure_kind': 'http_transport'}),
            (S.SuiteError('GET /private returned HTTP 700 (secret)'), {'failure_kind': 'assertion_failed'}),
            (S.SuiteError('private assertion and response'), {'failure_kind': 'assertion_failed'}),
            (RuntimeError('POST /private returned HTTP 403 (secret)'), {'failure_kind': 'unexpected_exception'}),
        ]
        for error, expected in cases:
            with self.subTest(expected=expected):
                result = M.failure_diagnostics(error)
                self.assertEqual(result, expected)
                self.assertNotIn('private', json.dumps(result))
                self.assertNotIn('secret', json.dumps(result))

    def test_model_prepare_diagnostic_retains_only_closed_operations_and_codes(self):
        operations = {
            '/api/v1/meta/engines/17/items': 'meta_items_read',
            '/api/v1/manager/model_3d_glb?page=1&item_fingerprint=private': 'manager_model_results_read',
            '/api/v1/manager/tasks?task_type=private': 'manager_model_tasks_read',
            '/api/v1/manager/downloads/file?locator=private&token=secret': 'manager_source_download',
            '/private?password=secret': 'unrecognized',
        }
        for path, operation in operations.items():
            for code in ('permission_denied', 'security_protection_required', 'source_authorization_required',
                         'source_composition_incomplete', 'engine_unavailable', 'authorization_service_unavailable',
                         'download_bundle_invalid', 'download_source_changed', 'unknown', 'private_secret_code'):
                with self.subTest(operation=operation, code=code):
                    error = M.MANAGER.SuiteError(f'GET {path} returned HTTP 403 ({code})')
                    diagnostic = M.model_prepare_diagnostics(error)
                    self.assertEqual(diagnostic, dict(operation=operation,
                        error_code='unrecognized' if code == 'private_secret_code' else code))
                    self.assertNotIn('private', json.dumps(diagnostic))
                    self.assertNotIn('secret', json.dumps(diagnostic))
        for error in (RuntimeError('GET /api/v1/manager/tasks returned HTTP 403 (permission_denied)'),
                      S.SuiteError('private assertion'), S.SuiteError('GET /private returned invalid JSON'),
                      S.SuiteError('GET /private transport failed: secret')):
            self.assertIsNone(M.model_prepare_diagnostics(error))

    def test_transfer_scan_failure_checkpoint_precedes_task_creation(self):
        for case in ('transfer-engine', 'transfer-capture-unknown', 'transfer-dlq-unknown'):
            engines = iter((9, 10))
            workflow = SimpleNamespace(register=lambda descriptor: next(engines))
            controls = SimpleNamespace(tenant=2, run_id='native-1', descriptor=lambda kind: {})
            report, stages = {'checks': []}, []
            with self.subTest(case=case), \
                    patch.object(M.MANAGER, 'wait_for_meta_scan', side_effect=S.SuiteError('private')), \
                    patch.object(M.REGISTRATION, 'initialize_exact_record_read_grants') as grant, \
                    patch.object(M, 'prepare_task') as prepare:
                with self.assertRaises(S.SuiteError):
                    M.run_transfer(object(), workflow, controls, case, report,
                                   lambda: stages.append(report['stage']))
                self.assertEqual(report['stage'], 'transfer_source_scan')
                self.assertEqual(stages[-1], report['stage'])
                prepare.assert_not_called()
                grant.assert_not_called()

    def test_dlq_case_uses_kafka_input_and_never_prepares_unrelated_cdc_grant(self):
        engines = iter((9, 10))
        workflow = SimpleNamespace(register=lambda descriptor: next(engines))
        controls = SimpleNamespace(tenant=2, run_id='native-1', descriptor=lambda kind: {})
        user = object()
        report = {'checks': []}
        with patch.object(M.MANAGER, 'wait_for_meta_scan') as scan, \
                patch.object(M.REGISTRATION, 'initialize_exact_record_read_grants',
                             side_effect=AssertionError('DLQ must not require an unrelated CDC Grant')) as grant, \
                patch.object(M, 'prepare_task', return_value=(7, ID)) as prepare, \
                patch.object(M, 'run_dlq_unknown') as unknown:
            checkpoint = lambda: None
            M.run_transfer(user, workflow, controls, 'transfer-dlq-unknown', report, checkpoint)
            scan.assert_called_once()
            self.assertEqual(scan.call_args.args[:2], (user, 9))
            grant.assert_not_called()
            prepare.assert_called_once_with(user, controls, 9, 10, 'kafka', 'native-1', report, checkpoint)
            unknown.assert_called_once_with(user, controls, 7, report, checkpoint)
        config = M.TRANSFER.task_config('kafka', 9, 10)
        self.assertEqual(config['source']['locator'], 'addp://engine/10/path/continuous_events?type=topic')
        self.assertEqual(config['target']['parent_locator'], 'addp://engine/9/path/public?type=schema')
        self.assertNotIn('continuous_source', json.dumps(config))
        self.assertNotIn('manager.data_item.read', S.CONTRACT['native_permissions']['transfer'])

    def test_transfer_task_preparation_records_the_failing_call_before_effects(self):
        stages = {'create': 'transfer_task_create', 'start': 'transfer_task_start',
                  'runtime': 'transfer_runtime_start', 'initial': 'transfer_initial_records',
                  'invalid': 'transfer_dlq_records', 'dead_letter_read': 'transfer_dlq_records',
                  'topic': 'transfer_dlq_native_verify', 'pause': 'transfer_task_pause',
                  'quiescence': 'transfer_task_pause', None: 'transfer_task_pause'}
        for failed, expected_stage in stages.items():
            report, checkpoints = {'tasks': [], 'executions': []}, []
            def effect(name):
                if name == failed: raise S.SuiteError('private failure')
            class User:
                def request(self, method, path, expected, body=None):
                    if method == 'POST' and path == M.TASKS: effect('create'); value = {'id': 7, 'tenant_id': 2, 'enabled': False}
                    elif method == 'POST' and path.endswith('/start'): effect('start'); value = {'execution_id': ID}
                    elif method == 'GET' and '/executions/' in path:
                        effect('runtime'); value = {'execution_id': ID, 'task_id': 7, 'status': 'running'}
                    elif method == 'GET' and '/dead-letters?' in path: effect('dead_letter_read'); value = {'total': 1}
                    elif method == 'POST' and path.endswith('/pause'): effect('pause'); value = {}
                    elif method == 'GET' and path == M.TASKS + '/7': value = {'desired_state': 'paused', 'status': 'idle'}
                    else: raise AssertionError('unexpected preparation request')
                    return SimpleNamespace(status=201 if path == M.TASKS else 200, payload=value)
            class Controls:
                tenant = 2
                def produce_initial(self): effect('initial')
                def produce_invalid(self): effect('invalid')
                def topic(self, task, *, present): effect('topic')
                def require_invocations_stopped(self, task): effect('quiescence')
            def wait(read, predicate, message, **kwargs):
                value = read(); M.require(predicate(value), message); return value
            with self.subTest(failed=failed), patch.object(S, 'wait_for', side_effect=wait), \
                    patch.object(M.TRANSFER, 'physical', return_value=True):
                def run():
                    return M.prepare_task(User(), Controls(), 9, 10, 'kafka', 'native-1', report,
                                          lambda: checkpoints.append(report['stage']))
                if failed is None: self.assertEqual(run(), (7, ID))
                else:
                    with self.assertRaises(S.SuiteError): run()
                self.assertEqual(report['stage'], expected_stage)
                self.assertEqual(checkpoints[-1], expected_stage)
                if failed == 'runtime':
                    self.assertEqual(report['runtime_start_diagnostic'], {
                        'execution_id': ID, 'task_id': 7, 'observation': 'not_observed'})
                else:
                    self.assertNotIn('runtime_start_diagnostic', report)

    def test_proxy_evidence_requires_actual_upstream_response_and_single_forward(self):
        valid = dict(run_id='native-1', key=ID, forwarded=1, upstream_response=True, blocked_replays=0)
        M.assert_proxy(valid, 'native-1', ID)
        for update in ({'run_id': 'foreign'}, {'key': str(uuid.uuid4())}, {'forwarded': 0}, {'forwarded': 2},
                       {'forwarded': True}, {'upstream_response': False}, {'upstream_response': 1},
                       {'blocked_replays': -1}, {'blocked_replays': False}, {'raw_response': 'sensitive'}):
            value = dict(valid, **update)
            with self.subTest(update=update), self.assertRaises(S.SuiteError): M.assert_proxy(value, 'native-1', ID)

    def test_engine_begin_checkpoints_each_original_request_without_retry_or_private_text(self):
        stages = ['engine_before_assessment_read', 'engine_assessment_create', 'engine_assessment_read',
                  'engine_before_delete_read', 'engine_delete_confirm']
        for failed, expected_stage in enumerate(stages):
            calls, report = [], {'engines': [9], 'requests': [], 'stage': 'transfer_engine_deletion'}
            class User:
                def request(self, method, path, expected, body=None):
                    calls.append((method, path, expected, body))
                    if len(calls) - 1 == failed:
                        raise S.SuiteError(f'{method} {path} returned HTTP 400 (private error)')
                    engine = dict(id=9, tenant_id=2, engine_type='postgresql', lifecycle_state='active',
                                  version=3, name='private confirmation')
                    if method == 'POST':
                        self.assessment = body['request_id']
                        return SimpleNamespace(payload={'assessment_id': self.assessment})
                    if '/deletion-assessments/' in path:
                        return SimpleNamespace(payload=dict(status='success', task=dict(context={'engine_id': 9},
                            requested_by=17, action='scan', expected_modules=list(S.CONTRACT['cleanup_owners_by_cause']['engine.deleting']))))
                    return SimpleNamespace(payload=engine)
            with self.subTest(failed=failed), patch.object(S, 'user_identity', return_value='17'):
                workflow = M.EngineWorkflow(User(), 2, report, lambda: None); workflow.owned.add(9)
                with self.assertRaises(S.SuiteError) as caught: workflow.begin(9)
                self.assertEqual(report['stage'], expected_stage)
                self.assertEqual(len(calls), failed + 1)
                self.assertEqual(M.failure_diagnostics(caught.exception), {'failure_kind': 'http_unexpected_status', 'failure_http_status': 400})
                self.assertNotIn('private', json.dumps(report))

    def test_validation_observation_never_substitutes_for_execute_and_caps_http_budget(self):
        for status in ('pending', 'running', 'success', 'failed', 'timeout', 'cancelled'):
            value = dict(task_id=ID, status=status, task=dict(task_id=ID, execution_id=ID,
                context={'engine_id': 9}, requested_by=17, action='scan',
                expected_modules=list(S.CONTRACT['cleanup_owners_by_cause']['engine.deleting'])), module_executions=[])
            user = SimpleNamespace(base_url='http://127.0.0.1:8000', token='private', timeout=60)
            with self.subTest(status=status), patch.object(S, 'user_identity', return_value='17'), \
                    patch.object(M.time, 'monotonic', return_value=100), \
                    patch.object(S, 'local_client', return_value=user) as client, \
                    patch.object(M.PROTOCOL, 'task', return_value=value) as read:
                workflow = M.EngineWorkflow(user, 2, {}, lambda: None)
                workflow.engine = lambda _: dict(lifecycle_state='deleting', deletion_scan_task_id=ID,
                    deletion_error='cleanup_scan_incomplete_or_failed', connection_info={'password': 'private'})
                self.assertIsNone(workflow.execution(9, deadline=107))
                client.assert_called_once_with(user.base_url, user.token, 7)
                read.assert_called_once_with(user, ID)
                self.assertEqual(workflow.cleanup_observation['task_phase'], 'validation')
                self.assertEqual(workflow.cleanup_observation['task']['status'], status)
                self.assertNotIn('private', json.dumps(workflow.cleanup_observation))

    def test_validation_diagnostic_preserves_last_state_on_read_failure_and_expiry(self):
        user = SimpleNamespace(base_url='http://127.0.0.1:8000', token='private', timeout=60)
        value = dict(task_id=ID, status='failed', task=dict(task_id=ID, execution_id=ID,
            context={'engine_id': 9}, requested_by=17, action='scan',
            expected_modules=list(S.CONTRACT['cleanup_owners_by_cause']['engine.deleting'])), module_executions=[])
        report = {}
        def wait(read, predicate, message, timeout):
            self.assertEqual(timeout, 360)
            self.assertFalse(predicate(read()))
            self.assertFalse(predicate(read()))
            raise S.SuiteError(message)
        with patch.object(S, 'user_identity', return_value='17'), \
                patch.object(M.time, 'monotonic', return_value=100), \
                patch.object(S, 'local_client', return_value=user), \
                patch.object(M.PROTOCOL, 'task', side_effect=[value, S.SuiteError('GET /private returned HTTP 503 (private)')]) as read, \
                patch.object(S, 'wait_for', side_effect=wait):
            workflow = M.EngineWorkflow(user, 2, report, lambda: None)
            workflow.engine = lambda _: dict(lifecycle_state='deleting', deletion_scan_task_id=ID)
            with self.assertRaises(S.SuiteError): workflow.settled(9, success=False)
            diagnostic = report['engine_cleanup_diagnostic']
            self.assertEqual(diagnostic['task']['status'], 'failed')
            self.assertEqual(diagnostic['task_read_failure'], {'failure_kind': 'http_unexpected_status', 'failure_http_status': 503})
            self.assertNotIn('private', json.dumps(report))
            self.assertIsNone(workflow.execution(9, deadline=100))
            self.assertEqual(read.call_count, 2)
            self.assertEqual(diagnostic['task']['status'], 'failed')

    def test_invalid_validation_reference_and_unsafe_engine_values_cannot_expand_reads(self):
        for reference in (None, 'private', True, {}, ''):
            with self.subTest(reference=reference), patch.object(S, 'user_identity', return_value='17'), \
                    patch.object(M.PROTOCOL, 'task') as read:
                workflow = M.EngineWorkflow(object(), 2, {}, lambda: None)
                workflow.engine = lambda _: dict(deletion_scan_task_id=reference,
                    lifecycle_state='private', deletion_error='private')
                self.assertIsNone(workflow.execution(9, deadline=float('inf')))
                read.assert_not_called()
                self.assertEqual(workflow.cleanup_observation['engine']['deletion_error'], 'unrecognized')
                self.assertNotIn('private', json.dumps(workflow.cleanup_observation))

    def test_healthy_execute_reads_original_task_only_and_publishes_no_failure_field(self):
        value = dict(task_id=ID, status='success', task=dict(task_id=ID, execution_id=ID,
            context={'engine_id': 9}, requested_by=17, action='execute',
            expected_modules=list(S.CONTRACT['cleanup_owners_by_cause']['engine.deleting'])), module_executions=[])
        report, reads = {}, []
        def wait(read, predicate, message, timeout=120):
            reads.append(timeout)
            value = read()
            self.assertTrue(predicate(value))
            return value
        with patch.object(S, 'user_identity', return_value='17'), \
                patch.object(M.PROTOCOL, 'task', return_value=value) as task, \
                patch.object(S, 'local_client') as clone, patch.object(S, 'wait_for', side_effect=wait):
            user = object(); workflow = M.EngineWorkflow(user, 2, report, lambda: None)
            workflow.engine = lambda _: dict(lifecycle_state='deleted', deletion_scan_task_id=str(uuid.uuid4()),
                deletion_execute_task_id=ID)
            self.assertIs(workflow.settled(9, success=True), value)
            task.assert_called_once_with(user, ID); clone.assert_not_called()
            self.assertEqual(reads, [360, 120])
            self.assertNotIn('engine_cleanup_diagnostic', report)

    def test_engine_task_observation_requires_explicit_exact_phase(self):
        value = dict(task_id=ID, status='failed', task=dict(task_id=ID, execution_id=ID,
            context={'engine_id': 9}, requested_by=17, action='execute',
            expected_modules=list(S.CONTRACT['cleanup_owners_by_cause']['engine.deleting'])), module_executions=[])
        self.assertEqual(M.engine_task_observation(value, ID, 9, '17', action='execute')['observation'], 'observed')
        self.assertEqual(M.engine_task_observation(value, ID, 9, '17', action='scan')['observation'], 'identity_mismatch')
        with self.assertRaises(S.SuiteError): M.engine_task_observation(value, ID, 9, '17', action='private')

    def test_engine_execution_separates_engine_lookup_from_parent_read(self):
        for failed in ('engine', 'parent'):
            report, calls = {}, []
            def engine(identity):
                calls.append('engine')
                if failed == 'engine': raise S.SuiteError('GET /private returned HTTP 400 (secret)')
                return {'deletion_execute_task_id': ID}
            def parent(user, identity):
                calls.append('parent')
                raise S.SuiteError('GET /private returned HTTP 400 (secret)')
            with self.subTest(failed=failed), patch.object(S, 'user_identity', return_value='17'), \
                    patch.object(M.PROTOCOL, 'task', side_effect=parent):
                workflow = M.EngineWorkflow(object(), 2, report, lambda: None)
                workflow.engine = engine
                with self.assertRaises(S.SuiteError): workflow.execution(9, deadline=0)
                self.assertEqual(report['stage'], 'engine_cleanup_engine_read' if failed == 'engine' else 'engine_cleanup_task_read')
                self.assertEqual(calls, ['engine'] if failed == 'engine' else ['engine', 'parent'])
                self.assertNotIn('secret', json.dumps(report))

    def test_engine_workflow_keeps_name_version_assessment_and_cause_owner_identity(self):
        for mutation in ('none', 'wrong_assessment', 'missing_owner', 'extra_owner', 'duplicate_owner', 'tenant_owners', 'running', 'changed_version', 'foreign_engine', 'wrong_creator'):
            calls = []; reads = 0; deleted = False
            class Client:
                def request(self, method, path, expected, body=None):
                    nonlocal reads, deleted
                    calls.append((method, path, body))
                    engine = dict(id=9, tenant_id=2, engine_type='minio', lifecycle_state='active', version=3, name='owned')
                    if mutation == 'foreign_engine': engine['tenant_id'] = 3
                    if method == 'POST' and path == M.ENGINE: return SimpleNamespace(payload=engine)
                    if method == 'GET' and path == M.ENGINE + '/9':
                        reads += 1
                        if mutation == 'changed_version' and reads >= 2: engine['version'] = 4
                        return SimpleNamespace(payload=engine)
                    if method == 'POST' and path.endswith('/deletion-assessments'):
                        self.assessment = body['request_id']
                        return SimpleNamespace(payload={'assessment_id': ID if mutation == 'wrong_assessment' else self.assessment})
                    if method == 'GET' and '/deletion-assessments/' in path:
                        owners = list(S.CONTRACT['cleanup_owners_by_cause']['engine.deleting'])
                        if mutation == 'missing_owner': owners.pop()
                        elif mutation == 'extra_owner': owners.append('asset')
                        elif mutation == 'duplicate_owner': owners.append(owners[0])
                        elif mutation == 'tenant_owners': owners = list(S.OWNERS)
                        return SimpleNamespace(payload=dict(status='failed' if mutation == 'running' else 'success',
                            task=dict(context={'engine_id': 9}, requested_by=99 if mutation == 'wrong_creator' else 17,
                                      action='scan', expected_modules=owners)))
                    if method == 'DELETE':
                        deleted = True
                        self_body = dict(version=3, assessment_id=self.assessment, confirmation_token='owned', external_artifact_policy='delete')
                        if body != self_body: raise AssertionError('deletion lost frozen confirmation')
                        engine['lifecycle_state'] = 'deleting'
                        return SimpleNamespace(payload={'engine': engine})
                    raise AssertionError('unexpected native workflow API')
            report = dict(engines=[], requests=[])
            with patch.object(S, 'user_identity', return_value='17'):
                workflow = M.EngineWorkflow(Client(), 2, report, lambda: None)
                def run():
                    engine = workflow.register(dict(engine_type='minio', engine_origin='general'))
                    workflow.begin(engine)
                if mutation == 'none': run(); self.assertTrue(deleted)
                else:
                    with self.subTest(mutation=mutation), self.assertRaises(S.SuiteError): run()
                    self.assertFalse(deleted)
                with self.assertRaises(S.SuiteError): workflow.engine(999)

    def test_direct_delete_unknown_requires_physical_absence_retained_facts_and_no_replay(self):
        for mutation in ('none', 'no_source', 'no_response', 'topic_present', 'index_erased', 'operation_missing', 'late_write', 'resent', 'wrong_refusal_code', 'legacy_refusal'):
            calls = []; snapshots = 0; observations = 0
            class User:
                def request(self, method, path, expected):
                    calls.append((method, path, expected))
                    if method == 'POST':
                        if expected != (409,): raise AssertionError('producer refusal must be a conflict')
                        payload = {'error': 'safe localized message', 'error_code': 'transfer_cleanup_unresolved'}
                        if mutation == 'wrong_refusal_code': payload['error_code'] = 'unrelated_conflict'
                        if mutation == 'legacy_refusal': payload = {'error': 'start continuous execution: transfer external cleanup operation is unresolved'}
                        return SimpleNamespace(payload=payload)
                    return SimpleNamespace(payload={})
            class Controls:
                tenant, run_id = 2, 'native-1'
                def transfer_snapshot(self, task):
                    nonlocal snapshots
                    snapshots += 1
                    value = dict(task_exists=True, dead_letters=2, unknown_operations=0 if snapshots == 1 else 1)
                    if mutation == 'no_source' and snapshots == 1: value['dead_letters'] = 0
                    if mutation == 'index_erased' and snapshots >= 2: value['dead_letters'] = 0
                    if mutation == 'operation_missing': value['unknown_operations'] = 0
                    if mutation == 'late_write' and snapshots >= 3: value['dead_letters'] = 3
                    return value
                def target_rows(self, kind):
                    if kind != 'kafka': raise AssertionError('wrong native target')
                def arm(self, kind, *, topic):
                    if kind != 'dlq' or topic != '__addp_dlq.2.7': raise AssertionError('wrong task target')
                    return ID
                def fault(self, kind, key):
                    nonlocal observations
                    observations += 1
                    return dict(run_id='native-1', key=ID, forwarded=2 if mutation == 'resent' and observations > 1 else 1,
                                upstream_response=mutation != 'no_response', blocked_replays=0)
                def topic(self, task, *, present):
                    if present or mutation == 'topic_present': raise S.SuiteError('native topic still exists')
            report = dict(checks=[])
            if mutation == 'none':
                M.run_dlq_unknown(User(), Controls(), 7, report, lambda: None)
                self.assertEqual(report['entry'], 'transfer_task_delete')
                self.assertEqual(report['producer_refusal_http_status'], 409)
                self.assertEqual(report['producer_refusal_error_code'], 'transfer_cleanup_unresolved')
                self.assertEqual([x[:2] for x in calls], [('DELETE', M.TASKS + '/7'), ('DELETE', M.TASKS + '/7'), ('POST', M.TASKS + '/7/start')])
            else:
                with self.subTest(mutation=mutation), self.assertRaises(S.SuiteError): M.run_dlq_unknown(User(), Controls(), 7, report, lambda: None)
                self.assertNotIn('new_producer_rejected', report['checks'])
                self.assertEqual(report['stage'], {
                    'no_source': 'direct_task_delete_unknown', 'no_response': 'transfer_dlq_effect_probe',
                    'topic_present': 'transfer_dlq_native_absence', 'index_erased': 'transfer_dlq_hold_verify',
                    'operation_missing': 'transfer_dlq_hold_verify', 'late_write': 'transfer_dlq_hold_after_refusal',
                    'resent': 'transfer_dlq_no_replay', 'wrong_refusal_code': 'transfer_dlq_producer_refusal',
                    'legacy_refusal': 'transfer_dlq_producer_refusal',
                }[mutation])

    def test_manifest_rejects_foreign_runs_extra_fields_and_nonprivate_files(self):
        for mutation in ('foreign', 'case', 'extra', 'permissions', 'symlink'):
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'cleanup-native-fixture.json'
                value = dict(schema='addp.cleanup-native-fixture/v1', run_id='native-1', case='manager-success',
                             descriptors={}, source_bucket='source', proxies={}, native={})
                if mutation == 'foreign': value['run_id'] = 'other'
                if mutation == 'case': value['case'] = 'transfer-engine'
                if mutation == 'extra': value['raw_data'] = 'untrusted'
                path.write_text(json.dumps(value)); path.chmod(0o644 if mutation == 'permissions' else 0o600)
                if mutation == 'symlink':
                    target = path.with_suffix('.other'); path.rename(target); path.symlink_to(target)
                with self.subTest(mutation=mutation), self.assertRaises(S.SuiteError):
                    M.NativeControls(SimpleNamespace(secret=Path(directory), run_id='native-1'), 2, 'manager-success')

    def test_bounded_engine_observation_keeps_user_identity_and_window(self):
        user = SimpleNamespace(base_url='http://127.0.0.1:8000', token='private-user-token', timeout=60)
        for times, accepted, timeout in (([149, 149.4], True, 1), ([150], False, None),
                                         ([149, 150.1], False, 1)):
            reader = SimpleNamespace(request=lambda *args: SimpleNamespace(payload={'id': 9, 'tenant_id': 2}))
            with self.subTest(times=times), patch.object(S, 'user_identity', return_value='17'), \
                    patch.object(S, 'local_client', return_value=reader) as local, \
                    patch.object(M.time, 'monotonic', side_effect=times):
                workflow = M.EngineWorkflow(user, 2, {}, lambda: None)
                workflow.owned.add(9)
                if accepted:
                    self.assertEqual(workflow.engine(9, deadline=150), {'id': 9, 'tenant_id': 2})
                else:
                    with self.assertRaises(S.SuiteError): workflow.engine(9, deadline=150)
                if timeout is None: local.assert_not_called()
                else: local.assert_called_once_with(user.base_url, user.token, timeout)

    def test_manager_waits_for_registered_minio_before_model_preparation(self):
        observed, sleeps, deadlines = [], [], []
        ready = False
        model = SimpleNamespace(result_id=7)
        def engine(identity, **scope):
            nonlocal ready
            self.assertEqual(identity, 9)
            observed.append(identity)
            ready = len(observed) == 2
            return dict(engine_type='minio', lifecycle_state='active',
                        connection_status='online' if ready else 'unknown', password='private')
        def prepare(*args):
            self.assertTrue(ready, 'source Engine has not reached available before preparation')
            return model
        workflow = SimpleNamespace(register=lambda _: 9, engine=engine)
        controls = SimpleNamespace(tenant=2, manifest={'source_bucket': 'source'},
            source_objects=lambda: None, descriptor=lambda _: {})
        with patch.object(M.time, 'monotonic', side_effect=[5, 6, 7]), \
                patch.object(M.time, 'sleep', side_effect=lambda seconds: sleeps.append(seconds)), \
                patch.object(M, 'wait_manager_projection_ready') as protection, \
                patch.object(M.MANAGER, 'wait_for_meta_scan', side_effect=lambda user, identity, deadline: deadlines.append(deadline)), \
                patch.object(M.GRANTS, 'ModelSourceAuthorization', return_value=SimpleNamespace(grant=lambda _: None)), \
                patch.object(M.MANAGER, 'find_fixture_item', return_value={'fingerprint': M.hashlib.sha256(b'9:source/model3d/obj/model.obj').hexdigest()}), \
                patch.object(M.MANAGER, 'prepare_model_fixture', side_effect=prepare):
            report = {'principals': {'manager': '18'}}
            self.assertEqual(M.prepare_manager(object(), workflow, controls, object(), report, lambda: None), (9, model))
        self.assertEqual(deadlines, [185])
        self.assertEqual(protection.call_args.args[2], deadlines[0])
        self.assertEqual(observed, [9, 9])
        self.assertEqual(sleeps, [1])
        self.assertNotIn('private', json.dumps(report))

    def test_manager_expired_preparation_budget_cannot_grant_or_generate(self):
        workflow = SimpleNamespace(register=lambda _: 9, engine=lambda _, **scope: self.fail('expired budget must not read Engine'))
        controls = SimpleNamespace(tenant=2, manifest={'source_bucket': 'source'},
            source_objects=lambda: None, descriptor=lambda _: {})
        report = {'principals': {'manager': '18'}}
        with patch.object(M.time, 'monotonic', side_effect=[0, 180]), \
                patch.object(M, 'wait_manager_projection_ready'), \
                patch.object(M.MANAGER, 'wait_for_meta_scan') as scan, \
                patch.object(M.GRANTS, 'ModelSourceAuthorization') as authorization, \
                patch.object(M.MANAGER, 'prepare_model_fixture') as prepare:
            with self.assertRaises(S.SuiteError):
                M.prepare_manager(object(), workflow, controls, object(), report, lambda: None)
        scan.assert_called_once()
        self.assertEqual(scan.call_args.args[2], 180)
        authorization.assert_not_called()
        prepare.assert_not_called()
        self.assertEqual(report['stage'], 'manager_engine_readiness')
        self.assertEqual(report['engine_readiness_diagnostic'], {'engines': [
            {'engine_id': 9, 'engine_type': 'minio', 'observation': 'not_observed'}]})

    def test_manager_first_scan_requires_protection_readiness_within_the_shared_deadline(self):
        user = SimpleNamespace(base_url='http://127.0.0.1:8000', token='original-manager', timeout=60)
        controls = SimpleNamespace(tenant=2, manifest={'source_bucket': 'source'},
            source_objects=lambda: None, descriptor=lambda _: {})
        fingerprint = M.hashlib.sha256(b'9:source/model3d/obj/model.obj').hexdigest()
        workflow = SimpleNamespace(register=lambda _: 9)
        model = SimpleNamespace(result_id=7)
        for case in ('ready_after_pending', 'permission_denied', 'late_ready', 'still_pending'):
            report, requests, now = {'principals': {'manager': '18'}}, [], 0
            def observe(method, path, accepted):
                nonlocal now
                self.assertEqual((method, path, accepted), ('GET',
                    f'/api/v1/manager/model_3d_glb?item_fingerprint={fingerprint}&page=1&page_size=1', (200, 403)))
                requests.append('read')
                if case == 'permission_denied':
                    return SimpleNamespace(status=403, payload={'error_code': 'permission_denied'})
                if case == 'late_ready':
                    now = 180
                if len(requests) == 1 and case in {'ready_after_pending', 'still_pending'}:
                    now = 179.5 if case == 'still_pending' else 178.5
                    return SimpleNamespace(status=403, payload={'error_code': 'security_protection_required'})
                return SimpleNamespace(status=200, payload={'total': 0})
            def sleep(seconds):
                nonlocal now
                now += seconds
            def scan(actor, engine, deadline):
                self.assertEqual((actor, engine, deadline), (user, 9, 180))
                self.assertEqual(requests, ['read', 'read'])
                requests.append('scan')
            with self.subTest(case=case), patch.object(M.time, 'monotonic', side_effect=lambda: now), \
                 patch.object(M.time, 'sleep', side_effect=sleep), \
                 patch.object(M.MANAGER, 'GatewayClient', return_value=SimpleNamespace(request=observe)) as reader, \
                 patch.object(M.MANAGER, 'wait_for_meta_scan', side_effect=scan) as scan_mock, \
                 patch.object(M, 'wait_native_engines') as engine_ready, \
                 patch.object(M.GRANTS, 'ModelSourceAuthorization') as grant, \
                 patch.object(M.MANAGER, 'find_fixture_item', return_value={'fingerprint': fingerprint}), \
                 patch.object(M.MANAGER, 'prepare_model_fixture', return_value=model):
                if case == 'ready_after_pending':
                    self.assertEqual(M.prepare_manager(user, workflow, controls, None, report, lambda: None), (9, model))
                    scan_mock.assert_called_once()
                    self.assertEqual(engine_ready.call_args.args[2], 180)
                    self.assertEqual([c.args for c in reader.call_args_list],
                        [(user.base_url, user.token, 60), (user.base_url, user.token, .5)])
                else:
                    with self.assertRaises(S.SuiteError):
                        M.prepare_manager(user, workflow, controls, None, report, lambda: None)
                    self.assertEqual(requests, ['read'])
                    self.assertEqual(report['stage'], 'manager_protection_readiness')
                    scan_mock.assert_not_called()
                    engine_ready.assert_not_called()
                    grant.assert_not_called()

    def test_manager_grants_before_consumer_catalog_and_validates_same_source(self):
        import hashlib
        name = 'source/model3d/obj/model.obj'
        fingerprint = hashlib.sha256(f'9:{name}'.encode()).hexdigest()
        refs = [{'target': M.GRANTS.file_target(9, path, 'minio'), 'role': role,
                 'required': True, 'primary': role == 'model'} for path, role in (
            (name, 'model'), ('source/model3d/obj/texture.png', 'texture'),
            ('source/model3d/obj/model.mtl', 'material_library'))]
        refs.sort(key=lambda ref: json.dumps(ref['target'], ensure_ascii=False, separators=(',', ':')))
        baseline_facts = dict(schema_version='meta.data_item_authorization_facts/v1', engine_id='9',
            item_fingerprint=fingerprint, full_name=name, item_type='object', basis_state='complete',
            boundary={'layout': 'single', 'file_composition': {'format': 'obj', 'refs': refs}},
            boundary_hash='a' * 64, observed_at='2026-11-03T00:00:00Z')
        for mutation in ('none', 'foreign_facts', 'facts_unavailable', 'grant_denied',
                         'different_visible_fingerprint', 'consumer_still_hidden'):
            calls, issued, prepared, checkpoints = [], [], [], []
            facts = copy.deepcopy(baseline_facts)
            if mutation == 'foreign_facts': facts['engine_id'] = '10'
            class User:
                def __init__(self, principal, permissions):
                    self.principal, self.permissions = principal, permissions
                def request(self, method, path, expected, body=None):
                    calls.append((self.principal, method, path))
                    if path == '/api/v1/system/auth/context':
                        return SimpleNamespace(payload=dict(principal={'id': self.principal, 'type': 'user'},
                            context={'type': 'tenant', 'tenant_id': '2'}, token={'type': 'first_party_access_token'},
                            authorization={'role_assignments': [{'role_key': 'online.native',
                                'permissions': sorted(self.permissions)}]}))
                    if path.endswith('/authorization-facts'):
                        self_check.assertEqual(self.principal, '17')
                        self_check.assertEqual(path, f'/api/v1/system/engines/9/data-items/{fingerprint}/authorization-facts')
                        if mutation == 'facts_unavailable': raise M.GRANTS.SuiteError('GET /private returned HTTP 503 (private)')
                        return SimpleNamespace(payload=facts)
                    if method == 'POST' and path.endswith('/access_grants'):
                        self_check.assertEqual(self.principal, '17')
                        self_check.assertEqual(body['item_fingerprint'], fingerprint)
                        self_check.assertEqual(body['recipient_id'], '18')
                        if mutation == 'grant_denied': raise M.GRANTS.SuiteError('POST /private returned HTTP 403 (private)')
                        issued.append(body)
                        return SimpleNamespace(payload={**body, 'engine_id': '9', 'approval_mode': 'independent',
                            'expires_at': None, 'boundary_hash': facts['boundary_hash'],
                            'file_composition': facts['boundary']['file_composition'],
                            'operator_principal_id': '17', 'revocation': None})
                    if path == '/api/v1/meta/engines/9/items':
                        self_check.assertEqual(self.principal, '18')
                        # Current Meta visibility hides an ordinary consumer's
                        # ungranted source. A path/fingerprint is only a lookup key.
                        visible = issued and mutation != 'consumer_still_hidden'
                        return SimpleNamespace(payload=[dict(id=7, item_type='object', full_name=name,
                            fingerprint='b' * 64 if mutation == 'different_visible_fingerprint' else fingerprint)] if visible else [])
                    raise AssertionError((method, path))
            self_check = self
            manager = User('18', set(S.CONTRACT['native_permissions']['manager']))
            issuer = User('17', M.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS)
            report = {'principals': {'manager': '18'}}
            controls = SimpleNamespace(tenant=2, manifest={'source_bucket': 'source'},
                source_objects=lambda: None, descriptor=lambda kind: {})
            workflow = SimpleNamespace(register=lambda descriptor: 9, engine=lambda _, **scope: dict(
                engine_type='minio', lifecycle_state='active', connection_status='online'))
            model = SimpleNamespace(result_id=7)
            def prepare(*args):
                self.assertTrue(issued)
                prepared.append(1)
                return model
            with self.subTest(mutation=mutation), patch.object(M.MANAGER, 'wait_for_meta_scan'), \
                    patch.object(M, 'wait_manager_projection_ready'), \
                    patch.object(M.MANAGER, 'prepare_model_fixture', side_effect=prepare):
                invoke = lambda: M.prepare_manager(manager, workflow, controls, issuer, report,
                    lambda: checkpoints.append(report['stage']))
                if mutation == 'none':
                    self.assertEqual(invoke(), (9, model))
                    grant_pos = next(i for i, call in enumerate(calls) if call[1:] == ('POST', '/api/v1/system/engines/9/access_grants'))
                    browse_pos = next(i for i, call in enumerate(calls) if call[2] == '/api/v1/meta/engines/9/items')
                    self.assertLess(grant_pos, browse_pos)
                    self.assertEqual(prepared, [1])
                    self.assertEqual(checkpoints, ['manager_native_prepare', 'manager_protection_readiness', 'manager_source_scan', 'manager_engine_readiness',
                        'manager_source_grant', 'manager_source_identity', 'manager_model_prepare'])
                else:
                    with self.assertRaises((S.SuiteError, M.GRANTS.SuiteError, M.MANAGER.SuiteError)):
                        invoke()
                    self.assertEqual(prepared, [])
                    self.assertEqual(report['stage'], 'manager_source_grant' if mutation in
                        ('foreign_facts', 'facts_unavailable', 'grant_denied') else 'manager_source_identity')
                    if mutation in ('foreign_facts', 'facts_unavailable', 'grant_denied'):
                        self.assertFalse(issued)
                        self.assertFalse(any(call[2] == '/api/v1/meta/engines/9/items' for call in calls))

    def test_manager_prepares_grant_before_source_download_and_rejects_missing_native_evidence(self):
        for case in ('manager-success', 'manager-object-unknown', 'manager-index-unknown'):
            for mutation in ('none', 'missing_effect', 'lost_hold', 'dirty_audit', 'changed_after_refusal'):
                if mutation == 'dirty_audit' and case != 'manager-success': continue
                if mutation in {'missing_effect', 'lost_hold', 'changed_after_refusal'} and case == 'manager-success': continue
                granted, reads, indexed = [], [], []
                model = SimpleNamespace(item={'fingerprint': 'fp'}, result_id=7, execution_id=ID, locator='addp://engine/9/path/source/model.obj?type=object')
                class User:
                    def request(self, method, path, expected, body=None):
                        return SimpleNamespace(payload={})
                class Controls:
                    tenant, run_id = 2, 'native-1'
                    manifest = {'source_bucket': 'source', 'native': {'meili': {'index': 'content'}}}
                    def source_objects(self): pass
                    def descriptor(self, kind): return {}
                    def glb_storage(self, result): return 'ref'
                    def object(self, ref, *, present):
                        reads.append(present); return '/manager/tenant_2/out.glb'
                    def index_count(self, engine):
                        indexed.append(engine); return 2 if len(indexed) == 1 else 0
                    def manager_snapshot(self, engine, result):
                        current = dict(artifact=dict(status='ready', deleted=False, source_engine_id=9,
                            lifecycle={'input_preparations': [{'cleaned': False}]}),
                            artifact_unknown=0, index_unknown=0, target_holds=0, index_targets=0)
                        snapshots.append(1)
                        if len(snapshots) > 1:
                            if case == 'manager-success':
                                current['artifact']['deleted'] = True
                                current['artifact']['lifecycle']['input_preparations'][0]['cleaned'] = mutation != 'dirty_audit'
                            else:
                                current['artifact_unknown' if case == 'manager-object-unknown' else 'index_unknown'] = 1
                                current['target_holds'] = current['index_targets'] = 0 if mutation == 'lost_hold' else 1
                        if mutation == 'changed_after_refusal' and len(snapshots) > 2: current['artifact']['status'] = 'building'
                        return current
                    def arm(self, kind, **target): return ID
                    def fault(self, kind, key):
                        return dict(run_id='native-1', key=ID, forwarded=1, upstream_response=mutation != 'missing_effect', blocked_replays=0)
                workflow = SimpleNamespace(user=User(), register=lambda descriptor: 9, begin=lambda engine: None,
                    settled=lambda engine, **kwargs: {'task': {'based_on_scan': ID}},
                    engine=lambda engine, **scope: {'version': 3, 'name': 'owned', 'engine_type': 'minio',
                        'lifecycle_state': 'active', 'connection_status': 'online'})
                def prepare(*args):
                    if not granted: raise AssertionError('source download occurred before exact Grant')
                    return model
                def wait(read, predicate, message, **kwargs):
                    value = read(); M.require(predicate(value), message); return value
                snapshots = []
                with patch.object(M.MANAGER, 'wait_for_meta_scan'), patch.object(M.MANAGER, 'find_fixture_item', return_value={'fingerprint': M.hashlib.sha256(b'9:source/model3d/obj/model.obj').hexdigest()}), \
                     patch.object(M, 'wait_manager_projection_ready'), \
                     patch.object(M.MANAGER, 'prepare_model_fixture', side_effect=prepare), patch.object(M.MANAGER, 'generate_model_glb'), \
                     patch.object(M.GRANTS, 'ModelSourceAuthorization', return_value=SimpleNamespace(grant=lambda value: granted.append(value))), \
                     patch.object(M, 'professional', return_value={'summary': {'deleted_physical_artifacts': 1}}), patch.object(S, 'wait_for', side_effect=wait):
                    report = {'checks': [], 'principals': {'manager': '18'}}
                    if mutation == 'none':
                        M.run_manager(User(), workflow, Controls(), User(), case, report, lambda: None)
                        self.assertEqual(reads, [True, False])
                        self.assertEqual(report['entry'], 'engine_deletion')
                    else:
                        with self.subTest(case=case, mutation=mutation), self.assertRaises(S.SuiteError):
                            M.run_manager(User(), workflow, Controls(), User(), case, report, lambda: None)

    def test_manager_inventory_failure_identifies_the_first_unproven_probe(self):
        model = SimpleNamespace(result_id=7, execution_id=ID)
        probes = [
            ('storage_reference', 'glb_storage', S.SuiteError('native GLB target missing'), 'storage_reference_missing'),
            ('native_object', 'object', S.SuiteError('artifact is outside this Tenant output scope'), 'storage_reference_outside_scope'),
            ('index_projection', 'index_count', S.SuiteError('native index count is incomplete'), 'index_response_incomplete'),
            ('index_projection', 'index_count', HTTPError('http://127.0.0.1/private', 503, 'raw response', {}, None), 'http_error'),
            ('index_projection', 'index_count', None, 'index_not_observed'),
            ('artifact_state', 'manager_snapshot', S.SuiteError('private Hosted control failed'), 'private_read_failed'),
            ('artifact_state', 'manager_snapshot', RuntimeError('private raw response'), 'unrecognized'),
        ]
        for probe, method, error, reason in probes:
            calls = []
            def read(name, result):
                def observe(*args, **kwargs):
                    calls.append(name)
                    if name == method and error is not None:
                        raise error
                    return 0 if name == method == 'index_count' else result
                return observe
            controls = SimpleNamespace(tenant=2, glb_storage=read('glb_storage', 'ref'),
                object=read('object', '/output'), index_count=read('index_count', 1),
                manager_snapshot=read('manager_snapshot', {'artifact': {'status': 'ready', 'deleted': False}}))
            workflow = SimpleNamespace(begin=lambda *_: self.fail('inventory failure must prevent Engine deletion'))
            def observe_once(read, predicate, message):
                self.assertEqual(message, 'real source indexing never completed')
                S.require(predicate(read()), message)
            with self.subTest(probe=probe, reason=reason), \
                 patch.object(M, 'prepare_manager', return_value=(9, model)), \
                 patch.object(M.MANAGER, 'generate_model_glb'), \
                 patch.object(S, 'wait_for', side_effect=observe_once):
                report = {'checks': []}
                with self.assertRaises(type(error) if error else S.SuiteError) as raised:
                    M.run_manager(None, workflow, controls, None, 'manager-success', report, lambda: None)
                if error is not None:
                    self.assertIs(raised.exception, error)
                self.assertEqual(report['stage'], 'manager_native_inventory')
                diagnostic = {'probe': probe, 'reason': reason}
                if isinstance(error, HTTPError):
                    diagnostic['http_status'] = 503
                    error.close()
                self.assertEqual(report['manager_inventory_diagnostic'], diagnostic)
                self.assertEqual(report['checks'], [])
                expected = ['glb_storage', 'object', 'index_count', 'manager_snapshot']
                self.assertEqual(calls, expected[:expected.index(method) + 1])

    def test_transfer_engine_preserves_definition_and_dlq_until_explicit_delete(self):
        real_prepare = M.prepare_task
        for case in ('transfer-engine', 'transfer-capture-unknown'):
            for mutation in ('none', 'wrong_enabled_state', 'dlq_erased', 'native_capture_remains', 'business_rows_changed',
                             'delete_http400', 'professional_http400', 'foreign_task', 'foreign_tenant',
                             'missing_enabled', 'created_enabled_true'):
                if case == 'transfer-capture-unknown' and mutation == 'dlq_erased': continue
                if case != 'transfer-engine' and mutation in ('delete_http400', 'professional_http400'): continue
                if case != 'transfer-capture-unknown' and mutation == 'created_enabled_true': continue
                calls, deleted, snapshots = [], [], []
                definitions = {}
                class User:
                    def request(self, method, path, expected, body=None):
                        calls.append((method, path))
                        if method == 'POST' and path == M.TASKS:
                            self_enabled = body['enabled']
                            if self_enabled is not False or body.get('schedule'):
                                raise AssertionError('continuous fixture must not enable task-owned scheduling')
                            definitions[7] = dict(id=7, tenant_id=2,
                                                  enabled=mutation == 'created_enabled_true')
                            return SimpleNamespace(status=201, payload=dict(definitions[7]))
                        if method == 'POST' and path.endswith('/start'):
                            return SimpleNamespace(status=200 if expected == (200,) else 409,
                                                   payload={'execution_id': ID})
                        if method == 'DELETE':
                            if mutation == 'delete_http400': raise S.SuiteError('DELETE /private returned HTTP 400 (secret)')
                            deleted.append(8)
                        if method == 'GET' and path.endswith('/8') and expected == (404,): return SimpleNamespace(payload={})
                        task = int(path.rsplit('/', 1)[-1]) if method == 'GET' else 7
                        value = dict(definitions.get(task, dict(id=task, tenant_id=2, enabled=False)))
                        if mutation == 'wrong_enabled_state': value['enabled'] = True
                        if mutation == 'foreign_task': value['id'] = task + 100
                        if mutation == 'foreign_tenant': value['tenant_id'] = 3
                        if mutation == 'missing_enabled': value.pop('enabled')
                        return SimpleNamespace(payload=value)
                class Controls:
                    tenant, run_id = 2, 'native-1'
                    def descriptor(self, kind): return {'kind': kind}
                    def capture(self, task, *, present, connector_only=False):
                        if not present and mutation == 'native_capture_remains': raise S.SuiteError('native connector still exists')
                        return '/connectors/owned-generation'
                    def arm(self, kind, *, path):
                        if kind != 'capture' or path != '/connectors/owned-generation': raise AssertionError('wrong generation')
                        return ID
                    def fault(self, kind, key):
                        return dict(run_id='native-1', key=ID, forwarded=1, upstream_response=True, blocked_replays=0)
                    def require_invocations_stopped(self, task): pass
                    def topic(self, task, *, present):
                        calls.append(('NATIVE_TOPIC', present))
                        if present and mutation == 'dlq_erased': raise S.SuiteError('Engine erased DLQ')
                    def transfer_snapshot(self, task):
                        snapshots.append(task)
                        return dict(task_exists=True, dead_letters=0 if deleted else 2, unknown_operations=1)
                    def target_rows(self, kind):
                        if mutation == 'business_rows_changed': raise S.SuiteError('target rows changed')
                workflow = SimpleNamespace(user=User(), register=lambda descriptor: 9 if descriptor['kind'] == 'pg' else 10,
                    engine=lambda identity, **scope: dict(engine_type='postgresql' if identity == 9 else 'kafka',
                                                 lifecycle_state='active', connection_status='online'),
                    begin=lambda engine: calls.append(('ENGINE_BEGIN', engine)), settled=lambda *args, **kwargs: {})
                def prepare(user, controls, pg, kafka, kind, run_id, report, checkpoint):
                    if case == 'transfer-capture-unknown':
                        return real_prepare(user, controls, pg, kafka, kind, run_id, report, checkpoint)
                    task = 7 if kind == 'cdc' else 8
                    definitions[task] = dict(id=task, tenant_id=2, enabled=False)
                    report['tasks'].append(task)
                    return task, ID
                def professional(*_):
                    if mutation == 'professional_http400': raise S.SuiteError('GET /private returned HTTP 400 (secret)')
                    return {}
                with patch.object(M.MANAGER, 'wait_for_meta_scan'), patch.object(M.REGISTRATION, 'initialize_exact_record_read_grants'), \
                     patch.object(M, 'prepare_task', side_effect=prepare), patch.object(M, 'professional', side_effect=professional), \
                     patch.object(M, 'wait_transfer_runtime'), patch.object(M, 'wait_initial_records'), patch.object(M, 'wait_paused'):
                    report = dict(checks=[], tasks=[], executions=[])
                    if mutation == 'none':
                        M.run_transfer(User(), workflow, Controls(), case, report, lambda: None)
                        self.assertEqual(set(report['checks']), set(S.CONTRACT['native_case_checks'][case]))
                        if case == 'transfer-engine':
                            self.assertLess(calls.index(('ENGINE_BEGIN', 9)), calls.index(('DELETE', M.TASKS + '/8')))
                            self.assertLess(calls.index(('NATIVE_TOPIC', True)), calls.index(('DELETE', M.TASKS + '/8')))
                        else:
                            self.assertFalse(deleted)
                            self.assertIs(definitions[7]['enabled'], False)
                            self.assertLess(calls.index(('POST', M.TASKS)), calls.index(('ENGINE_BEGIN', 9)))
                    else:
                        with self.subTest(case=case, mutation=mutation), self.assertRaises(S.SuiteError):
                            M.run_transfer(User(), workflow, Controls(), case, report, lambda: None)
                        expected = dict(wrong_enabled_state='transfer_retained_task_read', dlq_erased='transfer_retained_dlq_verify',
                            native_capture_remains='transfer_capture_cleanup_verify', business_rows_changed='transfer_business_rows_verify',
                            delete_http400='transfer_task_delete', professional_http400='transfer_professional_result_read',
                            foreign_task='transfer_retained_task_read', foreign_tenant='transfer_retained_task_read',
                            missing_enabled='transfer_retained_task_read', created_enabled_true='transfer_task_create')
                        self.assertEqual(report['stage'], expected[mutation])

    def test_native_object_consumes_manager_json_reference_without_uri_decoding(self):
        key = f'tenant_2/model3d-quick-view/executions/{ID}/model +%2Fé.glb'
        reference = json.dumps(dict(type='object', provider='addp_object_storage', bucket='manager', object=key))
        calls = []
        def stat(bucket, actual):
            calls.append((bucket, actual)); return SimpleNamespace(size=9)
        sdk = SimpleNamespace(Minio=lambda *args, **kwargs: SimpleNamespace(stat_object=stat))
        controls = M.NativeControls.__new__(M.NativeControls)
        controls.tenant = 2
        controls.manifest = {'native': {'minio': {'endpoint': '127.0.0.1:1', 'access_key': 'private', 'secret_key': 'private'}}}
        with patch.dict(sys.modules, {'minio': sdk, 'minio.error': SimpleNamespace(S3Error=RuntimeError)}), \
                patch.object(M.GRANTS, 'object_ref', wraps=M.GRANTS.object_ref) as parse:
            actual = controls.object(reference, present=True)
        self.assertEqual(calls, [('manager', key)])
        self.assertEqual(actual, f'/manager/tenant_2/model3d-quick-view/executions/{ID}/model%20%2B%252F%C3%A9.glb')
        parse.assert_called_once_with(reference)

    def test_native_object_rejects_locator_and_foreign_or_malformed_refs_before_sdk(self):
        reference = dict(type='object', provider='addp_object_storage', bucket='manager', object='tenant_2/output.glb')
        values = ['addp-infra://minio/manager/tenant_2/output.glb?type=object', 'not JSON', '[]',
                  *[json.dumps({**reference, **change}) for change in (
                      {'type': 'file'}, {'provider': 'foreign'}, {'bucket': 'business'},
                      {'object': 'tenant_3/output.glb'}, {'object': 'tenant_2/../output.glb'},
                      {'object': 'tenant_2//output.glb'}, {'object': None}, {'url': 'private'})],
                  json.dumps({k: v for k, v in reference.items() if k != 'provider'})]
        controls = M.NativeControls.__new__(M.NativeControls)
        controls.tenant = 2
        controls.manifest = {}
        def connect(*args, **kwargs): self.fail('invalid reference reached the native SDK')
        sdk = SimpleNamespace(Minio=connect)
        for value in values:
            with self.subTest(reference=value), \
                    patch.dict(sys.modules, {'minio': sdk, 'minio.error': SimpleNamespace(S3Error=RuntimeError)}), \
                    self.assertRaises(S.SuiteError):
                controls.object(value, present=False)

    def test_native_object_never_treats_auth_failure_or_missing_bucket_as_absence(self):
        class S3Error(Exception):
            def __init__(self, code): self.code = code
        reference = dict(type='object', provider='addp_object_storage', bucket='manager', object='tenant_2/output.glb')
        for code in ('present', 'NoSuchKey', 'NoSuchBucket', 'AccessDenied'):
            def stat(*_):
                if code != 'present': raise S3Error(code)
                return SimpleNamespace(size=9)
            sdk = SimpleNamespace(Minio=lambda *args, **kwargs: SimpleNamespace(stat_object=stat))
            controls = M.NativeControls.__new__(M.NativeControls)
            controls.tenant = 2
            controls.manifest = {'native': {'minio': {'endpoint': '127.0.0.1:1', 'access_key': 'private', 'secret_key': 'private'}}}
            with patch.dict(sys.modules, {'minio': sdk, 'minio.error': SimpleNamespace(S3Error=S3Error)}):
                if code == 'NoSuchKey':
                    self.assertEqual(controls.object(json.dumps(reference), present=False), '/manager/tenant_2/output.glb')
                else:
                    with self.subTest(code=code), self.assertRaises(S.SuiteError):
                        controls.object(json.dumps(reference), present=False)
                with self.assertRaises(S.SuiteError): controls.object(json.dumps({**reference, 'object': 'tenant_3/output.glb'}), present=False)

    def test_late_writer_requires_real_old_attempt_failed_assessment_and_unpublished_native_upload(self):
        for mutation in ('none', 'not_running', 'wrong_failure', 'replayed', 'cleanup_passed', 'other_owner_failed',
                         'engine_changed', 'no_native_write', 'published', 'lost_hold', 'changed_terminal'):
            events, attempted = [], []
            state = dict(crashed=False, released=False, observed=False)
            model = SimpleNamespace(locator='owned-model', item={}, format='obj')
            pending = dict(execution_id=ID, task_id='11', storage_ref='owned-ref')
            class Deployment:
                @contextmanager
                def paused_owner(self, name):
                    if name != 'model3d-workflow-engine': raise AssertionError('foreign pause')
                    events.append('paused')
                    try: yield
                    finally: events.append('resumed')
                def crash_and_start_manager(self):
                    if 'resumed' not in events or 'put_entered' not in events: raise AssertionError('crashed before real PUT')
                    state['crashed'] = True; events.append('crashed')
            class Controls:
                tenant, run_id, deployment = 2, 'native-1', Deployment()
                def pending_glb(self, engine, execution):
                    return dict(id=7, status='ready' if state['released'] and mutation == 'published' else 'failed' if state['crashed'] else 'building',
                        pending=copy.deepcopy(pending), storage_ref='published' if state['released'] and mutation == 'published' else '',
                        generation_holds=0 if state['released'] and mutation == 'lost_hold' else 2, deleted=False)
                def object(self, ref, *, present, verify_glb=False):
                    if ref != 'owned-ref': raise AssertionError('wrong native output')
                    if present and (not state['released'] or mutation == 'no_native_write'): raise S.SuiteError('no native write')
                    if present: state['observed'] = True
                    return '/manager/tenant_2/model3d-quick-view/fp/output.glb'
                def arm_writer(self, target):
                    if 'resumed' in events: raise AssertionError('proxy armed after Runtime resumed')
                    return ID
                def fault(self, kind, key):
                    events.append('put_entered')
                    return dict(run_id='native-1', key=ID, entered=1, forwarded=1 if state['released'] else 0,
                        upstream_response=state['released'], released=state['released'], expired=False, blocked_replays=0)
                def manager_attempt(self, execution):
                    status = 'failed' if state['crashed'] or mutation == 'not_running' else 'running'
                    return dict(execution_id=ID, status=status, attempt=2 if state['crashed'] and mutation == 'replayed' else 1,
                        started_at='original-start', completed_at='finished' if state['crashed'] else None,
                        lease_live=not state['crashed'], error_code='wrong' if mutation == 'wrong_failure' else 'manager.execution.lease_expired',
                        state_digest='changed' if state['released'] and mutation == 'changed_terminal' else 'stable')
                def release_writer(self, key):
                    if not state['crashed'] or 'assessment_failed' not in events: raise AssertionError('released before terminal/assessment proof')
                    state['released'] = True; events.append('released')
                def source_objects(self): events.append('source_unchanged')
            class Manager:
                def request(self, method, path, expected, body=None):
                    if method == 'POST' and expected == (202,):
                        return SimpleNamespace(payload=dict(task_id=11, task_type=M.MANAGER.MODEL_TASK_TYPE, execution_id=ID))
                    if method == 'GET':
                        if not state['observed']: raise AssertionError('public visibility checked before native effect')
                    if method == 'POST' and expected == (409,): attempted.append('new_generation')
                    return SimpleNamespace(payload={})
            class EngineUser:
                def request(self, method, path, expected, body=None):
                    if method == 'POST':
                        self.request_id = body['request_id']; return SimpleNamespace(payload={'assessment_id': self.request_id})
                    events.append('assessment_failed')
                    return SimpleNamespace(payload=dict(status='success' if mutation == 'cleanup_passed' else 'failed',
                        task={'context': {'engine_id': 9}, 'expected_modules': list(S.CONTRACT['cleanup_owners_by_cause']['engine.deleting']), 'requested_by': 17},
                        module_executions=[dict(module='manager', status='success' if mutation == 'other_owner_failed' else 'failed')]))
            workflow = SimpleNamespace(user=EngineUser(), principal='17',
                engine=lambda engine: {'lifecycle_state': 'deleting' if mutation == 'engine_changed' else 'active'})
            def wait(read, predicate, message, **kwargs):
                value = read(); M.require(predicate(value), message); return value
            with patch.object(M, 'prepare_manager', return_value=(9, model)), patch.object(S, 'wait_for', side_effect=wait), \
                 patch.object(M.MANAGER, 'verify_mesh_source'):
                report = dict(checks=[], tasks=[], executions=[], requests=[])
                if mutation == 'none':
                    M.run_late_writer(Manager(), workflow, Controls(), None, report, lambda: None)
                    self.assertEqual(set(report['checks']), set(S.CONTRACT['native_case_checks']['manager-late-writer']))
                    self.assertEqual(report['native_late_writer_acceptance'], 'passed')
                    self.assertEqual(report['entry'], 'engine_deletion_assessment')
                    self.assertEqual(attempted, ['new_generation'])
                else:
                    with self.subTest(mutation=mutation), self.assertRaises(S.SuiteError):
                        M.run_late_writer(Manager(), workflow, Controls(), None, report, lambda: None)
                    self.assertNotIn('native_late_writer_acceptance', report)
                    if mutation == 'not_running': self.assertNotIn('crashed', events)
                    if mutation in {'cleanup_passed', 'other_owner_failed', 'engine_changed'}: self.assertNotIn('released', events)

    def test_manager_crash_and_model_pause_are_restricted_to_the_exact_late_writer_case(self):
        deployment = S.OwnedDeployment.__new__(S.OwnedDeployment)
        deployment.paused, deployment.pins = set(), {'manager-backend': ('owned-id', 'owned-image', 'old-start')}
        before = dict(Id='owned-id', Image='owned-image', State=dict(Running=True, StartedAt='old-start', Health={'Status': 'healthy'}),
            HostConfig={'RestartPolicy': {'Name': 'no'}}, Config={'Env': ['MANAGER_EXECUTION_LEASE_DURATION=30s', 'MANAGER_EXECUTION_HEARTBEAT_INTERVAL=5s']})
        after = copy.deepcopy(before); after['State']['StartedAt'] = 'new-start'
        stopped = dict(Id='owned-id', Image='owned-image', State={'Running': False, 'ExitCode': 137})
        for case in ('', 'manager-success', 'transfer-engine'):
            with patch.dict(M.os.environ, {'ADDP_ONLINE_NATIVE_CASE': case}), patch.object(deployment, 'inspect') as inspect, \
                 patch.object(deployment, 'command') as command:
                with self.assertRaises(S.SuiteError): deployment.crash_and_start_manager()
                with self.assertRaises(S.SuiteError): S.OwnedDeployment.inspect(deployment, 'model3d-workflow-engine')
                inspect.assert_not_called(); command.assert_not_called()
        with patch.dict(M.os.environ, {'ADDP_ONLINE_NATIVE_CASE': 'manager-late-writer'}), \
             patch.object(deployment, 'inspect', side_effect=[before, before, after]), \
             patch.object(deployment, 'command', side_effect=['', json.dumps([stopped]), '']) as command, \
             patch.object(S, 'wait_for', return_value=after):
            deployment.crash_and_start_manager()
        self.assertEqual([call.args[0] for call in command.call_args_list], [
            ['docker', 'kill', '--signal=KILL', 'owned-id'], ['docker', 'inspect', 'owned-id'], ['docker', 'start', 'owned-id']])

    def test_uncertain_writer_arm_retains_release_intent_and_never_releases_foreign_key(self):
        controls = M.NativeControls.__new__(M.NativeControls)
        controls.run_id, controls.proxies, controls.writer_keys = 'native-1', {}, set()
        calls = []
        def control(kind, action, body):
            calls.append(action)
            if action == 'arm':
                self.assertEqual(controls.proxies['object'], body['key'])
                self.assertIn(body['key'], controls.writer_keys)
                raise S.SuiteError('arm response lost')
            self.assertEqual(kind, 'object')
            self.assertEqual(body, {'run_id': 'native-1', 'key': controls.proxies['object']})
            return {'released': True, 'expired': False}
        with patch.object(controls, 'control', side_effect=control):
            with self.assertRaises(S.SuiteError): controls.arm_writer('/manager/tenant_2/model3d-quick-view/fp/out.glb')
            self.assertEqual(len(controls.writer_keys), 1)
            with self.assertRaises(S.SuiteError): controls.release_writer(str(uuid.uuid4()))
            self.assertEqual(calls, ['arm'])
            controls.close()
        self.assertEqual(calls, ['arm', 'release'])
        self.assertEqual(controls.writer_keys, set())

    def test_pending_glb_can_be_absent_before_worker_preparation_but_never_ambiguous(self):
        controls = M.NativeControls.__new__(M.NativeControls)
        controls.tenant = 2
        pending = {'id': 7, 'pending': {'execution_id': ID, 'storage_ref': 'owned'}}
        read = SimpleNamespace(read=None)
        controls.deployment = read
        with patch.object(read, 'read', side_effect=[[], [pending], [pending, pending]]):
            self.assertIsNone(controls.pending_glb(9, ID))
            self.assertEqual(controls.pending_glb(9, ID), pending)
            with self.assertRaises(S.SuiteError): controls.pending_glb(9, ID)

    def test_native_grants_use_exact_existing_initializer_without_deny_authority(self):
        initializer = M.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS
        consumer = set(S.CONTRACT['native_permissions']['manager'])
        class User:
            def __init__(self, principal, permissions): self.principal, self.permissions = principal, permissions
            def request(self, *_):
                return SimpleNamespace(payload=dict(principal={'id': self.principal, 'type': 'user'},
                    context={'type': 'tenant', 'tenant_id': '2'}, authorization={'role_assignments': [
                    {'role_key': 'online.native', 'permissions': sorted(self.permissions)}]}))
        with patch.object(M.GRANTS.API, 'validate_user_identity'):
            identity = M.GRANTS.ModelSourceAuthorization(User('17', initializer), User('18', consumer),
                dict(tenant_id=2, engine_id=9, principal_id='18'), source_type='minio',
                consumer_permissions=consumer, issuer_permissions=initializer)
            self.assertEqual(identity.operator, '17')
            with self.assertRaises(M.GRANTS.SuiteError):
                M.GRANTS.ModelSourceAuthorization(User('17', initializer | {'system.engine_access_deny.create'}),
                    User('18', consumer), dict(tenant_id=2, engine_id=9, principal_id='18'),
                    source_type='minio', consumer_permissions=consumer, issuer_permissions=initializer)

    def test_paused_does_not_mean_runtime_returned(self):
        calls = []
        class User:
            def request(self, method, path, expected):
                return SimpleNamespace(payload=dict(desired_state='paused', status='idle'))
        controls = SimpleNamespace(require_invocations_stopped=lambda task: calls.append(task))
        M.wait_paused(User(), 9, controls)
        self.assertEqual(calls, [9])
        def unfinished(_): raise S.SuiteError('original invocation still running')
        with self.assertRaises(S.SuiteError): M.wait_paused(User(), 9, SimpleNamespace(require_invocations_stopped=unfinished))


if __name__ == '__main__': unittest.main()
