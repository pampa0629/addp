"""Deterministic observer rejection and opt-in real pinned PG/Redis wiring."""
from importlib import import_module
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import sys
import subprocess
import tempfile
import threading
import time
import unittest
from urllib.parse import unquote, urlsplit
from unittest.mock import patch

scenario = import_module("scripts.test.redis-core-http-online")
transport = import_module("scripts.test.redis-core-transport")
ROOT = Path(__file__).resolve().parents[2]


def port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def wait_file(path, process, timeout=180):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if path.exists(): return path.read_text()
        if process.poll() is not None: raise RuntimeError("pinned wiring child exited; private protocol log retained until cleanup")
        time.sleep(.05)
    raise RuntimeError("pinned wiring readiness timed out")


class RedisEvidenceTest(unittest.TestCase):
    def test_identical_hop_request_ids_are_one_identity_but_conflicts_fail(self):
        value="21d37f20-4187-4b9f-bca7-d27f9327d49f"
        self.assertEqual(scenario.response_request_id(value+", "+value),value)
        with self.assertRaises(scenario.EvidenceError):scenario.response_request_id(value+", 0bf36b70-6c1a-4bf2-a46a-a18f7dfba181")

    def test_count_does_not_hide_reset_or_expiry(self):
        baseline = {"count": 1, "pttl_ms": 59000, "expires_at_ms": 1700000060000}
        expiry = scenario.assert_sample(baseline, 1)
        for row in (baseline | {"count": 2}, baseline | {"expires_at_ms": expiry + 1}, baseline | {"pttl_ms": -2}):
            with self.assertRaises(scenario.EvidenceError): scenario.assert_sample(row, 1, expiry)

    def test_refusal_must_be_exact_readiness_contract(self):
        for status, payload in ((200, {}), (503, {"error_code": "rate_limit_unavailable"}), (429, {})):
            with self.assertRaises(scenario.EvidenceError): scenario.assert_unavailable(scenario.Response(status, payload, ""))
        scenario.assert_unavailable(scenario.Response(503, {"error_code": "module_not_ready"}, ""))

    def test_request_counter_requires_complete_provenance(self):
        identity = "e1f68921-5773-4dcb-99b6-7e2d578ca531"
        response = scenario.Response(503, {}, identity)
        record = {"received": 1, "forwarded": 1, "completed": 1, "status": 503}
        scenario.assert_forwarded({"system": {"valid": True, "records": {identity: record}}}, "system", response, 1)
        for snap, count in (({"valid": False, "records": {}}, 0), ({"valid": True, "records": {identity: record}}, 0),
                            ({"valid": True, "records": {identity: record | {"forwarded": 2}}}, 1)):
            with self.assertRaises(scenario.EvidenceError): scenario.assert_forwarded({"system": snap}, "system", response, count)

    def test_token_projection_rejects_extra_permission_or_replacement(self):
        identity = {"ADDP_REDIS_HTTP_ACCOUNT_ID": "8", "ADDP_ONLINE_TEST_TENANT_ID": "2", "ADDP_REDIS_HTTP_CLIENT_ID": "probe", "ADDP_REDIS_HTTP_MEMBERSHIP_ID": "18"}
        context = {"schema_version":"addp.auth_context/v1", "principal": {"type": "service_principal", "id": "8"},
                   "context": {"type": "tenant", "tenant_id": "2", "tenant_membership_id":"18"},
                   "client": {"client_id": "probe", "scopes": [], "scope_mode": "unrestricted", "audiences": ["addp.api"]},
                   "token": {"type": "service_access_token", "issued_at": "a", "expires_at": "b"},
                   "authorization": {"role_assignments": []}}
        before = scenario.token_projection(context, identity)
        replacement = context | {"token": context["token"] | {"issued_at": "later"}}
        self.assertNotEqual(scenario.token_projection(replacement, identity), before)
        with self.assertRaises(scenario.EvidenceError): scenario.token_projection(context | {"authorization": {"role_assignments": [{}]}}, identity)


