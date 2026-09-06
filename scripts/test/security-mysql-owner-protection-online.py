#!/usr/bin/env python3
"""Accept MySQL email suppression through all four protected data owners."""

from __future__ import annotations

import importlib.util
import json
import os
import sys
import time
import urllib.parse
from pathlib import Path
from typing import Any, Iterable, Mapping


SUPPORT_PATH = Path(__file__).with_name("security-transfer-protection-online.py")
SUPPORT_SPEC = importlib.util.spec_from_file_location(
    "security_transfer_protection_online_support", SUPPORT_PATH
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
preview_rows = SUPPORT.preview_rows
create_and_run_task = SUPPORT.create_and_run_task
cleanup_tasks = SUPPORT.cleanup_tasks


SOURCE_TABLE = "customers"
TARGET_SCHEMA = "addp_online_security"
TARGET_TABLE = "mysql_email_transfer"
EMAIL_TYPE_CODE = "email"
EMAIL_DETECTOR = "addp.detector.email_metadata/v1"
SERVICE_PREFIX = "addp-online-security-mysql-email-"
TASK_PREFIX = "addp_online_security_mysql_email_"
TERMINAL_STATUSES = {"success", "failed", "cancelled", "timeout"}
OWNER_ACTIONS = {
    "manager": "preview",
    "develop": "query",
    "service": "service_execute",
    "transfer": "export",
}
EXPECTED_IDS = {"1", "2", "3", "4", "5"}
FORBIDDEN_ADMIN_ROLES = SUPPORT.FORBIDDEN_ADMIN_ROLES
REQUIRED_PERMISSIONS = {
    "develop.data_read.execute",
    "develop.task.execute",
    "develop.task.read",
    "manager.data_item.read",
    "meta.catalog.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
    "security.assessment.read",
    "security.detector.read",
    "security.enrollment.create",
    "security.enrollment.read",
    "security.finding.read",
    "security.finding.update",
    "security.protection_baseline.read",
    "security.sensitive_data_type.read",
    "service.data_read.execute",
    "service.definition.create",
    "service.definition.delete",
    "service.definition.read",
    "transfer.task.create",
    "transfer.task.delete",
    "transfer.task.execute",
    "transfer.task.read",
}


def list_pages(
    client: GatewayClient, path: str, data_key: str = "data"
) -> list[dict[str, object]]:
    result: list[dict[str, object]] = []
    page = 1
    separator = "&" if "?" in path else "?"
    while True:
        payload = _object(
            client.request(
                "GET", f"{path}{separator}page={page}&page_size=100", (200,)
            ).payload,
            path,
        )
        result.extend(
            item
            for item in _array(payload.get(data_key), f"{path} {data_key}")
            if isinstance(item, dict)
        )
        total_pages = nonnegative_int(payload.get("total_pages"), f"{path} total_pages")
        if page >= total_pages:
            return result
        page += 1


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
        raise SuiteError("Online MySQL protection token must belong to a User")
    principal_id = positive_int(principal.get("id"), "AuthContext principal.id")
    if tenant.get("type") != "tenant" or tenant.get("tenant_id") != str(tenant_id):
        raise SuiteError("Online MySQL protection token must use the configured Tenant Context")
    if token.get("type") not in {"first_party_access_token", "oauth_access_token"}:
        raise SuiteError("Online MySQL protection token must be a User Access Token")
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
            "Online MySQL protection token must not use administrator roles: "
            + ", ".join(sorted(forbidden))
        )
    missing = REQUIRED_PERMISSIONS - permissions
    if missing:
        raise SuiteError(
            "Online MySQL protection token is missing required permissions: "
            + ", ".join(sorted(missing))
        )
    return {
        "principal_id": str(principal_id),
        "principal_type": "user",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(REQUIRED_PERMISSIONS),
    }


def definition_array(client: GatewayClient, path: str) -> list[dict[str, object]]:
    return [
        item
        for item in _array(client.request("GET", path, (200,)).payload, path)
        if isinstance(item, dict)
    ]


