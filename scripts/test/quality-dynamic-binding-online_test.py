"""Deterministic contract tests; these do not substitute for a dedicated T4 run."""

import copy
import hashlib
import unittest
import uuid
from importlib import import_module
from pathlib import Path
from unittest.mock import patch

SUITE = import_module("scripts.test.quality-dynamic-binding-online")
FIXTURES = [
    {"logical_table_id": 10, "locator": "addp://engine/2/path/test/a?type=table", "row_count": 2},
    {"logical_table_id": 11, "locator": "addp://engine/2/path/test/b?type=table", "row_count": 3},
]


class Gateway:
    """Stateful owner API double with explicit independent contract facts."""

    def __init__(self, fault=None):
        self.fault = fault
        self.objects = {}
        self.history = {}
        self.issues = {}
        self.scopes = {}
        self.calls = []
        self.next_id = 100
        for fixture in FIXTURES:
            self.objects[f"{SUITE.MODEL}/{fixture['logical_table_id']}"] = {
                "id": fixture["logical_table_id"], "tenant_id": 2, "version": 7, "status": "approved",
                "materialization": {"target_parent_locator": "addp://engine/2/path/test?type=schema",
                                    "target_name": "a" if fixture["logical_table_id"] == 10 else "b"},
            }

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, copy.deepcopy(body)))
        payload, status = self.dispatch(method, path, body)
        if status not in expected:
            raise SUITE.SuiteError(f"unexpected HTTP {status}")
        return SUITE.API.Response(status, copy.deepcopy(payload))

    def dispatch(self, method, path, body):
        if method == "GET" and "/task-providers/model/tasks/" in path:
            return {"execution_contract": {"input_defaults": {"version": 7}}}, 200
        if method == "GET" and path.startswith(SUITE.QUALITY + "/issues?"):
            return {"data": list(self.issues.values()), "total": len(self.issues)}, 200
        if method == "GET" and path.startswith(SUITE.QUALITY + "/overview?"):
            return {"data": list(self.scopes.values()), "total": len(self.scopes)}, 200
        if method == "POST" and path.endswith("/execute"):
            if self.fault == "lost_enqueue":
                raise SUITE.SuiteError("transport failed")
            return self.execute(self.objects[path.removesuffix("/execute")]), 202
        if method == "POST":
            if self.fault == "lost_create":
                raise SUITE.SuiteError("transport failed")
            self.next_id += 1
            item = {**copy.deepcopy(body), "id": self.next_id, "tenant_id": 2, "version": 1}
            self.objects[f"{path}/{self.next_id}"] = item
            return item, 201
        if method == "PUT":
            current = self.objects[path]
            current.update(copy.deepcopy(body))
            current["version"] += 1
            return current, 200
        if method == "DELETE":
            if self.fault == "delete":
                return {}, 409
            item = self.objects.pop(path)
            if "/plans/" in path:
                self.issues = {k: v for k, v in self.issues.items() if v["plan_id"] != item["id"]}
            return {}, 200
        if method == "GET":
            if path in self.history:
                return self.history[path], 200
            return self.objects.get(path, {}), 200 if path in self.objects else 404
        raise AssertionError((method, path))

    def execute(self, orchestration):
        upstream, gate, after_step = orchestration["steps"]
        assert upstream["parameters"] == {}, "test must regress provider default input resolution"
        assert gate["parameters"] == {"table_bindings": {"target": "{{upstream.outputs.target_locator}}"}}
        fixture = next(f for f in FIXTURES if f["logical_table_id"] == upstream["task_id"])
        plan = self.objects[f"{SUITE.QUALITY}/plans/{gate['task_id']}"]
        after = self.objects[f"{SUITE.QUALITY}/plans/{after_step['task_id']}"]
        check = plan["check_items"][0]
        passed = check["revision_no"] == 2
        parent_id, child_id = str(uuid.uuid4()), str(uuid.uuid4())
        status = "success" if passed else "failed"
        key = hashlib.sha256(fixture["locator"].encode()).hexdigest()
        rule_result = {"rule_key": check["rule_key"], "revision_no": check["revision_no"], "passed": passed,
                       "total_count": fixture["row_count"], "observed": {"row_count": fixture["row_count"]},
                       "failed_count": 0 if passed else 1}
        child = {"execution_id": child_id, "parent_execution_id": parent_id, "status": status,
                 "execution_config": {"table_bindings": [{"alias": "target", "locator": fixture["locator"]}],
                                      "task_version": plan["version"], "target_key": key},
                 "metadata": {"passed": passed, "rules": [rule_result]}, "outputs": {"passed": passed},
                 "error_details": {} if passed else {"code": "quality.plan.rule_failed"}}
        if self.fault == "wrong_count":
            rule_result["total_count"] = 999
        if self.fault == "default_fallback" and fixture is FIXTURES[1]:
            child["execution_config"]["table_bindings"] = copy.deepcopy(plan["table_bindings"])
        if self.fault == "runtime_error":
            child["error_details"]["code"] = "quality.plan.sql_execution_failed"
        if self.fault == "lineage":
            child["parent_execution_id"] = str(uuid.uuid4())
        if self.fault == "active_child":
            child["status"] = "running"
        if self.fault == "mutated_defaults":
            plan["table_bindings"] = []
        plan["last_execution_id"] = child_id
        self.history[f"{SUITE.QUALITY}/executions/{child_id}"] = child
        if key not in self.issues:
            self.issues[key] = {"id": len(self.issues) + 1, "target_key": key, "plan_id": plan["id"]}
        self.issues[key].update(status="resolved" if passed else "open", last_execution_id=child_id)
        if self.fault == "cross_resolve" and passed:
            for issue in self.issues.values():
                issue["status"] = "resolved"
        if self.fault == "duplicate_issue":
            self.issues[key]["id"] += 1
        self.scopes[key] = {"target_key": key, "plan_id": plan["id"], "status": status,
                            "observed_execution_id": child_id, "observed_version": plan["version"],
                            "total_rules": 1, "pass_rate": 100 if passed else 0}
        if self.fault == "overview_mixed" and passed:
            for scope in self.scopes.values():
                scope["status"] = "success"
        steps = {"upstream": {"status": "success", "result": {"outputs": {"target_locator": fixture["locator"]}}},
                 "quality": {"status": status}}
        if passed or self.fault == "no_block":
            steps["after"] = {"status": "success"}
            after["last_execution_id"] = str(uuid.uuid4())
            self.history[f"{SUITE.QUALITY}/executions/{after['last_execution_id']}"] = {"status": "success"}
        self.history[f"{SUITE.ORCH}/executions/{parent_id}"] = {
            "execution_id": parent_id, "status": status, "metadata": {"step_results": steps}}
        return {"execution_id": parent_id, "status": "pending"}


