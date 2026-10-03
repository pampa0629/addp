"""Real nested Orchestrator executions and safe Monitor projections on Hosted T4."""

from __future__ import annotations

import json
import math
import os
import re
import signal
import subprocess
import sys
import time
from importlib import import_module
from pathlib import Path

API = import_module("scripts.utils.online-api")
FAULTS = import_module("scripts.test.orchestrator-execution-faults")
SuiteError = API.SuiteError
SUITE = "orchestrator-execution"
ORCH = "/api/v1/orchestrator"
META = "/api/v1/meta"
MONITOR = "/api/v1/monitor/executions/by-execution-id"
SOURCE = "addp-metric-online-disposable"
TERMINAL = {"success", "failed", "cancelled", "timeout"}
REQUIRED_PERMISSIONS = {
    "orchestrator.workflow.read", "orchestrator.workflow.create", "orchestrator.workflow.delete",
    "orchestrator.workflow.execute", "meta.scan_task.read", "meta.scan_task.create",
    "meta.scan_task.delete", "meta.scan_task.execute", "monitor.execution.read",
    "system.execution_authorization.create",
}
PRIVATE_KEYS = {
    "execution_config", "authorization_ref", "lease_owner", "lease_expires_at",
    "step_results", "outputs", "parameters", "result", "connection_info",
}
EVENT_FIELDS = {"id", "execution_id", "attempt", "occurred_at", "kind", "step_id", "counters"}
COUNTERS = {"progress", "batch_index", "batch_records", "records_read", "records_written",
            "bytes_read", "bytes_written", "items_scanned", "catalog_nodes_scanned", "fields_scanned"}


def require(condition, message):
    if not condition:
        raise SuiteError(message)


def assert_safe(value):
    if isinstance(value, dict):
        require(not PRIVATE_KEYS.intersection(value), "Monitor exposed private execution fields")
        for child in value.values():
            assert_safe(child)
    elif isinstance(value, list):
        for child in value:
            assert_safe(child)


def source_action(action):
    require(action in {"stop", "start"}, "invalid source lifecycle action")
    result = subprocess.run(["docker", "inspect", SOURCE], check=True, capture_output=True, text=True, timeout=15)
    containers = json.loads(result.stdout)
    require(isinstance(containers, list) and len(containers) == 1, "source container identity is invalid")
    labels = containers[0].get("Config", {}).get("Labels") or {}
    require(labels.get("com.addp.online-fixture") == "metric", "refusing an unowned source container")
    subprocess.run(["docker", action, SOURCE], check=True, capture_output=True, text=True, timeout=30)
    if action == "start":
        for _ in range(30):
            ready = subprocess.run(["docker", "exec", SOURCE, "pg_isready", "-U", "postgres", "-d", "metric_fixture"],
                                   capture_output=True, timeout=10)
            if ready.returncode == 0:
                return
            time.sleep(1)
        raise SuiteError("restored source did not become ready")


def wait_execution(client, execution_id, timeout=120):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        item = client.request("GET", f"{ORCH}/executions/{execution_id}", (200,)).payload
        require(item.get("execution_id") == execution_id, "execution identity changed")
        if item.get("status") in TERMINAL:
            return item
        require(item.get("status") in {"pending", "running"}, "unknown execution status")
        time.sleep(1)
    raise SuiteError("orchestration did not finish within the convergence deadline")


def step(name, provider, task_type, task_id, dependencies=()):
    return {"id": name, "name": name, "provider": provider, "task_type": task_type,
            "task_id": task_id, "parameters": {}, "depends_on": list(dependencies), "timeout": 120}


def tree_ids(node, parent=None):
    require(isinstance(node, dict) and node.get("truncated") is False, "unexpected execution tree truncation")
    item = node.get("execution", {})
    identity = item.get("execution_id")
    require(isinstance(identity, str) and identity, "execution tree identity is missing")
    require(item.get("parent_execution_id") == parent, "execution tree parent lineage mismatch")
    result = {identity: item}
    children = node.get("children")
    require(isinstance(children, list), "execution tree children are invalid")
    for child in children:
        descendants = tree_ids(child, identity)
        require(not result.keys() & descendants.keys(), "duplicate execution in tree")
        result.update(descendants)
    return result


