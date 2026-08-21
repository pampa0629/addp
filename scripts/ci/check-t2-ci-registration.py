#!/usr/bin/env python3
"""Verify registration of hosted disposable-service T2 gates."""

from __future__ import annotations

import argparse
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


def discover_postgres_gates(repository: Path) -> list[tuple[str, str, str]]:
    gates: list[tuple[str, str, str]] = []
    for script in git_files(repository, "scripts/test/*-postgres-gate.sh"):
        name = Path(script).name.removesuffix("-gate.sh")
        owner = name.split("-", 1)[0]
        gates.append((script, f"test-{name}", owner))
    if not gates:
        raise RegistrationError("no tracked scripts/test/*-postgres-gate.sh files found")
    return sorted(gates)


def make_recipe(makefile: str, target: str) -> str | None:
    match = re.search(
        rf"(?ms)^{re.escape(target)}\s*:[^\n]*\n(?P<recipe>(?:\t[^\n]*\n?)*)",
        makefile,
    )
    return match.group("recipe") if match else None


def validate_registration(repository: Path) -> list[str]:
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    workflow_path = repository / ".github/workflows/release-and-t2-gates.yml"
    if not workflow_path.is_file():
        raise RegistrationError(".github/workflows/release-and-t2-gates.yml is missing")
    workflow = workflow_path.read_text(encoding="utf-8")
    errors: list[str] = []

    if not re.search(r"(?m)^\s*image:\s*postgres:15@sha256:[0-9a-f]{64}\s*$", workflow):
        errors.append("Release/T2 workflow must pin the PostgreSQL 15 service image by digest")

    for script, target, owner in discover_postgres_gates(repository):
        recipe = make_recipe(makefile, target)
        if recipe is None:
            errors.append(f"{script}: Makefile target {target} is missing")
        elif script not in recipe:
            errors.append(f"{script}: Makefile target {target} does not invoke its owner script")
        if not re.search(
            rf"(?m)^\s*(?:-\s*)?run:\s*make\s+{re.escape(target)}\s*$",
            workflow,
        ):
            errors.append(f"{script}: GitHub Actions target {target} is missing")
        if f"'{script}'" not in workflow:
            errors.append(f"{script}: gate script path registration is missing")
        if f"'{owner}/backend/*'" not in workflow:
            errors.append(f"{script}: owner path {owner}/backend/* is missing")
    return errors


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    return parser.parse_args()


def main() -> int:
    repository = parse_args().repository.resolve()
    try:
        errors = validate_registration(repository)
        count = len(discover_postgres_gates(repository))
    except (RegistrationError, subprocess.CalledProcessError) as error:
        print(f"T2 CI registration check failed: {error}", file=sys.stderr)
        return 1
    if errors:
        for error in errors:
            print(f"T2 CI registration check failed: {error}", file=sys.stderr)
        return 1
    print(f"T2 CI registration check passed: {count} PostgreSQL gates are registered.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
