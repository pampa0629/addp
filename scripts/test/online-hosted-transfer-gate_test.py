import importlib
import os
import shutil
import subprocess
import unittest
from pathlib import Path

SUPPORT = importlib.import_module('scripts.test.online-hosted-opengauss-gate_test')
SCRIPT = Path(__file__).with_name('online-hosted-transfer-gate.sh')


class HostedTransferGateTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / 'scripts/test/online-hosted-transfer-gate.sh')
        self.host._write_repository_script('business/scripts/online-transfer-relational-sql-etl-fixture.sh', '''
            #!/usr/bin/env bash
            echo "transfer-fixture:$1" >> "$ADDP_TEST_GATE_TRACE"
            if [ "$1" = start ]; then
              printf '{}\\n' > "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"
              printf '{}\\n' > "$ADDP_ONLINE_FIXTURE_MONGODB_ENGINE_DESCRIPTOR_FILE"
            fi
            [ "${ADDP_TEST_FIXTURE_FAIL:-}" != "$1" ]
        ''')
        self.host._executable('go', '''
            #!/usr/bin/env bash
            echo "identity:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_IDENTITY_FAIL:-0}" != 1 ] || exit 1
            previous=
            for argument in "$@"; do
              if [ "$previous" = --output ]; then
                cat > "$argument" <<'EOF'
            export ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN=provisioner-token
            export ADDP_ONLINE_TEST_TENANT_ID=2
            export ADDP_ONLINE_TEST_USER_ACCESS_TOKEN=consumer-token
            export ADDP_ONLINE_TEST_USER_USERNAME=external-online-consumer
            export ADDP_ONLINE_TEST_USER_PASSWORD=consumer-password
            EOF
                exit 0
              fi
              previous=$argument
            done
            exit 2
        ''')
        self.host._write_repository_script('scripts/test/online-engine-registration.py', '''
            import os, sys
            from pathlib import Path
            with open(os.environ['ADDP_TEST_GATE_TRACE'], 'a') as trace:
                trace.write('engine-register:' + Path(sys.argv[2]).name + '\\n')
            if os.environ.get('ADDP_TEST_REGISTRATION_FAIL') == '1':
                sys.exit(1)
            assert os.environ['ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'] == 'provisioner-token'
            mongodb = Path(sys.argv[2]).name == 'transfer-mongodb-engine.json'
            if mongodb and os.environ.get('ADDP_TEST_MONGODB_REGISTRATION_FAIL') == '1':
                sys.exit(1)
            identifier = 18 if mongodb else 17
            Path(sys.argv[-1]).write_text(f'export ADDP_ONLINE_CONSUMER_ENGINE_ID={identifier}\\n')
        ''')
        self.host._executable('make', '''
            #!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "$ADDP_ONLINE_TEST_ENGINE_ID" = 17 ] &&
              [ "$ADDP_ONLINE_TEST_ENGINE_NAME" = 'Hosted Transfer PostgreSQL' ] &&
              [ "$ADDP_ONLINE_TEST_MONGODB_ENGINE_ID" = 18 ] &&
              [ "$ADDP_ONLINE_TEST_MONGODB_ENGINE_NAME" = 'Hosted Transfer MongoDB' ] &&
              [ -z "${TRANSFER_MONGODB_PASSWORD:-}" ] &&
              [ "$ADDP_ONLINE_TEST_USER_USERNAME" = external-online-consumer ] &&
              [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ] &&
              [ -z "${TRANSFER_FIXTURE_PASSWORD:-}" ] || exit 1
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        self.host._executable('npm', '''
            #!/usr/bin/env bash
            echo "npm:$*" >> "$ADDP_TEST_GATE_TRACE"
        ''')
        subprocess.run(['git', 'add', '.'], cwd=self.host.repository, check=True)
        subprocess.run(['git', 'commit', '-qm', 'transfer fixture'], cwd=self.host.repository, check=True)

    def tearDown(self):
        self.host.tearDown()

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f"{self.host.bin}:{os.environ['PATH']}",
                   GITHUB_ACTIONS='true', RUNNER_OS='Linux', RUNNER_TEMP=str(self.host.root),
                   ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1', ONLINE_SUITE_INPUT='transfer-relational-sql-etl',
                   ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts), ADDP_ONLINE_SECRET_DIR=str(self.host.secrets),
                   ADDP_TEST_GATE_TRACE=str(self.host.trace), ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        command = ['bash', 'scripts/test/online-hosted-transfer-gate.sh'] + (['--check-only'] if check else [])
        return subprocess.run(command, cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=20)

    def test_preflight_is_read_only_and_refuses_personal_and_other_profiles(self):
        result = self.run_gate(check=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.host.trace.exists())
        self.assertFalse(self.host.secrets.exists())
        for overrides in ({'ADDP_ONLINE_HOSTED': '0'}, {'GITHUB_ACTIONS': 'false'},
                          {'ADDP_ONLINE_OWNER_MANAGED': '1'}, {'ONLINE_SUITE_INPUT': 'opengauss-consumer-flow'},
                          {'ADDP_TEST_UNAME_S': 'Darwin'}):
            result = self.run_gate(check=True, **overrides)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(self.host.trace.exists())

    def test_success_uses_same_suite_then_verifies_rows_and_zero_residuals(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        trace = self.host.trace.read_text()
        sequence = ('infra-up', 'transfer-fixture:start', 'start:-transfer', 'start:-manager', 'start:-develop', 'start:-orchestrator', 'start:-monitor',
                    'playwright install --with-deps chromium', '--suite transfer-relational-sql-etl',
                    'engine-register:transfer-engine.json', 'engine-register:transfer-mongodb-engine.json', 'make:test-online ONLINE_SUITE=transfer-relational-sql-etl',
                    'transfer-fixture:verify', 'application-stop', 'transfer-fixture:stop', 'infra-down')
        indices = [trace.index(step) for step in sequence]
        self.assertEqual(indices, sorted(indices))
        self.assertNotIn('start:-all', trace)
        self.assertFalse(self.host.secrets.exists())
        summary = (self.host.artifacts / 'summary.txt').read_text()
        self.assertIn('result=passed', summary)
        self.assertIn('infra_cleanup=zero_residuals', summary)

    def test_failures_destroy_owned_resources(self):
        for flags in ({'ADDP_TEST_FIXTURE_FAIL': 'start'}, {'ADDP_TEST_IDENTITY_FAIL': '1'},
                      {'ADDP_TEST_REGISTRATION_FAIL': '1'}, {'ADDP_TEST_MONGODB_REGISTRATION_FAIL': '1'}, {'ADDP_TEST_SUITE_FAIL': '1'},
                      {'ADDP_TEST_FIXTURE_FAIL': 'verify'}):
            with self.subTest(flags=flags):
                self.host.trace.unlink(missing_ok=True)
                result = self.run_gate(**flags)
                self.assertNotEqual(result.returncode, 0)
                trace = self.host.trace.read_text()
                self.assertIn('transfer-fixture:stop', trace)
                self.assertIn('infra-down', trace)
                self.assertFalse(self.host.secrets.exists())
                self.assertIn('result=failed', (self.host.artifacts / 'summary.txt').read_text())

    def test_cleanup_failure_cannot_be_reported_as_passed(self):
        result = self.run_gate(ADDP_TEST_FIXTURE_FAIL='stop')
        self.assertNotEqual(result.returncode, 0)
        summary = (self.host.artifacts / 'summary.txt').read_text()
        self.assertIn('cleanup=failed', summary)
        self.assertIn('result=failed', summary)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('infra-down', self.host.trace.read_text())

    def test_interrupt_still_cleans_the_owned_fixture(self):
        self.host._executable('make', '''
            #!/usr/bin/env bash
            echo suite-interrupted >> "$ADDP_TEST_GATE_TRACE"
            kill -TERM "$PPID"
        ''')
        result = self.run_gate()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('transfer-fixture:stop', self.host.trace.read_text())
        self.assertIn('infra-down', self.host.trace.read_text())
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('result=failed', (self.host.artifacts / 'summary.txt').read_text())


if __name__ == '__main__':
    unittest.main()
