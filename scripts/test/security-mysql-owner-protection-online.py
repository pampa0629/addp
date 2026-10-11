#!/usr/bin/env python3
"""Accept relational and nested-document protection on disposable Hosted owners."""

from __future__ import annotations

import argparse
from datetime import datetime
import importlib.util
import json
import math
import re
import struct
import os
import signal
import subprocess
import sys
import time
import urllib.parse
import uuid
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
REGISTRATION_SPEC = importlib.util.spec_from_file_location('security_online_engine_registration', Path(__file__).with_name('online-engine-registration.py'))
REGISTRATION = importlib.util.module_from_spec(REGISTRATION_SPEC)
REGISTRATION_SPEC.loader.exec_module(REGISTRATION)

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
PHONE_DETECTOR = "addp.detector.phone_metadata/v2"
MONGODB_SOURCE = "Outdoor.Persons"
MONGODB_FIELD = "userInfo.phone"
DEFAULT_PROTECTION_PROBE_CODE = "online_default_protection_probe"
DEFAULT_PROTECTION_ROLLBACK_CODE = "online_default_protection_rollback_probe"
STRUCTURED_MASK_ALGORITHM = "addp.mask.keep_prefix_suffix/v2"
CONSTANT_ALGORITHM = "addp.mask.constant/v1"
SM3_ALGORITHM = "addp.mask.sm3/v1"
SPATIAL_SOURCE = "spatial_algorithm_source"
SPATIAL_TARGET = "spatial_algorithm_transfer"
SPATIAL_FIELDS = ["id", "value_a", "value_b", "value_c", "location_point"]
SOURCE_VALUES = {"1": "13812345678", "2": "张三abc", "3": "abc", "4": None, "5": ""}
# Independent OpenSSL SM3 vectors, including UTF-8 and the empty string.
SM3_VALUES = {
    "1": "a28b769072867c4cd1d2db46ebb6b4a93a03a1c9c2a9d9dfd14168eea03c8c0b",
    "2": "4f55976f4513cde4c9488850d4d3f8ea360e1a9565122c2e1cbc3b67e65382a4",
    "3": "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0",
    "4": None,
    "5": "1ab21d8355cfa17f8e61194831e81a8f22bec8c728fefb747ed035eb5082aa2b",
}
ALGORITHM_CASES = [
    (STRUCTURED_MASK_ALGORITHM, {"prefix_runes": 1, "suffix_runes": 1, "mask_rune": "*"}),
    (CONSTANT_ALGORITHM, {"value": "已脱敏"}),
    (SM3_ALGORITHM, {}),
]
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
    "manager.content.read",
    "manager.search.execute",
    "manager.derived_artifact.create",
    "manager.derived_artifact.read",
    "monitor.execution.read",
    "meta.catalog.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
    "security.assessment.read",
    "security.assessment.create",
    "security.detector.read",
    "security.enrollment.create",
    "security.enrollment.read",
    "security.finding.read",
    "security.finding.update",
    "security.protection_baseline.read",
    "security.protection_baseline.update",
    "security.policy.read",
    "security.policy.create",
    "security.policy.update",
    "security.policy.delete",
    "security.sensitive_data_type.create",
    "security.sensitive_data_type.delete",
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
    classification_id = positive_int(
        email_type.get("security_classification_id"),
        "email security classification id",
    )
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
        "security_classification_id": str(classification_id),
        "security_grade_id": str(grade_id),
        "detector_id": str(positive_int(bindings[0].get("id"), "email detector id")),
        "baseline_id": str(positive_int(matches[0].get("id"), "email baseline id")),
        "effect": "suppress",
    }


def _definition_matches(
    client: GatewayClient, path: str, key: str, value: object
) -> list[dict[str, object]]:
    return [item for item in definition_array(client, path) if item.get(key) == value]


def cleanup_default_protection_probe(client: GatewayClient, code: str) -> None:
    matches = _definition_matches(
        client, "/api/v1/security/sensitive-data-types", "code", code
    )
    if len(matches) > 1:
        raise SuiteError(f"default protection probe {code} is not unique")
    if not matches:
        return
    type_id = positive_int(matches[0].get("id"), f"{code} sensitive data type id")
    client.request(
        "DELETE",
        f"/api/v1/security/sensitive-data-types/{type_id}",
        (200,),
    )
    if _definition_matches(
        client, "/api/v1/security/sensitive-data-types", "code", code
    ):
        raise SuiteError(f"default protection probe {code} was not deleted")
    baselines = _definition_matches(
        client,
        "/api/v1/security/protection-baselines",
        "sensitive_data_type_id",
        str(type_id),
    )
    if baselines:
        raise SuiteError(f"default protection probe {code} left child baselines")


def default_protection_probe_payload(
    code: str,
    classification_id: int,
    grade_id: int,
    *,
    algorithm: str = STRUCTURED_MASK_ALGORITHM,
) -> dict[str, object]:
    return {
        "code": code,
        "name": "Online default protection probe",
        "description": "Disposable T4 transaction probe",
        "security_classification_id": classification_id,
        "default_security_grade_id": grade_id,
        "default_protection": {
            "effect": "mask",
            "algorithm": algorithm,
            "parameters": {"prefix_runes": 2, "suffix_runes": 2, "mask_rune": "*"},
            "allowed_algorithms": [STRUCTURED_MASK_ALGORITHM],
            "invalid_value_effect": "suppress",
        },
    }


