"""Pinned Gateway import/export acceptance; physical observers never grant access."""
from __future__ import annotations

import csv
import hashlib
import io
import json
import os
import select
import subprocess
import sys
import time
import zipfile
from contextlib import contextmanager
from importlib import import_module
from pathlib import Path
from urllib.parse import parse_qs, unquote, urlsplit
from uuid import UUID, uuid4

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
api = import_module("scripts.utils.online-api")
etl = import_module("scripts.test.transfer-relational-sql-etl-online")
claims = import_module("scripts.test.meta-scan-claim-online")

PERMISSIONS = {
    "manager.data_item.create", "manager.data_item.read", "manager.content.read",
    "manager.derived_artifact.create", "manager.derived_artifact.read", "meta.catalog.read",
    "meta.scan_task.read", "meta.scan_task.execute", "transfer.task.create", "transfer.task.read",
    "transfer.task.execute", "transfer.task.delete", "develop.task.read", "develop.task.execute",
    "develop.data_read.execute", "system.execution_authorization.create",
    "security.enrollment.create", "security.enrollment.read",
}
READER_PERMISSIONS = {"manager.derived_artifact.read", "develop.task.read", "develop.task.execute",
                      "develop.data_read.execute", "transfer.task.read", "transfer.task.execute",
                      "system.execution_authorization.create"}
TABLE = "object_import_points"


def require(ok, message):
    if not ok:
        raise api.SuiteError(message)


def uuid(value):
    require(isinstance(value, str) and str(UUID(value)) == value, "canonical UUID required")
    return value


def positive(value):
    require(type(value) is int and value > 0, "positive identity required")
    return value


def quote(value):
    return "'" + str(value).replace("'", "''") + "'"


def pg(sql, *, write=False, source=False):
    command = ["docker", "exec", "-i"]
    if not write:
        command += ["-e", "PGOPTIONS=-c default_transaction_read_only=on"]
    command += ["addp-object-online-disposable" if source else "addp-postgres",
                "psql", "-XqAt", "-v", "ON_ERROR_STOP=1", "-U",
                "postgres" if source else os.environ["POSTGRES_USER"], "-d",
                "object_fixture" if source else "addp_online"]
    result = subprocess.run(command, input=sql, text=True, capture_output=True, timeout=10)
    require(result.returncode == 0, "physical database observation failed")
    return json.loads(result.stdout) if result.stdout.strip() else None


def object_bytes(key):
    result = subprocess.run(["docker", "exec", "addp-minio", "mc", "cat", "local/manager/" + key],
                            capture_output=True, timeout=15)
    require(result.returncode == 0, "physical object read failed")
    return result.stdout


def list_keys(prefix):
    result = subprocess.run(["docker", "exec", "addp-minio", "mc", "ls", "--json", "--recursive",
                             "local/manager/" + prefix], capture_output=True, timeout=15)
    require(result.returncode == 0, "physical prefix listing failed")
    rows = [json.loads(line) for line in result.stdout.splitlines() if line.strip()]
    require(all(row.get("status") == "success" for row in rows), "physical listing returned an error")
    return [row["key"] for row in rows if row.get("type") == "file"]


def key_from_ref(locator, kind):
    parsed = urlsplit(locator)
    require(parsed.scheme == "addp-infra" and parsed.netloc == "object"
            and parsed.path.startswith("/manager/") and parse_qs(parsed.query) == {"type": [kind]},
            "unexpected logical object reference")
    key = unquote(parsed.path[len("/manager/"):])
    require(key and all(part not in {"", ".", ".."} for part in key.strip("/").split("/")),
            "invalid object key")
    return key


def identity(client, tenant, permissions):
    proof = api.validate_user_identity(client, tenant, permissions)
    current = client.request("GET", "/api/v1/system/auth/context", (200,)).payload
    proof["principal_id"] = str(positive(int(current["principal"]["id"])))
    return proof


