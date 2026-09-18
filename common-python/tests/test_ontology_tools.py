import asyncio
import httpx
from jsonschema import Draft202012Validator

from addp_common.client import OntologyClient
from addp_common.tools import ToolExecutionError, ToolExecutor, get_tool


GENERATION = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
BINDING = {"ontology_id": "beijing_outdoor", "revision": 2, "generation": GENERATION,
           "activation_version": 3, "digest": "a" * 64, "knowledge_kind": "native_definition"}


def test_ontology_sdk_and_executor_use_owner_api_and_delegated_identity():
    async def run():
        requests = []

        async def transport(request):
            requests.append(request)
            assert request.headers["Authorization"] == "Bearer addp_dat_fixture"
            if request.url.path.endswith("/classes"):
                assert not request.url.query
                return httpx.Response(200, json={**BINDING, "classes": [{"id": "activity", "name": "北京活动", "parents": []}]})
            assert request.url.params == httpx.QueryParams({"revision": "2", "generation": GENERATION, "activation_version": "3"})
            return httpx.Response(200, json={**BINDING, "class": {"id": "activity", "name": "北京活动", "parents": []}, "ancestors": [], "properties": [], "relations": [], "rules": []})

        def client_factory(client_type, token):
            assert client_type is OntologyClient
            client = OntologyClient("http://gateway", user_token=token)
            client._client._transport = httpx.MockTransport(transport)
            return client

        executor = ToolExecutor("http://gateway", "addp_at_private")
        executor._client = client_factory

        async def issue(definition, **kwargs):
            assert definition.auth.required_permissions == ["ontology.semantic.read"]
            assert kwargs == {"agent_run_id": "run", "tool_call_id": "call"}
            return "addp_dat_fixture"

        executor._issue_delegated_token = issue
        directory = await executor.call("ontology.classes.list", {"ontology_id": "beijing_outdoor"}, agent_run_id="run", tool_call_id="call")
        args = {key: directory[key] for key in ("ontology_id", "revision", "generation", "activation_version")}
        result = await executor.call("ontology.class.context", {**args, "class_id": "activity"}, agent_run_id="run", tool_call_id="call")
        assert result["knowledge_kind"] == "native_definition"
        assert [r.url.path for r in requests] == ["/api/v1/ontology/ontologies/beijing_outdoor/semantic/classes", "/api/v1/ontology/ontologies/beijing_outdoor/semantic/classes/activity"]

    asyncio.run(run())


def test_ontology_context_requires_binding_and_rejects_unbounded_arguments():
    tool = get_tool("ontology.class.context")
    validator = Draft202012Validator(tool.input_schema)
    args = {"ontology_id": "beijing_outdoor", "class_id": "activity", "revision": 2, "generation": GENERATION, "activation_version": 3}
    assert not list(validator.iter_errors(args))
    for key in args:
        assert list(validator.iter_errors({k: v for k, v in args.items() if k != key}))
    assert list(validator.iter_errors({**args, "cypher": "MATCH (n) RETURN n"}))
    assert list(validator.iter_errors({**args, "tenant_id": 102}))
    assert tool.risk == "read"


def test_ontology_owner_activation_error_is_not_a_success_or_retry():
    async def run():
        executor = ToolExecutor("http://gateway", "user")
        calls = []

        async def issue(*_args, **_kwargs):
            return "delegated"

        async def handler(*_args):
            calls.append(1)
            request = httpx.Request("GET", "http://gateway/api/v1/ontology/ontologies/outdoor/semantic/classes")
            response = httpx.Response(409, request=request, json={"error": "版本已变化", "error_code": "ontology_activation_changed"})
            raise httpx.HTTPStatusError("conflict", request=request, response=response)

        executor._issue_delegated_token = issue
        executor._handlers["ontology.classes.list"] = handler
        try:
            await executor.call("ontology.classes.list", {"ontology_id": "outdoor"}, agent_run_id="run", tool_call_id="call")
        except ToolExecutionError as exc:
            assert exc.code == "ontology_activation_changed"
        else:
            raise AssertionError("activation change must fail")
        assert calls == [1]

    asyncio.run(run())
