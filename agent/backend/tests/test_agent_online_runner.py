import argparse
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


REPO_ROOT = Path(__file__).resolve().parents[3]
SCENARIOS_ROOT = REPO_ROOT / "evals" / "agent-scenarios"
sys.path.insert(0, str(SCENARIOS_ROOT))

import online_runner
from addp_common.tools import ToolExecutionError


class _Executor:
    def __init__(self, results):
        self.results = list(results)
        self.calls = []

    async def call(self, name, arguments, *, agent_run_id, tool_call_id):
        self.calls.append((name, arguments, agent_run_id, tool_call_id))
        result = self.results.pop(0)
        if isinstance(result, Exception):
            raise result
        return result


class AgentOnlineRunnerTests(unittest.IsolatedAsyncioTestCase):
    async def test_source_token_only_uses_oauth_refresh(self):
        async def refresh(base_url):
            self.assertEqual(base_url, "http://gateway")
            return "addp_at_refreshed"

        with (
            patch.dict(online_runner.os.environ, {"ADDP_TOKEN": "addp_at_manual"}),
            patch.object(online_runner, "refresh_access_token", refresh),
        ):
            token = await online_runner._source_token("http://gateway")

        self.assertEqual(token, "addp_at_refreshed")

    async def test_read_only_uses_explicit_observed_locator_and_evaluates_trace(self):
        locator = "addp://engine/8/path/public/railway?type=table&item_id=60"
        executor = _Executor(
            [
                {"total": 1, "results": [{"location": {"locator": locator}}]},
                {"columns": ["name"], "rows": [{"name": "railway"}], "total": 1},
            ]
        )
        args = argparse.Namespace(query="铁路", locator=locator, limit=10)

        evidence, status = await online_runner._read_only(args, executor)

        self.assertEqual(status, "passed")
        self.assertEqual(evidence["schema"], online_runner.EVIDENCE_SCHEMA)
        self.assertEqual(evidence["trace"]["phases"][0]["presentations"], ["TablePreview"])
        self.assertEqual(executor.calls[1][1]["locator"], locator)
        self.assertNotIn("results", json.dumps(evidence, ensure_ascii=False))

    async def test_read_only_rejects_locator_not_observed_in_search(self):
        executor = _Executor([{"total": 0, "results": []}])
        args = argparse.Namespace(
            query="铁路",
            locator="addp://engine/8/path/public/invented?type=table",
            limit=10,
        )

        with self.assertRaisesRegex(ValueError, "未出现在本次 data.search"):
            await online_runner._read_only(args, executor)

        self.assertEqual(len(executor.calls), 1)

    async def test_tool_failure_preserves_stable_owner_error_code(self):
        executor = _Executor([ToolExecutionError("owner_api_unavailable", "Manager 不可用")])
        args = argparse.Namespace(
            query="铁路",
            locator="addp://engine/8/path/public/railway?type=table",
            limit=10,
        )

        with self.assertRaises(online_runner.OnlineEvaluationError) as context:
            await online_runner._read_only(args, executor)

        self.assertEqual(context.exception.code, "owner_api_unavailable")

    def test_main_writes_stable_online_error_without_traceback(self):
        async def fail(_args):
            raise online_runner.OnlineEvaluationError("approval_forbidden", "审批不属于当前 AgentRun")

        with (
            patch.object(online_runner, "_parser") as parser,
            patch.object(online_runner, "_run", fail),
            patch("builtins.print") as output,
        ):
            parser.return_value.parse_args.return_value = argparse.Namespace()
            code = online_runner.main()

        self.assertEqual(code, 1)
        self.assertEqual(json.loads(output.call_args.args[0])["error"]["code"], "approval_forbidden")

    def test_evidence_path_must_stay_outside_repository(self):
        with self.assertRaisesRegex(ValueError, "必须位于 ADDP 仓库外"):
            online_runner._require_external_path(str(REPO_ROOT / "evidence.json"))

    def test_evidence_contract_rejects_unknown_fields(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "evidence.json"
            path.write_text(
                json.dumps(
                    {
                        "schema": online_runner.EVIDENCE_SCHEMA,
                        "scenario": "approval-execution",
                        "skill": "workflow-analysis",
                        "created_at": "2026-07-17T00:00:00+00:00",
                        "approval": None,
                        "trace": {},
                        "token": "must-not-be-accepted",
                    }
                ),
                encoding="utf-8",
            )

            with self.assertRaisesRegex(ValueError, "字段不符合唯一契约"):
                online_runner._load_evidence(path, "approval-execution")

    def test_evidence_contract_rejects_nested_sensitive_fields(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "evidence.json"
            path.write_text(
                json.dumps(
                    online_runner._evidence(
                        "approval-execution",
                        {"skill": "workflow-analysis", "phases": [], "metadata": {"token": "secret"}},
                    )
                ),
                encoding="utf-8",
            )

            with self.assertRaisesRegex(ValueError, "evidence.trace.metadata.token"):
                online_runner._load_evidence(path, "approval-execution")

    async def test_approval_execution_request_and_resume_merge_one_evaluated_trace(self):
        approval_id = "46ea0d75-b9bc-4b25-8d4a-441947081813"
        fingerprint = "a" * 64
        with tempfile.TemporaryDirectory() as directory:
            workflow_path = Path(directory) / "workflow.json"
            workflow_path.write_text('{"tasks":[{"id":"load"}]}', encoding="utf-8")
            request_executor = _Executor(
                [
                    {
                        "status": "approval_required",
                        "interaction_id": approval_id,
                        "open_url": f"/develop/approvals/{approval_id}",
                        "request_fingerprint": fingerprint,
                        "request_summary": {"workflow_engine_id": 20, "task_count": 1},
                        "expires_at": "2026-07-18T00:00:00Z",
                    }
                ]
            )
            request_args = argparse.Namespace(
                scenario="approval-execution",
                workflow_file=str(workflow_path),
                workflow_engine_id=20,
            )

            request_evidence, request_status = await online_runner._approval_request(
                request_args,
                request_executor,
            )
            evidence_path = Path(directory) / "approval.json"
            online_runner._write_evidence(evidence_path, request_evidence)
            resume_executor = _Executor([{"execution_id": "704bcbcc-ecbb-4f2f-a84e-d601eeac1fdd"}])

            final_evidence, final_status = await online_runner._approval_resume(
                argparse.Namespace(evidence=str(evidence_path)),
                resume_executor,
            )

        self.assertEqual(request_status, "awaiting_owner")
        self.assertEqual(final_status, "passed")
        phases = final_evidence["trace"]["phases"]
        self.assertEqual([phase["name"] for phase in phases], ["request", "resume"])
        self.assertEqual(phases[0]["agent_run_id"], phases[1]["agent_run_id"])
        self.assertEqual(phases[1]["result_refs"][0]["kind"], "execution")
        self.assertEqual(
            set(resume_executor.calls[0][1]),
            {"approval_id", "request_fingerprint"},
        )

    async def test_rejection_then_other_run_replay_preserves_stable_errors(self):
        approval_id = "46ea0d75-b9bc-4b25-8d4a-441947081813"
        fingerprint = "a" * 64
        with tempfile.TemporaryDirectory() as directory:
            evidence_path = Path(directory) / "rejection.json"
            online_runner._write_evidence(
                evidence_path,
                online_runner._evidence(
                    "rejection-and-forbidden",
                    {"skill": "workflow-analysis", "phases": []},
                    {
                        "agent_run_id": "run-owner",
                        "approval_id": approval_id,
                        "request_fingerprint": fingerprint,
                        "open_url": f"/develop/approvals/{approval_id}",
                    },
                ),
            )
            executor = _Executor(
                [
                    ToolExecutionError("approval_rejected", "审批已拒绝"),
                    ToolExecutionError("approval_forbidden", "审批不属于当前 AgentRun"),
                ]
            )

            with patch.object(
                online_runner,
                "_owner_approval_status",
                new=unittest.mock.AsyncMock(return_value="rejected"),
            ):
                evidence, status = await online_runner._approval_rejection(
                    argparse.Namespace(evidence=str(evidence_path)),
                    executor,
                )

        self.assertEqual(status, "passed")
        phases = evidence["trace"]["phases"]
        self.assertEqual(
            [tool["error_code"] for phase in phases for tool in phase["tools"]],
            ["approval_rejected", "approval_forbidden"],
        )
        self.assertNotEqual(phases[0]["agent_run_id"], phases[1]["agent_run_id"])

    async def test_rejection_runner_stops_before_tool_call_unless_owner_is_rejected(self):
        approval_id = "46ea0d75-b9bc-4b25-8d4a-441947081813"
        with tempfile.TemporaryDirectory() as directory:
            evidence_path = Path(directory) / "rejection.json"
            online_runner._write_evidence(
                evidence_path,
                online_runner._evidence(
                    "rejection-and-forbidden",
                    {"skill": "workflow-analysis", "phases": []},
                    {
                        "agent_run_id": "run-owner",
                        "approval_id": approval_id,
                        "request_fingerprint": "a" * 64,
                        "open_url": f"/develop/approvals/{approval_id}",
                    },
                ),
            )
            executor = _Executor([])
            with patch.object(
                online_runner,
                "_owner_approval_status",
                new=unittest.mock.AsyncMock(return_value="approved"),
            ):
                with self.assertRaises(online_runner.OnlineEvaluationError) as context:
                    await online_runner._approval_rejection(
                        argparse.Namespace(evidence=str(evidence_path)),
                        executor,
                    )

        self.assertEqual(context.exception.code, "approval_not_rejected")
        self.assertEqual(executor.calls, [])


if __name__ == "__main__":
    unittest.main()
