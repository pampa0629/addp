import unittest
from unittest.mock import AsyncMock, patch

from agents.events import text_event
from agents.main_agent import stream_agent_response


class _Skill:
    tools = ["workflow.run"]
    max_iterations = 2

    @staticmethod
    def load_body():
        return "workflow skill"


class MainAgentResumeTests(unittest.IsolatedAsyncioTestCase):
    async def test_interaction_resume_reuses_recorded_skill_without_rerouting(self):
        async def fake_factory_run(**kwargs):
            self.assertEqual(kwargs["allowed_tool_names"], ["workflow.run"])
            yield text_event("resumed")

        route = AsyncMock(side_effect=AssertionError("resume must not reroute"))
        with (
            patch("agents.main_agent._get_skill_registry", return_value={"workflow-analysis": _Skill()}),
            patch("agents.main_agent._route_node", route),
            patch("agents.main_agent.get_llm", return_value=object()),
            patch("agents.main_agent.AgentFactory.run", side_effect=fake_factory_run),
        ):
            events = [
                event
                async for event in stream_agent_response(
                    [{"role": "user", "content": "Develop 已批准，继续执行"}],
                    user_id=3,
                    tenant_id=5,
                    token="user-token",
                    agent_run_id="run-1",
                    checkpoint={},
                    forced_skill_name="workflow-analysis",
                )
            ]

        self.assertFalse(route.called)
        self.assertEqual([event.kind for event in events], ["run_state", "text"])
        self.assertEqual(events[0].payload["skill_name"], "workflow-analysis")


if __name__ == "__main__":
    unittest.main()
