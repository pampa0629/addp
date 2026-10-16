import importlib.util
import json
import os
import sys
import copy
import struct
import subprocess
import time
import tempfile
import urllib.parse
import unittest
import uuid
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
        self.baseline_updates = []
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
                if (ONLINE.STRUCTURED_MASK_ALGORITHM in body["allowed_algorithms"]
                        and body["algorithm"] != ONLINE.STRUCTURED_MASK_ALGORITHM):
                    raise ONLINE.SuiteError("prefix limits require prefix baseline")
                self.baseline_updates.append(copy.deepcopy(body))
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
        self.component = {"key": ONLINE.MONGODB_FIELD, "value_type": "string",
                          "path": [{"name": "userInfo", "container": "object"}, {"name": "phone", "container": "scalar"}],
                          "schema_fingerprint": "sha256:phone-schema"}
        self.assessment_reads = 0
        self.query_requests = []
        self.query_executions = {}
        self.allow_protected_derived = False
        self.fail_unprotected_derived = False
        self.exports = {}
        self.export_requests = []
        self.export_mutation = None
        self.fail_export_at = None
        self.transfer_requests = []
        self.transfer_executions = {}
        self.transfer_deleted = set()
        self.transfer_targets = {name: [] for name in ONLINE.MONGODB_TRANSFER_TARGETS}
        self.transfer_mutation = None
        self.fail_transfer_at = None

    def rows(self, owner):
        self.preview_calls += 1
        if self.preview_calls == self.fail_preview:
            raise ONLINE.SuiteError("injected MongoDB preview failure")
        by_algorithm = {
            ONLINE.STRUCTURED_MASK_ALGORITHM: {"1": "1*********8", "2": "张***c", "3": "a*c", "4": None, "5": None},
            ONLINE.CONSTANT_ALGORITHM: {"1": "已脱敏", "2": "已脱敏", "3": "已脱敏", "4": None, "5": "已脱敏"},
            ONLINE.SM3_ALGORITHM: dict(ONLINE.SM3_VALUES),
        }
        algorithm = next((policy["algorithm"] for policy in self.policies
                          if policy["state"] == "active" and policy["consumer_owner"] == owner),
                         self.baseline["algorithm"])
        rows = []
        for key in "1234567":
            row = {"_id": key, "displayName": "person-" + key}
            if key != "7":
                row["userInfo"] = {"nickName": "nickname-" + key}
                value = by_algorithm[algorithm].get(key)
                if key == "4":
                    row["userInfo"]["phone"] = None
                elif value is not None:
                    row["userInfo"]["phone"] = value
            rows.append(row)
        return rows

    def request(self, method, path, expected, body=None, *, raw_response=False):
        response = ONLINE.SUPPORT.Response
        if method == "POST" and path == "/api/v1/transfer/task-definitions":
            self.transfer_requests.append(copy.deepcopy(body))
            return response(201, {"id": 100 + len(self.transfer_requests)})
        if path.startswith("/api/v1/transfer/task-definitions/"):
            task_id = int(path.split("/")[5])
            if path.endswith("/start"):
                payload = self.transfer_requests[task_id - 101]
                config = payload["config"]
                assert payload["task_type"] == "sync" and not payload["auto_scan_metadata"]
                assert config["runtime"] == {"boundary": "bounded"} and config["load"] == {"mode": "snapshot"}
                assert config["target"]["policy"] == {"apply_mode": "replace"}
                query = json.loads(config["source"]["query"]["statement"])
                assert config["source"]["query"]["language"] == "mql"
                assert "/path/Outdoor/Persons?" in config["source"]["locator"]
                project = query["pipeline"][0]["$project"]
                derived = "$concat" in project["userInfo__phone"]
                assert query == {"aggregate": "Persons", "pipeline": [{"$project": {
                    "_id": "$_id", "display_name": {"$ifNull": ["$displayName", None]},
                    "userInfo__phone": {"$concat": ["$userInfo.phone", ""]} if derived else {"$ifNull": ["$userInfo.phone", None]},
                    "nick_name": {"$ifNull": ["$userInfo.nickName", None]},
                }}]}
                assert config["transforms"] == [{"type": "field_mapping", "version": "v1", "mode": "project", "fields": [
                    {"source": field, "target": field, "target_type": "string", "nullable": field in {"userInfo__phone", "nick_name"}}
                    for field in ONLINE.MONGODB_TRANSFER_FIELDS]}]
                denied = derived and self.confirmed
                execution_id = str(uuid.UUID(int=task_id))
                execution = {"execution_id": execution_id, "task_id": task_id, "trigger_by": 7,
                    "status": "failed" if denied else "success", "records_read": 0 if denied else 7,
                    "records_written": 0 if denied else 7, "bytes_written": 0 if denied else 500,
                    "error_details": {"category": "execution_failed", "code": "query_output_lineage_unresolved", "diagnostic": {"schema_version": "addp.execution-failure/v2", "stage": "preparation", "reason": "data_protection_required"}} if denied else {}}
                target = config["target"]["name"]
                if not denied:
                    values = {key: {"_id": key, "userInfo": {"phone": ONLINE.SOURCE_VALUES.get(key)}} for key in "1234567"} if derived else {row["_id"]: row for row in self.rows("transfer")}
                    self.transfer_targets[target] = [{"_id": key, "display_name": "person-" + key,
                        "userInfo__phone": values[key].get("userInfo", {}).get("phone"),
                        "nick_name": "nickname-" + key if key != "7" else None} for key in "1234567"]
                if self.transfer_mutation == "fail_start" or len(self.transfer_requests) == self.fail_transfer_at:
                    raise ONLINE.SuiteError("injected transfer start failure")
                if self.transfer_mutation == "failed_control" and not self.confirmed: execution["status"] = "failed"
                if self.transfer_mutation == "allow_derived" and denied: execution["status"] = "success"
                if self.transfer_mutation == "denied_writes" and denied: execution["records_written"] = 1
                if self.transfer_mutation == "denied_bytes" and denied: execution["bytes_written"] = 1
                if self.transfer_mutation == "unrelated_failure" and denied: execution["error_details"]["code"] = "source_connection_unavailable"
                if self.transfer_mutation == "generic_denial" and denied: execution["error_details"]["code"] = "protected_data_denied"
                if self.transfer_mutation == "reason_shape" and denied: execution["error_details"]["code"] = {"message": "query_output_lineage_unresolved"}
                if self.transfer_mutation == "wrong_gate" and denied: execution["error_details"]["code"] = "query_read_failed"
                if self.transfer_mutation == "legacy_error" and denied:
                    execution["error_details"].pop("code")
                    execution["error_msg"] = "prepare query source protection: query output lineage unresolved: fixture"
                if self.transfer_mutation == "raw_text" and denied: execution["error_details"]["message"] = "private"

                if self.transfer_mutation == "denied_target" and denied: self.transfer_targets[target] = [{"_id": "unexpected"}]
                if self.transfer_mutation == "actor": execution["trigger_by"] = 8
                if self.transfer_mutation == "task": execution["task_id"] = 999
                if self.transfer_mutation == "count": execution["records_written"] = 6
                if self.transfer_mutation == "uuid": execution["execution_id"] = "other"
                rows = self.transfer_targets[target]
                if rows and self.transfer_mutation == "plaintext": rows[0]["userInfo__phone"] = "13812345678"
                if rows and self.transfer_mutation == "adjacent": rows[0]["nick_name"] = "changed"
                if rows and self.transfer_mutation == "extra": rows[0]["original_phone"] = "13812345678"
                if rows and self.transfer_mutation == "missing": rows.pop()
                if rows and self.transfer_mutation == "duplicate": rows[-1] = copy.deepcopy(rows[0])
                self.transfer_executions[execution_id] = execution
                return response(200, {"execution_id": execution_id})
            if method == "DELETE": self.transfer_deleted.add(task_id)
            assert method == "DELETE" or task_id in self.transfer_deleted
            return response(404 if method == "GET" else 200, {})
        if path.startswith("/api/v1/transfer/executions/"):
            return response(200, copy.deepcopy(self.transfer_executions[path.rsplit("/", 1)[1]]))
        if path == "/api/v1/meta/engines/17/items":
            return response(200, [{"id": 30 + index, "node_id": 9, "fingerprint": "sha256:" + name,
                "full_name": ONLINE.TARGET_SCHEMA + "." + name, "item_type": "table"}
                for index, name in enumerate(ONLINE.MONGODB_TRANSFER_TARGETS)])
        if path.startswith("/api/v1/manager/preview?"):
            locator = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)["locator"][0]
            for target in ONLINE.MONGODB_TRANSFER_TARGETS:
                if "/" + target + "?" in locator:
                    return response(200, {"preview_type": "table", "data": {
                        "columns": list(ONLINE.MONGODB_TRANSFER_FIELDS), "rows": copy.deepcopy(self.transfer_targets[target])}})
        if method == "POST" and path == "/api/v1/manager/exports":
            self.export_requests.append(copy.deepcopy(body))
            assert body["format"] == "mongodb_extended_jsonl"
            assert "/path/Outdoor/Persons?" in body["source_item_locator"]
            session_id = len(self.exports) + 1
            self.exports[session_id] = {"id": session_id, "source_item_locator": body["source_item_locator"],
                "format": body["format"], "transfer_execution_id": str(uuid.UUID(int=session_id)),
                "status": "success", "download_url": f"/api/v1/manager/exports/{session_id}/file"}
            return response(202, copy.deepcopy(self.exports[session_id]))
        if path.startswith("/api/v1/manager/exports/"):
            session = copy.deepcopy(self.exports[int(path.split("/")[5])])
            if path.endswith("/file"):
                assert raw_response
                rows = self.rows("transfer")
                for row in rows:
                    row["sourceVersion"] = {"$numberLong": "9007199254740993"}
                if len(self.exports) == self.fail_export_at:
                    raise ONLINE.SuiteError("injected export download failure")
                if self.export_mutation == "plaintext": rows[0]["userInfo"]["phone"] = "13812345678"
                if self.export_mutation == "rounded_int64": rows[0]["sourceVersion"] = 9007199254740992
                if self.export_mutation == "extra_field": rows[0]["originalPhone"] = "13812345678"
                raw = ("\n".join(json.dumps(row, ensure_ascii=False) for row in rows) + "\n").encode()
                if self.export_mutation == "invalid_json": raw = b"not JSON"
                if self.export_mutation == "duplicate_key": raw = raw.replace(b'"_id": "1"', b'"_id": "1", "_id": "1"')
                return response(200, raw)
            if self.export_mutation == "failed": session["status"] = "failed"
            if self.export_mutation == "wrong_session": session["id"] = 999
            if self.export_mutation == "wrong_url": session["download_url"] = "https://elsewhere.invalid/file"
            return response(200, session)
        if path.startswith("/api/v1/monitor/executions/by-execution-id/"):
            execution = {"execution_id": path.rsplit("/", 1)[1], "module": "transfer", "source": "manager",
                "source_task_id": None, "status": "success", "triggered_by": 7, "records_read": 7, "records_written": 7}
            if self.export_mutation == "wrong_initiator": execution["triggered_by"] = 8
            if self.export_mutation == "wrong_count": execution["records_written"] = 6
            if self.export_mutation == "task_defined": execution["source_task_id"] = 12
            if self.export_mutation == "wrong_owner": execution["source"] = "develop"
            return response(200, execution)
        if method == "POST" and path == "/api/v1/develop/executions":
            assert body["content"]["query_type"] == "mql"
            assert body["execution_config"]["engine_id"] == 29
            assert "/path/Outdoor/Persons?" in body["content"]["target_locator"]
            query = json.loads(body["content"]["query"])
            self.query_requests.append(query)
            execution_id = "mongo-query-" + str(len(self.query_requests))
            denied = "aggregate" in query and ((self.confirmed and not self.allow_protected_derived)
                                               or (not self.confirmed and self.fail_unprotected_derived))
            if denied:
                execution = {"status": "failed", "metadata": {}, "error_details": {
                    "category": "execution_failed", "code": "develop.query.protection_required",
                    "diagnostic": {"schema_version": "addp.execution-failure/v2", "stage": "preparation", "reason": "data_protection_required"}}}
            else:
                if "find" in query:
                    rows = self.rows("develop")
                elif "count" in query:
                    rows = [{"count": 7}]
                else:
                    assert query == {"aggregate": "Persons", "pipeline": [{"$project": {
                        "_id": 1, "phone": {"$concat": ["$userInfo.phone", ""]}}}]}
                    rows = [{"_id": key, "phone": ONLINE.SOURCE_VALUES.get(key)} for key in "1234567"]
                execution = {"status": "success", "metadata": {"result": {"summary": {"preview_rows": rows}}}}
            self.query_executions[execution_id] = execution
            return response(200, {"execution_id": execution_id})
        if path.startswith("/api/v1/develop/executions/mongo-query-"):
            return response(200, self.query_executions[path.rsplit("/", 1)[1]])
        if path == "/api/v1/meta/engines/29/items":
            return response(200, [copy.deepcopy(self.item)])
        if path.startswith("/api/v1/security/protection-enrollments?"):
            return response(200, {"data": [{"id": "enrolled", "target_snapshot": {
                "engine_id": 29, "full_name": ONLINE.MONGODB_SOURCE}}], "total_pages": 1})
        if path == "/api/v1/security/protection-enrollments/enrolled/components":
            return response(200, {"data": [] if self.confirmed else [{"component": copy.deepcopy(self.component)}]})
        if path == "/api/v1/security/assessments/phone-assessment":
            assert method == "GET" and self.confirmed
            self.assessment_reads += 1
            return response(200, {"id": "phone-assessment", "enrollment_id": "enrolled", "current": {
                "conclusion": "sensitive", "component": copy.deepcopy(self.component)}})
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


