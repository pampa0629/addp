import asyncio
import copy
import json

import httpx
import pytest
from jsonschema import Draft202012Validator

from addp_common.client import SystemClient
from addp_common.tools import ToolExecutionError, ToolExecutor, get_tool


def engine_row(**overrides):
    return {
        "id": 11, "name": "MongoDB fixture", "engine_type": "mongodb",
        "lifecycle_state": "active", "connection_status": "unknown",
        "connection_info": {"password": "private", "host": "private-host"},
        "capabilities": {"schema_version": "engine.capabilities/v1", "large": "x" * 200000},
        "capabilities_view": {"large": "y" * 200000},
        "tenant_id": 7, "created_by": 9, **overrides,
    }


def expected_summary(row):
    return {key: row[key] for key in (
        "id", "name", "engine_type", "lifecycle_state", "connection_status",
    )}


def call_engine_tool(rows, arguments=None):
    async def run():
        requests = []

        async def transport(request):
            requests.append(request)
            assert request.method == "GET"
            assert request.url.path == "/api/v1/system/engines"
            assert not request.url.params
            assert request.headers["Authorization"] == "Bearer addp_dat_fixture"
            return httpx.Response(200, json=rows)

        def client_factory(client_type, token):
            assert client_type is SystemClient
            client = client_type("http://gateway", user_token=token)
            client._client._transport = httpx.MockTransport(transport)
            return client

        executor = ToolExecutor("http://gateway", "private-user-token")
        executor._client = client_factory

        async def issue(definition, **binding):
            assert definition.auth.audience == "system"
            assert definition.auth.required_scopes == ["engine.list"]
            assert binding == {"agent_run_id": "run", "tool_call_id": "call"}
            return "addp_dat_fixture"

        executor._issue_delegated_token = issue
        try:
            return await executor.call("engine.list", arguments, agent_run_id="run", tool_call_id="call")
        finally:
            assert len(requests) == 1  # No retries, detail calls, or bypass API.

    return asyncio.run(run())


@pytest.mark.parametrize("arguments", [None, {"capability": "all"}])
def test_all_engines_project_before_size_check_without_losing_members(arguments):
    rows = [engine_row(), engine_row(id=12, name="PG fixture", engine_type="postgresql", connection_status="offline")]
    original = copy.deepcopy(rows)
    assert len(json.dumps(rows).encode()) > get_tool("engine.list").limits["max_bytes"]
    result = call_engine_tool(rows, arguments)
    assert result == [expected_summary(row) for row in rows]
    assert rows == original
    assert "private" not in json.dumps(result)


def test_workflow_uses_current_lifecycle_and_same_projection():
    capabilities = {"schema_version": "engine.capabilities/v1", "compute": {"workflow": {"supported": True}}}
    rows = [
        engine_row(id=1, engine_type="custom_workflow", capabilities=capabilities),
        engine_row(id=2, lifecycle_state="disabled", capabilities=capabilities, is_active=True),
        engine_row(id=3, capabilities={"compute": {"workflow": {"supported": True}}}),
        engine_row(id=4, capabilities={"schema_version": "engine.capabilities/v1", "compute": {"workflow": {"supported": False}}}),
    ]
    assert call_engine_tool(rows, {"capability": "workflow"}) == [expected_summary(rows[0])]


@pytest.mark.parametrize("rows", [[None], ["invalid"], [{"id": 11}], [engine_row(id="11")], [engine_row(connection_status="invented")]])
def test_invalid_owner_engine_facts_fail_closed(rows):
    with pytest.raises(ToolExecutionError) as failure:
        call_engine_tool(rows)
    assert failure.value.code == "invalid_owner_response"


def test_engine_summary_budget_is_not_silently_truncated():
    with pytest.raises(ToolExecutionError) as failure:
        call_engine_tool([engine_row(name="x" * 140000)])
    assert failure.value.code == "result_too_large"


def test_empty_engine_list_is_not_an_error():
    assert call_engine_tool([]) == []


def test_engine_tool_contract_rejects_management_fields_and_old_active_flag():
    definition = get_tool("engine.list")
    assert definition.version == "2.0.0"
    validator = Draft202012Validator(definition.output_schema)
    summary = expected_summary(engine_row())
    assert not list(validator.iter_errors([summary]))
    for key, value in [("connection_info", {}), ("capabilities", {}), ("is_active", True)]:
        assert list(validator.iter_errors([{**summary, key: value}]))
