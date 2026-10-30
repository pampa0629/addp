#!/usr/bin/env python3
"""Node/npm setup diagnostics and documentation; no network or service use."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
DOCTOR = Path('scripts/dev/doctor.sh')


class DevEnvironmentTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='addp-dev-doctor-')
        self.addCleanup(self.temporary.cleanup)
        self.workspace = Path(self.temporary.name) / 'checkout with spaces'
        self.workspace.mkdir()
        self.bin = Path(self.temporary.name) / 'commands'; self.bin.mkdir()
        self.calls = Path(self.temporary.name) / 'calls'
        for path in (DOCTOR, Path('scripts/utils/node-runtime.sh'), Path('Makefile')):
            target = self.workspace / path; target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / path, target)
        (self.workspace / '.node-version').write_text('24\n')
        (self.workspace / '.env').write_text('touch must-not-source-env\n')
        for name in ('.zshrc', '.bashrc', '.profile', 'package-lock.json'):
            (self.workspace / name).write_text('unchanged\n')
        self.env = dict(os.environ, PATH=f'{self.bin}:/usr/bin:/bin',
                        ADDP_TEST_CALLS=str(self.calls), ADDP_TEST_NODE_VERSION='v24.11.1')
        self.command('node', '''
printf 'node %s\n' "$*" >> "$ADDP_TEST_CALLS"
[ "$*" = --version ] || exit 90
[ "${ADDP_TEST_NODE_FAILURE:-0}" = 0 ] || exit 7
printf '%s\n' "$ADDP_TEST_NODE_VERSION"
''')
        self.command('npm', '''
printf 'npm %s\n' "$*" >> "$ADDP_TEST_CALLS"
[ "$*" = --version ] || exit 90
[ "${ADDP_TEST_NPM_FAILURE:-0}" = 0 ] || exit 7
printf '11.6.2\n'
''')
        for name in ('go', 'brew', 'fnm', 'nvm', 'curl', 'wget', 'docker'):
            self.command(name, 'printf "FORBIDDEN %s\\n" "$0 $*" >> "$ADDP_TEST_CALLS"; exit 99')

    def command(self, name, source):
        path = self.bin / name
        path.write_text('#!/bin/bash\n' + source + '\n'); path.chmod(0o755)

    def run_doctor(self, cwd=None, **environment):
        return subprocess.run(['/bin/bash', str(self.workspace / DOCTOR)],
                              cwd=cwd or self.workspace, env=dict(self.env, **environment),
                              capture_output=True, text=True, timeout=10)

    def assert_read_only_calls(self):
        calls = self.calls.read_text().splitlines() if self.calls.exists() else []
        self.assertTrue(all(call in ('node --version', 'npm --version') for call in calls), calls)
        return calls

    def test_valid_node_and_npm_report_real_paths_without_writes(self):
        before = {str(p.relative_to(self.workspace)): p.read_bytes()
                  for p in self.workspace.rglob('*') if p.is_file()}
        result = self.run_doctor(cwd=self.temporary.name)
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        for expected in ('24', 'v24.11.1', '11.6.2', str(self.bin / 'node'), str(self.bin / 'npm'),
                         str(self.workspace / '.node-version')):
            self.assertIn(expected, result.stdout)
        after = {str(p.relative_to(self.workspace)): p.read_bytes()
                 for p in self.workspace.rglob('*') if p.is_file()}
        self.assertEqual(before, after)
        self.assertIn('npm --version', self.assert_read_only_calls())

    def test_no_version_manager_is_required(self):
        for name in ('brew', 'fnm', 'nvm', 'curl', 'wget', 'docker'):
            (self.bin / name).unlink()
        result = self.run_doctor()
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        self.assert_read_only_calls()

    def test_install_or_version_override_arguments_are_rejected(self):
        result = subprocess.run(['/bin/bash', str(self.workspace / DOCTOR), '--install'],
                                cwd=self.workspace, env=self.env, capture_output=True, text=True, timeout=10)
        self.assertEqual(2, result.returncode)
        self.assertFalse(self.calls.exists())

    def test_wrong_and_prerelease_node_fail_before_running_npm(self):
        for version in ('v25.2.1', 'v22.9.0', 'v24.0.0-rc.1'):
            with self.subTest(version=version):
                result = self.run_doctor(ADDP_TEST_NODE_VERSION=version)
                self.assertNotEqual(0, result.returncode)
                self.assertIn(version, result.stderr)
                self.assertIn('.node-version', result.stdout)
                self.assertIn('https://nodejs.org', result.stdout)
                self.assertNotIn('npm --version', self.assert_read_only_calls())

    def test_root_policy_is_dynamic_and_not_environment_override(self):
        (self.workspace / '.node-version').write_text('26\n')
        result = self.run_doctor(ADDP_TEST_NODE_VERSION='v26.1.0', NODE_VERSION='24')
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        self.assertIn('26', result.stdout)
        self.assertIn('v26.1.0', result.stdout)
        self.assertNotEqual(0, self.run_doctor().returncode)

    def test_missing_or_invalid_policy_fails_closed(self):
        policy = self.workspace / '.node-version'
        for value in ('', 'lts/*\n', '24.11.1\n'):
            policy.write_text(value)
            self.assertNotEqual(0, self.run_doctor().returncode)
        policy.unlink()
        self.assertNotEqual(0, self.run_doctor().returncode)
        self.assertFalse(self.calls.exists())

    def test_missing_node_does_not_require_any_manager(self):
        (self.bin / 'node').unlink()
        for name in ('dirname', 'cat'):
            os.symlink(shutil.which(name), self.bin / name)
        result = self.run_doctor(PATH=str(self.bin))
        self.assertNotEqual(0, result.returncode)
        self.assertIn('node', result.stderr)
        self.assertNotIn('npm --version', self.assert_read_only_calls())

    def test_failed_node_version_is_not_ready(self):
        result = self.run_doctor(ADDP_TEST_NODE_FAILURE='1')
        self.assertNotEqual(0, result.returncode)
        self.assertNotIn('npm --version', self.assert_read_only_calls())

    def test_missing_or_failed_npm_is_not_ready(self):
        self.assertNotEqual(0, self.run_doctor(ADDP_TEST_NPM_FAILURE='1').returncode)
        (self.bin / 'npm').unlink()
        for name in ('dirname', 'cat'):
            os.symlink(shutil.which(name), self.bin / name)
        result = self.run_doctor(PATH=str(self.bin))
        self.assertNotEqual(0, result.returncode)
        self.assertIn('npm', result.stderr)
        self.assert_read_only_calls()

    def test_make_doctor_does_not_probe_go_or_require_valid_node_first(self):
        for version, expected in (('v24.11.1', 0), ('v25.2.1', 2)):
            result = subprocess.run(['make', 'dev-doctor'], cwd=self.workspace,
                                    env=dict(self.env, ADDP_TEST_NODE_VERSION=version),
                                    capture_output=True, text=True, timeout=10)
            self.assertEqual(expected, result.returncode, result.stdout + result.stderr)
            self.assertIn(str(self.bin / 'node'), result.stdout)
        self.assert_read_only_calls()

    def test_deferred_binary_suffix_preserves_cross_build_names(self):
        for os_name, suffix in (('windows', '.exe'), ('linux', '')):
            result = subprocess.run(['make', '-n', 'build-iam-bootstrap', f'GOOS={os_name}', 'GOARCH=amd64'],
                                    cwd=self.workspace, env=self.env, capture_output=True, text=True, timeout=10)
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertIn(f'/$name{suffix} ', result.stdout)
        self.assertFalse(self.calls.exists())

    def test_docs_use_root_policy_and_run_doctor_before_start(self):
        readme = (ROOT / 'README.md').read_text()
        node_requirement = next(line for line in readme.splitlines() if line.startswith('- **Node.js'))
        self.assertIn('.node-version', node_requirement)
        self.assertNotRegex(node_requirement, r'\d+\+')
        self.assertLess(readme.index('make dev-doctor'), readme.index('bash scripts/infra/up.sh'))
        guide = (ROOT / 'docs/guide/addp部署和开发步骤.md').read_text()
        self.assertIn('make dev-doctor', guide)
        self.assertLess(guide.index('make dev-doctor'), guide.index('bash scripts/infra/up.sh'))
        spec = (ROOT / 'docs/spec/addp技术栈规约.md').read_text()
        runtime = next(line for line in spec.splitlines() if line.startswith('- **运行时**: Node.js'))
        self.assertIn('.node-version', runtime)
        self.assertNotRegex(runtime, r'Node\.js \d+')

    def test_standard_and_ci_entries_cover_diagnostics(self):
        make = (ROOT / 'Makefile').read_text()
        self.assertIn('@bash scripts/dev/doctor.sh', make)
        self.assertIn('$(MAKE) test-dev-environment', make)
        workflow = (ROOT / '.github/workflows/platform-ci.yml').read_text()
        self.assertLess(workflow.index('test-dev-environment'), workflow.index('Prepare Agent Swagger environment'))
        self.assertLess(workflow.index('make dev-doctor'), workflow.index('Prepare Agent Swagger environment'))


if __name__ == '__main__':
    unittest.main()
