import importlib
import os
import shutil
import subprocess
import unittest
from pathlib import Path

SUPPORT = importlib.import_module("scripts.test.online-hosted-opengauss-gate_test")
SCRIPT = Path(__file__).with_name("online-hosted-orchestrator-gate.sh")


class HostedOrchestratorGateTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.addCleanup(self.host.tearDown)
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / "scripts/test/online-hosted-orchestrator-gate.sh")
        self.host._write_repository_script("business/scripts/online-metric-postgres-fixture.sh", '''
            #!/usr/bin/env bash
            echo "source-fixture:$1" >> "$ADDP_TEST_GATE_TRACE"
            if [ "$1" = start ]; then
              printf '{}\\n' > "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"
              [ "${ADDP_TEST_FIXTURE_FAIL:-0}" != 1 ] || exit 1
            fi
        ''')
        self.host._executable("make", '''
            #!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        self.host._executable("docker", '''
            #!/usr/bin/env bash
            if [ "$1 $2" = "container inspect" ]; then
              [ "${ADDP_TEST_EXISTING_CONTAINER:-}" = "$3" ]; exit
            fi
            exit 0
        ''')
        subprocess.run(["git", "add", "."], cwd=self.host.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "test fixture"], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f"{self.host.bin}:{os.environ['PATH']}", GITHUB_ACTIONS="true", RUNNER_OS="Linux",
                   RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST="1", ADDP_ONLINE_HOSTED="1",
                   ONLINE_SUITE_INPUT="orchestrator-execution", ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts),
                   ADDP_ONLINE_SECRET_DIR=str(self.host.secrets), ADDP_TEST_GATE_TRACE=str(self.host.trace),
                   ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        return subprocess.run(["bash", "scripts/test/online-hosted-orchestrator-gate.sh"] + (["--check-only"] if check else []),
                              cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=20)

    def test_admission_rejects_personal_or_occupied_environment_without_mutation(self):
        for overrides in ({"GITHUB_ACTIONS": "false"}, {"RUNNER_OS": "macOS"}, {"ADDP_ONLINE_HOSTED": "0"},
                          {"ONLINE_SUITE_INPUT": "compose-public-origin"},
                          {"ADDP_TEST_EXISTING_CONTAINER": "addp-metric-online-disposable"}):
            with self.subTest(overrides=overrides):
                result = self.run_gate(check=True, **overrides)
                self.assertNotEqual(result.returncode, 0, result.stderr)
                self.assertFalse(self.host.trace.exists())

    def test_readiness_does_not_start_any_service(self):
        result = self.run_gate(check=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.host.trace.exists())
        self.assertFalse(self.host.secrets.exists())

    def test_owner_startup_and_every_exit_cleanup_use_standard_lifecycle(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        trace = self.host.trace.read_text()
        for item in ("source-fixture:start", "start:-meta", "start:-orchestrator", "start:-monitor",
                     "make:test-online ONLINE_SUITE=orchestrator-execution", "application-stop", "source-fixture:stop", "infra-down"):
            self.assertIn(item, trace)
        self.assertLess(trace.index("application-stop"), trace.index("source-fixture:stop"))
        self.assertFalse(self.host.secrets.exists())
        self.assertIn("infra_cleanup=zero_residuals", (self.host.artifacts / "summary.txt").read_text())

    def test_suite_failure_still_destroys_owned_source_infra_and_credentials(self):
        result = self.run_gate(ADDP_TEST_SUITE_FAIL="1")
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        for item in ("application-stop", "source-fixture:stop", "infra-down"):
            self.assertIn(item, trace)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn("result=failed", (self.host.artifacts / "summary.txt").read_text())

    def test_source_start_failure_cleans_infra_without_starting_application(self):
        result = self.run_gate(ADDP_TEST_FIXTURE_FAIL="1")
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        self.assertNotIn("start:-meta", trace)
        self.assertIn("source-fixture:stop", trace)
        self.assertIn("infra-down", trace)
        self.assertFalse(self.host.secrets.exists())


if __name__ == "__main__":
    unittest.main()
