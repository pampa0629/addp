"""Agent-owned Inference Scenario Binding management API."""

from datetime import datetime
from uuid import UUID

from addp_common.auth import AuthorizationContext
from fastapi import APIRouter, Depends, HTTPException, Request, status
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy.ext.asyncio import AsyncSession

from authorization_permissions_generated import AGENT_CONFIGURATION_READ, AGENT_CONFIGURATION_UPDATE
from database import get_db
from middleware.auth import require_context_permissions
from repositories.inference_scenario_binding_repository import (
    InferenceScenarioBindingRepository,
    InferenceScenarioBindingVersionConflict,
)
from utils.llm import SCENARIOS


router = APIRouter()


class InferenceScenarioBindingUpdate(BaseModel):
    model_config = ConfigDict(extra="forbid")

    version: int = Field(ge=0)
    model_profile_id: str


class InferenceScenarioBindingResponse(BaseModel):
    scenario_code: str
    scope_type: str
    tenant_id: int | None = None
    model_profile_id: str | None = None
    version: int = 0
    updated_by: int | None = None
    updated_at: datetime | None = None
    effective: bool = False


def _scope(context: AuthorizationContext) -> tuple[str, int | None]:
    if context.context_type == "platform":
        return "platform", None
    if context.context_type == "tenant" and context.tenant_id is not None:
        return "tenant", context.tenant_id
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="不支持的授权上下文")


def _scenario(value: str) -> str:
    if value not in SCENARIOS:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Agent 推理场景不存在")
    return value


def _profile_id(value: str) -> str:
    try:
        return str(UUID(value.strip()))
    except (AttributeError, ValueError) as exc:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="model_profile_id 必须是 UUID") from exc


def _response(binding, *, effective: bool) -> InferenceScenarioBindingResponse:
    return InferenceScenarioBindingResponse(
        scenario_code=binding.scenario_code,
        scope_type=binding.scope_type,
        tenant_id=binding.tenant_id,
        model_profile_id=binding.model_profile_id,
        version=binding.version,
        updated_by=binding.updated_by,
        updated_at=binding.updated_at,
        effective=effective,
    )


@router.get(
    "/settings/inference-bindings/{scenario_code}",
    response_model=InferenceScenarioBindingResponse,
    summary="获取 Agent 推理场景绑定 | Get Agent Inference Scenario Binding",
    openapi_extra={
        "x-addp-auth-mode": "permission",
        "x-addp-required-permissions": [AGENT_CONFIGURATION_READ],
    },
)
async def get_inference_scenario_binding(
    scenario_code: str,
    request: Request,
    _permission: None = Depends(require_context_permissions(AGENT_CONFIGURATION_READ)),
    db: AsyncSession = Depends(get_db),
):
    scenario_code = _scenario(scenario_code)
    context: AuthorizationContext = request.state.authorization_context
    scope_type, tenant_id = _scope(context)
    repository = InferenceScenarioBindingRepository(db)
    binding = await repository.get(scope_type, tenant_id, scenario_code)
    if binding is None and tenant_id is not None:
        binding = await repository.resolve(tenant_id, scenario_code)
    if binding is None:
        return InferenceScenarioBindingResponse(
            scenario_code=scenario_code,
            scope_type=scope_type,
            tenant_id=tenant_id,
        )
    return _response(binding, effective=True)


@router.put(
    "/settings/inference-bindings/{scenario_code}",
    response_model=InferenceScenarioBindingResponse,
    summary="更新 Agent 推理场景绑定 | Update Agent Inference Scenario Binding",
    responses={409: {"description": "版本冲突 | Version conflict"}},
    openapi_extra={
        "x-addp-auth-mode": "permission",
        "x-addp-required-permissions": [AGENT_CONFIGURATION_UPDATE],
    },
)
async def update_inference_scenario_binding(
    scenario_code: str,
    body: InferenceScenarioBindingUpdate,
    request: Request,
    _permission: None = Depends(require_context_permissions(AGENT_CONFIGURATION_UPDATE)),
    db: AsyncSession = Depends(get_db),
):
    scenario_code = _scenario(scenario_code)
    context: AuthorizationContext = request.state.authorization_context
    scope_type, tenant_id = _scope(context)
    try:
        binding = await InferenceScenarioBindingRepository(db).save(
            scope_type=scope_type,
            tenant_id=tenant_id,
            scenario_code=scenario_code,
            model_profile_id=_profile_id(body.model_profile_id),
            expected_version=body.version,
            updated_by=context.principal_id,
        )
    except InferenceScenarioBindingVersionConflict as exc:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="inference_scenario_binding_version_conflict",
        ) from exc
    return _response(binding, effective=True)
