"""Exact historical arguments, real SQL scope predicates and readonly routing.

In-memory SQL is T1, not a PostgreSQL or live-model acceptance claim.
"""
import asyncio
import copy
from contextlib import aclosing
import json
import unittest
import uuid
from datetime import datetime, timezone
from unittest.mock import AsyncMock, patch

from langchain_core.messages import AIMessage
from langchain_core.tools import StructuredTool
from sqlalchemy import create_engine, JSON, MetaData
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import Session as DatabaseSession

from agents.main_agent import _route_node, stream_agent_response
from agents.checkpoint import new_checkpoint, normalize_checkpoint
from agents.events import text_event
from graph.state import PlatformRouteDecision, RouteDecision, DraftExplanationScope
from models.run import AgentRun
from models.run_step import AgentRunStep
from models.session import Session
from services.drafts import draft_snapshot, draft_presentation, list_session_drafts, read_session_draft, DraftReferenceError
from services.runs import resume_agent_run
from agents.platform_conditions import prepare_review
from models.interaction import Interaction
from tests.test_platform_conditions import platform_fixture, precheck_fixture
from tests.test_platform_routing import ScriptedRouter, catalog_fixture, registry_fixture


def checked_step(status="unknown"):
    _, _, _, arguments, checkpoint = platform_fixture()
    arguments["config"]["transforms"][0]["fields"].append({"source": "members", "target": "members", "target_type": "json"})
    precheck_fixture(arguments, checkpoint, status)
    return AgentRunStep(id=uuid.uuid4(), status="completed", tool_name="transfer.task.config.check",
        input_data=arguments, facts={"config_checks": [checkpoint["observed"]["config_checks"]["transfer.task.create"]]})


class DraftSnapshotTests(unittest.TestCase):
    def test_exact_json_target_and_full_parameters_survive_without_identity(self):
        for status in ("supported", "unsupported", "unknown"):
            step = checked_step(status)
            snapshot = draft_snapshot(step)
            self.assertEqual(snapshot["arguments"], step.input_data)
            self.assertEqual(snapshot["report"]["status"], status)
            self.assertNotIn("fixture-user", json.dumps(snapshot))
            view = draft_presentation(snapshot)
            self.assertEqual(json.loads(view["parameters"]), step.input_data)
            self.assertEqual(view["mappings"][-1]["target_type"], "json")
            step.input_data["name"] = "edited"
            self.assertNotEqual(snapshot["arguments"]["name"], step.input_data["name"])

    def test_unfinished_failed_tampered_invalid_and_oversize_snapshots_are_rejected(self):
        for change in ("unfinished", "failed", "input", "report", "facts", "credentials", "large"):
            step = checked_step()
            if change == "unfinished": step.status = "running"
            if change == "failed": step.status = "failed"
            if change == "input": step.input_data["config"]["target"]["name"] = "different"
            if change == "report": step.facts["config_checks"][0]["report"]["status"] = "passed"
            if change == "facts": step.facts = {}
            if change == "credentials": step.input_data["access_token"] = "private"
            if change == "large":
                # Valid field names can still exceed the aggregate snapshot budget.
                step.input_data["config"]["transforms"][0]["fields"][0]["source"] = "x" * (128 * 1024)
                from agents.platform_conditions import config_arguments_digest
                step.facts["config_checks"][0]["arguments_digest"] = config_arguments_digest(step.input_data)
            with self.subTest(change=change), self.assertRaises(DraftReferenceError): draft_snapshot(step)

    def test_presentation_checks_utf8_and_aggregate_escaped_projection_limits(self):
        from agents.platform_conditions import config_arguments_digest
        step = checked_step()
        step.input_data["config"]["transforms"][0]["fields"][0]["source"] = "\n" * 55000
        step.facts["config_checks"][0]["arguments_digest"] = config_arguments_digest(step.input_data)
        snapshot = draft_snapshot(step)  # The compact source is within 128 KiB.
        with self.assertRaisesRegex(DraftReferenceError, "too_large"):
            draft_presentation(snapshot)  # Parameter string plus mappings exceeds 256 KiB.


class DraftScopeTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.engine = create_engine("sqlite:///:memory:")
        metadata = MetaData()
        for model in (Session, AgentRun, AgentRunStep):
            table = model.__table__.to_metadata(metadata)
            for column in table.columns:
                if isinstance(column.type, JSONB): column.type = JSON()
        with self.engine.begin() as connection:
            connection.exec_driver_sql("ATTACH DATABASE ':memory:' AS agent")
            metadata.create_all(connection)
        self.database = DatabaseSession(self.engine)
        from tests.test_session_tenant_isolation import AsyncDatabaseAdapter
        self.db = AsyncDatabaseAdapter(self.database)
        self.sessions = []
        self.refs = []
        for user_id, tenant_id in ((1, 1), (2, 1), (1, 2), (1, 1)):
            session = Session(user_id=user_id, tenant_id=tenant_id, title="scope fixture")
            self.database.add(session)
            self.database.flush()
            run = AgentRun(id=uuid.uuid4(), session_id=session.id, user_id=user_id, tenant_id=tenant_id,
                           initial_protocol_run_id=str(uuid.uuid4()), status="completed", checkpoint={})
            self.database.add(run)
            step = checked_step()
            step.agent_run_id, step.sequence, step.step_type = run.id, 1, "tool_call"
            step.protocol_invocation_id, step.completed_at = "fixture", datetime.now(timezone.utc)
            self.database.add(step)
            self.sessions.append(session.id)
            self.refs.append(draft_snapshot(step)["reference"])
        self.database.commit()

    def tearDown(self):
        self.database.close()
        self.engine.dispose()

    def review_pipeline(self):
        _, _, _, arguments, checkpoint = platform_fixture()
        identity = prepare_review(arguments, checkpoint)
        run = self.database.query(AgentRun).filter_by(session_id=self.sessions[0]).one()
        run.status, run.checkpoint = "waiting", checkpoint
        review = AgentRunStep(id=uuid.uuid4(), agent_run_id=run.id, sequence=2, step_type="tool_call",
            protocol_invocation_id="review-invocation", tool_call_id="this-review", status="completed",
            tool_name="request_clarification", input_data={"reason": "transfer_create_review",
                "operation_review": {"tool": "transfer.task.create", "arguments": copy.deepcopy(arguments)}})
        check = AgentRunStep(id=uuid.uuid4(), agent_run_id=run.id, sequence=3, step_type="tool_call",
            protocol_invocation_id="review-invocation", tool_call_id="automatic-check", status="completed",
            tool_name="transfer.task.config.check", input_data=copy.deepcopy(arguments),
            facts={"config_checks": [checkpoint["observed"]["config_checks"]["transfer.task.create"]]})
        self.database.add_all([review, check])
        self.database.commit()
        self.db.flush = AsyncMock(side_effect=self.database.flush)
        answer = {"value": identity["fingerprint"], "candidate": {"operation_review": identity}}
        interaction = Interaction(agent_run_id=run.id, tool_call_id=review.tool_call_id, answer=answer)
        return run, review, check, interaction

    async def test_ordinary_confirmation_binds_this_review_check_and_restores_exact_parameters(self):
        run, _, check, interaction = self.review_pipeline()
        resumed = await resume_agent_run(self.db, interactions=[interaction], session_id=self.sessions[0], user_id=1, tenant_id=1)
        self.assertEqual(resumed.checkpoint["draft_ref"], draft_snapshot(check)["reference"])
        snapshot = await read_session_draft(self.db, resumed.checkpoint["draft_ref"], session_id=self.sessions[0], user_id=1, tenant_id=1)
        self.assertEqual(snapshot["arguments"], check.input_data)
        self.assertNotIn("arguments", resumed.checkpoint)
        self.assertEqual(resumed.id, run.id)
        self.assertEqual(resumed.status, "running")

    async def test_confirmation_does_not_fall_back_to_another_invocation_check(self):
        run, _, check, interaction = self.review_pipeline()
        check.protocol_invocation_id = "other-invocation"
        self.database.commit()
        with self.assertRaises(DraftReferenceError):
            await resume_agent_run(self.db, interactions=[interaction], session_id=self.sessions[0], user_id=1, tenant_id=1)
        self.assertIsNone(run.checkpoint["draft_ref"])
        self.assertEqual(run.status, "waiting")

    async def test_confirmation_rejects_ambiguous_check_and_changed_review_arguments(self):
        run, review, check, interaction = self.review_pipeline()
        duplicate = AgentRunStep(id=uuid.uuid4(), agent_run_id=run.id, sequence=4, step_type="tool_call",
            protocol_invocation_id=check.protocol_invocation_id, status="completed", tool_name=check.tool_name,
            input_data=copy.deepcopy(check.input_data), facts=copy.deepcopy(check.facts))
        self.database.add(duplicate)
        self.database.commit()
        with self.assertRaises(DraftReferenceError):
            await resume_agent_run(self.db, interactions=[interaction], session_id=self.sessions[0], user_id=1, tenant_id=1)
        self.database.delete(duplicate)
        review.input_data = {**review.input_data, "operation_review": {
            "tool": "transfer.task.create", "arguments": {**check.input_data, "name": "different"}}}
        self.database.commit()
        with self.assertRaises(DraftReferenceError):
            await resume_agent_run(self.db, interactions=[interaction], session_id=self.sessions[0], user_id=1, tenant_id=1)
        self.assertEqual(run.status, "waiting")

    async def test_query_enforces_user_tenant_session_and_exact_version(self):
        scope = dict(session_id=self.sessions[0], user_id=1, tenant_id=1)
        catalog = await list_session_drafts(self.db, **scope)
        self.assertEqual([value["reference"] for value in catalog], [self.refs[0]])
        snapshot = await read_session_draft(self.db, self.refs[0], **scope)
        self.assertEqual(snapshot["arguments"]["config"]["transforms"][0]["fields"][-1]["target_type"], "json")
        for reference in [*self.refs[1:], self.refs[0][:-1] + ("0" if self.refs[0][-1] != "0" else "1"), "missing", "draft:" + "-" * 36 + ":" + "a" * 64]:
            with self.subTest(reference=reference), self.assertRaises(DraftReferenceError):
                await read_session_draft(self.db, reference, **scope)

    async def test_catalog_is_bounded_and_ignores_unminted_evidence(self):
        run = self.database.query(AgentRun).filter_by(session_id=self.sessions[0]).one()
        for sequence in range(2, 14):
            step = checked_step()
            step.agent_run_id, step.sequence, step.step_type = run.id, sequence, "tool_call"
            step.protocol_invocation_id = "fixture"
            step.completed_at = datetime.now(timezone.utc)
            self.database.add(step)
        self.database.commit()
        scope = dict(session_id=self.sessions[0], user_id=1, tenant_id=1)
        self.assertEqual(len(await list_session_drafts(self.db, **scope)), 8)
        latest = self.database.query(AgentRunStep).filter_by(agent_run_id=run.id).order_by(AgentRunStep.sequence.desc()).first()
        latest.facts = {"config_checks": None}
        self.database.commit()
        catalog = await list_session_drafts(self.db, **scope)
        self.assertEqual(len(catalog), 7)  # Bounded window; do not scan older steps to refill.
        self.assertFalse(any(str(latest.id) in item["reference"] for item in catalog))


