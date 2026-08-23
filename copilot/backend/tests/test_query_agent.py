import asyncio

import pytest
from fastapi import HTTPException
from fastapi.security import HTTPAuthorizationCredentials

from addp_common.auth import AuthorizationContext
from addp_common.client.inference import InferenceError
from api import query_agent_api
from api.query_agent_api import QueryGenerationRequest
from addp_common.resources import ResourceFact


class FakeExecutor:
    def __init__(self, *_args, **_kwargs):
        self.calls = []

    async def call(self, name, arguments, **_audit):
        self.calls.append((name, arguments))
        if name == "engine.list":
            return [{
                "id": 8,
                "name": "Business PostgreSQL",
                "engine_type": "postgresql",
                "capabilities": {
                    "compute": {"query": {"supported": True, "languages": ["sql"]}}
                },
            }]
        if name == "resource.facts.get":
            return {
                "locator": arguments["locator"],
                "engine_id": int(arguments["locator"].split("/engine/", 1)[1].split("/", 1)[0]),
                "data_type": "table",
                "source_engine_type": "postgresql",
                "fields": [{"name": "shape", "type": "geometry(LineString,32650)"}],
                "geometry_column": "shape",
                "geometry_type": "LineString",
                "crs": "EPSG:32650",
            }
        raise AssertionError(name)


def test_query_generation_uses_only_current_engine_and_verified_resource(monkeypatch):
    executor = FakeExecutor()
    monkeypatch.setattr(query_agent_api, "ToolExecutor", lambda *_args, **_kwargs: executor)
    monkeypatch.setattr(
        query_agent_api.CopilotInferenceService,
        "chat_model",
        lambda *_args, **_kwargs: object(),
    )

    async def fake_generate(**kwargs):
        assert kwargs["engine"]["id"] == 8
        assert kwargs["query_language"] == "sql"
        assert kwargs["resources"][0]["engine_id"] == 8
        assert kwargs["resources"][0]["geometry_column"] == "shape"
        return {"query": "SELECT 1", "explanation": "candidate", "warnings": []}

    monkeypatch.setattr(query_agent_api.query_service, "generate", fake_generate)
    response = asyncio.run(query_agent_api.generate_query(
        QueryGenerationRequest(
            query="计算铁路两边宽度50米所占用的耕地面积",
            engine_id=8,
            query_language="sql",
            resources=[ResourceFact(
                role="铁路",
                engine_id=8,
                locator="addp://engine/8/path/public/railway?type=table&item_id=60",
            )],
        ),
        user=AuthorizationContext(principal_id=1, tenant_id=1),
        credentials=HTTPAuthorizationCredentials(scheme="Bearer", credentials="user-token"),
        db=None,
    ))

    assert response.status == "success"
    assert response.query == "SELECT 1"
    assert response.query_parameters == []
    assert [name for name, _ in executor.calls] == [
        "engine.list",
        "resource.facts.get",
    ]


def test_query_languages_read_engine_capability_without_engine_type_defaults():
    assert query_agent_api._query_languages({
        "engine_type": "custom",
        "capabilities": '{"compute":{"query":{"supported":true,"languages":["MQL"]}}}',
    }) == ["mql"]


