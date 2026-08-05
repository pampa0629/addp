"""
ADDP Agent 主智能体实现

架构（P3 更新）：
- 显式 AgentState 状态在节点间流转，职责清晰
- 路由节点（_route_node）：携带历史上下文，单次 LLM 调用（结构化输出）
  同时完成意图识别、技能路由和直接回复（无技能时）
- AgentFactory（graph/factory.py）：按需动态构建领域 Agent
  只注入 Skill `agents/addp.yaml` 声明的稳定 Tool 白名单

主流程：
1. 构建 AgentState（历史由 chat.py 从 DB 加载）
2. _route_node → 决定 routed_skill 或 direct_reply
3a. 有技能 → AgentFactory.run（隔离工具，可配置迭代次数）
3b. 无技能 → 直接输出 direct_reply（来自同一次 LLM 调用）
"""
import logging
from pathlib import Path
from typing import List, Dict, Any, AsyncIterator, Optional

import yaml
from addp_common.tools import load_manifest
from langchain_core.messages import HumanMessage, AIMessage, SystemMessage

from utils.llm import get_llm
from graph.state import AgentState, TaskContext, RouteDecision
from agents.events import AgentEvent, text_event
from graph.factory import AgentFactory

logger = logging.getLogger("agent.main")

_SKILLS_DIR = Path(__file__).resolve().parents[3] / "skills"

# 对外导出，供 AG-UI 适配层使用
__all__ = ["stream_agent_response"]


# ─────────────────────────────────────────────
# Skill 与 ADDP Runtime 装配加载（启动时一次性读取）
# ─────────────────────────────────────────────

class SkillMeta:
    """Skill 公共元数据与 ADDP Runtime 装配。"""

    def __init__(
        self,
        name: str,
        description: str,
        path: Path,
        tools: List[str],
        max_iterations: int,
    ):
        self.name = name
        self.description = description
        self.path = path
        self.tools = tools                   # 工具白名单
        self.max_iterations = max_iterations  # ReAct 最大迭代次数

    def load_body(self) -> str:
        """按需读取 SKILL.md 正文（去掉 front matter，作为 system prompt）"""
        content = self.path.read_text(encoding="utf-8").strip()
        if content.startswith("---"):
            parts = content.split("---", 2)
            if len(parts) >= 3:
                return parts[2].strip()
        return content


def _parse_front_matter(content: str) -> Dict[str, Any]:
    """解析 YAML front matter，返回字段字典"""
    if not content.startswith("---"):
        return {}
    parts = content.split("---", 2)
    if len(parts) < 3:
        return {}
    try:
        return yaml.safe_load(parts[1]) or {}
    except yaml.YAMLError:
        return {}


_skill_registry: Dict[str, SkillMeta] = {}


def _load_skill_registry() -> Dict[str, SkillMeta]:
    """扫描平台级 skills/，组合公共 front matter 与 agents/addp.yaml。"""
    registry: Dict[str, SkillMeta] = {}
    if not _SKILLS_DIR.exists():
        return registry

    available_tools = {tool.name for tool in load_manifest().tools}

    for skill_file in sorted(_SKILLS_DIR.glob("*/SKILL.md")):
        content = skill_file.read_text(encoding="utf-8").strip()
        meta = _parse_front_matter(content)
        name = meta.get("name", "")
        if not name:
            continue
        runtime_file = skill_file.parent / "agents" / "addp.yaml"
        if not runtime_file.exists():
            logger.info("Skill %s 未声明 ADDP Runtime 配置，跳过内置 Agent 装配", name)
            continue
        runtime = yaml.safe_load(runtime_file.read_text(encoding="utf-8")) or {}
        if runtime.get("schema") != "addp.skill-runtime/v1":
            raise ValueError(f"Skill {name} 的 agents/addp.yaml schema 无效")
        required_tools = runtime.get("required_tools") or []
        missing_tools = sorted(set(required_tools) - available_tools)
        if missing_tools:
            raise ValueError(f"Skill {name} 引用了不存在的 Tool: {missing_tools}")
        registry[name] = SkillMeta(
            name=name,
            description=meta.get("description", ""),
            path=skill_file,
            tools=required_tools,
            max_iterations=int(runtime.get("max_iterations", 5)),
        )
    return registry


