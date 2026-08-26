#!/usr/bin/env python3
"""Enforce ADDP runtime startup isolation and module registration lifecycle rules."""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path


RUNTIME_FLAGS = {
    "START_PYTHON_WORKFLOW",
    "START_MATH_WORKFLOW",
    "START_MODEL3D_WORKFLOW",
    "START_POINTCLOUD_WORKFLOW",
    "START_SUPERMAP_WORKFLOW",
    "START_SPARK_WORKFLOW",
    "START_JUPYTER",
    "START_DUCKDB",
    "START_INFERENCE_BACKEND",
    "START_INFERENCE_FRONTEND",
}

EXPLICIT_RUNTIME_CASES = {
    "geopython-workflow",
    "math-workflow",
    "model3d-workflow",
    "pointcloud-workflow",
    "supermap-workflow",
    "spark-workflow",
    "jupyter",
    "duckdb",
    "inference",
}

RUNTIME_COMPOSE_SERVICES = {
    "duckdb-engine",
    "jupyter-engine",
    "geopython-workflow-engine",
    "math-workflow-engine",
    "model3d-workflow-engine",
    "pointcloud-workflow-engine",
    "supermap-workflow-engine",
    "spark-workflow-engine",
}

REMOVED_SYSTEM_RUNTIME_CONFIG = {
    "DUCKDB_RUNTIME_URL",
    "INFERENCE_URL",
}

MODULE_REGISTRATION_CALL = re.compile(
    r"(?m)^\s*(?P<lifecycle>[A-Za-z][A-Za-z0-9_]*)\s*(?::=|=)\s*.*RegisterAndHeartbeat(?:WithMetadata)?\("
)


def selected_module_cases(start_script: str) -> dict[str, str]:
    match = re.search(r"(?ms)^\s*case \$SELECTED_MODULE in\n(?P<body>.*?)^\s*esac\s*$", start_script)
    if not match:
        raise ValueError("scripts/dev/start.sh selected-module case block is missing")
    body = match.group("body")
    cases: dict[str, str] = {}
    for case in re.finditer(
        r"(?ms)^\s{4}(?P<name>[a-z0-9-]+)\)\n(?P<body>.*?)(?=^\s{4}[a-z0-9-]+\)|\Z)",
        body,
    ):
        cases[case.group("name")] = case.group("body")
    if not cases:
        raise ValueError("scripts/dev/start.sh selected-module cases cannot be parsed")
    return cases


def compose_service_blocks(compose: str) -> dict[str, str]:
    matches = list(re.finditer(r"(?m)^  (?P<name>[a-z0-9-]+):\s*$", compose))
    blocks: dict[str, str] = {}
    for index, match in enumerate(matches):
        end = matches[index + 1].start() if index + 1 < len(matches) else len(compose)
        blocks[match.group("name")] = compose[match.end() : end]
    return blocks


def compose_dependencies(block: str) -> set[str]:
    match = re.search(r"(?ms)^    depends_on:\s*\n(?P<body>.*?)(?=^    [a-zA-Z0-9_-]+:|\Z)", block)
    if not match:
        return set()
    return set(re.findall(r"(?m)^      -?\s*([a-z0-9-]+):?\s*$", match.group("body")))


