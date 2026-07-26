import unittest
from unittest.mock import AsyncMock, patch

from addp_common.auth import (
    AuthorizationContext,
    allows_delegated_tool,
    allows_permissions,
    resolve_authorization_context,
)


class _SystemClient:
    response = None

    def __init__(self, **_kwargs):
        self.get_authorization_context = AsyncMock(return_value=self.response)

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args):
        return None


def _context_response(*, delegated: bool = False):
    return {
        "schema_version": "addp.auth_context/v1",
        "principal": {"type": "user", "id": "12"},
        "context": {"type": "tenant", "tenant_id": "3", "tenant_membership_id": "8"},
        "authentication": {
            "methods": ["password"],
            "assurance_level": "aal1",
            "authenticated_at": "2026-07-16T08:00:00Z",
            "step_up_expires_at": None,
        },
        "client": {
            "client_id": "addp-cli" if delegated else None,
            "audiences": ["develop"] if delegated else ["addp.api"],
            "scope_mode": "restricted" if delegated else "unrestricted",
            "scopes": ["workflow.validate"] if delegated else [],
        },
        "organization": {"departments": [], "project_groups": []},
        "authorization": {
            "authorization_version": "4",
            "role_assignments": [
                {
                    "assignment_id": "21",
                    "role_key": "tenant.data_engineer",
                    "scope": {"type": "tenant", "tenant_id": "3"},
                    "permissions": ["develop.task.read", "develop.task.execute"],
                    "source_type": "manual",
                    "valid_from": "2026-07-16T07:00:00Z",
                    "valid_until": None,
                }
            ],
        },
        "token": {
            "type": "delegated_access_token" if delegated else "first_party_access_token",
            "issued_at": "2026-07-16T08:00:00Z",
            "expires_at": "2026-07-16T08:15:00Z",
        },
        "delegation": (
            {"delegated_by_client_id": "addp-cli", "agent_run_id": "run-1", "tool_call_id": "call-1"}
            if delegated
            else None
        ),
    }


class AuthorizationContextTests(unittest.IsolatedAsyncioTestCase):
    async def test_resolves_canonical_auth_context_v1(self):
        _SystemClient.response = _context_response()

        with patch("addp_common.auth.SystemClient", _SystemClient):
            context = await resolve_authorization_context("http://system", "user-token")

        self.assertIsInstance(context, AuthorizationContext)
        self.assertEqual(context.principal_id, 12)
        self.assertEqual(context.tenant_id, 3)
        self.assertEqual(context.context_type, "tenant")
        self.assertEqual(context.permissions, ("develop.task.execute", "develop.task.read"))
        self.assertTrue(allows_permissions(context, ("develop.task.read",)))
        self.assertFalse(allows_permissions(context, ("develop.task.delete",)))

    async def test_rejects_tenant_context_without_required_ids(self):
        _SystemClient.response = _context_response()
        _SystemClient.response["context"] = {"type": "tenant"}

        with patch("addp_common.auth.SystemClient", _SystemClient):
            with self.assertRaisesRegex(ValueError, "context fields are invalid"):
                await resolve_authorization_context("http://system", "user-token")

    async def test_delegated_context_keeps_owner_scope_and_audit_binding(self):
        _SystemClient.response = _context_response(delegated=True)

        with patch("addp_common.auth.SystemClient", _SystemClient):
            context = await resolve_authorization_context("http://system", "addp_dat_test")

        self.assertTrue(allows_delegated_tool(context, "develop", ("workflow.validate",)))
        self.assertFalse(allows_delegated_tool(context, "manager", ("workflow.validate",)))
        self.assertEqual(context.delegated_by_client_id, "addp-cli")
        self.assertEqual(context.agent_run_id, "run-1")
        self.assertEqual(context.tool_call_id, "call-1")

    async def test_rejects_legacy_flat_context(self):
        _SystemClient.response = {"user_id": 12, "tenant_id": 3, "user_type": "user"}

        with patch("addp_common.auth.SystemClient", _SystemClient):
            with self.assertRaisesRegex(ValueError, "root fields are invalid"):
                await resolve_authorization_context("http://system", "user-token")


if __name__ == "__main__":
    unittest.main()
