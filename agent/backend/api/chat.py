"""
对话 API - 支持流式输出
"""
import asyncio
from fastapi import APIRouter, Depends, HTTPException, Request
from fastapi.responses import StreamingResponse
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy import select
from pydantic import BaseModel
from typing import Optional
import json
from database import get_db, AsyncSessionLocal
from models.session import Session
from models.message import Message
from agents.main_agent import stream_agent_response, MSG_TYPE_PROGRESS, MSG_TYPE_RESULT
from graph.factory import MSG_TYPE_DAG
from utils.summary import maybe_update_summary

router = APIRouter(prefix="", tags=["chat"])

# 每次请求加载的历史轮数上限（10 轮 = 20 条消息）
_HISTORY_LIMIT = 20


class ChatRequest(BaseModel):
    session_id: Optional[int] = None
    message: str  # 只发当前一条消息，历史由后端从 DB 加载


@router.post("/chat")
async def chat(request: Request, body: ChatRequest, db: AsyncSession = Depends(get_db)):
    """
    发送消息并获取流式回复 (OpenAI 兼容格式)
    支持 Vercel AI SDK useChat 集成
    """
    user_id = request.state.user_id
    tenant_id = request.state.tenant_id
    token = request.state.token

    # 获取或创建会话
    session_id = body.session_id
    if session_id:
        result = await db.execute(
            select(Session).where(Session.id == session_id, Session.user_id == user_id)
        )
        session = result.scalar_one_or_none()
        if not session:
            raise HTTPException(status_code=404, detail="会话不存在")
    else:
        session = Session(user_id=user_id, tenant_id=tenant_id)
        db.add(session)
        await db.commit()
        await db.refresh(session)
        session_id = session.id

    # 保存用户消息
    msg = Message(session_id=session_id, role="user", content=body.message)
    db.add(msg)
    await db.commit()

    # 更新会话标题（取前30字符作为标题）
    if not session.title:
        session.title = body.message[:30]
        await db.commit()

    # 从 DB 加载最近 _HISTORY_LIMIT 条历史（含刚保存的用户消息），时间正序
    history_result = await db.execute(
        select(Message)
        .where(Message.session_id == session_id)
        .order_by(Message.created_at.desc())
        .limit(_HISTORY_LIMIT)
    )
    history_msgs = list(reversed(history_result.scalars().all()))
    messages = [{"role": m.role, "content": m.content} for m in history_msgs]

    # 加载会话级历史摘要（历史超出窗口时由后台压缩生成）
    session_summary: Optional[str] = session.summary or None

    async def generate():
        full_response = ""
        dag_data = None
        try:
            async for msg_type, content in stream_agent_response(
                messages, user_id, tenant_id, token, session_summary=session_summary
            ):
                if not content:
                    continue
                # DAG 事件用特殊前缀发送
                if msg_type == MSG_TYPE_DAG:
                    yield f'dag:{content}\n'
                    dag_data = content
                else:
                    # 其他内容正常发送
                    yield f'0:{json.dumps(content, ensure_ascii=False)}\n'
                # 只有最终结果才累积，用于存 DB
                if msg_type == MSG_TYPE_RESULT:
                    full_response += content
        finally:
            # 存储消息（包含 DAG 数据）
            if full_response or dag_data:
                async with AsyncSessionLocal() as save_db:
                    async with save_db.begin():
                        ai_msg = Message(
                            session_id=session_id,
                            role="assistant",
                            content=full_response or "已生成工作流",
                            result_type="dag" if dag_data else "text",
                            result_data=json.loads(dag_data) if dag_data else None,
                        )
                        save_db.add(ai_msg)
                # 后台触发摘要更新
                asyncio.create_task(maybe_update_summary(session_id))

    return StreamingResponse(
        generate(),
        media_type="text/plain",
        headers={
            "X-Session-Id": str(session_id),
            "Cache-Control": "no-cache",
        },
    )


@router.get("/sessions/{session_id}/messages")
async def get_messages(session_id: int, request: Request, db: AsyncSession = Depends(get_db)):
    """获取会话消息历史"""
    result = await db.execute(
        select(Session).where(
            Session.id == session_id,
            Session.user_id == request.state.user_id,
        )
    )
    session = result.scalar_one_or_none()
    if not session:
        raise HTTPException(status_code=404, detail="会话不存在")

    msg_result = await db.execute(
        select(Message)
        .where(Message.session_id == session_id)
        .order_by(Message.created_at.asc())
    )
    messages = msg_result.scalars().all()

    return [
        {
            "id": m.id,
            "role": m.role,
            "content": m.content,
            "result_type": m.result_type,
            "result_data": m.result_data,
            "created_at": m.created_at.isoformat(),
        }
        for m in messages
    ]
