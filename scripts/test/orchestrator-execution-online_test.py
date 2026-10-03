import copy
import importlib
import subprocess
import unittest
from unittest.mock import patch

ONLINE = importlib.import_module("scripts.test.orchestrator-execution-online")
API = ONLINE.API


def execution_id(n):
    return f"00000000-0000-0000-0000-{n:012d}"


class FakeOwner:
    def __init__(self, fault=None):
        self.calls = []
        self.launches = 0
        self.definitions = 10
        self.fault = fault

    def tree(self, failed):
        root, inner, scan = [execution_id(n) for n in ((5, 6, 7) if failed else (1, 2, 3))]
        status = "failed" if failed else "success"

        def node(identity, module, parent=None, children=()):
            item = {"execution_id": identity, "module": module, "status": status, "progress": 0 if failed else 100,
                    "metadata": {}, "steps": []}
            if parent:
                item["parent_execution_id"] = parent
            return {"execution": item, "children": list(children), "truncated": False}

        leaf = node(scan, "meta", inner)
        middle = node(inner, "orchestrator", root, [leaf])
        children = [middle] if failed else [middle, node(execution_id(4), "meta", root)]
        result = node(root, "orchestrator", children=children)
        result["execution"]["steps"] = [{"id": "nested", "phase": "terminal", "error_code": "task_failed"}]
        if not failed:
            result["execution"]["steps"].append({"id": "after", "phase": "terminal"})
        else:
            result["execution"]["error_details"] = {"code": "orchestrator.execution.child_failed"}
        if self.fault == "private":
            leaf["execution"]["execution_config"] = {"password": "never-print"}
        if self.fault == "truncated":
            result["truncated"] = True
        if failed and self.fault == "dependent":
            children.append(node(execution_id(8), "meta", root))
        return result

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, body))
        if method == "POST" and path.endswith("/execute"):
            self.launches += 1
            return API.Response(202, {"execution_id": execution_id(1 if self.launches == 1 else 5), "status": "pending"})
        if method == "POST":
            if self.fault == "lost_create":
                raise API.SuiteError("create response unavailable")
            self.definitions += 1
            return API.Response(201, {"id": self.definitions, "tenant_id": 42})
        if method == "DELETE":
            return API.Response(200, {})
        if expected == (404,):
            return API.Response(404, {})
        failed = execution_id(5) in path
        if path.endswith("/tree"):
            return API.Response(200, self.tree(failed))
        if "/events?" in path:
            identity = execution_id(5 if failed else 1)
            items = [{"id": n, "execution_id": identity, "attempt": 1, "occurred_at": "2026-10-02T00:00:00Z",
                      "kind": kind, "counters": {}} for n, kind in ((1, "started"), (2, "failed" if failed else "completed"))]
            if self.fault == "event_private":
                items[0]["message"] = "password=never-print"
            if self.fault == "event_duplicate":
                items.append({**items[-1], "id": 3})
            return API.Response(200, {"items": items, "next_cursor": items[-1]["id"], "has_more": False, "retained_after": "2026-09-02"})
        steps = {"nested": {"status": "failed" if failed else "success"}}
        if not failed or self.fault == "dependent":
            steps["after"] = {"status": "success"}
        return API.Response(200, {"execution_id": execution_id(5 if failed else 1), "status": "failed" if failed else "success",
                                "progress": 100 if not failed or self.fault == "failure_progress" else 0,
                                "metadata": {"step_results": steps},
                                "error_details": {"code": "orchestrator.execution.step_timeout" if self.fault == "parent_timeout"
                                                  else "orchestrator.execution.child_failed"}})


class DeniedReader:
    def __init__(self):
        self.calls = []

    def request(self, method, path, expected):
        self.calls.append((method, path, expected))
        return API.Response(expected[0], {})


