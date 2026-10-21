import importlib
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

m = importlib.import_module('business.scripts.online-raster-disk-pressure-fixture')


class DiskPressureFixtureTest(unittest.TestCase):
    def test_start_preallocates_bounded_image_and_formats_without_discard(self):
        with tempfile.TemporaryDirectory() as d:
            runner = Path(d).resolve(); secret = runner/'secret'; secret.mkdir()
            calls = []
            def command(args, **kwargs):
                calls.append(args)
                return '/dev/loop7' if 'losetup' in args else ''
            with patch.dict(os.environ, {'RUNNER_TEMP': str(runner), 'ADDP_ONLINE_TEST_RUN_ID': 'disk-pressure-1-1'}), \
                 patch.object(m.shutil, 'disk_usage', return_value=SimpleNamespace(free=9*m.GIB)), \
                 patch.object(m.os, 'posix_fallocate', create=True) as allocate, patch.object(m, 'command', side_effect=command), \
                 patch.object(m, 'storage'):
                result = m.start_storage(secret)
            self.assertEqual(result['image_bytes'], 4*m.GIB)
            self.assertEqual(allocate.call_args.args[1:], (0, 4*m.GIB))
            mkfs = next(v for v in calls if 'mkfs.ext4' in v)
            self.assertEqual(mkfs[mkfs.index('-E')+1], 'nodiscard')

    def test_actual_space_window_uses_allocated_file_and_release_removes_it(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d); (root/'execution').mkdir()
            for phase in ('distance', 'encoding'):
                desired = m.PRESSURE_FREE[phase]
                reads = iter([desired+10000, desired, desired, desired])
                with patch.object(m, 'storage', return_value=({}, root, root)), \
                     patch.object(m.shutil, 'disk_usage', side_effect=lambda p: SimpleNamespace(free=next(reads), total=m.IMAGE_BYTES)), \
                     patch.object(m.os, 'posix_fallocate', create=True) as allocate:
                    result = m.pressure(None, phase)
                    self.assertEqual(allocate.call_args.args[1:], (0, 10000))
                    self.assertEqual(result['available_bytes'], desired)
                with patch.object(m, 'storage', return_value=({}, root, root)):
                    m.pressure(None, 'release')
                self.assertFalse((root/'pressure.bin').exists())

    def test_pressure_refuses_live_workspaces_and_symlink_filler(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d); work = root/'execution'; work.mkdir(); (work/'active').mkdir()
            with patch.object(m, 'storage', return_value=({}, root, root)), self.assertRaises(m.FixtureError):
                m.pressure(None, 'distance')
            (work/'active').rmdir(); outside = root/'outside'; outside.write_bytes(b'preserve')
            (root/'pressure.bin').symlink_to(outside)
            with patch.object(m, 'storage', return_value=({}, root, root)), self.assertRaises(m.FixtureError):
                m.pressure(None, 'release')
            self.assertEqual(outside.read_bytes(), b'preserve')

    def test_host_guard_rejects_personal_runner_without_mutations(self):
        with patch.object(m, 'command') as cmd, self.assertRaises(Exception):
            m.guard({'GITHUB_ACTIONS': 'false'})
        cmd.assert_not_called()

    def test_storage_cleanup_verifies_backing_file_and_unmount_before_detach(self):
        with tempfile.TemporaryDirectory() as d:
            secret = Path(d)/'secret'; secret.mkdir()
            root = Path(d)/'owned'; root.mkdir(); mount = root/'mount'; mount.mkdir()
            (root/'disk.img').write_bytes(b'owned')
            m.state_path(secret).write_text('{}')
            calls = []; attached = True
            def command(args, **kw):
                nonlocal attached
                calls.append(args)
                if '--json' in args and 'losetup' in args:
                    return json.dumps({'loopdevices': [{'name': '/dev/loop7', 'back-file': str(root/'disk.img')}] if attached else []})
                if 'findmnt' in args:
                    return json.dumps({'filesystems': [{'source': '/dev/loop7', 'fstype': 'ext4', 'target': str(mount)}]})
                if '--detach' in args: attached = False
                return ''
            mounts = iter([True, False])
            with patch.object(m, 'storage', return_value=({'device': '/dev/loop7'}, root, mount)), \
                 patch.object(m, 'command', side_effect=command), patch.object(m.os.path, 'ismount', side_effect=lambda p: next(mounts)):
                result = m.stop_storage(secret)
            self.assertTrue(result['loop_detached']); self.assertFalse(root.exists())
            self.assertLess(next(i for i,v in enumerate(calls) if 'umount' in v), next(i for i,v in enumerate(calls) if '--detach' in v))

    def test_foreign_loop_is_never_unmounted_or_detached(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d); mount = root/'mount'; mount.mkdir()
            m.state_path(root).write_text('{}')
            calls = []
            def command(args, **kw):
                calls.append(args)
                return json.dumps({'loopdevices': [{'name': '/dev/loop7', 'back-file': '/foreign/image'}]})
            with patch.object(m, 'storage', return_value=({'device': '/dev/loop7'}, root, mount)), \
                 patch.object(m, 'command', side_effect=command), self.assertRaises(m.FixtureError):
                m.stop_storage(root)
            self.assertFalse(any('umount' in v or '--detach' in v for v in calls))

    def test_partial_start_without_device_removes_only_owned_root(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)/'owned'; root.mkdir(); secret = Path(d)/'secret'; secret.mkdir()
            m.state_path(secret).write_text('{}')
            with patch.object(m, 'storage', return_value=({'device': None}, root, root/'mount')):
                result = m.stop_storage(secret)
            self.assertTrue(result['image_removed']); self.assertFalse(root.exists())

    def test_storage_refuses_foreign_root_marker_and_symlink_state(self):
        with tempfile.TemporaryDirectory() as d, patch.dict(os.environ, {'RUNNER_TEMP': d, 'ADDP_ONLINE_TEST_RUN_ID': 'disk-pressure-1-1'}):
            secret = Path(d)/'secret'; secret.mkdir()
            root = Path(tempfile.mkdtemp(prefix='addp-disk-pressure-storage-', dir=d))
            (root/'.addp-online-owner').write_text('foreign-run')
            state = m.state_path(secret); state.write_text(json.dumps({'root': str(root), 'device': None})); state.chmod(0o600)
            with self.assertRaises(m.FixtureError): m.storage(secret, mounted=False)
            (root/'.addp-online-owner').write_text('disk-pressure-1-1')
            real = secret/'state-real'; state.rename(real); state.symlink_to(real)
            with self.assertRaises(m.FixtureError): m.storage(secret, mounted=False)

    def test_runtime_refuses_privileged_restart_wrong_mount_or_worker_identity(self):
        import copy
        template = {'Id': 'container', 'Image': 'image', 'RestartCount': 0,
            'State': {'Running': True, 'OOMKilled': False},
            'Config': {'Labels': {'com.addp.online-runtime': m.SUITE, 'com.addp.online-run-id': 'disk-pressure-1-1'},
                       'Cmd': ['sh', 'container_entrypoint.sh'], 'Env': ['TMPDIR=/raster-pressure-tmp']},
            'HostConfig': {'Memory': 2*m.GIB, 'MemorySwap': 2*m.GIB, 'NanoCpus': 4_000_000_000, 'Privileged': False, 'CapAdd': None},
            'Mounts': [{'Type': 'bind', 'Source': '/owned/mount/execution', 'Destination': '/raster-pressure-tmp', 'RW': True}]}
        for change in ('none', 'privileged', 'restart', 'mount', 'swap', 'worker'):
            info = copy.deepcopy(template)
            if change == 'privileged': info['HostConfig']['Privileged'] = True
            if change == 'restart': info['RestartCount'] = 1
            if change == 'mount': info['Mounts'][0]['Source'] = '/foreign/path'
            if change == 'swap': info['HostConfig']['MemorySwap'] = -1
            def command(args, **kwargs):
                return json.dumps([info]) if args[:2] == ['docker', 'inspect'] else json.dumps([[1, '1']] if change == 'worker' else [[1, '1'], [2, '2']])
            with patch.dict(os.environ, {'ADDP_ONLINE_TEST_RUN_ID': 'disk-pressure-1-1'}), patch.object(m, 'guard'), \
                 patch.object(m, 'storage', return_value=({}, Path('/owned'), Path('/owned/mount'))), patch.object(m, 'command', side_effect=command):
                if change == 'none': self.assertEqual(m.runtime_identity()['container_id'], 'container')
                else:
                    with self.subTest(change=change), self.assertRaises(m.FixtureError): m.runtime_identity()


if __name__ == '__main__': unittest.main()
