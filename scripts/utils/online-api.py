"""Shared JSON owner-API transport and dedicated User identity checks for Online suites."""

from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request
import time
from dataclasses import dataclass
from datetime import datetime
from typing import Iterable


class SuiteError(RuntimeError):
    pass


@dataclass
class Response:
    status: int
    payload: dict[str, object] | list[object]


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
        *,
        response_type: type[dict] | type[list] = dict,
    ) -> Response:
        if response_type not in {dict, list}:
            raise SuiteError("unsupported JSON response type")
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
            with error:
                status = error.code
                raw = error.read()
        except (urllib.error.URLError, TimeoutError) as error:
            raise SuiteError(f"{method} {path} transport failed: {error}") from error
        try:
            payload = json.loads(raw) if raw else {}
        except json.JSONDecodeError as error:
            raise SuiteError(f"{method} {path} returned invalid JSON") from error
        if status not in set(expected):
            code = payload.get("error_code", "unknown") if isinstance(payload, dict) else "unknown"
            raise SuiteError(f"{method} {path} returned HTTP {status} ({code})")
        if not isinstance(payload, response_type):
            shape = "object" if response_type is dict else "array"
            raise SuiteError(f"{method} {path} response must be a JSON {shape}")
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


def wait_manager_read_boundary(client: GatewayClient, engine_id: int, deadline: float) -> None:
    """Observe initial Security synchronization without mutating owner storage."""
    while time.monotonic() < deadline:
        response = client.request("GET", "/api/v1/manager/engines", (200, 403))
        payload = response.payload
        if not isinstance(payload, dict):
            raise SuiteError("Manager engine readiness returned an invalid response")
        if response.status == 403:
            if payload.get("error_code") != "security_protection_required":
                raise SuiteError("Manager engine readiness was denied by authorization")
        else:
            engines = payload.get("data")
            if not isinstance(engines, list) or sum(
                isinstance(engine, dict) and engine.get("id") == engine_id for engine in engines
            ) != 1:
                raise SuiteError("Manager engine readiness did not return the exact target Engine")
            return
        time.sleep(1)
    raise SuiteError("Manager initial Security protection synchronization did not become ready")


def read_meta_scan_boundary(client, engine_id, source):
    """Freeze sibling facts and ancestor scan state through the canonical read APIs."""
    def _object(value, resource):
        if not isinstance(value, dict):
            raise SuiteError(resource + " response must be an object")
        return value

    source_id = require_positive_int(source, "id")
    tenant_id = require_positive_int(source, "tenant_id")
    parent_id = require_positive_int(source, "node_id")
    sibling = _object(client.request("GET", f"/api/v1/meta/items/{source_id}", (200,)).payload, "scan boundary sibling")
    if (sibling.get("id") != source_id or sibling.get("engine_id") != engine_id
            or sibling.get("tenant_id") != tenant_id or sibling.get("node_id") != parent_id
            or sibling.get("full_name") != source.get("full_name") or sibling.get("item_type") != "table"
            or not sibling.get("fingerprint")):
        raise SuiteError("scan boundary sibling identity changed")

    def scan_state(item, fields):
        stamp = item.get("scanned_at")
        try:
            parsed = datetime.fromisoformat(stamp.replace("Z", "+00:00"))
            if parsed.tzinfo is None:
                raise ValueError("timezone required")
        except (AttributeError, TypeError, ValueError):
            raise SuiteError("scan boundary requires a completed baseline scan timestamp") from None
        if item.get("scanned_depth") not in {"basic", "deep"}:
            raise SuiteError("scan boundary requires a completed baseline scan depth")
        return {field: item.get(field) for field in fields.split()}

    result = {"sibling": scan_state(sibling, "id tenant_id engine_id node_id item_type full_name fingerprint row_count size_bytes data_updated_at scanned_at scanned_depth"),
              "ancestors": []}
    seen = set()
    while parent_id is not None:
        parent_id = require_positive_int({"id": parent_id}, "id")
        if parent_id in seen or len(seen) >= 128:
            raise SuiteError("scan boundary ancestor chain is cyclic or too deep")
        seen.add(parent_id)
        node = _object(client.request("GET", f"/api/v1/meta/nodes/{parent_id}", (200,)).payload, "scan boundary ancestor")
        if (node.get("id") != parent_id or node.get("engine_id") != engine_id
                or node.get("tenant_id") != tenant_id or not node.get("scan_status")):
            raise SuiteError("scan boundary ancestor identity changed")
        # Counts are derived from the current subtree; creating a target legitimately changes them.
        result["ancestors"].append(scan_state(node, "id tenant_id engine_id parent_node_id scan_status scanned_at scanned_depth"))
        parent_id = node.get("parent_node_id")
    return result


def wait_meta_scan_tick(items, timeout=30):
    """Cross the public DTO's second-precision timestamps before provoking a scan."""
    next_tick = max(datetime.fromisoformat(item['scanned_at'].replace('Z', '+00:00')).timestamp()
                    for item in items) + 1
    deadline = time.monotonic() + timeout
    while time.time() < next_tick:
        if time.monotonic() >= deadline:
            raise SuiteError('baseline scan timestamp exceeds the verification clock budget')
        time.sleep(.05)
