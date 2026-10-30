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
LOOPBACK_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


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
    return result.stdout.rstrip()


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


def load_health(service: Service, endpoint: str, timeout: float) -> dict[str, object]:
    request = urllib.request.Request(
        service.base_url + endpoint,
        headers={"Accept": "application/json"},
    )
    try:
        with LOOPBACK_OPENER.open(request, timeout=timeout) as response:
            if response.status != 200:
                raise PreflightError(f"{service.module} health returned HTTP {response.status}")
            payload = json.load(response)
    except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as error:
        raise PreflightError(f"{service.module} health request failed: {error}") from error
    if not isinstance(payload, dict):
        raise PreflightError(f"{service.module} health response must be a JSON object")
    return payload


def validate_health(service: Service, payload: dict[str, object], expected_commit: str, expected_status: str) -> None:
    expected = {
        "status": expected_status,
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


def validate_online_environment(*, require_run_id: bool, execution: bool = False) -> dict[str, str]:
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
    pattern = re.compile(r"[A-Za-z0-9_-]{1,100}") if execution else RUN_ID_PATTERN
    if require_run_id and not pattern.fullmatch(run_id):
        raise PreflightError("ADDP_ONLINE_TEST_RUN_ID must be explicit and filesystem-safe")
    return {
        "tenant_id": str(tenant_id),
        "database": database,
        "run_id": run_id,
    }


def validate_execution_environment(profile: str, repository: Path, services: list[Service]) -> None:
    if (sys.platform != "linux" or os.environ.get("GITHUB_ACTIONS") != "true"
            or os.environ.get("RUNNER_OS") != "Linux" or os.environ.get("ADDP_ONLINE_HOSTED") != "1"
            or os.environ.get("ADDP_ONLINE_HOST") != "1"
            or not re.fullmatch(r"[A-Za-z0-9_-]{1,100}", os.environ.get("ADDP_ONLINE_TEST_RUN_ID", ""))):
        raise PreflightError("execution profile requires its exact Hosted Linux run identity")
    try:
        secret = Path(os.environ["ADDP_ONLINE_SECRET_DIR"])
        runner = Path(os.environ["RUNNER_TEMP"]).resolve(strict=True)
        if (secret.is_symlink() or secret.resolve(strict=True).parent != runner
                or not re.fullmatch(r"addp-online-secret-[A-Za-z0-9_.-]+", secret.name)
                or secret.stat().st_uid != os.getuid() or secret.stat().st_mode & 0o777 != 0o700
                or secret.resolve().is_relative_to(repository)):
            raise ValueError("private directory ownership differs")
        artifact = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]).resolve()
        if (artifact.is_relative_to(secret.resolve()) or secret.resolve().is_relative_to(artifact)
                or artifact.is_relative_to(repository)):
            raise ValueError("reports must be outside private state and checkout")
    except (KeyError, OSError, ValueError) as error:
        raise PreflightError("execution profile requires an owned private secret directory") from error
    tokens = (["ADDP_ONLINE_GRAPH_LIFECYCLE_ACCESS_TOKEN", "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"] if profile == "graph" else
              ["ADDP_ONLINE_CLEANUP_" + key for key in ("ACCESS_TOKEN", "OTHER_ACCESS_TOKEN", "READ_ACCESS_TOKEN",
                                                       "REVOKEE_ACCESS_TOKEN", "IAM_ACCESS_TOKEN", "ASSIGNMENT_ID",
                                                       "OTHER_USERNAME", "OTHER_PASSWORD", "OTHER_ASSIGNMENT_ID")])
    if any(not os.environ.get(key) for key in tokens):
        raise PreflightError("execution profile is missing a required private fixture identity")
    if profile == "graph":
        required = {"gateway", "system", "graph", "graph-peer"}
        if os.environ.get("ADDP_ONLINE_GRAPH_ENGINE") not in {"falkordb", "neo4j"}:
            raise PreflightError("execution profile requires an explicit native Graph engine")
        if not os.environ.get("ADDP_ONLINE_TEST_ENGINE_ID", "").isdigit() or int(os.environ["ADDP_ONLINE_TEST_ENGINE_ID"]) <= 0:
            raise PreflightError("execution profile requires a positive fixture Engine identity")
    else:
        required = {"gateway", "system", "meta", "manager", "transfer", "develop", "service", "quality", "graph", "asset", "model", "standard"}
        console = parse_service("console=" + os.environ.get("CONSOLE_URL", ""))
        if urllib.parse.urlsplit(console.base_url).scheme != "http" or urllib.parse.urlsplit(console.base_url).hostname != "127.0.0.1":
            raise PreflightError("Cleanup browser requires the exact loopback Console entry")
    addresses = {s.module: s.base_url for s in services}
    if not required.issubset(addresses):
        raise PreflightError("execution profile is missing a required service")
    if profile == "graph" and addresses["graph"] == addresses["graph-peer"]:
        raise PreflightError("Graph peer must use a distinct loopback endpoint")
    for service in services:
        variable = "ADDP_ONLINE_GRAPH_PEER_URL" if service.module == "graph-peer" else service.module.upper() + "_URL"
        raw = os.environ.get(variable, service.base_url)
        parsed = urllib.parse.urlsplit(raw)
        if (parsed.scheme != "http" or parsed.hostname != "127.0.0.1" or parsed.path or parsed.query
                or parsed.fragment or parsed.username is not None or parsed.password is not None):
            raise PreflightError("execution profile requires exact IPv4 loopback HTTP endpoints")


