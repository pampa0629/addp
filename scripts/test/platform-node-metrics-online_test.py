import copy
import importlib
import unittest
from unittest.mock import patch

ONLINE = importlib.import_module("scripts.test.platform-node-metrics-online")


def identity(role="platform.system_administrator"):
    return {"principal": {"type": "user"}, "context": {"type": "platform"},
            "authentication": {"assurance_level": "aal2"}, "token": {"type": "first_party_access_token"},
            "authorization": {"role_assignments": [{"role_key": role, "permissions": sorted(ONLINE.REQUIRED) if role.endswith("system_administrator") else []}]}}


class MetricsProtocolTest(unittest.TestCase):
    def test_rfc6238_sha1_vectors_use_six_digits(self):
        secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
        for at, expected in ((59, "287082"), (1111111109, "081804"), (1234567890, "005924"), (20000000000, "353130")):
            self.assertEqual(ONLINE.totp(secret, at), expected)

    def test_platform_identity_rejects_tenant_service_delegation_and_unverified_mfa(self):
        ONLINE.platform_identity(identity(), "platform.system_administrator")
        for change in ({"context": {"type": "tenant", "tenant_id": "2"}}, {"context": {"type": "platform", "tenant_id": "2"}},
                       {"principal": {"type": "service_principal"}}, {"authentication": {"assurance_level": "aal1"}},
                       {"delegation": {"id": "delegated"}}, {"token": {"type": "service_access_token"}}):
            with self.subTest(change=change), self.assertRaises(ONLINE.SuiteError):
                ONLINE.platform_identity(dict(identity(), **change), "platform.system_administrator")
        security = identity("platform.security_administrator")
        ONLINE.platform_identity(security, "platform.security_administrator")
        security["authorization"]["role_assignments"][0]["permissions"] = ["platform.host_node.read"]
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.platform_identity(security, "platform.security_administrator")

    def test_collector_requires_exact_service_client_and_only_discovery_permission(self):
        value = identity("system.metrics_discovery")
        value.update(principal={"type": "service_principal"}, token={"type": "service_access_token"}, client={"client_id": "addp-prometheus"})
        value["authorization"]["role_assignments"][0]["permissions"] = ["monitor.metrics_discovery.read"]
        ONLINE.service_identity(value)
        for field, replacement in (("client", {"client_id": "addp-monitor"}), ("principal", {"type": "user"}),
                                   ("context", {"type": "tenant", "tenant_id": "2"}), ("token", {"type": "oauth_access_token"})):
            with self.subTest(field=field), self.assertRaises(ONLINE.SuiteError):
                ONLINE.service_identity(dict(value, **{field: replacement}))
        value["authorization"]["role_assignments"][0]["permissions"].append("platform.host_node.read")
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.service_identity(value)

    def test_failed_saved_version_never_counts_as_applied_discovery(self):
        target = {"id": "target-id", "version": 2, "subject": {"node_id": "node-id"}}
        labels = {"addp_node_id": "node-id", "addp_monitor_kind": "host_resources", "addp_source": "node_exporter",
                  "__meta_addp_target_id": "target-id", "__meta_addp_target_version": "2", "__meta_addp_node_version": "3"}
        client = unittest.mock.Mock()
        client.request.return_value.payload = [{"targets": ["172.17.0.1:19100"], "labels": labels}]
        with patch.dict(ONLINE.os.environ, ADDP_ONLINE_METRICS_NODE_IP="172.17.0.1"):
            ONLINE.assert_discovery(client, target, 3)
            for key in labels:
                bad = copy.deepcopy(labels); bad[key] = "stale"
                client.request.return_value.payload[0]["labels"] = bad
                with self.subTest(key=key), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.assert_discovery(client, target, 3)

    def test_faults_reject_unowned_resources_before_mutation(self):
        with patch.object(ONLINE.FIXTURE, "command", side_effect=["node-container", '[{"Config":{"Labels":{"io.addp.node-metrics.owner":"foreign"}}}]']) as command:
            with self.assertRaises(ONLINE.SuiteError):
                ONLINE.source_action("stop")
            self.assertEqual(command.call_count, 2)
        with patch.object(ONLINE.FIXTURE, "compose") as compose:
            with self.assertRaises(ONLINE.SuiteError):
                ONLINE.owned_action("prometheus", "stop")
            compose.assert_not_called()


if __name__ == "__main__":
    unittest.main()
