import importlib
import os
import shutil
import subprocess
import unittest
from pathlib import Path


SUPPORT = importlib.import_module("scripts.test.online-hosted-opengauss-gate_test")
SCRIPT = Path(__file__).with_name("online-hosted-public-origin-gate.sh")
OVERLAY = Path(__file__).with_name("docker-compose.public-origin-t4.yml")


class HostedPublicOriginGateTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.addCleanup(self.host.tearDown)
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / "scripts/test/online-hosted-public-origin-gate.sh")
        shutil.copy2(OVERLAY, self.host.repository / "scripts/test/docker-compose.public-origin-t4.yml")
        (self.host.repository / "docker-compose.yml").write_text("services: {}\n", encoding="utf-8")
        self.host._executable("docker", '''
            #!/usr/bin/env bash
            if [ "$1 $2" = "compose version" ]; then exit 0; fi
            if [ "$1 $2" = "container inspect" ]; then
              [ "${ADDP_TEST_EXISTING_CONTAINER:-}" = "$3" ]; exit
            fi
            if [ "$1 $2" = "inspect -f" ]; then
              printf '%s\n' true
              exit 0
            fi
            if [ "$1 $2" = "volume ls" ] && [ "${ADDP_TEST_EXISTING_PROJECT_VOLUME:-}" = business ] &&
               [[ "$*" == *"com.docker.compose.project=business"* ]]; then
              printf '%s\n' retained-business-volume
              exit 0
            fi
            echo "docker:$*" >> "$ADDP_TEST_GATE_TRACE"
            if [ "${ADDP_TEST_RUNTIME_UP_FAIL:-0}" = 1 ] &&
               [[ "$*" == *"up -d --no-deps --wait --wait-timeout 180 geopython-workflow-engine"* ]]; then
              exit 1
            fi
            exit 0
        ''')
        self.host._executable("curl", "#!/usr/bin/env bash\nexit 0\n")
        self.host._executable("make", '''
            #!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            if [ "$1" = build-images ] && [ "${ADDP_TEST_BUILD_FAIL:-0}" = 1 ]; then exit 1; fi
            if [ "$1" = test-online ] && [ "${ADDP_TEST_SUITE_FAIL:-0}" = 1 ]; then exit 1; fi
        ''')
        subprocess.run(["git", "add", "."], cwd=self.host.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "public origin fixture"], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f"{self.host.bin}:{os.environ['PATH']}", GITHUB_ACTIONS="true",
                   RUNNER_OS="Linux", RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST="1", ADDP_ONLINE_HOSTED="1",
                   ONLINE_SUITE_INPUT="compose-public-origin", ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts),
                   ADDP_ONLINE_SECRET_DIR=str(self.host.secrets), ADDP_TEST_GATE_TRACE=str(self.host.trace),
                   ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids))
        env.update(overrides)
        return subprocess.run(["bash", "scripts/test/online-hosted-public-origin-gate.sh"] + (["--check-only"] if check else []),
                              cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=20)

    def test_preflight_refuses_existing_app_and_personal_environment(self):
        for override in ({"ADDP_TEST_EXISTING_CONTAINER": "registry"}, {"ADDP_TEST_EXISTING_PROJECT_VOLUME": "business"}, {"GITHUB_ACTIONS": "false"}, {"RUNNER_OS": "macOS"}):
            with self.subTest(override=override):
                result = self.run_gate(check=True, **override)
                self.assertNotEqual(result.returncode, 0)
                trace = self.host.trace.read_text() if self.host.trace.exists() else ""
                self.assertNotIn("infra-up", trace)
                self.assertNotIn("up -d", trace)
                self.host.trace.unlink(missing_ok=True)

    def test_builds_platform_and_runtime_images_and_cleans_owned_projects(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        trace = self.host.trace.read_text()
        for step in (
            "infra-up", "make:build BUILD_ARGS=--arch amd64 --services system-backend,gateway",
            "make:build-images IMAGE_BUILD_ARGS=--verify --services system-backend,gateway,console,system-frontend,nginx,geopython-workflow-engine",
            "make:test-online ONLINE_SUITE=compose-public-origin", "docker:compose -f", "docker:rm -fv addp-online-public-origin-upstreams registry", "infra-down",
            "docker:compose -f " + str(self.host.repository / "docker-compose.runtimes.yml") + " up -d --no-deps --wait --wait-timeout 180 geopython-workflow-engine",
            "docker:compose --env-file /dev/null -f " + str(self.host.repository / "business/docker-compose.yml") + " up -d --no-deps --wait --wait-timeout 180 minio",
        ):
            self.assertIn(step, trace)
        self.assertLess(trace.index("make:test-online ONLINE_SUITE=compose-public-origin"), trace.index("business/docker-compose.yml down --remove-orphans --volumes"))
        self.assertLess(trace.index("business/docker-compose.yml down --remove-orphans --volumes"), trace.index("docker-compose.runtimes.yml down --remove-orphans --volumes"))
        self.assertFalse(self.host.secrets.exists())
        self.assertIn("infra_cleanup=zero_residuals", (self.host.artifacts / "summary.txt").read_text())

    def test_scenario_failure_still_cleans_application_and_infra(self):
        result = self.run_gate(ADDP_TEST_SUITE_FAIL="1")
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        self.assertIn("down --remove-orphans --volumes", trace)
        self.assertIn("docker-compose.runtimes.yml down --remove-orphans --volumes", trace)
        self.assertIn("business/docker-compose.yml down --remove-orphans --volumes", trace)
        self.assertIn("docker:rm -fv addp-online-public-origin-upstreams registry", trace)
        self.assertIn("infra-down", trace)
        self.assertFalse(self.host.secrets.exists())

    def test_image_build_failure_cleans_registry_and_infra_before_app_start(self):
        result = self.run_gate(ADDP_TEST_BUILD_FAIL="1")
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        self.assertNotIn("up -d --no-deps", trace)
        self.assertIn("docker:rm -fv addp-online-public-origin-upstreams registry", trace)
        self.assertIn("infra-down", trace)
        self.assertFalse(self.host.secrets.exists())

    def test_runtime_start_failure_cleans_every_owned_project(self):
        result = self.run_gate(ADDP_TEST_RUNTIME_UP_FAIL="1")
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        for project_file in ("docker-compose.runtimes.yml", "business/docker-compose.yml", "docker-compose.yml"):
            self.assertIn(project_file + " down --remove-orphans --volumes", trace)
        self.assertIn("infra-down", trace)
        self.assertFalse(self.host.secrets.exists())


if __name__ == "__main__":
    unittest.main()
