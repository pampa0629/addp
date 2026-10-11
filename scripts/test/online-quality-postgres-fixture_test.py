import importlib.util
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

PATH = Path(__file__).resolve().parents[2] / "business/scripts/online-quality-postgres-fixture.py"
SPEC = importlib.util.spec_from_file_location("quality_postgres_fixture", PATH)
FIXTURE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(FIXTURE)


class QualityPostgresFixtureTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-quality-source-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.environment = dict(os.environ, GITHUB_ACTIONS="true", RUNNER_OS="Linux", ADDP_ONLINE_HOSTED="1",
                                ADDP_ONLINE_HOST="1", POSTGRES_DB="addp_online", ADDP_ONLINE_SECRET_DIR=str(self.root),
                                ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(self.root / "engine.json"))
        self.calls, self.present, self.label, self.rows = [], False, "quality", "t\nt"

    def docker(self, arguments, **kwargs):
        self.calls.append((arguments, kwargs))
        if arguments[1:3] == ["container", "ls"]:
            return "owned-id" if self.present else ""
        if arguments[1] == "run":
            self.present = True
        if arguments[1] == "inspect":
            return self.label
        if arguments[1] == "rm":
            self.present = False
        if "SELECT count(*)" in kwargs.get("data", ""):
            return self.rows
        return ""

    def test_model_owns_table_creation_and_descriptor_is_private(self):
        with patch.object(FIXTURE, "command", self.docker):
            FIXTURE.run("start", self.environment)
        run = next(call for call in self.calls if call[0][1] == "run")
        self.assertIn("type=tmpfs,destination=/var/lib/postgresql/data", run[0])
        self.assertIn("127.0.0.1:55435:5432", run[0])
        sql = "".join(kwargs.get("data", "") for _, kwargs in self.calls)
        self.assertNotIn("CREATE TABLE", sql)
        self.assertNotIn("COMMENT ON", sql)
        self.assertEqual((self.root / "engine.json").stat().st_mode & 0o777, 0o600)
        password = run[1]["environment"]["POSTGRES_PASSWORD"]
        self.assertEqual(len(password), 64)
        self.assertNotIn(password, repr(run[0]))

    def test_seed_is_atomic_and_data_is_verified(self):
        self.present = True
        with patch.object(FIXTURE, "command", self.docker):
            FIXTURE.run("seed", self.environment)
        sql = [kwargs["data"] for _, kwargs in self.calls if "data" in kwargs]
        self.assertTrue(sql[0].startswith("BEGIN;"))
        self.assertTrue(sql[0].endswith("COMMIT;\n"))
        self.assertIn("public.quality_target_a", sql[0])
        self.assertIn("public.quality_target_b", sql[0])
        self.rows = "t\nf"
        with patch.object(FIXTURE, "command", self.docker), self.assertRaises(FIXTURE.FixtureError):
            FIXTURE.run("verify", self.environment)

    def test_existing_or_foreign_source_is_never_taken_over(self):
        self.present = True
        with patch.object(FIXTURE, "command", self.docker), self.assertRaises(FIXTURE.FixtureError):
            FIXTURE.run("start", self.environment)
        self.label = "foreign"
        with patch.object(FIXTURE, "command", self.docker), self.assertRaises(FIXTURE.FixtureError):
            FIXTURE.run("stop", self.environment)
        self.assertFalse(any(call[0][1] == "rm" for call in self.calls))

    def test_cleanup_requires_owned_deletion_and_observed_absence(self):
        self.present = True
        with patch.object(FIXTURE, "command", self.docker):
            FIXTURE.run("stop", self.environment)
        self.assertFalse(self.present)
        with patch.object(FIXTURE, "command", side_effect=FIXTURE.FixtureError("Docker unavailable")), self.assertRaises(FIXTURE.FixtureError):
            FIXTURE.run("stop", self.environment)

    def test_local_or_development_database_is_rejected_before_docker(self):
        for key, value in (("GITHUB_ACTIONS", "false"), ("POSTGRES_DB", "addp"), ("ADDP_ONLINE_HOSTED", "0")):
            with self.subTest(key=key), patch.object(FIXTURE, "command") as command, self.assertRaises(FIXTURE.FixtureError):
                FIXTURE.run("start", dict(self.environment, **{key: value}))
            command.assert_not_called()

    def test_command_errors_do_not_expose_sql_or_credentials(self):
        with patch.object(FIXTURE.subprocess, "run", return_value=subprocess.CompletedProcess([], 1, "", "private-password private-SQL")):
            with self.assertRaises(FIXTURE.FixtureError) as error:
                FIXTURE.command(["docker", "exec"], data="private-SQL")
        self.assertNotIn("private", str(error.exception))


if __name__ == "__main__":
    unittest.main()
