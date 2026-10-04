"""真实 Harness + Manifest Adapter + Inference Adapter 的确定性边界回归。"""

import asyncio
import json
import unittest
from unittest.mock import AsyncMock, patch

import httpx

from addp_common.client import ChatResponse, Message, ToolCall
from addp_common.client.inference import Usage
from addp_common.inference_langchain import InferenceChatModel
from addp_common.tools import ToolExecutionError

from graph.factory import AgentFactory


class _InferenceFixture:
    def __init__(self, responses):
        self.responses = list(responses)
        self.calls = []

    async def chat(self, **arguments):
        self.calls.append(arguments)
        return ChatResponse(
            schema_version="addp.inference/v1", message=self.responses.pop(0), usage=Usage(),
            deployment_id="fixture", profile_version=1,
        )


def _call(name, arguments=None, identity="call-1"):
    return ToolCall(id=identity, name=name.replace(".", "__"), arguments=arguments or {})


def _model(client, tenant_id=1):
    return InferenceChatModel.model_construct(client=client, tenant_id=tenant_id, model_profile_id="test-profile")


async def _collect(client, names, tenant_id=1, checkpoint=None, run_id="run-fixture"):
    return [event async for event in AgentFactory.run(
        task_context={
            "skill_name": "data-browse", "user_request": "查看数据", "context_summary": "",
            "user_id": tenant_id, "tenant_id": tenant_id, "token": "private-user-token",
            "agent_run_id": run_id, "checkpoint": checkpoint,
        }, skill_body="测试 Skill", allowed_tool_names=names, max_iterations=5, llm=_model(client, tenant_id),
    )]


