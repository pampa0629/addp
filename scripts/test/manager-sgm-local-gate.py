#!/usr/bin/env python3
"""Manual, disposable ARM64 SGM page acceptance; never a Hosted/T4 profile."""
from __future__ import annotations

import base64
import copy
import hashlib
import importlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import secrets
import signal
import socket
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
T2 = importlib.import_module("scripts.test.supermap-sgm-gate")
MANAGER = importlib.import_module("scripts.test.manager-internal-artifact-lineage-online")
REGISTRATION = importlib.import_module("scripts.test.online-engine-registration")
LABEL = "com.addp.sgm-local"
BACKENDS = ("system-backend", "gateway", "meta-backend", "meta-worker", "manager-backend", "monitor-backend")
FRONTENDS = ("console", "system-frontend", "manager-frontend", "meta-frontend", "transfer-frontend",
             "orchestrator-frontend", "develop-frontend", "service-frontend", "nginx")
RUNTIMES = ("supermap-workflow-engine", "model3d-workflow-engine")
INFRA = ("postgres", "redis", "minio", "meilisearch")
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def preflight():
    T2.require(not os.environ.get("GITHUB_ACTIONS") and not os.environ.get("ADDP_ONLINE_HOSTED")
               and not os.environ.get("ADDP_ONLINE_OWNER_MANAGED"), "local SGM refuses GitHub profiles")
    T2.require(platform.system() in {"Darwin", "Linux"} and platform.machine() in {"arm64", "aarch64"},
               "local SGM requires a macOS/Linux ARM64 host")
    endpoint = os.environ.get("DOCKER_HOST") or json.loads(T2.command(["docker", "context", "inspect"]))[0]["Endpoints"]["docker"]["Host"]
    T2.require(endpoint.startswith("unix://"), "local SGM refuses remote Docker")
    T2.require(T2.command(["docker", "info", "--format", "{{.Architecture}}"]).strip() in {"arm64", "aarch64"},
               "local SGM refuses architecture emulation")
    node = T2.command(["node", "--version"]).strip().lstrip("v").split(".")[0]
    T2.require(node == (ROOT / ".node-version").read_text().strip(), "select the repository's declared Node major before local SGM acceptance")


def isolate_compose(document, selected, run_id, images):
    """Adapt ownership only. Retain the official service environment/entry/health."""
    result = {"name": run_id, "services": {}, "networks": {"isolated": {
        "name": run_id, "internal": True, "labels": {LABEL: run_id}}, "ingress": {
        "name": run_id + "-ingress", "labels": {LABEL: run_id}}}, "volumes": {}}
    for name in selected:
        service = copy.deepcopy(document["services"][name])
        T2.require("network_mode" not in service and not service.get("privileged"), "unexpected product network/privilege contract")
        service["container_name"] = run_id + "-" + name
        service["labels"] = {**service.get("labels", {}), LABEL: run_id}
        service["networks"] = {"isolated": {"aliases": [name]}}
        service["restart"] = "no"
        service["image"] = images.get(name, service.get("image"))
        service.pop("build", None)
        service.pop("depends_on", None)  # The caller starts each dependency in order.
        service.pop("ports", None)
        for mount in service.get("volumes", []):
            # No source, SDK, logs or data from the personal deployment is mounted.
            key = name + "-" + re.sub(r"[^a-zA-Z0-9]", "-", mount["target"]).strip("-")
            result["volumes"][key] = {"name": run_id + "-" + key, "labels": {LABEL: run_id}}
        # Replace mounts without relying on host paths or retaining read-only empty volumes.
        service["volumes"] = [{"type": "volume", "source": name + "-" + re.sub(r"[^a-zA-Z0-9]", "-", mount["target"]).strip("-"),
                               "target": mount["target"]} for mount in document["services"][name].get("volumes", [])]
        result["services"][name] = service
    for name, port in (("postgres", 5432), ("system-backend", 8180), ("gateway", 8000), ("nginx", 80)):
        if name in result["services"]:
            result["services"][name]["networks"]["ingress"] = {}
            result["services"][name]["ports"] = [{"target": port, "host_ip": "127.0.0.1", "published": "0", "protocol": "tcp"}]
    return result


