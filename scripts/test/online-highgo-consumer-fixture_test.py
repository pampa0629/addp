from importlib import import_module
import contextlib
import io
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

FIXTURE = import_module('business.scripts.online-highgo-consumer-fixture')


class HighGoFixtureTest(unittest.TestCase):
    def test_startup_diagnostics_keep_owned_state_and_redact_all_credentials(self):
        values = {'admin': 'private-admin', 'business': 'private-business', 'postgres': 'private-postgres'}
        state = {'Status': 'exited', 'ExitCode': 1, 'OOMKilled': False, 'Error': ''}
        def answer(*args, **kwargs):
            if args[1:3] == ('container', 'inspect'):
                return SimpleNamespace(stdout=json.dumps([{'State': state, 'Config': {'Env': ['DO_NOT_ARCHIVE']}}]), returncode=0)
            return SimpleNamespace(stdout='startup FATAL private-admin private-business', stderr='private-postgres', returncode=0)
        output = io.StringIO()
        with patch.object(FIXTURE, 'owned', return_value=True), patch.object(FIXTURE, 'command', side_effect=answer), contextlib.redirect_stderr(output):
            FIXTURE.startup_diagnostics(values)
        evidence = [json.loads(line) for line in output.getvalue().splitlines()]
        self.assertEqual([row['container'] for row in evidence], [FIXTURE.HIGHGO, FIXTURE.POSTGRES])
        self.assertTrue(all(row['state'] == state for row in evidence))
        self.assertIn('FATAL', output.getvalue())
        for secret in [*values.values(), 'DO_NOT_ARCHIVE']:
            self.assertNotIn(secret, output.getvalue())

    def test_startup_deadline_names_failed_source_and_collects_diagnostics_without_extending_budget(self):
        for failed in (FIXTURE.HIGHGO, FIXTURE.POSTGRES):
            with self.subTest(failed=failed), tempfile.TemporaryDirectory() as temp:
                def query(name, *args, **kwargs):
                    if name == failed:
                        raise RuntimeError('not ready')
                    return 'HighGo V4.5 Business'
                with patch.object(FIXTURE, 'secret_directory', return_value=Path(temp)), patch.object(FIXTURE, 'owned', return_value=False), patch.object(FIXTURE, 'command'), patch.object(FIXTURE, 'sql', side_effect=query), patch.object(FIXTURE.time, 'monotonic', side_effect=[0, 181]), patch.object(FIXTURE.time, 'sleep') as sleep, patch.object(FIXTURE, 'startup_diagnostics', create=True) as diagnostics:
                    with self.assertRaisesRegex(RuntimeError, 'database startup deadline exceeded: ' + failed):
                        FIXTURE.start()
                    diagnostics.assert_called_once()
                    sleep.assert_not_called()

    def test_startup_diagnostics_never_read_foreign_container_logs(self):
        output = io.StringIO()
        with patch.object(FIXTURE, 'owned', side_effect=[RuntimeError('container ownership mismatch'), False]), patch.object(FIXTURE, 'command') as command, contextlib.redirect_stderr(output):
            FIXTURE.startup_diagnostics({'admin': 'private-value'})
        command.assert_not_called()
        evidence = [json.loads(line) for line in output.getvalue().splitlines()]
        self.assertEqual(evidence[0]['diagnostic_error'], 'RuntimeError')
        self.assertEqual(evidence[1]['state'], {'Status': 'absent'})

    def test_preflight_rejects_personal_and_conflicting_host_before_docker(self):
        valid = dict(GITHUB_ACTIONS='true', RUNNER_OS='Linux', ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1', ONLINE_SUITE='highgo-consumer-flow', POSTGRES_DB='addp_online')
        for key, wrong in [('GITHUB_ACTIONS', 'false'), ('RUNNER_OS', 'macOS'), ('ADDP_ONLINE_HOST', '0'),
                           ('ADDP_ONLINE_HOSTED', '0'), ('ADDP_ONLINE_OWNER_MANAGED', '1'), ('ONLINE_SUITE', 'other'), ('POSTGRES_DB', 'addp_test')]:
            with patch.dict(os.environ, dict(valid, **{key: wrong}), clear=True), patch.object(FIXTURE, 'command') as command:
                with self.assertRaises(RuntimeError): FIXTURE.guard()
                command.assert_not_called()

    def test_native_guard_refuses_emulated_or_arm_host(self):
        with patch.dict(os.environ, dict(GITHUB_ACTIONS='true', RUNNER_OS='Linux', ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1', ONLINE_SUITE='highgo-consumer-flow', POSTGRES_DB='addp_online'), clear=True), patch.object(FIXTURE.platform, 'system', return_value='Linux'), patch.object(FIXTURE.platform, 'machine', return_value='aarch64'):
            with self.assertRaisesRegex(RuntimeError, 'x86_64'): FIXTURE.guard()

    def test_source_pin_is_shared_with_business(self):
        image = FIXTURE.highgo_image()
        self.assertRegex(image, r'^qiuchenjun/hgdb-see:4\.5\.10\.3@sha256:[0-9a-f]{64}$')

    def test_secret_files_are_exclusive_and_permissions_are_private(self):
        with tempfile.TemporaryDirectory() as temp:
            target = Path(temp) / 'secret'
            FIXTURE.write_private(target, 'secret')
            self.assertEqual(target.stat().st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError): FIXTURE.write_private(target, 'replacement')

    def test_owned_requires_label_and_daemon_failure_is_not_absence(self):
        with patch.object(FIXTURE, 'command') as command:
            command.return_value = SimpleNamespace(returncode=0, stdout=json.dumps([{'Config': {'Labels': {'com.addp.online-fixture': 'someone-else'}}}]))
            with self.assertRaisesRegex(RuntimeError, 'ownership'): FIXTURE.owned(FIXTURE.HIGHGO)
            command.side_effect = [SimpleNamespace(returncode=1), RuntimeError('daemon unavailable')]
            with self.assertRaisesRegex(RuntimeError, 'daemon'): FIXTURE.owned(FIXTURE.HIGHGO)

    def test_command_failure_never_discloses_raw_diagnostics(self):
        with patch.object(FIXTURE.subprocess, 'run', return_value=SimpleNamespace(returncode=1, stdout='secret', stderr='password SQL')):
            with self.assertRaises(RuntimeError) as error: FIXTURE.command('docker', 'exec', input='private password')
            self.assertNotIn('password', str(error.exception))
            self.assertNotIn('secret', str(error.exception))

    def test_start_refuses_existing_container_before_creating_passwords(self):
        with patch.object(FIXTURE, 'secret_directory'), patch.object(FIXTURE, 'owned', return_value=True), patch.object(FIXTURE, 'write_private') as write:
            with self.assertRaisesRegex(RuntimeError, 'existing'): FIXTURE.start()
            write.assert_not_called()

    def test_start_uses_business_initializer_random_loopback_ports_and_ordinary_tls(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            def query(name, statement, *args, **kwargs):
                if 'SELECT version()' in statement: return 'HighGo Database Management System 4.5'
                if 'SELECT json_build_object' in statement: return '{"user":"business","ssl":true,"admin":false}'
                if 'SELECT engine_name' in statement: return 'HighGo V4.5 Business'
                if "WHERE order_no='SENTINEL'" in statement: return '1'
                return ''
            with patch.object(FIXTURE, 'secret_directory', return_value=directory), patch.object(FIXTURE, 'owned', return_value=False), patch.object(FIXTURE, 'command') as command, patch.object(FIXTURE, 'sql', side_effect=query), patch.object(FIXTURE, 'descriptor') as descriptor:
                FIXTURE.start()
                runs = [call.args for call in command.call_args_list if call.args[:2] == ('docker', 'run')]
                self.assertEqual(len(runs), 2)
                self.assertIn('linux/amd64', runs[0])
                self.assertIn('127.0.0.1::5866', runs[0])
                self.assertIn('/addp/highgo/entrypoint.sh', runs[0])
                self.assertIn('127.0.0.1::5432', runs[1])
                credentials = json.loads((directory / 'highgo-fixture.json').read_text())
                self.assertEqual(len(set(credentials.values())), 3)
                for secret in credentials.values():
                    self.assertNotIn(secret, str(runs))
                self.assertEqual(descriptor.call_args_list[0].args[6], 'business')
                self.assertEqual(descriptor.call_args_list[1].args[6], 'source_reader')

    def test_highgo_sql_password_uses_stdin_only_and_requires_tls(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp); (path / 'highgo-fixture.json').write_text(json.dumps({'business': 'private-value'}))
            with patch.object(FIXTURE, 'command', return_value=SimpleNamespace(stdout='1\n')) as command:
                self.assertEqual(FIXTURE.sql(FIXTURE.HIGHGO, 'SELECT 1;', path), '1')
                args, kw = command.call_args
                self.assertNotIn('private-value', ' '.join(args))
                self.assertEqual(kw['input'], 'private-value\nSELECT 1;')
                self.assertIn('PGSSLMODE=require', ' '.join(args))

    def test_cleanup_continues_to_second_container_and_refuses_volume_residue(self):
        with patch.object(FIXTURE, 'owned', return_value=True), patch.object(FIXTURE, 'command') as command:
            def answer(*args, **kwargs):
                if args[1:3] == ('container', 'inspect'):
                    return SimpleNamespace(stdout=json.dumps([{'Mounts': [{'Type': 'volume', 'Name': 'owned-volume'}]}]), returncode=0)
                return SimpleNamespace(stdout='', returncode=0)
            command.side_effect = answer
            with self.assertRaisesRegex(RuntimeError, 'cleanup'): FIXTURE.stop()
            self.assertIn(('docker', 'rm', '-fv', FIXTURE.POSTGRES), [c.args for c in command.call_args_list])

    def test_native_verification_refuses_changed_rows(self):
        with patch.object(FIXTURE, 'secret_directory'), patch.object(FIXTURE, 'owned', return_value=True), patch.object(FIXTURE, 'sql', side_effect=['1|source', '1|target']):
            with self.assertRaisesRegex(RuntimeError, 'seven-field'): FIXTURE.verify()


if __name__ == '__main__':
    unittest.main()
