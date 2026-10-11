"""Prepare Model-owned targets through owner APIs for Hosted Quality T4."""

from __future__ import annotations

import json
import os
import re
import shlex
import signal
import sys
import time
import uuid
from importlib import import_module
from pathlib import Path

API = import_module("scripts.utils.online-api")
QUALITY = import_module("scripts.test.quality-dynamic-binding-online")
PREPARATION_PERMISSIONS = {
    "model.dw_layer.create", "model.logical_model.create", "model.logical_model.read",
    "model.logical_model.update", "model.materialization.execute", "monitor.execution.read",
    "meta.scan_task.execute", "meta.scan_task.read", "meta.catalog.read",
    "system.execution_authorization.create", "develop.data_read.execute", "develop.data_ddl.execute",
}


def validate_identity(client, tenant_id, permissions):
    identity = API.validate_user_identity(client, tenant_id, permissions)
    context = client.request("GET", "/api/v1/system/auth/context", (200,)).payload
    principal = context.get("principal")
    identifier = principal.get("id") if isinstance(principal, dict) else None
    if not isinstance(identifier, str) or not re.fullmatch(r"[1-9][0-9]*", identifier):
        raise API.SuiteError("fixture User principal identity is invalid")
    return dict(identity, principal_id=identifier)


def execution_id(item):
    identity = item.get("execution_id")
    if not isinstance(identity, str) or str(uuid.UUID(identity)) != identity:
        raise API.SuiteError("fixture execution identity is invalid")
    return identity


def wait(client, path, identity, timeout=180):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        item = client.request("GET", path, (200,)).payload
        if item.get("execution_id") != identity:
            raise API.SuiteError("fixture execution identity changed")
        if item.get("status") == "success":
            return
        if item.get("status") not in {"pending", "running"}:
            raise API.SuiteError("fixture execution did not succeed")
        time.sleep(1)
    raise API.SuiteError("fixture execution convergence timed out")


def scan(client, engine_id):
    started = client.request("POST", "/api/v1/meta/scan/run/manual", (201,), {
        "engine_id": engine_id, "catalog_paths": ["public"], "scan_depth": "deep", "force": True,
    }).payload
    identity = execution_id(started)
    wait(client, f"/api/v1/meta/executions/{identity}", identity)
    return identity


def prepare(client, tenant_id, engine_id, report, checkpoint):
    report.update(resources=[], materialization_executions=[], result="failed", uncertain_mutation=False)
    report["initial_scan_execution_id"] = scan(client, engine_id)
    checkpoint()

    def create(path, body):
        report["uncertain_mutation"] = True
        checkpoint()
        item = client.request("POST", path, (201,), body).payload
        identifier = API.require_positive_int(item, "id")
        report["resources"].append({"path": path, "id": identifier})
        report["uncertain_mutation"] = False
        API.assert_tenant(item, tenant_id, "Quality fixture resource")
        checkpoint()
        return item

    create("/api/v1/model/dw-layers", {"layer_code": "quality_online", "layer_name": "Quality Online"})
    fixtures = []
    for name in ("quality_target_a", "quality_target_b"):
        table = create(QUALITY.MODEL, {
            "name": name, "code": name, "table_type": "entity", "layer": "quality_online",
            "materialization": {"target_parent_locator": f"addp://engine/{engine_id}/path/public?type=schema",
                                "target_name": name},
        })
        path = f"{QUALITY.MODEL}/{table['id']}"
        field = client.request("POST", path + "/fields", (201,), {
            "version": API.require_positive_int(table, "version"), "name": "ID", "column_name": "id",
            "data_type": "bigint", "nullable": False, "is_pk": True, "field_role": "regular",
        }).payload
        table = client.request("POST", path + "/approve", (200,), {
            "version": API.require_positive_int(field, "version"),
        }).payload
        API.assert_tenant(table, tenant_id, "approved Quality fixture")
        if table.get("status") != "approved":
            raise API.SuiteError("fixture Model definition was not approved")
        report["uncertain_mutation"] = True
        checkpoint()
        started = client.request("POST", path + "/materialized-target", (202,), {
            "version": API.require_positive_int(table, "version"),
        }).payload
        identity = execution_id(started)
        report["materialization_executions"].append(identity)
        report["uncertain_mutation"] = False
        checkpoint()
        wait(client, f"/api/v1/monitor/executions/by-execution-id/{identity}", identity)
        fixtures.append({"logical_table_id": table["id"],
                         "locator": f"addp://engine/{engine_id}/path/public/{name}?type=table", "row_count": 2})
    QUALITY.validate_fixtures(fixtures)
    report.update(fixtures=fixtures, result="passed")
    checkpoint()
    return fixtures