def defaults():
    # Consume the same shell defaults as the standard lifecycle, never .env.
    environment = {key: value for key, value in os.environ.items() if key in {
        "PATH", "HOME", "TMPDIR", "DOCKER_HOST", "DOCKER_CONTEXT", "SSH_AUTH_SOCK",
        "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"}}
    text = T2.command(["bash", "-c", 'set -a; source .env.example; env -0'], environment=environment)
    values = dict(item.split("=", 1) for item in text.rstrip("\0").split("\0") if "=" in item)
    for key in values:
        if values[key] and key.endswith(("_PASSWORD", "_SECRET", "_KEY", "_PEPPER")):
            values[key] = secrets.token_hex(32)
    for key in ("ENCRYPTION_KEY", "IAM_MFA_ENCRYPTION_KEY", "OAUTH_USER_CODE_PEPPER"):
        values[key] = base64.b64encode(secrets.token_bytes(32)).decode()
    values.update(ENV="test", POSTGRES_DB="addp_online", POSTGRES_HOST="127.0.0.1",
                  POSTGRES_USER="sgm_fixture", ADDP_ONLINE_TEST="1", ADDP_ONLINE_LOCAL_SGM="1",
                  ADDP_OBSERVABILITY_LOGS_ENABLED="false", ADDP_OBSERVABILITY_METRICS_ENABLED="false",
                  COMPOSE_DISABLE_ENV_FILE="1", NO_PROXY="127.0.0.1,localhost", no_proxy="127.0.0.1,localhost")
    return values


def snapshot_module(checkout, name):
    module_name = "sgm_snapshot_" + name.replace("-", "_")
    spec = importlib.util.spec_from_file_location(module_name, checkout / "scripts/test" / (name + ".py"))
    module = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = module
    spec.loader.exec_module(module)
    return module


def published_port(life, service, port):
    info = json.loads(life.docker("inspect", life.run_id + "-" + service))[0]
    T2.require(info["Config"]["Labels"].get(LABEL) == life.run_id, "service ownership mismatch")
    bindings = info["NetworkSettings"]["Ports"].get(f"{port}/tcp")
    T2.require(isinstance(bindings, list) and len(bindings) == 1 and bindings[0]["HostIp"] == "127.0.0.1",
               "service must publish exactly one loopback binding")
    return int(bindings[0]["HostPort"])


def wait_url(url, timeout=180):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with OPENER.open(url, timeout=3) as response:
                if response.status == 200:
                    return
        except OSError:
            pass
        time.sleep(1)
    raise T2.GateError("isolated service readiness timed out")


def check_network_transport(life):
    """Exercise real Docker port publication before expensive licensed builds."""
    if not life.docker("image", "ls", "-q", "nginx:alpine").strip():
        life.docker("pull", "--platform", "linux/arm64", "nginx:alpine", timeout=900)
    image = T2.image_identity("nginx:alpine")["Id"]
    internal, ingress = life.run_id + "-transport-data", life.run_id + "-transport-ingress"
    life.docker("network", "create", "--internal", "--label", f"{LABEL}={life.run_id}", internal)
    life.docker("network", "create", "--label", f"{LABEL}={life.run_id}", ingress)
    observations = {}
    for name, dual in (("transport-internal", False), ("transport-control", True)):
        container = life.create(image, name, "--network", internal, "--publish", "127.0.0.1::80")
        if dual:
            life.docker("network", "connect", ingress, container)
        life.docker("start", container)
        info = json.loads(life.docker("inspect", container))[0]
        observations[name] = {"requested": info["HostConfig"]["PortBindings"], "actual": info["NetworkSettings"]["Ports"]}
        if dual:
            port = published_port(life, name, 80)
            wait_url(f"http://127.0.0.1:{port}/", timeout=30)
        life.docker("rm", "-fv", container)
    life.docker("network", "rm", internal, ingress)
    return {"image": image, "observations": observations, "control_loopback_http": True}


def worker(life, action, config):
    model = life.run_id + "-model3d-workflow-engine"
    life.copy(life.private_json("worker-" + action + ".json", config), model + ":/tmp/manager-sgm.json")
    code = (T2.ROOT / "scripts/test/manager-sgm-local-worker.py").read_bytes()
    return json.loads(life.docker("exec", "-i", model, "python", "-", action, data=code, timeout=120))


def model_tasks(client, fingerprint):
    payload = client.request("GET", "/api/v1/manager/tasks?task_type=model_3d_glb_generation&page=1&page_size=100", (200,)).payload
    rows = MANAGER._array(payload.get("items"), "local SGM task items")
    T2.require(payload.get("total") == len(rows), "task cleanup must inspect complete list")
    selected = [row for row in rows if row.get("config", {}).get("source", {}).get("item_fingerprint") == fingerprint]
    T2.require(len(selected) <= 1, "unexpected multiple SGM tasks for one source")
    return selected


