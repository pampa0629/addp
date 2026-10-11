"""Formal Tool/SDK boundary; no Agent or business execution is simulated here."""
import asyncio
import copy

import httpx
import pytest

from addp_common.client import TransferClient
from addp_common.tools import ToolExecutionError, ToolExecutor, get_tool
from test_transfer_config_check_client import arguments


def report(status="unknown"):
    return {
        "schema_version": "addp.transfer_task_config_check/v1",
        "tenant_id": 3, "principal_id": "user-fixture",
        "config_hash": "a" * 64, "binding_hash": "b" * 64,
        "observed_at": "2026-10-11T00:00:00Z", "status": status,
        "checked": ["request_shape"], "not_checked": ["business_rows"],
        "runtime_checks_required": ["per_value_encoding"],
        "facts": [{"role": "source", "engine_id": 1, "engine_version": 2,
                   "engine_type": "mongodb", "engine_facts_hash": "c" * 64,
                   "snapshot_hash": "d" * 64, "schema_coverage": "sampled",
                   "native_access": "supported", "meta_item_id": 2,
                   "scanned_at": "2026-10-10T00:00:00Z"}],
        "diagnostics": [{"status": status, "code": "source_schema_not_complete",
                         "check": "schema", "config_path": "/config/source"}],
    }


async def invoke(response, params=None):
    executor = ToolExecutor("http://gateway", "original-user-secret")
    requests = []
    async def delegate(definition, *, agent_run_id, tool_call_id):
        assert definition.name == "transfer.task.config.check"
        assert definition.risk == "read"
        assert definition.auth.audience == "transfer"
        assert definition.auth.required_scopes == [definition.name]
        assert definition.auth.required_permissions == ["transfer.task.create", "meta.catalog.read", "system.engine.read"]
        assert (agent_run_id, tool_call_id) == ("run", "check")
        return "addp_dat_precheck"
    executor._issue_delegated_token = delegate
    def owner(request):
        requests.append(request)
        assert request.url.path == "/api/v1/transfer/task-config-checks"
        assert request.method == "POST" and not request.url.query
        assert request.headers["Authorization"] == "Bearer addp_dat_precheck"
        return response
    def client_factory(client_type, token):
        assert client_type is TransferClient and token == "addp_dat_precheck"
        client = client_type("http://gateway", user_token=token)
        client._client._transport = httpx.MockTransport(owner)
        return client
    executor._client = client_factory
    try:
        return await executor.call("transfer.task.config.check", params or arguments(), agent_run_id="run", tool_call_id="check")
    finally:
        assert len(requests) <= 1  # Never create, run, scan, or retry.


@pytest.mark.parametrize("status", ["supported", "unsupported", "unknown"])
def test_tool_preserves_all_report_states_and_bindings(status):
    expected = report(status)
    original = copy.deepcopy(expected)
    assert asyncio.run(invoke(httpx.Response(200, json=expected))) == expected == original


@pytest.mark.parametrize("field,value", [
    ("status", "passed"), ("schema_version", "legacy"), ("tenant_id", "3"),
    ("config_hash", "short"), ("binding_hash", "A" * 64), ("facts", None),
    ("not_checked", "none"), ("credentials", {"password": "secret"}),
])
def test_tool_rejects_invalid_or_excess_owner_report(field, value):
    invalid = report()
    invalid[field] = value
    with pytest.raises(ToolExecutionError) as rejected:
        asyncio.run(invoke(httpx.Response(200, json=invalid)))
    assert rejected.value.code == "invalid_owner_response"


def test_tool_requires_full_evidence_not_just_http_success():
    with pytest.raises(ToolExecutionError) as rejected:
        asyncio.run(invoke(httpx.Response(200, json={"status": "supported"})))
    assert rejected.value.code == "invalid_owner_response"


@pytest.mark.parametrize("status", [400, 401, 403, 502])
def test_owner_errors_do_not_become_unknown_or_leak_body(status):
    with pytest.raises(ToolExecutionError) as rejected:
        asyncio.run(invoke(httpx.Response(status, json={"error": "private database details"})))
    assert rejected.value.code == "owner_api_error"
    assert rejected.value.details == {"status": status}
    assert "private" not in rejected.value.message


def test_report_budget_is_enforced_without_truncation():
    excessive = report()
    excessive["diagnostics"] = [{"status": "unknown", "code": "fixture", "check": "schema",
                                  "config_path": "/config", "target_field": "x" * 4096}] * 40
    with pytest.raises(ToolExecutionError) as rejected:
        asyncio.run(invoke(httpx.Response(200, json=excessive)))
    assert rejected.value.code == "result_too_large"


def test_input_schema_and_create_contract_are_identical():
    assert get_tool("transfer.task.config.check").input_schema == get_tool("transfer.task.create").input_schema
