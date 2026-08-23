"""查询工作台 Copilot：在当前 Query Engine 范围内生成候选查询语言。"""

from __future__ import annotations

import json
import logging
from typing import Any, Literal, Optional
from uuid import uuid4

from fastapi import APIRouter, Depends, HTTPException
from fastapi.security import HTTPAuthorizationCredentials
from pydantic import BaseModel, ConfigDict, Field, field_validator
from sqlalchemy.orm import Session

from addp_common.auth import AuthorizationContext
from addp_common.client.inference import InferenceError
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
from services.query_clarification import QueryClarification as DomainQueryClarification
from services.query_clarification import QueryClarificationRequired
from services.resource_discovery import ResourceDiscovery
from services.resource_resolution import ResourceResolutionPolicy, ResourceResolutionService

router = APIRouter()
logger = logging.getLogger(__name__)
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
    resource_scope_locator: Optional[str] = Field(
        default=None,
        min_length=1,
        max_length=4096,
        description="仅用于资源发现的 Owner Catalog 容器 locator，不是输入资源或查询执行范围",
    )
    engine_context: Optional[dict[str, Any]] = Field(
        default=None,
        description="由 Agent 的 engine.list Tool 提供的已验证引擎事实；Develop 用户入口由 Copilot 自行发现",
    )
    clarification_answers: dict[str, str | list[str]] = Field(
        default_factory=dict,
        max_length=20,
        description="用户对本次查询语义缺口的已确认答案；key 必须来自上一次结构化澄清",
    )

    @field_validator("clarification_answers")
    @classmethod
    def validate_clarification_answers(cls, value: dict[str, str | list[str]]):
        for key, answer in value.items():
            if not key.strip() or len(key) > 120:
                raise ValueError("clarification answer key is invalid")
            answers = answer if isinstance(answer, list) else [answer]
            if not answers or any(not item.strip() or len(item) > 2000 for item in answers):
                raise ValueError("clarification answer value is invalid")
        return value


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


