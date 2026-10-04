import copy
import importlib
import io
import json
import subprocess
import unittest
from unittest.mock import Mock, patch

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
        self.tasks = {}
        self.deleted = False

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
        if self.deleted:
            if self.fault == "history_tree_lost":
                result["children"] = []
            if self.fault == "history_step_changed":
                result["execution"]["steps"][0]["id"] = "invented"
            if failed and self.fault == "history_error_changed":
                result["execution"]["error_details"]["code"] = "invented"
        return result

    def request(self, method, path, expected, body=None, *, response_type=dict):
        self.calls.append((method, path, body))
        if method == "POST" and path.endswith("/execute"):
            self.launches += 1
            return API.Response(202, {"execution_id": execution_id(1 if self.launches == 1 else 5), "status": "pending"})
        if method == "POST":
            if self.fault == "lost_create":
                raise API.SuiteError("create response unavailable")
            self.definitions += 1
            payload = {"id": self.definitions, "tenant_id": 42}
            self.tasks[f"{path}/{self.definitions}"] = payload
            return API.Response(201, payload)
        if method == "DELETE":
            if self.fault != "delete_noop" and not (self.fault == "meta_delete_noop" and path.startswith(ONLINE.META)):
                del self.tasks[path]
            self.deleted = not self.tasks
            if self.fault == "lost_delete":
                raise API.SuiteError("delete response unavailable")
            return API.Response(200, {})
        if path == f"{ONLINE.META}/scan/tasks":
            if response_type is not list:
                raise API.SuiteError("scan task list requires an explicit array contract")
            if self.fault == "invalid_meta_list":
                return API.Response(200, {})
            return API.Response(200, [item for task, item in self.tasks.items() if task.startswith(ONLINE.META)])
        if path.startswith(f"{ONLINE.ORCH}/orchestrations/"):
            status = 200 if path in self.tasks else 404
            if status not in expected:
                raise API.SuiteError("definition response status rejected")
            return API.Response(status, self.tasks.get(path, {}))
        identity = path.split("/by-execution-id/", 1)[-1].split("/executions/", 1)[-1].split("/", 1)[0]
        failed = identity in {execution_id(n) for n in (5, 6, 7)}
        if path.endswith("/tree"):
            return API.Response(200, self.tree(failed))
        if "/events?" in path:
            items = [{"id": n, "execution_id": identity, "attempt": 1, "occurred_at": "2026-10-02T00:00:00Z",
                      "kind": kind, "counters": {}} for n, kind in ((1, "started"), (2, "failed" if failed else "completed"))]
            if self.fault == "event_private":
                items[0]["message"] = "password=never-print"
            if self.fault == "event_duplicate":
                items.append({**items[-1], "id": 3})
            if self.deleted and self.fault == "history_events_changed":
                items[-1]["counters"] = {"progress": 99}
            if self.deleted and self.fault == "history_event_missing":
                items = items[1:]
            return API.Response(200, {"items": items, "next_cursor": items[-1]["id"], "has_more": False, "retained_after": "2026-09-02"})
        if path.startswith(ONLINE.META + "/executions/"):
            detail = copy.deepcopy(ONLINE.tree_ids(self.tree(failed))[identity])
            if self.deleted and self.fault == "meta_history_lost":
                raise API.SuiteError("Meta historical detail returned 404")
            if self.deleted and self.fault == "meta_history_private":
                detail["execution_config"] = {"password": "never-print"}
            if self.deleted and self.fault == "meta_history_changed":
                detail["progress"] = 1
            return API.Response(200, detail)
        if path.startswith(ONLINE.MONITOR):
            if self.deleted and self.fault == "history_detail_lost":
                raise API.SuiteError("historical detail returned 404")
            detail = copy.deepcopy(ONLINE.tree_ids(self.tree(failed))[identity])
            if self.deleted and self.fault == "history_private":
                detail["execution_config"] = {"password": "never-print"}
            if self.deleted and self.fault == "history_detail_changed":
                detail["progress"] = 1
            return API.Response(200, detail)
        steps = {"nested": {"status": "failed" if failed else "success"}}
        if not failed or self.fault == "dependent":
            steps["after"] = {"status": "success"}
        if self.deleted and self.fault == "history_owner_steps_changed":
            steps["nested"]["status"] = "pending"
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


