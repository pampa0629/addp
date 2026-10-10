import importlib.util
import json
import os
import sys
import copy
import struct
import time
import tempfile
import urllib.parse
import unittest
from pathlib import Path
from unittest.mock import Mock, patch


SCRIPT = Path(__file__).with_name("security-mysql-owner-protection-online.py")
SPEC = importlib.util.spec_from_file_location(
    "security_mysql_owner_protection_online", SCRIPT
)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class DefinitionProbeClient:
    def __init__(self) -> None:
        self.types = []
        self.baselines = []
        self.next_type_id = 100
        self.next_baseline_id = 200

    def request(self, method, path, expected, body=None):
        if method == "GET" and path == "/api/v1/security/sensitive-data-types":
            return ONLINE.SUPPORT.Response(200, list(self.types))
        if method == "GET" and path == "/api/v1/security/protection-baselines":
            return ONLINE.SUPPORT.Response(200, list(self.baselines))
        if method == "POST" and path == "/api/v1/security/sensitive-data-types":
            protection = body["default_protection"]
            if protection["algorithm"] != ONLINE.STRUCTURED_MASK_ALGORITHM:
                return ONLINE.SUPPORT.Response(400, {"error_code": "bad_request"})
            type_id = str(self.next_type_id)
            baseline_id = str(self.next_baseline_id)
            self.next_type_id += 1
            self.next_baseline_id += 1
            created = {"id": type_id, **body, "version": "1"}
            created.pop("default_protection")
            self.types.append(created)
            self.baselines.append(
                {
                    "id": baseline_id,
                    "sensitive_data_type_id": type_id,
                    "security_grade_id": str(body["default_security_grade_id"]),
                    **protection,
                    "enabled": True,
                    "version": "1",
                }
            )
            return ONLINE.SUPPORT.Response(201, created)
        if method == "DELETE" and path.startswith(
            "/api/v1/security/sensitive-data-types/"
        ):
            type_id = path.rsplit("/", 1)[1]
            self.types = [item for item in self.types if item["id"] != type_id]
            self.baselines = [
                item
                for item in self.baselines
                if item["sensitive_data_type_id"] != type_id
            ]
            return ONLINE.SUPPORT.Response(200, {"message": "deleted"})
        raise AssertionError(f"unexpected request: {method} {path} {expected} {body}")


class SpatialClient:
    def __init__(self, fail_owner=None):
        self.baseline = {"id": "41", "sensitive_data_type_id": "11", "security_grade_id": "21",
                         "version": "1", "effect": "mask", "algorithm": ONLINE.STRUCTURED_MASK_ALGORITHM,
                         "parameters": {"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"},
                         "allowed_algorithms": [ONLINE.STRUCTURED_MASK_ALGORITHM],
                         "invalid_value_effect": "deny", "enabled": True}
        self.original = copy.deepcopy(self.baseline)
        self.policies = []
        self.deleted_tasks = []
        self.service_deleted = False
        self.fail_owner = fail_owner
        self.owner_calls = []
        self.search_calls = []
        self.assessments = [{"id": field, "component_key": field, "current": {
            "conclusion": "sensitive", "sensitive_data_type_id": "11", "security_grade_id": "21"
        }} for field in ONLINE.SPATIAL_FIELDS[1:4]]

    def rows(self, owner):
        self.owner_calls.append((self.baseline["algorithm"], owner))
        if owner == self.fail_owner:
            raise ONLINE.SuiteError("injected owner failure")
        mask = {"1": "1*********8", "2": "张***c", "3": "a*c", "4": None, "5": None}
        constant = {key: None if key == "4" else "已脱敏" for key in ONLINE.EXPECTED_IDS}
        hashed = dict(ONLINE.SM3_VALUES)
        by_algorithm = {ONLINE.STRUCTURED_MASK_ALGORITHM: mask, ONLINE.CONSTANT_ALGORITHM: constant, ONLINE.SM3_ALGORITHM: hashed}
        columns = {field: by_algorithm[self.baseline["algorithm"]] for field in ONLINE.SPATIAL_FIELDS[1:4]}
        if owner == "manager" and any(policy["state"] == "active" for policy in self.policies):
            columns = {"value_a": {"1": "###########", "2": "#####", "3": "###", "4": None, "5": None},
                       "value_b": constant, "value_c": hashed}
        return [{"id": key, **{field: values[key] for field, values in columns.items()},
                 "location_point": f"POINT({100 + int(key)} {20 + int(key)})" if owner in {"manager", "transfer"} else
                     {"type": "Point", "coordinates": [100 + int(key), 20 + int(key)]}}
                for key in sorted(ONLINE.EXPECTED_IDS)]

    def request(self, method, path, expected, body=None):
        response = ONLINE.SUPPORT.Response
        if path.startswith("/api/v1/manager/search?"):
            query = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)
            assert query["q"] == ["value_c"] and query["engine_id"] == ["17"]
            self.search_calls.append(self.baseline["algorithm"])
            item = {"id": 1, "full_name": "addp_online_security." + ONLINE.SPATIAL_SOURCE, "item_type": "table"}
            return response(200, {"data": {"results": [{
                "document_id": "sha256:fixture", "engine_id": 17, "full_name": item["full_name"],
                "locator": ONLINE.build_item_locator(17, item), "field_matches": [
                    {"name": "value_c", "data_type": "string", "highlights": {"name": "<mark>value_c</mark>"}}
                ]
            }]}})
        if method == "GET" and path == "/api/v1/security/sensitive-data-types":
            return response(200, [{"id": "11", "code": "phone", "default_security_grade_id": "21"}])
        if path == "/api/v1/security/protection-baselines":
            return response(200, [copy.deepcopy(self.baseline)])
        if path == "/api/v1/security/protection-baselines/41":
            if method == "PUT":
                assert body["version"] == int(self.baseline["version"])
                self.baseline.update(copy.deepcopy(body), version=str(body["version"] + 1))
                for key in ("sensitive_data_type_id", "security_grade_id"):
                    self.baseline[key] = str(self.baseline[key])
            return response(200, copy.deepcopy(self.baseline))
        if path == "/api/v1/meta/engines/17/items":
            return response(200, [{"id": index + 1, "node_id": 9, "fingerprint": "sha256:fixture",
                                   "full_name": "addp_online_security." + name, "item_type": "table",
                                   "attributes": {"type_info": {"table": {"fields": [{"name": "location_point", "type": "geometry"}]}},
                                                  "capabilities": {"spatial": {"primary_geometry_column": "location_point", "geometry_columns": [{"name": "location_point", "geometry_type": "Point", "srid": 4326}]}}}}
                                  for index, name in enumerate((ONLINE.SPATIAL_SOURCE, ONLINE.SPATIAL_TARGET))])
        if path.startswith("/api/v1/security/protection-enrollments?"):
            return response(200, {"data": [{"id": "enrolled", "target_snapshot": {
                "engine_id": 17, "full_name": "addp_online_security." + ONLINE.SPATIAL_SOURCE}}], "total_pages": 1})
        if path == "/api/v1/security/protection-enrollments/enrolled":
            return response(200, {"id": "enrolled", "version": "1", "state": "active", "latest_source_snapshot_hash": "snapshot",
                                   "owner_progress": [{"consumer_owner": owner, "projection_state": "active", "acknowledged": True}
                                                      for owner in ONLINE.OWNER_ACTIONS]})
        if path == "/api/v1/security/protection-enrollments/enrolled/components":
            return response(200, {"data": [{"component": {"key": field, "value_type": "geometry" if field == "location_point" else "string"}}
                                           for field in ONLINE.SPATIAL_FIELDS[1:]]})
        if path.startswith("/api/v1/security/assessments?"):
            return response(200, {"data": self.assessments, "total_pages": 1})
        if method == "POST" and path == "/api/v1/security/assessments":
            assert body["enrollment_id"] == "enrolled" and body["enrollment_version"] == "1"
            assert body["component_key"] in ONLINE.SPATIAL_FIELDS[1:4]
            assessment = {"id": body["component_key"], "component_key": body["component_key"], "current": {
                "conclusion": "sensitive", "sensitive_data_type_id": body["sensitive_data_type_id"], "security_grade_id": body["security_grade_id"]}}
            self.assessments.append(assessment)
            return response(201, assessment)
        if path.startswith("/api/v1/security/protection-policies?"):
            return response(200, {"data": copy.deepcopy(self.policies), "total_pages": 1})
        if method == "POST" and path == "/api/v1/security/protection-policies":
            assert body["consumer_owner"] == "manager" and body["action"] == "preview"
            policy = {"id": str(len(self.policies) + 1), "state": "active", "version": "1", **body}
            self.policies.append(policy)
            return response(201, copy.deepcopy(policy))
        if path.startswith("/api/v1/security/protection-policies/"):
            policy = self.policies[int(path.rsplit("/", 1)[1]) - 1]
            if method == "DELETE":
                assert body["version"] == policy["version"]
                policy.update(state="revoked", version=str(int(policy["version"]) + 1))
            if method == "PUT":
                policy.update(body, state="active", version=str(int(policy["version"]) + 1))
            return response(200, copy.deepcopy(policy))
        if method == "POST" and path == "/api/v1/service/query":
            assert body["data_config"]["default_fields"] == ONLINE.SPATIAL_FIELDS
            return response(201, {"id": 701})
        if path == "/api/v1/service/query/701":
            if method == "DELETE":
                self.service_deleted = True
            return response(404 if self.service_deleted else 200, {"version": "1"})
        if path.startswith("/api/v1/manager/preview?"):
            locator = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)["locator"][0]
            owner = "transfer" if ONLINE.SPATIAL_TARGET in locator else "manager"
            return response(200, {"preview_type": "table", "data": {"columns": ONLINE.SPATIAL_FIELDS, "rows": self.rows(owner)}})
        if method == "POST" and path == "/api/v1/develop/executions":
            assert body["content"]["query"] == "SELECT id, value_a, value_b, value_c, location_point FROM addp_online_security.spatial_algorithm_source ORDER BY id"
            return response(200, {"execution_id": "query"})
        if path == "/api/v1/develop/executions/query":
            return response(200, {"status": "success", "metadata": {"result": {"summary": {"preview_rows": self.rows("develop")}}}})
        if path.startswith("/api/query/"):
            assert body["select"] == ONLINE.SPATIAL_FIELDS
            return response(200, {"data": self.rows("service")})
        raise AssertionError(f"unexpected spatial request {method} {path}")


