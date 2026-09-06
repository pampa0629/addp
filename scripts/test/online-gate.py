#!/usr/bin/env python3
"""Dispatch registered ADDP Online suites through one safety-checked entrypoint."""

from __future__ import annotations

import argparse
import fcntl
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import uuid
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
from typing import Iterator, Mapping
from urllib.parse import urlsplit


SUITE_PATTERN = re.compile(r"^[a-z][a-z0-9-]*$")
RUN_ID_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$")


class OnlineGateError(RuntimeError):
    pass


class OnlineStageError(OnlineGateError):
    def __init__(self, stage: str, error_code: str, message: str) -> None:
        super().__init__(message)
        self.stage = stage
        self.error_code = error_code


@dataclass(frozen=True)
class Suite:
    command: tuple[str, ...]
    services: tuple[tuple[str, str], ...]


# Only executable owner-maintained Online suites belong here. Do not register
# placeholders: an entry means the suite is ready for real Online acceptance.
SUITES: Mapping[str, Suite] = {
    "consumer-engine-recovery": Suite(
        command=(sys.executable, "scripts/test/consumer-engine-recovery-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("manager", "MANAGER_URL"),
            ("service", "SERVICE_URL"),
        ),
    ),
    "module-registry-recovery": Suite(
        command=(sys.executable, "scripts/test/module-registry-recovery-online.py"),
        services=(("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL")),
    ),
    "manager-internal-artifact-lineage": Suite(
        command=(sys.executable, "scripts/test/manager-internal-artifact-lineage-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("manager", "MANAGER_URL"),
            ("monitor", "MONITOR_URL"),
        ),
    ),
    "security-transfer-protection": Suite(
        command=(sys.executable, "scripts/test/security-transfer-protection-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("security", "SECURITY_URL"),
            ("transfer", "TRANSFER_URL"),
            ("manager", "MANAGER_URL"),
        ),
    ),
    "security-protection-exemption": Suite(
        command=(sys.executable, "scripts/test/security-protection-exemption-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("security", "SECURITY_URL"),
            ("manager", "MANAGER_URL"),
            ("develop", "DEVELOP_URL"),
            ("service", "SERVICE_URL"),
            ("transfer", "TRANSFER_URL"),
        ),
    ),
    "security-mysql-owner-protection": Suite(
        command=(sys.executable, "scripts/test/security-mysql-owner-protection-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("security", "SECURITY_URL"),
            ("manager", "MANAGER_URL"),
            ("develop", "DEVELOP_URL"),
            ("service", "SERVICE_URL"),
            ("transfer", "TRANSFER_URL"),
        ),
    ),
    "standard-model-reference-deletion": Suite(
        command=(sys.executable, "scripts/test/standard-model-reference-deletion-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("standard", "STANDARD_URL"),
            ("model", "MODEL_URL"),
        ),
    ),
    "enterprise-catalog-publishing": Suite(
        command=(sys.executable, "scripts/test/enterprise-catalog-publishing-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("meta", "META_URL"),
            ("catalog", "CATALOG_URL"),
            ("asset", "ASSET_URL"),
            ("portal", "PORTAL_URL"),
        ),
    ),
    "workbench-service-consumption": Suite(
        command=(sys.executable, "scripts/test/workbench-service-consumption-online.py"),
        services=(
            ("gateway", "GATEWAY_URL"),
            ("system", "SYSTEM_URL"),
            ("service", "SERVICE_URL"),
            ("workbench", "WORKBENCH_URL"),
        ),
    ),
}


def parse_positive_timeout(value: str) -> float:
    try:
        timeout = float(value)
    except ValueError as error:
        raise OnlineGateError("ADDP_ONLINE_TEST_TIMEOUT_SECONDS must be numeric") from error
    if timeout <= 0:
        raise OnlineGateError("ADDP_ONLINE_TEST_TIMEOUT_SECONDS must be greater than zero")
    return timeout


def resolve_run_id(environment: dict[str, str]) -> str:
    run_id = environment.get("ADDP_ONLINE_TEST_RUN_ID")
    if not run_id:
        run_id = f"run-{uuid.uuid4().hex}"
        environment["ADDP_ONLINE_TEST_RUN_ID"] = run_id
    if not RUN_ID_PATTERN.fullmatch(run_id):
        raise OnlineGateError("ADDP_ONLINE_TEST_RUN_ID must be filesystem-safe")
    return run_id


def _report_directory(repository: Path, environment: Mapping[str, str], run_id: str) -> Path:
    configured = environment.get("ADDP_ONLINE_ARTIFACT_DIR")
    if configured:
        directory = Path(configured)
        if not directory.is_absolute():
            raise OnlineGateError("ADDP_ONLINE_ARTIFACT_DIR must be an absolute path")
        resolved = directory.resolve()
        try:
            resolved.relative_to(repository.resolve())
        except ValueError:
            return resolved
        raise OnlineGateError("ADDP_ONLINE_ARTIFACT_DIR must be outside the repository")
    return Path(tempfile.gettempdir()) / "addp-online-reports" / run_id


