from sqlalchemy import Column, Integer, String, Text, DateTime, ForeignKey, func
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import relationship
from . import Base


class Message(Base):
    __tablename__ = "messages"
    __table_args__ = {"schema": "agent"}

    id = Column(Integer, primary_key=True, index=True)
    session_id = Column(Integer, ForeignKey("agent.sessions.id", ondelete="CASCADE"), nullable=False)
    role = Column(String(50), nullable=False)  # 'user' | 'assistant' | 'system'
    content = Column(Text, nullable=False)
    message_type = Column(String(50), default="text")  # 'text' | 'tool_progress'（不存DB）
    result_type = Column(String(50))  # 'text' | 'table' | 'chart' | 'map' | 'error'
    result_data = Column(JSONB)
    created_at = Column(DateTime, server_default=func.now())

    session = relationship("Session", back_populates="messages")
