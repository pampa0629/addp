#!/usr/bin/env python3
"""Accept the browser-created PostgreSQL relational SQL ETL flow."""

from __future__ import annotations

import hashlib
import importlib.util
import json
import os
import signal
import subprocess
import sys
import time
import urllib.parse
from pathlib import Path
from typing import Mapping


SUPPORT_PATH = Path(__file__).with_name("security-transfer-protection-online.py")
SUPPORT_SPEC = importlib.util.spec_from_file_location("transfer_sql_etl_support", SUPPORT_PATH)
SUPPORT = importlib.util.module_from_spec(SUPPORT_SPEC)
assert SUPPORT_SPEC.loader is not None
sys.modules[SUPPORT_SPEC.name] = SUPPORT
SUPPORT_SPEC.loader.exec_module(SUPPORT)

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
NATIVE_TARGET = "addp_online_transfer_field_lineage_target"
NATIVE_DOWNSTREAM = "addp_online_transfer_field_lineage_downstream"
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
    return {name, name + "_native", name + "_replace", name + "_hop", name + "_mongodb"}


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


def validate_field_graph(payload: object, item_id: int, field: str, expected: set[tuple[int, str, int, str, str, str]]) -> dict[str, object]:
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
        if edge.get("granularity") != "field" or edge.get("status") != "active":
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


def execution_schema_hashes(execution: dict[str, object]) -> tuple[str, str]:
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


