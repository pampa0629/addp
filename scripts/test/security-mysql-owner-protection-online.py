#!/usr/bin/env python3
"""Accept relational and nested-document protection on disposable Hosted owners."""

from __future__ import annotations

import argparse
import csv
import hashlib
import io
from datetime import datetime, timezone, timedelta
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
from typing import Any, Callable, Iterable, Mapping


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
BANK_DETECTOR = "addp.detector.bank_card_number_metadata/v1"
BANK_SOURCE = "bank_card_accounts"
BANK_TARGET = "bank_card_transfer"
BANK_FIELD = "bank_card_number"
BANK_FIELDS = ["id", "customer_code", BANK_FIELD, "bank_account_no",
               "card_id", "bank_card_verified", "credit_card_number"]
# Independent fixture expectations; no product algorithm is used to calculate them.
BANK_SOURCE_VALUES = {"1": "6222000000000000001", "2": "测试卡abc", "3": "abc", "4": None, "5": ""}
BANK_MASKED = {"1": "6" + "*" * 17 + "1", "2": "测****c", "3": "a*c", "4": None, "5": None}
MONGODB_SOURCE = "Outdoor.Persons"
MONGODB_FIELD = "userInfo.phone"
MONGODB_TRANSFER_TARGETS = ("mongodb_query_transfer", "mongodb_query_control", "mongodb_query_denied")
MONGODB_TRANSFER_FIELDS = ("_id", "display_name", "userInfo__phone", "nick_name")
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
# Graph acknowledges protection but does not execute these tabular masking probes.
PROTECTION_OWNERS = (*OWNER_ACTIONS, "graph")
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
    "security.enrollment.update",
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
    "service.definition.update",
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



def utc_timestamp(value: object, field: str) -> datetime:
    if not isinstance(value, str):
        raise SuiteError(field + " must be an ISO 8601 timestamp with a timezone")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise SuiteError(field + " must be an ISO 8601 timestamp with a timezone") from error
    if parsed.tzinfo is None or parsed.utcoffset() is None:
        raise SuiteError(field + " must be an ISO 8601 timestamp with a timezone")
    return parsed.astimezone(timezone.utc)


def rediscovery_owner_progress(enrollment: Mapping[str, object]) -> dict[str, dict[str, object]]:
    progress = _array(enrollment.get("owner_progress"), "rediscovery owner progress")
    result = {}
    for owner, action in OWNER_ACTIONS.items():
        matches = [item for item in progress if isinstance(item, dict) and item.get("consumer_owner") == owner]
        if len(matches) != 1:
            raise SuiteError("rediscovery requires one projection per owner: " + owner)
        item = matches[0]
        revision = item.get("revision")
        if not isinstance(revision, str) or re.fullmatch(r"[0-9]{20}", revision) is None or int(revision) <= 0:
            raise SuiteError("rediscovery projection revision is invalid: " + owner)
        if item.get("projection_state") != "active":
            raise SuiteError("rediscovery projection is not active: " + owner)
        for key in ("projection_id", "published_cursor"):
            if not isinstance(item.get(key), str) or not item[key]:
                raise SuiteError("rediscovery projection " + key + " is missing: " + owner)
        rules = _array(item.get("rules"), "rediscovery projection rules")
        if (action, "suppress") not in {(rule.get("action"), rule.get("effect")) for rule in rules if isinstance(rule, dict)}:
            raise SuiteError("rediscovery lost the email suppression rule: " + owner)
        result[owner] = {key: item.get(key) for key in ("projection_id", "revision", "published_cursor", "acknowledged", "acknowledged_at")}
    return result


def exercise_explicit_rediscovery(client: GatewayClient, enrollment_id: str,
                                 probes: Mapping[str, Callable[[], dict[str, object]]],
                                 deadline: float) -> dict[str, object]:
    if set(probes) != set(OWNER_ACTIONS):
        raise SuiteError("rediscovery must verify all four actual output owners")
    path = "/api/v1/security/protection-enrollments/" + urllib.parse.quote(enrollment_id, safe="")
    before = _object(client.request("GET", path, (200,)).payload, "rediscovery enrollment before")
    version = positive_int(before.get("version"), "rediscovery enrollment version")
    previous = rediscovery_owner_progress(before)
    if before.get("id") != enrollment_id or before.get("state") != "active" or any(item["acknowledged"] is not True for item in previous.values()):
        raise SuiteError("rediscovery requires a fully synchronized active enrollment")
    discovered_before = utc_timestamp(before.get("last_discovered_at"), "previous discovery time")
    snapshot = before.get("latest_source_snapshot_hash")
    if not isinstance(snapshot, str) or not snapshot:
        raise SuiteError("rediscovery source snapshot is missing")
    created = _object(client.request("POST", path + "/discovery-executions", (201,), {"version": version}).payload, "rediscovery execution")
    execution_id = created.get("execution_id")
    try:
        if str(uuid.UUID(str(execution_id))) != execution_id:
            raise ValueError
    except (ValueError, TypeError, AttributeError) as error:
        raise SuiteError("rediscovery execution UUID is invalid") from error
    created_at = utc_timestamp(created.get("created_at"), "rediscovery creation time")
    if (created.get("enrollment_id") != enrollment_id or created.get("status") != "pending"
            or positive_int(created.get("enrollment_version"), "created enrollment version") != version + 1
            or execution_id == before.get("latest_discovery_execution_id") or created_at < discovered_before):
        raise SuiteError("rediscovery execution does not match the requested enrollment/version")
    while time.monotonic() < deadline:
        current = _object(client.request("GET", path, (200,)).payload, "rediscovery enrollment after")
        if current.get("id") != enrollment_id or current.get("state") != "active":
            raise SuiteError("rediscovery changed the protected enrollment identity/state")
        latest = current.get("latest_discovery_execution_id")
        if latest not in (before.get("latest_discovery_execution_id"), execution_id):
            raise SuiteError("rediscovery completed a different execution")
        if latest != execution_id:
            time.sleep(1)
            continue
        if (positive_int(current.get("version"), "completed enrollment version") < version + 2
                or current.get("latest_source_snapshot_hash") != snapshot
                or _object(current.get("discovery_summary"), "rediscovery summary").get("status") != "completed"):
            raise SuiteError("rediscovery completion facts differ")
        discovered_at = utc_timestamp(current.get("last_discovered_at"), "rediscovery completion time")
        if discovered_at <= discovered_before or discovered_at < created_at:
            raise SuiteError("rediscovery did not advance the latest discovery time")
        renewed = rediscovery_owner_progress(current)
        for owner, item in renewed.items():
            if (item["projection_id"] != previous[owner]["projection_id"]
                    or item["revision"] <= previous[owner]["revision"]
                    or item["published_cursor"] == previous[owner]["published_cursor"]):
                raise SuiteError("rediscovery did not publish a newer stable owner projection: " + owner)
        if not all(item["acknowledged"] is True for item in renewed.values()):
            time.sleep(1)
            continue
        for owner, item in renewed.items():
            if utc_timestamp(item["acknowledged_at"], "rediscovery acknowledgement time") < discovered_at:
                raise SuiteError("rediscovery acknowledgement predates the new projection: " + owner)
        outputs = {owner: probe() for owner, probe in probes.items()}
        return {"execution_id": execution_id, "enrollment_id": enrollment_id,
                "version_before": version, "version_after": current["version"],
                "created_at": created["created_at"], "last_discovered_at": current["last_discovered_at"],
                "last_discovered_at_before": before["last_discovered_at"],
                "previous_discovery_execution_id": before["latest_discovery_execution_id"],
                "source_snapshot_hash": snapshot, "source_snapshot_unchanged": True, "projections_before": previous, "projections_after": renewed,
                "all_current_revisions_acknowledged": True, "outputs": outputs}
    raise SuiteError("explicit rediscovery did not synchronize output owner revisions before the deadline")


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
    if len(rows) != 5 or ids != EXPECTED_IDS:
        raise SuiteError(f"{owner} did not return the five expected non-sensitive rows")
    return {"rows": len(rows), "email_field_present": False}



def assert_mysql_customer_rows(rows: Iterable[Mapping[str, object]], owner: str,
                               columns: Iterable[str] | None = None) -> dict[str, object]:
    rows = list(rows)
    output = assert_email_suppressed(rows, owner, columns)
    if columns is not None and "customer_code" not in columns or any(
            row.get("customer_code") != "CUST-1000" + normalize_id(row["id"]) for row in rows):
        raise SuiteError(owner + " changed non-sensitive customer fields")
    return {**output, "customer_codes_verified": True}


def develop_execution(
    client: GatewayClient, engine_id: int, source_locator: str, deadline: float,
    query: str, query_type: str, *, choices: list[dict[str, object]] | None = None,
) -> dict[str, object]:
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
                    "query_type": query_type,
                    "target_locator": source_locator,
                    "query_parameters": [],
                    **({"protection_choices": choices} if choices is not None else {}),
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
        if status in TERMINAL_STATUSES:
            return execution
        time.sleep(1)
    raise SuiteError("Develop execution did not finish before the deadline")


def develop_rows(
    client: GatewayClient, engine_id: int, source_locator: str, deadline: float,
    query: str = "SELECT id, customer_code, email FROM customers ORDER BY id",
    query_type: str = "sql",
) -> list[dict[str, object]]:
    execution = develop_execution(client, engine_id, source_locator, deadline, query, query_type)
    if execution.get("status") != "success":
        raise SuiteError(f"Develop execution ended with status {execution.get('status')}")
    metadata = _object(execution.get("metadata"), "Develop execution metadata")
    result = _object(metadata.get("result"), "Develop query result")
    summary = _object(result.get("summary"), "Develop query result summary")
    return [row for row in _array(summary.get("preview_rows"), "Develop preview rows") if isinstance(row, dict)]


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
        if enrollment.get("state") == "active" and len(progresses) == len(PROTECTION_OWNERS) and all(
            len(matches := [progress for progress in progresses if isinstance(progress, dict) and progress.get("consumer_owner") == owner]) == 1
            and matches[0].get("projection_state") == "active" and matches[0].get("acknowledged") is True
            for owner in PROTECTION_OWNERS
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


def manager_search_results(client: GatewayClient, engine_id: int, field_name: str) -> list[object] | None:
    query = urllib.parse.urlencode({"q": field_name, "engine_id": engine_id, "page_size": 100})
    response = client.request("GET", "/api/v1/manager/search?" + query, (200, 403, 503))
    payload = _object(response.payload, "Manager field search")
    if response.status == 403:
        if payload.get("error_code") != "security_protection_required":
            raise SuiteError("Manager field search was denied")
        return None
    if response.status == 503:
        if payload.get("error_code") != "manager_search_isolated":
            raise SuiteError("Manager field search is unavailable")
        return None
    return _array(_object(payload.get("data"), "Manager search data").get("results"), "Manager search results")


def wait_for_manager_search_ready(client: GatewayClient, engine_id: int, field_name: str, deadline: float) -> None:
    # Meta's initial indexing write uses the same durable protection boundary.
    # Wait before scanning: a rejected initial batch cannot be repaired by
    # merely polling the field search after the scan has finished.
    while time.monotonic() < deadline:
        if manager_search_results(client, engine_id, field_name) is not None:
            return
        time.sleep(min(1, max(0, deadline - time.monotonic())))
    raise SuiteError("Manager protection/search boundary was not ready before the initial scan")


def wait_for_technical_field(client: GatewayClient, engine_id: int, item: Mapping[str, object],
                             field_name: str, deadline: float) -> dict[str, object]:
    while time.monotonic() < deadline:
        results = manager_search_results(client, engine_id, field_name)
        if results is not None:
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
    raise SuiteError(f"protected technical field was not searchable before the deadline: Engine {engine_id}, {item.get('full_name')}, {field_name}")


def exercise_owner_unavailability(client: GatewayClient, tenant_id: int,
                                  probes: Mapping[str, Callable[[], dict[str, object]]], deadline: float) -> dict[str, object]:
    repository = Path(__file__).resolve().parents[2]
    SUPPORT.require_hosted_security_lifecycle(repository)
    identity = validate_user_identity(client, tenant_id)
    security_before = SUPPORT.ready_process("security")
    owners_before = {owner: SUPPORT.ready_process(owner) for owner in OWNER_ACTIONS}
    outputs: dict[str, object] = {}
    refusals: list[dict[str, str]] = []
    try:
        SUPPORT.run_security_lifecycle(repository, "stop", deadline)
        for name, probe in probes.items():
            if time.monotonic() >= deadline:
                raise SuiteError("Owner outage output exceeded the Online deadline")
            refusals.append({"phase": name, "boundary": "before", "observed_at": SUPPORT.security_listener_refused()})
            # Never retry an incorrect output or a failed execution.
            outputs[name] = probe()
            refusals.append({"phase": name, "boundary": "after", "observed_at": SUPPORT.security_listener_refused()})
            if time.monotonic() >= deadline:
                raise SuiteError("Owner outage output exceeded the Online deadline")
            if any(SUPPORT.ready_process(owner) != before for owner, before in owners_before.items()):
                raise SuiteError("Data Owner process changed during the Security outage")
    finally:
        SUPPORT.run_security_lifecycle(repository, "start", deadline)
    security_after = SUPPORT.ready_process("security")
    SUPPORT.verify_restarted_process("security", security_before, security_after)
    if any(SUPPORT.ready_process(owner) != before for owner, before in owners_before.items()):
        raise SuiteError("Data Owner process changed during Security restoration")
    if validate_user_identity(client, tenant_id) != identity:
        raise SuiteError("Security outage changed the ordinary User, Tenant or permissions")
    return {"outputs": outputs, "connection_refusals": refusals,
            "owner_processes": owners_before, "owner_processes_unchanged": True,
            "security_process": {"before": security_before, "after": security_after},
            "same_user_verified": True}


def verify_mysql_outage_transfer(client: GatewayClient, engine_id: int, locator: str,
                                 parent_locator: str, target_locator: str, name: str,
                                 identity: Mapping[str, object], deadline: float,
                                 tasks: list[int]) -> dict[str, object]:
    columns, rows = preview_rows(client, target_locator)
    if columns != ["id"] or rows:
        raise SuiteError("MySQL outage Transfer target must start empty with its fixture schema")
    return {**verify_mysql_transfer_output(client, engine_id, locator, parent_locator,
                                          name, identity, deadline, tasks), "target_empty_before": True}


def verify_mysql_transfer_output(client: GatewayClient, engine_id: int, locator: str,
                                parent_locator: str, name: str, identity: Mapping[str, object],
                                deadline: float, tasks: list[int]) -> dict[str, object]:
    previous_tasks = set(tasks)
    task_id, execution = create_and_run_task(client,
        transfer_payload(name, locator, parent_locator), deadline, tasks)
    if task_id in previous_tasks:
        raise SuiteError("MySQL Transfer reused a previous task")
    execution_id = execution.get("execution_id")
    try:
        if str(uuid.UUID(str(execution_id))) != execution_id:
            raise ValueError
    except (ValueError, TypeError, AttributeError) as error:
        raise SuiteError("MySQL Transfer execution UUID is invalid") from error
    if (positive_int(execution.get("task_id"), "Transfer task id") != task_id
            or positive_int(execution.get("trigger_by"), "Transfer initiator") != positive_int(identity["principal_id"], "ordinary User")
            or execution.get("status") != "success"
            or nonnegative_int(execution.get("records_read"), "Transfer rows read") != 5
            or nonnegative_int(execution.get("records_written"), "Transfer rows written") != 5):
        raise SuiteError("MySQL Transfer execution provenance or counts differ")
    scan = wait_for_scan(client, engine_id, deadline)
    target = find_item(client, engine_id, f"{TARGET_SCHEMA}.{TARGET_TABLE}", "table")
    columns, rows = preview_rows(client, build_item_locator(engine_id, target))
    evidence = assert_mysql_customer_rows(rows, "Transfer protected output", columns)
    return {**evidence, "task_id": task_id, "execution_id": execution_id,
            "records_read": 5, "records_written": 5, "same_user_verified": True,
            "target_rescan_execution_id": scan,
            "target_output_verified": True}


def exercise_restart_recovery(client: GatewayClient, engine_id: int, source: Mapping[str, object],
                              enrollment_id: str, baseline_id: str, policy_ids: list[str],
                              expected: Mapping[str, object], deadline: float, *,
                              field_name: str = "value_c", assert_rows=assert_spatial_rows) -> dict[str, object]:
    repository = Path(__file__).resolve().parents[2]
    SUPPORT.require_hosted_security_lifecycle(repository)
    tenant_id = positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "Tenant id")
    identity = validate_user_identity(client, tenant_id)
    paths = [f"/api/v1/security/protection-baselines/{baseline_id}"] + [
        f"/api/v1/security/protection-policies/{policy_id}" for policy_id in policy_ids]
    definitions = [client.request("GET", path, (200,)).payload for path in paths]
    before = {module: SUPPORT.ready_process(module) for module in ("security", "manager")}
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
    after = {module: SUPPORT.ready_process(module) for module in before}
    for module in before:
        SUPPORT.verify_restarted_process(module, before[module], after[module])
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
            expected_by_owner = {owner: dict(defaults) for owner in OWNER_ACTIONS}
            manager_expected = expected_by_owner["manager"]
            if index == 0:
                field_cases = [(STRUCTURED_MASK_ALGORITHM, {"prefix_runes": 0, "suffix_runes": 0, "mask_rune": "#"}), ALGORITHM_CASES[1], ALGORITHM_CASES[2]]
                for owner_index, (owner, action) in enumerate(OWNER_ACTIONS.items()):
                    for field_index, field in enumerate(assessed):
                        field_algorithm, field_parameters = field_cases[(field_index + owner_index) % len(field_cases)]
                        if owner == "transfer" and field_algorithm == STRUCTURED_MASK_ALGORITHM:
                            field_parameters = dict(field_parameters, mask_rune="@")
                        request = {"effect": "mask", "algorithm": field_algorithm, "parameters": field_parameters, "invalid_value_effect": "suppress", "rationale": "Online independent outlet and field acceptance"}
                        old = [item for item in bound if item.get("assessment_id") == assessed[field] and item.get("consumer_owner") == owner and item.get("action") == action]
                        if old:
                            path = f"/api/v1/security/protection-policies/{old[0]['id']}"
                            created = _object(client.request("PUT", path, (200,), dict(request, version=old[0]["version"])).payload, "field Policy")
                        else:
                            created = _object(client.request("POST", "/api/v1/security/protection-policies", (201,), dict(request, assessment_id=assessed[field], consumer_owner=owner, action=action)).payload, "field Policy")
                        policy_ids.append(str(created["id"]))
                        expected_by_owner[owner][field] = spatial_expected(field_algorithm, field_parameters)
                wait_for_algorithm_projections(client, enrollment_id, deadline)
            _, manager_rows = preview_rows(client, locator)
            manager = assert_spatial_rows(manager_rows, "manager", manager_expected)
            # Check immediately after protection changes, before the later target rescan.
            manager["technical_field_search"] = wait_for_technical_field(client, engine_id, source, "value_c", min(deadline, time.monotonic() + 60))
            if index == 0:
                manager["restart_recovery"] = exercise_restart_recovery(
                    client, engine_id, source, enrollment_id, baseline_id, policy_ids, manager_expected, deadline)
            query = "SELECT id, value_a, value_b, value_c, location_point FROM addp_online_security.spatial_algorithm_source ORDER BY id"
            develop = assert_spatial_rows(develop_rows(client, engine_id, locator, deadline, query), "develop", expected_by_owner["develop"])
            service = assert_spatial_rows(service_rows(client, service_name, SPATIAL_FIELDS), "service", expected_by_owner["service"])
            _, execution = create_and_run_task(client, transfer_payload("addp_online_security_spatial_" + run_suffix(run_id, "_") + "_" + str(index), locator, build_schema_locator(engine_id, target), SPATIAL_TARGET), deadline, task_ids)
            wait_for_scan(client, engine_id, deadline)
            target = find_item(client, engine_id, f"{TARGET_SCHEMA}.{SPATIAL_TARGET}", "table")
            assert_spatial_item(target)
            _, transferred = preview_rows(client, build_item_locator(engine_id, target))
            transfer = assert_spatial_rows(transferred, "transfer", expected_by_owner["transfer"])
            if positive_int(execution.get("records_written"), "spatial records_written") != 5:
                raise SuiteError("spatial Transfer must write exactly five rows")
            evidence.append({"algorithm": algorithm, "owners": {"manager": manager, "develop": develop, "service": service, "transfer": transfer}, "independent_outlet_fields": index == 0})
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


MONGODB_FIND_QUERY = '{"find":"Persons","filter":{}}'
MONGODB_COUNT_QUERY = '{"count":"Persons","query":{}}'
MONGODB_DERIVED_QUERY = '{"aggregate":"Persons","pipeline":[{"$project":{"_id":1,"phone":{"$concat":["$userInfo.phone",""]}}}]}'