def sgm_runtime_descriptor():
    # Same declared extension contract as System's workflow registration form.
    # System validates this declaration and probes the real health/operator APIs.
    return {"name": "Isolated SuperMap", "engine_type": "supermap_workflow", "engine_origin": "extension",
            "description": "Disposable local licensed Runtime",
            "connection_info": {"protocol": "http", "host": "supermap-workflow-engine", "port": 8103},
            "capabilities": {"schema_version": "engine.capabilities/v1", "engine_type": "supermap_workflow",
                "engine_family": "workflow", "compute": {"workflow": {"supported": True,
                    "runtime_api": "addp.workflow/v1", "dynamic_operators": True,
                    "supported_operator_mode": ["workflow", "direct"]}}}}


def register_engine(environment, token, descriptor, trace):
    client = MANAGER.GatewayClient(environment["SYSTEM_URL"], token, 30)

    def requester(base, bearer, method, path, body, expected):
        T2.require(base == environment["SYSTEM_URL"] and bearer == token, "registration identity changed")
        response = client.request(method, path, range(100, 600), body)
        event = {"engine_type": descriptor["engine_type"], "operation": "create" if path == "/api/v1/system/engines" else "test",
                 "http_status": response.status}
        payload = response.payload
        if isinstance(payload, dict):
            code = payload.get("error_code")
            if isinstance(code, str) and re.fullmatch(r"[a-z][a-z0-9_]{0,100}", code):
                event["error_code"] = code
            if isinstance(payload.get("success"), bool):
                event["connection_success"] = payload["success"]
        trace.append(event)
        T2.require(response.status in expected, "isolated engine registration failed with HTTP " + str(response.status))
        return payload

    return REGISTRATION.register_engine(environment["SYSTEM_URL"], token, descriptor, requester=requester)


def safe_failure(error):
    facts = {"type": type(error).__name__}
    match = re.search(r"returned HTTP ([1-5][0-9]{2}) \(([a-z][a-z0-9_]{0,100})\)", str(error))
    if match:
        facts.update(http_status=int(match[1]), error_code=match[2])
    return facts


def capability_facts(capability):
    return {key: value for key in ("status", "unavailable_reason", "source_kind", "render_source")
            if isinstance(value := capability.get(key), str) and re.fullmatch(r"[a-z][a-z0-9_]{0,100}", value)} | {
                "glb_generation_available": "generate_model_3d_glb" in capability.get("available_actions", [])}


def build_diagnostics(path):
    """Only fixed categories and numeric build facts leave private raw logs."""
    if not path.is_file():
        return {}
    raw = path.read_bytes()
    text = raw.decode(errors="replace")
    categories = [code for pattern, code in (
        ("Failed to fetch", "apt_fetch_failed"), ("502 Bad Gateway", "http_502"),
        ("503 Service Unavailable", "http_503"), ("CMake Error", "cmake_configuration_failed"),
        ("The following tests FAILED", "native_ctest_failed"),
        ("failed to solve", "docker_build_failed"), ("no space left on device", "disk_full"),
        ("i/o timeout", "network_timeout"), ("TLS handshake timeout", "tls_timeout"),
        ("cannot locate source identity fingerprint", "build_source_identity_missing"),
    ) if pattern in text]
    services = [{"service": name, "exit": int(code)} for name, code in re.findall(
        r"Image result: (supermap-workflow-engine|model3d-workflow-engine), exit=(\d+)", text)]
    return {"categories": categories or ["unclassified"], "services": services,
            "raw_log_sha256": hashlib.sha256(raw).hexdigest()}


