#!/usr/bin/env python3
"""Accept Security projection consumption by bounded MongoDB Transfer tasks."""

from __future__ import annotations

import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterable, Mapping


SOURCE_COLLECTION = "security_transfer_fixture"
TARGET_SCHEMA = "addp_online_security"
EXCLUDED_TARGET = "transfer_excluded"
MASKED_TARGET = "transfer_masked"
TASK_PREFIX = "addp_online_security_transfer_"
TERMINAL_STATUSES = {"success", "failed", "cancelled"}
FORBIDDEN_ADMIN_ROLES = {
    "platform.audit_administrator",
    "platform.security_administrator",
    "platform.system_administrator",
    "tenant.administrator",
}
REQUIRED_PERMISSIONS = {
    "manager.data_item.read",
    "meta.catalog.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
    "security.enrollment.create",
    "security.enrollment.read",
    "transfer.task.create",
    "transfer.task.delete",
    "transfer.task.execute",
    "transfer.task.read",
}


class SuiteError(RuntimeError):
    pass


@dataclass
class Response:
    status: int
    payload: Any


class GatewayClient:
    def __init__(self, base_url: str, token: str, timeout: float) -> None:
        parsed = urllib.parse.urlsplit(base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise SuiteError("GATEWAY_URL must be an absolute HTTP(S) URL")
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


def nonnegative_int(value: object, field: str) -> int:
    if isinstance(value, bool):
        raise SuiteError(f"{field} must be a non-negative integer")
    try:
        parsed = int(value)  # type: ignore[arg-type]
    except (TypeError, ValueError) as error:
        raise SuiteError(f"{field} must be a non-negative integer") from error
    if parsed < 0 or str(parsed) != str(value):
        raise SuiteError(f"{field} must be a canonical non-negative integer")
    return parsed


def required_environment(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise SuiteError(f"{name} is required")
    return value


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
        raise SuiteError("Online Security token must belong to a User")
    principal_id = positive_int(principal.get("id"), "AuthContext principal.id")
    if tenant.get("type") != "tenant" or tenant.get("tenant_id") != str(tenant_id):
        raise SuiteError("Online Security token must use the configured Tenant Context")
    if token.get("type") not in {"first_party_access_token", "oauth_access_token"}:
        raise SuiteError("Online Security token must be a User Access Token")
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
        raise SuiteError("Online Security token must not use administrator roles: " + ", ".join(sorted(forbidden)))
    missing = REQUIRED_PERMISSIONS - permissions
    if missing:
        raise SuiteError("Online Security token is missing required permissions: " + ", ".join(sorted(missing)))
    return {
        "principal_id": str(principal_id),
        "principal_type": "user",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(REQUIRED_PERMISSIONS),
    }


def wait_for_scan(client: GatewayClient, engine_id: int, deadline: float) -> str:
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
            return execution_id
        if status in TERMINAL_STATUSES:
            raise SuiteError(f"Meta scan ended with status {status}")
        time.sleep(1)
    raise SuiteError("Meta scan did not finish before the convergence timeout")


def find_item(
    client: GatewayClient,
    engine_id: int,
    full_name: str,
    expected_type: str,
) -> dict[str, object]:
    items = _array(
        client.request("GET", f"/api/v1/meta/engines/{engine_id}/items", (200,)).payload,
        "Meta engine items",
    )
    matches = [item for item in items if isinstance(item, dict) and item.get("full_name") == full_name]
    if len(matches) != 1:
        raise SuiteError(f"Meta must return exactly one {full_name} DataItem")
    item = matches[0]
    positive_int(item.get("id"), f"{full_name} item id")
    positive_int(item.get("node_id"), f"{full_name} node id")
    if item.get("item_type") != expected_type:
        raise SuiteError(f"{full_name} must be a {expected_type} DataItem")
    if not isinstance(item.get("fingerprint"), str) or not item.get("fingerprint"):
        raise SuiteError(f"{full_name} fingerprint is missing")
    return item


def build_item_locator(engine_id: int, item: Mapping[str, object]) -> str:
    item_id = positive_int(item.get("id"), "DataItem id")
    full_name = item.get("full_name")
    item_type = item.get("item_type")
    if not isinstance(full_name, str) or not full_name.strip():
        raise SuiteError("DataItem full_name is missing")
    if not isinstance(item_type, str) or not item_type.strip():
        raise SuiteError("DataItem item_type is missing")
    segments = [urllib.parse.quote(part, safe="") for part in full_name.strip("/").replace("/", ".").split(".")]
    path = "/".join(segment for segment in segments if segment)
    query = urllib.parse.urlencode({"type": item_type, "item_id": item_id})
    return f"addp://engine/{engine_id}/path/{path}?{query}"


def build_schema_locator(engine_id: int, item: Mapping[str, object]) -> str:
    node_id = positive_int(item.get("node_id"), "target schema node id")
    query = urllib.parse.urlencode({"type": "schema", "node_id": node_id})
    return f"addp://engine/{engine_id}/path/{urllib.parse.quote(TARGET_SCHEMA, safe='')}?{query}"


def ensure_protected_enrollment(
    client: GatewayClient,
    source_engine_id: int,
    source_full_name: str,
    source_locator: str,
    deadline: float,
) -> tuple[dict[str, object], bool]:
    matches: list[dict[str, object]] = []
    page = 1
    while True:
        listing = _object(
            client.request(
                "GET",
                f"/api/v1/security/protection-enrollments?scope=current&page={page}&page_size=100",
                (200,),
            ).payload,
            "ProtectionEnrollment list",
        )
        values = _array(listing.get("data"), "ProtectionEnrollment list data")
        for value in values:
            if not isinstance(value, dict):
                continue
            snapshot = value.get("target_snapshot")
            if isinstance(snapshot, dict) and snapshot.get("engine_id") == source_engine_id and snapshot.get("full_name") == source_full_name:
                matches.append(value)
        total_pages = nonnegative_int(listing.get("total_pages"), "ProtectionEnrollment list total_pages")
        if page >= total_pages:
            break
        page += 1
    if len(matches) > 1:
        raise SuiteError("the permanent Security fixture has multiple current enrollments")
    initialized = False
    if matches:
        enrollment_id = matches[0].get("id")
    else:
        created = _object(
            client.request("POST", "/api/v1/security/protection-enrollments", (201,), {"locator": source_locator}).payload,
            "ProtectionEnrollment",
        )
        enrollment_id = created.get("id")
        initialized = True
    if not isinstance(enrollment_id, str) or not enrollment_id:
        raise SuiteError("ProtectionEnrollment id is missing")
    while time.monotonic() < deadline:
        enrollment = _object(
            client.request("GET", f"/api/v1/security/protection-enrollments/{urllib.parse.quote(enrollment_id)}", (200,)).payload,
            "ProtectionEnrollment",
        )
        if enrollment.get("state") == "active":
            validate_transfer_projection(enrollment)
            return enrollment, initialized
        if enrollment.get("state") in {"releasing", "released"}:
            raise SuiteError("the permanent Security fixture enrollment is being released")
        time.sleep(1)
    raise SuiteError("Security enrollment did not become active before the convergence timeout")


def validate_transfer_projection(enrollment: Mapping[str, object]) -> None:
    progresses = enrollment.get("owner_progress")
    if not isinstance(progresses, list):
        raise SuiteError("ProtectionEnrollment owner_progress must be an array")
    matches = [value for value in progresses if isinstance(value, dict) and value.get("consumer_owner") == "transfer"]
    if len(matches) != 1:
        raise SuiteError("ProtectionEnrollment must expose exactly one Transfer projection")
    progress = matches[0]
    if progress.get("projection_state") != "active" or progress.get("acknowledged") is not True:
        raise SuiteError("Transfer protection projection is not active and acknowledged")
    rules = progress.get("rules")
    if not isinstance(rules, list):
        raise SuiteError("Transfer protection projection rules must be an array")
    effects = {
        (rule.get("action"), rule.get("effect"))
        for rule in rules
        if isinstance(rule, dict)
    }
    if ("export", "mask") not in effects:
        raise SuiteError("Transfer protection projection must contain export -> mask")


def assert_no_stale_tasks(client: GatewayClient) -> None:
    stale: list[object] = []
    page = 1
    while True:
        listing = _object(
            client.request(
                "GET",
                f"/api/v1/transfer/task-definitions?page={page}&page_size=100",
                (200,),
            ).payload,
            "Transfer task list",
        )
        items = _array(listing.get("items"), "Transfer task list items")
        stale.extend(
            item.get("id")
            for item in items
            if isinstance(item, dict)
            and isinstance(item.get("name"), str)
            and item["name"].startswith(TASK_PREFIX)
        )
        total = nonnegative_int(listing.get("total"), "Transfer task list total")
        if page * 100 >= total:
            break
        page += 1
    if stale:
        raise SuiteError("stale Security Transfer Online tasks exist before the run")


def task_payload(
    name: str,
    source_locator: str,
    target_parent_locator: str,
    target_name: str,
    statement: str,
    fields: list[dict[str, object]],
) -> dict[str, object]:
    return {
        "name": name,
        "description": "Dedicated Online acceptance for Security projection consumption",
        "task_type": "sync",
        "config": {
            "runtime": {"boundary": "bounded"},
            "load": {"mode": "snapshot"},
            "source": {
                "locator": source_locator,
                "data_type": "table",
                "representation": "native",
                "query": {"language": "mql", "statement": statement},
            },
            "target": {
                "parent_locator": target_parent_locator,
                "name": target_name,
                "data_type": "table",
                "representation": "native",
                "policy": {"apply_mode": "replace"},
            },
            "transforms": [{"type": "field_mapping", "version": "v1", "mode": "project", "fields": fields}],
            "batch_size": 100,
        },
        "schedule": "",
        "enabled": False,
        "batch_size": 100,
        "auto_scan_metadata": True,
    }


def create_and_run_task(
    client: GatewayClient,
    payload: dict[str, object],
    deadline: float,
    owned_task_ids: list[int],
) -> tuple[int, dict[str, object]]:
    created = _object(client.request("POST", "/api/v1/transfer/task-definitions", (201,), payload).payload, "Transfer task")
    task_id = positive_int(created.get("id"), "Transfer task id")
    owned_task_ids.append(task_id)
    execution = _object(
        client.request("POST", f"/api/v1/transfer/task-definitions/{task_id}/start", (200,)).payload,
        "Transfer execution",
    )
    execution_id = execution.get("execution_id")
    if not isinstance(execution_id, str) or not execution_id:
        raise SuiteError("Transfer execution_id is missing")
    while time.monotonic() < deadline:
        current = _object(
            client.request("GET", f"/api/v1/transfer/executions/{urllib.parse.quote(execution_id)}", (200,)).payload,
            "Transfer execution",
        )
        status = current.get("status")
        if status == "success":
            return task_id, current
        if status in TERMINAL_STATUSES:
            raise SuiteError(f"Transfer execution ended with status {status}")
        time.sleep(1)
    raise SuiteError("Transfer execution did not finish before the convergence timeout")


def preview_rows(client: GatewayClient, locator: str) -> tuple[list[str], list[dict[str, object]]]:
    query = urllib.parse.urlencode({"locator": locator, "page": 1, "page_size": 100})
    preview = _object(client.request("GET", f"/api/v1/manager/preview?{query}", (200,)).payload, "Manager preview")
    if preview.get("preview_type") != "table":
        raise SuiteError("Manager preview must return table data")
    data = _object(preview.get("data"), "Manager table preview")
    columns = _array(data.get("columns"), "Manager table preview columns")
    if not all(isinstance(column, str) for column in columns):
        raise SuiteError("Manager table preview columns must contain strings")
    rows = _array(data.get("rows"), "Manager table preview rows")
    if not all(isinstance(row, dict) for row in rows):
        raise SuiteError("Manager table preview rows must contain objects")
    return list(columns), list(rows)  # type: ignore[arg-type]


def validate_target_previews(
    excluded: tuple[list[str], list[dict[str, object]]],
    masked: tuple[list[str], list[dict[str, object]]],
) -> dict[str, object]:
    excluded_columns, excluded_rows = excluded
    masked_columns, masked_rows = masked
    if excluded_columns != ["_id", "display_name"]:
        raise SuiteError("the non-sensitive projection target has unexpected columns")
    if len(excluded_rows) != 3 or any("userInfo__phone" in row for row in excluded_rows):
        raise SuiteError("the non-sensitive projection exposed the protected phone field")
    if masked_columns != ["_id", "userInfo__phone"]:
        raise SuiteError("the protected projection target has unexpected columns")
    if len(masked_rows) != 3:
        raise SuiteError("the protected projection target must contain exactly three rows")
    values = {row.get("_id"): row.get("userInfo__phone") for row in masked_rows}
    expected = {"person-1": "138****5678", "person-2": "139****4321", "person-3": None}
    if values != expected:
        raise SuiteError("the protected projection target does not contain the expected masked values")
    return {
        "excluded_row_count": len(excluded_rows),
        "masked_row_count": len(masked_rows),
        "masked_non_null_count": sum(value is not None for value in values.values()),
        "plaintext_value_count": 0,
    }


def cleanup_tasks(client: GatewayClient, task_ids: Iterable[int]) -> None:
    errors: list[str] = []
    for task_id in reversed(list(task_ids)):
        try:
            client.request("DELETE", f"/api/v1/transfer/task-definitions/{task_id}", (200,))
            client.request("GET", f"/api/v1/transfer/task-definitions/{task_id}", (404,))
        except SuiteError as error:
            errors.append(str(error))
    if errors:
        raise SuiteError("Transfer task cleanup failed: " + "; ".join(errors))


def run_scenario(client: GatewayClient, tenant_id: int, source_engine_id: int, target_engine_id: int, run_id: str, timeout: float) -> dict[str, object]:
    deadline = time.monotonic() + timeout
    identity = validate_user_identity(client, tenant_id)
    source_scan = wait_for_scan(client, source_engine_id, deadline)
    target_scan = wait_for_scan(client, target_engine_id, deadline)
    source_full_name = f"{required_environment('ADDP_ONLINE_SECURITY_MONGODB_DATABASE')}.{SOURCE_COLLECTION}"
    source_item = find_item(client, source_engine_id, source_full_name, "collection")
    excluded_item = find_item(client, target_engine_id, f"{TARGET_SCHEMA}.{EXCLUDED_TARGET}", "table")
    source_locator = build_item_locator(source_engine_id, source_item)
    target_parent_locator = build_schema_locator(target_engine_id, excluded_item)
    enrollment, initialized = ensure_protected_enrollment(
        client, source_engine_id, source_full_name, source_locator, deadline
    )
    assert_no_stale_tasks(client)

    safe_run_id = run_id.replace("-", "_").replace(".", "_")
    excluded_statement = json.dumps(
        {"aggregate": SOURCE_COLLECTION, "pipeline": [{"$project": {"_id": "$_id", "display_name": {"$ifNull": ["$displayName", None]}}}]},
        separators=(",", ":"),
    )
    masked_statement = json.dumps(
        {"aggregate": SOURCE_COLLECTION, "pipeline": [{"$project": {"_id": "$_id", "userInfo__phone": {"$ifNull": ["$userInfo.phone", None]}}}]},
        separators=(",", ":"),
    )
    excluded_payload = task_payload(
        f"{TASK_PREFIX}{safe_run_id}_excluded",
        source_locator,
        target_parent_locator,
        EXCLUDED_TARGET,
        excluded_statement,
        [
            {"source": "_id", "target": "_id", "target_type": "string", "nullable": False},
            {"source": "display_name", "target": "display_name", "target_type": "string", "nullable": False},
        ],
    )
    masked_payload = task_payload(
        f"{TASK_PREFIX}{safe_run_id}_masked",
        source_locator,
        target_parent_locator,
        MASKED_TARGET,
        masked_statement,
        [
            {"source": "_id", "target": "_id", "target_type": "string", "nullable": False},
            {"source": "userInfo__phone", "target": "userInfo__phone", "target_type": "string", "nullable": True},
        ],
    )

    tasks: list[int] = []
    scenario_error: BaseException | None = None
    result: dict[str, object] = {}
    try:
        excluded_task_id, excluded_execution = create_and_run_task(client, excluded_payload, deadline, tasks)
        masked_task_id, masked_execution = create_and_run_task(client, masked_payload, deadline, tasks)
        target_rescan = wait_for_scan(client, target_engine_id, deadline)
        excluded_current = find_item(client, target_engine_id, f"{TARGET_SCHEMA}.{EXCLUDED_TARGET}", "table")
        masked_current = find_item(client, target_engine_id, f"{TARGET_SCHEMA}.{MASKED_TARGET}", "table")
        protection = validate_target_previews(
            preview_rows(client, build_item_locator(target_engine_id, excluded_current)),
            preview_rows(client, build_item_locator(target_engine_id, masked_current)),
        )
        result = {
            "schema_version": "addp.security-transfer-protection/v1",
            "result": "passed",
            "identity": identity,
            "fixture": {
                "source_engine_id": str(source_engine_id),
                "target_engine_id": str(target_engine_id),
                "source_scan_execution_id": source_scan,
                "target_scan_execution_id": target_scan,
                "target_rescan_execution_id": target_rescan,
                "security_enrollment_id": enrollment.get("id"),
                "security_fixture_initialized": initialized,
            },
            "executions": {
                "non_sensitive_projection": {
                    "execution_id": excluded_execution.get("execution_id"),
                    "records_read": excluded_execution.get("records_read"),
                    "records_written": excluded_execution.get("records_written"),
                },
                "protected_projection": {
                    "execution_id": masked_execution.get("execution_id"),
                    "records_read": masked_execution.get("records_read"),
                    "records_written": masked_execution.get("records_written"),
                },
            },
            "protection": protection,
            "residual_resources": 0,
        }
    except BaseException as error:
        scenario_error = error
    try:
        cleanup_tasks(client, tasks)
    except BaseException as cleanup_error:
        if scenario_error is not None:
            raise SuiteError(f"scenario failed: {scenario_error}; cleanup failed: {cleanup_error}") from cleanup_error
        raise
    if scenario_error is not None:
        raise scenario_error
    return result


def main() -> int:
    if os.environ.get("ADDP_ONLINE_TEST") != "1":
        raise SuiteError("ADDP_ONLINE_TEST must be exactly 1")
    tenant_id = positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "ADDP_ONLINE_TEST_TENANT_ID")
    source_engine_id = positive_int(required_environment("ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID"), "ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID")
    target_engine_id = positive_int(required_environment("ADDP_ONLINE_TEST_ENGINE_ID"), "ADDP_ONLINE_TEST_ENGINE_ID")
    run_id = required_environment("ADDP_ONLINE_TEST_RUN_ID")
    timeout = float(os.environ.get("ADDP_ONLINE_TEST_TIMEOUT_SECONDS", "900"))
    if timeout <= 0:
        raise SuiteError("ADDP_ONLINE_TEST_TIMEOUT_SECONDS must be greater than zero")
    client = GatewayClient(
        required_environment("GATEWAY_URL"),
        required_environment("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"),
        min(timeout, 30),
    )
    report = run_scenario(client, tenant_id, source_engine_id, target_engine_id, run_id, timeout)
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SuiteError as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
