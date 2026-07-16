import uuid

from sqlalchemy import Column, DateTime, ForeignKey, Integer, String, Text, UniqueConstraint, func
from sqlalchemy.dialects.postgresql import JSONB, UUID

from . import Base


class AgentRunStep(Base):
    __tablename__ = "run_steps"
    __table_args__ = (
        UniqueConstraint("agent_run_id", "sequence", name="uq_agent_run_steps_sequence"),
        {"schema": "agent"},
    )

    id = Column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    agent_run_id = Column(
        UUID(as_uuid=True),
        ForeignKey("agent.runs.id", ondelete="CASCADE"),
        nullable=False,
        index=True,
    )
    sequence = Column(Integer, nullable=False)
    step_type = Column(String(30), nullable=False)
    status = Column(String(30), nullable=False, default="running", index=True)
    protocol_invocation_id = Column(String(100), nullable=False)
    tool_call_id = Column(String(100), nullable=True, index=True)
    tool_name = Column(String(255), nullable=True)
    input_data = Column("input", JSONB, nullable=False, default=dict)
    output_summary = Column(Text, nullable=True)
    facts = Column(JSONB, nullable=False, default=dict)
    error_source = Column(String(30), nullable=True)
    error_code = Column(String(100), nullable=True)
    error_message = Column(Text, nullable=True)
    started_at = Column(DateTime(timezone=True), server_default=func.now(), nullable=False)
    completed_at = Column(DateTime(timezone=True), nullable=True)
