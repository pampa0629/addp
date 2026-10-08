import importlib
import os
import shutil
import subprocess
import unittest
from pathlib import Path

SUPPORT = importlib.import_module('scripts.test.online-hosted-opengauss-gate_test')
SCRIPT = Path(__file__).with_name('online-hosted-security-gate.sh')


class HostedSecurityGateTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.addCleanup(self.host.tearDown)
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / 'scripts/test/online-hosted-security-gate.sh')
        self.host._executable('npm', '''
            #!/usr/bin/env bash
            [ "$*" = '--prefix console/frontend exec -- playwright install --with-deps chromium' ] || exit 2
            echo "chromium" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_BROWSER_PREPARE_FAIL:-0}" != 1 ]
        ''')
        self.host._write_repository_script('business/scripts/online-security-owner-fixture.sh', '''
            #!/usr/bin/env bash
            echo "security-fixture:$1" >> "$ADDP_TEST_GATE_TRACE"
            [ "$1" != start ] || [ "${ADDP_TEST_FIXTURE_FAIL:-0}" != 1 ]
        ''')
        self.host._executable('go', '''
            #!/usr/bin/env bash
            previous=
            for argument in "$@"; do
              if [ "$previous" = --output ]; then
                cat > "$argument" <<'EOF'
            export ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN=engine-token
            export ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN=initializer-token
            export ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN=source-initializer-token
            export ADDP_ONLINE_TEST_TENANT_ID=2
            export ADDP_ONLINE_TEST_USER_ACCESS_TOKEN=consumer-token
            export ADDP_ONLINE_TEST_USER_USERNAME=external-online-consumer
            export ADDP_ONLINE_TEST_USER_PASSWORD=private-password
            EOF
                exit 0
              fi
              previous=$argument
            done
            exit 2
        ''')
        self.host._write_repository_script('scripts/test/online-engine-registration.py', '''
            import argparse, os
            p = argparse.ArgumentParser()
            p.add_argument('--descriptor'); p.add_argument('--output')
            args = p.parse_args()
            assert os.environ['ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'] == 'engine-token'
            engine = 17 if 'postgres' in args.descriptor else 23
            with open(args.output, 'w') as out:
                out.write(f'export ADDP_ONLINE_CONSUMER_ENGINE_ID={engine}\\n')
        ''')
        self.host._write_repository_script('scripts/test/security-mysql-owner-protection-online.py', '''
            import os, sys
            assert sys.argv[1:] == ['--initialize']
            assert os.environ['ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN'] == 'initializer-token'
            assert os.environ['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'] == 'source-initializer-token'
            with open(os.environ['ADDP_TEST_GATE_TRACE'], 'a') as out: out.write('initialize\\n')
            sys.exit(1 if os.environ.get('ADDP_TEST_INITIALIZE_FAIL') == '1' else 0)
        ''')
        self.host._executable('make', '''
            #!/usr/bin/env bash
            set -euo pipefail
            [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ]
            [ -z "${ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN:-}" ]
            [ -z "${ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN:-}" ]
            [ "$ADDP_ONLINE_TEST_ENGINE_ID" = 17 ]
            [ "$ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID" = 23 ]
            [ "$ADDP_ONLINE_TEST_USER_ACCESS_TOKEN" = consumer-token ]
            [ "$ADDP_ONLINE_TEST_USER_USERNAME" = external-online-consumer ]
            [ "$ADDP_ONLINE_TEST_USER_PASSWORD" = private-password ]
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        subprocess.run(['git', 'add', '.'], cwd=self.host.repository, check=True)
        subprocess.run(['git', 'commit', '-qm', 'test fixture'], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}', GITHUB_ACTIONS='true', RUNNER_OS='Linux',
                   RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1',
                   ONLINE_SUITE_INPUT='security-mysql-owner-protection', ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts),
                   ADDP_ONLINE_SECRET_DIR=str(self.host.secrets), ADDP_TEST_GATE_TRACE=str(self.host.trace),
                   ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        return subprocess.run(['bash', 'scripts/test/online-hosted-security-gate.sh'] + (['--check-only'] if check else []),
                              cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=60)

    def test_admission_and_readiness_never_mutate_services(self):
        for values in ({'GITHUB_ACTIONS': 'false'}, {'RUNNER_OS': 'macOS'}, {'ADDP_ONLINE_HOSTED': '0'},
                       {'ONLINE_SUITE_INPUT': 'compose-public-origin'}):
            with self.subTest(values=values):
                result = self.run_gate(check=True, **values)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.host.trace.exists())
        result = self.run_gate(check=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.host.trace.exists())
        self.assertFalse(self.host.secrets.exists())

    def test_separate_engine_ids_and_preparation_tokens_do_not_leak_into_owner_suite(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        trace = self.host.trace.read_text()
        for target in ('-meta', '-security', '-manager', '-develop', '-service', '-transfer', '-monitor'):
            self.assertIn('start:' + target, trace)
        self.assertIn('initialize', trace)
        self.assertIn('chromium', trace)
        self.assertIn('make:test-online ONLINE_SUITE=security-mysql-owner-protection', trace)
        self.assertLess(trace.index('initialize'), trace.index('make:'))
        self.assertLess(trace.index('chromium'), trace.index('make:'))
        self.assertLess(trace.index('application-stop'), trace.index('security-fixture:stop'))
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('cleanup=passed', (self.host.artifacts / 'summary.txt').read_text())

    def test_failures_destroy_owned_resources(self):
        for key in ('ADDP_TEST_FIXTURE_FAIL', 'ADDP_TEST_BROWSER_PREPARE_FAIL', 'ADDP_TEST_INITIALIZE_FAIL', 'ADDP_TEST_SUITE_FAIL'):
            with self.subTest(stage=key):
                # Each case needs a fresh external secret and evidence directory.
                shutil.rmtree(self.host.artifacts, ignore_errors=True)
                self.host.trace.unlink(missing_ok=True)
                result = self.run_gate(**{key: '1'})
                self.assertNotEqual(result.returncode, 0)
                trace = self.host.trace.read_text()
                self.assertIn('security-fixture:stop', trace)
                self.assertIn('infra-down', trace)
                self.assertFalse(self.host.secrets.exists())
                if key == 'ADDP_TEST_FIXTURE_FAIL':
                    self.assertNotIn('start:-meta', trace)
                else:
                    self.assertIn('application-stop', trace)
                self.assertIn('result=failed', (self.host.artifacts / 'summary.txt').read_text())


if __name__ == '__main__':
    unittest.main()
