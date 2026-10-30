"""Session organization and read-only workspace discovery."""
from datetime import datetime
from typing import Annotated

from fastapi import APIRouter, Depends, Query, Request
from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator
from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession

from api.runs import ErrorResponse
from authorization_permissions_generated import AGENT_SESSION_CREATE, AGENT_SESSION_DELETE, AGENT_SESSION_READ, AGENT_SESSION_UPDATE, AGENT_RUN_READ
from database import get_db
from i18n import workspace_message
from middleware.auth import require_permissions
from models.session import Session
from models.run import AgentRun
from services.sessions import owned_session, public_session, scope, version_conflict

router = APIRouter(tags=['会话管理 | Sessions'])


class SessionResponse(BaseModel):
    id: int
    title: str | None
    version: int
    pinned: bool
    archived: bool
    created_at: datetime
    updated_at: datetime


class SessionPage(BaseModel):
    data: list[SessionResponse]
    total: int
    page: int
    page_size: int
    total_pages: int


class SessionCreate(BaseModel):
    model_config = ConfigDict(extra='forbid')
    title: str | None = Field(default=None, min_length=1, max_length=255)


class VersionInput(BaseModel):
    model_config = ConfigDict(extra='forbid')
    version: int = Field(gt=0, strict=True, description='读取时的会话版本 | Observed session version')


class SessionUpdate(VersionInput):
    title: str | None = Field(default=None, min_length=1, max_length=255)
    pinned: bool | None = Field(default=None, strict=True)
    archived: bool | None = Field(default=None, strict=True)

    @field_validator('title')
    @classmethod
    def clean_title(cls, value):
        if value is None or not value.strip():
            raise ValueError('title must not be empty')
        return value.strip()

    @model_validator(mode='after')
    def require_change(self):
        if not self.model_fields_set - {'version'}:
            raise ValueError('at least one session field is required')
        if any(getattr(self, key) is None for key in self.model_fields_set - {'version'}):
            raise ValueError('session fields cannot be null')
        return self


def auth(permission):
    return {'x-addp-auth-mode': 'permission', 'x-addp-required-permissions': [permission]}


@router.get('/sessions', response_model=SessionPage, summary='分页查询会话 | List sessions',
            description='标题按字面量子串检索，默认未归档，置顶优先。 | Literal title search; active sessions by default, pinned first.',
            dependencies=[Depends(require_permissions(AGENT_SESSION_READ))], openapi_extra=auth(AGENT_SESSION_READ))