def main(action):
    if any(os.environ.get(key) != value for key, value in {
        "GITHUB_ACTIONS": "true", "RUNNER_OS": "Linux", "ADDP_ONLINE_HOSTED": "1",
        "ADDP_ONLINE_HOST": "1", "POSTGRES_DB": "addp_online",
    }.items()):
        raise API.SuiteError("fixture preparation requires disposable Hosted Online")
    tenant_id, engine_id = int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"]), int(os.environ["ADDP_ONLINE_CONSUMER_ENGINE_ID"])
    if tenant_id <= 1 or engine_id <= 0:
        raise API.SuiteError("fixture preparation requires explicit Tenant and Engine")
    token = os.environ["ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN"]
    consumer_token = os.environ["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"]
    identity = validate_identity(API.GatewayClient(os.environ["SYSTEM_URL"], token, 30), tenant_id, PREPARATION_PERMISSIONS)
    consumer = validate_identity(API.GatewayClient(os.environ["SYSTEM_URL"], consumer_token, 30), tenant_id,
                                         QUALITY.REQUIRED_PERMISSIONS | {"system.execution_authorization.create",
                                                                          "develop.data_read.execute", "develop.data_ddl.execute"})
    if identity["principal_id"] == consumer["principal_id"]:
        raise API.SuiteError("fixture preparation and execution must use different Users")
    report = {"schema_version": "addp.quality-online-fixture/v1", "tenant_id": str(tenant_id),
              "engine_id": engine_id, "preparation_identity": identity, "execution_identity": consumer}
    evidence = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "quality-fixture.json"
    if not evidence.is_absolute() or evidence.resolve().is_relative_to(Path(__file__).resolve().parents[2]):
        raise API.SuiteError("fixture evidence must be outside the checkout")
    if evidence.exists():
        report = json.loads(evidence.read_text())

    def checkpoint():
        evidence.write_text(json.dumps(report, sort_keys=True) + "\n")

    client = API.GatewayClient(os.environ["GATEWAY_URL"], token, 30)
    if action == "prepare":
        fixtures = prepare(client, tenant_id, engine_id, report, checkpoint)
        destination = Path(os.environ["ADDP_ONLINE_SECRET_DIR"]) / "quality-fixtures.env"
        with open(destination, "x", opener=lambda p, flags: os.open(p, flags, 0o600)) as output:
            output.write("export ADDP_ONLINE_QUALITY_FIXTURES_JSON=" + shlex.quote(json.dumps(fixtures)) + "\n")
    elif action == "scan":
        report["result"] = "failed"
        checkpoint()
        report["seeded_scan_execution_id"] = scan(client, engine_id)
        report["result"] = "passed"
        checkpoint()
    else:
        raise API.SuiteError("expected prepare or scan")


if __name__ == "__main__":
    try:
        if len(sys.argv) != 2:
            raise API.SuiteError("expected one fixture preparation action")
        def interrupted(signum, frame):
            raise API.SuiteError("fixture preparation interrupted")
        for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGALRM):
            signal.signal(signum, interrupted)
        signal.alarm(600)
        main(sys.argv[1])
        print("Quality owner fixture preparation completed")
    except (API.SuiteError, KeyError, ValueError, OSError):
        print("Quality owner fixture preparation failed; inspect evidence IDs", file=sys.stderr)
        raise SystemExit(1)
    finally:
        signal.alarm(0)
