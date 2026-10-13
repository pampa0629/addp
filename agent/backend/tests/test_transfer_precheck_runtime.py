"""Runtime-owned checks through the real Harness, Manifest adapter and SDK.

Only the HTTP transport and inference responses are controlled. No live task,
database row, scan or index is created by this gate.
"""

import asyncio
import copy
import json
import unittest
import sys
from pathlib import Path
from unittest.mock import patch

import httpx
from addp_common.client import Message

from agents.checkpoint import confirm_selection
from agents.main_agent import _load_skill_registry
from agents.platform_conditions import CHECK_TOOL, CREATE_TOOL, REVIEW_REASON, prepare_review
from graph.factory import AgentFactory
from tests.test_deerflow_runtime import _InferenceFixture, _call, _model
from tests.test_platform_conditions import platform_fixture, precheck_fixture

EVAL_ROOT = Path(__file__).resolve().parents[3] / "evals" / "agent-scenarios"
sys.path.insert(0, str(EVAL_ROOT))
from evaluator import evaluate_trace, load_scenario, phase_from_events


class TransferPrecheckRuntimeTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.definition, _, _, self.arguments, self.checkpoint = platform_fixture()
        self.report = precheck_fixture(self.arguments, self.checkpoint)
        self.requests = []
        self.delegations = []
        self.check_result = self.report
        self.check_results = []
        self.check_status = 200
        self.cancel_check = False
        self.check_started = asyncio.Event()

    async def transport(self, owner, method, path, **kwargs):
        self.requests.append((method, path, copy.deepcopy(kwargs)))
        request = httpx.Request(method, "http://fixture" + path)
        if path == "/api/v1/system/auth/delegations":
            body = kwargs["json"]
            self.delegations.append(body)
            self.assertEqual(owner._client.headers["Authorization"], "Bearer private-user-token")
            return httpx.Response(200, request=request, json={**body, "access_token": "addp_dat_fixture"})
        self.assertEqual(owner._client.headers["Authorization"], "Bearer addp_dat_fixture")
        if path == "/api/v1/ontology/platform/capabilities/transfer.task.create":
            return httpx.Response(200, request=request, json=self.definition)
        if path == "/api/v1/transfer/task-config-checks":
            if self.cancel_check:
                self.check_started.set()
                await asyncio.Event().wait()
            result = self.check_results.pop(0) if self.check_results else self.check_result
            return httpx.Response(self.check_status, request=request, json=result)
        self.assertEqual(path, "/api/v1/transfer/task-definitions")
        return httpx.Response(201, request=request, json={
            "id": 101, "name": self.arguments["name"], "task_type": "sync", "status": "idle",
            "desired_state": "stopped", "schedule": "", "enabled": False,
        })

    async def run_factory(self, responses, allowed=None, accept_language="zh-cn", user_request="先建任务，不要运行", skill_body="fixture"):
        client = _InferenceFixture(responses)
        events = []
        async def transport(owner, method, path, **kwargs):
            return await self.transport(owner, method, path, **kwargs)
        with patch("addp_common.client.base.BaseClient._request", new=transport):
            async for event in AgentFactory.run(
                task_context={
                    "skill_name": "transfer-generation", "user_request": user_request, "context_summary": "",
                    "user_id": 1, "tenant_id": 1, "token": "private-user-token", "agent_run_id": "precheck-run",
                    "checkpoint": self.checkpoint, "accept_language": accept_language,
                }, skill_body=skill_body, allowed_tool_names=allowed or ["platform.capability.context", CHECK_TOOL, CREATE_TOOL],
                max_iterations=3, llm=_model(client),
            ):
                events.append(event)
                if event.checkpoint_committed is not None:
                    event.checkpoint_committed.set_result(None)
        return events, client

    def review_call(self, arguments=None):
        return Message(role="assistant", tool_calls=[_call("request_clarification", {
            "prompt": "model cannot declare the check passed", "reason": REVIEW_REASON,
            "options": [{"label": "model confirm", "value": "confirm"}, {"label": "model cancel", "value": "cancel"}],
            "operation_review": {"tool": CREATE_TOOL, "arguments": arguments or self.arguments},
        })])

    def confirm(self):
        identity = prepare_review(self.arguments, self.checkpoint)
        confirm_selection(self.checkpoint, {"value": identity["fingerprint"], "candidate": {"operation_review": identity}})

    def writes(self):
        return [request for request in self.requests if request[1] == "/api/v1/transfer/task-definitions"]

    async def test_unknown_is_runtime_checked_audited_and_shown_before_confirmation(self):
        # No model check call: Runtime must still issue one using formal delegation.
        self.checkpoint["observed"]["config_checks"].clear()
        events, client = await self.run_factory([self.review_call()])
        interaction = next(e.payload for e in events if e.kind == "interaction_required")
        self.assertIn("unknown", interaction["prompt"])
        presentation = next(e.payload for e in events if e.kind == "presentation")
        self.assertIn("business_rows", presentation["report"]["not_checked"])
        self.assertIn("per_value_encoding", presentation["report"]["runtime_checks_required"])
        self.assertNotIn("business_rows", interaction["prompt"])
        self.assertEqual(interaction["candidates"][0]["label"], "知悉未核验项，仅创建任务")
        self.assertEqual(len(client.calls), 1)
        self.assertEqual(self.writes(), [])
        check = next(d for d in self.delegations if d["scopes"] == [CHECK_TOOL])
        self.assertEqual(check["audience"], "transfer")
        self.assertEqual(check["agent_run_id"], "precheck-run")
        self.assertTrue(check["tool_call_id"])
        result = next(e for e in events if e.kind == "tool_result" and e.payload["tool_name"] == CHECK_TOOL)
        self.assertEqual(result.payload["tool_call_id"], check["tool_call_id"])
        self.assertNotIn("private-user-token", str(events))

    async def test_creation_rechecks_exact_sdk_body_without_reconfirming_report_clock(self):
        self.confirm()
        self.check_result = {**self.report, "observed_at": "2026-10-11T00:01:00Z", "binding_hash": "f" * 64}
        events, _ = await self.run_factory([
            Message(role="assistant", tool_calls=[_call(CREATE_TOOL, self.arguments)]),
            Message(role="assistant", content="只创建未运行任务"),
        ])
        check = next(r for r in self.requests if r[1] == "/api/v1/transfer/task-config-checks")
        create = self.writes()
        self.assertEqual(len(create), 1)
        self.assertEqual(check[2]["json"], create[0][2]["json"])
        self.assertFalse(create[0][2]["json"]["enabled"])
        self.assertFalse(create[0][2]["json"]["auto_scan_metadata"])
        self.assertEqual(create[0][2]["json"]["schedule"], "")
        self.assertEqual([d["scopes"] for d in self.delegations], [["platform.capability.context"], [CHECK_TOOL], [CREATE_TOOL]])
        self.assertEqual(len({d["tool_call_id"] for d in self.delegations}), 3)
        self.assertTrue(any(e.checkpoint_committed is not None for e in events))

    async def test_unsupported_cannot_offer_confirmation_and_plain_draft_does_not_precheck(self):
        self.check_result = {**self.report, "status": "unsupported"}
        events, _ = await self.run_factory([self.review_call(), Message(role="assistant", content="请修正不支持的配置")])
        self.assertFalse(any(e.kind == "interaction_required" for e in events))
        self.assertEqual(self.writes(), [])
        self.requests.clear()
        events, _ = await self.run_factory([Message(role="assistant", content="这是草稿，尚未预检")])
        self.assertFalse(any(e.kind == "interaction_required" for e in events))
        self.assertFalse(any(r[1].startswith("/api/v1/transfer/") for r in self.requests))

    async def test_changed_report_or_unsupported_never_reaches_creation(self):
        self.confirm()
        for change in ({"not_checked": ["changed_schema"]}, {"status": "unsupported"}):
            self.requests.clear()
            self.check_result = {**self.report, **change}
            events, _ = await self.run_factory([
                Message(role="assistant", tool_calls=[_call(CREATE_TOOL, self.arguments)]),
                Message(role="assistant", content="需要修正或重新确认"),
            ])
            with self.subTest(change=change):
                self.assertEqual(self.writes(), [])
                result = next(e.payload for e in events if e.kind == "tool_result" and e.payload["tool_name"] == CREATE_TOOL)
                self.assertIn("user_review" if "not_checked" in change else "config_check_unsupported", result["content"])

    async def test_unsupported_diagnostics_reach_model_without_review_or_write(self):
        diagnostic = {
            "status": "unsupported", "code": "array_element_type_unsupported", "check": "target_definition",
            "config_path": "/config/transforms/0/fields/1", "source_field": "status", "target_field": "status",
        }
        self.check_result = {**self.report, "status": "unsupported", "diagnostics": [*self.report["diagnostics"], diagnostic]}
        for language, expected in (("zh-cn", "未创建任务"), ("en", "No task was created")):
            events, client = await self.run_factory(
                [self.review_call(), Message(role="assistant", content="请先修正目标字段定义")],
                accept_language=language,
            )
            with self.subTest(language=language):
                feedback = json.loads(next(m.content for m in client.calls[1]["messages"] if m.role == "tool"))
                self.assertEqual(feedback["error"]["code"], "platform_condition_unsatisfied")
                self.assertIn(expected, feedback["error"]["message"])
                self.assertEqual(feedback["config_check"], {
                    key: self.check_result[key]
                    for key in ("status", "checked", "not_checked", "runtime_checks_required", "diagnostics")
                })
                self.assertNotIn("principal_id", str(feedback))
                self.assertNotIn("config_hash", str(feedback))
                self.assertFalse(any(e.kind in {"interaction_required", "run_failed"} for e in events))
                self.assertEqual(self.writes(), [])

    async def test_fresh_unsupported_at_creation_returns_report_and_closes_both_steps(self):
        self.confirm()
        self.check_result = {**self.report, "status": "unsupported"}
        events, client = await self.run_factory([
            Message(role="assistant", tool_calls=[_call(CREATE_TOOL, self.arguments)]),
            Message(role="assistant", content="新的预检拒绝了配置，需要重新复核"),
        ])
        feedback = json.loads(next(m.content for m in client.calls[1]["messages"] if m.role == "tool"))
        self.assertEqual(feedback["config_check"]["diagnostics"], self.report["diagnostics"])
        self.assertEqual(feedback["config_check"]["status"], "unsupported")
        self.assertEqual(
            {e.payload["tool_call_id"] for e in events if e.kind == "tool_start"},
            {e.payload["tool_call_id"] for e in events if e.kind == "tool_result"},
        )
        self.assertEqual(self.writes(), [])
        self.assertFalse(any(e.kind == "interaction_required" for e in events))

    async def test_revised_draft_requires_fresh_formal_check_and_review(self):
        corrected = copy.deepcopy(self.arguments)
        corrected["config"]["transforms"][0]["fields"][1]["target_type"] = "json"
        self.check_results = [{**self.report, "status": "unsupported"}, self.report]
        events, client = await self.run_factory([self.review_call(), self.review_call(corrected)])
        checks = [r for r in self.requests if r[1] == "/api/v1/transfer/task-config-checks"]
        self.assertEqual(len(checks), 2)
        self.assertNotEqual(checks[0][2]["json"], checks[1][2]["json"])
        interaction = next(e.payload for e in events if e.kind == "interaction_required")
        self.assertIn('"target_type":"json"', interaction["prompt"])
        self.assertEqual(interaction["candidates"][0]["label"], "知悉未核验项，仅创建任务")
        self.assertEqual(len(client.calls), 2)
        self.assertEqual(self.writes(), [])
        self.assertEqual(self.checkpoint["confirmed"]["operation_reviews"], {})

    async def test_explicit_draft_check_preserves_array_field_without_review_or_write(self):
        registry = _load_skill_registry()
        skill_body = registry["transfer-generation"].load_body(registry)
        arguments = copy.deepcopy(self.arguments)
        arguments["config"]["source"]["query"]["statement"] = json.dumps({
            "aggregate": "events", "pipeline": [{"$project": {"id": "$_id", "status": 1, "members": 1, "_id": 0}}],
        })
        arguments["config"]["transforms"][0]["fields"].append({"source": "members", "target": "members", "target_type": "json"})
        for status in ("supported", "unknown", "unsupported"):
            self.requests.clear()
            self.check_result = {**self.report, "status": status}
            events, client = await self.run_factory([
                Message(role="assistant", tool_calls=[_call(CHECK_TOOL, arguments)]),
                Message(role="assistant", content="正式检查已返回，仍只展示草稿"),
            ], user_request="检查配置，只给草稿，不创建、不运行", skill_body=skill_body)
            with self.subTest(status=status):
                # Check production Skill composition reaches the actual Harness model.
                # Scripted replies do not prove a free model will stay concise.
                prompt = "\n".join(m.content for m in client.calls[1]["messages"] if m.role == "system")
                self.assertEqual(prompt.count("默认只给关键检查解释"), 1)
                self.assertIn("事实概要由 Runtime 固定卡片承担", prompt)
                self.assertIn("不要另写“草稿概要”", prompt)
                self.assertIn("不要逐项重列检查范围、未检查项、运行时要求或字段诊断", prompt)
                self.assertIn("用户明确要求详细说明", prompt)
                self.assertNotIn("按正式报告解释 supported、unsupported、unknown、字段诊断、未检查项及运行要求", prompt)
                checks = [r for r in self.requests if r[1] == "/api/v1/transfer/task-config-checks"]
                self.assertEqual(len(checks), 1)
                source = checks[0][2]["json"]["config"]["source"]
                project = json.loads(source["query"]["statement"])["pipeline"][0]["$project"]
                self.assertEqual(project["members"], 1)
                feedback = json.loads(next(m.content for m in client.calls[1]["messages"] if m.role == "tool"))
                self.assertEqual(feedback["status"], status)
                for key in ("checked", "not_checked", "runtime_checks_required", "diagnostics"):
                    self.assertEqual(feedback[key], self.check_result[key])
                self.assertEqual(self.writes(), [])
                self.assertFalse(any(e.kind == "interaction_required" for e in events))
                presentations = [e.payload for e in events if e.kind == "presentation"]
                self.assertEqual(len(presentations), 1)
                self.assertEqual(presentations[0]["kind"], "transfer_config_check")
                self.assertEqual(presentations[0]["report"]["status"], status)
                self.assertNotIn("principal_id", presentations[0]["report"])
                self.assertNotIn("binding_hash", presentations[0]["report"])
                phase = phase_from_events("draft_check", "fixture-draft-run", "completed", events,
                    owner_effects={"tasks_created": 0, "approvals_created": 0, "executions_created": 0, "business_rows_written": 0}, persisted_state={})
                evaluate_trace(load_scenario(EVAL_ROOT / "platform-transfer-draft-check"), {"skill": "transfer-generation", "phases": [phase]})

    async def test_formal_check_failure_clears_old_report_and_stops_sibling_create(self):
        self.confirm()
        self.check_status, self.check_result = 403, {"error": "forbidden"}
        events, client = await self.run_factory([Message(role="assistant", tool_calls=[
            _call(CREATE_TOOL, self.arguments), _call(CREATE_TOOL, self.arguments, "second-create"),
        ])])
        self.assertEqual(self.writes(), [])
        self.assertEqual(len(client.calls), 1)
        self.assertEqual(sum(r[1] == "/api/v1/transfer/task-config-checks" for r in self.requests), 1)
        self.assertTrue(any(e.kind == "run_failed" for e in events))
        before_failure = events[:next(i for i, e in enumerate(events) if e.kind == "run_failed")]
        self.assertEqual(
            {e.payload["tool_call_id"] for e in before_failure if e.kind == "tool_start"},
            {e.payload["tool_call_id"] for e in before_failure if e.kind == "tool_result"},
        )
        saved = [e.payload["checkpoint"] for e in events if e.kind == "checkpoint"][-1]
        self.assertEqual(saved["observed"]["config_checks"], {})
        self.assertFalse(any(e.kind == "presentation" for e in events))

    async def test_malformed_owner_report_stops_and_does_not_downgrade_to_unknown(self):
        self.check_result = {"status": "supported"}
        events, _ = await self.run_factory([self.review_call()])
        self.assertEqual(self.writes(), [])
        self.assertFalse(any(e.kind == "interaction_required" for e in events))
        self.assertTrue(any(e.kind == "run_failed" for e in events))
        self.assertFalse(any(e.kind == "presentation" for e in events))

    async def test_cancel_during_fresh_check_never_creates(self):
        from langgraph.errors import NodeCancelledError
        self.confirm()
        self.cancel_check = True
        run = asyncio.create_task(self.run_factory([Message(role="assistant", tool_calls=[_call(CREATE_TOOL, self.arguments)])]))
        try:
            await asyncio.wait_for(self.check_started.wait(), timeout=5)
        finally:
            run.cancel()
            with self.assertRaises((asyncio.CancelledError, NodeCancelledError)):
                await run
        self.assertEqual(self.writes(), [])

    async def test_missing_required_check_tool_fails_closed(self):
        with self.assertRaisesRegex(ValueError, "agent_skill_tool_not_found"):
            await self.run_factory([], allowed=["platform.capability.context", CREATE_TOOL])
        self.assertEqual(self.requests, [])
