import unittest

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
        tools = create_agent_tools("token")
        stable_names = [stable_tool_name(tool) for tool in tools]

        self.assertIn("workflow.validate", stable_names)
        validate_tool = tools[stable_names.index("workflow.validate")]
        self.assertEqual(validate_tool.name, "workflow__validate")
        schema = validate_tool.args_schema.model_json_schema()
        self.assertEqual(
            set(schema["required"]),
            {"workflow_engine_id", "workflow_definition"},
        )
        draft_tool = tools[stable_names.index("workflow.draft.generate")]
        draft_schema = draft_tool.args_schema.model_json_schema()
        self.assertEqual(
            set(draft_schema["required"]),
            {"query", "workflow_engine_id", "resources"},
        )


if __name__ == "__main__":
    unittest.main()
