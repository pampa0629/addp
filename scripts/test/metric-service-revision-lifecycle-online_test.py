import copy
import importlib
import json
import os
import sqlite3
import subprocess
import sys
import unittest
from contextlib import closing
from pathlib import Path
from unittest.mock import patch

_API = importlib.import_module("scripts.utils.online-api")
Response, SuiteError = _API.Response, _API.SuiteError

ONLINE = importlib.import_module("scripts.test.metric-service-revision-lifecycle-online")
QUERY = {"parameters": {"subject_id": "fixture", "start_date": "2026-01-01",
                        "end_date": "2027-01-01", "grain": "total"},
         "select": ["value"], "page": {"limit": 10}}
DATA = [{"value": 10}]


class FixtureGateway:
    """Owner contract double enforces approval, review and aggregate versions."""
    def __init__(self):
        self.tables = {}
        self.fields = {}
        self.requests = []
        self.serial = 10
        self.definition_version = 1

    def request(self, method, path, expected, body=None):
        self.requests.append((method, path, copy.deepcopy(body)))
        self.serial += 1
        identity = {"id": self.serial, "tenant_id": 42, "version": 1}
        if path.endswith("/dw-layers"):
            return Response(201, identity)
        if path == "/api/v1/standard/metrics":
            assert body["effective_from"] == "2020-01-01T00:00:00Z"
            return Response(201, dict(identity, draft_revision={"id": 7}))
        if path.startswith("/api/v1/standard/metrics/"):
            assert body["version"] == self.definition_version
            self.definition_version += 1
            return Response(200, {"version": self.definition_version, "current_revision": {"status": "published"}})
        if path.endswith("/logical-tables"):
            self.tables[identity["id"]] = dict(identity, **body, status="draft", fields=[])
            return Response(201, copy.deepcopy(self.tables[identity["id"]]))
        if "/logical-tables/" in path:
            table = self.tables[int(path.split("/")[-2])]
            assert body["version"] == table["version"]
            assert table["status"] == "draft"
            table["version"] += 1
            if path.endswith("/fields"):
                field = dict(body, id=self.serial)
                table["fields"].append(field)
                self.fields[self.serial] = field
                return Response(201, {"field": field, "version": table["version"]})
            if path.endswith("/dimension-relations"):
                assert self.tables[body["target_table"]]["status"] == "approved"
                assert self.fields[body["target_field"]]["is_pk"]
                return Response(201, {"relation": {"id": self.serial}, "version": table["version"]})
            assert path.endswith("/approve")
            assert any(field["is_pk"] for field in table["fields"])
            table["status"] = "approved"
            return Response(200, table)
        if path == ONLINE.MODEL:
            assert self.tables[body["fact_table_id"]]["status"] == "approved"
            return Response(201, identity)
        if path.endswith("/draft"):
            self.contract = body["contract"]
            assert self.fields[self.contract["time"]["field_id"]]["data_type"] == "date"
            return Response(200, {"version": 2, "revisions": [{"id": 99, "status": "draft"}]})
        if path.endswith("/99/publish"):
            return Response(200, {})
        raise AssertionError(path)