def setup(life, environment, images):
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        public_port = reservation.getsockname()[1]
    origin = "http://127.0.0.1:" + str(public_port)
    environment.update(NGINX_BIND_HOST="127.0.0.1", NGINX_PORT=str(public_port), ADDP_PUBLIC_ORIGIN=origin,
                       ALLOWED_ORIGINS=origin)
    documents = {}
    for filename, names in (("docker-compose.infra.yml", INFRA), ("docker-compose.yml", BACKENDS + FRONTENDS),
                            ("docker-compose.runtimes.yml", RUNTIMES)):
        document = json.loads(T2.command(["docker", "compose", "--env-file", "/dev/null", "-f", filename,
                                          "config", "--format", "json"], environment=environment))
        documents.update({name: document["services"][name] for name in names})
    infra_images = {}
    for name in INFRA:
        if name not in images:
            reference = documents[name]["image"]
            if not life.docker("image", "ls", "-q", reference).strip():
                life.docker("pull", "--platform", "linux/arm64", reference, timeout=900)
            images[name] = T2.image_identity(reference)["Id"]
        infra_images[name] = images[name]
    config = isolate_compose({"services": documents}, documents, life.run_id, images)
    config["services"]["nginx"]["ports"][0]["published"] = str(public_port)
    # Business storage is a second instance of the fixed MinIO product, never Infra's bucket.
    business = copy.deepcopy(config["services"]["minio"])
    business.update(container_name=life.run_id + "-business-minio", networks={"isolated": {"aliases": ["business-minio"]}},
                    volumes=[{"type": "volume", "source": "business-data", "target": "/data"}])
    config["volumes"]["business-data"] = {"name": life.run_id + "-business-data", "labels": {LABEL: life.run_id}}
    config["services"]["business-minio"] = business
    path = life.private_json("compose.json", config)

    def compose(*args):
        return T2.command(["docker", "compose", "--env-file", "/dev/null", "-f", str(path), *args],
                          environment=environment, timeout=360)
    compose("up", "-d", "--wait", *INFRA, "business-minio")
    # Init uses the standard bucket/lifecycle owner, scoped to the generated Compose file.
    T2.command(["bash", "scripts/infra/init-minio.sh", "--compose-file", str(path)],
               environment={**environment, "ADDP_INFRA_RESOLVED": "1"})
    compose("up", "-d", "--wait", "system-backend")
    for name in ("meta-backend", "meta-worker", "manager-backend", "monitor-backend", "gateway"):
        compose("up", "-d", "--wait", name)
    compose("up", "-d", "--wait", *RUNTIMES)
    compose("up", "-d", "--wait", *FRONTENDS[:-1])
    compose("up", "-d", "--wait", "nginx")
    # Establish browser-visible origin after Docker assigns a collision-free port.
    console = "http://127.0.0.1:" + str(published_port(life, "nginx", 80))
    environment.update(CONSOLE_URL=console, SYSTEM_URL="http://127.0.0.1:" + str(published_port(life, "system-backend", 8180)),
                       GATEWAY_URL="http://127.0.0.1:" + str(published_port(life, "gateway", 8000)),
                       POSTGRES_PORT=str(published_port(life, "postgres", 5432)))
    wait_url(console + "/health")
    return infra_images


