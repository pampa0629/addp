"""User/Tenant-owned session organization and admission serialization."""
from datetime import timezone

from fastapi import HTTPException, Request
from fastapi.responses import JSONResponse
from sqlalchemy import select

from i18n import workspace_message
from models.session import Session


def scope(request: Request):
    return (Session.user_id == int(request.state.principal_id), Session.tenant_id == int(request.state.tenant_id))


async def get_owned_session(db, session_id, user_id, tenant_id, *, lock=False, language=None):
    statement = select(Session).where(Session.id == session_id, Session.user_id == user_id, Session.tenant_id == tenant_id)
    if lock:
        statement = statement.with_for_update().execution_options(populate_existing=True)
    session = (await db.execute(statement)).scalar_one_or_none()
    if session is None:
        raise HTTPException(404, workspace_message(language, 'session_missing'))
    return session


async def owned_session(db, session_id: int, request: Request, *, lock=False):
    return await get_owned_session(db, session_id, int(request.state.principal_id), int(request.state.tenant_id),
                                   lock=lock, language=request.headers.get('accept-language'))


def public_session(session):
    def timestamp(value):
        return value.replace(tzinfo=timezone.utc) if value.tzinfo is None else value
    return dict(id=session.id, title=session.title, version=session.version, pinned=session.pinned,
                archived=session.archived, created_at=timestamp(session.created_at), updated_at=timestamp(session.updated_at))


def version_conflict(request):
    return JSONResponse(status_code=409, content={
        'error': workspace_message(request.headers.get('accept-language'), 'version_conflict'),
        'error_code': 'resource_version_conflict',
    })


def archived_response(request):
    return JSONResponse(status_code=409, content={
        'error': workspace_message(request.headers.get('accept-language'), 'session_archived'),
        'error_code': 'session_archived',
    })
