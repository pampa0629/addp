"""Shared JSON owner-API transport and dedicated User identity checks for Online suites."""

from __future__ import annotations

import json
import os
import re
import subprocess
import urllib.error
import urllib.parse
import urllib.request
import time
from dataclasses import dataclass
from datetime import datetime
from importlib import import_module
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


def _read_hosted_meta_scan_facts(tenant_id, engine_id, source_id):
    suite = os.environ.get("ONLINE_SUITE")
    engine_env = {
        "transfer-relational-sql-etl": "ADDP_ONLINE_TEST_ENGINE_ID",
        "transfer-continuous-events": "ADDP_ONLINE_CONTINUOUS_PG_ENGINE_ID",
    }.get(suite)
    container_id = os.environ.get("ADDP_ONLINE_SCAN_POSTGRES_ID", "")
    if (any(type(value) is not int or value <= 0 for value in (tenant_id, engine_id, source_id))
            or engine_env is None or os.environ.get(engine_env) != str(engine_id)
            or tenant_id <= 1 or os.environ.get("ADDP_ONLINE_TEST_TENANT_ID") != str(tenant_id)
            or not re.fullmatch(r"[0-9a-f]{64}", container_id)):
        raise SuiteError("scan state requires the exact Hosted fixture identities")
    postgres = import_module("scripts.test.execution-retention-readonly-online")
    # The identifiers above are exact positive integers from the formal catalog.
    sql = f"""BEGIN READ ONLY;
SET LOCAL statement_timeout='3s'; SET LOCAL lock_timeout='1s';
WITH RECURSIVE sibling AS (
  SELECT id, tenant_id, engine_id, node_id, item_type, full_name, fingerprint,
         row_count, size_bytes, data_updated_at, scanned_at, scanned_depth
  FROM meta.meta_item
  WHERE id = {source_id} AND tenant_id = {tenant_id} AND engine_id = {engine_id}
    AND item_type = 'table' AND deleted_at IS NULL
), ancestors AS (
  SELECT n.id, n.tenant_id, n.engine_id, n.parent_node_id, n.scan_status,
         n.scanned_at, n.scanned_depth, 1 AS level
  FROM meta.meta_node n JOIN sibling s ON n.id = s.node_id
    AND n.tenant_id = s.tenant_id AND n.engine_id = s.engine_id
  WHERE n.deleted_at IS NULL
  UNION ALL
  SELECT n.id, n.tenant_id, n.engine_id, n.parent_node_id, n.scan_status,
         n.scanned_at, n.scanned_depth, a.level + 1
  FROM meta.meta_node n JOIN ancestors a ON n.id = a.parent_node_id
    AND n.tenant_id = a.tenant_id AND n.engine_id = a.engine_id
  WHERE n.deleted_at IS NULL AND a.level < 128
)
SELECT jsonb_build_object('sibling', (SELECT to_jsonb(s) FROM sibling s),
  'ancestors', (SELECT jsonb_agg(to_jsonb(a) - 'level' ORDER BY a.level) FROM ancestors a));
COMMIT;
"""
    try:
        raw = postgres.sql(sql, expected_suite=suite, postgres_id=container_id)
        if 0 < len(raw.encode()) <= 65536:
            return json.loads(raw)
    except (KeyError, OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        pass
    raise SuiteError("bounded Hosted scan state read unavailable") from None


def read_meta_scan_boundary(client, engine_id, source):
    """Bind the visible identities to independent, read-only Hosted scan facts."""
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

    observed = _object(_read_hosted_meta_scan_facts(tenant_id, engine_id, source_id), "Hosted scan state")
    stored_sibling = _object(observed.get("sibling"), "Hosted scan sibling")
    identity = "id tenant_id engine_id node_id item_type full_name fingerprint".split()
    if any(stored_sibling.get(key) != sibling.get(key) for key in identity):
        raise SuiteError("Hosted scan sibling differs from the visible identity")
    result = {"sibling": scan_state(stored_sibling, "id tenant_id engine_id node_id item_type full_name fingerprint row_count size_bytes data_updated_at scanned_at scanned_depth"),
              "ancestors": []}
    ancestors = observed.get("ancestors")
    if not isinstance(ancestors, list) or not 1 <= len(ancestors) <= 128:
        raise SuiteError("Hosted scan ancestor chain is absent or too deep")
    seen = set()
    for stored_node in ancestors:
        parent_id = require_positive_int({"id": parent_id}, "id")
        if parent_id in seen:
            raise SuiteError("scan boundary ancestor chain is cyclic")
        seen.add(parent_id)
        node = _object(client.request("GET", f"/api/v1/meta/nodes/{parent_id}", (200,)).payload, "scan boundary ancestor")
        stored_node = _object(stored_node, "Hosted scan ancestor")
        if (node.get("id") != parent_id or node.get("engine_id") != engine_id
                or node.get("tenant_id") != tenant_id
                or any(stored_node.get(key) != node.get(key) for key in ("id", "tenant_id", "engine_id", "parent_node_id"))):
            raise SuiteError("scan boundary ancestor identity changed")
        if stored_node.get("scan_status") not in ("pending", "running", "completed", "failed"):
            raise SuiteError("Hosted scan ancestor has an invalid scan status")
        # Counts are derived from the current subtree; creating a target legitimately changes them.
        result["ancestors"].append(scan_state(stored_node, "id tenant_id engine_id parent_node_id scan_status scanned_at scanned_depth"))
        parent_id = node.get("parent_node_id")
    if parent_id is not None:
        raise SuiteError("Hosted scan ancestor chain is incomplete")
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
