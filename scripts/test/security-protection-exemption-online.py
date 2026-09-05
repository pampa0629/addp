#!/usr/bin/env python3
"""Accept time-bounded ProtectionExemption behavior across all four data owners."""

from __future__ import annotations

import importlib.util
import json
import os
import sys
import time
import urllib.parse
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Callable, Iterable, Mapping


SUPPORT_PATH = Path(__file__).with_name("security-transfer-protection-online.py")
SUPPORT_SPEC = importlib.util.spec_from_file_location(
    "security_transfer_protection_online_support", SUPPORT_PATH
)
SUPPORT = importlib.util.module_from_spec(SUPPORT_SPEC)
assert SUPPORT_SPEC.loader is not None
sys.modules[SUPPORT_SPEC.name] = SUPPORT
SUPPORT_SPEC.loader.exec_module(SUPPORT)

GatewayClient = SUPPORT.GatewayClient
Response = SUPPORT.Response
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


SOURCE_SCHEMA = "addp_online_security"
SOURCE_TABLE = "exemption_source"
TARGET_TABLE = "exemption_transfer"
SOURCE_FULL_NAME = f"{SOURCE_SCHEMA}.{SOURCE_TABLE}"
TARGET_FULL_NAME = f"{SOURCE_SCHEMA}.{TARGET_TABLE}"
TASK_PREFIX = "addp_online_security_exemption_"
SERVICE_PREFIX = "addp-online-security-exemption-"
TERMINAL_STATUSES = {"success", "failed", "cancelled", "timeout"}
OWNER_ACTIONS = {
    "manager": "preview",
    "develop": "query",
    "service": "service_execute",
    "transfer": "export",
}
REQUIRED_PERMISSIONS = {
    "develop.data_read.execute",
    "develop.task.execute",
    "develop.task.read",
    "manager.data_item.read",
    "meta.catalog.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
    "security.assessment.read",
    "security.finding.read",
    "security.finding.update",
    "security.enrollment.create",
    "security.enrollment.read",
    "security.protection_exemption.create",
    "security.protection_exemption.delete",
    "security.protection_exemption.read",
    "security.protection_exemption.update",
    "service.data_read.execute",
    "service.definition.create",
    "service.definition.delete",
    "service.definition.read",
    "transfer.task.create",
    "transfer.task.delete",
    "transfer.task.execute",
    "transfer.task.read",
}
FORBIDDEN_ADMIN_ROLES = SUPPORT.FORBIDDEN_ADMIN_ROLES
RAW_VALUES = {"1": "13812345678", "2": "13987654321", "3": None}
MASKED_VALUES = {"1": "138****5678", "2": "139****4321", "3": None}


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
        raise SuiteError("Online ProtectionExemption token must belong to a User")
    principal_id = positive_int(principal.get("id"), "AuthContext principal.id")
    if tenant.get("type") != "tenant" or tenant.get("tenant_id") != str(tenant_id):
        raise SuiteError("Online ProtectionExemption token must use the configured Tenant Context")
    if token.get("type") not in {"first_party_access_token", "oauth_access_token"}:
        raise SuiteError("Online ProtectionExemption token must be a User Access Token")
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
            "Online ProtectionExemption token must not use administrator roles: "
            + ", ".join(sorted(forbidden))
        )
    missing = REQUIRED_PERMISSIONS - permissions
    if missing:
        raise SuiteError(
            "Online ProtectionExemption token is missing required permissions: "
            + ", ".join(sorted(missing))
        )
    return {
        "principal_id": str(principal_id),
        "principal_type": "user",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(REQUIRED_PERMISSIONS),
    }


