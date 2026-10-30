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

from agents.checkpoint import capture_owner_facts, confirm_selection
from agents.main_agent import _load_skill_registry
from agents.platform_conditions import CHECK_TOOL, CREATE_TOOL, REVIEW_REASON, config_arguments_digest, prepare_review
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
        self.formal_facts = {}
        self.facts_status = self.context_status = self.create_status = 200

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
            return httpx.Response(self.context_status, request=request, json=self.definition)
        if path == "/api/v1/manager/resource-facts":
            return httpx.Response(self.facts_status, request=request, json=self.formal_facts[kwargs["params"]["locator"]])
        if path == "/api/v1/meta/resource-tree/12/node":
            return httpx.Response(self.facts_status, request=request, json=self.target_parent)
        if path == "/api/v1/transfer/task-config-checks":
            if self.cancel_check:
                self.check_started.set()
                await asyncio.Event().wait()
            result = self.check_results.pop(0) if self.check_results else self.check_result
            return httpx.Response(self.check_status, request=request, json=result)
        self.assertEqual(path, "/api/v1/transfer/task-definitions")
        return httpx.Response(self.create_status if self.create_status != 200 else 201, request=request, json={
            "id": 101, "name": self.arguments["name"], "task_type": "sync", "status": "idle",
            "desired_state": "stopped", "schedule": "", "enabled": False,
        })

    async def run_factory(self, responses, allowed=None, accept_language="zh-cn", user_request="先建任务，不要运行", skill_body="fixture", draft_arguments=None, resuming=False, commit=True):
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
                    "resuming": resuming,
                    **({"draft_arguments": draft_arguments} if draft_arguments is not None else {}),
                }, skill_body=skill_body, allowed_tool_names=allowed or ["platform.capability.context", CHECK_TOOL, CREATE_TOOL],
                max_iterations=3, llm=_model(client),
            ):
                events.append(event)
                if event.checkpoint_committed is not None:
                    if commit:
                        event.checkpoint_committed.set_result(None)
                    else:
                        event.checkpoint_committed.set_exception(RuntimeError("fixture_commit_failed"))
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

    def bind_draft(self):
        import uuid
        reference = f"draft:{uuid.uuid4()}:{config_arguments_digest(self.arguments)}"
        self.checkpoint["draft_ref"] = reference
        return reference

    async def test_confirmed_resume_creates_once_without_model_or_second_review(self):
        self.prepare_resume()
        events, client = await self.run_resume([self.review_call()])
        self.assertEqual(len(client.calls), 0)
        self.assertEqual(len(self.writes()), 1, [e.payload for e in events if e.kind in {"tool_result", "run_failed"}])
        self.assertEqual(next(e.payload["args"] for e in events if e.kind == "tool_start" and e.payload["tool_name"] == CREATE_TOOL), self.arguments)
        self.assertEqual(self.writes()[0][2]["json"]["config"], self.arguments["config"])
        self.assertFalse(any(e.kind in {"interaction_required", "run_failed"} for e in events))
        self.assertIn("101", next(e.payload["delta"] for e in events if e.kind == "text"))
        self.assertEqual([e.payload["tool_name"] for e in events if e.kind == "tool_start"],
                         ["platform.capability.context", "resource.facts.get", "resource.children.list", CREATE_TOOL, CHECK_TOOL])
        committed = next(e for e in events if e.checkpoint_committed is not None)
        self.assertEqual(committed.payload["checkpoint"]["confirmed"]["operation_reviews"], {})

    def prepare_resume(self):
        self.bind_draft()
        _, source, target, _, _ = platform_fixture()
        self.target_parent = target
        fact = {**source, "item_id": 42, "data_type": "table", "full_name": "demo.events",
                "item_fingerprint": "source-fingerprint", "scanned_depth": "table"}
        self.formal_facts[fact["locator"]] = fact
        capture_owner_facts("resource.facts.get", fact, self.checkpoint)
        self.confirm()

    async def run_resume(self, responses=None, **kwargs):
        return await self.run_factory(responses or [],
            allowed=["platform.capability.context", "resource.facts.get", "resource.children.list", CHECK_TOOL, CREATE_TOOL],
            draft_arguments=self.arguments, resuming=True, **kwargs)

    async def test_resume_changed_definition_facts_or_report_requires_new_confirmation(self):
        for changed in ("definition", "facts", "report"):
            with self.subTest(changed=changed):
                self.setUp()
                self.prepare_resume()
                if changed == "definition": self.definition = {**self.definition, "revision": self.definition["revision"] + 1}
                if changed == "facts": next(iter(self.formal_facts.values()))["fields"][0]["nullable"] = True
                if changed == "report": self.check_result = {**self.report, "not_checked": ["changed"]}
                events, client = await self.run_resume()
                self.assertEqual(len(client.calls), 0)
                self.assertEqual(self.writes(), [])
                interaction = next(e.payload for e in events if e.kind == "interaction_required")
                self.assertIn(json.dumps(self.arguments, ensure_ascii=False, sort_keys=True, separators=(",", ":")), interaction["prompt"])
                checkpoint = [e.payload["checkpoint"] for e in events if e.kind == "checkpoint"][-1]
                self.assertEqual(checkpoint["confirmed"]["operation_reviews"], {})
                self.checkpoint = checkpoint
                confirm_selection(self.checkpoint, interaction["candidates"][0])
                events, client = await self.run_resume()
                self.assertEqual(len(client.calls), 0)
                self.assertEqual(len(self.writes()), 1)
                self.assertFalse(any(e.kind in {"interaction_required", "run_failed"} for e in events))

    async def test_resume_dependency_denial_and_unsupported_stop_without_model_or_write(self):
        for failure in ("context", "facts", "check", "unsupported", "missing_fields"):
            with self.subTest(failure=failure):
                self.setUp()
                self.prepare_resume()
                if failure == "context": self.context_status = 403
                if failure == "facts": self.facts_status = 403
                if failure == "check": self.check_status = 403
                if failure == "unsupported": self.check_result = {**self.report, "status": "unsupported"}
                if failure == "missing_fields": next(iter(self.formal_facts.values())).pop("fields")
                events, client = await self.run_resume()
                self.assertEqual(len(client.calls), 0)
                self.assertEqual(self.writes(), [])
                self.assertTrue(any(e.kind == "run_failed" for e in events))
                self.assertFalse(any(e.kind == "interaction_required" for e in events))

    async def test_resume_commit_failure_and_uncertain_write_never_retry(self):
        self.prepare_resume()
        with self.assertRaisesRegex(RuntimeError, "fixture_commit_failed"):
            await self.run_resume(commit=False)
        self.assertEqual(self.writes(), [])
        self.create_status = 503
        events, client = await self.run_resume()
        self.assertEqual(len(client.calls), 0)
        self.assertEqual(len(self.writes()), 1)
        self.assertTrue(any(e.kind == "run_failed" for e in events))
        self.assertFalse(any(e.kind == "text" for e in events))
        checkpoint = [e.payload["checkpoint"] for e in events if e.kind == "checkpoint"][-1]
        self.assertEqual(checkpoint["confirmed"]["operation_reviews"], {})

    async def test_resume_clock_change_is_not_a_new_review_and_completion_is_localized(self):
        self.prepare_resume()
        self.check_result = {**self.report, "observed_at": "2026-10-29T00:00:00Z", "binding_hash": "f" * 64}
        events, client = await self.run_resume(accept_language="en-US")
        self.assertEqual(len(client.calls), 0)
        self.assertEqual(len(self.writes()), 1)
        self.assertIn("Task created: ID 101", next(e.payload["delta"] for e in events if e.kind == "text"))

    async def test_cancelled_review_does_not_trigger_runtime_creation(self):
        self.prepare_resume()
        identity = self.checkpoint["observed"]["operation_reviews"][CREATE_TOOL]
        confirm_selection(self.checkpoint, {"value": "cancel", "candidate": {"operation_review": identity}})
        events, _ = await self.run_resume([Message(role="assistant", content="已取消")])
        self.assertEqual(self.writes(), [])
        self.assertFalse(any(e.kind == "tool_start" and e.payload["tool_name"] in {CREATE_TOOL, CHECK_TOOL, "resource.facts.get"} for e in events))

    async def test_cancel_during_resume_check_never_creates(self):
        from langgraph.errors import NodeCancelledError
        self.prepare_resume()
        self.cancel_check = True
        task = asyncio.create_task(self.run_resume())
        try:
            await asyncio.wait_for(self.check_started.wait(), timeout=5)
        finally:
            task.cancel()
            with self.assertRaises((asyncio.CancelledError, NodeCancelledError)):
                await task
        self.assertEqual(self.writes(), [])

    async def test_bound_review_uses_exact_candidate_and_current_report(self):
        reference = self.bind_draft()
        call = Message(role="assistant", tool_calls=[_call("request_clarification", {
            "prompt": "review original draft", "reason": REVIEW_REASON,
            "options": [{"label": "confirm", "value": "confirm"}, {"label": "cancel", "value": "cancel"}],
            "operation_review": {"tool": CREATE_TOOL},
        })])
        events, client = await self.run_factory([call, Message(role="assistant", content="unexpected closing text")], draft_arguments=self.arguments)
        self.assertEqual(len(client.calls), 1)
        check = next(r for r in self.requests if r[1] == "/api/v1/transfer/task-config-checks")
        self.assertEqual(check[2]["json"]["name"], self.arguments["name"])
        interaction = next(e.payload for e in events if e.kind == "interaction_required")
        self.assertIn(self.arguments["name"], interaction["prompt"])
        self.assertEqual(interaction["candidates"][0]["label"], "知悉未核验项，仅创建任务")
        saved = [e.payload["checkpoint"] for e in events if e.kind == "checkpoint"][-1]
        self.assertEqual(saved["draft_ref"], reference)
        self.assertNotIn("draft_arguments", saved)
        self.assertEqual(self.writes(), [])

    async def test_bound_creation_ignores_model_parameter_drift_before_schema_and_adapter(self):
        self.bind_draft()
        self.confirm()
        events, _ = await self.run_factory([
            Message(role="assistant", tool_calls=[_call(CREATE_TOOL, {"name": "invented", "batch_size": 500, "enabled": True})]),
            Message(role="assistant", content="任务已创建，未运行"),
        ], draft_arguments=self.arguments)
        self.assertEqual(len(self.writes()), 1)
        write = self.writes()[0][2]["json"]
        check = next(r for r in self.requests if r[1] == "/api/v1/transfer/task-config-checks")[2]["json"]
        self.assertEqual(write, check)
        self.assertEqual(write["name"], self.arguments["name"])
        self.assertEqual(write["config"], self.arguments["config"])
        self.assertFalse(write["enabled"])
        self.assertNotEqual(write.get("batch_size"), 500)
        create = next(e.payload for e in events if e.kind == "tool_start" and e.payload["tool_name"] == CREATE_TOOL)
        self.assertEqual(create["args"], self.arguments)
        self.assertTrue(any(e.checkpoint_committed is not None for e in events))

    async def test_bound_arguments_digest_mismatch_fails_before_any_owner_request(self):
        self.bind_draft()
        changed = {**self.arguments, "name": "changed"}
        with self.assertRaisesRegex(ValueError, "draft_snapshot_invalid"):
            await self.run_factory([], draft_arguments=changed)
        self.assertEqual(self.requests, [])

    async def test_bound_creation_without_current_confirmation_never_writes(self):
        self.bind_draft()
        events, _ = await self.run_factory([
            Message(role="assistant", tool_calls=[_call(CREATE_TOOL, {})]),
            Message(role="assistant", content="需要复核"),
        ], draft_arguments=self.arguments)
        self.assertEqual(self.writes(), [])
        self.assertTrue(any(e.kind == "tool_result" and e.payload.get("error_code") == "platform_condition_unsatisfied" for e in events))

    async def test_bound_reference_is_not_a_substitute_for_current_resource_facts(self):
        self.bind_draft()
        self.confirm()
        self.checkpoint["observed"]["resources"].clear()
        events, _ = await self.run_factory([
            Message(role="assistant", tool_calls=[_call(CREATE_TOOL, {})]),
            Message(role="assistant", content="缺少当前资源事实"),
        ], draft_arguments=self.arguments)
        self.assertEqual(self.writes(), [])
        self.assertTrue(any(e.kind == "tool_result" and e.payload.get("error_code") == "platform_condition_unsatisfied" for e in events))

    async def test_bound_review_rejects_second_arguments_and_checks_current_support(self):
        self.bind_draft()
        call = Message(role="assistant", tool_calls=[_call("request_clarification", {
            "prompt": "review", "reason": REVIEW_REASON,
            "options": [{"label": "confirm", "value": "confirm"}, {"label": "cancel", "value": "cancel"}],
            "operation_review": {"tool": CREATE_TOOL},
        })])
        self.check_result = precheck_fixture(self.arguments, self.checkpoint, "unsupported")
        events, _ = await self.run_factory([self.review_call(), call, Message(role="assistant", content="当前配置不支持")], draft_arguments=self.arguments)
        self.assertEqual(len([r for r in self.requests if r[1] == "/api/v1/transfer/task-config-checks"]), 1)
        self.assertEqual(self.writes(), [])
        self.assertFalse(any(e.kind == "interaction_required" for e in events))
        rejection = next(e.payload for e in events if e.kind == "tool_result" and e.payload.get("error_code") == "platform_condition_unsatisfied")
        self.assertEqual(json.loads(rejection["content"])["config_check"]["status"], "unsupported")

    async def test_bound_creation_permission_failure_stops_without_reusing_old_check(self):
        self.bind_draft()
        self.confirm()
        self.check_status = 403
        self.check_result = {"code": "forbidden", "message": "denied"}
        events, client = await self.run_factory([
            Message(role="assistant", tool_calls=[_call(CREATE_TOOL, {})]),
            Message(role="assistant", content="must not continue"),
        ], draft_arguments=self.arguments)
        self.assertEqual(len(client.calls), 1)
        self.assertEqual(self.writes(), [])
        self.assertTrue(any(e.kind == "run_failed" for e in events))
        saved = [e.payload["checkpoint"] for e in events if e.kind == "checkpoint"][-1]
        self.assertEqual(saved["observed"]["config_checks"], {})

    async def test_bound_explicit_check_cannot_rebuild_or_complete_creation_goal(self):
        self.bind_draft()
        call = Message(role="assistant", tool_calls=[_call("request_clarification", {
            "prompt": "review", "reason": REVIEW_REASON,
            "options": [{"label": "confirm", "value": "confirm"}, {"label": "cancel", "value": "cancel"}],
            "operation_review": {"tool": CREATE_TOOL},
        })])
        events, client = await self.run_factory([
            Message(role="assistant", tool_calls=[_call(CHECK_TOOL, {**self.arguments, "name": "invented"})]), call,
        ], draft_arguments=self.arguments)
        self.assertEqual(len(client.calls), 2)
        checks = [r for r in self.requests if r[1] == "/api/v1/transfer/task-config-checks"]
        self.assertEqual(len(checks), 1)
        self.assertEqual(checks[0][2]["json"]["name"], self.arguments["name"])
        self.assertTrue(any(e.kind == "interaction_required" for e in events))
        self.assertEqual(self.writes(), [])

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
                self.assertEqual(len(client.calls), 1, "valid explicit check must not request model closing text")
                prompt = "\n".join(m.content for m in client.calls[0]["messages"] if m.role == "system")
                self.assertIn("不再调用模型收尾", prompt)
                self.assertNotIn("默认只给关键检查解释", prompt)
                checks = [r for r in self.requests if r[1] == "/api/v1/transfer/task-config-checks"]
                self.assertEqual(len(checks), 1)
                source = checks[0][2]["json"]["config"]["source"]
                project = json.loads(source["query"]["statement"])["pipeline"][0]["$project"]
                self.assertEqual(project["members"], 1)
                feedback = json.loads(next(e.payload["content"] for e in events if e.kind == "tool_result" and e.payload["tool_name"] == CHECK_TOOL))
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
                texts = [e.payload["delta"] for e in events if e.kind == "text"]
                self.assertEqual(len(texts), 1)
                self.assertIn(status, texts[0])
                self.assertIn("本次检查不创建任务或目标表", texts[0])
                self.assertNotIn("正式检查已返回，仍只展示草稿", texts[0])
                self.assertLess(next(i for i, e in enumerate(events) if e.kind == "tool_result" and e.payload["tool_name"] == CHECK_TOOL), next(i for i, e in enumerate(events) if e.kind == "text"))
                phase = phase_from_events("draft_check", "fixture-draft-run", "completed", events,
                    owner_effects={"tasks_created": 0, "approvals_created": 0, "executions_created": 0, "business_rows_written": 0}, persisted_state={})
                evaluate_trace(load_scenario(EVAL_ROOT / "platform-transfer-draft-check"), {"skill": "transfer-generation", "phases": [phase]})

    async def test_explicit_check_preempts_all_sibling_calls_in_either_order(self):
        self.confirm()
        for calls in (
            [_call(CREATE_TOOL, self.arguments), _call(CHECK_TOOL, self.arguments, "check")],
            [_call(CHECK_TOOL, self.arguments, "check"), _call(CREATE_TOOL, self.arguments)],
            [self.review_call().tool_calls[0], _call(CHECK_TOOL, self.arguments, "check"), _call(CHECK_TOOL, self.arguments, "second-check")],
        ):
            self.requests.clear()
            events, client = await self.run_factory([
                Message(role="assistant", tool_calls=calls), Message(role="assistant", content="unexpected model closing text"),
            ])
            self.assertEqual(len(client.calls), 1)
            self.assertEqual(self.writes(), [])
            self.assertEqual(sum(r[1] == "/api/v1/transfer/task-config-checks" for r in self.requests), 1)
            self.assertFalse(any(e.kind in {"interaction_required", "run_failed"} for e in events))
            self.assertEqual([e.payload["tool_name"] for e in events if e.kind == "tool_start"], ["platform.capability.context", CHECK_TOOL])

    async def test_explicit_input_error_can_be_corrected_without_sibling_write(self):
        self.confirm()
        events, client = await self.run_factory([
            Message(role="assistant", tool_calls=[_call(CHECK_TOOL, {}, "invalid-check"), _call(CREATE_TOOL, self.arguments)]),
            Message(role="assistant", tool_calls=[_call(CHECK_TOOL, self.arguments, "corrected-check")]),
            Message(role="assistant", content="unexpected model closing text"),
        ])
        self.assertEqual(len(client.calls), 2)
        self.assertEqual(self.writes(), [])
        self.assertEqual(sum(e.kind == "text" for e in events), 1)
        self.assertEqual(sum(e.kind == "presentation" for e in events), 1)
        self.assertFalse(any(e.kind == "run_failed" for e in events))

    async def test_explicit_failure_never_emits_completion(self):
        for http_status, report in ((403, {"error": "forbidden"}), (200, {"status": "supported"})):
            self.requests.clear()
            self.check_status, self.check_result = http_status, report
            events, client = await self.run_factory([
                Message(role="assistant", tool_calls=[_call(CHECK_TOOL, self.arguments)]),
                Message(role="assistant", content="unexpected model closing text"),
            ])
            self.assertEqual(len(client.calls), 1)
            self.assertTrue(any(e.kind == "run_failed" for e in events))
            self.assertFalse(any(e.kind in {"text", "presentation"} for e in events))
            self.assertEqual(self.writes(), [])

    async def test_explicit_check_completion_uses_user_language_and_exact_status(self):
        self.checkpoint["reply_detail"] = "concise"
        for status, expected in (("supported", "does not guarantee execution success"), ("unknown", "not passed"), ("unsupported", "Unsupported definitions")):
            self.check_result = {**self.report, "status": status}
            events, client = await self.run_factory([
                Message(role="assistant", tool_calls=[_call(CHECK_TOOL, self.arguments)]),
                Message(role="assistant", content="unexpected model closing text"),
            ], accept_language="en-US")
            self.assertEqual(len(client.calls), 1)
            text = next(e.payload["delta"] for e in events if e.kind == "text")
            self.assertIn(expected, text)
            self.assertIn(status, text)
            self.assertIn("This check does not create a task or target table", text)

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
        for tool in (CREATE_TOOL, CHECK_TOOL):
            self.check_started.clear()
            run = asyncio.create_task(self.run_factory([Message(role="assistant", tool_calls=[_call(tool, self.arguments)])]))
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
