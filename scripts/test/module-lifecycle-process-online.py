#!/usr/bin/env python3
"""Observe formal Manager/System/Gateway process lifecycle transitions for T4."""

from __future__ import annotations

import argparse
import ipaddress
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Protocol


PHASES = {
    "business-before-system",
    "manager-registered",
    "gateway-established",
    "system-interrupted",
    "system-recovered",
    "manager-abnormally-stopped",
    "manager-restarted",
    "manager-gracefully-stopped",
}


class ObservationError(RuntimeError):
    pass


class TransportUnavailable(ObservationError):
    pass


@dataclass(frozen=True)
class Response:
    status: int
    payload: dict[str, object]


class Client(Protocol):
    def get(self, base_url: str, path: str) -> Response: ...


class HTTPClient:
    def __init__(self, timeout: float, system_url: str, token: str) -> None:
        self.timeout = timeout
        self.system_url = system_url
        self.token = token
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def get(self, base_url: str, path: str) -> Response:
        headers = {"Accept": "application/json"}
        if base_url == self.system_url and path.startswith("/api/v1/system/"):
            headers["Authorization"] = f"Bearer {self.token}"
        request = urllib.request.Request(base_url.rstrip("/") + path, headers=headers)
        try:
            with self.opener.open(request, timeout=self.timeout) as response:
                status = response.status
                raw = response.read()
        except urllib.error.HTTPError as error:
            status = error.code
            try:
                raw = error.read()
            finally:
                error.close()
        except (urllib.error.URLError, TimeoutError) as error:
            raise TransportUnavailable(str(error)) from error
        try:
            payload = json.loads(raw) if raw else {}
        except json.JSONDecodeError as error:
            raise ObservationError(f"{path} returned invalid JSON") from error
        if not isinstance(payload, dict):
            raise ObservationError(f"{path} response must be a JSON object")
        return Response(status=status, payload=payload)


def _loopback_url(value: str, name: str) -> str:
    parsed = urllib.parse.urlsplit(value)
    try:
        port = parsed.port
    except ValueError as error:
        raise ObservationError(f"{name} must contain a valid port") from error
    if (
        parsed.scheme not in {"http", "https"}
        or parsed.hostname not in {"localhost", "127.0.0.1", "::1"}
        or port is None
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
        or parsed.path not in {"", "/"}
    ):
        raise ObservationError(f"{name} must be an explicit loopback HTTP(S) URL")
    return value.rstrip("/")


def _response(client: Client, base_url: str, path: str, status: int) -> dict[str, object]:
    response = client.get(base_url, path)
    if response.status != status:
        raise ObservationError(f"{path} returned HTTP {response.status}, expected {status}")
    return response.payload


def _unavailable(client: Client, base_url: str, module: str) -> None:
    try:
        response = client.get(base_url, "/health/live")
    except TransportUnavailable:
        return
    raise ObservationError(f"{module} is still reachable with HTTP {response.status}")


def _validate_build_identity(
    payload: dict[str, object], module: str, expected_git_commit: str | None
) -> None:
    if expected_git_commit is None:
        return
    if payload.get("git_commit") != expected_git_commit:
        raise ObservationError(
            f"{module} git_commit={payload.get('git_commit')!r}, expected {expected_git_commit!r}"
        )
    for field in ("build_id", "source_fingerprint", "built_at", "started_at"):
        value = payload.get(field)
        if not isinstance(value, str) or not value or value == "unknown":
            raise ObservationError(f"{module} {field} must contain a build identity")


