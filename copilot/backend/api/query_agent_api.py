"""查询工作台 Copilot：在当前 Query Engine 范围内生成候选查询语言。"""

from __future__ import annotations

import json
from typing import Any, Optional
from uuid import uuid4

from fastapi import APIRouter, Depends, HTTPException
from fastapi.security import HTTPAuthorizationCredentials
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy.orm import Session

from addp_common.auth import AuthorizationContext
from addp_common.tools import ToolExecutionError, ToolExecutor
from authorization_permissions_generated import COPILOT_SQL_EXECUTE
from chains.resource_intent_chain import ResourceIntentChain
from chains.resource_recommendation_chain import ResourceRecommendationChain
from config import settings
from database import get_db
from dependencies.auth import bearer_auth, require_tool_user
from addp_common.resources import ResourceFact
from services.inference_service import CopilotInferenceService
from services.query_service import query_service
from services.resource_discovery import ResourceDiscovery
from services.resource_resolution import ResourceResolutionPolicy, ResourceResolutionService

router = APIRouter()
require_query_draft_tool = require_tool_user(
    "copilot",
    "query.draft.generate",
    COPILOT_SQL_EXECUTE,
)


class QueryGenerationRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    query: str = Field(min_length=1)
    engine_id: int = Field(ge=1)
    query_language: str = Field(min_length=1)
    resources: list[ResourceFact] = Field(default_factory=list, max_length=20)
    current_query: Optional[str] = Field(
        default=None,
        max_length=200_000,
        description="编辑器中已有的查询文本，只作为生成上下文，不作为资源事实或执行范围",
    )
    engine_context: Optional[dict[str, Any]] = Field(
        default=None,
        description="由 Agent 的 engine.list Tool 提供的已验证引擎事实；Develop 用户入口由 Copilot 自行发现",
    )


class QueryResourceCandidate(ResourceFact):
    name: str
    engine_name: Optional[str] = None
    asset_type: str
    full_name: Optional[str] = None
    path: Optional[str] = None
    score: Optional[float] = None
    ancestors: list[dict[str, Any]] = Field(default_factory=list)
    recommended: bool = False
    recommendation_reason: Optional[str] = None


class QueryGenerationResponse(BaseModel):
    status: str
    query: Optional[str] = None
    query_language: str
    explanation: Optional[str] = None
    warnings: list[str] = Field(default_factory=list)
    resources: Optional[list[ResourceFact]] = None
    data_source_candidates: Optional[list[QueryResourceCandidate]] = None
    clarification_reason: Optional[str] = None
    message: Optional[str] = None


@router.post(
    "/query/generate",
    response_model=QueryGenerationResponse,
    summary="生成查询语言 | Generate Query Language",
    responses={
        400: {"description": "请求不符合查询生成约束 | Invalid query generation request"},
        502: {"description": "Owner Tool 调用失败 | Owner Tool call failed"},
        500: {"description": "查询生成失败 | Query generation failed"},
    },
    openapi_extra={
        "x-addp-auth-mode": "delegated_tool",
        "x-addp-required-permissions": [COPILOT_SQL_EXECUTE],
    },
)
async def generate_query(
    request: QueryGenerationRequest,
    user: AuthorizationContext = Depends(require_query_draft_tool),
    credentials: HTTPAuthorizationCredentials = Depends(bearer_auth),
    db: Session = Depends(get_db),
):
    """在当前查询工作台引擎范围内发现资源并生成候选查询文本。"""
    executor = ToolExecutor(settings.get_gateway_url(), credentials.credentials)
    try:
        engine = await _resolve_engine(request, user, executor)
        language = request.query_language.strip().lower()
        languages = _query_languages(engine)
        if language not in languages:
            return QueryGenerationResponse(
                status="need_clarification",
                query_language=language,
                clarification_reason="query_language_unsupported",
                message=f"当前引擎不支持查询语言 {language}",
            )

        resources: list[ResourceFact] = []
        use_current_mql = bool(
            language == "mql" and _mql_primary_collection(request.current_query)
        )
        if request.resources or not use_current_mql:
            resolution_llm = CopilotInferenceService.chat_model(
                db,
                tenant_id=user.tenant_id,
                scenario_code="resource_resolution",
                temperature=0,
                max_output_tokens=1200,
            )
            discovery = ResourceDiscovery(
                settings.get_gateway_url(),
                credentials.credentials,
                executor=executor,
                recommender=ResourceRecommendationChain(resolution_llm),
            )
            resolver = ResourceResolutionService(
                discovery=discovery,
                intent_chain=ResourceIntentChain(resolution_llm),
            )
            policy = ResourceResolutionPolicy.query(request.engine_id)
            if not request.resources:
                return await _discover_query_resources(request, resolver, policy, language)
            resources = await resolver.verify(request.resources, policy)
        generated = await query_service.generate(
            query=request.query,
            engine=engine,
            query_language=language,
            resources=[item.model_dump(exclude_none=True) for item in resources],
            current_query=request.current_query,
            tenant_id=user.tenant_id,
            db=db,
        )
        return QueryGenerationResponse(
            status="success",
            query=generated["query"],
            query_language=language,
            explanation=generated.get("explanation"),
            warnings=generated.get("warnings", []),
            resources=resources,
        )
    except ToolExecutionError as error:
        status_code = 400 if error.code == "invalid_arguments" else 502
        raise HTTPException(status_code=status_code, detail=error.message) from error
    except ValueError as error:
        raise HTTPException(status_code=400, detail=str(error)) from error
    except Exception as error:
        raise HTTPException(status_code=500, detail="查询生成失败") from error


