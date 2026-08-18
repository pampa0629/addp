import json
import unittest
from unittest.mock import patch

from agents.checkpoint import capture_owner_facts, new_checkpoint
from agents.main_agent import _build_routing_system_prompt
from graph.factory import AgentFactory


class _FakeTool:
    name = "workflow__draft__generate"
    metadata = {"addp_tool_name": "workflow.draft.generate"}

    async def ainvoke(self, _args):
        return json.dumps(
            {
                "status": "need_clarification",
                "message": "请补充已验证的资源事实",
                "clarification_reason": "resource_facts_required",
            },
            ensure_ascii=False,
        )


class _FakeResponse:
    tool_calls = [
        {"id": "call-1", "name": "workflow__draft__generate", "args": {"query": "分析铁路", "workflow_engine_id": 12}}
    ]
    content = ""


class _FakeLLM:
    def bind_tools(self, _tools):
        return self

    async def ainvoke(self, _messages):
        return _FakeResponse()


class _ClarificationResponse:
    tool_calls = [
        {
            "id": "clarify-1",
            "name": "request_clarification",
            "args": {
                "prompt": "请选择工作流运行时",
                "reason": "workflow_engine_ambiguous",
                "options": [
                    {
                        "label": "GeoPython Workflow",
                        "value": 20,
                        "candidate": {"id": 20, "engine_type": "geopython_workflow"},
                    }
                ],
            },
        }
    ]
    content = ""


class _ClarificationLLM(_FakeLLM):
    def __init__(self):
        self.calls = 0

    async def ainvoke(self, _messages):
        self.calls += 1
        if self.calls == 1:
            return _ScriptedResponse(
                tool_calls=[
                    {
                        "id": "engines-1",
                        "name": "engine__list",
                        "args": {"capability": "workflow"},
                    }
                ]
            )
        return _ClarificationResponse()


class _EmptyWorkflowTool(_FakeTool):
    async def ainvoke(self, _args):
        return json.dumps(
            {
                "status": "validation_failed",
                "message": "工作流生成失败，未形成可预览的任务",
                "workflow": {"metadata": {}, "tasks": []},
                "errors": ["工作流至少需要一个任务"],
            },
            ensure_ascii=False,
        )


class _FinalResponse:
    tool_calls = []
    content = "缺少生成工作流所需的事实"


class _EmptyWorkflowLLM(_FakeLLM):
    def __init__(self):
        self.calls = 0

    async def ainvoke(self, _messages):
        self.calls += 1
        return _FakeResponse() if self.calls == 1 else _FinalResponse()


_WORKFLOW_DEFINITION = {
    "tasks": [
        {
            "id": "load_railway",
            "operator": "load",
            "params": {
                "locator": "addp://engine/8/path/public/railway?type=table&item_id=60",
                "geom_column": "geom",
            },
            "depends_on": [],
        },
        {
            "id": "buffer_railway",
            "operator": "buffer",
            "params": {"input_gdf": {"$ref": "load_railway"}, "distance": 50},
            "depends_on": ["load_railway"],
        },
    ]
}


class _SuccessfulDraftTool:
    name = "workflow__draft__generate"
    metadata = {"addp_tool_name": "workflow.draft.generate"}

    async def ainvoke(self, _args):
        return json.dumps(
            {
                "status": "success",
                "workflow_definition": _WORKFLOW_DEFINITION,
                "explanation": "已生成候选工作流",
            },
            ensure_ascii=False,
        )


class _ValidationTool:
    name = "workflow__validate"
    metadata = {"addp_tool_name": "workflow.validate"}

    def __init__(self, *, valid):
        self.valid = valid

    async def ainvoke(self, _args):
        return json.dumps(
            {
                "valid": self.valid,
                "workflow_engine_id": 20,
                "errors": [] if self.valid else ["工作流无效"],
                "warnings": [],
            },
            ensure_ascii=False,
        )


