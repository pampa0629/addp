#!/usr/bin/env python3
"""Accept the real production Compose entry topology on a disposable Runner."""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import time
from functools import partial
from pathlib import Path
import urllib.error
import urllib.parse
import urllib.request
import uuid


class AcceptanceError(RuntimeError):
    pass


OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))
PUBLIC_PORT = 18080
MODULE_FRONTENDS = ("system", "meta", "manager", "transfer", "orchestrator", "workbench")
LOGIN_BACKENDS = {
    "workbench-backend": {"POSTGRES_HOST": "postgres", "POSTGRES_DB": "addp_online"},
    "asset-backend": {"POSTGRES_HOST": "postgres", "POSTGRES_DB": "addp_online"},
    "portal-backend": {"ASSET_URL": "http://asset-backend:8183", "REDIS_HOST": "redis"},
}
PUBLISHED_WORKBENCH = "ADDP_ONLINE_PUBLIC_ORIGIN_PUBLISHED_WORKBENCH"
WORKBENCH_PREPARATION_TOKEN = "ADDP_ONLINE_PUBLIC_WORKBENCH_PREPARATION_TOKEN"
ASSET_PUBLISHER_TOKEN = "ADDP_ONLINE_PUBLIC_ASSET_PUBLISHER_TOKEN"
WORKBENCH_APPLICATIONS = "/api/v1/workbench/data_applications"
QUERY_SERVICES = "/api/v1/service/query"
ASSET_ENTRIES = "/api/v1/asset/assets"
PORTAL_HOME = "/api/v1/portal/home"
WORKBENCH_PRESET_KEY = "person-a"
WORKBENCH_PRESET_PERSON_ID = "A"
WORKBENCH_DEFAULT_PERSON_ID = "B"


def docker_json(*args: str, env: dict[str, str] | None = None) -> object:
    result = subprocess.run(["docker", *args], check=True, capture_output=True, text=True, env=env)
    return json.loads(result.stdout)


def root_compose_ports() -> None:
    config = docker_json("compose", "-f", "docker-compose.yml", "config", "--format", "json")
    if not isinstance(config, dict) or not isinstance(config.get("services"), dict):
        raise AcceptanceError("production Compose config is invalid")
    published = {
        name: service["ports"]
        for name, service in config["services"].items()
        if isinstance(service, dict) and service.get("ports")
    }
    if set(published) != {"nginx"}:
        raise AcceptanceError("production Compose must publish only Nginx")
    ports = published["nginx"]
    if len(ports) != 1 or str(ports[0].get("published")) != str(PUBLIC_PORT) or ports[0].get("host_ip") != "127.0.0.1":
        raise AcceptanceError("Nginx must publish the selected loopback port")


def infra_compose_ports() -> None:
    config = docker_json("compose", "-f", "docker-compose.infra.yml", "config", "--format", "json")
    if not isinstance(config, dict) or not isinstance(config.get("services"), dict):
        raise AcceptanceError("Infra Compose config is invalid")
    ports = [port for service in config["services"].values() if isinstance(service, dict) for port in service.get("ports", [])]
    if not ports or any(port.get("host_ip") != "127.0.0.1" for port in ports):
        raise AcceptanceError("Hosted Infra must publish only loopback ports")


def running_container(name: str, project: str = "addp-platform") -> dict[str, object]:
    containers = docker_json("inspect", name)
    if not isinstance(containers, list) or len(containers) != 1:
        raise AcceptanceError(f"missing container {name}")
    container = containers[0]
    if not container["State"]["Running"] or container["Config"]["Labels"].get("com.docker.compose.project") != project:
        raise AcceptanceError(f"{name} is not an owned running Compose container")
    return container