class OnlineAPIResponseTest(unittest.TestCase):
    def test_real_transport_validates_object_and_array_contracts(self):
        client = API.GatewayClient("http://owned.test", "private-token", 2)
        for payload, response_type, accepted in (([{"id": 11}], list, True), ([], list, True),
                                                  ({}, dict, True), ({}, list, False), ([], dict, False),
                                                  (None, list, False), ("not-a-list", list, False)):
            response = Mock()
            response.__enter__ = Mock(return_value=response)
            response.__exit__ = Mock(return_value=False)
            response.status, response.read.return_value = 200, json.dumps(payload).encode()
            with self.subTest(payload=payload, response_type=response_type), patch.object(API.urllib.request, "urlopen", return_value=response):
                if accepted:
                    self.assertEqual(client.request("GET", "/tasks", (200,), response_type=response_type).payload, payload)
                else:
                    with self.assertRaisesRegex(API.SuiteError, "JSON"):
                        client.request("GET", "/tasks", (200,), response_type=response_type)

    def test_array_endpoint_http_errors_preserve_safe_error_code(self):
        error = API.urllib.error.HTTPError("http://owned.test/tasks", 403, "Forbidden", {},
            io.BytesIO(b'{"error_code":"access_denied","error":"never-print"}'))
        client = API.GatewayClient("http://owned.test", "private-token", 2)
        with patch.object(API.urllib.request, "urlopen", side_effect=error):
            with self.assertRaisesRegex(API.SuiteError, "HTTP 403 \\(access_denied\\)") as raised:
                client.request("GET", "/tasks", (200,), response_type=list)
        self.assertNotIn("never-print", str(raised.exception))
        self.assertNotIn("private-token", str(raised.exception))


