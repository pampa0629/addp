#!/usr/bin/env python3
"""Accept the browser-created PostgreSQL relational SQL ETL flow."""

from __future__ import annotations

import hashlib
from datetime import datetime, timezone
import io
import struct
import zipfile
from datetime import datetime
import importlib.util
import json
import os
import signal
import subprocess
import sys
import time
import urllib.parse
import uuid
from pathlib import Path
from typing import Mapping


SUPPORT_PATH = Path(__file__).with_name("security-transfer-protection-online.py")
SUPPORT_SPEC = importlib.util.spec_from_file_location("transfer_sql_etl_support", SUPPORT_PATH)
SUPPORT = importlib.util.module_from_spec(SUPPORT_SPEC)
assert SUPPORT_SPEC.loader is not None
sys.modules[SUPPORT_SPEC.name] = SUPPORT
SUPPORT_SPEC.loader.exec_module(SUPPORT)

API_SPEC = importlib.util.spec_from_file_location("transfer_online_api", Path(__file__).resolve().parents[1] / "utils/online-api.py")
API = importlib.util.module_from_spec(API_SPEC)
assert API_SPEC.loader is not None
sys.modules[API_SPEC.name] = API
API_SPEC.loader.exec_module(API)

GatewayClient = SUPPORT.GatewayClient
SuiteError = SUPPORT.SuiteError
_array = SUPPORT._array
_object = SUPPORT._object
positive_int = SUPPORT.positive_int
required_environment = SUPPORT.required_environment
wait_for_scan = SUPPORT.wait_for_scan
find_item = SUPPORT.find_item
cleanup_tasks = SUPPORT.cleanup_tasks

SOURCE_TABLE = "addp_online_transfer_sql_etl_source"
TARGET_TABLE = "addp_online_transfer_sql_etl_target"
IMPORT_TARGET = "addp_online_manager_import_points"
NATIVE_TARGET = "addp_online_transfer_field_lineage_target"
MULTI_TARGET = "addp_online_transfer_multi_query_target"
NATIVE_DOWNSTREAM = "addp_online_transfer_field_lineage_downstream"
WIDE_SOURCE = "addp_wide_source"
WIDE_TARGET = "addp_wide_target"
WIDE_DOWNSTREAM = "addp_wide_downstream"
WIDE_FIELDS = tuple(f"field_{i:04d}" for i in range(500))
MONGODB_SOURCE = "transfer_fixture.activities"
MONGODB_TARGET = "addp_online_transfer_mongodb_ods"
MONGODB_FIELDS = (
    ("_id", "activity_id"), ("status", "activity_status"),
    ("title.date", "activity_date_raw"), ("title.level", "activity_level_raw"),
    ("leader.personid", "leader_person_id"), ("leader.userInfo.nickName", "leader_nickname_snapshot"),
)
TASK_PREFIX = "addp_online_sql_etl_"
FORBIDDEN_ADMIN_ROLES = SUPPORT.FORBIDDEN_ADMIN_ROLES
REQUIRED_PERMISSIONS = {
    "develop.task.create", "develop.task.read", "develop.task.execute", "develop.task.delete",
    "develop.data_read.execute", "develop.data_write.execute", "system.execution_authorization.create",
    "orchestrator.workflow.create", "orchestrator.workflow.read", "orchestrator.workflow.execute", "orchestrator.workflow.delete",
    "monitor.execution.read",
    "manager.content.read",
    "manager.data_item.create",
    "manager.data_item.read",
    "meta.catalog.read",
    "meta.lineage.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
    "transfer.task.create",
    "transfer.task.delete",
    "transfer.task.execute",
    "transfer.task.read",
}


def task_name(run_id: str) -> str:
    return TASK_PREFIX + hashlib.sha256(run_id.encode()).hexdigest()[:16]


def owned_task_names(name: str) -> set[str]:
    return {name, name + "_native", name + "_replace", name + "_hop", name + "_mongodb", name + "_multi", name + "_wide", name + "_wide_hop"} | {name + "_" + engine + "_" + scenario for engine in WRITE_FAILURE_ENGINES for scenario in WRITE_FAILURE_CASES}


WRITE_FAILURE_CASES = ("late_execute_failure", "invalid_json")
WRITE_FAILURE_ENGINES = ("doris", "clickhouse")


def validate_write_failure_execution(execution, task_id, scenario=None):
    assert_transfer_execution_projection(execution)
    identifier = execution.get("execution_id")
    if not isinstance(identifier, str) or len(identifier) != 36:
        raise SuiteError("write failure must identify the actual execution")
    try:
        if str(uuid.UUID(identifier)) != identifier: raise ValueError()
    except ValueError as error:
        raise SuiteError("write failure execution id is not a canonical UUID") from error
    if execution.get("task_id") != task_id or execution.get("status") != "failed":
        raise SuiteError("write failure must preserve its task and failed terminal status")
    for key in ("records_read", "records_written"):
        if type(execution.get(key)) is not int or execution[key] != 0:
            raise SuiteError("failed first batch must not advertise completed batch progress")
    failure = _object(execution.get("error_details"), "write failure details")
    if failure.get("category") != "execution_failed" or not execution.get("end_time"):
        raise SuiteError("write failure must expose a safe failure category and terminal time")
    diagnostic = _object(failure.get("diagnostic"), "write failure diagnostic")
    expected_reason = "json_invalid" if scenario == "invalid_json" else "table_write_failed"
    if diagnostic.get("stage") != "execution" or diagnostic.get("reason") != expected_reason or diagnostic.get("target_change_possible") is not True or failure.get("code") != expected_reason:
        raise SuiteError("write failure lost its controlled cause or target risk")
    expected_write = {"column_index": 2, "declared_type": "json"} if scenario == "invalid_json" else None
    if diagnostic.get("write") != expected_write:
        raise SuiteError("write failure lost the actual write column or guessed a native failure column")
    if execution.get("metadata", {}).get("metadata_scan"):
        raise SuiteError("write failure unexpectedly triggered metadata scanning")
    return {"task_id": task_id, "execution_id": identifier, "status": "failed", "records_written": 0, "error_details": failure}


def validate_write_failure_physical(payload):
    if not isinstance(payload, dict) or set(payload) != set(WRITE_FAILURE_ENGINES):
        raise SuiteError("write failure physical evidence must cover both engines")
    for engine in WRITE_FAILURE_ENGINES:
        cases = _object(payload[engine], "write failure physical cases")
        if set(cases) != set(WRITE_FAILURE_CASES):
            raise SuiteError("write failure physical evidence is incomplete")
        for scenario in WRITE_FAILURE_CASES:
            expected = {"rows": 1001 if scenario == "late_execute_failure" else 1, "sentinel_rows": 1,
                        "valid_rows": 1000 if scenario == "late_execute_failure" else 0, "failed_row_absent": True}
            if cases[scenario] != expected or cases[scenario].get("failed_row_absent") is not True or any(type(cases[scenario].get(key)) is not int for key in ("rows", "sentinel_rows", "valid_rows")):
                raise SuiteError("write failure physical evidence contradicts autocommit boundaries")
    return payload


def write_failure_task(name, source_locator, source_table, target_engine_id, scenario):
    target = "addp_online_transfer_write_failure" if scenario == "late_execute_failure" else "addp_online_transfer_json_failure"
    return {"name": name, "task_type": "sync", "enabled": False, "schedule": "", "auto_scan_metadata": False,
            "config": {"runtime": {"boundary": "bounded"}, "load": {"mode": "snapshot"},
                       "source": {"locator": source_locator, "data_type": "table", "representation": "native",
                                  "query": {"language": "sql", "statement": f'SELECT id,document,payload,label FROM public.{source_table} ORDER BY id'}},
                       "target": {"parent_locator": f"addp://engine/{target_engine_id}/path/transfer_fixture?type=database", "name": target, "data_type": "table", "representation": "native", "policy": {"apply_mode": "append"}},
                       "transforms": [{"type": "field_mapping", "version": "v1", "mode": "project", "fields": [
                           {"source": field, "target": field, "target_type": field_type, "nullable": False}
                           for field, field_type in (("id", "bigint"), ("document", "json"), ("payload", "bytes"), ("label", "string"))]}],
                       "batch_size": 1001}}


def run_write_failures(client, source_engine_id, name, timeout, owned_ids):
    report = {}
    for engine in WRITE_FAILURE_ENGINES:
        engine_id = positive_int(required_environment("ADDP_ONLINE_TEST_" + engine.upper() + "_ENGINE_ID"), "write failure Engine id")
        if engine_id == source_engine_id or any(proof["engine_id"] == engine_id for proof in report.values()):
            raise SuiteError("write failure engine identities must be distinct")
        validate_engine(client, engine_id, "Hosted Transfer " + engine, engine, time.monotonic() + timeout)
        # This fixture has no target scan at all. Saving and failing these tasks
        # must not make an unscanned target appear in Meta.
        before = client.request("GET", f"/api/v1/meta/engines/{engine_id}/items", (200,)).payload
        if before != []:
            raise SuiteError("write failure target must start unscanned")
        report[engine] = {"engine_id": engine_id, "cases": {}}
        for scenario in WRITE_FAILURE_CASES:
            source_table = "addp_online_transfer_failure_source" if scenario == "late_execute_failure" else "addp_online_transfer_invalid_json_source"
            source = find_item(client, source_engine_id, "public." + source_table, "table")
            task = write_failure_task(name + "_" + engine + "_" + scenario, SUPPORT.build_item_locator(source_engine_id, source), source_table, engine_id, scenario)
            task_id, execution = SUPPORT.create_and_run_task(client, task, time.monotonic() + timeout, owned_ids, expected_status="failed")
            report[engine]["cases"][scenario] = validate_write_failure_execution(execution, task_id, scenario)
        if client.request("GET", f"/api/v1/meta/engines/{engine_id}/items", (200,)).payload != before:
            raise SuiteError("failed writes unexpectedly produced target metadata")
    repository = Path(os.environ.get("ADDP_ONLINE_REPOSITORY", Path(__file__).parents[2])).resolve()
    checked = subprocess.run(["bash", "business/scripts/online-transfer-relational-sql-etl-fixture.sh", "write-failures"], cwd=repository, capture_output=True, text=True, timeout=60)
    if checked.returncode != 0:
        raise SuiteError("write failure independent physical verification failed")
    physical = validate_write_failure_physical(json.loads(checked.stdout))
    for engine in WRITE_FAILURE_ENGINES:
        report[engine]["physical"] = physical[engine]
    return report


def native_task(name: str, source_locator: str, parent_locator: str, target: str, region_source: str, region_target: str) -> dict[str, object]:
    fields = [
        {"source": "id", "target": "id", "target_type": "bigint", "nullable": False},
        {"source": region_source, "target": region_target, "target_type": "string", "nullable": False},
        {"source": "amount", "target": "amount", "target_type": "decimal", "precision": 8, "scale": 2, "nullable": False},
    ]
    if target == NATIVE_TARGET:
        fields.append({"source": "", "target": "generated_label", "target_type": "string", "nullable": False, "default": "online"})
    return {
        "name": name, "task_type": "sync", "enabled": False, "schedule": "", "auto_scan_metadata": True,
        "config": {
            "runtime": {"boundary": "bounded"}, "load": {"mode": "snapshot"},
            "source": {"locator": source_locator, "data_type": "table", "representation": "native"},
            "target": {"parent_locator": parent_locator, "name": target, "data_type": "table", "representation": "native", "policy": {"apply_mode": "replace"}},
            "transforms": [{"type": "field_mapping", "version": "v1", "mode": "project", "fields": fields}], "batch_size": 100,
        },
    }


def validate_field_graph(payload: object, item_id: int, field: str, expected: set[tuple[int, str, int, str, str, str]], *, historical: bool = False) -> dict[str, object]:
    graph = _object(payload, "field lineage graph")
    subject = _object(graph.get("subject"), "field lineage subject")
    if graph.get("field_lineage_status") != "complete" or graph.get("truncated"):
        raise SuiteError("field lineage must be complete and untruncated")
    if subject.get("kind") != "field_ref" or not subject.get("schema_snapshot_hash") or subject.get("item_id") != item_id or subject.get("field_name") != field:
        raise SuiteError("field lineage subject identity mismatch")
    nodes = _array(graph.get("nodes"), "field lineage nodes")
    edges = _array(graph.get("edges"), "field lineage edges")
    identities = set()
    for raw in nodes:
        node = _object(raw, "field node")
        if node.get("kind") != "field_ref" or not node.get("field_name") or not node.get("schema_snapshot_hash"):
            raise SuiteError("field node is missing its exact field/snapshot identity")
        identities.add((node.get("item_id"), node.get("field_name")))
    observed = set()
    for raw in edges:
        edge = _object(raw, "field edge")
        source, target = _object(edge.get("source"), "edge source"), _object(edge.get("target"), "edge target")
        if edge.get("granularity") != "field" or edge.get("status") not in ({"active", "stale", "closed"} if historical else {"active"}):
            raise SuiteError("field lineage contains a non-active or non-field edge")
        evidence = _object(edge.get("evidence"), "field edge evidence")
        for endpoint in (source, target):
            matches = [node for node in nodes if node.get("item_id") == endpoint.get("item_id") and node.get("field_name") == endpoint.get("field_name")]
            if len(matches) != 1 or matches[0].get("schema_snapshot_hash") != endpoint.get("schema_snapshot_hash"):
                raise SuiteError("field edge crosses a schema snapshot")
        observed.add((source.get("item_id"), source.get("field_name"), target.get("item_id"), target.get("field_name"), edge.get("transformation"), evidence.get("execution_id")))
    expected_nodes = {(item_id, field)} | {(edge[0], edge[1]) for edge in expected} | {(edge[2], edge[3]) for edge in expected}
    if observed != expected or len(edges) != len(expected) or identities != expected_nodes or len(nodes) != len(expected_nodes):
        raise SuiteError("field lineage does not match the exact mappings/executions; old or unrelated fields may remain")
    root = [node for node in nodes if node.get("item_id") == item_id and node.get("field_name") == field][0]
    if root.get("schema_snapshot_hash") != subject.get("schema_snapshot_hash"):
        raise SuiteError("field lineage subject crosses a schema snapshot")
    return graph


