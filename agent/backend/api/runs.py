from datetime import datetime
from typing import Any
from uuid import UUID

from ag_ui.core import RunErrorEvent, StateSnapshotEvent
from fastapi import APIRouter, Depends, Query, Request, status
from fastapi.responses import JSONResponse, StreamingResponse
from pydantic import BaseModel
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from authorization_permissions_generated import AGENT_RUN_CANCEL, AGENT_RUN_READ
from database import AsyncSessionLocal, get_db
from middleware.auth import require_permissions
from models.run import AgentRun
from models.run_step import AgentRunStep
from services.run_events import append_run_event, encode_replayed_event, iter_run_events
from services.runs import AgentRunStateError, cancel_agent_run, refresh_run_metrics
from services.runtime import run_cancellation_registry


router = APIRouter(prefix="/runs", tags=["智能体运行 | Agent Runs"])


class AgentRunStepResponse(BaseModel):
    id: UUID
    sequence: int
    step_type: str
    status: str
    protocol_invocation_id: str
    tool_call_id: str | None
    tool_name: str | None
    input: dict[str, Any]
    output_summary: str | None
    facts: dict[str, Any]
    error_source: str | None
    error_code: str | None
    error_message: str | None
    started_at: datetime
    completed_at: datetime | None


class AgentRunResponse(BaseModel):
    id: UUID
    session_id: int
    status: str
    initial_protocol_run_id: str
    skill_name: str | None
    checkpoint: dict[str, Any]
    metrics: dict[str, Any]
    context_metrics: dict[str, Any]
    error_source: str | None
    error_code: str | None
    error_message: str | None
    created_at: datetime
    updated_at: datetime
    completed_at: datetime | None
    steps: list[AgentRunStepResponse]


class ErrorResponse(BaseModel):
    error: str


class AgentRunActionResponse(BaseModel):
    id: UUID
    status: str


@router.get(
    "/{agent_run_id}",
    response_model=AgentRunResponse,
    summary="获取智能体运行 | Get agent run",
    description="返回当前用户拥有的 AgentRun、语义检查点和受限步骤审计记录。",
    dependencies=[Depends(require_permissions(AGENT_RUN_READ))],
    responses={404: {"model": ErrorResponse, "description": "AgentRun 不存在 | Agent run not found"}},
    openapi_extra={
        "x-ai-hint": "agent_run_id 来自 AG-UI STATE_SNAPSHOT.agentRunId；该资源不是 owner 模块 execution。",
        "x-addp-auth-mode": "permission",
        "x-addp-required-permissions": [AGENT_RUN_READ],
    },
)
async def get_agent_run(agent_run_id: UUID, request: Request, db: AsyncSession = Depends(get_db)):
    user_id = int(request.state.principal_id)
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
        return JSONResponse(status_code=status.HTTP_404_NOT_FOUND, content={"error": "AgentRun 不存在"})

    step_result = await db.execute(
        select(AgentRunStep)
        .where(AgentRunStep.agent_run_id == run.id)
        .order_by(AgentRunStep.sequence.asc())
    )
    steps = step_result.scalars().all()
    return {
        "id": run.id,
        "session_id": run.session_id,
        "status": run.status,
        "initial_protocol_run_id": run.initial_protocol_run_id,
        "skill_name": run.skill_name,
        "checkpoint": run.checkpoint,
        "metrics": run.metrics,
        "context_metrics": run.context_metrics,
        "error_source": run.error_source,
        "error_code": run.error_code,
        "error_message": run.error_message,
        "created_at": run.created_at,
        "updated_at": run.updated_at,
        "completed_at": run.completed_at,
        "steps": [
            {
                "id": step.id,
                "sequence": step.sequence,
                "step_type": step.step_type,
                "status": step.status,
                "protocol_invocation_id": step.protocol_invocation_id,
                "tool_call_id": step.tool_call_id,
                "tool_name": step.tool_name,
                "input": step.input_data,
                "output_summary": step.output_summary,
                "facts": step.facts,
                "error_source": step.error_source,
                "error_code": step.error_code,
                "error_message": step.error_message,
                "started_at": step.started_at,
                "completed_at": step.completed_at,
            }
            for step in steps
        ],
    }


