import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("transfer-relational-sql-etl-online.py")
SPEC = importlib.util.spec_from_file_location("transfer_relational_sql_etl_online", SCRIPT)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class TransferRelationalSQLETLOnlineTest(unittest.TestCase):
    def test_validates_postgresql_engine_through_formal_connection_check(self) -> None:
        class Client:
            def __init__(self):
                self.calls = []

            def request(self, method, path, expected):
                self.calls.append((method, path, expected))
                return SimpleNamespace(
                    payload={
                        "id": 7,
                        "name": "Online PostgreSQL Fixture",
                        "engine_type": "postgresql",
                        "lifecycle_state": "active",
                        "connection_status": "online",
                    }
                )

        client = Client()
        with patch.object(ONLINE.time, "monotonic", return_value=1.0):
            result = ONLINE.validate_engine(client, 7, "Online PostgreSQL Fixture", 10.0)

        self.assertEqual(result["connection_status"], "online")
        self.assertEqual(
            client.calls,
            [
                ("POST", "/api/v1/system/engines/7/test", (200,)),
                ("GET", "/api/v1/system/engines/7", (200,)),
            ],
        )

    def test_task_name_is_stable_and_fits_ui_limit(self) -> None:
        first = ONLINE.task_name("run-123")

        self.assertEqual(first, ONLINE.task_name("run-123"))
        self.assertTrue(first.startswith(ONLINE.TASK_PREFIX))
        self.assertLessEqual(len(first), 50)
        self.assertNotEqual(first, ONLINE.task_name("run-456"))

    def test_validates_complete_browser_report(self) -> None:
        name = ONLINE.task_name("run-123")
        report = self.browser_report(name)

        self.assertEqual(ONLINE.validate_browser_report(report, "run-123", "42", name), report)
        report["records_written"] = 3
        with self.assertRaisesRegex(ONLINE.SuiteError, "records_written"):
            ONLINE.validate_browser_report(report, "run-123", "42", name)

    def test_run_browser_uses_registered_spec_and_validates_artifact(self) -> None:
        with tempfile.TemporaryDirectory(prefix="addp-transfer-sql-etl-browser-") as temporary:
            root = Path(temporary)
            (root / "console/frontend").mkdir(parents=True)
            artifacts = root / "artifacts"
            artifacts.mkdir()
            name = ONLINE.task_name("run-123")
            payload = self.browser_report(name)
            environment = {
                "ADDP_ONLINE_ARTIFACT_DIR": str(artifacts),
                "ADDP_ONLINE_TEST_RUN_ID": "run-123",
                "ADDP_ONLINE_TEST_TENANT_ID": "42",
            }

            def fake_run(command, **kwargs):
                (artifacts / "transfer-relational-sql-etl-browser.json").write_text(
                    json.dumps(payload), encoding="utf-8"
                )
                self.assertIn("e2e/online/transfer-relational-sql-etl.spec.js", command)
                self.assertEqual(kwargs["cwd"], root / "console/frontend")
                self.assertEqual(kwargs["env"]["ADDP_ONLINE_TRANSFER_SQL_ETL_TASK_NAME"], name)
                self.assertEqual(
                    kwargs["env"]["ADDP_ONLINE_TRANSFER_SQL_ETL_SOURCE_TABLE"],
                    ONLINE.SOURCE_TABLE,
                )
                self.assertEqual(
                    kwargs["env"]["ADDP_ONLINE_TRANSFER_SQL_ETL_TARGET_TABLE"],
                    ONLINE.TARGET_TABLE,
                )
                return SimpleNamespace(returncode=0, stdout="", stderr="")

            with patch.dict(ONLINE.os.environ, environment, clear=False), patch.object(
                ONLINE.subprocess, "run", side_effect=fake_run
            ):
                self.assertEqual(ONLINE.run_browser(root, environment, name), payload)

    @staticmethod
    def browser_report(name: str) -> dict[str, object]:
        return {
            "schema_version": "addp.transfer-relational-sql-etl-browser/v1",
            "suite": "transfer-relational-sql-etl",
            "run_id": "run-123",
            "result": "passed",
            "tenant_id": "42",
            "task_name": name,
            "query_language": "sql",
            "language_selector_hidden": True,
            "projected_fields": ["id", "region", "amount"],
            "parameter_count": 2,
            "records_read": 2,
            "records_written": 2,
            "target_row_count": 2,
            "task_deleted": True,
        }


if __name__ == "__main__":
    unittest.main()
