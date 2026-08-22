import hashlib
import json
import sys
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path
from unittest.mock import patch


REPO_ROOT = Path(__file__).resolve().parents[3]
EVAL_ROOT = REPO_ROOT / "evals" / "agent-scenarios"
sys.path.insert(0, str(EVAL_ROOT))

from gate import GateFailure, build_report, run_offline_checks  # noqa: E402


class AgentEvaluationGateTests(unittest.TestCase):
    @patch("gate._run_check")
    def test_offline_checks_use_the_gate_owned_runtime(self, run_check):
        run_check.return_value = {"name": "stub", "status": "passed", "duration_ms": 0}

        run_offline_checks()

        calls = run_check.call_args_list
        self.assertEqual(len(calls), 2)
        self.assertEqual(calls[0].args[0], "agent_evaluation_and_persistence")
        self.assertEqual(calls[0].args[1][0], str(REPO_ROOT / "agent/backend/venv/bin/python"))
        self.assertEqual(calls[1].args[0], "common_python")
        self.assertEqual(calls[1].args[1][0], str(REPO_ROOT / "agent/backend/venv/bin/python"))

    def test_offline_contract_gate_discovers_all_scenarios(self):
        report = build_report(EVAL_ROOT)
        self.assertEqual(report["schema"], "addp.agent-evaluation-gate/v2")
        self.assertEqual(report["status"], "passed")
        self.assertEqual(set(report["source"]), {"revision", "worktree_dirty"})
        self.assertEqual(len(report["source"]["revision"]), 40)
        self.assertIsInstance(report["source"]["worktree_dirty"], bool)
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
        self.assertTrue(all(entry["online_evidence"] is None for entry in report["scenarios"]))
        self.assertTrue(all(len(entry["contract_sha256"]) == 64 for entry in report["scenarios"]))
        self.assertEqual(
            {key: value for key, value in report["checks"][0].items() if key != "duration_ms"},
            {"name": "scenario_contracts", "status": "passed", "count": 4},
        )
        self.assertGreaterEqual(report["checks"][0]["duration_ms"], 0)

    def test_require_online_rejects_missing_golden_evidence(self):
        report = build_report(EVAL_ROOT, require_online=True)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(
            [entry["online"] for entry in report["scenarios"] if entry["name"] in {"read-only-query", "approval-execution", "rejection-and-forbidden"}],
            ["missing", "missing", "missing"],
        )

    def test_require_online_rejects_missing_golden_scenario_contract(self):
        with tempfile.TemporaryDirectory() as directory:
            scenario_root = Path(directory)
            for name in ("approval-execution", "railway-farmland-area", "read-only-query"):
                (scenario_root / name).symlink_to(EVAL_ROOT / name, target_is_directory=True)
            report = build_report(scenario_root, require_online=True)
            self.assertEqual(report["status"], "failed")
            self.assertIn(
                "rejection-and-forbidden.offline: 缺少黄金场景契约",
                report["failures"],
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
            report = build_report(
                EVAL_ROOT,
                {"read-only-query": path},
                now=datetime(2026, 7, 18, 0, 0, tzinfo=timezone.utc),
            )
            self.assertEqual(report["status"], "failed")
            self.assertEqual(
                next(entry["online"] for entry in report["scenarios"] if entry["name"] == "read-only-query"),
                "failed",
            )
            self.assertIn("禁止字段", report["failures"][0])

    def test_online_evidence_rejects_stale_created_at(self):
        with tempfile.TemporaryDirectory() as directory:
            path = self._write_read_only_evidence(Path(directory), "2026-07-16T11:59:59Z")
            report = build_report(
                EVAL_ROOT,
                {"read-only-query": path},
                now=datetime(2026, 7, 17, 12, 0, tzinfo=timezone.utc),
            )
            self.assertEqual(report["status"], "failed")
            self.assertEqual(
                next(entry["online"] for entry in report["scenarios"] if entry["name"] == "read-only-query"),
                "stale",
            )

    def test_online_evidence_adds_only_audit_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            path = self._write_read_only_evidence(Path(directory), "2026-07-17T12:00:00Z")
            report = build_report(
                EVAL_ROOT,
                {"read-only-query": path},
                now=datetime(2026, 7, 17, 12, 0, tzinfo=timezone.utc),
            )
            entry = next(entry for entry in report["scenarios"] if entry["name"] == "read-only-query")
            self.assertEqual(
                entry["online_evidence"],
                {
                    "created_at": "2026-07-17T12:00:00+00:00",
                    "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                },
            )
            serialized = json.dumps(report)
            for forbidden in ("approval_id", "request_fingerprint", "open_url", '"trace"', "agent_run_id"):
                self.assertNotIn(forbidden, serialized)

    def test_source_identity_is_strict(self):
        with self.assertRaises(GateFailure):
            build_report(
                EVAL_ROOT,
                source={"revision": "z" * 40, "worktree_dirty": False},
            )

    def test_online_evidence_rejects_future_created_at(self):
        with tempfile.TemporaryDirectory() as directory:
            path = self._write_read_only_evidence(Path(directory), "2026-07-17T12:05:01Z")
            report = build_report(
                EVAL_ROOT,
                {"read-only-query": path},
                now=datetime(2026, 7, 17, 12, 0, tzinfo=timezone.utc),
            )
            self.assertEqual(report["status"], "failed")
            self.assertIn("未来时钟偏差", report["failures"][0])

    def test_online_evidence_requires_timezone(self):
        with tempfile.TemporaryDirectory() as directory:
            path = self._write_read_only_evidence(Path(directory), "2026-07-17T12:00:00")
            report = build_report(
                EVAL_ROOT,
                {"read-only-query": path},
                now=datetime(2026, 7, 17, 12, 0, tzinfo=timezone.utc),
            )
            self.assertEqual(report["status"], "failed")
            self.assertIn("必须包含时区", report["failures"][0])

    def _write_read_only_evidence(self, directory: Path, created_at: str) -> Path:
        path = directory / "read-only.json"
        path.write_text(
            json.dumps(
                {
                    "schema": "addp.agent-online-evidence/v1",
                    "scenario": "read-only-query",
                    "skill": "workflow-analysis",
                    "created_at": created_at,
                    "approval": None,
                    "trace": {"skill": "workflow-analysis", "phases": []},
                }
            ),
            encoding="utf-8",
        )
        return path


if __name__ == "__main__":
    unittest.main()
