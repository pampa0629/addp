"""Real Platform identity, source admission and native Prometheus discovery T4."""
from __future__ import annotations

import base64
import datetime
import hashlib
import hmac
import http.server
import json
import math
import mmap
import os
import re
import secrets
import signal
import ssl
import struct
import subprocess
import sys
import time
import threading
import urllib.parse
import uuid
import urllib.request
from contextlib import contextmanager
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
OVERVIEW = "/api/v1/monitor/platform/resource_overview"
TRENDS = "/api/v1/monitor/platform/resource_trends"
QUERY_POLICY = "/api/v1/monitor/settings/resource-query-policy"
INSTANCES = "/api/v1/system/platform/module-instances"
PROCESS_SUMMARIES = "/api/v1/monitor/platform/process_resource_summaries"
PROCESS_METRICS = {"process.cpu.core_equivalents": ("cores", 60),
                   "process.memory.resident_bytes": ("bytes", 0), "process.uptime_seconds": ("seconds", 0)}
METRICS = {"node.cpu.logical_cores": "cores", "node.cpu.busy_percent": "percent", "node.memory.total_bytes": "bytes",
           "node.memory.available_bytes": "bytes", "node.memory.used_percent": "percent",
           "node.load.average_1m": "load", "node.load.average_5m": "load",
           "node.load.average_15m": "load", "node.uptime_seconds": "seconds", "node.tcp.current_connections": "connections"}
HOST_LIST_METRICS = ["node.cpu.logical_cores", "node.cpu.busy_percent", "node.memory.total_bytes", "node.memory.used_percent", "node.uptime_seconds"]
FILESYSTEM_METRICS = {"node.filesystem."+key: unit for key, unit in (("total_bytes", "bytes"), ("free_bytes", "bytes"), ("available_bytes", "bytes"), ("used_bytes", "bytes"), ("used_percent", "percent"))}
INODE_METRICS = {"node.filesystem."+key: unit for key, unit in (("inodes_total", "inodes"), ("inodes_free", "inodes"), ("inodes_used", "inodes"), ("inodes_used_percent", "percent"))}
DISK_METRICS = {"node.disk."+key: unit for key, unit in (("read_bytes_per_second", "bytes_per_second"), ("write_bytes_per_second", "bytes_per_second"), ("io_busy_percent", "percent"), ("read_mean_duration_milliseconds", "milliseconds"), ("write_mean_duration_milliseconds", "milliseconds"))}
NETWORK_METRICS = {"node.network."+key: "bytes_per_second" for key in ("receive_bytes_per_second", "transmit_bytes_per_second")}
DEVICE_RATE_FAMILIES = (DISK_METRICS, NETWORK_METRICS)
ALL_METRICS = dict(METRICS, **FILESYSTEM_METRICS, **INODE_METRICS, **DISK_METRICS, **NETWORK_METRICS)
REQUIRED = {"platform.host_node.create", "platform.host_node.read", "platform.host_node.update",
            "monitor.monitoring_target.create", "monitor.monitoring_target.read",
            "monitor.monitoring_target.update", "monitor.monitoring_target.delete",
            "monitor.resource_observation.read", "monitor.configuration.read", "monitor.configuration.update", "platform.module.read",
            "monitor.platform_resource_rule.read", "monitor.platform_resource_rule.create",
            "monitor.platform_resource_rule.update", "monitor.platform_resource_rule.delete",
            "monitor.platform_alert.read", "monitor.platform_alert_notification.read", "monitor.platform_alert_notification.update"}


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

    def query(self, expression, *, at=None):
        query = {"query": expression}
        if at is not None:
            query["time"] = at
        return self.request("/api/v1/query?"+urllib.parse.urlencode(query))["result"]

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


def source_scrape_failed(prom, expression):
    # Resource overview evaluates at whole seconds; its failure precondition needs the same precision.
    at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time()))
    return bool(prom.query("up" + expression + " == 0", at=at))


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


def resource_path(node, trend=False, keys=None, dimensions=None, range_seconds=60):
    query = {"node_id": node["node_id"], "metrics": ",".join(METRICS if keys is None else keys)}
    if dimensions:
        query.update(dimensions)
    if trend:
        end = int(time.time())
        for field, timestamp in (("start", end-range_seconds), ("end", end)):
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
                require(expected[key] not in {"inodes", "connections"} or (number <= 2**53-1 and number == math.floor(number)), "invalid resource integer")
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
    overview_failure = admin.request("GET", OVERVIEW, (422,)).payload
    require(overview_failure.get("error_code") == "observability_query_budget_exceeded" and "inventory" not in overview_failure and "rankings" not in overview_failure, "overview returned a partial scope under lowered budget")
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


def assert_process_discovery(groups, processes, host_count=0, source_ports=None):
    require(isinstance(groups, list) and len(groups) == len(processes)+host_count, "discovery contains missing or extra sources")
    actual = [g for g in groups if g.get("labels", {}).get("addp_monitor_kind") == "process_resources"]
    require(len(actual) == len(processes), "automatic process discovery missing")
    for row in processes:
        owned = [g for g in actual if g["labels"].get("addp_instance_id") == row["instance_id"]]
        require(len(owned) == 1, "process discovery identity duplicated or absent")
        group = owned[0]
        require(group["labels"] == {"__scheme__": "https", "__metrics_path__": "/metrics", "addp_module_name": row["module_name"],
                "addp_instance_id": row["instance_id"], "addp_runtime_role": row["role"], "addp_monitor_kind": "process_resources", "addp_source": "application"}
                and group.get("targets") == [os.environ["ADDP_ONLINE_METRICS_NODE_IP"]+":"+str(FIXTURE.PROCESS_PORTS[row["module_name"]] if source_ports is None else source_ports[row["instance_id"]])], "process discovery is not bound to formal instance/endpoint")


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


def process_query_ready(admin, instances, *, after=0, inactive=(), disconnected=(), evidence=None):
    value = admin.request("GET", process_path(instances), (200,)).payload
    assert_process_summaries(value, instances, fresh=False, inactive=inactive, disconnected=disconnected)
    if evidence is not None:
        # Retain only the already-validated public resource facts, never unknown fields.
        evidence["data"] = [{
            **{key: row[key] for key in ("subject", "node_id", "queried_at", "policy_version", "lookback_seconds")},
            "collection": {key: row["collection"].get(key) for key in ("state", "sampled_at")},
            "series": [{**{key: series[key] for key in ("metric_key", "unit", "window_seconds", "dimensions")},
                        "points": [{key: point.get(key) for key in ("evaluated_at", "sampled_at", "data_state", "value")} for point in series["points"]]}
                       for series in row["series"]]
        } for row in value["data"]]
    active = [r for r in value["data"] if r["subject"]["id"] not in {*inactive, *disconnected}]
    if not all(r["collection"]["state"] == "collecting" and all(p["data_state"] == "valid" and utc_timestamp(p["sampled_at"]) > after for s in r["series"] for p in s["points"]) for r in active): return False
    assert_process_summaries(value, instances, after=after, inactive=inactive, disconnected=disconnected)
    return True


def assert_source_process_isolation(admin, prom, instances, report):
    evidence = report["source_outage_process_summaries"] = {}
    ready = process_query_ready(admin, instances, evidence=evidence)
    if not ready:
        witnesses = report["source_outage_process_window"] = []
        for owner in instances:
            # The public batch is keyed by identity, not ordered by the request.
            snapshot = next(row for row in evidence["data"] if row["subject"]["id"] == owner["id"])
            at = snapshot["series"][0]["points"][0]["evaluated_at"]
            selector = '{job="addp_nodes",addp_monitor_kind="process_resources",addp_source="application",'+",".join(
                key+"="+json.dumps(owner[field]) for key, field in (("addp_module_name", "module_name"), ("addp_instance_id", "instance_id"), ("addp_runtime_role", "role")))+"}"
            cpu = "process_cpu_seconds_total"+selector
            probes = {"cpu_window_count": "max(count_over_time("+cpu+"[1m]))", "cpu_resets": "max(resets("+cpu+"[1m]))",
                      "cpu_rate": "max(rate("+cpu+"[1m]))"}
            identity = "addp_process_identity_info"+selector
            matched = identity[:-1]+","+",".join(key+"="+json.dumps(owner[field]) for key, field in (
                ("module_name", "module_name"), ("runtime_instance_id", "instance_id"), ("runtime_role", "role")))+',schema_version="addp.process-metrics/v1",operating_system=~"darwin|linux"}'
            for prefix, raw in (("cpu", cpu), ("identity", identity), ("start", "process_start_time_seconds"+selector)):
                for suffix, offset in (("current", ""), ("previous", " offset 1m")):
                    probes[prefix+"_"+suffix+"_count"] = "count("+raw+offset+")"
                    probes[prefix+"_"+suffix+"_value"] = "max("+raw+offset+")"
                    probes[prefix+"_"+suffix+"_stamp"] = "max(timestamp("+raw+offset+"))"
            for suffix, offset in (("current", ""), ("previous", " offset 1m")):
                probes["identity_"+suffix+"_matched_count"] = "count("+matched+offset+")"
            up = "up"+selector
            probes.update(up_count="count("+up+")", up_value="max("+up+")", up_stamp="max(timestamp("+up+"))")
            expression = " or ".join('label_replace(('+value+'),"addp_probe",'+json.dumps(name)+',"","")' for name, value in probes.items())
            witness = {"subject": snapshot["subject"], "evaluated_at": at, "expected_started_at": owner["process_started_at"], "state": "unavailable"}
            witnesses.append(witness)
            try:
                rows = prom.query(expression, at=at)
                require(isinstance(rows, list) and len(rows) <= len(probes), "invalid window witness cardinality")
                values, seen = dict.fromkeys(probes), set()
                for row in rows:
                    key, pair = row["metric"].get("addp_probe"), row["value"]
                    require(key in probes and key not in seen and len(pair) == 2 and float(pair[0]) == utc_timestamp(at), "invalid window witness identity or anchor")
                    number = float(pair[1])
                    require(math.isfinite(number), "invalid window witness value")
                    values[key] = number
                    seen.add(key)
                witness.update(state="captured", values=values)
            except Exception:
                # Diagnostics cannot replace the original isolation verdict or export raw errors.
                pass
    require(ready, "host source failure broke independent process resources")


def monitor_lifecycle(directory, action, *, node_id=None):
    require(action in {"stop", "start"}, "invalid process lifecycle action")
    require(FIXTURE.boundary() == directory, "process lifecycle must use owned Hosted deployment")
    environment = dict(os.environ, SKIP_MODTIDY="1")
    if node_id is not None:
        require(action == "start" and canonical_uuid(node_id), "host binding requires an explicit canonical node identity for a new process")
        environment["ADDP_HOST_NODE_ID"] = node_id
    # File output avoids inheriting a PIPE in the officially launched daemons.
    with (directory / ("monitor-"+action+".log")).open("a") as output:
        subprocess.run(["bash", "scripts/dev/"+action+".sh", "-monitor"], cwd=FIXTURE.ROOT,
                       env=environment, stdout=output, stderr=subprocess.STDOUT, check=True, timeout=300)
    if action == "start":
        service = PREFLIGHT.Service("monitor", os.environ["MONITOR_URL"])
        try:
            identity = PREFLIGHT.load_health(service, "/health/live", 10)
            PREFLIGHT.validate_health(service, identity,
                                      FIXTURE.command(["git", "-C", str(FIXTURE.ROOT), "rev-parse", "HEAD"]).strip(), "live")
            require(PREFLIGHT.load_health(service, "/health/ready", 10).get("status") == "ready", "restarted Monitor is not ready")
        except PREFLIGHT.PreflightError as error:
            raise SuiteError("restarted Monitor health validation failed: "+str(error)) from error


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


def assert_resource_overview(value, node, policy_version, display_name, disconnected=False, after=0, source_failed=False):
    """The disposable suite owns exactly one real host; never invent TOP5 peers."""
    require(isinstance(value, dict) and set(value) == {"scope", "evaluated_at", "queried_at", "policy_version", "lookback_seconds", "inventory", "rankings"}, "overview projection mismatch")
    evaluated, completed = utc_timestamp(value["evaluated_at"]), utc_timestamp(value["queried_at"])
    require(value["scope"] == "current_authorized_host_nodes" and evaluated == math.floor(evaluated)
            and completed >= evaluated and type(value["policy_version"]) is int and value["policy_version"] == policy_version
            and value["lookback_seconds"] == 300, "overview scope, time or budget mismatch")
    inventory = value["inventory"]
    require(not (disconnected and source_failed), "source failure must remain connected to its current target")
    state = "not_connected" if disconnected else "failed" if source_failed else "collecting"
    collection = {key: int(key == state) for key in ("collecting", "failed", "stale", "no_sample", "not_connected")}
    wanted = {"managed": 1, "enabled": int(node["enabled"]), "disabled": int(not node["enabled"]),
              "connected": int(not disconnected), "not_connected": int(disconnected), "collection": collection}
    require(isinstance(inventory, dict) and inventory == wanted and all(type(inventory[key]) is int for key in wanted if key != "collection")
            and all(type(v) is int for v in inventory["collection"].values()), "overview inventory is not the complete one-host scope")
    rankings = value["rankings"]
    require(isinstance(rankings, list) and len(rankings) == 2, "overview ranking catalog mismatch")
    for rank, (key, window) in zip(rankings, (("node.cpu.busy_percent", 60), ("node.memory.used_percent", 0))):
        require(isinstance(rank, dict) and set(rank) == {"metric_key", "unit", "window_seconds", "coverage", "top"}
                and rank["metric_key"] == key and rank["unit"] == "percent" and type(rank["window_seconds"]) is int and rank["window_seconds"] == window, "overview metric projection mismatch")
        wanted_coverage = {"expected": 1, "valid": int(not disconnected), "no_data": 0, "stale": 0,
                           "not_connected": int(disconnected), "complete": not disconnected}
        coverage = rank["coverage"]
        require(isinstance(coverage, dict) and set(coverage) == set(wanted_coverage) and type(coverage["complete"]) is bool
                and all(type(coverage[k]) is int and coverage[k] >= 0 for k in wanted_coverage if k != "complete"), "overview coverage types mismatch")
        if source_failed:
            require(coverage["expected"] == 1 and coverage["not_connected"] == 0
                    and coverage["valid"]+coverage["no_data"]+coverage["stale"] == 1
                    and coverage["complete"] == (coverage["valid"] == 1), "failed source coverage forged a healthy or disconnected scope")
        else:
            require(coverage == wanted_coverage, "overview coverage reused missing or disconnected data")
        require(isinstance(rank["top"], list) and len(rank["top"]) == coverage["valid"], "overview TOP5 missing or reused history")
        if not coverage["valid"]:
            continue
        row = rank["top"][0]
        require(isinstance(row, dict) and set(row) == {"node", "value", "sampled_at"}, "overview rank exposes extra facts")
        require(row["node"] == {"node_id": node["node_id"], "display_name": display_name, "node_kind": "virtual", "version": node["version"], "enabled": True}, "overview rank host identity changed")
        require(type(row["node"]["version"]) is int and row["node"]["enabled"] is True, "overview rank identity types mismatch")
        sampled = utc_timestamp(row["sampled_at"])
        require(type(row["value"]) in {int, float} and math.isfinite(row["value"]) and 0 <= row["value"] <= 100
                and after < sampled <= evaluated and completed-sampled <= 60, "overview rank has no fresh native sample")
    return value


def assert_source_overview_isolation(admin, prom, expression, node, policy_version, display_name, report):
    value = admin.request("GET", OVERVIEW, (200,)).payload
    try:
        return assert_resource_overview(value, node, policy_version, display_name, source_failed=True)
    except SuiteError:
        witness = report["source_outage_host_overview"] = {"state": "unavailable"}
        try:
            inventory = value["inventory"]
            witness["inventory"] = {key: inventory[key] for key in ("managed", "enabled", "disabled", "connected", "not_connected")
                                    if type(inventory.get(key)) is int and inventory[key] >= 0}
            collection = inventory["collection"]
            witness["inventory"]["collection"] = {key: collection[key] for key in ("collecting", "failed", "stale", "no_sample", "not_connected")
                                                  if type(collection.get(key)) is int and collection[key] >= 0}
            at = value["evaluated_at"]
            require(utc_timestamp(at) == math.floor(utc_timestamp(at)), "invalid overview diagnostic anchor")
            witness["evaluated_at"] = at
            up = "up" + expression
            probes = {"up_count": "count(" + up + ") or vector(0)", "up": "max(" + up + ")", "sampled_at": "max(timestamp(" + up + "))"}
            query = " or ".join('label_replace((' + raw + '),"addp_probe",' + json.dumps(key) + ',"","")' for key, raw in probes.items())
            rows = prom.query(query, at=at)
            require(isinstance(rows, list) and len(rows) <= len(probes), "invalid overview diagnostic cardinality")
            values, seen = dict.fromkeys(probes), set()
            for row in rows:
                key, pair = row["metric"].get("addp_probe"), row["value"]
                require(key in probes and key not in seen and len(pair) == 2 and float(pair[0]) == utc_timestamp(at), "invalid overview diagnostic identity or anchor")
                number = float(pair[1])
                require(math.isfinite(number), "invalid overview diagnostic value")
                values[key] = number
                seen.add(key)
            witness.update(state="captured", values=values)
        except Exception:
            # Diagnostics must never replace the original verdict or export raw errors.
            pass
        raise