def wait_field_graph(client: GatewayClient, item_id: int, field: str, expected: set[tuple[int, str, int, str, str, str]], timeout: float) -> dict[str, object]:
    deadline = time.monotonic() + timeout
    query = urllib.parse.urlencode({"subject_kind": "field_ref", "item_id": item_id, "field_name": field, "direction": "upstream", "depth": 3, "limit": 100})
    last_error = "collector has not converged"
    while time.monotonic() < deadline:
        payload = client.request("GET", "/api/v1/meta/lineage/graph?" + query, (200,)).payload
        try:
            return validate_field_graph(payload, item_id, field, expected)
        except SuiteError as error:
            last_error = str(error)
        time.sleep(1)
    raise SuiteError("field lineage did not converge: " + last_error)


def wait_transfer_target(client: GatewayClient, engine_id: int, full_name: str, timeout: float) -> dict[str, object]:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        items = _array(client.request("GET", f"/api/v1/meta/engines/{engine_id}/items", (200,)).payload, "Meta engine items")
        if any(isinstance(item, dict) and item.get("full_name") == full_name for item in items):
            # Once visible, use the strict canonical reader; malformed/duplicate facts fail immediately.
            return find_item(client, engine_id, full_name, "table")
        time.sleep(1)
    raise SuiteError(f"{full_name} did not appear through automatic metadata scanning")


def assert_transfer_execution_projection(execution):
    def fields(value, allowed, label):
        item = _object(value, label)
        if set(item) - set(allowed.split()):
            raise SuiteError(f"{label} exposed undeclared fields")
        return item

    fields(execution, "id execution_id task_id status start_time end_time records_read records_written bytes_read bytes_written error_details metadata trigger_type trigger_by", "Transfer execution")
    failure = fields(execution.get("error_details", {}), "category code error_code failed_targets_count diagnostic", "Transfer failure")
    if "diagnostic" in failure:
        diagnostic = fields(failure["diagnostic"], "schema_version stage reason sql write target_change_possible", "Transfer failure diagnostic")
        if diagnostic.get("schema_version") != "addp.execution-failure/v3":
            raise SuiteError("Transfer failure diagnostic uses an unknown contract")
        if "write" in diagnostic:
            write = fields(diagnostic["write"], "column_index declared_type", "Transfer failure write context")
            expected = {"json_invalid": "json", "array_invalid": "array", "bytes_invalid": "bytes"}
            if type(write.get("column_index")) is not int or not 1 <= write["column_index"] <= 65535 or diagnostic.get("reason") not in expected or write.get("declared_type") != expected[diagnostic["reason"]] or "sql" in diagnostic or diagnostic.get("stage") != "execution":
                raise SuiteError("Transfer failure write context is invalid")
        if "target_change_possible" in diagnostic and (diagnostic["target_change_possible"] is not True or diagnostic.get("stage") not in {"preparation", "execution", "result"}):
            raise SuiteError("Transfer target risk is invalid")
        if "sql" in diagnostic:
            sql = fields(diagnostic["sql"], "object position", "Transfer failure SQL context")
            if "object" in sql:
                fields(sql["object"], "kind name", "Transfer failure SQL object")
            if "position" in sql:
                fields(sql["position"], "line column", "Transfer failure SQL position")
    metadata = fields(execution.get("metadata", {}), "metadata_scan lineage_facts continuous stop_reason recovery_reason recovered_from_execution_id recovery_attempt recovery_consecutive_failures recovery_backoff_seconds recovery_not_before recovery_circuit_state", "Transfer professional metadata")
    if "metadata_scan" in metadata:
        fields(metadata["metadata_scan"], "status attempt execution_id error_code", "Transfer metadata scan")
    if "lineage_facts" in metadata:
        facts = fields(metadata["lineage_facts"], "schema_version inputs outputs operations runtime_execution_id meta_scan_refs", "Transfer lineage")
        for direction in ("inputs", "outputs"):
            for value in _array(facts.get(direction, []), "Transfer lineage resources"):
                resource = fields(value, "port locator item_id item_fingerprint write_mode schema_snapshot", "Transfer lineage resource")
                if "schema_snapshot" in resource:
                    snapshot = fields(resource["schema_snapshot"], "hash fields", "Transfer frozen schema")
                    for field in _array(snapshot.get("fields"), "Transfer frozen fields"):
                        fields(field, "name path type element_type native_type nullable primary_key size precision scale ordinal_position generated", "Transfer frozen field")
        for value in _array(facts.get("operations", []), "Transfer lineage operations"):
            operation = fields(value, "kind operator input_ports output_ports field_lineage_status field_mappings", "Transfer lineage operation")
            for mapping in _array(operation.get("field_mappings", []), "Transfer field mappings"):
                fields(mapping, "input_port output_port source_field target_field transformation", "Transfer field mapping")


def import_shapefile_zip() -> bytes:
    """Two WGS84 points; keep the upload fixture independent of GDAL installation."""
    def header(length):
        return struct.pack(">7i", 9994, 0, 0, 0, 0, 0, length // 2) + struct.pack(
            "<ii8d", 1000, 1, 10, 20, 30, 40, 0, 0, 0, 0)
    shp = header(156) + b"".join(struct.pack(">ii", i, 10) + struct.pack("<idd", 1, x, y)
                                for i, x, y in ((1, 10, 20), (2, 30, 40)))
    shx = header(116) + struct.pack(">4i", 50, 10, 64, 10)
    dbf = struct.pack("<BBBBIHH20x", 3, 126, 1, 1, 2, 97, 17)
    for name, kind in ((b"id", b"N"), (b"label", b"C")):
        dbf += struct.pack("<11sc4xBB14x", name, kind, 8, 0)
    dbf += b"\r" + b"        1alpha   " + b"        2beta    " + b"\x1a"
    files = {"shp": shp, "shx": shx, "dbf": dbf, "cpg": b"UTF-8",
             "prj": b'GEOGCS["WGS 84",DATUM["WGS_1984",SPHEROID["WGS 84",6378137,298.257223563]],PRIMEM["Greenwich",0],UNIT["degree",0.0174532925199433],AUTHORITY["EPSG","4326"]]'}
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as archive:
        for extension, content in files.items():
            archive.writestr("points." + extension, content)
    return output.getvalue()




def object_upload_files():
    with zipfile.ZipFile(io.BytesIO(import_shapefile_zip())) as archive:
        return {"upload-check.csv": b"id,value\n1,alpha\n2,beta\n", **{name: archive.read(name) for name in archive.namelist()}}


def source_instant(value):
    if not isinstance(value, str):
        raise SuiteError("object source modification time is missing")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise SuiteError("object source modification time is invalid") from error
    if parsed.tzinfo is None or parsed.year < 2000:
        raise SuiteError("object source modification time must identify an actual UTC instant")
    return parsed.astimezone(timezone.utc)


def validate_object_upload_facts(items, physical, files, engine_id):
    if physical.get("schema_version") != "addp.manager-object-physical/v1" or physical.get("bucket") != "transfer-upload":
        raise SuiteError("object physical evidence has the wrong schema or bucket")
    objects = _object(physical.get("objects"), "physical objects")
    if set(objects) != set(files):
        raise SuiteError("physical object set differs from upload")
    for name, content in files.items():
        facts = _object(objects[name], "physical object facts")
        if type(facts.get("size_bytes")) is not int or facts["size_bytes"] <= 0 or facts["size_bytes"] != len(content) or facts.get("sha256") != hashlib.sha256(content).hexdigest():
            raise SuiteError("physical uploaded bytes differ from the independent fixture")
        if not isinstance(facts.get("etag"), str) or not facts["etag"]:
            raise SuiteError("physical source ETag is missing")
        source_instant(facts.get("last_modified"))
    by_name = {item.get("full_name"): item for item in items}
    if len(items) != 2 or set(by_name) != {"transfer-upload/upload-check.csv", "transfer-upload/points.shp"}:
        raise SuiteError("automatic upload scan must create exactly two logical items")
    for name, item in by_name.items():
        if item.get("engine_id") != engine_id or not item.get("fingerprint") or item.get("scanned_depth") != "deep":
            raise SuiteError("uploaded Meta item lost its actual engine identity or deep scan")
        attrs = _object(item.get("attributes"), "uploaded item attributes")
        storage = _object(attrs.get("storage"), "uploaded item storage")
        descriptor = _object(attrs.get("item"), "uploaded item descriptor")
        single = name.endswith(".csv")
        expected = objects["upload-check.csv"]["size_bytes"] if single else sum(objects[key]["size_bytes"] for key in files if key != "upload-check.csv")
        if type(item.get("size_bytes")) is not int or type(storage.get("total_size")) is not int or item["size_bytes"] != expected or storage["total_size"] != expected:
            raise SuiteError("Meta source size must equal the actual physical object bytes")
        if descriptor.get("layout") != ("single" if single else "multi") or descriptor.get("format") != ("csv" if single else "shapefile"):
            raise SuiteError("upload logical composition changed")
        if single:
            facts = objects["upload-check.csv"]
            if storage.get("etag") != facts["etag"] or source_instant(item.get("data_updated_at")) != source_instant(facts["last_modified"]) or source_instant(storage.get("last_modified_at")) != source_instant(facts["last_modified"]):
                raise SuiteError("single object source ETag or modification time differs from Meta")
        else:
            refs = _array(descriptor.get("refs"), "uploaded Shapefile refs")
            expected_refs = {"transfer-upload/" + key for key in files if key != "upload-check.csv"}
            if len(refs) != 5 or {ref.get("path") for ref in refs} != expected_refs:
                raise SuiteError("Shapefile refs differ from the uploaded objects")
    return {"logical_items": 2, "physical_objects": len(objects), "single_source_facts_verified": True,
            "multi_physical_size_verified": True}


def run_object_upload(client, repository, identity, timeout):
    engine_id = positive_int(required_environment("ADDP_ONLINE_TRANSFER_MINIO_ENGINE_ID"), "upload MinIO engine id")
    validate_engine(client, engine_id, "Hosted Transfer MinIO", "minio", time.monotonic() + timeout)
    wait_manager_read_boundary(client, engine_id, time.monotonic() + timeout)
    if _array(client.request("GET", f"/api/v1/meta/engines/{engine_id}/items", (200,)).payload, "fresh object items"):
        raise SuiteError("object upload requires a fresh engine without scanned items")
    artifact = Path(required_environment("ADDP_ONLINE_ARTIFACT_DIR"))
    files = object_upload_files()
    directory = artifact / "manager-object-upload"
    directory.mkdir()
    for name, content in files.items():
        (directory / name).write_bytes(content)
    target = f"addp://engine/{engine_id}/path/transfer-upload?type=bucket"
    (artifact / "manager-object-upload-input.json").write_text(json.dumps({"files": sorted(files), "target_node_locator": target}))
    report_path = artifact / "manager-object-upload-api.json"
    report_path.unlink(missing_ok=True)
    response = subprocess.run(["npm", "exec", "--", "playwright", "test", "--config=playwright.online.config.js",
        "e2e/online/transfer-relational-sql-etl.spec.js", "--grep", "Manager object upload submits its actual automatic Meta scan$"],
        cwd=repository / "console/frontend", capture_output=True, text=True)
    if response.returncode:
        raise SuiteError("Manager public object upload API failed")
    report = _object(json.loads(report_path.read_text()), "object upload API proof")
    if set(report) != {"schema_version", "run_id", "principal_id", "tenant_id", "submission"} or report.get("schema_version") != "addp.manager-object-upload-api/v1" or report.get("run_id") != os.environ["ADDP_ONLINE_TEST_RUN_ID"] or report.get("principal_id") != identity["principal_id"] or report.get("tenant_id") != identity["tenant_id"]:
        raise SuiteError("object upload API proof lost its current User/Tenant/run")
    submission = _object(report.get("submission"), "object upload submission")
    identifier = submission.get("scan_execution_id")
    try:
        actual_uuid = isinstance(identifier, str) and str(uuid.UUID(identifier)) == identifier
    except ValueError:
        actual_uuid = False
    if set(submission) != {"scan_execution_id", "target_node_locator", "files"} or not actual_uuid or submission.get("target_node_locator") != target:
        raise SuiteError("object upload must return its actual Meta UUID and target")
    uploaded = _array(submission.get("files"), "uploaded objects")
    if len(uploaded) != len(files) or {entry.get("file_name") for entry in uploaded} != set(files):
        raise SuiteError("upload response lost actual object identities")
    for entry in uploaded:
        if set(entry) != {"file_name", "locator"}:
            raise SuiteError("upload response contains undeclared source fields")
        expected = f"addp://engine/{engine_id}/path/transfer-upload/{entry['file_name']}?type=object"
        if entry.get("locator") != expected:
            raise SuiteError("uploaded source locator is outside the exact engine/bucket")
    execution = wait_owner_execution(client, "meta", identifier, timeout)
    if execution.get("module") != "meta" or execution.get("source") != "manager" or execution.get("parent_execution_id") not in (None, "") or str(execution.get("triggered_by")) != identity["principal_id"] or str(execution.get("tenant_id")) != identity["tenant_id"] or execution.get("scan_context") != {"engine_id": engine_id, "scan_depth": "deep"}:
        raise SuiteError("object automatic scan lost its direct User origin or exact engine")
    listed = _array(client.request("GET", f"/api/v1/meta/engines/{engine_id}/items", (200,)).payload, "uploaded items")
    items = [_object(client.request("GET", f"/api/v1/meta/items/{positive_int(item.get('id'), 'uploaded item id')}", (200,)).payload, "uploaded item") for item in listed]
    checked = subprocess.run(["bash", "business/scripts/online-transfer-relational-sql-etl-fixture.sh", "object-facts"], cwd=repository, capture_output=True, text=True, timeout=60)
    if checked.returncode:
        raise SuiteError("independent object physical facts failed")
    physical = _object(json.loads(checked.stdout), "physical object proof")
    proof = validate_object_upload_facts(items, physical, files, engine_id)
    commit = subprocess.run(["git", "rev-parse", "HEAD"], cwd=repository, capture_output=True, text=True, check=True).stdout.strip()
    receipt = {"schema_version": "addp.manager-object-upload/v1", "result": "passed", "git_commit": commit,
        "run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"], "identity": identity, "engine_id": engine_id,
        "api": report, "scan_execution_id": identifier,
        "scan": {key: execution.get(key) for key in ("execution_id", "status", "module", "source", "parent_execution_id", "triggered_by", "tenant_id", "scan_context")},
        "items": items, "physical": physical, **proof}
    (artifact / "manager-object-upload.json").write_text(json.dumps(receipt, sort_keys=True) + "\n")
    return {"engine_id": engine_id, "scan_execution_id": identifier, **proof}

