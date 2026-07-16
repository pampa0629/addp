import uuid

from sqlalchemy import Column, DateTime, ForeignKey, Integer, String, Text, func
from sqlalchemy.dialects.postgresql import JSONB, UUID

from . import Base


class Interaction(Base):
    __tablename__ = "interactions"
    __table_args__ = {"schema": "agent"}

    id = Column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    session_id = Column(
        Integer,
        ForeignKey("agent.sessions.id", ondelete="CASCADE"),
        nullable=False,
        index=True,
    )
    user_id = Column(Integer, nullable=False, index=True)
    tenant_id = Column(Integer, nullable=False, index=True)
    agent_run_id = Column(
        UUID(as_uuid=True),
        ForeignKey("agent.runs.id", ondelete="CASCADE"),
        nullable=False,
        index=True,
    )
    tool_call_id = Column(String(100), nullable=True)
    kind = Column(String(50), nullable=False, default="clarification")
    owner = Column(String(50), nullable=False, default="agent")
    status = Column(String(30), nullable=False, default="pending", index=True)
    prompt = Column(Text, nullable=False)
    response_schema = Column(JSONB, nullable=False, default=dict)
    options = Column(JSONB, nullable=False, default=list)
    answer = Column(JSONB, nullable=True)
    created_at = Column(DateTime(timezone=True), server_default=func.now(), nullable=False)
    completed_at = Column(DateTime(timezone=True), nullable=True)
    expires_at = Column(DateTime(timezone=True), nullable=True)