def exercise_default_protection_transaction(
    client: GatewayClient,
    classification_id: int,
    grade_id: int,
) -> dict[str, object]:
    cleanup_default_protection_probe(client, DEFAULT_PROTECTION_PROBE_CODE)
    cleanup_default_protection_probe(client, DEFAULT_PROTECTION_ROLLBACK_CODE)
    evidence: dict[str, object] = {}
    created_type_id: int | None = None
    try:
        created = _object(
            client.request(
                "POST",
                "/api/v1/security/sensitive-data-types",
                (201,),
                default_protection_probe_payload(
                    DEFAULT_PROTECTION_PROBE_CODE, classification_id, grade_id
                ),
            ).payload,
            "default protection probe SensitiveDataType",
        )
        created_type_id = positive_int(
            created.get("id"), "default protection probe sensitive data type id"
        )
        if created.get("code") != DEFAULT_PROTECTION_PROBE_CODE:
            raise SuiteError("default protection probe returned a different code")

        current_types = _definition_matches(
            client,
            "/api/v1/security/sensitive-data-types",
            "code",
            DEFAULT_PROTECTION_PROBE_CODE,
        )
        if len(current_types) != 1 or str(current_types[0].get("id")) != str(
            created_type_id
        ):
            raise SuiteError("created default protection probe is not uniquely visible")
        baselines = _definition_matches(
            client,
            "/api/v1/security/protection-baselines",
            "sensitive_data_type_id",
            str(created_type_id),
        )
        if len(baselines) != 1:
            raise SuiteError("default protection probe must create exactly one baseline")
        baseline = baselines[0]
        if (
            str(baseline.get("security_grade_id")) != str(grade_id)
            or baseline.get("effect") != "mask"
            or baseline.get("algorithm") != STRUCTURED_MASK_ALGORITHM
            or baseline.get("parameters", {}).get("prefix_runes") != 2
            or baseline.get("parameters", {}).get("suffix_runes") != 2
            or baseline.get("invalid_value_effect") != "suppress"
            or baseline.get("enabled") is not True
        ):
            raise SuiteError("default protection probe baseline does not match the command")
        baseline_id = positive_int(
            baseline.get("id"), "default protection probe baseline id"
        )

        rollback = client.request(
            "POST",
            "/api/v1/security/sensitive-data-types",
            (400,),
            default_protection_probe_payload(
                DEFAULT_PROTECTION_ROLLBACK_CODE,
                classification_id,
                grade_id,
                algorithm="addp.mask.invalid/v1",
            ),
        )
        if _definition_matches(
            client,
            "/api/v1/security/sensitive-data-types",
            "code",
            DEFAULT_PROTECTION_ROLLBACK_CODE,
        ):
            raise SuiteError("invalid default protection left a sensitive data type")
        evidence = {
            "sensitive_data_type_id": str(created_type_id),
            "baseline_id": str(baseline_id),
            "effect": "mask",
            "algorithm": STRUCTURED_MASK_ALGORITHM,
            "invalid_request_status": rollback.status,
            "rollback_verified": True,
        }
    finally:
        cleanup_default_protection_probe(client, DEFAULT_PROTECTION_ROLLBACK_CODE)
        cleanup_default_protection_probe(client, DEFAULT_PROTECTION_PROBE_CODE)
    evidence["cleanup_verified"] = True
    return evidence


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
        raise SuiteError("the protected fixture has multiple enrollments")
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


def ensure_detected_assessment(
    client: GatewayClient, enrollment_id: str, deadline: float, component_key: str, detector: str,
) -> tuple[str, bool, str]:
    while time.monotonic() < deadline:
        findings = list_pages(
            client,
            "/api/v1/security/findings?"
            + urllib.parse.urlencode(
                {"enrollment_id": enrollment_id, "snapshot_scope": "current"}
            ),
        )
        matched_findings = [
            item
            for item in findings
            if item.get("detector_version") == detector
            and isinstance(item.get("component"), dict)
            and item["component"].get("key") == component_key
        ]
        if len(matched_findings) > 1:
            raise SuiteError("the current snapshot has multiple matching findings")
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
            and item["current"]["component"].get("key") == component_key
        ]
        if len(current) == 1:
            assessment_id = current[0].get("id")
            if not isinstance(assessment_id, str) or not assessment_id:
                raise SuiteError("detected Assessment id is missing")
            if matched_findings:
                finding_id = matched_findings[0].get("id")
                if isinstance(finding_id, str) and finding_id:
                    return assessment_id, False, finding_id
        candidates = [
            item
            for item in matched_findings
            if item.get("review") is None
        ]
        if candidates:
            finding_id = candidates[0].get("id")
            if not isinstance(finding_id, str) or not finding_id:
                raise SuiteError("detected SensitiveFinding id is missing")
            reviewed = _object(
                client.request(
                    "POST",
                    f"/api/v1/security/findings/{urllib.parse.quote(finding_id)}/reviews",
                    (201,),
                    {
                        "decision": "confirm",
                        "rationale": "Dedicated Online metadata Finding confirmation: " + component_key,
                    },
                ).payload,
                "SensitiveFinding review",
            )
            assessment = _object(reviewed.get("assessment"), "detected Assessment")
            assessment_id = assessment.get("id")
            if not isinstance(assessment_id, str) or not assessment_id:
                raise SuiteError("confirmed detected Assessment id is missing")
            return assessment_id, True, finding_id
        time.sleep(1)
    raise SuiteError("metadata finding or Assessment was not available before the deadline")


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


def run_suffix(run_id: str, separator: str) -> str:
    suffix = "".join(character.lower() if character.isascii() and character.isalnum() else separator for character in run_id).strip(separator)
    if not suffix:
        raise SuiteError("Online Run ID must contain ASCII letters or digits")
    return suffix


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
    client: GatewayClient, engine_id: int, source_locator: str, deadline: float,
    query: str = "SELECT id, customer_code, email FROM customers ORDER BY id",
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
                    "query": query,
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


def service_payload(name: str, engine_id: int, locator: str, fields: list[str] | None = None) -> dict[str, object]:
    return {
        "service_name": name,
        "title": "Security four-owner Online fixture",
        "description": "Relational field protection acceptance",
        "keywords": ["security", "online"],
        "config_type": "table",
        "engine_id": engine_id,
        "data_config": {
            "locator": locator,
            "default_fields": fields if fields is not None else ["id", "customer_code", "email"],
            "filterable_fields": ["id"],
        },
        "protocols": {"rest_api": {"enabled": True, "formats": ["json"]}},
        "public_access": False,
        "max_features": 100,
    }


