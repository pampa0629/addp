import asyncio

from jsonschema import Draft202012Validator

from addp_common.tools import ToolExecutionError, ToolExecutor, get_tool, load_manifest


def test_manifest_has_unique_stage_two_tools():
    manifest = load_manifest()
    names = [tool.name for tool in manifest.tools]
    assert manifest.schema_id == "addp.tool-manifest/v1"
    assert names == [
        "engine.list",
        "data.search",
        "resource.children.list",
        "resource.ancestors.get",
        "resource.facts.get",
        "data.preview",
        "workflow.operators.list",
        "workflow.draft.generate",
        "query.draft.generate",
        "notebook.draft.generate",
        "transfer.draft.generate",
        "workflow.validate",
        "workflow.run",
        "execution.get",
    ]
    assert get_tool("workflow.run").risk == "write"
    for tool in manifest.tools:
        assert tool.auth.type == "delegated_access_token"
        assert tool.auth.audience == tool.owner
        assert tool.auth.required_scopes == [tool.name]
    expected_permissions = {
        "engine.list": ["system.engine.read"],
        "data.search": ["manager.search.execute"],
        "resource.children.list": ["meta.catalog.read"],
        "resource.ancestors.get": ["meta.catalog.read"],
        "resource.facts.get": ["manager.data_item.read"],
        "data.preview": ["manager.data_item.read"],
        "workflow.operators.list": ["develop.task.read"],
        "workflow.draft.generate": ["copilot.workflow.execute"],
        "query.draft.generate": ["copilot.sql.execute"],
        "notebook.draft.generate": ["copilot.notebook.execute"],
        "transfer.draft.generate": ["copilot.transfer.execute"],
        "workflow.validate": ["develop.task.execute"],
        "workflow.run": ["develop.task.execute"],
        "execution.get": ["develop.task.read"],
    }
    for tool in manifest.tools:
        assert tool.auth.required_permissions == expected_permissions[tool.name]


def test_executor_validates_arguments_before_dispatch():
    async def run():
        executor = ToolExecutor("http://gateway", "token")
        try:
            await executor.call(
                "data.search",
                {"limit": 100},
                agent_run_id="run-1",
                tool_call_id="call-1",
            )
        except ToolExecutionError as exc:
            assert exc.code == "invalid_arguments"
        else:
            raise AssertionError("invalid arguments must fail")

    asyncio.run(run())


def test_query_draft_manifest_allows_current_mql_without_resources():
    definition = get_tool("query.draft.generate")
    validator = Draft202012Validator(definition.input_schema)

    errors = list(validator.iter_errors({
        "query": "只保留成年人",
        "engine_id": 11,
        "query_language": "mql",
        "engine_context": {"id": 11},
        "resources": [],
        "current_query": '{"find":"Persons","filter":{},"limit":10}',
    }))

    assert errors == []


def test_query_draft_manifest_allows_catalog_discovery_scope_without_resources():
    definition = get_tool("query.draft.generate")
    validator = Draft202012Validator(definition.input_schema)

    errors = list(validator.iter_errors({
        "query": "查询用户参加的活动",
        "engine_id": 11,
        "query_language": "mql",
        "engine_context": {"id": 11},
        "resources": [],
        "resource_scope_locator": "addp://engine/11/path/Outdoor?type=database&node_id=276",
    }))

    assert errors == []


def test_query_draft_manifest_requires_query_parameter_definitions_in_output():
    definition = get_tool("query.draft.generate")
    validator = Draft202012Validator(definition.output_schema)

    valid = {
        "status": "success",
        "query_language": "mql",
        "query": '{"find":"Persons","filter":{"userInfo.nickName":{"$param":"nickname"}}}',
        "query_parameters": [{"name": "nickname", "type": "string", "default": "PiPi"}],
    }

    assert list(validator.iter_errors(valid)) == []
    assert list(validator.iter_errors({**valid, "query_parameters": None}))


def test_executor_returns_validated_tool_result():
    async def run():
        executor = ToolExecutor("http://gateway", "token")

        async def fake_handler(arguments, delegated_token):
            assert delegated_token == "addp_dat_test"
            return {"valid": True, "workflow_engine_id": arguments["workflow_engine_id"], "errors": [], "warnings": []}

        executor._handlers["workflow.validate"] = fake_handler

        async def issue(definition, *, agent_run_id, tool_call_id):
            assert definition.name == "workflow.validate"
            assert agent_run_id == "run-1"
            assert tool_call_id == "call-1"
            return "addp_dat_test"

        executor._issue_delegated_token = issue
        return await executor.call(
            "workflow.validate",
            {
                "workflow_engine_id": 12,
                "workflow_definition": {"tasks": []},
            },
            agent_run_id="run-1",
            tool_call_id="call-1",
        )

    result = asyncio.run(run())
    assert result["valid"] is True


def test_workflow_run_manifest_supports_initial_and_approval_resume_only():
    definition = get_tool("workflow.run")
    schema = definition.input_schema
    validator = Draft202012Validator(schema)

    assert "approval_forbidden" in definition.errors

    assert not list(validator.iter_errors({
        "workflow_engine_id": 12,
        "workflow_definition": {"tasks": []},
    }))
    assert not list(validator.iter_errors({
        "approval_id": "1b842c47-cdf4-4228-af6e-25bfbaa8609b",
        "request_fingerprint": "a" * 64,
    }))
    assert list(validator.iter_errors({
        "workflow_engine_id": 12,
        "workflow_definition": {"tasks": []},
        "approval_id": "1b842c47-cdf4-4228-af6e-25bfbaa8609b",
        "request_fingerprint": "a" * 64,
    }))


def test_executor_preserves_stable_owner_approval_error():
    async def run_case(status_code, error_code, error_message):
        executor = ToolExecutor("http://gateway", "token")

        async def issue(*_args, **_kwargs):
            return "addp_dat_test"

        async def fail_handler(_arguments, _delegated_token):
            import httpx

            request = httpx.Request("POST", "http://gateway/api/v1/develop/executions")
            response = httpx.Response(
                status_code,
                request=request,
                json={"error": {"code": error_code, "message": error_message}},
            )
            raise httpx.HTTPStatusError("conflict", request=request, response=response)

        executor._issue_delegated_token = issue
        executor._handlers["workflow.run"] = fail_handler
        try:
            await executor.call(
                "workflow.run",
                {
                    "approval_id": "1b842c47-cdf4-4228-af6e-25bfbaa8609b",
                    "request_fingerprint": "a" * 64,
                },
                agent_run_id="run-1",
                tool_call_id="call-1",
            )
        except ToolExecutionError as exc:
            assert exc.code == error_code
            assert exc.message == error_message
        else:
            raise AssertionError("owner approval error must be preserved")

    asyncio.run(run_case(409, "approval_not_approved", "审批尚未批准"))
    asyncio.run(run_case(403, "approval_forbidden", "审批不属于当前 AgentRun"))
