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


if __name__ == "__main__":
    unittest.main()