def read_events(client, execution_id, terminal):
    cursor, items = 0, []
    for _ in range(11):
        page = client.request("GET", f"{MONITOR}/{execution_id}/events?after={cursor}&limit=100", (200,)).payload
        batch = page.get("items")
        require(isinstance(batch, list) and len(batch) <= 100 and page.get("retained_after"), "invalid event page")
        for item in batch:
            require(isinstance(item, dict) and set(item) <= EVENT_FIELDS, "event exposed an unbounded field")
            require(item.get("execution_id") == execution_id and item.get("attempt") == 1, "event execution identity mismatch")
            require(isinstance(item.get("id"), int) and item["id"] > cursor, "event cursor did not advance")
            require(item.get("kind") in {"started", "progress", "completed", "failed", "cancelled", "timeout", "truncated"},
                    "unknown event kind")
            counters = item.get("counters")
            require(isinstance(counters, dict) and set(counters) <= COUNTERS, "event counters are not closed")
            require(all(isinstance(n, int) and not isinstance(n, bool) and n >= 0 for n in counters.values()), "invalid counter")
            cursor = item["id"]
        items.extend(batch)
        require(isinstance(page.get("has_more"), bool) and page.get("next_cursor") == cursor, "invalid event continuation")
        if not page["has_more"]:
            require(items and items[0]["kind"] == "started" and items[-1]["kind"] == terminal, "missing lifecycle events")
            require(sum(x["kind"] in {"completed", "failed", "cancelled", "timeout"} for x in items) == 1,
                    "execution has duplicate terminal events")
            return items
        require(batch, "empty continued event page")
    raise SuiteError("event pagination exceeded the bounded attempt budget")


def assert_events(client, execution_id, terminal):
    return len(read_events(client, execution_id, terminal))


def history_snapshot(client, execution_id, visible):
    """Keep comparisons in memory only; never put Owner results in the evidence."""
    tree = client.request("GET", f"{MONITOR}/{execution_id}/tree", (200,)).payload
    assert_safe(tree)
    require(tree_ids(tree) == visible, "historical execution tree changed")
    details = {}
    for identity, item in visible.items():
        require(item.get("status") in TERMINAL, "historical child execution is still active")
        detail = client.request("GET", f"{MONITOR}/{identity}", (200,)).payload
        assert_safe(detail)
        require(detail == item, "historical execution detail differs from its tree")
        details[identity] = detail
    owner = client.request("GET", f"{ORCH}/executions/{execution_id}", (200,)).payload
    require(owner.get("execution_id") == execution_id and owner.get("status") == visible[execution_id]["status"],
            "historical Owner execution identity or status changed")
    terminal = {"success": "completed", "failed": "failed", "cancelled": "cancelled", "timeout": "timeout"}
    return {"tree": tree, "details": details,
            "events": read_events(client, execution_id, terminal[owner["status"]]),
            "owner_steps": owner.get("metadata", {}).get("step_results", {})}


def definition_present(client, path, present):
    identity = int(path.rsplit("/", 1)[1])
    if path.startswith(ORCH + "/orchestrations/"):
        payload = client.request("GET", path, (200,) if present else (404,)).payload
        if present:
            require(payload.get("id") == identity, "task definition identity changed")
    else:
        require(path.startswith(META + "/scan/tasks/"), "unexpected fixture definition owner")
        # Meta has no GET /scan/tasks/:id; a routing 404 cannot prove deletion.
        items = client.request("GET", f"{META}/scan/tasks", (200,), response_type=list).payload
        require(isinstance(items, list) and all(isinstance(item, dict) for item in items), "invalid scan task list")
        require(any(item.get("id") == identity for item in items) == present, "scan task definition presence mismatch")


