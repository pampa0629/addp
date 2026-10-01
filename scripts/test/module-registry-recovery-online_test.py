import importlib.util
import json
from datetime import datetime
import sys
import threading
import unittest
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit


SCRIPT = Path(__file__).with_name("module-registry-recovery-online.py")
SPEC = importlib.util.spec_from_file_location("module_registry_recovery_online", SCRIPT)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)

PROCESS_SCRIPT = Path(__file__).with_name("module-lifecycle-process-online.py")
PROCESS_SPEC = importlib.util.spec_from_file_location(
    "module_lifecycle_process_online", PROCESS_SCRIPT
)
PROCESS_ONLINE = importlib.util.module_from_spec(PROCESS_SPEC)
assert PROCESS_SPEC.loader is not None
sys.modules[PROCESS_SPEC.name] = PROCESS_ONLINE
PROCESS_SPEC.loader.exec_module(PROCESS_ONLINE)

MANAGER_PROCESS_URL = "http://127.0.0.1:8081"
SYSTEM_PROCESS_URL = "http://127.0.0.1:8180"
GATEWAY_PROCESS_URL = "http://127.0.0.1:8000"
PROCESS_INSTANCE_ID = "manager-runtime-42"
PROCESS_GIT_COMMIT = "0123456789abcdef"


def process_live_payload(module: str) -> dict[str, object]:
    return {
        "status": "live",
        "module": module,
        "git_commit": PROCESS_GIT_COMMIT,
        "build_id": f"build-{module}",
        "source_fingerprint": f"sha256:{module}",
        "built_at": "2026-08-26T00:00:00Z",
        "started_at": "2026-08-26T00:01:00Z",
    }


NODE_IPS = ("192.0.2.10", "2001:db8::10")


def instance_record(instance_id=PROCESS_INSTANCE_ID, status="up", reason="", *, newer=False):
    return {
        "instance_id": instance_id, "module_name": "manager", "role": "backend",
        "module_url": MANAGER_PROCESS_URL, "registered_host": "127.0.0.1",
        "host_node_ips": list(NODE_IPS), "host_node_name": "online-runner",
        "process_started_at": "2026-08-26T00:02:00Z" if newer else "2026-08-26T00:01:00Z",
        "registered_at": "2026-08-26T00:02:01Z",
        "status": status, "stop_reason": reason,
        "stopped_at": "2026-08-26T00:03:00Z" if status == "down" else None,
        "lease_expires_at": "2026-08-26T00:03:00Z",
    }


def baseline_record(record=None):
    return {"manager": {"instance": record or instance_record()}}


