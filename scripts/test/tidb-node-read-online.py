"""Verify one TiDB node Grant through System, Develop and native statement counters."""

import json
import os
import subprocess
import sys
import time
import uuid
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
    elif (
        result.get("status") != "success"
        or (summary or {}).get("summary", {}).get("preview_rows") != [{"id": expected}]
        or delta != 1
    ):
        raise FLOW.SuiteError(f"TiDB {phase}: expected one native SELECT and row {expected}")
    else:
        observation["returned_id"] = expected


def revoke(preparer, consumer, entry, *, confirmed=False):
    preparer.request(
        "POST",
        f"/api/v1/system/engines/{entry['engine']}/access_grants/{entry['request_id']}/revoke",
        (200,) if confirmed else (200, 404),
        {"reason": "End isolated TiDB acceptance"},
    )
    consumer.request(
        "POST",
        "/api/v1/system/engine-access/read-observations/current-source",
        (403,),
        {"targets": [entry["target"]]},
    )


def cleanup(preparer, consumer, journal, artifacts):
    failed, revoked = [], []
    for entry in json.loads(journal.read_text()):
        try:
            revoke(preparer, consumer, entry)
            revoked.append(entry["request_id"])
        except (RuntimeError, OSError, ValueError):
            failed.append(entry)
    write_json(journal, failed)
    write_json(
        artifacts / "tidb-grant-cleanup.json", dict(revoked=revoked, failed=[e["request_id"] for e in failed])
    )
    if failed:
        raise FLOW.SuiteError("TiDB Grant cleanup incomplete")


def verify(preparer, consumer, tenant, engine, journal, artifacts):
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
        [(engine, "database", "tidb_fixture", name) for name in (FLOW.SOURCE_TABLE, FLOW.TARGET_TABLE)],
        scan_preparation=True,
        on_pending=remember,
    )
    target = REG.ExactRecordGrants.target(engine, "tidb_scope", "node_existing", "table")
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
        source_account="tidb_consumer",
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
        reason="Isolated TiDB database Grant",
    )
    consumer.request(
        "POST", "/api/v1/system/engine-access/read-observations/current-source", (403,), {"targets": [target]}
    )
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
        raise FLOW.SuiteError("TiDB node grant receipt does not match command")

    def execute(engine_id, locator, query):
        return EXECUTIONS.develop_execution(
            consumer, engine_id, locator, time.monotonic() + 120, query, "sql"
        )

    try:
        observe(execute, engine, checks, "existing", "tidb_scope", "node_existing", 37)
        subprocess.run(["bash", FIXTURE, "create-future"], check=True)
        report["future_scan_id"] = FLOW.wait_for_scan(preparer, engine, time.monotonic() + 180)
        observe(execute, engine, checks, "future", "tidb_scope", "node_future", 41)
        observe(execute, engine, checks, "same_name_outside", "tidb_outside", "node_future", None)
        observe(
            execute,
            engine,
            checks,
            "join_outside",
            "tidb_scope",
            "node_future",
            None,
            "SELECT a.id FROM tidb_scope.node_future a JOIN tidb_outside.node_future b ON a.id=b.id",
        )
        revoke(preparer, consumer, pending[-1], confirmed=True)
        observe(execute, engine, checks, "revoked", "tidb_scope", "node_future", None)
    finally:
        write_json(artifacts / "tidb-node-read.json", report)


def main(action):
    url = os.environ["GATEWAY_URL"]
    consumer = FLOW.GatewayClient(url, os.environ["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"], 30)
    preparer = FLOW.GatewayClient(url, os.environ["ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"], 30)
    journal = Path(os.environ["ADDP_ONLINE_SECRET_DIR"], "tidb-grants.json")
    artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
    if action == "cleanup":
        cleanup(preparer, consumer, journal, artifacts)
    elif action == "verify":
        verify(
            preparer,
            consumer,
            int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"]),
            int(os.environ["ADDP_ONLINE_CONSUMER_ENGINE_ID"]),
            journal,
            artifacts,
        )
    else:
        raise FLOW.SuiteError("expected verify or cleanup")


if __name__ == "__main__":
    main(sys.argv[1])