class MongoExportTest(unittest.TestCase):
    def export(self, client):
        locator = ONLINE.build_item_locator(29, client.item)
        expected = {"1": "1*********8", "2": "张***c", "3": "a*c", "4": None, "5": None}
        return ONLINE.verify_mongodb_raw_export(client, locator, {"principal_id": "7"}, expected, time.monotonic() + 60)

    def test_downloaded_records_require_canonical_int64_and_exact_nested_values(self):
        client = MongoClient()
        report = self.export(client)
        self.assertEqual(report["rows"], 7)
        self.assertTrue(report["canonical_int64_preserved"])
        self.assertTrue(report["taskless_execution"])
        self.assertTrue(report["same_user_verified"])
        self.assertNotIn("13812345678", json.dumps(report))

    def test_rejects_bad_files_and_wrong_execution_provenance(self):
        for mutation in ("plaintext", "rounded_int64", "extra_field", "invalid_json", "duplicate_key",
                         "failed", "wrong_session", "wrong_url", "wrong_initiator", "wrong_count", "task_defined", "wrong_owner"):
            client = MongoClient()
            client.export_mutation = mutation
            with self.subTest(mutation=mutation), self.assertRaises(ONLINE.SuiteError):
                self.export(client)

    def test_export_failure_still_restores_baseline(self):
        for fail_at in (1, 2, 3, 4):
            client = MongoClient()
            client.fail_export_at = fail_at
            with self.subTest(fail_at=fail_at), self.assertRaisesRegex(ONLINE.SuiteError, "injected export download"):
                MongoAlgorithmTest().run_mongo(client)
            self.assertEqual({k: v for k, v in client.baseline.items() if k != "version"},
                             {k: v for k, v in client.original.items() if k != "version"})
            self.assertFalse(any(p["state"] == "active" for p in client.policies))