def validate_governance(client: GatewayClient) -> dict[str, object]:
    types = definition_array(client, "/api/v1/security/sensitive-data-types")
    email_types = [item for item in types if item.get("code") == EMAIL_TYPE_CODE]
    if len(email_types) != 1:
        raise SuiteError("the Tenant must define exactly one email sensitive data type")
    email_type = email_types[0]
    type_id = positive_int(email_type.get("id"), "email sensitive data type id")
    grade_id = positive_int(
        email_type.get("default_security_grade_id"), "email default security grade id"
    )

    detectors = definition_array(client, "/api/v1/security/detectors")
    bindings = [
        item
        for item in detectors
        if item.get("capability_key") == EMAIL_DETECTOR
        and str(item.get("sensitive_data_type_id")) == str(type_id)
        and item.get("enabled") is True
    ]
    if len(bindings) != 1:
        raise SuiteError("the Tenant must enable exactly one email metadata detector binding")

    baselines = definition_array(client, "/api/v1/security/protection-baselines")
    matches = [
        item
        for item in baselines
        if str(item.get("sensitive_data_type_id")) == str(type_id)
        and str(item.get("security_grade_id")) == str(grade_id)
        and item.get("enabled") is True
    ]
    if len(matches) != 1 or matches[0].get("effect") != "suppress":
        raise SuiteError("the email default protection baseline must be enabled with suppress effect")
    return {
        "sensitive_data_type_id": str(type_id),
        "detector_id": str(positive_int(bindings[0].get("id"), "email detector id")),
        "baseline_id": str(positive_int(matches[0].get("id"), "email baseline id")),
        "effect": "suppress",
    }


def ensure_enrollment(
    client: GatewayClient,
    source_engine_id: int,
    source_full_name: str,
    source_locator: str,
) -> tuple[str, bool]:
    enrollments = list_pages(
        client, "/api/v1/security/protection-enrollments?scope=current"
    )
    matches = [
        item
        for item in enrollments
        if isinstance(item.get("target_snapshot"), dict)
        and item["target_snapshot"].get("engine_id") == source_engine_id
        and item["target_snapshot"].get("full_name") == source_full_name
    ]
    if len(matches) > 1:
        raise SuiteError("the permanent MySQL customers fixture has multiple enrollments")
    initialized = False
    if matches:
        enrollment_id = matches[0].get("id")
    else:
        created = _object(
            client.request(
                "POST",
                "/api/v1/security/protection-enrollments",
                (201,),
                {"locator": source_locator},
            ).payload,
            "ProtectionEnrollment",
        )
        enrollment_id = created.get("id")
        initialized = True
    if not isinstance(enrollment_id, str) or not enrollment_id:
        raise SuiteError("ProtectionEnrollment id is missing")
    return enrollment_id, initialized


def ensure_email_assessment(
    client: GatewayClient, enrollment_id: str, deadline: float
) -> tuple[str, bool, str]:
    while time.monotonic() < deadline:
        findings = list_pages(
            client,
            "/api/v1/security/findings?"
            + urllib.parse.urlencode(
                {"enrollment_id": enrollment_id, "snapshot_scope": "current"}
            ),
        )
        email_findings = [
            item
            for item in findings
            if item.get("detector_version") == EMAIL_DETECTOR
            and isinstance(item.get("component"), dict)
            and item["component"].get("key") == "email"
        ]
        if len(email_findings) > 1:
            raise SuiteError("the current MySQL snapshot has multiple email findings")
        assessments = list_pages(
            client,
            "/api/v1/security/assessments?"
            + urllib.parse.urlencode({"enrollment_id": enrollment_id}),
        )
        current = [
            item
            for item in assessments
            if isinstance(item.get("current"), dict)
            and item["current"].get("conclusion") == "sensitive"
            and isinstance(item["current"].get("component"), dict)
            and item["current"]["component"].get("key") == "email"
        ]
        if len(current) == 1:
            assessment_id = current[0].get("id")
            if not isinstance(assessment_id, str) or not assessment_id:
                raise SuiteError("email Assessment id is missing")
            if email_findings:
                finding_id = email_findings[0].get("id")
                if isinstance(finding_id, str) and finding_id:
                    return assessment_id, False, finding_id
        candidates = [
            item
            for item in email_findings
            if item.get("review") is None
        ]
        if candidates:
            finding_id = candidates[0].get("id")
            if not isinstance(finding_id, str) or not finding_id:
                raise SuiteError("email SensitiveFinding id is missing")
            reviewed = _object(
                client.request(
                    "POST",
                    f"/api/v1/security/findings/{urllib.parse.quote(finding_id)}/reviews",
                    (201,),
                    {
                        "decision": "confirm",
                        "rationale": "Dedicated Online MySQL email assessment",
                    },
                ).payload,
                "SensitiveFinding review",
            )
            assessment = _object(reviewed.get("assessment"), "email Assessment")
            assessment_id = assessment.get("id")
            if not isinstance(assessment_id, str) or not assessment_id:
                raise SuiteError("confirmed email Assessment id is missing")
            return assessment_id, True, finding_id
        time.sleep(1)
    raise SuiteError("email finding or Assessment was not available before the deadline")


