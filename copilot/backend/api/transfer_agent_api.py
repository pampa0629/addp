"""Transfer 模块内的 AI 助手。

助手只返回待人工检查的任务草稿。源资源、目标父节点和 Transfer 任务配置
均由 owner 重新验证，接口不会创建或启动任务。
"""

from __future__ import annotations

from typing import Any
from urllib.parse import urlparse
from uuid import uuid4

from fastapi import APIRouter, Depends, HTTPException
from fastapi.security import HTTPAuthorizationCredentials
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy.orm import Session

from addp_common.auth import AuthorizationContext
from addp_common.tools import ToolExecutionError, ToolExecutor
from authorization_permissions_generated import COPILOT_TRANSFER_EXECUTE
from chains.resource_intent_chain import ResourceIntentChain
from chains.resource_recommendation_chain import ResourceRecommendationChain
from chains.transfer_generation_chain import TransferGenerationChain
from config import settings
from database import get_db
from dependencies.auth import bearer_auth, require_tool_user
from addp_common.resources import ResourceFact
from services.inference_service import (
    CopilotInferenceService,
    InferenceClientNotInitialized,
    InferenceScenarioNotConfigured,
)
from services.resource_discovery import ResourceDiscovery
from services.resource_resolution import ResourceResolutionPolicy, ResourceResolutionService

router = APIRouter()
require_transfer_draft_tool = require_tool_user(
    "copilot",
    "transfer.draft.generate",
    COPILOT_TRANSFER_EXECUTE,
)


class TransferGenerationRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    query: str = Field(min_length=1, max_length=4000)
    resources: list[ResourceFact] = Field(default_factory=list, max_length=1)
    source_engine_id: int | None = Field(
        default=None,
        ge=1,
        description="由 System 已注册引擎解析出的源引擎实例 ID；仅用于限定源资源发现",
    )
    task: dict[str, Any] | None = Field(
        default=None,
        description="由 Transfer 向导构造的当前草稿；不接受 endpoint 之外的身份或凭据",
    )


class TransferResourceCandidate(ResourceFact):
    name: str
    engine_name: str | None = None
    asset_type: str
    full_name: str | None = None
    path: str | None = None
    score: float | None = None
    ancestors: list[dict[str, Any]] = Field(default_factory=list)
    recommended: bool = False
    recommendation_reason: str | None = None
    representation: str | None = None
    format: str | None = None


class TransferGenerationResponse(BaseModel):
    status: str
    task: dict[str, Any] | None = None
    resources: list[ResourceFact] | None = None
    data_source_candidates: list[TransferResourceCandidate] | None = None
    clarification_reason: str | None = None
    message: str | None = None
    warnings: list[str] = Field(default_factory=list)


