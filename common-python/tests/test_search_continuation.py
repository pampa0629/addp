import asyncio

import httpx
import pytest

from addp_common.client.manager import ManagerClient
from addp_common.tools.executor import ToolExecutor, ToolExecutionError


def test_search_tool_consumes_empty_batches_with_one_delegation(monkeypatch):
    calls = []
    delegations = []
    batches = [
        {"results": [], "page_size": 2, "exhausted": False, "next_cursor": "first"},
        {"results": [{"document_id": "a"}], "page_size": 2, "exhausted": False, "next_cursor": "second"},
        {"results": [{"document_id": "b"}], "page_size": 2, "exhausted": False, "next_cursor": "third"},
    ]

    async def transport(owner, method, path, **kwargs):
        assert path == "/api/v1/manager/search"
        assert owner._client.headers["Authorization"] == "Bearer addp_dat_same_call"
        params = kwargs["params"]
        assert params["q"] == "roads" and params["engine_id"] == 8
        assert "page" not in params and "tenant_id" not in params
        calls.append(params)
        return httpx.Response(200, request=httpx.Request(method, "http://fixture" + path), json={"data": batches[len(calls)-1]})

    async def issue(*args, **kwargs):
        delegations.append(kwargs)
        return "addp_dat_same_call"

    async def run():
        executor = ToolExecutor("http://fixture", "user-token")
        monkeypatch.setattr(executor, "_issue_delegated_token", issue)
        monkeypatch.setattr("addp_common.client.base.BaseClient._request", transport)
        result = await executor.call("data.search", {"query": "roads", "engine_id": 8, "limit": 2}, agent_run_id="run", tool_call_id="call")
        assert result == {"results": [{"document_id": "a"}, {"document_id": "b"}], "page_size": 2, "exhausted": False}
        assert "next_cursor" not in result

    asyncio.run(run())
    assert len(delegations) == 1
    assert [c.get("cursor") for c in calls] == [None, "first", "second"]


@pytest.mark.parametrize("data", [
    {"results": [], "total": 0, "page": 1},
    {"results": [], "page_size": 2, "exhausted": False},
    {"results": [], "page_size": 2, "exhausted": False, "next_cursor": "same"},
    {"results": [], "page_size": 2, "exhausted": True, "next_cursor": "next"},
])
def test_search_client_rejects_nonprogressing_or_old_response(monkeypatch, data):
    async def run():
        async with ManagerClient(base_url="http://fixture") as client:
            async def get(*args, **kwargs):
                return {"data": data}
            monkeypatch.setattr(client, "get", get)
            with pytest.raises(ValueError):
                await client.search("roads", page_size=2, cursor="same")
    asyncio.run(run())


def test_search_context_change_terminates_tool_without_new_query(monkeypatch):
    calls = []
    async def transport(owner, method, path, **kwargs):
        calls.append(kwargs["params"])
        request = httpx.Request(method, "http://fixture" + path)
        if len(calls) == 1:
            return httpx.Response(200, request=request, json={"data": {"results": [], "page_size": 2, "exhausted": False, "next_cursor": "next"}})
        response = httpx.Response(409, request=request, json={"error": "检索依据已变化", "error_code": "search_context_changed"})
        raise httpx.HTTPStatusError("conflict", request=request, response=response)
    async def run():
        executor = ToolExecutor("http://fixture", "user-token")
        async def issue(*args, **kwargs): return "addp_dat_same_call"
        monkeypatch.setattr(executor, "_issue_delegated_token", issue)
        monkeypatch.setattr("addp_common.client.base.BaseClient._request", transport)
        with pytest.raises(ToolExecutionError) as error:
            await executor.call("data.search", {"query": "roads", "limit": 2}, agent_run_id="run", tool_call_id="call")
        assert error.value.code == "search_context_changed"
    asyncio.run(run())
    assert len(calls) == 2 and calls[1]["cursor"] == "next"
