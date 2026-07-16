from datetime import datetime, timezone
from typing import Any
from uuid import UUID

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from models.interaction import Interaction


class InteractionNotFoundError(Exception):
    pass


class InteractionStateError(Exception):
    pass


class InteractionAnswerError(Exception):
    pass


def normalize_options(candidates: list[dict[str, Any]]) -> list[dict[str, Any]]:
    options: list[dict[str, Any]] = []
    for index, candidate in enumerate(candidates):
        location = candidate.get("location") if isinstance(candidate.get("location"), dict) else {}
        value = (
            candidate.get("value")
            or candidate.get("locator")
            or location.get("locator")
            or candidate.get("id")
            or f"candidate-{index + 1}"
        )
        label = (
            candidate.get("label")
            or candidate.get("name")
            or candidate.get("full_name")
            or location.get("full_name")
            or str(value)
        )
        candidate_fact = candidate.get("candidate") or candidate
        options.append({"label": str(label), "value": value, "candidate": candidate_fact})
    return options


async def create_clarification(
    db: AsyncSession,
    *,
    session_id: int,
    user_id: int,
    tenant_id: int,
    agent_run_id: UUID,
    tool_call_id: str | None,
    prompt: str,
    candidates: list[dict[str, Any]],
) -> Interaction:
    options = normalize_options(candidates)
    interaction = Interaction(
        session_id=session_id,
        user_id=user_id,
        tenant_id=tenant_id,
        agent_run_id=agent_run_id,
        tool_call_id=tool_call_id,
        kind="clarification",
        owner="agent",
        status="pending",
        prompt=prompt,
        response_schema={
            "type": "object",
            "properties": {
                "value": {"type": ["string", "number"]},
                "label": {"type": "string"},
            },
            "required": ["value", "label"],
            "additionalProperties": True,
        },
        options=options,
    )
    db.add(interaction)
    await db.flush()
    return interaction


async def resolve_interaction(
    db: AsyncSession,
    *,
    interaction_id: str,
    session_id: int,
    user_id: int,
    tenant_id: int,
    payload: Any,
) -> Interaction:
    try:
        parsed_id = UUID(interaction_id)
    except ValueError as exc:
        raise InteractionNotFoundError from exc

    result = await db.execute(
        select(Interaction).where(
            Interaction.id == parsed_id,
            Interaction.session_id == session_id,
            Interaction.user_id == user_id,
            Interaction.tenant_id == tenant_id,
        )
    )
    interaction = result.scalar_one_or_none()
    if interaction is None:
        raise InteractionNotFoundError
    if interaction.status != "pending":
        raise InteractionStateError(interaction.status)

    if not isinstance(payload, dict):
        raise InteractionAnswerError("交互回答必须是对象")
    selected = next(
        (
            option
            for option in interaction.options or []
            if isinstance(option, dict) and option.get("value") == payload.get("value")
        ),
        None,
    )
    if selected is None:
        raise InteractionAnswerError("交互回答不在允许的选项中")

    interaction.status = "completed"
    interaction.answer = selected
    interaction.completed_at = datetime.now(timezone.utc)
    await db.flush()
    return interaction


async def cancel_pending_interactions(db: AsyncSession, *, agent_run_id: UUID) -> None:
    result = await db.execute(
        select(Interaction).where(
            Interaction.agent_run_id == agent_run_id,
            Interaction.status == "pending",
        )
    )
    for interaction in result.scalars():
        interaction.status = "cancelled"
        interaction.completed_at = datetime.now(timezone.utc)
    await db.flush()


def format_resume_message(interaction: Interaction) -> str:
    answer = interaction.answer
    if isinstance(answer, dict):
        label = answer.get("label") or answer.get("value")
        candidate = answer.get("candidate")
        if candidate:
            return f"用户已完成澄清，选择：{label}；候选事实：{candidate}"
        return f"用户已完成澄清，选择：{label}"
    return f"用户已完成澄清，回答：{answer}"