def verify_unprotected_mongodb_derived_query(client: GatewayClient, engine_id: int,
                                            locator: str, deadline: float) -> dict[str, object]:
    rows = develop_rows(client, engine_id, locator, deadline, MONGODB_DERIVED_QUERY, "mql")
    expected = [{"_id": key, "phone": SOURCE_VALUES.get(key)} for key in "1234567"]
    if sorted(rows, key=lambda row: str(row.get("_id"))) != expected:
        raise SuiteError("unprotected MongoDB derived query differs from fixture source values")
    return {"rows": 7, "source_values_verified": True}


def validate_develop_protection_refusal(execution: Mapping[str, object], stage: str, reason: str) -> dict[str, object]:
    metadata = _object(execution.get("metadata") or {}, "denied Develop metadata")
    details = _object(execution.get("error_details"), "Develop protection diagnostics")
    diagnostic = _object(details.get("diagnostic"), "Develop protection diagnostic")
    expected = {"schema_version": "addp.execution-failure/v3", "stage": stage, "reason": reason}
    if (execution.get("status") != "failed" or "result" in metadata
            or details.get("code") != "develop.query.protection_required"
            or set(details) - {"category", "code", "diagnostic"} or diagnostic != expected):
        raise SuiteError("Develop protection refusal must have the precise safe diagnostic and no result")
    return {"diagnostic_code": "develop.query.protection_required", "diagnostic_stage": stage,
            "diagnostic_reason": reason, "denied_result_absent": True}


def validate_transfer_protection_refusal(execution: Mapping[str, object], stage: str, code: str, reason: str) -> dict[str, object]:
    details = _object(execution.get("error_details"), "Transfer protection diagnostics")
    expected = {"schema_version": "addp.execution-failure/v3", "stage": stage, "reason": reason}
    if (execution.get("status") != "failed" or details.get("code") != code
            or set(details) - {"category", "code", "diagnostic"} or details.get("diagnostic") != expected):
        raise SuiteError("Transfer protection refusal must have the precise safe diagnostic")
    return {"diagnostic_code": code, "diagnostic_stage": stage, "diagnostic_reason": reason}


def validate_manager_protection_refusal(response: SUPPORT.Response) -> None:
    payload = _object(response.payload, "Manager protection refusal")
    if (response.status != 403 or set(payload) != {"error", "error_code"}
            or payload.get("error_code") != "security_protection_required"
            or not isinstance(payload.get("error"), str) or not payload["error"].strip()):
        raise SuiteError("Manager refusal must preserve its safe localized 403 contract")


def validate_service_protection_refusal(response: SUPPORT.Response) -> None:
    payload = _object(response.payload, "Service protection refusal")
    if (response.status != 403 or set(payload) != {"error", "error_code"}
            or payload.get("error_code") != "data_protection_required"
            or not isinstance(payload.get("error"), str) or not payload["error"].strip()):
        raise SuiteError("Service protection refusal must use its safe localized 403 contract")


def verify_mongodb_query_boundaries(client: GatewayClient, engine_id: int,
                                   locator: str, deadline: float) -> dict[str, object]:
    rows = develop_rows(client, engine_id, locator, deadline, MONGODB_COUNT_QUERY, "mql")
    if rows != [{"count": 7}]:
        raise SuiteError("MongoDB count must return seven without field values")
    execution = develop_execution(client, engine_id, locator, deadline, MONGODB_DERIVED_QUERY, "mql")
    evidence = validate_develop_protection_refusal(execution, "preparation", "protection_lineage_unverified")
    return {"count": 7, "derived_output_denied": True, "execution_id": execution["execution_id"], "history_result_absent": True, **evidence}


def verify_mongodb_raw_export(client: GatewayClient, locator: str, identity: Mapping[str, object],
                              expected: Mapping[str, object], deadline: float) -> dict[str, object]:
    """Validate the actual Canonical Extended JSONL artifact, never preview substitutes."""
    created = _object(client.request("POST", "/api/v1/manager/exports", (202,), {
        "source_item_locator": locator, "format": "mongodb_extended_jsonl",
        "file_name": "security-mongodb-" + uuid.uuid4().hex,
    }).payload, "MongoDB export session")
    session_id = positive_int(created.get("id"), "export session id")
    execution_id = created.get("transfer_execution_id")
    try:
        if not isinstance(execution_id, str) or str(uuid.UUID(execution_id)) != execution_id:
            raise ValueError("not a canonical UUID")
    except ValueError as error:
        raise SuiteError("MongoDB export requires a canonical Transfer execution UUID") from error
    session_path = f"/api/v1/manager/exports/{session_id}"
    while time.monotonic() < deadline:
        session = _object(client.request("GET", session_path, (200,)).payload, "MongoDB export session")
        if any(session.get(key) != value for key, value in {
            "id": session_id, "source_item_locator": locator, "format": "mongodb_extended_jsonl",
            "transfer_execution_id": execution_id,
        }.items()):
            raise SuiteError("MongoDB export session identity or source changed")
        if session.get("status") == "success":
            break
        if session.get("status") not in {"pending", "running"}:
            raise SuiteError("MongoDB raw record export did not succeed")
        time.sleep(1)
    else:
        raise SuiteError("MongoDB raw record export exceeded its deadline")
    file_path = session_path + "/file"
    if session.get("download_url") != file_path:
        raise SuiteError("MongoDB export download URL must be its authenticated session file path")
    raw = client.request("GET", file_path, (200,), raw_response=True).payload
    if not isinstance(raw, bytes) or not raw:
        raise SuiteError("MongoDB raw record export returned no file bytes")

    def unique_fields(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate JSON field")
            result[key] = value
        return result

    try:
        rows = [json.loads(line, object_pairs_hook=unique_fields) for line in raw.decode("utf-8").splitlines()]
    except (ValueError, UnicodeError) as error:
        # Do not include document values or decoder exception context in reports.
        raise SuiteError("MongoDB export is not unambiguous UTF-8 JSONL") from None
    evidence = assert_mongodb_rows(rows, "transfer raw record export", expected)
    for row in rows:
        keys = {"_id", "displayName", "sourceVersion"} | ({"userInfo"} if row["_id"] != "7" else set())
        if set(row) != keys or row.get("sourceVersion") != {"$numberLong": "9007199254740993"}:
            raise SuiteError("MongoDB export changed unprotected fields or canonical BSON int64")
        if "userInfo" in row:
            expected_keys = {"nickName"} | ({"phone"} if row["_id"] == "4" or expected.get(row["_id"]) is not None else set())
            if set(row["userInfo"]) != expected_keys:
                raise SuiteError("MongoDB export introduced unexpected nested fields")
    execution = _object(client.request("GET", "/api/v1/monitor/executions/by-execution-id/" + execution_id,
                                       (200,)).payload, "MongoDB export Monitor execution")
    if (any(execution.get(key) != value for key, value in {
            "execution_id": execution_id, "module": "transfer", "source": "manager", "status": "success",
        }.items()) or execution.get("source_task_id") is not None
            or str(execution.get("triggered_by")) != str(identity["principal_id"])
            or nonnegative_int(execution.get("records_read"), "MongoDB records read") != 7
            or nonnegative_int(execution.get("records_written"), "MongoDB records written") != 7):
        raise SuiteError("MongoDB export provenance or actual record counts differ from the ordinary user execution")
    return dict(evidence, session_id=session_id, execution_id=execution_id, downloaded_bytes=len(raw),
                canonical_int64_preserved=True, same_user_verified=True, taskless_execution=True,
                manager_source_verified=True, actual_record_counts_verified=True)


def prepare_mongodb_source(client: GatewayClient, engine_id: int, deadline: float) -> dict[str, object]:
    scan = wait_for_scan(client, engine_id, deadline)
    source = find_item(client, engine_id, MONGODB_SOURCE, "collection")
    search = wait_for_technical_field(client, engine_id, source, MONGODB_FIELD, min(deadline, time.monotonic() + 60))
    return {"scan_execution_id": scan, "item": source, "technical_field_search": search}


def prepare_mongodb_transfer_targets(client: GatewayClient, engine_id: int, deadline: float) -> dict[str, object]:
    scan = wait_for_scan(client, engine_id, deadline)
    items = {name: find_item(client, engine_id, TARGET_SCHEMA + "." + name, "table")
             for name in MONGODB_TRANSFER_TARGETS}
    locators = {name: build_item_locator(engine_id, item) for name, item in items.items()}
    for locator in locators.values():
        columns, rows = preview_rows(client, locator)
        if columns != list(MONGODB_TRANSFER_FIELDS) or rows:
            raise SuiteError("MongoDB query targets must start with the exact empty fixture structure")
    return {"scan_execution_id": scan, "parent_locator": build_schema_locator(engine_id, items[MONGODB_TRANSFER_TARGETS[0]]),
            "locators": locators}


def mongodb_transfer_payload(locator: str, targets: Mapping[str, object], target: str, derived: bool) -> dict[str, object]:
    phone = {"$concat": ["$userInfo.phone", ""]} if derived else {"$ifNull": ["$userInfo.phone", None]}
    query = {"aggregate": "Persons", "pipeline": [{"$project": {
        "_id": "$_id", "display_name": {"$ifNull": ["$displayName", None]},
        "userInfo__phone": phone, "nick_name": {"$ifNull": ["$userInfo.nickName", None]},
    }}]}
    payload = SUPPORT.task_payload("addp_online_security_mongo_" + uuid.uuid4().hex, locator,
        str(targets["parent_locator"]), target, json.dumps(query, ensure_ascii=False, separators=(",", ":")),
        [{"source": name, "target": name, "target_type": "string", "nullable": name in {"userInfo__phone", "nick_name"}}
         for name in MONGODB_TRANSFER_FIELDS])
    payload["auto_scan_metadata"] = False  # Seeded and scanned targets keep their structure.
    return payload


def assert_mongodb_transfer_rows(preview: tuple[list[str], list[dict[str, object]]],
                                 expected: Mapping[str, object]) -> dict[str, object]:
    columns, rows = preview
    if columns != list(MONGODB_TRANSFER_FIELDS) or len(rows) != 7:
        raise SuiteError("MongoDB query target has unexpected columns or row count")
    observed = {}
    for row in rows:
        key = row.get("_id")
        if (key not in set("1234567") or key in observed or set(row) != set(MONGODB_TRANSFER_FIELDS)
                or row["display_name"] != "person-" + key
                or row["nick_name"] != ("nickname-" + key if key != "7" else None)):
            raise SuiteError("MongoDB query target changed row identity or adjacent fields")
        observed[key] = row["userInfo__phone"]
    if observed != {key: expected.get(key) for key in "1234567"}:
        raise SuiteError("MongoDB query target differs from the independent algorithm values")
    return {"rows": 7, "algorithm_values_verified": True, "null_and_missing_as_sql_null": True,
            "adjacent_fields_preserved": True}


def verify_mongodb_query_transfer(client: GatewayClient, locator: str, targets: Mapping[str, object],
                                  identity: Mapping[str, object], expected: Mapping[str, object] | None,
                                  deadline: float, *, control: bool = False) -> dict[str, object]:
    denied = expected is None
    target = MONGODB_TRANSFER_TARGETS[1 if control else 2 if denied else 0]
    target_locator = str(_object(targets["locators"], "MongoDB transfer targets")[target])
    if denied:
        columns, rows = preview_rows(client, target_locator)
        if columns != list(MONGODB_TRANSFER_FIELDS) or rows:
            raise SuiteError("MongoDB denied query target is not empty before execution")
    tasks: list[int] = []
    error: BaseException | None = None
    evidence: dict[str, object] = {}
    try:
        task_id, execution = create_and_run_task(client, mongodb_transfer_payload(locator, targets, target, control or denied),
            deadline, tasks, expected_status="failed" if denied else "success")
        execution_id = execution.get("execution_id")
        try:
            if str(uuid.UUID(str(execution_id))) != execution_id:
                raise ValueError
        except (ValueError, TypeError, AttributeError) as invalid:
            raise SuiteError("MongoDB query Transfer execution UUID is invalid") from invalid
        if (positive_int(execution.get("task_id"), "query task id") != task_id
                or positive_int(execution.get("trigger_by"), "query initiator") != positive_int(identity["principal_id"], "ordinary user")
                or execution.get("status") != ("failed" if denied else "success")):
            raise SuiteError("MongoDB query execution identity or terminal status differs")
        written = nonnegative_int(execution.get("records_written"), "query records written")
        if denied:
            protection_diagnostic = validate_transfer_protection_refusal(execution, "preparation", "query_output_lineage_unresolved", "protection_lineage_unverified")
            diagnostics = _object(execution.get("error_details"), "query failure diagnostics")
            if (written != 0 or nonnegative_int(execution.get("bytes_written"), "query bytes written") != 0
                    or "result" in (execution.get("metadata") or {}) or "result" in execution
                    or diagnostics.get("category") != "execution_failed"
                    or diagnostics.get("code") != "query_output_lineage_unresolved"
                    or "error_msg" in execution or "message" in diagnostics or "details" in diagnostics):
                raise SuiteError("MongoDB derived query must fail at the protection lineage gate with zero writes")
            columns, rows = preview_rows(client, target_locator)
            if columns != list(MONGODB_TRANSFER_FIELDS) or rows:
                raise SuiteError("MongoDB denied query wrote target rows")
            evidence = {"derived_output_denied": True, "protection_failure": "query_output_lineage_unresolved",
                        "target_empty_before_and_after": True, "bytes_written": 0, **protection_diagnostic}
        else:
            if written != 7 or nonnegative_int(execution.get("records_read"), "query records read") != 7:
                raise SuiteError("MongoDB query execution must read and write all seven rows")
            evidence = assert_mongodb_transfer_rows(preview_rows(client, target_locator), expected)
            if control:
                evidence["source_values_verified"] = True
        evidence.update(execution_id=execution_id, task_id=task_id, records_written=written, same_user_verified=True)
    except BaseException as failure:
        error = failure
    try:
        cleanup_tasks(client, tasks)
    except BaseException as cleanup_error:
        raise SuiteError(f"MongoDB query task cleanup failed after {type(error).__name__ if error else 'execution'}") from cleanup_error
    if error:
        raise error
    evidence["task_deleted"] = True
    return evidence


def exercise_mongodb_algorithms(client: GatewayClient, engine_id: int, prepared: Mapping[str, object],
                               transfer_targets: Mapping[str, object],
                               deadline: float, timeout: float) -> dict[str, object]:
    identity = validate_user_identity(client, positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "Tenant id"))
    scan = prepared["scan_execution_id"]
    source = _object(prepared.get("item"), "prepared MongoDB item")
    locator = build_item_locator(engine_id, source)
    unprotected_derived = verify_unprotected_mongodb_derived_query(client, engine_id, locator, deadline)
    unprotected_transfer = verify_mongodb_query_transfer(client, locator, transfer_targets, identity,
        SOURCE_VALUES, deadline, control=True)
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
            case["query_boundaries"] = verify_mongodb_query_boundaries(client, engine_id, locator, deadline)
            case["develop"] = assert_mongodb_rows(
                develop_rows(client, engine_id, locator, deadline, MONGODB_FIND_QUERY, "mql"), "develop", expected)
            case["raw_record_export"] = verify_mongodb_raw_export(client, locator, identity, expected, deadline)
            if algorithm == SM3_ALGORITHM:
                case["security_unavailability"] = exercise_owner_unavailability(client,
                    positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "Tenant id"), {
                    "develop": lambda: assert_mongodb_rows(develop_rows(client, engine_id, locator, deadline,
                        MONGODB_FIND_QUERY, "mql"), "Develop during Security outage", expected),
                    "develop_query_boundaries": lambda: verify_mongodb_query_boundaries(client, engine_id, locator, deadline),
                    "transfer": lambda: verify_mongodb_query_transfer(client, locator, transfer_targets, identity, expected, deadline),
                    "derived_transfer_denied": lambda: verify_mongodb_query_transfer(client, locator, transfer_targets, identity, None, deadline),
                }, deadline)
            case["query_transfer"] = verify_mongodb_query_transfer(client, locator, transfer_targets, identity, expected, deadline)
            case["derived_transfer_denied"] = verify_mongodb_query_transfer(client, locator, transfer_targets, identity, None, deadline)
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
                case["develop_during_manager_policy"] = assert_mongodb_rows(
                    develop_rows(client, engine_id, locator, deadline, MONGODB_FIND_QUERY, "mql"),
                    "develop during Manager Policy", expected)
                case["export_during_manager_policy"] = verify_mongodb_raw_export(client, locator, identity, expected, deadline)
                case["transfer_during_manager_policy"] = verify_mongodb_query_transfer(client, locator, transfer_targets, identity, expected, deadline)
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
    transfers = [unprotected_transfer] + [case[key] for case in evidence
        for key in ("query_transfer", "derived_transfer_denied")]
    transfers.append(evidence[0]["transfer_during_manager_policy"])
    transfers.extend(evidence[2]["security_unavailability"]["outputs"][key]
                     for key in ("transfer", "derived_transfer_denied"))
    if (len(transfers) != 10 or len({entry["task_id"] for entry in transfers}) != 10
            or len({entry["execution_id"] for entry in transfers}) != 10
            or not all(entry["task_deleted"] for entry in transfers)):
        raise SuiteError("MongoDB query transfers require ten distinct cleaned tasks and executions")
    return {"source_collection": MONGODB_SOURCE, "component_key": MONGODB_FIELD, "enrollment_id": enrollment_id,
            "unprotected_derived_query": unprotected_derived,
            "unprotected_derived_transfer": unprotected_transfer,
            "target_scan_execution_id": transfer_targets["scan_execution_id"],
            "query_transfer_cleanup": {"created_tasks": 10, "deleted_tasks": 10, "residual_tasks": 0},
            "initial_technical_field_search": prepared["technical_field_search"],
            "component": component,
            "scan_execution_id": scan, "enrollment_initialized": initialized,
            "finding_id": finding_id, "assessment_id": assessment_id, "finding_confirmed": confirmed,
            "detector": PHONE_DETECTOR, "all_owner_projections_acknowledged": True,
            "cases": evidence, "baseline_restored": True, "active_policy_residuals": 0}


def assert_bank_card_rows(rows: Iterable[Mapping[str, object]], owner: str,
                          columns: Iterable[str] | None = None, *, protected: bool = True) -> dict[str, object]:
    rows = list(rows)
    columns = list(columns) if columns is not None else None
    ids = [normalize_id(row.get("id")) for row in rows]
    if len(rows) != 5 or set(ids) != EXPECTED_IDS:
        raise SuiteError(owner + " bank-card output lost or duplicated rows")
    if columns is not None and (len(columns) != len(BANK_FIELDS) or set(columns) != set(BANK_FIELDS)):
        raise SuiteError(owner + " bank-card output changed its field schema")
    for key, row in zip(ids, rows):
        fields = set(row)
        allowed = [set(BANK_FIELDS)]
        if key == "5" and protected:
            allowed.append(set(BANK_FIELDS) - {BANK_FIELD})
        expected = BANK_MASKED if protected else BANK_SOURCE_VALUES
        if (fields not in allowed or row.get(BANK_FIELD) != expected[key]
                or row.get("customer_code") != "CUST-1000" + key
                or row.get("bank_account_no") != "account-" + key
                or row.get("card_id") != "card-" + key
                or row.get("bank_card_verified") != "verified-" + key
                or normalize_id(row.get("credit_card_number")) != str(100 + int(key))):
            raise SuiteError(owner + " bank-card output differs from the independent fixture expectations")
    return {"rows": 5, "masked_values_verified": protected, "source_values_verified": not protected,
            "null_preserved": True, "empty_value_suppressed": protected, "non_sensitive_fields_preserved": True}


def assert_bank_card_mixed_rows(rows: Iterable[Mapping[str, object]], owner: str) -> dict[str, object]:
    rows = list(rows)
    originals = []
    for row in rows:
        key = normalize_id(row.get("id"))
        if (key not in EXPECTED_IDS or row.get("copied_card") != BANK_MASKED[key]
                or ("copied_card" in row) != (BANK_FIELD in row)
                or row.get("copied_code") != "CUST-1000" + key):
            raise SuiteError(owner + " mixed wildcard copies are not protected consistently")
        originals.append({k: v for k, v in row.items() if k not in {"copied_card", "copied_code"}})
    evidence = assert_bank_card_rows(originals, owner)
    return {**evidence, "sensitive_copies_verified": True, "non_sensitive_copy_verified": True}


def bank_card_sql_service_payload(name: str, engine_id: int, source: Mapping[str, object], locator: str) -> dict[str, object]:
    attributes = _object(source.get("attributes"), "bank-card source attributes")
    table = _object(_object(attributes.get("type_info"), "bank-card type info").get("table"), "bank-card table")
    fields = _array(table.get("fields"), "bank-card source fields")
    if not all(isinstance(field, dict) for field in fields) or [field.get("name") for field in fields] != BANK_FIELDS:
        raise SuiteError("SQL page fixture must use the actual bank-card source fields")
    contract = json.loads(json.dumps(table))
    for source_name, output_name in [(BANK_FIELD, "copied_card"), ("customer_code", "copied_code")]:
        field = next(field for field in fields if field["name"] == source_name)
        copied = json.loads(json.dumps(field))
        copied["name"] = output_name
        copied.pop("path", None)
        contract["fields"].append(copied)
    payload = service_payload(name, engine_id, locator, BANK_FIELDS + ["copied_card", "copied_code"])
    payload["config_type"] = "sql"
    payload["sql_query"] = "SELECT b.*,b." + BANK_FIELD + " AS copied_card,b.customer_code AS copied_code FROM " + BANK_SOURCE + " b WHERE b.id>=1 ORDER BY b.id LIMIT 100"
    payload["data_config"].pop("locator")
    payload["data_config"]["stable_key"] = ["id"]
    payload["output_contract"] = {"table": contract}
    return payload