def list_pages(
    client: GatewayClient,
    path: str,
    data_key: str = "data",
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


def wait_for_enrollment(
    client: GatewayClient,
    enrollment_id: str,
    deadline: float,
    expected_effects: Mapping[str, str],
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
        if enrollment.get("state") in {"releasing", "released"}:
            raise SuiteError("the permanent ProtectionExemption fixture enrollment is released")
        progresses = enrollment.get("owner_progress")
        if enrollment.get("state") == "active" and isinstance(progresses, list):
            ready = True
            for owner, effect in expected_effects.items():
                matches = [
                    item
                    for item in progresses
                    if isinstance(item, dict) and item.get("consumer_owner") == owner
                ]
                if len(matches) != 1 or matches[0].get("acknowledged") is not True:
                    ready = False
                    break
                rules = matches[0].get("rules")
                action = OWNER_ACTIONS[owner]
                if not isinstance(rules, list) or (action, effect) not in {
                    (rule.get("action"), rule.get("effect"))
                    for rule in rules
                    if isinstance(rule, dict)
                }:
                    ready = False
                    break
            if ready:
                return enrollment
        time.sleep(1)
    raise SuiteError("ProtectionExemption projections did not converge before the deadline")


def ensure_enrollment(
    client: GatewayClient,
    engine_id: int,
    locator: str,
    deadline: float,
) -> tuple[dict[str, object], bool]:
    enrollments = list_pages(
        client, "/api/v1/security/protection-enrollments?scope=current"
    )
    matches = [
        item
        for item in enrollments
        if isinstance(item.get("target_snapshot"), dict)
        and item["target_snapshot"].get("engine_id") == engine_id
        and item["target_snapshot"].get("full_name") == SOURCE_FULL_NAME
    ]
    if len(matches) > 1:
        raise SuiteError("the permanent ProtectionExemption fixture has multiple enrollments")
    initialized = False
    if matches:
        enrollment_id = matches[0].get("id")
    else:
        created = _object(
            client.request(
                "POST",
                "/api/v1/security/protection-enrollments",
                (201,),
                {"locator": locator},
            ).payload,
            "ProtectionEnrollment",
        )
        enrollment_id = created.get("id")
        initialized = True
    if not isinstance(enrollment_id, str) or not enrollment_id:
        raise SuiteError("ProtectionEnrollment id is missing")
    return (
        wait_for_enrollment(
            client,
            enrollment_id,
            deadline,
            {owner: "mask" for owner in OWNER_ACTIONS},
        ),
        initialized,
    )


def ensure_phone_assessment(
    client: GatewayClient,
    enrollment: Mapping[str, object],
    deadline: float,
) -> tuple[dict[str, object], bool]:
    enrollment_id = enrollment.get("id")
    if not isinstance(enrollment_id, str) or not enrollment_id:
        raise SuiteError("ProtectionEnrollment id is missing")
    while time.monotonic() < deadline:
        assessments = list_pages(
            client,
            "/api/v1/security/assessments?"
            + urllib.parse.urlencode({"enrollment_id": enrollment_id}),
        )
        phone = [
            item
            for item in assessments
            if isinstance(item.get("current"), dict)
            and item["current"].get("conclusion") == "sensitive"
            and isinstance(item["current"].get("component"), dict)
            and item["current"]["component"].get("key") == "phone"
        ]
        if len(phone) == 1:
            return phone[0], False
        findings = list_pages(
            client,
            "/api/v1/security/findings?"
            + urllib.parse.urlencode(
                {"enrollment_id": enrollment_id, "snapshot_scope": "current"}
            ),
        )
        candidates = [
            item
            for item in findings
            if isinstance(item.get("component"), dict)
            and item["component"].get("key") == "phone"
            and item.get("review") is None
        ]
        if len(candidates) == 1:
            finding_id = candidates[0].get("id")
            if not isinstance(finding_id, str) or not finding_id:
                raise SuiteError("phone SensitiveFinding id is missing")
            reviewed = _object(
                client.request(
                    "POST",
                    f"/api/v1/security/findings/{urllib.parse.quote(finding_id)}/reviews",
                    (201,),
                    {
                        "decision": "confirm",
                        "rationale": "Dedicated Online fixture phone assessment",
                    },
                ).payload,
                "SensitiveFinding review",
            )
            assessment = _object(reviewed.get("assessment"), "phone Assessment")
            return assessment, True
        time.sleep(1)
    raise SuiteError("phone Assessment was not available before the deadline")


def build_schema_locator(engine_id: int, item: Mapping[str, object]) -> str:
    node_id = positive_int(item.get("node_id"), "target schema node id")
    query = urllib.parse.urlencode({"type": "schema", "node_id": node_id})
    return (
        f"addp://engine/{engine_id}/path/"
        f"{urllib.parse.quote(SOURCE_SCHEMA, safe='')}?{query}"
    )


def service_payload(service_name: str, engine_id: int, locator: str) -> dict[str, object]:
    return {
        "service_name": service_name,
        "title": "ProtectionExemption Online fixture",
        "description": "Time-bounded field protection acceptance",
        "keywords": ["security", "online"],
        "config_type": "table",
        "engine_id": engine_id,
        "data_config": {
            "locator": locator,
            "stable_key": ["id"],
            "default_fields": ["id", "display_name", "phone"],
            "filterable_fields": ["id"],
        },
        "protocols": {"rest_api": {"enabled": True, "formats": ["json"]}},
        "public_access": False,
        "max_features": 100,
    }


def transfer_payload(
    name: str,
    source_locator: str,
    target_parent_locator: str,
) -> dict[str, object]:
    statement = (
        f'SELECT id, phone FROM "{SOURCE_SCHEMA}"."{SOURCE_TABLE}" ORDER BY id'
    )
    return {
        "name": name,
        "description": "Dedicated Online acceptance for ProtectionExemption",
        "task_type": "sync",
        "config": {
            "runtime": {"boundary": "bounded"},
            "load": {"mode": "snapshot"},
            "source": {
                "locator": source_locator,
                "data_type": "table",
                "representation": "native",
                "query": {"language": "sql", "statement": statement},
            },
            "target": {
                "parent_locator": target_parent_locator,
                "name": TARGET_TABLE,
                "data_type": "table",
                "representation": "native",
                "policy": {"apply_mode": "replace"},
            },
            "transforms": [
                {
                    "type": "field_mapping",
                    "version": "v1",
                    "mode": "project",
                    "fields": [
                        {
                            "source": "id",
                            "target": "id",
                            "target_type": "bigint",
                            "nullable": False,
                        },
                        {
                            "source": "phone",
                            "target": "phone",
                            "target_type": "string",
                            "nullable": True,
                        },
                    ],
                }
            ],
            "batch_size": 100,
        },
        "schedule": "",
        "enabled": False,
        "batch_size": 100,
        "auto_scan_metadata": False,
    }


def normalize_key(value: object) -> str:
    if isinstance(value, float) and value.is_integer():
        return str(int(value))
    return str(value)


def assert_phone_values(
    rows: Iterable[Mapping[str, object]],
    expected: Mapping[str, object],
    owner: str,
) -> dict[str, object]:
    values = {
        normalize_key(row.get("id")): row.get("phone")
        for row in rows
        if row.get("id") is not None
    }
    if values != dict(expected):
        raise SuiteError(f"{owner} phone values differ from the expected protection phase")
    return {"rows": len(values), "plaintext": values == RAW_VALUES}


def manager_values(
    client: GatewayClient, source_locator: str
) -> list[dict[str, object]]:
    _, rows = preview_rows(client, source_locator)
    return rows


def develop_values(
    client: GatewayClient,
    engine_id: int,
    source_locator: str,
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
                        f'SELECT id, display_name, phone FROM "{SOURCE_SCHEMA}"."{SOURCE_TABLE}" ORDER BY id'
                    ),
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


def service_values(
    client: GatewayClient, service_name: str
) -> list[dict[str, object]]:
    payload = _object(
        client.request(
            "POST",
            "/api/query/" + urllib.parse.quote(service_name, safe="") + "/query",
            (200,),
            {
                "select": ["id", "display_name", "phone"],
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


def transfer_values(
    client: GatewayClient,
    source_locator: str,
    target_parent_locator: str,
    target_locator: str,
    phase: str,
    run_id: str,
    deadline: float,
    task_ids: list[int],
) -> list[dict[str, object]]:
    safe_run_id = "".join(
        character if character.isalnum() else "_" for character in run_id
    )
    create_and_run_task(
        client,
        transfer_payload(
            f"{TASK_PREFIX}{safe_run_id}_{phase}", source_locator, target_parent_locator
        ),
        deadline,
        task_ids,
    )
    _, rows = preview_rows(client, target_locator)
    return rows


def owner_readers(
    client: GatewayClient,
    engine_id: int,
    source_locator: str,
    target_parent_locator: str,
    target_locator: str,
    service_name: str,
    run_id: str,
    deadline: float,
    task_ids: list[int],
) -> dict[str, Callable[[str], list[dict[str, object]]]]:
    return {
        "manager": lambda _phase: manager_values(client, source_locator),
        "develop": lambda _phase: develop_values(
            client, engine_id, source_locator, deadline
        ),
        "service": lambda _phase: service_values(client, service_name),
        "transfer": lambda phase: transfer_values(
            client,
            source_locator,
            target_parent_locator,
            target_locator,
            phase,
            run_id,
            deadline,
            task_ids,
        ),
    }


def validate_owner_phase(
    readers: Mapping[str, Callable[[str], list[dict[str, object]]]],
    phase: str,
    expected: Mapping[str, object],
) -> dict[str, object]:
    evidence: dict[str, object] = {}
    for owner in OWNER_ACTIONS:
        evidence[owner] = assert_phone_values(readers[owner](phase), expected, owner)
    return evidence


def list_exemptions(
    client: GatewayClient, enrollment_id: str
) -> list[dict[str, object]]:
    return list_pages(
        client,
        "/api/v1/security/protection-exemptions?"
        + urllib.parse.urlencode({"enrollment_id": enrollment_id}),
    )


def activate_exemptions(
    client: GatewayClient,
    enrollment_id: str,
    assessment_id: str,
    expires_at: datetime,
) -> list[dict[str, object]]:
    existing = list_exemptions(client, enrollment_id)
    activated: list[dict[str, object]] = []
    for owner, action in OWNER_ACTIONS.items():
        matches = [
            item
            for item in existing
            if item.get("assessment_id") == assessment_id
            and item.get("consumer_owner") == owner
            and item.get("action") == action
        ]
        if len(matches) > 1:
            raise SuiteError(f"multiple ProtectionExemptions exist for {owner}/{action}")
        if matches:
            version = matches[0].get("version")
            exemption_id = matches[0].get("id")
            if not isinstance(version, str) or not isinstance(exemption_id, str):
                raise SuiteError("ProtectionExemption identity or version is invalid")
            response = client.request(
                "PUT",
                f"/api/v1/security/protection-exemptions/{urllib.parse.quote(exemption_id)}",
                (200,),
                {
                    "version": version,
                    "expires_at": expires_at.isoformat(),
                    "rationale": "Dedicated Online time-bounded renewal",
                },
            )
        else:
            response = client.request(
                "POST",
                "/api/v1/security/protection-exemptions",
                (201,),
                {
                    "assessment_id": assessment_id,
                    "consumer_owner": owner,
                    "action": action,
                    "expires_at": expires_at.isoformat(),
                    "rationale": "Dedicated Online time-bounded grant",
                },
            )
        exemption = _object(response.payload, "ProtectionExemption")
        if exemption.get("effective_state") != "active":
            raise SuiteError(f"{owner}/{action} ProtectionExemption is not active")
        activated.append(exemption)
    return activated


def revoke_active_exemptions(
    client: GatewayClient, exemptions: Iterable[Mapping[str, object]]
) -> None:
    errors: list[str] = []
    for original in exemptions:
        exemption_id = original.get("id")
        if not isinstance(exemption_id, str):
            continue
        try:
            current_response = client.request(
                "GET",
                f"/api/v1/security/protection-exemptions/{urllib.parse.quote(exemption_id)}",
                (200, 404),
            )
            if current_response.status == 404:
                continue
            current = _object(current_response.payload, "ProtectionExemption cleanup")
            if current.get("effective_state") != "active":
                continue
            version = current.get("version")
            if not isinstance(version, str):
                raise SuiteError("ProtectionExemption cleanup version is invalid")
            client.request(
                "DELETE",
                f"/api/v1/security/protection-exemptions/{urllib.parse.quote(exemption_id)}",
                (200,),
                {
                    "version": version,
                    "rationale": "Dedicated Online failure cleanup",
                },
            )
        except SuiteError as error:
            errors.append(str(error))
    if errors:
        raise SuiteError("ProtectionExemption cleanup failed: " + "; ".join(errors))


def cleanup_service(client: GatewayClient, service_id: int | None) -> None:
    if service_id is None:
        return
    current = client.request(
        "GET", f"/api/v1/service/query/{service_id}", (200, 404)
    )
    if current.status == 200:
        client.request("DELETE", f"/api/v1/service/query/{service_id}", (200,))
    client.request("GET", f"/api/v1/service/query/{service_id}", (404,))


def run_scenario(
    client: GatewayClient,
    tenant_id: int,
    engine_id: int,
    run_id: str,
    timeout: float,
    exemption_seconds: int = 40,
) -> dict[str, object]:
    deadline = time.monotonic() + timeout
    identity = validate_user_identity(client, tenant_id)
    scan_execution_id = wait_for_scan(client, engine_id, deadline)
    source_item = find_item(client, engine_id, SOURCE_FULL_NAME, "table")
    target_item = find_item(client, engine_id, TARGET_FULL_NAME, "table")
    source_locator = build_item_locator(engine_id, source_item)
    target_locator = build_item_locator(engine_id, target_item)
    target_parent_locator = build_schema_locator(engine_id, target_item)
    enrollment, enrollment_initialized = ensure_enrollment(
        client, engine_id, source_locator, deadline
    )
    assessment, assessment_initialized = ensure_phone_assessment(
        client, enrollment, deadline
    )
    enrollment_id = enrollment.get("id")
    assessment_id = assessment.get("id")
    if not isinstance(enrollment_id, str) or not isinstance(assessment_id, str):
        raise SuiteError("ProtectionExemption fixture governance identity is missing")
    wait_for_enrollment(
        client,
        enrollment_id,
        deadline,
        {owner: "mask" for owner in OWNER_ACTIONS},
    )

    service_name = (
        SERVICE_PREFIX
        + "".join(character.lower() if character.isalnum() else "-" for character in run_id)
    )[:120].rstrip("-")
    service_id: int | None = None
    task_ids: list[int] = []
    exemptions: list[dict[str, object]] = []
    scenario_error: BaseException | None = None
    cleanup_errors: list[str] = []
    result: dict[str, object] = {}
    try:
        created_service = _object(
            client.request(
                "POST",
                "/api/v1/service/query",
                (201,),
                service_payload(service_name, engine_id, source_locator),
            ).payload,
            "Query Service",
        )
        service_id = positive_int(created_service.get("id"), "Query Service id")
        readers = owner_readers(
            client,
            engine_id,
            source_locator,
            target_parent_locator,
            target_locator,
            service_name,
            run_id,
            deadline,
            task_ids,
        )
        baseline = validate_owner_phase(readers, "baseline", MASKED_VALUES)

        expires_at = datetime.now(timezone.utc) + timedelta(seconds=exemption_seconds)
        exemptions = activate_exemptions(
            client, enrollment_id, assessment_id, expires_at
        )
        wait_for_enrollment(
            client,
            enrollment_id,
            min(deadline, time.monotonic() + exemption_seconds - 5),
            {owner: "allow" for owner in OWNER_ACTIONS},
        )
        exempted = validate_owner_phase(readers, "exempted", RAW_VALUES)

        remaining = (expires_at - datetime.now(timezone.utc)).total_seconds()
        if remaining > 0:
            time.sleep(remaining + 1)
        # No Security call is allowed between expiry and these reads. Each Owner
        # must evaluate valid_until locally and apply the embedded fallback.
        expired = validate_owner_phase(readers, "expired", MASKED_VALUES)
        result = {
            "schema_version": "addp.security-protection-exemption-online/v1",
            "result": "passed",
            "identity": identity,
            "fixture": {
                "engine_id": str(engine_id),
                "scan_execution_id": scan_execution_id,
                "security_enrollment_id": enrollment_id,
                "security_assessment_id": assessment_id,
                "enrollment_initialized": enrollment_initialized,
                "assessment_initialized": assessment_initialized,
            },
            "lifecycle": {
                "owners": OWNER_ACTIONS,
                "duration_seconds": exemption_seconds,
                "baseline": baseline,
                "exempted": exempted,
                "expired_without_security_refresh": expired,
            },
            "created_resources": 1 + len(task_ids),
            "deleted_resources": 1 + len(task_ids),
            "residual_resources": 0,
            "audit_revisions_retained": len(exemptions),
        }
    except BaseException as error:
        scenario_error = error
    try:
        revoke_active_exemptions(client, exemptions)
    except BaseException as error:
        cleanup_errors.append(str(error))
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
        engine_id,
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