@contextmanager
def hold_target():
    # A bounded fixture-only relation lock exposes actual Worker acquisition;
    # it neither manufactures execution rows nor changes the source bytes.
    process = subprocess.Popen(["docker", "exec", "-i", "addp-object-online-disposable", "psql",
                                "-XqAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "object_fixture"],
                               stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    try:
        process.stdin.write("BEGIN; SET LOCAL idle_in_transaction_session_timeout='60s'; "
                            f"LOCK TABLE public.{TABLE} IN ACCESS EXCLUSIVE MODE; SELECT 'locked';\n")
        process.stdin.flush()
        require(select.select([process.stdout], [], [], 8)[0]
                and process.stdout.readline().strip() == "locked", "source barrier failed")
        yield
    finally:
        process.stdin.close()
        try:
            require(process.wait(timeout=8) == 0, "source barrier release failed")
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)
            raise api.SuiteError("source barrier release timed out") from None
        finally:
            process.stdout.close()
            process.stderr.close()


def observe_claim(execution_id, tenant, task_id=None):
    uuid(execution_id)
    def probe():
        return pg(f"""
          SELECT json_build_object('execution_id',e.execution_id,'source_task_id',e.source_task_id,
            'tenant_id',e.tenant_id,'status',e.status,'attempt',e.attempt,'lease_owner',e.lease_owner,
            'started_at',e.started_at,'lease_valid',e.lease_token IS NOT NULL AND e.lease_expires_at>clock_timestamp(),
            'events',(SELECT coalesce(json_agg(json_build_object('id',v.id,'execution_id',v.execution_id,
              'attempt',v.attempt,'occurred_at',v.occurred_at)), '[]') FROM common.execution_events v
              WHERE v.execution_id=e.execution_id AND v.tenant_id=e.tenant_id AND v.kind='started'),
            'worker',(SELECT json_build_object('instance',h.instance_id,'capacity',h.capacity,
               'started_at',h.started_at) FROM common.background_runtime_heartbeats h
               WHERE h.module='transfer' AND h.role='execution_worker' AND h.runtime_name='sync'
               AND e.lease_owner LIKE h.instance_id||'-%' AND h.stopped_at IS NULL
               AND h.expires_at>clock_timestamp() LIMIT 1))
          FROM common.task_executions e WHERE e.execution_id={quote(execution_id)} AND e.module='transfer'
          AND e.task_type='sync' AND e.status='running'
          AND EXISTS (SELECT 1 FROM common.execution_events started
             WHERE started.execution_id=e.execution_id AND started.tenant_id=e.tenant_id AND started.kind='started')
        """)
    row = claims.wait_for(probe, 35, "real Transfer running lease was not observed")
    worker = row.pop("worker")
    require(worker is not None, "claim does not match a ready real Worker")
    if task_id is None:
        require(row["source_task_id"] in {None, ""}, "export unexpectedly owns a persistent task")
        row_for_check = row | {"source_task_id": "0"}
        claims.assert_claim(row_for_check, execution_id, 0, tenant, {worker["instance"]: worker})
    else:
        claims.assert_claim(row, execution_id, task_id, tenant, {worker["instance"]: worker})
    return row | {"worker": worker}


def wait_execution(client, module, execution_id):
    uuid(execution_id)
    deadline = time.monotonic() + 180
    while time.monotonic() < deadline:
        value = client.request("GET", f"/api/v1/{module}/executions/{execution_id}", (200,)).payload
        if value.get("status") in {"success", "failed", "cancelled"}:
            require(value.get("status") == "success", f"{module} execution failed: {execution_id}")
            return value
        time.sleep(.5)
    raise api.SuiteError("execution did not finish: " + execution_id)


def execution_proof(execution_id, tenant, principal, *, source, task_id=None):
    row = pg(f"""
      SELECT json_build_object('execution_id',e.execution_id,'tenant_id',e.tenant_id,'module',e.module,
        'source',e.source,'source_task_id',e.source_task_id,'triggered_by',e.triggered_by,
        'attempt',e.attempt,'status',e.status,'started_at',e.started_at,'completed_at',e.completed_at,
        'metadata',e.metadata,'events',(SELECT json_agg(json_build_object('id',v.id,'kind',v.kind,
          'attempt',v.attempt,'occurred_at',v.occurred_at)) FROM common.execution_events v
          WHERE v.execution_id=e.execution_id AND v.tenant_id=e.tenant_id))
      FROM common.task_executions e WHERE e.execution_id={quote(uuid(execution_id))}
    """)
    require(row and row["tenant_id"] == tenant and row["module"] == "transfer" and str(row["triggered_by"]) == principal
            and row["source"] == source and row["status"] == "success" and row["attempt"] == 1
            and row["completed_at"] and row["started_at"], "execution identity or terminal facts differ")
    if task_id is not None:
        require(str(row["source_task_id"]) == str(task_id), "execution belongs to a different task")
    events = row["events"] or []
    require(sum(event["kind"] == "started" for event in events) == 1
            and sum(event["kind"] == "completed" for event in events) == 1
            and all(event["attempt"] == 1 for event in events), "execution event timeline differs")
    # Metadata may contain private protection facts. Archive only closed facts.
    row.pop("metadata")
    return row


def import_once(client, engine, tenant, principal, report):
    archive = etl.import_shapefile_zip()
    boundary = "addp-object-t4-" + uuid4().hex
    node = f"addp://engine/{engine}/path/public?type=schema"
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="target_node_locator"\r\n\r\n{node}\r\n'
            f'--{boundary}\r\nContent-Disposition: form-data; name="target_table"\r\n\r\n{TABLE}\r\n'
            f'--{boundary}\r\nContent-Disposition: form-data; name="files"; filename="points.zip"\r\n'
            'Content-Type: application/zip\r\n\r\n').encode() + archive + f"\r\n--{boundary}--\r\n".encode()
    with hold_target():
        submitted = client.request("POST", "/api/v1/manager/imports", (202,), encoded_body=body,
                                   content_type="multipart/form-data; boundary=" + boundary).payload
        report.update(upload_uuid=uuid(submitted["upload_uuid"]), task_id=positive(submitted["transfer_task_id"]),
                      execution_id=uuid(submitted["transfer_execution_id"]))
        report["claim"] = observe_claim(report["execution_id"], tenant, report["task_id"])
    terminal = wait_execution(client, "transfer", report["execution_id"])
    require(terminal.get("records_read") == terminal.get("records_written") == 2, "import row counts differ")
    task = client.request("GET", f'/api/v1/transfer/task-definitions/{report["task_id"]}', (200,)).payload
    require(task.get("last_execution_id") == report["execution_id"] and task.get("last_execution_status") == "success",
            "import task summary does not identify its successful execution")
    runs = pg(f"SELECT json_agg(execution_id) FROM common.task_executions WHERE module='transfer' "
              f"AND tenant_id={tenant} AND source_task_id={quote(str(report['task_id']))}")
    require(runs == [report["execution_id"]], "import submitted more than one positive execution")
    locator = task["config"]["source"]["locator"]
    source = pg(f"SELECT row_to_json(s) FROM manager.import_sources s WHERE upload_uuid={quote(report['upload_uuid'])}")
    require(source and source["tenant_id"] == tenant and str(source["uploader_id"]) == principal
            and source["transfer_task_id"] == report["task_id"] and source["source_locator"] == locator,
            "upload UUID/task/user/tenant/source binding differs")
    key = key_from_ref(locator, "object")
    require(f'/{report["upload_uuid"]}/' in key and key.startswith(f"tenant_{tenant}/import/"),
            "upload UUID not retained in physical key")
    prefix = key.rsplit("/", 1)[0] + "/"
    files = {}
    with zipfile.ZipFile(io.BytesIO(archive)) as contents:
        for name in contents.namelist():
            expected = contents.read(name)
            physical = object_bytes(prefix + name)
            digest = hashlib.sha256(expected).hexdigest()
            require(physical == expected and source["files"].get(name) == digest,
                    "uploaded composition bytes or persisted digest differ")
            files[name] = {"bytes": len(expected), "sha256": digest}
    require(set(files) == set(source["files"]), "unexpected import composition")
    scan = terminal.get("metadata", {}).get("metadata_scan", {})
    require(scan.get("status") == "submitted", "import did not submit its metadata scan")
    child_id = uuid(scan.get("execution_id"))
    child = wait_execution(client, "meta", child_id)
    require(child.get("parent_execution_id") == report["execution_id"] and child.get("source") == "transfer",
            "write-after-scan parent identity differs")
    geometry = pg(f"SELECT json_agg(attname) FROM pg_attribute WHERE attrelid='public.{TABLE}'::regclass "
                  "AND atttypid='geometry'::regtype AND attnum>0 AND NOT attisdropped", source=True)
    require(geometry and len(geometry) == 1, "imported table must retain one geometry column")
    column = '"' + geometry[0].replace('"', '""') + '"'
    coordinates = pg(f"SELECT json_agg(json_build_array(id,label,ST_X({column}),ST_Y({column}),ST_SRID({column})) ORDER BY id) "
                     f"FROM public.{TABLE}", source=True)
    require(coordinates == [[1, "alpha", 10, 20, 4326], [2, "beta", 30, 40, 4326]], "imported physical rows or geometry differ")
    physical = pg(f"SELECT json_agg(row_to_json(t) ORDER BY id) FROM public.{TABLE} t", source=True)
    report.update(source_locator=locator, prefix=prefix, files=files, physical_rows=physical,
                  coordinates=coordinates, summary={"last_execution_id": task["last_execution_id"], "last_execution_status": task["last_execution_status"]},
                  meta_scan_execution_id=child_id,
                  terminal=execution_proof(report["execution_id"], tenant, principal, source="transfer", task_id=report["task_id"]))
    return f"addp://engine/{engine}/path/public/{TABLE}?type=table"