def service_rows(client: GatewayClient, name: str, fields: list[str] | None = None) -> list[dict[str, object]]:
    payload = _object(
        client.request(
            "POST",
            "/api/query/" + urllib.parse.quote(name, safe="") + "/query",
            (200,),
            {
                "select": fields if fields is not None else ["id", "customer_code", "email"],
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
    name: str, source_locator: str, target_parent_locator: str, target_name: str = TARGET_TABLE
) -> dict[str, object]:
    return {
        "name": name,
        "description": "Dedicated Online relational field protection acceptance",
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
                "name": target_name,
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
        client.request("DELETE", f"/api/v1/service/query/{service_id}", (200,), {"version": _object(current.payload, "Query Service")["version"]})
    client.request("GET", f"/api/v1/service/query/{service_id}", (404,))


def spatial_expected(algorithm: str, parameters: Mapping[str, object]) -> dict[str, object]:
    if algorithm == SM3_ALGORITHM:
        return dict(SM3_VALUES)
    if algorithm == CONSTANT_ALGORITHM:
        return {key: parameters["value"] if value is not None else None for key, value in SOURCE_VALUES.items()}
    prefix, suffix = int(parameters["prefix_runes"]), int(parameters["suffix_runes"])
    return {
        key: None if value is None or len(value) <= prefix + suffix else
        value[:prefix] + str(parameters["mask_rune"]) * (len(value) - prefix - suffix) + (value[-suffix:] if suffix else "")
        for key, value in SOURCE_VALUES.items()
    }


def point_coordinates(value: object) -> tuple[float, float]:
    # Current Manager table preview uses WKT; other owner protocols expose
    # GeoJSON or native hexadecimal EWKB. All must preserve the same 2D Point.
    if isinstance(value, str) and (match := re.fullmatch(r"POINT\s*\(([^()]*)\)", value)):
        try:
            coordinates = tuple(float(part) for part in match[1].split())
            if len(coordinates) == 2 and all(math.isfinite(part) for part in coordinates):
                return coordinates
        except ValueError:
            pass
        raise SuiteError("spatial output must contain a two-dimensional Point")
    if isinstance(value, str) and value.startswith("{"):
        value = json.loads(value)
    if isinstance(value, dict) and value.get("type") == "Point":
        coordinates = value.get("coordinates")
        if isinstance(coordinates, list) and len(coordinates) == 2:
            return float(coordinates[0]), float(coordinates[1])
    if isinstance(value, str):
        try:
            raw = bytes.fromhex(value)
            if not raw or raw[0] not in (0, 1):
                raise ValueError("invalid endian marker")
            endian = "<" if raw[0] else ">"
            kind = struct.unpack_from(endian + "I", raw, 1)[0]
            if kind not in (1, 0x20000001):
                raise ValueError("expected a two-dimensional point")
            offset = 5
            if kind & 0x20000000:
                if struct.unpack_from(endian + "I", raw, 5)[0] != 4326:
                    raise ValueError("unexpected SRID")
                offset += 4
            if len(raw) != offset + 16:
                raise ValueError("unexpected EWKB length")
            return struct.unpack_from(endian + "dd", raw, offset)
        except (ValueError, struct.error):
            pass
    raise SuiteError("spatial output must contain a two-dimensional Point")


def assert_spatial_rows(rows: Iterable[Mapping[str, object]], owner: str,
                        expected: Mapping[str, Mapping[str, object]]) -> dict[str, object]:
    rows = list(rows)
    ids = [normalize_id(row.get("id")) for row in rows]
    if len(rows) != 5 or set(ids) != EXPECTED_IDS:
        raise SuiteError(f"{owner} spatial output lost or duplicated fixture rows")
    for key, row in zip(ids, rows):
        for field, values in expected.items():
            if row.get(field) != values[key] or (key == "4" and field not in row):
                raise SuiteError(f"{owner} returned an unexpected protected value for {field}")
        try:
            coordinates = point_coordinates(row.get("location_point"))
        except SuiteError as error:
            raise SuiteError(f"{owner} spatial row {key}: {error}") from error
        if coordinates != (100 + int(key), 20 + int(key)):
            raise SuiteError(f"{owner} changed the spatial coordinates")
    return {"rows": 5, "protected_fields": sorted(expected), "coordinates_preserved": True,
            "null_preserved": True}


def assert_spatial_item(item: Mapping[str, object]) -> None:
    attributes = _object(item.get("attributes"), "spatial item attributes")
    table = _object(_object(attributes.get("type_info"), "spatial type info").get("table"), "spatial table")
    fields = _array(table.get("fields"), "spatial fields")
    if len([field for field in fields if isinstance(field, dict) and field.get("name") == "location_point" and field.get("type") == "geometry"]) != 1:
        raise SuiteError("spatial fixture must retain its geometry field type")
    spatial = _object(_object(attributes.get("capabilities"), "spatial capabilities").get("spatial"), "spatial facts")
    columns = _array(spatial.get("geometry_columns"), "geometry columns")
    if spatial.get("primary_geometry_column") != "location_point" or len(columns) != 1 or not isinstance(columns[0], dict) or columns[0].get("name") != "location_point" or columns[0].get("geometry_type") != "Point" or columns[0].get("srid") != 4326:
        raise SuiteError("spatial fixture must retain its Point geometry and SRID 4326")


def wait_for_algorithm_projections(client: GatewayClient, enrollment_id: str, deadline: float) -> None:
    while time.monotonic() < deadline:
        enrollment = _object(client.request("GET", f"/api/v1/security/protection-enrollments/{enrollment_id}", (200,)).payload, "algorithm Enrollment")
        progresses = _array(enrollment.get("owner_progress"), "algorithm owner progress")
        if enrollment.get("state") == "active" and all(
            len(matches := [progress for progress in progresses if isinstance(progress, dict) and progress.get("consumer_owner") == owner]) == 1
            and matches[0].get("projection_state") == "active" and matches[0].get("acknowledged") is True
            for owner in OWNER_ACTIONS
        ):
            return
        time.sleep(1)
    raise SuiteError("algorithm projections did not converge before the deadline")


def baseline_body(baseline: Mapping[str, object]) -> dict[str, object]:
    result = {key: baseline[key] for key in ("effect", "algorithm", "parameters", "allowed_algorithms", "invalid_value_effect", "enabled")}
    for key in ("sensitive_data_type_id", "security_grade_id", "version"):
        result[key] = positive_int(baseline[key], key)
    return result


def permitted_algorithms(default_algorithm: str) -> list[str]:
    # Prefix policies need retention limits from a prefix Baseline.
    return [algorithm for algorithm, _ in ALGORITHM_CASES
            if algorithm != STRUCTURED_MASK_ALGORITHM or default_algorithm == algorithm]


def restore_algorithm_state(client: GatewayClient, baseline_id: str, original: dict[str, object],
                          policies: list[str], service_id: int | None, tasks: list[int],
                          enrollment_id: str, timeout: float, expected_version: int) -> None:
    errors: list[str] = []
    for policy_id in policies:
        try:
            path = f"/api/v1/security/protection-policies/{policy_id}"
            current = _object(client.request("GET", path, (200,)).payload, "field Policy")
            if current.get("state") == "active":
                client.request("DELETE", path, (200,), {"version": current["version"], "rationale": "Online algorithm acceptance cleanup"})
            if _object(client.request("GET", path, (200,)).payload, "revoked Policy").get("state") != "revoked":
                raise SuiteError("algorithm field Policy is still active")
        except BaseException as error:
            errors.append(str(error))
    try:
        path = f"/api/v1/security/protection-baselines/{baseline_id}"
        current = _object(client.request("GET", path, (200,)).payload, "phone Baseline")
        if positive_int(current["version"], "Baseline version") != expected_version:
            raise SuiteError("phone Baseline changed outside this run; refusing to overwrite it during cleanup")
        body = dict(original, version=positive_int(current["version"], "Baseline version"))
        client.request("PUT", path, (200,), body)
        restored = baseline_body(_object(client.request("GET", path, (200,)).payload, "restored Baseline"))
        if {k: v for k, v in restored.items() if k != "version"} != {k: v for k, v in original.items() if k != "version"}:
            raise SuiteError("phone Baseline was not restored")
        wait_for_algorithm_projections(client, enrollment_id, time.monotonic() + min(timeout, 120))
    except BaseException as error:
        errors.append(str(error))
    for cleanup in (lambda: cleanup_tasks(client, tasks), lambda: cleanup_service(client, service_id)):
        try:
            cleanup()
        except BaseException as error:
            errors.append(str(error))
    if errors:
        raise SuiteError("algorithm cleanup failed: " + "; ".join(errors))


def wait_for_technical_field(client: GatewayClient, engine_id: int, item: Mapping[str, object],
                             field_name: str, deadline: float) -> dict[str, object]:
    query = urllib.parse.urlencode({"q": field_name, "engine_id": engine_id, "page_size": 100})
    while time.monotonic() < deadline:
        response = client.request("GET", "/api/v1/manager/search?" + query, (200, 503))
        payload = _object(response.payload, "Manager field search")
        if response.status == 503:
            if payload.get("error_code") != "manager_search_isolated":
                raise SuiteError("Manager field search is unavailable")
        else:
            results = _array(_object(payload.get("data"), "Manager search data").get("results"), "Manager search results")
            hits = [_object(raw, "Manager search hit") for raw in results]
            if any(positive_int(hit.get("engine_id"), "search Engine id") != engine_id for hit in hits):
                raise SuiteError("Manager field search crossed the requested Engine")
            matches = [hit for hit in hits if hit.get("document_id") == item.get("fingerprint")]
            if len(matches) > 1:
                raise SuiteError("Manager field search duplicated the DataItem")
            if matches:
                hit = matches[0]
                locator = urllib.parse.urlsplit(str(hit.get("locator", "")))
                expected_locator = urllib.parse.urlsplit(build_item_locator(engine_id, item))
                if (hit.get("full_name") != item.get("full_name")
                        or locator[:3] != expected_locator[:3]
                        or sorted(urllib.parse.parse_qsl(locator.query)) != sorted(urllib.parse.parse_qsl(expected_locator.query))
                        or locator.fragment):
                    raise SuiteError("Manager field search changed the DataItem identity")
                fields = _array(hit.get("field_matches"), "Manager matched fields")
                matched = [_object(raw, "Manager matched field") for raw in fields
                           if isinstance(raw, dict) and raw.get("name") == field_name]
                if len(matched) != 1 or matched[0].get("data_type") != "string":
                    raise SuiteError("Manager field search lost the protected field definition")
                highlight = _object(matched[0].get("highlights"), "Manager field highlights").get("name")
                if (not isinstance(highlight, str) or "<mark>" not in highlight or "</mark>" not in highlight
                        or re.sub(r"</?mark>", "", highlight) != field_name):
                    raise SuiteError("Manager field search has no actual field-name highlight")
                return {"document_id": hit["document_id"], "engine_id": str(engine_id),
                        "field_name": field_name, "field_type": "string", "field_highlight_verified": True,
                        "same_item_verified": True}
        time.sleep(min(1, max(0, deadline - time.monotonic())))
    raise SuiteError("protected technical field was not searchable before the deadline")


def require_hosted_restart(repository: Path) -> None:
    require_hosted_initialization(os.environ)
    if sys.platform != "linux" or os.uname().machine != "x86_64" or (repository / ".env").exists():
        raise SuiteError("Security restart requires a disposable Hosted Linux checkout without .env")
    for key, expected in (("SECURITY_URL", "http://127.0.0.1:8194"),
                          ("MANAGER_URL", "http://127.0.0.1:8081")):
        if os.environ.get(key) != expected:
            raise SuiteError("Security restart refuses a non-Hosted endpoint: " + key)
    for key in ("ADDP_ONLINE_SECRET_DIR", "ADDP_ONLINE_ARTIFACT_DIR"):
        path = Path(required_environment(key))
        if not path.is_absolute() or path.resolve().is_relative_to(repository.resolve()):
            raise SuiteError("Security restart requires external evidence and credentials: " + key)


def ready_process(module: str) -> dict[str, str]:
    # Health is public; never send the consumer's token to a diagnostic endpoint.
    payload = _object(GatewayClient(required_environment(module.upper() + "_URL"), "", 30)
                      .request("GET", "/health/ready", (200,)).payload, module + " Ready")
    if (payload.get("module") != module or payload.get("status") != "ready"
            or payload.get("role") != "backend" or payload.get("registration_state") != "registered"):
        raise SuiteError(module + " is not a registered ready backend")
    fields = ("instance_id", "started_at", "build_id", "git_commit", "source_fingerprint")
    if any(not isinstance(payload.get(key), str) or not payload[key] for key in fields):
        raise SuiteError(module + " Ready has incomplete process or build identity")
    try:
        if uuid.UUID(payload["instance_id"]).int == 0 or datetime.fromisoformat(payload["started_at"]).tzinfo is None:
            raise ValueError("invalid process identity")
    except ValueError as error:
        raise SuiteError(module + " Ready has invalid process identity") from error
    return {key: payload[key] for key in fields}


def verify_restarted_process(module: str, before: Mapping[str, str], after: Mapping[str, str]) -> None:
    if (before["instance_id"] == after["instance_id"]
            or datetime.fromisoformat(after["started_at"]) <= datetime.fromisoformat(before["started_at"])):
        raise SuiteError(module + " did not restart with a new process identity and later start")
    if any(before[key] != after[key] for key in ("build_id", "git_commit", "source_fingerprint")):
        raise SuiteError(module + " changed build inputs during restart acceptance")


def exercise_restart_recovery(client: GatewayClient, engine_id: int, source: Mapping[str, object],
                              enrollment_id: str, baseline_id: str, policy_ids: list[str],
                              expected: Mapping[str, object], deadline: float, *,
                              field_name: str = "value_c", assert_rows=assert_spatial_rows) -> dict[str, object]:
    repository = Path(__file__).resolve().parents[2]
    require_hosted_restart(repository)
    tenant_id = positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "Tenant id")
    identity = validate_user_identity(client, tenant_id)
    paths = [f"/api/v1/security/protection-baselines/{baseline_id}"] + [
        f"/api/v1/security/protection-policies/{policy_id}" for policy_id in policy_ids]
    definitions = [client.request("GET", path, (200,)).payload for path in paths]
    before = {module: ready_process(module) for module in ("security", "manager")}
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise SuiteError("Security restart acceptance deadline expired before lifecycle execution")
    log_path = Path(required_environment("ADDP_ONLINE_ARTIFACT_DIR")) / "security-restart-lifecycle.log"
    # A file, rather than a PIPE, lets managed daemons inherit output without
    # keeping subprocess.run blocked after the lifecycle launcher exits.
    with log_path.open("a", encoding="utf-8") as log:
        try:
            result = subprocess.run(["bash", "scripts/dev/restart.sh", "-security", "-manager"],
                                    cwd=repository, stdout=log, stderr=subprocess.STDOUT,
                                    timeout=remaining, check=False)
        except subprocess.TimeoutExpired as error:
            raise SuiteError("Security restart lifecycle timed out; Hosted exit cleanup must run") from error
    if result.returncode != 0:
        raise SuiteError("Security restart lifecycle failed; see security-restart-lifecycle.log")
    after = {module: ready_process(module) for module in before}
    for module in before:
        verify_restarted_process(module, before[module], after[module])
    if validate_user_identity(client, tenant_id) != identity:
        raise SuiteError("Security restart changed the ordinary User or Tenant")
    if [client.request("GET", path, (200,)).payload for path in paths] != definitions:
        raise SuiteError("Security restart changed the Baseline or field Policies")
    wait_for_algorithm_projections(client, enrollment_id, deadline)
    search = wait_for_technical_field(client, engine_id, source, field_name, min(deadline, time.monotonic() + 60))
    _, rows = preview_rows(client, build_item_locator(engine_id, source))
    preview = assert_rows(rows, "manager after restart", expected)
    return {"processes": {module: {"before": before[module], "after": after[module]} for module in before},
            "same_user_verified": True, "definitions_preserved": True,
            "technical_field_search": search, "protected_preview": preview,
            "verified_before_rescan": True}


def exercise_spatial_algorithms(client: GatewayClient, engine_id: int, run_id: str,
                               deadline: float, timeout: float) -> dict[str, object]:
    types = [item for item in definition_array(client, "/api/v1/security/sensitive-data-types") if item.get("code") == "phone"]
    if len(types) != 1:
        raise SuiteError("spatial acceptance requires exactly one existing phone type")
    phone = types[0]
    matches = [item for item in definition_array(client, "/api/v1/security/protection-baselines")
               if item.get("sensitive_data_type_id") == phone["id"] and item.get("security_grade_id") == phone["default_security_grade_id"] and item.get("enabled") is True]
    if len(matches) != 1:
        raise SuiteError("spatial acceptance requires an enabled phone initial Baseline")
    baseline = matches[0]
    baseline_id = str(baseline["id"])
    original = baseline_body(baseline)
    baseline_version = positive_int(baseline["version"], "Baseline version")
    source = find_item(client, engine_id, f"{TARGET_SCHEMA}.{SPATIAL_SOURCE}", "table")
    assert_spatial_item(source)
    target = find_item(client, engine_id, f"{TARGET_SCHEMA}.{SPATIAL_TARGET}", "table")
    locator = build_item_locator(engine_id, source)
    enrollment_id, initialized = ensure_enrollment(client, engine_id, f"{TARGET_SCHEMA}.{SPATIAL_SOURCE}", locator)
    while time.monotonic() < deadline:
        enrollment = _object(client.request("GET", f"/api/v1/security/protection-enrollments/{enrollment_id}", (200,)).payload, "spatial Enrollment")
        if enrollment.get("latest_source_snapshot_hash"):
            break
        time.sleep(1)
    else:
        raise SuiteError("spatial Enrollment has no current Meta snapshot")
    components = _object(client.request("GET", f"/api/v1/security/protection-enrollments/{enrollment_id}/components", (200,)).payload, "spatial components")
    options = {_object(item, "component option")["component"]["key"]: item["component"] for item in _array(components.get("data"), "components")}
    if options.get("location_point", {}).get("value_type") != "geometry":
        raise SuiteError("Meta must identify location_point as a geometry component")
    assessments = list_pages(client, "/api/v1/security/assessments?" + urllib.parse.urlencode({"enrollment_id": enrollment_id}))
    assessed: dict[str, str] = {}
    for field in SPATIAL_FIELDS[1:4]:
        existing = [item for item in assessments if item.get("component_key") == field]
        if len(existing) > 1:
            raise SuiteError("duplicate spatial Assessment")
        if existing:
            current = _object(existing[0].get("current"), "spatial Assessment current")
            if current.get("conclusion") != "sensitive" or current.get("sensitive_data_type_id") != phone["id"] or current.get("security_grade_id") != phone["default_security_grade_id"]:
                raise SuiteError("existing spatial Assessment does not match the phone fixture")
            assessed[field] = str(existing[0]["id"])
        else:
            if options.get(field, {}).get("value_type") != "string":
                raise SuiteError("Meta must expose the spatial string component")
            current_enrollment = _object(client.request("GET", f"/api/v1/security/protection-enrollments/{enrollment_id}", (200,)).payload, "spatial Enrollment version")
            created = _object(client.request("POST", "/api/v1/security/assessments", (201,), {
                "enrollment_id": enrollment_id, "enrollment_version": current_enrollment["version"],
                "component_key": field, "sensitive_data_type_id": phone["id"], "security_grade_id": phone["default_security_grade_id"],
                "rationale": "Dedicated Online spatial scalar algorithm fixture",
            }).payload, "spatial Assessment")
            assessed[field] = str(created["id"])
    existing_policies = list_pages(client, "/api/v1/security/protection-policies")
    bound = [item for item in existing_policies if item.get("assessment_id") in assessed.values()]
    if any(item.get("state") == "active" for item in bound):
        raise SuiteError("spatial fixture has a stale active Policy; manual cleanup is required")
    policy_ids: list[str] = []
    task_ids: list[int] = []
    service_id: int | None = None
    service_name = ("addp-online-security-spatial-" + run_suffix(run_id, "-"))[:120].rstrip("-")
    evidence: list[dict[str, object]] = []
    scenario_error: BaseException | None = None
    try:
        created_service = _object(client.request("POST", "/api/v1/service/query", (201,), service_payload(service_name, engine_id, locator, SPATIAL_FIELDS)).payload, "spatial Service")
        service_id = positive_int(created_service["id"], "spatial Service id")
        for index, (algorithm, parameters) in enumerate(ALGORITHM_CASES):
            current = _object(client.request("GET", f"/api/v1/security/protection-baselines/{baseline_id}", (200,)).payload, "phone Baseline")
            if positive_int(current["version"], "Baseline version") != baseline_version:
                raise SuiteError("phone Baseline changed outside this run")
            body = dict(original, version=positive_int(current["version"], "Baseline version"), effect="mask", algorithm=algorithm, parameters=parameters,
                        allowed_algorithms=permitted_algorithms(algorithm), invalid_value_effect="suppress", enabled=True)
            updated = _object(client.request("PUT", f"/api/v1/security/protection-baselines/{baseline_id}", (200,), body).payload, "updated phone Baseline")
            baseline_version = positive_int(updated["version"], "Baseline version")
            wait_for_algorithm_projections(client, enrollment_id, deadline)
            defaults = {field: spatial_expected(algorithm, parameters) for field in assessed}
            manager_expected = dict(defaults)
            if index == 0:
                for field, (field_algorithm, field_parameters) in zip(assessed, [
                    (STRUCTURED_MASK_ALGORITHM, {"prefix_runes": 0, "suffix_runes": 0, "mask_rune": "#"}), ALGORITHM_CASES[1], ALGORITHM_CASES[2]
                ]):
                    request = {"effect": "mask", "algorithm": field_algorithm, "parameters": field_parameters, "invalid_value_effect": "suppress", "rationale": "Online independent spatial field acceptance"}
                    old = [item for item in bound if item.get("assessment_id") == assessed[field]]
                    if old:
                        path = f"/api/v1/security/protection-policies/{old[0]['id']}"
                        created = _object(client.request("PUT", path, (200,), dict(request, version=old[0]["version"])).payload, "field Policy")
                    else:
                        created = _object(client.request("POST", "/api/v1/security/protection-policies", (201,), dict(request, assessment_id=assessed[field], consumer_owner="manager", action="preview")).payload, "field Policy")
                    policy_ids.append(str(created["id"]))
                    manager_expected[field] = spatial_expected(field_algorithm, field_parameters)
                wait_for_algorithm_projections(client, enrollment_id, deadline)
            _, manager_rows = preview_rows(client, locator)
            manager = assert_spatial_rows(manager_rows, "manager", manager_expected)
            # Check immediately after protection changes, before the later target rescan.
            manager["technical_field_search"] = wait_for_technical_field(client, engine_id, source, "value_c", min(deadline, time.monotonic() + 60))
            if index == 0:
                manager["restart_recovery"] = exercise_restart_recovery(
                    client, engine_id, source, enrollment_id, baseline_id, policy_ids, manager_expected, deadline)
            query = "SELECT id, value_a, value_b, value_c, location_point FROM addp_online_security.spatial_algorithm_source ORDER BY id"
            develop = assert_spatial_rows(develop_rows(client, engine_id, locator, deadline, query), "develop", defaults)
            service = assert_spatial_rows(service_rows(client, service_name, SPATIAL_FIELDS), "service", defaults)
            _, execution = create_and_run_task(client, transfer_payload("addp_online_security_spatial_" + run_suffix(run_id, "_") + "_" + str(index), locator, build_schema_locator(engine_id, target), SPATIAL_TARGET), deadline, task_ids)
            wait_for_scan(client, engine_id, deadline)
            target = find_item(client, engine_id, f"{TARGET_SCHEMA}.{SPATIAL_TARGET}", "table")
            assert_spatial_item(target)
            _, transferred = preview_rows(client, build_item_locator(engine_id, target))
            transfer = assert_spatial_rows(transferred, "transfer", defaults)
            if positive_int(execution.get("records_written"), "spatial records_written") != 5:
                raise SuiteError("spatial Transfer must write exactly five rows")
            evidence.append({"algorithm": algorithm, "owners": {"manager": manager, "develop": develop, "service": service, "transfer": transfer}, "independent_manager_fields": index == 0})
            if index == 0:
                for policy_id in policy_ids:
                    path = f"/api/v1/security/protection-policies/{policy_id}"
                    current_policy = _object(client.request("GET", path, (200,)).payload, "field Policy version")
                    client.request("DELETE", path, (200,), {"version": current_policy["version"], "rationale": "Online field case finished"})
    except BaseException as error:
        scenario_error = error
    try:
        restore_algorithm_state(client, baseline_id, original, policy_ids, service_id, task_ids, enrollment_id, timeout, baseline_version)
    except BaseException as error:
        if scenario_error:
            raise SuiteError(f"spatial scenario failed: {scenario_error}; {error}") from error
        raise
    if scenario_error:
        raise scenario_error
    return {"source_table": f"{TARGET_SCHEMA}.{SPATIAL_SOURCE}", "enrollment_initialized": initialized,
            "cases": evidence, "baseline_restored": True, "active_policy_residuals": 0,
            "temporary_resource_residuals": 0}


def assert_mongodb_rows(rows: list[dict[str, object]], owner: str,
                        expected: Mapping[str, object]) -> dict[str, object]:
    """Validate nested values and sparse objects against independent fixture facts."""
    if len(rows) != 7 or {row.get("_id") for row in rows} != set("1234567"):
        raise SuiteError(owner + " must retain all seven MongoDB Persons exactly once")
    for row in rows:
        key = str(row["_id"])
        if MONGODB_FIELD in row or "userInfo__phone" in row:
            raise SuiteError(owner + " exposed a second flattened MongoDB phone representation")
        if row.get("displayName") != "person-" + key:
            raise SuiteError(owner + " changed the unprotected MongoDB displayName")
        if key == "7":
            if "userInfo" in row:
                raise SuiteError(owner + " synthesized the missing MongoDB parent object")
            continue
        info = _object(row.get("userInfo"), owner + " userInfo")
        if info.get("nickName") != "nickname-" + key:
            raise SuiteError(owner + " changed the sibling MongoDB nickName")
        value = expected.get(key)
        if key in SOURCE_VALUES and SOURCE_VALUES[key] is None:
            if "phone" not in info or info["phone"] is not None:
                raise SuiteError(owner + " did not preserve the null MongoDB phone at row " + key)
        elif value is None:
            if "phone" in info:
                raise SuiteError(owner + " retained a suppressed or missing MongoDB phone at row " + key)
        elif info.get("phone") != value:
            raise SuiteError(owner + " MongoDB nested phone differs from its independent expected value")
    return {"rows": 7, "nested_field_verified": True, "sparse_objects_verified": True,
            "null_values_preserved": True, "sibling_attributes_preserved": True}


def exercise_mongodb_algorithms(client: GatewayClient, engine_id: int,
                               deadline: float, timeout: float) -> dict[str, object]:
    scan = wait_for_scan(client, engine_id, deadline)
    source = find_item(client, engine_id, MONGODB_SOURCE, "collection")
    locator = build_item_locator(engine_id, source)
    enrollment_id, initialized = ensure_enrollment(client, engine_id, MONGODB_SOURCE, locator)
    assessment_id, confirmed, finding_id = ensure_detected_assessment(
        client, enrollment_id, deadline, MONGODB_FIELD, PHONE_DETECTOR)
    # Confirmed fields leave the manual-assessment candidate list. Validate the
    # Meta-derived structure frozen by the formal Assessment instead.
    assessment = _object(client.request("GET", f"/api/v1/security/assessments/{assessment_id}", (200,)).payload, "MongoDB Assessment")
    current = _object(assessment.get("current"), "MongoDB Assessment current revision")
    component = _object(current.get("component"), "MongoDB assessed component")
    if (assessment.get("id") != assessment_id or assessment.get("enrollment_id") != enrollment_id
            or current.get("conclusion") != "sensitive" or component.get("key") != MONGODB_FIELD
            or component.get("value_type") != "string"
            or component.get("path") != [{"name": "userInfo", "container": "object"}, {"name": "phone", "container": "scalar"}]
            or not isinstance(component.get("schema_fingerprint"), str)
            or not component["schema_fingerprint"].startswith("sha256:")):
        raise SuiteError("Meta-derived Assessment must bind the actual nested userInfo.phone string component")
    phones = [item for item in definition_array(client, "/api/v1/security/sensitive-data-types") if item.get("code") == "phone"]
    if len(phones) != 1:
        raise SuiteError("MongoDB acceptance requires exactly one phone type")
    phone = phones[0]
    baselines = [item for item in definition_array(client, "/api/v1/security/protection-baselines")
                 if item.get("sensitive_data_type_id") == phone["id"]
                 and item.get("security_grade_id") == phone["default_security_grade_id"] and item.get("enabled") is True]
    if len(baselines) != 1:
        raise SuiteError("MongoDB acceptance requires an enabled phone Baseline")
    baseline = baselines[0]
    original, baseline_id = baseline_body(baseline), str(baseline["id"])
    version = positive_int(baseline["version"], "Baseline version")
    if any(item.get("assessment_id") == assessment_id and item.get("state") == "active"
           for item in list_pages(client, "/api/v1/security/protection-policies")):
        raise SuiteError("MongoDB fixture has a stale active field Policy")
    policies: list[str] = []
    evidence: list[dict[str, object]] = []
    scenario_error: BaseException | None = None
    try:
        for index, (algorithm, parameters) in enumerate(ALGORITHM_CASES):
            path = f"/api/v1/security/protection-baselines/{baseline_id}"
            current = _object(client.request("GET", path, (200,)).payload, "phone Baseline")
            if positive_int(current["version"], "Baseline version") != version:
                raise SuiteError("phone Baseline changed outside this run")
            body = dict(original, version=version, effect="mask", algorithm=algorithm,
                        parameters=parameters, allowed_algorithms=permitted_algorithms(algorithm),
                        invalid_value_effect="suppress", enabled=True)
            version = positive_int(_object(client.request("PUT", path, (200,), body).payload, "updated Baseline")["version"], "Baseline version")
            wait_for_algorithm_projections(client, enrollment_id, deadline)
            expected = spatial_expected(algorithm, parameters)
            _, rows = preview_rows(client, locator)
            case = {"algorithm": algorithm, "manager": assert_mongodb_rows(rows, "manager", expected),
                    "technical_field_search": wait_for_technical_field(client, engine_id, source, MONGODB_FIELD, min(deadline, time.monotonic() + 60))}
            if index == 0:
                policy = _object(client.request("POST", "/api/v1/security/protection-policies", (201,), {
                    "assessment_id": assessment_id, "consumer_owner": "manager", "action": "preview",
                    "effect": "mask", "algorithm": SM3_ALGORITHM, "parameters": {},
                    "invalid_value_effect": "suppress", "rationale": "Online nested field independent algorithm acceptance",
                }).payload, "nested field Policy")
                policies.append(str(policy["id"]))
                wait_for_algorithm_projections(client, enrollment_id, deadline)
                _, rows = preview_rows(client, locator)
                case["independent_manager_policy"] = assert_mongodb_rows(rows, "manager independent Policy", SM3_VALUES)
                case["restart_recovery"] = exercise_restart_recovery(client, engine_id, source, enrollment_id,
                    baseline_id, policies, SM3_VALUES, deadline, field_name=MONGODB_FIELD, assert_rows=assert_mongodb_rows)
                policy_path = f"/api/v1/security/protection-policies/{policies[0]}"
                current_policy = _object(client.request("GET", policy_path, (200,)).payload, "nested field Policy")
                client.request("DELETE", policy_path, (200,), {"version": current_policy["version"], "rationale": "Online nested field case finished"})
                wait_for_algorithm_projections(client, enrollment_id, deadline)
                _, rows = preview_rows(client, locator)
                case["baseline_after_policy_revocation"] = assert_mongodb_rows(rows, "manager after Policy revocation", expected)
            evidence.append(case)
    except BaseException as error:
        scenario_error = error
    try:
        restore_algorithm_state(client, baseline_id, original, policies, None, [], enrollment_id, timeout, version)
    except BaseException as error:
        if scenario_error:
            raise SuiteError(f"MongoDB scenario failed: {scenario_error}; {error}") from error
        raise
    if scenario_error:
        raise scenario_error
    return {"source_collection": MONGODB_SOURCE, "component_key": MONGODB_FIELD,
            "component": component,
            "scan_execution_id": scan, "enrollment_initialized": initialized,
            "finding_id": finding_id, "assessment_id": assessment_id, "finding_confirmed": confirmed,
            "detector": PHONE_DETECTOR, "all_owner_projections_acknowledged": True,
            "cases": evidence, "baseline_restored": True, "active_policy_residuals": 0}


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
    default_protection_transaction = exercise_default_protection_transaction(
        client,
        positive_int(
            governance.get("security_classification_id"),
            "governance security classification id",
        ),
        positive_int(governance.get("security_grade_id"), "governance security grade id"),
    )
    source_scan = wait_for_scan(client, source_engine_id, deadline)
    target_scan = wait_for_scan(client, target_engine_id, deadline)
    spatial_algorithms = exercise_spatial_algorithms(client, target_engine_id, run_id, deadline, timeout)
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
    assessment_id, assessment_initialized, finding_id = ensure_detected_assessment(
        client, enrollment_id, deadline, "email", EMAIL_DETECTOR,
    )
    projections = wait_for_owner_projections(client, enrollment_id, deadline)

    safe_run_id = run_suffix(run_id, "-")
    service_name = (SERVICE_PREFIX + safe_run_id)[:120].rstrip("-")
    task_name = TASK_PREFIX + run_suffix(run_id, "_")
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
            "schema_version": "addp.security-mysql-owner-protection-online/v5",
            "result": "passed",
            "identity": identity,
            "governance": governance,
            "default_protection_transaction": default_protection_transaction,
            "spatial_algorithms": spatial_algorithms,
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
            "created_resources": 3 + len(task_ids),
            "deleted_resources": 3 + len(task_ids),
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


def handle_termination(signum: int, frame: object) -> None:
    raise SuiteError("Online Security acceptance interrupted; restoring owned resources")


require_hosted_initialization = SUPPORT.require_hosted_initialization


def initialize_source_grants(
    authorizer: GatewayClient, consumer: GatewayClient, tenant_id: int,
    postgres_engine_id: int, mysql_engine_id: int, mysql_database: str, mongodb_engine_id: int,
) -> None:
    """Prepare precise reads through System's sole independent Grant command."""
    validate_user_identity(consumer, tenant_id)
    targets = [
        (postgres_engine_id, "schema", TARGET_SCHEMA, SPATIAL_SOURCE),
        (postgres_engine_id, "schema", TARGET_SCHEMA, SPATIAL_TARGET),
        (postgres_engine_id, "schema", TARGET_SCHEMA, TARGET_TABLE),
        (mysql_engine_id, "database", mysql_database, SOURCE_TABLE),
    ]
    try:
        REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, tenant_id, targets)
        REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, tenant_id,
            [(mongodb_engine_id, "database", "Outdoor", "Persons")], item_term="collection")
    except REGISTRATION.RegistrationError as error:
        raise SuiteError(str(error)) from error


