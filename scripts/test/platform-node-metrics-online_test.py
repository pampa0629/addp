import copy
import datetime
import tempfile
from pathlib import Path
import importlib
import io
import json
import unittest
from unittest.mock import patch

ONLINE = importlib.import_module("scripts.test.platform-node-metrics-online")


def identity(role="platform.system_administrator"):
    return {"principal": {"type": "user"}, "context": {"type": "platform"},
            "authentication": {"assurance_level": "aal2"}, "token": {"type": "first_party_access_token"},
            "authorization": {"role_assignments": [{"role_key": role, "permissions": sorted(ONLINE.REQUIRED) if role.endswith("system_administrator") else []}]}}


def collector_identity():
    value = identity("system.metrics_discovery")
    value.update(principal={"type": "service_principal", "id": "collector-id"},
                 token={"type": "service_access_token"}, client={"client_id": "addp-prometheus"})
    value["authorization"]["role_assignments"][0]["permissions"] = ["monitor.metrics_discovery.read"]
    return value


class MetricsProtocolTest(unittest.TestCase):
    def test_failure_reason_redacts_credentials_and_opaque_tokens(self):
        with patch.dict(ONLINE.os.environ, ADDP_ONLINE_METRICS_ADMIN_PASSWORD="private-password"):
            message = ONLINE.failure_reason(ONLINE.SuiteError("private-password addp_at_exampleopaquevalue 123456"))
            self.assertEqual(message, "[redacted] [redacted] [redacted]")
            self.assertNotIn("private-password", ONLINE.failure_reason(OSError("private-password")))

    def test_native_oauth_token_type_is_case_insensitive(self):
        for token_type in ("bearer", "Bearer", "BEARER"):
            opener = unittest.mock.Mock()
            opener.open.return_value = io.BytesIO(json.dumps({"access_token": "addp_at_test-only", "token_type": token_type, "scope": "addp.api", "expires_in": 300}).encode())
            with self.subTest(token_type=token_type), patch.dict(ONLINE.os.environ, PROMETHEUS_SERVICE_CLIENT_SECRET="test-only-secret"), patch.object(ONLINE.urllib.request, "build_opener", return_value=opener), patch.object(ONLINE.API.GatewayClient, "request", return_value=ONLINE.API.Response(200, collector_identity())):
                client = ONLINE.machine_client("http://127.0.0.1:8000")
                self.assertEqual(client.token, "addp_at_test-only")
                form = ONLINE.urllib.parse.parse_qs(opener.open.call_args.args[0].data.decode())
                self.assertEqual(form, {"grant_type": ["client_credentials"], "scope": ["addp.api"], "audience": ["addp.api"], "context_type": ["platform"]})

    def test_collector_renews_before_expiry_and_rechecks_the_same_identity(self):
        used = []
        def request(client, method, path, expected, **kwargs):
            used.append((path, client.token))
            return ONLINE.API.Response(200, collector_identity() if path.endswith("auth/context") else [])
        with patch.object(ONLINE, "machine_token", side_effect=[("addp_at_first", 300), ("addp_at_next", 300)]) as grant, patch.object(ONLINE.time, "monotonic", return_value=100) as clock, patch.object(ONLINE.API.GatewayClient, "request", autospec=True, side_effect=request):
            client = ONLINE.machine_client("http://127.0.0.1:8000")
            clock.return_value = 389
            client.request("GET", ONLINE.DISCOVERY, (200,), response_type=list)
            self.assertEqual(grant.call_count, 1)
            clock.return_value = 390
            client.request("GET", ONLINE.DISCOVERY, (200,), response_type=list)
            self.assertEqual(grant.call_count, 2)
            self.assertEqual(client.grant_count, 2)
            self.assertEqual(used, [("/api/v1/system/auth/context", "addp_at_first"), (ONLINE.DISCOVERY, "addp_at_first"), ("/api/v1/system/auth/context", "addp_at_next"), (ONLINE.DISCOVERY, "addp_at_next")])

    def test_collector_rejects_changed_or_expanded_identity_before_business_request(self):
        changes = [lambda v: v["principal"].update(id="other-id"),
                   lambda v: v["authorization"]["role_assignments"][0]["permissions"].append("platform.host_node.read"),
                   lambda v: v.update(context={"type": "tenant", "tenant_id": "2"})]
        for change in changes:
            bad = collector_identity(); change(bad)
            with self.subTest(change=change), patch.object(ONLINE, "machine_token", side_effect=[("addp_at_first", 300), ("addp_at_next", 300)]), patch.object(ONLINE.time, "monotonic", return_value=100) as clock, patch.object(ONLINE.API.GatewayClient, "request", side_effect=[ONLINE.API.Response(200, collector_identity()), ONLINE.API.Response(200, bad)]) as request:
                client = ONLINE.machine_client("http://127.0.0.1:8000")
                clock.return_value = 390
                with self.assertRaises(ONLINE.SuiteError):client.request("GET", ONLINE.DISCOVERY, (200,), response_type=list)
                self.assertEqual(client.token, "")
                self.assertEqual(request.call_count, 2)
                self.assertTrue(all(call.args[1].endswith("auth/context") for call in request.call_args_list))

    def test_collector_does_not_retry_authentication_failure_or_failed_grant(self):
        with patch.object(ONLINE, "machine_token", side_effect=[("addp_at_first", 300), ONLINE.SuiteError("grant denied")]) as grant, patch.object(ONLINE.time, "monotonic", return_value=100) as clock, patch.object(ONLINE.API.GatewayClient, "request", side_effect=[ONLINE.API.Response(200, collector_identity()), ONLINE.SuiteError("HTTP 401")]) as request:
            client = ONLINE.machine_client("http://127.0.0.1:8000")
            with self.assertRaisesRegex(ONLINE.SuiteError, "401"):client.request("GET", ONLINE.DISCOVERY, (200,), response_type=list)
            self.assertEqual(grant.call_count, 1)
            clock.return_value = 390
            with self.assertRaisesRegex(ONLINE.SuiteError, "grant denied"):client.request("GET", ONLINE.DISCOVERY, (200,), response_type=list)
            self.assertEqual(client.token, "")
            self.assertEqual(request.call_count, 2)

    def test_native_oauth_requires_positive_integer_lifetime(self):
        for lifetime in [None, 0, -1, True, "300"]:
            opener = unittest.mock.Mock()
            opener.open.return_value = io.BytesIO(json.dumps({"access_token": "addp_at_test-only", "token_type": "bearer", "scope": "addp.api", "expires_in": lifetime}).encode())
            with self.subTest(lifetime=lifetime), patch.dict(ONLINE.os.environ, PROMETHEUS_SERVICE_CLIENT_SECRET="test-only-secret"), patch.object(ONLINE.urllib.request, "build_opener", return_value=opener), self.assertRaises(ONLINE.SuiteError):
                ONLINE.machine_token("http://127.0.0.1:8000")

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

    def resource_reply(self, keys=None, trend=False, disconnected=False):
        stamp = "2026-10-06T00:01:00Z"
        rows = []
        for key in (ONLINE.METRICS if keys is None else keys):
            points = []
            for offset in (0, 15, 30, 45, 60) if trend else (60,):
                at = "2026-10-06T00:" + ("01:00Z" if offset == 60 else "00:"+f"{offset:02d}"+"Z")
                points.append({"evaluated_at": at, "sampled_at": None if disconnected else at,
                               "value": None if disconnected else {"node.filesystem.total_bytes":100,"node.filesystem.free_bytes":30,"node.filesystem.available_bytes":25,"node.filesystem.used_bytes":70,"node.filesystem.used_percent":100*70/95}.get(key,0), "data_state": "not_connected" if disconnected else "valid"})
            rows.append({"metric_key": key, "dimensions": {"device": "/dev/a", "mountpoint": "/data", "fstype": "ext4"} if key.startswith("node.filesystem.") and not disconnected else {}, "unit": ONLINE.ALL_METRICS[key], "window_seconds": 60 if key == "node.cpu.busy_percent" else 0, "points": points})
        return {"subject": {"kind": "node", "node_id": "node-id"}, "node_version": 1,
                "target_id": "target-id", "target_saved_version": 2, "policy_version": 0,
                "lookback_seconds": 300, "queried_at": stamp, "start": "2026-10-06T00:00:00Z" if trend else stamp,
                "end": stamp, "step_seconds": 15 if trend else 0, "series": rows}

    def test_initial_resource_readiness_waits_for_data_without_hiding_schema_or_identity_errors(self):
        node, target = {"node_id": "node-id", "version": 1}, {"id": "target-id", "version": 2}
        missing = self.resource_reply()
        for row in missing["series"]:
            row["points"][0].update(data_state="no_data", sampled_at=None, value=None)
        client = unittest.mock.Mock()
        client.request.return_value = ONLINE.API.Response(200, missing)
        self.assertFalse(ONLINE.resource_query_ready(client, node, target))
        self.assertEqual(client.request.call_args.args[2], (200,))
        client.request.return_value = ONLINE.API.Response(200, self.resource_reply())
        self.assertTrue(ONLINE.resource_query_ready(client, node, target))
        for mutation in (lambda v: v.update(node_version=2), lambda v: v.update(target_saved_version=1),
                         lambda v: v["series"].pop(), lambda v: v["series"][0]["points"][0].update(value=0)):
            bad = copy.deepcopy(missing); mutation(bad)
            client.request.return_value = ONLINE.API.Response(200, bad)
            with self.subTest(mutation=mutation), self.assertRaises(ONLINE.SuiteError):
                ONLINE.resource_query_ready(client, node, target)
        client.request.return_value = ONLINE.API.Response(403, self.resource_reply())
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.resource_query_ready(client, node, target)

    def test_cpu_busy_window_and_percentage_cannot_be_forged(self):
        node, target = {"node_id": "node-id", "version": 1}, {"id": "target-id", "version": 2}
        value = self.resource_reply(keys=["node.cpu.busy_percent"])
        ONLINE.assert_resources(value, node, target, keys=["node.cpu.busy_percent"])
        for window, number in ((0, 0), (15, 0), ("60", 0), (60, 101)):
            bad = copy.deepcopy(value)
            bad["series"][0]["window_seconds"] = window
            bad["series"][0]["points"][0]["value"] = number
            with self.subTest(window=window, number=number), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_resources(bad, node, target, keys=["node.cpu.busy_percent"])

    def test_resource_evidence_accepts_zero_and_rejects_stale_grid_or_forged_missing_values(self):
        node, target = {"node_id": "node-id", "version": 1}, {"id": "target-id", "version": 2}
        for trend in (False, True):
            value = self.resource_reply(trend=trend)
            ONLINE.assert_resources(value, node, target, trend)
            stopped = self.resource_reply(trend=trend, disconnected=True)
            ONLINE.assert_resources(stopped, node, trend=trend, disconnected=True)
            mutations = [lambda v: v.update(node_version=2), lambda v: v.update(target_saved_version=1),
                         lambda v: v["series"][0]["points"][0].update(sampled_at="2026-10-05T23:58:59Z"),
                         lambda v: v["series"][0]["points"][0].update(data_state="no_data"),
                         lambda v: v["series"][0]["points"][0].update(value=float("nan")),
                         lambda v: v["series"][0]["points"].clear()]
            for mutation in mutations:
                bad = copy.deepcopy(value); mutation(bad)
                with self.subTest(trend=trend, mutation=mutation), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.assert_resources(bad, node, target, trend)
            stopped["series"][0]["points"][0]["value"] = 0
            with self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_resources(stopped, node, trend=trend, disconnected=True)

    def test_filesystem_protocol_rejects_duplicate_or_private_mount_dimensions(self):
        node, target = {"node_id": "node-id", "version": 1}, {"id": "target-id", "version": 2}
        value = self.resource_reply(ONLINE.FILESYSTEM_METRICS)
        ONLINE.assert_resources(value, node, target, keys=ONLINE.FILESYSTEM_METRICS)
        for mutation in (lambda v: v["series"][0].update(dimensions={}), lambda v: v["series"][0]["dimensions"].update(private="secret"), lambda v: v["series"].append(v["series"][0]), lambda v: v["series"][0]["dimensions"].update(mountpoint="/foreign")):
            bad = copy.deepcopy(value); mutation(bad)
            with self.subTest(mutation=mutation), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_resources(bad, node, target, keys=ONLINE.FILESYSTEM_METRICS)

    def test_budget_verifies_cas_hot_consumption_and_restoration(self):
        initial = {"version": 0, "max_metrics": 12, "pending_restart": False}
        current, writes = dict(initial), []
        def request(method, path, expected, body=None):
            nonlocal current
            status = 200
            if path == ONLINE.QUERY_POLICY:
                if method == "PUT":
                    writes.append(dict(body))
                    if body["version"] != current["version"]:
                        status = 409
                    else:
                        current = dict(body, version=body["version"]+1, pending_restart=False)
                payload = dict(current)
            else:
                keys = ONLINE.urllib.parse.parse_qs(ONLINE.urllib.parse.urlsplit(path).query)["metrics"][0].split(",")
                if len(keys) > current["max_metrics"]:
                    status, payload = 422, {"error_code": "observability_query_budget_exceeded"}
                else:
                    payload = self.resource_reply(keys=keys)
                    payload["policy_version"] = current["version"]
            self.assertIn(status, expected)
            return ONLINE.API.Response(status, payload)
        client = unittest.mock.Mock(); client.request.side_effect = request
        restored = ONLINE.check_query_policy(client, {"node_id": "node-id", "version": 1})
        self.assertEqual(restored["version"], 2)
        self.assertEqual(restored["max_metrics"], initial["max_metrics"])
        self.assertTrue(all("pending_restart" not in write for write in writes))

    def test_recovery_requires_new_samples_for_every_metric_and_current_versions(self):
        node, target = {"node_id": "node-id", "version": 1}, {"id": "target-id", "version": 2}
        value = self.resource_reply()
        sampled = ONLINE.utc_timestamp(value["series"][0]["points"][0]["sampled_at"])
        client = unittest.mock.Mock()
        client.request.return_value = ONLINE.API.Response(200, value)
        # The opening sample can be fresh while still predating re-enablement.
        self.assertFalse(ONLINE.resource_query_after(client, node, target, sampled+1))
        self.assertFalse(ONLINE.resource_query_after(client, node, target, sampled))
        self.assertTrue(ONLINE.resource_query_after(client, node, target, sampled-1))
        for state in ("no_data", "stale", "not_connected"):
            delayed = copy.deepcopy(value)
            delayed["series"][-1]["points"][0]["data_state"] = state
            client.request.return_value = ONLINE.API.Response(200, delayed)
            self.assertFalse(ONLINE.resource_query_after(client, node, target, sampled-1))
        for status in (503, 504):
            client.request.return_value = ONLINE.API.Response(status, {})
            self.assertFalse(ONLINE.resource_query_after(client, node, target, sampled-1))
        for change in ({"node_version": 0}, {"target_saved_version": 1}):
            client.request.return_value = ONLINE.API.Response(200, dict(value, **change))
            with self.subTest(change=change), self.assertRaises(ONLINE.SuiteError):
                ONLINE.resource_query_after(client, node, target, sampled-1)

    def test_browser_report_binds_real_identity_versions_server_time_and_screenshots(self):
        expected = {"run_id": "unique-run", "node": {"node_id": "node-id", "version": 1},
                    "target": {"id": "target-id", "version": 2}, "policy_version": 0,
                    "admin_id": "admin-id", "security_id": "security-id"}
        admin, security = identity(), identity("platform.security_administrator")
        admin["principal"]["id"], security["principal"]["id"] = "admin-id", "security-id"
        instant = self.resource_reply()
        rows = [{"path": ONLINE.OBSERVATIONS, "query": {"node_id": "node-id", "metrics": ",".join(ONLINE.METRICS)}, "value": instant}]
        for key, span in (("node.memory.used_percent", 3600), ("node.cpu.busy_percent", 300), ("node.filesystem.used_percent", 300)):
            value = self.resource_reply([key], trend=True)
            end = datetime.datetime.fromisoformat(value["end"].replace("Z", "+00:00"))
            start = end - datetime.timedelta(seconds=span)
            stamp = lambda at: at.isoformat().replace("+00:00", "Z")
            value["start"] = stamp(start)
            value["series"][0]["points"] = [{"evaluated_at": stamp(start+datetime.timedelta(seconds=index*15)),
                "sampled_at": stamp(start+datetime.timedelta(seconds=index*15)), "value": 0, "data_state": "valid"} for index in range(span//15+1)]
            query = {"node_id": "node-id", "metrics": key, "start": value["start"], "end": value["end"]}
            if key.startswith("node.filesystem."): query.update(value["series"][0]["dimensions"])
            rows.append({"path": ONLINE.TRENDS, "query": query, "value": value})
        rows.append(copy.deepcopy(rows[0]))
        rows.append({"path": ONLINE.OBSERVATIONS, "query": {"node_id": "node-id", "metrics": ",".join(ONLINE.FILESYSTEM_METRICS)}, "value": self.resource_reply(ONLINE.FILESYSTEM_METRICS)})
        report = {"schema_version": "addp.node-resources-browser/v1", "result": "passed", "stage": "complete", "run_id": "unique-run",
                  "identity": admin, "negative_identity": security, "negative_no_business_reads": True, "resources": rows,
                  "auto_refresh": {"natural_timer": True, "server_end_advanced": True, "unchanged_url": True, "off_restored": True, "off_no_requests": True},
                  "navigation": {"list_without_fanout": True, "iframe_preserved": True, "history": True, "metric_reload": True, "range_reload": True, "server_window": True, "filesystem_reload": True}}
        with tempfile.TemporaryDirectory() as directory:
            artifacts = Path(directory)
            for name in ("list", "detail", "restored", "filesystem"):
                (artifacts / ("node-resources-" + name + ".png")).write_bytes(b"\x89PNG\r\n\x1a\n" + b"x"*1000)
            self.assertEqual(ONLINE.validate_resource_browser(report, expected, artifacts)["result"], "passed")
            mutations = [lambda v: v.update(result="failed"), lambda v: v.update(run_id="other-run"),
                         lambda v: v["identity"]["principal"].update(id="foreign"),
                         lambda v: v["negative_identity"].update(context={"type": "tenant", "tenant_id": "2"}),
                         lambda v: v.update(negative_no_business_reads=False), lambda v: v["navigation"].update(history=False),
                         lambda v: v["resources"][0]["value"].update(policy_version=1),
                         lambda v: v["resources"][1]["value"].update(target_saved_version=1),
                         lambda v: v["resources"][1]["query"].update(end="2026-10-06T00:01:01Z"),
                         lambda v: v["resources"][1]["value"]["series"][0]["points"].pop(),
                         lambda v: v["resources"][0]["value"]["series"][0]["points"][0].update(value=float("nan")),
                         lambda v: v.update(resources=[]), lambda v: v.pop("auto_refresh"),
                         lambda v: v["auto_refresh"].update(off_no_requests=False)]
            for mutation in mutations:
                bad = copy.deepcopy(report); mutation(bad)
                with self.subTest(mutation=mutation), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.validate_resource_browser(bad, expected, artifacts)
            (artifacts / "node-resources-detail.png").unlink()
            with self.assertRaises(ONLINE.SuiteError):
                ONLINE.validate_resource_browser(report, expected, artifacts)

    def test_browser_failure_cannot_reuse_report_or_leak_subprocess_output(self):
        with tempfile.TemporaryDirectory() as directory:
            artifacts = Path(directory)
            evidence = artifacts / "node-resources-browser.json"
            evidence.write_text('{"result":"passed"}')
            with patch.dict(ONLINE.os.environ, ADDP_ONLINE_ARTIFACT_DIR=directory, ADDP_ONLINE_SECRET_DIR=directory+"/private", ADDP_ONLINE_TEST_RUN_ID="run"), \
                 patch.object(ONLINE.subprocess, "run", return_value=unittest.mock.Mock(returncode=1, stdout="sensitive", stderr="sensitive")), \
                 patch("sys.stdout", new_callable=io.StringIO) as output:
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.run_resource_browser({"node_id": "node", "version": 1}, {"id": "target", "version": 2}, {"version": 0}, "node", {"principal": {"id": "admin"}}, {"principal": {"id": "security"}})
                self.assertFalse(evidence.exists())
                self.assertNotIn("sensitive", output.getvalue())
                value = json.loads((artifacts / "node-resources-browser-input.json").read_text())
                self.assertEqual(set(value["node"]), {"node_id", "version"})

    def test_center_fault_requires_hosted_boundary_and_owned_infra_identity(self):
        with patch.object(ONLINE.FIXTURE, "boundary", side_effect=ValueError("personal")), patch.object(ONLINE.FIXTURE, "command") as command:
            with self.assertRaises(ValueError):
                ONLINE.center_action("stop")
            command.assert_not_called()
        with patch.object(ONLINE.FIXTURE, "boundary"), patch.object(ONLINE.FIXTURE, "center_identity", side_effect=ValueError("foreign")), patch.object(ONLINE.FIXTURE, "command") as command:
            with self.assertRaises(ValueError):
                ONLINE.center_action("stop")
            command.assert_not_called()

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
