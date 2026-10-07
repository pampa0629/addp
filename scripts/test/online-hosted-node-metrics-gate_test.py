import importlib
import os
import shutil
import subprocess
import sys
import unittest
from pathlib import Path

SUPPORT = importlib.import_module("scripts.test.online-hosted-opengauss-gate_test")
SCRIPT = Path(__file__).with_name("online-hosted-node-metrics-gate.sh")


class HostedMetricsGateTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.addCleanup(self.host.tearDown)
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / "scripts/test" / SCRIPT.name)
        self.host._executable("python3", '''
            #!/usr/bin/env bash
            if [[ "${1:-}" == *platform-node-metrics-fixture.py ]]; then
              echo "metrics-fixture:$2" >> "$ADDP_TEST_GATE_TRACE"
              if [ "$2" = prepare ]; then
                [ "${ADDP_TEST_PREPARE_FAIL:-0}" != 1 ] || exit 1
                printf 'export ADDP_OBSERVABILITY_METRICS_ENABLED=true\\nexport ADDP_OBSERVABILITY_LOGS_ENABLED=false\\n' > "$ADDP_ONLINE_SECRET_DIR/metrics.env"
              fi
              [ "$2" != query ] || [ "${ADDP_TEST_QUERY_FAIL:-0}" != 1 ] || exit 1
              [ "$2" != down ] || [ "${ADDP_TEST_CLEANUP_FAIL:-0}" != 1 ]
              exit
            elif [[ "${1:-}" == *scripts/infra/node-metrics.py ]]; then
              echo "node-source:$2" >> "$ADDP_TEST_GATE_TRACE"
              if [ "$2" = down ]; then touch "$ADDP_ONLINE_SECRET_DIR/node-stopped"; fi
              [ "$2" != up ] || [ "${ADDP_TEST_NODE_FAIL:-0}" != 1 ]
              exit
            fi
            exec "$ADDP_TEST_REAL_PYTHON" "$@"
        ''')
        self.host._write_repository_script("scripts/dev/start.sh", '''
            #!/usr/bin/env bash
            [ "$ADDP_OBSERVABILITY_METRICS_ENABLED" = true ] || exit 1
            [ "$ADDP_OBSERVABILITY_LOGS_ENABLED" = false ] || exit 1
            echo "start:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_APP_FAIL:-0}" != 1 ]
        ''')
        self.host._executable("npm", '''
            #!/usr/bin/env bash
            echo "browser-prepare:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_BROWSER_PREPARE_FAIL:-0}" != 1 ]
        ''')
        self.host._executable("make", '''
            #!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "${ADDP_TEST_SUITE_FAIL:-0}" != 1 ]
        ''')
        self.host._executable("docker", '''
            #!/usr/bin/env bash
            if [ "$1 $2" = "container inspect" ]; then exit 1; fi
            if [ -f "$ADDP_ONLINE_SECRET_DIR/node-stopped" ] && [[ "$*" == *label=com.docker.compose.project=addp-node-metrics* ]]; then
              [ "${ADDP_TEST_INSPECTION_FAIL:-0}" != 1 ] || exit 2
              [ "${ADDP_TEST_RESIDUAL_NODE:-0}" != 1 ] || echo residual
            fi
            if [[ "$*" == *"label=com.docker.compose.project=${ADDP_TEST_OCCUPIED_PROJECT:-never}"* ]]; then echo occupied; fi
            exit 0
        ''')
        subprocess.run(["git", "add", "."], cwd=self.host.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "metrics fixture"], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f"{self.host.bin}:{os.environ['PATH']}", GITHUB_ACTIONS="true", RUNNER_OS="Linux",
                   RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST="1", ADDP_ONLINE_HOSTED="1", ONLINE_SUITE_INPUT="platform-node-metrics",
                   ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts), ADDP_ONLINE_SECRET_DIR=str(self.host.secrets),
                   ADDP_TEST_GATE_TRACE=str(self.host.trace), ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids), ADDP_TEST_REAL_PYTHON=sys.executable)
        env.update(overrides)
        return subprocess.run(["bash", "scripts/test/"+SCRIPT.name] + (["--check-only"] if check else []), cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=20)

    def test_readiness_is_readonly_and_rejects_existing_projects(self):
        result = self.run_gate(check=True)
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        self.assertFalse(self.host.trace.exists())
        self.assertFalse(self.host.secrets.exists())
        for project in ("addp-node-metrics", "addp-metrics-online"):
            result = self.run_gate(check=True, ADDP_TEST_OCCUPIED_PROJECT=project)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(self.host.trace.exists())
        result = self.run_gate(check=True, GITHUB_ACTIONS="false")
        self.assertNotEqual(result.returncode, 0)

    def test_success_requires_full_cleanup_and_private_credentials_destroyed(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        trace = self.host.trace.read_text()
        for item in ("infra-up", "metrics-fixture:prepare", "node-source:up", "metrics-fixture:up", "metrics-fixture:query", "start:-monitor",
                     "browser-prepare:--prefix console/frontend exec -- playwright install --with-deps chromium", "make:test-online ONLINE_SUITE=platform-node-metrics", "application-stop", "metrics-fixture:down", "node-source:down", "infra-down"):
            self.assertIn(item, trace)
        self.assertLess(trace.index("metrics-fixture:query"), trace.index("start:-monitor"))
        self.assertEqual(trace.count("infra-up"), 2)
        self.assertLess(trace.index("application-stop"), trace.index("node-source:down"))
        self.assertFalse(self.host.secrets.exists())
        self.assertIn("infra_cleanup=zero_residuals", (self.host.artifacts / "summary.txt").read_text())

    def test_failure_at_each_stage_still_cleans_owned_deployment(self):
        for variable in ("ADDP_TEST_PREPARE_FAIL", "ADDP_TEST_NODE_FAIL", "ADDP_TEST_QUERY_FAIL", "ADDP_TEST_APP_FAIL", "ADDP_TEST_BROWSER_PREPARE_FAIL", "ADDP_TEST_SUITE_FAIL", "ADDP_TEST_CLEANUP_FAIL", "ADDP_TEST_INSPECTION_FAIL", "ADDP_TEST_RESIDUAL_NODE"):
            with self.subTest(variable=variable):
                if self.host.artifacts.exists():
                    shutil.rmtree(self.host.artifacts)
                if self.host.trace.exists():
                    self.host.trace.unlink()
                result = self.run_gate(**{variable: "1"})
                self.assertNotEqual(result.returncode, 0, result.stdout+result.stderr)
                self.assertIn("infra-down", self.host.trace.read_text())
                self.assertFalse(self.host.secrets.exists())
                self.assertIn("result=failed", (self.host.artifacts / "summary.txt").read_text())


if __name__ == "__main__":
    unittest.main()
