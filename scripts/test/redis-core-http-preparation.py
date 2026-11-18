#!/usr/bin/env python3
"""Disposable Hosted preparation and scoped physical observation for Redis R1.

Only the explicit Hosted gate may start product processes. Normal unit/CI
runner gates do not invoke start/accept and cannot dispatch Online acceptance.
"""
from __future__ import annotations

import json
import http.client
import os
from pathlib import Path
import secrets
import signal
import socket
import subprocess
import sys
import time
from urllib.parse import urlsplit
from uuid import uuid4

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from importlib import import_module
s = import_module("scripts.test.redis-core-http-online")
api = import_module("scripts.utils.online-api")
registration = import_module("scripts.test.online-engine-registration")
pg_auth = import_module("scripts.test.pg-source-authorization-online")

SOURCE = "addp-redis-core-source"
SOURCE_DB = "redis_core_fixture"
TABLE = "redis_core_probe"


class IdentityClient(api.GatewayClient):
    def request(self, *args, **kwargs):
        response = super().request(*args, **kwargs)
        if len(args)>1 and args[1] == s.CONTEXT_PATH: self.context = response.payload
        return response


def private():
    path = Path(os.environ["ADDP_ONLINE_SECRET_DIR"])
    s.require(path.is_absolute() and path.stat().st_mode & 0o077 == 0, "owner-only Hosted directory required")
    return path


def command(args, **kwargs):
    result = subprocess.run(args, capture_output=True, **kwargs)
    s.require(result.returncode == 0, "owned fixture command failed")
    return result.stdout


def initialize_source(container, password, artifact_dir):
    args = ["docker", "exec", "-i", "-e", "PGPASSWORD", container,
            "psql", "-XqwAt", "-h", "127.0.0.1", "-U", "postgres", "-d", SOURCE_DB,
            "-v", "ON_ERROR_STOP=1"]
    environment = dict(os.environ, PGPASSWORD=password)

    def fail(stage, exit_code, stderr, reason):
        # The source has one generated credential. Never archive argv, env or SQL input.
        detail = (stderr or b"").decode(errors="replace").replace(password, "[redacted]")
        detail = " ".join(detail.split())[:512]
        record = {"stage": stage, "reason": reason, "exit_code": exit_code, "stderr": detail}
        (artifact_dir / "redis-core-source-error.json").write_text(json.dumps(record) + "\n")
        raise s.EvidenceError(json.dumps(record)) from None

    def execute(stage, options, timeout, sql=None):
        try:
            return subprocess.run([*args, *options], env=environment, input=sql,
                                  capture_output=True, timeout=timeout)
        except subprocess.TimeoutExpired as error:
            fail(stage, None, error.stderr, "command_timeout")

    deadline = time.monotonic() + 60
    while True:
        probe = execute("source_readiness", ["-c", "SELECT current_database()"], 5)
        if probe.returncode == 0:
            if probe.stdout.strip() != SOURCE_DB.encode():
                fail("source_readiness", 0, probe.stderr, "unexpected_database")
            break
        if time.monotonic() >= deadline:
            fail("source_readiness", probe.returncode, probe.stderr, "readiness_timeout")
        time.sleep(.2)

    # The application-visible TCP server has opened the target DB. Seed it once atomically.
    sql = (f"CREATE TABLE public.{TABLE}(id integer PRIMARY KEY,label text NOT NULL);\n"
           f"INSERT INTO public.{TABLE} VALUES(1,'redis-r1-byte-baseline');\n")
    seeded = execute("source_seed", ["--single-transaction", "-f", "-"], 10, sql.encode())
    if seeded.returncode:
        fail("source_seed", seeded.returncode, seeded.stderr, "command_failed")


def database(statement, source=False):
    # Caller only supplies fixed SQL and validated numeric fixture identities.
    args = ["docker", "exec", "-i", "-e", "PGOPTIONS=-c default_transaction_read_only=on", SOURCE if source else "addp-postgres",
            "psql", "-XqAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres" if source else os.environ["POSTGRES_USER"],
            "-d", SOURCE_DB if source else "addp_online"]
    raw = command(args, input=statement.encode(), timeout=3)
    return json.loads(raw) if raw.strip() else None