class ProcessFakeClient:
    def __init__(
        self,
        *,
        manager_ready: bool,
        system_available: bool,
        gateway_available: bool,
        gateway_manager_present: bool = False,
        instance_id: str = PROCESS_INSTANCE_ID,
        manager_available: bool = True,
        records=None,
        platform=True,
        read_permission=True,
        ignore_time_to=False,
    ) -> None:
        self.manager_ready = manager_ready
        self.system_available = system_available
        self.gateway_available = gateway_available
        self.gateway_manager_present = gateway_manager_present
        self.instance_id = instance_id
        self.manager_available = manager_available
        self.records = records if records is not None else [instance_record(instance_id)]
        self.platform = platform
        self.read_permission = read_permission
        self.ignore_time_to = ignore_time_to
        self.queries = []

    def get(self, base_url: str, path: str):
        if base_url == MANAGER_PROCESS_URL:
            if not self.manager_available:
                raise PROCESS_ONLINE.TransportUnavailable("connection refused")
            if path == "/health/live":
                live = process_live_payload("manager")
                current = next((row for row in self.records if row["instance_id"] == self.instance_id), None)
                if current:
                    live["started_at"] = current["process_started_at"]
                return PROCESS_ONLINE.Response(200, live)
            if path == "/health/ready":
                return PROCESS_ONLINE.Response(
                    200 if self.manager_ready else 503,
                    {
                        "status": "ready" if self.manager_ready else "not_ready",
                        "module": "manager",
                        "instance_id": self.instance_id,
                        "registration_state": (
                            "registered" if self.manager_ready else "recovering"
                        ),
                    },
                )
            if path == "/":
                if self.manager_ready:
                    return PROCESS_ONLINE.Response(
                        200, {"message": "Manager 数据管理服务"}
                    )
                return PROCESS_ONLINE.Response(
                    503,
                    {"error": "module is not ready", "error_code": "module_not_ready"},
                )
        if base_url == SYSTEM_PROCESS_URL:
            if not self.system_available:
                raise PROCESS_ONLINE.TransportUnavailable("connection refused")
            if path == "/health/live":
                return PROCESS_ONLINE.Response(200, process_live_payload("system"))
            if path == "/api/v1/system/auth/context":
                return PROCESS_ONLINE.Response(200, {
                    "context": {"type": "platform" if self.platform else "tenant"},
                    "authorization": {"role_assignments": [{"permissions":
                        ["platform.module.read"] if self.read_permission else []}]},
                })
            if path.startswith(PROCESS_ONLINE.INSTANCE_PATH + "?"):
                query = {key: values[0] for key, values in parse_qs(urlsplit(path).query).items()}
                self.queries.append(query)
                rows = []
                for record in self.records:
                    if any(record.get(key) != query[key] for key in
                           ("module_name", "role", "status", "stop_reason") if key in query):
                        continue
                    if "node_ip" in query and query["node_ip"] not in record["host_node_ips"]:
                        continue
                    if "time_from" in query:
                        date = PROCESS_ONLINE._timestamp(record["stopped_at"])
                        if date < PROCESS_ONLINE._timestamp(query["time_from"]):
                            continue
                        if not self.ignore_time_to and date >= PROCESS_ONLINE._timestamp(query["time_to"]):
                            continue
                    rows.append(record)
                return PROCESS_ONLINE.Response(200, {"data": rows, "page": int(query["page"]),
                                                     "total_pages": 1 if rows else 0})
            return PROCESS_ONLINE.Response(200, {"status": "ready", "module": "system"})
        if base_url == GATEWAY_PROCESS_URL:
            if not self.gateway_available:
                raise PROCESS_ONLINE.TransportUnavailable("connection refused")
            if path == "/health/live":
                return PROCESS_ONLINE.Response(200, process_live_payload("gateway"))
            if path == "/health/ready":
                return PROCESS_ONLINE.Response(200, {"status": "ready", "module": "gateway"})
            modules = {"system": SYSTEM_PROCESS_URL}
            if self.gateway_manager_present:
                modules["manager"] = MANAGER_PROCESS_URL
            return PROCESS_ONLINE.Response(200, {"modules": modules})
        raise AssertionError(f"unexpected request: {base_url}{path}")


class RegistryContractHandler(BaseHTTPRequestHandler):
    records = []
    fail_registration = False

    def do_GET(self):
        self.__class__.records.append(
            ("get", self.path, self.headers.get("Authorization"))
        )
        self._json(
            200,
            {
                "principal": {"type": "service_principal", "id": "7"},
                "context": {"type": "platform"},
                "client": {"client_id": "addp-manager"},
                "token": {"type": "service_access_token"},
                "authorization": {
                    "role_assignments": [
                        {
                            "role_key": "platform.manager_runtime",
                            "permissions": ["system.runtime_registry.update"],
                        }
                    ]
                },
            },
        )

    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        if self.path == "/api/v1/system/oauth/token":
            self.__class__.records.append(
                ("token", self.headers.get("Authorization"), parse_qs(raw.decode()))
            )
            self._json(200, {"access_token": "addp_at_online", "token_type": "Bearer"})
            return
        payload = json.loads(raw)
        self.__class__.records.append(
            ("post", self.path, self.headers.get("Authorization"), payload)
        )
        if self.path == "/api/v1/system/runtime/modules":
            if self.fail_registration or not payload.get("process_started_at"):
                self._json(400, {"error": "invalid probe", "error_code": "probe_invalid"})
                return
            datetime.fromisoformat(payload["process_started_at"])
        self._json(200, {"message": "ok", "module": "manager"})

    def do_DELETE(self):
        self.__class__.records.append(
            ("delete", self.path, self.headers.get("Authorization"))
        )
        self.send_response(204)
        self.end_headers()

    def _json(self, status, payload):
        raw = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, _format, *_args):
        return


