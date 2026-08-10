import asyncio

from addp_common.tools import ToolExecutionError
from chains.resource_intent_chain import ResourceIntent
from chains.resource_recommendation_chain import ResourceRecommendation
from addp_common.resources import ResourceFact
from services.resource_discovery import ResourceDiscovery


class FakeToolExecutor:
    def __init__(self, *, reject_preview_locator: str = "", mismatched_ancestor_locator: str = ""):
        self.calls = []
        self.reject_preview_locator = reject_preview_locator
        self.mismatched_ancestor_locator = mismatched_ancestor_locator

    async def call(self, name, arguments, **audit):
        self.calls.append((name, arguments, audit))
        if name == "data.search":
            resource_name = "farmland" if arguments["query"] == "耕地" else "railway"
            item_id = 61 if resource_name == "farmland" else 60
            return {
                "results": [
                    {
                        "name": resource_name,
                        "locator": f"addp://engine/8/path/public/{resource_name}?type=table&item_id={item_id}",
                        "engine_id": 8,
                        "engine_name": "spatial",
                        "asset_type": "table",
                        "score": 0.9,
                    },
                    {
                        "name": "document-about-railway",
                        "locator": "addp://engine/8/path/docs/railway.pdf?type=file&item_id=61",
                        "engine_id": 8,
                        "asset_type": "document",
                    },
                ]
            }
        if name == "resource.ancestors.get":
            return {
                "target_locator": self.mismatched_ancestor_locator or arguments["locator"],
                "ancestors": [{"label": "public", "type": "schema", "locator": "schema-locator"}],
            }
        if name == "data.preview":
            if arguments["locator"] == self.reject_preview_locator:
                raise ToolExecutionError("owner_api_error", "preview unavailable")
            return {
                "preview_type": "table",
                "metadata": {"locator": arguments["locator"]},
                "data": {
                    "column_metadata": [{"column_name": "shape", "type": "geometry(LineString,32650)", "nullable": True}],
                    "geometry_column": "shape",
                    "source_crs": "EPSG:32650",
                },
            }
        raise AssertionError(f"unexpected tool {name}")


def test_discovery_reuses_owner_tools_and_returns_verified_resource_facts():
    executor = FakeToolExecutor()
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)

    result = asyncio.run(discovery.discover([
        ResourceIntent(role="铁路", search_queries=["铁路"]),
        ResourceIntent(role="耕地", search_queries=["耕地"]),
    ]))

    assert result.missing_roles == []
    assert result.candidates[0] == {
        "role": "铁路",
        "name": "railway",
        "locator": "addp://engine/8/path/public/railway?type=table&item_id=60",
        "engine_id": 8,
        "engine_name": "spatial",
        "asset_type": "table",
        "full_name": None,
        "path": None,
        "score": 0.9,
        "ancestors": [{"label": "public", "type": "schema", "locator": "schema-locator"}],
        "data_type": "table",
        "geometry_column": "shape",
        "geometry_type": "LineString",
        "crs": "EPSG:32650",
        "fields": [{"name": "shape", "type": "geometry(LineString,32650)", "nullable": True}],
        "recommended": False,
        "recommendation_reason": None,
    }
    assert result.candidates[1]["role"] == "耕地"
    assert result.candidates[1]["name"] == "farmland"
    assert [name for name, _, _ in executor.calls] == [
        "data.search",
        "resource.ancestors.get",
        "data.preview",
        "data.search",
        "resource.ancestors.get",
        "data.preview",
    ]


def test_discovery_passes_current_engine_to_owner_search():
    executor = FakeToolExecutor()
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)

    asyncio.run(discovery.discover([
        ResourceIntent(role="铁路", search_queries=["railway"]),
    ], engine_id=8))

    assert executor.calls[0][0] == "data.search"
    assert executor.calls[0][1]["engine_id"] == 8


