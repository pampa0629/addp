import importlib
import os
import shutil
import subprocess
import unittest
from pathlib import Path

SUPPORT = importlib.import_module('scripts.test.online-hosted-opengauss-gate_test')
SCRIPT = Path(__file__).with_name('online-hosted-pg-authorization-gate.sh')


class HostedPGAuthorizationTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.addCleanup(self.host.tearDown)
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / 'scripts/test' / SCRIPT.name)
        self.host._write_repository_script('business/scripts/online-metric-postgres-fixture.sh', '''
            #!/usr/bin/env bash
            echo "pg-fixture:$1" >> "$ADDP_TEST_GATE_TRACE"
            if [ "$1" = start ]; then
              printf '{}\\n' > "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"
              [ "${ADDP_TEST_SOURCE_FAIL:-0}" != 1 ]
            fi
        ''')
        self.host._executable('make', '''
            #!/usr/bin/env bash
            [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ] || exit 9
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        self.host._executable('npm', '''
            #!/usr/bin/env bash
            [ "$*" = '--prefix console/frontend exec -- playwright install --with-deps chromium' ] || exit 2
            echo 'browser-install' >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_BROWSER_INSTALL_FAIL:-0}" != 1 ]
        ''')
        subprocess.run(['git', 'add', '.'], cwd=self.host.repository, check=True)
        subprocess.run(['git', 'commit', '-qm', 'PG fixture'], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}', GITHUB_ACTIONS='true',
                   RUNNER_OS='Linux', RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1',
                   ONLINE_SUITE_INPUT='pg-source-authorization', ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts),
                   ADDP_ONLINE_SECRET_DIR=str(self.host.secrets), ADDP_TEST_GATE_TRACE=str(self.host.trace),
                   ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        return subprocess.run(['bash', 'scripts/test/' + SCRIPT.name] + (['--check-only'] if check else []),
                              cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=20)

    def test_check_only_never_starts_or_creates_credentials(self):
        result = self.run_gate(check=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.host.trace.exists())
        self.assertFalse(self.host.secrets.exists())

    def test_refuses_personal_environment_before_starting(self):
        result = self.run_gate(check=True, GITHUB_ACTIONS='false')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.host.trace.exists())

    def test_lifecycle_and_source_failure_and_suite_failure_clean_owned_resources(self):
        # Each call needs a new secret directory; the lifecycle removes it on exit.
        for overrides in ({}, {'ADDP_TEST_SUITE_FAIL': '1'}, {'ADDP_TEST_SOURCE_FAIL': '1'},
                          {'ADDP_TEST_BROWSER_INSTALL_FAIL': '1'}):
            with self.subTest(overrides=overrides):
                result = self.run_gate(**overrides)
                self.assertEqual(result.returncode == 0, not bool(overrides), result.stderr)
                trace = self.host.trace.read_text()
                self.assertIn('pg-fixture:stop', trace)
                self.assertIn('infra-down', trace)
                self.assertFalse(self.host.secrets.exists())
                self.assertIn('infra_cleanup=zero_residuals', (self.host.artifacts / 'summary.txt').read_text())
                if not overrides:
                    self.assertIn('start:-meta', trace)
                    self.assertIn('start:-manager', trace)
                    self.assertIn('browser-install', trace)
                    self.assertLess(trace.index('browser-install'), trace.index('make:test-online'))
                    self.assertIn('make:test-online ONLINE_SUITE=pg-source-authorization', trace)
                    self.assertLess(trace.index('application-stop'), trace.index('pg-fixture:stop'))
                    self.assertNotIn('start:-develop', trace)
                self.host.trace.unlink()


if __name__ == '__main__':
    unittest.main()
