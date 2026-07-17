import json
import sys
import unittest
from pathlib import Path
from unittest.mock import patch

from graph.factory import AgentFactory


REPO_ROOT = Path(__file__).resolve().parents[3]
SCENARIOS_ROOT = REPO_ROOT / "evals" / "agent-scenarios"
sys.path.insert(0, str(SCENARIOS_ROOT))

from evaluator import EvaluationFailure, evaluate_trace, load_scenario, phase_from_events


class _Response:
    def __init__(self, *, tool_calls=None, content=""):
        self.tool_calls = tool_calls or []
        self.content = content


class _ScriptedLLM:
    def __init__(self, responses):
        self.responses = list(responses)

    def bind_tools(self, _tools):
        return self

    async def ainvoke(self, _messages):
        return self.responses.pop(0)


class _Tool:
    def __init__(self, stable_name, result):
        self.name = stable_name.replace(".", "__")
        self.metadata = {"addp_tool_name": stable_name}
        self.result = result

    async def ainvoke(self, _args):
        return json.dumps(self.result, ensure_ascii=False)


def _tool_call(stable_name, arguments=None, call_id="call-1"):
    return {
        "id": call_id,
        "name": stable_name.replace(".", "__"),
        "args": arguments or {},
    }


class AgentEvaluationBaselineTests(unittest.IsolatedAsyncioTestCase):
    def test_all_scenarios_use_the_single_versioned_contract(self):
        scenario_paths = sorted(SCENARIOS_ROOT.glob("*/scenario.yaml"))
        scenario_names = {path.parent.name for path in scenario_paths}
        self.assertTrue(
            {"approval-execution", "railway-farmland-area", "read-only-query", "rejection-and-forbidden"}
            <= scenario_names
        )
        for path in scenario_paths:
            scenario = load_scenario(path)
            self.assertEqual(scenario["schema"], "addp.agent-scenario/v1")

        railway = load_scenario(SCENARIOS_ROOT / "railway-farmland-area")
        trace = {
            "skill": "workflow-analysis",
            "conditions": ["railway_candidates_not_unique"],
            "assumptions": ["geometry_column_is_geom"],
            "phases": [
                {
                    "name": "design",
                    "agent_run_id": "run-railway",
                    "status": "completed",
                    "tools": [],
                    "interactions": [],
                    "presentations": [],
                    "result_refs": [],
                    "owner_effects": {"approvals_created": 0, "executions_created": 0},
                    "persisted_state": {},
                }
            ],
        }
        with self.assertRaises(EvaluationFailure) as context:
            evaluate_trace(railway, trace)
        self.assertIn("railway_candidates_not_unique", str(context.exception))
        self.assertIn("geometry_column_is_geom", str(context.exception))

    async def test_read_only_query_golden_scenario(self):
        scenario = load_scenario(SCENARIOS_ROOT / "read-only-query")
        events = await self._run_factory(
            agent_run_id="run-read",
            tools=[
                _Tool(
                    "data.search",
                    {
                        "total": 1,
                        "results": [
                            {
                                "name": "railway",
                                "location": {"locator": "addp://engine/8/path/public/railway?type=table"},
                            }
                        ],
                    },
                ),
                _Tool(
                    "data.preview",
                    {
                        "columns": ["name", "length_m"],
                        "rows": [{"name": "railway", "length_m": 1200}],
                        "total": 1,
                    },
                ),
            ],
            responses=[
                _Response(tool_calls=[_tool_call("data.search", {"query": "铁路", "limit": 10})]),
                _Response(
                    tool_calls=[
                        _tool_call(
                            "data.preview",
                            {"locator": "addp://engine/8/path/public/railway?type=table", "limit": 10},
                            call_id="call-2",
                        )
                    ]
                ),
                _Response(content="找到一个铁路数据项。"),
            ],
            allowed_tools=["data.search", "data.preview"],
        )
        trace = {
            "skill": "workflow-analysis",
            "phases": [
                phase_from_events(
                    "query",
                    "run-read",
                    "completed",
                    events,
                    owner_effects={"approvals_created": 0, "executions_created": 0},
                    persisted_state={"steps": [{"tool_name": "data.search", "output_summary": {"total": 1}}]},
                )
            ],
        }

        evaluate_trace(scenario, trace)

    async def test_observed_resource_ambiguity_uses_resource_picker(self):
        first_locator = "addp://engine/8/path/public/railway?type=table&item_id=60"
        second_locator = "addp://engine/8/path/public/railway_backup?type=table&item_id=61"
        events = await self._run_factory(
            agent_run_id="run-resource-picker",
            tools=[
                _Tool(
                    "data.search",
                    {
                        "total": 2,
                        "results": [
                            {
                                "name": "railway",
                                "location": {
                                    "locator": first_locator,
                                    "engine_id": 8,
                                    "full_name": "public.railway",
                                },
                            },
                            {
                                "name": "railway_backup",
                                "location": {
                                    "locator": second_locator,
                                    "engine_id": 8,
                                    "full_name": "public.railway_backup",
                                },
                            },
                        ],
                    },
                )
            ],
            responses=[
                _Response(tool_calls=[_tool_call("data.search", {"query": "铁路", "limit": 10})]),
                _Response(
                    tool_calls=[
                        _tool_call(
                            "request_clarification",
                            {
                                "prompt": "请选择铁路数据",
                                "reason": "data_source_ambiguous",
                                "options": [
                                    {
                                        "label": "public.railway",
                                        "value": first_locator,
                                        "candidate": {"locator": first_locator},
                                    },
                                    {
                                        "label": "public.railway_backup",
                                        "value": second_locator,
                                        "candidate": {"locator": second_locator},
                                    },
                                ],
                            },
                            call_id="clarify-1",
                        )
                    ]
                ),
            ],
            allowed_tools=["data.search"],
        )

        interaction = next(event for event in events if event.kind == "interaction_required")
        self.assertEqual(interaction.payload["reason"], "data_source_ambiguous")
        self.assertEqual(
            [candidate["value"] for candidate in interaction.payload["candidates"]],
            [first_locator, second_locator],
        )
        phase = phase_from_events(
            "design",
            "run-resource-picker",
            "waiting",
            events,
            owner_effects={"approvals_created": 0, "executions_created": 0},
            persisted_state={},
        )
        self.assertEqual(
            phase["interactions"],
            [{"kind": "clarification", "owner": "agent", "status": "pending"}],
        )
        self.assertEqual(phase["presentations"], ["ResourcePicker"])

    async def test_approval_execution_golden_scenario(self):
        scenario = load_scenario(SCENARIOS_ROOT / "approval-execution")
        approval_id = "46ea0d75-b9bc-4b25-8d4a-441947081813"
        fingerprint = "a" * 64
        request_events = await self._run_factory(
            agent_run_id="run-approval",
            tools=[
                _Tool(
                    "workflow.run",
                    {
                        "status": "approval_required",
                        "interaction_id": approval_id,
                        "open_url": f"/develop/approvals/{approval_id}",
                        "request_fingerprint": fingerprint,
                        "request_summary": {"workflow_engine_id": 20, "task_count": 2},
                        "expires_at": "2026-07-17T10:15:00Z",
                    },
                )
            ],
            responses=[
                _Response(
                    tool_calls=[
                        _tool_call(
                            "workflow.run",
                            {"workflow_engine_id": 20, "workflow_definition": {"tasks": [{"id": "load"}]}},
                        )
                    ]
                )
            ],
            allowed_tools=["workflow.run"],
        )
        resume_events = await self._run_factory(
            agent_run_id="run-approval",
            tools=[_Tool("workflow.run", {"execution_id": "704bcbcc-ecbb-4f2f-a84e-d601eeac1fdd"})],
            responses=[
                _Response(
                    tool_calls=[
                        _tool_call(
                            "workflow.run",
                            {"approval_id": approval_id, "request_fingerprint": fingerprint},
                        )
                    ]
                ),
                _Response(content="执行已创建。"),
            ],
            allowed_tools=["workflow.run"],
        )
        trace = {
            "skill": "workflow-analysis",
            "phases": [
                phase_from_events(
                    "request",
                    "run-approval",
                    "waiting",
                    request_events,
                    owner_effects={"approvals_created": 1, "executions_created": 0},
                    persisted_state={
                        "interaction": {"owner_interaction_id": approval_id, "request_fingerprint": fingerprint},
                        "step": {"workflow_engine_id": 20, "task_count": 1, "has_engine_specific": False},
                    },
                ),
                phase_from_events(
                    "resume",
                    "run-approval",
                    "completed",
                    resume_events,
                    owner_effects={"approvals_created": 0, "executions_created": 1},
                    persisted_state={"step": {"approval_id": approval_id, "request_fingerprint": fingerprint}},
                ),
            ],
        }

        evaluate_trace(scenario, trace)

    async def test_rejection_and_forbidden_golden_scenario(self):
        scenario = load_scenario(SCENARIOS_ROOT / "rejection-and-forbidden")
        rejected_events = await self._run_error_phase("run-owner", "approval_rejected")
        replay_events = await self._run_error_phase("run-replay", "approval_forbidden")
        trace = {
            "skill": "workflow-analysis",
            "phases": [
                phase_from_events(
                    "rejected",
                    "run-owner",
                    "completed",
                    rejected_events,
                    owner_effects={"approvals_created": 0, "executions_created": 0},
                    persisted_state={"step": {"error_code": "approval_rejected"}},
                ),
                phase_from_events(
                    "replay",
                    "run-replay",
                    "completed",
                    replay_events,
                    owner_effects={"approvals_created": 0, "executions_created": 0},
                    persisted_state={"step": {"error_code": "approval_forbidden"}},
                ),
            ],
        }

        evaluate_trace(scenario, trace)

    async def test_evaluator_rejects_error_downgrade_and_sensitive_payload(self):
        scenario = load_scenario(SCENARIOS_ROOT / "rejection-and-forbidden")
        rejected_events = await self._run_error_phase("run-owner", "approval_rejected")
        replay_events = await self._run_error_phase("run-replay", "owner_api_error")
        trace = {
            "skill": "workflow-analysis",
            "phases": [
                phase_from_events(
                    "rejected",
                    "run-owner",
                    "completed",
                    rejected_events,
                    owner_effects={"approvals_created": 0, "executions_created": 0},
                    persisted_state={"step": {"error_code": "approval_rejected"}},
                ),
                phase_from_events(
                    "replay",
                    "run-replay",
                    "completed",
                    replay_events,
                    owner_effects={"approvals_created": 0, "executions_created": 0},
                    persisted_state={"step": {"workflow_definition": {"tasks": []}}},
                ),
            ],
        }

        with self.assertRaises(EvaluationFailure) as context:
            evaluate_trace(scenario, trace)
        self.assertIn("approval_forbidden", str(context.exception))
        self.assertIn("workflow_definition", str(context.exception))

    async def _run_error_phase(self, agent_run_id, error_code):
        return await self._run_factory(
            agent_run_id=agent_run_id,
            tools=[_Tool("workflow.run", {"error": {"code": error_code, "message": "审批不可用"}})],
            responses=[
                _Response(
                    tool_calls=[
                        _tool_call(
                            "workflow.run",
                            {"approval_id": "46ea0d75-b9bc-4b25-8d4a-441947081813", "request_fingerprint": "a" * 64},
                        )
                    ]
                ),
                _Response(content="未执行工作流。"),
            ],
            allowed_tools=["workflow.run"],
        )

    async def _run_factory(self, *, agent_run_id, tools, responses, allowed_tools):
        context = {
            "skill_name": "workflow-analysis",
            "user_request": "评测请求",
            "context_summary": "",
            "user_id": 1,
            "tenant_id": 1,
            "token": "runtime-token",
            "agent_run_id": agent_run_id,
        }
        with (
            patch("graph.factory.create_agent_tools", return_value=tools),
            patch("graph.factory.get_llm", return_value=_ScriptedLLM(responses)),
        ):
            return [
                event
                async for event in AgentFactory.run(
                    task_context=context,
                    skill_body="评测 Skill",
                    allowed_tool_names=allowed_tools,
                    max_iterations=len(responses),
                )
            ]


if __name__ == "__main__":
    unittest.main()