class DeerFlowRuntimeTests(unittest.IsolatedAsyncioTestCase):
    async def test_explicit_illegal_null_is_not_silently_removed(self):
        client = _InferenceFixture([
            Message(role="assistant", tool_calls=[_call("data.search", {"query": "outdoor", "engine_id": None})]),
            Message(role="assistant", content="请指定合法引擎"),
        ])
        with patch("addp_common.client.base.BaseClient._request", new=AsyncMock()) as transport:
            events = await _collect(client, ["data.search"])
        transport.assert_not_awaited()
        self.assertEqual(next(e.payload["error_code"] for e in events if e.kind == "tool_result"), "invalid_arguments")

    async def test_harness_reaches_owner_only_with_audit_bound_delegated_token(self):
        requests = []

        async def transport(owner_client, method, path, **kwargs):
            requests.append(path)
            request = httpx.Request(method, "http://fixture" + path)
            if path == "/api/v1/system/auth/delegations":
                self.assertEqual(owner_client._client.headers["Authorization"], "Bearer private-user-token")
                arguments = kwargs["json"]
                self.assertEqual(arguments, {
                    "audience": "manager", "scopes": ["data.search"],
                    "agent_run_id": "run-fixture", "tool_call_id": "call-1",
                })
                return httpx.Response(200, request=request, json={**arguments, "access_token": "addp_dat_fixture"})
            self.assertEqual(path, "/api/v1/manager/search")
            self.assertEqual(owner_client._client.headers["Authorization"], "Bearer addp_dat_fixture")
            return httpx.Response(200, request=request, json={"data": {"total": 0, "results": []}})

        client = _InferenceFixture([
            Message(role="assistant", tool_calls=[_call("data.search", {"query": "outdoor", "limit": 5})]),
            Message(role="assistant", content="未找到数据"),
        ])
        with patch("addp_common.client.base.BaseClient._request", new=transport):
            events = await _collect(client, ["data.search"])
        self.assertEqual(requests, ["/api/v1/system/auth/delegations", "/api/v1/manager/search"],
                         [e.payload for e in events if e.kind == "tool_result"])
        self.assertFalse(next(e.payload["is_error"] for e in events if e.kind == "tool_result"))

    async def test_system_denial_never_contacts_owner(self):
        requests = []

        async def transport(owner_client, method, path, **kwargs):
            requests.append(path)
            return httpx.Response(403, request=httpx.Request(method, "http://fixture" + path), json={"error": "forbidden"})

        client = _InferenceFixture([
            Message(role="assistant", tool_calls=[_call("data.search", {"query": "outdoor", "limit": 5})]),
            Message(role="assistant", content="权限不足"),
        ])
        with patch("addp_common.client.base.BaseClient._request", new=transport):
            events = await _collect(client, ["data.search"])
        self.assertEqual(requests, ["/api/v1/system/auth/delegations"],
                         [e.payload for e in events if e.kind == "tool_result"])
        self.assertEqual(next(e.payload["error_code"] for e in events if e.kind == "tool_result"), "delegation_rejected")

    async def test_schema_rejection_is_a_failed_step_without_owner_call(self):
        client = _InferenceFixture([
            Message(role="assistant", tool_calls=[_call("data.search", {"limit": "invalid"})]),
            Message(role="assistant", content="请提供正确参数"),
        ])
        with patch("tools.langchain_tools.ToolExecutor.call", new=AsyncMock()) as executor:
            events = await _collect(client, ["data.search"])
        executor.assert_not_awaited()
        event = next(e for e in events if e.kind == "tool_result")
        self.assertTrue(event.payload["is_error"])
        self.assertEqual(event.payload["error_code"], "invalid_tool_arguments")

    async def test_delegation_denial_remains_a_tool_error(self):
        client = _InferenceFixture([
            Message(role="assistant", tool_calls=[_call("data.search", {"query": "outdoor", "limit": 5})]),
            Message(role="assistant", content="当前账号无权限"),
        ])
        with patch("tools.langchain_tools.ToolExecutor.call", side_effect=ToolExecutionError("permission_denied", "权限不足")):
            events = await _collect(client, ["data.search"])
        event = next(e for e in events if e.kind == "tool_result")
        self.assertTrue(event.payload["is_error"])
        self.assertEqual(event.payload["error_code"], "permission_denied")

    async def test_framework_loop_stop_fails_instead_of_claiming_completion(self):
        client = _InferenceFixture([
            Message(role="assistant", tool_calls=[_call("engine.list", {"capability": "all"}, f"loop-{index}")])
            for index in range(6)
        ])
        with patch("tools.langchain_tools.ToolExecutor.call", new=AsyncMock(return_value=[])):
            with self.assertRaisesRegex(RuntimeError, "agent_loop_detected"):
                await _collect(client, ["engine.list"])

    async def test_real_manifest_adapter_preserves_complete_result_and_audit_ids(self):
        result = {"total": 1, "results": [], "detail": "完整结果" * 1600}
        client = _InferenceFixture([
            Message(role="assistant", tool_calls=[_call("data.search", {"query": "outdoor", "limit": 5})]),
            Message(role="assistant", content="已读取"),
        ])
        with patch("tools.langchain_tools.ToolExecutor.call", new=AsyncMock(return_value=result)) as executor:
            events = await _collect(client, ["data.search"])
        executor.assert_awaited_once_with(
            "data.search", {"query": "outdoor", "limit": 5}, agent_run_id="run-fixture", tool_call_id="call-1",
        )
        self.assertEqual(json.loads(client.calls[1]["messages"][-1].content), result)
        self.assertEqual(json.loads(next(e.payload["content"] for e in events if e.kind == "tool_result")), result)
        self.assertEqual({t.name for t in client.calls[0]["tools"]}, {"data__search", "request_clarification"})
        self.assertNotIn("private-user-token", str(client.calls))

    async def test_unknown_tool_cannot_reach_executor(self):
        client = _InferenceFixture([
            Message(role="assistant", tool_calls=[_call("bash", {"command": "echo forbidden"})]),
            Message(role="assistant", content="不可执行"),
        ])
        with patch("tools.langchain_tools.ToolExecutor.call", new=AsyncMock()) as executor:
            events = await _collect(client, [])
        executor.assert_not_awaited()
        self.assertEqual(next(e.payload["error_code"] for e in events if e.kind == "tool_result"), "tool_not_allowed")

    async def test_clarification_preempts_sibling_write_and_rebuilds_same_run(self):
        call = _call("request_clarification", {
            "prompt": "请选择口径", "reason": "calculation_scope_ambiguous",
            "options": [{"label": "全部", "value": "all"}],
        }, "clarify-1")
        client = _InferenceFixture([Message(role="assistant", tool_calls=[
            _call("workflow.run", {"workflow_engine_id": 20}), call,
        ])])
        with patch("tools.langchain_tools.ToolExecutor.call", new=AsyncMock()) as executor:
            events = await _collect(client, ["workflow.run"])
            resumed = await _collect(_InferenceFixture([Message(role="assistant", content="继续处理")]), [], run_id="run-fixture")
        executor.assert_not_awaited()
        self.assertEqual([e.kind for e in events], ["tool_start", "tool_result", "interaction_required"])
        self.assertEqual([e.kind for e in resumed], ["text"])

    async def test_owner_approval_blocks_remaining_batch(self):
        client = _InferenceFixture([Message(role="assistant", tool_calls=[
            _call("workflow.run", {"workflow_engine_id": 20, "workflow_definition": {"tasks": []}}, "approval-1"),
            _call("data.search", {"query": "must not run", "limit": 5}, "sibling-1"),
        ])])
        result = {"status": "approval_required", "interaction_id": "owner-approval", "request_summary": {}}
        with patch("tools.langchain_tools.ToolExecutor.call", new=AsyncMock(return_value=result)) as executor:
            events = await _collect(client, ["workflow.run", "data.search"])
        self.assertEqual(executor.await_count, 1)
        self.assertEqual(events[-1].kind, "interaction_required")

    async def test_cancel_propagates_to_inflight_owner_call(self):
        started = asyncio.Event()
        cancelled = asyncio.Event()

        async def wait_owner(*args, **kwargs):
            started.set()
            try:
                await asyncio.Future()
            except asyncio.CancelledError:
                cancelled.set()
                raise

        client = _InferenceFixture([Message(role="assistant", tool_calls=[_call("data.search", {"query": "outdoor", "limit": 5})])])
        with patch("tools.langchain_tools.ToolExecutor.call", side_effect=wait_owner):
            task = asyncio.create_task(_collect(client, ["data.search"]))
            await asyncio.wait_for(started.wait(), timeout=5)
            task.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await task
            self.assertTrue(cancelled.is_set())

    async def test_tenant_runs_do_not_share_model_state(self):
        clients = [_InferenceFixture([Message(role="assistant", content=f"tenant-{tenant}")]) for tenant in (1, 2)]
        events = await asyncio.gather(*(_collect(client, [], tenant_id=tenant) for tenant, client in enumerate(clients, 1)))
        self.assertEqual([client.calls[0]["tenant_id"] for client in clients], [1, 2])
        self.assertEqual([group[0].payload["delta"] for group in events], ["tenant-1", "tenant-2"])