def bank_card_computed_service_payload(name: str, engine_id: int, source: Mapping[str, object], locator: str) -> dict[str, object]:
    payload = bank_card_sql_service_payload(name, engine_id, source, locator)
    fields = payload["output_contract"]["table"]["fields"]
    identifier = next(field for field in fields if field["name"] == "id")
    computed = dict(next(field for field in fields if field["name"] == BANK_FIELD))
    computed["name"] = "computed"
    computed.pop("path", None)
    payload["sql_query"] = "SELECT id,substring(" + BANK_FIELD + ",1,100) AS computed FROM " + BANK_SOURCE
    payload["data_config"]["default_fields"] = ["id", "computed"]
    payload["output_contract"]["table"]["fields"] = [identifier, computed]
    return payload


def exercise_bank_card_computed_outputs(client: GatewayClient, engine_id: int, locator: str, deadline: float) -> dict[str, object]:
    queries = {
        "substring": "SELECT substring(" + BANK_FIELD + ",1,100) AS computed FROM " + BANK_SOURCE,
        "coalesce": "SELECT coalesce(" + BANK_FIELD + ",NULL) AS computed FROM " + BANK_SOURCE,
        "case": "SELECT CASE WHEN " + BANK_FIELD + " IS NULL THEN NULL ELSE " + BANK_FIELD + " END AS computed FROM " + BANK_SOURCE,
        "cast": "SELECT CAST(" + BANK_FIELD + " AS CHAR) AS computed FROM " + BANK_SOURCE,
        "subquery_alias": "SELECT w.computed FROM (SELECT substring(" + BANK_FIELD + ",1,100) AS computed FROM " + BANK_SOURCE + ") w",
        "union": "SELECT " + BANK_FIELD + " AS computed FROM " + BANK_SOURCE + " UNION ALL SELECT customer_code AS other FROM " + BANK_SOURCE,
    }
    cases = {}
    execution_ids = set()
    for name, query in queries.items():
        execution = develop_execution(client, engine_id, locator, deadline, query, "sql")
        execution_id = execution.get("execution_id")
        try:
            valid_id = isinstance(execution_id, str) and str(uuid.UUID(execution_id)) == execution_id and uuid.UUID(execution_id).int != 0
        except (ValueError, TypeError):
            valid_id = False
        if not valid_id or execution_id in execution_ids:
            raise SuiteError("computed refusal must use distinct actual execution UUIDs")
        execution_ids.add(execution_id)
        evidence = validate_develop_protection_refusal(execution, "preparation", "protection_computed_output_unsupported")
        history = _object(client.request("GET", "/api/v1/develop/executions/" + execution_id, (200,)).payload, "computed execution history")
        if history.get("execution_id") != execution_id:
            raise SuiteError("computed refusal history execution differs")
        validate_develop_protection_refusal(history, "preparation", "protection_computed_output_unsupported")
        cases[name] = {**evidence, "execution_id": execution_id, "history_result_absent": True}
    counted = develop_rows(client, engine_id, locator, deadline, "SELECT count(*) AS count FROM " + BANK_SOURCE)
    if len(counted) != 1 or set(counted[0]) != {"count"} or normalize_id(counted[0]["count"]) != "5":
        raise SuiteError("value-free count control changed")
    ordinary = develop_rows(client, engine_id, locator, deadline, "SELECT id+1 AS next_id FROM " + BANK_SOURCE + " ORDER BY id")
    if [normalize_id(row.get("next_id")) for row in ordinary] != ["2", "3", "4", "5", "6"] or any(set(row) != {"next_id"} for row in ordinary):
        raise SuiteError("unprotected calculation control changed")
    return {"cases": cases, "count_star": 5, "unprotected_calculation_rows": 5, "protected_results_absent": True}


def develop_refusal_with_history(client: GatewayClient, engine_id: int, locator: str,
                                 query: str, reason: str, deadline: float) -> dict[str, object]:
    execution = develop_execution(client, engine_id, locator, deadline, query, "sql")
    execution_id = execution.get("execution_id")
    try:
        valid = isinstance(execution_id, str) and str(uuid.UUID(execution_id)) == execution_id and uuid.UUID(execution_id).int != 0
    except (ValueError, TypeError):
        valid = False
    if not valid:
        raise SuiteError("protection refusal needs an actual execution UUID")
    evidence = validate_develop_protection_refusal(execution, "preparation", reason)
    history = _object(client.request("GET", "/api/v1/develop/executions/" + execution_id, (200,)).payload,
                      "protection refusal history")
    if history.get("execution_id") != execution_id or history.get("error_details") != execution.get("error_details"):
        raise SuiteError("protection refusal history differs from the same execution")
    validate_develop_protection_refusal(history, "preparation", reason)
    return {**evidence, "execution_id": execution_id, "history_result_absent": True}


def postgres_query_refusal_payload(locator: str, parent: str, reason: str) -> dict[str, object]:
    relation = TARGET_SCHEMA + "." + SPATIAL_SOURCE
    if reason == "protection_computed_output_unsupported":
        query = "SELECT id,CAST(value_a AS text) AS value_a FROM " + relation
    elif reason == "protection_operation_unsupported":
        query = "SELECT DISTINCT id,value_a FROM " + relation
    else:
        raise SuiteError("unsupported PostgreSQL query refusal fixture")
    payload = transfer_payload("addp_security_refusal_" + uuid.uuid4().hex, locator, parent, BANK_TARGET)
    payload["config"]["source"]["query"] = {"language": "sql", "statement": query}
    payload["config"]["transforms"] = [{"type": "field_mapping", "version": "v1", "mode": "project", "fields": [
        {"source": "id", "target": "id", "target_type": "bigint", "nullable": False},
        {"source": "value_a", "target": BANK_FIELD, "target_type": "string", "nullable": True}]}]
    return payload


def postgres_query_transfer_refusal(client: GatewayClient, engine_id: int, parent: str, target_locator: str,
                                    reason: str, deadline: float, principal_id: int) -> dict[str, object]:
    # This existing fixture is PostgreSQL and already enrolled by the earlier
    # spatial scalar scenario. MySQL does not provide query read sessions.
    source = find_item(client, engine_id, TARGET_SCHEMA + "." + SPATIAL_SOURCE, "table")
    locator = build_item_locator(engine_id, source)
    payload = postgres_query_refusal_payload(locator, parent, reason)
    evidence = transfer_refusal_with_history(client, payload, target_locator, reason, deadline, principal_id)
    return {**evidence, "query_source": {"engine_type": "postgresql", "locator": locator}}


def transfer_refusal_with_history(client: GatewayClient, payload: dict[str, object], target_locator: str,
                                  reason: str, deadline: float, principal_id: int) -> dict[str, object]:
    before = preview_rows(client, target_locator)
    tasks: list[int] = []
    try:
        _, execution = create_and_run_task(client, payload, deadline, tasks, expected_status="failed")
        execution_id = execution.get("execution_id")
        try:
            if str(uuid.UUID(str(execution_id))) != execution_id or uuid.UUID(execution_id).int == 0:
                raise ValueError()
        except (ValueError, TypeError, AttributeError) as error:
            raise SuiteError("Transfer refusal needs an actual execution UUID") from error
        evidence = validate_transfer_protection_refusal(execution, "preparation", "data_protection_required", reason)
        history = _object(client.request("GET", "/api/v1/transfer/executions/" + execution_id, (200,)).payload, "same Transfer refusal")
        if history.get("execution_id") != execution_id or history.get("error_details") != execution.get("error_details"):
            raise SuiteError("Transfer refusal history changed its execution or diagnostic")
        validate_transfer_protection_refusal(history, "preparation", "data_protection_required", reason)
        for row in (execution, history):
            if (positive_int(row.get("trigger_by"), "refusal initiator") != principal_id
                    or nonnegative_int(row.get("records_written"), "refusal writes") != 0
                    or nonnegative_int(row.get("bytes_written"), "refusal bytes") != 0
                    or "result" in (row.get("metadata") or {}) or "result" in row):
                raise SuiteError("Transfer refusal retained output or wrote data")
        if preview_rows(client, target_locator) != before:
            raise SuiteError("Transfer refusal changed its existing target")
        result = {**evidence, "execution_id": execution_id, "history_result_absent": True,
                  "records_written": 0, "bytes_written": 0, "target_unchanged": True, "same_user_verified": True,
                  "created_resources": 1, "deleted_resources": 1, "residual_resources": 0}
    finally:
        cleanup_tasks(client, tasks)
    return result


def bank_outlet_refusals(client: GatewayClient, engine_id: int, query_engine_id: int, source: Mapping[str, object], fields: list[str], locator: str,
                        parent: str, target_locator: str, reason: str, query: str | None,
                        deadline: float, principal_id: int) -> dict[str, object]:
    if reason not in {"protection_not_ready", "protection_schema_mismatch", "protection_operation_unsupported"}:
        raise SuiteError("unsupported bank outlet refusal fixture")
    outcomes = {}
    if reason != "protection_operation_unsupported":
        response = client.request("GET", "/api/v1/manager/preview?" + urllib.parse.urlencode({"locator": locator, "page": 1, "page_size": 100}), (403,))
        validate_manager_protection_refusal(response)
        outcomes["manager"] = {"http_status": 403, "error_code": "security_protection_required", "result_absent": True}
    name = "addp-security-refusal-" + uuid.uuid4().hex
    service = service_payload(name, engine_id, locator, fields)
    if reason == "protection_operation_unsupported":
        service = bank_card_computed_service_payload(name, engine_id, source, locator)
        service["sql_query"] = query
        service["data_config"]["default_fields"] = ["id", BANK_FIELD]
        service["output_contract"]["table"]["fields"][1]["name"] = BANK_FIELD
    service_id = None
    try:
        created = _object(client.request("POST", "/api/v1/service/query", (201,), service).payload, "refusal Service")
        service_id = positive_int(created.get("id"), "refusal Service ID")
        response = client.request("POST", "/api/query/" + name + "/query", (403,), {
            "select": service["data_config"]["default_fields"], "order_by": [{"field": "id", "direction": "asc"}],
            "page": {"limit": 2}, "format": "json"})
        validate_service_protection_refusal(response)
        outcomes["service"] = {"http_status": 403, "error_code": "data_protection_required", "result_absent": True}
        if query:
            outcomes["transfer"] = postgres_query_transfer_refusal(client, query_engine_id, parent, target_locator, reason, deadline, principal_id)
        else:
            payload = transfer_payload("addp_security_refusal_" + uuid.uuid4().hex, locator, parent, BANK_TARGET)
            outcomes["transfer"] = transfer_refusal_with_history(client, payload, target_locator, reason, deadline, principal_id)
    finally:
        cleanup_service(client, service_id)
    return outcomes


def exercise_unready_source(client: GatewayClient, engine_id: int, principal_id: int, deadline: float) -> dict[str, object]:
    source = find_item(client, engine_id, TARGET_SCHEMA + "." + SPATIAL_SOURCE, "table")
    target = find_item(client, engine_id, TARGET_SCHEMA + "." + BANK_TARGET, "table")
    locator = build_item_locator(engine_id, source)
    enrollment_id, initialized = ensure_enrollment(client, engine_id, source["full_name"], locator)
    if not initialized:
        raise SuiteError("unready source must be a fresh isolated enrollment")
    while time.monotonic() < deadline:
        before = _object(client.request("GET", "/api/v1/security/protection-enrollments/" + enrollment_id, (200,)).payload, "unready discovery")
        if before.get("state") not in {"activating", "enrolling"}:
            raise SuiteError("unready source left its actual enrolling lifecycle")
        summary = before.get("discovery_summary") or {}
        if summary.get("status") == "completed":
            if any(type(summary.get(key)) is not int or summary[key] != 0 for key in ("finding_count", "reviewed_count", "pending_review_count")):
                raise SuiteError("unready source discovery must complete without Findings")
            if not before.get("latest_discovery_execution_id") or not before.get("latest_source_snapshot_hash"):
                raise SuiteError("unready source has no completed discovery identity")
            break
        time.sleep(1)
    else:
        raise SuiteError("unready source discovery did not complete before the deadline")
    # No matching Findings or manual Assessments exist: discovery has already
    # completed, so empty-rule enrolling is stable rather than a Worker race.
    proof = exercise_enrolling_refusal(client, enrollment_id, engine_id, locator, deadline,
        "SELECT id,value_a FROM " + TARGET_SCHEMA + "." + SPATIAL_SOURCE)
    proof["outlets"] = bank_outlet_refusals(client, engine_id, engine_id, source, SPATIAL_FIELDS, locator,
        build_schema_locator(engine_id, target), build_item_locator(engine_id, target),
        "protection_not_ready", None, deadline, principal_id)
    after = _object(client.request("GET", "/api/v1/security/protection-enrollments/" + enrollment_id, (200,)).payload, "unready source after outlets")
    def revisions(enrollment):
        progress = [_object(p, "unready owner") for p in _array(enrollment.get("owner_progress"), "unready owners")]
        if (enrollment.get("state") != "enrolling" or len(progress) != len(PROTECTION_OWNERS)
                or {p.get("consumer_owner") for p in progress} != set(PROTECTION_OWNERS)
                or any(p.get("projection_state") != "enrolling" or p.get("rules") != [] for p in progress)):
            raise SuiteError("unready source did not retain actual empty enrolling projections")
        return {p["consumer_owner"]: (p.get("revision"), p.get("published_cursor")) for p in progress}
    if (revisions(before) != revisions(after) or any(p.get("acknowledged") is not True for p in after["owner_progress"])
            or any(before.get(key) != after.get(key) for key in ("latest_discovery_execution_id", "latest_source_snapshot_hash", "discovery_summary"))):
        raise SuiteError("unready source changed during outlet refusal acceptance")
    return {**proof, "fixture_source": {"engine_type": "postgresql", "locator": locator,
            "discovery_execution_id": after["latest_discovery_execution_id"],
            "owner_revisions": {owner: values[0] for owner, values in revisions(after).items()}},
            "discovery_completed_without_findings": True, "all_outlets_enrolling_verified": True}


def protection_reason_matrix(report: Mapping[str, object]) -> dict[str, object]:
    """Validate the closed outlet matrix; protocol references never assert HTTP coverage."""
    bank = report["bank_card_metadata"]
    reasons = ("computed_output_unsupported", "lineage_unverified", "not_ready", "expired",
               "not_yet_valid", "schema_mismatch", "operation_unsupported")
    matrix = {"protection_" + reason: {owner: {
        "level": "protocol_only", "entry": "make test-common-postgres",
        "case": "TestProjectionStoreReasonMatrixAgainstPostgres/" + owner + "/protection_" + reason,
        "scope": "shared owner-local protocol, not outlet HTTP"} for owner in OWNER_ACTIONS} for reason in reasons}
    def runtime(reason, owner, evidence):
        if owner in {"manager", "service"}:
            expected_code = "security_protection_required" if owner == "manager" else "data_protection_required"
            if evidence != {"http_status": 403, "error_code": expected_code, "result_absent": True}:
                raise SuiteError("matrix HTTP refusal differs from its closed safe contract")
            transport = "http_controlled_fixture"
        else:
            expected_code = "develop.query.protection_required" if owner == "develop" else (
                "query_output_lineage_unresolved" if reason == "protection_lineage_unverified" else "data_protection_required")
            if (evidence.get("diagnostic_reason") != reason or evidence.get("diagnostic_stage") != "preparation"
                    or evidence.get("diagnostic_code") != expected_code or evidence.get("history_result_absent") is not True):
                raise SuiteError("matrix execution lacks its precise safe historical reason")
            try:
                value = evidence.get("execution_id")
                if str(uuid.UUID(str(value))) != value or uuid.UUID(value).int == 0: raise ValueError()
            except (ValueError, TypeError, AttributeError) as error:
                raise SuiteError("matrix execution needs a canonical UUID") from error
            if owner == "transfer" and (type(evidence.get("records_written")) is not int or evidence["records_written"] != 0
                    or type(evidence.get("bytes_written")) is not int or evidence["bytes_written"] != 0
                    or evidence.get("target_unchanged") is not True or evidence.get("same_user_verified") is not True):
                raise SuiteError("matrix Transfer needs zero writes and an unchanged target")
            if owner == "develop" and evidence.get("denied_result_absent") is not True:
                raise SuiteError("matrix Develop refusal retained output")
            if owner == "transfer" and reason in {"protection_computed_output_unsupported", "protection_operation_unsupported"}:
                source = evidence.get("query_source") or {}
                locator = urllib.parse.urlsplit(str(source.get("locator", "")))
                if (source.get("engine_type") != "postgresql" or locator.scheme != "addp" or locator.netloc != "engine"
                        or not re.fullmatch(r"/[1-9][0-9]*/path/" + TARGET_SCHEMA + "/" + SPATIAL_SOURCE, locator.path)
                        or urllib.parse.parse_qs(locator.query).get("type") != ["table"]):
                    raise SuiteError("matrix SQL Transfer refusal must identify its actual PostgreSQL source")
            transport = "execution_history"
        matrix[reason][owner] = {"level": "runtime", "transport": transport, "evidence": evidence}
    refusals = bank["refusal_causes"]
    unready = refusals["not_ready"]
    source = _object(unready.get("fixture_source"), "unready matrix source")
    try:
        discovery_id = source.get("discovery_execution_id")
        if str(uuid.UUID(str(discovery_id))) != discovery_id or uuid.UUID(discovery_id).int == 0:
            raise ValueError()
    except (ValueError, TypeError, AttributeError) as error:
        raise SuiteError("unready matrix needs an actual discovery UUID") from error
    revisions = _object(source.get("owner_revisions"), "unready matrix revisions")
    locator = urllib.parse.urlsplit(str(source.get("locator", "")))
    if (source.get("engine_type") != "postgresql" or locator.scheme != "addp" or locator.netloc != "engine"
            or not re.fullmatch(r"/[1-9][0-9]*/path/" + TARGET_SCHEMA + "/" + SPATIAL_SOURCE, locator.path)
            or urllib.parse.parse_qs(locator.query).get("type") != ["table"]
            or set(revisions) != set(PROTECTION_OWNERS) or any(not isinstance(v, str) or not re.fullmatch(r"[0-9]{20}", v) for v in revisions.values())
            or unready.get("discovery_completed_without_findings") is not True or unready.get("all_outlets_enrolling_verified") is not True):
        raise SuiteError("unready matrix needs its stable actual PostgreSQL source")
    for name in ("not_ready", "schema_mismatch", "operation_unsupported"):
        proof = refusals[name]
        reason = "protection_" + name
        runtime(reason, "develop", {k:v for k,v in proof.items() if k not in {"outlets", "enrolling_acknowledged", "fixture_source", "discovery_completed_without_findings", "all_outlets_enrolling_verified"}})
        expected = {"service", "transfer"} if name == "operation_unsupported" else {"manager", "service", "transfer"}
        if set(proof["outlets"]) != expected: raise SuiteError("matrix outlet refusal is incomplete")
        for owner, evidence in proof["outlets"].items(): runtime(reason, owner, evidence)
        if name == "not_ready":
            for cell in matrix[reason].values(): cell["fixture_source"] = source
    computed = bank["computed_outputs"]
    runtime("protection_computed_output_unsupported", "develop", computed["cases"]["substring"])
    runtime("protection_computed_output_unsupported", "service", computed["service_refusal"])
    runtime("protection_computed_output_unsupported", "transfer", computed["transfer_refusal"])
    mongodb = _object(report.get("mongodb_algorithms"), "required MongoDB acceptance evidence")
    mongo_cases = _array(mongodb.get("cases"), "required MongoDB acceptance cases")
    if not mongo_cases:
        raise SuiteError("reason matrix requires actual MongoDB acceptance cases")
    mongo = _object(mongo_cases[0], "MongoDB acceptance case")
    runtime("protection_lineage_unverified", "develop", mongo["query_boundaries"])
    derived = mongo["derived_transfer_denied"]
    runtime("protection_lineage_unverified", "transfer", {**derived,
        "history_result_absent": True, "target_unchanged": derived["target_empty_before_and_after"]})
    expired = report["resource_validity"]["expired_outputs"]
    for owner in ("manager", "service"):
        runtime("protection_expired", owner, {"http_status": expired[owner + "_http_status"],
            "error_code": "security_protection_required" if owner == "manager" else expired["service_error_code"], "result_absent": expired["result_absent"]})
    runtime("protection_expired", "develop", {"execution_id": expired["develop_execution_id"],
        "diagnostic_reason": "protection_expired", "diagnostic_stage": "preparation", "diagnostic_code": "develop.query.protection_required",
        "history_result_absent": True, "denied_result_absent": expired["result_absent"]})
    runtime("protection_expired", "transfer", {**expired["transfer_diagnostic"], "execution_id": expired["transfer_execution_id"],
        "history_result_absent": True, "records_written": expired["records_written"], "bytes_written": expired["bytes_written"], "target_unchanged": expired["target_unchanged"], "same_user_verified": report["resource_validity"]["same_user_verified"]})
    counts = {level: sum(cell["level"] == level for row in matrix.values() for cell in row.values()) for level in ("runtime", "protocol_only")}
    if counts != {"runtime": 20, "protocol_only": 8}: raise SuiteError("unexpected reason matrix coverage")
    return {"scope": "ordinary MySQL table and MongoDB collection profile; protocol-only cells retain outlet T4 gaps",
            "rows": matrix, "coverage_counts": counts}