class QueryParameterDefinition(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str = Field(pattern=r"^[A-Za-z_][A-Za-z0-9_]*$")
    type: Literal["string", "integer", "number", "boolean"]
    default: Any
    title: Optional[str] = None
    description: Optional[str] = None


class QueryClarificationOption(BaseModel):
    model_config = ConfigDict(extra="forbid")

    value: str
    label: str
    description: Optional[str] = None


class QueryClarification(BaseModel):
    model_config = ConfigDict(extra="forbid")

    key: str
    category: str
    prompt: str
    control: Literal["single_choice", "multiple_choice", "text", "resource_choice", "notice"]
    required: bool = True
    options: list[QueryClarificationOption] = Field(default_factory=list)
    resource_candidates: list[QueryResourceCandidate] = Field(default_factory=list)


class QueryGenerationResponse(BaseModel):
    status: str
    query: Optional[str] = None
    query_language: str
    explanation: Optional[str] = None
    warnings: list[str] = Field(default_factory=list)
    query_parameters: list[QueryParameterDefinition]
    resources: Optional[list[ResourceFact]] = None
    clarifications: list[QueryClarification] = Field(default_factory=list)


@router.post(
    "/query/generate",
    response_model=QueryGenerationResponse,
    summary="生成查询语言 | Generate Query Language",
    responses={
        400: {"description": "请求不符合查询生成约束 | Invalid query generation request"},
        502: {"description": "Owner Tool 或推理服务调用失败 | Owner Tool or inference service call failed"},
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
                query_parameters=[],
                clarifications=[_notice_clarification(
                    "query.language",
                    f"当前引擎不支持查询语言 {language}",
                )],
            )
        if language not in {"sql", "mql", "cypher"}:
            return QueryGenerationResponse(
                status="need_clarification",
                query_language=language,
                query_parameters=[],
                clarifications=[_notice_clarification(
                    "query.language",
                    f"AI 助手暂不支持生成 {language} 查询",
                )],
            )
        if request.resource_scope_locator and (request.resources or request.current_query):
            raise ToolExecutionError(
                "invalid_arguments",
                "resource_scope_locator 只能用于编辑器为空且尚未确认具体资源的首次发现",
            )

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
        query_capability = _query_capability(engine)
        federation = query_capability.get("federation") or {}
        policy = ResourceResolutionPolicy.query(
            request.engine_id,
            allowed_source_engine_types=(
                frozenset(str(item).strip().lower() for item in federation.get("source_engine_types", []) if str(item).strip())
                if federation.get("supported") else None
            ),
        )
        if not request.resources:
            return await _discover_query_resources(request, resolver, policy, language)
        resources = await resolver.verify(request.resources, policy)
        try:
            generated = await query_service.generate(
                query=request.query,
                engine=engine,
                query_language=language,
                resources=[item.model_dump(exclude_none=True) for item in resources],
                current_query=request.current_query,
                tenant_id=user.tenant_id,
                db=db,
                clarification_answers=request.clarification_answers,
            )
        except QueryClarificationRequired as error:
            return QueryGenerationResponse(
                status="need_clarification",
                query_language=language,
                query_parameters=[],
                resources=resources,
                clarifications=[_semantic_clarification(error.clarification)],
            )
        return QueryGenerationResponse(
            status="success",
            query=generated["query"],
            query_language=language,
            explanation=generated.get("explanation"),
            warnings=generated.get("warnings", []),
            query_parameters=generated.get("query_parameters", []),
            resources=resources,
        )
    except ToolExecutionError as error:
        logger.warning("query generation tool call failed: code=%s message=%s", error.code, error.message)
        status_code = 400 if error.code == "invalid_arguments" else 502
        raise HTTPException(status_code=status_code, detail=error.message) from error
    except ValueError as error:
        logger.warning("query generation validation rejected candidate: %s", error)
        raise HTTPException(status_code=400, detail=str(error)) from error
    except InferenceError as error:
        logger.warning("query generation inference failed: code=%s", error.error_code)
        raise HTTPException(status_code=502, detail="上游推理服务调用失败") from error
    except Exception as error:
        logger.exception("query generation failed")
        raise HTTPException(status_code=500, detail="查询生成失败") from error


async def _discover_query_resources(
    request: QueryGenerationRequest,
    resolver: ResourceResolutionService,
    policy: ResourceResolutionPolicy,
    language: str,
) -> QueryGenerationResponse:
    result = await resolver.discover(
        request.query,
        policy,
        scope_locator=request.resource_scope_locator,
    )
    if not result.intents:
        return QueryGenerationResponse(
            status="need_clarification",
            query_language=language,
            query_parameters=[],
            clarifications=[_notice_clarification(
                "query.resources",
                "未能从需求中识别查询输入数据源",
                category="resource_selection",
            )],
        )
    if result.missing_roles:
        return QueryGenerationResponse(
            status="need_clarification",
            query_language=language,
            query_parameters=[],
            clarifications=[_notice_clarification(
                "query.resources",
                "未找到当前查询引擎内的资源：" + "、".join(result.missing_roles),
                category="resource_selection",
            )],
        )
    return QueryGenerationResponse(
        status="need_clarification",
        query_language=language,
        query_parameters=[],
        clarifications=[QueryClarification(
            key="query.resources",
            category="resource_selection",
            prompt="请选择当前查询使用的具体数据资源",
            control="resource_choice",
            resource_candidates=[QueryResourceCandidate.model_validate(item) for item in result.candidates],
        )],
    )


def _semantic_clarification(clarification: DomainQueryClarification) -> QueryClarification:
    return QueryClarification(
        key=clarification.key,
        category=clarification.category,
        prompt=clarification.prompt,
        control=clarification.control,
        required=clarification.required,
        options=[QueryClarificationOption(
            value=option.value,
            label=option.label,
            description=option.description,
        ) for option in clarification.options],
    )


def _notice_clarification(key: str, prompt: str, *, category: str = "query_capability") -> QueryClarification:
    return QueryClarification(
        key=key,
        category=category,
        prompt=prompt,
        control="notice",
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
    query_capability = _query_capability(engine)
    if not query_capability.get("supported"):
        return []
    return [str(item).strip().lower() for item in query_capability.get("languages", []) if str(item).strip()]


def _query_capability(engine: dict[str, Any]) -> dict[str, Any]:
    capabilities = engine.get("capabilities")
    if isinstance(capabilities, str):
        try:
            capabilities = json.loads(capabilities)
        except json.JSONDecodeError:
            capabilities = {}
    value = (capabilities or {}).get("compute", {}).get("query", {})
    return value if isinstance(value, dict) else {}