def run_fault_case(client, launch, task_id, scan_id, case_id, mode, faults, report):
    faults.arm(case_id, scan_id, mode)
    try:
        identity, _ = launch(task_id, wait=False)
        if mode == "hold_status":
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                item = client.request("GET", f"{ORCH}/executions/{identity}", (200,)).payload
                recorded = item.get("metadata", {}).get("step_results", {}).get("probe", {})
                witness = faults.witness()
                if recorded.get("phase") == "waiting" and witness.get("child_execution_ids"):
                    require(item.get("status") == "running" and recorded.get("status") == "running"
                            and recorded.get("result", {}).get("execution_id") == witness["child_execution_ids"][0]
                            and witness.get("parent_execution_id") == identity and witness.get("posts") == 1
                            and not witness.get("error"), "crash precondition did not prove accepted child")
                    faults.crash()
                    faults.release()
                    faults.restart()
                    break
                require(item.get("status") not in TERMINAL, "crash fixture terminated before the waiting barrier")
                time.sleep(0.1)
            else:
                raise SuiteError("crash fixture did not reach the persisted waiting barrier")
        terminal = wait_execution(client, identity, timeout=180)
        code = "orchestrator.execution.lease_expired" if mode == "hold_status" else "orchestrator.execution.dispatch_uncertain"
        require(terminal.get("status") == "failed" and terminal.get("error_details", {}).get("code") == code
                and terminal.get("progress") == 0 and terminal.get("attempt") == 1, "fault parent did not converge truthfully")
        steps = terminal.get("metadata", {}).get("step_results", {})
        require(set(steps) == {"probe"}, "fault replayed or dispatched the dependent step")
        if mode == "hold_status":
            require(steps["probe"].get("status") == "running" and steps["probe"].get("phase") == "waiting",
                    "recovery replaced the last recorded step with an invented terminal state")
        else:
            result = steps["probe"].get("result")
            require(result is None or isinstance(result, dict), "lost response step result is invalid")
            require(result is None or not result.get("execution_id"), "lost response invented a known child identity")
        # Check beyond one supervisor pass; the real child must remain visible and finish normally.
        deadline = time.monotonic() + 120
        stable_after = time.monotonic() + 5
        while time.monotonic() < deadline:
            projection = client.request("GET", f"{MONITOR}/{identity}/tree", (200,)).payload
            assert_safe(projection)
            visible, witness = tree_ids(projection), faults.witness()
            children = [x for x in visible if x != identity]
            require(witness.get("posts") == 1 and witness.get("parent_execution_id") == identity
                    and not witness.get("error") and len(witness.get("child_execution_ids", [])) == 1,
                    "fault witness did not prove one accepted request")
            child = witness["child_execution_ids"][0]
            require(children == [child] and visible[child].get("module") == "meta", "fault tree lost or duplicated the real child")
            safe_steps = visible[identity].get("steps", [])
            require(len(safe_steps) == 1 and safe_steps[0].get("id") == "probe"
                    and visible[identity].get("status") == "failed" and visible[identity].get("progress") == 0
                    and visible[identity].get("error_details", {}).get("code") == code, "fault diagnostics lost its cause or step")
            if mode == "hold_status":
                require(safe_steps[0].get("status") == "running" and safe_steps[0].get("phase") == "waiting",
                        "Monitor misrepresented the historical waiting step")
            if visible[child].get("status") == "success" and time.monotonic() >= stable_after:
                break
            require(visible[child].get("status") in {"pending", "running", "success"}, "parent failure stopped or corrupted its real child")
            time.sleep(1)
        else:
            raise SuiteError("accepted child did not finish normally after its parent failed")
        event_count = assert_events(client, identity, "failed")
        report.setdefault("faults", []).append({"kind": case_id, "parent_execution_id": identity,
            "child_execution_id": child, "posts": 1, "error_code": code, "event_count": event_count})
        report["checks"].extend([case_id, case_id + "_no_replay"])
    finally:
        faults.release()


OWNER_READER_ROLE = "online.external_online_parent_reader"
IAM_ASSIGNMENTS = "/api/v1/system/tenant/role_assignments"
AUTH_CONTEXT = "/api/v1/system/auth/context"


def decimal_id(value):
    require(isinstance(value, str) and re.fullmatch(r"[1-9][0-9]*", value), "invalid IAM fixture identity")
    return value


def user_binding(payload, tenant):
    principal, context = payload.get("principal", {}), payload.get("context", {})
    require(isinstance(principal, dict) and isinstance(context, dict)
            and principal.get("type") == "user" and context.get("type") == "tenant"
            and context.get("tenant_id") == str(tenant), "IAM fixture User/Tenant binding changed")
    return decimal_id(principal.get("id")), decimal_id(context.get("tenant_membership_id"))


