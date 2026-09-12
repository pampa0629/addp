#!/usr/bin/env python3
"""Discover and run one ADDP module's registered T0-T3 gates."""

from __future__ import annotations

import argparse
import fnmatch
import os
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


class ModuleGateError(RuntimeError):
    pass


@dataclass(frozen=True)
class Step:
    label: str
    command: tuple[str, ...]
    cwd: Path
    environment: tuple[tuple[str, str], ...] = ()
    excluded_environment: tuple[str, ...] = ()
    required_environment: tuple[tuple[str, ...], ...] = ()


GO_T1_EXCLUDED_ENVIRONMENT = (
    "*_POSTGRES_TEST_DSN",
    "ADDP_*_INTEGRATION",
)
T2_SERVICES_PATTERN = re.compile(
    r"(?m)^# ADDP_T2_SERVICES=(?P<services>[a-z0-9_-]+(?:,[a-z0-9_-]+)*)\s*$"
)
T2_REQUIRED_ENVIRONMENT_PATTERN = re.compile(
    r"(?m)^# ADDP_T2_REQUIRED_ENV=(?P<requirements>"
    r"[A-Z][A-Z0-9_]*(?:\|[A-Z][A-Z0-9_]*)*"
    r"(?:,[A-Z][A-Z0-9_]*(?:\|[A-Z][A-Z0-9_]*)*)*)\s*$"
)
T2_REQUIRED_ENVIRONMENT_DECLARATION_PATTERN = re.compile(
    r"(?m)^# ADDP_T2_REQUIRED_ENV=.*$"
)


def step_environment(step: Step, base: dict[str, str] | None = None) -> dict[str, str]:
    environment = dict(os.environ if base is None else base)
    for pattern in step.excluded_environment:
        for name in tuple(environment):
            if fnmatch.fnmatchcase(name, pattern):
                environment.pop(name)
    environment.update(step.environment)
    return environment


def git_files(repository: Path, *patterns: str) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "-z", "--", *patterns],
        cwd=repository,
        check=True,
        capture_output=True,
        text=True,
    )
    return [path for path in result.stdout.split("\0") if path]


def repository_files(repository: Path, *patterns: str) -> list[str]:
    """Return tracked and untracked worktree files for pre-commit module gates."""
    result = subprocess.run(
        ["git", "ls-files", "-z", "-co", "--exclude-standard", "--", *patterns],
        cwd=repository,
        check=True,
        capture_output=True,
        text=True,
    )
    return sorted(
        path
        for path in result.stdout.split("\0")
        if path and (repository / path).is_file()
    )


def hosted_t2_scripts(repository: Path) -> list[str]:
    return [
        path
        for path in repository_files(repository, "scripts/test/*-gate.sh")
        if T2_SERVICES_PATTERN.search(
            (repository / path).read_text(encoding="utf-8")
        )
    ]


def required_t2_environment(script: str) -> tuple[tuple[str, ...], ...]:
    declarations = T2_REQUIRED_ENVIRONMENT_DECLARATION_PATTERN.findall(script)
    if len(declarations) > 1:
        raise ModuleGateError("T2 gate must declare ADDP_T2_REQUIRED_ENV at most once")
    if not declarations:
        return ()
    match = T2_REQUIRED_ENVIRONMENT_PATTERN.fullmatch(declarations[0])
    if match is None:
        raise ModuleGateError(
            "ADDP_T2_REQUIRED_ENV must contain uppercase names separated by commas; "
            "use | only for accepted alternatives"
        )
    return tuple(
        tuple(requirement.split("|"))
        for requirement in match.group("requirements").split(",")
    )


def make_target(makefile: str, target: str) -> re.Match[str] | None:
    logical_makefile = re.sub(r"\\\n\s*", " ", makefile)
    return re.search(
        rf"(?m)^{re.escape(target)}\s*:(?P<dependencies>[^\n]*)",
        logical_makefile,
    )


def discover_modules(repository: Path) -> set[str]:
    modules = {
        path.split("/", 1)[0]
        for path in repository_files(
            repository,
            "go.mod",
            "*/go.mod",
            "*/*/go.mod",
            "*/frontend/package.json",
            "*/pyproject.toml",
            "*/backend/requirements.txt",
        )
        if not path.startswith("scripts/test/")
    }
    for path in hosted_t2_scripts(repository):
        modules.add(Path(path).name.split("-", 1)[0])
    return modules


