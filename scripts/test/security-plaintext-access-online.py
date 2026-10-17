#!/usr/bin/env python3
"""Accept Manager-originated plaintext access request and Security approval."""

from __future__ import annotations

import argparse
import importlib.util
import json
import os
import subprocess
import sys
import time
import urllib.parse
import uuid
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
REGISTRATION_SPEC = importlib.util.spec_from_file_location("plaintext_engine_registration", Path(__file__).with_name("online-engine-registration.py"))
REGISTRATION = importlib.util.module_from_spec(REGISTRATION_SPEC)
assert REGISTRATION_SPEC.loader is not None
REGISTRATION_SPEC.loader.exec_module(REGISTRATION)
DEVELOP_SPEC = importlib.util.spec_from_file_location("plaintext_develop_output", Path(__file__).with_name("security-mysql-owner-protection-online.py"))
DEVELOP = importlib.util.module_from_spec(DEVELOP_SPEC)
assert DEVELOP_SPEC.loader is not None
DEVELOP_SPEC.loader.exec_module(DEVELOP)


SOURCE_FULL_NAME = "addp_online_security.exemption_source"
RAW_VALUES = {"1": "13812345678", "2": "13987654321", "3": None}
MASKED_VALUES = {"1": "138****5678", "2": "139****4321", "3": None}
FORBIDDEN_ADMIN_ROLES = SUPPORT.FORBIDDEN_ADMIN_ROLES
APPLICANT_PERMISSIONS = {
    "develop.task.read", "develop.task.execute", "develop.data_read.execute", "system.execution_authorization.create",
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
    "develop.task.read", "develop.task.execute", "develop.data_read.execute", "system.execution_authorization.create",
    "security.enrollment.read",
    "meta.catalog.read",
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
    if permissions != required_permissions:
        raise SuiteError(f"{role_name} token must have only its minimum suite permissions")
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
    rows = list(rows)
    if len(rows) != len(expected) or any("id" not in row or "phone" not in row for row in rows):
        raise SuiteError(f"Manager rows or phone field are missing in {phase} phase")
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
    client: GatewayClient, target_identity: str, owner: str = "manager", action: str = "preview"
) -> list[dict[str, object]]:
    path = "/api/v1/security/protection-access-request-targets?" + urllib.parse.urlencode(
        {
            "target_identity": target_identity,
            "consumer_owner": owner,
            "action": action,
        }
    )
    payload = _object(client.request("GET", path, (200,)).payload, "access targets")
    return [
        item
        for item in _array(payload.get("data"), "access targets data")
        if isinstance(item, dict)
    ]


def approve_access_request(applicant: GatewayClient, approver: GatewayClient,
                           assessment_id: str, authorization_seconds: int,
                           scopes: tuple[tuple[str, str], ...] = (("manager", "preview"),)) -> tuple[str, str, datetime]:
    expires_at = datetime.now(timezone.utc) + timedelta(seconds=authorization_seconds)
    created = _object(applicant.request("POST", "/api/v1/security/protection-access-requests", (201,), {
        "assessment_id": assessment_id, "scopes": [{"consumer_owner": owner, "action": action} for owner, action in scopes],
        "requested_expires_at": expires_at.isoformat(), "rationale": "Hosted Online Manager plaintext verification",
    }).payload, "ProtectionAccessRequest")
    request_id = created.get("id")
    if not isinstance(request_id, str) or not request_id or created.get("state") != "pending":
        raise SuiteError("ProtectionAccessRequest was not created as pending")
    applicant.request("POST",
        f"/api/v1/security/protection-access-requests/{urllib.parse.quote(request_id)}/decisions", (403,), {
            "version": positive_int(created.get("version"), "request version"), "decision": "approve",
            "expires_at": expires_at.isoformat(), "rationale": "Hosted Online applicant cannot approve",
        })
    matches = [item for item in list_pages(approver,
        "/api/v1/security/protection-access-requests/review-queue?scope=pending") if item.get("id") == request_id]
    if len(matches) != 1:
        raise SuiteError("approver cannot find the pending access request")
    approved = _object(approver.request("POST",
        f"/api/v1/security/protection-access-requests/{urllib.parse.quote(request_id)}/decisions", (200,), {
            "version": positive_int(matches[0].get("version"), "request version"), "decision": "approve",
            "expires_at": expires_at.isoformat(), "rationale": "Hosted Online independent approval",
        }).payload, "approved ProtectionAccessRequest")
    authorizations = _array(approved.get("authorizations"), "approved authorizations")
    if len(authorizations) != len(scopes) or {(grant.get("consumer_owner"), grant.get("action")) for grant in authorizations} != set(scopes):
        raise SuiteError("approval changed its explicit outlet scopes")
    exemption_id = next(grant.get("exemption_id") for grant in authorizations if (grant.get("consumer_owner"),grant.get("action")) == scopes[0])
    if approved.get("state") != "approved" or not isinstance(exemption_id, str) or not exemption_id:
        raise SuiteError("approval did not create a subject-scoped grant")
    return request_id, exemption_id, expires_at