def assert_platform_ports() -> None:
    expected = {"system-backend": ("8180/tcp", 8180), "gateway": ("8000/tcp", 8000), "addp-nginx": ("80/tcp", PUBLIC_PORT)}
    login_backends = dict(LOGIN_BACKENDS)
    if os.environ.get(PUBLISHED_WORKBENCH) == "1":
        login_backends["service-backend"] = {"POSTGRES_HOST": "postgres", "POSTGRES_DB": "addp_online", "SECURITY_URL": "http://security-backend:8194"}
        login_backends["standard-backend"] = {"POSTGRES_HOST": "postgres", "POSTGRES_DB": "addp_online"}
        login_backends["catalog-backend"] = {"POSTGRES_HOST": "postgres", "POSTGRES_DB": "addp_online",
                                             "STANDARD_URL": "http://standard-backend:8110", "WORKBENCH_URL": "http://workbench-backend:8193"}
    for name in ("system-backend", "gateway", "console", "security-backend", "meta-backend",
                 "manager-backend", "transfer-backend", "orchestrator-backend", *login_backends,
                 *(f"{module}-frontend" for module in MODULE_FRONTENDS), "portal-frontend", "addp-nginx"):
        container = running_container(name)
        bindings = container["HostConfig"].get("PortBindings") or {}
        if name in expected:
            internal, external = expected[name]
            expected_binding = [{"HostIp": "127.0.0.1", "HostPort": str(external)}]
            if bindings != {internal: expected_binding}:
                raise AcceptanceError(f"{name} published unexpected host ports")
        elif bindings:
            raise AcceptanceError(f"{name} must not publish host ports")
        env = dict(item.split("=", 1) for item in container["Config"]["Env"] if "=" in item)
        if name in login_backends:
            if container["State"].get("Health", {}).get("Status") != "healthy":
                raise AcceptanceError(f"{name} is not healthy")
            settings = {"SERVICE_HOST": name, "SYSTEM_URL": "http://system-backend:8180", **login_backends[name]}
            for key, value in settings.items():
                if env.get(key) != value:
                    raise AcceptanceError(f"{name} {key} differs from the internal topology")
        if name == "system-backend":
            for key in ("PUBLIC_API_URL", "CONSOLE_URL"):
                if env.get(key) != os.environ["ADDP_ONLINE_PUBLIC_ORIGIN"]:
                    raise AcceptanceError(f"System {key} differs from public origin")
        if name == "gateway":
            for key, value in {"SYSTEM_URL": "http://system-backend:8180", "POSTGRES_HOST": "postgres", "POSTGRES_DB": "addp_online", "REDIS_HOST": "redis"}.items():
                if env.get(key) != value:
                    raise AcceptanceError(f"Gateway {key} differs from the internal topology")
        if name == "meta-backend":
            if container["State"].get("Health", {}).get("Status") != "healthy":
                raise AcceptanceError("Meta Backend is not healthy")
            for key, value in {"SERVICE_HOST": "meta-backend", "SYSTEM_URL": "http://system-backend:8180", "POSTGRES_HOST": "postgres", "POSTGRES_DB": "addp_online"}.items():
                if env.get(key) != value:
                    raise AcceptanceError(f"Meta {key} differs from the internal topology")
        if name == "security-backend":
            if container["State"].get("Health", {}).get("Status") != "healthy":
                raise AcceptanceError("Security Backend is not healthy")
            for key, value in {"SERVICE_HOST": "security-backend", "SYSTEM_URL": "http://system-backend:8180", "POSTGRES_HOST": "postgres", "POSTGRES_DB": "addp_online"}.items():
                if env.get(key) != value:
                    raise AcceptanceError(f"Security {key} differs from the internal topology")
        if name in ("manager-backend", "transfer-backend") and env.get("SECURITY_URL") != "http://security-backend:8194":
            raise AcceptanceError(f"{name} Security address differs from the internal topology")
        if name == "manager-backend":
            if container["State"].get("Health", {}).get("Status") != "healthy":
                raise AcceptanceError("Manager Backend is not healthy")
            for key, value in {"SERVICE_HOST": "manager-backend", "SYSTEM_URL": "http://system-backend:8180", "META_URL": "http://meta-backend:8082", "POSTGRES_HOST": "postgres", "POSTGRES_DB": "addp_online", "REDIS_HOST": "redis", "MINIO_ENDPOINT": "minio:9000"}.items():
                if env.get(key) != value:
                    raise AcceptanceError(f"Manager {key} differs from the internal topology")
        if name == "transfer-backend":
            if container["State"].get("Health", {}).get("Status") != "healthy":
                raise AcceptanceError("Transfer Backend is not healthy")
            for key, value in {"SERVICE_HOST": "transfer-backend", "SYSTEM_URL": "http://system-backend:8180", "META_URL": "http://meta-backend:8082", "POSTGRES_HOST": "postgres", "POSTGRES_DB": "addp_online", "REDIS_HOST": "redis", "INFRA_KAFKA_BOOTSTRAP_SERVERS": "redpanda:29092", "KAFKA_CONNECT_URL": "http://kafka-connect:8083"}.items():
                if env.get(key) != value:
                    raise AcceptanceError(f"Transfer {key} differs from the internal topology")
        if name == "orchestrator-backend":
            if container["State"].get("Health", {}).get("Status") != "healthy":
                raise AcceptanceError("Orchestrator Backend is not healthy")
            for key, value in {"SERVICE_HOST": "orchestrator-backend", "SYSTEM_URL": "http://system-backend:8180", "POSTGRES_HOST": "postgres", "POSTGRES_DB": "addp_online", "REDIS_HOST": "redis"}.items():
                if env.get(key) != value:
                    raise AcceptanceError(f"Orchestrator {key} differs from the internal topology")