class RedisWindowTest(unittest.TestCase):
    def exercise(self, failure=None):
        from uuid import uuid4
        import copy
        identity={"ADDP_REDIS_HTTP_CONSUMER_ID":"9","ADDP_REDIS_HTTP_CONSUMER_KEY":"private",
            "ADDP_REDIS_HTTP_ACCOUNT_ID":"8","ADDP_ONLINE_TEST_TENANT_ID":"2","ADDP_REDIS_HTTP_CLIENT_ID":"probe", "ADDP_REDIS_HTTP_MEMBERSHIP_ID":"18"}
        context={"schema_version":"addp.auth_context/v1", "principal":{"id":"8","type":"service_principal"},"context":{"type":"tenant","tenant_id":"2","tenant_membership_id":"18"},
            "client":{"client_id":"probe","audiences":["addp.api"],"scopes":[],"scope_mode":"unrestricted"},
            "token":{"type":"service_access_token","issued_at":"a","expires_at":"b"},"authorization":{"role_assignments":[]}}
        config={"gateway_url":"http://127.0.0.1:1","system_url":"http://127.0.0.1:2","service_url":"http://127.0.0.1:3",
            "control":"private.sock","query_path":"/query","query_body":{},"expected_rows":[{"id":1}]}
        state={"gateway":True,"system":True}; records={"system":{"valid":True,"records":{}},"service":{"valid":True,"records":{}}}
        queries=iter((200,503,200,429)); sample_index=0
        def response(status,payload,owner=None):
            value=scenario.Response(status,payload,str(uuid4()))
            if owner:
                records[owner]["records"][value.request_id]={"received":1,"forwarded":1,"completed":1,"status":status}
            return value
        def request(origin,method,path,**kwargs):
            owner="gateway" if origin==config["gateway_url"] else "system"
            if path=="/health/live": return response(200,{"module":owner,"instance_id":owner})
            if path=="/health/ready":
                if origin==config["service_url"]: return response(200,{"module":"service"})
                good=state[owner]
                checks=[{"name":"redis","status":"ready" if good else "not_ready","error_code":"redis_unavailable"}]
                checks += [{"name":name,"status":"ready"} for name in (("system_registry_snapshot",) if owner=="gateway" else ("local_dependencies","iam_bootstrap"))]
                return response(200 if good else 503,{"module":owner,"checks":checks})
            if path==scenario.CONTEXT_PATH:
                projected=context if failure!="token" else context|{"token":context["token"]|{"issued_at":"new"}}
                return response(200,projected,"system")
            status=next(queries)
            if status==200: return response(status,{"data":[{"id":1}],"page":{"has_more":False},"service_version":"v1"},"service")
            if status==429: return response(status,{"error":"Rate limit exceeded","limit":2,"window":"1 minute"})
            return response(status,{"error_code":"rate_limit_unavailable" if failure=="refusal" else "module_not_ready"})
        def oauth(*args): return response(503,{"error_code":"module_not_ready"},"system" if state["gateway"] else None)
        def control(path,value):
            if value["action"]=="fault": state[value["owner"]]=value["available"];return {}
            answer=copy.deepcopy(records)
            if failure=="counter":answer["service"]["valid"]=False
            return answer
        class Redis:
            def sample(self,*_):
                nonlocal sample_index
                counts=(None,1,1,1,2,3);count=counts[sample_index];sample_index+=1
                return {"count":count,"pttl_ms":50000,"expires_at_ms":100000+(1 if failure=="expiry" and sample_index==4 else 0)}
            def identity(self):return "same"
        with patch.object(scenario,"request",request),patch.object(scenario,"oauth",oauth),patch.object(transport,"control",control):
            if failure=="deadline":
                with patch.object(scenario.time,"monotonic",side_effect=(0,36)):
                    return scenario.run_window(config,identity,"original",scenario.token_projection(context,identity),Redis(),lambda:{"stable":True})
            return scenario.run_window(config,identity,"original",scenario.token_projection(context,identity),Redis(),lambda:{"stable":True})

    def test_all_five_stages_and_archive(self):
        result=self.exercise()
        result.update(schema_version="addp.online-suite/v1",suite="infra-redis-core",product_sha=scenario.PRODUCT_SHA,
                      run_id="one",result="passed",cleanup="passed",harness_sha="f"*40,
                      identities={"tenant_id":2,"oauth_principal_id":8,"consumer_id":9,"query_service_id":10,"engine_id":11},
                      source_preparation={"engine_id":11,"recipient_principal_id":"7","meta_item_id":19,
                          "grant_request_id":"e1f68921-5773-4dcb-99b6-7e2d578ca531", "grant_receipt_verified":True,
                          "recipient_metadata_verified":True,"service_query_verified":True})
        result["checks"]["owner_cleanup"]=True
        scenario.validate_report(result,"one")
        for mutate in (lambda x:x["checks"].pop("system_outage"),lambda x:x.update(elapsed_seconds=36),
                       lambda x:x["window"][3].update(expires_at_ms=200000),lambda x:x["requests"].pop(),
                       lambda x:x.update(product_sha="other"),lambda x:x.update(cleanup="failed"),
                       lambda x:x.pop("source_preparation"),lambda x:x["source_preparation"].update(service_query_verified=False),
                       lambda x:x["source_preparation"].update(recipient_metadata_verified="true"),
                       lambda x:x["source_preparation"].update(engine_id=12)):
            import copy
            invalid=copy.deepcopy(result);mutate(invalid)
            with self.assertRaises(scenario.EvidenceError):scenario.validate_report(invalid,"one")

    def test_changed_token_window_counter_refusal_or_deadline_cannot_pass(self):
        for failure in ("token","expiry","counter","refusal","deadline"):
            with self.subTest(failure=failure),self.assertRaises(scenario.EvidenceError):self.exercise(failure)


class RedisTransportTest(unittest.TestCase):
    def test_actual_http_bytes_headers_and_counter(self):
        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                content = self.rfile.read(int(self.headers["Content-Length"]))
                self.server.received = (content, self.headers["Authorization"])
                self.send_response(201)
                self.send_header("X-Request-ID", self.headers["X-Request-ID"])
                self.send_header("Set-Cookie", "fixture=private; HttpOnly")
                self.end_headers(); self.wfile.write(content)
            def log_message(self, *_): pass
        upstream = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        observer = transport.HTTPObserver(f"http://127.0.0.1:{upstream.server_port}", [("POST", "/owned")])
        for server in (upstream, observer):
            threading.Thread(target=server.serve_forever, daemon=True).start()
            self.addCleanup(server.server_close); self.addCleanup(server.shutdown)
        identifier = "0bf36b70-6c1a-4bf2-a46a-a18f7dfba181"
        response = scenario.request(f"http://127.0.0.1:{observer.server_port}", "POST", "/owned",
            headers={"X-Request-ID": identifier, "Authorization": "Bearer private"}, body=b'{"bytes":"\\u0000\\u00ff"}')
        self.assertEqual(response.status, 201)
        self.assertEqual(upstream.received[1], "Bearer private")
        self.assertEqual(upstream.received[0], b'{"bytes":"\\u0000\\u00ff"}')
        self.assertEqual(response.payload, {"bytes":"\x00ÿ"})
        view = observer.snapshot()
        self.assertNotIn("private", json.dumps(view))
        scenario.assert_forwarded({"system": view}, "system", response, 1)

    def test_fault_closes_existing_connections_and_only_recovers_original_endpoint(self):
        class Echo(__import__('socketserver').BaseRequestHandler):
            def handle(self):
                try:
                    while data := self.request.recv(1024): self.request.sendall(data)
                except OSError: pass
        upstream = __import__('socketserver').ThreadingTCPServer(("127.0.0.1", 0), Echo)
        upstream.daemon_threads = True
        proxy = transport.FaultProxy(upstream.server_address)
        for server in (upstream, proxy):
            threading.Thread(target=server.serve_forever, daemon=True).start()
            self.addCleanup(server.server_close); self.addCleanup(server.shutdown)
        self.addCleanup(proxy.set_available, False)
        with socket.create_connection(proxy.server_address, timeout=2) as connection:
            connection.sendall(b"original"); self.assertEqual(connection.recv(16), b"original")
            proxy.set_available(False)
            try: self.assertEqual(connection.recv(16), b"")
            except ConnectionResetError: pass
        with socket.create_connection(proxy.server_address, timeout=2) as connection:
            try: self.assertEqual(connection.recv(16), b"")
            except ConnectionResetError: pass
        proxy.set_available(True)
        with socket.create_connection(proxy.server_address, timeout=2) as connection:
            connection.sendall(b"same"); self.assertEqual(connection.recv(16), b"same")