@router.get(
    "/{agent_run_id}/events",
    summary="重放智能体运行事件 | Replay agent run events",
    description="按 AgentRun 内 sequence 回放当前用户可见的安全 AG-UI 事件。",
    dependencies=[Depends(require_permissions(AGENT_RUN_READ))],
    response_class=StreamingResponse,
    responses={200: {"content": {"text/event-stream": {}}}, 404: {"model": ErrorResponse}},
    openapi_extra={
        "x-ai-hint": "after 是客户端最后已处理的 AgentRunEvent sequence；Tool 参数和原始结果不会出现在重放流中。",
        "x-addp-auth-mode": "permission",
        "x-addp-required-permissions": [AGENT_RUN_READ],
    },
)
async def replay_agent_run_events(
    agent_run_id: UUID,
    request: Request,
    after: int = Query(default=0, ge=0),
    db: AsyncSession = Depends(get_db),
):
    user_id = int(request.state.principal_id)
    tenant_id = int(request.state.tenant_id)
    result = await db.execute(
        select(AgentRun.id).where(
            AgentRun.id == agent_run_id,
            AgentRun.user_id == user_id,
            AgentRun.tenant_id == tenant_id,
        )
    )
    if result.scalar_one_or_none() is None:
        return JSONResponse(status_code=status.HTTP_404_NOT_FOUND, content={"error": "AgentRun 不存在"})

    async def generate():
        async with AsyncSessionLocal() as event_db:
            async for event in iter_run_events(event_db, agent_run_id=agent_run_id, after=after):
                yield encode_replayed_event(event)

    return StreamingResponse(
        generate(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


@router.post(
    "/{agent_run_id}/cancel",
    response_model=AgentRunActionResponse,
    summary="取消智能体运行 | Cancel agent run",
    description="取消内置 Agent Runtime 和待处理 Interaction，不取消 owner 模块 execution。",
    dependencies=[Depends(require_permissions(AGENT_RUN_CANCEL))],
    responses={404: {"model": ErrorResponse}, 409: {"model": ErrorResponse}},
    openapi_extra={
        "x-ai-hint": "只允许 running 或 waiting 的 AgentRun；cancelled 是终态。",
        "x-addp-auth-mode": "permission",
        "x-addp-required-permissions": [AGENT_RUN_CANCEL],
    },
)
async def cancel_owned_agent_run(
    agent_run_id: UUID,
    request: Request,
    db: AsyncSession = Depends(get_db),
):
    user_id = int(request.state.principal_id)
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
        return JSONResponse(status_code=status.HTTP_404_NOT_FOUND, content={"error": "AgentRun 不存在"})
    try:
        await cancel_agent_run(
            db,
            agent_run_id=agent_run_id,
            session_id=run.session_id,
            user_id=user_id,
            tenant_id=tenant_id,
        )
    except AgentRunStateError as exc:
        return JSONResponse(status_code=status.HTTP_409_CONFLICT, content={"error": str(exc)})

    for event in (
        StateSnapshotEvent(snapshot={"sessionId": run.session_id, "agentRunId": str(run.id), "status": "cancelled"}),
        RunErrorEvent(message="智能体运行已取消", code="cancelled"),
    ):
        await append_run_event(
            db,
            agent_run_id=run.id,
            protocol_invocation_id=run.initial_protocol_run_id,
            event_payload=event.model_dump(mode="json", by_alias=True, exclude_none=True),
        )
    await refresh_run_metrics(db, agent_run_id=run.id)
    await db.commit()
    run_cancellation_registry.cancel(run.id)
    return {"id": run.id, "status": "cancelled"}
