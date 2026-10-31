import importlib
from contextlib import contextmanager
from copy import deepcopy
import json
import hashlib
import io
import os
from pathlib import Path
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from types import SimpleNamespace
import unittest
import zipfile
from unittest.mock import Mock, patch

M = importlib.import_module('scripts.test.manager-filegdb-online')
P = importlib.import_module('scripts.test.manager-filegdb-physical')
OWNER = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')


def facts():
    return dict(schema_version='meta.data_item_authorization_facts/v1', engine_id='7',
        item_fingerprint='a' * 64, item_type='file', full_name=M.NAME, basis_state='complete',
        observed_at='2026-10-30T00:00:00Z',
        boundary_hash='b' * 64, boundary={'layout': 'whole', 'scope': {
            'format': 'filegdb', 'rule_version': 1, 'root': M.target(7)}})


def authorization():
    auth = object.__new__(M.Authorization)
    auth.engine_id = 7; auth.base = '/api/v1/system/engines/7'
    auth.target = lambda _: M.target(7)
    auth.operator = '9'; auth.recipient = '11'; auth.frozen = None
    auth.grants = []; auth.confirmed = set(); auth.denies = []; auth.confirmed_denies = set()
    auth.issuer = Mock()
    return auth


class FileGDBAcceptanceTest(unittest.TestCase):
    def test_failed_directory_download_never_accepts_content_or_another_denial_gate(self):
        for status in (403, 409, 503):
            for mutation in (None, 'data', 'archive', 'security'):
                with self.subTest(status=status, mutation=mutation), tempfile.TemporaryDirectory() as directory:
                    payload = {'error': 'explicit failure'}
                    if mutation == 'data': payload['data'] = 'private'
                    if mutation == 'security': payload['error_code'] = 'security_protection_required'
                    raw = b'PK-private' if mutation == 'archive' else json.dumps(payload).encode()
                    binary = Mock(); binary.request.return_value = SimpleNamespace(status=status, raw=raw, payload=payload)
                    client = SimpleNamespace(base_url='http://localhost:8000', token='unchanged', timeout=30)
                    destination = Path(directory) / 'failed.zip'
                    with patch.object(OWNER, 'GatewayClient', return_value=binary):
                        if mutation in {'data', 'archive'} or (status == 403 and mutation == 'security'):
                            with self.assertRaises(ValueError): M.directory_download(client, 'locator', {}, status, destination)
                        else:
                            self.assertEqual(M.directory_download(client, 'locator', {}, status, destination)['status'], status)
                        self.assertFalse(destination.exists())

    def test_complete_directory_archive_checks_exact_members_original_bytes_and_headers(self):
        for mutation in (None, 'missing', 'outside', 'duplicate', 'wrong_bytes', 'wrong_length'):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as directory:
                data = io.BytesIO()
                with zipfile.ZipFile(data, 'w') as archive:
                    archive.writestr(M.NAME + '/', b'')
                    if mutation != 'missing':
                        archive.writestr(M.NAME + '/a.gdbtable', b'changed' if mutation == 'wrong_bytes' else b'original')
                    if mutation == 'outside': archive.writestr('../outside', b'external')
                    if mutation == 'duplicate': archive.writestr(M.NAME + '/a.gdbtable', b'original')
                raw = data.getvalue()
                binary = Mock(); binary.request.return_value = SimpleNamespace(status=200, raw=raw,
                    headers={'Content-Type': 'application/zip', 'Content-Disposition': 'attachment; filename=authorization.gdb.zip',
                             'Content-Length': str(len(raw) + (1 if mutation == 'wrong_length' else 0))})
                client = SimpleNamespace(base_url='http://localhost:8000', token='unchanged', timeout=30)
                destination = Path(directory) / 'complete.zip'
                with patch.object(OWNER, 'GatewayClient', return_value=binary):
                    if mutation:
                        with self.assertRaises(ValueError):
                            M.directory_download(client, 'locator', {'a.gdbtable':hashlib.sha256(b'original').hexdigest()}, 200, destination)
                        self.assertFalse(destination.exists())
                    else:
                        result = M.directory_download(client, 'locator', {'a.gdbtable':hashlib.sha256(b'original').hexdigest()}, 200, destination)
                        self.assertEqual(result['sha256'],hashlib.sha256(destination.read_bytes()).hexdigest())
                        self.assertEqual(result['status'],200)

    def test_archive_handle_observation_is_exact_manager_and_checkout_owned(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); repository=root/'repository'; repository.mkdir()
            (repository/'.dev-pids').mkdir(); (repository/'.dev-pids/manager.pid').write_text('123')
            (repository/'.dev-bins').mkdir(); binary=repository/'.dev-bins/addp-manager'; binary.write_bytes(b'fixture')
            proc=root/'proc'; process=proc/'123'; process.mkdir(parents=True); (process/'fd').mkdir()
            (process/'cwd').symlink_to(repository); (process/'exe').symlink_to(binary)
            descriptor=process/'fd/1'; descriptor.symlink_to('/tmp/addp-manager-filegdb-download-owned.zip (deleted)')
            with patch.object(P,'owned_root',return_value=(root,root)):
                self.assertEqual(P.archive_state(repository,{},proc),{'pid':123,'open_archives':1})
                descriptor.unlink()
                self.assertEqual(P.archive_state(repository,{},proc),{'pid':123,'open_archives':0})
                (process/'cwd').unlink(); (process/'cwd').symlink_to(root)
                with self.assertRaisesRegex(ValueError,'another checkout'): P.archive_state(repository,{},proc)

    def test_raw_members_require_exact_source_bytes_ranges_and_denial_without_content(self):
        for mutation in (None, 'wrong_bytes', 'wrong_range', 'leaked_denial', 'security_denial'):
            with self.subTest(mutation=mutation):
                binary = Mock()
                def response(method, path, statuses, headers=None):
                    if statuses == (403,):
                        payload = {'error': 'denied'}
                        if mutation == 'leaked_denial': payload['data'] = 'private'
                        if mutation == 'security_denial': payload['error_code'] = 'security_protection_required'
                        return SimpleNamespace(status=403, payload=payload, raw=json.dumps(payload).encode())
                    raw = b'abcd-private-member'
                    if headers: raw = b'bad!' if mutation == 'wrong_range' else raw[:4]
                    elif mutation == 'wrong_bytes': raw = b'other-source'
                    return SimpleNamespace(status=statuses[0], payload={}, raw=raw)
                binary.request.side_effect = response
                user = SimpleNamespace(base_url='http://localhost:8000', token='unchanged', timeout=30)
                member = dict(name='a00000001.gdbtable', sha256=M.hashlib.sha256(b'abcd-private-member').hexdigest(), item_id=17)
                with patch.object(OWNER, 'GatewayClient', return_value=binary) as factory:
                    if mutation is None:
                        allowed = M.raw_member(user, 'locator', 7, member, 200)
                        denied = M.raw_member(user, 'locator', 7, member, 403)
                        self.assertEqual([r['range_status'] for r in allowed], [206, 206])
                        self.assertEqual([r['range_status'] for r in denied], [403, 403])
                        factory.assert_called_with(user.base_url, user.token, user.timeout)
                        paths = [call.args[1] for call in binary.request.call_args_list]
                        self.assertTrue(any('/storage-stream?' in path for path in paths))
                        self.assertTrue(any('/storage-assets/7/items/17/' in path for path in paths))
                    else:
                        with self.assertRaises(ValueError):
                            M.raw_member(user, 'locator', 7, member, 403 if mutation.endswith('denial') else 200)

    def test_real_http_item_array_is_explicit_and_other_endpoints_remain_object_typed(self):
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers()
                self.wfile.write(json.dumps([{'id': 17, 'item_type': 'file', 'full_name': M.NAME,
                    'fingerprint': 'a' * 64}]).encode())
            def log_message(self, *args): pass
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever); thread.start()
        try:
            client = M.API.GatewayClient(f'http://127.0.0.1:{server.server_port}', 'synthetic', 3)
            with self.assertRaisesRegex(M.API.SuiteError, 'JSON object'):
                client.request('GET', '/api/v1/meta/engines/7/items', (200,))
            self.assertEqual(M.find_item(client, 7)['id'], 17)
            # An unrelated object endpoint cannot silently accept a list.
            with self.assertRaisesRegex(M.API.SuiteError, 'JSON object'):
                client.request('GET', '/api/v1/system/auth/context', (200,))
        finally:
            server.shutdown(); server.server_close(); thread.join()

    def test_explicit_manager_case_keeps_all_previous_cases(self):
        for case in ('full', 'native-raster-media', 'native-table-orc', 'kml-export', 'filegdb-authorization'):
            self.assertEqual(OWNER.manager_case({'ADDP_ONLINE_MANAGER_CASE': case}), case)
        with self.assertRaises(OWNER.SuiteError):
            OWNER.manager_case({'ADDP_ONLINE_MANAGER_CASE': 'arbitrary'})

    def test_developer_environment_cannot_inject_faults_or_mounts(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                M.guard(Path(directory), {})
            with patch.dict(os.environ, {}, clear=True), self.assertRaises(ValueError):
                P.owned_root()

    def test_facts_require_exact_whole_scope_and_stable_hash(self):
        for field, changed in [('basis_state', 'needs_refresh'), ('engine_id', '8'),
                               ('item_type', 'directory'), ('boundary_hash', 'c' * 64),
                               ('boundary', {'layout': 'single'})]:
            with self.subTest(field=field):
                auth = authorization(); auth.frozen = facts()
                bad = facts(); bad[field] = changed
                auth.issuer.request.return_value = SimpleNamespace(payload=bad)
                with self.assertRaises(ValueError):
                    auth.facts({'fingerprint': 'a' * 64})
        auth = authorization(); auth.issuer.request.return_value = SimpleNamespace(payload=facts())
        self.assertEqual(auth.facts({'fingerprint': 'a' * 64}), facts())

    def test_grant_registers_unknown_write_before_send_and_cleanup_only_owns_that_id(self):
        auth = authorization()
        auth.facts = Mock(return_value=facts())
        auth.issuer.request.side_effect = TimeoutError('uncertain outcome')
        permission = dict(source='explicit', version='1')
        with patch.object(M.REGISTRATION, 'ensure_direct_permission', return_value=permission):
            with self.assertRaises(TimeoutError):
                auth.grant({'fingerprint': 'a' * 64})
        self.assertEqual(len(auth.grants), 1)
        command = auth.grants[0]
        auth.issuer.request.side_effect = None
        auth.revoke(command, cleanup=True)
        self.assertEqual(auth.grants, [])
        self.assertEqual(auth.issuer.request.call_args.args[1], auth.base + '/access_grants/' + command + '/revoke')
        self.assertEqual(auth.issuer.request.call_args.args[2], (200, 404))
        with self.assertRaises(M.FILES.SuiteError):
            auth.revoke('foreign')

    def test_grant_cannot_accept_changed_scope_or_recipient_receipt(self):
        for field in ('scope', 'recipient_id', 'boundary_hash'):
            auth = authorization(); auth.facts = Mock(return_value=facts())
            def response(method, path, statuses, body):
                row = dict(body, scope=facts()['boundary']['scope'], boundary_hash='b' * 64,
                           operator_principal_id='9', revocation=None)
                row[field] = 'wrong'
                return SimpleNamespace(payload=row)
            auth.issuer.request.side_effect = response
            with patch.object(M.REGISTRATION, 'ensure_direct_permission', return_value=dict(source='explicit', version='1')):
                with self.assertRaises(ValueError): auth.grant({'fingerprint': 'a' * 64})
            self.assertEqual(len(auth.grants), 1)
            self.assertFalse(auth.confirmed)

    def test_ungranted_account_is_denied_while_original_reader_stays_allowed(self):
        self.isolation_flow()

    def test_isolation_rejects_shared_identity_credentials_grant_or_wrong_denial(self):
        for mutation in ('same_user', 'issuer_user', 'same_token', 'issuer_token', 'has_grant',
                         'has_deny', 'leaked_read', 'security_not_ready', 'grant_changed', 'relation_created'):
            with self.subTest(mutation=mutation), self.assertRaises((ValueError, M.API.SuiteError)):
                self.isolation_flow(mutation)

    def isolation_flow(self, mutation=None):
        before = {'data': [{'grant_count': 1, 'inspection': {'observed_at': '2026-10-30T10:00:00Z',
            'reason': 'grant', 'sources': [{'request_id': 'original'}], 'denies': []}}], 'total': 1}
        after = deepcopy(before)
        if mutation == 'grant_changed': after['data'][0]['inspection']['sources'][0]['request_id'] = 'new'
        zero = {'data': [], 'total': 0}
        outsider_before = {'data': [{'grant_count': 1}], 'total': 1} if mutation == 'has_grant' else zero
        if mutation == 'has_deny': outsider_before = {'data': [{'grant_count': 0, 'denies': ['deny']}], 'total': 1}
        outsider_after = {'data': [{'grant_count': 1}], 'total': 1} if mutation == 'relation_created' else zero
        auth = Mock(recipient='11', operator='9'); auth.issuer.token = 'issuer'
        auth.current.side_effect = [before, outsider_before, after, outsider_after]
        auth.history.return_value = {'request_id': 'original', 'revocation': None}
        reader = Mock(token='reader'); outsider = Mock(token='outsider')
        outsider_id = '11' if mutation == 'same_user' else ('9' if mutation == 'issuer_user' else '12')
        if mutation == 'same_token': outsider.token = reader.token
        if mutation == 'issuer_token': outsider.token = auth.issuer.token
        def read_response(method, path, statuses):
            child = 'initial_points' if 'initial_points' in path else 'added_points'
            rows = 2 if child == 'initial_points' else 1
            return SimpleNamespace(status=200, payload={'data': {'total': rows, 'rows': [
                {'label': child + '_' + str(i), 'value': i + 1} for i in range(rows)]}})
        reader.request.side_effect = read_response
        if mutation == 'leaked_read': outsider.request.side_effect = read_response
        else:
            outsider.request.return_value = SimpleNamespace(status=403, payload={'error': 'denied', **(
                {'error_code': 'security_protection_required'} if mutation == 'security_not_ready' else {})})
        report = {'tenant_id': 2}
        with patch.object(M.FILES, 'exact_user_principal', return_value=outsider_id) as identity:
            evidence = M.verify_account_isolation(auth, reader, outsider, outsider_id, 'locator',
                {'request_id': 'original'}, [('initial_points', 2), ('added_points', 1)], report)
        self.assertEqual([r['outsider_denied']['status'] for r in evidence['reads']], [403, 403])
        self.assertEqual([r['allowed_after']['rows'] for r in evidence['reads']], [2, 1])
        self.assertTrue(evidence['same_grant']); self.assertTrue(evidence['same_credentials'])
        identity.assert_called_once_with(outsider, 2, M.OUTSIDER_PERMISSIONS)
        self.assertEqual(auth.current.call_args_list[1].kwargs, {'recipient': '12'})
        self.assertEqual(report['account_isolation']['principal_id'], '12')
        auth.grant.assert_not_called(); auth.revoke.assert_not_called(); auth.deny.assert_not_called()

    def test_deny_priority_and_release_keep_the_same_grant_and_both_child_reads(self):
        self.deny_flow()

    def test_deny_cannot_be_ignored_revoke_replace_grant_or_leak_content(self):
        for mutation in ('ignored', 'grant_changed', 'revoked', 'not_released', 'new_grant', 'leak', 'security_not_ready'):
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                self.deny_flow(mutation)

    def deny_flow(self, mutation=None):
        before = {'data': [{'grant_count': 1, 'inspection': {'observed_at': '2026-10-30T08:00:00Z',
            'sources': [{'request_id': 'original'}], 'reason': 'grant', 'rule_covered': True, 'denies': []}}], 'total': 1}
        during = deepcopy(before)
        during['data'][0]['inspection'].update(reason='explicit_deny', rule_covered=False, denies=[{
            'deny_id': 'denial', 'recipient_type': 'user', 'recipient_id': '11',
            'expiry_mode': 'until_revoked', 'expires_at': None}])
        after = deepcopy(before)
        if mutation == 'ignored': during = deepcopy(before)
        if mutation == 'grant_changed': during['data'][0]['inspection']['sources'][0]['request_id'] = 'other'
        if mutation == 'not_released': after = deepcopy(during)
        if mutation == 'new_grant': after['data'][0]['inspection']['sources'][0]['request_id'] = 'replacement'
        history = {'request_id': 'original', 'revocation': None}
        denied_history = dict(history, revocation={'reason': 'secret withdrawal'}) if mutation == 'revoked' else history
        auth = Mock(recipient='11')
        auth.current.side_effect = [before, during, after]
        auth.history.side_effect = [history, denied_history, history]
        auth.deny.return_value = 'denial'
        client = Mock(token='unchanged-credential')
        def response(method, path, statuses):
            child = 'initial_points' if 'initial_points' in path else 'added_points'
            if statuses == (403,):
                payload = {'data': {'rows': []}} if mutation == 'leak' else {'error': 'denied'}
                if mutation == 'security_not_ready': payload['error_code'] = 'security_protection_required'
                return SimpleNamespace(status=403, payload=payload)
            rows = 2 if child == 'initial_points' else 1
            return SimpleNamespace(status=200, payload={'data': {'total': rows, 'rows': [
                {'label': child + '_' + str(i), 'value': i + 1} for i in range(rows)]}})
        client.request.side_effect = response
        report = {'tenant_id': 2, 'engine_id': 7, 'raw_member': {'name': 'gdb'}}
        download = Mock(return_value={'status': 200})
        with patch.object(M.FILES, 'exact_user_principal', return_value='11'), patch.object(M, 'raw_member', return_value=[]) as raw:
            result = M.verify_deny(auth, client, {'full_name': M.NAME}, 'locator', {'request_id': 'original'}, report, download)
        self.assertEqual([call.args[-1] for call in raw.call_args_list], [403, 200])
        self.assertEqual([call.args[1:] for call in download.call_args_list], [(403, 'deny'), (200, 'deny-release')])
        self.assertEqual([r['status'] for r in result['denied_reads']], [403, 403])
        self.assertEqual([r['rows'] for r in result['restored_reads']], [2, 1])
        self.assertEqual(result['history_before'], result['history_after'])
        self.assertTrue(result['same_grant']); self.assertTrue(result['same_credential'])
        auth.deny.assert_called_once(); auth.release.assert_called_once_with('denial')
        auth.grant.assert_not_called(); auth.revoke.assert_not_called()

    def test_unknown_deny_and_grant_outcomes_are_cleaned_before_permission_is_closed(self):
        auth = authorization(); auth.frozen = facts()
        auth.issuer.request.side_effect = TimeoutError('unknown Deny response')
        with self.assertRaises(TimeoutError):
            auth.deny({'full_name': M.NAME, 'fingerprint': 'a' * 64})
        command, = auth.denies
        auth.grants = ['owned-grant']
        auth.issuer.request.side_effect = None
        auth.issuer.request.return_value = SimpleNamespace(status=404, payload={'error': 'not found'})
        auth.current = Mock(return_value={'total': 0, 'data': []})
        with patch.object(M.REGISTRATION, 'set_direct_permission', return_value={'state': 'denied'}):
            auth.close()
        self.assertEqual(auth.denies, []); self.assertEqual(auth.grants, [])
        paths = [call.args[1] for call in auth.issuer.request.call_args_list]
        self.assertEqual(paths[-2:], [auth.base + '/access_denies/' + command + '/release',
            auth.base + '/access_grants/owned-grant/revoke'])

    def test_environment_failure_is_not_counted_as_permission_denial_or_partial_content(self):
        client = Mock()
        client.request.return_value = SimpleNamespace(status=503, payload={'error': 'unavailable'})
        self.assertEqual(M.preview(client, 'exact', 'added_points', (500, 502, 503, 504))['status'], 503)
        self.assertEqual(client.request.call_args.args[2], (500, 502, 503, 504))
        client.request.return_value = SimpleNamespace(status=503, payload={'data': {'rows': []}})
        with self.assertRaises(ValueError): M.preview(client, 'exact', 'added_points', 503)
        client.request.return_value = SimpleNamespace(status=200, payload={'data': {'total': 2, 'rows': []}})
        with self.assertRaises(ValueError): M.preview(client, 'exact', 'added_points', 200, 2)

    def test_real_read_child_sigkill_leaves_no_marker_and_has_no_issuer_secret(self):
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200); self.send_header('Content-Type', 'application/json'); self.end_headers()
                self.wfile.write(b'{"data":{"total":1,"rows":[{"value":1,"label":"added_points_0"}]}}')
            def log_message(self, *args): pass
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever); thread.start()
        try:
            with tempfile.TemporaryDirectory() as directory:
                env = dict(os.environ, GATEWAY_URL=f'http://127.0.0.1:{server.server_port}',
                    ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='synthetic-test-token', ADDP_ONLINE_SECRET_DIR=directory,
                    ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='must-not-reach-child')
                real = M.subprocess.Popen
                def launch(*args, **kwargs):
                    self.assertNotIn('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN', kwargs['env'])
                    return real(*args, **kwargs)
                with patch.object(M.subprocess, 'Popen', side_effect=launch):
                    evidence = M.kill_read_child(env, 'addp://engine/7/path/authorization.gdb', Path(directory))
                self.assertTrue(evidence['killed']); self.assertEqual(evidence['rows'], 1)
                self.assertFalse((Path(directory) / 'filegdb-child-ready.json').exists())
        finally:
            server.shutdown(); server.server_close(); thread.join()

    def test_outage_compares_rules_while_preserving_each_query_observation(self):
        self.outage_observations()

    def test_outage_still_rejects_changed_grant_content_and_invalid_observation(self):
        changes = [
            lambda row: row.update(expires_at='2026-11-01T00:00:00Z'),
            lambda row: row.update(grant_count=2),
            lambda row: row['inspection']['sources'][0].update(request_id='replacement'),
            lambda row: row['inspection'].update(denies=[{'deny_id': 'new-deny'}]),
            lambda row: row['catalog_path'].update(engine_id=8),
            lambda row: row['inspection'].pop('observed_at'),
            lambda row: row['inspection'].update(observed_at='invalid'),
        ]
        for change in changes:
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.outage_observations(change)

    def outage_observations(self, change=None):
        baseline = {'data': [{'catalog_path': M.target(7), 'grant_count': 1, 'expires_at': None,
            'inspection': {'observed_at': '2026-10-30T06:00:00+00:00', 'reason': 'grant',
                'rule_covered': True, 'sources': [{'request_id': 'original'}], 'denies': []}}],
            'total': 1, 'page': 1, 'page_size': 20, 'total_pages': 1}
        observations = [deepcopy(baseline) for _ in range(5)]
        for index, current in enumerate(observations):
            current['data'][0]['inspection']['observed_at'] = f'2026-10-30T06:00:0{index}+00:00'
        if change:
            change(observations[1]['data'][0])
        original = deepcopy(observations)
        before = dict(instance_id='before', git_commit='sha', build_id='build', source_fingerprint='fp')
        after = dict(before, instance_id='after')
        health = Mock(); health.request.side_effect = [SimpleNamespace(payload=value) for value in (before, after, after)]
        @contextmanager
        def interruption(*args):
            yield {'module': args[-1]}
        current = Mock(side_effect=observations)
        download = Mock(side_effect=lambda actor, status, phase: {'status': status if isinstance(status, int) else 503})
        with patch.object(M.API, 'GatewayClient', return_value=health), \
                patch.object(M.API, 'wait_manager_read_boundary'), patch.object(P, 'interruption', interruption):
            evidence = P.verify_outages(Path('/unused'), {'MANAGER_URL': 'http://unused',
                'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '7'}, Mock(), 'locator', current,
                lambda: {'a.gdbtable': 'same-sha'}, Mock(return_value={'status': 503}), download)
        self.assertEqual(observations, original)  # Raw timestamps remain available for independent review.
        for index, outage in enumerate(evidence):
            self.assertTrue(outage['same_grant']); self.assertTrue(outage['source_unchanged'])
            self.assertEqual(outage['current_before'], original[0])
            self.assertEqual(outage['current_during'], original[1 + 2 * index])
            self.assertEqual(outage['current_after'], original[2 + 2 * index])
            self.assertEqual(outage['failed_archive']['status'], 503)
            self.assertEqual(outage['recovered_archive']['status'], 200)
        self.assertEqual([call.args[2] for call in download.call_args_list],
                         ['fault-manager', 'recovered-manager', 'fault-nfs', 'recovered-nfs'])

    def test_nfs_daemon_stop_is_not_accepted_when_actual_mount_is_still_readable(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); (root / M.NAME).mkdir(); (root / M.NAME / 'a.gdbtable').write_bytes(b'source')
            (root / 'filegdb-nfs-mount').write_text(str(root))
            calls = []
            def run(args, **kwargs):
                calls.append(args)
                return SimpleNamespace(returncode=0)
            with patch.object(P, 'owned_root', return_value=(root, root)), patch.object(P.subprocess, 'run', side_effect=run):
                with self.assertRaisesRegex(ValueError, 'data path did not fail'):
                    with P.interruption(root, {}, 'nfs'): self.fail('false outage accepted')
            self.assertEqual(calls[-1], ['sudo', 'systemctl', 'start', 'nfs-kernel-server.service'])

    def test_manager_lifecycle_recovers_on_assertion_failure_using_existing_exact_scripts(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); calls = []
            def run(args, **kwargs):
                calls.append(args)
                self.assertNotIn('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN', kwargs['env'])
                return SimpleNamespace(returncode=0)
            with patch.object(P, 'owned_root', return_value=(root, root)), patch.object(P.subprocess, 'run', side_effect=run), \
                    patch.object(P.socket, 'create_connection', side_effect=ConnectionRefusedError):
                with self.assertRaisesRegex(ValueError, 'probe failure'):
                    with P.interruption(root, {'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'secret'}, 'manager'):
                        raise ValueError('probe failure')
            self.assertIn('--force', calls[0]); self.assertIn('--exact-process', calls[-1])
            self.assertEqual(calls[-1][-1], '-manager')

    def test_physical_snapshot_and_cleanup_preserve_unowned_sibling(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); (root / M.NAME).mkdir(); (root / M.NAME / 'a.gdbtable').write_bytes(b'exact')
            (root / '.filegdb-acceptance-owner').write_text('run'); (root / 'keep').write_text('foreign')
            with patch.object(P, 'owned_root', return_value=(root, root)), patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='run'):
                before = P.native('snapshot', root)
                self.assertEqual(list(before), ['a.gdbtable'])
                self.assertTrue(P.native('delete', root)['source_removed'])
            self.assertEqual((root / 'keep').read_text(), 'foreign')

    def test_cleanup_must_prove_zero_before_closing_permission(self):
        auth = authorization(); auth.frozen = facts(); auth.cleanup = Mock(); auth.current = Mock(return_value={'total': 1})
        with patch.object(M.REGISTRATION, 'set_direct_permission') as permission:
            with self.assertRaisesRegex(ValueError, 'survived cleanup'): auth.close()
            permission.assert_not_called()

    def test_early_scan_failure_stays_failed_but_still_closes_authorization_and_deletes_owned_source(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            env = {'ADDP_ONLINE_ARTIFACT_DIR': directory, 'ADDP_ONLINE_SECRET_DIR': directory,
                'GATEWAY_URL': 'http://localhost:8000', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'test',
                'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'issuer', 'ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN': 'outsider',
                'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '7',
                'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_TEST_RUN_ID': 'manager-1-1'}
            auth = Mock(); auth.current.return_value = {'total': 0, 'data': []}; auth.close.return_value = None
            owner = Mock(); owner.wait_for_meta_scan.side_effect = ValueError('source scan unavailable')
            actions = []
            def physical(action, target):
                actions.append(action)
                if action == 'snapshot': raise OSError('source not readable')
                return {'source_removed': True}
            with patch.object(M, 'guard', return_value=root), patch.object(M.API, 'GatewayClient'), \
                    patch.object(M.FILES, 'exact_user_principal', side_effect=['11', '12']), \
                    patch.object(M.API, 'wait_manager_read_boundary'), patch.object(M, 'Authorization', return_value=auth), \
                    patch.object(P, 'physical', side_effect=physical):
                with self.assertRaises(ValueError): M.run(root, env, owner)
            auth.close.assert_called_once_with()
            self.assertEqual(actions, ['seed', 'snapshot', 'delete'])
            report = json.loads((root / 'manager-filegdb.json').read_text())
            self.assertEqual(report['result'], 'failed')
            self.assertEqual(report['error_type'], 'ValueError')
            self.assertEqual(report['cleanup_errors'], [{'stage': 'source_snapshot', 'error_type': 'OSError'}])


if __name__ == '__main__': unittest.main()
