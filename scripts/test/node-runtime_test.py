#!/usr/bin/env python3
"""Node policy and fail-before-side-effect regression; never starts services."""
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
HELPER = Path('scripts/utils/node-runtime.sh')


class NodeRuntimeTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='addp-node-runtime-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name) / 'workspace with spaces'
        self.root.mkdir()
        self.bin = self.root / 'fake-bin'
        self.bin.mkdir()
        self.log = self.root / 'effects'
        self.env = dict(os.environ, PATH=f'{self.bin}:/usr/bin:/bin',
                        ADDP_TEST_EFFECTS=str(self.log), ADDP_TEST_NODE_VERSION='v24.11.1')
        self.copy(HELPER)
        (self.root / '.node-version').write_text('24\n')
        self.command('node', '''
if [ "$1" != --version ]; then printf 'unexpected node command\n' >> "$ADDP_TEST_EFFECTS"; exit 90; fi
printf '%s\n' "$ADDP_TEST_NODE_VERSION"
''')
        self.command('npm', 'printf "npm %s\\n" "$*" >> "$ADDP_TEST_EFFECTS"')

    def copy(self, relative):
        target = self.root / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / relative, target)

    def command(self, name, body):
        path = self.bin / name
        path.write_text('#!/bin/bash\n' + body + '\n')
        path.chmod(0o755)

    def run_command(self, args, **environment):
        return subprocess.run(args, cwd=self.root, env=dict(self.env, **environment),
                              text=True, capture_output=True, timeout=10)

    def check(self, **environment):
        return self.run_command(['bash', str(HELPER)], **environment)

    def test_accepts_current_major_and_patch_variation(self):
        for version in ('v24.0.0', 'v24.11.1', 'v24.99.12'):
            with self.subTest(version=version):
                result = self.check(ADDP_TEST_NODE_VERSION=version)
                self.assertEqual(0, result.returncode, result.stderr)
        self.assertFalse(self.log.exists())

    def test_rejects_older_newer_prerelease_and_invalid_output(self):
        for version in ('v22.22.0', 'v25.2.1', 'v26.0.0', 'v24.0.0-rc.1', '24', 'garbage'):
            with self.subTest(version=version):
                result = self.check(ADDP_TEST_NODE_VERSION=version)
                self.assertNotEqual(0, result.returncode)
                self.assertIn('.node-version', result.stderr)
                self.assertIn(version, result.stderr)
        self.assertFalse(self.log.exists())

    def test_reads_root_file_and_does_not_accept_environment_override(self):
        (self.root / '.node-version').write_text('26\n')
        self.assertEqual(0, self.check(ADDP_TEST_NODE_VERSION='v26.1.0').returncode)
        result = self.check(NODE_VERSION='24', ADDP_NODE_VERSION='24')
        self.assertNotEqual(0, result.returncode)
        self.assertIn('26', result.stderr)

    def test_missing_and_invalid_version_file_fail_closed(self):
        version = self.root / '.node-version'
        for value in ('', '0\n', '24.11.1\n', '24\n25\n', 'lts/*\n'):
            version.write_text(value)
            self.assertNotEqual(0, self.check().returncode, value)
        version.unlink()
        self.assertNotEqual(0, self.check().returncode)

    def test_missing_and_failed_node_are_explicit(self):
        (self.bin / 'node').unlink()
        # An isolated PATH contains only commands needed to locate/read policy.
        for name in ('dirname', 'cat'):
            os.symlink(shutil.which(name), self.bin / name)
        result = self.run_command(['/bin/bash', str(HELPER)], PATH=str(self.bin))
        self.assertNotEqual(0, result.returncode)
        self.assertIn('node', result.stderr.lower())
        self.command('node', 'exit 7')
        self.assertNotEqual(0, self.check().returncode)

    def test_start_restart_and_image_build_reject_before_any_side_effect(self):
        for path, args in [('scripts/dev/start.sh', ['-monitor']),
                           ('scripts/dev/restart.sh', ['-monitor']),
                           ('scripts/build/build-images.sh', ['--verify'])]:
            with self.subTest(path=path):
                self.copy(path)
                result = self.run_command(['bash', path, *args], ADDP_TEST_NODE_VERSION='v25.2.1')
                self.assertNotEqual(0, result.returncode)
                self.assertIn('Node.js 24 is required, found v25.2.1', result.stderr)
                self.assertFalse((self.root / '.dev-state').exists())
                self.assertFalse((self.root / 'logs').exists())
                self.assertFalse(self.log.exists())

    def test_install_checks_before_npm_and_preserves_lock(self):
        self.copy('scripts/dev/node-dependencies.sh')
        app = self.root / 'app'; app.mkdir()
        (app / 'package.json').write_text('{}\n')
        lock = app / 'package-lock.json'; lock.write_text('{"locked":true}\n')
        command = ['bash', '-c', 'source scripts/dev/node-dependencies.sh; addp_install_node_dependencies app --omit=dev']
        rejected = self.run_command(command, ADDP_TEST_NODE_VERSION='v25.2.1')
        self.assertNotEqual(0, rejected.returncode)
        self.assertFalse(self.log.exists())
        accepted = self.run_command(command)
        self.assertEqual(0, accepted.returncode, accepted.stderr)
        self.assertEqual('npm ci --omit=dev\n', self.log.read_text())
        self.assertEqual('{"locked":true}\n', lock.read_text())

    def test_batch_install_checks_first_and_preserves_every_lock(self):
        for path in ('scripts/dev/install-frontend-deps.sh', 'scripts/dev/node-dependencies.sh'):
            self.copy(path)
        locks = []
        for module in ('monitor', 'console', 'new-module'):
            app = self.root / module / 'frontend'; app.mkdir(parents=True)
            (app / 'package.json').write_text('{}\n')
            lock = app / 'package-lock.json'; lock.write_text('{"locked":true}\n'); locks.append(lock)
        marker = self.root / 'common-frontend/node_modules/untouched'
        marker.parent.mkdir(parents=True); marker.touch()
        command = ['bash', 'scripts/dev/install-frontend-deps.sh']
        result = self.run_command(command, ADDP_TEST_NODE_VERSION='v25.2.1')
        self.assertNotEqual(0, result.returncode)
        self.assertFalse(self.log.exists())
        self.assertTrue(marker.exists())
        result = self.run_command(command)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(['npm ci'] * 3, self.log.read_text().splitlines())
        self.assertTrue(marker.exists())
        self.assertTrue(all(lock.read_text() == '{"locked":true}\n' for lock in locks))

    def test_explicit_dependency_update_rejects_wrong_node(self):
        self.copy('scripts/dev/update-element-plus.sh')
        result = self.run_command(['bash', 'scripts/dev/update-element-plus.sh'], ADDP_TEST_NODE_VERSION='v25.2.1')
        self.assertNotEqual(0, result.returncode)
        self.assertIn('Node.js 24 is required, found v25.2.1', result.stderr)
        self.assertFalse(self.log.exists())

    def test_notebook_checks_effective_venv_path_before_allocating_build(self):
        for path in ('scripts/dev/jupyter-env.sh', 'scripts/dev/node-dependencies.sh'):
            self.copy(path)
        venv = self.root / 'venv/bin'; venv.mkdir(parents=True)
        node = venv / 'node'; node.write_text('#!/bin/bash\necho v25.2.1\n'); node.chmod(0o755)
        result = self.run_command(
            ['bash', '-c', 'source scripts/dev/jupyter-env.sh; install_jupyter_labextension engine venv'],
            TMPDIR=str(self.root))
        self.assertNotEqual(0, result.returncode)
        self.assertIn('Node.js 24 is required, found v25.2.1', result.stderr)
        self.assertFalse(list(self.root.glob('addp-notebook-bridge.*')))
        self.assertFalse(self.log.exists())

    def test_make_rejects_before_frontend_test_or_build(self):
        self.copy('Makefile')
        (self.root / 'monitor/frontend').mkdir(parents=True)
        result = self.run_command(['make', 'test-monitor-frontend'], ADDP_TEST_NODE_VERSION='v25.2.1')
        self.assertNotEqual(0, result.returncode)
        self.assertIn('Node.js 24 is required, found v25.2.1', result.stderr)
        self.assertFalse(self.log.exists())
        result = self.run_command(['make', 'test-monitor-frontend'])
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(['npm test', 'npm run test:e2e', 'npm run build'], self.log.read_text().splitlines())

    def test_aggregate_make_gates_reject_before_dispatch(self):
        self.copy('Makefile')
        for args in (['test-changed'], ['test-module', 'MODULE=monitor'], ['test-platform'], ['test']):
            with self.subTest(args=args):
                result = self.run_command(['make', *args], ADDP_TEST_NODE_VERSION='v25.2.1')
                self.assertNotEqual(0, result.returncode)
                self.assertIn('Node.js 24 is required, found v25.2.1', result.stderr)
                self.assertFalse(self.log.exists())

    def test_browser_launcher_rejects_before_fixture_allocation(self):
        actual_node = shutil.which('node')
        self.assertIsNotNone(actual_node)
        frontend = self.root / 'monitor/frontend'; frontend.mkdir(parents=True)
        result = subprocess.run(
            [actual_node, str(ROOT / 'scripts/test/frontend-browser-gate.mjs')],
            cwd=frontend, env=dict(self.env, ADDP_TEST_NODE_VERSION='v25.2.1', TMPDIR=str(self.root)),
            text=True, capture_output=True, timeout=10)
        self.assertNotEqual(0, result.returncode)
        self.assertIn('Node.js 24 is required, found v25.2.1', result.stderr)
        self.assertFalse(list(self.root.glob('addp-*-browser-runtime-*')))
        self.assertFalse(list(self.root.glob('addp-*-playwright-results-*')))
        self.assertFalse(self.log.exists())

    def test_all_make_npm_owners_have_shared_preflight(self):
        source = (ROOT / 'Makefile').read_text()
        for match in re.finditer(r'(?m)^([a-zA-Z0-9_-]+):([^\n]*)\n((?:\t[^\n]*\n|\n)*)', source):
            if re.search(r'\b(?:npm|npx)\b', match[3]):
                with self.subTest(target=match[1]):
                    self.assertIn('check-node-runtime', match[2])
        self.assertIn('$(MAKE) test-node-runtime', source)

    def test_keepalive_checks_before_lock_without_executing_it(self):
        source = (ROOT / 'scripts/dev/keepalive.sh').read_text()
        self.assertIn('scripts/utils/node-runtime.sh', source)
        self.assertLess(source.index('addp_require_node_runtime || exit 1'),
                        source.index('addp_acquire_lifecycle_lock'))

    def test_ci_checks_policy_before_dependency_preparation(self):
        source = (ROOT / '.github/workflows/platform-ci.yml').read_text()
        self.assertLess(source.index('make check-node-runtime test-node-runtime'), source.index('Prepare Agent Swagger environment'))
        product = re.search(r'(?ms)^  product-build:\n(.*?)(?=^  [\w-]+:\n|\Z)', source)[1]
        self.assertIn('node-version-file: .node-version', product)
        self.assertLess(product.index('run: make check-node-runtime'), product.index('run: make build'))
        action = (ROOT / '.github/actions/prepare-frontend-gate/action.yml').read_text()
        self.assertIn('node-version-file: .node-version', action)
        self.assertLess(action.index('node-runtime.sh'), action.index('run: npm ci'))


if __name__ == '__main__':
    unittest.main()
