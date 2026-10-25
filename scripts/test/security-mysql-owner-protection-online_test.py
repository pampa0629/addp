import importlib.util
import ast
import json
import os
import sys
import copy
import csv
import io
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
        for policy in self.policies:
            if policy.get("state") != "active" or policy["consumer_owner"] != owner:
                continue
            field = policy.get("assessment_id")
            if field in columns:
                if policy["algorithm"] == ONLINE.STRUCTURED_MASK_ALGORITHM:
                    rune = policy["parameters"]["mask_rune"]
                    columns[field] = {"1": rune * 11, "2": rune * 5, "3": rune * 3, "4": None, "5": None}
                else:
                    columns[field] = by_algorithm[policy["algorithm"]]
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
                                                      for owner in ONLINE.PROTECTION_OWNERS]})
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
            assert ONLINE.OWNER_ACTIONS[body["consumer_owner"]] == body["action"]
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
                    "error_details": {"category": "execution_failed", "code": "query_output_lineage_unresolved", "diagnostic": {"schema_version": "addp.execution-failure/v3", "stage": "preparation", "reason": "protection_lineage_unverified"}} if denied else {}}
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
                execution = {"execution_id": str(uuid.uuid4()), "status": "failed", "metadata": {}, "error_details": {
                    "category": "execution_failed", "code": "develop.query.protection_required",
                    "diagnostic": {"schema_version": "addp.execution-failure/v3", "stage": "preparation", "reason": "protection_lineage_unverified"}}}
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
        self.assertTrue(all(case["query_boundaries"]["diagnostic_reason"] == "protection_lineage_unverified" for case in report["cases"]))
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
        client.policies = [{"id": str(index), "state": "active", "version": "1", "consumer_owner": "manager", "assessment_id": field, "algorithm": algorithm, "parameters": parameters} for index, (field, (algorithm, parameters)) in enumerate(zip(ONLINE.SPATIAL_FIELDS[1:4], [(ONLINE.STRUCTURED_MASK_ALGORITHM, {"mask_rune":"#"}), ONLINE.ALGORITHM_CASES[1], ONLINE.ALGORITHM_CASES[2]]), 1)]
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
            self.assertEqual(restart.call_args.args[5], [str(i) for i in range(1, 13)])
            self.assertEqual({(p["consumer_owner"], p["action"]) for p in client.policies}, set(ONLINE.OWNER_ACTIONS.items()))
            self.assertEqual(len({(p["assessment_id"], p["consumer_owner"]) for p in client.policies}), 12)
            return result

    def test_spatial_algorithms_cover_four_owners_and_restore_baseline(self):
        client = SpatialClient()
        evidence = self.spatial_run(client)
        self.assertEqual(len(evidence["cases"]), 3)
        self.assertEqual({owner for _, owner in client.owner_calls}, set(ONLINE.OWNER_ACTIONS))
        self.assertEqual(len(client.owner_calls), 12)
        self.assertTrue(evidence["cases"][0]["independent_outlet_fields"])
        self.assertTrue(evidence["cases"][0]["owners"]["manager"]["restart_recovery"]["verified_before_rescan"])
        self.assertEqual(client.search_calls, [case[0] for case in ONLINE.ALGORITHM_CASES])
        for case in evidence["cases"]:
            self.assertTrue(case["owners"]["manager"]["technical_field_search"]["same_item_verified"])
        self.assertTrue(all(policy["state"] == "revoked" for policy in client.policies))
        self.assertTrue(client.service_deleted)
        self.assertEqual(len(client.deleted_tasks), 3)
        self.assertEqual({k: v for k, v in client.baseline.items() if k != "version"},
                         {k: v for k, v in client.original.items() if k != "version"})

    def test_rejects_an_outlet_receiving_another_outlets_field_rules(self):
        client = SpatialClient()
        original = client.rows
        client.rows = lambda owner: original("manager" if owner == "develop" else owner)
        with self.assertRaisesRegex(ONLINE.SuiteError, "protected value"):
            self.spatial_run(client)
        self.assertTrue(all(policy["state"] == "revoked" for policy in client.policies))
        self.assertTrue(client.service_deleted)

    def test_owner_failure_still_restores_phone_rule_and_revokes_all_policies(self):
        client = SpatialClient(fail_owner="service")
        with self.assertRaisesRegex(ONLINE.SuiteError, "injected owner failure"):
            self.spatial_run(client)
        self.assertEqual(client.baseline["parameters"], client.original["parameters"])
        self.assertEqual(client.baseline["invalid_value_effect"], "deny")
        self.assertEqual(client.baseline["allowed_algorithms"], client.original["allowed_algorithms"])
        self.assertTrue(client.service_deleted)
        self.assertEqual(len(client.policies), 12)
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
        self.assertEqual(len(client.policies), 12)
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

    def test_pre_scan_readiness_waits_for_checkpoint_but_does_not_require_an_indexed_item(self):
        client = Mock()
        client.request.side_effect = [ONLINE.SUPPORT.Response(403, {"error_code": "security_protection_required"}),
                                     ONLINE.SUPPORT.Response(503, {"error_code": "manager_search_isolated"}),
                                     ONLINE.SUPPORT.Response(200, {"data": {"results": []}})]
        with patch.object(ONLINE.time, "sleep"):
            ONLINE.wait_for_manager_search_ready(client, 17, "value_c", time.monotonic() + 5)
        self.assertEqual(client.request.call_count, 3)

    def test_pre_scan_readiness_rejects_malformed_success_and_persistent_refusal(self):
        client = Mock()
        for response in (ONLINE.SUPPORT.Response(200, {}),
                         ONLINE.SUPPORT.Response(403, {"error_code": "permission_denied"}),
                         ONLINE.SUPPORT.Response(403, {"error_code": "security_protection_required"})):
            client.request.return_value = response
            with patch.object(ONLINE.time, "monotonic", side_effect=[1, 1, 3]), patch.object(ONLINE.time, "sleep"):
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.wait_for_manager_search_ready(client, 17, "value_c", 2)

    def test_initial_field_search_has_its_own_short_deadline_after_a_single_scan(self):
        client = Mock()
        with patch.object(ONLINE.time, "monotonic", return_value=100), \
             patch.object(ONLINE, "wait_for_scan", return_value="scan") as scan, \
             patch.object(ONLINE, "find_item", return_value=self.item), \
             patch.object(ONLINE, "wait_for_technical_field", side_effect=ONLINE.SuiteError("index unavailable")) as search:
            with self.assertRaisesRegex(ONLINE.SuiteError, "index unavailable"):
                ONLINE.prepare_mongodb_source(client, 17, 1000)
        scan.assert_called_once()
        self.assertEqual(search.call_args.args[-1], 160)

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

    def test_initial_projection_warmup_and_transient_isolation_are_retried(self):
        client = Mock()
        client.request.side_effect = [ONLINE.SUPPORT.Response(403, {"error_code": "security_protection_required"}),
                                     ONLINE.SUPPORT.Response(503, {"error_code": "manager_search_isolated"}),
                                     ONLINE.SUPPORT.Response(200, {"data": {"results": [self.hit]}})]
        with patch.object(ONLINE.time, "sleep"):
            self.assertTrue(ONLINE.wait_for_technical_field(client, 17, self.item, "value_c", time.monotonic() + 5)["same_item_verified"])
        self.assertEqual(client.request.call_count, 3)
        self.assertEqual(client.request.call_args.args[2], (200, 403, 503))

    def test_other_errors_and_persistent_projection_refusal_cannot_pass(self):
        client = Mock()
        for response in (ONLINE.SUPPORT.Response(403, {"error_code": "permission_denied"}),
                         ONLINE.SUPPORT.Response(503, {"error_code": "search_unconfigured"}),
                         ONLINE.SUPPORT.Response(403, {"error_code": "security_protection_required"}),
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
    def test_browser_required_owner_catalogue_and_complete_progress_are_strict(self):
        probe = r"""
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const { source, requiredOwners } = JSON.parse(readFileSync(0, 'utf8'))
const helpers = source.slice(source.indexOf('function requiredProtectionOwners('), source.indexOf('async function verifyResourceCombinationFilters('))
const { catalogue, synchronized } = new Function(helpers + ';return { catalogue: requiredProtectionOwners, synchronized: synchronizedProtectionOwners }')()
const env = value => ({ ADDP_ONLINE_SECURITY_OUTPUT_BROWSER_CONTEXT: JSON.stringify({ required_owners: value }) })
assert.deepEqual(catalogue(env(requiredOwners)), [...requiredOwners].sort())
for (const bad of [undefined, null, [], 'graph', [requiredOwners[0], requiredOwners[0]], [null], ['bad,owner']]) {
  assert.throws(() => catalogue(env(bad)))
}
const expected = catalogue(env(requiredOwners))
const progress = requiredOwners.map(consumer_owner => ({ consumer_owner, acknowledged: true, projection_state: 'active', validity: 'valid' }))
assert.equal(synchronized(progress, expected), true)
const graph = progress.find(row => row.consumer_owner === 'graph')
assert.ok(graph)
for (const bad of [progress.filter(row => row !== graph), [...progress, graph], null, [null]]) {
  assert.equal(synchronized(bad, expected), false)
}
for (const [key, value] of [['acknowledged', false], ['acknowledged', 1], ['projection_state', 'enrolling'], ['validity', 'expired']]) {
  const changed = structuredClone(progress)
  changed.find(row => row.consumer_owner === 'graph')[key] = value
  assert.equal(synchronized(changed, expected), false)
}
"""
        result = subprocess.run(["node", "--input-type=module", "-e", probe], input=json.dumps({
            "source": (SCRIPT.resolve().parents[2] / "console/frontend/e2e/online/security-manager-export.spec.js").read_text(),
            "requiredOwners": list(ONLINE.PROTECTION_OWNERS),
        }), capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_result_context_reuses_direct_query_and_existing_target_without_authorization_writes(self):
        baseline = {"id": "9", "version": 1, "sensitive_data_type_id": "7", "security_grade_id": "2", "effect": "mask",
                    "algorithm": ONLINE.STRUCTURED_MASK_ALGORITHM,
                    "parameters": {"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"},
                    "allowed_algorithms": [ONLINE.STRUCTURED_MASK_ALGORITHM], "invalid_value_effect": "suppress", "enabled": True}
        client = Mock()
        targets = {"parent_locator": "target-parent", "locators": {ONLINE.MONGODB_TRANSFER_TARGETS[2]: "existing-target"}}
        def definitions(_, path):
            return [{"id": "7", "code": "phone", "default_security_grade_id": "2"}] if path.endswith("sensitive-data-types") else [baseline]
        with patch.object(ONLINE, "definition_array", side_effect=definitions), patch.object(ONLINE, "build_item_locator", return_value="existing-source"):
            result = ONLINE.result_browser_context(client, 3, {"fingerprint": "known"}, targets, "enrollment")
            self.assertEqual(result["baseline_body"]["invalid_value_effect"], "suppress")
            self.assertEqual(result["target_locator"], "existing-target")
            normal, denied = result["normal_payload"], result["denied_payload"]
            self.assertEqual(normal["config"]["source"], denied["config"]["source"])
            self.assertNotIn("$concat", normal["config"]["source"]["query"]["statement"])
            self.assertEqual(normal["config"]["target"]["policy"], {"apply_mode": "replace"})
            self.assertEqual(denied["config"]["target"]["policy"], {"apply_mode": "append"})
            self.assertFalse(denied["auto_scan_metadata"])
            self.assertEqual(result["expected_phones"]["1"], "138****5678")
            for change in ({"invalid_value_effect": "deny"}, {"algorithm": ONLINE.SM3_ALGORITHM}, {"parameters": {}}):
                saved = dict(baseline); baseline.update(change)
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.result_browser_context(client, 3, {}, targets, "enrollment")
                baseline.clear(); baseline.update(saved)
        client.request.assert_not_called()

    def test_result_browser_restores_baseline_and_attempts_all_task_cleanup_on_failures(self):
        probe = r"""
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const { source, requiredOwners } = JSON.parse(readFileSync(0, 'utf8'))
const ownerHelpers = source.slice(source.indexOf('function requiredProtectionOwners('), source.indexOf('async function verifyResourceCombinationFilters('))
const helper = source.slice(source.indexOf('async function verifyTransferResultProtection('), source.indexOf('async function verifyServiceTransferProtectionRefusal('))
const expect = value => ({ toBe: expected => assert.equal(value, expected), toEqual: expected => assert.deepEqual(value, expected),
  toContain: expected => assert.ok(value.includes(expected)), toBeGreaterThan: expected => assert.ok(value > expected),
  not: { toBe: expected => assert.notEqual(value, expected) } })
expect.poll = callback => ({ toBe: async expected => assert.equal(await callback(), expected) })
const failedID = '75834320-203b-4d8c-838e-15e01024d484'
const original = { sensitive_data_type_id: 7, security_grade_id: 2, effect: 'mask', algorithm: 'addp.mask.keep_prefix_suffix/v2',
  parameters: { prefix_runes: 3, suffix_runes: 4, mask_rune: '*' }, allowed_algorithms: ['addp.mask.keep_prefix_suffix/v2'],
  invalid_value_effect: 'suppress', enabled: true }
const baseRows = Array.from({ length: 7 }, (_, i) => ({ _id: String(i + 1), display_name: `person-${i + 1}`,
  nick_name: i === 6 ? null : `nickname-${i + 1}`, userInfo__phone: i === 0 ? '138****5678' : null }))
const columns = ['_id', 'display_name', 'userInfo__phone', 'nick_name']
const input = { baseline_id: '9', enrollment_id: 'source', baseline_body: original, columns, expected_phones: { '1': '138****5678' },
  normal_payload: { config: { source: { locator: 'source' }, target: { policy: { apply_mode: 'replace' } } } },
  denied_payload: { config: { source: { locator: 'source' }, target: { policy: { apply_mode: 'append' } } } } }
const run = new Function('expect', 'json', 'resolve', 'checkProtectionFailurePanel', ownerHelpers + helper + ';return verifyTransferResultProtection')(
  expect, async value => value, (...parts) => parts.join('/'), async (_, id, stage) => { assert.equal(id, failedID); assert.equal(stage, 'result') })
for (const fault of ['none', 'stage', 'target', 'writes', 'deny-response', 'restore-response', 'delete']) {
  let baseline = { ...structuredClone(original), version: 1 }, rows = structuredClone(baseRows), denied = false, injected = false
  const tasks = new Set(), deleted = [], navigations = [], screenshots = []
  const api = {
    get: async path => {
      if (path.includes('protection-baselines')) return structuredClone(baseline)
      if (path.includes('protection-enrollments')) return { validity_mode: 'permanent', expires_at: null,
        owner_progress: requiredOwners.map(consumer_owner => ({ consumer_owner, acknowledged: true,
          projection_state: 'active', validity: 'valid', projection_id: consumer_owner, revision: String(baseline.version), published_cursor: String(baseline.version) })) }
      if (path.includes('/manager/preview')) return { data: { columns, rows: structuredClone(rows) } }
      return { status: () => 404 }
    },
    put: async (_, { data }) => {
      assert.equal(data.version, baseline.version)
      baseline = { ...structuredClone(data), version: data.version + 1 }
      if (!injected && ((fault === 'deny-response' && data.invalid_value_effect === 'deny') ||
          (fault === 'restore-response' && data.invalid_value_effect === 'suppress'))) { injected = true; throw new Error('lost response') }
      return structuredClone(baseline)
    },
    post: async () => { const id = 41 + tasks.size; tasks.add(id); return { id } },
    delete: async path => { const id = Number(path.split('/').at(-1)); deleted.push(id); tasks.delete(id)
      if (fault === 'delete' && deleted.length === 1) throw new Error('cleanup failure')
      return { ok: () => true } }
  }
  const runTransfer = async (status, id) => {
    assert.ok(tasks.has(id))
    if (status === 'failed') {
      assert.equal(baseline.invalid_value_effect, 'deny'); denied = true
      if (fault === 'target') rows[0].display_name = 'changed'
      return { execution_id: failedID, records_written: fault === 'writes' ? 1 : 0, bytes_written: 0, error_details: { category: 'execution_failed', code: 'data_protection_required',
        diagnostic: { schema_version: 'addp.execution-failure/v3', stage: fault === 'stage' ? 'preparation' : 'result', reason: 'data_protection_required' } } }
    }
    assert.equal(baseline.invalid_value_effect, 'suppress'); rows = structuredClone(baseRows)
    return { execution_id: denied ? 'recovery' : 'baseline', records_read: 7, records_written: 7 }
  }
  const panel = { getByRole: () => ({ getAttribute: async () => `/monitor/executions?execution_id=${failedID}` }) }
  const page = { frameLocator: () => ({ locator: () => panel }), url: () => 'http://localhost:5170/', screenshot: async value => screenshots.push(value.path) }
  const action = () => run(page, { ADDP_ONLINE_SECURITY_OUTPUT_BROWSER_CONTEXT: JSON.stringify({ result_protection: input, required_owners: requiredOwners }), ADDP_ONLINE_ARTIFACT_DIR: '/tmp' },
    async path => navigations.push(path), () => api, runTransfer)
  if (fault === 'none') {
    const report = await action(); assert.equal(report.transfer_result_stage, 'result'); assert.equal(report.result_browser_residual_resources, 0)
    assert.equal(report.result_transfer_execution_id, failedID); assert.equal(navigations.length, 2); assert.equal(screenshots.length, 2)
  } else await assert.rejects(action)
  const { version, ...restored } = baseline
  assert.deepEqual(restored, original, fault)
  assert.deepEqual(deleted.sort(), [41, 42], fault)
  assert.equal(tasks.size, 0, fault)
}
"""
        result = subprocess.run(["node", "--input-type=module", "-e", probe], input=json.dumps({
            "source": (SCRIPT.resolve().parents[2] / "console/frontend/e2e/online/security-manager-export.spec.js").read_text(),
            "requiredOwners": list(ONLINE.PROTECTION_OWNERS),
        }), capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_main_waits_for_protected_email_search_before_starting_browser(self):
        environment = {
            'ADDP_ONLINE_TEST': '1', 'ADDP_ONLINE_TEST_TENANT_ID': '2',
            'ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID': '23', 'ADDP_ONLINE_TEST_ENGINE_ID': '17',
            'ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE': 'security_fixture',
            'ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID': '29',
            'ADDP_ONLINE_TEST_RUN_ID': 'run-42', 'GATEWAY_URL': 'http://127.0.0.1:8000',
            'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'fixture-token',
        }
        environment.update({key: "private-role-fixture" for key in ONLINE.ROLE_ENVIRONMENT})
        for unavailable in (False, True):
            scenario_report = FourOutletReasonMatrixTest().report()
            mongo_report = scenario_report.pop('mongodb_algorithms')
            mongo_report['enrollment_id'] = 'mongo-enrollment'
            scenario_report.update(identity={'principal_id': '7'}, fixture={'security_enrollment_id': 'enrollment'})
            with (
                self.subTest(unavailable=unavailable),
                patch.dict(os.environ, environment, clear=True),
                patch.object(sys, 'argv', [str(SCRIPT)]),
                patch.object(ONLINE.signal, 'signal'),
                patch.object(ONLINE.SUPPORT, 'require_hosted_security_lifecycle'),
                patch.object(ONLINE, 'validate_user_identity', return_value={}) as identity,
                patch.object(ONLINE, 'wait_for_manager_search_ready', create=True) as initial_readiness,
                patch.object(ONLINE, 'prepare_mongodb_source', return_value={'scan_execution_id': 'prepared', 'item': {'id': 1}}) as prepare,
                patch.object(ONLINE, 'prepare_mongodb_transfer_targets', return_value={}) as targets,
                patch.object(ONLINE, 'run_scenario', return_value=scenario_report) as scenario,
                patch.object(ONLINE, 'exercise_mongodb_algorithms', return_value=mongo_report) as mongo,
                patch.object(ONLINE, 'protection_reason_matrix', wraps=ONLINE.protection_reason_matrix) as matrix,
                patch.object(ONLINE, 'result_browser_context', return_value={'fixture': 'result'}),
                patch.object(ONLINE, 'find_item', return_value={'fingerprint': 'source', 'node_id': 12}),
                patch.object(ONLINE, 'build_item_locator', return_value='source-locator'),
                patch.object(ONLINE, 'wait_for_technical_field') as readiness,
                patch.object(ONLINE, 'run_export_browser') as browser,
                patch('builtins.print'),
            ):
                order = Mock()
                order.attach_mock(identity, 'identity')
                order.attach_mock(initial_readiness, 'initial_readiness')
                order.attach_mock(prepare, 'prepare')
                order.attach_mock(targets, 'targets')
                order.attach_mock(scenario, 'scenario')
                order.attach_mock(mongo, 'mongo')
                order.attach_mock(matrix, 'matrix')
                readiness.return_value = {'same_item_verified': True}
                if unavailable:
                    readiness.side_effect = ONLINE.SuiteError('protected field not searchable')
                    with self.assertRaises(ONLINE.SuiteError):
                        ONLINE.main()
                    browser.assert_not_called()
                else:
                    def launch(*args):
                        self.assertTrue(all(key not in os.environ for key in ONLINE.ROLE_ENVIRONMENT))
                        self.assertEqual(scenario.call_args.kwargs["role_environment"],
                                         {key: "private-role-fixture" for key in ONLINE.ROLE_ENVIRONMENT})
                        readiness.assert_called_once()
                        self.assertEqual(scenario_report['protection_reason_matrix']['coverage_counts'], {'runtime': 20, 'protocol_only': 8})
                        context = args[3]
                        self.assertEqual(context["principal_id"], "7")
                        self.assertEqual(context["result_protection"], {"fixture": "result"})
                        self.assertEqual(context["enrollment_id"], "enrollment")
                        self.assertFalse(context["service_payload"]["public_access"])
                        self.assertFalse(context["transfer_payload"]["enabled"])
                        self.assertEqual(context["transfer_payload"]["config"]["source"]["locator"], args[2])
                        self.assertEqual(context["transfer_payload"]["config"]["runtime"], {"boundary": "bounded"})
                        return self.report()
                    browser.side_effect = launch
                    self.assertEqual(ONLINE.main(), 0)
                    self.assertEqual([call[0] for call in order.mock_calls], ['identity', 'initial_readiness', 'prepare', 'targets', 'scenario', 'mongo', 'matrix'])
                    self.assertEqual(readiness.call_args.args[1:4], (23, {'fingerprint': 'source', 'node_id': 12}, 'email'))
                matrix.assert_called_once_with(scenario_report)
                self.assertIs(scenario_report['mongodb_algorithms'], mongo_report)

    def report(self):
        return {
            "schema_version": "addp.security-manager-export-browser/v9", "result": "passed",
            "run_id": "run-42", "tenant_id": "2", "records": 5,
            "execution_id": "53834320-203b-4d8c-838e-15e01024d484",
            "protection_transfer_execution_id": "64834320-203b-4d8c-838e-15e01024d484",
            "result_transfer_execution_id": "75834320-203b-4d8c-838e-15e01024d484",
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
            "additional_protection_histories_verified": True, "additional_protection_monitors_verified": True, "additional_protection_refusal_count": 3,
            "service_normal_output_verified": True, "service_refusal_http_status": 403,
            "service_refusal_visible": True, "service_old_preview_cleared": True, "service_restored_output_verified": True,
            "transfer_refusal_diagnostic_verified": True, "transfer_refusal_visible": True, "transfer_monitor_same_execution": True,
            "transfer_target_unchanged": True, "output_protection_permanent_restored": True,
            "output_browser_created_resources": 2, "output_browser_deleted_resources": 2, "output_browser_residual_resources": 0,
            "expected_service_refusal_403": 1,
            "transfer_result_stage": "result", "transfer_result_diagnostic_verified": True,
            "transfer_result_visible": True, "transfer_result_monitor_same_execution": True,
            "transfer_result_target_unchanged": True, "transfer_result_records_written": 0,
            "transfer_result_baseline_restored": True, "transfer_result_recovery_verified": True,
            "result_browser_created_resources": 2, "result_browser_deleted_resources": 2, "result_browser_residual_resources": 0,
            "browser_warning_errors": 0, "failed_business_responses": 0, "anonymous_refresh_401": 1,
        }

    def test_rejects_missing_provenance_hidden_detail_wrong_run_and_browser_errors(self):
        valid = self.report()
        self.assertEqual(ONLINE.validate_export_browser_report(valid, "run-42", "2"), valid)
        for key, value in (
            ("additional_protection_histories_verified", False), ("additional_protection_monitors_verified", False), ("additional_protection_refusal_count", 2), ("additional_protection_refusal_count", "3"), ("additional_protection_refusal_count", True),
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
            ("transfer_result_stage", "preparation"), ("transfer_result_diagnostic_verified", False),
            ("transfer_result_visible", False), ("transfer_result_monitor_same_execution", False),
            ("transfer_result_target_unchanged", False), ("transfer_result_records_written", 1),
            ("transfer_result_records_written", False), ("transfer_result_baseline_restored", False),
            ("transfer_result_recovery_verified", False), ("result_browser_created_resources", 0),
            ("result_browser_deleted_resources", 1), ("result_browser_residual_resources", 1),
            ("result_transfer_execution_id", "bad"), ("result_transfer_execution_id", None),
            ("result_transfer_execution_id", "00000000-0000-0000-0000-000000000000"),
            ("result_transfer_execution_id", "53834320-203b-4d8c-838e-15e01024d484"),
            ("result_transfer_execution_id", "64834320-203b-4d8c-838e-15e01024d484"),
            ("schema_version", "addp.security-manager-export-browser/v7"),
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
const helper = source.slice(source.indexOf('  async function runTransfer('), source.indexOf('  async function updateValidity('))
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
            environment.update({key: "role-fixture-private" for key in ONLINE.ROLE_ENVIRONMENT})
            for exit_code in (1, 0):
                path.write_text(json.dumps(self.report()))
                with patch.object(ONLINE.subprocess, "run", return_value=Mock(returncode=exit_code, stdout="", stderr="")):
                    with self.subTest(exit_code=exit_code), self.assertRaises(ONLINE.SuiteError):
                        ONLINE.run_export_browser(artifact, environment, "source", {"enrollment_id": "fixture"})
                self.assertFalse(path.exists())
            def browser(command, **kwargs):
                self.assertTrue(all(key not in kwargs["env"] for key in ONLINE.ROLE_ENVIRONMENT))
                self.assertIn("e2e/online/security-manager-export.spec.js", command)
                self.assertEqual(kwargs["env"]["ADDP_ONLINE_SECURITY_EXPORT_LOCATOR"], "source")
                self.assertEqual(json.loads(kwargs["env"]["ADDP_ONLINE_SECURITY_OUTPUT_BROWSER_CONTEXT"]),
                                 {"enrollment_id": "fixture", "required_owners": list(ONLINE.PROTECTION_OWNERS)})
                path.write_text(json.dumps(self.report()))
                for name in ("security-manager-protected-preview.png", "security-manager-field-search.png",
                             "security-manager-export.png", "security-manager-export-monitor.png",
                             "security-resource-combination-filters.png", "security-resource-empty-intersection.png",
                             "security-resource-filter-reset.png", "security-develop-protection-refusal.png",
                             "security-develop-protection-monitor.png", "security-service-protection-refusal.png",
                             "security-service-protection-restored.png", "security-transfer-protection-refusal.png",
                             "security-transfer-protection-monitor.png", "security-transfer-result-refusal.png", "security-transfer-result-monitor.png", "security-develop-not_ready-history.png", "security-develop-not_ready-monitor.png", "security-develop-operation_unsupported-history.png", "security-develop-operation_unsupported-monitor.png", "security-develop-schema_mismatch-history.png", "security-develop-schema_mismatch-monitor.png"):
                    (artifact / name).write_bytes(b"test screenshot")
                return Mock(returncode=0, stdout="", stderr="")
            with patch.object(ONLINE.subprocess, "run", side_effect=browser):
                self.assertEqual(ONLINE.run_export_browser(artifact, environment, "source", {"enrollment_id": "fixture"}), self.report())

            for missing in ("security-manager-protected-preview.png", "security-manager-field-search.png",
                            "security-resource-combination-filters.png", "security-resource-empty-intersection.png",
                            "security-resource-filter-reset.png", "security-develop-protection-refusal.png",
                             "security-develop-protection-monitor.png", "security-service-protection-refusal.png",
                             "security-service-protection-restored.png", "security-transfer-protection-refusal.png",
                             "security-transfer-protection-monitor.png", "security-transfer-result-refusal.png", "security-transfer-result-monitor.png", "security-develop-not_ready-history.png", "security-develop-not_ready-monitor.png", "security-develop-operation_unsupported-history.png", "security-develop-operation_unsupported-monitor.png", "security-develop-schema_mismatch-history.png", "security-develop-schema_mismatch-monitor.png"):
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
        self.search_ready = patch.object(ONLINE, "wait_for_manager_search_ready", create=True).start()
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
        order = Mock()
        order.attach_mock(self.search_ready, "ready")
        order.attach_mock(self.scan, "scan")
        ONLINE.initialize_postgres_read_grants(authorizer, consumer, 2, 17)
        self.assertEqual([call[0] for call in order.mock_calls], ["ready", "scan"])
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

    def test_initial_checkpoint_failure_stops_before_scan_or_grant_writes(self):
        authorizer, consumer = self.source_clients()
        self.search_ready.side_effect = ONLINE.SuiteError("Manager startup refused")
        with self.assertRaisesRegex(ONLINE.SuiteError, "Manager startup refused"):
            ONLINE.initialize_postgres_read_grants(authorizer, consumer, 2, 17)
        self.scan.assert_not_called()
        authorizer.request.assert_not_called()

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

    def source_item(self):
        return {"id": 1, "attributes": {"type_info": {"table": {"name": ONLINE.BANK_SOURCE,
            "fields": [{"name": name, "type": "bigint" if name in {"id", "credit_card_number"} else "string"} for name in ONLINE.BANK_FIELDS]}}}}

    def enrollment(self):
        return {"state": "active", "last_discovered_at": "2026-10-13T10:00:00+08:00",
                "owner_progress": [{"consumer_owner": owner, "projection_state": "active",
                    "projection_id": str(uuid.uuid4()), "revision": "00000000000000000001",
                    "published_cursor": str(index), "acknowledged": True,
                    "acknowledged_at": "2026-10-13T02:00:01Z", "validity": "valid",
                    "rules": [{"action": action, "effect": "deny" if owner == "graph" else "mask"}]}
                    for index, (owner, action) in enumerate((*ONLINE.OWNER_ACTIONS.items(), ("graph", "graph_read")), 1)]}

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

    def mixed_rows(self):
        return [{**row, "copied_card": row[ONLINE.BANK_FIELD], "copied_code": row["customer_code"]} for row in self.rows()]

    def test_mixed_wildcard_verifies_every_sensitive_copy(self):
        self.assertTrue(ONLINE.assert_bank_card_mixed_rows(self.mixed_rows(), "Develop")["sensitive_copies_verified"])
        for change in [lambda row: row.update(copied_card=ONLINE.BANK_SOURCE_VALUES["1"]),
                       lambda row: row.pop("copied_card"), lambda row: row.update(copied_code="wrong"),
                       lambda row: row.update(hidden_original="unexpected")]:
            rows = self.mixed_rows()
            change(rows[0])
            with self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_bank_card_mixed_rows(rows, "Develop")
        rows = self.mixed_rows()
        rows[4].pop(ONLINE.BANK_FIELD)
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.assert_bank_card_mixed_rows(rows, "Develop")

    def test_sql_page_contract_and_cursor_fail_closed(self):
        payload = ONLINE.bank_card_sql_service_payload("sql-page", 23, self.source_item(), "locator")
        self.assertEqual(payload["config_type"], "sql")
        self.assertNotIn("locator", payload["data_config"])
        self.assertEqual([field["name"] for field in payload["output_contract"]["table"]["fields"]], ONLINE.BANK_FIELDS + ["copied_card", "copied_code"])
        source = self.source_item()
        source["attributes"]["type_info"]["table"]["fields"].pop()
        with self.assertRaises(ONLINE.SuiteError): ONLINE.bank_card_sql_service_payload("sql-page", 23, source, "locator")
        for change in [lambda pages: pages[0]["data"].pop(),
                       lambda pages: pages[0]["page"].update(has_more=1),
                       lambda pages: pages[1]["page"].update(next_cursor="page-1"),
                       lambda pages: pages[2]["page"].update(next_cursor=1),
                       lambda pages: pages[2]["data"][0].update(id="4"),
                       lambda pages: pages[0]["data"][0].update(copied_card=ONLINE.BANK_SOURCE_VALUES["1"])]:
            rows = self.mixed_rows()
            pages = [{"data": rows[:2], "page": {"has_more": True, "next_cursor": "page-1"}},
                     {"data": rows[2:4], "page": {"has_more": True, "next_cursor": "page-2"}},
                     {"data": rows[4:], "page": {"has_more": False}}]
            change(pages)
            client = Mock()
            client.request.side_effect = [ONLINE.SUPPORT.Response(200, page) for page in pages]
            with self.assertRaises(ONLINE.SuiteError): ONLINE.exercise_bank_card_sql_pages(client, "sql-page")

    def test_computed_outputs_require_exact_refusal_empty_history_and_live_controls(self):
        payload = ONLINE.bank_card_computed_service_payload("computed", 23, self.source_item(), "locator")
        self.assertEqual(payload["data_config"]["default_fields"], ["id", "computed"])
        self.assertEqual([f["name"] for f in payload["output_contract"]["table"]["fields"]], ["id", "computed"])
        self.assertIn("substring(bank_card_number,1,100)", payload["sql_query"])
        histories = {}
        queries = []
        def execution(*args):
            execution_id = str(uuid.uuid4())
            row = {"execution_id": execution_id, "status": "failed", "metadata": {},
                "error_details": {"code": "develop.query.protection_required", "category": "data_protection",
                    "diagnostic": {"schema_version": "addp.execution-failure/v3", "stage": "preparation", "reason": "protection_computed_output_unsupported"}}}
            histories[execution_id] = row
            queries.append(args[4])
            return copy.deepcopy(row)
        client = Mock()
        client.request.side_effect = lambda method, path, expected: ONLINE.SUPPORT.Response(200, copy.deepcopy(histories[path.rsplit("/", 1)[1]]))
        controls = [[{"count": "5"}], [{"next_id": i} for i in range(2, 7)]]
        with (patch.object(ONLINE, "develop_execution", side_effect=execution),
              patch.object(ONLINE, "develop_rows", side_effect=controls)):
            result = ONLINE.exercise_bank_card_computed_outputs(client, 23, "locator", time.monotonic()+10)
        self.assertEqual(set(result["cases"]), {"substring", "coalesce", "case", "cast", "subquery_alias", "union"})
        self.assertEqual(len(queries), 6)
        self.assertTrue(all(case["history_result_absent"] for case in result["cases"].values()))
        self.assertEqual(result["count_star"], 5)
        self.assertEqual(result["unprotected_calculation_rows"], 5)
        for failure in ["success", "result", "wrong_history", "duplicate_id", "control", "generic_reason"]:
            client = Mock()
            fixed = execution(None, None, None, None, "test")
            def request(method, path, expected):
                row = copy.deepcopy(histories[path.rsplit("/", 1)[1]])
                if failure == "result": row["metadata"]["result"] = {"summary": {"preview_rows": []}}
                if failure == "wrong_history": row["execution_id"] = str(uuid.uuid4())
                return ONLINE.SUPPORT.Response(200, row)
            client.request.side_effect = request
            def run(*args):
                row = copy.deepcopy(fixed) if failure == "duplicate_id" else execution(*args)
                if failure == "success": row["status"] = "success"
                if failure == "generic_reason": row["error_details"]["diagnostic"]["reason"] = "data_protection_required"
                return row
            with (patch.object(ONLINE, "develop_execution", side_effect=run),
                  patch.object(ONLINE, "develop_rows", side_effect=[[{"count": 4}], controls[1]] if failure == "control" else controls)):
                with self.subTest(failure=failure), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.exercise_bank_card_computed_outputs(client, 23, "locator", time.monotonic()+10)

    def run_case(self, changes=None, cleanup_failure=False):
        client = Mock()
        def request(method, path, expected, body=None):
            if path.startswith("/api/query/") and expected == (403,):
                payload = {"error": "protected output denied", "error_code": "data_protection_required"}
            elif path.startswith("/api/query/"):
                cursor = body["page"].get("cursor", "")
                index = {"": 0, "page-1": 1, "page-2": 2}[cursor]
                payload = {"data": self.mixed_rows()[index * 2:index * 2 + 2], "page": {"has_more": index < 2}}
                if index < 2: payload["page"]["next_cursor"] = "page-" + str(index + 1)
            elif path.endswith("/sensitive-data-types"): payload = [{"code": "bank_card", "id": "17"}]
            elif path.endswith("/enrollment-bank"): payload = self.enrollment()
            elif path.endswith("/assessment-bank"): payload = self.assessment()
            elif path.startswith("/api/v1/service/query"): payload = {"id": 43 if body and body.get("sql_query", "").startswith("SELECT id,substring(") else 42 if body and body.get("config_type") == "sql" else 41, "version": 1}
            else: raise AssertionError((method, path))
            return ONLINE.SUPPORT.Response(expected[0], payload)
        client.request.side_effect = request
        execution = {"execution_id": str(uuid.uuid4()), "task_id": 51, "trigger_by": 7,
                     "status": "success", "records_read": 5, "records_written": 5, **(changes or {})}
        def transfer(*args):
            args[3].append(51)
            return 51, execution
        with (patch.object(ONLINE, "find_item", return_value=self.source_item()),
              patch.object(ONLINE, "build_item_locator", side_effect=lambda engine, item: "locator-" + str(engine)),
              patch.object(ONLINE, "build_schema_locator", return_value="parent-locator"),
              patch.object(ONLINE, "preview_rows", side_effect=[(["id"], []), (ONLINE.BANK_FIELDS, self.rows(False)),
                  (ONLINE.BANK_FIELDS, self.rows()), (ONLINE.BANK_FIELDS, self.rows())]),
              patch.object(ONLINE, "ensure_enrollment", return_value=("enrollment-bank", True)),
              patch.object(ONLINE, "ensure_detected_assessment", return_value=("assessment-bank", True, "finding-bank")),
              patch.object(ONLINE, "list_pages", return_value=[self.finding()]),
              patch.object(ONLINE, "wait_for_algorithm_projections"),
              patch.object(ONLINE, "exercise_bank_card_computed_outputs", return_value={"protected_results_absent": True}),
              patch.object(ONLINE, "exercise_enrolling_refusal", return_value={"diagnostic_reason": "protection_not_ready"}),
              patch.object(ONLINE, "bank_outlet_refusals", return_value={}),
              patch.object(ONLINE, "postgres_query_transfer_refusal", return_value={}),
              patch.object(ONLINE, "transfer_refusal_with_history", return_value={}),
              patch.object(ONLINE, "exercise_bank_card_refusal_causes", return_value={"schema_restored": True}),
              patch.object(ONLINE, "develop_rows", side_effect=lambda *args: self.mixed_rows() if "copied_card" in args[4] else self.rows()) as develop,
              patch.object(ONLINE, "service_rows", return_value=self.rows()),
              patch.object(ONLINE, "create_and_run_task", side_effect=transfer),
              patch.object(ONLINE, "wait_for_scan", return_value=str(uuid.uuid4())),
              patch.object(ONLINE, "cleanup_tasks", side_effect=ONLINE.SuiteError("cleanup blocked") if cleanup_failure else None) as cleanup):
            try:
                return ONLINE.exercise_bank_card_metadata(client, 23, 17, "security_fixture", "run-42",
                                                          {"principal_id": "7"}, time.monotonic() + 30, {"diagnostic_reason": "protection_not_ready"})
            finally:
                self.assertEqual(develop.call_count, 7)
                self.assertEqual([call.args[4] for call in develop.call_args_list][1:], [
                    "SELECT * FROM bank_card_accounts ORDER BY id",
                    "SELECT b.* FROM bank_card_accounts b ORDER BY b.id",
                    "SELECT b.*,b.bank_card_number AS copied_card,b.customer_code AS copied_code FROM bank_card_accounts b ORDER BY b.id",
                    "SELECT b.bank_card_number AS copied_card,b.customer_code AS copied_code,b.* FROM bank_card_accounts b ORDER BY b.id",
                    "SELECT w.* FROM (SELECT b.*,b.bank_card_number AS copied_card,b.customer_code AS copied_code FROM bank_card_accounts b WHERE b.id>=1 ORDER BY b.id LIMIT 100) w ORDER BY w.id LIMIT 100",
                    "SELECT " + ",".join("w." + field for field in ONLINE.BANK_FIELDS + ["copied_card", "copied_code"]) + " FROM (SELECT b.*,b.bank_card_number AS copied_card,b.customer_code AS copied_code FROM bank_card_accounts b) w ORDER BY w.id LIMIT 100"])
                cleanup.assert_called_once_with(client, [51])
                self.assertTrue(any(call.args[:3] == ("GET", "/api/v1/service/query/41", (404,))
                                    for call in client.request.call_args_list))
                self.assertTrue(any(call.args[:3] == ("GET", "/api/v1/service/query/42", (404,))
                                    for call in client.request.call_args_list))
                self.assertTrue(any(call.args[:3] == ("GET", "/api/v1/service/query/43", (404,))
                                    for call in client.request.call_args_list))

    def test_transfer_requires_new_execution_actual_counts_and_ordinary_initiator(self):
        result = self.run_case()
        self.assertEqual(set(result["owners"]), set(ONLINE.OWNER_ACTIONS))
        self.assertEqual(set(result["develop_wildcard"]), {"unqualified", "table_alias"})
        for output in result["develop_wildcard"].values():
            self.assertEqual(output, result["owners"]["develop"])
        self.assertEqual(set(result["develop_mixed_wildcard"]), {"copy_after_star", "copy_before_star"})
        for output in result["develop_mixed_wildcard"].values():
            self.assertTrue(output["sensitive_copies_verified"] and output["non_sensitive_copy_verified"])
        self.assertEqual(set(result["develop_page_wildcard"]), {"outer_star", "outer_direct"})
        self.assertEqual(result["service_sql_page"]["page_sizes"], [2, 2, 1])
        self.assertTrue(result["service_sql_page"]["no_duplicate_or_missing_rows"])
        self.assertTrue(result["sql_service_deleted"])
        self.assertTrue(result["computed_service_deleted"])
        self.assertTrue(result["computed_outputs"]["service_refusal"]["result_absent"])
        self.assertEqual(result["temporary_execution_resources"], 11)
        self.assertEqual(result["owners"]["transfer"]["records_written"], 5)
        self.assertTrue(result["task_deleted"] and result["service_deleted"])
        self.assertNotIn("6222000000000000001", json.dumps(result))
        for changes in ({"execution_id": "bad"}, {"trigger_by": 8}, {"records_read": 4},
                        {"records_written": 0}, {"task_id": 50}, {"status": "failed"}):
            with self.subTest(changes=changes), self.assertRaises(ONLINE.SuiteError):
                self.run_case(changes)
        with self.assertRaisesRegex(ONLINE.SuiteError, "owned resource cleanup failed"):
            self.run_case(cleanup_failure=True)



class RequiredOwnerConvergenceTest(unittest.TestCase):
    def enrollment(self):
        return {"state": "active", "owner_progress": [
            {"consumer_owner": owner, "projection_state": "active", "acknowledged": True}
            for owner in ONLINE.PROTECTION_OWNERS]}

    def test_graph_must_acknowledge_before_the_required_owner_barrier_opens(self):
        current = self.enrollment()
        pending = copy.deepcopy(current)
        next(row for row in pending["owner_progress"] if row["consumer_owner"] == "graph")["acknowledged"] = False
        client = Mock()
        client.request.side_effect = [ONLINE.SUPPORT.Response(200, pending), ONLINE.SUPPORT.Response(200, current)]
        with patch.object(ONLINE.time, "sleep") as sleep:
            ONLINE.wait_for_algorithm_projections(client, "resource", time.monotonic() + 5)
        self.assertEqual(client.request.call_count, 2)
        sleep.assert_called_once_with(1)

    def test_validity_case_waits_for_existing_owner_revisions_before_its_initial_snapshot(self):
        client = Mock()
        with patch.object(ONLINE, "wait_for_algorithm_projections", side_effect=ONLINE.SuiteError("initial barrier pending")):
            with self.assertRaisesRegex(ONLINE.SuiteError, "initial barrier pending"):
                ONLINE.exercise_resource_validity(client, "resource", 23, 17, "source", "parent", "target",
                                                  "service", "task", {"principal_id": "7"}, 2, [])
        client.request.assert_not_called()

    def test_missing_duplicate_unacknowledged_or_nonactive_graph_never_passes(self):
        for mutation in ("missing", "duplicate", "unacknowledged", "enrolling"):
            current = self.enrollment()
            graph = next(row for row in current["owner_progress"] if row["consumer_owner"] == "graph")
            if mutation == "missing":
                current["owner_progress"].remove(graph)
            elif mutation == "duplicate":
                current["owner_progress"].append(copy.deepcopy(graph))
            elif mutation == "unacknowledged":
                graph["acknowledged"] = False
            else:
                graph["projection_state"] = "enrolling"
            client = Mock()
            client.request.return_value = ONLINE.SUPPORT.Response(200, current)
            with self.subTest(mutation=mutation), patch.object(ONLINE.time, "monotonic", side_effect=[1, 3]), patch.object(ONLINE.time, "sleep"):
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.wait_for_algorithm_projections(client, "resource", 2)


class ServiceRoleCursorClient:
    """Stateful formal-API fixture; no product masking/cursor implementation."""
    def __init__(self, fault=None):
        self.fault = fault
        self.calls = []
        self.policy = None
        self.service = None
        self.deleted = False
        self.cursor = "synthetic-owned-cursor-do-not-archive"
        self.target = {"owner_module": "meta", "resource_type": "data_item", "resource_identity": "sha256:bank-fixture"}
        self.baseline = [{"id": "1", "version": "1", "algorithm": ONLINE.STRUCTURED_MASK_ALGORITHM}]
        self.option = None

    def request(self, method, path, expected, body=None, *, raw_response=False):
        self.calls.append((method, path, copy.deepcopy(body)))
        response = ONLINE.SUPPORT.Response
        if path == "/api/v1/system/auth/subject-roles":
            now = ONLINE.datetime.now(ONLINE.timezone.utc)
            roles = {"tenant_id": "2", "actor_principal_id": "7", "role_keys": ["online.security_mysql_owner_protection"],
                     "observed_at": now.isoformat(), "expires_at": (now + ONLINE.timedelta(minutes=1)).isoformat()}
            if self.fault == "foreign_user":
                roles["actor_principal_id"] = "8"
            if self.fault == "expired_roles":
                roles["expires_at"] = (now - ONLINE.timedelta(seconds=1)).isoformat()
            if self.fault == "role_changed" and self.policy and self.policy["state"] == "revoked":
                roles["role_keys"] = ["online.other"]
            return response(200, roles)
        if path.startswith("/api/v1/security/protection-enrollments/"):
            revision = 1 if not self.policy else (2 if self.policy["state"] == "active" else 3)
            if self.fault == "stale_projection" and revision == 3:
                revision = 2
            return response(200, {"state": "active", "owner_progress": [{"consumer_owner": "service",
                "projection_state": "active", "projection_id": "service-projection", "revision": f"{revision:020}",
                "published_cursor": "projection-" + str(revision), "acknowledged": True}]})
        if path == "/api/v1/security/protection-baselines":
            value = copy.deepcopy(self.baseline)
            if self.fault == "baseline_changed" and self.policy and self.policy["state"] == "revoked":
                value[0]["version"] = "2"
            return response(200, value)
        if path == "/api/v1/security/protection-policies":
            assert "id" not in body["role_options"][0], "new role option identities belong to Security"
            self.option = dict(body["role_options"][0], id="11111111-1111-4111-8111-111111111111")
            self.policy = {**copy.deepcopy(body), "id": "owned-policy", "version": 1, "current_revision": 1, "state": "active"}
            self.policy["current"] = {"role_options": [copy.deepcopy(self.option)]}
            return response(201, copy.deepcopy(self.policy))
        if path == "/api/v1/security/protection-policies/owned-policy":
            if method == "DELETE":
                self.policy.update(state="revoked", version=2, current_revision=2)
            current = copy.deepcopy(self.policy)
            if self.fault == "cleanup_policy" and method == "GET" and self.policy["state"] == "revoked":
                current["state"] = "active"
            return response(200, current)
        if path.startswith("/api/v1/service/query-protection-options?"):
            field = {"target": self.target, "component_key": ONLINE.BANK_FIELD, "policy_id": "owned-policy",
                     "policy_revision": 1, "assessment_revision": 4,
                     "options": [{"id": self.option["id"], "name": self.option["name"]}]}
            if self.fault == "foreign_source":
                field["target"] = dict(self.target, resource_identity="sha256:other")
            return response(200, {"fields": [field]})
        if path == "/api/v1/service/query":
            self.service = {**copy.deepcopy(body), "id": 701, "version": 1, "service_version": "chosen-version", "status": "active"}
            return response(201, copy.deepcopy(self.service))
        if path == "/api/v1/service/query/701":
            if method == "DELETE":
                self.deleted = True
                return response(200, {"message": "deleted"})
            if method == "PUT":
                assert body["version"] == self.service["version"]
                self.service.update(copy.deepcopy({key: value for key, value in body.items() if key != "data_config"}))
                self.service["version"] += 1
                if "data_config" in body:
                    assert set(body["data_config"]) == {"protection_choices"}, "published source facts are read-only"
                    self.service["data_config"].update(copy.deepcopy(body["data_config"]))
                    self.service["service_version"] = "default-version"
            result = copy.deepcopy(self.service)
            if self.fault == "silent_clear" and self.policy["state"] == "revoked" and method == "GET":
                result["data_config"]["protection_choices"] = []
            if self.fault == "retained_version_changed" and self.policy["state"] == "revoked" and method == "GET":
                result["version"] += 1
            return response(404 if self.deleted else 200, result)
        if path.startswith("/api/query/"):
            if self.fault == "interrupt":
                raise KeyboardInterrupt("interrupted owned scenario")
            chosen = bool(self.service["data_config"].get("protection_choices"))
            if chosen and self.policy["state"] == "revoked":
                error = {"error": "Selected masking method is no longer available", "error_code": "query_protection_choice_invalid"}
                status = 409
                if self.fault == "wrong_error":
                    error["error_code"] = "invalid_query_cursor"
                if self.fault == "wrong_status":
                    status = 403
                if self.fault == "denial_rows":
                    error["data"] = []
                return response(status, error)
            cursor = body["page"].get("cursor")
            if cursor:
                assert cursor == self.cursor
            ids = ["5"] if cursor else ["1", "2", "3", "4"]
            if self.fault == "duplicate_rows":
                ids = ["1", "1", "3", "4"]
            rows = []
            for key in ids:
                original = ONLINE.BANK_SOURCE_VALUES[key]
                value = ("*" * len(original) if original else None) if chosen else ONLINE.BANK_MASKED[key]
                if self.fault == "plaintext":
                    value = original
                rows.append({"id": int(key), "customer_code": "CUST-1000" + key, ONLINE.BANK_FIELD: value,
                             "bank_account_no": "account-" + key, "card_id": "card-" + key,
                             "bank_card_verified": "verified-" + key, "credit_card_number": 100 + int(key)})
            if raw_response:
                output = io.StringIO()
                writer = csv.DictWriter(output, fieldnames=ONLINE.BANK_FIELDS)
                writer.writeheader()
                if self.fault == "csv_nil":
                    for row in rows:
                        if row[ONLINE.BANK_FIELD] is None:
                            row[ONLINE.BANK_FIELD] = "<nil>"
                writer.writerows(rows)
                return response(200, output.getvalue().encode())
            return response(200, {"data": rows, "service_version": "chosen-version" if chosen else "default-version",
                "page": {"has_more": not bool(cursor), "next_cursor": self.cursor if not cursor else ""}})
        raise AssertionError((method, path, expected))


class ServiceRoleCursorRevocationTest(unittest.TestCase):
    def run_case(self, client):
        source = {"id": 1, "full_name": "fixture.bank_card_accounts", "item_type": "table", "fingerprint": "sha256:bank-fixture"}
        with (patch.object(ONLINE, "find_item", return_value=source),
              patch.object(ONLINE, "list_pages", return_value=[])):
            return ONLINE.exercise_service_role_cursor_revocation(client, 23, "fixture",
                {"enrollment_id": "enrollment", "assessment_id": "assessment"},
                {"principal_id": "7", "tenant_id": "2"}, "run-42", time.monotonic() + 30)

    def test_same_cursor_valid_before_revoke_both_formats_refuse_and_explicit_default_recovers(self):
        client = ServiceRoleCursorClient()
        report = self.run_case(client)
        self.assertEqual(report["first_page"]["rows"], 4)
        self.assertEqual(report["valid_old_cursor_continuation"]["ids"], ["5"])
        self.assertEqual(report["old_cursor_refusals"], {format_name: {
            "http_status": 409, "error_code": "query_protection_choice_invalid", "result_absent": True}
            for format_name in ("json", "csv")})
        queries = [body for method, path, body in client.calls if path.startswith("/api/query/")]
        self.assertEqual([body["page"].get("cursor") for body in queries[2:6]], [client.cursor] * 4)
        self.assertEqual([body["format"] for body in queries[2:6]], ["json", "csv", "json", "csv"])
        self.assertNotIn("cursor", queries[6]["page"])
        self.assertEqual(report["explicit_removal_default_recovery"]["service_version"], "default-version")
        self.assertEqual(report["explicit_removal_default_recovery"]["total_rows"], 5)
        self.assertTrue(report["user_roles_unchanged"] and report["pure_stop_retained_invalid_choice"])
        self.assertTrue(client.deleted and report["service_deleted"] and report["policy_revoked"])
        self.assertEqual(report["residual_resources"], 0)
        self.assertEqual((report["created_resources"], report["deleted_resources"]), (1, 1))
        self.assertEqual((report["created_policies"], report["revoked_policies"], report["final_policy_revision"]), (1, 1, 2))
        encoded = json.dumps(report)
        self.assertNotIn(client.cursor, encoded)
        for value in ONLINE.BANK_SOURCE_VALUES.values():
            if value:
                self.assertNotIn(value, encoded)
        self.assertEqual(client.policy["parameters"], {"prefix_runes": 1, "suffix_runes": 1, "mask_rune": "*"})
        self.assertEqual(client.option["decision"]["parameters"], {"prefix_runes": 0, "suffix_runes": 0, "mask_rune": "*"})

    def test_corrupt_results_wrong_denial_and_silent_fallback_fail_and_clean_owned_resources(self):
        for fault in ("foreign_source", "duplicate_rows", "plaintext", "csv_nil", "wrong_error", "wrong_status",
                      "denial_rows", "silent_clear", "retained_version_changed", "role_changed", "baseline_changed"):
            client = ServiceRoleCursorClient(fault)
            with self.subTest(fault=fault), self.assertRaises(ONLINE.SuiteError):
                self.run_case(client)
            self.assertEqual(client.policy["state"], "revoked")
            if client.service:
                self.assertTrue(client.deleted)

    def test_current_user_role_binding_and_expiry_fail_before_mutation(self):
        for fault in ("foreign_user", "expired_roles"):
            client = ServiceRoleCursorClient(fault)
            with self.subTest(fault=fault), self.assertRaises(ONLINE.SuiteError):
                self.run_case(client)
            self.assertIsNone(client.policy)

    def test_interruption_cleans_and_propagates_without_success(self):
        client = ServiceRoleCursorClient("interrupt")
        with self.assertRaises(KeyboardInterrupt):
            self.run_case(client)
        self.assertTrue(client.deleted)
        self.assertEqual(client.policy["state"], "revoked")

    def test_policy_cleanup_failure_still_deletes_service_and_fails(self):
        client = ServiceRoleCursorClient("cleanup_policy")
        with self.assertRaisesRegex(ONLINE.SuiteError, "cleanup failed"):
            self.run_case(client)
        self.assertTrue(client.deleted)

    def test_new_ack_requires_same_projection_new_revision_and_new_cursor(self):
        previous = {"projection_id": "id", "revision": "00000000000000000001", "published_cursor": "one"}
        valid = dict(previous, revision="00000000000000000002", published_cursor="two", acknowledged=True)
        for changes in ({"acknowledged": False}, {"published_cursor": "one"}, {"revision": previous["revision"]}):
            with self.subTest(changes=changes), patch.object(ONLINE, "role_projection", return_value=dict(valid, **changes)), \
                    patch.object(ONLINE.time, "monotonic", side_effect=[0, 2]), patch.object(ONLINE.time, "sleep"), \
                    self.assertRaises(ONLINE.SuiteError):
                ONLINE.wait_role_projection(None, "enrollment", previous, 1)
        with patch.object(ONLINE, "role_projection", return_value=dict(valid, projection_id="other")), self.assertRaises(ONLINE.SuiteError):
            ONLINE.wait_role_projection(None, "enrollment", previous, time.monotonic() + 1)


class MembershipClient:
    def __init__(self, backend, token):
        self.backend, self.token, self.base_url, self.timeout = backend, token, "http://127.0.0.1:8000", 1

    def request(self, method, path, expected, body=None, **kwargs):
        response = self.backend.for_user(self.token, method, path, expected, body, **kwargs)
        if response.status not in expected:
            raise ONLINE.SuiteError("controlled fixture HTTP status differs")
        return response


class MembershipBackend(ServiceRoleCursorClient):
    def __init__(self, fault=None):
        super().__init__()
        self.member_fault = fault
        self.revoked = False
        self.events = []
        self.assignment = {"id": "42", "principal_id": "7", "membership_id": "8", "principal_type": "user",
            "role_key": ONLINE.SERVICE_ELIGIBILITY_ROLE, "scope_type": "tenant", "username": "external-online-security-role-reader", "status": "active"}

    def for_user(self, token, method, path, expected, body=None, **kwargs):
        self.events.append((token, method, path, copy.deepcopy(body)))
        response = ONLINE.SUPPORT.Response
        fault = self.member_fault
        if path == "/api/v1/system/auth/context":
            if token == "reader-old" and self.revoked and fault != "stale_session_live":
                return response(401, {"error": "old session revoked", "error_code": "unauthorized"})
            permissions = (ONLINE.SERVICE_ROLE_CONTROLLER_PERMISSIONS if token == "controller"
                           else ONLINE.SERVICE_ROLE_READER_PERMISSIONS)
            principal_id = "8" if token == "controller" else "7"
            if fault == "foreign_relogin" and token == "reader-fresh":
                principal_id = "10"
            if fault == "controller_is_reader" and token == "controller":
                principal_id = "7"
            granted = sorted(permissions)
            if fault == "permission_lost" and token == "reader-fresh":
                granted.remove("service.data_read.execute")
            return response(200, {"principal": {"id": principal_id, "type": "user"},
                "context": {"type": "tenant", "tenant_id": "2", "tenant_membership_id": "8" if token != "controller" else "9"},
                "token": {"type": "first_party_access_token"},
                "authorization": {"authorization_version": "2" if token == "reader-fresh" else "1",
                    "role_assignments": [{"role_key": "tenant.administrator" if token == "controller" else "online.functional", "permissions": granted}]}})
        if path == "/api/v1/system/auth/subject-roles":
            original = super().request(method, path, expected, body)
            original.payload["role_keys"] = ["online.functional"]
            if not self.revoked or fault == "kept_role":
                original.payload["role_keys"].append(ONLINE.SERVICE_ELIGIBILITY_ROLE)
            return original
        if path == "/api/v1/system/tenant/role_assignments/42/revoke":
            assert token == "controller" and method == "POST"
            self.revoked = True
            self.assignment["status"] = "revoked"
            return response(200, copy.deepcopy(self.assignment))
        if path == "/api/v1/system/tenant/role_assignments/42":
            assert token == "controller"
            if self.revoked and fault == "cleanup_iam":
                raise ONLINE.SuiteError("controlled IAM cleanup failure")
            assignment = copy.deepcopy(self.assignment)
            if fault == "wrong_assignment":
                assignment["principal_id"] = "17"
            return response(200, assignment)
        if path == "/api/v1/system/login":
            assert token == "" and body == {"username": "external-online-security-role-reader", "password": "private-password"}
            return response(200, {"next_action": "session_issued", "session": {"access_token": "reader-fresh"}})
        if path.startswith("/api/v1/service/query-protection-options?") and self.revoked:
            return response(200, {"fields": []})
        if path.startswith("/api/query/"):
            assert token in {"reader-old", "reader-fresh"}, "controller/owner must never consume role results"
            if fault == "interrupt":
                raise KeyboardInterrupt("controlled role acceptance interruption")
            if self.revoked and self.service["data_config"].get("protection_choices"):
                assert token == "reader-fresh"
                status = {"denial_401": 401, "denial_409": 409}.get(fault, 403)
                payload = {"error": "Current signed-in user is ineligible", "error_code": "query_protection_roles_unavailable"}
                if fault == "wrong_code":
                    payload["error_code"] = "data_protection_required"
                if fault == "denial_rows":
                    payload["data"] = []
                return response(status, payload)
        value = super().request(method, path, expected, body, **kwargs)
        if self.revoked and path == "/api/v1/security/protection-policies/owned-policy" and method == "GET" and fault == "policy_changed":
            value.payload["current_revision"] += 1
        if self.revoked and path.startswith("/api/v1/security/protection-enrollments/") and fault == "projection_changed":
            value.payload["owner_progress"][0]["revision"] = "00000000000000000003"
        if self.revoked and path == "/api/v1/service/query/701" and method == "GET" and fault == "silent_clear":
            value.payload["data_config"]["protection_choices"] = []
        return value


class ServiceRoleMembershipRevocationTest(unittest.TestCase):
    def run_case(self, backend):
        source = {"id": 1, "full_name": "fixture.bank_card_accounts", "item_type": "table", "fingerprint": "sha256:bank-fixture"}
        environment = dict(zip(ONLINE.SERVICE_ROLE_ENVIRONMENT,
            ("reader-old", "external-online-security-role-reader", "private-password", "42", "controller")))
        factory = lambda base, token, timeout: MembershipClient(backend, token)
        with (patch.object(ONLINE, "GatewayClient", side_effect=factory),
              patch.object(ONLINE, "find_item", return_value=source), patch.object(ONLINE, "list_pages", return_value=[])):
            return ONLINE.exercise_service_role_membership_revocation(MembershipClient(backend, "governor"), 23, "fixture",
                {"enrollment_id": "enrollment", "assessment_id": "assessment"}, {"principal_id": "9", "tenant_id": "2"},
                "run-42", time.monotonic() + 30, environment)

    def test_401_is_separate_from_fresh_user_exact_403_and_default_recovery(self):
        backend = MembershipBackend()
        report = self.run_case(backend)
        self.assertEqual(report["old_session_http_status"], 401)
        self.assertEqual(report["identity_before"]["permissions"], report["identity_after"]["permissions"])
        self.assertEqual(report["role_keys_after"], ["online.functional"])
        self.assertEqual(report["assignment_after"]["status"], "revoked")
        self.assertEqual(set(report["fresh_user_refusals"]), {"json", "csv", "no_cursor"})
        for refusal in report["fresh_user_refusals"].values():
            self.assertEqual(refusal, {"http_status": 403, "error_code": "query_protection_roles_unavailable", "result_absent": True})
        self.assertTrue(report["policy_projection_and_saved_choice_unchanged_at_refusal"])
        self.assertEqual(report["explicit_default_recovery"]["total_rows"], 5)
        self.assertTrue(backend.deleted and backend.revoked and report["eligibility_assignment_revoked"])
        self.assertEqual(backend.policy["state"], "revoked")
        self.assertEqual(sum(path.endswith("/42/revoke") for _, _, path, _ in backend.events), 1)
        outputs = [(token, body["page"].get("cursor")) for token, _, path, body in backend.events if path.startswith("/api/query/")]
        self.assertEqual([cursor for _, cursor in outputs[2:6]], [backend.cursor] * 4)
        self.assertEqual([token for token, _ in outputs[4:7]], ["reader-fresh"] * 3)
        for secret in ("private-password", "reader-old", "reader-fresh", "controller", backend.cursor):
            self.assertNotIn('"'+secret+'"', json.dumps(report))

    def test_wrong_status_permissions_roles_or_mutated_protection_cannot_pass(self):
        for fault in ("stale_session_live", "foreign_relogin", "permission_lost", "kept_role", "policy_changed",
                      "projection_changed", "silent_clear", "denial_401", "denial_409", "wrong_code", "denial_rows"):
            backend = MembershipBackend(fault)
            with self.subTest(fault=fault), self.assertRaises(ONLINE.SuiteError):
                self.run_case(backend)
            self.assertTrue(backend.revoked and backend.deleted)
            self.assertEqual(backend.policy["state"], "revoked")

    def test_wrong_assignment_or_controller_never_mutates_identity(self):
        for fault in ("controller_is_reader", "wrong_assignment"):
            backend = MembershipBackend(fault)
            with self.subTest(fault=fault), self.assertRaises(ONLINE.SuiteError):
                self.run_case(backend)
            self.assertFalse(backend.revoked)
            self.assertIsNone(backend.policy)

    def test_interruption_revokes_only_owned_assignment_and_cleans_service(self):
        backend = MembershipBackend("interrupt")
        with self.assertRaises(KeyboardInterrupt):
            self.run_case(backend)
        self.assertTrue(backend.revoked and backend.deleted)

    def test_iam_cleanup_failure_does_not_skip_policy_and_service_cleanup(self):
        backend = MembershipBackend("cleanup_iam")
        with self.assertRaisesRegex(ONLINE.SuiteError, "cleanup failed"):
            self.run_case(backend)
        self.assertTrue(backend.deleted)
        self.assertEqual(backend.policy["state"], "revoked")

    def test_revoked_policy_is_reactivated_by_versioned_update_not_duplicate_create(self):
        client = Mock()
        client.request.return_value = ONLINE.SUPPORT.Response(200, {"id": "previous", "current_revision": 3})
        previous = {"id": "previous", "version": 2, "state": "revoked", "assessment_id": "assessment",
                    "consumer_owner": "service", "action": "service_execute"}
        with patch.object(ONLINE, "list_pages", return_value=[previous]):
            ONLINE.create_role_policy(client, "assessment", [ONLINE.SERVICE_ELIGIBILITY_ROLE])
        method, path, expected, body = client.request.call_args.args
        self.assertEqual((method, path, expected, body["version"]), ("PUT", "/api/v1/security/protection-policies/previous", (200,), 2))
        self.assertNotIn("assessment_id", body)
        self.assertNotIn("id", body["role_options"][0])


class RoleExpiryBackend(ServiceRoleCursorClient):
    def __init__(self, fault=None):
        super().__init__()
        self.now = ONLINE.datetime.now(ONLINE.timezone.utc)
        self.expiry_fault = fault
        self.expired = False
        self.timed = None
        self.events = []
        self.previous = {"id": "42", "principal_id": "7", "membership_id": "8", "principal_type": "user", "role_id": "20",
            "role_key": ONLINE.SERVICE_ELIGIBILITY_ROLE, "scope_type": "tenant", "username": "external-online-security-role-reader", "status": "revoked"}

    def wait(self, expires, deadline):
        self.events.append(("wait", expires, deadline, None))
        if self.expiry_fault == "interrupt_wait":
            raise KeyboardInterrupt("controlled timed-role interruption")
        if self.expiry_fault == "premature_wait":
            return
        self.now = expires + ONLINE.timedelta(seconds=2)
        self.expired = True
        if self.expiry_fault == "revoked_instead":
            self.timed["status"] = "revoked"
            self.timed["revoked_at"] = self.now.isoformat()

    def for_user(self, token, method, path, expected, body=None, **kwargs):
        self.events.append((token, method, path, copy.deepcopy(body)))
        response = ONLINE.SUPPORT.Response
        fault = self.expiry_fault
        if path == "/api/v1/system/login":
            assert token == "" and body["username"] == "external-online-security-role-reader"
            return response(200, {"next_action": "session_issued", "session": {"access_token": "reader-active" if self.timed else "reader-initial"}})
        if path == "/api/v1/system/auth/context":
            if self.expired and fault == "session401":
                return response(401, {"error": "not a role refusal", "error_code": "unauthorized"})
            controller = token == "controller"
            permissions = set(ONLINE.SERVICE_ROLE_EXPIRY_CONTROLLER_PERMISSIONS if controller else ONLINE.SERVICE_ROLE_READER_PERMISSIONS)
            if self.expired and fault == "permission_lost" and not controller:
                permissions.remove("service.data_read.execute")
            version = "1" if token == "reader-initial" else ("3" if self.expired and fault == "version_changed" else "2")
            return response(200, {"principal": {"type": "user", "id": "8" if controller else "7"},
                "context": {"type": "tenant", "tenant_id": "2", "tenant_membership_id": "9" if controller else "8"},
                "token": {"type": "first_party_access_token", "expires_at": (self.now + ONLINE.timedelta(seconds=30 if fault == "short_session" else 3600)).isoformat()},
                "authorization": {"authorization_version": version, "role_assignments": [{
                    "role_key": "tenant.administrator" if controller else "online.functional", "permissions": sorted(permissions)}]}})
        if path == "/api/v1/system/auth/subject-roles":
            keys = ["online.functional"]
            if self.timed and (not self.expired or fault == "role_survived"):
                keys.append(ONLINE.SERVICE_ELIGIBILITY_ROLE)
            until = ONLINE.utc_timestamp(self.timed["valid_until"], "controlled deadline") if len(keys) == 2 else self.now + ONLINE.timedelta(hours=1)
            return response(200, {"tenant_id": "2", "actor_principal_id": "7", "role_keys": keys,
                "observed_at": self.now.isoformat(), "expires_at": until.isoformat()})
        if path == "/api/v1/system/tenant/role_assignments/42":
            return response(200, copy.deepcopy(self.previous))
        if path == "/api/v1/system/tenant/role_assignments":
            assert token == "controller" and method == "POST" and body["role_ids"] == ["20"] and body["membership_id"] == "8"
            self.timed = dict(self.previous, id="43", status="active", effective_state="effective", valid_until=body["valid_until"], revoked_at=None)
            return response(201, [copy.deepcopy(self.timed)])
        if path == "/api/v1/system/tenant/role_assignments/43":
            value = copy.deepcopy(self.timed)
            value["effective_state"] = "expired" if self.expired else "effective"
            if self.expired and fault == "extended_deadline":
                value["valid_until"] = (self.now + ONLINE.timedelta(minutes=1)).isoformat()
            return response(200, value)
        if path == "/api/v1/system/tenant/role_assignments/43/revoke":
            self.timed["status"] = "revoked"
            return response(200, copy.deepcopy(self.timed))
        if path.startswith("/api/query/"):
            assert token == "reader-active", "only the same ordinary Access Token may consume results"
            if self.expired and self.service["data_config"].get("protection_choices"):
                status = 409 if fault == "denial409" else 403
                value = {"error": "Current User lacks role eligibility", "error_code": "query_protection_roles_unavailable"}
                if fault == "wrong_code": value["error_code"] = "data_protection_required"
                if fault == "denial_data": value["data"] = []
                return response(status, value)
        value = super().request(method, path, expected, body, **kwargs)
        if self.expired and fault == "policy_changed" and path == "/api/v1/security/protection-policies/owned-policy" and method == "GET":
            value.payload["current_revision"] += 1
        if self.expired and fault == "projection_changed" and path.startswith("/api/v1/security/protection-enrollments/"):
            value.payload["owner_progress"][0]["revision"] = "00000000000000000003"
        if self.expired and fault == "choice_cleared" and path == "/api/v1/service/query/701" and method == "GET":
            value.payload["data_config"]["protection_choices"] = []
        return value


class ServiceRoleNaturalExpiryTest(unittest.TestCase):
    def run_case(self, backend):
        class Clock(ONLINE.datetime):
            @classmethod
            def now(cls, tz=None): return backend.now
        source = {"id": 1, "full_name": "fixture.bank_card_accounts", "item_type": "table", "fingerprint": "sha256:bank-fixture"}
        environment = dict(zip(ONLINE.SERVICE_ROLE_ENVIRONMENT,
            ("unused-old-reader", "external-online-security-role-reader", "private-password", "42", "controller")))
        factory = lambda base, token, timeout: MembershipClient(backend, token)
        with (patch.object(ONLINE, "GatewayClient", side_effect=factory), patch.object(ONLINE, "datetime", Clock),
              patch.object(ONLINE, "wait_protection_deadline", side_effect=backend.wait),
              patch.object(ONLINE, "find_item", return_value=source), patch.object(ONLINE, "list_pages", return_value=[])):
            return ONLINE.exercise_service_role_membership_expiry(MembershipClient(backend, "governor"), 23, "fixture",
                {"enrollment_id": "enrollment", "assessment_id": "assessment"}, {"principal_id": "9", "tenant_id": "2"},
                "run-42", time.monotonic() + 300, environment)

    def test_same_session_survives_role_deadline_but_old_cursor_and_new_query_refuse(self):
        backend = RoleExpiryBackend()
        report = self.run_case(backend)
        self.assertEqual(report["identity_before"], report["identity_after"])
        self.assertTrue(report["same_access_session_valid"] and report["natural_expiry_verified"])
        self.assertEqual(report["assignment_after"]["status"], "active")
        self.assertEqual(report["assignment_after"]["effective_state"], "expired")
        self.assertEqual(report["assignment_before"]["valid_until"], report["assignment_after"]["valid_until"])
        self.assertEqual(report["timed_assignment_cleanup"], "expired")
        self.assertEqual(report["explicit_default_recovery"]["total_rows"], 5)
        for refusal in report["same_session_refusals"].values():
            self.assertEqual(refusal, {"http_status": 403, "error_code": "query_protection_roles_unavailable", "result_absent": True})
        self.assertFalse(any(str(path).endswith("/revoke") for _, _, path, _ in backend.events))
        wait_index = next(i for i, item in enumerate(backend.events) if item[0] == "wait")
        self.assertEqual(sum(path == "/api/v1/system/login" for _, _, path, _ in backend.events), 2)
        self.assertFalse(any(path == "/api/v1/system/login" for _, _, path, _ in backend.events[wait_index+1:]))
        queries = [(token, body["page"].get("cursor")) for token, _, path, body in backend.events if str(path).startswith("/api/query/")]
        self.assertEqual([cursor for _, cursor in queries[2:6]], [backend.cursor] * 4)
        self.assertEqual({token for token, _ in queries}, {"reader-active"})
        self.assertTrue(backend.deleted)
        for secret in (backend.cursor, "private-password", "reader-active", "reader-initial"):
            self.assertNotIn('"'+secret+'"', json.dumps(report))

    def test_session_failure_revocation_permission_or_protection_change_cannot_mimic_expiry(self):
        for fault in ("session401", "version_changed", "permission_lost", "role_survived", "premature_wait", "revoked_instead",
                      "extended_deadline", "short_session", "policy_changed", "projection_changed", "choice_cleared",
                      "denial409", "wrong_code", "denial_data"):
            backend = RoleExpiryBackend(fault)
            with self.subTest(fault=fault), self.assertRaises(ONLINE.SuiteError):
                self.run_case(backend)
            self.assertEqual(backend.policy["state"], "revoked")
            if backend.service: self.assertTrue(backend.deleted)

    def test_interruption_revokes_live_timed_assignment_without_touching_previous_revocation(self):
        backend = RoleExpiryBackend("interrupt_wait")
        with self.assertRaises(KeyboardInterrupt): self.run_case(backend)
        self.assertEqual(backend.timed["status"], "revoked")
        self.assertTrue(backend.deleted)
        revocations = [path for _, _, path, _ in backend.events if str(path).endswith("/revoke")]
        self.assertEqual(revocations, ["/api/v1/system/tenant/role_assignments/43/revoke"])

    def test_cleanup_crosses_deadline_only_accepts_typed_expired_conflict_and_never_retries(self):
        identity = {"principal_id": "7", "membership_id": "8"}
        expiry = ONLINE.datetime.now(ONLINE.timezone.utc)
        payload = dict(RoleExpiryBackend().previous, id="43", status="active", valid_until=expiry.isoformat(), effective_state="effective")
        for code in ("role_assignment_expired", "unrelated_conflict"):
            client = Mock()
            client.request.side_effect = [ONLINE.SUPPORT.Response(200, payload), ONLINE.SUPPORT.Response(409, {"error_code": code}),
                                         ONLINE.SUPPORT.Response(200, dict(payload, effective_state="expired"))]
            if code == "role_assignment_expired":
                self.assertEqual(ONLINE.cleanup_timed_service_assignment(client, identity, "43", "20", expiry), "expired")
            else:
                with self.assertRaises(ONLINE.SuiteError): ONLINE.cleanup_timed_service_assignment(client, identity, "43", "20", expiry)
            self.assertEqual(sum(call.args[0] == "POST" for call in client.request.call_args_list), 1)


class DevelopRoleExpiryBackend(RoleExpiryBackend):
    def __init__(self, fault=None):
        super().__init__(fault)
        self.previous.update(principal_id="11", membership_id="12")
        self.executions = {}

    def for_user(self, token, method, path, expected, body=None, **kwargs):
        response = ONLINE.SUPPORT.Response
        if path.startswith("/api/v1/develop/executions"):
            self.events.append((token, method, path, copy.deepcopy(body)))
            assert token == "reader-active", "Develop must use the exact same ordinary session"
            if method == "POST":
                assert body["execution_config"] == {"engine_id": 23}
                assert body["content"]["query_type"] == "sql"
                assert body["content"]["query"] == "SELECT " + ",".join(ONLINE.BANK_FIELDS) + " FROM bank_card_accounts ORDER BY id"
                selected = bool(body["content"].get("protection_choices"))
                execution_id = str(ONLINE.uuid.UUID(int=len(self.executions) + 1))
                value = {"execution_id": execution_id, "status": "success",
                    "execution_config": {"content": copy.deepcopy(body["content"])}, "metadata": {}}
                if selected and self.expired:
                    value.update(status="failed", error_details={"category": "protection", "code": "develop.query.protection_required",
                        "diagnostic": {"schema_version": "addp.execution-failure/v3", "stage": "preparation", "reason": "protection_role_choice_unavailable"}})
                    if self.expiry_fault == "wrong_reason": value["error_details"]["diagnostic"]["reason"] = "protection_expired"
                    if self.expiry_fault == "failed_data": value["metadata"]["result"] = {}
                else:
                    rows = []
                    for key in ("1", "2", "3", "4", "5"):
                        rows.append({"id": key, "customer_code": "CUST-1000"+key, ONLINE.BANK_FIELD: ONLINE.BANK_MASKED[key],
                            "bank_account_no": "account-"+key, "card_id": "card-"+key, "bank_card_verified": "verified-"+key,
                            "credit_card_number": str(100+int(key))})
                    value["metadata"]["result"] = {"columns": ONLINE.BANK_FIELDS, "summary": {"preview_rows": rows}}
                self.executions[execution_id] = value
                return response(200, {"execution_id": execution_id})
            execution_id = path.split("/")[5]
            value = copy.deepcopy(self.executions[execution_id])
            selected = bool(value["execution_config"]["content"].get("protection_choices"))
            if path.endswith("/result") and selected:
                if self.expired:
                    error = {"error": "role eligibility expired", "error_code": "query_protection_roles_unavailable"}
                    if self.expiry_fault == "wrong_delivery_code": error["error_code"] = "query_protection_choice_invalid"
                    if self.expiry_fault == "delivery_data": error["metadata"] = {}
                    return response(401 if self.expiry_fault == "delivery401" else 409, error)
                for key, row in zip(("1", "2", "3", "4", "5"), value["metadata"]["result"]["summary"]["preview_rows"]):
                    raw = ONLINE.BANK_SOURCE_VALUES[key]
                    row[ONLINE.BANK_FIELD] = "*"*len(raw) if raw else None
                if self.expiry_fault == "selected_missing_field": value["metadata"]["result"]["summary"]["preview_rows"][3].pop(ONLINE.BANK_FIELD)
                if self.expiry_fault == "default_instead": value["metadata"]["result"]["summary"]["preview_rows"][0][ONLINE.BANK_FIELD] = ONLINE.BANK_MASKED["1"]
            if self.expired and self.expiry_fault == "history_raw" and value["status"] == "success":
                value["metadata"]["result"]["summary"]["preview_rows"][0][ONLINE.BANK_FIELD] = ONLINE.BANK_SOURCE_VALUES["1"]
            if self.expired and self.expiry_fault == "snapshot_cleared": value["execution_config"]["content"]["protection_choices"] = []
            if self.expired and self.expiry_fault == "history_reason_changed" and value["status"] == "failed":
                value["error_details"]["diagnostic"]["stage"] = "result"
            if self.expiry_fault == "missing_columns": value["metadata"]["result"].pop("columns")
            return response(200, value)
        original_path = path
        if path.startswith("/api/v1/develop/query-protection-options?"):
            assert ONLINE.urllib.parse.parse_qs(ONLINE.urllib.parse.urlsplit(path).query)["engine_id"] == ["23"]
            path = path.replace("/develop/", "/service/")
        if path == "/api/v1/system/login":
            assert body["username"] == ONLINE.DEVELOP_ROLE_USERNAME
            body = dict(body, username="external-online-security-role-reader")
        value = super().for_user(token, method, path, expected, body, **kwargs)
        if original_path == "/api/v1/system/auth/context" and token != "controller" and value.status == 200:
            permissions = set(ONLINE.DEVELOP_ROLE_READER_PERMISSIONS)
            if self.expired and self.expiry_fault == "permission_lost": permissions.remove("develop.data_read.execute")
            value.payload["authorization"]["role_assignments"][0]["permissions"] = sorted(permissions)
        if original_path == "/api/v1/system/tenant/role_assignments":
            self.timed.update(principal_id="7", membership_id="8", username=ONLINE.DEVELOP_ROLE_USERNAME)
            value.payload = [copy.deepcopy(self.timed)]
        if original_path.startswith("/api/v1/security/protection-enrollments/"):
            value.payload["owner_progress"][0]["consumer_owner"] = "develop"
        return value


class DevelopRoleNaturalExpiryTest(unittest.TestCase):
    def run_case(self, backend):
        class Clock(ONLINE.datetime):
            @classmethod
            def now(cls, tz=None): return backend.now
        source = {"id": 1, "full_name": "fixture.bank_card_accounts", "item_type": "table", "fingerprint": "sha256:bank-fixture"}
        environment = dict(zip(ONLINE.SERVICE_ROLE_ENVIRONMENT,
            ("unused", "external-online-security-role-reader", "private-password", "42", "controller")))
        environment.update(ADDP_ONLINE_SECURITY_DEVELOP_ROLE_USERNAME=ONLINE.DEVELOP_ROLE_USERNAME,
                           ADDP_ONLINE_SECURITY_DEVELOP_ROLE_PASSWORD="private-develop-password")
        with (patch.object(ONLINE, "GatewayClient", side_effect=lambda base, token, timeout: MembershipClient(backend, token)),
              patch.object(ONLINE, "datetime", Clock), patch.object(ONLINE, "wait_protection_deadline", side_effect=backend.wait),
              patch.object(ONLINE, "find_item", return_value=source), patch.object(ONLINE, "list_pages", return_value=[])):
            return ONLINE.exercise_develop_role_membership_expiry(MembershipClient(backend, "governor"), 23, "fixture",
                {"enrollment_id": "enrollment", "assessment_id": "assessment"}, {"principal_id": "9", "tenant_id": "2"},
                time.monotonic()+300, environment)

    def test_same_session_old_live_delivery_and_new_execution_refuse_history_remains_default(self):
        backend = DevelopRoleExpiryBackend()
        report = self.run_case(backend)
        self.assertEqual(report["identity_before"], report["identity_after"])
        self.assertEqual(report["old_live_result_refusal"], {"http_status":409,"error_code":"query_protection_roles_unavailable","result_absent":True})
        self.assertEqual(report["new_query_refusal"]["diagnostic_reason"], "protection_role_choice_unavailable")
        self.assertTrue(report["history_before"]["default_mask"] and report["history_after"]["default_mask"])
        self.assertTrue(report["selected_live_result"]["selected_full_mask"])
        self.assertTrue(report["explicit_default_recovery"]["live"]["default_mask"])
        self.assertEqual(report["assignment_after"]["effective_state"], "expired")
        self.assertEqual(report["timed_assignment_cleanup"], "expired")
        self.assertEqual(len(backend.executions),3)
        self.assertFalse(any(str(path).endswith("/revoke") for _,_,path,_ in backend.events))
        self.assertEqual(sum(path=="/api/v1/system/login" for _,_,path,_ in backend.events),2)
        wait = next(i for i,event in enumerate(backend.events) if event[0]=="wait")
        self.assertFalse(any(path=="/api/v1/system/login" for _,_,path,_ in backend.events[wait+1:]))
        for secret in ("private-develop-password", "reader-active", "reader-initial", *filter(None, ONLINE.BANK_SOURCE_VALUES.values())):
            self.assertNotIn('"'+secret+'"',json.dumps(report,ensure_ascii=False))
        self.assertEqual(backend.policy["state"],"revoked")
        self.assertFalse(backend.service)

    def test_auth_role_protection_diagnostic_history_and_silent_fallback_cannot_fake_expiry(self):
        for fault in ("session401","version_changed","permission_lost","role_survived","premature_wait","revoked_instead",
                      "extended_deadline","short_session","policy_changed","projection_changed","default_instead",
                      "wrong_delivery_code","delivery_data","delivery401","wrong_reason","failed_data","history_raw",
                      "snapshot_cleared","history_reason_changed","missing_columns","selected_missing_field"):
            with self.subTest(fault=fault), self.assertRaises(ONLINE.SuiteError):
                self.run_case(DevelopRoleExpiryBackend(fault))

    def test_interruption_revokes_only_own_live_assignment_and_policy(self):
        backend = DevelopRoleExpiryBackend("interrupt_wait")
        with self.assertRaises(KeyboardInterrupt): self.run_case(backend)
        self.assertEqual(backend.timed["status"],"revoked")
        self.assertEqual(backend.previous["status"],"revoked")
        self.assertEqual(backend.policy["state"],"revoked")
        self.assertEqual([path for _,_,path,_ in backend.events if str(path).endswith("/revoke")],
                         ["/api/v1/system/tenant/role_assignments/43/revoke"])


class TransferRoleExpiryBackend(DevelopRoleExpiryBackend):
    def __init__(self, fault=None):
        super().__init__(fault)
        self.task = None
        self.task_history = []
        self.sessions = {}
        self.target_rows = self.rows(False)
        self.task_deleted = False

    def rows(self, selected):
        result = []
        for key in ("1","2","3","4","5"):
            source = ONLINE.BANK_SOURCE_VALUES[key]
            value = ("*"*len(source) if source else None) if selected else ONLINE.BANK_MASKED[key]
            result.append({"id":key,"customer_code":"CUST-1000"+key,ONLINE.BANK_FIELD:value,"bank_account_no":"account-"+key,
                "card_id":"card-"+key,"bank_card_verified":"verified-"+key,"credit_card_number":str(100+int(key))})
        return result

    def refusal(self, code):
        value={"error":"qualification expired","error_code":code}
        if self.expiry_fault=="denial_data":value["data"]=[]
        if self.expiry_fault=="wrong_transfer_code" and code.startswith("transfer_"):value["error_code"]="transfer_protection_choice_invalid"
        if self.expiry_fault=="wrong_bff_code" and code.startswith("query_"):value["error_code"]="query_protection_roles_unavailable"
        return ONLINE.SUPPORT.Response(401 if self.expiry_fault=="denial401" else 409,value)

    def for_user(self, token, method, path, expected, body=None, **kwargs):
        response=ONLINE.SUPPORT.Response
        task_path="/api/v1/transfer/task-definitions/501"
        if path.startswith("/api/v1/transfer/"):
            self.events.append((token,method,path,copy.deepcopy(body)))
            assert token=="reader-active", "Transfer task must retain the exact ordinary session"
            if path.startswith("/api/v1/transfer/source-protection-options?"):
                values=ONLINE.urllib.parse.parse_qs(ONLINE.urllib.parse.urlsplit(path).query)
                assert set(values)=={"source_locator"}
                return ServiceRoleCursorClient.request(self,method,"/api/v1/service/query-protection-options?source",expected,body,**kwargs)
            if path=="/api/v1/transfer/source-protection-checks":
                return self.refusal("transfer_protection_roles_unavailable") if self.expired else response(200,{"valid":True})
            if path=="/api/v1/transfer/task-definitions":
                self.task={**copy.deepcopy(body),"id":501}
                assert self.task["config"]["source"]["protection_choices"]
                return response(201,copy.deepcopy(self.task))
            if path==task_path:
                if method=="DELETE":
                    self.task_deleted=True
                    return response(200,{"message":"deleted"})
                if method=="PUT":
                    if "name" in body and self.expired:
                        return self.refusal("transfer_protection_roles_unavailable")
                    self.task.update(copy.deepcopy(body))
                value=copy.deepcopy(self.task)
                if self.expired and self.expiry_fault=="task_choice_cleared":value["config"]["source"]["protection_choices"]=[]
                return response(404 if self.task_deleted else 200,value)
            if path==task_path+"/start":
                selected=bool(self.task["config"]["source"]["protection_choices"])
                if self.expired and selected:
                    if self.expiry_fault=="hidden_execution":self.task_history.append({"execution_id":str(ONLINE.uuid.UUID(int=999))})
                    if self.expiry_fault=="hidden_target_write":self.target_rows[0]["customer_code"]="modified"
                    return self.refusal("transfer_protection_roles_unavailable")
                execution_id=str(ONLINE.uuid.UUID(int=100+len(self.task_history)))
                value={"execution_id":execution_id,"task_id":501,"status":"success","trigger_type":"manual","trigger_by":7,"records_read":5,"records_written":5}
                if self.expiry_fault=="wrong_counts":value["records_written"]=4
                self.task_history.append(value)
                self.target_rows=self.rows(selected)
                return response(200,copy.deepcopy(value))
            if path.startswith(task_path+"/executions?"):
                return response(200,{"data":copy.deepcopy(self.task_history),"total":len(self.task_history)})
            if path.startswith("/api/v1/transfer/executions/"):
                return response(200,copy.deepcopy(next(row for row in self.task_history if row["execution_id"]==path.rsplit("/",1)[-1])))
            raise AssertionError("unexpected Transfer route "+path)
        export_create=path.startswith("/api/v1/develop/executions/") and path.endswith("/exports")
        if export_create or path.startswith("/api/v1/develop/exports/"):
            self.events.append((token,method,path,copy.deepcopy(body)))
            assert token=="reader-active", "CSV must retain the same ordinary session"
            if export_create:
                source_id=path.split("/")[5]
                selected=bool(self.executions[source_id]["execution_config"]["content"]["export_protection_choices"])
                if selected and self.expired:return self.refusal("query_protection_choice_invalid")
                identifier=len(self.sessions)+1
                path="/api/v1/develop/exports/"+str(identifier)
                value={"id":identifier,"transfer_execution_id":str(ONLINE.uuid.UUID(int=300+identifier)),"format":"csv","status":"success",
                    "requires_current_user":selected,"download_url":path+"/file"}
                self.sessions[identifier]=value
                return response(202,copy.deepcopy(value))
            identifier=int(path.split("/")[5]);value=copy.deepcopy(self.sessions[identifier])
            selected=value["requires_current_user"]
            if selected and self.expired:
                if self.expiry_fault=="old_file_unlocked" and path.endswith("/file"):
                    return response(200,b"not a refusal")
                return self.refusal("query_protection_choice_invalid")
            if not path.endswith("/file"):return response(200,value)
            rows=self.rows(selected)
            if self.expiry_fault=="csv_raw":rows[0][ONLINE.BANK_FIELD]=ONLINE.BANK_SOURCE_VALUES["1"]
            if self.expiry_fault=="csv_default_instead" and selected:rows=self.rows(False)
            if self.expiry_fault=="csv_default_leaked" and not selected:rows[0][ONLINE.BANK_FIELD]=ONLINE.BANK_SOURCE_VALUES["1"]
            stream=ONLINE.io.StringIO();writer=ONLINE.csv.DictWriter(stream,fieldnames=ONLINE.BANK_FIELDS);writer.writeheader();writer.writerows(rows)
            return response(200,stream.getvalue().encode())
        if path=="/api/v1/system/login":
            assert body["username"]==ONLINE.TRANSFER_ROLE_USERNAME
            body=dict(body,username=ONLINE.DEVELOP_ROLE_USERNAME)
        value=super().for_user(token,method,path,expected,body,**kwargs)
        if path=="/api/v1/system/auth/context" and token!="controller" and value.status==200:
            permissions=set(ONLINE.TRANSFER_ROLE_READER_PERMISSIONS)
            if self.expired and self.expiry_fault=="permission_lost":permissions.remove("transfer.task.execute")
            value.payload["authorization"]["role_assignments"][0]["permissions"]=sorted(permissions)
        if path=="/api/v1/system/tenant/role_assignments":
            self.timed["username"]=ONLINE.TRANSFER_ROLE_USERNAME;value.payload=[copy.deepcopy(self.timed)]
        if path.startswith("/api/v1/security/protection-enrollments/"):value.payload["owner_progress"][0]["consumer_owner"]="transfer"
        return value


class TransferRoleNaturalExpiryTest(unittest.TestCase):
    def run_case(self, backend):
        class Clock(ONLINE.datetime):
            @classmethod
            def now(cls,tz=None):return backend.now
        source={"id":1,"full_name":"fixture.bank_card_accounts","item_type":"table","fingerprint":"sha256:bank-fixture"}
        target={"id":2,"node_id":3,"full_name":ONLINE.TARGET_SCHEMA+"."+ONLINE.BANK_TARGET,"item_type":"table"}
        env=dict(zip(ONLINE.SERVICE_ROLE_ENVIRONMENT,("unused","external-online-security-role-reader","private-password","42","controller")))
        env.update(ADDP_ONLINE_SECURITY_TRANSFER_ROLE_USERNAME=ONLINE.TRANSFER_ROLE_USERNAME,ADDP_ONLINE_SECURITY_TRANSFER_ROLE_PASSWORD="private-transfer-password")
        with (patch.object(ONLINE,"GatewayClient",side_effect=lambda base,token,timeout:MembershipClient(backend,token)),
              patch.object(ONLINE,"datetime",Clock),patch.object(ONLINE,"wait_protection_deadline",side_effect=backend.wait),
              patch.object(ONLINE,"find_item",side_effect=lambda client,engine,*args:source if engine==23 else target),
              patch.object(ONLINE,"list_pages",return_value=[]),
              patch.object(ONLINE,"preview_rows",side_effect=lambda *args:(ONLINE.BANK_FIELDS,copy.deepcopy(backend.target_rows)))):
            return ONLINE.exercise_transfer_role_membership_expiry(MembershipClient(backend,"governor"),23,31,"fixture",
                {"enrollment_id":"enrollment","assessment_id":"assessment"},{"principal_id":"9","tenant_id":"2"},"run-42",time.monotonic()+300,env)

    def test_same_session_blocks_start_and_old_actual_csv_then_explicit_default_recovers(self):
        backend=TransferRoleExpiryBackend();report=self.run_case(backend)
        self.assertEqual(report["identity_before"],report["identity_after"])
        self.assertTrue(report["selected_csv"]["selected_full_mask"] and report["selected_csv"]["csv_empty_cells_verified"])
        self.assertTrue(report["default_csv"]["default_mask"] and report["default_target"]["default_mask"])
        self.assertEqual(len(report["same_session_refusals"]),6)
        for name,refusal in report["same_session_refusals"].items():
            expected="transfer_protection_roles_unavailable" if name in ("task_start","task_name_update","direct_selection_check") else "query_protection_choice_invalid"
            self.assertEqual(refusal,{"http_status":409,"error_code":expected,"result_absent":True})
        self.assertTrue(report["no_execution_or_target_write_after_refusal"])
        self.assertEqual(report["assignment_after"]["effective_state"],"expired")
        self.assertEqual(len(backend.task_history),2);self.assertEqual(len(backend.sessions),2)
        self.assertTrue(backend.task_deleted);self.assertEqual(backend.policy["state"],"revoked")
        self.assertFalse(any(str(path).endswith("/revoke") for _,_,path,_ in backend.events))
        self.assertEqual(sum(path=="/api/v1/system/login" for _,_,path,_ in backend.events),2)
        wait=next(i for i,row in enumerate(backend.events) if row[0]=="wait")
        self.assertFalse(any(path=="/api/v1/system/login" for _,_,path,_ in backend.events[wait+1:]))
        for secret in ("private-transfer-password","reader-active",*filter(None,ONLINE.BANK_SOURCE_VALUES.values())):
            self.assertNotIn('"'+secret+'"',json.dumps(report,ensure_ascii=False))

    def test_auth_protection_wrong_codes_hidden_writes_csv_leaks_and_silent_clear_cannot_pass(self):
        for fault in ("session401","version_changed","permission_lost","role_survived","premature_wait","revoked_instead","extended_deadline",
                      "short_session","policy_changed","projection_changed","wrong_counts","wrong_transfer_code","wrong_bff_code","denial_data",
                      "denial401","hidden_execution","hidden_target_write","task_choice_cleared","csv_raw","csv_default_instead","csv_default_leaked","old_file_unlocked"):
            with self.subTest(fault=fault),self.assertRaises(ONLINE.SuiteError):self.run_case(TransferRoleExpiryBackend(fault))

    def test_interruption_deletes_own_task_before_qualifier_revocation_invalidates_its_session(self):
        backend=TransferRoleExpiryBackend("interrupt_wait")
        with self.assertRaises(KeyboardInterrupt):self.run_case(backend)
        self.assertTrue(backend.task_deleted);self.assertEqual(backend.timed["status"],"revoked")
        delete=next(i for i,row in enumerate(backend.events) if row[1:3]==("DELETE","/api/v1/transfer/task-definitions/501"))
        revoke=next(i for i,row in enumerate(backend.events) if str(row[2]).endswith("/revoke"))
        self.assertLess(delete,revoke)
        self.assertEqual(backend.previous["status"],"revoked")


class ResourceValiditySnapshotTest(unittest.TestCase):
    def snapshot(self, until=None, expired=False):
        return {"validity_mode": "permanent" if until is None else "until", "expires_at": until,
            "owner_progress": [{"consumer_owner": owner,"projection_id":"projection-"+owner,"revision":"00000000000000000004",
                "published_cursor":"cursor-"+owner,"acknowledged":True,"projection_state":"active",
                "validity":"expired" if expired else "valid","expires_at":until} for owner in ONLINE.PROTECTION_OWNERS]}

    def test_permanent_requires_explicit_null_for_resource_and_every_owner(self):
        value=self.snapshot()
        self.assertEqual(set(ONLINE.validity_snapshot(value,"permanent",None)),set(ONLINE.PROTECTION_OWNERS))
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
            "diagnostic": {"schema_version": "addp.execution-failure/v3", "stage": "result", "reason": "data_protection_required"}}}

    def test_requires_specific_safe_diagnostic_and_absent_result(self):
        valid = self.valid_execution()
        self.assertEqual(ONLINE.validate_develop_protection_refusal(valid, "result", "data_protection_required")["diagnostic_reason"], "data_protection_required")
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
                ONLINE.validate_develop_protection_refusal(value_execution, "result", "data_protection_required")



class TransferProtectionDiagnosticContractTests(unittest.TestCase):
    def test_service_refusal_has_only_safe_403_contract(self):
        response = ONLINE.SUPPORT.Response(403, {"error":"localized protection explanation", "error_code":"data_protection_required"})
        ONLINE.validate_service_protection_refusal(response)
        for status, payload in [(500,response.payload), (403,{"error":"generic", "error_code":"query_execution_failed"}), (403,{**response.payload,"data":[]}), (403,{**response.payload,"details":"private"}), (403,{**response.payload,"error":""})]:
            with self.subTest(status=status,payload=payload), self.assertRaises(ONLINE.SuiteError):
                ONLINE.validate_service_protection_refusal(ONLINE.SUPPORT.Response(status,payload))

    def test_precise_diagnostic_rejects_generic_or_private_failures(self):
        import copy
        value = {"status": "failed", "error_details": {"category": "execution_failed", "code": "data_protection_required", "diagnostic": {"schema_version": "addp.execution-failure/v3", "stage": "preparation", "reason": "data_protection_required"}}}
        ONLINE.validate_transfer_protection_refusal(value, "preparation", "data_protection_required", "data_protection_required")
        for target, key, bad in [("details", "diagnostic", None), ("details", "message", "private"), ("details", "code", "query_execution_failed"), ("diagnostic", "reason", "query_failed"), ("diagnostic", "stage", "result"), ("diagnostic", "sql", {"object": {"name": "private"}})]:
            mutated = copy.deepcopy(value)
            destination = mutated["error_details"]
            if target == "diagnostic": destination = destination["diagnostic"]
            destination[key] = bad
            with self.subTest(key=key), self.assertRaises(ONLINE.SuiteError):
                ONLINE.validate_transfer_protection_refusal(mutated, "preparation", "data_protection_required", "data_protection_required")

class AdditionalProtectionRefusalTest(unittest.TestCase):
    def execution(self, reason):
        return {"execution_id": "98d20ee1-a505-4b77-99ad-0f5c6fda2b8e", "status": "failed", "metadata": {},
                "error_details": {"category": "execution_failed", "code": "develop.query.protection_required",
                    "diagnostic": {"schema_version": "addp.execution-failure/v3", "stage": "preparation", "reason": reason}}}

    def test_each_actual_cause_requires_same_execution_history_without_result(self):
        for reason in ('protection_not_ready', 'protection_operation_unsupported', 'protection_schema_mismatch'):
            original = self.execution(reason)
            for mutation in ('none', 'generic', 'stage', 'result', 'other_execution', 'raw_detail'):
                history = copy.deepcopy(original)
                if mutation == 'generic': history['error_details']['diagnostic']['reason'] = 'data_protection_required'
                if mutation == 'stage': history['error_details']['diagnostic']['stage'] = 'result'
                if mutation == 'result': history['metadata']['result'] = {'rows': ['private']}
                if mutation == 'other_execution': history['execution_id'] = str(uuid.uuid4())
                if mutation == 'raw_detail': history['error_details']['message'] = 'private'
                client = Mock()
                client.request.return_value = ONLINE.SUPPORT.Response(200, history)
                with self.subTest(reason=reason, mutation=mutation), patch.object(ONLINE, 'develop_execution', return_value=original):
                    if mutation == 'none':
                        result = ONLINE.develop_refusal_with_history(client, 23, 'locator', 'SELECT phone', reason, time.monotonic()+10)
                        self.assertEqual(result['diagnostic_reason'], reason)
                        self.assertTrue(result['history_result_absent'])
                    else:
                        with self.assertRaises(ONLINE.SuiteError):
                            ONLINE.develop_refusal_with_history(client, 23, 'locator', 'SELECT phone', reason, time.monotonic()+10)

    def test_unready_needs_all_enrolling_receipts_before_querying(self):
        base = {'state': 'enrolling', 'owner_progress': [
            {'consumer_owner': owner, 'projection_state': 'enrolling', 'acknowledged': True, 'rules': []}
            for owner in ONLINE.PROTECTION_OWNERS]}
        for fault in ('none', 'active', 'missing_receipt', 'missing_owner', 'rules'):
            value = copy.deepcopy(base)
            if fault == 'active': value['state'] = 'active'
            if fault == 'missing_receipt': value['owner_progress'][0]['acknowledged'] = False
            if fault == 'missing_owner': value['owner_progress'].pop()
            if fault == 'rules': value['owner_progress'][0]['rules'] = [{'effect': 'mask'}]
            client = Mock()
            client.request.return_value = ONLINE.SUPPORT.Response(200, value)
            with self.subTest(fault=fault), patch.object(ONLINE, 'develop_refusal_with_history', return_value={}) as refusal, \
                    patch.object(ONLINE.time, 'monotonic', side_effect=[0, 100]), patch.object(ONLINE.time, 'sleep'):
                if fault == 'none':
                    self.assertTrue(ONLINE.exercise_enrolling_refusal(client, 'id', 23, 'locator', 10, 'SELECT id,value_a FROM addp_online_security.spatial_algorithm_source')['enrolling_acknowledged'])
                    self.assertEqual(refusal.call_args.args[-2], 'protection_not_ready')
                else:
                    with self.assertRaises(ONLINE.SuiteError):
                        ONLINE.exercise_enrolling_refusal(client, 'id', 23, 'locator', 10, 'SELECT id,value_a FROM addp_online_security.spatial_algorithm_source')
                    refusal.assert_not_called()

    def test_unready_waits_for_initial_installation_and_receipts_within_existing_deadline(self):
        ready = {'state': 'enrolling', 'owner_progress': [
            {'consumer_owner': owner, 'projection_state': 'enrolling', 'acknowledged': True, 'rules': []}
            for owner in ONLINE.PROTECTION_OWNERS]}
        pending = copy.deepcopy(ready)
        pending['owner_progress'][0]['acknowledged'] = False
        client = Mock()
        client.request.side_effect = [ONLINE.SUPPORT.Response(200, v) for v in
                                     ({'state': 'activating', 'owner_progress': []}, pending, ready)]
        with patch.object(ONLINE, 'develop_refusal_with_history', return_value={}) as refusal, \
                patch.object(ONLINE.time, 'monotonic', return_value=0), patch.object(ONLINE.time, 'sleep') as sleep:
            self.assertTrue(ONLINE.exercise_enrolling_refusal(client, 'id', 23, 'locator', 10, 'SELECT id,value_a FROM addp_online_security.spatial_algorithm_source')['enrolling_acknowledged'])
            self.assertEqual(client.request.call_count, 3)
            self.assertEqual(sleep.call_count, 2)
            refusal.assert_called_once()
        client.request.side_effect = None
        client.request.return_value = ONLINE.SUPPORT.Response(200, {'state': 'activating', 'owner_progress': []})
        with patch.object(ONLINE, 'develop_refusal_with_history') as refusal, \
                patch.object(ONLINE.time, 'monotonic', side_effect=[0, 11]), patch.object(ONLINE.time, 'sleep'):
            with self.assertRaisesRegex(ONLINE.SuiteError, 'before refusal acceptance'):
                ONLINE.exercise_enrolling_refusal(client, 'id', 23, 'locator', 10, 'SELECT id,value_a FROM addp_online_security.spatial_algorithm_source')
            refusal.assert_not_called()

    def test_schema_restoration_runs_on_add_or_query_failure_and_checks_recovery(self):
        for failure in ('none', 'add', 'query'):
            calls = []
            def physical(action, deadline):
                calls.append(action)
                if failure == 'add' and action == 'bank-schema-add': raise ONLINE.SuiteError('injected add failure')
            def refuse(*args):
                if failure == 'query' and args[-2] == 'protection_schema_mismatch': raise ONLINE.SuiteError('injected query failure')
                return {'diagnostic_reason': args[-2]}
            client = Mock()
            client.request.return_value = ONLINE.SUPPORT.Response(200, {'owner_progress': [{'revision': '1'}]})
            with self.subTest(failure=failure), patch.object(ONLINE, 'run_bank_schema_fixture', side_effect=physical), \
                    patch.object(ONLINE, 'develop_refusal_with_history', side_effect=refuse), \
                    patch.object(ONLINE, 'develop_rows', return_value=[]) as rows, \
                    patch.object(ONLINE, 'assert_bank_card_rows', return_value={'rows': 5}):
                if failure == 'none':
                    result = ONLINE.exercise_bank_card_refusal_causes(client, 'id', 23, 'locator', time.monotonic()+10, Mock(return_value={}))
                    self.assertTrue(result['schema_restored'])
                    self.assertTrue(result['protection_revisions_unchanged'])
                    rows.assert_called_once()
                else:
                    with self.assertRaisesRegex(ONLINE.SuiteError, 'injected'):
                        ONLINE.exercise_bank_card_refusal_causes(client, 'id', 23, 'locator', time.monotonic()+10, Mock(return_value={}))
                    rows.assert_not_called()
                self.assertEqual(calls, ['bank-schema-add', 'bank-schema-remove'])

    def test_physical_cleanup_has_independent_bounded_budget_and_admission(self):
        with patch.object(ONLINE.SUPPORT, 'require_hosted_security_lifecycle') as admission, \
                patch.object(ONLINE.subprocess, 'run', return_value=Mock(returncode=0)) as command:
            ONLINE.run_bank_schema_fixture('bank-schema-remove', 0)
            self.assertEqual(command.call_args.kwargs['timeout'], 30)
            admission.assert_called_once()
            with self.assertRaises(ONLINE.SuiteError): ONLINE.run_bank_schema_fixture('unknown', 0)
            with self.assertRaises(ONLINE.SuiteError): ONLINE.run_bank_schema_fixture('bank-schema-add', 0)
            self.assertEqual(command.call_count, 1)


class UnreadySourceFixtureTest(unittest.TestCase):
    def enrollment(self):
        return {'state': 'enrolling', 'latest_discovery_execution_id': str(uuid.uuid4()), 'latest_source_snapshot_hash': 'sha256:source',
                'discovery_summary': {'status': 'completed', 'finding_count': 0, 'reviewed_count': 0, 'pending_review_count': 0},
                'owner_progress': [{'consumer_owner': owner, 'revision': '00000000000000000002', 'published_cursor': owner,
                                   'projection_state': 'enrolling', 'acknowledged': True, 'rules': []} for owner in ONLINE.PROTECTION_OWNERS]}

    def test_completed_empty_discovery_keeps_all_outlets_before_manual_assessment(self):
        for fault in ('none', 'finding', 'active', 'after_active', 'after_revision', 'after_receipt', 'after_discovery', 'reused'):
            before = self.enrollment(); after = copy.deepcopy(before)
            if fault == 'finding': before['discovery_summary']['finding_count'] = 1
            if fault == 'active': before['state'] = 'active'
            if fault == 'after_active': after['state'] = 'active'
            if fault == 'after_revision': after['owner_progress'][0]['revision'] = '00000000000000000003'
            if fault == 'after_receipt': after['owner_progress'][0]['acknowledged'] = False
            if fault == 'after_discovery': after['latest_discovery_execution_id'] = str(uuid.uuid4())
            client = Mock(); client.request.side_effect = [ONLINE.SUPPORT.Response(200, item) for item in (before, after)]
            source = {'id': 3, 'full_name': ONLINE.TARGET_SCHEMA + '.' + ONLINE.SPATIAL_SOURCE, 'item_type': 'table'}
            target = {'id': 4, 'node_id': 5, 'full_name': ONLINE.TARGET_SCHEMA + '.' + ONLINE.BANK_TARGET, 'item_type': 'table'}
            with self.subTest(fault=fault), patch.object(ONLINE, 'find_item', side_effect=[source,target]), \
                    patch.object(ONLINE, 'ensure_enrollment', return_value=('enrollment', fault != 'reused')), \
                    patch.object(ONLINE, 'exercise_enrolling_refusal', return_value={'enrolling_acknowledged': True}) as develop, \
                    patch.object(ONLINE, 'bank_outlet_refusals', return_value={}) as outlets:
                if fault == 'none':
                    proof = ONLINE.exercise_unready_source(client,17,7,time.monotonic()+10)
                    self.assertTrue(proof['all_outlets_enrolling_verified'])
                    self.assertEqual(proof['fixture_source']['locator'], ONLINE.build_item_locator(17,source))
                    self.assertEqual(develop.call_args.args[-1], 'SELECT id,value_a FROM addp_online_security.spatial_algorithm_source')
                    self.assertEqual(outlets.call_args.args[1:5], (17,17,source,ONLINE.SPATIAL_FIELDS))
                else:
                    with self.assertRaises(ONLINE.SuiteError): ONLINE.exercise_unready_source(client,17,7,time.monotonic()+10)
                    if fault in ('finding','active','reused'):
                        develop.assert_not_called(); outlets.assert_not_called()

    def test_empty_discovery_must_actually_complete_within_original_deadline(self):
        ready = self.enrollment()
        pending = copy.deepcopy(ready); pending['discovery_summary']['status'] = 'not_completed'
        source = {'id': 3, 'full_name': ONLINE.TARGET_SCHEMA + '.' + ONLINE.SPATIAL_SOURCE, 'item_type': 'table'}
        target = {'id': 4, 'node_id': 5, 'full_name': ONLINE.TARGET_SCHEMA + '.' + ONLINE.BANK_TARGET, 'item_type': 'table'}
        client = Mock(); client.request.side_effect = [ONLINE.SUPPORT.Response(200,v) for v in (pending,ready,ready)]
        with patch.object(ONLINE, 'find_item', side_effect=[source,target]), patch.object(ONLINE, 'ensure_enrollment', return_value=('enrollment',True)), \
                patch.object(ONLINE, 'exercise_enrolling_refusal', return_value={}) as develop, patch.object(ONLINE,'bank_outlet_refusals',return_value={}), \
                patch.object(ONLINE.time,'sleep') as sleep:
            ONLINE.exercise_unready_source(client,17,7,time.monotonic()+10)
            sleep.assert_called_once_with(1); develop.assert_called_once()
        client.request.side_effect = None; client.request.return_value = ONLINE.SUPPORT.Response(200,pending)
        with patch.object(ONLINE, 'find_item', side_effect=[source,target]), patch.object(ONLINE, 'ensure_enrollment', return_value=('enrollment',True)), \
                patch.object(ONLINE, 'exercise_enrolling_refusal') as develop, patch.object(ONLINE.time,'sleep'), \
                patch.object(ONLINE.time,'monotonic',side_effect=[0,11]):
            with self.assertRaisesRegex(ONLINE.SuiteError, 'did not complete'):
                ONLINE.exercise_unready_source(client,17,7,10)
            develop.assert_not_called()


class FourOutletReasonMatrixTest(unittest.TestCase):
    def test_matrix_has_one_final_report_owner_after_evidence_collection(self):
        owners = [function.name for function in ast.walk(ast.parse(SCRIPT.read_text()))
                  if isinstance(function, (ast.FunctionDef, ast.AsyncFunctionDef))
                  for node in ast.walk(function)
                  if isinstance(node, ast.Call) and isinstance(node.func, ast.Name)
                  and node.func.id == 'protection_reason_matrix']
        self.assertEqual(owners, ['main'])

    def test_missing_or_empty_mongodb_evidence_cannot_complete_matrix(self):
        for evidence in (None, {}, {'cases': []}):
            report = self.report()
            if evidence is None:
                del report['mongodb_algorithms']
            else:
                report['mongodb_algorithms'] = evidence
            with self.subTest(evidence=evidence), self.assertRaises(ONLINE.SuiteError):
                ONLINE.protection_reason_matrix(report)

    def proof(self, reason, owner):
        if owner in ('manager', 'service'):
            return {'http_status': 403, 'error_code': 'security_protection_required' if owner == 'manager' else 'data_protection_required', 'result_absent': True}
        return {'execution_id': str(uuid.uuid4()), 'diagnostic_reason': reason, 'diagnostic_stage': 'preparation',
                **({'query_source': {'engine_type': 'postgresql', 'locator': 'addp://engine/17/path/addp_online_security/spatial_algorithm_source?type=table&item_id=3'}}
                   if owner == 'transfer' and reason in {'protection_computed_output_unsupported', 'protection_operation_unsupported'} else {}),
                'diagnostic_code': 'develop.query.protection_required' if owner == 'develop' else ('query_output_lineage_unresolved' if reason.endswith('lineage_unverified') else 'data_protection_required'),
                'history_result_absent': True, 'denied_result_absent': True,
                'records_written': 0, 'bytes_written': 0, 'target_unchanged': True, 'same_user_verified': True}

    def report(self):
        refusals = {}
        for name in ('not_ready','schema_mismatch','operation_unsupported'):
            reason='protection_'+name
            owners=('service','transfer') if name=='operation_unsupported' else ('manager','service','transfer')
            refusals[name]={**self.proof(reason,'develop'),'outlets':{o:self.proof(reason,o) for o in owners}}
        refusals['not_ready'].update(fixture_source={'engine_type': 'postgresql', 'locator': 'addp://engine/17/path/addp_online_security/spatial_algorithm_source?type=table&item_id=3',
                                    'discovery_execution_id': str(uuid.uuid4()), 'owner_revisions': {o: '00000000000000000002' for o in ONLINE.PROTECTION_OWNERS}},
                                    discovery_completed_without_findings=True, all_outlets_enrolling_verified=True)
        lineage=self.proof('protection_lineage_unverified','transfer')
        lineage['target_empty_before_and_after']=True
        return {'bank_card_metadata':{'refusal_causes':refusals, 'computed_outputs':{
                    'cases':{'substring':self.proof('protection_computed_output_unsupported','develop')},
                    'service_refusal':self.proof('protection_computed_output_unsupported','service'),
                    'transfer_refusal':self.proof('protection_computed_output_unsupported','transfer')}},
                'mongodb_algorithms':{'cases':[{'query_boundaries':self.proof('protection_lineage_unverified','develop'),'derived_transfer_denied':lineage}]},
                'resource_validity':{'same_user_verified':True,'expired_outputs':{'manager_http_status':403,'service_http_status':403,
                    'service_error_code':'data_protection_required','develop_execution_id':str(uuid.uuid4()),'transfer_execution_id':str(uuid.uuid4()),
                    'transfer_diagnostic':self.proof('protection_expired','transfer'),'records_written':0,'bytes_written':0,'target_unchanged':True,'result_absent':True}}}

    def test_closed_matrix_keeps_protocol_cells_distinct_from_runtime(self):
        matrix=ONLINE.protection_reason_matrix(self.report())
        self.assertEqual(len(matrix['rows']),7)
        self.assertEqual(matrix['coverage_counts'],{'runtime':20,'protocol_only':8})
        for reason,row in matrix['rows'].items():
            self.assertEqual(set(row),set(ONLINE.OWNER_ACTIONS))
            for owner,cell in row.items():
                if cell['level']=='protocol_only':
                    self.assertEqual(cell['entry'],'make test-common-postgres')
                    self.assertIn('/'+owner+'/'+reason,cell['case'])
                    self.assertNotIn('evidence',cell)
        self.assertTrue(all(c['level']=='protocol_only' for c in matrix['rows']['protection_not_yet_valid'].values()))

    def test_matrix_rejects_missing_http_or_historical_proof_and_transfer_writes(self):
        for fault in ('missing','reason','history','uuid','http_data','writes','bool_writes','target'):
            report=self.report(); proof=report['bank_card_metadata']['refusal_causes']['not_ready']
            if fault=='missing': del proof['outlets']['manager']
            if fault=='reason': proof['diagnostic_reason']='data_protection_required'
            if fault=='history': proof['history_result_absent']=False
            if fault=='uuid': proof['execution_id']='not-an-execution'
            if fault=='http_data': proof['outlets']['service']['rows']=[]
            if fault=='writes': proof['outlets']['transfer']['records_written']=1
            if fault=='bool_writes': proof['outlets']['transfer']['records_written']=False
            if fault=='target': proof['outlets']['transfer']['target_unchanged']=False
            with self.subTest(fault=fault),self.assertRaises(ONLINE.SuiteError): ONLINE.protection_reason_matrix(report)

    def test_matrix_sql_transfer_proof_cannot_claim_mysql_or_another_source(self):
        for reason in ('computed_output_unsupported', 'operation_unsupported'):
            for fault in ('missing', 'mysql', 'wrong_table', 'missing_type', 'wrong_scheme'):
                report = self.report()
                proof = (report['bank_card_metadata']['computed_outputs']['transfer_refusal'] if reason.startswith('computed')
                         else report['bank_card_metadata']['refusal_causes'][reason]['outlets']['transfer'])
                if fault == 'missing': del proof['query_source']
                elif fault == 'mysql': proof['query_source']['engine_type'] = 'mysql'
                elif fault == 'wrong_table': proof['query_source']['locator'] = 'addp://engine/17/path/security_fixture/bank_card_accounts?type=table'
                elif fault == 'missing_type': proof['query_source']['locator'] = 'addp://engine/17/path/addp_online_security/spatial_algorithm_source'
                else: proof['query_source']['locator'] = proof['query_source']['locator'].replace('addp:', 'https:')
                with self.subTest(reason=reason, fault=fault), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.protection_reason_matrix(report)

    def test_unready_matrix_requires_stable_completed_discovery_and_actual_source(self):
        for fault in ('incomplete_discovery', 'changed_state', 'mysql', 'wrong_table', 'missing_source', 'discovery_uuid', 'owner_revision'):
            report = self.report()
            proof = report['bank_card_metadata']['refusal_causes']['not_ready']
            if fault == 'incomplete_discovery': proof['discovery_completed_without_findings'] = False
            elif fault == 'changed_state': proof['all_outlets_enrolling_verified'] = False
            elif fault == 'mysql': proof['fixture_source']['engine_type'] = 'mysql'
            elif fault == 'wrong_table': proof['fixture_source']['locator'] = 'addp://engine/23/path/security_fixture/bank_card_accounts?type=table'
            elif fault == 'discovery_uuid': proof['fixture_source']['discovery_execution_id'] = 'not-an-execution'
            elif fault == 'owner_revision': del proof['fixture_source']['owner_revisions']['transfer']
            else: del proof['fixture_source']
            with self.subTest(fault=fault), self.assertRaises(ONLINE.SuiteError): ONLINE.protection_reason_matrix(report)

    def test_sql_transfer_probes_bind_existing_postgres_source_and_keep_strict_history(self):
        source = {'id': 3, 'full_name': ONLINE.TARGET_SCHEMA + '.' + ONLINE.SPATIAL_SOURCE, 'item_type': 'table'}
        for suffix in ('computed_output_unsupported', 'operation_unsupported'):
            reason = 'protection_' + suffix
            with self.subTest(reason=reason), patch.object(ONLINE, 'find_item', return_value=source) as find, \
                    patch.object(ONLINE, 'transfer_refusal_with_history', return_value=self.proof(reason, 'transfer')) as refusal:
                result = ONLINE.postgres_query_transfer_refusal(Mock(), 17, 'parent', 'target', reason, 60, 7)
                find.assert_called_once_with(refusal.call_args.args[0], 17, source['full_name'], 'table')
                payload = refusal.call_args.args[1]
                self.assertEqual(result['query_source'], {'engine_type': 'postgresql', 'locator': ONLINE.build_item_locator(17, source)})
                self.assertEqual(payload['config']['source']['locator'], result['query_source']['locator'])
                self.assertEqual(payload['config']['source']['query']['language'], 'sql')
                statement = payload['config']['source']['query']['statement']
                self.assertIn(' FROM addp_online_security.spatial_algorithm_source', statement)
                self.assertIn('CAST(value_a AS text) AS value_a' if suffix.startswith('computed') else "SELECT DISTINCT id,value_a FROM", statement)
                self.assertEqual(payload['config']['transforms'][0]['fields'][1]['source'], 'value_a')
                self.assertEqual(refusal.call_args.args[2:], ('target', reason, 60, 7))
        with self.assertRaises(ONLINE.SuiteError): ONLINE.postgres_query_refusal_payload('source', 'parent', 'unknown')

    def test_transfer_refusal_compares_same_history_and_target_and_always_cleans(self):
        reason='protection_schema_mismatch'
        for fault in ('none','reason','history','writes','bytes','result','target','actor','start'):
            row={'status':'failed','trigger_by':7,'metadata':{},'records_written':0,'bytes_written':0,**self.proof(reason,'transfer')}
            row['error_details']={'code':'data_protection_required','diagnostic':{'schema_version':'addp.execution-failure/v3','reason':reason,'stage':'preparation'}}
            history=copy.deepcopy(row)
            if fault=='reason': history['error_details']['diagnostic']['reason']='data_protection_required'
            if fault=='history': history['execution_id']=str(uuid.uuid4())
            if fault=='writes': history['records_written']=1
            if fault=='bytes': history['bytes_written']=1
            if fault=='result': history['metadata']['result']={'rows':['private']}
            if fault=='actor': history['trigger_by']=9
            client=Mock();client.request.return_value=ONLINE.SUPPORT.Response(200,history)
            def run(*args,**kwargs):
                args[3].append(31)
                if fault=='start': raise ONLINE.SuiteError('start failed')
                return 31,row
            with self.subTest(fault=fault),patch.object(ONLINE,'create_and_run_task',side_effect=run),patch.object(ONLINE,'cleanup_tasks') as cleanup, \
                    patch.object(ONLINE,'preview_rows',side_effect=[(['id'],[]),(['id'],[{'id':1}]) if fault=='target' else (['id'],[])]):
                if fault=='none': self.assertTrue(ONLINE.transfer_refusal_with_history(client,{},'target',reason,time.monotonic()+10,7)['target_unchanged'])
                else:
                    with self.assertRaises(ONLINE.SuiteError): ONLINE.transfer_refusal_with_history(client,{},'target',reason,time.monotonic()+10,7)
                cleanup.assert_called_once_with(client,[31])

    def test_bank_http_outlets_preserve_contract_query_source_and_cleanup(self):
        source=BankCardMetadataAcceptanceTest().source_item()
        for name in ('not_ready','schema_mismatch','operation_unsupported'):
            reason='protection_'+name
            for fault in ('none','service','transfer'):
                client=Mock()
                def request(method,path,status,body=None):
                    if method=='GET': return ONLINE.SUPPORT.Response(403,{'error':'safe','error_code':'security_protection_required'})
                    if status==(201,): return ONLINE.SUPPORT.Response(201,{'id':41})
                    return ONLINE.SUPPORT.Response(403,{'error':'safe','error_code':'data_protection_required',**({'rows':['private']} if fault=='service' else {})})
                client.request.side_effect=request
                query="SELECT id,bank_card_number FROM bank_card_accounts WHERE customer_code='CUST-10001'" if name=='operation_unsupported' else None
                with self.subTest(name=name,fault=fault),patch.object(ONLINE,'cleanup_service') as cleanup, \
                        patch.object(ONLINE,'transfer_refusal_with_history',side_effect=ONLINE.SuiteError('transfer failed') if fault=='transfer' else None,return_value=self.proof(reason,'transfer')) as transfer, \
                        patch.object(ONLINE,'postgres_query_transfer_refusal',side_effect=ONLINE.SuiteError('transfer failed') if fault=='transfer' else None,return_value=self.proof(reason,'transfer')) as sql_transfer:
                    if fault=='none':
                        result=ONLINE.bank_outlet_refusals(client,23,17,source,ONLINE.BANK_FIELDS,'source','parent','target',reason,query,time.monotonic()+10,7)
                        self.assertEqual(set(result),{'service','transfer'} if query else {'manager','service','transfer'})
                        if query:
                            self.assertEqual(sql_transfer.call_args.args[:5],(client,17,'parent','target',reason))
                            transfer.assert_not_called()
                        else:
                            self.assertNotIn('query',transfer.call_args.args[1]['config']['source'])
                            sql_transfer.assert_not_called()
                    else:
                        with self.assertRaises(ONLINE.SuiteError): ONLINE.bank_outlet_refusals(client,23,17,source,ONLINE.BANK_FIELDS,'source','parent','target',reason,query,time.monotonic()+10,7)
                    cleanup.assert_called_once_with(client,41)


if __name__ == "__main__":
    unittest.main()