class _EngineListTool:
    name = "engine__list"
    metadata = {"addp_tool_name": "engine.list"}

    async def ainvoke(self, _args):
        return json.dumps(
            {
                "engines": [
                    {
                        "id": 20,
                        "name": "GeoPython Workflow",
                        "engine_type": "geopython_workflow",
                        "lifecycle_state": "active",
                        "connection_status": "online",
                    }
                ]
            },
            ensure_ascii=False,
        )


class _DocumentSearchTool:
    name = "data__search"
    metadata = {"addp_tool_name": "data.search"}

    async def ainvoke(self, _args):
        return json.dumps(
            {
                "total": 1,
                "results": [
                    {
                        "name": "data.meta",
                        "asset_type": "object",
                        "location": {
                            "locator": "addp://engine/9/path/gischain/data/data.meta?type=object&item_id=216",
                            "engine_id": 9,
                            "full_name": "gischain/data/data.meta",
                        },
                    }
                ],
            },
            ensure_ascii=False,
        )


class _FailingTool:
    name = "data__search"
    metadata = {"addp_tool_name": "data.search"}

    async def ainvoke(self, _args):
        raise RuntimeError("owner service unavailable")


class _OwnerErrorTool:
    name = "data__search"
    metadata = {"addp_tool_name": "data.search"}

    async def ainvoke(self, _args):
        return json.dumps(
            {"error": {"code": "owner_api_unavailable", "message": "manager unavailable"}},
            ensure_ascii=False,
        )


class _WorkflowApprovalTool:
    name = "workflow__run"
    metadata = {"addp_tool_name": "workflow.run"}

    async def ainvoke(self, _args):
        return json.dumps(
            {
                "status": "approval_required",
                "interaction_id": "46ea0d75-b9bc-4b25-8d4a-441947081813",
                "open_url": "/develop/approvals/46ea0d75-b9bc-4b25-8d4a-441947081813",
                "request_fingerprint": "a" * 64,
                "request_summary": {"workflow_engine_id": 20, "task_count": 2},
                "expires_at": "2026-07-17T10:15:00Z",
            },
            ensure_ascii=False,
        )


class _PreviewTool:
    name = "data__preview"
    metadata = {"addp_tool_name": "data.preview"}

    async def ainvoke(self, _args):
        return json.dumps(
            {
                "columns": ["name", "shape"],
                "rows": [
                    {
                        "name": "railway",
                        "shape": {"type": "Point", "coordinates": [104.0, 30.0]},
                    }
                ],
                "total": 1,
                "geometry_column": "shape",
                "source_crs": "EPSG:4326",
            },
            ensure_ascii=False,
        )


class _ScriptedResponse:
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


def _draft_call():
    return {
        "id": "draft-1",
        "name": "workflow__draft__generate",
        "args": {
            "query": "计算铁路两侧50米范围内占用的耕地面积",
            "workflow_engine_id": 20,
            "resources": [],
        },
    }


def _validation_call():
    return {
        "id": "validate-1",
        "name": "workflow__validate",
        "args": {
            "workflow_engine_id": 20,
            "workflow_definition": _WORKFLOW_DEFINITION,
        },
    }


