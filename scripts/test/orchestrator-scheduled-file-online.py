"""One Hosted scheduled file authorization case, using the real owner APIs."""

from datetime import datetime, timedelta, timezone
import hashlib
from http.client import HTTPConnection, HTTPException
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from importlib import import_module
import json
import os
from pathlib import Path
import re
import secrets
import signal
import subprocess
import sys
import threading
import time
from urllib.parse import quote, urlsplit
import uuid

API = import_module("scripts.utils.online-api")
ORCH = import_module("scripts.test.orchestrator-execution-online")
HOST = import_module("scripts.test.orchestrator-execution-faults")
GRANTS = import_module("scripts.test.online-engine-registration")
BUCKET_PREFIX = "t4-scheduled-"
KEY = "source/one-point.geojson"
SOURCE = b'{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"fixture_id":"scheduled-read-one"},"geometry":{"type":"Point","coordinates":[112.0,28.0]}}]}\n'
IMAGE = "addp-minio:RELEASE.2025-10-15T17-29-55Z"
CONTAINER = "addp-orchestrator-scheduled-source"
CASE = "scheduled-controlled-file"
ISSUE_PATH = "/api/v1/system/runtime/execution-authorizations/develop-file-workflows"
TASKS = "/api/v1/develop/task-definitions"
PIPELINES = ORCH.ORCH + "/orchestrations"
PERMISSIONS = {
    "orchestrator.workflow.create", "orchestrator.workflow.read", "orchestrator.workflow.update",
    "orchestrator.workflow.delete", "orchestrator.workflow.execute", "system.execution_authorization.create",
    "develop.task.create", "develop.task.read", "develop.task.delete", "develop.task.execute",
    "develop.data_read.execute",
}
require = ORCH.require


def owner():
    directory = HOST.deployment_secret()
    run_id = os.environ["ADDP_ONLINE_TEST_RUN_ID"]
    require(os.environ.get("ADDP_ONLINE_ORCHESTRATOR_CASE") == CASE
            and bool(re.fullmatch(r"orchestrator-[0-9]{1,20}-[0-9]{1,6}", run_id)),
            "scheduled file acceptance requires its dedicated Hosted run")
    return directory, run_id


def docker(*args, data=None, env=None):
    result = subprocess.run(["docker", *args], capture_output=True, input=data, env=env, timeout=60)
    require(result.returncode == 0, "owned MinIO fixture Docker operation failed")
    return result.stdout


def source_container():
    _, run_id = owner()
    ids = docker("ps", "-aq", "--no-trunc", "--filter", f"name=^/{CONTAINER}$").decode().split()
    if not ids:
        return None
    identity, = ids
    entry, = json.loads(docker("container", "inspect", identity))
    require(entry["Id"] == identity, "scheduled source identity changed")
    labels = entry["Config"]["Labels"]
    require(labels.get("com.addp.online-fixture") == CASE and labels.get("com.addp.online-run-id") == run_id,
            "refusing foreign scheduled file source")
    return entry


def objectstore(*args, key=None, data=None):
    directory, run_id = owner()
    container = source_container()
    require(container is not None, "scheduled file source missing")
    credentials = json.loads((directory / "orchestrator-engine.json").read_text())["connection_info"]
    locator = f"fixture/{BUCKET_PREFIX}{run_id}" + (f"/{key}" if key else "")
    alias = "http://" + credentials["access_key"] + ":" + credentials["secret_key"] + "@127.0.0.1:9000"
    return docker("exec", "-i", "-e", "MC_HOST_fixture", container["Id"], "mc", *args, locator,
                  data=data, env=dict(os.environ, MC_HOST_fixture=alias))