class RedisRegistrationTest(unittest.TestCase):
    def test_pinned_product_and_identity_are_separate_and_manual_only(self):
        workflow=(ROOT/".github/workflows/online-t4-gates.yml").read_text()
        job=workflow.split("  infra-redis-core-hosted:\n",1)[1].split("\n  infra-object-storage-hosted:\n",1)[0]
        self.assertIn("github.event_name == 'workflow_dispatch' && inputs.suite == 'infra-redis-core'",job)
        self.assertIn('redis-http-identity" apply ',job)
        self.assertNotIn('redis-http-product" apply ',job)
        self.assertIn('redis-core-identity.go',job)
        entry=(ROOT/"scripts/test/online-hosted-redis-core-gate.sh").read_text()
        self.assertIn('"${REDIS_HTTP_SOURCE_COMMIT:-}" == "$PINNED"',entry)
        self.assertIn('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',entry)
        self.assertNotIn('keepalive.sh',entry)
        gate=import_module("scripts.test.online-gate")
        self.assertFalse(gate.SUITES["infra-redis-core"].nightly)
        self.assertIn('"infra-redis-core": "redis-http-product"',(ROOT/"scripts/test/online-gate.py").read_text())


class RedisCleanupTest(unittest.TestCase):
    def execute_cleanup(self, foreign=False):
        from types import SimpleNamespace
        preparation=import_module("scripts.test.redis-core-http-preparation")
        deleted=[]
        def docker(args,**kwargs):
            if args[1:3]==["volume","inspect"]:return SimpleNamespace(returncode=1,stdout=b"")
            if deleted:return SimpleNamespace(returncode=1,stdout=b"")
            labels={"com.addp.online-fixture":"infra-redis-core","com.addp.online-run-id":"wrong" if foreign else "one"}
            return SimpleNamespace(returncode=0,stdout=json.dumps([{"Config":{"Labels":labels},"Mounts":[{"Type":"volume","Name":"owned-data"}]}]).encode())
        def command(args,**kwargs):deleted.append(args);return b""
        with tempfile.TemporaryDirectory() as directory,patch.dict(os.environ,{"ADDP_ONLINE_SECRET_DIR":directory,"ADDP_ONLINE_TEST_RUN_ID":"one"}),patch.object(preparation,"recover",side_effect=RuntimeError("expected recovery fault")),patch.object(preparation.subprocess,"run",docker),patch.object(preparation,"command",command):
            with self.assertRaises(scenario.EvidenceError):preparation.stop()
        return deleted

    def test_recovery_failure_still_destroys_exact_owned_source_and_cannot_pass(self):
        self.assertEqual(self.execute_cleanup(),[["docker","rm","-fv","addp-redis-core-source"]])

    def test_foreign_run_is_never_destroyed(self):
        self.assertEqual(self.execute_cleanup(True),[])


class RedisIdentityStageTest(unittest.TestCase):
    def test_build_once_before_infra_and_reuse_for_all_operations(self):
        preparation = import_module("scripts.test.redis-core-http-preparation")
        entry = (ROOT / "scripts/test/online-hosted-redis-core-gate.sh").read_text()
        self.assertEqual(entry.count(" build-identity\n"), 1)
        self.assertLess(entry.index(" build-identity\n"), entry.index("infra_owned=1"))
        with tempfile.TemporaryDirectory() as tmp, patch.dict(os.environ, {
                "RUNNER_TEMP": tmp, "ADDP_ONLINE_SECRET_DIR": tmp}), patch.object(preparation, "identity_stage") as stage:
            identity = Path(tmp) / "identity.json"
            preparation.build_identity()
            for mode in ("", "prepare", "cleanup", "retire"):
                preparation.fixture(mode, identity, 11 if mode == "prepare" else 0)
        calls = [c.args for c in stage.call_args_list]
        binary = str(Path(tmp) / "online-test-fixture")
        self.assertEqual(calls[0][:2], ("build", ["go", "build", "-o", binary, "./cmd/online-test-fixture"]))
        self.assertEqual([c[0] for c in calls], ["build","initial","prepare","cleanup","retire"])
        self.assertEqual([c[3] for c in calls], [900, 180, 180, 180, 180])
        for _, args, cwd, _, path in calls[1:]:
            self.assertEqual(args[0], binary)
            self.assertEqual(cwd, calls[0][2])
            self.assertEqual(path, identity)
        self.assertEqual(calls[2][1][-4:], ["--redis-consumer","prepare","--redis-query-id","11"])

    def test_timeout_reaps_writer_before_observation_and_never_exports_raw_stderr(self):
        preparation = import_module("scripts.test.redis-core-http-preparation")
        secret = secrets.token_hex(12)
        with tempfile.TemporaryDirectory() as tmp, patch.dict(os.environ, {
                "ADDP_ONLINE_ARTIFACT_DIR": tmp, "ADDP_ONLINE_TEST_RUN_ID": "test-timeout"}):
            root = Path(tmp)
            late = "import time;from pathlib import Path;time.sleep(2);Path('late').touch()"
            code = ("import os,sys,time,subprocess; from pathlib import Path; "
                    "Path('pid').write_text(str(os.getpid())); "
                    f"subprocess.Popen([sys.executable,'-c',{late!r}]); "
                    "print(sys.argv[1]+' permission denied',file=sys.stderr,flush=True);time.sleep(30)")
            def observe():
                if (root / "pid").exists():
                    with self.assertRaises(ProcessLookupError):
                        os.kill(int((root / "pid").read_text()), 0)
                    return {"users":1}
                return {"users":0}
            with patch.object(preparation, "identity_counts", side_effect=observe):
                with self.assertRaises(preparation.IdentityTimeout):
                    preparation.identity_stage("initial", [sys.executable,"-c",code,secret], root, 1, root/"identity.json")
            raw = (root/"redis-core-identity-initial.json").read_text()
            record = json.loads(raw)
            self.assertEqual((record["result"],record["exit_code"]), ("timeout",-9))
            self.assertEqual((record["counts_before"],record["counts_after"]), ({"users":0},{"users":1}))
            self.assertFalse(record["identity_file_present"])
            self.assertNotIn(secret,raw)
            self.assertEqual(record["stderr_categories"],["permission denied"])
            time.sleep(1.2)
            self.assertFalse((root/"late").exists())

    def test_build_exit_and_failed_observation_keep_original_outcome(self):
        preparation = import_module("scripts.test.redis-core-http-preparation")
        with tempfile.TemporaryDirectory() as tmp, patch.dict(os.environ, {
                "ADDP_ONLINE_ARTIFACT_DIR": tmp, "ADDP_ONLINE_TEST_RUN_ID": "test-records"}):
            root = Path(tmp)
            preparation.identity_stage("build", [sys.executable,"-c","pass"],root,10)
            built = json.loads((root/"redis-core-identity-build.json").read_text())
            self.assertEqual((built["result"],built["exit_code"]),("passed",0))
            with patch.object(preparation,"identity_counts",side_effect=[{"users":0},scenario.EvidenceError("db read failed")]):
                with self.assertRaises(scenario.EvidenceError):
                    preparation.identity_stage("initial",[sys.executable,"-c","import sys;sys.exit(7)"],root,10,root/"identity.json")
            failed = json.loads((root/"redis-core-identity-initial.json").read_text())
            self.assertEqual((failed["exit_code"],failed["observation_error"]),(7,"post_exit_counts_unavailable"))


