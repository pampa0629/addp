from sqlalchemy import Column, Integer, String, Boolean, DateTime, func
from . import Base


class SkillUsage(Base):
    __tablename__ = "skill_usage"
    __table_args__ = {"schema": "agent"}

    id = Column(Integer, primary_key=True, index=True)
    skill_name = Column(String(255), nullable=False)
    user_id = Column(Integer, nullable=False)
    tenant_id = Column(Integer, nullable=False)
    success = Column(Boolean, default=True)
    execution_time_ms = Column(Integer)
    created_at = Column(DateTime, server_default=func.now())
