#!/usr/bin/env python3
"""Select one module gate from the shared ADDP change-impact matrix."""

from __future__ import annotations

import argparse
import importlib.util
import os
import subprocess
import sys
from pathlib import Path


class SelectionError(RuntimeError):
    pass


def load_changed_gate(repository: Path):
    path = repository / "scripts/test/changed-gate.py"
    spec = importlib.util.spec_from_file_location("addp_changed_gate", path)
    if spec is None or spec.loader is None:
        raise SelectionError(f"cannot load shared change-impact matrix: {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def required_environment(environment: dict[str, str], name: str) -> str:
    value = environment.get(name, "")
    if not value:
        raise SelectionError(f"{name} is required")
    return value


def selection_base(
    repository: Path,
    event: str,
    head: str,
    environment: dict[str, str],
) -> str | None:
    if event == "pull_request":
        return required_environment(environment, "ADDP_CI_PR_BASE")
    before = environment.get("ADDP_CI_BEFORE", "")
    if event == "push" and before and before != "0" * 40:
        return before
    parent = subprocess.run(
        ["git", "rev-parse", "--verify", f"{head}^"],
        cwd=repository,
        capture_output=True,
        text=True,
    )
    return f"{head}^" if parent.returncode == 0 else None


def select_module(
    repository: Path,
    module_name: str,
    environment: dict[str, str],
) -> tuple[bool, str]:
    changed_gate = load_changed_gate(repository)
    registered = changed_gate.MODULE_GATE.discover_modules(repository)
    if module_name not in registered:
        raise SelectionError(f"unknown module {module_name!r}")

    if environment.get("ADDP_CI_FORCE", "false").lower() == "true":
        return True, "forced by caller"
    event = required_environment(environment, "ADDP_CI_EVENT")
    if event in {"schedule", "workflow_dispatch"}:
        return True, f"{event} event"

    head = required_environment(environment, "ADDP_CI_HEAD")
    base = selection_base(repository, event, head, environment)
    if base is None:
        return True, "no parent revision is available"
    files = changed_gate.changed_files_between(repository, base, head)
    affected = changed_gate.affected_modules(repository, files)
    if module_name in affected:
        return True, f"shared matrix selected {module_name} from {len(files)} changed files"
    return False, f"shared matrix did not select {module_name} from {len(files)} changed files"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    parser.add_argument("--module", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    repository = args.repository.resolve()
    try:
        selected, reason = select_module(repository, args.module, dict(os.environ))
        output = required_environment(dict(os.environ), "GITHUB_OUTPUT")
        with Path(output).open("a", encoding="utf-8") as handle:
            handle.write(f"run={'true' if selected else 'false'}\n")
        print(f"{args.module} gate {'selected' if selected else 'skipped'}: {reason}.")
    except (SelectionError, subprocess.CalledProcessError) as error:
        print(f"Module gate selection failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
