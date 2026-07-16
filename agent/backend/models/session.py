from sqlalchemy import Column, Integer, String, Text, DateTime, func
from sqlalchemy.orm import relationship
from . import Base


class Session(Base):
    __tablename__ = "sessions"
    __table_args__ = {"schema": "agent"}

    id = Column(Integer, primary_key=True, index=True)
    user_id = Column(Integer, nullable=False)
    tenant_id = Column(Integer, nullable=False)
    title = Column(String(255))
    summary = Column(Text, nullable=True)   # LLM 压缩的早期历史摘要（P4）
    summary_message_id = Column(Integer, nullable=True)
    created_at = Column(DateTime, server_default=func.now())
    updated_at = Column(DateTime, server_default=func.now(), onupdate=func.now())

    messages = relationship("Message", back_populates="session", cascade="all, delete-orphan")
