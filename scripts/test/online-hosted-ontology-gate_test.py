import importlib
import os
import shutil
import subprocess
import unittest
from pathlib import Path

SUPPORT = importlib.import_module("scripts.test.online-hosted-opengauss-gate_test")
SCRIPT = Path(__file__).with_name("online-hosted-ontology-gate.sh")


class HostedOntologyGateTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.addCleanup(self.host.tearDown)
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / "scripts/test/online-hosted-ontology-gate.sh")
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
            if [ "$1 $2" = "volume ls" ]; then
              if [ "${ADDP_TEST_EXISTING_VOLUME:-0}" = 1 ] ||
                 { [ "${ADDP_TEST_RESIDUAL:-0}" = 1 ] && [ -f "$ADDP_TEST_GATE_TRACE" ]; }; then
                echo owned-volume
              fi
            fi
            exit 0
        ''')
        subprocess.run(["git", "add", "."], cwd=self.host.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "ontology fixture"], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f"{self.host.bin}:{os.environ['PATH']}", GITHUB_ACTIONS="true",
                   RUNNER_OS="Linux", RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST="1", ADDP_ONLINE_HOSTED="1",
                   ONLINE_SUITE_INPUT="ontology-revision-lifecycle", ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts),
                   ADDP_ONLINE_SECRET_DIR=str(self.host.secrets), ADDP_TEST_GATE_TRACE=str(self.host.trace),
                   ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        return subprocess.run(["bash", "scripts/test/online-hosted-ontology-gate.sh"] + (["--check-only"] if check else []),
                              cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=20)

    def test_preflight_rejects_personal_environment_and_old_graph_resources(self):
        for overrides in ({"GITHUB_ACTIONS": "false"}, {"RUNNER_OS": "macOS"}, {"ADDP_ONLINE_HOSTED": "0"},
                          {"ADDP_TEST_EXISTING_CONTAINER": "addp-falkordb"}, {"ADDP_TEST_EXISTING_VOLUME": "1"},
                          {"COMPOSE_PROJECT_NAME": "another-project"}):
            with self.subTest(overrides=overrides):
                result = self.run_gate(check=True, **overrides)
                self.assertNotEqual(0, result.returncode)
                self.assertFalse(self.host.trace.exists())

    def test_success_starts_ontology_without_business_engine_and_destroys_history(self):
        result = self.run_gate()
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        trace = self.host.trace.read_text()
        for step in ("start:-ontology", "make:test-online ONLINE_SUITE=ontology-revision-lifecycle", "application-stop", "infra-down"):
            self.assertIn(step, trace)
        self.assertNotIn("fixture:start", trace)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn("infra_cleanup=zero_residuals", (self.host.artifacts / "summary.txt").read_text())

    def test_suite_failure_still_destroys_owned_deployment(self):
        result = self.run_gate(ADDP_TEST_SUITE_FAIL="1")
        self.assertNotEqual(0, result.returncode)
        self.assertIn("infra-down", self.host.trace.read_text())
        self.assertFalse(self.host.secrets.exists())
        self.assertIn("result=failed", (self.host.artifacts / "summary.txt").read_text())

    def test_residual_volume_makes_successful_suite_fail(self):
        result = self.run_gate(ADDP_TEST_RESIDUAL="1")
        self.assertNotEqual(0, result.returncode)
        self.assertIn("infra_cleanup=failed", (self.host.artifacts / "summary.txt").read_text())


if __name__ == "__main__":
    unittest.main()
