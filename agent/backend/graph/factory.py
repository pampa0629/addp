"""
AgentFactory：领域 Agent 动态构建与执行

根据 Skill 元数据（front matter 中的 tools 白名单、max_iterations）
动态为每次请求构建隔离的领域 Agent，只注入该 Skill 声明的工具子集。

核心优势（相比旧 _run_skill_agent）：
- 工具隔离：领域 Agent 只能调用白名单内的工具，不暴露无关工具
- 可配置迭代次数：每个 Skill 可声明自己的 max_iterations
- 标准化接口：统一通过 TaskContext 传入，便于未来扩展为真正的 LangGraph 子图
"""
import logging
from typing import AsyncIterator, Dict, List, Any

from langchain_core.messages import HumanMessage, SystemMessage, ToolMessage

from graph.state import TaskContext
from tools.langchain_tools import create_agent_tools
from utils.llm import get_llm

logger = logging.getLogger("agent.factory")

MSG_TYPE_PROGRESS = "tool_progress"
MSG_TYPE_RESULT = "result"
MSG_TYPE_DAG = "dag"

# ToolMessage 截断上限（与 main_agent 保持一致）
_TOOL_RESULT_MAX_LEN = 3000


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
    ) -> AsyncIterator[tuple[str, str]]:
        """
        动态构建并执行领域 Agent（ReAct 循环）。

        参数：
            task_context:       主 Agent 打包的任务上下文
            skill_body:         SKILL.md 正文（作为 system prompt）
            allowed_tool_names: 该 Skill 声明的工具白名单
            max_iterations:     最大 ReAct 迭代次数

        yield (msg_type, content)：
            - (MSG_TYPE_PROGRESS, "正在调用工具 `xxx`...")  ← 中间过程
            - (MSG_TYPE_RESULT,   "最终自然语言回复")        ← 最终结果
        """
        skill_name = task_context["skill_name"]
        user_request = task_context["user_request"]
        context_summary = task_context["context_summary"]
        token = task_context["token"]

        # 构建用户输入（注入对话背景）
        if context_summary:
            human_input = f"对话背景：\n{context_summary}\n\n用户请求：{user_request}"
        else:
            human_input = user_request

        # 只创建白名单内的工具（隔离）
        all_tools = create_agent_tools(
            token,
            tenant_id=task_context.get("tenant_id", 1),
            user_id=task_context.get("user_id", 1),
        )
        all_tool_map = {t.name: t for t in all_tools}
        tools = [all_tool_map[name] for name in allowed_tool_names if name in all_tool_map]
        # 执行时也只允许白名单内的工具，防止 LLM 调用未授权工具
        tool_map = {t.name: t for t in tools}

        missing = [name for name in allowed_tool_names if name not in all_tool_map]
        if missing:
            logger.warning("[FACTORY:%s] 白名单中的工具未找到: %s", skill_name, missing)

        llm_with_tools = get_llm(streaming=False).bind_tools(tools)

        # 领域 Agent 的独立消息栈（用完即销毁，不污染主 Agent 历史）
        lc_messages = [
            SystemMessage(content=skill_body),
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
                tool_args = tc["args"]
                logger.info("[FACTORY:%s] 执行工具: %s | args: %s", skill_name, tool_name, tool_args)

                yield (MSG_TYPE_PROGRESS, f"正在调用工具 `{tool_name}`...\n")

                tool_fn = tool_map.get(tool_name)
                if tool_fn is None:
                    tool_result = f"错误：工具 `{tool_name}` 不在该 Skill 的白名单内"
                    logger.error("[FACTORY:%s] 工具不在白名单: %s", skill_name, tool_name)
                else:
                    try:
                        raw = await tool_fn.ainvoke(tool_args)
                        tool_result = str(raw)
                        preview = tool_result[:200] + ("..." if len(tool_result) > 200 else "")
                        logger.info("[FACTORY:%s] 工具结果（前200字符）: %s", skill_name, preview)

                        # 检测 generate_workflow 成功，发送 DAG 可视化事件
                        if tool_name == "generate_workflow":
                            try:
                                import json
                                result_obj = json.loads(tool_result)
                                if result_obj.get("status") in ("success", "validation_failed") and result_obj.get("workflow"):
                                    workflow_data = result_obj["workflow"]
                                    yield (MSG_TYPE_DAG, json.dumps(workflow_data, ensure_ascii=False))
                                    logger.info("[FACTORY:%s] 已发送 DAG 可视化数据，任务数: %d", skill_name, len(workflow_data.get("tasks", [])))
                            except Exception as e:
                                logger.warning("[FACTORY:%s] DAG 解析失败: %s", skill_name, e)
                    except Exception as e:
                        tool_result = f"工具执行失败: {e}"
                        logger.error("[FACTORY:%s] 工具 %s 执行失败: %s", skill_name, tool_name, e)

                # 截断过长的工具结果
                if len(tool_result) > _TOOL_RESULT_MAX_LEN:
                    tool_result = (
                        tool_result[:_TOOL_RESULT_MAX_LEN]
                        + f"\n...[结果过长已截断，共 {len(tool_result)} 字符]"
                    )

                lc_messages.append(ToolMessage(content=tool_result, tool_call_id=tc["id"]))
        else:
            logger.warning("[FACTORY:%s] 达到最大迭代次数 %d", skill_name, max_iterations)

        # 输出最终结果
        if final_response is not None and final_response.content:
            yield (MSG_TYPE_RESULT, final_response.content)
        else:
            # 超出迭代次数，流式重新生成最终回复
            logger.info("[FACTORY:%s] 超出迭代次数，流式重新生成", skill_name)
            result_text = ""
            async for chunk in get_llm(streaming=True).astream(lc_messages):
                if chunk.content:
                    result_text += chunk.content
                    yield (MSG_TYPE_PROGRESS, chunk.content)
            if result_text:
                yield (MSG_TYPE_RESULT, "")
