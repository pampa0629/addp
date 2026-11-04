"""
Agent 状态定义

AgentState:    请求级状态，在路由节点和执行节点之间传递
TaskContext:   主 Agent → 领域 Agent 的标准化任务包
RouteDecision: 路由节点的结构化决策 Schema
"""
from typing import TypedDict, Optional, List, Dict, Any, NotRequired, Literal
from pydantic import BaseModel, ConfigDict, Field


class DraftExplanationScope(BaseModel):
    """Private inspect scope: facts stay with Runtime; only selected fragments reach the model."""

    model_config = ConfigDict(extra="forbid")
    topics: List[Literal["status", "policy", "boundary", "authorization", "diagnostics", "fields", "query"]] = Field(
        min_length=1, max_length=7,
        description="只选择本轮用户问及的主题；泛问解释草稿时选status、policy、boundary、authorization。正式诊断原因选diagnostics，字段/映射解释选fields，已有查询表达式选query。",
    )
    fields: List[str] = Field(max_length=200, description="fields主题必须列出用户明确问及的映射source或target精确名称；其他主题为[]。不得编造字段或填入整个用户请求。")


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
    answer_context: Optional[str]   # 语义选择的受限回答依据，不是公开正文
    needs_platform_semantics: NotRequired[bool]
    platform_capability: NotRequired[Optional[str]]
    reply_detail: NotRequired[str]  # 表达偏好，不改变业务目标或权限
    draft_catalog: NotRequired[List[Dict[str, str]]]
    draft_action: NotRequired[Optional[Literal["inspect", "create"]]]
    draft_ref: NotRequired[Optional[str]]
    draft_explanation: NotRequired[Optional[Dict[str, Any]]]


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
    parameter_reader: NotRequired[Any]  # 服务端按当前身份恢复原步骤或用户消息，非模型工具
    plan: NotRequired[List[Dict[str, str]]]  # 用户可见计划进度，不是业务事实
    resuming: NotRequired[bool]  # 服务端已恢复同一 Run 的正式 Interaction，非模型输入
    platform_capability: NotRequired[str]  # 由正式目录核验的本次操作身份


class RouteDecision(BaseModel):
    """只返回路由决策；正文由独立回答阶段生成"""

    model_config = ConfigDict(extra="forbid")
    draft_action: Optional[Literal["inspect", "create"]] = Field(default=None, description="草稿动作优先于通用平台领域分类。inspect 只解释已有历史草稿：skill=null 且 needs_platform_semantics=false；create 仅本轮明确肯定地请求沿用全部历史原参数创建、不修改、不运行：skill=null 且 needs_platform_semantics=true。明确不创建、只预检或只生成草稿时禁止create；当前请求附带完整传输配置并要求按原参数预检时，本字段和draft_ref为null、skill=null、needs_platform_semantics=true，不索要历史草稿引用。请求修改也走普通平台路线。历史草稿指代不明时保留其动作但draft_ref=null，不能猜选。")
    draft_ref: Optional[str] = Field(default=None, description="只选近期受信任草稿目录的确切 reference；缺失或有歧义时为 null，不能从正文推测。")
    draft_explanation: Optional[DraftExplanationScope] = Field(default=None, description="draft_action=inspect时必须声明解释范围，其他动作必须为null。状态、策略、执行边界和授权由Runtime固定解释，只有明确字段/映射或已有查询表达式需要正文模型。")
    skill: Optional[str] = Field(
        default=None,
        description=(
            "需要激活的已注册普通技能名称；具体名称以当前路由提示词的可用技能清单为准。"
            "draft_action 非 null 时必须为 null；无草稿动作且不需要技能时也为 null。"
        ),
    )
    needs_platform_semantics: bool = Field(
        default=False,
        description="draft_action=inspect 时必须为 false；draft_action=create 时必须为 true，两种动作的 skill 均为 null。仅无草稿动作时再按本轮平台语义任务领域分类：属于该领域为 true、skill=null，必须先读取平台能力目录；普通目标为 false。",
    )


class PlatformRouteDecision(BaseModel):
    capability: Optional[str] = Field(default=None, description="从 owner 返回的目录选择唯一能力身份；不能编造，无匹配或有歧义时为 null。")
    answer_context: Optional[str] = Field(default=None, description="无匹配或歧义的依据，仅供最终回答阶段使用，不生成用户正文；选中能力时为 null。")
