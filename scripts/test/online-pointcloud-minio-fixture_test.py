import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).parents[2] / "business/scripts/online-pointcloud-minio-fixture.sh"


class OnlinePointCloudMinIOFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-pointcloud-minio-")
        self.root = Path(self.temporary.name)
        self.business = self.root / "business"
        self.bin = self.root / "bin"
        self.state = self.root / "running"
        self.log = self.root / "docker.log"
        (self.business / "scripts").mkdir(parents=True)
        (self.business / "nfs/data/点云").mkdir(parents=True)
        self.bin.mkdir()
        shutil.copy2(SCRIPT, self.business / "scripts/online-pointcloud-minio-fixture.sh")
        (self.business / "docker-compose.yml").write_text("services: {}\n", encoding="utf-8")
        (self.business / "nfs/data/点云/pdal_las12_format0.las").write_bytes(b"LAS fixture")
        self._executable("uname", "#!/bin/bash\necho Darwin\n")
        self._executable(
            "curl",
            "#!/bin/bash\n[ -f \"$ADDP_TEST_CONTAINER_STATE\" ]\n",
        )
        self._executable(
            "docker",
            """#!/bin/bash
printf '%s|%s|%s|%s\n' "$*" "$MINIO_API_PORT" "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >> "$ADDP_TEST_DOCKER_LOG"
case "$1" in
  compose)
    case " $* " in
      *" up -d minio "*) touch "$ADDP_TEST_CONTAINER_STATE" ;;
      *" rm -sf minio "*) rm -f "$ADDP_TEST_CONTAINER_STATE" ;;
    esac
    ;;
  inspect)
    [ -f "$ADDP_TEST_CONTAINER_STATE" ] || exit 1
    case " $* " in
      *"com.docker.compose.project"*) echo "${ADDP_TEST_CONTAINER_OWNERSHIP:-business/minio}" ;;
      *"NetworkSettings.Networks"*) echo business_default ;;
      *) echo true ;;
    esac
    ;;
  run)
    [ -f "$ADDP_TEST_CONTAINER_STATE" ] || exit 1
    ;;
esac
""",
        )
        self.environment = dict(os.environ)
        self.environment.update(
            {
                "PATH": str(self.bin) + os.pathsep + self.environment["PATH"],
                "ADDP_ONLINE_HOST": "1",
                "ADDP_ONLINE_POINTCLOUD_MINIO_PORT": "59002",
                "ADDP_ONLINE_POINTCLOUD_MINIO_ACCESS_KEY": "online-pointcloud",
                "ADDP_ONLINE_POINTCLOUD_MINIO_SECRET_KEY": "pointcloud-secret-1234",
                "ADDP_ONLINE_POINTCLOUD_MINIO_BUCKET": "addp-online",
                "ADDP_ONLINE_POINTCLOUD_MINIO_OBJECT": "pointcloud/pdal_las12_format0.las",
                "ADDP_TEST_CONTAINER_STATE": str(self.state),
                "ADDP_TEST_DOCKER_LOG": str(self.log),
                "MINIO_API_PORT": "9002",
                "MINIO_ROOT_USER": "personal",
                "MINIO_ROOT_PASSWORD": "personal-secret",
            }
        )

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def _executable(self, name: str, content: str) -> None:
        path = self.bin / name
        path.write_text(content, encoding="utf-8")
        path.chmod(0o755)

    def run_fixture(self, action: str, **overrides: str) -> subprocess.CompletedProcess[str]:
        environment = dict(self.environment)
        environment.update(overrides)
        return subprocess.run(
            ["bash", "business/scripts/online-pointcloud-minio-fixture.sh", action],
            cwd=self.root,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_starts_seeds_validates_and_stops_dedicated_minio(self) -> None:
        started = self.run_fixture("start")
        checked = self.run_fixture("status")
        stopped = self.run_fixture("stop")

        self.assertEqual(started.returncode, 0, started.stderr)
        self.assertEqual(checked.returncode, 0, checked.stderr)
        self.assertEqual(stopped.returncode, 0, stopped.stderr)
        commands = self.log.read_text(encoding="utf-8")
        self.assertIn("--env-file /dev/null", commands)
        self.assertIn("up -d minio", commands)
        self.assertIn("minio/mc:latest mb --ignore-existing fixture/addp-online", commands)
        self.assertIn("minio/mc:latest cp --quiet /fixture/source.las fixture/addp-online/pointcloud/pdal_las12_format0.las", commands)
        self.assertIn("|59002|online-pointcloud|pointcloud-secret-1234", commands)
        self.assertNotIn("|9002|personal|personal-secret", commands)
        self.assertFalse((self.business / ".env").exists())
        self.assertFalse(self.state.exists())

    def test_rejects_unsafe_secret_before_docker(self) -> None:
        result = self.run_fixture("start", ADDP_ONLINE_POINTCLOUD_MINIO_SECRET_KEY="bad secret")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("URL-safe", result.stderr)
        self.assertFalse(self.log.exists())

    def test_rejects_object_key_with_dot_segments_before_docker(self) -> None:
        result = self.run_fixture("start", ADDP_ONLINE_POINTCLOUD_MINIO_OBJECT="pointcloud/../source.las")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("dot segments", result.stderr)
        self.assertFalse(self.log.exists())

    def test_rejects_non_las_object_key_before_docker(self) -> None:
        result = self.run_fixture(
            "start", ADDP_ONLINE_POINTCLOUD_MINIO_OBJECT="pointcloud/source.csv"
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must end with .las", result.stderr)
        self.assertFalse(self.log.exists())

    def test_refuses_container_owned_by_another_compose_service(self) -> None:
        self.state.touch()

        result = self.run_fixture("stop", ADDP_TEST_CONTAINER_OWNERSHIP="personal/minio")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not owned", result.stderr)
        self.assertTrue(self.state.exists())


if __name__ == "__main__":
    unittest.main()
