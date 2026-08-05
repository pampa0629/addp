import asyncio
import unittest
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from addp_common.client import ChatResponse, Message, ToolCall
from addp_common.client.inference import Usage
from addp_common.inference_langchain import InferenceChatModel
from langchain_core.messages import HumanMessage
from pydantic import BaseModel

from utils.llm import _BindingSnapshot


class Route(BaseModel):
    skill: str | None = None
    direct_reply: str | None = None


class FakeInferenceClient:
    def __init__(self, response: ChatResponse):
        self.response = response
        self.calls = []

    async def chat(self, **kwargs):
        self.calls.append(kwargs)
        return self.response


class InferenceLLMTests(unittest.IsolatedAsyncioTestCase):
    async def test_langchain_adapter_normalizes_tool_call(self):
        client = FakeInferenceClient(ChatResponse(
        schema_version="addp.inference/v1",
        message=Message(
            role="assistant",
            tool_calls=[ToolCall(id="call-1", name="lookup", arguments={"query": "railway"})],
        ),
        usage=Usage(input_tokens=4, output_tokens=2, total_tokens=6),
        deployment_id="deployment-1",
        profile_version=3,
        ))
        model = InferenceChatModel.model_construct(
            client=client,
            tenant_id=7,
            model_profile_id="profile-1",
            profile_resolver=None,
            temperature=None,
            max_output_tokens=0,
        )

        response = await model.bind_tools([{
            "name": "lookup",
            "description": "Lookup data",
            "parameters": {"type": "object", "properties": {"query": {"type": "string"}}},
        }]).ainvoke([HumanMessage(content="find railway")])

        self.assertEqual(response.tool_calls, [{
            "id": "call-1",
            "name": "lookup",
            "args": {"query": "railway"},
            "type": "tool_call",
        }])
        self.assertEqual(client.calls[0]["tool_choice"], "auto")
        self.assertEqual(client.calls[0]["tools"][0].name, "lookup")


    async def test_langchain_adapter_supports_structured_output(self):
        client = FakeInferenceClient(ChatResponse(
        schema_version="addp.inference/v1",
        message=Message(
            role="assistant",
            tool_calls=[ToolCall(
                id="call-route",
                name="Route",
                arguments={"skill": "workflow-analysis", "direct_reply": None},
            )],
        ),
        usage=Usage(),
        deployment_id="deployment-1",
        profile_version=1,
        ))
        model = InferenceChatModel.model_construct(
            client=client,
            tenant_id=7,
            model_profile_id="profile-1",
            profile_resolver=None,
            temperature=None,
            max_output_tokens=0,
        )

        result = await model.with_structured_output(Route).ainvoke([HumanMessage(content="build workflow")])

        self.assertEqual(result, Route(skill="workflow-analysis"))
        self.assertEqual(client.calls[0]["tool_choice"], "required")


    async def test_binding_snapshot_resolves_profile_once_under_concurrency(self):
        binding = SimpleNamespace(model_profile_id="profile-snapshot")
        repository = SimpleNamespace(resolve=AsyncMock(return_value=binding))

        class SessionContext:
            async def __aenter__(self):
                return object()

            async def __aexit__(self, *_args):
                return None

        snapshot = _BindingSnapshot(tenant_id=7, scenario_code="reasoning")
        with (
            patch("utils.llm.AsyncSessionLocal", return_value=SessionContext()),
            patch("utils.llm.InferenceScenarioBindingRepository", return_value=repository),
        ):
            values = await asyncio.gather(snapshot.resolve(), snapshot.resolve(), snapshot.resolve())

        self.assertEqual(values, ["profile-snapshot"] * 3)
        repository.resolve.assert_awaited_once_with(7, "reasoning")
