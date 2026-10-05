"""每次请求构建进程内 DeerFlow Harness；ADDP 保留授权、事件和语义暂停边界。"""

import asyncio
import copy
import json
import logging
from typing import Any, AsyncIterator

from deerflow.agents import create_deerflow_agent
from deerflow.agents.middlewares.dangling_tool_call_middleware import DanglingToolCallMiddleware
from deerflow.agents.middlewares.loop_detection_middleware import LoopDetectionMiddleware
from deerflow.agents.middlewares.system_message_coalescing_middleware import SystemMessageCoalescingMiddleware
from langchain.agents.middleware import AgentMiddleware, hook_config
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from langchain_core.tools import StructuredTool
from langgraph.config import get_stream_writer
from langgraph.graph import END
from langgraph.types import Command
from pydantic import BaseModel, Field, ValidationError

from agents.checkpoint import canonicalize_clarification_options, capture_owner_facts, checkpoint_prompt, normalize_checkpoint
from agents.events import AgentEvent, text_event
from agents.result_refs import build_result_ref
from graph.state import TaskContext
from protocol.a2ui import preview_presentations
from tools.langchain_tools import create_agent_tools, stable_tool_name
from utils.llm import get_llm

logger = logging.getLogger("agent.factory")


class ClarificationOption(BaseModel):
    label: str = Field(description="展示给用户的选项名称")
    value: str | int = Field(description="恢复运行时使用的稳定值")
    candidate: dict[str, Any] | None = Field(default=None, description="owner API 候选事实")


class ClarificationRequest(BaseModel):
    prompt: str = Field(description="需要用户回答的单个明确问题")
    reason: str = Field(description="稳定澄清原因")
    options: list[ClarificationOption] = Field(min_length=1, description="基于 owner 事实的候选选项")


async def _request_clarification(**_: Any) -> str:
    """仅声明模型输入 Schema；中间件校验并转换为 ADDP Interaction。"""
    raise RuntimeError("clarification_middleware_required")


def _clarification_tool() -> StructuredTool:
    return StructuredTool.from_function(
        coroutine=_request_clarification, name="request_clarification",
        description="缺少可信候选、口径或写入授权时，提出一个基于已观察事实的问题并暂停当前 run。",
        args_schema=ClarificationRequest,
    )


def _runtime_instructions() -> str:
    return """## ADDP Agent Runtime 交互约束

- `request_clarification` 是 Runtime 提供的暂停控制能力，不是 ADDP 平台 Tool。
- 只要 Skill 的“必须澄清”条件成立，就调用 `request_clarification`，不要用普通文本列出问题后结束 run。
- 每次只询问当前阻塞流程的一个问题；恢复后再处理下一个待确认事项。
- options 必须来自 Tool 返回的候选或明确的口径选项，不得虚构 locator、引擎或字段事实。
- AgentCheckpoint 中的 observed 事实可以直接复用，除非事实缺失或本次操作明确要求刷新；confirmed 选择视为用户已经完成，不得重复澄清。
- `workflow.run` 返回 `approval_required` 时当前 run 必须暂停，不能重试或把客户端确认当作批准。
- 恢复消息提供 approval_id 和 request_fingerprint 时，再次调用 `workflow.run` 且只提交这两个字段。
- `data.search` 的服务、委托或响应失败终止当前运行，不是零召回，不能重复搜索或切换目录枚举、样本读取、扫描；不解除保护隔离。
"""


def _emit(event_kind: str, **payload: Any) -> None:
    get_stream_writer()(AgentEvent(kind=event_kind, payload=payload))