class OrchestratorOnlineTest(unittest.TestCase):
    def run_owner(self, owner, control=None):
        self.report, self.controls = {}, []
        self.denied, self.foreign = DeniedReader(), DeniedReader()
        return ONLINE.run_suite(owner, self.denied, self.foreign, 42, "run-test", 9, self.report,
                                control=control or self.controls.append)

    def test_permission_extension_is_required_before_definition_cleanup_and_failure_defers_teardown(self):
        for failed in (False, True):
            owner, report, order = FakeOwner(), {}, []

            class Permissions:
                def parent_read_child_hidden(self, identity, visible, evidence):
                    order.append("partial")
                    self_check = len(visible) == 4 and len(evidence["checks"]) == 15
                    if not self_check:
                        raise API.SuiteError("permissions ran before the existing faults and full tree")

                def revoke_owner_read(self, *args):
                    order.append("revoke")
                    if failed:
                        raise API.SuiteError("permission assertion failed")

            def fault_case(*args):
                args[-1]["checks"].extend(["fault", "no_replay"])

            def unavailable(*args):
                order.append("unavailable")

            with patch.object(ONLINE, "run_fault_case", side_effect=fault_case), patch.object(ONLINE, "owner_unavailable", side_effect=unavailable):
                def run():
                    return ONLINE.run_suite(owner, DeniedReader(), DeniedReader(), 42, "run-test", 9, report,
                        control=lambda _: None, faults=object(), permissions=Permissions())
                if failed:
                    with self.assertRaisesRegex(API.SuiteError, "permission assertion"):
                        run()
                    self.assertEqual(report["cleanup"], "deferred_to_deployment_destruction")
                    self.assertFalse(any(method == "DELETE" for method, _, _ in owner.calls))
                else:
                    run()
                    self.assertEqual(report["cleanup"], "definitions_deleted")
                self.assertEqual(order, ["partial", "unavailable", "revoke"])

    def test_real_owner_contract_and_closed_report(self):
        owner = FakeOwner()
        report = self.run_owner(owner)
        self.assertEqual(report["result"], "passed")
        self.assertEqual(self.controls, ["stop", "start"])
        self.assertEqual(report["cleanup"], "definitions_deleted")
        self.assertEqual(report["history_cleanup"], "pending_deployment_destruction")
        self.assertEqual(len(report["checks"]), 11)
        self.assertEqual(len(self.denied.calls), 4)
        self.assertEqual(len(self.foreign.calls), 4)
        self.assertEqual([path for method, path, _ in owner.calls if method == "DELETE"], list(reversed(report["resources"])))
        self.assertNotIn("step_results", str(report))
        self.assertNotIn("execution_config", str(report))
        self.assertNotIn("residual_resources", report)

    def test_rejects_private_projection_or_truncated_tree_before_fault(self):
        for fault in ("private", "truncated", "event_private", "event_duplicate"):
            with self.subTest(fault=fault), self.assertRaises(API.SuiteError):
                self.run_owner(FakeOwner(fault))
            self.assertEqual(self.controls, [])
            self.assertEqual(self.report["result"], "failed")

    def test_failure_progress_and_dependency_execution_are_rejected(self):
        for fault in ("failure_progress", "dependent"):
            with self.subTest(fault=fault), self.assertRaises(API.SuiteError):
                self.run_owner(FakeOwner(fault))
            self.assertEqual(self.controls, ["stop", "start"])
            self.assertEqual(self.report["result"], "failed")

    def test_unknown_post_outcome_is_not_retried_or_reported_clean(self):
        owner = FakeOwner("lost_create")
        with self.assertRaisesRegex(API.SuiteError, "cleanup failed"):
            self.run_owner(owner)
        self.assertTrue(self.report["uncertain_mutation"])
        self.assertEqual(self.report["cleanup"], "failed")
        self.assertEqual(len(owner.calls), 1)

    def test_parent_timeout_does_not_delete_definitions_of_unknown_active_children(self):
        owner = FakeOwner("parent_timeout")
        with self.assertRaisesRegex(API.SuiteError, "failure reason"):
            self.run_owner(owner)
        self.assertEqual(self.controls, ["stop", "start"])
        self.assertFalse(any(method == "DELETE" for method, _, _ in owner.calls))
        self.assertEqual(self.report["cleanup"], "deferred_to_deployment_destruction")

    def test_stop_failure_still_attempts_restore(self):
        controls = []

        def control(action):
            controls.append(action)
            if action == "stop":
                raise API.SuiteError("interrupted")

        with self.assertRaisesRegex(API.SuiteError, "interrupted"):
            self.run_owner(FakeOwner(), control)
        self.assertEqual(controls, ["stop", "start"])

    def test_restore_failure_fails_suite_and_preserves_definitions_for_teardown(self):
        def control(action):
            if action == "start":
                raise API.SuiteError("restore failed")

        owner = FakeOwner()
        with self.assertRaisesRegex(API.SuiteError, "cleanup failed"):
            self.run_owner(owner, control)
        self.assertEqual(self.report["result"], "failed")
        self.assertFalse(any(method == "DELETE" for method, _, _ in owner.calls))

    def test_nested_lineage_detects_duplicate_or_wrong_parent(self):
        node = FakeOwner().tree(False)
        node["children"].append(copy.deepcopy(node["children"][0]))
        with self.assertRaisesRegex(API.SuiteError, "duplicate"):
            ONLINE.tree_ids(node)
        node = FakeOwner().tree(False)
        node["children"][0]["execution"]["parent_execution_id"] = "other"
        with self.assertRaisesRegex(API.SuiteError, "lineage"):
            ONLINE.tree_ids(node)

    @patch.object(ONLINE.subprocess, "run")
    def test_source_fault_rejects_an_unowned_container_before_stop(self, run):
        run.return_value = subprocess.CompletedProcess([], 0, '[{"Config":{"Labels":{"com.addp.online-fixture":"other"}}}]', '')
        with self.assertRaisesRegex(API.SuiteError, "unowned"):
            ONLINE.source_action("stop")
        self.assertEqual(run.call_count, 1)

    @patch.object(ONLINE.subprocess, "run")
    def test_source_fault_only_controls_the_verified_disposable_container(self, run):
        run.return_value = subprocess.CompletedProcess([], 0, '[{"Config":{"Labels":{"com.addp.online-fixture":"metric"}}}]', '')
        ONLINE.source_action("start")
        self.assertEqual(run.call_args_list[1].args[0], ["docker", "start", "addp-metric-online-disposable"])
        self.assertEqual(run.call_args_list[2].args[0], ["docker", "exec", "addp-metric-online-disposable", "pg_isready", "-U", "postgres", "-d", "metric_fixture"])

    @patch.object(ONLINE.time, "monotonic", side_effect=[0, 121])
    def test_pending_execution_has_a_bounded_wait(self, clock):
        with self.assertRaisesRegex(API.SuiteError, "convergence"):
            ONLINE.wait_execution(FakeOwner(), execution_id(1))