class QualityOnlineTest(unittest.TestCase):
    def run_scenario(self, fault=None):
        client, report = Gateway(fault), {}
        SUITE.run_suite(client, 2, "run-unit", copy.deepcopy(FIXTURES), report)
        return client, report

    def test_full_lifecycle_and_reverse_cleanup(self):
        client, report = self.run_scenario()
        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["cleanup"], "passed")
        self.assertEqual(report["temporary_residual_resources"], 0)
        self.assertEqual(len(report["executions"]), 5)
        self.assertEqual(len(client.objects), 2)  # Only permanent Model fixtures survive.
        self.assertEqual(client.issues, {})
        self.assertEqual([p for m, p, _ in client.calls if m == "DELETE"], list(reversed(report["resources"])))

    def test_regressions_fail_closed_and_cleanup(self):
        for fault in ("wrong_count", "default_fallback", "runtime_error", "lineage", "mutated_defaults",
                      "cross_resolve", "overview_mixed", "duplicate_issue", "no_block"):
            with self.subTest(fault=fault):
                client, report = Gateway(fault), {}
                with self.assertRaises(SUITE.SuiteError):
                    SUITE.run_suite(client, 2, "run-unit", FIXTURES, report)
                self.assertEqual(report["result"], "failed")
                self.assertEqual(report["cleanup"], "passed")
                self.assertEqual(len(client.objects), 2)

    def test_cleanup_failure_is_not_success(self):
        client, report = Gateway("delete"), {}
        with self.assertRaisesRegex(SUITE.SuiteError, "cleanup failed"):
            SUITE.run_suite(client, 2, "run-unit", FIXTURES, report)
        self.assertEqual(report["result"], "failed")
        self.assertEqual(report["cleanup"], "failed")
        self.assertEqual(len(report["cleanup_failures"]), 6)
        self.assertNotIn("temporary_residual_resources", report)

    def test_uncertain_mutations_do_not_race_cleanup_or_claim_zero_residue(self):
        for fault in ("lost_create", "lost_enqueue"):
            client, report = Gateway(fault), {}
            with self.subTest(fault=fault), self.assertRaisesRegex(SUITE.SuiteError, "uncertain"):
                SUITE.run_suite(client, 2, "run-unit", FIXTURES, report)
            self.assertTrue(report["uncertain_mutation"])
            self.assertEqual(report["cleanup"], "failed")
            self.assertFalse(any(m == "DELETE" for m, _, _ in client.calls))
            self.assertNotIn("temporary_residual_resources", report)

    def test_fixture_validation(self):
        invalid = [[], [FIXTURES[0]], [FIXTURES[0], FIXTURES[0]]]
        for field, value in (("row_count", 0), ("row_count", True), ("logical_table_id", -1),
                             ("locator", "http://example.test/table")):
            items = copy.deepcopy(FIXTURES)
            items[0][field] = value
            invalid.append(items)
        for items in invalid:
            with self.subTest(items=items), self.assertRaises(SUITE.SuiteError):
                SUITE.validate_fixtures(items)

    def test_active_child_prevents_definition_deletion(self):
        client, report = Gateway("active_child"), {}
        with self.assertRaisesRegex(SUITE.SuiteError, "child still active"):
            SUITE.run_suite(client, 2, "run-unit", FIXTURES, report)
        self.assertEqual(report["cleanup"], "failed")
        self.assertFalse(any(m == "DELETE" for m, _, _ in client.calls))
        self.assertNotIn("temporary_residual_resources", report)

    def test_polling_has_deadline_and_rejects_unknown_status(self):
        client = Gateway()
        execution_id = str(uuid.uuid4())
        path = f"{SUITE.ORCH}/executions/{execution_id}"
        client.history[path] = {"execution_id": execution_id, "status": "running"}
        with patch.object(SUITE.time, "monotonic", side_effect=[0, 0, 121]), patch.object(SUITE.time, "sleep"):
            with self.assertRaisesRegex(SUITE.SuiteError, "deadline"):
                SUITE.wait_execution(client, execution_id)
        client.history[path]["status"] = "unknown"
        with self.assertRaisesRegex(SUITE.SuiteError, "unknown"):
            SUITE.wait_execution(client, execution_id)

    def test_permissions_exist_in_owner_registries(self):
        root = Path(__file__).resolve().parents[2]
        for permission in SUITE.REQUIRED_PERMISSIONS:
            module = permission.split(".")[0]
            generated = root / module / "backend/internal/authorization/permissions_generated.go"
            self.assertIn(f'"{permission}"', generated.read_text())

    def test_no_dedicated_environment_fails_before_network(self):
        with patch.dict(SUITE.os.environ, {}, clear=True), patch.object(SUITE.API, "GatewayClient") as client:
            self.assertEqual(SUITE.main(), 1)
            client.assert_not_called()


if __name__ == "__main__":
    unittest.main()
