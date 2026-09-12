import hashlib
import os
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


HELPER = Path(__file__).parents[1] / "lib/kingbase-official-media.sh"
GATE = Path(__file__).with_name("kingbase-official-media-release-gate.sh")


class KingbaseOfficialMediaReleaseGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-kingbase-release-")
        self.root = Path(self.temporary.name)
        self.repository = self.root / "repository"
        self.repository.mkdir()
        self.license = self.root / "license.dat"
        self.license.write_text("owner-license\n", encoding="utf-8")
        self.license_sha256 = hashlib.sha256(self.license.read_bytes()).hexdigest()

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def run_validation(self, license_path: Path, license_sha256: str) -> subprocess.CompletedProcess[str]:
        environment = dict(os.environ)
        environment.update(
            {
                "ADDP_KINGBASE_LICENSE_FILE": str(license_path),
                "ADDP_KINGBASE_LICENSE_SHA256": license_sha256,
            }
        )
        return subprocess.run(
            [
                "bash",
                "-c",
                'source "$1"; kingbase_validate_license_input "$2"',
                "_",
                str(HELPER),
                str(self.repository),
            ],
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_pins_exact_official_media_and_owner_evidence(self) -> None:
        helper = HELPER.read_text(encoding="utf-8")
        gate = GATE.read_text(encoding="utf-8")

        self.assertIn("V009R001C010B0004", helper)
        self.assertIn(
            "16a436608cc204349e510cb136b8fc1fcbdf6874aee7b204cdac20a3522282da",
            helper,
        )
        self.assertIn("kingbase.com.cn/download.html", helper)
        self.assertIn("kingbase_validate_license_input", gate)
        self.assertIn('"license_sha256": license_sha256', gate)
        self.assertIn("docker create", gate)
        self.assertIn("docker rm --force", gate)
        self.assertIn("docker image rm", gate)

    def test_accepts_matching_external_license_and_rejects_repository_copy(self) -> None:
        valid = self.run_validation(self.license, self.license_sha256)
        self.assertEqual(valid.returncode, 0, valid.stderr)

        repository_license = self.repository / "license.dat"
        repository_license.write_bytes(self.license.read_bytes())
        invalid = self.run_validation(repository_license, self.license_sha256)
        self.assertNotEqual(invalid.returncode, 0)
        self.assertIn("outside the repository", invalid.stderr)

    def test_license_staging_is_deleted_after_docker_copy(self) -> None:
        work_dir = self.root / "stage"
        bin_dir = self.root / "bin"
        trace = self.root / "docker.log"
        work_dir.mkdir()
        bin_dir.mkdir()
        docker = bin_dir / "docker"
        docker.write_text(
            textwrap.dedent(
                """
                #!/usr/bin/env bash
                printf '%s\n' "$*" > "$ADDP_TEST_DOCKER_TRACE"
                """
            ).lstrip(),
            encoding="utf-8",
        )
        docker.chmod(0o755)
        environment = dict(os.environ)
        environment.update(
            {
                "PATH": f"{bin_dir}:{environment['PATH']}",
                "ADDP_TEST_DOCKER_TRACE": str(trace),
            }
        )

        result = subprocess.run(
            [
                "bash",
                "-c",
                'source "$1"; kingbase_install_license_into_created_container sample "$2" "$3"',
                "_",
                str(HELPER),
                str(self.license),
                str(work_dir),
            ],
            env=environment,
            capture_output=True,
            text=True,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("cp", trace.read_text(encoding="utf-8"))
        self.assertEqual(list(work_dir.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
