"""Verify database node Grants through their integrated consumer and native statement counters."""

import json
import os
import re
import subprocess
import sys
import time
import uuid
from urllib.parse import urlencode
from importlib import import_module
from pathlib import Path

FLOW = import_module("scripts.test.relational-consumer-flow-online")
EXECUTIONS = import_module("scripts.test.security-mysql-owner-protection-online")
FIXTURE = FLOW.PROFILES["tidb"].fixture_script
REG = FLOW.REGISTRATION


def write_json(path, value):
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(value, indent=2))
    temporary.replace(path)


def counts():
    output = subprocess.check_output(["bash", FIXTURE, "counts"], text=True)
    business, probes = map(int, output.split())
    return business, probes


def observe(execute, engine, checks, phase, database, table, expected, statement=None):
    query = statement or f"SELECT id FROM {database}.{table} ORDER BY id"
    locator = f"addp://engine/{engine}/path/{database}/{table}?type=table"
    before, probes_before = counts()
    result = execute(engine, locator, query)
    execution_id = result.get("execution_id")
    previous_ids = {check["execution_id"] for check in checks.values()}
    observation = dict(
        execution_id=execution_id,
        status=result.get("status"),
        query=query,
        locator=locator,
        source_before=before,
    )
    checks[phase] = observation
    after, probes_after = counts()
    delta = after - before
    observation.update(source_after=after, source_selects=delta, identity_probes=probes_after - probes_before)
    if not execution_id or execution_id in previous_ids:
        raise FLOW.SuiteError(f"TiDB {phase}: missing or repeated execution ID")
    summary = (result.get("metadata") or {}).get("result")
    if expected is None:
        details = result.get("error_details") or {}
        diagnostic = details.get("diagnostic") or {}
        observation.update(
            failure_code=details.get("code"), failure_stage=diagnostic.get("stage"),
            failure_reason=diagnostic.get("reason"),
        )
        if (
            result.get("status") != "failed"
            or details.get("code") != "develop.query.source_read_denied"
            or diagnostic.get("stage") != "authorization"
            or diagnostic.get("reason") != "permission_denied"
            or summary
            or result.get("rows_affected")
            or delta != 0
        ):
            raise FLOW.SuiteError(f"TiDB {phase}: expected authorization denial and zero business SELECTs")
    else:
        # The fixture's BIGINT primary key has an exact JSON string representation.
        expected_id = str(expected)
        rows = ((summary or {}).get("summary") or {}).get("preview_rows")
        row_count = len(rows) if isinstance(rows, list) else None
        observation.update(expected_id=expected_id, preview_row_count=row_count)
        if row_count == 1 and isinstance(rows[0], dict) and "id" in rows[0]:
            returned_id = rows[0]["id"]
            observation["returned_id_type"] = type(returned_id).__name__
            if type(returned_id) in (str, int) and re.fullmatch(r"-?[0-9]{1,19}", str(returned_id)):
                observation["returned_id"] = returned_id
        if result.get("status") != "success" or rows != [{"id": expected_id}] or delta != 1:
            raise FLOW.SuiteError(f"TiDB {phase}: expected one native SELECT and row {expected_id!r}")


def revoke(preparer, consumer, entry, *, confirmed=False):
    preparer.request(
        "POST",
        f"/api/v1/system/engines/{entry['engine']}/access_grants/{entry['request_id']}/revoke",
        (200,) if confirmed else (200, 404),
        {"reason": "End isolated database acceptance"},
    )
    consumer.request(
        "POST",
        "/api/v1/system/engine-access/read-observations/current-source",
        (403,),
        {"targets": [entry["target"]]},
    )


def cleanup(preparer, consumer, journal, artifacts, engine_type="tidb"):
    failed, revoked = [], []
    for entry in json.loads(journal.read_text()):
        try:
            revoke(preparer, consumer, entry)
            revoked.append(entry["request_id"])
        except (RuntimeError, OSError, ValueError):
            failed.append(entry)
    write_json(journal, failed)
    write_json(
        artifacts / f"{engine_type}-grant-cleanup.json", dict(revoked=revoked, failed=[e["request_id"] for e in failed])
    )
    if failed:
        raise FLOW.SuiteError("database Grant cleanup incomplete")


def observe_preview(consumer, engine, checks, phase, database, table, expected):
    fixture = FLOW.PROFILES["oceanbase"].fixture_script
    locator = f"addp://engine/{engine}/path/{database}/{table}?type=table"
    position = int(subprocess.check_output(["bash", fixture, "position"], text=True).strip())
    status = 403 if expected is None else 200
    entry = checks[phase] = dict(locator=locator, audit_position=position, expected_status=status)
    if expected is None:
        query = urlencode({"locator": locator, "page": 1, "page_size": 100})
        consumer.request("GET", f"/api/v1/manager/preview?{query}", (403,))
    else:
        columns, rows = FLOW.SUPPORT.preview_rows(consumer, locator)
        if columns != ["id"] or rows not in ([{"id": expected}], [{"id": str(expected)}]):
            raise FLOW.SuiteError(f"OceanBase {phase}: wrong Manager preview")
        entry["returned_id"] = expected
    count = subprocess.check_output(["bash", fixture, "counts"], text=True,
                                    env=dict(os.environ, OB_AUDIT_POSITION=str(position)))
    delta = int(count.strip())
    entry.update(status=status, source_selects=delta)
    if (expected is None and delta != 0) or (expected is not None and delta < 1):
        raise FLOW.SuiteError(f"OceanBase {phase}: unexpected source business SELECT count {delta}")