def _manager_state(
    client: Client,
    manager_url: str,
    *,
    ready: bool,
    expected_instance_id: str | None,
    expected_git_commit: str | None,
) -> dict[str, object]:
    live = _response(client, manager_url, "/health/live", 200)
    if live.get("status") != "live" or live.get("module") != "manager":
        raise ObservationError("Manager liveness contract is invalid")
    _validate_build_identity(live, "Manager", expected_git_commit)
    expected_status = 200 if ready else 503
    readiness = _response(client, manager_url, "/health/ready", expected_status)
    expected_readiness = "ready" if ready else "not_ready"
    if readiness.get("status") != expected_readiness:
        raise ObservationError(f"Manager readiness is not {expected_readiness}")
    instance_id = readiness.get("instance_id")
    if not isinstance(instance_id, str) or not instance_id:
        raise ObservationError("Manager readiness does not expose instance_id")
    if expected_instance_id and instance_id != expected_instance_id:
        raise ObservationError(
            f"Manager instance_id changed from {expected_instance_id} to {instance_id}"
        )
    business = _response(client, manager_url, "/", 200 if ready else 503)
    if ready:
        if business.get("message") != "Manager 数据管理服务":
            raise ObservationError("Manager business route did not recover")
    elif business.get("error_code") != "module_not_ready":
        raise ObservationError("Manager business route did not enforce module_not_ready")
    return {
        "live": True,
        "ready": ready,
        "process_started_at": live.get("started_at"),
        "instance_id": instance_id,
        "registration_state": readiness.get("registration_state"),
        "business_route_status": 200 if ready else 503,
    }


def _system_ready(
    client: Client, system_url: str, expected_git_commit: str | None
) -> None:
    live = _response(client, system_url, "/health/live", 200)
    ready = _response(client, system_url, "/health/ready", 200)
    if live.get("status") != "live" or ready.get("status") != "ready":
        raise ObservationError("System is not Ready")
    _validate_build_identity(live, "System", expected_git_commit)


def _gateway_state(
    client: Client,
    gateway_url: str,
    manager_url: str,
    *,
    manager_present: bool,
    expected_git_commit: str | None,
) -> dict[str, object]:
    live = _response(client, gateway_url, "/health/live", 200)
    ready = _response(client, gateway_url, "/health/ready", 200)
    root = _response(client, gateway_url, "/", 200)
    if live.get("status") != "live" or ready.get("status") != "ready":
        raise ObservationError("Gateway is not Ready")
    _validate_build_identity(live, "Gateway", expected_git_commit)
    modules = root.get("modules")
    if not isinstance(modules, dict):
        raise ObservationError("Gateway root does not expose a module snapshot")
    observed = modules.get("manager")
    if manager_present and observed != manager_url:
        raise ObservationError(
            f"Gateway Manager route is {observed!r}, expected {manager_url!r}"
        )
    if not manager_present and observed is not None:
        raise ObservationError("Gateway still exposes an expired Manager route")
    return {"ready": True, "manager_route_present": manager_present}



INSTANCE_PATH = "/api/v1/system/platform/module-instances"
INSTANCE_FIELDS = (
    "module_name", "instance_id", "role", "module_url", "registered_host",
    "host_node_name", "host_node_ips", "process_started_at", "registered_at",
    "status", "stop_reason", "stopped_at", "lease_expires_at",
)


def node_ips(value: str) -> tuple[str, ...]:
    try:
        addresses = [ipaddress.ip_address(part.strip()) for part in value.split(",")]
    except ValueError as error:
        raise ObservationError("ADDP_HOST_NODE_IPS must contain valid IP addresses") from error
    return tuple(dict.fromkeys(str(getattr(ip, "ipv4_mapped", None) or ip) for ip in addresses))


def _timestamp(value: object) -> datetime:
    if not isinstance(value, str):
        raise ObservationError("instance timestamp is missing")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise ObservationError("instance timestamp is invalid") from error
    if parsed.tzinfo is None:
        raise ObservationError("instance timestamp must include timezone")
    return parsed


def _platform_identity(client: Client, system_url: str) -> None:
    identity = _response(client, system_url, "/api/v1/system/auth/context", 200)
    context = identity.get("context", {})
    authorization = identity.get("authorization", {})
    if not isinstance(context, dict) or context.get("type") != "platform":
        raise ObservationError("observer must use Platform Context")
    assignments = authorization.get("role_assignments", []) if isinstance(authorization, dict) else []
    if not isinstance(assignments, list) or not any(
        isinstance(assignment, dict) and isinstance(assignment.get("permissions"), list)
        and "platform.module.read" in assignment["permissions"] for assignment in assignments
    ):
        raise ObservationError("observer requires platform.module.read")