def facts(identity):
    tenant, account, consumer, query, engine = (int(identity[key]) for key in (
        "ADDP_ONLINE_TEST_TENANT_ID", "ADDP_REDIS_HTTP_ACCOUNT_ID", "ADDP_REDIS_HTTP_CONSUMER_ID",
        "ADDP_REDIS_HTTP_QUERY_ID", "ADDP_REDIS_HTTP_ENGINE_ID"))
    s.require(tenant > 1 and min(account, consumer, query, engine) > 0, "scoped physical facts required")
    reader, authorizer = (int(identity[key]) for key in ("ADDP_REDIS_HTTP_READER_ID", "ADDP_REDIS_HTTP_SOURCE_ID"))
    s.require(min(reader,authorizer)>0, "scoped preparation identities missing")
    # No token/credential hashes or values are selected. Runtime credential
    # last_used_at is expected to change on valid requests and is not authority.
    observation = database(f"""SELECT json_build_object(
      'tokens',(SELECT coalesce(json_agg(json_build_object('id',t.id,'family',t.family_id,'expires',t.expires_at,'revoked',t.revoked_at) ORDER BY t.id),'[]')
        FROM system.access_tokens t JOIN system.refresh_token_families f ON f.id=t.family_id WHERE f.principal_id={account}),
      'families',(SELECT coalesce(json_agg(json_build_object('id',f.id,'version',f.issued_authorization_version,'expires',f.expires_at,'revoked',f.revoked_at) ORDER BY f.id),'[]')
        FROM system.refresh_token_families f WHERE f.principal_id={account}),
      'assignments',(SELECT coalesce(json_agg(to_jsonb(r) ORDER BY r.id),'[]') FROM system.role_assignments r WHERE r.principal_id IN ({account},{reader},{authorizer})),
      'probe_roles',(SELECT count(*) FROM system.role_assignments r WHERE r.principal_id={account}),
      'source_grants',(SELECT coalesce(json_agg(to_jsonb(g) ORDER BY g.request_id),'[]') FROM system.engine_access_grants g WHERE g.tenant_id={tenant} AND g.engine_id={engine}),
      'consumer',(SELECT to_jsonb(c) FROM system.api_consumers c WHERE c.id={consumer} AND c.tenant_id={tenant}),
      'consumer_grants',(SELECT coalesce(json_agg(to_jsonb(g) ORDER BY g.id),'[]') FROM system.api_consumer_service_grants g WHERE g.api_consumer_id={consumer}),
      'credentials',(SELECT coalesce(json_agg(json_build_object('id',c.id,'status',c.status,'expires',c.expires_at,'revoked',c.revoked_at) ORDER BY c.id),'[]') FROM system.api_consumer_credentials c WHERE c.api_consumer_id={consumer}),
      'query',(SELECT to_jsonb(q) FROM service.query_services q WHERE q.id={query} AND q.tenant_id={tenant}),
      'issuance_audits',(SELECT coalesce(json_agg(a.id ORDER BY a.id),'[]') FROM system.audit_logs a WHERE a.principal_id={account} AND a.event_name='oauth.token.issued' AND a.result='succeeded')
    )""")
    observation["source"] = database(f"SELECT json_agg(t ORDER BY t.id) FROM public.{TABLE} t", True)
    s.require(len(observation["tokens"]) == 1 and observation["probe_roles"] == 0 and observation["query"] is not None,
              "dedicated Token/Query physical baseline missing")
    return observation


class IdentityTimeout(s.EvidenceError):
    """Hand the disposable deployment to outer cleanup after observing the writer."""


