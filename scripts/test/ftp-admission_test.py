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


class FTPAdmissionTests(unittest.TestCase):
    def test_pinned_image_and_secure_business_defaults(self):
        self.assertRegex(config.image(), r'^drakkan/sftpgo:v2\.7\.6@sha256:[a-f0-9]{64}$')
        facts = config.server(50000, 50009)
        self.assertEqual(facts['ftpd']['bindings'][0]['tls_mode'], 1)
        self.assertEqual(facts['ftpd']['bindings'][0]['passive_connections_security'], 0)
        self.assertTrue(facts['ftpd']['disable_active_mode'])
        for protocol in ('sftpd', 'httpd', 'webdavd'):
            self.assertEqual(facts[protocol]['bindings'][0]['port'], 0)
        self.assertEqual(config.users('reader', 'secret')['users'][0]['permissions'], {'/': ['list', 'download']})

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
