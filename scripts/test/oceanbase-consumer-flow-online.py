#!/usr/bin/env python3
"""Accept one real OceanBase flow through Transfer, Develop, and Service."""

from __future__ import annotations

import hashlib
import importlib.util
import json
import os
import subprocess
import sys
import time
import urllib.parse
from decimal import Decimal, InvalidOperation
from pathlib import Path
from typing import Iterable, Mapping


SUPPORT_PATH = Path(__file__).with_name("security-transfer-protection-online.py")
SUPPORT_SPEC = importlib.util.spec_from_file_location(
    "oceanbase_consumer_flow_support", SUPPORT_PATH
)
SUPPORT = importlib.util.module_from_spec(SUPPORT_SPEC)
assert SUPPORT_SPEC.loader is not None
sys.modules[SUPPORT_SPEC.name] = SUPPORT
SUPPORT_SPEC.loader.exec_module(SUPPORT)

GatewayClient = SUPPORT.GatewayClient
SuiteError = SUPPORT.SuiteError
_array = SUPPORT._array
_object = SUPPORT._object
positive_int = SUPPORT.positive_int
nonnegative_int = SUPPORT.nonnegative_int
required_environment = SUPPORT.required_environment
wait_for_scan = SUPPORT.wait_for_scan
find_item = SUPPORT.find_item
build_item_locator = SUPPORT.build_item_locator
cleanup_tasks = SUPPORT.cleanup_tasks


SOURCE_TABLE = "addp_online_consumer_source"
TARGET_TABLE = "addp_online_consumer_target"
TASK_PREFIX = "addp_online_oceanbase_consumer_"
SERVICE_PREFIX = "addp-online-oceanbase-consumer-"
TERMINAL_STATUSES = {"success", "failed", "cancelled", "timeout"}
FORBIDDEN_ADMIN_ROLES = SUPPORT.FORBIDDEN_ADMIN_ROLES
REQUIRED_PERMISSIONS = {
    "develop.data_read.execute",
    "develop.task.execute",
    "develop.task.read",
    "meta.catalog.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
    "service.data_read.execute",
    "service.definition.create",
    "service.definition.delete",
    "service.definition.read",
    "system.engine.read",
    "transfer.task.create",
    "transfer.task.delete",
    "transfer.task.execute",
    "transfer.task.read",
}

BASELINE_ROWS = [
    {"id": "1", "item_code": "OB-1001", "quantity": 2, "amount": "19.90"},
    {"id": "2", "item_code": "OB-1002", "quantity": 4, "amount": "39.50"},
    {"id": "3", "item_code": "OB-1003", "quantity": 1, "amount": "99.00"},
    {"id": "4", "item_code": "OB-1004", "quantity": 8, "amount": "12.25"},
    {"id": "5", "item_code": "OB-1005", "quantity": 3, "amount": "50.75"},
]
FINAL_ROWS = [
    BASELINE_ROWS[0],
    {"id": "2", "item_code": "OB-1002", "quantity": 5, "amount": "44.50"},
    *BASELINE_ROWS[2:],
    {"id": "6", "item_code": "OB-1006", "quantity": 6, "amount": "66.60"},
]


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
        raise SuiteError("Online OceanBase token must belong to a User")
    principal_id = positive_int(principal.get("id"), "AuthContext principal.id")
    if tenant.get("type") != "tenant" or tenant.get("tenant_id") != str(tenant_id):
        raise SuiteError("Online OceanBase token must use the configured Tenant Context")
    if token.get("type") not in {"first_party_access_token", "oauth_access_token"}:
        raise SuiteError("Online OceanBase token must be a User Access Token")
    assignments = _array(authorization.get("role_assignments"), "AuthContext role_assignments")
    roles: set[str] = set()
    permissions: set[str] = set()
    for raw in assignments:
        assignment = _object(raw, "AuthContext role assignment")
        role = assignment.get("role_key")
        granted = assignment.get("permissions")
        if not isinstance(role, str) or not isinstance(granted, list) or not all(
            isinstance(key, str) for key in granted
        ):
            raise SuiteError("AuthContext role assignment is incomplete")
        roles.add(role)
        permissions.update(granted)
    forbidden = roles & FORBIDDEN_ADMIN_ROLES
    if forbidden:
        raise SuiteError(
            "Online OceanBase token must not use administrator roles: "
            + ", ".join(sorted(forbidden))
        )
    missing = REQUIRED_PERMISSIONS - permissions
    if missing:
        raise SuiteError(
            "Online OceanBase token is missing required permissions: "
            + ", ".join(sorted(missing))
        )
    return {
        "principal_id": str(principal_id),
        "principal_type": "user",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(REQUIRED_PERMISSIONS),
    }


