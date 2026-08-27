import importlib.util
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("consumer-engine-recovery-online.py")
SPEC = importlib.util.spec_from_file_location("consumer_engine_recovery_online", SCRIPT)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class ConsumerEngineRecoveryOnlineTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-consumer-engine-online-")
        self.root = Path(self.temporary.name)
        self.artifacts = self.root / "artifacts"
        self.artifacts.mkdir()
        self.repository = self.root / "repository"
        (self.repository / "console/frontend").mkdir(parents=True)
        self.environment = {
            "GATEWAY_URL": "http://127.0.0.1:8000",
            "CONSOLE_URL": "http://127.0.0.1:5170",
            "ADDP_ONLINE_ARTIFACT_DIR": str(self.artifacts),
            "ADDP_ONLINE_TEST_RUN_ID": "run-42",
            "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "addp_at_online",
            "ADDP_ONLINE_TEST_USER_USERNAME": "online-user",
            "ADDP_ONLINE_TEST_USER_PASSWORD": "online-password",
            "ADDP_ONLINE_TEST_TENANT_ID": "42",
            "ADDP_ONLINE_TEST_ENGINE_ID": "7",
            "ADDP_ONLINE_TEST_ENGINE_NAME": "Online PostgreSQL Fixture",
            "ADDP_ONLINE_TEST_ENGINE_PORT": "55433",
            "ADDP_ONLINE_TEST_ENGINE_USER": "online_engine",
            "ADDP_ONLINE_TEST_ENGINE_PASSWORD": "online-engine-password",
            "ADDP_ONLINE_TEST_ENGINE_DATABASE": "online_engine",
        }

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def report(self) -> dict[str, object]:
        return {
            "schema_version": "addp.consumer-engine-recovery/v1",
            "suite": "consumer-engine-recovery",
            "run_id": "run-42",
            "result": "passed",
            "engine_id": 7,
            "final_connection_status": "online",
            "consumer_processes_restarted": 0,
        }

    def test_requires_the_dedicated_browser_user_and_engine_fixture(self) -> None:
        environment = dict(self.environment)
        environment.pop("ADDP_ONLINE_TEST_ENGINE_ID")
        with self.assertRaisesRegex(ONLINE.SuiteError, "ADDP_ONLINE_TEST_ENGINE_ID"):
            ONLINE.required_environment(environment)

    def test_validates_browser_report_contract(self) -> None:
        self.assertEqual(
            ONLINE.validate_browser_report(self.report(), self.environment)["result"],
            "passed",
        )
        invalid = self.report()
        invalid["consumer_processes_restarted"] = 1
        with self.assertRaisesRegex(ONLINE.SuiteError, "consumer_processes_restarted"):
            ONLINE.validate_browser_report(invalid, self.environment)

    @patch.object(ONLINE.time, "sleep", return_value=None)
    @patch.object(
        ONLINE,
        "request_json",
        side_effect=[
            (
                200,
                {
                    "id": 7,
                    "name": "Online PostgreSQL Fixture",
                    "engine_type": "postgresql",
                    "lifecycle_state": "active",
                    "connection_status": "offline",
                },
            ),
            (200, {"success": True}),
            (200, {"id": 7, "name": "Online PostgreSQL Fixture", "connection_status": "offline"}),
            (200, {"id": 7, "name": "Online PostgreSQL Fixture", "connection_status": "online"}),
        ],
    )
    def test_restore_uses_official_engine_test_and_waits_for_online(self, request_json, _sleep) -> None:
        result = ONLINE.restore_engine_online(self.environment, timeout=1)

        self.assertEqual(result["connection_status"], "online")
        self.assertEqual(request_json.call_args_list[0].args[1:], ("GET", "/api/v1/system/engines/7"))
        self.assertEqual(request_json.call_args_list[1].args[1:], ("POST", "/api/v1/system/engines/7/test"))

    @patch.object(ONLINE.subprocess, "run")
    def test_runs_the_owner_playwright_config_and_returns_its_report(self, run) -> None:
        report_path = self.artifacts / "consumer-engine-recovery-browser.json"
        report_path.write_text(json.dumps(self.report()) + "\n", encoding="utf-8")
        run.return_value = subprocess.CompletedProcess([], 0, stdout="browser passed\n", stderr="")

        result = ONLINE.run_browser(self.repository, self.environment)

        self.assertEqual(result["engine_id"], 7)
        self.assertEqual(run.call_args.kwargs["cwd"], self.repository / "console/frontend")
        self.assertIn("--config=playwright.online.config.js", run.call_args.args[0])
        self.assertEqual(
            run.call_args.kwargs["env"]["ADDP_ONLINE_REPOSITORY"],
            str(self.repository),
        )


if __name__ == "__main__":
    unittest.main()
