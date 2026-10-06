import importlib
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

FIXTURE = importlib.import_module("scripts.test.platform-node-metrics-fixture")


class MetricsFixtureTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="addp-metrics-fixture-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.secret = self.root / "addp-online-secret-test"
        self.secret.mkdir(mode=0o700)
        self.env = dict(GITHUB_ACTIONS="true", RUNNER_OS="Linux", ADDP_ONLINE_HOSTED="1", ADDP_ONLINE_HOST="1",
                        ONLINE_SUITE="platform-node-metrics", POSTGRES_DB="addp_online",
                        ADDP_ONLINE_SECRET_DIR=str(self.secret), ADDP_ONLINE_ARTIFACT_DIR=str(self.root / "artifacts"))

    def test_boundary_refuses_personal_hosts_databases_and_public_secrets(self):
        with patch.dict(os.environ, self.env, clear=True), patch.object(FIXTURE, "ROOT", self.root / "repository"), patch.object(FIXTURE.platform, "system", return_value="Linux"), patch.object(FIXTURE.platform, "machine", return_value="x86_64"):
            self.assertEqual(FIXTURE.boundary(), self.secret)
            for key, value in (("GITHUB_ACTIONS", "false"), ("RUNNER_OS", "macOS"), ("POSTGRES_DB", "addp_test"), ("ONLINE_SUITE", "other"), ("ADDP_ONLINE_HOSTED", "0")):
                with self.subTest(key=key), patch.dict(os.environ, {key: value}), self.assertRaises(ValueError):
                    FIXTURE.boundary()
            self.secret.chmod(0o755)
            with self.assertRaises(ValueError):
                FIXTURE.boundary()

    def test_prepare_uses_production_center_and_distinct_real_tls_identities(self):
        calls = []
        original = FIXTURE.command
        def command(args):
            calls.append(args)
            if args[:4] == ["docker", "network", "inspect", "bridge"]:
                return '[{"IPAM":{"Config":[{"Gateway":"172.17.0.1"}]}}]'
            return original(args)
        with patch.dict(os.environ, self.env), patch.object(FIXTURE, "command", side_effect=command), patch.object(FIXTURE.socket, "socket"):
            FIXTURE.prepare(self.secret)
        spec = json.loads((self.secret / "metrics-compose.json").read_text())
        self.assertEqual(set(spec["services"]), {"control-tls"})
        self.assertNotIn("volumes", spec)
        self.assertEqual(spec["services"]["control-tls"]["network_mode"], "host")
        nginx = (self.secret / "nginx.conf").read_text()
        for module in ("client_body", "proxy", "fastcgi", "uwsgi", "scgi"):
            self.assertIn(module+"_temp_path /tmp/", nginx)
        config = (self.secret / "deployment/prometheus.yml").read_text()
        self.assertNotIn("@@", config)
        self.assertIn("https://172.17.0.1:9444/api/v1/monitor/platform/metrics_discovery", config)
        self.assertEqual({p.name for p in (self.secret / "node-tls").iterdir()}, {"ca.crt", "server.crt", "server.key"})
        self.assertEqual((self.secret / "query/client.key").stat().st_mode & 0o777, 0o600)
        self.assertEqual((self.secret / "query").stat().st_mode & 0o777, 0o700)
        self.assertNotEqual((self.secret / "query/client.crt").read_bytes(), (self.secret / "center-tls/health.crt").read_bytes())
        self.assertFalse(any("/query" in volume["source"] for volume in spec["services"]["control-tls"]["volumes"]))
        self.assertNotEqual((self.secret / "center-tls/health.crt").read_bytes(), (self.secret / "deployment/collector.crt").read_bytes())
        self.assertNotEqual((self.secret / "control-ca.crt").read_bytes(), (self.secret / "source-ca.crt").read_bytes())
        for cert, ca in (("center-tls/health.crt", "center-ca.crt"), ("node-tls/server.crt", "source-ca.crt"), ("deployment/collector.crt", "source-ca.crt")):
            subprocess.run(["openssl", "verify", "-CAfile", str(self.secret / ca), str(self.secret / cert)], check=True, capture_output=True)
        env = (self.secret / "metrics.env").read_text()
        self.assertEqual((self.secret / "metrics.env").stat().st_mode & 0o777, 0o600)
        self.assertIn(env, (self.secret / "runtime.env").read_text())
        self.assertFalse(any("up" in args or "run" in args for args in calls))
        with patch.object(FIXTURE, "command") as command:
            with self.assertRaises(ValueError):
                FIXTURE.prepare(self.secret)
            command.assert_not_called()

    def test_query_origin_uses_verified_center_mapping_and_refuses_foreign_or_public_ports(self):
        (self.secret / "query").mkdir(mode=0o700)
        labels = {"com.docker.compose.project": "addp-infra", "com.docker.compose.service": "prometheus",
                  "com.docker.compose.project.working_dir": str(FIXTURE.ROOT)}
        inspect = json.dumps([{"Config": {"Labels": labels}}])
        with patch.object(FIXTURE, "command", side_effect=["center-id", inspect, "127.0.0.1:29990"]):
            FIXTURE.configure_query(self.secret)
        for name in ("metrics.env", "runtime.env"):
            self.assertIn("https://127.0.0.1:29990", (self.secret / name).read_text())
            self.assertEqual((self.secret / name).stat().st_mode & 0o777, 0o600)
        (self.secret / "query/connected").unlink()
        for port in ("0.0.0.0:19090", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:019090", "127.0.0.1:19090\n127.0.0.1:19091"):
            with self.subTest(port=port), patch.object(FIXTURE, "command", side_effect=["center-id", inspect, port]), self.assertRaises(ValueError):
                FIXTURE.configure_query(self.secret)
        labels["com.docker.compose.project.working_dir"] = "foreign"
        with patch.object(FIXTURE, "command", side_effect=["center-id", json.dumps([{"Config": {"Labels": labels}}])]) as command, self.assertRaises(ValueError):
            FIXTURE.center_origin()
        self.assertEqual(command.call_count, 2)

    def test_cleanup_refuses_foreign_container_before_compose(self):
        with patch.object(FIXTURE, "command", side_effect=["foreign-id", '[{"Config":{"Labels":{}}}]']) as command:
            with self.assertRaises(ValueError):
                FIXTURE.assert_owned(self.secret)
            self.assertEqual(command.call_count, 2)


if __name__ == "__main__":
    unittest.main()
