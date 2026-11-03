"""Real Harness/Tool/SDK, controlled HTTP and inference; not live execution."""
import copy
import json
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
from uuid import uuid4

import httpx
from addp_common.client import Message
from agents.checkpoint import new_checkpoint
from agents.main_agent import _load_skill_registry
from agents.transfer_run import START_TOOL, GET_TOOL, confirm_run_review, validate_start
from graph.factory import AgentFactory
from tests.test_deerflow_runtime import _InferenceFixture, _call, _model

ROOT = Path(__file__).resolve().parents[3]


class TransferRunRuntimeTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.contexts = {}
        for filename in ("transfer.json", "transfer-start.json", "transfer-execution.json"):
            value = json.loads((ROOT / "ontology/backend/internal/platform" / filename).read_text())
            value["digest"] = "a" * 64
            self.contexts[value["capability"]] = value
        self.approval_id, self.execution_id = str(uuid4()), str(uuid4())
        self.requests = []
        self.checkpoint = new_checkpoint()
        self.result = {"execution_id": self.execution_id, "task_id": 7, "status": "pending",
                       "records_read": 0, "records_written": 0}

    async def invoke(self, responses=(), *, resuming=False, commit=True, capability=START_TOOL, language="zh-cn"):
        events = []
        inference = _InferenceFixture(list(responses))
        async def transport(client, method, path, **kwargs):
            self.requests.append((method, path, copy.deepcopy(kwargs)))
            request = httpx.Request(method, "http://fixture" + path)
            if path == "/api/v1/system/auth/delegations":
                return httpx.Response(200, request=request, json={**kwargs["json"], "access_token": "addp_dat_fixture"})
            if path.startswith("/api/v1/ontology/platform/capabilities/"):
                return httpx.Response(200, request=request, json=self.contexts[path.rsplit("/", 1)[1]])
            if path == "/api/v1/transfer/task-definitions/7/start":
                if kwargs["json"]:
                    self.assertEqual(kwargs["json"], {"approval_id": self.approval_id, "request_fingerprint": "b" * 64})
                    return httpx.Response(200, request=request, json=self.result)
                return httpx.Response(202, request=request, json={
                    "status": "approval_required", "interaction_id": self.approval_id,
                    "request_fingerprint": "b" * 64, "request_summary": {"task_id": 7},
                    "expires_at": "2026-11-02T00:15:00Z",
                })
            self.assertEqual(path, f"/api/v1/transfer/executions/{self.execution_id}")
            return httpx.Response(200, request=request, json=self.result)

        with patch("addp_common.client.base.BaseClient._request", new=transport):
            async for event in AgentFactory.run({
                "skill_name": "transfer-generation", "user_request": "运行任务7",
                "context_summary": "", "user_id": 42, "tenant_id": 7,
                "token": "private-user-token", "agent_run_id": "fixture-run",
                "checkpoint": self.checkpoint, "resuming": resuming,
                "platform_capability": capability,
                "accept_language": language,
            }, "fixture", _load_skill_registry()["transfer-generation"].tools,
                max_iterations=len(responses)+1, llm=_model(inference)):
                events.append(event)
                if event.checkpoint_committed is not None:
                    if commit: event.checkpoint_committed.set_result(None)
                    else: event.checkpoint_committed.set_exception(RuntimeError("fixture_commit_failed"))
        checkpoints = [event.payload["checkpoint"] for event in events if event.kind == "checkpoint"]
        if checkpoints: self.checkpoint = checkpoints[-1]
        return events, inference

    async def request_review(self):
        events, _ = await self.invoke([Message(role="assistant", tool_calls=[_call(START_TOOL, {"task_id": 7})])])
        self.assertTrue(any(e.kind == "interaction_required" and e.payload["owner"] == "transfer" for e in events))
        self.assertEqual(self.checkpoint["observed"]["run_approval"]["approval_id"], self.approval_id)
        return events

    def owner_decision(self, status="approved"):
        answer = {"status": status, "approval_id": self.approval_id, "request_fingerprint": "b" * 64}
        if status == "consumed": answer["execution_id"] = self.execution_id
        confirm_run_review(self.checkpoint, SimpleNamespace(owner_interaction_id=self.approval_id, answer=answer))

    async def test_first_request_pauses_without_deciding_or_running(self):
        await self.request_review()
        calls = [r for r in self.requests if r[1].startswith("/api/v1/transfer/")]
        self.assertEqual([(m, p, options["json"]) for m, p, options in calls],
                         [("POST", "/api/v1/transfer/task-definitions/7/start", {})])
        lookups = [r[1] for r in self.requests if r[1].startswith('/api/v1/ontology/')]
        self.assertEqual(lookups, ['/api/v1/ontology/platform/capabilities/transfer.task.start'])
        with self.assertRaisesRegex(ValueError, "owner_run_approval"):
            validate_start({"task_id": 7, "approval_id": self.approval_id, "request_fingerprint": "b" * 64}, self.checkpoint)

    async def test_read_observation_needs_no_create_or_start_definition(self):
        self.contexts = {GET_TOOL: self.contexts[GET_TOOL]}
        _, inference = await self.invoke([
            Message(role='assistant', tool_calls=[_call(GET_TOOL, {'execution_id': self.execution_id})]),
            Message(role='assistant', content='执行尚未完成。'),
        ], capability=GET_TOOL)
        lookups = [r[1] for r in self.requests if r[1].startswith('/api/v1/ontology/')]
        self.assertEqual(lookups, ['/api/v1/ontology/platform/capabilities/transfer.execution.get'])
        self.assertFalse(any(r[0] == 'POST' and r[1].startswith('/api/v1/transfer/') for r in self.requests))
        tools = json.dumps(inference.calls[0]['tools'], default=lambda value: value.model_dump())
        self.assertNotIn('transfer__task__create', tools)
        self.assertNotIn('transfer__task__start', tools)

    async def test_unknown_read_condition_stops_before_owner_observation(self):
        self.contexts[GET_TOOL]["operation"]["inputs_required"].append("future_read_condition")
        self.contexts[GET_TOOL]["concepts"].append({"id": "future_read_condition", "name": {"zh-cn": "未实现条件", "en": "Unimplemented condition"}})
        events, _ = await self.invoke([
            Message(role='assistant', tool_calls=[_call(GET_TOOL, {'execution_id': self.execution_id})]),
            Message(role='assistant', content='当前读取条件尚未实现，不能查询。'),
        ], capability=GET_TOOL)
        self.assertFalse(any('/api/v1/transfer/executions/' in r[1] for r in self.requests))
        self.assertTrue(any(e.kind == 'tool_result' and e.payload.get('error_code') == 'platform_condition_unsatisfied' for e in events))

    async def test_approved_resume_is_deterministic_and_consumes_local_confirmation(self):
        await self.request_review(); self.owner_decision(); self.requests.clear()
        events, inference = await self.invoke(resuming=True)
        self.assertEqual(inference.calls, [])
        writes = [r for r in self.requests if r[1].startswith("/api/v1/transfer/task-definitions/")]
        self.assertEqual(len(writes), 1)
        self.assertIsNone(self.checkpoint["confirmed"]["run_approval"])
        self.assertFalse(any(e.kind == "run_failed" for e in events))
        self.assertTrue(any(e.kind == "text" and self.execution_id in e.payload["delta"] and "尚未同步完成" in e.payload["delta"] for e in events))

    async def test_rejected_or_consumed_does_not_submit_another_start(self):
        for state in ("rejected", "consumed"):
            with self.subTest(state=state):
                await self.request_review(); self.owner_decision(state); self.requests.clear()
                events, inference = await self.invoke(resuming=True)
                self.assertEqual(inference.calls, [])
                self.assertFalse(any(r[1].startswith("/api/v1/transfer/task-definitions/") for r in self.requests))
                self.assertFalse(any(e.kind == "run_failed" for e in events))

    async def test_changed_semantics_does_not_start(self):
        await self.request_review(); self.owner_decision(); self.requests.clear()
        self.contexts[START_TOOL]["digest"] = "c" * 64
        events, inference = await self.invoke(resuming=True)
        self.assertEqual(inference.calls, [])
        self.assertFalse(any(r[1].startswith("/api/v1/transfer/task-definitions/") for r in self.requests))
        self.assertTrue(any(e.kind == "run_failed" and e.payload["error_code"] == "run_review_changed" for e in events))

    async def test_expired_owner_review_ends_without_model_or_transfer_call(self):
        for language, text in (("zh-cn", "审批已过期，未启动。"),
                               ("en", "The approval has expired. No start was submitted.")):
            with self.subTest(language=language):
                await self.request_review()
                self.owner_decision("expired")
                # A newer capability cannot revive an expired owner approval,
                # nor require a new review just to end the old waiting state.
                self.contexts[START_TOOL]["digest"] = "c" * 64
                self.requests.clear()
                events, inference = await self.invoke(resuming=True, language=language)
                self.assertEqual(inference.calls, [])
                self.assertFalse(any("/api/v1/transfer/" in path for _, path, _ in self.requests))
                self.assertIsNone(self.checkpoint["confirmed"]["run_approval"])
                self.assertFalse(any(e.kind in {"run_failed", "interaction_required"} for e in events))
                self.assertTrue(any(e.kind == "text" and e.payload["delta"] == text for e in events))
                with self.assertRaisesRegex(ValueError, "owner_run_approval"):
                    validate_start({"task_id": 7, "approval_id": self.approval_id,
                                    "request_fingerprint": "b" * 64}, self.checkpoint)

    async def test_failed_commit_has_no_owner_start(self):
        await self.request_review(); self.owner_decision(); self.requests.clear()
        with self.assertRaisesRegex(RuntimeError, "fixture_commit_failed"):
            await self.invoke(resuming=True, commit=False)
        self.assertFalse(any(r[1].startswith("/api/v1/transfer/task-definitions/") for r in self.requests))
