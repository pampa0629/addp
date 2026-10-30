"""Deployment lifecycle contracts without a running database or Docker daemon."""
import importlib.util
import json
import os
from pathlib import Path
import shlex
import sys
import tempfile
import unittest
import subprocess
import io
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('postgres_metrics_lifecycle', ROOT / 'scripts/infra/postgres-observation/lifecycle.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class LifecycleTest(unittest.TestCase):
    def test_activation_failure_reports_only_closed_stage_and_keeps_cleanup(self):
        from contextlib import nullcontext, redirect_stderr
        with tempfile.TemporaryDirectory() as temporary:
            database = mock.Mock(project='business', maintenance='postgres', networks=['127.0.0.1/32'])
            database.locked.return_value = nullcontext()
            database.read_hba.return_value = 'local all all trust\n'
            database.sql.return_value = '0'
            database.write_hba.side_effect = OSError('private-hba-password-sentinel')
            docker = mock.Mock(info={'id': 'fixture'})
            docker.run.return_value = ''
            lifecycle = module.SourceLifecycle(docker, 'business', Path(temporary).resolve() / 'state', lambda: database)
            with mock.patch.object(module, 'transport', return_value=(Path(temporary), '127.0.0.1', 9443)):
                with self.assertRaises(OSError) as error:
                    lifecycle.up({})
            database.disable.assert_called()
            self.assertFalse(any(call.args[0][0] == 'compose' for call in docker.run.call_args_list))
            output = io.StringIO()
            with mock.patch.object(sys, 'argv', ['lifecycle.py', 'up', '--scope', 'business']), \
                    mock.patch.object(module, 'run', side_effect=error.exception), redirect_stderr(output):
                self.assertEqual(module.main(), 1)
            self.assertIn('stage=authentication-update', output.getvalue())
            self.assertNotIn('sentinel', output.getvalue())

    def test_main_reports_closed_lifecycle_reason_without_raw_exception_values(self):
        from contextlib import redirect_stderr
        for error, expected in [(module.LifecycleError('PostgreSQL authentication reload timed out'), 'reload timed out'),
                                (ValueError('private-json-sentinel'), 'configuration failed'),
                                (OSError('private-path-sentinel'), 'configuration failed')]:
            with self.subTest(error=type(error).__name__):
                output = io.StringIO()
                with mock.patch.object(sys, 'argv', ['lifecycle.py', 'up', '--scope', 'business']), \
                        mock.patch.object(module, 'run', side_effect=error), redirect_stderr(output):
                    self.assertEqual(module.main(), 1)
                self.assertIn(expected, output.getvalue())
                self.assertNotIn('sentinel', output.getvalue())

    def test_cleanup_failure_does_not_replace_primary_activation_failure(self):
        from contextlib import nullcontext, redirect_stderr
        with tempfile.TemporaryDirectory() as temporary:
            database = mock.Mock()
            database.locked.return_value = nullcontext()
            database.disable.side_effect = [None, OSError('private-cleanup-sentinel')]
            docker = mock.Mock(info={'id': 'fixture'})
            docker.run.return_value = ''
            manager = module.SourceLifecycle(docker, 'infra', Path(temporary).resolve() / 'state', lambda: database)
            original = module.LifecycleError('PostgreSQL source certificate verification failed')
            with mock.patch.object(module, 'transport', side_effect=original):
                with self.assertRaises(module.LifecycleError) as error:
                    manager.up({})
            self.assertIs(error.exception, original)
            self.assertTrue(original.observation_cleanup_failed)
            output = io.StringIO()
            with mock.patch.object(sys, 'argv', ['lifecycle.py', 'up', '--scope', 'infra']), \
                    mock.patch.object(module, 'run', side_effect=original), redirect_stderr(output):
                self.assertEqual(module.main(), 1)
            self.assertIn('stage=transport-check', output.getvalue())
            self.assertIn('cleanup=failed', output.getvalue())
            self.assertNotIn('sentinel', output.getvalue())

    def test_authentication_read_diagnostics_never_echo_private_output(self):
        database = module.Database.__new__(module.Database)
        database.identity = 'a' * 64
        database.settings = {'hba': '/fixed/pg_hba.conf'}
        database.docker = mock.Mock()
        for code, reason in [(41, 'symbolic link'), (42, 'not a regular file'),
                             (45, 'content read failed'), (1, 'container command failed')]:
            database.docker.run.return_value = subprocess.CompletedProcess([], code, 'private-hba-sentinel', 'private-error-sentinel')
            with self.assertRaises(module.LifecycleError) as error:
                database.read_hba()
            self.assertIn(reason, str(error.exception))
            self.assertNotIn('sentinel', str(error.exception))
        database.docker.run.return_value = subprocess.CompletedProcess([], 0, 'local all all trust\n', '')
        self.assertEqual(database.read_hba(), 'local all all trust\n')

    def test_authentication_read_is_bounded_and_missing_has_closed_diagnosis(self):
        database = module.Database.__new__(module.Database)
        database.identity = 'owned'
        database.settings = {'hba': '/fixed/pg_hba.conf'}
        database.docker = mock.Mock()
        for code, stderr, reason in [(45, 'head: No such file or directory', 'not visible'),
                                     (45, 'head: Permission denied', 'content read failed'),
                                     (41, '', 'symbolic link'), (42, '', 'not a regular file')]:
            database.docker.run.return_value = subprocess.CompletedProcess([], code, '', stderr)
            with self.assertRaises(module.LifecycleError) as error:
                database.read_hba()
            self.assertIn(reason, str(error.exception))
        database.docker.run.return_value = subprocess.CompletedProcess([], 0, 'a' * (module.source.HBA_LIMIT + 1), '')
        with self.assertRaisesRegex(module.LifecycleError, 'size budget exceeded'):
            database.read_hba()
        command = database.docker.run.call_args.args[0]
        self.assertIn('head -c 262145', command[command.index('-c') + 1])
        self.assertNotIn('wc -c', command[command.index('-c') + 1])

    def test_authentication_reader_shell_rejects_special_files_and_bounds_real_reads(self):
        database = module.Database.__new__(module.Database)
        database.identity = 'owned'
        database.settings = {'hba': '/fixed/pg_hba.conf'}
        database.docker = mock.Mock()
        database.docker.run.return_value = subprocess.CompletedProcess([], 0, '', '')
        database.read_hba()
        command = database.docker.run.call_args.args[0]
        script = command[command.index('-c') + 1]
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            regular = root / 'hba'
            regular.write_text('local all all trust\n')
            link = root / 'linked'
            link.symlink_to(regular)
            fifo = root / 'fifo'
            os.mkfifo(fifo)
            for path, expected in [(regular, 0), (link, 41), (root, 42), (fifo, 42), (root/'missing', 45)]:
                result = subprocess.run(['sh', '-c', script, 'reader-fixture', str(path)],
                    capture_output=True, text=True, timeout=3, env=dict(os.environ, LC_ALL='C'))
                self.assertEqual(result.returncode, expected, str(path))
            regular.write_text('a' * (module.source.HBA_LIMIT * 2))
            result = subprocess.run(['sh', '-c', script, 'reader-fixture', str(regular)],
                capture_output=True, timeout=3)
            self.assertEqual(len(result.stdout), module.source.HBA_LIMIT + 1)

    def test_host_publication_refuses_missing_drift_and_permission_without_reload(self):
        for problem in ['third-version', module.LifecycleError('not visible'), module.LifecycleError('permission denied'), 'old']:
            database = module.Database.__new__(module.Database)
            database.authentication = Path('/private/owned-auth')
            database.read_hba = mock.Mock(side_effect=problem) if isinstance(problem, Exception) else mock.Mock(return_value=problem)
            database.sql = mock.Mock()
            with mock.patch.object(module.auth, 'replace'):
                with self.assertRaises(module.LifecycleError):
                    database.write_hba('old', 'new')
            database.sql.assert_not_called()

    def test_unchanged_authentication_keeps_inode_after_all_version_checks(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary).resolve() / 'auth'
            baseline = 'local all all trust\n'
            runtime = module.auth.prepare_business(directory, baseline)
            value = module.source.authentication_configuration(baseline, 'business', ['127.0.0.1/32'])
            module.auth.replace(directory, baseline, value)
            path = runtime / 'pg_hba.conf'
            before = path.stat()
            with mock.patch.object(module.auth.tempfile, 'mkstemp', side_effect=AssertionError('Unchanged policy must not be republished')):
                module.auth.replace(directory, value, value)
                stale = module.source.authentication_configuration(baseline, 'business', ['127.0.0.1/32', '::1/128'])
                with self.assertRaisesRegex(module.auth.AuthenticationError, 'version changed'):
                    module.auth.replace(directory, stale, value)
                path.chmod(0o600)
                try:
                    with self.assertRaises(module.auth.AuthenticationError):
                        module.auth.replace(directory, value, value)
                finally:
                    path.chmod(0o644)
            after = path.stat()
            self.assertEqual((after.st_ino, after.st_mtime_ns), (before.st_ino, before.st_mtime_ns))
            self.assertEqual(path.read_text(), value)

    def test_source_queries_are_project_scoped(self):
        docker = mock.Mock()
        docker.run.return_value = ''
        manager = module.SourceLifecycle(docker, 'infra', Path('/private/unused'), mock.Mock(), project='owned-t2')
        self.assertFalse(manager.source_containers())
        self.assertIn('label=com.docker.compose.project=owned-t2', docker.run.call_args.args[0])
        probe = (ROOT / 'scripts/test/postgres-lifecycle-probe.py').read_text()
        self.assertNotIn("docker.run(['ps', '-q', '--filter', 'label='+module.OWNER", probe)

    def test_business_selection_does_not_inherit_infra_inputs(self):
        keys = ['ADDP_POSTGRES_METRICS_'+suffix for suffix in ('ENABLED', 'TLS_DIR', 'LISTEN', 'STATE_DIR')]
        parent = dict(os.environ, **dict(zip(keys, ['true', '/infra-tls', '127.0.0.1:9999', '/infra-state'])))
        for name in ('start', 'stop'):
            script = (ROOT / 'business/scripts' / (name+'.sh')).read_text()
            block = script[script.index('unset ADDP_POSTGRES_METRICS_ENABLED'):]
            block = block[:block.index('if [ "$ENABLE_FALKORDB"' if name == 'start' else 'python3 "$PROJECT_ROOT/')]
            for content, expected in [('', [None]*4), ('POSTGRES_USER=business\n', [None]*4),
                                      ('ADDP_POSTGRES_METRICS_ENABLED=true\nADDP_POSTGRES_METRICS_LISTEN=127.0.0.1:9449\n', ['true', None, '127.0.0.1:9449', None])]:
                with self.subTest(script=name, content=content), tempfile.TemporaryDirectory() as temporary:
                    if content:
                        (Path(temporary)/'.env').write_text(content)
                    command = block + '\n' + shlex.join([sys.executable, '-c',
                        'import json,os; print(json.dumps([os.environ.get(key) for key in '+repr(keys)+']))'])
                    result = subprocess.run(['bash', '-eu', '-c', command], cwd=temporary, env=parent,
                                            capture_output=True, text=True, check=True, timeout=5)
                    self.assertEqual(json.loads(result.stdout), expected)

    def test_docker_mount_order_is_not_an_identity_change(self):
        docker = module.Docker.__new__(module.Docker)
        mounts = [{'Destination': '/a', 'Type': 'bind', 'Source': '/host/a', 'RW': False},
                  {'Destination': '/b', 'Type': 'bind', 'Source': '/host/b', 'RW': False}]
        docker.run = mock.Mock(side_effect=[json.dumps({'mounts': mounts}),
                                           json.dumps({'mounts': list(reversed(mounts))}),
                                           json.dumps({'mounts': [dict(mounts[0], RW=True), mounts[1]]})])
        original = docker.fact('owned')
        self.assertEqual(original, docker.fact('owned'))
        self.assertNotEqual(original, docker.fact('owned'))

    def test_installation_has_one_active_revision_and_explicit_login_intent(self):
        for scope in ('infra', 'business'):
            for enabled in (False, True):
                value = module.install_input(scope, '1' * 64, enabled)
                self.assertIn("\\set observer_enabled '"+str(enabled).lower()+"'", value)
                self.assertNotIn('1' * 64, value)
                self.assertIn('CREATE ROLE %I NOLOGIN', value)
        for scope, enabled in [('tenant', True), ('infra', 'true')]:
            with self.assertRaises(ValueError):
                module.install_input(scope, '1' * 64, enabled)

    def test_state_is_external_and_scope_separated(self):
        with tempfile.TemporaryDirectory() as temporary:
            env = {'ADDP_POSTGRES_METRICS_STATE_DIR': str(Path(temporary).resolve())}
            self.assertNotEqual(module.state_directory(env, 'infra'), module.state_directory(env, 'business'))
        for value in ('relative', str(ROOT / '.dev-state/metrics')):
            with self.assertRaises(ValueError):
                module.state_directory({'ADDP_POSTGRES_METRICS_STATE_DIR': value}, 'infra')

    def test_foreign_source_is_never_stopped_or_replaced(self):
        docker = mock.Mock()
        docker.run.return_value = 'a' * 64
        docker.fact.return_value = {'labels': {'com.docker.compose.project': 'addp-postgres-metrics-infra',
            'com.docker.compose.service': 'postgres-metrics', module.OWNER: '/foreign', module.SCOPE: 'infra'}}
        lifecycle = module.SourceLifecycle(docker, 'infra', Path('/private/unused'), mock.Mock())
        with self.assertRaises(ValueError):
            lifecycle.stop_sources()
        self.assertFalse(any(call.args[0][0] == 'rm' for call in docker.run.call_args_list))

    def test_disabled_uninstalled_source_needs_no_database_or_credentials(self):
        docker = mock.Mock()
        docker.run.return_value = ''
        factory = mock.Mock(side_effect=AssertionError('Unselected source must not read database'))
        with tempfile.TemporaryDirectory() as temporary:
            state = Path(temporary) / 'uncreated'
            module.SourceLifecycle(docker, 'infra', state, factory).down()
            self.assertFalse(state.exists())
            factory.assert_not_called()

    def test_failure_closes_our_account_and_keeps_source_stopped(self):
        with tempfile.TemporaryDirectory() as temporary:
            database = mock.Mock()
            from contextlib import nullcontext
            database.locked.return_value = nullcontext()
            docker = mock.Mock(info={'id': 'fixture'})
            docker.run.return_value = ''
            lifecycle = module.SourceLifecycle(docker, 'infra', Path(temporary).resolve() / 'state', lambda: database)
            with mock.patch.object(module, 'transport', side_effect=ValueError('invalid TLS')):
                with self.assertRaises(ValueError):
                    lifecycle.up({})
            self.assertGreaterEqual(database.disable.call_count, 1)
            self.assertFalse(any(call.args[0][0] == 'compose' for call in docker.run.call_args_list))

    def test_publication_rejects_automatic_or_public_addresses(self):
        for listen in ('', ':9443', '0.0.0.0:9443', '[::]:9443', '8.8.8.8:9443', '127.0.0.1:0', '127.0.0.1:09443'):
            with self.subTest(listen=listen), self.assertRaises(ValueError):
                module.transport({'ADDP_POSTGRES_METRICS_LISTEN': listen})


if __name__ == '__main__':
    unittest.main()
