import asyncio
import importlib.util
from pathlib import Path

import httpx


BACKEND_DIR = Path(__file__).resolve().parents[1]


def load_module(name: str, relative_path: str):
    spec = importlib.util.spec_from_file_location(name, BACKEND_DIR / relative_path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


meta_tools = load_module("metadata_search_tool_module", "tools/meta_tools.py")
MetadataSearchTool = meta_tools.MetadataSearchTool


class FakeManagerClient:
    seen_tenant_id = None

    def __init__(self, **_kwargs):
        pass

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args):
        return None

    async def search(self, q: str, tenant_id: int, page: int, page_size: int):
        self.__class__.seen_tenant_id = tenant_id
        return {
            "results": [{
                "name": "roads",
                "asset_type": "table",
                "engine_id": 8,
                "engine_name": "Business PostgreSQL",
                "engine_type": "postgresql",
                "schema": "public",
                "score": 0.95,
                "locator": "addp://engine/8/path/public/roads?type=table&item_id=91",
            }]
        }


class FakeMetaClient:
    def __init__(self, **_kwargs):
        pass

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args):
        return None

    async def get_resource_tree_ancestors(self, engine_id: int, locator: str):
        assert engine_id == 8
        assert locator.endswith("item_id=91")
        return {
            "target_locator": locator,
            "ancestors": [
                {"locator": "addp://engine/8/path/?type=server&node_id=1"},
                {"locator": "addp://engine/8/path/public?type=schema&node_id=7"},
                {"locator": locator},
            ],
        }


async def _assert_metadata_search_keeps_verified_fact_locators(monkeypatch):
    monkeypatch.setattr(meta_tools, "ManagerClient", FakeManagerClient)
    monkeypatch.setattr(meta_tools, "MetaClient", FakeMetaClient)

    result = await MetadataSearchTool()._arun(
        query="roads",
        metadata_type="table",
        tenant_id=3,
        limit=5,
    )

    assert FakeManagerClient.seen_tenant_id == 3
    assert result == [{
        "name": "roads",
        "type": "table",
        "engine_id": 8,
        "engine_name": "Business PostgreSQL",
        "engine_type": "postgresql",
        "schema": "public",
        "bucket": None,
        "path": None,
        "score": 0.95,
        "metadata": {},
        "locator": "addp://engine/8/path/public/roads?type=table&item_id=91",
        "target_parent_locator": "addp://engine/8/path/public?type=schema&node_id=7",
    }]


def test_metadata_search_keeps_verified_fact_locators(monkeypatch):
    asyncio.run(_assert_metadata_search_keeps_verified_fact_locators(monkeypatch))


class StaleMetaClient(FakeMetaClient):
    async def get_resource_tree_ancestors(self, engine_id: int, locator: str):
        request = httpx.Request("GET", f"http://meta/resource-tree/{engine_id}/ancestors")
        response = httpx.Response(404, request=request)
        raise httpx.HTTPStatusError("stale locator", request=request, response=response)


async def _assert_metadata_search_skips_stale_locator(monkeypatch):
    monkeypatch.setattr(meta_tools, "ManagerClient", FakeManagerClient)
    monkeypatch.setattr(meta_tools, "MetaClient", StaleMetaClient)

    result = await MetadataSearchTool()._arun(query="roads", tenant_id=3, limit=5)

    assert result == []


def test_metadata_search_skips_stale_locator(monkeypatch):
    asyncio.run(_assert_metadata_search_skips_stale_locator(monkeypatch))
