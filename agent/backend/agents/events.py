from dataclasses import dataclass, field
from typing import Any, Literal


AgentEventKind = Literal[
    "text",
    "tool_start",
    "tool_result",
    "checkpoint",
    "result_ref",
    "run_state",
    "presentation",
    "interaction_required",
]


@dataclass(frozen=True)
class AgentEvent:
    """Agent 内部结构化事件，由 AG-UI 适配层负责协议编码。"""

    kind: AgentEventKind
    payload: dict[str, Any] = field(default_factory=dict)


def text_event(delta: str) -> AgentEvent:
    return AgentEvent(kind="text", payload={"delta": delta})
