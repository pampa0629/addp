import importlib
import os
from pathlib import Path
import shutil
import subprocess
import sys
import unittest

SUPPORT = importlib.import_module('scripts.test.online-hosted-opengauss-gate_test')
SCRIPT = Path(__file__).with_name('online-hosted-execution-retention-gate.sh')


class HostedExecutionRetentionGateTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest(); self.host.setUp()
        self.addCleanup(self.host.tearDown);self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository/'scripts/test'/SCRIPT.name)
        self.host._write_repository_script('scripts/dev/stop-exact-process.sh', '''
            #!/usr/bin/env bash
            echo "stop-exact:$*" >> "$ADDP_TEST_GATE_TRACE"
        ''')
        self.host._write_repository_script('scripts/dev/start.sh', '''
            #!/usr/bin/env bash
            [ -z "${ADDP_ONLINE_TEST_USER_ACCESS_TOKEN:-}" ] || exit 1
            echo "start:$*" >> "$ADDP_TEST_GATE_TRACE"
        ''')
        self.host._executable('go', """
            #!/usr/bin/env bash
            previous=
            for arg in "$@"; do
              if [ "$previous" = --output ]; then
                printf '%s\\n' 'export ADDP_ONLINE_TEST_TENANT_ID=2' 'export ADDP_ONLINE_TEST_USER_ACCESS_TOKEN=consumer-token' 'export ADDP_ONLINE_RETENTION_FOREIGN_TENANT_ID=3' > "$arg"
                exit 0
              fi
              previous=$arg
            done
            exit 1
        """)
        self.host._executable('python3', '''
            #!/usr/bin/env bash
            if [ "${1:-}" = -m ]; then
              [ -n "$ADDP_ONLINE_TEST_USER_ACCESS_TOKEN" ] || exit 1
              echo "retention:$3" >> "$ADDP_TEST_GATE_TRACE"
              [ "${ADDP_TEST_FAIL_PREPARE:-0}" != 1 ] || [ "$3" != prepare ]
              exit
            fi
            exec "$ADDP_TEST_REAL_PYTHON" "$@"
        ''')
        self.host._executable('docker', '''
            #!/usr/bin/env bash
            if [ "$1 $2" = "container inspect" ]; then exit 1; fi
            if [ "$1 $2" = "inspect --format" ]; then echo owned-postgres; fi
            if [ "$1 $2" = "volume ls" ] && [ "${ADDP_TEST_RESIDUAL:-0}" = 1 ]; then
              [ -f "$ADDP_ONLINE_ARTIFACT_DIR/cleanup-started" ] && echo residual
            fi
            exit 0
        ''')
        self.host._executable('make', '''
            #!/usr/bin/env bash
            [ "$ADDP_ONLINE_RETENTION_POSTGRES_ID" = owned-postgres ] || exit 1
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        self.host._write_repository_script('scripts/infra/down.sh', '''
            #!/usr/bin/env bash
            touch "$ADDP_ONLINE_ARTIFACT_DIR/cleanup-started"
            echo infra-down >> "$ADDP_TEST_GATE_TRACE"
        ''')
        subprocess.run(['git','add','.'],cwd=self.host.repository,check=True)
        subprocess.run(['git','commit','-qm','retention fixture'],cwd=self.host.repository,check=True)

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f'{self.host.bin}:{os.environ["PATH"]}', GITHUB_ACTIONS='true', RUNNER_OS='Linux',
                   RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST='1', ADDP_ONLINE_HOSTED='1',
                   ONLINE_SUITE_INPUT='execution-retention-readonly', ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts),
                   ADDP_ONLINE_SECRET_DIR=str(self.host.secrets), ADDP_TEST_GATE_TRACE=str(self.host.trace),
                   ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids), ADDP_TEST_REAL_PYTHON=sys.executable)
        env.update(overrides)
        return subprocess.run(['bash',f'scripts/test/{SCRIPT.name}']+(['--check-only'] if check else []),
                              cwd=self.host.repository,env=env,capture_output=True,text=True,timeout=20)

    def test_real_system_cycle_order_and_disposable_cleanup(self):
        result=self.run_gate();self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        trace=self.host.trace.read_text()
        ordered=['infra-up','start:--exact-process -system','retention:authorize','stop-exact:-system',
                 'retention:prepare','start:--exact-process -system','start:--exact-process -gateway',
                 'make:test-online ONLINE_SUITE=execution-retention-readonly','application-stop','infra-down']
        offset=0
        for action in ordered:
            offset=trace.index(action,offset)+len(action)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('infra_cleanup=zero_residuals', (self.host.artifacts/'summary.txt').read_text())

    def test_event_suite_uses_same_owned_lifecycle_and_its_own_dispatch(self):
        result=self.run_gate(ONLINE_SUITE_INPUT='execution-events-retention')
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
        trace=self.host.trace.read_text()
        ordered=['infra-up','retention:authorize','stop-exact:-system','retention:prepare',
                 'start:--exact-process -system','start:--exact-process -gateway',
                 'make:test-online ONLINE_SUITE=execution-events-retention','application-stop','infra-down']
        offset=0
        for action in ordered:
            offset=trace.index(action,offset)+len(action)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('infra_cleanup=zero_residuals',(self.host.artifacts/'summary.txt').read_text())

    def test_event_suite_failures_still_destroy_only_owned_deployment(self):
        result=self.run_gate(ONLINE_SUITE_INPUT='execution-events-retention',ADDP_TEST_SUITE_FAIL='1')
        self.assertNotEqual(result.returncode,0)
        self.assertIn('infra-down',self.host.trace.read_text())
        self.assertFalse(self.host.secrets.exists())

    def test_unknown_suite_cannot_start_any_service(self):
        result=self.run_gate(ONLINE_SUITE_INPUT='unknown')
        self.assertNotEqual(result.returncode,0)
        self.assertFalse(self.host.trace.exists())

    def test_rejects_nonhosted_before_starting_any_service(self):
        result=self.run_gate(check=True,GITHUB_ACTIONS='false')
        self.assertNotEqual(result.returncode,0)
        self.assertFalse(self.host.trace.exists())

    def test_fixture_failure_blocks_restart_and_still_cleans(self):
        result=self.run_gate(ADDP_TEST_FAIL_PREPARE='1')
        self.assertNotEqual(result.returncode,0)
        trace=self.host.trace.read_text();self.assertNotIn('make:test-online',trace)
        self.assertEqual(trace.count('start:--exact-process -system'),1)
        self.assertIn('infra-down',trace);self.assertFalse(self.host.secrets.exists())

    def test_scenario_failure_still_destroys_owned_infra_and_credentials(self):
        result=self.run_gate(ADDP_TEST_SUITE_FAIL='1')
        self.assertNotEqual(result.returncode,0)
        self.assertIn('infra-down',self.host.trace.read_text());self.assertFalse(self.host.secrets.exists())
        self.assertIn('result=failed',(self.host.artifacts/'summary.txt').read_text())

    def test_cleanup_residuals_cannot_pass(self):
        result=self.run_gate(ADDP_TEST_RESIDUAL='1')
        self.assertNotEqual(result.returncode,0)
        self.assertIn('infra_cleanup=failed',(self.host.artifacts/'summary.txt').read_text())


if __name__=='__main__':
    unittest.main()