def seed():
    directory, run_id = owner()
    require(source_container() is None, "refusing to reuse a fixture")
    password = secrets.token_hex(32)
    with open(directory / "scheduled-source.env", "x", opener=lambda path, flags: os.open(path, flags, 0o600)) as envfile:
        envfile.write("MINIO_ROOT_USER=scheduled-source\nMINIO_ROOT_PASSWORD=" + password + "\n")
    docker("run", "-d", "--name", CONTAINER, "--label", f"com.addp.online-fixture={CASE}",
           "--label", f"com.addp.online-run-id={run_id}", "--env-file", str(directory / "scheduled-source.env"),
           "--mount", "type=tmpfs,destination=/data", "-p", "127.0.0.1::9000", IMAGE, "server", "/data")
    container = source_container()
    port, = container["NetworkSettings"]["Ports"]["9000/tcp"]
    require(port["HostIp"] == "127.0.0.1", "scheduled source port is not loopback")
    with open(directory / "orchestrator-engine.json", "x", opener=lambda path, flags: os.open(path, flags, 0o600)) as out:
        json.dump(dict(name=f"T4 {run_id}", engine_type="minio", engine_origin="general",
                       connection_info=dict(endpoint="127.0.0.1:" + port["HostPort"],
                                            access_key="scheduled-source", secret_key=password, use_ssl=False),
                       description="Disposable scheduled read source"), out)
    probe = API.GatewayClient("http://127.0.0.1:" + port["HostPort"], "", 2)
    for attempt in range(60):
        try:
            probe.request("GET", "/minio/health/ready", (200,), raw_response=True)
            break
        except API.SuiteError:
            if attempt == 59:
                raise
            time.sleep(1)
    objectstore("mb")
    objectstore("pipe", key=KEY, data=SOURCE)


def issue_event(data, status, body):
    execution = str(uuid.UUID(json.loads(data)["execution_id"]))
    error_code = json.loads(body).get("error_code")
    if error_code is not None and not re.fullmatch(r"[a-z_]{1,64}", str(error_code)):
        error_code = "invalid"
    return {"execution_id": execution, "status": status, "error_code": error_code}


def stop():
    entry = source_container()
    if entry is not None:
        require(all(mount["Type"] == "tmpfs" for mount in entry["Mounts"]), "unexpected source mount")
        docker("rm", "-fv", entry["Id"])
    require(source_container() is None, "scheduled MinIO source left a container")


