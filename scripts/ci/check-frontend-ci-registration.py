#!/usr/bin/env python3
"""Verify that every tracked ADDP frontend is registered in the CI contract."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path


class RegistrationError(RuntimeError):
    pass


FRONTEND_GATE_ACTION = "uses: ./.github/actions/prepare-frontend-gate"
MODULE_GATE_SELECTOR = "python3 scripts/ci/select-module-gate.py"


def git_files(repository: Path, pattern: str) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", pattern],
        cwd=repository,
        check=True,
        capture_output=True,
        text=True,
    )
    return [line for line in result.stdout.splitlines() if line]


def discover_frontends(repository: Path) -> list[str]:
    modules: list[str] = []
    for relative_path in git_files(repository, "*/frontend/package.json"):
        package_path = repository / relative_path
        package = json.loads(package_path.read_text(encoding="utf-8"))
        scripts = package.get("scripts")
        if not isinstance(scripts, dict) or not scripts.get("build"):
            raise RegistrationError(f"{relative_path} must declare scripts.build")
        modules.append(relative_path.split("/", 1)[0])
    if not modules:
        raise RegistrationError("no tracked */frontend/package.json files found")
    return sorted(modules)


def workflow_jobs(repository: Path) -> list[str]:
    jobs: list[str] = []
    workflow_paths = sorted((repository / ".github/workflows").glob("*.yml"))
    workflow_paths.extend(sorted((repository / ".github/workflows").glob("*.yaml")))
    if not workflow_paths:
        raise RegistrationError("no GitHub Actions workflows found")
    for path in workflow_paths:
        content = path.read_text(encoding="utf-8")
        matches = list(re.finditer(r"(?m)^  [a-zA-Z0-9_-]+:\s*$", content))
        jobs.extend(
            content[
                match.start() : (
                    matches[index + 1].start() if index + 1 < len(matches) else len(content)
                )
            ]
            for index, match in enumerate(matches)
        )
    return jobs


def validate_registration(repository: Path) -> list[str]:
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    logical_makefile = re.sub(r"\\\n\s*", " ", makefile)
    jobs = workflow_jobs(repository)
    errors: list[str] = []
    modules = discover_frontends(repository)
    for module in modules:
        target = f"test-{module}-frontend"
        if not re.search(rf"(?m)^{re.escape(target)}\s*:", makefile):
            errors.append(f"{module}: Makefile target {target} is missing")
        target_job = next(
            (
                job
                for job in jobs
                if re.search(rf"(?m)(?:target:\s*|make\s+){re.escape(target)}\s*$", job)
            ),
            None,
        )
        if target_job is None:
            errors.append(f"{module}: GitHub Actions target {target} is missing")
            errors.append(f"{module}: shared module change selector is missing")
            continue
        if FRONTEND_GATE_ACTION not in target_job:
            errors.append(f"{module}: standard frontend gate setup is missing from {target} job")
        direct_selector = re.search(
            rf"{re.escape(MODULE_GATE_SELECTOR)}\s+--module\s+['\"]?{re.escape(module)}['\"]?",
            target_job,
        )
        matrix_selector = (
            MODULE_GATE_SELECTOR in target_job
            and "--module '${{ matrix.module }}'" in target_job
            and module in re.findall(
                r"(?m)^\s*- module:\s*([a-z][a-z0-9-]*)\s*$", target_job
            )
        )
        if direct_selector is None and not matrix_selector:
            errors.append(f"{module}: shared module change selector is missing from {target} job")
        test_target = re.search(r"(?m)^test\s*:(?P<dependencies>[^\n]*)", logical_makefile)
        if (
            test_target is None
            or target not in test_target.group("dependencies").split()
        ):
            errors.append(
                f"{module}: root test target dependency {target} is missing"
            )
    return errors


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    return parser.parse_args()


def main() -> int:
    repository = parse_args().repository.resolve()
    try:
        errors = validate_registration(repository)
    except (RegistrationError, json.JSONDecodeError, subprocess.CalledProcessError) as error:
        print(f"Frontend CI registration check failed: {error}", file=sys.stderr)
        return 1
    if errors:
        for error in errors:
            print(f"Frontend CI registration check failed: {error}", file=sys.stderr)
        return 1
    count = len(discover_frontends(repository))
    print(f"Frontend CI registration check passed: {count} tracked frontends are registered.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
