import asyncio
import copy
import json
from pathlib import Path

import httpx
from jsonschema import Draft202012Validator

from addp_common.client import MetaClient, OntologyClient, TransferClient
from addp_common.tools import ToolExecutionError, ToolExecutor, get_tool, load_manifest


ROOT = Path(__file__).resolve().parents[2]


def task_arguments():
    return {
        "name": "Outdoor MongoDB to PostgreSQL fixture",
        "config": {
            "runtime": {"boundary": "bounded"}, "load": {"mode": "snapshot"},
            "source": {
                "locator": "addp://engine/11/path/Outdoor/Activities?type=collection&item_id=42",
                "data_type": "table", "representation": "native",
                "query": {"language": "mql", "statement": '{"aggregate":"Activities","pipeline":[{"$project":{"activity_id":"$_id","status":1,"_id":0}}]}'},
            },
            "target": {
                "parent_locator": "addp://engine/12/path/demo?type=schema&node_id=5",
                "name": "outdoor_activities", "data_type": "table", "representation": "native",
                "policy": {"apply_mode": "replace"},
            },
            "transforms": [{"type": "field_mapping", "version": "v1", "mode": "project", "fields": [
                {"source": "activity_id", "target": "activity_id", "target_type": "string", "nullable": False},
                {"source": "status", "target": "status", "target_type": "string", "nullable": True},
            ]}],
        },
    }


def test_platform_definition_references_real_tools_and_owner_skill():
    context = json.loads((ROOT / "ontology/backend/internal/platform/transfer.json").read_text())
    context["digest"] = "a" * 64  # Owner computes the content binding, not this fixture.
    assert not list(Draft202012Validator(get_tool("platform.capability.context").output_schema).iter_errors(context))
    tools = {tool.name: tool for tool in load_manifest().tools}
    operation = context["operation"]
    assert tools[operation["tool"]].owner == operation["owner"]
    assert (ROOT / "skills" / operation["skill"] / "SKILL.md").is_file()
    for requirement in context["requirements"]:
        assert set(requirement["tools"]) <= tools.keys()
    assert "execution.created" in operation["excluded_effects"]
    assert "business_data.written" in operation["excluded_effects"]
    assert "tenant_id" not in context


def test_transfer_create_schema_rejects_schedules_credentials_and_other_modes():
    validator = Draft202012Validator(get_tool("transfer.task.create").input_schema)
    valid = task_arguments()
    assert not list(validator.iter_errors(valid))
    for path, value in [
        (("schedule",), "* * * * *"), (("enabled",), True),
        (("tenant_id",), 8), (("run",), True),
        (("config", "runtime", "boundary"), "continuous"),
        (("config", "load", "mode"), "incremental"),
        (("config", "source", "connection_info"), {"password": "private"}),
        (("config", "target", "policy", "apply_mode"), "upsert_delete"),
    ]:
        invalid = copy.deepcopy(valid)
        target = invalid
        for key in path[:-1]:
            target = target[key]
        target[path[-1]] = value
        assert list(validator.iter_errors(invalid)), path
    definition = get_tool("transfer.task.create")
    assert definition.risk == "write" and definition.approval == {"mode": "none"}
    assert definition.auth.audience == "transfer"
    assert definition.auth.required_permissions == ["transfer.task.create"]
    missing_mapping = copy.deepcopy(valid)
    del missing_mapping["config"]["transforms"]
    assert list(validator.iter_errors(missing_mapping))
    missing_mapping["config"]["transforms"] = []
    assert list(validator.iter_errors(missing_mapping))
    upsert = copy.deepcopy(valid)
    upsert["config"]["target"]["policy"] = {"apply_mode": "upsert"}
    assert list(validator.iter_errors(upsert))
    upsert["config"]["target"]["policy"]["keys"] = ["activity_id"]
    assert not list(validator.iter_errors(upsert))


def test_platform_and_transfer_executor_use_exact_owner_and_safe_sdk_projection():
    async def run():
        requests = []

        async def transport(request):
            requests.append(request)
            assert request.headers["Authorization"] == "Bearer addp_dat_fixture"
            if request.url.path.startswith("/api/v1/ontology/"):
                assert request.url.path == "/api/v1/ontology/platform/capabilities/transfer.task.create"
                context = json.loads((ROOT / "ontology/backend/internal/platform/transfer.json").read_text())
                return httpx.Response(200, json={**context, "digest": "a" * 64})
            assert request.method == "POST" and request.url.path == "/api/v1/transfer/task-definitions"
            body = json.loads(request.content)
            assert body["schedule"] == "" and body["enabled"] is False and body["auto_scan_metadata"] is False
            assert body["task_type"] == "sync" and body["config"] == task_arguments()["config"]
            assert "tenant_id" not in body
            return httpx.Response(201, json={"id": 87, "name": body["name"], "task_type": "sync", "status": "idle", "desired_state": "stopped", "enabled": False, "schedule": "", "config": body["config"], "created_by": 9, "tenant_id": 7})

        def client_factory(client_type, token):
            assert client_type in (TransferClient, OntologyClient)
            client = client_type("http://gateway", user_token=token)
            client._client._transport = httpx.MockTransport(transport)
            return client

        executor = ToolExecutor("http://gateway", "addp_at_private")
        executor._client = client_factory

        async def issue(definition, **binding):
            assert binding == {"agent_run_id": "run", "tool_call_id": "call"}
            assert definition.auth.required_scopes == [definition.name]
            return "addp_dat_fixture"

        executor._issue_delegated_token = issue
        context = await executor.call("platform.capability.context", {"capability": "transfer.task.create"}, agent_run_id="run", tool_call_id="call")
        assert context["availability"] == "not_observed"
        task = await executor.call("transfer.task.create", task_arguments(), agent_run_id="run", tool_call_id="call")
        assert task["id"] == 87 and "config" not in task and "tenant_id" not in task
        assert len(requests) == 2  # No execution, scan, start or second metadata write.

    asyncio.run(run())


def test_transfer_creation_timeout_is_not_retried():
    async def run():
        executor = ToolExecutor("http://gateway", "private")
        calls = []

        async def issue(*args, **kwargs):
            return "delegated"

        async def handler(*args):
            calls.append(1)
            raise httpx.ReadTimeout("unknown write outcome")

        executor._issue_delegated_token = issue
        executor._handlers["transfer.task.create"] = handler
        try:
            await executor.call("transfer.task.create", task_arguments(), agent_run_id="run", tool_call_id="call")
        except ToolExecutionError as exc:
            assert exc.code == "owner_api_unavailable"
        else:
            raise AssertionError("unknown write outcome must fail")
        assert calls == [1]

    asyncio.run(run())


def test_root_browsing_uses_meta_root_api_not_invented_locator():
    async def run():
        requests = []

        async def transport(request):
            requests.append(request)
            assert request.url.path == "/api/v1/meta/resource-tree/12"
            assert request.url.params == httpx.QueryParams({"expand_depth": "1"})
            return httpx.Response(200, json={"locator": "addp://engine/12/path?type=engine", "children": []})

        def client_factory(client_type, token):
            assert client_type is MetaClient
            client = MetaClient("http://gateway", user_token=token)
            client._client._transport = httpx.MockTransport(transport)
            return client

        executor = ToolExecutor("http://gateway", "private")
        executor._client = client_factory

        async def issue(*args, **kwargs):
            return "delegated"

        executor._issue_delegated_token = issue
        result = await executor.call("resource.children.list", {"engine_id": 12}, agent_run_id="run", tool_call_id="call")
        assert result["children"] == [] and len(requests) == 1

    asyncio.run(run())