def run(life, result):
    global MANAGER, REGISTRATION
    life.phase = "source_snapshot"
    source = ROOT
    commit = T2.command(["git", "rev-parse", "HEAD"]).strip()
    # Never build other sessions' uncommitted source or touch their lifecycle state.
    for name in ("manager-sgm-local-gate.py", "manager-sgm-local-worker.py"):
        relative = "scripts/test/" + name
        expected = T2.command(["git", "show", f"{commit}:{relative}"])
        T2.require((ROOT / relative).read_text() == expected, "commit the local SGM harness before real acceptance")
    checkout = life.work / "checkout"
    T2.command(["git", "clone", "--local", "--no-hardlinks", "--single-branch", "--branch", "main", str(ROOT), str(checkout)], timeout=180)
    T2.ROOT = checkout
    T2.require(T2.command(["git", "rev-parse", "HEAD"]).strip() == commit, "main changed while snapshotting")
    MANAGER = snapshot_module(checkout, "manager-internal-artifact-lineage-online")
    REGISTRATION = snapshot_module(checkout, "online-engine-registration")
    environment = defaults()
    environment.update(IMAGE_TAG=life.run_id, REGISTRY="localhost:5001", SUPERMAP_WORKFLOW_BASE_IMAGE=T2.BASE,
                       ADDP_BUILD_GOMODCACHE=str(source / ".gomodcache"), ADDP_BUILD_GOCACHE=str(source / ".cache/go-build"))
    life.phase = "runtime_build"
    base_id, runtime_images, inputs = T2.build_images(life, failure_log=life.work / "runtime-build.log")
    result.update(source_commit=commit, source_input_sha256=inputs, MEDIA_SHA256=base_id.removeprefix("sha256:"))
    services = BACKENDS + FRONTENDS
    references = {name: f"localhost:5001/addp-{name}:{life.run_id}" for name in services}
    life.images.extend(references.values())
    life.phase = "platform_build"
    print("SGM local: build isolated platform from committed source", flush=True)
    T2.command(["make", "build", "BUILD_ARGS=--arch arm64 --services " + ",".join(BACKENDS)], timeout=3600, environment=environment)
    T2.command(["make", "build-images", "IMAGE_BUILD_ARGS=--verify --jobs 1 --services " + ",".join(services)
                + " --registry localhost:5001 --tag " + life.run_id], timeout=7200, environment=environment)
    images = {name: T2.image_identity(ref)["Id"] for name, ref in references.items()}
    images.update(zip(RUNTIMES, runtime_images[:2]))
    images["minio"] = runtime_images[3]
    result["images"] = images
    life.phase = "isolated_platform_start"
    result["infra_images"] = setup(life, environment, images)
    result["runtime_entries"] = {}
    for name in RUNTIMES:
        info = json.loads(life.docker("inspect", life.run_id + "-" + name))[0]
        expected = T2.image_identity(images[name])
        T2.require(info["Image"] == images[name] and info["State"]["Running"], "Runtime image identity mismatch")
        for field in ("Entrypoint", "Cmd"):
            T2.require(info["Config"].get(field) == expected["Config"].get(field), "Runtime default entry changed")
        result["runtime_entries"][name] = {key: info["Config"].get(key) for key in ("Entrypoint", "Cmd")}
    health_probe = '''import json, urllib.request
facts = {}
for module, port in (("system",8180),("gateway",8000),("meta",8082),("manager",8081),("monitor",8100)):
    host = module if module == "gateway" else module + "-backend"
    body = json.load(urllib.request.urlopen(f"http://{host}:{port}/health/live", timeout=10))
    assert body["module"] == module and body["status"] == "live"
    facts[module] = body["git_commit"]
print(json.dumps(facts))'''
    result["module_commits"] = json.loads(life.docker("exec", life.run_id + "-model3d-workflow-engine", "python", "-c", health_probe))
    T2.require(all(value == commit for value in result["module_commits"].values()), "platform commit differs from snapshot")
    licenses = life.docker("exec", life.run_id + "-supermap-workflow-engine", "sh", "-ec", "sha256sum /opt/supermap/bin/bin/*.lic12")
    result["LICENSE_SHA256"] = [line.split()[0] for line in licenses.splitlines()]
    life.phase = "private_fixture"
    fixture = life.create(base_id, "private-fixture", "--network", "none")
    model = life.run_id + "-model3d-workflow-engine"
    for remote, name in (("Resource/compass.SGM", "compass.SGM"), ("Resource/Textures/compass.png", "compass.png")):
        life.copy(fixture + ":/opt/supermap/bin/bin/" + remote, life.work / name)
        os.chmod(life.work / name, 0o600)
        life.copy(life.work / name, model + ":/tmp/" + name)
    cfg = {"root_user": environment["MINIO_ROOT_USER"], "root_secret": environment["MINIO_ROOT_PASSWORD"],
           "source_user": "sgm-source-" + secrets.token_hex(8), "source_secret": secrets.token_hex(32)}
    result["fixture"] = worker(life, "seed", cfg)
    business = life.run_id + "-business-minio"
    policy = life.private_json("source-policy.json", result["fixture"].pop("policy"))
    life.copy(policy, business + ":/tmp/source-policy.json")
    credentials = life.work / "source.env"
    credentials.write_text(f'SOURCE_USER={cfg["source_user"]}\nSOURCE_SECRET={cfg["source_secret"]}\n')
    life.copy(credentials, business + ":/tmp/source.env")
    life.docker("exec", business, "sh", "-ec", '''
      . /tmp/source.env
      mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null
      mc admin user add local "$SOURCE_USER" "$SOURCE_SECRET" >/dev/null
      mc admin policy create local sgm-source /tmp/source-policy.json >/dev/null
      mc admin policy attach local sgm-source --user "$SOURCE_USER" >/dev/null
      rm /tmp/source-policy.json /tmp/source.env
    ''')
    life.phase = "ordinary_identity"
    identity = life.work / "identity.env"
    T2.command(["bash", "-c", 'cd system/backend && GOWORK=off go run ./cmd/online-test-fixture --suite manager-internal-artifact-lineage --output "$1"', "_", str(identity)],
               environment=environment, timeout=300)
    exports = T2.command(["bash", "-c", 'set -a; source "$1"; env -0', "_", str(identity)], environment=environment)
    environment.update(dict(item.split("=", 1) for item in exports.rstrip("\0").split("\0") if item.startswith("ADDP_ONLINE_") and "=" in item))
    life.phase = "engine_registration"
    provisioner = environment.pop("ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN")
    result["registration_requests"] = []
    engine_id = register_engine(environment, provisioner, {
        "name": "Isolated SGM source", "engine_type": "minio", "engine_origin": "general", "description": "Disposable local SGM source",
        "connection_info": {"endpoint": "business-minio:9000", "access_key": cfg["source_user"], "secret_key": cfg["source_secret"], "use_ssl": False}}, result["registration_requests"])
    supermap_id = register_engine(environment, provisioner, sgm_runtime_descriptor(), result["registration_requests"])
    del provisioner
    client = MANAGER.GatewayClient(environment["GATEWAY_URL"], environment["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"], 30)
    result["identity"] = MANAGER.validate_user_identity(client, int(environment["ADDP_ONLINE_TEST_TENANT_ID"]))
    life.phase = "source_scan"
    MANAGER.wait_for_meta_scan(client, engine_id, time.monotonic() + 180)
    item = MANAGER.find_fixture_item(client, engine_id, "sgm-local/source/compass.SGM", "SGM")
    identity_facts = item.get("attributes", {}).get("item", {})
    T2.require(all(identity_facts.get(key) == value for key, value in {
        "data_type": "model_3d", "format": "sgm", "layout": "single"}.items()), "SGM scan identity mismatch")
    model_fixture = MANAGER.ArtifactFixture(format="sgm", item=item, locator=MANAGER.build_item_locator(engine_id, item))
    T2.require(not model_tasks(client, str(item["fingerprint"])), "stale SGM task")
    query = MANAGER.urllib.parse.urlencode({"locator": model_fixture.locator})
    capability = client.request("GET", "/api/v1/manager/quick-view/capability?" + query, (200,)).payload
    result["initial_capability"] = capability_facts(capability)
    sql = """SELECT coalesce(json_agg(json_build_object('engine_type',engine_type,'is_builtin',is_builtin,
        'tenant_id',tenant_id,'connection_status',connection_status,'lifecycle_state',lifecycle_state)), '[]')
        FROM system.engines WHERE engine_type IN ('model3d_workflow','supermap_workflow')"""
    result["runtime_registrations"] = json.loads(life.docker("exec", life.run_id + "-postgres", "psql", "-U",
        environment["POSTGRES_USER"], "-d", "addp_online", "-At", "-c", sql))
    model_fixture.previous_mode = capability["preferred_mode"]
    T2.require(model_fixture.previous_mode in {"basic_preview", "map_quick_view"}, "unknown initial preview mode")
    model_fixture.mode_changed = True
    artifact_dir = life.work / "browser"
    artifact_dir.mkdir(mode=0o700)
    environment.update(ADDP_ONLINE_TEST_RUN_ID=life.run_id, ADDP_ONLINE_ARTIFACT_DIR=str(artifact_dir),
                       ADDP_SGM_SOURCE_JSON=json.dumps({"locator": model_fixture.locator, **result["fixture"]}))
    print("SGM local: ordinary User, task, browser and cache assertions", flush=True)
    try:
        T2.require(result["initial_capability"]["glb_generation_available"], "SGM generation unavailable: " + result["initial_capability"].get("unavailable_reason", "unknown"))
        life.phase = "browser_prepare"
        T2.command(["npm", "--prefix", "console/frontend", "ci"], timeout=600, environment=environment)
        T2.command(["npm", "--prefix", "console/frontend", "exec", "--", "playwright", "install", "chromium"], timeout=600, environment=environment)
        life.phase = "browser_assertions"
        T2.command(["npm", "exec", "--prefix", "console/frontend", "--", "playwright", "test", "--config", "console/frontend/playwright.online.config.js",
                    "manager-sgm-local.spec.js"], timeout=600, environment=environment)
        browser = json.loads((artifact_dir / "sgm-browser.json").read_text())
        T2.require(browser.get("result") == "passed" and browser.get("run_id") == life.run_id, "browser evidence mismatch")
        model_fixture.task_id, model_fixture.execution_id = browser["task_id"], browser["execution_id"]
        model_fixture.result_id = browser["result_id"]
        life.phase = "artifact_assertions"
        execution = MANAGER.wait_for_manager_execution(client, model_fixture.execution_id, time.monotonic() + 180, "SGM")
        facts = execution.get("metadata", {})
        T2.require(facts.get("sgm_conversion", {}).get("operator") == "sgm_to_osgb"
                   and facts["sgm_conversion"].get("engine_id") == supermap_id
                   and facts.get("workflow_runtime", {}).get("operator") == "osgb_to_glb",
                   "Manager execution must use both production stages")
        serialized = json.dumps(execution)
        T2.require(all(value not in serialized for value in (cfg["root_secret"], cfg["source_secret"])),
                   "execution metadata leaked storage credentials")
        content = client.request("GET", browser["preview_url"], (200,), headers={"Accept": "model/gltf-binary"})
        glb = life.work / "result.glb"
        glb.write_bytes(content.raw)
        life.copy(glb, model + ":/tmp/manager-sgm.glb")
        result["conversion"] = worker(life, "verify", cfg)
        result["browser"] = browser
        result["task_status"] = execution["status"]
    finally:
        business_failed = sys.exc_info()[0] is not None
        failure_report = artifact_dir / "sgm-browser-failure.json"
        if failure_report.is_file():
            result["browser_failure"] = json.loads(failure_report.read_text())
        # Recover the one task by fingerprint even if a browser failure hid the response.
        cleanup_errors = []
        try:
            rows = model_tasks(client, str(item["fingerprint"]))
            if rows and model_fixture.task_id is None:
                task = rows[0]
                model_fixture.task_id = task["id"]
                model_fixture.execution_id = task.get("last_execution_id")
            MANAGER.cleanup_managed_artifact(client, model_fixture, 180)
        except Exception as error:
            cleanup_errors.append({"stage": "domain", **safe_failure(error)})
        result["domain_cleanup"] = model_fixture.cleanup
        try:
            result["physical_cleanup"] = worker(life, "deleted", cfg)
        except Exception as error:
            cleanup_errors.append({"stage": "physical", **safe_failure(error)})
        if cleanup_errors:
            result["artifact_cleanup_failures"] = cleanup_errors
            if not business_failed:
                raise T2.GateError("isolated SGM artifact cleanup failed")


