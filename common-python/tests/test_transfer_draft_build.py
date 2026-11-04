import asyncio
import copy
import json

import httpx
import pytest
from jsonschema import Draft202012Validator

from addp_common.client import TransferClient
from addp_common.tools import ToolExecutor, get_tool


def choices():
    return {"name": "persons", "source_locator": "addp://engine/11/path/Outdoor/Persons?type=collection&item_id=106",
            "target": {"parent_locator": "addp://engine/2/path/outdoor?type=schema&node_id=15", "name": "persons", "apply_mode": "replace"},
            "row_grain": {"mode": "document"}, "fields": [{"path": ["members"], "target": "members"}]}


def arguments():
    return {"name": "persons", "config": {"runtime": {"boundary": "bounded"}, "load": {"mode": "snapshot"},
            "source": {"locator": choices()["source_locator"], "data_type": "table", "representation": "native",
                       "query": {"language": "mql", "statement": '{"aggregate":"Persons","pipeline":[{"$project":{"_id":0,"field_0":"$members"}}]}'}},
            "target": {"parent_locator": choices()["target"]["parent_locator"], "name": "persons", "data_type": "table", "representation": "native", "policy": {"apply_mode": "replace"}},
            "transforms": [{"type": "field_mapping", "version": "v1", "mode": "project", "fields": [{"source": "field_0", "target": "members", "target_type": "json", "nullable": True}]}]}}


def test_draft_tool_sdk_and_executor_return_exact_owner_arguments():
    async def run():
        seen = []
        definition = get_tool("transfer.task.draft.build")
        assert set(definition.auth.required_permissions) == {"transfer.task.create", "meta.catalog.read", "system.engine.read", "manager.data_item.read"}

        def owner(request):
            assert request.url.path == "/api/v1/transfer/task-config-drafts"
            assert request.headers["Authorization"] == "Bearer addp_dat_draft"
            assert json.loads(request.content) == choices()
            seen.append(request)
            return httpx.Response(200, json={"arguments": arguments()})

        def client_factory(client_type, token):
            assert client_type is TransferClient and token == "addp_dat_draft"
            client = client_type("http://gateway", user_token=token)
            client._client._transport = httpx.MockTransport(owner)
            return client

        executor = ToolExecutor("http://gateway", "addp_at_private")
        executor._client = client_factory
        async def issue(tool, **binding):
            assert tool.name == "transfer.task.draft.build"
            assert binding == {"agent_run_id": "run", "tool_call_id": "draft"}
            return "addp_dat_draft"
        executor._issue_delegated_token = issue
        result = await executor.call(definition.name, choices(), agent_run_id="run", tool_call_id="draft")
        async with client_factory(TransferClient, "addp_dat_draft") as client:
            direct = await client.build_task_config_draft(**choices())
        assert result == direct == {"arguments": arguments()}
        assert len(seen) == 2
    asyncio.run(run())


def test_draft_schema_rejects_executable_input_and_unconfirmed_row_grain():
    validator = Draft202012Validator(get_tool("transfer.task.draft.build").input_schema)
    assert not list(validator.iter_errors(choices()))
    for key in ("config", "schema", "query", "credential", "tenant_id"):
        invalid = {**choices(), key: {}}
        assert list(validator.iter_errors(invalid))
    invalid = copy.deepcopy(choices())
    invalid["row_grain"] = {"mode": "array_element"}
    assert list(validator.iter_errors(invalid))


def test_draft_sdk_validates_result_against_the_single_create_contract():
    async def run():
        invalid = arguments()
        invalid["config"]["target"]["policy"]["apply_mode"] = "invented"
        async with TransferClient("http://gateway", user_token="addp_dat_draft") as client:
            client._client._transport = httpx.MockTransport(lambda _: httpx.Response(200, json={"arguments": invalid}))
            with pytest.raises(ValueError):
                await client.build_task_config_draft(**choices())
    asyncio.run(run())
