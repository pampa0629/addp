import asyncio

from addp_common.tools import ToolExecutionError, ToolExecutor, get_tool, load_manifest


def test_manifest_has_unique_stage_two_tools():
    manifest = load_manifest()
    names = [tool.name for tool in manifest.tools]
    assert manifest.schema_id == "addp.tool-manifest/v1"
    assert names == [
        "engine.list",
        "data.search",
        "resource.ancestors.get",
        "data.preview",
        "workflow.operators.list",
        "workflow.draft.generate",
        "workflow.validate",
        "workflow.run",
        "execution.get",
    ]
    assert get_tool("workflow.run").risk == "write"


def test_executor_validates_arguments_before_dispatch():
    async def run():
        executor = ToolExecutor("http://gateway", "token")
        try:
            await executor.call("data.search", {"limit": 100})
        except ToolExecutionError as exc:
            assert exc.code == "invalid_arguments"
        else:
            raise AssertionError("invalid arguments must fail")

    asyncio.run(run())


def test_executor_returns_validated_tool_result():
    async def run():
        executor = ToolExecutor("http://gateway", "token")

        async def fake_handler(arguments):
            return {"valid": True, "workflow_engine_id": arguments["workflow_engine_id"], "errors": [], "warnings": []}

        executor._handlers["workflow.validate"] = fake_handler
        return await executor.call("workflow.validate", {
            "workflow_engine_id": 12,
            "workflow_definition": {"tasks": []},
        })

    result = asyncio.run(run())
    assert result["valid"] is True