def assert_isolated_groups() -> None:
    runtimes = docker_json("compose", "-f", "docker-compose.runtimes.yml", "config", "--format", "json")
    business = docker_json(
        "compose", "--env-file", "/dev/null", "-f", "business/docker-compose.yml", "config", "--format", "json",
        env={**os.environ, "MINIO_BIND_HOST": "127.0.0.1", "MINIO_API_PORT": "19002", "MINIO_CONSOLE_PORT": "19003"},
    )
    if runtimes.get("name") != "addp-runtimes" or "geopython-workflow-engine" not in runtimes.get("services", {}):
        raise AcceptanceError("Runtime Compose project is invalid")
    if any(service.get("ports") for service in runtimes["services"].values()):
        raise AcceptanceError("Runtime Compose must not publish host ports")
    if business.get("name") != "business" or "minio" not in business.get("services", {}):
        raise AcceptanceError("Business Compose project is invalid")
    minio_ports = business["services"]["minio"].get("ports", [])
    if {(port.get("target"), port.get("published"), port.get("host_ip")) for port in minio_ports} != {
        (9000, "19002", "127.0.0.1"), (9001, "19003", "127.0.0.1")
    }:
        raise AcceptanceError("Business MinIO must publish only selected loopback ports")

    runtime = running_container("geopython-workflow-engine", "addp-runtimes")
    minio = running_container("business-minio", "business")
    for name, container, service in (
        ("geopython-workflow-engine", runtime, "geopython-workflow-engine"),
        ("business-minio", minio, "minio"),
    ):
        if container["Config"]["Labels"].get("com.docker.compose.service") != service:
            raise AcceptanceError(f"{name} has the wrong Compose service label")
        if container["State"].get("Health", {}).get("Status") != "healthy":
            raise AcceptanceError(f"{name} is not healthy")
    runtime_networks = set(runtime["NetworkSettings"]["Networks"])
    business_networks = set(minio["NetworkSettings"]["Networks"])
    if "addp-network" not in runtime_networks or "addp-network" in business_networks or runtime_networks & business_networks:
        raise AcceptanceError("Runtime and Business Compose networks are not isolated")
    if runtime["HostConfig"].get("PortBindings"):
        raise AcceptanceError("Runtime published a host port")
    if minio["HostConfig"].get("PortBindings") != {
        "9000/tcp": [{"HostIp": "127.0.0.1", "HostPort": "19002"}],
        "9001/tcp": [{"HostIp": "127.0.0.1", "HostPort": "19003"}],
    }:
        raise AcceptanceError("Business MinIO published unexpected host ports")
    subprocess.run(
        ["docker", "exec", "geopython-workflow-engine", "python", "-c",
         "import urllib.request; urllib.request.urlopen('http://system-backend:8180/health/live', timeout=5)"],
        check=True, capture_output=True, text=True, timeout=15,
    )
    with OPENER.open("http://127.0.0.1:19002/minio/health/live", timeout=5) as response:
        if response.status != 200:
            raise AcceptanceError("Business MinIO is not reachable through its selected loopback port")


def request(path: str, token: str | None = None, *, method: str = "GET", body: bytes | None = None) -> tuple[int, bytes, str]:
    headers = {"Accept": "application/json, text/html"}
    if token is not None:
        headers["Authorization"] = f"Bearer {token}"
    if body is not None:
        headers["Content-Type"] = "application/json"
    target = os.environ["ADDP_ONLINE_PUBLIC_ORIGIN"] + path
    try:
        with OPENER.open(urllib.request.Request(target, data=body, headers=headers, method=method), timeout=15) as response:
            return response.status, response.read(2_000_000), response.headers.get("Content-Type", "")
    except urllib.error.HTTPError as error:
        return error.code, error.read(2_000_000), error.headers.get("Content-Type", "")


def assert_frontend(path: str, asset_prefix: str) -> None:
    status, body, content_type = request(path)
    if status != 200 or "text/html" not in content_type or b'id="app"' not in body:
        raise AcceptanceError(f"{path} did not serve the real frontend entry")
    html = body.decode("utf-8")
    matches = re.findall(r'(?:src|href)="(' + re.escape(asset_prefix) + r'assets/[^\"]+)"', html)
    if not matches:
        raise AcceptanceError(f"{path} has no production asset under {asset_prefix}")
    status, asset, _ = request(matches[0])
    if status != 200 or not asset:
        raise AcceptanceError(f"{path} production asset is unavailable")


