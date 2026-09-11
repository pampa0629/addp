import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


REPOSITORY = Path(__file__).parents[2]
SCRIPT = REPOSITORY / "business/scripts/online-tidb-consumer-fixture.sh"
COMPOSE = REPOSITORY / "scripts/test/docker-compose.tidb-t2.yml"


class OnlineTiDBConsumerFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(
            prefix="addp-online-tidb-consumer-"
        )
        self.root = Path(self.temporary.name)
        self.bin = self.root / "bin"
        self.state = self.root / "running"
        self.log = self.root / "docker.log"
        (self.root / "business/scripts").mkdir(parents=True)
        (self.root / "scripts/test").mkdir(parents=True)
        self.bin.mkdir()
        shutil.copy2(SCRIPT, self.root / "business/scripts")
        shutil.copy2(COMPOSE, self.root / "scripts/test")
        self._executable("uname", "#!/bin/bash\necho Darwin\n")
        self._executable(
            "docker",
            """#!/bin/bash
printf '%s|%s\n' "$*" "${TIDB_T2_PORT:-}" >> "$ADDP_TEST_DOCKER_LOG"
if [ "$1" = "compose" ]; then
  case " $* " in
    *" up -d --force-recreate tidb-pd tidb-tikv tidb "*) touch "$ADDP_TEST_CONTAINER_STATE" ;;
    *" ps --status running --services "*)
      [ -f "$ADDP_TEST_CONTAINER_STATE" ] && echo tidb
      ;;
    *" down --volumes --remove-orphans "*) rm -f "$ADDP_TEST_CONTAINER_STATE" ;;
  esac
  exit 0
fi
if [ "$1" = "ps" ]; then
  [ -f "$ADDP_TEST_CONTAINER_STATE" ] && echo residual
  exit 0
fi
if [ "$1" = "run" ]; then
  [ -f "$ADDP_TEST_CONTAINER_STATE" ] || exit 1
  input=""
  mysql_execute_count=$(printf '%s' " $* " | grep -o -- ' -e ' | wc -l | tr -d ' ')
  [ "$mysql_execute_count" -gt 1 ] || input=$(cat)
  printf 'stdin:%s\n' "$input" >> "$ADDP_TEST_DOCKER_LOG"
  case " $* " in
    *"updated_at >"*) echo 2 ;;
    *"addp_online_consumer_source"*) echo 5 ;;
    *"addp_online_consumer_target"*) echo 0 ;;
    *"SELECT 1"*) echo 1 ;;
  esac
fi
""",
        )
        self.environment = dict(os.environ)
        self.environment.update(
            {
                "PATH": str(self.bin) + os.pathsep + self.environment["PATH"],
                "ADDP_ONLINE_HOST": "1",
                "ADDP_ONLINE_TIDB_PORT": "54000",
                "ADDP_ONLINE_TIDB_DATABASE": "tidb_fixture",
                "ADDP_ONLINE_TIDB_USER": "root",
                "ADDP_ONLINE_TIDB_PASSWORD": "",
                "ADDP_TEST_CONTAINER_STATE": str(self.state),
                "ADDP_TEST_DOCKER_LOG": str(self.log),
            }
        )

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def _executable(self, name: str, content: str) -> None:
        path = self.bin / name
        path.write_text(content, encoding="utf-8")
        path.chmod(0o755)

    def run_fixture(
        self, action: str, **overrides: str
    ) -> subprocess.CompletedProcess[str]:
        environment = dict(self.environment)
        environment.update(overrides)
        return subprocess.run(
            ["bash", "business/scripts/online-tidb-consumer-fixture.sh", action],
            cwd=self.root,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_owns_three_component_lifecycle_and_zero_residue(self) -> None:
        started = self.run_fixture("start")
        advanced = self.run_fixture("advance")
        stopped = self.run_fixture("stop")

        self.assertEqual(started.returncode, 0, started.stderr)
        self.assertEqual(advanced.returncode, 0, advanced.stderr)
        self.assertEqual(stopped.returncode, 0, stopped.stderr)
        commands = self.log.read_text(encoding="utf-8")
        self.assertIn(
            "compose -p addp-online-tidb-consumer -f ", commands
        )
        self.assertIn(
            "up -d --force-recreate tidb-pd tidb-tikv tidb|54000", commands
        )
        self.assertIn("down --volumes --remove-orphans|54000", commands)
        self.assertIn("CREATE TABLE `addp_online_consumer_source`", commands)
        self.assertIn("VALUES (6, 'TIDB-1006'", commands)
        self.assertFalse(self.state.exists())

    def test_compose_pins_all_official_images_by_digest(self) -> None:
        compose = (self.root / "scripts/test/docker-compose.tidb-t2.yml").read_text(
            encoding="utf-8"
        )
        for image in ("pingcap/pd:v8.5.8", "pingcap/tikv:v8.5.8", "pingcap/tidb:v8.5.8"):
            self.assertRegex(compose, image.replace("/", r"/") + r"@sha256:[0-9a-f]{64}")

    def test_rejects_unsafe_database_before_docker(self) -> None:
        result = self.run_fixture(
            "start", ADDP_ONLINE_TIDB_DATABASE="personal-database"
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("safe TiDB identifier", result.stderr)
        self.assertFalse(self.log.exists())


if __name__ == "__main__":
    unittest.main()