def _instances(client: Client, system_url: str, **filters: str) -> list[dict[str, object]]:
    rows = []
    page = 1
    while True:
        query = urllib.parse.urlencode({**filters, "page": page, "page_size": 100})
        payload = _response(client, system_url, INSTANCE_PATH + "?" + query, 200)
        data, pages = payload.get("data"), payload.get("total_pages")
        if not isinstance(data, list) or not all(isinstance(row, dict) for row in data):
            raise ObservationError("instance query data must be a list of objects")
        if not isinstance(pages, int) or pages < 0 or payload.get("page") != page:
            raise ObservationError("instance query pagination contract is invalid")
        rows.extend(data)
        if page >= pages:
            return rows
        if not data:
            raise ObservationError("instance query pagination made no progress")
        page += 1


def _target(rows: list[dict[str, object]], instance_id: str) -> dict[str, object]:
    matches = [row for row in rows if row.get("instance_id") == instance_id]
    if len(matches) != 1:
        raise ObservationError(f"expected one instance {instance_id}, found {len(matches)}")
    return {field: matches[0].get(field) for field in INSTANCE_FIELDS}


def _runtime_instance(
    client: Client, system_url: str, instance_id: str, status: str,
    expected_ips: tuple[str, ...], reason: str = "",
) -> dict[str, object]:
    filters = {"module_name": "manager", "role": "backend", "status": status}
    if reason:
        filters["stop_reason"] = reason
    record = _target(_instances(client, system_url, **filters), instance_id)
    if any(record.get(key) != value for key, value in filters.items()):
        raise ObservationError("instance query returned a mismatched module, role, status or reason")
    if set(record.get("host_node_ips") or []) != set(expected_ips):
        raise ObservationError("instance host_node_ips do not match deployment")
    started = _timestamp(record.get("process_started_at"))
    if status == "down":
        stopped = _timestamp(record.get("stopped_at"))
        if stopped < started:
            raise ObservationError("instance stopped before process start")
        if reason == "lease_expired" and stopped != _timestamp(record.get("lease_expires_at")):
            raise ObservationError("lease_expired time must equal lease_expires_at")
        filters.update(time_basis="offline", time_from=record["stopped_at"],
                       time_to=(stopped + timedelta(seconds=1)).isoformat())
    for ip in expected_ips:
        selected = _instances(client, system_url, **filters, node_ip=ip)
        _target(selected, instance_id)
        if any(any(row.get(key) != value for key, value in filters.items()
                   if key not in {"time_basis", "time_from", "time_to"})
               or ip not in (row.get("host_node_ips") or []) for row in selected):
            raise ObservationError("combined instance query returned a mismatched row")
        if status == "down":
            excluded_filters = {**filters, "time_from": (stopped - timedelta(seconds=1)).isoformat(),
                                "time_to": record["stopped_at"], "node_ip": ip}
            excluded = _instances(client, system_url, **excluded_filters)
            if any(row.get("instance_id") == instance_id for row in excluded):
                raise ObservationError("time_to must exclude the exact offline boundary")
    return record


def _baseline_instance(baseline: dict[str, object] | None) -> dict[str, object]:
    manager = baseline.get("manager", {}) if isinstance(baseline, dict) else {}
    record = manager.get("instance") if isinstance(manager, dict) else None
    if not isinstance(record, dict) or not record.get("instance_id"):
        raise ObservationError("phase requires baseline Manager instance evidence")
    return record


def _same_process(record: dict[str, object], baseline: dict[str, object]) -> None:
    for field in ("instance_id", "process_started_at", "module_url", "registered_host", "host_node_ips"):
        if record.get(field) != baseline.get(field):
            raise ObservationError(f"same process instance field changed: {field}")

