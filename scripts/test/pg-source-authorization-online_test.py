import importlib
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch
from urllib.parse import parse_qs, urlsplit

ONLINE = importlib.import_module('scripts.test.pg-source-authorization-online')
REAL_BASIC_SCAN = ONLINE.basic_scan


class Server:
    def __init__(self, fault=None):
        self.grants, self.denies, self.calls = {}, {}, []
        self.expired, self.fault = False, fault

    def client(self, kind):
        server = self
        class Client:
            token = kind + '-private-token'
            def request(self, method, url, expected, body=None, **kwargs):
                path, query = urlsplit(url).path, parse_qs(urlsplit(url).query)
                server.calls.append((kind, method, path, body))
                status, result = 200, {}
                if path.endswith('/access_grants') and method == 'POST':
                    if any(row['revocation'] is None and not server.expired for row in server.grants.values()):
                        status, result = 409, {'error_code': 'engine_access_grant_relation_exists'}
                    else:
                        result = dict(body, engine_id='2', boundary_hash=body['expected_boundary_hash'], revocation=None)
                        server.grants[body['request_id']] = result
                        server.expired = False
                        status = 201
                elif '/access_grants/' in path and path.endswith('/revoke'):
                    server.grants[path.split('/')[-2]]['revocation'] = {'reason': body['reason']}
                elif path.endswith('/access_denies'):
                    server.denies[body['deny_id']] = True
                    status, result = 200, dict(body)
                elif path.endswith('/release'):
                    server.denies[path.split('/')[-2]] = False
                    result = {'deny_id': path.split('/')[-2]}
                elif path.endswith('/history') or path.endswith('/access_grants'):
                    state = query.get('state', [''])[0]
                    if state == 'expired' and server.fault != 'never_expire':
                        server.expired = True
                    rows = []
                    for row in server.grants.values():
                        revoked = row['revocation'] is not None
                        current = 'revoked' if revoked else 'expired' if server.expired else 'active'
                        if current == (state or 'active'):
                            rows.append(dict(row))
                    result = {'data': rows, 'total': len(rows)}
                elif path == '/api/v1/manager/preview':
                    target = query['locator'][0]
                    active = any(row['revocation'] is None for row in server.grants.values())
                    allowed = active and not server.expired and not any(server.denies.values()) and 'metric_people?' in target
                    if server.fault == 'deny_ignored' and any(server.denies.values()):
                        allowed = True
                    if server.fault == 'expired_ignored' and server.expired:
                        allowed = True
                    if server.fault == 'adjacent_granted' and active and 'metric_events?' in target:
                        allowed = True
                    status = 200 if allowed else 403
                    result = {'preview_type': 'table', 'data': {'columns': ['person_id'], 'rows': [
                        {'person_id': 'A'}, {'person_id': 'B'}], 'total': 2}} if allowed else {'error': 'denied'}
                    if server.fault == 'wrong_rows' and allowed:
                        result['data']['rows'][1]['person_id'] = 'wrong'
                    if server.fault == 'leaked_denial' and not allowed:
                        result['data'] = {'rows': []}
                else:
                    raise AssertionError('unexpected request ' + path)
                if status not in expected:
                    raise ONLINE.SuiteError('unexpected test HTTP status')
                return SimpleNamespace(status=status, payload=result)
        return Client()


