import os
import signal
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).parents[2] / "business/scripts/online-workbench-mysql-fixture.sh"


class OnlineWorkbenchMySQLFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-workbench-mysql-")
        self.root = Path(self.temporary.name)
        self.business = self.root / "business"
        self.bin = self.root / "bin"
        self.state = self.root / "running"
        self.log = self.root / "docker.log"
        (self.business / "scripts").mkdir(parents=True)
        (self.business / "mysql").mkdir(parents=True)
        self.bin.mkdir()
        shutil.copy2(SCRIPT, self.business / "scripts/online-workbench-mysql-fixture.sh")
        (self.business / "docker-compose.yml").write_text("services: {}\n", encoding="utf-8")
        seed = self.business / "mysql/test-data.sh"
        seed.write_text('#!/bin/bash\nprintf "seed:%s:%s\\n" "$MYSQL_USER" "$MYSQL_DATABASE" >> "$ADDP_TEST_DOCKER_LOG"\n', encoding="utf-8")
        seed.chmod(0o755)
        self._executable("uname", "#!/bin/bash\necho Darwin\n")
        self._executable(
            "docker",
            """#!/bin/bash
printf '%s|%s|%s|%s\n' "$*" "$MYSQL_PORT" "$MYSQL_DATABASE" "$MYSQL_ROOT_PASSWORD" >> "$ADDP_TEST_DOCKER_LOG"
case "$1" in
  compose)
    case " $* " in
      *" up -d mysql "*) touch "$ADDP_TEST_CONTAINER_STATE" ;;
      *" rm -sf mysql "*) rm -f "$ADDP_TEST_CONTAINER_STATE" ;;
    esac
    ;;
  inspect)
    [ -f "$ADDP_TEST_CONTAINER_STATE" ] || exit 1
    case " $* " in
      *"com.docker.compose.project"*) echo "${ADDP_TEST_CONTAINER_OWNERSHIP:-business/mysql}" ;;
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
      *"COUNT(*)"*) echo 4 ;;
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
                "ADDP_ONLINE_WORKBENCH_MYSQL_PORT": "53306",
                "ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE": "commerce_fixture",
                "ADDP_ONLINE_WORKBENCH_MYSQL_USER": "workbench_reader",
                "ADDP_ONLINE_WORKBENCH_MYSQL_PASSWORD": "reader-password-1234",
                "ADDP_ONLINE_WORKBENCH_MYSQL_ROOT_PASSWORD": "root-secret",
                "ADDP_TEST_CONTAINER_STATE": str(self.state),
                "ADDP_TEST_DOCKER_LOG": str(self.log),
                "MYSQL_PORT": "3306",
                "MYSQL_DATABASE": "personal",
                "MYSQL_ROOT_PASSWORD": "personal-secret",
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
            ["bash", "business/scripts/online-workbench-mysql-fixture.sh", action],
            cwd=self.root,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_seeds_deterministic_data_and_grants_only_select_to_reader(self) -> None:
        started = self.run_fixture("start")
        stopped = self.run_fixture("stop")

        self.assertEqual(started.returncode, 0, started.stderr)
        self.assertEqual(stopped.returncode, 0, stopped.stderr)
        commands = self.log.read_text(encoding="utf-8")
        self.assertIn("--env-file /dev/null", commands)
        self.assertIn("|53306|commerce_fixture|root-secret", commands)
        self.assertNotIn("|3306|personal|personal-secret", commands)
        self.assertIn("seed:root:commerce_fixture", commands)
        self.assertIn("REVOKE ALL PRIVILEGES, GRANT OPTION", commands)
        self.assertIn("GRANT SELECT ON `commerce_fixture`.*", commands)
        self.assertNotIn("GRANT INSERT", commands)
        self.assertFalse((self.business / ".env").exists())

    def test_start_does_not_consume_open_parent_stdin_for_e_queries(self) -> None:
        process = subprocess.Popen(
            ["bash", "business/scripts/online-workbench-mysql-fixture.sh", "start"],
            cwd=self.root,
            env=self.environment,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            start_new_session=True,
        )
        timed_out = False
        try:
            returncode = process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            timed_out = True
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
        finally:
            process.stdin.close()

        stdout = process.stdout.read()
        stderr = process.stderr.read()
        process.stdout.close()
        process.stderr.close()
        if timed_out:
            self.fail("mysql -e fixture checks consumed the still-open parent stdin")
        self.assertEqual(returncode, 0, stderr)
        self.assertIn("Online Workbench MySQL fixture is ready", stdout)

    def test_rejects_unsafe_reader_password_before_docker(self) -> None:
        result = self.run_fixture("start", ADDP_ONLINE_WORKBENCH_MYSQL_PASSWORD="bad quote'")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("URL-safe", result.stderr)
        self.assertFalse(self.log.exists())

    def test_refuses_container_owned_by_another_compose_service(self) -> None:
        self.state.touch()

        result = self.run_fixture("stop", ADDP_TEST_CONTAINER_OWNERSHIP="personal/mysql")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not owned", result.stderr)
        self.assertTrue(self.state.exists())


if __name__ == "__main__":
    unittest.main()
