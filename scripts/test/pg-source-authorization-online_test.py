import importlib
import io
import itertools
import json
import tempfile
import unittest
import uuid
from copy import deepcopy
from datetime import datetime, timedelta, timezone
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
REAL_BIND_AUDITS = ONLINE.bind_audit_requests
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
        self.permissions = {}

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
                elif path.endswith('/direct-permission'):
                    current = server.permissions.setdefault(path.split('/')[-2], {
                        'engine_id': path.split('/')[5], 'item_fingerprint': path.split('/')[-2],
                        'state': 'allowed', 'source': 'explicit', 'version': '1'})
                    if method == 'PUT':
                        if (body['expected_source'], body['expected_version']) != (current['source'], current['version']):
                            status, result = 409, {'error_code': 'resource_version_conflict'}
                        else:
                            state = 'allowed' if body['allowed'] else 'denied'
                            if state != current['state']:
                                current.update(state=state, version=str(int(current['version']) + 1))
                    if status == 200:
                        result = deepcopy(current)
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


class AuditLatencyObservationTest(unittest.TestCase):
    def test_audit_binding_requires_exact_unique_request_windows(self):
        stamp = '2026-10-22T01:19:16.956+00:00'
        nanos = int(datetime.fromisoformat(stamp).timestamp() * 1000000000)
        request = {'started_unix_ns': nanos - 1000000, 'finished_unix_ns': nanos + 2000000}
        audit = {'id': 41, 'request_id': '0730c2c4-e230-42df-a63c-a88673af29f0',
                 'created_at': stamp, 'pre_audit_ms': 11}
        rows = [dict(request)]
        REAL_BIND_AUDITS(rows, [audit])
        self.assertEqual((rows[0]['audit_id'], rows[0]['pre_audit_ms']), (41, 11))
        for requests, audits in [([request], []), ([request, request], [audit, audit]),
                                  ([dict(request, started_unix_ns=nanos + 1000000000)], [audit]),
                                  ([request], [dict(audit, request_id='private diagnostic')])]:
            with self.subTest(audits=audits), self.assertRaises(ONLINE.SuiteError):
                REAL_BIND_AUDITS(requests, audits)

    def test_query_is_bounded_and_accepts_split_json_without_stderr(self):
        observer = ONLINE.AuditLatencyObservation({}, '33')
        observer.process = SimpleNamespace(stdin=io.BytesIO(), stdout=Mock())
        observer.process.stdout.fileno.return_value = 19
        with patch.object(ONLINE.select, 'select', return_value=([19], [], [])), \
             patch.object(ONLINE.os, 'read', side_effect=[b'{"safe":', b'true}\n']):
            self.assertEqual(observer.query('SELECT 1;'), {'safe': True})
        self.assertEqual(observer.process.stdin.getvalue(), b'SELECT 1;\n')
        for chunks, ready in [([b''], [19]), ([b'x' * (512 * 1024 + 1)], [19]), ([], [])]:
            observer.buffer = b''
            with self.subTest(ready=ready), patch.object(ONLINE.select, 'select', return_value=(ready, [], [])), \
                 patch.object(ONLINE.os, 'read', side_effect=chunks), self.assertRaises(ONLINE.SuiteError):
                observer.query('SELECT 1;')

    def test_personal_deployment_is_rejected_before_any_docker_call(self):
        with patch.dict(ONLINE.os.environ, {'GITHUB_ACTIONS': 'false'}), \
             patch.object(ONLINE.subprocess, 'run') as run, patch.object(ONLINE.subprocess, 'Popen') as popen:
            with self.assertRaises(ONLINE.SuiteError):
                with ONLINE.AuditLatencyObservation({}, '33'):
                    self.fail('personal deployment was admitted')
            run.assert_not_called()
            popen.assert_not_called()
        for principal in ('0', '33; SELECT 1', '-1'):
            with self.subTest(principal=principal), self.assertRaises(ONLINE.SuiteError):
                ONLINE.AuditLatencyObservation({}, principal)

    def test_sampler_failure_is_not_zero_wait_success(self):
        evidence = {}
        observer = ONLINE.AuditLatencyObservation(evidence, '33')
        observer.process = Mock()
        observer.stop = Mock()
        observer.stop.wait.return_value = False
        with patch.object(observer, 'sample', side_effect=ONLINE.SuiteError('private diagnostic')):
            observer.collect()
        self.assertEqual(observer.failure, 'audit_observation_failed')
        with self.assertRaises(ONLINE.SuiteError):
            observer.healthy()
        self.assertEqual(evidence['result'], 'failed')
        self.assertNotIn('private', json.dumps(evidence))

    def test_linux_disk_counters_preserve_raw_units_and_skip_loop_devices(self):
        documents = {'/proc/diskstats': '7 0 loop0 ' + ' '.join(['0'] * 17) + '\n'
                     + '259 1 nvme0n1p1 ' + ' '.join(map(str, range(17))),
                     '/proc/pressure/io': 'some avg10=0.00 total=3', '/proc/stat': 'cpu 1 2 3 4 5 6 7 8\ncpu0 1',
                     '/proc/pressure/cpu': 'some avg10=1.00 total=7', '/proc/loadavg': '1.00 1.20 0.30 4/50 100'}
        with patch.object(Path, 'read_text', autospec=True, side_effect=lambda path: documents[str(path)]):
            sample = ONLINE.disk_observation()
        self.assertEqual(list(sample['disks']), ['259:1'])
        self.assertEqual(sample['disks']['259:1']['counters'], list(range(17)))
        self.assertEqual(sample['cpu_pressure'], documents['/proc/pressure/cpu'])
        self.assertEqual(sample['load_average'], documents['/proc/loadavg'])

    def test_read_only_session_ownership_settings_and_cleanup(self):
        class Process:
            def __init__(self):
                self.stdin, self.stdout = io.BytesIO(), io.BytesIO()
                self.returncode = None
            def poll(self):
                return self.returncode
            def wait(self, timeout):
                self.input = self.stdin.getvalue()
                self.returncode = 0
                return 0
            def kill(self):
                raise AssertionError('clean observer must not require kill')
        container = {'Id': 'a' * 64, 'Config': {'Labels': {
            'com.docker.compose.project': 'addp-infra', 'com.docker.compose.service': 'postgres',
            'com.docker.compose.project.working_dir': str(Path(ONLINE.__file__).resolve().parents[2])},
            'Env': ['POSTGRES_DB=addp_online', 'POSTGRES_USER=addp', 'POSTGRES_PASSWORD=private-password']},
            'State': {'Running': True}, 'Mounts': [{'Destination': '/var/lib/postgresql/data',
                'Type': 'volume', 'Name': 'addp-infra_postgres_data', 'Source': '/var/lib/docker/volumes/addp-infra_postgres_data/_data'}]}
        settings = {'database': 'addp_online', 'pid': 44, 'read_only': 'on', 'track_io_timing': 'on', 'track_wal_io_timing': 'on'}
        with tempfile.TemporaryDirectory() as directory:
            env = {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_TEST': '1',
                'ADDP_ONLINE_HOSTED': '1', 'ADDP_ONLINE_HOST': '1', 'POSTGRES_DB': 'addp_online',
                'ADDP_ONLINE_ARTIFACT_DIR': directory, 'POSTGRES_USER': 'addp', 'POSTGRES_PASSWORD': 'private-password'}
            for fault in ('none', 'owner', 'working_directory', 'database', 'read_only', 'timing', 'init_query', 'final_query'):
                with self.subTest(fault=fault):
                    evidence, process = {}, Process()
                    current = json.loads(json.dumps(container))
                    if fault == 'owner':
                        current['Config']['Labels']['com.docker.compose.project'] = 'personal'
                    if fault == 'working_directory':
                        current['Config']['Labels']['com.docker.compose.project.working_dir'] = '/personal'
                    if fault == 'database':
                        current['Config']['Env'][0] = 'POSTGRES_DB=personal'
                    current_settings = dict(settings)
                    if fault == 'read_only':
                        current_settings['read_only'] = 'off'
                    if fault == 'timing':
                        current_settings['track_wal_io_timing'] = 'off'
                    replies = [current_settings, {'connections': 1, 'activity': []}, {'connections': 1, 'activity': []}, []]
                    if fault == 'init_query':
                        replies[0] = ONLINE.SuiteError('private-password')
                    if fault == 'final_query':
                        replies[-1] = ONLINE.SuiteError('private-password')
                    thread = Mock()
                    thread.is_alive.return_value = False
                    with patch.dict(ONLINE.os.environ, env), patch.object(ONLINE.sys, 'platform', 'linux'), \
                         patch.object(Path, 'exists', return_value=False), \
                         patch.object(Path, 'read_text', return_value='1 0 259:1 / / rw - ext4 /dev/nvme0n1p1 rw'), \
                         patch.object(ONLINE.subprocess, 'run', return_value=SimpleNamespace(stdout=json.dumps(current))), \
                         patch.object(ONLINE.subprocess, 'Popen', return_value=process) as popen, \
                         patch.object(ONLINE.AuditLatencyObservation, 'query', side_effect=replies) as query, \
                         patch.object(ONLINE, 'disk_observation', return_value={'disks': {}}), \
                         patch.object(ONLINE.threading, 'Thread', return_value=thread):
                        def run():
                            with ONLINE.AuditLatencyObservation(evidence, '33') as observer:
                                observer.healthy()
                        if fault == 'none':
                            run()
                            self.assertEqual(evidence['result'], 'passed')
                            self.assertEqual(evidence['samples'], 2)
                            self.assertTrue(evidence['connection_closed'])
                            self.assertEqual(evidence['volume_host_device'], '259:1')
                            self.assertEqual(process.input, b'\\q\n')
                            command = popen.call_args.args[0]
                            self.assertIn('a' * 64, command)
                            self.assertNotIn('private-password', ' '.join(command))
                            self.assertIn('default_transaction_read_only=on', popen.call_args.kwargs['env']['PGOPTIONS'])
                            self.assertIn('statement_timeout=2000', popen.call_args.kwargs['env']['PGOPTIONS'])
                            self.assertTrue(all(call.args[0].startswith('SELECT') for call in query.call_args_list))
                        else:
                            with self.assertRaises(ONLINE.SuiteError):
                                run()
                            self.assertEqual(evidence['result'], 'failed')
                        if fault not in ('owner', 'working_directory', 'database'):
                            self.assertEqual(process.returncode, 0)
                        else:
                            popen.assert_not_called()
                    Path(directory, 'pg-audit-waits.jsonl').unlink(missing_ok=True)
                    self.assertNotIn('private', json.dumps(evidence))

    def test_sample_count_byte_and_activity_caps_fail_closed(self):
        for fault in ('samples', 'bytes', 'connections', 'missing_activity'):
            with self.subTest(fault=fault):
                observer = ONLINE.AuditLatencyObservation({}, '33')
                observer.stream = io.StringIO()
                if fault == 'samples':
                    observer.samples = 2000
                if fault == 'bytes':
                    observer.bytes = 32 * 1024 * 1024
                result = {'connections': 258 if fault == 'connections' else 1,
                          'activity': None if fault == 'missing_activity' else []}
                with patch.object(observer, 'query', return_value=result), \
                     patch.object(ONLINE, 'disk_observation', return_value={}), self.assertRaises(ONLINE.SuiteError):
                    observer.sample()
                self.assertEqual(observer.stream.getvalue(), '')

    def test_close_failure_cannot_publish_a_pass(self):
        evidence = {}
        observer = ONLINE.AuditLatencyObservation(evidence, '33')
        observer.thread = Mock()
        observer.thread.is_alive.return_value = False
        with patch.object(observer, 'sample'), patch.object(observer, 'query', return_value=[]), \
             patch.object(observer, 'close', side_effect=ONLINE.SuiteError('cleanup failed')):
            with self.assertRaises(ONLINE.SuiteError):
                observer.__exit__(None, None, None)
        self.assertEqual(evidence['result'], 'failed')
        self.assertFalse(evidence['connection_closed'])


