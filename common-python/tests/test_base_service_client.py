import json

import httpx
import pytest

from addp_common.client import BaseClient, OAuthServiceTokenSource


@pytest.mark.asyncio
async def test_tenant_service_client_refreshes_rejected_token_once():
    token_requests = 0
    owner_requests = 0

    async def system_handler(_request):
        nonlocal token_requests
        token_requests += 1
        return httpx.Response(200, json={
            "access_token": f"addp_at_token-{token_requests}",
            "token_type": "Bearer",
            "expires_in": 120,
            "scope": "addp.api",
        })

    async def owner_handler(request):
        nonlocal owner_requests
        owner_requests += 1
        assert "X-Internal-API-Key" not in request.headers
        assert "X-Tenant-ID" not in request.headers
        if request.headers["Authorization"] == "Bearer addp_at_token-1":
            return httpx.Response(401, json={"error_code": "unauthorized"})
        assert request.headers["Authorization"] == "Bearer addp_at_token-2"
        assert json.loads(request.content) == {"value": 1}
        return httpx.Response(200, json={"ok": True})

    token_source = OAuthServiceTokenSource(
        "http://system",
        "addp-copilot",
        "test-service-client-secret-32bytes",
        transport=httpx.MockTransport(system_handler),
    )
    client = BaseClient(
        "http://owner",
        tenant_id=7,
        service_token_source=token_source,
    )
    await client._client.aclose()
    client._client = httpx.AsyncClient(
        base_url="http://owner",
        transport=httpx.MockTransport(owner_handler),
    )
    try:
        response = await client.post("/resource", json={"value": 1})
    finally:
        await client.close()
        await token_source.close()

    assert response == {"ok": True}
    assert token_requests == 2
    assert owner_requests == 2


def test_tenant_service_client_rejects_ambiguous_credentials():
    token_source = object()
    with pytest.raises(ValueError, match="cannot be combined"):
        BaseClient(
            "http://owner",
            user_token="addp_at_user_token",
            tenant_id=7,
            service_token_source=token_source,
        )
