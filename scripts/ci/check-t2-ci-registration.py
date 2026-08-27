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


def repository_files(repository: Path, pattern: str) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "-z", "-co", "--exclude-standard", "--", pattern],
        cwd=repository,
        check=True,
        capture_output=True,
        text=True,
    )
    return sorted(path for path in result.stdout.split("\0") if path)


def discover_postgres_gates(repository: Path) -> list[tuple[str, str, str]]:
    gates: list[tuple[str, str, str]] = []
    for script in repository_files(repository, "scripts/test/*-postgres-gate.sh"):
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


def yaml_blocks(content: str, pattern: str) -> list[str]:
    matches = list(re.finditer(pattern, content))
    return [
        content[
            match.start() : (
                matches[index + 1].start() if index + 1 < len(matches) else len(content)
            )
        ]
        for index, match in enumerate(matches)
    ]


def validate_registration(repository: Path) -> list[str]:
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    workflow_path = repository / ".github/workflows/release-and-t2-gates.yml"
    if not workflow_path.is_file():
        raise RegistrationError(".github/workflows/release-and-t2-gates.yml is missing")
    workflow = workflow_path.read_text(encoding="utf-8")
    jobs = yaml_blocks(workflow, r"(?m)^  [a-zA-Z0-9_-]+:\s*$")
    steps = yaml_blocks(workflow, r"(?m)^      - name:\s*.+$")
    errors: list[str] = []
    integration_recipe = make_recipe(makefile, "test-integration")

    if integration_recipe is None:
        errors.append("Makefile target test-integration is missing")

    for script, target, owner in discover_postgres_gates(repository):
        recipe = make_recipe(makefile, target)
        if recipe is None:
            errors.append(f"{script}: Makefile target {target} is missing")
        elif script not in recipe:
            errors.append(f"{script}: Makefile target {target} does not invoke its owner script")
        if integration_recipe is not None and not re.search(
            rf"(?m)^\t@?\$\(MAKE\)\s+{re.escape(target)}\s*$",
            integration_recipe,
        ):
            errors.append(
                f"{script}: root test-integration does not invoke {target} sequentially"
            )
        target_job = next(
            (
                job
                for job in jobs
                if re.search(
                    rf"(?m)^\s*(?:-\s*)?run:\s*make\s+{re.escape(target)}\s*$",
                    job,
                )
            ),
            None,
        )
        if target_job is None:
            errors.append(f"{script}: GitHub Actions target {target} is missing")
        elif not re.search(
            r"(?m)^\s*image:\s*postgres:15@sha256:[0-9a-f]{64}\s*$",
            target_job,
        ):
            errors.append(f"{script}: PostgreSQL 15 service image is not pinned in {target} job")

        selection_step = next(
            (
                step
                for step in steps
                if re.search(rf"(?m)^\s*id:\s*{re.escape(owner)}\s*$", step)
            ),
            None,
        )
        if selection_step is None:
            errors.append(f"{script}: selection step id {owner} is missing")
            errors.append(f"{script}: shared module change selector is missing")
            continue
        if not re.search(
            rf"python3\s+scripts/ci/select-module-gate\.py\s+--module\s+['\"]?{re.escape(owner)}['\"]?",
            selection_step,
        ):
            errors.append(f"{script}: shared module change selector is missing")
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
