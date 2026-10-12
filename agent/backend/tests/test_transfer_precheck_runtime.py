"""Runtime-owned checks through the real Harness, Manifest adapter and SDK.

Only the HTTP transport and inference responses are controlled. No live task,
database row, scan or index is created by this gate.
"""

import asyncio
import copy
import json
import unittest
from unittest.mock import patch

import httpx
from addp_common.client import Message

from agents.checkpoint import confirm_selection
from agents.platform_conditions import CHECK_TOOL, CREATE_TOOL, REVIEW_REASON, prepare_review
from graph.factory import AgentFactory
from tests.test_deerflow_runtime import _InferenceFixture, _call, _model
from tests.test_platform_conditions import platform_fixture, precheck_fixture


class TransferPrecheckRuntimeTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.definition, _, _, self.arguments, self.checkpoint = platform_fixture()
        self.report = precheck_fixture(self.arguments, self.checkpoint)
        self.requests = []
        self.delegations = []
        self.check_result = self.report
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
            return httpx.Response(self.check_status, request=request, json=self.check_result)
        self.assertEqual(path, "/api/v1/transfer/task-definitions")
        return httpx.Response(201, request=request, json={
            "id": 101, "name": self.arguments["name"], "task_type": "sync", "status": "idle",
            "desired_state": "stopped", "schedule": "", "enabled": False,
        })

    async def run_factory(self, responses, allowed=None):
        client = _InferenceFixture(responses)
        events = []
        async def transport(owner, method, path, **kwargs):
            return await self.transport(owner, method, path, **kwargs)
        with patch("addp_common.client.base.BaseClient._request", new=transport):
            async for event in AgentFactory.run(
                task_context={
                    "skill_name": "transfer-generation", "user_request": "先建任务，不要运行", "context_summary": "",
                    "user_id": 1, "tenant_id": 1, "token": "private-user-token", "agent_run_id": "precheck-run",
                    "checkpoint": self.checkpoint,
                }, skill_body="fixture", allowed_tool_names=allowed or ["platform.capability.context", CHECK_TOOL, CREATE_TOOL],
                max_iterations=3, llm=_model(client),
            ):
                events.append(event)
                if event.checkpoint_committed is not None:
                    event.checkpoint_committed.set_result(None)
        return events, client

    def review_call(self):
        return Message(role="assistant", tool_calls=[_call("request_clarification", {
            "prompt": "model cannot declare the check passed", "reason": REVIEW_REASON,
            "options": [{"label": "model confirm", "value": "confirm"}, {"label": "model cancel", "value": "cancel"}],
            "operation_review": {"tool": CREATE_TOOL, "arguments": self.arguments},
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
        self.assertIn("business_rows", interaction["prompt"])
        self.assertIn("per_value_encoding", interaction["prompt"])
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

    async def test_malformed_owner_report_stops_and_does_not_downgrade_to_unknown(self):
        self.check_result = {"status": "supported"}
        events, _ = await self.run_factory([self.review_call()])
        self.assertEqual(self.writes(), [])
        self.assertFalse(any(e.kind == "interaction_required" for e in events))
        self.assertTrue(any(e.kind == "run_failed" for e in events))

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
