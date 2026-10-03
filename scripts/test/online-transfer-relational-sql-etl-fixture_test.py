import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).parents[2] / "business/scripts/online-transfer-relational-sql-etl-fixture.sh"


class OnlineTransferRelationalSQLETLFixtureTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-transfer-sql-etl-")
        self.root = Path(self.temporary.name)
        self.bin = self.root / "bin"
        self.postgres_state = self.root / "postgres-running"
        self.log = self.root / "fixture.log"
        (self.root / "business/scripts").mkdir(parents=True)
        self.bin.mkdir()
        shutil.copy2(SCRIPT, self.root / "business/scripts/online-transfer-relational-sql-etl-fixture.sh")
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
printf 'docker:%s\n' "$*" >> "$ADDP_TEST_FIXTURE_LOG"
case "$1" in
  inspect)
    [ -f "$ADDP_TEST_POSTGRES_STATE" ] || exit 1
    echo true
    ;;
  exec)
    [ -f "$ADDP_TEST_POSTGRES_STATE" ] || exit 1
    input=""
    case " $* " in
      *" -Atc "*|*" -c "*) ;;
      *) input=$(cat) ;;
    esac
    printf 'stdin:%s\n' "$input" >> "$ADDP_TEST_FIXTURE_LOG"
    case " $* " in
      *"COUNT(*) FROM information_schema.tables"*) echo "${ADDP_TEST_REMAINING_TABLES:-0}" ;;
      *"numeric_precision"*) echo '8|2' ;;
      *"generated_label"*) echo "${ADDP_TEST_NATIVE_VALUES:-5|1411.50|3|2|5}" ;;
      *"WHERE area"*) echo '5|1411.50|3|2' ;;
      *"CONCAT_WS"*) echo '2|3|4|701.00' ;;
      *"string_agg(column_name"*) echo 'id,region,amount' ;;
      *"COUNT(*) FROM public.addp_online_transfer_sql_etl_source"*) echo 5 ;;
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
                "ADDP_ONLINE_TEST_ENGINE_USER": "online_engine",
                "ADDP_ONLINE_TEST_ENGINE_DATABASE": "online_engine",
                "ADDP_TEST_POSTGRES_STATE": str(self.postgres_state),
                "ADDP_TEST_FIXTURE_LOG": str(self.log),
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
            ["bash", "business/scripts/online-transfer-relational-sql-etl-fixture.sh", action],
            cwd=self.root,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_owns_projection_filter_verification_and_cleanup(self) -> None:
        results = [self.run_fixture(action) for action in ("start", "status", "verify", "stop")]

        for result in results:
            self.assertEqual(result.returncode, 0, result.stderr)
        commands = self.log.read_text(encoding="utf-8")
        self.assertIn("CREATE TABLE public.addp_online_transfer_sql_etl_source", commands)
        self.assertIn("internal_note varchar(64) NOT NULL", commands)
        self.assertIn("(3, 'north', 'active', 300.25, 'hidden-3')", commands)
        self.assertIn("CONCAT_WS", commands)
        self.assertIn("string_agg(column_name", commands)
        self.assertGreaterEqual(
            commands.count("DROP TABLE IF EXISTS public.addp_online_transfer_sql_etl_target"),
            2,
        )
        self.assertEqual(commands.count("postgres-fixture:start"), 1)
        self.assertEqual(commands.count("postgres-fixture:stop"), 1)
        for table in ("addp_online_transfer_field_lineage_target", "addp_online_transfer_field_lineage_downstream"):
            self.assertGreaterEqual(commands.count("DROP TABLE IF EXISTS public." + table), 2)
        self.assertIn("numeric_precision, numeric_scale", commands)
        self.assertFalse(self.postgres_state.exists())

    def test_requires_the_dedicated_online_host_marker(self) -> None:
        result = self.run_fixture("start", ADDP_ONLINE_HOST="0")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ADDP_ONLINE_HOST", result.stderr)
        self.assertFalse(self.log.exists())

    def test_rejects_native_rows_that_do_not_prove_replace_and_generation(self):
        self.assertEqual(self.run_fixture("start").returncode, 0)
        result = self.run_fixture("verify", ADDP_TEST_NATIVE_VALUES="5|1411.50|5|0|5")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("native target does not prove", result.stderr)

    def test_cleanup_fails_if_owned_tables_remain(self):
        self.assertEqual(self.run_fixture("start").returncode, 0)
        result = self.run_fixture("stop", ADDP_TEST_REMAINING_TABLES="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("owned tables remain", result.stderr)
        self.assertTrue(self.postgres_state.exists())


if __name__ == "__main__":
    unittest.main()
