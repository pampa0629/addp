"""Short business input uses owner platform semantics before choosing a Skill.

Scripted routing decisions verify the consumption boundary, not LLM accuracy.
"""
import copy
import json
import unittest
from pathlib import Path
from unittest.mock import patch

from langchain_core.tools import StructuredTool

from agents.events import text_event
from agents.main_agent import SkillMeta, _route_node, _select_platform_skill, stream_agent_response
from graph.state import PlatformRouteDecision, RouteDecision


ROOT = Path(__file__).resolve().parents[3]
SHORT_REQUEST = "把 MongoDB 的 outdoor 传到 PostgreSQL，先建任务不要运行。"


def catalog_fixture():
    definition = json.loads((ROOT / "ontology/backend/internal/platform/transfer.json").read_text())
    definition["digest"] = "a" * 64
    return {"schema_version": "addp.platform-capability-catalog/v1", "capabilities": [definition]}


def registry_fixture():
    return {"transfer-generation": SkillMeta(
        "transfer-generation", "导入、导出、复制已有数据，创建不等于运行。",
        ROOT / "skills/transfer-generation/SKILL.md", ["transfer.task.create"], 14,
        platform_routed=True,
    )}


class ScriptedRouter:
    def __init__(self, decisions):
        self.decisions = list(decisions)
        self.schemas = []
        self.messages = []

    def with_structured_output(self, schema):
        self.schemas.append(schema)
        return self

    async def ainvoke(self, messages):
        self.messages.append(messages)
        return self.decisions.pop(0)


class PlatformRoutingTests(unittest.IsolatedAsyncioTestCase):
    async def test_short_request_selects_from_owner_catalog_before_domain_execution(self):
        model = ScriptedRouter([
            RouteDecision(needs_platform_semantics=True),
            PlatformRouteDecision(capability="transfer.task.create"),
        ])
        trace = []

        async def catalog_call():
            trace.append("platform.capabilities.list")
            return json.dumps(catalog_fixture(), ensure_ascii=False)

        tool = StructuredTool.from_function(
            name="platform__capabilities__list", description="catalog", coroutine=catalog_call,
            metadata={"addp_tool_name": "platform.capabilities.list"},
        )

        async def domain_run(**kwargs):
            trace.append(kwargs["task_context"]["skill_name"])
            self.assertEqual(kwargs["task_context"]["user_request"], SHORT_REQUEST)
            self.assertEqual(kwargs["allowed_tool_names"], ["transfer.task.create"])
            yield text_event("等待真实资源发现与配置确认")

        with (
            patch("agents.main_agent._get_skill_registry", return_value=registry_fixture()),
            patch("agents.main_agent.get_llm", return_value=model),
            patch("agents.main_agent.create_agent_tools", return_value=[tool]),
            patch("agents.main_agent.AgentFactory.run", side_effect=domain_run),
        ):
            events = [event async for event in stream_agent_response(
                [{"role": "user", "content": SHORT_REQUEST}], 3, 5, "user-token", "run-short",
            )]
        self.assertEqual(trace, ["platform.capabilities.list", "transfer-generation"])
        self.assertEqual(model.schemas, [RouteDecision, PlatformRouteDecision])
        self.assertEqual([event.kind for event in events], ["tool_start", "tool_result", "run_state", "text"])
        self.assertNotIn("`transfer-generation`:", model.messages[0][0].content)
        selection = json.loads(model.messages[1][1].content)
        self.assertEqual(selection["user_request"], SHORT_REQUEST)
        self.assertEqual(selection["platform_catalog"], catalog_fixture())
        self.assertIn("business_data.written", selection["platform_catalog"]["capabilities"][0]["operation"]["excluded_effects"])

    async def test_platform_skill_cannot_be_selected_without_semantic_catalog(self):
        state = {"messages": [{"role": "user", "content": SHORT_REQUEST}], "session_summary": None}
        with patch("agents.main_agent._get_skill_registry", return_value=registry_fixture()):
            for decision in [RouteDecision(skill="transfer-generation"), RouteDecision(needs_platform_semantics=True, skill="transfer-generation")]:
                with self.assertRaises(ValueError):
                    await _route_node(state, ScriptedRouter([decision]))

    async def test_unobserved_capability_and_wrong_contract_are_rejected(self):
        state = {"messages": [{"role": "user", "content": SHORT_REQUEST}]}
        with patch("agents.main_agent._get_skill_registry", return_value=registry_fixture()):
            with self.assertRaisesRegex(ValueError, "platform_capability_not_observed"):
                await _select_platform_skill(state, catalog_fixture(), ScriptedRouter([PlatformRouteDecision(capability="transfer.task.run")]))
            for key, value in [("skill", "other-skill"), ("tool", "workflow.run"), ("owner", "develop")]:
                catalog = copy.deepcopy(catalog_fixture())
                catalog["capabilities"][0]["operation"][key] = value
                with self.assertRaises(ValueError):
                    await _select_platform_skill(state, catalog, ScriptedRouter([PlatformRouteDecision(capability="transfer.task.create")]))

    async def test_empty_or_unmatched_catalog_does_not_default_to_create(self):
        state = {"messages": [{"role": "user", "content": "持续同步并立即运行"}]}
        with patch("agents.main_agent._get_skill_registry", return_value=registry_fixture()):
            for catalog in [catalog_fixture(), {"schema_version": "addp.platform-capability-catalog/v1", "capabilities": []}]:
                got = await _select_platform_skill(state, catalog, ScriptedRouter([
                    PlatformRouteDecision(direct_reply="当前目录只提供创建未启动任务，不能执行该目标。"),
                ]))
                self.assertIsNone(got["routed_skill"])

    async def test_catalog_error_terminates_without_retry_or_domain_model(self):
        model = ScriptedRouter([RouteDecision(needs_platform_semantics=True)])

        async def catalog_call():
            return json.dumps({"error": {"code": "owner_api_unavailable", "message": "Ontology unavailable"}})

        tool = StructuredTool.from_function(name="platform__capabilities__list", description="catalog", coroutine=catalog_call, metadata={"addp_tool_name": "platform.capabilities.list"})
        with (
            patch("agents.main_agent._get_skill_registry", return_value=registry_fixture()),
            patch("agents.main_agent.get_llm", return_value=model),
            patch("agents.main_agent.create_agent_tools", return_value=[tool]),
            patch("agents.main_agent.AgentFactory.run", side_effect=AssertionError("no fallback")),
        ):
            events = [event async for event in stream_agent_response([{"role": "user", "content": SHORT_REQUEST}], 3, 5, "user-token", "run-failed")]
        self.assertEqual(len(model.messages), 1)
        self.assertEqual([event.kind for event in events], ["tool_start", "tool_result", "run_failed"])
        self.assertEqual(events[-1].payload["error_source"], "owner")

    async def test_chat_does_not_assume_ontology_exists(self):
        model = ScriptedRouter([RouteDecision(direct_reply="你好")])
        with (
            patch("agents.main_agent._get_skill_registry", return_value=registry_fixture()),
            patch("agents.main_agent.get_llm", return_value=model),
            patch("agents.main_agent.create_agent_tools", side_effect=AssertionError("no ontology dependency")),
        ):
            events = [event async for event in stream_agent_response([{ "role": "user", "content": "你好"}], 3, 5, "user-token", "run-chat")]
        self.assertEqual([event.kind for event in events], ["text"])


if __name__ == "__main__":
    unittest.main()