class PGAuthorizationOnlineTest(unittest.TestCase):
    def setUp(self):
        self.facts = {'item_fingerprint': 'a' * 64, 'boundary_hash': 'b' * 64}
        self.scans = patch.object(ONLINE, 'basic_scan', side_effect=['initial-basic', 'exact-basic']).start()
        self.addCleanup(patch.stopall)
        patch.object(ONLINE, 'item', side_effect=lambda reader, engine, name: {
            'id': 10 if name == 'metric_people' else 11, 'fingerprint': 'a' * 64}).start()
        patch.object(ONLINE.REGISTRATION, 'read_exact_record_authorization_facts', return_value=self.facts).start()
        patch.object(ONLINE.time, 'sleep').start()

    def lifecycle(self, fault=None):
        server = Server(fault)
        lifecycle = ONLINE.Lifecycle(server.client('operator'), server.client('reader'), 3, 2, '33')
        return server, lifecycle

    def test_full_flow_keeps_one_table_same_credential_and_natural_expiry(self):
        server, lifecycle = self.lifecycle()
        report = {}
        lifecycle.run(report)
        lifecycle.cleanup()
        self.assertEqual(len(report['checks']), 8)
        self.assertEqual(len(server.grants), 2)
        self.assertTrue(server.expired)
        self.assertIsNone(server.grants[lifecycle.expiry_grant]['revocation'])
        self.assertFalse(any(server.denies.values()))
        self.assertEqual(self.scans.call_args_list[0].args[2], ['metric_people', 'metric_events'])
        self.assertEqual(self.scans.call_args_list[1].args[2], ['metric_people'])
        writes = [call for call in server.calls if call[1] == 'POST']
        self.assertTrue(all(call[0] == 'operator' for call in writes))
        self.assertTrue(all(call[3]['catalog_path']['segments'][-1]['name'] == 'metric_people'
                            for call in writes if 'catalog_path' in call[3]))
        self.assertNotIn('private-token', json.dumps(report))
        self.assertNotIn('person_id', json.dumps(report))

    def test_deny_success_status_matches_published_contract(self):
        swagger = json.loads((Path(__file__).resolve().parents[2] /
                              'system/backend/docs/swagger.json').read_text())
        responses = swagger['paths']['/engines/{id}/access_denies']['post']['responses']
        self.assertEqual({key for key in responses if key.startswith('2')}, {'200'})
        server, lifecycle = self.lifecycle()
        with patch.object(lifecycle.operator, 'request', wraps=lifecycle.operator.request) as request:
            command = lifecycle.deny(self.facts)
        self.assertEqual(request.call_args.args[2], (200,))
        self.assertTrue(server.denies[command])
        lifecycle.cleanup()

    def test_oracle_rejects_ignored_deny_expiry_scope_data_and_denial_leaks(self):
        for fault in ('deny_ignored', 'expired_ignored', 'adjacent_granted', 'wrong_rows', 'leaked_denial'):
            with self.subTest(fault=fault):
                self.scans.side_effect = ['initial-basic', 'exact-basic']
                server, lifecycle = self.lifecycle(fault)
                with self.assertRaises(ONLINE.SuiteError):
                    lifecycle.run({})
                lifecycle.cleanup()
                self.assertFalse(any(server.denies.values()))

    def test_changed_boundary_after_basic_scan_fails_and_cleans_the_grant(self):
        ONLINE.REGISTRATION.read_exact_record_authorization_facts.side_effect = [
            self.facts, dict(self.facts, boundary_hash='c' * 64)]
        server, lifecycle = self.lifecycle()
        with self.assertRaisesRegex(ONLINE.SuiteError, 'boundary'):
            lifecycle.run({})
        lifecycle.cleanup()
        self.assertTrue(all(row['revocation'] is not None for row in server.grants.values()))

    def test_natural_expiry_deadline_is_bounded_and_failure_still_withdraws(self):
        server, lifecycle = self.lifecycle('never_expire')
        with patch.object(ONLINE.time, 'monotonic', side_effect=[0, 1, 100]):
            with self.assertRaisesRegex(ONLINE.SuiteError, 'naturally expire'):
                lifecycle.run({})
        lifecycle.cleanup()
        self.assertTrue(all(row['revocation'] is not None for row in server.grants.values()))

    def test_basic_scan_uses_only_leaf_locators_and_checks_counter_scope(self):
        # Call the real function saved before its suite-level patch.
        execution = 'd4263076-2286-4a52-9ea0-c472c92eb2dd'
        client = Mock()
        for metadata, succeeds in (({'items_scanned': 1, 'catalog_nodes_scanned': 0}, True),
                                   ({'items_scanned': 2, 'catalog_nodes_scanned': 1}, False)):
            client.request.side_effect = [SimpleNamespace(payload={'execution_id': execution}),
                SimpleNamespace(payload={'execution_id': execution, 'status': 'success',
                                         'scan_context': {'scan_depth': 'basic'}, 'metadata': metadata})]
            if succeeds:
                self.assertEqual(REAL_BASIC_SCAN(client, 2, ['metric_people']), execution)
            else:
                with self.assertRaisesRegex(ONLINE.SuiteError, 'selected leaves'):
                    REAL_BASIC_SCAN(client, 2, ['metric_people'])
            body = client.request.call_args_list[-2].args[3]
            self.assertEqual(body['scan_depth'], 'basic')
            self.assertTrue(body['force'])
            self.assertNotIn('catalog_paths', body)
            self.assertEqual(len(body['targets']), 1)

    def test_unknown_write_outcome_is_tracked_before_submission(self):
        server, lifecycle = self.lifecycle()
        original = lifecycle.operator.request
        def timeout_after_commit(method, path, expected, body=None):
            result = original(method, path, expected, body)
            if method == 'POST' and path.endswith('/access_grants'):
                raise ONLINE.SuiteError('response lost')
            return result
        lifecycle.operator.request = timeout_after_commit
        with self.assertRaises(ONLINE.SuiteError):
            lifecycle.grant(self.facts)
        self.assertEqual(len(lifecycle.grants), 1)
        lifecycle.operator.request = original
        lifecycle.cleanup()
        self.assertTrue(all(row['revocation'] is not None for row in server.grants.values()))

    def test_cleanup_never_revokes_an_unowned_grant_and_reports_failure(self):
        server, lifecycle = self.lifecycle()
        server.grants['unowned'] = {'request_id': 'unowned', 'revocation': None}
        with self.assertRaises(ONLINE.SuiteError):
            lifecycle.cleanup()
        self.assertIsNone(server.grants['unowned']['revocation'])

    def test_cleanup_attempts_grants_even_if_deny_release_fails(self):
        server, lifecycle = self.lifecycle()
        lifecycle.grant(self.facts)
        lifecycle.denies = ['deny']
        with patch.object(lifecycle, 'release', side_effect=ONLINE.SuiteError('failed')):
            with self.assertRaises(ONLINE.SuiteError):
                lifecycle.cleanup()
        self.assertTrue(all(row['revocation'] is not None for row in server.grants.values()))

    def test_main_rejects_personal_environment_before_any_client(self):
        with patch.dict(ONLINE.os.environ, {}, clear=True), patch.object(ONLINE.API, 'GatewayClient') as client:
            self.assertEqual(ONLINE.main(), 1)
            client.assert_not_called()

    def test_main_failure_after_writes_still_cleans_and_reports_no_secrets(self):
        for after_deny in (False, True):
            with self.subTest(after_deny=after_deny), tempfile.TemporaryDirectory() as directory:
                server, _ = self.lifecycle()
                env = {'ADDP_ONLINE_TEST': '1', 'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1',
                       'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'POSTGRES_DB': 'addp_online',
                       'ADDP_ONLINE_TEST_TENANT_ID': '3', 'ADDP_ONLINE_CONSUMER_ENGINE_ID': '2',
                       'ADDP_ONLINE_ARTIFACT_DIR': directory, 'ADDP_ONLINE_TEST_RUN_ID': 'pg-fixture',
                       'GATEWAY_URL': 'http://127.0.0.1:8000', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'reader',
                       'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'operator'}
                def failed_run(lifecycle, report):
                    lifecycle.grant(self.facts)
                    if after_deny:
                        lifecycle.deny(self.facts)
                    raise ONLINE.SuiteError('injected interruption')
                with patch.dict(ONLINE.os.environ, env, clear=True), \
                     patch.object(ONLINE.API, 'GatewayClient', side_effect=lambda base, token, timeout: server.client(token)), \
                     patch.object(ONLINE, 'identity', side_effect=[('33', {}), ('31', {})]), \
                     patch.object(ONLINE.Lifecycle, 'run', autospec=True, side_effect=failed_run):
                    self.assertEqual(ONLINE.main(), 1)
                report = json.loads((Path(directory) / (ONLINE.SUITE + '.json')).read_text())
                self.assertEqual(report['result'], 'failed')
                self.assertEqual(report['cleanup'], 'zero_current_source_rules')
                self.assertFalse(any(server.denies.values()))
                self.assertTrue(all(row['revocation'] is not None for row in server.grants.values()))
                self.assertNotIn('private-token', json.dumps(report))


if __name__ == '__main__':
    unittest.main()
