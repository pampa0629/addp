from datetime import datetime, timezone
from typing import Any
from uuid import UUID

import httpx
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from addp_common.client import DevelopClient
from config import settings
from models.interaction import Interaction


class InteractionNotFoundError(Exception):
    pass


class InteractionStateError(Exception):
    pass


class InteractionAnswerError(Exception):
    pass


class InteractionTextAnswerError(InteractionAnswerError):
    """A stable validation reason, translated at the HTTP boundary."""


CLARIFICATION_TEXT_MAX_LENGTH = 2000


class InteractionOwnerStateError(Exception):
    pass


class InteractionOwnerUnavailableError(Exception):
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
        response_schema={"oneOf": [
            {
                "type": "object",
                "properties": {
                    "value": {"type": ["string", "number"]},
                    "label": {"type": "string"},
                },
                "required": ["value", "label"],
                "additionalProperties": True,
                "not": {"required": ["text"]},
            },
            {
                "type": "object",
                "properties": {"text": {"type": "string", "minLength": 1,
                                         "maxLength": CLARIFICATION_TEXT_MAX_LENGTH}},
                "required": ["text"],
                "additionalProperties": False,
            },
        ]},
        options=options,
    )
    db.add(interaction)
    await db.flush()
    return interaction


async def create_owner_approval(
    db: AsyncSession,
    *,
    session_id: int,
    user_id: int,
    tenant_id: int,
    agent_run_id: UUID,
    tool_call_id: str | None,
    owner: str,
    owner_interaction_id: str,
    open_url: str,
    request_fingerprint: str,
    request_summary: dict[str, Any],
    expires_at: datetime | None,
) -> Interaction:
    interaction = Interaction(
        session_id=session_id,
        user_id=user_id,
        tenant_id=tenant_id,
        agent_run_id=agent_run_id,
        tool_call_id=tool_call_id,
        owner_interaction_id=owner_interaction_id,
        owner_request_fingerprint=request_fingerprint,
        open_url=open_url,
        request_summary=request_summary,
        kind="owner_approval",
        owner=owner,
        status="pending",
        prompt="工作流执行需要在 Develop 完成审批",
        response_schema={
            "type": "object",
            "properties": {"action": {"const": "check"}},
            "required": ["action"],
            "additionalProperties": False,
        },
        options=[],
        expires_at=expires_at,
    )
    db.add(interaction)
    await db.flush()
    return interaction


async def _load_owner_approval(owner_interaction_id: str, source_token: str) -> dict[str, Any]:
    async with DevelopClient(
        base_url=settings.get_gateway_url(),
        user_token=source_token,
    ) as client:
        return await client.get_tool_approval(owner_interaction_id)


async def resolve_interaction(
    db: AsyncSession,
    *,
    interaction_id: str,
    session_id: int,
    user_id: int,
    tenant_id: int,
    payload: Any,
    source_token: str | None = None,
    owner_approval_loader=None,
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
        ).with_for_update()
    )
    interaction = result.scalar_one_or_none()
    if interaction is None:
        raise InteractionNotFoundError
    if interaction.status != "pending":
        raise InteractionStateError(interaction.status)

    if not isinstance(payload, dict):
        raise InteractionAnswerError("交互回答必须是对象")

    if interaction.kind == "owner_approval":
        if payload != {"action": "check"}:
            raise InteractionAnswerError("审批交互只允许检查 Owner 状态")
        if not source_token:
            raise InteractionAnswerError("缺少用于检查审批状态的用户访问令牌")
        loader = owner_approval_loader or _load_owner_approval
        try:
            approval = await loader(interaction.owner_interaction_id, source_token)
        except httpx.HTTPError as exc:
            raise InteractionOwnerUnavailableError from exc
        if approval.get("request_fingerprint") != interaction.owner_request_fingerprint:
            raise InteractionAnswerError("Owner 审批请求指纹不匹配")
        owner_status = str(approval.get("status") or "")
        if owner_status not in {"approved", "consumed"}:
            raise InteractionOwnerStateError(owner_status or "unknown")
        answer = {
            "status": owner_status,
            "approval_id": interaction.owner_interaction_id,
            "request_fingerprint": interaction.owner_request_fingerprint,
        }
        if approval.get("execution_id"):
            answer["execution_id"] = approval["execution_id"]
        interaction.status = "completed"
        interaction.answer = answer
        interaction.completed_at = datetime.now(timezone.utc)
        await db.flush()
        return interaction

    if "text" in payload:
        if interaction.kind != "clarification" or set(payload) != {"text"} or not isinstance(payload["text"], str):
            raise InteractionTextAnswerError("invalid_shape")
        text = payload["text"].strip()
        if not text or len(payload["text"]) > CLARIFICATION_TEXT_MAX_LENGTH:
            raise InteractionTextAnswerError("invalid_length")
        selected = _match_text_option(interaction.options or [], text) or {"text": text}
    else:
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


def _match_text_option(options: list[dict[str, Any]], text: str) -> dict[str, Any] | None:
    # Review approval must always use the persisted button option, never text.
    if any(isinstance(option.get("candidate"), dict) and "operation_review" in option["candidate"]
           for option in options if isinstance(option, dict)):
        return None
    matches = []
    for option in options:
        if not isinstance(option, dict):
            continue
        candidate = option.get("candidate") if isinstance(option.get("candidate"), dict) else {}
        names = [option.get("label"), option.get("value"), candidate.get("name"), candidate.get("full_name")]
        if any(isinstance(name, (str, int)) and not isinstance(name, bool) and text == str(name) for name in names):
            matches.append(option)
    return matches[0] if len(matches) == 1 else None


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


def format_resume_display(interaction: Interaction) -> str:
    """Project a confirmed choice for the UI, not the runtime candidate facts."""
    if interaction.kind == "clarification" and isinstance(interaction.answer, dict):
        if "text" in interaction.answer:
            return interaction.answer["text"]
        return str(interaction.answer.get("label") or interaction.answer.get("value") or "")
    return format_resume_message(interaction)


def format_resume_message(interaction: Interaction) -> str:
    answer = interaction.answer
    if interaction.kind == "clarification" and isinstance(answer, dict) and "text" in answer:
        return (
            f"用户对当前澄清的文字回答：{answer['text']}\n"
            "这是补充要求，不是写入批准或新的 owner 资源事实；"
            "请沿用当前 Skill、已观察事实和必要澄清，修改配置后重新复核。\n"
            f"原澄清问题：{interaction.prompt}"
        )
    if interaction.kind == "owner_approval" and isinstance(answer, dict):
        if answer.get("status") == "consumed" and answer.get("execution_id"):
            return (
                "Develop 审批已消费，工作流 execution 已创建；"
                f"execution_id={answer['execution_id']}。不要再次调用 workflow.run。"
            )
        return (
            "Develop 已批准本次工作流执行。请再次调用 workflow.run，且只提交 "
            f"approval_id={answer.get('approval_id')} 和 "
            f"request_fingerprint={answer.get('request_fingerprint')}。"
        )
    if isinstance(answer, dict):
        label = answer.get("label") or answer.get("value")
        candidate = answer.get("candidate")
        if candidate:
            return f"用户已完成澄清，选择：{label}；候选事实：{candidate}"
        return f"用户已完成澄清，选择：{label}"
    return f"用户已完成澄清，回答：{answer}"