def exercise_enrolling_refusal(client: GatewayClient, enrollment_id: str, engine_id: int,
                               locator: str, deadline: float, query: str) -> dict[str, object]:
    while time.monotonic() < deadline:
        enrollment = _object(client.request("GET", "/api/v1/security/protection-enrollments/" + enrollment_id,
                                            (200,)).payload, "unready enrollment")
        if enrollment.get("state") == "activating":
            time.sleep(1)
            continue
        if enrollment.get("state") != "enrolling":
            raise SuiteError("unready acceptance requires the actual enrolling state")
        progress = [_object(v, "enrolling owner") for v in _array(enrollment.get("owner_progress"), "enrolling owners")]
        by_owner = {v.get("consumer_owner"): v for v in progress}
        if (set(by_owner) == set(PROTECTION_OWNERS) and len(progress) == len(PROTECTION_OWNERS)
                and all(v.get("projection_state") == "enrolling" and v.get("acknowledged") is True
                        and v.get("rules") == [] for v in progress)):
            result = develop_refusal_with_history(client, engine_id, locator, query,
                "protection_not_ready", deadline)
            return {**result, "enrolling_acknowledged": True}
        time.sleep(1)
    raise SuiteError("enrolling protection did not converge before refusal acceptance")


def run_bank_schema_fixture(action: str, deadline: float) -> None:
    if action not in {"bank-schema-add", "bank-schema-remove"}:
        raise SuiteError("unsupported owned schema action")
    repository = Path(__file__).resolve().parents[2]
    SUPPORT.require_hosted_security_lifecycle(repository)
    remaining = 30 if action == "bank-schema-remove" else deadline - time.monotonic()
    if remaining <= 0:
        raise SuiteError("owned schema acceptance exceeded its budget")
    result = subprocess.run(["bash", "business/scripts/online-security-owner-fixture.sh", action],
        cwd=repository, capture_output=True, text=True, timeout=min(remaining, 30))
    if result.returncode:
        raise SuiteError("owned bank schema fixture failed: " + action)


def exercise_bank_card_refusal_causes(client: GatewayClient, enrollment_id: str, engine_id: int,
                                      locator: str, deadline: float, probe: Callable[[str, str | None], dict[str, object]]) -> dict[str, object]:
    # This direct field has proven lineage. The SQL output compiler rejects
    # string-literal predicates in its current conservative transform subset.
    unsupported = develop_refusal_with_history(client, engine_id, locator,
        "SELECT id," + BANK_FIELD + " FROM " + BANK_SOURCE + " WHERE customer_code='CUST-10001'",
        "protection_operation_unsupported", deadline)
    unsupported["outlets"] = probe("protection_operation_unsupported",
        "SELECT id," + BANK_FIELD + " FROM " + BANK_SOURCE + " WHERE customer_code='CUST-10001'")
    before = _object(client.request("GET", "/api/v1/security/protection-enrollments/" + enrollment_id,
                                    (200,)).payload, "pre-drift enrollment")
    attempted = False
    try:
        attempted = True
        run_bank_schema_fixture("bank-schema-add", deadline)
        mismatch = develop_refusal_with_history(client, engine_id, locator,
            "SELECT id," + BANK_FIELD + " FROM " + BANK_SOURCE,
            "protection_schema_mismatch", deadline)
        mismatch["outlets"] = probe("protection_schema_mismatch", None)
    finally:
        if attempted:
            run_bank_schema_fixture("bank-schema-remove", deadline)
    after = _object(client.request("GET", "/api/v1/security/protection-enrollments/" + enrollment_id,
                                   (200,)).payload, "restored enrollment")
    if before.get("owner_progress") != after.get("owner_progress"):
        raise SuiteError("schema drift acceptance unexpectedly changed protection revisions")
    recovered = assert_bank_card_rows(develop_rows(client, engine_id, locator, deadline,
        "SELECT " + ",".join(BANK_FIELDS) + " FROM " + BANK_SOURCE + " ORDER BY id"), "schema restoration query")
    return {"operation_unsupported": unsupported, "schema_mismatch": mismatch,
            "schema_restored": True, "protection_revisions_unchanged": True, "recovered": recovered}


def exercise_bank_card_sql_pages(client: GatewayClient, name: str) -> dict[str, object]:
    rows = []
    cursor = ""
    seen = set()
    for index, count in enumerate([2, 2, 1]):
        page = {"limit": 2}
        if cursor:
            page["cursor"] = cursor
        payload = _object(client.request("POST", "/api/query/" + urllib.parse.quote(name, safe="") + "/query", (200,),
            {"select": BANK_FIELDS + ["copied_card", "copied_code"], "order_by": [{"field": "id", "direction": "asc"}], "page": page, "format": "json"}).payload, "SQL page result")
        data = _array(payload.get("data"), "SQL page rows")
        pagination = _object(payload.get("page"), "SQL page pagination")
        more = index < 2
        if len(data) != count or type(pagination.get("has_more")) is not bool or pagination["has_more"] != more:
            raise SuiteError("SQL service cursor page count or continuation differs")
        cursor = pagination.get("next_cursor", "")
        if not isinstance(cursor, str) or (more and (not cursor or cursor in seen)):
            raise SuiteError("SQL service cursor must advance")
        if not more and cursor:
            raise SuiteError("SQL service final page must not continue")
        seen.add(cursor)
        rows.extend(_object(row, "SQL page row") for row in data)
    if [normalize_id(row.get("id")) for row in rows] != ["1", "2", "3", "4", "5"]:
        raise SuiteError("SQL service cursor pages lost, duplicated or reordered rows")
    return {**assert_bank_card_mixed_rows(rows, "Service SQL cursor pages"), "pages": 3,
            "page_sizes": [2, 2, 1], "no_duplicate_or_missing_rows": True, "actual_source_contract_verified": True}


def bank_card_projections(enrollment: Mapping[str, object]) -> dict[str, object]:
    progress = _array(enrollment.get("owner_progress"), "bank-card Owner progress")
    if enrollment.get("state") != "active" or {p.get("consumer_owner") for p in progress if isinstance(p, dict)} != set(PROTECTION_OWNERS) or len(progress) != len(PROTECTION_OWNERS):
        raise SuiteError("bank-card Enrollment must have all required Owner projections")
    graph = next(p for p in progress if p.get("consumer_owner") == "graph")
    if graph.get("acknowledged") is not True or graph.get("projection_state") != "active" or not any(rule.get("action") == "graph_read" and rule.get("effect") == "deny" for rule in graph.get("rules", [])):
        raise SuiteError("Graph protection deny must be installed")
    evidence = {}
    for owner, action in OWNER_ACTIONS.items():
        matches = [p for p in progress if isinstance(p, dict) and p.get("consumer_owner") == owner]
        if len(matches) != 1:
            raise SuiteError("bank-card Owner progress is not unique")
        item = matches[0]
        rules = _array(item.get("rules"), "bank-card Owner rules")
        revision = item.get("revision")
        if (item.get("projection_state") != "active" or item.get("acknowledged") is not True
                or item.get("validity") != "valid"
                or not isinstance(revision, str) or not re.fullmatch(r"[0-9]{20}", revision)
                or int(revision) <= 0 or not item.get("published_cursor")
                or (action, "mask") not in {(r.get("action"), r.get("effect"))
                                            for r in rules if isinstance(r, dict)}):
            raise SuiteError("bank-card Owner did not acknowledge its current mask projection")
        try:
            uuid.UUID(str(item.get("projection_id")))
        except ValueError as error:
            raise SuiteError("bank-card projection ID is invalid") from error
        acknowledged = utc_timestamp(item.get("acknowledged_at"), "bank-card acknowledgement")
        discovered = utc_timestamp(enrollment.get("last_discovered_at"), "bank-card discovery time")
        if acknowledged < discovered:
            raise SuiteError("bank-card acknowledgement predates discovery")
        evidence[owner] = {"projection_id": item["projection_id"], "revision": revision,
                           "published_cursor": item["published_cursor"], "acknowledged": True,
                           "action": action, "effect": "mask"}
    return evidence


def assert_bank_card_discovery(client: GatewayClient, enrollment_id: str,
                               assessment_id: str, finding_id: str) -> dict[str, object]:
    findings = list_pages(client, "/api/v1/security/findings?" + urllib.parse.urlencode(
        {"enrollment_id": enrollment_id, "snapshot_scope": "current"}))
    if (len(findings) != 1 or findings[0].get("id") != finding_id
            or findings[0].get("detector_version") != BANK_DETECTOR
            or _object(findings[0].get("component"), "bank-card Finding component").get("key") != BANK_FIELD):
        raise SuiteError("bank-card discovery must find only the exact string field, excluding negative fields")
    assessment = _object(client.request("GET", "/api/v1/security/assessments/" + assessment_id,
                                        (200,)).payload, "bank-card Assessment")
    current = _object(assessment.get("current"), "bank-card Assessment revision")
    component = _object(current.get("component"), "bank-card assessed component")
    types = [item for item in definition_array(client, "/api/v1/security/sensitive-data-types")
             if item.get("code") == "bank_card"]
    if len(types) != 1:
        raise SuiteError("bank-card discovery requires its unique configured sensitive type")
    type_id = positive_int(types[0].get("id"), "bank-card sensitive type ID")
    if (assessment.get("id") != assessment_id or assessment.get("enrollment_id") != enrollment_id
            or positive_int(current.get("sensitive_data_type_id"), "bank-card Assessment type") != type_id
            or positive_int(findings[0].get("sensitive_data_type_id"), "bank-card Finding type") != type_id
            or current.get("conclusion") != "sensitive" or component.get("key") != BANK_FIELD
            or component.get("value_type") != "string"
            or component.get("path") != [{"name": BANK_FIELD, "container": "scalar"}]
            or not isinstance(component.get("schema_fingerprint"), str)
            or not component["schema_fingerprint"].startswith("sha256:")):
        raise SuiteError("bank-card Assessment must bind the actual Meta string field")
    return {"finding_id": finding_id, "assessment_id": assessment_id,
            "sensitive_data_type_id": str(type_id),
            "detector": BANK_DETECTOR, "component_key": BANK_FIELD,
            "meta_string_component_verified": True, "negative_fields_not_detected": True}


def exercise_bank_card_metadata(client: GatewayClient, source_engine_id: int,
                                target_engine_id: int, database: str, run_id: str,
                                identity: Mapping[str, object], deadline: float, enrolling_refusal: dict[str, object]) -> dict[str, object]:
    source = find_item(client, source_engine_id, database + "." + BANK_SOURCE, "table")
    target = find_item(client, target_engine_id, TARGET_SCHEMA + "." + BANK_TARGET, "table")
    locator = build_item_locator(source_engine_id, source)
    columns, rows = preview_rows(client, build_item_locator(target_engine_id, target))
    if columns != ["id"] or rows:
        raise SuiteError("bank-card Transfer target must start empty")
    columns, rows = preview_rows(client, locator)
    assert_bank_card_rows(rows, "Unprotected bank-card source control", columns, protected=False)
    enrollment_id, initialized = ensure_enrollment(client, source_engine_id, database + "." + BANK_SOURCE, locator)
    if not initialized:
        raise SuiteError("bank protection acceptance requires a fresh isolated enrollment")
    parent = build_schema_locator(target_engine_id, target)
    target_locator = build_item_locator(target_engine_id, target)
    def probe(reason, query):
        return bank_outlet_refusals(client, source_engine_id, target_engine_id, source, BANK_FIELDS, locator, parent, target_locator, reason, query, deadline, positive_int(identity["principal_id"], "ordinary principal"))
    assessment_id, confirmed, finding_id = ensure_detected_assessment(
        client, enrollment_id, deadline, BANK_FIELD, BANK_DETECTOR)
    discovery = assert_bank_card_discovery(client, enrollment_id, assessment_id, finding_id)
    wait_for_algorithm_projections(client, enrollment_id, deadline)
    enrollment = _object(client.request("GET", "/api/v1/security/protection-enrollments/" + enrollment_id,
                                        (200,)).payload, "bank-card Enrollment")
    projections = bank_card_projections(enrollment)
    refusal_causes = exercise_bank_card_refusal_causes(client, enrollment_id, source_engine_id, locator, deadline, probe)
    name = "addp-online-security-bank-" + run_suffix(run_id, "-")
    service_id = None
    sql_service_id = None
    computed_service_id = None
    tasks: list[int] = []
    failure: BaseException | None = None
    outputs: dict[str, object] = {}
    try:
        columns, rows = preview_rows(client, locator)
        outputs["manager"] = assert_bank_card_rows(rows, "Manager bank-card preview", columns)
        query = "SELECT " + ", ".join(BANK_FIELDS) + " FROM " + BANK_SOURCE + " ORDER BY id"
        outputs["develop"] = assert_bank_card_rows(
            develop_rows(client, source_engine_id, locator, deadline, query), "Develop bank-card query")
        develop_wildcard = {}
        for label, statement in {
            "unqualified": "SELECT * FROM " + BANK_SOURCE + " ORDER BY id",
            "table_alias": "SELECT b.* FROM " + BANK_SOURCE + " b ORDER BY b.id",
        }.items():
            develop_wildcard[label] = assert_bank_card_rows(
                develop_rows(client, source_engine_id, locator, deadline, statement),
                "Develop bank-card wildcard " + label)
            if develop_wildcard[label] != outputs["develop"]:
                raise SuiteError("Develop wildcard differs from direct-field protection")
        develop_mixed_wildcard = {}
        for label, statement in {
            "copy_after_star": "SELECT b.*,b." + BANK_FIELD + " AS copied_card,b.customer_code AS copied_code FROM " + BANK_SOURCE + " b ORDER BY b.id",
            "copy_before_star": "SELECT b." + BANK_FIELD + " AS copied_card,b.customer_code AS copied_code,b.* FROM " + BANK_SOURCE + " b ORDER BY b.id",
        }.items():
            develop_mixed_wildcard[label] = assert_bank_card_mixed_rows(
                develop_rows(client, source_engine_id, locator, deadline, statement),
                "Develop bank-card mixed wildcard " + label)
        develop_page_wildcard = {}
        for label, statement in {
            "outer_star": "SELECT w.* FROM (SELECT b.*,b." + BANK_FIELD + " AS copied_card,b.customer_code AS copied_code FROM " + BANK_SOURCE + " b WHERE b.id>=1 ORDER BY b.id LIMIT 100) w ORDER BY w.id LIMIT 100",
            "outer_direct": "SELECT " + ",".join("w." + field for field in BANK_FIELDS + ["copied_card", "copied_code"]) + " FROM (SELECT b.*,b." + BANK_FIELD + " AS copied_card,b.customer_code AS copied_code FROM " + BANK_SOURCE + " b) w ORDER BY w.id LIMIT 100",
        }.items():
            develop_page_wildcard[label] = assert_bank_card_mixed_rows(
                develop_rows(client, source_engine_id, locator, deadline, statement), "Develop wildcard page " + label)
        service = _object(client.request("POST", "/api/v1/service/query", (201,),
            service_payload(name, source_engine_id, locator, BANK_FIELDS)).payload, "bank-card Query Service")
        service_id = positive_int(service.get("id"), "bank-card service ID")
        outputs["service"] = assert_bank_card_rows(service_rows(client, name, BANK_FIELDS), "Service bank-card query")
        sql_name = name + "-sql-page"
        sql_service = _object(client.request("POST", "/api/v1/service/query", (201,),
            bank_card_sql_service_payload(sql_name, source_engine_id, source, locator)).payload, "bank-card SQL service")
        sql_service_id = positive_int(sql_service.get("id"), "bank-card SQL service ID")
        service_sql_page = exercise_bank_card_sql_pages(client, sql_name)
        computed_outputs = exercise_bank_card_computed_outputs(client, source_engine_id, locator, deadline)
        computed_name = name + "-derived"
        computed_service = _object(client.request("POST", "/api/v1/service/query", (201,),
            bank_card_computed_service_payload(computed_name, source_engine_id, source, locator)).payload, "bank-card computed SQL service")
        computed_service_id = positive_int(computed_service.get("id"), "bank-card computed SQL service ID")
        refusal = client.request("POST", "/api/query/" + urllib.parse.quote(computed_name, safe="") + "/query", (403,),
            {"select": ["id", "computed"], "order_by": [{"field": "id", "direction": "asc"}], "page": {"limit": 2}, "format": "json"})
        validate_service_protection_refusal(refusal)
        computed_outputs["service_refusal"] = {"http_status": 403, "error_code": "data_protection_required", "result_absent": True}
        computed_outputs["transfer_refusal"] = postgres_query_transfer_refusal(client, target_engine_id, parent,
            target_locator, "protection_computed_output_unsupported", deadline, positive_int(identity["principal_id"], "ordinary principal"))

        task_id, execution = create_and_run_task(client,
            transfer_payload("addp_online_security_bank_" + run_suffix(run_id, "_"), locator,
                             build_schema_locator(target_engine_id, target), BANK_TARGET), deadline, tasks)
        execution_id = execution.get("execution_id")
        try:
            if str(uuid.UUID(str(execution_id))) != execution_id:
                raise ValueError
        except (ValueError, TypeError, AttributeError) as error:
            raise SuiteError("bank-card Transfer execution UUID is invalid") from error
        if (execution.get("status") != "success" or positive_int(execution.get("task_id"), "bank task") != task_id
                or positive_int(execution.get("trigger_by"), "bank initiator") != positive_int(identity["principal_id"], "ordinary user")
                or nonnegative_int(execution.get("records_read"), "bank rows read") != 5
                or nonnegative_int(execution.get("records_written"), "bank rows written") != 5):
            raise SuiteError("bank-card Transfer execution provenance or counts differ")
        scan = wait_for_scan(client, target_engine_id, deadline)
        target = find_item(client, target_engine_id, TARGET_SCHEMA + "." + BANK_TARGET, "table")
        columns, rows = preview_rows(client, build_item_locator(target_engine_id, target))
        outputs["transfer"] = {**assert_bank_card_rows(rows, "Transfer bank-card target", columns),
            "execution_id": execution_id, "task_id": task_id, "records_read": 5, "records_written": 5,
            "same_user_verified": True, "target_empty_before": True, "target_output_verified": True,
            "target_rescan_execution_id": scan}
    except BaseException as error:
        failure = error
    cleanup_errors = []
    for cleanup in (lambda: cleanup_tasks(client, tasks), lambda: cleanup_service(client, service_id), lambda: cleanup_service(client, sql_service_id), lambda: cleanup_service(client, computed_service_id)):
        try:
            cleanup()
        except BaseException as error:
            cleanup_errors.append(str(error))
    if cleanup_errors:
        raise SuiteError("bank-card owned resource cleanup failed: " + "; ".join(cleanup_errors)) from failure
    if failure:
        raise failure
    return {**discovery, "enrollment_id": enrollment_id, "enrollment_initialized": initialized,
            "unprotected_source_verified": True,
            "finding_confirmed": confirmed, "source_table": database + "." + BANK_SOURCE,
            "target_table": TARGET_SCHEMA + "." + BANK_TARGET, "service_id": service_id,
            "algorithm": STRUCTURED_MASK_ALGORITHM, "projections": projections, "owners": outputs,
            "develop_wildcard": develop_wildcard,
            "develop_mixed_wildcard": develop_mixed_wildcard,
            "develop_page_wildcard": develop_page_wildcard, "service_sql_page": service_sql_page,
            "computed_outputs": computed_outputs, "computed_service_deleted": True,
            "refusal_causes": {"not_ready": enrolling_refusal, **refusal_causes},
            "task_deleted": True, "service_deleted": True, "sql_service_deleted": True, "temporary_execution_resources": 11,
            "temporary_execution_resource_residuals": 0}



def role_projection(client: GatewayClient, enrollment_id: str, owner: str = "service") -> dict[str, object]:
    enrollment = _object(client.request("GET", "/api/v1/security/protection-enrollments/" + enrollment_id,
                                        (200,)).payload, "role cursor Enrollment")
    matches = [item for item in _array(enrollment.get("owner_progress"), "role cursor owner progress")
               if isinstance(item, dict) and item.get("consumer_owner") == owner]
    if enrollment.get("state") != "active" or len(matches) != 1:
        raise SuiteError("role cursor requires one active Owner projection")
    progress = matches[0]
    if (progress.get("projection_state") != "active"
            or not isinstance(progress.get("revision"), str)
            or re.fullmatch(r"[0-9]{20}", progress["revision"]) is None
            or int(progress["revision"]) <= 0
            or any(not isinstance(progress.get(key), str) or not progress[key]
                   for key in ("projection_id", "published_cursor"))):
        raise SuiteError("role cursor Owner projection identity is incomplete")
    return {key: progress.get(key) for key in ("projection_id", "revision", "published_cursor", "acknowledged")}