class RedisSourceDatabaseTest(unittest.TestCase):
    def test_probe_retries_only_reads_and_seed_failure_is_archived_once(self):
        preparation = import_module("scripts.test.redis-core-http-preparation")
        password = secrets.token_hex(24)
        results = [subprocess.CompletedProcess([], 2, b"", b"connection refused"),
                   subprocess.CompletedProcess([], 0, b"redis_core_fixture\n", b""),
                   subprocess.CompletedProcess([], 3, b"", f"ERROR: duplicate relation {password}".encode())]
        with tempfile.TemporaryDirectory() as tmp, patch.object(preparation.subprocess, "run", side_effect=results) as run, \
                patch.object(preparation.time, "sleep"):
            with self.assertRaises(scenario.EvidenceError) as failure:
                preparation.initialize_source("owned", password, Path(tmp))
            record = json.loads((Path(tmp) / "redis-core-source-error.json").read_text())
        self.assertEqual(json.loads(str(failure.exception)), record)
        self.assertEqual(record, {"stage": "source_seed", "reason": "command_failed", "exit_code": 3,
                                  "stderr": "ERROR: duplicate relation [redacted]"})
        self.assertEqual(run.call_count, 3)
        for call in run.call_args_list:
            args = call.args[0]
            self.assertNotIn(password, " ".join(args))
            self.assertEqual(args[args.index("-h") + 1], "127.0.0.1")
            self.assertEqual(call.kwargs["env"]["PGPASSWORD"], password)
        self.assertEqual([c.kwargs["input"] is not None for c in run.call_args_list], [False, False, True])

    def test_readiness_failures_cannot_seed(self):
        preparation = import_module("scripts.test.redis-core-http-preparation")
        for result, reason, code in (
            (subprocess.CompletedProcess([], 0, b"postgres\n", b""), "unexpected_database", 0),
            (subprocess.CompletedProcess([], 2, b"", b"refused"), "readiness_timeout", 2),
            (subprocess.TimeoutExpired(["psql"], 5), "command_timeout", None),
        ):
            with self.subTest(reason=reason), tempfile.TemporaryDirectory() as tmp, \
                    patch.object(preparation.time, "monotonic", side_effect=(0, 61)), \
                    patch.object(preparation.subprocess, "run", side_effect=[result]) as run:
                with self.assertRaises(scenario.EvidenceError):
                    preparation.initialize_source("owned", "fixture-secret", Path(tmp))
                record = json.loads((Path(tmp) / "redis-core-source-error.json").read_text())
                self.assertEqual((record["stage"], record["reason"], record["exit_code"]),
                                 ("source_readiness", reason, code))
                self.assertIsNone(run.call_args.kwargs["input"])
                run.assert_called_once()


def verify_source_cold_start():
    """The Compose entrypoint barrier holds the temporary socket-only PG server."""
    preparation = import_module("scripts.test.redis-core-http-preparation")
    container = os.environ["ADDP_REDIS_CORE_SOURCE_ID"]
    password = os.environ["BUSINESS_REDIS_ADMIN_PASSWORD"]
    docker = ["docker", "exec", container]
    tcp = ["docker", "exec", "-e", "PGPASSWORD", container, "psql", "-XqwAt", "-h", "127.0.0.1",
           "-U", "postgres", "-d", preparation.SOURCE_DB, "-c"]
    deadline = time.monotonic() + 60
    while subprocess.run([*docker, "pg_isready", "-U", "postgres", "-d", preparation.SOURCE_DB],
                         capture_output=True, timeout=5).returncode:
        scenario.require(time.monotonic() < deadline, "temporary PG socket readiness timed out")
        time.sleep(.2)
    env = dict(os.environ, PGPASSWORD=password)
    temporary = subprocess.run([*tcp, "SELECT current_database()"], env=env, capture_output=True, timeout=5)
    scenario.require(temporary.returncode == 2 and b"Connection refused" in temporary.stderr,
                     "temporary PG must accept the socket while refusing TCP")
    subprocess.run([*docker, "touch", "/tmp/release-init"], check=True, timeout=5)
    with tempfile.TemporaryDirectory() as tmp:
        preparation.initialize_source(container, password, Path(tmp))
    rejected = subprocess.run([*tcp, "SELECT current_database()"], env=dict(env, PGPASSWORD="incorrect"),
                              capture_output=True, timeout=5)
    scenario.require(rejected.returncode == 2 and b"password authentication failed" in rejected.stderr,
                     "final source TCP connection must require the PG credential")
    rows = subprocess.run([*tcp, f"SELECT id,label FROM public.{preparation.TABLE} ORDER BY id"], env=env,
                          capture_output=True, check=True, timeout=5)
    scenario.require(rows.stdout.splitlines() == [b"1|redis-r1-byte-baseline"], "source seed rows differ")
    print("Redis R1 source PG cold start: socket=0, temporary TCP=2 (refused), "
          "authenticated database=redis_core_fixture, wrong password=2, seed rows=1")