class AgentFactoryEventTests(unittest.IsolatedAsyncioTestCase):
    def test_routing_prompt_requires_skill_for_spatial_workflow_design(self):
        prompt = _build_routing_system_prompt()

        self.assertIn("涉及空间分析、工作流设计、DAG、算子组合", prompt)
        self.assertIn("可执行方案", prompt)
        self.assertIn("不能退化为常识计算", prompt)

    async def test_runtime_clarification_uses_observed_engine_fact(self):
        context = {
            "skill_name": "workflow-analysis",
            "user_request": "分析铁路",
            "context_summary": "",
            "user_id": 1,
            "tenant_id": 1,
            "token": "token",
            "agent_run_id": "run-1",
        }
        with (
            patch("graph.factory.create_agent_tools", return_value=[_EngineListTool()]),
            patch("graph.factory.get_llm", return_value=_ClarificationLLM()),
        ):
            events = [
                event
                async for event in AgentFactory.run(
                    task_context=context,
                    skill_body="test",
                    allowed_tool_names=["engine.list"],
                    max_iterations=2,
                )
            ]

        self.assertEqual(
            [event.kind for event in events],
            ["tool_start", "checkpoint", "tool_result", "tool_start", "tool_result", "interaction_required"],
        )
        self.assertEqual(events[-1].payload["reason"], "workflow_engine_ambiguous")
        self.assertEqual(events[-1].payload["candidates"][0]["value"], 20)
        self.assertEqual(events[-1].payload["candidates"][0]["candidate"]["id"], 20)

    async def test_runtime_clarification_uses_observed_resource_fact(self):
        locator = "addp://engine/9/path/gischain/data/data.meta?type=object&item_id=216"
        responses = [
            _ScriptedResponse(
                tool_calls=[
                    {
                        "id": "search-observed",
                        "name": "data__search",
                        "args": {"query": "metadata", "limit": 10},
                    }
                ]
            ),
            _ScriptedResponse(
                tool_calls=[
                    {
                        "id": "clarify-observed",
                        "name": "request_clarification",
                        "args": {
                            "prompt": "请选择数据",
                            "reason": "data_source_ambiguous",
                            "options": [
                                {
                                    "label": "模型提供的标签不会成为事实",
                                    "value": locator,
                                    "candidate": {"locator": locator, "full_name": "错误名称"},
                                }
                            ],
                        },
                    }
                ]
            ),
        ]
        events = await self._run_workflow_events(
            tools=[_DocumentSearchTool()],
            responses=responses,
            allowed_tool_names=["data.search"],
        )

        interaction = next(event for event in events if event.kind == "interaction_required")
        option = interaction.payload["candidates"][0]
        self.assertEqual(option["value"], locator)
        self.assertEqual(option["label"], "gischain/data/data.meta")
        self.assertEqual(option["candidate"]["engine_id"], 9)
        self.assertEqual(option["candidate"]["full_name"], "gischain/data/data.meta")

    async def test_resumed_factory_uses_checkpoint_without_repeating_search(self):
        locator = "addp://engine/9/path/gischain/data/data.meta?type=object&item_id=216"
        checkpoint = new_checkpoint()
        capture_owner_facts(
            "data.search",
            {
                "results": [
                    {
                        "name": "data.meta",
                        "asset_type": "object",
                        "location": {
                            "locator": locator,
                            "engine_id": 9,
                            "full_name": "gischain/data/data.meta",
                        },
                    }
                ]
            },
            checkpoint,
        )
        response = _ScriptedResponse(
            tool_calls=[
                {
                    "id": "clarify-from-checkpoint",
                    "name": "request_clarification",
                    "args": {
                        "prompt": "请选择数据",
                        "reason": "data_source_ambiguous",
                        "options": [{"label": "data.meta", "value": locator, "candidate": {"locator": locator}}],
                    },
                }
            ]
        )
        context = {
            "skill_name": "workflow-analysis",
            "user_request": "继续",
            "context_summary": "",
            "user_id": 1,
            "tenant_id": 1,
            "token": "token",
            "agent_run_id": "run-1",
            "checkpoint": checkpoint,
        }
        with (
            patch("graph.factory.create_agent_tools", return_value=[]),
            patch("graph.factory.get_llm", return_value=_ScriptedLLM([response])),
        ):
            events = [
                event
                async for event in AgentFactory.run(
                    task_context=context,
                    skill_body="test",
                    allowed_tool_names=[],
                    max_iterations=1,
                )
            ]

        self.assertEqual(
            [event.kind for event in events],
            ["tool_start", "tool_result", "interaction_required"],
        )
        self.assertEqual(events[-1].payload["candidates"][0]["value"], locator)

    async def test_unobserved_locator_cannot_create_clarification(self):
        invented_locator = "addp://engine/8/path/public/dltb?type=table&item_id=61"
        responses = [
            _ScriptedResponse(
                tool_calls=[
                    {
                        "id": "search-1",
                        "name": "data__search",
                        "args": {"query": "耕地", "limit": 10},
                    }
                ]
            ),
            _ScriptedResponse(
                tool_calls=[
                    {
                        "id": "clarify-invented",
                        "name": "request_clarification",
                        "args": {
                            "prompt": "请选择耕地数据",
                            "reason": "data_source_ambiguous",
                            "options": [
                                {
                                    "label": "public.dltb",
                                    "value": invented_locator,
                                    "candidate": {"locator": invented_locator},
                                }
                            ],
                        },
                    }
                ]
            ),
            _ScriptedResponse(content="未找到可信候选"),
        ]
        events = await self._run_workflow_events(
            tools=[_DocumentSearchTool()],
            responses=responses,
            allowed_tool_names=["data.search"],
        )

        self.assertNotIn("interaction_required", [event.kind for event in events])
        clarification_results = [
            event.payload["content"]
            for event in events
            if event.kind == "tool_result" and event.payload["tool_name"] == "request_clarification"
        ]
        self.assertEqual(len(clarification_results), 1)
        self.assertIn("clarification_option_not_observed", clarification_results[0])
        self.assertIn(invented_locator, clarification_results[0])

    async def test_empty_invalid_workflow_does_not_emit_dag_presentation(self):
        context = {
            "skill_name": "workflow-analysis",
            "user_request": "分析铁路",
            "context_summary": "",
            "user_id": 1,
            "tenant_id": 1,
            "token": "token",
            "agent_run_id": "run-1",
        }
        with (
            patch("graph.factory.create_agent_tools", return_value=[_EmptyWorkflowTool()]),
            patch("graph.factory.get_llm", return_value=_EmptyWorkflowLLM()),
        ):
            events = [
                event
                async for event in AgentFactory.run(
                    task_context=context,
                    skill_body="test",
                    allowed_tool_names=["workflow.draft.generate"],
                    max_iterations=2,
                )
            ]

        self.assertNotIn("presentation", [event.kind for event in events])

    async def test_successful_draft_without_validation_does_not_emit_dag_presentation(self):
        events = await self._run_workflow_events(
            tools=[_SuccessfulDraftTool()],
            responses=[
                _ScriptedResponse(tool_calls=[_draft_call()]),
                _ScriptedResponse(content="候选工作流尚未校验"),
            ],
            allowed_tool_names=["workflow.draft.generate"],
        )

        self.assertNotIn("presentation", [event.kind for event in events])

    async def test_failed_validation_does_not_emit_dag_presentation(self):
        events = await self._run_workflow_events(
            tools=[_SuccessfulDraftTool(), _ValidationTool(valid=False)],
            responses=[
                _ScriptedResponse(tool_calls=[_draft_call()]),
                _ScriptedResponse(tool_calls=[_validation_call()]),
                _ScriptedResponse(content="工作流校验失败"),
            ],
            allowed_tool_names=["workflow.draft.generate", "workflow.validate"],
        )

        self.assertNotIn("presentation", [event.kind for event in events])

    async def test_successful_validation_emits_validated_dag_once(self):
        events = await self._run_workflow_events(
            tools=[_SuccessfulDraftTool(), _ValidationTool(valid=True)],
            responses=[
                _ScriptedResponse(tool_calls=[_draft_call()]),
                _ScriptedResponse(tool_calls=[_validation_call()]),
                _ScriptedResponse(content="工作流已通过校验"),
            ],
            allowed_tool_names=["workflow.draft.generate", "workflow.validate"],
        )

        presentations = [event for event in events if event.kind == "presentation"]
        self.assertEqual(len(presentations), 1)
        self.assertEqual(presentations[0].payload["kind"], "workflow_dag")
        self.assertEqual(presentations[0].payload["workflow"], _WORKFLOW_DEFINITION)

    async def test_data_preview_emits_table_then_map_presentations(self):
        events = await self._run_workflow_events(
            tools=[_PreviewTool()],
            responses=[
                _ScriptedResponse(
                    tool_calls=[
                        {
                            "id": "preview-1",
                            "name": "data__preview",
                            "args": {
                                "locator": "addp://engine/8/path/public/railway?type=table&item_id=60",
                                "limit": 10,
                            },
                        }
                    ]
                ),
                _ScriptedResponse(content="预览完成"),
            ],
            allowed_tool_names=["data.preview"],
        )

        presentations = [event.payload["kind"] for event in events if event.kind == "presentation"]
        self.assertEqual(presentations, ["table_preview", "map_view"])

    async def test_tool_exception_emits_error_result_for_failed_step_audit(self):
        events = await self._run_workflow_events(
            tools=[_FailingTool()],
            responses=[
                _ScriptedResponse(
                    tool_calls=[
                        {
                            "id": "search-failed",
                            "name": "data__search",
                            "args": {"query": "railway", "limit": 10},
                        }
                    ]
                ),
                _ScriptedResponse(content="数据检索失败"),
            ],
            allowed_tool_names=["data.search"],
        )

        tool_result = next(event for event in events if event.kind == "tool_result")
        self.assertTrue(tool_result.payload["is_error"])
        self.assertEqual(tool_result.payload["tool_name"], "data.search")
        self.assertEqual(tool_result.payload["error_source"], "runtime")
        self.assertEqual(tool_result.payload["error_code"], "tool_adapter_exception")
        self.assertIn("owner service unavailable", tool_result.payload["content"])

    async def test_owner_error_envelope_is_attributed_to_owner(self):
        events = await self._run_workflow_events(
            tools=[_OwnerErrorTool()],
            responses=[
                _ScriptedResponse(
                    tool_calls=[
                        {
                            "id": "search-owner-failed",
                            "name": "data__search",
                            "args": {"query": "railway", "limit": 10},
                        }
                    ]
                ),
                _ScriptedResponse(content="owner 服务不可用"),
            ],
            allowed_tool_names=["data.search"],
        )

        tool_result = next(event for event in events if event.kind == "tool_result")
        self.assertTrue(tool_result.payload["is_error"])
        self.assertEqual(tool_result.payload["error_source"], "owner")
        self.assertEqual(tool_result.payload["error_code"], "owner_api_unavailable")

    async def test_workflow_approval_pauses_run_with_owner_projection(self):
        events = await self._run_workflow_events(
            tools=[_WorkflowApprovalTool()],
            responses=[
                _ScriptedResponse(
                    tool_calls=[{
                        "id": "workflow-run-1",
                        "name": "workflow__run",
                        "args": {
                            "workflow_engine_id": 20,
                            "workflow_definition": _WORKFLOW_DEFINITION,
                        },
                    }]
                )
            ],
            allowed_tool_names=["workflow.run"],
        )

        self.assertEqual([event.kind for event in events], ["tool_start", "tool_result", "interaction_required"])
        interaction = events[-1].payload
        self.assertEqual(interaction["interaction_kind"], "owner_approval")
        self.assertEqual(interaction["owner"], "develop")
        self.assertEqual(interaction["request_summary"]["task_count"], 2)
        self.assertNotIn("workflow_definition", interaction)

    async def _run_workflow_events(self, *, tools, responses, allowed_tool_names):
        context = {
            "skill_name": "workflow-analysis",
            "user_request": "分析铁路",
            "context_summary": "",
            "user_id": 1,
            "tenant_id": 1,
            "token": "token",
            "agent_run_id": "run-1",
        }
        with (
            patch("graph.factory.create_agent_tools", return_value=tools),
            patch("graph.factory.get_llm", return_value=_ScriptedLLM(responses)),
        ):
            return [
                event
                async for event in AgentFactory.run(
                    task_context=context,
                    skill_body="test",
                    allowed_tool_names=allowed_tool_names,
                    max_iterations=len(responses),
                )
            ]


if __name__ == "__main__":
    unittest.main()
