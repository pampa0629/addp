import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("release-gate.py")
SPEC = importlib.util.spec_from_file_location("release_gate", SCRIPT)
RELEASE_GATE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = RELEASE_GATE
SPEC.loader.exec_module(RELEASE_GATE)


class ReleaseGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-release-gate-")
        self.root = Path(self.temporary.name)
        self.repository = self.root / "repository"
        self.repository.mkdir()
        self.artifacts = self.root / "artifacts"
        self.environment = {
            "ADDP_RELEASE_ARTIFACT_DIR": str(self.artifacts),
            "ADDP_RELEASE_TEST_SECRET": "never-persist-this-secret",
        }
        self.suites = {
            "sample-product": RELEASE_GATE.Suite(
                target="test-sample-release",
                artifact_environment=(("SAMPLE_REPORT", "sample.json"),),
                owner_report="sample.json",
            )
        }

    def tearDown(self) -> None:
        self.temporary.cleanup()

    @patch.object(RELEASE_GATE.time, "monotonic", side_effect=(10.0, 12.5))
    @patch.object(RELEASE_GATE.subprocess, "run")
    def test_dispatches_owner_target_and_writes_report(self, run, _monotonic) -> None:
        def execute(command, *, cwd, env, check):
            self.assertEqual(command, ("make", "--no-print-directory", "test-sample-release"))
            self.assertEqual(cwd, self.repository)
            self.assertFalse(check)
            Path(env["SAMPLE_REPORT"]).write_text('{"result":"passed"}\n', encoding="utf-8")
            self.assertEqual(env["ADDP_RELEASE_SUITE"], "sample-product")
            return subprocess.CompletedProcess(command, 0)

        run.side_effect = execute
        report = RELEASE_GATE.run_release(
            "sample-product", self.repository, self.environment, self.suites
        )

        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["duration_ms"], 2500)
        self.assertEqual(report["artifacts"], ["sample.json"])
        persisted = json.loads(
            (self.artifacts / "release-report.json").read_text(encoding="utf-8")
        )
        persisted_text = (self.artifacts / "release-report.json").read_text(
            encoding="utf-8"
        )
        self.assertEqual(persisted["schema_version"], "addp.release-gate/v1")
        self.assertEqual(persisted["owner_report"], "sample.json")
        self.assertNotIn("never-persist-this-secret", persisted_text)
        summary = (self.artifacts / "release-summary.md").read_text(encoding="utf-8")
        self.assertIn("| sample-product | `test-sample-release` | passed |", summary)

    @patch.object(RELEASE_GATE.time, "monotonic", side_effect=(1.0, 2.0))
    @patch.object(
        RELEASE_GATE.subprocess,
        "run",
        return_value=subprocess.CompletedProcess(("make",), 7),
    )
    def test_owner_failure_still_writes_failed_report(self, _run, _monotonic) -> None:
        with self.assertRaisesRegex(RELEASE_GATE.ReleaseOwnerGateError, "status 7"):
            RELEASE_GATE.run_release(
                "sample-product", self.repository, self.environment, self.suites
            )
        report = json.loads(
            (self.artifacts / "release-report.json").read_text(encoding="utf-8")
        )
        self.assertEqual(report["result"], "failed")
        self.assertEqual(report["error_code"], "release_owner_gate_failed")

    def test_registers_only_current_t5_suites(self) -> None:
        self.assertEqual(
            set(RELEASE_GATE.SUITES),
            {"agent-evaluation", "common-python-cli"},
        )
        self.assertEqual(
            RELEASE_GATE.SUITES["agent-evaluation"].target,
            "test-agent-eval-release",
        )
        self.assertEqual(
            RELEASE_GATE.SUITES["common-python-cli"].target,
            "test-common-python-cli-release",
        )

    def test_rejects_unknown_suite(self) -> None:
        with self.assertRaisesRegex(RELEASE_GATE.ReleaseGateError, "registered suites"):
            RELEASE_GATE.run_release(
                "missing", self.repository, self.environment, self.suites
            )

    def test_rejects_relative_artifact_directory(self) -> None:
        self.environment["ADDP_RELEASE_ARTIFACT_DIR"] = "artifacts"
        with self.assertRaisesRegex(RELEASE_GATE.ReleaseGateError, "absolute"):
            RELEASE_GATE.run_release(
                "sample-product", self.repository, self.environment, self.suites
            )

    def test_rejects_artifact_directory_inside_repository(self) -> None:
        self.environment["ADDP_RELEASE_ARTIFACT_DIR"] = str(
            self.repository / "artifacts"
        )
        with self.assertRaisesRegex(RELEASE_GATE.ReleaseGateError, "outside"):
            RELEASE_GATE.run_release(
                "sample-product", self.repository, self.environment, self.suites
            )

    def test_rejects_non_empty_artifact_directory(self) -> None:
        self.artifacts.mkdir()
        (self.artifacts / "stale.whl").write_text("stale", encoding="utf-8")
        with self.assertRaisesRegex(RELEASE_GATE.ReleaseGateError, "empty"):
            RELEASE_GATE.run_release(
                "sample-product", self.repository, self.environment, self.suites
            )


if __name__ == "__main__":
    unittest.main()
