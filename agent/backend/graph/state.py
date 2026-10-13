"""
Agent 状态定义

AgentState:    请求级状态，在路由节点和执行节点之间传递
TaskContext:   主 Agent → 领域 Agent 的标准化任务包
RouteDecision: 路由节点的结构化输出 Schema（路由 + 直接回复二合一）
"""
from typing import TypedDict, Optional, List, Dict, Any, NotRequired, Literal
from pydantic import BaseModel, ConfigDict, Field


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
    needs_platform_semantics: NotRequired[bool]
    draft_catalog: NotRequired[List[Dict[str, str]]]
    draft_action: NotRequired[Optional[Literal["inspect", "create"]]]
    draft_ref: NotRequired[Optional[str]]


class TaskContext(TypedDict):
    """主 Agent → 领域 Agent 的标准化任务包"""

    skill_name: str          # 要激活的 Skill 名称
    user_request: str        # 用户的原始请求（本轮）
    context_summary: str     # 最近 N 轮历史摘要（由主 Agent 提供）
    user_id: int
    tenant_id: int
    token: str               # User Access Token，仅用于解析身份和申请委托令牌
    agent_run_id: str        # 持久 AgentRun UUID，用于委托令牌审计绑定
    checkpoint: NotRequired[Dict[str, Any]]  # addp.agent-checkpoint/v1 语义检查点
    accept_language: NotRequired[str]  # 当前 HTTP 请求语言，只用于 Runtime 展示，不参与复核身份
    draft_arguments: NotRequired[Dict[str, Any]]  # 本次按身份恢复的原参数，仅临时输入；checkpoint 只存引用


class RouteDecision(BaseModel):
    """路由 + 直接回复的结构化输出 Schema（单次 LLM 调用）"""

    model_config = ConfigDict(extra="forbid")
    draft_action: Optional[Literal["inspect", "create"]] = Field(default=None, description="inspect 只解释已有草稿且不操作业务；create 仅明确沿用全部原参数创建、不修改、不运行，此时必须 needs_platform_semantics=true。请求修改则走普通平台路线。指代不明时保留动作但 draft_ref=null，不能猜选。")
    draft_ref: Optional[str] = Field(default=None, description="只选近期受信任草稿目录的确切 reference；缺失或有歧义时为 null，不能从正文推测。")
    skill: Optional[str] = Field(
        default=None,
        description=(
            "需要激活的技能名称，例如 data-browse、execute-sql、data-preview、metadata-search。"
            "如果不需要技能则为 null。"
        ),
    )
    needs_platform_semantics: bool = Field(
        default=False,
        description="用户目标属于平台语义任务领域时为 true，此时 skill 和 direct_reply 都为 null，必须先读取平台能力目录。",
    )
    direct_reply: Optional[str] = Field(
        default=None,
        description=(
            "当 skill 为 null 时，对用户问题的完整回复（中文）。"
            "如果 skill 不为 null，此字段为 null。"
        ),
    )


class PlatformRouteDecision(BaseModel):
    capability: Optional[str] = Field(default=None, description="从 owner 返回的目录选择唯一能力身份；不能编造，无匹配或有歧义时为 null。")
    direct_reply: Optional[str] = Field(default=None, description="不能选择唯一能力时，用中文解释支持边界或询问业务意图；选中能力时为 null。")
