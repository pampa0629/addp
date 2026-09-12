#!/usr/bin/env python3
"""Verify registration of hosted, hosted-only, and owner-managed T2 gates."""

from __future__ import annotations

import argparse
import importlib.util
import re
import subprocess
import sys
from pathlib import Path


class RegistrationError(RuntimeError):
    pass


def load_module_gate():
    path = Path(__file__).parents[1] / "test" / "module-gate.py"
    spec = importlib.util.spec_from_file_location("addp_t2_module_gate", path)
    if spec is None or spec.loader is None:
        raise RegistrationError(f"cannot load module gate metadata owner: {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


MODULE_GATE = load_module_gate()
HOSTED_ONLY_PATTERN = re.compile(
    r"(?m)^#\s*ADDP_T2_HOSTED_ONLY=(?P<runtime>[a-zA-Z0-9_-]+)\s*$"
)
OWNER_MANAGED_PATTERN = re.compile(
    r"(?m)^#\s*ADDP_T2_OWNER_MANAGED=(?P<runtime>[a-zA-Z0-9_-]+)\s*$"
)
OWNED_SERVICES_PATTERN = re.compile(
    r"(?m)^#\s*ADDP_T2_OWNED_SERVICES=(?P<services>[a-zA-Z0-9_,-]+)\s*$"
)
COMPOSE_FILE_PATTERN = re.compile(
    r"(?m)^#\s*ADDP_T2_COMPOSE_FILE=(?P<path>[^\s]+)\s*$"
)


def gate_requires_explicit_disposable_database(content: str) -> bool:
    return "*disposable*" in content and "*test*" not in content


def discover_hosted_service_gates(
    repository: Path,
) -> list[tuple[str, str, str, tuple[str, ...]]]:
    gates: list[tuple[str, str, str, tuple[str, ...]]] = []
    for script in MODULE_GATE.hosted_t2_scripts(repository):
        content = (repository / script).read_text(encoding="utf-8")
        match = MODULE_GATE.T2_SERVICES_PATTERN.search(content)
        assert match is not None
        name = Path(script).name.removesuffix("-gate.sh")
        owner = name.split("-", 1)[0]
        services = tuple(match.group("services").split(","))
        gates.append((script, f"test-{name}", owner, services))
    if not gates:
        raise RegistrationError(
            "no scripts/test/*-gate.sh files declare ADDP_T2_SERVICES"
        )
    return sorted(gates)


def discover_hosted_only_gates(
    repository: Path,
) -> list[tuple[str, str, str, str]]:
    gates: list[tuple[str, str, str, str]] = []
    for path in sorted((repository / "scripts/test").glob("*-gate.sh")):
        content = path.read_text(encoding="utf-8")
        match = HOSTED_ONLY_PATTERN.search(content)
        if match is None:
            continue
        script = path.relative_to(repository).as_posix()
        name = path.name.removesuffix("-gate.sh")
        owner = name.split("-", 1)[0]
        gates.append((script, f"test-{name}", owner, match.group("runtime")))
    return gates


def discover_owner_managed_gates(
    repository: Path,
) -> list[tuple[str, str, str, str]]:
    gates: list[tuple[str, str, str, str]] = []
    for path in sorted((repository / "scripts/test").glob("*-gate.sh")):
        content = path.read_text(encoding="utf-8")
        match = OWNER_MANAGED_PATTERN.search(content)
        if match is None:
            continue
        script = path.relative_to(repository).as_posix()
        name = path.name.removesuffix("-gate.sh")
        owner = name.split("-", 1)[0]
        gates.append((script, f"test-{name}", owner, match.group("runtime")))
    return gates


def discover_owned_service_gates(
    repository: Path,
) -> list[tuple[str, str, str, tuple[str, ...], str]]:
    gates: list[tuple[str, str, str, tuple[str, ...], str]] = []
    for path in sorted((repository / "scripts/test").glob("*-gate.sh")):
        content = path.read_text(encoding="utf-8")
        services_match = OWNED_SERVICES_PATTERN.search(content)
        if services_match is None:
            continue
        compose_match = COMPOSE_FILE_PATTERN.search(content)
        if compose_match is None:
            raise RegistrationError(f"{path}: ADDP_T2_COMPOSE_FILE is missing")
        script = path.relative_to(repository).as_posix()
        name = path.name.removesuffix("-gate.sh")
        owner = name.split("-", 1)[0]
        services = tuple(services_match.group("services").split(","))
        gates.append((script, f"test-{name}", owner, services, compose_match.group("path")))
    return gates


def workflow_service_block(job: str, service: str) -> str | None:
    services_match = re.search(
        r"(?ms)^    services:\s*\n(?P<body>.*?)(?=^    [a-zA-Z0-9_-]+:\s*$|\Z)",
        job,
    )
    if services_match is None:
        return None
    service_match = re.search(
        rf"(?ms)^      {re.escape(service)}:\s*\n(?P<body>.*?)(?=^      [a-zA-Z0-9_-]+:\s*$|\Z)",
        services_match.group("body"),
    )
    return service_match.group("body") if service_match else None


def workflow_job_environment_block(job: str) -> str | None:
    match = re.search(
        r"(?ms)^    env:\s*\n(?P<body>.*?)(?=^    [a-zA-Z0-9_-]+:\s*$|\Z)",
        job,
    )
    return match.group("body") if match else None


def workflow_step_environment_block(step: str) -> str | None:
    match = re.search(
        r"(?ms)^        env:\s*\n(?P<body>.*?)(?=^        [a-zA-Z0-9_-]+:\s*$|\Z)",
        step,
    )
    return match.group("body") if match else None


def environment_scope_provides(scope: str | None, name: str) -> bool:
    return scope is not None and re.search(
        rf"(?m)^\s*{re.escape(name)}:\s*\S.*$", scope
    ) is not None


def service_image_is_pinned(service_block: str) -> bool:
    return re.search(
        r"(?m)^\s*image:\s*\S+:[^@\s]+@sha256:[0-9a-f]{64}\s*$",
        service_block,
    ) is not None


def compose_service_block(compose: str, service: str) -> str | None:
    match = re.search(
        rf"(?ms)^  {re.escape(service)}:\s*\n(?P<body>.*?)(?=^  [a-zA-Z0-9_-]+:\s*$|\Z)",
        compose,
    )
    return match.group("body") if match else None


def service_has_required_nofile_limit(service_block: str) -> bool:
    soft = re.search(r"(?m)^\s*soft:\s*(\d+)\s*$", service_block)
    hard = re.search(r"(?m)^\s*hard:\s*(\d+)\s*$", service_block)
    return (
        "nofile:" in service_block
        and soft is not None
        and hard is not None
        and int(soft.group(1)) >= 1_000_000
        and int(hard.group(1)) >= 1_000_000
    )


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
    hosted_integration_recipe = make_recipe(makefile, "test-integration-hosted")
    owner_managed_recipe = make_recipe(makefile, "test-integration-owner-managed")

    if integration_recipe is None:
        errors.append("Makefile target test-integration is missing")

    for script, target, owner, services in discover_hosted_service_gates(repository):
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
        else:
            script_content = (repository / script).read_text(encoding="utf-8")
            target_step = next(
                (
                    step
                    for step in steps
                    if re.search(
                        rf"(?m)^\s*(?:-\s*)?run:\s*make\s+{re.escape(target)}\s*$",
                        step,
                    )
                ),
                None,
            )
            job_environment = workflow_job_environment_block(target_job)
            step_environment = (
                workflow_step_environment_block(target_step) if target_step else None
            )
            for alternatives in MODULE_GATE.required_t2_environment(script_content):
                if target_step is None or not any(
                    environment_scope_provides(step_environment, name)
                    or environment_scope_provides(job_environment, name)
                    for name in alternatives
                ):
                    requirement = " or ".join(alternatives)
                    errors.append(
                        f"{script}: GitHub Actions target {target} does not provide "
                        f"{requirement}"
                    )
            for service in services:
                service_block = workflow_service_block(target_job, service)
                if service_block is None:
                    errors.append(
                        f"{script}: declared {service} service is missing in {target} job"
                    )
                elif not service_image_is_pinned(service_block):
                    errors.append(
                        f"{script}: {service} service image must pin an explicit tag and digest "
                        f"in {target} job"
                    )
            if "postgres" in services and gate_requires_explicit_disposable_database(
                script_content
            ):
                database_match = re.search(
                    r"(?m)^\s*POSTGRES_DB:\s*([A-Za-z0-9_-]+)\s*$", target_job
                )
                if database_match is None:
                    errors.append(
                        f"{script}: hosted PostgreSQL database is not declared"
                    )
                else:
                    database = database_match.group(1)
                    if "disposable" not in database:
                        errors.append(
                            f"{script}: hosted PostgreSQL database {database} is not "
                            "explicitly disposable"
                        )
                    else:
                        health_database = re.search(
                            rf"pg_isready[^\n]*\s-d\s+{re.escape(database)}(?:\s|[\"'])",
                            target_job,
                        )
                        dsn_database = re.search(
                            rf"postgres(?:ql)?://[^\s]+/{re.escape(database)}\?",
                            target_job,
                        )
                        if health_database is None or dsn_database is None:
                            errors.append(
                                f"{script}: hosted PostgreSQL database {database} is not "
                                "used consistently by its health check and gate DSN"
                            )

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

    for script, target, owner, services, compose_path in discover_owned_service_gates(repository):
        recipe = make_recipe(makefile, target)
        if recipe is None:
            errors.append(f"{script}: Makefile target {target} is missing")
        elif script not in recipe:
            errors.append(f"{script}: Makefile target {target} does not invoke its owner script")
        if integration_recipe is not None and not re.search(
            rf"(?m)^\t@?\$\(MAKE\)\s+{re.escape(target)}\s*$",
            integration_recipe,
        ):
            errors.append(f"{script}: root test-integration does not invoke {target} sequentially")
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
        compose_file = repository / compose_path
        if not compose_file.is_file():
            errors.append(f"{script}: owned-service Compose file {compose_path} is missing")
        else:
            compose = compose_file.read_text(encoding="utf-8")
            for service in services:
                service_block = compose_service_block(compose, service)
                if service_block is None:
                    errors.append(f"{script}: declared {service} is missing from {compose_path}")
                elif not service_image_is_pinned(service_block):
                    errors.append(
                        f"{script}: {service} image must pin an explicit tag and digest in {compose_path}"
                    )
                elif service == "tidb-tikv" and not service_has_required_nofile_limit(
                    service_block
                ):
                    errors.append(
                        f"{script}: tidb-tikv must set nofile soft/hard limits to at least 1000000 in {compose_path}"
                    )
        script_content = (repository / script).read_text(encoding="utf-8")
        if "docker compose" not in script_content or "down --volumes --remove-orphans" not in script_content or "disposable" not in script_content:
            errors.append(f"{script}: owned-service gate must own disposable Compose startup and cleanup")
        selection_step = next(
            (
                step
                for step in steps
                if re.search(rf"(?m)^\s*id:\s*{re.escape(owner)}\s*$", step)
            ),
            None,
        )
        if selection_step is None or not re.search(
            rf"python3\s+scripts/ci/select-module-gate\.py\s+--module\s+['\"]?{re.escape(owner)}['\"]?",
            selection_step or "",
        ):
            errors.append(f"{script}: shared module change selector is missing")

    for script, target, owner, runtime in discover_hosted_only_gates(repository):
        recipe = make_recipe(makefile, target)
        if recipe is None:
            errors.append(f"{script}: Makefile target {target} is missing")
        elif script not in recipe:
            errors.append(f"{script}: Makefile target {target} does not invoke its owner script")
        if hosted_integration_recipe is None:
            errors.append("Makefile target test-integration-hosted is missing")
        elif not re.search(
            rf"(?m)^\t@?\$\(MAKE\)\s+{re.escape(target)}\s*$",
            hosted_integration_recipe,
        ):
            errors.append(
                f"{script}: root test-integration-hosted does not invoke {target} sequentially"
            )
        if integration_recipe is not None and re.search(
            rf"(?m)^\t@?\$\(MAKE\)\s+{re.escape(target)}\s*$",
            integration_recipe,
        ):
            errors.append(
                f"{script}: hosted-only target {target} must not run in local test-integration"
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
        script_content = (repository / script).read_text(encoding="utf-8")
        if "disposable" not in script_content or "docker run" not in script_content or "docker rm" not in script_content:
            errors.append(
                f"{script}: hosted-only {runtime} gate must own a disposable Docker lifecycle"
            )
        selection_step = next(
            (
                step
                for step in steps
                if re.search(rf"(?m)^\s*id:\s*{re.escape(owner)}\s*$", step)
            ),
            None,
        )
        if selection_step is None or not re.search(
            rf"python3\s+scripts/ci/select-module-gate\.py\s+--module\s+['\"]?{re.escape(owner)}['\"]?",
            selection_step or "",
        ):
            errors.append(f"{script}: shared module change selector is missing")

    for script, target, owner, runtime in discover_owner_managed_gates(repository):
        recipe = make_recipe(makefile, target)
        if recipe is None:
            errors.append(f"{script}: Makefile target {target} is missing")
        elif script not in recipe:
            errors.append(f"{script}: Makefile target {target} does not invoke its owner script")
        if owner_managed_recipe is None:
            errors.append("Makefile target test-integration-owner-managed is missing")
        elif not re.search(
            rf"(?m)^\t@?\$\(MAKE\)\s+{re.escape(target)}\s*$",
            owner_managed_recipe,
        ):
            errors.append(
                f"{script}: root test-integration-owner-managed does not invoke {target} sequentially"
            )
        for aggregate, aggregate_recipe in (
            ("test-integration", integration_recipe),
            ("test-integration-hosted", hosted_integration_recipe),
        ):
            if aggregate_recipe is not None and re.search(
                rf"(?m)^\t@?\$\(MAKE\)\s+{re.escape(target)}\s*$",
                aggregate_recipe,
            ):
                errors.append(
                    f"{script}: owner-managed target {target} must not run in {aggregate}"
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
        else:
            required_job_fragments = (
                "github.event_name == 'workflow_dispatch'",
                "self-hosted",
                "Linux",
                "X64",
                f"environment: addp-{runtime}",
                f"ADDP_{runtime.upper().replace('-', '_')}_GATE_ENV_FILE:",
                "actions/upload-artifact@",
                "./.github/actions/ci-gate-summary",
            )
            for fragment in required_job_fragments:
                if fragment not in target_job:
                    errors.append(
                        f"{script}: owner-managed {runtime} job is missing {fragment}"
                    )
        script_content = (repository / script).read_text(encoding="utf-8")
        required_script_fragments = (
            "disposable",
            "docker create",
            "docker rm",
            "LICENSE_SHA256",
            "MEDIA_SHA256",
        )
        if any(fragment not in script_content for fragment in required_script_fragments):
            errors.append(
                f"{script}: owner-managed {runtime} gate must own licensed disposable Docker lifecycle and SHA evidence"
            )
        selection_step = next(
            (
                step
                for step in steps
                if re.search(rf"(?m)^\s*id:\s*{re.escape(owner)}\s*$", step)
            ),
            None,
        )
        if selection_step is None or not re.search(
            rf"python3\s+scripts/ci/select-module-gate\.py\s+--module\s+['\"]?{re.escape(owner)}['\"]?",
            selection_step or "",
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
        hosted_service_count = len(discover_hosted_service_gates(repository))
        owned_service_count = len(discover_owned_service_gates(repository))
        hosted_only_count = len(discover_hosted_only_gates(repository))
        owner_managed_count = len(discover_owner_managed_gates(repository))
    except (RegistrationError, subprocess.CalledProcessError) as error:
        print(f"T2 CI registration check failed: {error}", file=sys.stderr)
        return 1
    if errors:
        for error in errors:
            print(f"T2 CI registration check failed: {error}", file=sys.stderr)
        return 1
    print(
        "T2 CI registration check passed: "
        f"{hosted_service_count} hosted-service gates and "
        f"{owned_service_count} gate-owned Compose service gates and "
        f"{hosted_only_count} hosted-only gates and "
        f"{owner_managed_count} owner-managed gates are registered."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