def run_native_lineage(client: GatewayClient, engine_id: int, source: dict[str, object], name: str, timeout: float, owned_ids: list[int]) -> dict[str, object]:
    source_id = positive_int(source.get("id"), "source item id")
    parent = f"addp://engine/{engine_id}/path/public?type=schema&node_id={positive_int(source.get('node_id'), 'source schema node id')}"
    source_locator = SUPPORT.build_item_locator(engine_id, source)

    def execute(suffix: str, locator: str, target: str, input_field: str, output_field: str) -> tuple[str, str, str]:
        _, execution = SUPPORT.create_and_run_task(client, native_task(name + suffix, locator, parent, target, input_field, output_field), time.monotonic() + timeout, owned_ids)
        if execution.get("records_read") != 5 or execution.get("records_written") != 5:
            raise SuiteError("native field lineage execution must read/write exactly five rows")
        wait_for_scan(client, engine_id, time.monotonic() + timeout)
        identifier = execution.get("execution_id")
        if not isinstance(identifier, str) or not identifier:
            raise SuiteError("native execution is missing its execution_id")
        source_hash, target_hash = execution_schema_hashes(execution)
        return identifier, source_hash, target_hash

    first, source_hash, target_hash = execute("_native", source_locator, NATIVE_TARGET, "region", "region_name")
    target = find_item(client, engine_id, f"public.{NATIVE_TARGET}", "table")
    target_id = positive_int(target.get("id"), "native target id")
    for field, expected in (
        ("region_name", {(source_id, "region", target_id, "region_name", "direct", first)}),
        ("amount", {(source_id, "amount", target_id, "amount", "derived", first)}),
        ("generated_label", set()),
    ):
        graph = wait_field_graph(client, target_id, field, expected, timeout)
        validate_graph_snapshots(graph, {source_id: source_hash, target_id: target_hash})
    replacement, source_hash, target_hash = execute("_replace", source_locator, NATIVE_TARGET, "status", "region_name")
    target = find_item(client, engine_id, f"public.{NATIVE_TARGET}", "table")
    if target.get("id") != target_id:
        raise SuiteError("replace must preserve the target DataItem identity")
    graph = wait_field_graph(client, target_id, "region_name", {(source_id, "status", target_id, "region_name", "direct", replacement)}, timeout)
    validate_graph_snapshots(graph, {source_id: source_hash, target_id: target_hash})
    hop, hop_source_hash, downstream_hash = execute("_hop", SUPPORT.build_item_locator(engine_id, target), NATIVE_DOWNSTREAM, "region_name", "area")
    if hop_source_hash != target_hash:
        raise SuiteError("two-hop execution schemas must match at the intermediate table")
    downstream = find_item(client, engine_id, f"public.{NATIVE_DOWNSTREAM}", "table")
    downstream_id = positive_int(downstream.get("id"), "downstream id")
    expected_edges = {
        (source_id, "status", target_id, "region_name", "direct", replacement),
        (target_id, "region_name", downstream_id, "area", "direct", hop),
    }
    graph = wait_field_graph(client, downstream_id, "area", expected_edges, timeout)
    validate_graph_snapshots(graph, {source_id: source_hash, target_id: target_hash, downstream_id: downstream_hash})
    return {"execution_ids": [first, replacement, hop], "target_locator": SUPPORT.build_item_locator(engine_id, downstream), "target_item_id": downstream_id,
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
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            try:
                target = find_item(client, target_engine_id, f"public.{MONGODB_TARGET}", "table")
                break
            except SuiteError:
                time.sleep(1)
        else:
            raise SuiteError("MongoDB ODS target did not appear through automatic metadata scanning")
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
    for _ in range(2):
        started = _object(client.request("POST", f"/api/v1/orchestrator/orchestrations/{root}/execute", (202,)).payload, "chain admission")
        parent_id = started.get("execution_id")
        if not isinstance(parent_id, str) or not parent_id or parent_id in seen or started.get("status") != "pending":
            raise SuiteError("chain rerun must admit a distinct pending execution")
        parent = wait_owner_execution(client, "orchestrator", parent_id, timeout)
        if parent.get("source_task_id") != str(root) or parent.get("tenant_id") != tenant_id or parent.get("metadata", {}).get("lineage_facts"):
            raise SuiteError("Orchestrator must only own the parent context")
        steps = parent.get("metadata", {}).get("step_results", {})
        if set(steps) != {"ods", "dim", "dwd"} or any(value.get("status") != "success" for value in steps.values()):
            raise SuiteError("chain must complete all three real steps")
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
            ("person_nickname", "leader_nickname_snapshot", "leader.userInfo.nickName", "direct"),
            ("intensity", "activity_level_raw", "title.level", "derived"),
        ):
            expected_fields[field] = {(source_id, raw, ods_id, ods_field, "direct", ids["ods"]), (ods_id, ods_field, dwd_id, field, transformation, ids["dwd"])}
        expected_fields["activity_date"] = {(source_id, "title.date", ods_id, "activity_date_raw", "direct", ids["ods"]),
            (ods_id, "activity_date_raw", dim_id, "activity_date", "derived", ids["dim"]), (dim_id, "activity_date", dwd_id, "activity_date", "direct", ids["dwd"])}
        for field, expected in expected_fields.items():
            graph = wait_field_graph(client, dwd_id, field, expected, timeout)
            merge_graph_snapshots(graph, hashes, allowed_items)
            validate_graph_snapshots(graph, hashes)
            for node in graph["nodes"]:
                if node.get("engine_id") != (mongodb["source_engine_id"] if node["item_id"] == source_id else engine_id):
                    raise SuiteError("chain field graph crossed the wrong Engine Instance")
        if set(hashes) != allowed_items or (stable_hashes is not None and hashes != stable_hashes):
            raise SuiteError("chain rerun changed a frozen table structure")
        stable_hashes = dict(hashes)
        all_edges = ods_edges | {(ods_id, source, dim_id, target, transformation, ids["dim"]) for _, source, target, transformation in dim_mappings} | {(dim_id if port == "input.dim" else ods_id, source, dwd_id, target, transformation, ids["dwd"]) for port, source, target, transformation in dwd_mappings}
        wait_resource_chain(client, dwd_id, {(source_id, ods_id, ids["ods"]), (ods_id, dim_id, ids["dim"]), (ods_id, dwd_id, ids["dwd"]), (dim_id, dwd_id, ids["dwd"])}, timeout)
        rounds.append({"parent_execution_id": parent_id, "child_execution_ids": ids, "dim_rows": 3, "dwd_rows": 2})
        # Manager verifies the latest active ODS edges after the enclosing DAG rerun as well.
        mongodb.update(expected_edges=sorted(ods_edges), latest_execution_id=ids["ods"], schema_snapshot_hash=ods_hash)
    return {"orchestration_id": root, "rounds": rounds, "target_item_id": dwd_id, "target_locator": dwd_locator,
            "schema_snapshot_hash": hashes[dwd_id], "snapshot_hashes": hashes,
            "expected_edges": sorted(all_edges), "date_edges": sorted(expected_fields["activity_date"]), "nickname_edges": sorted(expected_fields["person_nickname"]),
            "owner_context_verified": True, "three_hop_verified": True, "rerun_verified": True, "resource_owner_evidence_verified": True}


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
            if not isinstance(name, str) or not name.startswith(TASK_PREFIX):
                continue
            if exact_name is not None and name not in owned_task_names(exact_name):
                continue
            result.append(positive_int(item.get("id"), "Transfer task id"))
        total = int(listing.get("total", 0))
        if page * 100 >= total:
            return result
        page += 1