def revoke_exemption(approver: GatewayClient, exemption_id: str) -> None:
    path = f"/api/v1/security/protection-exemptions/{urllib.parse.quote(exemption_id)}"
    exemption = _object(approver.request("GET", path, (200,)).payload, "ProtectionExemption")
    if exemption.get("effective_state") != "active":
        raise SuiteError("the owned grant is not active before revocation")
    approver.request("DELETE", path, (200,), {"version": positive_int(exemption.get("version"), "grant version"),
        "rationale": "Hosted Online revocation verification"})


def verify_approval_history(approver: GatewayClient, request_id: str, authorization_state: str) -> dict[str, object]:
    matches = [item for item in list_pages(approver,
        "/api/v1/security/protection-access-requests/review-queue?" + urllib.parse.urlencode({
            "scope": "history", "state": "approved", "authorization_state": authorization_state,
        })) if item.get("id") == request_id]
    if len(matches) != 1 or matches[0].get("state") != "approved" or not any(isinstance(grant, dict) and grant.get("consumer_owner") == "manager" and grant.get("action") == "preview" and grant.get("authorization_state") == authorization_state for grant in matches[0].get("authorizations", [])):
        raise SuiteError("approval history does not match the current authorization state")
    return {"state": "approved", "authorization_state": authorization_state, "request_id": request_id}



def wait_for_grant_expiry(expires_at: datetime, deadline: float) -> None:
    while datetime.now(timezone.utc) <= expires_at + timedelta(seconds=1):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise SuiteError("grant did not expire before the Online deadline")
        time.sleep(min(1, remaining))


def wait_for_current_grant(applicant: GatewayClient, target_identity: str,
                           assessment_id: str, exemption_id: str | None, deadline: float,
                           owner: str = "manager", action: str = "preview") -> str:
    while time.monotonic() < deadline:
        matches = [target for target in access_targets(applicant, target_identity, owner, action)
                   if target.get("assessment_id") == assessment_id]
        if (len(matches) == 1 and bool(matches[0].get("active_exemption_id")) and (exemption_id is None or matches[0].get("active_exemption_id") == exemption_id)
                and matches[0].get("projection_acknowledged") is True):
            return matches[0]["active_exemption_id"]
        time.sleep(1)
    raise SuiteError("outage grant projection was not acknowledged before the Online deadline")


