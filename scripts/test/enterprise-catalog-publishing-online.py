#!/usr/bin/env python3
"""Accept the unique Meta -> Catalog -> Asset -> Portal route through Gateway APIs."""

from __future__ import annotations

import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from typing import Any, Iterable


FIXTURE_TABLE = "addp_online_catalog_fixture"
TERMINAL_EXECUTION_STATUSES = {"success", "failed", "timeout", "cancelled"}


class SuiteError(RuntimeError):
    pass


@dataclass
class Response:
    status: int
    payload: Any


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
        if status not in set(expected):
            code = payload.get("error_code", "unknown") if isinstance(payload, dict) else "unknown"
            raise SuiteError(f"{method} {path} returned HTTP {status} ({code})")
        return Response(status=status, payload=payload)


REQUIRED_PERMISSIONS = {
    "meta.catalog.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
    "catalog.entry.read",
    "catalog.entry.update",
    "catalog.inventory.read",
    "asset.management.read",
    "asset.catalog.create",
    "asset.catalog.delete",
    "asset.catalog.read",
    "asset.entry.delete",
    "asset.entry.offline",
    "asset.entry.publish",
    "asset.entry.read",
    "asset.entry.update",
}


def _object(payload: Any, resource: str) -> dict[str, object]:
    if not isinstance(payload, dict):
        raise SuiteError(f"{resource} response must be an object")
    return payload


def _array(payload: Any, resource: str) -> list[object]:
    if not isinstance(payload, list):
        raise SuiteError(f"{resource} response must be an array")
    return payload


def positive_int(value: object, field: str) -> int:
    if isinstance(value, bool):
        raise SuiteError(f"{field} must be a positive integer")
    try:
        parsed = int(value)
    except (TypeError, ValueError) as error:
        raise SuiteError(f"{field} must be a positive integer") from error
    if parsed <= 0 or str(parsed) != str(value):
        raise SuiteError(f"{field} must be a canonical positive integer")
    return parsed


def validate_user_identity(system: GatewayClient, tenant_id: int) -> dict[str, object]:
    payload = _object(system.request("GET", "/api/v1/system/auth/context", (200,)).payload, "AuthContext")
    principal = _object(payload.get("principal"), "AuthContext principal")
    context = _object(payload.get("context"), "AuthContext context")
    token = _object(payload.get("token"), "AuthContext token")
    authorization = _object(payload.get("authorization"), "AuthContext authorization")
    if principal.get("type") != "user":
        raise SuiteError("Online catalog token must belong to a User")
    principal_id = positive_int(principal.get("id"), "AuthContext principal.id")
    if context.get("type") != "tenant" or context.get("tenant_id") != str(tenant_id):
        raise SuiteError("Online catalog token must use the configured Tenant Context")
    if token.get("type") not in {"first_party_access_token", "oauth_access_token"}:
        raise SuiteError("Online catalog token must be a User Access Token")
    assignments = authorization.get("role_assignments")
    if not isinstance(assignments, list):
        raise SuiteError("AuthContext role_assignments must be an array")
    roles: set[str] = set()
    permissions: set[str] = set()
    for assignment in assignments:
        item = _object(assignment, "AuthContext role assignment")
        role = item.get("role_key")
        granted = item.get("permissions")
        if not isinstance(role, str) or not isinstance(granted, list) or not all(isinstance(key, str) for key in granted):
            raise SuiteError("AuthContext role assignment is incomplete")
        roles.add(role)
        permissions.update(granted)
    missing = REQUIRED_PERMISSIONS - permissions
    if missing:
        raise SuiteError("Online catalog token is missing required permissions: " + ", ".join(sorted(missing)))
    return {
        "principal_id": str(principal_id),
        "principal_type": "user",
        "context_type": "tenant",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(REQUIRED_PERMISSIONS),
    }


def wait_for_scan(client: GatewayClient, engine_id: int, deadline: float) -> dict[str, object]:
    run = _object(
        client.request(
            "POST",
            "/api/v1/meta/scan/run/manual",
            (201,),
            {"engine_id": engine_id, "scan_depth": "deep", "trigger_type": "manual", "force": True},
        ).payload,
        "Meta scan execution",
    )
    execution_id = run.get("execution_id")
    if not isinstance(execution_id, str) or not execution_id:
        raise SuiteError("Meta scan execution_id is missing")
    while time.monotonic() < deadline:
        execution = _object(
            client.request("GET", f"/api/v1/meta/executions/{urllib.parse.quote(execution_id)}", (200,)).payload,
            "Meta execution",
        )
        status = execution.get("status")
        if status == "success":
            return execution
        if status in TERMINAL_EXECUTION_STATUSES:
            raise SuiteError(f"Meta scan ended with status {status}")
        time.sleep(1)
    raise SuiteError("Meta scan did not finish before the convergence timeout")