class DraftRoutingTests(unittest.IsolatedAsyncioTestCase):
    async def test_historical_explanation_streams_before_completion_with_exact_snapshot_and_no_tools(self):
        from langchain_core.messages import AIMessageChunk
        snapshot = draft_snapshot(checked_step())
        release = asyncio.Event()
        class Model(ScriptedRouter):
            async def astream(inner, messages):
                inner.messages.append(messages)
                yield AIMessageChunk(content='第一段历史说明')
                await release.wait()
                yield AIMessageChunk(content='第二段历史说明')
        model = Model([RouteDecision(draft_action='inspect', draft_ref=snapshot['reference'], draft_explanation=DraftExplanationScope(topics=['fields'], fields=['members']))])
        reader = AsyncMock(return_value=snapshot)
        with (patch('agents.main_agent.get_llm', return_value=model),
              patch('agents.main_agent.create_agent_tools') as tools,
              patch('agents.main_agent.AgentFactory.run') as harness):
            async with aclosing(stream_agent_response([{'role':'user','content':'只解释历史草稿'}],
                    1, 1, 'token', 'history-stream', draft_catalog=[{'reference':snapshot['reference'],'name':snapshot['name']}],
                    draft_reader=reader)) as events:
                received = []
                while True:
                    event = await asyncio.wait_for(anext(events), 2)
                    received.append(event)
                    if event.kind == 'text' and event.payload['delta'] == '第一段历史说明':
                        break
                self.assertFalse(release.is_set())
                presentation = next(e.payload for e in received if e.kind=='presentation' and e.payload['kind']=='transfer_draft_snapshot')
                self.assertEqual(json.loads(presentation['parameters']), snapshot['arguments'])
                self.assertEqual([e.payload['kind'] for e in received if e.kind=='presentation'],
                    ['transfer_draft_snapshot','transfer_config_check'])
                release.set()
                rest = [event async for event in events]
            self.assertEqual([e.payload['delta'] for e in rest if e.kind=='text'], ['第二段历史说明'])
            reader.assert_awaited_once_with(snapshot['reference'])
            tools.assert_not_called()
            harness.assert_not_called()

    async def test_invalid_draft_route_stops_before_tools_and_logs_only_closed_metadata(self):
        request = 'private-user-content'
        reference = 'private-draft-reference'
        for decision in (RouteDecision(draft_action='inspect', draft_ref=reference, needs_platform_semantics=True),
                         RouteDecision(draft_ref=reference)):
            with (self.subTest(decision=decision),
                  patch('agents.main_agent._get_skill_registry', return_value=registry_fixture()),
                  patch('agents.main_agent.get_llm', return_value=ScriptedRouter([decision])),
                  patch('agents.main_agent.create_agent_tools') as tools,
                  patch('agents.main_agent.AgentFactory.run') as harness,
                  patch('agents.main_agent.logger.info') as log):
                reader = AsyncMock()
                with self.assertRaisesRegex(ValueError, 'invalid_draft_route'):
                    _ = [event async for event in stream_agent_response([{'role':'user','content':request}],
                        1, 1, 'private-token', 'invalid-route', draft_catalog=[{'reference':reference,'name':'draft'}],
                        draft_reader=reader)]
                tools.assert_not_called()
                harness.assert_not_called()
                reader.assert_not_awaited()
                records = [call.args for call in log.call_args_list if call.args[0].startswith('[ROUTE]')]
                self.assertEqual(len(records), 1)
                self.assertEqual(records[0][1:], (decision.draft_action, bool(decision.skill),
                    decision.needs_platform_semantics, bool(decision.draft_ref)))
                for secret in (request, reference, 'private-token'):
                    self.assertNotIn(secret, str(records))

    async def test_creation_restores_only_parameters_before_current_platform_selection(self):
        snapshot = draft_snapshot(checked_step())
        reference = snapshot["reference"]
        reader = AsyncMock(return_value=snapshot)
        model = ScriptedRouter([RouteDecision(draft_action="create", draft_ref=reference, needs_platform_semantics=True),
                                PlatformRouteDecision(capability="transfer.task.create")])
        trace = []

        async def catalog_call():
            trace.append("catalog")
            return json.dumps(catalog_fixture())

        tool = StructuredTool.from_function(name="platform__capabilities__list", description="catalog", coroutine=catalog_call,
                                           metadata={"addp_tool_name": "platform.capabilities.list"})

        async def domain_run(**kwargs):
            trace.append("harness")
            context = kwargs["task_context"]
            self.assertEqual(context["draft_arguments"], snapshot["arguments"])
            self.assertEqual(context["checkpoint"]["draft_ref"], reference)
            self.assertEqual(context["checkpoint"]["observed"]["resources"], {})
            self.assertEqual(context["checkpoint"]["observed"]["config_checks"], {})
            self.assertEqual(context["checkpoint"]["confirmed"]["operation_reviews"], {})
            self.assertNotIn("report", context)
            yield text_event("等待当前复核")

        with (patch("agents.main_agent._get_skill_registry", return_value=registry_fixture()),
              patch("agents.main_agent.get_llm", return_value=model),
              patch("agents.main_agent.create_agent_tools", return_value=[tool]),
              patch("agents.main_agent.AgentFactory.run", side_effect=domain_run)):
            events = [e async for e in stream_agent_response([{"role": "user", "content": "按原稿创建，不运行"}], 1, 1, "token", "create-original",
                draft_catalog=[{"reference": reference, "name": snapshot["name"]}], draft_reader=reader)]
        reader.assert_awaited_once_with(reference)
        self.assertEqual(trace, ["catalog", "harness"])
        self.assertEqual([e.payload["kind"] for e in events if e.kind == "presentation"], ["transfer_draft_snapshot"])

    async def test_resume_rereads_scoped_reference_without_parameters_in_checkpoint(self):
        snapshot = draft_snapshot(checked_step())
        checkpoint = new_checkpoint()
        checkpoint["draft_ref"] = snapshot["reference"]
        reader = AsyncMock(return_value=snapshot)

        async def domain_run(**kwargs):
            context = kwargs["task_context"]
            self.assertEqual(context["draft_arguments"], snapshot["arguments"])
            self.assertEqual(context["checkpoint"], checkpoint)
            yield text_event("恢复当前复核")

        with (patch("agents.main_agent._get_skill_registry", return_value=registry_fixture()),
              patch("agents.main_agent.get_llm", return_value=ScriptedRouter([])),
              patch("agents.main_agent.create_agent_tools") as tools,
              patch("agents.main_agent.AgentFactory.run", side_effect=domain_run)):
            events = [e async for e in stream_agent_response([{"role": "user", "content": "确认"}], 1, 1, "token", "resume",
                forced_skill_name="transfer-generation", checkpoint=checkpoint, draft_reader=reader)]
        reader.assert_awaited_once_with(snapshot["reference"])
        tools.assert_not_called()  # Fresh semantics are read by the real Harness, not mocked here.
        self.assertTrue(any(e.kind == "run_state" for e in events))

    async def test_resume_unavailable_or_tampered_reference_stops_before_harness(self):
        snapshot = draft_snapshot(checked_step())
        checkpoint = new_checkpoint()
        checkpoint["draft_ref"] = snapshot["reference"]
        changed = copy.deepcopy(snapshot)
        changed["arguments"]["name"] = "unrequested-change"
        for reader in (AsyncMock(return_value=changed), AsyncMock(side_effect=DraftReferenceError("draft_reference_unavailable"))):
            with (
                self.subTest(reader=reader),
                patch("agents.main_agent._get_skill_registry", return_value=registry_fixture()),
                patch("agents.main_agent.get_llm", return_value=ScriptedRouter([])),
                patch("agents.main_agent.create_agent_tools") as tools,
                patch("agents.main_agent.AgentFactory.run") as harness,
            ):
                events = [e async for e in stream_agent_response([{"role": "user", "content": "确认"}], 1, 1, "token", "resume-invalid",
                    forced_skill_name="transfer-generation", checkpoint=checkpoint, draft_reader=reader)]
            self.assertTrue(any(e.kind == "run_failed" for e in events))
            tools.assert_not_called()
            harness.assert_not_called()

    async def test_ambiguous_creation_neither_restores_nor_reads_platform(self):
        reader = AsyncMock()
        with (patch("agents.main_agent.get_llm", return_value=ScriptedRouter([RouteDecision(draft_action="create", needs_platform_semantics=True)])),
              patch("agents.main_agent.create_agent_tools") as tools):
            events = [e async for e in stream_agent_response([{"role": "user", "content": "按原稿创建"}], 1, 1, "token", "ambiguous-create",
                draft_catalog=[{"reference": "a", "name": "one"}, {"reference": "b", "name": "two"}], draft_reader=reader)]
        reader.assert_not_called()
        tools.assert_not_called()
        self.assertEqual([e.kind for e in events], ["text"])

    def test_checkpoint_preserves_reference_but_rejects_wrong_type(self):
        checkpoint = new_checkpoint()
        checkpoint["draft_ref"] = draft_snapshot(checked_step())["reference"]
        checkpoint["draft_arguments"] = {"untrusted": "not persisted"}
        restored = normalize_checkpoint(checkpoint)
        self.assertEqual(restored["draft_ref"], checkpoint["draft_ref"])
        self.assertNotIn("draft_arguments", restored)
        checkpoint["draft_ref"] = {"not": "a reference"}
        with self.assertRaisesRegex(ValueError, "invalid_draft_reference"):
            normalize_checkpoint(checkpoint)

    async def test_mixed_and_unobserved_routes_are_rejected(self):
        for decision in (RouteDecision(draft_action="inspect", skill="transfer-generation"),
                         RouteDecision(draft_action="inspect", needs_platform_semantics=True),
                         RouteDecision(draft_action="inspect", draft_ref="not-observed"),
                         RouteDecision(draft_action="create", draft_ref="known-ref"),
                         RouteDecision(draft_ref="known-ref")):
            with self.subTest(decision=decision), self.assertRaises(ValueError):
                await _route_node({"messages": [{"role": "user", "content": "解释草稿"}],
                                   "draft_catalog": [{"reference": "known-ref", "name": "one"}]}, ScriptedRouter([decision]))

    async def test_inspection_receives_exact_snapshot_and_never_assembles_business_tools(self):
        snapshot = draft_snapshot(checked_step())
        catalog = [{"reference": snapshot["reference"], "name": snapshot["name"], "observed_at": snapshot["report"]["observed_at"]}]
        model = ScriptedRouter([RouteDecision(draft_action="inspect", draft_ref=snapshot["reference"], draft_explanation=DraftExplanationScope(topics=["fields"], fields=["members"])), AIMessage(content="按原配置解释，未执行。")])
        reader = AsyncMock(return_value=snapshot)
        with (patch("agents.main_agent.get_llm", return_value=model),
              patch("agents.main_agent.create_agent_tools") as tools,
              patch("agents.main_agent.AgentFactory.run") as harness):
            events = [e async for e in stream_agent_response([{"role": "user", "content": "解释刚才的字段映射，不修改"}],
                1, 1, "private-token", "inspect-run", draft_catalog=catalog, draft_reader=reader)]
        tools.assert_not_called()
        harness.assert_not_called()
        reader.assert_awaited_once_with(snapshot["reference"])
        context = json.loads(model.messages[1][-1].content)
        self.assertEqual(set(context), {"mappings"})
        self.assertEqual([field["source"] for field in context["mappings"]], ["members"])
        self.assertNotIn("snapshot", context)
        self.assertNotIn("request", context)
        views = [e.payload for e in events if e.kind == "presentation"]
        self.assertEqual(views[0]["mappings"][-1]["target_type"], "json")
        self.assertEqual(views[1]["report"], snapshot["report"])
        self.assertEqual([e.payload["tool_name"] for e in events if e.kind == "tool_start"], ["draft.inspect"])

    async def test_ambiguous_draft_does_not_pick_first_or_call_reader(self):
        model = ScriptedRouter([RouteDecision(draft_action="inspect", draft_explanation=DraftExplanationScope(topics=["status"], fields=[]))])
        reader = AsyncMock()
        with patch("agents.main_agent.get_llm", return_value=model):
            events = [e async for e in stream_agent_response([{"role": "user", "content": "解释草稿"}], 1, 1, "token", "ambiguous",
                draft_catalog=[{"reference": "a", "name": "one"}, {"reference": "b", "name": "two"}], draft_reader=reader)]
        reader.assert_not_called()
        self.assertFalse(any(e.kind == "presentation" for e in events))

    async def test_unavailable_selected_snapshot_fails_closed_without_model_explanation(self):
        model = ScriptedRouter([RouteDecision(draft_action="inspect", draft_ref="known-ref", draft_explanation=DraftExplanationScope(topics=["status"], fields=[]))])
        with patch("agents.main_agent.get_llm", return_value=model):
            events = [e async for e in stream_agent_response([{"role": "user", "content": "解释"}], 1, 1, "token", "missing",
                draft_catalog=[{"reference": "known-ref", "name": "one"}],
                draft_reader=AsyncMock(side_effect=DraftReferenceError("draft_reference_unavailable")))]
        self.assertEqual(len(model.messages), 1)
        self.assertTrue(any(e.kind == "run_failed" for e in events))
        self.assertFalse(any(e.kind == "presentation" for e in events))


