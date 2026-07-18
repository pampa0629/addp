import copy
import json
import os
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[3]
EVAL_ROOT = REPO_ROOT / "evals" / "agent-scenarios"
sys.path.insert(0, str(EVAL_ROOT))

from gate import (  # noqa: E402
    COMPARISON_SCHEMA,
    GateFailure,
    build_report,
    compare_reports,
    load_gate_report,
)


class AgentEvaluationComparisonTests(unittest.TestCase):
    def test_clean_comparison_reports_contract_evidence_and_duration_changes(self):
        baseline = self._online_report()
        current = copy.deepcopy(baseline)
        current["source"]["revision"] = "b" * 40
        current["scenarios"][0]["contract_sha256"] = "c" * 64
        current["scenarios"][0]["online_evidence"]["sha256"] = "d" * 64
        added_scenario = copy.deepcopy(current["scenarios"][1])
        added_scenario["name"] = "new-evaluation-scenario"
        current["scenarios"].append(added_scenario)
        current["checks"][0]["count"] = len(current["scenarios"])
        current["checks"][1]["duration_ms"] = 160
        current["checks"].append(
            {"name": "new_check", "status": "passed", "exit_code": 0, "duration_ms": 20}
        )

        comparison = compare_reports(
            baseline,
            current,
            now=datetime(2026, 7, 18, 12, 0, tzinfo=timezone.utc),
        )

        self.assertEqual(comparison["schema"], COMPARISON_SCHEMA)
        self.assertEqual(comparison["policy"], "comparison")
        self.assertEqual(comparison["status"], "passed")
        self.assertEqual(comparison["regressions"], [])
        self.assertEqual(comparison["release_readiness"], {"eligible": True, "blockers": []})
        self.assertEqual(comparison["summary"]["added_scenarios"], ["new-evaluation-scenario"])
        self.assertEqual(comparison["summary"]["added_checks"], ["new_check"])
        self.assertEqual(comparison["summary"]["changed_contracts"], ["approval-execution"])
        scenario = next(entry for entry in comparison["scenarios"] if entry["name"] == "approval-execution")
        self.assertTrue(scenario["contract_changed"])
        self.assertTrue(scenario["evidence_changed"])
        check = next(
            entry for entry in comparison["checks"] if entry["name"] == "agent_evaluation_and_persistence"
        )
        self.assertEqual(check["duration_ms"], {"baseline": 100, "current": 160, "delta": 60})
        self.assertNotIn("d" * 64, json.dumps(comparison))

    def test_regression_detects_removed_and_degraded_entries(self):
        baseline = self._online_report()
        current = copy.deepcopy(baseline)
        current["scenarios"] = [
            scenario for scenario in current["scenarios"] if scenario["name"] != "rejection-and-forbidden"
        ]
        current["checks"][0]["count"] = len(current["scenarios"])
        read_only = next(scenario for scenario in current["scenarios"] if scenario["name"] == "read-only-query")
        read_only["online"] = "failed"
        current["checks"][1]["status"] = "failed"
        current["checks"][1]["exit_code"] = 1
        current["status"] = "failed"
        current["failures"] = ["comparison fixture regression"]

        comparison = compare_reports(baseline, current)

        self.assertEqual(comparison["status"], "regressed")
        self.assertEqual(comparison["summary"]["removed_scenarios"], ["rejection-and-forbidden"])
        self.assertIn("scenario.rejection-and-forbidden: removed", comparison["regressions"])
        self.assertIn("scenario.read-only-query.online: passed -> failed", comparison["regressions"])
        self.assertIn("check.agent_evaluation_and_persistence: passed -> failed", comparison["regressions"])
        self.assertIn("report.current: failed", comparison["regressions"])

    def test_comparison_rejects_different_modes(self):
        with self.assertRaises(GateFailure):
            compare_reports(self._report(), self._online_report())

    def test_release_policy_accepts_clean_online_reports(self):
        comparison = compare_reports(
            self._online_report(),
            self._online_report(),
            require_release_ready=True,
        )
        self.assertEqual(comparison["policy"], "release")
        self.assertEqual(comparison["status"], "passed")
        self.assertEqual(comparison["release_readiness"], {"eligible": True, "blockers": []})

    def test_release_policy_blocks_dirty_or_offline_reports(self):
        with self.subTest("dirty"):
            baseline = self._online_report()
            current = self._online_report()
            current["source"]["worktree_dirty"] = True
            comparison = compare_reports(baseline, current, require_release_ready=True)
            self.assertEqual(comparison["status"], "blocked")
            self.assertEqual(
                comparison["release_readiness"],
                {"eligible": False, "blockers": ["current_worktree_dirty"]},
            )
        with self.subTest("offline"):
            comparison = compare_reports(self._report(), self._report(), require_release_ready=True)
            self.assertEqual(comparison["status"], "blocked")
            self.assertEqual(
                comparison["release_readiness"],
                {"eligible": False, "blockers": ["mode_not_online_required"]},
            )

    def test_comparison_strictly_rejects_v1_and_unknown_fields(self):
        baseline = self._report()
        current = self._report()
        with self.subTest("v1"):
            baseline["schema"] = "addp.agent-evaluation-gate/v1"
            with self.assertRaises(GateFailure):
                compare_reports(baseline, current)
        with self.subTest("unknown"):
            baseline = self._report()
            baseline["legacy_status"] = "passed"
            with self.assertRaises(GateFailure):
                compare_reports(baseline, current)

    def test_report_loader_requires_external_strict_json(self):
        with self.assertRaises(GateFailure):
            load_gate_report(EVAL_ROOT / "read-only-query" / "scenario.yaml")
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "report.json"
            report = self._report()
            report["schema"] = "addp.agent-evaluation-gate/v1"
            path.write_text(json.dumps(report), encoding="utf-8")
            with self.assertRaises(GateFailure):
                load_gate_report(path)

    def test_compare_shell_entry_supports_plain_compare_mode(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            baseline = root / "baseline.json"
            current = root / "current.json"
            output = root / "comparison.json"
            baseline.write_text(json.dumps(self._report()), encoding="utf-8")
            current.write_text(json.dumps(self._report()), encoding="utf-8")
            completed = subprocess.run(
                ["bash", str(REPO_ROOT / "scripts" / "test" / "agent-evaluation-gate.sh"), "compare"],
                cwd=REPO_ROOT,
                env={
                    **os.environ,
                    "ADDP_AGENT_EVAL_BASELINE": str(baseline),
                    "ADDP_AGENT_EVAL_CURRENT": str(current),
                    "ADDP_AGENT_EVAL_REPORT": str(output),
                },
                capture_output=True,
                text=True,
                timeout=30,
                check=False,
            )
            self.assertEqual(completed.returncode, 0, completed.stderr)
            self.assertEqual(json.loads(output.read_text(encoding="utf-8"))["policy"], "comparison")

    def _report(self):
        return build_report(
            EVAL_ROOT,
            checks=[
                {
                    "name": "agent_evaluation_and_persistence",
                    "status": "passed",
                    "exit_code": 0,
                    "duration_ms": 100,
                }
            ],
            now=datetime(2026, 7, 18, 11, 0, tzinfo=timezone.utc),
            source={"revision": "a" * 40, "worktree_dirty": False},
        )

    def _online_report(self):
        report = self._report()
        report["mode"] = "online_required"
        for scenario in report["scenarios"]:
            if scenario["name"] in {"read-only-query", "approval-execution", "rejection-and-forbidden"}:
                scenario["online"] = "passed"
                scenario["online_evidence"] = {
                    "created_at": "2026-07-18T10:00:00+00:00",
                    "sha256": "e" * 64,
                }
        return report


if __name__ == "__main__":
    unittest.main()