def validate_engine(
    client: GatewayClient, engine_id: int, deadline: float
) -> dict[str, object]:
    last_status = "unknown"
    while time.monotonic() < deadline:
        engine = _object(
            client.request(
                "GET", f"/api/v1/system/engines/{engine_id}", (200,)
            ).payload,
            "OceanBase Engine Instance",
        )
        if engine.get("engine_type") != "oceanbase":
            raise SuiteError("configured Engine Instance must use engine_type=oceanbase")
        if engine.get("lifecycle_state") != "active":
            raise SuiteError("configured OceanBase Engine Instance must be active")
        last_status = str(engine.get("connection_status", "unknown"))
        if last_status == "online":
            return {
                "engine_id": str(engine_id),
                "engine_type": "oceanbase",
                "lifecycle_state": "active",
                "connection_status": "online",
            }
        time.sleep(1)
    raise SuiteError(
        f"configured OceanBase Engine Instance did not become online; last status={last_status}"
    )


def build_database_locator(engine_id: int, item: Mapping[str, object], database: str) -> str:
    node_id = positive_int(item.get("node_id"), "OceanBase database node id")
    query = urllib.parse.urlencode({"type": "database", "node_id": node_id})
    return (
        f"addp://engine/{engine_id}/path/"
        f"{urllib.parse.quote(database, safe='')}?{query}"
    )


def transfer_payload(
    name: str, source_locator: str, target_parent_locator: str
) -> dict[str, object]:
    return {
        "name": name,
        "description": "Dedicated Online OceanBase cross-module consumer acceptance",
        "task_type": "sync",
        "config": {
            "runtime": {"boundary": "bounded"},
            "load": {
                "mode": "incremental",
                "change_detection": {
                    "type": "watermark",
                    "field": "updated_at",
                    "tie_breaker": ["id"],
                    "start": "committed",
                    "end": "execution_upper_bound",
                },
            },
            "source": {
                "locator": source_locator,
                "data_type": "table",
                "representation": "native",
            },
            "target": {
                "parent_locator": target_parent_locator,
                "name": TARGET_TABLE,
                "data_type": "table",
                "representation": "native",
                "policy": {"apply_mode": "upsert", "keys": ["id"]},
            },
            "transforms": [],
            "batch_size": 100,
        },
        "schedule": "",
        "enabled": False,
        "batch_size": 100,
        "auto_scan_metadata": False,
    }


def create_task(
    client: GatewayClient, payload: dict[str, object], owned_task_ids: list[int]
) -> int:
    created = _object(
        client.request("POST", "/api/v1/transfer/task-definitions", (201,), payload).payload,
        "Transfer task",
    )
    task_id = positive_int(created.get("id"), "Transfer task id")
    owned_task_ids.append(task_id)
    return task_id