def _get_skill_registry() -> Dict[str, SkillMeta]:
    """获取 skill 注册表（模块级缓存，启动后不再变化）"""
    global _skill_registry
    if not _skill_registry:
        _skill_registry = _load_skill_registry()
        if _skill_registry:
            logger.info(
                "已加载 %d 个 skill: %s",
                len(_skill_registry),
                {name: meta.tools for name, meta in _skill_registry.items()},
            )
        else:
            logger.warning("未找到 skill，skills/ 目录为空或不存在")
    return _skill_registry


# ─────────────────────────────────────────────
# 路由节点（上下文感知，合并直接回复）
# ─────────────────────────────────────────────

def _build_routing_system_prompt(session_summary: Optional[str] = None) -> str:
    """构建路由节点的 system prompt：指导 LLM 路由 + 直接回复二合一"""
    registry = _get_skill_registry()
    skill_list = "\n".join(
        f"- `{name}`: {meta.description}" for name, meta in registry.items()
    ) if registry else "（暂无可用技能）"

    summary_section = ""
    if session_summary:
        summary_section = f"\n## 早期对话背景（历史摘要）\n{session_summary}\n"

    return f"""你是 ADDP（全域数据平台）的 AI 助手，帮助用户通过自然语言完成数据管理操作。
{summary_section}
根据对话历史和用户最新消息，你需要做出决策：
1. 如果用户需要操作数据（浏览目录、查询、预览、搜索等），选择合适的技能（skill 字段）
2. 如果不需要操作数据（问候、询问功能说明、闲聊等），直接回复（direct_reply 字段，skill 为 null）

## 可用技能
{skill_list}

## 决策规则
- 涉及查看/查询/预览/搜索数据 → 激活对应技能，skill 填写技能名称，direct_reply 为 null
- 涉及空间分析、工作流设计、DAG、算子组合、工作流校验或执行 → 必须激活对应技能
- 用户要求“方案”或“可执行方案”时，如果完成它需要读取平台数据或算子事实，也必须激活技能，不能退化为常识计算
- 问候、功能说明、一般性回答 → skill 为 null，direct_reply 填写中文回复
- 可以根据对话历史理解上下文（如"继续查询"、"那个表"等指代上文的表达）

## 直接回复规范（当 skill 为 null 时）
- 中文回复，语气友好简洁
- 可基于对话历史回答上下文问题
- 不要假装执行了任何数据操作"""


async def _route_node(state: AgentState, llm: Any) -> AgentState:
    """
    路由节点：单次 LLM 调用（结构化输出），上下文感知。
    同时完成：意图识别 + 技能路由 + 直接回复（无技能时）。
    """
    messages = state["messages"]
    registry = _get_skill_registry()
    session_summary = state.get("session_summary")

    # 最近 3 轮历史（6 条消息，不含本轮用户消息）
    history = messages[:-1]
    recent_history = history[-(3 * 2):]
    user_message = messages[-1]["content"]

    lc_messages = [SystemMessage(content=_build_routing_system_prompt(session_summary))]
    for m in recent_history:
        if m["role"] == "user":
            lc_messages.append(HumanMessage(content=m["content"]))
        elif m["role"] == "assistant":
            lc_messages.append(AIMessage(content=m["content"]))
    lc_messages.append(HumanMessage(content=user_message))

    llm_with_structure = llm.with_structured_output(RouteDecision)
    decision: RouteDecision = await llm_with_structure.ainvoke(lc_messages)
    skill_name = decision.skill if (decision.skill and decision.skill in registry) else None
    direct_reply = decision.direct_reply if not skill_name else None
    logger.info("[ROUTE] skill=%s has_direct_reply=%s", skill_name, bool(direct_reply))

    return {**state, "routed_skill": skill_name, "direct_reply": direct_reply}


