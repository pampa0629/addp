import importlib
import os
import shutil
import subprocess
import unittest
from pathlib import Path

SUPPORT = importlib.import_module("scripts.test.online-hosted-opengauss-gate_test")
SCRIPT = Path(__file__).with_name("online-hosted-metric-gate.sh")


class HostedMetricGateTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / "scripts/test/online-hosted-metric-gate.sh")
        self.host._write_repository_script("business/scripts/online-metric-postgres-fixture.sh", '''
            #!/usr/bin/env bash
            echo "metric-fixture:$1" >> "$ADDP_TEST_GATE_TRACE"
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
        subprocess.run(["git", "add", "."], cwd=self.host.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "metric fixture"], cwd=self.host.repository, check=True)

    def tearDown(self):
        self.host.tearDown()

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f"{self.host.bin}:{os.environ['PATH']}",
                   GITHUB_ACTIONS="true", RUNNER_OS="Linux", RUNNER_TEMP=str(self.host.root),
                   ADDP_ONLINE_HOST="1", ADDP_ONLINE_HOSTED="1", ONLINE_SUITE_INPUT="metric-service-revision-lifecycle",
                   ADDP_ONLINE_METRIC_ENGINE_TYPE="postgresql",
                   ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts), ADDP_ONLINE_SECRET_DIR=str(self.host.secrets),
                   ADDP_TEST_GATE_TRACE=str(self.host.trace), ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        command = ["bash", "scripts/test/online-hosted-metric-gate.sh"] + (["--check-only"] if check else [])
        return subprocess.run(command, cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=20)

    def test_preflight_is_read_only_and_rejects_personal_or_other_profile(self):
        result = self.run_gate(check=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.host.trace.exists())
        for overrides in ({"ADDP_ONLINE_HOSTED": "0"}, {"GITHUB_ACTIONS": "false"},
                          {"ADDP_ONLINE_OWNER_MANAGED": "1"}, {"ONLINE_SUITE_INPUT": "opengauss-consumer-flow"},
                          {"ADDP_ONLINE_METRIC_ENGINE_TYPE": "mysql"},
                          {"ADDP_ONLINE_METRIC_ENGINE_TYPE": ""}):
            result = self.run_gate(check=True, **overrides)
            self.assertNotEqual(result.returncode, 0, result.stderr)
            self.assertFalse(self.host.trace.exists())

    def test_success_starts_required_owners_and_destroys_fixture_and_secrets(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = self.host.trace.read_text()
        for step in ("metric-fixture:start", "start:-model", "start:-service", "start:-security",
                     "make:test-online ONLINE_SUITE=metric-service-revision-lifecycle", "application-stop",
                     "metric-fixture:stop", "infra-down"):
            self.assertIn(step, trace)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn("cleanup=passed", (self.host.artifacts / "summary.txt").read_text())

    def test_tidb_variant_owns_cluster_and_runs_same_online_suite(self):
        self.host._executable("docker", '''
            #!/usr/bin/env bash
            if [ "$1 $2" = "compose version" ]; then exit 0; fi
            if [ "$1 $2" = "container inspect" ] || [ "$1 $2" = "image inspect" ]; then exit 1; fi
            echo "docker:$*" >> "$ADDP_TEST_GATE_TRACE"
            if [[ " $* " == *" mysql "* ]]; then
              if [[ " $* " == *" SELECT 1 "* ]]; then printf '1\\n'; else cat > "$ADDP_TEST_TIDB_SEED_SQL"; fi
            fi
        ''')
        seed = self.host.root / "tidb-seed.sql"
        result = self.run_gate(ADDP_ONLINE_METRIC_ENGINE_TYPE="tidb", ADDP_TEST_TIDB_SEED_SQL=str(seed))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = self.host.trace.read_text()
        self.assertIn("up -d --force-recreate tidb-pd tidb-tikv tidb", trace)
        self.assertIn("make:test-online ONLINE_SUITE=metric-service-revision-lifecycle", trace)
        self.assertIn("down --volumes --remove-orphans", trace)
        self.assertNotIn("metric-fixture:start", trace)
        self.assertIn("CREATE TABLE metric_fixture.metric_facts", seed.read_text())
        self.assertIn("leader TINYINT(1)", seed.read_text())
        self.assertFalse(self.host.secrets.exists())
        self.assertIn("cleanup=passed", (self.host.artifacts / "summary.txt").read_text())

    def test_tidb_preflight_refuses_existing_compose_network(self):
        self.host._executable("docker", '''
            #!/usr/bin/env bash
            if [ "$1 $2" = "network ls" ]; then printf 'foreign-network\\n'; fi
        ''')
        result = self.run_gate(check=True, ADDP_ONLINE_METRIC_ENGINE_TYPE="tidb")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("refuses an existing TiDB network", result.stderr)
        self.assertFalse(self.host.trace.exists())

    def test_tidb_seed_failure_hides_database_error_and_cleans_cluster(self):
        self.host._executable("docker", '''
            #!/usr/bin/env bash
            if [ "$1 $2" = "compose version" ]; then exit 0; fi
            if [ "$1 $2" = "container inspect" ] || [ "$1 $2" = "image inspect" ]; then exit 1; fi
            echo "docker:$*" >> "$ADDP_TEST_GATE_TRACE"
            if [[ " $* " == *" mysql "* ]]; then
              if [[ " $* " == *" SELECT 1 "* ]]; then printf '1\\n'; else
                cat >/dev/null
                echo 'SECRET_MARKER from mysql' >&2
                exit 1
              fi
            fi
        ''')
        result = self.run_gate(ADDP_ONLINE_METRIC_ENGINE_TYPE="tidb")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("metric TiDB seed could not be initialized", result.stdout)
        self.assertNotIn("SECRET_MARKER", result.stdout + result.stderr)
        self.assertIn("down --volumes --remove-orphans", self.host.trace.read_text())
        self.assertFalse(self.host.secrets.exists())

    def test_failures_still_destroy_the_resources_owned_by_this_run(self):
        for flag in ("ADDP_TEST_SUITE_FAIL", "ADDP_TEST_FIXTURE_FAIL"):
            with self.subTest(flag=flag):
                self.host.trace.unlink(missing_ok=True)
                result = self.run_gate(**{flag: "1"})
                self.assertNotEqual(result.returncode, 0)
                trace = self.host.trace.read_text()
                self.assertIn("metric-fixture:stop", trace)
                self.assertIn("infra-down", trace)
                self.assertFalse(self.host.secrets.exists())
                self.assertIn("result=failed", (self.host.artifacts / "summary.txt").read_text())

    def test_rejects_nested_or_preexisting_secret_directory_before_lifecycle(self):
        for path in (self.host.artifacts / "secrets", self.host.root):
            result = self.run_gate(check=True, ADDP_ONLINE_SECRET_DIR=str(path))
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(self.host.trace.exists())
        self.host.secrets.mkdir()
        result = self.run_gate(check=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.host.trace.exists())


if __name__ == "__main__":
    unittest.main()
