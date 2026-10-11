"""Owner SDK contract; Tool and Agent integration have separate evidence."""
import asyncio
import copy
import json

import httpx
import pytest

from addp_common.client import TransferClient


def arguments():
    return {
        "name": "definition fixture <北京>",
        "description": "check exactly the submitted definition",
        "batch_size": 23,
        "config": {
            "runtime": {"boundary": "bounded"}, "load": {"mode": "snapshot"},
            "source": {
                "locator": "addp://engine/1/path/demo/source?type=table&item_id=2",
                "data_type": "table", "representation": "native",
            },
            "target": {
                "parent_locator": "addp://engine/3/path/demo?type=schema&node_id=4",
                "name": "target", "data_type": "table", "representation": "native",
                "policy": {"apply_mode": "replace"},
            },
            "transforms": [{"type": "field_mapping", "version": "v1", "mode": "project", "fields": [
                {"source": "id", "target": "id", "target_type": "string", "nullable": False},
            ]}],
        },
    }


def report(status):
    return {
        "schema_version": "addp.transfer_task_config_check/v1",
        "status": status, "tenant_id": 3, "principal_id": "user-fixture",
        "config_hash": "a" * 64, "binding_hash": "b" * 64,
        "observed_at": "2026-10-11T00:00:00Z",
        "checked": ["task_scope"], "not_checked": ["business_rows"],
        "runtime_checks_required": ["per_value_encoding"],
        "facts": [{"role": "source", "schema_coverage": "sampled"}],
        "diagnostics": [{"status": status, "code": "fixture", "check": "schema",
                         "config_path": "config.source"}],
    }


@pytest.mark.parametrize("status", ["supported", "unsupported", "unknown"])
def test_check_preserves_owner_report_without_side_effects(status):
    async def run():
        requests = []
        expected = report(status)
        params = arguments()
        original = copy.deepcopy(params)

        async def transport(request):
            requests.append(request)
            assert request.method == "POST"
            assert request.url.path == "/api/v1/transfer/task-config-checks"
            assert not request.url.query
            assert request.headers["Authorization"] == "Bearer delegated-fixture"
            assert json.loads(request.content) == {
                **params, "task_type": "sync", "schedule": "",
                "enabled": False, "auto_scan_metadata": False,
            }
            return httpx.Response(200, json=expected)

        async with TransferClient("http://gateway", user_token="delegated-fixture") as client:
            client._client._transport = httpx.MockTransport(transport)
            assert await client.check_task_config(**params) == expected
        assert params == original
        assert len(requests) == 1  # No task, execution, scan, or automatic retry.

    asyncio.run(run())


def test_check_and_create_submit_identical_full_requests():
    async def run():
        requests = []

        async def transport(request):
            requests.append(request)
            if request.url.path == "/api/v1/transfer/task-config-checks":
                return httpx.Response(200, json=report("unknown"))
            assert request.url.path == "/api/v1/transfer/task-definitions"
            return httpx.Response(201, json={"id": 7, "status": "idle", "desired_state": "stopped"})

        async with TransferClient("http://gateway", user_token="delegated-fixture") as client:
            client._client._transport = httpx.MockTransport(transport)
            params = arguments()
            await client.check_task_config(**params)
            assert len(requests) == 1  # Creating remains an explicit separate call.
            assert (await client.create_task(**params))["id"] == 7
        assert len(requests) == 2
        assert requests[0].content == requests[1].content

    asyncio.run(run())


@pytest.mark.parametrize("status", [400, 401, 403, 502])
def test_http_errors_are_not_unknown_reports_or_retried(status):
    async def run():
        requests = []

        async def transport(request):
            requests.append(request)
            return httpx.Response(status, json={"error": "owner error"})

        async with TransferClient("http://gateway", user_token="delegated-fixture") as client:
            client._client._transport = httpx.MockTransport(transport)
            with pytest.raises(httpx.HTTPStatusError) as error:
                await client.check_task_config(**arguments())
            assert error.value.response.status_code == status
        assert len(requests) == 1

    asyncio.run(run())


def test_transport_failure_is_not_retried_or_downgraded():
    async def run():
        requests = []

        async def transport(request):
            requests.append(request)
            raise httpx.ReadTimeout("fixture timeout")

        async with TransferClient("http://gateway", user_token="delegated-fixture") as client:
            client._client._transport = httpx.MockTransport(transport)
            with pytest.raises(httpx.ReadTimeout):
                await client.check_task_config(**arguments())
        assert len(requests) == 1

    asyncio.run(run())


@pytest.mark.parametrize("body", [None, [], "supported"])
def test_check_rejects_non_object_owner_response(body):
    async def run():
        async with TransferClient("http://gateway", user_token="delegated-fixture") as client:
            client._client._transport = httpx.MockTransport(
                lambda request: httpx.Response(200, content=json.dumps(body),
                                               headers={"Content-Type": "application/json"})
            )
            with pytest.raises(ValueError, match="response must be an object"):
                await client.check_task_config(**arguments())

    asyncio.run(run())