class MongoAlgorithmTest(unittest.TestCase):
    def run_mongo(self, client):
        process = RestartRecoveryTest().process
        with tempfile.TemporaryDirectory() as directory, \
             patch.dict(os.environ, {"ADDP_ONLINE_ARTIFACT_DIR": directory, "ADDP_ONLINE_TEST_TENANT_ID": "2"}), \
             patch.object(ONLINE.SUPPORT, "require_hosted_security_lifecycle"), \
             patch.object(ONLINE, "validate_user_identity", return_value={"principal_id": "7"}), \
             patch.object(ONLINE.SUPPORT, "ready_process", side_effect=[process(), process(), process(True), process(True)]), \
             patch.object(ONLINE.subprocess, "run", return_value=Mock(returncode=0)), \
             patch.object(ONLINE, "exercise_owner_unavailability", side_effect=lambda client, tenant, probes, deadline:
                 {"outputs": {name: probe() for name, probe in probes.items()}}) as outage, \
             patch.object(ONLINE, "wait_for_scan", return_value="mongo-scan") as scan:
            prepared = ONLINE.prepare_mongodb_source(client, 29, time.monotonic() + 60)
            targets = ONLINE.prepare_mongodb_transfer_targets(client, 17, time.monotonic() + 60)
            result = ONLINE.exercise_mongodb_algorithms(client, 29, prepared, targets, time.monotonic() + 60, 60)
            self.assertEqual(scan.call_count, 2)
            outage.assert_called_once()
            self.assertEqual(set(outage.call_args.args[2]), {"develop", "develop_query_boundaries", "transfer", "derived_transfer_denied"})
            return result

    def test_three_algorithms_nested_policy_and_restart_restore_without_rescan(self):
        client = MongoClient()
        report = self.run_mongo(client)
        self.assertTrue(client.confirmed)
        self.assertEqual(client.assessment_reads, 1)
        self.assertEqual(client.request("GET", "/api/v1/security/protection-enrollments/enrolled/components", (200,)).payload["data"], [])
        self.assertEqual(report["component"], client.component)
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
        self.assertTrue(all(case["manager"]["null_values_preserved"] for case in report["cases"]))
        self.assertTrue(report["unprotected_derived_query"]["source_values_verified"])
        self.assertTrue(report["initial_technical_field_search"]["same_item_verified"])
        self.assertTrue(all(case["develop"]["sparse_objects_verified"] for case in report["cases"]))
        self.assertEqual(len(client.export_requests), 4)
        self.assertEqual(len(client.transfer_requests), 10)
        self.assertEqual(client.transfer_deleted, set(range(101, 111)))
        self.assertEqual(report["query_transfer_cleanup"], {"created_tasks": 10, "deleted_tasks": 10, "residual_tasks": 0})
        control_query = client.transfer_requests[0]["config"]["source"]["query"]
        for payload in client.transfer_requests:
            if payload["config"]["target"]["name"] == ONLINE.MONGODB_TRANSFER_TARGETS[2]:
                self.assertEqual(payload["config"]["source"]["query"], control_query)
        self.assertEqual(report["unprotected_derived_transfer"]["records_written"], 7)
        self.assertTrue(all(case["query_transfer"]["algorithm_values_verified"] for case in report["cases"]))
        self.assertTrue(all(case["derived_transfer_denied"]["target_empty_before_and_after"] for case in report["cases"]))
        self.assertTrue(all(case["derived_transfer_denied"]["protection_failure"] == "query_output_lineage_unresolved" for case in report["cases"]))
        self.assertTrue(report["cases"][0]["transfer_during_manager_policy"]["same_user_verified"])
        self.assertTrue(all(case["raw_record_export"]["canonical_int64_preserved"] for case in report["cases"]))
        self.assertTrue(report["cases"][0]["export_during_manager_policy"]["same_user_verified"])
        self.assertEqual([case["query_boundaries"]["count"] for case in report["cases"]], [7, 7, 7])
        self.assertTrue(all(case["query_boundaries"]["derived_output_denied"] for case in report["cases"]))
        self.assertTrue(all(case["query_boundaries"]["diagnostic_stage"] == "preparation" for case in report["cases"]))
        self.assertTrue(all(case["query_boundaries"]["diagnostic_reason"] == "data_protection_required" for case in report["cases"]))
        self.assertTrue(report["cases"][0]["develop_during_manager_policy"]["null_values_preserved"])
        self.assertEqual(sum("find" in query for query in client.query_requests), 5)
        self.assertEqual(sum("count" in query for query in client.query_requests), 4)
        self.assertEqual(sum("aggregate" in query for query in client.query_requests), 5)
        self.assertEqual([body["allowed_algorithms"] for body in client.baseline_updates], [
            [ONLINE.STRUCTURED_MASK_ALGORITHM, ONLINE.CONSTANT_ALGORITHM, ONLINE.SM3_ALGORITHM],
            [ONLINE.CONSTANT_ALGORITHM, ONLINE.SM3_ALGORITHM],
            [ONLINE.CONSTANT_ALGORITHM, ONLINE.SM3_ALGORITHM],
            client.original["allowed_algorithms"],
        ])
        self.assertEqual(client.policies[0]["state"], "revoked")
        self.assertEqual({k: v for k, v in client.baseline.items() if k != "version"},
                         {k: v for k, v in client.original.items() if k != "version"})

    def test_invalid_unprotected_control_cannot_count_as_protection_denial(self):
        client = MongoClient()
        client.fail_unprotected_derived = True
        with self.assertRaisesRegex(ONLINE.SuiteError, "Develop execution ended"):
            self.run_mongo(client)
        self.assertFalse(client.confirmed)
        self.assertEqual(client.baseline, client.original)

    def test_transfer_failures_clean_tasks_and_restore_baseline_and_policy(self):
        for fail_at in range(1, 9):
            client = MongoClient()
            client.fail_transfer_at = fail_at
            with self.subTest(fail_at=fail_at), self.assertRaisesRegex(ONLINE.SuiteError, "injected transfer start"):
                self.run_mongo(client)
            self.assertEqual(client.transfer_deleted, set(range(101, 101 + fail_at)))
            self.assertFalse(any(policy["state"] == "active" for policy in client.policies))
            self.assertEqual({k: v for k, v in client.baseline.items() if k != "version"},
                             {k: v for k, v in client.original.items() if k != "version"})

    def test_failed_transfer_control_cannot_count_as_protection_denial(self):
        client = MongoClient()
        client.transfer_mutation = "failed_control"
        with self.assertRaisesRegex(ONLINE.SuiteError, "ended with status failed"):
            self.run_mongo(client)
        self.assertFalse(client.confirmed)
        self.assertEqual(client.transfer_deleted, {101})

    def test_query_transfer_rejects_leaks_wrong_identity_and_false_zero_write_evidence(self):
        for mutation in ("plaintext", "adjacent", "extra", "missing", "duplicate", "actor", "task", "uuid", "count",
                         "allow_derived", "denied_writes", "denied_bytes", "unrelated_failure", "generic_denial", "wrong_gate", "reason_shape", "denied_target", "legacy_error", "raw_text"):
            client = MongoClient()
            client.confirmed = True
            with patch.object(ONLINE, "wait_for_scan", return_value="target-scan"):
                targets = ONLINE.prepare_mongodb_transfer_targets(client, 17, time.monotonic() + 60)
            client.transfer_mutation = mutation
            denied = mutation in {"allow_derived", "denied_writes", "denied_bytes", "unrelated_failure", "generic_denial", "wrong_gate", "reason_shape", "denied_target", "legacy_error", "raw_text"}
            expected = None if denied else {"1": "1*********8", "2": "张***c", "3": "a*c", "4": None, "5": None}
            with self.subTest(mutation=mutation), self.assertRaises(ONLINE.SuiteError):
                ONLINE.verify_mongodb_query_transfer(client, ONLINE.build_item_locator(29, client.item), targets,
                    {"principal_id": "7"}, expected, time.monotonic() + 60)
            self.assertEqual(client.transfer_deleted, {101})

    def test_targets_must_start_empty_and_denied_target_must_be_empty_before_execution(self):
        client = MongoClient()
        client.transfer_targets[ONLINE.MONGODB_TRANSFER_TARGETS[2]] = [{"_id": "stale"}]
        with patch.object(ONLINE, "wait_for_scan", return_value="target-scan"), \
             self.assertRaisesRegex(ONLINE.SuiteError, "exact empty fixture"):
            ONLINE.prepare_mongodb_transfer_targets(client, 17, time.monotonic() + 60)
        target = ONLINE.MONGODB_TRANSFER_TARGETS[2]
        targets = {"locators": {target: f"addp://engine/17/path/addp_online_security/{target}?type=table&item_id=32"}}
        with self.assertRaisesRegex(ONLINE.SuiteError, "not empty before"):
            ONLINE.verify_mongodb_query_transfer(client, "source", targets, {"principal_id": "7"}, None, time.monotonic() + 60)
        self.assertEqual(client.transfer_requests, [])

    def test_protected_derived_output_must_be_denied_and_state_restored(self):
        client = MongoClient()
        client.allow_protected_derived = True
        with self.assertRaises(ONLINE.SuiteError):
            self.run_mongo(client)
        self.assertFalse(any(policy["state"] == "active" for policy in client.policies))
        self.assertEqual({k: v for k, v in client.baseline.items() if k != "version"},
                         {k: v for k, v in client.original.items() if k != "version"})

    def test_confirmed_assessment_structure_must_match_before_protection_changes(self):
        for mutation in ("key", "value_type", "path", "schema_fingerprint"):
            client = MongoClient()
            client.component[mutation] = {"key": "other.phone", "value_type": "json", "path": [{"name": "userInfo.phone", "container": "scalar"}], "schema_fingerprint": "missing"}[mutation]
            with self.subTest(mutation=mutation), self.assertRaisesRegex(ONLINE.SuiteError, "Meta-derived Assessment"):
                self.run_mongo(client)
            self.assertEqual(client.preview_calls, 0)
            self.assertEqual(client.policies, [])
            self.assertEqual(client.baseline, client.original)

    def test_preview_failures_restore_baseline_and_revoke_created_policy(self):
        for fail_at in (1, 2, 3, 4, 5, 6):
            client = MongoClient(); client.fail_preview = fail_at
            with self.subTest(fail_at=fail_at), self.assertRaisesRegex(ONLINE.SuiteError, "injected MongoDB"):
                self.run_mongo(client)
            self.assertFalse(any(policy["state"] == "active" for policy in client.policies))
            self.assertEqual({k: v for k, v in client.baseline.items() if k != "version"},
                             {k: v for k, v in client.original.items() if k != "version"})

    def test_rejects_plaintext_null_changes_missing_field_fabrication_and_sibling_changes(self):
        for mutation in ("plaintext", "null_removed", "null_replaced", "suppressed_empty_null", "missing_field_null", "sibling", "parent", "flattened", "flattened_sparse", "missing", "duplicate"):
            rows = MongoClient().rows("manager")
            # The client's initial 3/4 Baseline uses the independent 1/1 fake vector;
            # validate using that explicit expected vector rather than production code.
            expected = {"1": "1*********8", "2": "张***c", "3": "a*c", "4": None, "5": None}
            if mutation == "plaintext": rows[0]["userInfo"]["phone"] = "13812345678"
            if mutation == "null_removed": del rows[3]["userInfo"]["phone"]
            if mutation == "null_replaced": rows[3]["userInfo"]["phone"] = "changed"
            if mutation == "suppressed_empty_null": rows[4]["userInfo"]["phone"] = None
            if mutation == "missing_field_null": rows[5]["userInfo"]["phone"] = None
            if mutation == "sibling": rows[0]["userInfo"]["nickName"] = "changed"
            if mutation == "parent": rows[6]["userInfo"] = {}
            if mutation == "flattened": rows[0]["userInfo.phone"] = "13812345678"
            if mutation == "flattened_sparse": rows[6]["userInfo__phone"] = "13812345678"
            if mutation == "missing": rows.pop()
            if mutation == "duplicate": rows[-1] = rows[0]
            with self.subTest(mutation=mutation), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_mongodb_rows(rows, "manager", expected)



class OwnerUnavailabilityTest(unittest.TestCase):
    def exercise(self, fail=None):
        events = []
        self.events = events
        process = {"instance_id": "00000000-0000-4000-8000-000000000001",
                   "started_at": "2026-10-12T00:00:00Z", "build_id": "build",
                   "git_commit": "commit", "source_fingerprint": "source"}
        newer = dict(process, instance_id="00000000-0000-4000-8000-000000000002",
                     started_at="2026-10-12T00:02:00Z")
        stopped = [False]
        def lifecycle(repo, operation, deadline):
            events.append(operation)
            stopped[0] = operation == "stop"
            if fail == operation:
                raise ONLINE.SuiteError(operation + " failure")
        def ready(module):
            if module == "security" and events:
                return newer if fail != "build" else dict(newer, build_id="other")
            if fail == "process" and stopped[0] and module == "develop":
                return newer
            return process
        def refusal():
            events.append("refused")
            if fail == "probe":
                raise ONLINE.SuiteError("listener reachable")
            return "2026-10-12T00:01:00Z"
        def output(name):
            events.append(name)
            self.assertTrue(stopped[0])
            if fail == name:
                raise ONLINE.SuiteError("incorrect protected output")
            return {"rows": 5, "email_field_present": False}
        identity = {"principal_id": "7", "tenant_id": "2"}
        with patch.object(ONLINE.SUPPORT, "require_hosted_security_lifecycle"), \
             patch.object(ONLINE.SUPPORT, "run_security_lifecycle", side_effect=lifecycle), \
             patch.object(ONLINE.SUPPORT, "security_listener_refused", side_effect=refusal), \
             patch.object(ONLINE.SUPPORT, "ready_process", side_effect=ready), \
             patch.object(ONLINE, "validate_user_identity", side_effect=[identity, {} if fail == "identity" else identity]):
            return ONLINE.exercise_owner_unavailability(Mock(), 2,
                {name: lambda name=name: output(name) for name in ("develop", "service", "transfer")},
                time.monotonic() + 60)

    def test_real_outputs_are_bracketed_by_refusals_and_restored_without_owner_restart(self):
        result = self.exercise()
        self.assertEqual(self.events, ["stop", "refused", "develop", "refused",
            "refused", "service", "refused", "refused", "transfer", "refused", "start"])
        self.assertEqual(len(result["connection_refusals"]), 6)
        self.assertEqual(set(result["outputs"]), {"develop", "service", "transfer"})
        self.assertEqual(set(result["owner_processes"]), set(ONLINE.OWNER_ACTIONS))
        self.assertTrue(result["owner_processes_unchanged"])
        self.assertTrue(result["same_user_verified"])

    def test_failure_never_skips_recovery_or_becomes_success(self):
        for failure in ("stop", "probe", "develop", "service", "transfer", "process", "start", "build", "identity"):
            with self.subTest(failure=failure), self.assertRaises(ONLINE.SuiteError):
                self.exercise(failure)
            self.assertEqual(self.events[-1], "start")