def identity_counts():
    # All five usernames and both tenant codes belong to this fixture in its exclusive DB.
    # Product traffic can issue unrelated service tokens and audits while we are observing.
    return database("""WITH users AS (
        SELECT user_id FROM system.local_accounts WHERE normalized_username IN
        ('external-online-reserve','external-online-administrator','external-online-consumer',
         'external-online-source-initializer','external-online-engine-provisioner')
    ), tenants AS (
        SELECT id FROM system.tenants WHERE code IN ('external-online-reserve','external-online')
    ), oauth AS (
        SELECT service_principal_id FROM system.oauth_clients WHERE owner_tenant_id IN (SELECT id FROM tenants)
          AND display_name='redis-core-oauth-probe'
    ), actors AS (
        SELECT user_id FROM users UNION SELECT service_principal_id FROM oauth
    ), consumers AS (
        SELECT id FROM system.api_consumers WHERE tenant_id IN (SELECT id FROM tenants)
    )
    SELECT json_build_object(
        'users',(SELECT count(*) FROM users), 'tenants',(SELECT count(*) FROM tenants),
        'memberships',(SELECT count(*) FROM system.tenant_memberships WHERE tenant_id IN (SELECT id FROM tenants)),
        'assignments',(SELECT count(*) FROM system.role_assignments WHERE tenant_id IN (SELECT id FROM tenants)),
        'oauth_clients',(SELECT count(*) FROM oauth),
        'user_tokens',(SELECT count(*) FROM system.access_tokens t JOIN system.refresh_token_families f
                       ON t.family_id=f.id WHERE f.principal_id IN (SELECT user_id FROM users)),
        'audits',(SELECT count(*) FROM system.audit_logs WHERE principal_id IN (SELECT * FROM actors)
                  OR tenant_id IN (SELECT id FROM tenants) OR request_id LIKE 'external-online-%'),
        'consumers',(SELECT count(*) FROM consumers),
        'grants',(SELECT count(*) FROM system.api_consumer_service_grants WHERE api_consumer_id IN (SELECT id FROM consumers)),
        'credentials',(SELECT count(*) FROM system.api_consumer_credentials WHERE api_consumer_id IN (SELECT id FROM consumers))
    )""")


def identity_stage(stage, args, cwd, timeout, identity_path=None):
    artifact = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / f"redis-core-identity-{stage}.json"
    record = {"stage": stage, "limit_seconds": timeout, "exit_code": None, "result": "not_started",
              "run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"], "product_sha": s.PRODUCT_SHA}
    started = time.monotonic()
    try:
        if identity_path is not None:
            try:
                record["counts_before"] = identity_counts()
            except (OSError, ValueError, subprocess.SubprocessError, s.EvidenceError):
                record["observation_error"] = "pre_execution_counts_unavailable"
                raise s.EvidenceError(f"identity {stage}: baseline unavailable") from None
        started = time.monotonic()
        child = subprocess.Popen(args, cwd=cwd, env=dict(os.environ, GOWORK="off"),
                                 stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                                 stderr=subprocess.PIPE, start_new_session=True)
        try:
            _, stderr = child.communicate(timeout=timeout)
            record["result"] = "passed" if child.returncode == 0 else "failed"
        except subprocess.TimeoutExpired:
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass  # Exited between the deadline and the signal; still collect its status.
            _, stderr = child.communicate()
            record["result"] = "timeout"
        record.update(exit_code=child.returncode, elapsed_seconds=round(time.monotonic()-started, 3))
        if record["result"] != "passed":
            # Only known categories leave the private runner, never raw Go/DB errors.
            categories = ("context deadline exceeded", "connection refused", "permission denied",
                          "no space left on device", "already contains user principals", "create consumer role",
                          "create reserve tenant", "write identity", "no required module provides package")
            record["stderr_categories"] = [tag for tag in categories if tag.encode() in stderr.lower()]
            if stderr and not record["stderr_categories"]:
                record["stderr_categories"] = ["unclassified"]
            if identity_path is not None:
                record["identity_file_present"] = identity_path.is_file()
                try:
                    record["counts_after"] = identity_counts()
                except (OSError, ValueError, subprocess.SubprocessError, s.EvidenceError):
                    record["observation_error"] = "post_exit_counts_unavailable"
            failure = IdentityTimeout if record["result"] == "timeout" else s.EvidenceError
            raise failure(f"identity {stage}: {record['result']}")
    except OSError as error:
        record["os_errno"] = error.errno
        raise s.EvidenceError(f"identity {stage}: OS error") from None
    finally:
        record.setdefault("elapsed_seconds", round(time.monotonic()-started, 3))
        artifact.write_text(json.dumps(record, sort_keys=True) + "\n")