def assert_authorized_gateway() -> None:
    token = os.environ["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"]
    expected_tenant = os.environ["ADDP_ONLINE_TEST_TENANT_ID"]
    status, _, _ = request("/api/v1/system/auth/context")
    if status != 401:
        raise AcceptanceError("public Gateway accepted unauthenticated AuthContext")
    status, body, _ = request("/api/v1/system/auth/context", token)
    if status != 200:
        raise AcceptanceError(f"public Gateway AuthContext returned HTTP {status}")
    context = json.loads(body)
    if context.get("principal", {}).get("type") != "user" or context.get("context", {}).get("tenant_id") != expected_tenant:
        raise AcceptanceError("public Gateway AuthContext has the wrong Tenant User")
    authorization = context.get("authorization")
    if not isinstance(authorization, dict):
        raise AcceptanceError("public Gateway AuthContext omitted authorization")
    assignments = authorization.get("role_assignments") or []
    if not isinstance(assignments, list):
        raise AcceptanceError("public Gateway AuthContext has invalid role assignments")
    permissions = {
        permission
        for assignment in assignments
        for permission in assignment.get("permissions", [])
    }
    if permissions:
        raise AcceptanceError("public entry fixture User must have no granted permissions")
    status, _, _ = request("/api/v1/system/engine-types", token)
    if status != 403:
        raise AcceptanceError("public Gateway did not preserve System's engine permission guard")


def assert_context_permissions(token: str, expected: set[str]) -> dict:
    status, body, _ = request("/api/v1/system/auth/context", token)
    if status != 200:
        raise AcceptanceError(f"permission fixture AuthContext returned HTTP {status}")
    context = json.loads(body)
    if context.get("principal", {}).get("type") != "user" or context.get("context", {}).get("tenant_id") != os.environ["ADDP_ONLINE_TEST_TENANT_ID"]:
        raise AcceptanceError("permission fixture has the wrong Tenant User context")
    assignments = context.get("authorization", {}).get("role_assignments")
    if not isinstance(assignments, list):
        raise AcceptanceError("permission fixture AuthContext omitted assignments")
    if any(assignment.get("scope") != {"type": "tenant", "tenant_id": os.environ["ADDP_ONLINE_TEST_TENANT_ID"]} for assignment in assignments):
        raise AcceptanceError("permission fixture AuthContext contains an unrelated scope")
    granted = {
        key for assignment in assignments
        for key in assignment.get("permissions", [])
    }
    if granted != expected:
        raise AcceptanceError(f"permission fixture grants {sorted(granted)}, expected {sorted(expected)}")
    return context


def assert_partial_permission_matrix() -> None:
    reader = os.environ["ADDP_ONLINE_READ_USER_ACCESS_TOKEN"]
    creator = os.environ["ADDP_ONLINE_CREATE_USER_ACCESS_TOKEN"]
    administrator = os.environ["ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN"]
    assert_context_permissions(reader, {
        "meta.catalog.read", "manager.content.read", "manager.data_item.read", "transfer.task.read", "orchestrator.workflow.read",
    })
    assert_context_permissions(creator, {
        "meta.scan_task.create", "transfer.task.create", "orchestrator.workflow.create", "orchestrator.workflow.execute",
    })

    for module, path in (
        ("Meta", "/api/v1/meta/engines"),
        ("Manager", "/api/v1/manager/engines"),
        ("Transfer", "/api/v1/transfer/task-definitions"),
        ("Orchestrator", "/api/v1/orchestrator/orchestrations"),
    ):
        for token, expected, context in ((None, 401, "unauthenticated"), (reader, 200, "reader"), (creator, 403, "creator")):
            status, _, _ = request(path, token)
            if status != expected:
                raise AcceptanceError(f"{module} {context} list returned HTTP {status}, expected {expected}")

    for module, path in (
        ("Meta", "/api/v1/meta/scan/tasks"),
        ("Transfer", "/api/v1/transfer/task-definitions"),
        ("Orchestrator", "/api/v1/orchestrator/orchestrations"),
    ):
        for token, expected, context in ((None, 401, "unauthenticated"), (reader, 403, "reader"), (creator, 400, "creator")):
            status, _, _ = request(path, token, method="POST", body=b"{")
            if status != expected:
                raise AcceptanceError(f"{module} {context} malformed create returned HTTP {status}, expected {expected}")

    own_id = os.environ["ADDP_ONLINE_OWN_ASSIGNMENT_ID"]
    foreign_id = os.environ["ADDP_ONLINE_CROSS_TENANT_ASSIGNMENT_ID"]
    if not own_id.isdecimal() or not foreign_id.isdecimal() or own_id == foreign_id:
        raise AcceptanceError("permission fixture assignment identities are invalid")
    own_path = f"/api/v1/system/tenant/role_assignments/{own_id}"
    for token, expected, context in ((None, 401, "unauthenticated"), (reader, 403, "reader"), (administrator, 200, "administrator")):
        status, body, _ = request(own_path, token)
        if status != expected:
            raise AcceptanceError(f"System {context} assignment detail returned HTTP {status}, expected {expected}")
        if expected == 200 and json.loads(body).get("id") != own_id:
            raise AcceptanceError("System returned a different own-Tenant assignment")
    foreign_path = f"/api/v1/system/tenant/role_assignments/{foreign_id}"
    status, _, _ = request(foreign_path, administrator)
    if status != 404:
        raise AcceptanceError(f"System cross-Tenant assignment detail returned HTTP {status}, expected 404")