class PGAuthorizationOnlineTest(unittest.TestCase):
    def setUp(self):
        self.facts = {'item_fingerprint': 'a' * 64, 'boundary_hash': 'b' * 64,
                      'boundary': {'native_target': ONLINE.table_path(2, 'metric_people')}}
        self.scans = patch.object(ONLINE, 'basic_scan', side_effect=[
            'initial-basic', 'exact-basic', 'content-grant-basic', 'content-deny-basic',
            'content-revoked-basic', 'content-restored-basic']).start()
        self.addCleanup(patch.stopall)
        observation = patch.object(ONLINE, 'AuditLatencyObservation').start()
        def open_observation(evidence, principal):
            evidence.update(result='passed', connection_closed=True, audit_rows=[])
            return observation.return_value
        observation.side_effect = open_observation
        patch.object(ONLINE, 'bind_audit_requests').start()
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
        self.assertEqual((ONLINE.LOAD_ROUNDS, ONLINE.LOAD_SAMPLES, ONLINE.LOAD_WARMUPS), (3, 100, 10))
        self.assertEqual(ONLINE.latency_statistics(list(range(1, 101))),
                         {'sample_count': 100, 'p50_ms': 50, 'p95_ms': 95, 'min_ms': 1, 'max_ms': 100})
        for invalid in ([], [float('nan')], [float('inf')], [-1], [True]):
            with self.subTest(invalid=invalid), self.assertRaises(ONLINE.SuiteError):
                ONLINE.latency_statistics(invalid)

    def test_latency_sampling_is_read_only_and_excludes_warmups(self):
        server, lifecycle = self.lifecycle()
        command, _ = lifecycle.grant(self.facts)
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
        self.assertEqual(len(result['requests']), 1530)
        self.assertEqual(sum(row['warmup'] for row in result['requests']), 30)
        self.assertTrue(result['audit_observation']['connection_closed'])

    def test_observer_failure_stops_sampling_without_retry_or_rule_writes(self):
        server, lifecycle = self.lifecycle()
        command, _ = lifecycle.grant(self.facts)
        start = len(server.calls)
        with patch.object(ONLINE, 'AuditLatencyObservation') as context:
            context.return_value.__enter__.return_value.healthy.side_effect = ONLINE.SuiteError('observer failed')
            report = {}
            with self.assertRaises(ONLINE.SuiteError):
                lifecycle.measure_read_latency(report, self.facts, command)
            context.return_value.__exit__.assert_called_once()
        self.assertEqual(report['authorization_latency']['result'], 'failed')
        self.assertFalse(any(call[1] == 'POST' for call in server.calls[start:]))

    def test_latency_failure_stops_without_retry_and_preserves_partial_evidence(self):
        for fault in ('denied', 'transport', 'invalid_time', 'extra_data', 'credential', 'context', 'grant', 'boundary', 'budget'):
            with self.subTest(fault=fault):
                server, lifecycle = self.lifecycle()
                command, _ = lifecycle.grant(self.facts)
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
              ['PUT', '/data-items/test/direct-permission', true], ['POST', '/access_delegations', true],
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
        baseline = ONLINE.REGISTRATION.read_direct_permission(lifecycle.operator, 2, self.facts['item_fingerprint'])
        with patch.object(ONLINE, 'WorkspaceObservation') as observation:
            observed = observation.return_value.__enter__.return_value
            def finish():
                self.workspace_permission_transitions(lifecycle)
                lifecycle.revoke(lifecycle.grants[-1])
                return {'result': 'passed'}
            observed.finish.side_effect = finish
            lifecycle.workspace_flow(report, '35')
            self.assertEqual(observation.call_args.args, (3, '33', ONLINE.locator(2, 'metric_people', 10), 2, '35', lifecycle.grants[-1],
                baseline, self.facts))
        self.assertEqual(report['checks'], ONLINE.WORKSPACE_CHECKS)
        self.assertEqual(report['workspace_permission_cleanup'], 'allowed')
        self.assertEqual(server.permissions['a' * 64]['version'], '3')
        self.assertIsNotNone(server.grants[lifecycle.grants[-1]]['revocation'])
        lifecycle.cleanup()

    def workspace_permission_transitions(self, lifecycle):
        for allowed in (False, True):
            ONLINE.REGISTRATION.set_direct_permission(lifecycle.operator, lifecycle.path, self.facts, allowed)

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
                self.workspace_permission_transitions(lifecycle)
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
                    else:
                        observed.finish.side_effect = lambda: self.workspace_permission_transitions(lifecycle) or {'result': 'passed'}
                    with self.assertRaises(ONLINE.SuiteError):
                        lifecycle.workspace_flow({'checks': []}, '35')
                self.assertEqual(len(lifecycle.grants), 1)
                lifecycle.cleanup()
                self.assertTrue(all(row['revocation'] for row in server.grants.values()))

    def test_workspace_failure_after_close_restores_only_its_version_after_browser_exit(self):
        server, lifecycle = self.lifecycle()
        report = {'checks': []}
        closed = []
        with patch.object(ONLINE, 'WorkspaceObservation') as observation:
            observed = observation.return_value.__enter__.return_value
            observation.return_value.__exit__.side_effect = lambda *_: closed.append(True) and False
            def interrupted():
                ONLINE.REGISTRATION.set_direct_permission(lifecycle.operator, lifecycle.path, self.facts, False)
                raise ONLINE.SuiteError('browser failed after close')
            observed.finish.side_effect = interrupted
            restore = lifecycle.restore_workspace_permission
            def restore_after_exit(*args):
                self.assertEqual(closed, [True])
                restore(*args)
            with patch.object(lifecycle, 'restore_workspace_permission', side_effect=restore_after_exit), \
                    self.assertRaisesRegex(ONLINE.SuiteError, 'browser failed after close'):
                lifecycle.workspace_flow(report, '35')
        self.assertEqual(report['workspace_permission_cleanup'], 'allowed')
        self.assertEqual(report['checks'], [])
        self.assertEqual(server.permissions['a' * 64]['state'], 'allowed')
        self.assertEqual(server.permissions['a' * 64]['version'], '3')
        writes = [call[3] for call in server.calls if call[1] == 'PUT']
        self.assertEqual([(body['expected_version'], body['allowed']) for body in writes], [('1', False), ('2', True)])
        lifecycle.cleanup()
        self.assertTrue(all(row['revocation'] for row in server.grants.values()))

    def test_workspace_cleanup_refuses_unowned_state_and_does_not_rewrite_allowed(self):
        for state, version in [('denied', '1'), ('allowed', '2'), ('denied', '3'), ('allowed', '4'), ('allowed', '1'), ('allowed', '3')]:
            with self.subTest(state=state, version=version):
                server, lifecycle = self.lifecycle()
                baseline = ONLINE.REGISTRATION.read_direct_permission(lifecycle.operator, 2, self.facts['item_fingerprint'])
                server.permissions['a' * 64].update(state=state, version=version)
                if state == 'allowed' and version in {'1', '3'}:
                    lifecycle.restore_workspace_permission(baseline, self.facts)
                else:
                    with self.assertRaisesRegex(ONLINE.SuiteError, 'unowned permission'):
                        lifecycle.restore_workspace_permission(baseline, self.facts)
                self.assertFalse(any(call[1] == 'PUT' for call in server.calls))

    def test_workspace_permission_cleanup_failure_does_not_claim_success_or_lose_grant_cleanup(self):
        server, lifecycle = self.lifecycle()
        report = {'checks': []}
        with patch.object(ONLINE, 'WorkspaceObservation') as observation, \
                patch.object(lifecycle, 'restore_workspace_permission', side_effect=ONLINE.SuiteError('restore failed')):
            observation.return_value.__enter__.return_value.finish.side_effect = ONLINE.SuiteError('browser failed')
            with self.assertRaisesRegex(ONLINE.SuiteError, 'restore failed'):
                lifecycle.workspace_flow(report, '35')
        self.assertEqual(report['workspace_permission_cleanup'], 'failed')
        self.assertEqual(report['checks'], [])
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
        self.permission = {'engine_id': '2', 'item_fingerprint': self.facts['item_fingerprint'],
            'state': 'unknown', 'source': 'deployment_unknown', 'version': '0'}
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
            permit = path.endswith('/direct-permission')
            update = permit and method == 'PUT'
            fault = self.server.fault
            valid_direct = direct and (body['request_id'] in self.server.grants or (
                self.permission['state'] == 'allowed' and (body['direct_permission_version'] == self.permission['version']
                                                            or fault == 'stale_direct_accepted')))
            if (valid_direct or ('/catalog/' not in path and not update
                    and not permit
                    and not direct)):
                return original(method, url, expected, body, **kwargs)
            self.server.calls.append((kind, method, path, body))
            status, result = 200, {}
            if update:
                if kind != 'operator' and fault != 'missing_update_permission_ignored':
                    result, status = {'error': 'update forbidden'}, 403
                elif body['expected_version'] != self.permission['version'] and fault != 'stale_update_accepted':
                    result, status = {'error': 'stale update', 'error_code': 'resource_version_conflict'}, 409
                else:
                    state = 'allowed' if body['allowed'] else 'denied'
                    if self.permission['source'] != 'explicit' or state != self.permission['state'] or fault == 'same_value_changes_version':
                        self.permission.update(state=state, source='explicit', version=str(int(self.permission['version']) + 1))
                    result = dict(self.permission)
                    if fault == 'same_value_changes_source':
                        result['source'] = 'deployment_default'
                    if fault == 'switch_rewrites_old_grant':
                        for row in self.server.grants.values():
                            row['direct_permission_version'] = self.permission['version']
            elif direct:
                status, result = 409, {'error': 'wrong approval mode', 'error_code': 'engine_access_grant_approval_changed'}
                if fault == 'direct_fallback':
                    result, status = dict(body, revocation=None), 201
                    self.server.grants[body['request_id']] = result
                elif fault == 'duplicate_masks_approval':
                    result['error_code'] = 'engine_access_grant_relation_exists'
            elif permit:
                result = dict(self.permission)
                if self.server.catalog_down and fault == 'implicit_mode_change':
                    result['state'] = 'allowed'
            elif '/catalog/' in path and self.server.catalog_down:
                status, result = 503, {'error': 'module unavailable'}
                if fault == 'offline_fulfillment_issued':
                    self.issue(body['request_id'])
                    status, result = 200, {'state': 'accepted'}
                elif fault == 'outage_wrong_status':
                    status = 404
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
                if self.permission['state'] == 'allowed' and fault == 'direct_configuration_blocks_catalog':
                    return SimpleNamespace(status=202, payload={'request_id': body['request_id'], 'state': 'closed'})
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
                              'decision_id': grant['business_decision_id'],
                              'recipient_id': grant['recipient_id'], 'action': 'read'}
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
                 'business_decision_id': decision['decision_id'],
                 'engine_id': '2', 'operator_principal_id': '35', 'direct_permission_source': '', 'direct_permission_version': '0',
                 'catalog_path': self.path, 'item_fingerprint': self.facts['item_fingerprint'],
                 'boundary_hash': self.facts['boundary_hash'], 'recipient_type': 'user', 'recipient_id': decision['recipient_id'],
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
        self.assertEqual([call[0] for call in decisions], ['business', 'business'])
        self.assertEqual([call[0] for call in requests], ['operator'] * 6)
        self.assertEqual(requests[0][3], requests[-1][3])
        self.assertNotEqual(requests[0][3]['request_id'], requests[2][3]['request_id'])
        self.assertEqual(set(requests[0][3]), {'request_id', 'decision_id'})
        self.assertFalse(any('/runtime/' in call[2] for call in self.server.calls))
        direct = [call for call in self.server.calls if call[2].endswith('/access_grants') and call[1] == 'POST']
        self.assertEqual(len(direct), 6)
        self.assertTrue(all(call[3]['recipient_id'] == '34' and call[3]['direct_permission_source'] == 'explicit' for call in direct))
        self.assertEqual(self.report['direct_permission_changes']['versions'], [1, 2, 3])
        self.assertIn('catalog_acceptance_independent_of_direct_configuration', self.report['checks'])
        self.assertEqual(self.permission['version'], '3')
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
                with self.assertRaises((ONLINE.SuiteError, ONLINE.REGISTRATION.RegistrationError)):
                    self.run_flow()
                self.assertNotEqual(len(self.report['checks']), 21)

    def test_approval_switch_rejects_permission_version_grant_mutation_and_silent_catalog_upgrade(self):
        for fault in ('missing_update_permission_ignored', 'switch_rewrites_old_grant', 'stale_direct_accepted',
                      'same_value_changes_version', 'same_value_changes_source',
                      'stale_update_accepted', 'direct_replay_renews', 'direct_replay_restores_revoked',
                      'direct_configuration_blocks_catalog'):
            with self.subTest(fault=fault):
                self.reset()
                self.server.fault = fault
                with self.assertRaises((ONLINE.SuiteError, ONLINE.REGISTRATION.RegistrationError)):
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
                with self.assertRaises((ONLINE.SuiteError, ONLINE.REGISTRATION.RegistrationError)):
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
            'ADDP_ONLINE_PG_SOURCE_PREFIX': 'pg_auth_0123456789abcdef',
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
                self.assertEqual(call.kwargs['env']['PGAPPNAME'], 'pg_auth_0123456789abcdef_' + self.module)
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

    def test_collection_browser_exact_report_and_private_operator_login(self):
        environment = dict(self.environment, ADDP_ONLINE_PG_OPERATOR_USERNAME='external-online-pg-authorizer',
                           ADDP_ONLINE_PG_OPERATOR_PASSWORD='operator-private-password')
        with patch.dict(ONLINE.os.environ, environment), patch.object(ONLINE.subprocess, 'Popen', return_value=self.process) as popen, \
             patch.object(ONLINE.os, 'killpg'):
            with ONLINE.CollectionObservation(2, '35', 1, 42, 'a' * 64, 'governance', '2099-01-01T00:00:00Z') as browser:
                browser.command_id = str(uuid.uuid4())
                expected = dict(browser.binding, schema_version='addp.pg-collection-browser/v1', result='passed',
                    command_id=browser.command_id, checks=ONLINE.COLLECTION_BROWSER_CHECKS[:-1],
                    login_requests=1, approval_requests=2, revocation_requests=1,
                    dropped_approval_responses=1, navigation_writes=0, unchanged_identity=True)
                browser.evidence.write_text(json.dumps(expected))
                self.assertEqual(browser.finish(), expected)
                env = popen.call_args.kwargs['env']
                self.assertFalse(any('TOKEN' in key or 'SECRET' in key for key in env))
                self.assertNotIn('ADDP_ONLINE_TEST_USER_PASSWORD', env)
                self.assertEqual(env['ADDP_ONLINE_PG_OPERATOR_PASSWORD'], 'operator-private-password')
                for changes in ({'command_id': str(uuid.uuid4())}, {'checks': []}, {'approval_requests': 3},
                                {'revocation_requests': 0}, {'dropped_approval_responses': 0},
                                {'navigation_writes': 1}, {'unchanged_identity': False}, {'login_requests': 2},
                                {'task_id': '43'}, {'raw_token': 'untrusted'}):
                    browser.evidence.write_text(json.dumps(dict(expected, **changes)))
                    with self.subTest(changes=changes), self.assertRaises(ONLINE.SuiteError):
                        browser.finish()
                self.process.poll.return_value = 0

    def test_collection_command_must_match_exact_pre_registered_scope_and_expiry(self):
        with patch.dict(ONLINE.os.environ, self.environment):
            browser = ONLINE.CollectionObservation(2, '35', 1, 42, 'a' * 64, 'governance', '2099-01-01T00:00:00Z')
            body = {'command_id': str(uuid.uuid4()), 'task_id': '42', 'expected_task_definition_digest': 'a' * 64,
                    'purpose': 'governance', 'expires_at': '2099-01-01T08:00:00+08:00'}
            browser.read_ready = Mock(return_value=dict(browser.marker('command'), command=body))
            self.assertEqual(browser.capture_command(), body)
            for changes in ({'task_id': '43'}, {'purpose': 'different'}, {'expected_task_definition_digest': 'b' * 64},
                            {'expires_at': '2099-01-01T00:00:00.000000001Z'}, {'command_id': 'unknown'},
                            {'engine_id': 'foreign'}):
                browser.command_id = None
                browser.read_ready.return_value = dict(browser.marker('command'), command=dict(body, **changes))
                with self.subTest(changes=changes), self.assertRaises(ONLINE.SuiteError):
                    browser.capture_command()
                self.assertIsNone(browser.command_id)
            browser.read_ready.return_value = dict(browser.marker('command'), command=body, raw_token='forbidden')
            with self.assertRaisesRegex(ONLINE.SuiteError, 'binding'):
                browser.capture_command()

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
            permission = {'engine_id': '1', 'item_fingerprint': 'a' * 64, 'state': 'allowed', 'source': 'explicit', 'version': '3'}
            facts = {'boundary': {'native_target': ONLINE.table_path(1, 'metric_people')}, 'boundary_hash': 'b' * 64}
            with REAL_WORKSPACE_OBSERVATION(2, '33', 'target', 1, '35', 'owned-grant', permission, facts) as browser:
                expected = dict(browser.binding, schema_version='addp.pg-authorization-workspace/v1', result='passed',
                                checks=ONLINE.WORKSPACE_CHECKS, login_requests=3, preview_statuses=[200, 403],
                                direct_permission_edit={
                                    'item_fingerprint': 'a' * 64, 'boundary_hash': 'b' * 64, 'versions': ['3', '4', '5'],
                                    'writes': [{'expected_version': '3', 'allowed': False, 'status': 200},
                                               {'expected_version': '3', 'allowed': False, 'status': 409},
                                               {'expected_version': '4', 'allowed': True, 'status': 200}],
                                    'conflict_code': 'resource_version_conflict', 'cancelled_writes': 0, 'review_writes': 0,
                                    'grant_unchanged': True, 'closed_preview_status': 200, 'same_reader_credential': True,
                                    'final_state': 'allowed'},
                                direct_permission_observations=[{'search': 'metric_people', 'state': state, 'total': total,
                                    'item_fingerprints': ['a' * 64] if total else []}
                                    for state, total in [('', 1), ('allowed', 1), ('denied', 0)]])
                browser.evidence.write_text(json.dumps(expected))
                self.assertEqual(browser.finish(), expected)
                env = popen.call_args.kwargs['env']
                self.assertFalse(any('TOKEN' in key or 'SECRET' in key for key in env))
                self.assertEqual(env['ADDP_ONLINE_PG_OPERATOR_PASSWORD'], 'operator-private-password')
                self.assertIn(browser.test_name, popen.call_args.args[0])
                for fault in ('missing_check', 'missing_permission_check', 'wrong_permission_target', 'denied_filter_leaks_target',
                              'wrong_grant', 'relogin', 'still_readable', 'extra_field', 'missing_edit', 'wrong_conflict',
                              'auto_retry', 'wrong_version', 'wrong_boundary', 'grant_changed', 'closed_read_denied',
                              'reader_replaced', 'cancel_writes', 'review_writes', 'final_denied'):
                    bad = deepcopy(expected)
                    if fault == 'missing_check': bad['checks'] = bad['checks'][:-1]
                    if fault == 'missing_permission_check': bad['checks'].remove('direct_permission_tab_read_only_filters')
                    if fault == 'wrong_permission_target': bad['direct_permission_observations'][1]['item_fingerprints'] = ['b' * 64]
                    if fault == 'denied_filter_leaks_target': bad['direct_permission_observations'][2].update(total=1, item_fingerprints=['a' * 64])
                    if fault == 'wrong_grant': bad['grant_id'] = 'unowned'
                    if fault == 'relogin': bad['login_requests'] = 4
                    if fault == 'still_readable': bad['preview_statuses'] = [200, 200]
                    if fault == 'extra_field': bad['raw_token'] = 'must-not-accept'
                    if fault == 'missing_edit': bad.pop('direct_permission_edit')
                    if fault == 'wrong_conflict': bad['direct_permission_edit']['conflict_code'] = 'other_conflict'
                    if fault == 'auto_retry': bad['direct_permission_edit']['writes'].append(bad['direct_permission_edit']['writes'][-1])
                    if fault == 'wrong_version': bad['direct_permission_edit']['versions'] = ['3', '4', '4']
                    if fault == 'wrong_boundary': bad['direct_permission_edit']['boundary_hash'] = 'c' * 64
                    if fault == 'grant_changed': bad['direct_permission_edit']['grant_unchanged'] = False
                    if fault == 'closed_read_denied': bad['direct_permission_edit']['closed_preview_status'] = 403
                    if fault == 'reader_replaced': bad['direct_permission_edit']['same_reader_credential'] = False
                    if fault == 'cancel_writes': bad['direct_permission_edit']['cancelled_writes'] = 1
                    if fault == 'review_writes': bad['direct_permission_edit']['review_writes'] = 1
                    if fault == 'final_denied': bad['direct_permission_edit']['final_state'] = 'denied'
                    browser.evidence.write_text(json.dumps(bad))
                    with self.subTest(fault=fault), self.assertRaises(ONLINE.SuiteError):
                        browser.finish()


