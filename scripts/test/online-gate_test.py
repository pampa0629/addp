import importlib.util
import fcntl
import sys
import tempfile
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
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-gate-")
        self.root = Path(self.temporary.name)
        self.repository = self.root / "repository"
        self.repository.mkdir()
        self.suites = {
            "owner-scenario": ONLINE_GATE.Suite(
                command=("owner-gate", "--online"),
                services=(("system", "SYSTEM_URL"), ("model", "MODEL_URL")),
            )
        }
        self.environment = {
            "ADDP_ONLINE_TEST": "1",
            "ADDP_ONLINE_TEST_TENANT_ID": "42",
            "POSTGRES_DB": "addp_online",
            "SYSTEM_URL": "http://127.0.0.1:8180",
            "MODEL_URL": "http://127.0.0.1:8087",
            "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "addp_at_never-persist-this",
            "MANAGER_SERVICE_CLIENT_SECRET": "never-persist-this-secret",
            "ADDP_ONLINE_ARTIFACT_DIR": str(self.root / "artifacts"),
        }

    def tearDown(self):
        self.temporary.cleanup()

    @patch.object(ONLINE_GATE.time, "monotonic", side_effect=(10.0, 12.0, 14.0))
    @patch.object(
        ONLINE_GATE,
        "run_stage",
        side_effect=(
            ({"schema_version": "addp.online-preflight/v1", "git_commit": "abc"}, 10),
            ({"schema_version": "addp.online-suite/v1", "residual_resources": 0}, 20),
        ),
    )
    def test_dispatches_preflight_before_owner_gate(self, run_stage, _monotonic):
        report = ONLINE_GATE.run_online(
            "owner-scenario", self.repository, self.environment, self.suites
        )

        self.assertEqual(run_stage.call_count, 2)
        preflight = run_stage.call_args_list[0]
        owner = run_stage.call_args_list[1]
        self.assertEqual(preflight.args[0], "preflight")
        self.assertIn("online-preflight.py", preflight.args[1][1])
        self.assertIn("system=http://127.0.0.1:8180", preflight.args[1])
        self.assertEqual(owner.args, ("scenario", ("owner-gate", "--online")))
        self.assertEqual(preflight.kwargs["timeout"], 900)
        self.assertEqual(owner.kwargs["timeout"], 898)
        self.assertRegex(self.environment["ADDP_ONLINE_TEST_RUN_ID"], r"^run-[0-9a-f]{32}$")
        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["failure_stage"], None)
        persisted = (self.root / "artifacts/online-report.json").read_text(encoding="utf-8")
        self.assertIn('"schema_version": "addp.online-gate/v1"', persisted)
        self.assertNotIn("addp_at_", persisted)
        self.assertNotIn("never-persist-this-secret", persisted)

    def test_registers_only_executable_online_suites(self):
        self.assertEqual(
            set(ONLINE_GATE.SUITES),
            {
                "consumer-engine-recovery",
                "enterprise-catalog-publishing",
                "manager-internal-artifact-lineage",
                "module-registry-recovery",
                "security-protection-exemption",
                "security-transfer-protection",
                "standard-model-reference-deletion",
                "workbench-service-consumption",
            },
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
        registry_suite = ONLINE_GATE.SUITES["module-registry-recovery"]
        self.assertEqual(
            registry_suite.services,
            (("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL")),
        )
        consumer_suite = ONLINE_GATE.SUITES["consumer-engine-recovery"]
        self.assertEqual(
            consumer_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("manager", "MANAGER_URL"),
                ("service", "SERVICE_URL"),
            ),
        )
        catalog_suite = ONLINE_GATE.SUITES["enterprise-catalog-publishing"]
        self.assertEqual(
            catalog_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("meta", "META_URL"),
                ("catalog", "CATALOG_URL"),
                ("asset", "ASSET_URL"),
                ("portal", "PORTAL_URL"),
            ),
        )
        workbench_suite = ONLINE_GATE.SUITES["workbench-service-consumption"]
        self.assertEqual(
            workbench_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("service", "SERVICE_URL"),
                ("workbench", "WORKBENCH_URL"),
            ),
        )
        security_suite = ONLINE_GATE.SUITES["security-transfer-protection"]
        self.assertEqual(
            security_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("meta", "META_URL"),
                ("security", "SECURITY_URL"),
                ("transfer", "TRANSFER_URL"),
                ("manager", "MANAGER_URL"),
            ),
        )
        exemption_suite = ONLINE_GATE.SUITES["security-protection-exemption"]
        self.assertEqual(
            exemption_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("meta", "META_URL"),
                ("security", "SECURITY_URL"),
                ("manager", "MANAGER_URL"),
                ("develop", "DEVELOP_URL"),
                ("service", "SERVICE_URL"),
                ("transfer", "TRANSFER_URL"),
            ),
        )

    @patch.object(
        ONLINE_GATE,
        "run_stage",
        side_effect=(({"schema_version": "preflight"}, 1), ({"schema_version": "suite"}, 1)),
    )
    def test_preserves_explicit_run_id(self, _run_stage):
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

    @patch.object(
        ONLINE_GATE,
        "run_stage",
        side_effect=ONLINE_GATE.OnlineStageError(
            "preflight", "online_preflight_failed", "invalid service URL"
        ),
    )
    def test_preflight_owns_service_url_validation_and_failure_report(self, run_stage):
        self.environment["MODEL_URL"] = "http://localhost:invalid"
        with self.assertRaisesRegex(ONLINE_GATE.OnlineStageError, "invalid service URL"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )

        self.assertEqual(run_stage.call_count, 1)
        report = (self.root / "artifacts/online-report.json").read_text(encoding="utf-8")
        self.assertIn('"error_code": "online_preflight_failed"', report)
        self.assertIn('"service_addresses": {}', report)

    def test_rejects_non_positive_timeout(self):
        self.environment["ADDP_ONLINE_TEST_TIMEOUT_SECONDS"] = "0"
        with self.assertRaisesRegex(ONLINE_GATE.OnlineGateError, "greater than zero"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )

    @patch.object(
        ONLINE_GATE,
        "run_stage",
        side_effect=ONLINE_GATE.OnlineStageError(
            "preflight", "online_preflight_timeout", "timed out"
        ),
    )
    def test_records_timeout_with_stable_error_code(self, _run_stage):
        with self.assertRaisesRegex(ONLINE_GATE.OnlineStageError, "timed out"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )
        report = (self.root / "artifacts/online-report.json").read_text(encoding="utf-8")
        self.assertIn('"error_code": "online_preflight_timeout"', report)
        self.assertIn('"failure_stage": "preflight"', report)

    @patch.object(ONLINE_GATE.time, "monotonic", side_effect=(10.0, 12.0, 13.0))
    @patch.object(
        ONLINE_GATE,
        "run_stage",
        return_value=({"schema_version": "preflight"}, 1),
    )
    def test_rejects_exhausted_total_timeout(self, run_stage, _monotonic):
        self.environment["ADDP_ONLINE_TEST_TIMEOUT_SECONDS"] = "1"
        with self.assertRaisesRegex(ONLINE_GATE.OnlineStageError, "total timeout"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )
        self.assertEqual(run_stage.call_count, 1)

    def test_rejects_concurrent_same_suite_and_run_id(self):
        self.environment["ADDP_ONLINE_TEST_RUN_ID"] = "nightly-42"
        lock_root = self.root / "locks"
        lock_root.mkdir()
        lock_path = lock_root / "owner-scenario--nightly-42.lock"
        with lock_path.open("a+") as lock:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(ONLINE_GATE.OnlineStageError, "already active"):
                ONLINE_GATE.run_online(
                    "owner-scenario",
                    self.repository,
                    self.environment,
                    self.suites,
                    lock_root=lock_root,
                )
        report = (self.root / "artifacts/online-report.json").read_text(encoding="utf-8")
        self.assertIn('"error_code": "online_run_active"', report)

    def test_rejects_artifact_directory_inside_repository(self):
        self.environment["ADDP_ONLINE_ARTIFACT_DIR"] = str(self.repository / "artifacts")
        with self.assertRaisesRegex(ONLINE_GATE.OnlineGateError, "outside"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )


if __name__ == "__main__":
    unittest.main()