def assert_module_gateway_route(module: str, path: str) -> None:
    token = os.environ["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"]
    for attempt in range(30):
        status, _, _ = request(path, token)
        if status == 403:
            return
        if status != 503:
            raise AcceptanceError(f"public Gateway {module} route returned HTTP {status}, expected owner permission guard")
        if attempt < 29:
            time.sleep(1)
    raise AcceptanceError(f"public Gateway did not discover the registered {module} Backend")


def published_workbench_payload(service_id: int, fingerprint: str, run_id: str) -> dict[str, object]:
    component_id = str(uuid.uuid4())
    return {
        "name": f"Public origin Workbench {run_id}",
        "snapshot": {
            "schema_version": "addp.workbench_data_application/v1",
            "page": {"id": str(uuid.uuid4()), "title": "Public origin data", "display_mode": "desktop",
                     "refresh_interval_seconds": 0, "visible_sections": ["title", "parameters", "query_actions"],
                     "placements": [{"component_id": component_id, "x": 0, "y": 0, "width": 12, "height": 6}]},
            "components": [{
                "id": component_id, "title": "People",
                "service_ref": {"service_type": "query", "service_id": service_id},
                "contract_fingerprint": fingerprint,
                "parameter_definitions": [
                    {"key": "person_id", "label": "Person ID", "control_type": "text", "required": True},
                ],
                "query_template": {
                    "select": ["person_id"], "order_by": [{"field": "person_id", "direction": "asc"}],
                    "parameter_filters": [{"parameter_key": "person_id", "field": "person_id", "operator": "eq"}],
                    "page_limit": 10, "format": "json",
                },
                "renderer_type": "table", "renderer_config": {"columns": ["person_id"]},
            }],
            "parameters": [{
                "key": "person_id", "label": "Person ID", "control_type": "text", "required": True,
                "default_value": WORKBENCH_DEFAULT_PERSON_ID,
            }],
            "parameter_bindings": [{
                "application_parameter_key": "person_id", "component_id": component_id, "component_parameter_key": "person_id",
            }],
            "parameter_presets": [{
                "key": WORKBENCH_PRESET_KEY, "name": "Person A", "parameter_values": {"person_id": WORKBENCH_PRESET_PERSON_ID},
            }],
        },
    }


def fixture_request(token: str, method: str, path: str, status: int, body: dict | None = None):
    code, raw, _ = request(path, token, method=method, body=None if body is None else json.dumps(body).encode())
    if code != status:
        raise AcceptanceError(f"{method} {path} returned HTTP {code}, expected {status}")
    return json.loads(raw) if raw else None


def wait_for_workbench_catalog_entry(api, application_id: str) -> dict:
    reference = {"source_module": "workbench", "source_type": "data_application", "source_identity": application_id}
    api("GET", "/api/v1/catalog/entries?view=inventory&page=1&page_size=1", 200)
    deadline = time.monotonic() + 150
    while time.monotonic() < deadline:
        result, = api("POST", "/api/v1/catalog/entries/resolve-sources", 200, {"references": [reference]})["results"]
        if result["found"]:
            entry = result["entry"]
            if (any(result[key] != value for key, value in reference.items())
                    or entry["source_status"] != "active" or entry["governance_status"] != "discovered"):
                raise AcceptanceError("Catalog did not resolve the new active Workbench application")
            return entry
        time.sleep(2)
    raise AcceptanceError("Workbench CatalogEntry did not converge")


