from datetime import timedelta
from copy import deepcopy
from importlib import import_module
import unittest
import os
import json
import tempfile
from unittest.mock import patch
from types import SimpleNamespace
from pathlib import Path

M = import_module('scripts.test.transfer-continuous-events-online')
UUID = 'aaaaaaaa-1111-4111-8111-111111111111'


def fact(identity, kind='progress'):
    return {'id': identity, 'execution_id': UUID, 'attempt': 1, 'occurred_at': '2026-10-12T01:00:00Z',
            'kind': kind, 'counters': {'records_read': 2, 'records_written': 2}}


def page(items, more=False):
    return {'items': items, 'next_cursor': items[-1]['id'] if items else 0,
            'has_more': more, 'retained_after': '2026-09-12T01:00:00Z'}


class Client:
    def __init__(self, pages):
        self.pages = iter(pages)
        self.calls = []
    def request(self, *args, **kwargs):
        self.calls.append(args)
        return SimpleNamespace(payload=next(self.pages))


class ContinuousRunnerTest(unittest.TestCase):
    def mock_scan_observer(self, snapshot):
        self.enterContext(patch.dict(os.environ, {
            'ONLINE_SUITE': M.SUITE, 'ADDP_ONLINE_TEST_TENANT_ID': '2',
            'ADDP_ONLINE_CONTINUOUS_PG_ENGINE_ID': '10', 'ADDP_ONLINE_SCAN_POSTGRES_ID': 'a' * 64,
        }))
        postgres = import_module('scripts.test.execution-retention-readonly-online')
        return self.enterContext(patch.object(postgres, 'sql', side_effect=lambda *args, **kwargs: json.dumps(snapshot)))

    def test_scan_observer_reads_only_the_bound_fixture(self):
        _, sibling, _, observer = self.scan_fixture()
        items, boundary = M.read_scan_catalog(10, 2)
        self.assertEqual(items[M.SCAN_SIBLING], sibling)
        self.assertEqual([node['id'] for node in boundary['ancestors']], [9, 1])
        observer.assert_called_once()
        query, = observer.call_args.args
        self.assertEqual(observer.call_args.kwargs, {
            'expected_suite': M.SUITE, 'postgres_id': 'a' * 64, 'variables': {'tenant': 2, 'engine': 10},
        })
        self.assertTrue(query.startswith('BEGIN READ ONLY;'))
        self.assertIn("tenant_id = :'tenant'::bigint AND engine_id = :'engine'::bigint", query)
        self.assertIn("full_name IN ('public.continuous_cdc_target', 'public.continuous_kafka_target', "
                      "'public.continuous_scan_sibling', 'public.continuous_scheduled_target')", query)
        self.assertIn("jsonb_agg(field->>'name')", query)
        observer.reset_mock()
        for key, invalid in [('ONLINE_SUITE', 'other'), ('ADDP_ONLINE_TEST_TENANT_ID', '3'),
                             ('ADDP_ONLINE_CONTINUOUS_PG_ENGINE_ID', '11'), ('ADDP_ONLINE_SCAN_POSTGRES_ID', 'local')]:
            with self.subTest(key=key), patch.dict(os.environ, {key: invalid}), self.assertRaises(M.SuiteError):
                M.read_scan_catalog(10, 2)
        observer.assert_not_called()

    def test_scan_baseline_diagnostics_exclude_private_values(self):
        private = 'business-value-sentinel password=private'
        for items, reason in [
            ([], 'source_catalog_sibling_unavailable'),
            ([{'full_name': 'public.continuous_scan_sibling', 'tenant_id': 3, 'metadata': private}],
             'source_catalog_identity_invalid'),
        ]:
            evidence = {}
            self.mock_scan_observer({'items': items, 'ancestors': []})
            with self.assertRaises(M.SuiteError) as caught:
                M.read_scan_catalog(10, 2, evidence=evidence)
            self.assertEqual(evidence, {'source': 'hosted_postgres_readonly', 'state': 'identity_check',
                                        'matching_items': len(items)})
            self.assertEqual(M.failure_diagnostic('source_catalog', caught.exception)['reason'], reason)
        observer = self.mock_scan_observer({})
        observer.side_effect = OSError(private)
        with self.assertRaises(M.SuiteError) as caught:
            M.read_scan_catalog(10, 2)
        diagnostic = M.failure_diagnostic('source_catalog', caught.exception)
        self.assertEqual(diagnostic['reason'], 'source_catalog_observer_unavailable')
        self.assertNotIn(private, json.dumps(diagnostic))

    def test_professional_initial_authorization_is_closed_status_only(self):
        for initial in [{'status': 'pending'}, {'status': 'pending', 'code': 'scan_pending'},
                        {'status': 'issued'}, {'status': 'denied', 'code': 'source_access_denied'}]:
            item = {'execution_id': UUID, 'metadata': {'initial_authorization': initial}}
            M.assert_projection(item, UUID)
        for initial in [None, {'status': []}, {'status': 'unknown'}, {'status': 'pending', 'code': {}},
                        {'status': 'pending', 'code': None}, {'status': 'pending', 'code': 'business-value-sentinel'},
                        {'status': 'issued', 'code': 'scan_pending'}, {'status': 'denied'},
                        {'status': 'pending', 'attempts': 1}, {'status': 'issued', 'grant_request_id': UUID},
                        {'status': 'pending', 'next_attempt_unix': 1}]:
            with self.subTest(initial=initial), self.assertRaises(M.SuiteError):
                M.assert_projection({'execution_id': UUID, 'metadata': {'initial_authorization': initial}}, UUID)
        monitor = {'execution_id': UUID, 'module': 'transfer', 'task_type': 'sync',
                   'execution_boundary': 'continuous', 'queue_duration_ms': 0, 'run_duration_ms': 1,
                   'lease_state': 'none', 'metadata': {'initial_authorization': {'status': 'pending'}}}
        with self.assertRaisesRegex(M.SuiteError, 'generic diagnostic'):
            M.assert_projection(monitor, UUID, monitor=True)

    def test_failure_diagnostic_keeps_phase_without_exception_text_or_subprocess_output(self):
        self.assertEqual(M.failure_diagnostic('scheduled_completion', M.SuiteError('Owner exposed arbitrary metadata')),
                         {'phase': 'scheduled_completion', 'reason': 'owner_metadata_contract_mismatch'})
        private = 'Bearer private-token business-value-sentinel raw_response'
        for error, reason in [(M.SuiteError(private), 'request_or_contract_failed'),
                              (M.subprocess.CalledProcessError(1, private, output=private), 'subprocess_failed'),
                              (OSError(private), 'io_failed')]:
            receipt = M.failure_diagnostic('scheduled_scan', error)
            self.assertEqual(receipt, {'phase': 'scheduled_scan', 'reason': reason})
            self.assertNotIn('private-token', json.dumps(receipt))
        self.assertEqual(M.failure_diagnostic(private, ValueError(private))['phase'], 'preflight')

    def test_cleanup_records_each_owned_task_and_verifies_deletion_after_safe_history(self):
        for fail_history in [False, True]:
            calls, report = [], {}
            class Actor:
                def request(self, method, path, expected):
                    calls.append((method, path, expected))
                    if path.endswith('/executions?page_size=100'):
                        if fail_history and '/3/' in path:
                            raise M.SuiteError('Bearer private-token business-value-sentinel')
                        return SimpleNamespace(payload={'total': 1, 'data': [{'execution_id': UUID, 'status': 'success',
                            'metadata': {'initial_authorization': {'status': 'pending'}}}]})
                    if method == 'GET' and expected == (200,):
                        return SimpleNamespace(payload={'enabled': False, 'desired_state': 'stopped'})
                    return SimpleNamespace(payload={'error': 'private-token'})
            complete = M.cleanup_owned_tasks(Actor(), [(2, 'kafka', 'owned-kafka'), (3, 'scheduled', 'owned-scheduled')],
                                             lambda *args: self.fail('already quiescent'), report, lambda: None)
            self.assertEqual(complete, not fail_history)
            self.assertEqual(report['residual_resources'], int(fail_history))
            self.assertEqual([r['task_id'] for r in report['cleanup_tasks']], [3, 2])
            self.assertEqual(report['cleanup_tasks'][-1]['step'], 'verified_absent')
            self.assertIn(('DELETE', M.OWNER + '/task-definitions/2', (200,)), calls)
            self.assertIn(('GET', M.OWNER + '/task-definitions/2', (404,)), calls)
            if fail_history:
                self.assertEqual(report['cleanup_tasks'][0]['step'], 'wait_terminal_history')
                self.assertEqual(report['cleanup_tasks'][0]['failure']['reason'], 'request_or_contract_failed')
            else:
                self.assertEqual(report['cleanup'], 'passed')
                self.assertIn(('DELETE', M.OWNER + '/task-definitions/3', (200,)), calls)
                self.assertIn(('GET', M.OWNER + '/task-definitions/3', (404,)), calls)
            self.assertNotIn('private-token', json.dumps(report))

    def test_hard_delete_wait_covers_runtime_stop_but_definition_read_keeps_original_timeout(self):
        import io
        import urllib.error
        actor=M.API.GatewayClient('http://127.0.0.1:8000','private-token',10)
        before={'task_present':True,'sessions':3,'open':1,'selected':1,'selected_open':1,'operations':0}
        calls=[]
        def urlopen(request,timeout):
            method=request.get_method();calls.append((method,timeout,request.get_header('Authorization')))
            if method=='DELETE':
                if timeout<=45:raise TimeoutError('simulated server stop response needs the 45 second window')
                raise urllib.error.HTTPError(request.full_url,503,'unavailable',{},io.BytesIO(b'{"error":"unavailable"}'))
            response=io.BytesIO(b'{"id":7,"tenant_id":2,"desired_state":"stopped"}')
            response.status=200
            return response
        with patch.object(M.API.urllib.request,'urlopen',side_effect=urlopen), \
                patch.object(M,'kafka_delete_facts',side_effect=[before,before]):
            result=M.verify_kafka_hard_delete(actor,2,7,UUID)
        self.assertEqual([(method,timeout)for method,timeout,_ in calls],[('DELETE',60),('GET',10)])
        self.assertTrue(all(auth=='Bearer private-token' for _,_,auth in calls))
        self.assertEqual(actor.timeout,10)
        self.assertEqual(result['delete_status'],503)
        self.assertTrue(result['task_retained']and result['no_delete_dispatch'])

    def test_hard_exit_requires_exact_old_session_refusal_and_no_delete_intent(self):
        path = M.OWNER + '/task-definitions/7'
        before = {'task_present': True, 'sessions': 3, 'open': 1, 'selected': 1,
                  'selected_open': 1, 'operations': 0}
        for status in (502, 503):
            actor = SimpleNamespace(base_url='http://127.0.0.1:8000', token='private-token', request=unittest.mock.Mock(side_effect=[
                SimpleNamespace(status=status, payload={'error': 'unavailable'}),
                SimpleNamespace(payload={'id': 7, 'tenant_id': 2, 'desired_state': 'stopped'}),
            ]))
            with patch.object(M.API, 'GatewayClient', return_value=actor) as factory, \
                    patch.object(M, 'kafka_delete_facts', side_effect=[before, before]):
                result = M.verify_kafka_hard_delete(actor, 2, 7, UUID)
            factory.assert_called_once_with(actor.base_url, actor.token, 60)
            self.assertEqual(result, {'task_id': 7, 'execution_id': UUID, 'delete_status': status,
                'original_invocation_unconfirmed': True, 'task_retained': True, 'no_delete_dispatch': True})
            self.assertEqual(actor.request.call_args_list, [
                unittest.mock.call('DELETE', path, (502, 503)), unittest.mock.call('GET', path, (200,)),
            ])
        actor.request.reset_mock()
        with patch.object(M, 'kafka_delete_facts', return_value={**before, 'selected_open': 0}), self.assertRaises(M.SuiteError):
            M.verify_kafka_hard_delete(actor, 2, 7, UUID)
        actor.request.assert_not_called()
        actor.request.side_effect = [SimpleNamespace(status=503, payload={}),
                                     SimpleNamespace(payload={'id': 7, 'tenant_id': 2, 'desired_state': 'stopped'})]
        with patch.object(M.API, 'GatewayClient', return_value=actor), \
                patch.object(M, 'kafka_delete_facts', side_effect=[before, {**before, 'operations': 1}]), \
                self.assertRaises(M.KafkaHardDeleteFailure) as caught:
            M.verify_kafka_hard_delete(actor, 2, 7, UUID)
        self.assertEqual(caught.exception.diagnostic['step'], 'post_delete_verify')

    def test_hard_delete_failure_locates_each_boundary_without_private_error_text(self):
        private='Bearer private-token business-value-sentinel raw_response'
        before={'task_present':True,'sessions':3,'open':1,'selected':1,'selected_open':1,'operations':0}
        for step in ['pre_delete_facts','delete_request','delete_response','definition_read',
                     'definition_verify','post_delete_facts','post_delete_verify']:
            actor=SimpleNamespace(base_url='http://127.0.0.1:8000',token='private-token',
                request=unittest.mock.Mock(return_value=SimpleNamespace(payload={
                    'id':8 if step=='definition_verify' else 7,'tenant_id':2,'desired_state':'stopped'})))
            response=SimpleNamespace(status=503,payload={'raw_response':private}if step=='delete_response'else{})
            deletion=SimpleNamespace(request=unittest.mock.Mock(return_value=response))
            facts=[M.SuiteError(private)if step=='pre_delete_facts'else before,
                   M.SuiteError(private)if step=='post_delete_facts'else {**before,'operations':1}if step=='post_delete_verify'else before]
            if step=='delete_request':deletion.request.side_effect=M.SuiteError(private)
            if step=='definition_read':actor.request.side_effect=M.SuiteError(private)
            with self.subTest(step=step), patch.object(M.API,'GatewayClient',return_value=deletion), \
                    patch.object(M,'kafka_delete_facts',side_effect=facts), \
                    self.assertRaises(M.KafkaHardDeleteFailure)as caught:
                M.verify_kafka_hard_delete(actor,2,7,UUID)
            value=caught.exception.diagnostic
            self.assertEqual(value['step'],step)
            self.assertEqual(value['reason'],'contract_failed')
            self.assertEqual(value['delete_request_timeout_seconds'],60)
            self.assertNotIn('private',json.dumps(value))
            self.assertNotIn('business-value-sentinel',json.dumps(value))
            self.assertLessEqual(deletion.request.call_count,1)
            if step not in ['pre_delete_facts','delete_request']:self.assertEqual(value['delete_http_status'],503)

    def test_hard_delete_transport_and_response_causes_have_closed_safe_categories(self):
        import urllib.error
        for cause,reason in [(TimeoutError('private'),'transport_timeout'),
                (urllib.error.URLError(TimeoutError('private')),'transport_timeout'),
                (urllib.error.URLError('private'),'transport_unavailable'),
                (json.JSONDecodeError('private','private',0),'invalid_response')]:
            error=M.SuiteError('private raw response');error.__cause__=cause
            value=M.KafkaHardDeleteFailure('delete_request',error,None)
            self.assertEqual(value.diagnostic,{'step':'delete_request','reason':reason,'delete_request_timeout_seconds':60})
            self.assertEqual(M.failure_diagnostic('kafka_hard_delete',value),
                             {'phase':'kafka_hard_delete','reason':'kafka_hard_delete_'+reason})

    def test_cleanup_only_deletes_graceful_task_and_requires_physical_dlq_absence(self):
        stopped = {'task_present': True, 'sessions': 1, 'open': 0, 'selected': 1,
                   'selected_open': 0, 'operations': 0}
        path = M.OWNER + '/task-definitions/8'
        tasks = [(7, 'kafka', 'hard'), (8, 'kafka-graceful', 'normal')]
        for topic_after in (False, True):
            report = {'kafka_hard_exit': {'task_id': 7}}
            actor = SimpleNamespace(request=unittest.mock.Mock(return_value=SimpleNamespace(
                payload={'desired_state': 'stopped'})))
            with patch.object(M, 'history', return_value=[{'status': 'cancelled'}]), \
                    patch.object(M, 'kafka_delete_facts', return_value=stopped) as facts, \
                    patch.object(M, 'kafka_dlq_exists', side_effect=[True, topic_after]) as dlq:
                success = M.cleanup_owned_tasks(actor, tasks, None, report, lambda: None,
                                               hard_task=7, graceful=(2, 8, UUID))
            self.assertEqual(success, not topic_after)
            self.assertEqual(report['expected_deployment_residuals'], 1)
            self.assertEqual(report['residual_resources'], 1 + int(topic_after))
            self.assertEqual(report['cleanup_tasks'][-1]['result'], 'retained_for_deployment')
            self.assertEqual(actor.request.call_args_list, [unittest.mock.call('GET', path, (200,)),
                unittest.mock.call('DELETE', path, (200,)), unittest.mock.call('GET', path, (404,))])
            facts.assert_called_once_with(2, 8, UUID)
            self.assertEqual(dlq.call_count, 2)
            self.assertEqual('kafka_graceful_exit' in report, not topic_after)
        actor.request.reset_mock()
        report = {}
        self.assertFalse(M.cleanup_owned_tasks(actor, tasks[:1], None, report, lambda: None, hard_task=7))
        actor.request.assert_not_called()
        self.assertEqual(report['expected_deployment_residuals'], 0)

    def test_kafka_observer_returns_only_bound_counts_and_reads_exact_topic(self):
        facts = {'task_present': True, 'sessions': 1, 'open': 0, 'selected': 1,
                 'selected_open': 0, 'operations': 0}
        sql = self.mock_scan_observer(facts)
        self.assertEqual(M.kafka_delete_facts(2, 7, UUID), facts)
        self.assertTrue(sql.call_args.args[0].startswith('BEGIN READ ONLY;'))
        self.assertEqual(sql.call_args.kwargs['variables'], {'tenant': 2, 'task': 7, 'execution': UUID})
        sql.reset_mock()
        with self.assertRaises(M.SuiteError):
            M.kafka_delete_facts(3, 7, UUID)
        sql.assert_not_called()

        broker_id = 'b' * 64
        broker = {'Id': broker_id, 'State': {'Running': True}, 'Config': {'Labels': {
            'com.docker.compose.project': 'addp-infra', 'com.docker.compose.service': 'redpanda',
            'com.docker.compose.project.working_dir': str(M.ROOT),
        }}}
        with patch.dict(os.environ, {'ADDP_ONLINE_DLQ_REDPANDA_ID': broker_id,
                                    'INFRA_KAFKA_TRANSFER_PASSWORD': 'private-sentinel'}), \
                patch.object(M.FAULTS.SUPPORT, 'deployment_secret'), \
                patch.object(M.subprocess, 'run') as command:
            for names, present in [(['__addp_dlq.2.7', '__addp_dlq.2.70'], True),
                                   (['__addp_dlq.2.70', '__addp_dlq.3.7'], False)]:
                command.side_effect = [SimpleNamespace(stdout=json.dumps([broker])),
                    SimpleNamespace(stdout=json.dumps([{'name': name} for name in names]))]
                self.assertIs(M.kafka_dlq_exists(2, 7), present)
                self.assertEqual(command.call_args.args[0][1:5],
                                 ['exec', '-e', 'INFRA_KAFKA_TRANSFER_PASSWORD', broker_id])
                self.assertNotIn('private-sentinel', str(command.call_args))
            command.reset_mock()
            command.side_effect = [SimpleNamespace(stdout=json.dumps([{**broker, 'Id': 'c' * 64}]))]
            with self.assertRaises(M.SuiteError):
                M.kafka_dlq_exists(2, 7)
            command.assert_called_once()

    def test_scenario_error_survives_a_second_history_failure_during_cleanup(self):
        calls, report = [], {}
        self_case = self
        class Actor:
            def request(self, method, path, expected, body=None):
                calls.append((method, path))
                if path.endswith('/scan/run/manual') or path.endswith('/start'):
                    return SimpleNamespace(payload={'execution_id': UUID, 'metadata': {}})
                if path == '/api/v1/meta/executions/' + UUID:
                    self_case.assertEqual(expected, (404,))
                    return SimpleNamespace(payload={'status': 'success'})
                if path == M.OWNER + '/task-definitions':
                    return SimpleNamespace(payload={'id': 1, 'tenant_id': 2})
                if path.endswith('/executions?page_size=100'):
                    return SimpleNamespace(payload={'total': 1, 'data': [{'execution_id': UUID, 'status': 'success',
                        'metadata': {'initial_authorization': {'status': 'issued', 'grant_request_id': UUID}}}]})
                if path == M.OWNER + '/task-definitions/1':
                    return SimpleNamespace(payload={'desired_state': 'stopped'})
                raise AssertionError('unexpected fixture operation')
        class Preparer(Actor):
            def request(self, method, path, expected, body=None):
                if path == '/api/v1/meta/executions/' + UUID:
                    return SimpleNamespace(payload={'status': 'success', 'triggered_by': 34})
                return super().request(method, path, expected, body)
        with patch.object(M.REGISTRATION, 'source_grant_participants', return_value=('34', '31')), \
                patch.object(M, 'read_scan_catalog', return_value=({}, {'sibling': {'id': 9}, 'ancestors': [{}, {}]})), \
                patch.object(M.API, 'wait_meta_scan_tick'), \
                patch.object(M, 'wait_status', side_effect=M.SuiteError('execution reached an unexpected terminal status')), \
                self.assertRaisesRegex(M.SuiteError, 'unexpected terminal status'):
            M.run_suite(Actor(), None, None, Actor(), None, None, None, 2, 'run-1', 10, 11,
                        report, lambda: None, None, preparer=Preparer())
        self.assertEqual(report['failure']['phase'], 'kafka_initial')
        self.assertEqual(report['failure']['reason'], 'unexpected_terminal_status')
        self.assertEqual(report['cleanup'], 'failed')
        self.assertEqual(report['cleanup_tasks'][0]['step'], 'wait_terminal_history')
        self.assertEqual(report['cleanup_tasks'][0]['failure']['reason'], 'owner_authorization_status_contract_mismatch')
        self.assertFalse(any(method == 'DELETE' for method, _ in calls))

    def test_owner_browser_control_requires_bound_observation_and_restored_process_proof(self):
        with tempfile.TemporaryDirectory() as directory:
            control = Path(directory)
            marker = {'run_id': 'run-1', 'phase': 'owner_unavailable', 'execution_id': UUID}
            process = SimpleNamespace(poll=lambda: None)
            def stopped(operation):
                M.write_browser_marker(control, 'owner_failure_observed', marker)
                operation()
                self.assertEqual(json.loads((control / 'owner_stopped.json').read_text()), marker)
                return {'owner_pid': 42, 'stopped_verified': True, 'resumed_verified': True}
            faults = SimpleNamespace(stop_owner_during=stopped)
            self.assertTrue(M.observe_browser_owner_outage(control, marker, process, faults)['resumed_verified'])
            M.write_browser_marker(control, 'owner_failure_observed', {**marker, 'execution_id': 'other'})
            def wrong_marker(operation):
                operation()
            with self.assertRaisesRegex(M.SuiteError, 'control identity'):
                M.observe_browser_owner_outage(control, marker, process, SimpleNamespace(stop_owner_during=wrong_marker))
            for proof in [None, {'owner_pid': 42, 'stopped_verified': True, 'resumed_verified': False},
                          {'owner_pid': True, 'stopped_verified': True, 'resumed_verified': True},
                          {'owner_pid': 42, 'stopped_verified': True, 'resumed_verified': True, 'raw_response': {}}]:
                with self.subTest(proof=proof), self.assertRaisesRegex(M.SuiteError, 'process identity'):
                    M.observe_browser_owner_outage(control, marker, process,
                        SimpleNamespace(stop_owner_during=lambda action: proof))

    def test_owner_browser_control_does_not_wait_after_failed_browser(self):
        with tempfile.TemporaryDirectory() as directory, self.assertRaisesRegex(M.SuiteError, 'controlled observation'):
            M.wait_browser_marker(Path(directory), 'owner_failure_observed', {}, SimpleNamespace(poll=lambda: 1))

    def test_owner_browser_receipt_binds_two_real_executions_current_user_and_external_fault(self):
        second = 'bbbbbbbb-1111-4111-8111-111111111111'
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifacts, secrets = root / 'artifacts', root / 'private'
            artifacts.mkdir(); secrets.mkdir()
            subject = {'tenant_id': '2', 'principal_id': '33'}
            marker = {'run_id': 'run-1', 'phase': 'owner_unavailable', 'execution_id': UUID}
            proof = {'owner_pid': 42, 'stopped_verified': True, 'resumed_verified': True}
            env = {'ADDP_ONLINE_ARTIFACT_DIR': str(artifacts),
                   'ADDP_ONLINE_TEST_USER_USERNAME': 'external-online-consumer',
                   'ADDP_ONLINE_TEST_USER_PASSWORD': 'private-password'}
            class Process:
                pid = 999999
                def __init__(self, args, **kwargs):
                    self.ended = False
                    self.control = Path(kwargs['env']['ADDP_CONTINUOUS_BROWSER_CONTROL'])
                    M.write_browser_marker(self.control, 'ready', marker)
                    M.write_browser_marker(self.control, 'owner_failure_observed', marker)
                def poll(self): return 0 if self.ended else None
                def wait(self, timeout):
                    assert json.loads((self.control / 'owner_stopped.json').read_text()) == marker
                    assert json.loads((self.control / 'release.json').read_text()) == marker
                    report = {'schema_version': 'addp.continuous-execution-browser/v1', 'phase': 'owner_unavailable',
                              'run_id': 'run-1', 'tenant_id': '2', 'principal_id': '33',
                              'execution_ids': [UUID, second], 'checks': M.OWNER_OUTAGE_CHECKS, 'result': 'passed'}
                    (artifacts / 'continuous-browser-owner_unavailable.json').write_text(json.dumps(report))
                    self.ended = True
                    return 0
            class Faults:
                def stop_owner_during(self, operation):
                    operation()
                    return proof
            with patch.dict(os.environ, env), patch.object(M.subprocess, 'Popen', Process):
                result = M.run_browser('owner_unavailable', [UUID, second], subject, 'run-1', secrets, faults=Faults())
            self.assertEqual(result['owner_process'], proof)
            self.assertEqual(result['execution_ids'], [UUID, second])
            receipt = artifacts / 'continuous-browser-owner_unavailable.json'
            self.assertEqual(json.loads(receipt.read_text()), result)
            self.assertNotIn('private-password', receipt.read_text())
            self.assertEqual(list(secrets.iterdir()), [])
        for ids in [[UUID], [UUID, UUID], [UUID, 'invalid']]:
            with self.subTest(ids=ids), self.assertRaises(M.SuiteError):
                M.run_browser('owner_unavailable', ids, {}, 'run-1', '/not-used', faults=object())
        with self.assertRaisesRegex(M.SuiteError, 'fault binding'):
            M.run_browser('owner_unavailable', [UUID, second], {}, 'run-1', '/not-used')

    def test_scheduled_scan_requires_exact_parent_user_and_peer_isolation(self):
        execution = {'execution_id': UUID, 'status': 'success', 'trigger_type': 'scheduled', 'trigger_by': 31, 'metadata': {}}
        scan_id = 'bbbbbbbb-1111-4111-8111-111111111111'
        scan = {'execution_id': scan_id, 'status': 'success', 'triggered_by': 31, 'parent_execution_id': UUID}
        owner, peer = Client([scan]), Client([execution, {'error': 'not found'}])
        self.assertTrue(M.assert_scheduled_scan_origin(owner, peer, execution, scan_id, 31))
        self.assertEqual(peer.calls[0][2], (200,))
        self.assertEqual(peer.calls[1][2], (404,))
        for changed in [{**scan, 'triggered_by': 32}, {**scan, 'parent_execution_id': scan_id},
                        {**scan, 'status': 'failed'}]:
            with self.subTest(scan=changed), self.assertRaises(M.SuiteError):
                M.assert_scheduled_scan_origin(Client([changed]), Client([{}]), execution, scan_id, 31)
        for changed in [{**execution, 'trigger_type': 'manual'}, {**execution, 'trigger_by': 32}]:
            with self.assertRaises(M.SuiteError):
                M.assert_scheduled_scan_origin(Client([scan]), Client([{}]), changed, scan_id, 31)
        with self.assertRaises(M.SuiteError):
            M.assert_scheduled_scan_origin(Client([scan]), Client([execution, {'execution_id': scan_id}]), execution, scan_id, 31)

    def test_scheduled_body_uses_existing_scanned_source_and_a_single_new_target(self):
        body = M.scheduled_task_body('scheduled', 10)
        self.assertTrue(body['enabled'] and body['auto_scan_metadata'])
        self.assertEqual(body['schedule'], '*/15 * * * * *')
        self.assertEqual(body['config']['runtime'], {'boundary': 'bounded'})
        self.assertEqual(body['config']['source']['locator'], 'addp://engine/10/path/public/continuous_scan_sibling?type=table')
        self.assertEqual(body['config']['target']['name'], 'continuous_scheduled_target')

    def test_failed_scheduled_phase_pauses_future_ticks_before_cleanup(self):
        calls = []
        def mutate(*args):
            calls.append(args)
        actor = object()
        M.quiesce_task_for_cleanup(actor, 7, 'scheduled', 'scheduled',
                                  {'enabled': True, 'desired_state': 'stopped'}, mutate)
        self.assertEqual(calls, [(actor, M.OWNER + '/task-definitions/7/pause')])
        calls.clear()
        M.quiesce_task_for_cleanup(actor, 7, 'scheduled', 'scheduled',
                                  {'enabled': False, 'desired_state': 'stopped'}, mutate)
        self.assertEqual(calls, [])
        M.quiesce_task_for_cleanup(actor, 8, 'cdc', 'owned-cdc', {'desired_state': 'paused'}, mutate)
        self.assertEqual(calls, [(actor, M.OWNER + '/task-definitions/8/stop',
                                 {'confirmed': True, 'confirmation_text': 'owned-cdc'})])
        with self.assertRaises(M.SuiteError):
            M.quiesce_task_for_cleanup(actor, 9, 'unknown', 'foreign', {}, mutate)

    def scan_fixture(self):
        sibling = {'id': 5, 'engine_id': 10, 'tenant_id': 2, 'node_id': 9, 'item_type': 'table',
                   'full_name': 'public.continuous_scan_sibling', 'fingerprint': 'sibling',
                   'scanned_at': '2026-10-16T09:00:00Z', 'scanned_depth': 'deep', 'row_count': 2}
        schema = {'id': 9, 'engine_id': 10, 'tenant_id': 2, 'parent_node_id': 1,
                  'scan_status': 'completed', 'scanned_depth': 'deep', 'scanned_at': sibling['scanned_at']}
        root = {**schema, 'id': 1, 'parent_node_id': None}
        target = {**sibling, 'id': 6, 'full_name': 'public.continuous_cdc_target', 'fingerprint': 'target',
                  'scanned_at': '2026-10-16T09:00:02Z', 'fields': ['id', 'label']}
        snapshot = {'items': [sibling, target], 'ancestors': [schema, root]}
        return snapshot, sibling, target, self.mock_scan_observer(snapshot)

    def test_target_scan_requires_fresh_deep_schema_and_preserves_outside_facts(self):
        snapshot, sibling, target, _ = self.scan_fixture()
        _, boundary = M.read_scan_catalog(10, 2)
        def wait(fields):
            return M.wait_target_scan(10, 2, 'continuous_cdc_target', fields, boundary)

        proof = wait({'id', 'label'})
        self.assertEqual(proof['target_locator'], 'addp://engine/10/path/public/continuous_cdc_target?type=table')
        self.assertEqual(proof['item_id'], 6)
        with patch.object(M, 'until', side_effect=lambda check: self.assertIsNone(check())):
            wait({'id', 'label', 'scan_extra'})
        target['fields'].append('scan_extra')
        target['scanned_at'] = '2026-10-16T09:00:04Z'
        self.assertEqual(wait({'id', 'label', 'scan_extra'})['fields'], ['id', 'label', 'scan_extra'])
        for item, field, value in [(sibling, 'scanned_at', '2026-10-16T09:00:03Z'),
                                   (snapshot['ancestors'][0], 'scan_status', 'running'),
                                   (snapshot['ancestors'][1], 'scanned_depth', 'basic')]:
            old = item[field]
            item[field] = value
            with self.subTest(field=field), self.assertRaisesRegex(M.SuiteError, 'sibling facts or ancestor'):
                wait({'id', 'label', 'scan_extra'})
            item[field] = old

    def test_target_scan_rejects_wrong_identity_duplicates_and_incomplete_scan(self):
        snapshot, sibling, target, _ = self.scan_fixture()
        _, boundary = M.read_scan_catalog(10, 2)
        def wait():
            return M.wait_target_scan(10, 2, 'continuous_cdc_target', {'id', 'label'}, boundary)

        for key, value in [('tenant_id', 3), ('engine_id', 11), ('node_id', 99), ('item_type', 'file'),
                           ('fingerprint', ''), ('full_name', 'public.foreign_table'), ('id', sibling['id'])]:
            with self.subTest(key=key), patch.dict(target, {key: value}), self.assertRaises(M.SuiteError):
                wait()
        for key, value in [('scanned_at', None), ('scanned_at', sibling['scanned_at']),
                           ('scanned_depth', 'basic'), ('fields', ['id', 'id'])]:
            with self.subTest(key=key), patch.dict(target, {key: value}), \
                    patch.object(M, 'until', side_effect=lambda check: self.assertIsNone(check())):
                wait()
        snapshot['items'].append(target)
        with self.assertRaisesRegex(M.SuiteError, 'identity is duplicated'):
            wait()

    def test_scan_baseline_requires_the_exact_connected_ancestor_chain(self):
        snapshot, _, _, _ = self.scan_fixture()
        schema, root = snapshot['ancestors']
        for ancestors in [[schema], [root, schema], [schema, {**root, 'parent_node_id': schema['id']}],
                          [schema, {**root, 'tenant_id': 3}]]:
            with self.subTest(ancestors=ancestors), patch.dict(snapshot, {'ancestors': ancestors}), \
                    self.assertRaises(M.SuiteError):
                M.read_scan_catalog(10, 2)

    def test_suite_enables_auto_scan_only_after_complete_absent_target_baseline(self):
        snapshot, sibling, _, observer = self.scan_fixture()
        snapshot['items'] = [sibling]
        calls = []
        class FixtureClient:
            def request(self, method, path, expected, body=None, **kwargs):
                calls.append((method, path, body))
                if path == '/api/v1/meta/scan/run/manual':
                    raise AssertionError('log consumer must not prepare the source scan')
                if path == '/api/v1/meta/executions/' + UUID:
                    self_case.assertEqual(expected, (404,))
                    return SimpleNamespace(payload={'status': 'success'})
                if path == M.OWNER + '/task-definitions':
                    self_payload = body
                    if not self_payload.get('auto_scan_metadata'):
                        raise AssertionError('continuous fixture disabled automatic scanning')
                    raise M.SuiteError('stop after verified creation request')
                raise AssertionError(f'unexpected fixture operation: {method} {path}')
        report = {}
        class Preparer(FixtureClient):
            def request(self, method, path, expected, body=None, **kwargs):
                if path == '/api/v1/meta/executions/' + UUID:
                    self_case.assertEqual(expected, (200,))
                    return SimpleNamespace(payload={'status': 'success', 'triggered_by': 34})
                self_case.assertEqual(path, '/api/v1/meta/scan/run/manual')
                calls.append((method, path, body))
                return SimpleNamespace(payload={'execution_id': UUID})
        self_case = self
        with patch.object(M.REGISTRATION, 'source_grant_participants', return_value=('34', '31')), \
                patch.object(M.API, 'wait_meta_scan_tick') as tick, self.assertRaisesRegex(M.SuiteError, 'verified creation'):
            M.run_suite(FixtureClient(), None, None, FixtureClient(), None, None, None, 2, 'run-1', 10, 11,
                        report, lambda: None, None, preparer=Preparer())
        tick.assert_called_once()
        self.assertEqual(len(tick.call_args.args[0]), 3)
        self.assertEqual(report['cleanup'], 'passed')
        self.assertEqual(report['precise_scan_scope']['scans'], [])
        self.assertEqual(report['source_scan_preparation'], {'execution_id': UUID, 'principal_id': '34',
            'consumer_read_denied': True, 'peer_read_denied': True})
        before = calls[:-1]
        self.assertTrue(all(method == 'GET' or path.endswith('/manual') for method, path, _ in before))
        self.assertEqual(sum(path == '/api/v1/meta/executions/' + UUID for _, path, _ in before), 2)
        observer.assert_called_once()
        self.assertEqual(report['source_catalog_baseline'], {
            'source': 'hosted_postgres_readonly', 'state': 'identity_verified', 'matching_items': 1,
        })

    def test_schema_approval_preserves_distinct_users_and_rejects_cross_reads(self):
        initial_id = '00000000-0000-4000-8000-000000000002'
        scan_id = '00000000-0000-4000-8000-000000000003'
        fields = [{'source': 'scan_extra', 'target': 'scan_extra', 'target_type': 'string', 'nullable': True}]
        pending = {'id': 61, 'task_id': 57, 'execution_id': UUID, 'status': 'pending',
                   'approvable': True, 'suggested_fields': fields, 'from_revision': 1}
        applied = {'id': 61, 'status': 'applied', 'metadata_scan_status': 'success',
                   'metadata_scan_attempt': 1, 'to_revision': 2, 'metadata_scan_execution_id': scan_id}
        scan = {'execution_id': scan_id, 'status': 'success', 'triggered_by': 32}
        original = {'execution_id': UUID, 'status': 'failed', 'metadata': {}, 'trigger_by': 31}
        def scenario(*, result=None, scan_change=None, initial_actor=34, historical_actor=31,
                     historical_actor_after=31, source_visible_to_approver=False, approval_visible_to_initiator=False):
            calls = []
            class Actor:
                def __init__(self, principal): self.principal = principal
                def request(self, method, path, expected, body=None):
                    calls.append((self.principal, method, path, expected))
                    status = 200
                    if path == '/api/v1/meta/executions/' + initial_id:
                        status = 200 if self.principal == 34 or source_visible_to_approver else 404
                        payload = {'triggered_by': initial_actor}
                    elif path == '/api/v1/meta/executions/' + scan_id:
                        status = 200 if self.principal == 32 or approval_visible_to_initiator else 404
                        payload = {**scan, **(scan_change or {})}
                    elif path == M.OWNER + '/executions/' + UUID:
                        reads = sum(p == path for _, _, p, _ in calls)
                        payload = {**original, 'trigger_by': historical_actor if reads == 1 else historical_actor_after}
                    elif path == M.OWNER + '/task-definitions/57/schema-change':
                        payload = pending
                    else: raise AssertionError(path)
                    if status not in expected: raise M.SuiteError('unexpected cross-user read result')
                    return SimpleNamespace(payload=payload)
            initiator, approver = Actor(31), Actor(32)
            def approve(actor, path, body):
                self.assertIs(actor, approver)
                self.assertEqual(body, {'fields': fields})
                return {**applied, **(result or {})}
            output = M.apply_cdc_schema_approval(approver, 57, UUID, approve, initiator=initiator,
                initial_scan_id=initial_id, initial_reader=Actor(34), initial_principal='34',
                initiator_id='31', approver_id='32')
            return output, calls
        output, calls = scenario()
        self.assertEqual(output[2], scan_id)
        self.assertIn((32, 'GET', '/api/v1/meta/executions/' + initial_id, (404,)), calls)
        self.assertIn((31, 'GET', '/api/v1/meta/executions/' + scan_id, (404,)), calls)
        self.assertEqual(sum(path == M.OWNER + '/executions/' + UUID for _, _, path, _ in calls), 2)
        for change in [{'metadata_scan_status': 'failed'}, {'metadata_scan_attempt': 2}, {'to_revision': 1}, {'id': 62}]:
            with self.subTest(change=change), self.assertRaises(M.SuiteError): scenario(result=change)
        for change in [{'parent_execution_id': UUID}, {'triggered_by': 31}, {'status': 'failed'}]:
            with self.subTest(change=change), self.assertRaises(M.SuiteError): scenario(scan_change=change)
        for kwargs in [{'initial_actor': 31}, {'historical_actor': 32}, {'historical_actor_after': 32},
                       {'source_visible_to_approver': True}, {'approval_visible_to_initiator': True}]:
            with self.subTest(change=kwargs), self.assertRaises(M.SuiteError): scenario(**kwargs)

    def test_schema_approval_refuses_reusing_the_initiating_user(self):
        for approver in ('31', 31):
            with self.subTest(approver=approver), self.assertRaisesRegex(M.SuiteError, 'two distinct User'):
                M.apply_cdc_schema_approval(None, 57, UUID, None, initiator=None,
                    initial_scan_id=UUID, initial_reader=None, initial_principal='34', initiator_id='31', approver_id=approver)

    def test_scan_clock_crosses_latest_ancestor_tick_and_has_bounded_wait(self):
        stamps = [{'scanned_at': '1970-01-01T00:00:01Z'}, {'scanned_at': '1970-01-01T00:00:02Z'}]
        with patch.object(M.API.time, 'time', side_effect=[2.5, 3]), \
                patch.object(M.API.time, 'monotonic', return_value=0), patch.object(M.API.time, 'sleep') as sleep:
            M.API.wait_meta_scan_tick(stamps)
            sleep.assert_called_once_with(.05)
        with patch.object(M.API.time, 'time', return_value=2.5), \
                patch.object(M.API.time, 'monotonic', side_effect=[0, 31]), self.assertRaises(M.SuiteError):
            M.API.wait_meta_scan_tick(stamps)

    def test_browser_uses_private_controls_and_only_closed_public_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifacts, secrets = root / 'artifacts', root / 'private'
            artifacts.mkdir(); secrets.mkdir()
            subject = {'tenant_id': '2', 'principal_id': '33'}
            env = {'ADDP_ONLINE_ARTIFACT_DIR': str(artifacts),
                   'ADDP_ONLINE_TEST_USER_USERNAME': 'external-online-consumer',
                   'ADDP_ONLINE_TEST_USER_PASSWORD': 'private-password'}
            instances = []
            class Process:
                pid = 999999
                def __init__(self, args, **kwargs):
                    self.ended = False
                    self.env = kwargs['env']
                    control = Path(self.env['ADDP_CONTINUOUS_BROWSER_CONTROL'])
                    self.control = control
                    self.assert_args = args
                    instances.append(self)
                    marker = {'run_id': 'run-1', 'phase': 'live', 'execution_id': UUID}
                    for name in ('ready', 'live_progress_observed'):
                        (control / f'{name}.json').write_text(json.dumps(marker))
                    report = {'schema_version': 'addp.continuous-execution-browser/v1', 'phase': 'live',
                              'run_id': 'run-1', 'tenant_id': '2', 'principal_id': '33',
                              'execution_ids': [UUID], 'checks': ['timeline', 'automatic_committed_progress',
                                  'cursor_and_refresh_match', 'same_page_terminal_convergence', 'no_samples'], 'result': 'passed'}
                    (artifacts / 'continuous-browser-live.json').write_text(json.dumps(report))
                def poll(self): return 0 if self.ended else None
                def wait(self, timeout):
                    assert json.loads((self.control / 'release.json').read_text())['execution_id'] == UUID
                    self.ended = True
                    return 0
            action = []
            def finish():
                self.assertTrue((instances[-1].control / 'release.json').is_file())
                action.append('finish')
            with patch.dict(os.environ, env), patch.object(M.subprocess, 'Popen', Process):
                result = M.run_browser('live', [UUID], subject, 'run-1', secrets, lambda: action.append('publish'), finish=finish)
            self.assertEqual(action, ['publish', 'finish'])
            self.assertEqual(result['principal_id'], '33')
            self.assertIn('--output=' + str(instances[0].control / 'playwright'), instances[0].assert_args)
            self.assertFalse(instances[0].control.exists())
            self.assertNotIn('private-password', (artifacts / 'continuous-browser-live.json').read_text())
            for change in [{'principal_id': '999'}, {'execution_ids': ['other']}, {'checks': []},
                           {'access_token': 'private'}, {'raw_response': {}}]:
                class Incorrect(Process):
                    def __init__(self, args, **kwargs):
                        super().__init__(args, **kwargs)
                        path = artifacts / 'continuous-browser-live.json'
                        payload = json.loads(path.read_text()); payload.update(change)
                        path.write_text(json.dumps(payload))
                with self.subTest(change=change), patch.dict(os.environ, env), patch.object(M.subprocess, 'Popen', Incorrect), self.assertRaises(M.SuiteError):
                    M.run_browser('live', [UUID], subject, 'run-1', secrets, lambda: None, finish=finish)
            self.assertFalse(list(secrets.iterdir()))

    def test_browser_rejects_invalid_phase_identity_and_action_before_launch(self):
        with patch.object(M.subprocess, 'Popen') as launch:
            for phase, ids, action in [('unknown', [UUID], None), ('live', [UUID], None), ('live', [UUID], lambda: None),
                                       ('revocation', ['other'], lambda: None), ('recovery', [UUID], None)]:
                with self.subTest(phase=phase), self.assertRaises(M.SuiteError):
                    M.run_browser(phase, ids, {}, 'run-1', '/not-used', action)
            launch.assert_not_called()

    def test_drains_cursor_pages_without_offset_payloads(self):
        items = [fact(4, 'started'), fact(9), fact(12, 'cancelled')]
        client = Client([page(items[:2], True), page(items[2:])])
        evidence = {}
        self.assertEqual(M.events(client, M.MONITOR, UUID, evidence=evidence), items)
        self.assertEqual(evidence, {'page_size': 2, 'page_count': 2, 'event_count': 3,
                                    'first_cursor': 9, 'last_cursor': 12})
        self.assertIn('after=9&limit=2', client.calls[1][1])
        M.lifecycle_events(items, 'cancelled')

    def test_live_cursor_continues_from_pre_commit_tail(self):
        before = [fact(4, 'started'), fact(9)]
        after = fact(13)
        client = Client([page(before), page([after])])
        observations = []
        def publish():
            observations.append(len(client.calls))
        evidence = {}
        self.assertEqual(M.events(client, M.MONITOR, UUID, after_first=publish, evidence=evidence), [*before, after])
        self.assertEqual(observations, [1])
        self.assertTrue(client.calls[1][1].endswith('after=9&limit=2'))
        self.assertEqual(evidence, {'page_size': 2, 'page_count': 2, 'event_count': 3,
                                    'first_cursor': 9, 'last_cursor': 13})

    def test_rejects_unknown_fields_counters_samples_attempts_and_cursor_replay(self):
        base = page([fact(1, 'started')])
        mutations = [lambda p: p['items'][0].update(execution_config={}),
                     lambda p: p['items'][0]['counters'].update(next_offset=1),
                     lambda p: p['items'][0]['counters'].update(records_read=-1),
                     lambda p: p['items'][0]['counters'].update(records_read=True),
                     lambda p: p['items'][0].update(step_id='business-value-sentinel-invalid'),
                     lambda p: p['items'][0].update(execution_id='other'),
                     lambda p: p['items'][0].update(attempt=0),
                     lambda p: p.update(next_cursor=0),
                     lambda p: p.update(has_more=1),
                     lambda p: p.update(retained_after='yesterday'),
                     lambda p: p['items'][0].update(occurred_at='yesterday'),
                     lambda p: p['items'][0].update(occurred_at='2026-10-12T01:00:00'),
                     lambda p: p['items'][0].update(occurred_at='2026-09-01T01:00:00Z'),
                     lambda p: p['items'].append(deepcopy(p['items'][0]))]
        for mutate in mutations:
            p = deepcopy(base)
            mutate(p)
            with self.subTest(p=p), self.assertRaises(M.SuiteError):
                M.events(Client([p]), M.MONITOR, UUID)

    def test_running_lifecycle_requires_committed_progress_without_terminal_events(self):
        running = [fact(1, 'started'), fact(2)]
        M.lifecycle_events(running)
        for terminal in ['completed', 'failed', 'cancelled', 'timeout']:
            with self.subTest(terminal=terminal), self.assertRaises(M.SuiteError):
                M.lifecycle_events(running + [fact(3, terminal)])

    def test_requires_worker_progress_and_one_terminal(self):
        valid = [fact(1, 'started'), fact(2), fact(3, 'failed')]
        M.lifecycle_events(valid, 'failed')
        for invalid in [valid[1:], valid[:1] + valid[2:], valid + [fact(4, 'failed')], valid + [fact(4)]]:
            with self.assertRaises(M.SuiteError):
                M.lifecycle_events(invalid, 'failed')

    def test_diagnostic_excludes_ownership_credentials_and_business_values(self):
        for key in M.PRIVATE:
            with self.subTest(key=key), self.assertRaises(M.SuiteError):
                M.assert_safe({'continuous': {key: 'private'}})
        with self.assertRaises(M.SuiteError):
            M.assert_safe({'error_details': {'message': 'business-value-sentinel-unknown'}})
        M.assert_safe({'continuous': {'partitions': {'0': {'values': {'next_offset': '12'}}}}})
        item = {'execution_id': UUID, 'module': 'transfer', 'task_type': 'sync',
                'execution_boundary': 'continuous', 'queue_duration_ms': 10, 'run_duration_ms': 20,
                'lease_state': 'none', 'metadata': {'continuous': {'diagnostics': {'health': 'healthy'}}}}
        M.assert_projection(item, UUID, monitor=True, tenant=2)
        item['metadata']['arbitrary_response'] = {}
        with self.assertRaises(M.SuiteError):
            M.assert_projection(item, UUID, monitor=True, tenant=2)

    def test_monitor_continuous_projection_accepts_observation_not_private_runtime(self):
        item = {'execution_id': UUID, 'module': 'transfer', 'task_type': 'sync',
                'execution_boundary': 'continuous', 'queue_duration_ms': 12, 'run_duration_ms': 340,
                'lease_state': 'none', 'recovery_reason': 'lease_expired', 'metadata': {}}
        M.assert_projection(item, UUID, monitor=True, tenant=2)
        mutations = [lambda p: p.update(queue_duration_ms=-1),
                     lambda p: p.update(run_duration_ms=True),
                     lambda p: p.pop('queue_duration_ms'),
                     lambda p: p.update(lease_state='active'),
                     lambda p: p.update(lease_owner='private-worker'),
                     lambda p: p.update(lease_expires_at='2026-10-12T01:00:00Z'),
                     lambda p: p.update(fencing_token=1),
                     lambda p: p.update(runtime_response={}),
                     lambda p: p.update(unreviewed_field='private')]
        for mutate in mutations:
            projection = deepcopy(item)
            mutate(projection)
            with self.subTest(mutation=mutate), self.assertRaises(M.SuiteError):
                M.assert_projection(projection, UUID, monitor=True, tenant=2)

    def test_source_configs_preserve_distinct_business_kafka_and_cdc_contracts(self):
        kafka = M.task_config('kafka', 10, 11)
        cdc = M.task_config('cdc', 10, 11)
        normal = M.task_config('kafka-graceful', 10, 11)
        self.assertEqual(normal['source'], kafka['source'])
        self.assertEqual(normal['target']['name'], 'continuous_kafka_graceful_target')
        self.assertEqual(normal['runtime']['record_failure']['mode'], 'dead_letter')
        self.assertEqual(kafka['runtime']['record_failure']['mode'], 'block')
        self.assertEqual(kafka['source']['locator'], 'addp://engine/11/path/continuous_events?type=topic')
        self.assertEqual(kafka['source']['change_stream']['start'], {'mode': 'committed', 'initial': 'earliest'})
        self.assertEqual(cdc['load']['change_detection'], {'type': 'cdc', 'bootstrap': 'initial_snapshot'})
        self.assertEqual(cdc['target']['policy'], {'apply_mode': 'upsert_delete', 'keys': ['id']})
        for config in (kafka, cdc):
            self.assertEqual(config['runtime'], {'boundary': 'continuous', 'record_failure': {'mode': 'block'}})
            self.assertNotIn('connection_info', str(config))

    def test_failed_physical_truth_never_publishes_samples_or_sql_output(self):
        with patch.object(M.subprocess, 'run', return_value=SimpleNamespace(returncode=1, stdout=b'sensitive', stderr=b'sensitive')) as run:
            self.assertFalse(M.physical('verify-cdc-mutated'))
            self.assertTrue(run.call_args.kwargs['capture_output'])
        with self.assertRaises(M.SuiteError):
            M.physical('drop-all')

    def test_denial_covers_owner_detail_progress_events_monitor_detail_events_tree(self):
        client = Client([{'error': 'denied'}] * 6)
        M.invisible(client, UUID, 403, 404, cursor=9)
        self.assertEqual(len(client.calls), 6)
        self.assertEqual([call[2] for call in client.calls], [(403,)] * 3 + [(404,)] * 3)
        event_paths = [call[1] for call in client.calls if '/events?' in call[1]]
        self.assertEqual(len(event_paths), 2)
        self.assertTrue(all('after=9&limit=2' in path for path in event_paths))
        client = Client([{'execution_id': UUID}])
        with self.assertRaises(M.SuiteError):
            M.invisible(client, UUID, 403, 404)

    def test_hidden_monitor_total_requires_a_closed_empty_page_and_integer_zero(self):
        empty = {'executions': [], 'total': 0, 'page': 1, 'page_size': 1}
        self.assertEqual(M.hidden_monitor_total(Client([empty])), 0)
        for changed in [{'total': 1}, {'total': False}, {'total': 0.0}, {'executions': [{'execution_id': UUID}]},
                        {'page': True}, {'page_size': 20}, {'execution_config': {}}]:
            with self.subTest(changed=changed), self.assertRaises(M.SuiteError):
                M.hidden_monitor_total(Client([{**empty, **changed}]))

    def test_owner_revocation_uses_formal_iam_and_fresh_monitor_only_session(self):
        context = {'principal': {'type': 'user', 'id': '17'},
                   'context': {'type': 'tenant', 'tenant_id': '2', 'tenant_membership_id': '27'}}
        assignment = {'id': '37', 'principal_type': 'user', 'principal_id': '17', 'membership_id': '27',
                      'username': 'external-online-transfer-reader', 'role_key': 'online.external_online_transfer_reader',
                      'scope_type': 'tenant', 'status': 'active'}
        admin_context = {'principal': {'type': 'user', 'id': '99'},
                         'context': {'type': 'tenant', 'tenant_id': '2'},
                         'authorization': {'role_assignments': [{'role_key': 'tenant.administrator',
                             'permissions': ['iam.tenant_role_assignment.read', 'iam.tenant_role_assignment.revoke']}]}}
        fresh_context = {**context, 'authorization': {'role_assignments': [
            {'role_key': 'online.execution_monitor_reader', 'permissions': ['monitor.execution.read']}]}}
        items = [fact(4, 'started'), fact(9), fact(12, 'failed')]
        pages = [page(items[:2], True), page(items[2:])]
        reader, admin = Client([context] + pages * 2 + [{'error': 'denied'}] * 6), Client([admin_context, assignment])
        fresh = Client([fresh_context, fresh_context] + [{'error': 'denied'}] * 6 +
                       [{'executions': [], 'total': 0, 'page': 1, 'page_size': 1}])
        reader.base_url, reader.timeout, reader.token = 'http://localhost:8000', 10, 'old-token'
        mutations = []
        def mutate(actor, path, body=None, expected=(200,)):
            mutations.append(path)
            if path.endswith('/revoke'):
                return {**assignment, 'status': 'revoked'}
            return {'next_action': 'session_issued', 'session': {'access_token': 'fresh-token'}}
        with patch.dict(os.environ, {'ADDP_ONLINE_PARENT_ASSIGNMENT_ID': '37',
                'ADDP_ONLINE_PARENT_USER_USERNAME': 'external-online-transfer-reader',
                'ADDP_ONLINE_PARENT_USER_PASSWORD': 'private'}), patch.object(M.API, 'validate_user_identity', return_value={}), \
                patch.object(M.API, 'GatewayClient', return_value=fresh):
            receipt = M.revoke(reader, admin, object(), 2, UUID, mutate)
        self.assertEqual(mutations, ['/api/v1/system/tenant/role_assignments/37/revoke', '/api/v1/system/login'])
        self.assertEqual([call[2] for call in reader.calls[5:]], [(401,)] * 6)
        self.assertEqual([call[2] for call in fresh.calls[2:8]], [(403,)] * 3 + [(404,)] * 3)
        self.assertTrue(all('after=9&limit=2' in call[1] for call in reader.calls[5:] if '/events?' in call[1]))
        self.assertTrue(all('after=9&limit=2' in call[1] for call in fresh.calls[2:8] if '/events?' in call[1]))
        self.assertEqual(receipt['principal_id'], '17')
        self.assertEqual(receipt['execution_id'], UUID)
        self.assertEqual(receipt['owner_pages'], receipt['monitor_pages'])
        self.assertEqual(receipt['monitor_pages']['page_count'], 2)
        self.assertEqual(receipt['fresh_monitor_only_total'], 0)
        self.assertTrue(receipt['saved_cursor_denied_with_old_token'] and receipt['saved_cursor_denied_with_fresh_token'])

    def test_revocation_requires_real_multiple_pages_before_mutating_iam(self):
        context = {'principal': {'id': '17'}, 'context': {'tenant_membership_id': '27'}}
        one_page = page([fact(4, 'started'), fact(9, 'failed')])
        reader = Client([context, one_page, one_page])
        with patch.object(M.API, 'validate_user_identity', return_value={}), \
                self.assertRaisesRegex(M.SuiteError, 'real multi-page'):
            M.revoke(reader, None, None, 2, UUID, lambda *args: self.fail('must not mutate IAM'))

    def test_manual_only_registry_and_workflow_use_owned_hosted_lifecycle(self):
        gate = import_module('scripts.test.online-gate')
        suite = gate.SUITES[M.SUITE]
        self.assertFalse(suite.nightly)
        self.assertEqual({name for name, _ in suite.services}, {'gateway', 'system', 'meta', 'security', 'transfer', 'monitor'})
        workflow = (M.ROOT / '.github/workflows/online-t4-gates.yml').read_text()
        self.assertIn("inputs.suite != 'transfer-continuous-events'", workflow)
        job = workflow.split('  continuous-hosted-t4:', 1)[1].split('\n  redis-hosted-t4:', 1)[0]
        for fragment in ["inputs.suite == 'transfer-continuous-events'", 'runs-on: ubuntu-24.04',
                         'online-hosted-continuous-gate.sh --check-only', 'ADDP_ONLINE_SECRET_DIR:', 'if: always()']:
            self.assertIn(fragment, job)
        self.assertNotIn('secrets.', job)
        self.assertIn('test-transfer-continuous-online-runner', (M.ROOT / 'Makefile').read_text())
        hosted = (M.ROOT / 'scripts/test/online-hosted-continuous-gate.sh').read_text()
        self.assertIn('playwright install --with-deps chromium', hosted)
        makefile = (M.ROOT / 'Makefile').read_text().split('test-transfer-continuous-online-runner:', 1)[1].split('\n\n', 1)[0]
        self.assertIn('node --check --input-type=module < console/frontend/e2e/online/continuous-execution-events.spec.js', makefile)
        browser = (M.ROOT / 'console/frontend/e2e/online/continuous-execution-events.spec.js').read_text()
        self.assertNotIn('page.route(', browser)
        self.assertIn('setOffline(true)', browser)
        self.assertIn("body.error_code).toBe('execution_owner_unavailable')", browser)
        self.assertIn("signal('owner_failure_observed')", browser)
        self.assertIn("checks.push('old_list_and_total_cleared')", browser)
        self.assertIn('transfer-browser-support.js', browser)
        self.assertIn("localStorage.setItem('addp-lang', 'zh-cn')", browser)
        self.assertIn("locale: 'zh-CN'", browser)


