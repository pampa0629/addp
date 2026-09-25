import importlib.util
import io
import json
import os
import sys
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import DEFAULT, MagicMock, patch


SCRIPT = Path(__file__).with_name("compose-public-origin-online.py")
SPEC = importlib.util.spec_from_file_location("compose_public_origin_online", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


def container(name, bindings=None, env=None, project="addp-platform", service=None, networks=None):
    return [{"State": {"Running": True, "Health": {"Status": "healthy"}}, "Config": {"Labels": {"com.docker.compose.project": project, "com.docker.compose.service": service or name}, "Env": env or []}, "HostConfig": {"PortBindings": bindings or {}}, "NetworkSettings": {"Networks": {network: {} for network in (networks or [])}}}]


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

    def test_main_includes_module_frontends_and_gateway_routes(self):
        checks = {
            name: DEFAULT for name in (
                "root_compose_ports", "infra_compose_ports", "assert_platform_ports", "assert_isolated_groups",
                "assert_frontend", "assert_authorized_gateway", "assert_module_gateway_route",
            )
        }
        with patch.multiple(MODULE, **checks) as mocked, redirect_stdout(io.StringIO()) as output:
            self.assertEqual(MODULE.main(), 0)
        mocked["assert_frontend"].assert_any_call("/meta/", "/meta/")
        mocked["assert_frontend"].assert_any_call("/manager/", "/manager/")
        mocked["assert_frontend"].assert_any_call("/transfer/", "/transfer/")
        mocked["assert_frontend"].assert_any_call("/orchestrator/", "/orchestrator/")
        self.assertEqual(mocked["assert_module_gateway_route"].call_count, 4)
        mocked["assert_module_gateway_route"].assert_any_call("Meta", "/api/v1/meta/engines")
        mocked["assert_module_gateway_route"].assert_any_call("Manager", "/api/v1/manager/engines")
        mocked["assert_module_gateway_route"].assert_any_call("Transfer", "/api/v1/transfer/system-engines")
        mocked["assert_module_gateway_route"].assert_any_call("Orchestrator", "/api/v1/orchestrator/orchestrations")
        report = json.loads(output.getvalue())
        self.assertEqual(report["frontends"], ["console", "system", "meta", "manager", "transfer", "orchestrator"])
        self.assertEqual(report["gateway_manager_permission_guard"], "passed")
        self.assertEqual(report["gateway_transfer_permission_guard"], "passed")
        self.assertEqual(report["gateway_orchestrator_permission_guard"], "passed")

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
            "meta-backend": container("meta-backend", env=["SERVICE_HOST=meta-backend", "SYSTEM_URL=http://system-backend:8180", "POSTGRES_HOST=postgres", "POSTGRES_DB=addp_online"]),
            "meta-frontend": container("meta-frontend"),
            "manager-backend": container("manager-backend", env=["SERVICE_HOST=manager-backend", "SYSTEM_URL=http://system-backend:8180", "META_URL=http://meta-backend:8082", "POSTGRES_HOST=postgres", "POSTGRES_DB=addp_online", "REDIS_HOST=redis", "MINIO_ENDPOINT=minio:9000"]),
            "manager-frontend": container("manager-frontend"),
            "transfer-backend": container("transfer-backend", env=["SERVICE_HOST=transfer-backend", "SYSTEM_URL=http://system-backend:8180", "META_URL=http://meta-backend:8082", "POSTGRES_HOST=postgres", "POSTGRES_DB=addp_online", "REDIS_HOST=redis", "INFRA_KAFKA_BOOTSTRAP_SERVERS=redpanda:29092", "KAFKA_CONNECT_URL=http://kafka-connect:8083"]),
            "transfer-frontend": container("transfer-frontend"),
            "orchestrator-backend": container("orchestrator-backend", env=["SERVICE_HOST=orchestrator-backend", "SYSTEM_URL=http://system-backend:8180", "POSTGRES_HOST=postgres", "POSTGRES_DB=addp_online", "REDIS_HOST=redis"]),
            "orchestrator-frontend": container("orchestrator-frontend"),
            "addp-nginx": container("addp-nginx", {"80/tcp": [{"HostIp": "127.0.0.1", "HostPort": "18080"}]}),
        }
        docker_json.side_effect = lambda *args: fixtures[args[-1]]
        MODULE.assert_platform_ports()
        self.assertIn("meta-backend", [call.args[-1] for call in docker_json.call_args_list])
        self.assertIn("meta-frontend", [call.args[-1] for call in docker_json.call_args_list])
        self.assertIn("manager-backend", [call.args[-1] for call in docker_json.call_args_list])
        self.assertIn("manager-frontend", [call.args[-1] for call in docker_json.call_args_list])
        self.assertIn("transfer-backend", [call.args[-1] for call in docker_json.call_args_list])
        self.assertIn("transfer-frontend", [call.args[-1] for call in docker_json.call_args_list])
        self.assertIn("orchestrator-backend", [call.args[-1] for call in docker_json.call_args_list])
        self.assertIn("orchestrator-frontend", [call.args[-1] for call in docker_json.call_args_list])
        fixtures["orchestrator-backend"][0]["State"]["Health"]["Status"] = "unhealthy"
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.assert_platform_ports()
        fixtures["orchestrator-backend"][0]["State"]["Health"]["Status"] = "healthy"
        fixtures["orchestrator-backend"][0]["Config"]["Env"][0] = "SERVICE_HOST=127.0.0.1"
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.assert_platform_ports()
        fixtures["orchestrator-backend"][0]["Config"]["Env"][0] = "SERVICE_HOST=orchestrator-backend"
        fixtures["transfer-backend"][0]["State"]["Health"]["Status"] = "unhealthy"
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.assert_platform_ports()
        fixtures["transfer-backend"][0]["State"]["Health"]["Status"] = "healthy"
        fixtures["transfer-backend"][0]["Config"]["Env"][6] = "INFRA_KAFKA_BOOTSTRAP_SERVERS=localhost:19092"
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.assert_platform_ports()
        fixtures["transfer-backend"][0]["Config"]["Env"][6] = "INFRA_KAFKA_BOOTSTRAP_SERVERS=redpanda:29092"
        fixtures["manager-backend"][0]["State"]["Health"]["Status"] = "unhealthy"
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.assert_platform_ports()
        fixtures["manager-backend"][0]["State"]["Health"]["Status"] = "healthy"
        fixtures["manager-backend"][0]["Config"]["Env"][2] = "META_URL=http://127.0.0.1:8082"
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.assert_platform_ports()
        fixtures["manager-backend"][0]["Config"]["Env"][2] = "META_URL=http://meta-backend:8082"
        fixtures["meta-backend"][0]["State"]["Health"]["Status"] = "unhealthy"
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.assert_platform_ports()
        fixtures["meta-backend"][0]["State"]["Health"]["Status"] = "healthy"
        fixtures["system-backend"][0]["Config"]["Env"][0] = "PUBLIC_API_URL=http://localhost:80"
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.assert_platform_ports()

    @patch("time.sleep")
    @patch.object(MODULE, "request")
    def test_meta_route_reaches_owner_permission_guard(self, request, sleep):
        request.side_effect = [(503, b"{}", "application/json"), (403, b"{}", "application/json")]
        MODULE.assert_module_gateway_route("Meta", "/api/v1/meta/engines")
        self.assertEqual(request.call_count, 2)
        request.assert_any_call("/api/v1/meta/engines", "test-token")
        sleep.assert_called_once()

    @patch("time.sleep")
    @patch.object(MODULE, "request")
    def test_manager_route_reaches_owner_permission_guard(self, request, sleep):
        request.side_effect = [(503, b"{}", "application/json"), (403, b"{}", "application/json")]
        MODULE.assert_module_gateway_route("Manager", "/api/v1/manager/engines")
        self.assertEqual(request.call_count, 2)
        request.assert_any_call("/api/v1/manager/engines", "test-token")
        sleep.assert_called_once()

    @patch("time.sleep")
    @patch.object(MODULE, "request")
    def test_orchestrator_route_reaches_owner_permission_guard(self, request, sleep):
        request.side_effect = [(503, b"{}", "application/json"), (403, b"{}", "application/json")]
        MODULE.assert_module_gateway_route("Orchestrator", "/api/v1/orchestrator/orchestrations")
        self.assertEqual(request.call_count, 2)
        request.assert_any_call("/api/v1/orchestrator/orchestrations", "test-token")
        sleep.assert_called_once()

    @patch.object(MODULE, "docker_json")
    @patch.object(MODULE.subprocess, "run")
    @patch.object(MODULE.OPENER, "open")
    def test_runtime_and_business_projects_are_isolated_and_reachable(self, open_url, run, docker_json):
        response = MagicMock()
        response.status = 200
        open_url.return_value.__enter__.return_value = response
        configs = {
            "docker-compose.runtimes.yml": {"name": "addp-runtimes", "services": {"geopython-workflow-engine": {"expose": ["8099"]}}},
            "business/docker-compose.yml": {"name": "business", "services": {"minio": {"ports": [
                {"target": 9000, "published": "19002", "host_ip": "127.0.0.1"},
                {"target": 9001, "published": "19003", "host_ip": "127.0.0.1"},
            ]}}},
        }
        fixtures = {
            "geopython-workflow-engine": container("geopython-workflow-engine", project="addp-runtimes", networks=["addp-network"]),
            "business-minio": container("business-minio", project="business", service="minio", networks=["business_business-network"], bindings={
                "9000/tcp": [{"HostIp": "127.0.0.1", "HostPort": "19002"}],
                "9001/tcp": [{"HostIp": "127.0.0.1", "HostPort": "19003"}],
            }),
        }
        docker_json.side_effect = lambda *args, **kwargs: fixtures[args[-1]] if args[0] == "inspect" else configs[args[args.index("-f") + 1]]
        MODULE.assert_isolated_groups()
        business_call = next(call for call in docker_json.call_args_list if "business/docker-compose.yml" in call.args)
        self.assertEqual(business_call.kwargs["env"]["MINIO_API_PORT"], "127.0.0.1:19002")
        self.assertEqual(business_call.kwargs["env"]["MINIO_CONSOLE_PORT"], "127.0.0.1:19003")
        run.assert_called_once()
        open_url.assert_called_once_with("http://127.0.0.1:19002/minio/health/live", timeout=5)

        fixtures["business-minio"][0]["Config"]["Labels"]["com.docker.compose.project"] = "addp-platform"
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.assert_isolated_groups()
        fixtures["business-minio"][0]["Config"]["Labels"]["com.docker.compose.project"] = "business"
        fixtures["business-minio"][0]["NetworkSettings"]["Networks"] = {"addp-network": {}}
        with self.assertRaises(MODULE.AcceptanceError):
            MODULE.assert_isolated_groups()

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