def session_row(owner, session_id):
    require(owner in {"manager", "develop"}, "invalid export owner")
    return pg(f"SELECT row_to_json(s) FROM {owner}.export_sessions s WHERE id={positive(session_id)}")


def csv_proof(data):
    rows = list(csv.DictReader(io.StringIO(data.decode("utf-8-sig"))))
    require(len(rows) == 2 and {(int(row["id"]), row["label"]) for row in rows}
            == {(1, "alpha"), (2, "beta")}, "CSV values differ from the actual imported source")
    return {"bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(), "rows": 2}


def export_once(client, owner, locator, engine, tenant, principal, report):
    if owner == "manager":
        route = "/api/v1/manager/exports"
        body = {"source_item_locator": locator, "format": "csv", "file_name": "object-manager"}
    else:
        created = client.request("POST", "/api/v1/develop/executions", (200,), {
            "dev_type": "query", "trigger_type": "manual",
            "content": {"query": f"SELECT id,label FROM public.{TABLE} ORDER BY id", "query_type": "sql",
                        "target_locator": locator, "query_parameters": []},
            "execution_config": {"engine_id": engine}, "parameters": {}, "timeout": 120,
        }).payload
        query_id = uuid(created["execution_id"])
        wait_execution(client, "develop", query_id)
        report["query_execution_id"] = query_id
        route = f"/api/v1/develop/executions/{query_id}/exports"
        body = {"format": "csv", "file_name": "object-develop"}
    with hold_target():
        created = client.request("POST", route, (202,), body).payload
        report.update(session_id=positive(created["id"]), execution_id=uuid(created["transfer_execution_id"]))
        report["claim"] = observe_claim(report["execution_id"], tenant)
    wait_execution(client, "transfer", report["execution_id"])
    path = f'/api/v1/{owner}/exports/{report["session_id"]}'
    current = client.request("GET", path, (200,)).payload
    require(current["status"] == "success" and current["transfer_execution_id"] == report["execution_id"],
            "export session terminal differs")
    row = session_row(owner, report["session_id"])
    require(row["tenant_id"] == tenant and str(row["user_id"]) == principal
            and row["transfer_execution_id"] == report["execution_id"]
            and len(row["execution_request_digest"]) == 64, "export session identity/digest differs")
    key = key_from_ref(row["target_locator"], "object")
    prefix = key_from_ref(row["target_parent_locator"], "prefix").rstrip("/") + "/"
    require(prefix.startswith(f"tenant_{tenant}/export/{owner}/") and key.startswith(prefix),
            "export object escaped its owner's prefix")
    downloaded = client.request("GET", path + "/file", (200,), raw_response=True).payload
    require(downloaded == object_bytes(key), "Gateway download differs from the actual manager bucket")
    report.update(path=path, target_locator=row["target_locator"], prefix=prefix, key=key,
                  request_digest=row["execution_request_digest"], csv=csv_proof(downloaded),
                  terminal=execution_proof(report["execution_id"], tenant, principal, source=owner))


def invalidate_protection(client, locator, exports):
    enrollment = client.request("POST", "/api/v1/security/protection-enrollments", (201,), {"locator": locator}).payload
    deadline = time.monotonic() + 120
    pending = list(exports)
    while pending and time.monotonic() < deadline:
        for item in list(pending):
            response = client.request("GET", item["path"], (200, 409))
            if response.status == 409:
                require(response.payload.get("error_code") == "export_protection_changed", "wrong protection refusal")
                file = client.request("GET", item["path"] + "/file", (409,))
                require(file.payload.get("error_code") == "export_protection_changed", "file did not refuse stale protection")
                item["protection_denial"] = {"status_http": 409, "file_http": 409,
                                              "error_code": "export_protection_changed"}
                pending.remove(item)
        if pending:
            time.sleep(1)
    require(not pending, "source enrollment did not invalidate both exported artifacts")
    return {"enrollment_id": uuid(enrollment["id"]), "entry": "Gateway protection-enrollments"}


def age_sql(rows):
    require(set(rows) == {"manager", "develop"}, "exactly two owner sessions required")
    updates = []
    for owner, row in rows.items():
        require(row["status"] == "success", "only downloaded successful sessions may be aged")
        filters = " AND ".join([
            f'id={positive(row["id"])}', f'tenant_id={positive(row["tenant_id"])}',
            f'user_id={positive(row["user_id"])}', "status='success'",
            "transfer_execution_id=" + quote(uuid(row["transfer_execution_id"])),
            "target_locator=" + quote(row["target_locator"]),
            "execution_request_digest=" + quote(row["execution_request_digest"]),
            "updated_at=" + quote(row["updated_at"]),
        ])
        updates.append(f"UPDATE {owner}.export_sessions SET created_at=clock_timestamp()-interval '25 hours', "
                       f"updated_at=clock_timestamp()-interval '25 hours' WHERE {filters}; "
                       "GET DIAGNOSTICS affected=ROW_COUNT; IF affected<>1 THEN "
                       "RAISE EXCEPTION 'exact T4 session was not aged'; END IF;")
    return "BEGIN; SET LOCAL lock_timeout='3s'; SET LOCAL statement_timeout='5s'; DO $$ DECLARE affected integer; BEGIN " + " ".join(updates) + " END $$; COMMIT;"


def timestamp_fixture(exports, tenant, principal):
    before = {owner: session_row(owner, item["session_id"]) for owner, item in exports.items()}
    for owner, row in before.items():
        require(row["tenant_id"] == tenant and str(row["user_id"]) == principal
                and row["transfer_execution_id"] == exports[owner]["execution_id"]
                and row["target_locator"] == exports[owner]["target_locator"], "timestamp fixture scope changed")
    pg(age_sql(before), write=True)
    evidence = {}
    for owner, row in before.items():
        after = session_row(owner, row["id"])
        def stable(value):
            return {key: value for key, value in value.items() if key not in {"created_at", "updated_at"}}
        require(stable(row) == stable(after), "timestamp fixture changed non-time session facts")
        evidence[owner] = {"id": row["id"], "created_at_before": row["created_at"], "updated_at_before": row["updated_at"],
                           "created_at_after": after["created_at"], "updated_at_after": after["updated_at"],
                           "unchanged_fields_sha256": hashlib.sha256(json.dumps(stable(row), sort_keys=True).encode()).hexdigest()}
    return {"mode": "approved_session_timestamp_fixture", "natural_24h_wait": False, "sessions": evidence}


def main(report):
    root = Path.cwd()
    require(os.environ.get("GITHUB_ACTIONS") == "true" and os.environ.get("ADDP_ONLINE_HOSTED") == "1"
            and os.environ.get("ONLINE_SUITE") == "infra-object-storage" and sys.platform == "linux"
            and os.environ.get("POSTGRES_DB") == "addp_online"
            and root == Path(os.environ["RUNNER_TEMP"]) / "infra-object-product", "pinned Hosted product required")
    sha = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    require(sha == os.environ["INFRA_OBJECT_SOURCE_COMMIT"]
            and not subprocess.check_output(["git", "status", "--porcelain"], text=True).strip(), "product checkout differs")
    tenant = positive(int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"]))
    foreign_tenant = positive(int(os.environ["ADDP_ONLINE_EVENTS_FOREIGN_TENANT_ID"]))
    require(tenant > 1 and foreign_tenant > 1 and tenant != foreign_tenant, "distinct disposable tenants required")
    engine = positive(int(os.environ["ADDP_ONLINE_CONSUMER_ENGINE_ID"]))
    def client(key):
        return api.GatewayClient(os.environ["GATEWAY_URL"], os.environ[key], 30)
    owner = client("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN")
    outsider = client("ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN")
    foreign = client("ADDP_ONLINE_FOREIGN_USER_ACCESS_TOKEN")
    owner_identity = identity(owner, tenant, PERMISSIONS)
    outsider_identity = identity(outsider, tenant, READER_PERMISSIONS)
    foreign_identity = identity(foreign, foreign_tenant, READER_PERMISSIONS)
    principal = owner_identity["principal_id"]
    require(principal != outsider_identity["principal_id"], "wrong-user test requires a distinct user")
    report.update(product_sha=sha, harness_sha=os.environ["GITHUB_SHA"], run_id=os.environ["ADDP_ONLINE_TEST_RUN_ID"],
                  tenant_id=tenant, foreign_tenant_id=foreign_tenant, engine_id=engine, physical_bucket="manager",
                  identities={"owner": owner_identity, "wrong_user": outsider_identity, "foreign": foreign_identity})
    report["phase"] = "import"
    imported = report["import"] = {}
    locator = import_once(owner, engine, tenant, principal, imported)
    report["phase"] = "export"
    exports = report["exports"] = {"manager": {}, "develop": {}}
    for module, evidence in exports.items():
        export_once(owner, module, locator, engine, tenant, principal, evidence)
        evidence["identity_denials"] = {}
        for name, reader in (("wrong_user", outsider), ("foreign", foreign)):
            reader.request("GET", evidence["path"], (404,))
            reader.request("GET", evidence["path"] + "/file", (404,))
            evidence["identity_denials"][name] = {"status_http": 404, "file_http": 404}
    report["phase"] = "import_identity_denial"
    foreign.request("GET", f'/api/v1/transfer/task-definitions/{imported["task_id"]}', (404,))
    rejected = etl.verify_unreadable_source_execution(outsider, imported["task_id"], 120)
    physical = pg(f"SELECT json_agg(row_to_json(t) ORDER BY id) FROM public.{TABLE} t", source=True)
    require(physical == imported["physical_rows"], "rejected execution changed the imported target")
    imported["denials"] = {"foreign_task_http": 404, "wrong_user_execution_id": rejected,
                            "target_unchanged": True, "records_read": 0, "records_written": 0}
    report["phase"] = "invalidate_protection"
    report["protection"] = invalidate_protection(owner, locator, list(exports.values()))
    # Independent object sentinels prove that prefix cleanup does not expand to
    # an owner, tenant or bucket. These are fixtures, not downloadable sessions.
    sentinels = {f"tenant_{tenant}/export/develop/t4-{uuid4().hex}/sentinel": b"other-owner",
                 f"tenant_{foreign_tenant}/export/manager/t4-{uuid4().hex}/sentinel": b"other-tenant"}
    for key, content in sentinels.items():
        result = subprocess.run(["docker", "exec", "-i", "addp-minio", "mc", "pipe", "local/manager/" + key],
                                input=content, capture_output=True, timeout=15)
        require(result.returncode == 0, "sentinel creation failed")
    report["phase"] = "age_only_two_sessions"
    report["expiry"] = timestamp_fixture(exports, tenant, principal)
    report["phase"] = "wait_original_product_cleanup"
    deadline = time.monotonic() + 31 * 60
    while time.monotonic() < deadline:
        if all(session_row(module, item["session_id"]) is None for module, item in exports.items()):
            break
        time.sleep(5)
    require(all(session_row(module, item["session_id"]) is None for module, item in exports.items()),
            "original scheduled cleaners did not delete both expired sessions")
    for item in exports.values():
        require(list_keys(item["prefix"]) == [], "expired session objects remain")
        owner.request("GET", item["path"], (404,))
        owner.request("GET", item["path"] + "/file", (404,))
    for key, content in sentinels.items():
        require(object_bytes(key) == content, "scheduled cleanup crossed its exact prefix")
    for name, proof in imported["files"].items():
        require(hashlib.sha256(object_bytes(imported["prefix"] + name)).hexdigest() == proof["sha256"],
                "export cleanup removed or changed import data")
    report["expiry"].update(cleanup="passed", exact_prefixes_removed=True, import_preserved=True,
                            sentinels=[{"key": key, "sha256": hashlib.sha256(value).hexdigest()} for key, value in sentinels.items()])
    report["phase"] = "task_cleanup"
    owner.request("DELETE", f'/api/v1/transfer/task-definitions/{imported["task_id"]}', (200,))
    owner.request("GET", f'/api/v1/transfer/task-definitions/{imported["task_id"]}', (404,))
    report.update(result="passed", phase="complete")


if __name__ == "__main__":
    evidence = {"schema_version": "addp.online-suite/v1", "suite": "infra-object-storage",
                "result": "failed", "phase": "startup"}
    try:
        main(evidence)
    finally:
        print(json.dumps(evidence, ensure_ascii=False, sort_keys=True))