class RedisSourcePreparationTest(unittest.TestCase):
    def prepare(self, fault=None):
        """Exercise the real preparer up to Consumer creation, with owner APIs simulated."""
        import copy
        import hashlib
        preparation = import_module("scripts.test.redis-core-http-preparation")
        calls, commands = [], []
        fingerprint = hashlib.sha256(f"3:public.{preparation.TABLE}".encode()).hexdigest()
        boundary = "a" * 64
        issued = None

        def request(client, method, path, expected, body=None, **kwargs):
            nonlocal issued
            owner = client.token == "owner"
            calls.append(("owner" if owner else "authorizer", method, path))
            status, payload = 200, {}
            if path == scenario.CONTEXT_PATH:
                permissions = list(scenario.CONTRACT["permissions"]) if owner else sorted(
                    preparation.registration.SOURCE_INITIALIZER_PERMISSIONS | {"meta.scan_task.execute", "meta.scan_task.read"})
                if owner and fault == "extra_permission": permissions.append("manager.data_item.read")
                payload = {"principal": {"type": "user", "id": "7" if owner else "8"},
                    "context": {"type": "tenant", "tenant_id": "2"}, "token": {"type": "first_party_access_token"},
                    "authorization": {"role_assignments": [{"role_key": "redis-query-preparer" if owner else
                        preparation.registration.SOURCE_INITIALIZER_ROLE, "permissions": permissions}]}}
            elif path.endswith("/authorization-facts"):
                self.assertFalse(owner)
                payload = {"schema_version": "meta.data_item_authorization_facts/v1", "engine_id": "3",
                    "item_fingerprint": fingerprint, "item_type": "table", "full_name": "public." + preparation.TABLE,
                    "basis_state": "complete", "boundary_hash": boundary, "observed_at": "now",
                    "boundary": {"layout": "single", "native_target": preparation.pg_auth.table_path(3, preparation.TABLE)}}
            elif path == "/api/v1/system/engine-access/read-observations/current-source":
                # Reproduce the original product boundary: this User has no Manager Permission.
                status, payload = 403, {"error_code": "permission_denied"}
            elif path == "/api/v1/system/engines/3/access_grants":
                self.assertFalse(owner)
                self.assertEqual(body["catalog_path"], preparation.pg_auth.table_path(3, preparation.TABLE))
                self.assertEqual((body["recipient_id"], body["action"], body["expiry_mode"]), ("7", "read", "until_revoked"))
                issued = body["request_id"]
                status, payload = 201, copy.deepcopy(body)
                payload.update(engine_id="3", approval_mode="independent", revocation=None,
                    item_fingerprint=fingerprint, boundary_hash=boundary)
                if fault == "wrong_recipient": payload["recipient_id"] = "9"
                if fault == "wider_grant": payload["node_recursive"] = True
            elif "/access_grants/" in path and path.endswith("/revoke"):
                self.assertFalse(owner)
                self.assertEqual(path, f"/api/v1/system/engines/3/access_grants/{issued}/revoke")
                if fault == "cleanup": status = 503
            elif path == "/api/v1/meta/engines/3/items?branch=public":
                self.assertTrue(owner)
                self.assertIsNotNone(issued)
                self.assertIs(kwargs.get("response_type"), list)
                payload = [{"id": 19, "item_type": "table", "full_name": "public." + preparation.TABLE,
                    "fingerprint": "wrong" if fault == "wrong_metadata" else fingerprint}]
                if fault == "duplicate_metadata": payload += copy.deepcopy(payload)
            elif path == "/api/v1/security/protection-enrollments": status, payload = 201, {"id": "protection"}
            elif path.endswith("/protection-enrollments/protection"):
                payload = {"state": "active", "owner_progress": [{"consumer_owner": "service", "acknowledged": True}]}
            elif path == "/api/v1/service/sql/output-contract": payload = {"table": {"fields": []}}
            elif path == "/api/v1/service/query": status, payload = 201, {"id": 11}
            elif path == "/api/query/redis-core-source-preparation/query":
                self.assertTrue(owner)
                payload = {"data": [{"id": 1, "label": "wrong" if fault == "query_bytes" else "redis-r1-byte-baseline"}]}
                if fault == "query_denied": status, payload = 403, {"error_code": "permission_denied"}
            elif method == "DELETE" and path == "/api/v1/service/query/11": pass
            else: self.fail("unexpected preparation API: " + path)
            if status not in expected: raise preparation.api.SuiteError(f"{method} {path} returned HTTP {status} (permission_denied)")
            return preparation.api.Response(status, payload)

        class ConsumerBoundary(Exception): pass
        def fixture(mode, *args):
            commands.append(mode)
            if mode == "prepare" and fault == "identity_timeout": raise preparation.IdentityTimeout("identity prepare: timeout")
            if mode == "prepare": raise ConsumerBoundary("stop before Consumer/OAuth/window")

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "identity.json").write_text(json.dumps({"ADDP_ONLINE_TEST_TENANT_ID": "2",
                "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "owner", "ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN": "authorizer",
                "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN": "provisioner", "ADDP_REDIS_HTTP_ACCOUNT_ID": "9"}))
            for name in ("runtime.json", "source.json"): (root / name).write_text("{}")
            with patch.dict(os.environ, {"ADDP_ONLINE_SECRET_DIR": directory, "ADDP_ONLINE_ARTIFACT_DIR": directory,
                "ADDP_ONLINE_TEST_RUN_ID": "source-preparation", "GITHUB_SHA": "f" * 40, "GATEWAY_URL": "http://127.0.0.1:1"}), \
                patch.object(preparation.api.GatewayClient, "request", request), \
                patch.object(preparation.registration, "register_engine", return_value=3), \
                patch.object(preparation.pg_auth, "basic_scan"), patch.object(preparation, "fixture", fixture), \
                patch("builtins.print"):
                with self.assertRaises((ConsumerBoundary, scenario.EvidenceError, preparation.api.SuiteError,
                    preparation.registration.RegistrationError)):
                    preparation.accept()
            report = json.loads((root / "redis-core-http-evidence.json").read_text())
        return report, calls, commands

    def test_service_preparer_reaches_consumer_without_manager_permission(self):
        report, calls, commands = self.prepare()
        self.assertEqual(commands, ["prepare", "retire"])
        self.assertFalse(any("read-observations" in path for _, _, path in calls))
        self.assertEqual({k: report["source_preparation"][k] for k in
            ("grant_receipt_verified", "recipient_metadata_verified", "service_query_verified")},
            dict(grant_receipt_verified=True, recipient_metadata_verified=True, service_query_verified=True))
        self.assertEqual(report["source_preparation"]["meta_item_id"], 19)
        self.assertEqual(report["result"], "failed")  # Stopping before A–E can never pass T4.
        self.assertEqual(report["cleanup"], "passed")

    def test_wrong_or_wider_grant_is_rejected_and_revoked(self):
        for fault in ("wrong_recipient", "wider_grant"):
            with self.subTest(fault=fault):
                report, calls, commands = self.prepare(fault)
                self.assertNotIn("prepare", commands)
                self.assertFalse(any("/items?" in path for _, _, path in calls))
                self.assertTrue(any(path.endswith("/revoke") for _, _, path in calls))
                self.assertEqual(report["cleanup"], "passed")

    def test_wrong_or_duplicate_recipient_metadata_prevents_service_creation(self):
        for fault in ("wrong_metadata", "duplicate_metadata"):
            with self.subTest(fault=fault):
                report, calls, commands = self.prepare(fault)
                self.assertNotIn("prepare", commands)
                self.assertFalse(any(path == "/api/v1/service/query" for _, _, path in calls))
                self.assertEqual(report["cleanup"], "passed")

    def test_receipt_and_output_contract_cannot_hide_wrong_query_bytes(self):
        for fault in ("query_bytes", "query_denied"):
            with self.subTest(fault=fault):
                report, _, commands = self.prepare(fault)
                self.assertNotIn("prepare", commands)
                self.assertTrue(report["source_preparation"]["grant_receipt_verified"])
                self.assertTrue(report["source_preparation"]["recipient_metadata_verified"])
                self.assertFalse(report["source_preparation"]["service_query_verified"])
                self.assertEqual(report["result"], "failed")

    def test_extra_manager_permission_fails_before_grant(self):
        _, calls, commands = self.prepare("extra_permission")
        self.assertFalse(any(path.endswith("/access_grants") for _, _, path in calls))
        self.assertNotIn("prepare", commands)

    def test_consumer_timeout_goes_directly_to_outer_destruction(self):
        report, calls, commands = self.prepare("identity_timeout")
        self.assertEqual(commands, ["prepare"])
        self.assertEqual(report["error_code"], "identity prepare: timeout")
        self.assertEqual((report["result"], report["cleanup"]), ("failed","failed"))
        self.assertFalse(any(method == "DELETE" or path.endswith("/revoke") for _, method, path in calls))

    def test_failed_grant_revocation_cannot_report_cleanup_success(self):
        report, _, _ = self.prepare("cleanup")
        self.assertEqual(report["cleanup"], "failed")
        self.assertFalse(report["checks"]["owner_cleanup"])


