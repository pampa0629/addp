"""Copilot-owned Inference Scenario Binding management API."""

from datetime import datetime

from addp_common.auth import AuthorizationContext
from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy.orm import Session

from authorization_permissions_generated import COPILOT_CONFIGURATION_READ, COPILOT_CONFIGURATION_UPDATE
from database import get_db
from dependencies.auth import require_permissions
from repositories.inference_scenario_binding_repository import (
    InferenceScenarioBindingRepository,
    InferenceScenarioBindingVersionConflict,
)
from services.inference_service import SCENARIOS, binding_to_dict, validate_profile_id


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
    raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="Unsupported authorization context")


def _scenario(value: str) -> str:
    if value not in SCENARIOS:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Unknown Copilot inference scenario")
    return value


@router.get(
    "/settings/inference-bindings/{scenario_code}",
    response_model=InferenceScenarioBindingResponse,
    summary="获取 Copilot 推理场景绑定 | Get Copilot Inference Scenario Binding",
    openapi_extra={
        "x-addp-auth-mode": "permission",
        "x-addp-required-permissions": [COPILOT_CONFIGURATION_READ],
    },
)
async def get_inference_scenario_binding(
    scenario_code: str,
    context: AuthorizationContext = Depends(require_permissions(COPILOT_CONFIGURATION_READ)),
    db: Session = Depends(get_db),
):
    scenario_code = _scenario(scenario_code)
    scope_type, tenant_id = _scope(context)
    repository = InferenceScenarioBindingRepository(db)
    binding = repository.get(scope_type, tenant_id, scenario_code)
    if binding is None and tenant_id is not None:
        binding = repository.resolve(tenant_id, scenario_code)
    if binding is None:
        return InferenceScenarioBindingResponse(
            scenario_code=scenario_code,
            scope_type=scope_type,
            tenant_id=tenant_id,
        )
    return InferenceScenarioBindingResponse.model_validate(binding_to_dict(binding, effective=True))


@router.put(
    "/settings/inference-bindings/{scenario_code}",
    response_model=InferenceScenarioBindingResponse,
    summary="更新 Copilot 推理场景绑定 | Update Copilot Inference Scenario Binding",
    responses={409: {"description": "版本冲突 | Version conflict"}},
    openapi_extra={
        "x-addp-auth-mode": "permission",
        "x-addp-required-permissions": [COPILOT_CONFIGURATION_UPDATE],
    },
)
async def update_inference_scenario_binding(
    scenario_code: str,
    request: InferenceScenarioBindingUpdate,
    context: AuthorizationContext = Depends(require_permissions(COPILOT_CONFIGURATION_UPDATE)),
    db: Session = Depends(get_db),
):
    scenario_code = _scenario(scenario_code)
    scope_type, tenant_id = _scope(context)
    try:
        profile_id = validate_profile_id(request.model_profile_id)
    except (AttributeError, ValueError) as exc:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="model_profile_id must be a UUID") from exc
    try:
        binding = InferenceScenarioBindingRepository(db).save(
            scope_type=scope_type,
            tenant_id=tenant_id,
            scenario_code=scenario_code,
            model_profile_id=profile_id,
            expected_version=request.version,
            updated_by=context.principal_id,
        )
    except InferenceScenarioBindingVersionConflict as exc:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="inference_scenario_binding_version_conflict") from exc
    return InferenceScenarioBindingResponse.model_validate(binding_to_dict(binding, effective=True))
