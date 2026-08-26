#!/usr/bin/env python3
"""Observe formal Manager/System/Gateway process lifecycle transitions for T4."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Protocol


PHASES = {
    "business-before-system",
    "manager-registered",
    "gateway-established",
    "system-interrupted",
    "system-recovered",
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
    def __init__(self, timeout: float) -> None:
        self.timeout = timeout

    def get(self, base_url: str, path: str) -> Response:
        request = urllib.request.Request(
            base_url.rstrip("/") + path,
            headers={"Accept": "application/json"},
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
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


def observe_once(
    phase: str,
    client: Client,
    *,
    manager_url: str,
    system_url: str,
    gateway_url: str,
    expected_instance_id: str | None,
    expected_git_commit: str | None = None,
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
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    parser.add_argument("--timeout", type=float, default=45.0)
    parser.add_argument("--output", type=Path, required=True)
    return parser.parse_args()


def main() -> int:
    try:
        args = parse_args()
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
            HTTPClient(timeout=min(args.timeout, 2.0)),
            manager_url=manager_url,
            system_url=system_url,
            gateway_url=gateway_url,
            expected_instance_id=args.expected_instance_id,
            expected_git_commit=expected_git_commit,
            timeout=args.timeout,
        )
        _write_report(args.output, report)
    except (ObservationError, OSError, subprocess.CalledProcessError) as error:
        print(f"Module lifecycle process observation failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