def find_fixture_item(client: GatewayClient, engine_id: int) -> dict[str, object]:
    items = _array(
        client.request("GET", f"/api/v1/meta/engines/{engine_id}/items", (200,)).payload,
        "Meta engine items",
    )
    matches = [item for item in items if isinstance(item, dict) and item.get("name") == FIXTURE_TABLE]
    if len(matches) != 1:
        raise SuiteError(f"Meta must return exactly one {FIXTURE_TABLE} DataItem")
    item = matches[0]
    fingerprint = item.get("fingerprint")
    if not isinstance(fingerprint, str) or not fingerprint:
        raise SuiteError("Meta fixture DataItem fingerprint is missing")
    return item


def wait_for_catalog_entry(client: GatewayClient, fingerprint: str, deadline: float) -> dict[str, object]:
    query = urllib.parse.urlencode({"view": "inventory", "source_identity": fingerprint, "page": 1, "page_size": 2})
    while time.monotonic() < deadline:
        result = _object(client.request("GET", f"/api/v1/catalog/entries?{query}", (200,)).payload, "Catalog list")
        data = result.get("data")
        if isinstance(data, list) and len(data) == 1 and isinstance(data[0], dict):
            entry_id = data[0].get("id")
            if not isinstance(entry_id, str) or not entry_id:
                raise SuiteError("CatalogEntry id is missing")
            return _object(client.request("GET", f"/api/v1/catalog/entries/{entry_id}", (200,)).payload, "CatalogEntry")
        if isinstance(data, list) and len(data) > 1:
            raise SuiteError("one Meta fingerprint resolved to multiple CatalogEntries")
        time.sleep(1)
    raise SuiteError("CatalogEntry did not appear before the convergence timeout")


def editable_catalog_payload(entry: dict[str, object]) -> dict[str, object]:
    semantic_links = entry.get("semantic_links") or []
    responsibilities = entry.get("responsibilities") or []
    component_elements = entry.get("component_elements") or []
    if not all(isinstance(values, list) for values in (semantic_links, responsibilities, component_elements)):
        raise SuiteError("CatalogEntry editable associations must be arrays")
    return {
        "version": positive_int(entry.get("version"), "CatalogEntry version"),
        "business_name": entry.get("business_name"),
        "business_description": entry.get("business_description"),
        "governance_status": entry.get("governance_status"),
        "visibility": entry.get("visibility"),
        "domains": [
            {"id": str(link["semantic_id"]), "role": link["relation_role"]}
            for link in semantic_links
            if isinstance(link, dict) and link.get("semantic_type") == "domain"
        ],
        "glossary_ids": [
            str(link["semantic_id"])
            for link in semantic_links
            if isinstance(link, dict) and link.get("semantic_type") == "glossary"
        ],
        "responsibilities": [
            {"role": item["role"], "subject_type": item["subject_type"], "subject_id": str(item["subject_id"])}
            for item in responsibilities
            if isinstance(item, dict)
        ],
        "component_elements": [
            {"component_id": str(item["component_id"]), "element_id": str(item["element_id"])}
            for item in component_elements
            if isinstance(item, dict)
        ],
        "deprecation_reason": None,
    }


def curate_fixture_entry(
    client: GatewayClient,
    entry: dict[str, object],
    run_id: str,
    domain_id: int,
    department_id: int,
    principal_id: int,
) -> tuple[dict[str, object], dict[str, object] | None, bool]:
    entry_id = entry.get("id")
    if not isinstance(entry_id, str) or not entry_id:
        raise SuiteError("CatalogEntry id is missing")
    original = editable_catalog_payload(entry)
    status = original["governance_status"]
    if status == "deprecated":
        raise SuiteError("Online Catalog fixture must not be deprecated")
    initialized = status == "discovered"
    if initialized:
        update = {
            "version": original["version"],
            "business_name": "ADDP Online Catalog Fixture",
            "business_description": "Permanent CatalogEntry curated by the dedicated ADDP Online fixture",
            "governance_status": "curated",
            "visibility": "tenant",
            "domains": [{"id": str(domain_id), "role": "primary"}],
            "glossary_ids": [],
            "responsibilities": [
                {"role": "accountable_department", "subject_type": "department", "subject_id": str(department_id)},
                {"role": "business_owner", "subject_type": "user", "subject_id": str(principal_id)},
                {"role": "data_steward", "subject_type": "user", "subject_id": str(principal_id)},
            ],
            "component_elements": [],
            "deprecation_reason": None,
        }
        restore = None
    else:
        update = dict(original)
        update["business_name"] = f"ADDP Online Catalog Fixture {run_id}"
        update["business_description"] = f"Temporary curation evidence for ADDP Online run {run_id}"
        restore = original
    curated = _object(
        client.request("PUT", f"/api/v1/catalog/entries/{entry_id}", (200,), update).payload,
        "curated CatalogEntry",
    )
    if curated.get("governance_status") not in {"curated", "certified"}:
        raise SuiteError("CatalogEntry did not become publishable after curation")
    return curated, restore, initialized


