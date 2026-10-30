"""Bounded plan presentation; never owner facts, execution state or authorization."""
from typing import Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, StringConstraints, ValidationError
from typing import Annotated
from sqlalchemy import select

from models.run import AgentRun
from models.run_event import AgentRunEvent

PLAN_ACTIVITY = 'addp-plan'
PLAN_TOOL = 'write_todos'


class PlanItem(BaseModel):
    model_config = ConfigDict(extra='forbid', strict=True)
    content: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=300)]
    status: Literal['pending', 'in_progress', 'completed']


class PlanInput(BaseModel):
    model_config = ConfigDict(extra='forbid', strict=True)
    todos: list[PlanItem] = Field(max_length=12)


def plan_todos(value):
    return [item.model_dump() for item in PlanInput.model_validate({'todos': value}).todos]


async def read_run_plan(db, run_id: UUID, user_id: int, tenant_id: int):
    payload = (await db.execute(select(AgentRunEvent.payload).join(AgentRun, AgentRun.id == AgentRunEvent.agent_run_id)
        .where(AgentRun.id == run_id, AgentRun.user_id == user_id, AgentRun.tenant_id == tenant_id,
               AgentRunEvent.event_type == 'ACTIVITY_SNAPSHOT', AgentRunEvent.payload['activityType'].as_string() == PLAN_ACTIVITY)
        .order_by(AgentRunEvent.sequence.desc()).limit(1))).scalar_one_or_none()
    if payload is None:
        return []
    if payload['content']['agent_run_id'] != str(run_id):
        raise ValueError('agent_plan_identity_mismatch')
    try:
        return plan_todos(payload['content']['todos'])
    except (KeyError, TypeError, ValidationError):
        raise ValueError('invalid_agent_plan') from None
