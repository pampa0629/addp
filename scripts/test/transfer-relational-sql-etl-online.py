#!/usr/bin/env python3
"""Accept the browser-created PostgreSQL relational SQL ETL flow."""

from __future__ import annotations

import hashlib
import importlib.util
import json
import os
import subprocess
import sys
import time
from pathlib import Path
from typing import Mapping


SUPPORT_PATH = Path(__file__).with_name("security-transfer-protection-online.py")
SUPPORT_SPEC = importlib.util.spec_from_file_location("transfer_sql_etl_support", SUPPORT_PATH)
SUPPORT = importlib.util.module_from_spec(SUPPORT_SPEC)
assert SUPPORT_SPEC.loader is not None
sys.modules[SUPPORT_SPEC.name] = SUPPORT
SUPPORT_SPEC.loader.exec_module(SUPPORT)

GatewayClient = SUPPORT.GatewayClient
SuiteError = SUPPORT.SuiteError
_array = SUPPORT._array
_object = SUPPORT._object
positive_int = SUPPORT.positive_int
required_environment = SUPPORT.required_environment
wait_for_scan = SUPPORT.wait_for_scan
find_item = SUPPORT.find_item
cleanup_tasks = SUPPORT.cleanup_tasks

SOURCE_TABLE = "addp_online_transfer_sql_etl_source"
TARGET_TABLE = "addp_online_transfer_sql_etl_target"
TASK_PREFIX = "addp_online_sql_etl_"
FORBIDDEN_ADMIN_ROLES = SUPPORT.FORBIDDEN_ADMIN_ROLES
REQUIRED_PERMISSIONS = {
    "meta.catalog.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
    "system.engine.execute",
    "system.engine.read",
    "transfer.task.create",
    "transfer.task.delete",
    "transfer.task.execute",
    "transfer.task.read",
}


def task_name(run_id: str) -> str:
    return TASK_PREFIX + hashlib.sha256(run_id.encode()).hexdigest()[:16]


def validate_user_identity(client: GatewayClient, tenant_id: int) -> dict[str, object]:
    context = _object(client.request("GET", "/api/v1/system/auth/context", (200,)).payload, "AuthContext")
    principal = _object(context.get("principal"), "AuthContext principal")
    tenant = _object(context.get("context"), "AuthContext context")
    token = _object(context.get("token"), "AuthContext token")
    authorization = _object(context.get("authorization"), "AuthContext authorization")
    if principal.get("type") != "user":
        raise SuiteError("Online Transfer SQL ETL token must belong to a User")
    principal_id = positive_int(principal.get("id"), "AuthContext principal.id")
    if tenant.get("type") != "tenant" or tenant.get("tenant_id") != str(tenant_id):
        raise SuiteError("Online Transfer SQL ETL token must use the configured Tenant Context")
    if token.get("type") not in {"first_party_access_token", "oauth_access_token"}:
        raise SuiteError("Online Transfer SQL ETL token must be a User Access Token")
    assignments = _array(authorization.get("role_assignments"), "AuthContext role_assignments")
    roles: set[str] = set()
    permissions: set[str] = set()
    for raw in assignments:
        assignment = _object(raw, "AuthContext role assignment")
        role = assignment.get("role_key")
        granted = assignment.get("permissions")
        if not isinstance(role, str) or not isinstance(granted, list) or not all(isinstance(value, str) for value in granted):
            raise SuiteError("AuthContext role assignment is incomplete")
        roles.add(role)
        permissions.update(granted)
    forbidden = roles & FORBIDDEN_ADMIN_ROLES
    if forbidden:
        raise SuiteError("Online Transfer SQL ETL token must not use administrator roles: " + ", ".join(sorted(forbidden)))
    missing = REQUIRED_PERMISSIONS - permissions
    if missing:
        raise SuiteError("Online Transfer SQL ETL token is missing required permissions: " + ", ".join(sorted(missing)))
    return {
        "principal_id": str(principal_id),
        "principal_type": "user",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(REQUIRED_PERMISSIONS),
    }


def validate_engine(client: GatewayClient, engine_id: int, expected_name: str, deadline: float) -> dict[str, object]:
    client.request("POST", f"/api/v1/system/engines/{engine_id}/test", (200,))
    while time.monotonic() < deadline:
        engine = _object(client.request("GET", f"/api/v1/system/engines/{engine_id}", (200,)).payload, "Engine Instance")
        if engine.get("engine_type") != "postgresql":
            raise SuiteError(f"configured Engine Instance {engine_id} must use engine_type=postgresql")
        if engine.get("name") != expected_name:
            raise SuiteError(f"configured Engine Instance {engine_id} has an unexpected name")
        if engine.get("lifecycle_state") != "active":
            raise SuiteError(f"configured Engine Instance {engine_id} must be active")
        if engine.get("connection_status") == "online":
            return {
                "engine_id": str(engine_id),
                "engine_type": "postgresql",
                "engine_name": expected_name,
                "connection_status": "online",
            }
        time.sleep(1)
    raise SuiteError(f"configured Engine Instance {engine_id} did not become online")


def suite_task_ids(client: GatewayClient, exact_name: str | None = None) -> list[int]:
    result: list[int] = []
    page = 1
    while True:
        listing = _object(
            client.request("GET", f"/api/v1/transfer/task-definitions?page={page}&page_size=100", (200,)).payload,
            "Transfer task list",
        )
        items = _array(listing.get("items"), "Transfer task list items")
        for raw in items:
            item = _object(raw, "Transfer task list item")
            name = item.get("name")
            if not isinstance(name, str) or not name.startswith(TASK_PREFIX):
                continue
            if exact_name is not None and name != exact_name:
                continue
            result.append(positive_int(item.get("id"), "Transfer task id"))
        total = int(listing.get("total", 0))
        if page * 100 >= total:
            return result
        page += 1