def run_manager_import_browser(engine_id, identity, owned_ids, proof=None):
    repository = Path(os.environ.get("ADDP_ONLINE_REPOSITORY", Path(__file__).parents[2])).resolve()
    artifact_dir = Path(required_environment("ADDP_ONLINE_ARTIFACT_DIR"))
    run_id = required_environment("ADDP_ONLINE_TEST_RUN_ID")
    stage = "rejection" if proof is not None else "upload"
    report_path = artifact_dir / f"manager-import-{stage}-browser.json"
    report_path.unlink(missing_ok=True)
    arguments = {"stage": stage, "target_table": IMPORT_TARGET}
    if proof is None:
        (artifact_dir / "manager-import-points.zip").write_bytes(import_shapefile_zip())
    else:
        arguments.update({"transfer_task_id": proof["transfer_task_id"], "rejected_execution_id": proof["rejected_execution_id"]})
    environment = dict(os.environ)
    environment.update({"ADDP_ONLINE_REPOSITORY": str(repository), "ADDP_ONLINE_TEST_ENGINE_ID": str(engine_id),
                        "ADDP_ONLINE_MANAGER_IMPORT_BROWSER": json.dumps(arguments)})
    try:
        result = subprocess.run(["npm", "exec", "--", "playwright", "test", "--config=playwright.online.config.js",
                                 "e2e/online/transfer-relational-sql-etl.spec.js", "--grep", "browser imports a private Manager source through Console$"],
                                cwd=repository / "console/frontend", env=environment, capture_output=True, text=True)
    finally:
        report = _object(json.loads(report_path.read_text()), "Manager import browser report") if report_path.is_file() else {}
        if stage == "upload" and isinstance(report.get("submission"), dict):
            # Capture accepted identities even if the browser process is interrupted.
            task_id = positive_int(report["submission"].get("transfer_task_id"), "browser import task id")
            if task_id not in owned_ids:
                owned_ids.append(task_id)
    for output, stream in ((result.stdout, sys.stdout), (result.stderr, sys.stderr)):
        if output:
            print(output, file=stream, end="" if output.endswith("\n") else "\n")
    if result.returncode != 0:
        raise SuiteError(f"Manager import {stage} browser failed with status {result.returncode}")
    return validate_manager_import_browser(report, run_id, identity, stage, proof)


def validate_manager_import_browser(report, run_id, identity, stage, proof=None):
    allowed = {"schema_version", "result", "run_id", "principal_id", "tenant_id", "stage"}
    allowed.update({"submission", "meta_scan_execution_id", "upload_ui_verified", "metadata_wait_verified"} if stage == "upload"
                   else {"rejected_execution_id", "failure_ui_verified"})
    if set(report) - allowed:
        raise SuiteError("Manager import browser proof exposed undeclared fields")
    if (report.get("schema_version") != "addp.manager-private-import-browser/v1" or report.get("result") != "passed"
            or report.get("run_id") != run_id or report.get("stage") != stage
            or report.get("principal_id") != identity["principal_id"] or report.get("tenant_id") != identity["tenant_id"]):
        raise SuiteError("Manager import browser proof lost its current User, Tenant, run or stage")
    if stage == "upload":
        submission = _object(report.get("submission"), "browser import submission")
        positive_int(submission.get("transfer_task_id"), "browser import task id")
        for identifier in (submission.get("transfer_execution_id"), report.get("meta_scan_execution_id")):
            if not isinstance(identifier, str) or str(uuid.UUID(identifier)) != identifier:
                raise SuiteError("Manager import browser proof requires actual execution UUIDs")
        if report.get("upload_ui_verified") is not True or report.get("metadata_wait_verified") is not True:
            raise SuiteError("Manager import browser proof is missing upload or Meta completion")
    elif (stage != "rejection" or report.get("failure_ui_verified") is not True
            or report.get("rejected_execution_id") != proof["rejected_execution_id"]):
        raise SuiteError("Manager import browser proof is missing the actual cross-user failure")
    return report


def run_manager_import(client, engine_id, identity, timeout, owned_ids, outsider):
    browser_upload = run_manager_import_browser(engine_id, identity, owned_ids)
    submitted = browser_upload["submission"]
    task_id = positive_int(submitted.get("transfer_task_id"), "import task id")
    if task_id not in owned_ids:
        owned_ids.append(task_id)
    identifier = submitted.get("transfer_execution_id")
    if not isinstance(identifier, str) or str(uuid.UUID(identifier)) != identifier:
        raise SuiteError("Manager import must return the actual Transfer execution UUID")
    task = _object(client.request("GET", f"/api/v1/transfer/task-definitions/{task_id}", (200,)).payload, "import task")
    config = _object(task.get("config"), "import config")
    if (task.get("task_type") != "sync" or task.get("auto_scan_metadata") is not True
            or config.get("runtime") != {"boundary": "bounded"} or config.get("load") != {"mode": "snapshot"}
            or _object(config.get("target"), "import target").get("policy") != {"apply_mode": "replace"}
            or "mode" in config):
        raise SuiteError("Manager import did not persist the current bounded snapshot contract")
    execution = wait_owner_execution(client, "transfer", identifier, timeout)
    if execution.get("records_read") != 2 or execution.get("records_written") != 2:
        raise SuiteError("Manager import must transfer both uploaded points")
    scan = _object(_object(execution.get("metadata"), "import metadata").get("metadata_scan"), "automatic scan")
    if scan.get("status") != "submitted" or not scan.get("execution_id"):
        raise SuiteError("Manager import automatic scan was not submitted successfully")
    child = wait_owner_execution(client, "meta", scan["execution_id"], timeout)
    if (child.get("module") != "meta" or child.get("source") != "transfer"
            or child.get("parent_execution_id") != identifier
            or str(child.get("triggered_by")) != identity["principal_id"]
            or str(child.get("tenant_id")) != identity["tenant_id"]
            or child.get("scan_context", {}).get("engine_id") != engine_id):
        raise SuiteError("automatic Meta scan lost its actual parent, current User or Tenant")
    item = wait_transfer_target(client, engine_id, "public." + IMPORT_TARGET, timeout)
    source = _object(config.get("source"), "import source").get("locator")
    if not isinstance(source, str) or not source:
        raise SuiteError("Manager import lost its exact source locator")
    for user in (client, outsider):
        user.request("POST", "/api/v1/meta/inspect", (403,), {"locator": source})
    denied_execution = verify_import_private_access(outsider, task_id, identity, timeout)

    proof = {"transfer_task_id": task_id, "transfer_execution_id": identifier,
            "meta_scan_execution_id": scan["execution_id"], "target_item_id": item["id"],
            "records_read": 2, "records_written": 2, "current_user_origin_verified": True,
            "automatic_scan_verified": True, "user_inspect_rejected": True, "non_uploader_rejected": True, "rejected_execution_id": denied_execution}
    if browser_upload["meta_scan_execution_id"] != scan["execution_id"]:
        raise SuiteError("Manager UI waited for a different Meta child")
    proof["browser"] = {"upload": browser_upload, "rejection": run_manager_import_browser(engine_id, identity, owned_ids, proof)}
    return proof


def verify_scan_trace_visibility(client, outsider, identity, outsider_identity, scan_id, parent_id=None):
    """A readable task parent never grants access to another User's ad-hoc scan."""
    if (outsider_identity.get("tenant_id") != identity["tenant_id"]
            or outsider_identity.get("principal_id") == identity["principal_id"]
            or outsider_identity.get("principal_type") != "user"
            or set(outsider_identity.get("permissions_verified", [])) != REQUIRED_PERMISSIONS):
        raise SuiteError("trace peer must be a distinct same-Tenant User with the same existing permissions")
    monitor = "/api/v1/monitor/executions/by-execution-id/"
    source = "transfer" if parent_id else "manager"
    detail = _object(client.request("GET", monitor + scan_id, (200,)).payload, "Monitor scan")
    if (detail.get("execution_id") != scan_id or detail.get("module") != "meta"
            or detail.get("task_type") != "scan" or detail.get("status") != "success"
            or detail.get("source") != source or detail.get("source_task_id") is not None
            or detail.get("parent_execution_id") != parent_id
            or detail.get("triggered_by") != int(identity["principal_id"])):
        raise SuiteError("Monitor scan lost its exact origin or direct parent")

    observation_fields = ("execution_id", "module", "task_type", "status", "source",
                          "source_task_id", "parent_execution_id", "triggered_by")

    def tree(user, root_id, expected):
        value = _object(user.request("GET", monitor + root_id + "/tree", (200,)).payload, "Monitor trace tree")
        nodes = {}
        def visit(node, parent):
            node = _object(node, "trace node")
            if set(node) != {"execution", "children", "truncated"} or node["truncated"] is not False:
                raise SuiteError("trace tree must be complete and contain only declared fields")
            execution = _object(node["execution"], "trace execution")
            identifier = execution.get("execution_id")
            if identifier not in expected or identifier in nodes or execution.get("parent_execution_id") != parent:
                raise SuiteError("trace tree contains a missing, duplicated or unrelated execution")
            if execution.get("module") != expected[identifier] or execution.get("status") != "success":
                raise SuiteError("trace execution owner or terminal status differs")
            nodes[identifier] = execution
            for child in _array(node["children"], "trace children"):
                visit(child, identifier)
        visit(value, parent_id if root_id == scan_id else None)
        if set(nodes) != set(expected):
            raise SuiteError("trace tree lost an expected execution")
        if scan_id in nodes and nodes[scan_id] != detail:
            raise SuiteError("Monitor scan detail differs from its tree projection")
        return [{key: nodes[identifier].get(key) for key in observation_fields} for identifier in sorted(nodes)]

    root_id = parent_id or scan_id
    expected = {scan_id: "meta", **({parent_id: "transfer"} if parent_id else {})}
    visible = tree(client, root_id, expected)
    # Verify the child's own tree as well; it must not invent or climb to a parent.
    tree(client, scan_id, {scan_id: "meta"})
    hidden_paths = [f"/api/v1/meta/executions/{scan_id}"] + [monitor + scan_id + suffix for suffix in ("", "/tree", "/events")]
    peer_denials = []
    for path in hidden_paths:
        response = outsider.request("GET", path, (404,))
        denied = _object(response.payload, "hidden scan error")
        if response.status != 404 or set(denied) - {"error", "error_code"} or not denied.get("error"):
            raise SuiteError("hidden scan response leaked execution data")
        peer_denials.append({"path": path, "status": response.status, "response_fields": sorted(denied)})
    peer_visible = []
    if parent_id:
        parent = _object(outsider.request("GET", monitor + parent_id, (200,)).payload, "peer task parent")
        if parent.get("execution_id") != parent_id or parent.get("module") != "transfer":
            raise SuiteError("peer cannot read the actual task parent")
        peer_visible = tree(outsider, parent_id, {parent_id: "transfer"})
    return {"scan_execution_id": scan_id, "parent_execution_id": parent_id,
            "owner_visible_execution_ids": [node["execution_id"] for node in visible],
            "peer_visible_execution_ids": [node["execution_id"] for node in peer_visible],
            "owner_visible_observations": visible, "peer_visible_observations": peer_visible,
            "peer_denials": peer_denials,
            "peer_principal_id": outsider_identity["principal_id"], "same_permissions_verified": True,
            "owner_detail_tree_consistent": True, "peer_hidden_endpoints": 4,
            "parent_read_does_not_grant_scan_read": bool(parent_id)}


