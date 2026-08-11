import asyncio

from addp_common.client.manager import ManagerClient
from addp_common.client.meta import MetaClient


async def _assert_manager_search_unwraps_canonical_data(monkeypatch):
    client = ManagerClient(base_url="http://manager")

    async def fake_get(path, params=None):
        assert path == "/api/v1/manager/search"
        assert params["tenant_id"] == 3
        assert params["engine_id"] == 8
        return {"data": {"total": 1, "results": [{"locator": "addp://fact"}]}}

    monkeypatch.setattr(client, "get", fake_get)
    try:
        result = await client.search(q="roads", engine_id=8, tenant_id=3, page=1, page_size=5)
    finally:
        await client.close()

    assert result == {"total": 1, "results": [{"locator": "addp://fact"}]}


def test_manager_search_unwraps_canonical_data(monkeypatch):
    asyncio.run(_assert_manager_search_unwraps_canonical_data(monkeypatch))


async def _assert_meta_client_reads_resource_ancestors(monkeypatch):
    client = MetaClient(base_url="http://meta")

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


async def _assert_meta_client_reads_resource_children(monkeypatch):
    client = MetaClient(base_url="http://meta")

    async def fake_get(path, params=None):
        assert path == "/api/v1/meta/resource-tree/11/node"
        assert params == {"locator": "addp://engine/11/path/Outdoor?type=database&node_id=276"}
        return {"locator": params["locator"], "children": []}

    monkeypatch.setattr(client, "get", fake_get)
    try:
        result = await client.get_resource_tree_node(
            11,
            "addp://engine/11/path/Outdoor?type=database&node_id=276",
        )
    finally:
        await client.close()

    assert result["children"] == []


def test_meta_client_reads_resource_children(monkeypatch):
    asyncio.run(_assert_meta_client_reads_resource_children(monkeypatch))