def build_identity():
    directory = Path(os.environ["RUNNER_TEMP"]) / "redis-http-identity/system/backend"
    identity_stage("build", ["go", "build", "-o", str(private() / "online-test-fixture"),
                             "./cmd/online-test-fixture"], directory, 900)


def fixture(mode, identity_path, query=0):
    directory = Path(os.environ["RUNNER_TEMP"]) / "redis-http-identity/system/backend"
    args = [str(identity_path.parent / "online-test-fixture"), "--suite", s.CONTRACT["suite"],
            "--output", str(identity_path)]
    if mode:
        args += ["--redis-consumer", mode]
        if query:
            args += ["--redis-query-id", str(query)]
    identity_stage(mode or "initial", args, directory, 180, identity_path)


def start():
    root = Path.cwd()
    s.pinned_product(root)
    secret = private()
    settings = {"redis_upstream": [os.environ.get("REDIS_HOST", "127.0.0.1"), int(os.environ["REDIS_PORT"])],
                "system_upstream": os.environ["SYSTEM_URL"], "service_upstream": os.environ["SERVICE_URL"],
                "system_paths": [["POST", s.TOKEN_PATH], ["GET", s.CONTEXT_PATH]],
                "service_paths": [["POST", "/api/query/redis-core-" + os.environ["ADDP_ONLINE_TEST_RUN_ID"] + "/query"]],
                "control": str(secret / "transport.sock"), "ready": str(secret / "transport.ready")}
    # The Query endpoint reaches Service with its original data-plane path.
    config = secret / "transport.json"; config.write_text(json.dumps(settings)); config.chmod(0o600)
    processes = []

    def spawn(args, name, environment):
        log = open(secret / (name + ".log"), "ab", buffering=0)
        try:
            child = subprocess.Popen(args, env=environment, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        finally: log.close()
        processes.append({"pid": child.pid, "name": name, "started": Path(f"/proc/{child.pid}/stat").read_text().rsplit(") ",1)[1].split()[19]})
        (secret / "processes.json").write_text(json.dumps(processes))
        return child

    harness = Path(__file__).resolve().parents[2]
    proxy = spawn([sys.executable, "-m", "scripts.test.redis-core-transport", str(config)], "transport",
                  dict(os.environ, PYTHONPATH=str(harness)))
    deadline = time.monotonic() + 10
    while not Path(settings["ready"]).exists():
        s.require(proxy.poll() is None and time.monotonic() < deadline, "private transport did not start")
        time.sleep(.05)
    ports = json.loads(Path(settings["ready"]).read_text())
    runtime = {"control": settings["control"], "redis_upstream": settings["redis_upstream"], "ports": ports,
               "gateway_url": os.environ["GATEWAY_URL"], "system_url": os.environ["SYSTEM_URL"], "service_url": os.environ["SERVICE_URL"]}
    (secret / "runtime.json").write_text(json.dumps(runtime))

    # Use the platform compiler, receiver and deployment CLI. No go build/run
    # temporary product server or altered product launcher is introduced.
    command(["make", "build", "BUILD_ARGS=--local --services system-backend,meta-backend,meta-worker,security-backend,security-worker,service-backend,gateway,runtime-log"], timeout=1200)
    outputs = list((root / "dist").glob("release-*"))
    matching = [p for p in outputs if (p / "system").is_file()]
    s.require(len(matching) == 1, "unique standard product artifact directory required")
    binaries = root / ".dev-bins"; binaries.mkdir(exist_ok=True)
    for name in ("system", "meta", "meta-worker", "security", "security-worker", "service", "gateway", "runtime-log"):
        (binaries / ("addp-" + name)).symlink_to(matching[0] / name)
    (root / ".dev-pids").mkdir(exist_ok=True)
    modules = ["system", "meta", "security", "service", "gateway"]
    publisher = [sys.executable, "scripts/prod/publish-module-declarations.py"]
    command([*publisher, "--prepare", *modules], timeout=60)

    def launch(name, extra=None):
        owner = name.removesuffix("-worker")
        role = "worker" if name.endswith("-worker") else ("ingress" if name == "gateway" else "backend")
        child = spawn([sys.executable, "scripts/dev/process-metrics-launch.py", name, owner, role,
                       str(binaries / "addp-runtime-log"), str(binaries / ("addp-" + name))], name,
                      dict(os.environ, **(extra or {})))
        (root / ".dev-pids" / (name + ".pid")).write_text(str(child.pid))
        return child

    def wait(origin, child):
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            s.require(child.poll() is None, "product process exited during preparation")
            try:
                response = s.request(origin, "GET", "/health/ready", timeout=2)
                if response.status == 200:
                    s.require(response.payload.get("git_commit") == s.PRODUCT_SHA, "product health SHA differs")
                    return
                s.require(response.status == 503, "unexpected preparation readiness")
            except (OSError, http.client.HTTPException): pass
            time.sleep(.2)
        raise s.EvidenceError("product preparation readiness timed out")

    system = launch("system", {"REDIS_HOST": "127.0.0.1", "REDIS_PORT": str(ports["system"])})
    wait(os.environ["SYSTEM_URL"], system)
    command([*publisher, "--provision", *modules], timeout=180)
    command([*publisher, *modules], timeout=180)
    for name, url in (("meta", os.environ["META_URL"]), ("security", os.environ["SECURITY_URL"]), ("service", os.environ["SERVICE_URL"])):
        extra = {"SERVICE_HOST": "127.0.0.1:" + str(ports["service_http"])} if name == "service" else {}
        child = launch(name, extra); wait(url, child)
        if name in {"meta", "security"}: launch(name + "-worker")
    gateway = launch("gateway", {"REDIS_HOST": "127.0.0.1", "REDIS_PORT": str(ports["gateway"]),
                                  "SYSTEM_URL": "http://127.0.0.1:" + str(ports["system_http"])})
    wait(os.environ["GATEWAY_URL"], gateway)
    # Dedicated fixture database is created by this disposable container, never
    # CREATE DATABASE on the local shared Infra.
    descriptor_password = secrets.token_hex(24)
    command(["docker", "run", "-d", "--name", SOURCE, "--label", "com.addp.online-fixture=infra-redis-core",
             "--label", "com.addp.online-run-id="+os.environ["ADDP_ONLINE_TEST_RUN_ID"],
             "--label", "com.docker.compose.project=addp-redis-core-fixture",
             "--label", "com.docker.compose.service=source-postgres",
             "-e", "POSTGRES_USER=postgres", "-e", f"POSTGRES_DB={SOURCE_DB}", "-e", "POSTGRES_PASSWORD",
             "-e", "POSTGRES_INITDB_ARGS=--auth-host=scram-sha-256",
             "-p", "127.0.0.1::5432", "addp-postgres-pgvector:latest"],
            env=dict(os.environ, POSTGRES_PASSWORD=descriptor_password), timeout=60)
    initialize_source(SOURCE, descriptor_password, Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]))
    mapping = command(["docker", "port", SOURCE, "5432/tcp"], timeout=5).decode().strip()
    descriptor = {"name": "Redis R1 source", "engine_type": "postgresql", "connection_info": {
        "host": "127.0.0.1", "port": int(mapping.rsplit(":", 1)[1]), "user": "postgres", "password": descriptor_password, "database": SOURCE_DB, "sslmode": "disable"}}
    (secret / "source.json").write_text(json.dumps(descriptor))
    fixture("", secret / "identity.json")


