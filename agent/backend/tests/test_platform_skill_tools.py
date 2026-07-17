import unittest
from unittest.mock import AsyncMock, patch

from agents.main_agent import _load_skill_registry
from tools.langchain_tools import create_agent_tools, stable_tool_name


class PlatformSkillToolTests(unittest.TestCase):
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
                "data.preview",
                "workflow.operators.list",
                "workflow.draft.generate",
                "workflow.validate",
                "workflow.run",
                "execution.get",
            ],
        )
        self.assertEqual(skill.max_iterations, 8)

    def test_langchain_adapter_uses_runtime_safe_names_and_manifest_schemas(self):
        tools = create_agent_tools("token", "run-1")
        stable_names = [stable_tool_name(tool) for tool in tools]

        self.assertIn("workflow.validate", stable_names)
        validate_tool = tools[stable_names.index("workflow.validate")]
        self.assertEqual(validate_tool.name, "workflow__validate")
        schema = validate_tool.tool_call_schema.model_json_schema()
        self.assertEqual(
            set(schema["required"]),
            {"workflow_engine_id", "workflow_definition"},
        )
        draft_tool = tools[stable_names.index("workflow.draft.generate")]
        draft_schema = draft_tool.tool_call_schema.model_json_schema()
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


if __name__ == "__main__":
    unittest.main()
