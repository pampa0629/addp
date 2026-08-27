#!/usr/bin/env python3
"""Run and validate the real-browser consumer Engine recovery Online suite."""

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
from pathlib import Path


class SuiteError(RuntimeError):
    pass


def required_environment(
    environment: dict[str, str], *, require_run_id: bool = False
) -> dict[str, str]:
    required = [
        "GATEWAY_URL",
        "CONSOLE_URL",
        "ADDP_ONLINE_ARTIFACT_DIR",
        "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN",
        "ADDP_ONLINE_TEST_USER_USERNAME",
        "ADDP_ONLINE_TEST_USER_PASSWORD",
        "ADDP_ONLINE_TEST_TENANT_ID",
        "ADDP_ONLINE_TEST_ENGINE_ID",
        "ADDP_ONLINE_TEST_ENGINE_NAME",
        "ADDP_ONLINE_TEST_ENGINE_PORT",
        "ADDP_ONLINE_TEST_ENGINE_USER",
        "ADDP_ONLINE_TEST_ENGINE_PASSWORD",
        "ADDP_ONLINE_TEST_ENGINE_DATABASE",
    ]
    if require_run_id:
        required.append("ADDP_ONLINE_TEST_RUN_ID")
    missing = [name for name in required if not environment.get(name)]
    if missing:
        raise SuiteError("missing required environment: " + ", ".join(missing))
    return {name: environment[name] for name in required}


def request_json(
    environment: dict[str, str], method: str, path: str
) -> tuple[int, dict[str, object]]:
    base_url = environment["GATEWAY_URL"].rstrip("/")
    parsed = urllib.parse.urlsplit(base_url)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise SuiteError("GATEWAY_URL must be an absolute HTTP(S) URL")
    request = urllib.request.Request(
        base_url + path,
        method=method,
        headers={
            "Accept": "application/json",
            "Authorization": f"Bearer {environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN']}",
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            status = response.status
            raw = response.read()
    except urllib.error.HTTPError as error:
        status = error.code
        raw = error.read()
    except (urllib.error.URLError, TimeoutError) as error:
        raise SuiteError(f"{method} {path} transport failed: {error}") from error
    try:
        payload = json.loads(raw) if raw else {}
    except json.JSONDecodeError as error:
        raise SuiteError(f"{method} {path} returned invalid JSON") from error
    if not isinstance(payload, dict):
        raise SuiteError(f"{method} {path} response must be a JSON object")
    return status, payload


def validate_engine_fixture(environment: dict[str, str]) -> dict[str, object]:
    engine_id = int(environment["ADDP_ONLINE_TEST_ENGINE_ID"])
    status, engine = request_json(
        environment, "GET", f"/api/v1/system/engines/{engine_id}"
    )
    if status != 200:
        raise SuiteError(f"Engine detail returned HTTP {status}")
    expected = {
        "name": environment["ADDP_ONLINE_TEST_ENGINE_NAME"],
        "engine_type": "postgresql",
        "lifecycle_state": "active",
    }
    mismatches = [key for key, value in expected.items() if engine.get(key) != value]
    if mismatches:
        raise SuiteError("configured Engine Fixture mismatch: " + ", ".join(mismatches))
    return engine


def restore_engine_online(environment: dict[str, str], timeout: float = 60.0) -> dict[str, object]:
    engine_id = int(environment["ADDP_ONLINE_TEST_ENGINE_ID"])
    validate_engine_fixture(environment)
    status, payload = request_json(
        environment, "POST", f"/api/v1/system/engines/{engine_id}/test"
    )
    if status != 200:
        raise SuiteError(f"Engine test returned HTTP {status}: {payload.get('error_code', 'unknown')}")
    deadline = time.monotonic() + timeout
    last_status = "unknown"
    while time.monotonic() < deadline:
        status, engine = request_json(
            environment, "GET", f"/api/v1/system/engines/{engine_id}"
        )
        if status != 200:
            raise SuiteError(f"Engine detail returned HTTP {status}")
        last_status = str(engine.get("connection_status", "unknown"))
        if last_status == "online":
            return {
                "engine_id": engine_id,
                "engine_name": engine.get("name"),
                "connection_status": last_status,
            }
        time.sleep(1)
    raise SuiteError(f"Engine {engine_id} did not recover online; last status={last_status}")


def validate_browser_report(report: object, environment: dict[str, str]) -> dict[str, object]:
    if not isinstance(report, dict):
        raise SuiteError("browser report must be a JSON object")
    expected = {
        "schema_version": "addp.consumer-engine-recovery/v1",
        "suite": "consumer-engine-recovery",
        "run_id": environment["ADDP_ONLINE_TEST_RUN_ID"],
        "result": "passed",
        "engine_id": int(environment["ADDP_ONLINE_TEST_ENGINE_ID"]),
        "final_connection_status": "online",
        "consumer_processes_restarted": 0,
    }
    mismatches = [key for key, value in expected.items() if report.get(key) != value]
    if mismatches:
        raise SuiteError("browser report contract mismatch: " + ", ".join(mismatches))
    return report


def run_browser(repository: Path, environment: dict[str, str]) -> dict[str, object]:
    artifact_dir = Path(environment["ADDP_ONLINE_ARTIFACT_DIR"])
    report_path = artifact_dir / "consumer-engine-recovery-browser.json"
    browser_environment = dict(environment)
    browser_environment["ADDP_ONLINE_REPOSITORY"] = str(repository)
    result = subprocess.run(
        [
            "npm",
            "run",
            "test:e2e",
            "--",
            "--config=playwright.online.config.js",
            "e2e/online/consumer-engine-recovery.spec.js",
        ],
        cwd=repository / "console/frontend",
        env=browser_environment,
        text=True,
        capture_output=True,
    )
    if result.stdout:
        print(result.stdout, end="" if result.stdout.endswith("\n") else "\n")
    if result.stderr:
        print(result.stderr, end="" if result.stderr.endswith("\n") else "\n", file=sys.stderr)
    if result.returncode != 0:
        raise SuiteError(f"Playwright exited with status {result.returncode}")
    if not report_path.is_file():
        raise SuiteError("Playwright did not write consumer-engine-recovery-browser.json")
    return validate_browser_report(
        json.loads(report_path.read_text(encoding="utf-8")), environment
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--restore-only", action="store_true")
    args = parser.parse_args()
    environment = dict(os.environ)
    try:
        required_environment(environment, require_run_id=not args.restore_only)
        if args.restore_only:
            print(json.dumps(restore_engine_online(environment), ensure_ascii=False, sort_keys=True))
            return 0
        repository = Path(environment.get("ADDP_ONLINE_REPOSITORY", Path(__file__).parents[2])).resolve()
        report = run_browser(repository, environment)
        print(json.dumps(report, ensure_ascii=False, sort_keys=True))
        return 0
    except (OSError, ValueError, SuiteError, json.JSONDecodeError) as error:
        print(f"consumer-engine-recovery Online suite failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
