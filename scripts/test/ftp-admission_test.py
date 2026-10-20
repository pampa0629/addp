import importlib.util
import json
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
        self.assertTrue(any(p['target'] == '/addp/samples' and p.get('read_only') for p in value['volumes']))
        ports = {(p['host_ip'], int(p['published']), p['target']) for p in value['ports']}
        self.assertEqual(ports, {('127.0.0.1', p, p) for p in [2121, *range(50000, 50010)]})
        self.assertEqual({p['source'] for p in value['volumes'] if p['type'] == 'volume'}, {'ftp_data', 'ftp_state'})
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
            self.assertTrue(all(p.stat().st_mode & 0o777 == 0o644 for p in root.iterdir()))
            self.assertEqual(len(list(root.iterdir())), 3)
            self.assertEqual(json.loads((root / '订单 示例.json').read_text()), seed.ROWS)
            self.assertEqual((root / '客户 示例.parquet').read_bytes()[:4], b'PAR1')
            csv_file = root / '客户 中文%.csv'
            csv_file.write_text('user modified')
            seed.write_samples(root)
            self.assertEqual(csv_file.read_text(), 'user modified')
            csv_file.unlink()
            csv_file.symlink_to(root / '订单 示例.json')
            with self.assertRaisesRegex(ValueError, 'ordinary file'):
                seed.write_samples(root)
        for relative in ('business/ftp/data/nested/example.json', 'business/ftp/samples/example.parquet'):
            result = subprocess.run(['git', 'check-ignore', '--no-index', '-q', relative], cwd=ROOT)
            self.assertEqual(result.returncode, 0, relative)

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
