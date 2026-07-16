import copy
import json
from datetime import datetime, timezone
from typing import Any
from uuid import UUID

from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession

from agents.checkpoint import confirm_selection, new_checkpoint, normalize_checkpoint
from models.interaction import Interaction
from models.run import AgentRun
from models.run_event import AgentRunEvent
from models.run_step import AgentRunStep
from services.interactions import cancel_pending_interactions


ERROR_MESSAGE_MAX_LENGTH = 1000


class AgentRunNotFoundError(Exception):
    pass


class AgentRunStateError(Exception):
    pass


def summarize_tool_result(content: str) -> str:
    try:
        value = json.loads(content)
    except (TypeError, json.JSONDecodeError):
        return json.dumps(
            {
                "value_type": "text",
                "result_size_bytes": len((content or "").encode("utf-8")),
            },
            ensure_ascii=False,
            separators=(",", ":"),
        )

    if isinstance(value, list):
        return json.dumps({"item_count": len(value)}, ensure_ascii=False, separators=(",", ":"))
    if not isinstance(value, dict):
        return json.dumps({"value_type": type(value).__name__}, ensure_ascii=False, separators=(",", ":"))

    summary: dict[str, Any] = {}
    for key in (
        "status",
        "message",
        "valid",
        "errors",
        "warnings",
        "total",
        "page",
        "page_size",
        "preview_type",
        "clarification_reason",
        "execution_id",
    ):
        if key in value:
            summary[key] = value[key]
    for key in ("results", "engines", "items", "data"):
        if isinstance(value.get(key), list):
            summary[f"{key}_count"] = len(value[key])
    summary["result_size_bytes"] = len(content.encode("utf-8"))
    return json.dumps(summary, ensure_ascii=False, separators=(",", ":"))


def _limit_error_message(error_message: str | None) -> str | None:
    return error_message[:ERROR_MESSAGE_MAX_LENGTH] if error_message else None


async def create_agent_run(
    db: AsyncSession,
    *,
    session_id: int,
    user_id: int,
    tenant_id: int,
    protocol_run_id: str,
) -> AgentRun:
    run = AgentRun(
        session_id=session_id,
        user_id=user_id,
        tenant_id=tenant_id,
        initial_protocol_run_id=protocol_run_id,
        status="running",
        checkpoint=new_checkpoint(),
    )
    db.add(run)
    await db.flush()
    return run


async def get_owned_agent_run(
    db: AsyncSession,
    *,
    agent_run_id: UUID,
    session_id: int,
    user_id: int,
    tenant_id: int,
) -> AgentRun:
    result = await db.execute(
        select(AgentRun).where(
            AgentRun.id == agent_run_id,
            AgentRun.session_id == session_id,
            AgentRun.user_id == user_id,
            AgentRun.tenant_id == tenant_id,
        )
    )
    run = result.scalar_one_or_none()
    if run is None:
        raise AgentRunNotFoundError
    return run


async def resume_agent_run(
    db: AsyncSession,
    *,
    interactions: list[Interaction],
    session_id: int,
    user_id: int,
    tenant_id: int,
) -> AgentRun:
    run_ids = {interaction.agent_run_id for interaction in interactions}
    if len(run_ids) != 1:
        raise AgentRunStateError("一次 resume 只能恢复一个 AgentRun")
    run = await get_owned_agent_run(
        db,
        agent_run_id=next(iter(run_ids)),
        session_id=session_id,
        user_id=user_id,
        tenant_id=tenant_id,
    )
    if run.status != "waiting":
        raise AgentRunStateError(f"AgentRun 当前状态不可恢复: {run.status}")

    checkpoint = normalize_checkpoint(run.checkpoint)
    for interaction in interactions:
        confirm_selection(checkpoint, interaction.answer)
    run.checkpoint = checkpoint
    run.status = "running"
    run.error_source = None
    run.error_code = None
    run.error_message = None
    run.completed_at = None
    await db.flush()
    return run


async def retry_agent_run(
    db: AsyncSession,
    *,
    agent_run_id: UUID,
    session_id: int,
    user_id: int,
    tenant_id: int,
) -> AgentRun:
    run = await get_owned_agent_run(
        db,
        agent_run_id=agent_run_id,
        session_id=session_id,
        user_id=user_id,
        tenant_id=tenant_id,
    )
    if run.status != "failed":
        raise AgentRunStateError(f"只有失败的 AgentRun 可以重试: {run.status}")
    run.status = "running"
    run.error_source = None
    run.error_code = None
    run.error_message = None
    run.completed_at = None
    await db.flush()
    return run


async def cancel_agent_run(
    db: AsyncSession,
    *,
    agent_run_id: UUID,
    session_id: int,
    user_id: int,
    tenant_id: int,
) -> AgentRun:
    run = await get_owned_agent_run(
        db,
        agent_run_id=agent_run_id,
        session_id=session_id,
        user_id=user_id,
        tenant_id=tenant_id,
    )
    if run.status not in {"running", "waiting"}:
        raise AgentRunStateError(f"AgentRun 当前状态不可取消: {run.status}")
    await cancel_pending_interactions(db, agent_run_id=agent_run_id)
    await set_run_status(db, agent_run_id=agent_run_id, status="cancelled")
    return run


