import asyncio

from addp_common.tools import ToolExecutionError
from chains.resource_intent_chain import ResourceIntent
from chains.resource_recommendation_chain import ResourceRecommendation
from addp_common.resources import ResourceFact
from services.resource_discovery import ResourceDiscovery

QUERY_DATA_TYPES = frozenset({"table", "graph"})


def resource_facts(locator, *, data_type="table", source_engine_type="postgresql", fields=None, **extra):
    engine_id = extra.pop("engine_id", int(locator.split("/engine/", 1)[1].split("/", 1)[0]))
    return {
        "locator": locator,
        "engine_id": engine_id,
        "data_type": data_type,
        "source_engine_type": source_engine_type,
        "full_name": extra.pop("full_name", None),
        "query_names": extra.pop("query_names", {}),
        "schema_coverage": extra.pop("schema_coverage", "complete"),
        "fields": fields or [],
        **extra,
    }


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
        if name == "resource.facts.get":
            if arguments["locator"] == self.reject_preview_locator:
                raise ToolExecutionError("owner_api_error", "resource facts unavailable")
            is_document = arguments["locator"].endswith("railway.pdf?type=file&item_id=61")
            return resource_facts(
                arguments["locator"],
                data_type="document" if is_document else "table",
                fields=[{"name": "shape", "type": "geometry(LineString,32650)", "nullable": True}],
                geometry_column="shape",
                geometry_type="LineString",
                crs="EPSG:32650",
            )
        raise AssertionError(f"unexpected tool {name}")


def test_discovery_reuses_owner_tools_and_returns_verified_resource_facts():
    executor = FakeToolExecutor()
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)

    result = asyncio.run(discovery.discover([
        ResourceIntent(role="铁路", search_queries=["铁路"]),
        ResourceIntent(role="耕地", search_queries=["耕地"]),
    ], allowed_data_types=QUERY_DATA_TYPES))

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
        "source_engine_type": "postgresql",
        "query_names": {},
        "schema_coverage": "complete",
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
        "resource.facts.get",
        "resource.ancestors.get",
        "resource.facts.get",
        "data.search",
        "resource.ancestors.get",
        "resource.facts.get",
    ]


def test_discovery_passes_current_engine_to_owner_search():
    executor = FakeToolExecutor()
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)

    asyncio.run(discovery.discover([
        ResourceIntent(role="铁路", search_queries=["railway"]),
    ], engine_id=8, allowed_data_types=QUERY_DATA_TYPES))

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


def test_verify_uses_owner_data_type_for_mongodb_collection():
    executor = FakeToolExecutor()
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)

    resources = asyncio.run(discovery.verify([
        ResourceFact(
            role="人员",
            locator="addp://engine/11/path/Outdoor/Persons?type=collection&item_id=51657",
            engine_id=11,
        )
    ], engine_id=11, allowed_data_types=QUERY_DATA_TYPES))

    assert resources[0].data_type == "table"


class ContainerPreviewExecutor:
    async def call(self, name, arguments, **_audit):
        if name == "resource.facts.get":
            raise ToolExecutionError("invalid_arguments", "请选择具体数据项")
        raise AssertionError(f"unexpected tool {name}")


def test_verify_reports_container_selection_as_invalid_arguments():
    discovery = ResourceDiscovery(
        "http://gateway",
        "addp_at_user",
        executor=ContainerPreviewExecutor(),
    )

    try:
        asyncio.run(discovery.verify([
            ResourceFact(
                role="Outdoor",
                locator="addp://engine/11/path/Outdoor?type=database&node_id=276",
                engine_id=11,
            )
        ], engine_id=11, allowed_data_types=QUERY_DATA_TYPES))
    except ToolExecutionError as error:
        assert error.code == "invalid_arguments"
    else:
        raise AssertionError("container resource must be reported as invalid arguments")


class MongoDiscoveryExecutor:
    async def call(self, name, arguments, **_audit):
        locator = "addp://engine/11/path/Outdoor/Persons?type=collection&item_id=51657"
        if name == "data.search":
            return {
                "results": [{
                    "name": "Persons",
                    "locator": locator,
                    "engine_id": 11,
                    "engine_name": "MongoDB",
                    "asset_type": "collection",
                }]
            }
        if name == "resource.ancestors.get":
            return {"target_locator": locator, "ancestors": []}
        if name == "resource.facts.get":
            return resource_facts(locator, source_engine_type="mongodb", fields=[{"name": "_id", "type": "string"}], query_names={"mql": "Persons"}, schema_coverage="sampled")
        raise AssertionError(f"unexpected tool {name}")


