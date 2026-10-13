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
    def test_online_module_syntax_rejects_duplicate_callback_before_other_gate_stages(self):
        root = Path(__file__).resolve().parents[2]
        for name, target, separated in (("platform-node-resources", "test-node-metrics-online-runner", False),
                                        ("transfer-relational-sql-etl", "test-transfer-online-runner", False),
                                        ("manager-internal-artifact-lineage", "test-manager-online-runner", False),
                                        ("platform-node-resources", "test-node-metrics-online-runner", True)):
            with self.subTest(source=name, separated=separated), tempfile.TemporaryDirectory() as directory:
                source = root / "console/frontend/e2e/online" / (name+".spec.js")
                original = source.read_text()
                self.assertIn("try {", original)
                if separated:
                    self.assertIn("const beforeServiceAuto =", original)
                    invalid = original.replace("const beforeServiceAuto =", "const beforeAuto =", 1)
                else:
                    invalid = original.replace("try {", "try { const addpInvalidSyntax = 1; const addpInvalidSyntax = 2;", 1)
                copied = Path(directory) / source.relative_to(root)
                copied.parent.mkdir(parents=True)
                copied.write_text(invalid)
                (Path(directory) / "console/frontend/package.json").write_text((root / "console/frontend/package.json").read_text())
                result = ONLINE.subprocess.run(["make", "-f", str(root / "Makefile"), target], cwd=directory,
                                               capture_output=True, text=True, timeout=20)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("SyntaxError", result.stderr)
                self.assertIn("already been declared", result.stderr)
                self.assertNotIn("ModuleNotFoundError", result.stderr)

    def process_reply(self):
        owners = [{"id": i, "instance_id": module+"-runtime", "module_name": module, "role": "backend", "node_id": "",
                   "status": "up", "process_metrics_declared": True, "process_started_at": "2026-10-11T00:00:00Z"}
                  for i, module in ((7, "system"), (9, "monitor"))]
        data = []
        for owner in owners:
            data.append({"subject": {"kind": "module_instance", **{k: owner[k] for k in ("id", "instance_id", "module_name", "role")}},
                         "node_id": "", "queried_at": "2026-10-11T01:00:00Z", "policy_version": 4, "lookback_seconds": 300,
                         "collection": {"state": "collecting", "sampled_at": "2026-10-11T00:59:55Z"},
                         "series": [{"metric_key": key, "unit": unit, "window_seconds": window, "dimensions": {},
                                     "points": [{"evaluated_at": "2026-10-11T01:00:00Z", "sampled_at": "2026-10-11T00:59:55Z", "data_state": "valid",
                                                 "value": 0.25 if unit == "cores" else 100*1024**2 if unit == "bytes" else 3600}]}
                                    for key, (unit, window) in ONLINE.PROCESS_METRICS.items()]})
        return owners, {"data": data}

    def test_instance_times_preserve_offset_microseconds_and_resource_utc_contract(self):
        owners, reply = self.process_reply()
        owners[0]["process_started_at"] = "2026-10-11T08:00:00+08:00"
        owners[1]["process_started_at"] = "2026-10-10T20:00:00-04:00"
        ONLINE.assert_process_summaries(reply, owners)
        self.assertEqual(ONLINE.instance_timestamp("2026-10-11T08:00:00.123456+08:00"),
                         ONLINE.utc_timestamp("2026-10-11T00:00:00.123456Z"))
        self.assertGreater(ONLINE.instance_timestamp("2026-10-10T20:00:00.123457-04:00"),
                           ONLINE.instance_timestamp("2026-10-11T08:00:00.123456+08:00"))
        for value in (None, "2026-10-11T08:00:00", "2026-10-11", "2026-10-11T08:00:00+25:00", "2026-02-30T08:00:00Z"):
            with self.subTest(value=value), self.assertRaises(ONLINE.SuiteError): ONLINE.instance_timestamp(value)
        reply["data"][0]["queried_at"] = "2026-10-11T09:00:00+08:00"
        with self.assertRaisesRegex(ONLINE.SuiteError, "resource time must be UTC"):
            ONLINE.assert_process_summaries(reply, owners)

    def test_process_proof_rejects_foreign_identity_percent_cpu_missing_windows_and_fabricated_values(self):
        owners, reply = self.process_reply()
        ONLINE.assert_process_summaries(reply, owners)
        mutations = [lambda v: v["data"][0]["subject"].update(instance_id="foreign"),
                     lambda v: v["data"][1].update(subject=v["data"][0]["subject"]),
                     lambda v: v["data"][0]["series"][0].update(unit="percent"),
                     lambda v: v["data"][0]["series"][0].update(window_seconds=0),
                     lambda v: v["data"][0]["series"][0]["points"][0].update(data_state="no_data", value=None, sampled_at=None),
                     lambda v: v["data"][0]["series"][1]["points"][0].update(value=0),
                     lambda v: v["data"][0]["series"][1]["points"][0].update(value=True),
                     lambda v: v["data"][0]["series"][1]["points"][0].update(value=float("nan")),
                     lambda v: v["data"][0]["series"][2]["points"][0].update(value=3601),
                     lambda v: v["data"][0]["collection"].update(sampled_at="2026-10-11T01:00:01Z"),
                     lambda v: v["data"][1].update(policy_version=5), lambda v: v["data"].pop()]
        for change in mutations:
            bad = copy.deepcopy(reply); change(bad)
            with self.subTest(change=change), self.assertRaises(ONLINE.SuiteError): ONLINE.assert_process_summaries(bad, owners)

    def test_process_warmup_and_stopped_identity_cannot_borrow_live_peer_values(self):
        owners, reply = self.process_reply()
        point = reply["data"][0]["series"][0]["points"][0]
        point.update(data_state="no_data", value=None, sampled_at=None)
        ONLINE.assert_process_summaries(reply, owners, fresh=False)
        client = unittest.mock.Mock(request=unittest.mock.Mock(return_value=ONLINE.API.Response(200, reply)))
        self.assertFalse(ONLINE.process_query_ready(client, owners))
        stopped = reply["data"][0]
        stopped["collection"] = {"state": "not_active", "sampled_at": None}
        for series in stopped["series"]: series["points"][0].update(data_state="not_active", value=None, sampled_at=None)
        ONLINE.assert_process_summaries(reply, owners, inactive=[7])
        self.assertTrue(ONLINE.process_query_ready(client, owners, inactive=[7]))
        stopped["series"][1]["points"][0] = copy.deepcopy(reply["data"][1]["series"][1]["points"][0])
        with self.assertRaises(ONLINE.SuiteError): ONLINE.assert_process_summaries(reply, owners, inactive=[7])

    def test_outage_probe_retains_safe_diagnostics_without_retrying_or_relaxing_readiness(self):
        for mode in ("ready", "warmup", "failed"):
            with self.subTest(mode=mode):
                owners, reply = self.process_reply()
                row = reply["data"][0]
                if mode == "warmup":
                    row["series"][0]["points"][0].update(data_state="no_data", value=None, sampled_at=None)
                elif mode == "failed":
                    row["collection"]["state"] = "failed"
                    for series in row["series"]:
                        series["points"][0].update(data_state="no_data", value=None, sampled_at=None)
                for target in (reply, row, row["collection"], row["series"][0], row["series"][0]["points"][0]):
                    target["unexpected_credential"] = "must-never-retain"
                before = copy.deepcopy(reply)
                client = unittest.mock.Mock(request=unittest.mock.Mock(return_value=ONLINE.API.Response(200, reply)))
                evidence = {}
                self.assertEqual(ONLINE.process_query_ready(client, owners, evidence=evidence), mode == "ready")
                client.request.assert_called_once_with("GET", ONLINE.process_path(owners), (200,))
                self.assertEqual(reply, before)
                self.assertNotIn("must-never-retain", json.dumps(evidence))
                self.assertEqual(len(evidence["data"]), 2)
                captured = evidence["data"][0]
                self.assertEqual(captured["subject"], row["subject"])
                self.assertEqual(captured["queried_at"], row["queried_at"])
                self.assertEqual(captured["collection"]["state"], row["collection"]["state"])
                self.assertEqual(captured["series"][0]["points"][0], {key: row["series"][0]["points"][0][key]
                    for key in ("value", "sampled_at", "evaluated_at", "data_state")})

    def test_source_isolation_witness_is_anchored_bounded_safe_and_preserves_original_failure(self):
        for mode in ("ready", "warmup", "failed", "reordered", "unavailable", "duplicate", "nonfinite", "wrong_time"):
            with self.subTest(mode=mode):
                owners, reply = self.process_reply()
                at = reply["data"][0]["series"][0]["points"][0]["evaluated_at"]
                row = reply["data"][0]
                if mode != "ready":
                    row["series"][0]["points"][0].update(data_state="no_data", value=None, sampled_at=None)
                if mode == "failed":
                    row["collection"]["state"] = "failed"
                    for series in row["series"]:
                        series["points"][0].update(data_state="no_data", value=None, sampled_at=None)
                if mode == "reordered": reply["data"].reverse()
                client = unittest.mock.Mock(request=unittest.mock.Mock(return_value=ONLINE.API.Response(200, reply)))
                point = {"metric": {"addp_probe": "cpu_window_count", "credential": "must-never-retain"},
                         "value": [ONLINE.utc_timestamp(at), "3"], "credential": "must-never-retain"}
                values = [point]
                if mode == "duplicate": values = [point, copy.deepcopy(point)]
                if mode == "nonfinite": point["value"][1] = "NaN"
                if mode == "wrong_time": point["value"][0] -= 1
                prom = unittest.mock.Mock(query=unittest.mock.Mock(return_value=values))
                if mode == "unavailable": prom.query.side_effect = ValueError("must-never-retain")
                report = {}
                if mode == "ready":
                    ONLINE.assert_source_process_isolation(client, prom, owners, report)
                    prom.query.assert_not_called()
                    self.assertNotIn("source_outage_process_window", report)
                else:
                    with self.assertRaisesRegex(ONLINE.SuiteError, "host source failure broke independent process resources"):
                        ONLINE.assert_source_process_isolation(client, prom, owners, report)
                    self.assertEqual(prom.query.call_count, 2)
                    for call, owner in zip(prom.query.call_args_list, owners):
                        self.assertEqual(call.kwargs, {"at": at})
                        expression = call.args[0]
                        self.assertIn('addp_instance_id='+json.dumps(owner["instance_id"]), expression)
                        self.assertIn('addp_module_name='+json.dumps(owner["module_name"]), expression)
                        for fragment in ("count_over_time(", "resets(", " offset 1m", "process_start_time_seconds", "addp_process_identity_info", 'addp_probe'):
                            self.assertIn(fragment, expression)
                        self.assertNotIn('job="prometheus"', expression)
                    witnesses = report["source_outage_process_window"]
                    self.assertEqual(len(witnesses), 2)
                    for witness in witnesses:
                        source = next(row for row in reply["data"] if row["subject"]["id"] == witness["subject"]["id"])
                        self.assertEqual(witness["subject"], source["subject"])
                        self.assertEqual(witness["evaluated_at"], at)
                        self.assertEqual(witness["state"], "captured" if mode in {"warmup", "failed", "reordered"} else "unavailable")
                        if witness["state"] == "captured":
                            self.assertEqual(witness["values"]["cpu_window_count"], 3)
                            self.assertIsNone(witness["values"]["cpu_previous_stamp"])
                        else:
                            self.assertNotIn("values", witness)
                    self.assertNotIn("must-never-retain", json.dumps(report))
                client.request.assert_called_once_with("GET", ONLINE.process_path(owners), (200,))

    def test_prometheus_diagnostic_time_uses_the_existing_query_route(self):
        prom = ONLINE.Prometheus.__new__(ONLINE.Prometheus)
        prom.request = unittest.mock.Mock(return_value={"result": []})
        at = "2026-10-13T00:32:11Z"
        self.assertEqual(prom.query("count(up)", at=at), [])
        path = prom.request.call_args.args[0]
        self.assertEqual(ONLINE.urllib.parse.urlparse(path).path, "/api/v1/query")
        self.assertEqual(ONLINE.urllib.parse.parse_qs(ONLINE.urllib.parse.urlparse(path).query), {"query": ["count(up)"], "time": [at]})
        prom.query("up")
        self.assertEqual(ONLINE.urllib.parse.parse_qs(ONLINE.urllib.parse.urlparse(prom.request.call_args.args[0]).query), {"query": ["up"]})

    def test_process_browser_binds_actual_owner_page_scope_and_requires_units_screenshot_and_denial(self):
        owners, reply = self.process_reply()
        expected = {"processes": owners, "policy_version": 4}
        batch = {"path": ONLINE.PROCESS_SUMMARIES, "query": {"instance_ids": "7,9"}, "value": reply,
                 "instance_query": {"page": "1", "page_size": "20", "role": "backend", "status": "up", "process_metrics_declared": "true"},
                 "instances": {"data": owners, "total": 2, "page": 1, "page_size": 20}}
        report = {"service_batches": [batch, copy.deepcopy(batch)], "service_presentation": dict.fromkeys(
            ("cpu_cores", "rss_iec", "elapsed_uptime", "inline_hints", "sample_time_visible", "unbound_host", "url_restore", "iframe_preserved", "negative_no_reads"), True)}
        trend = {k: copy.deepcopy(v) for k, v in reply["data"][0].items() if k != "collection"}
        trend.update(start="2026-10-11T00:55:00Z", end="2026-10-11T01:00:00Z", step_seconds=15)
        trend["series"] = trend["series"][:2]
        for line in trend["series"]:
            point = line["points"][0]
            line["points"] = []
            for index in range(21):
                at = datetime.datetime(2026, 10, 11, 0, 55, tzinfo=datetime.timezone.utc) + datetime.timedelta(seconds=15*index)
                stamp = at.isoformat().replace("+00:00", "Z")
                line["points"].append({**point, "evaluated_at": stamp, "sampled_at": stamp})
        entry = {"path": "/api/v1/monitor/platform/process_resource_trends", "query": {"instance_id": "7", "start": trend["start"], "end": trend["end"]}, "value": trend}
        report["service_trends"] = [entry, copy.deepcopy(entry)]
        report["process_trend_presentation"] = dict.fromkeys(("cpu_cores", "rss_iec", "same_current_instance", "startup_bound", "url_restore"), True)
        def advance(value):
            if isinstance(value, dict): return {key: advance(item) for key, item in value.items()}
            if isinstance(value, list): return [advance(item) for item in value]
            if isinstance(value, str) and value.startswith("2026-") and value.endswith("Z"):
                return (datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))+datetime.timedelta(seconds=15)).isoformat().replace("+00:00", "Z")
            return value
        next_batch = copy.deepcopy(batch)
        next_batch["value"] = advance(batch["value"])
        for row in next_batch["value"]["data"]:
            next(line for line in row["series"] if line["metric_key"] == "process.uptime_seconds")["points"][0]["value"] += 15
        report["service_auto_refresh"] = {**dict.fromkeys(("natural_timer", "server_end_advanced", "unchanged_url", "iframe_preserved", "off_restored", "off_no_requests"), True),
            "batches": [copy.deepcopy(batch), next_batch], "trends": [copy.deepcopy(entry), advance(entry)]}
        with tempfile.TemporaryDirectory() as directory:
            artifacts = Path(directory)
            screenshot = artifacts / "node-resources-services.png"
            screenshot.write_bytes(b"\x89PNG\r\n\x1a\n"+b"x"*1001)
            (artifacts / "node-resources-process-trends.png").write_bytes(screenshot.read_bytes())
            (artifacts / "node-resources-process-trends-memory.png").write_bytes(screenshot.read_bytes())
            self.assertEqual(ONLINE.validate_process_browser(report, expected, artifacts)["result"], "passed")
            mutations = [lambda v: v.pop("service_auto_refresh"),
                         lambda v: v["service_auto_refresh"].update(off_no_requests=False),
                         lambda v: v["service_auto_refresh"].update(trends=[]),
                         lambda v: v["service_auto_refresh"]["trends"][1].update(value=copy.deepcopy(v["service_auto_refresh"]["trends"][0]["value"]), query=copy.deepcopy(v["service_auto_refresh"]["trends"][0]["query"])),
                         lambda v: v["service_auto_refresh"]["batches"][1]["query"].update(instance_ids="7,9,12"),
                         lambda v: v["service_auto_refresh"]["trends"][1]["value"]["subject"].update(instance_id="new-runtime"),
                         lambda v: v["service_auto_refresh"]["batches"][1]["value"]["data"][0].update(policy_version=0),
                         lambda v: v["process_trend_presentation"].update(startup_bound=False),
                         lambda v: v["service_trends"][0]["value"]["subject"].update(instance_id="previous"),
                         lambda v: v["service_trends"][0]["value"]["series"][0]["points"][0].update(value=-1),
                         lambda v: v["service_trends"][0]["value"].update(start="2026-10-10T00:00:00Z"),
                         lambda v: v["service_presentation"].update(cpu_cores=False),
                         lambda v: v["service_presentation"].update(negative_no_reads=False),
                         lambda v: v["service_batches"][0]["query"].update(instance_ids="7,9,12"),
                         lambda v: v["service_batches"][0]["instance_query"].pop("status"),
                         lambda v: v["service_batches"][0]["instance_query"].pop("process_metrics_declared"),
                         lambda v: v["service_batches"][0]["instances"]["data"][0].update(instance_id="old-runtime"),
                         lambda v: v["service_batches"][0]["value"]["data"][0].update(policy_version=3),
                         lambda v: v.update(service_batches=[])]
            for change in mutations:
                bad = copy.deepcopy(report); change(bad)
                with self.subTest(change=change), self.assertRaises(ONLINE.SuiteError): ONLINE.validate_process_browser(bad, expected, artifacts)
            screenshot.unlink()
            with self.assertRaises(ONLINE.SuiteError): ONLINE.validate_process_browser(report, expected, artifacts)

    def test_process_lifecycle_refuses_personal_host_before_running_standard_scripts(self):
        with patch.object(ONLINE.FIXTURE, "boundary", side_effect=ValueError("not Hosted")), patch.object(ONLINE.subprocess, "run") as run:
            with self.assertRaises(ValueError): ONLINE.monitor_lifecycle(Path("/tmp/private-test"), "stop")
            run.assert_not_called()

    def test_restart_validates_standard_live_build_identity_and_ready_status(self):
        live = {"status": "live", "module": "monitor", "git_commit": "test-commit", "build_id": "test-build",
                "source_fingerprint": "sha256:test-source", "built_at": "2026-10-11T00:00:00Z", "started_at": "2026-10-11T01:00:00Z"}
        cases = [(live, {"status": "ready"}, False),
                 (dict(live, status="alive"), {"status": "ready"}, True),
                 (dict(live, git_commit="foreign-commit"), {"status": "ready"}, True),
                 (dict(live, source_fingerprint="unknown"), {"status": "ready"}, True),
                 (live, {"status": "not_ready"}, True)]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for identity, readiness, fails in cases:
                with self.subTest(identity=identity, readiness=readiness), \
                     patch.object(ONLINE.FIXTURE, "boundary", return_value=root), \
                     patch.object(ONLINE.FIXTURE, "command", return_value="test-commit\n"), \
                     patch.object(ONLINE.subprocess, "run") as launch, \
                     patch.object(ONLINE.PREFLIGHT, "load_health", side_effect=[identity, readiness]) as health, \
                     patch.dict(ONLINE.os.environ, MONITOR_URL="http://127.0.0.1:8100"):
                    if fails:
                        with self.assertRaises(ONLINE.SuiteError): ONLINE.monitor_lifecycle(root, "start")
                    else:
                        ONLINE.monitor_lifecycle(root, "start")
                        self.assertEqual([call.args[1] for call in health.call_args_list], ["/health/live", "/health/ready"])
                    self.assertEqual(launch.call_args.args[0], ["bash", "scripts/dev/start.sh", "-monitor"])

    def test_optional_source_failure_restores_private_key_on_assertion_failure(self):
        owners, _ = self.process_reply()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); (root / "process-monitor").mkdir()
            key = root / "process-monitor/server.key"; key.write_text("test-only-key")
            with patch.object(ONLINE, "monitor_lifecycle") as lifecycle, patch.object(ONLINE, "instance_list", side_effect=ONLINE.SuiteError("protocol failure")):
                with self.assertRaises(ONLINE.SuiteError): ONLINE.check_optional_process_source(None, None, root, owners, [], {})
                self.assertEqual([call.args[1] for call in lifecycle.call_args_list], ["stop", "start", "stop", "start"])
            self.assertEqual(key.read_text(), "test-only-key")
            self.assertFalse((root / "process-monitor-key-withheld").exists())

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
            ONLINE.assert_discovery(client, target, 3, [])
            for key in labels:
                bad = copy.deepcopy(labels); bad[key] = "stale"
                client.request.return_value.payload[0]["labels"] = bad
                with self.subTest(key=key), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.assert_discovery(client, target, 3, [])

    def resource_reply(self, keys=None, trend=False, disconnected=False):
        stamp = "2026-10-06T00:01:00Z"
        rows = []
        for key in (ONLINE.METRICS if keys is None else keys):
            points = []
            for offset in (0, 15, 30, 45, 60) if trend else (60,):
                at = "2026-10-06T00:" + ("01:00Z" if offset == 60 else "00:"+f"{offset:02d}"+"Z")
                points.append({"evaluated_at": at, "sampled_at": None if disconnected else at,
                               "value": None if disconnected else {"node.filesystem.total_bytes":100,"node.filesystem.free_bytes":30,"node.filesystem.available_bytes":25,"node.filesystem.used_bytes":70,"node.filesystem.used_percent":100*70/95,"node.filesystem.inodes_total":100,"node.filesystem.inodes_free":80,"node.filesystem.inodes_used":20,"node.filesystem.inodes_used_percent":20}.get(key,0), "data_state": "not_connected" if disconnected else "valid"})
            rows.append({"metric_key": key, "dimensions": {"device": "/dev/a", "mountpoint": "/data", "fstype": "ext4"} if key.startswith("node.filesystem.") and not disconnected else {"device": "sda"} if (key in ONLINE.DISK_METRICS or key in ONLINE.NETWORK_METRICS) and not disconnected else {}, "unit": ONLINE.ALL_METRICS[key], "window_seconds": 60 if key == "node.cpu.busy_percent" or key in ONLINE.DISK_METRICS or key in ONLINE.NETWORK_METRICS else 0, "points": points})
        return {"subject": {"kind": "node", "node_id": "node-id"}, "node_version": 1,
                "target_id": "target-id", "target_saved_version": 2, "policy_version": 0,
                "lookback_seconds": 300, "queried_at": stamp, "start": "2026-10-06T00:00:00Z" if trend else stamp,
                "end": stamp, "step_seconds": 15 if trend else 0, "series": rows,
                **({} if trend else {"collection": {"state": "not_connected" if disconnected else "collecting", "sampled_at": None if disconnected else stamp, "filesystem": "unknown" if disconnected else "available", "network": "unknown" if disconnected else "available"}})}

    def test_resource_collection_requires_network_evidence_and_rejects_old_success(self):
        node = {"node_id": "node-id", "version": 1}
        for mutation in (lambda c: c.pop("network"), lambda c: c.update(network="unexpected"),
                         lambda c: c.update(state="failed"), lambda c: c.update(sampled_at="2026-10-05T00:00:00Z")):
            value = self.resource_reply()
            mutation(value["collection"])
            with self.subTest(mutation=mutation), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_resources(value, node)
        value = self.resource_reply()
        value["collection"].update(state="failed", filesystem="unknown", network="unknown")
        ONLINE.assert_resources(value, node)

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

    def test_inode_protocol_checks_integer_formula_and_atomic_sample_evidence(self):
        node, target = {"node_id": "node-id", "version": 1}, {"id": "target-id", "version": 2}
        value = self.resource_reply(ONLINE.INODE_METRICS)
        ONLINE.assert_resources(value, node, target, keys=ONLINE.INODE_METRICS)
        self.assertEqual(len(ONLINE.fresh_resource_groups(value, ONLINE.INODE_METRICS)), 1)
        for index, number in ((0, 0), (0, 2**53), (1, 80.5), (1, 101), (2, 21), (3, 100)):
            bad = copy.deepcopy(value); bad["series"][index]["points"][0]["value"] = number
            with self.subTest(index=index, number=number), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_resources(bad, node, target, keys=ONLINE.INODE_METRICS)
        bad = copy.deepcopy(value); bad["series"][0]["points"][0]["sampled_at"] = "2026-10-06T00:00:59Z"
        with self.assertRaises(ONLINE.SuiteError): ONLINE.assert_resources(bad, node, target, keys=ONLINE.INODE_METRICS)

    def test_unsupported_inode_mounts_are_null_but_do_not_prove_a_successful_read(self):
        node = {"node_id": "node-id", "version": 1}
        value = self.resource_reply(ONLINE.INODE_METRICS)
        for row in value["series"]:
            row["points"][0].update(value=None, sampled_at=None, data_state="no_data")
        ONLINE.assert_resources(value, node, keys=ONLINE.INODE_METRICS, require_fresh=False)
        self.assertEqual(ONLINE.fresh_resource_groups(value, ONLINE.INODE_METRICS), [])
        with self.assertRaises(ONLINE.SuiteError): ONLINE.assert_resources(value, node, keys=ONLINE.INODE_METRICS)
        bad = copy.deepcopy(value); bad["series"][0]["points"][0].update(value=100, sampled_at=value["end"], data_state="valid")
        with self.assertRaises(ONLINE.SuiteError): ONLINE.assert_resources(bad, node, keys=ONLINE.INODE_METRICS, require_fresh=False)

    def test_inode_recovery_requires_a_supported_mount_after_the_recovery_instant(self):
        node, target = {"node_id": "node-id", "version": 1}, {"id": "target-id", "version": 2}
        value = self.resource_reply(ONLINE.INODE_METRICS)
        missing = copy.deepcopy(value["series"])
        for row in missing:
            row["dimensions"]["mountpoint"] = "/unsupported"
            row["points"][0].update(value=None, sampled_at=None, data_state="no_data")
        value["series"] += missing
        client = unittest.mock.Mock(); client.request.return_value = ONLINE.API.Response(200, value)
        at = ONLINE.utc_timestamp(value["end"])
        self.assertTrue(ONLINE.resource_query_after(client, node, target, at-1, ONLINE.INODE_METRICS))
        self.assertFalse(ONLINE.resource_query_after(client, node, target, at, ONLINE.INODE_METRICS))

    def check_device_protocol(self, family):
        node = {"node_id": "node-id", "version": 1}
        value = self.resource_reply(family)
        ONLINE.assert_resources(value, node, keys=family)
        self.assertEqual(ONLINE.fresh_resource_groups(value, family), [{"device": "sda"}])
        # Zero is valid throughput; a missing source is not a zero-rate device.
        for mutate in (lambda row: row.update(dimensions={}),
                       lambda row: row.update(dimensions={"device": "sda", "mountpoint": "/"}),
                       lambda row: row.update(window_seconds=0),
                       lambda row: row.update(dimensions={"device": "foreign"}),
                       lambda row: row["points"][0].update(value=-1)):
            bad = copy.deepcopy(value); mutate(bad["series"][0])
            with self.subTest(mutate=mutate), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_resources(bad, node, keys=family)
        missing = copy.deepcopy(value)
        for row in missing["series"]:
            row["points"][0].update(value=None, sampled_at=None, data_state="no_data")
        ONLINE.assert_resources(missing, node, keys=family, require_fresh=False)
        self.assertEqual(ONLINE.fresh_resource_groups(missing, family), [])
        selected = {"device": "sda"}
        history = self.resource_reply(family, trend=True)
        ONLINE.assert_resources(history, node, trend=True, keys=family, dimensions=selected)
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.assert_resources(history, node, trend=True, keys=family, dimensions={"device": "other"})

    def check_device_recovery(self, family):
        node, target = {"node_id": "node-id", "version": 1}, {"id": "target-id", "version": 2}
        value = self.resource_reply(family)
        client = unittest.mock.Mock(); client.request.return_value = ONLINE.API.Response(200, value)
        at = ONLINE.utc_timestamp(value["end"])
        self.assertTrue(ONLINE.resource_query_after(client, node, target, at-1, family))
        value["series"][1]["points"][0]["sampled_at"] = "2026-10-06T00:00:59Z"
        self.assertFalse(ONLINE.resource_query_after(client, node, target, at-1, family))
        disconnected = self.resource_reply(family, disconnected=True)
        ONLINE.assert_resources(disconnected, node, keys=family, disconnected=True)
        disconnected["series"][0]["points"][0].update(data_state="valid", value=0, sampled_at=disconnected["end"])
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.assert_resources(disconnected, node, keys=family, disconnected=True)

    def test_device_rate_protocol_and_recovery(self):
        for family in ONLINE.DEVICE_RATE_FAMILIES:
            with self.subTest(family=list(family)):
                self.check_device_protocol(family)
                self.check_device_recovery(family)

    def test_idle_disk_timing_is_unknown_without_weakening_busy_or_unit_evidence(self):
        node, target = {"node_id": "node-id", "version": 1}, {"id": "target-id", "version": 2}
        value = self.resource_reply(ONLINE.DISK_METRICS)
        for row in value["series"]:
            if row["unit"] == "milliseconds":
                row["points"][0].update(data_state="no_data", sampled_at=None, value=None)
        ONLINE.assert_resources(value, node, target, keys=ONLINE.DISK_METRICS)
        self.assertEqual(ONLINE.fresh_resource_groups(value, ONLINE.DISK_METRICS), [{"device": "sda"}])
        wrong_unit = copy.deepcopy(value)
        wrong_unit["series"][-1]["unit"] = "seconds"
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.assert_resources(wrong_unit, node, target, keys=ONLINE.DISK_METRICS)
        value["series"][2]["points"][0].update(data_state="no_data", sampled_at=None, value=None)
        self.assertEqual(ONLINE.fresh_resource_groups(value, ONLINE.DISK_METRICS), [])
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.assert_resources(value, node, target, keys=ONLINE.DISK_METRICS)

    def test_browser_report_binds_real_identity_versions_server_time_and_screenshots(self):
        expected = {"run_id": "unique-run", "node": {"node_id": "node-id", "version": 1},
                    "target": {"id": "target-id", "version": 2}, "policy_version": 0,
                    "admin_id": "admin-id", "security_id": "security-id"}
        admin, security = identity(), identity("platform.security_administrator")
        admin["principal"]["id"], security["principal"]["id"] = "admin-id", "security-id"
        instant = self.resource_reply()
        rows = [{"path": ONLINE.OBSERVATIONS, "query": {"node_id": "node-id", "metrics": ",".join(ONLINE.METRICS)}, "value": instant}]
        for key, span in (("node.memory.used_percent", 3600), ("node.cpu.busy_percent", 300), ("node.filesystem.used_percent", 300), ("node.filesystem.inodes_used_percent", 300), *((key, 300) for key in ONLINE.ALL_METRICS if key in ONLINE.DISK_METRICS or key in ONLINE.NETWORK_METRICS)):
            value = self.resource_reply([key], trend=True)
            end = datetime.datetime.fromisoformat(value["end"].replace("Z", "+00:00"))
            start = end - datetime.timedelta(seconds=span)
            stamp = lambda at: at.isoformat().replace("+00:00", "Z")
            value["start"] = stamp(start)
            value["series"][0]["points"] = [{"evaluated_at": stamp(start+datetime.timedelta(seconds=index*15)),
                "sampled_at": stamp(start+datetime.timedelta(seconds=index*15)), "value": 0, "data_state": "valid"} for index in range(span//15+1)]
            query = {"node_id": "node-id", "metrics": key, "start": value["start"], "end": value["end"]}
            if key.startswith("node.filesystem.") or key in ONLINE.DISK_METRICS or key in ONLINE.NETWORK_METRICS: query.update(value["series"][0]["dimensions"])
            rows.append({"path": ONLINE.TRENDS, "query": query, "value": value})
        rows.append(copy.deepcopy(rows[0]))
        rows.append({"path": ONLINE.OBSERVATIONS, "query": {"node_id": "node-id", "metrics": ",".join(ONLINE.FILESYSTEM_METRICS)}, "value": self.resource_reply(ONLINE.FILESYSTEM_METRICS)})
        rows.append({"path": ONLINE.OBSERVATIONS, "query": {"node_id": "node-id", "metrics": ",".join(ONLINE.INODE_METRICS)}, "value": self.resource_reply(ONLINE.INODE_METRICS)})
        rows.append({"path": ONLINE.OBSERVATIONS, "query": {"node_id": "node-id", "metrics": ",".join(ONLINE.DISK_METRICS)}, "value": self.resource_reply(ONLINE.DISK_METRICS)})
        rows.append({"path": ONLINE.OBSERVATIONS, "query": {"node_id": "node-id", "metrics": ",".join(ONLINE.NETWORK_METRICS)}, "value": self.resource_reply(ONLINE.NETWORK_METRICS)})
        report = {"schema_version": "addp.node-resources-browser/v1", "result": "passed", "stage": "complete", "run_id": "unique-run",
                  "identity": admin, "negative_identity": security, "negative_no_business_reads": True, "resources": rows,
                  "summaries": [{"path": ONLINE.SUMMARIES, "query": {"node_ids": "node-id"}, "value": {"data": [self.resource_reply(["node.cpu.busy_percent", "node.memory.used_percent"])]}}],
                  "auto_refresh": {"natural_timer": True, "server_end_advanced": True, "unchanged_url": True, "off_restored": True, "off_no_requests": True},
                  "navigation": {"list_summary_batch": True, "iframe_preserved": True, "history": True, "metric_reload": True, "range_reload": True, "server_window": True, "filesystem_reload": True, "inode_reload": True, "disk_reload": True, "network_reload": True},
                  "presentation": {"iec_capacity": True, "elapsed_uptime": True, "system_load_count": True, "disk_rate_units": True, "network_rate_units": True, "disk_timing_units": True}}
        following = copy.deepcopy(report["summaries"][0])
        fresh = following["value"]["data"][0]
        at = "2026-10-06T00:01:15Z"
        fresh.update(end=at, start=at, queried_at=at)
        fresh["collection"]["sampled_at"] = at
        for series in fresh["series"]:
            series["points"][0].update(evaluated_at=at, sampled_at=at)
        report["summaries"].append(following)
        report["host_list_auto_refresh"] = {"natural_timer": True, "server_end_advanced": True, "unchanged_url": True,
            "iframe_preserved": True, "off_restored": True, "off_no_requests": True, "before_index": 0, "after_index": 1}
        with tempfile.TemporaryDirectory() as directory:
            artifacts = Path(directory)
            for name in ("list", "detail", "restored", "filesystem", "inodes", "disks", "networks"):
                (artifacts / ("node-resources-" + name + ".png")).write_bytes(b"\x89PNG\r\n\x1a\n" + b"x"*1000)
            self.assertEqual(ONLINE.validate_resource_browser(report, expected, artifacts)["result"], "passed")
            for family in ONLINE.DEVICE_RATE_FAMILIES:
                with self.subTest(counter_family=list(family)):
                    gap = copy.deepcopy(next(row for row in rows if row["path"] == ONLINE.OBSERVATIONS and set(row["query"]["metrics"].split(",")) == set(family)))
                    for series in gap["value"]["series"]:
                        series["points"][0].update(data_state="no_data", value=None, sampled_at=None)
                    intermittent = copy.deepcopy(report)
                    intermittent["resources"].append(gap)
                    self.assertEqual(ONLINE.validate_resource_browser(intermittent, expected, artifacts)["result"], "passed")
                    unavailable = copy.deepcopy(intermittent)
                    for row in unavailable["resources"]:
                        if row["path"] == ONLINE.OBSERVATIONS and set(row["query"]["metrics"].split(",")) == set(family):
                            for series in row["value"]["series"]:
                                series["points"][0].update(data_state="no_data", value=None, sampled_at=None)
                    with self.assertRaises(ONLINE.SuiteError):
                        ONLINE.validate_resource_browser(unavailable, expected, artifacts)
                    for field, value in (("value", 0), ("sampled_at", gap["value"]["end"])):
                        invalid = copy.deepcopy(intermittent)
                        invalid["resources"][-1]["value"]["series"][0]["points"][0][field] = value
                        with self.subTest(invalid_gap_field=field), self.assertRaises(ONLINE.SuiteError):
                            ONLINE.validate_resource_browser(invalid, expected, artifacts)
            mutations = [lambda v: v.update(result="failed"), lambda v: v.update(run_id="other-run"),
                         lambda v: v.update(summaries=[]), lambda v: v.update(summaries=v["summaries"]*5),
                         lambda v: v["summaries"][0]["query"].update(node_ids="foreign"),
                         lambda v: v["summaries"][0]["value"]["data"][0].update(node_version=2),
                         lambda v: v["summaries"][0]["value"]["data"][0].update(policy_version=1),
                         lambda v: v["identity"]["principal"].update(id="foreign"),
                         lambda v: v["negative_identity"].update(context={"type": "tenant", "tenant_id": "2"}),
                         lambda v: v.update(negative_no_business_reads=False), lambda v: v["navigation"].update(history=False),
                         lambda v: v["resources"][0]["value"].update(policy_version=1),
                         lambda v: v["resources"][1]["value"].update(target_saved_version=1),
                         lambda v: v["resources"][1]["query"].update(end="2026-10-06T00:01:01Z"),
                         lambda v: v["resources"][1]["value"]["series"][0]["points"].pop(),
                         lambda v: v["resources"][0]["value"]["series"][0]["points"][0].update(value=float("nan")),
                         lambda v: v.update(resources=[]), lambda v: v.update(resources=[v["resources"][0]]*157), lambda v: v.pop("auto_refresh"),
                         lambda v: v["auto_refresh"].update(off_no_requests=False),
                         lambda v: v.pop("host_list_auto_refresh"),
                         lambda v: v["host_list_auto_refresh"].update(off_no_requests=False),
                         lambda v: v["host_list_auto_refresh"].update(before_index=True),
                         lambda v: v["host_list_auto_refresh"].update(after_index=9),
                         lambda v: v["host_list_auto_refresh"].update(after_index=0),
                         lambda v: v["summaries"][1]["value"]["data"][0].update(end=v["summaries"][0]["value"]["data"][0]["end"]),
                         lambda v: v["navigation"].pop("disk_reload"), lambda v: v["presentation"].update(disk_rate_units=False),
                         lambda v: v["navigation"].pop("network_reload"), lambda v: v["presentation"].update(network_rate_units=False),
                         lambda v: v["presentation"].update(disk_timing_units=False),
                         lambda v: v.update(resources=[row for row in v["resources"] if row["query"]["metrics"] != "node.disk.io_busy_percent"]),
                         lambda v: v.update(resources=[row for row in v["resources"] if row["query"]["metrics"] != "node.disk.read_mean_duration_milliseconds"]),
                         lambda v: v.update(resources=[row for row in v["resources"] if row["query"]["metrics"] != "node.disk.write_mean_duration_milliseconds"]),
                         lambda v: v.update(resources=[row for row in v["resources"] if row["query"]["metrics"] != "node.network.transmit_bytes_per_second"]),
                         lambda v: v.update(resources=[row for row in v["resources"] if row["query"]["metrics"] != "node.disk.write_bytes_per_second"])]
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
                    ONLINE.run_resource_browser({"node_id": "node", "version": 1}, {"id": "target", "version": 2}, {"version": 0}, "node", {"principal": {"id": "admin"}}, {"principal": {"id": "security"}}, [])
                self.assertFalse(evidence.exists())
                self.assertNotIn("sensitive", output.getvalue())
                value = json.loads((artifacts / "node-resources-browser-input.json").read_text())
                self.assertEqual(set(value["node"]), {"node_id", "version"})

    def test_browser_runtime_diagnostics_keep_deadlines_and_omit_captured_secrets(self):
        for captured, deadline in (("Test timeout of 240000ms exceeded. secret-password", True),
                                   ("\x1b[31mTest timeout of 240000ms exceeded.\x1b[0m secret-mfa", True),
                                   ("page.goto: Test ended. secret-token", False)):
            with self.subTest(deadline=deadline), tempfile.TemporaryDirectory() as directory:
                with patch.dict(ONLINE.os.environ, ADDP_ONLINE_ARTIFACT_DIR=directory, ADDP_ONLINE_SECRET_DIR=directory+"/private", ADDP_ONLINE_TEST_RUN_ID="run"), \
                     patch.object(ONLINE.subprocess, "run", return_value=unittest.mock.Mock(returncode=1, stdout=captured, stderr="private-credentials")) as command, \
                     patch("sys.stdout", new_callable=io.StringIO) as output:
                    with self.assertRaisesRegex(ONLINE.SuiteError, "resource browser failed"):
                        ONLINE.run_resource_browser({"node_id": "node", "version": 1}, {"id": "target", "version": 2}, {"version": 0}, "node", {"principal": {"id": "admin"}}, {"principal": {"id": "security"}}, [])
                    self.assertEqual(command.call_args.kwargs["timeout"], 300)
                    content = (Path(directory) / "node-resources-browser-runtime.json").read_text()
                    diagnostic = json.loads(content)
                    self.assertEqual(diagnostic["test_deadline_reported"], deadline)
                    self.assertFalse(diagnostic["subprocess_deadline_exceeded"])
                    self.assertEqual(diagnostic["subprocess_timeout_seconds"], 300)
                    self.assertIs(type(diagnostic["duration_ms"]), int)
                    self.assertGreaterEqual(diagnostic["duration_ms"], 0)
                    self.assertNotIn("secret-", content+output.getvalue())
                    self.assertNotIn("private-credentials", content+output.getvalue())
                    self.assertEqual(set(diagnostic), {"schema_version", "result", "duration_ms", "test_deadline_reported", "subprocess_deadline_exceeded", "subprocess_timeout_seconds"})

    def test_browser_subprocess_deadline_records_safe_failure_without_extending_it(self):
        with tempfile.TemporaryDirectory() as directory:
            expired = ONLINE.subprocess.TimeoutExpired(["npm", "secret-argument"], 300, output="secret-output", stderr="secret-error")
            with patch.dict(ONLINE.os.environ, ADDP_ONLINE_ARTIFACT_DIR=directory, ADDP_ONLINE_SECRET_DIR=directory+"/private", ADDP_ONLINE_TEST_RUN_ID="run"), \
                 patch.object(ONLINE.subprocess, "run", side_effect=expired):
                with self.assertRaisesRegex(ONLINE.SuiteError, "resource browser subprocess exceeded existing deadline"):
                    ONLINE.run_resource_browser({"node_id": "node", "version": 1}, {"id": "target", "version": 2}, {"version": 0}, "node", {"principal": {"id": "admin"}}, {"principal": {"id": "security"}}, [])
            content = (Path(directory) / "node-resources-browser-runtime.json").read_text()
            diagnostic = json.loads(content)
            self.assertTrue(diagnostic["subprocess_deadline_exceeded"])
            self.assertFalse(diagnostic["test_deadline_reported"])
            self.assertEqual(diagnostic["result"], "failed")
            self.assertNotIn("secret-", content)

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
