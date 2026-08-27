#!/usr/bin/env python3
"""Accept System module leases and Gateway route recovery through official APIs."""

from __future__ import annotations

import base64
import json
import os
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Iterable


class SuiteError(RuntimeError):
    pass


LOOPBACK_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


@dataclass(frozen=True)
class Registration:
    module_name: str
    instance_id: str
    role: str
    module_url: str
    route_prefix: str
    health_check_url: str
    metadata: dict[str, object]


@dataclass(frozen=True)
class Probe:
    instance_id: str
    module_url: str

    def registration(self, run_id: str, release: str) -> Registration:
        return Registration(
            module_name="manager",
            instance_id=self.instance_id,
            role="backend",
            module_url=self.module_url,
            route_prefix="/api/v1/manager",
            health_check_url=f"{self.module_url}/health/ready",
            metadata={"online_run_id": run_id, "release": release},
        )


@dataclass(frozen=True)
class HTTPResponse:
    status: int
    payload: dict[str, object]


def _identity_values(payload: dict[str, object]) -> tuple[set[str], set[str]]:
    authorization = payload.get("authorization")
    if not isinstance(authorization, dict):
        raise SuiteError("System AuthContext authorization must be an object")
    assignments = authorization.get("role_assignments")
    if not isinstance(assignments, list):
        raise SuiteError("System AuthContext role_assignments must be an array")
    roles: set[str] = set()
    permissions: set[str] = set()
    for assignment in assignments:
        if not isinstance(assignment, dict):
            raise SuiteError("System AuthContext role assignment must be an object")
        role = assignment.get("role_key")
        assigned_permissions = assignment.get("permissions")
        if not isinstance(role, str) or not isinstance(assigned_permissions, list):
            raise SuiteError("System AuthContext role assignment is incomplete")
        if not all(isinstance(permission, str) for permission in assigned_permissions):
            raise SuiteError("System AuthContext permissions must contain strings")
        roles.add(role)
        permissions.update(assigned_permissions)
    return roles, permissions


def validate_manager_service_identity(
    payload: dict[str, object], client_id: str
) -> dict[str, object]:
    principal = payload.get("principal")
    context = payload.get("context")
    client = payload.get("client")
    token = payload.get("token")
    if not isinstance(principal, dict) or principal.get("type") != "service_principal":
        raise SuiteError("Manager registry token must belong to a Service Principal")
    if not isinstance(context, dict) or context.get("type") != "platform":
        raise SuiteError("Manager registry token must use Platform Context")
    if not isinstance(client, dict) or client.get("client_id") != client_id:
        raise SuiteError(f"Manager registry token client_id must be {client_id}")
    if not isinstance(token, dict) or token.get("type") != "service_access_token":
        raise SuiteError("Manager registry token must be a service_access_token")
    roles, permissions = _identity_values(payload)
    expected_permissions = {"system.runtime_registry.update"}
    if permissions != expected_permissions:
        raise SuiteError(
            "Manager registry Service Principal permissions must be exactly "
            "system.runtime_registry.update"
        )
    if roles != {"platform.manager_runtime"}:
        raise SuiteError("Manager registry Service Principal must use platform.manager_runtime")
    return {
        "principal_type": "service_principal",
        "context_type": "platform",
        "client_id": client_id,
        "role": "platform.manager_runtime",
        "permissions": sorted(expected_permissions),
    }


def _absolute_http_url(value: str, name: str) -> str:
    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise SuiteError(f"{name} must be an absolute HTTP(S) URL")
    return value.rstrip("/")


class JSONClient:
    def __init__(self, base_url: str, timeout: float, name: str) -> None:
        self.base_url = _absolute_http_url(base_url, name)
        self.timeout = timeout

    def request(
        self,
        method: str,
        path: str,
        expected: Iterable[int],
        *,
        body: dict[str, object] | None = None,
        headers: dict[str, str] | None = None,
        form: dict[str, str] | None = None,
    ) -> HTTPResponse:
        request_headers = {"Accept": "application/json", **(headers or {})}
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            request_headers["Content-Type"] = "application/json"
        elif form is not None:
            data = urllib.parse.urlencode(form).encode()
            request_headers["Content-Type"] = "application/x-www-form-urlencoded"
        request = urllib.request.Request(
            self.base_url + path,
            data=data,
            method=method,
            headers=request_headers,
        )
        try:
            with LOOPBACK_OPENER.open(request, timeout=self.timeout) as response:
                status = response.status
                raw = response.read()
        except urllib.error.HTTPError as error:
            status = error.code
            try:
                raw = error.read()
            finally:
                error.close()
        except (urllib.error.URLError, TimeoutError) as error:
            raise SuiteError(f"{method} {path} transport failed: {error}") from error
        try:
            decoded = json.loads(raw) if raw else {}
        except json.JSONDecodeError as error:
            text = raw[:512].decode(errors="replace")
            raise SuiteError(f"{method} {path} returned invalid JSON: {text!r}") from error
        if not isinstance(decoded, dict):
            raise SuiteError(f"{method} {path} response must be a JSON object")
        if status not in set(expected):
            response_body = raw[:8192].decode(errors="replace")
            raise SuiteError(
                f"{method} {path} returned HTTP {status}, response_body={response_body!r}"
            )
        return HTTPResponse(status=status, payload=decoded)


