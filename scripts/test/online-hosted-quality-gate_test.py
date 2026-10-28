import importlib
import os
import shutil
import subprocess
import unittest
from pathlib import Path

SUPPORT = importlib.import_module("scripts.test.online-hosted-opengauss-gate_test")
SCRIPT = Path(__file__).with_name("online-hosted-quality-gate.sh")


class HostedQualityGateTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.addCleanup(self.host.tearDown)
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / "scripts/test/online-hosted-quality-gate.sh")
        self.host._executable("docker", '''
            #!/usr/bin/env bash
            if [ "$1 $2" = "container inspect" ]; then
              [ "${ADDP_TEST_EXISTING_CONTAINER:-}" = "$3" ]; exit
            fi
            exit 0
        ''')
        self.host._executable("python3", '''
            #!/usr/bin/env bash
            if [[ "${1:-}" == *online-quality-postgres-fixture.py ]]; then
              echo "source:$2" >> "$ADDP_TEST_GATE_TRACE"
              [ "${ADDP_TEST_FAIL_AT:-}" != "source:$2" ]; exit
            fi
            if [ "${1:-}" = -m ] && [ "${2:-}" = scripts.test.quality-bounded-recovery-faults ]; then
              echo "recovery:$3" >> "$ADDP_TEST_GATE_TRACE"
              [ "${ADDP_TEST_FAIL_AT:-}" != recovery ]; exit
            fi
            if [ "${1:-}" = -m ] && [ "${2:-}" = scripts.test.quality-online-fixture ]; then
              echo "owner:$3" >> "$ADDP_TEST_GATE_TRACE"
              [ "${ADDP_TEST_FAIL_AT:-}" != "owner:$3" ] || exit 1
              if [ "$3" = prepare ]; then
                printf "export ADDP_ONLINE_QUALITY_FIXTURES_JSON='[]'\\n" > "$ADDP_ONLINE_SECRET_DIR/quality-fixtures.env"
              fi
              exit 0
            fi
            exec "$ADDP_TEST_REAL_PYTHON" "$@"
        ''')
        self.host._executable("go", '''
            #!/usr/bin/env bash
            previous=
            for argument in "$@"; do
              if [ "$previous" = --output ]; then
                printf 'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN=provisioner-token\\nADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN=preparer-token\\nADDP_ONLINE_TEST_TENANT_ID=2\\nADDP_ONLINE_TEST_USER_ACCESS_TOKEN=consumer-token\\n' > "$argument"
                exit 0
              fi
              previous=$argument
            done
            exit 1
        ''')
        self.host._executable("make", '''
            #!/usr/bin/env bash
            [ -z "${ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN:-}" ] || exit 1
            [ -z "${ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN:-}" ] || exit 1
            [ "$MONITOR_URL" = http://127.0.0.1:8100 ] || exit 1
            [ "$QUALITY_URL" = http://127.0.0.1:8182 ] || exit 1
            if [ "${ADDP_ONLINE_QUALITY_CASE:-full}" = bounded-recovery ]; then
              [ "$QUALITY_WORKER_LEASE_DURATION" = 30s ] || exit 1
              [ "$QUALITY_WORKER_CONCURRENCY" = 1 ] || exit 1
              [ "$ADDP_DEV_QUALITY_WORKER_REPLICAS" = 2 ] || exit 1
              echo 'case:bounded-recovery' >> "$ADDP_TEST_GATE_TRACE"
            fi
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_FAIL_AT:-}" != suite ]
        ''')
        subprocess.run(["git", "add", "."], cwd=self.host.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "Quality lifecycle fixture"], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        environment = dict(os.environ, PATH=f"{self.host.bin}:{os.environ['PATH']}", GITHUB_ACTIONS="true", RUNNER_OS="Linux",
                           RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST="1", ADDP_ONLINE_HOSTED="1",
                           ONLINE_SUITE_INPUT="quality-dynamic-binding", ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts),
                           ADDP_ONLINE_SECRET_DIR=str(self.host.secrets), ADDP_TEST_GATE_TRACE=str(self.host.trace),
                           ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids), ADDP_TEST_REAL_PYTHON=os.environ.get("ADDP_TEST_REAL_PYTHON", os.sys.executable))
        environment.update(overrides)
        return subprocess.run(["bash", "scripts/test/online-hosted-quality-gate.sh"] + (["--check-only"] if check else []),
                              cwd=self.host.repository, env=environment, capture_output=True, text=True, timeout=20)

    def test_preflight_is_read_only_and_refuses_personal_or_wrong_profile(self):
        result = self.run_gate(check=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.host.trace.exists())
        for overrides in ({"GITHUB_ACTIONS": "false"}, {"ADDP_ONLINE_HOSTED": "0"}, {"ONLINE_SUITE_INPUT": "orchestrator-execution"}):
            with self.subTest(overrides=overrides):
                self.assertNotEqual(self.run_gate(check=True, **overrides).returncode, 0)
        self.assertFalse(self.host.trace.exists())

    def test_success_prepares_then_seeds_and_checks_events_before_destroying_owned_deployment(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = self.host.trace.read_text().splitlines()
        expected = ["infra-up", "source:start", "start:-model -meta -quality -orchestrator -monitor", "owner:prepare",
                    "source:seed", "owner:scan", "make:test-online ONLINE_SUITE=quality-dynamic-binding", "source:verify",
                    "application-stop", "source:stop", "infra-down"]
        self.assertEqual([item for item in trace if item in expected], expected)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn("cleanup=passed", (self.host.artifacts / "summary.txt").read_text())

    def test_existing_source_is_rejected_before_any_lifecycle_action(self):
        result = self.run_gate(check=True, ADDP_TEST_EXISTING_CONTAINER="addp-quality-online-disposable")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.host.trace.exists())

    def test_source_start_failure_cleans_infra_without_starting_application(self):
        result = self.run_gate(ADDP_TEST_FAIL_AT="source:start")
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        self.assertNotIn("start:-model", trace)
        self.assertIn("source:stop", trace)
        self.assertIn("infra-down", trace)
        self.assertFalse(self.host.secrets.exists())

    def test_each_preparation_or_business_failure_still_cleans_its_owned_resources(self):
        for fault in ("owner:prepare", "source:seed", "owner:scan", "suite", "source:verify", "source:stop"):
            with self.subTest(fault=fault):
                result = self.run_gate(ADDP_TEST_FAIL_AT=fault)
                self.assertNotEqual(result.returncode, 0, result.stderr)
                trace = self.host.trace.read_text()
                self.assertIn("application-stop", trace)
                self.assertIn("source:stop", trace)
                self.assertIn("infra-down", trace)
                self.assertFalse(self.host.secrets.exists())
                self.assertIn("result=failed", (self.host.artifacts / "summary.txt").read_text())
                self.host.trace.unlink()

    def test_recovery_case_uses_temporary_configuration_and_recovers_before_outer_shutdown(self):
        result = self.run_gate(ADDP_ONLINE_QUALITY_CASE="bounded-recovery")
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = self.host.trace.read_text().splitlines()
        self.assertIn("case:bounded-recovery", trace)
        self.assertLess(trace.index("recovery:resume"), trace.index("application-stop"))
        self.assertFalse(self.host.secrets.exists())

    def test_recovery_failure_still_destroys_the_owned_deployment_and_marks_failure(self):
        result = self.run_gate(ADDP_ONLINE_QUALITY_CASE="bounded-recovery", ADDP_TEST_FAIL_AT="recovery")
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text().splitlines()
        self.assertLess(trace.index("recovery:resume"), trace.index("application-stop"))
        self.assertIn("infra-down", trace)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn("cleanup=failed", (self.host.artifacts / "summary.txt").read_text())

    def test_unknown_case_is_rejected_before_any_lifecycle_action(self):
        self.assertNotEqual(self.run_gate(check=True, ADDP_ONLINE_QUALITY_CASE="unknown").returncode, 0)
        self.assertFalse(self.host.trace.exists())


if __name__ == "__main__":
    unittest.main()