def record_scan_trace_receipt(client, outsider, identity, peer_identity, object_upload, manager_import):
    cases = {
        "object_upload": verify_scan_trace_visibility(client, outsider, identity, peer_identity, object_upload["scan_execution_id"]),
        "bounded_import": verify_scan_trace_visibility(client, outsider, identity, peer_identity,
            manager_import["meta_scan_execution_id"], manager_import["transfer_execution_id"]),
    }
    artifact = Path(required_environment("ADDP_ONLINE_ARTIFACT_DIR"))
    upload = _object(json.loads((artifact / "manager-object-upload.json").read_text()), "object upload receipt")
    run_id = required_environment("ADDP_ONLINE_TEST_RUN_ID")
    if (upload.get("schema_version") != "addp.manager-object-upload/v1" or upload.get("result") != "passed"
            or upload.get("run_id") != run_id or upload.get("identity") != identity
            or upload.get("scan_execution_id") != object_upload["scan_execution_id"]):
        raise SuiteError("scan trace receipt is not bound to the actual current upload")
    receipt = {"schema_version": "addp.scan-trace-visibility/v1", "result": "passed",
        "run_id": run_id, "git_commit": upload["git_commit"],
        "identity": identity, "peer_identity": peer_identity, "cases": cases}
    (artifact / "scan-trace-visibility.json").write_text(json.dumps(receipt, sort_keys=True) + "\n")
    return cases


def record_manager_import_receipt(repository, artifact_dir, run_id, identity, engine_id, proof):
    browser = _object(proof.get("browser"), "Manager import browser proofs")
    for stage in ("upload", "rejection"):
        validate_manager_import_browser(_object(browser.get(stage), "Manager import browser stage"), run_id, identity, stage, proof)
    upload = browser["upload"]
    if (upload["submission"] != {"transfer_task_id": proof["transfer_task_id"], "transfer_execution_id": proof["transfer_execution_id"]}
            or upload["meta_scan_execution_id"] != proof["meta_scan_execution_id"]):
        raise SuiteError("Manager import receipt lost its actual browser submission or Meta child")
    checked = subprocess.run(["bash", "business/scripts/online-transfer-relational-sql-etl-fixture.sh", "manager-import"],
                             cwd=repository, capture_output=True, text=True, timeout=60)
    if checked.returncode != 0:
        raise SuiteError("Manager import independent physical verification failed")
    source = subprocess.run(["git", "rev-parse", "HEAD"], cwd=repository, capture_output=True, text=True, timeout=10)
    commit = source.stdout.strip()
    if source.returncode != 0 or len(commit) != 40 or any(c not in "0123456789abcdef" for c in commit):
        raise SuiteError("Manager import receipt requires the actual source commit")
    proof["physical_geometry_verified"] = True
    receipt = {"schema_version": "addp.manager-private-import/v2", "result": "passed", "run_id": run_id,
               "git_commit": commit, "identity": identity, "engine_id": engine_id, "manager_import": proof}
    path = Path(artifact_dir) / "manager-private-import.json"
    path.write_text(json.dumps(receipt, sort_keys=True) + "\n")


def verify_import_private_access(outsider, task_id, owner_identity, timeout):
    identity = validate_user_identity(outsider, int(owner_identity["tenant_id"]))
    if identity["principal_id"] == owner_identity["principal_id"]:
        raise SuiteError("import private-source proof requires a distinct User")
    started = _object(outsider.request("POST", f"/api/v1/transfer/task-definitions/{task_id}/start", (200,)).payload, "non-uploader execution")
    identifier = started.get("execution_id")
    if not isinstance(identifier, str) or str(uuid.UUID(identifier)) != identifier:
        raise SuiteError("non-uploader admission lost its execution UUID")
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        result = _object(outsider.request("GET", f"/api/v1/transfer/executions/{identifier}", (200,)).payload, "non-uploader execution")
        assert_transfer_execution_projection(result)
        if result.get("status") in SUPPORT.TERMINAL_STATUSES:
            if (result.get("status") != "failed" or result.get("error_details", {}).get("code") != "data_protection_required"
                    or result.get("records_read") != 0 or result.get("records_written") != 0
                    or "metadata_scan" in result.get("metadata", {})):
                raise SuiteError("non-uploader was not rejected before reading and writing")
            return identifier
        time.sleep(1)
    raise SuiteError("non-uploader execution did not fail before timeout")


def execution_schema_hashes(execution: dict[str, object]) -> tuple[str, str]:
    assert_transfer_execution_projection(execution)
    facts = _object(_object(execution.get("metadata"), "native execution metadata").get("lineage_facts"), "native lineage facts")
    inputs, outputs, operations = (_array(facts.get(key), key) for key in ("inputs", "outputs", "operations"))
    if facts.get("schema_version") != "addp.lineage-facts/v1" or len(inputs) != 1 or len(outputs) != 1 or len(operations) != 1:
        raise SuiteError("native execution must persist one source, target and operation")
    if _object(operations[0], "native operation").get("field_lineage_status") != "complete":
        raise SuiteError("native execution must persist complete field lineage")
    hashes = []
    for reference, port in ((inputs[0], "source"), (outputs[0], "target")):
        resource = _object(reference, "native lineage resource")
        snapshot = _object(resource.get("schema_snapshot"), "native frozen schema")
        if resource.get("port") != port or not isinstance(snapshot.get("hash"), str) or not snapshot["hash"] or not _array(snapshot.get("fields"), "frozen schema fields"):
            raise SuiteError("native execution is missing its frozen source/target schema")
        hashes.append(snapshot["hash"])
    return hashes[0], hashes[1]


def validate_graph_snapshots(graph: dict[str, object], expected: dict[int, str]) -> None:
    for node in graph["nodes"]:
        if node["schema_snapshot_hash"] != expected.get(node["item_id"]):
            raise SuiteError("Meta field graph does not match the owner's frozen execution schemas")


def validate_multi_source_execution(execution, bindings, target_locator):
    assert_transfer_execution_projection(execution)
    facts = _object(_object(execution.get("metadata"), "multi-source metadata").get("lineage_facts"), "multi-source facts")
    inputs, outputs, operations = (_array(facts.get(key), key) for key in ("inputs", "outputs", "operations"))
    if (facts.get("schema_version") != "addp.lineage-facts/v1" or len(inputs) != 2 or len(outputs) != 1 or len(operations) != 1
            or execution.get("records_read") != 4 or execution.get("records_written") != 4
            or not isinstance(execution.get("execution_id"), str) or not execution["execution_id"]):
        raise SuiteError("multi-source execution must preserve exact inputs, output and four rows")
    if {ref.get("port"): ref.get("locator") for ref in inputs} != bindings:
        raise SuiteError("multi-source frozen input ports differ from declarations")
    output, operation = outputs[0], operations[0]
    if (output.get("port") != "target" or output.get("locator") != target_locator or output.get("write_mode") != "replace"
            or operation.get("field_lineage_status") != "complete" or operation.get("input_ports") != ["base", "mapped"]
            or operation.get("output_ports") != ["target"]):
        raise SuiteError("multi-source write must record complete field evidence and actual target")
    mappings = _array(operation.get("field_mappings"), "multi-source field mappings")
    expected = {("base", "id", "id", "derived"), ("base", "region", "combined_label", "derived"),
                ("mapped", "region_name", "combined_label", "derived"), ("base", "amount", "combined_amount", "derived"),
                ("mapped", "amount", "combined_amount", "derived")}
    observed = {(m.get("input_port", ""), m.get("source_field", ""), m.get("target_field"), m.get("transformation")) for m in mappings}
    if observed != expected or len(mappings) != len(expected) or any(m.get("output_port") != "target" for m in mappings):
        raise SuiteError("multi-source exact value origins differ; JOIN-only fields must not become origins")
    expected_fields = {"base": ["id", "region", "status", "amount", "internal_note"],
                       "mapped": ["id", "region_name", "amount", "generated_label"],
                       "target": ["id", "combined_label", "combined_amount"]}
    hashes = {}
    for ref in inputs + outputs:
        snapshot = _object(ref.get("schema_snapshot"), "multi-source frozen schema")
        fields = _array(snapshot.get("fields"), "multi-source frozen fields")
        if ([f.get("name") for f in fields] != expected_fields[ref["port"]]
                or any("path" in f for f in fields)
                or not isinstance(snapshot.get("hash"), str) or not snapshot["hash"]):
            raise SuiteError("multi-source frozen schema lost original fields or precise paths")
        hashes[ref["port"]] = snapshot["hash"]
    return hashes


def run_multi_source_lineage(client, engine_id, source, native, name, timeout, owned_ids):
    mapped = find_item(client, engine_id, "public." + NATIVE_TARGET, "table")
    bindings = {"base": SUPPORT.build_item_locator(engine_id, source), "mapped": SUPPORT.build_item_locator(engine_id, mapped)}
    parent = f"addp://engine/{engine_id}/path/public?type=schema&node_id={positive_int(source.get('node_id'), 'multi-source parent')}"
    payload = native_task(name + "_multi", bindings["base"], parent, MULTI_TARGET, "region", "combined_label")
    payload["config"]["source"]["query"] = {
        "language": "sql", "statement": f'''WITH joined AS (
            SELECT a.id, a.region || ':' || b.region_name AS label, a.amount + b.amount AS amount
            FROM public.{SOURCE_TABLE} a JOIN public.{NATIVE_TARGET} b ON a.id=b.id
            WHERE a.status=:status AND a.id>=:minimum
        ) SELECT id,label,amount FROM joined UNION ALL SELECT id,label,amount FROM joined''',
        "parameters": {"status": "active", "minimum": 3},
        "inputs": [{"name": port, "locator": locator} for port, locator in bindings.items()],
    }
    payload["config"]["transforms"][0]["fields"] = [
        {"source": "id", "target": "id", "target_type": "bigint", "nullable": False},
        {"source": "label", "target": "combined_label", "target_type": "string", "nullable": False},
        {"source": "amount", "target": "combined_amount", "target_type": "decimal", "precision": 10, "scale": 2, "nullable": False},
    ]
    _, execution = SUPPORT.create_and_run_task(client, payload, time.monotonic() + timeout, owned_ids)
    target_locator = f"addp://engine/{engine_id}/path/public/{MULTI_TARGET}?type=table"
    hashes = validate_multi_source_execution(execution, bindings, target_locator)
    target = wait_transfer_target(client, engine_id, "public." + MULTI_TARGET, timeout)
    source_id, mapped_id, target_id = (positive_int(item.get("id"), "multi-source item") for item in (source, mapped, target))
    identifier = execution["execution_id"]
    previous = native["execution_ids"][1]
    expected = {
        "id": {(source_id, "id", target_id, "id", "derived", identifier)},
        "combined_label": {(source_id, "region", target_id, "combined_label", "derived", identifier),
                           (mapped_id, "region_name", target_id, "combined_label", "derived", identifier),
                           (source_id, "status", mapped_id, "region_name", "direct", previous)},
        "combined_amount": {(source_id, "amount", target_id, "combined_amount", "derived", identifier),
                            (mapped_id, "amount", target_id, "combined_amount", "derived", identifier),
                            (source_id, "amount", mapped_id, "amount", "derived", previous)},
    }
    for field, edges in expected.items():
        graph = wait_field_graph(client, target_id, field, edges, timeout)
        validate_graph_snapshots(graph, {source_id: hashes["base"], mapped_id: hashes["mapped"], target_id: hashes["target"]})
    return {"execution_id": identifier, "target_item_id": target_id, "records_written": 4,
            "input_schema_hashes": {"base": hashes["base"], "mapped": hashes["mapped"]}, "target_schema_hash": hashes["target"],
            "multiple_origins_verified": True, "cte_join_union_verified": True,
            "exact_field_mappings_verified": True, "graph_snapshots_verified": True}


def validate_wide_graph(payload, engine_id, item_ids, hashes, execution_ids):
    graph = _object(payload, "wide field graph")
    subject = _object(graph.get("subject"), "wide graph subject")
    if (graph.get("granularity") != "field" or graph.get("truncated") or graph.get("field_lineage_status") != "complete"
            or subject.get("kind") != "data_item" or subject.get("item_id") != item_ids[-1]
            or subject.get("schema_snapshot_hash") != hashes[item_ids[-1]]):
        raise SuiteError("wide field graph subject/status mismatch")
    nodes = _array(graph.get("nodes"), "wide nodes")
    edges = _array(graph.get("edges"), "wide edges")
    expected_nodes = {(item, field) for item in item_ids for field in WIDE_FIELDS}
    actual_nodes = {(node.get("item_id"), node.get("field_name")) for node in nodes}
    if len(nodes) != 1500 or actual_nodes != expected_nodes:
        raise SuiteError("wide field graph must contain all 1500 precise field nodes")
    for node in nodes:
        if node.get("kind") != "field_ref" or node.get("engine_id") != engine_id or node.get("schema_snapshot_hash") != hashes[node["item_id"]]:
            raise SuiteError("wide field node engine/snapshot mismatch")
    expected_edges = {(item_ids[i], field, item_ids[i+1], field, execution_ids[i]) for i in range(2) for field in WIDE_FIELDS}
    actual_edges = set()
    for edge in edges:
        source, target = edge["source"], edge["target"]
        if edge.get("status") != "active" or edge.get("granularity") != "field" or edge.get("transformation") != "direct":
            raise SuiteError("wide field edge must be active direct evidence")
        for endpoint in (source, target):
            if endpoint.get("engine_id") != engine_id or endpoint.get("schema_snapshot_hash") != hashes.get(endpoint.get("item_id")):
                raise SuiteError("wide field edge engine/snapshot mismatch")
        actual_edges.add((source.get("item_id"), source.get("field_name"), target.get("item_id"), target.get("field_name"), edge.get("evidence", {}).get("execution_id")))
    if len(edges) != 1000 or actual_edges != expected_edges:
        raise SuiteError("wide field graph must contain all 1000 exact execution mappings")
    return graph


