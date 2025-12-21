"""
对话消息模型
"""
from sqlalchemy import Column, Integer, String, Text, DateTime, ForeignKey
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import relationship
from sqlalchemy.sql import func
from database import Base


class Message(Base):
    """对话消息表"""
    __tablename__ = 'messages'
    __table_args__ = {'schema': 'copilot'}

    id = Column(Integer, primary_key=True, autoincrement=True)
    conversation_id = Column(Integer, ForeignKey('copilot.conversations.id', ondelete='CASCADE'), nullable=False, index=True)
    role = Column(String(20), nullable=False)  # 'user', 'assistant', 'system'
    content = Column(Text, nullable=False)
    extra_data = Column(JSONB)  # 附加信息（生成的 SQL、选中的数据源等）
    token_count = Column(Integer)
    created_at = Column(DateTime(timezone=True), server_default=func.now())

    # 关系：多对一 Conversation
    conversation = relationship("Conversation", back_populates="messages")

    def __repr__(self):
        return f"<Message(id={self.id}, conversation_id={self.conversation_id}, role={self.role})>"