def test_discovery_accepts_native_collection_and_filters_by_owner_data_type():
    discovery = ResourceDiscovery(
        "http://gateway",
        "addp_at_user",
        executor=MongoDiscoveryExecutor(),
    )

    result = asyncio.run(discovery.discover(
        [ResourceIntent(role="人员", search_queries=["Persons"])],
        engine_id=11,
        allowed_data_types=QUERY_DATA_TYPES,
    ))

    assert result.missing_roles == []
    assert result.candidates[0]["asset_type"] == "collection"
    assert result.candidates[0]["data_type"] == "table"


class ScopedMongoDiscoveryExecutor:
    def __init__(self):
        self.calls = []

    async def call(self, name, arguments, **_audit):
        self.calls.append((name, arguments))
        scope = "addp://engine/11/path/Outdoor?type=database&node_id=276"
        locator = "addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=51658"
        if name == "resource.children.list":
            return {
                "locator": scope,
                "label": "Outdoor",
                "type": "database",
                "children": [{"locator": locator, "label": "Outdoors", "type": "collection"}],
            }
        if name == "resource.facts.get":
            return resource_facts(locator, source_engine_type="mongodb", fields=[{"name": "members.userInfo.nickName", "type": "string"}], query_names={"mql": "Outdoors"}, schema_coverage="sampled")
        raise AssertionError(f"unexpected tool {name}")


def test_scoped_discovery_lists_direct_children_and_reads_resource_facts():
    executor = ScopedMongoDiscoveryExecutor()
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)
    scope = "addp://engine/11/path/Outdoor?type=database&node_id=276"

    result = asyncio.run(discovery.discover_scoped(
        [ResourceIntent(role="活动参与", search_queries=["activities"])],
        engine_id=11,
        scope_locator=scope,
        allowed_data_types=QUERY_DATA_TYPES,
    ))

    assert result.missing_roles == []
    assert result.candidates[0]["name"] == "Outdoors"
    assert result.candidates[0]["ancestors"] == [{
        "label": "Outdoor",
        "type": "database",
        "locator": scope,
    }]
    assert result.candidates[0]["fields"][0]["name"] == "members.userInfo.nickName"
    assert [name for name, _ in executor.calls] == ["resource.children.list", "resource.facts.get"]


def test_discovery_propagates_owner_error_when_no_candidate_can_be_verified():
    locator = "addp://engine/8/path/public/railway?type=table&item_id=60"
    executor = FakeToolExecutor(reject_preview_locator=locator)
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)

    try:
        asyncio.run(discovery.discover([
            ResourceIntent(role="铁路", search_queries=["铁路"]),
        ], allowed_data_types=QUERY_DATA_TYPES))
    except ToolExecutionError as error:
        assert error.code == "owner_api_error"
    else:
        raise AssertionError("owner resource facts failure must not be reported as no search result")


def test_discovery_drops_candidate_when_ancestors_do_not_confirm_search_locator():
    executor = FakeToolExecutor(mismatched_ancestor_locator="addp://engine/8/path/public/other?type=table&item_id=70")
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)

    result = asyncio.run(discovery.discover([
        ResourceIntent(role="铁路", search_queries=["铁路"]),
    ], allowed_data_types=QUERY_DATA_TYPES))

    assert result.candidates == []
    assert result.missing_roles == ["铁路"]


def test_discovery_merges_synonym_searches_by_role_and_locator():
    executor = FakeToolExecutor()
    discovery = ResourceDiscovery("http://gateway", "addp_at_user", executor=executor)

    result = asyncio.run(discovery.discover([
        ResourceIntent(role="铁路", search_queries=["铁路", "railway"]),
    ], allowed_data_types=QUERY_DATA_TYPES))

    assert len(result.candidates) == 1
    assert [name for name, _, _ in executor.calls] == [
        "data.search",
        "resource.ancestors.get",
        "resource.facts.get",
        "resource.ancestors.get",
        "resource.facts.get",
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
        if name == "resource.facts.get":
            return resource_facts(
                arguments["locator"],
                fields=[{"name": "shape", "type": "geometry(Polygon,32650)", "nullable": True}],
                geometry_column="shape",
                geometry_type="Polygon",
                crs="EPSG:32650",
            )
        raise AssertionError(f"unexpected tool {name}")


class PreferSecondEngine:
    async def recommend(self, candidates, **_context):
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
