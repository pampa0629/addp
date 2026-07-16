"""AG-UI 对话接口与消息历史。"""

import asyncio
import copy
import json
import logging
import uuid
from typing import Any
from uuid import UUID

from ag_ui.core import (
    ActivitySnapshotEvent,
    Interrupt,
    RunAgentInput,
    RunErrorEvent,
    RunFinishedEvent,
    RunFinishedInterruptOutcome,
    RunFinishedSuccessOutcome,
    RunStartedEvent,
    StateSnapshotEvent,
    TextMessageContentEvent,
    TextMessageEndEvent,
    TextMessageStartEvent,
    ToolCallArgsEvent,
    ToolCallEndEvent,
    ToolCallResultEvent,
    ToolCallStartEvent,
)
from ag_ui.encoder import EventEncoder
from fastapi import APIRouter, Depends, HTTPException, Request, status
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession

from agents.main_agent import stream_agent_response
from agents.context import HISTORY_MESSAGE_LIMIT, build_context_window
from database import AsyncSessionLocal, get_db
from models.interaction import Interaction
from models.message import Message
from models.run import AgentRun
from models.session import Session
from protocol.a2ui import clarification_surface, workflow_dag_surface
from services.interactions import (
    InteractionAnswerError,
    InteractionNotFoundError,
    InteractionStateError,
    create_clarification,
    format_resume_message,
    resolve_interaction,
)
from services.run_events import append_run_event
from services.runs import (
    AgentRunNotFoundError,
    AgentRunStateError,
    attach_step_facts,
    complete_run_step,
    create_agent_run,
    create_run_step,
    resume_agent_run,
    retry_agent_run,
    refresh_run_metrics,
    set_run_context_metrics,
    set_run_skill,
    set_run_status,
    summarize_tool_result,
    update_run_checkpoint,
)
from services.runtime import run_cancellation_registry
from utils.summary import maybe_update_summary

router = APIRouter(prefix="", tags=["对话 | Chat"])
logger = logging.getLogger(__name__)

_A2UI_ACTIVITY_TYPE = "a2ui-surface"


class RetryAgentRunInput(BaseModel):
    model_config = ConfigDict(populate_by_name=True)

    run_id: str = Field(alias="runId", min_length=1, max_length=100)


def _plain_text(content: Any) -> str:
    if isinstance(content, str):
        return content.strip()
    if isinstance(content, list):
        values = [part.get("text", "") for part in content if isinstance(part, dict) and part.get("type") == "text"]
        return "\n".join(value for value in values if value).strip()
    return ""


async def _get_owned_session(db: AsyncSession, session_id: int, user_id: int) -> Session:
    result = await db.execute(
        select(Session).where(Session.id == session_id, Session.user_id == user_id)
    )
    session = result.scalar_one_or_none()
    if session is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="会话不存在")
    return session


async def _save_user_input(
    db: AsyncSession,
    *,
    body: RunAgentInput,
    session: Session,
    user_id: int,
    tenant_id: int,
) -> list[Interaction]:
    if body.resume:
        resolved_interactions: list[Interaction] = []
        for entry in body.resume:
            if entry.status != "resolved":
                raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="澄清已取消")
            try:
                interaction = await resolve_interaction(
                    db,
                    interaction_id=entry.interrupt_id,
                    session_id=session.id,
                    user_id=user_id,
                    tenant_id=tenant_id,
                    payload=entry.payload,
                )
            except InteractionNotFoundError as exc:
                raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="交互请求不存在") from exc
            except InteractionStateError as exc:
                raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="交互请求已处理") from exc
            except InteractionAnswerError as exc:
                raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail=str(exc)) from exc

            resume_text = format_resume_message(interaction)
            db.add(
                Message(
                    session_id=session.id,
                    role="user",
                    content=resume_text,
                    protocol_message_id=f"resume:{interaction.id}",
                    parts=[{"type": "text", "text": resume_text}],
                )
            )
            resolved_interactions.append(interaction)
        session.updated_at = func.now()
        return resolved_interactions

    user_messages = [message for message in body.messages if message.role == "user"]
    if not user_messages:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="缺少用户消息")
    latest = user_messages[-1]
    content = _plain_text(latest.content)
    if not content:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="用户消息不能为空")

    existing = await db.execute(
        select(Message.id).where(
            Message.session_id == session.id,
            Message.protocol_message_id == latest.id,
        )
    )
    if existing.scalar_one_or_none() is not None:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="消息已经处理")

    db.add(
        Message(
            session_id=session.id,
            role="user",
            content=content,
            protocol_message_id=latest.id,
            parts=[{"type": "text", "text": content}],
        )
    )
    if not session.title:
        session.title = content[:30]
    session.updated_at = func.now()
    return []