def run_wide_lineage(client, engine_id, name, timeout, owned_ids):
    source = find_item(client, engine_id, f"public.{WIDE_SOURCE}", "table")
    parent = f"addp://engine/{engine_id}/path/public?type=schema&node_id={positive_int(source.get('node_id'), 'wide source schema')}"
    item_ids, hashes, execution_ids = [positive_int(source.get("id"), "wide source item")], {}, []
    for suffix, target_name in (("_wide", WIDE_TARGET), ("_wide_hop", WIDE_DOWNSTREAM)):
        task = native_task(name + suffix, SUPPORT.build_item_locator(engine_id, source), parent, target_name, "region", "region")
        task["config"]["transforms"][0]["fields"] = [{"source": field, "target": field, "target_type": "bigint", "nullable": False} for field in WIDE_FIELDS]
        _, execution = SUPPORT.create_and_run_task(client, task, time.monotonic() + timeout, owned_ids)
        if execution.get("records_read") != 2 or execution.get("records_written") != 2:
            raise SuiteError("wide transfer must read/write exactly two rows")
        source_hash, target_hash = execution_schema_hashes(execution)
        if item_ids[-1] in hashes and hashes[item_ids[-1]] != source_hash:
            raise SuiteError("wide hop changed its frozen input schema")
        hashes[item_ids[-1]] = source_hash
        target = wait_transfer_target(client, engine_id, f"public.{target_name}", timeout)
        item_ids.append(positive_int(target.get("id"), "wide target item"))
        hashes[item_ids[-1]] = target_hash
        identifier = execution.get("execution_id")
        if not isinstance(identifier, str) or not identifier:
            raise SuiteError("wide execution id missing")
        execution_ids.append(identifier)
        source = target
    query = urllib.parse.urlencode({"subject_kind": "data_item", "granularity": "field", "item_id": item_ids[-1], "direction": "both", "depth": 2})
    deadline, last_error = time.monotonic() + timeout, "collector has not converged"
    while time.monotonic() < deadline:
        started = time.monotonic()
        graph = client.request("GET", "/api/v1/meta/lineage/graph?" + query, (200,)).payload
        elapsed = time.monotonic() - started
        try:
            validate_wide_graph(graph, engine_id, item_ids, hashes, execution_ids)
            return {"target_locator": SUPPORT.build_item_locator(engine_id, source), "target_item_id": item_ids[-1], "item_ids": item_ids,
                    "schema_hashes": hashes, "execution_ids": execution_ids, "field_count": 500, "node_count": 1500, "edge_count": 1000,
                    "query_seconds": elapsed, "schema_snapshot_hash": hashes[item_ids[-1]]}
        except SuiteError as error:
            last_error = str(error)
        time.sleep(1)
    raise SuiteError("wide graph did not converge: " + last_error)


def read_transfer_scan_boundary(client, engine_id, source):
    # The bounded transport has its own exception contract.
    try:
        return API.read_meta_scan_boundary(client, engine_id, source)
    except API.SuiteError as error:
        raise SuiteError(str(error)) from error


def run_native_lineage(client: GatewayClient, engine_id: int, source: dict[str, object], name: str, timeout: float, owned_ids: list[int]) -> dict[str, object]:
    source_id = positive_int(source.get("id"), "source item id")
    parent = f"addp://engine/{engine_id}/path/public?type=schema&node_id={positive_int(source.get('node_id'), 'source schema node id')}"
    source_locator = SUPPORT.build_item_locator(engine_id, source)
    scan_boundary = read_transfer_scan_boundary(client, engine_id, source)
    # Meta's read DTO timestamps have second precision. Cross that observed tick
    # before triggering work so a parent rescan cannot compare equal by truncation.
    next_scan_tick = datetime.fromisoformat(scan_boundary["sibling"]["scanned_at"].replace("Z", "+00:00")).timestamp() + 1
    clock_deadline = time.monotonic() + timeout
    while time.time() < next_scan_tick:
        if time.monotonic() >= clock_deadline:
            raise SuiteError("baseline scan timestamp exceeds the verification clock budget")
        time.sleep(0.05)

    def assert_scan_boundary():
        if read_transfer_scan_boundary(client, engine_id, source) != scan_boundary:
            raise SuiteError("automatic target scan changed sibling facts or ancestor scan state")

    def execute(suffix: str, locator: str, target: str, input_field: str, output_field: str) -> tuple[str, str, str]:
        _, execution = SUPPORT.create_and_run_task(client, native_task(name + suffix, locator, parent, target, input_field, output_field), time.monotonic() + timeout, owned_ids)
        if execution.get("records_read") != 5 or execution.get("records_written") != 5:
            raise SuiteError("native field lineage execution must read/write exactly five rows")
        identifier = execution.get("execution_id")
        if not isinstance(identifier, str) or not identifier:
            raise SuiteError("native execution is missing its execution_id")
        source_hash, target_hash = execution_schema_hashes(execution)
        return identifier, source_hash, target_hash

    first, source_hash, target_hash = execute("_native", source_locator, NATIVE_TARGET, "region", "region_name")
    target = wait_transfer_target(client, engine_id, f"public.{NATIVE_TARGET}", timeout)
    target_id = positive_int(target.get("id"), "native target id")
    for field, expected in (
        ("region_name", {(source_id, "region", target_id, "region_name", "direct", first)}),
        ("amount", {(source_id, "amount", target_id, "amount", "derived", first)}),
        ("generated_label", set()),
    ):
        graph = wait_field_graph(client, target_id, field, expected, timeout)
        validate_graph_snapshots(graph, {source_id: source_hash, target_id: target_hash})
    assert_scan_boundary()
    replacement, source_hash, target_hash = execute("_replace", source_locator, NATIVE_TARGET, "status", "region_name")
    target = wait_transfer_target(client, engine_id, f"public.{NATIVE_TARGET}", timeout)
    if target.get("id") != target_id:
        raise SuiteError("replace must preserve the target DataItem identity")
    graph = wait_field_graph(client, target_id, "region_name", {(source_id, "status", target_id, "region_name", "direct", replacement)}, timeout)
    validate_graph_snapshots(graph, {source_id: source_hash, target_id: target_hash})
    assert_scan_boundary()
    hop, hop_source_hash, downstream_hash = execute("_hop", SUPPORT.build_item_locator(engine_id, target), NATIVE_DOWNSTREAM, "region_name", "area")
    if hop_source_hash != target_hash:
        raise SuiteError("two-hop execution schemas must match at the intermediate table")
    downstream = wait_transfer_target(client, engine_id, f"public.{NATIVE_DOWNSTREAM}", timeout)
    downstream_id = positive_int(downstream.get("id"), "downstream id")
    expected_edges = {
        (source_id, "status", target_id, "region_name", "direct", replacement),
        (target_id, "region_name", downstream_id, "area", "direct", hop),
    }
    graph = wait_field_graph(client, downstream_id, "area", expected_edges, timeout)
    validate_graph_snapshots(graph, {source_id: source_hash, target_id: target_hash, downstream_id: downstream_hash})
    assert_scan_boundary()
    return {"precise_scan_scope": {"target_only_verified": True, "sibling_item_id": source_id, "ancestor_nodes_verified": len(scan_boundary["ancestors"]), "executions_verified": 3},
            "execution_ids": [first, replacement, hop], "target_locator": SUPPORT.build_item_locator(engine_id, downstream), "target_item_id": downstream_id,
            "field_name": "area", "schema_snapshot_hash": graph["subject"]["schema_snapshot_hash"], "direct_verified": True, "derived_verified": True,
            "generated_verified": True, "replace_verified": True, "two_hop_verified": True, "expected_edges": sorted(expected_edges)}


def mongodb_task(name: str, source_locator: str, parent_locator: str) -> dict[str, object]:
    payload = native_task(name + "_mongodb", source_locator, parent_locator, MONGODB_TARGET, "", "")
    projection = {"_id": 0, **{f"source_{target}": "$" + source for source, target in MONGODB_FIELDS}}
    payload["config"]["source"]["query"] = {"language": "mql", "statement": json.dumps({"aggregate": "activities", "pipeline": [{"$project": projection}]})}
    payload["config"]["transforms"][0]["fields"] = [
        {"source": f"source_{target}", "target": target, "target_type": "string", "nullable": False}
        for _, target in MONGODB_FIELDS
    ]
    return payload


def validate_mongodb_execution(execution: dict[str, object], source_locator: str, target_engine_id: int) -> tuple[str, str]:
    if execution.get("records_read") != 3 or execution.get("records_written") != 3:
        raise SuiteError("MongoDB ODS execution must read/write exactly three rows")
    hashes = execution_schema_hashes(execution)
    facts = execution["metadata"]["lineage_facts"]
    if facts["inputs"][0].get("locator") != source_locator:
        raise SuiteError("MongoDB execution source locator differs from the selected collection")
    target_locator = facts["outputs"][0].get("locator", "")
    parsed = urllib.parse.urlparse(target_locator)
    if parsed.netloc != "engine" or parsed.path != f"/{target_engine_id}/path/public/{MONGODB_TARGET}":
        raise SuiteError("MongoDB execution target locator differs from the PostgreSQL ODS")
    source_fields = [field.get("name") for field in facts["inputs"][0]["schema_snapshot"]["fields"]]
    target_fields = [field.get("name") for field in facts["outputs"][0]["schema_snapshot"]["fields"]]
    if not {source for source, _ in MONGODB_FIELDS}.issubset(source_fields) or target_fields != [target for _, target in MONGODB_FIELDS]:
        raise SuiteError("MongoDB frozen schemas must retain exact nested source names and mapped ODS columns")
    for source_name, _ in MONGODB_FIELDS:
        fields = [field for field in facts["inputs"][0]["schema_snapshot"]["fields"] if field.get("name") == source_name]
        if len(fields) != 1 or fields[0].get("path") != source_name.split("."):
            raise SuiteError("MongoDB frozen source schema must preserve the exact structured nested path")
    mappings = facts["operations"][0].get("field_mappings", [])
    observed = {(value.get("source_field"), value.get("target_field"), value.get("transformation"), value.get("input_port"), value.get("output_port")) for value in mappings}
    expected = {(source, target, "direct", "source", "target") for source, target in MONGODB_FIELDS}
    if observed != expected or len(mappings) != len(expected):
        raise SuiteError("MongoDB execution must prove all six exact direct field mappings")
    return hashes


def run_mongodb_lineage(client: GatewayClient, source_engine_id: int, target_engine_id: int, source: dict[str, object], pg_source: dict[str, object], name: str, timeout: float, owned_ids: list[int]) -> dict[str, object]:
    if source_engine_id == target_engine_id:
        raise SuiteError("MongoDB source and PostgreSQL target must use distinct Engine Instances")
    source_id = positive_int(source.get("id"), "MongoDB source item id")
    source_locator = SUPPORT.build_item_locator(source_engine_id, source)
    parent = f"addp://engine/{target_engine_id}/path/public?type=schema&node_id={positive_int(pg_source.get('node_id'), 'PostgreSQL schema node id')}"
    payload = mongodb_task(name, source_locator, parent)
    created = _object(client.request("POST", "/api/v1/transfer/task-definitions", (201,), payload).payload, "MongoDB Transfer task")
    task_id = positive_int(created.get("id"), "MongoDB Transfer task id")
    owned_ids.append(task_id)
    execution_ids = []
    target_id = None
    for _ in range(2):
        deadline = time.monotonic() + timeout
        started = _object(client.request("POST", f"/api/v1/transfer/task-definitions/{task_id}/start", (200,)).payload, "MongoDB execution")
        identifier = started.get("execution_id")
        if not isinstance(identifier, str) or not identifier or identifier in execution_ids:
            raise SuiteError("MongoDB rerun must produce a distinct execution_id")
        while time.monotonic() < deadline:
            execution = _object(client.request("GET", f"/api/v1/transfer/executions/{urllib.parse.quote(identifier)}", (200,)).payload, "MongoDB execution")
            if execution.get("status") == "success":
                break
            if execution.get("status") in SUPPORT.TERMINAL_STATUSES:
                raise SuiteError("MongoDB ODS execution failed: " + str(execution.get("status")))
            time.sleep(1)
        else:
            raise SuiteError("MongoDB ODS execution did not finish before timeout")
        if execution.get("execution_id") != identifier:
            raise SuiteError("MongoDB execution response identity mismatch")
        source_hash, target_hash = validate_mongodb_execution(execution, source_locator, target_engine_id)
        # Observe owner-triggered metadata scanning. Never manually scan the target or collect lineage.
        target = wait_transfer_target(client, target_engine_id, f"public.{MONGODB_TARGET}", timeout)
        current_id = positive_int(target.get("id"), "MongoDB ODS target id")
        if target_id is not None and current_id != target_id:
            raise SuiteError("MongoDB rerun must preserve the target DataItem identity")
        target_id = current_id
        expected_edges = set()
        for source_field, target_field in MONGODB_FIELDS:
            expected = {(source_id, source_field, target_id, target_field, "direct", identifier)}
            graph = wait_field_graph(client, target_id, target_field, expected, timeout)
            validate_graph_snapshots(graph, {source_id: source_hash, target_id: target_hash})
            for node in graph["nodes"]:
                if node.get("engine_id") != (source_engine_id if node["item_id"] == source_id else target_engine_id):
                    raise SuiteError("MongoDB field graph crosses the wrong Engine Instance")
            expected_edges.update(expected)
        execution_ids.append(identifier)
    return {"execution_ids": execution_ids, "task_id": task_id, "target_locator": SUPPORT.build_item_locator(target_engine_id, target),
            "source_locator": source_locator, "source_item_id": source_id, "target_item_id": target_id, "source_engine_id": source_engine_id, "target_engine_id": target_engine_id,
            "source_schema_snapshot_hash": source_hash, "schema_snapshot_hash": target_hash, "records_read": 3, "records_written": 3,
            "nested_fields_verified": True, "automatic_collection_verified": True, "rerun_verified": True, "expected_edges": sorted(expected_edges)}


