#!/usr/bin/env python3
"""Dispatch registered ADDP T5 release suites through one reporting contract."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping


SUITE_PATTERN = re.compile(r"^[a-z][a-z0-9-]*$")


class ReleaseGateError(RuntimeError):
    pass


class ReleaseOwnerGateError(ReleaseGateError):
    def __init__(self, suite: str, returncode: int) -> None:
        super().__init__(f"release suite {suite!r} failed with status {returncode}")
        self.returncode = returncode


@dataclass(frozen=True)
class Suite:
    target: str
    artifact_environment: tuple[tuple[str, str], ...]
    owner_report: str | None = None


# T5 suites keep their product-specific prerequisites and owner targets. This
# registry only standardizes dispatch and reporting; it does not merge runtimes.
SUITES: Mapping[str, Suite] = {
    "agent-evaluation": Suite(
        target="test-agent-eval-release",
        artifact_environment=(("ADDP_AGENT_EVAL_REPORT", "agent-evaluation.json"),),
        owner_report="agent-evaluation.json",
    ),
    "common-python-cli": Suite(
        target="test-common-python-cli-release",
        artifact_environment=(("ADDP_CLI_RELEASE_DIST", "."),),
    ),
}


def _artifact_directory(repository: Path, environment: Mapping[str, str]) -> Path:
    configured = environment.get("ADDP_RELEASE_ARTIFACT_DIR")
    if configured:
        directory = Path(configured)
        if not directory.is_absolute():
            raise ReleaseGateError("ADDP_RELEASE_ARTIFACT_DIR must be an absolute path")
        resolved = directory.resolve()
        try:
            resolved.relative_to(repository.resolve())
        except ValueError:
            return resolved
        raise ReleaseGateError("ADDP_RELEASE_ARTIFACT_DIR must be outside the repository")
    return Path(tempfile.gettempdir()) / "addp-release-reports" / uuid.uuid4().hex


def _prepare_artifact_directory(directory: Path) -> None:
    directory.mkdir(parents=True, exist_ok=True)
    if any(directory.iterdir()):
        raise ReleaseGateError("ADDP_RELEASE_ARTIFACT_DIR must be empty before the run")


def _write_json(path: Path, payload: Mapping[str, object]) -> None:
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(
        json.dumps(payload, ensure_ascii=False, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    temporary.replace(path)


def _write_summary(path: Path, report: Mapping[str, object]) -> None:
    owner_report = report.get("owner_report") or "-"
    path.write_text(
        "\n".join(
            (
                "### T5 release suite",
                "",
                "| Suite | Owner target | Result | Owner report |",
                "| --- | --- | --- | --- |",
                f"| {report['suite']} | `{report['owner_target']}` | {report['result']} | `{owner_report}` |",
                "",
            )
        ),
        encoding="utf-8",
    )


def _artifact_files(directory: Path) -> list[str]:
    excluded = {"release-report.json", "release-summary.md"}
    return sorted(
        path.relative_to(directory).as_posix()
        for path in directory.rglob("*")
        if path.is_file() and path.name not in excluded
    )


def run_release(
    suite_name: str,
    repository: Path,
    environment: dict[str, str],
    suites: Mapping[str, Suite] = SUITES,
) -> dict[str, object]:
    if not SUITE_PATTERN.fullmatch(suite_name):
        raise ReleaseGateError("RELEASE_SUITE must use lowercase kebab-case")
    suite = suites.get(suite_name)
    if suite is None:
        available = ", ".join(sorted(suites)) or "none"
        raise ReleaseGateError(
            f"unknown RELEASE_SUITE {suite_name!r}; registered suites: {available}"
        )

    artifact_directory = _artifact_directory(repository, environment)
    _prepare_artifact_directory(artifact_directory)
    environment["ADDP_RELEASE_ARTIFACT_DIR"] = str(artifact_directory)
    environment["ADDP_RELEASE_SUITE"] = suite_name
    for variable, relative_path in suite.artifact_environment:
        environment[variable] = str((artifact_directory / relative_path).resolve())

    report_path = artifact_directory / "release-report.json"
    summary_path = artifact_directory / "release-summary.md"
    started_at = time.monotonic()
    report: dict[str, object] = {
        "schema_version": "addp.release-gate/v1",
        "suite": suite_name,
        "owner_target": suite.target,
        "owner_report": suite.owner_report,
        "result": "failed",
        "error_code": "release_owner_gate_failed",
        "artifacts": [],
    }
    command = ("make", "--no-print-directory", suite.target)
    print(f"Release suite: {suite_name}")
    print(f"Owner target: {suite.target}")
    try:
        result = subprocess.run(command, cwd=repository, env=environment, check=False)
        if result.returncode != 0:
            raise ReleaseOwnerGateError(suite_name, result.returncode)
        report["result"] = "passed"
        report["error_code"] = None
    finally:
        report["duration_ms"] = round((time.monotonic() - started_at) * 1000)
        report["artifacts"] = _artifact_files(artifact_directory)
        _write_json(report_path, report)
        _write_summary(summary_path, report)
        print(f"Release report: {report_path}")
    return report


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, required=True)
    parser.add_argument("--suite", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        run_release(args.suite, args.repository.resolve(), dict(os.environ))
    except ReleaseGateError as error:
        print(f"Release gate failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