def run_published_portal_browser(workbench: dict, run_id: str) -> dict[str, object]:
    token = os.environ[ASSET_PUBLISHER_TOKEN]
    api = partial(fixture_request, token)
    publisher = assert_context_permissions(token, {
        "standard.domain.create",
        "catalog.entry.read", "catalog.entry.update", "catalog.inventory.read",
        "asset.management.read", "asset.entry.read", "asset.entry.update", "asset.entry.publish",
        "asset.entry.offline", "asset.entry.delete",
    })["principal"]["id"]
    if publisher == os.environ["ADDP_ONLINE_PUBLIC_MFA_PRINCIPAL_ID"]:
        raise AcceptanceError("Portal fixture publisher must be separate from the browser User")

    name = f"Public origin asset {run_id}"
    description = "Published Workbench application for Portal acceptance"
    domain = api("POST", "/api/v1/standard/domains", 201, {"code": "public_origin", "name": name})
    entry = wait_for_workbench_catalog_entry(api, workbench["application_id"])
    entry_id = entry["id"]
    curated = api("PUT", f"/api/v1/catalog/entries/{entry_id}", 200, {
        "version": entry["version"], "business_name": name, "business_description": description,
        "governance_status": "curated", "visibility": "tenant",
        "domains": [{"id": str(domain["id"]), "role": "primary"}],
        "responsibilities": [
            {"role": "accountable_department", "subject_type": "department",
             "subject_id": os.environ["ADDP_ONLINE_PUBLIC_ASSET_DEPARTMENT_ID"]},
            {"role": "business_owner", "subject_type": "user", "subject_id": publisher},
            {"role": "data_steward", "subject_type": "user", "subject_id": publisher},
        ],
    })
    if curated["id"] != entry_id or curated["governance_status"] != "curated":
        raise AcceptanceError("Workbench CatalogEntry did not become curated")
    types = api("GET", "/api/v1/asset/type-definitions", 200)
    application_types = [item for item in types if item["enabled"] and item["code"] == "application"]
    if len(application_types) != 1:
        raise AcceptanceError("Asset did not expose the enabled data application type")
    asset = api("POST", ASSET_ENTRIES, 201, {
        "name": name, "description": description, "type_id": application_types[0]["id"],
        "components": [{"catalog_entry_id": entry_id, "role": "primary", "sort_order": 0}],
    })
    asset_path = f"{ASSET_ENTRIES}/{asset['id']}"
    portal_path = f"/api/v1/portal/assets/{asset['id']}"
    try:
        api("POST", f"{asset_path}/publish", 200)
        browser = run_browser({**workbench, "portal_asset": {"id": asset["id"], "name": name, "description": description}})
    finally:
        current = api("GET", asset_path, 200)
        if current["status"] == "published":
            api("POST", f"{asset_path}/offline", 200)
        try:
            api("GET", portal_path, 404)
            home = api("GET", PORTAL_HOME, 200)
            if home["total_published"] != 0 or home["latest_assets"]:
                raise AcceptanceError("Portal home did not become empty after offlining the asset")
        finally:
            api("DELETE", asset_path, 200)
            api("GET", asset_path, 404)
    browser["published_portal_cleanup"] = "offline_and_asset_deleted"
    return browser


def run_published_workbench_browser() -> dict[str, object]:
    token = os.environ[WORKBENCH_PREPARATION_TOKEN]
    engine_id = int(os.environ["ADDP_ONLINE_CONSUMER_ENGINE_ID"])
    run_id = os.environ["ADDP_ONLINE_TEST_RUN_ID"]
    api = partial(fixture_request, token)

    context = assert_context_permissions(token, {
        "asset.entry.read", "service.data_read.execute",
        "service.definition.read", "service.definition.create", "service.definition.delete",
        "workbench.data_application.execute", "workbench.data_application.read",
        "workbench.data_application.create", "workbench.data_application.publish",
    })
    if (context["principal"]["id"] != os.environ["ADDP_ONLINE_PUBLIC_MFA_PRINCIPAL_ID"]
            or context["context"]["tenant_membership_id"] != os.environ["ADDP_ONLINE_PUBLIC_MFA_MEMBERSHIP_ID"]):
        raise AcceptanceError("published Workbench preparation identity does not match the browser User")

    sql = "SELECT person_id FROM public.metric_people"
    contract = api("POST", "/api/v1/service/sql/output-contract", 200, {"engine_id": engine_id, "sql": sql})
    service_name = f"public-origin-{run_id}"
    service = api("POST", QUERY_SERVICES, 201, {
        "service_name": service_name, "title": service_name, "config_type": "sql", "engine_id": engine_id,
        "sql_query": sql, "output_contract": contract, "public_access": False, "max_features": 10,
        "data_config": {"stable_key": ["person_id"], "default_fields": ["person_id"], "filterable_fields": ["person_id"]},
        "protocols": {"rest_api": {"enabled": True, "formats": ["json"]}},
    })
    service_path = f"{QUERY_SERVICES}/{service['id']}"
    app_path = None
    try:
        descriptor = api("GET", f"/api/v1/service/consumer/services/query/{service['id']}", 200)
        app = api("POST", WORKBENCH_APPLICATIONS, 201,
                  published_workbench_payload(service["id"], descriptor["contract_fingerprint"], run_id))
        app_path = f"{WORKBENCH_APPLICATIONS}/{app['id']}"
        published = api("POST", f"{app_path}/publish", 200, {"version": app["version"]})
        revision = published["current_revision_number"]
        if published["publication_status"] != "published" or type(revision) is not int or revision <= 0:
            raise AcceptanceError("Workbench did not return a published revision")
        browser = run_published_portal_browser({
            "application_id": app["id"], "published_revision": revision, "preset_key": WORKBENCH_PRESET_KEY,
            "default_person_id": WORKBENCH_DEFAULT_PERSON_ID, "query_path": f"/api/query/{service_name}/query",
            "query_filter": {"field": "person_id", "op": "eq", "value": WORKBENCH_PRESET_PERSON_ID},
            "table_values": [WORKBENCH_PRESET_PERSON_ID],
        }, run_id)
    finally:
        try:
            if app_path is not None:
                current = api("GET", app_path, 200)
                if current["publication_status"] == "published":
                    api("POST", f"{app_path}/offline", 200, {"version": current["version"]})
                    api("GET", f"{app_path}/runtime", 409)
        finally:
            api("DELETE", service_path, 200, {"version": service["version"]})
            api("GET", service_path, 404)
    browser["published_workbench_cleanup"] = "offline_and_service_deleted"
    return browser