def exercise_security_unavailability(applicant: GatewayClient, approver: GatewayClient,
                                     tenant_id: int, locator: str, target_identity: str,
                                     assessment_id: str, identities: list[dict[str, object]],
                                     deadline: float, engine_id: int) -> dict[str, object]:
    repository = Path(__file__).resolve().parents[2]
    SUPPORT.require_hosted_security_lifecycle(repository)
    security_before = SUPPORT.ready_process("security")
    manager_before = SUPPORT.ready_process("manager")
    develop_before = SUPPORT.ready_process("develop")
    request_id, exemption_id, expires_at = approve_access_request(applicant, approver, assessment_id, 90, (("manager", "preview"), ("develop", "query")))
    wait_for_manager_values(applicant, locator, RAW_VALUES, "before-outage", deadline)
    wait_for_current_grant(applicant, target_identity, assessment_id, exemption_id, deadline)
    develop_grant = wait_for_current_grant(applicant, target_identity, assessment_id, None, deadline, "develop", "query")
    develop_execution = DEVELOP.develop_execution(applicant, engine_id, locator, deadline, "SELECT id, phone FROM addp_online_security.exemption_source ORDER BY id", "sql")
    develop_history_evidence(develop_execution, RAW_VALUES)
    refusals: list[dict[str, str]] = []
    previews: dict[str, object] = {}
    try:
        SUPPORT.run_security_lifecycle(repository, "stop", deadline)
        for phase, consumer, expected in (
            ("authorized_applicant", applicant, RAW_VALUES),
            ("protected_other_subject", approver, MASKED_VALUES),
            ("expired_applicant", applicant, MASKED_VALUES),
            ("expired_other_subject", approver, MASKED_VALUES),
        ):
            active = phase in {"authorized_applicant", "protected_other_subject"}
            if phase == "expired_applicant":
                wait_for_grant_expiry(expires_at, deadline)
            if time.monotonic() >= deadline:
                raise SuiteError("Security outage previews exceeded the Online deadline")
            if active and datetime.now(timezone.utc) >= expires_at:
                raise SuiteError("outage grant expired before its active preview was verified")
            refusals.append({"phase": phase, "boundary": "before", "observed_at": SUPPORT.security_listener_refused()})
            # One real request: retries could hide an incorrect protected output.
            previews[phase] = manager_evidence(consumer, locator, expected, phase)
            if phase in {"authorized_applicant", "expired_applicant"}:
                history = applicant.request("GET", f"/api/v1/develop/executions/{urllib.parse.quote(develop_execution['execution_id'])}", (200,)).payload
                develop_history_evidence(history, expected)
            refusals.append({"phase": phase, "boundary": "after", "observed_at": SUPPORT.security_listener_refused()})
            if active and datetime.now(timezone.utc) >= expires_at:
                raise SuiteError("outage grant expired during its active preview")
            if SUPPORT.ready_process("manager") != manager_before:
                raise SuiteError("Manager process changed during the Security outage")
            if SUPPORT.ready_process("develop") != develop_before:
                raise SuiteError("Develop process changed during the Security outage")
    finally:
        # A partially failed stop still needs recovery; outer cleanup remains mandatory.
        SUPPORT.run_security_lifecycle(repository, "start", deadline)
    security_after = SUPPORT.ready_process("security")
    SUPPORT.verify_restarted_process("security", security_before, security_after)
    if SUPPORT.ready_process("manager") != manager_before:
        raise SuiteError("Manager process changed during Security restoration")
    if SUPPORT.ready_process("develop") != develop_before:
        raise SuiteError("Develop process changed during Security restoration")
    after_identities = [validate_user_identity(applicant, tenant_id, APPLICANT_PERMISSIONS, "applicant"),
                        validate_user_identity(approver, tenant_id, APPROVER_PERMISSIONS, "approver")]
    if after_identities != identities:
        raise SuiteError("Security outage changed the two ordinary Users or Tenant")
    history = verify_approval_history(approver, request_id, "expired")
    return {"request_id": request_id, "exemption_id": exemption_id, "develop_exemption_id": develop_grant,
            "develop_history_while_unavailable_verified": True, "develop_process_unchanged": True,
            "develop_execution_id": develop_execution["execution_id"],
            "authorization_seconds": 90, "expires_at": expires_at.isoformat(),
            "connection_refusals": refusals, **previews,
            "manager_process_unchanged": True, "manager_process": manager_before,
            "security_process": {"before": security_before, "after": security_after},
            "same_users_verified": True, "expired_approval_history": history}


def develop_history_evidence(execution: object, expected: Mapping[str, object]) -> dict[str, object]:
    item = _object(execution, "Develop history")
    if item.get("status") != "success" or "protected_query_history" in json.dumps(item):
        raise SuiteError("Develop history did not return a safe successful result")
    result = _object(_object(item.get("metadata"), "Develop metadata").get("result"), "Develop result")
    if result.get("columns") != ["id", "phone"]:
        raise SuiteError("Develop history returned unexpected output columns")
    rows = _array(_object(result.get("summary"), "Develop summary").get("preview_rows"), "Develop rows")
    actual = {str(row.get("id")): row.get("phone") for row in rows if isinstance(row, dict)}
    if len(rows) != len(expected) or actual != expected:
        raise SuiteError("Develop current-user protected output mismatch")
    return {"records": len(rows), "private_history_excluded": True}


