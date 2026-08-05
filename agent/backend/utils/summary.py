"""Incremental session-history summarization with a persisted message watermark."""

import logging

from langchain_core.messages import HumanMessage
from sqlalchemy import select

from agents.context import HISTORY_MESSAGE_LIMIT, SUMMARY_CHAR_LIMIT
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


def _select_incremental_messages(uncompressed: list[Message]) -> tuple[list[Message], int | None]:
    if len(uncompressed) <= HISTORY_MESSAGE_LIMIT:
        return [], None
    messages = uncompressed[:-HISTORY_MESSAGE_LIMIT]
    return messages, int(messages[-1].id)


async def maybe_update_summary(session_id: int) -> None:
    try:
        async with AsyncSessionLocal() as db:
            session = await db.get(Session, session_id)
            if session is None:
                return
            watermark = int(session.summary_message_id or 0)
            result = await db.execute(
                select(Message)
                .where(Message.session_id == session_id, Message.id > watermark)
                .order_by(Message.id.asc())
            )
            uncompressed = list(result.scalars().all())
            new_older_messages, target_watermark = _select_incremental_messages(uncompressed)
            if target_watermark is None:
                return
            existing_summary = session.summary or "（无）"
            tenant_id = int(session.tenant_id)

        history = "\n".join(
            f"{'用户' if message.role == 'user' else '助手'}: {message.content}"
            for message in new_older_messages
        )
        response = await get_llm(tenant_id, "general-chat").ainvoke(
            [HumanMessage(content=_COMPRESS_PROMPT.format(existing_summary=existing_summary, history=history))]
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
                if session is None or int(session.summary_message_id or 0) >= target_watermark:
                    return
                session.summary = summary
                session.summary_message_id = target_watermark
        logger.info(
            "[SUMMARY] session_id=%d 摘要水位更新至 message_id=%d，新增压缩 %d 条消息",
            session_id,
            target_watermark,
            len(new_older_messages),
        )
    except Exception as exc:
        logger.error("[SUMMARY] session_id=%d 摘要更新异常: %s", session_id, exc)
