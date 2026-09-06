#!/usr/bin/env python3
"""Accept Manager-originated plaintext access request and Security approval."""

from __future__ import annotations

import importlib.util
import json
import os
import sys
import time
import urllib.parse
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Iterable, Mapping


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


SOURCE_FULL_NAME = "addp_online_security.exemption_source"
RAW_VALUES = {"1": "13812345678", "2": "13987654321", "3": None}
MASKED_VALUES = {"1": "138****5678", "2": "139****4321", "3": None}
FORBIDDEN_ADMIN_ROLES = SUPPORT.FORBIDDEN_ADMIN_ROLES
APPLICANT_PERMISSIONS = {
    "manager.data_item.read",
    "meta.catalog.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
    "security.assessment.read",
    "security.enrollment.create",
    "security.enrollment.read",
    "security.finding.read",
    "security.finding.update",
    "security.protection_access_request.create",
    "security.protection_access_request.read",
}
APPROVER_PERMISSIONS = {
    "manager.data_item.read",
    "security.protection_access_request.update",
    "security.protection_exemption.delete",
    "security.protection_exemption.read",
}


def validate_user_identity(
    client: GatewayClient,
    tenant_id: int,
    required_permissions: set[str],
    role_name: str,
) -> dict[str, object]:
    context = _object(
        client.request("GET", "/api/v1/system/auth/context", (200,)).payload,
        f"{role_name} AuthContext",
    )
    principal = _object(context.get("principal"), f"{role_name} principal")
    tenant = _object(context.get("context"), f"{role_name} tenant context")
    token = _object(context.get("token"), f"{role_name} token")
    authorization = _object(
        context.get("authorization"), f"{role_name} authorization"
    )
    if principal.get("type") != "user":
        raise SuiteError(f"{role_name} token must belong to a User")
    principal_id = positive_int(principal.get("id"), f"{role_name} principal.id")
    if tenant.get("type") != "tenant" or tenant.get("tenant_id") != str(tenant_id):
        raise SuiteError(f"{role_name} token must use the configured Tenant Context")
    if token.get("type") not in {"first_party_access_token", "oauth_access_token"}:
        raise SuiteError(f"{role_name} token must be a User Access Token")
    assignments = _array(
        authorization.get("role_assignments"), f"{role_name} role assignments"
    )
    roles: set[str] = set()
    permissions: set[str] = set()
    for raw in assignments:
        assignment = _object(raw, f"{role_name} role assignment")
        role = assignment.get("role_key")
        granted = assignment.get("permissions")
        if not isinstance(role, str) or not isinstance(granted, list) or not all(
            isinstance(key, str) for key in granted
        ):
            raise SuiteError(f"{role_name} AuthContext role assignment is incomplete")
        roles.add(role)
        permissions.update(granted)
    forbidden = roles & FORBIDDEN_ADMIN_ROLES
    if forbidden:
        raise SuiteError(
            f"{role_name} token must not use administrator roles: "
            + ", ".join(sorted(forbidden))
        )
    missing = required_permissions - permissions
    if missing:
        raise SuiteError(
            f"{role_name} token is missing required permissions: "
            + ", ".join(sorted(missing))
        )
    return {
        "principal_id": str(principal_id),
        "principal_type": "user",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(required_permissions),
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


def wait_for_manager_projection(
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
        if enrollment.get("state") in {"releasing", "released"}:
            raise SuiteError("the permanent plaintext-access fixture is released")
        progresses = enrollment.get("owner_progress")
        if enrollment.get("state") == "active" and isinstance(progresses, list):
            manager = [
                item
                for item in progresses
                if isinstance(item, dict) and item.get("consumer_owner") == "manager"
            ]
            if len(manager) == 1 and manager[0].get("acknowledged") is True:
                rules = manager[0].get("rules")
                effects = (
                    {
                        rule.get("effect")
                        for rule in rules
                        if isinstance(rule, dict) and rule.get("action") == "preview"
                    }
                    if isinstance(rules, list)
                    else set()
                )
                if effects & {"mask", "suppress", "deny"}:
                    return enrollment
        time.sleep(1)
    raise SuiteError("Manager protection projection did not converge before the deadline")


def ensure_enrollment(
    client: GatewayClient, engine_id: int, locator: str, deadline: float
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
        raise SuiteError("the plaintext-access fixture has multiple active enrollments")
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
    return wait_for_manager_projection(client, enrollment_id, deadline), initialized


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
            return _object(reviewed.get("assessment"), "phone Assessment"), True
        time.sleep(1)
    raise SuiteError("phone Assessment was not available before the deadline")


def normalize_key(value: object) -> str:
    if isinstance(value, float) and value.is_integer():
        return str(int(value))
    return str(value)


def assert_phone_values(
    rows: Iterable[Mapping[str, object]], expected: Mapping[str, object], phase: str
) -> dict[str, object]:
    values = {
        normalize_key(row.get("id")): row.get("phone")
        for row in rows
        if row.get("id") is not None
    }
    if values != dict(expected):
        raise SuiteError(f"Manager phone values differ in {phase} phase")
    return {"rows": len(values), "plaintext": values == RAW_VALUES}


def manager_evidence(
    client: GatewayClient,
    locator: str,
    expected: Mapping[str, object],
    phase: str,
) -> dict[str, object]:
    _, rows = preview_rows(client, locator)
    return assert_phone_values(rows, expected, phase)


def wait_for_manager_values(
    client: GatewayClient,
    locator: str,
    expected: Mapping[str, object],
    phase: str,
    deadline: float,
) -> dict[str, object]:
    last_error: BaseException | None = None
    while time.monotonic() < deadline:
        try:
            return manager_evidence(client, locator, expected, phase)
        except SuiteError as error:
            last_error = error
            time.sleep(1)
    raise SuiteError(f"Manager did not reach {phase} state: {last_error}")


def access_targets(
    client: GatewayClient, target_identity: str
) -> list[dict[str, object]]:
    path = "/api/v1/security/protection-access-request-targets?" + urllib.parse.urlencode(
        {
            "target_identity": target_identity,
            "consumer_owner": "manager",
            "action": "preview",
        }
    )
    payload = _object(client.request("GET", path, (200,)).payload, "access targets")
    return [
        item
        for item in _array(payload.get("data"), "access targets data")
        if isinstance(item, dict)
    ]


def reject_stale_pending_requests(
    approver: GatewayClient, applicant_id: str, assessment_id: str
) -> None:
    for request in list_pages(
        approver, "/api/v1/security/protection-access-requests/review-queue"
    ):
        if (
            request.get("subject_type") == "user"
            and request.get("subject_id") == applicant_id
            and request.get("assessment_id") == assessment_id
            and request.get("consumer_owner") == "manager"
            and request.get("action") == "preview"
        ):
            request_id = request.get("id")
            if not isinstance(request_id, str):
                raise SuiteError("stale access request id is missing")
            approver.request(
                "POST",
                f"/api/v1/security/protection-access-requests/{urllib.parse.quote(request_id)}/decisions",
                (200,),
                {
                    "version": positive_int(request.get("version"), "request version"),
                    "decision": "reject",
                    "rationale": "Dedicated Online stale request cleanup",
                },
            )


def list_exemptions(
    client: GatewayClient, enrollment_id: str
) -> list[dict[str, object]]:
    return list_pages(
        client,
        "/api/v1/security/protection-exemptions?"
        + urllib.parse.urlencode({"enrollment_id": enrollment_id}),
    )


def revoke_active_grants(
    approver: GatewayClient,
    enrollment_id: str,
    assessment_id: str,
    applicant_id: str,
    rationale: str,
) -> None:
    for exemption in list_exemptions(approver, enrollment_id):
        if (
            exemption.get("assessment_id") == assessment_id
            and exemption.get("consumer_owner") == "manager"
            and exemption.get("action") == "preview"
            and exemption.get("subject_type") == "user"
            and exemption.get("subject_id") == applicant_id
            and exemption.get("effective_state") == "active"
        ):
            exemption_id = exemption.get("id")
            if not isinstance(exemption_id, str):
                raise SuiteError("active grant id is missing")
            approver.request(
                "DELETE",
                f"/api/v1/security/protection-exemptions/{urllib.parse.quote(exemption_id)}",
                (200,),
                {
                    "version": positive_int(exemption.get("version"), "grant version"),
                    "rationale": rationale,
                },
            )


def run_scenario(
    applicant: GatewayClient,
    approver: GatewayClient,
    tenant_id: int,
    engine_id: int,
    timeout: float,
    authorization_seconds: int = 40,
) -> dict[str, object]:
    deadline = time.monotonic() + timeout
    applicant_identity = validate_user_identity(
        applicant, tenant_id, APPLICANT_PERMISSIONS, "applicant"
    )
    approver_identity = validate_user_identity(
        approver, tenant_id, APPROVER_PERMISSIONS, "approver"
    )
    if applicant_identity["principal_id"] == approver_identity["principal_id"]:
        raise SuiteError("applicant and approver must be two different Users")

    scan_execution_id = wait_for_scan(applicant, engine_id, deadline)
    source_item = find_item(applicant, engine_id, SOURCE_FULL_NAME, "table")
    source_locator = build_item_locator(engine_id, source_item)
    target_identity = source_item.get("fingerprint")
    if not isinstance(target_identity, str) or not target_identity:
        raise SuiteError("source DataItem resource_identity is missing")
    enrollment, enrollment_initialized = ensure_enrollment(
        applicant, engine_id, source_locator, deadline
    )
    assessment, assessment_initialized = ensure_phone_assessment(
        applicant, enrollment, deadline
    )
    enrollment_id = enrollment.get("id")
    assessment_id = assessment.get("id")
    if not isinstance(enrollment_id, str) or not isinstance(assessment_id, str):
        raise SuiteError("plaintext-access governance identity is missing")
    applicant_id = str(applicant_identity["principal_id"])

    reject_stale_pending_requests(approver, applicant_id, assessment_id)
    revoke_active_grants(
        approver,
        enrollment_id,
        assessment_id,
        applicant_id,
        "Dedicated Online precondition cleanup",
    )
    baseline = wait_for_manager_values(
        applicant, source_locator, MASKED_VALUES, "baseline", deadline
    )

    target = [
        item
        for item in access_targets(applicant, target_identity)
        if item.get("assessment_id") == assessment_id
    ]
    if len(target) != 1 or target[0].get("requestable") is not True:
        raise SuiteError("formal phone field is not requestable from Manager preview")
    requested_expires_at = datetime.now(timezone.utc) + timedelta(
        seconds=authorization_seconds
    )
    created = _object(
        applicant.request(
            "POST",
            "/api/v1/security/protection-access-requests",
            (201,),
            {
                "assessment_id": assessment_id,
                "consumer_owner": "manager",
                "action": "preview",
                "requested_expires_at": requested_expires_at.isoformat(),
                "rationale": "Dedicated Online Manager plaintext verification",
            },
        ).payload,
        "ProtectionAccessRequest",
    )
    request_id = created.get("id")
    if not isinstance(request_id, str) or created.get("state") != "pending":
        raise SuiteError("ProtectionAccessRequest was not created as pending")

    reviewer_queue = list_pages(
        approver, "/api/v1/security/protection-access-requests/review-queue"
    )
    matches = [item for item in reviewer_queue if item.get("id") == request_id]
    if len(matches) != 1:
        raise SuiteError("approver cannot find the pending access request")
    approved = _object(
        approver.request(
            "POST",
            f"/api/v1/security/protection-access-requests/{urllib.parse.quote(request_id)}/decisions",
            (200,),
            {
                "version": positive_int(matches[0].get("version"), "request version"),
                "decision": "approve",
                "expires_at": requested_expires_at.isoformat(),
                "rationale": "Dedicated Online independent approval",
            },
        ).payload,
        "approved ProtectionAccessRequest",
    )
    exemption_id = approved.get("exemption_id")
    if approved.get("state") != "approved" or not isinstance(exemption_id, str):
        raise SuiteError("approval did not create a subject-scoped grant")

    authorized = wait_for_manager_values(
        applicant, source_locator, RAW_VALUES, "authorized", deadline
    )
    other_subject = wait_for_manager_values(
        approver, source_locator, MASKED_VALUES, "other-subject", deadline
    )

    remaining = (requested_expires_at - datetime.now(timezone.utc)).total_seconds()
    if remaining > 0:
        time.sleep(remaining + 1)
    # No Security request is allowed after this point. Manager must evaluate the
    # embedded subject grant expiry locally and restore the default protection.
    expired = wait_for_manager_values(
        applicant, source_locator, MASKED_VALUES, "expired", deadline
    )

    return {
        "schema_version": "addp.security-plaintext-access-online/v1",
        "result": "passed",
        "identities": {
            "applicant": applicant_identity,
            "approver": approver_identity,
        },
        "fixture": {
            "engine_id": str(engine_id),
            "scan_execution_id": scan_execution_id,
            "security_enrollment_id": enrollment_id,
            "security_assessment_id": assessment_id,
            "enrollment_initialized": enrollment_initialized,
            "assessment_initialized": assessment_initialized,
        },
        "lifecycle": {
            "owner": "manager",
            "action": "preview",
            "subject_type": "user",
            "authorization_seconds": authorization_seconds,
            "baseline": baseline,
            "authorized_applicant": authorized,
            "protected_other_subject": other_subject,
            "expired_without_security_refresh": expired,
        },
        "created_resources": 0,
        "deleted_resources": 0,
        "residual_resources": 0,
        "audit_records_retained": 2,
    }


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
    gateway_url = required_environment("GATEWAY_URL")
    applicant = GatewayClient(
        gateway_url,
        required_environment("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"),
        min(timeout, 30),
    )
    approver = GatewayClient(
        gateway_url,
        required_environment("ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN"),
        min(timeout, 30),
    )
    report = run_scenario(applicant, approver, tenant_id, engine_id, timeout)
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SuiteError as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