def role_permissions(payload):
    authorization = payload.get("authorization")
    require(isinstance(authorization, dict), "IAM fixture authorization must be an object")
    assignments = authorization.get("role_assignments")
    require(isinstance(assignments, list), "IAM fixture role assignments must be an array")
    roles, permissions = set(), set()
    for assignment in assignments:
        require(isinstance(assignment, dict) and isinstance(assignment.get("role_key"), str)
                and isinstance(assignment.get("permissions"), list)
                and all(isinstance(p, str) for p in assignment["permissions"]), "invalid IAM fixture role assignment")
        roles.add(assignment["role_key"])
        permissions.update(assignment["permissions"])
    return roles, permissions


def invisible_projection(client, execution_id, status):
    for suffix in ("", "/tree", "/events"):
        payload = client.request("GET", f"{MONITOR}/{execution_id}{suffix}", (status,)).payload
        require(set(payload) <= {"error", "error_code"}, "invisible execution returned diagnostic data")


class PermissionCases:
    """IAM controller is used only for revocation; every diagnostic read uses a normal User."""

    def __init__(self, reader, administrator, login, tenant, assignment_id, username, password):
        self.reader, self.administrator, self.login, self.tenant = reader, administrator, login, tenant
        self.assignment_id = decimal_id(assignment_id)
        self.username, self.password = username, password
        require(username == "external-online-parent-reader" and bool(password), "dedicated reader login required")
        API.validate_user_identity(reader, tenant, {"monitor.execution.read", "orchestrator.workflow.read"})
        original = reader.request("GET", AUTH_CONTEXT, (200,)).payload
        self.binding = user_binding(original, tenant)
        _, permissions = role_permissions(original)
        require("meta.scan_task.read" not in permissions, "parent reader must not read Meta executions")
        admin = administrator.request("GET", AUTH_CONTEXT, (200,)).payload
        user_binding(admin, tenant)
        require(admin.get("token", {}).get("type") == "first_party_access_token", "dedicated IAM controller token required")
        roles, permissions = role_permissions(admin)
        require("tenant.administrator" in roles and not (roles & (API.FORBIDDEN_ADMIN_ROLES - {"tenant.administrator"}))
                and {"iam.tenant_role_assignment.read", "iam.tenant_role_assignment.revoke"} <= permissions,
                "IAM controller must be the fixture Tenant administrator")
        require(user_binding(admin, tenant)[0] != self.binding[0], "IAM controller and reader must be different Users")
        target = self.iam_request("GET", (200,)).payload
        self.assert_assignment(target, "active")

    def iam_request(self, method, expected, body=None):
        path = f"{IAM_ASSIGNMENTS}/{self.assignment_id}" + ("/revoke" if method == "POST" else "")
        try:
            return self.administrator.request(method, path, expected, body)
        except SuiteError:
            raise SuiteError("fixture IAM assignment request failed") from None

    def assert_assignment(self, target, status):
        require(target.get("id") == self.assignment_id and target.get("principal_type") == "user"
                and (target.get("principal_id"), target.get("membership_id")) == self.binding
                and target.get("username") == self.username and target.get("role_key") == OWNER_READER_ROLE
                and target.get("scope_type") == "tenant" and target.get("status") == status,
                "revocation target does not match the dedicated reader Owner assignment")

    def parent_read_child_hidden(self, execution_id, visible, report, check="parent_read_child_hidden"):
        detail = self.reader.request("GET", f"{MONITOR}/{execution_id}", (200,)).payload
        assert_safe(detail)
        require(detail.get("execution_id") == execution_id, "readable parent detail identity changed")
        projected = self.reader.request("GET", f"{MONITOR}/{execution_id}/tree", (200,)).payload
        assert_safe(projected)
        allowed = {i for i, item in visible.items() if item["module"] == "orchestrator"}
        filtered = tree_ids(projected)
        require(len(allowed) == 2 and set(filtered) == allowed
                and all(item.get("module") == "orchestrator" and item.get("status") == "success"
                        for item in filtered.values()), "parent read leaked or lost child owners")
        assert_events(self.reader, execution_id, "completed")
        for identity, item in visible.items():
            if item["module"] == "meta":
                invisible_projection(self.reader, identity, 404)
        report["checks"].append(check)

    def revoke_owner_read(self, execution_id, report, checkpoint):
        # A missing response must never cause a second revoke command or a clean report.
        report["uncertain_mutation"] = True
        checkpoint()
        target = self.iam_request("POST", (200,),
            {"reason": "Disposable Online Owner execution-read revocation acceptance"}).payload
        self.assert_assignment(target, "revoked")
        report["uncertain_mutation"] = False
        checkpoint()
        invisible_projection(self.reader, execution_id, 401)
        report["checks"].append("revoked_token_invalid")
        report["uncertain_mutation"] = True
        checkpoint()
        issued = self.login.request("POST", "/api/v1/system/login", (200,),
                                    {"username": self.username, "password": self.password}).payload
        token = (issued.get("session") or {}).get("access_token")
        require(issued.get("next_action") == "session_issued" and isinstance(token, str) and bool(token)
                and token != self.reader.token, "formal reader re-login did not issue a fresh session")
        fresh = API.GatewayClient(self.reader.base_url, token, self.reader.timeout)
        API.validate_user_identity(fresh, self.tenant, {"monitor.execution.read"})
        context = fresh.request("GET", AUTH_CONTEXT, (200,)).payload
        require(user_binding(context, self.tenant) == self.binding, "re-login changed the reader identity")
        _, permissions = role_permissions(context)
        require("orchestrator.workflow.read" not in permissions and "meta.scan_task.read" not in permissions,
                "fresh reader retained revoked Owner access")
        report["uncertain_mutation"] = False
        checkpoint()
        invisible_projection(fresh, execution_id, 404)
        fresh.request("GET", f"{ORCH}/executions/{execution_id}", (403,))
        report["checks"].append("revoked_owner_read_invisible")


