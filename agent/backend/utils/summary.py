"""Incremental session-history summarization with a persisted message watermark."""

import logging

from langchain_core.messages import HumanMessage
from sqlalchemy import func, select

from agents.context import (
    HISTORY_CHAR_LIMIT,
    HISTORY_MESSAGE_LIMIT,
    MESSAGE_CHAR_LIMIT,
    SUMMARY_CHAR_LIMIT,
    truncate_context_text,
)
from database import AsyncSessionLocal
from models.message import Message
from models.session import Session
from utils.llm import get_llm

logger = logging.getLogger("agent.summary")

_COMPRESS_PROMPT = """\
请把已有摘要与新增的早期对话合并为一段简洁中文摘要（不超过 400 字）。
只保留用户目标、已确认的数据或参数、owner Tool 结论和待解决事项；不要补充推测。

已有摘要：
{existing_summary}

新增早期对话：
{history}

输出合并后的摘要（直接输出，不加标题）："""


def _build_summary_prompt(messages, existing_summary: str) -> tuple[str, int]:
    remaining = HISTORY_CHAR_LIMIT - len(_COMPRESS_PROMPT.format(existing_summary=existing_summary, history=""))
    lines = []
    for message in messages:
        content, _ = truncate_context_text(message.content, MESSAGE_CHAR_LIMIT)
        line = f"{'用户' if message.role == 'user' else '助手'}: {content}"
        size = len(line) + bool(lines)  # Separator before each subsequent line.
        if size > remaining:
            break
        lines.append(line)
        remaining -= size
    return _COMPRESS_PROMPT.format(existing_summary=existing_summary, history="\n".join(lines)), len(lines)


async def maybe_update_summary(session_id: int) -> None:
    try:
        async with AsyncSessionLocal() as db:
            session = await db.get(Session, session_id)
            if session is None:
                return
            watermark = int(session.summary_message_id or 0)
            uncompressed = (
                Message.session_id == session_id,
                Message.id > watermark,
                Message.is_complete.is_(True),
            )
            last_eligible_id = (
                select(Message.id).where(*uncompressed).order_by(Message.id.desc())
                .offset(HISTORY_MESSAGE_LIMIT).limit(1).correlate(None).scalar_subquery()
            )
            result = await db.execute(
                select(Message.id, Message.role, func.substr(Message.content, 1, MESSAGE_CHAR_LIMIT + 1).label("content"))
                .where(*uncompressed, Message.id <= last_eligible_id)
                .order_by(Message.id.asc()).limit(HISTORY_MESSAGE_LIMIT)
            )
            messages = result.all()
            if not messages:
                return
            existing_summary, _ = truncate_context_text(session.summary or "（无）", SUMMARY_CHAR_LIMIT)
            tenant_id = int(session.tenant_id)

        prompt, message_count = _build_summary_prompt(messages, existing_summary)
        if not message_count:
            return
        target_watermark = messages[message_count - 1].id
        response = await get_llm(tenant_id, "general-chat").ainvoke(
            [HumanMessage(content=prompt)]
        )
        summary = str(response.content or "").strip()[:SUMMARY_CHAR_LIMIT]
        if not summary:
            logger.warning("[SUMMARY] session_id=%d LLM 返回空摘要，跳过", session_id)
            return

        async with AsyncSessionLocal() as db:
            async with db.begin():
                result = await db.execute(
                    select(Session).where(Session.id == session_id).with_for_update()
                )
                session = result.scalar_one_or_none()
                if session is None or int(session.summary_message_id or 0) != watermark:
                    return
                session.summary = summary
                session.summary_message_id = target_watermark
        logger.info(
            "[SUMMARY] session_id=%d 摘要水位更新至 message_id=%d，新增压缩 %d 条消息",
            session_id,
            target_watermark,
            message_count,
        )
    except Exception as exc:
        logger.error(
            "[SUMMARY] session_id=%d 摘要更新异常: error_type=%s",
            session_id,
            type(exc).__name__,
        )