@router.post(
    "/transfer/generate",
    response_model=TransferGenerationResponse,
    summary="生成 Transfer 任务草稿 | Generate Transfer task draft",
    responses={503: {"description": "推理场景未配置或推理运行时未就绪 | Inference scenario is not configured or runtime is not ready"}},
    openapi_extra={
        "x-addp-auth-mode": "delegated_tool",
        "x-addp-required-permissions": [COPILOT_TRANSFER_EXECUTE],
    },
)
async def generate_transfer(
    request: TransferGenerationRequest,
    user: AuthorizationContext = Depends(require_transfer_draft_tool),
    credentials: HTTPAuthorizationCredentials = Depends(bearer_auth),
    db: Session = Depends(get_db),
):
    """发现并确认单一源资源，之后生成不带副作用的 Transfer 草稿。"""
    executor = ToolExecutor(settings.get_gateway_url(), credentials.credentials)
    try:
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
        policy = ResourceResolutionPolicy.transfer(request.source_engine_id)
        if not request.resources:
            return await _discover_transfer_resources(
                request.query,
                resolver,
                policy,
            )

        source = (await resolver.verify(request.resources, policy))[0]
        if request.task is None:
            return TransferGenerationResponse(
                status="need_clarification",
                resources=[source],
                clarification_reason="target_configuration_required",
                message="请先在 Transfer 向导中确认目标引擎和目标位置",
            )

        _validate_task_context(request.task, source)
        target = request.task["config"]["target"]
        await _verify_target_parent(executor, target)
        current_task = {
            "name": request.task.get("name", ""),
            "description": request.task.get("description", ""),
        }
        llm = CopilotInferenceService.chat_model(
            db,
            tenant_id=user.tenant_id,
            scenario_code="transfer_generation",
            temperature=0,
            max_output_tokens=1600,
        )
        intent = await TransferGenerationChain(llm).generate(
            request.query,
            source=source.model_dump(exclude_none=True),
            target=target,
            current_task=current_task,
        )
        task = _build_task_draft(request.task, source, intent)
        return TransferGenerationResponse(
            status="success",
            task=task,
            resources=[source],
            warnings=["运行边界、装载模式、目标引擎和目标策略沿用 Transfer 向导当前选择；请在提交前复核。"],
        )
    except ToolExecutionError as error:
        raise HTTPException(status_code=502, detail=error.message) from error
    except InferenceScenarioNotConfigured as error:
        raise HTTPException(
            status_code=503,
            detail="transfer_inference_scenario_not_configured",
        ) from error
    except InferenceClientNotInitialized as error:
        raise HTTPException(
            status_code=503,
            detail="copilot_inference_runtime_not_initialized",
        ) from error
    except ValueError as error:
        raise HTTPException(status_code=400, detail=str(error)) from error
    except Exception as error:
        raise HTTPException(status_code=500, detail="Transfer 草稿生成失败") from error


async def _discover_transfer_resources(
    query: str,
    resolver: ResourceResolutionService,
    policy: ResourceResolutionPolicy,
) -> TransferGenerationResponse:
    result = await resolver.discover(query, policy)
    if len(result.intents) != 1:
        return TransferGenerationResponse(
            status="need_clarification",
            clarification_reason="single_source_required",
            message="Transfer 任务一次只允许一个源资源，请明确要传输的源数据",
            data_source_candidates=[],
        )
    if result.missing_roles:
        return TransferGenerationResponse(
            status="need_clarification",
            clarification_reason="data_source_not_found",
            message="未找到可用于 Transfer 的源资源：" + "、".join(result.missing_roles),
            data_source_candidates=[],
        )
    return TransferGenerationResponse(
        status="need_clarification",
        clarification_reason="data_source_confirmation_required",
        message="请确认唯一源资源后再生成 Transfer 草稿",
        data_source_candidates=[TransferResourceCandidate.model_validate(item) for item in result.candidates],
    )


def _validate_task_context(task: dict[str, Any], source: ResourceFact) -> None:
    if set(task) - {"name", "description", "task_type", "config", "schedule", "enabled", "batch_size", "auto_scan_metadata"}:
        raise ValueError("Transfer 草稿包含不受支持的字段")
    if task.get("task_type") not in (None, "", "sync"):
        raise ValueError("Transfer 任务类型固定为 sync")
    config = task.get("config")
    if not isinstance(config, dict) or not {"runtime", "load", "source", "target"}.issubset(config):
        raise ValueError("Transfer 草稿必须包含 runtime、load、source 和 target")
    if any(key in config for key in ("mode", "write_mode", "connector_type", "source_config", "target_config", "output_format", "file_type")):
        raise ValueError("Transfer 草稿包含已删除的旧配置字段")
    source_config = config["source"]
    if not isinstance(source_config, dict) or source_config.get("locator") != source.locator:
        raise ValueError("Transfer 草稿 source locator 必须与已确认资源一致")
    source_unknown = set(source_config) - {"locator", "data_type", "representation", "format", "options", "policy", "change_stream"}
    if source_unknown:
        raise ValueError("Transfer source endpoint 包含不受支持的字段")
    target = config["target"]
    if not isinstance(target, dict) or not target.get("parent_locator") or not target.get("name"):
        raise ValueError("Transfer 草稿必须先确认目标父节点和目标名称")
    target_unknown = set(target) - {"parent_locator", "name", "data_type", "representation", "format", "options", "policy"}
    if target_unknown:
        raise ValueError("Transfer target endpoint 包含不受支持的字段")
    boundary = config["runtime"].get("boundary") if isinstance(config["runtime"], dict) else None
    if boundary not in {"bounded", "continuous"}:
        raise ValueError("Transfer runtime.boundary 不受支持")
    load_mode = config["load"].get("mode") if isinstance(config["load"], dict) else None
    if load_mode not in {"snapshot", "incremental"}:
        raise ValueError("Transfer load.mode 不受支持")


