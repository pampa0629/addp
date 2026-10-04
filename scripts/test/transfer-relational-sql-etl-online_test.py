import importlib.util
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch


SCRIPT = Path(__file__).with_name("transfer-relational-sql-etl-online.py")
SPEC = importlib.util.spec_from_file_location("transfer_relational_sql_etl_online", SCRIPT)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class TransferRelationalSQLETLOnlineTest(unittest.TestCase):
    def test_registered_browser_spec_parses(self):
        browser = SCRIPT.parents[2] / "console/frontend/e2e/online/transfer-relational-sql-etl.spec.js"
        result = subprocess.run(["node", "--check", str(browser)], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_validates_same_engine_through_read_only_meta_projection(self) -> None:
        class Client:
            def __init__(self):
                self.calls = []

            def request(self, method, path, expected):
                self.calls.append((method, path, expected))
                return SimpleNamespace(
                    payload=[{
                        "id": 7,
                        "name": "Online PostgreSQL Fixture",
                        "resource_type": "postgresql",
                        "lifecycle_state": "active",
                        "connection_status": "online",
                    }]
                )

        client = Client()
        with patch.object(ONLINE.time, "monotonic", return_value=1.0):
            result = ONLINE.validate_engine(client, 7, "Online PostgreSQL Fixture", "postgresql", 10.0)

        self.assertEqual(result["connection_status"], "online")
        self.assertEqual(
            client.calls,
            [
                ("GET", "/api/v1/meta/engines", (200,)),
            ],
        )

    def test_meta_projection_rejects_wrong_engine_and_duplicate_identity(self):
        engine = {"id": 7, "name": "fixture", "resource_type": "postgresql",
                  "lifecycle_state": "active", "connection_status": "online"}
        for projection in ([dict(engine, resource_type="mysql")], [dict(engine, name="other")],
                           [dict(engine, lifecycle_state="deleted")], [engine, engine]):
            client = SimpleNamespace(request=lambda *args: SimpleNamespace(payload=projection))
            with patch.object(ONLINE.time, "monotonic", return_value=1.0), self.assertRaises(ONLINE.SuiteError):
                ONLINE.validate_engine(client, 7, "fixture", "postgresql", 10.0)

    def test_meta_projection_waits_for_same_id_without_control_plane_calls(self):
        engine = {"id": 7, "name": "fixture", "resource_type": "postgresql",
                  "lifecycle_state": "active", "connection_status": "online"}
        client = SimpleNamespace(request=Mock(side_effect=[SimpleNamespace(payload=[dict(engine, id=8)]),
                                                          SimpleNamespace(payload=[engine])]))
        with patch.object(ONLINE.time, "monotonic", return_value=1.0), patch.object(ONLINE.time, "sleep"):
            report = ONLINE.validate_engine(client, 7, "fixture", "postgresql", 10.0)
        self.assertEqual(report["verification_owner"], "deployment_profile")
        for call in client.request.call_args_list:
            self.assertEqual(call.args, ("GET", "/api/v1/meta/engines", (200,)))

    def test_task_name_is_stable_and_fits_ui_limit(self) -> None:
        first = ONLINE.task_name("run-123")

        self.assertEqual(first, ONLINE.task_name("run-123"))
        self.assertTrue(first.startswith(ONLINE.TASK_PREFIX))
        self.assertLessEqual(len(first), 50)
        self.assertNotEqual(first, ONLINE.task_name("run-456"))

    def test_validates_complete_browser_report(self) -> None:
        name = ONLINE.task_name("run-123")
        report = self.browser_report(name)

        self.assertEqual(ONLINE.validate_browser_report(report, "run-123", "42", name), report)
        report["records_written"] = 3
        with self.assertRaisesRegex(ONLINE.SuiteError, "records_written"):
            ONLINE.validate_browser_report(report, "run-123", "42", name)

    def test_run_browser_uses_registered_spec_and_validates_artifact(self) -> None:
        with tempfile.TemporaryDirectory(prefix="addp-transfer-sql-etl-browser-") as temporary:
            root = Path(temporary)
            (root / "console/frontend").mkdir(parents=True)
            artifacts = root / "artifacts"
            artifacts.mkdir()
            name = ONLINE.task_name("run-123")
            payload = self.browser_report(name)
            environment = {
                "ADDP_ONLINE_ARTIFACT_DIR": str(artifacts),
                "ADDP_ONLINE_TEST_RUN_ID": "run-123",
                "ADDP_ONLINE_TEST_TENANT_ID": "42",
            }

            def fake_run(command, **kwargs):
                (artifacts / "transfer-relational-sql-etl-browser.json").write_text(
                    json.dumps(payload), encoding="utf-8"
                )
                self.assertIn("e2e/online/transfer-relational-sql-etl.spec.js", command)
                self.assertEqual(kwargs["cwd"], root / "console/frontend")
                self.assertEqual(kwargs["env"]["ADDP_ONLINE_TRANSFER_SQL_ETL_TASK_NAME"], name)
                self.assertEqual(
                    kwargs["env"]["ADDP_ONLINE_TRANSFER_SQL_ETL_SOURCE_TABLE"],
                    ONLINE.SOURCE_TABLE,
                )
                self.assertEqual(
                    kwargs["env"]["ADDP_ONLINE_TRANSFER_SQL_ETL_TARGET_TABLE"],
                    ONLINE.TARGET_TABLE,
                )
                self.assertEqual(json.loads(kwargs["env"]["ADDP_ONLINE_TRANSFER_FIELD_LINEAGE"]), {"target_item_id": 11})
                self.assertEqual(json.loads(kwargs["env"]["ADDP_ONLINE_TRANSFER_MONGODB_FIELD_LINEAGE"]), {"target_item_id": 12})
                return SimpleNamespace(returncode=0, stdout="", stderr="")

            with patch.dict(ONLINE.os.environ, environment, clear=False), patch.object(
                ONLINE.subprocess, "run", side_effect=fake_run
            ):
                self.assertEqual(ONLINE.run_browser(root, environment, name, {"target_item_id": 11}, {"target_item_id": 12}), payload)

    @staticmethod
    def browser_report(name: str) -> dict[str, object]:
        return {
            "schema_version": "addp.transfer-relational-sql-etl-browser/v3",
            "suite": "transfer-relational-sql-etl",
            "run_id": "run-123",
            "result": "passed",
            "tenant_id": "42",
            "task_name": name,
            "query_language": "sql",
            "language_selector_hidden": True,
            "projected_fields": ["id", "region", "amount"],
            "parameter_count": 2,
            "records_read": 2,
            "records_written": 2,
            "target_row_count": 2,
            "task_deleted": True,
            "manager_field_graph_verified": True,
            "query_field_lineage_verified": True,
            "manager_mongodb_field_graph_verified": True,
        }

    def test_browser_proofs_and_current_report_version_are_required(self):
        name = ONLINE.task_name("run-123")
        for key in ("manager_field_graph_verified", "query_field_lineage_verified", "manager_mongodb_field_graph_verified"):
            with self.subTest(key=key):
                report = self.browser_report(name)
                report[key] = False
                with self.assertRaisesRegex(ONLINE.SuiteError, key):
                    ONLINE.validate_browser_report(report, "run-123", "42", name)
        report = self.browser_report(name)
        report["schema_version"] = "addp.transfer-relational-sql-etl-browser/v2"
        with self.assertRaisesRegex(ONLINE.SuiteError, "schema_version"):
            ONLINE.validate_browser_report(report, "run-123", "42", name)

    @staticmethod
    def field_graph():
        def node(item_id, field):
            return {"kind": "field_ref", "item_id": item_id, "field_name": field, "schema_snapshot_hash": f"sha256:table-{item_id}"}
        source, target, downstream = node(5, "status"), node(7, "region_name"), node(11, "area")
        return {"field_lineage_status": "complete", "truncated": False, "subject": downstream, "nodes": [source, target, downstream], "edges": [
            {"source": source.copy(), "target": target.copy(), "granularity": "field", "status": "active", "transformation": "direct", "evidence": {"execution_id": "replacement"}},
            {"source": target.copy(), "target": downstream.copy(), "granularity": "field", "status": "active", "transformation": "direct", "evidence": {"execution_id": "hop"}},
        ]}

    def test_rejects_wrong_fields_versions_and_execution_proofs(self):
        expected = {(5, "status", 7, "region_name", "direct", "replacement"), (7, "region_name", 11, "area", "direct", "hop")}
        ONLINE.validate_field_graph(self.field_graph(), 11, "area", expected)
        mutations = {
            "wrong_field": lambda graph: graph["edges"][0]["source"].update(field_name="region"),
            "mixed_snapshot": lambda graph: graph["edges"][0]["target"].update(schema_snapshot_hash="old-schema"),
            "closed": lambda graph: graph["edges"][0].update(status="closed"),
            "wrong_execution": lambda graph: graph["edges"][0]["evidence"].update(execution_id="first"),
            "missing_hash": lambda graph: graph["nodes"][0].pop("schema_snapshot_hash"),
            "truncated": lambda graph: graph.update(truncated=True),
            "unavailable": lambda graph: graph.update(field_lineage_status="unavailable"),
            "duplicate": lambda graph: graph["edges"].append(graph["edges"][0]),
            "wrong_subject_snapshot": lambda graph: graph.update(subject={**graph["subject"], "schema_snapshot_hash": "old-schema"}),
        }
        for label, mutate in mutations.items():
            with self.subTest(label=label):
                graph = self.field_graph()
                mutate(graph)
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.validate_field_graph(graph, 11, "area", expected)

    def test_generated_evidence_has_no_source_edge_and_is_not_unavailable(self):
        graph = self.field_graph()
        graph["subject"]["field_name"] = "generated_label"
        graph["nodes"] = [graph["subject"]]
        graph["edges"] = []
        ONLINE.validate_field_graph(graph, 11, "generated_label", set())
        graph["field_lineage_status"] = "unavailable"
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.validate_field_graph(graph, 11, "generated_label", set())

    def test_native_tasks_use_snapshot_mapping_and_owned_names(self):
        name = ONLINE.task_name("run-123")
        payload = ONLINE.native_task(name + "_native", "source-locator", "parent-locator", ONLINE.NATIVE_TARGET, "region", "region_name")
        config = payload["config"]
        self.assertNotIn("query", config["source"])
        self.assertEqual(config["load"], {"mode": "snapshot"})
        self.assertEqual(config["target"]["policy"], {"apply_mode": "replace"})
        fields = config["transforms"][0]["fields"]
        self.assertEqual((fields[1]["source"], fields[1]["target"]), ("region", "region_name"))
        self.assertEqual((fields[2]["precision"], fields[2]["scale"]), (8, 2))
        self.assertEqual((fields[3]["source"], fields[3]["default"]), ("", "online"))
        self.assertEqual(ONLINE.owned_task_names(name), {name, name + "_native", name + "_replace", name + "_hop", name + "_mongodb"})

    def test_cleanup_selection_is_limited_to_the_exact_run_family(self):
        name = ONLINE.task_name("run-123")
        names = sorted(ONLINE.owned_task_names(name)) + [ONLINE.task_name("another-run"), name + "_unrelated"]
        client = SimpleNamespace(request=lambda *args: SimpleNamespace(payload={"items": [{"id": index + 1, "name": value} for index, value in enumerate(names)], "total": len(names)}))
        self.assertEqual(ONLINE.suite_task_ids(client, exact_name=name), [1, 2, 3, 4, 5])

    def test_online_identity_requires_lineage_and_manager_permissions(self):
        self.assertFalse(any(key.startswith("system.engine.") for key in ONLINE.REQUIRED_PERMISSIONS))
        permissions = sorted(ONLINE.REQUIRED_PERMISSIONS)
        context = {"principal": {"type": "user", "id": "1"}, "context": {"type": "tenant", "tenant_id": "42"},
                   "token": {"type": "first_party_access_token"}, "authorization": {"role_assignments": [{"role_key": "online_operator", "permissions": permissions}]}}
        client = SimpleNamespace(request=lambda *args: SimpleNamespace(payload=context))
        ONLINE.validate_user_identity(client, 42)
        permissions.append("system.engine.execute")
        with self.assertRaisesRegex(ONLINE.SuiteError, "exceeds minimum permissions"):
            ONLINE.validate_user_identity(client, 42)
        permissions.remove("system.engine.execute")
        permissions.remove("meta.lineage.read")
        with self.assertRaisesRegex(ONLINE.SuiteError, "meta.lineage.read"):
            ONLINE.validate_user_identity(client, 42)

    def test_interruption_cleans_the_current_run_and_returns_failure(self):
        environment = {"ADDP_ONLINE_TEST": "1", "ADDP_ONLINE_TEST_TENANT_ID": "42", "ADDP_ONLINE_TEST_ENGINE_ID": "3",
                       "ADDP_ONLINE_TEST_ENGINE_NAME": "fixture", "ADDP_ONLINE_TEST_MONGODB_ENGINE_ID": "4", "ADDP_ONLINE_TEST_MONGODB_ENGINE_NAME": "mongo-fixture", "ADDP_ONLINE_TEST_RUN_ID": "run-123", "CONSOLE_URL": "http://127.0.0.1:5170",
                       "GATEWAY_URL": "http://127.0.0.1:8000", "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "fixture-token",
                       "ADDP_ONLINE_TEST_USER_USERNAME": "fixture", "ADDP_ONLINE_TEST_USER_PASSWORD": "fixture-only",
                       "ADDP_ONLINE_ARTIFACT_DIR": "/tmp/fixture-only", "ADDP_ONLINE_CONVERGENCE_TIMEOUT_SECONDS": "30"}
        client = object()
        def interrupt(*args):
            ONLINE.interrupt_online(ONLINE.signal.SIGTERM, None)
        with patch.dict(ONLINE.os.environ, environment), patch.object(ONLINE, "GatewayClient", return_value=client), patch.object(ONLINE.signal, "signal") as handlers, patch.object(ONLINE, "validate_user_identity"), patch.object(ONLINE, "validate_engine"), patch.object(ONLINE, "suite_task_ids", side_effect=[[], [10, 11]]) as owned, patch.object(ONLINE, "wait_for_scan"), patch.object(ONLINE, "find_item"), patch.object(ONLINE, "run_native_lineage", side_effect=interrupt), patch.object(ONLINE, "cleanup_tasks") as cleanup, patch.object(ONLINE.sys, "stderr"):
            self.assertEqual(ONLINE.main(), 1)
        self.assertEqual(handlers.call_args_list[0].args, (ONLINE.signal.SIGINT, ONLINE.interrupt_online))
        owned.assert_called_with(client, exact_name=ONLINE.task_name("run-123"))
        cleanup.assert_called_once_with(client, [10, 11])

    def test_native_scenario_requires_generated_replace_and_exact_two_hop_proofs(self):
        source = {"id": 5, "node_id": 9, "full_name": "public.source", "item_type": "table"}
        target = {"id": 7, "node_id": 9, "full_name": "public.target", "item_type": "table"}
        downstream = {"id": 11, "node_id": 9, "full_name": "public.downstream", "item_type": "table"}
        executions, proofs, owned = [], [], []

        def execute(client, payload, deadline, owned_ids):
            self.assertNotIn("query", payload["config"]["source"])
            identifier = ["first", "replacement", "hop"][len(executions)]
            executions.append(payload)
            owned_ids.append(len(executions))
            execution = self.execution_with_snapshots()
            execution.update(execution_id=identifier, records_read=5, records_written=5)
            if identifier == "hop":
                facts = execution["metadata"]["lineage_facts"]
                facts["inputs"][0]["schema_snapshot"]["hash"] = "sha256:target"
                facts["outputs"][0]["schema_snapshot"]["hash"] = "sha256:downstream"
            return len(executions), execution

        def proof(client, item_id, field, expected, timeout):
            proofs.append((item_id, field, expected))
            hashes = {5: "sha256:source", 7: "sha256:target", 11: "sha256:downstream"}
            identities = {(item_id, field)} | {(edge[0], edge[1]) for edge in expected} | {(edge[2], edge[3]) for edge in expected}
            return {"subject": {"schema_snapshot_hash": hashes[item_id]}, "nodes": [{"item_id": key, "field_name": name, "schema_snapshot_hash": hashes[key]} for key, name in identities]}

        with patch.object(ONLINE.SUPPORT, "create_and_run_task", side_effect=execute), patch.object(ONLINE, "wait_for_scan") as scan, patch.object(ONLINE, "find_item", side_effect=[target, target, downstream]), patch.object(ONLINE, "wait_field_graph", side_effect=proof):
            result = ONLINE.run_native_lineage(object(), 3, source, "run-name", 30, owned)
        self.assertEqual(owned, [1, 2, 3])
        self.assertEqual(scan.call_count, 3)
        self.assertEqual(proofs[2], (7, "generated_label", set()))
        self.assertEqual(proofs[3], (7, "region_name", {(5, "status", 7, "region_name", "direct", "replacement")}))
        self.assertEqual(proofs[4], (11, "area", {(5, "status", 7, "region_name", "direct", "replacement"), (7, "region_name", 11, "area", "direct", "hop")}))
        self.assertTrue(result["two_hop_verified"])

    @staticmethod
    def mongodb_execution(identifier="mongo-first"):
        source_locator = "addp://engine/4/path/transfer_fixture/activities?type=table&item_id=20"
        return {"execution_id": identifier, "status": "success", "records_read": 3, "records_written": 3, "metadata": {"lineage_facts": {
            "schema_version": "addp.lineage-facts/v1",
            "inputs": [{"port": "source", "locator": source_locator, "schema_snapshot": {"hash": "sha256:mongo", "fields": [{"name": source, "path": source.split(".")} for source, _ in ONLINE.MONGODB_FIELDS]}}],
            "outputs": [{"port": "target", "locator": "addp://engine/3/path/public/" + ONLINE.MONGODB_TARGET + "?type=table", "schema_snapshot": {"hash": "sha256:ods", "fields": [{"name": target} for _, target in ONLINE.MONGODB_FIELDS]}}],
            "operations": [{"field_lineage_status": "complete", "field_mappings": [{"input_port": "source", "output_port": "target", "source_field": source, "target_field": target, "transformation": "direct"} for source, target in ONLINE.MONGODB_FIELDS]}],
        }}}

    def test_mongodb_frozen_evidence_rejects_aliases_wrong_endpoints_and_incomplete_mappings(self):
        locator = self.mongodb_execution()["metadata"]["lineage_facts"]["inputs"][0]["locator"]
        self.assertEqual(ONLINE.validate_mongodb_execution(self.mongodb_execution(), locator, 3), ("sha256:mongo", "sha256:ods"))
        mutations = {
            "alias": lambda facts: facts["inputs"][0]["schema_snapshot"]["fields"][2].update(name="activity_date_raw"),
            "source": lambda facts: facts["inputs"][0].update(locator="other-source"),
            "target": lambda facts: facts["outputs"][0].update(locator="addp://engine/4/path/public/" + ONLINE.MONGODB_TARGET),
            "missing_mapping": lambda facts: facts["operations"][0]["field_mappings"].pop(),
            "wrong_structured_path": lambda facts: facts["inputs"][0]["schema_snapshot"]["fields"][5].update(path=["nickName"]),
            "wrong_path": lambda facts: facts["operations"][0]["field_mappings"][5].update(source_field="nickName"),
        }
        for label, mutate in mutations.items():
            with self.subTest(label=label):
                execution = self.mongodb_execution()
                mutate(execution["metadata"]["lineage_facts"])
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.validate_mongodb_execution(execution, locator, 3)
        execution = self.mongodb_execution()
        execution["records_written"] = 6
        with self.assertRaisesRegex(ONLINE.SuiteError, "three rows"):
            ONLINE.validate_mongodb_execution(execution, locator, 3)

    def test_mongodb_reruns_one_task_and_requires_all_latest_automatic_proofs(self):
        source = {"id": 20, "node_id": 21, "full_name": ONLINE.MONGODB_SOURCE, "item_type": "table"}
        pg_source = {"id": 5, "node_id": 9}
        target = {"id": 22, "node_id": 9, "full_name": "public." + ONLINE.MONGODB_TARGET, "item_type": "table"}
        calls, proofs, owned = [], [], []
        executions = iter(["mongo-first", "mongo-rerun"])
        def request(method, path, expected, payload=None):
            calls.append((method, path, payload))
            if method == "POST" and path == "/api/v1/transfer/task-definitions":
                self.assertTrue(payload["auto_scan_metadata"])
                self.assertNotIn("query", payload["config"]["source"])
                self.assertIn("/engine/3/path/public?", payload["config"]["target"]["parent_locator"])
                self.assertEqual([(field["source"], field["target"]) for field in payload["config"]["transforms"][0]["fields"]], list(ONLINE.MONGODB_FIELDS))
                return SimpleNamespace(payload={"id": 30})
            if method == "POST" and path == "/api/v1/transfer/task-definitions/30/start":
                return SimpleNamespace(payload={"execution_id": next(executions)})
            if method == "GET" and path.startswith("/api/v1/transfer/executions/"):
                return SimpleNamespace(payload=self.mongodb_execution(path.rsplit("/", 1)[1]))
            raise AssertionError("unexpected call (manual scans/collects forbidden): " + method + " " + path)
        def proof(client, item_id, field, expected, timeout):
            proofs.append(expected)
            source_field = next(iter(expected))[1]
            return {"nodes": [{"item_id": 20, "field_name": source_field, "schema_snapshot_hash": "sha256:mongo", "engine_id": 4},
                              {"item_id": 22, "field_name": field, "schema_snapshot_hash": "sha256:ods", "engine_id": 3}]}
        with patch.object(ONLINE, "find_item", side_effect=[ONLINE.SuiteError("not scanned yet"), target, target]), patch.object(ONLINE.time, "sleep"), patch.object(ONLINE, "wait_field_graph", side_effect=proof):
            report = ONLINE.run_mongodb_lineage(SimpleNamespace(request=request), 4, 3, source, pg_source, "run-name", 30, owned)
        self.assertEqual(owned, [30])
        self.assertEqual([call[1] for call in calls if call[0] == "POST"], ["/api/v1/transfer/task-definitions", "/api/v1/transfer/task-definitions/30/start", "/api/v1/transfer/task-definitions/30/start"])
        self.assertEqual(len(proofs), 12)
        self.assertEqual(report["execution_ids"], ["mongo-first", "mongo-rerun"])
        self.assertTrue(report["automatic_collection_verified"])
        self.assertTrue(report["rerun_verified"])
        self.assertEqual(report["expected_edges"], sorted((20, source, 22, target, "direct", "mongo-rerun") for source, target in ONLINE.MONGODB_FIELDS))

    def test_mongodb_engine_projection_and_distinct_engine_requirement(self):
        engine = {"id": 4, "name": "mongo", "resource_type": "mongodb", "lifecycle_state": "active", "connection_status": "online"}
        client = SimpleNamespace(request=lambda *args: SimpleNamespace(payload=[engine]))
        self.assertEqual(ONLINE.validate_engine(client, 4, "mongo", "mongodb", ONLINE.time.monotonic() + 1)["engine_type"], "mongodb")
        with self.assertRaisesRegex(ONLINE.SuiteError, "distinct Engine"):
            ONLINE.run_mongodb_lineage(client, 4, 4, {}, {}, "run-name", 30, [])

    @staticmethod
    def execution_with_snapshots():
        return {"metadata": {"lineage_facts": {
            "schema_version": "addp.lineage-facts/v1",
            "inputs": [{"port": "source", "schema_snapshot": {"hash": "sha256:source", "fields": [{"name": "id"}]}}],
            "outputs": [{"port": "target", "schema_snapshot": {"hash": "sha256:target", "fields": [{"name": "id"}]}}],
            "operations": [{"field_lineage_status": "complete"}],
        }}}

    def test_owner_schemas_are_required_and_meta_must_use_the_same_snapshots(self):
        execution = self.execution_with_snapshots()
        self.assertEqual(ONLINE.execution_schema_hashes(execution), ("sha256:source", "sha256:target"))
        for resource in ("inputs", "outputs"):
            with self.subTest(resource=resource):
                execution = self.execution_with_snapshots()
                execution["metadata"]["lineage_facts"][resource][0]["schema_snapshot"]["fields"] = []
                with self.assertRaisesRegex(ONLINE.SuiteError, "frozen source/target schema"):
                    ONLINE.execution_schema_hashes(execution)
        execution = self.execution_with_snapshots()
        execution["metadata"]["lineage_facts"]["operations"][0]["field_lineage_status"] = "unavailable"
        with self.assertRaisesRegex(ONLINE.SuiteError, "complete field lineage"):
            ONLINE.execution_schema_hashes(execution)
        graph = self.field_graph()
        ONLINE.validate_graph_snapshots(graph, {5: "sha256:table-5", 7: "sha256:table-7", 11: "sha256:table-11"})
        with self.assertRaisesRegex(ONLINE.SuiteError, "frozen execution schemas"):
            ONLINE.validate_graph_snapshots(graph, {5: "wrong", 7: "sha256:table-7", 11: "sha256:table-11"})


if __name__ == "__main__":
    unittest.main()