class RedisPinnedFixtureTest(unittest.TestCase):
    def test_fixed_identity_compiles_and_respects_real_catalog(self):
        self.execute(False)

    def execute(self, real):
        with tempfile.TemporaryDirectory(prefix="redis-r1-pinned-") as directory:
            root = Path(directory); checkout = root / "redis-http-identity"
            checkout_started = time.monotonic()
            subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), str(checkout)], check=True, capture_output=True)
            if not real:
                # The pinned System fixture builds against Common. Keep every owner's
                # authorization directory so the Go catalog still discovers the full set.
                subprocess.run(["git", "sparse-checkout", "set", "--no-cone", "/system/backend/",
                                "/common/", "/*/authorization/"], cwd=checkout, check=True, capture_output=True)
            subprocess.run(["git", "checkout", "--detach", scenario.PRODUCT_SHA], cwd=checkout, check=True, capture_output=True)
            checkout_seconds = time.monotonic() - checkout_started
            subprocess.run(["git", "apply", "--unidiff-zero", str(ROOT / "scripts/test/redis-core-identity.patch")], cwd=checkout, check=True, capture_output=True)
            for source, destination in (("redis-core-identity.go", "cmd/online-test-fixture/redis_core_fixture.go"),
                                        ("redis-core-identity-test.go", "cmd/online-test-fixture/redis_core_fixture_test.go")):
                shutil.copyfile(ROOT / "scripts/test" / source, checkout / "system/backend" / destination)
            if real:
                shutil.copyfile(ROOT / "scripts/test/redis-core-system-wiring.go", checkout / "system/backend/internal/api/redis_core_wiring_test.go")
                shutil.copyfile(ROOT / "scripts/test/redis-core-gateway-wiring.go", checkout / "gateway/internal/router/redis_core_wiring_test.go")
            identity = root / "identity.json"
            env = dict(os.environ, GOWORK="off", ADDP_REDIS_WIRING_IDENTITY=str(identity))
            preparation = import_module("scripts.test.redis-core-http-preparation")
            stage_env = {"RUNNER_TEMP": directory, "ADDP_ONLINE_SECRET_DIR": directory,
                         "ADDP_ONLINE_ARTIFACT_DIR": directory, "ADDP_ONLINE_TEST_RUN_ID": "identity-gate"}
            with patch.dict(os.environ, stage_env):
                preparation.build_identity()
            build = json.loads((root/"redis-core-identity-build.json").read_text())
            self.assertEqual((build["exit_code"],build["result"]),(0,"passed"))
            self.assertTrue(os.access(root/"online-test-fixture",os.X_OK))
            pattern = "^TestRedisCorePermissionBoundary$" if not real else "^TestRedisCore(PermissionBoundary|IdentityAgainstPostgres)$"
            test_started = time.monotonic()
            result = subprocess.run(["go", "test", "./cmd/online-test-fixture", "-run", pattern, "-count=1", "-v"],
                                    cwd=checkout / "system/backend", env=env, capture_output=True, text=True, timeout=240)
            test_seconds = time.monotonic() - test_started
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertNotIn("--- SKIP:", result.stdout)
            self.assertIn("--- PASS: TestRedisCorePermissionBoundary ", result.stdout)
            permission_result = next(line for line in result.stdout.splitlines()
                                     if "--- PASS: TestRedisCorePermissionBoundary " in line)
            print(f"Redis pinned fixture: checkout={checkout_seconds:.3f}s "
                  f"CLI build={build['elapsed_seconds']:.3f}s go test={test_seconds:.3f}s; {permission_result.strip()}", flush=True)
            if real:
                self.assertIn("--- PASS: TestRedisCoreIdentityAgainstPostgres ", result.stdout)
                # Use the real domain writer on the already migrated, gate-locked IAM database.
                # Only the DB transport changes here: Hosted reads through its own Docker container.
                testbin = root/"identity-writer.test"
                subprocess.run(["go","test","-c","-o",str(testbin),"./cmd/online-test-fixture"],
                               cwd=checkout/"system/backend",env=env,check=True,timeout=240)
                dsn = urlsplit(env["ADDP_SYSTEM_POSTGRES_TEST_DSN"])
                pg_env = dict(env, PGHOST=dsn.hostname, PGPORT=str(dsn.port or 5432), PGDATABASE=dsn.path[1:],
                              PGUSER=unquote(dsn.username), PGPASSWORD=unquote(dsn.password),
                              PGOPTIONS="-c default_transaction_read_only=on")
                def read_gate_db(statement):
                    result = subprocess.run(["psql","-XwqAt","-v","ON_ERROR_STOP=1"],input=statement,text=True,
                        env=pg_env,
                        capture_output=True,check=True,timeout=5)
                    return json.loads(result.stdout)
                timeout_artifacts = root / "interrupted"
                timeout_artifacts.mkdir()
                with patch.dict(os.environ, stage_env | {"ADDP_ONLINE_ARTIFACT_DIR": str(timeout_artifacts)}), \
                        patch.object(preparation, "database", read_gate_db):
                    with self.assertRaises(preparation.IdentityTimeout):
                        preparation.identity_stage("initial",[str(testbin),"-test.run=^TestRedisCoreInterruptedWriter$"],
                            checkout/"system/backend",8,root/"incomplete.json")
                interrupted = json.loads((timeout_artifacts/"redis-core-identity-initial.json").read_text())
                before, after = interrupted["counts_before"], interrupted["counts_after"]
                self.assertEqual(interrupted["exit_code"],-9)
                self.assertFalse(interrupted["identity_file_present"])
                self.assertEqual(after, before | {"users":before["users"]+1,"audits":before["audits"]+1})
                print("Redis identity stage: fixed fixture built; interrupted domain writer reaped; PG partial users=1 audits=1 tenants=0")
                if os.environ.get("GITHUB_JOB") == "common-redis-core":
                    self.hosted_identity(root, checkout, testbin, stage_env)
                self.wire(root, checkout, identity, env)
                retired = subprocess.run(["go", "test", "./cmd/online-test-fixture", "-run", "^TestRedisCoreRetireAgainstPostgres$", "-count=1", "-v"],
                    cwd=checkout / "system/backend", env=env, capture_output=True, text=True, timeout=120)
                self.assertEqual(retired.returncode,0,retired.stdout+retired.stderr)
                self.assertIn("--- PASS: TestRedisCoreRetireAgainstPostgres ",retired.stdout)
                print("Redis pinned identity and cross-process System/Gateway wiring: passed; product=" + scenario.PRODUCT_SHA)

    def hosted_identity(self, root, checkout, testbin, stage_env):
        preparation = import_module("scripts.test.redis-core-http-preparation")
        container = os.environ["ADDP_REDIS_IDENTITY_PG_CONTAINER"]
        env = dict(stage_env, POSTGRES_DB="addp_online", POSTGRES_HOST="127.0.0.1", POSTGRES_USER="postgres",
                   POSTGRES_PORT=os.environ["ADDP_REDIS_IDENTITY_PG_PORT"],
                   POSTGRES_PASSWORD=os.environ["ADDP_REDIS_IDENTITY_PG_PASSWORD"], ADDP_ONLINE_HOSTED="1")
        self.assertEqual(os.environ["RUNNER_ENVIRONMENT"], "github-hosted")
        identity = root / "hosted-identity.json"
        def sql(statement):
            result = subprocess.run(["docker", "exec", "-i", "-e", "PGOPTIONS=-c default_transaction_read_only=on",
                container, "psql", "-XwqAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "addp_online"],
                input=statement, text=True, capture_output=True, check=True, timeout=5)
            return json.loads(result.stdout)
        with patch.dict(os.environ, env), patch.object(preparation, "database", sql):
            subprocess.run([str(testbin), "-test.run=^TestRedisCoreMigrateHostedIdentity$"],
                           cwd=checkout/"system/backend", check=True, timeout=180)
            preparation.fixture("", identity)
            counts = preparation.identity_counts()
            self.assertEqual((counts["users"], counts["tenants"], counts["user_tokens"], counts["oauth_clients"]),
                             (5, 2, 3, 1))
            preparation.fixture("prepare", identity, 731)
            counts = preparation.identity_counts()
            self.assertEqual((counts["consumers"],counts["grants"],counts["credentials"]), (1,1,1))
            preparation.fixture("cleanup", identity)
            preparation.fixture("retire", identity)
            self.assertEqual(preparation.identity_counts()["consumers"], 0)
            self.assertEqual(sql("select count(*) from system.service_principals s join system.principals p "
                                 "on s.id=p.id where s.name='redis-core-oauth-probe' and p.status='suspended'"), 1)
            for stage in ("build", "initial", "prepare", "cleanup", "retire"):
                record = json.loads((root/f"redis-core-identity-{stage}.json").read_text())
                self.assertEqual((record["result"],record["exit_code"]), ("passed",0))
                print(f"Hosted identity {stage}: seconds={record['elapsed_seconds']} exit={record['exit_code']}")
        print("Hosted first identity + Consumer prepare/cleanup + retire: one fixed binary, five successful stage records")

    def wire(self, root, checkout, identity_path, env):
        source_port = port()
        config = {"redis_upstream": os.environ["ADDP_REDIS_CORE_ENDPOINT"].rsplit(":", 1),
                  "system_upstream": f"http://127.0.0.1:{source_port}", "service_upstream": f"http://127.0.0.1:{source_port}",
                  "system_paths": [["POST", scenario.TOKEN_PATH]], "service_paths": []}
        config["redis_upstream"][1] = int(config["redis_upstream"][1])
        rig = transport.Rig(config); ports = rig.start()
        children = []; logs = []
        env |= {"ADDP_REDIS_WIRING_GATEWAY_SECRET": secrets.token_hex(24), "ADDP_REDIS_WIRING_STOP": str(root / "stop")}
        try:
            for owner, listen in (("system", source_port), ("gateway", port())):
                ready = root / (owner + ".ready")
                log = (root / (owner + ".log")).open("w+"); logs.append(log)
                child = subprocess.Popen(["go", "test", "./internal/" + ("api" if owner == "system" else "router"),
                    "-run", "^TestRedisCore" + owner.title() + "Wire$", "-count=1", "-timeout=3m", "-v"],
                    cwd=checkout / ("system/backend" if owner == "system" else "gateway"),
                    env=env | {"ADDP_REDIS_WIRING_CHILD": owner, "ADDP_REDIS_WIRING_READY": str(ready),
                       "ADDP_REDIS_WIRING_LISTEN": f"127.0.0.1:{listen}", "ADDP_REDIS_WIRING_ENDPOINT": f"127.0.0.1:{ports[owner]}",
                       "ADDP_REDIS_WIRING_SYSTEM_URL": f"http://127.0.0.1:{ports['system_http']}"},
                    stdout=log, stderr=subprocess.STDOUT)
                children.append(child)
                try: wait_file(ready, child)
                except RuntimeError:
                    log.seek(0); self.fail(log.read()[-6000:])
            gateway = "http://" + (root / "gateway.ready").read_text()
            system = "http://127.0.0.1:" + str(source_port)
            # Listener existence is not module readiness. Wait only on health,
            # before issuing the one normal OAuth business request.
            deadline = time.monotonic() + 15
            for origin in (system, gateway):
                while True:
                    current = scenario.request(origin, "GET", "/health/ready", timeout=2)
                    if current.status == 200: break
                    if time.monotonic() >= deadline:

                        for log in logs:
                            log.flush(); log.seek(0)
                            content=log.read()
                            for value in (env["ADDP_REDIS_WIRING_GATEWAY_SECRET"], identity_path.read_text()):
                                content=content.replace(value,"[private]")
                            print("PROTOCOL LOG:",content[-4000:])
                        self.fail("protocol prerequisite not ready: " + json.dumps(current.payload))
                    time.sleep(.1)
            identity = json.loads(identity_path.read_text())
            token = scenario.oauth(gateway, identity)
            self.assertEqual(token.status, 200, token.payload)
            t0 = token.payload["access_token"]
            before = scenario.request(gateway, "GET", scenario.CONTEXT_PATH, headers={"Authorization": "Bearer " + t0})
            self.assertEqual(before.status, 200)
            projection = scenario.token_projection(before.payload, identity)
            rig.faults["gateway"].set_available(False)
            scenario.ready(gateway, 503); scenario.ready(system, 200)
            refused = scenario.oauth(gateway, identity); scenario.assert_unavailable(refused)
            scenario.assert_forwarded({name: x.snapshot() for name, x in rig.http.items()}, "system", refused, 0)
            rig.faults["system"].set_available(False); rig.faults["gateway"].set_available(True)
            scenario.ready(gateway, 200); scenario.ready(system, 503)
            refused = scenario.oauth(gateway, identity); scenario.assert_unavailable(refused)
            scenario.assert_forwarded({name: x.snapshot() for name, x in rig.http.items()}, "system", refused, 1)
            rig.faults["system"].set_available(True)
            recovered = scenario.request(gateway, "GET", scenario.CONTEXT_PATH, headers={"Authorization": "Bearer " + t0})
            self.assertEqual(recovered.status, 200)
            self.assertEqual(scenario.token_projection(recovered.payload, identity), projection)
            self.assertEqual(scenario.request(gateway, "GET", "/health/live").status, 200)
        finally:
            (root / "stop").touch()
            for child in children:
                try: child.wait(timeout=15)
                except subprocess.TimeoutExpired: child.kill(); child.wait(timeout=5)
            rig.close()
            for log in logs: log.close()
        for child in children: self.assertEqual(child.returncode, 0, "pinned child failed to close")


if __name__ == "__main__":
    import sys
    if sys.argv[1:] == ["--real-source-pg"]:
        verify_source_cold_start()
    elif sys.argv[1:] == ["--real-wiring"]:
        RedisPinnedFixtureTest().execute(True)
    else:
        unittest.main()
