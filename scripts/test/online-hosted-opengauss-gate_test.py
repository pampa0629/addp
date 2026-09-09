import os
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("online-hosted-opengauss-gate.sh")


class OnlineHostedOpenGaussGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(
            prefix="addp-online-hosted-opengauss-"
        )
        self.root = Path(self.temporary.name)
        self.repository = self.root / "repository"
        self.bin = self.root / "bin"
        self.artifacts = self.root / "artifacts"
        self.secrets = self.root / "secrets"
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


if __name__ == "__main__":
    unittest.main()