def validate_browser_report(report: object, run_id: str, tenant_id: str, expected_task_name: str) -> dict[str, object]:
    payload = _object(report, "Transfer relational SQL ETL browser report")
    expected = {
        "schema_version": "addp.transfer-relational-sql-etl-browser/v4",
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
        "manager_field_graph_verified": True,
        "query_field_lineage_verified": True,
        "manager_mongodb_field_graph_verified": True,
        "manager_orchestrated_field_graph_verified": True,
    }
    mismatches = [key for key, value in expected.items() if payload.get(key) != value]
    if mismatches:
        raise SuiteError("Transfer relational SQL ETL browser report contract mismatch: " + ", ".join(mismatches))
    return payload


def run_browser(repository: Path, environment: Mapping[str, str], expected_task_name: str, lineage: dict[str, object], mongodb_lineage: dict[str, object], orchestrated_lineage: dict[str, object]) -> dict[str, object]:
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
        }
    )
    result = subprocess.run(
        [
            "npm",
            "run",
            "test:e2e",
            "--",
            "--config=playwright.online.config.js",
            "e2e/online/transfer-relational-sql-etl.spec.js",
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
        run_id = required_environment("ADDP_ONLINE_TEST_RUN_ID")
        owned_name = task_name(run_id)
        client = GatewayClient(required_environment("GATEWAY_URL"), required_environment("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"), timeout)

        identity_report = validate_user_identity(client, tenant_id)
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
        lineage = run_native_lineage(client, engine_id, source, owned_name, convergence_timeout, owned_ids)

        mongodb_scan = wait_for_scan(client, mongodb_engine_id, time.monotonic() + convergence_timeout)
        mongodb_source = find_item(client, mongodb_engine_id, MONGODB_SOURCE, "collection")
        mongodb_lineage = run_mongodb_lineage(client, mongodb_engine_id, engine_id, mongodb_source, source, owned_name, convergence_timeout, owned_ids)

        chain = run_orchestrated_lineage(client, engine_id, tenant_id, mongodb_lineage, owned_name, convergence_timeout, owned_paths)

        repository = Path(os.environ.get("ADDP_ONLINE_REPOSITORY", Path(__file__).parents[2])).resolve()
        browser = run_browser(repository, dict(os.environ), owned_name, lineage, mongodb_lineage, chain)
        cleanup_definitions(client, owned_paths)
        cleanup_tasks(client, owned_ids)
        owned_ids.clear()
        residual = suite_task_ids(client, exact_name=owned_name)
        if residual:
            raise SuiteError("browser left the owned Transfer relational SQL ETL task behind")
        report = {
            "schema_version": "addp.transfer-relational-sql-etl-online/v4",
            "suite": "transfer-relational-sql-etl",
            "run_id": run_id,
            "result": "passed",
            "identity": identity_report,
            "engine": engine,
            "source_scan_execution_id": scan_execution_id,
            "browser": browser,
            "field_lineage": lineage,
            "mongodb_engine": mongodb_engine,
            "mongodb_source_scan_execution_id": mongodb_scan,
            "mongodb_field_lineage": mongodb_lineage,
            "orchestrated_field_lineage": chain,
            "created_resources": 8,
            "deleted_resources": 8,
            "residual_resources": 0,
        }
        print(json.dumps(report, sort_keys=True))
        return 0
    except (OSError, ValueError, SuiteError) as error:
        if client is not None and owned_name:
            try:
                cleanup_definitions(client, owned_paths)
                cleanup_tasks(client, suite_task_ids(client, exact_name=owned_name))
            except SuiteError as cleanup_error:
                print(f"Transfer relational SQL ETL Online cleanup failed: {cleanup_error}", file=sys.stderr)
        print(f"Transfer relational SQL ETL Online failed: {error}", file=sys.stderr)
        return 1
    finally:
        for value, handler in handlers.items():
            signal.signal(value, handler)


if __name__ == "__main__":
    raise SystemExit(main())
