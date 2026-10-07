"""Real Platform identity, source admission and native Prometheus discovery T4."""
from __future__ import annotations

import base64
import datetime
import hashlib
import hmac
import json
import math
import os
import re
import signal
import ssl
import struct
import subprocess
import sys
import time
import urllib.parse
import urllib.request
from importlib import import_module
from pathlib import Path

API = import_module("scripts.utils.online-api")
FIXTURE = import_module("scripts.test.platform-node-metrics-fixture")
SuiteError = API.SuiteError
NODES = "/api/v1/system/platform/host_nodes"
TARGETS = "/api/v1/monitor/platform/monitoring_targets"
DISCOVERY = "/api/v1/monitor/platform/metrics_discovery"
OBSERVATIONS = "/api/v1/monitor/platform/resource_observations"
TRENDS = "/api/v1/monitor/platform/resource_trends"
QUERY_POLICY = "/api/v1/monitor/settings/resource-query-policy"
METRICS = {"node.cpu.logical_cores": "cores", "node.memory.total_bytes": "bytes",
           "node.memory.available_bytes": "bytes", "node.memory.used_percent": "percent",
           "node.load.average_1m": "load", "node.load.average_5m": "load",
           "node.load.average_15m": "load", "node.uptime_seconds": "seconds"}
REQUIRED = {"platform.host_node.create", "platform.host_node.read", "platform.host_node.update",
            "monitor.monitoring_target.create", "monitor.monitoring_target.read",
            "monitor.monitoring_target.update", "monitor.monitoring_target.delete",
            "monitor.resource_observation.read", "monitor.configuration.read", "monitor.configuration.update"}


def require(ok, message):
    if not ok:
        raise SuiteError(message)


def failure_reason(error):
    if not isinstance(error, SuiteError):
        return "metrics acceptance failed outside a protocol assertion"
    message = str(error)
    for key, value in os.environ.items():
        if any(part in key for part in ("PASSWORD", "TOKEN", "SECRET", "TOTP")) and value:
            message = message.replace(value, "[redacted]")
    return re.sub(r"addp_[a-z]+_[A-Za-z0-9_-]+|\b\d{6}\b", "[redacted]", message)[:2000]


