"""Real Platform identity, source admission and native Prometheus discovery T4."""
from __future__ import annotations

import base64
import hashlib
import hmac
import json
import math
import os
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
REQUIRED = {"platform.host_node.create", "platform.host_node.read", "platform.host_node.update",
            "monitor.monitoring_target.create", "monitor.monitoring_target.read",
            "monitor.monitoring_target.update", "monitor.monitoring_target.delete"}


def require(ok, message):
    if not ok:
        raise SuiteError(message)


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
        ids = FIXTURE.command(["docker", "ps", "-q", "--filter", "label=com.docker.compose.project=addp-infra", "--filter", "label=com.docker.compose.service=prometheus"]).split()
        require(len(ids) == 1, "expected the standard Infra metrics center")
        address = FIXTURE.command(["docker", "port", ids[0], "9090/tcp"]).strip()
        require(address.startswith("127.0.0.1:") and len(address.splitlines()) == 1, "center must publish only the owned loopback port")
        self.base = "https://"+address

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


def run(base, directory, report):
    report["stage"] = "platform-login-mfa"
    admin = login(base, "ADDP_ONLINE_METRICS_ADMIN")
    security = login(base, "ADDP_ONLINE_METRICS_SECURITY")
    admin_identity = admin.request("GET", "/api/v1/system/auth/context", (200,)).payload
    platform_identity(admin_identity, "platform.system_administrator")
    platform_identity(security.request("GET", "/api/v1/system/auth/context", (200,)).payload, "platform.security_administrator")
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
    for metric in ("node_cpu_seconds_total", "node_memory_MemTotal_bytes", "node_load1", "node_disk_reads_completed_total", "node_network_receive_bytes_total", "node_boot_time_seconds"):
        rows = prom.query(metric+expression)
        require(rows and all(math.isfinite(float(row["value"][1])) for row in rows), "missing or invalid resource metric "+metric)
        require(all(row["metric"].get("addp_source") == "node_exporter" and row["metric"].get("addp_monitor_kind") == "host_resources" for row in rows), "resource identity labels mismatch")
    report["applied_version"], report["resource_metrics"] = 2, True
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
    eventually(lambda: not prom.targets(node["node_id"]), "disabled node removal after control recovery")
    node = admin.request("PUT", NODES+"/"+node["node_id"], (200,), dict(node_input, version=node["version"])).payload
    assert_discovery(machine, target, node["version"])
    eventually(active, "re-enabled node sampling")
    # Native SD + fresh sample prove recovery; historical range data alone does not.
    eventually(lambda: bool(prom.query("timestamp(node_memory_MemTotal_bytes"+expression+") > "+str(since))), "new resource sample after recovery")
    report["stage"] = "target-disable-and-delete"
    target = admin.request("PUT", target_path, (200,), dict(body, enabled=False, version=target["version"])).payload
    require(machine.request("GET", DISCOVERY, (200,), response_type=list).payload == [], "disabled target remains in discovery")
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
    except (OSError, ValueError, KeyError, SuiteError, subprocess.SubprocessError):
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
