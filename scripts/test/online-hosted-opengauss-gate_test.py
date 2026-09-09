import os
import signal
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("online-hosted-opengauss-gate.sh")
POSTGRES_DOCKERFILE = SCRIPT.parents[1] / "infra/Dockerfile.postgres"


class OnlineHostedOpenGaussGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(
            prefix="addp-online-hosted-opengauss-"
        )
        self.root = Path(self.temporary.name).resolve()
        self.repository = self.root / "repository"
        self.bin = self.root / "bin"
        self.artifacts = self.root / "artifacts"
        self.secrets = self.root / "addp-online-secret-test"
        self.trace = self.root / "trace.log"
        self.background_pids = self.root / "background-pids"
        (self.repository / "scripts/test").mkdir(parents=True)
        (self.repository / "scripts/lib").mkdir(parents=True)
        (self.repository / "scripts/infra").mkdir(parents=True)
        self.bin.mkdir()
        shutil.copy2(SCRIPT, self.repository / "scripts/test/online-hosted-opengauss-gate.sh")
        shutil.copy2(
            SCRIPT.parents[1] / "lib/opengauss-official-media.sh",
            self.repository / "scripts/lib/opengauss-official-media.sh",
        )
        shutil.copy2(
            SCRIPT.parents[1] / "infra/Dockerfile.postgres",
            self.repository / "scripts/infra/Dockerfile.postgres",
        )
        self._executable(
            "uname",
            """
            #!/usr/bin/env bash
            if [ "${1:-}" = "-s" ]; then
              printf '%s\n' "${ADDP_TEST_UNAME_S:-Linux}"
            else
              printf '%s\n' "${ADDP_TEST_UNAME_M:-x86_64}"
            fi
            """,
        )
        self._executable(
            "docker",
            """
            #!/usr/bin/env bash
            if [ "$1 $2" = "compose version" ]; then
              exit 0
            fi
            if [ "$1 $2" = "container inspect" ]; then
              exit 1
            fi
            if [ "$1 $2" = "image inspect" ]; then
              [ "${ADDP_TEST_PREEXIST_IMAGE:-0}" = "1" ]
              exit
            fi
            exit 0
            """,
        )
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        subprocess.run(
            ["git", "config", "user.email", "online@example.invalid"],
            cwd=self.repository,
            check=True,
        )
        subprocess.run(
            ["git", "config", "user.name", "Online Gate Test"],
            cwd=self.repository,
            check=True,
        )
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(
            ["git", "commit", "-qm", "fixture"], cwd=self.repository, check=True
        )

    def tearDown(self) -> None:
        if self.background_pids.exists():
            for value in self.background_pids.read_text(
                encoding="utf-8"
            ).splitlines():
                try:
                    os.kill(int(value), signal.SIGTERM)
                except (ProcessLookupError, ValueError):
                    pass
        self.temporary.cleanup()

    def _executable(self, name: str, content: str) -> None:
        path = self.bin / name
        path.write_text(textwrap.dedent(content).lstrip(), encoding="utf-8")
        path.chmod(0o755)

    def run_check(self, **overrides: str) -> subprocess.CompletedProcess[str]:
        environment = dict(os.environ)
        environment.update(
            {
                "PATH": f"{self.bin}:{environment['PATH']}",
                "GITHUB_ACTIONS": "true",
                "RUNNER_OS": "Linux",
                "RUNNER_TEMP": str(self.root),
                "ADDP_ONLINE_HOST": "1",
                "ADDP_ONLINE_HOSTED": "1",
                "ONLINE_SUITE_INPUT": "opengauss-consumer-flow",
                "ADDP_ONLINE_ARTIFACT_DIR": str(self.artifacts),
                "ADDP_ONLINE_SECRET_DIR": str(self.secrets),
            }
        )
        environment.update(overrides)
        return subprocess.run(
            ["bash", "scripts/test/online-hosted-opengauss-gate.sh", "--check-only"],
            cwd=self.repository,
            env=environment,
            capture_output=True,
            text=True,
        )

    def _write_repository_script(self, relative_path: str, content: str) -> None:
        path = self.repository / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(textwrap.dedent(content).lstrip(), encoding="utf-8")
        path.chmod(0o755)

    def prepare_run_fixture(self) -> None:
        (self.repository / ".env.example").write_text("\n", encoding="utf-8")
        (self.repository / "system/backend").mkdir(parents=True)
        self._write_repository_script(
            "scripts/infra/up.sh",
            """
            #!/usr/bin/env bash
            echo infra-up >> "$ADDP_TEST_GATE_TRACE"
            """,
        )
        self._write_repository_script(
            "scripts/infra/down.sh",
            """
            #!/usr/bin/env bash
            echo infra-down >> "$ADDP_TEST_GATE_TRACE"
            """,
        )
        self._write_repository_script(
            "business/scripts/online-opengauss-consumer-fixture.sh",
            """
            #!/usr/bin/env bash
            echo "fixture:$1" >> "$ADDP_TEST_GATE_TRACE"
            if [ "$1" = start ]; then
              printf '{}\n' > "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"
            fi
            """,
        )
        self._write_repository_script(
            "scripts/dev/start.sh",
            """
            #!/usr/bin/env bash
            echo "start:$*" >> "$ADDP_TEST_GATE_TRACE"
            sleep 30 &
            echo "$!" >> "$ADDP_TEST_BACKGROUND_PIDS"
            echo "started:$*"
            """,
        )
        self._write_repository_script(
            "scripts/dev/stop.sh",
            """
            #!/usr/bin/env bash
            if [ -f "$ADDP_TEST_BACKGROUND_PIDS" ]; then
              while IFS= read -r pid; do
                kill "$pid" 2>/dev/null || true
              done < "$ADDP_TEST_BACKGROUND_PIDS"
            fi
            echo application-stop >> "$ADDP_TEST_GATE_TRACE"
            """,
        )
        self._write_repository_script(
            "scripts/test/online-engine-registration.py",
            """
            #!/usr/bin/env python3
            import argparse

            parser = argparse.ArgumentParser()
            parser.add_argument("--descriptor", required=True)
            parser.add_argument("--output", required=True)
            arguments = parser.parse_args()
            with open(arguments.output, "w", encoding="utf-8") as output:
                output.write("ADDP_ONLINE_CONSUMER_ENGINE_ID=17\\n")
            """,
        )
        self._executable(
            "go",
            """
            #!/usr/bin/env bash
            previous=
            for argument in "$@"; do
              if [ "$previous" = "--output" ]; then
                cat > "$argument" <<'EOF'
            ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN=provisioner-token
            ADDP_ONLINE_TEST_TENANT_ID=2
            ADDP_ONLINE_TEST_USER_ACCESS_TOKEN=consumer-token
            EOF
                exit 0
              fi
              previous=$argument
            done
            exit 2
            """,
        )
        self._executable(
            "make",
            """
            #!/usr/bin/env bash
            echo "make:$*" >> "$ADDP_TEST_GATE_TRACE"
            """,
        )
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(
            ["git", "commit", "-qm", "run fixture"],
            cwd=self.repository,
            check=True,
        )

    def run_gate(self) -> subprocess.CompletedProcess[str]:
        environment = dict(os.environ)
        environment.update(
            {
                "PATH": f"{self.bin}:{environment['PATH']}",
                "GITHUB_ACTIONS": "true",
                "RUNNER_OS": "Linux",
                "RUNNER_TEMP": str(self.root),
                "ADDP_ONLINE_HOST": "1",
                "ADDP_ONLINE_HOSTED": "1",
                "ONLINE_SUITE_INPUT": "opengauss-consumer-flow",
                "ADDP_ONLINE_ARTIFACT_DIR": str(self.artifacts),
                "ADDP_ONLINE_SECRET_DIR": str(self.secrets),
                "ADDP_TEST_GATE_TRACE": str(self.trace),
                "ADDP_TEST_BACKGROUND_PIDS": str(self.background_pids),
            }
        )
        return subprocess.run(
            ["bash", "scripts/test/online-hosted-opengauss-gate.sh"],
            cwd=self.repository,
            env=environment,
            capture_output=True,
            text=True,
            timeout=10,
        )

    def test_check_only_accepts_clean_github_linux_and_separates_secrets(self) -> None:
        result = self.run_check()

        self.assertEqual(result.returncode, 0, result.stderr)
        readiness = (self.artifacts / "readiness.txt").read_text(encoding="utf-8")
        self.assertIn("runner=github-hosted-linux-x86_64", readiness)
        self.assertIn("database=addp_online", readiness)
        self.assertIn("secret_dir_external=true", readiness)
        self.assertFalse(self.secrets.exists())

    def test_rejects_repository_env_before_lifecycle(self) -> None:
        (self.repository / ".env").write_text("POSTGRES_DB=unsafe\n", encoding="utf-8")

        result = self.run_check()

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("forbids a repository root .env", result.stderr)
        self.assertFalse(self.artifacts.exists())

    def test_rejects_shared_artifact_and_secret_directory(self) -> None:
        result = self.run_check(ADDP_ONLINE_SECRET_DIR=str(self.artifacts))

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must be different", result.stderr)

    def test_rejects_preexisting_opengauss_image(self) -> None:
        result = self.run_check(ADDP_TEST_PREEXIST_IMAGE="1")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("refusing to reuse existing image", result.stderr)

    def test_infra_builds_repository_postgres_image_before_image_checks(self) -> None:
        script = (SCRIPT.parents[1] / "infra/up.sh").read_text(encoding="utf-8")

        selection = script.index("BUILD_REPOSITORY_POSTGRES_IMAGE=true")
        build = script.index("docker build --file scripts/infra/Dockerfile.postgres")
        image_checks = script.index("# Images to check")
        self.assertLess(selection, build)
        self.assertLess(build, image_checks)
        self.assertNotIn("postgis/postgis:15-3.4", script)

    def test_postgres_image_uses_supported_debian_base_and_installs_postgis(self) -> None:
        dockerfile = POSTGRES_DOCKERFILE.read_text(encoding="utf-8")

        self.assertIn("ARG POSTGRES_BASE_IMAGE=postgres:15-bookworm", dockerfile)
        self.assertIn("postgresql-15-postgis-3", dockerfile)
        self.assertNotIn("postgis/postgis:", dockerfile)
        self.assertNotIn("Check-Valid-Until=false", dockerfile)

    def test_daemon_launchers_do_not_keep_the_gate_log_pipe_open(self) -> None:
        self.prepare_run_fixture()

        result = self.run_gate()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("started:-manager", result.stdout)
        self.assertIn("started:-develop", result.stdout)
        self.assertIn("started:-service", result.stdout)
        self.assertIn(
            "make:test-online ONLINE_SUITE=opengauss-consumer-flow",
            self.trace.read_text(encoding="utf-8"),
        )
        summary = (self.artifacts / "summary.txt").read_text(encoding="utf-8")
        self.assertIn("result=passed", summary)
        self.assertIn("cleanup=passed", summary)


if __name__ == "__main__":
    unittest.main()
