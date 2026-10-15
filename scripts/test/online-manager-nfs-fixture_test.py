import importlib.util
import json
import os
import struct
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPTS = Path(__file__).parents[2] / "business/scripts"


class ContentBoundaryFixtureTest(unittest.TestCase):
    def test_deterministic_point_shapes_and_independent_resources(self):
        spec = importlib.util.spec_from_file_location("content_boundary_fixture", SCRIPTS / "online-content-boundary-fixture.py")
        generator = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(generator)
        with tempfile.TemporaryDirectory() as first, tempfile.TemporaryDirectory() as second:
            for root in (Path(first), Path(second)):
                generator.generate(root)
            contents = lambda root: {str(path.relative_to(root)): path.read_bytes() for path in Path(root).rglob("*") if path.is_file()}
            self.assertEqual(contents(first), contents(second))
            self.assertEqual(len(contents(first)), 41)
            for scope in ("directory-convergence", "refs-convergence"):
                for path in (Path(first) / "scan-boundary/directory").iterdir():
                    self.assertEqual((Path(first) / "scan-boundary" / scope / path.name).read_bytes(), path.read_bytes())
            shape = (Path(first) / "scan-boundary/directory/roads.shp").read_bytes()
            self.assertEqual(struct.unpack_from(">i", shape)[0], 9994)
            self.assertEqual(struct.unpack_from(">i", shape, 24)[0] * 2, len(shape))
            self.assertEqual(struct.unpack_from("<i2d", shape, 108), (1, 120, 30))
            dbf = (Path(first) / "scan-boundary/directory/roads.dbf").read_bytes()
            self.assertEqual(struct.unpack_from("<I2H", dbf, 4), (1, 65, 17))
            self.assertEqual(dbf[65:82], b" roads           ")


class NFSFixtureLifecycleTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.secret = self.root / "secret"
        self.secret.mkdir(mode=0o700)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.exports = self.root / "export"
        self.trace = self.root / "trace"
        self.env = dict(os.environ, PATH=f"{self.bin}:{os.environ['PATH']}",
                        ADDP_ONLINE_HOST="1", ADDP_ONLINE_HOSTED="1", GITHUB_ACTIONS="true", RUNNER_OS="Linux",
                        ADDP_ONLINE_TEST_RUN_ID="manager-42-1", ADDP_ONLINE_SECRET_DIR=str(self.secret),
                        TEST_EXPORT=str(self.exports), TEST_TRACE=str(self.trace))
        self.executable("uname", '#!/bin/sh\nif [ "$1" = -m ]; then echo x86_64; else echo Linux; fi\n')
        self.executable("sudo", '''#!/usr/bin/env python3
import os, sys
from pathlib import Path
args = sys.argv[1:]
export = Path(os.environ['TEST_EXPORT'])
with open(os.environ['TEST_TRACE'], 'a') as trace: trace.write(' '.join(args) + '\\n')
if args[:2] == ['exportfs', '-s']:
    if os.environ.get('TEST_EXISTING_EXPORT') == '1': print('/existing 127.0.0.1')
    elif export.exists(): print(export.read_text().strip())
elif args[0] == 'tee': export.write_text(sys.stdin.read())
elif args[:2] == ['rm', '-f']: export.unlink(missing_ok=True)
elif args[:2] == ['systemctl', 'start'] and os.environ.get('TEST_START_FAIL') == '1': sys.exit(1)
''')
        self.executable("showmount", '''#!/usr/bin/env python3
import os
from pathlib import Path
if os.environ.get('TEST_DISCOVERY_FAIL') != '1': print(Path(os.environ['TEST_EXPORT']).read_text().split()[0] + ' 127.0.0.1')
''')
        self.addCleanup(self.cleanup_fixture)

    def executable(self, name, text):
        path = self.bin / name
        path.write_text(text)
        path.chmod(0o755)

    def invoke(self, action, **environment):
        return subprocess.run(["bash", str(SCRIPTS / "online-manager-nfs-fixture.sh"), action],
                              env=dict(self.env, **environment), capture_output=True, text=True, timeout=10)

    def cleanup_fixture(self):
        if (self.secret / 'nfs-root').exists():
            result = self.invoke("stop")
            self.assertEqual(result.returncode, 0, result.stderr)

    def test_start_stop_exact_export_and_descriptor(self):
        result = self.invoke("start")
        self.assertEqual(result.returncode, 0, result.stderr)
        state = self.secret / "nfs-root"
        root = Path(state.read_text().strip())
        descriptor = json.loads((self.secret / 'manager-nfs-engine.json').read_text())
        self.assertEqual(descriptor['connection_info'], {'server': '127.0.0.1', 'export_path': str(root)})
        self.assertEqual(len(list((root / 'scan-boundary').rglob('*.*'))), 41)
        self.assertIn('127.0.0.1(ro,sync,no_subtree_check,insecure,root_squash)', self.exports.read_text())
        self.assertEqual(self.invoke("stop").returncode, 0)
        self.assertFalse(root.exists())
        self.assertFalse(state.exists())
        self.assertFalse(self.exports.exists())
        self.assertIn('systemctl stop nfs-kernel-server.service rpcbind.service rpcbind.socket', self.trace.read_text())
        self.assertEqual(self.invoke("stop").returncode, 0)

    def test_personal_host_rejected_before_any_privileged_action(self):
        for env in ({'GITHUB_ACTIONS': 'false'}, {'RUNNER_OS': 'macOS'}, {'ADDP_ONLINE_HOSTED': '0'}):
            with self.subTest(env=env):
                self.assertNotEqual(self.invoke("start", **env).returncode, 0)
                self.assertFalse(self.trace.exists())

    def test_existing_export_is_not_owned_or_deleted(self):
        self.assertNotEqual(self.invoke('start', TEST_EXISTING_EXPORT='1').returncode, 0)
        self.assertFalse((self.secret / 'nfs-root').exists())
        self.assertEqual(self.invoke('stop').returncode, 0)
        self.assertNotIn('systemctl', self.trace.read_text())

    def test_partial_start_and_discovery_failure_can_be_cleaned(self):
        for env in ({'TEST_START_FAIL': '1'}, {'TEST_DISCOVERY_FAIL': '1'}):
            with self.subTest(env=env):
                self.assertNotEqual(self.invoke('start', **env).returncode, 0)
                root = Path((self.secret / 'nfs-root').read_text().strip())
                self.assertEqual(self.invoke('stop').returncode, 0)
                self.assertFalse(root.exists())
                self.assertFalse(self.exports.exists())

    def test_cleanup_refuses_a_root_owned_by_another_run(self):
        self.assertEqual(self.invoke('start').returncode, 0)
        root = Path((self.secret / 'nfs-root').read_text().strip())
        owner = root / '.addp-online-owner'
        owner.write_text('another-run\n')
        try:
            self.assertNotEqual(self.invoke('stop').returncode, 0)
            self.assertTrue(root.exists())
            self.assertTrue(self.exports.exists())
        finally:
            owner.write_text('manager-42-1\n')
