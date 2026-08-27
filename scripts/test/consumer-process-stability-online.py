#!/usr/bin/env python3
"""Capture and verify that Online consumer processes were not restarted."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
from pathlib import Path


PROCESS_NAMES = (
    "manager",
    "service",
    "console-frontend",
    "manager-frontend",
    "service-frontend",
)


class StabilityError(RuntimeError):
    pass


def process_command(pid: int) -> str:
    result = subprocess.run(
        ["ps", "-p", str(pid), "-o", "command="],
        capture_output=True,
        text=True,
        check=False,
    )
    return result.stdout.strip() if result.returncode == 0 else ""


def read_processes(repository: Path) -> dict[str, dict[str, object]]:
    processes: dict[str, dict[str, object]] = {}
    for name in PROCESS_NAMES:
        pid_path = repository / ".dev-pids" / f"{name}.pid"
        if not pid_path.is_file():
            raise StabilityError(f"missing managed PID file: {pid_path.name}")
        try:
            pid = int(pid_path.read_text(encoding="utf-8").strip())
            os.kill(pid, 0)
        except (OSError, ValueError) as error:
            raise StabilityError(f"managed process is not alive: {name}") from error
        processes[name] = {"pid": pid, "command": process_command(pid)}
    return processes


def verify_processes(
    expected: object, current: dict[str, dict[str, object]]
) -> None:
    if not isinstance(expected, dict):
        raise StabilityError("process snapshot is invalid")
    restarted = [
        name for name in PROCESS_NAMES
        if expected.get(name, {}).get("pid") != current[name]["pid"]
        or (
            expected.get(name, {}).get("command")
            and expected.get(name, {}).get("command") != current[name]["command"]
        )
    ]
    if restarted:
        raise StabilityError("consumer processes restarted: " + ", ".join(restarted))


def main() -> int:
    parser = argparse.ArgumentParser()
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--capture", action="store_true")
    mode.add_argument("--verify", action="store_true")
    parser.add_argument("--repository", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    try:
        repository = args.repository.resolve()
        current = read_processes(repository)
        if args.capture:
            report = {
                "schema_version": "addp.consumer-process-stability/v1",
                "verified": False,
                "processes": current,
            }
        else:
            report = json.loads(args.output.read_text(encoding="utf-8"))
            expected = report.get("processes")
            verify_processes(expected, current)
            report["verified"] = True
            report["consumer_processes_restarted"] = 0
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(
            json.dumps(report, ensure_ascii=False, sort_keys=True) + "\n",
            encoding="utf-8",
        )
        print(json.dumps(report, ensure_ascii=False, sort_keys=True))
        return 0
    except (OSError, json.JSONDecodeError, StabilityError) as error:
        print(f"consumer process stability failed: {error}", file=os.sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
