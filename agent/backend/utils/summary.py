"""
会话历史摘要（P4）

当会话消息数超过上下文窗口时，异步压缩早期历史并保存到 session.summary。
该任务在助手回复完成后以 fire-and-forget 方式触发，不阻塞主流程。
"""
import logging
from sqlalchemy import select
from langchain_core.messages import HumanMessage

from database import AsyncSessionLocal
from models.session import Session
from models.message import Message
from utils.llm import get_llm

logger = logging.getLogger("agent.summary")

# 与 chat.py 保持一致：上下文窗口大小（条消息数）
_HISTORY_LIMIT = 20

_COMPRESS_PROMPT = """\
以下是一段用户与 ADDP 数据平台 AI 助手的对话历史（较早部分）。
请将其压缩为一段简洁的中文摘要（不超过 400 字），重点保留：
- 用户的核心诉求和目标
- 已探索或操作过的数据集、表名
- 重要发现或查询结论
- 尚未解决的问题或待续的任务

对话历史：
{history}

输出摘要（直接输出，不加标题）："""


async def maybe_update_summary(session_id: int) -> None:
    """
    检查并在需要时更新会话历史摘要（后台 fire-and-forget 任务）。

    逻辑：
    - 加载会话所有消息
    - 若总消息数 <= _HISTORY_LIMIT，不需要摘要，直接返回
    - 否则取窗口外的早期消息，调用 LLM 压缩
    - 将摘要写入 agent.sessions.summary
    """
    try:
        async with AsyncSessionLocal() as db:
            async with db.begin():
                # 加载所有消息（时间正序）
                msg_result = await db.execute(
                    select(Message)
                    .where(Message.session_id == session_id)
                    .order_by(Message.created_at.asc())
                )
                all_msgs = msg_result.scalars().all()

                total = len(all_msgs)
                if total <= _HISTORY_LIMIT:
                    logger.debug(
                        "[SUMMARY] session_id=%d 消息数=%d ≤ 窗口(%d)，跳过摘要",
                        session_id, total, _HISTORY_LIMIT,
                    )
                    return

                # 需要压缩的早期消息（窗口外部分）
                older_msgs = all_msgs[:-_HISTORY_LIMIT]
                lines = []
                for m in older_msgs:
                    role = "用户" if m.role == "user" else "助手"
                    lines.append(f"{role}: {m.content}")
                history_text = "\n".join(lines)

                # LLM 压缩摘要
                llm = get_llm(streaming=False)
                prompt = _COMPRESS_PROMPT.format(history=history_text)
                response = await llm.ainvoke([HumanMessage(content=prompt)])
                new_summary = response.content.strip()

                if not new_summary:
                    logger.warning("[SUMMARY] session_id=%d LLM 返回空摘要，跳过", session_id)
                    return

                # 写入 session.summary
                sess_result = await db.execute(
                    select(Session).where(Session.id == session_id)
                )
                session_obj = sess_result.scalar_one_or_none()
                if session_obj:
                    session_obj.summary = new_summary
                    logger.info(
                        "[SUMMARY] session_id=%d 摘要已更新（覆盖 %d 条早期消息）",
                        session_id, len(older_msgs),
                    )

    except Exception as e:
        logger.error("[SUMMARY] session_id=%d 摘要更新异常: %s", session_id, e)