def run_preflight(args: argparse.Namespace) -> dict[str, object]:
    if args.timeout <= 0:
        raise PreflightError("timeout must be greater than zero")
    environment = validate_online_environment(require_run_id=True, execution=bool(getattr(args, "execution_profile", None)))

    if args.repository is None:
        raise PreflightError("--repository is required for a full Online preflight")
    repository = args.repository.resolve()
    expected_commit = git(repository, "rev-parse", "HEAD")
    repository_changes = git(repository, "status", "--short", "--untracked-files=all")
    if repository_changes:
        raise PreflightError(
            "Online tests require a clean repository build identity; detected changes:\n"
            f"{repository_changes}"
        )

    services = [parse_service(value) for value in args.service]
    if not services:
        raise PreflightError("at least one --service module=url is required")
    if len({service.module for service in services}) != len(services):
        raise PreflightError("service modules must be unique")

    profile = getattr(args, "execution_profile", None)
    if profile:
        validate_execution_environment(profile, repository, services)

    health = {}
    for service in services:
        payload = load_health(service, "/health/live", args.timeout)
        identity = Service("graph", service.base_url) if service.module == "graph-peer" and profile == "graph" else service
        validate_health(identity, payload, expected_commit, "live")
        ready_payload = load_health(service, "/health/ready", args.timeout)
        if ready_payload.get("status") != "ready":
            raise PreflightError(f"{service.module} readiness status is not ready")
        if profile:
            validate_health(identity, ready_payload, expected_commit, "ready")
            if ready_payload.get("instance_id") != payload.get("instance_id"):
                raise PreflightError("runtime identity changed during execution preflight")
        health[service.module] = {
            "build_id": payload["build_id"],
            "git_commit": payload["git_commit"],
            "source_fingerprint": payload["source_fingerprint"],
            "instance_id": payload.get("instance_id"),
        }
    if profile == "graph":
        instances = [health[name]["instance_id"] for name in ("graph", "graph-peer")]
        if any(not isinstance(value, str) or not value for value in instances) or instances[0] == instances[1]:
            raise PreflightError("Graph main/peer must expose independent runtime instance identities")
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
    parser.add_argument("--execution-profile", choices=("graph", "cleanup"))
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