async def update_run_checkpoint(
    db: AsyncSession,
    *,
    agent_run_id: UUID,
    checkpoint: dict[str, Any],
) -> None:
    run = await db.get(AgentRun, agent_run_id)
    if run is None:
        raise AgentRunNotFoundError
    run.checkpoint = normalize_checkpoint(checkpoint)
    await db.flush()


async def set_run_skill(db: AsyncSession, *, agent_run_id: UUID, skill_name: str | None) -> None:
    run = await db.get(AgentRun, agent_run_id)
    if run is None:
        raise AgentRunNotFoundError
    run.skill_name = skill_name
    await db.flush()


async def set_run_status(
    db: AsyncSession,
    *,
    agent_run_id: UUID,
    status: str,
    error_source: str | None = None,
    error_code: str | None = None,
    error_message: str | None = None,
) -> None:
    if status not in {"running", "waiting", "completed", "failed", "cancelled"}:
        raise ValueError(f"无效 AgentRun 状态: {status}")
    run = await db.get(AgentRun, agent_run_id)
    if run is None:
        raise AgentRunNotFoundError
    run.status = status
    run.error_source = error_source
    run.error_code = error_code
    run.error_message = _limit_error_message(error_message)
    run.completed_at = datetime.now(timezone.utc) if status in {"completed", "failed", "cancelled"} else None
    await db.flush()


async def create_run_step(
    db: AsyncSession,
    *,
    agent_run_id: UUID,
    protocol_invocation_id: str,
    step_type: str,
    tool_call_id: str | None = None,
    tool_name: str | None = None,
    input_data: dict[str, Any] | None = None,
) -> AgentRunStep:
    result = await db.execute(
        select(func.coalesce(func.max(AgentRunStep.sequence), 0)).where(
            AgentRunStep.agent_run_id == agent_run_id
        )
    )
    sequence = int(result.scalar_one()) + 1
    step = AgentRunStep(
        agent_run_id=agent_run_id,
        sequence=sequence,
        step_type=step_type,
        status="running",
        protocol_invocation_id=protocol_invocation_id,
        tool_call_id=tool_call_id,
        tool_name=tool_name,
        input_data=copy.deepcopy(input_data or {}),
    )
    db.add(step)
    await db.flush()
    return step


async def complete_run_step(
    db: AsyncSession,
    *,
    step_id: UUID,
    status: str,
    output_summary: str | None = None,
    facts: dict[str, Any] | None = None,
    error_source: str | None = None,
    error_code: str | None = None,
    error_message: str | None = None,
) -> None:
    step = await db.get(AgentRunStep, step_id)
    if step is None:
        raise AgentRunNotFoundError
    step.status = status
    step.output_summary = output_summary[:2000] if output_summary else None
    if facts:
        step.facts = copy.deepcopy(facts)
    step.error_source = error_source
    step.error_code = error_code
    step.error_message = _limit_error_message(error_message)
    step.completed_at = datetime.now(timezone.utc)
    await db.flush()


async def attach_step_facts(
    db: AsyncSession,
    *,
    step_id: UUID,
    facts: dict[str, Any],
) -> None:
    step = await db.get(AgentRunStep, step_id)
    if step is None:
        raise AgentRunNotFoundError
    step.facts = copy.deepcopy(facts)
    await db.flush()


async def set_run_context_metrics(
    db: AsyncSession,
    *,
    agent_run_id: UUID,
    context_metrics: dict[str, int],
) -> None:
    run = await db.get(AgentRun, agent_run_id)
    if run is None:
        raise AgentRunNotFoundError
    run.context_metrics = copy.deepcopy(context_metrics)
    await db.flush()


async def refresh_run_metrics(db: AsyncSession, *, agent_run_id: UUID) -> dict[str, int]:
    run = await db.get(AgentRun, agent_run_id)
    if run is None:
        raise AgentRunNotFoundError
    step_result = await db.execute(
        select(AgentRunStep).where(AgentRunStep.agent_run_id == agent_run_id)
    )
    steps = list(step_result.scalars().all())
    event_result = await db.execute(
        select(AgentRunEvent).where(AgentRunEvent.agent_run_id == agent_run_id)
    )
    events = list(event_result.scalars().all())
    invocation_ids = {
        value
        for value in [
            *(step.protocol_invocation_id for step in steps),
            *(event.protocol_invocation_id for event in events),
        ]
        if value
    }
    end_time = run.completed_at or datetime.now(timezone.utc)
    duration_ms = 0
    if run.created_at is not None:
        duration_ms = max(0, int((end_time - run.created_at).total_seconds() * 1000))
    metrics = {
        "protocol_invocation_count": len(invocation_ids),
        "step_count": len(steps),
        "failed_step_count": sum(step.status == "failed" for step in steps),
        "event_count": len(events),
        "text_character_count": sum(
            len(str(event.payload.get("delta") or ""))
            for event in events
            if event.event_type == "TEXT_MESSAGE_CONTENT"
        ),
        "duration_ms": duration_ms,
    }
    run.metrics = metrics
    await db.flush()
    return metrics
