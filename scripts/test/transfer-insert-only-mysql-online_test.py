import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("transfer-insert-only-mysql-online.py")
SPEC = importlib.util.spec_from_file_location("transfer_insert_only_mysql_online", SCRIPT)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class TransferInsertOnlyMySQLOnlineTest(unittest.TestCase):
    def test_validates_engine_through_the_formal_connection_check(self) -> None:
        class Client:
            def __init__(self):
                self.calls = []

            def request(self, method, path, expected):
                self.calls.append((method, path, expected))
                return SimpleNamespace(
                    payload={
                        "id": 17,
                        "name": "Online Transfer MySQL Fixture",
                        "engine_type": "mysql",
                        "lifecycle_state": "active",
                        "connection_status": "online",
                    }
                )

        client = Client()
        with patch.object(ONLINE.time, "monotonic", return_value=1.0):
            result = ONLINE.validate_engine(
                client,
                17,
                "mysql",
                "Online Transfer MySQL Fixture",
                10.0,
            )

        self.assertEqual(result["connection_status"], "online")
        self.assertEqual(
            client.calls,
            [
                ("POST", "/api/v1/system/engines/17/test", (200,)),
                ("GET", "/api/v1/system/engines/17", (200,)),
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
        report = {
            "schema_version": "addp.transfer-insert-only-mysql-browser/v1",
            "suite": "transfer-insert-only-mysql",
            "run_id": "run-123",
            "result": "passed",
            "tenant_id": "42",
            "task_name": name,
            "initial_records_written": 6,
            "incremental_records_written": 1,
            "target_row_count": 7,
            "old_update_ignored": True,
            "decimal_precision": 6,
            "decimal_scale": 2,
            "task_deleted": True,
        }

        self.assertEqual(
            ONLINE.validate_browser_report(report, "run-123", "42", name),
            report,
        )

        report["incremental_records_written"] = 2
        with self.assertRaisesRegex(ONLINE.SuiteError, "incremental_records_written"):
            ONLINE.validate_browser_report(report, "run-123", "42", name)

    def test_run_browser_uses_registered_spec_and_validates_artifact(self) -> None:
        with tempfile.TemporaryDirectory(prefix="addp-transfer-insert-only-browser-") as temporary:
            root = Path(temporary)
            (root / "console/frontend").mkdir(parents=True)
            artifacts = root / "artifacts"
            artifacts.mkdir()
            name = ONLINE.task_name("run-123")
            payload = {
                "schema_version": "addp.transfer-insert-only-mysql-browser/v1",
                "suite": "transfer-insert-only-mysql",
                "run_id": "run-123",
                "result": "passed",
                "tenant_id": "42",
                "task_name": name,
                "initial_records_written": 6,
                "incremental_records_written": 1,
                "target_row_count": 7,
                "old_update_ignored": True,
                "decimal_precision": 6,
                "decimal_scale": 2,
                "task_deleted": True,
            }
            (artifacts / "transfer-insert-only-mysql-browser.json").write_text(
                json.dumps(payload), encoding="utf-8"
            )
            environment = {
                "ADDP_ONLINE_ARTIFACT_DIR": str(artifacts),
                "ADDP_ONLINE_TEST_RUN_ID": "run-123",
                "ADDP_ONLINE_TEST_TENANT_ID": "42",
            }

            def fake_run(command, **kwargs):
                (artifacts / "transfer-insert-only-mysql-browser.json").write_text(
                    json.dumps(payload), encoding="utf-8"
                )
                self.assertIn("e2e/online/transfer-insert-only-mysql.spec.js", command)
                self.assertEqual(kwargs["cwd"], root / "console/frontend")
                self.assertEqual(kwargs["env"]["ADDP_ONLINE_TRANSFER_TASK_NAME"], name)
                self.assertEqual(
                    kwargs["env"]["ADDP_ONLINE_TRANSFER_SOURCE_TABLE"],
                    ONLINE.SOURCE_TABLE,
                )
                return SimpleNamespace(returncode=0, stdout="", stderr="")

            with patch.dict(ONLINE.os.environ, environment, clear=False), patch.object(
                ONLINE.subprocess, "run", side_effect=fake_run
            ):
                self.assertEqual(ONLINE.run_browser(root, environment, name), payload)


if __name__ == "__main__":
    unittest.main()
