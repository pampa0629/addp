from copy import deepcopy
from importlib import import_module
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

LOCAL = import_module("scripts.test.sql-pool-local-gate")
HOSTED = import_module("scripts.test.sql-pool-consumer-flow-online_test")
RUN = "addp-sql-pool-local-" + "a" * 32


class LocalSQLPoolTest(unittest.TestCase):
    def test_only_native_local_docker_is_admitted(self):
        self.assertEqual(LOCAL.validate_local({}, "unix:///test.sock", "arm64", "aarch64"), "arm64")
        for env, endpoint, daemon in (({"GITHUB_ACTIONS": "true"}, "unix:///test.sock", "arm64"),
                                      ({}, "ssh://foreign", "arm64"), ({}, "unix:///test.sock", "amd64")):
            with self.subTest(env=env, endpoint=endpoint, daemon=daemon), self.assertRaises(LOCAL.CORE.GateError):
                LOCAL.validate_local(env, endpoint, "arm64", daemon)

    def test_stack_has_only_owned_data_and_private_sidecar(self):
        selected = LOCAL.INFRA + LOCAL.BACKENDS + LOCAL.FRONTENDS
        services = {name: {"command": [], "environment": {}, "image": name} for name in selected}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for key, filename in (("postgres", "infra-postgresql.conf"), ("redis", "redis-authentication.sh")):
                services[key]["volumes"] = [
                    {"type": "bind", "target": "/addp/" + filename, "source": str(root / filename), "read_only": True},
                    {"type": "volume", "target": "/data", "source": "personal-volume"},
                ]
            services["manager-backend"]["volumes"] = [{"type": "bind", "target": "/var/log/addp", "source": "/personal"}]
            config = LOCAL.deployment(services, RUN, {"gateway": "sha256:gateway"}, 54123, root / "proxy", root / "gate", root)
        actual = config["services"]
        self.assertEqual(actual["nginx"]["ports"][0]["host_ip"], "127.0.0.1")
        self.assertEqual(actual["nginx"]["ports"][0]["published"], "54123")
        self.assertEqual(actual["postgres"]["command"][-1], "cluster_name=" + RUN)
        self.assertEqual(actual["pool-meta"]["image"], "sha256:gateway")
        self.assertNotIn("ports", actual["pool-meta"])
        self.assertEqual(actual["manager-backend"]["environment"]["META_URL"], "http://pool-meta:8082")
        self.assertNotIn("/personal", repr(actual))
        self.assertTrue(config["networks"]["isolated"]["internal"])
        self.assertTrue(all(service["labels"][LOCAL.LABEL] == RUN for service in actual.values()))

    def test_local_browser_requires_correct_source_and_both_timeout_stages(self):
        evidence, expected = HOSTED.SQLPoolEvidenceTest().connection_evidence()
        for value in (expected, evidence):
            value["cases"] = [value["cases"][1]]
        evidence["schema_version"] = "addp.sql-pool-console-local/v1"
        evidence["cases"][0]["saturation"]["configuration"] = None
        LOCAL.validate_local_browser(evidence, expected)
        for change in ("source", "manager", "develop", "recovery"):
            bad = deepcopy(evidence)
            row, = bad["cases"]
            if change == "source":
                row["source_identity"]["native_id"] = "other-source"
            if change == "manager":
                row["saturation"]["manager_queued"]["http_status"] = 403
            if change == "develop":
                row["saturation"]["native"]["reason"] = "timeout"
            if change == "recovery":
                row["saturation"]["recovered_manager_rows"] = 0
            with self.subTest(change=change), self.assertRaises(RuntimeError):
                LOCAL.validate_local_browser(bad, expected)

    def test_local_fixture_names_never_resolve_to_the_business_mysql(self):
        with patch.dict(LOCAL.os.environ, {LOCAL.POOL.LOCAL_RUN: RUN}):
            self.assertEqual(LOCAL.POOL.container("mysql"), RUN + "-mysql")
            with self.assertRaises(RuntimeError):
                LOCAL.POOL.container("postgresql")


if __name__ == "__main__":
    unittest.main()
