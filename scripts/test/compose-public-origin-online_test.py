import importlib.util
import json
import os
import sys
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("compose-public-origin-online.py")
SPEC = importlib.util.spec_from_file_location("compose_public_origin_online", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


def container(name, bindings=None, env=None):
    return [{"State": {"Running": True}, "Config": {"Labels": {"com.docker.compose.project": "addp-app"}, "Env": env or []}, "HostConfig": {"PortBindings": bindings or {}}}]


class ComposePublicOriginOnlineTest(unittest.TestCase):
    def setUp(self):
        self.env = patch.dict(os.environ, {
            "ADDP_ONLINE_TEST": "1", "ADDP_ONLINE_HOSTED": "1",
            "ADDP_ONLINE_PUBLIC_ORIGIN": "http://127.0.0.1:18080",
            "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "test-token",
            "ADDP_ONLINE_TEST_TENANT_ID": "42",
        })
        self.env.start()
        self.addCleanup(self.env.stop)

    @patch.object(MODULE, "docker_json")
    def test_production_compose_publishes_only_one_loopback_entry(self, docker_json):
        docker_json.return_value = {"services": {"nginx": {"ports": [{"published": "18080", "host_ip": "127.0.0.1"}]}, "gateway": {}, "system-backend": {}}}
        MODULE.root_compose_ports()
        docker_json.return_value["services"]["gateway"]["ports"] = [{"published": "8000"}]
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.root_compose_ports()

    @patch.object(MODULE, "docker_json")
    def test_runtime_ports_and_origin_use_actual_containers(self, docker_json):
        fixtures = {
            "system-backend": container("system-backend", {"8180/tcp": [{"HostIp": "127.0.0.1", "HostPort": "8180"}]}, ["PUBLIC_API_URL=http://127.0.0.1:18080", "CONSOLE_URL=http://127.0.0.1:18080"]),
            "gateway": container("gateway", {"8000/tcp": [{"HostIp": "127.0.0.1", "HostPort": "8000"}]}, ["SYSTEM_URL=http://system-backend:8180", "POSTGRES_HOST=postgres", "POSTGRES_DB=addp_online", "REDIS_HOST=redis"]),
            "console": container("console"),
            "system-frontend": container("system-frontend"),
            "addp-nginx": container("addp-nginx", {"80/tcp": [{"HostIp": "127.0.0.1", "HostPort": "18080"}]}),
        }
        docker_json.side_effect = lambda *args: fixtures[args[-1]]
        MODULE.assert_runtime_ports()
        fixtures["system-backend"][0]["Config"]["Env"][0] = "PUBLIC_API_URL=http://localhost:80"
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.assert_runtime_ports()

    @patch.object(MODULE, "docker_json")
    def test_hosted_infra_publishes_only_loopback_ports(self, docker_json):
        docker_json.return_value = {"services": {"postgres": {"ports": [{"host_ip": "127.0.0.1", "published": "15432"}]}}}
        MODULE.infra_compose_ports()
        docker_json.return_value["services"]["postgres"]["ports"][0]["host_ip"] = "0.0.0.0"
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.infra_compose_ports()

    @patch.object(MODULE, "request")
    def test_gateway_requires_real_tenant_and_enforces_engine_permission(self, request):
        context = {"principal": {"type": "user"}, "context": {"tenant_id": "42"}, "authorization": {"role_assignments": None}}
        request.side_effect = [(401, b"{}", "application/json"), (200, json.dumps(context).encode(), "application/json"), (403, b"{}", "application/json")]
        MODULE.assert_authorized_gateway()
        context["context"]["tenant_id"] = "1"
        request.side_effect = [(401, b"{}", "application/json"), (200, json.dumps(context).encode(), "application/json")]
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.assert_authorized_gateway()

    @patch.object(MODULE, "request")
    def test_frontend_requires_built_asset(self, request):
        request.side_effect = [(200, b'<div id="app"></div><script src="/system/assets/index.js"></script>', "text/html"), (200, b"compiled", "application/javascript")]
        MODULE.assert_frontend("/system/", "/system/")
        request.assert_any_call("/system/assets/index.js")


if __name__ == "__main__":
    unittest.main()