def run_browser(published_workbench: dict | None = None) -> dict[str, object]:
    for key in ("ADDP_ONLINE_ARTIFACT_DIR", "ADDP_ONLINE_SECRET_DIR", "ADDP_ONLINE_TEST_RUN_ID",
                "ADDP_ONLINE_READ_USER_USERNAME", "ADDP_ONLINE_READ_USER_PASSWORD",
                "ADDP_ONLINE_PUBLIC_MFA_USERNAME", "ADDP_ONLINE_PUBLIC_MFA_PASSWORD", "ADDP_ONLINE_PUBLIC_MFA_TOTP",
                "ADDP_ONLINE_PUBLIC_MFA_PRINCIPAL_ID", "ADDP_ONLINE_PUBLIC_MFA_MEMBERSHIP_ID",
                "ADDP_ONLINE_PUBLIC_MFA_ALTERNATE_MEMBERSHIP_ID"):
        if not os.environ.get(key):
            raise AcceptanceError(f"missing browser environment: {key}")
    artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]).resolve()
    report_file = artifacts / "public-origin-browser.json"
    screenshots = [artifacts / f"public-origin-{name}.png" for name in ("console", "independent")]
    login_reports = {entry: artifacts / f"public-origin-{entry}-login.json" for entry in ("workbench", "portal")}
    for path in [report_file, *screenshots, *login_reports.values()]:
        path.unlink(missing_ok=True)
    # Playwright writes a DOM snapshot on failures even with screenshots off.
    private_output = Path(os.environ["ADDP_ONLINE_SECRET_DIR"]).resolve() / "public-origin-playwright"
    browser_env = {**os.environ, "CONSOLE_URL": os.environ["ADDP_ONLINE_PUBLIC_ORIGIN"],
                   "ADDP_ONLINE_PUBLIC_ORIGIN_BROWSER_REPORT": str(report_file)}
    browser_env.pop(WORKBENCH_PREPARATION_TOKEN, None)
    browser_env.pop(ASSET_PUBLISHER_TOKEN, None)
    browser_env.pop("ADDP_ONLINE_PUBLIC_ORIGIN_WORKBENCH_FIXTURE", None)
    if published_workbench is not None:
        browser_env["ADDP_ONLINE_PUBLIC_ORIGIN_WORKBENCH_FIXTURE"] = json.dumps(published_workbench)
    result = subprocess.run(
        ["npm", "exec", "--", "playwright", "test", "e2e/online/compose-public-origin.spec.js",
         "--config=playwright.online.config.js", "--output", str(private_output)],
        cwd=Path(__file__).resolve().parents[2] / "console/frontend",
        env=browser_env,
    )
    if result.returncode != 0 or not report_file.is_file():
        raise AcceptanceError("production browser acceptance failed or omitted evidence")
    evidence = json.loads(report_file.read_text())
    expected = {
        "run_id": os.environ["ADDP_ONLINE_TEST_RUN_ID"], "origin": os.environ["ADDP_ONLINE_PUBLIC_ORIGIN"],
        "tenant_id": os.environ["ADDP_ONLINE_TEST_TENANT_ID"], "username": os.environ["ADDP_ONLINE_READ_USER_USERNAME"],
        "concurrent_refreshes": 1, "iframe_converged": True, "cookie_rotated": True,
        "reload_without_refresh": True, "logout_propagated": True, "javascript_tokens_persisted": False,
        "iframe_preserved_on_refresh": True,
    }
    if (not isinstance(evidence, dict) or type(evidence.get("concurrent_refreshes")) is not int
            or type(evidence.get("iframe_preserved_on_refresh")) is not bool
            or set(evidence) != set(expected)
            or any(evidence.get(key) != value for key, value in expected.items())):
        raise AcceptanceError("production browser evidence differs from the required identity and session results")
    if any(not path.is_file() or path.stat().st_size == 0 for path in screenshots):
        raise AcceptanceError("production browser screenshot evidence is missing")
    login_fields = {
        "run_id": expected["run_id"], "origin": expected["origin"], "tenant_id": expected["tenant_id"],
        "mfa": True, "explicit_tenant_selection": True, "full_return_url": True, "http_only_refresh": True,
        "reload_preserved_identity": True, "cross_module_session_reused": True,
        "browser_process_restarted": True, "restart_restored_identity": True,
        "restart_cookie_rotated": True, "browser_profile_removed": True,
        "workbench_runtime_status": 404, "portal_home_status": 200,
    }
    if published_workbench is not None:
        login_fields.update(workbench_runtime_status=200, workbench_application_id=published_workbench["application_id"],
                            workbench_published_revision=published_workbench["published_revision"],
                            workbench_preset_key=published_workbench["preset_key"],
                            workbench_query_filter=published_workbench["query_filter"],
                            workbench_table_values=published_workbench["table_values"],
                            portal_published_asset=published_workbench["portal_asset"])
    login_evidence = {}
    for entry, path in login_reports.items():
        login_expected = {**login_fields, "entry": entry}
        if not path.is_file():
            raise AcceptanceError(f"{entry} production login evidence is missing")
        payload = json.loads(path.read_text())
        if (not isinstance(payload, dict) or payload != login_expected
                or any(type(payload[key]) is not type(value) for key, value in login_expected.items())):
            raise AcceptanceError(f"{entry} production login evidence differs from the required results")
        login_evidence[entry] = payload
    evidence["public_login"] = login_evidence
    return evidence


