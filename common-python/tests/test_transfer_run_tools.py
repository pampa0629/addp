"""Delegated Tool/SDK contract; these tests never approve or run business work."""
import asyncio
import json
from uuid import uuid4

import httpx
import pytest
from jsonschema import Draft202012Validator

from addp_common.client import TransferClient
from addp_common.tools import ToolExecutionError, ToolExecutor, get_tool


def test_start_contract_separates_request_and_consumption():
    definition = get_tool("transfer.task.start")
    validator = Draft202012Validator(definition.input_schema)
    assert definition.approval == {"mode": "owner_policy"}
    assert definition.auth.required_permissions == ["transfer.task.execute", "transfer.task.read"]
    assert not list(validator.iter_errors({"task_id": 7}))
    valid = {"task_id": 7, "approval_id": str(uuid4()), "request_fingerprint": "a" * 64}
    assert not list(validator.iter_errors(valid))
    for params in [{}, {"task_id": 7, "approved": True}, {"task_id": 7, "approval_id": valid["approval_id"]},
                   {**valid, "config": {}}, {**valid, "task_id": True}]:
        assert list(validator.iter_errors(params))


def test_start_sdk_preserves_approval_and_never_calls_decision():
    async def run():
        requests = []
        approval = {"status": "approval_required", "interaction_id": str(uuid4()),
                    "request_fingerprint": "a" * 64, "request_summary": {"task_id": 7},
                    "expires_at": "2026-11-02T00:15:00Z"}
        async def handle(request):
            requests.append(request)
            assert request.url.path == "/api/v1/transfer/task-definitions/7/start"
            assert request.headers["Authorization"] == "Bearer delegated-fixture"
            assert json.loads(request.content) == {}
            return httpx.Response(202, json={**approval, "private": "do-not-forward"})
        client = TransferClient("http://gateway", user_token="delegated-fixture")
        await client._client.aclose()
        client._client = httpx.AsyncClient(base_url="http://gateway", headers={"Authorization": "Bearer delegated-fixture"}, transport=httpx.MockTransport(handle))
        async with client:
            assert await client.start_task(7) == approval
        assert len(requests) == 1
    asyncio.run(run())


def test_start_write_is_not_retried_after_uncertain_response():
    async def run():
        calls = []
        async def unavailable(request):
            calls.append(request)
            raise httpx.ReadTimeout("response lost", request=request)
        client = TransferClient("http://gateway", user_token="delegated-fixture")
        await client._client.aclose()
        client._client = httpx.AsyncClient(base_url="http://gateway", transport=httpx.MockTransport(unavailable))
        async with client:
            with pytest.raises(httpx.ReadTimeout):
                await client.start_task(7, approval_id=str(uuid4()), request_fingerprint="a" * 64)
        assert len(calls) == 1
    asyncio.run(run())


def test_execution_projection_excludes_runtime_text_and_configuration():
    payload = {"execution_id": str(uuid4()), "task_id": 7, "status": "failed", "records_read": 0,
               "records_written": 0, "metadata": {"secret": "hidden"}, "config": {"password": "hidden"},
               "error_details": {"message": "business value", "diagnostic": {
                   "schema_version": "addp.execution-failure/v3", "stage": "execution", "reason": "array_invalid",
                   "target_change_possible": True, "write": {"column_index": 2, "declared_type": "array"},
                   "sql": {"identifier": "hidden"},
               }}}
    result = TransferClient._execution_projection(payload)
    assert "metadata" not in result and "config" not in result and "error_details" not in result
    assert "sql" not in result["diagnostic"]
    assert result["diagnostic"]["target_change_possible"] is True
    assert not list(Draft202012Validator(get_tool("transfer.execution.get").output_schema).iter_errors(result))
    # The existing professional owner scope also includes the current user's
    # ad-hoc execution, represented by task_id=0 rather than an invented task.
    assert not list(Draft202012Validator(get_tool("transfer.execution.get").output_schema).iter_errors({**result, "task_id": 0}))


def test_invalid_start_arguments_fail_before_delegation():
    async def run():
        executor = ToolExecutor("http://gateway", "source-user")
        async def forbidden(*args, **kwargs):
            raise AssertionError("invalid arguments must not delegate")
        executor._issue_delegated_token = forbidden
        with pytest.raises(ToolExecutionError) as exc:
            await executor.call("transfer.task.start", {"task_id": 7, "approved": True}, agent_run_id="run", tool_call_id="call")
        assert exc.value.code == "invalid_arguments"
    asyncio.run(run())