def test_verify_rejects_resource_from_other_query_engine():
    executor = FakeToolExecutor()
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)

    try:
        asyncio.run(discovery.verify([
            ResourceFact(
                role="铁路",
                locator="addp://engine/9/path/public/railway?type=table&item_id=60",
                engine_id=9,
            )
        ], engine_id=8))
    except ToolExecutionError as error:
        assert error.code == "invalid_arguments"
    else:
        raise AssertionError("resource from another query engine must be rejected")

    assert executor.calls == []


def test_discovery_propagates_owner_error_when_no_candidate_can_be_verified():
    locator = "addp://engine/8/path/public/railway?type=table&item_id=60"
    executor = FakeToolExecutor(reject_preview_locator=locator)
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)

    try:
        asyncio.run(discovery.discover([
            ResourceIntent(role="铁路", search_queries=["铁路"]),
        ]))
    except ToolExecutionError as error:
        assert error.code == "owner_api_error"
    else:
        raise AssertionError("owner preview failure must not be reported as no search result")


def test_discovery_drops_candidate_when_ancestors_do_not_confirm_search_locator():
    executor = FakeToolExecutor(mismatched_ancestor_locator="addp://engine/8/path/public/other?type=table&item_id=70")
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)

    result = asyncio.run(discovery.discover([
        ResourceIntent(role="铁路", search_queries=["铁路"]),
    ]))

    assert result.candidates == []
    assert result.missing_roles == ["铁路"]


def test_discovery_merges_synonym_searches_by_role_and_locator():
    executor = FakeToolExecutor()
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)

    result = asyncio.run(discovery.discover([
        ResourceIntent(role="铁路", search_queries=["铁路", "railway"]),
    ]))

    assert len(result.candidates) == 1
    assert [name for name, _, _ in executor.calls] == [
        "data.search",
        "resource.ancestors.get",
        "data.preview",
        "data.search",
    ]


class SameNameAcrossEnginesExecutor:
    async def call(self, name, arguments, **_audit):
        if name == "data.search":
            return {
                "results": [
                    {
                        "name": "farmland",
                        "full_name": "public.farmland",
                        "locator": "addp://engine/8/path/public/farmland?type=table&item_id=61",
                        "engine_id": 8,
                        "engine_name": "spatial-a",
                        "asset_type": "table",
                        "score": 0.95,
                    },
                    {
                        "name": "farmland",
                        "full_name": "public.farmland",
                        "locator": "addp://engine/9/path/public/farmland?type=table&item_id=71",
                        "engine_id": 9,
                        "engine_name": "spatial-b",
                        "asset_type": "table",
                        "score": 0.94,
                    },
                ]
            }
        if name == "resource.ancestors.get":
            return {"target_locator": arguments["locator"], "ancestors": []}
        if name == "data.preview":
            return {
                "preview_type": "table",
                "metadata": {"locator": arguments["locator"]},
                "data": {
                    "column_metadata": [
                        {"column_name": "shape", "type": "geometry(Polygon,32650)", "nullable": True}
                    ],
                    "geometry_column": "shape",
                    "source_crs": "EPSG:32650",
                },
            }
        raise AssertionError(f"unexpected tool {name}")


class PreferSecondEngine:
    async def recommend(self, candidates):
        assert len(candidates) == 2
        return {
            "耕地": ResourceRecommendation(
                role="耕地",
                ranked_locators=[candidates[1]["locator"], candidates[0]["locator"]],
                recommended_locator=candidates[1]["locator"],
                recommendation_reason="字段与分析目标更匹配",
            )
        }


def test_discovery_keeps_same_name_candidates_across_engines_after_recommendation():
    discovery = ResourceDiscovery(
        "http://gateway",
        "addp_at_user",
        executor=SameNameAcrossEnginesExecutor(),
        recommender=PreferSecondEngine(),
    )

    result = asyncio.run(discovery.discover([
        ResourceIntent(role="耕地", search_queries=["farmland"]),
    ]))

    assert result.missing_roles == []
    assert [(candidate["engine_id"], candidate["recommended"]) for candidate in result.candidates] == [
        (9, True),
        (8, False),
    ]
    assert result.candidates[0]["recommendation_reason"] == "字段与分析目标更匹配"
    assert result.candidates[1]["recommendation_reason"] is None
