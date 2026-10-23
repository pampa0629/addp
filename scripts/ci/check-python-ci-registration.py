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
MODULE_GATE_SELECTOR = "python3 scripts/ci/select-module-gate.py"


def git_files(repository: Path, *patterns: str) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "--", *patterns],
        cwd=repository,
        check=True,
        capture_output=True,
        text=True,
    )
    return [line for line in result.stdout.splitlines() if line]


def discover_python_modules(repository: Path) -> list[tuple[str, str]]:
    discovered = {}
    for path in git_files(repository, "*/pyproject.toml", "*/backend/requirements.txt"):
        owner = path.split("/", 1)[0]
        discovered[owner] = (owner, path)
    # A named Make target opts an owner runtime into the deterministic gate contract.
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    for path in git_files(repository, "*/*/requirements.txt"):
        runtime = Path(path).parent.name
        if make_dependencies(makefile, f"test-{runtime}") is not None:
            discovered[runtime] = (runtime, path)
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


def workflow_steps(job: str) -> list[tuple[int, str]]:
    matches = list(re.finditer(r"(?m)^      - ", job))
    return [
        (match.start(), job[match.start():matches[index + 1].start() if index + 1 < len(matches) else len(job)])
        for index, match in enumerate(matches)
    ]


def step_setting(step: str, name: str) -> str:
    match = re.search(rf"(?m)^\s+{re.escape(name)}:\s*(.*?)\s*$", step)
    return match.group(1).strip("\"'") if match else ""


def validate_topology_capacity(repository: Path) -> list[str]:
    """Keep the manual matrix separate from ordinary Python/Online gates."""
    required = {
        'Makefile': ('test-vector-topology-capacity:', 'test-vector-topology-capacity-runner:',
                     'test_topology_capacity.py', '@$(MAKE) test-vector-topology-capacity-runner'),
        '.github/workflows/vector-topology-capacity.yml': ('  workflow_dispatch:',
                     'run: make test-vector-topology-capacity\n', 'run: make test-vector-topology-capacity-runner',
                     'runs-on: ubuntu-24.04', 'if: always()', 'actions/upload-artifact@',
                     'default: matrix', '          - observation', 'TOPOLOGY_CAPACITY_PROFILE: ${{ inputs.profile }}'),
        'scripts/test/vector-topology-capacity.py': ('CPUS = 4', 'MEMORY = 2 << 30', 'SECONDS = 180',
                     "'--memory-swap'", "'--network', 'none'", "'sh', 'container_entrypoint.sh'",
                     'frame_digest(frame) == before', 'verify_output(case, target)',
                     "enumerate(('ABC', 'BCA', 'CAB'), 1)", 'def observation_summary(groups):'),
        'engines/geopython-workflow/test_topology_capacity.py': ('test_topology_capacity_native_oracle_rejects_real_saved_corruption',
                     'test_topology_capacity_native_observer_modes_preserve_exact_result',),
    }
    errors = []
    for name, fragments in required.items():
        path = repository / name
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            errors.append(f'{name}: manual line topology capacity registration is incomplete')
    workflow = repository / '.github/workflows/vector-topology-capacity.yml'
    if workflow.is_file() and re.search(r'(?m)^  (?:push|pull_request|schedule):', workflow.read_text()):
        errors.append('line topology capacity must be manual only')
    make = (repository / 'Makefile').read_text() if (repository / 'Makefile').is_file() else ''
    if 'test-vector-topology-capacity' in (make_dependencies(make, 'test') or []):
        errors.append('large line capacity must not run in the ordinary test gate')
    return errors


def validate_registration(repository: Path) -> list[str]:
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    jobs = workflow_jobs(repository)
    root_dependencies = make_dependencies(makefile, "test") or []
    errors = validate_topology_capacity(repository) if (repository / 'engines/geopython-workflow/requirements.txt').is_file() else []
    fastapi_environments = []
    for owner, manifest in discover_python_modules(repository):
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
        if (repository / Path(manifest).parent / "openapi.json").is_file():
            owner_setup = next((step for _, step in workflow_steps(target_job) if PYTHON_GATE_ACTION in step), "")
            fastapi_environments.append((manifest, owner_setup))
        selector_owner = manifest.split("/", 1)[0]
        if not re.search(
            rf"{re.escape(MODULE_GATE_SELECTOR)}\s+--module\s+['\"]?{re.escape(selector_owner)}['\"]?",
            target_job,
        ):
            errors.append(f"{manifest}: shared module change selector is missing")

    if fastapi_environments:
        coverage = re.search(r"(?m)^test-swagger:[^\n]*\n(?P<recipe>(?:\t[^\n]*\n)*)", makefile)
        authorization = re.search(r"(?m)^test-authorization:[^\n]*\n(?P<recipe>(?:\t[^\n]*\n)*)", makefile)
        if (
            not coverage or not authorization
            or "python3 scripts/test/swagger-route-coverage_test.py" not in coverage.group("recipe")
            or "\t@bash scripts/swagger/check-route-coverage.sh all" not in coverage.group("recipe").splitlines()
            or "$(MAKE) test-swagger" not in authorization.group("recipe")
            or not (repository / "scripts/test/swagger-route-coverage_test.py").is_file()
        ):
            errors.append("FastAPI Swagger checks must retain their regression and coverage entry in test-authorization")
        platform_jobs = [job for job in jobs if re.search(r"(?m)^\s*(?:-\s*)?run:\s*make test-platform\s*$", job)]
        for manifest, owner_setup in fastapi_environments:
            for job in platform_jobs or [""]:
                gate = re.search(r"(?m)^\s*(?:-\s*)?run:\s*make test-platform\s*$", job)
                if not any(
                    gate and start < gate.start()
                    and PYTHON_GATE_ACTION in step
                    and not re.search(r"(?m)^\s+if:", step)
                    and step_setting(step, "venv-path") == str(Path(manifest).parent / "venv")
                    and step_setting(step, "requirements-file") == manifest
                    and step_setting(step, "python-version") == step_setting(owner_setup, "python-version")
                    for start, step in workflow_steps(job)
                ):
                    errors.append(f"{Path(manifest).parent}/openapi.json: platform Swagger checks require the unconditional owner Python environment before make test-platform")
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
