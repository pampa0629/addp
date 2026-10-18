import importlib
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

m = importlib.import_module('business.scripts.online-raster-contour-fixture')


class ContourFixtureTest(unittest.TestCase):
    def test_admission_rejects_personal_execution_before_commands(self):
        with patch.object(m, 'command') as command:
            for env in ({}, {'GITHUB_ACTIONS': 'true'}, {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux'}):
                with self.assertRaises(m.FixtureError): m.guard(env)
            command.assert_not_called()

    def test_owned_root_rejects_symlink_foreign_identity_and_arbitrary_cleanup_scope(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='contour-1-1'):
            secret = Path(directory)
            for path in ('/', '/tmp', directory):
                (secret / 'contour-root').write_text(path)
                with self.assertRaises(m.FixtureError): m.owned_root(secret)
            with tempfile.TemporaryDirectory(prefix='addp-contour-', dir='/tmp') as root_name:
                root = Path(root_name); (secret / 'contour-root').write_text(root_name)
                (root / '.addp-online-owner').write_text('foreign')
                with self.assertRaises(m.FixtureError): m.owned_root(secret)
                (root / '.addp-online-owner').write_text('contour-1-1')
                # The production admission is Linux-only; macOS resolves /tmp
                # to /private/tmp. Exercise the Linux canonical path here.
                with patch.object(Path, 'resolve', lambda path: path):
                    self.assertEqual(m.owned_root(secret), root)
                (secret / 'contour-root').unlink(); (secret / 'contour-root').symlink_to(root / '.addp-online-owner')
                with self.assertRaises(m.FixtureError): m.owned_root(secret)

    def test_worker_rejects_foreign_product_and_nondefault_entry(self):
        info = {'Config': {'Labels': {'com.addp.online-runtime': m.SUITE, 'com.addp.online-run-id': 'contour-1-1'}, 'Cmd': ['sh', 'container_entrypoint.sh']}}
        with patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='contour-1-1'):
            for key in ('label', 'entry'):
                value = json.loads(json.dumps(info))
                if key == 'label': value['Config']['Labels']['com.addp.online-run-id'] = 'other'
                else: value['Config']['Cmd'] = ['python', 'api_server.py']
                with patch.object(m, 'command', return_value=json.dumps([value])) as command:
                    with self.assertRaises(m.FixtureError): m.runtime_worker('verify', Path('/tmp/owned'), ['levels.gpkg'])
                    self.assertEqual(command.call_count, 1)

    def test_stop_rejects_broken_state_symlink_without_system_mutation(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='contour-1-1'):
            secret = Path(directory); (secret / 'contour-root').symlink_to(secret / 'missing')
            with patch.object(m, 'guard', return_value=secret), patch.object(m, 'command') as command:
                with self.assertRaises(m.FixtureError): m.main('stop')
                command.assert_not_called()

    def test_start_emits_descriptor_accepted_by_the_real_engine_registration_contract(self):
        registration = importlib.import_module('scripts.test.online-engine-registration')
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='contour-123-1'):
            base = Path(directory); secret = base / 'private'; secret.mkdir(mode=0o700)
            root = base / 'export'; root.mkdir()
            def command(args, **kwargs):
                if args[:2] == ['showmount', '-e']:
                    return f'Export list for 127.0.0.1:\n{root} 127.0.0.1'
                return ''
            with patch.object(m, 'guard', return_value=secret), patch.object(m.tempfile, 'mkdtemp', return_value=str(root)), patch.object(m, 'command', side_effect=command):
                self.assertEqual(m.main('start'), {'started': True})
            descriptor = registration.load_descriptor((secret / 'contour-nfs-engine.json').resolve())
            self.assertEqual(descriptor['engine_type'], 'nfs')
            self.assertEqual(descriptor['connection_info'], {'server': '127.0.0.1', 'export_path': str(root)})
            self.assertTrue(descriptor['description'])