def wait_role_projection(client: GatewayClient, enrollment_id: str,
                                 previous: Mapping[str, object], deadline: float, owner: str = "service") -> dict[str, object]:
    while time.monotonic() < deadline:
        current = role_projection(client, enrollment_id, owner)
        if current["projection_id"] != previous["projection_id"]:
            raise SuiteError("role cursor Owner projection identity changed")
        if (int(current["revision"]) > int(previous["revision"])
                and current["published_cursor"] != previous["published_cursor"]
                and current["acknowledged"] is True):
            return current
        time.sleep(1)
    raise SuiteError("role cursor Owner projection did not acknowledge a new revision")


def assert_service_role_page(client: GatewayClient, path: str, expected_ids: list[str],
                             role_selected: bool, cursor: str = "") -> tuple[dict[str, object], str, str]:
    request = {"select": BANK_FIELDS, "order_by": [{"field": "id", "direction": "asc"}],
               "page": {"limit": 4, **({"cursor": cursor} if cursor else {})}, "format": "json"}
    response = client.request("POST", path, (200,), request)
    payload = _object(response.payload, "role cursor JSON page")
    rows = [_object(row, "role cursor row") for row in _array(payload.get("data"), "role cursor data")]
    if [normalize_id(row.get("id")) for row in rows] != expected_ids:
        raise SuiteError("role cursor page lost, duplicated or reordered rows")
    page = _object(payload.get("page"), "role cursor page")
    more = expected_ids[-1] != "5"
    next_cursor = page.get("next_cursor", "")
    version = payload.get("service_version")
    if (page.get("has_more") is not more or not isinstance(next_cursor, str)
            or bool(next_cursor) != more or (more and next_cursor == cursor)
            or not isinstance(version, str) or not version):
        raise SuiteError("role cursor pagination or service version is invalid")
    for key, row in zip(expected_ids, rows):
        source_value = BANK_SOURCE_VALUES[key]
        expected = ("*" * len(source_value) if source_value else None) if role_selected else BANK_MASKED[key]
        allowed = [set(BANK_FIELDS)]
        if key == "5":
            allowed.append(set(BANK_FIELDS) - {BANK_FIELD})
        if (set(row) not in allowed or row.get(BANK_FIELD) != expected
                or row.get("customer_code") != "CUST-1000" + key
                or row.get("bank_account_no") != "account-" + key
                or row.get("card_id") != "card-" + key
                or row.get("bank_card_verified") != "verified-" + key
                or normalize_id(row.get("credit_card_number")) != str(100 + int(key))):
            raise SuiteError("role cursor protected or adjacent field differs from the independent fixture")
    csv_response = client.request("POST", path, (200,), dict(request, format="csv"), raw_response=True)
    if not isinstance(csv_response.payload, bytes):
        raise SuiteError("role cursor CSV response must be bytes")
    try:
        reader = csv.DictReader(io.StringIO(csv_response.payload.decode("utf-8-sig")))
        csv_rows = list(reader)
    except (UnicodeError, csv.Error) as error:
        raise SuiteError("role cursor CSV is malformed") from error
    expected_csv = [{field: "" if row.get(field) is None else str(row[field]) for field in BANK_FIELDS} for row in rows]
    if reader.fieldnames != BANK_FIELDS or csv_rows != expected_csv:
        raise SuiteError("role cursor CSV fields, protected values or empty cells differ from JSON")
    return ({"json_http_status": response.status, "csv_http_status": csv_response.status,
             "rows": len(rows), "ids": expected_ids, "protected_values_verified": True,
             "non_sensitive_fields_preserved": True, "csv_empty_cells_verified": True,
             "has_more": more, "service_version": version}, next_cursor, version)


def assert_service_role_refusal(response: SUPPORT.Response) -> dict[str, object]:
    payload = _object(response.payload, "role cursor refusal")
    if (response.status != 409 or set(payload) != {"error", "error_code"}
            or payload.get("error_code") != "query_protection_choice_invalid"
            or not isinstance(payload.get("error"), str) or not payload["error"].strip()):
        raise SuiteError("role cursor must refuse with exact 409 choice-invalid and no result")
    return {"http_status": response.status, "error_code": payload["error_code"], "result_absent": True}


def service_subject_role_observation(client: GatewayClient, identity: Mapping[str, object]) -> dict[str, object]:
    roles = _object(client.request("GET", "/api/v1/system/auth/subject-roles", (200,)).payload, "own roles")
    keys = roles.get("role_keys")
    now = datetime.now(timezone.utc)
    if (roles.get("tenant_id") != identity["tenant_id"] or roles.get("actor_principal_id") != identity["principal_id"]
            or not isinstance(keys, list) or not keys or not all(isinstance(key, str) for key in keys)
            or keys != sorted(set(keys)) or any(key in FORBIDDEN_ADMIN_ROLES for key in keys)
            or utc_timestamp(roles.get("expires_at"), "own roles expiry") <= now
            or utc_timestamp(roles.get("observed_at"), "own roles observation") > now + timedelta(minutes=1)):
        raise SuiteError("role cursor requires the same ordinary current Tenant User")
    return roles


def service_subject_roles(client: GatewayClient, identity: Mapping[str, object]) -> dict[str, object]:
    roles = service_subject_role_observation(client, identity)
    return {key: roles.get(key) for key in ("tenant_id", "actor_principal_id", "role_keys")}


def create_role_policy(client: GatewayClient, assessment_id: str, role_keys: list[str], owner: str = "service") -> dict[str, object]:
    action = {"service": "service_execute", "develop": "query"}[owner]
    previous = [policy for policy in list_pages(client, "/api/v1/security/protection-policies")
                if policy.get("assessment_id") == assessment_id and policy.get("consumer_owner") == owner
                and policy.get("action") == action]
    if len(previous) > 1 or any(policy.get("state") != "revoked" for policy in previous):
        raise SuiteError("role fixture must not take over an active Owner Policy")
    body = {
        "effect": "mask", "algorithm": STRUCTURED_MASK_ALGORITHM,
        "parameters": {"prefix_runes": 1, "suffix_runes": 1, "mask_rune": "*"},
        "invalid_value_effect": "suppress", "rationale": "Disposable Owner role option acceptance",
        "role_options": [{"name": "Online cursor full mask", "role_keys": role_keys,
                          "decision": {"effect": "mask", "algorithm": STRUCTURED_MASK_ALGORITHM,
                                       "parameters": {"prefix_runes": 0, "suffix_runes": 0, "mask_rune": "*"},
                                       "invalid_value_effect": "suppress"}}],
    }
    if previous:
        return _object(client.request("PUT", "/api/v1/security/protection-policies/" + str(previous[0]["id"]),
            (200,), dict(body, version=previous[0]["version"])).payload, "role Policy reactivation")
    return _object(client.request("POST", "/api/v1/security/protection-policies", (201,),
        dict(body, assessment_id=assessment_id, consumer_owner=owner, action=action)).payload, "role cursor Policy")

def owner_role_choice(client: GatewayClient, locator: str, source: Mapping[str, object], policy: Mapping[str, object], owner: str = "service", engine_id: int | None = None) -> dict[str, object]:
    policy_id = str(policy["id"])
    saved_options = _array(_object(policy.get("current"), "role Policy current").get("role_options"), "saved role options")
    if len(saved_options) != 1:
        raise SuiteError("role cursor Policy did not create exactly one server-owned option")
    option_id = _object(saved_options[0], "saved role option").get("id")
    try:
        if str(uuid.UUID(str(option_id))) != option_id:
            raise ValueError
    except (ValueError, TypeError, AttributeError) as error:
        raise SuiteError("role cursor Policy did not return a canonical option UUID") from error
    options = _object(client.request("GET", "/api/v1/" + owner + "/query-protection-options?" +
        urllib.parse.urlencode({"target_locator": locator, **({"engine_id": engine_id} if owner == "develop" else {})}), (200,)).payload, "Owner role options")
    matches = [field for field in _array(options.get("fields"), "Owner role fields")
               if isinstance(field, dict) and field.get("component_key") == BANK_FIELD]
    if len(matches) != 1:
        raise SuiteError("role cursor requires exactly one eligible bank-card field")
    field = matches[0]
    target = {"owner_module": "meta", "resource_type": "data_item", "resource_identity": source["fingerprint"]}
    if (field.get("target") != target or field.get("policy_id") != policy_id
            or positive_int(field.get("policy_revision"), "role policy revision") != positive_int(policy.get("current_revision"), "Policy revision")
            or field.get("options") != [{"id": option_id, "name": "Online cursor full mask"}]):
        raise SuiteError("role cursor available choice does not bind this source and Policy revision")
    choice = {"target": target, "component_key": BANK_FIELD, "selection": {
        "policy_id": policy_id, "assessment_revision": positive_int(field.get("assessment_revision"), "Assessment revision"),
        "policy_revision": positive_int(field["policy_revision"], "Policy revision"), "option_id": option_id}}
    return choice


def restore_service_default(client: GatewayClient, service_path: str, query_path: str, choice: Mapping[str, object], retained: Mapping[str, object]) -> tuple[dict[str, object], str]:
    def definition():
        return _object(client.request("GET", service_path, (200,)).payload, "role cursor saved Service")
    client.request("PUT", service_path, (200,), {"version": retained["version"], "status": "inactive"})
    stopped = definition()
    if stopped.get("status") != "inactive" or stopped["data_config"].get("protection_choices") != [choice]:
        raise SuiteError("role cursor pure stop must retain the invalid choice")
    client.request("PUT", service_path, (200,), {"version": stopped["version"],
        "data_config": {"protection_choices": []}})
    cleared = definition()
    if cleared["data_config"].get("protection_choices") != [] or cleared.get("status") != "inactive":
        raise SuiteError("role cursor explicit removal did not persist")
    client.request("PUT", service_path, (200,), {"version": cleared["version"], "status": "active"})
    recovery, recovered_cursor, recovered_version = assert_service_role_page(client, query_path, ["1", "2", "3", "4"], False)
    recovered_last, _, last_version = assert_service_role_page(client, query_path, ["5"], False, recovered_cursor)
    recovery.update(continuation=recovered_last, total_rows=5)
    if last_version != recovered_version:
        raise SuiteError("default recovery pagination changed its Service version")
    return recovery, recovered_version


def cleanup_service_role_resources(client: GatewayClient, policy_id: str | None, service_id: int | None, cause: BaseException | None = None) -> dict[str, object]:
    final_policy = {}
    cleanup_errors = []
    if policy_id is not None:
        try:
            path = "/api/v1/security/protection-policies/" + policy_id
            current = _object(client.request("GET", path, (200,)).payload, "role Policy cleanup")
            if current.get("state") == "active":
                client.request("DELETE", path, (200,), {"version": current["version"], "rationale": "Disposable role cursor cleanup"})
            final_policy = _object(client.request("GET", path, (200,)).payload, "revoked role Policy")
            if final_policy.get("state") != "revoked":
                raise SuiteError("role cursor Policy cleanup left an active option")
        except BaseException as error:
            cleanup_errors.append(str(error))
    try:
        cleanup_service(client, service_id)
    except BaseException as error:
        cleanup_errors.append(str(error))
    if cleanup_errors:
        raise SuiteError("role cursor cleanup failed: " + "; ".join(cleanup_errors)) from cause
    return final_policy


def exercise_service_role_cursor_revocation(client: GatewayClient, engine_id: int, database: str,
        bank: Mapping[str, object], identity: Mapping[str, object], run_id: str, deadline: float) -> dict[str, object]:
    """Real User HTTP only; synthetic data and policy owned by this disposable run."""
    source = find_item(client, engine_id, database + "." + BANK_SOURCE, "table")
    locator = build_item_locator(engine_id, source)
    enrollment_id, assessment_id = str(bank["enrollment_id"]), str(bank["assessment_id"])
    previous = role_projection(client, enrollment_id)
    roles = service_subject_roles(client, identity)
    policies = list_pages(client, "/api/v1/security/protection-policies")
    if any(policy.get("assessment_id") == assessment_id and policy.get("consumer_owner") == "service"
           and policy.get("action") == "service_execute" and policy.get("state") == "active" for policy in policies):
        raise SuiteError("role cursor fixture already has an active Service Policy")
    baseline_before = definition_array(client, "/api/v1/security/protection-baselines")
    policy_id = None
    service_id = None
    failure = None
    result = {}
    try:
        policy = create_role_policy(client, assessment_id, roles["role_keys"])
        policy_id = str(policy["id"])
        installed = wait_role_projection(client, enrollment_id, previous, deadline)
        choice = owner_role_choice(client, locator, source, policy)
        name = ("online-security-role-cursor-" + run_suffix(run_id, "-"))[:120].rstrip("-")
        body = service_payload(name, engine_id, locator, BANK_FIELDS)
        body["protocols"]["rest_api"]["formats"] = ["json", "csv"]
        body["data_config"]["protection_choices"] = [choice]
        created = _object(client.request("POST", "/api/v1/service/query", (201,), body).payload, "role cursor Service")
        service_id = positive_int(created.get("id"), "role Service ID")
        service_path = f"/api/v1/service/query/{service_id}"
        def definition():
            return _object(client.request("GET", service_path, (200,)).payload, "role cursor saved Service")
        selected = definition()
        if (selected.get("status") != "active" or selected.get("public_access") is not False
                or _object(selected.get("data_config"), "saved role config").get("protection_choices") != [choice]):
            raise SuiteError("role cursor Service must persist the explicit choice and stay private")
        query_path = "/api/query/" + urllib.parse.quote(name, safe="") + "/query"
        first, cursor, version = assert_service_role_page(client, query_path, ["1", "2", "3", "4"], True)
        continuation, _, continued_version = assert_service_role_page(client, query_path, ["5"], True, cursor)
        if continued_version != version:
            raise SuiteError("role cursor version changed before revocation")
        current = _object(client.request("GET", "/api/v1/security/protection-policies/" + policy_id, (200,)).payload, "role Policy version")
        client.request("DELETE", "/api/v1/security/protection-policies/" + policy_id, (200,),
                       {"version": current["version"], "rationale": "Revoke only this run's role option; retain saved Service choice"})
        revoked = wait_role_projection(client, enrollment_id, installed, deadline)
        retained = definition()
        if retained != selected:
            raise SuiteError("role cursor Service definition changed before the old cursor probes")
        denials = {}
        for format_name in ("json", "csv"):
            request = {"select": BANK_FIELDS, "order_by": [{"field": "id", "direction": "asc"}],
                       "page": {"limit": 4, "cursor": cursor}, "format": format_name}
            denials[format_name] = assert_service_role_refusal(client.request("POST", query_path, (409,), request))
        request["page"] = {"limit": 4}
        request["format"] = "json"
        control = assert_service_role_refusal(client.request("POST", query_path, (409,), request))
        recovery, recovered_version = restore_service_default(client, service_path, query_path, choice, retained)
        if recovered_version == version or service_subject_roles(client, identity) != roles:
            raise SuiteError("role cursor recovery must change the choice version without changing User roles")
        if definition_array(client, "/api/v1/security/protection-baselines") != baseline_before:
            raise SuiteError("role cursor acceptance changed a default Baseline")
        result = {"source_locator": locator, "service_id": service_id, "policy_id": policy_id,
                  "principal_id": identity["principal_id"], "tenant_id": identity["tenant_id"],
                  "choice": choice, "installed_projection": installed, "revoked_projection": revoked,
                  "first_page": first, "valid_old_cursor_continuation": continuation,
                  "old_cursor": {"length": len(cursor), "sha256": hashlib.sha256(cursor.encode()).hexdigest()},
                  "old_cursor_refusals": denials, "no_cursor_control": control,
                  "saved_choice_retained_until_explicit_removal": True, "pure_stop_retained_invalid_choice": True,
                  "explicit_removal_default_recovery": recovery, "user_roles_unchanged": True, "baselines_unchanged": True}
    except BaseException as error:
        failure = error
    final_policy = cleanup_service_role_resources(client, policy_id, service_id, failure)
    if failure:
        raise failure
    # Policy revisions remain audit facts until the disposable Infra is destroyed.
    return dict(result, created_resources=1, deleted_resources=1, residual_resources=0,
                created_policies=1, revoked_policies=1, policy_revoked=True,
                final_policy_revision=positive_int(final_policy["current_revision"], "revoked Policy revision"),
                service_deleted=True)


SERVICE_ELIGIBILITY_ROLE = "online.security_masking_eligible"
SERVICE_ROLE_READER_PERMISSIONS = {"service.definition.read", "service.definition.create", "service.definition.update",
                                   "service.definition.delete", "service.data_read.execute", "meta.catalog.read"}
SERVICE_ROLE_CONTROLLER_PERMISSIONS = {"iam.tenant_role_assignment.read", "iam.tenant_role_assignment.revoke"}
SERVICE_ROLE_EXPIRY_CONTROLLER_PERMISSIONS = SERVICE_ROLE_CONTROLLER_PERMISSIONS | {"iam.tenant_role_assignment.create"}
SERVICE_ROLE_EXPIRY_SECONDS = 90
SERVICE_ROLE_ENVIRONMENT = ("ADDP_ONLINE_SECURITY_ROLE_USER_ACCESS_TOKEN", "ADDP_ONLINE_SECURITY_ROLE_USER_USERNAME",
                           "ADDP_ONLINE_SECURITY_ROLE_USER_PASSWORD", "ADDP_ONLINE_SECURITY_ROLE_ASSIGNMENT_ID",
                           "ADDP_ONLINE_SECURITY_ROLE_CONTROLLER_ACCESS_TOKEN")

DEVELOP_ROLE_ENVIRONMENT = ("ADDP_ONLINE_SECURITY_DEVELOP_ROLE_USERNAME", "ADDP_ONLINE_SECURITY_DEVELOP_ROLE_PASSWORD")
ROLE_ENVIRONMENT = SERVICE_ROLE_ENVIRONMENT + DEVELOP_ROLE_ENVIRONMENT


def service_role_user_identity(client: GatewayClient, tenant_id: str, required: set[str], *, iam_controller: bool = False) -> dict[str, object]:
    context = _object(client.request("GET", "/api/v1/system/auth/context", (200,)).payload, "role User AuthContext")
    principal = _object(context.get("principal"), "role User principal")
    tenant = _object(context.get("context"), "role User Tenant")
    token = _object(context.get("token"), "role User credential")
    authorization = _object(context.get("authorization"), "role User authorization")
    if (principal.get("type") != "user" or tenant.get("type") != "tenant" or tenant.get("tenant_id") != tenant_id
            or token.get("type") != "first_party_access_token" or context.get("delegation") is not None):
        raise SuiteError("role revocation must use a first-party User in the same Tenant")
    roles, permissions = set(), set()
    for item in _array(authorization.get("role_assignments"), "role User assignments"):
        assignment = _object(item, "role User assignment")
        key = assignment.get("role_key")
        granted = assignment.get("permissions")
        if not isinstance(key, str) or not isinstance(granted, list) or not all(isinstance(p, str) for p in granted):
            raise SuiteError("role User permissions are incomplete")
        roles.add(key)
        permissions.update(granted)
    if ((not iam_controller and (roles & FORBIDDEN_ADMIN_ROLES or permissions != required))
            or (iam_controller and (roles != {"tenant.administrator"} or not required <= permissions))):
        raise SuiteError("role revocation User must have exactly its ordinary fixture permissions")
    return {"principal_id": str(positive_int(principal.get("id"), "role User ID")), "tenant_id": tenant_id,
            "membership_id": str(positive_int(tenant.get("tenant_membership_id"), "role User membership")),
            "authorization_version": str(positive_int(authorization.get("authorization_version"), "role User authorization version")),
            "permissions": sorted(permissions)}


def assert_service_eligibility_assignment(payload: object, identity: Mapping[str, object], assignment_id: str, status: str, username: str = "external-online-security-role-reader") -> dict[str, object]:
    assignment = _object(payload, "masking eligibility Assignment")
    if (assignment.get("id") != assignment_id or assignment.get("principal_type") != "user"
            or assignment.get("principal_id") != identity["principal_id"]
            or assignment.get("membership_id") != identity["membership_id"]
            or assignment.get("role_key") != SERVICE_ELIGIBILITY_ROLE or assignment.get("scope_type") != "tenant"
            or assignment.get("username") != username or assignment.get("status") != status):
        raise SuiteError("role revocation target differs from the dedicated eligibility Assignment")
    return {key: assignment[key] for key in ("id", "principal_id", "membership_id", "role_key", "status")}


def formal_service_role_login(public: GatewayClient, environment: Mapping[str, str]) -> GatewayClient:
    issued = _object(public.request("POST", "/api/v1/system/login", (200,), {
        "username": environment["ADDP_ONLINE_SECURITY_ROLE_USER_USERNAME"],
        "password": environment["ADDP_ONLINE_SECURITY_ROLE_USER_PASSWORD"]}).payload, "formal role User login")
    token = _object(issued.get("session"), "role User new session").get("access_token")
    if issued.get("next_action") != "session_issued" or not isinstance(token, str) or not token:
        raise SuiteError("role scenario must formally issue a User session")
    return GatewayClient(public.base_url, token, public.timeout)


