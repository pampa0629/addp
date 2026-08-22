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


def validate_registration(repository: Path) -> list[str]:
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    logical_makefile = re.sub(r"\\\n\s*", " ", makefile)
    workflow_paths = sorted((repository / ".github/workflows").glob("*.yml"))
    workflow_paths.extend(sorted((repository / ".github/workflows").glob("*.yaml")))
    if not workflow_paths:
        raise RegistrationError("no GitHub Actions workflows found")
    workflows = "\n".join(path.read_text(encoding="utf-8") for path in workflow_paths)
    errors: list[str] = []
    modules = discover_frontends(repository)
    for module in modules:
        target = f"test-{module}-frontend"
        if not re.search(rf"(?m)^{re.escape(target)}\s*:", makefile):
            errors.append(f"{module}: Makefile target {target} is missing")
        if not re.search(rf"(?m)(?:target:\s*|make\s+){re.escape(target)}\s*$", workflows):
            errors.append(f"{module}: GitHub Actions target {target} is missing")
        path_pattern = f"'{module}/frontend/*'"
        if path_pattern not in workflows and module not in re.findall(
            r"(?m)^\s*- module:\s*([a-z][a-z0-9-]*)\s*$", workflows
        ):
            errors.append(f"{module}: GitHub Actions path registration is missing")
        test_target = re.search(r"(?m)^test\s*:(?P<dependencies>[^\n]*)", logical_makefile)
        aggregate_dependency = "test-agent-eval" if module == "agent" else target
        if (
            test_target is None
            or aggregate_dependency not in test_target.group("dependencies").split()
        ):
            errors.append(
                f"{module}: root test target dependency {aggregate_dependency} is missing"
            )
    if "agent" in modules:
        agent_eval = re.search(
            r"(?m)^test-agent-eval\s*:(?P<dependencies>[^\n]*)", logical_makefile
        )
        if (
            agent_eval is None
            or "test-agent-frontend" not in agent_eval.group("dependencies").split()
        ):
            errors.append(
                "agent: test-agent-eval dependency test-agent-frontend is missing"
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
