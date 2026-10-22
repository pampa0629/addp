from importlib import import_module
import os
import json
from pathlib import Path
import signal
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace

M = import_module('scripts.test.continuous-execution-faults')


class ContinuousFaultsTest(unittest.TestCase):
    def test_protection_readiness_uses_only_the_proven_hosted_owner_checkpoint(self):
        faults = object.__new__(M.HostedFaults)
        faults.root, faults.secret = Path('/owned'), Path('/private')
        container = {'Id': 'owned-container', 'State': {'Running': True}, 'Config': {
            'Labels': {'com.docker.compose.service': 'postgres', 'com.docker.compose.project.working_dir': '/owned'},
            'Env': ['POSTGRES_DB=addp_online', 'POSTGRES_USER=fixture', 'POSTGRES_PASSWORD=private']}}
        with patch.dict(os.environ, POSTGRES_USER='fixture', POSTGRES_PASSWORD='private'), \
                patch.object(M.SUPPORT, 'deployment_secret', return_value=faults.secret):
            for state, expected in [('', False), ('false', False), ('true', True)]:
                with self.subTest(state=state), patch.object(M.subprocess, 'run', side_effect=[
                        SimpleNamespace(stdout=json.dumps([container])), SimpleNamespace(stdout=state)]) as run:
                    self.assertEqual(faults.protection_ready(2), expected)
                    sql = run.call_args.kwargs['input']
                    self.assertEqual(sql, 'BEGIN READ ONLY; SELECT initialized::text FROM transfer.protection_projection_checkpoints WHERE tenant_id = 2; COMMIT;')
                    self.assertIn('owned-container', run.call_args.args[0])
                    self.assertNotIn('private', ' '.join(run.call_args.args[0]))
            for field, value in [('POSTGRES_DB', 'addp'), ('POSTGRES_PASSWORD', 'foreign')]:
                changed = json.loads(json.dumps(container))
                changed['Config']['Env'] = [v for v in changed['Config']['Env'] if not v.startswith(field + '=')] + [field + '=' + value]
                with self.subTest(field=field), patch.object(M.subprocess, 'run', return_value=SimpleNamespace(stdout=json.dumps([changed]))) as run, self.assertRaises(ValueError):
                    faults.protection_ready(2)
                self.assertEqual(run.call_count, 1)
            for field in ['com.docker.compose.service', 'com.docker.compose.project.working_dir']:
                changed = json.loads(json.dumps(container)); changed['Config']['Labels'][field] = 'foreign'
                with self.subTest(field=field), patch.object(M.subprocess, 'run', return_value=SimpleNamespace(stdout=json.dumps([changed]))) as run, self.assertRaises(ValueError):
                    faults.protection_ready(2)
                self.assertEqual(run.call_count, 1)

    def test_protection_readiness_fails_before_docker_on_wrong_deployment_or_tenant(self):
        faults = object.__new__(M.HostedFaults); faults.secret = Path('/private')
        with patch.object(M.subprocess, 'run') as run:
            for tenant in [True, 1, 0, '2']:
                with self.subTest(tenant=tenant), self.assertRaises(ValueError): faults.protection_ready(tenant)
            with patch.object(M.SUPPORT, 'deployment_secret', return_value=Path('/foreign')), self.assertRaises(ValueError):
                faults.protection_ready(2)
            run.assert_not_called()

    def test_protection_wait_requires_a_true_durable_fact_and_has_a_deadline(self):
        faults = object.__new__(M.HostedFaults)
        with patch.object(faults, 'protection_ready', side_effect=[False, True]) as ready, patch.object(M.time, 'sleep'):
            faults.wait_protection_ready(2)
            self.assertEqual(ready.call_count, 2)
        with patch.object(faults, 'protection_ready', return_value=False), \
                patch.object(M.time, 'monotonic', side_effect=[0, 91]), self.assertRaises(ValueError):
            faults.wait_protection_ready(2)

    def test_local_deployment_is_rejected_before_pid_or_network_access(self):
        with patch.dict(os.environ, {'GITHUB_ACTIONS': 'false'}, clear=False), self.assertRaises(ValueError):
            M.HostedFaults()

    def test_replacement_runtime_does_not_receive_human_tokens_passwords_or_engine_provisioner(self):
        values = {'PATH': '/bin', 'POSTGRES_DB': 'addp_online', 'ADDP_ONLINE_HOSTED': '1',
                  'ADDP_ONLINE_SECRET_DIR': '/tmp/private', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'human',
                  'ADDP_ONLINE_PARENT_USER_PASSWORD': 'human-password',
                  'ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN': 'admin', 'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN': 'service'}
        runtime = M.runtime_environment(values)
        self.assertEqual(runtime, {'PATH': '/bin', 'POSTGRES_DB': 'addp_online',
                                   'ADDP_ONLINE_HOSTED': '1', 'ADDP_ONLINE_SECRET_DIR': '/tmp/private'})

    def test_hung_owner_is_resumed_even_when_operation_raises(self):
        faults = object.__new__(M.HostedFaults)
        faults.pin = lambda module: (42, 123)
        with patch.object(M.signal, 'pidfd_send_signal', create=True) as send, patch.object(M.os, 'close') as close, \
                patch.object(M, 'wait_owner_state') as wait:
            with self.assertRaisesRegex(RuntimeError, 'request failed'):
                faults.stop_owner_during(lambda: (_ for _ in ()).throw(RuntimeError('request failed')))
            self.assertEqual([call.args for call in send.call_args_list], [(123, signal.SIGSTOP), (123, signal.SIGCONT)])
            close.assert_called_once_with(123)
            self.assertEqual([call.args for call in wait.call_args_list], [(42, True), (42, False)])

    def test_descriptor_closed_when_resume_itself_fails(self):
        faults = object.__new__(M.HostedFaults)
        faults.pin = lambda module: (42, 123)
        with patch.object(M.signal, 'pidfd_send_signal', side_effect=[None, OSError('exited')], create=True), patch.object(M.os, 'close') as close, \
                patch.object(M, 'wait_owner_state'):
            with self.assertRaises(OSError):
                faults.stop_owner_during(lambda: True)
            close.assert_called_once_with(123)

    def test_owner_fault_receipt_requires_the_same_process_to_stop_and_resume(self):
        faults = object.__new__(M.HostedFaults)
        faults.pin = lambda module: (42, 123)
        operation = unittest.mock.Mock()
        with patch.object(M.signal, 'pidfd_send_signal', create=True), patch.object(M.os, 'close') as close, \
                patch.object(M, 'wait_owner_state') as wait:
            result = faults.stop_owner_during(operation)
        self.assertEqual(result, {'owner_pid': 42, 'stopped_verified': True, 'resumed_verified': True})
        operation.assert_called_once_with()
        self.assertEqual([call.args for call in wait.call_args_list], [(42, True), (42, False)])
        close.assert_called_once_with(123)

    def test_failed_stop_observation_still_resumes_but_never_runs_the_browser_action(self):
        faults = object.__new__(M.HostedFaults)
        faults.pin = lambda module: (42, 123)
        operation = unittest.mock.Mock()
        with patch.object(M.signal, 'pidfd_send_signal', create=True) as send, patch.object(M.os, 'close') as close, \
                patch.object(M, 'wait_owner_state', side_effect=[ValueError('not stopped'), None]), \
                self.assertRaisesRegex(ValueError, 'not stopped'):
            faults.stop_owner_during(operation)
        operation.assert_not_called()
        self.assertEqual([call.args for call in send.call_args_list], [(123, signal.SIGSTOP), (123, signal.SIGCONT)])
        close.assert_called_once_with(123)

    def test_process_state_observation_rejects_running_as_stopped_and_zombie_as_resumed(self):
        for stopped, state in [(True, 'S'), (False, 'Z')]:
            with self.subTest(stopped=stopped), patch.object(M.Path, 'read_text', return_value='State:\t' + state), \
                    patch.object(M.time, 'monotonic', side_effect=[0, 6]), self.assertRaises(ValueError):
                M.wait_owner_state(42, stopped)
        for stopped, state in [(True, 'T'), (False, 'S')]:
            with self.subTest(stopped=stopped), patch.object(M.Path, 'read_text', return_value='State:\t' + state):
                M.wait_owner_state(42, stopped)

    def test_process_must_match_executable_database_secret_and_uid(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            secret = root / 'private'
            binary = root / '.dev-bins/addp-transfer-continuous-worker'
            binary.parent.mkdir()
            binary.touch()
            proc = root / 'proc'
            proc.mkdir()
            (proc / 'exe').symlink_to(binary)
            good = {'ADDP_ONLINE_SECRET_DIR': str(secret), 'POSTGRES_DB': 'addp_online', 'ADDP_ONLINE_HOSTED': '1'}
            def environment(values):
                (proc / 'environ').write_bytes('\0'.join(f'{k}={v}' for k, v in values.items()).encode())
            environment(good)
            M.validate_process(proc, root, secret, 'transfer-continuous-worker-1')
            for key, value in [('POSTGRES_DB', 'addp'), ('ADDP_ONLINE_SECRET_DIR', '/other'), ('ADDP_ONLINE_HOSTED', '0')]:
                environment({**good, key: value})
                with self.assertRaises(ValueError):
                    M.validate_process(proc, root, secret, 'transfer-continuous-worker-1')
            environment(good)
            with self.assertRaises(ValueError):
                M.validate_process(proc, root, secret, 'system')


if __name__ == '__main__':
    unittest.main()
