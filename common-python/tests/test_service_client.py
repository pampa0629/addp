import json
import subprocess
import sys

import httpx
import pytest

from addp_common.client import (
    ConsumerDescriptor,
    QueryPageRequest,
    ServiceConsumerAPIError,
    ServiceConsumerClient,
    ServiceConsumerContractError,
    ServiceReference,
    StructuredQueryRequest,
)


FINGERPRINT = "sha256:" + "a" * 64


def descriptor_payload(
    *,
    fingerprint: str = FINGERPRINT,
    operation_path: str = "/api/query/generic_records/query",
    output_kind: str = "tabular",
) -> dict:
    output_contract = {
        "kind": output_kind,
        "fields": [
            {"name": "record_key", "type": "uuid", "nullable": False},
            {"name": "measure", "type": "double", "nullable": True},
        ],
    }
    if output_kind == "spatial_tabular":
        output_contract["fields"].append(
            {"name": "footprint_json", "type": "geometry", "nullable": False},
        )
        output_contract["spatial"] = {
            "primary_geometry_field": "footprint_json",
            "srid": 4490,
            "crs_ref": "EPSG:4490",
            "geometry_fields": [
                {
                    "name": "footprint_json",
                    "geometry_type": "MultiPolygon",
                    "srid": 4490,
                    "crs_ref": "EPSG:4490",
                    "dimension": 2,
                },
            ],
        }
    return {
        "schema_version": "addp.service_consumer/v1",
        "ref": {"service_type": "query", "service_id": 41},
        "title": "Generic records",
        "description": "Contract fixture without domain assumptions",
        "status": "active",
        "access_mode": "private",
        "contract_fingerprint": fingerprint,
        "operations": [
            {
                "key": "query",
                "method": "POST",
                "path": operation_path,
                "input_kind": "structured_query",
                "output_kind": output_kind,
            },
        ],
        "input_contract": {
            "kind": "structured_query",
            "named_parameters": [
                {
                    "name": "minimum_measure",
                    "type": "double",
                    "required": False,
                    "description": "Minimum measure",
                    "default": 0,
                },
            ],
            "fields": [
                {
                    "name": "record_key",
                    "type": "uuid",
                    "nullable": False,
                    "selectable": True,
                    "filterable": True,
                    "operators": ["eq", "in"],
                    "sortable": True,
                },
                {
                    "name": "measure",
                    "type": "double",
                    "nullable": True,
                    "selectable": True,
                    "filterable": True,
                    "operators": ["eq", "gte", "lte"],
                    "sortable": True,
                },
            ],
            "default_selection": ["record_key", "measure"],
            "filter": {
                "combinators": ["and", "or", "not"],
                "max_depth": 16,
                "max_nodes": 256,
                "max_in_values": 1000,
            },
            "order": {"directions": ["asc", "desc"], "stable_key": ["record_key"]},
            "page": {"kind": "cursor", "default_limit": 50, "max_limit": 1000},
            "formats": ["json", "csv"],
            "intent": {
                "header": "X-ADDP-Query-Intent",
                "allowed_values": ["query", "export"],
                "default_value": "query",
            },
        },
        "output_contract": output_contract,
    }


