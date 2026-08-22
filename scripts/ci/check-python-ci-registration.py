#!/usr/bin/env python3
"""Verify deterministic Python module gates are registered in Make and CI."""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path


class RegistrationError(RuntimeError):
    pass


PYTHON_GATE_ACTION = "uses: ./.github/actions/prepare-python-gate"


def git_files(repository: Path, *patterns: str) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "--", *patterns],
        cwd=repository,
        check=True,
        capture_output=True,
        text=True,
    )
    return [line for line in result.stdout.splitlines() if line]


def discover_python_modules(repository: Path) -> list[tuple[str, str, str]]:
    discovered = {}
    for path in git_files(repository, "*/pyproject.toml", "*/backend/requirements.txt"):
        owner = path.split("/", 1)[0]
        discovered[owner] = (owner, path, f"{owner}/*" if "/backend/" not in path else f"{owner}/backend/*")
    if not discovered:
        raise RegistrationError("no tracked Python module manifests found")
    return [discovered[owner] for owner in sorted(discovered)]


def make_dependencies(makefile: str, target: str) -> list[str] | None:
    logical = re.sub(r"\\\n\s*", " ", makefile)
    match = re.search(rf"(?m)^{re.escape(target)}\s*:(?P<dependencies>[^\n]*)", logical)
    return match.group("dependencies").split() if match else None


def workflow_jobs(repository: Path) -> list[str]:
    jobs = []
    for path in sorted((repository / ".github/workflows").glob("*.y*ml")):
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
    jobs = workflow_jobs(repository)
    root_dependencies = make_dependencies(makefile, "test") or []
    errors = []
    for owner, manifest, owner_path in discover_python_modules(repository):
        eval_target = f"test-{owner}-eval"
        target = eval_target if make_dependencies(makefile, eval_target) is not None else f"test-{owner}"
        if make_dependencies(makefile, target) is None:
            errors.append(f"{manifest}: Makefile target {target} is missing")
        if target not in root_dependencies:
            errors.append(f"{manifest}: root test dependency {target} is missing")
        target_job = next(
            (
                job
                for job in jobs
                if re.search(rf"(?m)^\s*(?:-\s*)?run:\s*make\s+{re.escape(target)}\s*$", job)
            ),
            None,
        )
        if target_job is None:
            errors.append(f"{manifest}: GitHub Actions target {target} is missing")
            continue
        if PYTHON_GATE_ACTION not in target_job:
            errors.append(f"{manifest}: Python gate setup action is missing from {target} job")
        if f"'{owner_path}'" not in target_job:
            errors.append(f"{manifest}: workflow path {owner_path} is missing")
        content = (repository / manifest).read_text(encoding="utf-8")
        if "common-python" in content and "'common-python/*'" not in target_job:
            errors.append(f"{manifest}: shared path common-python/* is missing")
    return errors


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    return parser.parse_args()


def main() -> int:
    repository = parse_args().repository.resolve()
    try:
        errors = validate_registration(repository)
        count = len(discover_python_modules(repository))
    except (RegistrationError, subprocess.CalledProcessError) as error:
        print(f"Python CI registration check failed: {error}", file=sys.stderr)
        return 1
    if errors:
        for error in errors:
            print(f"Python CI registration check failed: {error}", file=sys.stderr)
        return 1
    print(f"Python CI registration check passed: {count} modules are registered.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