def main() -> int:
    if os.environ.get("ADDP_ONLINE_TEST") != "1" or os.environ.get("ADDP_ONLINE_HOSTED") != "1":
        raise AcceptanceError("disposable Hosted Online context is required")
    if os.environ.get("ADDP_ONLINE_PUBLIC_ORIGIN") != "http://127.0.0.1:18080":
        raise AcceptanceError("public origin must use the selected nondefault loopback port")
    root_compose_ports()
    infra_compose_ports()
    assert_platform_ports()
    assert_isolated_groups()
    assert_frontend("/", "/")
    for module in MODULE_FRONTENDS:
        assert_frontend(f"/module-ui/{module}/", f"/module-ui/{module}/")
    assert_frontend("/portal/", "/portal/")
    assert_authorized_gateway()
    assert_module_gateway_route("Meta", "/api/v1/meta/engines")
    assert_module_gateway_route("Manager", "/api/v1/manager/engines")
    assert_module_gateway_route("Transfer", "/api/v1/transfer/task-definitions")
    assert_module_gateway_route("Orchestrator", "/api/v1/orchestrator/orchestrations")
    assert_module_gateway_route("Workbench", "/api/v1/workbench/data_applications")
    assert_module_gateway_route("Portal", PORTAL_HOME)
    assert_partial_permission_matrix()
    if os.environ.get(PUBLISHED_WORKBENCH) == "1":
        assert_module_gateway_route("Service", QUERY_SERVICES)
        assert_module_gateway_route("Standard", "/api/v1/standard/domains")
        assert_module_gateway_route("Catalog", "/api/v1/catalog/entries")
        assert_module_gateway_route("Asset", ASSET_ENTRIES)
        browser = run_published_workbench_browser()
    else:
        browser = run_browser()
    print(json.dumps({"schema_version": "addp.online-suite/v1", "suite": "compose-public-origin", "public_port": PUBLIC_PORT, "frontends": ["console", *MODULE_FRONTENDS, "portal"], "gateway_auth_context": "passed", "gateway_engine_permission_guard": "passed", "gateway_meta_permission_guard": "passed", "gateway_manager_permission_guard": "passed", "gateway_transfer_permission_guard": "passed", "gateway_orchestrator_permission_guard": "passed", "gateway_partial_permission_matrix": "passed", "compose_projects": ["addp-infra", "addp-platform", "addp-runtimes", "business"], "browser": browser}, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (AcceptanceError, KeyError, ValueError, subprocess.CalledProcessError, urllib.error.URLError) as error:
        print(f"Compose public origin acceptance failed: {error}", file=sys.stderr)
        raise SystemExit(1)
