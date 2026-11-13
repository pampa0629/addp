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


class HighGoTimeoutBarrierTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.environment = patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='owned-run')
        self.environment.start()
        self.addCleanup(self.environment.stop)

    def run_barrier(self, operation, responses, *, phase='manager', owned=True):
        with patch.object(FIXTURE, 'secret_directory', return_value=self.directory), \
             patch.object(FIXTURE, 'owned', return_value=owned), \
             patch.object(FIXTURE, 'command') as command, \
             patch.object(FIXTURE, 'sql', side_effect=responses) as sql:
            result = FIXTURE.timeout_barrier(operation, phase)
        return result, command, sql

    def receipt(self, holder=123):
        (self.directory / 'highgo-manager-barrier.json').write_text(json.dumps(
            dict(run_id='owned-run', phase='manager', holder_id=holder)))

    def test_acquire_observe_release_follow_same_native_holder_and_private_journal(self):
        holding = json.dumps(dict(holder_id=123, waiters=[]))
        acquired, command, _ = self.run_barrier('acquire', [holding])
        self.assertEqual(acquired, dict(run_id='owned-run', phase='manager', stage='acquire',
                                       holder_id=123, held=True, waiters=[]))
        journal = self.directory / 'highgo-manager-barrier.json'
        self.assertEqual(journal.stat().st_mode & 0o777, 0o600)
        args = command.call_args.args
        self.assertEqual(args[:4], ('docker', 'exec', '-d', FIXTURE.HIGHGO))
        self.assertIn('LOCK TABLE addp_business.addp_engine_probe IN ACCESS EXCLUSIVE MODE', args[-1])
        self.assertIn('pg_sleep(240)', args[-1])
        waiting = dict(id=456, state='active', wait_event_type='Lock', operation='table_read')
        observed, _, sql = self.run_barrier('observe', [json.dumps(dict(holder_id=123, waiters=[waiting]))])
        self.assertEqual(observed['waiters'], [waiting])
        observation = sql.call_args.args[1]
        self.assertIn('l.pid=a.pid', observation)
        self.assertIn("NOT l.granted AND l.relation='addp_business.addp_engine_probe'::regclass", observation)
        self.assertIn("query LIKE '%pg_total_relation_size(%'", observation)
        released, _, sql = self.run_barrier('release', [holding, 't', json.dumps(dict(holder_id=None, waiters=[]))])
        self.assertFalse(released['held'])
        termination = sql.call_args_list[1].args[1]
        self.assertIn("pid=123 AND application_name='hg_timeout_manager_owned-run'", termination)
        self.assertIn("usename='sysdba' AND datname='highgo'", termination)

    def test_unknown_dispatch_outcome_leaves_intent_and_refuses_repeat(self):
        with patch.object(FIXTURE, 'secret_directory', return_value=self.directory), \
             patch.object(FIXTURE, 'owned', return_value=True), \
             patch.object(FIXTURE, 'command', side_effect=RuntimeError('dispatch failed')) as command:
            with self.assertRaisesRegex(RuntimeError, 'dispatch failed'):
                FIXTURE.timeout_barrier('acquire', 'manager')
            with self.assertRaisesRegex(RuntimeError, 'cannot be repeated'):
                FIXTURE.timeout_barrier('acquire', 'manager')
            self.assertEqual(command.call_count, 1)
        self.assertIsNone(json.loads((self.directory / 'highgo-manager-barrier.json').read_text())['holder_id'])

    def test_release_never_terminates_a_replaced_or_missing_holder(self):
        self.receipt()
        for identity in (None, 999):
            with self.subTest(identity=identity), \
                 patch.object(FIXTURE, 'secret_directory', return_value=self.directory), \
                 patch.object(FIXTURE, 'owned', return_value=True), \
                 patch.object(FIXTURE, 'sql', return_value=json.dumps(dict(holder_id=identity, waiters=[]))) as sql:
                with self.assertRaisesRegex(RuntimeError, 'disappeared'):
                    FIXTURE.timeout_barrier('release', 'manager')
                self.assertEqual(sql.call_count, 1)

    def test_rejects_wrong_run_foreign_container_and_unknown_scope(self):
        self.receipt()
        with patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='another-run'):
            with self.assertRaisesRegex(RuntimeError, 'another run'):
                self.run_barrier('release', [])
        with self.assertRaisesRegex(RuntimeError, 'owned HighGo'):
            self.run_barrier('acquire', [], owned=False)
        for operation, phase in [('retry', 'manager'), ('acquire', 'business-table')]:
            with self.assertRaisesRegex(RuntimeError, 'use barrier'):
                self.run_barrier(operation, [], phase=phase)

    def test_observation_rejects_duplicate_nonlock_or_unbounded_payload(self):
        self.receipt()
        valid = dict(id=456, state='active', wait_event_type='Lock', operation='table_read')
        for waiters in ([valid, valid], [dict(valid, wait_event_type='IO')],
                        [dict(valid, id=True)], [dict(valid, operation='unknown')], [dict(valid, query='private SQL')]):
            with self.subTest(waiters=waiters), self.assertRaisesRegex(RuntimeError, 'waiter identity'):
                self.run_barrier('observe', [json.dumps(dict(holder_id=123, waiters=waiters))])

    def test_expired_acquisition_is_not_retried_or_extended(self):
        with patch.object(FIXTURE.time, 'monotonic', side_effect=[0, 11]), \
             patch.object(FIXTURE.time, 'sleep') as sleep, self.assertRaisesRegex(RuntimeError, 'deadline exceeded'):
            self.run_barrier('acquire', [json.dumps(dict(holder_id=None, waiters=[]))])
        sleep.assert_not_called()


