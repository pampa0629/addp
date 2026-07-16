import unittest
from unittest.mock import AsyncMock, patch

from addp_common.auth import AuthorizationContext, resolve_authorization_context


class _SystemClient:
    response = None

    def __init__(self, **_kwargs):
        self.get_authorization_context = AsyncMock(return_value=self.response)

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args):
        return None


class AuthorizationContextTests(unittest.IsolatedAsyncioTestCase):
    async def test_resolves_canonical_user_and_tenant_context(self):
        _SystemClient.response = {
            "subject_type": "user",
            "user_id": 12,
            "username": "alice",
            "user_type": "tenant_admin",
            "tenant_id": 3,
            "auth_type": "first_party_access_token",
            "client_id": None,
            "audiences": [],
            "scopes": [],
            "delegated_by": None,
            "agent_run_id": None,
            "tool_call_id": None,
            "issued_at": "2026-07-16T08:00:00Z",
            "expires_at": "2026-07-16T08:15:00Z",
        }

        with patch("addp_common.auth.SystemClient", _SystemClient):
            context = await resolve_authorization_context("http://system", "user-token")

        self.assertIsInstance(context, AuthorizationContext)
        self.assertEqual(context.user_id, 12)
        self.assertEqual(context.tenant_id, 3)
        self.assertEqual(context.user_type, "tenant_admin")
        self.assertEqual(context.scopes, ())

    async def test_rejects_tenant_user_without_tenant(self):
        _SystemClient.response = {
            "subject_type": "user",
            "user_id": 12,
            "username": "alice",
            "user_type": "user",
            "tenant_id": None,
            "auth_type": "first_party_access_token",
            "audiences": [],
            "scopes": [],
            "issued_at": "2026-07-16T08:00:00Z",
            "expires_at": "2026-07-16T08:15:00Z",
        }

        with patch("addp_common.auth.SystemClient", _SystemClient):
            with self.assertRaisesRegex(ValueError, "must contain tenant_id"):
                await resolve_authorization_context("http://system", "user-token")


if __name__ == "__main__":
    unittest.main()