def wait_for_owner_projections(
    client: GatewayClient, enrollment_id: str, deadline: float
) -> dict[str, object]:
    while time.monotonic() < deadline:
        enrollment = _object(
            client.request(
                "GET",
                f"/api/v1/security/protection-enrollments/{urllib.parse.quote(enrollment_id)}",
                (200,),
            ).payload,
            "ProtectionEnrollment",
        )
        progresses = enrollment.get("owner_progress")
        if enrollment.get("state") == "active" and isinstance(progresses, list):
            evidence: dict[str, object] = {}
            for owner, action in OWNER_ACTIONS.items():
                matches = [
                    item
                    for item in progresses
                    if isinstance(item, dict) and item.get("consumer_owner") == owner
                ]
                if len(matches) != 1 or matches[0].get("acknowledged") is not True:
                    break
                rules = matches[0].get("rules")
                if not isinstance(rules, list) or (action, "suppress") not in {
                    (rule.get("action"), rule.get("effect"))
                    for rule in rules
                    if isinstance(rule, dict)
                }:
                    break
                evidence[owner] = {"action": action, "effect": "suppress"}
            if len(evidence) == len(OWNER_ACTIONS):
                return evidence
        if enrollment.get("state") in {"releasing", "released"}:
            raise SuiteError("the permanent MySQL customers enrollment is released")
        time.sleep(1)
    raise SuiteError("MySQL email projections did not converge before the deadline")


def normalize_id(value: object) -> str:
    if isinstance(value, float) and value.is_integer():
        return str(int(value))
    return str(value)


def assert_email_suppressed(
    rows: Iterable[Mapping[str, object]], owner: str, columns: Iterable[str] | None = None
) -> dict[str, object]:
    rows = list(rows)
    column_list = list(columns or [])
    if "email" in column_list or any("email" in row for row in rows):
        raise SuiteError(f"{owner} exposed the suppressed email field")
    ids = {normalize_id(row.get("id")) for row in rows if row.get("id") is not None}
    if ids != EXPECTED_IDS:
        raise SuiteError(f"{owner} did not return the five expected non-sensitive rows")
    return {"rows": len(rows), "email_field_present": False}


