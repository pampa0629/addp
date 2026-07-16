import copy
import json
from typing import Any, AsyncIterator
from uuid import UUID

from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession

from models.run import AgentRun
from models.run_event import AgentRunEvent


_REPLAYABLE_EVENT_TYPES = {
    "RUN_STARTED",
    "STATE_SNAPSHOT",
    "TEXT_MESSAGE_START",
    "TEXT_MESSAGE_CONTENT",
    "TEXT_MESSAGE_END",
    "TOOL_CALL_START",
    "TOOL_CALL_RESULT",
    "ACTIVITY_SNAPSHOT",
    "RUN_FINISHED",
    "RUN_ERROR",
}


def replay_payload(event_payload: dict[str, Any]) -> dict[str, Any] | None:
    """Returns the safe, client-replayable projection of one AG-UI event."""
    event_type = str(event_payload.get("type") or "")
    if event_type not in _REPLAYABLE_EVENT_TYPES:
        return None

    payload = copy.deepcopy(event_payload)
    if event_type == "TOOL_CALL_RESULT":
        payload["content"] = "工具调用已完成；详情见运行审计"
    return payload


async def append_run_event(
    db: AsyncSession,
    *,
    agent_run_id: UUID,
    protocol_invocation_id: str,
    event_payload: dict[str, Any],
) -> AgentRunEvent | None:
    payload = replay_payload(event_payload)
    if payload is None:
        return None

    await db.execute(
        select(AgentRun.id)
        .where(AgentRun.id == agent_run_id)
        .with_for_update()
    )
    result = await db.execute(
        select(func.coalesce(func.max(AgentRunEvent.sequence), 0)).where(
            AgentRunEvent.agent_run_id == agent_run_id
        )
    )
    event = AgentRunEvent(
        agent_run_id=agent_run_id,
        sequence=int(result.scalar_one()) + 1,
        protocol_invocation_id=protocol_invocation_id,
        event_type=str(payload["type"]),
        payload=payload,
    )
    db.add(event)
    await db.flush()
    return event


async def iter_run_events(
    db: AsyncSession,
    *,
    agent_run_id: UUID,
    after: int,
) -> AsyncIterator[AgentRunEvent]:
    result = await db.stream_scalars(
        select(AgentRunEvent)
        .where(AgentRunEvent.agent_run_id == agent_run_id, AgentRunEvent.sequence > after)
        .order_by(AgentRunEvent.sequence.asc())
    )
    async for event in result:
        yield event


def encode_replayed_event(event: AgentRunEvent) -> str:
    return f"id: {event.sequence}\ndata: {json.dumps(event.payload, ensure_ascii=False, separators=(',', ':'))}\n\n"
