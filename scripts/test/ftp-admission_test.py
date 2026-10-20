import importlib.util
import json
import ftplib
import socket
import subprocess
import threading
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'business/ftp'))
import config
import admission
import prepare
import seed


class FTPAdmissionTests(unittest.TestCase):
    def test_private_configuration_preserves_identity_and_certificate(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / 'ftp/config'
            root.mkdir(parents=True)
            (root / 'server.crt').write_text('existing certificate')
            (root / 'server.key').write_text('existing private key')
            prepare.prepare(root, 'business_reader', 'strong-fixture-password', 'explicit_tls')
            self.assertEqual(root.stat().st_mode & 0o777, 0o755)
            self.assertEqual(root.parent.stat().st_mode & 0o777, 0o700)
            original = (root / 'users.json').read_bytes()
            prepare.prepare(root, 'business_reader', 'strong-fixture-password', 'plain')
            self.assertEqual((root / 'server.key').read_text(), 'existing private key')
            with self.assertRaisesRegex(ValueError, 'implicit rotation'):
                prepare.prepare(root, 'business_reader', 'changed-fixture-password', 'explicit_tls')
            self.assertEqual((root / 'users.json').read_bytes(), original)

    def test_compose_and_standard_routes(self):
        import subprocess
        result = subprocess.run(['docker', 'compose', '--env-file', str(ROOT / 'business/.env.example'),
                                 '-f', str(ROOT / 'business/docker-compose.yml'), '--profile', 'ftp',
                                 'config', '--format', 'json'], check=True, capture_output=True, text=True)
        value = json.loads(result.stdout)['services']['ftp']
        self.assertEqual(value['profiles'], ['ftp'])
        self.assertEqual(value['entrypoint'], ['sh', '/addp/ftp/entrypoint.sh'])
        bindings = [p for p in value['volumes'] if p['target'] == '/srv/sftpgo/data']
        self.assertEqual(len(bindings), 1)
        self.assertEqual(bindings[0]['type'], 'bind')
        self.assertEqual(Path(bindings[0]['source']), ROOT / 'business/ftp/data')
        self.assertTrue(bindings[0]['read_only'])
        self.assertFalse(any(p['target'] in ('/addp/samples', '/srv/sftpgo') for p in value['volumes']))
        ports = {(p['host_ip'], int(p['published']), p['target']) for p in value['ports']}
        self.assertEqual(ports, {('127.0.0.1', p, p) for p in [2121, *range(50000, 50010)]})
        self.assertEqual({p['source'] for p in value['volumes'] if p['type'] == 'volume'}, {'ftp_state'})
        for script in ('start.sh', 'stop.sh'):
            result = subprocess.run(['bash', str(ROOT / 'business/scripts' / script), '-ftp', '-postgres'],
                                    capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('-ftp 必须独立使用', result.stderr)

    def test_pinned_image_and_secure_business_defaults(self):
        self.assertRegex(config.image(), r'^drakkan/sftpgo:v2\.7\.6@sha256:[a-f0-9]{64}$')
        facts = config.server(50000, 50009)
        self.assertEqual(facts['ftpd']['bindings'][0]['tls_mode'], 1)
        self.assertEqual(facts['ftpd']['bindings'][0]['passive_connections_security'], 0)
        self.assertTrue(facts['ftpd']['disable_active_mode'])
        for protocol in ('sftpd', 'httpd', 'webdavd'):
            self.assertEqual(facts[protocol]['bindings'][0]['port'], 0)
        self.assertEqual(config.users('reader', 'secret')['users'][0]['permissions'], {'/': ['list', 'download']})

    def test_generated_data_is_ignored_and_preserves_existing_files(self):
        import subprocess
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / 'data'
            old_umask = os.umask(0o077)
            try:
                seed.write_samples(root)
            finally:
                os.umask(old_umask)
            self.assertEqual(root.stat().st_mode & 0o777, 0o755)
            examples = root / 'examples'
            self.assertEqual(examples.stat().st_mode & 0o777, 0o755)
            self.assertTrue(all(p.stat().st_mode & 0o777 == 0o644 for p in examples.iterdir()))
            self.assertEqual(len(list(examples.iterdir())), 3)
            self.assertEqual(json.loads((examples / '订单 示例.json').read_text()), seed.ROWS)
            self.assertEqual((examples / '客户 示例.parquet').read_bytes()[:4], b'PAR1')
            user_directory = root / 'user-created'
            user_directory.mkdir(mode=0o700)
            user_file = user_directory / 'private.txt'
            user_file.write_text('owned by host user')
            csv_file = examples / '客户 中文%.csv'
            csv_file.write_text('user modified')
            seed.write_samples(root)
            self.assertEqual(csv_file.read_text(), 'user modified')
            self.assertEqual(user_file.read_text(), 'owned by host user')
            self.assertEqual(user_directory.stat().st_mode & 0o777, 0o700)
            csv_file.unlink()
            csv_file.symlink_to(examples / '订单 示例.json')
            with self.assertRaisesRegex(ValueError, 'ordinary file'):
                seed.write_samples(root)
        for relative in ('business/ftp/data/nested/example.json', 'business/ftp/samples/example.parquet'):
            result = subprocess.run(['git', 'check-ignore', '--no-index', '-q', relative], cwd=ROOT)
            self.assertEqual(result.returncode, 0, relative)

    def test_host_root_and_examples_cannot_redirect_initialization(self):
        for component in ('root', 'examples'):
            with self.subTest(component=component), tempfile.TemporaryDirectory() as directory:
                parent = Path(directory)
                target = parent / 'outside'
                target.mkdir()
                root = parent / 'data'
                if component == 'root':
                    root.symlink_to(target, target_is_directory=True)
                else:
                    root.mkdir()
                    (root / 'examples').symlink_to(target, target_is_directory=True)
                with self.assertRaisesRegex(ValueError, 'symbolic link'):
                    seed.write_samples(root)
                self.assertEqual(list(target.iterdir()), [])

    def test_greeting_wait_retries_only_before_the_ftp_server_is_ready(self):
        # Reproduce a published TCP port accepting before an FTP server is ready:
        # first connection closes without a banner, next serves an actual 220.
        listener = socket.socket()
        listener.bind(('127.0.0.1', 0))
        listener.listen(2)
        listener.settimeout(3)
        def serve():
            try:
                for banner in (b'', b'220 fixture ready\r\n'):
                    connection, _ = listener.accept()
                    with connection:
                        if banner:
                            connection.sendall(banner)
            finally:
                listener.close()
        thread = threading.Thread(target=serve, daemon=True)
        thread.start()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            state = {'name': 'owned'}
            def docker(*args):
                if args[1] == 'inspect':
                    return json.dumps({'Running': True, 'Status': 'running', 'ExitCode': 0})
                return ''
            with patch.object(admission, 'run', side_effect=docker), patch.object(admission.time, 'sleep'):
                admission.wait_for_greeting(root, state, {'host': '127.0.0.1', 'port': listener.getsockname()[1]}, 'recreated')
            report = json.loads((root / 'readiness-recreated.json').read_text())
            self.assertTrue(report['ready'])
            self.assertEqual([attempt['outcome'] for attempt in report['attempts']], ['EOFError', '220'])
        thread.join(timeout=3)
        self.assertFalse(thread.is_alive())

    def test_greeting_wait_does_not_hide_server_rejection_or_exit(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            state = {'name': 'owned'}
            connection = {'host': '127.0.0.1', 'port': 2121}
            running = json.dumps({'Running': True, 'Status': 'running', 'ExitCode': 0})
            with patch.object(admission, 'run', return_value=running), patch.object(admission.ftplib, 'FTP') as ftp:
                ftp.return_value.connect.side_effect = ftplib.error_perm('530 invalid configuration')
                with self.assertRaises(ftplib.error_perm):
                    admission.wait_for_greeting(root, state, connection, 'rejected')
                self.assertEqual(ftp.return_value.connect.call_count, 1)
            stopped = json.dumps({'Running': False, 'Status': 'exited', 'ExitCode': 1})
            with patch.object(admission, 'run', return_value=stopped), patch.object(admission.ftplib, 'FTP') as ftp:
                with self.assertRaisesRegex(RuntimeError, 'exited'):
                    admission.wait_for_greeting(root, state, connection, 'exited')
                ftp.assert_not_called()

    def test_persistence_never_retries_tls_or_authentication_failure(self):
        for category in ('tls', 'authentication'):
            with self.subTest(category=category), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                admission.write(root / 'state.json', {'name': 'owned', 'args': ['docker', 'run', 'fixture']})
                admission.write(root / 'connection.json', {'host': '127.0.0.1', 'port': 2121, 'user': 'reader', 'password': 'private', 'ca': 'private.crt'})
                seed.write_samples(root / 'data')
                with patch.dict(os.environ, ADDP_FTP_ADMISSION_DIR=directory), patch.object(admission, 'run', return_value='owned'), patch.object(admission, 'wait_for_greeting'), patch.object(admission.ssl, 'create_default_context') as tls, patch.object(admission.ftplib, 'FTP_TLS') as ftp:
                    if category == 'tls':
                        tls.side_effect = admission.ssl.SSLCertVerificationError('invalid certificate')
                        expected = admission.ssl.SSLCertVerificationError
                    else:
                        ftp.return_value.__enter__.return_value.login.side_effect = ftplib.error_perm('530 rejected')
                        expected = ftplib.error_perm
                    with self.assertRaises(expected):
                        admission.persistence()
                    self.assertEqual(tls.call_count, 1)
                    self.assertFalse((root / 'business-lifecycle.json').exists())
                    if category == 'authentication':
                        self.assertEqual(ftp.return_value.__enter__.return_value.login.call_count, 1)

    def test_greeting_wait_has_a_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            running = json.dumps({'Running': True, 'Status': 'running', 'ExitCode': 0})
            with patch.object(admission, 'run', return_value=running), patch.object(admission.time, 'monotonic', side_effect=[0, 61]):
                with self.assertRaisesRegex(RuntimeError, 'timed out'):
                    admission.wait_for_greeting(Path(directory), {'name': 'owned'}, {'host': '127.0.0.1', 'port': 2121}, 'timeout', timeout=60)

    def test_command_failure_publishes_no_command_arguments_or_stderr(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, ADDP_FTP_ADMISSION_DIR=directory):
            failure = subprocess.CalledProcessError(1, ['docker', 'exec', 'private-container', 'private-user'], stderr='private-password and server details')
            admission.command_failure(failure, 'start')
            root = Path(directory)
            report = json.loads((root / 'command-failure.json').read_text())
            self.assertEqual(report, {'action': 'start', 'command': ['docker', 'exec'], 'exit_code': 1})
            private = root / 'command-failure.stderr'
            self.assertEqual(private.stat().st_mode & 0o777, 0o600)
            self.assertEqual(private.read_text(), failure.stderr)

    def test_cleanup_only_uses_owned_labels(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, ADDP_FTP_ADMISSION_DIR=directory):
            root = Path(directory)
            admission.write(root / 'state.json', {'name': 'owned', 'label': 'org.addp.ftp.owner=owned', 'volumes': []})
            calls = []
            def run(*args):
                calls.append(args)
                if args[:3] == ('docker', 'ps', '-aq'):
                    return 'owned-id' if len(calls) == 1 else ''
                return ''
            with patch.object(admission, 'run', run):
                admission.stop()
            self.assertIn(('docker', 'rm', '-f', 'owned-id'), calls)
            for call in calls:
                if '--filter' in call:
                    self.assertIn('label=org.addp.ftp.owner=owned', call)
            self.assertFalse((root / 'state.json').exists())
            self.assertEqual(json.loads((root / 'cleanup.json').read_text()), {'residuals': 0})

    def test_zero_cleanup_failure_never_reported_as_success(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, ADDP_FTP_ADMISSION_DIR=directory):
            root = Path(directory)
            admission.write(root / 'state.json', {'name': 'owned', 'label': 'org.addp.ftp.owner=owned', 'volumes': []})
            with patch.object(admission, 'run', return_value='residual'):
                with self.assertRaisesRegex(RuntimeError, 'remain'):
                    admission.stop()
            self.assertTrue((root / 'state.json').exists())
            self.assertGreater(json.loads((root / 'cleanup.json').read_text())['residuals'], 0)


if __name__ == '__main__':
    unittest.main()
