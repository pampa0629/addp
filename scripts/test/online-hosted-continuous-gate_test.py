from importlib import import_module
import os
from pathlib import Path
import shutil
import subprocess
import unittest

SUPPORT = import_module('scripts.test.online-hosted-opengauss-gate_test')
SCRIPT = Path(__file__).with_name('online-hosted-continuous-gate.sh')


class HostedContinuousGateTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / 'scripts/test' / SCRIPT.name)
        self.host._write_repository_script('business/scripts/online-continuous-fixture.sh', '''
            #!/usr/bin/env bash
            echo "continuous-fixture:$1" >> "$ADDP_TEST_GATE_TRACE"
            if [ "$1" = start ]; then
              printf '{}\\n' > "$ADDP_ONLINE_SECRET_DIR/continuous-pg.json"
              printf '{}\\n' > "$ADDP_ONLINE_SECRET_DIR/continuous-kafka.json"
            fi
            [ "${ADDP_TEST_FIXTURE_FAIL:-}" != "$1" ]
        ''')
        self.host._write_repository_script('scripts/test/online-engine-registration.py', '''
            import os,sys
            from pathlib import Path
            def load_descriptor(path):
                if os.environ.get('ADDP_TEST_DESCRIPTOR_FAIL') == '1': raise ValueError('bad descriptor')
            def main():
                name=Path(sys.argv[2]).name
                with open(os.environ['ADDP_TEST_GATE_TRACE'],'a') as trace: trace.write('engine-register:'+name+'\\n')
                if os.environ.get('ADDP_TEST_REGISTRATION_FAIL') == name: raise SystemExit(1)
                assert os.environ['ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'] == 'provisioner-token'
                engine=17 if name=='continuous-pg.json' else 18
                Path(sys.argv[-1]).write_text(f'export ADDP_ONLINE_CONSUMER_ENGINE_ID={engine}\\n')
            if __name__ == '__main__': main()
        ''')
        self.host._executable('npm', '''
            #!/usr/bin/env bash
            echo "npm:$*" >> "$ADDP_TEST_GATE_TRACE"
            exit 0
        ''')
        self.host._executable('go', '''
            #!/usr/bin/env bash
            previous=
            for argument in "$@"; do
              if [ "$previous" = --output ]; then
                cat > "$argument" <<'EOF'
            export ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN=provisioner-token
            export ADDP_ONLINE_TEST_TENANT_ID=2
            export ADDP_ONLINE_TEST_USER_ACCESS_TOKEN=consumer-token
            EOF
                exit 0
              fi
              previous=$argument
            done
            exit 2
        ''')
        self.host._executable('make', '''
            #!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "$ADDP_ONLINE_CONTINUOUS_PG_ENGINE_ID" = 17 ] &&
              [ "$ADDP_ONLINE_CONTINUOUS_KAFKA_ENGINE_ID" = 18 ] &&
              [ "$TRANSFER_CONTINUOUS_LEASE_DURATION" = 8s ] &&
              [ "$TRANSFER_CONTINUOUS_HEARTBEAT_INTERVAL" = 2s ] &&
              [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ] || exit 1
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        # Pin the actual shell for fake commands; PATH lookup through env is unnecessary.
        for command in self.host.bin.iterdir():
            text = command.read_text()
            command.write_text(text.replace('#!/usr/bin/env bash', '#!/bin/bash', 1))
        subprocess.run(['git', 'add', '.'], cwd=self.host.repository, check=True)
        subprocess.run(['git', 'commit', '-qm', 'continuous fixture'], cwd=self.host.repository, check=True)

    def tearDown(self):
        self.host.tearDown()

    def gate(self, **overrides):
        environment = {**os.environ, 'PATH': f'{self.host.bin}:{os.environ["PATH"]}',
                       'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'RUNNER_TEMP': str(self.host.root),
                       'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1', 'ONLINE_SUITE_INPUT': 'transfer-continuous-events',
                       'ADDP_ONLINE_ARTIFACT_DIR': str(self.host.artifacts), 'ADDP_ONLINE_SECRET_DIR': str(self.host.secrets),
                       'ADDP_TEST_GATE_TRACE': str(self.host.trace), 'ADDP_TEST_BACKGROUND_PIDS': str(self.host.background_pids),
                       **overrides}
        return subprocess.run(['bash', 'scripts/test/' + SCRIPT.name], cwd=self.host.repository,
                              env=environment, capture_output=True, text=True, timeout=45)

    def test_formal_start_identity_two_engines_gate_truth_and_cleanup(self):
        result = self.gate()
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        trace = self.host.trace.read_text()
        for fragment in ['start:-transfer', 'start:-monitor', 'engine-register:continuous-pg.json',
                         'engine-register:continuous-kafka.json', 'make:test-online ONLINE_SUITE=transfer-continuous-events',
                         'continuous-fixture:verify-clean', 'application-stop', 'continuous-fixture:stop', 'infra-down']:
            self.assertIn(fragment, trace)
        self.assertIn('cleanup=passed', (self.host.artifacts / 'summary.txt').read_text())
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('playwright install --with-deps chromium', SCRIPT.read_text())
        self.assertIn('playwright install --with-deps chromium', trace)

    def test_suite_failure_still_destroys_owned_application_business_and_infra(self):
        result = self.gate(ADDP_TEST_SUITE_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        self.assertIn('application-stop', trace)
        self.assertIn('continuous-fixture:stop', trace)
        self.assertIn('infra-down', trace)
        self.assertFalse(self.host.secrets.exists())

    def test_second_engine_registration_failure_does_not_start_acceptance(self):
        result = self.gate(ADDP_TEST_REGISTRATION_FAIL='continuous-kafka.json')
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        self.assertNotIn('make:test-online', trace)
        self.assertIn('continuous-fixture:stop', trace)
        self.assertFalse(self.host.secrets.exists())

    def test_invalid_descriptor_fails_before_application_build_and_cleans_fixture(self):
        result = self.gate(ADDP_TEST_DESCRIPTOR_FAIL='1')
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        self.assertNotIn('start:-transfer', trace)
        self.assertNotIn('make:test-online', trace)
        self.assertIn('continuous-fixture:stop', trace)
        self.assertIn('infra-down', trace)
        self.assertFalse(self.host.secrets.exists())

    def test_local_profile_refused_before_infra_start(self):
        result = self.gate(GITHUB_ACTIONS='false')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.host.trace.exists())


if __name__ == '__main__':
    unittest.main()
