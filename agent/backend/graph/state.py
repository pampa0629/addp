"""
Agent 状态定义

AgentState:    请求级状态，在路由节点和执行节点之间传递
TaskContext:   主 Agent → 领域 Agent 的标准化任务包
RouteDecision: 路由节点的结构化输出 Schema（路由 + 直接回复二合一）
"""
from typing import TypedDict, Optional, List, Dict, Any
from pydantic import BaseModel, Field


class AgentState(TypedDict):
    """Agent 请求级状态，在节点间传递（不持久化）"""

    # 请求上下文（请求开始时注入）
    user_id: int
    tenant_id: int
    token: str

    # 从 DB 加载的对话历史（含本轮用户消息，最多 20 条）
    messages: List[Dict[str, Any]]  # [{"role": "user/assistant", "content": "..."}]

    # 会话级历史摘要（当历史超出窗口时由后台 LLM 压缩生成，可为空）
    session_summary: Optional[str]

    # 路由决策（由 _route_node 填充）
    routed_skill: Optional[str]   # 技能名称；None 表示无需技能
    direct_reply: Optional[str]   # 无需技能时的直接回复内容（来自同一次 LLM 调用）


class TaskContext(TypedDict):
    """主 Agent → 领域 Agent 的标准化任务包"""

    skill_name: str          # 要激活的 Skill 名称
    user_request: str        # 用户的原始请求（本轮）
    context_summary: str     # 最近 N 轮历史摘要（由主 Agent 提供）
    user_id: int
    tenant_id: int
    token: str               # JWT token，用于 ADDP API 调用


class RouteDecision(BaseModel):
    """路由 + 直接回复的结构化输出 Schema（单次 LLM 调用）"""

    skill: Optional[str] = Field(
        default=None,
        description=(
            "需要激活的技能名称，例如 data-browse、execute-sql、data-preview、metadata-search。"
            "如果不需要技能则为 null。"
        ),
    )
    direct_reply: Optional[str] = Field(
        default=None,
        description=(
            "当 skill 为 null 时，对用户问题的完整回复（中文）。"
            "如果 skill 不为 null，此字段为 null。"
        ),
    )