class Gateway:
    def __init__(self, defect=None):
        self.defect = defect
        self.calls = []
        self.template = {"id": 10, "version": 4, "tenant_id": 42, "fact_table_id": 5,
                         "metric_definition_id": 6, "revisions": [{"id": 100, "status": "published",
                         "metric_definition_revision_id": 60, "contract": {"operation": "count_distinct"}}]}
        self.item = None
        self.service = None
        self.queries = 0

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, copy.deepcopy(body)))
        status, payload = self.respond(method, path, body)
        if status not in expected:
            raise SuiteError(f"unexpected HTTP {status}")
        return Response(status, copy.deepcopy(payload))

    def respond(self, method, path, body):
        if method == "GET" and path == ONLINE.MODEL + "/10":
            return 200, self.template
        if path == ONLINE.MODEL + "/10/revisions/100/plan":
            return 200, {}
        if method == "POST" and path == ONLINE.MODEL:
            self.item = dict(body, id=11, tenant_id=42, version=1, revisions=[])
            return 201, self.item
        if path == ONLINE.MODEL + "/11/draft":
            assert body["version"] == self.item["version"]
            self.item["version"] += 1
            self.item["revisions"].append(dict(body, id=101 + len(self.item["revisions"]), status="draft"))
            return 200, self.item
        if path.endswith(("/publish", "/withdraw")):
            assert body["version"] == self.item["version"]
            revision_id = int(path.split("/")[-2])
            revision = next(r for r in self.item["revisions"] if r["id"] == revision_id)
            revision["status"] = "published" if path.endswith("/publish") else "withdrawn"
            self.item["version"] += 1
            if self.defect == "auto_bind" and revision_id == 102:
                self.service["data_config"]["source_snapshot"]["metric_source"]["revision_id"] = 102
            return 200, self.item
        if path.endswith("/101/plan"):
            return 409, {"error_code": "metric_implementation_state_conflict"}
        if method == "POST" and path == ONLINE.SERVICE:
            self.service = dict(body, id=22, version=1, tenant_id=42, status="active", service_version="v1",
                                data_config={"source_snapshot": {"metric_source": body["metric_source"]}})
            return 201, self.service
        if path == ONLINE.SERVICE + "/22" and method == "GET":
            return (200, self.service) if self.service else (404, {})
        if path.endswith("/metric-source"):
            assert body["version"] == self.service["version"]
            if self.defect != "ignore_rebind":
                self.service["data_config"]["source_snapshot"]["metric_source"] = body["metric_source"]
                self.service.update(version=2, service_version="v2")
            return 200, self.service
        if path == ONLINE.SERVICE + "/22" and method == "DELETE":
            assert body["version"] == self.service["version"]
            if self.defect == "cleanup":
                raise SuiteError("forced cleanup failure")
            self.service = None
            return 200, {}
        if path.startswith("/api/query/"):
            self.queries += 1
            if self.defect == "interrupt":
                raise KeyboardInterrupt()
            if self.defect == "query_fail":
                raise SuiteError("forced query failure")
            bound = self.service["data_config"]["source_snapshot"]["metric_source"]["revision_id"]
            revision = next(r for r in self.item["revisions"] if r["id"] == bound)
            if revision["status"] == "withdrawn" and self.defect != "executes_withdrawn":
                return 500, {"error_code": "wrong" if self.defect == "wrong_error" else "query_execution_failed"}
            data = [{"value": 999}] if self.defect == "wrong_data" else DATA
            return 200, {"data": data, "page": {"has_more": self.defect == "partial"},
                         "service_version": self.service["service_version"]}
        raise AssertionError(f"unexpected request {method} {path}")