def accept():
    secret = private(); identity_path = secret / "identity.json"
    identity = json.loads(identity_path.read_text())
    runtime = json.loads((secret / "runtime.json").read_text())
    report = {"schema_version": "addp.online-suite/v1", "suite": s.CONTRACT["suite"], "result": "failed",
              "product_sha": s.PRODUCT_SHA, "harness_sha": os.environ["GITHUB_SHA"], "run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"]}
    owner = IdentityClient(os.environ["GATEWAY_URL"], identity["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"], 10)
    authorizer = IdentityClient(os.environ["GATEWAY_URL"], identity["ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"], 10)
    pending = []; query_id = None
    cleanup_failed = False
    try:
        checker = owner
        api.validate_user_identity(checker, int(identity["ADDP_ONLINE_TEST_TENANT_ID"]), set(s.CONTRACT["permissions"]))
        permissions = {key for role in checker.context["authorization"]["role_assignments"] for key in role["permissions"]}
        s.require(permissions == set(s.CONTRACT["permissions"]), "Query preparer permissions differ")
        engine = registration.register_engine(os.environ["GATEWAY_URL"], identity["ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN"], json.loads((secret / "source.json").read_text()))
        identity["ADDP_REDIS_HTTP_ENGINE_ID"] = str(engine)
        identity_path.write_text(json.dumps(identity))
        pg_auth.basic_scan(authorizer, engine, [TABLE])
        # Grant preparation is shared; the consumption proof belongs to Service.
        # Manager's current-source observation requires a different Permission.
        _, recipient = registration.source_grant_participants(authorizer, owner,
            int(identity["ADDP_ONLINE_TEST_TENANT_ID"]), scan_preparation=True)
        path = pg_auth.table_path(engine, TABLE)
        source = registration.read_exact_record_authorization_facts(authorizer, path)
        body = {"request_id": str(uuid4()), "catalog_path": path, "recipient_type": "user", "recipient_id": recipient,
            "action": "read", "expiry_mode": "until_revoked", "reason": "Redis R1 exact Service source preparation",
            "item_fingerprint": source["item_fingerprint"], "expected_boundary_hash": source["boundary_hash"]}
        pending.append((engine, body["request_id"]))
        report["source_preparation"] = {"engine_id": engine, "recipient_principal_id": recipient,
            "grant_request_id": body["request_id"], "grant_receipt_verified": False,
            "recipient_metadata_verified": False, "service_query_verified": False}
        receipt = authorizer.request("POST", f"/api/v1/system/engines/{engine}/access_grants", (201,), body).payload
        expected = {key: body[key] for key in ("request_id", "catalog_path", "recipient_type", "recipient_id", "action", "expiry_mode")}
        expected.update(engine_id=str(engine), approval_mode="independent", revocation=None, expires_at=None,
            item_fingerprint=source["item_fingerprint"], boundary_hash=source["boundary_hash"])
        s.require(isinstance(receipt, dict) and all(receipt.get(key) == value for key, value in expected.items())
            and receipt.get("node_recursive") is None and not any(receipt.get(key) for key in
                ("metadata_target", "file_composition", "scope")), "Service source Grant receipt differs")
        report["source_preparation"]["grant_receipt_verified"] = True
        items = owner.request("GET", f"/api/v1/meta/engines/{engine}/items?branch=public", (200,), response_type=list).payload
        matches = [item for item in items if item.get("full_name") == "public." + TABLE and item.get("item_type") == "table"]
        s.require(len(matches) == 1 and type(matches[0].get("id")) is int and matches[0]["id"] > 0
            and matches[0].get("fingerprint") == source["item_fingerprint"], "Service recipient DataItem differs")
        report["source_preparation"].update(recipient_metadata_verified=True, meta_item_id=matches[0]["id"])
        # Establish explicit protection before freezing the published definition;
        # only actual owner acknowledgement permits Query preparation.
        locator = pg_auth.locator(engine, TABLE)
        enrollment = owner.request("POST", "/api/v1/security/protection-enrollments", (201,), {"locator": locator}).payload
        enrollment_id = enrollment["id"]
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            current = owner.request("GET", "/api/v1/security/protection-enrollments/" + enrollment_id, (200,)).payload
            progresses = current.get("owner_progress", [])
            if current.get("state") == "active" and any(x.get("consumer_owner") == "service" and x.get("acknowledged") is True for x in progresses): break
            time.sleep(.2)
        else: raise s.EvidenceError("Service protection did not converge")
        sql = f"SELECT id,label FROM public.{TABLE}"
        contract = owner.request("POST", "/api/v1/service/sql/output-contract", (200,), {"engine_id": engine, "sql": sql}).payload
        service_name = "redis-core-" + os.environ["ADDP_ONLINE_TEST_RUN_ID"]
        query = owner.request("POST", "/api/v1/service/query", (201,), {
            "service_name": service_name, "title": "Redis R1 private probe", "config_type": "sql", "engine_id": engine,
            "sql_query": sql, "data_config": {"stable_key": ["id"], "default_fields": ["id", "label"], "filterable_fields": []},
            "output_contract": contract, "protocols": {"rest_api": {"enabled": True, "formats": ["json"]}}, "public_access": False, "max_features": 1}).payload
        query_id = int(query["id"])
        identity["ADDP_REDIS_HTTP_READER_ID"] = owner.context["principal"]["id"]
        identity["ADDP_REDIS_HTTP_SOURCE_ID"] = authorizer.context["principal"]["id"]
        identity_path.write_text(json.dumps(identity))
        # Warm the complete real read path using its ordinary publisher User,
        # before C exists. Runtime membership/authorization preparation cannot
        # be mistaken for a fault-induced change or consume C's first window.
        warmed=owner.request("POST", "/api/query/"+service_name+"/query", (200,),
            {"select":["id","label"],"order_by":[{"field":"id","direction":"asc"}],"page":{"limit":1},"format":"json"}).payload
        s.require(warmed.get("data")==[{"id":1,"label":"redis-r1-byte-baseline"}], "ordinary Query warmup differs")
        report["source_preparation"]["service_query_verified"] = True
        fixture("prepare", identity_path, query_id)
        identity = json.loads(identity_path.read_text())
        report["identities"]={"tenant_id":int(identity["ADDP_ONLINE_TEST_TENANT_ID"]),
            "oauth_principal_id":int(identity["ADDP_REDIS_HTTP_ACCOUNT_ID"]),"consumer_id":int(identity["ADDP_REDIS_HTTP_CONSUMER_ID"]),
            "query_service_id":query_id,"engine_id":engine}
        issued = s.oauth(runtime["gateway_url"], identity)
        s.require(issued.status == 200, "normal OAuth baseline failed")
        token = issued.payload["access_token"]
        s.require(issued.payload.get("expires_in", 0) >= 120 and not issued.payload.get("refresh_token"), "normal service Token lifetime differs")
        context = s.request(runtime["gateway_url"], "GET", s.CONTEXT_PATH, headers={"Authorization": "Bearer " + token})
        s.require(context.status == 200, "normal OAuth AuthContext failed")
        projection = s.token_projection(context.payload, identity)
        runtime |= {"query_path": "/api/query/" + service_name + "/query",
                    "query_body": {"select": ["id", "label"], "order_by": [{"field": "id", "direction": "asc"}], "page": {"limit": 1}, "format": "json"},
                    "expected_rows": [{"id": 1, "label": "redis-r1-byte-baseline"}]}
        redis = s.RedisObserver(runtime["redis_upstream"], os.environ["REDIS_PASSWORD"])
        report.update(s.run_window(runtime, identity, token, projection, redis, lambda: facts(identity), report))
    except Exception as error:
        cleanup_failed = isinstance(error, IdentityTimeout)
        report["error_code"] = str(error) if isinstance(error, s.EvidenceError) else "redis_http_preparation_failed"
        raise
    finally:
        if not cleanup_failed:
            try:
                if identity.get("ADDP_REDIS_HTTP_CONSUMER_ID"):
                    fixture("cleanup", identity_path)
                if query_id is not None: owner.request("DELETE", f"/api/v1/service/query/{query_id}", (200, 204))
                for engine, request_id in reversed(pending):
                    authorizer.request("POST", f"/api/v1/system/engines/{engine}/access_grants/{request_id}/revoke", (200,), {"reason": "Redis R1 owned acceptance cleanup"})
                if identity.get("ADDP_REDIS_HTTP_ACCOUNT_ID"): fixture("retire", identity_path)
            except Exception as error:
                cleanup_failed = True
                report["cleanup_error"] = str(error) if isinstance(error, IdentityTimeout) else "owner cleanup failed"
        report.setdefault("checks", {})["owner_cleanup"] = not cleanup_failed
        if all(report["checks"].get(name) is True for name in s.CONTRACT["checks"]): report["result"] = "passed"
        report["cleanup"] = "failed" if cleanup_failed else "passed"
        # IAM/Meta/enrollment/Engine histories are deliberately retained until
        # exact outer Infra destruction; do not claim logical engine deletion.
        report["remaining_fixture_history"] = "destroy_with_owned_infra"
        target = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "redis-core-http-evidence.json"
        if report["result"] == "passed": s.validate_report(report, os.environ["ADDP_ONLINE_TEST_RUN_ID"])
        target.write_text(json.dumps(report, sort_keys=True) + "\n")
        print(json.dumps(report, sort_keys=True))
        if cleanup_failed and "error_code" not in report:
            raise s.EvidenceError(report["cleanup_error"])


