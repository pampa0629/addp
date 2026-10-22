import importlib
import itertools
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
REAL_WORKSPACE_OBSERVATION = ONLINE.WorkspaceObservation
REAL_DEPENDENCY_OUTAGE = ONLINE.DependencyOutage
REAL_MUTATE_SOURCE_CONTENT = ONLINE.mutate_source_content
READ_CHECK = '/api/v1/system/engine-access/read-checks/manager-preview'


class Server:
    def __init__(self, fault=None):
        self.grants, self.denies, self.calls = {}, {}, []
        self.deny_bodies, self.expired_grants = {}, set()
        self.expired, self.fault = False, fault
        self.memberships = {'department': '41', 'project_group': '51'}
        self.version, self.current_reader_token = 1, 'reader-private-token'
        self.meta_down = False
        self.catalog_down = False
        self.people = ('A', 'B')

    def context(self, kind):
        reader = kind.startswith('reader')
        permissions = ONLINE.READER_PERMISSIONS if reader or kind == 'outsider' else (
            ONLINE.CONFIRMER_PERMISSIONS if kind == 'business' else
            ONLINE.ORGANIZATION_PERMISSIONS if kind == 'coordinator' else ONLINE.OPERATOR_PERMISSIONS)
        if kind == 'business' and self.fault == 'function_permission_added':
            permissions = permissions | {'manager.data_item.read'}
        return {'principal': {'id': '33' if reader else '34' if kind == 'outsider' else '36' if kind == 'business' else '35', 'type': 'user'},
                'context': {'type': 'tenant', 'tenant_id': '3', 'tenant_membership_id': '71'},
                'token': {'type': 'first_party_access_token'},
                'authorization': {'authorization_version': str(self.version), 'role_assignments': [
                    {'role_key': 'online.reader', 'scope': {'type': 'tenant', 'tenant_id': '3'},
                     'permissions': sorted(permissions)}]},
                'organization': {'departments': [{'department_id': '41'}] if reader and 'department' in self.memberships else [],
                                 'project_groups': [{'project_group_id': '51'}] if reader and 'project_group' in self.memberships else []}}

    def inherited(self, row, principal='33'):
        kind = row['recipient_type']
        return (kind == 'user' and row['recipient_id'] == principal) or (kind != 'user' and principal == '33'
                and (kind in self.memberships or self.fault == 'inherit_after_exit'))

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
                elif path.endswith('/authorization-facts'):
                    status, result = 503, {'error': 'dependency unavailable', 'error_code': 'authorization_facts_unavailable'}
                elif path == READ_CHECK:
                    assert method == 'POST' and kind == 'reader' and body == {'targets': [ONLINE.table_path(2, 'metric_people')]}
                    result = {'observed_at': '2026-10-22T00:00:00Z'}
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
                elif server.catalog_down and server.fault == 'catalog_blocks_rule_writes' and method == 'POST':
                    status, result = 503, {'error': 'wrong rule dependency'}
                elif path.endswith('/access_grants') and method == 'POST':
                    if body['request_id'] in server.grants:
                        result, status = dict(server.grants[body['request_id']]), 201
                        if server.fault == 'direct_replay_renews':
                            server.grants[body['request_id']]['granted_at'] = '2026-10-16T02:00:00Z'
                        if server.fault == 'direct_replay_restores_revoked':
                            server.grants[body['request_id']]['revocation'] = None
                    elif any(row['revocation'] is None and key not in server.expired_grants
                           and (row['recipient_type'], row['recipient_id']) == (body['recipient_type'], body['recipient_id'])
                           for key, row in server.grants.items()):
                        status, result = 409, {'error_code': 'engine_access_grant_relation_exists'}
                    else:
                        result = dict(body, engine_id='2', boundary_hash=body['expected_boundary_hash'], revocation=None,
                                      approval_mode='independent', granted_at='2026-10-16T01:00:00Z')
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
                        state_matches = (not state or current == state) if history else current == 'active'
                        if row['catalog_path'] == json.loads(query['catalog_path'][0]) and state_matches and (matches or (account and server.inherited(row))):
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
                    if kind == 'operator' and recipient == ('user', '36') and server.fault == 'function_grant_missing':
                        result = {'data': [], 'total': 0}
                        server.fault = None
                    if recipient == ('user', '36') and server.fault == 'function_wrong_binding' and rows:
                        rows[0]['item_fingerprint'] = 'wrong-dataitem'
                elif path == '/api/v1/manager/engines':
                    result = {'data': [{'id': 2}]}
                elif path == '/api/v1/manager/preview':
                    target = query['locator'][0]
                    matching = [row for key, row in server.grants.items()
                                if row['revocation'] is None and key not in server.expired_grants
                                and row['catalog_path']['segments'][-1]['name'] + '?' in target]
                    active = any(server.inherited(row, '33' if kind.startswith('reader') else '36' if kind == 'business' else '34') for row in matching)
                    allowed = (kind.startswith('reader') or kind == 'outsider') and active and not any(server.denies.values())
                    if server.fault == 'nonmember_granted' and kind == 'outsider' and matching:
                        allowed = True
                    if server.fault == 'deny_ignored' and any(server.denies.values()):
                        allowed = True
                    if server.fault == 'expired_ignored' and server.expired:
                        allowed = True
                    if server.fault == 'adjacent_granted' and 'metric_events?' in target and any(
                            row['revocation'] is None for row in server.grants.values()):
                        allowed = True
                    status = 200 if allowed else 403
                    result = {'preview_type': 'table', 'data': {'columns': ['person_id'], 'rows': [
                        {'person_id': value} for value in server.people], 'total': len(server.people)}} if allowed else {'error': 'denied'}
                    if allowed and server.fault == 'stale_content' and len(server.people) == 3:
                        result['data'] = {'columns': ['person_id'], 'rows': [{'person_id': 'A'}, {'person_id': 'B'}], 'total': 2}
                    if allowed and 'metric_events?' in target:
                        result['data'] = {'columns': ['event_id', 'event_date'],
                            'rows': [{'event_id': value, 'event_date': '2026-01-01'} for value in ('e1', 'e2', 'e3', 'old')], 'total': 4}
                    if server.fault == 'wrong_rows' and allowed:
                        result['data']['rows'][1]['person_id'] = 'wrong'
                    if server.fault == 'leaked_denial' and not allowed:
                        result['data'] = {'rows': []}
                    if server.meta_down:
                        status, result = 503, {'error': 'dependency unavailable', 'error_code': 'authorization_service_unavailable'}
                        if server.fault == 'outage_fallback':
                            status, result = 200, {'data': {'rows': []}}
                        elif server.fault == 'outage_masquerades_as_missing':
                            status = 404
                        elif server.fault == 'outage_leaks':
                            result['data'] = {'rows': []}
                    if server.catalog_down and server.fault == 'catalog_blocks_existing_read':
                        status, result = 503, {'error': 'wrong read dependency'}
                    if kind == 'business':
                        status, result = 403, {'error': 'forbidden', 'error_code': 'permission_denied'}
                        if active and server.fault == 'function_grant_bypasses_guard':
                            status, result = 200, {'data': {'rows': []}}
                        if server.fault == 'function_wrong_denial':
                            result['error_code'] = 'source_access_denied'
                        if server.fault == 'function_denial_leaks':
                            result['data'] = {'rows': []}
                        if active and server.fault == 'function_denial_revokes_grant':
                            for row in matching:
                                row['revocation'] = {'reason': 'wrong functional denial side effect'}
                else:
                    raise AssertionError('unexpected request ' + path)
                if status not in expected:
                    raise ONLINE.SuiteError('unexpected test HTTP status')
                return SimpleNamespace(status=status, payload=result)
        return Client()