def validate_develop_scope_report(payload: object) -> dict[str, object]:
    report = _object(payload, "Develop plaintext report")
    expected = {"manager_only_does_not_authorize_develop": True, "explicit_two_outlet_approval": True,
                "authorized_query_verified": True, "other_user_masked": True,
                "same_history_masked_at_expiry": True, "same_history_masked_at_revocation": True,
                "manager_grant_independent": True, "private_history_excluded": True, "records": 3}
    if set(report) != set(expected) | {"authorized_execution_id", "revoked_execution_id", "request_ids", "exemption_ids"}:
        raise SuiteError("Develop plaintext report fields are incomplete or unexpected")
    if any(type(report.get(key)) is not type(value) or report.get(key) != value for key, value in expected.items()):
        raise SuiteError("Develop plaintext evidence is incomplete")
    ids = [report["authorized_execution_id"], report["revoked_execution_id"]]
    for key, count in (("request_ids", 2), ("exemption_ids", 2)):
        if not isinstance(report[key], list) or len(report[key]) != count:
            raise SuiteError("Develop plaintext audit identifiers are incomplete")
        ids.extend(report[key])
    try:
        if len(set(ids)) != len(ids) or any(not isinstance(value, str) or str(uuid.UUID(value)) != value or uuid.UUID(value).int == 0 for value in ids):
            raise ValueError("invalid identifiers")
    except (ValueError, TypeError, AttributeError):
        raise SuiteError("Develop plaintext evidence has invalid or reused identifiers")
    return report