class PGReadLoadTest(unittest.TestCase):
    def setUp(self):
        observation = patch.object(ONLINE, 'AuditLatencyObservation').start()
        def observe(evidence, principal, *, phase):
            self.assertEqual(phase, 'load')
            evidence.update(result='passed', connection_closed=True, audit_rows=[])
            return observation.return_value
        observation.side_effect = observe
        self.audit_windows = patch.object(ONLINE, 'verify_load_audit_windows').start()
        self.server = Server()
        self.reader = self.server.client('reader')
        self.lifecycle = ONLINE.Lifecycle(self.server.client('operator'), self.reader, 3, 2, '33', {},
            self.server.client('outsider'), Mock(), Mock(), 'external-online-consumer', 'private-password')
        self.paths = [ONLINE.table_path(2, name) for name in ('metric_people', 'pg_initial_disposable')]
        self.bindings = [(path, {'item_fingerprint': letter * 64, 'boundary_hash': 'b' * 64}, str(index))
                         for index, (path, letter) in enumerate(zip(self.paths, ('a', 'c')))]
        for path, facts, command in self.bindings:
            self.server.grants[command] = dict(request_id=command, catalog_path=path, **facts,
                recipient_type='user', recipient_id='33', action='read', revocation=None,
                expires_at='2099-01-01T00:00:00Z')
        def facts(client, path):
            return next(dict(value) for target, value, _ in self.bindings if target == path)
        patch.object(ONLINE.REGISTRATION, 'read_exact_record_authorization_facts', side_effect=facts).start()
        for key, value in [('LOAD_ROUNDS', 1), ('LOAD_SAMPLES', 20), ('LOAD_WARMUPS', 2)]:
            patch.object(ONLINE, key, value).start()
        self.addCleanup(patch.stopall)
        self.reads, self.active = [], 0
        self.lock = ONLINE.threading.Lock()
        self.fault = None
        for client in (self.reader, self.lifecycle.outsider):
            original = client.request
            def request(method, path, expected, body=None, *, client=client, original=original):
                if path != READ_CHECK:
                    return original(method, path, expected, body)
                self.assertEqual(method, 'POST')
                self.assertTrue(all(target in self.paths for target in body['targets']))
                with self.lock:
                    self.active += 1
                    self.reads.append((client is self.reader, body))
                try:
                    # Permit true thread overlap without globally patching the worker clock.
                    ONLINE.threading.Event().wait(.002)
                    allowed = client is self.reader and all(any(row['catalog_path'] == target
                        and row['revocation'] is None for row in self.server.grants.values()) for target in body['targets'])
                    payload = {'observed_at': '2026-10-22T00:00:00Z'} if allowed else {'error': 'denied'}
                    status = 200 if allowed else 403
                    if client is self.reader and self.fault:
                        if self.fault == 'transport':
                            raise ONLINE.SuiteError('private secret diagnostics')
                        if self.fault == 'wrong_status':
                            status = 403
                        if self.fault == 'time':
                            payload = {'observed_at': '2026-10-22T00:00:00'}
                        if self.fault == 'shape':
                            payload['data'] = []
                        if self.fault == 'credential':
                            self.reader.token = 'changed-private-token'
                        if self.fault == 'context':
                            self.server.version += 1
                        if self.fault == 'grant':
                            self.server.grants['0']['expires_at'] = '2098-01-01T00:00:00Z'
                        if self.fault == 'boundary':
                            self.bindings[0][1]['boundary_hash'] = 'd' * 64
                    return SimpleNamespace(status=status, payload=payload)
                finally:
                    with self.lock:
                        self.active -= 1
            client.request = request

    def test_matrix_uses_real_distinct_targets_and_counts_operations_not_http_calls(self):
        report = {}
        before = json.dumps(self.server.grants, sort_keys=True)
        self.lifecycle.measure_read_load(report, self.bindings)
        evidence = report['authorization_load']
        self.assertEqual(evidence['result'], 'passed')
        self.assertTrue(evidence['audit_observer_enabled'])
        self.assertTrue(evidence['audit_observation']['connection_closed'])
        self.assertEqual([(cell['mode'], cell['concurrency_limit']) for cell in evidence['cells']],
            [('single_target', 1), ('single_target', 5), ('single_target', 10),
             ('two_target_batch', 1), ('two_target_batch', 5), ('two_target_batch', 10), ('two_target_separate', 1)])
        for cell in evidence['cells']:
            self.assertEqual(cell['sample_count'], 20)
            row = cell['rounds'][0]
            self.assertEqual(len(row['requests']), 22)
            self.assertEqual(sum(sample['warmup'] for sample in row['requests']), 2)
            self.assertEqual(row['http_attempted_count'], 22 * cell['http_requests_per_operation'])
            self.assertGreater(row['observed_operations_per_second'], 0)
            if cell['concurrency_limit'] > 1:
                self.assertGreater(row['peak_in_flight'], 1)
        self.assertEqual(self.active, 0)
        self.assertEqual(json.dumps(self.server.grants, sort_keys=True), before)
        self.assertTrue(all(call[1] == 'GET' for call in self.server.calls))
        self.assertNotIn('private', json.dumps(evidence))
        self.server.grants['0']['revocation'] = {'reason': 'test'}
        self.lifecycle.require_batch_denied(self.reader, self.paths)
        self.assertIsNone(self.server.grants['1']['revocation'])

    def test_failure_stops_without_retry_drains_threads_and_preserves_failure(self):
        for fault in ('transport', 'wrong_status', 'time', 'shape', 'credential', 'context', 'grant', 'boundary'):
            with self.subTest(fault=fault):
                self.fault = fault
                report = {}
                with self.assertRaises(ONLINE.SuiteError):
                    self.lifecycle.measure_read_load(report, self.bindings)
                self.assertEqual(report['authorization_load']['result'], 'failed')
                self.assertEqual(self.active, 0)
                self.assertNotIn('private', json.dumps(report))
                if fault in ('transport', 'wrong_status', 'time', 'shape', 'credential'):
                    self.assertLessEqual(len(report['authorization_load']['cells'][0]['rounds'][0]['requests']), 1)
                self.reader.token = 'reader-private-token'
                self.server.version = 1
                self.server.grants['0']['expires_at'] = '2099-01-01T00:00:00Z'
                self.bindings[0][1]['boundary_hash'] = 'b' * 64

    def test_duplicate_item_or_control_mismatch_stops_before_sampling(self):
        for bindings in ([self.bindings[0], self.bindings[0]],
                         [self.bindings[0], (self.paths[1], self.bindings[0][1], '1')],
                         [(*self.bindings[0][:2], 'wrong'), self.bindings[1]]):
            with self.subTest(bindings=bindings), self.assertRaises(ONLINE.SuiteError):
                self.lifecycle.measure_read_load({}, bindings)
        self.assertEqual(self.reads, [])

    def test_observer_failure_never_counts_as_a_valid_load_sample(self):
        with patch.object(ONLINE, 'AuditLatencyObservation') as context:
            context.return_value.__enter__.return_value.healthy.side_effect = ONLINE.SuiteError('observer failed')
            report = {}
            with self.assertRaises(ONLINE.SuiteError):
                self.lifecycle.measure_read_load(report, self.bindings)
            context.return_value.__exit__.assert_called_once()
        self.assertEqual(report['authorization_load']['result'], 'failed')
        self.assertEqual(self.reads, [])

    def test_observer_initialization_failure_retains_failure_evidence(self):
        report = {}
        with patch.object(ONLINE, 'AuditLatencyObservation') as context:
            context.return_value.__enter__.side_effect = ONLINE.SuiteError('initialization failed')
            with self.assertRaises(ONLINE.SuiteError):
                self.lifecycle.measure_read_load(report, self.bindings)
        self.assertEqual(report['authorization_load']['result'], 'failed')
        self.assertIn('audit_observation', report['authorization_load'])
        self.assertEqual(self.reads, [])

    def test_concurrent_failure_drains_all_requests_without_later_cells(self):
        original = self.reader.request
        def fail_in_second_cell(method, path, expected, body=None):
            # First cell completes 22 operations; fail only when actual load is concurrent.
            if path == READ_CHECK and sum(reader for reader, _ in self.reads) >= 22:
                self.fault = 'transport'
            return original(method, path, expected, body)
        self.reader.request = fail_in_second_cell
        report = {}
        with self.assertRaises(ONLINE.SuiteError):
            self.lifecycle.measure_read_load(report, self.bindings)
        cells = report['authorization_load']['cells']
        self.assertEqual([cell['concurrency_limit'] for cell in cells], [1, 5])
        self.assertEqual(cells[0]['result'], 'passed')
        self.assertGreater(cells[1]['failed_count'], 0)
        self.assertLessEqual(cells[1]['rounds'][0]['http_attempted_count'], 2)
        self.assertEqual(self.active, 0)

    def test_budget_and_denial_shape_fail_closed(self):
        clock = [0]
        original = self.reader.request
        def expired(method, path, expected, body=None):
            response = original(method, path, expected, body)
            if path == READ_CHECK:
                clock[0] = 100
            return response
        self.reader.request = expired
        with patch.object(ONLINE.time, 'monotonic', side_effect=lambda: clock[0]), self.assertRaises(ONLINE.SuiteError):
            self.lifecycle.measure_read_load({}, self.bindings)
        self.assertEqual(self.active, 0)
        for payload, status in (({'error': 'denied', 'data': []}, 403),
                                ({'error': 'denied', 'error_code': 'permission_denied'}, 403),
                                ({'error': 'denied'}, 200)):
            with self.subTest(payload=payload, status=status), patch.object(self.lifecycle.outsider, 'request',
                return_value=SimpleNamespace(status=status, payload=payload)), self.assertRaises(ONLINE.SuiteError):
                self.lifecycle.require_batch_denied(self.lifecycle.outsider, self.paths)


class PGLoadAuditWindowTest(unittest.TestCase):
    def fixture(self):
        start = datetime(2026, 10, 22, tzinfo=timezone.utc).timestamp() * 1000000000
        result = {'cells': [{'rounds': [{'http_attempted_count': count,
            'requests': [{'started_unix_ns': start + offset, 'finished_unix_ns': start + offset + 10000000}]}]}
            for count, offset in ((2, 0), (1, 20000000))]}
        audits = [{'id': index, 'request_id': str(ONLINE.uuid.uuid4()), 'created_at':
            datetime.fromtimestamp((start + offset) / 1000000000, timezone.utc).isoformat()}
            for index, offset in enumerate((1000000, 2000000, 21000000))]
        return result, audits

    def test_commit_order_is_not_individual_request_order(self):
        result, audits = self.fixture()
        ONLINE.verify_load_audit_windows(result, list(reversed(audits)))
        self.assertEqual(result['audit_correlation'], 'cell_time_windows_only_not_individual_requests')
        self.assertNotIn('request_id', result['cells'][0]['rounds'][0]['requests'][0])

    def test_missing_duplicate_or_outside_audits_fail_closed(self):
        for fault in ('missing', 'duplicate_request', 'duplicate_id', 'outside', 'timezone'):
            with self.subTest(fault=fault):
                result, audits = self.fixture()
                if fault == 'missing':
                    audits.pop()
                elif fault == 'duplicate_request':
                    audits[1]['request_id'] = audits[0]['request_id']
                elif fault == 'duplicate_id':
                    audits[1]['id'] = audits[0]['id']
                elif fault == 'outside':
                    audits[0]['created_at'] = '2026-10-22T00:01:00+00:00'
                else:
                    audits[0]['created_at'] = '2026-10-22T00:00:00'
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.verify_load_audit_windows(result, audits)

    def test_observation_phase_keeps_separate_file_and_bounded_query(self):
        load = ONLINE.AuditLatencyObservation({}, '33', phase='load')
        serial = ONLINE.AuditLatencyObservation({}, '33')
        self.assertEqual((load.file, load.audit_limit), ('pg-load-waits.jsonl', 2641))
        self.assertEqual((serial.file, serial.audit_limit), ('pg-audit-waits.jsonl', 1531))
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.AuditLatencyObservation({}, '33', phase='arbitrary')


