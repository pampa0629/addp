"""
AgentFactory：领域 Agent 动态构建与执行

根据 Skill `agents/addp.yaml` 中的稳定 Tool 白名单和 max_iterations
动态为每次请求构建隔离的领域 Agent，只注入该 Skill 声明的工具子集。

核心优势（相比旧 _run_skill_agent）：
- 工具隔离：领域 Agent 只能调用白名单内的工具，不暴露无关工具
- 可配置迭代次数：每个 Skill 可声明自己的 max_iterations
- 标准化接口：统一通过 TaskContext 传入，便于未来扩展为真正的 LangGraph 子图
"""
import json
import logging
from typing import AsyncIterator, List, Any

from langchain_core.messages import HumanMessage, SystemMessage, ToolMessage
from langchain_core.tools import StructuredTool
from pydantic import BaseModel, Field

from graph.state import TaskContext
from agents.checkpoint import (
    canonicalize_clarification_options,
    capture_owner_facts,
    checkpoint_prompt,
    normalize_checkpoint,
)
from agents.events import AgentEvent, text_event
from agents.result_refs import build_result_ref
from protocol.a2ui import preview_presentations
from tools.langchain_tools import create_agent_tools, stable_tool_name
from utils.llm import get_llm

logger = logging.getLogger("agent.factory")

# ToolMessage 截断上限（与 main_agent 保持一致）
_TOOL_RESULT_MAX_LEN = 3000


class ClarificationOption(BaseModel):
    label: str = Field(description="展示给用户的选项名称")
    value: str | int = Field(description="恢复运行时使用的稳定值")
    candidate: dict[str, Any] | None = Field(
        default=None,
        description="可选的 owner API 候选事实，例如 locator、引擎类型或空间元数据",
    )


class ClarificationRequest(BaseModel):
    prompt: str = Field(description="需要用户回答的单个明确问题")
    reason: str = Field(description="稳定原因，例如 workflow_engine_ambiguous 或 data_source_ambiguous")
    options: list[ClarificationOption] = Field(
        min_length=1,
        description="可操作的候选选项；不得虚构 owner API 未返回的事实",
    )


async def _request_clarification(**_: Any) -> str:
    """占位 coroutine；AgentFactory 在调用前将其转换为持久 Interaction。"""
    return "等待用户完成澄清"


