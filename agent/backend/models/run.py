from datetime import datetime, timezone
import uuid

from sqlalchemy import Column, ForeignKey, Integer, String, Text, UniqueConstraint, func
from sqlalchemy import Uuid
from addp_common.infra.sqltypes import Instant, PlatformJSON

from . import Base


class AgentRun(Base):
    __tablename__ = "runs"
    __table_args__ = (
        UniqueConstraint("session_id", "initial_protocol_run_id", name="uq_agent_runs_session_protocol_run"),
        {"schema": "agent"},
    )

    id = Column(Uuid(as_uuid=True), primary_key=True, default=uuid.uuid4)
    session_id = Column(
        Integer,
        ForeignKey("agent.sessions.id", ondelete="CASCADE"),
        nullable=False,
        index=True,
    )
    user_id = Column(Integer, nullable=False, index=True)
    tenant_id = Column(Integer, nullable=False, index=True)
    initial_protocol_run_id = Column(String(100), nullable=False)
    status = Column(String(30), nullable=False, default="running", index=True)
    skill_name = Column(String(255), nullable=True)
    checkpoint = Column(PlatformJSON, nullable=False, default=dict)
    metrics = Column(PlatformJSON, nullable=False, default=dict)
    context_metrics = Column(PlatformJSON, nullable=False, default=dict)
    error_source = Column(String(30), nullable=True)
    error_code = Column(String(100), nullable=True)
    error_message = Column(Text, nullable=True)
    created_at = Column(Instant(), server_default=func.now(), nullable=False)
    updated_at = Column(
        Instant(),
        server_default=func.now(),
        onupdate=lambda: datetime.now(timezone.utc),
        nullable=False,
    )
    completed_at = Column(Instant(), nullable=True)
