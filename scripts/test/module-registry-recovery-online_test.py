import importlib.util
import json
import sys
import threading
import unittest
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs


SCRIPT = Path(__file__).with_name("module-registry-recovery-online.py")
SPEC = importlib.util.spec_from_file_location("module_registry_recovery_online", SCRIPT)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


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
        if self.path == "/api/v1/system/runtime/modules" and self.fail_registration:
            self._json(400, {"error": "invalid probe", "error_code": "probe_invalid"})
            return
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
        self.assertEqual(registration.health_check_url, "http://127.0.0.1:19081/health")
        self.assertEqual(registration.metadata["online_run_id"], "run-42")


if __name__ == "__main__":
    unittest.main()
