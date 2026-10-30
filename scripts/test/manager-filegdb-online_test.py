import importlib
import json
import os
from pathlib import Path
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from types import SimpleNamespace
import unittest
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
    auth.operator = '9'; auth.recipient = '11'; auth.frozen = None
    auth.grants = []; auth.confirmed = set(); auth.denies = []; auth.confirmed_denies = set()
    auth.issuer = Mock()
    return auth


class FileGDBAcceptanceTest(unittest.TestCase):
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
                'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'issuer', 'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '7',
                'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_TEST_RUN_ID': 'manager-1-1'}
            auth = Mock(); auth.current.return_value = {'total': 0}; auth.close.return_value = None
            owner = Mock(); owner.wait_for_meta_scan.side_effect = ValueError('source scan unavailable')
            actions = []
            def physical(action, target):
                actions.append(action)
                if action == 'snapshot': raise OSError('source not readable')
                return {'source_removed': True}
            with patch.object(M, 'guard', return_value=root), patch.object(M.API, 'GatewayClient'), \
                    patch.object(M.FILES, 'exact_user_principal', return_value='11'), \
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
