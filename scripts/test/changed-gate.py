#!/usr/bin/env python3
"""Run registered T0-T3 gates affected by repository changes."""

from __future__ import annotations

import argparse
import importlib.util
import re
import subprocess
import sys
from pathlib import Path


def load_module_gate():
    path = Path(__file__).with_name("module-gate.py")
    spec = importlib.util.spec_from_file_location("addp_module_gate", path)
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


MODULE_GATE = load_module_gate()

GLOBAL_GATE_CONTROL_PATHS = (
    ".github/actions/",
    ".github/workflows/",
    "scripts/ci/",
)
GLOBAL_GATE_CONTROL_FILES = {
    "Makefile",
    "scripts/test/changed-gate.py",
    "scripts/test/changed-gate_test.py",
    "scripts/test/module-gate.py",
    "scripts/test/module-gate_test.py",
}


def git_lines(repository: Path, arguments: list[str]) -> list[str]:
    result = subprocess.run(
        ["git", *arguments],
        cwd=repository,
        check=True,
        capture_output=True,
        text=True,
    )
    return [line for line in result.stdout.splitlines() if line]


def changed_files(repository: Path, base_ref: str | None) -> list[str]:
    reference = base_ref or "HEAD"
    files = set(git_lines(repository, ["diff", "--name-only", reference, "--"]))
    files.update(git_lines(repository, ["ls-files", "--others", "--exclude-standard"]))
    return sorted(files)


def changed_files_between(repository: Path, base_ref: str, head_ref: str) -> list[str]:
    return sorted(
        set(
            git_lines(
                repository,
                ["diff", "--name-only", f"{base_ref}...{head_ref}", "--"],
            )
        )
    )


def file_consumers(repository: Path, pattern: str, marker: str) -> set[str]:
    consumers = set()
    for relative_path in MODULE_GATE.git_files(repository, pattern):
        path = repository / relative_path
        # git ls-files still reports tracked paths deleted in the worktree. A
        # rename or removal must not make the changed gate try to read them.
        if not path.is_file():
            continue
        if marker in path.read_text(encoding="utf-8", errors="ignore"):
            consumers.add(relative_path.split("/", 1)[0])
    return consumers


def affected_modules(repository: Path, files: list[str]) -> list[str]:
    registered = MODULE_GATE.discover_modules(repository)
    if any(
        path in GLOBAL_GATE_CONTROL_FILES
        or path.startswith(GLOBAL_GATE_CONTROL_PATHS)
        for path in files
    ):
        return sorted(registered)

    affected = {
        path.split("/", 1)[0]
        for path in files
        if "/" in path and path.split("/", 1)[0] in registered
    }
    roots = {path.split("/", 1)[0] for path in files if "/" in path}

    for path in files:
        evaluation_match = re.fullmatch(r"evals/([a-z][a-z0-9-]*)-scenarios(?:/.*)?", path)
        if evaluation_match:
            affected.add(evaluation_match.group(1))
        for module in registered:
            if re.fullmatch(
                rf"scripts/test/{re.escape(module)}-.+-gate\.sh",
                path,
            ):
                affected.add(module)

    if "common" in roots:
        affected.add("common")
        affected.update(
            file_consumers(repository, "*/go.mod", "github.com/addp/common")
        )
        affected.update(
            file_consumers(repository, "*/*/go.mod", "github.com/addp/common")
        )
    if "common-frontend" in roots:
        affected.update(
            file_consumers(
                repository,
                "*/frontend/*",
                "common-frontend",
            )
        )
    if "common-python" in roots:
        affected.add("common-python")
        affected.update(
            file_consumers(repository, "*/backend/requirements*.txt", "common-python")
        )
    return sorted(affected & registered)


def plan_changed(repository: Path, files: list[str]) -> list:
    steps = [MODULE_GATE.Step("platform T0", ("make", "test-platform"), repository)]
    seen = {(steps[0].command, steps[0].cwd)}
    for module in affected_modules(repository, files):
        for step in MODULE_GATE.plan_module(repository, module, include_platform=False):
            identity = (step.command, step.cwd)
            if identity not in seen:
                steps.append(step)
                seen.add(identity)
    return steps


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    parser.add_argument("--base-ref")
    parser.add_argument("--dry-run", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    repository = args.repository.resolve()
    try:
        files = changed_files(repository, args.base_ref)
        modules = affected_modules(repository, files)
        print("Changed files: " + (str(len(files)) if files else "none"), flush=True)
        print("Affected registered modules: " + (", ".join(modules) or "none"), flush=True)
        MODULE_GATE.run_steps(plan_changed(repository, files), args.dry_run)
    except (MODULE_GATE.ModuleGateError, subprocess.CalledProcessError) as error:
        print(f"Changed gate failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