class OrchestratorOnlineTest(unittest.TestCase):
    def run_owner(self, owner, control=None):
        self.report, self.controls = {}, []
        self.denied, self.foreign = DeniedReader(), DeniedReader()
        return ONLINE.run_suite(owner, self.denied, self.foreign, 42, "run-test", 9, self.report,
                                control=control or self.controls.append)

    def test_permissions_are_rechecked_on_deleted_history_before_revocation(self):
        for failed in (False, True):
            owner, report, order = FakeOwner(), {}, []

            class Permissions:
                def parent_read_child_hidden(self, identity, visible, evidence, check="parent_read_child_hidden"):
                    history = check == "deleted_parent_read_child_hidden"
                    order.append("history_partial" if history else "partial")
                    self_check = len(visible) == 4 and evidence["cleanup"] == ("definitions_deleted" if history else "not_started")
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
                    self.assertEqual(report["cleanup"], "definitions_deleted")
                    self.assertEqual(report["result"], "failed")
                else:
                    run()
                    self.assertEqual(report["cleanup"], "definitions_deleted")
                self.assertEqual(order, ["partial", "unavailable", "history_partial", "revoke"])
                self.assertEqual(sum(method == "DELETE" for method, _, _ in owner.calls), 4)

    def test_real_owner_contract_and_closed_report(self):
        owner = FakeOwner()
        report = self.run_owner(owner)
        self.assertEqual(report["result"], "passed")
        self.assertEqual(self.controls, ["stop", "start"])
        self.assertEqual(report["cleanup"], "definitions_deleted")
        self.assertEqual(report["history_cleanup"], "pending_deployment_destruction")
        self.assertEqual(len(report["checks"]), 14)
        self.assertEqual(report["deleted_definition_count"], 3)
        self.assertEqual(len(self.denied.calls), 27)
        self.assertEqual(len(self.foreign.calls), 27)
        self.assertEqual([path for method, path, _ in owner.calls if method == "DELETE"], list(reversed(report["resources"])))
        self.assertNotIn("step_results", str(report))
        self.assertNotIn("execution_config", str(report))
        self.assertNotIn("residual_resources", report)

    def test_definition_deletion_cannot_erase_rewrite_or_expose_history(self):
        for fault in ("history_tree_lost", "history_detail_lost", "history_step_changed", "history_error_changed",
                      "history_detail_changed", "history_events_changed", "history_event_missing", "history_private",
                      "history_owner_steps_changed", "meta_history_lost", "meta_history_private", "meta_history_changed"):
            with self.subTest(fault=fault), self.assertRaises(API.SuiteError):
                self.run_owner(FakeOwner(fault))
            self.assertEqual(self.report["result"], "failed")
            self.assertEqual(self.report["cleanup"], "definitions_deleted")
            self.assertNotIn("deleted_history_access_isolated", self.report["checks"])
            self.assertNotIn("never-print", str(self.report))

    def test_definition_deletion_must_be_verified_and_unknown_outcome_not_retried(self):
        for fault in ("delete_noop", "meta_delete_noop", "lost_delete", "invalid_meta_list"):
            owner = FakeOwner(fault)
            with self.subTest(fault=fault), self.assertRaises(API.SuiteError):
                self.run_owner(owner)
            self.assertEqual(self.report["result"], "failed")
            self.assertNotIn("deleted_success_history_retained", self.report["checks"])
            deletions = [p for m, p, _ in owner.calls if m == "DELETE"]
            self.assertEqual(len(deletions), len(set(deletions)))
            if fault == "lost_delete":
                self.assertEqual(len(deletions), 1)
                self.assertTrue(self.report["uncertain_mutation"])
                self.assertEqual(self.report["cleanup"], "failed")

    def test_deleted_history_denial_must_contain_no_execution_data(self):
        owner, report = FakeOwner(), {}

        class Reader(DeniedReader):
            def request(self, method, path, expected):
                response = super().request(method, path, expected)
                if owner.deleted and path.startswith(ONLINE.MONITOR):
                    return API.Response(404, {"execution_id": execution_id(1)})
                return response

        with self.assertRaisesRegex(API.SuiteError, "diagnostic data"):
            ONLINE.run_suite(owner, Reader(), DeniedReader(), 42, "run-test", 9, report, control=lambda _: None)
        self.assertEqual(report["result"], "failed")
        self.assertEqual(report["cleanup"], "definitions_deleted")
        self.assertNotIn("deleted_history_access_isolated", report["checks"])

    def test_parent_child_permissions_and_revocation_are_required_after_deletion(self):
        for leak in (False, True):
            owner, report, state = FakeOwner(), {}, {}

            class ParentReader(PermissionGateway):
                def request(self, *args, **kwargs):
                    state["leak_child"] = leak and owner.deleted
                    return super().request(*args, **kwargs)

            cases = ONLINE.PermissionCases(ParentReader(state, "reader"), PermissionGateway(state, "admin"),
                PermissionGateway(state, "login"), 42, "11", "external-online-parent-reader", "private-password")

            def unavailable(*args):
                args[-1]["checks"].extend(["owner_unavailable_fail_closed", "owner_recovery_restores_access"])

            def fault_case(*args):
                args[-1]["checks"].extend([args[4], args[4] + "_no_replay"])
                if args[5] == "hold_renewal":
                    args[-1]["checks"].extend(["renewal_failure_cancels_request", "renewal_failure_terminal_stable"])

            with self.subTest(leak=leak), patch.object(ONLINE, "run_fault_case", side_effect=fault_case), \
                    patch.object(ONLINE, "owner_unavailable", side_effect=unavailable), \
                    patch.object(API, "GatewayClient", side_effect=lambda *_: PermissionGateway(state, "fresh")):
                def run():
                    return ONLINE.run_suite(owner, DeniedReader(), DeniedReader(), 42, "run-test", 9, report,
                        control=lambda _: None, faults=object(), permissions=cases)
                if leak:
                    with self.assertRaisesRegex(API.SuiteError, "child owners"):
                        run()
                    self.assertEqual(report["result"], "failed")
                    self.assertFalse(state.get("revoked", False))
                    self.assertNotIn("deleted_parent_read_child_hidden", report["checks"])
                else:
                    run()
                    self.assertEqual(report["result"], "passed")
                    self.assertEqual(len(report["checks"]), 28)
                    self.assertTrue(state["revoked"])
                    self.assertEqual(report["checks"][-3:], ["deleted_parent_read_child_hidden",
                        "revoked_token_invalid", "revoked_owner_read_invisible"])
                self.assertEqual(report["cleanup"], "definitions_deleted")
                self.assertEqual(report["deleted_definition_count"], 4)

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


