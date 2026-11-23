from sqlalchemy.orm import DeclarativeBase


class Base(DeclarativeBase):
    # Async callers consume server timestamps immediately after flush. MySQL
    # needs an explicit fetch because it has no INSERT ... RETURNING.
    __mapper_args__ = {"eager_defaults": True}

from .session import Session
from .message import Message
from .skill_usage import SkillUsage
from .run import AgentRun
from .run_step import AgentRunStep
from .run_event import AgentRunEvent
from .interaction import Interaction
from .inference_scenario_binding import InferenceScenarioBinding

__all__ = [
    "Base",
    "Session",
    "Message",
    "SkillUsage",
    "AgentRun",
    "AgentRunStep",
    "AgentRunEvent",
    "Interaction",
    "InferenceScenarioBinding",
]
