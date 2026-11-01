"""脚本化测试数据的 LangChain 协议适配；执行使用真实 DeerFlow Harness。"""

from typing import Any

from langchain_core.language_models.chat_models import BaseChatModel
from langchain_core.messages import AIMessage, AIMessageChunk
from langchain_core.outputs import ChatGeneration, ChatGenerationChunk, ChatResult
from langchain_core.tools import StructuredTool


class HarnessTestModel(BaseChatModel):
    source: Any
    answer_response: Any = None

    @property
    def _llm_type(self):
        return "scripted-harness-test"

    def bind_tools(self, tools, **kwargs):
        return self

    def _generate(self, *args, **kwargs):
        raise AssertionError("synchronous model invocation is forbidden")

    async def _agenerate(self, messages, **kwargs):
        response = await self.source.ainvoke(messages)
        if not response.tool_calls:
            self.answer_response = response
            return ChatResult(generations=[ChatGeneration(message=AIMessage(content='', tool_calls=[
                {'id': 'fixture-answer-ready', 'name': 'answer_ready', 'args': {}, 'type': 'tool_call'}]))])
        message = AIMessage(content=response.content, tool_calls=response.tool_calls)
        return ChatResult(generations=[ChatGeneration(message=message)])

    async def _astream(self, messages, **kwargs):
        if self.answer_response is None:
            raise AssertionError('answer stream was not admitted')
        yield ChatGenerationChunk(message=AIMessageChunk(content=self.answer_response.content))


def harness_tools(fixtures):
    def convert(fixture):
        async def call(**arguments):
            return await fixture.ainvoke(arguments)
        return StructuredTool.from_function(
            name=fixture.name, description="controlled owner response",
            args_schema={"type": "object", "properties": {}, "additionalProperties": True},
            coroutine=call, metadata=fixture.metadata,
        )
    return [convert(fixture) for fixture in fixtures]
