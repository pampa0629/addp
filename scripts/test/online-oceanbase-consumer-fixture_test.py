import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = (
    Path(__file__).parents[2]
    / "business/scripts/online-oceanbase-consumer-fixture.sh"
)


class OnlineOceanBaseConsumerFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(
            prefix="addp-online-oceanbase-consumer-"
        )
        self.root = Path(self.temporary.name)
        self.business = self.root / "business"
        self.bin = self.root / "bin"
        self.state = self.root / "running"
        self.log = self.root / "docker.log"
        (self.business / "scripts").mkdir(parents=True)
        self.bin.mkdir()
        shutil.copy2(
            SCRIPT,
            self.business / "scripts/online-oceanbase-consumer-fixture.sh",
        )
        (self.business / "docker-compose.yml").write_text(
            "services: {}\n", encoding="utf-8"
        )
        self._executable("uname", "#!/bin/bash\necho Darwin\n")
        self._executable(
            "docker",
            """#!/bin/bash
printf '%s|%s|%s|%s|%s|%s\n' "$*" "$OCEANBASE_IMAGE" "$OCEANBASE_PORT" "$OCEANBASE_DATABASE" "$OCEANBASE_TENANT_NAME" "$OCEANBASE_PASSWORD" >> "$ADDP_TEST_DOCKER_LOG"
case "$1" in
  compose)
    case " $* " in
      *" up -d oceanbase "*) touch "$ADDP_TEST_CONTAINER_STATE" ;;
      *" rm -sf oceanbase "*) rm -f "$ADDP_TEST_CONTAINER_STATE" ;;
    esac
    ;;
  inspect)
    [ -f "$ADDP_TEST_CONTAINER_STATE" ] || exit 1
    case " $* " in
      *"com.docker.compose.project"*) echo "${ADDP_TEST_CONTAINER_OWNERSHIP:-business/oceanbase}" ;;
      *) echo true ;;
    esac
    ;;
  exec)
    [ -f "$ADDP_TEST_CONTAINER_STATE" ] || exit 1
    input=""
    case " $* " in
      *" --batch --skip-column-names -e "*) ;;
      *) input=$(cat) ;;
    esac
    printf 'stdin:%s\n' "$input" >> "$ADDP_TEST_DOCKER_LOG"
    case " $* " in
      *"updated_at >"*) echo 2 ;;
      *"addp_online_consumer_source"*) echo 5 ;;
      *"addp_online_consumer_target"*) echo 0 ;;
      *"SELECT 1"*) echo 1 ;;
    esac
    ;;
esac
""",
        )
        self.environment = dict(os.environ)
        self.environment.update(
            {
                "PATH": str(self.bin) + os.pathsep + self.environment["PATH"],
                "ADDP_ONLINE_HOST": "1",
                "ADDP_ONLINE_OCEANBASE_PORT": "52881",
                "ADDP_ONLINE_OCEANBASE_DATABASE": "oceanbase_fixture",
                "ADDP_ONLINE_OCEANBASE_USER": "root@test",
                "ADDP_ONLINE_OCEANBASE_PASSWORD": "oceanbase-secret-1234",
                "ADDP_TEST_CONTAINER_STATE": str(self.state),
                "ADDP_TEST_DOCKER_LOG": str(self.log),
                "OCEANBASE_PORT": "2881",
                "OCEANBASE_DATABASE": "personal",
                "OCEANBASE_PASSWORD": "personal-secret",
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
            [
                "bash",
                "business/scripts/online-oceanbase-consumer-fixture.sh",
                action,
            ],
            cwd=self.root,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_resets_advances_and_restores_the_fixed_tables(self) -> None:
        started = self.run_fixture("start")
        advanced = self.run_fixture("advance")
        stopped = self.run_fixture("stop")

        self.assertEqual(started.returncode, 0, started.stderr)
        self.assertEqual(advanced.returncode, 0, advanced.stderr)
        self.assertEqual(stopped.returncode, 0, stopped.stderr)
        commands = self.log.read_text(encoding="utf-8")
        self.assertIn("--env-file /dev/null", commands)
        self.assertIn(
            "|oceanbase/oceanbase-ce:4.4.2-lts|52881|oceanbase_fixture|test|oceanbase-secret-1234",
            commands,
        )
        self.assertNotIn("|2881|personal|", commands)
        self.assertGreaterEqual(commands.count("DROP TABLE IF EXISTS `addp_online_consumer_target`"), 2)
        self.assertIn("CREATE TABLE `addp_online_consumer_source`", commands)
        self.assertIn("ENGINE=InnoDB", commands)
        self.assertIn("WHERE id = 2", commands)
        self.assertIn("VALUES (6, 'OB-1006'", commands)
        self.assertFalse((self.business / ".env").exists())
        self.assertFalse(self.state.exists())

    def test_rejects_unqualified_account_before_docker(self) -> None:
        result = self.run_fixture(
            "start", ADDP_ONLINE_OCEANBASE_USER="root"
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("tenant-qualified", result.stderr)
        self.assertFalse(self.log.exists())

    def test_refuses_container_owned_by_another_compose_service(self) -> None:
        self.state.touch()

        result = self.run_fixture(
            "stop", ADDP_TEST_CONTAINER_OWNERSHIP="personal/oceanbase"
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not owned", result.stderr)
        self.assertTrue(self.state.exists())


if __name__ == "__main__":
    unittest.main()