class HighGoFixtureTest(unittest.TestCase):
    def test_random_password_cannot_contain_highgo_reserved_db_keyword(self):
        random_hex = 'db0123456789abcdefdb0123456789abcdef00'
        with patch.object(FIXTURE.secrets, 'token_hex', return_value=random_hex) as random:
            value = FIXTURE.password()
        random.assert_called_once_with(18)
        self.assertEqual(value, 'Aa1!' + random_hex.replace('b', 'x').replace('d', 'y'))
        self.assertNotIn('db', value.lower())
        self.assertEqual(len(set('0123456789abcdef'.translate(str.maketrans({'b': 'x', 'd': 'y'})))), 16)

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

    def test_timeout_case_starts_only_native_highgo_and_verifies_probe_and_no_control_sessions(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            def query(name, statement, *args, **kwargs):
                self.assertEqual(name, FIXTURE.HIGHGO)
                if 'SELECT version()' in statement: return 'HighGo Database Management System 4.5'
                if 'SELECT json_build_object' in statement: return '{"user":"business","ssl":true,"admin":false}'
                if 'SELECT engine_name' in statement: return 'HighGo V4.5 Business'
                if 'SELECT id,engine_name' in statement: return '1|HighGo V4.5 Business'
                if 'pg_stat_activity' in statement: return '0'
                self.fail('timeout case must not initialize other target tables')
            with patch.dict(os.environ, ADDP_ONLINE_HIGHGO_CASE='sql-timeout'), \
                 patch.object(FIXTURE, 'secret_directory', return_value=directory), \
                 patch.object(FIXTURE, 'owned', return_value=False), \
                 patch.object(FIXTURE, 'command') as command, patch.object(FIXTURE, 'sql', side_effect=query), \
                 patch.object(FIXTURE, 'descriptor') as descriptor:
                FIXTURE.start()
                runs = [c for c in command.call_args_list if c.args[:2] == ('docker', 'run')]
                self.assertEqual(len(runs), 1)
                descriptor.assert_called_once()
                with patch.object(FIXTURE, 'owned', side_effect=lambda name: name == FIXTURE.HIGHGO):
                    FIXTURE.verify()

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
