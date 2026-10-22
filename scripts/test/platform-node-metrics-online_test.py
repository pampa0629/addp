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
    @staticmethod
    def overview_reply(at="2026-10-06T00:01:00Z", disconnected=False, enabled=True):
        return {"scope": "current_authorized_host_nodes", "evaluated_at": at, "queried_at": at, "policy_version": 2, "lookback_seconds": 300,
                "inventory": {"managed": 1, "enabled": int(enabled), "disabled": int(not enabled), "connected": int(not disconnected), "not_connected": int(disconnected),
                              "collection": {key: int(key == ("not_connected" if disconnected else "collecting")) for key in ("collecting", "failed", "stale", "no_sample", "not_connected")}},
                "rankings": [{"metric_key": key, "unit": "percent", "window_seconds": window,
                              "coverage": {"expected": 1, "valid": int(not disconnected), "no_data": 0, "stale": 0, "not_connected": int(disconnected), "complete": not disconnected},
                              "top": [] if disconnected else [{"node": {"node_id": "node-id", "version": 1, "display_name": "Online host", "node_kind": "virtual", "enabled": True}, "value": 0, "sampled_at": at}]}
                             for key, window in (("node.cpu.busy_percent", 60), ("node.memory.used_percent", 0))]}

    def test_overview_rejects_partial_scope_wrong_identity_stale_samples_and_private_facts(self):
        node = {"node_id": "node-id", "version": 1, "enabled": True}
        valid = self.overview_reply()
        ONLINE.assert_resource_overview(valid, node, 2, "Online host")
        mutations = [lambda v: v.update(scope="current_page"), lambda v: v.update(policy_version=1),
                     lambda v: v.update(policy_version=True), lambda v: v.update(evaluated_at="2026-10-06T00:01:00.5Z"),
                     lambda v: v.update(queried_at="2026-10-06T00:00:59Z"), lambda v: v.update(endpoint="private"),
                     lambda v: v["inventory"].update(managed=2), lambda v: v["inventory"].update(managed=True),
                     lambda v: v["inventory"]["collection"].update(collecting=True), lambda v: v["rankings"].pop(),
                     lambda v: v["rankings"][0].update(window_seconds=0), lambda v: v["rankings"][0].update(unit="cores"),
                     lambda v: v["rankings"][0]["coverage"].update(expected=2), lambda v: v["rankings"][0]["coverage"].update(complete=1),
                     lambda v: v["rankings"][0]["top"][0]["node"].update(node_id="foreign"),
                     lambda v: v["rankings"][0]["top"][0]["node"].update(version=2),
                     lambda v: v["rankings"][0]["top"][0]["node"].update(addresses=["private"]),
                     lambda v: v["rankings"][0]["top"][0].update(value=float("nan")),
                     lambda v: v["rankings"][0]["top"][0].update(value=True),
                     lambda v: v["rankings"][0]["top"][0].update(value=101),
                     lambda v: v["rankings"][0]["top"][0].update(sampled_at="2026-10-06T00:01:01Z"),
                     lambda v: v["rankings"][0]["top"][0].update(sampled_at="2026-10-05T23:59:59Z")]
        for mutate in mutations:
            bad = copy.deepcopy(valid); mutate(bad)
            with self.subTest(mutate=mutate), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_resource_overview(bad, node, 2, "Online host")
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.assert_resource_overview(valid, node, 2, "Online host", after=ONLINE.utc_timestamp(valid["evaluated_at"]))

    def test_overview_disabled_node_and_target_cannot_reuse_ranked_history(self):
        for enabled in (True, False):
            node = {"node_id": "node-id", "version": 1, "enabled": enabled}
            disabled = self.overview_reply(disconnected=True, enabled=enabled)
            ONLINE.assert_resource_overview(disabled, node, 2, "Online host", disconnected=True)
            for mutate in (lambda v: v["rankings"][0].update(top=self.overview_reply()["rankings"][0]["top"]),
                           lambda v: v["rankings"][0]["coverage"].update(valid=1, not_connected=0, complete=True),
                           lambda v: v["inventory"].update(connected=1, not_connected=0)):
                bad = copy.deepcopy(disabled); mutate(bad)
                with self.subTest(enabled=enabled, mutate=mutate), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.assert_resource_overview(bad, node, 2, "Online host", disconnected=True)

    def test_overview_source_failure_keeps_scope_and_only_ranks_fresh_valid_evidence(self):
        node = {"node_id": "node-id", "version": 1, "enabled": True}
        for state in ("valid", "no_data", "stale"):
            failed = self.overview_reply()
            failed["inventory"]["collection"].update(collecting=0, failed=1)
            rank = failed["rankings"][0]
            rank["coverage"].update(valid=int(state == "valid"), no_data=int(state == "no_data"), stale=int(state == "stale"), complete=state == "valid")
            if state != "valid": rank["top"] = []
            with self.subTest(state=state):
                ONLINE.assert_resource_overview(failed, node, 2, "Online host", source_failed=True)
                with self.assertRaises(ONLINE.SuiteError): ONLINE.assert_resource_overview(failed, node, 2, "Online host")
            mutations = [lambda v: v["inventory"]["collection"].update(collecting=1, failed=0),
                         lambda v: v["inventory"].update(connected=0, not_connected=1),
                         lambda v: v["rankings"][0]["coverage"].update(not_connected=1),
                         lambda v: v["rankings"][0]["coverage"].update(expected=2),
                         lambda v: v["rankings"][0]["coverage"].update(no_data=True),
                         lambda v: v["rankings"][0]["coverage"].update(complete=state != "valid")]
            if state == "valid":
                mutations.append(lambda v: v["rankings"][0]["top"][0].update(sampled_at="2026-10-05T23:59:59Z"))
            else:
                mutations.append(lambda v: v["rankings"][0].update(top=self.overview_reply()["rankings"][0]["top"]))
            for mutate in mutations:
                bad = copy.deepcopy(failed); mutate(bad)
                with self.subTest(state=state, mutate=mutate), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.assert_resource_overview(bad, node, 2, "Online host", source_failed=True)
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.assert_resource_overview(self.overview_reply(), node, 2, "Online host", source_failed=True, disconnected=True)

    def test_source_outage_waits_for_failure_visible_at_the_api_whole_second_anchor(self):
        node = {"node_id": "node-id", "version": 1, "enabled": True}
        selector = '{job="addp_nodes",addp_node_id="node-id"}'
        for stamp in ("2026-10-16T16:46:35Z", "2026-10-16T16:46:59Z"):
            with self.subTest(stamp=stamp):
                base = ONLINE.utc_timestamp(stamp)
                failure_sample = base + 0.475
                clock = [base + 0.836]
                def query(expression, *, at=None):
                    self.assertEqual(expression, "up" + selector + " == 0")
                    evaluated = clock[0] if at is None else ONLINE.utc_timestamp(at)
                    return [{"metric": {}, "value": [evaluated, "0"]}] if evaluated >= failure_sample else []
                prom = unittest.mock.Mock(query=unittest.mock.Mock(side_effect=query))
                # Real-time evidence can already fail while the API's integer anchor still sees up=1.
                self.assertTrue(prom.query("up" + selector + " == 0"))
                prom.query.reset_mock()
                healthy = self.overview_reply(at=stamp)
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.assert_resource_overview(healthy, node, 2, "Online host", source_failed=True)
                with patch.object(ONLINE.time, "time", side_effect=lambda: clock[0]):
                    self.assertFalse(ONLINE.source_scrape_failed(prom, selector))
                    self.assertEqual(prom.query.call_args.kwargs, {"at": stamp})
                    clock[0] = base + 1.05
                    self.assertTrue(ONLINE.source_scrape_failed(prom, selector))
                next_stamp = datetime.datetime.fromtimestamp(base + 1, datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
                self.assertEqual(prom.query.call_args.kwargs, {"at": next_stamp})
                self.assertEqual(prom.query.call_count, 2)
                failed = self.overview_reply(at=next_stamp)
                failed["inventory"]["collection"].update(collecting=0, failed=1)
                ONLINE.assert_resource_overview(failed, node, 2, "Online host", source_failed=True)

    def test_source_overview_success_keeps_the_single_request_without_diagnostics(self):
        node = {"node_id": "node-id", "version": 1, "enabled": True}
        value = self.overview_reply()
        value["inventory"]["collection"].update(collecting=0, failed=1)
        admin = unittest.mock.Mock(request=unittest.mock.Mock(return_value=ONLINE.API.Response(200, value)))
        prom, report = unittest.mock.Mock(), {}
        self.assertIs(ONLINE.assert_source_overview_isolation(admin, prom, '{addp_node_id="node-id"}', node, 2, "Online host", report), value)
        admin.request.assert_called_once_with("GET", ONLINE.OVERVIEW, (200,))
        prom.query.assert_not_called()
        self.assertEqual(report, {})

    def test_source_overview_failure_captures_only_bounded_same_anchor_facts_and_preserves_verdict(self):
        node = {"node_id": "node-id", "version": 1, "enabled": True}
        for mode in ("collecting", "missing", "unavailable", "duplicate", "foreign", "wrong_time", "nonfinite", "over_budget", "bad_inventory", "bad_anchor", "bool_count"):
            with self.subTest(mode=mode):
                value = self.overview_reply()
                value["inventory"]["credential"] = "must-never-retain"
                value["inventory"]["collection"]["credential"] = "must-never-retain"
                if mode == "bad_inventory": value["inventory"]["collection"] = []
                if mode == "bad_anchor": value["evaluated_at"] = "must-never-retain"
                if mode == "bool_count": value["inventory"]["managed"] = True
                before = copy.deepcopy(value)
                admin = unittest.mock.Mock(request=unittest.mock.Mock(return_value=ONLINE.API.Response(200, value)))
                at = self.overview_reply()["evaluated_at"]
                rows = [{"metric": {"addp_probe": key, "credential": "must-never-retain"},
                         "value": [ONLINE.utc_timestamp(at), number], "credential": "must-never-retain"}
                        for key, number in (("up_count", "1"), ("up", "1"), ("sampled_at", str(ONLINE.utc_timestamp(at) - 15)))]
                if mode == "missing": rows = []
                if mode == "duplicate": rows = [rows[0], copy.deepcopy(rows[0])]
                if mode == "foreign": rows[0]["metric"]["addp_probe"] = "foreign"
                if mode == "wrong_time": rows[0]["value"][0] += 1
                if mode == "nonfinite": rows[0]["value"][1] = "NaN"
                if mode == "over_budget": rows *= 2
                prom = unittest.mock.Mock(query=unittest.mock.Mock(return_value=rows))
                if mode == "unavailable": prom.query.side_effect = ValueError("must-never-retain")
                report = {}
                with self.assertRaises(ONLINE.SuiteError) as original:
                    ONLINE.assert_resource_overview(value, node, 2, "Online host", source_failed=True)
                with self.assertRaises(ONLINE.SuiteError) as observed:
                    ONLINE.assert_source_overview_isolation(admin, prom, '{addp_node_id="node-id"}', node, 2, "Online host", report)
                self.assertEqual(str(observed.exception), str(original.exception))
                admin.request.assert_called_once_with("GET", ONLINE.OVERVIEW, (200,))
                self.assertEqual(value, before)
                witness = report["source_outage_host_overview"]
                self.assertEqual(witness["state"], "captured" if mode in {"collecting", "missing", "bool_count"} else "unavailable")
                self.assertNotIn("must-never-retain", json.dumps(report))
                if mode in {"bad_inventory", "bad_anchor"}:
                    prom.query.assert_not_called()
                else:
                    self.assertEqual(prom.query.call_count, 1)
                    self.assertEqual(prom.query.call_args.kwargs, {"at": at})
                    query = prom.query.call_args.args[0]
                    self.assertIn('addp_node_id="node-id"', query)
                    for fragment in ('count(up', 'max(up', 'max(timestamp(up', '"addp_probe"'):
                        self.assertIn(fragment, query)
                if witness["state"] == "captured":
                    self.assertEqual(witness["values"]["up"], 1 if mode != "missing" else None)
                else:
                    self.assertNotIn("values", witness)
                if mode == "bool_count": self.assertNotIn("managed", witness["inventory"])

    def test_overview_browser_requires_real_refresh_restore_scope_and_screenshot(self):
        expected = {"node": {"node_id": "node-id", "version": 1}, "display_name": "Online host", "policy_version": 2}
        flags = ("natural_timer", "server_end_advanced", "unchanged_url", "iframe_preserved", "off_restored", "off_no_requests", "detail_and_back", "negative_no_reads")
        evidence = {**{key: True for key in flags}, "responses": [{"path": ONLINE.OVERVIEW, "query": {}, "value": self.overview_reply(at)} for at in ("2026-10-06T00:01:00Z", "2026-10-06T00:01:15Z", "2026-10-06T00:01:16Z", "2026-10-06T00:01:17Z")]}
        with tempfile.TemporaryDirectory() as directory:
            artifacts = Path(directory)
            screenshot = artifacts / "node-resources-overview.png"
            screenshot.write_bytes(b"\x89PNG\r\n\x1a\n" + b"x"*1000)
            ONLINE.validate_overview_browser({"overview": evidence}, expected, artifacts)
            mutations = [lambda v, key=key: v.update({key: False}) for key in flags] + [
                lambda v: v.update(responses=[]), lambda v: v.update(responses=v["responses"]*2),
                lambda v: v["responses"][0].update(path=ONLINE.SUMMARIES),
                lambda v: v["responses"][0].update(query={"node_ids": "node-id"}),
                lambda v: v["responses"][1].update(value=v["responses"][0]["value"]),
                lambda v: v["responses"][0]["value"]["inventory"].update(managed=2)]
            for mutate in mutations:
                bad = copy.deepcopy(evidence); mutate(bad)
                with self.subTest(mutate=mutate), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.validate_overview_browser({"overview": bad}, expected, artifacts)
            screenshot.unlink()
            with self.assertRaises(ONLINE.SuiteError): ONLINE.validate_overview_browser({"overview": evidence}, expected, artifacts)

    def test_process_dialog_close_finishes_its_real_batch_before_host_navigation(self):
        source=Path(__file__).resolve().parents[2]/"console/frontend/e2e/online/platform-node-resources.spec.js"
        script=r"""
import {readFileSync} from 'node:fs'
const text=readFileSync(process.argv[1],'utf8')
const boundary='trends: [beforeServiceAuto, afterAuto] }\n'
if(!text.includes(boundary))throw new Error('service refresh boundary missing')
const code=text.split(boundary)[1].split("    await expect(monitor.getByTestId('resource-node-list'))")[0]
const FunctionAsync=Object.getPrototypeOf(async()=>{}).constructor
for(const failBody of [false,true]){
 const pending=[];let responseWaiter;let bodyFinished=false;let navigated=false;let checked=false
 const response={url:()=> 'http://example.invalid/api/v1/monitor/platform/process_resource_summaries',ok:()=>true}
 const page={
  waitForResponse:predicate=>new Promise(resolve=>{responseWaiter=()=>{if(!predicate(response))throw new Error('wrong response scope');resolve(response)}}),
  goto:async path=>{if(!bodyFinished)throw new Error('navigation aborted unfinished close response');if(path!='/monitor/node-resources')throw new Error('wrong host route');navigated=true}
 }
 const monitor={locator:selector=>({click:async()=>{
  if(selector!=='.el-dialog__headerbtn')throw new Error('wrong close control')
  const body=new Promise((resolve,reject)=>setTimeout(()=>{if(failBody)reject(new Error('close response failed'));else{bodyFinished=true;resolve({data:[]})}},10))
  pending.push(body);responseWaiter?.()
 }})}
 const checkServices=async()=>{await Promise.all(pending);checked=true}
 let error
 try{await new FunctionAsync('page','monitor','checkServices','save',code)(page,monitor,checkServices,()=>{})}catch(e){error=e}
 await Promise.allSettled(pending)
 if(failBody){if(error?.message!=='close response failed'||navigated)throw new Error('failed close response was hidden')}
 else{if(error)throw error;if(!navigated||!checked)throw new Error('closed service batch was not validated')}
}
"""
        result=ONLINE.subprocess.run(["node","--input-type=module","-e",script,str(source)],capture_output=True,text=True,timeout=20)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)

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
        partial_overview = False
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
            elif path == ONLINE.OVERVIEW:
                self.assertEqual(current["max_metrics"], 1)
                status, payload = 422, {"error_code": "observability_query_budget_exceeded"}
                if partial_overview:
                    payload["rankings"] = []
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
        client.request.assert_any_call("GET", ONLINE.OVERVIEW, (422,))
        partial_overview = True
        with self.assertRaisesRegex(ONLINE.SuiteError, "partial scope"):
            ONLINE.check_query_policy(client, {"node_id": "node-id", "version": 1})

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

    def add_period_comparison_evidence(self, report, artifacts, gap_state=None):
        report["period_comparisons"] = []
        stamp = lambda at: at.isoformat().replace("+00:00", "Z")
        for phase, key in (("cpu", "node.cpu.busy_percent"), ("memory", "node.memory.used_percent"), ("restored", "node.memory.used_percent")):
            before = len(report["resources"])
            for family in (ONLINE.METRICS, ONLINE.FILESYSTEM_METRICS, ONLINE.INODE_METRICS, ONLINE.DISK_METRICS, ONLINE.NETWORK_METRICS):
                report["resources"].append({"path": ONLINE.OBSERVATIONS, "query": {"node_id": "node-id", "metrics": ",".join(family)}, "value": self.resource_reply(family)})
            response = self.resource_reply([key], trend=True)
            end = datetime.datetime.fromisoformat(response["end"].replace("Z", "+00:00"))
            start, boundary = end - datetime.timedelta(seconds=600), end - datetime.timedelta(seconds=300)
            response["start"] = stamp(start)
            points = [{"evaluated_at": stamp(start + datetime.timedelta(seconds=index*15)),
                       "sampled_at": stamp(start + datetime.timedelta(seconds=index*15)), "value": 100 if index == 0 else 30 if index <= 20 else 40,
                       "data_state": "valid"} for index in range(41)]
            if gap_state:
                points[1].update(data_state=gap_state, value=30 if gap_state == "stale" else None,
                                 sampled_at=stamp(start - datetime.timedelta(seconds=46)) if gap_state == "stale" else None)
            response["series"][0]["points"] = points
            query = {"node_id": "node-id", "metrics": key, "start": response["start"], "end": response["end"]}
            report["resources"].append({"path": ONLINE.TRENDS, "query": query, "value": response})
            statistics = {"previous": {"start": stamp(start), "end": stamp(boundary), "valid": 19 if gap_state else 20, "total": 20, "mean": None if gap_state else 30},
                          "current": {"start": stamp(boundary), "end": stamp(end), "valid": 20, "total": 20, "mean": None if gap_state else 40},
                          "complete": not bool(gap_state), "delta": None if gap_state else 10, "stepSeconds": 15}
            report["period_comparisons"].append({"phase": phase, "before_index": before, "resource_index": len(report["resources"]) - 1,
                                                "page_path": "/monitor/node-resources/node-id", "page_query": {"range": "5m", "compare": "previous", "refresh": "off", **({"metric": key} if phase == "cpu" else {})},
                                                "statistics": statistics})
            (artifacts / ("node-resources-comparison-" + phase + ".png")).write_bytes(b"\x89PNG\r\n\x1a\n" + b"x"*1000)

    def test_period_comparison_independently_recomputes_full_and_incomplete_evidence(self):
        expected = {"node": {"node_id": "node-id", "version": 1}, "target": {"id": "target-id", "version": 2}, "policy_version": 0}
        for gap in (None, "no_data", "stale"):
            with self.subTest(gap=gap), tempfile.TemporaryDirectory() as directory:
                report, artifacts = {"resources": []}, Path(directory)
                self.add_period_comparison_evidence(report, artifacts, gap)
                indices, summary = ONLINE.validate_resource_comparison_browser(report, expected, artifacts)
                self.assertEqual(indices, {5, 11, 17})
                self.assertEqual(summary, {"result": "passed", "cases": 3, "complete_cases": 0 if gap else 3,
                                           "incomplete_cases": 3 if gap else 0, "single_query_each": True, "url_restore": True})

    def test_period_comparison_rejects_wrong_binding_grid_periods_statistics_and_extra_reads(self):
        expected = {"node": {"node_id": "node-id", "version": 1}, "target": {"id": "target-id", "version": 2}, "policy_version": 0}
        with tempfile.TemporaryDirectory() as directory:
            artifacts, report = Path(directory), {"resources": []}
            self.add_period_comparison_evidence(report, artifacts)
            mutations = [lambda v: v.pop("period_comparisons"), lambda v: v["period_comparisons"].pop(),
                         lambda v: v["period_comparisons"][0].update(resource_index=True),
                         lambda v: v["period_comparisons"][0].update(before_index=1),
                         lambda v: v["period_comparisons"][1].update(before_index=0, resource_index=5),
                         lambda v: v["period_comparisons"][2]["page_query"].update(compare="unknown"),
                         lambda v: v["period_comparisons"][0].update(page_path="/monitor/node-resources/foreign"),
                         lambda v: v["resources"][5]["query"].update(node_id="foreign"),
                         lambda v: v["resources"][5]["query"].update(metrics="node.memory.used_percent"),
                         lambda v: v["resources"][5]["query"].update(start="2026-10-05T23:56:00Z"),
                         lambda v: v["resources"][5]["query"].update(arbitrary="query"),
                         lambda v: v["resources"][0]["value"].update(end="2026-10-06T00:00:45Z"),
                         lambda v: v["resources"][5]["value"].update(node_version=2),
                         lambda v: v["resources"][5]["value"].update(target_saved_version=1),
                         lambda v: v["resources"][5]["value"].update(policy_version=1),
                         lambda v: v["resources"][5]["value"]["series"][0]["points"].pop(),
                         lambda v: v["resources"][5]["value"]["series"][0]["points"][1].update(sampled_at="2026-10-06T00:01:00Z"),
                         lambda v: v["period_comparisons"][0]["statistics"]["previous"].update(total=21),
                         lambda v: v["period_comparisons"][0]["statistics"]["current"].update(valid=True),
                         lambda v: v["period_comparisons"][0]["statistics"]["previous"].update(mean=30.01),
                         lambda v: v["period_comparisons"][0]["statistics"].update(delta=100/3),
                         lambda v: v["period_comparisons"][0]["statistics"].update(stepSeconds=30),
                         lambda v: v["period_comparisons"][0]["statistics"]["current"].update(start="2026-10-05T23:55:00Z"),
                         lambda v: v["resources"][0].update(path=ONLINE.TRENDS)]
            for mutation in mutations:
                invalid = copy.deepcopy(report); mutation(invalid)
                with self.subTest(mutation=mutation), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.validate_resource_comparison_browser(invalid, expected, artifacts)
            (artifacts / "node-resources-comparison-restored.png").unlink()
            with self.assertRaises(ONLINE.SuiteError): ONLINE.validate_resource_comparison_browser(report, expected, artifacts)

    def test_period_comparison_accepts_a_valid_zero_baseline_without_a_division(self):
        expected = {"node": {"node_id": "node-id", "version": 1}, "target": {"id": "target-id", "version": 2}, "policy_version": 0}
        with tempfile.TemporaryDirectory() as directory:
            artifacts, report = Path(directory), {"resources": []}
            self.add_period_comparison_evidence(report, artifacts)
            for case in report["period_comparisons"]:
                points = report["resources"][case["resource_index"]]["value"]["series"][0]["points"]
                for point in points[1:21]: point["value"] = 0
                case["statistics"]["previous"]["mean"] = 0
                case["statistics"]["delta"] = 40
            self.assertEqual(ONLINE.validate_resource_comparison_browser(report, expected, artifacts)[1]["complete_cases"], 3)

    def test_period_comparison_rejects_means_and_change_when_any_period_has_a_gap(self):
        expected = {"node": {"node_id": "node-id", "version": 1}, "target": {"id": "target-id", "version": 2}, "policy_version": 0}
        with tempfile.TemporaryDirectory() as directory:
            artifacts, report = Path(directory), {"resources": []}
            self.add_period_comparison_evidence(report, artifacts, "no_data")
            for change in ({"complete": True}, {"delta": 0}, {"delta": 10}, {"previous": {"mean": 30}}, {"current": {"mean": 40}}):
                invalid = copy.deepcopy(report)
                for key, value in change.items():
                    if isinstance(value, dict): invalid["period_comparisons"][0]["statistics"][key].update(value)
                    else: invalid["period_comparisons"][0]["statistics"][key] = value
                with self.subTest(change=change), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.validate_resource_comparison_browser(invalid, expected, artifacts)

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
            self.add_period_comparison_evidence(report, artifacts)
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
                         lambda v: v.update(resources=[]), lambda v: v.update(resources=[v["resources"][0]]*175), lambda v: v.pop("auto_refresh"),
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