def totp(secret, at):
    key = base64.b32decode(secret + "=" * ((-len(secret)) % 8))
    digest = hmac.new(key, struct.pack(">Q", int(at)//30), hashlib.sha1).digest()
    offset = digest[-1] & 15
    number = struct.unpack(">I", digest[offset:offset+4])[0] & 0x7fffffff
    return f"{number % 1000000:06d}"


def permissions(identity):
    assignments = identity.get("authorization", {}).get("role_assignments")
    require(isinstance(assignments, list), "missing authoritative role assignments")
    result = set()
    for assignment in assignments:
        require(isinstance(assignment, dict) and isinstance(assignment.get("permissions"), list) and all(isinstance(p, str) for p in assignment["permissions"]), "invalid assignment")
        result.update(assignment["permissions"])
    return result


def platform_identity(identity, role):
    require(identity.get("principal", {}).get("type") == "user", "platform token must be a User")
    require(identity.get("context") == {"type": "platform"}, "platform identity contains a Tenant")
    require(identity.get("delegation") is None, "platform identity must not be delegated")
    require(identity.get("token", {}).get("type") == "first_party_access_token", "platform token must come from login")
    require(identity.get("authentication", {}).get("assurance_level") in {"aal2", "aal3"}, "real MFA is required")
    roles = {a["role_key"] for a in identity["authorization"]["role_assignments"]}
    require(roles == {role}, "platform roles are not separated")
    if role == "platform.system_administrator":
        require(REQUIRED <= permissions(identity), "administrator lacks node/target permissions")
    else:
        require(not REQUIRED.intersection(permissions(identity)), "negative platform identity has node/target permissions")


def login(base, prefix):
    client = API.GatewayClient(base, "", 10)
    result = client.request("POST", "/api/v1/system/login", (200,), {
        "username": os.environ[prefix+"_USERNAME"], "password": os.environ[prefix+"_PASSWORD"]}).payload
    require(result.get("next_action") == "verify_mfa" and not result.get("session"), "platform login skipped MFA")
    challenge = result.get("mfa", {})
    require(challenge.get("method") == "totp" and isinstance(challenge.get("challenge_token"), str), "invalid MFA challenge")
    result = client.request("POST", "/api/v1/system/auth/mfa-verifications", (200,), {
        "challenge_token": challenge["challenge_token"], "code": totp(os.environ[prefix+"_TOTP"], time.time())}).payload
    require(result.get("next_action") == "session_issued", "MFA did not issue the Platform session")
    token = result.get("session", {}).get("access_token")
    require(isinstance(token, str) and token.startswith("addp_at_"), "missing Platform access token")
    return API.GatewayClient(base, token, 10)


def service_identity(identity):
    require(identity.get("principal", {}).get("type") == "service_principal", "discovery must use Service Principal")
    require(identity.get("context") == {"type": "platform"} and identity.get("delegation") is None, "discovery scope is not Platform")
    require(identity.get("client", {}).get("client_id") == "addp-prometheus", "discovery Client is not fixed")
    require(identity.get("token", {}).get("type") == "service_access_token", "discovery token is not machine-issued")
    require(permissions(identity) == {"monitor.metrics_discovery.read"}, "Prometheus identity exceeds discovery scope")


def machine_client(base):
    credentials = "addp-prometheus:"+os.environ["PROMETHEUS_SERVICE_CLIENT_SECRET"]
    request = urllib.request.Request(base+"/api/v1/system/oauth/token", method="POST",
        data=urllib.parse.urlencode({"grant_type": "client_credentials", "scope": "addp.api", "context_type": "platform", "audience": "addp.api"}).encode(),
        headers={"Content-Type": "application/x-www-form-urlencoded", "Authorization": "Basic "+base64.b64encode(credentials.encode()).decode()})
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(request, timeout=10) as response:
        result = json.load(response)
    token_type = result.get("token_type")
    require(isinstance(token_type, str) and token_type.lower() == "bearer" and result.get("scope") == "addp.api", "invalid native OAuth response")
    token = result.get("access_token")
    require(isinstance(token, str) and token.startswith("addp_at_"), "missing native OAuth token")
    return API.GatewayClient(base, token, 10)


class Prometheus:
    def __init__(self, directory):
        context = ssl.create_default_context(cafile=str(directory / "center-tls/ca.crt"))
        context.load_cert_chain(str(directory / "center-tls/health.crt"), str(directory / "center-tls/health.key"))
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=context))
        self.base = FIXTURE.center_origin()

    def request(self, path):
        with self.opener.open(self.base+path, timeout=10) as response:
            result = json.load(response)
        require(result.get("status") == "success", "Prometheus query failed")
        return result["data"]

    def query(self, expression):
        return self.request("/api/v1/query?"+urllib.parse.urlencode({"query": expression}))["result"]

    def targets(self, node):
        return [item for item in self.request("/api/v1/targets?state=active")["activeTargets"]
                if item.get("labels", {}).get("addp_node_id") == node]


def eventually(check, message, timeout=100):
    deadline = time.monotonic()+timeout
    while time.monotonic() < deadline:
        if check():
            return
        time.sleep(1)
    raise SuiteError(message+" did not converge")


def owned_action(service, action):
    require(service in {"control-tls"} and action in {"stop", "start"}, "invalid fault action")
    directory = FIXTURE.boundary()
    FIXTURE.assert_owned(directory)
    FIXTURE.compose(directory, action, service)


def source_action(action):
    require(action in {"stop", "start"}, "invalid source action")
    ids = FIXTURE.command(["docker", "ps", "-aq", "--filter", "label=com.docker.compose.project=addp-node-metrics"]).split()
    require(len(ids) == 1, "expected exactly one owned node source")
    container = json.loads(FIXTURE.command(["docker", "inspect", ids[0]]))[0]
    require(container["Config"].get("Labels", {}).get("io.addp.node-metrics.owner") == str(FIXTURE.ROOT), "refusing an unowned node source")
    FIXTURE.command(["docker", action, ids[0]])


