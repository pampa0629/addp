import asyncio

from addp_common.client.manager import ManagerClient
from addp_common.client.meta import MetaClient


async def _assert_manager_search_unwraps_canonical_data(monkeypatch):
    client = ManagerClient(base_url="http://manager", internal_api_key="test")

    async def fake_get(path, params=None):
        assert path == "/api/v1/manager/search"
        assert params["tenant_id"] == 3
        return {"data": {"total": 1, "results": [{"locator": "addp://fact"}]}}

    monkeypatch.setattr(client, "get", fake_get)
    try:
        result = await client.search(q="roads", tenant_id=3, page=1, page_size=5)
    finally:
        await client.close()

    assert result == {"total": 1, "results": [{"locator": "addp://fact"}]}


def test_manager_search_unwraps_canonical_data(monkeypatch):
    asyncio.run(_assert_manager_search_unwraps_canonical_data(monkeypatch))


async def _assert_meta_client_reads_resource_ancestors(monkeypatch):
    client = MetaClient(base_url="http://meta", internal_api_key="test")

    async def fake_get(path, params=None):
        assert path == "/api/v1/meta/resource-tree/8/ancestors"
        assert params == {"locator": "addp://fact"}
        return {"target_locator": "addp://fact", "ancestors": []}

    monkeypatch.setattr(client, "get", fake_get)
    try:
        result = await client.get_resource_tree_ancestors(8, "addp://fact")
    finally:
        await client.close()

    assert result["target_locator"] == "addp://fact"


def test_meta_client_reads_resource_ancestors(monkeypatch):
    asyncio.run(_assert_meta_client_reads_resource_ancestors(monkeypatch))
