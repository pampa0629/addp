"""Deterministic observer rejection and opt-in real pinned PG/Redis wiring."""
from importlib import import_module
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import tempfile
import threading
import time
import unittest
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
                      identities={"tenant_id":2,"oauth_principal_id":8,"consumer_id":9,"query_service_id":10,"engine_id":11})
        result["checks"]["owner_cleanup"]=True
        scenario.validate_report(result,"one")
        for mutate in (lambda x:x["checks"].pop("system_outage"),lambda x:x.update(elapsed_seconds=36),
                       lambda x:x["window"][3].update(expires_at_ms=200000),lambda x:x["requests"].pop(),
                       lambda x:x.update(product_sha="other"),lambda x:x.update(cleanup="failed")):
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


class RedisPinnedFixtureTest(unittest.TestCase):
    def test_fixed_identity_compiles_and_respects_real_catalog(self):
        self.execute(False)

    def execute(self, real):
        with tempfile.TemporaryDirectory(prefix="redis-r1-pinned-") as directory:
            root = Path(directory); checkout = root / "identity"
            subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), str(checkout)], check=True, capture_output=True)
            subprocess.run(["git", "checkout", "--detach", scenario.PRODUCT_SHA], cwd=checkout, check=True, capture_output=True)
            subprocess.run(["git", "apply", "--unidiff-zero", str(ROOT / "scripts/test/redis-core-identity.patch")], cwd=checkout, check=True, capture_output=True)
            for source, destination in (("redis-core-identity.go", "cmd/online-test-fixture/redis_core_fixture.go"),
                                        ("redis-core-identity-test.go", "cmd/online-test-fixture/redis_core_fixture_test.go"),
                                        ("redis-core-system-wiring.go", "internal/api/redis_core_wiring_test.go")):
                shutil.copyfile(ROOT / "scripts/test" / source, checkout / "system/backend" / destination)
            shutil.copyfile(ROOT / "scripts/test/redis-core-gateway-wiring.go", checkout / "gateway/internal/router/redis_core_wiring_test.go")
            identity = root / "identity.json"
            env = dict(os.environ, GOWORK="off", ADDP_REDIS_WIRING_IDENTITY=str(identity))
            pattern = "^TestRedisCorePermissionBoundary$" if not real else "^TestRedisCore(PermissionBoundary|IdentityAgainstPostgres)$"
            result = subprocess.run(["go", "test", "./cmd/online-test-fixture", "-run", pattern, "-count=1", "-v"],
                                    cwd=checkout / "system/backend", env=env, capture_output=True, text=True, timeout=240)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertNotIn("--- SKIP:", result.stdout)
            self.assertIn("--- PASS: TestRedisCorePermissionBoundary ", result.stdout)
            if real:
                self.assertIn("--- PASS: TestRedisCoreIdentityAgainstPostgres ", result.stdout)
                self.wire(root, checkout, identity, env)
                retired = subprocess.run(["go", "test", "./cmd/online-test-fixture", "-run", "^TestRedisCoreRetireAgainstPostgres$", "-count=1", "-v"],
                    cwd=checkout / "system/backend", env=env, capture_output=True, text=True, timeout=120)
                self.assertEqual(retired.returncode,0,retired.stdout+retired.stderr)
                self.assertIn("--- PASS: TestRedisCoreRetireAgainstPostgres ",retired.stdout)
                print("Redis pinned identity and cross-process System/Gateway wiring: passed; product=" + scenario.PRODUCT_SHA)

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
    if sys.argv[1:] == ["--real-wiring"]:
        RedisPinnedFixtureTest().execute(True)
    else:
        unittest.main()
