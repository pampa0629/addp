"""Notebook Copilot：理解 Session 资源意图、排序候选并生成 Python 单元。"""

from __future__ import annotations

from typing import Any, Literal

from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy.orm import Session

from addp_common.auth import AuthorizationContext
from authorization_permissions_generated import COPILOT_NOTEBOOK_EXECUTE
from chains.notebook_resource_recommendation_chain import NotebookResourceRecommendationChain
from chains.resource_intent_chain import ResourceIntent, ResourceIntentChain
from database import get_db
from dependencies.auth import require_tool_user
from services.inference_service import CopilotInferenceService
from services.notebook_service import notebook_service
from services.resource_resolution import ResourceResolutionPolicy, ResourceResolutionService


router = APIRouter()
require_notebook_draft_tool = require_tool_user(
    "copilot", "notebook.draft.generate", COPILOT_NOTEBOOK_EXECUTE,
)


class NotebookCatalogCandidate(BaseModel):
    model_config = ConfigDict(extra="forbid")
    candidate_id: str = Field(min_length=1)
    role: str = Field(min_length=1)
    engine_id: int = Field(ge=1)
    engine_name: str = Field(min_length=1)
    engine_type: str = Field(min_length=1)
    name: str = Field(min_length=1)
    term: str = Field(min_length=1)
    kind: str = Field(min_length=1)
    path: dict[str, Any]
    path_names: list[str] = Field(default_factory=list, max_length=16)


class NotebookResourceFact(NotebookCatalogCandidate):
    path_segments: list[dict[str, str]] = Field(min_length=1, max_length=16)
    fields: list[dict[str, Any]] = Field(default_factory=list, max_length=200)
    geometry_column: str | None = None
    geometry_type: str | None = None
    crs: str | None = None


class NotebookMissingIntent(BaseModel):
    model_config = ConfigDict(extra="forbid")
    role: str = Field(min_length=1)
    search_queries: list[str] = Field(min_length=1, max_length=8)


class NotebookGenerationRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    query: str = Field(min_length=1)
    kernel: Literal["python3"] = "python3"
    candidates: list[NotebookCatalogCandidate] = Field(default_factory=list, max_length=320)
    resources: list[NotebookResourceFact] = Field(default_factory=list, max_length=8)
    missing_intents: list[NotebookMissingIntent] = Field(
        default_factory=list,
        max_length=8,
        description="首轮零召回时请求补充尚未尝试的资源检索词",
    )


class NotebookGenerationResponse(BaseModel):
    status: Literal["intents_ready", "candidates_ready", "success", "need_clarification"]
    intents: list[dict[str, Any]] = Field(default_factory=list)
    candidates: list[dict[str, Any]] = Field(default_factory=list)
    code: str | None = None
    explanation: str | None = None
    warnings: list[str] = Field(default_factory=list)
    clarification_reason: str | None = None
    message: str | None = None


@router.post(
    "/notebook/generate",
    response_model=NotebookGenerationResponse,
    summary="生成 Notebook Python 单元 | Generate Notebook Python cell",
    openapi_extra={
        "x-addp-auth-mode": "delegated_tool",
        "x-addp-required-permissions": [COPILOT_NOTEBOOK_EXECUTE],
    },
)
async def generate_notebook(
    request: NotebookGenerationRequest,
    user: AuthorizationContext = Depends(require_notebook_draft_tool),
    db: Session = Depends(get_db),
):
    """仅处理 Develop 已限定的 Session 候选和资源事实，不自行搜索全租户数据。"""
    try:
        llm = CopilotInferenceService.chat_model(
            db, tenant_id=user.tenant_id, scenario_code="resource_resolution", temperature=0, max_output_tokens=2400,
        )
        resolver = ResourceResolutionService(
            intent_chain=ResourceIntentChain(llm),
            notebook_recommender=NotebookResourceRecommendationChain(llm),
        )
        policy = ResourceResolutionPolicy.notebook()
        if request.missing_intents:
            expanded = await resolver.expand_missing(
                request.query,
                [
                    ResourceIntent(role=item.role, search_queries=item.search_queries)
                    for item in request.missing_intents
                ],
            )
            return NotebookGenerationResponse(
                status="intents_ready",
                intents=[item.model_dump() for item in expanded],
            )
        if request.resources:
            generated = await notebook_service.generate(
                query=request.query,
                kernel=request.kernel,
                resources=[item.model_dump(exclude_none=True) for item in request.resources],
                tenant_id=user.tenant_id,
                db=db,
            )
            return NotebookGenerationResponse(status="success", **generated)
        if request.candidates:
            recommendations = await resolver.rank_session_candidates(
                request.query,
                [item.model_dump() for item in request.candidates],
                policy,
            )
            ordered = []
            by_id = {item.candidate_id: item for item in request.candidates}
            for role in dict.fromkeys(item.role for item in request.candidates):
                recommendation = recommendations.get(role)
                ids = recommendation.ranked_candidate_ids if recommendation else [
                    item.candidate_id for item in request.candidates if item.role == role
                ]
                if recommendation:
                    ids = list(ids)
                    ids.extend(
                        item.candidate_id
                        for item in request.candidates
                        if item.role == role and item.candidate_id not in ids
                    )
                for candidate_id in ids:
                    candidate = by_id.get(candidate_id)
                    if candidate is None or candidate.role != role:
                        continue
                    value = candidate.model_dump()
                    value["recommended"] = bool(
                        recommendation and recommendation.recommended_candidate_id == candidate_id
                    )
                    value["recommendation_reason"] = (
                        recommendation.recommendation_reason if value["recommended"] else None
                    )
                    ordered.append(value)
            return NotebookGenerationResponse(status="candidates_ready", candidates=ordered)

        intents = await resolver.extract(request.query, policy)
        if not intents:
            return NotebookGenerationResponse(
                status="need_clarification",
                clarification_reason="data_source_not_identified",
                message="未能从需求中识别 Notebook 输入数据源",
            )
        return NotebookGenerationResponse(
            status="intents_ready",
            intents=[item.model_dump() for item in intents],
        )
    except ValueError as error:
        raise HTTPException(status_code=400, detail=str(error)) from error
    except Exception as error:
        raise HTTPException(status_code=500, detail="Notebook 代码生成失败") from error