class RegistryContractServer:
    def __enter__(self):
        RegistryContractHandler.records = []
        RegistryContractHandler.fail_registration = False
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), RegistryContractHandler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        host, port = self.server.server_address
        self.url = f"http://{host}:{port}"
        return self

    def __exit__(self, _exc_type, _exc, _traceback):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2.0)


class FakeRegistry:
    def __init__(self, fail_register=False):
        self.fail_register = fail_register
        self.calls = []

    def register(self, registration):
        self.calls.append(("register", registration.instance_id, registration.metadata["release"]))
        if self.fail_register:
            raise ONLINE.SuiteError("forced registration failure")

    def heartbeat(self, module_name, instance_id):
        self.calls.append(("heartbeat", instance_id))

    def deregister(self, module_name, instance_id):
        self.calls.append(("deregister", instance_id))


class FakeRoutes:
    def __init__(self):
        self.calls = []

    def wait(self, status, expected_instances=(), require_all=False, timeout=None):
        self.calls.append((status, tuple(expected_instances), require_all, timeout))
        return set(expected_instances)


class ModuleRegistryRecoveryOnlineTest(unittest.TestCase):
    def test_registry_client_uses_platform_oauth_and_instance_scoped_contract(self):
        with RegistryContractServer() as server:
            client = ONLINE.RegistryClient(server.url, "manager-secret", 2.0)
            registration = ONLINE.Probe(
                "manager-online-a", "http://127.0.0.1:19081"
            ).registration("run-42", "v1")
            client.register(registration)
            client.heartbeat("manager", "manager-online-a")
            client.deregister("manager", "manager-online-a")

        token = RegistryContractHandler.records[0]
        self.assertEqual(token[0], "token")
        self.assertTrue(token[1].startswith("Basic "))
        self.assertEqual(token[2]["context_type"], ["platform"])
        identity_request = RegistryContractHandler.records[1]
        self.assertEqual(identity_request[1], "/api/v1/system/auth/context")
        self.assertEqual(identity_request[2], "Bearer addp_at_online")
        registration_request = RegistryContractHandler.records[2]
        self.assertEqual(registration_request[1], "/api/v1/system/runtime/modules")
        self.assertEqual(registration_request[2], "Bearer addp_at_online")
        self.assertEqual(registration_request[3]["instance_id"], "manager-online-a")
        self.assertEqual(registration_request[3]["role"], "backend")
        self.assertEqual(registration_request[3]["process_started_at"], registration.process_started_at)
        self.assertIsNotNone(datetime.fromisoformat(registration.process_started_at).tzinfo)
        self.assertEqual(registration.process_started_at,
                         ONLINE.Probe("manager-online-a", "http://127.0.0.1:19081")
                         .registration("run-42", "v2").process_started_at)
        heartbeat_request = RegistryContractHandler.records[3]
        self.assertEqual(
            heartbeat_request[1], "/api/v1/system/runtime/modules/heartbeat"
        )
        self.assertEqual(
            heartbeat_request[3],
            {"module_name": "manager", "instance_id": "manager-online-a"},
        )
        self.assertEqual(
            RegistryContractHandler.records[4][1],
            "/api/v1/system/runtime/modules/manager/instances/manager-online-a",
        )

    def test_registry_client_reports_4xx_response_body(self):
        with RegistryContractServer() as server:
            RegistryContractHandler.fail_registration = True
            client = ONLINE.RegistryClient(server.url, "manager-secret", 2.0)
            registration = ONLINE.Probe(
                "manager-online-a", "http://127.0.0.1:19081"
            ).registration("run-42", "v1")
            with self.assertRaisesRegex(ONLINE.SuiteError, "probe_invalid"):
                client.register(registration)

    def test_rejects_manager_token_with_broader_or_wrong_identity(self):
        payload = {
            "principal": {"type": "user", "id": "7"},
            "context": {"type": "platform"},
            "client": {"client_id": "addp-manager"},
            "token": {"type": "service_access_token"},
            "authorization": {
                "role_assignments": [
                    {
                        "role_key": "platform.manager_runtime",
                        "permissions": ["system.runtime_registry.update"],
                    }
                ]
            },
        }
        with self.assertRaisesRegex(ONLINE.SuiteError, "Service Principal"):
            ONLINE.validate_manager_service_identity(payload, "addp-manager")

        payload["principal"]["type"] = "service_principal"
        payload["authorization"]["role_assignments"][0]["permissions"].append(
            "platform.module.update"
        )
        with self.assertRaisesRegex(ONLINE.SuiteError, "exactly"):
            ONLINE.validate_manager_service_identity(payload, "addp-manager")

    def test_accepts_idempotency_lease_recovery_and_multi_instance_handover(self):
        registry = FakeRegistry()
        routes = FakeRoutes()

        report = ONLINE.run_suite(
            registry,
            routes,
            ONLINE.Probe("manager-online-a", "http://127.0.0.1:19081"),
            ONLINE.Probe("manager-online-b", "http://127.0.0.1:19082"),
            "run-42",
            route_timeout=12.0,
            lease_timeout=45.0,
        )

        self.assertEqual(report["residual_active_instances"], 0)
        self.assertEqual(report["registered_instances"], 2)
        self.assertEqual(
            registry.calls,
            [
                ("register", "manager-online-a", "v1"),
                ("register", "manager-online-a", "v1"),
                ("heartbeat", "manager-online-a"),
                ("register", "manager-online-a", "v1"),
                ("register", "manager-online-b", "v2"),
                ("deregister", "manager-online-a"),
                ("register", "manager-online-a", "v3"),
                ("deregister", "manager-online-b"),
                ("deregister", "manager-online-a"),
            ],
        )
        self.assertEqual(routes.calls[0], (503, (), False, 12.0))
        self.assertIn((200, ("manager-online-a", "manager-online-b"), True, 12.0), routes.calls)
        self.assertEqual(routes.calls[-1], (503, (), False, 12.0))

    def test_registration_failure_still_deregisters_both_probe_identities(self):
        registry = FakeRegistry(fail_register=True)
        routes = FakeRoutes()

        with self.assertRaisesRegex(ONLINE.SuiteError, "forced registration failure"):
            ONLINE.run_suite(
                registry,
                routes,
                ONLINE.Probe("manager-online-a", "http://127.0.0.1:19081"),
                ONLINE.Probe("manager-online-b", "http://127.0.0.1:19082"),
                "run-42",
                route_timeout=12.0,
                lease_timeout=45.0,
            )

        self.assertEqual(
            registry.calls[-2:],
            [("deregister", "manager-online-b"), ("deregister", "manager-online-a")],
        )

    def test_registration_payload_uses_manager_backend_contract(self):
        probe = ONLINE.Probe("manager-online-a", "http://127.0.0.1:19081")
        registration = probe.registration("run-42", "v1")

        self.assertEqual(registration.module_name, "manager")
        self.assertEqual(registration.role, "backend")
        self.assertEqual(registration.route_prefix, "/api/v1/manager")
        self.assertEqual(registration.health_check_url, "http://127.0.0.1:19081/health/ready")
        self.assertEqual(registration.metadata["online_run_id"], "run-42")


