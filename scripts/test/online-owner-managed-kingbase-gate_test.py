import hashlib
import os
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("online-owner-managed-kingbase-gate.sh")
MEDIA_HELPER = SCRIPT.parents[1] / "lib/kingbase-official-media.sh"
POSTGRES_DOCKERFILE = SCRIPT.parents[1] / "infra/Dockerfile.postgres"


class OnlineOwnerManagedKingbaseGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-kingbase-")
        self.root = Path(self.temporary.name).resolve()
        self.repository = self.root / "repository"
        self.bin = self.root / "bin"
        self.artifacts = self.root / "artifacts"
        self.secrets = self.root / "addp-online-secret-test"
        self.license = self.root / "license.dat"
        self.owner_environment = self.root / "kingbase.env"
        (self.repository / "scripts/test").mkdir(parents=True)
        (self.repository / "scripts/lib").mkdir(parents=True)
        (self.repository / "scripts/infra").mkdir(parents=True)
        self.bin.mkdir()
        shutil.copy2(SCRIPT, self.repository / "scripts/test/online-owner-managed-kingbase-gate.sh")
        shutil.copy2(MEDIA_HELPER, self.repository / "scripts/lib/kingbase-official-media.sh")
        shutil.copy2(POSTGRES_DOCKERFILE, self.repository / "scripts/infra/Dockerfile.postgres")
        self.license.write_text("owner-license\n", encoding="utf-8")
        license_sha256 = hashlib.sha256(self.license.read_bytes()).hexdigest()
        self.owner_environment.write_text(
            f"ADDP_KINGBASE_LICENSE_FILE={self.license}\n"
            f"ADDP_KINGBASE_LICENSE_SHA256={license_sha256}\n",
            encoding="utf-8",
        )
        self.owner_environment.chmod(0o600)
        self._executable(
            "uname",
            """
            #!/usr/bin/env bash
            if [ "${1:-}" = "-s" ]; then printf '%s\n' Linux; else printf '%s\n' x86_64; fi
            """,
        )
        self._executable(
            "docker",
            """
            #!/usr/bin/env bash
            if [ "$1 $2" = "compose version" ]; then exit 0; fi
            if [ "$1 $2" = "container inspect" ]; then exit 1; fi
            if [ "$1 $2" = "image inspect" ]; then [ "${ADDP_TEST_PREEXIST_IMAGE:-0}" = 1 ]; exit; fi
            exit 0
            """,
        )
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        subprocess.run(["git", "config", "user.email", "online@example.invalid"], cwd=self.repository, check=True)
        subprocess.run(["git", "config", "user.name", "Online Gate Test"], cwd=self.repository, check=True)
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "fixture"], cwd=self.repository, check=True)

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
                "ADDP_ONLINE_OWNER_MANAGED": "1",
                "ONLINE_SUITE_INPUT": "kingbase-consumer-flow",
                "ADDP_ONLINE_ARTIFACT_DIR": str(self.artifacts),
                "ADDP_ONLINE_SECRET_DIR": str(self.secrets),
                "ADDP_KINGBASE_GATE_ENV_FILE": str(self.owner_environment),
            }
        )
        environment.update(overrides)
        return subprocess.run(
            ["bash", "scripts/test/online-owner-managed-kingbase-gate.sh", "--check-only"],
            cwd=self.repository,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_check_only_accepts_clean_licensed_linux_runner(self) -> None:
        result = self.run_check()

        self.assertEqual(result.returncode, 0, result.stderr)
        readiness = (self.artifacts / "readiness.txt").read_text(encoding="utf-8")
        self.assertIn("runner=owner-managed-linux-x86_64", readiness)
        self.assertIn("media_sha256=16a436608cc204", readiness)
        self.assertIn("license_sha256=", readiness)
        self.assertFalse(self.secrets.exists())

    def test_rejects_group_readable_owner_environment(self) -> None:
        self.owner_environment.chmod(0o640)

        result = self.run_check()

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must not be accessible by group", result.stderr)

    def test_rejects_preexisting_official_image(self) -> None:
        result = self.run_check(ADDP_TEST_PREEXIST_IMAGE="1")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("refusing to reuse existing image", result.stderr)


if __name__ == "__main__":
    unittest.main()
