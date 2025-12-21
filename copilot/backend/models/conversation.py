"""
对话会话模型
"""
from sqlalchemy import Column, Integer, String, DateTime, ForeignKey
from sqlalchemy.orm import relationship
from sqlalchemy.sql import func
from database import Base


class Conversation(Base):
    """对话会话表"""
    __tablename__ = 'conversations'
    __table_args__ = {'schema': 'copilot'}

    id = Column(Integer, primary_key=True, autoincrement=True)
    tenant_id = Column(Integer, nullable=False, index=True)
    user_id = Column(Integer, nullable=False, index=True)
    context_type = Column(String(32), nullable=False)  # 'sql' 或 'workflow'
    status = Column(String(32), default='active')  # 'active', 'archived'
    created_at = Column(DateTime(timezone=True), server_default=func.now())
    updated_at = Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now())

    # 关系：一对多 Message
    messages = relationship("Message", back_populates="conversation", cascade="all, delete-orphan")

    def __repr__(self):
        return f"<Conversation(id={self.id}, tenant_id={self.tenant_id}, context_type={self.context_type})>"