def main(args=None):
    args = [] if args is None else args
    T2.require(args in ([], ["--check-network"]), "usage: local SGM [--check-network]")
    os.umask(0o077)
    preflight()  # No mutation, even temporary files, before deployment admission.
    destination = T2.report_path(os.environ["SGM_LOCAL_REPORT"]) if os.environ.get("SGM_LOCAL_REPORT") else None
    life = T2.Lifecycle(prefix="addp-sgm-local-", label=LABEL)
    result = {"schema_version": "addp.manager-sgm-local/v1", "scope": "local-isolated-technical-page-acceptance",
              "formal_t4": False, "run_id": life.run_id, "result": "failed"}
    try:
        life.phase = "network_preflight"
        result["network_probe"] = check_network_transport(life)
        if args:
            result["scope"] = "local-network-diagnostic"
        else:
            run(life, result)
        result["result"] = "passed"
    except (Exception, KeyboardInterrupt) as error:
        # Browser/SDK/backend errors can contain model data and credentials.
        result["failure_type"] = type(error).__name__
        result["failure_stage"] = getattr(life, "phase", "preflight")
        diagnostics = build_diagnostics(life.work / "runtime-build.log")
        if diagnostics:
            result["build_diagnostics"] = diagnostics
            print("SGM local build categories: " + ",".join(diagnostics["categories"]), file=sys.stderr)
        print("SGM local failed (" + type(error).__name__ + "); private output discarded", file=sys.stderr)
        if isinstance(error, T2.GateError):
            print(str(error), file=sys.stderr)
    finally:
        failures = life.cleanup()
        result.update(zero_residue=not failures, owned_image_tags=life.images)
        if failures:
            result.update(result="failed", cleanup_failures=failures)
        if destination:
            with open(destination, "x", opener=lambda p, f: os.open(p, f, 0o600)) as stream:
                json.dump(result, stream, indent=2)
                stream.write("\n")
    print(f"SGM local ({result['scope']}): {result['result']}; zero residue={result['zero_residue']}; formal T4=false", flush=True)
    return 0 if result["result"] == "passed" else 1


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(KeyboardInterrupt()))
    sys.exit(main(sys.argv[1:]))
