import asyncio

import httpx

from addp_common.client.system import SystemClient


def test_list_engines_decodes_paginated_response():
    asyncio.run(_test_list_engines_decodes_paginated_response())


async def _test_list_engines_decodes_paginated_response():
    async def handler(request):
        assert request.url.path == "/api/v1/system/engines"
        assert request.url.params["tenant_id"] == "7"
        return httpx.Response(200, json={
            "data": [{"id": 1, "name": "GeoPython Workflow"}],
            "total": 1,
            "page": 1,
            "page_size": 10,
            "total_pages": 1,
        })

    client = SystemClient("http://system")
    client._client = httpx.AsyncClient(
        base_url="http://system",
        transport=httpx.MockTransport(handler),
    )

    try:
        engines = await client.list_engines(tenant_id=7)
    finally:
        await client.close()

    assert engines == [{"id": 1, "name": "GeoPython Workflow"}]


def test_list_engines_rejects_legacy_response_shape():
    asyncio.run(_test_list_engines_rejects_legacy_response_shape())


async def _test_list_engines_rejects_legacy_response_shape():
    async def handler(request):
        return httpx.Response(200, json={"engines": []})

    client = SystemClient("http://system")
    client._client = httpx.AsyncClient(
        base_url="http://system",
        transport=httpx.MockTransport(handler),
    )

    try:
        try:
            await client.list_engines()
        except ValueError as err:
            assert "paginated object with data" in str(err)
        else:
            raise AssertionError("list_engines() should reject legacy response shape")
    finally:
        await client.close()


def test_list_internal_engines_decodes_array_response():
    asyncio.run(_test_list_internal_engines_decodes_array_response())


def test_internal_client_sends_tenant_context_header():
    asyncio.run(_test_internal_client_sends_tenant_context_header())


async def _test_internal_client_sends_tenant_context_header():
    client = SystemClient(
        "http://system",
        internal_api_key="internal-key",
        tenant_id=7,
    )
    try:
        assert client._client.headers["X-Internal-API-Key"] == "internal-key"
        assert client._client.headers["X-Tenant-ID"] == "7"
    finally:
        await client.close()


async def _test_list_internal_engines_decodes_array_response():
    async def handler(request):
        assert request.url.path == "/api/v1/internal/engines"
        assert request.url.params["tenant_id"] == "3"
        return httpx.Response(200, json=[{"id": 11, "name": "Spark Runtime"}])

    client = SystemClient("http://system", internal_api_key="internal-key")
    client._client = httpx.AsyncClient(
        base_url="http://system",
        transport=httpx.MockTransport(handler),
    )

    try:
        engines = await client.list_internal_engines(tenant_id=3)
    finally:
        await client.close()

    assert engines == [{"id": 11, "name": "Spark Runtime"}]


def test_get_workflow_engines_filters_active_v1_workflow_engines():
    asyncio.run(_test_get_workflow_engines_filters_active_v1_workflow_engines())


async def _test_get_workflow_engines_filters_active_v1_workflow_engines():
    async def handler(request):
        return httpx.Response(200, json={
            "data": [
                {
                    "id": 1,
                    "name": "GeoPython Workflow",
                    "engine_type": "geopython_workflow",
                    "is_active": True,
                    "connection_status": "online",
                    "capabilities": {
                        "schema_version": "engine.capabilities/v1",
                        "engine_type": "geopython_workflow",
                        "engine_family": "workflow",
                        "compute": {
                            "workflow": {
                                "supported": True,
                                "runtime_api": "addp.workflow/v1",
                                "dynamic_operators": True,
                            }
                        },
                    },
                },
                {
                    "id": 2,
                    "name": "Inactive Workflow",
                    "engine_type": "math_workflow",
                    "is_active": False,
                    "capabilities": {
                        "schema_version": "engine.capabilities/v1",
                        "compute": {"workflow": {"supported": True}},
                    },
                },
                {
                    "id": 3,
                    "name": "Legacy Workflow",
                    "engine_type": "legacy_workflow",
                    "is_active": True,
                    "capabilities": {"compute": [{"dev_modes": ["workflow"]}]},
                },
            ],
            "total": 3,
        })

    client = SystemClient("http://system")
    client._client = httpx.AsyncClient(
        base_url="http://system",
        transport=httpx.MockTransport(handler),
    )

    try:
        engines = await client.get_workflow_engines()
    finally:
        await client.close()

    assert engines == [{
        "id": 1,
        "name": "GeoPython Workflow",
        "engine_type": "geopython_workflow",
        "is_active": True,
        "connection_status": "online",
    }]


def test_supports_workflow_uses_structured_compute_schema():
    capabilities = {
        "schema_version": "engine.capabilities/v1",
        "engine_type": "geopython_workflow",
        "engine_family": "workflow",
        "compute": {
            "workflow": {
                "supported": True,
                "runtime_api": "addp.workflow/v1",
                "dynamic_operators": True,
            }
        },
    }

    assert SystemClient._supports_workflow(capabilities)


def test_supports_workflow_rejects_legacy_dev_modes_schema():
    legacy_capabilities = {
        "compute": [
            {
                "dev_modes": ["workflow"],
            }
        ]
    }

    assert not SystemClient._supports_workflow(legacy_capabilities)


def test_supports_workflow_rejects_non_workflow_compute_schema():
    capabilities = {
        "schema_version": "engine.capabilities/v1",
        "engine_type": "postgresql",
        "engine_family": "tabular",
        "compute": {
            "query": {
                "supported": True,
                "languages": ["sql"],
            }
        },
    }

    assert not SystemClient._supports_workflow(capabilities)