class CDCTaskObservationTest(unittest.TestCase):
    def task(self):
        now = M.datetime.now(M.timezone.utc)
        times = {'sampled_at': (now - timedelta(seconds=1)).isoformat(),
                 'expires_at': (now + timedelta(seconds=29)).isoformat()}
        pg = {'status': 'available', **times, 'slot_active': True, 'wal_status': 'reserved',
              'retained_wal_bytes': '9007199254740993', 'unacknowledged_wal_bytes': '42',
              'safe_wal_size_bytes': None, 'wal_keep_unlimited': True, 'risk_state': 'reserved'}
        delay = {'status': 'available', **times, 'connected': True, 'delay_seconds': None}
        buffer = {'status': 'available', **times, 'earliest_offset': '0', 'latest_offset': '4',
                  'committed_offset': '4', 'lag_offsets': '0', 'recovery_state': 'available'}
        return {'id': 7, 'last_execution_id': UUID, 'desired_state': 'running',
                'config': {'source': {'locator': 'addp://engine/10/path/public/continuous_source?type=table'}},
                'capture': {'source_type': 'postgresql', 'generation': 1, 'status': 'running',
                    'postgresql_observation': pg, 'delay_observation': delay, 'buffer_observation': buffer}}

    def native(self, task, ms=-1):
        pg = deepcopy(task['capture']['postgresql_observation'])
        return ({'identity_matches': True, **{key: pg[key] for key in ['slot_active', 'wal_status',
                 'retained_wal_bytes', 'unacknowledged_wal_bytes', 'safe_wal_size_bytes', 'wal_keep_unlimited']}},
                {'Connected': True, 'MilliSecondsBehindSource': ms})

    def test_current_task_sample_preserves_exact_bytes_and_no_event_delay(self):
        task = self.task()
        sample = M.cdc_task_sample(Client([task]), 7, 10, 1)
        self.assertEqual(sample['source']['retained_wal_bytes'], '9007199254740993')
        self.assertIsNone(sample['delay']['delay_seconds'])
        self.assertEqual(sample['buffer']['lag_offsets'], '0')

    def test_old_generation_foreign_task_source_and_private_fields_are_rejected(self):
        for mutation in ['generation', 'task', 'source', 'private', 'nested_private']:
            task = self.task()
            if mutation == 'generation': task['capture']['generation'] = 2
            elif mutation == 'task': task['id'] = 8
            elif mutation == 'source': task['config']['source']['locator'] = 'addp://engine/11/path/public/continuous_source?type=table'
            elif mutation == 'private': task['capture']['connector_name'] = 'private-connector'
            else: task['capture']['delay_observation']['error'] = 'private-error'
            with self.subTest(mutation=mutation), self.assertRaises(M.SuiteError):
                M.cdc_task_sample(Client([task]), 7, 10, 1)

    def test_each_observation_must_be_fresh_and_post_phase(self):
        for field in ['postgresql_observation', 'delay_observation', 'buffer_observation']:
            task = self.task()
            task['capture'][field]['expires_at'] = task['capture'][field]['sampled_at']
            with self.subTest(field=field), self.assertRaises(M.SuiteError):
                M.cdc_task_sample(Client([task]), 7, 10, 1)
            task = self.task()
            value = task['capture'][field]
            value['sampled_at'] = '2020-01-01T00:00:00Z'; value['expires_at'] = '2020-01-01T00:00:30Z'
            self.assertIsNone(M.cdc_task_sample(Client([task]), 7, 10, 1))
        self.assertIsNone(M.cdc_task_sample(Client([self.task()]), 7, 10, 1, after=M.datetime.now(M.timezone.utc)))

    def test_byte_and_buffer_precision_and_arithmetic_fail_closed(self):
        for field, value in [('retained_wal_bytes', 42), ('retained_wal_bytes', '-1'),
                             ('safe_wal_size_bytes', '18446744073709551616')]:
            task = self.task(); task['capture']['postgresql_observation'][field] = value
            with self.subTest(field=field, value=value), self.assertRaises(M.SuiteError):
                M.cdc_task_sample(Client([task]), 7, 10, 1)
        task = self.task(); task['capture']['buffer_observation']['lag_offsets'] = '1'
        with self.assertRaises(M.SuiteError): M.cdc_task_sample(Client([task]), 7, 10, 1)

    def test_exact_native_delay_sentinel_negative_and_changed_event_sample(self):
        for ms in [-1, -2, 125]:
            task = self.task(); task['capture']['delay_observation']['delay_seconds'] = None if ms == -1 else ms / 1000
            binding = {'slot': 'owned_slot', 'publication': 'owned_pub', 'connector': 'owned-connector'}
            with patch.object(M, 'cdc_capture_binding', return_value=binding), \
                    patch.object(M, 'cdc_native_observation', return_value=self.native(task, ms)):
                sample = M.cdc_observation(Client([task]), 7, 2, 10, 1)
                self.assertTrue(sample['exact_connector_delay_verified'])
                self.assertNotIn('owned', json.dumps(sample))
                value = M.cdc_observation(Client([task]), 7, 2, 10, 1, event_delay=True)
                self.assertEqual(value is None, ms == -1)
        task = self.task()
        with patch.object(M, 'cdc_capture_binding', return_value=binding), \
                patch.object(M, 'cdc_native_observation', return_value=self.native(task, 125)):
            self.assertIsNone(M.cdc_observation(Client([task]), 7, 2, 10, 1))

    def test_sample_expiring_during_independent_native_reads_cannot_pass(self):
        task=self.task();native=self.native(task);start=M.datetime.now(M.timezone.utc)
        class Clock(M.datetime):
            calls=0
            @classmethod
            def now(cls,tz=None):
                cls.calls+=1
                return start if cls.calls==1 else start+timedelta(seconds=31)
        with patch.object(M,'datetime',Clock), patch.object(M,'cdc_capture_binding',return_value={
                'slot':'owned_slot','publication':'owned_pub','connector':'owned-connector'}), \
                patch.object(M,'cdc_native_observation',return_value=native):
            self.assertIsNone(M.cdc_observation(Client([task]),7,2,10,1))

    def test_native_wal_state_and_headroom_must_agree_without_equating_lag_to_bytes(self):
        task = self.task(); native = self.native(task)
        native[0]['retained_wal_bytes'] = '9007199254741000'  # WAL can advance after the task sample.
        with patch.object(M, 'cdc_capture_binding', return_value={'slot':'owned_slot', 'publication':'owned_pub', 'connector':'owned-connector'}), \
                patch.object(M, 'cdc_native_observation', return_value=native):
            self.assertIsNotNone(M.cdc_observation(Client([task]), 7, 2, 10, 1))
            native[0]['wal_status'] = 'lost'
            with self.assertRaises(M.SuiteError): M.cdc_observation(Client([task]), 7, 2, 10, 1)
            native[0]['wal_status'] = 'reserved'; native[0]['safe_wal_size_bytes'] = '0'
            with self.assertRaises(M.SuiteError): M.cdc_observation(Client([task]), 7, 2, 10, 1)

    def test_all_native_wal_risk_states_preserve_their_exact_meaning(self):
        for risk in ['reserved','extended','unreserved','lost']:
            task = self.task()
            task['capture']['postgresql_observation'].update(wal_status=risk,risk_state=risk)
            with self.subTest(risk=risk), patch.object(M,'cdc_capture_binding',return_value={
                    'slot':'owned_slot','publication':'owned_pub','connector':'owned-connector'}), \
                    patch.object(M,'cdc_native_observation',return_value=self.native(task)):
                self.assertEqual(M.cdc_observation(Client([task]),7,2,10,1)['source']['risk_state'],risk)
        task = self.task();task['capture']['postgresql_observation']['risk_state']='lost'
        with self.assertRaises(M.SuiteError):M.cdc_task_sample(Client([task]),7,10,1)

    def test_binding_reads_only_exact_owned_generation_in_pinned_hosted_database(self):
        env = {'ONLINE_SUITE': M.SUITE, 'ADDP_ONLINE_TEST_TENANT_ID':'2',
               'ADDP_ONLINE_SCAN_POSTGRES_ID':'a'*64, 'ADDP_ONLINE_CONTINUOUS_PG_ENGINE_ID':'10'}
        postgres = import_module('scripts.test.execution-retention-readonly-online')
        binding = {'slot':'owned_slot', 'publication':'owned_pub', 'connector':'owned-connector'}
        with patch.dict(os.environ, env), patch.object(postgres, 'sql', return_value=json.dumps(binding)) as sql:
            self.assertEqual(M.cdc_capture_binding(2, 7, 10, 1), binding)
            self.assertEqual(sql.call_args.kwargs, {'expected_suite':M.SUITE, 'postgres_id':'a'*64,
                'variables':{'tenant':2,'task':7,'engine':10,'generation':1}})
            query = sql.call_args.args[0]
            for text in ['BEGIN READ ONLY', 'p.slot_owned AND p.publication_owned', "r.generation=:'generation'", "r.source_engine_id=:'engine'", "r.source_table='continuous_source'"]:
                self.assertIn(text, query)
            for args in [(1,7,10,1),(2,7,11,1),(2,7,10,0)]:
                with self.assertRaises(M.SuiteError): M.cdc_capture_binding(*args)
            sql.assert_called_once()

    def test_native_probe_pins_connect_and_reads_only_exact_two_attributes(self):
        binding = {'slot':'owned_slot', 'publication':'owned_pub', 'connector':'owned-connector'}
        container = {'Id':'c'*64,'State':{'Running':True},'Config':{'Labels':{
            'com.docker.compose.project':'addp-infra','com.docker.compose.service':'kafka-connect',
            'com.docker.compose.project.working_dir':str(M.ROOT)}}}
        task = self.task(); pg, value = self.native(task, 125)
        bean = 'debezium.postgres:context=streaming,server=owned-connector,type=connector-metrics'
        replies = [json.dumps([container]),json.dumps(pg),json.dumps({'status':200,'request':{'mbean':bean},'value':value})]
        with patch.dict(os.environ, {'ADDP_ONLINE_CDC_CONNECT_ID':'c'*64}), \
                patch.object(M.subprocess,'run',side_effect=[SimpleNamespace(stdout=v) for v in replies]) as run:
            self.assertEqual(M.cdc_native_observation(binding),(pg,value))
            self.assertEqual(run.call_args_list[1].args[0][-3:],['observe-cdc-source','owned_slot','owned_pub'])
            command = run.call_args.args[0]
            self.assertEqual(command[:4],['docker','exec','-i','c'*64])
            request = json.loads(run.call_args.kwargs['input'])
            self.assertEqual(request,{'type':'read','mbean':bean,'attribute':['Connected','MilliSecondsBehindSource']})
            self.assertNotIn('Authorization',json.dumps(command))
        container['Config']['Labels']['com.docker.compose.project'] = 'foreign'
        with patch.dict(os.environ, {'ADDP_ONLINE_CDC_CONNECT_ID':'c'*64}), \
                patch.object(M.subprocess,'run',return_value=SimpleNamespace(stdout=json.dumps([container]))) as run, \
                self.assertRaises(M.SuiteError):
            M.cdc_native_observation(binding)
        run.assert_called_once()

    def test_native_source_or_connector_response_from_another_identity_is_rejected(self):
        binding = {'slot':'owned_slot','publication':'owned_pub','connector':'owned-connector'}
        container = {'Id':'c'*64,'State':{'Running':True},'Config':{'Labels':{
            'com.docker.compose.project':'addp-infra','com.docker.compose.service':'kafka-connect',
            'com.docker.compose.project.working_dir':str(M.ROOT)}}}
        for wrong in ['source','connector']:
            pg, value = self.native(self.task(),125)
            if wrong=='source':pg['identity_matches']=False
            bean='debezium.postgres:context=streaming,server='+('other' if wrong=='connector' else 'owned-connector')+',type=connector-metrics'
            replies=[json.dumps([container]),json.dumps(pg),json.dumps({'status':200,'request':{'mbean':bean},'value':value})]
            with self.subTest(wrong=wrong), patch.dict(os.environ,{'ADDP_ONLINE_CDC_CONNECT_ID':'c'*64}), \
                    patch.object(M.subprocess,'run',side_effect=[SimpleNamespace(stdout=v)for v in replies]), \
                    self.assertRaises(M.SuiteError):
                M.cdc_native_observation(binding)

    def test_pause_resume_orchestration_requires_unchanged_target_and_two_new_zero_samples(self):
        task = self.task(); initial = M.cdc_task_sample(Client([task]),7,10,1)
        paused = deepcopy(initial); paused['desired_state']='paused'
        growth = deepcopy(paused); growth['buffer'].update(latest_offset='7',lag_offsets='3')
        resumed = deepcopy(initial); resumed['execution_id']='bbbbbbbb-2222-4222-8222-222222222222'
        resumed['buffer'].update(latest_offset='7',committed_offset='7')
        sustained = deepcopy(resumed)
        calls=[]; phases=[]
        def mutate(actor,path): calls.append(path)
        def launch(task_id,action): calls.append(action); return resumed['execution_id']
        with patch.object(M,'cdc_observation',side_effect=[initial,paused,growth,resumed,sustained]) as observe, \
                patch.object(M,'wait_status') as wait, patch.object(M,'physical',return_value=True) as physical:
            execution, proof = M.verify_cdc_task_monitoring(None,7,2,10,UUID,launch,mutate,phases.append)
        self.assertEqual(calls,[M.OWNER+'/task-definitions/7/pause','resume'])
        wait.assert_called_once_with(None,UUID,'cancelled')
        self.assertEqual([c.args[0] for c in physical.call_args_list],
                         ['mutate-cdc','verify-cdc-initial','verify-cdc-mutated','verify-cdc-mutated'])
        self.assertEqual(execution,resumed['execution_id']);self.assertEqual(proof['event_count'],3)
        self.assertEqual(list(proof['samples']),['initial','paused','growth','resumed','sustained'])
        self.assertEqual(observe.call_count,5)
        self.assertTrue(all(c.kwargs.get('after') is not None for c in observe.call_args_list[1:]))
        self.assertEqual(phases,['cdc_monitor_initial','cdc_pause','cdc_mutation','cdc_buffer_growth','cdc_monitor_resume'])

    def test_pause_changed_commit_target_or_reused_execution_cannot_pass(self):
        initial = M.cdc_task_sample(Client([self.task()]),7,10,1)
        paused = deepcopy(initial);paused['desired_state']='paused'
        growth = deepcopy(paused);growth['buffer'].update(latest_offset='7',lag_offsets='3')
        for case in ['commit','target','execution']:
            bad = deepcopy(growth)
            if case=='commit':bad['buffer']['committed_offset']='5'
            if case=='execution':bad['execution_id']='bbbbbbbb-2222-4222-8222-222222222222'
            with self.subTest(case=case), patch.object(M,'cdc_observation',side_effect=[initial,paused,bad]), \
                    patch.object(M,'wait_status'), patch.object(M,'physical',side_effect=[True,case!='target']), \
                    self.assertRaises(M.SuiteError):
                M.verify_cdc_task_monitoring(None,7,2,10,UUID,lambda *args:self.fail('must not resume'),lambda *args:None,lambda *args:None)


if __name__ == '__main__':
    unittest.main()