class MySQLOutageTransferTest(unittest.TestCase):
    def transfer(self, mutation=None):
        execution = {"execution_id": str(uuid.uuid4()), "task_id": 81, "trigger_by": 7,
                     "status": "success", "records_read": 5, "records_written": 5}
        mutations = {"uuid": ("execution_id", "invalid"), "task": ("task_id", 82),
                     "user": ("trigger_by", 9), "status": ("status", "failed"),
                     "read": ("records_read", 0), "written": ("records_written", 0)}
        if mutation in mutations:
            key, value = mutations[mutation]
            execution[key] = value
        rows = [{"id": i, "customer_code": "CUST-1000" + str(i)} for i in range(1, 6)]
        if mutation == "plaintext": rows[0]["email"] = "fixture@example.test"
        if mutation == "duplicate": rows.append(dict(rows[0]))
        if mutation == "neighbor": rows[0]["customer_code"] = "wrong"
        before = (["id"], [] if mutation != "stale" else [{"id": 1}])
        with patch.object(ONLINE, "preview_rows", side_effect=[before, (["id", "customer_code"], rows)]), \
             patch.object(ONLINE, "create_and_run_task", return_value=(81, execution)) as run, \
             patch.object(ONLINE, "wait_for_scan", return_value="scan"), \
             patch.object(ONLINE, "find_item", return_value={"id": 1, "full_name": "addp_online_security.mysql_email_transfer", "item_type": "table"}):
            result = ONLINE.verify_mysql_outage_transfer(Mock(), 17, "source", "parent", "target",
                "outage", {"principal_id": "7"}, time.monotonic() + 60, [])
            self.assertEqual(run.call_args.args[1]["config"]["transforms"], [])
            return result

    def test_requires_real_execution_and_new_target_output_without_field_dropping(self):
        result = self.transfer()
        self.assertEqual(result["records_written"], 5)
        self.assertTrue(result["target_empty_before"])
        self.assertTrue(result["target_output_verified"])
        self.assertTrue(result["same_user_verified"])

    def test_stale_target_plaintext_or_wrong_provenance_cannot_pass(self):
        for mutation in ("uuid", "task", "user", "status", "read", "written", "plaintext", "duplicate", "neighbor", "stale"):
            with self.subTest(mutation=mutation), self.assertRaises(ONLINE.SuiteError):
                self.transfer(mutation)


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
             patch.object(ONLINE.SUPPORT, "require_hosted_security_lifecycle"), \
             patch.object(ONLINE, "validate_user_identity", side_effect=identity or [{"principal_id": "7"}] * 2), \
             patch.object(ONLINE.SUPPORT, "ready_process", side_effect=[self.process(), self.process(), after or self.process(True), self.process(True)]), \
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
                ONLINE.SUPPORT.require_hosted_security_lifecycle(repository)
                for key, value in (("MANAGER_URL", "http://localhost:8081"),
                                   ("ADDP_ONLINE_ARTIFACT_DIR", str(repository / "artifacts")),
                                   ("ADDP_ONLINE_SECRET_DIR", "relative")):
                    with self.subTest(key=key), patch.dict(os.environ, {key: value}), self.assertRaises(ONLINE.SuiteError):
                        ONLINE.SUPPORT.require_hosted_security_lifecycle(repository)
                (repository / ".env").touch()
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.SUPPORT.require_hosted_security_lifecycle(repository)
                (repository / ".env").unlink()
                with patch.object(ONLINE.sys, "platform", "darwin"), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.SUPPORT.require_hosted_security_lifecycle(repository)

    def test_ready_requires_registered_backend_and_complete_process_identity(self):
        payload = dict(self.process(), module="manager", status="ready", role="backend", registration_state="registered")
        with patch.dict(os.environ, {"MANAGER_URL": "http://127.0.0.1:8081"}), patch.object(ONLINE.SUPPORT, "GatewayClient") as health:
            health.return_value.request.return_value = ONLINE.SUPPORT.Response(200, payload)
            self.assertEqual(ONLINE.SUPPORT.ready_process("manager"), self.process())
            self.assertEqual(health.call_args.args[1], "")
            for key, value in (("registration_state", "recovering"), ("instance_id", "invalid"),
                               ("started_at", "2026-10-10T00:00:00"), ("build_id", ""), ("module", "meta")):
                health.return_value.request.return_value = ONLINE.SUPPORT.Response(200, dict(payload, **{key: value}))
                with self.subTest(key=key), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.SUPPORT.ready_process("manager")

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



class RediscoveryClient:
    def __init__(self):
        self.execution_id = "11111111-2222-4333-8444-555555555555"
        self.before = {"id": "enrollment", "state": "active", "version": 10,
            "latest_discovery_execution_id": "previous-execution", "latest_source_snapshot_hash": "sha256:source",
            "last_discovered_at": "2026-10-13T00:00:00Z", "discovery_summary": {"status": "completed"},
            "owner_progress": [{"consumer_owner": owner, "projection_id": "projection-" + owner,
                "revision": "00000000000000000001", "projection_state": "active",
                "published_cursor": "previous-" + owner, "acknowledged": True,
                "acknowledged_at": "2026-10-13T00:00:01Z", "rules": [{"action": action, "effect": "suppress"}]}
                for owner, action in ONLINE.OWNER_ACTIONS.items()]}
        self.after = copy.deepcopy(self.before)
        self.after.update(version=12, latest_discovery_execution_id=self.execution_id,
                          last_discovered_at="2026-10-13T00:01:00Z")
        for item in self.after["owner_progress"]:
            item.update(revision="00000000000000000002", published_cursor="renewed-" + item["consumer_owner"],
                        acknowledged_at="2026-10-13T00:01:01Z")
        self.created = {"execution_id": self.execution_id, "enrollment_id": "enrollment",
            "enrollment_version": 11, "status": "pending", "created_at": "2026-10-13T00:00:59Z"}
        self.posted = False
        self.requests = []
        self.polls = []

    def request(self, method, path, expected, body=None):
        self.requests.append((method, path, body))
        if method == "POST":
            assert not self.posted and path == "/api/v1/security/protection-enrollments/enrollment/discovery-executions"
            assert expected == (201,) and body == {"version": 10}
            self.posted = True
            return ONLINE.SUPPORT.Response(201, copy.deepcopy(self.created))
        assert method == "GET" and path == "/api/v1/security/protection-enrollments/enrollment"
        payload = (self.polls.pop(0) if self.polls else self.after) if self.posted else self.before
        return ONLINE.SUPPORT.Response(200, copy.deepcopy(payload))


