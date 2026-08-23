#!/usr/bin/env python3
"""Select the baseline and change-affected product images for CI verification."""

from __future__ import annotations

import argparse
import os
import re
import shlex
import subprocess
from pathlib import Path


BASELINE_SERVICES = {
    "system-backend",
    "agent-backend",
    "console",
    "nginx",
}
HOSTED_RUNNER_EXCLUSIONS = {
    "model3d-workflow-engine",
    "supermap-workflow-engine",
}


def registered_services(repository: Path) -> list[tuple[str, str]]:
    script = (repository / "scripts/build/build-images.sh").read_text(encoding="utf-8")
    match = re.search(r"(?ms)^\s*local services=\(\n(?P<body>.*?)^\s*\)\s*$", script)
    if not match:
        raise RuntimeError("build-images.sh services registration is missing")
    entries = shlex.split(match.group("body"), comments=True)
    registrations = []
    for entry in entries:
        name, separator, directory = entry.partition(":")
        if not separator or not name or not directory:
            raise RuntimeError(f"invalid image service registration: {entry}")
        registrations.append((name, directory))
    return registrations


def select_services(
    repository: Path,
    registrations: list[tuple[str, str]],
    changed_paths: set[str],
) -> list[str]:
    available = {name for name, _ in registrations}
    selected = BASELINE_SERVICES & available
    hosted = available - HOSTED_RUNNER_EXCLUSIONS

    for changed_path in changed_paths:
        if changed_path == ".dockerignore":
            selected.update(hosted)
            continue
        for name, directory in registrations:
            if name in HOSTED_RUNNER_EXCLUSIONS:
                continue
            if changed_path == directory or changed_path.startswith(directory + "/"):
                selected.add(name)

    if any(path == "common" or path.startswith("common/") for path in changed_paths):
        selected.update(
            name
            for name in hosted
            if name == "gateway"
            or name == "duckdb-engine"
            or name.endswith("-backend")
            or name.endswith("-worker")
        )

    if any(
        path == "common-frontend" or path.startswith("common-frontend/")
        for path in changed_paths
    ):
        selected.update(
            name
            for name in hosted
            if name == "console" or name.endswith("-frontend")
        )

    if any(
        path == "common-python" or path.startswith("common-python/")
        for path in changed_paths
    ):
        for name, directory in registrations:
            if name in HOSTED_RUNNER_EXCLUSIONS:
                continue
            dockerfiles = (repository / directory).glob("**/Dockerfile*")
            if any("common-python" in path.read_text(encoding="utf-8") for path in dockerfiles):
                selected.add(name)

    return [name for name, _ in registrations if name in selected]


def changed_paths_from_git(repository: Path) -> set[str]:
    event = os.environ.get("ADDP_CI_EVENT", "")
    head = os.environ.get("ADDP_CI_HEAD", "HEAD")
    if event == "pull_request":
        base = os.environ.get("ADDP_CI_PR_BASE", "")
    elif event == "push" and os.environ.get("ADDP_CI_BEFORE", "") not in {
        "",
        "0" * 40,
    }:
        base = os.environ["ADDP_CI_BEFORE"]
    else:
        parent = subprocess.run(
            ["git", "rev-parse", "--verify", f"{head}^"],
            cwd=repository,
            capture_output=True,
            text=True,
        )
        if parent.returncode != 0:
            return set()
        base = parent.stdout.strip()
    if not base:
        raise RuntimeError(f"base revision is required for {event or 'this'} event")
    result = subprocess.run(
        ["git", "diff", "--name-only", f"{base}...{head}"],
        cwd=repository,
        check=True,
        capture_output=True,
        text=True,
    )
    return set(result.stdout.splitlines())


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    parser.add_argument("--changed-path", action="append", default=[])
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    repository = args.repository.resolve()
    changed_paths = set(args.changed_path) or changed_paths_from_git(repository)
    services = select_services(repository, registered_services(repository), changed_paths)
    print(",".join(services))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