class AdHocGateway:
    def __init__(self, owner, actor):
        self.owner, self.actor = owner, actor
        self.token = actor

    def context(self):
        peer = self.actor == "peer"
        fault = self.owner.fault
        return {"principal": {"type": "user", "id": "7" if not peer or fault == "same_actor" else "8"},
                "context": {"type": "tenant", "tenant_id": "99" if peer and fault == "foreign_peer" else "42",
                            "tenant_membership_id": "17" if not peer else "18"},
                "token": {"type": "oauth_access_token" if fault == "oauth_peer" and peer else "first_party_access_token"},
                "client": {"scope_mode": "restricted" if fault == "restricted_peer" and peer else "unrestricted"},
                "authorization": {"role_assignments": [{"role_key": "tenant.administrator" if peer and fault == "admin_peer" else "online.meta_reader",
                    "permissions": ["monitor.execution.read", "meta.scan_task.read"] +
                                   (["meta.scan_task.execute"] if not peer or fault == "peer_write" else [])}]}}

    def detail(self):
        item = {"execution_id": execution_id(9), "module": "meta", "task_type": "scan",
                "status": "success", "progress": 100, "trigger_type": "manual", "triggered_by": 7, "steps": []}
        changes = {"wrong_actor": {"triggered_by": 8}, "missing_actor": {"triggered_by": None},
                   "has_definition": {"source_task_id": "11"}, "has_parent": {"parent_execution_id": execution_id(1)},
                   "adhoc_failed": {"status": "failed"}, "adhoc_unfinished": {"status": "running"}, "adhoc_private": {"execution_config": {"password": "never-print"}}}
        item.update(changes.get(self.owner.fault, {}))
        return item

    def request(self, method, path, expected, body=None, *, response_type=dict):
        self.owner.calls.append((method, path, body))
        fault, peer = self.owner.fault, self.actor == "peer"
        status, payload = 200, {}
        if path == ONLINE.AUTH_CONTEXT:
            payload = self.context()
        elif method == "POST" and path == ONLINE.META + "/scan/run/manual":
            self.owner.ad_hoc_created = True
            if fault == "lost_adhoc_create":
                raise API.SuiteError("ad-hoc response unavailable")
            status, payload = 201, {**self.detail(), "tenant_id": 99 if fault == "foreign_execution" else 42, "status": "pending"}
        elif path.startswith(ONLINE.META + "/scan/runs?"):
            items = [copy.deepcopy(item) for item in ONLINE.tree_ids(self.owner.tree(False)).values() if item["module"] == "meta"]
            if getattr(self.owner, "ad_hoc_created", False) and (not peer or fault == "meta_peer_list_leak"):
                items.append(self.detail())
            payload = {"items": items, "total": len(items) + (1 if fault == "meta_total_leak" else 0), "page": 1, "page_size": 100}
        elif path.startswith(ONLINE.META + "/executions/") and execution_id(9) not in path:
            identity = path.rsplit("/", 1)[1]
            visible = {**ONLINE.tree_ids(self.owner.tree(False)), **ONLINE.tree_ids(self.owner.tree(True))}
            if identity in visible and visible[identity]["module"] == "meta":
                payload = copy.deepcopy(visible[identity])
                if fault == "meta_history_private": payload["execution_config"] = {"password": "never-print"}
            else:
                status, payload = 404, {}
                if fault == "meta_cross_module_leak": status, payload = 200, {"execution_id": identity}
                if fault == "meta_missing_500" and identity not in visible: status = 500
        elif path.startswith("/api/v1/monitor/executions?"):
            items = list(ONLINE.tree_ids(self.owner.tree(False)).values())
            items = [item for item in items if item["module"] == "meta"]
            if fault == "peer_no_history" and peer:
                items = []
            if getattr(self.owner, "ad_hoc_created", False) and (not peer or fault == "peer_list_leak"):
                items.append(self.detail())
            payload = {"executions": items, "total": len(items) + (1 if fault == "wrong_list_total" else 0), "page": 1, "page_size": 100}
        elif execution_id(9) in path:
            if peer:
                status = 200 if fault == "peer_detail_leak" or (fault == "meta_peer_detail_leak" and path.startswith(ONLINE.META)) else 404
                payload = self.detail() if fault in {"peer_detail_leak", "denial_payload_leak", "meta_peer_detail_leak"} else {}
            elif path.startswith(ONLINE.META):
                payload = {**self.detail(), "tenant_id": 42}
            elif path.endswith("/tree"):
                payload = {"execution": self.detail(), "children": [], "truncated": False}
                if fault == "adhoc_lineage":
                    payload["children"] = [self.owner.tree(False)]
            elif "/events?" in path:
                payload = self.owner.request(method, path, expected).payload
                if fault == "adhoc_missing_events":
                    payload["items"] = []
            else:
                payload = self.detail()
        else:
            return self.owner.request(method, path, expected, body, response_type=response_type)
        if status not in expected:
            raise API.SuiteError("real response status rejected by ad-hoc assertion")
        return API.Response(status, payload)