class ExplicitRediscoveryTest(unittest.TestCase):
    def setUp(self):
        self.client = RediscoveryClient()
        self.probes = {owner: Mock(return_value={"rows": 5, "email_field_present": False}) for owner in ONLINE.OWNER_ACTIONS}

    def run_rediscovery(self, deadline=None):
        with patch.object(ONLINE.time, "sleep"):
            return ONLINE.exercise_explicit_rediscovery(self.client, "enrollment", self.probes,
                time.monotonic() + 5 if deadline is None else deadline)

    def test_waits_for_current_execution_and_current_revision_ack_before_all_four_outputs(self):
        pending_ack = copy.deepcopy(self.client.after)
        pending_ack["owner_progress"][0]["acknowledged"] = False
        self.client.polls = [copy.deepcopy(self.client.before), pending_ack]
        result = self.run_rediscovery()
        self.assertTrue(result["all_current_revisions_acknowledged"])
        self.assertEqual(result["execution_id"], self.client.execution_id)
        self.assertEqual((result["version_before"], result["version_after"]), (10, 12))
        self.assertEqual(set(result["outputs"]), set(ONLINE.OWNER_ACTIONS))
        self.assertEqual(len(self.client.requests), 5)
        self.assertEqual([r[0] for r in self.client.requests], ["GET", "POST", "GET", "GET", "GET"])
        for owner, probe in self.probes.items():
            probe.assert_called_once_with()
            self.assertEqual(result["projections_before"][owner]["projection_id"], result["projections_after"][owner]["projection_id"])
            self.assertLess(result["projections_before"][owner]["revision"], result["projections_after"][owner]["revision"])
        self.assertIn("security.enrollment.update", ONLINE.REQUIRED_PERMISSIONS)

    def test_rejects_wrong_execution_version_timestamp_or_snapshot_without_reading_outputs(self):
        mutations = [
            ("created", "execution_id", "invalid-uuid"), ("created", "enrollment_id", "other"),
            ("created", "enrollment_version", 10), ("created", "status", "success"),
            ("after", "latest_discovery_execution_id", "other-execution"), ("after", "version", 11),
            ("after", "last_discovered_at", self.client.before["last_discovered_at"]),
            ("after", "last_discovered_at", "2026-10-13T00:01:00"),
            ("after", "latest_source_snapshot_hash", "sha256:changed"), ("after", "state", "released"),
            ("after", "id", "other"), ("after", "discovery_summary", {"status": "not_completed"}),
        ]
        for target, field, value in mutations:
            with self.subTest(target=target, field=field):
                self.client = RediscoveryClient()
                getattr(self.client, target)[field] = value
                with self.assertRaises(ONLINE.SuiteError):
                    self.run_rediscovery()
                for probe in self.probes.values():
                    probe.assert_not_called()

    def test_accepts_iso8601_offsets_and_compares_absolute_instants(self):
        self.client.before["last_discovered_at"] = "2026-10-13T08:00:00+08:00"
        self.client.created["created_at"] = "2026-10-12T19:00:59-05:00"
        self.client.after["last_discovered_at"] = "2026-10-13T08:01:00+08:00"
        for index, item in enumerate(self.client.after["owner_progress"]):
            item["acknowledged_at"] = ("2026-10-12T19:01:01-05:00" if index % 2
                                       else "2026-10-13T00:01:01Z")
        result = self.run_rediscovery()
        self.assertTrue(result["all_current_revisions_acknowledged"])
        for probe in self.probes.values():
            probe.assert_called_once_with()
        self.assertEqual(ONLINE.utc_timestamp("2026-10-13T08:01:00+08:00", "time"),
                         ONLINE.utc_timestamp("2026-10-13T00:01:00Z", "time"))

    def test_offset_timestamps_cannot_hide_old_completion_or_acknowledgement(self):
        for field in ("last_discovered_at", "acknowledged_at"):
            with self.subTest(field=field):
                self.client = RediscoveryClient()
                if field == "last_discovered_at":
                    self.client.after[field] = "2026-10-13T08:00:00+08:00"
                else:
                    self.client.after["owner_progress"][0][field] = "2026-10-13T08:00:59+08:00"
                with self.assertRaises(ONLINE.SuiteError):
                    self.run_rediscovery()
                for probe in self.probes.values():
                    probe.assert_not_called()

    def test_rejects_missing_naive_or_invalid_timestamp(self):
        for value in (None, "", "invalid", "2026-10-13", "2026-10-13T08:00:00"):
            with self.subTest(value=value), self.assertRaises(ONLINE.SuiteError):
                ONLINE.utc_timestamp(value, "time")

    def test_rejects_old_or_wrong_projection_proofs_even_when_acknowledged(self):
        for field, value in (("revision", "00000000000000000001"), ("revision", "2"),
                             ("revision", "00000000000000000000"), ("projection_id", "other"),
                             ("published_cursor", "previous-manager"), ("published_cursor", ""),
                             ("acknowledged_at", "2026-10-13T00:00:01Z"), ("projection_state", "enrolling"),
                             ("rules", [{"action": "preview", "effect": "allow"}])):
            with self.subTest(field=field):
                self.client = RediscoveryClient()
                self.client.after["owner_progress"][0][field] = value
                with self.assertRaises(ONLINE.SuiteError):
                    self.run_rediscovery()
                for probe in self.probes.values():
                    probe.assert_not_called()

    def test_missing_or_duplicate_owner_and_incomplete_probe_set_are_rejected(self):
        for duplicate in (False, True):
            self.client = RediscoveryClient()
            if duplicate:
                self.client.after["owner_progress"].append(copy.deepcopy(self.client.after["owner_progress"][0]))
            else:
                self.client.after["owner_progress"].pop()
            with self.assertRaises(ONLINE.SuiteError):
                self.run_rediscovery()
        self.client = RediscoveryClient()
        self.probes.pop("transfer")
        with self.assertRaises(ONLINE.SuiteError):
            self.run_rediscovery()
        self.assertEqual(self.client.requests, [])

    def test_unsynchronized_new_revision_times_out_without_output(self):
        self.client.after["owner_progress"][0]["acknowledged"] = False
        with patch.object(ONLINE.time, "monotonic", side_effect=[1, 3]), self.assertRaisesRegex(ONLINE.SuiteError, "did not synchronize"):
            self.run_rediscovery(2)
        for probe in self.probes.values():
            probe.assert_not_called()

    def test_renewed_transfer_cannot_reuse_a_previous_task_as_new_output(self):
        with patch.object(ONLINE, "create_and_run_task", return_value=(81, {})), self.assertRaisesRegex(ONLINE.SuiteError, "reused a previous task"):
            ONLINE.verify_mysql_transfer_output(Mock(), 17, "source", "parent", "renewed", {"principal_id": "7"}, time.monotonic() + 5, [81])

    def test_renewed_output_rejects_plaintext_or_changed_neighbor_values(self):
        rows = [{"id": i, "customer_code": "CUST-1000" + str(i)} for i in range(1, 6)]
        self.assertTrue(ONLINE.assert_mysql_customer_rows(rows, "owner", ["id", "customer_code"])["customer_codes_verified"])
        for mutation in ("email", "customer_code", "duplicate"):
            with self.subTest(mutation=mutation):
                changed = copy.deepcopy(rows)
                if mutation == "email": changed[0]["email"] = "fixture@example.test"
                elif mutation == "customer_code": changed[0]["customer_code"] = "changed"
                else: changed.append(copy.deepcopy(changed[0]))
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.assert_mysql_customer_rows(changed, "owner")
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.assert_mysql_customer_rows(rows, "owner", ["id", "customer_code", "email"])

    def test_actual_owner_output_failure_does_not_produce_a_passed_report(self):
        self.probes["develop"].side_effect = ONLINE.SuiteError("actual output failed")
        with self.assertRaisesRegex(ONLINE.SuiteError, "actual output failed"):
            self.run_rediscovery()
        self.probes["service"].assert_not_called()
        self.probes["transfer"].assert_not_called()


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
                patch.object(ONLINE.SUPPORT, 'require_hosted_security_lifecycle'),
                patch.object(ONLINE, 'validate_user_identity', return_value={}) as identity,
                patch.object(ONLINE, 'prepare_mongodb_source', return_value={'scan_execution_id': 'prepared'}) as prepare,
                patch.object(ONLINE, 'prepare_mongodb_transfer_targets', return_value={}) as targets,
                patch.object(ONLINE, 'run_scenario', return_value={'identity': {'principal_id': '7'}, 'fixture': {'security_enrollment_id': 'enrollment'}}) as scenario,
                patch.object(ONLINE, 'exercise_mongodb_algorithms', return_value={'cases': []}),
                patch.object(ONLINE, 'find_item', return_value={'fingerprint': 'source', 'node_id': 12}),
                patch.object(ONLINE, 'build_item_locator', return_value='source-locator'),
                patch.object(ONLINE, 'wait_for_technical_field') as readiness,
                patch.object(ONLINE, 'run_export_browser') as browser,
                patch('builtins.print'),
            ):
                order = Mock()
                order.attach_mock(identity, 'identity')
                order.attach_mock(prepare, 'prepare')
                order.attach_mock(targets, 'targets')
                order.attach_mock(scenario, 'scenario')
                readiness.return_value = {'same_item_verified': True}
                if unavailable:
                    readiness.side_effect = ONLINE.SuiteError('protected field not searchable')
                    with self.assertRaises(ONLINE.SuiteError):
                        ONLINE.main()
                    browser.assert_not_called()
                else:
                    def launch(*args):
                        readiness.assert_called_once()
                        context = args[3]
                        self.assertEqual(context["principal_id"], "7")
                        self.assertEqual(context["enrollment_id"], "enrollment")
                        self.assertFalse(context["service_payload"]["public_access"])
                        self.assertFalse(context["transfer_payload"]["enabled"])
                        self.assertEqual(context["transfer_payload"]["config"]["source"]["locator"], args[2])
                        self.assertEqual(context["transfer_payload"]["config"]["runtime"], {"boundary": "bounded"})
                        return self.report()
                    browser.side_effect = launch
                    self.assertEqual(ONLINE.main(), 0)
                    self.assertEqual([call[0] for call in order.mock_calls], ['identity', 'prepare', 'targets', 'scenario'])
                    self.assertEqual(readiness.call_args.args[1:4], (23, {'fingerprint': 'source', 'node_id': 12}, 'email'))

    def report(self):
        return {
            "schema_version": "addp.security-manager-export-browser/v7", "result": "passed",
            "run_id": "run-42", "tenant_id": "2", "records": 5,
            "execution_id": "53834320-203b-4d8c-838e-15e01024d484",
            "protection_transfer_execution_id": "64834320-203b-4d8c-838e-15e01024d484",
            "email_field_present": False, "non_sensitive_fields_preserved": True,
            "same_user_verified": True, "initiator_verified": True, "taskless_execution": True,
            "manager_source_verified": True, "monitor_detail_visible": True,
            "protected_preview_verified": True, "technical_field_search_verified": True,
            "search_to_preview_verified": True,
            "resource_combination_filters_verified": True, "resource_literal_filter_verified": True,
            "resource_scope_intersection_verified": True, "resource_reset_verified": True,
            "resource_filter_total_verified": True, "resource_filters_read_only": True,
            "owner_validity_visible": True, "resource_filter_case_count": 9,
            "browser_session_rebound_verified": True, "resource_permanent_validity_visible": True,
            "develop_alias_protected": True, "develop_refusal_rows": 0,
            "develop_refusal_diagnostic_verified": True, "develop_refusal_visible": True,
            "develop_monitor_same_execution": True,
            "service_normal_output_verified": True, "service_refusal_http_status": 403,
            "service_refusal_visible": True, "service_old_preview_cleared": True, "service_restored_output_verified": True,
            "transfer_refusal_diagnostic_verified": True, "transfer_refusal_visible": True, "transfer_monitor_same_execution": True,
            "transfer_target_unchanged": True, "output_protection_permanent_restored": True,
            "output_browser_created_resources": 2, "output_browser_deleted_resources": 2, "output_browser_residual_resources": 0,
            "expected_service_refusal_403": 1,
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
            ("schema_version", "addp.security-manager-export-browser/v3"),
            ("resource_combination_filters_verified", False), ("resource_filters_read_only", False),
            ("resource_filter_case_count", 8), ("resource_filter_case_count", "9"),
            ("resource_filter_case_count", True), ("owner_validity_visible", False), ("resource_permanent_validity_visible", False),
            ("browser_session_rebound_verified", False), ("develop_alias_protected", False),
            ("develop_refusal_rows", 1), ("develop_refusal_rows", False),
            ("develop_refusal_diagnostic_verified", False), ("develop_refusal_visible", False),
            ("develop_monitor_same_execution", False),
            ("service_refusal_http_status", 500), ("service_refusal_visible", False),
            ("service_old_preview_cleared", False), ("service_restored_output_verified", False),
            ("transfer_refusal_diagnostic_verified", False), ("transfer_refusal_visible", False),
            ("transfer_monitor_same_execution", False), ("transfer_target_unchanged", False),
            ("output_protection_permanent_restored", False), ("output_browser_created_resources", 0),
            ("output_browser_deleted_resources", 1), ("output_browser_residual_resources", 1),
            ("expected_service_refusal_403", 0), ("expected_service_refusal_403", 2),
            ("expected_service_refusal_403", True), ("protection_transfer_execution_id", "bad"),
            ("protection_transfer_execution_id", None), ("protection_transfer_execution_id", True),
            ("protection_transfer_execution_id", "00000000-0000-0000-0000-000000000000"),
            ("protection_transfer_execution_id", "53834320-203b-4d8c-838e-15e01024d484"),
            ("initiator_verified", 1), ("browser_warning_errors", False),
        ):
            with self.subTest(key=key, value=value), self.assertRaises(ONLINE.SuiteError):
                ONLINE.validate_export_browser_report(dict(valid, **{key: value}), "run-42", "2")
        for key in valid:
            incomplete = dict(valid); incomplete.pop(key)
            with self.subTest(missing=key), self.assertRaises(ONLINE.SuiteError):
                ONLINE.validate_export_browser_report(incomplete, "run-42", "2")

    def test_online_browser_javascript_parses(self):
        result = subprocess.run(["node", "--check", "--input-type=module"],
            input=(SCRIPT.resolve().parents[2] / "console/frontend/e2e/online/security-manager-export.spec.js").read_text(),
            capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_browser_transfer_uses_professional_initiator_contract(self):
        # Execute the real browser helper against the professional HTTP shape;
        # Monitor's triggered_by field must not substitute for trigger_by.
        probe = r"""
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const source = readFileSync(0, 'utf8')
const helper = source.slice(source.indexOf('  async function runTransfer(status) {'), source.indexOf('  async function updateValidity('))
assert.ok(helper.includes('async function runTransfer'))
const expect = value => ({ toBe: expected => assert.equal(value, expected), toMatch: expected => assert.match(value, expected) })
expect.poll = callback => ({ toMatch: async expected => assert.match(await callback(), expected) })
const executionID = '53834320-203b-4d8c-838e-15e01024d484'
let execution
const api = { post: async () => ({ execution_id: executionID }), get: async () => execution }
const run = new Function('json', 'getAPI', 'expect', 'input', 'taskID', helper + '; return runTransfer')(
  async value => value, () => api, expect, { principal_id: '33' }, 42)
for (const status of ['success', 'failed']) {
  execution = { execution_id: executionID, task_id: 42, status, trigger_by: 33 }
  assert.equal(await run(status), execution)
  for (const actor of [{ trigger_by: 34 }, {}, { triggered_by: 33 }]) {
    execution = { execution_id: executionID, task_id: 42, status, ...actor }
    await assert.rejects(() => run(status), { code: 'ERR_ASSERTION' })
  }
}
"""
        result = subprocess.run(["node", "--input-type=module", "-e", probe],
            input=(SCRIPT.resolve().parents[2] / "console/frontend/e2e/online/security-manager-export.spec.js").read_text(),
            capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_browser_native_transfer_preserves_all_protected_source_columns(self):
        probe = r"""
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const source = readFileSync(0, 'utf8')
const helper = source.slice(source.indexOf('  function checkRows('), source.indexOf('  async function runTransfer('))
assert.ok(helper.includes('async function targetRows'))
const expect = value => ({ toBe: expected => assert.equal(value, expected),
  toHaveLength: expected => assert.equal(value.length, expected), toEqual: expected => assert.deepEqual(value, expected) })
const rows = Array.from({ length: 5 }, (_, i) => ({ id: i + 1, customer_code: `CUST-1000${i + 1}`, city: 'fixture-city' }))
const columns = ['city', 'customer_code', 'id']
let payload = { data: { columns, rows } }
const methods = new Function('json', 'getAPI', 'expect', 'input', 'targetColumns', 'serviceColumns', helper + '; return { targetRows, checkRows }')(
  async value => value, () => ({ get: async () => payload }), expect, { target_locator: 'fixture' }, columns, ['customer_code', 'id'])
assert.deepEqual(await methods.targetRows(), rows)
methods.checkRows(rows.map(({ id, customer_code }) => ({ id, customer_code })))
for (const data of [
  { columns: ['customer_code', 'id'], rows: rows.map(({ id, customer_code }) => ({ id, customer_code })) },
  { columns: [...columns, 'email'], rows: rows.map(row => ({ ...row, email: 'sensitive' })) },
  { columns, rows: rows.map(row => ({ ...row, email: 'hidden-sensitive' })) },
  { columns, rows: rows.slice(1) },
  { columns, rows: rows.map(row => ({ ...row, customer_code: 'changed' })) }
]) {
  payload = { data }
  await assert.rejects(() => methods.targetRows(), { code: 'ERR_ASSERTION' })
}
"""
        result = subprocess.run(["node", "--input-type=module", "-e", probe],
            input=(SCRIPT.resolve().parents[2] / "console/frontend/e2e/online/security-manager-export.spec.js").read_text(),
            capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_browser_failure_and_missing_report_cannot_reuse_previous_success(self):
        with tempfile.TemporaryDirectory() as directory:
            artifact = Path(directory)
            path = artifact / "security-manager-export-browser.json"
            environment = dict(ADDP_ONLINE_ARTIFACT_DIR=directory, ADDP_ONLINE_TEST_RUN_ID="run-42",
                ADDP_ONLINE_TEST_TENANT_ID="2", ADDP_ONLINE_TEST_USER_ACCESS_TOKEN="test-token",
                ADDP_ONLINE_TEST_USER_USERNAME="test-user", ADDP_ONLINE_TEST_USER_PASSWORD="test-password",
                ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID="29",
                GATEWAY_URL="http://127.0.0.1:8000", CONSOLE_URL="http://127.0.0.1:5170")
            for exit_code in (1, 0):
                path.write_text(json.dumps(self.report()))
                with patch.object(ONLINE.subprocess, "run", return_value=Mock(returncode=exit_code, stdout="", stderr="")):
                    with self.subTest(exit_code=exit_code), self.assertRaises(ONLINE.SuiteError):
                        ONLINE.run_export_browser(artifact, environment, "source", {"enrollment_id": "fixture"})
                self.assertFalse(path.exists())
            def browser(command, **kwargs):
                self.assertIn("e2e/online/security-manager-export.spec.js", command)
                self.assertEqual(kwargs["env"]["ADDP_ONLINE_SECURITY_EXPORT_LOCATOR"], "source")
                self.assertEqual(json.loads(kwargs["env"]["ADDP_ONLINE_SECURITY_OUTPUT_BROWSER_CONTEXT"]), {"enrollment_id": "fixture"})
                path.write_text(json.dumps(self.report()))
                for name in ("security-manager-protected-preview.png", "security-manager-field-search.png",
                             "security-manager-export.png", "security-manager-export-monitor.png",
                             "security-resource-combination-filters.png", "security-resource-empty-intersection.png",
                             "security-resource-filter-reset.png", "security-develop-protection-refusal.png",
                             "security-develop-protection-monitor.png", "security-service-protection-refusal.png",
                             "security-service-protection-restored.png", "security-transfer-protection-refusal.png",
                             "security-transfer-protection-monitor.png"):
                    (artifact / name).write_bytes(b"test screenshot")
                return Mock(returncode=0, stdout="", stderr="")
            with patch.object(ONLINE.subprocess, "run", side_effect=browser):
                self.assertEqual(ONLINE.run_export_browser(artifact, environment, "source", {"enrollment_id": "fixture"}), self.report())

            for missing in ("security-manager-protected-preview.png", "security-manager-field-search.png",
                            "security-resource-combination-filters.png", "security-resource-empty-intersection.png",
                            "security-resource-filter-reset.png", "security-develop-protection-refusal.png",
                             "security-develop-protection-monitor.png", "security-service-protection-refusal.png",
                             "security-service-protection-restored.png", "security-transfer-protection-refusal.png",
                             "security-transfer-protection-monitor.png"):
                def incomplete_browser(command, **kwargs):
                    result = browser(command, **kwargs)
                    (artifact / missing).unlink()
                    return result
                with self.subTest(missing=missing), patch.object(ONLINE.subprocess, "run", side_effect=incomplete_browser):
                    with self.assertRaisesRegex(ONLINE.SuiteError, "screenshots"):
                        ONLINE.run_export_browser(artifact, environment, "source", {"enrollment_id": "fixture"})


class HostedGovernanceInitializationTest(unittest.TestCase):
    def setUp(self):
        self.scan = patch.object(ONLINE, "wait_for_scan", return_value="preparation-scan").start()
        self.addCleanup(patch.stopall)

    def source_clients(self):
        import hashlib
        authorizer, consumer = Mock(), Mock()
        context = ONLINE.SUPPORT.Response(200, {
            "principal": {"type": "user", "id": "7"}, "context": {"type": "tenant", "tenant_id": "2"},
            "token": {"type": "first_party_access_token"},
            "authorization": {"role_assignments": [{"role_key": "online.security", "permissions": sorted(ONLINE.REQUIRED_PERMISSIONS)}]}})
        def prepare(method, path, expected, body=None):
            if path == '/api/v1/system/auth/context':
                return ONLINE.SUPPORT.Response(200, {"principal": {"type": "user", "id": "8"},
                    "context": {"type": "tenant", "tenant_id": "2"},
                    "authorization": {"role_assignments": [{"permissions": sorted(ONLINE.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS)}]}})
            if method == 'GET':
                fingerprint = path.split('/')[-2]
                engine = int(path.split('/')[5])
                self.assertEqual(engine, 17, 'Security preparation must not call MySQL/MongoDB source authorization')
                namespace = ONLINE.TARGET_SCHEMA
                names = list(ONLINE.MONGODB_TRANSFER_TARGETS) + [ONLINE.SPATIAL_SOURCE, ONLINE.SPATIAL_TARGET, ONLINE.TARGET_TABLE, ONLINE.BANK_TARGET]
                name = next(name for name in names if hashlib.sha256(f'{engine}:{namespace}.{name}'.encode()).hexdigest() == fingerprint)
                term = 'table'
                return ONLINE.SUPPORT.Response(200, {'schema_version': 'meta.data_item_authorization_facts/v1',
                    'item_fingerprint': fingerprint, 'engine_id': str(engine), 'item_type': term,
                    'full_name': namespace + '.' + name, 'basis_state': 'complete', 'observed_at': '2026-10-16T00:00:00Z',
                    'boundary_hash': 'a' * 64, 'boundary': {'layout': 'single', 'native_target': {
                        'version': 'catalog.path/v1', 'engine_id': engine, 'segments': [
                            {'term': 'server', 'kind': 'server', 'name': ''},
                            {'term': 'schema', 'kind': 'namespace', 'name': namespace},
                            {'term': term, 'kind': term, 'name': name}]}}})
            return ONLINE.SUPPORT.Response(201, dict(body, engine_id=str(body['catalog_path']['engine_id']),
                approval_mode='independent', revocation=None, boundary_hash=body['expected_boundary_hash']))
        def read(method, path, expected, body=None):
            if path == '/api/v1/system/auth/context': return context
            if method == 'GET':
                engine = int(path.split('/')[5])
                self.assertEqual(engine, 17, 'Security preparation must not call MySQL/MongoDB source authorization')
                namespace = ONLINE.TARGET_SCHEMA
                names = list(ONLINE.MONGODB_TRANSFER_TARGETS) + [ONLINE.SPATIAL_SOURCE, ONLINE.SPATIAL_TARGET, ONLINE.TARGET_TABLE, ONLINE.BANK_TARGET]
                return ONLINE.SUPPORT.Response(200, [{'full_name': namespace + '.' + name,
                    'item_type': 'table',
                    'fingerprint': hashlib.sha256(f'{engine}:{namespace}.{name}'.encode()).hexdigest()} for name in names])
            return ONLINE.SUPPORT.Response(expected[0], {'observed_at': '2026-10-10T00:00:00Z'})
        authorizer.request.side_effect, consumer.request.side_effect = prepare, read
        return authorizer, consumer

    def test_source_grants_cover_only_seven_postgres_tables_for_the_consumer(self):
        authorizer, consumer = self.source_clients()
        ONLINE.initialize_postgres_read_grants(authorizer, consumer, 2, 17)
        writes = [call for call in authorizer.request.call_args_list if call.args[0] == "POST"]
        self.assertEqual([call.args[1] for call in self.scan.call_args_list], [17])
        self.assertEqual(len(writes), 7)
        self.assertEqual(len({call.args[3]["request_id"] for call in writes}), 7)
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
            self.assertEqual(table["term"], "table")
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
            (17, "schema", "addp_online_security", "mongodb_query_transfer"),
            (17, "schema", "addp_online_security", "mongodb_query_control"),
            (17, "schema", "addp_online_security", "mongodb_query_denied"),
            (17, "schema", "addp_online_security", "bank_card_transfer"),
        })

    def test_initializer_does_not_require_mysql_or_mongodb_authorization_inputs(self):
        environment = {
            "GATEWAY_URL": "http://gateway", "ADDP_ONLINE_TEST_TENANT_ID": "2",
            "ADDP_ONLINE_TEST_ENGINE_ID": "17",
            "ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN": "governance-token",
            "ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN": "source-token",
            "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "consumer-token",
        }
        with patch.dict(os.environ, environment, clear=True), \
             patch.object(sys, "argv", [str(SCRIPT), "--initialize"]), \
             patch.object(ONLINE.signal, "signal"), \
             patch.object(ONLINE, "require_hosted_initialization"), \
             patch.object(ONLINE, "GatewayClient", side_effect=["governance-user", "source-user", "consumer"]), \
             patch.object(ONLINE, "initialize_governance") as governance, \
             patch.object(ONLINE, "initialize_postgres_read_grants") as grants, \
             patch("builtins.print") as notice:
            self.assertEqual(ONLINE.main(), 0)
        governance.assert_called_once_with("governance-user", 2)
        grants.assert_called_once_with("source-user", "consumer", 2, 17)
        notice.assert_called_once_with("Disposable Security governance and PostgreSQL table read Grants are ready")

    def test_source_preparation_rejects_wrong_tenant_and_self_grant_before_writing(self):
        for tenant, principal in (("9", "8"), ("2", "7")):
            authorizer, consumer = self.source_clients()
            authorizer.request.side_effect = None
            authorizer.request.return_value = ONLINE.SUPPORT.Response(200, {
                "principal": {"type": "user", "id": principal},
                "context": {"type": "tenant", "tenant_id": tenant}})
            with self.subTest(tenant=tenant, principal=principal), self.assertRaises(ONLINE.SuiteError):
                ONLINE.initialize_postgres_read_grants(authorizer, consumer, 2, 17)
            self.assertTrue(all(call.args[0] == "GET" for call in authorizer.request.call_args_list))

    def test_source_preparation_rejects_a_mismatched_issuance_without_continuing(self):
        authorizer, consumer = self.source_clients()
        original = authorizer.request.side_effect
        def wrong_receipt(method, path, expected, body=None):
            if method == 'POST': return ONLINE.SUPPORT.Response(201, {"recipient_id": "8"})
            return original(method, path, expected, body)
        authorizer.request.side_effect = wrong_receipt
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.initialize_postgres_read_grants(authorizer, consumer, 2, 17)
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

    def test_creates_email_phone_and_bank_with_atomic_defaults_through_api(self):
        client = Mock()
        client.request.side_effect = [
            ONLINE.SUPPORT.Response(200, {"principal": {"type": "user"}, "context": {"type": "tenant", "tenant_id": "2"}}),
            *[ONLINE.SUPPORT.Response(200, []) for _ in range(4)],
            ONLINE.SUPPORT.Response(201, {"id": "11"}), ONLINE.SUPPORT.Response(201, {"id": "12"}),
            ONLINE.SUPPORT.Response(201, {"id": "13"}), ONLINE.SUPPORT.Response(201, {"id": "14"}),
            ONLINE.SUPPORT.Response(201, {"id": "15"}), ONLINE.SUPPORT.Response(201, {"id": "16"}),
            ONLINE.SUPPORT.Response(201, {"id": "17"}), ONLINE.SUPPORT.Response(201, {"id": "18"}),
        ]
        ONLINE.initialize_governance(client, 2)
        writes = [call for call in client.request.call_args_list if call.args[0] == "POST"]
        email, detector, phone, phone_detector, bank, bank_detector = [call.args[3] for call in writes[2:]]
        self.assertEqual(email["default_protection"]["effect"], "suppress")
        self.assertEqual(detector["sensitive_data_type_id"], 13)
        self.assertEqual(detector["capability_key"], ONLINE.EMAIL_DETECTOR)
        self.assertEqual(phone["default_protection"]["allowed_algorithms"], [case[0] for case in ONLINE.ALGORITHM_CASES])
        self.assertEqual(phone_detector["capability_key"], ONLINE.PHONE_DETECTOR)
        self.assertEqual(phone_detector["sensitive_data_type_id"], 15)
        self.assertEqual(bank_detector["sensitive_data_type_id"], 17)
        self.assertEqual(bank_detector["capability_key"], ONLINE.BANK_DETECTOR)
        self.assertEqual(bank["default_protection"]["parameters"], {"prefix_runes": 1, "suffix_runes": 1, "mask_rune": "*"})
        self.assertEqual(phone["security_classification_id"], 11)
        self.assertEqual(phone["default_security_grade_id"], 12)


class BankCardMetadataAcceptanceTest(unittest.TestCase):
    def rows(self, protected=True):
        return [{"id": key, "customer_code": "CUST-1000" + key,
                 "bank_card_number": (ONLINE.BANK_MASKED if protected else ONLINE.BANK_SOURCE_VALUES)[key], "bank_account_no": "account-" + key,
                 "card_id": "card-" + key, "bank_card_verified": "verified-" + key,
                 "credit_card_number": 100 + int(key)} for key in "12345"]

    def enrollment(self):
        return {"state": "active", "last_discovered_at": "2026-10-13T10:00:00+08:00",
                "owner_progress": [{"consumer_owner": owner, "projection_state": "active",
                    "projection_id": str(uuid.uuid4()), "revision": "00000000000000000001",
                    "published_cursor": str(index), "acknowledged": True,
                    "acknowledged_at": "2026-10-13T02:00:01Z", "validity": "valid",
                    "rules": [{"action": action, "effect": "mask"}]}
                    for index, (owner, action) in enumerate(ONLINE.OWNER_ACTIONS.items(), 1)]}

    def assessment(self):
        return {"id": "assessment-bank", "enrollment_id": "enrollment-bank", "current": {
            "conclusion": "sensitive", "sensitive_data_type_id": "17", "component": {"key": ONLINE.BANK_FIELD, "value_type": "string",
            "path": [{"name": ONLINE.BANK_FIELD, "container": "scalar"}],
            "schema_fingerprint": "sha256:" + "a" * 64}}}

    def finding(self):
        return {"id": "finding-bank", "detector_version": ONLINE.BANK_DETECTOR,
                "sensitive_data_type_id": "17",
                "component": {"key": ONLINE.BANK_FIELD}}

    def test_outputs_reject_plaintext_schema_and_negative_field_corruption(self):
        expected = ONLINE.assert_bank_card_rows(self.rows(), "owner", ONLINE.BANK_FIELDS)
        self.assertTrue(expected["null_preserved"])
        ONLINE.assert_bank_card_rows(self.rows(False), "unprotected control", ONLINE.BANK_FIELDS, protected=False)
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.assert_bank_card_rows(self.rows(), "stale protected source", protected=False)
        rows = self.rows(); del rows[4][ONLINE.BANK_FIELD]
        ONLINE.assert_bank_card_rows(rows, "raw invalid value suppression")
        for mutation in ("plaintext", "unicode", "empty", "null", "account", "card_id", "number", "suffix", "duplicate", "missing"):
            rows = self.rows()
            if mutation == "plaintext": rows[0][ONLINE.BANK_FIELD] = "6222000000000000001"
            if mutation == "unicode": rows[1][ONLINE.BANK_FIELD] = "测试卡abc"
            if mutation == "empty": rows[4][ONLINE.BANK_FIELD] = ""
            if mutation == "null": del rows[3][ONLINE.BANK_FIELD]
            if mutation == "account": rows[0]["bank_account_no"] = "masked"
            if mutation == "card_id": del rows[0]["card_id"]
            if mutation == "number": rows[0]["credit_card_number"] = None
            if mutation == "suffix": rows[0]["bank_card_verified"] = "masked"
            if mutation == "duplicate": rows[1]["id"] = "1"
            if mutation == "missing": rows.pop()
            with self.subTest(mutation=mutation), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_bank_card_rows(rows, "owner")
        for columns in (ONLINE.BANK_FIELDS[:-1], ONLINE.BANK_FIELDS + ["unexpected"]):
            with self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_bank_card_rows(self.rows(), "schema", columns)

    def test_projection_requires_every_current_acknowledgement_and_valid_mask(self):
        self.assertEqual(set(ONLINE.bank_card_projections(self.enrollment())), set(ONLINE.OWNER_ACTIONS))
        for field, value in (("acknowledged", False), ("validity", "expired"), ("revision", "1"),
                             ("published_cursor", ""), ("projection_id", "bad"),
                             ("acknowledged_at", "2026-10-13T01:59:59Z"),
                             ("rules", [{"action": "export", "effect": "allow"}])):
            enrollment = self.enrollment(); enrollment["owner_progress"][0][field] = value
            with self.subTest(field=field), self.assertRaises(ONLINE.SuiteError):
                ONLINE.bank_card_projections(enrollment)
        enrollment = self.enrollment(); enrollment["owner_progress"].pop()
        with self.assertRaises(ONLINE.SuiteError): ONLINE.bank_card_projections(enrollment)

    def test_discovery_rejects_false_positive_and_wrong_meta_component(self):
        client = Mock(); client.request.return_value = ONLINE.SUPPORT.Response(200, self.assessment())
        client.request.side_effect = lambda method, path, expected: (
            ONLINE.SUPPORT.Response(200, [{"code": "bank_card", "id": "17"}])
            if path.endswith("/sensitive-data-types") else client.request.return_value)
        with patch.object(ONLINE, "list_pages", return_value=[self.finding()]):
            result = ONLINE.assert_bank_card_discovery(client, "enrollment-bank", "assessment-bank", "finding-bank")
        self.assertTrue(result["negative_fields_not_detected"])
        for field in ("bank_account_no", "card_id", "bank_card_verified", "credit_card_number"):
            finding = self.finding(); finding["component"]["key"] = field
            with self.subTest(field=field), patch.object(ONLINE, "list_pages", return_value=[self.finding(), finding]), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_bank_card_discovery(client, "enrollment-bank", "assessment-bank", "finding-bank")
        for field, value in (("value_type", "int"), ("key", "credit_card_number"), ("path", []), ("schema_fingerprint", "")):
            assessment = self.assessment(); assessment["current"]["component"][field] = value
            client.request.return_value = ONLINE.SUPPORT.Response(200, assessment)
            with self.subTest(field=field), patch.object(ONLINE, "list_pages", return_value=[self.finding()]), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_bank_card_discovery(client, "enrollment-bank", "assessment-bank", "finding-bank")

    def run_case(self, changes=None, cleanup_failure=False):
        client = Mock()
        def request(method, path, expected, body=None):
            if path.endswith("/sensitive-data-types"): payload = [{"code": "bank_card", "id": "17"}]
            elif path.endswith("/enrollment-bank"): payload = self.enrollment()
            elif path.endswith("/assessment-bank"): payload = self.assessment()
            elif path.startswith("/api/v1/service/query"): payload = {"id": 41, "version": 1}
            else: raise AssertionError((method, path))
            return ONLINE.SUPPORT.Response(expected[0], payload)
        client.request.side_effect = request
        execution = {"execution_id": str(uuid.uuid4()), "task_id": 51, "trigger_by": 7,
                     "status": "success", "records_read": 5, "records_written": 5, **(changes or {})}
        def transfer(*args):
            args[3].append(51)
            return 51, execution
        with (patch.object(ONLINE, "find_item", return_value={"id": 1}),
              patch.object(ONLINE, "build_item_locator", side_effect=lambda engine, item: "locator-" + str(engine)),
              patch.object(ONLINE, "build_schema_locator", return_value="parent-locator"),
              patch.object(ONLINE, "preview_rows", side_effect=[(["id"], []), (ONLINE.BANK_FIELDS, self.rows(False)),
                  (ONLINE.BANK_FIELDS, self.rows()), (ONLINE.BANK_FIELDS, self.rows())]),
              patch.object(ONLINE, "ensure_enrollment", return_value=("enrollment-bank", True)),
              patch.object(ONLINE, "ensure_detected_assessment", return_value=("assessment-bank", True, "finding-bank")),
              patch.object(ONLINE, "list_pages", return_value=[self.finding()]),
              patch.object(ONLINE, "wait_for_algorithm_projections"),
              patch.object(ONLINE, "develop_rows", return_value=self.rows()),
              patch.object(ONLINE, "service_rows", return_value=self.rows()),
              patch.object(ONLINE, "create_and_run_task", side_effect=transfer),
              patch.object(ONLINE, "wait_for_scan", return_value=str(uuid.uuid4())),
              patch.object(ONLINE, "cleanup_tasks", side_effect=ONLINE.SuiteError("cleanup blocked") if cleanup_failure else None) as cleanup):
            try:
                return ONLINE.exercise_bank_card_metadata(client, 23, 17, "security_fixture", "run-42",
                                                          {"principal_id": "7"}, time.monotonic() + 30)
            finally:
                cleanup.assert_called_once_with(client, [51])
                self.assertTrue(any(call.args[:3] == ("GET", "/api/v1/service/query/41", (404,))
                                    for call in client.request.call_args_list))

    def test_transfer_requires_new_execution_actual_counts_and_ordinary_initiator(self):
        result = self.run_case()
        self.assertEqual(set(result["owners"]), set(ONLINE.OWNER_ACTIONS))
        self.assertEqual(result["owners"]["transfer"]["records_written"], 5)
        self.assertTrue(result["task_deleted"] and result["service_deleted"])
        self.assertNotIn("6222000000000000001", json.dumps(result))
        for changes in ({"execution_id": "bad"}, {"trigger_by": 8}, {"records_read": 4},
                        {"records_written": 0}, {"task_id": 50}, {"status": "failed"}):
            with self.subTest(changes=changes), self.assertRaises(ONLINE.SuiteError):
                self.run_case(changes)
        with self.assertRaisesRegex(ONLINE.SuiteError, "owned resource cleanup failed"):
            self.run_case(cleanup_failure=True)



class ResourceValiditySnapshotTest(unittest.TestCase):
    def snapshot(self, until=None, expired=False):
        return {"validity_mode": "permanent" if until is None else "until", "expires_at": until,
            "owner_progress": [{"consumer_owner": owner,"projection_id":"projection-"+owner,"revision":"00000000000000000004",
                "published_cursor":"cursor-"+owner,"acknowledged":True,"projection_state":"active",
                "validity":"expired" if expired else "valid","expires_at":until} for owner in ONLINE.OWNER_ACTIONS]}

    def test_permanent_requires_explicit_null_for_resource_and_every_owner(self):
        value=self.snapshot()
        self.assertEqual(set(ONLINE.validity_snapshot(value,"permanent",None)),set(ONLINE.OWNER_ACTIONS))
        for target in [value]+value["owner_progress"]:
            with self.subTest(target=target.get("consumer_owner")):
                old=target.pop("expires_at")
                with self.assertRaises(ONLINE.SuiteError): ONLINE.validity_snapshot(value,"permanent",None)
                target["expires_at"]=old
        value["owner_progress"][0]["expires_at"]="2099-01-01T00:00:00Z"
        with self.assertRaises(ONLINE.SuiteError): ONLINE.validity_snapshot(value,"permanent",None)

    def test_bounded_requires_identical_owner_deadlines_and_server_expiry(self):
        until="2026-10-15T12:00:00Z"
        expected=ONLINE.utc_timestamp(until,"test deadline")
        value=self.snapshot(until,expired=True)
        ONLINE.validity_snapshot(value,"until",expected,expired=True)
        for key,bad in [("acknowledged",False),("validity","valid"),("projection_state","enrolling"),("expires_at",None),("revision","4")]:
            row=value["owner_progress"][0];old=row[key];row[key]=bad
            with self.subTest(key=key),self.assertRaises(ONLINE.SuiteError): ONLINE.validity_snapshot(value,"until",expected,expired=True)
            row[key]=old



class DevelopProtectionDiagnosticTest(unittest.TestCase):
    def valid_execution(self):
        return {"status": "failed", "metadata": {}, "error_details": {
            "category": "execution_failed", "code": "develop.query.protection_required",
            "diagnostic": {"schema_version": "addp.execution-failure/v2", "stage": "result", "reason": "data_protection_required"}}}

    def test_requires_specific_safe_diagnostic_and_absent_result(self):
        valid = self.valid_execution()
        self.assertEqual(ONLINE.validate_develop_protection_refusal(valid, "result")["diagnostic_reason"], "data_protection_required")
        for target, key, value in [
            ("execution", "status", "success"), ("execution", "metadata", {"result": {"summary": {"preview_rows": []}}}),
            ("details", "code", "develop.query.execution_failed"), ("details", "message", "private runtime value"),
            ("details", "diagnostic", None), ("diagnostic", "reason", "query_failed"),
            ("diagnostic", "stage", "preparation"), ("diagnostic", "schema_version", "old"),
            ("diagnostic", "sql", {"object": {"name": "private"}}),
        ]:
            value_execution = copy.deepcopy(valid)
            destination = value_execution if target == "execution" else value_execution["error_details"]
            if target == "diagnostic": destination = destination["diagnostic"]
            destination[key] = value
            with self.subTest(target=target,key=key), self.assertRaises(ONLINE.SuiteError):
                ONLINE.validate_develop_protection_refusal(value_execution, "result")



class TransferProtectionDiagnosticContractTests(unittest.TestCase):
    def test_service_refusal_has_only_safe_403_contract(self):
        response = ONLINE.SUPPORT.Response(403, {"error":"localized protection explanation", "error_code":"data_protection_required"})
        ONLINE.validate_service_protection_refusal(response)
        for status, payload in [(500,response.payload), (403,{"error":"generic", "error_code":"query_execution_failed"}), (403,{**response.payload,"data":[]}), (403,{**response.payload,"details":"private"}), (403,{**response.payload,"error":""})]:
            with self.subTest(status=status,payload=payload), self.assertRaises(ONLINE.SuiteError):
                ONLINE.validate_service_protection_refusal(ONLINE.SUPPORT.Response(status,payload))

    def test_precise_diagnostic_rejects_generic_or_private_failures(self):
        import copy
        value = {"status": "failed", "error_details": {"category": "execution_failed", "code": "data_protection_required", "diagnostic": {"schema_version": "addp.execution-failure/v2", "stage": "preparation", "reason": "data_protection_required"}}}
        ONLINE.validate_transfer_protection_refusal(value, "preparation", "data_protection_required")
        for target, key, bad in [("details", "diagnostic", None), ("details", "message", "private"), ("details", "code", "query_execution_failed"), ("diagnostic", "reason", "query_failed"), ("diagnostic", "stage", "result"), ("diagnostic", "sql", {"object": {"name": "private"}})]:
            mutated = copy.deepcopy(value)
            destination = mutated["error_details"]
            if target == "diagnostic": destination = destination["diagnostic"]
            destination[key] = bad
            with self.subTest(key=key), self.assertRaises(ONLINE.SuiteError):
                ONLINE.validate_transfer_protection_refusal(mutated, "preparation", "data_protection_required")

if __name__ == "__main__":
    unittest.main()