class HistoricalExplanationBoundaryTests(unittest.IsolatedAsyncioTestCase):
    async def test_closing_mixed_explanation_preserves_fixed_text_and_closes_model_stream(self):
        from langchain_core.messages import AIMessageChunk
        snapshot = draft_snapshot(checked_step())
        closed = asyncio.Event()

        class Model(ScriptedRouter):
            async def astream(inner, messages):
                try:
                    yield AIMessageChunk(content='未完成的字段说明')
                    await asyncio.Event().wait()
                finally:
                    closed.set()

        model = Model([RouteDecision(draft_action='inspect', draft_ref=snapshot['reference'],
            draft_explanation=DraftExplanationScope(topics=['status', 'boundary', 'fields'], fields=['members']))])
        with patch('agents.main_agent.get_llm', return_value=model):
            async with aclosing(stream_agent_response([{'role':'user','content':'解释状态、边界及 members 映射'}],
                    1, 1, 'token', 'mixed-close', draft_catalog=[{'reference':snapshot['reference'],'name':snapshot['name']}],
                    draft_reader=AsyncMock(return_value=snapshot))) as stream:
                received = []
                while True:
                    event = await asyncio.wait_for(anext(stream), 2)
                    received.append(event)
                    if event.kind == 'text' and event.payload['delta'] == '未完成的字段说明':
                        break
            self.assertTrue(closed.is_set())
        text = ''.join(e.payload['delta'] for e in received if e.kind == 'text')
        self.assertIn('不是权限或批准边界', text)
        self.assertIn('未完成的字段说明', text)
        self.assertEqual([e.payload['kind'] for e in received if e.kind == 'presentation'],
                         ['transfer_draft_snapshot', 'transfer_config_check'])

    async def test_key_topics_use_runtime_only_in_both_languages_for_all_report_states(self):
        from i18n import draft_explanation_facts
        scope = DraftExplanationScope(topics=['status', 'policy', 'boundary', 'authorization', 'diagnostics'], fields=[])
        for status in ('supported', 'unknown', 'unsupported'):
            for language in ('zh-cn', 'en'):
                snapshot = draft_snapshot(checked_step(status))
                model = ScriptedRouter([RouteDecision(draft_action='inspect', draft_ref=snapshot['reference'], draft_explanation=scope)])
                with (patch('agents.main_agent.get_llm', return_value=model),
                      patch('agents.main_agent.create_agent_tools') as tools,
                      patch('agents.main_agent.AgentFactory.run') as harness):
                    events = [e async for e in stream_agent_response([{'role':'user','content':'explain status, policy, bounded and authorization'}],
                        1, 1, 'token', 'fixed-history', accept_language=language,
                        draft_catalog=[{'reference':snapshot['reference'],'name':snapshot['name']}],
                        draft_reader=AsyncMock(return_value=snapshot))]
                self.assertEqual(len(model.messages), 1)  # Route only: astream would consume another decision.
                text = ''.join(e.payload['delta'] for e in events if e.kind=='text')
                self.assertIn(status, text)
                self.assertIn('bounded', text)
                self.assertIn('replace', text)
                self.assertIn('不是权限或批准' if language=='zh-cn' else 'not a permission or approval', text)
                self.assertIn('未核验当前权限' if language=='zh-cn' else 'permission or approval was not checked', text)
                self.assertEqual(next(e.payload['report'] for e in events if e.kind=='presentation' and e.payload['kind']=='transfer_config_check'), snapshot['report'])
                tools.assert_not_called()
                harness.assert_not_called()

    def test_scope_projects_only_selected_mapping_and_query_without_request_report_or_unrelated_config(self):
        from services.drafts import build_draft_explanation
        snapshot = draft_snapshot(checked_step())
        before = copy.deepcopy(snapshot)
        fixed, fragments = build_draft_explanation(snapshot, {'topics':['status','fields'], 'fields':['members']}, 'zh-cn')
        self.assertEqual(set(fragments), {'mappings'})
        self.assertEqual(fragments['mappings'], [{'source':'members','target':'members','target_type':'json'}])
        for private in ('arguments', 'report', 'reference', 'name', 'request', 'runtime', 'policy'):
            self.assertNotIn(private, fragments)
        _, query = build_draft_explanation(snapshot, {'topics':['query'], 'fields':[]}, 'en')
        self.assertEqual(query, {'query':snapshot['arguments']['config']['source']['query']})
        self.assertEqual(snapshot, before)

    def test_invalid_scope_unknown_field_and_duplicate_topics_fail_closed(self):
        from services.drafts import build_draft_explanation
        snapshot = draft_snapshot(checked_step())
        for scope in (None, {'topics':['invented'],'fields':[]}, {'topics':['fields'],'fields':['absent']},
                      {'topics':['fields'],'fields':[]}, {'topics':['status'],'fields':['members']},
                      {'topics':['status','status'],'fields':[]}, {'topics':['fields'],'fields':['members','members']},
                      {'topics':['status'],'fields':[], 'request':'untrusted'}):
            with self.subTest(scope=scope), self.assertRaisesRegex(DraftReferenceError, 'draft_explanation_scope_invalid'):
                build_draft_explanation(snapshot, scope, 'zh-cn')

    def test_missing_unknown_policy_boundary_and_query_do_not_invent_defaults(self):
        from services.drafts import build_draft_explanation
        snapshot = draft_snapshot(checked_step())
        config = snapshot['arguments']['config']
        for policy, boundary in ((None,None), ('unrecognised',{})):
            config['target']['policy'] = {'apply_mode':policy}
            config['runtime'] = {'boundary':boundary}
            config['source'].pop('query', None)
            text, fragments = build_draft_explanation(snapshot, {'topics':['policy','boundary','query'], 'fields':[]}, 'zh-cn')
            self.assertEqual(fragments, {})
            self.assertIn('没有可解释的写入策略', text)
            self.assertIn('没有可解释的执行边界', text)
            self.assertIn('没有可解释的已有查询', text)
            self.assertNotIn('append', text)
            self.assertNotIn('bounded', text)

    def test_append_and_continuous_describe_configuration_not_current_authority(self):
        from services.drafts import build_draft_explanation
        snapshot = draft_snapshot(checked_step())
        snapshot['arguments']['config']['target']['policy']['apply_mode'] = 'append'
        snapshot['arguments']['config']['runtime']['boundary'] = 'continuous'
        text, _ = build_draft_explanation(snapshot, {'topics':['policy','boundary'], 'fields':[]}, 'zh-cn')
        self.assertIn('持续等待变化', text)
        self.assertIn('不表示已经写入', text)
        self.assertIn('未核验当前权限', text)
        self.assertNotIn('当前没有授权', text)

    async def test_scope_required_only_for_inspect_without_legacy_default(self):
        for decision in (RouteDecision(draft_action='inspect'),
                         RouteDecision(skill=None, draft_explanation=DraftExplanationScope(topics=['status'],fields=[])),
                         RouteDecision(draft_action='create', needs_platform_semantics=True,
                                       draft_explanation=DraftExplanationScope(topics=['status'],fields=[]))):
            with self.subTest(decision=decision), self.assertRaisesRegex(ValueError,'draft_explanation_scope_invalid'):
                await _route_node({'messages':[{'role':'user','content':'inspect'}], 'draft_catalog':[], 'reply_detail':'standard'},
                                  ScriptedRouter([decision]))