def owner_unavailable(client, execution_id, visible, scan, faults, report):
    require(faults is not None, "Owner unavailable acceptance requires the real external proxy")
    try:
        faults.arm("owner_unavailable", scan, "drop_scope")
        for suffix in ("", "/tree", "/events"):
            payload = client.request("GET", f"{MONITOR}/{execution_id}{suffix}", (503,)).payload
            require(set(payload) == {"error", "error_code"}
                    and payload["error_code"] == "execution_owner_unavailable" and isinstance(payload["error"], str),
                    "unavailable Owner exposed diagnostic data or the wrong failure category")
        witness = faults.witness()
        require(witness.get("scope_drops", 0) >= 3 and witness.get("posts") == 0
                and witness.get("child_execution_ids") == [] and not witness.get("error"),
                "real Owner scope transport failure was not proven")
        report["checks"].append("owner_unavailable_fail_closed")
    finally:
        faults.release()
    recovered = client.request("GET", f"{MONITOR}/{execution_id}/tree", (200,)).payload
    assert_safe(recovered)
    require(tree_ids(recovered) == visible, "Owner recovery changed the full authorized execution tree")
    report["checks"].append("owner_recovery_restores_access")


def run_suite(client, denied, foreign, tenant, run_id, engine_id, report, control=source_action, checkpoint=lambda: None, faults=None, permissions=None):
    report.update(schema_version="addp.online-suite/v1", suite=SUITE, run_id=run_id,
                  tenant_id=str(tenant), result="failed", checks=[], resources=[], executions=[],
                  cleanup="not_started", history_cleanup="pending_deployment_destruction", uncertain_mutation=False)
    resources, executions = report["resources"], report["executions"]
    source_stopped = False

    def create(path, body):
        # Persist uncertainty before POST. Never retry a mutation whose response was lost.
        report["uncertain_mutation"] = True
        checkpoint()
        item = client.request("POST", path, (201,), body).payload
        identity = API.require_positive_int(item, "id")
        API.assert_tenant(item, tenant, "fixture definition")
        resources.append(f"{path}/{identity}")
        report["uncertain_mutation"] = False
        checkpoint()
        return identity

    def launch(task_id, wait=True):
        report["uncertain_mutation"] = True
        checkpoint()
        response = client.request("POST", f"{ORCH}/orchestrations/{task_id}/execute", (202,)).payload
        identity = response.get("execution_id")
        require(isinstance(identity, str) and re.fullmatch(r"[0-9a-f-]{36}", identity), "missing execution identity")
        require(response.get("status") == "pending", "manual trigger did not admit pending execution")
        executions.append(identity)
        report["uncertain_mutation"] = False
        checkpoint()
        return identity, wait_execution(client, identity) if wait else response

    try:
        scan = create(f"{META}/scan/tasks", {"name": f"{run_id}-scan", "engine_id": engine_id,
                      "catalog_paths": ["public"], "scan_depth": "basic", "force": True, "enabled": False})
        nested = create(f"{ORCH}/orchestrations", {"name": f"{run_id}-nested", "enabled": False,
                        "steps": [step("scan", "meta", "scan", scan)]})
        root = create(f"{ORCH}/orchestrations", {"name": f"{run_id}-root", "enabled": False,
                      "steps": [step("nested", "orchestrator", "orchestration", nested),
                                step("after", "meta", "scan", scan, ["nested"])]})
        success_id, success = launch(root)
        require(success.get("status") == "success" and success.get("progress") == 100, "nested DAG did not succeed")
        successful_steps = success.get("metadata", {}).get("step_results", {})
        require(set(successful_steps) == {"nested", "after"}
                and all(s.get("status") == "success" for s in successful_steps.values()), "successful step evidence mismatch")
        projection = client.request("GET", f"{MONITOR}/{success_id}/tree", (200,)).payload
        assert_safe(projection)
        visible = tree_ids(projection)
        require(len(visible) == 4 and all(i.get("status") == "success" for i in visible.values()), "nested tree is incomplete")
        require(sorted(i["module"] for i in visible.values()) == ["meta", "meta", "orchestrator", "orchestrator"],
                "unexpected nested task owners")
        root_steps = visible[success_id].get("steps", [])
        require({s.get("id") for s in root_steps} == {"nested", "after"}
                and all(s.get("phase") == "terminal" for s in root_steps), "Monitor lost safe terminal steps")
        report["success_event_count"] = assert_events(client, success_id, "completed")
        report["checks"].extend(["nested_serial_dag", "successful_progress", "parent_child_lineage", "safe_steps", "safe_events"])
        for reader in (denied, foreign):
            for suffix in ("", "/tree", "/events"):
                reader.request("GET", f"{MONITOR}/{success_id}{suffix}", (404,))
        foreign.request("GET", f"{ORCH}/executions/{success_id}", (404,))
        denied.request("GET", f"{ORCH}/executions/{success_id}", (403,))
        report["checks"].extend(["cross_tenant_invisible", "monitor_without_owner_invisible"])
        # Only the disposable business source is interrupted; platform PG stays alive.
        source_stopped = True
        control("stop")
        failure_id, failure = launch(root)
        require(failure.get("status") == "failed" and failure.get("progress") == 0, "failure progress must remain truthful")
        require(failure.get("error_details", {}).get("code") == "orchestrator.execution.child_failed", "unexpected failure reason")
        failed_steps = failure.get("metadata", {}).get("step_results", {})
        require(set(failed_steps) == {"nested"} and failed_steps["nested"].get("status") == "failed", "dependent step was executed")
        failure_tree = client.request("GET", f"{MONITOR}/{failure_id}/tree", (200,)).payload
        assert_safe(failure_tree)
        failed_visible = tree_ids(failure_tree)
        require(len(failed_visible) == 3 and all(i.get("status") == "failed" for i in failed_visible.values()),
                "failed nested lineage is incomplete or a dependent child was created")
        safe_failure = failed_visible[failure_id]
        require(safe_failure.get("error_details", {}).get("code") == "orchestrator.execution.child_failed",
                "Monitor lost the stable failure code")
        require(len(safe_failure.get("steps", [])) == 1
                and safe_failure["steps"][0].get("phase") == "terminal"
                and safe_failure["steps"][0].get("error_code"), "Monitor lost the step failure category")
        report["failure_event_count"] = assert_events(client, failure_id, "failed")
        report["checks"].extend(["real_source_failure", "dependency_blocked", "failure_reason", "failure_progress"])
        if faults is not None:
            control("start")
            source_stopped = False
            fault_root = create(f"{ORCH}/orchestrations", {"name": f"{run_id}-faults", "enabled": False,
                "steps": [step("probe", "meta", "scan", scan), step("after", "meta", "scan", scan, ["probe"])]})
            for case_id, mode in (("response_lost", "lose_response"), ("process_crash", "hold_status")):
                run_fault_case(client, launch, fault_root, scan, case_id, mode, faults, report)
                checkpoint()
        if permissions is not None:
            permissions.parent_read_child_hidden(success_id, visible, report)
            owner_unavailable(client, success_id, visible, scan, faults, report)

        # Deletion is a business acceptance phase, with exactly one mutation route.
        # Restore the owned source and prove every known child terminal beforehand.
        if source_stopped:
            control("start")
            source_stopped = False
        snapshots = {identity: history_snapshot(client, identity, family)
                     for identity, family in ((success_id, visible), (failure_id, failed_visible))}
        require(snapshots[success_id]["owner_steps"] == successful_steps
                and snapshots[failure_id]["owner_steps"] == failed_steps, "Owner lost the recorded terminal steps")
        for identity in executions:
            require(client.request("GET", f"{ORCH}/executions/{identity}", (200,)).payload.get("status") in TERMINAL,
                    "execution is still active before definition deletion")
        for path in resources:
            definition_present(client, path, True)
        report["cleanup"] = "deleting_definitions"
        report["deleted_definition_count"] = 0
        for path in reversed(resources):
            report["uncertain_mutation"] = True
            checkpoint()
            client.request("DELETE", path, (200,))
            report["uncertain_mutation"] = False
            definition_present(client, path, False)
            report["deleted_definition_count"] += 1
            checkpoint()
        report["cleanup"] = "definitions_deleted"
        checkpoint()
        for identity, family, check in ((success_id, visible, "deleted_success_history_retained"),
                                        (failure_id, failed_visible, "deleted_failure_history_retained")):
            require(history_snapshot(client, identity, family) == snapshots[identity],
                    "definition deletion changed execution history")
            report["checks"].append(check)
            for reader in (denied, foreign):
                for child_id in family:
                    invisible_projection(reader, child_id, 404)
            foreign.request("GET", f"{ORCH}/executions/{identity}", (404,))
            denied.request("GET", f"{ORCH}/executions/{identity}", (403,))
        report["checks"].append("deleted_history_access_isolated")
        if permissions is not None:
            permissions.parent_read_child_hidden(success_id, visible, report, "deleted_parent_read_child_hidden")
            permissions.revoke_owner_read(success_id, report, checkpoint)
            checkpoint()
        report["result"] = "passed"
    finally:
        failures = []
        if source_stopped:
            try:
                control("start")
            except Exception:
                failures.append("restore_disposable_source")
        # An unfinished execution/unknown POST must be resolved by deployment destruction.
        if report["uncertain_mutation"]:
            failures.append("unknown_mutation_outcome")
        for identity in executions:
            try:
                require(client.request("GET", f"{ORCH}/executions/{identity}", (200,)).payload.get("status") in TERMINAL,
                        "execution is still active")
            except Exception:
                failures.append("unfinished_execution")
        if report["cleanup"] == "deleting_definitions":
            failures.append("definition_deletion_incomplete")
        report["cleanup_failures"] = failures
        report["cleanup"] = ("failed" if failures else "definitions_deleted" if report["cleanup"] == "definitions_deleted"
                             else "deferred_to_deployment_destruction")
        if failures:
            report["result"] = "failed"
        checkpoint()
        require(not failures, "cleanup failed; disposable deployment destruction is required")
    return report


