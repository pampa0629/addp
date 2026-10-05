import asyncio
import copy
import json
from unittest.mock import AsyncMock

import httpx
import pytest
from fastapi import FastAPI, HTTPException
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
from addp_common.client import CopilotClient
from addp_common.tools import ToolExecutor
from api import transfer_agent_api


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
            RoleAssignment(4, "tenant.transfer_user", "tenant", ("copilot.transfer.execute",), 7),
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


def test_transfer_tool_context_accepts_query_and_preserves_confirmed_configuration():
    from jsonschema import Draft202012Validator
    from addp_common.tools import get_tool

    source = _source()
    task = _task(source)
    task["config"]["source"]["query"] = {
        "language": "sql", "statement": "SELECT road_id, name FROM public.roads",
        "parameters": {},
    }
    task["config"]["transforms"] = [{
        "type": "field_mapping", "version": "v1", "mode": "project",
        "fields": [{"source": "road_id", "target": "road_id", "target_type": "bigint"}],
    }]
    arguments = {"query": "完善任务名称和说明", "resources": [source.model_dump()], "task": task}
    assert not list(Draft202012Validator(get_tool("transfer.draft.generate").input_schema).iter_errors(arguments))
    request = TransferGenerationRequest.model_validate(arguments)
    _validate_task_context(request.task, request.resources[0])
    draft = _build_task_draft(request.task, request.resources[0], TransferGenerationOutput(name="道路同步"))
    assert draft["config"] == task["config"]
    assert task["name"] == ""


def test_transfer_resource_payload_from_failed_agent_call_is_not_a_resource_fact():
    with pytest.raises(ValidationError) as error:
        TransferGenerationRequest.model_validate({
            "query": "生成 MongoDB 到 PostgreSQL 的草稿",
            "resources": [{
                "locator": "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=111",
                "name": "Outdoors", "item_type": "collection",
                "row_grain": "one_document_per_row", "project_fields": ["status"],
            }],
        })
    assert {issue["loc"][-1] for issue in error.value.errors()} == {
        "role", "name", "item_type", "row_grain", "project_fields",
    }


def test_query_source_draft_cannot_add_unconfirmed_output_columns():
    source = _source()  # Owner Schema has both road_id and name, but this query outputs only road_id.
    task = _task(source)
    task["config"]["source"]["query"] = {"language": "sql", "statement": "SELECT road_id FROM public.roads"}
    task["config"]["transforms"] = [{
        "type": "field_mapping", "version": "v1", "mode": "project",
        "fields": [{"source": "road_id", "target": "road_id", "target_type": "bigint"}],
    }]
    original = copy.deepcopy(task)
    draft = _build_task_draft(task, source, TransferGenerationOutput(
        name="道路编号归档", mappings=[
            TransferFieldMappingIntent(source="road_id", target="id"),
            TransferFieldMappingIntent(source="name", target="road_name"),
            TransferFieldMappingIntent(source="guessed_query_alias", target="alias"),
        ],
    ))
    assert draft["config"]["source"] == original["config"]["source"]
    assert draft["config"]["transforms"][0]["fields"] == [{"source": "road_id", "target": "id", "target_type": "bigint"}]
    assert task == original

    task["config"]["transforms"] = []
    draft = _build_task_draft(task, source, TransferGenerationOutput(
        name="道路编号归档",
        mappings=[TransferFieldMappingIntent(source="road_id", target="id")],
    ))
    assert draft["config"]["transforms"] == []


def test_transfer_draft_tool_sdk_http_contract_preserves_mongodb_configuration(monkeypatch):
    source = ResourceFact(
        role="source", engine_id=11,
        locator="addp://engine/11/path/Outdoor/Activities?type=collection&item_id=42",
        source_engine_type="mongodb", data_type="table", query_names={"mql": "Activities"},
        fields=[{"name": "_id", "type": "string"}, {"name": "status", "type": "string"}],
    )
    task = _task(source)
    task["config"]["source"]["query"] = {
        "language": "mql", "statement": '{"aggregate":"Activities","pipeline":[{"$project":{"_id":1,"status":1}}]}',
    }
    task["config"]["transforms"] = [{
        "type": "field_mapping", "version": "v1", "mode": "project",
        "fields": [{"source": "_id", "target": "_id", "target_type": "string"},
                   {"source": "status", "target": "status", "target_type": "string"}],
    }]
    original = copy.deepcopy(task)
    verify_source = AsyncMock(return_value=[source])
    verify_target = AsyncMock()
    generate_intent = AsyncMock(return_value=TransferGenerationOutput(name="活动同步", description="归档活动"))
    monkeypatch.setattr(transfer_agent_api.CopilotInferenceService, "chat_model", lambda *_args, **_kwargs: object())
    monkeypatch.setattr(transfer_agent_api.ResourceResolutionService, "verify", verify_source)
    monkeypatch.setattr(transfer_agent_api, "_verify_target_parent", verify_target)
    monkeypatch.setattr(transfer_agent_api.TransferGenerationChain, "generate", generate_intent)
    app = FastAPI()
    app.include_router(transfer_agent_api.router, prefix="/api/v1/copilot")
    app.dependency_overrides[transfer_agent_api.require_transfer_draft_tool] = lambda: AuthorizationContext(
        principal_id=11, tenant_id=7, tenant_membership_id=9, token_type="delegated_access_token",
    )
    app.dependency_overrides[transfer_agent_api.get_db] = lambda: object()
    requests = []

    async def run():
        transport = httpx.ASGITransport(app=app)

        async def record_and_dispatch(request):
            requests.append(request)
            assert request.method == "POST" and request.url.path == "/api/v1/copilot/transfer/generate"
            assert request.headers["Authorization"] == "Bearer addp_dat_fixture"
            body = json.loads(request.content)
            assert body == {"query": "补充活动归档名称", "resources": [source.model_dump()], "task": original}
            return await transport.handle_async_request(request)

        def client_factory(client_type, token):
            assert client_type is CopilotClient
            client = CopilotClient("http://gateway", user_token=token)
            client._client._transport = httpx.MockTransport(record_and_dispatch)
            return client

        executor = ToolExecutor("http://gateway", "private-user-token")
        executor._client = client_factory
        executor._issue_delegated_token = AsyncMock(return_value="addp_dat_fixture")
        return await executor.call(
            "transfer.draft.generate", {"query": "补充活动归档名称", "resources": [source.model_dump()], "task": task},
            agent_run_id="run-draft", tool_call_id="call-draft",
        )

    result = asyncio.run(run())
    assert result["status"] == "success"
    assert result["task"]["name"] == "活动同步"
    assert result["task"]["config"] == original["config"]
    assert task == original
    assert len(requests) == 1  # Draft only; no query execution, task creation, scan or business write.
    verify_source.assert_awaited_once()
    verify_target.assert_awaited_once()
    generate_intent.assert_awaited_once()


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