def validate_browser_report(report: object, run_id: str, tenant_id: str, expected_task_name: str) -> dict[str, object]:
    payload = _object(report, "Transfer relational SQL ETL browser report")
    expected = {
        "schema_version": "addp.transfer-relational-sql-etl-browser/v1",
        "suite": "transfer-relational-sql-etl",
        "run_id": run_id,
        "result": "passed",
        "tenant_id": tenant_id,
        "task_name": expected_task_name,
        "query_language": "sql",
        "language_selector_hidden": True,
        "projected_fields": ["id", "region", "amount"],
        "parameter_count": 2,
        "records_read": 2,
        "records_written": 2,
        "target_row_count": 2,
        "task_deleted": True,
    }
    mismatches = [key for key, value in expected.items() if payload.get(key) != value]
    if mismatches:
        raise SuiteError("Transfer relational SQL ETL browser report contract mismatch: " + ", ".join(mismatches))
    return payload


def run_browser(repository: Path, environment: Mapping[str, str], expected_task_name: str) -> dict[str, object]:
    artifact_dir = Path(required_environment("ADDP_ONLINE_ARTIFACT_DIR"))
    report_path = artifact_dir / "transfer-relational-sql-etl-browser.json"
    report_path.unlink(missing_ok=True)
    browser_environment = dict(environment)
    browser_environment.update(
        {
            "ADDP_ONLINE_REPOSITORY": str(repository),
            "ADDP_ONLINE_TRANSFER_SQL_ETL_TASK_NAME": expected_task_name,
            "ADDP_ONLINE_TRANSFER_SQL_ETL_SOURCE_TABLE": SOURCE_TABLE,
            "ADDP_ONLINE_TRANSFER_SQL_ETL_TARGET_TABLE": TARGET_TABLE,
        }
    )
    result = subprocess.run(
        [
            "npm",
            "run",
            "test:e2e",
            "--",
            "--config=playwright.online.config.js",
            "e2e/online/transfer-relational-sql-etl.spec.js",
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
        raise SuiteError("Playwright did not write transfer-relational-sql-etl-browser.json")
    return validate_browser_report(
        json.loads(report_path.read_text(encoding="utf-8")),
        environment["ADDP_ONLINE_TEST_RUN_ID"],
        environment["ADDP_ONLINE_TEST_TENANT_ID"],
        expected_task_name,
    )


def main() -> int:
    client: GatewayClient | None = None
    owned_name = ""
    try:
        if os.environ.get("ADDP_ONLINE_TEST") != "1":
            raise SuiteError("ADDP_ONLINE_TEST must be exactly 1")
        tenant_id = positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "ADDP_ONLINE_TEST_TENANT_ID")
        engine_id = positive_int(required_environment("ADDP_ONLINE_TEST_ENGINE_ID"), "ADDP_ONLINE_TEST_ENGINE_ID")
        timeout = float(os.environ.get("ADDP_ONLINE_REQUEST_TIMEOUT_SECONDS", "30"))
        if timeout <= 0:
            raise SuiteError("ADDP_ONLINE_REQUEST_TIMEOUT_SECONDS must be greater than zero")
        required_environment("CONSOLE_URL")
        required_environment("ADDP_ONLINE_TEST_USER_USERNAME")
        required_environment("ADDP_ONLINE_TEST_USER_PASSWORD")
        required_environment("ADDP_ONLINE_ARTIFACT_DIR")
        run_id = required_environment("ADDP_ONLINE_TEST_RUN_ID")
        owned_name = task_name(run_id)
        client = GatewayClient(required_environment("GATEWAY_URL"), required_environment("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"), timeout)

        identity_report = validate_user_identity(client, tenant_id)
        deadline = time.monotonic() + float(os.environ.get("ADDP_ONLINE_CONVERGENCE_TIMEOUT_SECONDS", "180"))
        engine = validate_engine(client, engine_id, required_environment("ADDP_ONLINE_TEST_ENGINE_NAME"), deadline)
        stale = suite_task_ids(client)
        if stale:
            raise SuiteError("stale Transfer relational SQL ETL Online tasks exist before the run")
        scan_execution_id = wait_for_scan(client, engine_id, deadline)
        find_item(client, engine_id, f"public.{SOURCE_TABLE}", "table")

        repository = Path(os.environ.get("ADDP_ONLINE_REPOSITORY", Path(__file__).parents[2])).resolve()
        browser = run_browser(repository, dict(os.environ), owned_name)
        residual = suite_task_ids(client, exact_name=owned_name)
        if residual:
            raise SuiteError("browser left the owned Transfer relational SQL ETL task behind")
        report = {
            "schema_version": "addp.transfer-relational-sql-etl-online/v1",
            "suite": "transfer-relational-sql-etl",
            "run_id": run_id,
            "result": "passed",
            "identity": identity_report,
            "engine": engine,
            "source_scan_execution_id": scan_execution_id,
            "browser": browser,
            "created_resources": 1,
            "deleted_resources": 1,
            "residual_resources": 0,
        }
        print(json.dumps(report, sort_keys=True))
        return 0
    except (OSError, ValueError, SuiteError) as error:
        if client is not None and owned_name:
            try:
                cleanup_tasks(client, suite_task_ids(client, exact_name=owned_name))
            except SuiteError as cleanup_error:
                print(f"Transfer relational SQL ETL Online cleanup failed: {cleanup_error}", file=sys.stderr)
        print(f"Transfer relational SQL ETL Online failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