class MongoClient(SpatialClient):
    def __init__(self):
        super().__init__()
        self.confirmed = False
        self.preview_calls = 0
        self.fail_preview = None
        self.item = {"id": 19, "node_id": 9, "fingerprint": "sha256:mongo-persons",
                     "full_name": ONLINE.MONGODB_SOURCE, "item_type": "collection"}

    def rows(self, owner):
        self.preview_calls += 1
        if self.preview_calls == self.fail_preview:
            raise ONLINE.SuiteError("injected MongoDB preview failure")
        by_algorithm = {
            ONLINE.STRUCTURED_MASK_ALGORITHM: {"1": "1*********8", "2": "张***c", "3": "a*c", "4": None, "5": None},
            ONLINE.CONSTANT_ALGORITHM: {"1": "已脱敏", "2": "已脱敏", "3": "已脱敏", "4": None, "5": "已脱敏"},
            ONLINE.SM3_ALGORITHM: dict(ONLINE.SM3_VALUES),
        }
        algorithm = next((policy["algorithm"] for policy in self.policies if policy["state"] == "active"), self.baseline["algorithm"])
        rows = []
        for key in "1234567":
            row = {"_id": key, "displayName": "person-" + key}
            if key != "7":
                row["userInfo"] = {"nickName": "nickname-" + key}
                value = by_algorithm[algorithm].get(key)
                if value is not None:
                    row["userInfo"]["phone"] = value
            rows.append(row)
        return rows

    def request(self, method, path, expected, body=None):
        response = ONLINE.SUPPORT.Response
        if path == "/api/v1/meta/engines/29/items":
            return response(200, [copy.deepcopy(self.item)])
        if path.startswith("/api/v1/security/protection-enrollments?"):
            return response(200, {"data": [{"id": "enrolled", "target_snapshot": {
                "engine_id": 29, "full_name": ONLINE.MONGODB_SOURCE}}], "total_pages": 1})
        if path == "/api/v1/security/protection-enrollments/enrolled/components":
            return response(200, {"data": [{"component": {"key": ONLINE.MONGODB_FIELD, "value_type": "string"}}]})
        if path.startswith("/api/v1/security/findings?"):
            return response(200, {"data": [{"id": "phone-finding", "detector_version": ONLINE.PHONE_DETECTOR,
                "component": {"key": ONLINE.MONGODB_FIELD}, "review": {} if self.confirmed else None}], "total_pages": 1})
        if path.startswith("/api/v1/security/assessments?"):
            return response(200, {"data": [{"id": "phone-assessment", "current": {
                "conclusion": "sensitive", "component": {"key": ONLINE.MONGODB_FIELD}}}] if self.confirmed else [], "total_pages": 1})
        if path == "/api/v1/security/findings/phone-finding/reviews":
            assert body["decision"] == "confirm" and method == "POST"
            self.confirmed = True
            return response(201, {"assessment": {"id": "phone-assessment"}})
        if path.startswith("/api/v1/manager/preview?"):
            return response(200, {"preview_type": "table", "data": {"columns": ["_id", "displayName", "userInfo"], "rows": self.rows("manager")}})
        if path.startswith("/api/v1/manager/search?"):
            query = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)
            assert query["q"] == [ONLINE.MONGODB_FIELD] and query["engine_id"] == ["29"]
            return response(200, {"data": {"results": [{"document_id": self.item["fingerprint"], "engine_id": 29,
                "full_name": ONLINE.MONGODB_SOURCE, "locator": ONLINE.build_item_locator(29, self.item),
                "field_matches": [{"name": ONLINE.MONGODB_FIELD, "data_type": "string",
                    "highlights": {"name": "userInfo.<mark>phone</mark>"}}]}]}})
        return super().request(method, path, expected, body)


