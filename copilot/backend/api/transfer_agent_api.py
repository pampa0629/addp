"""Transfer 模块内的 AI 助手。

助手仅提取资源意图或使用调用方提供的上下文生成草稿。
不访问资源 Owner、不二次委托，也不创建或启动任务。
"""

from __future__ import annotations

from typing import Any, Literal
from urllib.parse import urlparse

from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy.orm import Session

from addp_common.auth import AuthorizationContext
from authorization_permissions_generated import COPILOT_TRANSFER_EXECUTE
from chains.resource_intent_chain import ResourceIntent, ResourceIntentChain, ResourceIntentScope
from chains.transfer_generation_chain import TransferGenerationChain
from database import get_db
from dependencies.auth import require_tool_user
from addp_common.resources import ResourceFact
from services.inference_service import (
    CopilotInferenceService,
    InferenceClientNotInitialized,
    InferenceScenarioNotConfigured,
)

router = APIRouter()
require_transfer_draft_tool = require_tool_user(
    "copilot",
    "transfer.draft.generate",
    COPILOT_TRANSFER_EXECUTE,
)


class TransferGenerationRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    query: str = Field(min_length=1, max_length=4000)
    resources: list[ResourceFact] = Field(
        default_factory=list, max_length=1,
        description="调用方提供的源资源上下文，不是授权证明 | Caller-supplied source context, not proof of authorization",
    )
    task: dict[str, Any] | None = Field(
        default=None,
        description="由 Transfer 向导构造的当前草稿；不接受 endpoint 之外的身份或凭据",
    )


class TransferGenerationResponse(BaseModel):
    status: Literal["intents_ready", "need_clarification", "success"]
    task: dict[str, Any] | None = None
    resources: list[ResourceFact] | None = None
    intents: list[ResourceIntent] | None = Field(
        default=None,
        description="无资源时返回的检索意图，由调用方发现资源 | Search intents for caller-owned discovery when no resource is supplied",
    )
    clarification_reason: str | None = None
    message: str | None = None
    warnings: list[str] = Field(default_factory=list)


@router.post(
    "/transfer/generate",
    response_model=TransferGenerationResponse,
    summary="提取 Transfer 源意图或生成任务草稿 | Extract Transfer source intent or generate task draft",
    responses={503: {"description": "推理场景未配置或推理运行时未就绪 | Inference scenario is not configured or runtime is not ready"}},
    openapi_extra={
        "x-addp-auth-mode": "delegated_tool",
        "x-addp-required-permissions": [COPILOT_TRANSFER_EXECUTE],
    },
)
async def generate_transfer(
    request: TransferGenerationRequest,
    user: AuthorizationContext = Depends(require_transfer_draft_tool),
    db: Session = Depends(get_db),
):
    """提取单一源意图，或在调用方上下文内生成无副作用的草稿。"""
    try:
        if not request.resources:
            if request.task is not None:
                raise ValueError("transfer_source_context_missing")
            resolution_llm = CopilotInferenceService.chat_model(
                db, tenant_id=user.tenant_id, scenario_code="resource_resolution",
                temperature=0, max_output_tokens=1200,
            )
            intents = await ResourceIntentChain(resolution_llm).extract(
                request.query, scope=ResourceIntentScope.TRANSFER_SOURCE,
            )
            if len(intents) != 1:
                return TransferGenerationResponse(
                    status="need_clarification",
                    clarification_reason="single_source_required",
                    message="Transfer 任务一次只允许一个源资源，请明确要传输的源数据",
                )
            return TransferGenerationResponse(status="intents_ready", intents=intents)

        source = request.resources[0]
        if request.task is None:
            return TransferGenerationResponse(
                status="need_clarification",
                resources=[source],
                clarification_reason="target_configuration_required",
                message="请先在 Transfer 向导中确认目标引擎和目标位置",
            )

        _validate_task_context(request.task, source)
        target = request.task["config"]["target"]
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
    if source.engine_id != _locator_engine_id(source.locator):
        raise ValueError("transfer_source_engine_mismatch")
    if not source.data_type or source_config.get("data_type") != source.data_type:
        raise ValueError("transfer_source_datatype_mismatch")
    source_unknown = set(source_config) - {"locator", "data_type", "representation", "format", "options", "policy", "change_stream", "query"}
    if source_unknown:
        raise ValueError("Transfer source endpoint 包含不受支持的字段")
    target = config["target"]
    if not isinstance(target, dict) or not target.get("parent_locator") or not target.get("name"):
        raise ValueError("Transfer 草稿必须先确认目标父节点和目标名称")
    target_unknown = set(target) - {"parent_locator", "name", "data_type", "representation", "format", "options", "policy"}
    if target_unknown:
        raise ValueError("Transfer target endpoint 包含不受支持的字段")
    _locator_engine_id(target["parent_locator"])
    boundary = config["runtime"].get("boundary") if isinstance(config["runtime"], dict) else None
    if boundary not in {"bounded", "continuous"}:
        raise ValueError("Transfer runtime.boundary 不受支持")
    load_mode = config["load"].get("mode") if isinstance(config["load"], dict) else None
    if load_mode not in {"snapshot", "incremental"}:
        raise ValueError("Transfer load.mode 不受支持")


def _locator_engine_id(locator: str) -> int:
    if not isinstance(locator, str) or not locator.strip():
        raise ValueError("Transfer locator 必须是 ADDP ResourceLocator")
    parsed = urlparse(locator)
    if parsed.scheme != "addp" or parsed.netloc != "engine":
        raise ValueError("Transfer locator 必须是 ADDP ResourceLocator")
    parts = [part for part in parsed.path.split("/") if part]
    if len(parts) < 2 or not parts[0].isdigit() or parts[1] != "path" or int(parts[0]) <= 0:
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
    if source_config.get("query") is not None:
        # Collection/table facts do not describe a query's projected or aliased output.
        # Only rename an already-confirmed direct mapping; never add a source column.
        source_fields.intersection_update(
            str(field.get("source") or "").strip()
            for transform in config.get("transforms", [])
            if isinstance(transform, dict) and transform.get("type") == "field_mapping"
            for field in transform.get("fields", [])
            if isinstance(field, dict)
        )
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