def observe_once(
    phase: str,
    client: Client,
    *,
    manager_url: str,
    system_url: str,
    gateway_url: str,
    expected_instance_id: str | None,
    expected_git_commit: str | None = None,
    expected_host_node_ips: tuple[str, ...] = (),
    baseline: dict[str, object] | None = None,
) -> dict[str, object]:
    if phase not in PHASES:
        raise ObservationError(f"unsupported lifecycle phase: {phase}")

    if phase == "business-before-system":
        manager = _manager_state(
            client,
            manager_url,
            ready=False,
            expected_instance_id=expected_instance_id,
            expected_git_commit=expected_git_commit,
        )
        _unavailable(client, system_url, "System")
        _unavailable(client, gateway_url, "Gateway")
        system = {"reachable": False}
        gateway = {"reachable": False}
    elif phase == "manager-registered":
        _system_ready(client, system_url, expected_git_commit)
        manager = _manager_state(
            client,
            manager_url,
            ready=True,
            expected_instance_id=expected_instance_id,
            expected_git_commit=expected_git_commit,
        )
        _unavailable(client, gateway_url, "Gateway")
        system = {"ready": True}
        gateway = {"reachable": False}
    elif phase == "gateway-established":
        _system_ready(client, system_url, expected_git_commit)
        manager = _manager_state(
            client,
            manager_url,
            ready=True,
            expected_instance_id=expected_instance_id,
            expected_git_commit=expected_git_commit,
        )
        system = {"ready": True}
        gateway = _gateway_state(
            client,
            gateway_url,
            manager_url,
            manager_present=True,
            expected_git_commit=expected_git_commit,
        )
    elif phase == "system-interrupted":
        _unavailable(client, system_url, "System")
        manager = _manager_state(
            client,
            manager_url,
            ready=False,
            expected_instance_id=expected_instance_id,
            expected_git_commit=expected_git_commit,
        )
        system = {"reachable": False}
        gateway = _gateway_state(
            client,
            gateway_url,
            manager_url,
            manager_present=False,
            expected_git_commit=expected_git_commit,
        )
    elif phase in {"manager-abnormally-stopped", "manager-gracefully-stopped"}:
        previous = _baseline_instance(baseline)
        if expected_instance_id != previous["instance_id"]:
            raise ObservationError("stopped instance does not match baseline")
        _unavailable(client, manager_url, "Manager")
        _system_ready(client, system_url, expected_git_commit)
        _platform_identity(client, system_url)
        reason = "lease_expired" if phase == "manager-abnormally-stopped" else "graceful"
        record = _runtime_instance(client, system_url, expected_instance_id, "down", expected_host_node_ips, reason)
        _same_process(record, previous)
        manager = {"live": False, "ready": False, "instance_id": expected_instance_id, "instance": record,
                   "runtime_seconds": (_timestamp(record["stopped_at"]) - _timestamp(record["process_started_at"])).total_seconds()}
        if phase == "manager-gracefully-stopped":
            previous_manager = baseline["manager"]
            original = previous_manager.get("previous_instance")
            if not isinstance(original, dict):
                raise ObservationError("graceful phase requires the original DOWN instance evidence")
            old = _runtime_instance(client, system_url, original["instance_id"], "down", expected_host_node_ips, "lease_expired")
            _same_process(old, original)
            manager["previous_instance"] = old
        system = {"ready": True}
        gateway = _gateway_state(client, gateway_url, manager_url, manager_present=False,
                                 expected_git_commit=expected_git_commit)
    else:
        _system_ready(client, system_url, expected_git_commit)
        manager = _manager_state(
            client,
            manager_url,
            ready=True,
            expected_instance_id=expected_instance_id,
            expected_git_commit=expected_git_commit,
        )
        system = {"ready": True}
        gateway = _gateway_state(
            client,
            gateway_url,
            manager_url,
            manager_present=True,
            expected_git_commit=expected_git_commit,
        )

    if phase in {"manager-registered", "gateway-established", "system-recovered", "manager-restarted"}:
        _platform_identity(client, system_url)
        record = _runtime_instance(client, system_url, manager["instance_id"], "up", expected_host_node_ips)
        if record.get("stop_reason") or record.get("stopped_at"):
            raise ObservationError("UP instance retains offline state")
        if _timestamp(record["process_started_at"]) != _timestamp(manager["process_started_at"]):
            raise ObservationError("registry process_started_at differs from Manager liveness")
        manager["instance"] = record
        observed_at = datetime.now(timezone.utc)
        manager["observed_at"] = observed_at.isoformat()
        manager["uptime_seconds"] = (observed_at - _timestamp(record["process_started_at"])).total_seconds()
        if manager["uptime_seconds"] < 0:
            raise ObservationError("Manager process_started_at is in the future")
        if phase == "system-recovered":
            _same_process(record, _baseline_instance(baseline))
        if phase == "manager-restarted":
            previous = _baseline_instance(baseline)
            if record["instance_id"] == previous["instance_id"]:
                raise ObservationError("restarted process must have a new instance_id")
            if _timestamp(record["process_started_at"]) <= _timestamp(previous["process_started_at"]):
                raise ObservationError("restarted process must have a newer process_started_at")
            old = _runtime_instance(client, system_url, previous["instance_id"], "down", expected_host_node_ips, "lease_expired")
            _same_process(old, previous)
            manager["previous_instance"] = old

    return {
        "schema_version": "addp.module-lifecycle-process/v1",
        "phase": phase,
        "git_commit": expected_git_commit,
        "manager": manager,
        "system": system,
        "gateway": gateway,
    }


