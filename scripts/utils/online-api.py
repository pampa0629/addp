"""Shared JSON owner-API transport and dedicated User identity checks for Online suites."""

from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from typing import Iterable


class SuiteError(RuntimeError):
    pass


@dataclass
class Response:
    status: int
    payload: dict[str, object]


class GatewayClient:
    def __init__(self, base_url: str, token: str, timeout: float, name: str = "GATEWAY_URL") -> None:
        parsed = urllib.parse.urlsplit(base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise SuiteError(f"{name} must be an absolute HTTP(S) URL")
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.timeout = timeout

    def request(
        self,
        method: str,
        path: str,
        expected: Iterable[int],
        body: dict[str, object] | None = None,
    ) -> Response:
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(
            self.base_url + path,
            data=data,
            method=method,
            headers={
                "Accept": "application/json",
                "Content-Type": "application/json",
                "Authorization": f"Bearer {self.token}",
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                status = response.status
                raw = response.read()
        except urllib.error.HTTPError as error:
            status = error.code
            raw = error.read()
        except (urllib.error.URLError, TimeoutError) as error:
            raise SuiteError(f"{method} {path} transport failed: {error}") from error
        try:
            payload = json.loads(raw) if raw else {}
        except json.JSONDecodeError as error:
            raise SuiteError(f"{method} {path} returned invalid JSON") from error
        if not isinstance(payload, dict):
            raise SuiteError(f"{method} {path} response must be a JSON object")
        if status not in set(expected):
            code = payload.get("error_code", "unknown")
            raise SuiteError(f"{method} {path} returned HTTP {status} ({code})")
        return Response(status=status, payload=payload)


FORBIDDEN_ADMIN_ROLES = {
    "platform.audit_administrator",
    "platform.security_administrator",
    "platform.system_administrator",
    "tenant.administrator",
}


def validate_user_identity(
    system: GatewayClient, tenant_id: int, required_permissions: set[str]
) -> dict[str, object]:
    payload = system.request("GET", "/api/v1/system/auth/context", (200,)).payload
    principal = payload.get("principal")
    context = payload.get("context")
    token = payload.get("token")
    authorization = payload.get("authorization")
    if not isinstance(principal, dict) or principal.get("type") != "user":
        raise SuiteError("Online business token must belong to a User")
    if not isinstance(context, dict) or context.get("type") != "tenant":
        raise SuiteError("Online business token must use Tenant Context")
    if context.get("tenant_id") != str(tenant_id):
        raise SuiteError("Online business token Tenant does not match ADDP_ONLINE_TEST_TENANT_ID")
    if not isinstance(token, dict) or token.get("type") not in {
        "first_party_access_token",
        "oauth_access_token",
    }:
        raise SuiteError("Online business token must be a User Access Token")
    if not isinstance(authorization, dict):
        raise SuiteError("System AuthContext authorization must be an object")
    assignments = authorization.get("role_assignments")
    if not isinstance(assignments, list):
        raise SuiteError("System AuthContext role_assignments must be an array")
    roles: set[str] = set()
    permissions: set[str] = set()
    for assignment in assignments:
        if not isinstance(assignment, dict):
            raise SuiteError("System AuthContext role assignment must be an object")
        role = assignment.get("role_key")
        assigned_permissions = assignment.get("permissions")
        if not isinstance(role, str) or not isinstance(assigned_permissions, list):
            raise SuiteError("System AuthContext role assignment is incomplete")
        if not all(isinstance(permission, str) for permission in assigned_permissions):
            raise SuiteError("System AuthContext permissions must contain strings")
        roles.add(role)
        permissions.update(assigned_permissions)
    forbidden = roles & FORBIDDEN_ADMIN_ROLES
    if forbidden:
        raise SuiteError(
            "Online business token must not use administrator roles: "
            + ", ".join(sorted(forbidden))
        )
    missing = required_permissions - permissions
    if missing:
        raise SuiteError(
            "Online business token is missing required permissions: "
            + ", ".join(sorted(missing))
        )
    return {
        "principal_type": "user",
        "context_type": "tenant",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(required_permissions),
    }


def require_positive_int(payload: dict[str, object], field: str) -> int:
    value = payload.get(field)
    if not isinstance(value, int) or isinstance(value, bool) or value <= 0:
        raise SuiteError(f"response {field} must be a positive integer")
    return value


def assert_tenant(payload: dict[str, object], tenant_id: int, resource: str) -> None:
    if payload.get("tenant_id") != tenant_id:
        raise SuiteError(f"{resource} was not created in the configured Tenant")


def cleanup_resource(client: GatewayClient, path: str) -> bool:
    current = client.request("GET", path, (200, 404))
    if current.status == 404:
        return False
    version = require_positive_int(current.payload, "version")
    client.request("DELETE", path, (200,), {"version": version})
    remaining = client.request("GET", path, (404,))
    return remaining.status == 404


