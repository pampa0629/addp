from importlib import import_module
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

FIXTURE = import_module('business.starrocks.admission')


class StarRocksAdmissionTest(unittest.TestCase):
    def test_official_version_and_multiplatform_index_are_pinned(self):
        self.assertEqual(FIXTURE.IMAGE, 'starrocks/allin1-ubuntu:3.5.21@sha256:faf5f6b17a301b276b829482af11d350004833dc9162232e1083a2aad0fa8b2a')

    def test_private_directory_rejects_repository_and_public_state(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp).resolve()
            path.chmod(0o755)
            for value in (str(path), str(FIXTURE.ROOT)):
                with patch.dict(os.environ, ADDP_STARROCKS_ADMISSION_DIR=value), self.assertRaises(RuntimeError):
                    FIXTURE.directory()

    def test_private_state_is_exclusive_under_restrictive_umask(self):
        with tempfile.TemporaryDirectory() as temp:
            target = Path(temp, 'state')
            previous = os.umask(0o077)
            try:
                FIXTURE.private(target, {'run_id': 'owned'})
            finally:
                os.umask(previous)
            self.assertEqual(target.stat().st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError):
                FIXTURE.private(target, {})

    def test_foreign_resource_is_never_deleted(self):
        with patch.object(FIXTURE, 'inspect', return_value={'Config': {'Labels': {FIXTURE.LABEL: 'foreign'}}}), patch.object(FIXTURE, 'run') as run:
            with self.assertRaises(RuntimeError):
                FIXTURE.owned('container', 'owned', {'run_id': 'expected'})
            run.assert_not_called()

    def test_cleanup_checks_every_owned_resource_and_removes_only_owned_state(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp).resolve()
            path.chmod(0o700)
            (path / 'state.json').write_text(json.dumps({'run_id': 'test', 'name': 'owned', 'image': 'owned:local', 'volumes': ['fe', 'be']}))
            with patch.dict(os.environ, ADDP_STARROCKS_ADMISSION_DIR=str(path)), patch.object(FIXTURE, 'owned', return_value={}), patch.object(FIXTURE, 'inspect', return_value=None) as inspect, patch.object(FIXTURE, 'run') as run:
                FIXTURE.stop()
                self.assertEqual(inspect.call_count, 4)
                self.assertEqual(run.call_count, 4)
                self.assertEqual(json.loads((path / 'cleanup.json').read_text()), {'result': 'passed', 'remaining_resources': 0})
                self.assertFalse((path / 'state.json').exists())

    def test_docker_failure_does_not_echo_sql_or_password(self):
        with patch.object(FIXTURE.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, 'private SQL', 'private password')):
            with self.assertRaises(RuntimeError) as error:
                FIXTURE.run('docker', 'exec', 'owned', input='private SQL')
            self.assertNotIn('private', str(error.exception))

    def test_schema_diagnostics_exclude_account_statements(self):
        source = Path(FIXTURE.__file__).read_text()
        schema = source.split('sql(name, """CREATE DATABASE', 1)[1].split("evidence=path / 'schema-initialization.json'", 1)[0]
        self.assertNotIn('PASSWORD', schema.upper())
        self.assertNotIn('CREATE USER', schema.upper())
        self.assertNotIn('password', schema)

    def test_unknown_docker_failure_is_not_absence(self):
        with patch.object(FIXTURE.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, '', 'authorization denied')), patch.object(FIXTURE, 'run'):
            with self.assertRaises(RuntimeError):
                FIXTURE.inspect('container', 'owned')


if __name__ == '__main__':
    unittest.main()