def exercise_develop_scopes(applicant: GatewayClient, approver: GatewayClient, engine_id: int,
                            locator: str, identity: str, assessment_id: str, deadline: float) -> dict[str, object]:
    scopes = (("manager", "preview"), ("develop", "query"))
    query = "SELECT id, phone FROM addp_online_security.exemption_source ORDER BY id"
    grants: list[str] = []
    requests: list[str] = []

    def execute(client: GatewayClient, expected: Mapping[str, object]) -> dict[str, object]:
        execution = DEVELOP.develop_execution(client, engine_id, locator, deadline, query, "sql")
        develop_history_evidence(execution, expected)
        return execution

    def history(execution_id: str, expected: Mapping[str, object]) -> dict[str, object]:
        execution = applicant.request("GET", f"/api/v1/develop/executions/{urllib.parse.quote(execution_id)}", (200,)).payload
        develop_history_evidence(execution, expected)
        return _object(execution, "Develop history")

    def approve(seconds: int) -> tuple[str, datetime]:
        request_id, manager_grant, expires = approve_access_request(applicant, approver, assessment_id, seconds, scopes)
        requests.append(request_id)
        detail = _object(approver.request("GET", f"/api/v1/security/protection-access-requests/{urllib.parse.quote(request_id)}", (200,)).payload, "multi-outlet approval")
        authorizations = _array(detail.get("authorizations"), "multi-outlet grants")
        if {(grant.get("consumer_owner"), grant.get("action")) for grant in authorizations} != set(scopes):
            raise SuiteError("multi-outlet approval lost its explicit scope")
        for grant in authorizations:
            grant_id = grant.get("exemption_id")
            if not isinstance(grant_id, str) or not grant_id:
                raise SuiteError("multi-outlet approval is missing a grant")
            grants.append(grant_id)
        while time.monotonic() < deadline:
            ready = True
            for owner, action in scopes:
                path = "/api/v1/security/protection-access-request-targets?" + urllib.parse.urlencode({"target_identity": identity, "consumer_owner": owner, "action": action})
                targets = _object(applicant.request("GET", path, (200,)).payload, "outlet targets").get("data", [])
                matches = [target for target in targets if target.get("assessment_id") == assessment_id]
                ready = ready and len(matches) == 1 and bool(matches[0].get("active_exemption_id")) and matches[0].get("projection_acknowledged") is True
            if ready:
                develop_grant = next(grant["exemption_id"] for grant in authorizations if grant["consumer_owner"] == "develop")
                return develop_grant, expires
            time.sleep(1)
        raise SuiteError("explicit outlet grants did not synchronize")

    try:
        baseline = execute(applicant, MASKED_VALUES)
        _, expires = approve(45)
        wait_for_manager_values(applicant, locator, RAW_VALUES, "multi-outlet", deadline)
        authorized = execute(applicant, RAW_VALUES)
        other = execute(approver, MASKED_VALUES)
        remaining = (expires - datetime.now(timezone.utc)).total_seconds()
        if remaining > 0:
            time.sleep(remaining + 1)
        history(authorized["execution_id"], MASKED_VALUES)
        execute(applicant, MASKED_VALUES)
        develop_grant, _ = approve(120)
        second = execute(applicant, RAW_VALUES)
        revoke_exemption(approver, develop_grant)
        while time.monotonic() < deadline:
            try:
                history(second["execution_id"], MASKED_VALUES)
                break
            except SuiteError:
                time.sleep(1)
        else:
            raise SuiteError("revoked Develop history remained plaintext")
        wait_for_manager_values(applicant, locator, RAW_VALUES, "Manager grant remains independent", deadline)
        return validate_develop_scope_report({"manager_only_does_not_authorize_develop": True, "explicit_two_outlet_approval": True,
                "authorized_query_verified": True, "other_user_masked": True,
                "same_history_masked_at_expiry": True, "same_history_masked_at_revocation": True,
                "manager_grant_independent": True, "private_history_excluded": True, "records": 3,
                "authorized_execution_id": authorized["execution_id"], "revoked_execution_id": second["execution_id"],
                "request_ids": requests.copy(), "exemption_ids": sorted(set(grants))})
    finally:
        errors = []
        for grant_id in set(grants):
            try:
                state = _object(approver.request("GET", f"/api/v1/security/protection-exemptions/{urllib.parse.quote(grant_id)}", (200,)).payload, "cleanup grant")
                if state.get("effective_state") == "active":
                    revoke_exemption(approver, grant_id)
            except Exception as error:
                errors.append(type(error).__name__)
        if errors:
            raise SuiteError("multi-outlet cleanup failed: " + ",".join(errors))


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
    approver.request("POST", "/api/v1/security/protection-access-requests", (403,), {
        "assessment_id": assessment_id, "scopes": [{"consumer_owner": "manager", "action": "preview"}],
        "requested_expires_at": (datetime.now(timezone.utc) + timedelta(seconds=authorization_seconds)).isoformat(),
        "rationale": "Hosted Online approver cannot request",
    })
    request_id, exemption_id, requested_expires_at = approve_access_request(
        applicant, approver, assessment_id, authorization_seconds)

    authorized = wait_for_manager_values(
        applicant, source_locator, RAW_VALUES, "authorized", deadline
    )
    manager_only_query = DEVELOP.develop_execution(applicant, engine_id, source_locator, deadline,
        "SELECT id, phone FROM addp_online_security.exemption_source ORDER BY id", "sql")
    develop_history_evidence(manager_only_query, MASKED_VALUES)
    other_subject = wait_for_manager_values(
        approver, source_locator, MASKED_VALUES, "other-subject", deadline
    )

    remaining = (requested_expires_at - datetime.now(timezone.utc)).total_seconds()
    if remaining > 0:
        time.sleep(remaining + 1)
    # No Security request or projection refresh between the wait and preview.
    # Manager must evaluate the embedded subject grant expiry locally.
    expired = wait_for_manager_values(
        applicant, source_locator, MASKED_VALUES, "expired", deadline
    )

    expired_history = verify_approval_history(approver, request_id, "expired")
    revoked_request_id, revoked_exemption_id, _ = approve_access_request(
        applicant, approver, assessment_id, 120)
    renewed = wait_for_manager_values(applicant, source_locator, RAW_VALUES, "second-approval", deadline)
    other_subject_again = wait_for_manager_values(approver, source_locator, MASKED_VALUES, "other-subject-second-approval", deadline)
    revoke_exemption(approver, revoked_exemption_id)
    revoked = wait_for_manager_values(applicant, source_locator, MASKED_VALUES, "revoked", deadline)
    revoked_history = verify_approval_history(approver, revoked_request_id, "revoked")

    outage = exercise_security_unavailability(applicant, approver, tenant_id, source_locator,
        target_identity, assessment_id, [applicant_identity, approver_identity], deadline, engine_id)
    develop_scopes = exercise_develop_scopes(applicant, approver, engine_id, source_locator, target_identity, assessment_id, deadline)

    return {
        "schema_version": "addp.security-plaintext-access-online/v5",
        "result": "passed",
        "identities": {
            "applicant": applicant_identity,
            "approver": approver_identity,
        },
        "fixture": {
            "engine_id": str(engine_id),
            "source_locator": source_locator,
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
            "applicant_approval_denied": True,
            "approver_request_denied": True,
            "baseline": baseline,
            "authorized_applicant": authorized,
            "protected_other_subject": other_subject,
            "expired_without_security_refresh": expired,
            "expired_approval_history": expired_history,
            "second_authorized_applicant": renewed,
            "second_protected_other_subject": other_subject_again,
            "revoked_applicant": revoked,
            "revoked_approval_history": revoked_history,
        },
        "security_unavailability": outage,
        "develop_plaintext": develop_scopes,
        "created_resources": 0,
        "deleted_resources": 0,
        "residual_resources": 0,
        "audit_retained": {"access_request_ids": [request_id, revoked_request_id, outage["request_id"]] + develop_scopes["request_ids"],
                           "exemption_ids": sorted({exemption_id, revoked_exemption_id, outage["exemption_id"], outage["develop_exemption_id"], *develop_scopes["exemption_ids"]})},
    }


