import unittest
from unittest.mock import AsyncMock, patch

import httpx
from starlette.requests import Request
from starlette.responses import JSONResponse

from addp_common.auth import AuthorizationContext
from middleware.auth import auth_middleware


def _request(token: str | None = "user-token") -> Request:
    headers = []
    if token is not None:
        headers.append((b"authorization", f"Bearer {token}".encode()))
    return Request({
        "type": "http",
        "http_version": "1.1",
        "method": "GET",
        "scheme": "http",
        "path": "/api/v1/agent/sessions",
        "raw_path": b"/api/v1/agent/sessions",
        "query_string": b"",
        "headers": headers,
        "client": ("127.0.0.1", 1),
        "server": ("test", 80),
    })


async def _next(_request):
    return JSONResponse({"ok": True})


class AgentAuthMiddlewareTests(unittest.IsolatedAsyncioTestCase):
    async def test_uses_system_authorization_context(self):
        context = AuthorizationContext(
            user_id=12,
            tenant_id=3,
            username="alice",
            user_type="tenant_admin",
        )
        resolver = AsyncMock(return_value=context)
        request = _request()

        with patch("middleware.auth.resolve_authorization_context", resolver):
            response = await auth_middleware(request, _next)

        self.assertEqual(response.status_code, 200)
        self.assertEqual(request.state.user_id, 12)
        self.assertEqual(request.state.tenant_id, 3)
        self.assertEqual(request.state.authorization_context, context)
        resolver.assert_awaited_once()

    async def test_returns_unauthorized_for_rejected_token(self):
        error = httpx.HTTPStatusError(
            "unauthorized",
            request=httpx.Request("GET", "http://system/api/v1/system/auth/context"),
            response=httpx.Response(401),
        )
        request = _request("bad-token")

        with patch(
            "middleware.auth.resolve_authorization_context",
            AsyncMock(side_effect=error),
        ):
            response = await auth_middleware(request, _next)

        self.assertEqual(response.status_code, 401)

    async def test_returns_service_unavailable_for_invalid_system_response(self):
        request = _request()

        with patch(
            "middleware.auth.resolve_authorization_context",
            AsyncMock(side_effect=ValueError("invalid context")),
        ):
            response = await auth_middleware(request, _next)

        self.assertEqual(response.status_code, 503)


if __name__ == "__main__":
    unittest.main()