DIM_TARGET = "addp_online_transfer_dim_activity"
DWD_TARGET = "addp_online_transfer_dwd_activity"


def query_task(name, engine_id, query, bindings):
    return {"name": name, "dev_type": "query", "timeout": 120,
            "content": {"query_type": "sql", "query": query,
                        "query_parameters": [{"name": key, "type": "relation", "default": {"locator": value}}
                                             for key, value in bindings.items()]},
            "execution_config": {"engine_id": engine_id}}


def wait_owner_execution(client, module, identifier, timeout):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        execution = _object(client.request("GET", f"/api/v1/{module}/executions/{urllib.parse.quote(identifier)}", (200,)).payload, "owner execution")
        if module == "transfer":
            assert_transfer_execution_projection(execution)
        if execution.get("execution_id") != identifier:
            raise SuiteError("owner execution identity changed")
        if execution.get("status") == "success":
            return execution
        if execution.get("status") in SUPPORT.TERMINAL_STATUSES:
            raise SuiteError(f"{module} execution failed: {execution.get('error_details', {}).get('code', execution.get('status'))}")
        time.sleep(1)
    raise SuiteError(f"{module} execution did not finish before timeout")


def validate_orchestrated_child(execution, parent_id, module, task_id):
    expected = {"parent_execution_id": parent_id, "module": module, "source": "orchestrator",
                "source_task_id": str(task_id), "status": "success",
                "task_type": "sync" if module == "transfer" else "query"}
    if any(execution.get(key) != value for key, value in expected.items()):
        raise SuiteError("orchestrated child owner, task or parent identity mismatch")


def validate_orchestrated_parent(parent, execution_id, task_id):
    expected = {"execution_id": execution_id, "module": "orchestrator", "task_type": "orchestration",
                "source_task_id": str(task_id), "status": "success"}
    if any(parent.get(key) != value for key, value in expected.items()):
        raise SuiteError("Orchestrator parent identity mismatch")
    if set(parent) & {"tenant_id", "execution_config", "actor_principal_id", "actor_tenant_membership_id",
                      "issued_authorization_version", "execution_authorization_id", "authorization_expires_at", "lease_token"}:
        raise SuiteError("Orchestrator parent exposed execution internals")
    metadata = _object(parent.get("metadata"), "parent metadata")
    if set(metadata) != {"step_results"}:
        raise SuiteError("Orchestrator must only own the parent context")
    steps = _object(metadata["step_results"], "parent steps")
    if set(steps) != {"ods", "dim", "dwd"}:
        raise SuiteError("chain must complete all three real steps")
    for step in steps.values():
        step = _object(step, "parent step")
        if step.get("status") != "success" or set(step) - {"id", "status", "phase", "started_at", "ended_at", "duration", "error_code", "error", "result"}:
            raise SuiteError("parent step must contain only successful safe orchestration facts")
        result = _object(step.get("result"), "child execution reference")
        if set(result) != {"execution_id"}:
            raise SuiteError("parent step must expose only a child execution reference")
        identifier = result["execution_id"]
        try:
            valid = isinstance(identifier, str) and str(uuid.UUID(identifier)) == identifier and uuid.UUID(identifier).int != 0
        except ValueError:
            valid = False
        if not valid:
            raise SuiteError("child execution reference must be a canonical nonzero UUID")
    return steps


def canonical_table_locator(locator):
    """ReadSet identity is the native table path, independent of catalog selection IDs."""
    parsed = urllib.parse.urlparse(locator)
    segments = parsed.path.split("/")
    query = urllib.parse.parse_qs(parsed.query)
    if (parsed.scheme != "addp" or parsed.netloc != "engine" or len(segments) != 5
            or not segments[1].isdigit() or int(segments[1]) <= 0 or segments[2] != "path"
            or not all(segments[3:]) or query.get("type") != ["table"] or parsed.fragment):
        raise SuiteError("query ReadSet must identify a canonical native table")
    return urllib.parse.urlunparse(parsed._replace(query="type=table"))


def validate_query_facts(execution, bindings, target_locator, rows):
    # Develop's professional DTO deliberately projects safe resources only.
    # Complete mappings and frozen schema identities are verified through Meta.
    metadata = _object(execution.get("metadata"), "query metadata")
    facts = _object(metadata.get("lineage_facts"), "query lineage facts")
    inputs, outputs = (_array(facts.get(key), key) for key in ("inputs", "outputs"))
    if (facts.get("schema_version") != "addp.lineage-facts/v1" or len(inputs) != len(bindings)
            or len(outputs) != 1 or execution.get("rows_affected") != rows
            or execution.get("outputs") != {"execution_id": execution["execution_id"], "target_locator": target_locator, "row_count": rows}):
        raise SuiteError("query must persist exact table write outputs and row count")
    if {ref.get("port"): ref.get("locator") for ref in inputs} != {"input." + key: canonical_table_locator(value) for key, value in bindings.items()}:
        raise SuiteError("query frozen ReadSet differs from the relation bindings")
    output = outputs[0]
    if output.get("port") != "target" or output.get("locator") != target_locator or output.get("write_mode") != "replace":
        raise SuiteError("query must record actual overwrite target")


def merge_graph_snapshots(graph, hashes, allowed_items):
    # Edges have already been checked against exact fields and current execution IDs.
    # Preserve the same frozen identity across every field, source and target.
    for node in _array(graph.get("nodes"), "chain field nodes"):
        item_id, snapshot = node.get("item_id"), node.get("schema_snapshot_hash")
        if item_id not in allowed_items or not isinstance(snapshot, str) or not snapshot:
            raise SuiteError("chain field graph has an unknown or missing frozen schema")
        if item_id in hashes and hashes[item_id] != snapshot:
            raise SuiteError("chain field graph crosses frozen execution schemas")
        hashes[item_id] = snapshot


def cleanup_definitions(client, paths):
    errors = []
    for path in list(reversed(paths)):
        paths.remove(path)  # An unknown mutation outcome must never be replayed.
        try:
            client.request("DELETE", path, (200,))
            client.request("GET", path, (404,))
        except SuiteError as error:
            errors.append(str(error))
    if errors:
        raise SuiteError("chain definition cleanup failed: " + "; ".join(errors))


def wait_resource_chain(client, target_id, expected, timeout):
    query = urllib.parse.urlencode({"subject_kind": "data_item", "item_id": target_id, "direction": "upstream", "depth": 3, "limit": 100})
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        graph = _object(client.request("GET", "/api/v1/meta/lineage/graph?" + query, (200,)).payload, "chain resource graph")
        edges = _array(graph.get("edges"), "resource edges")
        actual = {(edge.get("source", {}).get("item_id"), edge.get("target", {}).get("item_id"), edge.get("evidence", {}).get("execution_id")) for edge in edges}
        if not graph.get("truncated") and actual == expected and len(edges) == len(expected) and all(edge.get("status") == "active" and edge.get("granularity") == "item" for edge in edges):
            return
        time.sleep(1)
    raise SuiteError("resource graph must contain only actual latest Transfer/Develop child evidence")


def evolve_fixture():
    # Only the existing Hosted Business owner may mutate its exclusive physical fixture.
    repository = Path(__file__).resolve().parents[2]
    result = subprocess.run(["bash", "business/scripts/online-transfer-relational-sql-etl-fixture.sh", "evolve"], cwd=repository)
    if result.returncode != 0:
        raise SuiteError("Hosted physical schema evolution failed")


def field_graph_request(client, item_id, field, **parameters):
    query = urllib.parse.urlencode({"subject_kind": "field_ref", "item_id": item_id, "field_name": field,
                                  "direction": "upstream", "depth": 3, "limit": 100, **parameters})
    return _object(client.request("GET", "/api/v1/meta/lineage/graph?" + query, (200,)).payload, "schema evolution field graph")


def verify_unproven_schema(client, item_id, old_hash):
    hashes, graphs = set(), {}
    for field in ("activity_id", "activity_date", "person_display_name", "intensity"):
        graph = field_graph_request(client, item_id, field)
        subject = graph.get("subject", {})
        nodes = graph.get("nodes", [])
        if (graph.get("field_lineage_status") != "unavailable" or graph.get("truncated") or graph.get("edges") != []
                or subject.get("kind") != "field_ref" or subject.get("item_id") != item_id or subject.get("field_name") != field
                or len(nodes) != 1 or nodes[0].get("item_id") != item_id or nodes[0].get("field_name") != field
                or nodes[0].get("schema_snapshot_hash") != subject.get("schema_snapshot_hash")):
            raise SuiteError("changed current structure must be unproven without old field edges")
        hashes.add(subject.get("schema_snapshot_hash"))
        graphs[field] = graph
    if len(hashes) != 1 or not next(iter(hashes)) or old_hash in hashes:
        raise SuiteError("physical rename/type change must produce a new current structure hash")
    query = urllib.parse.urlencode({"subject_kind": "field_ref", "item_id": item_id, "field_name": "person_nickname"})
    client.request("GET", "/api/v1/meta/lineage/graph?" + query, (404,))
    return {"schema_snapshot_hash": next(iter(hashes)), "field_graphs": graphs}


def verify_historical_schema(client, item_id, hashes, as_of, expected_fields, target_status):
    if not isinstance(as_of, str):
        raise SuiteError("historical anchor must use the actual parent completed_at")
    try:
        timestamp = datetime.fromisoformat(as_of.replace("Z", "+00:00"))
    except ValueError as error:
        raise SuiteError("parent completed_at must be RFC3339") from error
    if timestamp.tzinfo is None:
        raise SuiteError("parent completed_at must have a timezone")
    graphs = {}
    for field, expected in expected_fields.items():
        graph = field_graph_request(client, item_id, field, schema_snapshot_hash=hashes[item_id], as_of=as_of)
        validate_field_graph(graph, item_id, field, expected, historical=True)
        validate_graph_snapshots(graph, hashes)
        try:
            returned_time = datetime.fromisoformat(str(graph.get("as_of")).replace("Z", "+00:00"))
            observed_times = [datetime.fromisoformat(str(edge.get("last_observed_at")).replace("Z", "+00:00")) for edge in graph["edges"]]
        except ValueError as error:
            raise SuiteError("historical graph must preserve valid observation times") from error
        if returned_time != timestamp or any(value.tzinfo is None or value > timestamp for value in observed_times):
            raise SuiteError("historical graph contains evidence after the requested time")
        if any(edge.get("status") != target_status for edge in graph["edges"] if edge["target"]["item_id"] == item_id):
            raise SuiteError("historical target edges have the wrong schema lifecycle status")
        graphs[field] = graph
    return graphs


def validate_query_events(payload, identifier):
    page = _object(payload, "Query process events")
    if set(page) != {"items", "next_cursor", "has_more", "retained_after"} or page["has_more"] is not False:
        raise SuiteError("Query event page must be complete and closed")
    items = _array(page["items"], "Query process events")
    if len(items) != 3:
        raise SuiteError("Query must record one started, progress and completed event")
    try:
        retained = datetime.fromisoformat(str(page["retained_after"]).replace("Z", "+00:00"))
    except ValueError as error:
        raise SuiteError("Query event retention must be RFC3339") from error
    if retained.tzinfo is None:
        raise SuiteError("Query event retention must have a timezone")
    previous_id, previous_time = 0, retained
    for event, kind in zip(items, ("started", "progress", "completed")):
        event = _object(event, "Query process event")
        required = {"id", "execution_id", "attempt", "occurred_at", "kind", "counters"}
        if not required <= set(event) or set(event) - required - {"step_id"}:
            raise SuiteError("Query event contains private or undeclared fields")
        expected = {"progress": 30} if kind == "progress" else {}
        if (type(event["id"]) is not int or event["id"] <= previous_id or event["execution_id"] != identifier
                or type(event["attempt"]) is not int or event["attempt"] != 1 or event["kind"] != kind
                or event.get("step_id", "") != "" or event["counters"] != expected
                or any(type(value) is not int for value in _object(event["counters"], "Query event counters").values())):
            raise SuiteError("Query event identity, sequence or closed counters mismatch")
        try:
            occurred = datetime.fromisoformat(str(event["occurred_at"]).replace("Z", "+00:00"))
        except ValueError as error:
            raise SuiteError("Query event time must be RFC3339") from error
        if occurred.tzinfo is None or occurred < previous_time:
            raise SuiteError("Query event times must be ordered inside the retained window")
        previous_id, previous_time = event["id"], occurred
    if type(page["next_cursor"]) is not int or page["next_cursor"] != previous_id:
        raise SuiteError("Query event cursor must identify the last verified event")