def _mql_primary_collection(current_query: str | None) -> str | None:
    """返回单一 MQL command object 的主 collection，其他文本一律视为未声明。"""
    if not isinstance(current_query, str) or not current_query.strip():
        return None
    try:
        command = json.loads(current_query)
    except (TypeError, json.JSONDecodeError):
        return None
    if not isinstance(command, dict):
        return None
    collections = [
        str(command[key]).strip()
        for key in ("find", "aggregate", "count", "distinct")
        if isinstance(command.get(key), str) and str(command[key]).strip()
    ]
    return collections[0] if len(collections) == 1 else None


async def _discover_query_resources(
    request: QueryGenerationRequest,
    resolver: ResourceResolutionService,
    policy: ResourceResolutionPolicy,
    language: str,
) -> QueryGenerationResponse:
    result = await resolver.discover(request.query, policy)
    if not result.intents:
        return QueryGenerationResponse(
            status="need_clarification",
            query_language=language,
            clarification_reason="data_source_not_found",
            message="未能从需求中识别查询输入数据源",
            data_source_candidates=[],
        )
    if result.missing_roles:
        return QueryGenerationResponse(
            status="need_clarification",
            query_language=language,
            clarification_reason="data_source_not_found",
            message="未找到当前查询引擎内的资源：" + "、".join(result.missing_roles),
            data_source_candidates=[],
        )
    return QueryGenerationResponse(
        status="need_clarification",
        query_language=language,
        clarification_reason="data_source_confirmation_required",
        message="请确认当前查询引擎内的查询输入资源后再生成",
        data_source_candidates=[QueryResourceCandidate.model_validate(item) for item in result.candidates],
    )


async def _resolve_engine(
    request: QueryGenerationRequest,
    user: AuthorizationContext,
    executor: ToolExecutor,
) -> dict[str, Any]:
    if request.engine_context is not None:
        engine = dict(request.engine_context)
        if engine.get("id") != request.engine_id:
            raise ToolExecutionError("invalid_arguments", "引擎事实与请求 engine_id 不一致")
        return engine
    if user.token_type not in {"first_party_access_token", "oauth_access_token"}:
        raise ToolExecutionError("invalid_arguments", "委托调用必须提供已验证的 engine_context")
    result = await executor.call(
        "engine.list",
        {},
        agent_run_id=f"copilot-query-{uuid4()}",
        tool_call_id=f"engine-list-{uuid4()}",
    )
    for engine in result if isinstance(result, list) else []:
        if isinstance(engine, dict) and engine.get("id") == request.engine_id:
            return engine
    raise ToolExecutionError("owner_api_error", "当前用户无法访问所选查询引擎")


def _query_languages(engine: dict[str, Any]) -> list[str]:
    capabilities = engine.get("capabilities")
    if isinstance(capabilities, str):
        try:
            capabilities = json.loads(capabilities)
        except json.JSONDecodeError:
            capabilities = {}
    query_capability = (capabilities or {}).get("compute", {}).get("query", {})
    if not query_capability.get("supported"):
        return []
    return [str(item).strip().lower() for item in query_capability.get("languages", []) if str(item).strip()]
