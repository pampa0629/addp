"""LangChain ChatModel adapter for the ADDP Inference Runtime contract."""

from __future__ import annotations

from collections.abc import Awaitable, Callable, Sequence
from typing import Any

from langchain_core.callbacks import AsyncCallbackManagerForLLMRun, CallbackManagerForLLMRun
from langchain_core.language_models.chat_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage, ToolMessage
from langchain_core.outputs import ChatGeneration, ChatResult
from langchain_core.runnables import Runnable
from langchain_core.tools import BaseTool
from langchain_core.utils.function_calling import convert_to_openai_tool
from pydantic import ConfigDict, Field

from .client.inference import InferenceClient, Message, ResponseSchema, ToolCall, ToolDefinition


class InferenceChatModel(BaseChatModel):
    model_config = ConfigDict(arbitrary_types_allowed=True)

    client: InferenceClient = Field(exclude=True)
    tenant_id: int
    model_profile_id: str = ""
    profile_resolver: Callable[[], Awaitable[str]] | None = Field(default=None, exclude=True)
    temperature: float | None = None
    max_output_tokens: int = 0

    @property
    def _llm_type(self) -> str:
        return "addp-inference-runtime"

    @property
    def _identifying_params(self) -> dict[str, Any]:
        return {"model_profile_id": self.model_profile_id or "owner-resolved"}

    def _generate(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: CallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> ChatResult:
        raise RuntimeError("ADDP Inference ChatModel only supports asynchronous invocation")

    async def _agenerate(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: AsyncCallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> ChatResult:
        if stop:
            raise ValueError("Inference Runtime chat contract does not accept caller-defined stop sequences")
        profile_id = self.model_profile_id
        if not profile_id and self.profile_resolver is not None:
            profile_id = await self.profile_resolver()
        if not profile_id:
            raise ValueError("Inference ChatModel requires a resolved Model Profile ID")

        tools = kwargs.get("tools")
        response_schema = kwargs.get("response_schema")
        response = await self.client.chat(
            tenant_id=self.tenant_id,
            model_profile_id=profile_id,
            messages=[_to_inference_message(message) for message in messages],
            tools=tools,
            tool_choice=kwargs.get("tool_choice"),
            response_schema=response_schema,
            temperature=kwargs.get("temperature", self.temperature),
            max_output_tokens=kwargs.get(
                "max_tokens",
                kwargs.get("max_output_tokens", self.max_output_tokens),
            ),
        )
        ai_message = AIMessage(
            content=response.message.content,
            tool_calls=[
                {"id": call.id, "name": call.name, "args": call.arguments, "type": "tool_call"}
                for call in response.message.tool_calls
            ],
            response_metadata={
                "deployment_id": response.deployment_id,
                "profile_version": response.profile_version,
            },
            usage_metadata={
                "input_tokens": response.usage.input_tokens,
                "output_tokens": response.usage.output_tokens,
                "total_tokens": response.usage.total_tokens,
            },
        )
        return ChatResult(
            generations=[ChatGeneration(message=ai_message)],
            llm_output={
                "deployment_id": response.deployment_id,
                "profile_version": response.profile_version,
                "usage": response.usage.model_dump(),
            },
        )

    def bind_tools(
        self,
        tools: Sequence[dict[str, Any] | type | Callable[..., Any] | BaseTool],
        *,
        tool_choice: str | bool | dict[str, Any] | None = None,
        **kwargs: Any,
    ) -> Runnable:
        definitions: list[ToolDefinition] = []
        for tool in tools:
            converted = convert_to_openai_tool(tool)
            function = converted.get("function") or {}
            definitions.append(
                ToolDefinition(
                    name=str(function.get("name") or ""),
                    description=str(function.get("description") or ""),
                    parameters=function.get("parameters") or {"type": "object", "properties": {}},
                )
            )
        return self.bind(
            tools=definitions,
            tool_choice=_normalize_tool_choice(tool_choice),
            **kwargs,
        )

    def with_response_schema(self, schema: ResponseSchema) -> Runnable:
        return self.bind(response_schema=schema)


def _normalize_tool_choice(value: str | bool | dict[str, Any] | None) -> str:
    if isinstance(value, dict):
        raise ValueError("Inference Runtime does not accept provider-specific named tool choice")
    if value in {True, "any", "required"}:
        return "required"
    if value in {False, "none"}:
        return "none"
    if value is None or value == "auto":
        return "auto"
    raise ValueError("Inference Runtime only supports auto, none, or required tool choice")


def _to_inference_message(message: BaseMessage) -> Message:
    if not isinstance(message.content, str):
        raise ValueError("Inference Runtime ChatModel only supports text message content")
    if isinstance(message, ToolMessage):
        return Message(role="tool", content=message.content, tool_call_id=message.tool_call_id)
    role = {"human": "user", "ai": "assistant", "system": "system"}.get(message.type)
    if role is None:
        raise ValueError(f"unsupported LangChain message type: {message.type}")
    calls = []
    if isinstance(message, AIMessage):
        calls = [
            ToolCall(id=str(call["id"]), name=str(call["name"]), arguments=dict(call.get("args") or {}))
            for call in message.tool_calls
        ]
    return Message(role=role, content=message.content, tool_calls=calls)