def validate_plaintext_browser_report(payload: object, environment: Mapping[str, str],
                                      api_report: Mapping[str, object]) -> dict[str, object]:
    report = _object(payload, "plaintext browser report")
    expected = {
        "schema_version": "addp.security-plaintext-access-browser/v2", "result": "passed",
        "run_id": environment["ADDP_ONLINE_TEST_RUN_ID"],
        "tenant_id": environment["ADDP_ONLINE_TEST_TENANT_ID"],
        "applicant_id": api_report["identities"]["applicant"]["principal_id"],
        "approver_id": api_report["identities"]["approver"]["principal_id"],
        "records": 3, "isolated_browser_contexts": True, "same_users_verified": True,
        "request_from_manager_ui": True, "approval_from_security_ui": True, "multi_outlet_request_verified": True,
        "authorized_applicant_verified": True, "other_subject_protected": True,
        "expired_preview_protected": True, "expired_history_verified": True,
        "revocation_from_security_ui": True, "revoked_preview_protected": True,
        "revoked_history_verified": True, "null_preserved": True,
        "browser_warning_errors": 0, "failed_business_responses": 0, "anonymous_refresh_401": 2,
    }
    if set(report) != set(expected) | {"request_ids"}:
        raise SuiteError("plaintext browser report fields are incomplete or unexpected")
    for key, value in expected.items():
        if type(report.get(key)) is not type(value) or report.get(key) != value:
            raise SuiteError("plaintext browser report mismatch: " + key)
    ids = report.get("request_ids")
    if (not isinstance(ids, list) or len(ids) != 2 or
            not all(isinstance(value, str) and value for value in ids) or len(set(ids)) != 2 or
            set(ids) & set(api_report["audit_retained"]["access_request_ids"])):
        raise SuiteError("plaintext browser must create two distinct requests through the UI")
    return report


