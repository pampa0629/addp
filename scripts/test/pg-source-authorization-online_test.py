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
REAL_BROWSER_OBSERVATION = ONLINE.BrowserObservation


class Server:
    def __init__(self, fault=None):
        self.grants, self.denies, self.calls = {}, {}, []
        self.deny_bodies, self.expired_grants = {}, set()
        self.expired, self.fault = False, fault
        self.memberships = {'department': '41', 'project_group': '51'}
        self.version, self.current_reader_token = 1, 'reader-private-token'

    def context(self, kind):
        reader = kind.startswith('reader')
        permissions = ONLINE.READER_PERMISSIONS if reader or kind == 'outsider' else (
            ONLINE.ORGANIZATION_PERMISSIONS if kind == 'coordinator' else ONLINE.OPERATOR_PERMISSIONS)
        return {'principal': {'id': '33' if reader else '34' if kind == 'outsider' else '35', 'type': 'user'},
                'context': {'type': 'tenant', 'tenant_id': '3', 'tenant_membership_id': '71'},
                'token': {'type': 'first_party_access_token'},
                'authorization': {'authorization_version': str(self.version), 'role_assignments': [
                    {'role_key': 'online.reader', 'permissions': sorted(permissions)}]},
                'organization': {'departments': [{'department_id': '41'}] if reader and 'department' in self.memberships else [],
                                 'project_groups': [{'project_group_id': '51'}] if reader and 'project_group' in self.memberships else []}}

    def inherited(self, row):
        kind = row['recipient_type']
        return kind == 'user' or kind in self.memberships or self.fault == 'inherit_after_exit'

    def client(self, kind):
        server = self
        class Client:
            token = kind + '-private-token'
            base_url, timeout = 'http://127.0.0.1:8000', 10
            def request(self, method, url, expected, body=None, **kwargs):
                path, query = urlsplit(url).path, parse_qs(urlsplit(url).query)
                server.calls.append((kind, method, path, body))
                status, result = 200, {}
                if kind.startswith('reader') and self.token != server.current_reader_token and server.fault != 'stale_token_accepted':
                    status, result = 401, {'error': 'authentication required'}
                elif path == '/api/v1/system/auth/context':
                    result = server.context(kind)
                    if server.fault == 'changed_login_identity' and '-' in kind:
                        result['principal']['id'] = '999'
                    if server.fault == 'changed_login_permissions' and '-' in kind:
                        result['authorization']['role_assignments'][0]['permissions'].append('system.engine_access_grant.create')
                    if server.fault == 'same_authorization_version' and '-' in kind:
                        result['authorization']['authorization_version'] = '1'
                elif path == '/api/v1/system/login':
                    server.current_reader_token = f'reader-{server.version}-private-token'
                    result = {'next_action': 'session_issued', 'session': {'access_token': server.current_reader_token}}
                elif '/memberships' in path:
                    membership_kind = 'department' if '/departments/' in path else 'project_group'
                    row = {'id': '81' if membership_kind == 'department' else '91', 'version': 1,
                           'principal_id': '33', 'tenant_membership_id': '71', 'status': 'active'}
                    if method == 'GET':
                        rows = [row] if membership_kind in server.memberships else []
                        result = {'data': rows, 'total': len(rows)}
                    else:
                        assert kind == 'coordinator' and body['version'] == 1
                        server.memberships.pop(membership_kind)
                        server.version += 1
                        server.current_reader_token = None
                        if server.fault == 'exit_revokes_organization':
                            for grant in server.grants.values():
                                if grant['recipient_type'] == membership_kind:
                                    grant['revocation'] = {'reason': 'wrong member exit'}
                        result = dict(row, status='ended', version=2, ended_at='2026-10-16T04:00:00Z')
                        if server.fault == 'wrong_membership_closed':
                            result['principal_id'] = '999'
                elif path.endswith('/access_grants') and method == 'POST':
                    if any(row['revocation'] is None and key not in server.expired_grants
                           and (row['recipient_type'], row['recipient_id']) == (body['recipient_type'], body['recipient_id'])
                           for key, row in server.grants.items()):
                        status, result = 409, {'error_code': 'engine_access_grant_relation_exists'}
                    else:
                        result = dict(body, engine_id='2', boundary_hash=body['expected_boundary_hash'], revocation=None)
                        server.grants[body['request_id']] = result
                        server.expired = False
                        status = 201
                elif '/access_grants/' in path and path.endswith('/revoke'):
                    server.grants[path.split('/')[-2]]['revocation'] = {'reason': body['reason']}
                    if server.fault == 'withdrawal_overreach' and any(row['recipient_type'] != 'user' for row in server.grants.values()):
                        for row in server.grants.values():
                            row['revocation'] = {'reason': body['reason']}
                elif path.endswith('/access_denies'):
                    server.denies[body['deny_id']] = True
                    server.deny_bodies[body['deny_id']] = dict(body)
                    status, result = 200, dict(body)
                elif path.endswith('/release'):
                    server.denies[path.split('/')[-2]] = False
                    if server.fault == 'release_all_denies':
                        server.denies = dict.fromkeys(server.denies, False)
                    result = {'deny_id': path.split('/')[-2]}
                elif path.endswith('/history') or path.endswith('/access_grants'):
                    state = query.get('state', [''])[0]
                    if state == 'expired' and server.fault != 'never_expire':
                        server.expired = True
                        server.expired_grants.update(key for key, row in server.grants.items() if row['revocation'] is None)
                    history = path.endswith('/history')
                    recipient = (query['recipient_type'][0], query['recipient_id'][0])
                    account = recipient == ('user', '33') and not history
                    rows = []
                    for key, row in server.grants.items():
                        revoked = row['revocation'] is not None
                        current = 'revoked' if revoked else 'expired' if key in server.expired_grants else 'active'
                        matches = (row.get('recipient_type', 'user'), row.get('recipient_id', '33')) == recipient
                        if server.fault == 'history_expands_organizations' and history and recipient == ('user', '33'):
                            matches = True
                        if current == (state or 'active') and (matches or (account and server.inherited(row))):
                            rows.append(dict(row))
                    if account:
                        sources = [{key: row[key] for key in ('recipient_type', 'recipient_id', 'request_id', 'expiry_mode', 'expires_at')}
                                   | {'grant_count': 1} for row in rows]
                        denies = [{key: body[key] for key in ('recipient_type', 'recipient_id', 'deny_id', 'expiry_mode', 'expires_at')}
                                  for command, body in server.deny_bodies.items() if server.denies[command]]
                        if server.fault == 'wrong_source_anchor' and any(row['recipient_type'] == 'project_group' for row in sources):
                            sources[-1]['request_id'] = 'wrong'
                        rows = []
                        if (sources or denies) and not (server.fault == 'hide_deny_only' and not sources):
                            reason = 'explicit_deny' if denies else 'grant'
                            target = json.loads(query['catalog_path'][0])
                            rows = [{'catalog_path': target, 'item_fingerprint': 'a' * 64, 'recipient_type': 'user',
                                     'recipient_id': '33', 'grant_count': len(sources), 'inspection': {
                                         'account_id': '33', 'catalog_path': target, 'item_fingerprint': 'a' * 64,
                                         'observed_at': '2026-10-16T03:00:00Z', 'rule_covered': reason == 'grant',
                                         'reason': reason, 'sources': sources, 'denies': denies}}]
                    result = {'data': rows, 'total': len(rows)}
                elif path == '/api/v1/manager/preview':
                    target = query['locator'][0]
                    active = any(row['revocation'] is None and key not in server.expired_grants and server.inherited(row)
                                 for key, row in server.grants.items())
                    allowed = kind.startswith('reader') and active and not any(server.denies.values()) and 'metric_people?' in target
                    if server.fault == 'nonmember_granted' and kind == 'outsider' and active:
                        allowed = True
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
        self.browser = patch.object(ONLINE, 'BrowserObservation').start().return_value.__enter__.return_value
        self.browser.finish.return_value = {'result': 'passed'}
        patch.object(ONLINE.API, 'GatewayClient', side_effect=lambda base, token, timeout:
                     self.server.client(token.removesuffix('-private-token'))).start()

    def lifecycle(self, fault=None):
        server = Server(fault)
        self.server = server
        lifecycle = ONLINE.Lifecycle(server.client('operator'), server.client('reader'), 3, 2, '33',
                                     {'department': '41', 'project_group': '51'}, server.client('outsider'),
                                     server.client('coordinator'), server.client('login'), 'external-online-consumer', 'private-password')
        return server, lifecycle

    def test_full_flow_keeps_one_table_same_credential_and_natural_expiry(self):
        server, lifecycle = self.lifecycle()
        report = {}
        lifecycle.run(report)
        lifecycle.cleanup()
        self.assertEqual(len(report['checks']), 22)
        self.assertEqual(len(server.grants), 7)
        self.assertTrue(report['same_credential_through_grant_expiry'])
        self.assertIn(lifecycle.expiry_grant, server.expired_grants)
        self.assertIsNone(server.grants[lifecycle.expiry_grant]['revocation'])
        self.assertFalse(any(server.denies.values()))
        self.assertEqual(self.scans.call_args_list[0].args[2], ['metric_people', 'metric_events'])
        self.assertEqual(self.scans.call_args_list[1].args[2], ['metric_people'])
        writes = [call for call in server.calls if call[1] == 'POST']
        self.assertTrue(all(call[0] == ('login' if call[2].endswith('/login') else
                                      'coordinator' if '/memberships/' in call[2] else 'operator') for call in writes))
        self.assertTrue(all(call[3]['catalog_path']['segments'][-1]['name'] == 'metric_people'
                            for call in writes if 'catalog_path' in call[3]))
        self.assertNotIn('private-token', json.dumps(report))
        self.assertNotIn('person_id', json.dumps(report))
        self.assertEqual([len(row['sources']) for row in report['source_observations']], [1, 2, 3, 2, 2, 2, 2, 2, 1, 0, 0, 2, 1, 0])
        self.assertEqual(report['membership_observations'], [
            {'closed_type': 'department', 'old_token_status': 401, 'remaining_sources': 1, 'preview_status': 200},
            {'closed_type': 'project_group', 'old_token_status': 401, 'remaining_sources': 0, 'preview_status': 403}])
        self.assertEqual(server.memberships, {})
        self.assertEqual([call.args[0] for call in self.browser.wait.call_args_list], ['department', 'project_group'])
        self.assertEqual([call.args[0] for call in self.browser.release.call_args_list], ['department', 'project_group'])

    def test_browser_failure_still_withdraws_both_organization_grants(self):
        server, lifecycle = self.lifecycle()
        self.browser.wait.side_effect = ONLINE.SuiteError('browser did not reach its authorized observation')
        with self.assertRaisesRegex(ONLINE.SuiteError, 'browser'):
            lifecycle.run({})
        lifecycle.cleanup()
        self.assertTrue(all(row['revocation'] is not None or key in server.expired_grants
                            for key, row in server.grants.items()))
        self.assertEqual(server.memberships, {'department': '41', 'project_group': '51'})

    def test_member_exit_oracles_reject_stale_tokens_inheritance_and_grant_deletion(self):
        for fault in ('stale_token_accepted', 'inherit_after_exit', 'exit_revokes_organization',
                      'changed_login_identity', 'changed_login_permissions', 'same_authorization_version',
                      'wrong_membership_closed'):
            with self.subTest(fault=fault):
                self.scans.side_effect = ['initial-basic', 'exact-basic']
                server, lifecycle = self.lifecycle(fault)
                with self.assertRaises(ONLINE.SuiteError):
                    lifecycle.run({})
                lifecycle.cleanup()
                self.assertTrue(all(row['revocation'] is not None or key in server.expired_grants
                                    for key, row in server.grants.items()))

    def test_member_exit_interruption_still_cleans_organization_grants(self):
        server, lifecycle = self.lifecycle()
        original = lifecycle.organization_operator.request
        def interrupted(method, path, expected, body=None):
            response = original(method, path, expected, body)
            if method == 'POST':
                raise ONLINE.SuiteError('membership response lost')
            return response
        lifecycle.organization_operator.request = interrupted
        with self.assertRaisesRegex(ONLINE.SuiteError, 'response lost'):
            lifecycle.run({})
        lifecycle.cleanup()
        self.assertNotIn('department', server.memberships)
        self.assertIn('project_group', server.memberships)
        self.assertTrue(all(row['revocation'] is not None or key in server.expired_grants
                            for key, row in server.grants.items()))

    def test_organization_projection_history_and_nonmember_oracles(self):
        for fault in ('wrong_source_anchor', 'nonmember_granted', 'release_all_denies', 'hide_deny_only',
                      'history_expands_organizations', 'withdrawal_overreach'):
            with self.subTest(fault=fault):
                self.scans.side_effect = ['initial-basic', 'exact-basic']
                server, lifecycle = self.lifecycle(fault)
                with self.assertRaises(ONLINE.SuiteError):
                    lifecycle.run({})
                lifecycle.cleanup()
                self.assertFalse(any(server.denies.values()))
                self.assertTrue(all(row['revocation'] is not None or key in server.expired_grants
                                    for key, row in server.grants.items()))

    def test_cleanup_tracks_organization_unknown_write_outcomes(self):
        for kind, recipient in (('department', '41'), ('project_group', '51')):
            with self.subTest(kind=kind):
                server, lifecycle = self.lifecycle()
                original = lifecycle.operator.request
                def interrupted(method, path, expected, body=None):
                    response = original(method, path, expected, body)
                    if method == 'POST' and path.endswith('/access_grants'):
                        raise ONLINE.SuiteError('response lost')
                    return response
                lifecycle.operator.request = interrupted
                with self.assertRaises(ONLINE.SuiteError):
                    lifecycle.grant(self.facts, recipient_type=kind, recipient_id=recipient)
                lifecycle.operator.request = original
                lifecycle.cleanup()
                self.assertTrue(all(row['revocation'] is not None for row in server.grants.values()))

    def test_organization_memberships_must_match_current_context(self):
        client = Mock()
        client.request.return_value.payload = {'organization': {'departments': [{'department_id': '41'}],
                                                               'project_groups': [{'project_group_id': '51'}]}}
        ONLINE.organization_memberships(client, {'department': '41', 'project_group': '51'})
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.organization_memberships(client, {})
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.organization_memberships(client, {'department': '42', 'project_group': '51'})

    def test_membership_actor_requires_existing_same_tenant_administrator(self):
        client = Mock()
        context = Server().context('coordinator')
        context['authorization']['role_assignments'][0]['role_key'] = 'tenant.administrator'
        client.request.return_value.payload = context
        actor, _ = ONLINE.organization_administrator(client, 3)
        self.assertEqual(actor, '35')
        for change in ('tenant', 'principal', 'permissions', 'role', 'token'):
            with self.subTest(change=change):
                invalid = json.loads(json.dumps(context))
                if change == 'tenant':
                    invalid['context']['tenant_id'] = '4'
                elif change == 'principal':
                    invalid['principal']['type'] = 'service_principal'
                elif change == 'permissions':
                    invalid['authorization']['role_assignments'][0]['permissions'].pop()
                elif change == 'role':
                    invalid['authorization']['role_assignments'][0]['role_key'] = 'online.reader'
                else:
                    invalid['token']['type'] = 'oauth_access_token'
                client.request.return_value.payload = invalid
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.organization_administrator(client, 3)

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

    def test_cleanup_continues_other_recipients_without_touching_unowned_grants(self):
        server, lifecycle = self.lifecycle()
        lifecycle.grant(self.facts, recipient_type='department', recipient_id='41')
        lifecycle.grant(self.facts, recipient_type='project_group', recipient_id='51')
        server.grants['unowned'] = {'request_id': 'unowned', 'revocation': None, 'recipient_type': 'department', 'recipient_id': '41'}
        with self.assertRaises(ONLINE.SuiteError):
            lifecycle.cleanup()
        self.assertIsNone(server.grants['unowned']['revocation'])
        self.assertTrue(all(row['revocation'] is not None for key, row in server.grants.items() if key != 'unowned'))

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
                       'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'operator',
                       'ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN': 'outsider',
                       'ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN': 'coordinator',
                       'ADDP_ONLINE_TEST_USER_USERNAME': 'external-online-consumer',
                       'ADDP_ONLINE_TEST_USER_PASSWORD': 'private-password',
                       'ADDP_ONLINE_FIXTURE_DEPARTMENT_ID': '41', 'ADDP_ONLINE_FIXTURE_PROJECT_GROUP_ID': '51'}
                def failed_run(lifecycle, report):
                    lifecycle.grant(self.facts)
                    if after_deny:
                        lifecycle.deny(self.facts)
                    raise ONLINE.SuiteError('injected interruption')
                with patch.dict(ONLINE.os.environ, env, clear=True), \
                     patch.object(ONLINE.API, 'GatewayClient', side_effect=lambda base, token, timeout: server.client(token)), \
                     patch.object(ONLINE, 'identity', side_effect=[('33', {}), ('31', {}), ('34', {})]), \
                     patch.object(ONLINE, 'organization_administrator', return_value=('35', {})), \
                     patch.object(ONLINE, 'organization_memberships'), \
                     patch.object(ONLINE.Lifecycle, 'run', autospec=True, side_effect=failed_run):
                    self.assertEqual(ONLINE.main(), 1)
                report = json.loads((Path(directory) / (ONLINE.SUITE + '.json')).read_text())
                self.assertEqual(report['result'], 'failed')
                self.assertEqual(report['cleanup'], 'zero_current_source_rules')
                self.assertFalse(any(server.denies.values()))
                self.assertTrue(all(row['revocation'] is not None for row in server.grants.values()))
                self.assertNotIn('private-token', json.dumps(report))


class PGBrowserObservationTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.environment = {'ADDP_ONLINE_ARTIFACT_DIR': self.directory.name,
            'ADDP_ONLINE_SECRET_DIR': self.directory.name, 'ADDP_ONLINE_TEST_RUN_ID': 'pg-browser-test',
            'ADDP_ONLINE_HOSTED': '1', 'ADDP_ONLINE_TEST': '1', 'CONSOLE_URL': 'http://127.0.0.1:5170',
            'ADDP_ONLINE_TEST_USER_USERNAME': 'external-online-consumer',
            'ADDP_ONLINE_TEST_USER_PASSWORD': 'reader-private-password',
            'ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN': 'must-not-reach-browser',
            'SYSTEM_SERVICE_CLIENT_SECRET': 'must-not-reach-browser'}
        self.process = Mock(pid=12345)
        self.process.poll.return_value = None
        self.process.wait.return_value = 0

    def expected(self, browser):
        return dict(browser.binding, schema_version='addp.pg-source-authorization-browser/v1', result='passed',
                    checks=ONLINE.BROWSER_CHECKS, login_requests=1, anonymous_refreshes=1,
                    observations=[{'closed_type': 'department', 'refreshes': 1, 'preview_status': 200},
                                  {'closed_type': 'project_group', 'refreshes': 1, 'preview_status': 403}])

    def test_control_binding_private_process_scope_and_exact_report(self):
        with patch.dict(ONLINE.os.environ, self.environment), patch.object(ONLINE.subprocess, 'Popen', return_value=self.process) as popen, \
             patch.object(ONLINE.os, 'killpg') as kill:
            with REAL_BROWSER_OBSERVATION(2, '33', 'addp://engine/1/path/public/metric_people?type=table&item_id=10') as browser:
                control = browser.control
                for kind in ('department', 'project_group'):
                    (control / f'ready-{kind}.json').write_text(json.dumps(browser.marker(kind)))
                    browser.wait(kind)
                    browser.release(kind)
                    self.assertEqual(json.loads((control / f'release-{kind}.json').read_text()), browser.marker(kind))
                browser.evidence.write_text(json.dumps(self.expected(browser)))
                self.assertEqual(browser.finish(), self.expected(browser))
                env = popen.call_args.kwargs['env']
                self.assertNotIn('ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN', env)
                self.assertNotIn('SYSTEM_SERVICE_CLIENT_SECRET', env)
                self.assertEqual(env['ADDP_ONLINE_TEST_USER_PASSWORD'], 'reader-private-password')
                self.assertTrue(popen.call_args.kwargs['start_new_session'])
                self.assertTrue(popen.call_args.args[0][-2].startswith('--output=' + str(control)))
                self.process.poll.return_value = 0
            kill.assert_not_called()
            self.assertFalse(control.exists())

    def test_report_missing_relogin_or_duplicate_refresh_cannot_pass(self):
        with patch.dict(ONLINE.os.environ, self.environment), patch.object(ONLINE.subprocess, 'Popen', return_value=self.process), \
             patch.object(ONLINE.os, 'killpg'):
            with REAL_BROWSER_OBSERVATION(2, '33', 'target') as browser:
                for field in ('missing', 'relogin', 'duplicate_refresh', 'wrong_principal', 'incomplete_checks'):
                    with self.subTest(field=field):
                        value = self.expected(browser)
                        if field == 'missing':
                            browser.evidence.unlink(missing_ok=True)
                        else:
                            if field == 'relogin': value['login_requests'] = 2
                            if field == 'duplicate_refresh': value['observations'][0]['refreshes'] = 2
                            if field == 'wrong_principal': value['principal_id'] = 'other'
                            if field == 'incomplete_checks': value['checks'] = value['checks'][:-1]
                            browser.evidence.write_text(json.dumps(value))
                        with self.assertRaises(ONLINE.SuiteError):
                            browser.finish()
                (browser.control / 'ready-department.json').write_text(json.dumps({'run_id': 'wrong'}))
                with self.assertRaisesRegex(ONLINE.SuiteError, 'binding'):
                    browser.wait('department')

    def test_early_exit_and_deadline_stop_private_browser_process(self):
        with patch.dict(ONLINE.os.environ, self.environment), patch.object(ONLINE.subprocess, 'Popen', return_value=self.process), \
             patch.object(ONLINE.os, 'killpg') as kill:
            with REAL_BROWSER_OBSERVATION(2, '33', 'target') as browser:
                control = browser.control
                self.process.poll.return_value = 1
                with self.assertRaisesRegex(ONLINE.SuiteError, 'observation'):
                    browser.wait('department')
                self.process.poll.return_value = None
                self.process.wait.side_effect = [ONLINE.subprocess.TimeoutExpired('playwright', 180), 0]
            self.assertEqual([call.args[1] for call in kill.call_args_list], [ONLINE.signal.SIGTERM, ONLINE.signal.SIGKILL])
            self.assertFalse(control.exists())


if __name__ == '__main__':
    unittest.main()