class CompleteComparisonCaseTest(unittest.TestCase):
    node = {"node_id": "node-id", "version": 1}
    target = {"id": "target-id", "version": 2}
    policy = {"version": 0}

    def reply(self, path, gap=False):
        query = ONLINE.urllib.parse.parse_qs(ONLINE.urllib.parse.urlsplit(path).query)
        self.assertEqual(set(query), {"node_id", "metrics", "start", "end"})
        self.assertEqual(set(query["metrics"][0].split(",")), {"node.cpu.busy_percent", "node.memory.used_percent"})
        start, end = (ONLINE.utc_timestamp(query[k][0]) for k in ("start", "end"))
        self.assertEqual(end-start, 600)
        value = MetricsProtocolTest().resource_reply(query["metrics"][0].split(","), trend=True)
        value.update(start=query["start"][0], end=query["end"][0], queried_at=query["end"][0])
        for row in value["series"]:
            row["points"] = []
            for offset in range(0, 601, 15):
                at = datetime.datetime.fromtimestamp(start+offset, datetime.timezone.utc).isoformat().replace("+00:00", "Z")
                row["points"].append({"evaluated_at": at, "sampled_at": at, "value": 0, "data_state": "valid"})
            if gap and row["metric_key"] == "node.cpu.busy_percent":
                row["points"][7].update(sampled_at=None, value=None, data_state="no_data")
        return value

    def test_case_selection_is_closed_and_rejects_before_login(self):
        for value in ("regression", "complete-comparison", "cpu-pressure", "host-admission", "engine-admission", "meta-workers", "transfer-workers"):
            with patch.dict(ONLINE.os.environ, ADDP_ONLINE_NODE_METRICS_CASE=value):
                self.assertEqual(ONLINE.acceptance_case(), value)
        for value in ("", "unknown", "cpu-pressure ", "regression "):
            with patch.dict(ONLINE.os.environ, ADDP_ONLINE_NODE_METRICS_CASE=value), patch.object(ONLINE, "login") as login:
                with self.assertRaises(ONLINE.SuiteError): ONLINE.run("http://example.invalid", Path("/secret"), {})
                login.assert_not_called()

    @staticmethod
    def admission_browser_reply(artifacts):
        host_id = "10000000-0000-4000-8000-000000000001"
        target_id = "20000000-0000-4000-8000-000000000001"
        manifest = {"schema": "addp.host-admission/v2", "host": {"display_name": "Online admission run", "node_kind": "virtual",
                    "addresses": ["127.0.0.1"], "allowed_module_bindings": [{"module_name": "monitor", "client_id": "addp-monitor"}]},
                    "collection": {"monitor_kind": "host_resources", "source": {"type": "node_exporter", "endpoint": "https://127.0.0.1:19100/metrics"}}}
        expected = {"run_id": "run", "admin_id": "admin", "admission": manifest}
        host_input = dict(manifest["host"], enabled=True)
        body = dict(manifest["collection"], subject={"kind": "node", "node_id": host_id}, enabled=True)
        rejected = dict(body, source=dict(body["source"], endpoint="https://127.0.0.1:19100/rejected-path"))
        actor = identity(); actor["principal"]["id"] = "admin"
        value = {"schema_version": "addp.node-resources-browser/v1", "result": "passed", "stage": "complete", "run_id": "run", "identity": actor,
                 "admission": {"host": dict(host_input, node_id=host_id, version=1), "target": dict(body, id=target_id, version=1),
                    "writes": [{"path": ONLINE.NODES, "input": host_input}, {"path": ONLINE.TARGETS, "input": rejected}, {"path": ONLINE.TARGETS, "input": body}],
                    "failure": {"status": 400, "error_code": "invalid_monitoring_target"}, "reloaded": True}}
        for name in ("node-admission-partial.png", "node-admission-saved.png"):
            (artifacts / name).write_bytes(b"\x89PNG\r\n\x1a\n"+b"x"*1100)
        return expected, value

    def test_admission_browser_rejects_duplicates_scope_changes_and_missing_screenshot(self):
        with tempfile.TemporaryDirectory() as folder:
            artifacts = Path(folder)
            expected, value = self.admission_browser_reply(artifacts)
            host, target = ONLINE.validate_host_admission_browser(value, expected, artifacts)
            self.assertEqual(target["subject"]["node_id"], host["node_id"])
            mutations = [lambda v: v.update(run_id="other"), lambda v: v["identity"]["principal"].update(id="other"),
                lambda v: v["admission"]["host"].update(enabled=False), lambda v: v["admission"]["host"].update(version=2),
                lambda v: v["admission"]["target"]["subject"].update(node_id="other"),
                lambda v: v["admission"]["target"]["source"].update(endpoint="https://foreign.test:19100/metrics"),
                lambda v: v["admission"]["writes"].insert(2, copy.deepcopy(v["admission"]["writes"][0])),
                lambda v: v["admission"]["writes"].pop(1), lambda v: v["admission"].update(reloaded=False),
                lambda v: v["admission"]["failure"].update(status=503)]
            for change in mutations:
                invalid = copy.deepcopy(value); change(invalid)
                with self.assertRaises(ONLINE.SuiteError): ONLINE.validate_host_admission_browser(invalid, expected, artifacts)
            (artifacts / "node-admission-saved.png").unlink()
            with self.assertRaises(ONLINE.SuiteError): ONLINE.validate_host_admission_browser(value, expected, artifacts)

    def test_admission_case_reads_both_owners_before_retiring_exact_browser_subjects(self):
        with tempfile.TemporaryDirectory() as folder:
            artifacts = Path(folder)
            expected, value = self.admission_browser_reply(artifacts)
            host, target = value["admission"]["host"], value["admission"]["target"]
            actor = value["identity"]
            name = "Online admission run"
            declaration = copy.deepcopy(expected["admission"])
            self.assertEqual(declaration["host"]["display_name"], name)
            for mismatch in (False, True):
                report = {}
                stored_target = dict(target, version=2) if mismatch else target
                replies = [ONLINE.API.Response(200, host), ONLINE.API.Response(200, stored_target),
                           ONLINE.API.Response(200, {"data": [host], "total": 1}), ONLINE.API.Response(200, {"data": [target], "total": 1})]
                client = unittest.mock.Mock(request=unittest.mock.Mock(side_effect=replies))
                with patch.dict(ONLINE.os.environ, ADDP_ONLINE_TEST_RUN_ID="run", ADDP_ONLINE_METRICS_NODE_IP="127.0.0.1", ADDP_ONLINE_ARTIFACT_DIR=folder), \
                     patch.object(ONLINE.FIXTURE, "boundary", return_value=artifacts), \
                     patch.object(ONLINE.subprocess, "check_output", return_value=json.dumps(declaration)) as export, \
                     patch.object(ONLINE, "execute_resource_browser", return_value=value), \
                     patch.object(ONLINE, "renew_admin", return_value=client), \
                     patch.object(ONLINE, "run_host_binding_case", return_value=host) as binding, \
                     patch.object(ONLINE, "write_evidence"), patch.object(ONLINE, "retire_owned_configuration") as retire:
                    if mismatch:
                        with self.assertRaises(ONLINE.SuiteError): ONLINE.run_host_admission_case("base", artifacts, actor, report)
                        retire.assert_not_called(); binding.assert_not_called()
                    else:
                        ONLINE.run_host_admission_case("base", artifacts, actor, report)
                        retire.assert_called_once_with(client, host, target, declaration["host"], report)
                        self.assertEqual(report["host_admission"]["host_posts"], 1)
                        binding.assert_called_once_with("base", artifacts, client, actor, host, target, report)
                    self.assertIn("export-admission", export.call_args.args[0])
                    paths = [ONLINE.NODES+"/"+host["node_id"], ONLINE.TARGETS+"/"+target["id"]]
                    self.assertEqual([c.args[1] for c in client.request.call_args_list], paths if mismatch else paths+[ONLINE.NODES, ONLINE.TARGETS])

    def test_host_binding_requires_current_System_verdict_and_keeps_rejection_explicit(self):
        node = "10000000-0000-4000-8000-000000000001"
        owner = {"module_name": "monitor", "role": "backend", "status": "up", "process_metrics_declared": True,
                 "declared_node_id": node, "node_id": node, "node_binding_state": "bound", "node_binding_reason": ""}
        ONLINE.assert_host_binding(owner, node)
        rejected = dict(owner, node_id="", node_binding_state="rejected", node_binding_reason="node_disabled")
        ONLINE.assert_host_binding(rejected, node, rejected=True)
        for change in ({"module_name": "system"}, {"status": "down"}, {"node_id": ""}, {"declared_node_id": ""},
                       {"node_binding_state": "unbound"}, {"node_binding_reason": "source_not_allowed"}, {"process_metrics_declared": False}):
            with self.subTest(change=change), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_host_binding(dict(owner, **change), node)
        with self.assertRaises(ONLINE.SuiteError): ONLINE.assert_host_binding(owner, node, rejected=True)

    def test_binding_browser_checks_identity_both_resources_restoration_and_screenshots(self):
        with tempfile.TemporaryDirectory() as folder:
            artifacts = Path(folder)
            expected, admission = self.admission_browser_reply(artifacts)
            host, target = admission["admission"]["host"], admission["admission"]["target"]
            owner, process = MetricsProtocolTest().process_reply()
            owner, process = owner[1], {"data": [process["data"][1]]}
            owner["node_id"] = host["node_id"]; process["data"][0]["node_id"] = host["node_id"]
            resource = MetricsProtocolTest().resource_reply()
            resource.update(subject={"kind": "node", "node_id": host["node_id"]}, node_version=host["version"], target_id=target["id"], target_saved_version=target["version"])
            expected = {"run_id": expected["run_id"], "admin_id": expected["admin_id"], "binding": {"host": host, "target": target, "instance": owner}}
            value = {"schema_version": "addp.node-resources-browser/v1", "result": "passed", "stage": "complete", "run_id": expected["run_id"],
                     "identity": admission["identity"], "binding": {"host": host, "instance": owner, "same_iframe": True, "back_restored": True, "reload_restored": True,
                     "process_summary": {"query": {"instance_ids": str(owner["id"])}, "value": process},
                     "resources": {"path": "/api/v1/monitor/platform/resource_observations", "query": {"node_id": host["node_id"], "metrics": ",".join(ONLINE.METRICS)}, "value": resource}}}
            for name in ("node-binding-services.png", "node-binding-resources.png"):
                (artifacts / name).write_bytes(b"\x89PNG\r\n\x1a\n"+b"x"*1100)
            self.assertEqual(ONLINE.validate_host_binding_browser(value, expected, artifacts)["result"], "passed")
            for mutate in (lambda v: v.update(result="failed"), lambda v: v["identity"]["principal"].update(id="other"),
                           lambda v: v["binding"].update(back_restored=False), lambda v: v["binding"]["host"].update(version=9),
                           lambda v: v["binding"]["instance"].update(instance_id="old"),
                           lambda v: v["binding"]["process_summary"]["query"].update(instance_ids="7,9"),
                           lambda v: v["binding"]["resources"].update(path="/api/v1/monitor/platform/resource_trends"),
                           lambda v: v["binding"]["resources"]["query"].update(node_id="another"),
                           lambda v: v["binding"]["resources"]["query"].update(metrics="node.memory.total_bytes"),
                           lambda v: v["binding"]["resources"]["query"].update(metrics="node.network.receive_bytes_per_second,node.network.transmit_bytes_per_second"),
                           lambda v: v["binding"]["resources"]["query"].update(metrics=",".join([*list(ONLINE.METRICS)[:-1], next(iter(ONLINE.METRICS))])),
                           lambda v: v["binding"]["resources"]["query"].update(end="2026-10-19T00:00:00Z"),
                           lambda v: v["binding"]["resources"]["value"]["collection"].update(state="failed")):
                bad = copy.deepcopy(value); mutate(bad)
                with self.subTest(mutate=mutate), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.validate_host_binding_browser(bad, copy.deepcopy(expected), artifacts)
            (artifacts / "node-binding-services.png").unlink()
            with self.assertRaises(ONLINE.SuiteError): ONLINE.validate_host_binding_browser(value, expected, artifacts)

    def test_bound_process_is_new_peer_unchanged_and_host_state_is_reversible_without_restarting(self):
        with tempfile.TemporaryDirectory() as folder:
            artifacts = Path(folder)
            expected, admission = self.admission_browser_reply(artifacts)
            host, target = admission["admission"]["host"], admission["admission"]["target"]
            initial, _ = MetricsProtocolTest().process_reply()
            for item in initial: item["declared_node_id"] = ""
            peer, old = initial
            stopped = dict(old, status="down")
            new = dict(old, id=10, instance_id="new-runtime", process_started_at="2026-10-11T00:01:00Z", node_id=host["node_id"],
                       declared_node_id=host["node_id"], node_binding_state="bound", node_binding_reason="")
            rejected = dict(new, node_id="", node_binding_state="rejected", node_binding_reason="node_disabled")
            disabled, restored = dict(host, enabled=False, version=2), dict(host, version=3)
            admin = unittest.mock.Mock(request=unittest.mock.Mock(side_effect=[ONLINE.API.Response(200, row) for row in (disabled, {}, restored, {})]))
            report = {}
            with patch.dict(ONLINE.os.environ, MONITOR_URL="http://localhost:8100", ADDP_ONLINE_TEST_RUN_ID="run", ADDP_ONLINE_ARTIFACT_DIR=folder), \
                 patch.object(ONLINE.FIXTURE, "boundary", return_value=artifacts), patch.object(ONLINE, "current_processes", return_value=initial), \
                 patch.object(ONLINE, "instance_list", side_effect=[[stopped], [new], [peer], [stopped], [rejected], [new]]), \
                 patch.object(ONLINE, "monitor_lifecycle") as lifecycle, patch.object(ONLINE, "process_query_ready", return_value=True), \
                 patch.object(ONLINE, "assert_process_summaries"), patch.object(ONLINE, "write_evidence"), \
                 patch.object(ONLINE.API.GatewayClient, "request") as health, patch.object(ONLINE, "execute_resource_browser", return_value={}), \
                 patch.object(ONLINE, "validate_host_binding_browser", return_value={"result": "passed"}) as browser:
                self.assertEqual(ONLINE.run_host_binding_case("base", artifacts, admin, admission["identity"], host, target, report), restored)
                self.assertEqual(lifecycle.call_args_list, [unittest.mock.call(artifacts, "stop"), unittest.mock.call(artifacts, "start", node_id=host["node_id"])])
                self.assertEqual(health.call_count, 2)
                self.assertEqual(report["host_binding"]["disabled"]["instance"]["instance_id"], new["instance_id"])
                self.assertEqual(report["host_binding"]["result"], "passed")
                self.assertEqual(browser.call_args.args[1]["binding"]["host"]["version"], 3)
                writes = [call for call in admin.request.call_args_list if call.args[0] == "PUT"]
                self.assertEqual([(call.args[3]["version"], call.args[3]["enabled"]) for call in writes], [(1, False), (2, True)])

    def test_binding_lifecycle_injects_the_exact_host_only_into_the_owned_new_process(self):
        node = "10000000-0000-4000-8000-000000000001"
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            with patch.object(ONLINE.FIXTURE, "boundary", return_value=root), patch.object(ONLINE.subprocess, "run") as launch, \
                 patch.dict(ONLINE.os.environ, MONITOR_URL="http://localhost:8100"), \
                 patch.object(ONLINE.PREFLIGHT, "load_health", side_effect=[{}, {"status": "ready"}]), \
                 patch.object(ONLINE.PREFLIGHT, "validate_health"), patch.object(ONLINE.FIXTURE, "command", return_value="current-source"):
                before = dict(ONLINE.os.environ)
                ONLINE.monitor_lifecycle(root, "start", node_id=node)
                launch.assert_called_once()
                self.assertEqual(launch.call_args.args[0], ["bash", "scripts/dev/start.sh", "-monitor"])
                self.assertEqual(launch.call_args.kwargs["env"]["ADDP_HOST_NODE_ID"], node)
                self.assertEqual(dict(ONLINE.os.environ), before)
                self.assertFalse((root / ".env").exists())

    def test_binding_lifecycle_rejects_personal_and_invalid_identity_before_launch(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            with patch.object(ONLINE.FIXTURE, "boundary", side_effect=ValueError("personal")), patch.object(ONLINE.subprocess, "run") as launch:
                with self.assertRaises(ValueError): ONLINE.monitor_lifecycle(root, "start", node_id="invalid")
                launch.assert_not_called()
            with patch.object(ONLINE.FIXTURE, "boundary", return_value=root), patch.object(ONLINE.subprocess, "run") as launch:
                for action, node in (("start", "bad"), ("start", "00000000-0000-0000-0000-000000000000"), ("stop", "10000000-0000-4000-8000-000000000001")):
                    with self.assertRaises(ONLINE.SuiteError): ONLINE.monitor_lifecycle(root, action, node_id=node)
                launch.assert_not_called()

    def test_renewal_after_browser_mfa_uses_a_later_natural_counter(self):
        clock = [186.5]
        secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
        previous = int(clock[0])//30
        sleeps, writes = [], []
        context = identity(); context["principal"]["id"] = "admin"
        def sleep(seconds):
            sleeps.append(seconds); clock[0] += seconds
        def request(method, path, statuses, body=None):
            if path.endswith("/system/login"):
                writes.append("password")
                return ONLINE.API.Response(200, {"next_action": "verify_mfa", "mfa": {"method": "totp", "challenge_token": "test-challenge"}})
            if path.endswith("/auth/mfa-verifications"):
                writes.append("mfa")
                if int(clock[0])//30 <= previous: raise ONLINE.SuiteError("TOTP replay rejected")
                self.assertTrue(body["code"] == ONLINE.totp(secret, clock[0]))
                return ONLINE.API.Response(200, {"next_action": "session_issued", "session": {"access_token": "addp_at_test-only"}})
            self.assertEqual(path, "/api/v1/system/auth/context")
            return ONLINE.API.Response(200, context)
        with patch.dict(ONLINE.os.environ, ADDP_ONLINE_METRICS_ADMIN_USERNAME="test-admin", ADDP_ONLINE_METRICS_ADMIN_PASSWORD="test-password", ADDP_ONLINE_METRICS_ADMIN_TOTP=secret), \
             patch.object(ONLINE.time, "time", side_effect=lambda: clock[0]), patch.object(ONLINE.time, "sleep", side_effect=sleep), \
             patch.object(ONLINE.API.GatewayClient, "request", side_effect=request):
            ONLINE.renew_admin("http://example.invalid", "admin")
        self.assertEqual(writes, ["password", "mfa"])
        self.assertEqual(len(sleeps), 1)
        self.assertGreater(sleeps[0], 0); self.assertLessEqual(sleeps[0], 30.15)
        self.assertEqual(int(clock[0])//30, previous+1)

    def test_renewal_does_not_retry_a_failed_authentication(self):
        with patch.object(ONLINE.time, "time", return_value=186.5), patch.object(ONLINE.time, "sleep"), \
             patch.object(ONLINE, "login", side_effect=ONLINE.SuiteError("MFA 401")) as login:
            with self.assertRaisesRegex(ONLINE.SuiteError, "MFA 401"):
                ONLINE.renew_admin("http://example.invalid", "admin")
            login.assert_called_once_with("http://example.invalid", "ADDP_ONLINE_METRICS_ADMIN")

    def test_renewed_admin_must_keep_role_and_principal(self):
        for role, principal in (("platform.system_administrator", "admin"), ("platform.system_administrator", "other"), ("platform.security_administrator", "admin")):
            context=identity(role);context["principal"]["id"]=principal
            client=unittest.mock.Mock(request=unittest.mock.Mock(return_value=ONLINE.API.Response(200,context)))
            with self.subTest(role=role,principal=principal), patch.object(ONLINE.time,"time",return_value=186.5), patch.object(ONLINE.time,"sleep"), patch.object(ONLINE,"login",return_value=client):
                if role=="platform.system_administrator" and principal=="admin":
                    self.assertIs(ONLINE.renew_admin("http://example.invalid","admin"),client)
                else:
                    with self.assertRaises(ONLINE.SuiteError):ONLINE.renew_admin("http://example.invalid","admin")

    def test_history_waits_for_both_complete_natural_series(self):
        attempts = []; clock = [0.0]
        def request(method, path, statuses):
            attempts.append(path)
            return ONLINE.API.Response(200, self.reply(path, gap=len(attempts)==1))
        admin = unittest.mock.Mock(request=unittest.mock.Mock(side_effect=request))
        with patch.object(ONLINE.time, "monotonic", side_effect=lambda:clock[0]), \
             patch.object(ONLINE.time, "sleep", side_effect=lambda n:clock.__setitem__(0,clock[0]+n)):
            evidence = ONLINE.wait_for_complete_comparison_history(admin, self.node, self.target, self.policy)
        self.assertEqual(len(attempts), 2)
        self.assertEqual(evidence["attempts"], 2)
        self.assertEqual(evidence["duration_ms"], 15000)
        self.assertTrue(all(len(row["points"])==41 and all(p["data_state"]=="valid" for p in row["points"]) for row in evidence["value"]["series"]))

    def test_history_does_not_fill_gaps_or_exceed_its_bounded_wait(self):
        clock = [0.0]
        admin = unittest.mock.Mock(request=unittest.mock.Mock(side_effect=lambda method,path,statuses:ONLINE.API.Response(200,self.reply(path,gap=True))))
        with patch.object(ONLINE.time, "monotonic", side_effect=lambda:clock[0]), \
             patch.object(ONLINE.time, "sleep", side_effect=lambda n:clock.__setitem__(0,clock[0]+n)):
            with self.assertRaisesRegex(ONLINE.SuiteError, "complete comparison history"):
                ONLINE.wait_for_complete_comparison_history(admin, self.node, self.target, self.policy)
        self.assertLessEqual(clock[0],720)
        self.assertLessEqual(admin.request.call_count,49)

    def test_history_rejects_bad_scope_policy_and_grid_without_waiting(self):
        mutations = (
            lambda v:v["subject"].update(node_id="foreign"),
            lambda v:v.update(target_saved_version=3),
            lambda v:v.update(policy_version=1),
            lambda v:v["series"][0]["points"].pop(7),
            lambda v:v["series"][0]["points"][7].update(value=-1),
        )
        for change in mutations:
            def request(method,path,statuses):
                value=self.reply(path);change(value);return ONLINE.API.Response(200,value)
            with self.subTest(change=change), patch.object(ONLINE.time,"sleep") as sleep:
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.wait_for_complete_comparison_history(unittest.mock.Mock(request=request),self.node,self.target,self.policy)
                sleep.assert_not_called()

    def test_comparison_case_requires_all_three_complete_branches_and_cleans_only_its_subjects(self):
        for completed in (2,3):
            report={"result":"failed"}; admin=unittest.mock.Mock()
            def request(method,path,statuses,body=None):
                if method=="PUT":return ONLINE.API.Response(200,dict(self.node,version=2,enabled=False))
                return ONLINE.API.Response(next(iter(statuses)),{})
            admin.request.side_effect=request
            with self.subTest(completed=completed), tempfile.TemporaryDirectory() as directory, \
                 patch.dict(ONLINE.os.environ,ADDP_ONLINE_ARTIFACT_DIR=directory), \
                 patch.object(ONLINE,"renew_admin",return_value=admin), \
                 patch.object(ONLINE,"wait_for_complete_comparison_history",return_value={"attempts":1,"value":{"series":[]}}), \
                 patch.object(ONLINE,"run_resource_browser",return_value={"result":"passed","period_comparison":{"complete_cases":completed,"incomplete_cases":3-completed}}), \
                 patch.object(ONLINE,"run_resource_alert_lifecycle",side_effect=AssertionError("must not repeat pressure")) as memory, \
                 patch.object(ONLINE,"source_action",side_effect=AssertionError("must not repeat faults")):
                args=("http://example.invalid",self.node,self.target,self.policy,{"display_name":"owned"},{"principal":{"id":"admin"}},{},[],report)
                if completed!=3:
                    with self.assertRaisesRegex(ONLINE.SuiteError,"all three"):
                        ONLINE.run_complete_comparison_case(*args)
                    admin.request.assert_not_called()
                    self.assertEqual(report["result"],"failed")
                else:
                    ONLINE.run_complete_comparison_case(*args)
                    self.assertEqual(report["result"],"passed")
                    self.assertTrue(report["configuration_retired"])
                    self.assertEqual([(c.args[0],c.args[1]) for c in admin.request.call_args_list],[("DELETE",ONLINE.TARGETS+"/target-id"),("GET",ONLINE.TARGETS+"/target-id"),("PUT",ONLINE.NODES+"/node-id")])
                memory.assert_not_called()


class ResourceAlertProtocolTest(unittest.TestCase):
    def context(self):
        node = {"node_id": "11111111-1111-4111-8111-111111111111", "version": 1}
        target = {"id": "22222222-2222-4222-8222-222222222222", "version": 2}
        policy = {"version": 2}
        rule = {"id": "33333333-3333-4333-8333-333333333333", "version": 2,
                "metric_key": "node.memory.used_percent", "trigger_percent": 40, "recovery_percent": 35, "trigger_seconds": 30, "recovery_seconds": 30}
        return node, target, policy, rule

    def event(self, kind="opened"):
        node, target, policy, rule = self.context()
        evidence = {"rule_id": rule["id"], "rule_version": 2, "policy": {k: rule[k] for k in ONLINE.PRESSURE_FIELDS},
                    "node_version": 1, "target_id": target["id"], "target_version": 2, "query_policy_version": 2,
                    "evaluated_at": "2026-10-17T01:01:00Z", "sampled_from": "2026-10-17T01:00:29Z",
                    "sampled_to": "2026-10-17T01:00:59Z", "samples": 3,
                    "minimum": 42, "maximum": 44, "latest": 43}
        if kind == "resolved": evidence.update(minimum=29, maximum=31, latest=30)
        return {"schema": "addp.platform-resource-alert/v1", "event_id": "44444444-4444-4444-8444-444444444444",
                "event_type": kind, "incident_id": 1, "category": "resource", "subject_kind": "host_node",
                "subject_id": node["node_id"], "signal": "node.memory.used_percent", "severity": "warning",
                "occurred_at": evidence["evaluated_at"], "evidence": evidence}

    def receiver(self):
        node, target, policy, rule = self.context()
        receiver = ONLINE.ResourceAlertReceiver(Path("/unused"), node, target, policy)
        receiver.rule = rule
        return receiver

    def signed(self, receiver, payload=None, stamp=None, delivery="55555555-5555-4555-8555-555555555555"):
        body = json.dumps(self.event() if payload is None else payload).encode()
        stamp = str(int(ONLINE.time.time())) if stamp is None else stamp
        signature = "v1=" + ONLINE.hmac.new(receiver.secret.encode(), stamp.encode()+b"."+body, ONLINE.hashlib.sha256).hexdigest()
        return body, {"X-ADDP-Webhook-ID": delivery, "X-ADDP-Webhook-Timestamp": stamp,
                      "X-ADDP-Webhook-Signature": signature}

    def test_cpu_budget_caps_both_machine_fraction_and_core_count(self):
        for cores in (1,2,4,8,12):
            plan=ONLINE.cpu_pressure_plan(cores,5)
            self.assertLessEqual(plan['quota_nanos'],1_000_000_000)
            self.assertLessEqual(plan['quota_nanos'],cores*250_000_000)
            self.assertLess(plan['recovery_percent'],plan['trigger_percent'])
            self.assertLess(plan['trigger_percent'],100)
            self.assertEqual(plan['max_seconds'],300)
        for args in ((True,5),(0,5),(2.5,5),(64,5),(2,float('nan')),(2,99)):
            with self.subTest(args=args),self.assertRaises(ONLINE.SuiteError):ONLINE.cpu_pressure_plan(*args)

    def test_cpu_event_must_match_the_configured_rule_signal(self):
        node,target,policy,rule=self.context();rule['metric_key']='node.cpu.busy_percent'
        event=self.event();event['signal']='node.cpu.busy_percent'
        ONLINE.assert_resource_alert_event(event,node,target,policy,rule)
        event['signal']='node.memory.used_percent'
        with self.assertRaises(ONLINE.SuiteError):ONLINE.assert_resource_alert_event(event,node,target,policy,rule)

    def test_memory_budget_uses_smallest_cap_and_rejects_indistinguishable_or_invalid_pressure(self):
        gib = 1024**3
        for total, available, expected in ((8*gib, 6*gib, int(8*gib*.12)), (16*gib, 6*gib, int(1.5*gib)), (20*gib, 18*gib, 2*gib)):
            plan = ONLINE.memory_pressure_plan(total, available, 100*(1-available/total))
            self.assertEqual(plan["bytes"], expected)
            self.assertLess(plan["recovery_percent"], plan["trigger_percent"])
            self.assertGreaterEqual(plan["delta_percent"], 8)
            self.assertEqual(plan["trigger_seconds"], 30)
        for args in ((32*gib, 24*gib, 25), (8*gib, gib, 87.5), (0, 0, 0), (True, 1, 0), (gib, 2*gib, 0), (gib, gib, float("nan")), (gib, gib, 99)):
            with self.subTest(args=args), self.assertRaises(ONLINE.SuiteError): ONLINE.memory_pressure_plan(*args)

    def test_memory_mapping_cannot_allocate_before_hosted_boundary_and_always_closes(self):
        plan = ONLINE.memory_pressure_plan(8*1024**3, 6*1024**3, 25)
        report = {}
        with patch.object(ONLINE.FIXTURE, "boundary", side_effect=ValueError("not hosted")), patch.object(ONLINE.mmap, "mmap") as allocate:
            with self.assertRaises(ValueError):
                with ONLINE.native_memory_pressure(Path("/secret"), plan, report): pass
            allocate.assert_not_called()
        mapping = unittest.mock.MagicMock()
        with patch.object(ONLINE.FIXTURE, "boundary", return_value=Path("/secret")), patch.object(ONLINE.mmap, "mmap", return_value=mapping), patch.object(ONLINE.os, "sysconf", return_value=plan["bytes"]):
            with self.assertRaisesRegex(ONLINE.SuiteError, "original failure"):
                with ONLINE.native_memory_pressure(Path("/secret"), plan, report): raise ONLINE.SuiteError("original failure")
        mapping.close.assert_called_once()
        self.assertTrue(report["memory_released"])

    def test_resource_event_requires_current_scope_finite_thresholds_and_real_sample_window(self):
        node, target, policy, rule = self.context()
        for kind in ("opened", "resolved"):
            valid = self.event(kind)
            ONLINE.assert_resource_alert_event(valid, node, target, policy, rule)
            mutations = [lambda v: v.update(extra="private"), lambda v: v.update(subject_id="foreign"),
                         lambda v: v.update(incident_id=True), lambda v: v.update(event_type="test"),
                         lambda v: v["evidence"].update(rule_version=1), lambda v: v["evidence"].update(target_version=1),
                         lambda v: v["evidence"].update(query_policy_version=1), lambda v: v["evidence"].update(samples=True),
                         lambda v: v["evidence"].update(minimum=float("nan")), lambda v: v["evidence"].update(latest=101),
                         lambda v: v["evidence"].update(sampled_from="2026-10-17T01:00:45Z"),
                         lambda v: v["evidence"].update(sampled_to="2026-10-17T01:01:01Z"),
                         lambda v: v["evidence"]["policy"].update(trigger_percent=99)]
            mutations.append((lambda v: v["evidence"].update(minimum=40)) if kind == "opened" else (lambda v: v["evidence"].update(maximum=35)))
            for mutate in mutations:
                bad = copy.deepcopy(valid); mutate(bad)
                with self.subTest(kind=kind, mutate=mutate), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.assert_resource_alert_event(bad, node, target, policy, rule)

    def test_alert_lifecycle_time_accepts_equivalent_offsets_but_resource_evidence_stays_utc(self):
        node, target, policy, rule = self.context()
        event = self.event()
        event["occurred_at"] = "2026-10-17T09:01:00+08:00"
        ONLINE.assert_resource_alert_event(event, node, target, policy, rule)
        for stamp in ("2026-10-17T01:01:00+08:00", "2026-10-17T01:01:00", "invalid"):
            bad = copy.deepcopy(event); bad["occurred_at"] = stamp
            with self.subTest(stamp=stamp), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_resource_alert_event(bad, node, target, policy, rule)
        for field, stamp in (("evaluated_at", "2026-10-17T09:01:00+08:00"),
                             ("sampled_from", "2026-10-17T09:00:29+08:00"),
                             ("sampled_to", "2026-10-17T09:00:59+08:00")):
            bad = copy.deepcopy(event); bad["evidence"][field] = stamp
            with self.subTest(field=field), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_resource_alert_event(bad, node, target, policy, rule)

    def test_signed_receiver_retries_same_delivery_but_rejects_rebinding_and_secret_reflection(self):
        receiver = self.receiver()
        body, headers = self.signed(receiver)
        self.assertEqual(receiver.accept(body, headers), 503)
        self.assertEqual(receiver.accept(body, headers), 202)
        self.assertEqual([r["response_status"] for r in receiver.snapshot()], [503, 202])
        bad = self.event(); bad["incident_id"] = 2
        changed, signed = self.signed(receiver, bad)
        self.assertEqual(receiver.accept(changed, signed), 400)
        self.assertEqual(receiver.invalid, 1)
        self.assertNotIn(receiver.secret, json.dumps(receiver.snapshot()))
        self.assertNotIn(headers["X-ADDP-Webhook-Signature"], json.dumps(receiver.snapshot()))

    def test_receiver_rejects_unsigned_stale_duplicate_keys_and_oversized_payloads(self):
        for mode in ("unsigned", "old", "oversized", "duplicate", "foreign", "wrong_id"):
            receiver = self.receiver()
            body, headers = self.signed(receiver)
            if mode == "unsigned": headers["X-ADDP-Webhook-Signature"] = "v1=bad"
            if mode == "old": body, headers = self.signed(receiver, stamp=str(int(ONLINE.time.time())-60))
            if mode == "oversized": body = b" "*65537
            if mode == "duplicate":
                body = body[:-1]+b',"category":"resource"}'
                stamp = headers["X-ADDP-Webhook-Timestamp"]
                headers["X-ADDP-Webhook-Signature"] = "v1="+ONLINE.hmac.new(receiver.secret.encode(), stamp.encode()+b"."+body, ONLINE.hashlib.sha256).hexdigest()
            if mode == "foreign":
                value = self.event(); value["subject_id"] = "foreign"; body, headers = self.signed(receiver, value)
            if mode == "wrong_id": headers["X-ADDP-Webhook-ID"] = "bad"
            with self.subTest(mode=mode):
                self.assertEqual(receiver.accept(body, headers), 400)
                self.assertEqual(receiver.snapshot(), [])
                self.assertEqual(receiver.invalid, 1)

    def test_delivery_audit_binds_real_retry_and_rejects_private_fields(self):
        receiver = self.receiver()
        body, headers = self.signed(receiver)
        receiver.accept(body, headers); receiver.accept(body, headers)
        event = self.event()
        row = {"id": headers["X-ADDP-Webhook-ID"], "event_id": event["event_id"], "destination_id": 7,
               "channel": "webhook", "status": "delivered", "attempt_count": 2, "manual_retry_count": 0,
               "cycle_attempt_count": 2, "event_type": "opened", "occurred_at": event["occurred_at"],
               "incident_id": 1, "category": "resource", "subject_kind": "host_node", "subject_id": event["subject_id"],
               "incident_status": "open", "destination_version": 3, "delivered_at": "2026-10-17T01:01:05Z"}
        ONLINE.assert_alert_delivery(row, receiver.snapshot(), {"id": 7, "version": 3}, "open")
        offset = dict(row, occurred_at="2026-10-17T09:01:00+08:00", delivered_at="2026-10-17T09:01:05+08:00")
        ONLINE.assert_alert_delivery(offset, receiver.snapshot(), {"id": 7, "version": 3}, "open")
        for field, value in (("event_id", "foreign"), ("attempt_count", 1), ("manual_retry_count", 1), ("destination_id", 8), ("payload", "private"), ("status", "pending"), ("destination_version", 2),
                             ("occurred_at", "2026-10-17T01:01:00+08:00"), ("occurred_at", "2026-10-17T01:01:00"),
                             ("delivered_at", "2026-10-17T09:00:59+08:00"), ("delivered_at", "2026-10-17T01:01:05")):
            bad = dict(row); bad[field] = value
            with self.subTest(field=field), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_alert_delivery(bad, receiver.snapshot(), {"id": 7, "version": 3}, "open")


    def test_pressure_calibration_ignores_samples_from_before_control_setup(self):
        node, target, _, _ = self.context()
        after = ONLINE.utc_timestamp('2026-10-17T01:00:00Z')
        def sample(stamp, percent):
            total = 8*1024**3
            return {'series': [{'metric_key': key, 'points': [{'value': value, 'sampled_at': stamp}]} for key, value in
                               zip(ONLINE.MEMORY_KEYS, (total, total*(1-percent/100), percent))]}
        old = sample('2026-10-17T00:59:59Z', 35)
        fresh = sample('2026-10-17T01:00:01Z', 25)
        admin = unittest.mock.Mock(request=unittest.mock.Mock(side_effect=[ONLINE.API.Response(200, old), ONLINE.API.Response(200, fresh)]))
        detail = {}
        def wait(check, message, timeout):
            self.assertEqual(timeout, 45)
            self.assertFalse(check())
            self.assertNotIn('memory_baseline', detail)
            self.assertTrue(check())
        with patch.object(ONLINE, 'assert_resources'), patch.object(ONLINE, 'eventually', side_effect=wait):
            plan = ONLINE.fresh_pressure_plan(admin, node, target, after, detail)
        self.assertEqual(plan['baseline_percent'], 25)
        self.assertEqual(detail['memory_baseline']['sampled_at'], '2026-10-17T01:00:01Z')

    def test_cpu_calibration_requires_both_current_core_count_and_busy_sample(self):
        node,target,_,_=self.context();after=ONLINE.utc_timestamp('2026-10-17T01:00:00Z')
        old={'series':[{'metric_key':key,'points':[{'value':value,'sampled_at':stamp}]} for key,value,stamp in (
            (ONLINE.CPU_KEYS[0],2.0,'2026-10-17T00:59:59Z'),(ONLINE.CPU_KEYS[1],5,'2026-10-17T01:00:01Z'))]}
        fresh=copy.deepcopy(old);fresh['series'][0]['points'][0]['sampled_at']='2026-10-17T01:00:01Z'
        client=unittest.mock.Mock(request=unittest.mock.Mock(side_effect=[ONLINE.API.Response(200,old),ONLINE.API.Response(200,fresh)]))
        def wait(check,message,timeout):self.assertFalse(check());self.assertTrue(check())
        with patch.object(ONLINE,'assert_resources'),patch.object(ONLINE,'eventually',side_effect=wait):
            detail={};plan=ONLINE.fresh_pressure_plan(client,node,target,after,detail,'cpu')
        self.assertEqual(plan['logical_cores'],2)
        self.assertEqual(plan['baseline_percent'],5)
        self.assertEqual(detail['cpu_baseline']['sampled_at'],'2026-10-17T01:00:01Z')

    def test_lifecycle_enforces_identity_cas_missing_source_recovery_and_history_cleanup(self):
        for pressure_kind, fail_at in (("memory",None),("memory","destination_enable"),("memory","source_unknown"),("cpu",None),("cpu","destination_enable"),("cpu","pressure_open")):
            with self.subTest(pressure_kind=pressure_kind,fail_at=fail_at):
                node, target, query_policy, _ = self.context()
                clock = [1800000000.0]
                state = {"rule": None, "destination": None, "incident": None, "deliveries": [], "phase": "baseline", "source_actions": [], "baseline_reads": 0}
                report = {}
                receiver = self.receiver()
                receiver.rule = None
                receiver.node, receiver.target, receiver.query_policy = node, target, query_policy
                receiver.url = "https://127.0.0.1:12345/resource-alert"
                def stamp(at=None, database=False):
                    value = datetime.datetime.fromtimestamp(clock[0] if at is None else at, datetime.timezone.utc)
                    return value.astimezone(datetime.timezone(datetime.timedelta(hours=8))).isoformat() if database else value.strftime("%Y-%m-%dT%H:%M:%SZ")
                def notify(kind):
                    incident, rule = state["incident"], state["rule"]
                    evidence = {"rule_id": rule["id"], "rule_version": 2, "policy": {k: rule[k] for k in ONLINE.PRESSURE_FIELDS},
                                "node_version": 1, "target_id": target["id"], "target_version": 2, "query_policy_version": 2,
                                "evaluated_at": stamp(), "sampled_from": stamp(clock[0]-31), "sampled_to": stamp(clock[0]-1),
                                "samples": 3, "minimum": 42 if kind == "opened" else 25, "maximum": 44 if kind == "opened" else 26,
                                "latest": 43 if kind == "opened" else 25}
                    if kind == "opened": incident["resource_evidence"] = evidence
                    event = {"schema": "addp.platform-resource-alert/v1", "event_id": ("4" if kind == "opened" else "6")*8+"-4444-4444-8444-444444444444",
                             "event_type": kind, "incident_id": 9, "category": "resource", "subject_kind": "host_node", "subject_id": node["node_id"],
                             "signal": ONLINE.PRESSURE_METRICS[pressure_kind][-1], "severity": "warning", "occurred_at": stamp(), "evidence": evidence}
                    body, headers = self.signed(receiver, event, delivery=("5" if kind == "opened" else "7")*8+"-5555-4555-8555-555555555555")
                    for _ in range(2 if kind == "opened" else 1): receiver.accept(body, headers)
                    state["deliveries"].append({"id": headers["X-ADDP-Webhook-ID"], "event_id": event["event_id"], "event_type": kind,
                        "destination_id": 7, "channel": "webhook", "status": "delivered", "attempt_count": 2 if kind == "opened" else 1,
                        "cycle_attempt_count": 2 if kind == "opened" else 1, "manual_retry_count": 0, "occurred_at": stamp(database=True), "incident_id": 9,
                        "category": "resource", "subject_kind": "host_node", "subject_id": node["node_id"], "delivered_at": stamp(database=True)})
                def page(rows): return {"data": copy.deepcopy(rows), "total": len(rows), "page": 1, "page_size": 100}
                def request(method, path, expected, body=None, **kwargs):
                    status, value = 200, {}
                    route = path.split("?")[0]
                    if route == ONLINE.OBSERVATIONS:
                        # Setup/browser memory can disappear before enablement. The pressure plan must use a new causal baseline.
                        percent = 35 if state["baseline_reads"] == 0 else 25
                        state["baseline_reads"] += 1
                        total = 8*1024**3
                        values=(total,total*(1-percent/100),percent) if pressure_kind=="memory" else (2,percent)
                        value = {"series": [{"metric_key": k, "points": [{"value": v, "sampled_at": stamp()}]} for k, v in zip(ONLINE.PRESSURE_METRICS[pressure_kind],values)]}
                    elif route == ONLINE.RESOURCE_RULES and method == "GET":
                        rows = [] if state["rule"] is None else [dict(state["rule"])]
                        if rows and rows[0]["enabled"]:
                            phase = state["phase"]
                            verdict = "unknown" if phase == "unknown" else "risk" if phase == "high" else "normal"
                            rows[0].update(last_verdict=verdict, last_error="no_data" if verdict == "unknown" else "", last_evaluated_at=stamp(database=True))
                            if verdict != "unknown": rows[0].update(last_sample_at=stamp(database=True), last_value=43 if verdict == "risk" else 25)
                            if fail_at == "source_unknown" and phase == "unknown": raise ONLINE.SuiteError("original source failure")
                        value = page(rows)
                    elif route.startswith(ONLINE.RESOURCE_RULES):
                        old = state["rule"]
                        if method != "POST" and (body["version"] != old["version"] or method == "DELETE" and state["incident"] and state["incident"]["status"] == "open"):
                            status = 409
                        elif method == "DELETE": state["rule"] = None
                        else:
                            state["rule"] = dict(body, id="33333333-3333-4333-8333-333333333333", version=1 if old is None else old["version"]+1,
                                                 last_verdict="unknown" if body["enabled"] else "paused")
                            value = copy.deepcopy(state["rule"])
                    elif route == ONLINE.ALERT_DESTINATIONS and method == "GET":
                        value = [] if state["destination"] is None else [copy.deepcopy(state["destination"])]
                    elif route.startswith(ONLINE.ALERT_DESTINATIONS):
                        old = state["destination"]
                        if method != "POST" and body["version"] != old["version"]: status = 409
                        elif route.endswith("/credential"):
                            old.update(version=old["version"]+1, secret_configured=True); value = dict(old)
                        elif method == "DELETE": state["destination"] = None
                        elif method == "PUT" and body["enabled"] and fail_at == "destination_enable": raise ONLINE.SuiteError("original destination failure")
                        else:
                            state["destination"] = dict(body, id=7, version=1 if old is None else old["version"]+1,
                                                        secret_configured=False if old is None else old["secret_configured"])
                            value = dict(state["destination"])
                    elif route == ONLINE.ALERTS:
                        if fail_at=="pressure_open" and state["phase"]=="high":raise ONLINE.SuiteError("original pressure failure")
                        if state["phase"] == "high" and state["incident"] is None:
                            state["incident"] = {"id": 9, "category": "resource", "subject_kind": "host_node", "subject_id": node["node_id"],
                                                 "signal": ONLINE.PRESSURE_METRICS[pressure_kind][-1], "severity": "warning", "status": "open", "opened_at": stamp(database=True)}
                            notify("opened")
                        if state["phase"] == "released" and state["incident"]["status"] == "open":
                            state["incident"].update(status="resolved", resolved_at=stamp(database=True)); notify("resolved")
                        value = page([] if state["incident"] is None else [state["incident"]])
                    elif route == ONLINE.ALERT_DELIVERIES:
                        rows = [dict(r, destination_version=0 if state["destination"] is None else state["destination"]["version"], incident_status=state["incident"]["status"]) for r in state["deliveries"]]
                        value = page(rows)
                    else: self.fail("unexpected route")
                    self.assertIn(status, expected)
                    return ONLINE.API.Response(status, value)
                def wait(check, message, timeout=100):
                    clock[0] += 60
                    self.assertTrue(check(), message)
                @ONLINE.contextmanager
                def pressure(directory, plan, detail):
                    self.assertGreaterEqual(state["baseline_reads"], 2)
                    self.assertEqual(plan['baseline_percent'], 25)
                    self.assertLess(state['rule']['trigger_percent'], 35 if pressure_kind=="memory" else 45)
                    state["phase"] = "high"; detail["pressure_started_at"] = clock[0]
                    try: yield
                    finally:
                        state["phase"] = "released"; detail.update({pressure_kind+"_released":True,"pressure_released_at":clock[0]})
                def action(value):
                    state["source_actions"].append(value)
                    state["phase"] = "unknown" if value == "stop" else "high"
                admin = unittest.mock.Mock(base_url="http://example.invalid", request=unittest.mock.Mock(side_effect=request))
                denied = unittest.mock.Mock(token="negative", request=unittest.mock.Mock(return_value=ONLINE.API.Response(403, {})))
                def enter(_receiver): return receiver
                def close(_receiver, *_args): receiver.closed = True
                with patch.dict(ONLINE.os.environ, {"ADDP_ONLINE_TEST_RUN_ID": "test"}), patch.object(ONLINE.FIXTURE, "boundary", return_value=Path("/secret")), \
                     patch.object(ONLINE, "assert_resources"), patch.object(ONLINE, "ResourceAlertReceiver", return_value=receiver), \
                     patch.object(type(receiver), "__enter__", enter), patch.object(type(receiver), "__exit__", close), \
                     patch.object(ONLINE.API, "GatewayClient", return_value=denied), patch.object(ONLINE, "eventually", side_effect=wait), \
                     patch.object(ONLINE, "write_evidence") as checkpoint, patch.object(ONLINE, "native_memory_pressure", side_effect=pressure) as memory_load, patch.object(ONLINE.FIXTURE,"cpu_pressure",side_effect=pressure) as cpu_load, patch.object(ONLINE, "source_action", side_effect=action), \
                     patch.object(ONLINE, "source_scrape_failed", return_value=True), patch.object(ONLINE.time, "time", side_effect=lambda: clock[0]), \
                     patch.object(ONLINE.time, "monotonic", side_effect=lambda: clock[0]), patch.object(ONLINE.time, "sleep", side_effect=lambda seconds: clock.__setitem__(0, clock[0]+seconds)):
                    if fail_at:
                        with self.assertRaisesRegex(ONLINE.SuiteError, "original .* failure"):
                            ONLINE.run_resource_alert_lifecycle(admin, denied, denied, [denied, denied], unittest.mock.Mock(), node, target, query_policy, Path("/secret"), report, pressure_kind=pressure_kind)
                        self.assertFalse(state["destination"]["enabled"])
                        checkpoint.assert_not_called()
                        self.assertTrue(report["resource_alert_lifecycle"]["receiver_closed"])
                        if fail_at == "source_unknown":
                            self.assertTrue(report["resource_alert_lifecycle"]["memory_released"])
                            self.assertFalse(state["rule"]["enabled"])
                            self.assertEqual(state["source_actions"], ["stop", "start"])
                    else:
                        ONLINE.run_resource_alert_lifecycle(admin, denied, denied, [denied, denied], unittest.mock.Mock(), node, target, query_policy, Path("/secret"), report, pressure_kind=pressure_kind)
                        detail = report["resource_alert_lifecycle"]
                        self.assertEqual(detail["result"], "passed")
                        checkpoint.assert_called_once_with(report)
                        self.assertTrue(detail["configuration_removed_history_retained"])
                        self.assertIsNone(state["rule"]); self.assertIsNone(state["destination"])
                        if pressure_kind=="memory":
                            self.assertGreaterEqual(detail["unknown_does_not_resolve"]["duration_seconds"], 45)
                        else:self.assertNotIn("unknown_does_not_resolve",detail)
                        self.assertEqual(state["source_actions"], ["stop", "start"] if pressure_kind=="memory" else [])
                        self.assertEqual(detail["opened_incident"]["id"], detail["resolved_incident"]["id"])
                        self.assertEqual(len(detail["signed_receipts"]), 3)
                    self.assertEqual(memory_load.called,pressure_kind=="memory" and fail_at!="destination_enable")
                    self.assertEqual(cpu_load.called,pressure_kind=="cpu" and fail_at!="destination_enable")
                    if fail_at=="pressure_open":
                        self.assertTrue(report["resource_alert_lifecycle"]["cpu_released"])
                        self.assertFalse(state["rule"]["enabled"])
                        self.assertEqual(state["source_actions"],[])


class MetricsEvidenceCheckpointTest(unittest.TestCase):
    def test_completed_phase_survives_abrupt_exit_without_passing_the_suite(self):
        import os
        import subprocess
        import sys
        with tempfile.TemporaryDirectory() as directory:
            report = {"schema_version": "addp.platform-node-metrics-online/v1", "result": "failed",
                      "stage": "resource-alert-real-memory-lifecycle", "resource_alert_lifecycle": {"result": "passed"}}
            source = ("import os, importlib; m=importlib.import_module('scripts.test.platform-node-metrics-online'); "
                      "m.write_evidence("+repr(report)+"); os._exit(0)")
            result = subprocess.run([sys.executable, "-c", source], cwd=Path(__file__).resolve().parents[2],
                                    env=dict(os.environ, ADDP_ONLINE_ARTIFACT_DIR=directory), capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            output = Path(directory) / "platform-node-metrics.json"
            self.assertEqual(json.loads(output.read_text()), report)
            self.assertEqual(list(Path(directory).iterdir()), [output])
            with patch.dict(ONLINE.os.environ, ADDP_ONLINE_ARTIFACT_DIR=directory):
                ONLINE.write_evidence(dict(report, result="passed", stage="complete"))
            self.assertEqual(json.loads(output.read_text())["stage"], "complete")
            with patch.dict(ONLINE.os.environ, ADDP_ONLINE_ARTIFACT_DIR=directory), \
                 patch.object(ONLINE.Path, "replace", side_effect=OSError("publication failed")):
                with self.assertRaisesRegex(OSError, "publication failed"):
                    ONLINE.write_evidence(report)
            self.assertEqual(json.loads(output.read_text())["stage"], "complete")
            self.assertEqual(list(Path(directory).iterdir()), [output])



class EngineAdmissionEvidenceTest(unittest.TestCase):
    def test_browser_receipts_require_exact_batch_identity_scope_reload_and_screenshots(self):
        with tempfile.TemporaryDirectory() as folder:
            artifacts=Path(folder);host="10000000-0000-4000-8000-000000000001"
            items=[{"scope":scope,"service":"postgres","fingerprint":letter*64,"display_name":scope+" PG","enabled":True,"declared_node_id":host} for scope,letter in (("infra","a"),("business","b"))]
            expected={"run_id":"run","admin_id":"admin-id","engine_admission":{"items":items}}
            actor=identity();actor["principal"]["id"]="admin-id"
            created=[{"deployment_id":"20000000-0000-4000-8000-00000000000"+str(index+1),"version":1,"display_name":item["display_name"],"engine_type":"postgresql","enabled":True,"declared_node_id":host,"node_id":host,"host_binding_state":"bound","inventory":{"schema":"addp.engine-deployment-inventory/v1","source":{"project":"addp-infra" if item["scope"]=="infra" else "business"},"services":[{"containers":[{"container_id":("a" if index==0 else "b")*64}]}]}} for index,item in enumerate(items)]
            value={"schema_version":"addp.node-resources-browser/v1","run_id":"run","result":"passed","stage":"complete","identity":actor,"engine_admission":{"writes":[{"items":items}],"created":created,"reloaded":True,"registered_disabled":True,"current_sources":[{"deployment_id":row["deployment_id"],"container_id":row["inventory"]["services"][0]["containers"][0]["container_id"]} for row in created]}}
            for name in ("engine-admission-saved.png","engine-admission-restored.png"):(artifacts/name).write_bytes(b"\x89PNG\r\n\x1a\n"+b"x"*32)
            self.assertEqual(ONLINE.validate_engine_admission_browser(value,expected,artifacts),created)
            for mutate in (lambda v:v.update(run_id="foreign"),lambda v:v["identity"]["principal"].update(id="foreign"),lambda v:v["engine_admission"].update(reloaded=False),lambda v:v["engine_admission"].update(registered_disabled=False),lambda v:v["engine_admission"].update(current_sources=[]),lambda v:v["engine_admission"]["current_sources"][0].update(container_id="c"*64),lambda v:v["engine_admission"]["writes"].append({"items":items}),lambda v:v["engine_admission"]["writes"][0]["items"][0].update(declared_node_id="foreign"),lambda v:v["engine_admission"]["created"][0].update(version=True),lambda v:v["engine_admission"]["created"][0]["inventory"]["source"].update(project="foreign")):
                invalid=copy.deepcopy(value);mutate(invalid)
                with self.assertRaises(ONLINE.SuiteError):ONLINE.validate_engine_admission_browser(invalid,expected,artifacts)
            (artifacts/"engine-admission-restored.png").unlink()
            with self.assertRaises(ONLINE.SuiteError):ONLINE.validate_engine_admission_browser(value,expected,artifacts)

    def test_personal_boundary_rejects_before_any_engine_request(self):
        from unittest.mock import Mock
        admin=Mock()
        with patch.object(ONLINE.FIXTURE,"boundary",return_value=Path("/other")):
            with self.assertRaises(ONLINE.SuiteError):ONLINE.run_engine_admission_case("base",Path("/owned"),admin,{},[],{})
        admin.request.assert_not_called()


class EngineProfessionalEvidenceTest(unittest.TestCase):
    def setUp(self):
        self.at = "2026-10-21T00:05:00Z"
        self.owner = {"deployment_id":"20000000-0000-4000-8000-000000000001", "display_name":"Owned Infra PG", "node_id":"10000000-0000-4000-8000-000000000001",
                      "inventory":{"services":[{"containers":[{"started_at":"2026-10-21T00:01:00.123Z"}]}]}}

    def response(self, keys=None, *, trend=False, absent=False):
        keys = keys or ONLINE.ENGINE_METRICS
        points = [{"evaluated_at":self.at, "sampled_at":None if absent else self.at,
                   "data_state":"not_connected" if absent else "valid", "value":None if absent else 0}]
        if trend: points.insert(0, {"evaluated_at":"2026-10-21T00:04:45Z", "sampled_at":None, "data_state":"no_data", "value":None})
        return {"data":[{"subject":{"kind":"engine_deployment", "deployment_id":self.owner["deployment_id"], "display_name":self.owner["display_name"], "engine_type":"postgresql"},
                         "node_id":self.owner["node_id"], "collection":{"state":"not_connected" if absent else "collecting", "sampled_at":None if absent else self.at},
                         "series":[{"metric_key":key, "unit":ONLINE.ENGINE_METRICS[key][0], "window_seconds":ONLINE.ENGINE_METRICS[key][1], "dimensions":{}, "points":copy.deepcopy(points)} for key in keys]}],
                "queried_at":self.at, "start":"2026-10-21T00:04:45Z" if trend else self.at, "end":self.at, "step_seconds":15, "policy_version":1, "lookback_seconds":300}

    def test_actual_queries_include_required_closed_metric_selection(self):
        from urllib.parse import urlsplit, parse_qs
        for trend in (False, True):
            value = urlsplit(ONLINE.engine_query_path(self.owner["deployment_id"], trend))
            self.assertEqual(value.path, ONLINE.ENGINE_TRENDS if trend else ONLINE.ENGINE_OBSERVATIONS)
            query = parse_qs(value.query)
            self.assertEqual(query['deployment_id'], [self.owner['deployment_id']])
            self.assertEqual(set(query['metrics'][0].split(',')), {'postgresql.transactions.committed_per_second'} if trend else set(ONLINE.ENGINE_METRICS))
            self.assertEqual(set(query), {'deployment_id', 'metrics', 'start', 'end'} if trend else {'deployment_id', 'metrics'})

    def test_default_budget_version_zero_is_valid_and_invalid_metadata_rejects(self):
        value = self.response()
        value["policy_version"] = 0
        self.assertEqual(ONLINE.assert_engine_resources(value, [self.owner], fresh=True), value)
        for version in (-1, True, 0.5, None):
            invalid = copy.deepcopy(value)
            invalid["policy_version"] = version
            with self.subTest(version=version), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_engine_resources(invalid, [self.owner], fresh=True)

    def test_fixed_catalog_zero_is_valid_and_invalid_scope_units_or_private_fields_reject(self):
        value = self.response()
        self.assertEqual(ONLINE.assert_engine_resources(value, [self.owner], fresh=True), value)
        for mutate in (lambda v:v["data"][0]["subject"].update(deployment_id="foreign"),
                       lambda v:v["data"][0].update(endpoint="https://private/metrics"),
                       lambda v:v["data"][0]["series"].pop(),
                       lambda v:v["data"][0]["series"][0].update(unit="bytes"),
                       lambda v:v["data"][0]["series"][0]["points"][0].update(value=0.5),
                       lambda v:v["data"][0]["series"][0]["points"][0].update(value=True),
                       lambda v:v["data"][0]["series"][0]["points"][0].update(sampled_at="2026-10-21T00:00:00Z")):
            invalid = copy.deepcopy(value); mutate(invalid)
            with self.subTest(mutate=mutate), self.assertRaises(ONLINE.SuiteError): ONLINE.assert_engine_resources(invalid, [self.owner], fresh=True)

    def test_trend_retains_gap_but_requires_current_container_and_some_actual_sample(self):
        key = {"postgresql.transactions.committed_per_second"}
        value = self.response(key, trend=True)
        ONLINE.assert_engine_resources(value, [self.owner], keys=key, trend=True)
        old = copy.deepcopy(self.owner); old["inventory"]["services"][0]["containers"][0]["started_at"] = "2026-10-21T00:04:50Z"
        with self.assertRaises(ONLINE.SuiteError): ONLINE.assert_engine_resources(value, [old], keys=key, trend=True)
        value["data"][0]["series"][0]["points"][1].update(data_state="no_data", value=None, sampled_at=None)
        with self.assertRaises(ONLINE.SuiteError): ONLINE.assert_engine_resources(value, [self.owner], keys=key, trend=True)

    def test_stopped_source_must_clear_every_value_and_sample(self):
        value = self.response(absent=True)
        ONLINE.assert_engine_resources(value, [self.owner], absent=True)
        value["data"][0]["series"][0]["points"][0].update(value=0)
        with self.assertRaises(ONLINE.SuiteError): ONLINE.assert_engine_resources(value, [self.owner], absent=True)

    def test_browser_evidence_requires_all_endpoints_both_identities_units_and_screenshots(self):
        actor, denied = identity(), identity("platform.security_administrator")
        actor["principal"]["id"] = "admin"; denied["principal"]["id"] = "security"
        wanted = {"run_id":"run", "admin_id":"admin", "security_id":"security", "engine_resources":{"owners":[self.owner]}}
        reads = [{"path":ONLINE.ENGINE_SUMMARIES, "query":{"deployment_ids":self.owner["deployment_id"]}, "value":self.response(ONLINE.ENGINE_SUMMARY_KEYS), "status":200}]
        for _ in range(2):
            reads.extend([{"path":ONLINE.ENGINE_OBSERVATIONS, "query":{"deployment_id":self.owner["deployment_id"]}, "value":self.response(), "status":200},
                          {"path":ONLINE.ENGINE_TRENDS, "query":{"deployment_id":self.owner["deployment_id"]}, "value":self.response({"postgresql.transactions.committed_per_second"}, trend=True), "status":200}])
        value = {"schema_version":"addp.node-resources-browser/v1", "run_id":"run", "result":"passed", "stage":"complete", "identity":actor, "negative_identity":denied,
                 "engine_resources":{"reads":reads, "reloaded":True, "zero_and_units":True, "denied":True}}
        with tempfile.TemporaryDirectory() as folder:
            directory = Path(folder)
            for name in ("engine-resources-list.png", "engine-resources-detail.png"): (directory/name).write_bytes(b"\x89PNG\r\n\x1a\n")
            ONLINE.validate_engine_professional_browser(value, wanted, directory)
            for mutate in (lambda v:v.update(run_id="other"), lambda v:v["negative_identity"]["principal"].update(id="other"),
                           lambda v:v["engine_resources"].update(denied=False), lambda v:v["engine_resources"].update(zero_and_units=False),
                           lambda v:v["engine_resources"]["reads"][0].update(status=403), lambda v:v["engine_resources"]["reads"][0]["query"].update(deployment_ids="foreign"),
                           lambda v:v["engine_resources"].update(reads=[item for item in reads if item["path"] != ONLINE.ENGINE_TRENDS])):
                invalid = copy.deepcopy(value); mutate(invalid)
                with self.subTest(mutate=mutate), self.assertRaises(ONLINE.SuiteError): ONLINE.validate_engine_professional_browser(invalid, wanted, directory)
            (directory/"engine-resources-detail.png").unlink()
            with self.assertRaises(ONLINE.SuiteError): ONLINE.validate_engine_professional_browser(value, wanted, directory)


class MetaWorkerAcceptanceTest(unittest.TestCase):
    case = 'meta-workers'
    module = 'meta'

    def setUp(self):
        self.env = patch.dict(ONLINE.os.environ, ADDP_ONLINE_NODE_METRICS_CASE=self.case)
        self.env.start()
        self.addCleanup(self.env.stop)
        self.profile = ONLINE.FIXTURE.WORKER_PROFILES[self.case]

    host_id = "10000000-0000-4000-8000-000000000001"

    def owners(self):
        rows = []
        for i, role in enumerate(["backend"]+["worker"]*(len(self.profile["ports"])-1), 1):
            rows.append({"id": i, "instance_id": self.module+"-instance-"+str(i), "module_name": self.module, "role": role,
                         "status": "up", "process_metrics_declared": True, "process_started_at": "2026-10-11T00:00:00Z",
                         "node_id": self.host_id, "declared_node_id": self.host_id, "node_binding_state": "bound", "node_binding_reason": ""})
        if self.module == 'transfer':
            for i, row in enumerate(rows[1:]):
                row['metadata'] = {'runtime_name': 'sync' if i < 2 else 'continuous_sync'}
        return rows

    def test_binding_requires_two_distinct_same_module_workers_and_current_host_verdict(self):
        good = self.owners()
        for mutate in (None, lambda r: r.pop(), lambda r: r[2].update(instance_id=r[1]["instance_id"]),
                       lambda r: r[2].update(role="scheduler"), lambda r: r[1].update(node_id=""),
                       lambda r: r[1].update(declared_node_id="foreign"), lambda r: r[1].update(process_metrics_declared=False)):
            rows = copy.deepcopy(good)
            if mutate: mutate(rows)
            client = unittest.mock.Mock()
            with patch.object(ONLINE, 'instance_list', return_value=rows):
                if mutate:
                    with self.assertRaises(ONLINE.SuiteError): ONLINE.worker_current_instances(client, self.host_id)
                else:
                    self.assertEqual(ONLINE.worker_current_instances(client, self.host_id), good)
        rows = copy.deepcopy(good)
        for row in rows: row.update(node_id="", node_binding_state="rejected", node_binding_reason="node_disabled")
        with patch.object(ONLINE, 'instance_list', return_value=rows):
            self.assertEqual(ONLINE.worker_current_instances(client, self.host_id, rejected=True), rows)

    def test_slots_use_real_sdk_identity_and_exact_per_unit_endpoints(self):
        owners = self.owners()
        rows = [{"metric": {"instance": "192.0.2.8:"+str(19103+i), "addp_instance_id": owners[i]["instance_id"],
                            "runtime_instance_id": owners[i]["instance_id"], "module_name": self.module, "runtime_role": "worker"},
                 "value": [100, "1"]} for i in range(1, len(owners))]
        for mutate in (None, lambda r: r.pop(), lambda r: r[1]["metric"].update(instance=r[0]["metric"]["instance"]),
                       lambda r: r[0]["metric"].update(runtime_instance_id="foreign"),
                       lambda r: r[0]["metric"].update(instance="192.0.2.9:19104"),
                       lambda r: r[0]["value"].__setitem__(1, "0")):
            data = copy.deepcopy(rows)
            if mutate: mutate(data)
            client = unittest.mock.Mock(query=unittest.mock.Mock(return_value=data))
            with patch.dict(ONLINE.os.environ, ADDP_ONLINE_METRICS_NODE_IP="192.0.2.8"):
                if mutate:
                    with self.assertRaises(ONLINE.SuiteError): ONLINE.worker_slots(client, owners)
                else:
                    self.assertEqual(ONLINE.worker_slots(client, owners), dict(zip([u for u in self.profile['ports'] if u != self.module], owners[1:])))

    def test_lifecycle_is_bounded_owned_and_uses_standard_entrypoints(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            with patch.dict(ONLINE.os.environ, ADDP_ONLINE_NODE_METRICS_CASE=self.case), \
                 patch.object(ONLINE.FIXTURE, 'boundary', return_value=root), patch.object(ONLINE.subprocess, 'run') as run:
                for action, expected in [('stop', ['bash','scripts/dev/stop.sh','-'+self.module]),
                                         ('start', ['bash','scripts/dev/start.sh','-'+self.module,'-monitor']),
                                         ('stop-worker', ['bash','scripts/dev/stop-exact-process.sh','-'+self.profile['replace'][0]])]:
                    ONLINE.worker_lifecycle(root, action, self.host_id, self.profile['replace'][0] if action == 'stop-worker' else None)
                    args, options = run.call_args
                    self.assertEqual(args[0], expected)
                    self.assertEqual(options['env']['ADDP_HOST_NODE_ID'], self.host_id)
                    self.assertEqual(options['timeout'], 300)
                    self.assertTrue(options['check'])
                before = run.call_count
                for action, node in [('unknown', self.host_id), ('start', 'invalid')]:
                    with self.assertRaises(ONLINE.SuiteError): ONLINE.worker_lifecycle(root, action, node)
                with self.assertRaises(ONLINE.SuiteError): ONLINE.worker_lifecycle(root/'foreign', 'start', self.host_id)
                self.assertEqual(run.call_count, before)

    def browser_evidence(self, artifacts):
        workers = self.owners()[1:]
        _, base = MetricsProtocolTest().process_reply()
        batch = copy.deepcopy(base)
        batch['data'] = [copy.deepcopy(base['data'][0]) for _ in workers]
        frames = []
        for row, owner in zip(batch['data'], workers):
            row['subject'] = {'kind':'module_instance', **{k:owner[k] for k in ('id','instance_id','module_name','role')}}
            row['node_id'] = self.host_id
            value = {k: row[k] for k in ('subject','node_id','queried_at','policy_version','lookback_seconds')}
            value.update(start='2026-10-11T00:55:00Z', end='2026-10-11T01:00:00Z', step_seconds=15)
            value['series'] = []
            for series in row['series'][:2]:
                line = copy.deepcopy(series); line['points'] = []
                for i in range(21):
                    at = ONLINE.datetime.datetime(2026,10,11,0,55,tzinfo=ONLINE.datetime.timezone.utc)+ONLINE.datetime.timedelta(seconds=i*15)
                    line['points'].append({'evaluated_at':at.isoformat().replace('+00:00','Z'),
                                           'sampled_at':(at-ONLINE.datetime.timedelta(seconds=5)).isoformat().replace('+00:00','Z'),
                                           'data_state':'valid','value':0.25})
                value['series'].append(line)
            frames.append({'path':'/api/v1/monitor/platform/process_resource_trends',
                           'query':{'instance_id':str(owner['id']), 'start':value['start'],'end':value['end']}, 'value':value})
        actor = identity(); actor['principal']['id'] = 'admin'
        expected = {'run_id':'run','admin_id':'admin','module_workers':{'module':self.module,'workers':workers,'host':{'node_id':self.host_id}}}
        value = {'schema_version':'addp.node-resources-browser/v1','result':'passed','stage':'complete','run_id':'run','identity':actor,
                 'module_workers':{'reload_restored':True,'host_navigation':True,'summary':{'path':ONLINE.PROCESS_SUMMARIES,
                     'query':{'instance_ids':','.join(str(r['id']) for r in workers)},'value':batch},'trends':frames}}
        for name in ('module-workers-list.png','module-workers-trends.png'):
            (artifacts/name).write_bytes(b'\x89PNG\r\n\x1a\n'+b'x'*1100)
        return expected, value

    def test_browser_rejects_incomplete_cross_worker_or_replayed_receipts(self):
        with tempfile.TemporaryDirectory() as folder:
            artifacts = Path(folder)
            expected, good = self.browser_evidence(artifacts)
            ONLINE.validate_workers_browser(good, expected, artifacts)
            mutations = [lambda v:v.update(run_id='old'), lambda v:v['identity']['principal'].update(id='foreign'),
                         lambda v:v['module_workers'].update(host_navigation=False),
                         lambda v:v['module_workers']['summary']['query'].update(instance_ids='2,3,4'),
                         lambda v:v['module_workers']['trends'].pop(),
                         lambda v:v['module_workers']['trends'][0]['value']['subject'].update(id=3),
                         lambda v:v['module_workers']['trends'][1]['query'].update(instance_id='2'),
                         lambda v:v['module_workers']['trends'][0]['value']['series'][0]['points'][0].update(value=None)]
            for mutate in mutations:
                value = copy.deepcopy(good); mutate(value)
                with self.assertRaises(ONLINE.SuiteError): ONLINE.validate_workers_browser(value, expected, artifacts)
            (artifacts/'module-workers-list.png').unlink()
            with self.assertRaises(ONLINE.SuiteError): ONLINE.validate_workers_browser(good, expected, artifacts)


class TransferWorkerAcceptanceTest(MetaWorkerAcceptanceTest):
    case = 'transfer-workers'
    module = 'transfer'

    def test_worker_endpoints_cannot_swap_bounded_and_continuous_identities(self):
        owners = self.owners()
        owners[1]['metadata']['runtime_name'] = 'continuous_sync'
        rows = [{"metric": {"instance": "192.0.2.8:"+str(19103+i), "addp_instance_id": owners[i]["instance_id"],
                            "runtime_instance_id": owners[i]["instance_id"], "module_name": self.module, "runtime_role": "worker"},
                 "value": [100, "1"]} for i in range(1, len(owners))]
        with patch.dict(ONLINE.os.environ, ADDP_ONLINE_METRICS_NODE_IP='192.0.2.8'):
            with self.assertRaises(ONLINE.SuiteError):
                ONLINE.worker_slots(unittest.mock.Mock(query=unittest.mock.Mock(return_value=rows)), owners)

    def test_foreign_family_and_backend_stop_rejected_before_command(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            with patch.object(ONLINE.FIXTURE, 'boundary', return_value=root), patch.object(ONLINE.subprocess, 'run') as run:
                for unit in ('meta-worker-1', 'transfer', 'transfer-bounded-worker-2', None):
                    with self.assertRaises(ONLINE.SuiteError):
                        ONLINE.worker_lifecycle(root, 'stop-worker', self.host_id, unit)
                run.assert_not_called()
                for unit in self.profile['replace']:
                    ONLINE.worker_lifecycle(root, 'stop-worker', self.host_id, unit)
                    self.assertEqual(run.call_args.args[0][-1], '-'+unit)


if __name__ == "__main__":
    unittest.main()
