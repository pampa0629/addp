#!/usr/bin/env python3
"""Validate the safety boundary and build identity for an ADDP Online suite."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path


RUN_ID_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$")
LOOPBACK_HOSTS = {"localhost", "127.0.0.1", "::1"}
ONLINE_DATABASE = "addp_online"


class PreflightError(RuntimeError):
    pass


@dataclass(frozen=True)
class Service:
    module: str
    base_url: str


def git(repository: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", "-C", str(repository), *args],
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout.strip()


def parse_service(value: str) -> Service:
    module, separator, base_url = value.partition("=")
    if not separator or not module or not re.fullmatch(r"[a-z][a-z0-9_-]*", module):
        raise PreflightError(f"invalid service declaration: {value!r}")
    parsed = urllib.parse.urlsplit(base_url)
    try:
        port = parsed.port
    except ValueError as error:
        raise PreflightError(f"{module} URL port must be valid") from error
    if (
        parsed.scheme not in {"http", "https"}
        or parsed.hostname not in LOOPBACK_HOSTS
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
    ):
        raise PreflightError(
            f"{module} URL must be an HTTP(S) loopback URL without credentials, query, or fragment"
        )
    if parsed.path not in {"", "/"}:
        raise PreflightError(f"{module} URL must not contain a path")
    if port is None or port <= 0:
        raise PreflightError(f"{module} URL must contain an explicit valid port")
    return Service(module=module, base_url=base_url.rstrip("/"))


def load_health(service: Service, timeout: float) -> dict[str, object]:
    request = urllib.request.Request(
        service.base_url + "/health",
        headers={"Accept": "application/json"},
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            if response.status != 200:
                raise PreflightError(f"{service.module} health returned HTTP {response.status}")
            payload = json.load(response)
    except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as error:
        raise PreflightError(f"{service.module} health request failed: {error}") from error
    if not isinstance(payload, dict):
        raise PreflightError(f"{service.module} health response must be a JSON object")
    return payload


def validate_health(service: Service, payload: dict[str, object], expected_commit: str) -> None:
    expected = {
        "status": "ok",
        "module": service.module,
        "git_commit": expected_commit,
    }
    for field, value in expected.items():
        if payload.get(field) != value:
            raise PreflightError(
                f"{service.module} health {field}={payload.get(field)!r}, expected {value!r}"
            )
    for field in ("build_id", "source_fingerprint", "built_at", "started_at"):
        value = payload.get(field)
        if not isinstance(value, str) or not value or value == "unknown":
            raise PreflightError(f"{service.module} health {field} must contain a build identity")


def validate_online_environment(*, require_run_id: bool) -> dict[str, str]:
    if os.environ.get("ADDP_ONLINE_TEST") != "1":
        raise PreflightError("ADDP_ONLINE_TEST must be exactly 1")
    tenant_value = os.environ.get("ADDP_ONLINE_TEST_TENANT_ID", "")
    try:
        tenant_id = int(tenant_value)
    except ValueError as error:
        raise PreflightError("ADDP_ONLINE_TEST_TENANT_ID must be a positive integer") from error
    if tenant_id <= 1:
        raise PreflightError("ADDP_ONLINE_TEST_TENANT_ID must identify a dedicated non-default Tenant")
    database = os.environ.get("POSTGRES_DB", "")
    if database != ONLINE_DATABASE:
        raise PreflightError(f"POSTGRES_DB must be exactly {ONLINE_DATABASE} on the dedicated Online deployment")
    run_id = os.environ.get("ADDP_ONLINE_TEST_RUN_ID", "")
    if require_run_id and not RUN_ID_PATTERN.fullmatch(run_id):
        raise PreflightError("ADDP_ONLINE_TEST_RUN_ID must be explicit and filesystem-safe")
    return {
        "tenant_id": str(tenant_id),
        "database": database,
        "run_id": run_id,
    }


def run_preflight(args: argparse.Namespace) -> dict[str, object]:
    if args.timeout <= 0:
        raise PreflightError("timeout must be greater than zero")
    environment = validate_online_environment(require_run_id=True)

    if args.repository is None:
        raise PreflightError("--repository is required for a full Online preflight")
    repository = args.repository.resolve()
    expected_commit = git(repository, "rev-parse", "HEAD")
    if git(repository, "status", "--porcelain"):
        raise PreflightError("Online tests require a clean repository build identity")

    services = [parse_service(value) for value in args.service]
    if not services:
        raise PreflightError("at least one --service module=url is required")
    if len({service.module for service in services}) != len(services):
        raise PreflightError("service modules must be unique")

    health = {}
    for service in services:
        payload = load_health(service, args.timeout)
        validate_health(service, payload, expected_commit)
        health[service.module] = {
            "build_id": payload["build_id"],
            "git_commit": payload["git_commit"],
            "source_fingerprint": payload["source_fingerprint"],
        }
    return {
        "schema_version": "addp.online-preflight/v1",
        "run_id": environment["run_id"],
        "tenant_id": environment["tenant_id"],
        "database": {
            "category": "dedicated-online",
            "name": environment["database"],
        },
        "git_commit": expected_commit,
        "repository_clean": True,
        "network_boundary": "loopback-only",
        "services": health,
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--environment-only", action="store_true")
    parser.add_argument("--repository", type=Path)
    parser.add_argument("--service", action="append", default=[])
    parser.add_argument("--timeout", type=float, default=10.0)
    return parser.parse_args()


def main() -> int:
    try:
        args = parse_args()
        if args.environment_only:
            environment = validate_online_environment(require_run_id=False)
            report = {
                "schema_version": "addp.online-environment/v1",
                "tenant_id": environment["tenant_id"],
                "database": {
                    "category": "dedicated-online",
                    "name": environment["database"],
                },
            }
        else:
            report = run_preflight(args)
    except (PreflightError, subprocess.CalledProcessError) as error:
        print(f"Online preflight failed: {error}", file=sys.stderr)
        return 1
    json.dump(report, sys.stdout, ensure_ascii=False, sort_keys=True)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
