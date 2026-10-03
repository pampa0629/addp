import importlib
import os
import shutil
import subprocess
import unittest
from pathlib import Path

SUPPORT = importlib.import_module("scripts.test.online-hosted-opengauss-gate_test")
SCRIPT = Path(__file__).with_name("online-hosted-manager-gate.sh")


class HostedManagerGateTest(unittest.TestCase):
    def setUp(self):
        self.host = SUPPORT.OnlineHostedOpenGaussGateTest()
        self.host.setUp()
        self.addCleanup(self.host.tearDown)
        self.host.prepare_run_fixture()
        shutil.copy2(SCRIPT, self.host.repository / "scripts/test/online-hosted-manager-gate.sh")
        self.host._write_repository_script("business/scripts/online-manager-minio-fixture.sh", '''
            #!/usr/bin/env bash
            echo "source-fixture:$1" >> "$ADDP_TEST_GATE_TRACE"
            if [ "$1" = start ]; then
              printf '{}\\n' > "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"
              [ "${ADDP_TEST_FIXTURE_FAIL:-0}" != 1 ] || exit 1
            fi
        ''')
        self.host._write_repository_script("scripts/infra/up.sh", '''
            #!/usr/bin/env bash
            set -euo pipefail
            [ ! -e .env ]
            [ "$ADDP_ONLINE_ENV_FILE" = "$ADDP_ONLINE_SECRET_DIR/runtime.env" ]
            [ -f "$ADDP_ONLINE_ENV_FILE" ]
            [ "$ADDP_RUNTIME_LOG_OWNER" = "$(id -u):$(id -g)" ]
            python3 - <<'PYCREDENTIALS'
            import os
            from pathlib import Path
            secret = Path(os.environ['ADDP_ONLINE_ENV_FILE'])
            assert secret.stat().st_mode & 0o777 == 0o600
            assert len(os.environ['LOKI_READ_TOKEN']) == 64
            assert os.environ['LOKI_READ_TOKEN'] != os.environ['LOKI_WRITE_TOKEN']
            assert not secret.is_relative_to(Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']))
            witness = Path(os.environ['ADDP_TEST_RUNTIME_SECRET_WITNESS'])
            witness.write_text(secret.read_text())
            witness.chmod(0o600)
            PYCREDENTIALS
            echo infra-up >> "$ADDP_TEST_GATE_TRACE"
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
            if [ "$1 $2" = "compose version" ]; then exit 0; fi
            if [ "$1 $2" = "image inspect" ]; then exit 1; fi
            if [ "$1" = ps ] || [ "${2:-}" = ls ]; then exit 0; fi
            echo "docker:$*" >> "$ADDP_TEST_GATE_TRACE"
            exit 0
        ''')
        self.host._write_repository_script("scripts/build/build-images.sh", '''
            #!/usr/bin/env bash
            echo "build:$*" >> "$ADDP_TEST_GATE_TRACE"
            [ "$MODEL3D_CONVERTER_PLATFORM" = linux/amd64 ]
            [ "${ADDP_TEST_BUILD_FAIL:-0}" != 1 ]
        ''')
        self.host._executable("curl", "#!/usr/bin/env bash\nexit 0\n")
        self.host._executable("npm", '''
            #!/usr/bin/env bash
            echo "npm:$*" >> "$ADDP_TEST_GATE_TRACE"
        ''')
        subprocess.run(["git", "add", "."], cwd=self.host.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "test fixture"], cwd=self.host.repository, check=True)

    def run_gate(self, check=False, **overrides):
        env = dict(os.environ, PATH=f"{self.host.bin}:{os.environ['PATH']}", GITHUB_ACTIONS="true", RUNNER_OS="Linux",
                   RUNNER_TEMP=str(self.host.root), ADDP_ONLINE_HOST="1", ADDP_ONLINE_HOSTED="1",
                   ONLINE_SUITE_INPUT="manager-internal-artifact-lineage", ADDP_ONLINE_ARTIFACT_DIR=str(self.host.artifacts),
                   ADDP_ONLINE_SECRET_DIR=str(self.host.secrets), ADDP_TEST_GATE_TRACE=str(self.host.trace),
                   ADDP_TEST_BACKGROUND_PIDS=str(self.host.background_pids),
                   ADDP_TEST_RUNTIME_SECRET_WITNESS=str(self.host.root / "runtime-secret-witness.env"))
        env.update(overrides)
        return subprocess.run(["bash", "scripts/test/online-hosted-manager-gate.sh"] + (["--check-only"] if check else []),
                              cwd=self.host.repository, env=env, capture_output=True, text=True, timeout=60)

    def test_admission_rejects_personal_or_occupied_environment_without_mutation(self):
        for overrides in ({"GITHUB_ACTIONS": "false"}, {"RUNNER_OS": "macOS"}, {"ADDP_ONLINE_HOSTED": "0"},
                          {"ONLINE_SUITE_INPUT": "compose-public-origin"},
                          {"ADDP_TEST_EXISTING_CONTAINER": "business-minio"}):
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
        for item in ("source-fixture:start", "start:-meta", "start:-manager", "start:-monitor", "start:-pointcloud-workflow", "start:-document-workflow", "start:-model3d-workflow",
                     "make:test-online ONLINE_SUITE=manager-internal-artifact-lineage", "application-stop", "source-fixture:stop", "infra-down"):
            self.assertIn(item, trace)
        self.assertLess(trace.index("application-stop"), trace.index("source-fixture:stop"))
        self.assertFalse(self.host.secrets.exists())
        self.assertIn("infra_cleanup=zero_residuals", (self.host.artifacts / "summary.txt").read_text())
        self.assertFalse((self.host.repository / ".env").exists())
        witness = (self.host.root / "runtime-secret-witness.env").read_text()
        evidence = result.stdout + result.stderr + "".join(
            path.read_text() for path in self.host.artifacts.rglob("*") if path.is_file()
        )
        for line in witness.splitlines():
            if line.startswith(("LOKI_READ_TOKEN=", "LOKI_WRITE_TOKEN=", "LOKI_S3_SECRET_KEY=", "LOG_OBSERVER_SERVICE_CLIENT_SECRET=")):
                self.assertTrue(line.split("=", 1)[1] not in evidence, "runtime credential entered artifact")

    def test_suite_failure_still_destroys_owned_source_infra_and_credentials(self):
        result = self.run_gate(ADDP_TEST_SUITE_FAIL="1")
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        for item in ("application-stop", "source-fixture:stop", "infra-down"):
            self.assertIn(item, trace)
        self.assertFalse(self.host.secrets.exists())
        self.assertIn("result=failed", (self.host.artifacts / "summary.txt").read_text())

    def test_build_failure_cleans_registry_without_starting_infra(self):
        result = self.run_gate(ADDP_TEST_BUILD_FAIL="1")
        self.assertNotEqual(result.returncode, 0)
        trace = self.host.trace.read_text()
        self.assertNotIn("infra-up", trace)
        self.assertIn("docker:rm -fv addp-manager-online-registry", trace)
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