def resource_path(node, trend=False, keys=None):
    query = {"node_id": node["node_id"], "metrics": ",".join(METRICS if keys is None else keys)}
    if trend:
        end = int(time.time())
        for field, timestamp in (("start", end-60), ("end", end)):
            query[field] = datetime.datetime.fromtimestamp(timestamp, datetime.timezone.utc).isoformat().replace("+00:00", "Z")
    return (TRENDS if trend else OBSERVATIONS) + "?" + urllib.parse.urlencode(query)


def utc_timestamp(value):
    require(isinstance(value, str) and value.endswith("Z"), "resource time must be UTC")
    try:
        parsed = datetime.datetime.fromisoformat(value[:-1]+"+00:00")
    except ValueError as error:
        raise SuiteError("invalid resource UTC time") from error
    require(parsed.tzinfo is not None and parsed.utcoffset() == datetime.timedelta(0), "resource time lacks UTC offset")
    return parsed.timestamp()


def assert_resources(value, node, target=None, trend=False, keys=None, disconnected=False, range_seconds=60, require_fresh=True):
    expected = METRICS if keys is None else {key: METRICS[key] for key in keys}
    require(value.get("subject") == {"kind": "node", "node_id": node["node_id"]}, "query node identity mismatch")
    require(value.get("node_version") == node["version"], "query node version mismatch")
    require(type(value.get("policy_version")) is int and value["policy_version"] >= 0, "missing query budget version")
    require(value.get("lookback_seconds") == 300, "query lookback mismatch")
    if target is not None:
        require(value.get("target_id") == target["id"] and value.get("target_saved_version") == target["version"], "query target saved version mismatch")
    queried, start, end = (utc_timestamp(value.get(key)) for key in ("queried_at", "start", "end"))
    step = value.get("step_seconds")
    require(start <= end <= queried and type(step) is int, "invalid query evaluation interval")
    require((trend and end-start == range_seconds and step >= 15 and step % 15 == 0) or (not trend and start == end), "invalid query grid")
    count = int((end-start)//step)+1 if trend else 1
    rows = value.get("series")
    require(isinstance(rows, list) and len(rows) == len(expected), "missing or excess resource series")
    seen = set()
    for row in rows:
        key = row.get("metric_key")
        require(key in expected and key not in seen and row.get("unit") == expected[key] and row.get("window_seconds") == 0, "resource catalog mismatch")
        seen.add(key)
        points = row.get("points")
        require(isinstance(points, list) and len(points) == count, "resource grid truncated")
        valid = 0
        for index, point in enumerate(points):
            evaluated = utc_timestamp(point.get("evaluated_at"))
            require(abs(evaluated-(start+index*step)) < 0.001, "misaligned resource point")
            state, sample, number = point.get("data_state"), point.get("sampled_at"), point.get("value")
            require(state in {"valid", "no_data", "stale", "not_connected"}, "invalid data state")
            if disconnected:
                require(state == "not_connected" and sample is None and number is None, "disconnected query reused history")
            elif state in {"valid", "stale"}:
                sampled = utc_timestamp(sample)
                require(type(number) in {int, float} and math.isfinite(number) and number >= 0 and sampled <= evaluated, "invalid resource evidence")
                require(key != "node.memory.used_percent" or number <= 100, "invalid memory percentage")
                age = (evaluated if trend else queried)-sampled
                require((state == "valid" and age <= 60) or (state == "stale" and age > 60), "freshness state lacks evidence")
                valid += int(state == "valid")
            else:
                require(state == "no_data" and sample is None and number is None, "missing evidence became a value")
        if not disconnected and require_fresh:
            require(valid > 0, "metric has no fresh resource evidence")


def resource_query_ready(admin, node, target):
    result = admin.request("GET", resource_path(node), (200,))
    require(result.status == 200, "resource readiness query failed")
    assert_resources(result.payload, node, target, require_fresh=False)
    return all(row["points"][0]["data_state"] == "valid" for row in result.payload["series"])


def check_query_policy(admin, node):
    initial = admin.request("GET", QUERY_POLICY, (200,)).payload
    require(initial.get("pending_restart") is False and initial.get("max_metrics", 0) >= len(METRICS), "invalid initial query policy")
    original = {key: value for key, value in initial.items() if key != "pending_restart"}
    lowered = admin.request("PUT", QUERY_POLICY, (200,), dict(original, max_metrics=1)).payload
    require(lowered["version"] == initial["version"]+1 and lowered.get("pending_restart") is False, "query policy did not commit immediately")
    admin.request("PUT", QUERY_POLICY, (409,), dict(original, max_metrics=1))
    require(admin.request("GET", QUERY_POLICY, (200,)).payload == lowered, "conflicting query policy changed state")
    failure = admin.request("GET", resource_path(node), (422,)).payload
    require(failure.get("error_code") == "observability_query_budget_exceeded", "new query ignored lowered budget")
    key = ["node.memory.total_bytes"]
    limited = admin.request("GET", resource_path(node, keys=key), (200,)).payload
    assert_resources(limited, node, keys=key)
    require(limited["policy_version"] == lowered["version"], "query did not consume saved budget version")
    restored = admin.request("PUT", QUERY_POLICY, (200,), dict(original, version=lowered["version"])).payload
    require(restored["version"] == lowered["version"]+1 and restored.get("pending_restart") is False, "query budget restoration failed")
    resumed = admin.request("GET", resource_path(node), (200,)).payload
    assert_resources(resumed, node)
    require(resumed["policy_version"] == restored["version"], "restored budget not consumed")
    return restored


def center_action(action):
    require(action in {"stop", "start"}, "invalid center fault action")
    FIXTURE.boundary()
    FIXTURE.command(["docker", action, FIXTURE.center_identity()])


def resource_query_after(admin, node, target, after):
    result = admin.request("GET", resource_path(node), (200, 503, 504))
    if result.status != 200:
        return False
    rows = result.payload.get("series", [])
    if len(rows) != len(METRICS) or not all(row.get("points") and row["points"][0].get("data_state") == "valid" and utc_timestamp(row["points"][0].get("sampled_at")) > after for row in rows):
        return False
    assert_resources(result.payload, node, target)
    return True


def assert_discovery(client, target, node_version):
    groups = client.request("GET", DISCOVERY, (200,), response_type=list).payload
    require(len(groups) == 1, "discovery must have exactly this enabled node")
    group = groups[0]
    require(group["targets"] == [os.environ["ADDP_ONLINE_METRICS_NODE_IP"]+":19100"], "discovery endpoint mismatch")
    labels = group["labels"]
    expected = {"addp_node_id": target["subject"]["node_id"], "addp_monitor_kind": "host_resources", "addp_source": "node_exporter",
                "__meta_addp_target_id": target["id"], "__meta_addp_target_version": str(target["version"]),
                "__meta_addp_node_version": str(node_version)}
    require(all(labels.get(k) == v for k, v in expected.items()), "discovery is not bound to current identity/config versions")


def validate_resource_browser(value, expected, artifacts):
    require(value.get("schema_version") == "addp.node-resources-browser/v1" and value.get("result") == "passed"
            and value.get("stage") == "complete" and value.get("run_id") == expected["run_id"], "browser result mismatch")
    for field, principal, role in (("identity", "admin_id", "platform.system_administrator"),
                                    ("negative_identity", "security_id", "platform.security_administrator")):
        actor = value.get(field, {})
        platform_identity(actor, role)
        require(actor["principal"].get("id") == expected[principal], "browser did not use the API phase identity")
    require(value.get("negative_no_business_reads") is True and value.get("navigation") == {
        "list_without_fanout": True, "iframe_preserved": True, "history": True,
        "metric_reload": True, "range_reload": True, "server_window": True}, "browser navigation or denial evidence missing")
    require(value.get("auto_refresh") == {"natural_timer": True, "server_end_advanced": True,
            "unchanged_url": True, "off_restored": True, "off_no_requests": True}, "browser automatic refresh evidence missing")
    rows = value.get("resources")
    require(isinstance(rows, list) and 4 <= len(rows) <= 30, "browser resource evidence missing or unbounded")
    instants, trends = [], []
    for row in rows:
        query, resource = row.get("query", {}), row.get("value", {})
        require(query.get("node_id") == expected["node"]["node_id"], "browser requested another node")
        trend = row.get("path") == TRENDS
        require(trend or row.get("path") == OBSERVATIONS, "browser resource path mismatch")
        keys = query.get("metrics", "").split(",")
        require((trend and keys in [["node.memory.used_percent"], ["node.load.average_1m"]])
                or (not trend and len(keys) == len(METRICS) and set(keys) == set(METRICS)), "browser metric request mismatch")
        duration = utc_timestamp(query.get("end")) - utc_timestamp(query.get("start")) if trend else 0
        require(not trend or duration in {300, 3600}, "browser range mismatch")
        assert_resources(resource, expected["node"], expected["target"], trend, keys, range_seconds=duration)
        require(resource["policy_version"] == expected["policy_version"], "browser ignored current query budget")
        if trend:
            end = utc_timestamp(query.get("end"))
            require(end == utc_timestamp(resource["end"])
                    and any(utc_timestamp(item["end"]) == end for item in instants), "browser trend lacks server anchor")
            trends.append((keys[0], duration))
        else:
            instants.append(resource)
    require(len(instants) >= 2 and {("node.memory.used_percent", 3600), ("node.load.average_1m", 300)} <= set(trends), "browser restore evidence incomplete")
    for name in ("list", "detail", "restored"):
        screenshot = artifacts / ("node-resources-" + name + ".png")
        require(screenshot.is_file() and screenshot.stat().st_size > 1000
                and screenshot.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n", "browser screenshot missing or invalid")
    return {"result": "passed", "password_mfa": True, "same_platform_identity": True,
            "eight_metrics": True, "trend_server_window": True, "navigation_restore": True,
            "security_administrator_denied": True, "auto_refresh": True}


def run_resource_browser(node, target, policy, display_name, admin, security):
    artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
    expected = {"run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"], "node": node, "target": {"id": target["id"], "version": target["version"]},
                "display_name": display_name, "policy_version": policy["version"],
                "admin_id": admin["principal"]["id"], "security_id": security["principal"]["id"]}
    # Store only the subject/version facts needed by the page, never target TLS/credentials.
    expected["node"] = {"node_id": node["node_id"], "version": node["version"]}
    (artifacts / "node-resources-browser-input.json").write_text(json.dumps(expected))
    evidence = artifacts / "node-resources-browser.json"
    evidence.unlink(missing_ok=True)
    for name in ("list", "detail", "restored"):
        (artifacts / ("node-resources-" + name + ".png")).unlink(missing_ok=True)
    result = subprocess.run(["npm", "run", "test:e2e", "--", "--config=playwright.online.config.js",
                             "e2e/online/platform-node-resources.spec.js",
                             "--output=" + str(Path(os.environ["ADDP_ONLINE_SECRET_DIR"]) / "node-resource-browser-output")], cwd=FIXTURE.ROOT / "console/frontend",
                            env=dict(os.environ), capture_output=True, text=True, timeout=240)
    # Playwright may print assertions from login; never forward credential-bearing output.
    require(result.returncode == 0, "resource browser failed; inspect sanitized stage report")
    require(evidence.is_file(), "resource browser report missing")
    return validate_resource_browser(json.loads(evidence.read_text()), expected, artifacts)


def run(base, directory, report):
    report["stage"] = "platform-login-mfa"
    admin = login(base, "ADDP_ONLINE_METRICS_ADMIN")
    security = login(base, "ADDP_ONLINE_METRICS_SECURITY")
    admin_identity = admin.request("GET", "/api/v1/system/auth/context", (200,)).payload
    platform_identity(admin_identity, "platform.system_administrator")
    security_identity = security.request("GET", "/api/v1/system/auth/context", (200,)).payload
    platform_identity(security_identity, "platform.security_administrator")
    report["stage"] = "collector-identity"
    machine = machine_client(base)
    service_identity(machine.request("GET", "/api/v1/system/auth/context", (200,)).payload)
    report["stage"] = "tenant-platform-isolation"
    primary, foreign = int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"]), int(os.environ["ADDP_ONLINE_METRICS_FOREIGN_TENANT_ID"])
    require(primary > 1 and foreign > 1 and primary != foreign, "distinct nondefault Tenant identities required")
    tenant_clients = []
    for tenant, key in ((primary, "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"), (foreign, "ADDP_ONLINE_FOREIGN_USER_ACCESS_TOKEN")):
        client = API.GatewayClient(base, os.environ[key], 10)
        API.validate_user_identity(client, tenant, {"monitor.execution.read"})
        tenant_clients.append(client)
    for client in [security, machine, *tenant_clients, API.GatewayClient(base, "", 10)]:
        expected = (401,) if not client.token else (403,)
        client.request("GET", NODES, expected)
        client.request("GET", TARGETS, expected)
    for client in [admin, security, *tenant_clients]:
        client.request("GET", DISCOVERY, (403,))
    require(machine.request("GET", DISCOVERY, (200,), response_type=list).payload == [], "initial discovery is not empty")
    report["identity_isolation"] = True
    report["stage"] = "node-target-cas-and-discovery"
    node_input = {"display_name": "Online metrics "+os.environ["ADDP_ONLINE_TEST_RUN_ID"], "node_kind": "virtual",
                  "addresses": [os.environ["ADDP_ONLINE_METRICS_NODE_IP"]], "enabled": True, "allowed_module_bindings": []}
    node = admin.request("POST", NODES, (201,), node_input).payload
    require(node.get("version") == 1 and isinstance(node.get("node_id"), str), "invalid node create receipt")
    report["node_id"] = node["node_id"]
    body = {"subject": {"kind": "node", "node_id": node["node_id"]}, "monitor_kind": "host_resources",
            "source": {"type": "node_exporter", "endpoint": "https://"+os.environ["ADDP_ONLINE_METRICS_NODE_IP"]+":19100/metrics"}, "enabled": True}
    target = admin.request("POST", TARGETS, (201,), body).payload
    require(target.get("version") == 1, "invalid target create receipt")
    report["target_id"], report["saved_version"] = target["id"], target["version"]
    target_path = TARGETS+"/"+target["id"]
    target = admin.request("PUT", target_path, (200,), dict(body, version=target["version"])).payload
    require(target["version"] == 2, "target CAS did not advance")
    report["saved_version"] = target["version"]
    admin.request("PUT", target_path, (409,), dict(body, version=1))
    node_path = NODES+"/"+node["node_id"]
    for client in [security, machine, *tenant_clients]:
        client.request("GET", node_path, (403,))
        client.request("POST", NODES, (403,), node_input)
        client.request("PUT", node_path, (403,), dict(node_input, version=node["version"]))
        client.request("GET", target_path, (403,))
        client.request("POST", TARGETS, (403,), body)
        client.request("PUT", target_path, (403,), dict(body, version=target["version"]))
        client.request("DELETE", target_path, (403,), {"version": target["version"]})
    require(admin.request("GET", node_path, (200,)).payload["version"] == node["version"], "denied node requests changed state")
    require(admin.request("GET", target_path, (200,)).payload["version"] == target["version"], "denied target requests changed state")
    query_policy = admin.request("GET", QUERY_POLICY, (200,)).payload
    for client in [security, machine, *tenant_clients, API.GatewayClient(base, "", 10)]:
        expected = (401,) if not client.token else (403,)
        for trend in (False, True):
            client.request("GET", resource_path(node, trend), expected)
        client.request("GET", QUERY_POLICY, expected)
        client.request("PUT", QUERY_POLICY, expected, {key: value for key, value in query_policy.items() if key != "pending_restart"})
    require(admin.request("GET", QUERY_POLICY, (200,)).payload == query_policy, "denied budget writes changed state")
    report["query_identity_isolation"] = True
    assert_discovery(machine, target, node["version"])
    report["stage"] = "native-discovery-and-resource-samples"
    prom = Prometheus(directory)
    expression = '{job="addp_nodes",addp_node_id="'+node["node_id"]+'"}'
    def active():
        items = prom.targets(node["node_id"])
        expected = {"__meta_addp_target_id": target["id"], "__meta_addp_target_version": str(target["version"]),
                    "__meta_addp_node_version": str(node["version"])}
        return len(items) == 1 and items[0].get("health") == "up" and all(
            items[0].get("discoveredLabels", {}).get(key) == value for key, value in expected.items())
    eventually(active, "current saved target version and successful scrape")
    since = time.time()
    report["sampling_observed_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(since))
    report["native_sample_times"] = {}
    native_metrics = ("node_cpu_seconds_total", "node_memory_MemTotal_bytes", "node_load1", "node_disk_reads_completed_total", "node_network_receive_bytes_total", "node_boot_time_seconds")
    for metric in native_metrics:
        rows = prom.query(metric+expression)
        require(rows and all(math.isfinite(float(row["value"][1])) for row in rows), "missing or invalid resource metric "+metric)
        require(all(row["metric"].get("addp_source") == "node_exporter" and row["metric"].get("addp_monitor_kind") == "host_resources" for row in rows), "resource identity labels mismatch")
    report["applied_version"], report["resource_metrics"] = 2, True
    report["stage"] = "platform-resource-query-and-budget"
    for trend in (False, True):
        value = admin.request("GET", resource_path(node, trend), (200,)).payload
        report["resource_check_mode"] = "trend" if trend else "instant"
        (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "node-resource-query-check.json").write_text(json.dumps(value))
        if not trend:
            for metric in native_metrics:
                timestamps = prom.query("timestamp("+metric+expression+")")
                require(timestamps, "missing native sample timestamp")
                report["native_sample_times"][metric] = sorted({float(row["value"][1]) for row in timestamps})
            (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "node-resource-query-initial.json").write_text(json.dumps(value))
            assert_resources(value, node, target, require_fresh=False)
            if not all(row["points"][0]["data_state"] == "valid" for row in value["series"]):
                report["initial_resource_query_wait"] = True
                eventually(lambda: resource_query_ready(admin, node, target), "eight current resource metrics after initial scrape")
                value = admin.request("GET", resource_path(node), (200,)).payload
                (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "node-resource-query-check.json").write_text(json.dumps(value))
        assert_resources(value, node, target, trend)
    query_policy = check_query_policy(admin, node)
    report.update(resource_query=True, query_budget_cas_hot_read=True, query_policy_version=query_policy["version"])
    report["stage"] = "console-resource-browser"
    report["resource_browser"] = run_resource_browser(node, target, query_policy, node_input["display_name"], admin_identity, security_identity)
    report["stage"] = "center-query-outage-recovery"
    center_action("stop")
    try:
        failure = admin.request("GET", resource_path(node), (503, 504)).payload
        require(failure.get("error_code") in {"observability_backend_unavailable", "observability_query_timeout"}, "center failure became query success")
        API.GatewayClient(os.environ["MONITOR_URL"], "", 10).request("GET", "/health/ready", (200,))
        for client in tenant_clients:
            client.request("GET", "/api/v1/monitor/executions", (200,))
    finally:
        recovery = time.time()
        center_action("start")
    eventually(lambda: resource_query_after(admin, node, target, recovery), "new API query evidence after center recovery")
    report["center_query_outage_recovery"] = True
    report["stage"] = "source-outage-recovery"
    source_action("stop")
    try:
        eventually(lambda: bool(prom.query("up"+expression+" == 0")), "source outage")
        require(prom.query('up{job="prometheus"} == 1'), "center failed with source")
    finally:
        source_action("start")
    eventually(lambda: bool(prom.query("up"+expression+" == 1")), "source recovery")
    report["stage"] = "control-outage-recovery"
    baseline = sum(float(row["value"][1]) for row in prom.query("prometheus_sd_http_failures_total"))
    owned_action("control-tls", "stop")
    try:
        eventually(lambda: sum(float(row["value"][1]) for row in prom.query("prometheus_sd_http_failures_total")) > baseline, "real control transport failure")
        require(active(), "failed discovery unexpectedly removed the last successful target")
        require(prom.query("up"+expression+" == 1"), "existing source scrape stopped with control plane")
    finally:
        owned_action("control-tls", "start")
    report["stage"] = "node-disable-and-resume"
    node = admin.request("PUT", NODES+"/"+node["node_id"], (200,), dict(node_input, enabled=False, version=node["version"])).payload
    require(machine.request("GET", DISCOVERY, (200,), response_type=list).payload == [], "disabled node remains in current discovery")
    for trend in (False, True):
        assert_resources(admin.request("GET", resource_path(node, trend), (200,)).payload, node, trend=trend, disconnected=True)
    eventually(lambda: not prom.targets(node["node_id"]), "disabled node removal after control recovery")
    resumed_at = time.time()
    node = admin.request("PUT", NODES+"/"+node["node_id"], (200,), dict(node_input, version=node["version"])).payload
    assert_discovery(machine, target, node["version"])
    eventually(active, "re-enabled node sampling")
    # Require this activation's samples and current versions through the user API.
    eventually(lambda: resource_query_after(admin, node, target, resumed_at), "new API resource evidence after node re-enablement")
    report["node_query_resume_fresh_samples"] = True
    report["stage"] = "target-disable-and-delete"
    target = admin.request("PUT", target_path, (200,), dict(body, enabled=False, version=target["version"])).payload
    require(machine.request("GET", DISCOVERY, (200,), response_type=list).payload == [], "disabled target remains in discovery")
    for trend in (False, True):
        assert_resources(admin.request("GET", resource_path(node, trend), (200,)).payload, node, trend=trend, disconnected=True)
    report["disabled_query_does_not_reuse_history"] = True
    eventually(lambda: not prom.targets(node["node_id"]), "target disable application")
    admin.request("DELETE", target_path, (409,), {"version": 2})
    admin.request("DELETE", target_path, (204,), {"version": target["version"]})
    admin.request("GET", target_path, (404,))
    admin.request("PUT", NODES+"/"+node["node_id"], (200,), dict(node_input, enabled=False, version=node["version"]))
    report.update(source_outage_recovery=True, control_outage_recovery=True, node_and_target_removal=True,
                  cleanup="awaiting-hosted-deployment-destruction", result="passed", stage="complete")


def main():
    report = {"schema_version": "addp.platform-node-metrics-online/v1", "result": "failed"}
    evidence = None
    def interrupted(signum, frame):
        raise SuiteError("metrics suite interrupted")
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        directory = FIXTURE.boundary()
        require(os.environ.get("ADDP_ONLINE_TEST") == "1", "use make test-online")
        evidence = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "platform-node-metrics.json"
        run(os.environ["GATEWAY_URL"], directory, report)
    except (OSError, ValueError, KeyError, SuiteError, subprocess.SubprocessError) as error:
        report["failure_reason"] = failure_reason(error)
        report["result"] = "failed"
        print("Platform node metrics Online acceptance failed; credentials are omitted", file=sys.stderr)
    finally:
        if evidence is not None:
            evidence.write_text(json.dumps(report, sort_keys=True)+"\n")
    if report["result"] != "passed":
        return 1
    print(json.dumps(report, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