def test_mongodb_collection_uses_owner_table_data_type_for_mql_generation(monkeypatch):
    executor = FakeExecutor()
    monkeypatch.setattr(query_agent_api, "ToolExecutor", lambda *_args, **_kwargs: executor)
    monkeypatch.setattr(
        query_agent_api.CopilotInferenceService,
        "chat_model",
        lambda *_args, **_kwargs: object(),
    )

    async def fake_generate(**kwargs):
        assert kwargs["query_language"] == "mql"
        assert kwargs["resources"][0]["data_type"] == "table"
        return {"query": '{"find":"Persons","filter":{"age":{"$gte":18}},"limit":10}', "warnings": []}

    monkeypatch.setattr(query_agent_api.query_service, "generate", fake_generate)
    response = asyncio.run(query_agent_api.generate_query(
        QueryGenerationRequest(
            query="查询成年人员",
            engine_id=11,
            query_language="mql",
            engine_context={
                "id": 11,
                "engine_type": "mongodb",
                "capabilities": {
                    "compute": {"query": {"supported": True, "languages": ["mql"]}}
                },
            },
            resources=[ResourceFact(
                role="人员",
                engine_id=11,
                locator="addp://engine/11/path/Outdoor/Persons?type=collection&item_id=51657",
            )],
        ),
        user=AuthorizationContext(principal_id=1, tenant_id=1),
        credentials=HTTPAuthorizationCredentials(scheme="Bearer", credentials="user-token"),
        db=None,
    ))

    assert response.status == "success"
    assert response.query == '{"find":"Persons","filter":{"age":{"$gte":18}},"limit":10}'


def test_semantic_clarification_is_returned_as_structured_interaction(monkeypatch):
    executor = FakeExecutor()
    monkeypatch.setattr(query_agent_api, "ToolExecutor", lambda *_args, **_kwargs: executor)
    monkeypatch.setattr(query_agent_api.CopilotInferenceService, "chat_model", lambda *_args, **_kwargs: object())

    from services.query_clarification import (
        ClarificationOption,
        QueryClarification,
        QueryClarificationRequired,
    )

    async def fake_generate(**_kwargs):
        raise QueryClarificationRequired(QueryClarification(
            key="metric.definition",
            category="calculation_rule",
            prompt="请选择指标计算规则",
            control="single_choice",
            options=(ClarificationOption("count", "数量"), ClarificationOption("ratio", "比例")),
        ))

    monkeypatch.setattr(query_agent_api.query_service, "generate", fake_generate)
    response = asyncio.run(query_agent_api.generate_query(
        QueryGenerationRequest(
            query="计算指标",
            engine_id=11,
            query_language="mql",
            engine_context={
                "id": 11,
                "engine_type": "mongodb",
                "capabilities": {"compute": {"query": {"supported": True, "languages": ["mql"]}}},
            },
            resources=[ResourceFact(
                role="人员",
                engine_id=11,
                locator="addp://engine/11/path/Outdoor/Persons?type=collection&item_id=51657",
            )],
        ),
        user=AuthorizationContext(principal_id=1, tenant_id=1),
        credentials=HTTPAuthorizationCredentials(scheme="Bearer", credentials="user-token"),
        db=None,
    ))

    assert response.status == "need_clarification"
    assert response.clarifications[0].key == "metric.definition"
    assert response.clarifications[0].control == "single_choice"
    assert [option.value for option in response.clarifications[0].options] == ["count", "ratio"]
    assert not hasattr(response, "clarification_reason")


def test_mongodb_current_query_without_resource_enters_resource_discovery(monkeypatch):
    executor = FakeExecutor()
    monkeypatch.setattr(query_agent_api, "ToolExecutor", lambda *_args, **_kwargs: executor)
    monkeypatch.setattr(
        query_agent_api.CopilotInferenceService,
        "chat_model",
        lambda *_args, **_kwargs: object(),
    )

    class FakeResolver:
        async def discover(self, _query, _policy, *, scope_locator=None):
            assert scope_locator is None
            return type("Result", (), {"intents": [], "missing_roles": [], "candidates": []})()

    monkeypatch.setattr(query_agent_api, "ResourceResolutionService", lambda **_kwargs: FakeResolver())
    response = asyncio.run(query_agent_api.generate_query(
        QueryGenerationRequest(
            query="只保留成年人",
            engine_id=11,
            query_language="mql",
            current_query='{"find":"Persons","filter":{},"limit":10}',
            engine_context={
                "id": 11,
                "engine_type": "mongodb",
                "capabilities": {
                    "compute": {"query": {"supported": True, "languages": ["mql"]}}
                },
            },
            resources=[],
        ),
        user=AuthorizationContext(principal_id=1, tenant_id=1),
        credentials=HTTPAuthorizationCredentials(scheme="Bearer", credentials="user-token"),
        db=None,
    ))

    assert response.status == "need_clarification"
    assert response.clarifications[0].key == "query.resources"
    assert response.clarifications[0].control == "notice"