def assert_service_eligibility_denials(reader: GatewayClient, query_path: str, cursor: str) -> dict[str, object]:
    denials = {}
    for format_name in ("json", "csv", "no_cursor"):
        request = {"select": BANK_FIELDS, "order_by": [{"field": "id", "direction": "asc"}],
                   "page": {"limit": 4, **({"cursor": cursor} if format_name != "no_cursor" else {})},
                   "format": "json" if format_name == "no_cursor" else format_name}
        refusal = reader.request("POST", query_path, (403,), request)
        payload = _object(refusal.payload, "role eligibility refusal")
        if (refusal.status != 403 or set(payload) != {"error", "error_code"}
                or payload.get("error_code") != "query_protection_roles_unavailable"
                or not isinstance(payload.get("error"), str) or not payload["error"].strip()):
            raise SuiteError("membership refusal must be exact 403 roles-unavailable with no result")
        denials[format_name] = {"http_status": refusal.status, "error_code": payload["error_code"], "result_absent": True}
    return denials


def exercise_service_role_membership_revocation(client: GatewayClient, engine_id: int, database: str,
        bank: Mapping[str, object], owner_identity: Mapping[str, object], run_id: str, deadline: float,
        environment: Mapping[str, str] | None = None) -> dict[str, object]:
    env = os.environ if environment is None else environment
    if any(not isinstance(env.get(key), str) or not env[key] for key in SERVICE_ROLE_ENVIRONMENT):
        raise SuiteError("role revocation requires dedicated owner-only fixture credentials")
    if env["ADDP_ONLINE_SECURITY_ROLE_USER_USERNAME"] != "external-online-security-role-reader":
        raise SuiteError("role revocation must not use a personal username")
    reader = GatewayClient(client.base_url, env["ADDP_ONLINE_SECURITY_ROLE_USER_ACCESS_TOKEN"], client.timeout)
    controller = GatewayClient(client.base_url, env["ADDP_ONLINE_SECURITY_ROLE_CONTROLLER_ACCESS_TOKEN"], client.timeout)
    public = GatewayClient(client.base_url, "", client.timeout)
    identity = service_role_user_identity(reader, str(owner_identity["tenant_id"]), SERVICE_ROLE_READER_PERMISSIONS)
    operator = service_role_user_identity(controller, identity["tenant_id"], SERVICE_ROLE_CONTROLLER_PERMISSIONS, iam_controller=True)
    if len({identity["principal_id"], operator["principal_id"], owner_identity["principal_id"]}) != 3:
        raise SuiteError("role consumer, controller and governance owner must be distinct Users")
    roles = service_subject_roles(reader, identity)
    if SERVICE_ELIGIBILITY_ROLE not in roles["role_keys"]:
        raise SuiteError("dedicated Service User lacks the masking eligibility role")
    assignment_id = str(positive_int(env["ADDP_ONLINE_SECURITY_ROLE_ASSIGNMENT_ID"], "eligibility Assignment ID"))
    assignment_path = "/api/v1/system/tenant/role_assignments/" + assignment_id
    target = assert_service_eligibility_assignment(controller.request("GET", assignment_path, (200,)).payload, identity, assignment_id, "active")
    policy_id = service_id = None
    failure = None
    result = {}
    try:
        source = find_item(client, engine_id, database + "." + BANK_SOURCE, "table")
        locator = build_item_locator(engine_id, source)
        enrollment_id = str(bank["enrollment_id"])
        before_projection = role_projection(client, enrollment_id)
        baseline = definition_array(client, "/api/v1/security/protection-baselines")
        policy = create_role_policy(client, str(bank["assessment_id"]), [SERVICE_ELIGIBILITY_ROLE])
        policy_id = str(policy["id"])
        installed = wait_role_projection(client, enrollment_id, before_projection, deadline)
        policy_path = "/api/v1/security/protection-policies/" + policy_id
        published_policy = _object(client.request("GET", policy_path, (200,)).payload, "published eligibility Policy")
        choice = owner_role_choice(reader, locator, source, policy)
        name = ("online-security-membership-" + run_suffix(run_id, "-"))[:120].rstrip("-")
        body = service_payload(name, engine_id, locator, BANK_FIELDS)
        body["protocols"]["rest_api"]["formats"] = ["json", "csv"]
        body["data_config"]["protection_choices"] = [choice]
        created = _object(reader.request("POST", "/api/v1/service/query", (201,), body).payload, "membership Service")
        service_id = positive_int(created.get("id"), "membership Service ID")
        service_path = f"/api/v1/service/query/{service_id}"
        selected = _object(reader.request("GET", service_path, (200,)).payload, "membership Service definition")
        if (selected.get("status") != "active" or selected.get("public_access") is not False
                or _object(selected.get("data_config"), "membership Service config").get("protection_choices") != [choice]):
            raise SuiteError("membership Service must preserve the private explicit role choice")
        query_path = "/api/query/" + urllib.parse.quote(name, safe="") + "/query"
        first, cursor, version = assert_service_role_page(reader, query_path, ["1", "2", "3", "4"], True)
        continuation, _, last_version = assert_service_role_page(reader, query_path, ["5"], True, cursor)
        if last_version != version:
            raise SuiteError("membership Service version changed before role revocation")
        revoked = assert_service_eligibility_assignment(controller.request("POST", assignment_path + "/revoke", (200,),
            {"reason": "Disposable masking eligibility revocation; keep functional permissions and Policy unchanged"}).payload,
            identity, assignment_id, "revoked")
        old_session = reader.request("GET", "/api/v1/system/auth/context", (401,))
        if old_session.status != 401 or not set(_object(old_session.payload, "old session refusal")) <= {"error", "error_code"}:
            raise SuiteError("revoked eligibility must invalidate the old session without returning identity")
        fresh = formal_service_role_login(public, env)
        if fresh.token == reader.token:
            raise SuiteError("role revocation must formally issue a fresh User session")
        current = service_role_user_identity(fresh, identity["tenant_id"], SERVICE_ROLE_READER_PERMISSIONS)
        if ({k: v for k, v in current.items() if k != "authorization_version"} !=
                {k: v for k, v in identity.items() if k != "authorization_version"}
                or int(current["authorization_version"]) <= int(identity["authorization_version"])):
            raise SuiteError("fresh role User identity or functional permissions changed")
        current_roles = service_subject_roles(fresh, current)
        if current_roles["role_keys"] != [key for key in roles["role_keys"] if key != SERVICE_ELIGIBILITY_ROLE]:
            raise SuiteError("role revocation must remove only the dedicated eligibility role")
        if (client.request("GET", policy_path, (200,)).payload != published_policy
                or role_projection(client, enrollment_id) != installed
                or fresh.request("GET", service_path, (200,)).payload != selected):
            raise SuiteError("membership revocation must not change Policy, protection projection or Service definition")
        options = _object(fresh.request("GET", "/api/v1/service/query-protection-options?" +
            urllib.parse.urlencode({"target_locator": locator}), (200,)).payload, "revoked role options")
        if any(_object(field, "revoked role field").get("options") for field in _array(options.get("fields"), "revoked role fields")):
            raise SuiteError("revoked User still observes eligible masking options")
        denials = assert_service_eligibility_denials(fresh, query_path, cursor)
        recovery, recovered_version = restore_service_default(fresh, service_path, query_path, choice, selected)
        if (recovered_version == version or client.request("GET", policy_path, (200,)).payload != published_policy
                or role_projection(client, enrollment_id) != installed
                or definition_array(client, "/api/v1/security/protection-baselines") != baseline):
            raise SuiteError("membership default recovery must not alter protection facts")
        result = {"identity_before": identity, "identity_after": current, "role_keys_before": roles["role_keys"],
            "role_keys_after": current_roles["role_keys"], "assignment_before": target, "assignment_after": revoked,
            "controller_principal_id": operator["principal_id"], "choice": choice, "service_id": service_id,
            "policy_revision": policy["current_revision"], "installed_projection": installed,
            "policy_projection_and_saved_choice_unchanged_at_refusal": True, "functional_permissions_unchanged": True,
            "old_session_http_status": 401, "formal_same_user_login_verified": True,
            "first_page": first, "valid_cursor_continuation": continuation,
            "old_cursor": {"length": len(cursor), "sha256": hashlib.sha256(cursor.encode()).hexdigest()},
            "fresh_user_refusals": denials, "explicit_default_recovery": recovery}
    except BaseException as error:
        failure = error
    errors = []
    try:
        assignment = controller.request("GET", assignment_path, (200,)).payload
        state = _object(assignment, "eligibility cleanup").get("status")
        assert_service_eligibility_assignment(assignment, identity, assignment_id, state)
        if state == "active":
            assignment = controller.request("POST", assignment_path + "/revoke", (200,),
                {"reason": "Disposable masking eligibility cleanup"}).payload
        assert_service_eligibility_assignment(assignment, identity, assignment_id, "revoked")
    except BaseException as error:
        errors.append(str(error))
    try:
        cleanup_service_role_resources(client, policy_id, service_id, failure)
    except BaseException as error:
        errors.append(str(error))
    if errors:
        raise SuiteError("membership revocation cleanup failed: " + "; ".join(errors)) from failure
    if failure:
        raise failure
    return dict(result, created_resources=1, deleted_resources=1, residual_resources=0,
                eligibility_assignment_revoked=True, policy_revoked=True, service_deleted=True)


def assert_timed_service_assignment(payload: object, identity: Mapping[str, object], assignment_id: str,
        role_id: str, expires_at: datetime, effective_state: str, username: str = "external-online-security-role-reader") -> dict[str, object]:
    assignment = _object(payload, "timed Service eligibility Assignment")
    binding = assert_service_eligibility_assignment(assignment, identity, assignment_id, "active", username)
    if (assignment.get("role_id") != role_id or assignment.get("effective_state") != effective_state
            or utc_timestamp(assignment.get("valid_until"), "eligibility valid_until") != expires_at
            or any(assignment.get(key) is not None for key in ("revoked_at", "revoked_by", "revoked_reason"))):
        raise SuiteError("timed eligibility must preserve its deadline and natural effective state without revocation")
    return dict(binding, role_id=role_id, valid_until=assignment["valid_until"], effective_state=effective_state)


def cleanup_timed_service_assignment(controller: GatewayClient, identity: Mapping[str, object], assignment_id: str,
        role_id: str, expires_at: datetime, username: str = "external-online-security-role-reader") -> str:
    path = "/api/v1/system/tenant/role_assignments/" + assignment_id
    assignment = _object(controller.request("GET", path, (200,)).payload, "timed eligibility cleanup")
    if assignment.get("status") == "revoked":
        assert_service_eligibility_assignment(assignment, identity, assignment_id, "revoked", username)
        return "revoked"
    state = assignment.get("effective_state")
    if state not in {"effective", "expired"}:
        raise SuiteError("timed eligibility cleanup cannot identify its effective state")
    assert_timed_service_assignment(assignment, identity, assignment_id, role_id, expires_at, state, username)
    if state == "expired":
        return "expired"
    # A cleanup command may cross the real deadline. Never retry the mutation;
    # a typed expired conflict is resolved by one authoritative read.
    response = controller.request("POST", path + "/revoke", (200, 409), {"reason": "Disposable timed masking eligibility cleanup"})
    if response.status == 200:
        assert_service_eligibility_assignment(response.payload, identity, assignment_id, "revoked", username)
        return "revoked"
    if _object(response.payload, "timed cleanup conflict").get("error_code") != "role_assignment_expired":
        raise SuiteError("timed eligibility cleanup returned an unrelated conflict")
    assert_timed_service_assignment(controller.request("GET", path, (200,)).payload,
                                    identity, assignment_id, role_id, expires_at, "expired", username)
    return "expired"


def exercise_service_role_membership_expiry(client: GatewayClient, engine_id: int, database: str,
        bank: Mapping[str, object], owner_identity: Mapping[str, object], run_id: str, deadline: float,
        environment: Mapping[str, str] | None = None) -> dict[str, object]:
    env = os.environ if environment is None else environment
    if any(not isinstance(env.get(key), str) or not env[key] for key in SERVICE_ROLE_ENVIRONMENT):
        raise SuiteError("role expiry requires the dedicated owner-only fixture credentials")
    if env["ADDP_ONLINE_SECURITY_ROLE_USER_USERNAME"] != "external-online-security-role-reader":
        raise SuiteError("role expiry must not use a personal username")
    public = GatewayClient(client.base_url, "", client.timeout)
    controller = GatewayClient(client.base_url, env["ADDP_ONLINE_SECURITY_ROLE_CONTROLLER_ACCESS_TOKEN"], client.timeout)
    reader = formal_service_role_login(public, env)
    identity = service_role_user_identity(reader, str(owner_identity["tenant_id"]), SERVICE_ROLE_READER_PERMISSIONS)
    operator = service_role_user_identity(controller, identity["tenant_id"], SERVICE_ROLE_EXPIRY_CONTROLLER_PERMISSIONS, iam_controller=True)
    if len({identity["principal_id"], operator["principal_id"], owner_identity["principal_id"]}) != 3:
        raise SuiteError("timed role consumer, IAM controller and governor must be distinct")
    initial_roles = service_subject_role_observation(reader, identity)
    if SERVICE_ELIGIBILITY_ROLE in initial_roles["role_keys"]:
        raise SuiteError("role expiry requires the previous eligibility to be revoked")
    previous_id = str(positive_int(env["ADDP_ONLINE_SECURITY_ROLE_ASSIGNMENT_ID"], "previous eligibility ID"))
    previous_path = "/api/v1/system/tenant/role_assignments/" + previous_id
    previous_assignment = _object(controller.request("GET", previous_path, (200,)).payload, "previous eligibility Assignment")
    assert_service_eligibility_assignment(previous_assignment, identity, previous_id, "revoked")
    role_id = str(positive_int(previous_assignment.get("role_id"), "masking eligibility role ID"))
    policy_id = service_id = assignment_id = None
    expires_at = None
    failure = None
    result = {}
    try:
        source = find_item(client, engine_id, database + "." + BANK_SOURCE, "table")
        locator = build_item_locator(engine_id, source)
        enrollment_id = str(bank["enrollment_id"])
        baseline = definition_array(client, "/api/v1/security/protection-baselines")
        previous_projection = role_projection(client, enrollment_id)
        policy = create_role_policy(client, str(bank["assessment_id"]), [SERVICE_ELIGIBILITY_ROLE])
        policy_id = str(policy["id"])
        installed = wait_role_projection(client, enrollment_id, previous_projection, deadline)
        policy_path = "/api/v1/security/protection-policies/" + policy_id
        published_policy = client.request("GET", policy_path, (200,)).payload
        if deadline - time.monotonic() < SERVICE_ROLE_EXPIRY_SECONDS + 15:
            raise SuiteError("role natural expiry has insufficient remaining suite budget")
        observed = service_subject_role_observation(reader, identity)
        expires_at = utc_timestamp(observed.get("observed_at"), "System database observation") + timedelta(seconds=SERVICE_ROLE_EXPIRY_SECONDS)
        created = _array(controller.request("POST", "/api/v1/system/tenant/role_assignments", (201,), {
            "membership_id": identity["membership_id"], "role_ids": [role_id], "valid_until": expires_at.isoformat(),
            "reason": "Disposable natural masking eligibility expiry; preserve main functional role"}).payload, "timed eligibility creation")
        if len(created) != 1:
            raise SuiteError("role expiry must create exactly one timed Assignment")
        new_id = str(positive_int(_object(created[0], "timed Assignment").get("id"), "timed Assignment ID"))
        if new_id == previous_id:
            raise SuiteError("role expiry must not overwrite the previous revoked Assignment")
        assignment_id = new_id
        before_assignment = assert_timed_service_assignment(created[0], identity, assignment_id, role_id, expires_at, "effective")
        # Granting the new timed assignment advances authorization_version.
        # Log in once after that mutation, then retain this exact session.
        active_reader = formal_service_role_login(public, env)
        active = service_role_user_identity(active_reader, identity["tenant_id"], SERVICE_ROLE_READER_PERMISSIONS)
        if ({k: v for k, v in active.items() if k != "authorization_version"} !=
                {k: v for k, v in identity.items() if k != "authorization_version"}
                or int(active["authorization_version"]) <= int(identity["authorization_version"])):
            raise SuiteError("timed assignment changed identity or functional permissions")
        roles_before = service_subject_role_observation(active_reader, active)
        if (roles_before["role_keys"] != sorted(initial_roles["role_keys"] + [SERVICE_ELIGIBILITY_ROLE])
                or utc_timestamp(roles_before.get("expires_at"), "current role observation deadline") != expires_at):
            raise SuiteError("current role observation must include only the timed eligibility and its exact deadline")
        context = _object(active_reader.request("GET", "/api/v1/system/auth/context", (200,)).payload, "timed User session")
        if utc_timestamp(_object(context.get("token"), "timed User credential").get("expires_at"), "Access Token expiry") <= expires_at + timedelta(seconds=15):
            raise SuiteError("User credential must outlive the role deadline and refusal probes")
        choice = owner_role_choice(active_reader, locator, source, policy)
        name = ("online-security-role-expiry-" + run_suffix(run_id, "-"))[:120].rstrip("-")
        body = service_payload(name, engine_id, locator, BANK_FIELDS)
        body["protocols"]["rest_api"]["formats"] = ["json", "csv"]
        body["data_config"]["protection_choices"] = [choice]
        service = _object(active_reader.request("POST", "/api/v1/service/query", (201,), body).payload, "timed Service")
        service_id = positive_int(service.get("id"), "timed Service ID")
        service_path = f"/api/v1/service/query/{service_id}"
        selected = active_reader.request("GET", service_path, (200,)).payload
        if (_object(selected, "timed Service definition").get("public_access") is not False or selected.get("status") != "active"
                or _object(selected.get("data_config"), "timed Service config").get("protection_choices") != [choice]):
            raise SuiteError("timed Service must persist the private role choice")
        query_path = "/api/query/" + urllib.parse.quote(name, safe="") + "/query"
        first, cursor, version = assert_service_role_page(active_reader, query_path, ["1", "2", "3", "4"], True)
        continuation, _, last_version = assert_service_role_page(active_reader, query_path, ["5"], True, cursor)
        before_wait = service_subject_role_observation(active_reader, active)
        if (last_version != version or SERVICE_ELIGIBILITY_ROLE not in before_wait["role_keys"]
                or utc_timestamp(before_wait.get("observed_at"), "baseline database observation") >= expires_at):
            raise SuiteError("timed role positive control did not finish before natural expiry")
        wait_protection_deadline(expires_at, deadline)
        after = service_role_user_identity(active_reader, identity["tenant_id"], SERVICE_ROLE_READER_PERMISSIONS)
        roles_after = service_subject_role_observation(active_reader, after)
        if (after != active or roles_after["role_keys"] != initial_roles["role_keys"]
                or utc_timestamp(roles_after.get("observed_at"), "expired database observation") < expires_at):
            raise SuiteError("natural expiry must retain the same session and functional permissions, removing only eligibility")
        assignment_path = "/api/v1/system/tenant/role_assignments/" + assignment_id
        expired_assignment = assert_timed_service_assignment(controller.request("GET", assignment_path, (200,)).payload,
                                                            active, assignment_id, role_id, expires_at, "expired")
        if (client.request("GET", policy_path, (200,)).payload != published_policy
                or role_projection(client, enrollment_id) != installed
                or active_reader.request("GET", service_path, (200,)).payload != selected):
            raise SuiteError("natural role expiry changed Policy, protection projection or saved Service choice")
        denials = assert_service_eligibility_denials(active_reader, query_path, cursor)
        recovery, recovered_version = restore_service_default(active_reader, service_path, query_path, choice, selected)
        if (recovered_version == version or service_role_user_identity(active_reader, identity["tenant_id"], SERVICE_ROLE_READER_PERMISSIONS) != active
                or client.request("GET", policy_path, (200,)).payload != published_policy
                or role_projection(client, enrollment_id) != installed
                or definition_array(client, "/api/v1/security/protection-baselines") != baseline
                or controller.request("GET", previous_path, (200,)).payload != previous_assignment):
            raise SuiteError("natural expiry recovery changed protection or previous revocation facts")
        result = {"identity_before": active, "identity_after": after, "same_access_session_valid": True,
            "functional_permissions_and_authorization_version_unchanged": True,
            "role_keys_before": roles_before["role_keys"], "role_keys_after": roles_after["role_keys"],
            "assignment_before": before_assignment, "assignment_after": expired_assignment,
            "database_observed_before": before_wait["observed_at"], "database_observed_after": roles_after["observed_at"],
            "natural_expiry_verified": True, "policy_projection_and_saved_choice_unchanged_at_refusal": True,
            "choice": choice, "service_id": service_id, "installed_projection": installed,
            "first_page": first, "valid_cursor_continuation": continuation,
            "old_cursor": {"length": len(cursor), "sha256": hashlib.sha256(cursor.encode()).hexdigest()},
            "same_session_refusals": denials, "explicit_default_recovery": recovery}
    except BaseException as error:
        failure = error
    errors = []
    assignment_cleanup = None
    if assignment_id is not None:
        try:
            assignment_cleanup = cleanup_timed_service_assignment(controller, identity, assignment_id, role_id, expires_at)
        except BaseException as error:
            errors.append(str(error))
    try:
        cleanup_service_role_resources(client, policy_id, service_id, failure)
    except BaseException as error:
        errors.append(str(error))
    if errors:
        raise SuiteError("natural role expiry cleanup failed: " + "; ".join(errors)) from failure
    if failure:
        raise failure
    if assignment_cleanup != "expired":
        raise SuiteError("natural role expiry cannot pass by revoking eligibility")
    return dict(result, created_resources=1, deleted_resources=1, residual_resources=0,
                timed_assignment_cleanup="expired", policy_revoked=True, service_deleted=True)