def verify(preparer, consumer, tenant, engine, journal, artifacts, engine_type="tidb"):
    manager_preview = engine_type == "oceanbase"
    database = "sales" if manager_preview else "tidb_scope"
    outside = "outside_sales" if manager_preview else "tidb_outside"
    fixture = FLOW.PROFILES[engine_type].fixture_script
    operator, recipient = REG.source_grant_participants(preparer, consumer, tenant, scan_preparation=True)
    FLOW.validate_user_identity(consumer, tenant)
    pending = []

    def remember(engine_id, request_id, target):
        pending.append(dict(engine=engine_id, request_id=request_id, target=target))
        write_json(journal, pending)

    initial_scan = FLOW.wait_for_scan(preparer, engine, time.monotonic() + 180)
    REG.initialize_exact_record_read_grants(
        preparer,
        consumer,
        tenant,
        [(engine, "database", f"{engine_type}_fixture", name) for name in (FLOW.SOURCE_TABLE, FLOW.TARGET_TABLE)],
        scan_preparation=True,
        on_pending=remember,
    )
    target = REG.ExactRecordGrants.target(engine, database, "node_existing", "table")
    scope = dict(target, segments=target["segments"][:-1])
    command = str(uuid.uuid4())
    checks = {}
    report = dict(
        tenant_id=tenant,
        engine_id=engine,
        principal_id=recipient,
        grant=command,
        scope=scope,
        initial_scan_id=initial_scan,
        source_account="ob_consumer@test" if manager_preview else "tidb_consumer",
        checks=checks,
    )
    body = dict(
        request_id=command,
        catalog_path=scope,
        node_recursive=False,
        action="read",
        recipient_type="user",
        recipient_id=recipient,
        expiry_mode="until_revoked",
        reason=f"Isolated {engine_type} database Grant",
    )
    consumer.request(
        "POST", "/api/v1/system/engine-access/read-observations/current-source", (403,), {"targets": [target]}
    )
    if manager_preview:
        report["consumer"] = "manager.preview"
        report["develop_per_table_authorization"] = "not_verified"
        try:
            observe_preview(consumer, engine, checks, "before_grant", database, "node_existing", None)
        finally:
            write_json(artifacts / f"{engine_type}-node-read.json", report)
    remember(engine, command, target)
    receipt = preparer.request("POST", f"/api/v1/system/engines/{engine}/access_grants", (201,), body).payload
    if (
        any(
            receipt.get(k) != body[k]
            for k in ("request_id", "catalog_path", "action", "recipient_type", "recipient_id", "expiry_mode")
        )
        or receipt.get("node_recursive") is not False
        or receipt.get("item_fingerprint")
        or receipt.get("engine_id") != str(engine)
        or receipt.get("operator_principal_id") != operator
    ):
        raise FLOW.SuiteError("database node grant receipt does not match command")

    def execute(engine_id, locator, query):
        return EXECUTIONS.develop_execution(
            consumer, engine_id, locator, time.monotonic() + 120, query, "sql"
        )

    def check(phase, namespace, table, expected):
        if manager_preview:
            observe_preview(consumer, engine, checks, phase, namespace, table, expected)
        else:
            observe(execute, engine, checks, phase, namespace, table, expected)

    try:
        check("existing", database, "node_existing", 37)
        subprocess.run(["bash", fixture, "create-future"], check=True)
        report["future_scan_id"] = FLOW.wait_for_scan(preparer, engine, time.monotonic() + 180)
        check("future", database, "node_future", 41)
        check("same_name_outside", outside, "node_future", None)
        if not manager_preview:
            observe(execute, engine, checks, "join_outside", database, "node_future", None,
                    "SELECT a.id FROM tidb_scope.node_future a JOIN tidb_outside.node_future b ON a.id=b.id")
        revoke(preparer, consumer, pending[-1], confirmed=True)
        check("revoked", database, "node_future", None)
    finally:
        write_json(artifacts / f"{engine_type}-node-read.json", report)


def main(action):
    engine_type = {"oceanbase-consumer-flow": "oceanbase", "tidb-consumer-flow": "tidb"}[os.environ["ONLINE_SUITE_INPUT"]]
    url = os.environ["GATEWAY_URL"]
    consumer = FLOW.GatewayClient(url, os.environ["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"], 30)
    preparer = FLOW.GatewayClient(url, os.environ["ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"], 30)
    journal = Path(os.environ["ADDP_ONLINE_SECRET_DIR"], f"{engine_type}-grants.json")
    artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
    if action == "cleanup":
        cleanup(preparer, consumer, journal, artifacts, engine_type)
    elif action == "verify":
        verify(
            preparer,
            consumer,
            int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"]),
            int(os.environ["ADDP_ONLINE_CONSUMER_ENGINE_ID"]),
            journal,
            artifacts, engine_type,
        )
    else:
        raise FLOW.SuiteError("expected verify or cleanup")


if __name__ == "__main__":
    main(sys.argv[1])
