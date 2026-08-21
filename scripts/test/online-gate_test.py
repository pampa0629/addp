import importlib.util
import subprocess
import sys
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("online-gate.py")
SPEC = importlib.util.spec_from_file_location("online_gate", SCRIPT)
ONLINE_GATE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE_GATE
SPEC.loader.exec_module(ONLINE_GATE)


class OnlineGateTest(unittest.TestCase):
    def setUp(self):
        self.repository = Path("/tmp/addp-online-gate-test")
        self.suites = {
            "owner-scenario": ONLINE_GATE.Suite(
                command=("owner-gate", "--online"),
                services=(("system", "SYSTEM_URL"), ("model", "MODEL_URL")),
            )
        }
        self.environment = {
            "ADDP_ONLINE_TEST": "1",
            "ADDP_ONLINE_TEST_TENANT_ID": "42",
            "SYSTEM_URL": "http://127.0.0.1:8180",
            "MODEL_URL": "http://127.0.0.1:8087",
        }

    @patch.object(ONLINE_GATE.time, "monotonic", side_effect=(10.0, 12.0))
    @patch.object(ONLINE_GATE.subprocess, "run")
    def test_dispatches_preflight_before_owner_gate(self, run, _monotonic):
        ONLINE_GATE.run_online(
            "owner-scenario", self.repository, self.environment, self.suites
        )

        self.assertEqual(run.call_count, 2)
        preflight = run.call_args_list[0]
        owner = run.call_args_list[1]
        self.assertIn("online-preflight.py", preflight.args[0][1])
        self.assertIn("system=http://127.0.0.1:8180", preflight.args[0])
        self.assertEqual(owner.args[0], ("owner-gate", "--online"))
        self.assertEqual(preflight.kwargs["timeout"], 900)
        self.assertEqual(owner.kwargs["timeout"], 898)
        self.assertRegex(self.environment["ADDP_ONLINE_TEST_RUN_ID"], r"^run-[0-9a-f]{32}$")

    def test_registers_only_the_executable_standard_model_suite(self):
        self.assertEqual(
            set(ONLINE_GATE.SUITES), {"standard-model-reference-deletion"}
        )
        suite = ONLINE_GATE.SUITES["standard-model-reference-deletion"]
        self.assertEqual(
            suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("standard", "STANDARD_URL"),
                ("model", "MODEL_URL"),
            ),
        )

    @patch.object(ONLINE_GATE.subprocess, "run")
    def test_preserves_explicit_run_id(self, run):
        self.environment["ADDP_ONLINE_TEST_RUN_ID"] = "nightly-42"
        ONLINE_GATE.run_online(
            "owner-scenario", self.repository, self.environment, self.suites
        )
        self.assertEqual(self.environment["ADDP_ONLINE_TEST_RUN_ID"], "nightly-42")

    def test_rejects_unregistered_suite(self):
        with self.assertRaisesRegex(ONLINE_GATE.OnlineGateError, "registered suites"):
            ONLINE_GATE.run_online("missing", self.repository, self.environment, self.suites)

    def test_requires_explicit_online_switch(self):
        self.environment.pop("ADDP_ONLINE_TEST")
        with self.assertRaisesRegex(ONLINE_GATE.OnlineGateError, "exactly 1"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )

    def test_requires_all_suite_service_urls(self):
        self.environment.pop("MODEL_URL")
        with self.assertRaisesRegex(ONLINE_GATE.OnlineGateError, "MODEL_URL"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )

    def test_rejects_non_positive_timeout(self):
        self.environment["ADDP_ONLINE_TEST_TIMEOUT_SECONDS"] = "0"
        with self.assertRaisesRegex(ONLINE_GATE.OnlineGateError, "greater than zero"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )

    @patch.object(
        ONLINE_GATE.subprocess,
        "run",
        side_effect=subprocess.TimeoutExpired(("owner-gate",), 1),
    )
    def test_propagates_timeout(self, _run):
        with self.assertRaises(subprocess.TimeoutExpired):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )

    @patch.object(ONLINE_GATE.time, "monotonic", side_effect=(10.0, 12.0))
    @patch.object(ONLINE_GATE.subprocess, "run")
    def test_rejects_exhausted_total_timeout(self, run, _monotonic):
        self.environment["ADDP_ONLINE_TEST_TIMEOUT_SECONDS"] = "1"
        with self.assertRaises(subprocess.TimeoutExpired):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )
        self.assertEqual(run.call_count, 1)


if __name__ == "__main__":
    unittest.main()