class _PlatformBoundary(AgentMiddleware):
    """平台边界中间件，不实现模型／工具执行循环，不持久化框架状态。"""

    def __init__(self, tools: list[StructuredTool], checkpoint: dict, max_iterations: int):
        super().__init__()
        self.names = {tool.name: stable_tool_name(tool) for tool in tools}
        self.checkpoint = checkpoint
        self.max_iterations = max_iterations
        self.model_calls = 0
        self.paused = False
        # LangGraph 默认并发调度同批工具；owner 暂停必须先于后续副作用。
        self.tool_lock = asyncio.Lock()

    @hook_config(can_jump_to=["end"])
    async def abefore_model(self, state, runtime):
        if self.paused:
            return {"jump_to": "end"}
        if self.model_calls >= self.max_iterations:
            raise RuntimeError("agent_iteration_limit_reached")
        self.model_calls += 1

    async def aafter_model(self, state, runtime):
        message = state["messages"][-1]
        if not isinstance(message, AIMessage):
            return
        clarifications = [call for call in message.tool_calls if call["name"] == "request_clarification"]
        if clarifications:
            # 澄清优先，即使业务写入排在批次前面也不得执行。
            return {"messages": [message.model_copy(update={"tool_calls": clarifications[:1]})]}
        if not message.tool_calls and message.content:
            get_stream_writer()(text_event(message.text))

    async def awrap_tool_call(self, request, handler):
        async with self.tool_lock:
            call = request.tool_call
            if self.paused:
                return Command(update={"messages": [ToolMessage(
                    content="当前运行已暂停，未执行此调用", tool_call_id=call["id"],
                )]}, goto=END)
            name = self.names.get(call["name"], call["name"])
            _emit("tool_start", tool_call_id=call["id"], tool_name=name, args=call["args"])
            if name == "request_clarification":
                return self._clarify(call)
            if call["name"] not in self.names:
                return self._error(call, name, "tool_not_allowed", "错误：工具不在该 Skill 的白名单内")
            try:
                message = await handler(request)
            except Exception as exc:
                # 异常可能包含连接地址／凭据，只记录异常类型。
                logger.error("Tool adapter failed: %s error_type=%s", name, type(exc).__name__)
                return self._error(call, name, "tool_adapter_exception", "工具执行失败")
            if not isinstance(message, ToolMessage):
                raise RuntimeError("invalid_tool_adapter_result")
            if message.status == "error":
                return self._error(call, name, "invalid_tool_arguments", "工具参数不符合 Schema")
            approval = self._owner_result(call, name, message.content)
            if isinstance(approval, Command):
                return approval
            if approval is not None:
                self.paused = True
                _emit("interaction_required", tool_call_id=call["id"], **approval)
                return Command(update={"messages": [message]}, goto=END)
            return message

    def _error(self, call, name, code, message):
        content = json.dumps({"error": {"code": code, "message": message}}, ensure_ascii=False)
        _emit("tool_result", tool_call_id=call["id"], tool_name=name, content=content,
              is_error=True, error_source="runtime", error_code=code)
        halted = self._halt_failed_search(call, name, "runtime", code, message, content)
        if halted is not None:
            return halted
        return ToolMessage(content=content, tool_call_id=call["id"], name=call["name"], status="error")

    def _halt_failed_search(self, call, name, source, code, message, content):
        if name != "data.search" or not code or code in {"invalid_arguments", "invalid_tool_arguments", "tool_not_allowed"}:
            return None
        self.paused = True
        _emit("run_failed", error_source=source, error_code=code, message=str(message)[:1000])
        return Command(update={"messages": [ToolMessage(
            content=content, tool_call_id=call["id"], name=call["name"], status="error",
        )]}, goto=END)

    def _clarify(self, call):
        try:
            args = ClarificationRequest.model_validate(call["args"])
            options = canonicalize_clarification_options(
                args.reason, [option.model_dump() for option in args.options], self.checkpoint,
            )
        except ValidationError:
            return self._error(call, "request_clarification", "invalid_clarification_request", "澄清请求结构无效")
        except ValueError as exc:
            return self._error(call, "request_clarification", "clarification_option_not_observed", str(exc))
        self.paused = True
        content = "等待用户完成澄清"
        _emit("tool_result", tool_call_id=call["id"], tool_name="request_clarification", content=content, is_error=False)
        _emit("interaction_required", tool_call_id=call["id"], prompt=args.prompt, reason=args.reason, candidates=options)
        return Command(update={"messages": [ToolMessage(content=content, tool_call_id=call["id"])]}, goto=END)

    def _owner_result(self, call, name, content):
        if not isinstance(content, str):
            raise RuntimeError("invalid_tool_adapter_result")
        try:
            result = json.loads(content)
        except json.JSONDecodeError:
            result = None
        error = result.get("error") if isinstance(result, dict) else None
        code = str(error.get("code") or "tool_error") if isinstance(error, dict) else None
        source = None
        if code:
            source = "owner" if code.startswith("approval_") or code in {
                "owner_api_error", "owner_api_unavailable", "invalid_owner_response", "manager_search_isolated",
            } else "tool"
        if result is not None:
            facts = capture_owner_facts(name, result, self.checkpoint)
            if facts:
                _emit("checkpoint", tool_call_id=call["id"], checkpoint=copy.deepcopy(self.checkpoint), facts=facts)
            result_ref = build_result_ref(name, result)
            if result_ref is not None:
                _emit("result_ref", result_ref=result_ref)
            if name == "data.preview":
                for presentation in preview_presentations(result):
                    get_stream_writer()(AgentEvent(kind="presentation", payload=presentation))
            if name == "workflow.validate" and isinstance(result, dict) and result.get("valid") is True:
                definition = call["args"].get("workflow_definition") or {}
                if isinstance(definition.get("tasks"), list) and definition["tasks"]:
                    _emit("presentation", kind="workflow_dag", workflow=definition)
        _emit("tool_result", tool_call_id=call["id"], tool_name=name, content=content,
              is_error=code is not None, error_source=source, error_code=code)
        halted = self._halt_failed_search(
            call, name, source, code, error.get("message", code) if isinstance(error, dict) else None, content,
        )
        if halted is not None:
            return halted
        if name == "workflow.run" and isinstance(result, dict) and result.get("status") == "approval_required":
            return {
                "interaction_kind": "owner_approval", "owner": "develop",
                "owner_interaction_id": result.get("interaction_id"), "open_url": result.get("open_url"),
                "request_fingerprint": result.get("request_fingerprint"),
                "request_summary": result.get("request_summary") or {}, "expires_at": result.get("expires_at"),
            }


