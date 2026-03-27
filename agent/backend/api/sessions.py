"""
会话管理 API
"""
from fastapi import APIRouter, Depends, HTTPException, Request, status
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select
from pydantic import BaseModel
from typing import Optional, List
from database import get_db
from models.session import Session

router = APIRouter(prefix="/api/v1/agent/sessions", tags=["sessions"])


class SessionCreate(BaseModel):
    title: Optional[str] = None


class SessionResponse(BaseModel):
    id: int
    title: Optional[str]
    created_at: str
    updated_at: str

    class Config:
        from_attributes = True


@router.get("")
async def list_sessions(request: Request, db: AsyncSession = Depends(get_db)):
    """获取当前用户的会话列表"""
    user_id = request.state.user_id
    result = await db.execute(
        select(Session)
        .where(Session.user_id == user_id)
        .order_by(Session.updated_at.desc())
    )
    sessions = result.scalars().all()
    return [
        {
            "id": s.id,
            "title": s.title or "新对话",
            "created_at": s.created_at.isoformat(),
            "updated_at": s.updated_at.isoformat(),
        }
        for s in sessions
    ]


@router.post("", status_code=201)
async def create_session(request: Request, body: SessionCreate, db: AsyncSession = Depends(get_db)):
    """创建新会话"""
    session = Session(
        user_id=request.state.user_id,
        tenant_id=request.state.tenant_id,
        title=body.title,
    )
    db.add(session)
    await db.commit()
    await db.refresh(session)
    return {
        "id": session.id,
        "title": session.title or "新对话",
        "created_at": session.created_at.isoformat(),
    }


@router.get("/{session_id}")
async def get_session(session_id: int, request: Request, db: AsyncSession = Depends(get_db)):
    """获取会话详情"""
    result = await db.execute(
        select(Session).where(
            Session.id == session_id,
            Session.user_id == request.state.user_id,
        )
    )
    session = result.scalar_one_or_none()
    if not session:
        raise HTTPException(status_code=404, detail="会话不存在")
    return {
        "id": session.id,
        "title": session.title or "新对话",
        "created_at": session.created_at.isoformat(),
        "updated_at": session.updated_at.isoformat(),
    }


@router.delete("/{session_id}")
async def delete_session(session_id: int, request: Request, db: AsyncSession = Depends(get_db)):
    """删除会话"""
    result = await db.execute(
        select(Session).where(
            Session.id == session_id,
            Session.user_id == request.state.user_id,
        )
    )
    session = result.scalar_one_or_none()
    if not session:
        raise HTTPException(status_code=404, detail="会话不存在")
    await db.delete(session)
    await db.commit()
    return {"message": "删除成功"}
