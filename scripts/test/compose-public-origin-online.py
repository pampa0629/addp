#!/usr/bin/env python3
"""Accept the real production Compose entry topology on a disposable Runner."""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request


class AcceptanceError(RuntimeError):
    pass


OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))
PUBLIC_PORT = 18080


def docker_json(*args: str) -> object:
    result = subprocess.run(["docker", *args], check=True, capture_output=True, text=True)
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


def running_container(name: str) -> dict[str, object]:
    containers = docker_json("inspect", name)
    if not isinstance(containers, list) or len(containers) != 1:
        raise AcceptanceError(f"missing container {name}")
    container = containers[0]
    if not container["State"]["Running"] or container["Config"]["Labels"].get("com.docker.compose.project") != "addp-platform":
        raise AcceptanceError(f"{name} is not an owned running Compose container")
    return container


def assert_runtime_ports() -> None:
    expected = {"system-backend": ("8180/tcp", 8180), "gateway": ("8000/tcp", 8000), "addp-nginx": ("80/tcp", PUBLIC_PORT)}
    for name in ("system-backend", "gateway", "console", "system-frontend", "addp-nginx"):
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
        if name == "system-backend":
            for key in ("PUBLIC_API_URL", "CONSOLE_URL"):
                if env.get(key) != os.environ["ADDP_ONLINE_PUBLIC_ORIGIN"]:
                    raise AcceptanceError(f"System {key} differs from public origin")
        if name == "gateway":
            for key, value in {"SYSTEM_URL": "http://system-backend:8180", "POSTGRES_HOST": "postgres", "POSTGRES_DB": "addp_online", "REDIS_HOST": "redis"}.items():
                if env.get(key) != value:
                    raise AcceptanceError(f"Gateway {key} differs from the internal topology")


def request(path: str, token: str | None = None) -> tuple[int, bytes, str]:
    headers = {"Accept": "application/json, text/html"}
    if token is not None:
        headers["Authorization"] = f"Bearer {token}"
    target = os.environ["ADDP_ONLINE_PUBLIC_ORIGIN"] + path
    try:
        with OPENER.open(urllib.request.Request(target, headers=headers), timeout=15) as response:
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


def main() -> int:
    if os.environ.get("ADDP_ONLINE_TEST") != "1" or os.environ.get("ADDP_ONLINE_HOSTED") != "1":
        raise AcceptanceError("disposable Hosted Online context is required")
    if os.environ.get("ADDP_ONLINE_PUBLIC_ORIGIN") != "http://127.0.0.1:18080":
        raise AcceptanceError("public origin must use the selected nondefault loopback port")
    root_compose_ports()
    infra_compose_ports()
    assert_runtime_ports()
    assert_frontend("/", "/")
    assert_frontend("/system/", "/system/")
    assert_authorized_gateway()
    print(json.dumps({"schema_version": "addp.online-suite/v1", "suite": "compose-public-origin", "public_port": PUBLIC_PORT, "frontends": ["console", "system"], "gateway_auth_context": "passed", "gateway_engine_permission_guard": "passed", "residual_resources": 0}, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (AcceptanceError, KeyError, ValueError, subprocess.CalledProcessError, urllib.error.URLError) as error:
        print(f"Compose public origin acceptance failed: {error}", file=sys.stderr)
        raise SystemExit(1)