def observe_issuance():
    """Forward only to the verified local System, saving only issuance outcomes."""
    directory, _ = owner()
    ledger = directory / "scheduled-issue.jsonl"
    ledger.touch(mode=0o600, exist_ok=False)
    lock = threading.Lock()
    hop = {"connection", "proxy-connection", "proxy-authenticate", "proxy-authorization",
           "keep-alive", "te", "trailer", "transfer-encoding", "upgrade", "host", "content-length", "server", "date"}

    class Observer(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def forward(self):
            target = urlsplit(self.path)
            if target.netloc or not target.path.startswith("/") or self.headers.get("Transfer-Encoding"):
                self.send_error(400)
                return
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 <= length <= 4 * 1024 * 1024:
                self.send_error(413)
                return
            data = self.rfile.read(length)
            headers = {k: v for k, v in self.headers.items() if k.lower() not in hop | {"accept-encoding"}}
            upstream = HTTPConnection("127.0.0.1", 8180, timeout=20)
            try:
                upstream.request(self.command, self.path, body=data or None, headers=headers)
                response = upstream.getresponse()
                body = response.read(4 * 1024 * 1024 + 1)
                require(len(body) <= 4 * 1024 * 1024, "System response limit exceeded")
                if self.command == "POST" and target.path == ISSUE_PATH:
                    event = issue_event(data, response.status, body)
                    with lock, ledger.open("a") as records:
                        records.write(json.dumps(event) + "\n")
                self.send_response(response.status)
                for key, value in response.getheaders():
                    if key.lower() not in hop:
                        self.send_header(key, value)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            finally:
                upstream.close()

        def serve_request(self):
            try:
                self.forward()
            except (OSError, HTTPException, ValueError, KeyError, TypeError, API.SuiteError):
                # Preserve the missing witness as a failure without printing credentials.
                self.send_error(502)

        do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = serve_request

    with ThreadingHTTPServer(("127.0.0.1", 0), Observer) as server:
        (directory / "scheduled-system-port").write_text(str(server.server_port))
        server.serve_forever()


def verify_build():
    owner()
    sha = os.environ["ADDP_ONLINE_ORCHESTRATOR_SOURCE_COMMIT"]
    require(re.fullmatch(r"[0-9a-f]{40}", sha), "missing exact source SHA")
    builds = {}
    for name in ("SYSTEM", "GATEWAY", "META", "ORCHESTRATOR", "DEVELOP", "MONITOR", "SECURITY"):
        client = API.GatewayClient(os.environ[name + "_URL"], "", 10)
        live = client.request("GET", "/health/live", (200,)).payload
        require(live.get("git_commit") == sha, "service source identity differs")
        fields = ("build_id", "source_fingerprint", "built_at", "started_at")
        require(all(isinstance(live.get(k), str) and live[k] not in {"", "unknown"} for k in fields),
                "service build proof is incomplete")
        client.request("GET", "/health/ready", (200,))
        builds[name.lower()] = {k: live[k] for k in fields}
    runtime = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "raster-runtime-build.txt"
    require(f"git_commit={sha}" in runtime.read_text().splitlines(), "Runtime build differs")
    (runtime.parent / "scheduled-build.json").write_text(json.dumps({"sha": sha, "services": builds}))


class ExactScheduledGrant(GRANTS.ExactFileReadGrants):
    """Use the source authorizer's Grant list, without Manager permissions."""

    def observe(self, target, allowed, *, phase="read_check"):
        self.phase = phase
        result = self.authorizer.request("GET", f"/api/v1/system/engines/{target['engine_id']}/access_grants",
                                         (200,)).payload
        rows = result.get("data")
        require(type(rows) is list and result.get("total") == len(rows) == int(allowed),
                "unexpected active Grants in the dedicated source engine")
        if allowed:
            row = self.pending[0]
            require(rows[0].get("request_id") == row["request_id"]
                    and rows[0].get("catalog_path") == target
                    and rows[0].get("recipient_id") == self.recipient
                    and rows[0].get("action") == "read"
                    and rows[0].get("grant_count") == 1, "wrong active source Grant")

    def withdraw(self, report, checkpoint):
        row, = self.pending
        command = row["request_id"]
        base = f"/api/v1/system/engines/{row['engine']}/access_grants"
        if not row["confirmed"]:
            # A lost create response never authorizes issuing another Grant.
            history = self.authorizer.request("GET", base + "/history", (200,)).payload
            records = history.get("data")
            require(type(records) is list and history.get("total") == len(records) <= 1,
                    "cannot resolve the original Grant creation")
            if not records:
                self.observe(row["target"], False)
                self.pending.remove(row)
                return
            require(records[0].get("request_id") == command
                    and records[0].get("catalog_path") == row["target"]
                    and records[0].get("recipient_id") == self.recipient,
                    "unknown Grant differs from the original creation")
        require("grant_revoke_attempted" not in report, "original Grant withdrawal already attempted")
        report["grant_revoke_attempted"] = command
        checkpoint()
        receipt = self.authorizer.request("POST", f"{base}/{command}/revoke", (200,),
                                          {"reason": "Disposable scheduled read acceptance"}).payload
        require(receipt.get("request_id") == command and receipt.get("revoked_request_ids") == [command],
                "withdrawal receipt differs from the original Grant")
        withdrawn = instant(receipt["revoked_at"])
        history = self.authorizer.request("GET", base + "/history", (200,)).payload
        rows = history.get("data")
        require(type(rows) is list and history.get("total") == len(rows) == 1
                and rows[0].get("request_id") == command and rows[0].get("revocation") == receipt,
                "original withdrawal history missing")
        self.observe(row["target"], False, phase="after_withdrawal")
        self.pending.remove(row)
        report["grant_revoked_at"] = withdrawn.isoformat()
        checkpoint()
        return withdrawn


def instant(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def slots(now):
    first = now.replace(second=0, microsecond=0) + timedelta(minutes=3)
    if first.minute > 51:
        first = first.replace(minute=0) + timedelta(hours=1)
    second = first + timedelta(minutes=8)
    require(second - now <= timedelta(minutes=22), "two schedule slots exceed the run budget")
    return first, second, f"CRON_TZ=UTC {first.minute},{second.minute} {first.hour} {first.day} {first.month} *"


def json_rows(postgres, query):
    return json.loads(postgres("BEGIN READ ONLY; SET LOCAL statement_timeout='5s'; " + query + "; COMMIT;"))


def observations(postgres, tenant, pipeline, task):
    # IDs originate in accepted Owner responses, never in caller-controlled SQL.
    require(all(type(i) is int and 0 < i < 2**32 for i in (tenant, pipeline, task)), "invalid observation scope")
    return json_rows(postgres, f"""
WITH parents AS (
 SELECT execution_id, status, trigger_type, actor_principal_id, actor_tenant_membership_id,
        issued_authorization_version, attempt, created_at
 FROM common.task_executions
 WHERE tenant_id={tenant} AND module='orchestrator' AND source_task_id='{pipeline}'
)
SELECT jsonb_build_object('parents',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.created_at)
 FROM parents p),'[]'::jsonb),'children',COALESCE((
 SELECT jsonb_agg(jsonb_build_object(
    'id',c.execution_id,'parent',c.parent_execution_id,'status',c.status,'trigger',c.trigger_type,
    'attempt',c.attempt,'code',c.error_details->>'code',
    'expected_child',c.module='develop' AND c.source='orchestrator' AND c.task_type='workflow'
                        AND c.source_task_id='{task}',
    'actor',c.actor_principal_id,'membership',c.actor_tenant_membership_id,'version',c.issued_authorization_version,
    'started',c.started_at IS NOT NULL,'admission',f.admission_state,
    'scope',ea.develop_file_scope,'runtime',f.runtime_execution_id IS NOT NULL,
    'runtime_status',c.metadata->'result'->'runtime_status'->>'status',
    'source_reads',f.source_read_proofs_json,
    'no_output',f.publication_json IS NULL AND NOT COALESCE(c.metadata ? 'outputs', false),
    'input_directory',f.input_directory,'issued',c.execution_authorization_id IS NOT NULL,
    'private_admission',f.issued_authorization_json IS NOT NULL,
    'parent_claim',(f.intent_json->'parent') - 'lease_digest', 'intent_digest',f.intent_json->>'config_digest'
 ))
 FROM common.task_executions c JOIN parents p ON p.execution_id=c.parent_execution_id
 LEFT JOIN develop.file_executions f ON f.execution_id=c.execution_id AND f.tenant_id=c.tenant_id
 LEFT JOIN system.execution_authorizations ea ON ea.id=c.execution_authorization_id AND ea.execution_id::text=c.execution_id
 WHERE c.tenant_id={tenant}
),'[]'::jsonb))
""")


def complete_parent(client, postgres, tenant, pipeline, task, due, ordinal):
    while datetime.now(timezone.utc) <= due + timedelta(minutes=2):
        parents = observations(postgres, tenant, pipeline, task)["parents"]
        require(len(parents) <= ordinal, "unexpected extra parent execution")
        if len(parents) == ordinal:
            parent = parents[-1]
            require(parent["trigger_type"] == "scheduled" and instant(parent["created_at"]) >= due,
                    "parent did not originate from the scheduled slot")
            return parent, ORCH.wait_execution(client, parent["execution_id"], timeout=175)
        time.sleep(1)
    raise API.SuiteError("scheduled parent did not arrive within two minutes")


def check_execution(rows, parent, identity, allowed, target, engine_version, grant):
    matches = [item for item in rows["children"] if item["parent"] == parent["execution_id"]]
    require(len(matches) == 1, "expected one durable child for this parent")
    child, = matches
    require((parent["actor_principal_id"], parent["actor_tenant_membership_id"],
             parent["issued_authorization_version"]) == identity
            and (child["actor"], child["membership"], child["version"]) == identity,
            "scheduled execution identity differs from the configured User")
    require(child["expected_child"] is True and child["trigger"] == "scheduled"
            and child["parent_claim"] == {"execution_id": parent["execution_id"], "attempt": parent["attempt"]}
            and child["no_output"] is True, "child trigger, parent claim or output differs")
    if allowed:
        require(child["status"] == "success" and child["attempt"] == 1 and child["started"] is True
                and child["runtime"] is True and child["runtime_status"] == "success"
                and child["issued"] is True and child["private_admission"] is True,
                "authorized child did not finish its private read")
        staging = Path(child["input_directory"])
        require(staging.is_absolute() and staging.name.startswith("addp-develop-inputs-"),
                "private input directory identity differs")
        deadline = time.monotonic() + 10
        while os.path.lexists(staging):
            require(time.monotonic() < deadline, "private input directory was not released")
            time.sleep(0.2)
        scope = child["scope"]
        require(isinstance(scope, dict) and "target" not in scope and scope.get("sources") == [{
            "target": target, "format": "geojson", "item_fingerprint": grant["item_fingerprint"],
            "boundary_hash": grant["boundary_hash"], "engine_version": engine_version,
        }], "sealed source scope differs")
        reads = child["source_reads"]
        require(type(reads) is list and len(reads) == 1 and reads[0].get("node_id") == "load"
                and reads[0].get("engine_version") == engine_version
                and reads[0].get("read_set", {}).get("paths") == [target], "actual source read session differs")
    else:
        require(child["status"] == "failed" and child["admission"] == "rejected" and child["attempt"] == 0
                and child["code"] == "develop.workflow.file_authorization_rejected"
                and child["scope"] is None and child["source_reads"] is None and child["runtime"] is False
                and child["runtime_status"] is None
                and child["started"] is False and not child["input_directory"]
                and child["issued"] is False and child["private_admission"] is False,
                "revoked child crossed read admission")
    return child["id"]


def case(consumer, authorizer, postgres, tenant, engine, directory, run_id, report, checkpoint, refresh_consumer):
    ctx_path = "/api/v1/system/auth/context"
    def current():
        ctx = consumer.request("GET", ctx_path, (200,)).payload
        roles, permissions = ORCH.role_permissions(ctx)
        require(permissions == PERMISSIONS and not roles.intersection(API.FORBIDDEN_ADMIN_ROLES),
                "scheduled User permission set differs")
        return tuple(map(int, (*ORCH.user_binding(ctx, tenant), ctx["authorization"]["authorization_version"])))

    identity = current()
    def record_grant(pending):
        if pending:
            report["source_grant_id"] = pending[0]["request_id"]
            checkpoint()

    grants = ExactScheduledGrant(authorizer, consumer, tenant, scan_preparation=True, on_change=record_grant)
    full_name = f"{BUCKET_PREFIX}{run_id}/{KEY}"
    target = grants.target(engine, full_name, "minio")
    pipeline, task, definition = None, None, None
    report["source"] = {"engine_id": engine, "key": full_name, "sha256": hashlib.sha256(SOURCE).hexdigest()}
    report["user_identity"] = dict(zip(("principal_id", "membership_id", "authorization_version"), identity))
    checkpoint()
    failure = None
    try:
        require(objectstore("cat", key=KEY) == SOURCE, "prepared source bytes differ")
        report["source"]["fixture_bytes_verified"] = True
        report["pending_mutation"] = "basic_scan"
        checkpoint()
        scan = authorizer.request("POST", "/api/v1/meta/scan/run/manual", (201,), {
            "engine_id": engine, "catalog_paths": [full_name.rsplit("/", 1)[0]], "scan_depth": "basic",
            "trigger_type": "manual", "force": True,
        }).payload
        scan_id = str(uuid.UUID(scan["execution_id"]))
        report["scan_execution_id"] = scan_id
        report.pop("pending_mutation")
        checkpoint()
        state = ORCH.wait_execution(authorizer, scan_id, module=ORCH.META)
        require(state["status"] == "success" and state.get("scan_context", {}).get("scan_depth") == "basic",
                "source basic scan failed")
        grant = grants.grant(engine, full_name, storage="minio")
        def engine_version():
            return str(json_rows(postgres, f"SELECT version FROM system.engines WHERE id={engine} AND tenant_id={tenant}"))

        version = engine_version()
        def item_identity():
            return json_rows(postgres, f"""SELECT
 CASE WHEN count(*)=1 THEN max(id) FILTER (
    WHERE item_type='object' AND fingerprint='{grant["item_fingerprint"]}') END
 FROM meta.meta_item WHERE tenant_id={tenant} AND engine_id={engine} AND deleted_at IS NULL""")

        item = item_identity()
        require(version.isdecimal() and int(version) > 0 and type(item) is int and item > 0,
                "precise source version or item identity unavailable")
        report["source"].update(item_id=item, engine_version=version, item_fingerprint=grant["item_fingerprint"],
                                boundary_hash=grant["boundary_hash"])
        checkpoint()
        runtime_list = consumer.request("GET", "/api/v1/develop/workflow-engines", (200,), response_type=list).payload
        runtimes = [r for r in runtime_list if r.get("engine_type") == "geopython_workflow"
                    and r.get("connection_status") == "online"]
        require(len(runtimes) == 1, "expected one online GeoPython Runtime")
        runtime = API.require_positive_int(runtimes[0], "id")
        locator = f"addp://engine/{engine}/path/{quote(full_name)}?type=object&item_id={item}"
        task_body = {"name": run_id, "dev_type": "workflow", "timeout": 120, "execution_config": {"engine_id": runtime},
                     "content": {"workflow_definition": {"tasks": [
                         {"id": "load", "operator": "load", "depends_on": [], "params": {"locator": locator}}
                     ]}}}
        report["pending_mutation"] = "create_develop_task"
        checkpoint()
        task = API.require_positive_int(consumer.request("POST", TASKS, (200,), task_body).payload, "id")
        report["develop_task_id"] = task
        report.pop("pending_mutation")
        checkpoint()
        task_path = f"{TASKS}/{task}"
        original_task = consumer.request("GET", task_path, (200,)).payload
        task_fields = ("name", "dev_type", "content", "execution_config")
        require(original_task.get("content") == task_body["content"]
                and original_task.get("execution_config") == task_body["execution_config"], "saved Develop definition differs")
        first, second, cron = slots(datetime.now(timezone.utc))
        definition = {"name": run_id, "schedule": cron, "enabled": True,
                      "steps": [ORCH.step("read", "develop", "workflow", task)]}
        report["pending_mutation"] = "create_orchestration"
        checkpoint()
        created = consumer.request("POST", PIPELINES, (201,), definition).payload
        pipeline = API.require_positive_int(created, "id")
        report.update(orchestration_id=pipeline, planned_slots=[first.isoformat(), second.isoformat()])
        report.pop("pending_mutation")
        checkpoint()
        def saved():
            return consumer.request("GET", f"{PIPELINES}/{pipeline}", (200,)).payload

        initial = saved()
        require(initial.get("enabled") is True and instant(initial["next_run_at"]) == first,
                "first next_run_at does not match the selected schedule")
        for ordinal, (due, allowed) in enumerate(((first, True), (second, False)), 1):
            refresh_consumer(consumer)
            parent, result = complete_parent(consumer, postgres, tenant, pipeline, task, due, ordinal)
            phase = "allow" if allowed else "deny"
            report[phase] = {"parent": parent["execution_id"], "status": result["status"],
                             "parent_created_at": parent["created_at"], "parent_completed_at": result.get("completed_at")}
            checkpoint()
            expected = "success" if allowed else "failed"
            require(result["status"] == expected and result.get("trigger_type") == "scheduled"
                    and due <= instant(result["completed_at"]) <= due + timedelta(minutes=5),
                    "Orchestrator scheduled terminal status or timing differs")
            steps = result.get("metadata", {}).get("step_results", {})
            require(set(steps) == {"read"} and steps["read"].get("status") == expected,
                    "Orchestrator step outcome differs")
            rows = observations(postgres, tenant, pipeline, task)
            report[phase]["children_observed"] = [
                {key: item[key] for key in ("id", "status", "code", "attempt")}
                for item in rows["children"] if item["parent"] == parent["execution_id"]
            ]
            ledger = [json.loads(row) for row in (directory / "scheduled-issue.jsonl").read_text().splitlines()]
            report["system_issuance_observed"] = ledger
            report["execution_counts_observed"] = [len(rows["parents"]), len(rows["children"])]
            checkpoint()
            child = check_execution(rows, parent, identity, allowed, target, version, grant)
            expected_issue = {"execution_id": child, "status": 201 if allowed else 403,
                              "error_code": None if allowed else "permission_denied"}
            require(ledger == ([expected_issue] if allowed else [report["allow"]["issue"], expected_issue]),
                    "actual System issuance response differs")
            if allowed:
                require(steps["read"].get("result") == {"execution_id": child}, "Orchestrator linked a different positive child")
            observed = next(item for item in rows["children"] if item["id"] == child)
            report[phase].update(child=child, issue=expected_issue, private_facts={
                key: observed[key] for key in ("actor", "membership", "version", "parent_claim", "intent_digest",
                                               "attempt", "admission", "scope", "runtime", "runtime_status", "source_reads", "no_output")
            })
            checkpoint()
            if not allowed:
                require(withdrawn < instant(parent["created_at"]) and observed["intent_digest"] == allow_intent,
                        "second scheduler claim preceded withdrawal or used a different input definition")
            if allowed:
                allow_intent = observed["intent_digest"]
                require(datetime.now(timezone.utc) < second - timedelta(seconds=30), "no safe revocation window")
                withdrawn = grants.withdraw(report, checkpoint)
                after = saved()
                final_task = consumer.request("GET", task_path, (200,)).payload
                after_facts = authorizer.request("GET", f"/api/v1/system/engines/{engine}/data-items/{grant['item_fingerprint']}/authorization-facts",
                                                 (200,)).payload
                require(current() == identity and engine_version() == version and item_identity() == item
                        and after_facts.get("boundary_hash") == grant["boundary_hash"]
                        and all(final_task.get(k) == original_task.get(k) for k in task_fields)
                        and after.get("enabled") is True
                        and instant(after["next_run_at"]) == second
                        and all(after.get(k) == initial.get(k) for k in ("schedule", "steps", "name")),
                        "schedule, task definition or User authorization changed after revocation")
                require(datetime.now(timezone.utc) < second - timedelta(seconds=15), "revocation not confirmed before the second slot")
        require(len(rows["parents"]) == len(rows["children"]) == 2
                and objectstore("cat", key=KEY) == SOURCE, "execution count or source bytes differ")
        inventory = [json.loads(line) for line in objectstore("ls", "--recursive", "--json").splitlines()]
        require(len(inventory) == 1 and inventory[0].get("key") == KEY, "source bucket gained an unexpected object")
        report["source_unchanged"] = True
        checkpoint()
    except Exception as error:
        failure = error
        report["first_failure"] = str(error) if isinstance(error, API.SuiteError) else type(error).__name__
        checkpoint()
        raise
    finally:
        errors = []
        def cleanup(name, action):
            try:
                action()
                report.setdefault("cleanup", {})[name] = True
            except Exception:
                errors.append(name)
                report.setdefault("cleanup", {})[name] = False
            checkpoint()

        if pipeline is not None:
            def remove_pipeline():
                client_path = f"{PIPELINES}/{pipeline}"
                consumer.request("PUT", client_path, (200,), dict(definition, enabled=False))
                state = consumer.request("GET", client_path, (200,)).payload
                require(state.get("enabled") is False and state.get("next_run_at") is None,
                        "schedule remained enabled")
                consumer.request("DELETE", client_path, (200,))
                consumer.request("GET", client_path, (404,))
            cleanup("orchestration", remove_pipeline)
        if grants.pending:
            if "grant_revoke_attempted" in report:
                errors.append("grant_withdrawal_unverified")
            else:
                cleanup("grant", lambda: grants.withdraw(report, checkpoint))
        if task is not None:
            def remove_task():
                client_path = f"{TASKS}/{task}"
                consumer.request("DELETE", client_path, (200,))
                consumer.request("GET", client_path, (404,))
            cleanup("develop_task", remove_task)
        if report.get("source_unchanged"):
            def remove_source():
                objectstore("rm", key=KEY)
                require(not objectstore("ls", "--recursive", "--json").strip(), "source bucket still contains objects")
            cleanup("source_object", remove_source)
        if report.get("pending_mutation"):
            errors.append("unknown_mutation")
        report["cleanup_errors"] = errors
        checkpoint()
        if failure is None:
            require(not errors, "scheduled acceptance cleanup incomplete")


def run():
    directory, run_id = owner()
    report = {"case": CASE, "run_id": run_id}
    artifact = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "orchestrator-scheduled-file.json"
    def checkpoint():
        temp = artifact.with_suffix(".tmp")
        temp.write_text(json.dumps(report, sort_keys=True) + "\n")
        temp.replace(artifact)

    def interrupted(*_):
        raise API.SuiteError("scheduled acceptance interrupted")

    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, interrupted)
    try:
        build = json.loads((artifact.parent / "scheduled-build.json").read_text())
        require(build["sha"] == os.environ["ADDP_ONLINE_ORCHESTRATOR_SOURCE_COMMIT"], "build evidence mismatch")
        report["build"] = build
        tenant, engine = (int(os.environ[k]) for k in ("ADDP_ONLINE_TEST_TENANT_ID", "ADDP_ONLINE_CONSUMER_ENGINE_ID"))
        require(1 < tenant < 2**32 and 0 < engine < 2**32, "disposable Tenant and precise engine required")
        base = os.environ["GATEWAY_URL"]
        login = API.GatewayClient(base, "", 10)
        def refresh_consumer(client):
            issued = login.request("POST", "/api/v1/system/login", (200,), {
                "username": os.environ["ADDP_ONLINE_TEST_USER_USERNAME"],
                "password": os.environ["ADDP_ONLINE_TEST_USER_PASSWORD"],
            }).payload
            token = (issued.get("session") or {}).get("access_token")
            require(issued.get("next_action") == "session_issued" and isinstance(token, str) and bool(token),
                    "ordinary User login did not issue a session")
            client.token = token

        consumer = API.GatewayClient(base, "", 10)
        refresh_consumer(consumer)
        API.validate_user_identity(consumer, tenant, PERMISSIONS)
        authorizer = API.GatewayClient(base, os.environ["ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"], 10)
        case(consumer, authorizer, HOST.HostedFaults().postgres, tenant, engine, directory, run_id, report, checkpoint, refresh_consumer)
        report["result"] = "passed"
        return 0
    except Exception as error:
        report.setdefault("first_failure", str(error) if isinstance(error, API.SuiteError) else type(error).__name__)
        report["result"] = "failed"
        return 1
    finally:
        checkpoint()
        print(json.dumps(report, sort_keys=True))


if __name__ == "__main__":
    commands = {"start": seed, "stop": stop, "verify-build": verify_build, "observe-issuance": observe_issuance}
    if len(sys.argv) != 2 or sys.argv[1] not in commands:
        raise SystemExit("use the standard Hosted lifecycle")
    commands[sys.argv[1]]()