class MongoAlgorithmTest(unittest.TestCase):
    def run_mongo(self, client):
        process = RestartRecoveryTest().process
        with tempfile.TemporaryDirectory() as directory, \
             patch.dict(os.environ, {"ADDP_ONLINE_ARTIFACT_DIR": directory, "ADDP_ONLINE_TEST_TENANT_ID": "2"}), \
             patch.object(ONLINE, "require_hosted_restart"), \
             patch.object(ONLINE, "validate_user_identity", return_value={"principal_id": "7"}), \
             patch.object(ONLINE, "ready_process", side_effect=[process(), process(), process(True), process(True)]), \
             patch.object(ONLINE.subprocess, "run", return_value=Mock(returncode=0)), \
             patch.object(ONLINE, "wait_for_scan", return_value="mongo-scan") as scan:
            result = ONLINE.exercise_mongodb_algorithms(client, 29, time.monotonic() + 60, 60)
            scan.assert_called_once()
            return result

    def test_three_algorithms_nested_policy_and_restart_restore_without_rescan(self):
        client = MongoClient()
        report = self.run_mongo(client)
        self.assertTrue(client.confirmed)
        self.assertEqual([case["algorithm"] for case in report["cases"]], [case[0] for case in ONLINE.ALGORITHM_CASES])
        self.assertEqual(report["component_key"], "userInfo.phone")
        self.assertEqual(report["finding_id"], "phone-finding")
        self.assertEqual(report["assessment_id"], "phone-assessment")
        self.assertTrue(report["all_owner_projections_acknowledged"])
        restart = report["cases"][0]["restart_recovery"]
        self.assertTrue(restart["verified_before_rescan"])
        self.assertEqual(restart["technical_field_search"]["document_id"], "sha256:mongo-persons")
        self.assertEqual(restart["protected_preview"]["rows"], 7)
        self.assertTrue(report["cases"][0]["baseline_after_policy_revocation"]["sparse_objects_verified"])
        self.assertEqual(client.policies[0]["state"], "revoked")
        self.assertEqual({k: v for k, v in client.baseline.items() if k != "version"},
                         {k: v for k, v in client.original.items() if k != "version"})

    def test_preview_failures_restore_baseline_and_revoke_created_policy(self):
        for fail_at in (1, 2, 3, 4, 5, 6):
            client = MongoClient(); client.fail_preview = fail_at
            with self.subTest(fail_at=fail_at), self.assertRaisesRegex(ONLINE.SuiteError, "injected MongoDB"):
                self.run_mongo(client)
            self.assertFalse(any(policy["state"] == "active" for policy in client.policies))
            self.assertEqual({k: v for k, v in client.baseline.items() if k != "version"},
                             {k: v for k, v in client.original.items() if k != "version"})

    def test_rejects_plaintext_null_leak_flattened_copy_and_sibling_changes(self):
        for mutation in ("plaintext", "null", "sibling", "parent", "flattened", "flattened_sparse", "missing", "duplicate"):
            rows = MongoClient().rows("manager")
            # The client's initial 3/4 Baseline uses the independent 1/1 fake vector;
            # validate using that explicit expected vector rather than production code.
            expected = {"1": "1*********8", "2": "张***c", "3": "a*c", "4": None, "5": None}
            if mutation == "plaintext": rows[0]["userInfo"]["phone"] = "13812345678"
            if mutation == "null": rows[3]["userInfo"]["phone"] = None
            if mutation == "sibling": rows[0]["userInfo"]["nickName"] = "changed"
            if mutation == "parent": rows[6]["userInfo"] = {}
            if mutation == "flattened": rows[0]["userInfo.phone"] = "13812345678"
            if mutation == "flattened_sparse": rows[6]["userInfo__phone"] = "13812345678"
            if mutation == "missing": rows.pop()
            if mutation == "duplicate": rows[-1] = rows[0]
            with self.subTest(mutation=mutation), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_mongodb_rows(rows, "manager", expected)