def validate_module_registration_lifecycle(repository: Path) -> list[str]:
    errors: list[str] = []
    call_count = 0
    command_sources = sorted(
        path
        for path in repository.glob("*/backend/cmd/**/*.go")
        if not path.name.endswith("_test.go")
    )
    for path in command_sources:
        source = path.read_text(encoding="utf-8")
        if "RegisterAndHeartbeat(" not in source and "RegisterAndHeartbeatWithMetadata(" not in source:
            continue
        relative = path.relative_to(repository)
        call_count += source.count("RegisterAndHeartbeat(") + source.count("RegisterAndHeartbeatWithMetadata(")
        if (
            "RegisterAndHeartbeat(context.Background()" in source
            or "RegisterAndHeartbeatWithMetadata(context.Background()" in source
        ):
            errors.append(f"{relative} uses context.Background() for module registration")
        if "signal.NotifyContext(" not in source:
            errors.append(f"{relative} does not own a signal lifecycle context")
        matches = list(MODULE_REGISTRATION_CALL.finditer(source))
        if not matches:
            errors.append(f"{relative} ignores the registration lifecycle completion signal")
            continue
        for match in matches:
            lifecycle = match.group("lifecycle")
            if not re.search(rf"<-\s*{re.escape(lifecycle)}\.Done\(\)", source):
                errors.append(f"{relative} does not wait for {lifecycle} before exiting")

    if call_count == 0:
        errors.append("no Go module registration callsites were found")

    client_path = repository / "common/client/system_service.go"
    client_source = client_path.read_text(encoding="utf-8")
    if not re.search(
        r"func \(c \*SystemServiceClient\) RegisterAndHeartbeat\([^)]*\) \*ModuleRegistrationLifecycle",
        client_source,
    ):
        errors.append("common/client/system_service.go does not expose lifecycle completion")
    return errors


def validate(repository: Path) -> list[str]:
    errors: list[str] = []
    start_path = repository / "scripts/dev/start.sh"
    compose_path = repository / "docker-compose.yml"
    system_main_path = repository / "system/backend/cmd/server/main.go"
    system_config_path = repository / "system/backend/internal/config/config.go"

    start_script = start_path.read_text(encoding="utf-8")
    try:
        cases = selected_module_cases(start_script)
    except ValueError as error:
        errors.append(str(error))
        cases = {}
    assignment_pattern = re.compile(r"(?m)^\s*(START_[A-Z0-9_]+)=true\s*$")
    for name, body in cases.items():
        if name in EXPLICIT_RUNTIME_CASES:
            continue
        enabled = set(assignment_pattern.findall(body))
        forbidden = sorted(enabled & RUNTIME_FLAGS)
        if forbidden:
            errors.append(f"scripts/dev/start.sh case {name} implicitly starts Engine Runtime flags: {', '.join(forbidden)}")

    compose = compose_path.read_text(encoding="utf-8")
    for service, block in compose_service_blocks(compose).items():
        dependencies = compose_dependencies(block)
        if service in RUNTIME_COMPOSE_SERVICES and "system-backend" in dependencies:
            errors.append(f"docker-compose.yml Engine Runtime {service} depends on System startup order")
        if service in RUNTIME_COMPOSE_SERVICES or service == "inference-frontend":
            continue
        if not (service.endswith("-backend") or service.endswith("-worker")):
            continue
        forbidden = sorted(dependencies & RUNTIME_COMPOSE_SERVICES)
        if forbidden:
            errors.append(f"docker-compose.yml service {service} depends on Engine Runtime services: {', '.join(forbidden)}")

    system_sources = {
        str(system_main_path.relative_to(repository)): system_main_path.read_text(encoding="utf-8"),
        str(system_config_path.relative_to(repository)): system_config_path.read_text(encoding="utf-8"),
        str(compose_path.relative_to(repository)): compose,
        ".env.example": (repository / ".env.example").read_text(encoding="utf-8"),
    }
    for relative_path, source in system_sources.items():
        for key in sorted(REMOVED_SYSTEM_RUNTIME_CONFIG):
            if key in source:
                errors.append(f"{relative_path} still contains removed System runtime config {key}")

    system_main = system_sources[str(system_main_path.relative_to(repository))]
    for forbidden_call in ("RegisterBuiltinRuntime", "RefreshAllEngineCapabilities"):
        if forbidden_call in system_main:
            errors.append(f"system startup still calls {forbidden_call}")
    errors.extend(validate_module_registration_lifecycle(repository))
    return errors


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    return parser.parse_args()


def main() -> int:
    errors = validate(parse_args().repository.resolve())
    if errors:
        for error in errors:
            print(f"Engine startup isolation check failed: {error}", file=sys.stderr)
        return 1
    print("Runtime startup consistency check passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
