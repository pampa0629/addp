import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).parents[2] / "business/scripts/online-security-transfer-fixture.sh"


class OnlineSecurityTransferFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-security-transfer-")
        self.root = Path(self.temporary.name)
        self.business = self.root / "business"
        self.bin = self.root / "bin"
        self.state = self.root / "mongodb-running"
        self.log = self.root / "commands.log"
        (self.business / "scripts").mkdir(parents=True)
        self.bin.mkdir()
        shutil.copy2(SCRIPT, self.business / "scripts/online-security-transfer-fixture.sh")
        (self.business / "docker-compose.yml").write_text("services: {}\n", encoding="utf-8")
        self._executable(
            self.business / "scripts/online-engine-fixture.sh",
            '#!/bin/bash\nprintf "postgres-fixture:%s\\n" "$1" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._executable(self.bin / "uname", "#!/bin/bash\necho Darwin\n")
        self._executable(
            self.bin / "docker",
            """#!/bin/bash
printf '%s|mongo-port=%s|mongo-db=%s\n' "$*" "$MONGO_PORT" "$MONGO_DB" >> "$ADDP_TEST_COMMAND_LOG"
case "$1" in
  compose)
    case " $* " in
      *" up -d mongodb "*) touch "$ADDP_TEST_CONTAINER_STATE" ;;
      *" rm -sf mongodb "*) rm -f "$ADDP_TEST_CONTAINER_STATE" ;;
    esac
    ;;
  inspect)
    [ -f "$ADDP_TEST_CONTAINER_STATE" ] || exit 1
    case " $* " in
      *"com.docker.compose.project"*) echo "${ADDP_TEST_CONTAINER_OWNERSHIP:-business/mongodb}" ;;
      *) echo true ;;
    esac
    ;;
  exec)
    input=""
    case " $* " in
      *" --file /dev/stdin "*) input=$(cat) ;;
    esac
    printf 'stdin:%s\n' "$input" >> "$ADDP_TEST_COMMAND_LOG"
    case "$input $*" in
      *"countDocuments"*) echo 3 ;;
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
                "ADDP_ONLINE_TEST_ENGINE_PASSWORD": "online-engine-password",
                "ADDP_ONLINE_TEST_ENGINE_DATABASE": "online_engine",
                "ADDP_ONLINE_SECURITY_MONGODB_PORT": "57017",
                "ADDP_ONLINE_SECURITY_MONGODB_DATABASE": "security_online",
                "ADDP_ONLINE_SECURITY_MONGODB_USER": "security_reader",
                "ADDP_ONLINE_SECURITY_MONGODB_PASSWORD": "security-reader-1234",
                "ADDP_ONLINE_SECURITY_MONGODB_ROOT_USER": "online_root",
                "ADDP_ONLINE_SECURITY_MONGODB_ROOT_PASSWORD": "security-root-1234",
                "ADDP_TEST_CONTAINER_STATE": str(self.state),
                "ADDP_TEST_COMMAND_LOG": str(self.log),
                "MONGO_PORT": "27017",
                "MONGO_DB": "personal",
            }
        )

    def tearDown(self) -> None:
        self.temporary.cleanup()

    @staticmethod
    def _executable(path: Path, content: str) -> None:
        path.write_text(content, encoding="utf-8")
        path.chmod(0o755)

    def run_fixture(self, action: str, **overrides: str) -> subprocess.CompletedProcess[str]:
        environment = dict(self.environment)
        environment.update(overrides)
        return subprocess.run(
            ["bash", "business/scripts/online-security-transfer-fixture.sh", action],
            cwd=self.root,
            env=environment,
            capture_output=True,
            text=True,
            timeout=5,
        )

    def test_seeds_read_only_mongodb_source_and_stable_postgresql_targets(self) -> None:
        started = self.run_fixture("start")
        stopped = self.run_fixture("stop")

        self.assertEqual(started.returncode, 0, started.stderr)
        self.assertEqual(stopped.returncode, 0, stopped.stderr)
        commands = self.log.read_text(encoding="utf-8")
        self.assertIn("postgres-fixture:start", commands)
        self.assertIn("postgres-fixture:stop", commands)
        self.assertIn("--env-file /dev/null", commands)
        self.assertIn("mongo-port=57017|mongo-db=security_online", commands)
        self.assertNotIn("up -d mongodb|mongo-port=27017|mongo-db=personal", commands)
        self.assertIn('roles: [{role: "read", db: fixtureDatabase}]', commands)
        self.assertNotIn('role: "readWrite"', commands)
        self.assertIn("CREATE SCHEMA IF NOT EXISTS addp_online_security", commands)
        self.assertIn("TRUNCATE addp_online_security.transfer_excluded", commands)
        self.assertIn("addp_online_security.exemption_source", commands)
        self.assertIn("addp_online_security.exemption_transfer", commands)
        self.assertIn("13812345678", commands)
        self.assertIn('phone: "13812345678"', commands)
        self.assertFalse((self.business / ".env").exists())

    def test_rejects_unsafe_password_before_docker_or_postgresql_lifecycle(self) -> None:
        result = self.run_fixture(
            "start", ADDP_ONLINE_SECURITY_MONGODB_PASSWORD="bad password"
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("URL-safe", result.stderr)
        self.assertFalse(self.log.exists())

    def test_refuses_container_owned_by_another_compose_service(self) -> None:
        self.state.touch()

        result = self.run_fixture(
            "stop", ADDP_TEST_CONTAINER_OWNERSHIP="personal/mongodb"
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not owned", result.stderr)
        self.assertTrue(self.state.exists())


if __name__ == "__main__":
    unittest.main()