class PGAuthorizationOnlineTest(unittest.TestCase):
    def setUp(self):
        self.facts = {'item_fingerprint': 'a' * 64, 'boundary_hash': 'b' * 64}
        self.scans = patch.object(ONLINE, 'basic_scan', side_effect=[
            'initial-basic', 'exact-basic', 'content-grant-basic', 'content-deny-basic',
            'content-revoked-basic', 'content-restored-basic']).start()
        self.addCleanup(patch.stopall)
        patch.object(ONLINE, 'item', side_effect=lambda reader, engine, name: {
            'id': 10 if name == 'metric_people' else 11, 'fingerprint': 'a' * 64}).start()
        patch.object(ONLINE.REGISTRATION, 'read_exact_record_authorization_facts', return_value=self.facts).start()
        patch.object(ONLINE.time, 'sleep').start()
        def mutate(action):
            self.server.people = ('A', 'B2', 'C') if action == 'content-change' else ('A', 'B')
            if self.server.fault == 'content_loses_deny' and action == 'content-reset':
                self.server.denies = dict.fromkeys(self.server.denies, False)
            if self.server.fault == 'content_restores_withdrawn' and action == 'content-change':
                for row in self.server.grants.values():
                    row['revocation'] = None
        self.content = patch.object(ONLINE, 'mutate_source_content', side_effect=mutate).start()
        self.browser = patch.object(ONLINE, 'BrowserObservation').start().return_value.__enter__.return_value
        self.browser.finish.return_value = {'result': 'passed'}
        self.outage = patch.object(ONLINE, 'DependencyOutage').start().return_value
        self.outage.before, self.outage.after = {}, {}
        def down():
            self.server.meta_down = True
            return self.outage
        def restored(*_):
            self.server.meta_down = False
            if self.server.fault == 'recovery_loses_deny':
                self.server.denies = dict.fromkeys(self.server.denies, False)
        self.outage.__enter__.side_effect = down
        self.outage.__exit__.side_effect = restored
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
        self.assertEqual(len(report['checks']), 33)
        self.assertEqual(len(server.grants), 9)
        self.assertTrue(report['same_credential_through_grant_expiry'])
        self.assertIn(lifecycle.expiry_grant, server.expired_grants)
        self.assertIsNone(server.grants[lifecycle.expiry_grant]['revocation'])
        self.assertFalse(any(server.denies.values()))
        self.assertEqual(self.scans.call_args_list[0].args[2], ['metric_people', 'metric_events'])
        self.assertEqual(self.scans.call_args_list[1].args[2], ['metric_people'])
        decisions = [call for call in server.calls if call[2] == READ_CHECK]
        self.assertEqual(len(decisions), 1530)
        self.assertEqual(report['authorization_latency']['sample_count'], 1500)
        writes = [call for call in server.calls if call[1] == 'POST' and call[2] != READ_CHECK]
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
        self.assertTrue(report['meta_recovery']['verified_without_rescan'])
        self.assertEqual(self.scans.call_count, 6)
        self.assertEqual(server.people, ('A', 'B'))
        self.assertTrue(report['content_changes']['source_baseline_restored'])

    def test_latency_statistics_use_nearest_rank_without_discarding_tail(self):
        self.assertEqual(ONLINE.latency_statistics(list(range(1, 101))),
                         {'sample_count': 100, 'p50_ms': 50, 'p95_ms': 95, 'min_ms': 1, 'max_ms': 100})
        for invalid in ([], [float('nan')], [float('inf')], [-1], [True]):
            with self.subTest(invalid=invalid), self.assertRaises(ONLINE.SuiteError):
                ONLINE.latency_statistics(invalid)

    def test_latency_sampling_is_read_only_and_excludes_warmups(self):
        server, lifecycle = self.lifecycle()
        command, _ = lifecycle.grant(self.facts, initialize=True)
        before_calls = len(server.calls)
        self.assertEqual((ONLINE.LATENCY_ROUNDS, ONLINE.LATENCY_SAMPLES, ONLINE.LATENCY_WARMUPS), (3, 500, 10))
        # Each warmup costs 1s; each measured request costs 1..500ms per round.
        durations = ([1000] * 10 + list(range(1, 501))) * 3
        clock = iter(value for duration in durations for value in (0, duration * 1000000))
        report = {}
        with patch.object(ONLINE.time, 'perf_counter_ns', side_effect=lambda: next(clock)):
            lifecycle.measure_read_latency(report, self.facts, command)
        result = report['authorization_latency']
        self.assertEqual((result['sample_count'], result['warmup_count'], result['failed_count']), (1500, 30, 0))
        self.assertEqual((result['p50_ms'], result['p95_ms']), (250, 475))
        self.assertEqual(result['round_p95_range_ms'], [475, 475])
        self.assertTrue(result['unchanged_identity_rules_boundary'])
        self.assertEqual(len(result['rounds']), 3)
        self.assertTrue(all(row['samples_ms'] == list(range(1, 501)) for row in result['rounds']))
        self.assertEqual(len(server.grants), 1)
        self.assertTrue(all(call[1] == 'GET' or call[1:3] == ('POST', READ_CHECK)
                            for call in server.calls[before_calls:]))
        self.assertNotIn('private-token', json.dumps(result))

    def test_latency_failure_stops_without_retry_and_preserves_partial_evidence(self):
        for fault in ('denied', 'transport', 'invalid_time', 'extra_data', 'credential', 'context', 'grant', 'boundary', 'budget'):
            with self.subTest(fault=fault):
                server, lifecycle = self.lifecycle()
                command, _ = lifecycle.grant(self.facts, initialize=True)
                request = lifecycle.reader.request
                count = 0
                def intercept(method, path, expected, body=None):
                    nonlocal count
                    if path == READ_CHECK:
                        count += 1
                        if count == 12:
                            if fault in ('denied', 'transport'):
                                raise ONLINE.SuiteError('private diagnostic must not enter public evidence')
                            if fault == 'credential':
                                lifecycle.reader.token = 'changed-private-token'
                            if fault == 'context':
                                server.version += 1
                            if fault == 'grant':
                                server.grants[command]['expires_at'] = '2099-01-01T00:00:00Z'
                            if fault in ('invalid_time', 'extra_data'):
                                return SimpleNamespace(status=200, payload={'observed_at': 'invalid'} if fault == 'invalid_time'
                                                       else {'observed_at': '2026-10-22T00:00:00Z', 'data': []})
                    return request(method, path, expected, body)
                lifecycle.reader.request = intercept
                reads = 0
                def facts(*_):
                    nonlocal reads
                    reads += 1
                    return dict(self.facts, boundary_hash='c' * 64) if fault == 'boundary' and reads > 1 else self.facts
                report = {}
                with patch.object(ONLINE.REGISTRATION, 'read_exact_record_authorization_facts', side_effect=facts), \
                     patch.object(ONLINE.time, 'monotonic', side_effect=(lambda: 0 if count < 12 else 100) if fault == 'budget'
                                  else ONLINE.time.monotonic):
                    with self.assertRaises(ONLINE.SuiteError):
                        lifecycle.measure_read_latency(report, self.facts, command)
                result = report['authorization_latency']
                self.assertEqual(result['result'], 'failed')
                self.assertLessEqual(count, 510)
                self.assertNotIn('private', json.dumps(result))
                if fault in ('denied', 'transport', 'invalid_time', 'extra_data'):
                    self.assertEqual((count, result['sample_count'], result['failed_count']), (12, 1, 1))

    def test_initial_security_boundary_must_be_ready_before_scan_or_grant(self):
        server, lifecycle = self.lifecycle()
        report = {}
        with patch.object(ONLINE.API, 'wait_manager_read_boundary', create=True,
                          side_effect=ONLINE.SuiteError('initial Security synchronization')) as ready:
            with self.assertRaisesRegex(ONLINE.SuiteError, 'initial Security synchronization'):
                lifecycle.run(report)
        self.assertEqual(report['phase'], 'initial_protection_sync')
        self.assertEqual(ready.call_args.args[:2], (lifecycle.reader, 2))
        self.scans.assert_not_called()
        self.assertFalse(server.grants)
        self.assertFalse(server.denies)

    def test_workspace_distinguishes_exact_catalog_read_from_authorization_writes(self):
        spec = Path(__file__).resolve().parents[2] / 'console/frontend/e2e/online/pg-source-authorization.spec.js'
        program = r'''
            const { readFileSync } = require('node:fs')
            const source = readFileSync(process.argv[1], 'utf8')
            if (!source.includes('if (isEngineAuthorizationWrite(request.method(), path, base)) writes.push(path)')) {
              throw new Error('workspace listener does not use the classifier')
            }
            const body = source.match(/^function isEngineAuthorizationWrite\(method, path, base\) \{\n([\s\S]*?)^\}/m)
            if (!body) throw new Error('workspace request classifier missing')
            const classify = new Function('method', 'path', 'base', body[1])
            const base = '/api/v1/system/engines/7'
            for (const [method, suffix, expected] of [
              ['GET', '/access_grants', false], ['GET', '/access_grants/history', false],
              ['POST', '/catalog/children', false], ['POST', '/catalog/children/extra', true],
              ['POST', '/catalog/children-extra', true], ['PUT', '/catalog/children', true],
              ['DELETE', '/catalog/children', true], ['POST', '/access_grants', true],
              ['POST', '/access_grants/known/revoke', true], ['POST', '/access_denies', true],
              ['POST', '/access_denies/known/release', true], ['PATCH', '/unknown', true],
              ['POST', '/access_approval_requirements', true], ['POST', '/access_delegations', true],
            ]) {
              if (classify(method, base + suffix, base) !== expected) {
                throw new Error('incorrect workspace classification: ' + method + ' ' + suffix)
              }
            }
        '''
        result = ONLINE.subprocess.run(['node', '-e', program, str(spec)],
                                       capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_shared_manager_boundary_waits_only_for_initial_projection(self):
        client = SimpleNamespace(request=Mock(side_effect=[
            SimpleNamespace(status=403, payload={'error_code': 'security_protection_required'}),
            SimpleNamespace(status=200, payload={'data': [{'id': 2}]}),
        ]))
        with patch.object(ONLINE.time, 'monotonic', return_value=0), patch.object(ONLINE.time, 'sleep') as sleep:
            ONLINE.API.wait_manager_read_boundary(client, 2, 10)
        sleep.assert_called_once_with(1)
        for call in client.request.call_args_list:
            self.assertEqual(call.args, ('GET', '/api/v1/manager/engines', (200, 403)))

    def test_shared_manager_boundary_rejects_wrong_permissions_or_engine(self):
        for response in [SimpleNamespace(status=403, payload={'error_code': 'permission_denied'}),
                         SimpleNamespace(status=200, payload={'data': []}),
                         SimpleNamespace(status=200, payload={'data': [{'id': 3}]}),
                         SimpleNamespace(status=200, payload={'data': [{'id': 2}, {'id': 2}]}),
                         SimpleNamespace(status=200, payload={'data': {}}),
                         SimpleNamespace(status=200, payload=[])]:
            client = SimpleNamespace(request=Mock(return_value=response))
            with self.subTest(response=response), patch.object(ONLINE.time, 'monotonic', return_value=0), \
                    patch.object(ONLINE.time, 'sleep') as sleep, self.assertRaises(ONLINE.SuiteError):
                ONLINE.API.wait_manager_read_boundary(client, 2, 10)
            sleep.assert_not_called()
            client.request.assert_called_once()

    def test_shared_manager_boundary_fails_when_initial_sync_does_not_finish(self):
        client = SimpleNamespace(request=Mock(return_value=SimpleNamespace(
            status=403, payload={'error_code': 'security_protection_required'})))
        with patch.object(ONLINE.time, 'monotonic', side_effect=[0, 11]), \
                self.assertRaisesRegex(ONLINE.SuiteError, 'initial Security protection synchronization'):
            ONLINE.API.wait_manager_read_boundary(client, 2, 10)
        client.request.assert_called_once()

    def test_workspace_requires_actual_withdrawal_and_preserves_tracked_history(self):
        server, lifecycle = self.lifecycle()
        report = {'checks': []}
        with patch.object(ONLINE, 'WorkspaceObservation') as observation:
            observed = observation.return_value.__enter__.return_value
            observed.finish.side_effect = lambda: lifecycle.revoke(lifecycle.grants[-1]) or {'result': 'passed'}
            lifecycle.workspace_flow(report, '35')
            self.assertEqual(observation.call_args.args, (3, '33', ONLINE.locator(2, 'metric_people', 10), 2, '35', lifecycle.grants[-1]))
        self.assertEqual(report['checks'], ONLINE.WORKSPACE_CHECKS)
        self.assertIsNotNone(server.grants[lifecycle.grants[-1]]['revocation'])
        lifecycle.cleanup()

    def test_valid_grant_does_not_replace_manager_read_function(self):
        server, lifecycle = self.lifecycle()
        report = {'checks': []}
        lifecycle.function_permission_flow(report, server.client('business'), '36')
        proof = report['function_permission_boundary']
        self.assertEqual(proof['principal_id'], '36')
        self.assertEqual(proof['preview_status'], 403)
        self.assertEqual(proof['error_code'], 'permission_denied')
        self.assertTrue(proof['grant_still_active'])
        self.assertTrue(proof['same_credential'])
        self.assertTrue(proof['withdrawn'])
        self.assertEqual(report['checks'], ['data_grant_without_read_function_denied'])
        command = proof['grant_id']
        self.assertEqual(server.grants[command]['recipient_id'], '36')
        self.assertIsNotNone(server.grants[command]['revocation'])
        preview_calls = [call for call in server.calls if call[2] == '/api/v1/manager/preview']
        self.assertEqual([call[0] for call in preview_calls], ['business'])
        self.assertNotIn('private-token', json.dumps(report))
        lifecycle.cleanup()

    def test_function_boundary_rejects_bypass_leaks_lost_grant_or_added_permission(self):
        for fault in ('function_grant_bypasses_guard', 'function_wrong_denial', 'function_denial_leaks',
                      'function_denial_revokes_grant', 'function_grant_missing', 'function_wrong_binding',
                      'function_permission_added'):
            with self.subTest(fault=fault):
                server, lifecycle = self.lifecycle(fault)
                with self.assertRaises(ONLINE.SuiteError):
                    lifecycle.function_permission_flow({'checks': []}, server.client('business'), '36')
                if fault == 'function_permission_added':
                    self.assertFalse(server.grants)
                lifecycle.cleanup()
                self.assertTrue(all(row['revocation'] is not None for row in server.grants.values()))

    def test_workspace_isolates_retained_organization_grants_without_erasing_history(self):
        server, lifecycle = self.lifecycle()
        report = {}
        lifecycle.run(report)
        retained = {key for key, row in server.grants.items()
                    if row['revocation'] is None and key not in server.expired_grants}
        self.assertEqual({server.grants[key]['recipient_type'] for key in retained},
                         {'department', 'project_group'})
        previous = set(server.grants)
        with patch.object(ONLINE, 'WorkspaceObservation') as observation:
            observed = observation.return_value.__enter__.return_value
            def withdraw():
                self.assertTrue(all(server.grants[key]['revocation'] for key in retained))
                self.assertTrue(previous.issubset(server.grants))
                self.assertFalse(any(server.denies.values()))
                active = {key for key, row in server.grants.items()
                          if row['revocation'] is None and key not in server.expired_grants}
                self.assertEqual(active, {lifecycle.grants[-1]})
                lifecycle.revoke(lifecycle.grants[-1])
                return {'result': 'passed'}
            observed.finish.side_effect = withdraw
            lifecycle.workspace_flow(report, '35')
        self.assertEqual(report['workspace_isolation'], 'zero_current_source_rules')
        lifecycle.cleanup()

    def test_workspace_does_not_issue_or_open_browser_after_failed_isolation(self):
        _, lifecycle = self.lifecycle()
        with patch.object(lifecycle, 'cleanup', side_effect=ONLINE.SuiteError('residual rules')), \
             patch.object(lifecycle, 'grant') as grant, \
             patch.object(ONLINE, 'WorkspaceObservation') as browser:
            with self.assertRaisesRegex(ONLINE.SuiteError, 'residual rules'):
                lifecycle.workspace_flow({'checks': []}, '35')
            grant.assert_not_called()
            browser.assert_not_called()

    def test_workspace_missing_withdrawal_or_browser_failure_still_owns_cleanup(self):
        for fault in ('missing_withdrawal', 'browser_failed'):
            with self.subTest(fault=fault):
                server, lifecycle = self.lifecycle()
                with patch.object(ONLINE, 'WorkspaceObservation') as observation:
                    observed = observation.return_value.__enter__.return_value
                    observed.finish.return_value = {'result': 'passed'}
                    if fault == 'browser_failed':
                        observed.finish.side_effect = ONLINE.SuiteError('browser failed')
                    with self.assertRaises(ONLINE.SuiteError):
                        lifecycle.workspace_flow({'checks': []}, '35')
                self.assertEqual(len(lifecycle.grants), 1)
                lifecycle.cleanup()
                self.assertTrue(all(row['revocation'] for row in server.grants.values()))

    def test_content_changes_require_current_data_stable_binding_deny_and_withdrawal(self):
        for fault in ('stale_content', 'content_loses_deny', 'content_restores_withdrawn',
                      'direct_replay_restores_revoked', 'content_changes_identity', 'content_changes_boundary'):
            with self.subTest(fault=fault):
                server, lifecycle = self.lifecycle(fault)
                self.scans.side_effect = lambda *_: 'exact-content-basic'
                facts = dict(self.facts)
                if fault == 'content_changes_boundary':
                    facts['boundary_hash'] = 'c' * 64
                with patch.object(ONLINE.REGISTRATION, 'read_exact_record_authorization_facts', return_value=facts), \
                     patch.object(ONLINE, 'item', return_value={
                         'id': 99 if fault == 'content_changes_identity' else 10, 'fingerprint': 'a' * 64}):
                    with self.assertRaises(ONLINE.SuiteError):
                        lifecycle.content_change_flow({'checks': []}, self.facts, {'id': 10, 'fingerprint': 'a' * 64},
                            ONLINE.locator(2, 'metric_people', 10), ONLINE.locator(2, 'metric_events', 11))
                self.assertEqual(server.people, ('A', 'B'))
                lifecycle.cleanup()
                self.assertFalse(any(server.denies.values()))
                self.assertTrue(all(row['revocation'] is not None for row in server.grants.values()))

    def test_lost_source_mutation_response_restores_baseline_and_leaves_grant_owned_for_cleanup(self):
        mutate = self.content.side_effect
        for interrupted_action in ('content-change', 'content-reset'):
            with self.subTest(action=interrupted_action):
                server, lifecycle = self.lifecycle()
                self.scans.side_effect = lambda *_: 'exact-content-basic'
                interrupted = False
                def lost_response(action):
                    nonlocal interrupted
                    mutate(action)
                    if action == interrupted_action and not interrupted:
                        interrupted = True
                        raise ONLINE.SuiteError('source operation response lost')
                self.content.side_effect = lost_response
                with self.assertRaisesRegex(ONLINE.SuiteError, 'response lost'):
                    lifecycle.content_change_flow({'checks': []}, self.facts, {'id': 10, 'fingerprint': 'a' * 64},
                        ONLINE.locator(2, 'metric_people', 10), ONLINE.locator(2, 'metric_events', 11))
                self.assertTrue(interrupted)
                self.assertEqual(server.people, ('A', 'B'))
                self.assertEqual(len(lifecycle.grants), 1)
                lifecycle.cleanup()
                self.assertFalse(any(server.denies.values()))
                self.assertTrue(all(row['revocation'] is not None for row in server.grants.values()))

    def test_meta_recovery_rejects_fallback_wrong_status_lost_deny_and_leaks(self):
        for fault in ('outage_fallback', 'outage_masquerades_as_missing', 'outage_leaks', 'recovery_loses_deny'):
            with self.subTest(fault=fault):
                server, lifecycle = self.lifecycle(fault)
                with self.assertRaises(ONLINE.SuiteError):
                    lifecycle.meta_recovery_flow({}, self.facts, ONLINE.locator(2, 'metric_people', 10),
                                                  ONLINE.locator(2, 'metric_events', 11))
                lifecycle.cleanup()
                self.assertFalse(server.meta_down)
                self.assertFalse(any(server.denies.values()))
                self.assertTrue(all(row['revocation'] is not None for row in server.grants.values()))

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

    def test_functional_roles_require_current_tenant_scope_without_losing_memberships(self):
        client = Mock()
        current = Server().context('reader')
        client.request.return_value.payload = current
        principal, _ = ONLINE.identity(client, 3, ONLINE.READER_PERMISSIONS)
        self.assertEqual(principal, '33')
        ONLINE.organization_memberships(client, {'department': '41', 'project_group': '51'})
        for scope in (None, {}, {'type': 'platform'}, {'type': 'tenant', 'tenant_id': '4'},
                      {'type': 'department', 'tenant_id': '3', 'department_id': '41'},
                      {'type': 'project_group', 'tenant_id': '3', 'project_group_id': '51'},
                      {'type': 'tenant', 'tenant_id': '3', 'department_id': None}):
            with self.subTest(scope=scope):
                invalid = json.loads(json.dumps(current))
                invalid['authorization']['role_assignments'][0]['scope'] = scope
                client.request.return_value.payload = invalid
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.identity(client, 3, ONLINE.READER_PERMISSIONS)

    def test_membership_actor_requires_existing_same_tenant_administrator(self):
        client = Mock()
        context = Server().context('coordinator')
        context['authorization']['role_assignments'][0]['role_key'] = 'tenant.administrator'
        client.request.return_value.payload = context
        actor, _ = ONLINE.organization_administrator(client, 3)
        self.assertEqual(actor, '35')
        for change in ('tenant', 'principal', 'permissions', 'role', 'scope', 'token'):
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
                elif change == 'scope':
                    invalid['authorization']['role_assignments'][0]['scope'] = {'type': 'project_group', 'tenant_id': '3', 'project_group_id': '51'}
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
        with patch.object(lifecycle, 'measure_read_latency'), \
             patch.object(ONLINE.time, 'monotonic', side_effect=[0, 1, 0, 1, 100]):
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
        server.grants['unowned'] = {'request_id': 'unowned', 'revocation': None, 'catalog_path': lifecycle.path}
        with self.assertRaises(ONLINE.SuiteError):
            lifecycle.cleanup()
        self.assertIsNone(server.grants['unowned']['revocation'])

    def test_cleanup_continues_other_recipients_without_touching_unowned_grants(self):
        server, lifecycle = self.lifecycle()
        lifecycle.grant(self.facts, recipient_type='department', recipient_id='41')
        lifecycle.grant(self.facts, recipient_type='project_group', recipient_id='51')
        server.grants['unowned'] = {'request_id': 'unowned', 'revocation': None, 'catalog_path': lifecycle.path,
                                    'recipient_type': 'department', 'recipient_id': '41'}
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
                       'ADDP_ONLINE_FIXTURE_CONFIRMER_ACCESS_TOKEN': 'business',
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
                     patch.object(ONLINE, 'identity', side_effect=[('33', {}), ('31', {}), ('34', {}), ('36', {})]), \
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
                if not after_deny:
                    del env['ADDP_ONLINE_FIXTURE_CONFIRMER_ACCESS_TOKEN']
                    with patch.dict(ONLINE.os.environ, env, clear=True), patch.object(ONLINE.Lifecycle, 'run') as run:
                        self.assertEqual(ONLINE.main(), 1)
                        run.assert_not_called()
                    missing = json.loads((Path(directory) / (ONLINE.SUITE + '.json')).read_text())
                    self.assertEqual(missing['phase'], 'fixture_identity_validation')
                    self.assertNotIn('grant_commands', missing)


class PGCatalogApprovalTest(unittest.TestCase):
    def setUp(self):
        self.reset()
        self.clock = itertools.count()
        patch.object(ONLINE.time, 'monotonic', side_effect=lambda: next(self.clock) * 10).start()
        patch.object(ONLINE.time, 'sleep').start()
        patch.object(ONLINE.REGISTRATION, 'read_exact_record_authorization_facts', return_value=self.facts).start()
        patch.object(ONLINE, 'item', return_value={'id': 11, 'fingerprint': 'c' * 64}).start()
        self.outage = patch.object(ONLINE, 'DependencyOutage').start().return_value
        self.outage.before, self.outage.after = {}, {}
        def down():
            self.server.catalog_down = True
            return self.outage
        def restored(*_):
            self.server.catalog_down = False
            if self.server.fault == 'catalog_recovery_restores_grant':
                self.server.grants[self.request_id]['revocation'] = None
        self.outage.__enter__.side_effect = down
        self.outage.__exit__.side_effect = restored
        self.addCleanup(patch.stopall)

    def reset(self):
        self.server = Server()
        self.path = ONLINE.table_path(2, 'metric_events')
        self.facts = {'item_fingerprint': 'c' * 64, 'boundary_hash': 'd' * 64}
        self.entry = {'id': '553f9868-9021-4d23-bae2-43d5a9fbc030', 'version': '1',
                      'governance_status': 'discovered', 'visibility': 'tenant', 'responsibilities': [], 'semantic_links': []}
        self.requirement = {'id': 'f96cac39-7738-4b3f-b8a2-166b0a1a2e21', 'engine_id': '2',
            'catalog_path': self.path, 'item_fingerprint': self.facts['item_fingerprint'], 'mode': 'catalog', 'version': 1,
            'updated_at': '2026-10-16T09:00:01+08:00'}
        self.fulfillments = {}
        self.lifecycle = ONLINE.Lifecycle(self.client('operator'), self.client('reader'), 3, 2, '33',
            {'department': '41', 'project_group': '51'}, self.client('outsider'), self.client('coordinator'),
            self.client('login'), 'external-online-consumer', 'private-password')
        self.report = {'checks': [], 'principals': {'operator': '35'}}

    def client(self, kind):
        client = self.server.client(kind)
        original = client.request
        def request(method, url, expected, body=None, **kwargs):
            path = urlsplit(url).path
            direct = path.endswith('/access_grants') and method == 'POST'
            update = '/access_approval_requirements/' in path and method == 'PUT'
            fault = self.server.fault
            valid_direct = direct and (body['request_id'] in self.server.grants or (
                self.requirement['mode'] == 'independent' and (body['requirement_version'] == str(self.requirement['version'])
                                                            or fault == 'stale_direct_accepted')))
            if (valid_direct or ('/catalog/' not in path and not update
                    and not path.endswith(('/access_approval_requirements', '/access_handling_requirement'))
                    and not direct)):
                return original(method, url, expected, body, **kwargs)
            self.server.calls.append((kind, method, path, body))
            status, result = 200, {}
            if update:
                if kind != 'operator' and fault != 'missing_update_permission_ignored':
                    result, status = {'error': 'update forbidden'}, 403
                elif body['version'] != self.requirement['version'] and fault != 'stale_update_accepted':
                    result, status = {'error': 'stale update', 'error_code': 'resource_version_conflict'}, 409
                else:
                    if body['mode'] != self.requirement['mode'] or fault == 'same_mode_changes_version':
                        self.requirement.update(mode=body['mode'], version=self.requirement['version'] + 1)
                        self.requirement['updated_at'] = f"2026-10-16T09:00:0{self.requirement['version']}+08:00"
                        # The write returns wallClock().UTC(); the persisted PG
                        # timestamptz read can represent that instant in local time.
                        result = dict(self.requirement, updated_at=f"2026-10-16T01:00:0{self.requirement['version']}Z")
                    else:
                        result = dict(self.requirement)
                        if fault == 'same_mode_changes_timestamp':
                            result['updated_at'] = '2026-10-16T09:01:03+08:00'
                        elif fault == 'same_mode_drops_timestamp_zone':
                            result['updated_at'] = '2026-10-16T01:00:03'
                    if fault == 'switch_rewrites_old_grant':
                        for row in self.server.grants.values():
                            row['requirement_version'] = str(self.requirement['version'])
            elif direct:
                status, result = 409, {'error': 'wrong approval mode', 'error_code': 'engine_access_grant_approval_changed'}
                if fault == 'direct_fallback':
                    result, status = dict(body, revocation=None), 201
                    self.server.grants[body['request_id']] = result
                elif fault == 'duplicate_masks_approval':
                    result['error_code'] = 'engine_access_grant_relation_exists'
            elif path.endswith('/access_handling_requirement'):
                result = {'mode': 'independent' if self.server.catalog_down and fault == 'implicit_mode_change'
                          else self.requirement['mode'], 'requirement_version': str(self.requirement['version'])}
            elif '/catalog/' in path and self.server.catalog_down:
                status, result = 503, {'error': 'module unavailable'}
                if fault == 'offline_fulfillment_issued':
                    self.issue(body['request_id'])
                    status, result = 200, {'state': 'accepted'}
                elif fault == 'outage_wrong_status':
                    status = 404
            elif path.endswith('/access_approval_requirements'):
                result, status = dict(self.requirement), 201
            elif path == '/api/v1/catalog/entries':
                result = {'data': [] if fault == 'missing_catalog_source' else [dict(self.entry)], 'total': 1}
            elif path.endswith('/sharing_boundary'):
                result = dict(self.facts, native_target=self.path)
                if fault == 'wrong_boundary':
                    result['boundary_hash'] = 'e' * 64
            elif path.endswith('/sharing_decisions'):
                self.decision = dict(body)
                result, status = dict(body, id=body['decision_id'], confirmed_by='36', self_beneficiary=False,
                    item_fingerprint=self.facts['item_fingerprint'], boundary_hash=self.facts['boundary_hash']), 201
                if fault == 'wrong_confirmer':
                    result['confirmed_by'] = '33'
                if fault == 'confirmation_grants':
                    self.issue('unrequested')
            elif path.endswith('/sharing_fulfillments'):
                if body['request_id'] in self.fulfillments:
                    row = self.fulfillments[body['request_id']]
                    result = {'request_id': body['request_id'], 'state': row['state']}
                    if result['state'] == 'closed' and fault == 'closed_stale_request_reopens':
                        result['state'] = 'pending'
                    return SimpleNamespace(status=200, payload=result)
                if body['request_id'] not in self.server.grants and (self.requirement['mode'] != 'catalog'
                        or body['requirement_version'] != str(self.requirement['version'])):
                    self.fulfillments[body['request_id']] = dict(body, state='pending' if fault == 'stale_catalog_never_closes'
                                                               else 'closed', granted_at=None)
                    if fault == 'stale_catalog_auto_upgrades':
                        self.fulfillments[body['request_id']].update(state='accepted', requirement_version=str(self.requirement['version']))
                    return SimpleNamespace(status=202, payload={'request_id': body['request_id'], 'state': 'pending'})
                self.request_id = body['request_id']
                if self.request_id not in self.server.grants and fault != 'never_issue':
                    self.issue(self.request_id)
                    if fault == 'lost_issuance_response':
                        raise ONLINE.SuiteError('private upstream response must not enter the public report')
                elif self.request_id in self.server.grants:
                    grant = self.server.grants[self.request_id]
                    if fault == 'replay_renews':
                        grant['granted_at'] = '2026-10-16T02:00:00Z'
                    if fault == 'replay_restores_revoked':
                        grant['revocation'] = None
                result = {'request_id': self.request_id, 'state': 'accepted'}
            elif '/sharing_fulfillments/' in path:
                pending = self.fulfillments.get(path.rsplit('/', 1)[-1])
                if pending is not None:
                    return SimpleNamespace(status=200, payload=dict(pending))
                grant = self.server.grants.get(path.rsplit('/', 1)[-1])
                if grant is None:
                    result, status = {'error': 'not found'}, 404
                else:
                    result = {'request_id': grant['request_id'], 'state': 'accepted', 'granted_at': grant['granted_at'],
                              'decision_id': self.decision['decision_id'], 'requirement_version': '1',
                              'recipient_id': '33', 'action': 'read'}
            elif method == 'PUT':
                self.entry.update(body, version='2')
                result = dict(self.entry)
            else:
                result = dict(self.entry)
            if status not in expected:
                raise ONLINE.SuiteError('unexpected Catalog test status')
            return SimpleNamespace(status=status, payload=result)
        client.request = request
        return client

    def issue(self, request_id):
        decision = self.decision
        grant = {'request_id': request_id, 'catalog_request_id': request_id, 'approval_mode': 'catalog',
                 'engine_id': '2', 'operator_principal_id': '35', 'requirement_version': '1',
                 'catalog_path': self.path, 'item_fingerprint': self.facts['item_fingerprint'],
                 'boundary_hash': self.facts['boundary_hash'], 'recipient_type': 'user', 'recipient_id': '33',
                 'action': 'read', 'expiry_mode': 'at_time', 'expires_at': decision['expires_at'],
                 'granted_at': '2026-10-16T01:00:00Z', 'revocation': None}
        if self.server.fault == 'wrong_grant_boundary':
            grant['boundary_hash'] = 'e' * 64
        if self.server.fault == 'wrong_grant_recipient':
            grant['recipient_id'] = '34'
        self.server.grants[request_id] = grant

    def run_flow(self):
        self.lifecycle.catalog_flow(self.report, self.client('business'), '36')

    def test_business_approval_uses_formal_human_commands_and_system_history_not_runtime_credentials(self):
        self.run_flow()
        self.assertEqual(len(self.report['checks']), 21)
        self.assertEqual(self.report['catalog_approval']['rows'], 4)
        self.assertTrue(self.report['catalog_approval']['revoked_replay_denied'])
        decisions = [call for call in self.server.calls if call[2].endswith('/sharing_decisions')]
        requests = [call for call in self.server.calls if call[2].endswith('/sharing_fulfillments')]
        self.assertEqual([call[0] for call in decisions], ['business'])
        self.assertEqual([call[0] for call in requests], ['operator'] * 7)
        self.assertEqual(requests[0][3], requests[-2][3])
        self.assertNotEqual(requests[0][3]['request_id'], requests[-3][3]['request_id'])
        self.assertEqual(set(requests[0][3]), {'request_id', 'decision_id', 'requirement_version'})
        self.assertFalse(any('/runtime/' in call[2] for call in self.server.calls))
        direct = [call for call in self.server.calls if call[2].endswith('/access_grants') and call[1] == 'POST']
        self.assertEqual(len(direct), 6)
        self.assertTrue(all(call[3]['recipient_id'] == '34' and not call[3]['initialize_approval'] for call in direct))
        self.assertEqual(self.report['approval_mode_handoff']['versions'], [1, 2, 3])
        self.assertEqual(self.report['approval_mode_handoff']['stale_catalog_state'], 'closed')
        self.assertEqual(self.report['approval_mode_noop_timestamps'], {
            'before': '2026-10-16T01:00:03Z', 'after': '2026-10-16T09:00:03+08:00'})
        self.assertEqual(self.requirement['version'], 3)
        self.assertTrue(self.report['catalog_outage']['same_credential'])
        self.assertFalse(self.server.catalog_down)
        self.assertEqual(self.lifecycle.paths, [ONLINE.table_path(2, 'metric_people'), self.path])
        self.lifecycle.cleanup()
        self.assertFalse(any(self.server.denies.values()))
        self.assertTrue(all(row['revocation'] is not None for row in self.server.grants.values()))

    def test_wrong_confirmation_binding_issuance_scope_replay_or_missing_convergence_cannot_pass(self):
        for fault in ('missing_catalog_source', 'wrong_boundary', 'wrong_confirmer',
                      'wrong_grant_boundary', 'wrong_grant_recipient', 'never_issue',
                      'replay_renews', 'replay_restores_revoked'):
            with self.subTest(fault=fault):
                self.reset()
                self.server.fault = fault
                with self.assertRaises(ONLINE.SuiteError):
                    self.run_flow()
                self.assertNotEqual(len(self.report['checks']), 21)

    def test_approval_switch_rejects_permission_version_grant_mutation_and_silent_catalog_upgrade(self):
        for fault in ('missing_update_permission_ignored', 'switch_rewrites_old_grant', 'stale_direct_accepted',
                      'same_mode_changes_version', 'same_mode_changes_timestamp', 'same_mode_drops_timestamp_zone',
                      'stale_update_accepted', 'direct_replay_renews', 'direct_replay_restores_revoked',
                      'stale_catalog_auto_upgrades', 'stale_catalog_never_closes', 'closed_stale_request_reopens'):
            with self.subTest(fault=fault):
                self.reset()
                self.server.fault = fault
                with self.assertRaises(ONLINE.SuiteError):
                    self.run_flow()
                self.assertFalse(self.server.catalog_down)
                self.lifecycle.cleanup()
                self.assertTrue(all(row['revocation'] is not None for row in self.server.grants.values()))

    def test_catalog_outage_cannot_mask_read_dependency_fallback_mode_change_or_recovery_regrant(self):
        for fault in ('catalog_blocks_existing_read', 'direct_fallback', 'duplicate_masks_approval',
                      'implicit_mode_change', 'offline_fulfillment_issued', 'outage_wrong_status',
                      'catalog_recovery_restores_grant', 'catalog_blocks_rule_writes'):
            with self.subTest(fault=fault):
                self.reset()
                self.server.fault = fault
                with self.assertRaises(ONLINE.SuiteError):
                    self.run_flow()
                self.assertFalse(self.server.catalog_down)
                self.lifecycle.cleanup()
                self.assertFalse(any(self.server.denies.values()))
                self.assertTrue(all(row['revocation'] is not None for row in self.server.grants.values()))

    def test_unknown_issuance_outcome_is_owned_before_send_and_cleanup_checks_both_targets(self):
        self.server.fault = 'lost_issuance_response'
        with self.assertRaises(ONLINE.SuiteError):
            self.run_flow()
        self.assertIn(self.request_id, self.lifecycle.grants)
        with patch.object(self.lifecycle, 'listing', wraps=self.lifecycle.listing) as listing:
            self.lifecycle.cleanup()
            targets = {call.kwargs['catalog_path']['segments'][-1]['name'] for call in listing.call_args_list}
            self.assertEqual(targets, {'metric_people', 'metric_events'})
        self.assertIsNotNone(self.server.grants[self.request_id]['revocation'])
        self.assertEqual(len(self.lifecycle.paths), 2)


class PGDependencyOutageTest(unittest.TestCase):
    module = 'meta'

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.env = {'ADDP_ONLINE_TEST': '1', 'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1',
            'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'POSTGRES_DB': 'addp_online',
            'PATH': '/usr/bin:/bin',
            'META_URL': 'http://127.0.0.1:8082', 'MANAGER_URL': 'http://127.0.0.1:8081',
            'SYSTEM_URL': 'http://127.0.0.1:8180', 'GATEWAY_URL': 'http://127.0.0.1:8000',
            'CATALOG_URL': 'http://127.0.0.1:8192',
            'ADDP_ONLINE_SECRET_DIR': self.directory.name,
            'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'must-not-inherit',
            'ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN': 'must-not-inherit',
            'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'must-not-inherit'}
        patch.dict(ONLINE.os.environ, self.env, clear=True).start()
        patch.object(ONLINE.sys, 'platform', 'linux').start()
        patch.object(ONLINE.os, 'uname', return_value=SimpleNamespace(machine='x86_64')).start()
        patch.object(ONLINE.Path, 'exists', return_value=False).start()
        patch.object(ONLINE.PREFLIGHT, 'git', return_value='commit').start()
        self.addCleanup(patch.stopall)

    def identity(self, module, recovered=False):
        return {'instance_id': 'new' if recovered and module == self.module else 'original-' + module,
                'started_at': '2026-10-16T01:01:00+00:00' if recovered and module == self.module else '2026-10-16T01:00:00+00:00',
                'build_id': module + '-build', 'git_commit': 'commit', 'source_fingerprint': module + '-source'}

    def test_unsafe_host_is_refused_before_lifecycle(self):
        with patch.dict(ONLINE.os.environ, {'GITHUB_ACTIONS': 'false'}), patch.object(ONLINE.subprocess, 'run') as run:
            with self.assertRaises(ONLINE.SuiteError):
                REAL_DEPENDENCY_OUTAGE(self.module)
            run.assert_not_called()
        with patch.object(ONLINE.subprocess, 'run') as run:
            with self.assertRaises(ONLINE.SuiteError):
                REAL_DEPENDENCY_OUTAGE('system')
            run.assert_not_called()

    def test_content_fixture_is_bounded_token_free_and_checks_source_receipt(self):
        for action, receipt in (('content-change', 'pg-content:changed'), ('content-reset', 'pg-content:baseline')):
            with self.subTest(action=action), \
                 patch.object(ONLINE.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout=receipt)) as run:
                REAL_MUTATE_SOURCE_CONTENT(action)
                self.assertEqual(run.call_args.args[0][-1], action)
                self.assertEqual(run.call_args.kwargs['timeout'], 30)
                self.assertEqual(run.call_args.kwargs['env']['ONLINE_SUITE'], ONLINE.SUITE)
                self.assertFalse(any(key.startswith(('ADDP_ONLINE_TEST_USER_', 'ADDP_ONLINE_ADMIN_USER_',
                                                    'ADDP_ONLINE_FIXTURE_')) for key in run.call_args.kwargs['env']))
        for response in (SimpleNamespace(returncode=1, stdout='pg-content:changed'),
                         SimpleNamespace(returncode=0, stdout='wrong rows'),
                         ONLINE.subprocess.TimeoutExpired('private-output', 30)):
            with self.subTest(response=response), patch.object(ONLINE.subprocess, 'run', side_effect=[response]):
                with self.assertRaises(ONLINE.SuiteError):
                    REAL_MUTATE_SOURCE_CONTENT('content-change')
        for action, overrides in (('arbitrary-sql', {}), ('content-change', {'GITHUB_ACTIONS': 'false'}),
                                  ('content-reset', {'POSTGRES_DB': 'personal'})):
            with self.subTest(action=action, overrides=overrides), patch.dict(ONLINE.os.environ, overrides), \
                 patch.object(ONLINE.subprocess, 'run') as run:
                with self.assertRaises(ONLINE.SuiteError):
                    REAL_MUTATE_SOURCE_CONTENT(action)
                run.assert_not_called()

    def test_health_uses_shared_client_and_the_actual_gateway_standalone_contract(self):
        outage = REAL_DEPENDENCY_OUTAGE(self.module)
        payload = {'module': 'meta', 'status': 'ready', 'role': 'backend', 'registration_state': 'registered',
            'instance_id': 'ca8c0683-5f9f-4d15-9c97-f00bb74ba05c', 'started_at': '2026-10-16T01:00:00+00:00',
            'built_at': '2026-10-16T00:00:00+00:00', 'build_id': 'build', 'git_commit': 'commit', 'source_fingerprint': 'sha256:source'}
        with patch.object(ONLINE.API, 'GatewayClient') as client:
            client.return_value.request.return_value.payload = payload
            self.assertEqual(outage.snapshot('meta')['instance_id'], payload['instance_id'])
            self.assertEqual(client.call_args.args, ('http://127.0.0.1:8082', '', 10))
            self.assertEqual(client.return_value.request.call_args.args, ('GET', '/health/ready', (200,)))
            payload['module'] = 'gateway'
            for key in ('role', 'registration_state', 'instance_id'):
                payload.pop(key)
            self.assertNotIn('instance_id', outage.snapshot('gateway'))
            payload['git_commit'] = 'different-build'
            with self.assertRaises(ONLINE.PREFLIGHT.PreflightError):
                outage.snapshot('gateway')

    def test_standard_stop_start_private_output_and_identity_verification(self):
        outage = REAL_DEPENDENCY_OUTAGE(self.module)
        snapshots = lambda module: self.identity(module, run.call_count == 2)
        with patch.object(outage, 'snapshot', side_effect=snapshots), \
             patch.object(ONLINE.subprocess, 'run', return_value=SimpleNamespace(returncode=0)) as run, \
             patch.object(ONLINE.socket, 'create_connection', side_effect=ConnectionRefusedError):
            with outage:
                self.assertTrue(outage.stopped)
            self.assertEqual([call.args[0] for call in run.call_args_list], [
                ['bash', 'scripts/dev/stop.sh', '-' + self.module], ['bash', 'scripts/dev/start.sh', '-' + self.module]])
            self.assertEqual(ONLINE.socket.create_connection.call_args.args[0],
                             ('127.0.0.1', 8082 if self.module == 'meta' else 8192))
            for call in run.call_args_list:
                self.assertFalse(any(key.startswith(('ADDP_ONLINE_TEST_USER_', 'ADDP_ONLINE_ADMIN_USER_',
                                                     'ADDP_ONLINE_FIXTURE_')) for key in call.kwargs['env']))
                self.assertEqual(call.kwargs['timeout'], 120)
                self.assertTrue(Path(call.kwargs['stdout'].name).resolve().is_relative_to(Path(self.directory.name).resolve()))

    def test_failure_and_live_listener_still_attempt_recovery(self):
        for cause in ('body', 'listener', 'stop', 'timeout'):
            with self.subTest(cause=cause):
                outage = REAL_DEPENDENCY_OUTAGE(self.module)
                snapshots = lambda module: self.identity(module, run.call_count == 2)
                failure = ONLINE.subprocess.TimeoutExpired('stop', 120) if cause == 'timeout' else SimpleNamespace(returncode=1)
                with patch.object(outage, 'snapshot', side_effect=snapshots), \
                     patch.object(ONLINE.subprocess, 'run', side_effect=[failure if cause in {'stop', 'timeout'} else SimpleNamespace(returncode=0),
                                                                       SimpleNamespace(returncode=0)]) as run, \
                     patch.object(ONLINE.socket, 'create_connection', side_effect=None if cause == 'listener' else ConnectionRefusedError):
                    with self.assertRaises(ONLINE.SuiteError):
                        with outage:
                            raise ONLINE.SuiteError('injected body failure')
                    self.assertEqual(run.call_count, 2)

    def test_recovery_rejects_noop_changed_build_or_another_owner_restart(self):
        for fault in ('noop', 'build', 'other_owner'):
            with self.subTest(fault=fault):
                outage = REAL_DEPENDENCY_OUTAGE(self.module)
                def snapshot(module):
                    result = self.identity(module, run.call_count == 2 and fault != 'noop')
                    if run.call_count == 2 and ((fault == 'build' and module == self.module) or (fault == 'other_owner' and module == 'system')):
                        result['build_id'] = 'changed'
                    return result
                with patch.object(outage, 'snapshot', side_effect=snapshot), \
                     patch.object(ONLINE.subprocess, 'run', return_value=SimpleNamespace(returncode=0)) as run, \
                     patch.object(ONLINE.socket, 'create_connection', side_effect=ConnectionRefusedError):
                    with self.assertRaises(ONLINE.SuiteError):
                        with outage:
                            pass


class PGCatalogOutageTest(PGDependencyOutageTest):
    module = 'catalog'


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

    def test_workspace_private_logins_exact_completion_and_no_token_inheritance(self):
        environment = dict(self.environment, ADDP_ONLINE_PG_OPERATOR_USERNAME='external-online-pg-authorizer',
                           ADDP_ONLINE_PG_OPERATOR_PASSWORD='operator-private-password')
        with patch.dict(ONLINE.os.environ, environment), patch.object(ONLINE.subprocess, 'Popen', return_value=self.process) as popen, \
             patch.object(ONLINE.os, 'killpg'):
            with REAL_WORKSPACE_OBSERVATION(2, '33', 'target', 1, '35', 'owned-grant') as browser:
                expected = dict(browser.binding, schema_version='addp.pg-authorization-workspace/v1', result='passed',
                                checks=ONLINE.WORKSPACE_CHECKS, login_requests=2, preview_statuses=[200, 403])
                browser.evidence.write_text(json.dumps(expected))
                self.assertEqual(browser.finish(), expected)
                env = popen.call_args.kwargs['env']
                self.assertFalse(any('TOKEN' in key or 'SECRET' in key for key in env))
                self.assertEqual(env['ADDP_ONLINE_PG_OPERATOR_PASSWORD'], 'operator-private-password')
                self.assertIn(browser.test_name, popen.call_args.args[0])
                for fault in ('missing_check', 'wrong_grant', 'relogin', 'still_readable', 'extra_field'):
                    bad = dict(expected)
                    if fault == 'missing_check': bad['checks'] = bad['checks'][:-1]
                    if fault == 'wrong_grant': bad['grant_id'] = 'unowned'
                    if fault == 'relogin': bad['login_requests'] = 3
                    if fault == 'still_readable': bad['preview_statuses'] = [200, 200]
                    if fault == 'extra_field': bad['raw_token'] = 'must-not-accept'
                    browser.evidence.write_text(json.dumps(bad))
                    with self.subTest(fault=fault), self.assertRaises(ONLINE.SuiteError):
                        browser.finish()


class InitialProductionOnlineTest(unittest.TestCase):
    def setUp(self):
        self.execution = 'b9041a08-785c-48bc-bb12-c0650b720dac'
        self.command = 'c9041a08-785c-48bc-bb12-c0650b720dac'
        self.source_command = 'd9041a08-785c-48bc-bb12-c0650b720dac'
        self.facts = {'item_fingerprint': 'a' * 64, 'boundary_hash': 'b' * 64}
        self.expiry = '2030-10-20T10:00:00+00:00'
        self.calls, self.source_revoked, self.output_revoked = [], False, False
        self.bad = None
        self.reader, self.operator = Mock(), Mock()
        self.lifecycle = ONLINE.Lifecycle(self.operator, self.reader, 3, 2, '33', {}, Mock(), Mock(), Mock(),
                                          'external-online-consumer', 'private-password')
        self.lifecycle.grants.append(self.source_command)
        self.lifecycle.grant = Mock(return_value=(self.source_command, {'expires_at': self.expiry}))
        self.lifecycle.listing = Mock(side_effect=self.listing)
        self.lifecycle.revoke = Mock(side_effect=self.revoke)
        self.reader.request.side_effect = self.request
        patch.object(ONLINE, 'item', return_value={'id': 10, 'node_id': 7}).start()
        patch.object(ONLINE.REGISTRATION, 'read_exact_record_authorization_facts', return_value=self.facts).start()
        self.preview = patch.object(ONLINE, 'preview', side_effect=self.read).start()
        patch.object(ONLINE.time, 'sleep').start()
        self.addCleanup(patch.stopall)

    def row(self):
        row = dict(request_id=self.command, approval_mode='task_production', producer_owner='transfer',
                   production_execution_id=self.execution, catalog_path=self.path, recipient_type='user', recipient_id='33',
                   operator_principal_id='33', action='read', requirement_version='0', expiry_mode='at_time',
                   expires_at=self.expiry, revocation={'reason': 'test'} if self.output_revoked else None,
                   issuer_principal_id='55', issuer_membership_id='66', **self.facts)
        if self.bad:
            row.update(self.bad)
        return row

    def request(self, method, url, expected, body=None, **kwargs):
        self.calls.append((method, url))
        status, result = 200, {}
        if url.endswith('/items?branch=public'):
            result = []
        elif method == 'POST' and url.endswith('/task-definitions'):
            self.assertFalse(body['auto_scan_metadata'])
            self.assertEqual(body['config']['target']['policy'], {'apply_mode': 'replace'})
            self.path = ONLINE.table_path(2, body['config']['target']['name'])
            status, result = 201, {'id': 91}
        elif url.endswith('/91/start'):
            result = {'execution_id': self.execution}
        elif url.endswith('/91/executions?page_size=100'):
            result = {'total': 1, 'data': [{'execution_id': self.execution, 'task_id': 91}]}
        elif url.endswith('/executions/' + self.execution):
            result = {'execution_id': self.execution, 'task_id': 91, 'status': 'success',
                      'records_read': 2, 'records_written': 2,
                      'metadata': {'initial_authorization': {'status': 'issued'}}}
        elif url.endswith('/scan/runs?page_size=100'):
            result = {'total': 1, 'items': [{'parent_execution_id': self.execution, 'status': 'success',
                'execution_id': 'e9041a08-785c-48bc-bb12-c0650b720dac', 'source': 'transfer', 'triggered_by': 33,
                'scan_context': {'engine_id': 2, 'scan_depth': 'basic'},
                'metadata': {'items_scanned': 1, 'catalog_nodes_scanned': 0}}]}
        elif method == 'GET' and url.endswith('/task-definitions/91'):
            status = 404
        elif method != 'DELETE':
            self.fail('unexpected production request ' + url)
        self.assertIn(status, expected)
        return SimpleNamespace(status=status, payload=result)

    def listing(self, **options):
        if options.get('catalog_path') == getattr(self, 'path', None):
            rows = [self.row()]
            if options.get('state') == 'active' and self.output_revoked:
                rows = []
            if options.get('state') == 'revoked' and not self.output_revoked:
                rows = []
            if not options.get('history') and self.output_revoked:
                rows = []
        else:
            rows = [] if self.source_revoked else [{'request_id': self.source_command}]
        return {'total': len(rows), 'data': rows}

    def revoke(self, command):
        self.calls.append(('revoke', command))
        if command == self.source_command:
            self.source_revoked = True
        else:
            self.assertEqual(command, self.command)
            self.output_revoked = True

    def read(self, client, target, allowed, expected=None):
        source = '/metric_people?' in target
        actual = (not self.source_revoked if source else not self.output_revoked) and client is self.reader
        self.assertEqual(allowed, actual)
        if allowed:
            self.assertEqual(expected, 2)

    def test_initial_output_checks_actual_reads_and_cleans_only_owned_task(self):
        report = {'checks': []}
        self.lifecycle.production_flow(report)
        self.assertEqual(report['checks'], ONLINE.PRODUCTION_CHECKS)
        self.assertIn(self.command, self.lifecycle.grants)
        self.assertEqual(self.preview.call_count, 5)
        self.lifecycle.cleanup()
        self.assertIn(('DELETE', '/api/v1/transfer/task-definitions/91'), self.calls)
        self.assertIn(('GET', '/api/v1/transfer/task-definitions/91'), self.calls)
        self.assertEqual(report['initial_production']['scan_depth'], 'basic')

    def test_initial_grant_rejects_wrong_target_origin_source_or_expiry(self):
        for invalid in ({'approval_mode': 'independent'}, {'producer_owner': 'develop'},
                        {'production_execution_id': 'f9041a08-785c-48bc-bb12-c0650b720dac'},
                        {'recipient_id': '34'}, {'operator_principal_id': '35'}, {'issuer_principal_id': '33'},
                        {'boundary_hash': 'c' * 64}, {'requirement_version': '1'},
                        {'expires_at': '2031-10-20T10:00:00Z'}):
            self.bad = invalid
            with self.subTest(invalid=invalid), self.assertRaises(ONLINE.SuiteError):
                self.lifecycle.production_flow({'checks': []})

    def test_unknown_start_response_cleanup_recovers_by_owned_task_execution(self):
        original = self.request
        def lost(method, url, expected, body=None, **kwargs):
            result = original(method, url, expected, body, **kwargs)
            if url.endswith('/91/start'):
                raise ONLINE.SuiteError('response lost')
            return result
        self.reader.request.side_effect = lost
        with self.assertRaisesRegex(ONLINE.SuiteError, 'response lost'):
            self.lifecycle.production_flow({'checks': []})
        self.lifecycle.cleanup()
        self.assertTrue(self.source_revoked and self.output_revoked)
        self.assertIn(self.command, self.lifecycle.grants)
        self.assertLess(self.calls.index(('revoke', self.source_command)),
                        self.calls.index(('GET', '/api/v1/transfer/task-definitions/91/executions?page_size=100')))

    def test_cleanup_refuses_foreign_execution_grant(self):
        self.bad = {'production_execution_id': 'f9041a08-785c-48bc-bb12-c0650b720dac'}
        with self.assertRaises(ONLINE.SuiteError):
            self.lifecycle.production_flow({'checks': []})
        with self.assertRaises(ONLINE.SuiteError):
            self.lifecycle.cleanup()
        self.assertFalse(self.output_revoked)


if __name__ == '__main__':
    unittest.main()