def recover():
    runtime=private()/"runtime.json"
    if runtime.exists():
        config=json.loads(runtime.read_text())
        for owner in ("system","gateway"):
            s.transport.control(config["control"], {"action":"fault","owner":owner,"available":True})


def stop():
    secret = private()
    errors=[]
    try: recover()
    except Exception: errors.append("transport_recovery_failed")
    processes=secret/"processes.json"
    if processes.exists():
        for row in reversed(json.loads(processes.read_text())):
            if row["name"]!="transport": continue
            pid=row["pid"]; stat=Path(f"/proc/{pid}/stat")
            def owned_alive():
                try: return stat.read_text().rsplit(") ",1)[1].split()[19] == row["started"]
                except FileNotFoundError: return False
            if stat.exists() and not owned_alive():
                errors.append("transport_identity_changed"); continue
            if owned_alive():
                try: os.kill(pid,signal.SIGTERM)
                except ProcessLookupError: pass
            deadline=time.monotonic()+10
            socket_path=secret/"transport.sock"
            while socket_path.exists() and time.monotonic()<deadline:time.sleep(.05)
            if socket_path.exists():
                if owned_alive():
                    os.kill(pid,signal.SIGKILL)
                    errors.append("transport_forced_exit")
                else: socket_path.unlink()
    # Even a recovery failure must not bypass precise outer destruction.
    existing=subprocess.run(["docker","container","inspect",SOURCE],capture_output=True)
    if existing.returncode==0:
        facts=json.loads(existing.stdout)[0]
        labels=facts["Config"]["Labels"]
        if labels.get("com.addp.online-fixture")!=s.CONTRACT["suite"] or labels.get("com.addp.online-run-id")!=os.environ["ADDP_ONLINE_TEST_RUN_ID"]:
            errors.append("foreign_source_container")
        else:
            try:
                volumes=[row["Name"] for row in facts.get("Mounts",[]) if row.get("Type")=="volume"]
                command(["docker","rm","-fv",SOURCE],timeout=20)
                for volume in volumes:
                    s.require(subprocess.run(["docker","volume","inspect",volume],capture_output=True).returncode!=0,"source volume remains")
                s.require(subprocess.run(["docker","container","inspect",SOURCE],capture_output=True).returncode!=0,"source container remains")
            except Exception:errors.append("source_cleanup_failed")
    s.require(not errors,"owned fixture cleanup failed: "+",".join(errors))


if __name__ == "__main__":
    import http.client
    os.umask(0o077)
    s.require(os.environ.get("GITHUB_ACTIONS") == "true" and os.environ.get("ADDP_ONLINE_HOSTED") == "1"
              and os.environ.get("ONLINE_SUITE") == s.CONTRACT["suite"] and os.environ.get("POSTGRES_DB") == "addp_online",
              "owned Hosted profile required")
    {"build-identity": build_identity, "start": start, "stop": stop, "recover": recover}[sys.argv[1]]()
