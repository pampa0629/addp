import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).parents[2] / "business/scripts/online-transfer-insert-only-fixture.sh"


class OnlineTransferInsertOnlyFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-transfer-insert-only-")
        self.root = Path(self.temporary.name)
        self.business = self.root / "business"
        self.bin = self.root / "bin"
        self.postgres_state = self.root / "postgres-running"
        self.mysql_state = self.root / "mysql-running"
        self.log = self.root / "fixture.log"
        (self.business / "scripts").mkdir(parents=True)
        self.bin.mkdir()
        shutil.copy2(SCRIPT, self.business / "scripts/online-transfer-insert-only-fixture.sh")
        (self.business / "docker-compose.yml").write_text("services: {}\n", encoding="utf-8")
        self._executable("uname", "#!/bin/bash\necho Darwin\n")
        self._executable(
            "business/scripts/online-engine-fixture.sh",
            """#!/bin/bash
printf 'postgres-fixture:%s\n' "$1" >> "$ADDP_TEST_FIXTURE_LOG"
case "$1" in
  start) touch "$ADDP_TEST_POSTGRES_STATE" ;;
  stop) rm -f "$ADDP_TEST_POSTGRES_STATE" ;;
esac
""",
        )
        self._executable(
            "docker",
            """#!/bin/bash
printf 'docker:%s|%s|%s|%s\n' "$*" "$MYSQL_PORT" "$MYSQL_DATABASE" "$MYSQL_ROOT_PASSWORD" >> "$ADDP_TEST_FIXTURE_LOG"
case "$1" in
  compose)
    case " $* " in
      *" up -d mysql "*) touch "$ADDP_TEST_MYSQL_STATE" ;;
      *" rm -sf mysql "*) rm -f "$ADDP_TEST_MYSQL_STATE" ;;
    esac
    ;;
  inspect)
    case " $* " in
      *business-postgres*) state="$ADDP_TEST_POSTGRES_STATE"; owner=business/postgres ;;
      *) state="$ADDP_TEST_MYSQL_STATE"; owner=business/mysql ;;
    esac
    [ -f "$state" ] || exit 1
    case " $* " in
      *"com.docker.compose.project"*) echo "${ADDP_TEST_CONTAINER_OWNERSHIP:-$owner}" ;;
      *) echo true ;;
    esac
    ;;
  exec)
    case " $* " in
      *business-postgres*) [ -f "$ADDP_TEST_POSTGRES_STATE" ] || exit 1 ;;
      *business-mysql*) [ -f "$ADDP_TEST_MYSQL_STATE" ] || exit 1 ;;
    esac
    input=""
    case " $* " in
      *" --skip-column-names -e "*|*" -c "*|*" -Atc "*) ;;
      *) input=$(cat) ;;
    esac
    printf 'stdin:%s\n' "$input" >> "$ADDP_TEST_FIXTURE_LOG"
    case " $* " in
      *"NUMERIC_PRECISION"*) echo '6|2' ;;
      *"COUNT(DISTINCT id)"*) echo '7|1|7|100|10.25|700|700.70|0' ;;
      *"COUNT(*) FROM public.addp_online_transfer_insert_only_source"*) echo 6 ;;
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
                "ADDP_ONLINE_TEST_ENGINE_PORT": "55433",
                "ADDP_ONLINE_TEST_ENGINE_USER": "online_engine",
                "ADDP_ONLINE_TEST_ENGINE_PASSWORD": "postgres-password-1234",
                "ADDP_ONLINE_TEST_ENGINE_DATABASE": "online_engine",
                "ADDP_ONLINE_TRANSFER_MYSQL_PORT": "53307",
                "ADDP_ONLINE_TRANSFER_MYSQL_DATABASE": "transfer_fixture",
                "ADDP_ONLINE_TRANSFER_MYSQL_USER": "transfer_writer",
                "ADDP_ONLINE_TRANSFER_MYSQL_PASSWORD": "writer-password-1234",
                "ADDP_ONLINE_TRANSFER_MYSQL_ROOT_PASSWORD": "root-password-1234",
                "ADDP_TEST_POSTGRES_STATE": str(self.postgres_state),
                "ADDP_TEST_MYSQL_STATE": str(self.mysql_state),
                "ADDP_TEST_FIXTURE_LOG": str(self.log),
                "MYSQL_PORT": "3306",
                "MYSQL_DATABASE": "personal",
                "MYSQL_ROOT_PASSWORD": "personal-secret",
            }
        )

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def _executable(self, relative: str, content: str) -> None:
        path = (self.bin / relative) if "/" not in relative else (self.root / relative)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
        path.chmod(0o755)

    def run_fixture(self, action: str, **overrides: str) -> subprocess.CompletedProcess[str]:
        environment = dict(self.environment)
        environment.update(overrides)
        return subprocess.run(
            ["bash", "business/scripts/online-transfer-insert-only-fixture.sh", action],
            cwd=self.root,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_owns_reset_advance_verify_and_cleanup(self) -> None:
        results = [self.run_fixture(action) for action in ("start", "advance", "verify", "stop")]

        for result in results:
            self.assertEqual(result.returncode, 0, result.stderr)
        commands = self.log.read_text(encoding="utf-8")
        self.assertIn("--env-file /dev/null", commands)
        self.assertIn("|53307|transfer_fixture|root-password-1234", commands)
        self.assertNotIn("up -d mysql|3306|personal|personal-secret", commands)
        self.assertIn("REVOKE ALL PRIVILEGES, GRANT OPTION", commands)
        self.assertIn("GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, INDEX", commands)
        self.assertIn("CREATE TABLE public.addp_online_transfer_insert_only_source", commands)
        self.assertIn("amount numeric NOT NULL", commands)
        self.assertIn("SET points = 9999, amount = 9999.99", commands)
        self.assertIn("VALUES (7, 'customer-7', 700, 700.70)", commands)
        self.assertGreaterEqual(commands.count("DROP TABLE IF EXISTS `addp_online_transfer_insert_only_target`"), 2)
        self.assertEqual(commands.count("postgres-fixture:start"), 1)
        self.assertEqual(commands.count("postgres-fixture:stop"), 1)
        self.assertFalse(self.postgres_state.exists())
        self.assertFalse(self.mysql_state.exists())

    def test_rejects_unsafe_writer_password_before_docker(self) -> None:
        result = self.run_fixture("start", ADDP_ONLINE_TRANSFER_MYSQL_PASSWORD="bad quote'")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("URL-safe", result.stderr)
        self.assertFalse(self.log.exists())

    def test_refuses_mysql_container_owned_by_another_service(self) -> None:
        self.mysql_state.touch()

        result = self.run_fixture("stop", ADDP_TEST_CONTAINER_OWNERSHIP="personal/mysql")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not owned", result.stderr)
        self.assertTrue(self.mysql_state.exists())


if __name__ == "__main__":
    unittest.main()
