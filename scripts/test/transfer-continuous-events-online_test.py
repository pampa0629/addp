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
                  'scan_status': 'success', 'scanned_depth': 'deep', 'scanned_at': sibling['scanned_at']}
        root = {**schema, 'id': 1, 'parent_node_id': None}
        target = {**sibling, 'id': 6, 'full_name': 'public.continuous_cdc_target', 'fingerprint': 'target',
                  'scanned_at': '2026-10-16T09:00:02Z', 'attributes': {'type_info': {'table': {
                      'fields': [{'name': 'id'}, {'name': 'label'}]}}}}
        facts = {'/api/v1/meta/items/5': sibling, '/api/v1/meta/nodes/9': schema,
                 '/api/v1/meta/nodes/1': root, '/api/v1/meta/engines/10/items': [sibling, target]}
        class CatalogClient:
            def request(self, method, path, expected, **kwargs):
                assert method == 'GET' and expected == (200,)
                return SimpleNamespace(payload=deepcopy(facts[path]))
        return CatalogClient(), facts, sibling, target

    def test_target_scan_requires_fresh_deep_schema_and_preserves_outside_facts(self):
        client, facts, sibling, target = self.scan_fixture()
        boundary = M.API.read_meta_scan_boundary(client, 10, sibling)
        proof = M.wait_target_scan(client, 10, 2, 'continuous_cdc_target', {'id', 'label'}, sibling, boundary)
        self.assertEqual(proof['target_locator'], 'addp://engine/10/path/public/continuous_cdc_target?type=table')
        self.assertEqual(proof['item_id'], 6)
        # A completed task or old item alone is not evidence of the new schema scan.
        with patch.object(M, 'until', side_effect=lambda check: self.assertFalse(check())):
            M.wait_target_scan(client, 10, 2, 'continuous_cdc_target', {'id', 'label', 'scan_extra'}, sibling, boundary)
        target['attributes']['type_info']['table']['fields'].append({'name': 'scan_extra'})
        migrated = M.wait_target_scan(client, 10, 2, 'continuous_cdc_target', {'id', 'label', 'scan_extra'}, sibling, boundary)
        self.assertEqual(migrated['fields'], ['id', 'label', 'scan_extra'])
        for path, field, value in [('/api/v1/meta/items/5', 'scanned_at', '2026-10-16T09:00:03Z'),
                                   ('/api/v1/meta/nodes/9', 'scan_status', 'running'),
                                   ('/api/v1/meta/nodes/1', 'scanned_depth', 'basic')]:
            old = facts[path][field]; facts[path][field] = value
            with self.subTest(field=field), self.assertRaisesRegex(M.SuiteError, 'sibling facts or ancestor'):
                M.wait_target_scan(client, 10, 2, 'continuous_cdc_target', {'id', 'label', 'scan_extra'}, sibling, boundary)
            facts[path][field] = old

    def test_target_scan_rejects_wrong_identity_duplicates_and_incomplete_scan(self):
        for key, value in [('tenant_id', 3), ('engine_id', 11), ('node_id', 99), ('item_type', 'file'), ('fingerprint', '')]:
            client, facts, sibling, target = self.scan_fixture()
            boundary = M.API.read_meta_scan_boundary(client, 10, sibling)
            target[key] = value
            with self.subTest(key=key), self.assertRaises(M.SuiteError):
                M.wait_target_scan(client, 10, 2, 'continuous_cdc_target', {'id', 'label'}, sibling, boundary)
        for key, value in [('scanned_at', None), ('scanned_depth', 'basic')]:
            client, facts, sibling, target = self.scan_fixture()
            boundary = M.API.read_meta_scan_boundary(client, 10, sibling)
            target[key] = value
            with self.subTest(key=key), patch.object(M, 'until', side_effect=lambda check: self.assertFalse(check())):
                M.wait_target_scan(client, 10, 2, 'continuous_cdc_target', {'id', 'label'}, sibling, boundary)
        client, facts, sibling, target = self.scan_fixture()
        facts['/api/v1/meta/engines/10/items'].append(target)
        with self.assertRaises(M.SuiteError):
            M.catalog_item(client, 10, 2, 'continuous_cdc_target')

    def test_suite_enables_auto_scan_only_after_complete_absent_target_baseline(self):
        catalog, facts, sibling, target = self.scan_fixture()
        facts['/api/v1/meta/engines/10/items'] = [sibling]
        calls = []
        class FixtureClient:
            def request(self, method, path, expected, body=None, **kwargs):
                calls.append((method, path, body))
                if path == '/api/v1/meta/scan/run/manual':
                    return SimpleNamespace(payload={'execution_id': UUID})
                if path == '/api/v1/meta/executions/' + UUID:
                    return SimpleNamespace(payload={'status': 'success'})
                if path == M.OWNER + '/task-definitions':
                    self_payload = body
                    if not self_payload.get('auto_scan_metadata'):
                        raise AssertionError('continuous fixture disabled automatic scanning')
                    raise M.SuiteError('stop after verified creation request')
                return catalog.request(method, path, expected, **kwargs)
        report = {}
        with patch.object(M.API, 'wait_meta_scan_tick') as tick, self.assertRaisesRegex(M.SuiteError, 'verified creation'):
            M.run_suite(FixtureClient(), None, None, None, None, None, None, 2, 'run-1', 10, 11,
                        report, lambda: None, None)
        tick.assert_called_once()
        self.assertEqual(len(tick.call_args.args[0]), 3)
        self.assertEqual(report['cleanup'], 'passed')
        self.assertEqual(report['precise_scan_scope']['scans'], [])
        before = calls[:-1]
        self.assertTrue(all(method == 'GET' or path.endswith('/manual') for method, path, _ in before))
        self.assertEqual(sum(path.endswith('/items') for _, path, _ in before), 3)

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
        def scenario(*, result=None, scan_change=None, initial_actor=31, historical_actor=31,
                     historical_actor_after=31, source_visible_to_approver=False, approval_visible_to_initiator=False):
            calls = []
            class Actor:
                def __init__(self, principal): self.principal = principal
                def request(self, method, path, expected, body=None):
                    calls.append((self.principal, method, path, expected))
                    status = 200
                    if path == '/api/v1/meta/executions/' + initial_id:
                        status = 200 if self.principal == 31 or source_visible_to_approver else 404
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
                initial_scan_id=initial_id, initiator_id='31', approver_id='32')
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
        for kwargs in [{'initial_actor': 32}, {'historical_actor': 32}, {'historical_actor_after': 32},
                       {'source_visible_to_approver': True}, {'approval_visible_to_initiator': True}]:
            with self.subTest(change=kwargs), self.assertRaises(M.SuiteError): scenario(**kwargs)

    def test_schema_approval_refuses_reusing_the_initiating_user(self):
        for approver in ('31', 31):
            with self.subTest(approver=approver), self.assertRaisesRegex(M.SuiteError, 'two distinct User'):
                M.apply_cdc_schema_approval(None, 57, UUID, None, initiator=None,
                    initial_scan_id=UUID, initiator_id='31', approver_id=approver)

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
                    (control / 'ready.json').write_text(json.dumps({'run_id': 'run-1', 'phase': 'live', 'execution_id': UUID}))
                    report = {'schema_version': 'addp.continuous-execution-browser/v1', 'phase': 'live',
                              'run_id': 'run-1', 'tenant_id': '2', 'principal_id': '33',
                              'execution_ids': [UUID], 'checks': ['timeline', 'automatic_committed_progress', 'no_samples'], 'result': 'passed'}
                    (artifacts / 'continuous-browser-live.json').write_text(json.dumps(report))
                def poll(self): return 0 if self.ended else None
                def wait(self, timeout):
                    assert json.loads((self.control / 'release.json').read_text())['execution_id'] == UUID
                    self.ended = True
                    return 0
            action = []
            with patch.dict(os.environ, env), patch.object(M.subprocess, 'Popen', Process):
                result = M.run_browser('live', [UUID], subject, 'run-1', secrets, lambda: action.append(True))
            self.assertEqual(action, [True])
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
                    M.run_browser('live', [UUID], subject, 'run-1', secrets, lambda: None)
            self.assertFalse(list(secrets.iterdir()))

    def test_browser_rejects_invalid_phase_identity_and_action_before_launch(self):
        with patch.object(M.subprocess, 'Popen') as launch:
            for phase, ids, action in [('unknown', [UUID], None), ('live', [UUID], None),
                                       ('revocation', ['other'], lambda: None), ('recovery', [UUID], None)]:
                with self.subTest(phase=phase), self.assertRaises(M.SuiteError):
                    M.run_browser(phase, ids, {}, 'run-1', '/not-used', action)
            launch.assert_not_called()

    def test_drains_cursor_pages_without_offset_payloads(self):
        items = [fact(4, 'started'), fact(9), fact(12, 'cancelled')]
        client = Client([page(items[:2], True), page(items[2:])])
        self.assertEqual(M.events(client, M.MONITOR, UUID), items)
        self.assertIn('after=9&limit=2', client.calls[1][1])
        M.lifecycle_events(items, 'cancelled')

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
        M.invisible(client, UUID, 403, 404)
        self.assertEqual(len(client.calls), 6)
        self.assertEqual([call[2] for call in client.calls], [(403,)] * 3 + [(404,)] * 3)
        client = Client([{'execution_id': UUID}])
        with self.assertRaises(M.SuiteError):
            M.invisible(client, UUID, 403, 404)

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
        reader, admin = Client([context] + [{'error': 'denied'}] * 6), Client([admin_context, assignment])
        fresh = Client([fresh_context, fresh_context] + [{'error': 'denied'}] * 6)
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
            M.revoke(reader, admin, object(), 2, UUID, mutate)
        self.assertEqual(mutations, ['/api/v1/system/tenant/role_assignments/37/revoke', '/api/v1/system/login'])
        self.assertEqual([call[2] for call in reader.calls[1:]], [(401,)] * 6)
        self.assertEqual([call[2] for call in fresh.calls[2:]], [(403,)] * 3 + [(404,)] * 3)

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


if __name__ == '__main__':
    unittest.main()