DEVELOP_ROLE_USERNAME = "external-online-security-develop-reader"
DEVELOP_ROLE_READER_PERMISSIONS = {"develop.task.execute", "develop.task.read", "develop.data_read.execute",
    "system.execution_authorization.create", "meta.catalog.read", "service.definition.read"}


def assert_develop_role_result(execution: Mapping[str, object], selected: bool) -> dict[str, object]:
    if execution.get("status") != "success":
        raise SuiteError("Develop role positive control must succeed")
    result = _object(_object(execution.get("metadata"), "Develop role metadata").get("result"), "Develop role result")
    columns = _array(result.get("columns"), "Develop role columns")
    rows = [_object(row, "Develop role row") for row in _array(
        _object(result.get("summary"), "Develop role summary").get("preview_rows"), "Develop role preview")]
    if [normalize_id(row.get("id")) for row in rows] != ["1", "2", "3", "4", "5"]:
        raise SuiteError("Develop role control lost, duplicated or reordered five rows")
    if selected:
        # Reuse the full adjacent-field oracle while checking the selected mask
        # independently; never infer the expected mask from the returned data.
        for index, row in enumerate(rows, 1):
            allowed = [set(BANK_FIELDS)]
            if index == 5:
                allowed.append(set(BANK_FIELDS) - {BANK_FIELD})
            if set(row) not in allowed:
                raise SuiteError("Develop selected result changed the field schema")
        default_rows = [dict(row, **{BANK_FIELD: BANK_MASKED[str(index)]}) for index, row in enumerate(rows, 1)]
        assert_bank_card_rows(default_rows, "Develop selected adjacent fields", columns)
        for index, row in enumerate(rows, 1):
            value = BANK_SOURCE_VALUES[str(index)]
            if row.get(BANK_FIELD) != ("*" * len(value) if value else None):
                raise SuiteError("Develop selected result did not apply the full role mask")
    else:
        assert_bank_card_rows(rows, "Develop default history", columns)
    return {"rows": 5, "selected_full_mask": selected, "default_mask": not selected,
            "non_sensitive_fields_preserved": True, "null_and_empty_verified": True}


def develop_role_snapshot(execution: Mapping[str, object], choice: Mapping[str, object]) -> dict[str, object]:
    value = _object(_object(execution.get("execution_config"), "Develop execution snapshot").get("content"), "Develop content snapshot")
    if value.get("protection_choices") != [choice]:
        raise SuiteError("Develop immutable execution snapshot lost the explicit role choice")
    execution_id = execution.get("execution_id")
    try:
        if not isinstance(execution_id, str) or str(uuid.UUID(execution_id)) != execution_id or uuid.UUID(execution_id).int == 0:
            raise ValueError
    except (ValueError, TypeError, AttributeError) as error:
        raise SuiteError("Develop role scenario requires a real execution UUID") from error
    return value


def exercise_develop_role_membership_expiry(client: GatewayClient, engine_id: int, database: str,
        bank: Mapping[str, object], owner_identity: Mapping[str, object], deadline: float,
        environment: Mapping[str, str] | None = None) -> dict[str, object]:
    env = os.environ if environment is None else environment
    username = env.get("ADDP_ONLINE_SECURITY_DEVELOP_ROLE_USERNAME")
    password = env.get("ADDP_ONLINE_SECURITY_DEVELOP_ROLE_PASSWORD")
    if username != DEVELOP_ROLE_USERNAME or not isinstance(password, str) or not password:
        raise SuiteError("Develop expiry requires its independent disposable reader credentials")
    # The existing formal login mechanism is shared; only this in-memory mapping
    # selects the independent Develop user. Credentials never enter evidence.
    login_env = {"ADDP_ONLINE_SECURITY_ROLE_USER_USERNAME": username, "ADDP_ONLINE_SECURITY_ROLE_USER_PASSWORD": password}
    public = GatewayClient(client.base_url, "", client.timeout)
    controller = GatewayClient(client.base_url, env["ADDP_ONLINE_SECURITY_ROLE_CONTROLLER_ACCESS_TOKEN"], client.timeout)
    reader = formal_service_role_login(public, login_env)
    identity = service_role_user_identity(reader, str(owner_identity["tenant_id"]), DEVELOP_ROLE_READER_PERMISSIONS)
    operator = service_role_user_identity(controller, identity["tenant_id"], SERVICE_ROLE_EXPIRY_CONTROLLER_PERMISSIONS, iam_controller=True)
    if len({identity["principal_id"], operator["principal_id"], owner_identity["principal_id"]}) != 3:
        raise SuiteError("Develop consumer, governor and IAM controller must be distinct")
    initial = service_subject_role_observation(reader, identity)
    if SERVICE_ELIGIBILITY_ROLE in initial["role_keys"]:
        raise SuiteError("Develop reader must start without masking eligibility")
    previous_id = str(positive_int(env["ADDP_ONLINE_SECURITY_ROLE_ASSIGNMENT_ID"], "Service revoked Assignment ID"))
    previous_path = "/api/v1/system/tenant/role_assignments/" + previous_id
    previous = _object(controller.request("GET", previous_path, (200,)).payload, "Service qualification audit")
    if (previous.get("status") != "revoked" or previous.get("role_key") != SERVICE_ELIGIBILITY_ROLE or previous.get("principal_id") == identity["principal_id"]
            or previous.get("username") != "external-online-security-role-reader"):
        raise SuiteError("Develop must reuse only the role definition, preserving the other user's revoked Assignment")
    role_id = str(positive_int(previous.get("role_id"), "eligibility Role ID"))
    policy_id = assignment_id = None
    expires_at = None
    failure = None
    result = {}
    try:
        source = find_item(client, engine_id, database + "." + BANK_SOURCE, "table")
        locator = build_item_locator(engine_id, source)
        enrollment_id = str(bank["enrollment_id"])
        baselines = definition_array(client, "/api/v1/security/protection-baselines")
        old_projection = role_projection(client, enrollment_id, "develop")
        policy = create_role_policy(client, str(bank["assessment_id"]), [SERVICE_ELIGIBILITY_ROLE], "develop")
        policy_id = str(policy["id"])
        installed = wait_role_projection(client, enrollment_id, old_projection, deadline, "develop")
        policy_path = "/api/v1/security/protection-policies/" + policy_id
        published = client.request("GET", policy_path, (200,)).payload
        observed = service_subject_role_observation(reader, identity)
        expires_at = utc_timestamp(observed.get("observed_at"), "Develop System database time") + timedelta(seconds=SERVICE_ROLE_EXPIRY_SECONDS)
        if deadline - time.monotonic() < SERVICE_ROLE_EXPIRY_SECONDS + 15:
            raise SuiteError("Develop role expiry has insufficient remaining suite budget")
        created = _array(controller.request("POST", "/api/v1/system/tenant/role_assignments", (201,), {
            "membership_id": identity["membership_id"], "role_ids": [role_id], "valid_until": expires_at.isoformat(),
            "reason": "Disposable Develop masking eligibility natural expiry"}).payload, "Develop timed Assignment")
        if len(created) != 1:
            raise SuiteError("Develop expiry must create exactly one Assignment")
        assignment_id = str(positive_int(_object(created[0], "Develop Assignment").get("id"), "Develop Assignment ID"))
        if assignment_id == previous_id:
            raise SuiteError("Develop cannot overwrite Service Assignment audit")
        before_assignment = assert_timed_service_assignment(created[0], identity, assignment_id, role_id, expires_at, "effective", username)
        active_reader = formal_service_role_login(public, login_env)
        active = service_role_user_identity(active_reader, identity["tenant_id"], DEVELOP_ROLE_READER_PERMISSIONS)
        if ({k:v for k,v in active.items() if k != "authorization_version"} != {k:v for k,v in identity.items() if k != "authorization_version"}
                or int(active["authorization_version"]) <= int(identity["authorization_version"])):
            raise SuiteError("Develop timed grant changed identity or functional permissions")
        roles_before = service_subject_role_observation(active_reader, active)
        context = _object(active_reader.request("GET", "/api/v1/system/auth/context", (200,)).payload, "Develop same session")
        if (roles_before["role_keys"] != sorted(initial["role_keys"] + [SERVICE_ELIGIBILITY_ROLE])
                or utc_timestamp(roles_before.get("expires_at"), "Develop observation deadline") != expires_at
                or utc_timestamp(_object(context.get("token"), "Develop credential").get("expires_at"), "Develop token expiry") <= expires_at + timedelta(seconds=15)):
            raise SuiteError("Develop requires the exact qualification deadline and a longer-lived session")
        choice = owner_role_choice(active_reader, locator, source, policy, "develop", engine_id)
        query = "SELECT " + ",".join(BANK_FIELDS) + " FROM " + BANK_SOURCE + " ORDER BY id"
        success = develop_execution(active_reader, engine_id, locator, deadline, query, "sql", choices=[choice])
        snapshot = develop_role_snapshot(success, choice)
        successful_id = success["execution_id"]
        path = "/api/v1/develop/executions/" + successful_id
        history_before = assert_develop_role_result(success, False)
        live_before = assert_develop_role_result(_object(active_reader.request("GET", path + "/result", (200,)).payload, "Develop live result"), True)
        before_wait = service_subject_role_observation(active_reader, active)
        if (SERVICE_ELIGIBILITY_ROLE not in before_wait["role_keys"]
                or utc_timestamp(before_wait.get("observed_at"), "Develop baseline database time") >= expires_at):
            raise SuiteError("Develop positive control must finish before eligibility expires")
        wait_protection_deadline(expires_at, deadline)
        after = service_role_user_identity(active_reader, identity["tenant_id"], DEVELOP_ROLE_READER_PERMISSIONS)
        roles_after = service_subject_role_observation(active_reader, after)
        if (after != active or roles_after["role_keys"] != initial["role_keys"]
                or utc_timestamp(roles_after.get("observed_at"), "Develop expired database time") < expires_at):
            raise SuiteError("Develop natural expiry must preserve the exact session, authorization version and permissions")
        assignment_after = assert_timed_service_assignment(controller.request("GET", "/api/v1/system/tenant/role_assignments/" + assignment_id, (200,)).payload,
            active, assignment_id, role_id, expires_at, "expired", username)
        history = _object(active_reader.request("GET", path, (200,)).payload, "Develop history after expiry")
        if (develop_role_snapshot(history, choice) != snapshot or client.request("GET", policy_path, (200,)).payload != published
                or role_projection(client, enrollment_id, "develop") != installed):
            raise SuiteError("Develop role expiry changed Policy, projection or saved execution choice")
        history_after = assert_develop_role_result(history, False)
        denied = active_reader.request("GET", path + "/result", (409,))
        error = _object(denied.payload, "Develop expired role delivery refusal")
        if (set(error) != {"error", "error_code"} or error.get("error_code") != "query_protection_roles_unavailable"
                or not isinstance(error.get("error"), str) or not error["error"].strip()):
            raise SuiteError("Develop live delivery requires exact role-unavailable 409 with no result")
        failed = develop_execution(active_reader, engine_id, locator, deadline, query, "sql", choices=[choice])
        develop_role_snapshot(failed, choice)
        refusal = validate_develop_protection_refusal(failed, "preparation", "protection_role_choice_unavailable")
        failed_history = _object(active_reader.request("GET", "/api/v1/develop/executions/" + failed["execution_id"], (200,)).payload, "Develop refused history")
        if failed_history.get("execution_id") != failed["execution_id"] or failed_history.get("error_details") != failed.get("error_details"):
            raise SuiteError("Develop role refusal must persist on the same execution")
        validate_develop_protection_refusal(failed_history, "preparation", "protection_role_choice_unavailable")
        recovery = develop_execution(active_reader, engine_id, locator, deadline, query, "sql", choices=[])
        recovery_evidence = assert_develop_role_result(recovery, False)
        recovery_live = assert_develop_role_result(_object(active_reader.request("GET", "/api/v1/develop/executions/" + recovery["execution_id"] + "/result", (200,)).payload, "Develop default recovery"), False)
        ids = [successful_id, failed["execution_id"], recovery["execution_id"]]
        if (len(set(ids)) != 3 or any(str(uuid.UUID(value)) != value or uuid.UUID(value).int == 0 for value in ids)
                or _object(recovery.get("execution_config"), "recovery snapshot").get("content", {}).get("protection_choices") != []
                or service_role_user_identity(active_reader, identity["tenant_id"], DEVELOP_ROLE_READER_PERMISSIONS) != active
                or client.request("GET", policy_path, (200,)).payload != published
                or role_projection(client, enrollment_id, "develop") != installed
                or definition_array(client, "/api/v1/security/protection-baselines") != baselines
                or controller.request("GET", previous_path, (200,)).payload != previous):
            raise SuiteError("Develop recovery changed immutable protection, identity or Service audit facts")
        result = {"identity_before": active, "identity_after": after, "same_access_session_valid": True,
            "functional_permissions_and_authorization_version_unchanged": True, "natural_expiry_verified": True,
            "role_keys_before": roles_before["role_keys"], "role_keys_after": roles_after["role_keys"],
            "assignment_before": before_assignment, "assignment_after": assignment_after,
            "database_observed_before": before_wait["observed_at"], "database_observed_after": roles_after["observed_at"],
            "policy_projection_and_execution_choice_unchanged": True, "choice": choice, "installed_projection": installed,
            "successful_execution_id": successful_id, "selected_live_result": live_before,
            "history_before": history_before, "history_after": history_after,
            "old_live_result_refusal": {"http_status": denied.status, "error_code": error["error_code"], "result_absent": True},
            "new_query_refusal": {**refusal, "execution_id": failed["execution_id"], "same_history_result_absent": True},
            "explicit_default_recovery": {**recovery_evidence, "execution_id": recovery["execution_id"], "live": recovery_live}}
    except BaseException as error:
        failure = error
    errors = []
    assignment_cleanup = None
    if assignment_id is not None:
        try:
            assignment_cleanup = cleanup_timed_service_assignment(controller, identity, assignment_id, role_id, expires_at, username)
        except BaseException as error:
            errors.append(str(error))
    try:
        cleanup_service_role_resources(client, policy_id, None, failure)
    except BaseException as error:
        errors.append(str(error))
    if errors:
        raise SuiteError("Develop natural eligibility expiry cleanup failed: " + "; ".join(errors)) from failure
    if failure:
        raise failure
    if assignment_cleanup != "expired":
        raise SuiteError("Develop natural eligibility cannot pass by revoking its Assignment")
    return dict(result, timed_assignment_cleanup="expired", policy_revoked=True,
                temporary_definitions_created=0, residual_resources=0)


def validity_snapshot(enrollment: Mapping[str, object], mode: str,
                      expected_deadline: datetime | None, *, expired: bool = False) -> dict[str, object]:
    if mode not in {"permanent", "until"} or (mode == "permanent") != (expected_deadline is None) or (expired and mode == "permanent"):
        raise SuiteError("invalid resource validity expectation")
    if enrollment.get("validity_mode") != mode or "expires_at" not in enrollment:
        raise SuiteError("resource protection validity mode is missing or differs")
    actual = enrollment["expires_at"]
    if expected_deadline is None:
        if actual is not None:
            raise SuiteError("permanent resource has an unexpected deadline")
    elif utc_timestamp(actual, "resource protection deadline") != expected_deadline:
        raise SuiteError("resource protection deadline changed")
    progress = [_object(item, "validity Owner") for item in _array(enrollment.get("owner_progress"), "resource validity owners")]
    if len(progress) != len(PROTECTION_OWNERS):
        raise SuiteError("resource validity requires all required Owner projections")
    result = {}
    for owner in PROTECTION_OWNERS:
        matches = [item for item in progress if item.get("consumer_owner") == owner]
        if len(matches) != 1:
            raise SuiteError("resource validity Owner is missing or duplicated")
        item = matches[0]
        if (item.get("acknowledged") is not True or item.get("projection_state") != "active"
                or item.get("validity") != ("expired" if expired else "valid")
                or "expires_at" not in item):
            raise SuiteError("resource validity is not synchronized or has the wrong time state: "
                             f"Owner={owner}, acknowledged={item.get('acknowledged')}, "
                             f"projection_state={item.get('projection_state')}, validity={item.get('validity')}")
        if expected_deadline is None:
            if item["expires_at"] is not None:
                raise SuiteError("permanent Owner still has a hidden deadline")
        elif utc_timestamp(item["expires_at"], "Owner deadline") != expected_deadline:
            raise SuiteError("Owner did not install the exact resource deadline")
        revision = item.get("revision")
        if not isinstance(revision, str) or not re.fullmatch(r"[0-9]{20}", revision):
            raise SuiteError("validity Owner revision is invalid")
        result[owner] = {key: item[key] for key in ("projection_id", "revision", "published_cursor")}
    return result


def wait_protection_deadline(expires_at: datetime, deadline: float) -> None:
    # Real elapsed wall-clock time; never modify clocks or bypass Owner stores.
    while datetime.now(timezone.utc) <= expires_at + timedelta(seconds=1):
        if time.monotonic() >= deadline:
            raise SuiteError("resource protection natural expiry exceeded the suite budget")
        time.sleep(1)