def run_suite(
    client: GatewayClient,
    tenant_id: int,
    engine_id: int,
    run_id: str,
    domain_id: int,
    department_id: int,
    principal_id: int,
    convergence_timeout: float,
) -> dict[str, object]:
    deadline = time.monotonic() + convergence_timeout
    asset_id: int | None = None
    asset_catalog_id: int | None = None
    asset_status: str | None = None
    entry_id: str | None = None
    restore_payload: dict[str, object] | None = None
    cleanup_errors: list[str] = []
    fixture_initialized = False
    try:
        execution = wait_for_scan(client, engine_id, deadline)
        item = find_fixture_item(client, engine_id)
        fingerprint = str(item["fingerprint"])
        entry = wait_for_catalog_entry(client, fingerprint, deadline)
        entry_id = str(entry["id"])
        if not isinstance(entry.get("source"), dict) or entry["source"].get("source_identity") != fingerprint:
            raise SuiteError("CatalogEntry current source does not match the Meta fingerprint")
        initial_editable = editable_catalog_payload(entry)
        if initial_editable.get("governance_status") != "discovered":
            restore_payload = initial_editable
        curated, returned_restore, fixture_initialized = curate_fixture_entry(
            client, entry, run_id, domain_id, department_id, principal_id
        )
        if returned_restore is not None:
            restore_payload = returned_restore

        types = _array(client.request("GET", "/api/v1/asset/type-definitions", (200,)).payload, "Asset type definitions")
        enabled_types = [item for item in types if isinstance(item, dict) and item.get("enabled") is True]
        if not enabled_types:
            raise SuiteError("Asset has no enabled TypeDefinition")
        type_id = positive_int(enabled_types[0].get("id"), "Asset TypeDefinition id")

        asset_catalog = _object(
            client.request(
                "POST",
                "/api/v1/asset/catalogs",
                (201,),
                {"name": f"Online Catalog {run_id}", "description": f"Temporary Online run {run_id}", "sort_order": 0},
            ).payload,
            "Asset catalog",
        )
        asset_catalog_id = positive_int(asset_catalog.get("id"), "Asset catalog id")
        asset = _object(
            client.request(
                "POST",
                "/api/v1/asset/assets",
                (201,),
                {
                    "name": f"Online Asset {run_id}",
                    "description": f"Meta -> Catalog -> Asset -> Portal evidence for {run_id}",
                    "type_id": type_id,
                    "catalog_id": asset_catalog_id,
                    "tags": ["online", "enterprise-catalog"],
                    "components": [{"catalog_entry_id": entry_id, "role": "primary", "sort_order": 0}],
                },
            ).payload,
            "Asset",
        )
        asset_id = positive_int(asset.get("id"), "Asset id")
        asset_status = str(asset.get("status"))
        client.request("POST", f"/api/v1/asset/assets/{asset_id}/publish", (200,))
        asset_status = "published"

        portal_asset = _object(client.request("GET", f"/api/v1/portal/assets/{asset_id}", (200,)).payload, "Portal Asset")
        if portal_asset.get("status") != "published" or positive_int(portal_asset.get("id"), "Portal Asset id") != asset_id:
            raise SuiteError("Portal did not return the published Asset")
        components = portal_asset.get("components")
        if not isinstance(components, list) or len(components) != 1 or not isinstance(components[0], dict):
            raise SuiteError("Portal Asset components are incomplete")
        if components[0].get("catalog_entry_id") != entry_id:
            raise SuiteError("Portal Asset does not preserve the CatalogEntry identity")

        return {
            "schema_version": "addp.enterprise-catalog-publishing/v1",
            "suite": "enterprise-catalog-publishing",
            "run_id": run_id,
            "tenant_id": str(tenant_id),
            "engine_id": str(engine_id),
            "meta_execution_id": execution.get("execution_id"),
            "meta_data_item_id": str(item.get("id")),
            "source_identity": fingerprint,
            "catalog_entry_id": entry_id,
            "asset_id": str(asset_id),
            "fixture_initialized": fixture_initialized,
            "route": ["meta", "catalog", "asset", "portal"],
            "temporary_resources_created": 2,
            "residual_resources": 0,
            "cleanup": "passed",
        }
    finally:
        if asset_id is not None:
            try:
                current_asset = client.request("GET", f"/api/v1/asset/assets/{asset_id}", (200, 404))
                if current_asset.status == 404:
                    asset_status = None
                else:
                    asset_status = str(_object(current_asset.payload, "Asset cleanup").get("status"))
                if asset_status == "published":
                    client.request("POST", f"/api/v1/asset/assets/{asset_id}/offline", (200,))
                    asset_status = "offline"
                if asset_status is not None:
                    client.request("DELETE", f"/api/v1/asset/assets/{asset_id}", (200,))
                client.request("GET", f"/api/v1/asset/assets/{asset_id}", (404,))
                client.request("GET", f"/api/v1/portal/assets/{asset_id}", (404,))
            except Exception as error:
                cleanup_errors.append(f"Asset: {error}")
        if asset_catalog_id is not None:
            try:
                client.request("DELETE", f"/api/v1/asset/catalogs/{asset_catalog_id}", (200,))
                client.request("GET", f"/api/v1/asset/catalogs/{asset_catalog_id}", (404,))
            except Exception as error:
                cleanup_errors.append(f"Asset catalog: {error}")
        if entry_id is not None and restore_payload is not None:
            try:
                current = _object(client.request("GET", f"/api/v1/catalog/entries/{entry_id}", (200,)).payload, "CatalogEntry cleanup")
                restore = dict(restore_payload)
                restore["version"] = positive_int(current.get("version"), "CatalogEntry cleanup version")
                restored = _object(client.request("PUT", f"/api/v1/catalog/entries/{entry_id}", (200,), restore).payload, "restored CatalogEntry")
                if restored.get("business_name") != restore.get("business_name"):
                    raise SuiteError("CatalogEntry business metadata was not restored")
            except Exception as error:
                cleanup_errors.append(f"CatalogEntry: {error}")
        if cleanup_errors:
            raise SuiteError("cleanup failed: " + "; ".join(cleanup_errors))