def run_orchestrated_lineage(client, engine_id, tenant_id, mongodb, name, timeout, owned_paths):
    # Existing-table writes use the sole TableResultProvider route; no DDL or inferred DAG edges.
    ods_locator = mongodb["target_locator"]
    dim = find_item(client, engine_id, f"public.{DIM_TARGET}", "table")
    dwd = find_item(client, engine_id, f"public.{DWD_TARGET}", "table")
    dim_id, dwd_id = (positive_int(item.get("id"), "chain target id") for item in (dim, dwd))
    dim_locator, dwd_locator = (SUPPORT.build_item_locator(engine_id, item) for item in (dim, dwd))
    dim_bindings, dwd_bindings = {"ods": ods_locator}, {"ods": ods_locator, "dim": dim_locator}

    def create(module, collection, payload, status):
        path = f"/api/v1/{module}/{collection}"
        item = _object(client.request("POST", path, (status,), payload).payload, "chain definition")
        identifier = positive_int(item.get("id"), "chain definition id")
        owned_paths.append(f"{path}/{identifier}")
        if item.get("tenant_id") != tenant_id:
            raise SuiteError("chain definition tenant mismatch")
        return identifier

    dim_task = create("develop", "task-definitions", query_task(name + "_dim", engine_id,
                      "SELECT activity_id, activity_date_raw::date AS activity_date FROM ods", dim_bindings), 200)
    dwd_task = create("develop", "task-definitions", query_task(name + "_dwd", engine_id,
                      "WITH enriched AS (SELECT o.activity_id, d.activity_date, o.leader_nickname_snapshot AS person_nickname, "
                      "upper(o.activity_level_raw) AS intensity FROM ods AS o JOIN dim AS d ON o.activity_id=d.activity_id "
                      "WHERE o.activity_status='active') SELECT activity_id,activity_date,person_nickname,intensity FROM enriched", dwd_bindings), 200)
    def step(key, module, task_type, task_id, dependencies, parameters):
        return {"id": key, "name": key, "provider": module, "task_type": task_type, "task_id": task_id,
                "depends_on": dependencies, "parameters": parameters, "timeout": 120}
    root = create("orchestrator", "orchestrations", {"name": name + "_chain", "enabled": False, "steps": [
        step("ods", "transfer", "sync", mongodb["task_id"], [], {}),
        step("dim", "develop", "query", dim_task, ["ods"], {"target_locator": dim_locator, "write_mode": "overwrite"}),
        step("dwd", "develop", "query", dwd_task, ["dim"], {"target_locator": dwd_locator, "write_mode": "overwrite"}),
    ]}, 201)
    rounds, seen, stable_hashes = [], set(), None
    dim_mappings = {("input.ods", "activity_id", "activity_id", "direct"), ("input.ods", "activity_date_raw", "activity_date", "derived")}
    dwd_mappings = {("input.ods", "activity_id", "activity_id", "direct"), ("input.dim", "activity_date", "activity_date", "direct"),
                    ("input.ods", "leader_nickname_snapshot", "person_nickname", "direct"), ("input.ods", "activity_level_raw", "intensity", "derived")}
    for round_index in range(3):
        if round_index == 2:
            old_hashes, old_fields = dict(stable_hashes), dict(expected_fields)
            historical_as_of = rounds[-1]["completed_at"]
            evolve_fixture()
            evolution_scan = wait_for_scan(client, engine_id, time.monotonic() + timeout)
            unproven_schema = verify_unproven_schema(client, dwd_id, old_hashes[dwd_id])
            evolved_hash = unproven_schema["schema_snapshot_hash"]
            before_write = verify_historical_schema(client, dwd_id, old_hashes, historical_as_of, old_fields, "stale")
            dwd_mappings = {(port, source, "person_display_name" if target == "person_nickname" else target,
                             "derived" if port == "input.dim" else transformation)
                            for port, source, target, transformation in dwd_mappings}
        nickname_field = "person_display_name" if round_index == 2 else "person_nickname"
        date_transformation = "derived" if round_index == 2 else "direct"
        started = _object(client.request("POST", f"/api/v1/orchestrator/orchestrations/{root}/execute", (202,)).payload, "chain admission")
        parent_id = started.get("execution_id")
        if not isinstance(parent_id, str) or not parent_id or parent_id in seen or started.get("status") != "pending":
            raise SuiteError("chain rerun must admit a distinct pending execution")
        parent = wait_owner_execution(client, "orchestrator", parent_id, timeout)
        steps = validate_orchestrated_parent(parent, parent_id, root)
        ids, executions = {}, {}
        for key, module, task_id in (("ods", "transfer", mongodb["task_id"]), ("dim", "develop", dim_task), ("dwd", "develop", dwd_task)):
            identifier = steps[key].get("result", {}).get("execution_id")
            if not isinstance(identifier, str) or not identifier or identifier in seen or identifier == parent_id or identifier in ids.values():
                raise SuiteError("chain must have distinct real child executions on every rerun")
            execution = wait_owner_execution(client, module, identifier, timeout)
            if module == "transfer" and execution.get("task_id") != task_id:
                raise SuiteError("Transfer professional DTO has the wrong task identity")
            observed = _object(client.request("GET", f"/api/v1/monitor/executions/by-execution-id/{urllib.parse.quote(identifier)}", (200,)).payload, "child execution observation")
            if observed.get("execution_id") != identifier:
                raise SuiteError("Monitor observation has the wrong execution identity")
            validate_orchestrated_child(observed, parent_id, module, task_id)
            if module == "develop":
                event_page = client.request("GET", f"/api/v1/monitor/executions/by-execution-id/{urllib.parse.quote(identifier)}/events", (200,)).payload
                validate_query_events(event_page, identifier)
            ids[key], executions[key] = identifier, execution
        seen.update([parent_id, *ids.values()])
        source_locator = mongodb["source_locator"]
        source_hash, ods_hash = validate_mongodb_execution(executions["ods"], source_locator, engine_id)
        validate_query_facts(executions["dim"], dim_bindings, dim_locator, 3)
        validate_query_facts(executions["dwd"], dwd_bindings, dwd_locator, 2)
        if source_hash != mongodb["source_schema_snapshot_hash"]:
            raise SuiteError("chain changed the MongoDB source frozen schema")
        if find_item(client, engine_id, f"public.{MONGODB_TARGET}", "table").get("id") != mongodb["target_item_id"] or find_item(client, engine_id, f"public.{DIM_TARGET}", "table").get("id") != dim_id or find_item(client, engine_id, f"public.{DWD_TARGET}", "table").get("id") != dwd_id:
            raise SuiteError("chain rerun changed a target DataItem identity")
        source_id, ods_id = mongodb["source_item_id"], mongodb["target_item_id"]
        hashes = {source_id: source_hash, ods_id: ods_hash}
        allowed_items = {source_id, ods_id, dim_id, dwd_id}
        ods_edges = {(source_id, source, ods_id, target, "direct", ids["ods"]) for source, target in MONGODB_FIELDS}
        expected_fields = {}
        for raw, target in MONGODB_FIELDS:
            graph = wait_field_graph(client, ods_id, target, {(source_id, raw, ods_id, target, "direct", ids["ods"])}, timeout)
            validate_graph_snapshots(graph, hashes)
        for _, source, target, transformation in sorted(dim_mappings):
            raw = next(raw for raw, ods_field in MONGODB_FIELDS if ods_field == source)
            expected = {(source_id, raw, ods_id, source, "direct", ids["ods"]),
                        (ods_id, source, dim_id, target, transformation, ids["dim"])}
            graph = wait_field_graph(client, dim_id, target, expected, timeout)
            merge_graph_snapshots(graph, hashes, allowed_items)
        for field, ods_field, raw, transformation in (
            ("activity_id", "activity_id", "_id", "direct"),
            (nickname_field, "leader_nickname_snapshot", "leader.userInfo.nickName", "direct"),
            ("intensity", "activity_level_raw", "title.level", "derived"),
        ):
            expected_fields[field] = {(source_id, raw, ods_id, ods_field, "direct", ids["ods"]), (ods_id, ods_field, dwd_id, field, transformation, ids["dwd"])}
        expected_fields["activity_date"] = {(source_id, "title.date", ods_id, "activity_date_raw", "direct", ids["ods"]),
            (ods_id, "activity_date_raw", dim_id, "activity_date", "derived", ids["dim"]), (dim_id, "activity_date", dwd_id, "activity_date", date_transformation, ids["dwd"])}
        for field, expected in expected_fields.items():
            graph = wait_field_graph(client, dwd_id, field, expected, timeout)
            merge_graph_snapshots(graph, hashes, allowed_items)
            validate_graph_snapshots(graph, hashes)
            for node in graph["nodes"]:
                if node.get("engine_id") != (mongodb["source_engine_id"] if node["item_id"] == source_id else engine_id):
                    raise SuiteError("chain field graph crossed the wrong Engine Instance")
        if set(hashes) != allowed_items:
            raise SuiteError("chain graph lost a frozen table structure")
        if round_index == 2:
            expected_hashes = dict(old_hashes)
            expected_hashes[dwd_id] = evolved_hash
            if hashes != expected_hashes:
                raise SuiteError("schema evolution changed unrelated hashes or failed to freeze the new DWD structure")
        elif stable_hashes is not None and hashes != stable_hashes:
            raise SuiteError("chain rerun changed a frozen table structure")
        stable_hashes = dict(hashes)
        all_edges = ods_edges | {(ods_id, source, dim_id, target, transformation, ids["dim"]) for _, source, target, transformation in dim_mappings} | {(dim_id if port == "input.dim" else ods_id, source, dwd_id, target, transformation, ids["dwd"]) for port, source, target, transformation in dwd_mappings}
        wait_resource_chain(client, dwd_id, {(source_id, ods_id, ids["ods"]), (ods_id, dim_id, ids["dim"]), (ods_id, dwd_id, ids["dwd"]), (dim_id, dwd_id, ids["dwd"])}, timeout)
        rounds.append({"parent_execution_id": parent_id, "completed_at": parent.get("completed_at"), "child_execution_ids": ids, "dim_rows": 3, "dwd_rows": 2, "schema_snapshot_hash": hashes[dwd_id], "query_events_verified": 2})
        # Manager verifies the latest active ODS edges after the enclosing DAG rerun as well.
        mongodb.update(expected_edges=sorted(ods_edges), latest_execution_id=ids["ods"], schema_snapshot_hash=ods_hash)
    after_write = verify_historical_schema(client, dwd_id, old_hashes, historical_as_of, old_fields, "closed")
    old_field_query = urllib.parse.urlencode({"subject_kind": "field_ref", "item_id": dwd_id, "field_name": "person_nickname"})
    client.request("GET", "/api/v1/meta/lineage/graph?" + old_field_query, (404,))
    return {"orchestration_id": root, "rounds": rounds, "target_item_id": dwd_id, "target_locator": dwd_locator,
            "schema_snapshot_hash": hashes[dwd_id], "snapshot_hashes": hashes,
            "expected_edges": sorted(all_edges), "date_edges": sorted(expected_fields["activity_date"]), "nickname_edges": sorted(expected_fields[nickname_field]),
            "owner_context_verified": True, "three_hop_verified": True, "rerun_verified": True, "resource_owner_evidence_verified": True,
            "schema_evolution": {"old_hash": old_hashes[dwd_id], "new_hash": evolved_hash, "as_of": historical_as_of,
                                 "scan_execution_id": evolution_scan, "unproven_current_verified": True, "unproven_current_graphs": unproven_schema["field_graphs"],
                                 "history_before_write": before_write, "history_after_write": after_write,
                                 "old_field_name": "person_nickname", "new_field_name": nickname_field,
                                 "type_change": "date -> timestamp without time zone", "history_verified": True}}


def validate_user_identity(client: GatewayClient, tenant_id: int) -> dict[str, object]:
    context = _object(client.request("GET", "/api/v1/system/auth/context", (200,)).payload, "AuthContext")
    principal = _object(context.get("principal"), "AuthContext principal")
    tenant = _object(context.get("context"), "AuthContext context")
    token = _object(context.get("token"), "AuthContext token")
    authorization = _object(context.get("authorization"), "AuthContext authorization")
    if principal.get("type") != "user":
        raise SuiteError("Online Transfer SQL ETL token must belong to a User")
    principal_id = positive_int(principal.get("id"), "AuthContext principal.id")
    if tenant.get("type") != "tenant" or tenant.get("tenant_id") != str(tenant_id):
        raise SuiteError("Online Transfer SQL ETL token must use the configured Tenant Context")
    if token.get("type") not in {"first_party_access_token", "oauth_access_token"}:
        raise SuiteError("Online Transfer SQL ETL token must be a User Access Token")
    assignments = _array(authorization.get("role_assignments"), "AuthContext role_assignments")
    roles: set[str] = set()
    permissions: set[str] = set()
    for raw in assignments:
        assignment = _object(raw, "AuthContext role assignment")
        role = assignment.get("role_key")
        granted = assignment.get("permissions")
        if not isinstance(role, str) or not isinstance(granted, list) or not all(isinstance(value, str) for value in granted):
            raise SuiteError("AuthContext role assignment is incomplete")
        roles.add(role)
        permissions.update(granted)
    forbidden = roles & FORBIDDEN_ADMIN_ROLES
    if forbidden:
        raise SuiteError("Online Transfer SQL ETL token must not use administrator roles: " + ", ".join(sorted(forbidden)))
    missing = REQUIRED_PERMISSIONS - permissions
    if missing:
        raise SuiteError("Online Transfer SQL ETL token is missing required permissions: " + ", ".join(sorted(missing)))
    unexpected = permissions - REQUIRED_PERMISSIONS
    if unexpected:
        raise SuiteError("Online Transfer SQL ETL consumer exceeds minimum permissions: " + ", ".join(sorted(unexpected)))
    return {
        "principal_id": str(principal_id),
        "principal_type": "user",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(REQUIRED_PERMISSIONS),
    }


def wait_manager_read_boundary(client: GatewayClient, engine_id: int, deadline: float) -> None:
    # Only adapt this suite's error type; the shared helper owns readiness.
    try:
        API.wait_manager_read_boundary(client, engine_id, deadline)
    except API.SuiteError as error:
        raise SuiteError(str(error)) from error


