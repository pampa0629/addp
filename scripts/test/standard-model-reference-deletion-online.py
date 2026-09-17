#!/usr/bin/env python3
"""Accept Standard-to-Model reference deletion through Gateway owner APIs."""

from __future__ import annotations

import json
import os
import sys
from functools import partial

from importlib import import_module

_API = import_module("scripts.utils.online-api")
GatewayClient = _API.GatewayClient
SuiteError = _API.SuiteError
assert_tenant = _API.assert_tenant
cleanup_resource = _API.cleanup_resource
require_positive_int = _API.require_positive_int
Response = _API.Response
validate_online_user = _API.validate_user_identity



REQUIRED_USER_PERMISSIONS = {
    "standard.domain.create",
    "standard.domain.read",
    "standard.domain.delete",
    "model.entity.create",
    "model.entity.read",
    "model.entity.delete",
}
validate_user_identity = partial(
    validate_online_user, required_permissions=REQUIRED_USER_PERMISSIONS
)


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