def wait_for_phase(
    phase: str,
    client: Client,
    *,
    manager_url: str,
    system_url: str,
    gateway_url: str,
    expected_instance_id: str | None,
    expected_git_commit: str | None = None,
    timeout: float,
    expected_host_node_ips: tuple[str, ...] = (),
    baseline: dict[str, object] | None = None,
    interval: float = 0.25,
) -> dict[str, object]:
    if timeout <= 0 or interval <= 0:
        raise ObservationError("timeout and interval must be greater than zero")
    deadline = time.monotonic() + timeout
    last_error: ObservationError | None = None
    while time.monotonic() < deadline:
        try:
            return observe_once(
                phase,
                client,
                manager_url=manager_url,
                system_url=system_url,
                gateway_url=gateway_url,
                expected_instance_id=expected_instance_id,
                expected_git_commit=expected_git_commit,
                expected_host_node_ips=expected_host_node_ips,
                baseline=baseline,
            )
        except ObservationError as error:
            last_error = error
            time.sleep(interval)
    raise ObservationError(f"phase {phase} did not converge: {last_error}")


def _write_report(path: Path, report: dict[str, object]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(
        json.dumps(report, ensure_ascii=False, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    temporary.replace(path)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--phase", choices=sorted(PHASES), required=True)
    parser.add_argument("--manager-url", default=os.environ.get("MANAGER_URL", ""))
    parser.add_argument("--system-url", default=os.environ.get("SYSTEM_URL", ""))
    parser.add_argument("--gateway-url", default=os.environ.get("GATEWAY_URL", ""))
    parser.add_argument("--expected-instance-id")
    parser.add_argument("--baseline", type=Path)
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    parser.add_argument("--timeout", type=float, default=45.0)
    parser.add_argument("--output", type=Path, required=True)
    return parser.parse_args()


def main() -> int:
    try:
        args = parse_args()
        token = os.environ.get("ADDP_ONLINE_TEST_PLATFORM_ACCESS_TOKEN", "").strip()
        if not token:
            raise ObservationError("ADDP_ONLINE_TEST_PLATFORM_ACCESS_TOKEN is required")
        expected_ips = node_ips(os.environ.get("ADDP_HOST_NODE_IPS", ""))
        baseline = json.loads(args.baseline.read_text(encoding="utf-8")) if args.baseline else None
        manager_url = _loopback_url(args.manager_url, "MANAGER_URL")
        system_url = _loopback_url(args.system_url, "SYSTEM_URL")
        gateway_url = _loopback_url(args.gateway_url, "GATEWAY_URL")
        expected_git_commit = subprocess.run(
            ["git", "-C", str(args.repository.resolve()), "rev-parse", "HEAD"],
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
        report = wait_for_phase(
            args.phase,
            HTTPClient(timeout=min(args.timeout, 2.0), system_url=system_url, token=token),
            manager_url=manager_url,
            system_url=system_url,
            gateway_url=gateway_url,
            expected_instance_id=args.expected_instance_id,
            expected_git_commit=expected_git_commit,
            timeout=args.timeout,
            expected_host_node_ips=expected_ips,
            baseline=baseline,
        )
        _write_report(args.output, report)
    except (ObservationError, OSError, ValueError, subprocess.CalledProcessError) as error:
        print(f"Module lifecycle process observation failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
