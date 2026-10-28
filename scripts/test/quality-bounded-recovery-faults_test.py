"""Ownership, fencing and cleanup checks without signalling real processes."""
from importlib import import_module
import io
import json
import os
from pathlib import Path
import tempfile
import types
import unittest
from unittest.mock import Mock, patch

M = import_module('scripts.test.quality-bounded-recovery-faults')


class FaultTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='quality-fault-guard-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.secret = self.root / 'secret'
        self.secret.mkdir(mode=0o700)
        self.row = {'unit': 'quality-worker-1', 'pid': 42, 'start_ticks': '123', 'binary_sha256': 'a'*64, 'instance_id': '00000000-0000-4000-8000-000000000001'}

    def test_boundary_refuses_other_cases_and_worker_configuration(self):
        env = {'ADDP_ONLINE_QUALITY_CASE': 'bounded-recovery', 'ADDP_DEV_QUALITY_WORKER_REPLICAS': '2',
               'QUALITY_WORKER_CONCURRENCY': '1', 'QUALITY_WORKER_LEASE_DURATION': '30s'}
        with patch.object(M.HOSTED, 'deployment_secret', return_value=self.secret), \
                patch.object(M.os, 'pidfd_open', create=True), patch.object(M.signal, 'pidfd_send_signal', create=True):
            with patch.dict(os.environ, env, clear=True):
                self.assertEqual(M.boundary(), self.secret)
            for key in env:
                with self.subTest(key=key), patch.dict(os.environ, {**env, key: 'wrong'}, clear=True), self.assertRaises(ValueError):
                    M.boundary()

    def test_process_requires_exact_binary_cwd_private_deployment_and_live_identity(self):
        (self.root / '.dev-bins').mkdir()
        binary = self.root / '.dev-bins/addp-quality-worker'
        binary.write_bytes(b'owned-worker')
        proc = self.root / 'proc'
        proc.mkdir()
        (proc / 'cwd').symlink_to(self.root)
        (proc / 'exe').symlink_to(binary)
        (proc / 'stat').write_text('42 (quality-worker) S ' + ' '.join(['0']*18 + ['123']))
        env = f'ADDP_ONLINE_SECRET_DIR={self.secret}\0ADDP_ONLINE_HOSTED=1\0POSTGRES_DB=addp_online\0ADDP_PROCESS_INSTANCE_ID=00000000-0000-4000-8000-000000000001\0'
        (proc / 'environ').write_text(env)
        observed = M.validate_process(proc, self.root, self.secret, 'quality-worker-1', 42)
        self.assertEqual(observed['start_ticks'], '123')
        for replacement in (env.replace('addp_online', 'addp_test'), env.replace(str(self.secret), '/foreign'), env.replace('HOSTED=1', 'HOSTED=0')):
            (proc / 'environ').write_text(replacement)
            with self.assertRaises(ValueError):
                M.validate_process(proc, self.root, self.secret, 'quality-worker-1', 42)
        (proc / 'environ').write_text(env)
        with self.assertRaises(ValueError):
            M.validate_process(proc, self.root, self.secret, 'system', 42)
        (proc / 'stat').write_text('42 (quality-worker) Z ' + ' '.join(['0']*18 + ['123']))
        with self.assertRaises(ValueError):
            M.validate_process(proc, self.root, self.secret, 'quality-worker-1', 42)

    def test_pause_persists_recovery_before_signal_and_rejects_identity_change(self):
        with patch.object(M, 'boundary', return_value=self.secret), patch.object(M, 'ROOT', self.root):
            faults = M.HostedFaults(2, 3)
            faults.pins['quality-worker-1'] = self.row, 99
            def sent(fd, sig):
                path = self.secret / M.RECOVERY
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                self.assertEqual(json.loads(path.read_text())['workers'], [self.row])
                self.assertEqual(fd, 99)
            with patch.object(M, 'process_record', return_value=self.row), \
                    patch.object(M.signal, 'pidfd_send_signal', side_effect=sent, create=True), patch.object(M.CHILD, 'wait_owner_state'):
                faults.pause('quality-worker-1', True)
            with patch.object(M, 'process_record', return_value={**self.row, 'start_ticks': 'reused'}), \
                    patch.object(M.signal, 'pidfd_send_signal', create=True) as signal_call, self.assertRaises(ValueError):
                faults.pause('quality-worker-1', False)
            signal_call.assert_not_called()

    def test_pin_race_closes_descriptor_without_sending_signal(self):
        with patch.object(M, 'boundary', return_value=self.secret), patch.object(M, 'ROOT', self.root):
            faults = M.HostedFaults(2, 3)
            with patch.object(M, 'process_record', side_effect=[self.row, {**self.row, 'pid': 43}]), \
                    patch.object(M.os, 'pidfd_open', return_value=99, create=True), patch.object(M.os, 'close') as close, \
                    patch.object(M.signal, 'pidfd_send_signal', create=True) as send, self.assertRaises(ValueError):
                faults.pin('quality-worker-1')
            close.assert_called_once_with(99)
            send.assert_not_called()

    def test_crash_replacement_preserves_peer_and_backend_and_removes_human_credentials(self):
        (self.root / 'logs').mkdir()
        (self.root / '.dev-pids').mkdir()
        backend = self.root / '.dev-pids/quality.pid'
        backend.write_text('100')
        peer = {**self.row, 'unit': 'quality-worker-2', 'pid': 43}
        replacement = {**self.row, 'pid': 44, 'instance_id': '00000000-0000-4000-8000-000000000002'}
        with patch.object(M, 'boundary', return_value=self.secret), patch.object(M, 'ROOT', self.root), \
                patch.dict(os.environ, {'ADDP_ONLINE_ARTIFACT_DIR': str(self.root / 'logs'), 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'private-human-token'}):
            for changed in (False, True):
                backend.write_text('100')
                faults = M.HostedFaults(2, 3)
                faults.pins['quality-worker-1'] = self.row, 99
                def start(*args, **kwargs):
                    self.assertNotIn('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN', kwargs['env'])
                    if changed:
                        backend.write_text('101')
                with patch.object(M, 'process_record', side_effect=lambda unit: self.row if unit.endswith('-1') else peer), \
                        patch.object(M.signal, 'pidfd_send_signal', create=True), \
                        patch.object(M.select, 'select', return_value=([99], [], [])), \
                        patch.object(M.subprocess, 'run', side_effect=start), patch.object(faults, 'pin', return_value=replacement):
                    if changed:
                        with self.assertRaises(ValueError):
                            faults.crash_and_replace('quality-worker-1')
                    else:
                        evidence = faults.crash_and_replace('quality-worker-1')
                        self.assertTrue(evidence['peer_preserved'])
                        self.assertNotEqual(evidence['old_pid'], evidence['replacement_pid'])

    def test_private_snapshot_is_read_only_and_exactly_scoped(self):
        identity = '00000000-0000-4000-8000-000000000001'
        with patch.object(M, 'boundary', return_value=self.secret):
            faults = M.HostedFaults(2, 3)
        with patch.object(faults, 'postgres', return_value=['owned-psql']), \
                patch.object(M.subprocess, 'run', return_value=types.SimpleNamespace(returncode=0, stdout=json.dumps({'execution_id': identity}))) as run:
            with self.assertRaises(ValueError):
                faults.snapshot(identity)
            run.assert_not_called()
            faults.track(identity)
            faults.snapshot(identity)
            sql = run.call_args.kwargs['input']
            for fragment in ('BEGIN READ ONLY', 'tenant_id=2', "source_task_id='3'", "module='quality'", f"execution_id='{identity}'"):
                self.assertIn(fragment, sql)
            self.assertNotIn('UPDATE ', sql)
            self.assertNotIn('DELETE ', sql)

    def test_lock_rolls_back_on_exception_and_failed_release_is_a_gate_failure(self):
        with patch.object(M, 'boundary', return_value=self.secret):
            faults = M.HostedFaults(2, 3)
        proc = Mock(stdin=io.StringIO(), stdout=io.StringIO('quality-lock-ready\n'), returncode=0)
        with patch.object(faults, 'postgres', return_value=['owned-psql']), patch.object(M.subprocess, 'Popen', return_value=proc), \
                patch.object(M.select, 'select', return_value=([proc.stdout], [], [])):
            with self.assertRaisesRegex(RuntimeError, 'injected'):
                with faults.locked('events'):
                    raise RuntimeError('injected')
        self.assertTrue(proc.stdin.closed)
        proc.wait.assert_called_once_with(timeout=15)

    def test_outer_recovery_rejects_malformed_or_extra_scope(self):
        path = self.secret / M.RECOVERY
        path.write_text(json.dumps({'root': str(self.root), 'workers': [self.row], 'token': 'forbidden'}))
        path.chmod(0o600)
        with patch.object(M, 'boundary', return_value=self.secret), patch.object(M, 'ROOT', self.root), self.assertRaises(ValueError):
            M.resume_paused()
        self.assertTrue(path.exists())


if __name__ == '__main__':
    unittest.main()
