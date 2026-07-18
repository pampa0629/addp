import json
import sys
import tempfile
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[3]
EVAL_ROOT = REPO_ROOT / "evals" / "agent-scenarios"
sys.path.insert(0, str(EVAL_ROOT))

from gate import GateFailure, build_report  # noqa: E402


class AgentEvaluationGateTests(unittest.TestCase):
    def test_offline_contract_gate_discovers_all_scenarios(self):
        report = build_report(EVAL_ROOT)
        self.assertEqual(report["schema"], "addp.agent-evaluation-gate/v1")
        self.assertEqual(report["status"], "passed")
        self.assertEqual(
            [entry["name"] for entry in report["scenarios"]],
            [
                "approval-execution",
                "railway-farmland-area",
                "read-only-query",
                "rejection-and-forbidden",
            ],
        )
        self.assertTrue(all(entry["offline"] == "passed" for entry in report["scenarios"]))
        self.assertTrue(all(entry["online"] == "not_provided" for entry in report["scenarios"]))
        self.assertEqual(report["checks"], [{"name": "scenario_contracts", "status": "passed", "count": 4}])

    def test_require_online_rejects_missing_golden_evidence(self):
        report = build_report(EVAL_ROOT, require_online=True)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(
            [entry["online"] for entry in report["scenarios"] if entry["name"] in {"read-only-query", "approval-execution", "rejection-and-forbidden"}],
            ["missing", "missing", "missing"],
        )

    def test_online_evidence_path_must_stay_outside_repository(self):
        with self.assertRaises(GateFailure):
            build_report(
                EVAL_ROOT,
                {"read-only-query": EVAL_ROOT / "read-only-query" / "scenario.yaml"},
            )

    def test_unknown_online_scenario_is_rejected(self):
        with self.assertRaises(GateFailure):
            build_report(EVAL_ROOT, {"unknown": Path("/tmp/unknown.json")})

    def test_online_evidence_rejects_nested_sensitive_fields(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "read-only.json"
            path.write_text(
                json.dumps(
                    {
                        "schema": "addp.agent-online-evidence/v1",
                        "scenario": "read-only-query",
                        "skill": "workflow-analysis",
                        "created_at": "2026-07-18T00:00:00Z",
                        "approval": None,
                        "trace": {"token": "secret"},
                    }
                ),
                encoding="utf-8",
            )
            report = build_report(EVAL_ROOT, {"read-only-query": path})
            self.assertEqual(report["status"], "failed")
            self.assertEqual(
                next(entry["online"] for entry in report["scenarios"] if entry["name"] == "read-only-query"),
                "failed",
            )
            self.assertIn("禁止字段", report["failures"][0])


if __name__ == "__main__":
    unittest.main()
