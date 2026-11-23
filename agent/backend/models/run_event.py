import uuid

from sqlalchemy import Column, ForeignKey, Integer, String, UniqueConstraint, func
from sqlalchemy import Uuid
from addp_common.infra.sqltypes import Instant, PlatformJSON

from . import Base


class AgentRunEvent(Base):
    __tablename__ = "run_events"
    __table_args__ = (
        UniqueConstraint("agent_run_id", "sequence", name="uq_agent_run_events_sequence"),
        {"schema": "agent"},
    )

    id = Column(Uuid(as_uuid=True), primary_key=True, default=uuid.uuid4)
    agent_run_id = Column(
        Uuid(as_uuid=True),
        ForeignKey("agent.runs.id", ondelete="CASCADE"),
        nullable=False,
        index=True,
    )
    sequence = Column(Integer, nullable=False)
    protocol_invocation_id = Column(String(100), nullable=False)
    event_type = Column(String(80), nullable=False, index=True)
    payload = Column(PlatformJSON, nullable=False, default=dict)
    created_at = Column(Instant(), server_default=func.now(), nullable=False)