class MetricLifecycleTest(unittest.TestCase):
    def test_hosted_fixture_uses_reviewed_definition_and_approved_related_tables(self):
        client, report = FixtureGateway(), {}
        with patch.object(ONLINE.FIXTURE.SCAN, "wait_for_scan", return_value="scan-1"):
            implementation, revision = ONLINE.FIXTURE.prepare(client, 8, 42, "postgresql", report, lambda: None)
        self.assertGreater(implementation, 0)
        self.assertEqual(revision, 99)
        self.assertEqual(len(client.tables), 3)
        self.assertEqual(len(report["fixture_resources"]), 6)
        self.assertTrue(all(table["status"] == "approved" for table in client.tables.values()))
        self.assertEqual(client.contract["filters"][0]["value"], True)
        self.assertNotIn("DELETE", [method for method, _, _ in client.requests])

    def test_tidb_fixture_uses_database_namespace_and_same_metric_contract(self):
        client, report = FixtureGateway(), {}
        with patch.object(ONLINE.FIXTURE.SCAN, "wait_for_scan", return_value="scan-1"):
            ONLINE.FIXTURE.prepare(client, 8, 42, "tidb", report, lambda: None)
        self.assertEqual(len(client.tables), 3)
        self.assertTrue(all(table["materialization"]["target_parent_locator"] ==
                            "addp://engine/8/path/metric_fixture?type=database"
                            for table in client.tables.values()))
        self.assertEqual(client.contract["operation"], "count_distinct")
        self.assertEqual(client.contract["filters"][0]["value"], True)

    def test_failed_scan_creates_no_logical_or_standard_resources(self):
        client = FixtureGateway()
        with patch.object(ONLINE.FIXTURE.SCAN, "wait_for_scan", side_effect=ONLINE.FIXTURE.SCAN.SuiteError("failed")):
            with self.assertRaises(SuiteError):
                ONLINE.FIXTURE.prepare(client, 8, 42, "postgresql", {}, lambda: None)
        self.assertEqual(client.requests, [])

    def test_physical_seed_has_a_nontrivial_deterministic_expected_count(self):
        source = (Path(__file__).parents[2] / "business/scripts/online-metric-postgres-fixture.sh").read_text()
        sql = source.split("<<'SQL'\n", 1)[1].split("CREATE ROLE", 1)[0]
        # This checks the fixture's data contract, not the PostgreSQL runtime/compiler.
        with closing(sqlite3.connect(":memory:")) as connection:
            connection.executescript(sql)
            value = connection.execute("""SELECT count(DISTINCT f.event_id)
                FROM metric_facts f JOIN metric_events e USING(event_id)
                WHERE person_id='A' AND leader=true
                  AND event_date >= '2026-01-01' AND event_date < '2027-01-01'""").fetchone()[0]
            self.assertEqual(ONLINE.FIXTURE.EXPECTED_DATA, [{"value": value}])
            self.assertEqual(value, 2)
            self.assertGreater(connection.execute("SELECT count(*) FROM metric_facts WHERE person_id='A'").fetchone()[0], value)

        tidb_source = (Path(__file__).parents[2] / "scripts/test/online-hosted-metric-gate.sh").read_text()
        tidb_sql = tidb_source.split("<<SQL\n", 1)[1].split("\nSQL\n", 1)[0]
        tidb_sql = "\n".join(line.replace("metric_fixture.", "") for line in tidb_sql.splitlines()
                             if not line.startswith(("CREATE DATABASE", "CREATE USER", "GRANT ")))
        with closing(sqlite3.connect(":memory:")) as connection:
            connection.executescript(tidb_sql)
            value = connection.execute("""SELECT count(DISTINCT f.event_id)
                FROM metric_facts f JOIN metric_events e USING(event_id)
                WHERE person_id='A' AND leader=1
                  AND event_date >= '2026-01-01' AND event_date < '2027-01-01'""").fetchone()[0]
            self.assertEqual(ONLINE.FIXTURE.EXPECTED_DATA, [{"value": value}])

    def run_scenario(self, gateway, report):
        return ONLINE.run_suite(gateway, 42, "run-42", 10, 100, QUERY, DATA, report)

    def test_exact_binding_withdrawal_rebind_and_cleanup_preserve_history(self):
        client, report = Gateway(), {}
        original = copy.deepcopy(client.template)
        self.run_scenario(client, report)
        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["checks"], ["initial_query", "no_automatic_rebinding",
                         "withdrawn_query_rejected", "explicit_rebinding_restores_query"])
        self.assertEqual(report["retained_history"], {"implementation_id": 11, "revision_ids": [101, 102]})
        self.assertEqual(report["temporary_residual_resources"], 0)
        self.assertIsNone(client.service)
        self.assertEqual(client.template, original)
        self.assertEqual([r["status"] for r in client.item["revisions"]], ["withdrawn", "published"])
        self.assertEqual([c[1] for c in client.calls if c[0] == "DELETE"], [ONLINE.SERVICE + "/22"])
        # Cleanup uses the new service aggregate version, not its pre-rebind version.
        self.assertIn(("DELETE", ONLINE.SERVICE + "/22", {"version": 2}), client.calls)
        self.assertNotIn("parameters", json.dumps(report))

    def test_business_defects_fail_and_still_clean_service(self):
        for defect in ("auto_bind", "ignore_rebind", "executes_withdrawn", "wrong_error",
                       "query_fail", "wrong_data", "partial"):
            with self.subTest(defect=defect):
                client, report = Gateway(defect), {}
                with self.assertRaises(SuiteError):
                    self.run_scenario(client, report)
                self.assertIsNone(client.service)
                self.assertEqual(report["cleanup"], "passed")
                self.assertEqual(report["result"], "failed")
                self.assertEqual(report["retained_history"]["implementation_id"], 11)

    def test_interruption_still_cleans_service_and_retains_history(self):
        client, report = Gateway("interrupt"), {}
        with self.assertRaises(KeyboardInterrupt):
            self.run_scenario(client, report)
        self.assertIsNone(client.service)
        self.assertEqual(report["cleanup"], "passed")
        self.assertEqual(report["result"], "failed")

    def test_cleanup_failure_never_reports_success_or_zero_residual(self):
        client, report = Gateway("cleanup"), {}
        with self.assertRaisesRegex(SuiteError, "cleanup failed"):
            self.run_scenario(client, report)
        self.assertEqual(report["result"], "failed")
        self.assertEqual(report["cleanup"], "failed")
        self.assertNotIn("temporary_residual_resources", report)
        self.assertIsNotNone(client.service)

    def test_invalid_fixture_cannot_create_history(self):
        for change in ("wrong_tenant", "wrong_revision", "withdrawn"):
            client = Gateway()
            if change == "wrong_tenant":
                client.template["tenant_id"] = 7
            elif change == "wrong_revision":
                client.template["revisions"][0]["id"] = 200
            else:
                client.template["revisions"][0]["status"] = "withdrawn"
            with self.assertRaises(SuiteError):
                self.run_scenario(client, {})
            self.assertIsNone(client.item)

    def test_query_requires_nonempty_expected_complete_bounded_data(self):
        for query, data in ((QUERY, []), (dict(QUERY, page={"limit": 101}), DATA),
                            (dict(QUERY, page={"limit": True}), DATA),
                            (dict(QUERY, page={"limit": 10, "cursor": "old"}), DATA),
                            (QUERY, [{"unexpected": 10}]), (dict(QUERY, sql="SELECT 1"), DATA)):
            with self.assertRaises(SuiteError):
                ONLINE.validate_fixture_query(query, data)

    def test_module_entry_fails_closed_without_dedicated_configuration(self):
        result = subprocess.run([sys.executable, "-m", "scripts.test.metric-service-revision-lifecycle-online"],
                                env={"PATH": os.environ["PATH"]}, text=True, capture_output=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("dedicated addp_online", result.stderr)
        self.assertNotIn("Traceback", result.stderr)

    def test_permission_preflight_requires_offline_rebind_and_execution(self):
        assignments = [{"role_key": "tenant.online_metric", "permissions": sorted(ONLINE.REQUIRED_PERMISSIONS)}]
        payload = {"principal": {"type": "user"}, "context": {"type": "tenant", "tenant_id": "42"},
                   "token": {"type": "first_party_access_token"}, "authorization": {"role_assignments": assignments}}
        class System:
            def request(self, *args):
                return Response(200, payload)
        ONLINE.validate_user_identity(System(), 42, ONLINE.REQUIRED_PERMISSIONS)
        for permission in ("model.metric_implementation.offline", "service.definition.update", "service.data_read.execute"):
            with patch.dict(assignments[0], permissions=sorted(ONLINE.REQUIRED_PERMISSIONS - {permission})):
                with self.assertRaisesRegex(SuiteError, "missing required permissions"):
                    ONLINE.validate_user_identity(System(), 42, ONLINE.REQUIRED_PERMISSIONS)


if __name__ == "__main__":
    unittest.main()