def validate_engine(client: GatewayClient, engine_id: int, expected_name: str, engine_type: str, deadline: float) -> dict[str, object]:
    # Engine registration and connection testing belong to the Hosted Provisioner.
    # The consumer verifies the same tenant-visible Meta projection used by the UI.
    while time.monotonic() < deadline:
        engines = _array(client.request("GET", "/api/v1/meta/engines", (200,)).payload, "Meta Engines")
        matches = [engine for engine in engines if isinstance(engine, dict) and engine.get("id") == engine_id]
        if len(matches) > 1:
            raise SuiteError("Meta returned duplicate Engine identities")
        if not matches:
            time.sleep(1)
            continue
        engine = matches[0]
        if engine.get("resource_type") != engine_type:
            raise SuiteError(f"configured Engine Instance {engine_id} must use resource_type={engine_type}")
        if engine.get("name") != expected_name:
            raise SuiteError(f"configured Engine Instance {engine_id} has an unexpected name")
        if engine.get("lifecycle_state") != "active":
            raise SuiteError(f"configured Engine Instance {engine_id} must be active")
        if engine.get("connection_status") == "online":
            return {
                "engine_id": str(engine_id),
                "engine_type": engine_type,
                "engine_name": expected_name,
                "connection_status": "online",
                "verification_owner": "deployment_profile",
            }
        time.sleep(1)
    raise SuiteError(f"configured Engine Instance {engine_id} did not become visible and online")


def suite_task_ids(client: GatewayClient, exact_name: str | None = None) -> list[int]:
    result: list[int] = []
    page = 1
    while True:
        listing = _object(
            client.request("GET", f"/api/v1/transfer/task-definitions?page={page}&page_size=100", (200,)).payload,
            "Transfer task list",
        )
        items = _array(listing.get("items"), "Transfer task list items")
        for raw in items:
            item = _object(raw, "Transfer task list item")
            name = item.get("name")
            if not isinstance(name, str) or not (name.startswith(TASK_PREFIX) or name.startswith("import_" + IMPORT_TARGET + "_")):
                continue
            if exact_name is not None and name not in owned_task_names(exact_name) and not name.startswith("import_" + IMPORT_TARGET + "_"):
                continue
            result.append(positive_int(item.get("id"), "Transfer task id"))
        total = int(listing.get("total", 0))
        if page * 100 >= total:
            return result
        page += 1


def validate_browser_report(report: object, run_id: str, tenant_id: str, expected_task_name: str) -> dict[str, object]:
    payload = _object(report, "Transfer relational SQL ETL browser report")
    expected = {
        "schema_version": "addp.transfer-relational-sql-etl-browser/v7",
        "suite": "transfer-relational-sql-etl",
        "run_id": run_id,
        "result": "passed",
        "tenant_id": tenant_id,
        "task_name": expected_task_name,
        "query_language": "sql",
        "language_selector_hidden": True,
        "projected_fields": ["id", "region", "amount"],
        "parameter_count": 2,
        "records_read": 2,
        "records_written": 2,
        "target_row_count": 2,
        "task_deleted": True,
        "config_checks": {
            "statuses": ["supported", "unknown", "unsupported"], "requests": 3,
            "config_changed_report_cleared": True, "definitions_unchanged": True,
            "executions_unchanged": True, "forbidden_requests": 0, "draft_cancelled": True,
            "separate_identity_verified": True,
        },
        "manager_field_graph_verified": True,
        "query_field_lineage_verified": True,
        "manager_mongodb_field_graph_verified": True,
        "manager_orchestrated_field_graph_verified": True,
        "manager_evolved_schema_verified": True,
        "manager_wide_field_graph_verified": True,
        "write_failure_details_verified": 4,
    }
    mismatches = [key for key, value in expected.items() if payload.get(key) != value]
    if mismatches:
        raise SuiteError("Transfer relational SQL ETL browser report contract mismatch: " + ", ".join(mismatches))
    return payload


def run_browser(repository: Path, environment: Mapping[str, str], expected_task_name: str, lineage: dict[str, object], mongodb_lineage: dict[str, object], orchestrated_lineage: dict[str, object], wide_lineage: dict[str, object], write_failures: dict[str, object]) -> dict[str, object]:
    artifact_dir = Path(required_environment("ADDP_ONLINE_ARTIFACT_DIR"))
    report_path = artifact_dir / "transfer-relational-sql-etl-browser.json"
    report_path.unlink(missing_ok=True)
    browser_environment = dict(environment)
    browser_environment.update(
        {
            "ADDP_ONLINE_REPOSITORY": str(repository),
            "ADDP_ONLINE_TRANSFER_SQL_ETL_TASK_NAME": expected_task_name,
            "ADDP_ONLINE_TRANSFER_SQL_ETL_SOURCE_TABLE": SOURCE_TABLE,
            "ADDP_ONLINE_TRANSFER_SQL_ETL_TARGET_TABLE": TARGET_TABLE,
            "ADDP_ONLINE_TRANSFER_FIELD_LINEAGE": json.dumps(lineage),
            "ADDP_ONLINE_TRANSFER_MONGODB_FIELD_LINEAGE": json.dumps(mongodb_lineage),
            "ADDP_ONLINE_ORCHESTRATED_FIELD_LINEAGE": json.dumps(orchestrated_lineage),
            "ADDP_ONLINE_WIDE_FIELD_LINEAGE": json.dumps(wide_lineage),
            "ADDP_ONLINE_TRANSFER_WRITE_FAILURES": json.dumps(write_failures),
        }
    )
    result = subprocess.run(
        [
            "npm", "exec", "--", "playwright", "test",
            "--config=playwright.online.config.js",
            "e2e/online/transfer-relational-sql-etl.spec.js",
            "--grep", "browser executes SQL ETL and verifies native field lineage in Manager$",
        ],
        cwd=repository / "console/frontend",
        env=browser_environment,
        text=True,
        capture_output=True,
    )
    if result.stdout:
        print(result.stdout, end="" if result.stdout.endswith("\n") else "\n")
    if result.stderr:
        print(result.stderr, end="" if result.stderr.endswith("\n") else "\n", file=sys.stderr)
    if result.returncode != 0:
        raise SuiteError(f"Playwright exited with status {result.returncode}")
    if not report_path.is_file():
        raise SuiteError("Playwright did not write transfer-relational-sql-etl-browser.json")
    return validate_browser_report(
        json.loads(report_path.read_text(encoding="utf-8")),
        environment["ADDP_ONLINE_TEST_RUN_ID"],
        environment["ADDP_ONLINE_TEST_TENANT_ID"],
        expected_task_name,
    )


def interrupt_online(signum: int, _frame: object) -> None:
    raise SuiteError(f"Online acceptance interrupted by signal {signum}")


def main() -> int:
    client: GatewayClient | None = None
    owned_name = ""
    owned_ids: list[int] = []
    owned_paths: list[str] = []
    handlers = {value: signal.getsignal(value) for value in (signal.SIGINT, signal.SIGTERM)}
    for value in handlers:
        signal.signal(value, interrupt_online)
    try:
        if os.environ.get("ADDP_ONLINE_TEST") != "1":
            raise SuiteError("ADDP_ONLINE_TEST must be exactly 1")
        tenant_id = positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "ADDP_ONLINE_TEST_TENANT_ID")
        engine_id = positive_int(required_environment("ADDP_ONLINE_TEST_ENGINE_ID"), "ADDP_ONLINE_TEST_ENGINE_ID")
        mongodb_engine_id = positive_int(required_environment("ADDP_ONLINE_TEST_MONGODB_ENGINE_ID"), "ADDP_ONLINE_TEST_MONGODB_ENGINE_ID")
        if mongodb_engine_id == engine_id:
            raise SuiteError("MongoDB source and PostgreSQL target must use distinct Engine Instances")
        timeout = float(os.environ.get("ADDP_ONLINE_REQUEST_TIMEOUT_SECONDS", "30"))
        if timeout <= 0:
            raise SuiteError("ADDP_ONLINE_REQUEST_TIMEOUT_SECONDS must be greater than zero")
        required_environment("CONSOLE_URL")
        required_environment("ADDP_ONLINE_TEST_USER_USERNAME")
        required_environment("ADDP_ONLINE_TEST_USER_PASSWORD")
        required_environment("ADDP_ONLINE_ARTIFACT_DIR")
        outsider = GatewayClient(required_environment("GATEWAY_URL"), required_environment("ADDP_ONLINE_IMPORT_OUTSIDER_ACCESS_TOKEN"), timeout)
        run_id = required_environment("ADDP_ONLINE_TEST_RUN_ID")
        owned_name = task_name(run_id)
        client = GatewayClient(required_environment("GATEWAY_URL"), required_environment("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"), timeout)

        identity_report = validate_user_identity(client, tenant_id)
        outsider_identity = validate_user_identity(outsider, tenant_id)
        deadline = time.monotonic() + float(os.environ.get("ADDP_ONLINE_CONVERGENCE_TIMEOUT_SECONDS", "180"))
        engine = validate_engine(client, engine_id, required_environment("ADDP_ONLINE_TEST_ENGINE_NAME"), "postgresql", deadline)
        mongodb_engine = validate_engine(client, mongodb_engine_id, required_environment("ADDP_ONLINE_TEST_MONGODB_ENGINE_NAME"), "mongodb", deadline)
        stale = suite_task_ids(client)
        if stale:
            raise SuiteError("stale Transfer relational SQL ETL Online tasks exist before the run")
        scan_execution_id = wait_for_scan(client, engine_id, deadline)
        source = find_item(client, engine_id, f"public.{SOURCE_TABLE}", "table")
        convergence_timeout = float(os.environ.get("ADDP_ONLINE_CONVERGENCE_TIMEOUT_SECONDS", "180"))
        if convergence_timeout <= 0:
            raise SuiteError("ADDP_ONLINE_CONVERGENCE_TIMEOUT_SECONDS must be greater than zero")
        repository = Path(os.environ.get("ADDP_ONLINE_REPOSITORY", Path(__file__).parents[2])).resolve()
        wait_manager_read_boundary(client, engine_id, time.monotonic() + convergence_timeout)
        object_upload = run_object_upload(client, repository, identity_report, convergence_timeout)
        manager_import = run_manager_import(client, engine_id, identity_report, convergence_timeout, owned_ids, outsider)
        record_manager_import_receipt(repository, required_environment("ADDP_ONLINE_ARTIFACT_DIR"), run_id, identity_report, engine_id, manager_import)
        trace_visibility = record_scan_trace_receipt(client, outsider, identity_report, outsider_identity, object_upload, manager_import)
        lineage = run_native_lineage(client, engine_id, source, owned_name, convergence_timeout, owned_ids)

        mongodb_scan = wait_for_scan(client, mongodb_engine_id, time.monotonic() + convergence_timeout)
        mongodb_source = find_item(client, mongodb_engine_id, MONGODB_SOURCE, "collection")
        mongodb_lineage = run_mongodb_lineage(client, mongodb_engine_id, engine_id, mongodb_source, source, owned_name, convergence_timeout, owned_ids)

        chain = run_orchestrated_lineage(client, engine_id, tenant_id, mongodb_lineage, owned_name, convergence_timeout, owned_paths)

        wide = run_wide_lineage(client, engine_id, owned_name, convergence_timeout, owned_ids)
        write_failures = run_write_failures(client, engine_id, owned_name, convergence_timeout, owned_ids)
        browser = run_browser(repository, dict(os.environ), owned_name, lineage, mongodb_lineage, chain, wide, write_failures)
        multi = run_multi_source_lineage(client, engine_id, source, lineage, owned_name, convergence_timeout, owned_ids)
        cleanup_definitions(client, owned_paths)
        cleanup_tasks(client, owned_ids)
        owned_ids.clear()
        residual = suite_task_ids(client, exact_name=owned_name)
        if residual:
            raise SuiteError("browser left the owned Transfer relational SQL ETL task behind")
        report = {
            "schema_version": "addp.transfer-relational-sql-etl-online/v14",
            "suite": "transfer-relational-sql-etl",
            "run_id": run_id,
            "result": "passed",
            "scan_trace_visibility": trace_visibility,
            "identity": identity_report,
            "engine": engine,
            "source_scan_execution_id": scan_execution_id,
            "browser": browser,
            "field_lineage": lineage,
            "mongodb_engine": mongodb_engine,
            "mongodb_source_scan_execution_id": mongodb_scan,
            "mongodb_field_lineage": mongodb_lineage,
            "orchestrated_field_lineage": chain,
            "multi_source_field_lineage": multi,
            "wide_field_lineage": wide,
            "manager_import": manager_import,
            "object_upload": object_upload,
            "write_failures": write_failures,
            "created_resources": 16,
            "deleted_resources": 16,
            "residual_resources": 0,
        }
        print(json.dumps(report, sort_keys=True))
        return 0
    except (OSError, ValueError, SuiteError) as error:
        if client is not None and owned_name:
            try:
                cleanup_definitions(client, owned_paths)
                cleanup_tasks(client, list(dict.fromkeys([*owned_ids, *suite_task_ids(client, exact_name=owned_name)])))
            except SuiteError as cleanup_error:
                print(f"Transfer relational SQL ETL Online cleanup failed: {cleanup_error}", file=sys.stderr)
        print(f"Transfer relational SQL ETL Online failed: {error}", file=sys.stderr)
        return 1
    finally:
        for value, handler in handlers.items():
            signal.signal(value, handler)


if __name__ == "__main__":
    raise SystemExit(main())
