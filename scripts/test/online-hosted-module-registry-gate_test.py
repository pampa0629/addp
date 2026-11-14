import importlib
import unittest
import shutil
import subprocess
from pathlib import Path

SUPPORT = importlib.import_module('scripts.test.online-hosted-execution-retention-gate_test')
SCRIPT = Path(__file__).with_name('online-hosted-module-registry-gate.sh')


class HostedModuleRegistryGateTest(unittest.TestCase):
    # Reuse only the owned fake deployment setup, not the retention assertions.
    def setUp(self):
        SUPPORT.HostedExecutionRetentionGateTest.setUp(self)
        shutil.copy2(SCRIPT, self.host.repository / 'scripts/test' / SCRIPT.name)
        self.host._executable('go', '''
            #!/usr/bin/env bash
            previous=
            for arg in "$@"; do
              if [ "$previous" = --output ]; then
                echo 'export ADDP_ONLINE_TEST_TENANT_ID=2' > "$arg"
                echo 'export ADDP_ONLINE_TEST_PLATFORM_ACCESS_TOKEN=platform-token' >> "$arg"
                exit 0
              fi
              previous=$arg
            done
            exit 1
        ''')
        self.host._executable('python3', '''
            #!/usr/bin/env bash
            if [ "$1" = scripts/test/module-lifecycle-process-online.py ]; then
              [ "$ADDP_ONLINE_TEST_PLATFORM_ACCESS_TOKEN" = platform-token ] || exit 1
              phase=$3
              echo "observe:$phase" >> "$ADDP_TEST_GATE_TRACE"
              [ "${ADDP_TEST_FAIL_PHASE:-}" != "$phase" ] || exit 1
              while [ "$#" -gt 0 ]; do
                if [ "$1" = --output ]; then
                  echo '{"manager":{"instance_id":"formal-instance"}}' > "$2"
                  exit 0
                fi
                shift
              done
              exit 1
            fi
            exec "$ADDP_TEST_REAL_PYTHON" "$@"
        ''')
        self.host._executable('make', '''
            #!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
        ''')
        subprocess.run(['git', 'add', '.'], cwd=self.host.repository, check=True)
        subprocess.run(['git', 'commit', '-qm', 'registry lifecycle fixture'], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        # The common helper supplies guarded Hosted environment and cleanup fakes.
        old = SUPPORT.SCRIPT
        SUPPORT.SCRIPT = SCRIPT
        try:
            return SUPPORT.HostedExecutionRetentionGateTest.run_gate(self, check, ONLINE_SUITE_INPUT='module-registry-recovery', **overrides)
        finally:
            SUPPORT.SCRIPT = old

    def test_registry_process_order_and_cleanup(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        actions = self.host.trace.read_text().splitlines()
        expected = ['infra-up', 'start:--exact-process -system', 'stop-exact:-system',
                    'start:--exact-process --wait-live -manager', 'observe:business-before-system',
                    'start:--exact-process -system', 'observe:manager-registered',
                    'start:--exact-process -gateway', 'observe:gateway-established',
                    'stop-exact:-system', 'observe:system-interrupted',
                    'start:--exact-process -system', 'observe:system-recovered',
                    'stop-exact:--force -manager', 'observe:manager-abnormally-stopped',
                    'start:--exact-process -manager', 'observe:manager-restarted',
                    'stop-exact:-manager', 'observe:manager-gracefully-stopped',
                    'make:test-online ONLINE_SUITE=module-registry-recovery', 'application-stop', 'infra-down']
        self.assertEqual(actions, expected)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('infra_cleanup=zero_residuals', (self.host.artifacts / 'summary.txt').read_text())

    def test_fault_failure_still_cleans(self):
        result = self.run_gate(ADDP_TEST_FAIL_PHASE='manager-abnormally-stopped')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn('infra-down', self.host.trace.read_text())