def run_task(
    client: GatewayClient, task_id: int, deadline: float
) -> dict[str, object]:
    started = _object(
        client.request(
            "POST", f"/api/v1/transfer/task-definitions/{task_id}/start", (200,)
        ).payload,
        "Transfer execution",
    )
    execution_id = started.get("execution_id")
    if not isinstance(execution_id, str) or not execution_id:
        raise SuiteError("Transfer execution_id is missing")
    while time.monotonic() < deadline:
        execution = _object(
            client.request(
                "GET",
                f"/api/v1/transfer/executions/{urllib.parse.quote(execution_id)}",
                (200,),
            ).payload,
            "Transfer execution",
        )
        status = execution.get("status")
        if status == "success":
            return execution
        if status in TERMINAL_STATUSES:
            raise SuiteError(f"Transfer execution ended with status {status}")
        time.sleep(1)
    raise SuiteError("Transfer execution did not finish before the deadline")


def assert_transfer_counts(
    execution: Mapping[str, object], expected: int, phase: str
) -> dict[str, object]:
    records_read = nonnegative_int(execution.get("records_read"), f"{phase} records_read")
    records_written = nonnegative_int(
        execution.get("records_written"), f"{phase} records_written"
    )
    if records_read != expected or records_written != expected:
        raise SuiteError(
            f"{phase} must read and write exactly {expected} rows; "
            f"got read={records_read}, written={records_written}"
        )
    return {
        "execution_id": execution.get("execution_id"),
        "records_read": records_read,
        "records_written": records_written,
    }


def develop_rows(
    client: GatewayClient,
    engine_id: int,
    target_locator: str,
    deadline: float,
) -> list[dict[str, object]]:
    started = _object(
        client.request(
            "POST",
            "/api/v1/develop/executions",
            (200,),
            {
                "dev_type": "query",
                "trigger_type": "manual",
                "content": {
                    "query": (
                        f"SELECT id, item_code, quantity, amount FROM `{TARGET_TABLE}` "
                        "ORDER BY id"
                    ),
                    "query_type": "sql",
                    "target_locator": target_locator,
                    "query_parameters": [],
                },
                "execution_config": {"engine_id": engine_id},
                "parameters": {},
                "timeout": 120,
            },
        ).payload,
        "Develop execution",
    )
    execution_id = started.get("execution_id")
    if not isinstance(execution_id, str) or not execution_id:
        raise SuiteError("Develop execution_id is missing")
    while time.monotonic() < deadline:
        execution = _object(
            client.request(
                "GET",
                f"/api/v1/develop/executions/{urllib.parse.quote(execution_id)}",
                (200,),
            ).payload,
            "Develop execution",
        )
        status = execution.get("status")
        if status == "success":
            metadata = _object(execution.get("metadata"), "Develop execution metadata")
            result = _object(metadata.get("result"), "Develop query result")
            summary = _object(result.get("summary"), "Develop query result summary")
            return [
                row
                for row in _array(summary.get("preview_rows"), "Develop preview rows")
                if isinstance(row, dict)
            ]
        if status in TERMINAL_STATUSES:
            raise SuiteError(f"Develop execution ended with status {status}")
        time.sleep(1)
    raise SuiteError("Develop execution did not finish before the deadline")


def service_payload(name: str, engine_id: int, locator: str) -> dict[str, object]:
    fields = ["id", "item_code", "quantity", "amount"]
    return {
        "service_name": name,
        "title": "OceanBase consumer flow Online fixture",
        "description": "Transfer output consumed through a Query Service",
        "keywords": ["oceanbase", "online"],
        "config_type": "table",
        "engine_id": engine_id,
        "data_config": {
            "locator": locator,
            "stable_key": ["id"],
            "default_fields": fields,
            "filterable_fields": ["id", "item_code"],
        },
        "protocols": {"rest_api": {"enabled": True, "formats": ["json"]}},
        "public_access": False,
        "max_features": 100,
    }


def service_rows(client: GatewayClient, name: str) -> list[dict[str, object]]:
    payload = _object(
        client.request(
            "POST",
            "/api/query/" + urllib.parse.quote(name, safe="") + "/query",
            (200,),
            {
                "select": ["id", "item_code", "quantity", "amount"],
                "order_by": [{"field": "id", "direction": "asc"}],
                "page": {"limit": 100},
                "format": "json",
            },
        ).payload,
        "Service query result",
    )
    return [
        row
        for row in _array(payload.get("data"), "Service query rows")
        if isinstance(row, dict)
    ]


