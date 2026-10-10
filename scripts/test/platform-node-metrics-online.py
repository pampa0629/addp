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
PREFLIGHT = import_module("scripts.test.online-preflight")
SuiteError = API.SuiteError
NODES = "/api/v1/system/platform/host_nodes"
TARGETS = "/api/v1/monitor/platform/monitoring_targets"
DISCOVERY = "/api/v1/monitor/platform/metrics_discovery"
OBSERVATIONS = "/api/v1/monitor/platform/resource_observations"
SUMMARIES = "/api/v1/monitor/platform/resource_summaries"
TRENDS = "/api/v1/monitor/platform/resource_trends"
QUERY_POLICY = "/api/v1/monitor/settings/resource-query-policy"
INSTANCES = "/api/v1/system/platform/module-instances"
PROCESS_SUMMARIES = "/api/v1/monitor/platform/process_resource_summaries"
PROCESS_METRICS = {"process.cpu.core_equivalents": ("cores", 60),
                   "process.memory.resident_bytes": ("bytes", 0), "process.uptime_seconds": ("seconds", 0)}
METRICS = {"node.cpu.logical_cores": "cores", "node.cpu.busy_percent": "percent", "node.memory.total_bytes": "bytes",
           "node.memory.available_bytes": "bytes", "node.memory.used_percent": "percent",
           "node.load.average_1m": "load", "node.load.average_5m": "load",
           "node.load.average_15m": "load", "node.uptime_seconds": "seconds"}
FILESYSTEM_METRICS = {"node.filesystem."+key: unit for key, unit in (("total_bytes", "bytes"), ("free_bytes", "bytes"), ("available_bytes", "bytes"), ("used_bytes", "bytes"), ("used_percent", "percent"))}
INODE_METRICS = {"node.filesystem."+key: unit for key, unit in (("inodes_total", "inodes"), ("inodes_free", "inodes"), ("inodes_used", "inodes"), ("inodes_used_percent", "percent"))}
DISK_METRICS = {"node.disk."+key: unit for key, unit in (("read_bytes_per_second", "bytes_per_second"), ("write_bytes_per_second", "bytes_per_second"), ("io_busy_percent", "percent"), ("read_mean_duration_milliseconds", "milliseconds"), ("write_mean_duration_milliseconds", "milliseconds"))}
NETWORK_METRICS = {"node.network."+key: "bytes_per_second" for key in ("receive_bytes_per_second", "transmit_bytes_per_second")}
DEVICE_RATE_FAMILIES = (DISK_METRICS, NETWORK_METRICS)
ALL_METRICS = dict(METRICS, **FILESYSTEM_METRICS, **INODE_METRICS, **DISK_METRICS, **NETWORK_METRICS)
REQUIRED = {"platform.host_node.create", "platform.host_node.read", "platform.host_node.update",
            "monitor.monitoring_target.create", "monitor.monitoring_target.read",
            "monitor.monitoring_target.update", "monitor.monitoring_target.delete",
            "monitor.resource_observation.read", "monitor.configuration.read", "monitor.configuration.update", "platform.module.read"}


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


def machine_token(base):
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
    lifetime = result.get("expires_in")
    require(type(lifetime) is int and lifetime > 0, "invalid native OAuth token lifetime")
    return token, lifetime


class CollectorClient(API.GatewayClient):
    """Keep this suite's fixed collector identity current through long fault phases."""
    def __init__(self, base):
        super().__init__(base, "", 10)
        self.principal_id = None
        self.grant_count = 0
        self.renew_at = 0
        self.renew()

    def renew(self):
        started = time.monotonic()
        # Never send the previous credential if grant or identity verification fails.
        self.token = ""
        token, lifetime = machine_token(self.base_url)
        candidate = API.GatewayClient(self.base_url, token, self.timeout)
        identity = candidate.request("GET", "/api/v1/system/auth/context", (200,)).payload
        service_identity(identity)
        principal_id = identity["principal"].get("id")
        require(isinstance(principal_id, str) and principal_id, "missing collector principal identity")
        require(self.principal_id in {None, principal_id}, "collector principal changed during acceptance")
        self.principal_id = principal_id
        self.renew_at = started + lifetime - min(self.timeout, lifetime / 2)
        require(time.monotonic() < self.renew_at, "collector grant exhausted before identity verification")
        self.token = token
        self.grant_count += 1

    def request(self, *args, **kwargs):
        if time.monotonic() >= self.renew_at:
            self.renew()
        return super().request(*args, **kwargs)


def machine_client(base):
    return CollectorClient(base)


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


def resource_path(node, trend=False, keys=None, dimensions=None):
    query = {"node_id": node["node_id"], "metrics": ",".join(METRICS if keys is None else keys)}
    if dimensions:
        query.update(dimensions)
    if trend:
        end = int(time.time())
        for field, timestamp in (("start", end-60), ("end", end)):
            query[field] = datetime.datetime.fromtimestamp(timestamp, datetime.timezone.utc).isoformat().replace("+00:00", "Z")
    return (TRENDS if trend else OBSERVATIONS) + "?" + urllib.parse.urlencode(query)


def instance_timestamp(value):
    require(isinstance(value, str) and re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})", value) is not None,
            "instance time must be ISO 8601 with timezone")
    try:
        parsed = datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise SuiteError("invalid instance time") from error
    return parsed.timestamp()


def utc_timestamp(value):
    require(isinstance(value, str) and value.endswith("Z"), "resource time must be UTC")
    return instance_timestamp(value)