class MetaCollectionOnlineTest(unittest.TestCase):
    def setUp(self):
        self.tasks, self.qualifications, self.runs, self.calls = {}, {}, {}, []
        self.fault, self.lost = None, None
        self.expiry_clock = None
        self.schedule_dispatch = False
        self.reader, self.operator = Mock(), Mock()
        self.life = ONLINE.Lifecycle(self.operator, self.reader, 3, 2, '33', {}, Mock(), Mock(), Mock(),
                                     'external-online-consumer', 'private-password')
        self.life.listing = Mock(return_value={'total': 1, 'data': [{'request_id': 'persistent-history',
                                                                   'revocation': {'reason': 'prior suite'}}]})
        self.scenario = self.life.collection = ONLINE.MetaCollectionScenario(self.life, '35', 'fixture')
        self.reader.request.side_effect = lambda *a, **kw: self.request('reader', *a, **kw)
        self.operator.request.side_effect = lambda *a, **kw: self.request('operator', *a, **kw)
        self.report = {'checks': []}
        patch.object(ONLINE.time, 'sleep').start()
        self.addCleanup(patch.stopall)

    def facts(self, task):
        return {'schema_version': 'meta.scan_task_facts/v1', 'task_id': str(task), 'engine_id': '2',
                'task_definition_digest': ('b' if self.tasks[task]['parameters']['force'] else 'a') * 64,
                'scope_kind': 'engine', 'scan_depth': 'basic', 'read_options': {},
                'force': self.tasks[task]['parameters']['force']}

    def new_run(self, task, *, scheduled=False):
        current = self.tasks[task]
        approvals = [row for row in self.qualifications.values() if row['task_id'] == str(task)]
        state = 'unapproved' if not approvals else 'revoked' if approvals[-1]['status'] == 'revoked' else (
            'changed' if current['parameters']['force'] else 'allowed')
        if state == 'allowed' and self.expiry_clock is not None and datetime.fromisoformat(
                approvals[-1]['expires_at'].replace('Z', '+00:00')) <= self.expiry_clock.now(timezone.utc):
            state = 'expired'
        if current['name'].endswith('_copy'):
            state = 'copied'
        allowed = state == 'allowed' or self.fault == state + '_allowed'
        execution = str(uuid.uuid4())
        self.runs[execution] = {'execution_id': execution, 'module': 'meta', 'task_type': 'scan',
            'source_task_id': str(task), 'tenant_id': 3, 'triggered_by': 33,
            'scan_context': {'engine_id': 2, 'scan_depth': 'basic'},
            'status': 'success' if allowed else 'failed', 'completed_at': '2026-10-23T00:00:00Z',
            'metadata': {'items_scanned': 3, 'catalog_nodes_scanned': 2} if allowed else {},
            'error_details': {} if allowed else {'category': 'connection_failed' if self.fault == 'general_failure' else 'permission_denied'}}
        if self.expiry_clock is not None:
            self.runs[execution].update(started_at=self.expiry_clock.now(timezone.utc).isoformat(),
                                       completed_at=self.expiry_clock.now(timezone.utc).isoformat())
        if self.fault == 'wrong_task':
            self.runs[execution]['source_task_id'] = '999'
        if self.fault == 'read_before_deny' and not allowed:
            self.runs[execution]['metadata'] = {'items_scanned': 1}
        if self.fault != 'stale_summary':
            current.update(last_execution_id=execution, last_execution_status=self.runs[execution]['status'])
        if state == 'allowed' and self.fault == 'repeat_renews':
            approvals[-1]['expires_at'] = '2035-01-01T00:00:00Z'
        if scheduled:
            self.runs[execution].update(triggered_by=None, trigger_type='scheduled', source='meta', execution_boundary='bounded',
                                        created_at=self.expiry_clock.now(timezone.utc).isoformat())
            if self.fault == 'created_after_start': self.runs[execution]['created_at'] = (self.expiry_clock.now(timezone.utc) + timedelta(microseconds=1)).isoformat()
            if self.fault == 'start_after_completion': self.runs[execution]['started_at'] = (self.expiry_clock.now(timezone.utc) + timedelta(microseconds=1)).isoformat()
            if self.fault == 'missing_start': self.runs[execution].pop('started_at')
            if self.fault == 'invalid_start': self.runs[execution]['started_at'] = 'token=private-secret https://private.invalid'
            if self.fault == 'invalid_completion': self.runs[execution]['completed_at'] = {'token': 'private-secret'}
            if self.fault == 'manual_run': self.runs[execution]['trigger_type'] = 'manual'
            if self.fault == 'human_actor': self.runs[execution]['triggered_by'] = 35
            if self.fault == 'early_run': self.runs[execution]['created_at'] = '2029-01-01T00:00:00Z'
            if self.fault == 'wrong_source': self.runs[execution]['source'] = 'console'
            if self.fault == 'wrong_boundary': self.runs[execution]['execution_boundary'] = 'continuous'
            if self.fault == 'parent_run': self.runs[execution]['parent_execution_id'] = str(uuid.uuid4())
            if self.fault == 'retry_run': self.runs[execution]['retry_of_execution_id'] = str(uuid.uuid4())
        return self.runs[execution]

    def request(self, actor, method, url, expected, body=None, **kwargs):
        path, query = urlsplit(url).path, parse_qs(urlsplit(url).query)
        self.calls.append((actor, method, path, deepcopy(body)))
        status, result, operation = 200, {}, None
        if path == '/api/v1/meta/scan/tasks':
            if method == 'GET':
                result = list(self.tasks.values())
                if self.fault == 'foreign_task':
                    for row in result:
                        row['created_by'] = 999
            else:
                self.assertEqual(actor, 'operator')
                task = 100 + len(self.tasks)
                self.tasks[task] = dict(body, id=task, tenant_id=3, created_by=35,
                                        parameters={'force': body['force']})
                if self.expiry_clock is not None:
                    self.tasks[task]['updated_at'] = self.expiry_clock.now(timezone.utc).isoformat()
                status, result, operation = 201, self.tasks[task], 'create'
        elif path.startswith('/api/v1/meta/scan/tasks/'):
            task = int(path.split('/')[6])
            if path.endswith('/trigger'):
                self.assertEqual(actor, 'reader')
                result, operation = self.new_run(task), 'trigger'
            elif method == 'PUT':
                if self.fault != 'disable_noop' or body['enabled'] is not False:
                    self.tasks[task].update(body, parameters={'force': body['force']})
                if self.expiry_clock is not None:
                    self.tasks[task].update(updated_at=self.expiry_clock.now(timezone.utc).isoformat(),
                        next_run_at=(self.expiry_clock.now(timezone.utc) + timedelta(seconds=60)).isoformat())
                result, operation = self.tasks[task], 'enable' if body['enabled'] else 'disable'
            elif method == 'DELETE':
                self.tasks.pop(task)
                operation = 'delete'
        elif '/meta_collection_task_facts/' in path:
            result = self.facts(int(path.split('/')[-1]))
        elif '/meta_collection_qualifications' in path:
            if method == 'GET':
                result = {'data': list(self.qualifications.values()), 'total': len(self.qualifications),
                          'page': int(query['page'][0]), 'page_size': 100}
            elif method == 'POST':
                if actor == 'reader':
                    status, result = 403, {'error': 'permission denied', 'error_code': 'permission_denied'}
                else:
                    command = body['command_id']
                    if command not in self.qualifications:
                        self.qualifications[command] = dict(id=command, engine_id='2', task_id=body['task_id'],
                            task_definition_digest=body['expected_task_definition_digest'], engine_version='1',
                            purpose=body['purpose'], scan_depth='basic', scope_kind='engine', status='active', version='1',
                            granted_at='2026-10-23T00:00:00Z', expires_at=body['expires_at'], granted_by_principal_id='35')
                    elif self.fault == 'replay_revives':
                        self.qualifications[command]['status'] = 'active'
                    elif self.fault == 'expired_replay_renews':
                        self.qualifications[command]['expires_at'] = '2099-01-01T00:00:00Z'
                    status, result, operation = 201, self.qualifications[command], 'approve'
                    if self.fault == 'different_timestamp_zone':
                        # The POST's UTC time and a database read's local time
                        # can encode exactly the same instant differently.
                        result = deepcopy(result)
                        for key in ('granted_at', 'expires_at', 'revoked_at'):
                            if key in result:
                                self.qualifications[command][key] = datetime.fromisoformat(result[key].replace('Z', '+00:00')).astimezone(timezone(timedelta(hours=8))).isoformat()
            else:
                row = self.qualifications[path.split('/')[-1]]
                self.assertEqual(body['version'], row['version'])
                if self.fault != 'revoke_noop':
                    row.update(status='revoked', version=str(int(row['version']) + 1), revoked_at='2026-10-23T00:01:00Z',
                               revoked_by_principal_id='35', revoked_reason=body['reason'])
                    if self.expiry_clock is not None:
                        row['revoked_at'] = self.expiry_clock.now(timezone.utc).isoformat()
                result, operation = row, 'revoke'
        elif path.startswith('/api/v1/meta/executions/'):
            result = self.runs[path.split('/')[-1]]
        elif path == '/api/v1/meta/scan/runs':
            task = int(query['task_id'][0])
            current = self.tasks.get(task)
            if self.schedule_dispatch and current and current.get('enabled') and self.fault != 'scheduler_stalled' \
                    and self.expiry_clock.now(timezone.utc) >= datetime.fromisoformat(current['next_run_at']):
                self.new_run(task, scheduled=True)
                if self.fault == 'duplicate_scheduled': self.new_run(task, scheduled=True)
                current['next_run_at'] = (self.expiry_clock.now(timezone.utc) + timedelta(seconds=60)).isoformat()
            rows = [row for row in self.runs.values() if row['source_task_id'] == query['task_id'][0]]
            result = {'items': rows, 'total': len(rows)}
            if self.fault == 'truncated_scheduled': result['total'] += 1
        else:
            self.fail('unexpected collection API ' + url)
        self.assertIn(status, expected)
        if operation and self.lost == operation:
            self.lost = None
            raise ONLINE.SuiteError('unknown ' + operation + ' response')
        return SimpleNamespace(status=status, payload=deepcopy(result))

    def test_qualification_lifecycle_uses_one_approval_and_current_worker_results(self):
        self.scenario.run(self.report)
        self.assertEqual(self.report['checks'], ONLINE.COLLECTION_CHECKS)
        self.assertEqual(len(self.qualifications), 1)
        self.assertEqual(len(self.runs), 6)
        self.assertTrue(self.report['meta_collection']['source_rules_unchanged'])
        self.scenario.cleanup()
        self.assertFalse(self.tasks)
        self.assertTrue(all(row['status'] == 'revoked' for row in self.qualifications.values()))
        self.assertFalse(any('/access_grants' in path or '/access_denies' in path for _, _, path, _ in self.calls))

    def collection_expiry_fixture(self, failure=None):
        self.report['meta_collection'] = {'qualification_commands': []}
        origin = datetime(2030, 1, 1, tzinfo=timezone.utc)
        clock = SimpleNamespace(seconds=0.)
        clock.now = lambda *_: origin + timedelta(seconds=clock.seconds if failure != 'clock_stalled' else 0)
        def sleep(seconds):
            clock.seconds += seconds
            if failure == 'credential_changed': self.reader.token = 'changed-token'
        self.expiry_clock = clock
        fake_datetime = Mock(wraps=datetime)
        fake_datetime.now.side_effect = clock.now
        patch.object(ONLINE, 'datetime', fake_datetime).start()
        patch.object(ONLINE.time, 'monotonic', side_effect=lambda: clock.seconds).start()
        patch.object(ONLINE.time, 'sleep', side_effect=sleep).start()
        self.reader.token = 'same-reader-token'
        return clock

    def test_collection_natural_expiry_uses_same_credential_without_revocation_or_renewal(self):
        self.collection_expiry_fixture()
        self.scenario.run_expiry(self.report)
        self.assertEqual(self.report['checks'], ONLINE.COLLECTION_EXPIRY_CHECKS)
        self.assertEqual([x['status'] for x in self.runs.values()], ['success', 'failed'])
        self.assertTrue(self.report['meta_collection']['natural_expiry']['same_credential'])
        self.assertTrue(self.report['meta_collection']['natural_expiry']['history_unchanged'])
        self.scenario.cleanup()
        self.assertFalse(self.tasks)
        self.assertEqual(len(self.qualifications), 1)
        self.assertTrue(all(x['status'] == 'active' and x['version'] == '1' and 'revoked_at' not in x
                            for x in self.qualifications.values()))
        self.assertFalse(any(method == 'DELETE' and '/meta_collection_qualifications/' in path
                             for _, method, path, _ in self.calls))

    def test_collection_expiry_rejects_ignored_expiry_general_errors_and_changed_credentials(self):
        for fault in ('expired_allowed', 'general_failure', 'credential_changed', 'clock_stalled', 'source_changed'):
            with self.subTest(fault=fault):
                self.setUp()
                self.collection_expiry_fixture(fault)
                self.fault = fault
                if fault == 'source_changed': self.scenario.source_snapshot = Mock(side_effect=['before', 'after'])
                with self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run_expiry(self.report)
                self.assertNotEqual(self.report['checks'], ONLINE.COLLECTION_EXPIRY_CHECKS)
                self.scenario.cleanup()
                self.assertFalse(self.tasks)
                self.assertFalse(any('/access_grants' in path or '/access_denies' in path for _, _, path, _ in self.calls))

    def test_collection_expiry_requires_server_times_on_correct_side_and_exact_execution(self):
        for fault in ('before_late', 'after_early', 'missing_time', 'wrong_execution', 'wrong_module'):
            with self.subTest(fault=fault):
                self.setUp()
                clock = self.collection_expiry_fixture()
                original = self.reader.request.side_effect
                def response(method, url, *args, **kwargs):
                    result = original(method, url, *args, **kwargs)
                    # The extra observation used for time proof follows the
                    # terminal/task-summary verification in trigger().
                    if method == 'GET' and '/meta/executions/' in url:
                        count = sum(1 for _, m, p, _ in self.calls if m == 'GET' and p == url)
                        if count == 2:
                            run = result.payload
                            if fault == 'before_late' and clock.seconds == 0:
                                run['completed_at'] = (clock.now(timezone.utc) + timedelta(seconds=45)).isoformat()
                            if fault == 'after_early' and clock.seconds >= 45:
                                run['started_at'] = (clock.now(timezone.utc) - timedelta(seconds=1)).isoformat()
                            if fault == 'missing_time': run.pop('started_at')
                            if fault == 'wrong_execution': run['execution_id'] = str(uuid.uuid4())
                            if fault == 'wrong_module': run['module'] = 'transfer'
                    return result
                self.reader.request.side_effect = response
                with self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run_expiry(self.report)
                self.scenario.cleanup()
                self.assertFalse(self.tasks)

    def test_collection_expiry_keeps_cleanup_ownership_after_unknown_approval_or_trigger(self):
        for operation in ('approve', 'trigger'):
            with self.subTest(operation=operation):
                self.setUp()
                self.collection_expiry_fixture()
                self.lost = operation
                with self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run_expiry(self.report)
                self.scenario.cleanup()
                self.assertFalse(self.tasks)
                self.assertEqual(len(self.qualifications), 1)
                self.assertTrue(all(row['status'] == 'revoked' for row in self.qualifications.values()))

    def test_expired_qualification_replay_must_not_renew_or_blindly_clean_corrupt_binding(self):
        self.collection_expiry_fixture()
        self.fault = 'expired_replay_renews'
        with self.assertRaisesRegex(ONLINE.SuiteError, 'expired approval replay'):
            self.scenario.run_expiry(self.report)
        with self.assertRaisesRegex(ONLINE.SuiteError, 'cleanup'):
            self.scenario.cleanup()
        self.assertTrue(self.tasks)

    def test_manual_qualification_revocation_cannot_stand_in_for_natural_expiry(self):
        self.collection_expiry_fixture()
        original = ONLINE.time.sleep.side_effect
        def revoke_during_wait(seconds):
            original(seconds)
            row = next(iter(self.qualifications.values()))
            row.update(status='revoked', version='2', revoked_at='2030-01-01T00:00:01Z',
                       revoked_by_principal_id='35', revoked_reason='not natural expiry')
        ONLINE.time.sleep.side_effect = revoke_during_wait
        with self.assertRaisesRegex(ONLINE.SuiteError, 'manual revocation'):
            self.scenario.run_expiry(self.report)
        self.scenario.cleanup()
        self.assertFalse(self.tasks)

    def inflight_fixture(self, fault=None, *, natural_expiry=False):
        clock = self.collection_expiry_fixture(fault)
        self.report['meta_collection'] = {'qualification_commands': [], 'scheduled': {
            'executions': {'approved_repeat': {'items_scanned': 3}}}}
        barrier = SimpleNamespace(connection_observation=lambda *args: {'verified': True}, baseline={'reset_at': 'stable', 'calls': {}, 'other_statement_count': 0, 'other_statements': []}, closed=False, released=False)
        selected = []
        def open_source():
            if fault == 'source_open': raise ONLINE.SuiteError('source preparation failed')
            return {'pid': 123, 'locked': True, 'at': clock.now(timezone.utc).isoformat()}
        def blocked():
            if fault == 'barrier_missing': raise ONLINE.SuiteError('source did not reach lock')
            execution = list(self.runs)[-1]
            if selected:
                if fault == 'lost_expiry_barrier': raise ONLINE.SuiteError('lock expired before release')
                return {'pid': 999 if fault == 'changed_expiry_backend' else 124, 'blocker_pid': 123,
                        'started_at': self.runs[execution]['started_at'],
                        'observed_at': (clock.now(timezone.utc) - timedelta(seconds=1 if fault == 'early_source_clock' else 0)).isoformat()}
            selected.append(execution)
            self.runs[execution]['status'] = 'success' if fault == 'not_running' else 'running'
            self.runs[execution].pop('completed_at')
            if fault == 'admitted_after_expiry': clock.seconds += 13
            return {'pid': 124, 'blocker_pid': 123, 'started_at': clock.now(timezone.utc).isoformat(),
                    'observed_at': clock.now(timezone.utc).isoformat()}
        def release():
            # A successful expiry scenario never revokes; failures still revoke
            # any qualification whose original deadline has not passed.
            self.assertTrue(all(row['status'] == 'revoked' or natural_expiry and
                datetime.fromisoformat(row['expires_at']) <= clock.now(timezone.utc)
                for row in self.qualifications.values()))
            if selected and self.runs[selected[0]]['status'] == 'running':
                row = self.runs[selected[0]]
                row.update(status='failed', completed_at=clock.now(timezone.utc).isoformat(),
                    metadata={'items_scanned': 3, 'catalog_nodes_scanned': 1, 'fields_scanned': 0},
                    error_details={'category': 'connection_failed' if fault == 'general_failure' else 'permission_denied'})
                if fault == 'missing_partial': row['metadata'] = {}
                if fault == 'too_many_items': row['metadata']['items_scanned'] = 4
                self.tasks[int(row['source_task_id'])].update(last_execution_status='failed')
            barrier.released = True
        values = {'reset_at': 'changed' if fault == 'statistics_reset' else 'stable',
                  'calls': {'namespaces': 1, 'tables': 2 if fault == 'another_catalog' else 1,
                            'workspace_detection': 2, 'other': 1 if fault == 'other_sql' else 0}}
        calls = []
        def counts():
            calls.append(True)
            result = dict(deepcopy(values), other_statement_count=0, other_statements=[])
            if fault == 'repeat_source_io' and len(calls) > 1: result['calls']['tables'] += 1
            return result
        def close():
            release()
            self.assertFalse(any(row['status'] == 'running' for row in self.runs.values()))
            if fault == 'source_residual': raise ONLINE.SuiteError('source schema remains')
            barrier.closed = True
        barrier.open, barrier.wait_blocked, barrier.release = open_source, blocked, release
        barrier.counts, barrier.close = counts, close
        return barrier

    def test_inflight_expiry_uses_original_deadline_denies_next_catalog_and_preserves_history(self):
        barrier = self.inflight_fixture(natural_expiry=True)
        with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=barrier):
            self.scenario.run_inflight_expiry(self.report)
        self.assertEqual(self.report['checks'], ONLINE.COLLECTION_INFLIGHT_EXPIRY_CHECKS)
        evidence = self.report['meta_collection']['inflight_expiry']
        self.assertTrue(evidence['no_revocation'] and barrier.closed and barrier.released)
        self.assertEqual(evidence['stored_status'], 'active')
        self.assertEqual([r['status'] for r in self.runs.values()], ['failed', 'failed'])
        self.scenario.cleanup()
        self.assertFalse(self.tasks)
        self.assertTrue(all(r['status'] == 'active' and r['version'] == '1' for r in self.qualifications.values()))
        self.assertFalse(any(method == 'DELETE' and '/meta_collection_qualifications/' in path
                             for _, method, path, _ in self.calls))

    def test_inflight_expiry_rejects_missing_barrier_read_leaks_history_changes_and_wrong_timing(self):
        for fault in ('source_open', 'barrier_missing', 'not_running', 'general_failure', 'missing_partial',
                      'too_many_items', 'statistics_reset', 'another_catalog', 'other_sql', 'repeat_source_io',
                      'lost_expiry_barrier', 'changed_expiry_backend', 'admitted_after_expiry',
                      'early_source_clock', 'clock_stalled', 'credential_changed', 'expired_allowed', 'expired_replay_renews'):
            with self.subTest(fault=fault):
                self.setUp()
                barrier = self.inflight_fixture(fault, natural_expiry=True)
                self.fault = fault if fault in {'expired_allowed', 'expired_replay_renews'} else None
                with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=barrier), self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run_inflight_expiry(self.report)
                self.assertNotEqual(self.report['checks'], ONLINE.COLLECTION_INFLIGHT_EXPIRY_CHECKS)
                if fault == 'expired_replay_renews':
                    # A corrupt binding must not be blindly revoked or deleted.
                    with self.assertRaisesRegex(ONLINE.SuiteError, 'cleanup'): self.scenario.cleanup()
                    self.assertTrue(self.tasks)
                    continue
                self.scenario.cleanup()
                self.assertFalse(self.tasks)
                self.assertTrue(barrier.closed)

    def test_inflight_expiry_cannot_be_replaced_by_manual_revocation(self):
        barrier = self.inflight_fixture(natural_expiry=True)
        sleep = ONLINE.time.sleep.side_effect
        def revoke_while_waiting(seconds):
            sleep(seconds)
            row = next(iter(self.qualifications.values()))
            row.update(status='revoked', version='2', revoked_at=self.expiry_clock.now(timezone.utc).isoformat())
        ONLINE.time.sleep.side_effect = revoke_while_waiting
        with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=barrier), self.assertRaisesRegex(
                ONLINE.SuiteError, 'natural expiry changed qualification history'):
            self.scenario.run_inflight_expiry(self.report)
        self.scenario.cleanup()
        self.assertFalse(self.tasks)

    def test_connection_evidence_does_not_weaken_other_assertion_or_failure_cleanup(self):
        for fault in ('other_sql', 'attribution_missing', 'log_read_failed'):
            with self.subTest(fault=fault):
                self.setUp()
                barrier = self.inflight_fixture('other_sql' if fault == 'other_sql' else None, natural_expiry=True)
                connection = {'verified': fault == 'other_sql', 'events': [{'module': 'system', 'category': 'other'}]}
                barrier.connection_observation = Mock(return_value=connection)
                if fault == 'log_read_failed':
                    barrier.connection_observation.side_effect = ONLINE.SuiteError('private source log unavailable')
                with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=barrier), self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run_inflight_expiry(self.report)
                evidence = self.report['meta_collection']['inflight_expiry']
                if fault != 'log_read_failed': self.assertEqual(evidence['connection_observation'], connection)
                self.assertFalse(self.report['checks'])
                self.assertNotIn('next_catalog_denied', evidence)
                self.scenario.cleanup()
                self.assertTrue(barrier.closed and barrier.released)
                self.assertFalse(self.tasks)

    def test_inflight_counter_failures_retain_bounded_diagnostics_without_marking_passed(self):
        for fault, stage in [('statistics_reset', 'reset_check'), ('another_catalog', 'delta_check'),
                             ('counter_transport', 'query')]:
            with self.subTest(fault=fault):
                self.setUp()
                barrier = self.inflight_fixture(fault, natural_expiry=True)
                if fault == 'counter_transport':
                    barrier.counts = Mock(side_effect=ONLINE.SuiteError('private SQL and credential text'))
                with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=barrier), self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run_inflight_expiry(self.report)
                evidence = self.report['meta_collection']['inflight_expiry']
                self.assertEqual(evidence['source_counter_stage'], stage)
                self.assertNotIn('next_catalog_denied', evidence)
                self.assertFalse(self.report['checks'])
                self.assertNotIn('private SQL', json.dumps(evidence))
                if fault != 'counter_transport':
                    self.assertEqual(set(evidence['source_call_delta']),
                                     {'namespaces', 'tables', 'workspace_detection', 'other'})
                    self.assertEqual(set(evidence['source_counter_observed']), {'calls', 'reset_at', 'other_statement_count', 'other_statements'})
                self.scenario.cleanup()
                self.assertFalse(self.tasks)

    def test_repeat_source_failure_keeps_last_snapshot_and_each_statement_change(self):
        for natural_expiry in (False, True):
            for fault in ('probe', 'reset', 'balanced_changes', 'removed', 'classification'):
                with self.subTest(natural_expiry=natural_expiry, fault=fault):
                    self.setUp()
                    barrier = self.inflight_fixture(natural_expiry=natural_expiry)
                    counts = barrier.counts
                    before = dict(counts(), other_statement_count=2, other_statements=[
                        {'query_id': '-123', 'top_level': True, 'kind': 'version_probe', 'calls': 8},
                        {'query_id': '456', 'top_level': True, 'kind': 'unclassified', 'calls': 1}])
                    before['calls']['other'] = 9
                    barrier.baseline.update(other_statement_count=0, other_statements=[])
                    barrier.baseline['calls']['other'] = 9
                    after = deepcopy(before)
                    if fault == 'reset': after['reset_at'] = 'new-reset'
                    elif fault == 'removed':
                        after['other_statements'].pop()
                        after['other_statement_count'] = 1
                        after['calls']['other'] -= 1
                    elif fault == 'classification': after['other_statements'][0]['kind'] = 'unclassified'
                    else:
                        after['other_statements'][0]['calls'] += 1
                        if fault == 'probe': after['calls']['other'] += 1
                        else: after['other_statements'][1]['calls'] -= 1
                    barrier.counts = Mock(side_effect=[before, after])
                    run = self.scenario.run_inflight_expiry if natural_expiry else self.scenario.run_inflight
                    with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=barrier), self.assertRaisesRegex(
                            ONLINE.SuiteError, 'denied repeat touched source'):
                        run(self.report)
                    evidence = self.report['meta_collection']['inflight_expiry' if natural_expiry else 'inflight']
                    self.assertEqual(evidence['repeat_source_counter_stage'], 'equality_check')
                    self.assertTrue(evidence['repeat_source_counter_available'])
                    self.assertEqual(evidence['source_counter_observed'], before)
                    self.assertEqual(evidence['repeat_source_counter_observed'], after)
                    self.assertEqual(evidence['repeat_source_call_delta']['other'], {'probe': 1, 'removed': -1}.get(fault, 0))
                    changed = evidence['repeat_other_statement_delta']
                    self.assertEqual(len(changed), {'probe': 1, 'reset': 0, 'balanced_changes': 2, 'removed': 1, 'classification': 1}[fault])
                    if fault in ('probe', 'balanced_changes'):
                        self.assertEqual(changed[0], {'query_id': '-123', 'top_level': True,
                            'before': before['other_statements'][0], 'after': after['other_statements'][0], 'calls_delta': 1})
                    if fault == 'removed':
                        self.assertEqual(changed[0], {'query_id': '456', 'top_level': True,
                            'before': before['other_statements'][1], 'after': None, 'calls_delta': -1})
                    if fault == 'classification':
                        self.assertEqual(changed[0]['calls_delta'], 0)
                        self.assertEqual(changed[0]['before']['kind'], 'version_probe')
                        self.assertEqual(changed[0]['after']['kind'], 'unclassified')
                    self.assertEqual(len(self.report['checks']), 1)
                    self.scenario.cleanup()
                    self.assertTrue(barrier.closed)
                    self.assertFalse(self.tasks)

    def test_repeat_counter_transport_failure_is_distinct_from_zero_or_changed_counts(self):
        for natural_expiry in (False, True):
            with self.subTest(natural_expiry=natural_expiry):
                self.setUp()
                barrier = self.inflight_fixture(natural_expiry=natural_expiry)
                first = dict(barrier.counts(), other_statement_count=0, other_statements=[])
                barrier.counts = Mock(side_effect=[first, ONLINE.SuiteError('private SQL and credential text')])
                run = self.scenario.run_inflight_expiry if natural_expiry else self.scenario.run_inflight
                with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=barrier), self.assertRaises(ONLINE.SuiteError):
                    run(self.report)
                evidence = self.report['meta_collection']['inflight_expiry' if natural_expiry else 'inflight']
                self.assertEqual(evidence['repeat_source_counter_stage'], 'query')
                self.assertFalse(evidence['repeat_source_counter_available'])
                self.assertNotIn('repeat_source_counter_observed', evidence)
                self.assertNotIn('repeat_source_call_delta', evidence)
                self.assertNotIn('private SQL', json.dumps(evidence))
                self.assertEqual(len(self.report['checks']), 1)
                self.scenario.cleanup()
                self.assertFalse(self.tasks)
                self.assertTrue(barrier.closed)

    def test_version_probe_diagnostic_never_exempts_an_extra_other_call(self):
        for natural_expiry in (False, True):
            with self.subTest(natural_expiry=natural_expiry):
                self.setUp()
                barrier = self.inflight_fixture('other_sql', natural_expiry=natural_expiry)
                barrier.baseline.update(other_statement_count=0, other_statements=[])
                counts = barrier.counts
                row = {'query_id': '-123', 'top_level': True, 'kind': 'version_probe', 'calls': 1}
                barrier.counts = lambda: dict(counts(), other_statement_count=1, other_statements=[row])
                run = self.scenario.run_inflight_expiry if natural_expiry else self.scenario.run_inflight
                with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=barrier), self.assertRaisesRegex(
                        ONLINE.SuiteError, 'source opened another Catalog read'):
                    run(self.report)
                evidence = self.report['meta_collection']['inflight_expiry' if natural_expiry else 'inflight']
                self.assertEqual(evidence['source_counter_stage'], 'delta_check')
                self.assertEqual(evidence['source_call_delta']['other'], 1)
                self.assertEqual(evidence['source_counter_observed']['other_statements'], [row])
                self.assertNotIn('next_catalog_denied', evidence)
                self.assertFalse(self.report['checks'])
                self.scenario.cleanup()
                self.assertFalse(self.tasks)
                self.assertTrue(barrier.closed)

    def test_inflight_expiry_unknown_responses_and_source_cleanup_fail_closed(self):
        for operation in ('approve', 'trigger', 'replay', 'source_residual'):
            with self.subTest(operation=operation):
                self.setUp()
                barrier = self.inflight_fixture(operation if operation == 'source_residual' else None, natural_expiry=True)
                if operation == 'replay':
                    request = self.operator.request.side_effect
                    def lose_replay(*args, **kwargs):
                        if args[0] == 'POST' and '/meta_collection_qualifications' in args[1] and self.qualifications:
                            self.lost = 'approve'
                        return request(*args, **kwargs)
                    self.operator.request.side_effect = lose_replay
                else:
                    self.lost = operation
                with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=barrier), self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run_inflight_expiry(self.report)
                if operation == 'source_residual':
                    with self.assertRaisesRegex(ONLINE.SuiteError, 'source fixture cleanup'): self.scenario.cleanup()
                else:
                    self.scenario.cleanup()
                    self.assertFalse(self.tasks)

    def test_inflight_revocation_retains_admitted_results_denies_next_catalog_and_cleans_source(self):
        barrier = self.inflight_fixture()
        with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=barrier):
            self.scenario.run_inflight(self.report)
        self.assertEqual(self.report['checks'], ONLINE.COLLECTION_INFLIGHT_CHECKS)
        self.assertTrue(barrier.closed and barrier.released)
        self.assertEqual([x['status'] for x in self.runs.values()], ['failed', 'failed'])
        self.assertEqual(self.report['meta_collection']['inflight']['source_call_delta']['tables'], 1)
        self.assertEqual(self.scenario.attempts[0]['denial_boundary'], 'after_admitted_catalog')
        self.scenario.cleanup()
        self.assertFalse(self.tasks)

    def test_inflight_revocation_cannot_accept_ordinary_errors_additional_reads_or_statistics_reset(self):
        for fault in ('barrier_missing', 'source_open', 'not_running', 'general_failure', 'missing_partial',
                      'too_many_items', 'statistics_reset', 'another_catalog', 'other_sql', 'repeat_source_io'):
            with self.subTest(fault=fault):
                self.setUp()
                barrier = self.inflight_fixture(fault)
                with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=barrier), self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run_inflight(self.report)
                self.assertNotEqual(self.report['checks'], ONLINE.COLLECTION_INFLIGHT_CHECKS)
                self.scenario.cleanup()
                self.assertFalse(self.tasks)
                self.assertTrue(barrier.closed)
                self.assertFalse(any('/access_grants' in path or '/access_denies' in path for _, _, path, _ in self.calls))

    def test_inflight_unknown_approval_and_trigger_preserve_cleanup_and_source_residual_is_failure(self):
        for operation in ('approve', 'trigger', 'revoke', 'source_residual'):
            with self.subTest(operation=operation):
                self.setUp()
                barrier = self.inflight_fixture(operation if operation == 'source_residual' else None)
                self.lost = operation
                with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=barrier):
                    if operation == 'revoke':
                        self.scenario.run_inflight(self.report)
                    else:
                        with self.assertRaises(ONLINE.SuiteError): self.scenario.run_inflight(self.report)
                if operation == 'source_residual':
                    with self.assertRaisesRegex(ONLINE.SuiteError, 'source fixture cleanup'):
                        self.scenario.cleanup()
                else:
                    self.scenario.cleanup()
                    self.assertFalse(self.tasks)
                self.assertTrue(all(row['status'] == 'revoked' for row in self.qualifications.values()))

    def browser_fixture(self, failure=None):
        self.report['meta_collection'] = {'qualification_commands': []}
        browser = Mock()
        body, committed = {}, {}
        def factory(tenant, issuer, engine, task, digest, purpose, expiry):
            self.assertEqual((tenant, issuer, engine), (3, '35', 2))
            body.update(command_id=str(uuid.uuid4()), task_id=str(task), expected_task_definition_digest=digest,
                        purpose=purpose, expires_at=expiry)
        # Special methods are looked up on the type, not the instance dictionary.
        class Context:
            def __enter__(self): return browser
            def __exit__(self, *args): return False
        def create(*args):
            factory(*args)
            return Context()
        def capture():
            if failure == 'capture': raise ONLINE.SuiteError('browser stopped before send')
            return deepcopy(body)
        def release(kind):
            if kind == 'command':
                self.assertEqual(self.scenario.commands[body['command_id']], body)
                self.assertIn(body['command_id'], self.report['meta_collection']['qualification_commands'])
                result = self.request('operator', 'POST', self.scenario.route, (201,), body)
                committed.update(result.payload)
                if failure == 'after_commit': raise ONLINE.SuiteError('browser stopped after commit')
        def wait(kind):
            if failure == kind: raise ONLINE.SuiteError('browser stopped at ' + kind)
            if kind == 'approved':
                self.request('operator', 'POST', self.scenario.route, (201,), body)
                if failure == 'recovery_renews': self.qualifications[body['command_id']]['expires_at'] = '2099-01-01T00:00:00Z'
            if kind == 'revoked':
                self.request('operator', 'DELETE', self.scenario.route + '/' + body['command_id'], (200,),
                             {'version': '1', 'reason': 'Disposable PG browser qualification withdrawal'})
        browser.capture_command.side_effect = capture
        browser.release.side_effect = release
        browser.committed_row.side_effect = lambda: deepcopy(committed)
        browser.wait.side_effect = wait
        browser.finish.side_effect = ONLINE.SuiteError('incomplete browser report') if failure == 'finish' else None
        browser.finish.return_value = {'result': 'passed'}
        return create

    def test_scheduled_collection_uses_real_scheduler_observation_without_manual_trigger(self):
        self.collection_expiry_fixture()
        self.schedule_dispatch = True
        self.report['meta_collection'] = {'qualification_commands': []}
        self.scenario.run_scheduled(self.report)
        self.assertEqual(self.report['checks'], ONLINE.COLLECTION_SCHEDULED_CHECKS)
        self.assertEqual([row['status'] for row in self.runs.values()], ['failed', 'success', 'success', 'failed'])
        self.assertTrue(all(row['trigger_type'] == 'scheduled' and row.get('triggered_by') is None
                            for row in self.runs.values()))
        self.assertFalse(any(path.endswith('/trigger') for _, _, path, _ in self.calls))
        self.assertTrue(self.report['meta_collection']['scheduled']['history_unchanged'])
        self.assertTrue(all(attempt['stage'] == 'verified' and len(attempt['server_times']) == 4
                            for attempt in self.scenario.attempts))
        self.scenario.cleanup()
        self.assertFalse(self.tasks)
        self.assertTrue(all(row['status'] == 'revoked' for row in self.qualifications.values()))

    def scheduled_fixture(self, fault=None):
        self.collection_expiry_fixture()
        self.schedule_dispatch = True
        self.report['meta_collection'] = {'qualification_commands': []}
        self.fault = fault

    def test_scheduled_time_failures_keep_bounded_evidence_before_unchanged_assertion(self):
        for fault in ('created_after_start', 'start_after_completion', 'missing_start', 'invalid_start', 'invalid_completion'):
            with self.subTest(fault=fault):
                self.setUp()
                self.scheduled_fixture(fault)
                self.report['meta_collection']['attempts'] = self.scenario.attempts
                with self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run_scheduled(self.report)
                attempt = self.report['meta_collection']['attempts'][-1]
                self.assertEqual(attempt['stage'], 'scheduled_timing')
                self.assertTrue(attempt['summary_matches'])
                times = attempt['server_times']
                self.assertEqual(set(times), {'next_run_at', 'created_at', 'started_at', 'completed_at'})
                self.assertEqual(times['next_run_at'], attempt['next_run_at'])
                if fault in ('missing_start', 'invalid_start'):
                    self.assertEqual(times['started_at'], 'absent_or_invalid')
                if fault == 'invalid_completion':
                    self.assertEqual(times['completed_at'], 'absent_or_invalid')
                for key, value in times.items():
                    if value != 'absent_or_invalid': ONLINE.collection_instant(value)
                self.assertFalse(self.report['checks'])
                self.assertEqual(self.report['meta_collection']['scheduled']['executions'], {})
                self.assertNotIn('private-secret', json.dumps(self.report))
                self.fault = None
                self.scenario.cleanup()
                self.assertFalse(self.tasks)

    def test_scheduled_runs_reject_manual_identity_time_parent_retry_and_general_failures(self):
        for fault in ('manual_run', 'human_actor', 'early_run', 'wrong_source', 'wrong_boundary', 'parent_run',
                      'retry_run', 'unapproved_allowed', 'general_failure', 'read_before_deny', 'stale_summary',
                      'duplicate_scheduled', 'truncated_scheduled', 'scheduler_stalled', 'wrong_task'):
            with self.subTest(fault=fault):
                self.setUp()
                self.scheduled_fixture(fault)
                with self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run_scheduled(self.report)
                self.assertNotEqual(self.report['checks'], ONLINE.COLLECTION_SCHEDULED_CHECKS)
                self.fault = None
                self.scenario.cleanup()
                self.assertFalse(self.tasks)
                self.assertFalse(any(path.endswith('/trigger') for _, _, path, _ in self.calls))
                self.assertFalse(any('/access_grants' in path or '/access_denies' in path for _, _, path, _ in self.calls))

    def test_scheduled_approval_does_not_renew_or_revive_and_source_rules_stay_unchanged(self):
        for fault in ('repeat_renews', 'replay_revives', 'revoked_allowed', 'source_changed', 'credential_changed'):
            with self.subTest(fault=fault):
                self.setUp()
                self.scheduled_fixture(fault)
                if fault == 'source_changed': self.scenario.source_snapshot = Mock(side_effect=['before', 'after'])
                if fault == 'credential_changed':
                    original = ONLINE.time.sleep.side_effect
                    def changed(seconds):
                        original(seconds)
                        self.reader.token = 'another-credential'
                    ONLINE.time.sleep.side_effect = changed
                with self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run_scheduled(self.report)
                self.assertNotEqual(self.report['checks'], ONLINE.COLLECTION_SCHEDULED_CHECKS)
                if fault == 'repeat_renews':
                    with self.assertRaisesRegex(ONLINE.SuiteError, 'cleanup'):
                        self.scenario.cleanup()
                    self.assertFalse(any(method == 'DELETE' and '/meta_collection_qualifications/' in path
                                         for _, method, path, _ in self.calls))
                    continue
                self.fault = None
                self.scenario.cleanup()
                self.assertFalse(self.tasks)

    def test_scheduled_unknown_enable_or_disable_response_remains_owned_and_paused_before_delete(self):
        for operation in ('enable', 'disable', 'approve', 'revoke'):
            with self.subTest(operation=operation):
                self.setUp()
                self.scheduled_fixture()
                self.lost = operation
                if operation in ('enable', 'approve'):
                    with self.assertRaises(ONLINE.SuiteError):
                        self.scenario.run_scheduled(self.report)
                else:
                    self.scenario.run_scheduled(self.report)
                self.scenario.cleanup()
                self.assertFalse(self.tasks)
                self.assertTrue(all(row['status'] == 'revoked' for row in self.qualifications.values()))
                deletes = [i for i, (_, method, path, _) in enumerate(self.calls)
                           if method == 'DELETE' and '/meta/scan/tasks/' in path]
                self.assertTrue(deletes)
                self.assertTrue(any(method == 'PUT' and body['enabled'] is False
                    for _, method, path, body in self.calls[:deletes[0]]))

    def test_scheduled_cleanup_refuses_to_delete_if_disable_did_not_commit(self):
        self.scheduled_fixture('disable_noop')
        with self.assertRaisesRegex(ONLINE.SuiteError, 'schedule did not stop'):
            self.scenario.run_scheduled(self.report)
        with self.assertRaisesRegex(ONLINE.SuiteError, 'cleanup'):
            self.scenario.cleanup()
        self.assertTrue(self.tasks)
        self.assertFalse(any(method == 'DELETE' and '/meta/scan/tasks/' in path for _, method, path, _ in self.calls))
        self.fault = None
        self.scenario.cleanup()
        self.assertFalse(self.tasks)

    def test_scheduled_failed_poll_pauses_then_converges_inflight_execution_before_delete(self):
        self.scheduled_fixture()
        original = self.reader.request.side_effect
        observed = []
        def failed(method, url, *args, **kwargs):
            if method == 'GET' and '/meta/executions/' in url and not observed:
                execution = url.rsplit('/', 1)[1]
                self.runs[execution]['status'] = 'running'
                observed.append(execution)
                raise ONLINE.SuiteError('failed execution observation')
            return original(method, url, *args, **kwargs)
        self.reader.request.side_effect = failed
        with self.assertRaises(ONLINE.SuiteError):
            self.scenario.run_scheduled(self.report)
        self.assertTrue(observed)
        self.assertTrue(all(row['enabled'] is False for row in self.tasks.values()))
        sleep = ONLINE.time.sleep.side_effect
        def complete(seconds):
            self.assertTrue(self.tasks)  # Never delete while this execution is in flight.
            sleep(seconds)
            self.runs[observed[0]]['status'] = 'failed'
        ONLINE.time.sleep.side_effect = complete
        self.scenario.cleanup()
        self.assertFalse(self.tasks)

    def test_browser_approval_controls_current_worker_results_and_one_cleanup_identity(self):
        with patch.object(ONLINE, 'CollectionObservation', side_effect=self.browser_fixture()):
            self.scenario.run_browser(self.report)
        self.assertEqual(self.report['checks'], ONLINE.COLLECTION_BROWSER_CHECKS)
        self.assertEqual(len(self.qualifications), 1)
        self.assertEqual(len(self.runs), 5)
        self.assertTrue(self.report['meta_collection']['browser_flow']['source_rules_unchanged'])
        self.assertEqual([row['status'] for row in self.runs.values()], ['failed', 'success', 'success', 'failed', 'failed'])
        approvals = [body for _, method, path, body in self.calls if method == 'POST' and path == self.scenario.route]
        self.assertEqual(len(approvals), 2)
        self.assertEqual(approvals[0], approvals[1])
        self.scenario.cleanup()
        self.assertFalse(self.tasks)
        self.assertTrue(all(row['status'] == 'revoked' for row in self.qualifications.values()))

    def test_browser_failures_preserve_pre_send_cleanup_even_after_committed_lost_response(self):
        for failure in ('capture', 'after_commit', 'approved', 'revoked', 'finish'):
            with self.subTest(failure=failure):
                self.setUp()
                with patch.object(ONLINE, 'CollectionObservation', side_effect=self.browser_fixture(failure)), self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run_browser(self.report)
                self.assertFalse(self.report['checks'])
                self.scenario.cleanup()
                self.assertFalse(self.tasks)
                self.assertTrue(all(row['status'] == 'revoked' for row in self.qualifications.values()))
                self.assertFalse(any('/access_grants' in path or '/access_denies' in path for _, _, path, _ in self.calls))

    def test_browser_recovery_cannot_renew_and_corrupt_binding_does_not_authorize_blind_cleanup(self):
        with patch.object(ONLINE, 'CollectionObservation', side_effect=self.browser_fixture('recovery_renews')), \
             self.assertRaisesRegex(ONLINE.SuiteError, 'approved binding'):
            self.scenario.run_browser(self.report)
        self.assertFalse(self.report['checks'])
        with self.assertRaisesRegex(ONLINE.SuiteError, 'cleanup'):
            self.scenario.cleanup()
        self.assertTrue(self.tasks)
        self.assertFalse(any(method == 'DELETE' for _, method, _, _ in self.calls))

    def test_equivalent_post_and_history_timestamp_zones_preserve_full_qualification_binding(self):
        self.fault = 'different_timestamp_zone'
        self.scenario.run(self.report)
        self.assertEqual(self.report['checks'], ONLINE.COLLECTION_CHECKS)
        self.assertEqual(len(self.qualifications), 1)
        self.assertFalse(self.report['meta_collection']['approval']['raw_equal'])
        self.assertTrue(self.report['meta_collection']['approval']['semantic_equal'])
        self.assertEqual(self.report['meta_collection']['approval']['stage'], 'verified')
        self.scenario.cleanup()

    def test_qualification_equality_preserves_all_fields_and_nanosecond_expiry(self):
        original = {'id': 'stable-command', 'status': 'active', 'version': '1', 'task_definition_digest': 'a' * 64,
                    'expires_at': '2026-10-23T03:33:18.123456700Z'}
        same = dict(original, expires_at='2026-10-23T11:33:18.1234567+08:00')
        self.assertTrue(ONLINE.same_collection_row(original, same))
        for changes in ({'expires_at': '2026-10-23T03:33:18.123456701Z'}, {'status': 'revoked'},
                        {'version': '2'}, {'task_definition_digest': 'b' * 64}, {'unexpected': True}):
            with self.subTest(changes=changes):
                self.assertFalse(ONLINE.same_collection_row(original, dict(same, **changes)))
        for invalid in ('2026-10-23T03:33:18', '2026-10-23T03:33:18.1234567890Z', '2026-99-23T03:33:18Z', None):
            with self.subTest(invalid=invalid), self.assertRaises(ONLINE.SuiteError):
                ONLINE.same_collection_row(original, dict(same, expires_at=invalid))

    def test_operator_approval_failure_preserves_http_stage_and_command_for_cleanup(self):
        original = self.operator.request.side_effect
        def failed(method, url, *args, **kwargs):
            if method == 'POST' and url.endswith('/meta_collection_qualifications'):
                raise ONLINE.SuiteError('POST /api/v1/system/engines/2/meta_collection_qualifications returned HTTP 503 (meta_collection_facts_unavailable)')
            return original(method, url, *args, **kwargs)
        self.operator.request.side_effect = failed
        with self.assertRaises(ONLINE.SuiteError):
            self.scenario.run(self.report)
        approval = self.report['meta_collection']['approval']
        self.assertEqual(approval['stage'], 'operator_post')
        self.assertEqual(approval['http_status'], 503)
        self.assertEqual(approval['http_error_code'], 'meta_collection_facts_unavailable')
        self.assertIn(approval['command_id'], self.scenario.commands)
        self.assertEqual(self.report['checks'], [ONLINE.COLLECTION_CHECKS[0]])
        self.scenario.cleanup()

    def test_approval_readback_failure_reports_unique_row_or_field_binding_and_still_cleans(self):
        for fault in ('missing_row', 'wrong_digest'):
            with self.subTest(fault=fault):
                self.setUp()
                original = self.operator.request.side_effect
                injected = False
                def changed(method, url, *args, **kwargs):
                    nonlocal injected
                    result = original(method, url, *args, **kwargs)
                    if not injected and method == 'GET' and '/meta_collection_qualifications?' in url:
                        injected = True
                        if fault == 'missing_row':
                            result.payload.update(data=[], total=0)
                        else:
                            result.payload['data'][0]['task_definition_digest'] = 'f' * 64
                    return result
                self.operator.request.side_effect = changed
                with self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run(self.report)
                approval = self.report['meta_collection']['approval']
                self.assertEqual(approval['post_status'], 201)
                self.assertEqual(approval['http_status'], 200)
                self.assertEqual(approval['stage'], 'history_row_identity' if fault == 'missing_row' else 'history_row_binding')
                if fault == 'wrong_digest':
                    self.assertFalse(approval['binding']['digest'])
                self.scenario.cleanup()
                self.assertFalse(self.tasks)
                self.assertTrue(all(row['status'] == 'revoked' for row in self.qualifications.values()))

    def test_rejects_missing_safety_checks_without_counting_partial_pass(self):
        for fault in ('unapproved_allowed', 'changed_allowed', 'revoked_allowed', 'copied_allowed',
                      'repeat_renews', 'replay_revives', 'general_failure', 'stale_summary',
                      'read_before_deny', 'wrong_task', 'foreign_task'):
            with self.subTest(fault=fault):
                self.setUp()
                self.fault = fault
                with self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run(self.report)
                self.assertNotEqual(self.report['checks'], ONLINE.COLLECTION_CHECKS)

    def test_unknown_write_responses_are_recovered_without_creating_new_commands(self):
        for operation in ('create', 'approve', 'trigger'):
            with self.subTest(operation=operation):
                self.setUp()
                self.lost = operation
                with self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run(self.report)
                self.scenario.cleanup()
                self.assertFalse(self.tasks)
                self.assertTrue(all(row['status'] == 'revoked' for row in self.qualifications.values()))
                self.assertLessEqual(len(self.qualifications), 1)

    def test_unknown_revoke_and_delete_are_read_back_not_retried(self):
        for operation in ('revoke', 'delete'):
            with self.subTest(operation=operation):
                self.setUp()
                self.lost = operation
                self.scenario.run(self.report)
                self.scenario.cleanup()
                paths = [path for _, method, path, _ in self.calls if method == 'DELETE']
                self.assertEqual(len(paths), len(set(paths)))
                self.assertFalse(self.tasks)

    def test_cleanup_does_not_hide_revocation_or_foreign_qualification_failures(self):
        self.scenario.run(self.report)
        row = next(iter(self.qualifications.values()))
        row.update(status='active', version='1')
        self.fault = 'revoke_noop'
        with self.assertRaises(ONLINE.SuiteError):
            self.scenario.cleanup()
        self.assertTrue(self.tasks)
        self.fault = None
        foreign = deepcopy(row)
        foreign['id'] = str(uuid.uuid4())
        self.qualifications[foreign['id']] = foreign
        with self.assertRaises(ONLINE.SuiteError):
            self.scenario.cleanup()
        self.assertEqual(foreign['status'], 'active')
        self.assertTrue(self.tasks)

    def test_source_history_mutation_is_not_mistaken_for_qualification(self):
        self.scenario.source_snapshot = Mock(side_effect=['before', 'after'])
        with self.assertRaisesRegex(ONLINE.SuiteError, 'modified source'):
            self.scenario.run(self.report)
        self.scenario.cleanup()

    def test_failed_attempt_retains_exact_stage_without_raw_response_or_exception(self):
        for fault, stage in (('unapproved_allowed', 'terminal_state'), ('wrong_task', 'execution_binding'),
                             ('general_failure', 'authorization_denial'), ('stale_summary', 'task_summary')):
            with self.subTest(fault=fault):
                self.setUp()
                self.fault = fault
                with self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run(self.report)
                attempt = self.report['meta_collection']['attempts'][0]
                self.assertEqual(attempt['stage'], stage)
                self.assertEqual(attempt['http_status'], 200)
                self.assertEqual(attempt['execution_id'], next(iter(self.runs)))
                self.assertNotIn('unapproved', self.report['meta_collection']['executions'])
                self.assertNotIn('private-password', json.dumps(self.report))

    def test_non_200_trigger_still_fails_and_only_publishes_bounded_machine_error(self):
        for message, expected in (('POST /api/v1/meta/scan/tasks/100/trigger returned HTTP 500 (internal_error)',
                                   {'http_status': 500, 'http_error_code': 'internal_error'}),
                                  ('transport failed: token=private-secret https://private.invalid',
                                   {'request_failed': True})):
            with self.subTest(message=message):
                self.setUp()
                original = self.reader.request.side_effect
                def failed(method, url, *args, **kwargs):
                    if url.endswith('/trigger'):
                        raise ONLINE.SuiteError(message)
                    return original(method, url, *args, **kwargs)
                self.reader.request.side_effect = failed
                with self.assertRaises(ONLINE.SuiteError):
                    self.scenario.run(self.report)
                attempt = self.report['meta_collection']['attempts'][0]
                self.assertEqual(attempt['stage'], 'trigger')
                self.assertTrue(all(attempt[key] == value for key, value in expected.items()))
                self.assertNotIn('private-secret', json.dumps(self.report))
                self.assertFalse(self.report['checks'])
                self.scenario.cleanup()

    def test_cleanup_continues_other_tasks_after_one_delete_failure(self):
        self.scenario.run(self.report)
        original = self.operator.request.side_effect
        primary = self.report['meta_collection']['task_id']
        def failed(*args, **kwargs):
            if args[:2] == ('DELETE', '/api/v1/meta/scan/tasks/' + primary):
                raise ONLINE.SuiteError('delete did not commit')
            return original(*args, **kwargs)
        self.operator.request.side_effect = failed
        with self.assertRaises(ONLINE.SuiteError):
            self.scenario.cleanup()
        self.assertEqual(list(self.tasks), [int(primary)])


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
        self.lifecycle.measure_read_load = Mock(side_effect=lambda report, bindings:
                                               report.update(authorization_load={}))
        self.lifecycle.require_batch_denied = Mock()
        self.reader.request.side_effect = self.request
        patch.object(ONLINE, 'item', return_value={'id': 10, 'node_id': 7}).start()
        patch.object(ONLINE.REGISTRATION, 'read_exact_record_authorization_facts', return_value=self.facts).start()
        self.preview = patch.object(ONLINE, 'preview', side_effect=self.read).start()
        patch.object(ONLINE.time, 'sleep').start()
        self.addCleanup(patch.stopall)

    def row(self):
        row = dict(request_id=self.command, approval_mode='task_production', producer_owner='transfer',
                   production_execution_id=self.execution, catalog_path=self.path, recipient_type='user', recipient_id='33',
                   operator_principal_id='33', action='read', direct_permission_source='', direct_permission_version='0', expiry_mode='at_time',
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
        bindings = self.lifecycle.measure_read_load.call_args.args[1]
        self.assertEqual([(path, command) for path, _, command in bindings],
                         [(self.lifecycle.path, self.source_command), (self.path, self.command)])
        self.lifecycle.require_batch_denied.assert_called_once_with(self.reader, [self.lifecycle.path, self.path])
        self.assertTrue(report['authorization_load']['mixed_withdrawn_batch_denied'])

    def test_initial_grant_rejects_wrong_target_origin_source_or_expiry(self):
        for invalid in ({'approval_mode': 'independent'}, {'producer_owner': 'develop'},
                        {'production_execution_id': 'f9041a08-785c-48bc-bb12-c0650b720dac'},
                        {'recipient_id': '34'}, {'operator_principal_id': '35'}, {'issuer_principal_id': '33'},
                        {'boundary_hash': 'c' * 64}, {'direct_permission_version': '1'},
                        {'expires_at': '2031-10-20T10:00:00Z'}):
            self.bad = invalid
            with self.subTest(invalid=invalid), self.assertRaises(ONLINE.SuiteError):
                self.lifecycle.production_flow({'checks': []})

    def test_load_failure_keeps_source_and_output_owned_for_final_cleanup(self):
        self.lifecycle.measure_read_load.side_effect = ONLINE.SuiteError('load failed')
        with self.assertRaisesRegex(ONLINE.SuiteError, 'load failed'):
            self.lifecycle.production_flow({'checks': []})
        self.assertFalse(self.source_revoked or self.output_revoked)
        self.lifecycle.cleanup()
        self.assertTrue(self.source_revoked and self.output_revoked)

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


class PGSourceConnectionEvidenceTest(unittest.TestCase):
    def row(self, module='meta', category='tables', pid=185, kind='unclassified', query_id='12'):
        return {'module': module, 'category': category, 'kind': kind, 'pid': pid,
                'session_id': 'abcdef01.123', 'at': '2030-01-01T00:00:01+00:00', 'query_id': query_id}

    def test_source_evidence_distinguishes_health_probe_without_subtracting_it(self):
        rows = [self.row('system', 'other', 200, 'version_probe', '-123'), self.row()]
        result = ONLINE.source_connection_summary(rows, {'tables': 1, 'other': 1}, {'pid': 185})
        self.assertTrue(result['reconciled'])
        self.assertTrue(result['blocked_pid_matched'])
        self.assertEqual(result['by_module']['system']['other'], 1)
        self.assertEqual(result['observed_calls']['other'], 1)
        self.assertEqual(result['events'], rows)

    def test_unknown_or_missing_records_and_second_enumeration_cannot_pass(self):
        cases = [([self.row('unknown')], {'tables': 1}, {'pid': 185}),
                 ([], {'tables': 1}, {'pid': 185}),
                 ([self.row(pid=999)], {'tables': 1}, {'pid': 185}),
                 ([self.row(), self.row(pid=999)], {'tables': 2}, {'pid': 185})]
        for rows, delta, blocked in cases:
            with self.subTest(rows=rows):
                result = ONLINE.source_connection_summary(rows, delta, blocked)
                self.assertFalse(result['verified'])

    def test_rejects_raw_fields_truncation_and_untrusted_identifiers(self):
        for row in [dict(self.row(), query='secret'), dict(self.row(), module='private token'),
                    dict(self.row(), module=[]), dict(self.row(), kind={}), dict(self.row(), at=None),
                    dict(self.row(), pid=True), dict(self.row(), session_id='secret/../path'),
                    dict(self.row(), query_id='private query')]:
            with self.subTest(row=row), self.assertRaises(ONLINE.SuiteError):
                ONLINE.source_connection_summary([row], {'tables': 1}, None)
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.source_connection_summary([self.row()] * 257, {'tables': 257}, None)

    def test_zero_source_repeat_is_distinct_from_missing_query_snapshot(self):
        result = ONLINE.source_connection_summary([], {}, None)
        self.assertTrue(result['verified'])
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.source_connection_summary([], {'other': -1}, None)

    def test_empty_protocol_is_retained_separately_and_zero_query_id_is_not_an_exemption(self):
        ping = self.row('system', 'protocol_empty', 299, 'empty_query', '0')
        version = self.row('system', 'other', 299, 'version_probe', '-123')
        result = ONLINE.source_connection_summary([ping, version], {'other': 1}, None)
        self.assertTrue(result['verified'])
        self.assertEqual(result['events'], [ping, version])
        self.assertEqual(result['protocol_empty_calls'], {'system': 1})
        self.assertEqual(result['observed_calls']['other'], 1)
        self.assertEqual(result['record_count'], 2)
        unknown_zero = self.row('system', 'other', 299, 'unclassified', '0')
        result = ONLINE.source_connection_summary([unknown_zero, version], {'other': 1}, None)
        self.assertFalse(result['verified'])
        self.assertEqual(result['observed_calls']['other'], 2)
        self.assertEqual(result['protocol_empty_calls'], {})
        result = ONLINE.source_connection_summary([dict(ping, module='unknown')], {}, None)
        self.assertFalse(result['verified'])

    def test_empty_query_classification_cannot_hide_sql_category_or_unknown_kind(self):
        for row in (self.row('system', 'other', kind='empty_query', query_id='0'),
                    self.row('system', 'protocol_empty', kind='unclassified', query_id='0')):
            with self.subTest(row=row), self.assertRaises(ONLINE.SuiteError):
                ONLINE.source_connection_summary([row], {}, None)


class PGSourceConnectionObservationTest(unittest.TestCase):
    def setUp(self):
        self.barrier = ONLINE.PGCollectionReadBarrier()
        self.env = patch.dict(ONLINE.os.environ, {'ADDP_ONLINE_PG_SOURCE_PREFIX': 'pg_auth_0123456789abcdef'})
        self.env.start()
        self.addCleanup(self.env.stop)

    def test_checkpoint_requires_owned_complete_log_and_preserves_byte_offset(self):
        base = {'path': 'pg_source_private/statements.json', 'size': 120, 'complete': True}
        for value in (dict(base, path='/private/secret'), dict(base, size=True), dict(base, size=-1),
                      dict(base, complete=False)):
            with self.subTest(value=value), patch.object(self.barrier, 'query', return_value=value), self.assertRaises(ONLINE.SuiteError):
                self.barrier.begin_observation()
            self.assertIsNone(self.barrier.log_offset)
        with patch.object(self.barrier, 'query', return_value=base) as query:
            self.barrier.begin_observation()
            self.assertEqual(self.barrier.log_offset, 120)
            self.assertIn('pg_read_binary_file', query.call_args.args[0])
        with patch.dict(ONLINE.os.environ, {'ADDP_ONLINE_PG_SOURCE_PREFIX': 'foreign'}), patch.object(self.barrier, 'query') as query:
            with self.assertRaises(ONLINE.SuiteError): self.barrier.begin_observation()
            query.assert_not_called()

    def test_completed_projection_waits_for_collector_and_advances_only_complete_window(self):
        self.barrier.log_offset = 120
        empty = {'bytes': 0, 'complete': True, 'count': 0, 'events': []}
        row = PGSourceConnectionEvidenceTest().row()
        ready = {'bytes': 440, 'complete': True, 'count': 1, 'events': [row]}
        with patch.object(self.barrier, 'query', side_effect=[empty, ready]) as query, \
             patch.object(ONLINE.time, 'sleep') as sleep, patch.object(ONLINE.time, 'monotonic', return_value=0):
            evidence = self.barrier.connection_observation({'calls': {}}, {'calls': {'tables': 1}}, {'pid': 185})
        self.assertTrue(evidence['verified'])
        self.assertEqual(evidence['events'], [row])
        self.assertEqual(self.barrier.log_offset, 560)
        self.assertEqual(query.call_count, 2)
        sleep.assert_called_once_with(.05)
        sql = query.call_args.args[0]
        self.assertIn("j->>'user'='metric_reader'", sql)
        self.assertIn("j->>'dbname'='metric_fixture'", sql)
        self.assertIn('(statement|execute [^:]+)', sql)
        self.assertIn('LIMIT 257', sql)
        self.assertNotIn('parse [', sql)
        self.assertNotIn('message', json.dumps(evidence))

    def test_incomplete_oversized_or_unexpected_output_cannot_advance_cursor(self):
        base = {'bytes': 100, 'complete': True, 'count': 0, 'events': []}
        for value in (dict(base, bytes=262145), dict(base, count=1), dict(base, complete=False),
                      dict(base, complete=1), dict(base, bytes=True), dict(base, raw='private SQL'),
                      dict(base, count=257, events=[PGSourceConnectionEvidenceTest().row()] * 257)):
            self.barrier.log_offset = 120
            with self.subTest(value=value), patch.object(self.barrier, 'query', return_value=value), \
                 patch.object(ONLINE.time, 'monotonic', side_effect=[0, 3]), self.assertRaises(ONLINE.SuiteError):
                self.barrier.connection_observation({'calls': {}}, {'calls': {}})
            self.assertEqual(self.barrier.log_offset, 120)

    def test_missing_events_keep_counter_mismatch_visible_instead_of_returning_success(self):
        self.barrier.log_offset = 120
        sample = {'bytes': 0, 'complete': True, 'count': 0, 'events': []}
        with patch.object(self.barrier, 'query', return_value=sample), \
             patch.object(ONLINE.time, 'monotonic', side_effect=[0, 3]):
            evidence = self.barrier.connection_observation({'calls': {}}, {'calls': {'other': 1}})
        self.assertFalse(evidence['verified'])
        self.assertFalse(evidence['reconciled'])
        self.assertEqual(evidence['observed_calls']['other'], 0)


class PGCollectionBarrierTest(unittest.TestCase):
    def setUp(self):
        self.barrier = ONLINE.PGCollectionReadBarrier()

    def test_refuses_personal_context_before_any_docker_operation(self):
        with patch.dict(ONLINE.os.environ, {}, clear=True), patch.object(ONLINE.subprocess, 'run') as run:
            with self.assertRaises(ONLINE.SuiteError): self.barrier.open()
            run.assert_not_called()

    def test_source_query_is_container_bound_read_only_bounded_and_never_leaks_transport_text(self):
        self.barrier.container = 'a' * 64
        with patch.object(ONLINE.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout='{"value":1}')) as run:
            self.assertEqual(self.barrier.query('SELECT 1;'), {'value': 1})
            args, options = run.call_args
            self.assertIn(self.barrier.container, args[0])
            self.assertIn('metric_fixture', args[0])
            self.assertIn('default_transaction_read_only=on', options['env']['PGOPTIONS'])
            self.assertEqual(options['timeout'], 6)
            self.assertEqual(options['stderr'], ONLINE.subprocess.DEVNULL)
        with patch.object(ONLINE.subprocess, 'run', side_effect=ONLINE.subprocess.TimeoutExpired('private details', 6)):
            with self.assertRaisesRegex(ONLINE.SuiteError, '^source barrier query transport failed$'):
                self.barrier.query('SELECT 1;')

    def test_foreign_container_never_creates_schema_or_opens_lock(self):
        env = {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_TEST': '1', 'ADDP_ONLINE_HOST': '1',
               'ADDP_ONLINE_HOSTED': '1', 'POSTGRES_DB': 'addp_online', 'ONLINE_SUITE': ONLINE.SUITE}
        owner = {'Id': 'a' * 64, 'Config': {'Labels': {'com.addp.online-fixture': 'personal'},
                 'Env': ['POSTGRES_DB=metric_fixture']}, 'State': {'Running': True}, 'NetworkSettings': {'Ports': {}}}
        with patch.dict(ONLINE.os.environ, env, clear=True), patch.object(ONLINE.sys, 'platform', 'linux'), \
             patch.object(ONLINE.Path, 'exists', return_value=False), \
             patch.object(ONLINE.subprocess, 'run', return_value=SimpleNamespace(stdout=json.dumps(owner))), \
             patch.object(self.barrier, 'query') as query, patch.object(ONLINE.subprocess, 'Popen') as popen:
            with self.assertRaises(ONLINE.SuiteError): self.barrier.open()
            query.assert_not_called()
            popen.assert_not_called()

    def test_partial_source_creation_is_owned_before_failure_and_cleanup_is_precise(self):
        env = {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_TEST': '1', 'ADDP_ONLINE_HOST': '1',
               'ADDP_ONLINE_HOSTED': '1', 'POSTGRES_DB': 'addp_online', 'ONLINE_SUITE': ONLINE.SUITE}
        owner = {'Id': 'a' * 64, 'Config': {'Labels': {'com.addp.online-fixture': 'metric'},
                 'Env': ['POSTGRES_DB=metric_fixture']}, 'State': {'Running': True}, 'NetworkSettings': {
                 'Ports': {'5432/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '55433'}]}}}
        created, statements = [], []
        def query(sql, **kwargs):
            statements.append(sql)
            if 'CREATE SCHEMA' in sql:
                self.assertTrue(self.barrier.schema_owned)
                created.append(True)
                raise ONLINE.SuiteError('unknown committed creation')
            if 'DROP TABLE' in sql:
                self.assertIn(self.barrier.schema + '.canary', sql)
                self.assertNotIn('CASCADE', sql)
                created.clear()
                return {'dropped': True}
            if 'to_regnamespace' in sql: return {'exists': bool(created)}
            if 'pg_stat_activity' in sql: return {'sessions': 0}
            self.fail('unexpected source cleanup query')
        with patch.dict(ONLINE.os.environ, env, clear=True), patch.object(ONLINE.sys, 'platform', 'linux'), \
             patch.object(ONLINE.Path, 'exists', return_value=False), \
             patch.object(ONLINE.subprocess, 'run', return_value=SimpleNamespace(stdout=json.dumps(owner))), \
             patch.object(self.barrier, 'query', side_effect=query):
            with self.assertRaises(ONLINE.SuiteError): self.barrier.open()
            self.barrier.close()
            self.assertTrue(self.barrier.closed and self.barrier.released)
            self.assertFalse(created)
            self.barrier.close()
            self.assertEqual(sum('DROP TABLE' in s for s in statements), 1)

    def test_ambiguous_blocked_reader_and_lock_residual_are_never_passing_observations(self):
        self.barrier.pid = 123
        self.barrier.process = Mock()
        self.barrier.process.poll.return_value = None
        with patch.object(self.barrier, 'query', return_value={'at': 'time', 'blocked': [{'pid': 1}, {'pid': 2}]}):
            with self.assertRaises(ONLINE.SuiteError): self.barrier.wait_blocked()
        self.barrier.container, self.barrier.process = 'a' * 64, None
        with patch.object(self.barrier, 'query', return_value={'sessions': 1}):
            with self.assertRaises(ONLINE.SuiteError): self.barrier.release()
        self.assertFalse(self.barrier.released)

    def test_statistics_cannot_hide_invalid_counts_and_release_has_bounded_process_cleanup(self):
        with patch.object(self.barrier, 'query', return_value={'reset_at': 'same', 'calls': {'tables': -1}}):
            with self.assertRaises(ONLINE.SuiteError): self.barrier.counts()
        process = self.barrier.process = Mock()
        process.poll.side_effect = [None, None]
        process.wait.side_effect = [ONLINE.subprocess.TimeoutExpired('owned lock', 5), 0]
        with self.assertRaises(ONLINE.subprocess.TimeoutExpired): self.barrier.release()
        process.kill.assert_called_once_with()
        process.stdin.close.assert_called_once_with()
        process.stdout.close.assert_called_once_with()
        self.assertIsNone(self.barrier.process)

    def test_source_statistics_preserve_each_other_statement_without_exposing_sql(self):
        rows = [{'query_id': '-123', 'top_level': True, 'kind': 'version_probe', 'calls': 8},
                {'query_id': '456', 'top_level': False, 'kind': 'unclassified', 'calls': 1}]
        value = {'reset_at': 'same', 'calls': {'tables': 3, 'other': 9},
                 'other_statement_count': 2, 'other_statements': rows}
        with patch.object(self.barrier, 'query', return_value=value) as query:
            self.assertEqual(self.barrier.counts(), value)
        sql = query.call_args.args[0]
        self.assertIn('AS MATERIALIZED', sql)
        self.assertIn("btrim(query) = 'SELECT version()'", sql)
        self.assertIn('queryid::text', sql)
        self.assertIn('LIMIT 128', sql)
        self.assertIn("rolname='metric_reader'", sql)
        self.assertIn('datname=current_database()', sql)
        self.assertNotIn('SELECT version()', json.dumps(value))

    def test_source_other_statement_diagnostics_reject_truncation_duplicates_and_raw_fields(self):
        row = {'query_id': '-123', 'top_level': True, 'kind': 'version_probe', 'calls': 1}
        base = {'reset_at': 'same', 'calls': {'other': 1}, 'other_statement_count': 1, 'other_statements': [row]}
        values = [dict(base, other_statement_count=2), dict(base, other_statements=[]),
                  dict(base, query='private SQL'),
                  dict(base, calls={'other': 2}), dict(base, other_statements=[row, row], other_statement_count=2, calls={'other': 2}),
                  dict(base, other_statements=[dict(row, query='private SQL')]),
                  dict(base, other_statements=[dict(row, query_id='private SQL')]),
                  dict(base, other_statements=[dict(row, kind='private SQL')]),
                  dict(base, other_statements=[dict(row, kind={})]),
                  dict(base, other_statements=[dict(row, calls=True)]),
                  dict(base, other_statements=[dict(row, top_level=1)]),
                  dict(base, other_statement_count=129, other_statements=[row] * 129)]
        for value in values:
            with self.subTest(value=value), patch.object(self.barrier, 'query', return_value=value), self.assertRaises(ONLINE.SuiteError):
                self.barrier.counts()



class PGConnectionProbeControlTest(unittest.TestCase):
    def setUp(self):
        self.context = {'principal': {'id': '91', 'type': 'user'},
            'context': {'type': 'tenant', 'tenant_id': '3'},
            'token': {'type': 'first_party_access_token'},
            'authorization': {'role_assignments': [{'role_key': 'tenant.infrastructure_administrator',
                'scope': {'type': 'tenant', 'tenant_id': '3'},
                'permissions': ['system.engine.read', 'system.engine.execute']}]}}
        self.engine = {'id': 2, 'tenant_id': 3, 'engine_type': 'postgresql', 'version': 1,
                       'connection_info': {'host': '127.0.0.1', 'port': 55433, 'database': 'metric_fixture', 'user': 'metric_reader'}}
        self.before = {'reset_at': 'same', 'calls': {'other': 9}, 'other_statement_count': 2,
            'other_statements': [{'query_id': '-123', 'top_level': True, 'kind': 'version_probe', 'calls': 8},
                                 {'query_id': '456', 'top_level': True, 'kind': 'unclassified', 'calls': 1}]}
        self.after = deepcopy(self.before)
        self.after['calls']['other'] += 1
        self.after['other_statements'][0]['calls'] += 1
        self.calls, self.response, self.fault = [], {'success': True}, None
        self.client = SimpleNamespace(token='provisioner-private-token', request=self.request)
        self.life = SimpleNamespace(tenant=3, engine=2, base='/api/v1/system/engines/2')
        self.scenario = ONLINE.MetaCollectionScenario(self.life, '31', 'probe-fixture')
        self.scenario.source_snapshot = Mock(return_value='unchanged source rules')
        self.barrier = Mock()
        self.connection_events = [PGSourceConnectionEvidenceTest().row('system', 'protocol_empty', 299, 'empty_query', '0'),
                                  PGSourceConnectionEvidenceTest().row('system', 'other', 299, 'version_probe', '-123')]
        self.barrier.connection_observation.return_value = ONLINE.source_connection_summary(self.connection_events, {'other': 1}, None)
        self.barrier.counts.side_effect = lambda: deepcopy(self.before if not self.calls else self.after)
        self.report = {'checks': [], 'principals': {'operator': '31', 'reader': '33'}, 'meta_collection': {}}

    def request(self, method, path, expected, body=None):
        if path.endswith('/auth/context'): return SimpleNamespace(payload=deepcopy(self.context))
        if path.endswith('/test'):
            self.calls.append((method, path, body))
            if self.fault == 'transport': raise ONLINE.SuiteError('unknown probe response')
            return SimpleNamespace(status=200, payload=self.response)
        self.assertEqual((method, path), ('GET', '/api/v1/system/engines/2'))
        return SimpleNamespace(payload=deepcopy(self.engine))

    def run_control(self):
        with patch.object(ONLINE, 'PGCollectionReadBarrier', return_value=self.barrier):
            self.scenario.run_probe_control(self.report, self.client)

    def test_one_formal_probe_is_a_separate_control_and_keeps_unknown_other_counted(self):
        self.run_control()
        self.assertEqual(self.calls, [('POST', '/api/v1/system/engines/2/test', None)])
        self.barrier.admit.assert_called_once_with()
        self.barrier.open.assert_not_called()
        self.barrier.close.assert_called_once_with()
        self.assertIn(self.barrier, self.scenario.source_barriers)
        self.assertEqual(self.report['checks'], ['pg_connection_probe_other_increment_identified'])
        evidence = self.report['meta_collection']['connection_probe_control']
        self.assertEqual(evidence['source_call_delta'], {'namespaces': 0, 'tables': 0, 'workspace_detection': 0, 'other': 1})
        self.assertEqual(evidence['statement_increments'], [{'query_id': '-123', 'top_level': True,
                                                            'kind': 'version_probe', 'calls': 1}])
        self.assertTrue(evidence['source_rules_unchanged'] and evidence['identity_unchanged'])
        self.assertNotIn('private-token', json.dumps(self.report))
        self.assertNotIn('connection_info', json.dumps(self.report))

    def test_probe_requires_exact_empty_ping_and_version_pair_without_discarding_records(self):
        for fault in ('duplicate_ping', 'missing_ping', 'different_pid', 'different_session', 'wrong_query_id',
                      'unclassified_zero', 'unknown_module', 'wrong_order'):
            with self.subTest(fault=fault):
                self.setUp()
                events = deepcopy(self.connection_events)
                if fault == 'duplicate_ping': events.insert(0, deepcopy(events[0]))
                elif fault == 'missing_ping': events.pop(0)
                elif fault == 'different_pid': events[0]['pid'] += 1
                elif fault == 'different_session': events[0]['session_id'] = 'abcdef01.456'
                elif fault == 'wrong_query_id': events[1]['query_id'] = '-999'
                elif fault == 'unclassified_zero': events[0].update(category='other', kind='unclassified')
                elif fault == 'unknown_module': events[0]['module'] = 'unknown'
                elif fault == 'wrong_order': events.reverse()
                self.barrier.connection_observation.return_value = ONLINE.source_connection_summary(events, {'other': 1}, None)
                with self.assertRaises(ONLINE.SuiteError): self.run_control()
                self.assertFalse(self.report['checks'])
                self.assertEqual(self.report['meta_collection']['connection_probe_control']['connection_observation']['events'], events)
                self.assertIn(self.barrier, self.scenario.source_barriers, 'failed probe must remain owned for finally cleanup')
                self.assertEqual(self.calls, [('POST', '/api/v1/system/engines/2/test', None)])

    def test_wrong_identity_or_engine_refused_before_probe_and_statistics(self):
        mutations = [lambda: self.context['context'].update(tenant_id='9'),
            lambda: self.context['principal'].update(type='service_principal'),
            lambda: self.context['principal'].update(id='33'),
            lambda: self.context['authorization']['role_assignments'][0].update(role_key='custom.operator'),
            lambda: self.context['authorization']['role_assignments'][0].update(permissions=['system.engine.read']),
            lambda: self.engine.update(id=99), lambda: self.engine.update(engine_type='mysql'),
            lambda: self.engine.update(tenant_id=9), lambda: self.engine.update(version=0),
            lambda: self.engine['connection_info'].update(database='personal')]
        for mutate in mutations:
            with self.subTest(mutate=mutate):
                self.setUp(); mutate()
                with self.assertRaises(ONLINE.SuiteError): self.run_control()
                self.assertFalse(self.calls)
                self.barrier.counts.assert_not_called()

    def test_no_call_extra_unknown_reset_and_counter_replacement_never_pass(self):
        mutations = [lambda: self.after.update(reset_at='reset'),
            lambda: self.after.update(calls={'other': 9}),
            lambda: self.after['calls'].update(other=11),
            lambda: self.after['calls'].update(tables=1),
            lambda: self.after['other_statements'][1].update(calls=2),
            lambda: self.after['other_statements'][0].update(kind='unclassified'),
            lambda: self.after['other_statements'][0].update(top_level=False),
            lambda: self.after['other_statements'].pop()]
        for mutate in mutations:
            with self.subTest(mutate=mutate):
                self.setUp(); mutate()
                with self.assertRaises(ONLINE.SuiteError): self.run_control()
                self.assertEqual(len(self.calls), 1)
                self.assertFalse(self.report['checks'])
                self.assertIn(self.barrier, self.scenario.source_barriers)
                self.assertIn('source_counter_observed', self.report['meta_collection']['connection_probe_control'])

    def test_unknown_or_failed_response_is_not_retried_and_remains_owned(self):
        for fault in ('transport', 'false', 'wrong_shape'):
            with self.subTest(fault=fault):
                self.setUp()
                if fault == 'transport': self.fault = fault
                else: self.response = {'success': False} if fault == 'false' else {'data': {'success': True}}
                with self.assertRaises(ONLINE.SuiteError): self.run_control()
                self.assertEqual(len(self.calls), 1)
                self.assertFalse(self.report['checks'])
                self.assertIn(self.barrier, self.scenario.source_barriers)
                self.assertIn('source_counter_observed', self.report['meta_collection']['connection_probe_control'])

    def test_failed_control_uses_original_cleanup_without_native_creation_or_rule_writes(self):
        self.fault = 'transport'
        with self.assertRaises(ONLINE.SuiteError): self.run_control()
        self.scenario.recover_tasks = Mock(return_value=[])
        self.scenario.cleanup()
        self.barrier.release.assert_called_once_with()
        self.barrier.close.assert_called_once_with()
        self.barrier.open.assert_not_called()
        self.assertEqual(self.calls, [('POST', '/api/v1/system/engines/2/test', None)])
        self.assertFalse(self.scenario.tasks or self.scenario.commands)

    def test_rules_identity_and_engine_drift_are_not_passing_controls(self):
        for kind in ('rules', 'identity', 'engine'):
            with self.subTest(kind=kind):
                self.setUp()
                original = self.request
                def request(method, path, expected, body=None):
                    result = original(method, path, expected, body)
                    if path.endswith('/test'):
                        if kind == 'rules': self.scenario.source_snapshot.return_value = 'changed'
                        if kind == 'identity': self.context['authorization']['authorization_version'] = '2'
                        if kind == 'engine': self.engine['version'] = 2
                    return result
                self.client.request = request
                with self.assertRaises(ONLINE.SuiteError): self.run_control()
                self.assertFalse(self.report['checks'])
                self.assertEqual(len(self.calls), 1)


if __name__ == '__main__':
    unittest.main()