class RegistryClient:
    def __init__(
        self,
        system_url: str,
        client_secret: str,
        timeout: float,
        client_id: str = "addp-manager",
    ) -> None:
        if not client_secret:
            raise SuiteError("MANAGER_SERVICE_CLIENT_SECRET is required")
        self.http = JSONClient(system_url, timeout, "SYSTEM_URL")
        credentials = base64.b64encode(f"{client_id}:{client_secret}".encode()).decode()
        token_response = self.http.request(
            "POST",
            "/api/v1/system/oauth/token",
            (200,),
            form={
                "grant_type": "client_credentials",
                "scope": "addp.api",
                "audience": "addp.api",
                "context_type": "platform",
            },
            headers={"Authorization": f"Basic {credentials}"},
        ).payload
        token = token_response.get("access_token")
        if not isinstance(token, str) or not token:
            raise SuiteError("System OAuth response did not contain access_token")
        self.authorization = {"Authorization": f"Bearer {token}"}
        identity = self.http.request(
            "GET",
            "/api/v1/system/auth/context",
            (200,),
            headers=self.authorization,
        ).payload
        self.identity = validate_manager_service_identity(identity, client_id)

    def register(self, registration: Registration) -> None:
        self.http.request(
            "POST",
            "/api/v1/system/runtime/modules",
            (200,),
            body={
                "module_name": registration.module_name,
                "instance_id": registration.instance_id,
                "role": registration.role,
                "module_url": registration.module_url,
                "route_prefix": registration.route_prefix,
                "health_check_url": registration.health_check_url,
                "metadata": registration.metadata,
            },
            headers=self.authorization,
        )

    def heartbeat(self, module_name: str, instance_id: str) -> None:
        self.http.request(
            "POST",
            "/api/v1/system/runtime/modules/heartbeat",
            (200,),
            body={"module_name": module_name, "instance_id": instance_id},
            headers=self.authorization,
        )

    def deregister(self, module_name: str, instance_id: str) -> None:
        module = urllib.parse.quote(module_name, safe="")
        instance = urllib.parse.quote(instance_id, safe="")
        self.http.request(
            "DELETE",
            f"/api/v1/system/runtime/modules/{module}/instances/{instance}",
            (204,),
            headers=self.authorization,
        )


class RouteObserver:
    def __init__(self, gateway_url: str, request_timeout: float) -> None:
        self.http = JSONClient(gateway_url, request_timeout, "GATEWAY_URL")

    def wait(
        self,
        status: int,
        expected_instances: Iterable[str] = (),
        require_all: bool = False,
        timeout: float | None = None,
    ) -> set[str]:
        deadline = time.monotonic() + (timeout if timeout is not None else 15.0)
        expected = set(expected_instances)
        observed: set[str] = set()
        consecutive = 0
        last = "no response"
        while time.monotonic() < deadline:
            try:
                response = self.http.request(
                    "GET",
                    "/api/v1/manager/online-registry-probe",
                    (200, 503),
                )
                last = f"HTTP {response.status} {response.payload}"
                if response.status != status:
                    consecutive = 0
                elif status == 503:
                    consecutive += 1
                    if consecutive >= 2:
                        return observed
                else:
                    instance_id = response.payload.get("instance_id")
                    if isinstance(instance_id, str):
                        observed.add(instance_id)
                    if require_all and expected.issubset(observed):
                        return observed
                    if not require_all and instance_id in expected:
                        consecutive += 1
                        if consecutive >= 4:
                            return observed
                    else:
                        consecutive = 0
            except SuiteError as error:
                last = str(error)
                consecutive = 0
            time.sleep(0.25)
        expectation = f"HTTP {status}"
        if expected:
            expectation += f" from {sorted(expected)}"
        raise SuiteError(f"Gateway did not converge to {expectation}: last={last}")


class ProbeHandler(BaseHTTPRequestHandler):
    server_version = "ADDPOnlineProbe/1"

    def do_GET(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler contract
        payload = {
            "status": "ready" if self.path == "/health/ready" else "live",
            "instance_id": self.server.instance_id,
            "path": self.path,
        }
        raw = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, _format: str, *_args: object) -> None:
        return