async def _load_agent_history(db: AsyncSession, session_id: int) -> tuple[list[dict[str, Any]], int]:
    total_result = await db.execute(
        select(func.count(Message.id)).where(Message.session_id == session_id)
    )
    result = await db.execute(
        select(Message)
        .where(Message.session_id == session_id)
        .order_by(Message.created_at.desc(), Message.id.desc())
        .limit(HISTORY_MESSAGE_LIMIT)
    )
    rows = list(reversed(result.scalars().all()))
    messages = [
        {"role": row.role, "content": row.content}
        for row in rows
        if row.role in {"user", "assistant"} and row.content
    ]
    return messages, int(total_result.scalar_one())


async def _save_assistant_message(
    *,
    session_id: int,
    message_id: str,
    content: str,
    parts: list[dict[str, Any]],
) -> None:
    if not content and not parts:
        return
    async with AsyncSessionLocal() as db:
        async with db.begin():
            db.add(
                Message(
                    session_id=session_id,
                    role="assistant",
                    content=content,
                    protocol_message_id=message_id,
                    parts=parts,
                )
            )
    asyncio.create_task(maybe_update_summary(session_id))


@router.post(
    "/chat",
    summary="运行智能体 | Run Agent",
    description="接收标准 AG-UI RunAgentInput，并以 text/event-stream 返回 AG-UI 事件。",
    openapi_extra={
        "x-ai-hint": "使用 threadId 指定已有 ADDP Agent 会话；messages 中最后一条 user 消息是本次新输入。"
    },
    response_class=StreamingResponse,
    responses={200: {"content": {"text/event-stream": {}}}},
)
async def chat(request: Request, body: RunAgentInput, db: AsyncSession = Depends(get_db)):
    try:
        session_id = int(body.thread_id)
    except (TypeError, ValueError) as exc:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="threadId 必须是会话 ID") from exc

    user_id = int(request.state.user_id)
    tenant_id = int(request.state.tenant_id)
    session = await _get_owned_session(db, session_id, user_id)
    if session.tenant_id != tenant_id:
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="无权访问该会话")

    retry_run_id = getattr(request.state, "agent_run_retry_id", None)
    try:
        if retry_run_id is not None:
            agent_run = await retry_agent_run(
                db,
                agent_run_id=UUID(str(retry_run_id)),
                session_id=session_id,
                user_id=user_id,
                tenant_id=tenant_id,
            )
        else:
            resolved_interactions = await _save_user_input(
                db,
                body=body,
                session=session,
                user_id=user_id,
                tenant_id=tenant_id,
            )
            if resolved_interactions:
                agent_run = await resume_agent_run(
                    db,
                    interactions=resolved_interactions,
                    session_id=session_id,
                    user_id=user_id,
                    tenant_id=tenant_id,
                )
            else:
                agent_run = await create_agent_run(
                    db,
                    session_id=session_id,
                    user_id=user_id,
                    tenant_id=tenant_id,
                    protocol_run_id=body.run_id,
                )
    except AgentRunNotFoundError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="AgentRun 不存在") from exc
    except (AgentRunStateError, ValueError) as exc:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail=str(exc)) from exc
    await db.commit()
    raw_history, total_message_count = await _load_agent_history(db, session_id)
    history, session_summary, context_metrics = build_context_window(
        raw_history,
        session.summary or None,
        input_message_count=total_message_count,
    )
    agent_run_id = agent_run.id
    run_checkpoint = copy.deepcopy(agent_run.checkpoint)
    encoder = EventEncoder(request.headers.get("accept"))
    async with AsyncSessionLocal() as metrics_db:
        async with metrics_db.begin():
            await set_run_context_metrics(
                metrics_db,
                agent_run_id=agent_run_id,
                context_metrics=context_metrics,
            )

    async def generate():
        assistant_message_id = str(uuid.uuid4())
        full_text = ""
        parts: list[dict[str, Any]] = []
        text_started = False
        tool_steps: dict[str, Any] = {}
        cancellation_signal = run_cancellation_registry.activate(agent_run_id)

        async def emit(protocol_event: Any) -> str:
            event_payload = protocol_event.model_dump(mode="json", by_alias=True, exclude_none=True)
            async with AsyncSessionLocal() as event_db:
                async with event_db.begin():
                    persisted = await append_run_event(
                        event_db,
                        agent_run_id=agent_run_id,
                        protocol_invocation_id=body.run_id,
                        event_payload=event_payload,
                    )
            if event_payload.get("type") in {"RUN_FINISHED", "RUN_ERROR"}:
                async with AsyncSessionLocal() as metrics_db:
                    async with metrics_db.begin():
                        await refresh_run_metrics(metrics_db, agent_run_id=agent_run_id)
            encoded = encoder.encode(protocol_event)
            return f"id: {persisted.sequence}\n{encoded}" if persisted is not None else encoded

        def append_result_ref(result_ref: Any) -> None:
            if not isinstance(result_ref, dict):
                return
            if not all(isinstance(result_ref.get(key), str) and result_ref[key] for key in ("schema", "owner_module", "kind", "ref")):
                return
            part = {"type": "result_ref", **result_ref}
            if part not in parts:
                parts.append(part)

        yield await emit(
            RunStartedEvent(thread_id=body.thread_id, run_id=body.run_id, parent_run_id=body.parent_run_id)
        )
        yield await emit(
            StateSnapshotEvent(
                snapshot={"sessionId": session_id, "agentRunId": str(agent_run_id), "status": "running"}
            )
        )

        try:
            async for event in stream_agent_response(
                history,
                user_id,
                tenant_id,
                request.state.token,
                session_summary=session_summary,
            checkpoint=run_checkpoint,
        ):
                if await request.is_disconnected():
                    async with AsyncSessionLocal() as run_db:
                        async with run_db.begin():
                            await set_run_status(
                                run_db,
                                agent_run_id=agent_run_id,
                                status="failed",
                                error_source="client",
                                error_code="client_disconnected",
                                error_message="客户端流连接已断开",
                            )
                            await append_run_event(
                                run_db,
                                agent_run_id=agent_run_id,
                                protocol_invocation_id=body.run_id,
                                event_payload=StateSnapshotEvent(
                                    snapshot={
                                        "sessionId": session_id,
                                        "agentRunId": str(agent_run_id),
                                        "status": "failed",
                                    }
                                ).model_dump(mode="json", by_alias=True, exclude_none=True),
                            )
                            await append_run_event(
                                run_db,
                                agent_run_id=agent_run_id,
                                protocol_invocation_id=body.run_id,
                                event_payload=RunErrorEvent(
                                    message="客户端流连接已断开，请重试该 AgentRun",
                                    code="ClientDisconnected",
                                ).model_dump(mode="json", by_alias=True, exclude_none=True),
                            )
                            await refresh_run_metrics(run_db, agent_run_id=agent_run_id)
                    return
                if cancellation_signal.is_set():
                    return

                if event.kind == "text":
                    delta = str(event.payload.get("delta") or "")
                    if not delta:
                        continue
                    if not text_started:
                        text_started = True
                        yield await emit(
                            TextMessageStartEvent(message_id=assistant_message_id, role="assistant")
                        )
                    full_text += delta
                    yield await emit(
                        TextMessageContentEvent(message_id=assistant_message_id, delta=delta)
                    )
                    continue

                if event.kind == "run_state":
                    async with AsyncSessionLocal() as run_db:
                        async with run_db.begin():
                            await set_run_skill(
                                run_db,
                                agent_run_id=agent_run_id,
                                skill_name=event.payload.get("skill_name"),
                            )
                    continue

                if event.kind == "tool_start":
                    tool_call_id = str(event.payload["tool_call_id"])
                    async with AsyncSessionLocal() as run_db:
                        async with run_db.begin():
                            step = await create_run_step(
                                run_db,
                                agent_run_id=agent_run_id,
                                protocol_invocation_id=body.run_id,
                                step_type="tool_call",
                                tool_call_id=tool_call_id,
                                tool_name=str(event.payload["tool_name"]),
                                input_data=event.payload.get("args") or {},
                            )
                            tool_steps[tool_call_id] = step.id
                    yield await emit(
                        ToolCallStartEvent(
                            tool_call_id=tool_call_id,
                            tool_call_name=str(event.payload["tool_name"]),
                            parent_message_id=assistant_message_id,
                        )
                    )
                    yield encoder.encode(
                        ToolCallArgsEvent(
                            tool_call_id=tool_call_id,
                            delta=json.dumps(event.payload.get("args") or {}, ensure_ascii=False),
                        )
                    )
                    yield encoder.encode(ToolCallEndEvent(tool_call_id=tool_call_id))
                    continue

                if event.kind == "checkpoint":
                    tool_call_id = str(event.payload.get("tool_call_id") or "")
                    async with AsyncSessionLocal() as run_db:
                        async with run_db.begin():
                            await update_run_checkpoint(
                                run_db,
                                agent_run_id=agent_run_id,
                                checkpoint=event.payload["checkpoint"],
                            )
                            if tool_call_id in tool_steps:
                                await attach_step_facts(
                                    run_db,
                                    step_id=tool_steps[tool_call_id],
                                    facts=event.payload.get("facts") or {},
                                )
                    continue

                if event.kind == "result_ref":
                    append_result_ref(event.payload.get("result_ref"))
                    continue

                if event.kind == "tool_result":
                    tool_call_id = str(event.payload["tool_call_id"])
                    if tool_call_id in tool_steps:
                        async with AsyncSessionLocal() as run_db:
                            async with run_db.begin():
                                await complete_run_step(
                                    run_db,
                                    step_id=tool_steps[tool_call_id],
                                    status="failed" if event.payload.get("is_error") else "completed",
                                    output_summary=summarize_tool_result(
                                        str(event.payload.get("content") or "")
                                    ),
                                    error_source=event.payload.get("error_source"),
                                    error_code=event.payload.get("error_code"),
                                    error_message=(
                                        str(event.payload.get("content") or "")
                                        if event.payload.get("is_error")
                                        else None
                                    ),
                                )
                    yield await emit(
                        ToolCallResultEvent(
                            message_id=str(uuid.uuid4()),
                            tool_call_id=tool_call_id,
                            content=str(event.payload.get("content") or ""),
                            role="tool",
                        )
                    )
                    continue

                if event.kind == "presentation" and event.payload.get("kind") == "workflow_dag":
                    surface_id = f"surface:{uuid.uuid4()}"
                    operations = workflow_dag_surface(surface_id, event.payload["workflow"])
                    content = {"operations": operations}
                    parts.append(
                        {
                            "type": "presentation_ref",
                            "protocol": "a2ui",
                            "catalog_id": "addp.catalog/v1",
                            "surface_id": surface_id,
                            "activity_type": _A2UI_ACTIVITY_TYPE,
                            "content": content,
                        }
                    )
                    yield await emit(
                        ActivitySnapshotEvent(
                            message_id=f"activity:{uuid.uuid4()}",
                            activity_type=_A2UI_ACTIVITY_TYPE,
                            content=content,
                        )
                    )
                    continue

                if event.kind == "interaction_required":
                    async with AsyncSessionLocal() as interaction_db:
                        async with interaction_db.begin():
                            interaction = await create_clarification(
                                interaction_db,
                                session_id=session_id,
                                user_id=user_id,
                                tenant_id=tenant_id,
                                agent_run_id=agent_run_id,
                                tool_call_id=event.payload.get("tool_call_id"),
                                prompt=str(event.payload.get("prompt") or "请选择数据源"),
                                candidates=event.payload.get("candidates") or [],
                            )
                            await set_run_status(
                                interaction_db,
                                agent_run_id=agent_run_id,
                                status="waiting",
                            )
                            interaction_id = str(interaction.id)
                            interaction_options = list(interaction.options)
                            interaction_prompt = interaction.prompt

                    surface_id = f"surface:{uuid.uuid4()}"
                    operations = clarification_surface(
                        surface_id,
                        interaction_id=interaction_id,
                        prompt=interaction_prompt,
                        options=interaction_options,
                    )
                    content = {"operations": operations}
                    parts.extend(
                        [
                            {
                                "type": "interaction_ref",
                                "interaction_id": interaction_id,
                                "kind": "clarification",
                                "owner": "agent",
                                "status": "pending",
                            },
                            {
                                "type": "presentation_ref",
                                "protocol": "a2ui",
                                "catalog_id": "addp.catalog/v1",
                                "surface_id": surface_id,
                                "interaction_id": interaction_id,
                                "activity_type": _A2UI_ACTIVITY_TYPE,
                                "content": content,
                            },
                        ]
                    )
                    yield await emit(
                        ActivitySnapshotEvent(
                            message_id=f"activity:{uuid.uuid4()}",
                            activity_type=_A2UI_ACTIVITY_TYPE,
                            content=content,
                        )
                    )
                    if text_started:
                        yield await emit(TextMessageEndEvent(message_id=assistant_message_id))
                    if full_text:
                        parts.insert(0, {"type": "text", "text": full_text})
                    await _save_assistant_message(
                        session_id=session_id,
                        message_id=assistant_message_id,
                        content=full_text or interaction_prompt,
                        parts=parts,
                    )
                    interrupt = Interrupt(
                        id=interaction_id,
                        reason=str(event.payload.get("reason") or "missing_input"),
                        message=interaction_prompt,
                        tool_call_id=event.payload.get("tool_call_id"),
                        response_schema=interaction.response_schema,
                    )
                    yield await emit(
                        StateSnapshotEvent(
                            snapshot={
                                "sessionId": session_id,
                                "agentRunId": str(agent_run_id),
                                "status": "waiting",
                            }
                        )
                    )
                    yield await emit(
                        RunFinishedEvent(
                            thread_id=body.thread_id,
                            run_id=body.run_id,
                            outcome=RunFinishedInterruptOutcome(interrupts=[interrupt]),
                        )
                    )
                    return

            if text_started:
                yield await emit(TextMessageEndEvent(message_id=assistant_message_id))
            if full_text:
                parts.insert(0, {"type": "text", "text": full_text})
            await _save_assistant_message(
                session_id=session_id,
                message_id=assistant_message_id,
                content=full_text,
                parts=parts,
            )
            async with AsyncSessionLocal() as run_db:
                async with run_db.begin():
                    await set_run_status(run_db, agent_run_id=agent_run_id, status="completed")
            yield await emit(
                StateSnapshotEvent(
                    snapshot={
                        "sessionId": session_id,
                        "agentRunId": str(agent_run_id),
                        "status": "completed",
                    }
                )
            )
            yield await emit(
                RunFinishedEvent(
                    thread_id=body.thread_id,
                    run_id=body.run_id,
                    outcome=RunFinishedSuccessOutcome(),
                )
            )
        except Exception as exc:
            logger.exception(
                "智能体运行失败: session_id=%s run_id=%s",
                session_id,
                body.run_id,
            )
            async with AsyncSessionLocal() as run_db:
                async with run_db.begin():
                    await set_run_status(
                        run_db,
                        agent_run_id=agent_run_id,
                        status="failed",
                        error_source="runtime",
                        error_code="runtime_exception",
                        error_message=str(exc),
                    )
            yield await emit(RunErrorEvent(message="智能体运行失败", code=type(exc).__name__))
        finally:
            run_cancellation_registry.release(agent_run_id, cancellation_signal)

    return StreamingResponse(
        generate(),
        media_type=encoder.get_content_type(),
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


@router.post(
    "/runs/{agent_run_id}/retry",
    summary="重试智能体运行 | Retry agent run",
    description="仅重试失败的 AgentRun，并以新的协议调用 ID 返回 AG-UI SSE；不会创建第二个 AgentRun。",
    response_class=StreamingResponse,
    responses={200: {"content": {"text/event-stream": {}}}},
    openapi_extra={
        "x-ai-hint": "runId 是本次重试的协议调用 ID；仅接受 failed AgentRun，取消态不允许重试。"
    },
)
async def retry_chat(
    agent_run_id: UUID,
    request: Request,
    body: RetryAgentRunInput,
    db: AsyncSession = Depends(get_db),
):
    user_id = int(request.state.user_id)
    tenant_id = int(request.state.tenant_id)
    result = await db.execute(
        select(AgentRun).where(
            AgentRun.id == agent_run_id,
            AgentRun.user_id == user_id,
            AgentRun.tenant_id == tenant_id,
        )
    )
    run = result.scalar_one_or_none()
    if run is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="AgentRun 不存在")

    request.state.agent_run_retry_id = str(agent_run_id)
    retry_body = RunAgentInput.model_validate(
        {
            "threadId": str(run.session_id),
            "runId": body.run_id,
            "state": {},
            "messages": [],
            "tools": [],
            "context": [],
            "forwardedProps": {},
        }
    )
    return await chat(request, retry_body, db)


@router.get(
    "/sessions/{session_id}/messages",
    summary="获取会话消息 | List Session Messages",
)
async def get_messages(session_id: int, request: Request, db: AsyncSession = Depends(get_db)):
    await _get_owned_session(db, session_id, int(request.state.user_id))

    msg_result = await db.execute(
        select(Message)
        .where(Message.session_id == session_id)
        .order_by(Message.created_at.asc(), Message.id.asc())
    )
    messages = msg_result.scalars().all()
    interaction_result = await db.execute(
        select(Interaction).where(
            Interaction.session_id == session_id,
            Interaction.user_id == int(request.state.user_id),
        )
    )
    interaction_status = {
        str(interaction.id): interaction.status for interaction in interaction_result.scalars().all()
    }

    response = []
    for message in messages:
        parts = copy.deepcopy(message.parts or [])
        for part in parts:
            if part.get("type") == "interaction_ref":
                part["status"] = interaction_status.get(part.get("interaction_id"), part.get("status"))
        response.append(
            {
                "id": message.id,
                "protocol_message_id": message.protocol_message_id,
                "role": message.role,
                "content": message.content,
                "parts": parts,
                "created_at": message.created_at.isoformat(),
            }
        )
    return response