def _clarification_tool() -> StructuredTool:
    return StructuredTool.from_function(
        coroutine=_request_clarification,
        name="request_clarification",
        description=(
            "当继续分析所需的运行时、数据候选、CRS、单位、统计口径或写入授权不明确时调用。"
            "必须传入一个明确问题和基于已知事实构造的可选项；调用后当前 run 会暂停。"
        ),
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
"""


class AgentFactory:
    """
    领域 Agent 工厂。

    使用方式：
        async for msg_type, content in AgentFactory.run(task_context, skill_meta):
            ...

    不预建 Agent 实例，每次请求按需构建、执行完即销毁。
    """

    @staticmethod
    async def run(
        task_context: TaskContext,
        skill_body: str,
        allowed_tool_names: List[str],
        max_iterations: int = 5,
    ) -> AsyncIterator[AgentEvent]:
        """
        动态构建并执行领域 Agent（ReAct 循环）。

        参数：
            task_context:       主 Agent 打包的任务上下文
            skill_body:         SKILL.md 正文（作为 system prompt）
            allowed_tool_names: 该 Skill 声明的工具白名单
            max_iterations:     最大 ReAct 迭代次数

        产出结构化 AgentEvent，由 API 层映射为 AG-UI 事件。
        """
        skill_name = task_context["skill_name"]
        user_request = task_context["user_request"]
        context_summary = task_context["context_summary"]
        token = task_context["token"]
        agent_run_id = task_context["agent_run_id"]
        checkpoint = normalize_checkpoint(task_context.get("checkpoint"))

        # 构建用户输入（注入对话背景）
        if context_summary:
            human_input = f"对话背景：\n{context_summary}\n\n用户请求：{user_request}"
        else:
            human_input = user_request
        persisted_context = checkpoint_prompt(checkpoint)
        if persisted_context:
            human_input = f"{human_input}\n\n{persisted_context}"

        # 只创建白名单内的工具（隔离）
        all_tools = create_agent_tools(token, agent_run_id)
        all_tool_map = {stable_tool_name(tool): tool for tool in all_tools}
        platform_tools = [all_tool_map[name] for name in allowed_tool_names if name in all_tool_map]
        tools = [*platform_tools, _clarification_tool()]
        # 执行时也只允许白名单内的工具，防止 LLM 调用未授权工具
        tool_map = {t.name: t for t in tools}
        runtime_to_stable = {tool.name: stable_tool_name(tool) for tool in tools}

        missing = [name for name in allowed_tool_names if name not in all_tool_map]
        if missing:
            logger.warning("[FACTORY:%s] 白名单中的工具未找到: %s", skill_name, missing)

        llm_with_tools = get_llm(streaming=False).bind_tools(tools)

        # 领域 Agent 的独立消息栈（用完即销毁，不污染主 Agent 历史）
        lc_messages = [
            SystemMessage(content=f"{skill_body}\n\n{_runtime_instructions()}"),
            HumanMessage(content=human_input),
        ]

        logger.info(
            "[FACTORY:%s] 启动 | 工具: %s | max_iter: %d",
            skill_name, [t.name for t in tools], max_iterations,
        )

        # ReAct 循环
        final_response = None
        for iteration in range(max_iterations):
            response = await llm_with_tools.ainvoke(lc_messages)

            if not response.tool_calls:
                logger.info("[FACTORY:%s] ReAct 完成（第 %d 轮）", skill_name, iteration + 1)
                final_response = response
                break

            lc_messages.append(response)

            for tc in response.tool_calls:
                tool_name = tc["name"]
                stable_name = runtime_to_stable.get(tool_name, tool_name)
                tool_args = tc["args"]
                logger.info("[FACTORY:%s] 执行工具: %s | args: %s", skill_name, stable_name, tool_args)

                yield AgentEvent(
                    kind="tool_start",
                    payload={
                        "tool_call_id": tc["id"],
                        "tool_name": stable_name,
                        "args": tool_args,
                    },
                )

                if stable_name == "request_clarification":
                    try:
                        options = canonicalize_clarification_options(
                            str(tool_args.get("reason") or "missing_input"),
                            tool_args.get("options") or [],
                            checkpoint,
                        )
                    except ValueError as exc:
                        tool_result = json.dumps(
                            {
                                "error": {
                                    "code": "clarification_option_not_observed",
                                    "message": str(exc),
                                }
                            },
                            ensure_ascii=False,
                        )
                        yield AgentEvent(
                            kind="tool_result",
                            payload={
                                "tool_call_id": tc["id"],
                                "tool_name": stable_name,
                                "content": tool_result,
                                "is_error": True,
                                "error_source": "runtime",
                                "error_code": "clarification_option_not_observed",
                            },
                        )
                        lc_messages.append(ToolMessage(content=tool_result, tool_call_id=tc["id"]))
                        logger.warning("[FACTORY:%s] 拒绝未经 Tool 观察的澄清选项: %s", skill_name, exc)
                        continue
                    yield AgentEvent(
                        kind="tool_result",
                        payload={
                            "tool_call_id": tc["id"],
                            "tool_name": stable_name,
                            "content": "等待用户完成澄清",
                            "is_error": False,
                        },
                    )
                    yield AgentEvent(
                        kind="interaction_required",
                        payload={
                            "tool_call_id": tc["id"],
                            "prompt": tool_args.get("prompt") or "请选择一个选项",
                            "reason": tool_args.get("reason") or "missing_input",
                            "candidates": options,
                        },
                    )
                    return

                tool_fn = tool_map.get(tool_name)
                tool_failed = False
                tool_error_source = None
                tool_error_code = None
                owner_approval_request = None
                if tool_fn is None:
                    tool_result = f"错误：工具 `{tool_name}` 不在该 Skill 的白名单内"
                    tool_failed = True
                    tool_error_source = "runtime"
                    tool_error_code = "tool_not_allowed"
                    logger.error("[FACTORY:%s] 工具不在白名单: %s", skill_name, tool_name)
                else:
                    try:
                        raw = await tool_fn.ainvoke({
                            "name": tool_fn.name,
                            "args": tool_args,
                            "id": tc["id"],
                            "type": "tool_call",
                        })
                        tool_result = str(getattr(raw, "content", raw))
                        preview = tool_result[:200] + ("..." if len(tool_result) > 200 else "")
                        logger.info("[FACTORY:%s] 工具结果（前200字符）: %s", skill_name, preview)

                        try:
                            parsed_result = json.loads(tool_result)
                        except (TypeError, json.JSONDecodeError):
                            parsed_result = None
                        if parsed_result is not None:
                            if isinstance(parsed_result, dict) and isinstance(parsed_result.get("error"), dict):
                                tool_failed = True
                                tool_error_code = str(parsed_result["error"].get("code") or "tool_error")
                                tool_error_source = (
                                    "owner"
                                    if tool_error_code.startswith("approval_")
                                    or tool_error_code in {
                                        "owner_api_error",
                                        "owner_api_unavailable",
                                        "invalid_owner_response",
                                    }
                                    else "tool"
                                )
                            facts = capture_owner_facts(stable_name, parsed_result, checkpoint)
                            if facts:
                                yield AgentEvent(
                                    kind="checkpoint",
                                    payload={
                                        "tool_call_id": tc["id"],
                                        "checkpoint": checkpoint,
                                        "facts": facts,
                                    },
                                )
                            result_ref = build_result_ref(stable_name, parsed_result)
                            if result_ref is not None:
                                yield AgentEvent(
                                    kind="result_ref",
                                    payload={"result_ref": result_ref},
                                )
                            if stable_name == "data.preview":
                                for presentation in preview_presentations(parsed_result):
                                    yield AgentEvent(kind="presentation", payload=presentation)
                            if (
                                stable_name == "workflow.run"
                                and isinstance(parsed_result, dict)
                                and parsed_result.get("status") == "approval_required"
                            ):
                                owner_approval_request = {
                                    "interaction_kind": "owner_approval",
                                    "owner": "develop",
                                    "owner_interaction_id": parsed_result.get("interaction_id"),
                                    "open_url": parsed_result.get("open_url"),
                                    "request_fingerprint": parsed_result.get("request_fingerprint"),
                                    "request_summary": parsed_result.get("request_summary") or {},
                                    "expires_at": parsed_result.get("expires_at"),
                                }

                        if stable_name == "workflow.validate":
                            try:
                                result_obj = parsed_result if isinstance(parsed_result, dict) else {}
                                workflow_definition = tool_args.get("workflow_definition") or {}
                                workflow_tasks = workflow_definition.get("tasks")
                                if (
                                    result_obj.get("valid") is True
                                    and isinstance(workflow_tasks, list)
                                    and workflow_tasks
                                ):
                                    yield AgentEvent(
                                        kind="presentation",
                                        payload={"kind": "workflow_dag", "workflow": workflow_definition},
                                    )
                                    logger.info(
                                        "[FACTORY:%s] 已发送通过正式校验的 DAG，任务数: %d",
                                        skill_name,
                                        len(workflow_tasks),
                                    )
                            except Exception as e:
                                logger.warning("[FACTORY:%s] 工作流校验结果解析失败: %s", skill_name, e)
                    except Exception as e:
                        tool_result = f"工具执行失败: {e}"
                        tool_failed = True
                        tool_error_source = "runtime"
                        tool_error_code = "tool_adapter_exception"
                        logger.error("[FACTORY:%s] 工具 %s 执行失败: %s", skill_name, tool_name, e)

                # 截断过长的工具结果
                if len(tool_result) > _TOOL_RESULT_MAX_LEN:
                    tool_result = (
                        tool_result[:_TOOL_RESULT_MAX_LEN]
                        + f"\n...[结果过长已截断，共 {len(tool_result)} 字符]"
                    )

                yield AgentEvent(
                    kind="tool_result",
                    payload={
                        "tool_call_id": tc["id"],
                        "tool_name": stable_name,
                        "content": tool_result,
                        "is_error": tool_failed,
                        "error_source": tool_error_source,
                        "error_code": tool_error_code,
                    },
                )

                lc_messages.append(ToolMessage(content=tool_result, tool_call_id=tc["id"]))
                if owner_approval_request is not None:
                    yield AgentEvent(
                        kind="interaction_required",
                        payload={
                            "tool_call_id": tc["id"],
                            **owner_approval_request,
                        },
                    )
                    return
        else:
            logger.warning("[FACTORY:%s] 达到最大迭代次数 %d", skill_name, max_iterations)

        # 输出最终结果
        if final_response is not None and final_response.content:
            yield text_event(str(final_response.content))
        else:
            # 超出迭代次数，流式重新生成最终回复
            logger.info("[FACTORY:%s] 超出迭代次数，流式重新生成", skill_name)
            result_text = ""
            async for chunk in get_llm(streaming=True).astream(lc_messages):
                if chunk.content:
                    result_text += chunk.content
                    yield text_event(str(chunk.content))
