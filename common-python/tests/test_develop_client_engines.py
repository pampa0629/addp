import asyncio
import json

import httpx

from addp_common.client.develop import DevelopClient


def test_list_engines_decodes_array_response():
    asyncio.run(_test_list_engines_decodes_array_response())


async def _test_list_engines_decodes_array_response():
    async def handler(request):
        assert request.url.path == "/api/v1/develop/engines"
        return httpx.Response(200, json=[{
            "id": 1,
            "name": "PostgreSQL",
            "engine_type": "postgresql",
        }])

    client = DevelopClient("http://develop")
    client._client = httpx.AsyncClient(
        base_url="http://develop",
        transport=httpx.MockTransport(handler),
    )

    try:
        engines = await client.list_engines()
    finally:
        await client.close()

    assert engines == [{
        "id": 1,
        "name": "PostgreSQL",
        "engine_type": "postgresql",
    }]


def test_list_engines_rejects_legacy_object_response():
    asyncio.run(_test_list_engines_rejects_legacy_object_response())


async def _test_list_engines_rejects_legacy_object_response():
    async def handler(request):
        return httpx.Response(200, json={"engines": []})

    client = DevelopClient("http://develop")
    client._client = httpx.AsyncClient(
        base_url="http://develop",
        transport=httpx.MockTransport(handler),
    )

    try:
        try:
            await client.list_engines()
        except ValueError as err:
            assert "must be a list" in str(err)
        else:
            raise AssertionError("list_engines() should reject legacy object response")
    finally:
        await client.close()


def test_list_workflow_engines_decodes_array_response():
    asyncio.run(_test_list_workflow_engines_decodes_array_response())


async def _test_list_workflow_engines_decodes_array_response():
    async def handler(request):
        assert request.url.path == "/api/v1/develop/workflow-engines"
        return httpx.Response(200, json=[{
            "id": 12,
            "name": "GeoPython Workflow",
            "engine_type": "geopython_workflow",
        }])

    client = DevelopClient("http://develop")
    client._client = httpx.AsyncClient(
        base_url="http://develop",
        transport=httpx.MockTransport(handler),
    )

    try:
        engines = await client.list_workflow_engines()
    finally:
        await client.close()

    assert engines[0]["id"] == 12


def test_list_operators_uses_workflow_engine_route():
    asyncio.run(_test_list_operators_uses_workflow_engine_route())


async def _test_list_operators_uses_workflow_engine_route():
    async def handler(request):
        assert request.url.path == "/api/v1/develop/workflow-engines/12/operators"
        return httpx.Response(200, json={
            "workflow_engine_id": 12,
            "operators": [{
                "name": "tiff_to_cog",
                "engine_type": "geopython_workflow",
                "category_path": ["格式转换"],
                "execution_modes": ["workflow", "direct"],
            }]
        })

    client = DevelopClient("http://develop")
    client._client = httpx.AsyncClient(
        base_url="http://develop",
        transport=httpx.MockTransport(handler),
    )

    try:
        operators = await client.list_operators(12)
    finally:
        await client.close()

    assert operators[0]["execution_modes"] == ["workflow", "direct"]


def test_execute_sql_uses_content_and_execution_config():
    asyncio.run(_test_execute_sql_uses_content_and_execution_config())


def test_validate_workflow_uses_canonical_resource():
    asyncio.run(_test_validate_workflow_uses_canonical_resource())


async def _test_validate_workflow_uses_canonical_resource():
    workflow = {"tasks": [{"id": "task1", "operator": "noop", "params": {}, "depends_on": []}]}

    async def handler(request):
        assert request.url.path == "/api/v1/develop/workflow-validations"
        assert json.loads(request.content) == {
            "workflow_engine_id": 12,
            "workflow_definition": workflow,
        }
        return httpx.Response(200, json={
            "valid": True,
            "workflow_engine_id": 12,
            "errors": [],
            "warnings": [],
        })

    client = DevelopClient("http://develop")
    client._client = httpx.AsyncClient(
        base_url="http://develop",
        transport=httpx.MockTransport(handler),
    )
    try:
        result = await client.validate_workflow(workflow, workflow_engine_id=12)
    finally:
        await client.close()

    assert result["valid"] is True


async def _test_execute_sql_uses_content_and_execution_config():
    async def handler(request):
        assert request.url.path == "/api/v1/develop/execute"
        payload = json.loads(request.content)
        assert payload == {
            "content": {
                "query_type": "sql",
                "query": "SELECT 1",
            },
            "execution_config": {
                "engine_id": 7,
            },
        }
        assert "engine_id" not in {k for k in payload if k != "execution_config"}
        return httpx.Response(200, json={"columns": [], "rows": []})

    client = DevelopClient("http://develop")
    client._client = httpx.AsyncClient(
        base_url="http://develop",
        transport=httpx.MockTransport(handler),
    )

    try:
        result = await client.execute_sql("SELECT 1", engine_id=7)
    finally:
        await client.close()

    assert result == {"columns": [], "rows": []}


def test_run_workflow_content_uses_execution_config():
    asyncio.run(_test_run_workflow_content_uses_execution_config())


async def _test_run_workflow_content_uses_execution_config():
    workflow = {"tasks": [{"id": "task1", "operator": "noop", "params": {}, "depends_on": []}]}

    async def handler(request):
        assert request.url.path == "/api/v1/develop/executions"
        payload = json.loads(request.content)
        assert payload == {
            "dev_type": "workflow",
            "trigger_type": "manual",
            "content": {
                "workflow_definition": workflow,
                "inputs": {},
            },
            "execution_config": {
                "engine_id": 12,
            },
        }
        assert "engine_id" not in {k for k in payload if k != "execution_config"}
        return httpx.Response(200, json={"execution_id": "exec-1"})

    client = DevelopClient("http://develop")
    client._client = httpx.AsyncClient(
        base_url="http://develop",
        transport=httpx.MockTransport(handler),
    )

    try:
        result = await client.run_workflow_content(workflow, engine_id=12)
    finally:
        await client.close()

    assert result == {"execution_id": "exec-1"}


def test_run_workflow_content_supports_engine_specific_config():
    asyncio.run(_test_run_workflow_content_supports_engine_specific_config())


async def _test_run_workflow_content_supports_engine_specific_config():
    workflow = {"tasks": [{"id": "task1", "operator": "noop", "params": {}, "depends_on": []}]}

    async def handler(request):
        payload = json.loads(request.content)
        assert payload["execution_config"] == {
            "engine_id": 12,
            "engine_specific": {
                "spark_cluster_id": 88,
            },
        }
        return httpx.Response(200, json={"execution_id": "exec-1"})

    client = DevelopClient("http://develop")
    client._client = httpx.AsyncClient(
        base_url="http://develop",
        transport=httpx.MockTransport(handler),
    )

    try:
        result = await client.run_workflow_content(
            workflow,
            engine_id=12,
            engine_specific={"spark_cluster_id": 88},
        )
    finally:
        await client.close()

    assert result == {"execution_id": "exec-1"}