def assert_resources(value, node, target=None, trend=False, keys=None, disconnected=False, range_seconds=60, require_fresh=True, dimensions=None):
    expected = METRICS if keys is None else {key: ALL_METRICS[key] for key in keys}
    require(value.get("subject") == {"kind": "node", "node_id": node["node_id"]}, "query node identity mismatch")
    require(value.get("node_version") == node["version"], "query node version mismatch")
    require(type(value.get("policy_version")) is int and value["policy_version"] >= 0, "missing query budget version")
    require(value.get("lookback_seconds") == 300, "query lookback mismatch")
    if target is not None:
        require(value.get("target_id") == target["id"] and value.get("target_saved_version") == target["version"], "query target saved version mismatch")
    queried, start, end = (utc_timestamp(value.get(key)) for key in ("queried_at", "start", "end"))
    if not trend:
        collection = value.get("collection")
        require(isinstance(collection, dict) and collection.get("state") in {"not_connected", "no_sample", "collecting", "failed", "stale"}, "invalid collection state")
        state = collection["state"]
        for family in ("filesystem", "network"):
            require(collection.get(family) in {"unknown", "available", "failed", "not_collected"}, "invalid collector coverage")
            require(state == "collecting" or collection[family] == "unknown", "failed collection retained collector coverage")
        if state in {"collecting", "failed", "stale"}:
            sampled = utc_timestamp(collection.get("sampled_at"))
            require(sampled <= queried and ((queried-sampled > 60) if state == "stale" else (queried-sampled <= 60)), "invalid collection freshness")
        else:
            require(collection.get("sampled_at") is None, "unobserved collection contains sample")
    step = value.get("step_seconds")
    require(start <= end <= queried and type(step) is int, "invalid query evaluation interval")
    require((trend and end-start == range_seconds and step >= 15 and step % 15 == 0) or (not trend and start == end), "invalid query grid")
    count = int((end-start)//step)+1 if trend else 1
    rows = value.get("series")
    require(isinstance(rows, list) and len(expected) <= len(rows) <= 100 and len(rows)*count <= 20000, "missing or excess resource series")
    seen = set()
    for row in rows:
        key = row.get("metric_key")
        require(key in expected and row.get("unit") == expected[key] and row.get("window_seconds") == (60 if key == "node.cpu.busy_percent" or key in DISK_METRICS or key in NETWORK_METRICS else 0), "resource catalog mismatch")
        dims = row.get("dimensions")
        filesystem = key.startswith("node.filesystem.")
        device_rate = key in DISK_METRICS or key in NETWORK_METRICS
        require(type(dims) is dict, "missing resource dimensions")
        require(all(type(v) is str and 0 < len(v) <= 4096 and "\0" not in v for v in dims.values()), "invalid dimension value")
        require((not dims) or (filesystem and set(dims) == {"device", "mountpoint", "fstype"} and dims["mountpoint"].startswith("/")) or (device_rate and set(dims) == {"device"}), "invalid resource dimensions")
        require(not dimensions or dims == dimensions, "query device or mount mismatch")
        require(filesystem or device_rate or not dims, "scalar dimensions leaked")
        identity = (key, tuple(sorted(dims.items())))
        require(identity not in seen, "duplicate resource dimensions")
        seen.add(identity)
        points = row.get("points")
        require(isinstance(points, list) and len(points) == count, "resource grid truncated")
        valid = 0
        for index, point in enumerate(points):
            evaluated = utc_timestamp(point.get("evaluated_at"))
            require(abs(evaluated-(start+index*step)) < 0.001, "misaligned resource point")
            state, sample, number = point.get("data_state"), point.get("sampled_at"), point.get("value")
            require(state in {"valid", "no_data", "stale", "not_connected"}, "invalid data state")
            require(not ((filesystem or device_rate) and not dims) or state in {"no_data", "not_connected"}, "unknown mount became valid")
            if disconnected:
                require(state == "not_connected" and sample is None and number is None, "disconnected query reused history")
            elif state in {"valid", "stale"}:
                sampled = utc_timestamp(sample)
                require(type(number) in {int, float} and math.isfinite(number) and number >= 0 and sampled <= evaluated, "invalid resource evidence")
                require(expected[key] != "percent" or number <= 100, "invalid resource percentage")
                require(expected[key] != "inodes" or (number <= 2**53-1 and number == math.floor(number)), "invalid inode integer")
                age = (evaluated if trend else queried)-sampled
                require((state == "valid" and age <= 60) or (state == "stale" and age > 60), "freshness state lacks evidence")
                valid += int(state == "valid")
            else:
                require(state == "no_data" and sample is None and number is None, "missing evidence became a value")
        if not disconnected and require_fresh:
            require(valid > 0 or expected[key] == "milliseconds", "metric has no fresh resource evidence")

    groups = [{identity for metric, identity in seen if metric == key} for key in expected]
    require(all(groups), "missing resource catalog key")
    filesystem_groups = [group for key, group in zip(expected, groups) if key.startswith("node.filesystem.")]
    require(not filesystem_groups or all(group == filesystem_groups[0] for group in filesystem_groups), "filesystem group catalog mismatch")

    for family in DEVICE_RATE_FAMILIES:
        device_groups = [group for key, group in zip(expected, groups) if key in family]
        require(not device_groups or all(group == device_groups[0] for group in device_groups), "device rate group catalog mismatch")

    if set(FILESYSTEM_METRICS) <= set(expected):
        for group in filesystem_groups[0]:
            mounted = {row["metric_key"]: row for row in rows if tuple(sorted(row["dimensions"].items())) == group}
            for index in range(count):
                points = {key: mounted["node.filesystem."+key]["points"][index] for key in ("total_bytes", "free_bytes", "available_bytes", "used_bytes", "used_percent")}
                gauges = [points[key] for key in ("total_bytes", "free_bytes", "available_bytes", "used_bytes")]
                if not all(point["data_state"] == "valid" for point in gauges):
                    continue
                require(len({point["sampled_at"] for point in gauges}) == 1, "filesystem capacity spans scrapes")
                total, free, available, used = (points[key]["value"] for key in ("total_bytes", "free_bytes", "available_bytes", "used_bytes"))
                require(0 <= available <= free <= total and math.isclose(used, total-free, rel_tol=1e-9, abs_tol=1e-6), "filesystem byte formula mismatch")
                percentage = points["used_percent"]
                if used+available == 0:
                    require(percentage["data_state"] == "no_data", "zero denominator became a percentage")
                else:
                    require(percentage["data_state"] == "valid" and percentage["sampled_at"] == gauges[0]["sampled_at"] and math.isclose(percentage["value"], 100*used/(used+available), rel_tol=1e-9, abs_tol=1e-8), "filesystem non-root percentage mismatch")


    if set(INODE_METRICS) <= set(expected):
        for group in filesystem_groups[0]:
            mounted = {row["metric_key"]: row for row in rows if tuple(sorted(row["dimensions"].items())) == group}
            for index in range(count):
                points = [mounted[key]["points"][index] for key in INODE_METRICS]
                require(len({point["data_state"] for point in points}) == 1 and len({point["sampled_at"] for point in points}) == 1, "inode capacity spans scrapes or states")
                if points[0]["data_state"] in {"valid", "stale"}:
                    total, free, used, percentage = (point["value"] for point in points)
                    require(total > 0 and free <= total and used == total-free and math.isclose(percentage, 100*used/total, rel_tol=1e-9, abs_tol=1e-8), "inode capacity formula mismatch")


def fresh_resource_groups(value, keys, after=0):
    """Called only after strict resource validation; unsupported or incomplete groups are empty."""
    groups = {}
    for row in value["series"]:
        groups.setdefault(tuple(sorted(row["dimensions"].items())), {})[row["metric_key"]] = row["points"][0]
    return [dict(dimensions) for dimensions, points in groups.items() if dimensions and set(points) == set(keys)
            and all((point["data_state"] == "valid" and utc_timestamp(point["sampled_at"]) > after) or (ALL_METRICS[key] == "milliseconds" and point["data_state"] == "no_data") for key, point in points.items())]


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
    filesystem_failure = admin.request("GET", resource_path(node, keys=FILESYSTEM_METRICS), (422,)).payload
    require(filesystem_failure.get("error_code") == "observability_query_budget_exceeded", "filesystem query ignored hot metric budget")
    inode_failure = admin.request("GET", resource_path(node, keys=INODE_METRICS), (422,)).payload
    require(inode_failure.get("error_code") == "observability_query_budget_exceeded", "inode query ignored hot metric budget")
    disk_failure = admin.request("GET", resource_path(node, keys=DISK_METRICS), (422,)).payload
    require(disk_failure.get("error_code") == "observability_query_budget_exceeded", "disk query ignored hot metric budget")
    network_failure = admin.request("GET", resource_path(node, keys=NETWORK_METRICS), (422,)).payload
    require(network_failure.get("error_code") == "observability_query_budget_exceeded", "network query ignored hot metric budget")
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


def resource_query_after(admin, node, target, after, keys=None):
    expected = METRICS if keys is None else keys
    result = admin.request("GET", resource_path(node, keys=keys), (200, 503, 504))
    if result.status != 200:
        return False
    if keys in (INODE_METRICS, *DEVICE_RATE_FAMILIES):
        assert_resources(result.payload, node, target, keys=keys, require_fresh=False)
        return bool(fresh_resource_groups(result.payload, keys, after))
    rows = result.payload.get("series", [])
    if len(rows) < len(expected) or not all(row.get("points") and row["points"][0].get("data_state") == "valid" and utc_timestamp(row["points"][0].get("sampled_at")) > after for row in rows):
        return False
    assert_resources(result.payload, node, target, keys=keys)
    return True


def assert_discovery(client, target, node_version, processes):
    groups = client.request("GET", DISCOVERY, (200,), response_type=list).payload
    assert_process_discovery(groups, processes, host_count=1)
    hosts = [g for g in groups if g["labels"].get("addp_monitor_kind") == "host_resources"]
    require(len(hosts) == 1, "exactly one enabled host source required")
    group = hosts[0]
    require(group["targets"] == [os.environ["ADDP_ONLINE_METRICS_NODE_IP"]+":19100"], "discovery endpoint mismatch")
    labels = group["labels"]
    expected = {"addp_node_id": target["subject"]["node_id"], "addp_monitor_kind": "host_resources", "addp_source": "node_exporter",
                "__meta_addp_target_id": target["id"], "__meta_addp_target_version": str(target["version"]),
                "__meta_addp_node_version": str(node_version)}
    require(all(labels.get(k) == v for k, v in expected.items()), "discovery is not bound to current identity/config versions")


def instance_list(client, **query):
    value = client.request("GET", INSTANCES+"?"+urllib.parse.urlencode(dict(page=1, page_size=20, **query)), (200,)).payload
    rows = value.get("data")
    require(isinstance(rows, list) and len(rows) <= 20 and value.get("total") == len(rows)
            and value.get("page") == 1 and value.get("page_size") == 20, "instance list incomplete or unbounded")
    seen = set()
    for row in rows:
        require(type(row.get("id")) is int and row["id"] > 0 and row["id"] not in seen
                and isinstance(row.get("instance_id"), str) and row["instance_id"]
                and type(row.get("process_metrics_declared")) is bool
                and row.get("status") in {"up", "down"} and row.get("role") in {"backend", "worker", "scheduler", "ingress"}, "invalid runtime instance")
        require(not {"process_metrics", "endpoint", "tls_dir"}.intersection(row), "private process source leaked to User")
        seen.add(row["id"])
    return rows


def current_processes(admin):
    rows = [row for module in FIXTURE.PROCESS_PORTS for row in instance_list(admin, module_name=module, role="backend", status="up")]
    require(len(rows) == 2 and {r["module_name"] for r in rows} == set(FIXTURE.PROCESS_PORTS), "expected exactly two current Go processes")
    require(all(row["process_metrics_declared"] and row.get("node_id") == "" for row in rows), "process declaration or unbound identity missing")
    selected = instance_list(admin, ids=",".join(str(r["id"]) for r in rows))
    require({(r["id"], r["instance_id"]) for r in selected} == {(r["id"], r["instance_id"]) for r in rows}, "exact instance filter expanded scope")
    return rows


def assert_process_discovery(groups, processes, host_count=0):
    require(isinstance(groups, list) and len(groups) == len(processes)+host_count, "discovery contains missing or extra sources")
    actual = [g for g in groups if g.get("labels", {}).get("addp_monitor_kind") == "process_resources"]
    require(len(actual) == len(processes), "automatic process discovery missing")
    for row in processes:
        owned = [g for g in actual if g["labels"].get("addp_instance_id") == row["instance_id"]]
        require(len(owned) == 1, "process discovery identity duplicated or absent")
        group = owned[0]
        require(group["labels"] == {"__scheme__": "https", "__metrics_path__": "/metrics", "addp_module_name": row["module_name"],
                "addp_instance_id": row["instance_id"], "addp_runtime_role": row["role"], "addp_monitor_kind": "process_resources", "addp_source": "application"}
                and group.get("targets") == [os.environ["ADDP_ONLINE_METRICS_NODE_IP"]+":"+str(FIXTURE.PROCESS_PORTS[row["module_name"]])], "process discovery is not bound to formal instance/endpoint")


def process_path(instances):
    return PROCESS_SUMMARIES+"?"+urllib.parse.urlencode({"instance_ids": ",".join(str(r["id"]) for r in instances)})


def assert_process_summaries(value, instances, *, fresh=True, after=0, inactive=(), disconnected=()):
    rows = value.get("data")
    require(isinstance(rows, list) and len(rows) == len(instances), "process summary batch incomplete")
    owners = {r["id"]: r for r in instances}
    seen, anchors = set(), set()
    for row in rows:
        subject = row.get("subject", {})
        owner = owners.get(subject.get("id"))
        require(owner is not None and subject["id"] not in seen and subject == {"kind": "module_instance", "id": owner["id"],
                "module_name": owner["module_name"], "instance_id": owner["instance_id"], "role": owner["role"]}
                and row.get("node_id") == owner.get("node_id", ""), "process summary borrowed another identity")
        seen.add(subject["id"])
        queried = utc_timestamp(row.get("queried_at"))
        require(type(row.get("policy_version")) is int and row["policy_version"] >= 0 and row.get("lookback_seconds") == 300, "process policy missing")
        anchors.add((row["queried_at"], row["policy_version"]))
        collection = row.get("collection", {})
        absent = "not_active" if owner["id"] in inactive else "not_connected" if owner["id"] in disconnected else None
        require(collection.get("state") == absent if absent else collection.get("state") in {"collecting", "failed", "stale", "identity_mismatch", "no_sample"}, "process collection state invalid")
        if absent:
            require(collection.get("sampled_at") is None, "inactive process borrowed collection evidence")
        elif collection.get("state") == "no_sample":
            require(collection.get("sampled_at") is None and not fresh, "missing collection fabricated")
        else:
            sampled = utc_timestamp(collection.get("sampled_at"))
            require(sampled <= queried and (queried-sampled > 60 if collection["state"] == "stale" else queried-sampled <= 60), "process collection time invalid")
            if fresh: require(collection["state"] == "collecting" and sampled > after, "process collection not fresh")
        series = row.get("series")
        require(isinstance(series, list) and len(series) == 3 and {s.get("metric_key") for s in series} == set(PROCESS_METRICS), "process catalog not closed")
        for item in series:
            unit, window = PROCESS_METRICS[item["metric_key"]]
            require(item.get("unit") == unit and item.get("window_seconds") == window and item.get("dimensions") == {}
                    and isinstance(item.get("points"), list) and len(item["points"]) == 1, "process units/window/shape invalid")
            point = item["points"][0]
            at = utc_timestamp(point.get("evaluated_at"))
            require(0 <= queried-at <= 6, "process evaluation outside completion window")
            if absent:
                require(point.get("data_state") == absent and point.get("value") is None and point.get("sampled_at") is None, "inactive process reused history")
            elif point.get("data_state") == "valid":
                number = point.get("value")
                require(type(number) in {int, float} and math.isfinite(number) and number >= 0
                        and after < utc_timestamp(point.get("sampled_at")) <= at and queried-utc_timestamp(point["sampled_at"]) <= 60
                        and collection.get("state") == "collecting", "process value or freshness invalid")
                if item["metric_key"] == "process.memory.resident_bytes": require(number > 0, "real RSS missing")
                if item["metric_key"] == "process.uptime_seconds":
                    require(abs(number-(at-instance_timestamp(owner["process_started_at"]))) < 0.001, "process uptime not bound to current startup")
            else:
                require(not fresh and point.get("data_state") in {"no_data", "stale"}, "process window not complete")
                if point["data_state"] == "no_data": require(point.get("value") is None and point.get("sampled_at") is None, "missing process value fabricated")
                else:
                    require(type(point.get("value")) in {int, float} and math.isfinite(point["value"]) and point["value"] >= 0
                            and utc_timestamp(point.get("sampled_at")) <= at and queried-utc_timestamp(point["sampled_at"]) > 60, "stale process evidence invalid")
    require(len(anchors) == 1, "process batch anchors differ")
    return rows


def process_query_ready(admin, instances, *, after=0, inactive=(), disconnected=()):
    value = admin.request("GET", process_path(instances), (200,)).payload
    assert_process_summaries(value, instances, fresh=False, inactive=inactive, disconnected=disconnected)
    active = [r for r in value["data"] if r["subject"]["id"] not in {*inactive, *disconnected}]
    if not all(r["collection"]["state"] == "collecting" and all(p["data_state"] == "valid" and utc_timestamp(p["sampled_at"]) > after for s in r["series"] for p in s["points"]) for r in active): return False
    assert_process_summaries(value, instances, after=after, inactive=inactive, disconnected=disconnected)
    return True


def monitor_lifecycle(directory, action):
    require(action in {"stop", "start"}, "invalid process lifecycle action")
    require(FIXTURE.boundary() == directory, "process lifecycle must use owned Hosted deployment")
    # File output avoids inheriting a PIPE in the officially launched daemons.
    with (directory / ("monitor-"+action+".log")).open("a") as output:
        subprocess.run(["bash", "scripts/dev/"+action+".sh", "-monitor"], cwd=FIXTURE.ROOT,
                       env=dict(os.environ, SKIP_MODTIDY="1"), stdout=output, stderr=subprocess.STDOUT, check=True, timeout=300)
    if action == "start":
        identity = API.GatewayClient(os.environ["MONITOR_URL"], "", 10).request("GET", "/health/live", (200,)).payload
        try:
            PREFLIGHT.validate_health(PREFLIGHT.Service("monitor", os.environ["MONITOR_URL"]), identity,
                                      FIXTURE.command(["git", "-C", str(FIXTURE.ROOT), "rev-parse", "HEAD"]).strip(), "alive")
        except PREFLIGHT.PreflightError as error:
            raise SuiteError("restarted Monitor build identity mismatch") from error
        API.GatewayClient(os.environ["MONITOR_URL"], "", 10).request("GET", "/health/ready", (200,))


def check_process_restart(admin, machine, directory, instances, report):
    old = next(r for r in instances if r["module_name"] == "monitor")
    peer = next(r for r in instances if r["module_name"] == "system")
    monitor_lifecycle(directory, "stop")
    try:
        eventually(lambda: instance_list(admin, ids=str(old["id"]))[0]["status"] == "down", "gracefully stopped process identity")
        require(instance_list(admin, ids=str(peer["id"]))[0]["status"] == "up", "peer business stopped with monitored process")
    finally:
        monitor_lifecycle(directory, "start")
    current = current_processes(admin)
    new = next(r for r in current if r["module_name"] == "monitor")
    require(new["id"] != old["id"] and new["instance_id"] != old["instance_id"]
            and instance_timestamp(new["process_started_at"]) > instance_timestamp(old["process_started_at"])
            and next(r["id"] for r in current if r["module_name"] == "system") == peer["id"], "restart reused identity or restarted peer")
    # Current CPU can be warming up; the stopped instance is already all-null.
    assert_process_summaries(admin.request("GET", process_path([old, new, peer]), (200,)).payload, [old, new, peer], fresh=False, inactive=[old["id"]])
    eventually(lambda: process_query_ready(admin, [old, new, peer], inactive=[old["id"]]), "new process full-window resources and old instance exclusion", timeout=150)
    assert_process_discovery(machine.request("GET", DISCOVERY, (200,), response_type=list).payload, current, host_count=1)
    report["process_restart"] = {"old_id": old["id"], "new_id": new["id"], "peer_id": peer["id"], "old_values_null": True, "fresh_window": True}
    (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "process-restart-query.json").write_text(json.dumps(admin.request("GET", process_path([old, new, peer]), (200,)).payload))
    return current


def check_optional_process_source(admin, machine, directory, instances, tenant_clients, report):
    peer = next(r for r in instances if r["module_name"] == "system")
    key = directory / "process-monitor/server.key"
    backup = directory / "process-monitor-key-withheld"
    require(key.is_file() and not backup.exists(), "optional source fault requires owned private key")
    monitor_lifecycle(directory, "stop")
    key.rename(backup)
    try:
        monitor_lifecycle(directory, "start")
        rows = instance_list(admin, module_name="monitor", role="backend", status="up")
        require(len(rows) == 1 and not rows[0]["process_metrics_declared"] and rows[0]["node_id"] == "", "failed source still declared")
        failed = rows[0]
        assert_process_summaries(admin.request("GET", process_path([failed, peer]), (200,)).payload, [failed, peer], disconnected=[failed["id"]])
        assert_process_discovery(machine.request("GET", DISCOVERY, (200,), response_type=list).payload, [peer], host_count=1)
        API.GatewayClient(os.environ["MONITOR_URL"], "", 10).request("GET", "/health/ready", (200,))
        for client in tenant_clients: client.request("GET", "/api/v1/monitor/executions", (200,))
        report["optional_process_source"] = {"failed_instance_id": failed["id"], "declaration_absent": True, "business_ready": True, "peer_collecting": True}
    finally:
        monitor_lifecycle(directory, "stop")
        backup.rename(key)
        monitor_lifecycle(directory, "start")
    current = current_processes(admin)
    require(next(r["id"] for r in current if r["module_name"] == "system") == peer["id"], "optional source failure restarted peer")
    eventually(lambda: process_query_ready(admin, current), "source configuration recovery full-window", timeout=150)
    return current


def validate_resource_browser(value, expected, artifacts):
    require(value.get("schema_version") == "addp.node-resources-browser/v1" and value.get("result") == "passed"
            and value.get("stage") == "complete" and value.get("run_id") == expected["run_id"], "browser result mismatch")
    for field, principal, role in (("identity", "admin_id", "platform.system_administrator"),
                                    ("negative_identity", "security_id", "platform.security_administrator")):
        actor = value.get(field, {})
        platform_identity(actor, role)
        require(actor["principal"].get("id") == expected[principal], "browser did not use the API phase identity")
    require(value.get("negative_no_business_reads") is True and value.get("navigation") == {
        "list_summary_batch": True, "iframe_preserved": True, "history": True,
        "metric_reload": True, "range_reload": True, "server_window": True, "filesystem_reload": True, "inode_reload": True, "disk_reload": True, "network_reload": True}, "browser navigation or denial evidence missing")
    require(value.get("auto_refresh") == {"natural_timer": True, "server_end_advanced": True,
            "unchanged_url": True, "off_restored": True, "off_no_requests": True}, "browser automatic refresh evidence missing")
    summaries = value.get("summaries")
    require(isinstance(summaries, list) and 1 <= len(summaries) <= 4, "browser list summary missing or unbounded")
    for row in summaries:
        require(row.get("path") == SUMMARIES and row.get("query") == {"node_ids": expected["node"]["node_id"]}, "browser summary scope mismatch")
        data = row.get("value", {}).get("data")
        require(isinstance(data, list) and len(data) == 1, "browser summary batch incomplete")
        assert_resources(data[0], expected["node"], expected["target"], keys=["node.cpu.busy_percent", "node.memory.used_percent"])
        require(data[0]["policy_version"] == expected["policy_version"], "browser summary ignored current budget")
    rows = value.get("resources")
    # Six reads per complete round; 22 navigation/refresh rounds plus four bounded timer rounds.
    require(isinstance(rows, list) and 8 <= len(rows) <= 156, "browser resource evidence missing or unbounded")
    instants, trends = [], []
    for row in rows:
        query, resource = row.get("query", {}), row.get("value", {})
        require(query.get("node_id") == expected["node"]["node_id"], "browser requested another node")
        trend = row.get("path") == TRENDS
        allowed_query = {"node_id", "metrics", "start", "end", "device", "mountpoint", "fstype"} if trend else {"node_id", "metrics"}
        require(set(query) <= allowed_query, "browser query contains unapproved input")
        require(trend or row.get("path") == OBSERVATIONS, "browser resource path mismatch")
        keys = query.get("metrics", "").split(",")
        require((trend and len(keys) == 1 and keys[0] in {"node.memory.used_percent", "node.cpu.busy_percent", "node.filesystem.used_percent", "node.filesystem.inodes_used_percent", *DISK_METRICS, *NETWORK_METRICS})
                or (not trend and set(keys) in (set(METRICS), set(FILESYSTEM_METRICS), set(INODE_METRICS), set(DISK_METRICS), set(NETWORK_METRICS)) and len(keys) == len(set(keys))), "browser metric request mismatch")
        duration = utc_timestamp(query.get("end")) - utc_timestamp(query.get("start")) if trend else 0
        require(not trend or duration in {300, 3600}, "browser range mismatch")
        dims = {key: query[key] for key in ("device", "mountpoint", "fstype") if key in query}
        required_dimensions = ({"device"} if keys[0] in DISK_METRICS or keys[0] in NETWORK_METRICS else {"device", "mountpoint", "fstype"} if keys[0].startswith("node.filesystem.") else set()) if trend else set()
        require(set(dims) == required_dimensions, "browser selector mismatch")
        assert_resources(resource, expected["node"], expected["target"], trend, keys, range_seconds=duration, dimensions=dims, require_fresh=trend or set(keys) not in (set(INODE_METRICS), set(DISK_METRICS), set(NETWORK_METRICS)))
        if not trend and set(keys) == set(INODE_METRICS): require(fresh_resource_groups(resource, INODE_METRICS), "browser inode table has no supported fresh mount")
        if not trend and set(keys) == set(DISK_METRICS): require(fresh_resource_groups(resource, DISK_METRICS), "browser disk table has no fresh device")
        if not trend and set(keys) == set(NETWORK_METRICS): require(fresh_resource_groups(resource, NETWORK_METRICS), "browser network table has no fresh interface")
        require(resource["policy_version"] == expected["policy_version"], "browser ignored current query budget")
        if trend:
            end = utc_timestamp(query.get("end"))
            require(end == utc_timestamp(resource["end"])
                    and any(utc_timestamp(item["end"]) == end for item in instants), "browser trend lacks server anchor")
            trends.append((keys[0], duration))
        else:
            instants.append(resource)
    require(any(set(row["metric_key"] for row in item["series"]) == set(FILESYSTEM_METRICS) for item in instants), "browser mount table evidence missing")
    require(any(set(row["metric_key"] for row in item["series"]) == set(INODE_METRICS) for item in instants), "browser inode table evidence missing")
    require(any(set(row["metric_key"] for row in item["series"]) == set(DISK_METRICS) for item in instants), "browser disk table evidence missing")
    require(any(set(row["metric_key"] for row in item["series"]) == set(NETWORK_METRICS) for item in instants), "browser network table evidence missing")
    require(value.get("presentation") == {"iec_capacity": True, "elapsed_uptime": True, "system_load_count": True, "disk_rate_units": True, "network_rate_units": True, "disk_timing_units": True}, "browser quantity display evidence missing")
    require(len(instants) >= 2 and {("node.memory.used_percent", 3600), ("node.cpu.busy_percent", 300), ("node.filesystem.used_percent", 300), ("node.filesystem.inodes_used_percent", 300), *((key, 300) for key in ALL_METRICS if key in DISK_METRICS or key in NETWORK_METRICS)} <= set(trends), "browser restore evidence incomplete")
    for name in ("list", "detail", "restored", "filesystem", "inodes", "disks", "networks"):
        screenshot = artifacts / ("node-resources-" + name + ".png")
        require(screenshot.is_file() and screenshot.stat().st_size > 1000
                and screenshot.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n", "browser screenshot missing or invalid")
    return {"result": "passed", "password_mfa": True, "same_platform_identity": True,
            "catalog_metric_count": len(ALL_METRICS), "trend_server_window": True, "navigation_restore": True,
            "security_administrator_denied": True, "auto_refresh": True, "disk_trends": True, "network_trends": True, "disk_timing_trends": True, "quantity_display": True}


def validate_process_browser(value, expected, artifacts):
    require(value.get("service_presentation") == {"cpu_cores": True, "rss_iec": True, "elapsed_uptime": True,
            "inline_hints": True, "sample_time_visible": True, "unbound_host": True, "url_restore": True,
            "iframe_preserved": True, "negative_no_reads": True}, "service browser presentation or denial evidence missing")
    batches = value.get("service_batches")
    require(isinstance(batches, list) and 2 <= len(batches) <= 6, "service browser batch evidence missing or unbounded")
    wanted = {r["id"]: r for r in expected["processes"]}
    observed = set()
    for batch in batches:
        require(batch.get("path") == PROCESS_SUMMARIES and set(batch.get("query", {})) == {"instance_ids"}, "service browser used unapproved resource input")
        instances = batch.get("instances", {})
        rows = instances.get("data")
        require(isinstance(rows, list) and 1 <= len(rows) <= 20 and instances.get("page") == 1 and instances.get("page_size") == 20
                and type(instances.get("total")) is int and instances["total"] >= len(rows), "service browser list is not an authorized page")
        require(batch.get("instance_query") == {"page": "1", "page_size": "20", "role": "backend", "status": "up"}, "service browser list filters not restored")
        ids = [r.get("id") for r in rows]
        require(all(type(i) is int and i > 0 for i in ids) and len(set(ids)) == len(ids)
                and batch["query"]["instance_ids"] == ",".join(map(str, ids)), "service browser summary expanded owner page")
        for row in rows:
            if row["id"] in wanted:
                owner = wanted[row["id"]]
                require(all(row.get(key) == owner.get(key) for key in ("module_name", "instance_id", "role", "node_id", "process_started_at")), "browser instance differs from formal API phase")
                require(row.get("process_metrics_declared") is True, "browser current SDK declaration missing")
                observed.add(row["id"])
        inactive = [r["id"] for r in rows if r.get("status") != "up"]
        disconnected = [r["id"] for r in rows if not r.get("process_metrics_declared")]
        summaries = assert_process_summaries(batch.get("value", {}), rows, inactive=inactive, disconnected=disconnected)
        require(all(r["policy_version"] == expected["policy_version"] for r in summaries), "browser process query ignored current policy")
    require(observed == set(wanted), "browser never observed both current Go instances")
    screenshot = artifacts / "node-resources-services.png"
    require(screenshot.is_file() and screenshot.stat().st_size > 1000 and screenshot.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n", "service browser screenshot missing or invalid")
    return {"result": "passed", "real_instance_batch_reads": len(batches), "both_current_instances": True, "units_and_inline_hints": True,
            "url_restore": True, "same_platform_identity": True, "security_administrator_denied": True}


def run_resource_browser(node, target, policy, display_name, admin, security, processes):
    artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
    expected = {"run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"], "node": node, "target": {"id": target["id"], "version": target["version"]},
                "display_name": display_name, "policy_version": policy["version"],
                "admin_id": admin["principal"]["id"], "security_id": security["principal"]["id"],
                "processes": [{k: r[k] for k in ("id", "instance_id", "module_name", "role", "node_id", "process_started_at")} for r in processes]}
    # Store only the subject/version facts needed by the page, never target TLS/credentials.
    expected["node"] = {"node_id": node["node_id"], "version": node["version"]}
    (artifacts / "node-resources-browser-input.json").write_text(json.dumps(expected))
    evidence = artifacts / "node-resources-browser.json"
    evidence.unlink(missing_ok=True)
    for name in ("list", "detail", "restored", "filesystem", "inodes", "disks", "networks", "services"):
        (artifacts / ("node-resources-" + name + ".png")).unlink(missing_ok=True)
    result = subprocess.run(["npm", "exec", "--", "playwright", "test", "--config=playwright.online.config.js",
                             "e2e/online/platform-node-resources.spec.js",
                             "--output=" + str(Path(os.environ["ADDP_ONLINE_SECRET_DIR"]) / "node-resource-browser-output")], cwd=FIXTURE.ROOT / "console/frontend",
                            env=dict(os.environ), capture_output=True, text=True, timeout=300)
    # Playwright may print assertions from login; never forward credential-bearing output.
    require(result.returncode == 0, "resource browser failed; inspect sanitized stage report")
    require(evidence.is_file(), "resource browser report missing")
    value = json.loads(evidence.read_text())
    result = validate_resource_browser(value, expected, artifacts)
    result["services"] = validate_process_browser(value, expected, artifacts)
    return result


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
    report["collector_principal_id"] = machine.principal_id
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
    processes = current_processes(admin)
    assert_process_discovery(machine.request("GET", DISCOVERY, (200,), response_type=list).payload, processes)
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
            client.request("GET", resource_path(node, trend, keys=FILESYSTEM_METRICS), expected)
            client.request("GET", resource_path(node, trend, keys=INODE_METRICS), expected)
            client.request("GET", resource_path(node, trend, keys=DISK_METRICS), expected)
            client.request("GET", resource_path(node, trend, keys=NETWORK_METRICS), expected)
        client.request("GET", QUERY_POLICY, expected)
        client.request("PUT", QUERY_POLICY, expected, {key: value for key, value in query_policy.items() if key != "pending_restart"})
    require(admin.request("GET", QUERY_POLICY, (200,)).payload == query_policy, "denied budget writes changed state")
    report["query_identity_isolation"] = True
    assert_discovery(machine, target, node["version"], processes)
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
                eventually(lambda: resource_query_ready(admin, node, target), "all current catalog metrics after full-window warmup")
                value = admin.request("GET", resource_path(node), (200,)).payload
                (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "node-resource-query-check.json").write_text(json.dumps(value))
        assert_resources(value, node, target, trend)
    mount_value = admin.request("GET", resource_path(node, keys=FILESYSTEM_METRICS), (200,)).payload
    assert_resources(mount_value, node, target, keys=FILESYSTEM_METRICS)
    mounts = mount_value["series"]
    selected = mounts[0]["dimensions"]
    require(len(selected) == 3, "native filesystem dimensions missing")
    history = admin.request("GET", resource_path(node, True, keys=FILESYSTEM_METRICS, dimensions=selected), (200,)).payload
    assert_resources(history, node, target, trend=True, keys=FILESYSTEM_METRICS, dimensions=selected)
    (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "node-filesystem-query.json").write_text(json.dumps(mount_value))
    report["filesystem_query"] = True
    inode_value = admin.request("GET", resource_path(node, keys=INODE_METRICS), (200,)).payload
    assert_resources(inode_value, node, target, keys=INODE_METRICS, require_fresh=False)
    inode_mounts = fresh_resource_groups(inode_value, INODE_METRICS)
    require(inode_mounts, "native inode query has no supported fresh mount")
    inode_history = admin.request("GET", resource_path(node, True, keys=INODE_METRICS, dimensions=inode_mounts[0]), (200,)).payload
    assert_resources(inode_history, node, target, trend=True, keys=INODE_METRICS, dimensions=inode_mounts[0])
    (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "node-inode-query.json").write_text(json.dumps(inode_value))
    (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "node-inode-trend.json").write_text(json.dumps(inode_history))
    report["inode_query"] = True
    for name, family in (("disk", DISK_METRICS), ("network", NETWORK_METRICS)):
        report["stage"] = name+"-device-query-and-trends"
        eventually(lambda: resource_query_after(admin, node, target, 0, family), name+" fresh device after complete window")
        device_value = admin.request("GET", resource_path(node, keys=family), (200,)).payload
        assert_resources(device_value, node, target, keys=family, require_fresh=False)
        devices = fresh_resource_groups(device_value, family)
        require(devices, name+" native query has no fresh complete device")
        device_history = admin.request("GET", resource_path(node, True, keys=family, dimensions=devices[0]), (200,)).payload
        assert_resources(device_history, node, target, trend=True, keys=family, dimensions=devices[0])
        (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / ("node-"+name+"-query.json")).write_text(json.dumps(device_value))
        (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / ("node-"+name+"-trend.json")).write_text(json.dumps(device_history))
        report[name+"_query"] = True
    query_policy = check_query_policy(admin, node)
    report.update(resource_query=True, query_budget_cas_hot_read=True, query_policy_version=query_policy["version"])
    report["stage"] = "process-resource-identity-and-window"
    for client in [security, machine, *tenant_clients, API.GatewayClient(base, "", 10)]:
        expected = (401,) if not client.token else (403,)
        client.request("GET", process_path(processes), expected)
        client.request("GET", INSTANCES+"?ids="+str(processes[0]["id"]), expected)
    for query in ("0", "01", "1,1", "", "-1"):
        admin.request("GET", PROCESS_SUMMARIES+"?instance_ids="+urllib.parse.quote(query), (400,))
    eventually(lambda: process_query_ready(admin, processes), "two real SDK process full-window resources", timeout=150)
    report["native_process_samples"] = []
    for row in processes:
        selector = '{job="addp_nodes",addp_monitor_kind="process_resources",addp_source="application",addp_module_name="'+row["module_name"]+'",addp_instance_id="'+row["instance_id"]+'",addp_runtime_role="backend"}'
        samples = {}
        for metric in ("addp_process_identity_info", "process_cpu_seconds_total", "process_resident_memory_bytes", "process_start_time_seconds"):
            values = prom.query(metric+selector)
            require(len(values) == 1 and math.isfinite(float(values[0]["value"][1])), "real SDK family missing/duplicated")
            samples[metric] = float(values[0]["value"][1])
            if metric == "addp_process_identity_info":
                require(values[0]["metric"].get("runtime_instance_id") == row["instance_id"] and values[0]["metric"].get("operating_system") == "linux", "SDK self identity differs from formal registration")
        require(samples["process_resident_memory_bytes"] > 0 and abs(samples["process_start_time_seconds"]-instance_timestamp(row["process_started_at"])) <= 0.000001, "real SDK start/RSS invalid")
        report["native_process_samples"].append({"id": row["id"], "samples": samples})
    report["process_identity_isolation"] = True
    report["process_unbound_automatic_discovery"] = True
    (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "process-resource-query.json").write_text(json.dumps(admin.request("GET", process_path(processes), (200,)).payload))
    report["stage"] = "process-stop-restart-isolation"
    processes = check_process_restart(admin, machine, directory, processes, report)
    report["stage"] = "process-optional-source-business-boundary"
    processes = check_optional_process_source(admin, machine, directory, processes, tenant_clients, report)
    report["process_instances"] = [{k: r[k] for k in ("id", "instance_id", "module_name", "role", "process_started_at", "node_id")} for r in processes]
    report["stage"] = "console-resource-browser"
    report["resource_browser"] = run_resource_browser(node, target, query_policy, node_input["display_name"], admin_identity, security_identity, processes)
    # Fault phases use a new formal password/MFA session for the same principal;
    # do not extend production TTL or depend on the browser's private session.
    admin = login(base, "ADDP_ONLINE_METRICS_ADMIN")
    renewed = admin.request("GET", "/api/v1/system/auth/context", (200,)).payload
    platform_identity(renewed, "platform.system_administrator")
    require(renewed["principal"]["id"] == admin_identity["principal"]["id"], "fault phase principal changed")
    report["stage"] = "center-query-outage-recovery"
    center_action("stop")
    try:
        failure = admin.request("GET", resource_path(node), (503, 504)).payload
        require(failure.get("error_code") in {"observability_backend_unavailable", "observability_query_timeout"}, "center failure became query success")
        failure = admin.request("GET", process_path(processes), (503, 504)).payload
        require(failure.get("error_code") in {"observability_backend_unavailable", "observability_query_timeout"}, "center failure became process query success")
        require({r["id"] for r in current_processes(admin)} == {r["id"] for r in processes}, "center failure broke service registration")
        API.GatewayClient(os.environ["MONITOR_URL"], "", 10).request("GET", "/health/ready", (200,))
        for client in tenant_clients:
            client.request("GET", "/api/v1/monitor/executions", (200,))
    finally:
        recovery = time.time()
        center_action("start")
    eventually(lambda: resource_query_after(admin, node, target, recovery), "new API query evidence after center recovery")
    eventually(lambda: resource_query_after(admin, node, target, recovery, FILESYSTEM_METRICS), "new filesystem evidence after center recovery")
    eventually(lambda: resource_query_after(admin, node, target, recovery, INODE_METRICS), "new inode evidence after center recovery")
    eventually(lambda: resource_query_after(admin, node, target, recovery, DISK_METRICS), "new disk evidence after center recovery")
    eventually(lambda: resource_query_after(admin, node, target, recovery, NETWORK_METRICS), "new network evidence after center recovery")
    eventually(lambda: process_query_ready(admin, processes, after=recovery), "new process evidence after center recovery", timeout=150)
    report["process_center_outage_recovery"] = True
    report["center_query_outage_recovery"] = True
    report["stage"] = "source-outage-recovery"
    source_action("stop")
    try:
        eventually(lambda: bool(prom.query("up"+expression+" == 0")), "source outage")
        require(prom.query('up{job="prometheus"} == 1'), "center failed with source")
        require(process_query_ready(admin, processes), "host source failure broke independent process resources")
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
    assert_process_discovery(machine.request("GET", DISCOVERY, (200,), response_type=list).payload, processes)
    require(process_query_ready(admin, processes), "unbound process incorrectly disabled with host")
    for trend in (False, True):
        assert_resources(admin.request("GET", resource_path(node, trend), (200,)).payload, node, trend=trend, disconnected=True)
        assert_resources(admin.request("GET", resource_path(node, trend, keys=FILESYSTEM_METRICS), (200,)).payload, node, trend=trend, keys=FILESYSTEM_METRICS, disconnected=True)
        assert_resources(admin.request("GET", resource_path(node, trend, keys=INODE_METRICS), (200,)).payload, node, trend=trend, keys=INODE_METRICS, disconnected=True)
        assert_resources(admin.request("GET", resource_path(node, trend, keys=DISK_METRICS), (200,)).payload, node, trend=trend, keys=DISK_METRICS, disconnected=True)
        assert_resources(admin.request("GET", resource_path(node, trend, keys=NETWORK_METRICS), (200,)).payload, node, trend=trend, keys=NETWORK_METRICS, disconnected=True)
    eventually(lambda: not prom.targets(node["node_id"]), "disabled node removal after control recovery")
    resumed_at = time.time()
    node = admin.request("PUT", NODES+"/"+node["node_id"], (200,), dict(node_input, version=node["version"])).payload
    assert_discovery(machine, target, node["version"], processes)
    eventually(active, "re-enabled node sampling")
    # Require this activation's samples and current versions through the user API.
    eventually(lambda: resource_query_after(admin, node, target, resumed_at), "new API resource evidence after node re-enablement")
    eventually(lambda: resource_query_after(admin, node, target, resumed_at, FILESYSTEM_METRICS), "new filesystem evidence after node re-enablement")
    eventually(lambda: resource_query_after(admin, node, target, resumed_at, INODE_METRICS), "new inode evidence after node re-enablement")
    eventually(lambda: resource_query_after(admin, node, target, resumed_at, DISK_METRICS), "new disk evidence after node re-enablement")
    eventually(lambda: resource_query_after(admin, node, target, resumed_at, NETWORK_METRICS), "new network evidence after node re-enablement")
    report["node_query_resume_fresh_samples"] = True
    report["stage"] = "target-disable-and-delete"
    target = admin.request("PUT", target_path, (200,), dict(body, enabled=False, version=target["version"])).payload
    assert_process_discovery(machine.request("GET", DISCOVERY, (200,), response_type=list).payload, processes)
    for trend in (False, True):
        assert_resources(admin.request("GET", resource_path(node, trend), (200,)).payload, node, trend=trend, disconnected=True)
        assert_resources(admin.request("GET", resource_path(node, trend, keys=FILESYSTEM_METRICS), (200,)).payload, node, trend=trend, keys=FILESYSTEM_METRICS, disconnected=True)
        assert_resources(admin.request("GET", resource_path(node, trend, keys=INODE_METRICS), (200,)).payload, node, trend=trend, keys=INODE_METRICS, disconnected=True)
        assert_resources(admin.request("GET", resource_path(node, trend, keys=DISK_METRICS), (200,)).payload, node, trend=trend, keys=DISK_METRICS, disconnected=True)
        assert_resources(admin.request("GET", resource_path(node, trend, keys=NETWORK_METRICS), (200,)).payload, node, trend=trend, keys=NETWORK_METRICS, disconnected=True)
    report["disabled_query_does_not_reuse_history"] = True
    eventually(lambda: not prom.targets(node["node_id"]), "target disable application")
    admin.request("DELETE", target_path, (409,), {"version": 2})
    admin.request("DELETE", target_path, (204,), {"version": target["version"]})
    admin.request("GET", target_path, (404,))
    admin.request("PUT", NODES+"/"+node["node_id"], (200,), dict(node_input, enabled=False, version=node["version"]))
    report.update(collector_grant_count=machine.grant_count, source_outage_recovery=True, control_outage_recovery=True, node_and_target_removal=True,
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