def main():
    report, evidence = {}, None

    def checkpoint():
        if evidence is not None:
            temporary = evidence.with_suffix(".tmp")
            temporary.write_text(json.dumps(report, sort_keys=True) + "\n")
            temporary.replace(evidence)

    def interrupted(signum, frame):
        raise SuiteError("Orchestrator Online suite interrupted")

    try:
        require(os.environ.get("ADDP_ONLINE_TEST") == "1" and os.environ.get("ADDP_ONLINE_HOST") == "1"
                and os.environ.get("GITHUB_ACTIONS") == "true" and os.environ.get("RUNNER_OS") == "Linux"
                and os.environ.get("ADDP_ONLINE_HOSTED") == "1" and os.environ.get("POSTGRES_DB") == "addp_online",
                "use the standard disposable Hosted Online gate")
        tenant, engine = int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"]), int(os.environ["ADDP_ONLINE_CONSUMER_ENGINE_ID"])
        require(tenant > 1 and engine > 0, "dedicated Tenant and Engine required")
        run_id = os.environ["ADDP_ONLINE_TEST_RUN_ID"]
        require(re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,63}", run_id), "invalid Run ID")
        directory = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
        require(directory.is_absolute() and not directory.resolve().is_relative_to(Path(__file__).resolve().parents[2]),
                "evidence must be outside checkout")
        directory.mkdir(parents=True, exist_ok=True)
        evidence = directory / (SUITE + ".json")
        timeout = float(os.environ.get("ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS", "10"))
        require(math.isfinite(timeout) and 0 < timeout <= 30, "invalid HTTP timeout")
        token = os.environ["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"]
        require(bool(token), "User token required")
        report["identity"] = API.validate_user_identity(API.GatewayClient(os.environ["SYSTEM_URL"], token, timeout),
                                                       tenant, REQUIRED_PERMISSIONS)
        denied_token = os.environ["ADDP_ONLINE_READ_USER_ACCESS_TOKEN"]
        foreign_token = os.environ["ADDP_ONLINE_FOREIGN_USER_ACCESS_TOKEN"]
        require(bool(denied_token) and bool(foreign_token) and len({token, denied_token, foreign_token}) == 3,
                "three distinct User credentials are required")
        denied_system = API.GatewayClient(os.environ["SYSTEM_URL"], denied_token, timeout)
        report["denied_identity"] = API.validate_user_identity(denied_system, tenant, {"monitor.execution.read"})
        foreign_system = API.GatewayClient(os.environ["SYSTEM_URL"], foreign_token, timeout)
        foreign_context = foreign_system.request("GET", "/api/v1/system/auth/context", (200,)).payload.get("context", {})
        require(isinstance(foreign_context, dict), "foreign reader Context is invalid")
        foreign_tenant = int(foreign_context.get("tenant_id", "0"))
        require(foreign_tenant > 1 and foreign_tenant != tenant, "foreign reader must belong to a different nondefault Tenant")
        report["foreign_identity"] = API.validate_user_identity(foreign_system, foreign_tenant,
            {"monitor.execution.read", "orchestrator.workflow.read", "meta.scan_task.read"})
        parent_token = os.environ["ADDP_ONLINE_PARENT_USER_ACCESS_TOKEN"]
        admin_token = os.environ["ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN"]
        require(bool(parent_token) and bool(admin_token)
                and len({token, denied_token, foreign_token, parent_token, admin_token}) == 5,
                "five distinct fixture User credentials are required")
        gateway = os.environ["GATEWAY_URL"]
        permissions = PermissionCases(API.GatewayClient(gateway, parent_token, timeout),
            API.GatewayClient(gateway, admin_token, timeout), API.GatewayClient(gateway, "", timeout),
            tenant, os.environ["ADDP_ONLINE_PARENT_ASSIGNMENT_ID"],
            os.environ["ADDP_ONLINE_PARENT_USER_USERNAME"], os.environ["ADDP_ONLINE_PARENT_USER_PASSWORD"])
        for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGALRM):
            signal.signal(signum, interrupted)
        signal.alarm(600)
        run_suite(API.GatewayClient(os.environ["GATEWAY_URL"], token, timeout),
                  API.GatewayClient(os.environ["GATEWAY_URL"], denied_token, timeout),
                  API.GatewayClient(os.environ["GATEWAY_URL"], foreign_token, timeout),
                  tenant, run_id, engine, report, checkpoint=checkpoint, faults=FAULTS.HostedFaults(), permissions=permissions)
    except (KeyError, ValueError, SuiteError, subprocess.SubprocessError) as error:
        report["result"] = "failed"
        # Raw transport/process exceptions can contain secrets; evidence stores no response body.
        print("Orchestrator Online suite failed: " + (str(error) if isinstance(error, SuiteError) else "fixture/lifecycle error"),
              file=sys.stderr)
        return 1
    finally:
        signal.alarm(0)
        checkpoint()
    print(json.dumps(report, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