def validate_overview_browser(value, expected, artifacts):
    evidence = value.get("overview", {})
    flags = ("natural_timer", "server_end_advanced", "unchanged_url", "iframe_preserved", "off_restored", "off_no_requests", "detail_and_back", "negative_no_reads")
    require(all(evidence.get(key) is True for key in flags), "overview browser lifecycle evidence missing")
    rows = evidence.get("responses")
    require(isinstance(rows, list) and 4 <= len(rows) <= 6, "overview browser responses missing or unbounded")
    for row in rows:
        require(row.get("path") == OVERVIEW and row.get("query") == {}, "overview browser changed full scope")
        assert_resource_overview(row.get("value"), dict(expected["node"], enabled=True), expected["policy_version"], expected["display_name"])
    require(utc_timestamp(rows[1]["value"]["evaluated_at"]) > utc_timestamp(rows[0]["value"]["evaluated_at"]), "overview natural server time did not advance")
    screenshot = artifacts / "node-resources-overview.png"
    require(screenshot.is_file() and screenshot.stat().st_size > 1000 and screenshot.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n", "overview screenshot missing or invalid")
    return {"result": "passed", "real_hosts": 1, **{key: True for key in flags}}


def validate_resource_comparison_browser(value, expected, artifacts):
    cases, resources = value.get("period_comparisons"), value.get("resources")
    require(isinstance(cases, list) and len(cases) == 3 and isinstance(resources, list), "browser period comparison evidence missing")
    phases = (("cpu", "node.cpu.busy_percent"), ("memory", "node.memory.used_percent"), ("restored", "node.memory.used_percent"))
    indices, complete_count = set(), 0
    for position, (case, (phase, key)) in enumerate(zip(cases, phases)):
        require(isinstance(case, dict) and case.get("phase") == phase, "comparison phase mismatch")
        before, index = case.get("before_index"), case.get("resource_index")
        require(type(before) is int and type(index) is int and 0 <= before <= index < len(resources)
                and index == before + 5 and index not in indices, "comparison resource reference invalid")
        if position:
            require(before == cases[position - 1]["resource_index"] + 1, "comparison rounds overlap or have extra reads")
        indices.add(index)
        batch = resources[before:index + 1]
        require(sum(row.get("path") == TRENDS for row in batch) == 1 and batch[-1].get("path") == TRENDS
                and {frozenset(row.get("query", {}).get("metrics", "").split(",")) for row in batch[:-1]} == {
                    frozenset(family) for family in (METRICS, FILESYSTEM_METRICS, INODE_METRICS, DISK_METRICS, NETWORK_METRICS)}
                and all(row.get("path") == OBSERVATIONS for row in batch[:-1]), "comparison used duplicate or fallback queries")
        page_query = case.get("page_query", {})
        require(case.get("page_path") == "/monitor/node-resources/" + expected["node"]["node_id"]
                and page_query == {"range": "5m", "compare": "previous", "refresh": "off", **({"metric": key} if key == "node.cpu.busy_percent" else {})}, "comparison page scope changed")
        row = resources[index]
        query, response = row.get("query", {}), row.get("value", {})
        require(set(query) == {"node_id", "metrics", "start", "end"} and query["node_id"] == expected["node"]["node_id"]
                and query["metrics"] == key and utc_timestamp(query["end"]) - utc_timestamp(query["start"]) == 600, "comparison trend scope changed")
        anchor = next(item["value"] for item in batch[:-1] if set(item["query"]["metrics"].split(",")) == set(METRICS))
        require(utc_timestamp(query["end"]) == utc_timestamp(anchor["end"]), "comparison lost its own server anchor")
        assert_resources(response, expected["node"], expected["target"], True, [key], range_seconds=600)
        require(response["policy_version"] == expected["policy_version"], "comparison ignored current query budget")
        statistics = case.get("statistics", {})
        require(isinstance(statistics, dict) and set(statistics) == {"previous", "current", "complete", "delta", "stepSeconds"}
                and type(statistics["complete"]) is bool and type(statistics["stepSeconds"]) is int
                and statistics["stepSeconds"] == response["step_seconds"], "comparison statistics contract mismatch")
        start, end = utc_timestamp(response["start"]), utc_timestamp(response["end"])
        boundary = end - 300
        periods = {"previous": (start, boundary), "current": (boundary, end)}
        observations = response["series"][0]["points"]
        periods = {name: {"start": left, "end": right, "points": [point for point in observations if left < utc_timestamp(point["evaluated_at"]) <= right]} for name, (left, right) in periods.items()}
        complete = all(period["points"] and all(point["data_state"] == "valid" for point in period["points"]) for period in periods.values())
        require(statistics["complete"] == complete, "comparison concealed missing evidence")
        complete_count += int(complete)
        means = {}
        for name, period in periods.items():
            summary = statistics[name]
            require(isinstance(summary, dict) and set(summary) == {"start", "end", "valid", "total", "mean"}, "comparison period contract mismatch")
            require(utc_timestamp(summary["start"]) == period["start"] and utc_timestamp(summary["end"]) == period["end"], "comparison period boundary mismatch")
            count = sum(point["data_state"] == "valid" for point in period["points"])
            require(type(summary["valid"]) is int and type(summary["total"]) is int
                    and summary["valid"] == count and summary["total"] == len(period["points"]), "comparison coverage mismatch")
            if complete:
                means[name] = math.fsum(point["value"] for point in period["points"]) / len(period["points"])
                require(type(summary["mean"]) in {int, float} and math.isfinite(summary["mean"])
                        and math.isclose(summary["mean"], means[name], rel_tol=1e-12, abs_tol=1e-9), "comparison point mean mismatch")
            else:
                require(summary["mean"] is None, "comparison invented a partial-period mean")
        if complete:
            require(type(statistics["delta"]) in {int, float} and math.isfinite(statistics["delta"])
                    and math.isclose(statistics["delta"], means["current"] - means["previous"], rel_tol=1e-12, abs_tol=1e-9), "comparison percentage-point change mismatch")
        else:
            require(statistics["delta"] is None, "comparison invented a change with incomplete samples")
        screenshot = artifacts / ("node-resources-comparison-" + phase + ".png")
        require(screenshot.is_file() and screenshot.stat().st_size > 1000 and screenshot.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n", "comparison screenshot missing or invalid")
    require(cases[1]["page_query"] == cases[2]["page_query"], "comparison URL did not survive reload")
    return indices, {"result": "passed", "cases": 3, "complete_cases": complete_count, "incomplete_cases": 3 - complete_count,
                     "single_query_each": True, "url_restore": True}


def filesystem_mount_groups(sources):
    mounts = {}
    for source in sources:
        for series in source["series"]:
            dimensions = series["dimensions"]
            if set(dimensions) != {"device", "mountpoint", "fstype"}: continue
            key = tuple(dimensions[name] for name in ("device", "mountpoint", "fstype"))
            mounts.setdefault(key, {})[series["metric_key"]] = series["points"][0]
    return mounts


def validate_filesystem_mount_readings(rows, receipt):
    require(isinstance(receipt, dict) and set(receipt) == {"capacity_index", "inode_index", "row_index", "dimensions", "metrics"}, "browser mount readings missing")
    sources = []
    for field, family in (("capacity_index", FILESYSTEM_METRICS), ("inode_index", INODE_METRICS)):
        index = receipt[field]
        require(type(index) is int and 0 <= index < len(rows), "browser mount receipt reference invalid")
        source = rows[index]
        require(source["path"] == OBSERVATIONS and set(source["query"]["metrics"].split(",")) == set(family), "browser mount receipt family mismatch")
        sources.append(source["value"])
    mounts = filesystem_mount_groups(sources)
    index, dimensions = receipt["row_index"], receipt["dimensions"]
    require(type(index) is int and 0 <= index < len(mounts) and isinstance(dimensions, dict)
            and set(dimensions) == {"device", "mountpoint", "fstype"}, "browser mount row identity incomplete")
    key = tuple(dimensions[name] for name in ("device", "mountpoint", "fstype"))
    require(list(mounts)[index] == key, "browser displayed another mount row")
    metrics = receipt["metrics"]
    require(isinstance(metrics, dict) and set(metrics) == set(FILESYSTEM_METRICS) | set(INODE_METRICS), "browser mount displayed metric scope mismatch")
    for metric, text in metrics.items():
        point = mounts[key].get(metric, {})
        require(point.get("data_state") == "valid" and type(point.get("value")) in (int, float), "browser mount lacked a valid reading")
        require(isinstance(text, str) and len(text) <= 80, "browser mount display missing")
        number = point["value"]
        if metric.endswith("_bytes"):
            units, scale = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"], 0
            while number >= 1024 and scale < len(units)-1:
                number /= 1024
                scale += 1
            suffix = units[scale]
        elif metric.endswith("_percent"):
            suffix = "%"
        else:
            require(text.replace(",", "") == str(int(number)) + " 个", "browser inode count differs from API")
            continue
        match = re.fullmatch(r"([0-9][0-9,]*\.[0-9]{2}) " + re.escape(suffix), text)
        require(match is not None and abs(float(match[1].replace(",", ""))-number) <= 0.005000001, "browser mount value or unit differs from API")
    return dimensions


def validate_filesystem_mount_browser(value):
    rows = value["resources"]
    dimensions = validate_filesystem_mount_readings(rows, value.get("filesystem_mount"))
    # The same complete three-part identity must own both restored trends.
    for metric in ("node.filesystem.used_percent", "node.filesystem.inodes_used_percent"):
        require(any(row["path"] == TRENDS and row["query"].get("metrics") == metric
                    and all(row["query"].get(name) == item for name, item in dimensions.items()) for row in rows),
                "browser mount readings were not bound to restored trends")
    return {"capacity_and_inodes": True, "complete_mount_identity": True, "same_mount_trends": True}


def validate_filesystem_list_browser(value):
    receipt, rows = value.get("filesystem_list"), value["resources"]
    require(isinstance(receipt, dict) and set(receipt) == {"before_index", "opened", "refreshed", "unchanged_url", "iframe_preserved", "collapsed"}
            and all(receipt[key] is True for key in ("unchanged_url", "iframe_preserved", "collapsed")), "browser filesystem list lifecycle missing")
    before = receipt["before_index"]
    require(type(before) is int and 0 <= before <= len(rows)-4, "browser filesystem list batch reference invalid")
    for phase, offset in (("opened", 0), ("refreshed", 2)):
        mounted = receipt[phase]
        validate_filesystem_mount_readings(rows, mounted)
        indices = [mounted["capacity_index"], mounted["inode_index"]]
        require(sorted(indices) == [before+offset, before+offset+1], "browser filesystem list reused another batch")
    return {"complete_mount_readings": True, "manual_refresh": True, "bounded_reads": 4,
            "collapsed": True, "unchanged_url": True, "iframe_preserved": True}


def validate_resource_heatmap_browser(value):
    receipt, rows = value.get("resource_heatmap"), value["resources"]
    require(isinstance(receipt, dict) and set(receipt) == {"scalar_index", "capacity_index", "scalars", "partitions"}, "browser heatmap receipt missing")
    mounted = value.get("filesystem_mount")
    require(isinstance(mounted, dict) and receipt["capacity_index"] == mounted.get("capacity_index"), "browser heatmap and mount table used different observations")
    sources = []
    for field, family in (("scalar_index", METRICS), ("capacity_index", FILESYSTEM_METRICS)):
        index = receipt[field]
        require(type(index) is int and 0 <= index < len(rows), "browser heatmap source reference invalid")
        source = rows[index]
        require(source["path"] == OBSERVATIONS and set(source["query"]["metrics"].split(",")) == set(family), "browser heatmap source family mismatch")
        sources.append(source["value"])
    def check_cell(cell, point):
        require(isinstance(cell, dict) and set(cell) == {"data_state", "value", "text", "neutral"}
                and cell["data_state"] == point["data_state"], "browser heatmap data state mismatch")
        number = point["value"] if point["data_state"] == "valid" else None
        require(cell["neutral"] is (number is None), "browser heatmap colored missing data")
        if number is None:
            require(cell["value"] is None and cell["text"] == "—", "browser heatmap replaced missing data with a value")
        else:
            require(type(cell["value"]) in (int, float) and cell["value"] == number and isinstance(cell["text"], str), "browser heatmap progress differs from API")
            match = re.fullmatch(r"([0-9][0-9,]*\.[0-9]{2}) %", cell["text"])
            require(match is not None and abs(float(match[1].replace(",", ""))-number) <= 0.005000001, "browser heatmap percentage differs from API")
    keys = {"node.cpu.busy_percent", "node.memory.used_percent"}
    require(isinstance(receipt["scalars"], dict) and set(receipt["scalars"]) == keys, "browser heatmap scalar scope mismatch")
    for series in sources[0]["series"]:
        if series["metric_key"] in keys: check_cell(receipt["scalars"][series["metric_key"]], series["points"][0])
    mounts, partitions = filesystem_mount_groups([sources[1]]), receipt["partitions"]
    require(isinstance(partitions, list) and len(partitions) == len(mounts), "browser heatmap mount count mismatch")
    for (key, metrics), partition in zip(mounts.items(), partitions):
        require(isinstance(partition, dict) and set(partition) == {"dimensions", "data_state", "value", "text", "neutral"}
                and partition["dimensions"] == dict(zip(("device", "mountpoint", "fstype"), key)), "browser heatmap mount identity mismatch")
        check_cell({name: partition[name] for name in ("data_state", "value", "text", "neutral")}, metrics["node.filesystem.used_percent"])
    return {"existing_observations": True, "scalar_count": len(keys), "mount_count": len(mounts), "values_and_progress": True}


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
    require(isinstance(summaries, list) and 2 <= len(summaries) <= 8, "browser list summary missing or unbounded")
    for row in summaries:
        require(row.get("path") == SUMMARIES and row.get("query") == {"node_ids": expected["node"]["node_id"], "metrics": ",".join(HOST_LIST_METRICS)}, "browser summary scope mismatch")
        data = row.get("value", {}).get("data")
        require(isinstance(data, list) and len(data) == 1, "browser summary batch incomplete")
        assert_resources(data[0], expected["node"], expected["target"], keys=HOST_LIST_METRICS)
        require(data[0]["policy_version"] == expected["policy_version"], "browser summary ignored current budget")
    automatic = value.get("host_list_auto_refresh", {})
    require(all(automatic.get(key) is True for key in ("natural_timer", "server_end_advanced", "unchanged_url", "iframe_preserved", "off_restored", "off_no_requests")), "host list auto refresh lifecycle evidence missing")
    before, after = automatic.get("before_index"), automatic.get("after_index")
    require(type(before) is int and type(after) is int and 0 <= before < after < len(summaries)
            and after == before + 1, "host list auto refresh batch references invalid")
    require(utc_timestamp(summaries[after]["value"]["data"][0]["end"]) > utc_timestamp(summaries[before]["value"]["data"][0]["end"]), "host list automatic server window did not advance")
    rows = value.get("resources")
    # Original 31 six-read rounds plus four list observations: capacity/inodes on open and refresh.
    require(isinstance(rows, list) and 8 <= len(rows) <= 190, "browser resource evidence missing or unbounded")
    comparison_indices, comparison = validate_resource_comparison_browser(value, expected, artifacts)
    instants, trends = [], []
    for row_index, row in enumerate(rows):
        query, resource = row.get("query", {}), row.get("value", {})
        require(query.get("node_id") == expected["node"]["node_id"], "browser requested another node")
        trend = row.get("path") == TRENDS
        allowed_query = {"node_id", "metrics", "start", "end", "device", "mountpoint", "fstype"} if trend else {"node_id", "metrics"}
        require(set(query) <= allowed_query, "browser query contains unapproved input")
        require(trend or row.get("path") == OBSERVATIONS, "browser resource path mismatch")
        keys = query.get("metrics", "").split(",")
        require((trend and len(keys) == 1 and keys[0] in {"node.tcp.current_connections", "node.memory.used_percent", "node.cpu.busy_percent", "node.filesystem.used_percent", "node.filesystem.inodes_used_percent", *DISK_METRICS, *NETWORK_METRICS})
                or (not trend and set(keys) in (set(METRICS), set(FILESYSTEM_METRICS), set(INODE_METRICS), set(DISK_METRICS), set(NETWORK_METRICS)) and len(keys) == len(set(keys))), "browser metric request mismatch")
        duration = utc_timestamp(query.get("end")) - utc_timestamp(query.get("start")) if trend else 0
        require(not trend or duration in ({600} if row_index in comparison_indices else {300, 3600}), "browser range mismatch")
        dims = {key: query[key] for key in ("device", "mountpoint", "fstype") if key in query}
        required_dimensions = ({"device"} if keys[0] in DISK_METRICS or keys[0] in NETWORK_METRICS else {"device", "mountpoint", "fstype"} if keys[0].startswith("node.filesystem.") else set()) if trend else set()
        require(set(dims) == required_dimensions, "browser selector mismatch")
        assert_resources(resource, expected["node"], expected["target"], trend, keys, range_seconds=duration, dimensions=dims, require_fresh=trend or set(keys) not in (set(INODE_METRICS), set(DISK_METRICS), set(NETWORK_METRICS)))
        if not trend and set(keys) == set(INODE_METRICS): require(fresh_resource_groups(resource, INODE_METRICS), "browser inode table has no supported fresh mount")
        require(resource["policy_version"] == expected["policy_version"], "browser ignored current query budget")
        if trend:
            end = utc_timestamp(query.get("end"))
            require(end == utc_timestamp(resource["end"])
                    and any(utc_timestamp(item["end"]) == end for item in instants), "browser trend lacks server anchor")
            trends.append((keys[0], duration))
        else:
            instants.append(resource)
    # Each counter frame retains strict state validation; the run must witness a complete device for each family.
    for family in DEVICE_RATE_FAMILIES:
        require(any(set(row["metric_key"] for row in item["series"]) == set(family)
                    and fresh_resource_groups(item, family) for item in instants), "browser counter family has no fresh device: " + ",".join(family))
    require(any(set(row["metric_key"] for row in item["series"]) == set(FILESYSTEM_METRICS) for item in instants), "browser mount table evidence missing")
    require(any(set(row["metric_key"] for row in item["series"]) == set(INODE_METRICS) for item in instants), "browser inode table evidence missing")
    require(any(set(row["metric_key"] for row in item["series"]) == set(DISK_METRICS) for item in instants), "browser disk table evidence missing")
    require(any(set(row["metric_key"] for row in item["series"]) == set(NETWORK_METRICS) for item in instants), "browser network table evidence missing")
    require(value.get("presentation") == {"cpu_idle_complement": True, "iec_capacity": True, "elapsed_uptime": True, "system_load_count": True, "disk_rate_units": True, "network_rate_units": True, "disk_timing_units": True}, "browser quantity display evidence missing")
    require(len(instants) >= 2 and {("node.tcp.current_connections", 300), ("node.memory.used_percent", 3600), ("node.cpu.busy_percent", 300), ("node.filesystem.used_percent", 300), ("node.filesystem.inodes_used_percent", 300), *((key, 300) for key in ALL_METRICS if key in DISK_METRICS or key in NETWORK_METRICS)} <= set(trends), "browser restore evidence incomplete")
    mounted = validate_filesystem_mount_browser(value)
    mounted_list = validate_filesystem_list_browser(value)
    heatmap = validate_resource_heatmap_browser(value)
    for name in ("list", "detail", "restored", "filesystem", "inodes", "disks", "networks", "tcp", "list-filesystem", "heatmap"):
        screenshot = artifacts / ("node-resources-" + name + ".png")
        require(screenshot.is_file() and screenshot.stat().st_size > 1000
                and screenshot.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n", "browser screenshot missing or invalid")
    return {"result": "passed", "password_mfa": True, "same_platform_identity": True,
            "catalog_metric_count": len(ALL_METRICS), "trend_server_window": True, "navigation_restore": True, "host_list_auto_refresh": True,
            "security_administrator_denied": True, "auto_refresh": True, "disk_trends": True, "network_trends": True, "disk_timing_trends": True, "quantity_display": True,
            "filesystem_mount": mounted, "filesystem_list": mounted_list, "heatmap": heatmap, "period_comparison": comparison}


def assert_process_trend_evidence(trend, owner, policy_version, *, require_valid=True):
    query, reply = trend.get("query", {}), trend.get("value", {})
    require(trend.get("path") == "/api/v1/monitor/platform/process_resource_trends" and set(query) == {"instance_id", "start", "end"}, "process trend used unapproved input")
    subject = reply.get("subject", {})
    require(owner is not None and query["instance_id"] == str(owner["id"]) and subject == {"kind": "module_instance", **{k: owner[k] for k in ("id", "module_name", "instance_id", "role")}}, "process trend owner mismatch")
    start, end = utc_timestamp(reply.get("start")), utc_timestamp(reply.get("end"))
    step = reply.get("step_seconds")
    require(reply.get("node_id") == owner["node_id"] and reply.get("policy_version") == policy_version and reply.get("lookback_seconds") == 300 and end == utc_timestamp(query["end"]) and utc_timestamp(query["start"]) <= start <= end <= utc_timestamp(reply.get("queried_at")), "process trend window mismatch")
    require(start == min(end, max(utc_timestamp(query["start"]), math.ceil(instance_timestamp(owner["process_started_at"])))) and type(step) is int and step >= 15 and step % 15 == 0 and "collection" not in reply, "process trend crossed startup/budget boundary")
    lines = reply.get("series")
    require(isinstance(lines, list) and {s.get("metric_key") for s in lines} == {"process.cpu.core_equivalents", "process.memory.resident_bytes"} and len(lines) == 2, "process trend metric scope changed")
    count = int((end-start)//step) + 1
    require(1 <= count <= 1000, "process trend point budget bypass")
    complete = True
    for line in lines:
        unit, window = PROCESS_METRICS[line["metric_key"]]
        require(line.get("unit") == unit and line.get("window_seconds") == window and line.get("dimensions") == {} and len(line.get("points", [])) == count, "process trend series contract changed")
        valid = False
        for index, point in enumerate(line["points"]):
            at = utc_timestamp(point.get("evaluated_at"))
            require(at == start + index*step, "process trend grid mismatch")
            state = point.get("data_state")
            require(state in {"valid", "stale", "no_data", "not_active", "not_connected"}, "process trend state unknown")
            if state in {"valid", "stale"}:
                sample = utc_timestamp(point.get("sampled_at"))
                number = point.get("value")
                require(type(number) in {int, float} and math.isfinite(number) and number >= 0 and sample <= at and ((at-sample <= 60) == (state == "valid")), "process trend sample invalid")
                valid |= state == "valid"
            else:
                require(point.get("value") is None and point.get("sampled_at") is None, "process trend fabricated missing sample")
        complete = complete and valid
        if require_valid: require(valid, "real process trend has no valid samples")
    return complete


def validate_process_browser(value, expected, artifacts):
    require(value.get("service_presentation") == {"cpu_cores": True, "rss_iec": True, "elapsed_uptime": True,
            "inline_hints": True, "sample_time_visible": True, "unbound_host": True, "url_restore": True,
            "iframe_preserved": True, "negative_no_reads": True}, "service browser presentation or denial evidence missing")
    batches = value.get("service_batches")
    require(isinstance(batches, list) and 2 <= len(batches) <= 6, "service browser batch evidence missing or unbounded")
    automatic = value.get("service_auto_refresh", {})
    flags = {key: automatic.get(key) for key in ("natural_timer", "server_end_advanced", "unchanged_url", "iframe_preserved", "off_restored", "off_no_requests")}
    require(all(flag is True for flag in flags.values()), "service auto refresh lifecycle evidence missing")
    auto_batches, auto_trends = automatic.get("batches"), automatic.get("trends")
    require(isinstance(auto_batches, list) and len(auto_batches) == 2 and isinstance(auto_trends, list) and len(auto_trends) == 2,
            "service auto refresh responses missing or unbounded")
    batches = batches + auto_batches
    wanted = {r["id"]: r for r in expected["processes"]}
    observed = set()
    for batch in batches:
        require(batch.get("path") == PROCESS_SUMMARIES and set(batch.get("query", {})) == {"instance_ids"}, "service browser used unapproved resource input")
        instances = batch.get("instances", {})
        rows = instances.get("data")
        require(isinstance(rows, list) and 1 <= len(rows) <= 20 and instances.get("page") == 1 and instances.get("page_size") == 20
                and type(instances.get("total")) is int and instances["total"] >= len(rows), "service browser list is not an authorized page")
        require(batch.get("instance_query") == {"page": "1", "page_size": "20", "role": "backend", "status": "up", "process_metrics_declared": "true"}, "service browser list filters not restored")
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
    require(value.get("process_trend_presentation") == {"cpu_cores": True, "rss_iec": True, "same_current_instance": True, "startup_bound": True, "url_restore": True}, "process trend presentation missing")
    trends = value.get("service_trends")
    require(isinstance(trends, list) and len(trends) == 2, "process trend restore evidence missing or unbounded")
    for trend in trends + auto_trends:
        query, reply = trend.get("query", {}), trend.get("value", {})
        require(trend.get("path") == "/api/v1/monitor/platform/process_resource_trends" and set(query) == {"instance_id", "start", "end"}, "process trend used unapproved input")
        owner = wanted.get(reply.get("subject", {}).get("id"))
        assert_process_trend_evidence(trend, owner, expected["policy_version"])
    require(utc_timestamp(auto_trends[1]["value"]["end"]) > utc_timestamp(auto_trends[0]["value"]["end"])
            and auto_trends[0]["value"].get("subject") == auto_trends[1]["value"].get("subject"), "service auto refresh did not advance the same instance")
    for batch, trend in zip(auto_batches, auto_trends):
        require(utc_timestamp(trend["value"]["end"]) == math.floor(utc_timestamp(batch["value"]["data"][0]["queried_at"])),
                "service auto refresh trend not anchored to its summary")
    for name in ("node-resources-process-trends.png", "node-resources-process-trends-memory.png"):
        trend_screenshot = artifacts / name
        require(trend_screenshot.is_file() and trend_screenshot.stat().st_size > 1000 and trend_screenshot.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n", "process trend screenshot missing")
    screenshot = artifacts / "node-resources-services.png"
    require(screenshot.is_file() and screenshot.stat().st_size > 1000 and screenshot.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n", "service browser screenshot missing or invalid")
    return {"result": "passed", "real_instance_batch_reads": len(batches), "both_current_instances": True, "units_and_inline_hints": True, "auto_refresh": True,
            "url_restore": True, "same_platform_identity": True, "security_administrator_denied": True}


def run_resource_browser(node, target, policy, display_name, admin, security, processes):
    artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
    for name in ("node-native-source.json", "node-resources-browser.json"):
        (artifacts / name).unlink(missing_ok=True)
    native_source = FIXTURE.native_node_source_proof()
    (artifacts / "node-native-source.json").write_text(json.dumps(native_source))
    expected = {"run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"], "node": node, "target": {"id": target["id"], "version": target["version"]},
                "display_name": display_name, "policy_version": policy["version"],
                "admin_id": admin["principal"]["id"], "security_id": security["principal"]["id"],
                "processes": [{k: r[k] for k in ("id", "instance_id", "module_name", "role", "node_id", "process_started_at")} for r in processes]}
    # Store only the subject/version facts needed by the page, never target TLS/credentials.
    expected["node"] = {"node_id": node["node_id"], "version": node["version"]}
    (artifacts / "node-resources-browser-input.json").write_text(json.dumps(expected))
    for name in ("list", "detail", "restored", "filesystem", "list-filesystem", "heatmap", "inodes", "disks", "networks", "services", "overview", "comparison-cpu", "comparison-memory", "comparison-restored"):
        (artifacts / ("node-resources-" + name + ".png")).unlink(missing_ok=True)
    value = execute_resource_browser(artifacts)
    result = validate_resource_browser(value, expected, artifacts)
    result["services"] = validate_process_browser(value, expected, artifacts)
    result["overview"] = validate_overview_browser(value, expected, artifacts)
    result["native_source"] = native_source
    return result


def execute_resource_browser(artifacts):
    evidence = artifacts / "node-resources-browser.json"
    evidence.unlink(missing_ok=True)
    started = time.monotonic()
    runtime = {"schema_version": "addp.node-resources-browser-runtime/v1", "result": "failed", "duration_ms": 0,
               "test_deadline_reported": False, "subprocess_deadline_exceeded": False, "subprocess_timeout_seconds": 300}
    try:
        result = subprocess.run(["npm", "exec", "--", "playwright", "test", "--config=playwright.online.config.js",
                                 "e2e/online/platform-node-resources.spec.js",
                                 "--output=" + str(Path(os.environ["ADDP_ONLINE_SECRET_DIR"]) / "node-resource-browser-output")], cwd=FIXTURE.ROOT / "console/frontend",
                                env=dict(os.environ), capture_output=True, text=True, timeout=runtime["subprocess_timeout_seconds"])
        runtime["result"] = "passed" if result.returncode == 0 else "failed"
        # Classify captured output in memory only; never export login assertions or exception payloads.
        runtime["test_deadline_reported"] = any(re.search(r"\bTest timeout of [0-9]+ms exceeded", re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", stream or "")) is not None
                                                  for stream in (result.stdout, result.stderr))
    except subprocess.TimeoutExpired:
        runtime["subprocess_deadline_exceeded"] = True
        raise SuiteError("resource browser subprocess exceeded existing deadline") from None
    finally:
        runtime["duration_ms"] = max(0, round((time.monotonic() - started) * 1000))
        (artifacts / "node-resources-browser-runtime.json").write_text(json.dumps(runtime))
    # Playwright may print assertions from login; never forward credential-bearing output.
    require(result.returncode == 0, "resource browser failed; inspect sanitized stage report")
    require(evidence.is_file(), "resource browser report missing")
    return json.loads(evidence.read_text())


def validate_host_admission_browser(value, expected, artifacts):
    require(value.get("schema_version") == "addp.node-resources-browser/v1" and value.get("result") == "passed"
            and value.get("stage") == "complete" and value.get("run_id") == expected["run_id"], "admission browser result mismatch")
    actor = value.get("identity", {})
    platform_identity(actor, "platform.system_administrator")
    require(actor["principal"].get("id") == expected["admin_id"], "admission browser changed administrator")
    detail = value.get("admission", {})
    host, target = detail.get("host", {}), detail.get("target", {})
    host_input = dict(expected["admission"]["host"], enabled=True)
    require(canonical_uuid(host.get("node_id")) and type(host.get("version")) is int and host["version"] == 1
            and all(host.get(k) == v for k, v in host_input.items()), "admission host differs from deployment declaration")
    body = dict(expected["admission"]["collection"], subject={"kind": "node", "node_id": host["node_id"]}, enabled=True)
    require(canonical_uuid(target.get("id")) and type(target.get("version")) is int and target["version"] == 1
            and all(target.get(k) == v for k, v in body.items()), "admission collection changed its created subject or source")
    rejected = dict(body, source=dict(body["source"], endpoint=body["source"]["endpoint"].removesuffix("/metrics")+"/rejected-path"))
    require(detail.get("writes") == [{"path": NODES, "input": host_input}, {"path": TARGETS, "input": rejected}, {"path": TARGETS, "input": body}],
            "admission retried host creation, changed scope or skipped real rejection")
    require(detail.get("failure") == {"status": 400, "error_code": "invalid_monitoring_target"} and detail.get("reloaded") is True,
            "admission partial failure or reload evidence missing")
    for name in ("node-admission-partial.png", "node-admission-saved.png"):
        screenshot = artifacts / name
        require(screenshot.is_file() and screenshot.stat().st_size > 1000 and screenshot.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n", "admission screenshot missing")
    return host, target


def assert_host_binding(instance, node_id, *, rejected=False, module_name="monitor", role="backend"):
    require(canonical_uuid(node_id) and instance.get("module_name") == module_name and instance.get("role") == role
            and instance.get("status") == "up" and instance.get("process_metrics_declared") is True
            and instance.get("declared_node_id") == node_id
            and instance.get("node_id") == ("" if rejected else node_id)
            and instance.get("node_binding_state") == ("rejected" if rejected else "bound")
            and instance.get("node_binding_reason") == ("node_disabled" if rejected else ""), "current process host binding differs from System verdict")


def validate_host_binding_browser(value, expected, artifacts):
    require(value.get("schema_version") == "addp.node-resources-browser/v1" and value.get("result") == "passed"
            and value.get("stage") == "complete" and value.get("run_id") == expected["run_id"], "host binding browser did not complete")
    platform_identity(value.get("identity", {}), "platform.system_administrator")
    require(value["identity"]["principal"].get("id") == expected["admin_id"], "host binding browser changed administrator")
    detail, host = value.get("binding", {}), expected["binding"]["host"]
    owner = expected["binding"]["instance"]
    require(detail.get("instance") == owner and detail.get("host") == host, "host binding browser changed subject")
    require(all(detail.get(key) is True for key in ("same_iframe", "back_restored", "reload_restored")), "host binding navigation did not restore")
    batch = detail.get("process_summary", {})
    require(batch.get("query") == {"instance_ids": str(owner["id"])}, "host binding process query expanded scope")
    assert_process_summaries(batch.get("value", {}), [owner])
    resources = detail.get("resources", {})
    require(resources.get("path") == "/api/v1/monitor/platform/resource_observations"
            and resources.get("query", {}).get("node_id") == host["node_id"]
            and set(resources["query"]) == {"node_id", "metrics"}, "host binding resource query changed subject")
    metrics = resources["query"]["metrics"].split(",")
    require(len(metrics) == len(METRICS) and set(metrics) == set(METRICS)
            and resources.get("value", {}).get("collection", {}).get("state") == "collecting", "host binding browser lacks complete scalar resource evidence")
    assert_resources(resources.get("value", {}), host, expected["binding"]["target"], keys=metrics)
    for name in ("node-binding-services.png", "node-binding-resources.png"):
        screenshot = artifacts / name
        require(screenshot.is_file() and screenshot.stat().st_size > 1000 and screenshot.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n", "host binding screenshot missing")
    return {"result": "passed", "same_instance": True, "current_host_name": True, "resources": True,
            "same_iframe": True, "back_restored": True, "reload_restored": True}


def run_host_binding_case(base, directory, admin, actor, host, target, report):
    require(FIXTURE.boundary() == directory, "host binding requires the owned Hosted deployment")
    report["stage"] = "new-process-host-binding"
    write_evidence(report)
    initial = current_processes(admin)
    old, peer = (next(row for row in initial if row["module_name"] == module) for module in ("monitor", "system"))
    monitor_lifecycle(directory, "stop")
    try:
        eventually(lambda: instance_list(admin, ids=str(old["id"]))[0]["status"] == "down", "old unbound process stop")
    finally:
        monitor_lifecycle(directory, "start", node_id=host["node_id"])
    current = instance_list(admin, module_name="monitor", role="backend", status="up")
    require(len(current) == 1, "host binding expected one current Monitor instance")
    new = current[0]
    require(new["id"] != old["id"] and new["instance_id"] != old["instance_id"]
            and instance_timestamp(new["process_started_at"]) > instance_timestamp(old["process_started_at"]), "host binding reused old process identity")
    assert_host_binding(new, host["node_id"])
    peer_now = instance_list(admin, ids=str(peer["id"]))[0]
    require(peer_now["instance_id"] == peer["instance_id"] and peer_now["status"] == "up"
            and peer_now["node_id"] == peer_now.get("declared_node_id") == "", "host binding changed peer process")
    old_now = instance_list(admin, ids=str(old["id"]))[0]
    require(old_now["node_id"] == old_now.get("declared_node_id") == "" and old_now["status"] == "down", "host binding rewrote old declaration")
    evidence = {}
    eventually(lambda: process_query_ready(admin, [old_now, new, peer_now], inactive=[old["id"]], evidence=evidence),
               "bound process full-window resources and old identity exclusion", timeout=150)
    snapshots = {"bound": new, "old": old_now, "peer": peer_now, "process_resources": evidence}
    node_input = {k: host[k] for k in ("display_name", "node_kind", "addresses", "allowed_module_bindings")}
    for disabled in (True, False):
        previous_version = host["version"]
        host = admin.request("PUT", NODES+"/"+host["node_id"], (200,), dict(node_input, enabled=not disabled, version=host["version"])).payload
        require(host.get("enabled") is not disabled and host.get("node_id") == new["declared_node_id"]
                and host.get("version") == previous_version+1, "host lifecycle changed binding subject or CAS version")
        same = instance_list(admin, ids=str(new["id"]))[0]
        require(same["instance_id"] == new["instance_id"], "host state change restarted the process")
        assert_host_binding(same, host["node_id"], rejected=disabled)
        snapshot = admin.request("GET", process_path([same]), (200,)).payload
        assert_process_summaries(snapshot, [same])
        API.GatewayClient(os.environ["MONITOR_URL"], "", 10).request("GET", "/health/ready", (200,))
        snapshots["disabled" if disabled else "restored"] = {"instance": same, "resources": snapshot}
        new = same
    artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
    facts = {k: new[k] for k in ("id", "instance_id", "module_name", "role", "node_id", "process_started_at")}
    expected = {"run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"], "admin_id": actor["principal"]["id"],
                "binding": {"host": {k: host[k] for k in ("node_id", "version", "display_name")},
                            "target": {"id": target["id"], "version": target["version"]}, "instance": facts}}
    (artifacts / "node-resources-browser-input.json").write_text(json.dumps(expected))
    report["stage"] = "bound-service-host-navigation"
    write_evidence(report)
    browser = execute_resource_browser(artifacts)
    snapshots["browser"] = validate_host_binding_browser(browser, expected, artifacts)
    snapshots["result"] = "passed"
    report["host_binding"] = snapshots
    return host



ENGINE_DEPLOYMENTS = "/api/v1/system/platform/engine_deployments"
ENGINE_OBSERVATIONS = "/api/v1/monitor/platform/engine_resource_observations"
ENGINE_SUMMARIES = "/api/v1/monitor/platform/engine_resource_summaries"
ENGINE_TRENDS = "/api/v1/monitor/platform/engine_resource_trends"
ENGINE_METRICS = {"postgresql.connections."+key: ("connections", 0) for key in
                  ("total", "active", "idle", "idle_in_transaction", "waiting", "lock_waiting", "io_waiting", "maximum")}
ENGINE_METRICS.update({"postgresql.transactions."+key+"_per_second": ("transactions_per_second", 60) for key in ("committed", "rolled_back")})
ENGINE_METRICS.update({"postgresql.deadlocks.per_second": ("events_per_second", 60), "postgresql.database.total_bytes": ("bytes", 0)})
ENGINE_SUMMARY_KEYS = {"postgresql.connections.total", "postgresql.connections.active", "postgresql.transactions.committed_per_second", "postgresql.database.total_bytes"}


def assert_engine_resources(value, owners, *, keys=None, trend=False, fresh=False, absent=False):
    keys = set(keys or ENGINE_METRICS)
    require(isinstance(value, dict) and isinstance(value.get("data"), list) and len(value["data"]) == len(owners), "engine response scope differs")
    start, end, queried = (utc_timestamp(value.get(key)) for key in ("start", "end", "queried_at"))
    step = value.get("step_seconds")
    require(type(step) is int and step >= 15 and step % 15 == 0 and start <= end <= queried
            and (trend or start == end) and type(value.get("policy_version")) is int
            and value["policy_version"] >= 0 and value.get("lookback_seconds") == 300, "engine query metadata differs")
    count = int((end-start)//step)+1
    require(1 <= count <= 1000 and count*len(owners)*len(keys) <= 20000, "engine query exceeds declared budget")
    seen = set()
    for row in value["data"]:
        identifier = row.get("subject", {}).get("deployment_id")
        owner = next((item for item in owners if item["deployment_id"] == identifier), None)
        require(owner is not None and identifier not in seen, "engine response contains another deployment")
        seen.add(identifier)
        require(row["subject"] == {"kind":"engine_deployment", "deployment_id":identifier, "display_name":owner["display_name"], "engine_type":"postgresql"}
                and row.get("node_id") == owner["node_id"] and set(row) == {"subject", "node_id", "collection", "series"}, "engine subject changed or private fields escaped")
        require(isinstance(row.get("series"), list) and len(row["series"]) == len(keys)
                and {series.get("metric_key") for series in row["series"]} == keys, "engine fixed catalog differs")
        state = row.get("collection", {}).get("state")
        if fresh: require(state == "collecting", "engine snapshot is not collecting")
        if absent: require(state in {"not_active", "not_connected", "source_unavailable"}, "inactive engine remained connected")
        container_start = utc_timestamp(owner["inventory"]["services"][0]["containers"][0]["started_at"])
        if trend and not absent: require(start >= math.ceil(container_start) and end-start <= 7*86400, "engine trend includes a previous container run")
        for series in row["series"]:
            unit, window = ENGINE_METRICS[series["metric_key"]]
            require(series.get("unit") == unit and series.get("window_seconds") == window and series.get("dimensions") == {}
                    and isinstance(series.get("points"), list) and len(series["points"]) == count, "engine series contract differs")
            for index, point in enumerate(series["points"]):
                require(utc_timestamp(point.get("evaluated_at")) == start+index*step, "engine evaluation grid differs")
                status, number, sample = (point.get(key) for key in ("data_state", "value", "sampled_at"))
                if absent or state in {"not_active", "not_connected", "source_unavailable"}:
                    require(status in {"not_active", "not_connected", "source_unavailable"} and number is None and sample is None, "inactive engine reused a value")
                elif status == "valid":
                    require(type(number) in (int, float) and math.isfinite(number) and number >= 0
                            and (unit != "connections" or int(number) == number)
                            and 0 <= start+index*step-utc_timestamp(sample) <= 60, "engine value or sample is invalid")
                else:
                    require(not fresh or trend, "engine current window is incomplete")
                    require(status in {"no_data", "no_sample", "stale"}, "engine missing state differs")
                    if status != "stale": require(number is None and sample is None, "engine missing value fabricated")
                    else: require(type(number) in (int, float) and math.isfinite(number) and number >= 0 and start+index*step-utc_timestamp(sample) > 60, "stale engine point is invalid")
            if trend and not absent: require(any(point["data_state"] == "valid" for point in series["points"]), "engine trend contains no valid sample")
    return value


def engine_query_path(identifier, trend=False):
    params = {"deployment_id": identifier, "metrics": ",".join(ENGINE_METRICS)}
    if trend:
        end = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)
        params.update(metrics="postgresql.transactions.committed_per_second", start=(end-datetime.timedelta(minutes=5)).isoformat().replace("+00:00", "Z"), end=end.isoformat().replace("+00:00", "Z"))
    return (ENGINE_TRENDS if trend else ENGINE_OBSERVATIONS)+"?"+urllib.parse.urlencode(params)


def assert_engine_cleared(admin, owner):
    identifier = owner["deployment_id"]
    assert_engine_resources(admin.request("GET", engine_query_path(identifier), (200,)).payload, [owner], absent=True)
    assert_engine_resources(admin.request("GET", ENGINE_SUMMARIES+"?deployment_ids="+identifier, (200,)).payload, [owner], keys=ENGINE_SUMMARY_KEYS, absent=True)
    assert_engine_resources(admin.request("GET", engine_query_path(identifier, True), (200,)).payload, [owner], keys={"postgresql.transactions.committed_per_second"}, trend=True, absent=True)


def wait_engine_samples(admin, owners):
    # SD 30s + scrape 15s + SQL 5s + complete 60s window + margin.
    deadline = time.monotonic()+130
    while True:
        values = [assert_engine_resources(admin.request("GET", engine_query_path(row["deployment_id"]), (200,)).payload, [row]) for row in owners]
        if all(point.get("data_state") == "valid" for value in values for row in value["data"] for series in row["series"] for point in series["points"]):
            for value, owner in zip(values, owners): assert_engine_resources(value, [owner], fresh=True)
            return values
        require(time.monotonic() < deadline, "PostgreSQL complete sampling window did not converge")
        time.sleep(3)


def run_engine_professional_case(base, directory, admin, actor, denied, owners, report):
    report["stage"] = "engine-professional-current-container-queries"; write_evidence(report)
    values = wait_engine_samples(admin, owners)
    summary_path = ENGINE_SUMMARIES+"?"+urllib.parse.urlencode({"deployment_ids":",".join(row["deployment_id"] for row in owners)})
    summary = assert_engine_resources(admin.request("GET", summary_path, (200,)).payload, owners, keys=ENGINE_SUMMARY_KEYS, fresh=True)
    trends = [assert_engine_resources(admin.request("GET", engine_query_path(row["deployment_id"], True), (200,)).payload, [row], keys={"postgresql.transactions.committed_per_second"}, trend=True) for row in owners]
    for client in [*denied, API.GatewayClient(base, "", 10)]:
        for path in (engine_query_path(owners[0]["deployment_id"]), summary_path, engine_query_path(owners[0]["deployment_id"], True)):
            client.request("GET", path, (403,) if client.token else (401,))
    artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
    security_id = denied[0].request("GET", "/api/v1/system/auth/context", (200,)).payload["principal"]["id"]
    expected = {"run_id":os.environ["ADDP_ONLINE_TEST_RUN_ID"], "admin_id":actor["principal"]["id"], "security_id":security_id, "engine_resources":{"owners":owners}}
    (artifacts/"node-resources-browser-input.json").write_text(json.dumps(expected))
    report["stage"] = "engine-professional-console"; write_evidence(report)
    browser = execute_resource_browser(artifacts)
    validate_engine_professional_browser(browser, expected, artifacts)
    admin = renew_admin(base, actor["principal"]["id"])
    report["stage"] = "engine-professional-source-stop-and-recovery"; write_evidence(report)
    business = next(row for row in owners if row["inventory"]["source"]["project"] == "business")
    try:
        FIXTURE.engine_source_action(directory, "engine-metrics-pause")
        def stopped():
            value = admin.request("GET", engine_query_path(business["deployment_id"]), (200,)).payload
            if value.get("data", [{}])[0].get("collection", {}).get("state") not in {"not_connected", "source_unavailable"}: return False
            assert_engine_resources(value, [business], absent=True)
            assert_engine_cleared(admin, business)
            return True
        eventually(stopped, "stopped PostgreSQL source retained current values")
    finally:
        FIXTURE.engine_source_action(directory, "engine-metrics-resume")
    wait_engine_samples(admin, owners)
    report["engine_resources"] = {"result":"passed", "deployments":2, "instant_metrics":12, "summary_metrics":4,
                                  "current_container_trends":True, "source_stop_clears":True, "source_restored":True,
                                  "identity_denials":True, "browser":True}
    return admin


def validate_engine_professional_browser(value, expected, artifacts):
    require(isinstance(value, dict) and value.get("schema_version") == "addp.node-resources-browser/v1"
            and value.get("result") == "passed" and value.get("stage") == "complete" and value.get("run_id") == expected["run_id"], "engine professional browser result differs")
    platform_identity(value.get("identity", {}), "platform.system_administrator")
    require(value["identity"]["principal"]["id"] == expected["admin_id"], "engine professional browser changed identity")
    platform_identity(value.get("negative_identity", {}), "platform.security_administrator")
    require(value["negative_identity"]["principal"]["id"] == expected["security_id"], "engine professional browser changed negative identity")
    detail = value.get("engine_resources", {})
    require(detail.get("reloaded") is True and detail.get("zero_and_units") is True and detail.get("denied") is True, "engine professional browser assertions missing")
    owners = expected["engine_resources"]["owners"]
    reads = detail.get("reads", [])
    require(isinstance(reads, list) and len(reads) >= 5, "engine professional browser reads missing")
    covered = set()
    for item in reads:
        require(item.get("status") == 200, "browser professional query failed")
        path, query = item.get("path"), item.get("query", {})
        if path == ENGINE_SUMMARIES:
            require(set(query.get("deployment_ids", "").split(",")) == {row["deployment_id"] for row in owners}, "browser summary scope differs")
            assert_engine_resources(item.get("value"), owners, keys=ENGINE_SUMMARY_KEYS, fresh=True)
            covered.add((path, "all"))
        else:
            owner = next((row for row in owners if row["deployment_id"] == query.get("deployment_id")), None)
            require(owner is not None and path in {ENGINE_OBSERVATIONS, ENGINE_TRENDS}, "browser read another engine")
            trend = path == ENGINE_TRENDS
            assert_engine_resources(item.get("value"), [owner], keys={"postgresql.transactions.committed_per_second"} if trend else None, trend=trend, fresh=not trend)
            covered.add((path, owner["deployment_id"]))
    require((ENGINE_SUMMARIES, "all") in covered and all((path, row["deployment_id"]) in covered for row in owners for path in (ENGINE_OBSERVATIONS, ENGINE_TRENDS)), "browser missed a professional endpoint or deployment")
    for name in ("engine-resources-list.png", "engine-resources-detail.png"):
        path = artifacts/name
        require(path.is_file() and path.read_bytes().startswith(b"\x89PNG\r\n\x1a\n"), "engine professional screenshot missing")


def engine_candidates(admin):
    deadline=time.monotonic()+90
    while True:
        value=admin.request("GET",ENGINE_DEPLOYMENTS+"/discovery",(200,)).payload
        require(isinstance(value,dict) and isinstance(value.get("scopes"),list) and isinstance(value.get("candidates"),list) and all(isinstance(row,dict) for row in value["candidates"]),"engine discovery envelope differs")
        candidates=[row for row in value["candidates"] if row.get("service")=="postgres" and row.get("eligible") is True]
        if len(candidates)==2 and {row.get("scope") for row in candidates}=={"infra","business"}:
            require(all(re.fullmatch(r"[a-f0-9]{64}",row.get("fingerprint","")) and not row.get("deployment_id") for row in candidates),"engine candidate is already registered or malformed")
            return value, sorted(candidates,key=lambda row:row["scope"])
        require(time.monotonic()<deadline,"both owned PostgreSQL sources did not become available")
        time.sleep(2)


def validate_engine_admission_browser(value,expected,artifacts):
    require(isinstance(value,dict) and value.get("schema_version")=="addp.node-resources-browser/v1" and value.get("result")=="passed" and value.get("stage")=="complete" and value.get("run_id")==expected["run_id"],"engine browser result mismatch")
    actor=value.get("identity",{}); platform_identity(actor,"platform.system_administrator")
    require(actor["principal"]["id"]==expected["admin_id"],"engine browser changed administrator")
    detail=value.get("engine_admission",{})
    require(isinstance(detail,dict) and detail.get("reloaded") is True and detail.get("registered_disabled") is True,"engine browser reload or registered guard missing")
    writes=detail.get("writes",[])
    require(isinstance(writes,list) and len(writes)==1 and isinstance(writes[0],dict) and set(writes[0])=={"items"} and isinstance(writes[0]["items"],list) and len(writes[0]["items"])==2 and all(isinstance(row,dict) for row in writes[0]["items"]),"engine browser did not submit one bounded batch")
    items=sorted(writes[0]["items"],key=lambda row:row.get("scope",""))
    wanted=sorted(expected["engine_admission"]["items"],key=lambda row:row["scope"])
    require(items==wanted,"engine browser changed the explicit source or host confirmations")
    created=detail.get("created",[])
    require(isinstance(created,list) and len(created)==2 and all(isinstance(row,dict) and isinstance(row.get("inventory"),dict) and isinstance(row["inventory"].get("source"),dict) for row in created) and len({row.get("deployment_id") for row in created})==2,"engine browser deployment identities differ")
    for row,item in zip(sorted(created,key=lambda row:row.get("inventory",{}).get("source",{}).get("project","")),sorted(wanted,key=lambda item: "addp-infra" if item["scope"]=="infra" else "business")):
        require(canonical_uuid(row.get("deployment_id")) and type(row.get("version")) is int and row["version"]==1
                and row.get("enabled") is True and row.get("engine_type")=="postgresql" and row.get("display_name")==item["display_name"] and row.get("declared_node_id")==item["declared_node_id"]
                and row.get("node_id")==item["declared_node_id"] and row.get("host_binding_state")=="bound"
                and row.get("inventory",{}).get("schema")=="addp.engine-deployment-inventory/v1"
                and row["inventory"]["source"].get("project")==("addp-infra" if item["scope"]=="infra" else "business"),"engine admission receipt changed")
    sources=detail.get("current_sources",[])
    require(isinstance(sources,list) and len(sources)==2 and all(isinstance(source,dict) and set(source)=={"deployment_id","container_id"} and canonical_uuid(source["deployment_id"]) and isinstance(source["container_id"],str) for source in sources),"engine browser current sources missing")
    wanted_sources=[]
    for row in created:
        services=row["inventory"].get("services",[])
        require(isinstance(services,list) and len(services)==1 and isinstance(services[0],dict) and isinstance(services[0].get("containers"),list) and len(services[0]["containers"])==1 and isinstance(services[0]["containers"][0],dict),"engine receipt current container missing")
        container_id=services[0]["containers"][0].get("container_id")
        require(isinstance(container_id,str) and re.fullmatch(r"[0-9a-f]{64}",container_id) is not None,"engine receipt container identity invalid")
        wanted_sources.append({"deployment_id":row["deployment_id"],"container_id":container_id})
    require(sorted(sources,key=lambda source:source.get("deployment_id",""))==sorted(wanted_sources,key=lambda source:source["deployment_id"]),"engine browser current source identity changed")
    for name in ("engine-admission-saved.png","engine-admission-restored.png"):
        path=artifacts/name
        require(path.is_file() and path.read_bytes().startswith(b"\x89PNG\r\n\x1a\n"),"engine browser screenshot missing")
    return created


def run_engine_admission_case(base,directory,admin,actor,denied,report):
    require(FIXTURE.boundary()==directory,"engine admission requires owned Hosted deployment")
    artifacts=Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
    report["stage"]="engine-source-discovery"; write_evidence(report)
    require(admin.request("GET",ENGINE_DEPLOYMENTS,(200,)).payload.get("total")==0,"engine inventory is not empty")
    discovery,candidates=engine_candidates(admin)
    node_input={"display_name":"Online engine host "+os.environ["ADDP_ONLINE_TEST_RUN_ID"],"node_kind":"virtual","addresses":[],"enabled":True,"allowed_module_bindings":[]}
    host=admin.request("POST",NODES,(201,),node_input).payload
    require(canonical_uuid(host.get("node_id")) and host.get("version")==1,"engine host creation differs")
    items=[{k:row[k] for k in ("scope","service","fingerprint")} | {"display_name":"Online "+row["scope"]+" PostgreSQL "+os.environ["ADDP_ONLINE_TEST_RUN_ID"],"enabled":True,"declared_node_id":host["node_id"]} for row in candidates]
    report["stage"]="engine-batch-rejection-and-rollback"; write_evidence(report)
    invalid=[dict(row) for row in items]; next(row for row in invalid if row["scope"]=="business")["declared_node_id"]=str(uuid.uuid4())
    failure=admin.request("POST",ENGINE_DEPLOYMENTS,(409,),{"items":invalid}).payload
    require(failure.get("error_code")=="engine_deployment_host_unavailable","engine second-member rejection differs")
    require(admin.request("GET",ENGINE_DEPLOYMENTS,(200,)).payload.get("total")==0,"failed batch created an engine identity")
    admin.request("POST",ENGINE_DEPLOYMENTS,(400,),{"items":[items[0],items[0]]})
    for client in [*denied,API.GatewayClient(base,"",10)]:
        expected_status=(401,) if not client.token else (403,)
        for path in (ENGINE_DEPLOYMENTS,ENGINE_DEPLOYMENTS+"/discovery"):
            client.request("GET",path,expected_status)
        client.request("POST",ENGINE_DEPLOYMENTS,expected_status,{"items":items})
    report["stage"]="engine-real-source-change"; write_evidence(report)
    FIXTURE.engine_source_action(directory,"engine-restart")
    FIXTURE.engine_source_action(directory,"engine-metrics-up")
    changed,new_candidates=engine_candidates(admin)
    old={row["scope"]:row["fingerprint"] for row in items}; new={row["scope"]:row["fingerprint"] for row in new_candidates}
    require(old["business"]!=new["business"] and old["infra"]==new["infra"],"actual Business restart did not change only its source facts")
    failure=admin.request("POST",ENGINE_DEPLOYMENTS,(409,),{"items":items}).payload
    require(failure.get("error_code")=="engine_deployment_source_changed","stale source was not rejected")
    require(admin.request("GET",ENGINE_DEPLOYMENTS,(200,)).payload.get("total")==0,"stale confirmation created an identity")
    for row in items: row["fingerprint"]=new[row["scope"]]
    expected={"run_id":os.environ["ADDP_ONLINE_TEST_RUN_ID"],"admin_id":actor["principal"]["id"],"engine_admission":{"items":items,"host":host}}
    (artifacts/"node-resources-browser-input.json").write_text(json.dumps(expected))
    report["stage"]="engine-console-batch-admission"; write_evidence(report)
    browser=execute_resource_browser(artifacts)
    created=validate_engine_admission_browser(browser,expected,artifacts)
    admin=renew_admin(base,actor["principal"]["id"])
    require(admin.request("GET",ENGINE_DEPLOYMENTS,(200,)).payload.get("total")==2,"engine batch did not create exactly two roots")
    admin = run_engine_professional_case(base, directory, admin, actor, denied, created, report)
    report["stage"]="engine-owner-read-cas-and-retirement"; write_evidence(report)
    for receipt in created:
        path=ENGINE_DEPLOYMENTS+"/"+receipt["deployment_id"]
        stored=admin.request("GET",path,(200,)).payload
        require(all(stored.get(k)==receipt.get(k) for k in ("deployment_id","display_name","inventory","declared_node_id","version")),"engine browser receipt differs from Owner")
        body={"display_name":stored["display_name"]+" verified","enabled":True,"declared_node_id":host["node_id"],"version":1}
        updated=admin.request("PUT",path,(200,),body).payload
        require(updated.get("version")==2 and updated.get("display_name")==body["display_name"] and updated.get("inventory")==stored["inventory"],"engine management changed its source or did not advance version")
        admin.request("PUT",path,(409,),body)
        for client in denied:
            client.request("GET",path,(403,)); client.request("PUT",path,(403,),dict(body,version=2))
        unchanged=admin.request("GET",path,(200,)).payload
        require(all(unchanged.get(k)==updated.get(k) for k in ("version","display_name","inventory","declared_node_id","enabled")),"rejected engine writes changed current state")
        retired=admin.request("PUT",path,(200,),dict(body,enabled=False,version=2)).payload
        require(retired.get("version")==3 and retired.get("host_binding_reason")=="deployment_disabled","engine retirement differs")
        assert_engine_cleared(admin, retired)
    final_host=admin.request("PUT",NODES+"/"+host["node_id"],(200,),dict(node_input,enabled=False,version=1)).payload
    require(final_host.get("version")==2 and final_host.get("enabled") is False,"engine host was not retired")
    report.update(engine_admission={"result":"passed","batch_size":2,"failed_batch_no_roots":True,"source_changed":True,"duplicate_rejected":True,"browser_reload":True,"owner_reads":True,"cas":True,"retired":True},stage="complete",result="passed")
    write_evidence(report)

def run_host_admission_case(base, directory, admin_identity, report):
    require(FIXTURE.boundary() == directory, "host admission requires the owned Hosted deployment")
    artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
    name = "Online admission "+os.environ["ADDP_ONLINE_TEST_RUN_ID"]
    manifest = json.loads(subprocess.check_output(["python3", str(FIXTURE.ROOT / "scripts/infra/node-metrics.py"), "export-admission",
                                                  "--node-kind", "virtual", "--display-name", name,
                                                  "--address", os.environ["ADDP_ONLINE_METRICS_NODE_IP"], "--module", "monitor"], text=True, timeout=30))
    require(manifest == {"schema": "addp.host-admission/v2", "host": {"display_name": name, "node_kind": "virtual",
                "addresses": [os.environ["ADDP_ONLINE_METRICS_NODE_IP"]], "allowed_module_bindings": [{"module_name": "monitor", "client_id": "addp-monitor"}]},
                "collection": {"monitor_kind": "host_resources", "source": {"type": "node_exporter",
                "endpoint": "https://"+os.environ["ADDP_ONLINE_METRICS_NODE_IP"]+":19100/metrics"}}}, "deployment manifest differs from the owned source")
    expected = {"run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"], "admin_id": admin_identity["principal"]["id"], "admission": manifest}
    (artifacts / "node-resources-browser-input.json").write_text(json.dumps(expected))
    report["stage"] = "console-host-admission"
    write_evidence(report)
    value = execute_resource_browser(artifacts)
    host, target = validate_host_admission_browser(value, expected, artifacts)
    admin = renew_admin(base, admin_identity["principal"]["id"])
    stored_host = admin.request("GET", NODES+"/"+host["node_id"], (200,)).payload
    stored_target = admin.request("GET", TARGETS+"/"+target["id"], (200,)).payload
    require(all(stored_host.get(k) == v for k, v in host.items()) and all(stored_target.get(k) == v for k, v in target.items()),
            "admission browser receipts differ from the two Owners")
    hosts = admin.request("GET", NODES, (200,)).payload
    targets = admin.request("GET", TARGETS, (200,)).payload
    require(hosts.get("total") == targets.get("total") == 1 and len(hosts.get("data", [])) == len(targets.get("data", [])) == 1
            and hosts["data"][0].get("node_id") == host["node_id"] and targets["data"][0].get("id") == target["id"],
            "admission left duplicate or unexpected managed objects")
    report.update(node_id=host["node_id"], target_id=target["id"], saved_version=target["version"],
                  host_admission={"result": "passed", "host_posts": 1, "target_posts": 2, "real_rejection": True,
                                  "same_subject_retry": True, "reload": True, "owner_reads": True, "exact_inventory": True})
    (artifacts / "node-admission-browser.json").write_text(json.dumps(value))
    (artifacts / "node-admission-browser-input.json").write_text(json.dumps(expected))
    host = run_host_binding_case(base, directory, admin, admin_identity, host, target, report)
    retire_owned_configuration(renew_admin(base, admin_identity["principal"]["id"]), host, target,
                               {k: host[k] for k in ("display_name", "node_kind", "addresses", "allowed_module_bindings")}, report)


PRESSURE_FIELDS = ("trigger_percent", "recovery_percent", "trigger_seconds", "recovery_seconds")
RESOURCE_RULES = "/api/v1/monitor/platform/resource-alert-rules"
ALERTS = "/api/v1/monitor/platform/alerts"
ALERT_DESTINATIONS = "/api/v1/monitor/platform/alert-notification-destinations"
ALERT_DELIVERIES = "/api/v1/monitor/platform/alert-notification-deliveries"
MEMORY_KEYS = ("node.memory.total_bytes", "node.memory.available_bytes", "node.memory.used_percent")
CPU_KEYS = ("node.cpu.logical_cores", "node.cpu.busy_percent")
PRESSURE_METRICS = {"memory": MEMORY_KEYS, "cpu": CPU_KEYS}


def finite_number(value):
    return type(value) in {int, float} and math.isfinite(value)


def memory_pressure_plan(total, available, used):
    require(all(finite_number(v) for v in (total, available, used)) and total > 0 and 0 < available <= total
            and 0 <= used < 100 and abs(used - 100*(1-available/total)) < .01, "invalid memory baseline")
    size = int(min(total*.12, available*.25, 2*1024**3))
    delta = 100*size/total
    require(delta >= 8, "Hosted memory budget cannot create a distinguishable pressure window")
    return {"total_bytes": total, "available_bytes": available, "baseline_percent": used, "bytes": size,
            "delta_percent": delta, "trigger_percent": used+delta*.65, "recovery_percent": used+delta*.25,
            "trigger_seconds": 30, "recovery_seconds": 30}


def cpu_pressure_plan(cores, used):
    require(type(cores) is int and cores > 0 and finite_number(used) and 0 <= used < 100, "invalid CPU baseline")
    quota = min(1_000_000_000, cores*250_000_000)
    delta = 100*quota/(cores*1_000_000_000)
    require(delta >= 8 and used+delta*.65 < 100, "Hosted CPU budget cannot create a distinguishable pressure window")
    return {"logical_cores": cores, "quota_nanos": quota, "baseline_percent": used, "delta_percent": delta,
            "max_seconds": 300, "trigger_percent": used+delta*.65, "recovery_percent": used+delta*.25,
            "trigger_seconds": 30, "recovery_seconds": 30}


def pressure_plan(kind, points):
    values = [points[k]["value"] for k in PRESSURE_METRICS[kind]]
    if kind == "memory": return memory_pressure_plan(*values)
    # CPU core count is encoded as a JSON number by the metrics catalog.
    require(finite_number(values[0]) and values[0] == int(values[0]), "CPU core count is not integral")
    return cpu_pressure_plan(int(values[0]), values[1])


@contextmanager
def native_memory_pressure(directory, plan, report):
    require(FIXTURE.boundary() == directory, "memory pressure requires the owned Hosted deployment")
    require(plan == memory_pressure_plan(plan["total_bytes"], plan["available_bytes"], plan["baseline_percent"]),
            "memory pressure plan changed after validation")
    mapping = mmap.mmap(-1, plan["bytes"], flags=mmap.MAP_PRIVATE | mmap.MAP_ANONYMOUS,
                        prot=mmap.PROT_READ | mmap.PROT_WRITE)
    try:
        for offset in range(0, plan["bytes"], os.sysconf("SC_PAGE_SIZE")):
            mapping[offset] = 1
        report["memory_allocated_bytes"] = plan["bytes"]
        report["pressure_started_at"] = time.time()
        yield
    finally:
        mapping.close()
        report["memory_released"] = True
        report["pressure_released_at"] = time.time()


def canonical_uuid(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", value) is not None and value != "00000000-0000-0000-0000-000000000000"


def assert_resource_alert_event(value, node, target, query_policy, rule):
    fields = {"schema", "event_id", "event_type", "incident_id", "category", "subject_kind", "subject_id",
              "signal", "severity", "occurred_at", "evidence"}
    require(type(value) is dict and set(value) == fields, "resource alert payload fields mismatch")
    require(value["schema"] == "addp.platform-resource-alert/v1" and canonical_uuid(value["event_id"])
            and value["event_type"] in {"opened", "resolved"} and type(value["incident_id"]) is int and value["incident_id"] > 0,
            "invalid resource event identity")
    require(value["category"] == "resource" and value["subject_kind"] == "host_node" and value["subject_id"] == node["node_id"]
            and value["signal"] == rule["metric_key"] and value["severity"] == "warning", "resource event scope mismatch")
    evidence = value["evidence"]
    require(type(evidence) is dict and set(evidence) == {"rule_id", "rule_version", "policy", "node_version", "target_id", "target_version",
            "query_policy_version", "evaluated_at", "sampled_from", "sampled_to", "samples", "minimum", "maximum", "latest"}, "resource evidence fields mismatch")
    for field, expected in (("rule_id", rule["id"]), ("rule_version", rule["version"]), ("node_version", node["version"]),
                            ("target_id", target["id"]), ("target_version", target["version"]), ("query_policy_version", query_policy["version"])):
        require(type(evidence[field]) is type(expected) and evidence[field] == expected, "resource evidence configuration mismatch")
    require(evidence["policy"] == {k: rule[k] for k in PRESSURE_FIELDS}
            and all(type(evidence["policy"][k]) is int for k in PRESSURE_FIELDS[2:]), "resource event policy mismatch")
    evaluated, first, last = (utc_timestamp(evidence[k]) for k in ("evaluated_at", "sampled_from", "sampled_to"))
    occurred = instance_timestamp(value["occurred_at"])
    duration = rule["trigger_seconds" if value["event_type"] == "opened" else "recovery_seconds"]
    require(evaluated == int(evaluated) and occurred == evaluated and 0 <= evaluated-last <= 16
            and duration <= last-first <= duration+16 and type(evidence["samples"]) is int
            and 3 <= evidence["samples"] <= 22, "resource event lacks a continuous sampled window")
    low, high, latest = (evidence[k] for k in ("minimum", "maximum", "latest"))
    require(all(finite_number(v) for v in (low, high, latest)) and 0 <= low <= latest <= high <= 100, "invalid resource event values")
    require(low > rule["trigger_percent"] if value["event_type"] == "opened" else high < rule["recovery_percent"], "resource event threshold not witnessed")


def unique_json_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate resource notification JSON key")
        result[key] = value
    return result


class ResourceAlertReceiver:
    """Only the owned suite's signed resource events; no credential or arbitrary body logging."""
    def __init__(self, directory, node, target, query_policy):
        self.directory, self.node, self.target, self.query_policy = directory, node, target, query_policy
        self.rule = None
        self.secret = secrets.token_hex(32)
        self.lock = threading.Lock()
        self.receipts, self.invalid = [], 0
        self.server, self.thread = None, None
        self.closed = False

    def validate_event(self, value):
        assert_resource_alert_event(value, self.node, self.target, self.query_policy, self.rule)

    def accept(self, body, headers):
        with self.lock:
            try:
                require(0 < len(body) <= 65536 and self.rule is not None, "notification body outside boundary")
                stamp, signature, delivery = (headers.get(k, "") for k in ("X-ADDP-Webhook-Timestamp", "X-ADDP-Webhook-Signature", "X-ADDP-Webhook-ID"))
                require(re.fullmatch(r"[0-9]{1,12}", stamp) is not None and abs(time.time()-int(stamp)) <= 30
                        and canonical_uuid(delivery), "notification timestamp or delivery identity invalid")
                expected = "v1="+hmac.new(self.secret.encode(), stamp.encode()+b"."+body, hashlib.sha256).hexdigest()
                require(hmac.compare_digest(signature, expected), "notification signature invalid")
                value = json.loads(body, object_pairs_hook=unique_json_object)
                self.validate_event(value)
                require(len(self.receipts) < 16, "excess notification attempts")
                for receipt in self.receipts:
                    previous = receipt["payload"]
                    if receipt["delivery_id"] == delivery or previous["event_id"] == value["event_id"] or previous["event_type"] == value["event_type"]:
                        require(receipt["delivery_id"] == delivery and previous == value, "resource retry identity or body changed")
                    require(previous["incident_id"] == value["incident_id"], "resource lifecycle incident changed")
                status = 503 if value["event_type"] == "opened" and not self.receipts else 202
                self.receipts.append({"delivery_id": delivery, "payload": value, "response_status": status})
                return status
            except (SuiteError, ValueError, TypeError, KeyError, UnicodeError):
                self.invalid += 1
                return 400

    def snapshot(self):
        with self.lock:
            return json.loads(json.dumps(self.receipts))

    def __enter__(self):
        require(FIXTURE.boundary() == self.directory, "notification receiver requires the owned Hosted deployment")
        receiver = self
        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *_args): pass
            def do_POST(self):
                length = self.headers.get("Content-Length", "")
                valid = (self.path == "/resource-alert" and re.fullmatch(r"[0-9]{1,6}", length) is not None
                         and 0 < int(length) <= 65536 and not self.headers.get("Transfer-Encoding")
                         and self.headers.get_content_type() == "application/json"
                         and all(len(self.headers.get_all(k, [])) == 1 for k in ("Content-Length", "X-ADDP-Webhook-ID", "X-ADDP-Webhook-Timestamp", "X-ADDP-Webhook-Signature")))
                if valid:
                    status = receiver.accept(self.rfile.read(int(length)), self.headers)
                else:
                    with receiver.lock: receiver.invalid += 1
                    status = 400
                self.send_response(status)
                self.send_header("Content-Length", "0")
                self.end_headers()
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.minimum_version = ssl.TLSVersion.TLSv1_2
        context.load_cert_chain(str(self.directory/"center-tls/server.crt"), str(self.directory/"center-tls/server.key"))
        class Server(http.server.ThreadingHTTPServer):
            daemon_threads = False
            def get_request(self):
                connection, address = super().get_request()
                connection.settimeout(5)
                try:
                    return context.wrap_socket(connection, server_side=True), address
                except BaseException:
                    connection.close()
                    raise
        self.server = Server(("127.0.0.1", 0), Handler)
        self.url = "https://127.0.0.1:"+str(self.server.server_port)+"/resource-alert"
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        try:
            self.thread.start()
        except BaseException:
            self.server.server_close()
            self.closed = True
            raise
        return self

    def __exit__(self, *_args):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=6)
        self.closed = not self.thread.is_alive()


def assert_alert_delivery(row, receipts, destination, incident_status, deleted=False):
    allowed = {"id", "event_id", "destination_id", "channel", "status", "attempt_count", "manual_retry_count", "next_attempt_at", "last_error",
               "delivered_at", "created_at", "cycle_attempt_count", "event_type", "occurred_at", "incident_id", "category", "subject_kind",
               "subject_id", "incident_status", "suppressed_until", "destination_name", "destination_version"}
    require(type(row) is dict and set(row) <= allowed, "private or unknown notification fields exposed")
    matching = [r for r in receipts if r["delivery_id"] == row.get("id")]
    require(bool(matching), "delivery audit has no signed receipt")
    event = matching[0]["payload"]
    for field in ("event_id", "event_type", "incident_id", "category", "subject_kind", "subject_id"):
        require(row.get(field) == event[field], "delivery audit event mismatch")
    require(instance_timestamp(row.get("occurred_at")) == instance_timestamp(event["occurred_at"]), "delivery audit occurrence time mismatch")
    attempts = row.get("attempt_count")
    require(row.get("destination_id") == destination["id"] and row.get("destination_version") == (0 if deleted else destination["version"])
            and row.get("channel") == "webhook" and row.get("status") == "delivered" and row.get("incident_status") == incident_status
            and type(attempts) is int and (2 if event["event_type"] == "opened" else 1) <= attempts <= 8
            and row.get("cycle_attempt_count") == attempts and row.get("manual_retry_count") == 0
            and not row.get("last_error") and row.get("next_attempt_at") is None, "delivery audit final state mismatch")
    require(len(matching) <= attempts and matching[-1]["response_status"] == 202
            and (event["event_type"] != "opened" or matching[0]["response_status"] == 503), "automatic retry not witnessed")
    require(instance_timestamp(row.get("delivered_at")) >= instance_timestamp(event["occurred_at"]), "delivery predates event")


def alert_page(client, path):
    value = client.request("GET", path, (200,)).payload
    rows = value.get("data")
    require(type(rows) is list and type(value.get("total")) is int and value["total"] == len(rows)
            and len(rows) <= 100 and value.get("page") == 1 and value.get("page_size") == 100,
            "alert acceptance page is incomplete")
    return rows


def fresh_pressure_plan(admin, node, target, after, detail, kind="memory"):
    keys = PRESSURE_METRICS[kind]
    plans = []

    def observed():
        baseline = admin.request("GET", resource_path(node, keys=keys), (200,)).payload
        assert_resources(baseline, node, target, keys=keys)
        points = {row["metric_key"]: row["points"][0] for row in baseline["series"]}
        require(kind != "memory" or len({p["sampled_at"] for p in points.values()}) == 1, "memory baseline samples differ")
        sampled_at = min((p["sampled_at"] for p in points.values()), key=utc_timestamp)
        if utc_timestamp(sampled_at) < after:
            return False
        plans.append(pressure_plan(kind, points))
        detail[kind+"_baseline"] = {"requested_after": after, "sampled_at": sampled_at,
                                     "values": {k: points[k]["value"] for k in keys}}
        return True

    eventually(observed, "fresh "+kind+" baseline after control setup", timeout=45)
    return plans[0]


def run_resource_alert_lifecycle(admin, security, machine, tenant_clients, prom, node, target, query_policy, directory, report, pressure_kind="memory"):
    require(pressure_kind in PRESSURE_METRICS, "unsupported pressure kind")
    keys = PRESSURE_METRICS[pressure_kind]
    metric_key = keys[-1]
    report["stage"] = "resource-alert-real-"+pressure_kind+"-lifecycle"
    detail = {"result": "failed", "pressure_kind": pressure_kind, pressure_kind+"_released": False, "receiver_closed": False}
    report["resource_alert_lifecycle"] = detail
    require(FIXTURE.boundary() == directory, "resource alerts require the owned Hosted deployment")
    baseline = admin.request("GET", resource_path(node, keys=keys), (200,)).payload
    assert_resources(baseline, node, target, keys=keys)
    points = {row["metric_key"]: row["points"][0] for row in baseline["series"]}
    require(pressure_kind != "memory" or len({p["sampled_at"] for p in points.values()}) == 1, "memory baseline samples differ")
    plan = pressure_plan(pressure_kind, points)
    detail[pressure_kind+"_plan"] = plan
    detail["initial_"+pressure_kind+"_plan"] = dict(plan)
    rule_body = {"name": "online-"+pressure_kind+"-"+os.environ["ADDP_ONLINE_TEST_RUN_ID"], "node_id": node["node_id"],
                 "metric_key": metric_key, "enabled": False, **{k: plan[k] for k in PRESSURE_FIELDS}}
    rule, destination, destination_body = None, None, None
    source_stopped, completed, destination_removed = False, False, False
    receiver = ResourceAlertReceiver(directory, node, target, query_policy)
    rule_query = RESOURCE_RULES+"?"+urllib.parse.urlencode({"node_id": node["node_id"], "page": 1, "page_size": 100})
    incident_query = ALERTS+"?"+urllib.parse.urlencode({"category": "resource", "subject_id": node["node_id"], "page": 1, "page_size": 100})
    delivery_query = ALERT_DELIVERIES+"?page=1&page_size=100"

    def current_rule():
        rows = alert_page(admin, rule_query)
        require(len(rows) == 1 and rows[0].get("id") == rule["id"] and rows[0].get("version") == rule["version"]
                and all(rows[0].get(k) == rule_body[k] for k in (*PRESSURE_FIELDS, "node_id", "metric_key")), "rule configuration changed during evaluation")
        detail["latest_rule_evaluation"] = {k: rows[0][k] for k in ("version", "enabled", "last_verdict", "last_error", "last_evaluated_at", "last_sample_at", "last_value") if k in rows[0]}
        return rows[0]

    def incidents():
        rows = alert_page(admin, incident_query)
        require(len(rows) <= 1 and all(r.get("category") == "resource" and r.get("subject_kind") == "host_node"
                and r.get("subject_id") == node["node_id"] and r.get("signal") == metric_key
                and r.get("severity") == "warning" and not r.get("instance_id") for r in rows), "resource incident scope mismatch")
        return rows

    def check_incident(status, expected=None):
        if rule is not None:
            current_rule()
        rows = incidents()
        if not rows or rows[0].get("status") != status: return None
        incident = rows[0]
        require(type(incident.get("id")) is int and incident["id"] > 0, "invalid resource incident identity")
        if expected is not None:
            require(incident["id"] == expected["id"] and incident.get("resource_evidence") == expected["resource_evidence"]
                    and incident.get("opened_at") == expected["opened_at"], "resource incident opening evidence changed")
        else:
            event = {"schema": "addp.platform-resource-alert/v1", "event_id": rule["id"], "event_type": "opened",
                     "incident_id": incident["id"], "category": "resource", "subject_kind": "host_node", "subject_id": node["node_id"],
                     "signal": metric_key, "severity": "warning", "occurred_at": incident.get("opened_at"),
                     "evidence": incident.get("resource_evidence")}
            assert_resource_alert_event(event, node, target, query_policy, rule)
            require(utc_timestamp(event["evidence"]["sampled_from"]) >= detail["pressure_started_at"], "opening window predates real pressure")
        return incident

    def evaluated(verdict, after):
        row = current_rule()
        if row.get("last_verdict") != verdict or not row.get("last_evaluated_at"): return False
        if instance_timestamp(row["last_evaluated_at"]) < after: return False
        require(row.get("enabled") is True, "enabled rule unexpectedly paused")
        if verdict == "unknown":
            require(row.get("last_error") == "no_data" and row.get("last_value") is None and row.get("last_sample_at") is None,
                    "unknown resource verdict retained a value")
        else:
            require(row.get("last_error") == "" and finite_number(row.get("last_value")), "evaluated rule has no valid sample")
            if instance_timestamp(row.get("last_sample_at")) < after: return False
            if verdict == "normal" and row["last_value"] >= rule["recovery_percent"]: return False
            if verdict == "risk" and row["last_value"] <= rule["trigger_percent"]: return False
        detail["last_"+verdict] = {k: row[k] for k in ("last_evaluated_at", "last_verdict", "last_error", "last_sample_at", "last_value") if k in row}
        return True

    def deliveries_ready(kinds, incident, deleted=False):
        require(receiver.invalid == 0, "signed notification receiver rejected a request")
        receipts = receiver.snapshot()
        rows = [r for r in alert_page(admin, delivery_query) if r.get("destination_id") == destination["id"]]
        require(len(rows) <= len(kinds) and len({r.get("event_type") for r in rows}) == len(rows), "duplicate resource lifecycle deliveries")
        if {r.get("event_type") for r in rows} != set(kinds) or any(r.get("status") != "delivered" for r in rows): return False
        for row in rows:
            assert_alert_delivery(row, receipts, destination, incident["status"], deleted=deleted)
            require(row["incident_id"] == incident["id"], "delivery references another incident")
        detail["deliveries"] = rows
        detail["signed_receipts"] = receipts
        return True

    try:
        with receiver:
            destination_body = {"name": "online-resource-"+os.environ["ADDP_ONLINE_TEST_RUN_ID"], "channel": "webhook", "url": receiver.url,
                                "recipients": [], "categories": ["resource"], "event_types": ["opened", "resolved"], "enabled": False}
            require(not alert_page(admin, rule_query) and not incidents(), "new Hosted node already has resource alert history")
            destination = admin.request("POST", ALERT_DESTINATIONS, (200,), destination_body).payload
            require(type(destination.get("id")) is int and destination["id"] > 0 and destination.get("version") == 1
                    and not destination.get("enabled") and not destination.get("secret_configured"), "invalid disabled resource destination")
            destination_path = ALERT_DESTINATIONS+"/"+str(destination["id"])
            destination = admin.request("PUT", destination_path+"/credential", (200,), {"version": 1, "secret": receiver.secret}).payload
            require(destination.get("version") == 2 and destination.get("secret_configured") is True, "resource destination credential was not configured")
            destination_body["enabled"] = True
            destination = admin.request("PUT", destination_path, (200,), dict(destination_body, version=2)).payload
            require(destination.get("version") == 3 and destination.get("enabled") is True, "resource destination not enabled")
            rule = admin.request("POST", RESOURCE_RULES, (200,), rule_body).payload
            require(canonical_uuid(rule.get("id")) and rule.get("version") == 1 and rule.get("enabled") is False
                    and rule.get("last_verdict") == "paused", "resource rule was not created disabled")
            rule_path = RESOURCE_RULES+"/"+rule["id"]
            detail.update(rule_id=rule["id"], destination_id=destination["id"])
            for client in [security, machine, *tenant_clients, API.GatewayClient(admin.base_url, "", 10)]:
                denied = (401,) if not client.token else (403,)
                for path in (rule_query, incident_query, ALERT_DESTINATIONS, delivery_query):
                    # Rejections are JSON errors, even when the successful resource is an array.
                    client.request("GET", path, denied)
                for method, path, body in (("POST", RESOURCE_RULES, rule_body), ("PUT", rule_path, dict(rule_body, version=1)),
                    ("DELETE", rule_path, {"version": 1}), ("POST", ALERT_DESTINATIONS, dict(destination_body, enabled=False)),
                    ("PUT", destination_path, dict(destination_body, version=3)), ("DELETE", destination_path, {"version": 3}),
                    ("PUT", destination_path+"/credential", {"version": 3, "secret": receiver.secret})):
                    client.request(method, path, denied, body)
            require(current_rule()["enabled"] is False, "denied requests changed rule")
            saved = [d for d in admin.request("GET", ALERT_DESTINATIONS, (200,), response_type=list).payload if d.get("id") == destination["id"]]
            config_fields = ("id", "version", "name", "channel", "url", "recipients", "categories", "event_types", "enabled", "secret_configured")
            require(len(saved) == 1 and all(saved[0].get(k) == destination.get(k) for k in config_fields), "denied notification requests changed destination")
            detail["identity_isolation"] = True
            # Finalize thresholds from fresh post-setup evidence, before enablement or load.
            plan = fresh_pressure_plan(admin, node, target, time.time(), detail, pressure_kind)
            detail[pressure_kind+"_plan"] = plan
            rule_body.update({k: plan[k] for k in PRESSURE_FIELDS})
            enabled_at = time.time()
            rule = admin.request("PUT", rule_path, (200,), dict(rule_body, version=1, enabled=True)).payload
            require(rule.get("version") == 2 and rule.get("enabled") is True, "rule enable CAS did not advance")
            receiver.rule = rule
            admin.request("PUT", rule_path, (409,), dict(rule_body, version=1, enabled=True))
            admin.request("PUT", destination_path+"/credential", (409,), {"version": 1, "secret": receiver.secret})
            eventually(lambda: evaluated("normal", enabled_at), "baseline "+pressure_kind+" rule evaluation", timeout=150)
            require(not incidents() and not receiver.snapshot(), "baseline falsely opened an incident")
            load = native_memory_pressure if pressure_kind == "memory" else FIXTURE.cpu_pressure
            with load(directory, plan, detail):
                eventually(lambda: check_incident("open"), "real "+pressure_kind+" pressure incident", timeout=180)
                opened = check_incident("open")
                require(opened is not None, "opened incident vanished")
                detail["opened_incident"] = opened
                eventually(lambda: deliveries_ready(("opened",), opened), "signed opened notification automatic retry", timeout=90)
                admin.request("DELETE", rule_path, (409,), {"version": rule["version"]})
                detail["active_delete_rejected"] = True
                if pressure_kind == "memory":
                    source_action("stop")
                    source_stopped = True
                    try:
                        selector = '{job="addp_nodes",addp_node_id="'+node["node_id"]+'"}'
                        eventually(lambda: source_scrape_failed(prom, selector), "resource alert source failure")
                        failed_at = time.time()
                        eventually(lambda: evaluated("unknown", failed_at), "unknown alert verdict after failed source", timeout=100)
                        held_since = time.monotonic()
                        first_unknown = dict(detail["last_unknown"])
                        deadline = held_since+plan["recovery_seconds"]+15
                        observations = 0
                        while time.monotonic() < deadline:
                            require(evaluated("unknown", failed_at) and check_incident("open", opened) is not None,
                                    "missing source falsely recovered resource incident")
                            require(receiver.invalid == 0 and all(r["payload"]["event_type"] == "opened" for r in receiver.snapshot()),
                                    "missing source emitted a resolved notification")
                            observations += 1
                            time.sleep(2)
                        detail["unknown_does_not_resolve"] = {"duration_seconds": time.monotonic()-held_since, "observations": observations,
                                                                  "first": first_unknown, "last": dict(detail["last_unknown"])}
                    finally:
                        restarted_at = time.time()
                        source_action("start")
                        source_stopped = False
                    eventually(lambda: evaluated("risk", restarted_at), "fresh high memory window after source recovery", timeout=180)
                    require(check_incident("open", opened) is not None, "source recovery replaced the open incident")
                    detail["source_recovery_preserves_incident"] = True
            eventually(lambda: check_incident("resolved", opened), pressure_kind+" release recovers the original incident", timeout=180)
            resolved = check_incident("resolved", opened)
            require(resolved is not None and instance_timestamp(resolved.get("resolved_at")) >= detail["pressure_released_at"], "resource recovery predates pressure release")
            eventually(lambda: deliveries_ready(("opened", "resolved"), resolved), "signed resolved notification", timeout=90)
            receipts = receiver.snapshot()
            events = {r["payload"]["event_type"]: r["payload"] for r in receipts}
            require(events["opened"]["evidence"] == opened["resource_evidence"] and events["opened"]["event_id"] != events["resolved"]["event_id"]
                    and utc_timestamp(events["resolved"]["evidence"]["sampled_from"]) >= detail["pressure_released_at"], "notification lifecycle evidence mismatch")
            detail["resolved_incident"] = resolved
            rule = admin.request("PUT", rule_path, (200,), dict(rule_body, version=rule["version"], enabled=False)).payload
            require(rule.get("version") == 3 and rule.get("enabled") is False, "rule disable CAS did not advance")
            admin.request("DELETE", rule_path, (409,), {"version": 2})
            admin.request("DELETE", rule_path, (200,), {"version": rule["version"]})
            rule = None
            destination = admin.request("PUT", destination_path, (200,), dict(destination_body, version=destination["version"], enabled=False)).payload
            require(destination.get("version") == 4 and destination.get("enabled") is False, "destination disable CAS did not advance")
            admin.request("DELETE", destination_path, (409,), {"version": 3})
            admin.request("DELETE", destination_path, (200,), {"version": destination["version"]})
            destination_removed = True
            require(not alert_page(admin, rule_query), "deleted resource rule remains listed")
            require(all(d.get("id") != destination["id"] for d in admin.request("GET", ALERT_DESTINATIONS, (200,), response_type=list).payload), "deleted notification destination remains listed")
            require(check_incident("resolved", opened) == resolved and deliveries_ready(("opened", "resolved"), resolved, deleted=True), "config cleanup removed resource alert history")
            detail["configuration_removed_history_retained"] = True
            completed = True
    finally:
        # Best effort configuration disable must never replace the original failed assertion.
        if not completed:
            for obj, path, body in ((rule, RESOURCE_RULES, rule_body), (destination, ALERT_DESTINATIONS, destination_body)):
                if obj is not None and body is not None and not (path == ALERT_DESTINATIONS and destination_removed):
                    try:
                        admin.request("PUT", path+"/"+str(obj["id"]), (200,), dict(body, version=obj["version"], enabled=False))
                    except (SuiteError, OSError, ValueError):
                        detail["failure_config_disable_incomplete"] = True
        if source_stopped:
            try: source_action("start")
            except (SuiteError, OSError, ValueError, subprocess.SubprocessError): detail["failure_source_restart_incomplete"] = True
        detail["receiver_closed"] = receiver.closed
    require(receiver.invalid == 0, "signed notification receiver rejected a request")
    require(completed and detail[pressure_kind+"_released"] and detail["receiver_closed"], "resource alert fixture cleanup incomplete")
    detail["result"] = "passed"
    # Preserve this phase if a later fault phase exhausts the outer deadline.
    # The overall report stays failed until the complete scene finishes.
    write_evidence(report)


AVAILABILITY_RULES = '/api/v1/monitor/platform/service-availability-alert-rules'
AVAILABILITY_PERMISSIONS = {'monitor.platform_service_availability_rule.'+action for action in ('read','create','update','delete')}


def assert_availability_event(value, group, rule):
    fields = {'schema','event_id','event_type','incident_id','category','subject_kind','subject_id','signal','severity','occurred_at','evidence'}
    require(type(value) is dict and set(value) == fields and value['schema'] == 'addp.platform-service-availability-alert/v1'
            and canonical_uuid(value['event_id']) and value['event_type'] in {'opened','resolved'}
            and type(value['incident_id']) is int and value['incident_id'] > 0, 'invalid availability event envelope')
    require(value['category'] == 'service_availability' and value['subject_kind'] == 'service_group'
            and value['subject_id'] == group['id'] and value['signal'] == rule['id'] and value['severity'] == 'warning', 'availability event scope changed')
    e = value['evidence']
    require(type(e) is dict and set(e) == {'rule_name','rule_id','rule_version','deployment_id','module_name','role','runtime_name',
            'expected_replicas','intent_changed_at','policy','confirmed_at','observed_from','observed_to','online_replicas','samples'}, 'availability evidence fields changed')
    for key in ('deployment_id','module_name','role','runtime_name','expected_replicas'):
        require(type(e[key]) is type(group[key]) and e[key] == group[key], 'availability evidence targets another group')
    require(e['rule_id'] == rule['id'] and e['rule_name'] == rule['name'] and type(e['rule_version']) is int and e['rule_version'] == rule['version']
            and e['policy'] == {'trigger_seconds':60,'recovery_seconds':30}
            and all(type(v) is int for v in e['policy'].values()), 'availability evidence policy changed')
    times = [instance_timestamp(e[k]) for k in ('intent_changed_at','confirmed_at','observed_from','observed_to')]
    duration = 60 if value['event_type'] == 'opened' else 30
    require(times == sorted(times) and duration <= times[-1]-times[-2] <= duration+16
            and 0 <= instance_timestamp(value['occurred_at'])-times[-1] <= 16
            and type(e['samples']) is int and e['samples'] >= duration//15+1
            and type(e['online_replicas']) is int and (e['online_replicas'] == 0 if value['event_type'] == 'opened' else e['online_replicas'] > 0), 'availability evidence has no complete real observation window')


class AvailabilityAlertReceiver(ResourceAlertReceiver):
    def validate_event(self, value):
        assert_availability_event(value, self.node, self.rule)


def validate_availability_browser(value, expected):
    require(value.get('schema_version') == 'addp.node-resources-browser/v1' and value.get('result') == 'passed'
            and value.get('stage') == 'complete' and value.get('run_id') == expected['run_id'], 'availability browser incomplete')
    platform_identity(value.get('identity',{}), 'platform.system_administrator')
    require(value['identity']['principal']['id'] == expected['admin_id'] and AVAILABILITY_PERMISSIONS <= permissions(value['identity']), 'availability browser identity changed or missing rule permissions')
    detail = value.get('service_availability', {})
    require(set(detail) == {'rule','incident','reload_restored','evidence_visible'} and detail['reload_restored'] is True and detail['evidence_visible'] is True,
            'availability browser evidence missing')
    rule, incident = expected['service_availability']['rule'], expected['service_availability']['incident']
    keys = ('id','name','group_id','deployment_id','module_name','role','runtime_name','version','enabled','trigger_seconds','recovery_seconds')
    require(all(detail['rule'].get(k) == rule[k] for k in keys) and detail['incident'] == incident, 'availability browser differs from current API receipts')
    return {'result':'passed','reload_restored':True,'evidence_visible':True}


def run_service_availability_case(base, directory, admin, actor, denied, node, target, node_input, rows, slots, report):
    require(AVAILABILITY_PERMISSIONS <= permissions(actor) and set(slots) == set(FIXTURE.AVAILABILITY_UNITS), 'availability authorization or Worker set incomplete')
    groups = admin.request('GET', AVAILABILITY_RULES+'/groups', (200,)).payload
    candidates = [g for g in groups.get('data',[]) if (g.get('module_name'),g.get('role'),g.get('runtime_name')) == ('meta','worker','scan')]
    require(groups.get('total') == len(groups.get('data',[])) and len(candidates) == 1, 'Meta service group is ambiguous')
    group = candidates[0]
    require(group['expected_replicas'] == 2 and group['desired_state'] == 'running' and group['state'] == 'observed' and group['online_replicas'] == 2, 'two real Worker leases required')
    intent = Path(os.environ.get('ADDP_DEPLOYMENT_DISCOVERY_DIR') or FIXTURE.ROOT/'.dev-state/deployment-discovery') / 'service-expectations.json'
    original_intent = intent.read_bytes()
    body = {'name':'online-availability-'+os.environ['ADDP_ONLINE_TEST_RUN_ID'], 'group_id':group['id'], 'trigger_seconds':60,'recovery_seconds':30,'enabled':False}
    rule, dest, dest_body = None, None, None
    detail = report['service_availability'] = {'result':'failed'}
    rule_query = AVAILABILITY_RULES+'?page=1&page_size=100'
    incident_query = ALERTS+'?'+urllib.parse.urlencode({'category':'service_availability','subject_id':group['id'],'page':1,'page_size':100})
    receiver = AvailabilityAlertReceiver(directory, group, None, None)
    def current_rule():
        found = alert_page(admin, rule_query)
        require(len(found) == 1 and found[0]['id'] == rule['id'] and found[0]['version'] == rule['version']
                and all(found[0][k] == rule[k] for k in ('name','group_id','trigger_seconds','recovery_seconds','enabled')), 'availability rule changed outside this scene')
        return found[0]
    def incidents():
        found = alert_page(admin, incident_query)
        require(len(found) <= 1 and all(r['category']=='service_availability' and r['subject_kind']=='service_group'
                and r['subject_id']==group['id'] and r['signal']==rule['id'] for r in found), 'duplicate or foreign availability incident')
        return found
    def evaluated(online, after, verdict='normal'):
        r = current_rule()
        return (r.get('online_replicas') == online and r.get('last_verdict') == verdict and not r.get('last_error')
                and bool(r.get('confirmed_at')) and instance_timestamp(r['last_evaluated_at']) >= after)
    def wait_without_alert(online, seconds):
        start = time.monotonic()
        while time.monotonic()-start < seconds:
            require(intent.read_bytes() == original_intent, 'fault injection changed deployment intent')
            require(not incidents(), 'healthy or planned group falsely alerted')
            r = current_rule()
            require(r['enabled'] and r.get('online_replicas') == online and r['last_verdict'] == 'normal' and not r['last_error'], 'single-Worker continuity was lost')
            time.sleep(1)
    def incident_ready(state, previous=None):
        found = incidents()
        if not found or found[0]['status'] != state: return False
        r = found[0]
        require(type(r['id']) is int and r['id'] > 0, 'invalid availability incident')
        if previous:
            require(r['id']==previous['id'] and r['opened_at']==previous['opened_at'] and r['availability_evidence']==previous['availability_evidence'], 'recovery changed opening incident evidence')
        else:
            event = {'schema':'addp.platform-service-availability-alert/v1','event_id':rule['id'],'event_type':'opened',
                     'incident_id':r['id'],'category':r['category'],'subject_kind':r['subject_kind'],'subject_id':r['subject_id'],
                     'signal':r['signal'],'severity':r['severity'],'occurred_at':r['opened_at'],'evidence':r['availability_evidence']}
            assert_availability_event(event,group,rule)
        return r
    def deliveries_ready(kinds, incident):
        require(receiver.invalid == 0, 'availability receiver rejected signed delivery')
        receipts = receiver.snapshot()
        found = [r for r in alert_page(admin,ALERT_DELIVERIES+'?page=1&page_size=100') if r.get('destination_id') == dest['id']]
        require(len(found) <= len(kinds) and len({r['event_type'] for r in found}) == len(found), 'duplicate availability outbox delivery')
        if {r['event_type'] for r in found} != set(kinds) or any(r['status']!='delivered' for r in found): return False
        for r in found:
            assert_alert_delivery(r,receipts,dest,incident['status'])
            require(r['incident_id']==incident['id'], 'availability delivery belongs to another incident')
        detail.update(deliveries=found,signed_receipts=receipts)
        return True
    try:
        report['stage']='service-availability-registration'; write_evidence(report)
        require(not alert_page(admin,rule_query), 'isolated deployment already has availability rules')
        rule = admin.request('POST', AVAILABILITY_RULES,(201,),body).payload
        require(rule['version']==1 and rule['enabled'] is False and canonical_uuid(rule['id']), 'availability rule must start disabled')
        path = AVAILABILITY_RULES+'/'+rule['id']
        for client in [*denied,API.GatewayClient(base,'',10)]:
            status = (403,) if client.token else (401,)
            for endpoint in (rule_query, AVAILABILITY_RULES+'/groups', incident_query): client.request('GET',endpoint,status)
            client.request('POST',AVAILABILITY_RULES,status,body)
            client.request('PUT',path,status,dict(body,version=1,enabled=True))
            client.request('DELETE',path,status,{'version':1})
        with receiver:
            dest_body={'name':'online-availability-'+os.environ['ADDP_ONLINE_TEST_RUN_ID'],'channel':'webhook','url':receiver.url,
                       'recipients':[],'categories':['service_availability'],'event_types':['opened','resolved'],'enabled':False}
            dest=admin.request('POST',ALERT_DESTINATIONS,(200,),dest_body).payload
            dest_path=ALERT_DESTINATIONS+'/'+str(dest['id'])
            dest=admin.request('PUT',dest_path+'/credential',(200,),{'version':dest['version'],'secret':receiver.secret}).payload
            dest_body['enabled']=True
            dest=admin.request('PUT',dest_path,(200,),dict(dest_body,version=dest['version'])).payload
            rule=admin.request('PUT',path,(200,),dict(body,version=1,enabled=True)).payload
            receiver.rule=rule
            require(rule['version']==2 and rule['enabled'] is True, 'availability enable CAS failed')
            admin.request('PUT',path,(409,),dict(body,version=1,enabled=True))
            eventually(lambda:evaluated(2,0), 'two Worker baseline confirmation',timeout=100)
            require(not incidents() and not receiver.snapshot(), 'baseline falsely alerted')
            with FIXTURE.availability_pauses(directory) as change:
                detail['paused_workers_restored']=False
                report['stage']='service-availability-single-worker';write_evidence(report)
                stopped_at=time.time()
                change('meta-worker-1',True)
                eventually(lambda:evaluated(1,stopped_at),'single Worker real lease expiry',timeout=150)
                wait_without_alert(1,76)
                detail['single_worker_no_alert']=True
                report['stage']='service-availability-all-workers';write_evidence(report)
                all_stopped_at=time.time()
                change('meta-worker-2',True)
                eventually(lambda:incident_ready('open'),'all Worker real lease expiry and trigger',timeout=210)
                opened=incident_ready('open'); detail['opened_incident']=opened
                require(instance_timestamp(opened['availability_evidence']['observed_from'])>=all_stopped_at,'opening window predates fault')
                eventually(lambda:deliveries_ready(('opened',),opened),'availability signed opening retry',timeout=90)
                admin.request('DELETE',path,(409,),{'version':rule['version']})
                report['stage']='service-availability-recovery';write_evidence(report)
                resumed_at=time.time()
                change('meta-worker-1',False)
                eventually(lambda:incident_ready('resolved',opened),'real lease recovery of same incident',timeout=150)
                resolved=incident_ready('resolved',opened); detail['resolved_incident']=resolved
                eventually(lambda:deliveries_ready(('opened','resolved'),resolved),'availability signed recovery',timeout=90)
                recovery=[r['payload'] for r in receiver.snapshot() if r['payload']['event_type']=='resolved']
                require(recovery and instance_timestamp(recovery[-1]['evidence']['observed_from'])>=resumed_at,'recovery predates real resume')
                require(intent.read_bytes()==original_intent,'pause/resume altered deployment intent')
            detail['paused_workers_restored']=True
            eventually(lambda:evaluated(2,0),'both original Worker leases restored',timeout=100)
            require({r['instance_id'] for r in worker_current_instances(admin,node['node_id'])}=={r['instance_id'] for r in rows},'resume replaced process identities')
            # A new browser login consumes only a fresh real MFA window; it must not mutate rules.
            artifacts=Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
            expected={'run_id':os.environ['ADDP_ONLINE_TEST_RUN_ID'],'admin_id':actor['principal']['id'],
                      'service_availability':{'rule':current_rule(),'incident':incident_ready('resolved',opened)}}
            (artifacts/'node-resources-browser-input.json').write_text(json.dumps(expected))
            report['stage']='service-availability-console';write_evidence(report)
            detail['browser']=validate_availability_browser(execute_resource_browser(artifacts),expected)
            admin=renew_admin(base,actor['principal']['id'])
            worker_lifecycle(directory,'stop',node['node_id'])
            eventually(lambda:current_rule()['last_verdict']=='paused','planned module stop pauses rule',timeout=70)
            end=time.monotonic()+76
            while time.monotonic()<end:
                require(incident_ready('resolved',opened) and len(incidents())==1,'planned stop created or changed an incident')
                time.sleep(1)
            require(deliveries_ready(('opened','resolved'),resolved), 'planned stop created an extra delivery')
            detail['planned_stop_no_new_incident']=True
            rule=admin.request('PUT',path,(200,),dict(body,version=rule['version'],enabled=False)).payload
            admin.request('DELETE',path,(200,),{'version':rule['version']});rule=None
            dest=admin.request('PUT',dest_path,(200,),dict(dest_body,version=dest['version'],enabled=False)).payload
            admin.request('DELETE',dest_path,(200,),{'version':dest['version']});dest=None
            require(not alert_page(admin,rule_query), 'availability rule remains after deletion')
            require(len(alert_page(admin,incident_query))==1, 'availability configuration deletion removed history')
            detail['configuration_removed_history_retained']=True
            detail['result']='passed'
    finally:
        for obj,root,data in ((rule,AVAILABILITY_RULES,body),(dest,ALERT_DESTINATIONS,dest_body)):
            if obj is not None and data is not None:
                try: admin.request('PUT',root+'/'+str(obj['id']),(200,),dict(data,version=obj['version'],enabled=False))
                except (SuiteError,OSError,ValueError): pass
        detail['receiver_closed']=receiver.closed
    require(receiver.closed and not (directory/FIXTURE.PAUSED_WORKERS).exists(), 'availability cleanup incomplete')
    retire_owned_configuration(admin,node,target,node_input,report)


def worker_profile():
    profile = FIXTURE.WORKER_PROFILES.get(acceptance_case())
    require(profile is not None, "Worker lifecycle requires a registered worker case")
    return profile


def worker_lifecycle(directory, action, node_id, unit=None):
    profile = worker_profile()
    module = profile["module"]
    require(FIXTURE.boundary() == directory, "Worker lifecycle requires the owned Hosted case")
    require(action in {"start", "stop", "stop-worker"} and canonical_uuid(node_id), "invalid Worker lifecycle input")
    if action == "stop-worker":
        require(unit in profile["replace"], "Worker stop selector outside the current case")
        command = ["bash", "scripts/dev/stop-exact-process.sh", "-"+unit]
    else:
        require(unit is None, "unexpected Worker selector")
        command = ["bash", "scripts/dev/"+action+".sh", "-"+module]
        if action == "start": command.append("-monitor")
    with (directory / (module+"-workers-"+action+".log")).open("a") as output:
        subprocess.run(command, cwd=FIXTURE.ROOT, env=dict(os.environ, ADDP_HOST_NODE_ID=node_id, SKIP_MODTIDY="1"),
                       stdout=output, stderr=subprocess.STDOUT, check=True, timeout=300)


def worker_current_instances(admin, node_id, *, rejected=False):
    profile = worker_profile()
    count = len(profile["ports"])
    rows = instance_list(admin, module_name=profile["module"], status="up")
    require(len(rows) == count and sum(r["role"] == "worker" for r in rows) == count-1
            and sum(r["role"] == "backend" for r in rows) == 1
            and len({r["instance_id"] for r in rows}) == count, "Worker case requires one Backend and its independent Workers")
    for row in rows:
        assert_host_binding(row, node_id, rejected=rejected, module_name=profile["module"], role=row["role"])
    return rows


def worker_slots(prom, instances):
    profile = worker_profile()
    module = profile["module"]
    rows = prom.query('addp_process_identity_info{job="addp_nodes",addp_monitor_kind="process_resources",addp_module_name="'+module+'",addp_runtime_role="worker"}')
    require(len(rows) == len(profile["ports"])-1, "all Worker SDK identities must be sampled")
    result = {}
    for row in rows:
        labels = row.get("metric", {})
        instance = labels.get("addp_instance_id")
        require(float(row["value"][1]) == 1 and instance == labels.get("runtime_instance_id")
                and labels.get("module_name") == module and labels.get("runtime_role") == "worker"
                and instance in {r["instance_id"] for r in instances if r["role"] == "worker"}, "Worker SDK identity differs from System")
        units = [unit for unit in profile["ports"] if unit != module
                 if labels.get("instance") == os.environ["ADDP_ONLINE_METRICS_NODE_IP"]+":"+str(profile["ports"][unit])]
        require(len(units) == 1 and units[0] not in result, "Worker source endpoint duplicated or unknown")
        result[units[0]] = next(r for r in instances if r["instance_id"] == instance)
        if module == "transfer":
            runtime_name = "sync" if units[0].startswith("transfer-bounded-worker-") else "continuous_sync"
            require(result[units[0]].get("metadata", {}).get("runtime_name") == runtime_name,
                    "Transfer source endpoint belongs to another Worker family")
        if module == "quality":
            require(result[units[0]].get("metadata", {}).get("runtime_name") == "quality-bounded",
                    "Quality source endpoint belongs to another Worker runtime")
    require(len({row["instance_id"] for row in result.values()}) == len(result), "Worker endpoints share one identity")
    return result


def worker_trends_ready(admin, workers, evidence):
    complete = True
    frames = []
    for owner in workers:
        end = datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0)
        query = {"instance_id": str(owner["id"]), "start": (end-datetime.timedelta(minutes=5)).isoformat().replace("+00:00", "Z"),
                 "end": end.isoformat().replace("+00:00", "Z")}
        path = "/api/v1/monitor/platform/process_resource_trends"
        value = admin.request("GET", path+"?"+urllib.parse.urlencode(query), (200,)).payload
        require(type(value.get("policy_version")) is int and value["policy_version"] >= 0, "Worker trend policy missing")
        frame = {"path": path, "query": query, "value": value}
        complete = assert_process_trend_evidence(frame, owner, value["policy_version"], require_valid=False) and complete
        frames.append(frame)
    if complete: evidence[:] = frames
    return complete


def validate_workers_browser(value, expected, artifacts):
    require(value.get("schema_version") == "addp.node-resources-browser/v1" and value.get("result") == "passed"
            and value.get("stage") == "complete" and value.get("run_id") == expected["run_id"], "Worker browser incomplete")
    platform_identity(value.get("identity", {}), "platform.system_administrator")
    require(value["identity"]["principal"]["id"] == expected["admin_id"], "Worker browser changed administrator")
    detail = value.get("module_workers", {})
    require(detail.get("reload_restored") is True and detail.get("host_navigation") is True, "Worker browser navigation incomplete")
    owners = expected["module_workers"]["workers"]
    batch = detail.get("summary", {})
    require(batch.get("path") == PROCESS_SUMMARIES and set(batch.get("query", {})) == {"instance_ids"}
            and sorted(batch["query"]["instance_ids"].split(',')) == sorted(str(r["id"]) for r in owners), "Worker browser summary expanded scope")
    assert_process_summaries(batch.get("value", {}), owners)
    frames = detail.get("trends", [])
    require(len(frames) == len(owners) and {f.get("query", {}).get("instance_id") for f in frames} == {str(r["id"]) for r in owners}, "Worker browser trends incomplete")
    for frame in frames:
        owner = next(r for r in owners if str(r["id"]) == frame["query"]["instance_id"])
        assert_process_trend_evidence(frame, owner, batch["value"]["data"][0]["policy_version"])
    for name in ("module-workers-list.png", "module-workers-trends.png"):
        image = artifacts / name
        require(image.is_file() and image.stat().st_size > 1000 and image.read_bytes()[:8] == b"\x89PNG\r\n\x1a\n", "Worker browser screenshot missing")
    return {"result": "passed", "worker_count": len(owners), "reload_restored": True, "host_navigation": True}


def run_workers_case(base, directory, admin, admin_identity, machine, denied, report):
    profile = worker_profile()
    module = profile["module"]
    count = len(profile["ports"])
    report["stage"] = module+"-worker-host-declaration"
    write_evidence(report)
    peers = current_processes(admin)
    node_input = {"display_name": "Online "+module+" Worker host "+os.environ["ADDP_ONLINE_TEST_RUN_ID"], "node_kind": "virtual",
                  "addresses": [os.environ["ADDP_ONLINE_METRICS_NODE_IP"]], "enabled": True,
                  "allowed_module_bindings": [{"module_name": module, "client_id": "addp-"+module}]}
    node = admin.request("POST", NODES, (201,), node_input).payload
    require(canonical_uuid(node.get("node_id")) and node.get("version") == 1, "module host creation invalid")
    worker_lifecycle(directory, "stop", node["node_id"])
    worker_lifecycle(directory, "start", node["node_id"])
    eventually(lambda: len(instance_list(admin, module_name=module, status="up")) == count, "Worker formal registration")
    rows = worker_current_instances(admin, node["node_id"])
    require({r["instance_id"] for r in peers} == {r["instance_id"] for r in current_processes(admin)}, "module launch changed peer instances")
    target = admin.request("POST", TARGETS, (201,), {"subject": {"kind": "node", "node_id": node["node_id"]},
                            "monitor_kind": "host_resources", "source": {"type": "node_exporter", "endpoint": "https://"+os.environ["ADDP_ONLINE_METRICS_NODE_IP"]+":19100/metrics"}, "enabled": True}).payload
    for client in denied:
        client.request("GET", process_path(rows), (403,))
        client.request("GET", INSTANCES+"?page=1&page_size=20&module_name="+module+"&status=up", (403,))
    eventually(lambda: process_query_ready(admin, rows), "all bound process windows", timeout=130)
    prom = Prometheus(directory)
    slots = worker_slots(prom, rows)
    if acceptance_case() == "service-availability":
        run_service_availability_case(base,directory,admin,admin_identity,denied,node,target,node_input,rows,slots,report)
        return
    source_ports = {r["instance_id"]: FIXTURE.PROCESS_PORTS[r["module_name"]] for r in peers}
    source_ports.update({r["instance_id"]: profile["ports"][module] for r in rows if r["role"] == "backend"})
    source_ports.update({owner["instance_id"]: profile["ports"][unit] for unit, owner in slots.items()})
    assert_process_discovery(machine.request("GET", DISCOVERY, (200,), response_type=list).payload, [*peers, *rows], host_count=1, source_ports=source_ports)
    trends = []
    eventually(lambda: worker_trends_ready(admin, list(slots.values()), trends), "all current Worker trends")
    detail = report["module_workers"] = {"initial_instances": rows, "initial_trends": trends, "same_host": node["node_id"]}
    pid_paths = {unit: FIXTURE.ROOT / ".dev-pids" / (unit+".pid") for unit in profile["ports"]}
    pids = {unit: path.read_text().strip() for unit, path in pid_paths.items()}
    stopped = []
    for replace_unit in profile["replace"]:
        old = slots[replace_unit]
        worker_lifecycle(directory, "stop-worker", node["node_id"], replace_unit)
        eventually(lambda: instance_list(admin, ids=str(old["id"]))[0]["status"] == "down", "stopped module Worker leaves active lease")
        survivors = instance_list(admin, module_name=module, status="up")
        require({r["instance_id"] for r in survivors} == {r["instance_id"] for r in rows if r["id"] != old["id"]}, "stopping one Worker affected its peers")
        eventually(lambda: process_query_ready(admin, [old, *survivors], inactive=[old["id"]]), "stopped Worker values clear while peers continue")
        worker_lifecycle(directory, "start", node["node_id"])
        eventually(lambda: len(instance_list(admin, module_name=module, status="up")) == count, "Worker formal registration")
        rows = worker_current_instances(admin, node["node_id"])
        require(all(pid_paths[unit].read_text().strip() == pids[unit] for unit in profile["ports"] if unit != replace_unit), "repair restarted healthy Backend or Worker")
        eventually(lambda: process_query_ready(admin, rows), "replacement Worker complete CPU window", timeout=130)
        slots = worker_slots(prom, rows)
        require(slots[replace_unit]["instance_id"] != old["instance_id"]
                and instance_timestamp(slots[replace_unit]["process_started_at"]) > instance_timestamp(old["process_started_at"])
                and {r["instance_id"] for r in survivors}.issubset({r["instance_id"] for r in rows}), "Worker repair reused old identity or replaced peer")
        assert_process_summaries(admin.request("GET", process_path([old, *rows]), (200,)).payload, [old, *rows], inactive=[old["id"]])
        stopped.append(old)
        pids = {unit: path.read_text().strip() for unit, path in pid_paths.items()}
    for disabled in (True, False):
        node = admin.request("PUT", NODES+"/"+node["node_id"], (200,), dict(node_input, enabled=not disabled, version=node["version"])).payload
        current = worker_current_instances(admin, node["node_id"], rejected=disabled)
        require({r["instance_id"] for r in current} == {r["instance_id"] for r in rows}, "host toggle changed module process identities")
        assert_process_summaries(admin.request("GET", process_path(current), (200,)).payload, current)
        rows = current
    detail.update(replacement_instances=rows, stopped_instances=stopped, peer_pid_unchanged=True, host_toggle_preserved_instances=True)
    repaired_pids = {unit: path.read_text().strip() for unit, path in pid_paths.items()}
    report["stage"] = module+"-worker-observation-outage"
    write_evidence(report)
    try:
        center_action("stop")
        failure = admin.request("GET", process_path(rows), (503, 504)).payload
        require("data" not in failure, "center outage returned old Worker values")
        observed = worker_current_instances(admin, node["node_id"])
        require({r["instance_id"] for r in observed} == {r["instance_id"] for r in rows}, "center outage changed Worker leases")
        require(all(path.read_text().strip() == repaired_pids[unit] for unit, path in pid_paths.items()), "center outage restarted module processes")
    finally:
        center_action("start")
    after = time.time()
    eventually(lambda: process_query_ready(admin, rows, after=after), "fresh Worker samples after center recovery", timeout=130)
    detail["center_outage_preserved_workers"] = True
    workers = [r for r in rows if r["role"] == "worker"]
    eventually(lambda: worker_trends_ready(admin, workers, trends), "replacement Worker current trends")
    artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
    expected = {"run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"], "admin_id": admin_identity["principal"]["id"],
                "module_workers": {"module": module, "host": node, "workers": workers}}
    (artifacts / "node-resources-browser-input.json").write_text(json.dumps(expected))
    report["stage"] = "console-"+module+"-workers"
    write_evidence(report)
    detail["browser"] = validate_workers_browser(execute_resource_browser(artifacts), expected, artifacts)
    worker_lifecycle(directory, "stop", node["node_id"])
    require(not instance_list(renew_admin(base, admin_identity["principal"]["id"]), module_name=module, status="up"), "standard module stop left active Worker leases")
    require(all(not path.exists() for path in pid_paths.values()), "standard module stop left unit PID files")
    detail["standard_stop_cleared_units"] = True
    retire_owned_configuration(renew_admin(base, admin_identity["principal"]["id"]), node, target, node_input, report)


def acceptance_case():
    value = os.environ.get("ADDP_ONLINE_NODE_METRICS_CASE", "regression")
    require(value in {"regression", "complete-comparison", "cpu-pressure", "host-admission", "engine-admission", "meta-workers", "transfer-workers", "quality-workers", "service-availability"}, "unsupported Hosted metrics acceptance case")
    return value


def renew_admin(base, principal_id):
    # The browser may have just consumed this principal's current TOTP.
    # Wait for a new natural counter before one fresh password/MFA exchange.
    time.sleep(30 - time.time() % 30 + 0.15)
    admin = login(base, "ADDP_ONLINE_METRICS_ADMIN")
    current = admin.request("GET", "/api/v1/system/auth/context", (200,)).payload
    platform_identity(current, "platform.system_administrator")
    require(current["principal"]["id"] == principal_id, "renewed metrics administrator changed")
    return admin


def wait_for_complete_comparison_history(admin, node, target, policy):
    keys = ("node.cpu.busy_percent", "node.memory.used_percent")
    started = time.monotonic()
    deadline = started+720
    attempts = 0
    while time.monotonic() <= deadline:
        path = resource_path(node, True, keys=keys, range_seconds=600)
        value = admin.request("GET", path, (200,)).payload
        attempts += 1
        assert_resources(value, node, target, trend=True, keys=keys, range_seconds=600, require_fresh=False)
        query = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)
        require(all(utc_timestamp(value[k]) == utc_timestamp(query[k][0]) for k in ("start", "end")), "comparison history query bounds changed")
        require(value["policy_version"] == policy["version"], "comparison history query policy changed")
        if all(row["points"] and all(point["data_state"] == "valid" for point in row["points"]) for row in value["series"]):
            return {"attempts": attempts, "duration_ms": round((time.monotonic()-started)*1000), "value": value}
        remaining = deadline-time.monotonic()
        if remaining <= 0:
            break
        time.sleep(min(15, remaining))
    raise SuiteError("complete comparison history did not converge within 720 seconds")


def run_complete_comparison_case(base, node, target, policy, node_input, admin_identity, security_identity, processes, report):
    report["stage"] = "natural-comparison-history"
    admin = renew_admin(base, admin_identity["principal"]["id"])
    report["comparison_history"] = wait_for_complete_comparison_history(admin, node, target, policy)
    report["stage"] = "console-complete-comparison"
    write_evidence(report)
    report["resource_browser"] = run_resource_browser(node, target, policy, node_input["display_name"], admin_identity, security_identity, processes)
    comparison = report["resource_browser"]["period_comparison"]
    require(comparison["complete_cases"] == 3 and comparison["incomplete_cases"] == 0, "all three browser comparisons must have complete samples")
    retire_owned_configuration(renew_admin(base, admin_identity["principal"]["id"]), node, target, node_input, report)


def retire_owned_configuration(admin, node, target, node_input, report):
    report["stage"] = "owned-configuration-cleanup"
    admin.request("DELETE", TARGETS+"/"+target["id"], (204,), {"version": target["version"]})
    admin.request("GET", TARGETS+"/"+target["id"], (404,))
    retired = admin.request("PUT", NODES+"/"+node["node_id"], (200,), dict(node_input, enabled=False, version=node["version"])).payload
    require(retired.get("node_id") == node["node_id"] and retired.get("enabled") is False and retired.get("version") == node["version"]+1, "comparison host retirement failed")
    report.update(configuration_retired=True, result="passed", stage="complete", cleanup="awaiting-hosted-deployment-destruction")


def run(base, directory, report):
    selected_case = acceptance_case()
    report["acceptance_case"] = selected_case
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
    report["identity_isolation"] = True
    if selected_case in FIXTURE.WORKER_PROFILES:
        run_workers_case(base, directory, admin, admin_identity, machine, [security, machine, *tenant_clients], report)
        return
    if selected_case == "engine-admission":
        run_engine_admission_case(base,directory,admin,admin_identity,[security,machine,*tenant_clients],report)
        return
    if selected_case == "host-admission":
        run_host_admission_case(base, directory, admin_identity, report)
        return
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
        client.request("GET", OVERVIEW, expected)
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
    report["resource_overview"] = assert_resource_overview(admin.request("GET", OVERVIEW, (200,)).payload, node, query_policy["version"], node_input["display_name"])
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
    if selected_case == "cpu-pressure":
        admin = renew_admin(base, admin_identity["principal"]["id"])
        run_resource_alert_lifecycle(admin, security, machine, tenant_clients, prom, node, target, query_policy, directory, report, pressure_kind="cpu")
        retire_owned_configuration(renew_admin(base, admin_identity["principal"]["id"]), node, target, node_input, report)
        return
    if selected_case == "complete-comparison":
        run_complete_comparison_case(base, node, target, query_policy, node_input, admin_identity, security_identity, processes, report)
        return
    report["stage"] = "process-stop-restart-isolation"
    processes = check_process_restart(admin, machine, directory, processes, report)
    report["stage"] = "process-optional-source-business-boundary"
    processes = check_optional_process_source(admin, machine, directory, processes, tenant_clients, report)
    report["process_instances"] = [{k: r[k] for k in ("id", "instance_id", "module_name", "role", "process_started_at", "node_id")} for r in processes]
    report["stage"] = "console-resource-browser"
    report["resource_browser"] = run_resource_browser(node, target, query_policy, node_input["display_name"], admin_identity, security_identity, processes)
    # Fault phases use a new formal password/MFA session for the same principal;
    # do not extend production TTL or depend on the browser's private session.
    admin = renew_admin(base, admin_identity["principal"]["id"])
    run_resource_alert_lifecycle(admin, security, machine, tenant_clients, prom, node, target, query_policy, directory, report)
    admin = renew_admin(base, admin_identity["principal"]["id"])
    report["stage"] = "center-query-outage-recovery"
    center_action("stop")
    try:
        failure = admin.request("GET", resource_path(node), (503, 504)).payload
        require(failure.get("error_code") in {"observability_backend_unavailable", "observability_query_timeout"}, "center failure became query success")
        failure = admin.request("GET", process_path(processes), (503, 504)).payload
        require(failure.get("error_code") in {"observability_backend_unavailable", "observability_query_timeout"}, "center failure became process query success")
        failure = admin.request("GET", OVERVIEW, (503, 504)).payload
        require(failure.get("error_code") in {"observability_backend_unavailable", "observability_query_timeout"} and "inventory" not in failure and "rankings" not in failure, "center failure became overview success")
        require({r["id"] for r in current_processes(admin)} == {r["id"] for r in processes}, "center failure broke service registration")
        API.GatewayClient(os.environ["MONITOR_URL"], "", 10).request("GET", "/health/ready", (200,))
        for client in tenant_clients:
            client.request("GET", "/api/v1/monitor/executions", (200,))
    finally:
        recovery = time.time()
        report["center_recovery_started_at"] = datetime.datetime.fromtimestamp(recovery, datetime.timezone.utc).isoformat().replace("+00:00", "Z")
        center_action("start")
    eventually(lambda: resource_query_after(admin, node, target, recovery), "new API query evidence after center recovery")
    eventually(lambda: resource_query_after(admin, node, target, recovery, FILESYSTEM_METRICS), "new filesystem evidence after center recovery")
    eventually(lambda: resource_query_after(admin, node, target, recovery, INODE_METRICS), "new inode evidence after center recovery")
    eventually(lambda: resource_query_after(admin, node, target, recovery, DISK_METRICS), "new disk evidence after center recovery")
    eventually(lambda: resource_query_after(admin, node, target, recovery, NETWORK_METRICS), "new network evidence after center recovery")
    report["center_recovery_process_summaries"] = {}
    eventually(lambda: process_query_ready(admin, processes, after=recovery, evidence=report["center_recovery_process_summaries"]), "new process evidence after center recovery", timeout=150)
    report["process_center_outage_recovery"] = True
    report["center_query_outage_recovery"] = True
    report["overview_center_recovery"] = assert_resource_overview(admin.request("GET", OVERVIEW, (200,)).payload, node, query_policy["version"], node_input["display_name"], after=recovery)
    report["stage"] = "source-outage-recovery"
    source_action("stop")
    try:
        eventually(lambda: source_scrape_failed(prom, expression), "source outage")
        require(prom.query('up{job="prometheus"} == 1'), "center failed with source")
        assert_source_process_isolation(admin, prom, processes, report)
        report["overview_source_failed"] = assert_source_overview_isolation(admin, prom, expression, node, query_policy["version"], node_input["display_name"], report)
    finally:
        source_resumed_at = time.time()
        source_action("start")
    eventually(lambda: bool(prom.query("up"+expression+" == 1")), "source recovery")
    eventually(lambda: resource_query_after(admin, node, target, source_resumed_at, ["node.cpu.busy_percent", "node.memory.used_percent"]), "overview source recovery full-window fresh samples")
    report["overview_source_recovered"] = assert_resource_overview(admin.request("GET", OVERVIEW, (200,)).payload, node, query_policy["version"], node_input["display_name"], after=source_resumed_at)
    report["stage"] = "control-outage-recovery"
    baseline = sum(float(row["value"][1]) for row in prom.query("prometheus_sd_http_failures_total"))
    owned_action("control-tls", "stop")
    try:
        eventually(lambda: sum(float(row["value"][1]) for row in prom.query("prometheus_sd_http_failures_total")) > baseline, "real control transport failure")
        require(active(), "failed discovery unexpectedly removed the last successful target")
        require(prom.query("up"+expression+" == 1"), "existing source scrape stopped with control plane")
        report["overview_control_unavailable"] = assert_resource_overview(admin.request("GET", OVERVIEW, (200,)).payload, node, query_policy["version"], node_input["display_name"])
    finally:
        owned_action("control-tls", "start")
    assert_discovery(machine, target, node["version"], processes)
    report["stage"] = "node-disable-and-resume"
    node = admin.request("PUT", NODES+"/"+node["node_id"], (200,), dict(node_input, enabled=False, version=node["version"])).payload
    assert_process_discovery(machine.request("GET", DISCOVERY, (200,), response_type=list).payload, processes)
    require(process_query_ready(admin, processes), "unbound process incorrectly disabled with host")
    report["overview_node_disabled"] = assert_resource_overview(admin.request("GET", OVERVIEW, (200,)).payload, node, query_policy["version"], node_input["display_name"], disconnected=True)
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
    report["overview_node_resumed"] = assert_resource_overview(admin.request("GET", OVERVIEW, (200,)).payload, node, query_policy["version"], node_input["display_name"], after=resumed_at)
    # The center has consumed both removal and this changed owner version, proving the actual SD transport recovered.
    report["overview_control_recovered"] = report["overview_node_resumed"]
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
    report["overview_target_disabled"] = assert_resource_overview(admin.request("GET", OVERVIEW, (200,)).payload, node, query_policy["version"], node_input["display_name"], disconnected=True)
    eventually(lambda: not prom.targets(node["node_id"]), "target disable application")
    admin.request("DELETE", target_path, (409,), {"version": 2})
    admin.request("DELETE", target_path, (204,), {"version": target["version"]})
    admin.request("GET", target_path, (404,))
    report["overview_target_removed"] = assert_resource_overview(admin.request("GET", OVERVIEW, (200,)).payload, node, query_policy["version"], node_input["display_name"], disconnected=True)
    admin.request("PUT", NODES+"/"+node["node_id"], (200,), dict(node_input, enabled=False, version=node["version"]))
    report.update(collector_grant_count=machine.grant_count, source_outage_recovery=True, control_outage_recovery=True, node_and_target_removal=True,
                  cleanup="awaiting-hosted-deployment-destruction", result="passed", stage="complete")


def write_evidence(report):
    path = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "platform-node-metrics.json"
    temporary = path.with_suffix(".json.tmp")
    try:
        temporary.write_text(json.dumps(report, sort_keys=True)+"\n")
        temporary.replace(path)
    finally:
        temporary.unlink(missing_ok=True)


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
            write_evidence(report)
    if report["result"] != "passed":
        return 1
    print(json.dumps(report, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