def initialize_governance(client: GatewayClient, tenant_id: int) -> None:
    SUPPORT.initialize_fresh_governance(client, tenant_id, [
        {"code": "email", "protection": {"effect": "suppress", "invalid_value_effect": "suppress"}, "detector": EMAIL_DETECTOR},
        {"code": "phone", "detector": PHONE_DETECTOR, "protection": {"effect": "mask", "algorithm": STRUCTURED_MASK_ALGORITHM,
            "parameters": {"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"},
            "allowed_algorithms": [case[0] for case in ALGORITHM_CASES], "invalid_value_effect": "suppress"}},
    ])


def validate_export_browser_report(payload: object, run_id: str, tenant_id: str) -> dict[str, object]:
    report = _object(payload, "Manager export browser report")
    expected = {
        "schema_version": "addp.security-manager-export-browser/v3", "result": "passed",
        "run_id": run_id, "tenant_id": tenant_id, "records": 5,
        "email_field_present": False, "non_sensitive_fields_preserved": True,
        "same_user_verified": True, "initiator_verified": True, "taskless_execution": True,
        "manager_source_verified": True, "monitor_detail_visible": True,
        "protected_preview_verified": True, "technical_field_search_verified": True,
        "search_to_preview_verified": True,
        "browser_warning_errors": 0, "failed_business_responses": 0, "anonymous_refresh_401": 1,
    }
    mismatches = [key for key, value in expected.items()
                  if type(report.get(key)) is not type(value) or report.get(key) != value]
    if set(report) != set(expected) | {"execution_id"}:
        mismatches.append("report_fields")
    execution_id = report.get("execution_id")
    try:
        valid_uuid = (isinstance(execution_id, str) and str(uuid.UUID(execution_id)) == execution_id
                      and uuid.UUID(execution_id).int != 0)
    except ValueError:
        valid_uuid = False
    if not valid_uuid:
        mismatches.append("execution_id")
    if mismatches:
        raise SuiteError("Manager export browser report mismatch: " + ", ".join(mismatches))
    return report


