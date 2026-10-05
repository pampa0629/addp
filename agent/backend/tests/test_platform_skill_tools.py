import unittest
from unittest.mock import AsyncMock, patch
from langchain_core.utils.function_calling import convert_to_openai_tool

from agents.main_agent import _load_skill_registry
from tools.langchain_tools import create_agent_tools, stable_tool_name


class PlatformSkillToolTests(unittest.TestCase):
    def test_transfer_skill_uses_platform_semantics_and_only_metadata_write(self):
        registry = _load_skill_registry()
        skill = registry["transfer-generation"]
        self.assertEqual(skill.tools, ["platform.capability.context", "engine.list", "data.search", "resource.children.list", "resource.ancestors.get", "resource.facts.get", "transfer.draft.generate", "transfer.task.create"])
        self.assertNotIn("data.preview", skill.tools)
        self.assertNotIn("workflow.run", skill.tools)
        self.assertIn("尚未运行", skill.load_body(registry))
        body = skill.load_body(registry)
        self.assertIn("JSON command object", body)
        self.assertIn("query_names.mql", body)
        self.assertIn("不是 mongosh", body)
        self.assertIn("分别提交 `string`、`bool`", body)
        self.assertIn("不用于 schema、database", body)
        self.assertIn("必须包含 `role + locator`", body)
        self.assertIn("task.config.source.query", body)
        self.assertIn("task.config.transforms", body)
        tools = create_agent_tools("token", "run-transfer")
        create = next(tool for tool in tools if stable_tool_name(tool) == "transfer.task.create")
        schema = create.tool_call_schema
        self.assertEqual(schema["properties"]["config"]["properties"]["runtime"]["properties"]["boundary"], {"const": "bounded"})
        self.assertFalse(schema["properties"]["config"]["additionalProperties"])
        published = convert_to_openai_tool(create)["function"]["parameters"]
        self.assertNotIn("tool_call_id", published["properties"])
        expected_config = {key: value for key, value in schema["properties"]["config"].items() if key != "title"}
        self.assertEqual(published["properties"]["config"], expected_config)

    def test_transfer_draft_publishes_strict_context_to_harness(self):
        from addp_common.tools import get_tool

        tools = create_agent_tools("token", "run-draft")
        draft = next(tool for tool in tools if stable_tool_name(tool) == "transfer.draft.generate")
        published = convert_to_openai_tool(draft)["function"]["parameters"]
        definition = get_tool("transfer.draft.generate")
        self.assertEqual(definition.version, "2.0.0")
        for key in ("resources", "task"):
            actual = {name: value for name, value in published["properties"][key].items() if name != "title"}
            self.assertEqual(actual, definition.input_schema["properties"][key])

    def test_agent_loads_root_skill_and_addp_runtime_config(self):
        registry = _load_skill_registry()
        skill = registry["workflow-analysis"]

        self.assertIn("/skills/workflow-analysis/SKILL.md", skill.path.as_posix())
        self.assertEqual(
            skill.tools,
            [
                "engine.list",
                "data.search",
                "resource.ancestors.get",
                "resource.facts.get",
                "data.preview",
                "workflow.operators.list",
                "workflow.draft.generate",
                "workflow.validate",
                "workflow.run",
                "execution.get",
            ],
        )
        self.assertEqual(skill.max_iterations, 8)
        self.assertEqual(skill.required_skills, ["data-discovery"])
        self.assertIn("# 数据发现与确认", skill.load_body(registry))

    def test_skill_dependencies_do_not_inherit_tool_permissions(self):
        registry = _load_skill_registry()
        self.assertEqual(registry["notebook-generation"].tools, ["notebook.draft.generate"])

    def test_ontology_skill_exposes_only_definition_tools(self):
        registry = _load_skill_registry()
        skill = registry["ontology-exploration"]
        self.assertEqual(skill.tools, ["ontology.classes.list", "ontology.class.context"])
        self.assertEqual(skill.required_skills, [])
        self.assertEqual(skill.max_iterations, 6)
        self.assertIn("native_definition", skill.load_body(registry))
        tools = create_agent_tools("token", "run-ontology")
        context = next(tool for tool in tools if stable_tool_name(tool) == "ontology.class.context")
        self.assertEqual(set(context.tool_call_schema["required"]), {"ontology_id", "class_id", "revision", "generation", "activation_version"})

    def test_langchain_adapter_uses_runtime_safe_names_and_manifest_schemas(self):
        tools = create_agent_tools("token", "run-1")
        stable_names = [stable_tool_name(tool) for tool in tools]

        self.assertIn("workflow.validate", stable_names)
        validate_tool = tools[stable_names.index("workflow.validate")]
        self.assertEqual(validate_tool.name, "workflow__validate")
        schema = validate_tool.tool_call_schema
        self.assertEqual(
            set(schema["required"]),
            {"workflow_engine_id", "workflow_definition"},
        )
        draft_tool = tools[stable_names.index("workflow.draft.generate")]
        draft_schema = draft_tool.tool_call_schema
        self.assertEqual(
            set(draft_schema["required"]),
            {"query", "workflow_engine_id", "resources"},
        )

    def test_langchain_adapter_binds_agent_run_and_tool_call(self):
        executor = AsyncMock()
        executor.call.return_value = {"valid": True, "workflow_engine_id": 12, "errors": [], "warnings": []}
        with patch("tools.langchain_tools.ToolExecutor", return_value=executor):
            tools = create_agent_tools("source-token", "run-1")
        validate_tool = next(tool for tool in tools if stable_tool_name(tool) == "workflow.validate")

        async def invoke():
            return await validate_tool.ainvoke({
                "name": validate_tool.name,
                "args": {"workflow_engine_id": 12, "workflow_definition": {"tasks": []}},
                "id": "call-1",
                "type": "tool_call",
            })

        import asyncio

        result = asyncio.run(invoke())
        self.assertIn('"valid":true', result.content)
        executor.call.assert_awaited_once_with(
            "workflow.validate",
            {"workflow_engine_id": 12, "workflow_definition": {"tasks": []}},
            agent_run_id="run-1",
            tool_call_id="call-1",
        )

    def test_business_config_is_not_replaced_by_framework_run_config(self):
        executor = AsyncMock()
        executor.call.return_value = {"id": 1, "status": "idle", "desired_state": "stopped"}
        with patch("tools.langchain_tools.ToolExecutor", return_value=executor):
            tools = create_agent_tools("source-token", "run-transfer")
        create = next(tool for tool in tools if stable_tool_name(tool) == "transfer.task.create")
        arguments = {
            "name": "acceptance-task",
            "config": {
                "runtime": {"boundary": "bounded"},
                "load": {"mode": "snapshot"},
                "source": {"locator": "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=111"},
                "target": {
                    "parent_locator": "addp://engine/2/path/outdoor?type=schema&node_id=3",
                    "name": "acceptance_table",
                },
            },
        }

        async def invoke():
            await create.ainvoke({
                "name": create.name, "args": arguments,
                "id": "create-call", "type": "tool_call",
            }, config={"tags": ["framework-only"], "configurable": {"trace": "framework-only"}})

        import asyncio

        asyncio.run(invoke())
        executor.call.assert_awaited_once_with(
            "transfer.task.create", arguments,
            agent_run_id="run-transfer", tool_call_id="create-call",
        )


if __name__ == "__main__":
    unittest.main()