class ModuleLifecycleProcessOnlineTest(unittest.TestCase):
    def _observe(
        self,
        phase: str,
        client: ProcessFakeClient,
        expected_instance_id: str | None = None,
        expected_git_commit: str | None = None,
        baseline=None,
    ):
        return PROCESS_ONLINE.observe_once(
            phase,
            client,
            manager_url=MANAGER_PROCESS_URL,
            system_url=SYSTEM_PROCESS_URL,
            gateway_url=GATEWAY_PROCESS_URL,
            expected_instance_id=expected_instance_id,
            expected_git_commit=expected_git_commit,
            expected_host_node_ips=NODE_IPS,
            baseline=baseline if baseline is not None else baseline_record(),
        )

    def test_accepts_all_formal_process_lifecycle_phases(self) -> None:
        phases = (
            (
                "business-before-system",
                ProcessFakeClient(
                    manager_ready=False,
                    system_available=False,
                    gateway_available=False,
                ),
                None,
            ),
            (
                "manager-registered",
                ProcessFakeClient(
                    manager_ready=True,
                    system_available=True,
                    gateway_available=False,
                ),
                PROCESS_INSTANCE_ID,
            ),
            (
                "gateway-established",
                ProcessFakeClient(
                    manager_ready=True,
                    system_available=True,
                    gateway_available=True,
                    gateway_manager_present=True,
                ),
                PROCESS_INSTANCE_ID,
            ),
            (
                "system-interrupted",
                ProcessFakeClient(
                    manager_ready=False,
                    system_available=False,
                    gateway_available=True,
                    gateway_manager_present=False,
                ),
                PROCESS_INSTANCE_ID,
            ),
            (
                "system-recovered",
                ProcessFakeClient(
                    manager_ready=True,
                    system_available=True,
                    gateway_available=True,
                    gateway_manager_present=True,
                ),
                PROCESS_INSTANCE_ID,
            ),
        )
        for phase, client, instance_id in phases:
            with self.subTest(phase=phase):
                report = self._observe(phase, client, instance_id)
                self.assertEqual(report["phase"], phase)
                self.assertEqual(
                    report["manager"]["instance_id"], PROCESS_INSTANCE_ID
                )

    def test_rejects_instance_change_and_stale_gateway_route(self) -> None:
        with self.assertRaisesRegex(PROCESS_ONLINE.ObservationError, "instance_id changed"):
            self._observe(
                "system-recovered",
                ProcessFakeClient(
                    manager_ready=True,
                    system_available=True,
                    gateway_available=True,
                    gateway_manager_present=True,
                    instance_id="manager-runtime-replaced",
                ),
                PROCESS_INSTANCE_ID,
            )
        with self.assertRaisesRegex(PROCESS_ONLINE.ObservationError, "still exposes"):
            self._observe(
                "system-interrupted",
                ProcessFakeClient(
                    manager_ready=False,
                    system_available=False,
                    gateway_available=True,
                    gateway_manager_present=True,
                ),
                PROCESS_INSTANCE_ID,
            )

    def test_requires_current_checkout_build_identity(self) -> None:
        client = ProcessFakeClient(
            manager_ready=True,
            system_available=True,
            gateway_available=True,
            gateway_manager_present=True,
        )
        report = self._observe(
            "system-recovered",
            client,
            PROCESS_INSTANCE_ID,
            PROCESS_GIT_COMMIT,
        )
        self.assertEqual(report["git_commit"], PROCESS_GIT_COMMIT)

        with self.assertRaisesRegex(PROCESS_ONLINE.ObservationError, "git_commit"):
            self._observe(
                "system-recovered",
                client,
                PROCESS_INSTANCE_ID,
                "different-checkout",
            )


    def _fault_client(self, phase, **overrides):
        old = instance_record(status="down", reason="lease_expired")
        new = instance_record("manager-runtime-new", newer=True)
        defaults = dict(manager_ready=True, system_available=True, gateway_available=True,
                        manager_available=phase == "manager-restarted",
                        gateway_manager_present=phase == "manager-restarted")
        if phase == "manager-restarted":
            defaults.update(instance_id=new["instance_id"], records=[new, old])
        elif phase == "manager-gracefully-stopped":
            new.update(status="down", stop_reason="graceful", stopped_at="2026-08-26T00:04:00Z")
            defaults.update(instance_id=new["instance_id"], records=[new, old])
        else:
            defaults.update(records=[old])
        defaults.update(overrides)
        return ProcessFakeClient(**defaults)

    def test_accepts_forced_exit_restart_and_graceful_exit(self):
        phase = "manager-abnormally-stopped"
        client = self._fault_client(phase)
        report = self._observe(phase, client, PROCESS_INSTANCE_ID)
        self.assertEqual(report["manager"]["instance"]["stop_reason"], "lease_expired")
        self.assertTrue(any(query.get("node_ip") == NODE_IPS[1] and
                            query.get("time_basis") == "offline" and
                            query.get("stop_reason") == "lease_expired" for query in client.queries))
        restarted = self._observe("manager-restarted", self._fault_client("manager-restarted"))
        self.assertEqual(restarted["manager"]["previous_instance"]["status"], "down")
        self.assertNotEqual(restarted["manager"]["instance_id"], PROCESS_INSTANCE_ID)
        graceful = self._observe("manager-gracefully-stopped", self._fault_client("manager-gracefully-stopped"),
                                 "manager-runtime-new", baseline=restarted)
        self.assertEqual(graceful["manager"]["instance"]["stop_reason"], "graceful")

    def test_rejects_wrong_offline_reason_or_time_boundary(self):
        phase = "manager-abnormally-stopped"
        for override in ("reason", "boundary", "lease"):
            with self.subTest(override=override):
                client = self._fault_client(phase, ignore_time_to=override == "boundary")
                if override == "reason":
                    client.records[0]["stop_reason"] = "graceful"
                if override == "lease":
                    client.records[0]["stopped_at"] = "2026-08-26T00:03:01Z"
                with self.assertRaises(PROCESS_ONLINE.ObservationError):
                    self._observe(phase, client, PROCESS_INSTANCE_ID)

    def test_rejects_restart_reusing_identity_or_rewriting_old_start(self):
        for field in ("instance_id", "process_started_at", "old_start"):
            with self.subTest(field=field):
                client = self._fault_client("manager-restarted")
                if field == "instance_id":
                    client.instance_id = PROCESS_INSTANCE_ID
                    client.records[0][field] = PROCESS_INSTANCE_ID
                elif field == "old_start":
                    client.records[1]["process_started_at"] = "2026-08-26T00:01:01Z"
                else:
                    client.records[0][field] = instance_record()[field]
                with self.assertRaises(PROCESS_ONLINE.ObservationError):
                    self._observe("manager-restarted", client)

    def test_rejects_changed_start_after_same_process_recovery(self):
        client = self._fault_client("manager-restarted", instance_id=PROCESS_INSTANCE_ID,
                                    records=[instance_record()])
        client.records[0]["process_started_at"] = "2026-08-26T00:02:00Z"
        with self.assertRaisesRegex(PROCESS_ONLINE.ObservationError, "process_started_at"):
            self._observe("system-recovered", client, PROCESS_INSTANCE_ID)

    def test_rejects_missing_node_ips_and_observer_permission(self):
        for override in ("ips", "platform", "permission"):
            with self.subTest(override=override):
                client = self._fault_client("manager-abnormally-stopped",
                                            platform=override != "platform",
                                            read_permission=override != "permission")
                if override == "ips":
                    client.records[0]["host_node_ips"] = []
                with self.assertRaises(PROCESS_ONLINE.ObservationError):
                    self._observe("manager-abnormally-stopped", client, PROCESS_INSTANCE_ID)

    def test_node_ip_normalization_rejects_empty_and_invalid(self):
        self.assertEqual(PROCESS_ONLINE.node_ips("192.0.2.10,::ffff:192.0.2.10,2001:0db8::10"), NODE_IPS)
        for value in ("", "192.0.2.10,", "localhost"):
            with self.assertRaises(PROCESS_ONLINE.ObservationError):
                PROCESS_ONLINE.node_ips(value)

    def test_platform_token_is_sent_only_to_system_api(self):
        with RegistryContractServer() as server:
            client = PROCESS_ONLINE.HTTPClient(2.0, server.url, "observer-only-token")
            client.get(server.url, "/api/v1/system/auth/context")
            client.get(server.url, "/health/live")
            # Different origin, same local endpoint, must not receive the credential.
            client.get(server.url.replace("127.0.0.1", "localhost"), "/api/v1/system/auth/context")
        self.assertEqual([record[2] for record in RegistryContractHandler.records],
                         ["Bearer observer-only-token", None, None])


if __name__ == "__main__":
    unittest.main()