def main() -> int:
    try:
        if os.environ.get("ADDP_ONLINE_TEST") != "1":
            raise SuiteError("ADDP_ONLINE_TEST must be exactly 1")
        tenant_id = positive_int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"], "ADDP_ONLINE_TEST_TENANT_ID")
        engine_id = positive_int(os.environ["ADDP_ONLINE_TEST_ENGINE_ID"], "ADDP_ONLINE_TEST_ENGINE_ID")
        domain_id = positive_int(os.environ["ADDP_ONLINE_TEST_CATALOG_DOMAIN_ID"], "ADDP_ONLINE_TEST_CATALOG_DOMAIN_ID")
        department_id = positive_int(os.environ["ADDP_ONLINE_TEST_CATALOG_DEPARTMENT_ID"], "ADDP_ONLINE_TEST_CATALOG_DEPARTMENT_ID")
        run_id = os.environ["ADDP_ONLINE_TEST_RUN_ID"]
        token = os.environ.get("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN", "")
        if not token:
            raise SuiteError("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN is required")
        http_timeout = float(os.environ.get("ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS", "10"))
        convergence_timeout = float(os.environ.get("ADDP_ONLINE_CATALOG_CONVERGENCE_TIMEOUT_SECONDS", "180"))
        if http_timeout <= 0 or convergence_timeout <= 0:
            raise SuiteError("Online HTTP and convergence timeouts must be greater than zero")
        identity = validate_user_identity(
            GatewayClient(os.environ["SYSTEM_URL"], token, http_timeout, "SYSTEM_URL"), tenant_id
        )
        report = run_suite(
            GatewayClient(os.environ["GATEWAY_URL"], token, http_timeout),
            tenant_id,
            engine_id,
            run_id,
            domain_id,
            department_id,
            positive_int(identity["principal_id"], "Online principal id"),
            convergence_timeout,
        )
        report["identity"] = identity
    except (KeyError, ValueError, SuiteError) as error:
        print(f"Enterprise Catalog Online suite failed: {error}", file=sys.stderr)
        return 1
    json.dump(report, sys.stdout, ensure_ascii=False, sort_keys=True)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
