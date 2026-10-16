from importlib import import_module
import os
from pathlib import Path
import shutil
import subprocess
import unittest

SUPPORT = import_module('scripts.test.online-hosted-opengauss-gate_test')
ROOT = Path(__file__).resolve().parents[2]


class HostedHighGoTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.host.prepare_run_fixture()
        shutil.copyfile(ROOT / 'scripts/test/online-hosted-highgo-gate.sh', self.host.repository / 'scripts/test/online-hosted-highgo-gate.sh')
        self.host._write_repository_script('business/scripts/online-highgo-consumer-fixture.py', '''
            import os, sys
            from pathlib import Path
            with open(os.environ['ADDP_TEST_GATE_TRACE'], 'a') as trace:
                trace.write('highgo-fixture:' + sys.argv[1] + '\\n')
            if os.environ.get('ADDP_TEST_FIXTURE_FAIL') == sys.argv[1]: sys.exit(1)
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
            export ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN=source-token
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
            name = Path(sys.argv[2]).name
            with open(os.environ['ADDP_TEST_GATE_TRACE'], 'a') as trace:
                trace.write('register:' + name + '\\n')
            if os.environ.get('ADDP_TEST_REGISTRATION_FAIL') == '1': sys.exit(1)
            assert os.environ['ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'] == 'provisioner-token'
            identifier = 18 if name == 'highgo-pg-engine.json' else 17
            Path(sys.argv[-1]).write_text(f'export ADDP_ONLINE_CONSUMER_ENGINE_ID={identifier}\\n')
        ''')
        self.host._executable('make', '''
            #!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "$ADDP_ONLINE_HIGHGO_ENGINE_ID" = 17 ] &&
              [ "$ADDP_ONLINE_HIGHGO_PG_ENGINE_ID" = 18 ] &&
              [ "$ADDP_ONLINE_TEST_USER_USERNAME" = external-online-consumer ] &&
              [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ] &&
              [ "$ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN" = source-token ] || exit 1
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        self.host._executable('npm', '#!/usr/bin/env bash\necho "browser-install" >> "$ADDP_TEST_GATE_TRACE"\n')
        subprocess.run(['git', 'add', '.'], cwd=self.host.repository, check=True)
        subprocess.run(['git', 'commit', '-qm', 'highgo gate fixture'], cwd=self.host.repository, check=True)

    def tearDown(self):
        self.host.tearDown()

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}',
                   GITHUB_ACTIONS='true', RUNNER_OS='Linux', RUNNER_TEMP=str(self.host.root),
                   ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1', ONLINE_SUITE_INPUT='highgo-consumer-flow',
                   ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts), ADDP_ONLINE_SECRET_DIR=str(self.host.secrets),
                   ADDP_TEST_GATE_TRACE=str(self.host.trace), ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        return subprocess.run(['bash', 'scripts/test/online-hosted-highgo-gate.sh'] + (['--check-only'] if check else []),
                              cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=20)

    def test_preflight_is_read_only_and_rejects_personal_and_wrong_profile(self):
        result = self.run_gate(check=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.host.trace.exists())
        self.assertFalse(self.host.secrets.exists())
        for flags in ({'ADDP_ONLINE_HOSTED': '0'}, {'GITHUB_ACTIONS': 'false'},
                      {'ONLINE_SUITE_INPUT': 'redis-consumer-flow'}, {'ADDP_TEST_UNAME_M': 'aarch64'}):
            result = self.run_gate(check=True, **flags)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(self.host.trace.exists())

    def test_success_registers_two_engines_and_checks_native_then_cleans_everything(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        trace = self.host.trace.read_text()
        order = ['infra-up', 'highgo-fixture:start', 'start:-transfer', 'start:-manager', '--suite highgo-consumer-flow',
                 'register:highgo-engine.json', 'register:highgo-pg-engine.json', 'make:test-online ONLINE_SUITE=highgo-consumer-flow',
                 'highgo-fixture:verify', 'application-stop', 'highgo-fixture:stop', 'infra-down']
        indexes = [trace.index(step) for step in order]
        self.assertEqual(indexes, sorted(indexes))
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('infra_cleanup=zero_residuals', (self.host.artifacts / 'summary.txt').read_text())

    def test_failure_at_each_owned_stage_still_cleans_platform_and_fixture(self):
        for flags in ({'ADDP_TEST_FIXTURE_FAIL': 'start'}, {'ADDP_TEST_IDENTITY_FAIL': '1'},
                      {'ADDP_TEST_REGISTRATION_FAIL': '1'}, {'ADDP_TEST_SUITE_FAIL': '1'}, {'ADDP_TEST_FIXTURE_FAIL': 'verify'}):
            with self.subTest(flags=flags):
                self.host.trace.unlink(missing_ok=True)
                result = self.run_gate(**flags)
                self.assertNotEqual(result.returncode, 0)
                trace = self.host.trace.read_text()
                self.assertIn('highgo-fixture:stop', trace)
                self.assertIn('infra-down', trace)
                self.assertFalse(self.host.secrets.exists())


if __name__ == '__main__':
    unittest.main()
