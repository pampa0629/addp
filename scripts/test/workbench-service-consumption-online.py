#!/usr/bin/env python3
"""Accept Service -> Workbench consumption with the dedicated Business MySQL fixture."""

from __future__ import annotations

import csv
import io
import json
import os
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from decimal import Decimal, InvalidOperation
from pathlib import Path
from typing import Any, Callable, Iterable, Mapping


SERVICE_NAME = "commerce-order-analysis"
SQL = """SELECT
  o.order_no,
  c.customer_code,
  c.city,
  c.membership_level,
  o.status,
  o.total_amount,
  o.payment_method,
  o.ordered_at,
  o.shipped_at,
  c.active AS active_customer
FROM orders o
JOIN customers c ON c.id = o.customer_id"""
FIELDS = [
    "order_no",
    "customer_code",
    "city",
    "membership_level",
    "status",
    "total_amount",
    "payment_method",
    "ordered_at",
    "shipped_at",
    "active_customer",
]
FILTERABLE_FIELDS = [
    "ordered_at",
    "status",
    "city",
    "membership_level",
    "active_customer",
]
PII_FIELDS = {"name", "email", "phone", "shipping_address"}
REQUIRED_PERMISSIONS = {
    "service.data_read.execute",
    "service.definition.create",
    "service.definition.delete",
    "service.definition.read",
    "service.definition.update",
    "workbench.view.create",
    "workbench.view.delete",
    "workbench.view.read",
}
FORBIDDEN_ADMIN_ROLES = {
    "platform.audit_administrator",
    "platform.security_administrator",
    "platform.system_administrator",
    "tenant.administrator",
}


class SuiteError(RuntimeError):
    pass


@dataclass
class Response:
    status: int
    payload: Any
    headers: Mapping[str, str]
    raw: bytes = b""