def run_plaintext_browser(repository: Path, environment: Mapping[str, str],
                          api_report: Mapping[str, object]) -> dict[str, object]:
    required = ("ADDP_ONLINE_ARTIFACT_DIR", "ADDP_ONLINE_TEST_RUN_ID", "ADDP_ONLINE_TEST_TENANT_ID",
                "ADDP_ONLINE_TEST_USER_USERNAME", "ADDP_ONLINE_TEST_USER_PASSWORD",
                "ADDP_ONLINE_TEST_APPROVER_USERNAME", "ADDP_ONLINE_TEST_APPROVER_PASSWORD",
                "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN", "ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN",
                "GATEWAY_URL", "CONSOLE_URL")
    missing = [key for key in required if not environment.get(key)]
    if missing:
        raise SuiteError("plaintext browser environment is missing: " + ", ".join(missing))
    artifact_dir = Path(environment["ADDP_ONLINE_ARTIFACT_DIR"])
    report_path = artifact_dir / "security-plaintext-access-browser.json"
    screenshots = [artifact_dir / ("security-plaintext-" + name + ".png")
                   for name in ("pending", "expired", "revoked")]
    for path in [report_path, *screenshots]:
        path.unlink(missing_ok=True)
    browser_environment = dict(environment,
        ADDP_ONLINE_SECURITY_PLAINTEXT_LOCATOR=api_report["fixture"]["source_locator"],
        ADDP_ONLINE_SECURITY_ASSESSMENT_ID=api_report["fixture"]["security_assessment_id"])
    result = subprocess.run(
        ["npm", "exec", "--", "playwright", "test", "--config=playwright.online.config.js",
         "e2e/online/security-plaintext-access.spec.js"],
        cwd=repository / "console/frontend", env=browser_environment, text=True, capture_output=True)
    if result.stdout:
        print(result.stdout, end="" if result.stdout.endswith("\n") else "\n")
    if result.stderr:
        print(result.stderr, end="" if result.stderr.endswith("\n") else "\n", file=sys.stderr)
    if result.returncode:
        raise SuiteError(f"plaintext Playwright exited with status {result.returncode}")
    if not report_path.is_file() or any(not path.is_file() or path.stat().st_size == 0 for path in screenshots):
        raise SuiteError("plaintext browser report or protected screenshots are missing")
    try:
        payload = json.loads(report_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as error:
        raise SuiteError("plaintext browser report is not valid JSON") from error
    return validate_plaintext_browser_report(payload, environment, api_report)


def initialize_hosted(tenant_id: int, engine_id: int, gateway_url: str) -> None:
    SUPPORT.require_hosted_initialization(os.environ)
    applicant = GatewayClient(gateway_url, required_environment("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"), 30)
    approver = GatewayClient(gateway_url, required_environment("ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN"), 30)
    applicant_identity = validate_user_identity(applicant, tenant_id, APPLICANT_PERMISSIONS, "applicant")
    approver_identity = validate_user_identity(approver, tenant_id, APPROVER_PERMISSIONS, "approver")
    if applicant_identity["principal_id"] == approver_identity["principal_id"]:
        raise SuiteError("applicant and approver must be two different Users")
    SUPPORT.initialize_fresh_governance(GatewayClient(gateway_url,
        required_environment("ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN"), 30), tenant_id, [{
            "code": "phone", "detector": "addp.detector.phone_metadata/v2", "protection": {
                "effect": "mask", "algorithm": "addp.mask.keep_prefix_suffix/v2",
                "parameters": {"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"},
                "allowed_algorithms": ["addp.mask.keep_prefix_suffix/v2"], "invalid_value_effect": "suppress",
            },
        }])
    wait_for_scan(applicant, engine_id, time.monotonic() + 180)
    authorizer = GatewayClient(gateway_url, required_environment("ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"), 30)
    try:
        for index, consumer in enumerate((applicant, approver)):
            REGISTRATION.initialize_exact_record_read_grants(authorizer, consumer, tenant_id,
                [(engine_id, "schema", "addp_online_security", "exemption_source")], initialize_approval=index == 0)
    except REGISTRATION.RegistrationError as error:
        raise SuiteError(str(error)) from error


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--initialize", action="store_true")
    arguments = parser.parse_args()
    SUPPORT.require_hosted_initialization(os.environ)
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
    if arguments.initialize:
        initialize_hosted(tenant_id, engine_id, gateway_url)
        print("Disposable plaintext governance and two exact table read Grants are ready")
        return 0
    approver = GatewayClient(
        gateway_url,
        required_environment("ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN"),
        min(timeout, 30),
    )
    report = run_scenario(applicant, approver, tenant_id, engine_id, timeout)
    report["browser"] = run_plaintext_browser(Path(__file__).resolve().parents[2], os.environ, report)
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SuiteError as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