# ─────────────────────────────────────────────
# 上下文摘要（给领域 Agent 的背景）
# ─────────────────────────────────────────────

def _build_context_summary(
    messages: List[Dict[str, Any]],
    session_summary: Optional[str] = None,
    max_turns: int = 3,
) -> str:
    """构建领域 Agent 的对话背景摘要：历史摘要（若有）+ 最近 N 轮对话"""
    recent = messages[-(max_turns * 2):]
    lines = []
    for m in recent:
        role = "用户" if m.get("role") == "user" else "助手"
        content = m.get("content", "")
        lines.append(f"{role}: {content}")
    recent_text = "\n".join(lines) if lines else ""

    if session_summary and recent_text:
        return f"【早期历史摘要】\n{session_summary}\n\n【近期对话】\n{recent_text}"
    elif session_summary:
        return f"【早期历史摘要】\n{session_summary}"
    return recent_text


# ─────────────────────────────────────────────
# 主入口：流式输出
# ─────────────────────────────────────────────

async def stream_agent_response(
    messages: List[Dict[str, Any]],
    user_id: int,
    tenant_id: int,
    token: str,
    agent_run_id: str,
    session_summary: Optional[str] = None,
    checkpoint: Optional[Dict[str, Any]] = None,
    forced_skill_name: Optional[str] = None,
) -> AsyncIterator[AgentEvent]:
    """
    流式输出 Agent 回复。yield (msg_type, content) 元组。

    流程（P4 更新）：
    1. 构建 AgentState，注入从 DB 加载的历史、session_summary 和请求上下文
    2. _route_node：单次 LLM 调用（结构化输出），携带最近 3 轮历史 + 历史摘要
    3a. 有技能 → AgentFactory.run：按 Skill 白名单动态构建领域 Agent，ReAct 循环
    3b. 无技能 → 直接输出 direct_reply（已在路由节点生成，无额外 LLM 调用）
    """
    logger.info(
        "开始处理请求 | user_id=%s tenant_id=%s | 消息数=%d",
        user_id, tenant_id, len(messages),
    )

    # 初始化请求级状态
    state: AgentState = {
        "user_id": user_id,
        "tenant_id": tenant_id,
        "token": token,
        "agent_run_id": agent_run_id,
        "messages": messages,
        "session_summary": session_summary,
        "routed_skill": None,
        "direct_reply": None,
    }

    registry = _get_skill_registry()
    reasoning_llm = get_llm(tenant_id, "reasoning")
    if forced_skill_name:
        if forced_skill_name not in registry:
            raise ValueError(f"AgentRun 记录的 Skill 不存在: {forced_skill_name}")
        state = {**state, "routed_skill": forced_skill_name, "direct_reply": None}
        logger.info("[ROUTE] resume 使用 AgentRun 已记录 Skill: %s", forced_skill_name)
    else:
        # 路由节点（单次 LLM 调用，上下文感知）
        state = await _route_node(state, reasoning_llm)

    if state["routed_skill"]:
        # 技能执行路径：AgentFactory 动态构建领域 Agent
        skill_name = state["routed_skill"]
        skill_meta = registry[skill_name]

        task_context: TaskContext = {
            "skill_name": skill_name,
            "user_request": messages[-1]["content"],
            "context_summary": _build_context_summary(messages[:-1], session_summary),
            "user_id": user_id,
            "tenant_id": tenant_id,
            "token": token,
            "agent_run_id": agent_run_id,
            "checkpoint": checkpoint or {},
        }

        yield AgentEvent(kind="run_state", payload={"skill_name": skill_name})

        async for event in AgentFactory.run(
            task_context=task_context,
            skill_body=skill_meta.load_body(),
            allowed_tool_names=skill_meta.tools,
            max_iterations=skill_meta.max_iterations,
            llm=reasoning_llm,
        ):
            yield event
    else:
        # 直接回复路径（来自路由节点，无额外 LLM 调用）
        reply = state.get("direct_reply") or ""
        if reply:
            yield text_event(reply)
