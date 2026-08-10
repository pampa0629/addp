import asyncio

import pytest

from addp_common.resources import ResourceFact
from chains.resource_intent_chain import ResourceIntent
from services.resource_discovery import ResourceDiscoveryResult
from services.resource_resolution import ResourceResolutionPolicy, ResourceResolutionService


class IntentChain:
    def __init__(self, intents):
        self.intents = intents
        self.extract_calls = []
        self.expand_calls = []

    async def extract(self, query, *, scope):
        self.extract_calls.append((query, scope))
        return self.intents

    async def expand_missing(self, query, intents):
        self.expand_calls.append((query, intents))
        return [ResourceIntent(role=item.role, search_queries=[f"{item.role}-expanded"]) for item in intents]


class Discovery:
    def __init__(self, results):
        self.results = list(results)
        self.discover_calls = []
        self.verify_calls = []

    async def discover(self, intents, **kwargs):
        self.discover_calls.append((intents, kwargs))
        return self.results.pop(0)

    async def verify(self, resources, **kwargs):
        self.verify_calls.append((resources, kwargs))
        return resources


def test_query_policy_limits_discovery_to_selected_engine():
    intents = [ResourceIntent(role="铁路", search_queries=["铁路"])]
    discovery = Discovery([ResourceDiscoveryResult(candidates=[], missing_roles=[])])
    service = ResourceResolutionService(discovery=discovery, intent_chain=IntentChain(intents))

    result = asyncio.run(service.discover("分析铁路", ResourceResolutionPolicy.query(8)))

    assert result.missing_roles == []
    assert discovery.discover_calls[0][1]["engine_id"] == 8


def test_workflow_policy_preserves_multiple_input_roles():
    intents = [
        ResourceIntent(role="铁路", search_queries=["铁路"]),
        ResourceIntent(role="耕地", search_queries=["耕地"]),
    ]
    discovery = Discovery([ResourceDiscoveryResult(candidates=[], missing_roles=[])])
    service = ResourceResolutionService(discovery=discovery, intent_chain=IntentChain(intents))

    result = asyncio.run(service.discover("计算铁路与耕地", ResourceResolutionPolicy.workflow()))

    assert [item.role for item in result.intents] == ["铁路", "耕地"]


def test_transfer_multiple_intents_are_returned_for_clarification_without_search():
    intents = [
        ResourceIntent(role="源", search_queries=["source"]),
        ResourceIntent(role="目标", search_queries=["target"]),
    ]
    discovery = Discovery([])
    service = ResourceResolutionService(discovery=discovery, intent_chain=IntentChain(intents))

    result = asyncio.run(service.discover("从 pg 到 mysql", ResourceResolutionPolicy.transfer()))

    assert len(result.intents) == 2
    assert discovery.discover_calls == []


def test_zero_recall_retries_only_missing_roles_once():
    intents = [
        ResourceIntent(role="铁路", search_queries=["铁路"]),
        ResourceIntent(role="耕地", search_queries=["耕地"]),
    ]
    discovery = Discovery([
        ResourceDiscoveryResult(candidates=[{"role": "铁路", "locator": "railway"}], missing_roles=["耕地"]),
        ResourceDiscoveryResult(candidates=[{"role": "耕地", "locator": "farmland"}], missing_roles=[]),
    ])
    chain = IntentChain(intents)
    service = ResourceResolutionService(discovery=discovery, intent_chain=chain)

    result = asyncio.run(service.discover("计算铁路与耕地", ResourceResolutionPolicy.workflow()))

    assert [item["role"] for item in result.candidates] == ["铁路", "耕地"]
    assert len(discovery.discover_calls) == 2
    assert [item.role for item in discovery.discover_calls[1][0]] == ["耕地"]
    assert len(chain.expand_calls) == 1


def test_notebook_policy_does_not_use_tenant_tool_search():
    service = ResourceResolutionService(intent_chain=IntentChain([]))

    with pytest.raises(ValueError, match="不支持 Tool 资源搜索"):
        asyncio.run(service.discover("读取当前 Session", ResourceResolutionPolicy.notebook()))


def test_verify_enforces_engine_and_input_count():
    discovery = Discovery([])
    service = ResourceResolutionService(discovery=discovery, intent_chain=IntentChain([]))
    resource = ResourceFact(role="roads", locator="addp://engine/8/path/public/roads?type=table", engine_id=8)

    asyncio.run(service.verify([resource], ResourceResolutionPolicy.query(8)))
    assert discovery.verify_calls[0][1]["engine_id"] == 8

    with pytest.raises(ValueError, match="输入资源数量"):
        asyncio.run(service.verify([], ResourceResolutionPolicy.query(8)))