class PermissionGateway:
    def __init__(self, state, identity):
        self.state, self.identity = state, identity
        self.token, self.base_url, self.timeout = identity, "http://owned.test", 2

    def context(self):
        admin = self.identity == "admin"
        revoked = self.identity == "fresh"
        owner_access = not revoked or self.state.get("retained_permission")
        permissions = (["iam.tenant_role_assignment.read", "iam.tenant_role_assignment.revoke"] if admin
                       else ["monitor.execution.read"] + (["orchestrator.workflow.read"] if owner_access else []))
        return {"principal": {"type": "user", "id": "9" if admin else "7"},
                "context": {"type": "tenant", "tenant_id": self.state.get("admin_tenant", "42") if admin else "42", "tenant_membership_id": "8"},
                "token": {"type": "first_party_access_token"},
                "authorization": {"role_assignments": [{"role_key": self.state.get("admin_role", "tenant.administrator") if admin else ONLINE.OWNER_READER_ROLE,
                                                         "permissions": permissions}]}}

    def request(self, method, path, expected, body=None):
        self.state.setdefault("calls", []).append((self.identity, method, path))
        status, payload = 200, {}
        if path == ONLINE.AUTH_CONTEXT:
            payload = self.context()
        elif path.startswith(ONLINE.IAM_ASSIGNMENTS):
            if method == "POST":
                self.state["revoked"] = True
                if self.state.get("lost_revoke"):
                    raise API.SuiteError("revocation response unavailable")
            payload = {"id": "11", "principal_type": "user", "principal_id": "7",
                       "membership_id": "8", "username": "external-online-parent-reader",
                       "role_key": ONLINE.OWNER_READER_ROLE, "scope_type": "tenant",
                       "status": "revoked" if self.state.get("revoked") else "active"}
            if self.state.get("foreign_assignment"):
                payload["principal_id"] = "99"
        elif path == "/api/v1/system/login":
            payload = {"next_action": "session_issued", "session": {"access_token": "fresh"}}
        elif self.identity == "reader" and self.state.get("revoked"):
            status = 200 if self.state.get("old_token_valid") else 401
        elif self.identity == "fresh":
            status = 403 if path.startswith(ONLINE.ORCH) else 404
        elif any(execution_id(n) in path for n in (3, 4)):
            status = 404
        elif path.endswith("/tree"):
            payload = FakeOwner().tree(False)
            if not self.state.get("leak_child"):
                payload["children"] = payload["children"][:1]
                payload["children"][0]["children"] = []
        elif "/events?" in path:
            payload = FakeOwner().request(method, path, expected).payload
        elif path.startswith(ONLINE.MONITOR):
            payload = {"execution_id": execution_id(1)}
        if status == 404 and self.state.get("leak_denial"):
            payload = {"execution_id": execution_id(1)}
        require_status = status in expected
        if not require_status:
            raise API.SuiteError("real response status rejected by permission assertion")
        return API.Response(status, payload)


