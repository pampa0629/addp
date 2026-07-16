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
    protocol_message_id = Column(String(100), nullable=True)
    parts = Column(JSONB, nullable=False, default=list)
    created_at = Column(DateTime, server_default=func.now())

    session = relationship("Session", back_populates="messages")