def test_mongodb_database_scope_is_forwarded_to_shared_resource_resolution(monkeypatch):
    executor = FakeExecutor()
    monkeypatch.setattr(query_agent_api, "ToolExecutor", lambda *_args, **_kwargs: executor)
    monkeypatch.setattr(
        query_agent_api.CopilotInferenceService,
        "chat_model",
        lambda *_args, **_kwargs: object(),
    )
    scope = "addp://engine/11/path/Outdoor?type=database&node_id=276"
    calls = []

    class FakeResolver:
        async def discover(self, query, policy, *, scope_locator=None):
            calls.append((query, policy.engine_id, scope_locator))
            return type("Result", (), {"intents": [], "missing_roles": [], "candidates": []})()

    monkeypatch.setattr(query_agent_api, "ResourceResolutionService", lambda **_kwargs: FakeResolver())
    response = asyncio.run(query_agent_api.generate_query(
        QueryGenerationRequest(
            query="查询用户参加的活动",
            engine_id=11,
            query_language="mql",
            resource_scope_locator=scope,
            engine_context={
                "id": 11,
                "engine_type": "mongodb",
                "capabilities": {"compute": {"query": {"supported": True, "languages": ["mql"]}}},
            },
            resources=[],
        ),
        user=AuthorizationContext(principal_id=1, tenant_id=1),
        credentials=HTTPAuthorizationCredentials(scheme="Bearer", credentials="user-token"),
        db=None,
    ))

    assert response.status == "need_clarification"
    assert calls == [("查询用户参加的活动", 11, scope)]


def test_query_local_invalid_arguments_return_400(monkeypatch):
    monkeypatch.setattr(query_agent_api, "ToolExecutor", FakeExecutor)

    try:
        asyncio.run(query_agent_api.generate_query(
            QueryGenerationRequest(
                query="查询人员",
                engine_id=11,
                query_language="mql",
                engine_context={"id": 12},
            ),
            user=AuthorizationContext(principal_id=1, tenant_id=1),
            credentials=HTTPAuthorizationCredentials(scheme="Bearer", credentials="user-token"),
            db=None,
        ))
    except HTTPException as error:
        assert error.status_code == 400
    else:
        raise AssertionError("local invalid arguments must return HTTP 400")


def test_query_inference_upstream_failure_returns_502(monkeypatch):
    monkeypatch.setattr(query_agent_api, "ToolExecutor", FakeExecutor)
    monkeypatch.setattr(
        query_agent_api.CopilotInferenceService,
        "chat_model",
        lambda *_args, **_kwargs: object(),
    )

    class FailingResolver:
        async def discover(self, *_args, **_kwargs):
            raise InferenceError("inference_upstream_failed", "upstream unavailable")

    monkeypatch.setattr(query_agent_api, "ResourceResolutionService", lambda **_kwargs: FailingResolver())

    with pytest.raises(HTTPException) as captured:
        asyncio.run(query_agent_api.generate_query(
            QueryGenerationRequest(
                query="查询人员",
                engine_id=11,
                query_language="mql",
                engine_context={
                    "id": 11,
                    "engine_type": "mongodb",
                    "capabilities": {"compute": {"query": {"supported": True, "languages": ["mql"]}}},
                },
            ),
            user=AuthorizationContext(principal_id=1, tenant_id=1),
            credentials=HTTPAuthorizationCredentials(scheme="Bearer", credentials="user-token"),
            db=None,
        ))

    assert captured.value.status_code == 502
    assert captured.value.detail == "上游推理服务调用失败"
