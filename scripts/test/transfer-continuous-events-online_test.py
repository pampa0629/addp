from copy import deepcopy
from importlib import import_module
import unittest
import os
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
                'execution_boundary': 'continuous', 'metadata': {'continuous': {'diagnostics': {'health': 'healthy'}}}}
        M.assert_projection(item, UUID, monitor=True, tenant=2)
        item['metadata']['arbitrary_response'] = {}
        with self.assertRaises(M.SuiteError):
            M.assert_projection(item, UUID, monitor=True, tenant=2)

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
        self.assertEqual({name for name, _ in suite.services}, {'gateway', 'system', 'meta', 'transfer', 'monitor'})
        workflow = (M.ROOT / '.github/workflows/online-t4-gates.yml').read_text()
        self.assertIn("inputs.suite != 'transfer-continuous-events'", workflow)
        job = workflow.split('  continuous-hosted-t4:', 1)[1].split('\n  redis-hosted-t4:', 1)[0]
        for fragment in ["inputs.suite == 'transfer-continuous-events'", 'runs-on: ubuntu-24.04',
                         'online-hosted-continuous-gate.sh --check-only', 'ADDP_ONLINE_SECRET_DIR:', 'if: always()']:
            self.assertIn(fragment, job)
        self.assertNotIn('secrets.', job)
        self.assertIn('test-transfer-continuous-online-runner', (M.ROOT / 'Makefile').read_text())


if __name__ == '__main__':
    unittest.main()
