import asyncio

from addp_common.auth import AuthorizationContext
from api import notebook_agent_api
from api.notebook_agent_api import (
    NotebookEngineCatalogCandidate,
    NotebookGenerationRequest,
    NotebookResourceFact,
)


class FakeLLM:
    def __init__(self, content):
        self.content = content

    async def ainvoke(self, _messages, **_kwargs):
        return type("Response", (), {"content": self.content})()


def test_notebook_missing_intents_phase_expands_only_untried_queries(monkeypatch):
    class ExpansionLLM:
        async def ainvoke(self, messages, **_kwargs):
            prompt = str(messages[-1].content)
            assert "耕地" in prompt
            assert "耕地" in prompt and "cultivated land" in prompt
            return type("Response", (), {"content": '''{
              "resources": [{"role": "耕地", "search_queries": ["farmland", "cropland"]}]
            }'''})()

    monkeypatch.setattr(
        notebook_agent_api.CopilotInferenceService,
        "chat_model",
        lambda *_args, **_kwargs: ExpansionLLM(),
    )
    response = asyncio.run(notebook_agent_api.generate_notebook(
        NotebookGenerationRequest(
            query="计算铁路两边宽度50米所占用的耕地面积",
            missing_intents=[
                {
                    "role": "耕地",
                    "search_queries": ["耕地", "cultivated land"],
                },
            ],
        ),
        user=AuthorizationContext(principal_id=1, tenant_id=3),
        db=None,
    ))
    assert response.status == "intents_ready"
    assert response.intents == [{"role": "耕地", "search_queries": ["farmland", "cropland"]}]


def test_notebook_first_phase_extracts_multilingual_resource_intents(monkeypatch):
    monkeypatch.setattr(
        notebook_agent_api.CopilotInferenceService,
        "chat_model",
        lambda *_args, **_kwargs: FakeLLM('''{
          "resources": [
            {"role": "铁路", "search_queries": ["铁路", "railway"]},
            {"role": "耕地", "search_queries": ["耕地", "farmland", "cropland"]}
          ]
        }'''),
    )
    response = asyncio.run(notebook_agent_api.generate_notebook(
        NotebookGenerationRequest(query="计算铁路两边宽度50米所占用的耕地面积"),
        user=AuthorizationContext(principal_id=1, tenant_id=3),
        db=None,
    ))
    assert response.status == "intents_ready"
    assert response.intents[1]["search_queries"] == ["耕地", "farmland", "cropland"]


def test_notebook_candidate_phase_only_ranks_known_candidate_ids(monkeypatch):
    monkeypatch.setattr(
        notebook_agent_api.CopilotInferenceService,
        "chat_model",
        lambda *_args, **_kwargs: FakeLLM('''{
          "recommendations": [{
            "role": "耕地",
            "ranked_candidate_ids": ["farmland-2", "invented"],
            "recommended_candidate_id": "farmland-2",
            "recommendation_reason": "name matches farmland"
          }]
        }'''),
    )
    candidates = [
        NotebookEngineCatalogCandidate(
            candidate_id="farmland-1", role="耕地", engine_id=8,
            engine_name="PostgreSQL", engine_type="postgresql", name="farmland_history",
            term="table", kind="table", path_names=["public", "farmland_history"],
            path={"version": "catalog.path/v1", "engine_id": 8, "segments": []},
        ),
        NotebookEngineCatalogCandidate(
            candidate_id="farmland-2", role="耕地", engine_id=8,
            engine_name="PostgreSQL", engine_type="postgresql", name="farmland",
            term="table", kind="table", path_names=["public", "farmland"],
            path={"version": "catalog.path/v1", "engine_id": 8, "segments": []},
        ),
    ]
    response = asyncio.run(notebook_agent_api.generate_notebook(
        NotebookGenerationRequest(query="计算耕地面积", candidates=candidates),
        user=AuthorizationContext(principal_id=1, tenant_id=3),
        db=None,
    ))
    assert response.status == "candidates_ready"
    assert [item["candidate_id"] for item in response.candidates] == ["farmland-2", "farmland-1"]
    assert response.candidates[0]["recommended"] is True


def test_notebook_generation_uses_only_develop_verified_resources(monkeypatch):
    monkeypatch.setattr(
        notebook_agent_api.CopilotInferenceService,
        "chat_model",
        lambda *_args, **_kwargs: object(),
    )

    async def fake_generate(**kwargs):
        assert kwargs["kernel"] == "python3"
        assert kwargs["resources"][0]["geometry_column"] == "shape"
        return {
            "code": "from addp_common.notebook import engines\nengine = engines.client(8)",
            "explanation": "candidate",
            "warnings": [],
        }

    monkeypatch.setattr(notebook_agent_api.notebook_service, "generate", fake_generate)
    resource = NotebookResourceFact(
        candidate_id="railway-1", role="铁路", engine_id=8,
        engine_name="PostgreSQL", engine_type="postgresql", name="railway",
        term="table", kind="table", path_names=["public", "railway"],
        path={"version": "catalog.path/v1", "engine_id": 8, "segments": []},
        path_segments=[
            {"term": "server", "kind": "postgresql", "name": ""},
            {"term": "schema", "kind": "schema", "name": "public"},
            {"term": "table", "kind": "table", "name": "railway"},
        ],
        geometry_column="shape", geometry_type="MultiLineString", crs="EPSG:32650",
    )
    response = asyncio.run(notebook_agent_api.generate_notebook(
        NotebookGenerationRequest(query="铁路缓冲区", resources=[resource]),
        user=AuthorizationContext(principal_id=1, tenant_id=3),
        db=None,
    ))
    assert response.status == "success"
    assert "addp_common.notebook" in response.code