class RestartRecoveryTest(unittest.TestCase):
    def process(self, newer=False):
        return {"instance_id": "00000000-0000-4000-8000-00000000000" + ("2" if newer else "1"),
                "started_at": "2026-10-10T00:0" + ("2" if newer else "1") + ":00Z",
                "build_id": "fixture-build", "git_commit": "fixture-commit", "source_fingerprint": "sha256:fixture"}

    def run_recovery(self, client, restart=None, after=None, identity=None):
        source = client.request("GET", "/api/v1/meta/engines/17/items", (200,)).payload[0]
        client.policies = [{"id": str(index), "state": "active", "version": "1"} for index in range(1, 4)]
        expected = {"value_a": {"1": "###########", "2": "#####", "3": "###", "4": None, "5": None},
                    "value_b": ONLINE.spatial_expected(ONLINE.CONSTANT_ALGORITHM, {"value": "已脱敏"}),
                    "value_c": dict(ONLINE.SM3_VALUES)}
        client.baseline["algorithm"] = ONLINE.SM3_ALGORITHM
        with tempfile.TemporaryDirectory() as directory, \
             patch.dict(os.environ, {"ADDP_ONLINE_ARTIFACT_DIR": directory, "ADDP_ONLINE_TEST_TENANT_ID": "2"}), \
             patch.object(ONLINE, "require_hosted_restart"), \
             patch.object(ONLINE, "validate_user_identity", side_effect=identity or [{"principal_id": "7"}] * 2), \
             patch.object(ONLINE, "ready_process", side_effect=[self.process(), self.process(), after or self.process(True), self.process(True)]), \
             patch.object(ONLINE.subprocess, "run", side_effect=restart, return_value=Mock(returncode=0)) as lifecycle, \
             patch.object(ONLINE, "wait_for_scan", side_effect=AssertionError("restart must not rescan")):
            result = ONLINE.exercise_restart_recovery(client, 17, source, "enrolled", "41", ["1", "2", "3"], expected, time.monotonic() + 60)
            self.assertEqual(lifecycle.call_args.args[0], ["bash", "scripts/dev/restart.sh", "-security", "-manager"])
            return result

    def test_accepts_new_processes_same_identity_and_protected_values_without_rescan(self):
        result = self.run_recovery(SpatialClient())
        self.assertTrue(result["same_user_verified"])
        self.assertTrue(result["definitions_preserved"])
        self.assertTrue(result["verified_before_rescan"])
        self.assertEqual(result["protected_preview"]["rows"], 5)
        self.assertTrue(result["technical_field_search"]["same_item_verified"])

    def test_rejects_noop_restart_stale_start_and_changed_build(self):
        for field, value in (("instance_id", self.process()["instance_id"]),
                             ("started_at", self.process()["started_at"]),
                             ("build_id", "another-build"), ("git_commit", "another-commit"),
                             ("source_fingerprint", "another-source")):
            with self.subTest(field=field), self.assertRaises(ONLINE.SuiteError):
                self.run_recovery(SpatialClient(), after=dict(self.process(True), **{field: value}))

    def test_rejects_lifecycle_failure_and_timeout_without_reporting_recovery(self):
        for error in (None, ONLINE.subprocess.TimeoutExpired("restart", 60)):
            with self.subTest(error=error), self.assertRaisesRegex(ONLINE.SuiteError, "lifecycle"):
                self.run_recovery(SpatialClient(), restart=(
                    (lambda *args, **kwargs: Mock(returncode=1)) if error is None else error))

    def test_rejects_changed_user_or_definition(self):
        with self.assertRaisesRegex(ONLINE.SuiteError, "ordinary User"):
            self.run_recovery(SpatialClient(), identity=[{"principal_id": "7"}, {"principal_id": "8"}])
        for target in ("baseline", "policy"):
            client = SpatialClient()
            def mutate(*args, **kwargs):
                (client.baseline if target == "baseline" else client.policies[0])["version"] = "999"
                return Mock(returncode=0)
            with self.subTest(target=target), self.assertRaisesRegex(ONLINE.SuiteError, "Baseline or field Policies"):
                self.run_recovery(client, restart=mutate)

    def test_rejects_plaintext_after_restart(self):
        client = SpatialClient()
        original = client.rows
        def leak(owner):
            rows = original(owner)
            rows[0]["value_c"] = "13812345678"
            return rows
        client.rows = leak
        with self.assertRaisesRegex(ONLINE.SuiteError, "protected value"):
            self.run_recovery(client)

    def test_admission_refuses_personal_environment_before_invoking_lifecycle(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(ONLINE.subprocess, "run") as lifecycle:
            with self.assertRaises(ONLINE.SuiteError):
                ONLINE.exercise_restart_recovery(Mock(), 17, {}, "enrolled", "41", [], {}, time.monotonic() + 60)
            lifecycle.assert_not_called()

    def test_admission_requires_hosted_architecture_no_env_and_external_directories(self):
        with tempfile.TemporaryDirectory() as directory:
            repository = Path(directory) / "repository"
            repository.mkdir()
            environment = dict(GITHUB_ACTIONS="true", RUNNER_OS="Linux", ADDP_ONLINE_HOSTED="1", ADDP_ONLINE_HOST="1",
                               ADDP_ONLINE_TEST="1", POSTGRES_DB="addp_online", SECURITY_URL="http://127.0.0.1:8194",
                               MANAGER_URL="http://127.0.0.1:8081", ADDP_ONLINE_SECRET_DIR=directory + "/secret",
                               ADDP_ONLINE_ARTIFACT_DIR=directory + "/artifact")
            with patch.dict(os.environ, environment, clear=True), patch.object(ONLINE.sys, "platform", "linux"), \
                 patch.object(ONLINE.os, "uname", return_value=Mock(machine="x86_64")):
                ONLINE.require_hosted_restart(repository)
                for key, value in (("MANAGER_URL", "http://localhost:8081"),
                                   ("ADDP_ONLINE_ARTIFACT_DIR", str(repository / "artifacts")),
                                   ("ADDP_ONLINE_SECRET_DIR", "relative")):
                    with self.subTest(key=key), patch.dict(os.environ, {key: value}), self.assertRaises(ONLINE.SuiteError):
                        ONLINE.require_hosted_restart(repository)
                (repository / ".env").touch()
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.require_hosted_restart(repository)
                (repository / ".env").unlink()
                with patch.object(ONLINE.sys, "platform", "darwin"), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.require_hosted_restart(repository)

    def test_ready_requires_registered_backend_and_complete_process_identity(self):
        payload = dict(self.process(), module="manager", status="ready", role="backend", registration_state="registered")
        with patch.dict(os.environ, {"MANAGER_URL": "http://127.0.0.1:8081"}), patch.object(ONLINE, "GatewayClient") as health:
            health.return_value.request.return_value = ONLINE.SUPPORT.Response(200, payload)
            self.assertEqual(ONLINE.ready_process("manager"), self.process())
            self.assertEqual(health.call_args.args[1], "")
            for key, value in (("registration_state", "recovering"), ("instance_id", "invalid"),
                               ("started_at", "2026-10-10T00:00:00"), ("build_id", ""), ("module", "meta")):
                health.return_value.request.return_value = ONLINE.SUPPORT.Response(200, dict(payload, **{key: value}))
                with self.subTest(key=key), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.ready_process("manager")

    def test_restart_failure_still_restores_owned_rules(self):
        client = SpatialClient()
        with patch.object(ONLINE, "exercise_restart_recovery", side_effect=ONLINE.SuiteError("restart failed")), \
             patch.object(ONLINE, "cleanup_tasks"):
            with self.assertRaisesRegex(ONLINE.SuiteError, "restart failed"):
                ONLINE.exercise_spatial_algorithms(client, 17, "run-1", time.monotonic() + 30, 90)
        self.assertTrue(client.service_deleted)
        self.assertTrue(all(policy["state"] == "revoked" for policy in client.policies))
        self.assertEqual(client.baseline["parameters"], client.original["parameters"])


class SecurityMySQLOwnerProtectionOnlineTest(unittest.TestCase):
    def spatial_run(self, client):
        def create_task(client, payload, deadline, tasks):
            self.assertEqual(payload["config"]["transforms"], [])
            self.assertEqual(payload["config"]["source"]["representation"], "native")
            self.assertEqual(payload["config"]["target"]["name"], ONLINE.SPATIAL_TARGET)
            tasks.append(800 + len(tasks))
            return tasks[-1], {"records_written": 5}
        with patch.object(ONLINE, "wait_for_scan", return_value="scan"), \
             patch.object(ONLINE, "create_and_run_task", side_effect=create_task), \
             patch.object(ONLINE, "exercise_restart_recovery", return_value={"verified_before_rescan": True}) as restart, \
             patch.object(ONLINE, "cleanup_tasks", side_effect=lambda client, tasks: client.deleted_tasks.extend(tasks)):
            result = ONLINE.exercise_spatial_algorithms(client, 17, "run-1", time.monotonic() + 30, 90)
            restart.assert_called_once()
            self.assertEqual(restart.call_args.args[5], ["1", "2", "3"])
            return result

    def test_spatial_algorithms_cover_four_owners_and_restore_baseline(self):
        client = SpatialClient()
        evidence = self.spatial_run(client)
        self.assertEqual(len(evidence["cases"]), 3)
        self.assertEqual({owner for _, owner in client.owner_calls}, set(ONLINE.OWNER_ACTIONS))
        self.assertEqual(len(client.owner_calls), 12)
        self.assertTrue(evidence["cases"][0]["independent_manager_fields"])
        self.assertTrue(evidence["cases"][0]["owners"]["manager"]["restart_recovery"]["verified_before_rescan"])
        self.assertEqual(client.search_calls, [case[0] for case in ONLINE.ALGORITHM_CASES])
        for case in evidence["cases"]:
            self.assertTrue(case["owners"]["manager"]["technical_field_search"]["same_item_verified"])
        self.assertTrue(all(policy["state"] == "revoked" for policy in client.policies))
        self.assertTrue(client.service_deleted)
        self.assertEqual(len(client.deleted_tasks), 3)
        self.assertEqual({k: v for k, v in client.baseline.items() if k != "version"},
                         {k: v for k, v in client.original.items() if k != "version"})

    def test_owner_failure_still_restores_phone_rule_and_revokes_all_policies(self):
        client = SpatialClient(fail_owner="service")
        with self.assertRaisesRegex(ONLINE.SuiteError, "injected owner failure"):
            self.spatial_run(client)
        self.assertEqual(client.baseline["parameters"], client.original["parameters"])
        self.assertEqual(client.baseline["invalid_value_effect"], "deny")
        self.assertEqual(client.baseline["allowed_algorithms"], client.original["allowed_algorithms"])
        self.assertTrue(client.service_deleted)
        self.assertEqual(len(client.policies), 3)
        self.assertTrue(all(policy["state"] == "revoked" for policy in client.policies))

    def test_spatial_assertions_reject_plaintext_duplicate_rows_and_changed_geometry(self):
        client = SpatialClient()
        client.baseline["algorithm"] = ONLINE.SM3_ALGORITHM
        rows = client.rows("transfer")
        expected = {field: dict(ONLINE.SM3_VALUES) for field in ONLINE.SPATIAL_FIELDS[1:4]}
        rows[0]["value_a"] = "13812345678"
        with self.assertRaisesRegex(ONLINE.SuiteError, "protected value"):
            ONLINE.assert_spatial_rows(rows, "transfer", expected)
        rows = client.rows("transfer")
        rows[-1] = rows[0]
        with self.assertRaisesRegex(ONLINE.SuiteError, "duplicated"):
            ONLINE.assert_spatial_rows(rows, "transfer", expected)
        rows = client.rows("transfer")
        rows[0]["location_point"] = "POINT(0 0)"
        with self.assertRaisesRegex(ONLINE.SuiteError, "coordinates"):
            ONLINE.assert_spatial_rows(rows, "transfer", expected)

    def test_point_coordinates_support_native_ewkb_and_reject_wrong_srid(self):
        raw = struct.pack("<BIIdd", 1, 0x20000001, 4326, 101, 21)
        self.assertEqual(ONLINE.point_coordinates(raw.hex()), (101, 21))
        raw = struct.pack("<BIIdd", 1, 0x20000001, 3857, 101, 21)
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.point_coordinates(raw.hex())

    def test_point_coordinates_accept_current_manager_wkt_and_reject_invalid_geometry(self):
        for text, expected in (("POINT(101 21)", (101, 21)),
                               ("POINT(-101.5 2.1e1)", (-101.5, 21))):
            with self.subTest(text=text):
                self.assertEqual(ONLINE.point_coordinates(text), expected)
        for text in ("POINT Z(101 21 5)", "POINT(101 21 5)", "POINT EMPTY",
                     "POINT(nan 21)", "POINT(101 inf)", "LINESTRING(101 21,102 22)",
                     "POINT(101 21)extra"):
            with self.subTest(text=text), self.assertRaises(ONLINE.SuiteError):
                ONLINE.point_coordinates(text)

    def test_cleanup_refuses_to_overwrite_external_baseline_change_but_deletes_service(self):
        client = SpatialClient()
        client.baseline.update(version="99", parameters={"value": "external"})
        with self.assertRaisesRegex(ONLINE.SuiteError, "refusing to overwrite"):
            with patch.object(ONLINE, "cleanup_tasks", side_effect=lambda client, tasks: client.deleted_tasks.extend(tasks)):
                ONLINE.restore_algorithm_state(client, "41", ONLINE.baseline_body(client.original), [], 701, [801], "enrolled", 90, 1)
        self.assertEqual(client.baseline["parameters"], {"value": "external"})
        self.assertTrue(client.service_deleted)
        self.assertEqual(client.deleted_tasks, [801])

    def test_termination_is_caught_by_owned_resource_cleanup(self):
        client = SpatialClient()
        with patch.object(ONLINE, "service_rows", side_effect=lambda *args: ONLINE.handle_termination(15, None)):
            with self.assertRaisesRegex(ONLINE.SuiteError, "interrupted"):
                self.spatial_run(client)
        self.assertTrue(client.service_deleted)
        self.assertTrue(all(policy["state"] == "revoked" for policy in client.policies))
        self.assertEqual(client.baseline["parameters"], client.original["parameters"])

    def test_second_policy_write_failure_revokes_the_first_policy(self):
        client = SpatialClient()
        original_request = client.request
        def request(method, path, expected, body=None):
            if method == "POST" and path == "/api/v1/security/protection-policies" and client.policies:
                raise ONLINE.SuiteError("second Policy failed")
            return original_request(method, path, expected, body)
        client.request = request
        with self.assertRaisesRegex(ONLINE.SuiteError, "second Policy failed"):
            self.spatial_run(client)
        self.assertEqual(len(client.policies), 1)
        self.assertEqual(client.policies[0]["state"], "revoked")
        self.assertTrue(client.service_deleted)
        self.assertEqual(client.baseline["parameters"], client.original["parameters"])

    def test_active_policy_is_rejected_before_baseline_mutation(self):
        client = SpatialClient()
        client.policies.append({"id": "1", "state": "active", "assessment_id": "value_a", "version": "1"})
        with self.assertRaisesRegex(ONLINE.SuiteError, "stale active Policy"):
            self.spatial_run(client)
        self.assertEqual(client.baseline, client.original)
        self.assertFalse(client.service_deleted)

    def test_first_run_creates_assessments_from_meta_components(self):
        client = SpatialClient()
        client.assessments = []
        self.spatial_run(client)
        self.assertEqual({item["component_key"] for item in client.assessments}, set(ONLINE.SPATIAL_FIELDS[1:4]))

    def test_next_run_reuses_revoked_policies_and_preserves_audit_history(self):
        client = SpatialClient()
        self.spatial_run(client)
        client.owner_calls.clear()
        self.spatial_run(client)
        self.assertEqual(len(client.policies), 3)
        self.assertTrue(all(policy["state"] == "revoked" and int(policy["version"]) > 2 for policy in client.policies))
        self.assertEqual(len(client.owner_calls), 12)

    def test_target_spatial_metadata_rejects_json_type_and_lost_srid(self):
        item = SpatialClient().request("GET", "/api/v1/meta/engines/17/items", (200,)).payload[1]
        ONLINE.assert_spatial_item(item)
        item["attributes"]["type_info"]["table"]["fields"][0]["type"] = "json"
        with self.assertRaisesRegex(ONLINE.SuiteError, "geometry field type"):
            ONLINE.assert_spatial_item(item)
        item["attributes"]["type_info"]["table"]["fields"][0]["type"] = "geometry"
        item["attributes"]["capabilities"]["spatial"]["geometry_columns"][0]["srid"] = 0
        with self.assertRaisesRegex(ONLINE.SuiteError, "SRID 4326"):
            ONLINE.assert_spatial_item(item)

    def test_owner_actions_cover_the_four_projection_bindings(self) -> None:
        self.assertEqual(
            ONLINE.OWNER_ACTIONS,
            {
                "manager": "preview",
                "develop": "query",
                "service": "service_execute",
                "transfer": "export",
            },
        )

    def test_governance_requires_email_detector_and_suppress_baseline(self) -> None:
        client = Mock()
        client.request.side_effect = [
            ONLINE.SUPPORT.Response(
                200,
                [
                    {
                        "id": "11",
                        "code": "email",
                        "security_classification_id": "12",
                        "default_security_grade_id": "21",
                    }
                ],
            ),
            ONLINE.SUPPORT.Response(
                200,
                [
                    {
                        "id": "31",
                        "capability_key": ONLINE.EMAIL_DETECTOR,
                        "sensitive_data_type_id": "11",
                        "enabled": True,
                    }
                ],
            ),
            ONLINE.SUPPORT.Response(
                200,
                [
                    {
                        "id": "41",
                        "sensitive_data_type_id": "11",
                        "security_grade_id": "21",
                        "effect": "suppress",
                        "enabled": True,
                    }
                ],
            ),
        ]

        governance = ONLINE.validate_governance(client)

        self.assertEqual(
            governance,
            {
                "sensitive_data_type_id": "11",
                "security_classification_id": "12",
                "security_grade_id": "21",
                "detector_id": "31",
                "baseline_id": "41",
                "effect": "suppress",
            },
        )

    def test_default_protection_probe_is_atomic_and_leaves_no_resources(self) -> None:
        client = DefinitionProbeClient()

        evidence = ONLINE.exercise_default_protection_transaction(
            client, 12, 21
        )

        self.assertEqual(evidence["effect"], "mask")
        self.assertEqual(evidence["algorithm"], ONLINE.STRUCTURED_MASK_ALGORITHM)
        self.assertEqual(evidence["invalid_request_status"], 400)
        self.assertTrue(evidence["rollback_verified"])
        self.assertTrue(evidence["cleanup_verified"])
        self.assertEqual(client.types, [])
        self.assertEqual(client.baselines, [])
        self.assertIn("security.sensitive_data_type.create", ONLINE.REQUIRED_PERMISSIONS)
        self.assertIn("security.sensitive_data_type.delete", ONLINE.REQUIRED_PERMISSIONS)

    def test_email_suppression_keeps_all_non_sensitive_rows(self) -> None:
        evidence = ONLINE.assert_email_suppressed(
            [{"id": value, "customer_code": f"C-{value}"} for value in range(1, 6)],
            "manager",
            ["id", "customer_code"],
        )

        self.assertEqual(evidence, {"rows": 5, "email_field_present": False})
        with self.assertRaisesRegex(ONLINE.SuiteError, "exposed"):
            ONLINE.assert_email_suppressed(
                [{"id": value, "email": None} for value in range(1, 6)],
                "manager",
                ["id", "email"],
            )

    def test_transfer_uses_native_source_without_a_field_dropping_transform(self) -> None:
        payload = ONLINE.transfer_payload("test", "source", "target")
        config = payload["config"]

        self.assertEqual(config["runtime"], {"boundary": "bounded"})
        self.assertNotIn("query", config["source"])
        self.assertEqual(config["transforms"], [])
        self.assertEqual(config["target"]["policy"], {"apply_mode": "replace"})

    def test_table_service_payload_leaves_stable_key_to_service_snapshot(self) -> None:
        payload = ONLINE.service_payload(
            "online-service", 17, "addp://engine/17/path/public/customers"
        )

        self.assertEqual(
            payload["data_config"]["locator"],
            "addp://engine/17/path/public/customers",
        )
        self.assertNotIn("stable_key", payload["data_config"])


class ProtectedTechnicalFieldSearchTest(unittest.TestCase):
    def setUp(self):
        self.item = {"id": 12, "fingerprint": "source-fingerprint", "full_name": "public.protected", "item_type": "table"}
        self.hit = {"document_id": "source-fingerprint", "engine_id": 17, "full_name": "public.protected",
                    "locator": "addp://engine/17/path/public/protected?item_id=12&type=table",
                    "field_matches": [{"name": "value_c", "data_type": "string", "highlights": {"name": "<mark>value_c</mark>"}}]}

    def search(self, hits):
        client = Mock()
        client.request.return_value = ONLINE.SUPPORT.Response(200, {"data": {"results": hits}})
        return ONLINE.wait_for_technical_field(client, 17, self.item, "value_c", time.monotonic() + 5)

    def test_accepts_registered_identity_and_real_field_highlight(self):
        evidence = self.search([self.hit])
        self.assertTrue(evidence["same_item_verified"])
        self.assertEqual(evidence["document_id"], self.item["fingerprint"])

    def test_rejects_cross_engine_duplicate_identity_wrong_locator_and_invented_field_matches(self):
        for changes in (
            {"engine_id": 19}, {"full_name": "public.other"},
            {"locator": "addp://engine/17/path/public/protected?item_id=99&type=table"},
            {"locator": "addp://engine/17/path/public/protected?type=table"},
            {"field_matches": []},
            {"field_matches": [{"name": "value_c", "data_type": "json", "highlights": {"name": "<mark>value_c</mark>"}}]},
            {"field_matches": [{"name": "value_c", "data_type": "string", "highlights": {"name": "value_c"}}]},
            {"field_matches": [{"name": "value_c", "data_type": "string", "highlights": {"name": "<mark>other</mark>"}}]},
        ):
            with self.subTest(changes=changes), self.assertRaises(ONLINE.SuiteError):
                self.search([dict(self.hit, **changes)])
        with self.assertRaisesRegex(ONLINE.SuiteError, "duplicated"):
            self.search([self.hit, self.hit])

    def test_only_transient_isolation_is_retried_and_missing_index_cannot_pass(self):
        client = Mock()
        client.request.side_effect = [ONLINE.SUPPORT.Response(503, {"error_code": "manager_search_isolated"}),
                                     ONLINE.SUPPORT.Response(200, {"data": {"results": [self.hit]}})]
        with patch.object(ONLINE.time, "sleep"):
            self.assertTrue(ONLINE.wait_for_technical_field(client, 17, self.item, "value_c", time.monotonic() + 5)["same_item_verified"])
        client.request.side_effect = None
        for response in (ONLINE.SUPPORT.Response(503, {"error_code": "search_unconfigured"}),
                         ONLINE.SUPPORT.Response(200, {"data": {"results": []}})):
            client.request.return_value = response
            with patch.object(ONLINE.time, "monotonic", side_effect=[1, 1, 3]), patch.object(ONLINE.time, "sleep"):
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.wait_for_technical_field(client, 17, self.item, "value_c", 2)


class ManagerExportBrowserTest(unittest.TestCase):
    def test_main_waits_for_protected_email_search_before_starting_browser(self):
        environment = {
            'ADDP_ONLINE_TEST': '1', 'ADDP_ONLINE_TEST_TENANT_ID': '2',
            'ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID': '23', 'ADDP_ONLINE_TEST_ENGINE_ID': '17',
            'ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE': 'security_fixture',
            'ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID': '29',
            'ADDP_ONLINE_TEST_RUN_ID': 'run-42', 'GATEWAY_URL': 'http://127.0.0.1:8000',
            'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'fixture-token',
        }
        for unavailable in (False, True):
            with (
                self.subTest(unavailable=unavailable),
                patch.dict(os.environ, environment, clear=True),
                patch.object(sys, 'argv', [str(SCRIPT)]),
                patch.object(ONLINE.signal, 'signal'),
                patch.object(ONLINE, 'require_hosted_restart'),
                patch.object(ONLINE, 'run_scenario', return_value={}),
                patch.object(ONLINE, 'exercise_mongodb_algorithms', return_value={'cases': []}),
                patch.object(ONLINE, 'find_item', return_value={'fingerprint': 'source'}),
                patch.object(ONLINE, 'build_item_locator', return_value='source-locator'),
                patch.object(ONLINE, 'wait_for_technical_field') as readiness,
                patch.object(ONLINE, 'run_export_browser') as browser,
                patch('builtins.print'),
            ):
                readiness.return_value = {'same_item_verified': True}
                if unavailable:
                    readiness.side_effect = ONLINE.SuiteError('protected field not searchable')
                    with self.assertRaises(ONLINE.SuiteError):
                        ONLINE.main()
                    browser.assert_not_called()
                else:
                    def launch(*args):
                        readiness.assert_called_once()
                        return self.report()
                    browser.side_effect = launch
                    self.assertEqual(ONLINE.main(), 0)
                    self.assertEqual(readiness.call_args.args[1:4], (23, {'fingerprint': 'source'}, 'email'))

    def report(self):
        return {
            "schema_version": "addp.security-manager-export-browser/v3", "result": "passed",
            "run_id": "run-42", "tenant_id": "2", "records": 5,
            "execution_id": "53834320-203b-4d8c-838e-15e01024d484",
            "email_field_present": False, "non_sensitive_fields_preserved": True,
            "same_user_verified": True, "initiator_verified": True, "taskless_execution": True,
            "manager_source_verified": True, "monitor_detail_visible": True,
            "protected_preview_verified": True, "technical_field_search_verified": True,
            "search_to_preview_verified": True,
            "browser_warning_errors": 0, "failed_business_responses": 0, "anonymous_refresh_401": 1,
        }

    def test_rejects_missing_provenance_hidden_detail_wrong_run_and_browser_errors(self):
        valid = self.report()
        self.assertEqual(ONLINE.validate_export_browser_report(valid, "run-42", "2"), valid)
        for key, value in (
            ("schema_version", "old"), ("run_id", "other"), ("tenant_id", "9"),
            ("execution_id", "invalid"), ("execution_id", "00000000-0000-0000-0000-000000000000"),
            ("access_token", "unexpected"), ("records", 4), ("email_field_present", True),
            ("non_sensitive_fields_preserved", False), ("same_user_verified", False),
            ("initiator_verified", False), ("taskless_execution", False),
            ("manager_source_verified", False), ("monitor_detail_visible", False),
            ("protected_preview_verified", False), ("technical_field_search_verified", False),
            ("search_to_preview_verified", False),
            ("browser_warning_errors", 1), ("failed_business_responses", 1),
            ("anonymous_refresh_401", 0), ("anonymous_refresh_401", 2), ("anonymous_refresh_401", True),
            ("schema_version", "addp.security-manager-export-browser/v2"),
            ("initiator_verified", 1), ("browser_warning_errors", False),
        ):
            with self.subTest(key=key, value=value), self.assertRaises(ONLINE.SuiteError):
                ONLINE.validate_export_browser_report(dict(valid, **{key: value}), "run-42", "2")
        for key in valid:
            incomplete = dict(valid); incomplete.pop(key)
            with self.subTest(missing=key), self.assertRaises(ONLINE.SuiteError):
                ONLINE.validate_export_browser_report(incomplete, "run-42", "2")

    def test_browser_failure_and_missing_report_cannot_reuse_previous_success(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = Path(directory)
            path = artifact / "security-manager-export-browser.json"
            environment = dict(ADDP_ONLINE_ARTIFACT_DIR=directory, ADDP_ONLINE_TEST_RUN_ID="run-42",
                ADDP_ONLINE_TEST_TENANT_ID="2", ADDP_ONLINE_TEST_USER_ACCESS_TOKEN="test-token",
                ADDP_ONLINE_TEST_USER_USERNAME="test-user", ADDP_ONLINE_TEST_USER_PASSWORD="test-password",
                GATEWAY_URL="http://127.0.0.1:8000", CONSOLE_URL="http://127.0.0.1:5170")
            for exit_code in (1, 0):
                path.write_text(json.dumps(self.report()))
                with patch.object(ONLINE.subprocess, "run", return_value=Mock(returncode=exit_code, stdout="", stderr="")):
                    with self.subTest(exit_code=exit_code), self.assertRaises(ONLINE.SuiteError):
                        ONLINE.run_export_browser(artifact, environment, "source")
                self.assertFalse(path.exists())
            def browser(command, **kwargs):
                self.assertIn("e2e/online/security-manager-export.spec.js", command)
                self.assertEqual(kwargs["env"]["ADDP_ONLINE_SECURITY_EXPORT_LOCATOR"], "source")
                path.write_text(json.dumps(self.report()))
                for name in ("security-manager-protected-preview.png", "security-manager-field-search.png",
                             "security-manager-export.png", "security-manager-export-monitor.png"):
                    (artifact / name).write_bytes(b"test screenshot")
                return Mock(returncode=0, stdout="", stderr="")
            with patch.object(ONLINE.subprocess, "run", side_effect=browser):
                self.assertEqual(ONLINE.run_export_browser(artifact, environment, "source"), self.report())

            for missing in ("security-manager-protected-preview.png", "security-manager-field-search.png"):
                def incomplete_browser(command, **kwargs):
                    result = browser(command, **kwargs)
                    (artifact / missing).unlink()
                    return result
                with self.subTest(missing=missing), patch.object(ONLINE.subprocess, "run", side_effect=incomplete_browser):
                    with self.assertRaisesRegex(ONLINE.SuiteError, "screenshots"):
                        ONLINE.run_export_browser(artifact, environment, "source")


class HostedGovernanceInitializationTest(unittest.TestCase):
    def source_clients(self):
        authorizer = Mock()
        authorizer.request.side_effect = lambda method, path, expected, body=None: ONLINE.SUPPORT.Response(
            200, {"principal": {"type": "user", "id": "8"},
                  "context": {"type": "tenant", "tenant_id": "2"},
                  "authorization": {"role_assignments": [{"permissions": sorted(ONLINE.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS)}]}}) if method == "GET" else ONLINE.SUPPORT.Response(
                      201, dict(body, engine_id=str(body["catalog_path"]["engine_id"]), approval_mode="independent", revocation=None))
        consumer = Mock()
        consumer.request.return_value = ONLINE.SUPPORT.Response(200, {
            "principal": {"type": "user", "id": "7"},
            "context": {"type": "tenant", "tenant_id": "2"},
            "token": {"type": "first_party_access_token"},
            "authorization": {"role_assignments": [{"role_key": "online.security", "permissions": sorted(ONLINE.REQUIRED_PERMISSIONS)}]}})
        context = consumer.request.return_value
        consumer.request.side_effect = lambda method, path, expected, body=None: context if method == 'GET' else ONLINE.SUPPORT.Response(expected[0], {'observed_at': '2026-10-10T00:00:00Z'})
        return authorizer, consumer

    def test_source_grants_cover_only_five_exact_record_datasets_for_the_consumer(self):
        authorizer, consumer = self.source_clients()
        ONLINE.initialize_source_grants(authorizer, consumer, 2, 17, 23, "security_fixture", 29)
        writes = [call for call in authorizer.request.call_args_list if call.args[0] == "POST"]
        self.assertEqual(len(writes), 5)
        self.assertEqual(len({call.args[3]["request_id"] for call in writes}), 5)
        actual = set()
        for call in writes:
            body = call.args[3]
            engine = body["catalog_path"]["engine_id"]
            self.assertEqual(call.args[1], f"/api/v1/system/engines/{engine}/access_grants")
            self.assertEqual(call.args[2], (201,))
            self.assertEqual(body["catalog_path"]["version"], "catalog.path/v1")
            root, namespace, table = body["catalog_path"]["segments"]
            self.assertEqual(root, {"term": "server", "kind": "server", "name": ""})
            self.assertEqual(namespace["kind"], "namespace")
            self.assertEqual(table["term"], "collection" if engine == 29 else "table")
            self.assertEqual(table["kind"], table["term"])
            self.assertEqual(body["recipient_type"], "user")
            self.assertEqual(body["recipient_id"], "7")
            self.assertEqual(body["action"], "read")
            self.assertEqual(body["requirement_version"], "1")
            self.assertIs(body["initialize_approval"], True)
            self.assertEqual(body["expiry_mode"], "until_revoked")
            actual.add((engine, namespace["term"], namespace["name"], table["name"]))
        self.assertEqual(actual, {
            (17, "schema", "addp_online_security", "spatial_algorithm_source"),
            (17, "schema", "addp_online_security", "spatial_algorithm_transfer"),
            (17, "schema", "addp_online_security", "mysql_email_transfer"),
            (23, "database", "security_fixture", "customers"),
            (29, "database", "Outdoor", "Persons"),
        })

    def test_source_preparation_rejects_wrong_tenant_and_self_grant_before_writing(self):
        for tenant, principal in (("9", "8"), ("2", "7")):
            authorizer, consumer = self.source_clients()
            authorizer.request.side_effect = None
            authorizer.request.return_value = ONLINE.SUPPORT.Response(200, {
                "principal": {"type": "user", "id": principal},
                "context": {"type": "tenant", "tenant_id": tenant}})
            with self.subTest(tenant=tenant, principal=principal), self.assertRaises(ONLINE.SuiteError):
                ONLINE.initialize_source_grants(authorizer, consumer, 2, 17, 23, "security_fixture", 29)
            self.assertTrue(all(call.args[0] == "GET" for call in authorizer.request.call_args_list))

    def test_source_preparation_rejects_a_mismatched_issuance_without_continuing(self):
        authorizer, consumer = self.source_clients()
        context = authorizer.request("GET", "unused", (200,))
        authorizer.reset_mock()
        authorizer.request.side_effect = [context, ONLINE.SUPPORT.Response(201, {"recipient_id": "8"})]
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.initialize_source_grants(authorizer, consumer, 2, 17, 23, "security_fixture", 29)
        self.assertEqual(sum(call.args[0] == "POST" for call in authorizer.request.call_args_list), 1)

    def test_initialization_rejects_personal_environment(self):
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.require_hosted_initialization({})
        valid = dict(GITHUB_ACTIONS="true", RUNNER_OS="Linux", ADDP_ONLINE_HOSTED="1", ADDP_ONLINE_HOST="1", ADDP_ONLINE_TEST="1", POSTGRES_DB="addp_online")
        ONLINE.require_hosted_initialization(valid)
        for key in valid:
            with self.subTest(key=key), self.assertRaises(ONLINE.SuiteError):
                ONLINE.require_hosted_initialization(dict(valid, **{key: "wrong"}))

    def test_refuses_foreign_tenant_or_existing_governance_before_writing(self):
        for tenant, definitions in (("9", []), ("2", [{"id": "1"}])):
            client = Mock()
            client.request.side_effect = [
                ONLINE.SUPPORT.Response(200, {"principal": {"type": "user"}, "context": {"type": "tenant", "tenant_id": tenant}}),
                ONLINE.SUPPORT.Response(200, definitions),
            ]
            with self.assertRaises(ONLINE.SuiteError):
                ONLINE.initialize_governance(client, 2)
            self.assertTrue(all(call.args[0] == "GET" for call in client.request.call_args_list))

    def test_creates_fresh_email_and_phone_with_atomic_defaults_through_api(self):
        client = Mock()
        client.request.side_effect = [
            ONLINE.SUPPORT.Response(200, {"principal": {"type": "user"}, "context": {"type": "tenant", "tenant_id": "2"}}),
            *[ONLINE.SUPPORT.Response(200, []) for _ in range(4)],
            ONLINE.SUPPORT.Response(201, {"id": "11"}), ONLINE.SUPPORT.Response(201, {"id": "12"}),
            ONLINE.SUPPORT.Response(201, {"id": "13"}), ONLINE.SUPPORT.Response(201, {"id": "14"}),
            ONLINE.SUPPORT.Response(201, {"id": "15"}), ONLINE.SUPPORT.Response(201, {"id": "16"}),
        ]
        ONLINE.initialize_governance(client, 2)
        writes = [call for call in client.request.call_args_list if call.args[0] == "POST"]
        email, detector, phone, phone_detector = [call.args[3] for call in writes[2:]]
        self.assertEqual(email["default_protection"]["effect"], "suppress")
        self.assertEqual(detector["sensitive_data_type_id"], 13)
        self.assertEqual(detector["capability_key"], ONLINE.EMAIL_DETECTOR)
        self.assertEqual(phone["default_protection"]["allowed_algorithms"], [case[0] for case in ONLINE.ALGORITHM_CASES])
        self.assertEqual(phone_detector["capability_key"], ONLINE.PHONE_DETECTOR)
        self.assertEqual(phone_detector["sensitive_data_type_id"], 15)
        self.assertEqual(phone["security_classification_id"], 11)
        self.assertEqual(phone["default_security_grade_id"], 12)


if __name__ == "__main__":
    unittest.main()
