import asyncio

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