class PermissionCasesTest(unittest.TestCase):
    def cases(self, state):
        return ONLINE.PermissionCases(PermissionGateway(state, "reader"), PermissionGateway(state, "admin"),
            PermissionGateway(state, "login"), 42, "11", "external-online-parent-reader", "private-password")

    def test_parent_projection_and_formal_revocation_relogin_preserve_monitor_access(self):
        state, report = {}, {"checks": []}
        cases = self.cases(state)
        visible = ONLINE.tree_ids(FakeOwner().tree(False))
        cases.parent_read_child_hidden(execution_id(1), visible, report)
        with patch.object(API, "GatewayClient", side_effect=lambda *_: PermissionGateway(state, "fresh")):
            cases.revoke_owner_read(execution_id(1), report, lambda: None)
        self.assertEqual(report["checks"], ["parent_read_child_hidden", "revoked_token_invalid", "revoked_owner_read_invisible"])
        self.assertFalse(report["uncertain_mutation"])
        self.assertEqual(sum(m == "POST" and p.endswith("/revoke") for _, m, p in state["calls"]), 1)
        self.assertEqual(sum(who == "admin" and p.startswith(ONLINE.MONITOR) for who, _, p in state["calls"]), 0)
        for private in ("private-password", "membership_id", "assignment_id", "access_token"):
            self.assertNotIn(private, str(report))

    def test_foreign_assignment_is_refused_before_revocation(self):
        state = {"foreign_assignment": True}
        with self.assertRaisesRegex(API.SuiteError, "revocation target"):
            self.cases(state)
        self.assertFalse(any(method == "POST" for _, method, _ in state["calls"]))

    def test_wrong_tenant_or_platform_controller_is_rejected_before_target_lookup(self):
        for state in ({"admin_tenant": "99"}, {"admin_role": "platform.system_administrator"}):
            with self.subTest(state=state), self.assertRaises(API.SuiteError):
                self.cases(state)
            self.assertFalse(any(p.startswith(ONLINE.IAM_ASSIGNMENTS) for _, _, p in state["calls"]))

    def test_denied_child_response_cannot_carry_diagnostics(self):
        cases = self.cases({"leak_denial": True})
        with self.assertRaisesRegex(API.SuiteError, "diagnostic data"):
            cases.parent_read_child_hidden(execution_id(1), ONLINE.tree_ids(FakeOwner().tree(False)), {"checks": []})

    def test_child_leak_is_rejected(self):
        cases = self.cases({"leak_child": True})
        with self.assertRaisesRegex(API.SuiteError, "child owners"):
            cases.parent_read_child_hidden(execution_id(1), ONLINE.tree_ids(FakeOwner().tree(False)), {"checks": []})

    def test_unknown_revoke_is_not_retried_and_old_token_or_retained_owner_is_not_accepted(self):
        for fault in ("lost_revoke", "old_token_valid", "retained_permission"):
            state, report = {fault: True}, {"checks": []}
            cases = self.cases(state)
            with patch.object(API, "GatewayClient", side_effect=lambda *_: PermissionGateway(state, "fresh")):
                with self.subTest(fault=fault), self.assertRaises(API.SuiteError):
                    cases.revoke_owner_read(execution_id(1), report, lambda: None)
            self.assertNotIn("revoked_owner_read_invisible", report["checks"])
            self.assertEqual(sum(m == "POST" and p.endswith("/revoke") for _, m, p in state["calls"]), 1)
            if fault == "lost_revoke":
                self.assertTrue(report["uncertain_mutation"])
                self.assertFalse(any(p.endswith("/login") for _, _, p in state["calls"]))


class ScopeFault:
    def __init__(self):
        self.active, self.released = False, False

    def arm(self, *args):
        self.active = True

    def release(self):
        self.active, self.released = False, True

    def witness(self):
        return {"scope_drops": 3, "posts": 0, "child_execution_ids": []}


class OwnerUnavailableTest(unittest.TestCase):
    def test_real_unavailable_contract_and_restoration_are_required(self):
        for fault in (None, "leaked_data", "wrong_code", "changed_tree", "transport_unproven"):
            faults, report = ScopeFault(), {"checks": []}
            visible = ONLINE.tree_ids(FakeOwner().tree(False))

            class Reader:
                def request(self, method, path, expected):
                    if faults.active:
                        payload = {"error": "owner unavailable", "error_code": "execution_owner_unavailable"}
                        if fault == "leaked_data":
                            payload["execution"] = visible[execution_id(1)]
                        if fault == "wrong_code":
                            payload["error_code"] = "other_error"
                        return API.Response(503, payload)
                    tree = FakeOwner().tree(False)
                    if fault == "changed_tree":
                        tree["children"] = []
                    return API.Response(200, tree)

            if fault == "transport_unproven":
                faults.witness = lambda: {}
            with self.subTest(fault=fault):
                if fault is None:
                    ONLINE.owner_unavailable(Reader(), execution_id(1), visible, 7, faults, report)
                    self.assertEqual(len(report["checks"]), 2)
                else:
                    with self.assertRaises(API.SuiteError):
                        ONLINE.owner_unavailable(Reader(), execution_id(1), visible, 7, faults, report)
                    self.assertNotIn("owner_recovery_restores_access", report["checks"])
                self.assertTrue(faults.released)


if __name__ == "__main__":
    unittest.main()