def normalize_rows(rows: Iterable[Mapping[str, object]], owner: str) -> list[dict[str, object]]:
    normalized: list[dict[str, object]] = []
    for row in rows:
        try:
            row_id = str(int(row.get("id")))  # type: ignore[arg-type]
            quantity = int(row.get("quantity"))  # type: ignore[arg-type]
            amount = str(Decimal(str(row.get("amount"))).quantize(Decimal("0.01")))
        except (InvalidOperation, TypeError, ValueError) as error:
            raise SuiteError(f"{owner} returned a row with invalid scalar types") from error
        item_code = row.get("item_code")
        if not isinstance(item_code, str) or not item_code:
            raise SuiteError(f"{owner} returned a row without item_code")
        normalized.append(
            {
                "id": row_id,
                "item_code": item_code,
                "quantity": quantity,
                "amount": amount,
            }
        )
    normalized.sort(key=lambda row: int(str(row["id"])))
    if len({row["id"] for row in normalized}) != len(normalized):
        raise SuiteError(f"{owner} returned duplicate stable keys")
    return normalized


def assert_rows(
    rows: Iterable[Mapping[str, object]], expected: list[dict[str, object]], owner: str
) -> dict[str, object]:
    normalized = normalize_rows(rows, owner)
    if normalized != expected:
        raise SuiteError(f"{owner} did not return the expected OceanBase target rows")
    encoded = json.dumps(normalized, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return {
        "rows": len(normalized),
        "checksum": hashlib.sha256(encoded.encode()).hexdigest(),
    }


def advance_fixture() -> None:
    repository = Path(__file__).resolve().parents[2]
    result = subprocess.run(
        [
            "bash",
            str(repository / "business/scripts/online-oceanbase-consumer-fixture.sh"),
            "advance",
        ],
        cwd=repository,
        env=dict(os.environ),
        text=True,
        capture_output=True,
    )
    if result.returncode != 0:
        message = result.stderr.strip() or result.stdout.strip() or "unknown error"
        raise SuiteError(f"OceanBase fixture advance failed: {message}")


def cleanup_service(client: GatewayClient, service_id: int | None) -> None:
    if service_id is None:
        return
    current = client.request("GET", f"/api/v1/service/query/{service_id}", (200, 404))
    if current.status == 200:
        client.request("DELETE", f"/api/v1/service/query/{service_id}", (200,))
    client.request("GET", f"/api/v1/service/query/{service_id}", (404,))


def run_scenario(
    client: GatewayClient,
    tenant_id: int,
    engine_id: int,
    database: str,
    run_id: str,
    timeout: float,
) -> dict[str, object]:
    deadline = time.monotonic() + timeout
    identity = validate_user_identity(client, tenant_id)
    engine = validate_engine(client, engine_id, deadline)
    initial_scan = wait_for_scan(client, engine_id, deadline)
    source_item = find_item(
        client, engine_id, f"{database}.{SOURCE_TABLE}", "table"
    )
    target_item = find_item(
        client, engine_id, f"{database}.{TARGET_TABLE}", "table"
    )
    source_locator = build_item_locator(engine_id, source_item)
    target_locator = build_item_locator(engine_id, target_item)
    target_parent_locator = build_database_locator(engine_id, target_item, database)

    safe_service_run_id = "".join(
        character.lower() if character.isalnum() else "-" for character in run_id
    ).strip("-")
    service_name = (SERVICE_PREFIX + safe_service_run_id)[:120].rstrip("-")
    task_name = (TASK_PREFIX + "".join(
        character if character.isalnum() else "_" for character in run_id
    ))[:255]

    service_id: int | None = None
    task_ids: list[int] = []
    scenario_error: BaseException | None = None
    cleanup_errors: list[str] = []
    result: dict[str, object] = {}
    try:
        task_id = create_task(
            client,
            transfer_payload(task_name, source_locator, target_parent_locator),
            task_ids,
        )
        initial_transfer = assert_transfer_counts(
            run_task(client, task_id, deadline), 5, "initial watermark execution"
        )
        develop_initial = assert_rows(
            develop_rows(client, engine_id, target_locator, deadline),
            BASELINE_ROWS,
            "Develop initial query",
        )

        created_service = _object(
            client.request(
                "POST",
                "/api/v1/service/query",
                (201,),
                service_payload(service_name, engine_id, target_locator),
            ).payload,
            "Query Service",
        )
        service_id = positive_int(created_service.get("id"), "Query Service id")
        service_initial = assert_rows(
            service_rows(client, service_name), BASELINE_ROWS, "Service initial query"
        )

        advance_fixture()
        incremental_transfer = assert_transfer_counts(
            run_task(client, task_id, deadline), 2, "incremental watermark execution"
        )
        empty_transfer = assert_transfer_counts(
            run_task(client, task_id, deadline), 0, "empty watermark execution"
        )
        develop_final = assert_rows(
            develop_rows(client, engine_id, target_locator, deadline),
            FINAL_ROWS,
            "Develop final query",
        )
        service_final = assert_rows(
            service_rows(client, service_name), FINAL_ROWS, "Service final query"
        )
        if develop_final["checksum"] != service_final["checksum"]:
            raise SuiteError("Develop and Service returned different OceanBase results")

        result = {
            "schema_version": "addp.oceanbase-consumer-flow-online/v1",
            "result": "passed",
            "identity": identity,
            "engine": engine,
            "fixture": {
                "database": database,
                "source_table": SOURCE_TABLE,
                "target_table": TARGET_TABLE,
                "scan_execution_id": initial_scan,
            },
            "transfer": {
                "task_id": str(task_id),
                "initial": initial_transfer,
                "incremental": incremental_transfer,
                "empty_resume": empty_transfer,
            },
            "develop": {"initial": develop_initial, "final": develop_final},
            "service": {
                "service_id": str(service_id),
                "initial": service_initial,
                "final": service_final,
            },
            "created_resources": 1 + len(task_ids),
            "deleted_resources": 1 + len(task_ids),
            "residual_resources": 0,
        }
    except BaseException as error:
        scenario_error = error
    try:
        cleanup_tasks(client, task_ids)
    except BaseException as error:
        cleanup_errors.append(str(error))
    try:
        cleanup_service(client, service_id)
    except BaseException as error:
        cleanup_errors.append(str(error))
    if cleanup_errors:
        prefix = f"scenario failed: {scenario_error}; " if scenario_error else ""
        raise SuiteError(prefix + "cleanup failed: " + "; ".join(cleanup_errors))
    if scenario_error is not None:
        raise scenario_error
    return result


def main() -> int:
    if os.environ.get("ADDP_ONLINE_TEST") != "1":
        raise SuiteError("ADDP_ONLINE_TEST must be exactly 1")
    tenant_id = positive_int(
        required_environment("ADDP_ONLINE_TEST_TENANT_ID"),
        "ADDP_ONLINE_TEST_TENANT_ID",
    )
    engine_id = positive_int(
        required_environment("ADDP_ONLINE_OCEANBASE_ENGINE_ID"),
        "ADDP_ONLINE_OCEANBASE_ENGINE_ID",
    )
    timeout = float(os.environ.get("ADDP_ONLINE_TEST_TIMEOUT_SECONDS", "900"))
    if timeout <= 60:
        raise SuiteError("ADDP_ONLINE_TEST_TIMEOUT_SECONDS must be greater than 60")
    client = GatewayClient(
        required_environment("GATEWAY_URL"),
        required_environment("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"),
        min(timeout, 30),
    )
    report = run_scenario(
        client,
        tenant_id,
        engine_id,
        required_environment("ADDP_ONLINE_OCEANBASE_DATABASE"),
        required_environment("ADDP_ONLINE_TEST_RUN_ID"),
        timeout,
    )
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SuiteError as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
