"""Workflow Copilot API: confirm inputs, then generate and validate a candidate DAG."""

from __future__ import annotations

from typing import Any

from fastapi import APIRouter, Depends, HTTPException
from fastapi.security import HTTPAuthorizationCredentials
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy.orm import Session

from addp_common.auth import AuthorizationContext
from addp_common.resources import ResourceFact
from addp_common.tools import ToolExecutionError
from authorization_permissions_generated import COPILOT_WORKFLOW_EXECUTE
from chains.resource_intent_chain import ResourceIntentChain
from chains.resource_recommendation_chain import ResourceRecommendationChain
from config import settings
from database import get_db
from dependencies.auth import bearer_auth, require_tool_user
from services.workflow_service import WorkflowService
from services.inference_service import CopilotInferenceService
from services.resource_discovery import ResourceDiscovery
from services.resource_resolution import ResourceResolutionPolicy, ResourceResolutionService


router = APIRouter()
require_workflow_draft_tool = require_tool_user(
    "copilot",
    "workflow.draft.generate",
    COPILOT_WORKFLOW_EXECUTE,
)


def get_workflow_service(db: Session, tenant_id: int) -> WorkflowService:
    llm = CopilotInferenceService.chat_model(
        db,
        tenant_id=tenant_id,
        scenario_code="workflow_generation",
        temperature=0.2,
        max_output_tokens=4000,
    )
    return WorkflowService(llm=llm)


class WorkflowGenerationRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    query: str = Field(min_length=1, max_length=4000)
    workflow_engine_id: int = Field(ge=1)
    resources: list[ResourceFact] = Field(
        default_factory=list,
        max_length=8,
        description="由 owner Tool 验证的输入资源事实；为空时先返回候选供确认",
    )


class WorkflowResourceCandidate(ResourceFact):
    name: str
    engine_id: int
    engine_name: str | None = None
    asset_type: str
    full_name: str | None = None
    path: str | None = None
    score: float | None = None
    ancestors: list[dict[str, Any]] = Field(default_factory=list)
    recommended: bool = False
    recommendation_reason: str | None = None


class WorkflowGenerationResponse(BaseModel):
    status: str
    workflow: dict[str, Any] | None = None
    explanation: str | None = None
    clarification_reason: str | None = None
    message: str | None = None
    data_source_candidates: list[WorkflowResourceCandidate] | None = None
    errors: list[Any] | None = None
    warnings: list[Any] | None = None
    suggestions: list[Any] | None = None
    resources: list[ResourceFact] | None = None
    selected_operators: list[Any] | None = None
    validation_result: dict[str, Any] | None = None


@router.post(
    "/workflow/generate",
    response_model=WorkflowGenerationResponse,
    summary="生成工作流 | Generate Workflow",
    responses={
        400: {"description": "请求不符合工作流生成约束 | Invalid workflow generation request"},
        502: {"description": "Owner Tool 调用失败 | Owner Tool call failed"},
        500: {"description": "工作流生成失败 | Workflow generation failed"},
    },
    openapi_extra={
        "x-addp-auth-mode": "delegated_tool",
        "x-addp-required-permissions": [COPILOT_WORKFLOW_EXECUTE],
    },
)
async def generate_workflow(
    request: WorkflowGenerationRequest,
    user: AuthorizationContext = Depends(require_workflow_draft_tool),
    credentials: HTTPAuthorizationCredentials = Depends(bearer_auth),
    db: Session = Depends(get_db),
) -> WorkflowGenerationResponse:
    """Discover first-party candidates or consume already confirmed ResourceFact values."""
    try:
        if not request.resources and user.token_type in {"first_party_access_token", "oauth_access_token"}:
            resolution_llm = CopilotInferenceService.chat_model(
                db,
                tenant_id=user.tenant_id,
                scenario_code="resource_resolution",
                temperature=0,
                max_output_tokens=1200,
            )
            resolver = ResourceResolutionService(
                discovery=ResourceDiscovery(
                    settings.get_gateway_url(),
                    credentials.credentials,
                    recommender=ResourceRecommendationChain(resolution_llm),
                ),
                intent_chain=ResourceIntentChain(resolution_llm),
            )
            resolution = await resolver.discover(request.query, ResourceResolutionPolicy.workflow())
            if not resolution.intents:
                return WorkflowGenerationResponse(
                    status="need_clarification",
                    clarification_reason="data_source_not_found",
                    message="未能从需求中识别工作流输入资源",
                    data_source_candidates=[],
                )
            if resolution.missing_roles:
                return WorkflowGenerationResponse(
                    status="need_clarification",
                    clarification_reason="data_source_not_found",
                    message="未找到可确认的工作流输入资源：" + "、".join(resolution.missing_roles),
                    data_source_candidates=[],
                )
            roles = [candidate["role"] for candidate in resolution.candidates]
            ambiguous = any(roles.count(role) > 1 for role in set(roles))
            return WorkflowGenerationResponse(
                status="need_clarification",
                clarification_reason="data_source_ambiguous" if ambiguous else "data_source_confirmation_required",
                message="请确认工作流输入资源后再生成",
                data_source_candidates=[
                    WorkflowResourceCandidate.model_validate(candidate)
                    for candidate in resolution.candidates
                ],
            )

        result = await get_workflow_service(db, user.tenant_id).run(
            query=request.query,
            tenant_id=user.tenant_id,
            workflow_engine_id=request.workflow_engine_id,
            resources=request.resources,
        )
        status = result.get("status")
        if status == "success":
            return WorkflowGenerationResponse(
                status="success",
                workflow=result["workflow"],
                explanation=result.get("explanation", "工作流生成成功"),
                resources=result.get("resources"),
                selected_operators=result.get("selected_operators"),
                validation_result=result.get("validation_result"),
            )
        if status == "need_clarification":
            return WorkflowGenerationResponse(
                status="need_clarification",
                clarification_reason=result.get("clarification_reason"),
                message=result.get("message", "请补充已验证的资源事实"),
            )
        if status == "validation_failed":
            return WorkflowGenerationResponse(
                status="validation_failed",
                workflow=result.get("workflow"),
                errors=result.get("errors", []),
                warnings=result.get("warnings", []),
                suggestions=result.get("suggestions", []),
                message=result.get("message", "工作流生成但未通过验证"),
                resources=result.get("resources"),
                selected_operators=result.get("selected_operators"),
                validation_result=result.get("validation_result"),
            )
        raise RuntimeError("workflow_generation_failed")
    except HTTPException:
        raise
    except ToolExecutionError as error:
        raise HTTPException(status_code=502, detail=error.message) from error
    except ValueError as error:
        raise HTTPException(status_code=400, detail=str(error)) from error
    except Exception as error:
        raise HTTPException(status_code=500, detail="工作流生成失败") from error