class GatewayClient:
    def __init__(self, base_url: str, token: str, timeout: float) -> None:
        parsed = urllib.parse.urlsplit(base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise SuiteError("GATEWAY_URL must be an absolute HTTP(S) URL")
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.timeout = timeout

    def _request(
        self,
        method: str,
        path: str,
        expected: Iterable[int],
        body: dict[str, object] | None = None,
        headers: Mapping[str, str] | None = None,
    ) -> Response:
        data = None if body is None else json.dumps(body).encode()
        request_headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
            "Authorization": f"Bearer {self.token}",
        }
        request_headers.update(headers or {})
        request = urllib.request.Request(
            self.base_url + path,
            data=data,
            method=method,
            headers=request_headers,
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                status = response.status
                raw = response.read()
                response_headers = dict(response.headers.items())
        except urllib.error.HTTPError as error:
            status = error.code
            raw = error.read()
            response_headers = dict(error.headers.items()) if error.headers else {}
        except (urllib.error.URLError, TimeoutError) as error:
            raise SuiteError(f"{method} {path} transport failed: {error}") from error
        content_type = next(
            (value for key, value in response_headers.items() if key.lower() == "content-type"),
            "",
        ).lower()
        payload: Any = {}
        if raw and ("json" in content_type or status >= 400):
            try:
                payload = json.loads(raw)
            except json.JSONDecodeError as error:
                raise SuiteError(f"{method} {path} returned invalid JSON") from error
        if status not in set(expected):
            code = payload.get("error_code", "unknown") if isinstance(payload, dict) else "unknown"
            raise SuiteError(f"{method} {path} returned HTTP {status} ({code})")
        return Response(status=status, payload=payload, headers=response_headers, raw=raw)

    def request(
        self,
        method: str,
        path: str,
        expected: Iterable[int],
        body: dict[str, object] | None = None,
    ) -> Response:
        return self._request(method, path, expected, body)

    def query(
        self,
        body: dict[str, object],
        *,
        intent: str = "query",
    ) -> Response:
        path = "/api/query/" + urllib.parse.quote(SERVICE_NAME, safe="") + "/query"
        return self._request(
            "POST",
            path,
            (200,),
            body,
            {"X-ADDP-Query-Intent": intent},
        )


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
        parsed = int(value)  # type: ignore[arg-type]
    except (TypeError, ValueError) as error:
        raise SuiteError(f"{field} must be a positive integer") from error
    if parsed <= 0 or str(parsed) != str(value):
        raise SuiteError(f"{field} must be a canonical positive integer")
    return parsed


def validate_user_identity(client: GatewayClient, tenant_id: int) -> dict[str, object]:
    context = _object(
        client.request("GET", "/api/v1/system/auth/context", (200,)).payload,
        "AuthContext",
    )
    principal = _object(context.get("principal"), "AuthContext principal")
    tenant = _object(context.get("context"), "AuthContext context")
    token = _object(context.get("token"), "AuthContext token")
    authorization = _object(context.get("authorization"), "AuthContext authorization")
    if principal.get("type") != "user":
        raise SuiteError("Online Workbench token must belong to a User")
    principal_id = positive_int(principal.get("id"), "AuthContext principal.id")
    if tenant.get("type") != "tenant" or tenant.get("tenant_id") != str(tenant_id):
        raise SuiteError("Online Workbench token must use the configured Tenant Context")
    if token.get("type") not in {"first_party_access_token", "oauth_access_token"}:
        raise SuiteError("Online Workbench token must be a User Access Token")
    assignments = _array(authorization.get("role_assignments"), "AuthContext role_assignments")
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
    forbidden = roles & FORBIDDEN_ADMIN_ROLES
    if forbidden:
        raise SuiteError("Online Workbench token must not use administrator roles: " + ", ".join(sorted(forbidden)))
    missing = REQUIRED_PERMISSIONS - permissions
    if missing:
        raise SuiteError("Online Workbench token is missing required permissions: " + ", ".join(sorted(missing)))
    return {
        "principal_id": str(principal_id),
        "principal_type": "user",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(REQUIRED_PERMISSIONS),
    }


def assert_no_existing_service(client: GatewayClient) -> None:
    query = urllib.parse.urlencode({"search": SERVICE_NAME, "page": 1, "limit": 100})
    result = _object(client.request("GET", f"/api/v1/service/query?{query}", (200,)).payload, "Query Service list")
    matches = [
        item
        for item in _array(result.get("data"), "Query Service list data")
        if isinstance(item, dict) and item.get("service_name") == SERVICE_NAME
    ]
    if matches:
        raise SuiteError(f"stale Query Service {SERVICE_NAME} exists before the run")


def validate_output_contract(contract: dict[str, object], *, published: bool) -> None:
    table = _object(contract.get("table"), "SQL output contract table")
    fields = _array(table.get("fields"), "SQL output contract fields")
    by_name = {
        item.get("name"): item
        for item in fields
        if isinstance(item, dict) and isinstance(item.get("name"), str)
    }
    if list(by_name) != FIELDS:
        raise SuiteError(f"SQL output contract fields must be exactly {FIELDS}")
    if set(by_name) & PII_FIELDS:
        raise SuiteError("SQL output contract leaked PII fields")
    expected_types = {
        "order_no": "string",
        "customer_code": "string",
        "city": "string",
        "membership_level": "string",
        "status": "string",
        "total_amount": "decimal",
        "payment_method": "string",
        "ordered_at": "timestamp",
        "shipped_at": "timestamp",
        # MySQL exposes BOOLEAN as TINYINT metadata. The publisher resolves that
        # one ambiguous field explicitly; Service normalizes rows to the frozen
        # bool contract before returning JSON or CSV.
        "active_customer": "bool" if published else "int",
    }
    for name, expected in expected_types.items():
        if by_name[name].get("type") != expected:
            raise SuiteError(f"SQL output field {name} must use {expected}")
    if contract.get("spatial") is not None:
        raise SuiteError("commerce SQL output contract must not contain SpatialInfo")


def published_output_contract(detected: dict[str, object]) -> dict[str, object]:
    published = json.loads(json.dumps(detected))
    table = _object(published.get("table"), "published SQL output contract table")
    fields = _array(table.get("fields"), "published SQL output contract fields")
    matches = [item for item in fields if isinstance(item, dict) and item.get("name") == "active_customer"]
    if len(matches) != 1:
        raise SuiteError("detected SQL output contract must contain one active_customer field")
    matches[0]["type"] = "bool"
    validate_output_contract(published, published=True)
    return published


def validate_descriptor(descriptor: dict[str, object], service_id: int) -> str:
    if descriptor.get("schema_version") != "addp.service_consumer/v1":
        raise SuiteError("Consumer Descriptor schema_version is invalid")
    if descriptor.get("status") != "active" or descriptor.get("access_mode") != "private":
        raise SuiteError("commerce service must be an active private service")
    ref = _object(descriptor.get("ref"), "Consumer Descriptor ref")
    if ref != {"service_type": "query", "service_id": service_id}:
        raise SuiteError("Consumer Descriptor ref does not identify the created Query Service")
    fingerprint = descriptor.get("contract_fingerprint")
    if not isinstance(fingerprint, str) or not fingerprint.startswith("sha256:") or len(fingerprint) != 71:
        raise SuiteError("Consumer Descriptor contract_fingerprint is invalid")
    input_contract = _object(descriptor.get("input_contract"), "Consumer Descriptor input_contract")
    fields = _array(input_contract.get("fields"), "Consumer Descriptor input fields")
    by_name = {item.get("name"): item for item in fields if isinstance(item, dict)}
    if list(by_name) != FIELDS:
        raise SuiteError("Consumer Descriptor fields differ from the published contract")
    for name in FILTERABLE_FIELDS:
        if by_name[name].get("filterable") is not True:
            raise SuiteError(f"Consumer Descriptor field {name} must be filterable")
    order = _object(input_contract.get("order"), "Consumer Descriptor order")
    if order.get("stable_key") != ["order_no"]:
        raise SuiteError("Consumer Descriptor stable_key must be order_no")
    formats = input_contract.get("formats")
    if formats != ["json", "csv"]:
        raise SuiteError("non-spatial commerce service must expose only json and csv")
    output = _object(descriptor.get("output_contract"), "Consumer Descriptor output_contract")
    if output.get("kind") != "tabular" or output.get("spatial") is not None:
        raise SuiteError("commerce descriptor must be non-spatial tabular output")
    return fingerprint


def view_payload(service_id: int, run_id: str) -> dict[str, object]:
    return {
        "name": f"Commerce order analysis {run_id}",
        "description": "ADDP Online Workbench MySQL acceptance view",
        "service_ref": {"service_type": "query", "service_id": service_id},
        "parameter_definitions": [
            {"key": "statuses", "label": "Statuses", "control_type": "multiselect", "required": False},
            {"key": "ordered_after", "label": "Ordered after", "control_type": "datetime", "required": False},
        ],
        "query_template": {
            "select": FIELDS,
            "fixed_filter": None,
            "parameter_filters": [
                {"parameter_key": "statuses", "field": "status", "operator": "in"},
                {"parameter_key": "ordered_after", "field": "ordered_at", "operator": "gte"},
            ],
            "order_by": [{"field": "order_no", "direction": "asc"}],
            "page_limit": 100,
            "format": "json",
        },
        "default_parameter_values": {
            "statuses": ["delivered", "processing"],
        },
        "renderer_type": "table",
        "renderer_config": {"columns": FIELDS},
    }


def query_filter() -> dict[str, object]:
    return {
        "and": [
            {"field": "ordered_at", "op": "gte", "value": "2026-04-20 00:00:00"},
            {"field": "status", "op": "in", "value": ["delivered", "processing", "paid"]},
            {"field": "city", "op": "in", "value": ["上海", "北京", "深圳", "成都"]},
            {"field": "membership_level", "op": "in", "value": ["gold", "platinum", "silver"]},
            {"field": "active_customer", "op": "eq", "value": True},
        ]
    }


def query_body(limit: int, cursor: str = "", format_name: str = "json") -> dict[str, object]:
    page: dict[str, object] = {"limit": limit}
    if cursor:
        page["cursor"] = cursor
    return {
        "select": FIELDS,
        "filter": query_filter(),
        "order_by": [{"field": "order_no", "direction": "asc"}],
        "page": page,
        "format": format_name,
    }


def validate_rows(rows: list[object]) -> list[str]:
    order_numbers: list[str] = []
    for raw in rows:
        row = _object(raw, "query row")
        if set(row) != set(FIELDS) or set(row) & PII_FIELDS:
            raise SuiteError("query row fields differ from the PII-safe published contract")
        for name in ("order_no", "customer_code", "city", "membership_level", "status", "payment_method", "ordered_at"):
            if not isinstance(row.get(name), str) or not row[name]:
                raise SuiteError(f"query field {name} must be a non-empty string")
        if row.get("shipped_at") is not None and not isinstance(row.get("shipped_at"), str):
            raise SuiteError("shipped_at must be a timestamp string or null")
        if not isinstance(row.get("active_customer"), bool):
            raise SuiteError("active_customer must be a JSON boolean")
        try:
            Decimal(str(row.get("total_amount")))
        except (InvalidOperation, ValueError) as error:
            raise SuiteError("total_amount must be a decimal-compatible scalar") from error
        order_numbers.append(str(row["order_no"]))
    return order_numbers


def validate_json_pages(client: GatewayClient) -> dict[str, object]:
    first = _object(client.query(query_body(2)).payload, "first query page")
    first_rows = _array(first.get("data"), "first query page data")
    first_page = _object(first.get("page"), "first query page metadata")
    if len(first_rows) != 2 or first_page.get("has_more") is not True:
        raise SuiteError("first MySQL cursor page must contain two rows and has_more=true")
    cursor = first_page.get("next_cursor")
    if not isinstance(cursor, str) or not cursor:
        raise SuiteError("first MySQL cursor page must return next_cursor")
    second = _object(client.query(query_body(2, cursor)).payload, "second query page")
    second_rows = _array(second.get("data"), "second query page data")
    second_page = _object(second.get("page"), "second query page metadata")
    if len(second_rows) != 2 or second_page.get("has_more") is not False:
        raise SuiteError("second MySQL cursor page must contain two terminal rows")
    order_numbers = validate_rows(first_rows) + validate_rows(second_rows)
    if order_numbers != sorted(order_numbers) or len(set(order_numbers)) != 4:
        raise SuiteError("MySQL cursor pages must return four unique order_no values in stable order")
    if not isinstance(first.get("service_version"), str) or first.get("service_version") != second.get("service_version"):
        raise SuiteError("cursor pages must use one stable service_version")
    return {"rows": len(order_numbers), "pages": 2, "stable_key": "order_no"}


def _header(headers: Mapping[str, str], name: str) -> str:
    return next((value for key, value in headers.items() if key.lower() == name.lower()), "")


def validate_csv_export(client: GatewayClient) -> dict[str, object]:
    response = client.query(query_body(100, format_name="csv"), intent="export")
    if not _header(response.headers, "Content-Type").lower().startswith("text/csv"):
        raise SuiteError("CSV export must return text/csv")
    if _header(response.headers, "X-ADDP-Has-More").lower() != "false":
        raise SuiteError("bounded CSV export must be complete")
    rows = list(csv.reader(io.StringIO(response.raw.decode("utf-8-sig"))))
    if not rows or rows[0] != FIELDS or len(rows) != 5:
        raise SuiteError("CSV export must contain the PII-safe header and four rows")
    if set(rows[0]) & PII_FIELDS:
        raise SuiteError("CSV export leaked PII fields")
    return {"format": "csv", "rows": len(rows) - 1, "bounded": True}


def run_suite(
    client: GatewayClient,
    tenant_id: int,
    engine_id: int,
    run_id: str,
    browser_runner: Callable[[int, str, str], dict[str, object]] | None = None,
) -> dict[str, object]:
    service_id: int | None = None
    view_id: str | None = None
    created = 0
    deleted = 0
    cleanup_errors: list[str] = []
    identity = validate_user_identity(client, tenant_id)
    assert_no_existing_service(client)
    try:
        detected_contract = _object(
            client.request(
                "POST",
                "/api/v1/service/sql/output-contract",
                (200,),
                {"engine_id": engine_id, "sql": SQL},
            ).payload,
            "SQL output contract",
        )
        validate_output_contract(detected_contract, published=False)
        contract = published_output_contract(detected_contract)
        service = _object(
            client.request(
                "POST",
                "/api/v1/service/query",
                (201,),
                {
                    "service_name": SERVICE_NAME,
                    "title": "Commerce order analysis",
                    "description": "PII-safe read-only order analysis over Business MySQL",
                    "keywords": ["commerce", "mysql", "workbench-online"],
                    "config_type": "sql",
                    "engine_id": engine_id,
                    "sql_query": SQL,
                    "data_config": {
                        "stable_key": ["order_no"],
                        "default_fields": FIELDS,
                        "filterable_fields": FILTERABLE_FIELDS,
                    },
                    "output_contract": contract,
                    "protocols": {"rest_api": {"enabled": True, "formats": ["json", "csv"]}},
                    "public_access": False,
                    "max_features": 100,
                },
            ).payload,
            "created Query Service",
        )
        service_id = positive_int(service.get("id"), "Query Service id")
        if service.get("tenant_id") != tenant_id or service.get("service_name") != SERVICE_NAME:
            raise SuiteError("created Query Service identity is invalid")
        created += 1
        descriptor_path = f"/api/v1/service/consumer/services/query/{service_id}"
        descriptor = _object(client.request("GET", descriptor_path, (200,)).payload, "Consumer Descriptor")
        original_fingerprint = validate_descriptor(descriptor, service_id)

        view = _object(
            client.request("POST", "/api/v1/workbench/views", (201,), view_payload(service_id, run_id)).payload,
            "created Workbench View",
        )
        view_id_value = view.get("id")
        if not isinstance(view_id_value, str) or not view_id_value:
            raise SuiteError("created Workbench View id is missing")
        view_id = view_id_value
        if view.get("tenant_id") != tenant_id or view.get("contract_fingerprint") != original_fingerprint:
            raise SuiteError("Workbench View did not bind the current Tenant and contract fingerprint")
        created += 1

        cursor_evidence = validate_json_pages(client)
        csv_evidence = validate_csv_export(client)

        browser_evidence: dict[str, object] = {}
        if browser_runner is not None:
            browser_evidence = browser_runner(service_id, view_id, original_fingerprint)
        else:
            client.request(
                "PUT",
                f"/api/v1/service/query/{service_id}",
                (200,),
                {"data_config": {"default_fields": FIELDS[:-1], "filterable_fields": FILTERABLE_FIELDS}},
            )
        changed = _object(client.request("GET", descriptor_path, (200,)).payload, "changed Consumer Descriptor")
        changed_fingerprint = validate_descriptor(changed, service_id)
        if changed_fingerprint == original_fingerprint:
            raise SuiteError("public contract change did not change contract_fingerprint")
        saved = _object(client.request("GET", f"/api/v1/workbench/views/{view_id}", (200,)).payload, "saved Workbench View")
        if saved.get("contract_fingerprint") != original_fingerprint:
            raise SuiteError("Workbench View fingerprint changed without an explicit View update")

        return {
            "schema_version": "addp.workbench-service-consumption-online/v1",
            "service_name": SERVICE_NAME,
            "source_engine": "mysql",
            "route": ["service", "workbench"],
            "identity": identity,
            "cursor": cursor_evidence,
            "export": csv_evidence,
            "browser": browser_evidence,
            "renderers": {"table": True, "chart": True, "map": False},
            "contract_change_blocked": original_fingerprint != changed_fingerprint,
            "created_resources": created,
            "deleted_resources": created,
            "residual_resources": 0,
        }
    finally:
        if view_id is not None:
            try:
                client.request("DELETE", f"/api/v1/workbench/views/{view_id}", (200, 404))
                if client.request("GET", f"/api/v1/workbench/views/{view_id}", (404,)).status == 404:
                    deleted += 1
            except SuiteError as error:
                cleanup_errors.append(f"Workbench View: {error}")
        if service_id is not None:
            try:
                client.request("DELETE", f"/api/v1/service/query/{service_id}", (200, 404))
                if client.request("GET", f"/api/v1/service/query/{service_id}", (404,)).status == 404:
                    deleted += 1
            except SuiteError as error:
                cleanup_errors.append(f"Query Service: {error}")
        if cleanup_errors:
            raise SuiteError("cleanup failed: " + "; ".join(cleanup_errors))
        if deleted != created:
            raise SuiteError(f"cleanup removed {deleted} of {created} temporary resources")


def required_environment(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise SuiteError(f"{name} is required")
    return value


def validate_browser_report(
    report: object,
    run_id: str,
    tenant_id: str,
    service_id: int,
    view_id: str,
) -> dict[str, object]:
    payload = _object(report, "Workbench browser report")
    expected = {
        "schema_version": "addp.workbench-service-consumption-browser/v1",
        "suite": "workbench-service-consumption",
        "run_id": run_id,
        "result": "passed",
        "tenant_id": tenant_id,
        "service_id": service_id,
        "view_id": view_id,
        "table_rows": 2,
        "chart_rendered": True,
        "map_available": False,
        "contract_change_blocked": True,
    }
    mismatches = [key for key, value in expected.items() if payload.get(key) != value]
    if mismatches:
        raise SuiteError("Workbench browser report contract mismatch: " + ", ".join(mismatches))
    return payload


def run_browser(
    repository: Path,
    environment: dict[str, str],
    service_id: int,
    view_id: str,
    original_fingerprint: str,
) -> dict[str, object]:
    artifact_dir = Path(required_environment("ADDP_ONLINE_ARTIFACT_DIR"))
    report_path = artifact_dir / "workbench-service-consumption-browser.json"
    report_path.unlink(missing_ok=True)
    browser_environment = dict(environment)
    browser_environment.update(
        {
            "ADDP_ONLINE_REPOSITORY": str(repository),
            "ADDP_ONLINE_WORKBENCH_SERVICE_ID": str(service_id),
            "ADDP_ONLINE_WORKBENCH_VIEW_ID": view_id,
            "ADDP_ONLINE_WORKBENCH_ORIGINAL_FINGERPRINT": original_fingerprint,
        }
    )
    result = subprocess.run(
        [
            "npm",
            "run",
            "test:e2e",
            "--",
            "--config=playwright.online.config.js",
            "e2e/online/workbench-service-consumption.spec.js",
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
        raise SuiteError("Playwright did not write workbench-service-consumption-browser.json")
    return validate_browser_report(
        json.loads(report_path.read_text(encoding="utf-8")),
        environment["ADDP_ONLINE_TEST_RUN_ID"],
        environment["ADDP_ONLINE_TEST_TENANT_ID"],
        service_id,
        view_id,
    )


def main() -> int:
    try:
        if os.environ.get("ADDP_ONLINE_TEST") != "1":
            raise SuiteError("ADDP_ONLINE_TEST must be exactly 1")
        tenant_id = positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "ADDP_ONLINE_TEST_TENANT_ID")
        engine_id = positive_int(
            required_environment("ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID"),
            "ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID",
        )
        timeout = float(os.environ.get("ADDP_ONLINE_REQUEST_TIMEOUT_SECONDS", "30"))
        if timeout <= 0:
            raise SuiteError("ADDP_ONLINE_REQUEST_TIMEOUT_SECONDS must be greater than zero")
        client = GatewayClient(
            required_environment("GATEWAY_URL"),
            required_environment("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"),
            timeout,
        )
        required_environment("CONSOLE_URL")
        required_environment("ADDP_ONLINE_TEST_USER_USERNAME")
        required_environment("ADDP_ONLINE_TEST_USER_PASSWORD")
        required_environment("ADDP_ONLINE_ARTIFACT_DIR")
        environment = dict(os.environ)
        repository = Path(environment.get("ADDP_ONLINE_REPOSITORY", Path(__file__).parents[2])).resolve()
        report = run_suite(
            client,
            tenant_id,
            engine_id,
            required_environment("ADDP_ONLINE_TEST_RUN_ID"),
            lambda service_id, view_id, fingerprint: run_browser(
                repository, environment, service_id, view_id, fingerprint
            ),
        )
    except (SuiteError, ValueError) as error:
        print(f"Workbench Service consumption Online suite failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
