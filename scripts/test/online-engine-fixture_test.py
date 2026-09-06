import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).parents[2] / "business/scripts/online-engine-fixture.sh"


class OnlineEngineFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-engine-fixture-")
        self.root = Path(self.temporary.name)
        self.business = self.root / "business"
        self.bin = self.root / "bin"
        self.state = self.root / "running"
        self.log = self.root / "docker.log"
        (self.business / "scripts").mkdir(parents=True)
        self.bin.mkdir()
        shutil.copy2(SCRIPT, self.business / "scripts/online-engine-fixture.sh")
        (self.business / "docker-compose.yml").write_text("services: {}\n", encoding="utf-8")
        self._executable(
            "uname",
            """#!/bin/bash
if [ "$1" = "-s" ]; then echo Darwin; else echo arm64; fi
""",
        )
        self._executable(
            "docker",
            """#!/bin/bash
printf '%s|%s|%s|%s|%s|%s\n' "$*" "$POSTGRES_PORT" "$POSTGRES_USER" "$POSTGRES_PASSWORD" "$POSTGRES_DB" "$POSTGRES_IMAGE" >> "$ADDP_TEST_DOCKER_LOG"
case "$1" in
  compose)
    case " $* " in
      *" up -d postgres "*) touch "$ADDP_TEST_CONTAINER_STATE" ;;
      *" rm -sf postgres "*) rm -f "$ADDP_TEST_CONTAINER_STATE" ;;
    esac
    ;;
  inspect)
    [ -f "$ADDP_TEST_CONTAINER_STATE" ] || exit 1
    case " $* " in
      *"com.docker.compose.project"*) echo "${ADDP_TEST_CONTAINER_OWNERSHIP:-business/postgres}" ;;
      *) echo true ;;
    esac
    ;;
  exec)
    [ -f "$ADDP_TEST_CONTAINER_STATE" ]
    ;;
esac
""",
        )
        self.environment = dict(os.environ)
        self.environment.update(
            {
                "PATH": str(self.bin) + os.pathsep + self.environment["PATH"],
                "ADDP_ONLINE_HOST": "1",
                "ADDP_ONLINE_TEST_ENGINE_PORT": "55433",
                "ADDP_ONLINE_TEST_ENGINE_USER": "fixture_user",
                "ADDP_ONLINE_TEST_ENGINE_PASSWORD": "fixture_password",
                "ADDP_ONLINE_TEST_ENGINE_DATABASE": "fixture_database",
                "ADDP_TEST_CONTAINER_STATE": str(self.state),
                "ADDP_TEST_DOCKER_LOG": str(self.log),
                "POSTGRES_PORT": "15432",
                "POSTGRES_USER": "platform_user",
                "POSTGRES_PASSWORD": "platform_password",
                "POSTGRES_DB": "addp_online",
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
            ["bash", "business/scripts/online-engine-fixture.sh", action],
            cwd=self.root,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_uses_isolated_fixture_variables_and_never_platform_database_values(self) -> None:
        started = self.run_fixture("start")
        stopped = self.run_fixture("stop")

        self.assertEqual(started.returncode, 0, started.stderr)
        self.assertEqual(stopped.returncode, 0, stopped.stderr)
        commands = self.log.read_text(encoding="utf-8")
        self.assertIn("|55433|fixture_user|fixture_password|fixture_database|", commands)
        self.assertNotIn("|15432|platform_user|platform_password|addp_online|", commands)
        self.assertIn("--env-file /dev/null", commands)
        self.assertIn("CREATE TABLE IF NOT EXISTS public.addp_online_catalog_fixture", commands)
        self.assertIn("ON CONFLICT", commands)
        self.assertIn("CREATE SCHEMA IF NOT EXISTS addp_online_security", commands)
        self.assertIn("DROP TABLE IF EXISTS addp_online_security.mysql_email_transfer", commands)
        self.assertIn("CREATE TABLE addp_online_security.mysql_email_transfer", commands)
        self.assertFalse((self.business / ".env").exists())

    def test_rejects_non_dedicated_host_before_docker(self) -> None:
        result = self.run_fixture("start", ADDP_ONLINE_HOST="0")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ADDP_ONLINE_HOST", result.stderr)
        self.assertFalse(self.log.exists())

    def test_refuses_to_stop_a_container_owned_by_another_compose_service(self) -> None:
        self.state.touch()

        result = self.run_fixture("stop", ADDP_TEST_CONTAINER_OWNERSHIP="personal/postgres")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not owned", result.stderr)
        self.assertTrue(self.state.exists())


if __name__ == "__main__":
    unittest.main()