def develop_rows(
    client: GatewayClient, engine_id: int, source_locator: str, deadline: float
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
                    "query": "SELECT id, customer_code, email FROM customers ORDER BY id",
                    "query_type": "sql",
                    "target_locator": source_locator,
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
    return {
        "service_name": name,
        "title": "MySQL email suppression Online fixture",
        "description": "Four-owner field suppression acceptance",
        "keywords": ["security", "mysql", "online"],
        "config_type": "table",
        "engine_id": engine_id,
        "data_config": {
            "locator": locator,
            "stable_key": ["id"],
            "default_fields": ["id", "customer_code", "email"],
            "filterable_fields": ["id"],
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
                "select": ["id", "customer_code", "email"],
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


def transfer_payload(
    name: str, source_locator: str, target_parent_locator: str
) -> dict[str, object]:
    return {
        "name": name,
        "description": "Dedicated Online MySQL email suppression acceptance",
        "task_type": "sync",
        "config": {
            "runtime": {"boundary": "bounded"},
            "load": {"mode": "snapshot"},
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
                "policy": {"apply_mode": "replace"},
            },
            "transforms": [],
            "batch_size": 100,
        },
        "schedule": "",
        "enabled": False,
        "batch_size": 100,
        "auto_scan_metadata": False,
    }


def build_schema_locator(engine_id: int, item: Mapping[str, object]) -> str:
    node_id = positive_int(item.get("node_id"), "target schema node id")
    query = urllib.parse.urlencode({"type": "schema", "node_id": node_id})
    return (
        f"addp://engine/{engine_id}/path/"
        f"{urllib.parse.quote(TARGET_SCHEMA, safe='')}?{query}"
    )


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
    source_engine_id: int,
    target_engine_id: int,
    source_database: str,
    run_id: str,
    timeout: float,
) -> dict[str, object]:
    deadline = time.monotonic() + timeout
    identity = validate_user_identity(client, tenant_id)
    governance = validate_governance(client)
    source_scan = wait_for_scan(client, source_engine_id, deadline)
    target_scan = wait_for_scan(client, target_engine_id, deadline)
    source_full_name = f"{source_database}.{SOURCE_TABLE}"
    source_item = find_item(client, source_engine_id, source_full_name, "table")
    target_item = find_item(
        client, target_engine_id, f"{TARGET_SCHEMA}.{TARGET_TABLE}", "table"
    )
    source_locator = build_item_locator(source_engine_id, source_item)
    target_locator = build_item_locator(target_engine_id, target_item)
    target_parent_locator = build_schema_locator(target_engine_id, target_item)
    enrollment_id, enrollment_initialized = ensure_enrollment(
        client, source_engine_id, source_full_name, source_locator
    )
    assessment_id, assessment_initialized, finding_id = ensure_email_assessment(
        client, enrollment_id, deadline
    )
    projections = wait_for_owner_projections(client, enrollment_id, deadline)

    safe_run_id = "".join(
        character.lower() if character.isalnum() else "-" for character in run_id
    ).strip("-")
    service_name = (SERVICE_PREFIX + safe_run_id)[:120].rstrip("-")
    task_name = TASK_PREFIX + "".join(
        character if character.isalnum() else "_" for character in run_id
    )
    service_id: int | None = None
    task_ids: list[int] = []
    scenario_error: BaseException | None = None
    cleanup_errors: list[str] = []
    result: dict[str, object] = {}
    try:
        manager_columns, manager_data = preview_rows(client, source_locator)
        manager = assert_email_suppressed(manager_data, "manager", manager_columns)
        develop = assert_email_suppressed(
            develop_rows(client, source_engine_id, source_locator, deadline), "develop"
        )
        created_service = _object(
            client.request(
                "POST",
                "/api/v1/service/query",
                (201,),
                service_payload(service_name, source_engine_id, source_locator),
            ).payload,
            "Query Service",
        )
        service_id = positive_int(created_service.get("id"), "Query Service id")
        service = assert_email_suppressed(service_rows(client, service_name), "service")

        _, transfer_execution = create_and_run_task(
            client,
            transfer_payload(task_name, source_locator, target_parent_locator),
            deadline,
            task_ids,
        )
        target_rescan = wait_for_scan(client, target_engine_id, deadline)
        current_target = find_item(
            client, target_engine_id, f"{TARGET_SCHEMA}.{TARGET_TABLE}", "table"
        )
        target_columns, target_rows = preview_rows(
            client, build_item_locator(target_engine_id, current_target)
        )
        transfer = assert_email_suppressed(target_rows, "transfer", target_columns)
        if "customer_code" not in target_columns:
            raise SuiteError("Transfer target lost non-sensitive customer fields")

        result = {
            "schema_version": "addp.security-mysql-owner-protection-online/v1",
            "result": "passed",
            "identity": identity,
            "governance": governance,
            "fixture": {
                "source_engine_id": str(source_engine_id),
                "target_engine_id": str(target_engine_id),
                "source_scan_execution_id": source_scan,
                "target_scan_execution_id": target_scan,
                "target_rescan_execution_id": target_rescan,
                "security_enrollment_id": enrollment_id,
                "security_assessment_id": assessment_id,
                "security_finding_id": finding_id,
                "enrollment_initialized": enrollment_initialized,
                "assessment_initialized": assessment_initialized,
            },
            "projections": projections,
            "owners": {
                "manager": manager,
                "develop": develop,
                "service": service,
                "transfer": {
                    **transfer,
                    "execution_id": transfer_execution.get("execution_id"),
                    "records_written": transfer_execution.get("records_written"),
                },
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
    source_engine_id = positive_int(
        required_environment("ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID"),
        "ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID",
    )
    target_engine_id = positive_int(
        required_environment("ADDP_ONLINE_TEST_ENGINE_ID"),
        "ADDP_ONLINE_TEST_ENGINE_ID",
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
        source_engine_id,
        target_engine_id,
        required_environment("ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE"),
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