@pytest.mark.asyncio
async def test_lists_catalog_and_freezes_descriptor_fingerprint():
    requests = []

    async def handler(request):
        requests.append(request)
        assert request.headers["Authorization"] == "Bearer user-access-token"
        if request.url.path == "/api/v1/service/consumer/services":
            assert dict(request.url.params) == {
                "page": "2",
                "page_size": "25",
                "search": "records",
                "service_type": "query",
                "output_kind": "tabular",
            }
            return httpx.Response(
                200,
                json={
                    "data": [
                        {
                            "ref": {"service_type": "query", "service_id": 41},
                            "title": "Generic records",
                            "description": "Contract fixture",
                            "access_mode": "private",
                            "output_kind": "tabular",
                            "contract_fingerprint": FINGERPRINT,
                        },
                    ],
                    "total": 26,
                    "page": 2,
                    "page_size": 25,
                    "total_pages": 2,
                },
            )
        assert request.url.path == "/api/v1/service/consumer/services/query/41"
        return httpx.Response(200, json=descriptor_payload())

    client = ServiceConsumerClient(
        "http://gateway",
        user_token="user-access-token",
        transport=httpx.MockTransport(handler),
    )
    try:
        page = await client.list_services(
            search="records",
            service_type="query",
            output_kind="tabular",
            page=2,
            page_size=25,
        )
        descriptor = await client.get_descriptor(
            page.data[0].ref,
            expected_contract_fingerprint=page.data[0].contract_fingerprint,
        )
    finally:
        await client.close()

    assert descriptor.ref.service_id == 41
    assert len(requests) == 2


@pytest.mark.asyncio
async def test_descriptor_rejects_changed_fingerprint_and_cross_origin_operation():
    responses = [
        descriptor_payload(fingerprint="sha256:" + "b" * 64),
        descriptor_payload(operation_path="https://invalid.example/query"),
    ]

    async def handler(_request):
        return httpx.Response(200, json=responses.pop(0))

    client = ServiceConsumerClient(
        "http://gateway",
        user_token="user-access-token",
        transport=httpx.MockTransport(handler),
    )
    ref = ServiceReference(service_type="query", service_id=41)
    try:
        with pytest.raises(ServiceConsumerContractError, match="fingerprint changed"):
            await client.get_descriptor(ref, expected_contract_fingerprint=FINGERPRINT)
        with pytest.raises(ServiceConsumerContractError, match="invalid query operation"):
            await client.get_descriptor(ref, expected_contract_fingerprint=FINGERPRINT)
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_executes_only_descriptor_query_operation_with_canonical_body():
    descriptor = ConsumerDescriptor.model_validate(descriptor_payload())

    async def handler(request):
        assert request.url.path == "/api/query/generic_records/query"
        assert request.method == "POST"
        assert request.headers["Authorization"] == "Bearer user-access-token"
        assert request.headers["X-ADDP-Query-Intent"] == "query"
        assert json.loads(request.content) == {
            "parameters": {"minimum_measure": 10.5},
            "select": ["record_key", "measure"],
            "filter": {"field": "measure", "op": "gte", "value": 10.5},
            "order_by": [{"field": "record_key", "direction": "asc"}],
            "page": {"limit": 2},
            "format": "json",
        }
        return httpx.Response(
            200,
            json={
                "data": [{"record_key": "r-1", "measure": 12.5}],
                "page": {"limit": 2, "has_more": False},
                "service_version": "revision-3",
            },
        )

    client = ServiceConsumerClient(
        "http://gateway",
        user_token="user-access-token",
        transport=httpx.MockTransport(handler),
    )
    request = StructuredQueryRequest(
        parameters={"minimum_measure": 10.5},
        select=["record_key", "measure"],
        filter={"field": "measure", "op": "gte", "value": 10.5},
        order_by=[{"field": "record_key", "direction": "asc"}],
        page=QueryPageRequest(limit=2),
    )
    try:
        result = await client.execute_query(descriptor, request)
    finally:
        await client.close()

    assert result.data == [{"record_key": "r-1", "measure": 12.5}]
    assert result.service_version == "revision-3"


