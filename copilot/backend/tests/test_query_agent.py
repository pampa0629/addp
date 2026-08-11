import asyncio

from fastapi import HTTPException
from fastapi.security import HTTPAuthorizationCredentials

from addp_common.auth import AuthorizationContext
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
        if name == "resource.ancestors.get":
            return {"target_locator": arguments["locator"], "ancestors": []}
        if name == "data.preview":
            return {
                "metadata": {"locator": arguments["locator"]},
                "data": {
                    "item_meta": {
                        "item_type": "table",
                        "attributes": [
                            {"key": "item", "value": {"data_type": "table"}},
                        ],
                    },
                    "column_metadata": [
                        {"column_name": "shape", "type": "geometry(LineString,32650)"}
                    ],
                    "geometry_column": "shape",
                    "source_crs": "EPSG:32650",
                },
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
    assert [name for name, _ in executor.calls] == [
        "engine.list",
        "resource.ancestors.get",
        "data.preview",
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


def test_mongodb_current_query_declares_collection_without_resource_discovery(monkeypatch):
    executor = FakeExecutor()
    monkeypatch.setattr(query_agent_api, "ToolExecutor", lambda *_args, **_kwargs: executor)

    async def fake_generate(**kwargs):
        assert kwargs["query_language"] == "mql"
        assert kwargs["resources"] == []
        assert kwargs["current_query"] == '{"find":"Persons","filter":{},"limit":10}'
        return {"query": '{"find":"Persons","filter":{"age":{"$gte":18}},"limit":10}', "warnings": []}

    monkeypatch.setattr(query_agent_api.query_service, "generate", fake_generate)
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

    assert response.status == "success"
    assert response.resources == []
    assert executor.calls == []


def test_mql_primary_collection_requires_one_supported_command():
    assert query_agent_api._mql_primary_collection('{"find":"Persons","filter":{}}') == "Persons"
    assert query_agent_api._mql_primary_collection('{"aggregate":"Orders","pipeline":[]}') == "Orders"
    assert query_agent_api._mql_primary_collection('{"count":"Persons","query":{}}') == "Persons"
    assert query_agent_api._mql_primary_collection('{"distinct":"Persons","key":"name"}') == "Persons"
    assert query_agent_api._mql_primary_collection('db.Persons.find({})') is None
    assert query_agent_api._mql_primary_collection('{"find":"Persons","count":"Persons"}') is None


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
