#!/usr/bin/env python3
"""Dispatch registered ADDP Online suites through one safety-checked entrypoint."""

from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
import time
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping


SUITE_PATTERN = re.compile(r"^[a-z][a-z0-9-]*$")


class OnlineGateError(RuntimeError):
    pass


@dataclass(frozen=True)
class Suite:
    command: tuple[str, ...]
    services: tuple[tuple[str, str], ...]


# Only executable owner-maintained Online suites belong here. Do not register
# placeholders: an entry means the suite is ready for real Online acceptance.
SUITES: Mapping[str, Suite] = {}


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
    if run_id:
        return run_id
    run_id = f"run-{uuid.uuid4().hex}"
    environment["ADDP_ONLINE_TEST_RUN_ID"] = run_id
    return run_id


def run_online(
    suite_name: str,
    repository: Path,
    environment: dict[str, str],
    suites: Mapping[str, Suite] = SUITES,
) -> None:
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
    subprocess.run(preflight, check=True, env=environment, timeout=timeout)
    remaining = timeout - (time.monotonic() - started_at)
    if remaining <= 0:
        raise subprocess.TimeoutExpired(suite.command, timeout)
    subprocess.run(
        suite.command,
        cwd=repository,
        check=True,
        env=environment,
        timeout=remaining,
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, required=True)
    parser.add_argument("--suite", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        run_online(args.suite, args.repository.resolve(), dict(os.environ))
    except (OnlineGateError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
        print(f"Online gate failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