def exercise_resource_validity(client: GatewayClient, enrollment_id: str, source_engine_id: int,
                               target_engine_id: int, locator: str, parent_locator: str,
                               target_locator: str, service_name: str, task_name: str,
                               identity: Mapping[str, object], deadline: float,
                               tasks: list[int]) -> dict[str, object]:
    wait_for_algorithm_projections(client, enrollment_id, deadline)
    path = "/api/v1/security/protection-enrollments/" + enrollment_id
    original = _object(client.request("GET", path, (200,)).payload, "validity resource")
    initial = validity_snapshot(original, "permanent", None)
    processes = {owner: SUPPORT.ready_process(owner) for owner in OWNER_ACTIONS}
    configured_deadline = (datetime.now(timezone.utc) + timedelta(seconds=90)).replace(microsecond=0)
    def update(row, mode, until):
        return _object(client.request("PUT", path + "/validity", (200,), {
            "version": positive_int(row.get("version"), "validity resource version"),
            "validity_mode": mode, "expires_at": until.isoformat() if until else None,
        }).payload, "updated validity resource")
    def synchronized(mode, until):
        wait_for_algorithm_projections(client, enrollment_id, deadline)
        row = _object(client.request("GET", path, (200,)).payload, "synchronized validity resource")
        return row, validity_snapshot(row, mode, until)
    def normal_outputs(suffix):
        columns, rows = preview_rows(client, locator)
        return {"manager": assert_mysql_customer_rows(rows, "Manager validity " + suffix, columns),
                "develop": assert_mysql_customer_rows(develop_rows(client, source_engine_id, locator, deadline), "Develop validity " + suffix),
                "service": assert_mysql_customer_rows(service_rows(client, service_name), "Service validity " + suffix),
                "transfer": verify_mysql_transfer_output(client, target_engine_id, locator, parent_locator,
                    task_name + "_validity_" + suffix, identity, deadline, tasks)}
    changed = False
    result = {}
    try:
        changed = True
        bounded = update(original, "until", configured_deadline)
        bounded, installed = synchronized("until", configured_deadline)
        for owner in OWNER_ACTIONS:
            if (installed[owner]["projection_id"] != initial[owner]["projection_id"]
                    or installed[owner]["revision"] <= initial[owner]["revision"]):
                raise SuiteError("validity update did not publish higher stable Owner revisions")
        conflict = client.request("PUT", path + "/validity", (409,), {
            "version": original["version"], "validity_mode": "permanent", "expires_at": None,
        })
        if _object(conflict.payload, "stale validity rejection").get("error_code") != "resource_version_conflict":
            raise SuiteError("stale validity update did not produce a version conflict")
        before = normal_outputs("before")
        if datetime.now(timezone.utc) >= configured_deadline:
            raise SuiteError("bounded validity pre-expiry outputs finished after the deadline")
        wait_protection_deadline(configured_deadline, deadline)
        expiry_observed_at = datetime.now(timezone.utc).isoformat()
        expired = _object(client.request("GET", path, (200,)).payload, "expired resource")
        if validity_snapshot(expired, "until", configured_deadline, expired=True) != installed:
            raise SuiteError("natural expiry silently refreshed Owner rules")
        query = urllib.parse.urlencode({"locator": locator, "page": 1, "page_size": 100})
        manager = client.request("GET", "/api/v1/manager/preview?" + query, (403,))
        validate_manager_protection_refusal(manager)
        if any(key in _object(manager.payload, "expired preview error") for key in ("data", "rows", "columns")):
            raise SuiteError("expired preview exposed data or field structure")
        develop = develop_execution(client, source_engine_id, locator, deadline,
            "SELECT id, customer_code, email FROM customers ORDER BY id", "sql")
        if develop.get("status") != "failed" or "result" in (develop.get("metadata") or {}):
            raise SuiteError("expired Develop query returned a result")
        validate_develop_protection_refusal(develop, "preparation", "protection_expired")
        service = client.request("POST", "/api/query/" + urllib.parse.quote(service_name, safe="") + "/query", (403,), {
            "select": ["id", "customer_code", "email"], "order_by": [{"field": "id", "direction": "asc"}],
            "page": {"limit": 100}, "format": "json",
        })
        validate_service_protection_refusal(service)
        columns_before, rows_before = preview_rows(client, target_locator)
        _, transfer = create_and_run_task(client, transfer_payload(task_name + "_validity_expired", locator, parent_locator),
                                          deadline, tasks, expected_status="failed")
        transfer_diagnostic = validate_transfer_protection_refusal(transfer, "preparation", "data_protection_required", "protection_expired")
        if ("result" in (transfer.get("metadata") or {}) or "result" in transfer
                or nonnegative_int(transfer.get("records_written"), "expired Transfer writes") != 0
                or nonnegative_int(transfer.get("bytes_written"), "expired Transfer bytes") != 0
                or positive_int(transfer.get("trigger_by"), "expired Transfer initiator") != positive_int(identity["principal_id"], "ordinary User")):
            raise SuiteError("expired Transfer wrote data or changed its initiating User")
        if preview_rows(client, target_locator) != (columns_before, rows_before):
            raise SuiteError("expired Transfer changed the existing protected target")
        restored = update(expired, "permanent", None)
        restored, renewed = synchronized("permanent", None)
        for owner in OWNER_ACTIONS:
            if renewed[owner]["projection_id"] != installed[owner]["projection_id"] or renewed[owner]["revision"] <= installed[owner]["revision"]:
                raise SuiteError("explicit restoration did not publish newer Owner revisions")
        after = normal_outputs("restored")
        if validate_user_identity(client, positive_int(identity["tenant_id"], "validity Tenant")) != identity:
            raise SuiteError("validity acceptance changed the ordinary User or Tenant")
        if any(SUPPORT.ready_process(owner) != processes[owner] for owner in OWNER_ACTIONS):
            raise SuiteError("validity acceptance changed an Owner process")
        result = {"initial_permanent": initial, "configured_deadline": configured_deadline.isoformat(),
                  "bounded": installed, "expired": installed, "restored_permanent": renewed,
                  "before_expiry": before, "after_restoration": after,
                  "same_user_verified": True, "owner_processes_unchanged": True,
                  "natural_expiry_verified": True, "stale_version_rejected": True,
                  "expiry_observed_at": expiry_observed_at, "owner_processes": processes,
                  "expired_outputs": {"manager_http_status": 403, "develop_status": "failed", "develop_execution_id": develop["execution_id"], "service_http_status": 403,
                    "service_error_code": "data_protection_required", "transfer_diagnostic": transfer_diagnostic,
                    "transfer_execution_id": transfer["execution_id"], "records_written": 0, "bytes_written": 0,
                    "target_unchanged": True, "result_absent": True}, "permanent_restored": True}
    finally:
        if changed:
            current = _object(client.request("GET", path, (200,)).payload, "validity cleanup resource")
            if current.get("validity_mode") != "permanent":
                update(current, "permanent", None)
                synchronized("permanent", None)
    return result


def run_scenario(
    client: GatewayClient,
    tenant_id: int,
    source_engine_id: int,
    target_engine_id: int,
    source_database: str,
    run_id: str,
    timeout: float,
    *, role_environment: Mapping[str, str] | None = None,
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
    enrolling_refusal = exercise_unready_source(client, target_engine_id, positive_int(identity["principal_id"], "ordinary principal"), deadline)
    spatial_algorithms = exercise_spatial_algorithms(client, target_engine_id, run_id, deadline, timeout)
    bank_card_metadata = exercise_bank_card_metadata(
        client, source_engine_id, target_engine_id, source_database, run_id, identity, deadline, enrolling_refusal)
    service_role_cursor_revocation = exercise_service_role_cursor_revocation(
        client, source_engine_id, source_database, bank_card_metadata, identity, run_id, deadline)
    service_role_membership_revocation = exercise_service_role_membership_revocation(
        client, source_engine_id, source_database, bank_card_metadata, identity, run_id, deadline, role_environment)
    service_role_membership_expiry = exercise_service_role_membership_expiry(
        client, source_engine_id, source_database, bank_card_metadata, identity, run_id, deadline, role_environment)
    develop_role_membership_expiry = exercise_develop_role_membership_expiry(
        client, source_engine_id, source_database, bank_card_metadata, identity, deadline, role_environment)
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

        outage = exercise_owner_unavailability(client, tenant_id, {
            "develop": lambda: assert_email_suppressed(
                develop_rows(client, source_engine_id, source_locator, deadline), "Develop during Security outage"),
            "service": lambda: assert_email_suppressed(service_rows(client, service_name), "Service during Security outage"),
            "transfer": lambda: verify_mysql_outage_transfer(client, target_engine_id, source_locator,
                target_parent_locator, target_locator, task_name + "_outage", identity, deadline, task_ids),
        }, deadline)

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

        def manager_after_rediscovery():
            columns, rows = preview_rows(client, source_locator)
            return assert_mysql_customer_rows(rows, "Manager after rediscovery", columns)

        rediscovery = exercise_explicit_rediscovery(client, enrollment_id, {
            "manager": manager_after_rediscovery,
            "develop": lambda: assert_mysql_customer_rows(develop_rows(client, source_engine_id, source_locator, deadline), "Develop after rediscovery"),
            "service": lambda: assert_mysql_customer_rows(service_rows(client, service_name), "Service after rediscovery"),
            "transfer": lambda: verify_mysql_transfer_output(client, target_engine_id, source_locator,
                target_parent_locator, task_name + "_rediscovery", identity, deadline, task_ids),
        }, deadline)

        resource_validity = exercise_resource_validity(client, enrollment_id, source_engine_id, target_engine_id,
            source_locator, target_parent_locator, target_locator, service_name, task_name, identity, deadline, task_ids)
        result = {
            "resource_validity": resource_validity,
            "schema_version": "addp.security-mysql-owner-protection-online/v29",
            "result": "passed",
            "identity": identity,
            "governance": governance,
            "default_protection_transaction": default_protection_transaction,
            "spatial_algorithms": spatial_algorithms,
            "bank_card_metadata": bank_card_metadata,
            "service_role_cursor_revocation": service_role_cursor_revocation,
            "service_role_membership_revocation": service_role_membership_revocation,
            "service_role_membership_expiry": service_role_membership_expiry,
            "develop_role_membership_expiry": develop_role_membership_expiry,
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
            "security_unavailability": outage,
            "explicit_rediscovery": rediscovery,
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
            "created_resources": 8 + len(task_ids),
            "deleted_resources": 8 + len(task_ids),
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


def initialize_postgres_read_grants(
    authorizer: GatewayClient, consumer: GatewayClient, tenant_id: int, postgres_engine_id: int,
) -> None:
    """Prepare only the PostgreSQL reads consumed by the protection scenario."""
    validate_user_identity(consumer, tenant_id)
    targets = [(postgres_engine_id, "schema", TARGET_SCHEMA, name) for name in MONGODB_TRANSFER_TARGETS] + [
        (postgres_engine_id, "schema", TARGET_SCHEMA, SPATIAL_SOURCE),
        (postgres_engine_id, "schema", TARGET_SCHEMA, SPATIAL_TARGET),
        (postgres_engine_id, "schema", TARGET_SCHEMA, TARGET_TABLE),
        (postgres_engine_id, "schema", TARGET_SCHEMA, BANK_TARGET),
    ]
    wait_for_manager_search_ready(consumer, postgres_engine_id, "value_c", time.monotonic() + 60)
    wait_for_scan(consumer, postgres_engine_id, time.monotonic() + 180)
    try:
        REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, tenant_id, targets)
    except REGISTRATION.RegistrationError as error:
        raise SuiteError(str(error)) from error


def initialize_governance(client: GatewayClient, tenant_id: int) -> None:
    SUPPORT.initialize_fresh_governance(client, tenant_id, [
        {"code": "email", "protection": {"effect": "suppress", "invalid_value_effect": "suppress"}, "detector": EMAIL_DETECTOR},
        {"code": "phone", "detector": PHONE_DETECTOR, "protection": {"effect": "mask", "algorithm": STRUCTURED_MASK_ALGORITHM,
            "parameters": {"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"},
            "allowed_algorithms": [case[0] for case in ALGORITHM_CASES], "invalid_value_effect": "suppress"}},
        {"code": "bank_card", "detector": BANK_DETECTOR, "protection": {
            "effect": "mask", "algorithm": STRUCTURED_MASK_ALGORITHM,
            "parameters": {"prefix_runes": 1, "suffix_runes": 1, "mask_rune": "*"},
            "allowed_algorithms": [STRUCTURED_MASK_ALGORITHM], "invalid_value_effect": "suppress"}},
    ])


def result_browser_context(client: GatewayClient, engine_id: int, source: Mapping[str, object],
                           targets: Mapping[str, object], enrollment_id: str) -> dict[str, object]:
    phones = [row for row in definition_array(client, "/api/v1/security/sensitive-data-types") if row.get("code") == "phone"]
    if len(phones) != 1:
        raise SuiteError("result browser requires the exact phone type")
    baselines = [row for row in definition_array(client, "/api/v1/security/protection-baselines")
                 if row.get("sensitive_data_type_id") == phones[0]["id"]
                 and row.get("security_grade_id") == phones[0]["default_security_grade_id"] and row.get("enabled") is True]
    if len(baselines) != 1:
        raise SuiteError("result browser requires the exact phone baseline")
    original = baseline_body(baselines[0])
    original.pop("version")  # Restoration preserves semantics, never rolls back the CAS version.
    parameters = {"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"}
    if (original["effect"] != "mask" or original["algorithm"] != STRUCTURED_MASK_ALGORITHM
            or original["parameters"] != parameters or original["invalid_value_effect"] != "suppress"):
        raise SuiteError("result browser requires the restored default phone mask")
    target = MONGODB_TRANSFER_TARGETS[2]
    normal = mongodb_transfer_payload(build_item_locator(engine_id, source), targets, target, False)
    denied = mongodb_transfer_payload(build_item_locator(engine_id, source), targets, target, False)
    denied["config"]["target"]["policy"]["apply_mode"] = "append"
    return {"baseline_id": str(baselines[0]["id"]), "baseline_body": original,
            "enrollment_id": enrollment_id, "target_locator": targets["locators"][target],
            "columns": list(MONGODB_TRANSFER_FIELDS), "expected_phones": spatial_expected(STRUCTURED_MASK_ALGORITHM, parameters),
            "normal_payload": normal, "denied_payload": denied}


def validate_export_browser_report(payload: object, run_id: str, tenant_id: str) -> dict[str, object]:
    report = _object(payload, "Manager export browser report")
    expected = {
        "schema_version": "addp.security-manager-export-browser/v9", "result": "passed",
        "run_id": run_id, "tenant_id": tenant_id, "records": 5,
        "email_field_present": False, "non_sensitive_fields_preserved": True,
        "same_user_verified": True, "initiator_verified": True, "taskless_execution": True,
        "manager_source_verified": True, "monitor_detail_visible": True,
        "protected_preview_verified": True, "technical_field_search_verified": True,
        "search_to_preview_verified": True,
        "resource_combination_filters_verified": True, "resource_literal_filter_verified": True,
        "resource_scope_intersection_verified": True, "resource_reset_verified": True,
        "resource_filter_total_verified": True, "resource_filters_read_only": True,
        "owner_validity_visible": True, "resource_filter_case_count": 9,
        "browser_session_rebound_verified": True, "resource_permanent_validity_visible": True,
        "develop_alias_protected": True, "develop_refusal_rows": 0,
        "develop_refusal_diagnostic_verified": True, "develop_refusal_visible": True,
        "develop_monitor_same_execution": True,
        "additional_protection_histories_verified": True, "additional_protection_monitors_verified": True,
        "additional_protection_refusal_count": 3,
        "service_normal_output_verified": True, "service_refusal_http_status": 403,
        "service_refusal_visible": True, "service_old_preview_cleared": True, "service_restored_output_verified": True,
        "transfer_refusal_diagnostic_verified": True, "transfer_refusal_visible": True, "transfer_monitor_same_execution": True,
        "transfer_target_unchanged": True, "output_protection_permanent_restored": True,
        "output_browser_created_resources": 2, "output_browser_deleted_resources": 2, "output_browser_residual_resources": 0,
        "expected_service_refusal_403": 1,
        "transfer_result_stage": "result", "transfer_result_diagnostic_verified": True,
        "transfer_result_visible": True, "transfer_result_monitor_same_execution": True,
        "transfer_result_target_unchanged": True, "transfer_result_records_written": 0,
        "transfer_result_baseline_restored": True, "transfer_result_recovery_verified": True,
        "result_browser_created_resources": 2, "result_browser_deleted_resources": 2, "result_browser_residual_resources": 0,
        "browser_warning_errors": 0, "failed_business_responses": 0, "anonymous_refresh_401": 1,
    }
    mismatches = [key for key, value in expected.items()
                  if type(report.get(key)) is not type(value) or report.get(key) != value]
    if set(report) != set(expected) | {"execution_id", "protection_transfer_execution_id", "result_transfer_execution_id"}:
        mismatches.append("report_fields")
    execution_id = report.get("execution_id")
    try:
        valid_uuid = (isinstance(execution_id, str) and str(uuid.UUID(execution_id)) == execution_id
                      and uuid.UUID(execution_id).int != 0)
    except ValueError:
        valid_uuid = False
    if not valid_uuid:
        mismatches.append("execution_id")
    refusal_id = report.get("protection_transfer_execution_id")
    try:
        valid_refusal_id = (isinstance(refusal_id, str) and str(uuid.UUID(refusal_id)) == refusal_id
                            and uuid.UUID(refusal_id).int != 0 and refusal_id != execution_id)
    except ValueError:
        valid_refusal_id = False
    if not valid_refusal_id:
        mismatches.append("protection_transfer_execution_id")
    result_id = report.get("result_transfer_execution_id")
    try:
        valid_result_id = (isinstance(result_id, str) and str(uuid.UUID(result_id)) == result_id
                           and uuid.UUID(result_id).int != 0 and result_id not in {execution_id, refusal_id})
    except (ValueError, TypeError):
        valid_result_id = False
    if not valid_result_id:
        mismatches.append("result_transfer_execution_id")
    if mismatches:
        raise SuiteError("Manager export browser report mismatch: " + ", ".join(mismatches))
    return report


def run_export_browser(repository: Path, environment: Mapping[str, str], locator: str,
                       output_context: Mapping[str, object]) -> dict[str, object]:
    required = ("ADDP_ONLINE_ARTIFACT_DIR", "ADDP_ONLINE_TEST_RUN_ID", "ADDP_ONLINE_TEST_TENANT_ID",
                "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN", "ADDP_ONLINE_TEST_USER_USERNAME",
                "ADDP_ONLINE_TEST_USER_PASSWORD", "ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID", "GATEWAY_URL", "CONSOLE_URL")
    missing = [key for key in required if not environment.get(key)]
    if missing:
        raise SuiteError("Manager export browser environment is missing: " + ", ".join(missing))
    report_path = Path(environment["ADDP_ONLINE_ARTIFACT_DIR"]) / "security-manager-export-browser.json"
    report_path.unlink(missing_ok=True)
    screenshots = [report_path.parent / name for name in (
        "security-manager-protected-preview.png", "security-manager-field-search.png",
        "security-manager-export.png", "security-manager-export-monitor.png",
        "security-resource-combination-filters.png", "security-resource-empty-intersection.png",
        "security-resource-filter-reset.png", "security-develop-protection-refusal.png",
        "security-develop-protection-monitor.png", "security-service-protection-refusal.png",
        "security-service-protection-restored.png", "security-transfer-protection-refusal.png",
        "security-transfer-protection-monitor.png", "security-transfer-result-refusal.png", "security-transfer-result-monitor.png",
    )] + [report_path.parent / f"security-develop-{name}-{view}.png"
          for name in ("not_ready", "operation_unsupported", "schema_mismatch") for view in ("history", "monitor")]
    for screenshot in screenshots:
        screenshot.unlink(missing_ok=True)
    browser_environment = dict(environment, ADDP_ONLINE_SECURITY_EXPORT_LOCATOR=locator,
                               ADDP_ONLINE_SECURITY_OUTPUT_BROWSER_CONTEXT=json.dumps(
                                   dict(output_context, required_owners=list(PROTECTION_OWNERS)), ensure_ascii=False))
    for key in ROLE_ENVIRONMENT:
        browser_environment.pop(key, None)
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
    parser.add_argument("--initialize", action="store_true", help="initialize fresh Hosted governance and PostgreSQL table read Grants through formal APIs")
    arguments = parser.parse_args()
    signal.signal(signal.SIGTERM, handle_termination)
    if arguments.initialize:
        require_hosted_initialization(os.environ)
        initialize_governance(GatewayClient(required_environment("GATEWAY_URL"),
            required_environment("ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN"), 30),
            positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "Tenant id"))
        initialize_postgres_read_grants(
            GatewayClient(required_environment("GATEWAY_URL"), required_environment("ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"), 30),
            GatewayClient(required_environment("GATEWAY_URL"), required_environment("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"), 30),
            positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "Tenant id"),
            positive_int(required_environment("ADDP_ONLINE_TEST_ENGINE_ID"), "PostgreSQL Engine id"),
        )
        print("Disposable Security governance and PostgreSQL table read Grants are ready")
        return 0
    if os.environ.get("ADDP_ONLINE_TEST") != "1":
        raise SuiteError("ADDP_ONLINE_TEST must be exactly 1")
    SUPPORT.require_hosted_security_lifecycle(Path(__file__).resolve().parents[2])
    # Keep the IAM controller and dedicated reader out of every lifecycle and
    # browser subprocess; only the bounded membership scenario receives them.
    role_environment = {key: required_environment(key) for key in ROLE_ENVIRONMENT}
    for key in ROLE_ENVIRONMENT:
        os.environ.pop(key, None)
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
    # Submit the initial technical snapshot while no protection mutation can
    # isolate Manager's index. A successful Meta scan alone is not index proof.
    validate_user_identity(client, tenant_id)
    wait_for_manager_search_ready(client, mongodb_engine_id, MONGODB_FIELD, time.monotonic() + min(timeout, 60))
    mongodb_source = prepare_mongodb_source(client, mongodb_engine_id, time.monotonic() + timeout)
    mongodb_transfer_targets = prepare_mongodb_transfer_targets(client, target_engine_id, time.monotonic() + timeout)
    report = run_scenario(
        client,
        tenant_id,
        source_engine_id,
        target_engine_id,
        required_environment("ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE"),
        required_environment("ADDP_ONLINE_TEST_RUN_ID"),
        timeout,
        role_environment=role_environment,
    )
    report["mongodb_algorithms"] = exercise_mongodb_algorithms(client, mongodb_engine_id, mongodb_source,
        mongodb_transfer_targets, time.monotonic() + timeout, timeout)
    report["protection_reason_matrix"] = protection_reason_matrix(report)
    source_item = find_item(client, source_engine_id,
                            f"{required_environment('ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE')}.{SOURCE_TABLE}", "table")
    # Projection changes and owned-resource cleanup queue external index purges.
    # Require the final protected field to be searchable before browser entry.
    report["manager_export_search_readiness"] = wait_for_technical_field(
        client, source_engine_id, source_item, "email", time.monotonic() + min(timeout, 120))
    target_item = find_item(client, target_engine_id, f"{TARGET_SCHEMA}.{TARGET_TABLE}", "table")
    source_locator = build_item_locator(source_engine_id, source_item)
    browser_name = "online-security-browser-" + run_suffix(required_environment("ADDP_ONLINE_TEST_RUN_ID"), "-")
    output_context = {
        "principal_id": report["identity"]["principal_id"],
        "enrollment_id": report["fixture"]["security_enrollment_id"],
        "target_locator": build_item_locator(target_engine_id, target_item),
        "service_payload": service_payload(browser_name, source_engine_id, source_locator),
        "transfer_payload": transfer_payload(browser_name, source_locator, build_schema_locator(target_engine_id, target_item)),
        "result_protection": result_browser_context(client, mongodb_engine_id, mongodb_source["item"],
            mongodb_transfer_targets, report["mongodb_algorithms"]["enrollment_id"]),
        "additional_refusals": {name: report["bank_card_metadata"]["refusal_causes"][name]
                                for name in ("not_ready", "operation_unsupported", "schema_mismatch")},
    }
    report["manager_export_browser"] = run_export_browser(
        Path(__file__).resolve().parents[2], dict(os.environ), source_locator, output_context)
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SuiteError as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