async def list_sessions(request: Request, db: AsyncSession = Depends(get_db),
                        page: Annotated[int, Query(ge=1)] = 1,
                        page_size: Annotated[int, Query(ge=1, le=100)] = 20,
                        search: Annotated[str, Query(max_length=200)] = '', archived: bool = False):
    conditions = [*scope(request), Session.archived == archived]
    if search:
        conditions.append(Session.title.contains(search, autoescape=True))
    total = int((await db.execute(select(func.count()).select_from(Session).where(*conditions))).scalar_one())
    rows = (await db.execute(select(Session).where(*conditions)
            .order_by(Session.pinned.desc(), Session.updated_at.desc(), Session.id.desc())
            .offset((page - 1) * page_size).limit(page_size))).scalars().all()
    return dict(data=[public_session(row) for row in rows], total=total, page=page, page_size=page_size,
                total_pages=(total + page_size - 1) // page_size)


@router.post('/sessions', response_model=SessionResponse, status_code=201, summary='创建会话 | Create session',
             dependencies=[Depends(require_permissions(AGENT_SESSION_CREATE))], openapi_extra=auth(AGENT_SESSION_CREATE))
async def create_session(request: Request, body: SessionCreate, db: AsyncSession = Depends(get_db)):
    session = Session(user_id=request.state.principal_id, tenant_id=request.state.tenant_id, title=body.title)
    db.add(session)
    await db.commit()
    await db.refresh(session)
    return public_session(session)


@router.get('/sessions/{session_id}', response_model=SessionResponse, summary='获取会话 | Get session',
            responses={404: {'model': ErrorResponse, 'description': '会话不存在 | Session not found'}},
            dependencies=[Depends(require_permissions(AGENT_SESSION_READ))], openapi_extra=auth(AGENT_SESSION_READ))
async def get_session(session_id: int, request: Request, db: AsyncSession = Depends(get_db)):
    return public_session(await owned_session(db, session_id, request))


@router.patch('/sessions/{session_id}', response_model=SessionResponse, summary='更新会话组织 | Update session organization',
              description='按版本修改标题、置顶或归档；归档不取消已接纳的运行。 | Versioned title, pin or archive update; archiving does not cancel admitted runs.',
              responses={404: {'model': ErrorResponse, 'description': '会话不存在 | Session not found'}, 409: {'model': ErrorResponse, 'description': '会话版本冲突 | Session version conflict'}},
              dependencies=[Depends(require_permissions(AGENT_SESSION_UPDATE))], openapi_extra=auth(AGENT_SESSION_UPDATE))
async def update_session(session_id: int, request: Request, body: SessionUpdate, db: AsyncSession = Depends(get_db)):
    session = await owned_session(db, session_id, request, lock=True)
    if session.version != body.version:
        return version_conflict(request)
    for name, value in body.model_dump(exclude_unset=True, exclude={'version'}).items():
        setattr(session, name, value)
    session.version += 1
    await db.commit()
    await db.refresh(session)
    return public_session(session)


@router.delete('/sessions/{session_id}', summary='删除会话 | Delete session',
               responses={404: {'model': ErrorResponse, 'description': '会话不存在 | Session not found'}, 409: {'model': ErrorResponse, 'description': '会话版本冲突 | Session version conflict'}},
               dependencies=[Depends(require_permissions(AGENT_SESSION_DELETE))], openapi_extra=auth(AGENT_SESSION_DELETE))
async def delete_session(session_id: int, request: Request, body: VersionInput, db: AsyncSession = Depends(get_db)):
    session = await owned_session(db, session_id, request, lock=True)
    if session.version != body.version:
        return version_conflict(request)
    await db.delete(session)
    await db.commit()
    return {'message': workspace_message(request.headers.get('accept-language'), 'session_deleted')}


class SkillResponse(BaseModel):
    name: str
    description: str


@router.get('/skills', response_model=list[SkillResponse], summary='列出已登记 Skill | List registered Skills',
            description='方法目录不代表当前权限或运行可用性。 | Method catalog does not imply permission or runtime availability.',
            dependencies=[Depends(require_permissions(AGENT_SESSION_READ))], openapi_extra=auth(AGENT_SESSION_READ))
async def list_skills():
    from agents.main_agent import _get_skill_registry
    return [dict(name=skill.name, description=skill.description) for skill in _get_skill_registry().values()]


class RunSummary(BaseModel):
    id: str
    status: str
    skill_name: str | None
    created_at: datetime
    updated_at: datetime
    completed_at: datetime | None
    metrics: dict
    error_source: str | None
    error_code: str | None
    error_message: str | None


class RunPage(BaseModel):
    data: list[RunSummary]
    total: int
    page: int
    page_size: int
    total_pages: int


@router.get('/sessions/{session_id}/runs', response_model=RunPage, summary='分页查询会话运行 | List session runs',
            responses={404: {'model': ErrorResponse, 'description': '会话不存在 | Session not found'}},
            description='仅当前 User/Tenant 的会话运行；不返回检查点或工具原始结果。 | Current User/Tenant session runs without checkpoints or raw tool results.',
            dependencies=[Depends(require_permissions(AGENT_RUN_READ))], openapi_extra=auth(AGENT_RUN_READ))
async def list_session_runs(session_id: int, request: Request, db: AsyncSession = Depends(get_db),
                            page: Annotated[int, Query(ge=1)] = 1, page_size: Annotated[int, Query(ge=1, le=100)] = 20):
    await owned_session(db, session_id, request)
    conditions = (AgentRun.session_id == session_id, AgentRun.user_id == request.state.principal_id,
                  AgentRun.tenant_id == request.state.tenant_id)
    total = int((await db.execute(select(func.count()).select_from(AgentRun).where(*conditions))).scalar_one())
    rows = (await db.execute(select(AgentRun).where(*conditions).order_by(AgentRun.created_at.desc(), AgentRun.id.desc())
                            .offset((page-1)*page_size).limit(page_size))).scalars().all()
    fields = ('status','skill_name','created_at','updated_at','completed_at','metrics','error_source','error_code','error_message')
    return dict(data=[dict(id=str(row.id), **{name:getattr(row,name) for name in fields}) for row in rows], total=total,
                page=page,page_size=page_size,total_pages=(total+page_size-1)//page_size)