class ProbeServer(ThreadingHTTPServer):
    def __init__(self, instance_id: str) -> None:
        super().__init__(("127.0.0.1", 0), ProbeHandler)
        self.instance_id = instance_id
        self._thread = threading.Thread(target=self.serve_forever, daemon=True)

    def start(self) -> Probe:
        self._thread.start()
        host, port = self.server_address
        return Probe(self.instance_id, f"http://{host}:{port}")

    def close(self) -> None:
        self.shutdown()
        self.server_close()
        self._thread.join(timeout=2.0)


def run_suite(
    registry: RegistryClient,
    routes: RouteObserver,
    probe_a: Probe,
    probe_b: Probe,
    run_id: str,
    *,
    route_timeout: float,
    lease_timeout: float,
) -> dict[str, object]:
    cleanup_needed = {probe_a.instance_id, probe_b.instance_id}
    cleanup_errors: list[str] = []
    try:
        routes.wait(503, timeout=route_timeout)

        registration_a_v1 = probe_a.registration(run_id, "v1")
        registry.register(registration_a_v1)
        routes.wait(200, (probe_a.instance_id,), timeout=route_timeout)

        registry.register(registration_a_v1)
        registry.heartbeat("manager", probe_a.instance_id)
        routes.wait(200, (probe_a.instance_id,), timeout=route_timeout)

        routes.wait(503, timeout=lease_timeout)
        registry.register(registration_a_v1)
        routes.wait(200, (probe_a.instance_id,), timeout=route_timeout)

        registry.register(probe_b.registration(run_id, "v2"))
        routes.wait(
            200,
            (probe_a.instance_id, probe_b.instance_id),
            require_all=True,
            timeout=route_timeout,
        )

        registry.deregister("manager", probe_a.instance_id)
        cleanup_needed.discard(probe_a.instance_id)
        routes.wait(200, (probe_b.instance_id,), timeout=route_timeout)

        registry.register(probe_a.registration(run_id, "v3"))
        cleanup_needed.add(probe_a.instance_id)
        routes.wait(
            200,
            (probe_a.instance_id, probe_b.instance_id),
            require_all=True,
            timeout=route_timeout,
        )

        registry.deregister("manager", probe_b.instance_id)
        cleanup_needed.discard(probe_b.instance_id)
        routes.wait(200, (probe_a.instance_id,), timeout=route_timeout)
        registry.deregister("manager", probe_a.instance_id)
        cleanup_needed.discard(probe_a.instance_id)
        routes.wait(503, timeout=route_timeout)
        return {
            "schema_version": "addp.online-suite/v1",
            "suite": "module-registry-recovery",
            "run_id": run_id,
            "registered_instances": 2,
            "reused_instance_ids": 1,
            "lease_expiry_recoveries": 1,
            "release_metadata_updates": 1,
            "residual_active_instances": 0,
            "cleanup": "passed",
        }
    finally:
        for instance_id in (probe_b.instance_id, probe_a.instance_id):
            if instance_id not in cleanup_needed:
                continue
            try:
                registry.deregister("manager", instance_id)
            except Exception as error:
                cleanup_errors.append(f"{instance_id}: {error}")
        if cleanup_errors:
            raise SuiteError("cleanup failed: " + "; ".join(cleanup_errors))


def main() -> int:
    servers: list[ProbeServer] = []
    try:
        if os.environ.get("ADDP_ONLINE_TEST") != "1":
            raise SuiteError("ADDP_ONLINE_TEST must be exactly 1")
        run_id = os.environ["ADDP_ONLINE_TEST_RUN_ID"]
        request_timeout = float(os.environ.get("ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS", "10"))
        route_timeout = float(os.environ.get("ADDP_ONLINE_ROUTE_TIMEOUT_SECONDS", "20"))
        lease_timeout = float(os.environ.get("ADDP_ONLINE_LEASE_TIMEOUT_SECONDS", "50"))
        if min(request_timeout, route_timeout, lease_timeout) <= 0:
            raise SuiteError("Online HTTP, route, and lease timeouts must be greater than zero")

        suffix = run_id.replace(".", "-")[-40:]
        server_a = ProbeServer(f"manager-online-{suffix}-a")
        server_b = ProbeServer(f"manager-online-{suffix}-b")
        servers.extend((server_a, server_b))
        probe_a = server_a.start()
        probe_b = server_b.start()
        registry = RegistryClient(
            os.environ["SYSTEM_URL"],
            os.environ.get("MANAGER_SERVICE_CLIENT_SECRET", ""),
            request_timeout,
        )
        report = run_suite(
            registry,
            RouteObserver(os.environ["GATEWAY_URL"], request_timeout),
            probe_a,
            probe_b,
            run_id,
            route_timeout=route_timeout,
            lease_timeout=lease_timeout,
        )
        report["identity"] = registry.identity
    except (KeyError, ValueError, SuiteError) as error:
        print(f"Module Registry Recovery Online suite failed: {error}", file=sys.stderr)
        return 1
    finally:
        for server in reversed(servers):
            server.close()
    json.dump(report, sys.stdout, ensure_ascii=False, sort_keys=True)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