class AgentFactory:
    @staticmethod
    async def run(
        task_context: TaskContext, skill_body: str, allowed_tool_names: list[str],
        max_iterations: int = 5, llm: Any | None = None,
    ) -> AsyncIterator[AgentEvent]:
        if max_iterations < 1:
            raise ValueError("invalid_agent_iteration_limit")
        checkpoint = normalize_checkpoint(task_context.get("checkpoint"))
        human_input = task_context["user_request"]
        if task_context["context_summary"]:
            human_input = f"对话背景：\n{task_context['context_summary']}\n\n用户请求：{human_input}"
        persisted = checkpoint_prompt(checkpoint)
        if persisted:
            human_input = f"{human_input}\n\n{persisted}"
        tool_map = {stable_tool_name(tool): tool for tool in create_agent_tools(
            task_context["token"], task_context["agent_run_id"],
        )}
        if set(allowed_tool_names) - tool_map.keys():
            raise ValueError("agent_skill_tool_not_found")
        tools = [tool_map[name] for name in allowed_tool_names] + [_clarification_tool()]
        boundary = _PlatformBoundary(tools, checkpoint, max_iterations)
        loop_detection = LoopDetectionMiddleware()
        graph = create_deerflow_agent(
            model=llm or get_llm(task_context["tenant_id"], "reasoning"), tools=tools,
            system_prompt=f"{skill_body}\n\n{_runtime_instructions()}",
            middleware=[DanglingToolCallMiddleware(), SystemMessageCoalescingMiddleware(), loop_detection, boundary],
            checkpointer=None, name=task_context["skill_name"],
        )
        # 业务 UUID 不是框架 invocation ID；configurable.run_id 会触发重放语义。
        context = {"run_id": task_context["agent_run_id"], "tenant_id": task_context["tenant_id"]}
        async for event in graph.astream(
            {"messages": [HumanMessage(content=human_input)]},
            config={"recursion_limit": max_iterations * 8 + 16}, context=context, stream_mode="custom",
        ):
            yield event
        if loop_detection.consume_stop_reason(task_context["agent_run_id"]):
            raise RuntimeError("agent_loop_detected")
