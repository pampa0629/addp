import asyncio

import pytest
from fastapi import HTTPException
from fastapi.security import HTTPAuthorizationCredentials
from pydantic import ValidationError

from api.transfer_agent_api import (
    TransferGenerationRequest,
    _build_task_draft,
    _locator_engine_id,
    _validate_task_context,
    generate_transfer,
)
from chains.transfer_generation_chain import TransferFieldMappingIntent, TransferGenerationOutput
from addp_common.resources import ResourceFact
from addp_common.auth import AuthorizationContext, RoleAssignment
from services.inference_service import InferenceScenarioNotConfigured


def _source():
    return ResourceFact(
        role="source",
        engine_id=8,
        locator="addp://engine/8/path/public/roads?type=table&item_id=60",
        data_type="table",
        fields=[{"name": "road_id", "type": "bigint"}, {"name": "name", "type": "string"}],
    )


def _task(source):
    return {
        "name": "",
        "description": "",
        "task_type": "sync",
        "config": {
            "runtime": {"boundary": "bounded"},
            "load": {"mode": "snapshot"},
            "source": {"locator": source.locator, "data_type": "table", "representation": "native"},
            "target": {
                "parent_locator": "addp://engine/9/path/public?type=schema&node_id=12",
                "name": "roads_copy",
                "data_type": "table",
                "representation": "native",
                "policy": {"apply_mode": "replace"},
            },
            "transforms": [],
        },
    }


def test_transfer_request_forbids_identity_fields():
    with pytest.raises(ValidationError):
        TransferGenerationRequest(query="传输道路", tenant_id=1)


def test_transfer_request_accepts_registered_source_engine_scope():
    request = TransferGenerationRequest(query="从 pg 到 mysql，同步 farmland", source_engine_id=8)
    assert request.source_engine_id == 8


def test_transfer_reports_missing_inference_binding_as_service_unavailable(monkeypatch):
    def missing_binding(*_args, **_kwargs):
        raise InferenceScenarioNotConfigured("inference_scenario_not_configured")

    monkeypatch.setattr(
        "api.transfer_agent_api.CopilotInferenceService.chat_model",
        missing_binding,
    )
    context = AuthorizationContext(
        principal_id=11,
        principal_type="user",
        token_type="delegated_access_token",
        client_id="addp-web",
        context_type="tenant",
        tenant_id=7,
        tenant_membership_id=9,
        role_assignments=(
            RoleAssignment(4, "tenant.transfer_user", "tenant", ("copilot.transfer.execute",)),
        ),
    )
    with pytest.raises(HTTPException) as error:
        asyncio.run(generate_transfer(
            TransferGenerationRequest(query="从 pg 到 mysql，同步 farmland"),
            context,
            HTTPAuthorizationCredentials(scheme="Bearer", credentials="delegated-token"),
            object(),
        ))
    assert error.value.status_code == 503
    assert error.value.detail == "transfer_inference_scenario_not_configured"


def test_transfer_locator_uses_engine_segment_from_resource_locator():
    assert _locator_engine_id("addp://engine/9/path/public?type=schema") == 9
    with pytest.raises(ValueError):
        _locator_engine_id("https://example.invalid/data")


def test_transfer_context_rejects_legacy_parallel_fields():
    source = _source()
    task = _task(source)
    task["config"]["mode"] = "snapshot"
    with pytest.raises(ValueError, match="旧配置字段"):
        _validate_task_context(task, source)


def test_transfer_context_rejects_endpoint_credentials_and_private_facts():
    source = _source()
    task = _task(source)
    task["config"]["target"]["connection_info"] = {"password": "secret"}
    with pytest.raises(ValueError, match="target endpoint"):
        _validate_task_context(task, source)


def test_transfer_draft_preserves_owner_boundary_and_filters_unknown_mapping_source():
    source = _source()
    task = _task(source)
    intent = TransferGenerationOutput(
        name="道路同步",
        description="同步道路",
        mappings=[
            TransferFieldMappingIntent(source="road_id", target="id"),
            TransferFieldMappingIntent(source="not_a_real_field", target="x"),
        ],
    )
    draft = _build_task_draft(task, source, intent)
    assert draft["task_type"] == "sync"
    assert draft["config"]["runtime"] == {"boundary": "bounded"}
    assert draft["config"]["target"]["parent_locator"] == task["config"]["target"]["parent_locator"]
    assert draft["config"]["transforms"][0]["fields"] == [{"source": "road_id", "target": "id"}]


def test_transfer_draft_merges_mapping_without_removing_existing_fields_or_transforms():
    source = _source()
    task = _task(source)
    task["config"]["transforms"] = [
        {
            "type": "field_mapping",
            "version": "v1",
            "mode": "project",
            "fields": [
                {"source": "road_id", "target": "road_id", "target_type": "bigint"},
                {"source": "name", "target": "name", "target_type": "string"},
            ],
        },
        {"type": "custom_transform", "version": "v1"},
    ]
    draft = _build_task_draft(
        task,
        source,
        TransferGenerationOutput(
            name="道路同步",
            mappings=[TransferFieldMappingIntent(source="road_id", target="id")],
        ),
    )
    assert draft["config"]["transforms"] == [
        {
            "type": "field_mapping",
            "version": "v1",
            "mode": "project",
            "fields": [
                {"source": "road_id", "target": "id", "target_type": "bigint"},
                {"source": "name", "target": "name", "target_type": "string"},
            ],
        },
        {"type": "custom_transform", "version": "v1"},
    ]
