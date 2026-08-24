#!/usr/bin/env python3
"""Accept Standard-to-Model reference deletion through Gateway owner APIs."""

from __future__ import annotations

import json
import os
import sys
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


REQUIRED_USER_PERMISSIONS = {
    "standard.domain.create",
    "standard.domain.read",
    "standard.domain.delete",
    "model.entity.create",
    "model.entity.read",
    "model.entity.delete",
}
FORBIDDEN_ADMIN_ROLES = {
    "platform.audit_administrator",
    "platform.security_administrator",
    "platform.system_administrator",
    "tenant.administrator",
}


def validate_user_identity(
    system: GatewayClient, tenant_id: int
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
    missing = REQUIRED_USER_PERMISSIONS - permissions
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
        "permissions_verified": sorted(REQUIRED_USER_PERMISSIONS),
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


def run_suite(client: GatewayClient, tenant_id: int, run_id: str) -> dict[str, object]:
    suffix = run_id.replace("-", "_").replace(".", "_")
    domain_path: str | None = None
    entity_path: str | None = None
    cleanup_errors: list[str] = []
    created = 0
    deleted = 0
    try:
        domain = client.request(
            "POST",
            "/api/v1/standard/domains",
            (201,),
            {
                "name": f"Online domain {run_id}",
                "code": f"online_domain_{suffix}",
                "description": f"ADDP Online run {run_id}",
                "sort_order": 0,
            },
        ).payload
        domain_id = require_positive_int(domain, "id")
        domain_version = require_positive_int(domain, "version")
        domain_path = f"/api/v1/standard/domains/{domain_id}"
        created += 1
        assert_tenant(domain, tenant_id, "Standard Domain")

        entity = client.request(
            "POST",
            "/api/v1/model/entities",
            (201,),
            {
                "domain_id": domain_id,
                "name": f"Online entity {run_id}",
                "code": f"online_entity_{suffix}",
                "description": f"ADDP Online run {run_id}",
            },
        ).payload
        entity_id = require_positive_int(entity, "id")
        entity_version = require_positive_int(entity, "version")
        entity_path = f"/api/v1/model/entities/{entity_id}"
        created += 1
        assert_tenant(entity, tenant_id, "Model Entity")

        blocked = client.request(
            "DELETE", domain_path, (409,), {"version": domain_version}
        ).payload
        if blocked.get("error_code") != "standard_resource_referenced":
            raise SuiteError("Standard deletion did not return standard_resource_referenced")
        if require_positive_int(blocked, "reference_count") < 1:
            raise SuiteError("Standard deletion did not report a Model reference")
        current_domain = client.request("GET", domain_path, (200,)).payload
        if current_domain.get("lifecycle_state") != "active":
            raise SuiteError("blocked Standard Domain did not return to active state")

        client.request("DELETE", entity_path, (200,), {"version": entity_version})
        deleted += 1
        entity_path = None
        client.request("DELETE", domain_path, (200,), {"version": domain_version})
        deleted += 1
        domain_path = None
        client.request("GET", f"/api/v1/model/entities/{entity_id}", (404,))
        client.request("GET", f"/api/v1/standard/domains/{domain_id}", (404,))
        return {
            "schema_version": "addp.online-suite/v1",
            "suite": "standard-model-reference-deletion",
            "run_id": run_id,
            "tenant_id": str(tenant_id),
            "created_resources": created,
            "deleted_resources": deleted,
            "residual_resources": 0,
            "cleanup": "passed",
        }
    finally:
        for resource, path in (("Model Entity", entity_path), ("Standard Domain", domain_path)):
            if path is None:
                continue
            try:
                cleanup_resource(client, path)
            except Exception as error:  # cleanup evidence must preserve every failure
                cleanup_errors.append(f"{resource}: {error}")
        if cleanup_errors:
            raise SuiteError("cleanup failed: " + "; ".join(cleanup_errors))


def main() -> int:
    try:
        if os.environ.get("ADDP_ONLINE_TEST") != "1":
            raise SuiteError("ADDP_ONLINE_TEST must be exactly 1")
        tenant_id = int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"])
        run_id = os.environ["ADDP_ONLINE_TEST_RUN_ID"]
        token = os.environ.get("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN", "")
        if not token:
            raise SuiteError("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN is required")
        timeout = float(os.environ.get("ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS", "10"))
        if timeout <= 0:
            raise SuiteError("ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS must be greater than zero")
        identity = validate_user_identity(
            GatewayClient(os.environ["SYSTEM_URL"], token, timeout, "SYSTEM_URL"),
            tenant_id,
        )
        report = run_suite(
            GatewayClient(os.environ["GATEWAY_URL"], token, timeout),
            tenant_id,
            run_id,
        )
        report["identity"] = identity
    except (KeyError, ValueError, SuiteError) as error:
        print(f"Standard-Model Online suite failed: {error}", file=sys.stderr)
        return 1
    json.dump(report, sys.stdout, ensure_ascii=False, sort_keys=True)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
