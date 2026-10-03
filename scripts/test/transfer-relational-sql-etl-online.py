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
TASK_PREFIX = "addp_online_sql_etl_"
FORBIDDEN_ADMIN_ROLES = SUPPORT.FORBIDDEN_ADMIN_ROLES
REQUIRED_PERMISSIONS = {
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
    return {name, name + "_native", name + "_replace", name + "_hop"}


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


def validate_engine(client: GatewayClient, engine_id: int, expected_name: str, deadline: float) -> dict[str, object]:
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
        if engine.get("resource_type") != "postgresql":
            raise SuiteError(f"configured Engine Instance {engine_id} must use resource_type=postgresql")
        if engine.get("name") != expected_name:
            raise SuiteError(f"configured Engine Instance {engine_id} has an unexpected name")
        if engine.get("lifecycle_state") != "active":
            raise SuiteError(f"configured Engine Instance {engine_id} must be active")
        if engine.get("connection_status") == "online":
            return {
                "engine_id": str(engine_id),
                "engine_type": "postgresql",
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
        "schema_version": "addp.transfer-relational-sql-etl-browser/v2",
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
        "query_field_unavailable_verified": True,
    }
    mismatches = [key for key, value in expected.items() if payload.get(key) != value]
    if mismatches:
        raise SuiteError("Transfer relational SQL ETL browser report contract mismatch: " + ", ".join(mismatches))
    return payload


def run_browser(repository: Path, environment: Mapping[str, str], expected_task_name: str, lineage: dict[str, object]) -> dict[str, object]:
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
    handlers = {value: signal.getsignal(value) for value in (signal.SIGINT, signal.SIGTERM)}
    for value in handlers:
        signal.signal(value, interrupt_online)
    try:
        if os.environ.get("ADDP_ONLINE_TEST") != "1":
            raise SuiteError("ADDP_ONLINE_TEST must be exactly 1")
        tenant_id = positive_int(required_environment("ADDP_ONLINE_TEST_TENANT_ID"), "ADDP_ONLINE_TEST_TENANT_ID")
        engine_id = positive_int(required_environment("ADDP_ONLINE_TEST_ENGINE_ID"), "ADDP_ONLINE_TEST_ENGINE_ID")
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
        engine = validate_engine(client, engine_id, required_environment("ADDP_ONLINE_TEST_ENGINE_NAME"), deadline)
        stale = suite_task_ids(client)
        if stale:
            raise SuiteError("stale Transfer relational SQL ETL Online tasks exist before the run")
        scan_execution_id = wait_for_scan(client, engine_id, deadline)
        source = find_item(client, engine_id, f"public.{SOURCE_TABLE}", "table")
        convergence_timeout = float(os.environ.get("ADDP_ONLINE_CONVERGENCE_TIMEOUT_SECONDS", "180"))
        if convergence_timeout <= 0:
            raise SuiteError("ADDP_ONLINE_CONVERGENCE_TIMEOUT_SECONDS must be greater than zero")
        lineage = run_native_lineage(client, engine_id, source, owned_name, convergence_timeout, owned_ids)

        repository = Path(os.environ.get("ADDP_ONLINE_REPOSITORY", Path(__file__).parents[2])).resolve()
        browser = run_browser(repository, dict(os.environ), owned_name, lineage)
        cleanup_tasks(client, owned_ids)
        owned_ids.clear()
        residual = suite_task_ids(client, exact_name=owned_name)
        if residual:
            raise SuiteError("browser left the owned Transfer relational SQL ETL task behind")
        report = {
            "schema_version": "addp.transfer-relational-sql-etl-online/v2",
            "suite": "transfer-relational-sql-etl",
            "run_id": run_id,
            "result": "passed",
            "identity": identity_report,
            "engine": engine,
            "source_scan_execution_id": scan_execution_id,
            "browser": browser,
            "field_lineage": lineage,
            "created_resources": 4,
            "deleted_resources": 4,
            "residual_resources": 0,
        }
        print(json.dumps(report, sort_keys=True))
        return 0
    except (OSError, ValueError, SuiteError) as error:
        if client is not None and owned_name:
            try:
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