class AdHocCasesTest(unittest.TestCase):
    def cases(self, fault=None):
        self.owner = FakeOwner(fault)
        self.initiator, self.peer = AdHocGateway(self.owner, "initiator"), AdHocGateway(self.owner, "peer")
        return ONLINE.AdHocCases(self.initiator, self.peer, 42)

    def run_case(self, fault=None):
        case = self.cases(fault)
        self.report, self.checkpoints = {"checks": [], "uncertain_mutation": False}, []
        case.run(9, ONLINE.tree_ids(self.owner.tree(False)), DeniedReader(), DeniedReader(), self.report,
                 lambda: self.checkpoints.append(copy.deepcopy(self.report)))
        return self.report

    def test_initiator_visible_peer_has_owner_read_but_not_actor_access(self):
        report = self.run_case()
        self.assertEqual(report["checks"], ["ad_hoc_peer_owner_read_proven", "meta_direct_owner_history_and_scope", "ad_hoc_initiator_diagnostics",
                                          "ad_hoc_list_count_isolated", "ad_hoc_other_user_invisible", "meta_direct_ad_hoc_isolation"])
        self.assertEqual(report["ad_hoc_execution_id"], execution_id(9))
        self.assertEqual(report["ad_hoc_event_count"], 2)
        self.assertFalse(report["uncertain_mutation"])
        self.assertEqual(sum(method == "POST" for method, _, _ in self.owner.calls), 1)
        self.assertTrue(any(item["uncertain_mutation"] for item in self.checkpoints))
        for private in ("triggered_by", "execution_config", "never-print", "principal_id"):
            self.assertNotIn(private, json.dumps(report))

    def test_fixture_must_be_distinct_ordinary_same_tenant_read_only_user(self):
        for fault in ("same_actor", "foreign_peer", "oauth_peer", "restricted_peer", "admin_peer", "peer_write"):
            with self.subTest(fault=fault), self.assertRaises(API.SuiteError):
                self.cases(fault)
            self.assertFalse(any(method == "POST" for method, _, _ in self.owner.calls))

    def test_positive_control_and_exact_count_required_before_mutation(self):
        for fault in ("peer_no_history", "wrong_list_total", "meta_total_leak", "meta_cross_module_leak", "meta_missing_500", "meta_history_private"):
            with self.subTest(fault=fault), self.assertRaises(API.SuiteError):
                self.run_case(fault)
            self.assertFalse(any(method == "POST" for method, _, _ in self.owner.calls))

    def test_ad_hoc_actor_definition_terminal_and_safe_facts_cannot_be_substituted(self):
        for fault in ("foreign_execution", "wrong_actor", "missing_actor", "has_definition", "has_parent", "adhoc_failed", "adhoc_private",
                      "adhoc_lineage", "adhoc_missing_events"):
            with self.subTest(fault=fault), self.assertRaises(API.SuiteError):
                self.run_case(fault)
            self.assertNotIn("ad_hoc_other_user_invisible", self.report["checks"])
            self.assertNotIn("never-print", json.dumps(self.report))

    def test_peer_list_count_detail_or_denial_payload_leak_fails(self):
        for fault in ("peer_list_leak", "peer_detail_leak", "denial_payload_leak", "meta_peer_list_leak", "meta_peer_detail_leak"):
            with self.subTest(fault=fault), self.assertRaises(API.SuiteError):
                self.run_case(fault)
            self.assertNotIn("meta_direct_ad_hoc_isolation", self.report["checks"])

    def test_unknown_creation_not_retried_and_cleanup_fails_closed(self):
        case = self.cases("lost_adhoc_create")
        report = {}
        with self.assertRaisesRegex(API.SuiteError, "cleanup failed"):
            ONLINE.run_suite(self.initiator, DeniedReader(), DeniedReader(), 42, "run-test", 9, report,
                             control=lambda _: None, ad_hoc=case)
        self.assertTrue(report["uncertain_mutation"])
        self.assertEqual(report["cleanup_failures"], ["unknown_mutation_outcome"])
        self.assertEqual(sum(method == "POST" and path.endswith("/manual") for method, path, _ in self.owner.calls), 1)
        self.assertFalse(any(method == "DELETE" for method, _, _ in self.owner.calls))

    def test_known_unfinished_ad_hoc_execution_cannot_report_cleanup_success(self):
        case = self.cases("adhoc_unfinished")
        report = {}
        original_wait = ONLINE.wait_execution

        def wait(client, identity, timeout=120, module=ONLINE.ORCH):
            if module == ONLINE.META:
                raise API.SuiteError("ad-hoc deadline exceeded")
            return original_wait(client, identity, timeout, module)

        with patch.object(ONLINE, "wait_execution", side_effect=wait), self.assertRaisesRegex(API.SuiteError, "cleanup failed"):
            ONLINE.run_suite(self.initiator, DeniedReader(), DeniedReader(), 42, "run-test", 9, report,
                             control=lambda _: None, ad_hoc=case)
        self.assertFalse(report["uncertain_mutation"])
        self.assertEqual(report["ad_hoc_execution_id"], execution_id(9))
        self.assertEqual(report["cleanup_failures"], ["unfinished_ad_hoc_execution"])
        self.assertEqual(report["result"], "failed")
        self.assertFalse(any(method == "DELETE" for method, _, _ in self.owner.calls))

    def test_same_suite_runs_after_source_restore_and_before_history_deletion(self):
        case = self.cases()
        report, controls = {}, []
        ONLINE.run_suite(self.initiator, DeniedReader(), DeniedReader(), 42, "run-test", 9, report,
                         control=controls.append, ad_hoc=case)
        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["cleanup"], "definitions_deleted")
        self.assertEqual(len(report["checks"]), 20)
        self.assertEqual(report["deleted_definition_count"], 3)
        self.assertEqual(controls, ["stop", "start"])
        paths = [(method, path) for method, path, _ in self.owner.calls]
        admission = paths.index(("POST", ONLINE.META + "/scan/run/manual"))
        self.assertLess(admission, next(i for i, (method, _) in enumerate(paths) if method == "DELETE"))
        self.assertEqual(paths[-1], ("GET", ONLINE.META + "/executions/" + execution_id(9)))

if __name__ == "__main__":
    unittest.main()
