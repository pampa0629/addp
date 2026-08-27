#!/usr/bin/env python3
"""Accept the unique Meta -> Catalog -> Asset -> Portal route through Gateway APIs."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Iterable


FIXTURE_TABLE = "addp_online_catalog_fixture"
TERMINAL_EXECUTION_STATUSES = {"success", "failed", "timeout", "cancelled"}
GOVERNANCE_STATUSES = {"discovered", "curated", "certified", "deprecated"}
COVERAGE_DIMENSIONS = {
    "business_definition",
    "primary_domain",
    "accountable_department",
    "business_owner",
    "data_steward",
    "glossary",
    "component_element",
}


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


def non_negative_int(value: object, field: str) -> int:
    if isinstance(value, bool):
        raise SuiteError(f"{field} must be a non-negative integer")
    try:
        parsed = int(value)
    except (TypeError, ValueError) as error:
        raise SuiteError(f"{field} must be a non-negative integer") from error
    if parsed < 0 or str(parsed) != str(value):
        raise SuiteError(f"{field} must be a canonical non-negative integer")
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


def validate_catalog_source_resolution(
    client: GatewayClient, fingerprint: str, entry_id: str
) -> dict[str, object]:
    result = _object(
        client.request(
            "POST",
            "/api/v1/catalog/entries/resolve-sources",
            (200,),
            {
                "references": [
                    {
                        "source_module": "meta",
                        "source_type": "data_item",
                        "source_identity": fingerprint,
                    }
                ]
            },
        ).payload,
        "Catalog source resolution",
    )
    resolutions = result.get("results")
    if not isinstance(resolutions, list) or len(resolutions) != 1:
        raise SuiteError("Catalog source resolution must return exactly one result")
    resolution = _object(resolutions[0], "Catalog source resolution result")
    resolved_entry = _object(resolution.get("entry"), "resolved CatalogEntry")
    expected = {
        "source_module": "meta",
        "source_type": "data_item",
        "source_identity": fingerprint,
        "found": True,
    }
    if any(resolution.get(key) != value for key, value in expected.items()):
        raise SuiteError("Catalog source resolution did not preserve the exact Meta source identity")
    if resolved_entry.get("id") != entry_id or resolved_entry.get("source_status") != "active":
        raise SuiteError("Catalog source resolution did not return the active canonical CatalogEntry")
    return {
        "source_module": "meta",
        "source_type": "data_item",
        "source_identity": fingerprint,
        "catalog_entry_id": entry_id,
        "found": True,
    }


def validate_governance_coverage(client: GatewayClient) -> dict[str, object]:
    coverage = _object(
        client.request("GET", "/api/v1/catalog/governance/coverage", (200,)).payload,
        "Catalog governance coverage",
    )
    if coverage.get("view") != "inventory":
        raise SuiteError("Catalog governance coverage must use the inventory view")
    total = positive_int(coverage.get("total_entries"), "governance coverage total_entries")

    statuses = coverage.get("governance_statuses")
    if not isinstance(statuses, list) or len(statuses) != len(GOVERNANCE_STATUSES):
        raise SuiteError("Catalog governance coverage must return all governance statuses")
    status_counts: dict[str, int] = {}
    for raw_status in statuses:
        status = _object(raw_status, "governance status coverage")
        key = status.get("status")
        if not isinstance(key, str) or key in status_counts:
            raise SuiteError("Catalog governance coverage contains an invalid governance status")
        status_counts[key] = non_negative_int(status.get("count"), f"governance status {key} count")
    if set(status_counts) != GOVERNANCE_STATUSES or sum(status_counts.values()) != total:
        raise SuiteError("Catalog governance status distribution does not match total_entries")

    dimensions = coverage.get("dimensions")
    if not isinstance(dimensions, list) or len(dimensions) != len(COVERAGE_DIMENSIONS):
        raise SuiteError("Catalog governance coverage must return all fixed dimensions")
    dimension_summary: dict[str, dict[str, object]] = {}
    for raw_dimension in dimensions:
        dimension = _object(raw_dimension, "governance coverage dimension")
        key = dimension.get("key")
        if not isinstance(key, str) or key in dimension_summary:
            raise SuiteError("Catalog governance coverage contains an invalid dimension key")
        covered = non_negative_int(dimension.get("covered"), f"{key} covered")
        applicable = non_negative_int(dimension.get("applicable"), f"{key} applicable")
        not_covered = non_negative_int(dimension.get("not_covered"), f"{key} not_covered")
        not_applicable = non_negative_int(dimension.get("not_applicable"), f"{key} not_applicable")
        rate = dimension.get("coverage_rate")
        if not isinstance(rate, (int, float)) or isinstance(rate, bool) or rate < 0 or rate > 100:
            raise SuiteError(f"{key} coverage_rate must be between 0 and 100")
        if covered + not_covered != applicable or applicable + not_applicable != total:
            raise SuiteError(f"{key} governance coverage denominator is inconsistent")
        dimension_summary[key] = {
            "covered": covered,
            "applicable": applicable,
            "not_covered": not_covered,
            "not_applicable": not_applicable,
            "coverage_rate": rate,
        }
    if set(dimension_summary) != COVERAGE_DIMENSIONS:
        raise SuiteError("Catalog governance coverage dimension set is incomplete")
    return {
        "total_entries": total,
        "governance_statuses": status_counts,
        "dimensions": dimension_summary,
    }


def assert_catalog_entry_in_view(
    client: GatewayClient, view: str, fingerprint: str, entry_id: str
) -> None:
    query = urllib.parse.urlencode(
        {"view": view, "source_identity": fingerprint, "page": 1, "page_size": 2}
    )
    result = _object(
        client.request("GET", f"/api/v1/catalog/entries?{query}", (200,)).payload,
        f"Catalog {view} view",
    )
    data = result.get("data")
    if not isinstance(data, list) or len(data) != 1 or not isinstance(data[0], dict):
        raise SuiteError(f"Catalog {view} view must return exactly one fixture entry")
    if data[0].get("id") != entry_id:
        raise SuiteError(f"Catalog {view} view changed the fixture CatalogEntry identity")


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
    browser_runner: Callable[[str, str, str, int], dict[str, object]] | None = None,
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
        first_execution = wait_for_scan(client, engine_id, deadline)
        item = find_fixture_item(client, engine_id)
        fingerprint = str(item["fingerprint"])
        entry = wait_for_catalog_entry(client, fingerprint, deadline)
        entry_id = str(entry["id"])
        if not isinstance(entry.get("source"), dict) or entry["source"].get("source_identity") != fingerprint:
            raise SuiteError("CatalogEntry current source does not match the Meta fingerprint")
        first_entry_id = entry_id
        second_execution = wait_for_scan(client, engine_id, deadline)
        second_item = find_fixture_item(client, engine_id)
        if second_item.get("fingerprint") != fingerprint:
            raise SuiteError("repeated Meta scan changed the stable DataItem fingerprint")
        entry = wait_for_catalog_entry(client, fingerprint, deadline)
        entry_id = str(entry["id"])
        if entry_id != first_entry_id:
            raise SuiteError("repeated Meta scan created a second CatalogEntry")
        assert_catalog_entry_in_view(client, "inventory", fingerprint, entry_id)
        source_resolution = validate_catalog_source_resolution(client, fingerprint, entry_id)
        coverage_before = validate_governance_coverage(client)
        initial_editable = editable_catalog_payload(entry)
        if initial_editable.get("governance_status") != "discovered":
            restore_payload = initial_editable
        curated, returned_restore, fixture_initialized = curate_fixture_entry(
            client, entry, run_id, domain_id, department_id, principal_id
        )
        if returned_restore is not None:
            restore_payload = returned_restore
        assert_catalog_entry_in_view(client, "governance", fingerprint, entry_id)
        coverage_after = validate_governance_coverage(client)
        if coverage_after["total_entries"] != coverage_before["total_entries"]:
            raise SuiteError("Catalog curation unexpectedly changed the active entry denominator")

        browser_evidence: dict[str, object] = {}
        if browser_runner is not None:
            browser_evidence = browser_runner(
                entry_id,
                fingerprint,
                str(curated.get("business_name") or curated.get("display_name") or ""),
                int(coverage_after["total_entries"]),
            )

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
            "schema_version": "addp.enterprise-catalog-publishing/v2",
            "suite": "enterprise-catalog-publishing",
            "run_id": run_id,
            "tenant_id": str(tenant_id),
            "engine_id": str(engine_id),
            "meta_execution_ids": [
                first_execution.get("execution_id"),
                second_execution.get("execution_id"),
            ],
            "meta_data_item_id": str(item.get("id")),
            "source_identity": fingerprint,
            "catalog_entry_id": entry_id,
            "asset_id": str(asset_id),
            "fixture_initialized": fixture_initialized,
            "route": ["meta", "catalog", "asset", "portal"],
            "cases": {
                "scan_idempotency": "passed",
                "inventory_and_governance_views": "passed",
                "source_identity_resolution": "passed",
                "governance_coverage": "passed",
                "browser": "passed" if browser_runner is not None else "not-run",
                "asset_portal_publishing": "passed",
                "cleanup": "passed",
            },
            "source_resolution": source_resolution,
            "governance_coverage": {
                "before": coverage_before,
                "after": coverage_after,
            },
            "browser": browser_evidence,
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


def required_environment(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise SuiteError(f"{name} is required")
    return value


def validate_browser_report(
    report: object,
    run_id: str,
    tenant_id: str,
    entry_id: str,
    fingerprint: str,
    total_entries: int,
) -> dict[str, object]:
    payload = _object(report, "Enterprise Catalog browser report")
    expected = {
        "schema_version": "addp.enterprise-catalog-publishing-browser/v1",
        "suite": "enterprise-catalog-publishing",
        "run_id": run_id,
        "result": "passed",
        "tenant_id": tenant_id,
        "catalog_entry_id": entry_id,
        "source_identity": fingerprint,
        "coverage_total_entries": total_entries,
        "coverage_dimensions": len(COVERAGE_DIMENSIONS),
        "human_readable_filter_selectors": 3,
        "explicit_batch_governance_ui": True,
        "browser_warning_errors": 0,
    }
    mismatches = [key for key, value in expected.items() if payload.get(key) != value]
    if mismatches:
        raise SuiteError("Enterprise Catalog browser report contract mismatch: " + ", ".join(mismatches))
    return payload


def run_browser(
    repository: Path,
    environment: dict[str, str],
    entry_id: str,
    fingerprint: str,
    business_name: str,
    total_entries: int,
) -> dict[str, object]:
    artifact_dir = Path(required_environment("ADDP_ONLINE_ARTIFACT_DIR"))
    report_path = artifact_dir / "enterprise-catalog-publishing-browser.json"
    report_path.unlink(missing_ok=True)
    browser_environment = dict(environment)
    browser_environment.update(
        {
            "ADDP_ONLINE_REPOSITORY": str(repository),
            "ADDP_ONLINE_CATALOG_ENTRY_ID": entry_id,
            "ADDP_ONLINE_CATALOG_SOURCE_IDENTITY": fingerprint,
            "ADDP_ONLINE_CATALOG_BUSINESS_NAME": business_name,
            "ADDP_ONLINE_CATALOG_COVERAGE_TOTAL": str(total_entries),
        }
    )
    result = subprocess.run(
        [
            "npm",
            "run",
            "test:e2e",
            "--",
            "--config=playwright.online.config.js",
            "e2e/online/enterprise-catalog-publishing.spec.js",
        ],
        cwd=repository / "console/frontend",
        env=browser_environment,
        text=True,
        capture_output=True,
    )
    if result.stdout:
        print(result.stdout, end="" if result.stdout.endswith("\n") else "\n")
    if result.stderr:
        print(result.stderr, end="" if result.stderr.endswith("\n") else "\n", file=sys.stderr)
    if result.returncode != 0:
        raise SuiteError(f"Playwright exited with status {result.returncode}")
    if not report_path.is_file():
        raise SuiteError("Playwright did not write enterprise-catalog-publishing-browser.json")
    return validate_browser_report(
        json.loads(report_path.read_text(encoding="utf-8")),
        environment["ADDP_ONLINE_TEST_RUN_ID"],
        environment["ADDP_ONLINE_TEST_TENANT_ID"],
        entry_id,
        fingerprint,
        total_entries,
    )


def main() -> int:
    try:
        if os.environ.get("ADDP_ONLINE_TEST") != "1":
            raise SuiteError("ADDP_ONLINE_TEST must be exactly 1")
        tenant_id = positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "ADDP_ONLINE_TEST_TENANT_ID")
        engine_id = positive_int(required_environment("ADDP_ONLINE_TEST_ENGINE_ID"), "ADDP_ONLINE_TEST_ENGINE_ID")
        domain_id = positive_int(required_environment("ADDP_ONLINE_TEST_CATALOG_DOMAIN_ID"), "ADDP_ONLINE_TEST_CATALOG_DOMAIN_ID")
        department_id = positive_int(required_environment("ADDP_ONLINE_TEST_CATALOG_DEPARTMENT_ID"), "ADDP_ONLINE_TEST_CATALOG_DEPARTMENT_ID")
        run_id = required_environment("ADDP_ONLINE_TEST_RUN_ID")
        token = required_environment("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN")
        required_environment("CONSOLE_URL")
        required_environment("ADDP_ONLINE_TEST_USER_USERNAME")
        required_environment("ADDP_ONLINE_TEST_USER_PASSWORD")
        required_environment("ADDP_ONLINE_ARTIFACT_DIR")
        http_timeout = float(os.environ.get("ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS", "10"))
        convergence_timeout = float(os.environ.get("ADDP_ONLINE_CATALOG_CONVERGENCE_TIMEOUT_SECONDS", "180"))
        if http_timeout <= 0 or convergence_timeout <= 0:
            raise SuiteError("Online HTTP and convergence timeouts must be greater than zero")
        environment = dict(os.environ)
        repository = Path(environment.get("ADDP_ONLINE_REPOSITORY", Path(__file__).parents[2])).resolve()
        identity = validate_user_identity(
            GatewayClient(required_environment("SYSTEM_URL"), token, http_timeout, "SYSTEM_URL"), tenant_id
        )
        report = run_suite(
            GatewayClient(required_environment("GATEWAY_URL"), token, http_timeout),
            tenant_id,
            engine_id,
            run_id,
            domain_id,
            department_id,
            positive_int(identity["principal_id"], "Online principal id"),
            convergence_timeout,
            lambda entry_id, fingerprint, business_name, total_entries: run_browser(
                repository,
                environment,
                entry_id,
                fingerprint,
                business_name,
                total_entries,
            ),
        )
        report["identity"] = identity
    except (KeyError, OSError, ValueError, json.JSONDecodeError, SuiteError) as error:
        print(f"Enterprise Catalog Online suite failed: {error}", file=sys.stderr)
        return 1
    json.dump(report, sys.stdout, ensure_ascii=False, sort_keys=True)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