async def _verify_target_parent(executor: ToolExecutor, target: dict[str, Any]) -> None:
    parent_locator = str(target.get("parent_locator") or "").strip()
    engine_id = _locator_engine_id(parent_locator)
    ancestors = await executor.call(
        "resource.ancestors.get",
        {"engine_id": engine_id, "locator": parent_locator},
        agent_run_id=f"copilot-transfer-{uuid4()}",
        tool_call_id=f"transfer-target-ancestors-{uuid4()}",
    )
    if not isinstance(ancestors, dict) or ancestors.get("target_locator") != parent_locator:
        raise ToolExecutionError("invalid_owner_response", "Meta 未确认 Transfer 目标父 locator")


def _locator_engine_id(locator: str) -> int:
    parsed = urlparse(locator)
    if parsed.scheme != "addp" or parsed.netloc != "engine":
        raise ValueError("Transfer locator 必须是 ADDP ResourceLocator")
    parts = [part for part in parsed.path.split("/") if part]
    if len(parts) < 3 or not parts[0].isdigit() or parts[1] != "path" or int(parts[0]) <= 0:
        raise ValueError("Transfer locator 缺少有效 engine_id")
    return int(parts[0])


def _build_task_draft(task: dict[str, Any], source: ResourceFact, intent: Any) -> dict[str, Any]:
    result = {key: value for key, value in task.items() if key != "config"}
    result["task_type"] = "sync"
    config = {key: value for key, value in task["config"].items()}
    source_config = {key: value for key, value in config["source"].items()}
    source_config.update({"locator": source.locator, "data_type": source.data_type or source_config.get("data_type", "table")})
    config["source"] = source_config
    source_fields = {
        str(field.get("name")).strip()
        for field in source.fields
        if isinstance(field, dict) and str(field.get("name") or "").strip()
    }
    mappings = [
        mapping for mapping in intent.mappings
        if mapping.source.strip() in source_fields and mapping.target.strip()
    ]
    if mappings and source.data_type == "table":
        transforms = [dict(value) for value in config.get("transforms", []) if isinstance(value, dict)]
        mapping_index = next(
            (index for index, value in enumerate(transforms) if value.get("type") == "field_mapping"),
            None,
        )
        field_mapping = (
            dict(transforms[mapping_index])
            if mapping_index is not None
            else {"type": "field_mapping", "version": "v1", "mode": "project", "fields": []}
        )
        fields = [dict(value) for value in field_mapping.get("fields", []) if isinstance(value, dict)]
        field_index = {
            str(value.get("source") or "").strip(): index
            for index, value in enumerate(fields)
            if str(value.get("source") or "").strip()
        }
        for mapping in mappings:
            source_name = mapping.source.strip()
            target_name = mapping.target.strip()
            if source_name in field_index:
                fields[field_index[source_name]]["target"] = target_name
            else:
                field_index[source_name] = len(fields)
                fields.append({"source": source_name, "target": target_name})
        field_mapping["fields"] = fields
        if mapping_index is None:
            transforms.insert(0, field_mapping)
        else:
            transforms[mapping_index] = field_mapping
        config["transforms"] = transforms
    result["config"] = config
    result["name"] = intent.name or result.get("name") or "Transfer sync task"
    result["description"] = intent.description or result.get("description", "")
    return result