def run_export_browser(repository: Path, environment: Mapping[str, str], locator: str) -> dict[str, object]:
    required = ("ADDP_ONLINE_ARTIFACT_DIR", "ADDP_ONLINE_TEST_RUN_ID", "ADDP_ONLINE_TEST_TENANT_ID",
                "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN", "ADDP_ONLINE_TEST_USER_USERNAME",
                "ADDP_ONLINE_TEST_USER_PASSWORD", "GATEWAY_URL", "CONSOLE_URL")
    missing = [key for key in required if not environment.get(key)]
    if missing:
        raise SuiteError("Manager export browser environment is missing: " + ", ".join(missing))
    report_path = Path(environment["ADDP_ONLINE_ARTIFACT_DIR"]) / "security-manager-export-browser.json"
    report_path.unlink(missing_ok=True)
    screenshots = [report_path.parent / name for name in (
        "security-manager-protected-preview.png", "security-manager-field-search.png",
        "security-manager-export.png", "security-manager-export-monitor.png",
    )]
    for screenshot in screenshots:
        screenshot.unlink(missing_ok=True)
    browser_environment = dict(environment, ADDP_ONLINE_SECURITY_EXPORT_LOCATOR=locator)
    result = subprocess.run(
        ["npm", "exec", "--", "playwright", "test", "--config=playwright.online.config.js",
         "e2e/online/security-manager-export.spec.js"],
        cwd=repository / "console/frontend", env=browser_environment, text=True, capture_output=True,
    )
    if result.stdout:
        print(result.stdout, end="" if result.stdout.endswith("\n") else "\n")
    if result.stderr:
        print(result.stderr, end="" if result.stderr.endswith("\n") else "\n", file=sys.stderr)
    if result.returncode:
        raise SuiteError(f"Manager export Playwright exited with status {result.returncode}")
    if not report_path.is_file():
        raise SuiteError("Playwright did not write security-manager-export-browser.json")
    if any(not screenshot.is_file() or screenshot.stat().st_size == 0 for screenshot in screenshots):
        raise SuiteError("Manager protection browser screenshots are missing or empty")
    try:
        report = json.loads(report_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as error:
        raise SuiteError("Manager export browser report is not valid JSON") from error
    return validate_export_browser_report(report, environment["ADDP_ONLINE_TEST_RUN_ID"],
                                         environment["ADDP_ONLINE_TEST_TENANT_ID"])


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--initialize", action="store_true", help="initialize fresh Hosted governance and exact table read Grants through formal APIs")
    arguments = parser.parse_args()
    signal.signal(signal.SIGTERM, handle_termination)
    if arguments.initialize:
        require_hosted_initialization(os.environ)
        initialize_governance(GatewayClient(required_environment("GATEWAY_URL"),
            required_environment("ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN"), 30),
            positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "Tenant id"))
        initialize_source_grants(
            GatewayClient(required_environment("GATEWAY_URL"), required_environment("ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"), 30),
            GatewayClient(required_environment("GATEWAY_URL"), required_environment("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"), 30),
            positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "Tenant id"),
            positive_int(required_environment("ADDP_ONLINE_TEST_ENGINE_ID"), "PostgreSQL Engine id"),
            positive_int(required_environment("ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID"), "MySQL Engine id"),
            required_environment("ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE"),
            positive_int(required_environment("ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID"), "MongoDB Engine id"),
        )
        print("Disposable Security governance and five exact record read Grants are ready")
        return 0
    if os.environ.get("ADDP_ONLINE_TEST") != "1":
        raise SuiteError("ADDP_ONLINE_TEST must be exactly 1")
    require_hosted_restart(Path(__file__).resolve().parents[2])
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
    mongodb_engine_id = positive_int(required_environment("ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID"), "MongoDB Engine id")
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
    report["mongodb_algorithms"] = exercise_mongodb_algorithms(client, mongodb_engine_id,
        time.monotonic() + timeout, timeout)
    source_item = find_item(client, source_engine_id,
                            f"{required_environment('ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE')}.{SOURCE_TABLE}", "table")
    # Projection changes and owned-resource cleanup queue external index purges.
    # Require the final protected field to be searchable before browser entry.
    report["manager_export_search_readiness"] = wait_for_technical_field(
        client, source_engine_id, source_item, "email", time.monotonic() + min(timeout, 120))
    report["manager_export_browser"] = run_export_browser(
        Path(__file__).resolve().parents[2], dict(os.environ), build_item_locator(source_engine_id, source_item))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SuiteError as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
