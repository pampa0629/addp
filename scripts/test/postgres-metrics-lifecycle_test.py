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
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('postgres_metrics_lifecycle', ROOT / 'scripts/infra/postgres-observation/lifecycle.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class LifecycleTest(unittest.TestCase):
    def test_authentication_read_diagnostics_never_echo_private_output(self):
        database = module.Database.__new__(module.Database)
        database.identity = 'a' * 64
        database.settings = {'hba': '/fixed/pg_hba.conf'}
        database.docker = mock.Mock()
        for code, reason in [(41, 'symbolic link'), (42, 'not a regular file'), (43, 'size read failed'),
                             (44, 'size budget exceeded'), (45, 'content read failed'), (1, 'container command failed')]:
            database.docker.run.return_value = subprocess.CompletedProcess([], code, 'private-hba-sentinel', 'private-error-sentinel')
            with self.assertRaises(module.LifecycleError) as error:
                database.read_hba()
            self.assertIn(reason, str(error.exception))
            self.assertNotIn('sentinel', str(error.exception))
        database.docker.run.return_value = subprocess.CompletedProcess([], 0, 'local all all trust\n', '')
        self.assertEqual(database.read_hba(), 'local all all trust\n')

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