@pytest.mark.asyncio
async def test_iterates_query_pages_with_opaque_cursor_and_stable_service_version():
    descriptor = ConsumerDescriptor.model_validate(descriptor_payload())
    cursors = []

    async def handler(request):
        body = json.loads(request.content)
        cursor = body["page"].get("cursor", "")
        cursors.append(cursor)
        if not cursor:
            return httpx.Response(
                200,
                json={
                    "data": [{"record_key": "r-1", "measure": 1}],
                    "page": {"limit": 1, "has_more": True, "next_cursor": "opaque-A"},
                    "service_version": "revision-7",
                },
            )
        assert cursor == "opaque-A"
        return httpx.Response(
            200,
            json={
                "data": [{"record_key": "r-2", "measure": 2}],
                "page": {"limit": 1, "has_more": False},
                "service_version": "revision-7",
            },
        )

    client = ServiceConsumerClient(
        "http://gateway",
        user_token="user-access-token",
        transport=httpx.MockTransport(handler),
    )
    pages = []
    try:
        async for page in client.iter_query_pages(
            descriptor,
            StructuredQueryRequest(page=QueryPageRequest(limit=1)),
        ):
            pages.append(page)
    finally:
        await client.close()

    assert cursors == ["", "opaque-A"]
    assert [row["record_key"] for page in pages for row in page.data] == ["r-1", "r-2"]


@pytest.mark.asyncio
async def test_cli_session_refreshes_once_after_unauthorized(monkeypatch):
    loaded_tokens = []

    async def load_token(_base_url):
        token = f"user-token-{len(loaded_tokens) + 1}"
        loaded_tokens.append(token)
        return token

    async def handler(request):
        token = request.headers["Authorization"]
        if token == "Bearer user-token-1":
            return httpx.Response(401, json={"error": "expired", "error_code": "unauthorized"})
        assert token == "Bearer user-token-2"
        return httpx.Response(
            200,
            json={"data": [], "total": 0, "page": 1, "page_size": 20, "total_pages": 0},
        )

    monkeypatch.setattr("addp_common.oauth.refresh_access_token", load_token)
    client = ServiceConsumerClient.from_cli_session(
        "http://gateway",
        transport=httpx.MockTransport(handler),
    )
    try:
        result = await client.list_services()
    finally:
        await client.close()

    assert result.total == 0
    assert loaded_tokens == ["user-token-1", "user-token-2"]


@pytest.mark.asyncio
async def test_preserves_stable_service_api_error():
    async def handler(_request):
        return httpx.Response(403, json={"error": "Forbidden", "error_code": "permission_denied"})

    client = ServiceConsumerClient(
        "http://gateway",
        user_token="user-access-token",
        transport=httpx.MockTransport(handler),
    )
    try:
        with pytest.raises(ServiceConsumerAPIError) as captured:
            await client.list_services()
    finally:
        await client.close()

    assert captured.value.status_code == 403
    assert captured.value.error_code == "permission_denied"


def test_accepts_spatial_contract_without_conventional_geometry_field_name():
    descriptor = ConsumerDescriptor.model_validate(descriptor_payload(output_kind="spatial_tabular"))
    assert descriptor.output_contract.spatial is not None
    assert descriptor.output_contract.spatial.primary_geometry_field == "footprint_json"


def test_rejects_query_operation_path_traversal():
    descriptor = ConsumerDescriptor.model_validate(
        descriptor_payload(operation_path="/api/query/../query"),
    )
    with pytest.raises(ServiceConsumerContractError, match="invalid query operation"):
        descriptor.query_operation()


def test_rejects_ambiguous_or_missing_authentication_source():
    with pytest.raises(ValueError, match="exactly one"):
        ServiceConsumerClient("http://gateway")

    async def load_token():
        return "user-access-token"

    with pytest.raises(ValueError, match="exactly one"):
        ServiceConsumerClient(
            "http://gateway",
            user_token="user-access-token",
            _token_loader=load_token,
        )


def test_common_client_import_does_not_require_desktop_oauth_dependencies():
    script = """
import sys

class DesktopOAuthDependencyBlocker:
    def find_spec(self, fullname, path=None, target=None):
        if fullname in {"addp_common.oauth", "filelock", "keyring"}:
            raise ModuleNotFoundError(fullname)
        return None

sys.meta_path.insert(0, DesktopOAuthDependencyBlocker())
from addp_common.client import CopilotClient
assert CopilotClient.__name__ == "CopilotClient"
"""
    result = subprocess.run(
        [sys.executable, "-c", script],
        check=False,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stderr
