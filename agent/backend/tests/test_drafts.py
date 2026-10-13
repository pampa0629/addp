"""Exact historical arguments, real SQL scope predicates and readonly routing.

In-memory SQL is T1, not a PostgreSQL or live-model acceptance claim.
"""
import copy
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
from graph.state import PlatformRouteDecision, RouteDecision
from models.run import AgentRun
from models.run_step import AgentRunStep
from models.session import Session
from services.drafts import draft_snapshot, draft_presentation, list_session_drafts, read_session_draft, DraftReferenceError
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
                         RouteDecision(draft_action="inspect", direct_reply="guessed"),
                         RouteDecision(draft_action="inspect", draft_ref="not-observed"),
                         RouteDecision(draft_action="create", draft_ref="known-ref"),
                         RouteDecision(draft_ref="known-ref")):
            with self.subTest(decision=decision), self.assertRaises(ValueError):
                await _route_node({"messages": [{"role": "user", "content": "解释草稿"}],
                                   "draft_catalog": [{"reference": "known-ref", "name": "one"}]}, ScriptedRouter([decision]))

    async def test_inspection_receives_exact_snapshot_and_never_assembles_business_tools(self):
        snapshot = draft_snapshot(checked_step())
        catalog = [{"reference": snapshot["reference"], "name": snapshot["name"], "observed_at": snapshot["report"]["observed_at"]}]
        model = ScriptedRouter([RouteDecision(draft_action="inspect", draft_ref=snapshot["reference"]), AIMessage(content="按原配置解释，未执行。")])
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
        self.assertEqual(context["snapshot"]["arguments"], snapshot["arguments"])
        views = [e.payload for e in events if e.kind == "presentation"]
        self.assertEqual(views[0]["mappings"][-1]["target_type"], "json")
        self.assertEqual(views[1]["report"], snapshot["report"])
        self.assertEqual([e.payload["tool_name"] for e in events if e.kind == "tool_start"], ["draft.inspect"])

    async def test_ambiguous_draft_does_not_pick_first_or_call_reader(self):
        model = ScriptedRouter([RouteDecision(draft_action="inspect")])
        reader = AsyncMock()
        with patch("agents.main_agent.get_llm", return_value=model):
            events = [e async for e in stream_agent_response([{"role": "user", "content": "解释草稿"}], 1, 1, "token", "ambiguous",
                draft_catalog=[{"reference": "a", "name": "one"}, {"reference": "b", "name": "two"}], draft_reader=reader)]
        reader.assert_not_called()
        self.assertFalse(any(e.kind == "presentation" for e in events))

    async def test_unavailable_selected_snapshot_fails_closed_without_model_explanation(self):
        model = ScriptedRouter([RouteDecision(draft_action="inspect", draft_ref="known-ref")])
        with patch("agents.main_agent.get_llm", return_value=model):
            events = [e async for e in stream_agent_response([{"role": "user", "content": "解释"}], 1, 1, "token", "missing",
                draft_catalog=[{"reference": "known-ref", "name": "one"}],
                draft_reader=AsyncMock(side_effect=DraftReferenceError("draft_reference_unavailable")))]
        self.assertEqual(len(model.messages), 1)
        self.assertTrue(any(e.kind == "run_failed" for e in events))
        self.assertFalse(any(e.kind == "presentation" for e in events))
