"""
对话记忆服务：管理对话历史
"""
from typing import Optional, List, Dict
from datetime import datetime, timedelta
from sqlalchemy.orm import Session
from langchain_core.messages import HumanMessage, AIMessage, SystemMessage

from models.conversation import Conversation
from models.message import Message
from database import SessionLocal


class ConversationMemoryService:
    """对话记忆服务"""

    def __init__(self):
        pass

    async def get_memory(self, conversation_id: int) -> Optional[List]:
        """
        获取对话记忆

        Args:
            conversation_id: 对话 ID

        Returns:
            LangChain Messages 列表
        """
        db = SessionLocal()
        try:
            # 查询对话消息
            messages = db.query(Message).filter(
                Message.conversation_id == conversation_id
            ).order_by(Message.created_at).all()

            if not messages:
                return None

            # 构建 LangChain Messages
            memory_messages = []

            for msg in messages:
                if msg.role == 'user':
                    memory_messages.append(HumanMessage(content=msg.content))
                elif msg.role == 'assistant':
                    memory_messages.append(AIMessage(content=msg.content))
                elif msg.role == 'system':
                    memory_messages.append(SystemMessage(content=msg.content))

            return memory_messages

        finally:
            db.close()

    async def save_message(
        self,
        conversation_id: Optional[int],
        tenant_id: int,
        user_id: int,
        user_message: str,
        assistant_message: str,
        metadata: Optional[Dict] = None,
        context_type: str = 'workflow'
    ) -> int:
        """
        保存对话消息

        Args:
            conversation_id: 对话 ID（None 表示创建新对话）
            tenant_id: 租户 ID
            user_id: 用户 ID
            user_message: 用户消息
            assistant_message: 助手回复
            metadata: 附加元数据（生成的 SQL、工作流等）
            context_type: 上下文类型 ('sql' 或 'workflow')

        Returns:
            对话 ID
        """
        db = SessionLocal()
        try:
            # 创建或获取对话
            if conversation_id is None:
                conversation = Conversation(
                    tenant_id=tenant_id,
                    user_id=user_id,
                    context_type=context_type,
                    status='active'
                )
                db.add(conversation)
                db.flush()  # 获取 ID
                conversation_id = conversation.id
            else:
                conversation = db.query(Conversation).filter(
                    Conversation.id == conversation_id
                ).first()
                if not conversation:
                    raise ValueError(f"Conversation {conversation_id} not found")
                conversation.updated_at = datetime.utcnow()

            # 保存用户消息
            user_msg = Message(
                conversation_id=conversation_id,
                role='user',
                content=user_message
            )
            db.add(user_msg)

            # 保存助手消息
            assistant_msg = Message(
                conversation_id=conversation_id,
                role='assistant',
                content=assistant_message,
                extra_data=metadata
            )
            db.add(assistant_msg)

            db.commit()
            return conversation_id

        except Exception as e:
            db.rollback()
            raise e
        finally:
            db.close()

    async def get_conversation_history(
        self,
        conversation_id: int
    ) -> List[Dict]:
        """
        获取对话历史

        Args:
            conversation_id: 对话 ID

        Returns:
            对话消息列表
        """
        db = SessionLocal()
        try:
            messages = db.query(Message).filter(
                Message.conversation_id == conversation_id
            ).order_by(Message.created_at).all()

            return [
                {
                    "id": msg.id,
                    "role": msg.role,
                    "content": msg.content,
                    "metadata": msg.extra_data,
                    "created_at": msg.created_at.isoformat()
                }
                for msg in messages
            ]

        finally:
            db.close()

    async def clear_old_conversations(self, days: int = 30):
        """
        清理旧对话（超过指定天数）

        Args:
            days: 保留天数
        """
        db = SessionLocal()
        try:
            cutoff_date = datetime.utcnow() - timedelta(days=days)
            db.query(Conversation).filter(
                Conversation.updated_at < cutoff_date
            ).delete()
            db.commit()
        except Exception as e:
            db.rollback()
            raise e
        finally:
            db.close()


# 全局单例
memory_service = ConversationMemoryService()