def plan_module(repository: Path, module: str, include_platform: bool = True) -> list[Step]:
    if not re.fullmatch(r"[a-z][a-z0-9-]*", module):
        raise ModuleGateError("MODULE must be a lowercase ADDP module name")
    modules = discover_modules(repository)
    if module not in modules:
        available = ", ".join(sorted(modules))
        raise ModuleGateError(f"unknown MODULE {module!r}; available modules: {available}")

    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    steps = []
    if include_platform:
        steps.append(Step("platform T0", ("make", "test-platform"), repository))

    go_modules = []
    for path in repository_files(repository, "go.mod", "*/go.mod", "*/*/go.mod"):
        parent = str(Path(path).parent)
        owner = path.split("/", 1)[0] if "/" in path else "."
        if owner == module:
            go_modules.append(parent)
    for relative_path in sorted(go_modules):
        steps.append(
            Step(
                f"{module} Go T1 ({relative_path})",
                ("go", "test", "./..."),
                repository / relative_path,
                (("GOWORK", "off"),),
                GO_T1_EXCLUDED_ENVIRONMENT,
            )
        )

    frontend_target = f"test-{module}-frontend"
    eval_target = f"test-{module}-eval"
    eval_match = make_target(makefile, eval_target)
    if eval_match is not None:
        steps.append(Step(f"{module} evaluation T1", ("make", eval_target), repository))
    frontend_path = f"{module}/frontend/package.json"
    if frontend_path in repository_files(repository, "*/frontend/package.json"):
        frontend_match = make_target(makefile, frontend_target)
        if frontend_match is None:
            raise ModuleGateError(f"Makefile target {frontend_target} is missing")
        eval_dependencies = eval_match.group("dependencies").split() if eval_match else []
        if frontend_target not in eval_dependencies:
            steps.append(
                Step(f"{module} frontend T1/T3", ("make", frontend_target), repository)
            )

    python_paths = repository_files(repository, "*/pyproject.toml", "*/backend/requirements.txt")
    owns_python = any(path.split("/", 1)[0] == module for path in python_paths)
    if owns_python and eval_match is None:
        python_target = f"test-{module}"
        if make_target(makefile, python_target) is None:
            raise ModuleGateError(f"Makefile target {python_target} is missing")
        steps.append(Step(f"{module} Python T1", ("make", python_target), repository))

    integration_scripts = hosted_t2_scripts(repository)
    registered_targets: set[str] = set()
    for path in integration_scripts:
        name = Path(path).name.removesuffix("-gate.sh")
        if name.split("-", 1)[0] != module:
            continue
        target = f"test-{name}"
        if target in registered_targets or make_target(makefile, target) is None:
            continue
        registered_targets.add(target)
        script = (repository / path).read_text(encoding="utf-8")
        steps.append(
            Step(
                f"{module} integration T2",
                ("make", target),
                repository,
                required_environment=required_t2_environment(script),
            )
        )

    minimum_step_count = 1 if include_platform else 0
    if len(steps) == minimum_step_count:
        raise ModuleGateError(f"MODULE {module!r} has no registered module gate")
    return steps


def local_postgres_environment_example(name: str) -> str | None:
    if "POSTGRES" not in name or not name.endswith("_DSN"):
        return None
    database = (
        "addp_iam_test" if name == "ADDP_SYSTEM_POSTGRES_TEST_DSN" else "addp_test"
    )
    return f"postgres://addp:addp_password@127.0.0.1:15432/{database}?sslmode=disable"


def preflight_required_environment(
    steps: list[Step], base_environment: dict[str, str]
) -> None:
    missing = [
        (
            step,
            tuple(
                alternatives
                for alternatives in step.required_environment
                if not any(
                    base_environment.get(name, "").strip()
                    for name in alternatives
                )
            ),
        )
        for step in steps
    ]
    missing = [(step, names) for step, names in missing if names]
    if not missing:
        return

    lines = ["required T2 environment is missing before gate execution:"]
    for step, requirements in missing:
        target = step.command[1] if len(step.command) > 1 else " ".join(step.command)
        requirement_text = ", ".join(" or ".join(group) for group in requirements)
        lines.append(f"- {target}: {requirement_text}")

        names = [group[0] for group in requirements if len(group) == 1]
        examples = [(name, local_postgres_environment_example(name)) for name in names]
        if (
            len(names) == len(requirements)
            and examples
            and all(value is not None for _, value in examples)
            and target.startswith("test-")
        ):
            module = target.removeprefix("test-").split("-", 1)[0]
            assignments = " ".join(f"{name}='{value}'" for name, value in examples)
            lines.append(
                f"  local PostgreSQL example: {assignments} "
                f"make test-module MODULE={module}"
            )
    lines.append(
        "Configure only allowed disposable test resources; see scripts/infra/README.md."
    )
    raise ModuleGateError("\n".join(lines))


def run_steps(
    steps: list[Step],
    dry_run: bool,
    base_environment: dict[str, str] | None = None,
) -> None:
    environment = dict(os.environ if base_environment is None else base_environment)
    if not dry_run:
        preflight_required_environment(steps, environment)
    for step in steps:
        command = " ".join(step.command)
        print(f"==> {step.label}: {command} (cwd={step.cwd})", flush=True)
        if dry_run:
            continue
        subprocess.run(
            step.command,
            cwd=step.cwd,
            env=step_environment(step, environment),
            check=True,
        )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    parser.add_argument("--module", required=True)
    parser.add_argument("--dry-run", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        steps = plan_module(args.repository.resolve(), args.module)
        run_steps(steps, args.dry_run)
    except (ModuleGateError, subprocess.CalledProcessError) as error:
        print(f"Module gate failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