def _write_report(path: Path, report: Mapping[str, object]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(".json.tmp")
    temporary.write_text(
        json.dumps(report, ensure_ascii=False, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    temporary.replace(path)


def _parse_stage_report(stage: str, stdout: str) -> dict[str, object]:
    lines = [line for line in stdout.splitlines() if line.strip()]
    if not lines:
        raise OnlineStageError(
            stage,
            f"online_{stage}_report_missing",
            f"{stage} did not emit a JSON report",
        )
    try:
        payload = json.loads(lines[-1])
    except json.JSONDecodeError as error:
        raise OnlineStageError(
            stage,
            f"online_{stage}_report_invalid",
            f"{stage} emitted invalid JSON",
        ) from error
    if not isinstance(payload, dict):
        raise OnlineStageError(
            stage,
            f"online_{stage}_report_invalid",
            f"{stage} report must be a JSON object",
        )
    return payload


def run_stage(
    stage: str,
    command: tuple[str, ...] | list[str],
    *,
    repository: Path,
    environment: Mapping[str, str],
    timeout: float,
) -> tuple[dict[str, object], int]:
    started_at = time.monotonic()
    try:
        result = subprocess.run(
            command,
            cwd=repository,
            env=environment,
            timeout=timeout,
            capture_output=True,
            text=True,
        )
    except subprocess.TimeoutExpired as error:
        raise OnlineStageError(
            stage,
            f"online_{stage}_timeout",
            f"{stage} exceeded the Online total timeout",
        ) from error
    if result.stdout:
        print(result.stdout, end="" if result.stdout.endswith("\n") else "\n")
    if result.stderr:
        print(
            result.stderr,
            end="" if result.stderr.endswith("\n") else "\n",
            file=sys.stderr,
        )
    if result.returncode != 0:
        raise OnlineStageError(
            stage,
            f"online_{stage}_failed",
            f"{stage} exited with status {result.returncode}",
        )
    duration_ms = round((time.monotonic() - started_at) * 1000)
    return _parse_stage_report(stage, result.stdout), duration_ms


@contextmanager
def online_run_lock(
    suite_name: str, run_id: str, lock_root: Path | None = None
) -> Iterator[None]:
    root = lock_root or Path(tempfile.gettempdir()) / "addp-online-locks"
    root.mkdir(parents=True, exist_ok=True)
    path = root / f"{suite_name}--{run_id}.lock"
    with path.open("a+", encoding="utf-8") as lock:
        try:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise OnlineStageError(
                "gate",
                "online_run_active",
                f"an Online run is already active for {suite_name} and {run_id}",
            ) from error
        try:
            yield
        finally:
            fcntl.flock(lock.fileno(), fcntl.LOCK_UN)


def _service_addresses(suite: Suite, environment: Mapping[str, str]) -> dict[str, str]:
    addresses: dict[str, str] = {}
    for module, variable in suite.services:
        parsed = urlsplit(environment[variable])
        addresses[module] = f"{parsed.scheme}://{parsed.hostname}:{parsed.port}"
    return addresses


def run_online(
    suite_name: str,
    repository: Path,
    environment: dict[str, str],
    suites: Mapping[str, Suite] = SUITES,
    lock_root: Path | None = None,
) -> dict[str, object]:
    if environment.get("ADDP_ONLINE_TEST") != "1":
        raise OnlineGateError("ADDP_ONLINE_TEST must be exactly 1")
    if not SUITE_PATTERN.fullmatch(suite_name):
        raise OnlineGateError("ONLINE_SUITE must use lowercase kebab-case")
    suite = suites.get(suite_name)
    if suite is None:
        available = ", ".join(sorted(suites)) or "none"
        raise OnlineGateError(f"unknown ONLINE_SUITE {suite_name!r}; registered suites: {available}")

    timeout = parse_positive_timeout(
        environment.get("ADDP_ONLINE_TEST_TIMEOUT_SECONDS", "900")
    )
    started_at = time.monotonic()
    run_id = resolve_run_id(environment)
    report_path = _report_directory(repository, environment, run_id) / "online-report.json"
    preflight = [
        sys.executable,
        str(repository / "scripts/test/online-preflight.py"),
        "--repository",
        str(repository),
    ]
    for module, variable in suite.services:
        url = environment.get(variable)
        if not url:
            raise OnlineGateError(f"{suite_name} requires {variable}")
        preflight.extend(("--service", f"{module}={url}"))

    print(f"Online suite: {suite_name}")
    print(f"Online run ID: {run_id}")
    report: dict[str, object] = {
        "schema_version": "addp.online-gate/v1",
        "suite": suite_name,
        "scenario": suite_name,
        "run_id": run_id,
        "result": "failed",
        "failure_stage": "gate",
        "error_code": "online_gate_failed",
        "database": {
            "category": "dedicated-online",
            "name": environment.get("POSTGRES_DB", ""),
        },
        "service_addresses": {},
        "stages": {},
    }
    try:
        with online_run_lock(suite_name, run_id, lock_root):
            preflight_report, preflight_ms = run_stage(
                "preflight",
                preflight,
                repository=repository,
                environment=environment,
                timeout=timeout,
            )
            report["preflight"] = preflight_report
            report["service_addresses"] = _service_addresses(suite, environment)
            report["stages"] = {"preflight_duration_ms": preflight_ms}
            remaining = timeout - (time.monotonic() - started_at)
            if remaining <= 0:
                raise OnlineStageError(
                    "scenario",
                    "online_scenario_timeout",
                    "scenario exceeded the Online total timeout",
                )
            suite_report, scenario_ms = run_stage(
                "scenario",
                suite.command,
                repository=repository,
                environment=environment,
                timeout=remaining,
            )
            report["suite_report"] = suite_report
            report["stages"] = {
                "preflight_duration_ms": preflight_ms,
                "scenario_duration_ms": scenario_ms,
            }
            report["result"] = "passed"
            report["failure_stage"] = None
            report["error_code"] = None
    except OnlineStageError as error:
        report["failure_stage"] = error.stage
        report["error_code"] = error.error_code
        raise
    finally:
        report["duration_ms"] = round((time.monotonic() - started_at) * 1000)
        _write_report(report_path, report)
        print(f"Online report: {report_path}")
    return report


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, required=True)
    parser.add_argument("--suite", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        run_online(args.suite, args.repository.resolve(), dict(os.environ))
    except OnlineGateError as error:
        print(f"Online gate failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
