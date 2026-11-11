#!/usr/bin/env python3
"""Keep registered Online suites, deployment profiles, and T4 workflow aligned."""

from __future__ import annotations

import argparse
import ast
import fnmatch
from functools import lru_cache
import json
import importlib.util
import re
import shlex
import subprocess
import sys
from pathlib import Path


class RegistrationError(RuntimeError):
    pass


def execution_runner_ci_inputs(repository: Path) -> set[str]:
    """Follow executed Python imports; static source reads are inputs, not programs."""
    # Use the same source inventory as changed-gate: tracked files (including
    # fixtures) plus new, non-ignored files. A running developer's PID/binary
    # files are runtime state, even when a source guard mentions their paths.
    result = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=repository, capture_output=True, check=True,
    )
    source_files = set(result.stdout.decode().split("\0"))
    commands = make_target_commands((repository / "Makefile").read_text(), "test-execution-online-ci-runner")
    if not commands:
        raise RegistrationError("Execution runner canonical Make target is missing")
    inputs = {"Makefile", ".node-version", ".github/workflows/platform-ci.yml",
              ".github/actions/ci-gate-summary/action.yml", "scripts/ci/select-gate-by-paths.sh"}
    pending = [(path, path.endswith(".py")) for path in re.findall(r"[\w.-]+(?:/[\w.*-]+)+", commands)]
    pending += [(value.replace(".", "/") + ".py", True) for value in re.findall(r"scripts(?:\.[\w-]+)+", commands)]
    parsed = set()
    while pending:
        relative, execute = pending.pop()
        path = repository / relative
        if not path.is_file():
            continue
        inputs.add(relative)
        if not execute or relative in parsed or path.suffix != ".py":
            continue
        parsed.add(relative)
        for parent in path.relative_to(repository).parents:
            initializer = str(parent / "__init__.py")
            if (repository / initializer).is_file() and initializer not in parsed:
                pending.append((initializer, True))
        text = path.read_text()
        tree = ast.parse(text)
        bindings = {}
        for node in ast.walk(tree):
            if isinstance(node, ast.Assign):
                for target in node.targets:
                    if isinstance(target, ast.Name):
                        bindings.setdefault(target.id, []).append(node.value)
        def strings(node):
            values = set()
            pending_nodes = [node]
            seen_names = set()
            while pending_nodes:
                for item in ast.walk(pending_nodes.pop()):
                    if isinstance(item, ast.Constant) and isinstance(item.value, str):
                        values.add(item.value)
                    elif isinstance(item, ast.Name) and item.id not in seen_names:
                        seen_names.add(item.id)
                        pending_nodes.extend(bindings.get(item.id, []))
            return values
        candidates = [(value, False) for value in re.findall(r"[\w.-]+(?:/[\w.*-]+)+", text)]
        for node in ast.walk(tree):
            if isinstance(node, ast.Constant) and isinstance(node.value, str):
                candidates.append((node.value, False))
            elif isinstance(node, (ast.Import, ast.ImportFrom)):
                modules = [alias.name for alias in node.names] if isinstance(node, ast.Import) else [node.module or "", *((node.module + "." if node.module else "") + alias.name for alias in node.names)]
                for module in modules:
                    candidates.extend(((module.replace(".", "/") + ".py", True), (module.replace(".", "/") + "/__init__.py", True)))
            elif isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and node.func.attr in {"import_module", "spec_from_file_location"}:
                arguments = node.args[:1] if node.func.attr == "import_module" else node.args[1:2]
                for argument in arguments:
                    for value in strings(argument):
                        candidates.append((value.replace(".", "/") + ".py" if node.func.attr == "import_module" and re.fullmatch(r"[\w-]+(?:\.[\w-]+)+", value) else value, True))
            elif isinstance(node, ast.BinOp) and isinstance(node.op, ast.Div):
                parts, current = [], node
                while isinstance(current, ast.BinOp) and isinstance(current.op, ast.Div) and isinstance(current.right, ast.Constant) and isinstance(current.right.value, str):
                    parts.insert(0, current.right.value)
                    current = current.left
                if parts:
                    candidates.append(("/".join(parts), False))
        for candidate, imported in candidates:
            if not re.fullmatch(r"[.\w-]+(?:/[.\w*-]+)*", candidate) or ".." in Path(candidate).parts:
                continue
            # A bare name is only a sibling source file, never a development .env.
            values = [str(path.parent.relative_to(repository) / candidate)]
            if "/" in candidate:
                values.append(candidate)
            for value in values:
                if value in source_files and (repository / value).is_file() and (value not in inputs or imported and value not in parsed):
                    pending.append((value, imported))
    return inputs


def validate_execution_runner_ci(repository: Path) -> None:
    """Keep focused Linux evidence independent while retaining the full platform gate."""
    workflow = (repository / ".github/workflows/platform-ci.yml").read_text()
    matches = re.findall(r"(?ms)^  execution-online-ci-runner:\n(.*?)(?=^  [a-zA-Z0-9_-]+:\n|\Z)", workflow)
    if len(matches) != 1:
        raise RegistrationError("Execution runner requires exactly one independent Linux job")
    job = matches[0]
    if re.findall(r"(?m)^    ([a-z-]+):", job) != ["name", "runs-on", "timeout-minutes", "steps"]:
        raise RegistrationError("Execution runner must have no dependencies, matrix or failure-masking job controls")
    steps = re.findall(r"(?ms)^      - name: [^\n]+\n.*?(?=^      - name:|\Z)", job)
    if len(steps) != 5:
        raise RegistrationError("Execution runner requires only checkout, selection, Node, canonical Make and summary")
    uses = [re.findall(r"(?m)^        uses: (\S+)", step) for step in steps]
    if not (len(uses[0]) == 1 and re.fullmatch(r"actions/checkout@[0-9a-f]{40}", uses[0][0])
            and uses[1] == [] and len(uses[2]) == 1 and re.fullmatch(r"actions/setup-node@[0-9a-f]{40}", uses[2][0])
            and uses[3] == [] and uses[4] == ["./.github/actions/ci-gate-summary"]):
        raise RegistrationError("Execution runner must use only the existing pinned checkout, Node and summary actions")
    conditions = [re.findall(r"(?m)^        if: (.+)$", step) for step in steps]
    if conditions != [[], [], ["steps.selection.outputs.run == 'true'"], ["steps.selection.outputs.run == 'true'"], ["always()"]]:
        raise RegistrationError("Execution runner selection must execute before guarded setup/Make and truthful summary")
    if re.findall(r"(?m)^          (ADDP_CI_[A-Z_]+):", steps[1]) != ["ADDP_CI_BEFORE", "ADDP_CI_EVENT", "ADDP_CI_HEAD", "ADDP_CI_PR_BASE"]:
        raise RegistrationError("Execution runner must preserve the event commit range without forcing selection")
    if re.findall(r"(?m)^          ([a-z-]+):", steps[2]) != ["node-version-file"]:
        raise RegistrationError("Execution runner Node version must come only from .node-version")
    if re.search(r"(?m)^    (?:needs|if|concurrency|continue-on-error|services|container|environment):", job) or "continue-on-error:" in job:
        raise RegistrationError("Execution runner must run independently without masking failures or services")
    required = ("    name: Execution online CI runner\n", "    runs-on: ubuntu-24.04\n", "    timeout-minutes: 10\n",
                "          fetch-depth: 0\n", "          persist-credentials: false\n", "          node-version-file: .node-version\n",
                "          ADDP_CI_BEFORE: ${{ github.event.before }}\n", "          ADDP_CI_EVENT: ${{ github.event_name }}\n",
                "          ADDP_CI_HEAD: ${{ github.sha }}\n", "          ADDP_CI_PR_BASE: ${{ github.event.pull_request.base.sha }}\n",
                "        uses: ./.github/actions/ci-gate-summary\n", "          gate: Execution online CI runner\n",
                "          selected: ${{ steps.selection.outputs.run }}\n", "          status: ${{ job.status }}\n")
    if any(not source_contract_contains(job, fragment, repository) for fragment in required):
        raise RegistrationError("Execution runner runtime, selection environment or truthful summary is incomplete")
    runs = re.findall(r"(?m)^        run: \|\n((?:          .*\n)+)", job)
    if len(runs) != 2 or len(re.findall(r"(?m)^        run:", job)) != 2:
        raise RegistrationError("Execution runner must only select paths and invoke its canonical Make target")
    selection = shlex.split(runs[0].replace("\\\n", ""))
    if selection[:3] != ["bash", "scripts/ci/select-gate-by-paths.sh", "Execution online CI runner"]:
        raise RegistrationError("Execution runner must reuse the shared path selector")
    patterns = selection[3:]
    if not patterns or len(patterns) != len(set(patterns)) or "*" in patterns:
        raise RegistrationError("Execution runner selection must declare distinct bounded input patterns")
    missing = [path for path in execution_runner_ci_inputs(repository) if not any(fnmatch.fnmatchcase(path, pattern) for pattern in patterns)]
    if missing:
        raise RegistrationError("Execution runner selection misses source inputs: " + ", ".join(sorted(missing)))
    if [line.strip() for line in runs[1].splitlines()] != ["make check-node-runtime", "make test-execution-online-ci-runner"]:
        raise RegistrationError("Execution runner must invoke the canonical Make target without masking its exit")


@lru_cache(maxsize=16)
def make_target_definitions(source: str) -> tuple[tuple[str, str, str, str], ...]:
    """Cache immutable parsing by complete source bytes, never by path or mtime."""
    return tuple((match.group(1), match.group(2), match.group(3), match.group(0))
                 for match in re.finditer(r"(?ms)^([a-zA-Z0-9_.-]+):([^\n]*)\n(.*?)(?=^[^\t\n#]|\Z)", source))


@lru_cache(maxsize=16)
def required_test_definitions(source: str) -> frozenset[str]:
    classes = [node for node in ast.parse(source).body if isinstance(node, ast.ClassDef)]
    collected = set()
    while True:
        found = {node.name for node in classes if node.name not in collected and any(
            (isinstance(base, ast.Attribute) and isinstance(base.value, ast.Name)
             and base.value.id == "unittest" and base.attr == "TestCase")
            or (isinstance(base, ast.Name) and base.id in collected) for base in node.bases)}
        if not found:
            break
        collected.update(found)
    return frozenset(method.name for node in classes if node.name in collected
                     for method in node.body if isinstance(method, ast.FunctionDef) and method.name.startswith("test"))


def make_target_commands(makefile: str, target: str, visited: set[str] | None = None) -> str:
    """Resolve the same focused Make owners invoked by the Online aggregate."""
    if visited is None:
        visited = set()
    if target in visited:
        return ""
    visited.add(target)
    match = next((entry for entry in make_target_definitions(makefile) if entry[0] == target), None)
    if match is None:
        return ""
    _, dependencies, body, definition = match
    children = re.findall(r"\btest-[a-z0-9-]+\b", dependencies.split("#", 1)[0])
    children += re.findall(r"\$\(MAKE\)\s+(test-[a-z0-9-]+)", body)
    return definition + "".join(make_target_commands(makefile, child, visited) for child in children)



def source_contract_contains(source: str, fragment: str, repository: Path | None = None) -> bool:
    """Make owner calls assert dependency reachability, independent of Make syntax."""
    owner = re.fullmatch(r"\s*@?\$\(MAKE\)\s+(test-[a-z0-9-]+)\s*", fragment)
    if owner is None:
        if (repository is not None and re.fullmatch(r"test_[a-zA-Z0-9_]+", fragment)
                and re.search(r"(?m)^test-[a-z0-9-]+:", source)):
            first = re.search(r"(?m)^(test-[a-z0-9-]+):", source).group(1)
            root = "test-online-runner" if re.search(r"(?m)^test-online-runner:", source) else first
            closure = make_target_commands(source, root)
            selector = "scripts/ci/check-online-ci-registration_test.py"
            tests = repository / selector
            if selector in closure and tests.is_file():
                return fragment in required_test_definitions(tests.read_text())
        return fragment in source
    target = owner.group(1)
    definitions = make_target_definitions(source)
    if not any(match[0] == target for match in definitions):
        return False
    for match in definitions:
        # Make special targets declare attributes, not executable owner edges.
        if match[0].startswith('.'):
            continue
        dependencies = re.findall(r"\btest-[a-z0-9-]+\b", match[1].split("#", 1)[0])
        dependencies += re.findall(r"\$\(MAKE\)\s+(test-[a-z0-9-]+)", match[2])
        if target in dependencies:
            return True
    return False


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


def load_suite_registry(repository: Path) -> dict[str, object]:
    path = repository / "scripts/test/online-gate.py"
    spec = importlib.util.spec_from_file_location("addp_online_gate_registration", path)
    if spec is None or spec.loader is None:
        raise RegistrationError(f"cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    try:
        spec.loader.exec_module(module)
    except (OSError, SyntaxError) as error:
        raise RegistrationError("cannot load Online suite registry") from error
    return dict(module.SUITES)


def load_registered_suites(registry: dict[str, object]) -> set[str]:
    return set(registry)


def load_nightly_suites(registry: dict[str, object]) -> set[str]:
    nightly: set[str] = set()
    for name, suite in registry.items():
        enabled = getattr(suite, "nightly", False)
        if not isinstance(enabled, bool):
            raise RegistrationError(
                f"Online suite {name} nightly registration must be boolean"
            )
        if enabled:
            nightly.add(name)
    return nightly


def load_deployment_profiles(repository: Path) -> dict[str, str]:
    path = repository / "scripts/test/online-host-gate.sh"
    if not path.is_file():
        raise RegistrationError(f"{path.relative_to(repository)} is missing")
    text = path.read_text(encoding="utf-8")
    required_fragments = (
        "python3 scripts/test/online-preflight.py --environment-only",
        'run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        "printf 'database=%s\\n' \"$POSTGRES_DB\"",
    )
    missing = [fragment for fragment in required_fragments if not source_contract_contains(text, fragment, repository)]
    if missing:
        raise RegistrationError(
            "Online host gate is missing: " + ", ".join(missing)
        )
    matches = re.findall(
        r"(?m)^  ([a-z][a-z0-9-]*)\)\n    START_TARGET=(-[a-z][a-z0-9-]*)$",
        text,
    )
    profiles = dict(matches)
    if len(profiles) != len(matches):
        raise RegistrationError("online deployment profiles contain duplicate suites")
    external_paths = sorted(
        (repository / "scripts/test").glob("online-hosted-*-gate.sh")
    ) + sorted(
        (repository / "scripts/test").glob("online-owner-managed-*-gate.sh")
    )
    for external_path in external_paths:
        external_text = external_path.read_text(encoding="utf-8")
        external_suites = re.findall(
            r"(?m)^# ADDP_ONLINE_SUITES=([a-z][a-z0-9-]*)$", external_text
        )
        external_runner = re.search(
            r"(?m)^# ADDP_ONLINE_RUNNER=([a-z][a-z0-9_-]*)$", external_text
        )
        if not external_suites or external_runner is None:
            profile_kind = (
                "Hosted"
                if external_path.name.startswith("online-hosted-")
                else "owner-managed"
            )
            raise RegistrationError(
                f"{external_path.relative_to(repository)} is missing {profile_kind} Online metadata"
            )
        for suite in external_suites:
            if suite in profiles:
                raise RegistrationError(f"Online suite {suite} has multiple deployment profiles")
            profiles[suite] = external_runner.group(1)
    return profiles


def validate_workflow_runtime_health_profile(repository: Path, registered: set[str]) -> None:
    if "workflow-runtime-health-recovery" not in registered:
        return
    host = (repository / "scripts/test/online-host-gate.sh").read_text()
    if "workflow-runtime-health-recovery" in host:
        raise RegistrationError("Runtime health must not retain a self-hosted profile")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  workflow-health-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
            or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'workflow-runtime-health-recovery'" not in job.group("body")
            or "inputs.suite != 'workflow-runtime-health-recovery'" not in workflow):
        raise RegistrationError("Runtime health requires one manual Hosted Linux job")
    for filename in ("workflow-runtime-health-recovery-online.py", "workflow-runtime-health-recovery-online_test.py",
                     "workflow-runtime-health-tool.py", "online-hosted-workflow-health-gate.sh", "online-hosted-workflow-health-gate_test.py"):
        if not (repository / "scripts/test" / filename).is_file():
            raise RegistrationError(f"Runtime health requires {filename}")
    makefile = (repository / "Makefile").read_text()
    if not source_contract_contains(make_target_commands(makefile, "test-online-runner"), "$(MAKE) test-workflow-runtime-health-online-runner", repository):
        raise RegistrationError("Runtime health regression must belong to test-online-runner")
    commands = make_target_commands(makefile, "test-workflow-runtime-health-online-runner")
    for fragment in ("workflow-runtime-health-recovery-online_test.py", "online-hosted-workflow-health-gate_test.py", "WorkflowRuntimeHealth"):
        if not source_contract_contains(commands, fragment, repository):
            raise RegistrationError(f"Runtime health regression missing {fragment}")
    gate = (repository / "scripts/test/online-hosted-workflow-health-gate.sh").read_text()
    for fragment in ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
                     'scripts/dev/start.sh --exact-process "$target"', 'for target in -system -gateway',
                     '--suite workflow-runtime-health-recovery', '--services model3d-workflow-engine,pointcloud-workflow-engine,document-workflow-engine',
                     'workflow-runtime-health-recovery-online.py --restore-only', 'docker rm -fv', 'runtime_cleanup=',
                     'workflow-runtime-health-recovery-online.py --assert-empty', 'image-missing',
                     'verify_online_product_identity', 'startup-missing'):
        if not source_contract_contains(gate, fragment, repository):
            raise RegistrationError(f"Runtime health Hosted lifecycle missing {fragment}")
    scenario = (repository / "scripts/test/workflow-runtime-health-recovery-online.py").read_text()
    tests = (repository / "scripts/test/workflow-runtime-health-recovery-online_test.py").read_text()
    if '"startup_cycles": startup_cycles' not in scenario or "run_startup_recovery(observer)" not in scenario:
        raise RegistrationError("Runtime health must run and report initial unhealthy registration")
    if "test_startup_recovery_uses_only_automatic_registration_and_periodic_observation" not in tests:
        raise RegistrationError("Runtime health startup registration regression is missing")


def validate_module_registry_process_profile(repository: Path, registered: set[str]) -> None:
    if "module-registry-recovery" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "bash scripts/dev/start.sh --exact-process --wait-live -manager",
        "observe_module_lifecycle business-before-system",
        "bash scripts/dev/start.sh --exact-process -system",
        "observe_module_lifecycle manager-registered",
        "bash scripts/dev/start.sh --exact-process -gateway",
        "observe_module_lifecycle gateway-established",
        "bash scripts/dev/stop-exact-process.sh -system",
        "observe_module_lifecycle system-interrupted",
        "observe_module_lifecycle system-recovered",
        "bash scripts/dev/stop-exact-process.sh --force -manager",
        "observe_module_lifecycle manager-abnormally-stopped",
        "observe_module_lifecycle manager-restarted",
        "observe_module_lifecycle manager-gracefully-stopped",
        "ADDP_ONLINE_TEST_PLATFORM_ACCESS_TOKEN",
        "ADDP_HOST_NODE_IPS",
        "bash scripts/dev/stop-exact-process.sh -manager",
    )
    missing = [fragment for fragment in required_fragments if not source_contract_contains(host_gate, fragment, repository)]
    if missing:
        raise RegistrationError(
            "module-registry-recovery process profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "scripts/dev/stop-exact-process.sh",
        "scripts/test/module-lifecycle-process-online.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"module-registry-recovery process profile requires {relative}")
    start_script = (repository / "scripts/dev/start.sh").read_text(encoding="utf-8")
    for fragment in ("--exact-process", "ADDP_ONLINE_HOST", "--wait-live"):
        if not source_contract_contains(start_script, fragment, repository):
            raise RegistrationError(
                f"module-registry-recovery exact start contract is missing {fragment}"
            )


def validate_consumer_engine_recovery_profile(repository: Path, registered: set[str]) -> None:
    if "consumer-engine-recovery" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "bash business/scripts/online-engine-fixture.sh start",
        "bash business/scripts/online-engine-fixture.sh stop",
        "bash scripts/dev/start.sh",
        "playwright install chromium",
        "consumer-process-stability-online.py",
        "consumer-engine-recovery-online.py --restore-only",
    )
    missing = [fragment for fragment in required_fragments if not source_contract_contains(host_gate, fragment, repository)]
    if missing:
        raise RegistrationError(
            "consumer-engine-recovery process profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-engine-fixture.sh",
        "scripts/test/consumer-engine-recovery-online.py",
        "scripts/test/consumer-process-stability-online.py",
        "console/frontend/playwright.online.config.js",
        "console/frontend/e2e/online/consumer-engine-recovery.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"consumer-engine-recovery requires {relative}")
    fixture = (repository / "business/scripts/online-engine-fixture.sh").read_text(encoding="utf-8")
    for fragment in ("ADDP_ONLINE_HOST", "--env-file /dev/null", "business-postgres"):
        if not source_contract_contains(fixture, fragment, repository):
            raise RegistrationError(
                f"consumer-engine-recovery fixture contract is missing {fragment}"
            )


def validate_enterprise_catalog_publishing_profile(repository: Path, registered: set[str]) -> None:
    if "enterprise-catalog-publishing" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "enterprise-catalog-publishing)",
        "START_TARGET=-all",
        "META_URL CATALOG_URL ASSET_URL PORTAL_URL CONSOLE_URL",
        "ADDP_ONLINE_TEST_USER_USERNAME",
        "ADDP_ONLINE_TEST_USER_PASSWORD",
        "ADDP_ONLINE_TEST_CATALOG_DOMAIN_ID",
        "ADDP_ONLINE_TEST_CATALOG_DEPARTMENT_ID",
        "bash business/scripts/online-engine-fixture.sh start",
        'bash scripts/dev/start.sh "$START_TARGET"',
        "playwright install chromium",
    )
    missing = [fragment for fragment in required_fragments if not source_contract_contains(host_gate, fragment, repository)]
    if missing:
        raise RegistrationError(
            "enterprise-catalog-publishing profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-engine-fixture.sh",
        "scripts/test/enterprise-catalog-publishing-online.py",
        "console/frontend/playwright.online.config.js",
        "console/frontend/e2e/online/enterprise-catalog-publishing.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"enterprise-catalog-publishing requires {relative}")
    browser_spec = (
        repository / "console/frontend/e2e/online/enterprise-catalog-publishing.spec.js"
    ).read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_ASSET_CATEGORY_ID",
        "ADDP_ONLINE_ASSET_ID",
        "/portal/categories/",
        "portal_category_assets",
        "data-governance-status",
        "catalog-certify-action",
        "catalog-deprecate-action",
        "catalog-withdraw-curation-action",
        "catalog_lifecycle_actions",
    ):
        if not source_contract_contains(browser_spec, fragment, repository):
            raise RegistrationError(
                f"enterprise-catalog-publishing browser contract is missing {fragment}"
            )
    fixture = (repository / "business/scripts/online-engine-fixture.sh").read_text(encoding="utf-8")
    for fragment in ("addp_online_catalog_fixture", "CREATE TABLE IF NOT EXISTS", "ON CONFLICT"):
        if not source_contract_contains(fixture, fragment, repository):
            raise RegistrationError(
                f"enterprise-catalog-publishing fixture contract is missing {fragment}"
            )


def validate_workbench_service_consumption_profile(repository: Path, registered: set[str]) -> None:
    if "workbench-service-consumption" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "workbench-service-consumption)",
        "START_TARGET=-all",
        "SYSTEM_URL GATEWAY_URL SERVICE_URL WORKBENCH_URL CONSOLE_URL",
        "ADDP_ONLINE_TEST_USER_USERNAME",
        "ADDP_ONLINE_TEST_USER_PASSWORD",
        "ADDP_ONLINE_TEST_TENANT_ADMIN_ACCESS_TOKEN",
        "ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID",
        "bash business/scripts/online-workbench-mysql-fixture.sh start",
        "bash business/scripts/online-workbench-mysql-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
        "playwright install chromium",
    )
    missing = [fragment for fragment in required_fragments if not source_contract_contains(host_gate, fragment, repository)]
    if missing:
        raise RegistrationError(
            "workbench-service-consumption profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-workbench-mysql-fixture.sh",
        "scripts/test/workbench-service-consumption-online.py",
        "console/frontend/playwright.online.config.js",
        "console/frontend/e2e/online/workbench-service-consumption.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"workbench-service-consumption requires {relative}")
    fixture = (repository / "business/scripts/online-workbench-mysql-fixture.sh").read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "business-mysql",
        "REVOKE ALL PRIVILEGES, GRANT OPTION",
        "GRANT SELECT ON",
    ):
        if not source_contract_contains(fixture, fragment, repository):
            raise RegistrationError(
                f"workbench-service-consumption fixture contract is missing {fragment}"
            )


def validate_manager_internal_artifact_lineage_profile(repository: Path, registered: set[str]) -> None:
    if "manager-internal-artifact-lineage" not in registered:
        return
    hosted_path = repository / "scripts/test/online-hosted-manager-gate.sh"
    if not hosted_path.is_file():
        raise RegistrationError("manager-internal-artifact-lineage requires Hosted lifecycle")
    hosted = hosted_path.read_text(encoding="utf-8")
    for fragment in (
        'source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
        'MODEL3D_DOCKER_PLATFORM=linux/amd64',
        'start_online_geopython_runtime',
        'online-raster-minio-fixture.py manager-seed',
        'RUNTIME_CONTAINER=addp-manager-raster-runtime',
        'scripts/build/build-images.sh --verify',
        '-meta -manager -monitor -pointcloud-workflow -document-workflow',
        'start_online_model3d_runtime', 'remove_online_product_container addp-manager-model3d-runtime',
        'start_online_model3d_runtime addp-manager-model3d-runtime', 'addp-model3d-workflow-engine:online',
        'ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID',
        'bash business/scripts/online-manager-minio-fixture.sh start',
        'bash business/scripts/online-manager-minio-fixture.sh stop',
        'bash business/scripts/online-manager-nfs-fixture.sh start',
        'bash business/scripts/online-manager-nfs-fixture.sh stop',
        'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID',
        'go run ./cmd/online-test-fixture --suite manager-internal-artifact-lineage',
        'scripts/test/online-engine-registration.py',
        'playwright install --with-deps chromium',
    ):
        if not source_contract_contains(hosted, fragment, repository):
            raise RegistrationError(f"manager-internal-artifact-lineage Hosted profile is missing {fragment}")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
    job = re.search(r"(?ms)^  manager-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if not job or "runs-on: ubuntu-24.04" not in job.group("body"):
        raise RegistrationError("manager-internal-artifact-lineage requires Ubuntu Hosted job")
    manager_input = re.search(r"(?ms)^      manager_case:\n(?P<body>.*?)(?=^      [a-z_]+:\n|^jobs:|\Z)", workflow)
    if (not manager_input or any(not source_contract_contains(manager_input.group('body'), fragment, repository) for fragment in
            ('required: true', 'type: choice', 'default: full', '- full', '- native-raster-media', '- native-table-orc', '- native-table-avro', '- native-table-feather', '- delimited-read-settings', '- native-netcdf', '- native-grib', '- native-envi', '- kml-export', '- filegdb-export', '- filegdb-mysql-export', '- filegdb-authorization'))
            or any(not source_contract_contains(job.group('body'), fragment, repository) for fragment in (
                'ADDP_ONLINE_MANAGER_CASE: ${{ inputs.manager_case }}',
                'name: Online T4 (manager-internal-artifact-lineage, ${{ inputs.manager_case }}, Linux x86_64)',
                'name: online-t4-manager-internal-artifact-lineage-${{ inputs.manager_case }}-${{ github.run_id }}'))
            or any(not source_contract_contains(hosted, fragment, repository) for fragment in (
                'ADDP_ONLINE_MANAGER_CASE=${ADDP_ONLINE_MANAGER_CASE-full}', 'full|native-raster-media|native-table-orc|native-table-avro|native-table-feather|delimited-read-settings|native-netcdf|native-grib|native-envi|kml-export|filegdb-export|filegdb-mysql-export|filegdb-authorization|nfs-upload-initial)',
                'manager-case.json', 'addp.manager-acceptance-case/v1'))):
        raise RegistrationError('Manager acceptance case requires explicit full/media selection and case-specific evidence')
    netcdf_paths = ('scripts/test/manager-netcdf-online.py', 'scripts/test/manager-netcdf-online_test.py',
                    'business/scripts/online-netcdf-fixture.py', 'console/frontend/e2e/online/manager-netcdf.spec.js')
    if any(not (repository / path).is_file() for path in netcdf_paths):
        raise RegistrationError('NetCDF case is missing its owner, physical oracle, browser or regression')
    netcdf_owner = (repository / netcdf_paths[0]).read_text(encoding='utf-8')
    netcdf_case_owner = (repository / 'scripts/test/manager-internal-artifact-lineage-online.py').read_text(encoding='utf-8')
    netcdf_make = (repository / 'Makefile').read_text(encoding='utf-8')
    if (any(not source_contract_contains(netcdf_owner, fragment, repository) for fragment in ('native-netcdf', 'addp.manager-netcdf/v2', 'manager-netcdf.json', 'source_drift_denied', 'original_download_verified', 'ExactFileMetadataGrants', 'metadata_authorization', 'validate_structural_metadata'))
            or "if case == 'native-netcdf':" not in netcdf_case_owner
            or any(not source_contract_contains(netcdf_make, fragment, repository) for fragment in (netcdf_paths[1], netcdf_paths[3]))):
        raise RegistrationError('NetCDF case must retain explicit dispatch, standard gates, raw values, source version, download and cleanup evidence')
    grib_paths=('scripts/test/manager-grib-online.py','scripts/test/manager-grib-online_test.py',
                'business/scripts/online-grib-fixture.py','console/frontend/e2e/online/manager-grib.spec.js',
                'engines/geopython-workflow/test_online_grib_fixture.py')
    if any(not (repository/path).is_file() for path in grib_paths):
        raise RegistrationError('GRIB case requires owner, byte oracle, browser and native regressions')
    grib_owner=(repository/grib_paths[0]).read_text()
    if ("if case == 'native-grib':" not in netcdf_case_owner
        or any(not source_contract_contains(netcdf_make, path, repository) for path in (grib_paths[1],grib_paths[3],'test_online_grib_fixture.py'))
        or any(value not in grib_owner for value in ('addp.manager-grib/v2','addp.manager-grib-browser/v2','viewport_grid_verified','decoded_native_units','source_drift_denied','original_download_verified','manager-grib.json'))):
        raise RegistrationError('GRIB case must retain decoded units, message windows, version, downloads and cleanup evidence')
    envi_paths=('scripts/test/manager-envi-online.py','scripts/test/manager-envi-online_test.py','business/scripts/online-envi-fixture.py','engines/geopython-workflow/test_online_envi_fixture.py')
    if any(not (repository/path).is_file() for path in envi_paths):
        raise RegistrationError('ENVI requires its exact pair owner, physical oracle and regressions')
    envi_owner=(repository/envi_paths[0]).read_text()
    if ("if case == 'native-envi':" not in netcdf_case_owner
        or not source_contract_contains(netcdf_make, 'scripts/test/manager-envi-online_test.py', repository)
        or not source_contract_contains(netcdf_make, 'test_online_envi_fixture.py', repository)
        or any(value not in envi_owner for value in ('addp.manager-envi/v4','verify_minio_node_scope','minio_node_scope','nonrecursive_future_and_sibling_isolation','natural_expiry','zero_current_source_rules','future_objects_removed','validate_model_bundle','validate_structural_metadata','validate_content_metadata','source_authorization','metadata_details_hidden','current_source_revocation_verified','issuer_permissions=ENVI_INITIALIZER_PERMISSIONS',"scan_client=issuer if storage=='nfs' else client",'initializer_permissions_verified','source_scans','deleted-cog','grant.cleanup()','manager-envi.json'))):
        raise RegistrationError('ENVI requires page/download/current authorization/cache/cleanup through standard gates')
    envi_browser=(repository/'console/frontend/e2e/online/manager-img.spec.js').read_text()
    if any(value not in envi_browser for value in ('enviContentMetadata','metadata_projection_verified','content_metadata_verified')):
        raise RegistrationError('ENVI requires separate public structure and protected browser content metadata')
    if hosted.index('full|native-raster-media|native-table-orc|native-table-avro|native-table-feather|delimited-read-settings|native-netcdf|native-grib|native-envi|kml-export|filegdb-export|filegdb-mysql-export|filegdb-authorization|nfs-upload-initial)') > hosted.index('source "$ROOT_DIR/scripts/utils/hosted-online.sh"'):
        raise RegistrationError('Manager acceptance case must reject invalid input before Hosted lifecycle')
    if "nfs-kernel-server" not in job.group("body"):
        raise RegistrationError("Manager content boundary acceptance requires its Hosted NFS package")
    if "inputs.suite != 'manager-internal-artifact-lineage'" not in workflow:
        raise RegistrationError("manager-internal-artifact-lineage must not also dispatch on self-hosted")
    for relative in (
        "business/scripts/online-manager-minio-fixture.sh",
        "business/scripts/online-manager-nfs-fixture.sh",
        "business/scripts/online-content-boundary-fixture.py",
        "scripts/test/manager-internal-artifact-lineage-online.py",
        "scripts/test/manager-img-online.py",
        "scripts/test/manager-img-online_test.py",
        "scripts/test/manager-kml-export-online.py",
        "scripts/test/manager-kml-export-online_test.py",
        "console/frontend/playwright.online.config.js",
        "console/frontend/e2e/online/manager-internal-artifact-lineage.spec.js",
        "console/frontend/e2e/online/manager-img.spec.js",
        "console/frontend/e2e/online/manager-kml-export.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"manager-internal-artifact-lineage requires {relative}")
    img_owner = (repository / 'scripts/test/manager-img-online.py').read_text(encoding='utf-8')
    img_browser = (repository / 'console/frontend/e2e/online/manager-img.spec.js').read_text(encoding='utf-8')
    manager_owner = (repository / 'scripts/test/manager-internal-artifact-lineage-online.py').read_text(encoding='utf-8')
    for fragment in ('def manager_case(', 'def run_full_case(', 'def run_raster_media_scenes(',
                     'def run_native_raster_media_case(', 'manager-native-raster-media.json',
                     'addp.manager-native-raster-media/v2', 'case = manager_case(os.environ)',
                     'if case == "native-raster-media":'):
        if not source_contract_contains(manager_owner, fragment, repository):
            raise RegistrationError('Manager acceptance case owner must preserve full and separate media routing: ' + fragment)
    make = (repository / 'Makefile').read_text(encoding='utf-8')
    img_runner = re.search(r'(?ms)^test-manager-online-runner:[^\n]*\n(?P<body>(?:\t[^\n]*\n)+)', make)
    if ('nfs-upload-initial' not in manager_input.group('body')
            or "if case == 'nfs-upload-initial':" not in manager_owner
            or 'scripts/test/manager-upload-initial-online_test.py' not in make_target_commands(make, 'test-manager-online-runner')
            or not (repository / 'scripts/test/manager-upload-initial-online.py').is_file()
            or '127.0.0.1(rw,sync' not in (repository / 'business/scripts/online-manager-nfs-fixture.sh').read_text()):
        raise RegistrationError('NFS upload initial authorization requires explicit case, isolated writable fixture and runner tests')
    filegdb = repository / 'scripts/test/manager-filegdb-online.py'
    filegdb_browser = repository / 'console/frontend/e2e/online/manager-filegdb.spec.js'
    if (not filegdb.is_file() or not (repository / 'scripts/test/manager-filegdb-physical.py').is_file()
            or not filegdb_browser.is_file()
            or not img_runner or 'scripts/test/manager-filegdb-online_test.py' not in make_target_commands(make, "test-manager-online-runner")
            or 'console/frontend/e2e/online/manager-filegdb.spec.js' not in make_target_commands(make, "test-manager-online-runner")
            or "if case == 'filegdb-authorization':" not in manager_owner
            or 'manager-filegdb-online' not in manager_owner
            or 'python3 -m scripts.test.manager-filegdb-physical unmount' not in hosted
            or 'src=$nfs_read_mount,dst=$nfs_root,readonly' not in hosted):
        raise RegistrationError('NFS FileGDB requires explicit routing, browser registration, real NFS read mount and bounded cleanup')
    kml_owner = (repository / 'scripts/test/manager-kml-export-online.py').read_text(encoding='utf-8')
    kml_browser = (repository / 'console/frontend/e2e/online/manager-kml-export.spec.js').read_text(encoding='utf-8')
    if ("if case == 'kml-export':" not in manager_owner
            or 'manager-kml-export-online' not in manager_owner
            or not img_runner or 'scripts/test/manager-kml-export-online_test.py' not in make_target_commands(make, "test-manager-online-runner")
            or 'console/frontend/e2e/online/manager-kml-export.spec.js' not in make_target_commands(make, "test-manager-online-runner")
            or any(not source_contract_contains(hosted, fragment, repository) for fragment in ('online-metric-postgres-fixture.sh start', 'online-metric-postgres-fixture.sh stop', '-security -meta -manager -monitor -transfer', '--wait-protection-owners manager,transfer'))
            or any(not source_contract_contains(kml_owner, fragment, repository) for fragment in ('initialize_exact_record_read_grants', 'validate_kml', 'source_snapshot', 'manager-kml-export-browser.json', 'source_unchanged=True'))
            or any(not source_contract_contains(kml_browser, fragment, repository) for fragment in ("name: 'KML'", "waitForEvent('download'", 'actual_button_download: true', 'monitor_verified: true'))):
        raise RegistrationError('Manager KML export requires real button, physical oracle, identity and isolated cleanup regressions')
    filegdb_export = repository / 'scripts/test/manager-filegdb-export-online.py'
    if (not filegdb_export.is_file()
            or "if case in {'filegdb-export', 'filegdb-mysql-export'}:" not in manager_owner
            or 'scripts/test/manager-filegdb-export-online_test.py' not in make_target_commands(make, 'test-manager-online-runner')
            or any(fragment not in filegdb_export.read_text(encoding='utf-8') for fragment in (
                'actual_button_download', 'unpack_archive', 'OpenFileGDB', 'initialize_exact_record_read_grants',
                'same_tenant_non_owner_denied', 'source_unchanged=True'))
            or 'File Geodatabase (.gdb.zip)' not in kml_browser):
        raise RegistrationError('Manager FileGDB export requires actual download, independent readback and source and download authorization checks')
    mysql_fixture = repository / 'business/scripts/online-manager-mysql-fixture.sh'
    if (not mysql_fixture.is_file() or 'filegdb-mysql-export' not in workflow
            or any(not source_contract_contains(hosted, part, repository) for part in (
                'online-manager-mysql-fixture.sh start', 'online-manager-mysql-fixture.sh stop',
                'ADDP_ONLINE_MANAGER_EXPORT_ENGINE_ID'))
            or any(part not in filegdb_export.read_text(encoding='utf-8') for part in (
                "'filegdb-mysql-export'", "'engine_type': 'mysql'", "'term': 'database'", 'source_profile'))):
        raise RegistrationError('Manager MySQL FileGDB requires its explicit case, database scope and owned source lifecycle')
    if ("img_scene.run(repository, environment, sys.modules[__name__], source_format=source_format)" not in manager_owner
            or "for source_format in ('img', 'jpeg2000', 'grd', 'tiff')" not in manager_owner
            or not img_runner or '\t@python3 -m unittest scripts/test/manager-img-online_test.py' not in make_target_commands(make, "test-manager-online-runner")
            or '\t@node --check --input-type=module < console/frontend/e2e/online/manager-img.spec.js' not in make_target_commands(make, "test-manager-online-runner")
            or any(not source_contract_contains(img_owner, fragment, repository) for fragment in ('manager-img.spec.js', 'generate_raster_cog', 'cleanup_managed_artifact', 'source_format', 'JPEG2000_SOURCES', 'GRD_SOURCES', 'grid_facts', 'addp.manager-raster-media/v2'))
            or any(not source_contract_contains(img_browser, fragment, repository) for fragment in ('modelPixels', 'range_loaded', 'reused_result', 'generation_unavailable', 'source_image_absent', 'unsupported_message_visible', 'download_sha256', 'matchesSourcePreviewResponse', 'source_preview_verified', 'addp.manager-raster-media-browser/v4'))):
        raise RegistrationError('Manager IMG Hosted subscene must include real generation, browser, cleanup and standard regressions')
    tiff_physical=(repository/'business/scripts/online-raster-minio-fixture.py').read_text()
    tiff_native=(repository/'engines/geopython-workflow/test_online_raster_fixture.py').read_text()
    if (any(not source_contract_contains(img_owner, fragment, repository) for fragment in ('TIFF_SOURCES','validate_tiff_meta','validate_tiff_physical','refresh_tiff','source_raw'))
            or any(not source_contract_contains(img_browser, fragment, repository) for fragment in ('tiff_pages_verified','cog_metadata_visible','zero_overviews_verified','refreshed_metadata_verified','tiff_pixels_verified','tiff-unlocated-small-cog-button.tif'))
            or any(not source_contract_contains(tiff_physical, fragment, repository) for fragment in ('def manager_tiff_facts','def manager_seed_tiff','def manager_tiff_source','tiff-source','full_check=True'))
            or any(not source_contract_contains(tiff_native, fragment, repository) for fragment in ('test_manager_tiff_unlocated_small_cog_complete_pixels_and_source_bytes','test_manager_tiff_rejects_semantic_tampering_even_with_rewritten_receipt'))):
        raise RegistrationError('Manager TIFF COG requires complete physical, exact refresh, browser and regression proofs')
    for relative in ('scripts/test/manager-orc-online.py','scripts/test/manager-orc-online_test.py',
                     'business/scripts/online-orc-fixture.py','console/frontend/e2e/online/manager-orc.spec.js'):
        if not (repository / relative).is_file():
            raise RegistrationError('Manager ORC requires its source, physical, browser and regression files')
    orc_owner = (repository / 'scripts/test/manager-orc-online.py').read_text()
    orc_browser = (repository / 'console/frontend/e2e/online/manager-orc.spec.js').read_text()
    if ("if case == 'native-table-orc':" not in manager_owner
            or 'manager-orc-online' not in manager_owner
            or not img_runner or 'scripts/test/manager-orc-online_test.py' not in make_target_commands(make, "test-manager-online-runner")
            or 'console/frontend/e2e/online/manager-orc.spec.js' not in make_target_commands(make, "test-manager-online-runner")
            or any(not source_contract_contains(orc_owner, fragment, repository) for fragment in ('task-definitions','source_version','physical("verify")','physical("delete")','manager-orc.json'))
            or any(not source_contract_contains(orc_browser, fragment, repository) for fragment in ('page_button_verified: true','original_button_download_verified: true',"expect(expected.permissions.size).toBe(11)"))):
        raise RegistrationError('Manager ORC requires complete page, download, batch and cleanup evidence')
    delimited_paths = ('scripts/test/manager-delimited-online.py', 'scripts/test/manager-delimited-online_test.py',
                       'business/scripts/online-delimited-fixture.py', 'console/frontend/e2e/online/manager-delimited.spec.js')
    if any(not (repository / p).is_file() for p in delimited_paths):
        raise RegistrationError('Manager delimited case requires its owner, fixed bytes, browser and regressions')
    delimited_owner = (repository / delimited_paths[0]).read_text()
    delimited_browser = (repository / delimited_paths[3]).read_text()
    if ("if case == 'delimited-read-settings':" not in manager_owner
            or 'manager-delimited-online_test.py' not in make_target_commands(make, 'test-manager-online-runner')
            or 'manager-delimited.spec.js' not in make_target_commands(make, 'test-manager-online-runner')
            or any(part not in delimited_owner for part in ("physical('verify')", "physical('delete')", 'validate_metadata', 'validate_page', 'manager-delimited.json', 'ExactFileMetadataGrants'))
            or any(part not in delimited_browser for part in ('header_mode_verified: true', 'ordinary_refresh_verified: true', 'original_button_download_verified: true', 'toBe(10)'))):
        raise RegistrationError('Manager delimited case must retain read settings, pages, source downloads and cleanup')
    avro_paths = ('scripts/test/manager-avro-online.py', 'scripts/test/manager-avro-online_test.py',
                  'business/scripts/online-avro-fixture.py', 'scripts/test/online-table-fixture-support.py',
                  'console/frontend/e2e/online/manager-avro.spec.js',
                  'engines/geopython-workflow/test_online_avro_fixture.py')
    if any(not (repository / p).is_file() for p in avro_paths):
        raise RegistrationError('Manager Avro requires its fixture, shared transport, browser and regressions')
    avro_owner = (repository / avro_paths[0]).read_text()
    avro_browser = (repository / avro_paths[-2]).read_text()
    if ("if case == 'native-table-avro':" not in manager_owner
            or 'manager-avro-online_test.py' not in make_target_commands(make, 'test-manager-online-runner')
            or 'manager-avro.spec.js' not in make_target_commands(make, 'test-manager-online-runner')
            or 'test_online_avro_fixture.py' not in make_target_commands(make, 'test-geopython-workflow')
            or any(part not in avro_owner for part in ("physical('verify')", "physical('delete')", 'task-definitions', 'validate_page', 'manager-avro.json'))
            or any(part not in avro_browser for part in ('page_button_verified: true', 'values_verified: true', 'download_sha256', 'expect(identity.permissions.size).toBe(11)'))):
        raise RegistrationError('Manager Avro requires complete page, original download, batch and cleanup proofs')
    feather_paths = ('scripts/test/manager-feather-online.py','scripts/test/manager-feather-online_test.py',
                     'business/scripts/online-feather-fixture.py','console/frontend/e2e/online/manager-feather.spec.js',
                     'engines/geopython-workflow/test_online_feather_fixture.py')
    if any(not (repository/p).is_file() for p in feather_paths):
        raise RegistrationError('Manager Feather requires its owner, physical, browser and regressions')
    feather_owner=(repository/feather_paths[0]).read_text()
    feather_browser=(repository/feather_paths[3]).read_text()
    if ("if case == 'native-table-feather':" not in manager_owner
            or 'manager-feather-online_test.py' not in make_target_commands(make,'test-manager-online-runner')
            or 'manager-feather.spec.js' not in make_target_commands(make,'test-manager-online-runner')
            or 'test_online_feather_fixture.py' not in make_target_commands(make,'test-geopython-workflow')
            or any(part not in feather_owner for part in ("physical('verify')", "physical('delete')",'validate_content','task-definitions','manager-feather.json','ExactFileMetadataGrants','original_download_denied'))
            or any(part not in feather_browser for part in ('page_button_verified: true','original_button_download_verified: true','original_download_unavailable_verified: true','download_sha256','browserAPIFromNavigation','toBe(11)'))):
        raise RegistrationError('Manager Feather requires exact sources, real pages, originals, batch and cleanup')
    browser_config = (repository / "console/frontend/playwright.online.config.js").read_text(encoding="utf-8")
    for flag in ("--use-gl=angle", "--use-angle=swiftshader-webgl", "--enable-unsafe-swiftshader"):
        if flag not in browser_config:
            raise RegistrationError("manager-internal-artifact-lineage Hosted browser requires software WebGL")
    fixture = (repository / "business/scripts/online-manager-minio-fixture.sh").read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "business-minio",
        "pdal_las12_format0.las",
        "addp_online_preview_fixture.pptx",
        "MC_HOST_fixture",
        "dae/model.dae",
        "3ds/model.3ds",
        "texture.png",
        "format-samples/gbk-quoted.csv",
        "format-samples/gbk-quoted.tsv",
        "format-samples/format-validation.xlsx",
        "format-samples/document.json",
        "format-samples/records.json",
        "format-samples/features.geojson",
        "format-samples/features.json",
        "format-samples/trajectory.csv",
        "format-samples/trajectory.json",
        "format-samples/trajectory.geojson",
        "format-samples/vector-exchange/sites.kml",
        "format-samples/vector-exchange/sites.gml",
        "format-samples/vector-exchange/projected.topojson",
        "index-source-versions/source.csv",
        "index-source-versions/source.tsv",
        "index-csv-change",
        "index-tsv-restore",
        "index source restoration refused unexpected bytes",
    ):
        if not source_contract_contains(fixture, fragment, repository):
            raise RegistrationError(
                f"manager-internal-artifact-lineage fixture contract is missing {fragment}"
            )
    owner = (
        repository / "scripts/test/manager-internal-artifact-lineage-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        "addp.lineage-facts/v1",
        "/api/v1/meta/scan/run/manual",
        "/api/v1/monitor/executions/by-execution-id/",
        "addp-infra://minio/manager/tenant_",
        "/api/v1/manager/point_cloud_copc/",
        "/api/v1/manager/quick-view/capability",
        "/api/v1/manager/quick-view/actions",
        "/api/v1/manager/tasks/{PPTX_TASK_TYPE}/",
        '"cache_reused": True',
        "model_3d_glb_generation",
        "/api/v1/manager/model_3d_glb/",
        "validate_model_glb",
        "cleanup_managed_artifact",
        "generate_raster_cog",
        "raster_to_cog",
        "raster_physical",
        "DELIMITED_SHA256",
        "delimited_sources",
        "WORKBOOK_SHA256",
        "WORKBOOK_SHEETS",
        "workbook_source",
        "STRUCTURED_JSON_SOURCES",
        "structured_json_sources",
        "TRAJECTORY_SOURCES",
        "trajectory_sources",
        "VECTOR_EXCHANGE_SOURCES",
        "vector_exchange_sources",
        "MODEL_FORMATS",
        "MESH_SOURCE_FACTS",
        "FBX_BASIC_SOURCE",
        "prepare_basic_fbx_source",
        "fbx_basic_preview",
        "verify_content_boundaries",
        "boundaries = verify_content_boundaries",
        "boundaries += verify_content_boundaries",
        "content-boundaries.json",
        "addp.content-boundaries/v2",
        "directory-convergence",
        "refs-convergence",
        '"removed_components"',
        '"preserved_initial_items"',
        "force=not converging",
        "ADDP_ONLINE_MANAGER_NFS_ENGINE_ID",
        "verify_mesh_source",
        "validate_model_bundle",
        "download_bundle",
        "addp.manager-internal-artifact-lineage/v15",
        '"header_mode"',
        '"tested_has_header": False',
        "first_record_verified",
        "ordinary_refresh_preserved",
        "versions = verify_index_source_versions",
        "index-source-versions.json",
        "addp.index-source-versions/v2",
        'storage in ("nfs", "minio")',
        "verify_manager_execution_events",
        "addp.manager-execution-events/v1",
        "manager-execution-events.json",
    ):
        if not source_contract_contains(owner, fragment, repository):
            raise RegistrationError(
                f"manager-internal-artifact-lineage owner contract is missing {fragment}"
            )
    nfs = (repository / "business/scripts/online-manager-nfs-fixture.sh").read_text(encoding="utf-8")
    for fragment in ("GITHUB_ACTIONS", "ADDP_ONLINE_HOSTED", "127.0.0.1(ro,sync", ".addp-online-owner", "online-content-boundary-fixture.py", "NFS export cleanup has residuals"):
        if not source_contract_contains(nfs, fragment, repository):
            raise RegistrationError(f"Manager NFS lifecycle contract is missing {fragment}")
    generator = (repository / "business/scripts/online-content-boundary-fixture.py").read_text(encoding="utf-8")
    for fragment in ("directory-convergence", "refs-convergence"):
        if not source_contract_contains(generator, fragment, repository):
            raise RegistrationError(f"Manager boundary fixture contract is missing {fragment}")
    browser = (
        repository
        / "console/frontend/e2e/online/manager-internal-artifact-lineage.spec.js"
    ).read_text(encoding="utf-8")
    for fragment in (
        ".execution-lineage__group",
        ".execution-lineage__resource-action",
        "平台内部产物|Platform-internal artifact",
        "platform_internal_outputs",
        ".pptx-preview .pdf-preview",
        "pptx_page_after_engine_refresh",
        "pptx_generation_requests",
        ".model-preview",
        "model_loaded",
        "content_loaded",
        ".raster-tiff-quick-view",
        "raster_generation_requests",
        "range_loaded",
        "map_loaded",
        "ADDP_ONLINE_MANAGER_BROWSER_PHASE",
        "generation-entry",
        "cached-preview",
        "generation_entry_visible",
        "source_model_loaded",
        "verifyBasicColoredFBX",
        "basic_preview_source",
        "basic_preview: basicPreviewProof",
        "vertex_colors_verified",
        "model-preview-support.js",
        "model_generation_requests",
        "anonymous_refresh_401",
        "failed_business_responses",
        "addp.manager-internal-artifact-lineage-browser/v15",
        "header_mode:",
        "applyHeaderMode(false)",
        "verifyHeaderlessPage(20, 6)",
        "applyHeaderMode(true)",
        "ADDP_ONLINE_MANAGER_DELIMITED_JSON",
        "verifyDelimitedTable",
        "delimited_tables",
        "encoding_request_verified",
        "download_sha256",
        "ADDP_ONLINE_MANAGER_WORKBOOK_JSON",
        "verifyWorkbook",
        ".container-preview",
        "workbook: workbookEvidence",
        "ADDP_ONLINE_MANAGER_STRUCTURED_JSON",
        "verifyStructuredJSON",
        "structured_json: structuredEvidence",
        "ADDP_ONLINE_MANAGER_TRAJECTORIES_JSON",
        "verifyTrajectoryRange",
        "trajectories: trajectoryEvidence",
        "ADDP_ONLINE_MANAGER_VECTOR_EXCHANGE_JSON",
        "verifyVectorExchange",
        "vector_exchange: vectorEvidence",
        "layer_proofs",
        "temporary_reset_verified",
        "content_verified",
        "table_verified",
        "geometry_verified",
        "source_crs",
        "source_srid",
        "unknown_total_verified",
        "refresh_preserved_page",
        "refresh_preserved_empty_sheet",
    ):
        if not source_contract_contains(browser, fragment, repository):
            raise RegistrationError(
                f"manager-internal-artifact-lineage browser contract is missing {fragment}"
            )


def validate_manager_hybrid_search_profile(repository: Path, registered: set[str]) -> None:
    if "manager-hybrid-search" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "manager-hybrid-search)",
        "START_TARGET=-all",
        "SYSTEM_URL GATEWAY_URL META_URL MANAGER_URL INFERENCE_URL",
        "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN ADDP_ONLINE_TEST_TENANT_ID",
        "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID",
        "ADDP_ONLINE_MANAGER_MINIO_PORT",
        "ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY",
        "ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY",
        "ADDP_ONLINE_MANAGER_MINIO_BUCKET",
        "ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT",
        "ADDP_ONLINE_MANAGER_EMBEDDING_MODEL_PROFILE_ID",
        "bash business/scripts/online-manager-minio-fixture.sh start",
        "bash business/scripts/online-manager-minio-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
    )
    missing = [fragment for fragment in required_fragments if not source_contract_contains(host_gate, fragment, repository)]
    if missing:
        raise RegistrationError(
            "manager-hybrid-search profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-manager-minio-fixture.sh",
        "scripts/test/manager-hybrid-search-online.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"manager-hybrid-search requires {relative}")
    fixture = (repository / "business/scripts/online-manager-minio-fixture.sh").read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "business-minio",
        "Autocop_4X3.jpg",
        "ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT",
        "MC_HOST_fixture",
    ):
        if not source_contract_contains(fixture, fragment, repository):
            raise RegistrationError(
                f"manager-hybrid-search fixture contract is missing {fragment}"
            )
    owner = (repository / "scripts/test/manager-hybrid-search-online.py").read_text(encoding="utf-8")
    for fragment in (
        "/api/v1/meta/scan/run/manual",
        "/api/v1/manager/embedding_executions",
        "/api/v1/manager/embeddings?",
        "/api/v1/manager/search?",
        'expected_methods: list[str]',
        '["keyword", "vector"]',
        '["vector"]',
        "vector_hits",
        '"residual_resources": -1',
    ):
        if not source_contract_contains(owner, fragment, repository):
            raise RegistrationError(
                f"manager-hybrid-search owner contract is missing {fragment}"
            )


def validate_security_transfer_protection_profile(repository: Path, registered: set[str]) -> None:
    if "security-transfer-protection" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "security-transfer-protection)",
        "START_TARGET=-all",
        "SYSTEM_URL GATEWAY_URL META_URL SECURITY_URL GRAPH_URL TRANSFER_URL MANAGER_URL",
        "ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID",
        "ADDP_ONLINE_SECURITY_MONGODB_ROOT_PASSWORD",
        "bash business/scripts/online-security-transfer-fixture.sh start",
        "bash business/scripts/online-security-transfer-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
    )
    missing = [fragment for fragment in required_fragments if not source_contract_contains(host_gate, fragment, repository)]
    if missing:
        raise RegistrationError(
            "security-transfer-protection profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-security-transfer-fixture.sh",
        "scripts/test/security-transfer-protection-online.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"security-transfer-protection requires {relative}")
    fixture = (repository / "business/scripts/online-security-transfer-fixture.sh").read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "business-mongodb",
        "online-engine-fixture.sh",
        'role: "read"',
        "addp_online_security.transfer_masked",
    ):
        if not source_contract_contains(fixture, fragment, repository):
            raise RegistrationError(
                f"security-transfer-protection fixture contract is missing {fragment}"
            )
    owner = (repository / "scripts/test/security-transfer-protection-online.py").read_text(encoding="utf-8")
    for fragment in (
        "/api/v1/meta/scan/run/manual",
        "/api/v1/security/protection-enrollments",
        '("export", "mask")',
        "/api/v1/transfer/task-definitions",
        "/api/v1/manager/preview",
        "cleanup_tasks(client, tasks)",
        '"residual_resources": 0',
    ):
        if not source_contract_contains(owner, fragment, repository):
            raise RegistrationError(
                f"security-transfer-protection owner contract is missing {fragment}"
            )


def validate_security_plaintext_access_profile(repository: Path, registered: set[str]) -> None:
    if "security-plaintext-access" not in registered:
        return
    host = (repository / "scripts/test/online-host-gate.sh").read_text()
    if "security-plaintext-access" in host:
        raise RegistrationError("security-plaintext-access must not keep a self-hosted route")
    requirements = {
        "scripts/test/online-hosted-security-gate.sh": (
            "# ADDP_ONLINE_SUITES=security-plaintext-access", "security-plaintext-access-online.py",
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite "$1"', 'playwright install --with-deps chromium',
            '"scripts/test/$INITIALIZER_SCRIPT" --initialize',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN',
        ),
        "business/scripts/online-security-owner-fixture.sh": ("addp_online_security.exemption_source", "13987654321", "GRANT SELECT ON addp_online_security.exemption_source"),
        "scripts/test/security-plaintext-access-online.py": (
            "SUPPORT.require_hosted_initialization(os.environ)", "initialize_fresh_governance(",
            "initialize_exact_record_read_grants(", "ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN",
            "applicant and approver must be two different Users", "preview_rows = SUPPORT.preview_rows",
            "/api/v1/security/protection-access-request-targets", "review-queue?scope=pending",
            '"scope": "history"', "protected_other_subject", "expired_without_security_refresh",
            "revoked_applicant", "revoked_approval_history", 'addp.security-plaintext-access-online/v6', 'validate_plaintext_browser_report(', 'run_plaintext_browser(', 'report["browser"] = run_plaintext_browser(',
            'outage = exercise_security_unavailability(', 'SUPPORT.require_hosted_security_lifecycle(repository)',
            'SUPPORT.run_security_lifecycle(repository, "stop", deadline)', 'SUPPORT.run_security_lifecycle(repository, "start", deadline)',
            'finally:', 'SUPPORT.security_listener_refused()', 'connection_refusals', 'manager_process_unchanged',
            'current_develop_result(', 'authorized_history_masked', 'current_result_rechecked', 'exercise_develop_scopes(', 'same_history_masked_at_expiry', 'same_history_masked_at_revocation',
            'manager_only_does_not_authorize_develop', 'private_history_excluded', 'manager_grant_independent',
            'validate_develop_scope_report(',
            'develop_history_while_unavailable_verified', 'develop_process_unchanged',
        ),
        "scripts/test/security-transfer-protection-online.py": (
            'def require_hosted_security_lifecycle(', 'def ready_process(', 'def verify_restarted_process(',
            'def run_security_lifecycle(', 'def security_listener_refused(',
            'ADDP_ONLINE_TEST_USER_', 'ADDP_ONLINE_TEST_APPROVER_', 'ADDP_ONLINE_FIXTURE_',
            'a disposable Hosted Linux checkout without .env', 'registration_state', 'source_fingerprint',
        ),
        "system/backend/cmd/online-test-fixture/main.go": ("plaintextApplicantPermissions", "plaintextApproverPermissions", "external-online-plaintext-approver", "ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN", "ADDP_ONLINE_TEST_APPROVER_USERNAME", "ADDP_ONLINE_TEST_APPROVER_PASSWORD"),
        "console/frontend/e2e/online/security-plaintext-access.spec.js": (
            "addp.security-plaintext-access-browser/v2", "browser.newContext(", "browserIdentity(",
            "submitRequest(", "approve(", "history(", "revocation_from_security_ui",
            "target.projection_acknowledged === true", "await synchronized", "await secondSynchronized",
            "Date.now() >= Date.parse(first.requested_expires_at) + 1000", "expired_history_verified",
            "revoked_history_verified", "isAnonymousRefreshResponse", "state.started = true",
            "multi_outlet_request_verified", "['manager','develop']",
            "expect(applicantState.failures.concat(approverState.failures)).toEqual([])",
            "expect(applicantState.errors.concat(approverState.errors)).toEqual([])",
            "screenshot: 'off'", "for (const context of contexts) await context.close()",
        ),
    }
    for relative, fragments in requirements.items():
        path = repository / relative
        if not path.is_file():
            raise RegistrationError(f"security-plaintext-access requires {relative}")
        text = path.read_text()
        for fragment in fragments:
            if not source_contract_contains(text, fragment, repository):
                raise RegistrationError("security-plaintext-access contract is missing " + fragment)
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  security-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "runs-on: ubuntu-24.04" not in job.group("body") or "inputs.suite == 'security-plaintext-access'" not in job.group("body"):
        raise RegistrationError("security-plaintext-access requires a manual Ubuntu Hosted job")
    if "&& inputs.suite != 'security-plaintext-access'" not in workflow:
        raise RegistrationError("security-plaintext-access must not also dispatch on self-hosted")
    aggregate = make_target_commands((repository / "Makefile").read_text(), "test-online-runner")
    for test in ("security-plaintext-access-online_test.py", "online-hosted-security-gate_test.py"):
        if not aggregate or not source_contract_contains(aggregate, test, repository):
            raise RegistrationError("security-plaintext-access regression must enter test-online-runner")


def validate_security_mysql_owner_protection_profile(
    repository: Path, registered: set[str]
) -> None:
    if "security-mysql-owner-protection" not in registered:
        return
    suite = load_suite_registry(repository)["security-mysql-owner-protection"]
    if ("monitor", "MONITOR_URL") not in suite.services:
        raise RegistrationError("security-mysql-owner-protection must preflight Monitor")
    hosted_path = repository / "scripts/test/online-hosted-security-gate.sh"
    if not hosted_path.is_file():
        raise RegistrationError("security-mysql-owner-protection requires Hosted lifecycle")
    hosted = hosted_path.read_text(encoding="utf-8")
    for fragment in (
        'source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
        'GOWORK="$ADDP_ONLINE_SECRET_DIR/go.work"', 'go work init "${go_modules[@]}"',
        'GOMODCACHE="$ROOT_DIR/.gomodcache"',
        'cp "$ROOT_DIR/go.work.sum" "${GOWORK}.sum"',
        'for start_target in -meta -security -manager -develop -service -transfer -graph -monitor',
        'playwright install --with-deps chromium',
        'MONITOR_URL=', 'CONSOLE_URL=',
        'bash business/scripts/online-security-owner-fixture.sh start',
        'bash business/scripts/online-security-owner-fixture.sh stop',
        '--suite "$1"', 'scripts/test/online-engine-registration.py',
        'security-mongodb.json', 'ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID', 'addp-security-online-mongodb',
        '"scripts/test/$INITIALIZER_SCRIPT" --initialize',
        'before_online_application_stop() (', 'source "$IDENTITY_ENV" || return 1',
        'security-record-grants.json',
        'scripts/test/security-mysql-owner-protection-online.py --cleanup-record-grants',
        'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN',
        'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
    ):
        if not source_contract_contains(hosted, fragment, repository):
            raise RegistrationError(f"security-mysql-owner-protection Hosted profile is missing {fragment}")
    host = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    if "security-mysql-owner-protection" in host:
        raise RegistrationError("security-mysql-owner-protection must not keep a self-hosted route")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
    job = re.search(r"(?ms)^  security-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
            or "inputs.suite == 'security-mysql-owner-protection'" not in job.group("body")):
        raise RegistrationError("security-mysql-owner-protection requires a manual Ubuntu Hosted job")
    if "&& inputs.suite != 'security-mysql-owner-protection'" not in workflow:
        raise RegistrationError("security-mysql-owner-protection must not also dispatch on self-hosted")
    if '      security_source_commit:\n' not in workflow or any(
        not source_contract_contains(job.group('body'), fragment, repository)
        for fragment in (
            'name: Validate requested Security source commit',
            'SOURCE_COMMIT: ${{ inputs.security_source_commit }}',
            '^[0-9a-f]{40}$',
            '[[ -z "$SOURCE_COMMIT" || "$SUITE" == security-mysql-owner-protection ]]',
            'ref: ${{ inputs.security_source_commit || github.sha }}',
            'name: Verify Security source ancestry',
            'DISPATCH_COMMIT: ${{ github.sha }}',
            '[ "$(git rev-parse HEAD)" = "$SOURCE_COMMIT" ]',
            'git merge-base --is-ancestor "$SOURCE_COMMIT" "$DISPATCH_COMMIT"',
        )
    ):
        raise RegistrationError("Security fixed source must validate exact SHA, suite, checkout and dispatch ancestry")
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    aggregate = make_target_commands((repository / "Makefile").read_text(), "test-online-runner")
    for test in ("online-hosted-security-gate_test.py", "online-security-owner-fixture_test.py", "security-mysql-owner-protection-online_test.py"):
        if not aggregate or not source_contract_contains(aggregate, test, repository):
            raise RegistrationError("security-mysql-owner-protection regression must enter test-online-runner")
    for relative in (
        "business/scripts/online-security-owner-fixture.sh",
        "scripts/test/security-mysql-owner-protection-online.py",
        "scripts/test/security-transfer-protection-online.py",
        "console/frontend/e2e/online/security-manager-export.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"security-mysql-owner-protection requires {relative}")
    fixture = (repository / "business/scripts/online-security-owner-fixture.sh").read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOSTED", "GITHUB_ACTIONS", "container ownership mismatch", "container remains after cleanup",
        "bank-schema-add|bank-schema-remove", "_security_drift_probe", "ADD COLUMN", "DROP COLUMN",
        "--tmpfs /var/lib/postgresql/data", "--tmpfs /var/lib/mysql", "CREATE EXTENSION postgis",
        "location_point geometry(Point, 4326)", "addp_online_security.mysql_email_transfer",
        "CREATE TABLE bank_card_accounts", "credit_card_number int NOT NULL", "addp_online_security.bank_card_transfer",
        "addp_online_security.spatial_algorithm_source", "addp_online_security.spatial_algorithm_transfer",
        "business/mysql/test-data.sh", "security_reader", "security_writer",
        "addp-security-online-mongodb", "--tmpfs /data/db", "fixture.Persons.insertMany", "Outdoor",
        'sourceVersion: NumberLong("9007199254740993")',
        "addp_online_security.mongodb_query_transfer", "addp_online_security.mongodb_query_control", "addp_online_security.mongodb_query_denied",
    ):
        if not source_contract_contains(fixture, fragment, repository):
            raise RegistrationError("security-mysql-owner-protection fixture contract is missing " + fragment)
    owner = (
        repository / "scripts/test/security-mysql-owner-protection-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        'def record_grant_targets(', 'database != "security_fixture"',
        'target(mongo, "Outdoor", "Persons", "collection")', 'for name in (SOURCE_TABLE, BANK_SOURCE)',
        'def initialize_record_grants(', 'def cleanup_record_grants(',
        'REGISTRATION.ExactRecordGrants', 'write_record_grant_checkpoint(state, new=True)',
        'record_grant_receipt("preparation", state', 'record_grant_receipt("cleanup", state',
        'len(state["pending"]) > 6', 'row not in state["issued"]',
        'grant.cleanup()', 'cleanup_final_visibility', 'cleanup_final_content_read',
        'RECORD_GRANT_ACTIONS = ("metadata_read", "read")', 'for action in reversed(RECORD_GRANT_ACTIONS)',
    ):
        if not source_contract_contains(owner, fragment, repository):
            raise RegistrationError("security-mysql-owner-protection metadata preparation is missing " + fragment)
    owner_contract = owner + (
        repository / "scripts/test/security-transfer-protection-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        "/api/v1/meta/scan/run/manual",
        "/api/v1/security/sensitive-data-types",
        "security.sensitive_data_type.create",
        "security.sensitive_data_type.delete",
        '"default_protection"',
        '"invalid_request_status"',
        '"rollback_verified"',
        '"cleanup_verified"',
        "addp.detector.email_metadata/v1", "addp.detector.phone_metadata/v2",
        "exercise_bank_card_metadata(", "assert_bank_card_rows(", "assert_bank_card_discovery(", "bank_card_projections(",
        "addp.detector.bank_card_number_metadata/v1", '"bank_card_metadata"', '"negative_fields_not_detected"',
        '"temporary_execution_resource_residuals": 0',
        "exercise_mongodb_algorithms(", "assert_mongodb_rows(", '"mongodb_algorithms"', '"sparse_objects_verified"',
        "ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID", "def initialize_postgres_read_grants(",
        "/api/v1/security/protection-baselines",
        "/api/v1/security/protection-enrollments",
        "/api/v1/develop/executions",
        "/api/query/",
        "/api/v1/transfer/task-definitions",
        '"effect": "suppress"',
        '"residual_resources": 0',
    ):
        if not source_contract_contains(owner_contract, fragment, repository):
            raise RegistrationError(
                "security-mysql-owner-protection owner contract is missing "
                + fragment
            )

    initializer = re.search(
        r"(?ms)^def initialize_postgres_read_grants\((?P<parameters>.*?)\) -> None:\n(?P<body>.*?)(?=^def |\Z)", owner)
    if (initializer is None
            or any(engine in initializer.group("parameters").lower() for engine in ("mysql", "mongodb"))
            or re.search(r"item_term\s*=\s*['\"]collection['\"]", initializer.group("body"))
            or initializer.group("body").count("REGISTRATION.initialize_exact_record_read_grants(") != 1):
        raise RegistrationError("security-mysql-owner-protection must prepare only PostgreSQL table read Grants")

    for fragment in ("run_export_browser(", "validate_export_browser_report(", "e2e/online/security-manager-export.spec.js",
                     "wait_for_technical_field(", '"technical_field_search"', '"manager.search.execute"',
                     "SUPPORT.require_hosted_security_lifecycle(", "exercise_restart_recovery(", "verify_restarted_process(",
                     '"scripts/dev/restart.sh", "-security", "-manager"', '"verified_before_rescan"',
                     "prepare_mongodb_source(", '"initial_technical_field_search"',
                     "verify_unprotected_mongodb_derived_query(", "verify_mongodb_query_boundaries(", "validate_develop_protection_refusal(",
                     "validate_transfer_protection_refusal(", "validate_service_protection_refusal(",
                     "prepare_mongodb_transfer_targets(", "verify_mongodb_query_transfer(",
                     '"query_transfer"', '"derived_transfer_denied"', '"transfer_during_manager_policy"',
                     '"target_empty_before_and_after"', '"query_output_lineage_unresolved"',
                     "verify_mongodb_raw_export(", '"raw_record_export"', '"export_during_manager_policy"',
                     "canonical_int64_preserved=True",
                     '"develop_during_manager_policy"', '"addp.security-mysql-owner-protection-online/v46"',
                     'postgres_transfer_join = exercise_postgres_transfer_join(',
                     '"postgres_transfer_join": postgres_transfer_join',
                     'postgres_grouped_outputs = exercise_postgres_grouped_outputs(',
                     '"postgres_grouped_outputs": postgres_grouped_outputs',
                     'verify_postgres_grouped_transfer(', '"default_csv_restored_exactly"',
                     '"history_default_verified": True', '"live_result_verified": True',
                     'postgres_grouped_outputs["deleted_resources"]',
                     'join_prepared = prepare_service_join_eligibility(client, reader, postgres_engine_id,',
                     'join_result = verify_service_join_eligibility(client, fresh, join_prepared)',
                     '"postgres_join_eligibility": join_result',
                     'postgres_join_policies_revoked=len(join_policy_ids)',
                     "exercise_service_role_cursor_revocation(", '"service_role_cursor_revocation"',
                     "service_role_cursor_revocation = exercise_service_role_cursor_revocation(",
                     '"service_role_cursor_revocation": service_role_cursor_revocation',
                     "service_sql_role_cursor_revocation = exercise_service_role_cursor_revocation(",
                     '"service_sql_role_cursor_revocation": service_sql_role_cursor_revocation',
                     "service_sql_parameter_role_cursor_revocation = exercise_service_role_cursor_revocation(",
                     '"service_sql_parameter_role_cursor_revocation": service_sql_parameter_role_cursor_revocation',
                     'config_type="sql", parameters={"minimum_id": 1})',
                     'def prepare_service_sql_eligibility(',
                     'sql_prepared = prepare_service_sql_eligibility(reader,',
                     'sql_prepared = prepare_service_sql_eligibility(active_reader,',
                     'sql_result = verify_service_sql_eligibility(fresh,',
                     'sql_result = verify_service_sql_eligibility(active_reader,',
                     '"sql_eligibility": sql_result',
                     'cleanup_service_sql_eligibility(client, sql_service_ids, failure)',
                     '"no_cursor_csv"',
                     'service_sql_default_parameter_role_cursor_revocation = exercise_service_role_cursor_revocation(',
                     '"service_sql_default_parameter_role_cursor_revocation": service_sql_default_parameter_role_cursor_revocation',
                     'config_type="sql", parameters={}, parameter_default=1)',
                     '"default_resolution"',
                     '"resolved_default_equivalence_verified"',
                     '"omitted_cursor_explicit_continuation"',
                     '"explicit_cursor_omitted_continuation"',
                     '"explicit_default_parameter_refusals"',
                     "def verify_service_published_default_revision(", '"published_default_revision"',
                     "default_revision = verify_service_published_default_revision(",
                     '"named_parameter_defaults": {"minimum_id": 2}',
                     '"continuation_checks"', '"reference_rows": 4', '"combined_ids": joined',
                     '"omitted_cursor_explicit_second"', '"explicit_cursor_omitted_second"',
                     '"changed_parameter_refusals"', '"csv_pagination_headers_verified"',
                     '"integer_parameter_changed"', '"string_parameter_changed"',
                     '"invalid_default_updates"', '"unchanged_default_cursor"', '"option_domains_preserved"',
                     "def verify_service_string_default_revision(", "string_revision = verify_service_string_default_revision(",
                     '"string_default_revision"', '"stale_update"',
                     "def wait_for_postgres_item_scan(", '"postgres_refresh_scope"', '"target_scan_execution_ids"',
                     "scan_preparation=True", "wait_for_scan(authorizer,", 'scan_depth="basic"',
                     '"integer_outside_options"', '"string_outside_options"', '"mixed_partial_invalid"',
                     '"complete_definition_unchanged"', '"invalid_named_parameter_defaults"',
                     '"minimum_code", "type": "string"',
                     'control_first, control_cursor, control_version = assert_service_role_page(',
                     '"explicit_old_default"', '"default_revision_persisted"',
                     '"immutable_declarations_preserved"', '"explicit_old_value_new_query"',
                     "def service_role_parameter_boundaries(", '"parameter_boundary_checks"',
                     '"continuation_parameters_bound"', '"cursor_parameter_changed"',
                     'deadline, config_type="sql")', '"default_csv_restored_exactly"',
                     '"retained_choice_save_refusal"', '"stale_choice_reenable_refusal"',
                     '"created_resources": 14 + len(task_ids)', '"deleted_resources": 14 + len(task_ids)',
                     "wait_role_projection(", '"valid_old_cursor_continuation"',
                     '"old_cursor_refusals"', '"query_protection_choice_invalid"',
                     '"saved_choice_retained_until_explicit_removal"', '"explicit_removal_default_recovery"',
                     "service_role_membership_revocation = exercise_service_role_membership_revocation(",
                     '"service_role_membership_revocation": service_role_membership_revocation',
                     '"query_protection_roles_unavailable"', '"old_session_http_status": 401',
                     '"functional_permissions_unchanged"', '"formal_same_user_login_verified"',
                     '"policy_projection_and_saved_choice_unchanged_at_refusal"', 'eligibility_assignment_revoked=True',
                     'browser_environment.pop(key, None)', '"/api/v1/system/login"',
                     "service_role_membership_expiry = exercise_service_role_membership_expiry(",
                     '"service_role_membership_expiry": service_role_membership_expiry',
                     '"same_access_session_valid"', '"functional_permissions_and_authorization_version_unchanged"',
                     '"same_session_refusals"', 'timed_assignment_cleanup="expired"',
                     "assert_timed_service_assignment(", "cleanup_timed_service_assignment(",
                     "develop_role_membership_expiry = exercise_develop_role_membership_expiry(",
                     '"develop_role_membership_expiry": develop_role_membership_expiry',
                     '"old_live_result_refusal"', '"new_query_refusal"', '"history_after"',
                     '"policy_projection_and_execution_choice_unchanged"',
                     '"ADDP_ONLINE_SECURITY_DEVELOP_ROLE_USERNAME"',
                     "transfer_role_membership_expiry = exercise_transfer_role_membership_expiry(",
                     '"transfer_role_membership_expiry": transfer_role_membership_expiry',
                     '"no_execution_or_target_write_after_refusal"', '"old_file_still_refused_after_default_recovery"',
                     '"transfer_protection_roles_unavailable"', '"query_protection_choice_invalid"',
                     'retained_export_sessions=2', '"ADDP_ONLINE_SECURITY_TRANSFER_ROLE_USERNAME"',
                     '"export_bff_role_reason_preserved": True',
                     '"export_refusal_cache_control": export_cache_refusals',
                     '"cache_control": {"create": create_cache, "status": status_cache, "file": file_cache}',
                     "def export_no_store(",
                     "default_export_protection = exercise_default_export_protection(",
                     '"default_export_protection": default_export_protection',
                     "def default_export_csv(", "def default_export_refusals(",
                     '"retained_export_sessions": 6', '"export_protection_changed"',
                     '"artifact_cleanup_scope": "isolated Infra teardown"',
                     '"develop_wildcard"', '"SELECT b.* FROM "', '"develop_mixed_wildcard"',
                     "assert_bank_card_mixed_rows(", '"sensitive_copies_verified"', '"develop_page_wildcard"', '"service_sql_page"',
                     "exercise_bank_card_sql_pages(", '"no_duplicate_or_missing_rows"',
                     "exercise_bank_card_computed_outputs(", '"computed_outputs"', '"history_result_absent"', '"unprotected_calculation_rows"',
                     "develop_refusal_with_history(", "exercise_enrolling_refusal(", "exercise_bank_card_refusal_causes(",
                     "def protection_reason_matrix(", "bank_outlet_refusals(", "transfer_refusal_with_history(",
                     "validate_manager_protection_refusal(", '"protection_reason_matrix"', '"protocol_only"', '"coverage_counts"',
                     "run_bank_schema_fixture(", '"protection_not_ready"', '"protection_schema_mismatch"',
                     '"protection_operation_unsupported"', '"refusal_causes"', '"schema_restored"', '"additional_refusals"',
                     '"protection_computed_output_unsupported"', '"protection_lineage_unverified"', '"protection_expired"',
                     '"independent_outlet_fields"', 'expected_by_owner[owner][field]',
                     'outage = exercise_owner_unavailability(', 'case["security_unavailability"] = exercise_owner_unavailability(',
                     'verify_mysql_outage_transfer(', 'SUPPORT.run_security_lifecycle(repository, "stop", deadline)',
                     'SUPPORT.run_security_lifecycle(repository, "start", deadline)', 'finally:',
                     'SUPPORT.security_listener_refused()', 'owner_processes_unchanged', 'target_empty_before',
                     'same_user_verified', 'target_output_verified', 'diagnostics.get("code") != "query_output_lineage_unresolved"',
                     'exercise_explicit_rediscovery(', '"explicit_rediscovery"',
                     '"all_current_revisions_acknowledged"', '"projections_before"', '"projections_after"',
                     'verify_mysql_transfer_output(', '"security.enrollment.update"',
                     'exercise_resource_validity(', 'wait_protection_deadline(', '"resource_validity": resource_validity',
                     '"natural_expiry_verified"', '"permanent_restored"', '"resource_permanent_validity_visible"'):
        if not source_contract_contains(owner, fragment, repository):
            raise RegistrationError("security-mysql-owner-protection must dispatch and validate export browser: " + fragment)
    browser = (repository / "console/frontend/e2e/online/security-manager-export.spec.js").read_text(encoding="utf-8")
    for fragment in (
        "login(page,", "page.waitForEvent('download'", "source_item_locator",
        "format: 'csv'", "csv.DictReader",
        "transfer_execution_id", "execution.triggered_by", "execution.source_task_id",
        "/api/v1/monitor/executions/by-execution-id/", ".execution-detail-content",
        "addp.security-manager-export-browser/v10", "anonymous_refresh_401", "download.delete()",
        "protected_preview_verified", "technical_field_search_verified", "search_to_preview_verified",
        "verifyResourceCombinationFilters(", "resource_combination_filters_verified", "resource_filters_read_only",
        "verifyDevelopProtectionRefusal(", "develop_refusal_diagnostic_verified", "develop_monitor_same_execution",
        "verifyAdditionalProtectionHistories(", "additional_protection_histories_verified",
        "additional_protection_monitors_verified", "additional_protection_refusal_count",
        "verifyServiceTransferProtectionRefusal(", "service_old_preview_cleared", "transfer_refusal_visible",
        "verifyTransferResultProtection(", "transfer_result_stage", "transfer_result_target_unchanged",
        "transfer_result_baseline_restored", "result_browser_residual_resources", "result_transfer_execution_id",
        "security-transfer-result-refusal.png", "security-transfer-result-monitor.png",
        "output_protection_permanent_restored", "output_browser_residual_resources", "protection_transfer_execution_id",
        "security-service-protection-refusal.png", "security-service-protection-restored.png",
        "checkServiceExpiryPreview(", "selectServiceLanguage(", "service_chinese_expiry_verified", "service_chinese_recovery_verified",
        "security-service-protection-refusal-zh-cn.png", "security-service-protection-restored-zh-cn.png",
        "security-transfer-protection-refusal.png", "security-transfer-protection-monitor.png",
        "security-develop-protection-refusal.png", "security-develop-protection-monitor.png",
        "browserAPIFromResponse(", "browser_session_rebound_verified",
        "resource_filter_case_count", "security-resource-combination-filters.png", "security-resource-empty-intersection.png",
        "security-resource-filter-reset.png",
        "security-manager-protected-preview.png", "security-manager-field-search.png", ".field-name mark",
        "expect(browserErrors).toEqual([])", "expect(failedResponses).toEqual([])",
    ):
        if not source_contract_contains(browser, fragment, repository):
            raise RegistrationError("security-mysql-owner-protection export browser contract is missing " + fragment)


def validate_oceanbase_consumer_flow_profile(
    repository: Path, registered: set[str]
) -> None:
    if "oceanbase-consumer-flow" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(
        encoding="utf-8"
    )
    required_fragments = (
        "oceanbase-consumer-flow)",
        "SYSTEM_URL GATEWAY_URL META_URL MANAGER_URL TRANSFER_URL DEVELOP_URL SERVICE_URL",
        "ADDP_ONLINE_OCEANBASE_ENGINE_ID",
        "ADDP_ONLINE_OCEANBASE_PORT",
        "ADDP_ONLINE_OCEANBASE_DATABASE",
        "ADDP_ONLINE_OCEANBASE_USER",
        "ADDP_ONLINE_OCEANBASE_PASSWORD",
        "bash business/scripts/online-oceanbase-consumer-fixture.sh start",
        "bash business/scripts/online-oceanbase-consumer-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
    )
    missing = [fragment for fragment in required_fragments if not source_contract_contains(host_gate, fragment, repository)]
    if missing:
        raise RegistrationError(
            "oceanbase-consumer-flow profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-oceanbase-consumer-fixture.sh",
        "scripts/test/relational-consumer-flow-online.py",
        "scripts/test/online-oceanbase-consumer-fixture_test.py",
        "scripts/test/relational-consumer-flow-online_test.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"oceanbase-consumer-flow requires {relative}")
    fixture = (
        repository / "business/scripts/online-oceanbase-consumer-fixture.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "oceanbase/oceanbase-ce:4.4.2-lts",
        "business-oceanbase",
        "addp_online_consumer_source",
        "addp_online_consumer_target",
        "start|advance|stop|status",
        "reset_fixture",
    ):
        if not source_contract_contains(fixture, fragment, repository):
            raise RegistrationError(
                f"oceanbase-consumer-flow fixture contract is missing {fragment}"
            )
    owner = (
        repository / "scripts/test/relational-consumer-flow-online.py"
    ).read_text(encoding="utf-8")
    owner_contract = owner + (
        repository / "scripts/test/security-transfer-protection-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        '"verification_owner": "deployment_profile"',
        "/api/v1/meta/scan/run/manual",
        '"type": "watermark"',
        '"start": "committed"',
        '"end": "execution_upper_bound"',
        '"apply_mode": "upsert"',
        '"manager.data_item.read"',
        "/api/v1/manager/preview",
        "/api/v1/develop/executions",
        "/api/query/",
        "advance_fixture(profile)",
        '"empty_resume"',
        "cleanup_tasks(client, task_ids)",
        "cleanup_service(client, service_id)",
        '"residual_resources": 0',
    ):
        if not source_contract_contains(owner_contract, fragment, repository):
            raise RegistrationError(
                f"oceanbase-consumer-flow owner contract is missing {fragment}"
            )
    if "/api/v1/system/engines" in owner or '"system.engine.read"' in owner:
        raise RegistrationError(
            "oceanbase-consumer-flow consumer must not access the System Engine control plane"
        )


def validate_tidb_consumer_flow_profile(
    repository: Path, registered: set[str]
) -> None:
    if "tidb-consumer-flow" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(
        encoding="utf-8"
    )
    required_fragments = (
        "tidb-consumer-flow)",
        "SYSTEM_URL GATEWAY_URL META_URL MANAGER_URL TRANSFER_URL DEVELOP_URL SERVICE_URL",
        "ADDP_ONLINE_TIDB_ENGINE_ID",
        "ADDP_ONLINE_TIDB_PORT",
        "ADDP_ONLINE_TIDB_DATABASE",
        "ADDP_ONLINE_TIDB_USER",
        "bash business/scripts/online-tidb-consumer-fixture.sh start",
        "bash business/scripts/online-tidb-consumer-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
    )
    missing = [fragment for fragment in required_fragments if not source_contract_contains(host_gate, fragment, repository)]
    if missing:
        raise RegistrationError(
            "tidb-consumer-flow profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-tidb-consumer-fixture.sh",
        "scripts/test/docker-compose.tidb-t2.yml",
        "scripts/test/online-tidb-consumer-fixture_test.py",
        "scripts/test/relational-consumer-flow-online.py",
        "scripts/test/relational-consumer-flow-online_test.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"tidb-consumer-flow requires {relative}")
    fixture = (
        repository / "business/scripts/online-tidb-consumer-fixture.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "docker-compose.tidb-t2.yml",
        "addp-online-tidb-consumer",
        "addp_online_consumer_source",
        "addp_online_consumer_target",
        "start|advance|stop|status",
        "reset_fixture",
        "down --volumes --remove-orphans",
        "assert_zero_residue",
    ):
        if not source_contract_contains(fixture, fragment, repository):
            raise RegistrationError(
                f"tidb-consumer-flow fixture contract is missing {fragment}"
            )
    compose = (
        repository / "scripts/test/docker-compose.tidb-t2.yml"
    ).read_text(encoding="utf-8")
    for image in ("pingcap/pd", "pingcap/tikv", "pingcap/tidb"):
        if re.search(
            rf"(?m)^\s*image:\s*{re.escape(image)}:v8\.5\.8@sha256:[0-9a-f]{{64}}\s*$",
            compose,
        ) is None:
            raise RegistrationError(
                f"tidb-consumer-flow Compose must pin {image}:v8.5.8 by digest"
            )
    tikv_service = compose_service_block(compose, "tidb-tikv")
    if tikv_service is None or not service_has_required_nofile_limit(tikv_service):
        raise RegistrationError(
            "tidb-consumer-flow Compose must set tidb-tikv nofile soft/hard limits to at least 1000000"
        )
    if re.search(r"(?m)^volumes:\s*$", compose):
        raise RegistrationError(
            "tidb-consumer-flow Compose must remain disposable and volume-free"
        )
    owner = (
        repository / "scripts/test/relational-consumer-flow-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        '"tidb": ConsumerProfile(',
        'namespace_kind="database"',
        'item_code_prefix="TIDB"',
        'identifier_quote="`"',
        'fixture_script="business/scripts/online-tidb-consumer-fixture.sh"',
        '"verification_owner": "deployment_profile"',
        '"schema_version": "addp.relational-consumer-flow-online/v1"',
        '"residual_resources": 0',
    ):
        if not source_contract_contains(owner, fragment, repository):
            raise RegistrationError(
                f"tidb-consumer-flow owner contract is missing {fragment}"
            )
    if "/api/v1/system/engines" in owner or '"system.engine.read"' in owner:
        raise RegistrationError(
            "tidb-consumer-flow consumer must not access the System Engine control plane"
        )


def validate_opengauss_consumer_flow_profile(
    repository: Path, registered: set[str]
) -> None:
    if "opengauss-consumer-flow" not in registered:
        return
    hosted = (repository / "scripts/test/online-hosted-opengauss-gate.sh").read_text(
        encoding="utf-8"
    )
    if 'source "$ROOT_DIR/scripts/utils/hosted-online.sh"' not in hosted:
        raise RegistrationError("opengauss profile must use the shared Hosted lifecycle")
    hosted += (repository / "scripts/utils/hosted-online.sh").read_text(encoding="utf-8")
    for fragment in (
        "# ADDP_ONLINE_SUITES=opengauss-consumer-flow",
        "GITHUB_ACTIONS",
        "RUNNER_OS",
        "Linux",
        "x86_64",
        "POSTGRES_DB=addp_online",
        "ADDP_ONLINE_SECRET_DIR",
        "go run ./cmd/online-test-fixture",
        "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN",
        "opengauss-official-media.sh",
        "refusing to reuse existing image",
        "bash business/scripts/online-opengauss-consumer-fixture.sh start",
        "python3 scripts/test/online-engine-registration.py",
        "--descriptor \"$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE\"",
        "for start_target in -security -manager -develop -service -transfer",
        "--wait-protection-owners manager,develop,service,transfer",
        "unset ADDP_TEST_OPENGAUSS_DSN",
        "python3 scripts/test/relational-consumer-flow-online.py --initialize-source-read",
        "unset ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN",
        'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        "bash scripts/dev/stop.sh",
        "bash scripts/infra/down.sh --volumes --force",
    ):
        if not source_contract_contains(hosted, fragment, repository):
            raise RegistrationError(
                f"opengauss-consumer-flow Hosted profile is missing {fragment}"
            )
    infra_up = (repository / "scripts/infra/up.sh").read_text(encoding="utf-8")
    for fragment in (
        "BUILD_REPOSITORY_POSTGRES_IMAGE=true",
        "docker build --file scripts/infra/Dockerfile.postgres",
        '--tag "$POSTGRES_IMAGE" scripts/infra',
    ):
        if not source_contract_contains(infra_up, fragment, repository):
            raise RegistrationError(
                f"opengauss-consumer-flow Infra lifecycle is missing {fragment}"
            )
    for relative in (
        "business/scripts/online-opengauss-consumer-fixture.sh",
        "scripts/infra/Dockerfile.postgres",
        "scripts/infra/up.sh",
        "scripts/test/online-hosted-opengauss-gate.sh",
        "scripts/test/online-hosted-opengauss-gate_test.py",
        "scripts/test/online-engine-registration.py",
        "scripts/test/online-engine-registration_test.py",
        "scripts/test/online-opengauss-consumer-fixture_test.py",
        "scripts/test/relational-consumer-flow-online.py",
        "scripts/test/relational-consumer-flow-online_test.py",
        "system/backend/cmd/online-test-fixture/main.go",
        "system/backend/cmd/online-test-fixture/main_test.go",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"opengauss-consumer-flow requires {relative}")
    fixture = (
        repository / "business/scripts/online-opengauss-consumer-fixture.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "opengauss_ensure_official_image x86_64",
        "refusing to reuse existing image",
        "addp-opengauss-online-disposable",
        "addp_opengauss_online",
        "addp_online_consumer_source",
        "addp_online_consumer_target",
        "MERGE INTO",
        "container_exists",
        "start|advance|stop|status",
        "ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE",
        '"engine_type": "opengauss"',
    ):
        if not source_contract_contains(fixture, fragment, repository):
            raise RegistrationError(
                f"opengauss-consumer-flow fixture contract is missing {fragment}"
            )
    if "/api/v1/system/engines" in fixture:
        raise RegistrationError(
            "opengauss-consumer-flow Business fixture must not own System Engine registration"
        )
    registration = (
        repository / "scripts/test/online-engine-registration.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        "/api/v1/system/engines",
        'test_result.get("success") is not True',
        "ADDP_ONLINE_CONSUMER_ENGINE_ID",
    ):
        if not source_contract_contains(registration, fragment, repository):
            raise RegistrationError(
                f"opengauss-consumer-flow registration helper is missing {fragment}"
            )
    owner = (
        repository / "scripts/test/relational-consumer-flow-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        '"opengauss": ConsumerProfile(',
        'namespace_kind="schema"',
        'identifier_quote=\'"\'',
        'fixture_script="business/scripts/online-opengauss-consumer-fixture.sh"',
        '"verification_owner": "deployment_profile"',
        '"schema_version": "addp.relational-consumer-flow-online/v1"',
        '"residual_resources": 0',
    ):
        if not source_contract_contains(owner, fragment, repository):
            raise RegistrationError(
                f"opengauss-consumer-flow owner contract is missing {fragment}"
            )
    if "/api/v1/system/engines" in owner or '"system.engine.read"' in owner:
        raise RegistrationError(
            "opengauss-consumer-flow consumer must not access the System Engine control plane"
        )
    identity_fixture = (
        repository / "system/backend/cmd/online-test-fixture/main.go"
    ).read_text(encoding="utf-8")
    for fragment in (
        'engineProvisionerRoleKey = "tenant.infrastructure_administrator"',
        "repository.GetActiveBuiltinRoleByKey(ctx, engineProvisionerRoleKey)",
        'RoleKey: "online." + strings.ReplaceAll(*suite, "-", "_")',
        "permissions, err := suitePermissions(*suite)",
        "PermissionKeys: permissions",
        '"ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN"',
    ):
        if not source_contract_contains(identity_fixture, fragment, repository):
            raise RegistrationError(
                f"opengauss-consumer-flow identity fixture is missing {fragment}"
            )
    consumer_role = re.search(r'(?ms)^var consumerPermissions = \[\]string\{(?P<body>.*?)^\}', identity_fixture)
    if consumer_role is None or re.search(r'"system\.engine(?:\.|_)[^"]*"', consumer_role.group('body')):
        raise RegistrationError(
            "opengauss-consumer-flow consumer role must not include System Engine permissions"
        )
    if "ADDP_ONLINE_FIXTURE_ADMIN_ACCESS_TOKEN" in identity_fixture:
        raise RegistrationError(
            "opengauss-consumer-flow must not issue an administrator fixture token"
        )


def validate_kingbase_consumer_flow_profile(
    repository: Path, registered: set[str]
) -> None:
    if "kingbase-consumer-flow" not in registered:
        return
    owner_gate = (
        repository / "scripts/test/online-owner-managed-kingbase-gate.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "# ADDP_ONLINE_SUITES=kingbase-consumer-flow",
        "# ADDP_ONLINE_RUNNER=owner-managed-linux-x86_64",
        "ADDP_ONLINE_OWNER_MANAGED",
        "kingbase_load_owner_environment",
        "kingbase_validate_license_input",
        "bash business/scripts/online-kingbase-consumer-fixture.sh start",
        "bash business/scripts/online-kingbase-consumer-fixture.sh stop",
        "scripts/test/online-engine-registration.py",
        'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        "media_sha256=",
        "license_sha256=",
    ):
        if not source_contract_contains(owner_gate, fragment, repository):
            raise RegistrationError(
                f"kingbase-consumer-flow owner-managed profile is missing {fragment}"
            )
    for relative in (
        "business/scripts/online-kingbase-consumer-fixture.sh",
        "scripts/lib/kingbase-official-media.sh",
        "scripts/test/online-owner-managed-kingbase-gate.sh",
        "scripts/test/online-owner-managed-kingbase-gate_test.py",
        "scripts/test/online-kingbase-consumer-fixture_test.py",
        "scripts/test/online-engine-registration.py",
        "scripts/test/relational-consumer-flow-online.py",
        "scripts/test/relational-consumer-flow-online_test.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"kingbase-consumer-flow requires {relative}")
    fixture = (
        repository / "business/scripts/online-kingbase-consumer-fixture.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "kingbase_ensure_official_image",
        "kingbase_validate_license_input",
        "docker create",
        "docker rm --force",
        "docker image rm",
        "addp-kingbase-online-disposable",
        "addp_kingbase_online",
        "addp_online_consumer_source",
        "addp_online_consumer_target",
        "ON CONFLICT (id) DO UPDATE",
        '"engine_type": "kingbase"',
        "start|advance|stop|status",
    ):
        if not source_contract_contains(fixture, fragment, repository):
            raise RegistrationError(
                f"kingbase-consumer-flow fixture contract is missing {fragment}"
            )
    owner = (
        repository / "scripts/test/relational-consumer-flow-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        '"kingbase": ConsumerProfile(',
        'engine_type="kingbase"',
        'namespace_kind="schema"',
        'fixture_script="business/scripts/online-kingbase-consumer-fixture.sh"',
        '"schema_version": "addp.relational-consumer-flow-online/v1"',
        '"residual_resources": 0',
    ):
        if not source_contract_contains(owner, fragment, repository):
            raise RegistrationError(
                f"kingbase-consumer-flow owner contract is missing {fragment}"
            )
    if "/api/v1/system/engines" in owner:
        raise RegistrationError(
            "kingbase-consumer-flow consumer must not access the System Engine control plane"
        )


def validate_transfer_insert_only_mysql_profile(
    repository: Path, registered: set[str]
) -> None:
    if "transfer-insert-only-mysql" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(
        encoding="utf-8"
    )
    required_fragments = (
        "transfer-insert-only-mysql)",
        "SYSTEM_URL GATEWAY_URL META_URL TRANSFER_URL CONSOLE_URL",
        "ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_ID",
        "ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_NAME",
        "bash business/scripts/online-transfer-insert-only-fixture.sh start",
        "bash business/scripts/online-transfer-insert-only-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
        "playwright install chromium",
    )
    missing = [fragment for fragment in required_fragments if not source_contract_contains(host_gate, fragment, repository)]
    if missing:
        raise RegistrationError(
            "transfer-insert-only-mysql profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-transfer-insert-only-fixture.sh",
        "scripts/test/transfer-insert-only-mysql-online.py",
        "scripts/test/online-transfer-insert-only-fixture_test.py",
        "scripts/test/transfer-insert-only-mysql-online_test.py",
        "console/frontend/playwright.online.config.js",
        "console/frontend/e2e/online/transfer-insert-only-mysql.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"transfer-insert-only-mysql requires {relative}")
    environment_example = (repository / ".env.example").read_text(encoding="utf-8")
    for variable in (
        "ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_ID",
        "ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_NAME",
        "ADDP_ONLINE_TRANSFER_MYSQL_PORT",
        "ADDP_ONLINE_TRANSFER_MYSQL_DATABASE",
        "ADDP_ONLINE_TRANSFER_MYSQL_USER",
        "ADDP_ONLINE_TRANSFER_MYSQL_PASSWORD",
        "ADDP_ONLINE_TRANSFER_MYSQL_ROOT_PASSWORD",
    ):
        if variable not in environment_example:
            raise RegistrationError(
                f"transfer-insert-only-mysql environment template is missing {variable}"
            )
    fixture = (
        repository / "business/scripts/online-transfer-insert-only-fixture.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "online-engine-fixture.sh",
        "business-mysql",
        "addp_online_transfer_insert_only_source",
        "addp_online_transfer_insert_only_target",
        "start|advance|verify|stop|status",
        "REVOKE ALL PRIVILEGES, GRANT OPTION",
        "GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, INDEX",
        "target rows do not prove insert-only semantics",
        "DECIMAL(6,2)",
    ):
        if not source_contract_contains(fixture, fragment, repository):
            raise RegistrationError(
                f"transfer-insert-only-mysql fixture contract is missing {fragment}"
            )
    owner = (
        repository / "scripts/test/transfer-insert-only-mysql-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        "/api/v1/meta/scan/run/manual",
        "transfer-insert-only-mysql.spec.js",
        "initial_records_written",
        "incremental_records_written",
        "old_update_ignored",
        "cleanup_tasks(client",
        '"residual_resources": 0',
    ):
        owner_contract = owner + (
            repository / "scripts/test/security-transfer-protection-online.py"
        ).read_text(encoding="utf-8")
        if not source_contract_contains(owner_contract, fragment, repository):
            raise RegistrationError(
                f"transfer-insert-only-mysql owner contract is missing {fragment}"
            )
    browser = (
        repository / "console/frontend/e2e/online/transfer-insert-only-mysql.spec.js"
    ).read_text(encoding="utf-8")
    for fragment in (
        "task-recommend-decimal-definitions",
        "input[value=\"insert_only\"]",
        "change_detection?.tie_breaker).toEqual([])",
        "initial_records_written: 6",
        "incremental_records_written: 1",
        "old_update_ignored: true",
        "controlFixture(repository, 'verify')",
        "task_deleted: true",
    ):
        if not source_contract_contains(browser, fragment, repository):
            raise RegistrationError(
                f"transfer-insert-only-mysql browser contract is missing {fragment}"
            )


def validate_transfer_relational_sql_etl_profile(repository: Path, registered: set[str]) -> None:
    if "transfer-relational-sql-etl" not in registered:
        return
    suite = load_suite_registry(repository)["transfer-relational-sql-etl"]
    if not {("manager", "MANAGER_URL"), ("develop", "DEVELOP_URL"), ("orchestrator", "ORCHESTRATOR_URL"), ("monitor", "MONITOR_URL"), ("security", "SECURITY_URL")}.issubset(suite.services):
        raise RegistrationError("transfer-relational-sql-etl must preflight Manager, Develop, Orchestrator, Monitor and Security")
    hosted_path = repository / "scripts/test/online-hosted-transfer-gate.sh"
    if not hosted_path.is_file():
        raise RegistrationError("transfer-relational-sql-etl requires Hosted lifecycle")
    hosted = hosted_path.read_text(encoding="utf-8")
    for fragment in (
        'source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
        'for start_target in -security -transfer -manager -develop -orchestrator -monitor', 'CONSOLE_URL=', 'SECURITY_URL=',
        'bash business/scripts/online-transfer-relational-sql-etl-fixture.sh start',
        'bash business/scripts/online-transfer-relational-sql-etl-fixture.sh stop',
        'bash business/scripts/online-transfer-relational-sql-etl-fixture.sh verify',
        '--suite transfer-relational-sql-etl', 'scripts/test/online-engine-registration.py',
        'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
        'export ADDP_ONLINE_TEST_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"',
        'HOSTED_FIXTURE_CONTAINERS=(addp-transfer-online-disposable addp-transfer-mongodb-online-disposable addp-transfer-doris-online-disposable addp-transfer-clickhouse-online-disposable addp-transfer-upload-online-disposable)',
        'ADDP_ONLINE_FIXTURE_MONGODB_ENGINE_DESCRIPTOR_FILE',
        'export ADDP_ONLINE_TEST_MONGODB_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"',
        'transfer-minio-engine.json',
        'ADDP_ONLINE_TRANSFER_MINIO_ENGINE_ID',
        'playwright install --with-deps chromium',
        'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
    ):
        if not source_contract_contains(hosted, fragment, repository):
            raise RegistrationError(f"transfer-relational-sql-etl Hosted profile is missing {fragment}")
    host = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    if "transfer-relational-sql-etl" in host:
        raise RegistrationError("transfer-relational-sql-etl must not keep a self-hosted route")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
    job = re.search(r"(?ms)^  transfer-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
            or "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'transfer-relational-sql-etl'\n" not in job.group("body")
            or "ONLINE_SUITE_INPUT: transfer-relational-sql-etl" not in job.group("body")):
        raise RegistrationError("transfer-relational-sql-etl requires a manual Ubuntu Hosted job")
    if "&& inputs.suite != 'transfer-relational-sql-etl'" not in workflow:
        raise RegistrationError("transfer-relational-sql-etl must not also dispatch on self-hosted")
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    aggregate = re.search(r"(?ms)^test-online-runner:[^\n]*\n(?P<body>.*?)(?=^[^\t\n#]|\Z)", makefile)
    owner_gate = re.search(r"(?ms)^test-transfer-online-runner:[^\n]*\n(?P<body>(?:\t[^\n]*\n)+)", makefile)
    if (aggregate is None or not source_contract_contains(make_target_commands(makefile, "test-online-runner"), "$(MAKE) test-transfer-online-runner", repository)
            or owner_gate is None or "scripts/test/online-hosted-transfer-gate_test.py" not in make_target_commands(makefile, "test-transfer-online-runner")):
        raise RegistrationError("transfer-relational-sql-etl Hosted regression must enter test-online-runner")
    contracts = {
        "Makefile": ("go test ./internal/planner -run '^TestHostedWriteFailureTaskContract$$' -count=1",),
        "transfer/backend/internal/planner/online_write_failure_contract_test.go": ("TestHostedWriteFailureTaskContract", "ParseTableExportTaskSpec", "module.write_failure_task"),
        "scripts/test/transfer-relational-sql-etl-online.py": (
            '"manager.data_item.read"', '"manager.content.read"', '"meta.lineage.read"',
            "evolve_fixture", "verify_unproven_schema", "verify_historical_schema", "history_before_write", "history_after_write", "addp.transfer-relational-sql-etl-online/v17",
            "mongodb_source_access", "addp.mongodb-source-access/v1", "ExactRecordGrants", "rejected_before_read_write",
            "verify_scan_trace_visibility", "record_scan_trace_receipt", "scan-trace-visibility.json", "addp.scan-trace-visibility/v1", "parent_read_does_not_grant_scan_read", "peer_hidden_endpoints", "owner_visible_observations", "peer_denials",
            "run_object_upload", "validate_object_upload_facts", "manager-object-upload.json", "addp.manager-object-upload/v2", "single_source_facts_verified", "multi_physical_size_verified", "ADDP_ONLINE_TRANSFER_MINIO_ENGINE_ID",
            "read_object_upload_meta", "verify_upload_visibility", "ExactFileMetadataGrants", "detail_404_after_revoke",
            "record_manager_import_receipt", "manager-private-import.json", "addp.manager-private-import/v3", "validate_manager_import_browser", "metadata_wait_verified", "failure_ui_verified", "physical_geometry_verified", "user_inspect_rejected", "run_manager_import", "current_user_origin_verified", "automatic_scan_verified", "non_uploader_rejected", "ADDP_ONLINE_IMPORT_OUTSIDER_ACCESS_TOKEN", '"manager.data_item.create"',
            "run_wide_lineage", "validate_wide_graph", "WIDE_FIELDS", "manager_wide_field_graph_verified", "config_checks",
            "run_write_failures", "validate_write_failure_execution", "validate_write_failure_physical", "write_failure_details_verified", "ADDP_ONLINE_TRANSFER_WRITE_FAILURES",
            "run_manager_import_with_grant", "import_grant_history", "hidden_after_revoke", "uploader_preview_verified",
            "run_binary_roundtrip", "validate_binary_physical", "validate_binary_worker_events", "clickhouse-binary-roundtrip.json", "task_deleted_and_read_back_404",
            "initialize_exact_record_read_grants", "source_read_denied_after_revoke", "wait_transfer_read_boundary(tenant_id)",
            "run_multi_source_lineage", "validate_multi_source_execution", "multiple_origins_verified", "cte_join_union_verified",
            "run_orchestrated_lineage", "validate_orchestrated_child", "canonical_table_locator", "validate_query_facts", "wait_resource_chain", "cleanup_definitions", "three_hop_verified",
            "run_mongodb_lineage", "validate_mongodb_execution", "ADDP_ONLINE_TEST_MONGODB_ENGINE_ID", "automatic_collection_verified", "rerun_verified",
            "run_native_lineage", "wait_field_graph", "execution_schema_hashes", "validate_graph_snapshots", "generated_label", '"_replace"', '"_hop"',
            '"residual_resources": 0', "cleanup_tasks(client", "owned_task_names", "schema_snapshot_hash",
            "addp.transfer-relational-sql-etl-browser/v7",
        ),
        "business/scripts/online-transfer-relational-sql-etl-fixture.sh": (
            "object-facts", "start_minio", "addp.manager-object-physical/v1", "addp-transfer-upload-online-disposable",
            "ADDP_ONLINE_HOSTED", "GITHUB_ACTIONS", "NATIVE_TARGET", "NATIVE_DOWNSTREAM", "DROP TABLE IF EXISTS public.${NATIVE_TARGET}", "read-only configuration check created a target table",
            "DROP TABLE IF EXISTS public.${NATIVE_DOWNSTREAM}", "numeric_precision, numeric_scale", "generated_label",
            "WIDE_SOURCE", "WIDE_DOWNSTREAM", "generate_series(0,499)", "EXCEPT ALL", "wide target complete row multiset differs",
            "start_write_failure_targets", "verify_write_failures", "addp-transfer-doris-online-disposable", "addp-transfer-clickhouse-online-disposable", "enable_insert_strict=true", "PARTITION BY RANGE(id)", "CONSTRAINT fixture_label_length",
            "binary-roundtrip", "read_binary_roundtrip", "addp:type=bytes", "octet_length(payload)", "payload IS NULL",
            "verify_manager_import", "manager-import", "MULTI_TARGET", "multi-source CTE/JOIN/UNION rows differ", "DROP TABLE IF EXISTS public.${MULTI_TARGET}",
            "DIM_TARGET", "DWD_TARGET", "DIM rows differ", "DWD rows differ",
            "RENAME COLUMN person_nickname TO person_display_name", "TYPE timestamp without time zone USING activity_date::timestamp", "evolved DWD columns differ",
            "MONGODB_TARGET", "mongo:7.0", "--tmpfs /data/db --tmpfs /data/configdb", "leader_nickname_snapshot",
            "container remains after cleanup", "container ownership mismatch", "--tmpfs /var/lib/postgresql/data",
        ),
        "console/frontend/e2e/online/transfer-relational-sql-etl.spec.js": (
            "read private import initial authorization", "visibilityDeadline",
            "addp.manager-private-import-browser/v1", "browserAPIFromResponse", "manager-import-console-success.png", "manager-import-console-rejection.png", "upload_ui_verified", "metadata_wait_verified", "failure_ui_verified",
            "/manager/data-explorer", "field_ref", "schema_snapshot_hash", "lineage-fields", "lineage-canvas", "lineageFieldConnections",
            "verifyTaskConfigChecks", "taskExecutionIdentities", "/api/v1/transfer/task-config-checks", "json_representation_not_proven", "constant_encoding_invalid", "forbiddenRequests", "config_checks: configChecks",
            "verifyWideLineage", "manager_wide_field_graph_verified: true", "expect(graph.nodes).toHaveLength(1500)", "expect(graph.edges).toHaveLength(1000)", "expect(requests).toBe(1)",
            "manager_evolved_schema_verified: true", "schema_evolution.history_verified", "expect(chain.rounds).toHaveLength(3)",
            "manager_orchestrated_field_graph_verified: true", "ADDP_ONLINE_ORCHESTRATED_FIELD_LINEAGE", "transfer-orchestrated-dwd-field-lineage.png",
            "verifyWriteFailureDetails", "write_failure_details_verified: writeFailureDetailsVerified", "ADDP_ONLINE_TRANSFER_WRITE_FAILURES",
            "manager_mongodb_field_graph_verified: true", "ADDP_ONLINE_TRANSFER_MONGODB_FIELD_LINEAGE", "transfer-mongodb-ods-field-lineage.png", "expect(mongodbGraphRequests).toBe(1)",
            "manager_field_graph_verified: true", "query_field_lineage_verified: true", "expect(query.graph.field_lineage_status).toBe('complete')", "expect(query.focused.edges).toHaveLength(1)",
            "addp.transfer-relational-sql-etl-browser/v7",
        ),
        "scripts/test/transfer-relational-sql-etl-online_test.py": ("test_readonly_config_checks_require_complete_proofs_and_zero_side_effects", "test_multi_source_frozen_evidence_rejects_partial_or_guessed_origins", "test_multi_source_scenario_preserves_inputs_parameters_and_exact_graph_origins", "test_rejects_wrong_fields_versions_and_execution_proofs", "test_mongodb_reruns_one_task_and_requires_all_latest_automatic_proofs", "test_chain_reruns_same_definition_and_only_observes_latest_owner_proofs"),
        "scripts/test/online-hosted-transfer-gate_test.py": ("test_failures_destroy_owned_resources", "ADDP_ONLINE_TRANSFER_CHECK_USER_USERNAME"),
        "system/backend/cmd/online-test-fixture/main.go": ("grantTransferConfigCheckEngineReader", "ADDP_ONLINE_TRANSFER_CHECK_USER_PASSWORD"),
        "system/backend/cmd/online-test-fixture/main_test.go": ("TestTransferPrecheckFixtureKeepsConsumerSeparateAndReissuesSession",),
        "scripts/test/online-transfer-relational-sql-etl-fixture_test.py": ("test_readonly_config_check_target_must_remain_absent", "test_rejects_incorrect_multi_source_query_rows", "test_rejects_native_rows", "test_partial_mongodb_initialization_is_redacted_and_cleans_both_containers"),
    }
    for relative, fragments in contracts.items():
        path = repository / relative
        if not path.is_file():
            raise RegistrationError(f"transfer-relational-sql-etl requires {relative}")
        content = path.read_text(encoding="utf-8")
        for fragment in fragments:
            if not source_contract_contains(content, fragment, repository):
                raise RegistrationError(f"transfer-relational-sql-etl {relative} contract is missing {fragment}")


def load_workflow_suites(
    repository: Path, nightly_suites: set[str]
) -> set[str]:
    path = repository / ".github/workflows/online-t4-gates.yml"
    if not path.is_file():
        raise RegistrationError(f"{path.relative_to(repository)} is missing")
    text = path.read_text(encoding="utf-8")
    required_fragments = (
        "workflow_dispatch:",
        "permissions:\n  contents: read",
        "- self-hosted",
        "- macOS",
        "- addp-online",
        "environment: addp-online",
        "ADDP_ONLINE_ENV_FILE: ${{ vars.ADDP_ONLINE_ENV_FILE }}",
        "bash scripts/test/online-host-gate.sh --check-only",
        "bash scripts/test/online-host-gate.sh",
        "actions/upload-artifact@",
    )
    missing = [fragment for fragment in required_fragments if not source_contract_contains(text, fragment, repository)]
    if missing:
        raise RegistrationError("Online T4 workflow is missing: " + ", ".join(missing))
    jobs_text = text.split("\njobs:\n", 1)
    if len(jobs_text) != 2:
        raise RegistrationError("Online T4 workflow jobs are missing")
    job_blocks = re.findall(
        r"(?ms)^  ([a-z][a-z0-9-]*):\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)",
        jobs_text[1],
    )
    self_hosted_jobs = [
        (job_name, body)
        for job_name, body in job_blocks
        if re.search(r"(?m)^      - self-hosted\s*$", body)
    ]
    if not self_hosted_jobs:
        raise RegistrationError("Online T4 workflow has no self-hosted job")
    for job_name, body in self_hosted_jobs:
        accepts_manual_dispatch = "github.event_name == 'workflow_dispatch'" in body
        accepts_fixed_schedule = (
            "github.event_name == 'schedule'" in body
            and re.search(
                r"(?m)^      ONLINE_SUITE_INPUT: [a-z][a-z0-9-]*$", body
            )
            is not None
        )
        if not accepts_manual_dispatch and not accepts_fixed_schedule:
            raise RegistrationError(
                f"self-hosted Online T4 job {job_name} must only accept "
                "workflow_dispatch or a fixed schedule"
            )
        if "${{ secrets." in body:
            raise RegistrationError(
                f"self-hosted Online T4 job {job_name} must not receive repository secrets"
            )
        for action in re.findall(r"(?m)^\s*- uses:\s*([^\s]+)\s*$", body):
            if action.startswith("./"):
                continue
            _, separator, revision = action.rpartition("@")
            if not separator or re.fullmatch(r"[0-9a-f]{40}", revision) is None:
                raise RegistrationError(
                    f"self-hosted Online T4 job {job_name} action must use an immutable commit SHA: {action}"
                )
    hosted_gates = sorted(
        (repository / "scripts/test").glob("online-hosted-*-gate.sh")
    )
    owner_managed_gates = sorted(
        (repository / "scripts/test").glob("online-owner-managed-*-gate.sh")
    )
    for external_gate in hosted_gates + owner_managed_gates:
        relative = external_gate.relative_to(repository).as_posix()
        lifecycle_flag = (
            'ADDP_ONLINE_HOSTED: "1"'
            if external_gate in hosted_gates
            else 'ADDP_ONLINE_OWNER_MANAGED: "1"'
        )
        for fragment in (
            f"bash {relative} --check-only",
            f"bash {relative}",
            lifecycle_flag,
            "ADDP_ONLINE_SECRET_DIR:",
        ):
            if not source_contract_contains(text, fragment, repository):
                raise RegistrationError(
                    f"Online T4 workflow is missing external profile fragment: {fragment}"
                )
    if (hosted_gates or owner_managed_gates) and "node-version-file: .node-version" not in text:
        raise RegistrationError(
            "Online T4 external profiles must use the repository .node-version"
        )
    job_environment_blocks = re.findall(
        r"(?ms)^    env:\n(?P<body>(?:      [^\n]*\n)+)",
        text,
    )
    if any("${{ runner." in block for block in job_environment_blocks):
        raise RegistrationError(
            "Online T4 workflow must not use the step-only runner context in job-level env"
        )
    artifact_assignment = (
        "          ADDP_ONLINE_ARTIFACT_DIR: "
        "${{ runner.temp }}/addp-online-${{ github.run_id }}"
    )
    expected_artifact_assignments = 2 * (
        1 + len(hosted_gates) + len(owner_managed_gates)
    )
    if text.splitlines().count(artifact_assignment) != expected_artifact_assignments:
        raise RegistrationError(
            "Online T4 workflow must configure the Runner temp artifact directory "
            "on both lifecycle steps for every Runner profile"
        )
    has_schedule = re.search(r"(?m)^  schedule:\s*$", text) is not None
    if has_schedule and not nightly_suites:
        raise RegistrationError(
            "Online T4 workflow must remain manual until the first real run passes"
        )
    if nightly_suites and not has_schedule:
        raise RegistrationError(
            "Online nightly suite registration requires a workflow schedule"
        )
    if has_schedule:
        cron_values = re.findall(
            r"(?m)^    - cron: [\"']([^\"']+)[\"']$", text
        )
        if len(cron_values) != 1 or re.fullmatch(
            r"(?:[0-5]?\d) (?:[01]?\d|2[0-3]) \* \* \*", cron_values[0]
        ) is None:
            raise RegistrationError(
                "Online T4 nightly schedule must contain exactly one daily UTC cron"
            )
        scheduled_workflow_suites: set[str] = set()
        for job_name, body in job_blocks:
            if "github.event_name == 'schedule'" in body:
                suite = re.search(
                    r"(?m)^      ONLINE_SUITE_INPUT: ([a-z][a-z0-9-]*)$", body
                )
                if suite is None:
                    raise RegistrationError(
                        f"scheduled Online T4 job {job_name} must set a fixed "
                        "ONLINE_SUITE_INPUT"
                    )
                suite_name = suite.group(1)
                if f"group: online-t4-{suite_name}" not in body:
                    raise RegistrationError(
                        f"scheduled Online T4 job {job_name} must use its fixed "
                        "suite concurrency group"
                    )
                scheduled_workflow_suites.add(suite_name)
            elif "github.event_name == 'workflow_dispatch'" not in body:
                raise RegistrationError(
                    f"Online T4 job {job_name} must explicitly exclude schedule events"
                )
        if scheduled_workflow_suites != nightly_suites:
            raise RegistrationError(
                "Online workflow nightly suites "
                f"{sorted(scheduled_workflow_suites)} do not match registered nightly "
                f"suites {sorted(nightly_suites)}"
            )
    options = re.search(
        r"(?m)^        options:\n(?P<body>(?:          - [a-z][a-z0-9-]*\n)+)",
        text,
    )
    if options is None:
        raise RegistrationError("Online T4 workflow suite choices are missing")
    return set(re.findall(r"(?m)^          - ([a-z][a-z0-9-]*)$", options.group("body")))


def validate_metric_engine_variant(repository: Path, registered: set[str]) -> None:
    if "metric-service-revision-lifecycle" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
    selection = re.search(
        r"(?ms)^      metric_engine:\n(?P<body>.*?)(?=^permissions:|\Z)", workflow
    )
    if selection is None:
        raise RegistrationError("metric Online T4 engine choice is missing")
    choice = selection.group("body")
    options = re.search(r"(?m)^        options:\n(?P<body>(?:          - [a-z]+\n)+)", choice)
    if ("        type: choice\n" not in choice
            or "        required: true\n" not in choice
            or "        default: postgresql\n" not in choice
            or options is None
            or re.findall(r"(?m)^          - ([a-z]+)$", options.group("body")) != ["postgresql", "tidb"]):
        raise RegistrationError("metric Online T4 must select PostgreSQL or TiDB explicitly")
    job = re.search(r"(?ms)^  metric-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None:
        raise RegistrationError("Hosted metric T4 job is missing")
    body = job.group("body")
    required = (
        "    if: github.event_name == 'schedule' || (github.event_name == 'workflow_dispatch' && inputs.suite == 'metric-service-revision-lifecycle')\n",
        "      fail-fast: false\n",
        "        metric_engine: ${{ fromJSON(github.event_name == 'schedule' && '[\"postgresql\",\"tidb\"]' || format('[\"{0}\"]', inputs.metric_engine)) }}\n",
        "      group: online-t4-metric-service-revision-lifecycle-${{ matrix.metric_engine }}\n",
        "      ADDP_ONLINE_METRIC_ENGINE_TYPE: ${{ matrix.metric_engine }}\n",
        "          name: online-t4-metric-service-revision-lifecycle-${{ matrix.metric_engine }}-${{ github.run_id }}\n",
    )
    if any(not source_contract_contains(body, fragment, repository) for fragment in required):
        raise RegistrationError("Hosted metric T4 must schedule both engines and dispatch only the selected engine")


def validate_platform_node_metrics_profile(repository: Path, registered: set[str]) -> None:
    if "platform-node-metrics" not in registered:
        return
    for relative in (
        "scripts/test/platform-node-metrics-online.py", "scripts/test/platform-node-metrics-online_test.py",
        "scripts/test/platform-node-metrics-fixture.py", "scripts/test/platform-node-metrics-fixture_test.py",
        "scripts/test/online-hosted-node-metrics-gate_test.py",
        "system/backend/cmd/online-test-fixture/platform_metrics.go",
        "system/backend/cmd/online-test-fixture/platform_metrics_test.go",
        "scripts/infra/node-metrics.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"platform-node-metrics requires {relative}")
    makefile = (repository / "Makefile").read_text()
    owned = re.search(r"(?ms)^test-node-metrics-online-runner:[^\n]*\n(?P<body>.*?)(?=^[^\t\n#]|\Z)", makefile)
    aggregate = re.search(r"(?ms)^test-online-runner:[^\n]*\n(?P<body>.*?)(?=^[^\t\n#]|\Z)", makefile)
    if owned is None or aggregate is None or not source_contract_contains(make_target_commands(makefile, "test-online-runner"), "$(MAKE) test-node-metrics-online-runner", repository):
        raise RegistrationError("platform-node-metrics requires its owner regression in test-online-runner")
    for filename in ("platform-node-metrics-online_test.py", "platform-node-metrics-fixture_test.py", "online-hosted-node-metrics-gate_test.py"):
        if filename not in make_target_commands(makefile, 'test-node-metrics-online-runner'):
            raise RegistrationError(f"platform-node-metrics regression is missing {filename}")
    gate = (repository / "scripts/test/online-hosted-node-metrics-gate.sh").read_text()
    for fragment in ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', "scripts/infra/node-metrics.py up",
                     "scripts/infra/node-metrics.py down", "--suite platform-node-metrics", 'modules=(-monitor -gateway)',
                     'if [ "$ADDP_ONLINE_NODE_METRICS_CASE" = host-admission ] || [ "$ADDP_ONLINE_NODE_METRICS_CASE" = engine-admission ] || [ "$ADDP_ONLINE_NODE_METRICS_CASE" = redis-engine-admission ]; then modules+=(-system); fi',
                     'scripts/dev/start.sh "${modules[@]}"',
                     'run_logged python3 scripts/test/platform-node-metrics-fixture.py standard-cleanup || status=1',
                     'make test-online "ONLINE_SUITE=$ONLINE_SUITE"'):
        if not source_contract_contains(gate, fragment, repository):
            raise RegistrationError(f"platform-node-metrics lifecycle is missing {fragment}")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  node-metrics-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'platform-node-metrics'\n" not in job.group("body"):
        raise RegistrationError("platform-node-metrics requires a manual Hosted T4 job")
    if "&& inputs.suite != 'platform-node-metrics'" not in workflow:
        raise RegistrationError("platform-node-metrics must not dispatch on the self-hosted deployment")
    selection = re.search(r"(?ms)^      node_metrics_case:\n(?P<body>.*?)(?=^      [a-z_]+:\n|^permissions:|\Z)", workflow)
    choice = selection.group("body") if selection else ""
    options = re.search(r"(?m)^        options:\n(?P<body>(?:          - [a-z-]+\n)+)", choice)
    if ("        type: choice\n" not in choice or "        required: true\n" not in choice
            or "        default: regression\n" not in choice or options is None
            or re.findall(r"(?m)^          - ([a-z-]+)$", options.group("body")) != ["regression", "complete-comparison", "cpu-pressure", "host-admission", "engine-admission", "redis-engine-admission", "meta-workers", "transfer-workers", "quality-workers", "service-availability", "standard-startup"]
            or "      ADDP_ONLINE_NODE_METRICS_CASE: ${{ inputs.node_metrics_case }}\n" not in job.group("body")
            or "regression|complete-comparison|cpu-pressure|host-admission|engine-admission|redis-engine-admission|meta-workers|transfer-workers|quality-workers|service-availability|standard-startup)" not in gate):
        raise RegistrationError("platform-node-metrics must select a closed, bounded acceptance case")


def validate_orchestrator_execution_profile(repository: Path, registered: set[str]) -> None:
    if "orchestrator-execution" not in registered:
        return
    gate = repository / "scripts/test/online-hosted-orchestrator-gate.sh"
    required_files = (
        "scripts/test/orchestrator-execution-online.py",
        "scripts/test/orchestrator-execution-online_test.py",
        "scripts/test/orchestrator-execution-faults.py",
        "scripts/test/orchestrator-execution-faults_test.py",
        "scripts/test/online-hosted-orchestrator-gate_test.py",
        "business/scripts/online-metric-postgres-fixture.sh",
    )
    for relative in required_files:
        if not (repository / relative).is_file():
            raise RegistrationError(f"orchestrator-execution requires {relative}")
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    owned = re.search(r"(?ms)^test-orchestrator-online-runner:[^\n]*\n(?P<body>.*?)(?=^[^\t\n#]|\Z)", makefile)
    aggregate = re.search(r"(?ms)^test-online-runner:[^\n]*\n(?P<body>.*?)(?=^[^\t\n#]|\Z)", makefile)
    if owned is None or aggregate is None or not source_contract_contains(makefile, "$(MAKE) test-orchestrator-online-runner", repository):
        raise RegistrationError("orchestrator-execution requires its owner T1 entry in test-online-runner")
    for test in ("orchestrator-execution-online_test.py", "orchestrator-execution-faults_test.py", "online-hosted-orchestrator-gate_test.py"):
        if test not in make_target_commands(makefile, "test-orchestrator-online-runner"):
            raise RegistrationError(f"orchestrator-execution owner T1 entry is missing {test}")
    text = gate.read_text(encoding="utf-8")
    for fragment in (
        'source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
        "for start_target in -meta -orchestrator -monitor",
        "bash business/scripts/online-metric-postgres-fixture.sh start",
        "bash business/scripts/online-metric-postgres-fixture.sh stop",
        "--suite orchestrator-execution",
        "orchestrator-execution-faults.py alias-add",
        "orchestrator-execution-faults.py alias-remove",
        'HTTP_PROXY="$ADDP_ONLINE_ORCHESTRATOR_PROXY_URL"',
        "SERVICE_HOST=addp-orchestrator-meta.test",
        "python3 scripts/test/online-engine-registration.py",
        'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
    ):
        if not source_contract_contains(text, fragment, repository):
            raise RegistrationError(f"orchestrator-execution Hosted profile is missing {fragment}")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
    job = re.search(r"(?ms)^  orchestrator-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'orchestrator-execution'\n" not in job.group("body"):
        raise RegistrationError("orchestrator-execution must remain a manual Hosted T4 job before its first real pass")
    if "&& inputs.suite != 'orchestrator-execution'" not in workflow:
        raise RegistrationError("orchestrator-execution must not also dispatch on the self-hosted deployment")


def validate_quality_dynamic_binding_profile(repository: Path, registered: set[str]) -> None:
    if "quality-dynamic-binding" not in registered:
        return
    host = (repository / "scripts/test/online-host-gate.sh").read_text()
    if re.search(r"(?m)^  quality-dynamic-binding\)", host):
        raise RegistrationError("Quality must have one Hosted profile, without the removed self-hosted route")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  quality-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "runs-on: ubuntu-24.04" not in job.group("body") or (
        "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'quality-dynamic-binding'\n" not in job.group("body")
    ):
        raise RegistrationError("Quality requires a manual Ubuntu Hosted T4 job")
    if "&& inputs.suite != 'quality-dynamic-binding'" not in workflow:
        raise RegistrationError("Quality must not also dispatch on self-hosted")
    required = {
        "scripts/test/online-hosted-quality-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', "--suite quality-dynamic-binding",
            "-model -meta -quality -orchestrator -monitor", "online-quality-postgres-fixture.py stop",
            "online-quality-postgres-fixture.py seed", "online-quality-postgres-fixture.py verify",
            "-m scripts.test.quality-online-fixture prepare", "-m scripts.test.quality-online-fixture scan",
            "unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN", "unset ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN",
            'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        ),
        "business/scripts/online-quality-postgres-fixture.py": (
            "container ownership mismatch", "container remains after cleanup", "source data changed",
            "type=tmpfs,destination=/var/lib/postgresql/data",
        ),
        "scripts/test/quality-online-fixture.py": ("/materialized-target", "different Users", "uncertain_mutation"),
        "system/backend/cmd/online-test-fixture/main.go": ('case "quality-dynamic-binding":', 'values["ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN"]'),
        "Makefile": ("$(MAKE) test-quality-online-runner", "scripts/test/quality-online-fixture_test.py",
                     "scripts/test/online-quality-postgres-fixture_test.py", "scripts/test/online-hosted-quality-gate_test.py",
                     "scripts/ci/check-quality-online-ci-registration_test.py"),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f"Quality Hosted owner contract is incomplete: {relative}")


def validate_vector_pmtiles_profile(repository: Path, registered: set[str]) -> None:
    if "vector-pmtiles-workflow" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  vector-pmtiles-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    suite = load_suite_registry(repository)["vector-pmtiles-workflow"]
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-pmtiles-workflow'" not in job.group("body")
        or "&& inputs.suite != 'vector-pmtiles-workflow'" not in workflow or suite.nightly
        or ("security", "SECURITY_URL") not in suite.services):
        raise RegistrationError("PMTiles requires a manual isolated Hosted job and Security preflight")
    required = {
        "scripts/test/online-hosted-vector-pmtiles-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite vector-pmtiles-workflow',
            '--wait-protection-owners develop,manager',
            'start_online_geopython_runtime', 'for start_target in -security -develop -manager -monitor',
            'business.scripts.online-vector-pmtiles-fixture stop', 'scripts/test/online-engine-registration.py',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'env -u POSTGRES_PASSWORD make test-online'),
        "scripts/utils/hosted-online.sh": ('stop_online_product_runtime || status=1',),
        "business/scripts/online-vector-pmtiles-fixture.py": (
            'shared.start_minio(', 'shared.owned(name, SUITE)', "com.addp.online-fixture=", 'validate_records(',
            "'vector_layers'", "header['min_zoom'] != 0", 'reader.get(5, 26, 13)', 'physical_worker('),
        "scripts/test/vector-pmtiles-workflow-online.py": (
            'validate_conflict(', 'before_scans != after_scans', 'before_graph != after_graph',
            "target['fingerprint'] != before_item['fingerprint']", "'meta_scan_runs'", 'wait_current_graph(',
            'validate_item(target, native)', 'browser_runner(repository, env, evidence)'),
        "console/frontend/e2e/online/vector-pmtiles-workflow.spec.js": (
            'login(', 'auth.principalID', "content.metadata.min_zoom).toBe(0)", "['cache-control']).toBe('no-store')",
            "value === 'no-store'", 'evidence.tile_sha256', 'evidence.feature_count', "getImageData("),
        "system/backend/cmd/online-test-fixture/main.go": ('case "vector-pmtiles-workflow":', 'return vectorPMTilesPermissions, nil'),
        "Makefile": ('@$(MAKE) test-vector-pmtiles-online-runner', 'test_online_pmtiles_fixture.py',
            'scripts/test/vector-pmtiles-workflow-online_test.py', 'scripts/test/online-vector-pmtiles-fixture_test.py',
            'scripts/test/online-hosted-vector-pmtiles-gate_test.py', 'test_pmtiles_requires_manual_isolation_content_and_cleanup'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f"PMTiles owner contract is incomplete: {relative}")



def validate_vector_topology_profile(repository: Path, registered: set[str]) -> None:
    if "vector-topology-persistence" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  topology-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-topology-persistence'" not in job.group("body")
        or "&& inputs.suite != 'vector-topology-persistence'" not in workflow
        or "nfs-kernel-server nfs-common" not in job.group("body")
        or load_suite_registry(repository)["vector-topology-persistence"].nightly):
        raise RegistrationError("topology requires a manual isolated Hosted NFS job")
    if ('      topology_case:' not in workflow or '          - line-network-normalize' not in workflow or '          - line-network-normalize-chains' not in workflow
        or 'ADDP_ONLINE_TOPOLOGY_CASE: ${{ inputs.topology_case }}' not in job.group('body')):
        raise RegistrationError('topology requires an explicit independently selected normalization case')
    platform = (repository/'.github/workflows/platform-ci.yml').read_text()
    platform_job = re.search(r'^  platform-consistency:\n(?P<body>.*?)(?=^  \S|\Z)', platform, re.M|re.S)
    body = platform_job.group('body') if platform_job else ''
    gate = body.find('run: make test-platform')
    for module in ('console','manager'):
        preparation = f'uses: ./.github/actions/prepare-frontend-gate\n        with:\n          frontend-directory: {module}/frontend'
        if gate < 0 or body.find(preparation) < 0 or body.find(preparation) > gate:
            raise RegistrationError('topology real discovery requires locked '+module+' dependencies before platform gate')
    normalize_required = {
        'scripts/test/online-hosted-topology-gate.sh': ('full|line-network-normalize', 'topology-case.json'),
        'scripts/test/vector-topology-persistence-online.py': ('run_normalize_scenario', 'normalize_definition',
            'validate_normalize_success', "schema+'/v1'", 'normalization validation failure must publish neither',
            "fixture.network_profile(case)['title']+'$'", "fixture.network_profile(case)['schema']+'-browser/v1'"),
        'business/scripts/online-vector-topology-fixture.py': ('normalize_source_features', 'normalize_expected_features',
            'normalize_worker', 'verify_normalize', 'occurrence_count', 'source_segment_no'),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('重合规整独立普通User双成果与字段拒绝',
            'normalizedMapExpectation', "'addp.topology-normalize'", 'normalization lineage'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_hosted_normalize_case_dual_save_independent_physical_oracle',),
        'scripts/test/vector-topology-persistence-online_test.py': ('test_real_playwright_discovery_selects_exactly_one_case_without_running_business',
            "[*args,'--list','--reporter=json']"),
    }
    for relative,fragments in normalize_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology normalization case contract incomplete: '+relative)
    buffer_required = {
        '.github/workflows/online-t4-gates.yml': ('          - multi-distance-buffer',),
        'scripts/test/online-hosted-topology-gate.sh': ('line-network-normalize-chains|multi-distance-buffer',),
        'business/scripts/online-vector-topology-fixture.py': ('def buffer_worker(', 'def verify_buffer(', 'def buffer_features(', 'BUFFER_SOURCES', 'buffer exact coordinates order attributes or types differ'),
        'scripts/test/vector-topology-persistence-online.py': ("'operator':'batch_buffer'", 'conflict_source', "cause='distance_conflict'"),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('多距缓冲独立普通User单成果与重名拒绝', "'multi-distance-buffer',{result:6},'addp.topology-buffer'", 'conflict_source:conflictSource'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_hosted_buffer_case_real_save_and_independent_oracle',),
        'scripts/test/vector-topology-persistence-online_test.py': ('test_buffer_uses_only_selected_sources_one_output_and_exact_conflict_diagnostic',),
    }
    for relative,fragments in buffer_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology buffer case contract incomplete: '+relative)
    diagnostics_required = {
        '.github/workflows/online-t4-gates.yml': ('          - operator-input-diagnostics',),
        'scripts/test/online-hosted-topology-gate.sh': ('multi-distance-buffer|operator-input-diagnostics',),
        'business/scripts/online-vector-topology-fixture.py': ('def diagnostic_worker(', 'DIAGNOSTIC_SOURCES', 'no physical publication'),
        'scripts/test/vector-topology-persistence-online.py': ('def run_diagnostic_scenario(', 'def validate_input_failure(', 'sixteen same-UUID observations', 'expected_input_diagnostic'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_hosted_input_diagnostic_sources_reach_exact_typed_failure_without_save',),
        'scripts/test/vector-topology-persistence-online_test.py': ('class InputDiagnosticOnlineTest', 'test_diagnostic_rejects_wrong_tuple_private_context_and_any_publication'),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('算子参数诊断独立普通User双语失败页', 'assertInputDiagnosticPanel', 'navigator.clipboard.readText()', 'await page.reload()', 'browserAPIFromResponse(response', 'observations).toHaveLength(16)'),
    }
    for relative,fragments in diagnostics_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology operator input diagnostics contract incomplete: '+relative)
    nodes_required = {
        '.github/workflows/online-t4-gates.yml': ('          - line-nodes',),
        'scripts/test/online-hosted-topology-gate.sh': ('line-nodes|line-trimming|full',),
        'business/scripts/online-vector-topology-fixture.py': ('def nodes_features(', 'def verify_nodes(', 'def nodes_worker(', 'NODES_FAILURES', 'def nodes_field_types('),
        'scripts/test/vector-topology-persistence-online.py': ('def nodes_definition(', 'def run_nodes_scenario(', 'def validate_nodes_failure(', 'def validate_nodes_browser_raw(', 'def validate_nodes_graph(', 'nodes_diagnostic(spec)'),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('节点提取独立普通User节点与来源明细双Point及九失败保护', 'topology-nodes-browser-raw.json', 'async function verifyNodesCase(', 'function assertNodesLineage(', 'function nodesPanelExpectation(', "output.port==='members'", 'assertProximityPointMap(renderer,expected)'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_hosted_line_nodes_dual_point_nine_failures_and_independent_oracle',),
        'scripts/test/vector-topology-persistence-online_test.py': ('class NodesOnlineTest', 'test_nodes_one_success_nine_failures_two_point_scans_and_exact_source_edges', 'test_nodes_raw_browser_rejects_point_incidence_ewkb_and_owner_locale_forgery'),
        'scripts/test/online-hosted-topology-gate_test.py': ('test_nodes_case_is_bound_without_old_business_or_capacity',),
        'scripts/test/online-vector-topology-fixture_test.py': ('class NodesFixtureTest',),
    }
    for relative,fragments in nodes_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology nodes case contract incomplete: '+relative)
    difference_required = {
        '.github/workflows/online-t4-gates.yml': ('          - geometry-difference',),
        'scripts/test/online-hosted-topology-gate.sh': ('geometry-difference|polygon-dissolve',),
        'business/scripts/online-vector-topology-fixture.py': ('DIFFERENCE_FAILURES', 'def difference_features(', 'def verify_difference(', 'def difference_worker(', 'def mixed_overlay_worker(', 'def verify_mixed_overlay('),
        'scripts/test/vector-topology-persistence-online.py': ('def difference_definition(', 'def run_difference_scenario(', 'def validate_difference_failure(', 'def validate_difference_browser_raw(', 'def run_mixed_overlay_scenario(', 'def validate_frozen_diagnostic_clipboards(', 'mixed overlay requires complete authorized source lineage before execution'),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('几何差集独立普通User双源混合成果及四失败保护', 'async function verifyDifferenceCase(', 'async function verifyMixedOverlayCase(', 'topology-difference-browser-raw.json'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_difference_t4_mixed_mask_and_four_refusals_independent_ogr',),
        'scripts/test/vector-topology-persistence-online_test.py': ('class DifferenceOnlineTest', 'class DifferenceBrowserRawTest', 'test_difference_requires_resource_visibility_before_any_execution'),
        'scripts/test/online-vector-topology-fixture_test.py': ('class DifferenceFixtureTest',),
        'scripts/test/online-hosted-topology-gate_test.py': ('test_difference_case_is_bound_without_old_business_or_capacity',),
    }
    for relative,fragments in difference_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology difference independent case contract incomplete: '+relative)
    dissolve_required = {
        '.github/workflows/online-t4-gates.yml': ('          - polygon-dissolve',),
        'scripts/test/online-hosted-topology-gate.sh': ('polygon-dissolve|intersection-overlay',),
        'business/scripts/online-vector-topology-fixture.py': ('DISSOLVE_FAILURES', 'def dissolve_features(', 'def verify_dissolve(', 'def dissolve_worker('),
        'scripts/test/vector-topology-persistence-online.py': ('def dissolve_definition(', 'def run_dissolve_scenario(', 'def validate_dissolve_failure(', 'def validate_dissolve_browser_raw(', 'dissolve requires complete authorized source lineage before execution', 'topology-dissolve-checkpoint.json'),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('融合几何独立普通User分组全局双成果及八失败保护', 'async function verifyDissolveCase(', 'async function assertDissolveMap(', 'topology-dissolve-browser-raw.json'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_dissolve_t4_two_success_eight_refusals_four_saves_and_ogr',),
        'scripts/test/vector-topology-persistence-online_test.py': ('class DissolveOnlineTest', 'class DissolveBrowserRawTest', 'test_dissolve_requires_resource_visibility_before_any_execution'),
        'scripts/test/online-vector-topology-fixture_test.py': ('class DissolveFixtureTest',),
        'scripts/test/online-hosted-topology-gate_test.py': ('test_dissolve_case_is_bound_without_old_business_or_capacity',),
    }
    for relative,fragments in dissolve_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology dissolve independent case contract incomplete: '+relative)
    overlay_required = {
        '.github/workflows/online-t4-gates.yml': ('          - intersection-overlay',),
        'scripts/test/online-hosted-topology-gate.sh': ('intersection-overlay|line-nodes',),
        'business/scripts/online-vector-topology-fixture.py': ('OVERLAY_FAILURES', 'def overlay_features(', 'def verify_overlay(', 'def overlay_worker(', 'def overlay_geometry('),
        'scripts/test/vector-topology-persistence-online.py': ('def overlay_definition(', 'def run_overlay_scenario(', 'def validate_overlay_failure(', 'def validate_overlay_browser_raw(', 'mixed overlay requires complete authorized source lineage before execution', 'topology-overlay-checkpoint.json'),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('相交叠加独立普通User完整混合交集及四失败保护', 'async function verifyOverlayCase(', 'async function assertOverlayMap(', 'async function assertOverlayCollectionPixels(', 'topology-overlay-browser-raw.json'),
        'manager/frontend/e2e/geometry-preview-coordinate.js': ('export function webMercatorPreviewGeometry(',),
        'engines/geopython-workflow/test_topology_operators.py': ('test_intersection_overlay_t4_fixture_full_mixed_and_four_refusals',),
        'scripts/test/vector-topology-persistence-online_test.py': ('class OverlayOnlineTest', 'class OverlayBrowserRawTest', 'class OverlayBrowserContractTest', 'test_overlay_requires_formal_resource_visibility_before_any_business_execution', 'test_overlay_raw_refuses_wire_that_disagrees_with_claimed_decoded_rows'),
        'scripts/test/online-vector-topology-fixture_test.py': ('class OverlayFixtureTest',),
        'scripts/test/online-hosted-topology-gate_test.py': ('test_overlay_case_is_bound_without_old_business_or_capacity',),
    }
    for relative,fragments in overlay_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology complete intersection case contract incomplete: '+relative)
    trim_required = {
        '.github/workflows/online-t4-gates.yml': ('          - line-trimming',),
        'scripts/test/online-hosted-topology-gate.sh': ('line-trimming|full|line-network-normalize',),
        'business/scripts/online-vector-topology-fixture.py': ('def trim_features(', 'def trim_worker(', 'TRIM_FAILURES', 'TRIM_LANDING_FAILURE'),
        'scripts/test/vector-topology-persistence-online.py': ('def trim_definition(', 'def run_trim_scenario(', 'def validate_trim_failure(', 'def validate_trim_browser_raw(', 'trim_diagnostic(trim_spec)', "if trim:prefix='trim'"),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('线端修剪独立普通User线与Point双成果及八失败保护','topology-trim-browser-raw.json','assertTrimRemovedSegment', 'assertTrimFailurePanel', 'record.monitor_page.execution.error_details'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_hosted_line_trim_case_dual_save_eight_failures_and_independent_oracle',),
        'scripts/test/vector-topology-persistence-online_test.py': ('class TrimOnlineTest', 'test_trim_one_success_eight_failures_exact_types_two_scans_four_edges'),
        'scripts/test/online-hosted-topology-gate_test.py': ('test_trim_case_is_bound_without_old_business_or_capacity',),
    }
    for relative,fragments in trim_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology trim case contract incomplete: '+relative)
    extension_required = {
        '.github/workflows/online-t4-gates.yml': ('          - line-extension',),
        'scripts/test/online-hosted-topology-gate.sh': ('line-proximity|line-extension',),
        'business/scripts/online-vector-topology-fixture.py': ('def extension_worker(', 'def verify_extension(', 'extension exact coordinates attributes IDs types direction or row order differ', 'EXTENSION_FAILURE'),
        'scripts/test/vector-topology-persistence-online.py': ('def run_extension_scenario(', 'def validate_extension_success(', 'def validate_extension_graph(', 'def validate_extension_failure(', "prefix='endpoint' if endpoint else 'extension'"),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('线端延伸独立普通User双成果与精确首次交点拒绝', 'assertExtensionLineMap', 'new_segment_pixels', 'topology-extension-browser-raw.json', 'persistExtensionEvidence', 'assertProximityLineage(observation.graph,binding)'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_hosted_line_extension_case_dual_save_failure_barrier_and_independent_oracle',),
        'scripts/test/vector-topology-persistence-online_test.py': ('class ExtensionOnlineTest', 'test_extension_actual_multiline_and_detail_map_rejects_missing_attribute_direction_pixels_and_old_geometry'),
        'scripts/test/online-hosted-topology-gate_test.py': ('test_extension_case_is_bound_without_old_business_or_capacity',),
    }
    for relative,fragments in extension_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology extension case contract incomplete: '+relative)
    vertices_required = {
        '.github/workflows/online-t4-gates.yml': ('          - vertex-extraction',),
        'scripts/test/online-hosted-topology-gate.sh': ('vertex-extraction|geometry-difference',),
        'business/scripts/online-vector-topology-fixture.py': ('def vertices_features(', 'def verify_vertices(', 'def vertices_worker(', 'VERTICES_FAILURES'),
        'scripts/test/vector-topology-persistence-online.py': ('def vertices_definition(', 'def run_vertices_scenario(', 'def validate_vertices_browser_raw(', 'vertices require complete formally authorized source lineage before execution'),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('顶点提取独立普通User三Point成果及七失败保护', 'async function verifyVerticesCase(', 'topology-vertices-browser-raw.json', 'if(vertices)observation.preview_text=await previewResponse.text()'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_vertex_extraction_t4_three_saves_seven_refusals_and_independent_ogr',),
        'scripts/test/vector-topology-persistence-online_test.py': ('class VerticesOnlineTest', 'test_vertices_formal_resource_visibility_precedes_any_business_execution'),
        'scripts/test/online-vector-topology-fixture_test.py': ('class VerticesFixtureTest',),
        'scripts/test/online-hosted-topology-gate_test.py': ('test_vertices_case_is_bound_without_old_business_or_capacity',),
    }
    for relative,fragments in vertices_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology vertex extraction case contract incomplete: '+relative)
    rings_required = {
        '.github/workflows/online-t4-gates.yml': ('          - polygon-rings',),
        'scripts/test/online-hosted-topology-gate.sh': ('endpoint-snapping|polygon-rings',),
        'business/scripts/online-vector-topology-fixture.py': ('def rings_features(', 'def verify_rings(', 'def rings_worker(', 'RINGS_FAILURES'),
        'scripts/test/vector-topology-persistence-online.py': ('def rings_definition(', 'def run_rings_scenario(', 'def validate_rings_graph(', 'validate_rings_preview_text', 'rings executions must be distinct'),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('面界提取独立普通User单成果与两失败保护','topology-rings-browser-raw.json','raw.preview_text=await previewResponse.text()','assertRingsLineage(raw.graph,evidence)'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_hosted_polygon_rings_save_two_failures_and_independent_oracle',),
        'scripts/test/vector-topology-persistence-online_test.py': ('class RingsOnlineTest', 'test_rings_one_source_one_scan_one_edge_and_two_failures'),
        'scripts/test/online-hosted-topology-gate_test.py': ('test_rings_case_is_bound_without_old_business_or_capacity',),
    }
    for relative,fragments in rings_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology polygon rings case contract incomplete: '+relative)
    rings_diagnostic_required = {
        '.github/workflows/online-t4-gates.yml': ('          - polygon-rings-input-diagnostics',),
        'scripts/test/online-hosted-topology-gate.sh': ('polygon-rings|polygon-rings-input-diagnostics',),
        'business/scripts/online-vector-topology-fixture.py': ('def rings_diagnostic_worker(', 'RINGS_DIAGNOSTIC_SOURCES', 'no physical publication'),
        'scripts/test/vector-topology-persistence-online.py': ('RINGS_DIAGNOSTIC_SPECS', 'def input_diagnostic_specs(', 'diagnostic frozen definition differs', 'distinct executions for the selected case'),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('面界提取参数诊断独立普通User双语失败页', 'verifyInputDiagnosticCase', 'observations).toHaveLength(8)', 'ring-geometry-invalid'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_hosted_polygon_ring_diagnostics_only_two_real_file_rejections',),
        'scripts/test/vector-topology-persistence-online_test.py': ('class RingInputDiagnosticOnlineTest', 'test_rings_diagnostic_requires_exact_eight_uuid_owner_locale_bindings'),
        'scripts/test/online-hosted-topology-gate_test.py': ('test_rings_diagnostic_case_runs_only_two_failures_without_old_business',),
    }
    for relative,fragments in rings_diagnostic_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology polygon ring diagnostics case incomplete: '+relative)
    rings_script=(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js').read_text()
    rings_match=re.search(r"(?ms)^test\('面界提取独立普通User单成果与两失败保护'.*?^\}\)",rings_script)
    rings_body=rings_match.group(0) if rings_match else ''
    if rings_body.find('await observeLineageCanvas(page)')<0 or rings_body.find('await login(')<rings_body.find('await observeLineageCanvas(page)'):
        raise RegistrationError('topology rings must await Canvas observer before navigation')
    endpoint_required = {
        '.github/workflows/online-t4-gates.yml': ('          - endpoint-snapping',),
        'scripts/test/online-hosted-topology-gate.sh': ('line-extension|endpoint-snapping',),
        'business/scripts/online-vector-topology-fixture.py': ('def endpoint_features(', 'def verify_endpoint(', 'def endpoint_worker(', 'ENDPOINT_FAILURES'),
        'scripts/test/vector-topology-persistence-online.py': ('def endpoint_definition(', 'def run_endpoint_scenario(', "failure['source_lineage']", 'failure executions must be distinct', 'def run_line_edit_scenario('),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('端点吸附独立普通User线与Point双成果及四失败保护','topology-endpoint-browser-raw.json','assertProximityPointMap(renderer,expected)','setCenter([24.25,4])'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_hosted_endpoint_case_dual_save_four_real_failures_and_independent_oracle',),
        'scripts/test/vector-topology-persistence-online_test.py': ('class EndpointOnlineTest', 'test_endpoint_four_failures_no_publication_two_scans_four_edges'),
        'scripts/test/online-hosted-topology-gate_test.py': ('test_endpoint_case_is_bound_without_old_business_or_capacity',),
    }
    for relative,fragments in endpoint_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology endpoint case contract incomplete: '+relative)
    extension_script=(repository/'console/frontend/e2e/online/vector-topology-persistence.spec.js').read_text()
    helper=re.search(r"(?ms)^async function verifyLineEditCase\(.*?^\}",extension_script)
    shared=helper.group(0) if helper else ''
    for title in ('线端修剪独立普通User线与Point双成果及八失败保护','线端延伸独立普通User双成果与精确首次交点拒绝','端点吸附独立普通User线与Point双成果及四失败保护'):
        match=re.search(r"(?ms)^test\('"+title+r"'.*?^\}\)",extension_script)
        body=match.group(0) if match else ''
        observer=body.find('await observeLineageCanvas(page)');verify=body.find('await verifyLineEditCase(')
        if observer<0 or verify<0 or observer>verify or 'await login(' not in shared or "output.port+'_graph_observed'" not in shared or "output.port+'_graph_rendered'" not in shared:
            raise RegistrationError('topology line edit must await its Canvas observer before navigation and preserve raw graph stages')
    proximity_required = {
        '.github/workflows/online-t4-gates.yml': ('          - line-proximity',),
        'scripts/test/online-hosted-topology-gate.sh': ('operator-input-diagnostics|line-proximity',),
        'business/scripts/online-vector-topology-fixture.py': ('def proximity_worker(', 'def verify_proximity(', 'proximity exact coordinates IDs values types or row order differ'),
        'scripts/test/vector-topology-persistence-online.py': ('def run_proximity_scenario(', 'def validate_proximity_success(', 'def validate_proximity_graph(', 'exactly two current-execution active source edges', 'meta_after'),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('近接检查独立普通User双源Point成果', 'assertProximityPointMap', 'assertProximityLineage', 'lineageCanvasSnapshot(canvas)', 'topology-proximity-browser-raw.json'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_hosted_line_proximity_case_exact_seven_points_and_independent_oracle',),
        'scripts/test/vector-topology-persistence-online_test.py': ('class ProximityOnlineTest',),
    }
    for relative,fragments in proximity_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology proximity case contract incomplete: '+relative)
    composition_required = {
        'business/scripts/online-vector-topology-fixture.py': ('line-network-normalize-chains', 'composition_source_features',
            'composition_expected_features', 'verify_composition_relations', '2**40', 'original direction or business fields differ'),
        'scripts/test/vector-topology-persistence-online.py': ("'match_fields':[]", "'depends_on':['normal','chains'] if composition and port=='edge_sources' else ['chains'] if composition else ['normal']", 'invalid_chain_id_field'),
        'console/frontend/e2e/online/vector-topology-persistence.spec.js': ('重合规整链路合并独立普通User三成果与字段拒绝',
            'verifyNetworkCase', 'chains:9,chain_edges:15,edge_sources:22', 'if(expected.length>20)', 'fullPageRead', 'addp.topology-normalize-chains'),
        'engines/geopython-workflow/test_topology_operators.py': ('test_hosted_normalize_chains_case_three_save_independent_physical_oracle',),
        'scripts/test/vector-topology-persistence-online_test.py': ('test_composition_exact_three_outputs_and_failure_barrier_without_old_scenarios',),
        'scripts/test/online-hosted-topology-gate.sh': ('full|line-network-normalize|line-network-normalize-chains',),
    }
    for relative,fragments in composition_required.items():
        if any(not source_contract_contains((repository/relative).read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('topology composition case contract incomplete: '+relative)
    required = {
        "scripts/test/online-hosted-topology-gate.sh": ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
            'start_online_geopython_runtime', '--mount', '--suite vector-topology-persistence',
            '--wait-protection-owners develop,manager', 'online-vector-topology-fixture.py stop',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'playwright install --with-deps chromium'),
        "scripts/test/vector-topology-persistence-online.py": ('validate_identity', 'PERMISSIONS = raster.PERMISSIONS',
            'validate_success', 'validate_item', 'triggered_by', 'meta_scan_runs', 'browser_runner', 'physical_size_bytes', 'check_geometry', 'repair_geometry', 'after_valid', 'fixture.SOURCES', 'snap_geometry', 'check_polygon_overlap', 'check_polygon_gap', 'check_dangling_points', 'check_line_self_overlap', 'line_self_overlap_expected', 'line_self_overlap_input', 'check_line_self_intersection', 'line_self_intersection_expected', 'line_self_intersection_input', 'split_line_intersections', "'scope': 'all'", 'split_line_scope', 'split_line_expected', 'split_line_input', 'group_line_components', 'line_component_expected', 'line_component_input', 'merge_line_chains', 'line_chain_expected', 'line_chain_members_expected', 'line_chain_input', 'check_line_intersection', 'line_intersection_expected', 'line_intersection_input', 'check_line_overlap', 'line_overlap_expected', 'line_overlap_input', 'dangling_expected', 'dangling_input', 'gap_expected', 'gap_coverage', 'overlap_expected', 'post_browser_physical', 'validate_failed_snap', 'collapsed_snap_diagnostic', 'topology-snap-failure.json', 'scan_ids(client)', 'vector.validate_item', 'addp.vector-topology-persistence/v14'),
        "business/scripts/online-vector-topology-fixture.py": ('NFSExport', 'validate_features', 'expected_geometries', 'source file changed', 'validate_repaired', 'repaired.geojson', 'SNAP_EXPECTED', 'validate_snap_features', 'verify_snap', 'snap-input.gpkg', 'snap-reference.gpkg', 'snapped.gpkg', 'archive_physical_evidence', 'verify_overlap', 'overlap_expected_features', 'overlap-input.gpkg', 'overlaps.gpkg', 'verify_gap', 'gap_expected_features', 'gap-input.gpkg', 'gap-coverage.gpkg', 'gaps.gpkg', 'verify_line_self_overlap', 'line_self_overlap_expected_features', 'line-self-overlap-input.gpkg', 'line-self-overlaps.gpkg', 'verify_split_line', '多次到访', '切点原重', 'verify_line_component', 'line_component_expected_features', 'line-component-input.gpkg', 'line-components.gpkg', 'verify_line_chain', 'line_chain_expected_features', 'line-chain-input.gpkg', 'line-chains.gpkg', 'line-chain-members.gpkg', 'member_no', 'source_part_no', 'is_reversed', 'component_id', 'part_no', 'segment_no', 'split_line_expected_features', 'split-line-input.gpkg', 'split-lines.gpkg', 'verify_line_self_intersection', 'line_self_intersection_expected_features', 'line-self-intersection-input.gpkg', 'line-self-intersections.gpkg', 'verify_line_intersection', 'line_intersection_expected_features', 'line-intersection-input.gpkg', 'line-intersections.gpkg', 'verify_line_overlap', 'line_overlap_expected_features', 'line-overlap-input.gpkg', 'line-overlaps.gpkg', 'verify_dangling', 'dangling_expected_features', 'dangling-input.gpkg', 'dangling.gpkg'),
        "scripts/utils/online_nfs_fixture.py": ('root_squash', 'owned_root', 'nfs_export_removed'),
        "console/frontend/e2e/online/vector-topology-persistence.spec.js": ('login(', 'browserAPIFromResponse(', 'principalID: evidence.principal_id', 'previewResponse', 'source_srid', 'getImageData', 'evidence.execution_id', 'topology-browser.json', 'row.geometry_valid', 'row.after_valid', 'MultiPolygon', 'topology-source-lineage-raw.json', 'expect(current).toEqual(source.before)', 'decodePreviewEWKB', 'snapCoordinates', 'source_srid', 'failed_execution_id', 'addp.topology-browser/v14', "expect(evidence.split_line_scope).toBe('all')", "split_lines: ['LineString', 43]", 'fullPageRead', ".table-preview .el-pagination__sizes .el-select", 'graph, preview', 'line_self_overlap_expected', '[evidence.sources[9].id]', 'line_self_intersection_expected', '[evidence.sources[11].id]', 'split_line_expected', '[evidence.sources[12].id]', 'line_component_expected', '[evidence.sources[13].id]', 'line_chain_expected', 'line_chain_members_expected', '[evidence.sources[14].id]', 'expectedOriginalLines', 'expectedOutputGeometry', 'expectedGeometryTypes(output.port)', 'expectedTypes.length', '.toEqual(expectedTypes)', 'Object.keys(partExpected[0].properties)', 'Object.fromEntries(fields.map(name => [name,feature.get(name)]))', 'line_intersection_expected', '[evidence.sources[10].id]', 'line_overlap_expected', 'overlap_length', '[evidence.sources[8].id]', 'dangling_expected', "source_id:feature.get('source_id')", "geometry.type).toBe('Point')", '[evidence.sources[7].id]', 'gap_expected', 'gapRings', 'gap_area', '[evidence.sources[5].id, evidence.sources[6].id]', 'overlap_expected', 'source_id_a', 'rendered', "getProjection().getCode() !== 'EPSG:3857'", 'toEqual(snapCoordinates.map(line => line.map(webMercatorPreviewCoordinate))'),
        "manager/frontend/e2e/geometry-preview-support.js": ('decodePreviewEWKB', 'zoomOutPreviewMap'),
        "manager/frontend/e2e/geometry-preview-coordinate.js": ('webMercatorPreviewCoordinate', '6378137', 'Math.atan(Math.exp(-y / radius))'),
        "manager/frontend/tests/previewGeometryWire.test.js": ('display projection round trip exactly', 'keeps display axis, displacement and vertex order differences observable'),
        "system/backend/cmd/online-test-fixture/main.go": ('case "vector-topology-persistence":', '"manager.data_item.read"'),
        "engines/geopython-workflow/test_topology_operators.py": ('test_hosted_topology_physical_oracle', 'test_hosted_line_component_fixture_rejects_real_physical_corruption', 'test_hosted_line_chain_fixture_rejects_real_physical_corruption',),
        "Makefile": ('@$(MAKE) test-vector-topology-online-runner', 'scripts/test/vector-topology-persistence-online_test.py',
            'scripts/test/online-vector-topology-fixture_test.py', 'scripts/test/online-hosted-topology-gate_test.py',
            'test_topology_requires_manual_nfs_identity_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f"topology owner contract is incomplete: {relative}")

def validate_raster_workflow_profile(repository: Path, registered: set[str]) -> None:
    if "raster-workflow" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  raster-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "runs-on: ubuntu-24.04" not in job.group("body") or (
        "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-workflow'\n" not in job.group("body")
    ):
        raise RegistrationError("raster-workflow requires a manual Ubuntu Hosted job")
    if "&& inputs.suite != 'raster-workflow'" not in workflow:
        raise RegistrationError("raster-workflow must not also dispatch on self-hosted")
    if load_suite_registry(repository)["raster-workflow"].nightly:
        raise RegistrationError("raster-workflow requires real evidence before schedule registration")
    case_input = re.search(r"(?ms)^      raster_case:\n(?P<body>.*?)(?=^      [a-z_]+:\n|\Z)", workflow)
    if (case_input is None or "type: choice" not in case_input.group("body")
        or "default: full" not in case_input.group("body")
        or re.findall(r"(?m)^          - ([a-z-]+)$", case_input.group("body")) != ["full", "branch-lineage"]
        or "ADDP_ONLINE_RASTER_CASE: ${{ inputs.raster_case }}" not in job.group("body")
        or "name: Online T4 (raster-workflow, ${{ inputs.raster_case }}, Linux x86_64)" not in job.group("body")
        or "name: online-t4-raster-workflow-${{ inputs.raster_case }}-${{ github.run_id }}" not in job.group("body")):
        raise RegistrationError("raster-workflow requires explicit full/branch-lineage input, propagation and evidence names")
    required = {
        "scripts/test/online-hosted-raster-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite raster-workflow',
            'scripts/test/online-engine-registration.py', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'env -u POSTGRES_PASSWORD make test-online', 'start_online_geopython_runtime',
            'RUNTIME_CONTAINER=addp-raster-online-runtime',
            'online-raster-minio-fixture.py stop',
            'full|branch-lineage)', 'raster-case.json', '${ADDP_ONLINE_RASTER_CASE-full}',
        ),
        "scripts/utils/hosted-online.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-product-runtime.sh"',
            'com.addp.online-runtime', 'stop_online_product_runtime || status=1',
        ),
        "scripts/utils/hosted-product-runtime.sh": (
            'start_online_geopython_runtime', 'verify_online_product_runtime',
            '--services geopython-workflow-engine --tag $tag --verify --jobs 1',
            'Runtime image identity mismatch', 'Runtime default entry mismatch',
            'com.addp.online-runtime',
        ),
        "business/scripts/online-raster-minio-fixture.py": (
            "full_check=True", "type=tmpfs,destination=/data", "source_unchanged", "com.addp.online-fixture",
            "verify-mosaic-first", "verify-mosaic-last", "baseline_sha256", "first_sha256",
            "verify-analysis", "analysis_expectations", "last_sha256",
            "prepare-images", "verify-images", "verify_ordinary_image",
        ),
        "scripts/test/raster-workflow-online.py": (
            "meta_scan_runs", "wait_lineage", "verify-replace", "failed create modified", "browser_runner",
            "spatial_workflow", "raster_reproject", "raster_clip", "raster_mosaic", "spatial_cases",
            "analysis_workflow", "raster_statistics", "raster_histogram", "validate_analysis", "analysis_cases",
            "scripts.test.raster-image-export-online", "report['image_exports']",
            "def raster_case(", "def run_selected_scenario(", "if case == 'branch-lineage':",
            "report['source_scans']", "run_branch_lineage(repository, env, client)",
        ),
        "scripts/test/raster-image-export-online.py": (
            "validate_editor", "validate_artifact", "EDITOR_PERMISSIONS", "preserved_sha256",
            "prepare-images", "verify-images", "source_unchanged", "strict_sizes", "meta_scan_runs",
        ),
        "console/frontend/e2e/online/raster-workflow.spec.js": (
            "login(", ".execution-lineage", "auth.principalID", "edge.evidence?.execution_id",
            "evidence.source_name", "evidence.target_name", "evidence.case_name",
            "evidence.expected_result", ".workflow-final-result-json", "professional.metadata.result.final_result",
            "栅格普通图片首次保存、重载与真实执行", "jpeg_quality", "page.reload()",
        ),
        "Makefile": ("test-raster-online-runner", "test-raster-workflow-online-runner",
                     "@$(MAKE) test-raster-workflow-online-runner", "scripts/test/raster-workflow-online_test.py",
                     "scripts/test/online-raster-minio-fixture_test.py", "scripts/test/online-hosted-raster-gate_test.py",
                     "scripts/test/raster-image-export-online_test.py", "test_raster_image_export.py",
                     "@$(MAKE) test-raster-image-export-online-runner"),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f"raster-workflow owner contract is incomplete: {relative}")


def validate_raster_vector_profile(repository: Path, registered: set[str], domain: str,
                                   source_name: str, oracle_test: str, physical_fragments: tuple[str, ...]) -> None:
    suite = f"raster-{domain}-persistence"
    if suite not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(rf"(?ms)^  raster-{domain}-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
        or f"if: github.event_name == 'workflow_dispatch' && inputs.suite == '{suite}'" not in job.group("body")
        or f"&& inputs.suite != '{suite}'" not in workflow
        or "nfs-kernel-server nfs-common" not in job.group("body")
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError(f"{domain} persistence requires a manual isolated Hosted NFS job")
    required = {
        f"scripts/test/online-hosted-raster-{domain}-gate.sh": ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
            'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
            '--wait-protection-owners develop,manager', f'online-raster-{domain}-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
        f"scripts/test/raster-{domain}-persistence-online.py": ('run_vector_scenario', f"source_name='{source_name}'"),
        "scripts/utils/online_vector_persistence.py": ('validate_identity', 'validate_success', 'validate_item',
            'wait_lineage', 'triggered_by', 'meta_scan_runs', 'physical_size_bytes'),
        f"business/scripts/online-raster-{domain}-fixture.py": ('owned_root', 'NFSExport', 'IsSame', 'runtime_workspaces_removed', *physical_fragments),
        "scripts/utils/online_nfs_fixture.py": ('root_squash', 'nfs_export_removed', 'owned_root'),
        "engines/geopython-workflow/test_online_raster_fixture.py": (oracle_test, f'test_{domain}_persistence_oracle_rejects_real_file_corruption'),
        "Makefile": (f'scripts/test/raster-{domain}-persistence-online_test.py',
            f'scripts/test/online-raster-{domain}-fixture_test.py', f'scripts/test/online-hosted-raster-{domain}-gate_test.py',
            f'test_{domain}_requires_manual_nfs_identity_and_registered_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f"{domain} persistence contract is incomplete: {relative}")


def validate_raster_contour_profile(repository: Path, registered: set[str]) -> None:
    validate_raster_vector_profile(repository, registered, 'contour', 'dem.tif',
        'test_contour_persistence_real_gdal_independent_plane_and_both_save_modes', ('GetPoint',))


def validate_raster_polygonize_profile(repository: Path, registered: set[str]) -> None:
    validate_raster_vector_profile(repository, registered, 'polygonize', 'classes.tif',
        'test_polygonize_persistence_real_gdal_all_regions_and_three_chains',
        ('expected_regions', 'region.equals', 'OFTInteger64'))


def validate_vector_rasterize_profile(repository: Path, registered: set[str]) -> None:
    suite = 'vector-rasterize-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  vector-rasterize-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-rasterize-persistence'" not in job.group('body')
        or "&& inputs.suite != 'vector-rasterize-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('rasterize requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-vector-rasterize-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
            'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
            '--wait-protection-owners develop,manager', 'online-vector-rasterize-fixture.py stop',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/vector-rasterize-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS',
            'fixture.SOURCES', 'triggered_by', "resource_type='file'", 'raster.strict_sizes', 'raster.wait_lineage',
            'raster.validate_target_item', 'scan_ids(client)!=before_scans', 'check_physical(native,previous)'),
        'business/scripts/online-vector-rasterize-fixture.py': ('NFSExport', 'owned_root', 'IsSame',
            'expected_pixels', 'full_check=True', 'runtime_workspaces_removed', 'source file changed'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_raster_fixture.py': (
            'test_rasterize_persistence_real_gdal_three_cog_chains_and_reference_independence',
            'test_rasterize_persistence_oracle_rejects_real_file_corruption'),
        'Makefile': ('@$(MAKE) test-vector-rasterize-online-runner', 'scripts/test/vector-rasterize-persistence-online_test.py',
            'scripts/test/online-vector-rasterize-fixture_test.py', 'scripts/test/online-hosted-vector-rasterize-gate_test.py',
            'test_rasterize_requires_manual_nfs_identity_strict_sizes_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'rasterize persistence contract is incomplete: {relative}')


def validate_vector_zonal_statistics_profile(repository: Path, registered: set[str]) -> None:
    suite = 'vector-zonal-statistics-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  vector-zonal-statistics-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-zonal-statistics-persistence'" not in job.group('body')
        or "&& inputs.suite != 'vector-zonal-statistics-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('vector-zone statistics requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-vector-zonal-statistics-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-vector-zonal-statistics-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/vector-zonal-statistics-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'triggered_by',
            'validate_item', 'used_sources(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', "for problem in ('band','collision')", 'complete_geometry_attributes_statistics_verified'),
        'business/scripts/online-vector-zonal-statistics-fixture.py': (
            'NFSExport', 'owned_root', 'expected_rows', 'statistics.pstdev', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source files changed', 'shapely.intersection', 'Integer64'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_raster_fixture.py': (
            'test_vector_zonal_persistence_real_gdal_three_gpkg_chains_and_full_content',
            'test_vector_zonal_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-vector-zonal-statistics-online-runner',
            'scripts/test/vector-zonal-statistics-persistence-online_test.py',
            'scripts/test/online-vector-zonal-statistics-fixture_test.py',
            'scripts/test/online-hosted-vector-zonal-statistics-gate_test.py',
            'test_vector_zonal_requires_manual_nfs_complete_gpkg_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'vector-zone statistics persistence contract is incomplete: {relative}')


def validate_raster_proximity_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-proximity-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-proximity-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-proximity-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-proximity-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('proximity requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-proximity-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-proximity-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-proximity-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'triggered_by',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', "for problem in ('band','targets')", 'full_pixel_and_mask_verification'),
        'business/scripts/online-raster-proximity-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'np.hypot', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_raster_fixture.py': (
            'test_proximity_persistence_real_gdal_two_cog_chains_and_complete_distances',
            'test_proximity_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-proximity-online-runner',
            'scripts/test/raster-proximity-persistence-online_test.py',
            'scripts/test/online-raster-proximity-fixture_test.py',
            'scripts/test/online-hosted-raster-proximity-gate_test.py',
            'test_proximity_requires_manual_nfs_identity_exact_distances_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'proximity persistence contract is incomplete: {relative}')


def validate_raster_focal_mode_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-mode-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-focal-mode-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-focal-mode-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-focal-mode-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal mode requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-focal-mode-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-focal-mode-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-focal-mode-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'triggered_by',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', "for problem in ('band','fractional','overflow')", 'full_pixel_and_mask_verification', 'exact_integer_pixels_verified'),
        'business/scripts/online-raster-focal-mode-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'Counter', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_focal_mode_fixture.py': (
            'test_focal_mode_persistence_real_gdal_three_cog_chains_and_complete_pixels',
            'test_focal_mode_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-focal-mode-online-runner',
            'test_online_focal_mode_fixture.py',
            'scripts/test/raster-focal-mode-persistence-online_test.py',
            'scripts/test/online-raster-focal-mode-fixture_test.py',
            'scripts/test/online-hosted-raster-focal-mode-gate_test.py',
            'test_focal_mode_requires_manual_nfs_exact_integer_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'focal mode persistence contract is incomplete: {relative}')


def validate_raster_boundary_clean_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-boundary-clean-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-boundary-clean-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-boundary-clean-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-boundary-clean-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('boundary clean requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-boundary-clean-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-boundary-clean-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-boundary-clean-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in ('band','fractional','overflow')", 'full_pixel_and_mask_verification', 'exact_integer_pixels_verified'),
        'business/scripts/online-raster-boundary-clean-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'Counter', 'empty-clean.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_boundary_clean_fixture.py': (
            'test_boundary_clean_persistence_real_gdal_three_cog_chains_and_complete_pixels',
            'test_boundary_clean_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-boundary-clean-online-runner',
            'test_online_boundary_clean_fixture.py',
            'scripts/test/raster-boundary-clean-persistence-online_test.py',
            'scripts/test/online-raster-boundary-clean-fixture_test.py',
            'scripts/test/online-hosted-raster-boundary-clean-gate_test.py',
            'test_boundary_clean_requires_manual_nfs_exact_integer_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'boundary clean persistence contract is incomplete: {relative}')


def validate_raster_thin_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-thin-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-thin-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-thin-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-thin-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('thinning requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-thin-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-thin-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-thin-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'exact_integer_pixels_verified', 'binary_skeleton_verified', 'invalid_diagnostic', 'foreground_value'),
        'business/scripts/online-raster-thin-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'adjacent_a', 'empty-thin.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_thin_fixture.py': (
            'test_thin_persistence_real_gdal_three_cog_chains_and_complete_pixels',
            'test_thin_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-thin-online-runner',
            'test_online_thin_fixture.py',
            'scripts/test/raster-thin-persistence-online_test.py',
            'scripts/test/online-raster-thin-fixture_test.py',
            'scripts/test/online-hosted-raster-thin-gate_test.py',
            'test_thin_requires_manual_nfs_exact_integer_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'thinning persistence contract is incomplete: {relative}')

def validate_raster_fill_depressions_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-fill-depressions-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-fill-depressions-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-fill-depressions-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-fill-depressions-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('DEM filling requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-fill-depressions-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-fill-depressions-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-fill-depressions-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'unit_metres_verified', 'depression_fill_verified', 'open_outlets_verified', 'never_lowered_verified', 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-fill-depressions-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'raw_filled_pixels', 'empty-filled.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_fill_depressions_fixture.py': (
            'test_fill_depressions_persistence_real_gdal_three_cog_chains_and_complete_pixels',
            'test_fill_depressions_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-fill-depressions-online-runner',
            'test_online_fill_depressions_fixture.py',
            'scripts/test/raster-fill-depressions-persistence-online_test.py',
            'scripts/test/online-raster-fill-depressions-fixture_test.py',
            'scripts/test/online-hosted-raster-fill-depressions-gate_test.py',
            'test_fill_depressions_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'DEM filling persistence contract is incomplete: {relative}')


def validate_raster_flow_direction_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-flow-direction-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-flow-direction-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-flow-direction-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-flow-direction-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('DEM D8 flow direction requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-flow-direction-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-flow-direction-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-flow-direction-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'direction_unit_cleared', 'direction_codes_verified', 'shortest_flat_paths_verified', 'acyclic_verified', 'final_open_outlets_verified', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-flow-direction-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'routing_evidence', 'empty-direction.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_flow_direction_d8_fixture.py': (
            'test_flow_direction_d8_persistence_real_gdal_three_cog_chains_and_complete_pixels',
            'test_flow_direction_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-flow-direction-d8-online-runner',
            'test_online_flow_direction_d8_fixture.py',
            'scripts/test/raster-flow-direction-d8-persistence-online_test.py',
            'scripts/test/online-raster-flow-direction-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-flow-direction-d8-gate_test.py',
            'test_flow_direction_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'DEM D8 flow direction persistence contract is incomplete: {relative}')


def validate_raster_flow_accumulation_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-flow-accumulation-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-flow-accumulation-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-flow-accumulation-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-flow-accumulation-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 flow accumulation requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-flow-accumulation-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-flow-accumulation-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-flow-accumulation-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'count_unit_cleared', 'counts_exclude_self', 'terminal_conservation_verified', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-flow-accumulation-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'conservation_evidence', 'empty-accumulation.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_flow_accumulation_d8_fixture.py': (
            'test_flow_accumulation_d8_persistence_real_gdal_four_cog_chains_and_complete_pixels',
            'test_flow_accumulation_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-flow-accumulation-d8-online-runner',
            'test_online_flow_accumulation_d8_fixture.py',
            'scripts/test/raster-flow-accumulation-d8-persistence-online_test.py',
            'scripts/test/online-raster-flow-accumulation-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-flow-accumulation-d8-gate_test.py',
            'test_flow_accumulation_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 flow accumulation persistence contract is incomplete: {relative}')


def validate_raster_drainage_basins_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-drainage-basins-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-drainage-basins-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-drainage-basins-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-drainage-basins-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 drainage basins requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-drainage-basins-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-drainage-basins-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-drainage-basins-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'basin_conservation_verified', 'basin_unit_cleared', 'unique_membership_verified', 'outlet_order_verified', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-drainage-basins-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'basin_evidence', 'empty-basins.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_drainage_basins_d8_fixture.py': (
            'test_drainage_basins_d8_persistence_real_gdal_four_cog_chains_and_complete_pixels',
            'test_drainage_basins_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-drainage-basins-d8-online-runner',
            'test_online_drainage_basins_d8_fixture.py',
            'scripts/test/raster-drainage-basins-d8-persistence-online_test.py',
            'scripts/test/online-raster-drainage-basins-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-drainage-basins-d8-gate_test.py',
            'test_drainage_basins_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 drainage basins persistence contract is incomplete: {relative}')


def validate_raster_flow_length_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-flow-length-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-flow-length-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-flow-length-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-flow-length-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 downstream flow length requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-flow-length-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-flow-length-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-flow-length-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'metre_unit_verified', 'terminals_zero', 'centre_steps_verified', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit', "'operator':'raster_flow_length_d8'", "'method':'downstream'"),
        'business/scripts/online-raster-flow-length-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'distance_evidence', 'empty-length.cog.tif', 'feet-length.cog.tif', 'survey-feet-length.cog.tif', 'SOURCE_UNITS', 'np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_flow_length_d8_fixture.py': (
            'test_flow_length_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels',
            'test_flow_length_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-flow-length-d8-online-runner',
            'test_online_flow_length_d8_fixture.py',
            'scripts/test/raster-flow-length-d8-persistence-online_test.py',
            'scripts/test/online-raster-flow-length-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-flow-length-d8-gate_test.py',
            'test_flow_length_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 downstream flow length persistence contract is incomplete: {relative}')


def validate_raster_flow_accumulation_area_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-flow-accumulation-area-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-flow-accumulation-area-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-flow-accumulation-area-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-flow-accumulation-area-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 contributing area requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-flow-accumulation-area-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-flow-accumulation-area-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-flow-accumulation-area-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'measure_unit_verified', 'isolated_terminal_self_verified', 'independent_measure_oracle_verified', 'count_control_equivalence_verified', 'nearest_overviews_verified', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-flow-accumulation-area-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'measure_evidence', 'empty-area.cog.tif', 'feet-area.cog.tif', 'survey-feet-area.cog.tif', 'SOURCE_UNITS', 'default-count.cog.tif', 'explicit-count.cog.tif', 'np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_flow_accumulation_area_d8_fixture.py': (
            'test_flow_accumulation_area_d8_persistence_real_gdal_eight_cog_chains_and_complete_pixels',
            'test_flow_accumulation_area_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-flow-accumulation-area-d8-online-runner',
            'test_online_flow_accumulation_area_d8_fixture.py',
            'scripts/test/raster-flow-accumulation-area-d8-persistence-online_test.py',
            'scripts/test/online-raster-flow-accumulation-area-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-flow-accumulation-area-d8-gate_test.py',
            'test_flow_accumulation_area_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 contributing area persistence contract is incomplete: {relative}')


def validate_raster_distance_to_stream_d8_profile(repository: Path, registered: set[str]) -> None:
    suite='raster-distance-to-stream-d8-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-distance-to-stream-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-distance-to-stream-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-distance-to-stream-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 distance to stream requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-distance-to-stream-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
            'start_online_geopython_runtime',
            '--mount',
            '--suite raster-workflow',
            '--wait-protection-owners develop,manager',
            'online-raster-distance-to-stream-d8-fixture.py stop',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
        ),
        'scripts/test/raster-distance-to-stream-d8-persistence-online.py': (
            'raster.validate_identity',
            'raster.EDITOR_PERMISSIONS',
            'fixture.SOURCES',
            'validate_scan(scanned,scan,identifier',
            'raster.strict_sizes',
            'raster.validate_target_item',
            'fixture.artifact_expectation(name)',
            'raster.wait_lineage',
            'scan_ids(client)!=before_scans',
            'source_edges(client',
            'rasterize.check_physical(native,previous)',
            'for name in fixture.TARGETS',
            'for problem in FAILURES',
            "for language in ('zh-cn','en')",
            'SubmittedLanguageClient',
            'invalid_diagnostic',
            'case_inputs(name)',
            'stream_raster',
            'stream_band',
            'elevation_unit',
            'first_hit_verified',
            'unreachable_nodata_verified',
            'river_zero_verified',
            'metre_unit_verified',
            'nearest_overviews_verified',
        ),
        'business/scripts/online-raster-distance-to-stream-d8-fixture.py': (
            'NFSExport',
            'owned_root',
            'expected_pixels',
            'walked_paths',
            'distance_evidence',
            'SOURCE_UNITS',
            'empty.cog.tif',
            'invalid.cog.tif',
            'feet.cog.tif',
            'survey-feet.cog.tif',
            'np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)',
            'source file changed',
            'runtime_workspaces_removed',
            'GDAL_TIFF_INTERNAL_MASK',
            'full_check=True',
        ),
        'scripts/utils/online_nfs_fixture.py': (
            'root_squash',
            'nfs_export_removed',
            'owned_root',
        ),
        'engines/geopython-workflow/test_online_distance_to_stream_d8_fixture.py': (
            'test_distance_to_stream_d8_persistence_real_gdal_eight_cog_chains_and_complete_pixels',
            'test_distance_to_stream_d8_persistence_oracle_rejects_corruption',
            'test_distance_to_stream_d8_persistence_twenty_four_failures_no_publication',
        ),
        'Makefile': (
            '@$(MAKE) test-raster-distance-to-stream-d8-online-runner',
            'test_online_distance_to_stream_d8_fixture.py',
            'scripts/test/raster-distance-to-stream-d8-persistence-online_test.py',
            'scripts/test/online-raster-distance-to-stream-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-distance-to-stream-d8-gate_test.py',
            'test_distance_to_stream_d8_requires_manual_nfs_full_pixels_and_regressions',
        ),
    }
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 distance to stream persistence contract is incomplete: {relative}')


def validate_raster_curvature_profile(repository: Path, registered: set[str]) -> None:
    suite='raster-curvature-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-curvature-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-curvature-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-curvature-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('DEM curvature requires a manual isolated Hosted NFS job')
    make=(repository/'Makefile').read_text()
    if not source_contract_contains(make, 'test-geopython-workflow:', repository) or 'test_online_curvature_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('curvature Native fixture must run in the standard GeoPython recipe')
    required={
      'scripts/test/online-hosted-raster-curvature-gate.sh':('source "$ROOT_DIR/scripts/utils/hosted-online.sh"','start_online_geopython_runtime','--mount','--suite raster-workflow','--wait-protection-owners develop,manager','online-raster-curvature-fixture.py stop','unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN','ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
      'scripts/test/raster-curvature-persistence-online.py':('raster.validate_identity','raster.EDITOR_PERMISSIONS','fixture.SOURCES','validate_scan(scanned,scan,identifier','raster.strict_sizes','raster.validate_target_item','fixture.artifact_expectation(name)','raster.wait_lineage','scan_ids(client)!=before_scans','source_edges(client','rasterize.check_physical(native,previous)','for name in fixture.TARGETS','for problem in FAILURES',"for language in ('zh-cn','en')",'SubmittedLanguageClient','invalid_diagnostic','case_inputs(name)','raster_curvature','elevation_unit','overview_count','query language rewrote stored failure'),
      'business/scripts/online-raster-curvature-fixture.py':('NFSExport','owned_root','expected_pixels','expected_dem','np.linalg.lstsq','np.linalg.solve','SOURCE_UNITS','small.cog.tif','sparse.cog.tif','filled.cog.tif','reclassified.cog.tif','1/metre','np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)','source file changed','runtime_workspaces_removed','GDAL_TIFF_INTERNAL_MASK','full_check=True','nearest_overviews'),
      'engines/geopython-workflow/test_online_curvature_fixture.py':('test_curvature_persistence_real_gdal_eight_cog_chains_and_complete_pixels','test_curvature_persistence_oracle_rejects_corruption','test_curvature_persistence_twenty_four_failures_no_publication','forced_centre','laplacian','wrong_units'),
      'Makefile':('@$(MAKE) test-raster-curvature-online-runner','scripts/test/raster-curvature-persistence-online_test.py','scripts/test/online-raster-curvature-fixture_test.py','scripts/test/online-hosted-raster-curvature-gate_test.py','test_curvature_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('DEM curvature persistence contract is incomplete: '+relative)


def validate_raster_cut_fill(repository: Path, registered: set[str]) -> None:
    suite='raster-cut-fill-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-cut-fill-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-cut-fill-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-cut-fill-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('DEM cut/fill requires a manual isolated Hosted NFS job')
    make=(repository/'Makefile').read_text()
    if not source_contract_contains(make, 'test-geopython-workflow:', repository) or 'test_online_cut_fill_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('cut/fill Native fixture must run in the standard GeoPython recipe')
    required={'scripts/test/online-hosted-raster-cut-fill-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
                                                        'start_online_geopython_runtime',
                                                        '--mount',
                                                        '--suite raster-workflow',
                                                        '--wait-protection-owners develop,manager',
                                                        'online-raster-cut-fill-fixture.py stop',
                                                        'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
                                                        'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
 'scripts/test/raster-cut-fill-persistence-online.py': ('raster.validate_identity',
                                                        'raster.EDITOR_PERMISSIONS',
                                                        'validate_scan(scanned,scan,identifier',
                                                        'validate_success(execution,bindings,target)',
                                                        'expected_lineage',
                                                        'input.before',
                                                        'input.after',
                                                        'output.save',
                                                        'lineage_observer.validate(facts,expected,public=True)',
                                                        'record_lineage',
                                                        'verify_lineage_archive',
                                                        'lineage_observer.collect',
                                                        'lineage_observer.verify_archive',
                                                        'validate_preview',
                                                        'validate_item',
                                                        'raster.wait_lineage',
                                                        'scan_ids(client)!=before_scans',
                                                        'source_edges(client',
                                                        'rasterize.check_physical(native,previous)',
                                                        'for name in fixture.TARGETS',
                                                        'for problem in FAILURES',
                                                        "for language in ('zh-cn','en')",
                                                        'SubmittedLanguageClient',
                                                        'invalid_diagnostic',
                                                        'case_inputs(name)',
                                                        'raster_cut_fill',
                                                        'after_elevation_unit',
                                                        'archive_csv',
                                                        'cut-fill-csv-manifest.json',
                                                        'csv_base64',
                                                        'previous archived CSV changed',
                                                        'query language rewrote stored failure'),
 'scripts/test/raster-cut-fill-persistence-online_test.py': ('test_node_ports_bind_roles_independent_of_task_and_fact_array_order',
                                                                'test_role_swaps_duplicate_missing_and_extra_ports_or_relations_fail_closed',
                                                                'test_failure_freezes_closed_actual_and_expected_facts_before_rejection',
                                                                'test_lineage_evidence_is_immutable_and_rejects_symlinks_or_extra_inventory'),
 'business/scripts/online-raster-cut-fill-fixture.py': ('NFSExport',
                                                        'owned_root',
                                                        'expected_row',
                                                        'Counter',
                                                        'context.prec=200',
                                                        'range(35)',
                                                        'independent_decimal_oracle_verified',
                                                        'count_and_area_conservation_verified',
                                                        'nonnegative_gross_and_net_difference_verified',
                                                        'parse_csv',
                                                        'csv_base64',
                                                        'source file changed',
                                                        'runtime_workspaces_removed',
                                                        'GDAL_TIFF_INTERNAL_MASK'),
 'engines/geopython-workflow/test_online_cut_fill_fixture.py': ('test_cut_fill_persistence_real_gdal_twelve_csv_chains_complete_oracle_and_original_bytes',
                                                                'test_cut_fill_persistence_thirty_six_exact_failures_no_publication',
                                                                'test_cut_fill_persistence_oracle_rejects_csv_corruption',
                                                                'one_ulp',
                                                                'test_cut_fill_persistence_worker_runs_from_stdin_without_repository_imports'),
 'Makefile': ('@$(MAKE) test-raster-cut-fill-online-runner',
              'scripts/test/raster-cut-fill-persistence-online_test.py',
              'scripts/test/online-raster-cut-fill-fixture_test.py',
              'scripts/test/online-hosted-raster-cut-fill-gate_test.py',
              'test_cut_fill_requires_manual_nfs_complete_csv_archive_and_regressions')}
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('cut/fill persistence contract is incomplete: '+relative)
    # Fixed first-phase matrix is executable data, not a comment claiming coverage.
    import ast
    fixture=ast.parse((repository/'business/scripts/online-raster-cut-fill-fixture.py').read_text())
    scene=ast.parse((repository/'scripts/test/raster-cut-fill-persistence-online.py').read_text())
    def literal(tree,name):
        return next(ast.literal_eval(node.value) for node in tree.body if isinstance(node,ast.Assign) and any(isinstance(t,ast.Name) and t.id==name for t in node.targets))
    if len(literal(fixture,'CASES'))!=12 or len(literal(scene,'FAILURES'))!=18:
        raise RegistrationError('cut/fill requires exactly twelve saves and thirty-six bilingual failures')


def validate_raster_surface_area(repository: Path, registered: set[str]) -> None:
    suite='raster-surface-area-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-surface-area-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-surface-area-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-surface-area-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('DEM surface area requires a manual isolated Hosted NFS job')
    make=(repository/'Makefile').read_text()
    if not source_contract_contains(make, 'test-geopython-workflow:', repository) or 'test_online_surface_area_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('surface area Native fixture must run in the standard GeoPython recipe')
    required={'scripts/test/online-hosted-raster-surface-area-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'online-raster-surface-area-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'), 'scripts/test/raster-surface-area-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'validate_scan(scanned,scan,identifier', 'raster.wait_lineage', 'scan_ids(client)!=before_scans', 'source_edges(client', 'rasterize.check_physical(native,previous)', 'for name in fixture.TARGETS', 'for problem in FAILURES', "for language in ('zh-cn','en')", 'SubmittedLanguageClient', 'invalid_diagnostic', 'raster_surface_area', 'input.source', 'output.save', 'archived_csv', 'query language rewrote stored failure', 'CSV physical/Meta sizes must be equal positive integers', 'all 48 calls and 510 full actual CSV checks are required', 'physical_verification_calls', 'full_target_checks', 'previous CSV manifest changed', 'previous CSV manifest inventory differs', 'archived CSV has no original manifest', 'exact contracted serialized preview row differs', 'preview_serialized_row'), 'business/scripts/online-raster-surface-area-fixture.py': ('NFSExport', 'owned_root', 'expected_row', 'context.prec=200', 'cross=[u[1]*v[2]-u[2]*v[1]', 'min(ad,bc)', 'max(ad,bc)', 'low+=', 'high+=', 'mean=(low+high)/2', 'source file changed', 'runtime_workspaces_removed', 'GDAL_TIFF_INTERNAL_MASK', 'csv_base64', 'complete_table_verified', 'dual_diagonal_and_centre_domain_verified', 'source_measurement_geometry_verified', 'value!=target', 'float(value)', 'int64.csv', 'uint64.csv', 'single.csv', "(spec['width']-1)*(spec['height']-1)", 'double_precision=15', 'preview_serialized_row', 'quads=Counter()', 'anchors='), 'engines/geopython-workflow/test_online_surface_area_fixture.py': ('test_surface_area_persistence_real_gdal_twelve_csv_chains_complete_oracle_and_original_bytes', 'test_surface_area_persistence_thirty_six_exact_failures_no_publication', 'test_surface_area_persistence_oracle_rejects_csv_corruption', 'one_ulp', 'test_surface_area_persistence_independent_full_scalar_four_corner_count', 'test_surface_area_persistence_worker_runs_from_stdin_without_repository_imports', 'test_surface_area_independent_csv_oracle_rejects_wrong_semantics', 'half', 'one_diagonal', 'frame_area', 'test_surface_area_original_csv_manifest_never_silently_repaired', 'scene.validate_preview', 'test_surface_area_host_oracle_requires_no_numpy_pandas_or_gdal'), 'Makefile': ('@$(MAKE) test-raster-surface-area-online-runner', 'scripts/test/raster-surface-area-persistence-online_test.py', 'scripts/test/online-raster-surface-area-fixture_test.py', 'scripts/test/online-hosted-raster-surface-area-gate_test.py', 'test_surface_area_requires_manual_nfs_complete_csv_archive_and_regressions'), 'scripts/test/raster-surface-area-persistence-online_test.py': ('test_twelve_exact_dags_one_actual_source_bands_units_and_existing_save', 'test_complete_matrix_exact_current_scan_lineage_and_original_csv_archive', 'physical_verification_calls', 'full_target_checks', 'test_identity_size_and_failure_publication_are_fail_closed', 'test_previous_physical_or_archive_bytes_cannot_change')}
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('surface area persistence contract is incomplete: '+relative)
    # Fixed first-phase matrix is executable data, not a comment claiming coverage.
    import ast
    fixture=ast.parse((repository/'business/scripts/online-raster-surface-area-fixture.py').read_text())
    scene=ast.parse((repository/'scripts/test/raster-surface-area-persistence-online.py').read_text())
    def literal(tree,name):
        return next(ast.literal_eval(node.value) for node in tree.body if isinstance(node,ast.Assign) and any(isinstance(t,ast.Name) and t.id==name for t in node.targets))
    if len(literal(fixture,'CASES'))!=12 or len(literal(scene,'FAILURES'))!=18:
        raise RegistrationError('surface area requires exactly twelve saves and thirty-six bilingual failures')

    expected_targets={'flat.csv','plane.csv','diagonals.csv','mixed.csv','feet.csv','survey-feet.csv','us-grid.csv','filled.csv','reclassified.csv','int64.csv','uint64.csv','single.csv'}
    if set(literal(fixture,'CASES'))!=expected_targets:raise RegistrationError('surface area fixed target identities changed')
    expected_failures=('band','alpha','complex','complex_alpha','rotated','south_up','geographic','no_crs','no_grid','unit','unit_conflict','unit_unknown','scale','offset','all_invalid','no_quad','width_one','height_one')
    if literal(scene,'FAILURES')!=expected_failures:raise RegistrationError('surface area fixed bilingual failure categories changed')



def validate_raster_slope_orthogonal(repository: Path, registered: set[str]) -> None:
    import ast
    suite='raster-slope-orthogonal-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-slope-orthogonal-hosted-t4:\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or any(text not in job[1]for text in(
        "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-slope-orthogonal-persistence'",
        'runs-on: ubuntu-24.04','cancel-in-progress: false','nfs-kernel-server nfs-common',
        'bash scripts/test/online-hosted-raster-slope-orthogonal-gate.sh --check-only',
        'run: bash scripts/test/online-hosted-raster-slope-orthogonal-gate.sh','if: always()'))
        or "&& inputs.suite != 'raster-slope-orthogonal-persistence'"not in workflow or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('orthogonal slope requires an isolated manual Hosted NFS job')
    source_input=re.search(r'(?ms)^      slope_orthogonal_source_commit:\n(.*?)(?=^      [a-z][a-z0-9_]*:\n|^jobs:\n|\Z)',workflow)
    fixed_source_fragments=(
        'name: Validate requested orthogonal slope source commit',
        '[[ "$SOURCE_COMMIT" =~ ^[0-9a-f]{40}$ ]]',
        'ref: ${{ inputs.slope_orthogonal_source_commit }}',
        'name: Verify orthogonal slope source ancestry',
        'SOURCE_COMMIT: ${{ inputs.slope_orthogonal_source_commit }}',
        'DISPATCH_COMMIT: ${{ github.sha }}',
        '[ "$(git rev-parse HEAD)" = "$SOURCE_COMMIT" ]',
        'git merge-base --is-ancestor "$SOURCE_COMMIT" "$DISPATCH_COMMIT"',
        "printf 'Orthogonal slope source commit=%s; dispatch commit=%s")
    if (source_input is None or 'type: string'not in source_input[1] or 'required: false'not in source_input[1]
        or any(fragment not in job[1]for fragment in fixed_source_fragments)
        or job[1].index('Validate requested orthogonal slope source commit')>job[1].index('Check out repository')
        or job[1].index('Verify orthogonal slope source ancestry')>job[1].index('Set up Go')):
        raise RegistrationError('orthogonal slope requires an explicit full ancestor source SHA and exact checkout')
    make=(repository/'Makefile').read_text()
    if ('test_online_slope_orthogonal_fixture.py'not in make_target_commands(make,'test-geopython-workflow')
        or 'test-raster-slope-orthogonal-online-runner:'not in make_target_commands(make,'test-raster-online-runner')):
        raise RegistrationError('orthogonal slope must use standard Native/Runner aggregates')
    required={
        'scripts/test/online-hosted-raster-slope-orthogonal-gate.sh':('ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600','--suite raster-slope-orthogonal-persistence','--wait-protection-owners develop,manager','start_online_geopython_runtime','online_raster_lineage.py prepare','online-raster-slope-orthogonal-fixture.py stop'),
        'business/scripts/online-raster-slope-orthogonal-fixture.py':('def reference','independent_horn_verified','full_check=True','def prepare_oracle','def cleanup_oracle','def archive_files','actual north-up COG contrast differs','source file changed'),
        'scripts/test/raster-slope-orthogonal-persistence-online.py':('COUNTS=(16,36,712,53)','view.run_scenario','raster.strict_sizes','def check_and_archive','def validate_frozen','ViewshedMetadata','source_scan_client=orth.source_scan_client','check_scan_inventory=orth.check_scan_inventory'),
        'scripts/test/raster-sight-orthogonal-persistence-online.py':('scan_preparation=True','source_grant_participants','def source_scan_client','def check_scan_inventory'),
        'scripts/test/raster-viewshed-persistence-online.py':('profile.COUNTS','profile.validate_persisted','profile.check_and_archive','lineage.collect','lineage.verify_archive','readonly.no_publication','BEGIN READ ONLY','ExactFileMetadataGrants','query language rewrote stored failure','source or prior target Meta snapshot changed'),
        'scripts/utils/online_raster_lineage.py':("'raster-slope-orthogonal-persistence': 'slope-orthogonal-lineage'",),
        'system/backend/cmd/online-test-fixture/main.go':('case \"raster-slope-orthogonal-persistence\", \"raster-sight-orthogonal-persistence\", \"sql-pool-consumer-flow\", \"transfer-continuous-events\":',),
        'system/backend/cmd/online-test-fixture/main_test.go':('TestSlopeOrthogonalPreparationKeepsConsumerTwelveAndSeparateThirteen',),
        'engines/geopython-workflow/test_online_slope_orthogonal_fixture.py':('test_slope_orthogonal_sixteen_real_chains_and_712_complete_original_checks','checks==712','test_slope_orthogonal_exact_failure_node_language_and_zero_private_output','test_slope_orthogonal_corrupt_actual_or_expected_never_passes'),
        'Makefile':('scripts/test/raster-slope-orthogonal-persistence-online_test.py','scripts/test/online-raster-slope-orthogonal-fixture_test.py','scripts/test/online-hosted-raster-slope-orthogonal-gate_test.py')}
    for name,fragments in required.items():
        path=repository/name
        if not path.is_file()or any(not source_contract_contains(path.read_text(),fragment,repository)for fragment in fragments):raise RegistrationError('orthogonal slope registration/evidence is incomplete: '+name)
    def literal(path,name):
        try:
            tree=ast.parse((repository/path).read_text())
            return next(ast.literal_eval(node.value)for node in tree.body if isinstance(node,ast.Assign)and any(isinstance(t,ast.Name)and t.id==name for t in node.targets))
        except (StopIteration,ValueError,SyntaxError):raise RegistrationError('orthogonal slope matrix is invalid')from None
    groups=literal('business/scripts/online-raster-slope-orthogonal-fixture.py','GROUPS')
    failures=literal('scripts/test/raster-slope-orthogonal-persistence-online.py','FAILURES')
    if groups!=('north','rotated','quarter-turn','half-turn','feet','survey','coverage-cross-block','invalid') or set(failures)!=set(('shear','near-orthogonal','reflect-column','reflect-row','geographic','no-crs','no-grid','degenerate','band-zero','band-high','alpha','unit','unit-bool','unit-conflict','unit-unknown','scale','offset','slope-unit')):
        raise RegistrationError('orthogonal slope requires eight fixed groups and eighteen bilingual categories')
    fixture=(repository/'business/scripts/online-raster-slope-orthogonal-fixture.py').read_text()
    if "for g in GROUPS for u in('degree','percent')"not in fixture or "'expected_array_count':16"not in fixture:
        raise RegistrationError('orthogonal slope requires degree/percent and sixteen frozen complete arrays')

def validate_raster_sight_orthogonal(repository: Path, registered: set[str]) -> None:
    import ast
    suite='raster-sight-orthogonal-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-sight-orthogonal-hosted-t4:\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or any(text not in job[1]for text in(
        "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-sight-orthogonal-persistence'",
        'runs-on: ubuntu-24.04','cancel-in-progress: false','nfs-kernel-server nfs-common',
        'bash scripts/test/online-hosted-raster-sight-orthogonal-gate.sh --check-only',
        'run: bash scripts/test/online-hosted-raster-sight-orthogonal-gate.sh','if: always()'))
        or "&& inputs.suite != 'raster-sight-orthogonal-persistence'"not in workflow or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('orthogonal sight requires an isolated manual Hosted NFS job')
    source_input=re.search(r'(?ms)^      sight_orthogonal_source_commit:\n(.*?)(?=^      [a-z][a-z0-9_]*:\n|^jobs:\n|\Z)',workflow)
    fixed_source_fragments=(
        'name: Validate requested orthogonal sight source commit',
        '[[ "$SOURCE_COMMIT" =~ ^[0-9a-f]{40}$ ]]',
        'ref: ${{ inputs.sight_orthogonal_source_commit }}',
        'name: Verify orthogonal sight source ancestry',
        'SOURCE_COMMIT: ${{ inputs.sight_orthogonal_source_commit }}',
        'DISPATCH_COMMIT: ${{ github.sha }}',
        '[ "$(git rev-parse HEAD)" = "$SOURCE_COMMIT" ]',
        'git merge-base --is-ancestor "$SOURCE_COMMIT" "$DISPATCH_COMMIT"',
        "printf 'Orthogonal sight source commit=%s; dispatch commit=%s")
    if (source_input is None or 'type: string'not in source_input[1] or 'required: false'not in source_input[1]
        or any(fragment not in job[1]for fragment in fixed_source_fragments)
        or job[1].index('Validate requested orthogonal sight source commit')>job[1].index('Check out repository')
        or job[1].index('Verify orthogonal sight source ancestry')>job[1].index('Set up Go')):
        raise RegistrationError('orthogonal sight requires an explicit full ancestor source SHA and exact checkout')
    make=(repository/'Makefile').read_text()
    if ('test_online_sight_orthogonal_fixture.py'not in make_target_commands(make,'test-geopython-workflow')
        or 'test-raster-sight-orthogonal-online-runner:'not in make_target_commands(make,'test-raster-online-runner')):
        raise RegistrationError('orthogonal sight must use standard Native/Runner aggregates')
    required={
        'scripts/test/online-hosted-raster-sight-orthogonal-gate.sh':('ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600','--suite raster-sight-orthogonal-persistence','--wait-protection-owners develop,manager','start_online_geopython_runtime','online_raster_lineage.py prepare','online-raster-sight-orthogonal-fixture.py stop'),
        'business/scripts/online-raster-sight-orthogonal-fixture.py':('def reference','enter>=leave','expected!=255','full_check=True','def prepare_oracle','def cleanup_oracle','def archive_files','actual north-up COG contrast differs','source file changed'),
        'scripts/test/raster-sight-orthogonal-persistence-online.py':('COUNTS=(16,40,776,57)','view.run_scenario','fixture.parse_csv','raster.strict_sizes','def check_and_archive','def validate_frozen','ViewshedMetadata','file_contract','scan_preparation=True','source_grant_participants','def source_scan_client','def check_scan_inventory'),
        'scripts/test/raster-viewshed-persistence-online.py':('profile.COUNTS','profile.validate_persisted','profile.check_and_archive','lineage.collect','lineage.verify_archive','readonly.no_publication','BEGIN READ ONLY','ExactFileMetadataGrants','query language rewrote stored failure','source or prior target Meta snapshot changed'),
        'scripts/utils/online_raster_lineage.py':("'raster-sight-orthogonal-persistence': 'sight-orthogonal-lineage'",),
        'system/backend/cmd/online-test-fixture/main.go':('case \"raster-slope-orthogonal-persistence\", \"raster-sight-orthogonal-persistence\", \"sql-pool-consumer-flow\", \"transfer-continuous-events\":',),
        'system/backend/cmd/online-test-fixture/main_test.go':('TestSightOrthogonalPreparationKeepsConsumerTwelveAndSeparateThirteen',),
        'engines/geopython-workflow/test_online_sight_orthogonal_fixture.py':('test_sight_orthogonal_sixteen_real_chains_and_776_complete_original_checks','checks==776','test_sight_orthogonal_exact_failure_node_language_and_zero_private_output','test_sight_orthogonal_corrupt_actual_or_expected_never_passes'),
        'Makefile':('scripts/test/raster-sight-orthogonal-persistence-online_test.py','scripts/test/online-raster-sight-orthogonal-fixture_test.py','scripts/test/online-hosted-raster-sight-orthogonal-gate_test.py')}
    for name,fragments in required.items():
        path=repository/name
        if not path.is_file()or any(not source_contract_contains(path.read_text(),fragment,repository)for fragment in fragments):raise RegistrationError('orthogonal sight registration/evidence is incomplete: '+name)
    def literal(path,name):
        tree=ast.parse((repository/path).read_text())
        try:return next(ast.literal_eval(node.value)for node in tree.body if isinstance(node,ast.Assign)and any(isinstance(t,ast.Name)and t.id==name for t in node.targets))
        except (StopIteration,ValueError):raise RegistrationError('orthogonal sight matrix is invalid')from None
    cases=literal('business/scripts/online-raster-sight-orthogonal-fixture.py','CASES')
    failures=literal('scripts/test/raster-sight-orthogonal-persistence-online.py','FAILURES')
    if len(cases)!=16 or sum(n.endswith('.csv')for n in cases)!=8 or len(failures)!=20:raise RegistrationError('orthogonal sight requires eight CSV/eight COG and twenty bilingual categories')


def validate_raster_viewshed(repository: Path, registered: set[str]) -> None:
    import ast
    suite='raster-viewshed-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-viewshed-hosted-t4:\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or any(text not in job[1]for text in(
        "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-viewshed-persistence'",
        'runs-on: ubuntu-24.04','cancel-in-progress: false','nfs-kernel-server nfs-common',
        'bash scripts/test/online-hosted-raster-viewshed-gate.sh --check-only','run: bash scripts/test/online-hosted-raster-viewshed-gate.sh','if: always()'))
        or "&& inputs.suite != 'raster-viewshed-persistence'"not in workflow or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('viewshed requires an isolated manual Hosted NFS job')
    make=(repository/'Makefile').read_text()
    if ('test_online_viewshed_fixture.py'not in make_target_commands(make,'test-geopython-workflow')
        or 'test-raster-viewshed-online-runner:'not in make_target_commands(make,'test-raster-online-runner')):
        raise RegistrationError('viewshed Native and Runner must use standard aggregate gates')
    required={
        'scripts/test/online-hosted-raster-viewshed-gate.sh':('ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600','--suite raster-workflow','--wait-protection-owners develop,manager','start_online_geopython_runtime','online_raster_lineage.py prepare','online-raster-viewshed-fixture.py stop'),
        'business/scripts/online-raster-viewshed-fixture.py':('def expected_pixels','enter>=leave','gdal.GDT_Byte','expected!=255','full_check=True','def prepare_oracle','def cleanup_oracle','def archive_cogs','source file changed'),
        'scripts/test/raster-viewshed-persistence-online.py':('(16, 36, 712, 53)',"for language in ('zh-cn', 'en')",'dtype=\'Byte\'','raster.strict_sizes','lineage.collect','lineage.verify_archive','readonly.no_publication','ViewshedMetadata','ExactFileMetadataGrants','BEGIN READ ONLY','query language rewrote stored failure','source or prior target Meta snapshot changed',"ARCHIVE_NAME='viewshed-cogs'","profile.ARCHIVE_NAME+'-manifest.json'"),
        'scripts/utils/online_raster_lineage.py':("'raster-viewshed-persistence': 'viewshed-lineage'",),
        'engines/geopython-workflow/test_online_viewshed_fixture.py':('test_viewshed_real_sixteen_cogs_complete_pixels_masks_and_immutable_originals','checks==712','test_viewshed_real_exact_failure_node_language_and_zero_publication','test_viewshed_independent_oracle_rejects_corrupt_actual_or_frozen_facts'),
        'scripts/test/raster-viewshed-persistence-online_test.py':('test_complete_fixed_matrix_rereads_and_original_manifest','test_identity_language_lineage_scan_size_and_metadata_fail_closed','test_unknown_write_outcome_remains_journalled_for_cleanup'),
        'Makefile':('scripts/test/raster-viewshed-persistence-online_test.py','scripts/test/online-raster-viewshed-fixture_test.py','scripts/test/online-hosted-raster-viewshed-gate_test.py')}
    for name,fragments in required.items():
        path=repository/name
        if not path.is_file()or any(not source_contract_contains(path.read_text(),text,repository)for text in fragments):
            raise RegistrationError('viewshed evidence or registration is incomplete: '+name)
    def literal(path,name):
        try:
            tree=ast.parse((repository/path).read_text())
            return next(ast.literal_eval(node.value)for node in tree.body if isinstance(node,ast.Assign)and any(isinstance(t,ast.Name)and t.id==name for t in node.targets))
        except (SyntaxError,ValueError,StopIteration):
            raise RegistrationError('viewshed fixed matrix is invalid') from None
    if len(literal('business/scripts/online-raster-viewshed-fixture.py','CASES'))!=16 or len(literal('scripts/test/raster-viewshed-persistence-online.py','FAILURES'))!=18:
        raise RegistrationError('viewshed requires sixteen chains and eighteen bilingual failures')


def validate_raster_line_of_sight(repository: Path, registered: set[str]) -> None:
    import ast

    suite = 'raster-line-of-sight-persistence'
    if suite not in registered:
        return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-line-of-sight-hosted-t4:\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    gate = 'scripts/test/online-hosted-raster-line-of-sight-gate.sh'
    if (job is None or any(fragment not in job[1] for fragment in (
            f"if: github.event_name == 'workflow_dispatch' && inputs.suite == '{suite}'",
            'runs-on: ubuntu-24.04', 'cancel-in-progress: false', 'nfs-kernel-server nfs-common',
            f'bash {gate} --check-only', f'run: bash {gate}', 'if: always()'))
            or f"&& inputs.suite != '{suite}'" not in workflow or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('line of sight requires an isolated manual Hosted NFS job')

    fixture = 'business/scripts/online-raster-line-of-sight-fixture.py'
    scene = 'scripts/test/raster-line-of-sight-persistence-online.py'
    runner = 'test-raster-line-of-sight-online-runner'
    native = 'test_online_line_of_sight_fixture.py'
    make = (repository/'Makefile').read_text()
    if (not source_contract_contains(make_target_commands(make, 'test-geopython-workflow'), native, repository)
            or runner+':' not in make_target_commands(make, 'test-raster-online-runner')
            or 'scripts/test/online-raster-lineage_test.py' not in make_target_commands(make, runner)):
        raise RegistrationError('line-of-sight Native and Runner must be in the standard aggregate gates')
    required = {
        gate: ('start_online_geopython_runtime', '--suite raster-workflow', '--wait-protection-owners develop,manager',
               'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare',
               'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'online-raster-line-of-sight-fixture.py stop'),
        fixture: ('GDAL_TIFF_INTERNAL_MASK', 'GCI_AlphaBand', 'enter >= leave', 'decimal', 'double_precision=15',
                  'runtime_workspaces_removed', 'source bytes changed', 'csv_base64'),
        scene: ('(16, 36, 712, 53)', "for language in ('zh-cn', 'en')", 'lineage.collect',
                'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'readonly.no_publication',
                'query language rewrote stored failure', 'source or prior target Meta snapshot changed',
                'line-of-sight-csv-manifest.json', 'fixture.parse_csv', 'immutable(archive/name, payload)'),
        'engines/geopython-workflow/'+native: ('test_line_of_sight_real_sixteen_csv_chains_preview_original_bytes_and_fail_closed_archive',
                                             'test_line_of_sight_real_exact_failure_node_language_and_no_publication'),
        'scripts/utils/online_raster_lineage.py': ("'raster-line-of-sight-persistence': 'line-of-sight-lineage'",),
        'scripts/test/raster-line-of-sight-persistence-online_test.py': ('test_complete_fixed_matrix_rereads_and_original_manifest',
                                                                       'test_identity_language_lineage_scan_size_and_metadata_fail_closed'),
        'Makefile': ('scripts/test/raster-line-of-sight-persistence-online_test.py',
                     'scripts/test/online-hosted-raster-line-of-sight-gate_test.py',
                     'test_line_of_sight_requires_manual_nfs_and_fixed_matrix'),
    }
    for name, fragments in required.items():
        path = repository/name
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('line-of-sight persistence evidence or registration is incomplete: '+name)

    def literal(relative, name):
        module = ast.parse((repository/relative).read_text())
        return next(ast.literal_eval(node.value) for node in module.body if isinstance(node, ast.Assign)
                    and any(isinstance(t, ast.Name) and t.id == name for t in node.targets))

    cases = literal(fixture, 'CASES')
    if (len(cases) != 16 or set(cases.values()) != {'visible', 'blocked', 'indeterminate'}
            or len(literal(scene, 'FAILURES')) != 18):
        raise RegistrationError('line of sight requires the fixed three-state and bilingual matrix')


def validate_raster_profile_curvature(repository: Path, registered: set[str]) -> None:
    suite='raster-profile-curvature-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-profile-curvature-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-profile-curvature-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-profile-curvature-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('DEM curvature requires a manual isolated Hosted NFS job')
    make=(repository/'Makefile').read_text()
    if not source_contract_contains(make, 'test-geopython-workflow:', repository) or 'test_online_profile_curvature_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('curvature Native fixture must run in the standard GeoPython recipe')
    required={
      'scripts/test/online-hosted-raster-profile-curvature-gate.sh':('source "$ROOT_DIR/scripts/utils/hosted-online.sh"','start_online_geopython_runtime','--mount','--suite raster-workflow','--wait-protection-owners develop,manager','online-raster-profile-curvature-fixture.py stop','unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN','ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
      'scripts/test/raster-profile-curvature-persistence-online.py':('raster.validate_identity','raster.EDITOR_PERMISSIONS','fixture.SOURCES','validate_scan(scanned,scan,identifier','raster.strict_sizes','raster.validate_target_item','fixture.artifact_expectation(name)','raster.wait_lineage','scan_ids(client)!=before_scans','source_edges(client','rasterize.check_physical(native,previous)','for name in fixture.TARGETS','for problem in FAILURES',"for language in ('zh-cn','en')",'SubmittedLanguageClient','invalid_diagnostic','case_inputs(name)','raster_curvature','elevation_unit','overview_count','query language rewrote stored failure','curvature_method','flat_zero_verified','zero_gradient_nodata_verified','mean_control_verified','mean_controls_equal','undefined_stationary_centres'),
      'business/scripts/online-raster-profile-curvature-fixture.py':('NFSExport','owned_root','expected_pixels','expected_dem','np.linalg.lstsq','np.linalg.solve','SOURCE_UNITS','small.cog.tif','sparse.cog.tif','filled.cog.tif','reclassified.cog.tif','1/metre','np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)','source file changed','runtime_workspaces_removed','GDAL_TIFF_INTERNAL_MASK','full_check=True','nearest_overviews','semantic_masks','math.fsum','np.einsum','mean-default.cog.tif','mean-explicit.cog.tif','stationary.cog.tif','flat.cog.tif'),
      'engines/geopython-workflow/test_online_profile_curvature_fixture.py':('test_profile_curvature_persistence_real_gdal_twelve_cog_chains_and_complete_pixels','test_profile_curvature_persistence_oracle_rejects_corruption','test_profile_curvature_persistence_twenty_six_failures_no_publication','forced_centre','laplacian','wrong_units'),
      'Makefile':('@$(MAKE) test-raster-profile-curvature-online-runner','scripts/test/raster-profile-curvature-persistence-online_test.py','scripts/test/online-raster-profile-curvature-fixture_test.py','scripts/test/online-hosted-raster-profile-curvature-gate_test.py','test_profile_curvature_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('DEM curvature persistence contract is incomplete: '+relative)


def validate_raster_tangential_curvature(repository: Path, registered: set[str]) -> None:
    suite='raster-tangential-curvature-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-tangential-curvature-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-tangential-curvature-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-tangential-curvature-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('DEM curvature requires a manual isolated Hosted NFS job')
    make=(repository/'Makefile').read_text()
    if not source_contract_contains(make, 'test-geopython-workflow:', repository) or 'test_online_tangential_curvature_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('curvature Native fixture must run in the standard GeoPython recipe')
    required={
      'scripts/test/online-hosted-raster-tangential-curvature-gate.sh':('source "$ROOT_DIR/scripts/utils/hosted-online.sh"','start_online_geopython_runtime','--mount','--suite raster-workflow','--wait-protection-owners develop,manager','online-raster-tangential-curvature-fixture.py stop','unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN','ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
      'scripts/test/raster-tangential-curvature-persistence-online.py':('raster.validate_identity','raster.EDITOR_PERMISSIONS','fixture.SOURCES','validate_scan(scanned,scan,identifier','raster.strict_sizes','raster.validate_target_item','fixture.artifact_expectation(name)','raster.wait_lineage','scan_ids(client)!=before_scans','source_edges(client','rasterize.check_physical(native,previous)','for name in fixture.TARGETS','for problem in FAILURES',"for language in ('zh-cn','en')",'SubmittedLanguageClient','invalid_diagnostic','case_inputs(name)','raster_curvature','elevation_unit','overview_count','query language rewrote stored failure','curvature_method','flat_zero_verified','zero_gradient_nodata_verified','mean_control_verified','mean_controls_equal','directional_identity_verified','directional_identity_pixels','profile_control_verified','tiny_gradient_verified','plan_distinction_verified','undefined_stationary_centres'),
      'business/scripts/online-raster-tangential-curvature-fixture.py':('NFSExport','owned_root','expected_pixels','expected_dem','np.linalg.lstsq','np.linalg.solve','SOURCE_UNITS','small.cog.tif','sparse.cog.tif','filled.cog.tif','reclassified.cog.tif','1/metre','np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)','source file changed','runtime_workspaces_removed','GDAL_TIFF_INTERNAL_MASK','full_check=True','nearest_overviews','semantic_masks','math.fsum','np.einsum','mean-default.cog.tif','mean-explicit.cog.tif','stationary.cog.tif','flat.cog.tif','profile-control.cog.tif','mode_controls','saved directional curvatures do not sum to twice the mean'),
      'engines/geopython-workflow/test_online_tangential_curvature_fixture.py':('test_tangential_curvature_persistence_real_gdal_thirteen_cog_chains_and_complete_pixels','test_tangential_curvature_persistence_oracle_rejects_corruption','test_tangential_curvature_persistence_twenty_six_failures_no_publication','forced_centre','laplacian','wrong_units'),
      'Makefile':('@$(MAKE) test-raster-tangential-curvature-online-runner','scripts/test/raster-tangential-curvature-persistence-online_test.py','scripts/test/online-raster-tangential-curvature-fixture_test.py','scripts/test/online-hosted-raster-tangential-curvature-gate_test.py','test_tangential_curvature_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('DEM curvature persistence contract is incomplete: '+relative)


def validate_raster_lineage_observer(repository: Path, registered: set[str]) -> None:
    if not registered.intersection({'raster-cut-fill-persistence','raster-tpi-persistence','raster-tri-persistence','raster-roughness-persistence','raster-surface-area-persistence','raster-twi-persistence', 'raster-spi-persistence', 'raster-sieve-persistence', 'raster-focal-percentile-persistence'}):return
    required={
        'scripts/utils/online_raster_lineage.py': ('BEGIN READ ONLY;', 'statement_timeout', 'LIMIT 1', 'public observation must hide internal operations', 'actor_principal_id', 'workflow_matches', "public=True", "public=False", 'closed(public, expected)', 'closed(internal, expected)', 'validate_container', 'StartedAt', 'read_error', "'prepare'"),
        'scripts/test/online-raster-lineage_test.py': ('test_public_null_and_complete_internal_are_both_required', 'test_public_cannot_expose_operations_or_omit_them', 'test_internal_null_empty_missing_extra_and_wrong_operation_rejected', 'test_current_execution_identity_and_workflow_are_required', 'test_container_identity_restart_labels_and_database_cannot_change'),
    }
    for path,fragments in required.items():
        text=(repository/path).read_text()
        if any(not source_contract_contains(text, fragment, repository) for fragment in fragments):raise RegistrationError('bounded raster internal lineage evidence incomplete: '+path)
    make=(repository/'Makefile').read_text()
    for prefix in ('cut-fill','tpi','tri','roughness','surface-area','twi', 'spi', 'sieve', 'focal-percentile'):
        gate=(repository/f'scripts/test/online-hosted-raster-{prefix}-gate.sh').read_text()
        if 'run_logged python3 scripts/utils/online_raster_lineage.py prepare' not in gate:raise RegistrationError('raster owner receipt preparation omitted')
        block=make_target_commands(make, f'test-raster-{prefix}-online-runner')
        if not source_contract_contains(block, 'scripts/test/online-raster-lineage_test.py', repository):raise RegistrationError('raster observer regression missing from standard gate')


def validate_raster_tpi(repository: Path, registered: set[str]) -> None:
    import ast
    suite='raster-tpi-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-tpi-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-tpi-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-tpi-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or '${{ runner.temp }}/addp-online-${{ github.run_id }}/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('TPI requires a manual isolated Hosted NFS job and whole original-COG evidence upload')
    make=(repository/'Makefile').read_text()
    if not source_contract_contains(make, 'test-geopython-workflow:', repository) or 'test_online_tpi_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('TPI Native fixture must run in the standard GeoPython recipe')
    required={'scripts/test/online-hosted-raster-tpi-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'online-raster-tpi-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'), 'scripts/test/raster-tpi-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'validate_scan(scanned,scan,identifier', 'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans', 'source_edges(client', 'rasterize.check_physical(native,previous)', 'for name in fixture.TARGETS', 'for problem in FAILURES', "for language in ('zh-cn','en')", 'SubmittedLanguageClient', 'invalid_diagnostic', 'raster_tpi', 'unit_unknown', 'validate_lineage', 'input.source', 'output.save', 'archived_cogs', 'incomplete fixed TPI matrix', 'oracle_prepare', 'oracle_cleanup', 'all 429 full target checks are required', 'physical_verification_calls', 'substage_ms', 'physical_profile_ms', 'ADDP_ONLINE_TPI_ORACLE_ANCHOR_SHA256'), 'business/scripts/online-raster-tpi-fixture.py': ('NFSExport', 'owned_root', 'expected_pixels', 'expected_dem', 'context.prec=2500', 'Decimal(8)', 'np.unique', 'np.array_equal(actual,expected,equal_nan=True)', 'source file changed', 'runtime_workspaces_removed', 'GDAL_TIFF_INTERNAL_MASK', 'full_check=True', 'nearest_overviews', 'metre', 'int64.cog.tif', 'uint64.cog.tif', 'saddle.cog.tif', 'sparse.cog.tif', 'archive_cogs', 'tpi-cogs-manifest.json', 'previous archived COG changed', 'previous COG manifest inventory differs', 'archived COG byte identity differs', 'def prepare_oracle', 'def load_expected', 'def cleanup_oracle', 'oracle anchor is required', 'oracle cache hash changed', 'allow_pickle=False', 'source_hash_ms', 'cache_load_ms', 'worker_transport_ms'), 'engines/geopython-workflow/test_online_tpi_fixture.py': ('test_tpi_persistence_real_gdal_thirteen_chains_complete_exact_pixels_and_original_cogs', 'test_tpi_persistence_twenty_six_accurate_bilingual_failures_no_publication', 'test_tpi_persistence_oracle_rejects_physical_corruption', 'one_ulp', 'centre_in_mean', 'test_tpi_original_cog_archive_cannot_change_or_silently_repair', 'test_tpi_worker_stdin_is_repository_independent_and_sources_remain_immutable', 'test_tpi_cached_expectation_never_caches_actual_pixels_or_rebuilds_on_hit', 'test_tpi_oracle_cache_poison_or_cross_scope_fails', 'test_tpi_partial_oracle_preparation_always_has_an_owned_cleanup'), 'Makefile': ('@$(MAKE) test-raster-tpi-online-runner', 'test_online_tpi_fixture.py', 'scripts/test/raster-tpi-persistence-online_test.py', 'scripts/test/online-raster-tpi-fixture_test.py', 'scripts/test/online-hosted-raster-tpi-gate_test.py', 'test_tpi_requires_manual_nfs_exact_pixels_archive_and_regressions'), 'scripts/test/online-raster-tpi-fixture_test.py': ('test_private_anchor_rejects_omission_tampering_and_cross_run_before_runtime', 'test_partial_host_preparation_cleanup_is_mandatory_and_bound'), 'scripts/test/raster-tpi-persistence-online_test.py': ('test_oracle_preparation_and_cleanup_are_inside_progress_and_all_429_checks_remain', 'test_oracle_failures_cleanup_and_preserve_first_failure', 'test_cache_and_timing_proofs_cannot_be_omitted_or_rebound')}
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('TPI persistence contract is incomplete: '+relative)
    for relative,key,size in [('business/scripts/online-raster-tpi-fixture.py','CASES',13),('scripts/test/raster-tpi-persistence-online.py','FAILURES',13)]:
        tree=ast.parse((repository/relative).read_text());values=[n.value for n in tree.body if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id==key for t in n.targets)]
        if len(values)!=1 or len(ast.literal_eval(values[0]))!=size:raise RegistrationError('TPI fixed 13-success/26-bilingual-failure matrix changed')


def validate_raster_tri(repository: Path, registered: set[str]) -> None:
    import ast
    suite='raster-tri-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-tri-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-tri-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-tri-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or '${{ runner.temp }}/addp-online-${{ github.run_id }}/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('TRI requires a manual isolated Hosted NFS job and whole original-COG evidence upload')
    make=(repository/'Makefile').read_text()
    if not source_contract_contains(make, 'test-geopython-workflow:', repository) or 'test_online_tri_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('TRI Native fixture must run in the standard GeoPython recipe')
    required={'scripts/test/online-hosted-raster-tri-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'online-raster-tri-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'), 'scripts/test/raster-tri-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'validate_scan(scanned,scan,identifier', 'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans', 'source_edges(client', 'rasterize.check_physical(native,previous)', 'for name in fixture.TARGETS', 'for problem in FAILURES', "for language in ('zh-cn','en')", 'SubmittedLanguageClient', 'invalid_diagnostic', 'raster_tri', 'unit_unknown', 'validate_lineage', 'input.source', 'output.save', 'archived_cogs', 'incomplete fixed TRI matrix', 'oracle_prepare', 'oracle_cleanup', 'all 429 full target checks are required', 'physical_verification_calls', 'substage_ms', 'physical_profile_ms', 'ADDP_ONLINE_TRI_ORACLE_ANCHOR_SHA256', 'nonnegative_verified', 'inclined plane or saddle must have strictly positive complete TRI values'), 'business/scripts/online-raster-tri-fixture.py': ('NFSExport', 'owned_root', 'expected_pixels', 'expected_dem', 'context.prec=2500', 'sum((centre-v)**2 for v in samples).sqrt()*factor', 'np.unique', 'np.array_equal(actual,expected,equal_nan=True)', 'source file changed', 'runtime_workspaces_removed', 'GDAL_TIFF_INTERNAL_MASK', 'full_check=True', 'nearest_overviews', 'metre', 'int64.cog.tif', 'uint64.cog.tif', 'saddle.cog.tif', 'sparse.cog.tif', 'archive_cogs', 'tri-cogs-manifest.json', 'previous archived COG changed', 'previous COG manifest inventory differs', 'archived COG byte identity differs', 'def prepare_oracle', 'def load_expected', 'def cleanup_oracle', 'oracle anchor is required', 'oracle cache hash changed', 'allow_pickle=False', 'source_hash_ms', 'cache_load_ms', 'worker_transport_ms', 'negative Riley TRI value', 'inclined plane or saddle TRI must be positive', 'nonnegative_verified'), 'engines/geopython-workflow/test_online_tri_fixture.py': ('test_tri_persistence_real_gdal_thirteen_chains_complete_exact_pixels_and_original_cogs', 'test_tri_persistence_twenty_six_accurate_bilingual_failures_no_publication', 'test_tri_persistence_oracle_rejects_physical_corruption', 'one_ulp', 'rms', 'test_tri_original_cog_archive_cannot_change_or_silently_repair', 'test_tri_worker_stdin_is_repository_independent_and_sources_remain_immutable', 'test_tri_cached_expectation_never_caches_actual_pixels_or_rebuilds_on_hit', 'test_tri_oracle_cache_poison_or_cross_scope_fails', 'test_tri_partial_oracle_preparation_always_has_an_owned_cleanup'), 'Makefile': ('@$(MAKE) test-raster-tri-online-runner', 'test_online_tri_fixture.py', 'scripts/test/raster-tri-persistence-online_test.py', 'scripts/test/online-raster-tri-fixture_test.py', 'scripts/test/online-hosted-raster-tri-gate_test.py', 'test_tri_requires_manual_nfs_exact_pixels_archive_and_regressions'), 'scripts/test/online-raster-tri-fixture_test.py': ('test_private_anchor_rejects_omission_tampering_and_cross_run_before_runtime', 'test_partial_host_preparation_cleanup_is_mandatory_and_bound'), 'scripts/test/raster-tri-persistence-online_test.py': ('test_oracle_preparation_and_cleanup_are_inside_progress_and_all_429_checks_remain', 'test_oracle_failures_cleanup_and_preserve_first_failure', 'test_cache_and_timing_proofs_cannot_be_omitted_or_rebound')}
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('TRI persistence contract is incomplete: '+relative)
    for relative,key,size in [('business/scripts/online-raster-tri-fixture.py','CASES',13),('scripts/test/raster-tri-persistence-online.py','FAILURES',13)]:
        tree=ast.parse((repository/relative).read_text());values=[n.value for n in tree.body if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id==key for t in n.targets)]
        if len(values)!=1 or len(ast.literal_eval(values[0]))!=size:raise RegistrationError('TRI fixed 13-success/26-bilingual-failure matrix changed')


def validate_raster_roughness(repository: Path, registered: set[str]) -> None:
    import ast
    suite='raster-roughness-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-roughness-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-roughness-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-roughness-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or '${{ runner.temp }}/addp-online-${{ github.run_id }}/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('ROUGHNESS requires a manual isolated Hosted NFS job and whole original-COG evidence upload')
    make=(repository/'Makefile').read_text()
    if not source_contract_contains(make, 'test-geopython-workflow:', repository) or 'test_online_roughness_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('ROUGHNESS Native fixture must run in the standard GeoPython recipe')
    required={'scripts/test/online-hosted-raster-roughness-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'online-raster-roughness-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'), 'scripts/test/raster-roughness-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'validate_scan(scanned,scan,identifier', 'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans', 'source_edges(client', 'rasterize.check_physical(native,previous)', 'for name in fixture.TARGETS', 'for problem in FAILURES', "for language in ('zh-cn','en')", 'SubmittedLanguageClient', 'invalid_diagnostic', 'raster_roughness', 'unit_unknown', 'validate_lineage', 'input.source', 'output.save', 'archived_cogs', 'incomplete fixed ROUGHNESS matrix', 'oracle_prepare', 'oracle_cleanup', 'all 429 full target checks are required', 'physical_verification_calls', 'substage_ms', 'physical_profile_ms', 'ADDP_ONLINE_ROUGHNESS_ORACLE_ANCHOR_SHA256', 'nonnegative_verified', 'inclined plane or saddle must have strictly positive complete ROUGHNESS values'), 'business/scripts/online-raster-roughness-fixture.py': ('NFSExport', 'owned_root', 'expected_pixels', 'expected_dem', 'context.prec=2500', '(max(samples)-min(samples))*factor', 'np.unique', 'np.array_equal(actual,expected,equal_nan=True)', 'source file changed', 'runtime_workspaces_removed', 'GDAL_TIFF_INTERNAL_MASK', 'full_check=True', 'nearest_overviews', 'metre', 'int64.cog.tif', 'uint64.cog.tif', 'saddle.cog.tif', 'sparse.cog.tif', 'archive_cogs', 'roughness-cogs-manifest.json', 'previous archived COG changed', 'previous COG manifest inventory differs', 'archived COG byte identity differs', 'def prepare_oracle', 'def load_expected', 'def cleanup_oracle', 'oracle anchor is required', 'oracle cache hash changed', 'allow_pickle=False', 'source_hash_ms', 'cache_load_ms', 'worker_transport_ms', 'negative nine-point roughness value', 'inclined plane or saddle ROUGHNESS must be positive', 'nonnegative_verified', 'centre extrema or opposite-neighbour full range differs', 'inclined plane full range must be ten metres'), 'engines/geopython-workflow/test_online_roughness_fixture.py': ('test_roughness_persistence_real_gdal_thirteen_chains_complete_exact_pixels_and_original_cogs', 'test_roughness_persistence_twenty_six_accurate_bilingual_failures_no_publication', 'test_roughness_persistence_oracle_rejects_physical_corruption', 'one_ulp', 'half_range', 'test_roughness_original_cog_archive_cannot_change_or_silently_repair', 'test_roughness_worker_stdin_is_repository_independent_and_sources_remain_immutable', 'test_roughness_cached_expectation_never_caches_actual_pixels_or_rebuilds_on_hit', 'test_roughness_oracle_cache_poison_or_cross_scope_fails', 'test_roughness_partial_oracle_preparation_always_has_an_owned_cleanup', 'test_roughness_independent_oracle_rejects_centre_omission_and_detrending', 'exclude_centre_max', 'exclude_centre_min', 'max_centre_difference', 'detrend_plane'), 'Makefile': ('@$(MAKE) test-raster-roughness-online-runner', 'test_online_roughness_fixture.py', 'scripts/test/raster-roughness-persistence-online_test.py', 'scripts/test/online-raster-roughness-fixture_test.py', 'scripts/test/online-hosted-raster-roughness-gate_test.py', 'test_roughness_requires_manual_nfs_exact_pixels_archive_and_regressions'), 'scripts/test/online-raster-roughness-fixture_test.py': ('test_private_anchor_rejects_omission_tampering_and_cross_run_before_runtime', 'test_partial_host_preparation_cleanup_is_mandatory_and_bound'), 'scripts/test/raster-roughness-persistence-online_test.py': ('test_oracle_preparation_and_cleanup_are_inside_progress_and_all_429_checks_remain', 'test_oracle_failures_cleanup_and_preserve_first_failure', 'test_cache_and_timing_proofs_cannot_be_omitted_or_rebound')}
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('ROUGHNESS persistence contract is incomplete: '+relative)
    for relative,key,size in [('business/scripts/online-raster-roughness-fixture.py','CASES',13),('scripts/test/raster-roughness-persistence-online.py','FAILURES',13)]:
        tree=ast.parse((repository/relative).read_text());values=[n.value for n in tree.body if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id==key for t in n.targets)]
        if len(values)!=1 or len(ast.literal_eval(values[0]))!=size:raise RegistrationError('ROUGHNESS fixed 13-success/26-bilingual-failure matrix changed')


def validate_raster_twi(repository: Path, registered: set[str]) -> None:
    import ast
    suite='raster-twi-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-twi-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-twi-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-twi-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or '${{ runner.temp }}/addp-online-${{ github.run_id }}/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('TWI requires a manual isolated Hosted NFS job and whole original-COG evidence upload')
    make=(repository/'Makefile').read_text()
    if not source_contract_contains(make, 'test-geopython-workflow:', repository) or 'test_online_twi_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('TWI Native fixture must run in the standard GeoPython recipe')
    required={'scripts/test/online-hosted-raster-twi-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'online-raster-twi-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare'), 'scripts/test/raster-twi-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'validate_scan(scanned,scan,identifier', 'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans', 'source_edges(client', 'rasterize.check_physical(native,previous)', 'for name in fixture.TARGETS', 'for problem in FAILURES', "for language in ('zh-cn','en')", 'SubmittedLanguageClient', 'invalid_diagnostic', 'raster_twi', 'input.area', 'input.slope', 'input.source', 'output.save', 'source_bindings', 'archived_cogs', 'incomplete fixed TWI matrix', 'oracle_prepare', 'oracle_cleanup', 'all 66 calls and 833 full target checks are required', 'physical_verification_calls', 'full_target_checks', 'ADDP_ONLINE_TWI_ORACLE_ANCHOR_SHA256', 'query language rewrote stored failure', 'fixed_area_over_width_verified', 'no_slope_threshold_verified',"maximum=52 if name=='negative'",'len(self.stages)>=113'), 'business/scripts/online-raster-twi-fixture.py': ('NFSExport', 'owned_root', 'expected_pixels', 'expected_inputs', 'context.prec=200', 'for odd in range(599,0,-2)', 'denominator=Decimal(odd)-x*x/denominator', '(Decimal(a)/d/tangents[b]).ln()', 'measurement_width', 'np.array_equal(actual,expected,equal_nan=True)', 'source file changed', 'runtime_workspaces_removed', 'GDAL_TIFF_INTERNAL_MASK', 'full_check=True', 'nearest_overviews', 'int64.cog.tif', 'uint64.cog.tif', 'disjoint.cog.tif', 'zero-slope.cog.tif', 'archive_cogs', 'twi-cogs-manifest.json', 'previous archived COG changed', 'previous COG manifest inventory differs', 'archived COG byte identity differs', 'def prepare_oracle', 'def load_expected', 'def cleanup_oracle', 'oracle anchor is required', 'oracle cache hash changed', 'allow_pickle=False', 'joint_coverage_verified', 'dimensionless_unit_verified', 'fixed_area_over_width_verified', 'no_slope_threshold_verified', 'legal_signed_zero_verified'), 'engines/geopython-workflow/test_online_twi_fixture.py': ('test_twi_persistence_real_gdal_fourteen_chains_exact_pixels_mask_and_original_cogs', 'test_twi_persistence_fifty_two_accurate_bilingual_failures_no_publication', 'test_twi_persistence_oracle_rejects_physical_corruption', 'one_ulp', 'test_twi_independent_oracle_rejects_wrong_semantics', 'width_multiplied', 'count_as_area', 'base10', 'tiny_threshold', 'test_twi_original_cog_archive_cannot_change_or_silently_repair', 'test_twi_worker_stdin_is_repository_independent_and_sources_remain_immutable', 'test_twi_cached_expectation_never_caches_actual_pixels_or_rebuilds_on_hit', 'test_twi_oracle_cache_poison_or_cross_scope_fails', 'test_twi_partial_oracle_preparation_always_has_an_owned_cleanup'), 'Makefile': ('@$(MAKE) test-raster-twi-online-runner', 'test_online_twi_fixture.py', 'scripts/test/raster-twi-persistence-online_test.py', 'scripts/test/online-raster-twi-fixture_test.py', 'scripts/test/online-hosted-raster-twi-gate_test.py', 'test_twi_requires_manual_nfs_full_oracle_archive_and_regressions'), 'scripts/test/online-raster-twi-fixture_test.py': ('test_private_anchor_rejects_omission_tampering_and_cross_run_before_runtime', 'test_partial_host_preparation_cleanup_is_mandatory_and_bound'), 'scripts/test/raster-twi-persistence-online_test.py': ('test_complete_14_success_52_bilingual_failures_833_full_checks_and_archive', 'test_role_bound_lineage_rejects_swaps_missing_extra_duplicates_and_operations', 'test_signed_counts_empty_results_flags_and_original_cog_receipts_are_mandatory', 'test_oracle_failures_cleanup_and_preserve_first_failure', 'test_cache_and_timing_proofs_cannot_be_omitted_or_rebound')}
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('TWI persistence contract is incomplete: '+relative)
    for relative,key,size in [('business/scripts/online-raster-twi-fixture.py','CASES',14),('scripts/test/raster-twi-persistence-online.py','FAILURES',26)]:
        tree=ast.parse((repository/relative).read_text());values=[n.value for n in tree.body if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id==key for t in n.targets)]
        if len(values)!=1 or len(ast.literal_eval(values[0]))!=size:raise RegistrationError('TWI fixed 14-success/52-bilingual-failure matrix changed')



    expected_cases={'degree.cog.tif': ('area.tif', 'slope-degree.tif', 'degree', 'direct'), 'percent.cog.tif': ('area.tif', 'slope-percent.tif', 'percent', 'direct'), 'foot.cog.tif': ('area-foot.tif', 'slope-foot.tif', 'degree', 'direct'), 'us-foot.cog.tif': ('area-us.tif', 'slope-us.tif', 'percent', 'direct'), 'tiny.cog.tif': ('area.tif', 'slope-tiny.tif', 'degree', 'direct'), 'classes.cog.tif': ('area.tif', 'slope-classes.tif', 'percent', 'reclassify'), 'dem-degree.cog.tif': ('dem.tif', None, 'degree', 'dem'), 'dem-percent.cog.tif': ('dem.tif', None, 'percent', 'dem'), 'filled.cog.tif': ('bowl.tif', None, 'degree', 'fill'), 'int64.cog.tif': ('area-int64.tif', 'slope-degree.tif', 'degree', 'direct'), 'uint64.cog.tif': ('area-uint64.tif', 'slope-degree.tif', 'degree', 'direct'), 'invalid.cog.tif': ('area-invalid.tif', 'slope-degree.tif', 'degree', 'direct'), 'disjoint.cog.tif': ('area-disjoint.tif', 'slope-disjoint.tif', 'degree', 'direct'), 'zero-slope.cog.tif': ('area.tif', 'slope-zero.tif', 'degree', 'direct')}
    fixture_tree=ast.parse((repository/'business/scripts/online-raster-twi-fixture.py').read_text())
    actual_cases=next(ast.literal_eval(n.value)for n in fixture_tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id=='CASES'for t in n.targets))
    if actual_cases!=expected_cases:raise RegistrationError('TWI exact source/unit/stage matrix changed')
    expected_targets={'degree.cog.tif','percent.cog.tif','foot.cog.tif','us-foot.cog.tif','tiny.cog.tif','classes.cog.tif','dem-degree.cog.tif','dem-percent.cog.tif','filled.cog.tif','int64.cog.tif','uint64.cog.tif','invalid.cog.tif','disjoint.cog.tif','zero-slope.cog.tif'}
    expected_failures={'area_band','slope_band','area_unit_missing','area_unit_wrong','slope_unit','slope_unit_conflict','zero_area','negative_area','negative_slope','angle90','size','origin','crs','rectangular','rotated','south_up','geographic','no_crs','no_grid','area_alpha','slope_alpha','complex','area_scale','area_offset','slope_scale','slope_offset'}
    for relative,key,expected in [('business/scripts/online-raster-twi-fixture.py','CASES',expected_targets),('scripts/test/raster-twi-persistence-online.py','FAILURES',expected_failures)]:
        tree=ast.parse((repository/relative).read_text());values=[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]
        if len(values)!=1 or set(ast.literal_eval(values[0]))!=expected:raise RegistrationError('TWI exact first-phase matrix identity changed')


def validate_raster_focal_percentile(repository: Path, registered: set[str]) -> None:
    import ast
    suite='raster-focal-percentile-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-focal-percentile-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-focal-percentile-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-focal-percentile-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or '${{ runner.temp }}/addp-online-${{ github.run_id }}/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('Focal percentile requires an independent manual Hosted NFS job and complete archive upload')
    make=(repository/'Makefile').read_text()
    if 'test_online_focal_percentile_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('Focal percentile Native fixture omitted from standard GeoPython recipe')
    required={'scripts/test/online-hosted-raster-focal-percentile-gate.sh': ('start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'online-raster-focal-percentile-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare'), 'scripts/test/raster-focal-percentile-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'validate_scan(scanned,scan,identifier', 'raster.strict_sizes', 'validate_target_item(item,expectation', 'raster.wait_lineage', 'scan_ids(client)!=before_scans', 'source_edges(client', 'rasterize.check_physical(native,previous)', 'for name in fixture.TARGETS', 'for problem in FAILURES', "for language in ('zh-cn','en')", 'SubmittedLanguageClient', 'invalid_diagnostic', 'raster_focal_statistics', 'input.source', 'output.save', 'source_bindings', 'archived_cogs', 'incomplete fixed PERCENTILE matrix', 'oracle_prepare', 'oracle_cleanup', 'all 52 calls and 712 full target checks are required', 'ADDP_ONLINE_FOCAL_PERCENTILE_ORACLE_ANCHOR_SHA256', 'query language rewrote stored failure', "maximum=36 if name=='negative'", 'len(self.stages)>=105', 'Meta fabricated missing spatial facts', 'actual endpoint/median comparison evidence differs'), 'business/scripts/online-raster-focal-percentile-fixture.py': ('NFSExport', 'owned_root', 'expected_pixels', 'expected_inputs', 'from fractions import Fraction', 'position=(len(ordered)-1)*Fraction(float(percentile))/100', 'scalar_value(sample,percentile)', 'source file changed', 'runtime_workspaces_removed', 'GDAL_TIFF_INTERNAL_MASK', 'full_check=True', 'nearest_overviews', 'archive_cogs', 'percentile-cogs-manifest.json', 'previous archived COG changed', 'previous COG manifest inventory differs', 'archived COG byte identity differs', 'def prepare_oracle', 'def load_expected', 'def cleanup_oracle', 'oracle anchor is required', 'oracle cache hash changed', 'allow_pickle=False', 'independent_fraction_oracle_verified', 'linear_position_verified', 'explicit_selected_band_verified', 'percentile_encoding_verified', 'missing source CRS was fabricated', 'actual percentile endpoint or median control differs', 'RTOL=4e-15', 'ATOL=0.'), 'engines/geopython-workflow/test_online_focal_percentile_fixture.py': ('test_percentile_persistence_real_gdal_sixteen_chains_complete_controls_pixels_masks_and_cogs', 'test_percentile_persistence_thirty_six_accurate_submitted_language_failures_no_publication', 'test_percentile_complex_alpha_is_separate_native_mixed_mem_evidence_only', 'test_percentile_persistence_oracle_rejects_physical_corruption', 'one_ulp', 'test_percentile_oracle_rejects_wrong_rank_precision_scale_and_coverage', 'nearest_rank', 'median_instead', 'implicit_scale', 'zero_discarded', 'hole_filled', 'test_percentile_original_cog_archive_cannot_change_or_silently_repair', 'test_percentile_worker_stdin_is_repository_independent_and_sources_remain_immutable', 'test_percentile_cached_expectation_never_caches_actual_pixels_or_rebuilds_on_hit', 'test_percentile_oracle_cache_poison_or_cross_scope_fails', 'test_percentile_partial_oracle_preparation_always_has_an_owned_cleanup'), 'Makefile': ('@$(MAKE) test-raster-focal-percentile-online-runner', 'test_online_focal_percentile_fixture.py', 'scripts/test/raster-focal-percentile-persistence-online_test.py', 'scripts/test/online-raster-focal-percentile-fixture_test.py', 'scripts/test/online-hosted-raster-focal-percentile-gate_test.py', 'test_focal_percentile_requires_manual_nfs_full_oracle_archive_and_regressions'), 'scripts/test/online-raster-focal-percentile-fixture_test.py': ('test_private_anchor_rejects_omission_tampering_and_cross_run_before_runtime', 'test_partial_host_preparation_cleanup_is_mandatory_and_bound'), 'scripts/test/raster-focal-percentile-persistence-online_test.py': ('test_complete_16_success_36_submitted_language_failures_712_full_checks_and_archive', 'test_role_bound_lineage_rejects_swaps_missing_extra_duplicates_and_operations', 'test_signed_counts_empty_results_flags_and_original_cog_receipts_are_mandatory', 'test_oracle_failures_cleanup_and_preserve_first_failure', 'test_cache_and_timing_proofs_cannot_be_omitted_or_rebound', 'test_unlocated_artifact_and_meta_reject_fabricated_location')}
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):
            raise RegistrationError('Focal percentile acceptance contract incomplete: '+relative)
    for relative,key,expected in [('business/scripts/online-raster-focal-percentile-fixture.py','CASES',{'p0-3.cog.tif': ('values.tif', 3, 'percentile', 0, 'direct'), 'min-3.cog.tif': ('values.tif', 3, 'min', None, 'direct'), 'p100-5.cog.tif': ('values.tif', 5, 'percentile', 100, 'direct'), 'max-5.cog.tif': ('values.tif', 5, 'max', None, 'direct'), 'p50-3.cog.tif': ('values.tif', 3, 'percentile', 50, 'direct'), 'median-3.cog.tif': ('values.tif', 3, 'median', None, 'direct'), 'p50-5.cog.tif': ('values.tif', 5, 'percentile', 50, 'direct'), 'median-5.cog.tif': ('values.tif', 5, 'median', None, 'direct'), 'p25-3.cog.tif': ('values.tif', 3, 'percentile', 25, 'direct'), 'p75-5.cog.tif': ('values.tif', 5, 'percentile', 75, 'direct'), 'p33-5.cog.tif': ('values.tif', 5, 'percentile', 33.3, 'direct'), 'reclassified.cog.tif': ('values.tif', 3, 'percentile', 90, 'reclassify'), 'int64.cog.tif': ('int64.tif', 3, 'percentile', 25, 'direct'), 'uint64.cog.tif': ('uint64.tif', 5, 'percentile', 75, 'direct'), 'unlocated.cog.tif': ('unlocated.tif', 5, 'percentile', 25, 'direct'), 'invalid.cog.tif': ('invalid.tif', 3, 'percentile', 50, 'direct')}),
                                  ('business/scripts/online-raster-focal-percentile-fixture.py','COMPARISONS',(('p0-3.cog.tif', 'min-3.cog.tif'), ('p100-5.cog.tif', 'max-5.cog.tif'), ('p50-3.cog.tif', 'median-3.cog.tif'), ('p50-5.cog.tif', 'median-5.cog.tif'))),
                                  ('scripts/test/raster-focal-percentile-persistence-online.py','FAILURES',('percentile_missing', 'percentile_null', 'percentile_boolean', 'percentile_string', 'percentile_array', 'percentile_low', 'percentile_high', 'unused', 'method', 'window_value', 'window_boolean', 'window_string', 'band', 'band_zero', 'band_boolean', 'band_string', 'alpha', 'complex'))]:
        tree=ast.parse((repository/relative).read_text())
        values=[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]
        if len(values)!=1 or ast.literal_eval(values[0])!=expected:
            raise RegistrationError('Focal percentile fixed 16-success/36-failure matrix changed')


def validate_raster_sieve(repository: Path, registered: set[str]) -> None:
    import ast
    suite='raster-sieve-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-sieve-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-sieve-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-sieve-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or '${{ runner.temp }}/addp-online-${{ github.run_id }}/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('SIEVE requires a manual isolated Hosted NFS job and whole original-COG evidence upload')
    make=(repository/'Makefile').read_text()
    if not source_contract_contains(make, 'test-geopython-workflow:', repository) or 'test_online_sieve_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('SIEVE Native fixture must run in the standard GeoPython recipe')
    required={'scripts/test/online-hosted-raster-sieve-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'online-raster-sieve-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare'), 'scripts/test/raster-sieve-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'validate_scan(scanned,scan,identifier', 'raster.strict_sizes', 'validate_target_item(item,expectation', 'raster.wait_lineage', 'scan_ids(client)!=before_scans', 'source_edges(client', 'rasterize.check_physical(native,previous)', 'for name in fixture.TARGETS', 'for problem in FAILURES', "for language in ('zh-cn','en')", 'SubmittedLanguageClient', 'invalid_diagnostic', 'raster_sieve', 'input.source', 'output.save', 'source_bindings', 'archived_cogs', 'incomplete fixed SIEVE matrix', 'oracle_prepare', 'oracle_cleanup', 'all 34 calls and 189 full target checks are required', 'physical_verification_calls', 'full_target_checks', 'ADDP_ONLINE_SIEVE_ORACLE_ANCHOR_SHA256', 'query language rewrote stored failure', "maximum=28 if name=='negative'", 'len(self.stages)>=57', 'Meta fabricated missing spatial facts'), 'business/scripts/online-raster-sieve-fixture.py': ('NFSExport', 'owned_root', 'expected_pixels', 'expected_inputs', 'from collections import deque', 'len(pixels)<minimum', 'len(components[other][1])>=minimum', 'donor_ids=set()', 'np.array_equal(actual,expected,equal_nan=True)', 'source file changed', 'runtime_workspaces_removed', 'GDAL_TIFF_INTERNAL_MASK', 'full_check=True', 'nearest_overviews', 'archive_cogs', 'sieve-cogs-manifest.json', 'previous archived COG changed', 'previous COG manifest inventory differs', 'archived COG byte identity differs', 'def prepare_oracle', 'def load_expected', 'def cleanup_oracle', 'oracle anchor is required', 'oracle cache hash changed', 'allow_pickle=False', 'independent_bfs_oracle_verified', 'frozen_components_verified', 'explicit_connectivity_verified', 'classification_encoding_verified', 'missing source CRS was fabricated'), 'engines/geopython-workflow/test_online_sieve_fixture.py': ('test_sieve_persistence_real_gdal_six_chains_exact_pixels_mask_and_original_cogs', 'test_sieve_persistence_twenty_eight_accurate_bilingual_failures_no_publication', 'test_sieve_persistence_oracle_rejects_physical_corruption', 'one_ulp', 'test_sieve_independent_oracle_rejects_wrong_semantics', 'wrong_connectivity', 'threshold_inclusive', 'class_global_count', 'small_transit', 'iterated_growth', 'nodata_fill', 'no_donor_replaced', 'implicit_scale', 'zero_discarded', 'test_sieve_original_cog_archive_cannot_change_or_silently_repair', 'test_sieve_worker_stdin_is_repository_independent_and_sources_remain_immutable', 'test_sieve_cached_expectation_never_caches_actual_pixels_or_rebuilds_on_hit', 'test_sieve_oracle_cache_poison_or_cross_scope_fails', 'test_sieve_partial_oracle_preparation_always_has_an_owned_cleanup'), 'Makefile': ('@$(MAKE) test-raster-sieve-online-runner', 'test_online_sieve_fixture.py', 'scripts/test/raster-sieve-persistence-online_test.py', 'scripts/test/online-raster-sieve-fixture_test.py', 'scripts/test/online-hosted-raster-sieve-gate_test.py', 'test_sieve_requires_manual_nfs_full_oracle_archive_and_regressions'), 'scripts/test/online-raster-sieve-fixture_test.py': ('test_private_anchor_rejects_omission_tampering_and_cross_run_before_runtime', 'test_partial_host_preparation_cleanup_is_mandatory_and_bound'), 'scripts/test/raster-sieve-persistence-online_test.py': ('test_complete_6_success_28_bilingual_failures_189_full_checks_and_archive', 'test_role_bound_lineage_rejects_swaps_missing_extra_duplicates_and_operations', 'test_signed_counts_empty_results_flags_and_original_cog_receipts_are_mandatory', 'test_oracle_failures_cleanup_and_preserve_first_failure', 'test_cache_and_timing_proofs_cannot_be_omitted_or_rebound', 'test_unlocated_artifact_and_meta_reject_fabricated_location')}
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('Sieve persistence contract is incomplete: '+relative)
    expected_cases={'four.cog.tif': ('classes.tif', 5, 4, 'direct'), 'eight.cog.tif': ('classes.tif', 5, 8, 'direct'), 'reclassified.cog.tif': ('classes.tif', 5, 4, 'reclassify'), 'identity.cog.tif': ('classes.tif', 1, 4, 'direct'), 'no-donor.cog.tif': ('unlocated.tif', 5, 4, 'direct'), 'invalid.cog.tif': ('invalid.tif', 5, 8, 'direct')}
    expected_faults=('band', 'threshold_zero', 'threshold_negative', 'threshold_fractional', 'threshold_boolean', 'connectivity_value', 'connectivity_fractional', 'connectivity_boolean', 'fractional', 'unsafe', 'int64', 'uint64', 'alpha', 'complex')
    for relative,key,expected in [('business/scripts/online-raster-sieve-fixture.py','CASES',expected_cases),('scripts/test/raster-sieve-persistence-online.py','FAILURES',expected_faults)]:
        tree=ast.parse((repository/relative).read_text());values=[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]
        if len(values)!=1 or ast.literal_eval(values[0])!=expected:raise RegistrationError('Sieve exact six-success/twenty-eight-bilingual matrix changed')


def validate_raster_spi(repository: Path, registered: set[str]) -> None:
    import ast
    suite='raster-spi-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-spi-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-spi-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-spi-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or '${{ runner.temp }}/addp-online-${{ github.run_id }}/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('SPI requires a manual isolated Hosted NFS job and whole original-COG evidence upload')
    make=(repository/'Makefile').read_text()
    if not source_contract_contains(make, 'test-geopython-workflow:', repository) or 'test_online_spi_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('SPI Native fixture must run in the standard GeoPython recipe')
    required={'scripts/test/online-hosted-raster-spi-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'online-raster-spi-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare'), 'scripts/test/raster-spi-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'validate_scan(scanned,scan,identifier', 'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans', 'source_edges(client', 'rasterize.check_physical(native,previous)', 'for name in fixture.TARGETS', 'for problem in FAILURES', "for language in ('zh-cn','en')", 'SubmittedLanguageClient', 'invalid_diagnostic', 'raster_spi', 'input.area', 'input.slope', 'input.source', 'output.save', 'source_bindings', 'archived_cogs', 'incomplete fixed SPI matrix', 'oracle_prepare', 'oracle_cleanup', 'all 66 calls and 833 full target checks are required', 'physical_verification_calls', 'full_target_checks', 'ADDP_ONLINE_SPI_ORACLE_ANCHOR_SHA256', 'query language rewrote stored failure', 'fixed_area_over_width_verified', 'no_slope_threshold_verified', "maximum=52 if name=='negative'", 'len(self.stages)>=113', "if fact['negative_pixels']", 'flat SPI must stay valid zero'), 'business/scripts/online-raster-spi-fixture.py': ('NFSExport', 'owned_root', 'expected_pixels', 'expected_inputs', 'context.prec=200', 'for odd in range(599,0,-2)', 'denominator=Decimal(odd)-x*x/denominator', 'Decimal(a)/d*tangents[b]', 'measurement_width', 'np.array_equal(actual,expected,equal_nan=True)', 'source file changed', 'runtime_workspaces_removed', 'GDAL_TIFF_INTERNAL_MASK', 'full_check=True', 'nearest_overviews', 'int64.cog.tif', 'uint64.cog.tif', 'disjoint.cog.tif', 'zero-slope.cog.tif', 'archive_cogs', 'spi-cogs-manifest.json', 'previous archived COG changed', 'previous COG manifest inventory differs', 'archived COG byte identity differs', 'def prepare_oracle', 'def load_expected', 'def cleanup_oracle', 'oracle anchor is required', 'oracle cache hash changed', 'allow_pickle=False', 'joint_coverage_verified', 'metre_unit_verified', 'fixed_area_over_width_verified', 'no_slope_threshold_verified', 'legal_zero_verified', 'complete=valid', "band.GetUnitType()!='metre'", 'Decimal(a)/d*tangents[b]'), 'engines/geopython-workflow/test_online_spi_fixture.py': ('test_spi_persistence_real_gdal_fourteen_chains_exact_pixels_mask_and_original_cogs', 'test_spi_persistence_fifty_two_accurate_bilingual_failures_no_publication', 'test_spi_persistence_oracle_rejects_physical_corruption', 'one_ulp', 'test_spi_independent_oracle_rejects_wrong_semantics', 'width_multiplied', 'count_as_area', 'logarithm', 'tiny_threshold', 'test_spi_original_cog_archive_cannot_change_or_silently_repair', 'test_spi_worker_stdin_is_repository_independent_and_sources_remain_immutable', 'test_spi_cached_expectation_never_caches_actual_pixels_or_rebuilds_on_hit', 'test_spi_oracle_cache_poison_or_cross_scope_fails', 'test_spi_partial_oracle_preparation_always_has_an_owned_cleanup', 'zero_slope_nodata', 'negative_index', 'percent_not_divided'), 'Makefile': ('@$(MAKE) test-raster-spi-online-runner', 'test_online_spi_fixture.py', 'scripts/test/raster-spi-persistence-online_test.py', 'scripts/test/online-raster-spi-fixture_test.py', 'scripts/test/online-hosted-raster-spi-gate_test.py', 'test_spi_requires_manual_nfs_full_oracle_archive_and_regressions'), 'scripts/test/online-raster-spi-fixture_test.py': ('test_private_anchor_rejects_omission_tampering_and_cross_run_before_runtime', 'test_partial_host_preparation_cleanup_is_mandatory_and_bound'), 'scripts/test/raster-spi-persistence-online_test.py': ('test_complete_14_success_52_bilingual_failures_833_full_checks_and_archive', 'test_role_bound_lineage_rejects_swaps_missing_extra_duplicates_and_operations', 'test_signed_counts_empty_results_flags_and_original_cog_receipts_are_mandatory', 'test_oracle_failures_cleanup_and_preserve_first_failure', 'test_cache_and_timing_proofs_cannot_be_omitted_or_rebound')}
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('SPI persistence contract is incomplete: '+relative)
    for relative,key,size in [('business/scripts/online-raster-spi-fixture.py','CASES',14),('scripts/test/raster-spi-persistence-online.py','FAILURES',26)]:
        tree=ast.parse((repository/relative).read_text());values=[n.value for n in tree.body if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id==key for t in n.targets)]
        if len(values)!=1 or len(ast.literal_eval(values[0]))!=size:raise RegistrationError('SPI fixed 14-success/52-bilingual-failure matrix changed')



    expected_cases={'degree.cog.tif': ('area.tif', 'slope-degree.tif', 'degree', 'direct'), 'percent.cog.tif': ('area.tif', 'slope-percent.tif', 'percent', 'direct'), 'foot.cog.tif': ('area-foot.tif', 'slope-foot.tif', 'degree', 'direct'), 'us-foot.cog.tif': ('area-us.tif', 'slope-us.tif', 'percent', 'direct'), 'tiny.cog.tif': ('area.tif', 'slope-tiny.tif', 'degree', 'direct'), 'classes.cog.tif': ('area.tif', 'slope-classes.tif', 'percent', 'reclassify'), 'dem-degree.cog.tif': ('dem.tif', None, 'degree', 'dem'), 'dem-percent.cog.tif': ('dem.tif', None, 'percent', 'dem'), 'filled.cog.tif': ('bowl.tif', None, 'degree', 'fill'), 'int64.cog.tif': ('area-int64.tif', 'slope-degree.tif', 'degree', 'direct'), 'uint64.cog.tif': ('area-uint64.tif', 'slope-degree.tif', 'degree', 'direct'), 'invalid.cog.tif': ('area-invalid.tif', 'slope-degree.tif', 'degree', 'direct'), 'disjoint.cog.tif': ('area-disjoint.tif', 'slope-disjoint.tif', 'degree', 'direct'), 'zero-slope.cog.tif': ('area.tif', 'slope-zero.tif', 'degree', 'direct')}
    fixture_tree=ast.parse((repository/'business/scripts/online-raster-spi-fixture.py').read_text())
    actual_cases=next(ast.literal_eval(n.value)for n in fixture_tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id=='CASES'for t in n.targets))
    if actual_cases!=expected_cases:raise RegistrationError('SPI exact source/unit/stage matrix changed')
    expected_targets={'degree.cog.tif','percent.cog.tif','foot.cog.tif','us-foot.cog.tif','tiny.cog.tif','classes.cog.tif','dem-degree.cog.tif','dem-percent.cog.tif','filled.cog.tif','int64.cog.tif','uint64.cog.tif','invalid.cog.tif','disjoint.cog.tif','zero-slope.cog.tif'}
    expected_failures={'area_band','slope_band','area_unit_missing','area_unit_wrong','slope_unit','slope_unit_conflict','zero_area','negative_area','negative_slope','angle90','size','origin','crs','rectangular','rotated','south_up','geographic','no_crs','no_grid','area_alpha','slope_alpha','complex','area_scale','area_offset','slope_scale','slope_offset'}
    for relative,key,expected in [('business/scripts/online-raster-spi-fixture.py','CASES',expected_targets),('scripts/test/raster-spi-persistence-online.py','FAILURES',expected_failures)]:
        tree=ast.parse((repository/relative).read_text());values=[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]
        if len(values)!=1 or set(ast.literal_eval(values[0]))!=expected:raise RegistrationError('SPI exact first-phase matrix identity changed')


def validate_raster_plan_curvature(repository: Path, registered: set[str]) -> None:
    suite='raster-plan-curvature-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-plan-curvature-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-plan-curvature-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-plan-curvature-persistence'" not in workflow
        or 'cancel-in-progress: false' not in job.group('body') or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('DEM curvature requires a manual isolated Hosted NFS job')
    make=(repository/'Makefile').read_text()
    if not source_contract_contains(make, 'test-geopython-workflow:', repository) or 'test_online_plan_curvature_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('curvature Native fixture must run in the standard GeoPython recipe')
    required={
      'scripts/test/online-hosted-raster-plan-curvature-gate.sh':('source "$ROOT_DIR/scripts/utils/hosted-online.sh"','start_online_geopython_runtime','--mount','--suite raster-workflow','--wait-protection-owners develop,manager','online-raster-plan-curvature-fixture.py stop','unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN','ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
      'scripts/test/raster-plan-curvature-persistence-online.py':('raster.validate_identity','raster.EDITOR_PERMISSIONS','fixture.SOURCES','validate_scan(scanned,scan,identifier','raster.strict_sizes','raster.validate_target_item','fixture.artifact_expectation(name)','raster.wait_lineage','scan_ids(client)!=before_scans','source_edges(client','rasterize.check_physical(native,previous)','for name in fixture.TARGETS','for problem in FAILURES',"for language in ('zh-cn','en')",'SubmittedLanguageClient','invalid_diagnostic','case_inputs(name)','raster_curvature','elevation_unit','overview_count','query language rewrote stored failure','curvature_method','flat_zero_verified','zero_gradient_nodata_verified','mean_control_verified','mean_controls_equal','directional_identity_verified','directional_identity_pixels','profile_control_verified','tiny_gradient_verified','plan_distinction_verified','circle_verified','height_invariance_verified','plan_tangential_identity_verified','plan_tangential_identity_pixels','tangential_control_verified','undefined_stationary_centres'),
      'business/scripts/online-raster-plan-curvature-fixture.py':('NFSExport','owned_root','expected_pixels','expected_dem','np.linalg.lstsq','np.linalg.solve','SOURCE_UNITS','small.cog.tif','sparse.cog.tif','filled.cog.tif','reclassified.cog.tif','1/metre','np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)','source file changed','runtime_workspaces_removed','GDAL_TIFF_INTERNAL_MASK','full_check=True','nearest_overviews','semantic_masks','math.fsum','np.einsum','mean-default.cog.tif','mean-explicit.cog.tif','stationary.cog.tif','flat.cog.tif','profile-control.cog.tif','mode_controls','height-transformed.cog.tif','tangential-control.cog.tif','circle inverse radius','positive height scale and shift','saved plan and tangential relation','saved directional curvatures do not sum to twice the mean'),
      'engines/geopython-workflow/test_online_plan_curvature_fixture.py':('test_plan_curvature_persistence_real_gdal_fifteen_cog_chains_and_complete_pixels','test_plan_curvature_persistence_oracle_rejects_corruption','test_plan_curvature_persistence_twenty_six_failures_no_publication','forced_centre','laplacian','wrong_units'),
      'Makefile':('@$(MAKE) test-raster-plan-curvature-online-runner','scripts/test/raster-plan-curvature-persistence-online_test.py','scripts/test/online-raster-plan-curvature-fixture_test.py','scripts/test/online-hosted-raster-plan-curvature-gate_test.py','test_plan_curvature_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('DEM curvature persistence contract is incomplete: '+relative)


def validate_raster_hand_d8_profile(repository: Path, registered: set[str]) -> None:
    suite='raster-hand-d8-persistence'
    if suite not in registered:return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-hand-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-hand-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-hand-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 HAND requires a manual isolated Hosted NFS job')
    make_path=repository/'Makefile'
    if not make_path.is_file() or 'test-geopython-workflow:' not in make_path.read_text():
        raise RegistrationError('HAND requires the standard GeoPython Native recipe')
    native=make_path.read_text().split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]
    if 'test_online_hand_d8_fixture.py' not in native:
        raise RegistrationError('HAND Native fixture must run in the standard GeoPython recipe')
    required = {'scripts/test/online-hosted-raster-hand-d8-gate.sh': ('source '
                                                           '"$ROOT_DIR/scripts/utils/hosted-online.sh"',
                                                           'start_online_geopython_runtime',
                                                           '--mount',
                                                           '--suite raster-workflow',
                                                           '--wait-protection-owners develop,manager',
                                                           'online-raster-hand-d8-fixture.py stop',
                                                           'unset '
                                                           'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
                                                           'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
     'scripts/test/raster-hand-d8-persistence-online.py': ('raster.validate_identity',
                                                           'raster.EDITOR_PERMISSIONS',
                                                           'fixture.SOURCES',
                                                           'validate_scan(scanned,scan,identifier',
                                                           'raster.strict_sizes',
                                                           'raster.validate_target_item',
                                                           'fixture.artifact_expectation(name)',
                                                           'raster.wait_lineage',
                                                           'scan_ids(client)!=before_scans',
                                                           'source_edges(client',
                                                           'rasterize.check_physical(native,previous)',
                                                           'for name in fixture.TARGETS',
                                                           'for problem in FAILURES',
                                                           "for language in ('zh-cn','en')",
                                                           'SubmittedLanguageClient',
                                                           'invalid_diagnostic',
                                                           'case_inputs(name)',
                                                           'stream_raster',
                                                           'stream_band',
                                                           'elevation_raster',
                                                           'elevation_band',
                                                           'elevation_unit',
                                                           'uphill_before',
                                                           'uphill_after',
                                                           'uphill_empty',
                                                           'mixed_original',
                                                           'non_increasing_elevation_verified',
                                                           'flat_zero_verified',
                                                           'first_hit_verified',
                                                           'unreachable_nodata_verified',
                                                           'river_zero_verified',
                                                           'metre_unit_verified',
                                                           'nearest_overviews_verified'),
     'business/scripts/online-raster-hand-d8-fixture.py': ('NFSExport',
                                                           'owned_root',
                                                           'expected_pixels',
                                                           'walked_paths',
                                                           'hand_evidence',
                                                           'SOURCE_UNITS',
                                                           'empty.cog.tif',
                                                           'invalid.cog.tif',
                                                           'feet.cog.tif',
                                                           'survey-feet.cog.tif',
                                                           'np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)',
                                                           'source file changed',
                                                           'runtime_workspaces_removed',
                                                           'GDAL_TIFF_INTERNAL_MASK',
                                                           'full_check=True'),
     'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
     'engines/geopython-workflow/test_online_hand_d8_fixture.py': ('test_hand_d8_persistence_real_gdal_eight_cog_chains_and_complete_pixels',
                                                                   'test_hand_d8_persistence_oracle_rejects_corruption',
                                                                   'test_hand_d8_persistence_forty_two_failures_no_publication'),
     'Makefile': ('@$(MAKE) test-raster-hand-d8-online-runner',
                  'test_online_hand_d8_fixture.py',
                  'scripts/test/raster-hand-d8-persistence-online_test.py',
                  'scripts/test/online-raster-hand-d8-fixture_test.py',
                  'scripts/test/online-hosted-raster-hand-d8-gate_test.py',
                  'test_hand_d8_requires_manual_nfs_full_pixels_and_regressions')}
    for relative,fragments in required.items():
        path=repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 HAND persistence contract is incomplete: {relative}')


def validate_raster_flow_length_upstream_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-flow-length-upstream-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-flow-length-upstream-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-flow-length-upstream-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-flow-length-upstream-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 longest upstream flow length requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-flow-length-upstream-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-flow-length-upstream-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-flow-length-upstream-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'metre_unit_verified', 'sources_zero', 'longest_path_verified', 'confluence_max_verified', 'outlets_receive_upstream', 'centre_steps_verified', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit', "'operator':'raster_flow_length_d8'", "'method':'upstream_longest'"),
        'business/scripts/online-raster-flow-length-upstream-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'distance_evidence', 'empty-length.cog.tif', 'feet-length.cog.tif', 'survey-feet-length.cog.tif', 'SOURCE_UNITS', 'np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_flow_length_upstream_d8_fixture.py': (
            'test_flow_length_upstream_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels',
            'test_flow_length_upstream_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-flow-length-upstream-d8-online-runner',
            'test_online_flow_length_upstream_d8_fixture.py',
            'scripts/test/raster-flow-length-upstream-d8-persistence-online_test.py',
            'scripts/test/online-raster-flow-length-upstream-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-flow-length-upstream-d8-gate_test.py',
            'test_flow_length_upstream_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 longest upstream flow length persistence contract is incomplete: {relative}')

def validate_raster_stream_order_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-stream-order-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-stream-order-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-stream-order-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-stream-order-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 Strahler stream order requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-stream-order-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-stream-order-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-stream-order-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'strahler_verified', 'stream_closure_verified', 'confluence_verified', 'zero_background_verified', 'case_inputs(name)', 'stream_raster', 'stream_band', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-stream-order-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'backward_oracle', 'order_evidence', 'empty-order.cog.tif', 'invalid-order.cog.tif', 'binary_values', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_stream_order_d8_fixture.py': (
            'test_stream_order_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels',
            'test_stream_order_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-stream-order-d8-online-runner',
            'test_online_stream_order_d8_fixture.py',
            'scripts/test/raster-stream-order-d8-persistence-online_test.py',
            'scripts/test/online-raster-stream-order-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-stream-order-d8-gate_test.py',
            'test_stream_order_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 Strahler stream order persistence contract is incomplete: {relative}')

def validate_raster_stream_shreve_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-stream-shreve-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-stream-shreve-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-stream-shreve-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-stream-shreve-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 Shreve stream magnitude requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-stream-shreve-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-stream-shreve-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-stream-shreve-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'independent_method_oracle_verified', 'headwater_conservation_verified', 'default_strahler_equivalent_verified', 'strahler_verified', 'stream_closure_verified', 'confluence_verified', 'zero_background_verified', 'case_inputs(name)', 'stream_raster', 'stream_band', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-stream-shreve-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'headwater_walk_oracle','default-strahler.cog.tif','explicit-strahler.cog.tif','backward_oracle', 'order_evidence', 'empty-shreve.cog.tif', 'invalid-shreve.cog.tif', 'binary_values', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_stream_shreve_d8_fixture.py': (
            'test_stream_shreve_d8_persistence_real_gdal_eight_cog_chains_and_complete_pixels',
            'test_stream_shreve_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-stream-shreve-d8-online-runner',
            'test_online_stream_shreve_d8_fixture.py',
            'scripts/test/raster-stream-shreve-d8-persistence-online_test.py',
            'scripts/test/online-raster-stream-shreve-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-stream-shreve-d8-gate_test.py',
            'test_stream_shreve_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 Shreve stream magnitude persistence contract is incomplete: {relative}')


def validate_raster_stream_to_vector_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-stream-to-vector-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-stream-to-vector-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-stream-to-vector-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-stream-to-vector-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 stream vectorization requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-stream-to-vector-d8-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'online-raster-stream-to-vector-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-stream-to-vector-d8-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'validate_success(execution', 'vector_persistence.validate_item', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'scan_ids(client)!=before_scans', 'rasterize.check_physical(native,previous)', 'for name in fixture.TARGETS', 'for problem in FAILURES', "for language in ('zh-cn','en')", 'SubmittedLanguageClient', 'invalid_diagnostic', 'case_inputs(name)', 'stream_raster', 'stream_band', 'elevation_unit', 'complete_geometry_attributes_verified', 'independent_edge_coverage_verified', 'terminal_confluence_verified', 'metre_lengths_verified', 'empty_schema_crs_verified', 'skipped_terminal_link_ids', 'RasterArtifact'),
        'business/scripts/online-raster-stream-to-vector-d8-fixture.py': ('NFSExport', 'owned_root', 'expected_lines', 'Counter(edges.items())', 'complete centre coordinates', 'Integer64', 'GetSpatialRef', 'source file changed', 'runtime_workspaces_removed', 'GDAL_TIFF_INTERNAL_MASK', 'isolated-streams.tif', 'mixed-streams.tif', 'foot.gpkg', 'survey.gpkg', 'empty.gpkg', 'invalid.gpkg'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_stream_to_vector_d8_fixture.py': ('test_stream_to_vector_d8_persistence_real_gdal_eight_gpkg_chains_and_complete_edges', 'test_stream_to_vector_d8_persistence_oracle_rejects_corruption', 'test_stream_to_vector_d8_persistence_twenty_precise_failures_no_publication'),
        'Makefile': ('@$(MAKE) test-raster-stream-to-vector-d8-online-runner', 'test_online_stream_to_vector_d8_fixture.py', 'scripts/test/raster-stream-to-vector-d8-persistence-online_test.py', 'scripts/test/online-raster-stream-to-vector-d8-fixture_test.py', 'scripts/test/online-hosted-raster-stream-to-vector-d8-gate_test.py', 'test_stream_to_vector_d8_requires_manual_nfs_complete_edges_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 stream vectorization persistence contract is incomplete: {relative}')


def validate_raster_stream_link_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-stream-link-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-stream-link-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-stream-link-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-stream-link-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 stream link requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-stream-link-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-stream-link-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-stream-link-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'stream_links_verified','start_order_verified','unique_coverage_verified', 'stream_closure_verified', 'confluence_verified', 'zero_background_verified', 'case_inputs(name)', 'stream_raster', 'stream_band', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-stream-link-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'backward_oracle','walk_oracle','DIRECT_STARTS', 'link_evidence', 'empty-links.cog.tif', 'invalid-links.cog.tif', 'binary_values', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_stream_link_d8_fixture.py': (
            'test_stream_link_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels',
            'test_stream_link_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-stream-link-d8-online-runner',
            'test_online_stream_link_d8_fixture.py',
            'scripts/test/raster-stream-link-d8-persistence-online_test.py',
            'scripts/test/online-raster-stream-link-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-stream-link-d8-gate_test.py',
            'test_stream_link_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 stream link persistence contract is incomplete: {relative}')



def validate_raster_flow_accumulation_weighted_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-flow-accumulation-weighted-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-flow-accumulation-weighted-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-flow-accumulation-weighted-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-flow-accumulation-weighted-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 weighted flow accumulation requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-flow-accumulation-weighted-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-flow-accumulation-weighted-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-flow-accumulation-weighted-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'weighted_sum_verified', 'self_exclusion_verified', 'mass_balance_verified', 'valid_zero_verified', 'weight_unit_verified', 'all_ones_equivalent_verified', 'case_inputs(name)', 'weights_raster', 'weights_band', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-flow-accumulation-weighted-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'walk_oracle', 'weighted_evidence', 'ones.cog.tif', 'invalid-weighted.cog.tif', 'weight_values', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_flow_accumulation_weighted_d8_fixture.py': (
            'test_flow_accumulation_weighted_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels',
            'test_flow_accumulation_weighted_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-flow-accumulation-weighted-d8-online-runner',
            'test_online_flow_accumulation_weighted_d8_fixture.py',
            'scripts/test/raster-flow-accumulation-weighted-d8-persistence-online_test.py',
            'scripts/test/online-raster-flow-accumulation-weighted-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-flow-accumulation-weighted-d8-gate_test.py',
            'test_flow_accumulation_weighted_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 weighted flow accumulation persistence contract is incomplete: {relative}')



def validate_raster_watershed_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-watershed-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-watershed-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-watershed-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-watershed-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 specified-outlet watershed requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-watershed-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-watershed-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-watershed-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'watershed_verified', 'point_self_verified', 'point_ids_verified', 'unselected_zero_verified', 'unit_cleared_verified', 'nonnested_verified', 'case_inputs(name)', 'points_raster', 'points_band', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-watershed-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'walk_oracle', 'watershed_evidence', 'reclass.cog.tif', 'dem-watershed.cog.tif', 'point_values', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_watershed_d8_fixture.py': (
            'test_watershed_d8_persistence_real_gdal_four_cog_chains_and_complete_pixels',
            'test_watershed_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-watershed-d8-online-runner',
            'test_online_watershed_d8_fixture.py',
            'scripts/test/raster-watershed-d8-persistence-online_test.py',
            'scripts/test/online-raster-watershed-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-watershed-d8-gate_test.py',
            'test_watershed_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 specified-outlet watershed persistence contract is incomplete: {relative}')





def validate_raster_subcatchments_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-subcatchments-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-subcatchments-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-subcatchments-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-subcatchments-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 incremental subcatchments requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-subcatchments-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-subcatchments-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-subcatchments-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'subcatchments_verified', 'point_self_verified', 'point_ids_verified', 'unselected_zero_verified', 'unit_cleared_verified', 'first_downstream_point_verified', 'disjoint_verified', 'partition_conservation_verified', 'nested_supported_verified', 'nonnested_reference_equal', 'full_nested', 'failure_task', 'raster_watershed_d8', 'raster_snap_pour_points', 'case_inputs(name)', 'points_raster', 'points_band', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-subcatchments-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'walk_oracle', 'subcatchments_evidence', 'reclass.cog.tif', 'dem-subcatchments.cog.tif', 'point_values', 'snap_oracle', 'SNAP_RADIUS', 'full-reference.cog.tif', 'nested.cog.tif', 'snapped.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_subcatchments_d8_fixture.py': (
            'test_subcatchments_d8_persistence_real_gdal_seven_cog_chains_and_complete_pixels',
            'test_subcatchments_d8_persistence_oracle_rejects_corruption',
            'test_subcatchments_d8_stdin_worker_is_self_contained_in_empty_directory'),
        'Makefile': ('@$(MAKE) test-raster-subcatchments-d8-online-runner',
            'test_online_subcatchments_d8_fixture.py',
            'scripts/test/raster-subcatchments-d8-persistence-online_test.py',
            'scripts/test/online-raster-subcatchments-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-subcatchments-d8-gate_test.py',
            'test_subcatchments_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'D8 incremental subcatchments persistence contract is incomplete: {relative}')





def validate_raster_snap_pour_points_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-snap-pour-points-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-snap-pour-points-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-snap-pour-points-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-snap-pour-points-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('pour-point snapping requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-snap-pour-points-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-snap-pour-points-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-snap-pour-points-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'snap_verified', 'radius_and_tie_verified', 'point_ids_verified', 'unselected_zero_verified', 'unit_cleared_verified', 'case_inputs(name)', 'points_raster', 'points_band', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-snap-pour-points-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'snap_oracle', 'snap_evidence', 'reclass.cog.tif', 'dem-snap.cog.tif', 'point_values', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_snap_pour_points_fixture.py': (
            'test_snap_pour_points_persistence_real_gdal_eight_cog_chains_and_complete_pixels',
            'test_snap_pour_points_persistence_oracle_rejects_corruption',
            'test_snap_pour_points_worker_is_self_contained_without_repository_imports'),
        'Makefile': ('@$(MAKE) test-raster-snap-pour-points-online-runner',
            'test_online_snap_pour_points_fixture.py',
            'scripts/test/raster-snap-pour-points-persistence-online_test.py',
            'scripts/test/online-raster-snap-pour-points-fixture_test.py',
            'scripts/test/online-hosted-raster-snap-pour-points-gate_test.py',
            'test_snap_pour_points_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'pour-point snapping persistence contract is incomplete: {relative}')





ZONAL_PERCENTILE_REQUIRED = {
    'scripts/test/online-hosted-raster-zonal-percentile-gate.sh': ('start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare',
        'online-raster-zonal-percentile-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
    'business/scripts/online-raster-zonal-percentile-fixture.py': ('NFSExport', 'owned_root', 'from fractions import Fraction',
        'WIDTH, HEIGHT = 1025, 513', 'position = (count-1)*Fraction(p)/100', 'sorted(samples.get(code, []))', 'np.array_equal',
        'GDAL_TIFF_INTERNAL_MASK', 'full_csv_checks', 'full_source_pixels_verified', 'runtime_workspaces_removed', 'archive_files',
        'previous archive was deleted', 'previous archived bytes changed', 'oracle cache hash changed', 'source file changed',
        'requested percentile changed original nine columns', 'source order changed percentile', 'zonal-percentile-csvs'),
    'scripts/test/raster-zonal-percentile-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS',
        'budget != 600', 'BudgetClient', 'remaining()', 'validate_preview', 'legacy.validate_success', 'lineage.collect',
        'lineage.validate(facts, wanted, public=True)', 'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage',
        'source_edges', 'readonly.no_publication', 'query language rewrote stored failure', 'previous complete CSV changed',
        'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1032', 'physical_calls != 73',
        'len(cases) != 16', 'len(failures) != 56', 'exact_cause', 'Monitor current output lineage differs'),
    'engines/geopython-workflow/test_online_zonal_percentile_fixture.py': (
        'test_zonal_percentile_sixteen_real_csvs_complete_independent_oracle_and_raw_archive',
        'test_zonal_percentile_fifty_six_exact_submitted_failures_and_no_output',
        'test_zonal_percentile_real_physical_corruption_is_rejected', 'one-ulp',
        'test_zonal_percentile_csv_archive_never_repairs_or_overwrites',
        'test_zonal_percentile_worker_stdin_frozen_oracle_poison_and_cleanup'),
    'scripts/test/raster-zonal-percentile-persistence-online_test.py': (
        'test_full_matrix_16_success_56_failures_1032_checks_and_cleanup', 'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
        'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_failures_preserve_primary_and_cleanup_error',
        'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced'),
    'scripts/utils/online_raster_lineage.py': ("'raster-zonal-percentile-persistence': 'zonal-percentile-lineage'",),
    'scripts/test/online-raster-lineage_test.py': ('test_zonal_percentile_approved_two_role_csv_facts_preserve_internal_and_public_boundaries',),
    'Makefile': ('test_online_zonal_percentile_fixture.py', '@$(MAKE) test-raster-zonal-percentile-online-runner',
        'scripts/test/raster-zonal-percentile-persistence-online_test.py', 'scripts/test/online-raster-zonal-percentile-fixture_test.py',
        'scripts/test/online-hosted-raster-zonal-percentile-gate_test.py', 'test_zonal_percentile_requires_exact_csv_matrix_budget_and_regressions'),
}


def validate_raster_zonal_percentile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-zonal-percentile-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-zonal-percentile-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-zonal-percentile-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-zonal-percentile-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('zonal percentile requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_zonal_percentile_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('zonal percentile Native omitted from standard recipe')
    for relative, fragments in ZONAL_PERCENTILE_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('zonal percentile strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-zonal-percentile-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'original.csv': ('values.tif', 'zones.tif', None, 2), 'p0.csv': ('values.tif', 'zones.tif', 0, 2), 'p100.csv': ('values.tif', 'zones.tif', 100, 2), 'p50-odd.csv': ('odd.tif', 'small-zones.tif', 50, 2), 'p50-even.csv': ('even.tif', 'small-zones.tif', 50, 2), 'p25.csv': ('values.tif', 'zones.tif', 25, 2), 'p75.csv': ('values.tif', 'zones.tif', 75, 2), 'p33.csv': ('values.tif', 'zones.tif', 33.3, 2), 'p33-reversed.csv': ('reversed.tif', 'zones.tif', 33.3, 2), 'reclassified.csv': ('values.tif', 'zones.tif', 90, 2), 'many-zones.csv': ('values.tif', 'zones.tif', 50, 1), 'int64.csv': ('int64.tif', 'large-zones.tif', 50, 2), 'uint64.csv': ('uint64.tif', 'large-zones.tif', 50, 2), 'coverage.csv': ('coverage.tif', 'coverage-zones.tif', 50, 2), 'invalid-values.csv': ('invalid.tif', 'small-zones.tif', 50, 2), 'empty-zones.csv': ('even.tif', 'empty-zones.tif', 50, 2)}
    expected_failures = ('percentile_null', 'percentile_boolean', 'percentile_string', 'percentile_array', 'percentile_object', 'percentile_low', 'percentile_high', 'values_band_zero', 'values_band_high', 'values_band_boolean', 'values_band_string', 'zones_band_zero', 'zones_band_high', 'zones_band_boolean', 'zones_band_string', 'values_complex', 'zones_complex', 'values_alpha', 'zones_alpha', 'fractional_zone', 'overflow_zone', 'missing_crs', 'different_crs', 'missing_grid', 'singular_grid', 'different_size', 'shifted_grid', 'rotated_grid')
    if (any(len(nodes) != 1 for nodes in actual.values()) or ast.literal_eval(actual['CASES'][0]) != expected_cases
        or ast.literal_eval(actual['FAILURES'][0]) != expected_failures): raise RegistrationError('zonal percentile fixed 16/56 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile')):
        raise RegistrationError('zonal percentile oracle must remain independent')


ZONAL_PERCENTILES_REQUIRED = {
    'scripts/test/online-hosted-raster-zonal-percentiles-gate.sh': ('start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare',
        'online-raster-zonal-percentiles-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
    'business/scripts/online-raster-zonal-percentiles-fixture.py': ('NFSExport', 'owned_root', 'from fractions import Fraction',
        'WIDTH, HEIGHT = 1025, 513', 'position=(count-1)*Fraction(p)/100', 'sorted(samples.get(code, []))', 'np.array_equal',
        'GDAL_TIFF_INTERNAL_MASK', 'full_csv_checks', 'full_source_pixels_verified', 'runtime_workspaces_removed', 'archive_files',
        'previous archive was deleted', 'previous archived bytes changed', 'oracle cache hash changed', 'source file changed',
        'requested percentiles changed original nine columns', 'source order changed percentiles', 'zonal-percentiles-csvs','actual batch and single percentile CSVs differ','def oracle_scope','fixture_sha256','source_files','oracle cache became writable'),
    'scripts/test/raster-zonal-percentiles-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS',
        'budget != 600', 'BudgetClient', 'remaining()', 'validate_preview', 'legacy.validate_success', 'lineage.collect',
        'lineage.validate(facts, wanted, public=True)', 'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage',
        'source_edges', 'readonly.no_publication', 'query language rewrote stored failure', 'previous complete CSV changed',
        'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1416', 'physical_calls != 97',
        'len(cases) != 16', 'len(failures) != 80', 'exact_cause', 'Monitor current output lineage differs','archive_response','archived_oracle','actual_single_controls_verified','target_metadata_after!=target_metadata_before','source_metadata_after!=before'),
    'engines/geopython-workflow/test_online_zonal_percentiles_fixture.py': (
        'test_zonal_percentiles_sixteen_real_csvs_complete_independent_oracle_and_raw_archive',
        'test_zonal_percentiles_eighty_exact_submitted_failures_and_no_output',
        'test_zonal_percentiles_real_physical_corruption_is_rejected', 'one-ulp',
        'test_zonal_percentiles_csv_archive_never_repairs_or_overwrites',
        'test_zonal_percentiles_worker_stdin_frozen_oracle_poison_and_cleanup','test_zonal_percentiles_oracle_binds_exact_source_script_and_run','test_zonal_percentiles_oracle_is_independent_of_product_and_percentile_helpers'),
    'scripts/test/raster-zonal-percentiles-persistence-online_test.py': (
        'test_full_matrix_16_success_80_failures_1416_checks_and_cleanup', 'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
        'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_failures_preserve_primary_and_cleanup_error',
        'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced','test_exactly_192_raw_terminal_responses_and_meta_snapshots','test_source_and_prior_target_metadata_cannot_change'),
    'scripts/test/online-raster-zonal-percentiles-fixture_test.py': ('test_main_archives_frozen_oracle_sources_and_sixteen_csv_original_bytes',),
    'scripts/utils/online_raster_lineage.py': ("'raster-zonal-percentiles-persistence': 'zonal-percentiles-lineage'",),
    'Makefile': ('test_online_zonal_percentiles_fixture.py', '@$(MAKE) test-raster-zonal-percentiles-online-runner',
        'scripts/test/raster-zonal-percentiles-persistence-online_test.py', 'scripts/test/online-raster-zonal-percentiles-fixture_test.py',
        'scripts/test/online-hosted-raster-zonal-percentiles-gate_test.py', 'test_zonal_percentiles_requires_exact_csv_matrix_budget_and_regressions'),
}


def validate_raster_zonal_percentiles(repository: Path, registered: set[str]) -> None:
    suite = 'raster-zonal-percentiles-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-zonal-percentiles-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-zonal-percentiles-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-zonal-percentiles-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('zonal percentiles requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_zonal_percentiles_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('zonal percentiles Native omitted from standard recipe')
    for relative, fragments in ZONAL_PERCENTILES_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('zonal percentiles strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-zonal-percentiles-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES','PERCENTILES')}
    expected_cases = {'original.csv': ('base-values.tif', 'base-zones.tif', None, 2), 'batch.csv': ('base-values.tif', 'base-zones.tif', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), 2), 'p0.csv': ('base-values.tif', 'base-zones.tif', (0.0,), 2), 'p25.csv': ('base-values.tif', 'base-zones.tif', (25.0,), 2), 'p33.csv': ('base-values.tif', 'base-zones.tif', (33.3,), 2), 'p50.csv': ('base-values.tif', 'base-zones.tif', (50.0,), 2), 'p75.csv': ('base-values.tif', 'base-zones.tif', (75.0,), 2), 'p100.csv': ('base-values.tif', 'base-zones.tif', (100.0,), 2), 'reclassified.csv': ('base-values.tif', 'base-zones.tif', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), 2), 'many-zones.csv': ('large-values.tif', 'large-zones.tif', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), 2), 'reversed.csv': ('reversed-values.tif', 'reversed-zones.tif', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), 2), 'int64.csv': ('int64.tif', 'native-zones.tif', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), 2), 'uint64.csv': ('uint64.tif', 'native-zones.tif', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), 2), 'coverage.csv': ('coverage-values.tif', 'coverage-zones.tif', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), 2), 'invalid-values.csv': ('invalid-values.tif', 'base-zones.tif', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), 2), 'empty-zones.csv': ('base-values.tif', 'empty-zones.tif', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), 2)}
    expected_failures = ('list_null', 'list_empty', 'scalar_number', 'scalar_boolean', 'scalar_string', 'scalar_object', 'item_null', 'item_boolean', 'item_string', 'item_list', 'item_object', 'item_low', 'item_high', 'duplicate', 'descending', 'values_band_zero', 'values_band_high', 'values_band_boolean', 'values_band_string', 'zones_band_zero', 'zones_band_high', 'zones_band_boolean', 'zones_band_string', 'values_alpha', 'zones_alpha', 'values_complex', 'zones_complex', 'fractional_zone', 'overflow_zone', 'missing_crs', 'different_crs', 'missing_grid', 'singular_grid', 'different_size', 'shifted_grid', 'rotated_grid', 'weights_conflict', 'weighted_stddev_conflict', 'frequency_conflict', 'frequency_null_conflict')
    if (any(len(nodes) != 1 for nodes in actual.values()) or ast.literal_eval(actual['CASES'][0]) != expected_cases
        or ast.literal_eval(actual['FAILURES'][0]) != expected_failures or ast.literal_eval(actual['PERCENTILES'][0]) != (0.0, 25.0, 33.3, 50.0, 75.0, 100.0)): raise RegistrationError('zonal percentiles fixed 16/80 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile')):
        raise RegistrationError('zonal percentiles oracle must remain independent')


ZONAL_CLASS_FREQUENCY_REQUIRED = {'business/scripts/online-raster-zonal-class-frequency-fixture.py': ('NFSExport', 'source_arrays', 'Fraction(counts[value], valid)', 'populations', 'FREQUENCY_COLUMNS', 'class_rules', 'def oracle_scope', 'source_files', 'fixture_sha256', 'GetNoDataValueAsInt64', 'GetNoDataValueAsUInt64', 'np.array_equal(band.ReadAsArray(), values, equal_nan=True)', 'archive_files', 'archived_oracle', 'oracle cache became writable', 'runtime_workspaces_removed', "with state.open('xb')"), 'scripts/test/raster-zonal-class-frequency-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'full_checks != 1160', 'len(cases) != 16', 'len(failures) != 64', 'physical_calls != 81', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'legacy.validate_success', 'validate_preview', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_csvs', 'frozen oracle archive differs'), 'scripts/test/online-hosted-raster-zonal-class-frequency-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-zonal-class-frequency-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'engines/geopython-workflow/test_online_zonal_class_frequency_fixture.py': ('test_zonal_class_frequency_sixteen_real_csvs_complete_independent_oracle_and_raw_archive', 'test_zonal_class_frequency_sixty_four_exact_submitted_failures_and_no_output', 'test_zonal_class_frequency_real_physical_corruption_is_rejected', 'test_zonal_class_frequency_csv_archive_never_repairs_or_overwrites', 'test_zonal_class_frequency_worker_stdin_frozen_oracle_poison_and_cleanup', 'diagnostic-zero', 'denominator', 'one-ulp'), 'scripts/test/raster-zonal-class-frequency-persistence-online_test.py': ('test_full_matrix_16_success_64_failures_1160_checks_and_cleanup', 'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_exact_thirty_two_failures_and_reclassified_original_inputs', 'test_meta_and_frozen_archive_are_not_allowed_to_change'), 'scripts/utils/online_raster_lineage.py': ("'raster-zonal-class-frequency-persistence': 'zonal-class-frequency-lineage'",), 'scripts/test/online-raster-lineage_test.py': ('test_zonal_class_frequency_approved_two_role_csv_facts_preserve_internal_and_public_boundaries',), 'Makefile': ('test_online_zonal_class_frequency_fixture.py', '@$(MAKE) test-raster-zonal-class-frequency-online-runner', 'scripts/test/raster-zonal-class-frequency-persistence-online_test.py', 'scripts/test/online-raster-zonal-class-frequency-fixture_test.py', 'scripts/test/online-hosted-raster-zonal-class-frequency-gate_test.py', 'test_zonal_class_frequency_requires_exact_csv_matrix_budget_and_regressions')}

def validate_raster_zonal_class_frequency(repository: Path, registered: set[str]) -> None:
    suite = 'raster-zonal-class-frequency-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-zonal-class-frequency-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-zonal-class-frequency-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-zonal-class-frequency-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('zonal class frequency requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_zonal_class_frequency_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('zonal class frequency Native omitted from standard recipe')
    for relative, fragments in ZONAL_CLASS_FREQUENCY_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('zonal class frequency strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-zonal-class-frequency-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'original.csv': ('values.tif', 'zones.tif', 'summary-default', 2), 'summary.csv': ('values.tif', 'zones.tif', 'summary', 2), 'direct.csv': ('values.tif', 'zones.tif', 'frequency', 2), 'single-class.csv': ('single.tif', 'zones.tif', 'frequency', 2), 'second-band.csv': ('second.tif', 'zones.tif', 'frequency', 2), 'cross-blocks.csv': ('cross-values.tif', 'cross-zones.tif', 'frequency', 2), 'reclassified-values.csv': ('values.tif', 'zones.tif', 'reclass-values', 2), 'reclassified-zones.csv': ('values.tif', 'zones.tif', 'reclass-zones', 2), 'rotated.csv': ('rotated-values.tif', 'rotated-zones.tif', 'frequency', 2), 'geographic.csv': ('geographic-values.tif', 'geographic-zones.tif', 'frequency', 2), 'reversed.csv': ('reversed-values.tif', 'reversed-zones.tif', 'frequency', 2), 'coverage.csv': ('coverage.tif', 'coverage-zones.tif', 'frequency', 2), 'native.csv': ('native-values.tif', 'native-zones.tif', 'frequency', 2), 'mixed-diagnostic.csv': ('mixed.tif', 'zones.tif', 'frequency', 2), 'invalid-values.csv': ('invalid.tif', 'zones.tif', 'frequency', 2), 'empty-zones.csv': ('values.tif', 'empty-zones.tif', 'frequency', 2)}
    expected_failures = ('method_unknown', 'method_empty', 'percentile_value', 'percentile_null', 'weights_raster', 'weights_band', 'weighted_stddev', 'stddev_string', 'values_band_zero', 'values_band_high', 'values_band_boolean', 'values_band_string', 'zones_band_zero', 'zones_band_high', 'zones_band_boolean', 'zones_band_string', 'values_alpha', 'zones_alpha', 'values_complex', 'zones_complex', 'fractional_class', 'unsafe_class', 'native_unsafe_class', 'fractional_zone', 'unsafe_zone', 'overflow_zone', 'different_size', 'shifted_grid', 'different_crs', 'missing_crs', 'missing_grid', 'singular_grid')
    if (any(len(nodes) != 1 for nodes in actual.values()) or ast.literal_eval(actual['CASES'][0]) != expected_cases
        or ast.literal_eval(actual['FAILURES'][0]) != expected_failures): raise RegistrationError('zonal class frequency fixed 16/64 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.unique', 'np.histogram')):
        raise RegistrationError('zonal class frequency oracle must remain independent')



ZONAL_WEIGHTED_REQUIRED = {
    'scripts/test/online-hosted-raster-zonal-weighted-gate.sh': ('start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare',
        'online-raster-zonal-weighted-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
    'business/scripts/online-raster-zonal-weighted-fixture.py': ('NFSExport','owned_root','WIDTH, HEIGHT = 513, 513','context.prec = 2000',
        'Decimal.from_float(v)*dw','np.array_equal','GDAL_TIFF_INTERNAL_MASK','full_csv_checks','full_source_pixels_verified','runtime_workspaces_removed',
        'archive_files','previous archive was deleted','previous archived bytes changed','oracle cache hash changed','source file changed',
        'weights changed original nine columns','all-one weights changed ordinary sum or mean','absolute weight scale or mean invariance changed','exact weighted statistic differs','zonal-weighted-csvs'),
    'scripts/test/raster-zonal-weighted-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS',
        'budget != 600', 'BudgetClient', 'remaining()', 'validate_preview', 'legacy.validate_success', 'lineage.collect',
        "inputs.sort(key=lambda value:value['port'])", "[value['locator'] for value in wanted['inputs']]",
        'lineage.validate(facts, wanted, public=True)', 'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage',
        'source_edges', 'readonly.no_publication', 'query language rewrote stored failure', 'previous complete CSV changed',
        'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1416', 'physical_calls != 97',
        'len(cases) != 16', 'len(failures) != 80', 'exact_cause', 'Monitor current output lineage differs'),
    'engines/geopython-workflow/test_online_zonal_weighted_fixture.py': (
        'test_zonal_weighted_sixteen_real_csvs_complete_independent_oracle_and_raw_archive',
        'test_zonal_weighted_eighty_exact_submitted_failures_and_no_output',
        'test_zonal_weighted_real_physical_corruption_is_rejected', 'one-ulp',
        'test_zonal_weighted_csv_archive_never_repairs_or_overwrites',
        'test_zonal_weighted_worker_stdin_frozen_oracle_poison_and_cleanup','test_zonal_weighted_independent_oracle_rejects_wrong_weight_semantics','normalize-sum','drop-scale','zero-as-null'),
    'scripts/test/raster-zonal-weighted-persistence-online_test.py': (
        'test_full_matrix_16_success_80_failures_1416_checks_and_cleanup', 'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
        'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_failures_preserve_primary_and_cleanup_error',
        'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced'),
    'scripts/utils/online_raster_lineage.py': ("'raster-zonal-weighted-persistence': 'zonal-weighted-lineage'",),
    'scripts/test/online-raster-lineage_test.py': ('test_zonal_weighted_approved_three_role_csv_facts_preserve_internal_and_public_boundaries',),
    'Makefile': ('test_online_zonal_weighted_fixture.py', '@$(MAKE) test-raster-zonal-weighted-online-runner',
        'scripts/test/raster-zonal-weighted-persistence-online_test.py', 'scripts/test/online-raster-zonal-weighted-fixture_test.py',
        'scripts/test/online-hosted-raster-zonal-weighted-gate_test.py', 'test_zonal_weighted_requires_exact_csv_matrix_budget_and_regressions'),
}


def validate_raster_zonal_weighted(repository: Path, registered: set[str]) -> None:
    suite = 'raster-zonal-weighted-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-zonal-weighted-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-zonal-weighted-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-zonal-weighted-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('zonal weighted requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_zonal_weighted_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('zonal weighted Native omitted from standard recipe')
    for relative, fragments in ZONAL_WEIGHTED_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('zonal weighted strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-zonal-weighted-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'weighted.csv': ('values.tif', 'zones.tif', 'weights.tif'), 'scaled.csv': ('values.tif', 'zones.tif', 'scaled-weights.tif'), 'ones.csv': ('even.tif', 'small-zones.tif', 'ones.tif'), 'control-ones.csv': ('even.tif', 'small-zones.tif', None), 'control-main.csv': ('values.tif', 'zones.tif', None), 'weights-reclassified.csv': ('values.tif', 'zones.tif', 'weight-classes.tif'), 'zones-reclassified.csv': ('values.tif', 'zones.tif', 'weights.tif'), 'zero.csv': ('even.tif', 'small-zones.tif', 'zero.tif'), 'mixed-empty.csv': ('mixed.tif', 'small-zones.tif', 'ones.tif'), 'invalid-values.csv': ('invalid.tif', 'small-zones.tif', 'ones.tif'), 'empty-zones.csv': ('even.tif', 'empty-zones.tif', 'ones.tif'), 'coverage.csv': ('coverage.tif', 'coverage-zones.tif', 'ignored-weights.tif'), 'rotated.csv': ('rotated-values.tif', 'rotated-zones.tif', 'rotated-weights.tif'), 'single.csv': ('single.tif', 'small-zones.tif', 'ones.tif'), 'int64.csv': ('int64.tif', 'large-zones.tif', 'int64-weights.tif'), 'uint64.csv': ('uint64.tif', 'large-zones.tif', 'uint64-weights.tif')}
    expected_failures = ('only_raster', 'only_band', 'raster_null', 'band_null', 'both_null', 'percentile_conflict', 'negative_weight', 'nodata_weight', 'mask_weight', 'alpha_weight', 'nan_weight', 'infinite_weight', 'values_band_zero', 'values_band_high', 'values_band_boolean', 'values_band_string', 'values_alpha', 'values_complex', 'zones_band_zero', 'zones_band_high', 'zones_band_boolean', 'zones_band_string', 'zones_alpha', 'zones_complex', 'weights_band_zero', 'weights_band_high', 'weights_band_boolean', 'weights_band_string', 'weights_alpha', 'weights_complex', 'zones_shifted', 'weights_size', 'weights_crs', 'weights_shifted', 'weights_rotated', 'weights_missing', 'weights_singular', 'fractional_zone', 'unsafe_zone', 'overflow_zone')
    if (any(len(nodes) != 1 for nodes in actual.values()) or ast.literal_eval(actual['CASES'][0]) != expected_cases
        or ast.literal_eval(actual['FAILURES'][0]) != expected_failures): raise RegistrationError('zonal weighted fixed 16/80 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile')):
        raise RegistrationError('zonal weighted oracle must remain independent')


ZONAL_WEIGHTED_STDDEV_REQUIRED = {
    'scripts/test/online-hosted-raster-zonal-weighted-stddev-gate.sh': ('start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare',
        'online-raster-zonal-weighted-stddev-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
    'business/scripts/online-raster-zonal-weighted-stddev-fixture.py': ('NFSExport','owned_root','WIDTH, HEIGHT = 513, 513','context.prec = 2000',
        'Decimal.from_float(v)*dw','np.array_equal','GDAL_TIFF_INTERNAL_MASK','full_csv_checks','full_source_pixels_verified','runtime_workspaces_removed',
        'archive_files','previous archive was deleted','previous archived bytes changed','oracle cache hash changed','source file changed',
        'weights changed original nine columns','all-one weights changed ordinary sum mean or stddev','absolute weight scale or mean invariance changed','exact weighted statistic differs','zonal-weighted-stddev-csvs','include_stddev','center=total/mass','second_moments[code]-2*shift*first_moments[code]+shift*shift*mass','variance.sqrt()','stage(\'oracle:start\')','stage(\'verify:done\',started)','weighted_stddev'),
    'scripts/test/raster-zonal-weighted-stddev-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS',
        'budget != 600', 'BudgetClient', 'remaining()', 'validate_preview', 'legacy.validate_success', 'lineage.collect',
        "inputs.sort(key=lambda value:value['port'])", "[value['locator'] for value in wanted['inputs']]",
        'lineage.validate(facts, wanted, public=True)', 'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage',
        'source_edges', 'readonly.no_publication', 'query language rewrote stored failure', 'previous complete CSV changed',
        'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1704', 'physical_calls != 115',
        'len(cases) != 16', 'len(failures) != 98', 'exact_cause', 'Monitor current output lineage differs','include_weighted_stddev','enabled_without_weights','flag_fraction'),
    'engines/geopython-workflow/test_online_zonal_weighted_stddev_fixture.py': (
        'test_zonal_weighted_stddev_sixteen_real_csvs_complete_independent_oracle_and_raw_archive',
        'test_zonal_weighted_stddev_ninety_eight_exact_submitted_failures_and_no_output',
        'test_zonal_weighted_stddev_real_physical_corruption_is_rejected', 'one-ulp',
        'test_zonal_weighted_stddev_csv_archive_never_repairs_or_overwrites',
        'test_zonal_weighted_stddev_worker_stdin_frozen_oracle_poison_and_cleanup','timeout=90','test_zonal_weighted_stddev_oracle_preserves_every_frozen_legacy_row','test_zonal_weighted_stddev_fixed_reference_matches_direct_centered_decimal','test_zonal_weighted_stddev_independent_oracle_rejects_wrong_weight_semantics','normalize-sum','drop-scale','zero-as-null','test_zonal_weighted_stddev_rejects_stddev_semantics_and_unchanged_original_controls','sample-denominator','one-ulp-stddev','all-one-both'),
    'scripts/test/raster-zonal-weighted-stddev-persistence-online_test.py': (
        'test_full_matrix_16_success_98_failures_1704_checks_and_cleanup', 'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
        'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_failures_preserve_primary_and_cleanup_error',
        'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced'),
    'scripts/utils/online_raster_lineage.py': ("'raster-zonal-weighted-stddev-persistence': 'zonal-weighted-stddev-lineage'",),
    'scripts/test/online-raster-lineage_test.py': ('test_zonal_weighted_stddev_approved_three_role_csv_facts_preserve_internal_and_public_boundaries',),
    'Makefile': ('test_online_zonal_weighted_stddev_fixture.py', '@$(MAKE) test-raster-zonal-weighted-stddev-online-runner',
        'scripts/test/raster-zonal-weighted-stddev-persistence-online_test.py', 'scripts/test/online-raster-zonal-weighted-stddev-fixture_test.py',
        'scripts/test/online-hosted-raster-zonal-weighted-stddev-gate_test.py', 'test_zonal_weighted_stddev_requires_exact_csv_matrix_budget_and_regressions'),
}


def validate_raster_zonal_weighted_stddev(repository: Path, registered: set[str]) -> None:
    suite = 'raster-zonal-weighted-stddev-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-zonal-weighted-stddev-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-zonal-weighted-stddev-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-zonal-weighted-stddev-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('zonal weighted stddev requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_zonal_weighted_stddev_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('zonal weighted stddev Native omitted from standard recipe')
    for relative, fragments in ZONAL_WEIGHTED_STDDEV_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('zonal weighted stddev strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-zonal-weighted-stddev-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'weighted.csv': ('values.tif', 'zones.tif', 'weights.tif'), 'scaled.csv': ('values.tif', 'zones.tif', 'scaled-weights.tif'), 'ones.csv': ('even.tif', 'small-zones.tif', 'ones.tif'), 'control-default.csv': ('values.tif', 'zones.tif', 'weights.tif'), 'control-false.csv': ('values.tif', 'zones.tif', 'weights.tif'), 'control-original.csv': ('even.tif', 'small-zones.tif', None), 'weights-reclassified.csv': ('values.tif', 'zones.tif', 'weight-classes.tif'), 'zones-reclassified.csv': ('values.tif', 'zones.tif', 'weights.tif'), 'zero.csv': ('even.tif', 'small-zones.tif', 'zero.tif'), 'mixed-empty.csv': ('mixed-values.tif', 'mixed-zones.tif', 'mixed-weights.tif'), 'invalid-values.csv': ('invalid.tif', 'small-zones.tif', 'ones.tif'), 'empty-zones.csv': ('even.tif', 'empty-zones.tif', 'ones.tif'), 'coverage.csv': ('coverage.tif', 'coverage-zones.tif', 'ignored-weights.tif'), 'rotated.csv': ('rotated-values.tif', 'rotated-zones.tif', 'rotated-weights.tif'), 'int64.csv': ('int64.tif', 'large-zones.tif', 'int64-weights.tif'), 'uint64.csv': ('uint64.tif', 'large-zones.tif', 'uint64-weights.tif')}
    expected_failures = ('only_raster', 'only_band', 'raster_null', 'band_null', 'both_null', 'percentile_conflict', 'negative_weight', 'nodata_weight', 'mask_weight', 'alpha_weight', 'nan_weight', 'infinite_weight', 'values_band_zero', 'values_band_high', 'values_band_boolean', 'values_band_string', 'values_alpha', 'values_complex', 'zones_band_zero', 'zones_band_high', 'zones_band_boolean', 'zones_band_string', 'zones_alpha', 'zones_complex', 'weights_band_zero', 'weights_band_high', 'weights_band_boolean', 'weights_band_string', 'weights_alpha', 'weights_complex', 'zones_shifted', 'weights_size', 'weights_crs', 'weights_shifted', 'weights_rotated', 'weights_missing', 'weights_singular', 'fractional_zone', 'unsafe_zone', 'overflow_zone', 'flag_null', 'flag_zero', 'flag_one', 'flag_fraction', 'flag_string_true', 'flag_string_false', 'flag_list', 'flag_object', 'enabled_without_weights')
    if (any(len(nodes) != 1 for nodes in actual.values()) or ast.literal_eval(actual['CASES'][0]) != expected_cases
        or ast.literal_eval(actual['FAILURES'][0]) != expected_failures): raise RegistrationError('zonal weighted stddev fixed 16/98 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile')):
        raise RegistrationError('zonal weighted stddev oracle must remain independent')


VECTOR_ZONAL_PERCENTILE_REQUIRED = {
    'scripts/test/online-hosted-vector-zonal-percentile-gate.sh': (
        'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
        'online_raster_lineage.py prepare', 'online-vector-zonal-percentile-fixture.py stop',
        'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
    ),
    'business/scripts/online-vector-zonal-percentile-fixture.py': (
        'NFSExport', 'owned_root', 'from fractions import Fraction', 'WIDTH,HEIGHT=519,515',
        'position=(count-1)*Fraction(float(p))/100', 'sorted(float(v)', 'shapely.contains', 'shapely.intersection',
        'np.array_equal', 'GDAL_TIFF_INTERNAL_MASK', 'full_gpkg_checks', 'full_source_pixels_verified',
        'full_source_vectors_verified', 'runtime_workspaces_removed', 'archive_files', 'oracle cache hash changed',
        'source file changed', 'percentile changed original eight fields', 'vector-zonal-percentile-gpkgs',
    ),
    'scripts/test/vector-zonal-percentile-persistence-online.py': (
        'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()',
        'validate_preview', 'legacy.validate_success', 'lineage.collect',
        'lineage.validate(facts, wanted, public=True)', 'lineage.verify_archive', 'base.validate_scan',
        'raster.wait_lineage', 'source_edges', 'readonly.no_publication', 'query language rewrote stored failure',
        'previous complete GeoPackage changed', 'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed',
        'full_checks != 904', 'physical_calls != 65', 'len(cases) != 16', 'len(failures) != 48', 'exact_cause',
        'Monitor current output lineage differs', 'vector_persistence.validate_item',
    ),
    'engines/geopython-workflow/test_online_vector_zonal_percentile_fixture.py': (
        'test_vector_zonal_percentile_sixteen_real_gpkgs_complete_independent_oracle_and_raw_archive',
        'test_vector_zonal_percentile_forty_eight_exact_submitted_failures_and_no_output',
        'test_vector_zonal_percentile_real_physical_corruption_is_rejected', 'one-ulp',
        'test_vector_zonal_percentile_gpkg_archive_never_repairs_or_overwrites',
        'test_vector_zonal_percentile_worker_stdin_frozen_oracle_poison_and_cleanup',
    ),
    'scripts/test/vector-zonal-percentile-persistence-online_test.py': (
        'test_full_matrix_16_success_48_failures_904_checks_and_cleanup',
        'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
        'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken',
        'test_failures_preserve_primary_and_cleanup_error',
        'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced',
    ),
    'business/scripts/online-raster-zonal-percentile-fixture.py': (
        'previous archive was deleted', 'previous archived bytes changed',
    ),
    'scripts/utils/online_raster_lineage.py': (
        "'vector-zonal-percentile-persistence': 'vector-zonal-percentile-lineage'",
    ),
    'scripts/test/online-raster-lineage_test.py': (
        'test_vector_zonal_percentile_approved_two_role_gpkg_facts_preserve_internal_and_public_boundaries',
    ),
    'Makefile': (
        'test_online_vector_zonal_percentile_fixture.py', '@$(MAKE) test-vector-zonal-percentile-online-runner',
        'scripts/test/vector-zonal-percentile-persistence-online_test.py',
        'scripts/test/online-vector-zonal-percentile-fixture_test.py',
        'scripts/test/online-hosted-vector-zonal-percentile-gate_test.py',
        'test_vector_zonal_percentile_requires_exact_gpkg_matrix_budget_and_regressions',
    ),
}


VARIETY_REQUIRED = {
    'scripts/test/online-hosted-raster-focal-variety-gate.sh': (
        'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
        'online_raster_lineage.py prepare', 'online-raster-focal-variety-fixture.py stop',
        'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
    ),
    'business/scripts/online-raster-focal-variety-fixture.py': (
        'NFSExport', 'owned_root', 'from fractions import Fraction', 'len(set(samples))', 'Counter(samples)',
        'statistics.pstdev', 'case_shape(name)', 'verify_sources(root)', 'np.array_equal',
        'GDAL_TIFF_INTERNAL_MASK', 'full_source_pixels_verified', 'runtime_workspaces_removed', 'archive_files',
        'oracle cache hash changed', 'source file changed', 'variety-cogs', 'archive history missing',
        'variety-expectations', 'validate_expected_export', 'archive_expectations',
        'allow_pickle=False', 'def cleanup_oracle', 'full_check=True', 'nearest_overviews', 'private_control',
    ),
    'scripts/test/raster-focal-variety-persistence-online.py': (
        'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()',
        'validate_success', 'lineage.collect', 'lineage.validate(facts,wanted,public=True)',
        'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges',
        'readonly.no_publication', 'query language rewrote stored failure', 'previous original COG changed',
        'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 584',
        'frozen independent array archive changed', 'archived_expectations',
        'physical_calls != 45', 'len(cases) != 16', 'len(failures) != 28', 'exact_cause',
        'Monitor current output lineage differs', 'raster.strict_sizes', 'fixture.expected_cache_bytes()',
    ),
    'engines/geopython-workflow/test_online_focal_variety_fixture.py': (
        'test_variety_hosted_sixteen_real_cogs_complete_oracle_old_eight_controls_and_original_bytes',
        'test_variety_hosted_twenty_eight_exact_submitted_failures_no_output_and_clean_workspace',
        'test_variety_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected', 'one-ulp',
        'test_variety_hosted_original_cog_archive_never_repairs_or_overwrites',
        'test_variety_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change',
        'test_variety_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup',
        'test_variety_hosted_frozen_expected_archive_rejects_tampering',
    ),
    'scripts/test/raster-focal-variety-persistence-online_test.py': (
        'test_full_matrix_16_success_28_failures_584_checks_and_cleanup',
        'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
        'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken',
        'test_failures_preserve_primary_and_cleanup_error',
        'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced',
    ),
    'business/scripts/online-raster-zonal-percentile-fixture.py': (
        'previous archive was deleted', 'previous archived bytes changed',
    ),
    'scripts/utils/online_raster_lineage.py': (
        "'raster-focal-variety-persistence': 'variety-lineage'", 'BEGIN READ ONLY;', 'statement_timeout',
        'workflow_matches', 'validate_container', 'closed(public, expected)', 'closed(internal, expected)',
    ),
    'scripts/test/online-raster-lineage_test.py': (
        'test_variety_approved_single_source_current_raster_save_and_hidden_public_operation',
    ),
    'Makefile': (
        'test_online_focal_variety_fixture.py', '@$(MAKE) test-raster-focal-variety-online-runner',
        'scripts/test/raster-focal-variety-persistence-online_test.py',
        'scripts/test/online-raster-focal-variety-fixture_test.py',
        'scripts/test/online-hosted-raster-focal-variety-gate_test.py',
        'test_variety_requires_exact_cog_matrix_budget_and_regressions',
    ),
}

def validate_raster_focal_variety(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-variety-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-focal-variety-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-focal-variety-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-focal-variety-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal variety requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_focal_variety_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('focal variety Native omitted from standard recipe')
    for relative, fragments in VARIETY_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('focal variety strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-focal-variety-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'variety-3.cog.tif': ('values.tif', 3, 'variety', None, 'direct'), 'variety-5.cog.tif': ('values.tif', 5, 'variety', None, 'direct'), 'reclassified.cog.tif': ('values.tif', 3, 'variety', None, 'reclassify'), 'zero.cog.tif': ('zero.tif', 5, 'variety', None, 'direct'), 'int64.cog.tif': ('int64.tif', 3, 'variety', None, 'direct'), 'uint64.cog.tif': ('uint64.tif', 5, 'variety', None, 'direct'), 'unlocated-row.cog.tif': ('unlocated.tif', 5, 'variety', None, 'direct'), 'invalid.cog.tif': ('invalid.tif', 3, 'variety', None, 'direct'), 'control-sum.cog.tif': ('values.tif', 3, 'sum', None, 'direct'), 'control-mean.cog.tif': ('values.tif', 3, 'mean', None, 'direct'), 'control-min.cog.tif': ('values.tif', 3, 'min', None, 'direct'), 'control-max.cog.tif': ('values.tif', 3, 'max', None, 'direct'), 'control-stddev.cog.tif': ('values.tif', 3, 'stddev', None, 'direct'), 'control-mode.cog.tif': ('values.tif', 3, 'mode', None, 'direct'), 'control-median.cog.tif': ('values.tif', 3, 'median', None, 'direct'), 'control-percentile.cog.tif': ('values.tif', 3, 'percentile', 50, 'direct')}
    expected_failures = ('fractional', 'positive_unsafe', 'negative_unsafe', 'band_zero', 'band_high', 'band_boolean', 'band_string', 'alpha', 'complex', 'window_value', 'window_boolean', 'window_string', 'method', 'unused')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('focal variety fixed 16/28 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('focal variety oracle must remain independent')


CIRCLE_REQUIRED = {'scripts/test/online-hosted-raster-focal-circle-gate.sh': ('start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare', 'online-raster-focal-circle-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'business/scripts/online-raster-focal-circle-fixture.py': ('NFSExport', 'owned_root', 'from fractions import Fraction', 'len(set(samples))', 'Counter(samples)', 'sum((d(v)-mean)**2 for v in samples)', 'case_shape(name)', 'verify_sources(root)', 'np.array_equal', 'GDAL_TIFF_INTERNAL_MASK', 'full_source_pixels_verified', 'runtime_workspaces_removed', 'archive_files', 'oracle cache hash changed', 'source file changed', 'circle-cogs', 'archive history missing', 'circle-expectations', 'validate_expected_export', 'archive_expectations', 'allow_pickle=False', 'def cleanup_oracle', 'full_check=True', 'nearest_overviews', 'private_control', 'context.prec=2000', 'Decimal.from_float', 'reference(source).GetLinearUnits()', '<=d(float(radius))**2', 'circle-sum.cog.tif', 'below.cog.tif', 'above.cog.tif', 'expected_overview_count', 'CASES', 'circle_membership_verified', 'circle_statistics_verified', 'WIDTH,HEIGHT=131,129', "name=='cross-block.tif':width,height=519,515", "name=='foot.tif':epsg=2222", "name=='survey.tif':epsg=2277", 'values=codes[(x%5)+5*(y%5)]'), 'scripts/test/raster-focal-circle-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()', 'validate_success', 'lineage.collect', 'lineage.validate(facts,wanted,public=True)', 'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges', 'readonly.no_publication', 'query language rewrote stored failure', 'previous original COG changed', 'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1971', 'frozen independent array archive changed', 'archived_expectations', 'physical_calls != 119', 'len(cases) != 18', 'len(failures) != 100', 'exact_cause', 'Monitor current output lineage differs', 'raster.strict_sizes', 'fixture.expected_cache_bytes()', 'window_type', 'radius_metres', 'target_metadata_before[name] = copy.deepcopy(item)', 'target_metadata_after != target_metadata_before', 'source_metadata_after'), 'engines/geopython-workflow/test_online_focal_circle_fixture.py': ('test_circle_hosted_eighteen_real_cogs_complete_oracle_circle_boundaries_controls_and_original_bytes', 'test_circle_hosted_one_hundred_exact_submitted_failures_no_output_and_clean_workspace', 'test_circle_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected', 'one-ulp', 'test_circle_hosted_original_cog_archive_never_repairs_or_overwrites', 'test_circle_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change', 'test_circle_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup', 'test_circle_hosted_frozen_expected_archive_rejects_tampering', 'test_circle_hosted_independent_semantic_mutations_cannot_pass', 'test_circle_hosted_oracle_never_calls_product_kernel_or_reducer', "f.expected_pixels('circle-variety.cog.tif')[60,60]==13"), 'scripts/test/raster-focal-circle-persistence-online_test.py': ('test_full_matrix_18_success_100_failures_1971_checks_and_cleanup', 'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_failures_preserve_primary_and_cleanup_error', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_exact_18_50_matrix_circle_radius_and_single_square_control', 'test_all_source_and_target_meta_snapshots_archived_and_mutation_rejected'), 'business/scripts/online-raster-zonal-percentile-fixture.py': ('previous archive was deleted', 'previous archived bytes changed'), 'scripts/utils/online_raster_lineage.py': ("'raster-focal-circle-persistence': 'circle-lineage'", 'BEGIN READ ONLY;', 'statement_timeout', 'workflow_matches', 'validate_container', 'closed(public, expected)', 'closed(internal, expected)'), 'scripts/test/online-raster-lineage_test.py': ('test_circle_approved_single_source_current_raster_save_and_hidden_public_operation',), 'Makefile': ('test_online_focal_circle_fixture.py', '@$(MAKE) test-raster-focal-circle-online-runner', 'scripts/test/raster-focal-circle-persistence-online_test.py', 'scripts/test/online-raster-focal-circle-fixture_test.py', 'scripts/test/online-hosted-raster-focal-circle-gate_test.py', 'test_circle_requires_exact_cog_matrix_budget_and_regressions')}


def validate_raster_focal_circle(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-circle-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-focal-circle-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-focal-circle-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-focal-circle-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal circle requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_focal_circle_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('focal circle Native omitted from standard recipe')
    for relative, fragments in CIRCLE_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('focal circle strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-focal-circle-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'circle-sum.cog.tif': ('boundary.tif', None, 'sum', None, 'direct', 5.0), 'circle-mean.cog.tif': ('cross-block.tif', None, 'mean', None, 'direct', 2.0), 'circle-min.cog.tif': ('row.tif', None, 'min', None, 'direct', 2.0), 'circle-max.cog.tif': ('column.tif', None, 'max', None, 'direct', 2.0), 'circle-stddev.cog.tif': ('values.tif', None, 'stddev', None, 'direct', 2.0), 'circle-mode.cog.tif': ('values.tif', None, 'mode', None, 'direct', 2.0), 'circle-median.cog.tif': ('values.tif', None, 'median', None, 'direct', 2.0), 'circle-percentile.cog.tif': ('values.tif', None, 'percentile', 25.0, 'direct', 2.0), 'circle-variety.cog.tif': ('values.tif', None, 'variety', None, 'direct', 2.0), 'below.cog.tif': ('boundary.tif', None, 'sum', None, 'direct', 4.999999999999999), 'above.cog.tif': ('boundary.tif', None, 'sum', None, 'direct', 5.000000000000001), 'percentile-50.cog.tif': ('values.tif', None, 'percentile', 50.0, 'direct', 2.0), 'foot.cog.tif': ('foot.tif', None, 'mean', None, 'direct', 0.6096), 'survey.cog.tif': ('survey.tif', None, 'mean', None, 'direct', 0.6096012192024384), 'center.cog.tif': ('values.tif', None, 'mean', None, 'direct', 0.25), 'reclassified.cog.tif': ('values.tif', None, 'mode', None, 'reclassify', 2.0), 'invalid.cog.tif': ('invalid.tif', None, 'mean', None, 'direct', 2.0), 'square-control.cog.tif': ('values.tif', 5, 'mean', None, 'direct', None)}
    expected_failures = ('type_null', 'type_bool', 'type_unknown', 'type_object', 'radius_missing', 'radius_null', 'radius_zero', 'radius_negative', 'radius_bool', 'radius_string', 'radius_list', 'radius_object', 'circle_window', 'circle_window_null', 'square_radius', 'square_radius_null', 'weighted_mean', 'weighted_stddev', 'weighted_sum', 'unused_weights', 'method', 'percentile_missing', 'percentile_null', 'percentile_bool', 'percentile_string', 'percentile_low', 'percentile_high', 'unused_percentile', 'geographic', 'compound', 'missing_crs', 'missing_grid', 'rotated', 'west_up', 'south_up', 'singular', 'unknown_unit', 'x_oversize', 'y_oversize', 'small_oversize', 'band_zero', 'band_high', 'band_boolean', 'band_string', 'band_fraction', 'alpha', 'mode_fractional', 'mode_unsafe', 'variety_fractional', 'variety_unsafe')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('focal circle fixed 18/100 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('focal circle oracle must remain independent')


ORTHOGONAL_CIRCLE_REQUIRED = {'scripts/test/online-hosted-raster-focal-circle-orthogonal-gate.sh': ('start_online_geopython_runtime',
                                                                       '--mount',
                                                                       '--suite raster-workflow',
                                                                       '--wait-protection-owners '
                                                                       'develop,manager',
                                                                       'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
                                                                       'online_raster_lineage.py '
                                                                       'prepare',
                                                                       'online-raster-focal-circle-orthogonal-fixture.py '
                                                                       'stop',
                                                                       'unset '
                                                                       'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
 'business/scripts/online-raster-focal-circle-orthogonal-fixture.py': ('NFSExport',
                                                                       'owned_root',
                                                                       'from fractions import Fraction',
                                                                       'len(set(samples))',
                                                                       'Counter(samples)',
                                                                       'sum((d(v)-mean)**2 for v in '
                                                                       'samples)',
                                                                       'case_shape(name)',
                                                                       'verify_sources(root)',
                                                                       'np.array_equal',
                                                                       'GDAL_TIFF_INTERNAL_MASK',
                                                                       'full_source_pixels_verified',
                                                                       'runtime_workspaces_removed',
                                                                       'archive_files',
                                                                       'oracle cache hash changed',
                                                                       'source file changed',
                                                                       'circle-orthogonal-cogs',
                                                                       'archive history missing',
                                                                       'circle-orthogonal-expectations',
                                                                       'validate_expected_export',
                                                                       'archive_expectations',
                                                                       'allow_pickle=False',
                                                                       'def cleanup_oracle',
                                                                       'full_check=True',
                                                                       'nearest_overviews',
                                                                       'private_control',
                                                                       'context.prec=2000',
                                                                       'Decimal.from_float',
                                                                       'reference(source).GetLinearUnits()',
                                                                       '<= radius_squared',
                                                                       'column*column_x + row*row_x',
                                                                       'column*column_y + row*row_y',
                                                                       'affine_transform',
                                                                       'circle-sum.cog.tif',
                                                                       'below.cog.tif',
                                                                       'above.cog.tif',
                                                                       'expected_overview_count',
                                                                       'CASES',
                                                                       'circle_membership_verified',
                                                                       'circle_statistics_verified',
                                                                       'WIDTH,HEIGHT=131,129',
                                                                       "name=='cross-block.tif':width,height=519,515",
                                                                       "name=='foot.tif':epsg=2222",
                                                                       "name=='survey.tif':epsg=2277",
                                                                       'values=codes[(x%5)+5*(y%5)]'),
 'scripts/test/raster-focal-circle-orthogonal-persistence-online.py': ('raster.validate_identity',
                                                                       'raster.EDITOR_PERMISSIONS',
                                                                       'budget != 600',
                                                                       'BudgetClient',
                                                                       'remaining()',
                                                                       'validate_success',
                                                                       'lineage.collect',
                                                                       'lineage.validate(facts,wanted,public=True)',
                                                                       'lineage.verify_archive',
                                                                       'base.validate_scan',
                                                                       'raster.wait_lineage',
                                                                       'source_edges',
                                                                       'readonly.no_publication',
                                                                       'query language rewrote stored '
                                                                       'failure',
                                                                       'previous original COG changed',
                                                                       'fixture_call',
                                                                       'cleanup-oracle',
                                                                       'oracle cleanup also failed',
                                                                       'full_checks != 2204',
                                                                       'six-coefficient affine proof differs',
                                                                       'frozen independent array '
                                                                       'archive changed',
                                                                       'archived_expectations',
                                                                       'physical_calls != 126',
                                                                       'len(cases) != 19',
                                                                       'len(failures) != 106',
                                                                       'exact_cause',
                                                                       'Monitor current output lineage '
                                                                       'differs',
                                                                       'raster.strict_sizes',
                                                                       'fixture.expected_cache_bytes()',
                                                                       'window_type',
                                                                       'radius_metres',
                                                                       'target_metadata_before[name] = '
                                                                       'copy.deepcopy(item)',
                                                                       'target_metadata_after != '
                                                                       'target_metadata_before',
                                                                       'source_metadata_after'),
 'engines/geopython-workflow/test_online_focal_circle_orthogonal_fixture.py': ('test_orthogonal_circle_hosted_nineteen_real_cogs_complete_oracle_circle_boundaries_controls_and_original_bytes',
                                                                               'test_orthogonal_circle_hosted_one_hundred_six_exact_submitted_failures_no_output_and_clean_workspace',
                                                                               'test_orthogonal_circle_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected',
                                                                               'one-ulp',
                                                                               'test_orthogonal_circle_hosted_original_cog_archive_never_repairs_or_overwrites',
                                                                               'test_orthogonal_circle_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change',
                                                                               'test_orthogonal_circle_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup',
                                                                               'test_orthogonal_circle_hosted_frozen_expected_archive_rejects_tampering',
                                                                               'test_orthogonal_circle_hosted_independent_semantic_mutations_cannot_pass',
                                                                               'test_orthogonal_circle_hosted_oracle_never_calls_product_kernel_or_reducer',
                                                                           'test_orthogonal_circle_hosted_actual_grid_witnesses',
                                                                               "f.expected_pixels('circle-variety.cog.tif')[60,60]==13"),
 'scripts/test/raster-focal-circle-orthogonal-persistence-online_test.py': ('test_full_matrix_19_success_106_failures_2204_checks_and_cleanup',
                                                                        'test_every_affine_coefficient_and_type_is_required',
                                                                            'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
                                                                            'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken',
                                                                            'test_failures_preserve_primary_and_cleanup_error',
                                                                            'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced',
                                                                            'test_exact_19_53_matrix_circle_radius_and_single_square_control',
                                                                            'test_all_source_and_target_meta_snapshots_archived_and_mutation_rejected'),
 'business/scripts/online-raster-zonal-percentile-fixture.py': ('previous archive was deleted',
                                                                'previous archived bytes changed'),
 'scripts/utils/online_raster_lineage.py': ("'raster-focal-circle-orthogonal-persistence': "
                                            "'circle-orthogonal-lineage'",
                                            'BEGIN READ ONLY;',
                                            'statement_timeout',
                                            'workflow_matches',
                                            'validate_container',
                                            'closed(public, expected)',
                                            'closed(internal, expected)'),
 'scripts/test/online-raster-lineage_test.py': ('test_orthogonal_circle_approved_single_source_current_raster_save_and_hidden_public_operation',),
 'Makefile': ('test_online_focal_circle_orthogonal_fixture.py',
              '@$(MAKE) test-raster-focal-circle-orthogonal-online-runner',
              'scripts/test/raster-focal-circle-orthogonal-persistence-online_test.py',
              'scripts/test/online-raster-focal-circle-orthogonal-fixture_test.py',
              'scripts/test/online-hosted-raster-focal-circle-orthogonal-gate_test.py',
              'test_orthogonal_circle_requires_exact_cog_matrix_budget_and_regressions')}


def validate_raster_focal_circle_orthogonal(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-circle-orthogonal-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-focal-circle-orthogonal-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-focal-circle-orthogonal-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-focal-circle-orthogonal-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal circle requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_focal_circle_orthogonal_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('focal circle Native omitted from standard recipe')
    for relative, fragments in ORTHOGONAL_CIRCLE_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('focal circle strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-focal-circle-orthogonal-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'circle-sum.cog.tif': ('boundary.tif', None, 'sum', None, 'direct', 25.0), 'circle-mean.cog.tif': ('cross-block.tif', None, 'mean', None, 'direct', 2.0), 'circle-min.cog.tif': ('row.tif', None, 'min', None, 'direct', 2.0), 'circle-max.cog.tif': ('column.tif', None, 'max', None, 'direct', 2.0), 'circle-stddev.cog.tif': ('values.tif', None, 'stddev', None, 'direct', 10.0), 'circle-mode.cog.tif': ('values.tif', None, 'mode', None, 'direct', 10.0), 'circle-median.cog.tif': ('values.tif', None, 'median', None, 'direct', 10.0), 'circle-percentile.cog.tif': ('values.tif', None, 'percentile', 25.0, 'direct', 10.0), 'circle-variety.cog.tif': ('values.tif', None, 'variety', None, 'direct', 10.0), 'below.cog.tif': ('boundary.tif', None, 'sum', None, 'direct', 24.999999999999996), 'above.cog.tif': ('boundary.tif', None, 'sum', None, 'direct', 25.000000000000004), 'percentile-50.cog.tif': ('values.tif', None, 'percentile', 50.0, 'direct', 10.0), 'foot.cog.tif': ('foot.tif', None, 'mean', None, 'direct', 0.6096), 'survey.cog.tif': ('survey.tif', None, 'mean', None, 'direct', 0.6096012192024384), 'center.cog.tif': ('values.tif', None, 'mean', None, 'direct', 1.0), 'reclassified.cog.tif': ('values.tif', None, 'mode', None, 'reclassify', 10.0), 'invalid.cog.tif': ('invalid.tif', None, 'mean', None, 'direct', 2.0), 'north-up.cog.tif': ('north-up.tif', None, 'sum', None, 'direct', 25.0), 'square-control.cog.tif': ('values.tif', 5, 'mean', None, 'direct', None)}
    expected_failures = ('type_null', 'type_bool', 'type_unknown', 'type_object', 'radius_missing', 'radius_null', 'radius_zero', 'radius_negative', 'radius_bool', 'radius_string', 'radius_list', 'radius_object', 'circle_window', 'circle_window_null', 'square_radius', 'square_radius_null', 'weighted_mean', 'weighted_stddev', 'weighted_sum', 'unused_weights', 'method', 'percentile_missing', 'percentile_null', 'percentile_bool', 'percentile_string', 'percentile_low', 'percentile_high', 'unused_percentile', 'geographic', 'compound', 'missing_crs', 'missing_grid', 'rotated', 'tiny_shear', 'rounded_dot', 'rotated_mirror', 'west_up', 'south_up', 'singular', 'unknown_unit', 'x_oversize', 'y_oversize', 'small_oversize', 'band_zero', 'band_high', 'band_boolean', 'band_string', 'band_fraction', 'alpha', 'mode_fractional', 'mode_unsafe', 'variety_fractional', 'variety_unsafe')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('focal circle fixed 19/106 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('focal circle oracle must remain independent')


WEIGHTED_CIRCLE_REQUIRED = {'scripts/test/online-hosted-raster-focal-circle-weighted-gate.sh': ('start_online_geopython_runtime',
                                                                     '--mount',
                                                                     '--suite raster-workflow',
                                                                     '--wait-protection-owners develop,manager',
                                                                     'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
                                                                     'online_raster_lineage.py prepare',
                                                                     'online-raster-focal-circle-weighted-fixture.py '
                                                                     'stop',
                                                                     'unset '
                                                                     'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
 'business/scripts/online-raster-focal-circle-weighted-fixture.py': ('NFSExport',
                                                                     'owned_root',
                                                                     'case_shape(name)',
                                                                     'verify_sources(root)',
                                                                     'np.array_equal',
                                                                     'GDAL_TIFF_INTERNAL_MASK',
                                                                     'full_source_pixels_verified',
                                                                     'runtime_workspaces_removed',
                                                                     'archive_files',
                                                                     'oracle cache hash changed',
                                                                     'source file changed',
                                                                     'circle-weighted-cogs',
                                                                     'archive history missing',
                                                                     'circle-weighted-expectations',
                                                                     'validate_expected_export',
                                                                     'archive_expectations',
                                                                     'allow_pickle=False',
                                                                     'def cleanup_oracle',
                                                                     'full_check=True',
                                                                     'nearest_overviews',
                                                                     'private_control',
                                                                     'Decimal.from_float',
                                                                     'reference(source).GetLinearUnits()',
                                                                     '<= radius_squared',
                                                                     'affine_transform',
                                                                     'below.cog.tif',
                                                                     'expected_overview_count',
                                                                     'CASES',
                                                                     'circle_membership_verified',
                                                                     'circle_statistics_verified',
                                                                     'WIDTH,HEIGHT=131,129',
                                                                     'context.prec = 2000',
                                                                     'sum(weight * (value - mean)**2 for value, '
                                                                     'weight in pairs) / mass',
                                                                     'dc*column_x + dr*row_x',
                                                                     'dc*column_y + dr*row_y',
                                                                     'fixture includes positive weight outside',
                                                                     'np.array_equal(actual, expected, '
                                                                     'equal_nan=True)',
                                                                     'GetMaskBand().ReadAsArray()',
                                                                     'actual all-one and unweighted COG pixels or '
                                                                     'masks differ',
                                                                     'actual exact/below boundary witness '
                                                                     'differs'),
 'scripts/test/raster-focal-circle-weighted-persistence-online.py': ('raster.validate_identity',
                                                                     'raster.EDITOR_PERMISSIONS',
                                                                     'budget != 600',
                                                                     'BudgetClient',
                                                                     'remaining()',
                                                                     'validate_success',
                                                                     'lineage.collect',
                                                                     'lineage.validate(facts,wanted,public=True)',
                                                                     'lineage.verify_archive',
                                                                     'base.validate_scan',
                                                                     'raster.wait_lineage',
                                                                     'source_edges',
                                                                     'readonly.no_publication',
                                                                     'query language rewrote stored failure',
                                                                     'previous original COG changed',
                                                                     'fixture_call',
                                                                     'cleanup-oracle',
                                                                     'oracle cleanup also failed',
                                                                     'full_checks != 1467',
                                                                     'six-coefficient affine proof differs',
                                                                     'frozen independent array archive changed',
                                                                     'archived_expectations',
                                                                     'physical_calls != 91',
                                                                     'len(cases) != 18',
                                                                     'len(failures) != 72',
                                                                     'exact_cause',
                                                                     'Monitor current output lineage differs',
                                                                     'raster.strict_sizes',
                                                                     'fixture.expected_cache_bytes()',
                                                                     'window_type',
                                                                     'radius_metres',
                                                                     'target_metadata_before[name] = '
                                                                     'copy.deepcopy(item)',
                                                                     'target_metadata_after != '
                                                                     'target_metadata_before',
                                                                     'source_metadata_after',
                                                                     'fixture.KERNELS',
                                                                     'overview_resampling',
                                                                     'boundary_witness',
                                                                     'failed execution added scan',
                                                                     'failed execution added derived lineage'),
 'engines/geopython-workflow/test_online_focal_circle_weighted_fixture.py': ('test_weighted_circle_hosted_eighteen_real_cogs_complete_oracle_circle_boundaries_controls_and_original_bytes',
                                                                             'test_weighted_circle_hosted_seventy_two_exact_submitted_failures_no_output_and_clean_workspace',
                                                                             'test_weighted_circle_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected',
                                                                             'one-ulp',
                                                                             'test_weighted_circle_hosted_original_cog_archive_never_repairs_or_overwrites',
                                                                             'test_weighted_circle_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change',
                                                                             'test_weighted_circle_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup',
                                                                             'test_weighted_circle_hosted_frozen_expected_archive_rejects_tampering',
                                                                             'test_weighted_circle_hosted_independent_semantic_mutations_cannot_pass',
                                                                             'test_weighted_circle_hosted_oracle_never_calls_product_kernel_or_reducer',
                                                                             'test_weighted_circle_hosted_actual_grid_witnesses'),
 'scripts/test/raster-focal-circle-weighted-persistence-online_test.py': ('test_full_matrix_18_success_72_failures_1467_checks_and_cleanup',
                                                                          'test_every_affine_coefficient_and_type_is_required',
                                                                          'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
                                                                          'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken',
                                                                          'test_failures_preserve_primary_and_cleanup_error',
                                                                          'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced',
                                                                          'test_exact_18_36_matrix_circle_radius_weights_and_actual_controls',
                                                                          'test_all_source_and_target_meta_snapshots_archived_and_mutation_rejected'),
 'business/scripts/online-raster-zonal-percentile-fixture.py': ('previous archive was deleted',
                                                                'previous archived bytes changed'),
 'scripts/utils/online_raster_lineage.py': ("'raster-focal-circle-weighted-persistence': "
                                            "'circle-weighted-lineage'",
                                            'BEGIN READ ONLY;',
                                            'statement_timeout',
                                            'workflow_matches',
                                            'validate_container',
                                            'closed(public, expected)',
                                            'closed(internal, expected)'),
 'scripts/test/online-raster-lineage_test.py': ('test_weighted_circle_approved_single_source_current_raster_save_and_hidden_public_operation',),
 'Makefile': ('test_online_focal_circle_weighted_fixture.py',
              '@$(MAKE) test-raster-focal-circle-weighted-online-runner',
              'scripts/test/raster-focal-circle-weighted-persistence-online_test.py',
              'scripts/test/online-raster-focal-circle-weighted-fixture_test.py',
              'scripts/test/online-hosted-raster-focal-circle-weighted-gate_test.py',
              'test_weighted_circle_requires_exact_cog_matrix_budget_and_regressions')}


def validate_raster_focal_circle_weighted(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-circle-weighted-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-focal-circle-weighted-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-focal-circle-weighted-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-focal-circle-weighted-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal circle requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_focal_circle_weighted_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('focal circle Native omitted from standard recipe')
    for relative, fragments in WEIGHTED_CIRCLE_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('focal circle strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-focal-circle-weighted-fixture.py').read_text()
    tree = ast.parse(source)
    expected = {'CASES': {'north-mean.cog.tif': ('north.tif', 'weighted_mean', 4.0, 'north', 'direct'),
               'north-stddev.cog.tif': ('north.tif', 'weighted_stddev', 4.0, 'north', 'direct'),
               'north-sum.cog.tif': ('north.tif', 'weighted_sum', 4.0, 'north', 'direct'),
               'rotated-mean.cog.tif': ('rotated-values.tif', 'weighted_mean', 10.0, 'rotated', 'direct'),
               'rotated-stddev.cog.tif': ('rotated-values.tif', 'weighted_stddev', 10.0, 'rotated', 'direct'),
               'rotated-sum.cog.tif': ('cross-block.tif', 'weighted_sum', 10.0, 'rotated', 'direct'),
               'ones-mean.cog.tif': ('control.tif', 'weighted_mean', 4.0, 'ones', 'direct'),
               'control-mean.cog.tif': ('control.tif', 'mean', 4.0, None, 'direct'),
               'ones-stddev.cog.tif': ('control.tif', 'weighted_stddev', 4.0, 'ones', 'direct'),
               'control-stddev.cog.tif': ('control.tif', 'stddev', 4.0, None, 'direct'),
               'ones-sum.cog.tif': ('control.tif', 'weighted_sum', 4.0, 'ones', 'direct'),
               'control-sum.cog.tif': ('control.tif', 'sum', 4.0, None, 'direct'),
               'foot.cog.tif': ('foot.tif', 'weighted_mean', 0.4, 'north', 'direct'),
               'survey.cog.tif': ('survey.tif', 'weighted_stddev', 0.4, 'north', 'direct'),
               'center.cog.tif': ('rotated-values.tif', 'weighted_sum', 1.0, 'center', 'direct'),
               'reclassified.cog.tif': ('north.tif', 'weighted_stddev', 4.0, 'north', 'reclassify'),
               'invalid.cog.tif': ('invalid.tif', 'weighted_sum', 10.0, 'rotated', 'direct'),
               'below.cog.tif': ('north.tif', 'weighted_mean', 3.9999999999999996, 'below', 'direct')},
     'KERNELS': {'north': [[0, 2, 0], [1, 0, 3], [0, 0, 0]],
                 'rotated': [[0, 0, 0, 0, 0],
                             [0, 0, 0.5, 0, 0],
                             [2, 0, 0, 3, 1],
                             [0, 0, 4, 0, 0],
                             [0, 0, 0, 0, 0]],
                 'ones': [[0, 1, 0], [1, 1, 1], [0, 1, 0]],
                 'center': [[2]],
                 'below': [[0, 0, 0], [1, 0, 3], [0, 0, 0]]},
     'FAILURES': ('weight_missing',
                  'weight_null',
                  'weight_scalar',
                  'weight_flat',
                  'weight_ragged',
                  'weight_size',
                  'weight_zero',
                  'weight_bool',
                  'weight_string',
                  'weight_null_element',
                  'weight_object_element',
                  'weight_negative',
                  'weight_outside',
                  'weight_below',
                  'method',
                  'unused_weights',
                  'unused_percentile',
                  'circle_window',
                  'circle_window_null',
                  'radius_missing',
                  'radius_null',
                  'radius_zero',
                  'radius_negative',
                  'radius_bool',
                  'geographic',
                  'compound',
                  'missing_crs',
                  'missing_grid',
                  'shear',
                  'mirror',
                  'x_oversize',
                  'y_oversize',
                  'band_zero',
                  'band_high',
                  'band_fraction',
                  'alpha'),
     'COMPARISONS': (('ones-mean.cog.tif', 'control-mean.cog.tif'),
                     ('ones-stddev.cog.tif', 'control-stddev.cog.tif'),
                     ('ones-sum.cog.tif', 'control-sum.cog.tif')),
     'BOUNDARY_PAIR': ('north-mean.cog.tif', 'below.cog.tif')}
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign)
                    and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)]
              for key in expected}
    if any(len(nodes) != 1 or repr(ast.literal_eval(nodes[0])) != repr(expected[key])
           for key, nodes in actual.items()):
        raise RegistrationError('weighted circle fixed 18/72 case, kernel or comparison matrix changed')
    if any(word in source for word in ('operators.raster_compute', '_focal_weighted_values', '_focal_circle_window', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('focal circle oracle must remain independent')


TWO_ROUND_BOUNDARY_REQUIRED = {'scripts/test/online-hosted-raster-boundary-clean-two-rounds-gate.sh': ('start_online_geopython_runtime',
                                                                         '--mount',
                                                                         '--suite raster-workflow',
                                                                         '--wait-protection-owners develop,manager',
                                                                         'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
                                                                         'online_raster_lineage.py prepare',
                                                                         'online-raster-boundary-clean-two-rounds-fixture.py '
                                                                         'stop',
                                                                         'unset '
                                                                         'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
 'business/scripts/online-raster-boundary-clean-two-rounds-fixture.py': ('NFSExport',
                                                                         'owned_root',
                                                                         'verify_sources(root)',
                                                                         'np.array_equal',
                                                                         'GDAL_TIFF_INTERNAL_MASK',
                                                                         'full_source_pixels_verified',
                                                                         'runtime_workspaces_removed',
                                                                         'archive_files',
                                                                         'oracle cache hash changed',
                                                                         'source file changed',
                                                                         'archive history missing',
                                                                         'validate_expected_export',
                                                                         'archive_expectations',
                                                                         'allow_pickle=False',
                                                                         'def cleanup_oracle',
                                                                         'full_check=True',
                                                                         'nearest_overviews',
                                                                         'private_control',
                                                                         'affine_transform',
                                                                         'expected_overview_count',
                                                                         'CASES',
                                                                         'np.array_equal(actual, expected, '
                                                                         'equal_nan=True)',
                                                                         'GetMaskBand().ReadAsArray()',
                                                                         'case_shape(name)',
                                                                         'from collections import Counter',
                                                                         'counts = Counter(int(value) for value in '
                                                                         'values[valid])',
                                                                         'counts[value], -value',
                                                                         'category = baseline[cell]',
                                                                         'baseline = shrunk',
                                                                         'frozen_priorities_verified',
                                                                         'complete_round_snapshots_verified',
                                                                         'actual default and explicit one-round COG '
                                                                         'pixels or masks differ',
                                                                         'actual round or frozen-priority witness '
                                                                         'differs',
                                                                         'GetGeoTransform(can_return_null=True)',
                                                                         'reference(CASES[name][0])',
                                                                         'values[509:512, 509:512]',
                                                                         'mask[504, 510] = 0',
                                                                         'alpha[505, 511] = 0.',
                                                                         'boundary-two-rounds-cogs',
                                                                         'boundary-two-rounds-expectations'),
 'scripts/test/raster-boundary-clean-two-rounds-persistence-online.py': ('raster.validate_identity',
                                                                         'raster.EDITOR_PERMISSIONS',
                                                                         'budget != 600',
                                                                         'BudgetClient',
                                                                         'remaining()',
                                                                         'validate_success',
                                                                         'lineage.collect',
                                                                         'lineage.validate(facts,wanted,public=True)',
                                                                         'lineage.verify_archive',
                                                                         'base.validate_scan',
                                                                         'raster.wait_lineage',
                                                                         'source_edges',
                                                                         'readonly.no_publication',
                                                                         'query language rewrote stored failure',
                                                                         'previous original COG changed',
                                                                         'fixture_call',
                                                                         'cleanup-oracle',
                                                                         'oracle cleanup also failed',
                                                                         'full_checks != 462',
                                                                         'six-coefficient affine proof differs',
                                                                         'frozen independent array archive changed',
                                                                         'archived_expectations',
                                                                         'physical_calls != 45',
                                                                         'len(cases) != 12',
                                                                         'len(failures) != 32',
                                                                         'exact_cause',
                                                                         'Monitor current output lineage differs',
                                                                         'raster.strict_sizes',
                                                                         'fixture.expected_cache_bytes()',
                                                                         'fixture.BAND_FAILURES',
                                                                         "params['rounds'] = rounds",
                                                                         'target_metadata_before[name] = '
                                                                         'copy.deepcopy(item)',
                                                                         'target_metadata_after != '
                                                                         'target_metadata_before',
                                                                         'source_metadata_after',
                                                                         'fixture.ROUND_FAILURES',
                                                                         'overview_resampling',
                                                                         'round_witnesses',
                                                                         'failed execution added scan',
                                                                         'failed execution added derived lineage',
                                                                         'located_or_unlocated.validate_success',
                                                                         'located_or_unlocated.validate_target_item',
                                                                         "'task': 'clean'",
                                                                         'fixture.WITNESSES', 'encoded(stored) != encoded(dag)'),
 'engines/geopython-workflow/test_online_boundary_clean_two_rounds_fixture.py': ('test_boundary_two_rounds_hosted_twelve_real_cogs_complete_oracle_rounds_controls_and_original_bytes',
                                                                                 'test_boundary_two_rounds_hosted_thirty_two_exact_submitted_failures_no_output_and_clean_workspace',
                                                                                 'test_boundary_two_rounds_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected',
                                                                                 'one-ulp',
                                                                                 'test_boundary_two_rounds_hosted_original_cog_archive_never_repairs_or_overwrites',
                                                                                 'test_boundary_two_rounds_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change',
                                                                                 'test_boundary_two_rounds_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup',
                                                                                 'test_boundary_two_rounds_hosted_frozen_expected_archive_rejects_tampering',
                                                                                 'test_boundary_two_rounds_hosted_independent_semantic_mutations_cannot_pass',
                                                                                 'test_boundary_two_rounds_hosted_oracle_never_calls_product_kernel_or_reducer',
                                                                                 'test_boundary_two_rounds_hosted_actual_grid_witnesses'),
 'scripts/test/raster-boundary-clean-two-rounds-persistence-online_test.py': ('test_full_matrix_12_success_32_failures_462_checks_and_cleanup',
                                                                              'test_every_affine_coefficient_and_type_is_required',
                                                                              'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
                                                                              'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken',
                                                                              'test_failures_preserve_primary_and_cleanup_error',
                                                                              'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced',
                                                                              'test_exact_12_16_matrix_rounds_and_actual_controls',
                                                                              'test_all_source_and_target_meta_snapshots_archived_and_mutation_rejected',
                                                                              'test_default_one_comparisons_and_measured_round_witnesses_are_required'),
 'business/scripts/online-raster-zonal-percentile-fixture.py': ('previous archive was deleted',
                                                                'previous archived bytes changed'),
 'scripts/utils/online_raster_lineage.py': ("'raster-boundary-clean-two-rounds-persistence': "
                                            "'boundary-two-rounds-lineage'",
                                            'BEGIN READ ONLY;',
                                            'statement_timeout',
                                            'workflow_matches',
                                            'validate_container',
                                            'closed(public, expected)',
                                            'closed(internal, expected)'),
 'scripts/test/online-raster-lineage_test.py': ('test_boundary_two_rounds_approved_single_source_current_raster_save_and_hidden_public_operation',),
 'Makefile': ('test_online_boundary_clean_two_rounds_fixture.py',
              '@$(MAKE) test-raster-boundary-clean-two-rounds-online-runner',
              'scripts/test/raster-boundary-clean-two-rounds-persistence-online_test.py',
              'scripts/test/online-raster-boundary-clean-two-rounds-fixture_test.py',
              'scripts/test/online-hosted-raster-boundary-clean-two-rounds-gate_test.py',
              'test_boundary_two_rounds_requires_exact_cog_matrix_budget_and_regressions')}


def validate_raster_boundary_clean_two_rounds(repository: Path, registered: set[str]) -> None:
    suite = 'raster-boundary-clean-two-rounds-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-boundary-clean-two-rounds-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-boundary-clean-two-rounds-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-boundary-clean-two-rounds-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('two-round boundary cleaning requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_boundary_clean_two_rounds_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('two-round boundary cleaning Native omitted from standard recipe')
    for relative, fragments in TWO_ROUND_BOUNDARY_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('two-round boundary cleaning strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-boundary-clean-two-rounds-fixture.py').read_text()
    tree = ast.parse(source)
    expected = {'CASES': {'change-default.cog.tif': ('change.tif', None, 'direct'),
               'change-one.cog.tif': ('change.tif', 1, 'direct'),
               'change-two.cog.tif': ('change.tif', 2, 'direct'),
               'frozen-default.cog.tif': ('frozen.tif', None, 'direct'),
               'frozen-one.cog.tif': ('frozen.tif', 1, 'direct'),
               'frozen-two.cog.tif': ('frozen.tif', 2, 'direct'),
               'seam-default.cog.tif': ('seam.tif', None, 'direct'),
               'seam-one.cog.tif': ('seam.tif', 1, 'direct'),
               'seam-two.cog.tif': ('seam.tif', 2, 'direct'),
               'reclassified-two.cog.tif': ('continuous.tif', 2, 'reclassify'),
               'empty-two.cog.tif': ('empty.tif', 2, 'direct'),
               'thin-two.cog.tif': ('thin.tif', 2, 'direct')},
     'ROUND_FAILURES': {'rounds_null': None,
                        'rounds_true': True,
                        'rounds_false': False,
                        'rounds_zero': 0,
                        'rounds_negative': -1,
                        'rounds_high': 3,
                        'rounds_fraction': 1.5,
                        'rounds_string': '2',
                        'rounds_list': [2],
                        'rounds_object': {}},
     'BAND_FAILURES': {'band_zero': 0,
                       'band_high': 99,
                       'band_fraction': 1.5,
                       'band_alpha': 3,
                       'class_fractional': 4,
                       'class_overflow': 5},
     'FAILURES': ('rounds_null',
                  'rounds_true',
                  'rounds_false',
                  'rounds_zero',
                  'rounds_negative',
                  'rounds_high',
                  'rounds_fraction',
                  'rounds_string',
                  'rounds_list',
                  'rounds_object',
                  'band_zero',
                  'band_high',
                  'band_fraction',
                  'band_alpha',
                  'class_fractional',
                  'class_overflow'),
     'COMPARISONS': (('change-default.cog.tif', 'change-one.cog.tif'),
                     ('frozen-default.cog.tif', 'frozen-one.cog.tif'),
                     ('seam-default.cog.tif', 'seam-one.cog.tif')),
     'WITNESSES': {'change-one.cog.tif': (1, 1, 1.0),
                   'change-two.cog.tif': (1, 1, 0.0),
                   'frozen-one.cog.tif': (0, 1, 0.0),
                   'frozen-two.cog.tif': (0, 1, 0.0),
                   'seam-one.cog.tif': (510, 510, 1.0),
                   'seam-two.cog.tif': (510, 510, 0.0)}}
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign)
                    and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)]
              for key in expected}
    if any(len(nodes) != 1 or repr(ast.literal_eval(nodes[0])) != repr(expected[key])
           for key, nodes in actual.items()):
        raise RegistrationError('two-round boundary cleaning fixed 12/32 case, parameter or witness matrix changed')
    if any(word in source for word in ('operators.raster_compute', '_boundary_clean_round', 'raster_boundary_clean(', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('two-round boundary cleaning oracle must remain independent')


REGION_REQUIRED = {'business/scripts/online-raster-region-group-fixture.py': ('NFSExport', 'source_arrays', 'from collections import deque', 'answer[row,col]=number', 'values[yy,xx]==values[row,col]', 'np.array_equal(actual,expected,equal_nan=True)', 'verify_csv', 'validate_records', 'pixel_conservation_verified', 'full_check=True', 'nearest_overviews(ds,actual)', 'def prepare_oracle', 'def cleanup_oracle', 'allow_pickle=False', 'private_control', 'archive_files', 'archive_expectations', 'validate_expected_export', 'full_source_pixels_verified', 'GetNoDataValueAsUInt64'), 'scripts/test/raster-region-group-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'full_checks != 819', 'len(cases) != 18', 'len(failures) != 36', 'physical_calls != 55', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.strict_sizes', 'validate_table_preview', 'validate_item(item', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_outputs'), 'scripts/test/online-hosted-raster-region-group-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-region-group-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'engines/geopython-workflow/test_online_region_group_fixture.py': ('test_region_hosted_eighteen_mixed_results_complete_oracle_and_original_bytes', 'test_region_hosted_thirty_six_exact_submitted_failures_no_output_and_clean_workspace', 'test_region_hosted_independent_semantic_mutations_cannot_pass', 'test_region_hosted_oracle_never_calls_product_kernel_or_reducer', 'test_region_hosted_csv_corruption_cannot_pass'), 'scripts/test/raster-region-group-persistence-online_test.py': ('test_full_matrix_18_success_36_failures_819_checks_and_cleanup', 'test_exact_18_18_matrix_region_parameters_and_csv_routes', 'test_all_source_and_target_meta_snapshots_archived_and_mutation_rejected'), 'Makefile': ('test_online_region_group_fixture.py', 'test-raster-region-group-online-runner', 'scripts/test/raster-region-group-persistence-online_test.py', 'test_region_requires_exact_cog_matrix_budget_and_regressions')}

def validate_raster_region_group(repository: Path, registered: set[str]) -> None:
    suite = 'raster-region-group-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-region-group-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-region-group-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-region-group-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal region requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_region_group_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('focal region Native omitted from standard recipe')
    for relative, fragments in REGION_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('focal region strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-region-group-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'four.cog.tif': ('values.tif', 4, 'direct'), 'eight.cog.tif': ('values.tif', 8, 'direct'), 'reclass-four.cog.tif': ('values.tif', 4, 'reclassify'), 'reclass-eight.cog.tif': ('values.tif', 8, 'reclassify'), 'cross-four.cog.tif': ('cross-block.tif', 4, 'direct'), 'cross-eight.cog.tif': ('cross-block.tif', 8, 'direct'), 'repeat-four.cog.tif': ('values.tif', 4, 'direct'), 'late-merge.cog.tif': ('late.tif', 4, 'direct'), 'coverage.cog.tif': ('coverage.tif', 8, 'direct'), 'rotated.cog.tif': ('rotated.tif', 4, 'direct'), 'geographic.cog.tif': ('geographic.tif', 8, 'direct'), 'unlocated.cog.tif': ('unlocated.tif', 4, 'direct'), 'int64.cog.tif': ('int64-safe.tif', 4, 'direct'), 'uint64.cog.tif': ('uint64-safe.tif', 8, 'direct'), 'invalid-four.cog.tif': ('invalid.tif', 4, 'direct'), 'invalid-eight.cog.tif': ('invalid.tif', 8, 'direct'), 'stats-four.csv': ('values.tif', 4, 'statistics'), 'stats-eight.csv': ('values.tif', 8, 'statistics')}
    expected_failures = ('band_zero', 'band_high', 'band_bool', 'band_string', 'band_null', 'band_list', 'connectivity_zero', 'connectivity_three', 'connectivity_bool', 'connectivity_string', 'connectivity_null', 'connectivity_list', 'alpha', 'fractional', 'unsafe_positive', 'unsafe_negative', 'int64', 'uint64')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('focal region fixed 18/36 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('focal region oracle must remain independent')




WEIGHTED_MEAN_REQUIRED = {
    'scripts/test/online-hosted-raster-focal-weighted-mean-gate.sh': (
        'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
        'online_raster_lineage.py prepare', 'online-raster-focal-weighted-mean-fixture.py stop',
        'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
    ),
    'business/scripts/online-raster-focal-weighted-mean-fixture.py': (
        'NFSExport', 'owned_root', 'from decimal import Decimal', 'context.prec=2000', 'zip(samples,kernel)',
        'float(total/mass)', 'case_shape(name)', 'source_arrays(name)', 'weight_orientation_verified', 'valid_weight_renormalization_verified', 'verify_sources(root)', 'np.array_equal',
        'GDAL_TIFF_INTERNAL_MASK', 'full_source_pixels_verified', 'runtime_workspaces_removed', 'archive_files',
        'oracle cache hash changed', 'source file changed', 'weighted_mean-cogs', 'archive history missing',
        'weighted_mean-expectations', 'validate_expected_export', 'archive_expectations',
        'allow_pickle=False', 'def cleanup_oracle', 'full_check=True', 'nearest_overviews', 'private_control',
    ),
    'scripts/test/raster-focal-weighted-mean-persistence-online.py': (
        'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()',
        'validate_success', 'lineage.collect', 'lineage.validate(facts,wanted,public=True)',
        'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges',
        'readonly.no_publication', 'query language rewrote stored failure', 'previous original COG changed',
        'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1032',
        'frozen independent array archive changed', 'archived_expectations',
        'physical_calls != 73', 'len(cases) != 16', 'len(failures) != 56', 'exact_cause',
        'Monitor current output lineage differs', 'raster.strict_sizes', 'fixture.expected_cache_bytes()',
    ),
    'engines/geopython-workflow/test_online_focal_weighted_mean_fixture.py': (
        'test_weighted_mean_hosted_sixteen_real_cogs_complete_oracle_weight_and_mean_controls_and_original_bytes',
        'test_weighted_mean_hosted_fifty_six_exact_submitted_failures_no_output_and_clean_workspace',
        'test_weighted_mean_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected', 'one-ulp',
        'test_weighted_mean_hosted_original_cog_archive_never_repairs_or_overwrites',
        'test_weighted_mean_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change',
        'test_weighted_mean_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup',
        'test_weighted_mean_hosted_frozen_expected_archive_rejects_tampering',
    ),
    'scripts/test/raster-focal-weighted-mean-persistence-online_test.py': (
        'test_full_matrix_16_success_56_failures_1032_checks_and_cleanup',
        'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
        'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken',
        'test_failures_preserve_primary_and_cleanup_error',
        'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced',
    ),
    'business/scripts/online-raster-zonal-percentile-fixture.py': (
        'previous archive was deleted', 'previous archived bytes changed',
    ),
    'scripts/utils/online_raster_lineage.py': (
        "'raster-focal-weighted-mean-persistence': 'weighted_mean-lineage'", 'BEGIN READ ONLY;', 'statement_timeout',
        'workflow_matches', 'validate_container', 'closed(public, expected)', 'closed(internal, expected)',
    ),
    'scripts/test/online-raster-lineage_test.py': (
        'test_weighted_mean_approved_single_source_current_raster_save_and_hidden_public_operation',
    ),
    'Makefile': (
        'test_online_focal_weighted_mean_fixture.py', '@$(MAKE) test-raster-focal-weighted-mean-online-runner',
        'scripts/test/raster-focal-weighted-mean-persistence-online_test.py',
        'scripts/test/online-raster-focal-weighted-mean-fixture_test.py',
        'scripts/test/online-hosted-raster-focal-weighted-mean-gate_test.py',
        'test_weighted_mean_requires_exact_cog_matrix_budget_and_regressions',
    ),
}

def validate_raster_focal_weighted_mean(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-weighted-mean-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-focal-weighted-mean-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-focal-weighted-mean-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-focal-weighted-mean-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal weighted_mean requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_focal_weighted_mean_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('focal weighted_mean Native omitted from standard recipe')
    for relative, fragments in WEIGHTED_MEAN_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('focal weighted_mean strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-focal-weighted-mean-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES','KERNELS')}
    expected_cases = {'weighted-3.cog.tif': ('values.tif', 3, 'weighted_mean', 'asymmetric-3', 'direct'), 'weighted-5.cog.tif': ('values.tif', 5, 'weighted_mean', 'asymmetric-5', 'direct'), 'ones-3.cog.tif': ('ones.tif', 3, 'weighted_mean', 'ones-3', 'direct'), 'ones-5.cog.tif': ('ones.tif', 5, 'weighted_mean', 'ones-5', 'direct'), 'control-mean-3.cog.tif': ('ones.tif', 3, 'mean', None, 'direct'), 'control-mean-5.cog.tif': ('ones.tif', 5, 'mean', None, 'direct'), 'scaled-3.cog.tif': ('values.tif', 3, 'weighted_mean', 'scaled-3', 'direct'), 'scaled-5.cog.tif': ('values.tif', 5, 'weighted_mean', 'scaled-5', 'direct'), 'zero-center.cog.tif': ('support.tif', 3, 'weighted_mean', 'asymmetric-3', 'direct'), 'invalid.cog.tif': ('invalid.tif', 5, 'weighted_mean', 'asymmetric-5', 'direct'), 'reclassified.cog.tif': ('values.tif', 3, 'weighted_mean', 'asymmetric-3', 'reclassify'), 'rotated.cog.tif': ('rotated.tif', 5, 'weighted_mean', 'asymmetric-5', 'direct'), 'unlocated-row.cog.tif': ('unlocated.tif', 3, 'weighted_mean', 'asymmetric-3', 'direct'), 'int64.cog.tif': ('int64.tif', 3, 'weighted_mean', 'asymmetric-3', 'direct'), 'uint64.cog.tif': ('uint64.tif', 5, 'weighted_mean', 'asymmetric-5', 'direct'), 'encoding.cog.tif': ('encoding.tif', 5, 'weighted_mean', 'asymmetric-5', 'direct')}
    expected_failures = ('weights_missing', 'weights_null', 'weights_number', 'weights_boolean', 'weights_string', 'weights_object', 'weights_empty', 'weights_vector', 'weights_rows', 'weights_columns', 'weights_ragged', 'weights_zero', 'weights_negative', 'weights_bool_element', 'weights_string_element', 'weights_null_element', 'weights_object_element', 'weights_list_element', 'unused_mean', 'unused_variety_null', 'unused_percentile', 'method', 'window_value', 'window_boolean', 'band_zero', 'band_high', 'alpha', 'complex')
    expected_kernels = {'asymmetric-3': [[0, 1, 2], [3, 0, 5], [6, 7, 8]], 'asymmetric-5': [[0, 1, 2, 3, 4], [5, 0, 7, 8, 9], [1, 2, 3, 4, 0], [6, 7, 8, 9, 1], [2, 3, 4, 5, 6]], 'ones-3': [[1, 1, 1], [1, 1, 1], [1, 1, 1]], 'ones-5': [[1, 1, 1, 1, 1], [1, 1, 1, 1, 1], [1, 1, 1, 1, 1], [1, 1, 1, 1, 1], [1, 1, 1, 1, 1]], 'scaled-3': [[0, 8, 16], [24, 0, 40], [48, 56, 64]], 'scaled-5': [[0, 8, 16, 24, 32], [40, 0, 56, 64, 72], [8, 16, 24, 32, 0], [48, 56, 64, 72, 8], [16, 24, 32, 40, 48]]}
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)
        or repr(ast.literal_eval(actual['KERNELS'][0])) != repr(expected_kernels)): raise RegistrationError('focal weighted_mean fixed 16/56 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('focal weighted_mean oracle must remain independent')


WEIGHTED_STDDEV_REQUIRED = {
    'scripts/test/online-hosted-raster-focal-weighted-stddev-gate.sh': (
        'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
        'online_raster_lineage.py prepare', 'online-raster-focal-weighted-stddev-fixture.py stop',
        'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
    ),
    'business/scripts/online-raster-focal-weighted-stddev-fixture.py': (
        'NFSExport', 'owned_root', 'from decimal import Decimal', 'context.prec=3000', 'zip(samples,kernel)',
        'float(variance.sqrt())', 'case_shape(name)', 'source_arrays(name)', 'weight_orientation_verified', 'valid_weight_renormalization_verified', 'population_stddev_verified', 'verify_sources(root)', 'np.array_equal',
        'GDAL_TIFF_INTERNAL_MASK', 'full_source_pixels_verified', 'runtime_workspaces_removed', 'archive_files',
        'oracle cache hash changed', 'source file changed', 'weighted_stddev-cogs', 'archive history missing',
        'weighted_stddev-expectations', 'validate_expected_export', 'archive_expectations',
        'allow_pickle=False', 'def cleanup_oracle', 'full_check=True', 'nearest_overviews', 'private_control',
    ),
    'scripts/test/raster-focal-weighted-stddev-persistence-online.py': (
        'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()',
        'validate_success', 'lineage.collect', 'lineage.validate(facts,wanted,public=True)',
        'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges',
        'readonly.no_publication', 'query language rewrote stored failure', 'previous original COG changed',
        'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1032',
        'frozen independent array archive changed', 'archived_expectations',
        'physical_calls != 73', 'len(cases) != 16', 'len(failures) != 56', 'exact_cause',
        'Monitor current output lineage differs', 'raster.strict_sizes', 'fixture.expected_cache_bytes()',
    ),
    'engines/geopython-workflow/test_online_focal_weighted_stddev_fixture.py': (
        'test_weighted_stddev_hosted_sixteen_real_cogs_complete_oracle_weight_and_stddev_controls_and_original_bytes',
        'test_weighted_stddev_hosted_fifty_six_exact_submitted_failures_no_output_and_clean_workspace',
        'test_weighted_stddev_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected', 'one-ulp',
        'test_weighted_stddev_hosted_original_cog_archive_never_repairs_or_overwrites',
        'test_weighted_stddev_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change',
        'test_weighted_stddev_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup',
        'test_weighted_stddev_hosted_frozen_expected_archive_rejects_tampering',
    ),
    'scripts/test/raster-focal-weighted-stddev-persistence-online_test.py': (
        'test_full_matrix_16_success_56_failures_1032_checks_and_cleanup',
        'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
        'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken',
        'test_failures_preserve_primary_and_cleanup_error',
        'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced',
    ),
    'business/scripts/online-raster-zonal-percentile-fixture.py': (
        'previous archive was deleted', 'previous archived bytes changed',
    ),
    'scripts/utils/online_raster_lineage.py': (
        "'raster-focal-weighted-stddev-persistence': 'weighted_stddev-lineage'", 'BEGIN READ ONLY;', 'statement_timeout',
        'workflow_matches', 'validate_container', 'closed(public, expected)', 'closed(internal, expected)',
    ),
    'scripts/test/online-raster-lineage_test.py': (
        'test_weighted_stddev_approved_single_source_current_raster_save_and_hidden_public_operation',
    ),
    'Makefile': (
        'test_online_focal_weighted_stddev_fixture.py', '@$(MAKE) test-raster-focal-weighted-stddev-online-runner',
        'scripts/test/raster-focal-weighted-stddev-persistence-online_test.py',
        'scripts/test/online-raster-focal-weighted-stddev-fixture_test.py',
        'scripts/test/online-hosted-raster-focal-weighted-stddev-gate_test.py',
        'test_weighted_stddev_requires_exact_cog_matrix_budget_and_regressions',
    ),
}

def validate_raster_focal_weighted_stddev(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-weighted-stddev-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-focal-weighted-stddev-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-focal-weighted-stddev-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-focal-weighted-stddev-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal weighted_stddev requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_focal_weighted_stddev_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('focal weighted_stddev Native omitted from standard recipe')
    for relative, fragments in WEIGHTED_STDDEV_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('focal weighted_stddev strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-focal-weighted-stddev-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES','KERNELS')}
    expected_cases = {'weighted-3.cog.tif': ('values.tif', 3, 'weighted_stddev', 'asymmetric-3', 'direct'), 'weighted-5.cog.tif': ('values.tif', 5, 'weighted_stddev', 'asymmetric-5', 'direct'), 'ones-3.cog.tif': ('ones.tif', 3, 'weighted_stddev', 'ones-3', 'direct'), 'ones-5.cog.tif': ('ones.tif', 5, 'weighted_stddev', 'ones-5', 'direct'), 'control-stddev-3.cog.tif': ('ones.tif', 3, 'stddev', None, 'direct'), 'control-stddev-5.cog.tif': ('ones.tif', 5, 'stddev', None, 'direct'), 'scaled-3.cog.tif': ('values.tif', 3, 'weighted_stddev', 'scaled-3', 'direct'), 'scaled-5.cog.tif': ('values.tif', 5, 'weighted_stddev', 'scaled-5', 'direct'), 'zero-center.cog.tif': ('support.tif', 3, 'weighted_stddev', 'asymmetric-3', 'direct'), 'invalid.cog.tif': ('invalid.tif', 5, 'weighted_stddev', 'asymmetric-5', 'direct'), 'reclassified.cog.tif': ('values.tif', 3, 'weighted_stddev', 'asymmetric-3', 'reclassify'), 'rotated.cog.tif': ('rotated.tif', 5, 'weighted_stddev', 'asymmetric-5', 'direct'), 'unlocated-row.cog.tif': ('unlocated.tif', 3, 'weighted_stddev', 'asymmetric-3', 'direct'), 'int64.cog.tif': ('int64.tif', 3, 'weighted_stddev', 'asymmetric-3', 'direct'), 'uint64.cog.tif': ('uint64.tif', 5, 'weighted_stddev', 'asymmetric-5', 'direct'), 'encoding.cog.tif': ('encoding.tif', 5, 'weighted_stddev', 'asymmetric-5', 'direct')}
    expected_failures = ('weights_missing', 'weights_null', 'weights_number', 'weights_boolean', 'weights_string', 'weights_object', 'weights_empty', 'weights_vector', 'weights_rows', 'weights_columns', 'weights_ragged', 'weights_zero', 'weights_negative', 'weights_bool_element', 'weights_string_element', 'weights_null_element', 'weights_object_element', 'weights_list_element', 'unused_stddev', 'unused_variety_null', 'unused_percentile', 'method', 'window_value', 'window_boolean', 'band_zero', 'band_high', 'alpha', 'complex')
    expected_kernels = {'asymmetric-3': [[0, 1, 2], [3, 0, 5], [6, 7, 8]], 'asymmetric-5': [[0, 1, 2, 3, 4], [5, 0, 7, 8, 9], [1, 2, 3, 4, 0], [6, 7, 8, 9, 1], [2, 3, 4, 5, 6]], 'ones-3': [[1, 1, 1], [1, 1, 1], [1, 1, 1]], 'ones-5': [[1, 1, 1, 1, 1], [1, 1, 1, 1, 1], [1, 1, 1, 1, 1], [1, 1, 1, 1, 1], [1, 1, 1, 1, 1]], 'scaled-3': [[0, 8, 16], [24, 0, 40], [48, 56, 64]], 'scaled-5': [[0, 8, 16, 24, 32], [40, 0, 56, 64, 72], [8, 16, 24, 32, 0], [48, 56, 64, 72, 8], [16, 24, 32, 40, 48]]}
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)
        or repr(ast.literal_eval(actual['KERNELS'][0])) != repr(expected_kernels)): raise RegistrationError('focal weighted_stddev fixed 16/56 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('focal weighted_stddev oracle must remain independent')


WEIGHTED_SUM_REQUIRED = {
    'scripts/test/online-hosted-raster-focal-weighted-sum-gate.sh': (
        'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
        'online_raster_lineage.py prepare', 'online-raster-focal-weighted-sum-fixture.py stop',
        'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
    ),
    'business/scripts/online-raster-focal-weighted-sum-fixture.py': (
        'NFSExport', 'owned_root', 'from decimal import Decimal', 'context.prec=2000', 'zip(samples,kernel)',
        'float(sum((v*w for v,w in pairs),Decimal(0)))', 'case_shape(name)', 'source_arrays(name)', 'weight_orientation_verified', 'no_weight_renormalization_verified', 'weighted_sum_verified', 'verify_sources(root)', 'np.array_equal',
        'GDAL_TIFF_INTERNAL_MASK', 'full_source_pixels_verified', 'runtime_workspaces_removed', 'archive_files',
        'oracle cache hash changed', 'source file changed', 'weighted_sum-cogs', 'archive history missing',
        'weighted_sum-expectations', 'validate_expected_export', 'archive_expectations',
        'allow_pickle=False', 'def cleanup_oracle', 'full_check=True', 'nearest_overviews', 'private_control', 'for left,right,factor in COMPARISONS:', 'a.ReadAsArray()*factor',
    ),
    'scripts/test/raster-focal-weighted-sum-persistence-online.py': (
        'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()',
        'validate_success', 'lineage.collect', 'lineage.validate(facts,wanted,public=True)',
        'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges',
        'readonly.no_publication', 'query language rewrote stored failure', 'previous original COG changed',
        'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1032',
        'frozen independent array archive changed', 'archived_expectations',
        'physical_calls != 73', 'len(cases) != 16', 'len(failures) != 56', 'exact_cause',
        'Monitor current output lineage differs', 'raster.strict_sizes', 'fixture.expected_cache_bytes()',
    ),
    'engines/geopython-workflow/test_online_focal_weighted_sum_fixture.py': (
        'test_weighted_sum_hosted_sixteen_real_cogs_complete_oracle_weight_and_sum_controls_and_original_bytes',
        'test_weighted_sum_hosted_fifty_six_exact_submitted_failures_no_output_and_clean_workspace',
        'test_weighted_sum_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected', 'one-ulp',
        'test_weighted_sum_hosted_original_cog_archive_never_repairs_or_overwrites',
        'test_weighted_sum_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change',
        'test_weighted_sum_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup',
        'test_weighted_sum_hosted_frozen_expected_archive_rejects_tampering',
        'test_weighted_sum_hosted_independent_oracle_rejects_wrong_semantics', 'renormalized', 'dropped-scale', 'negative-clamped',
    ),
    'scripts/test/raster-focal-weighted-sum-persistence-online_test.py': (
        'test_full_matrix_16_success_56_failures_1032_checks_and_cleanup',
        'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
        'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken',
        'test_failures_preserve_primary_and_cleanup_error',
        'test_sum_scale_comparisons_and_no_renormalization_proof_cannot_weaken',
        'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced',
    ),
    'business/scripts/online-raster-zonal-percentile-fixture.py': (
        'previous archive was deleted', 'previous archived bytes changed',
    ),
    'scripts/utils/online_raster_lineage.py': (
        "'raster-focal-weighted-sum-persistence': 'weighted_sum-lineage'", 'BEGIN READ ONLY;', 'statement_timeout',
        'workflow_matches', 'validate_container', 'closed(public, expected)', 'closed(internal, expected)',
    ),
    'scripts/test/online-raster-lineage_test.py': (
        'test_weighted_sum_approved_single_source_current_raster_save_and_hidden_public_operation',
    ),
    'Makefile': (
        'test_online_focal_weighted_sum_fixture.py', '@$(MAKE) test-raster-focal-weighted-sum-online-runner',
        'scripts/test/raster-focal-weighted-sum-persistence-online_test.py',
        'scripts/test/online-raster-focal-weighted-sum-fixture_test.py',
        'scripts/test/online-hosted-raster-focal-weighted-sum-gate_test.py',
        'test_weighted_sum_requires_exact_cog_matrix_budget_and_regressions',
    ),
}

def validate_raster_focal_weighted_sum(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-weighted-sum-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-focal-weighted-sum-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-focal-weighted-sum-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-focal-weighted-sum-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal weighted_sum requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_focal_weighted_sum_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('focal weighted_sum Native omitted from standard recipe')
    for relative, fragments in WEIGHTED_SUM_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('focal weighted_sum strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-focal-weighted-sum-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES','KERNELS','COMPARISONS')}
    expected_cases = {'weighted-3.cog.tif': ('values.tif', 3, 'weighted_sum', 'asymmetric-3', 'direct'), 'weighted-5.cog.tif': ('values.tif', 5, 'weighted_sum', 'asymmetric-5', 'direct'), 'ones-3.cog.tif': ('ones.tif', 3, 'weighted_sum', 'ones-3', 'direct'), 'ones-5.cog.tif': ('ones.tif', 5, 'weighted_sum', 'ones-5', 'direct'), 'control-sum-3.cog.tif': ('ones.tif', 3, 'sum', None, 'direct'), 'control-sum-5.cog.tif': ('ones.tif', 5, 'sum', None, 'direct'), 'scaled-3.cog.tif': ('values.tif', 3, 'weighted_sum', 'scaled-3', 'direct'), 'scaled-5.cog.tif': ('values.tif', 5, 'weighted_sum', 'scaled-5', 'direct'), 'zero-center.cog.tif': ('support.tif', 3, 'weighted_sum', 'asymmetric-3', 'direct'), 'invalid.cog.tif': ('invalid.tif', 5, 'weighted_sum', 'asymmetric-5', 'direct'), 'reclassified.cog.tif': ('values.tif', 3, 'weighted_sum', 'asymmetric-3', 'reclassify'), 'rotated.cog.tif': ('rotated.tif', 5, 'weighted_sum', 'asymmetric-5', 'direct'), 'unlocated-row.cog.tif': ('unlocated.tif', 3, 'weighted_sum', 'asymmetric-3', 'direct'), 'int64.cog.tif': ('int64.tif', 3, 'weighted_sum', 'asymmetric-3', 'direct'), 'uint64.cog.tif': ('uint64.tif', 5, 'weighted_sum', 'asymmetric-5', 'direct'), 'encoding.cog.tif': ('encoding.tif', 5, 'weighted_sum', 'asymmetric-5', 'direct')}
    expected_failures = ('weights_missing', 'weights_null', 'weights_number', 'weights_boolean', 'weights_string', 'weights_object', 'weights_empty', 'weights_vector', 'weights_rows', 'weights_columns', 'weights_ragged', 'weights_zero', 'weights_negative', 'weights_bool_element', 'weights_string_element', 'weights_null_element', 'weights_object_element', 'weights_list_element', 'unused_sum', 'unused_variety_null', 'unused_percentile', 'method', 'window_value', 'window_boolean', 'band_zero', 'band_high', 'alpha', 'complex')
    expected_kernels = {'asymmetric-3': [[0, 1, 2], [3, 0, 5], [6, 7, 8]], 'asymmetric-5': [[0, 1, 2, 3, 4], [5, 0, 7, 8, 9], [1, 2, 3, 4, 0], [6, 7, 8, 9, 1], [2, 3, 4, 5, 6]], 'ones-3': [[1, 1, 1], [1, 1, 1], [1, 1, 1]], 'ones-5': [[1, 1, 1, 1, 1], [1, 1, 1, 1, 1], [1, 1, 1, 1, 1], [1, 1, 1, 1, 1], [1, 1, 1, 1, 1]], 'scaled-3': [[0, 8, 16], [24, 0, 40], [48, 56, 64]], 'scaled-5': [[0, 8, 16, 24, 32], [40, 0, 56, 64, 72], [8, 16, 24, 32, 0], [48, 56, 64, 72, 8], [16, 24, 32, 40, 48]]}
    expected_comparisons = (('weighted-3.cog.tif', 'scaled-3.cog.tif', 8), ('weighted-5.cog.tif', 'scaled-5.cog.tif', 8), ('ones-3.cog.tif', 'control-sum-3.cog.tif', 1), ('ones-5.cog.tif', 'control-sum-5.cog.tif', 1))
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)
        or repr(ast.literal_eval(actual['KERNELS'][0])) != repr(expected_kernels)
        or repr(ast.literal_eval(actual['COMPARISONS'][0])) != repr(expected_comparisons)): raise RegistrationError('focal weighted_sum fixed 16/56 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('focal weighted_sum oracle must remain independent')


PROXIMITY_RECTANGULAR_REQUIRED = {'scripts/test/online-hosted-raster-proximity-rectangular-gate.sh': ('start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'business/scripts/online-raster-proximity-rectangular-fixture.py': ('NFSExport', 'owned_root', 'Decimal.from_float', 'context.prec=200', 'from fractions import Fraction', 'np.minimum', 'full_pixel_and_mask_verification', 'full_source_pixels_verified', 'verify_sources(root)', 'runtime_workspaces_removed', 'archive_files', 'oracle cache hash changed', 'source file changed', 'proximity_rectangular-cogs', 'proximity_rectangular-expectations', 'validate_expected_export', 'allow_pickle=False', 'cleanup_oracle', 'full_check=True', 'nearest_overviews', 'private_control', 'distance_unit', 'actual_metre_nearest_verified', 'square_results_preserved'), 'scripts/test/raster-proximity-rectangular-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges', 'readonly.no_publication', 'query language rewrote stored failure', 'previous original COG changed', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 968', 'physical_calls != 69', 'len(cases) != 16', 'len(failures) != 52', 'exact_cause', 'raster.strict_sizes', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', 'overview_count(name)'), 'engines/geopython-workflow/test_online_proximity_rectangular_fixture.py': ('test_proximity_rectangular_hosted_sixteen_real_cogs_full_pixels_masks_units_and_raw_archive', 'test_proximity_rectangular_hosted_fifty_two_exact_submitted_failures_without_publication', 'test_proximity_rectangular_hosted_real_physical_corruption_is_rejected', 'one-ulp', 'test_proximity_rectangular_hosted_original_cog_archive_never_repairs_or_overwrites', 'test_proximity_rectangular_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change', 'test_proximity_rectangular_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup', 'test_proximity_rectangular_hosted_frozen_expected_archive_rejects_tampering', 'test_proximity_rectangular_hosted_independent_oracle_rejects_wrong_semantics'), 'scripts/test/raster-proximity-rectangular-persistence-online_test.py': ('test_full_matrix_16_success_52_failures_968_checks_and_cleanup', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_actual_unit_and_conditional_overview_proof_cannot_weaken', 'test_source_and_all_old_target_meta_change_is_rejected', 'test_failures_preserve_primary_and_cleanup_error', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced'), 'scripts/utils/online_raster_lineage.py': ("'raster-proximity-rectangular-persistence': 'proximity_rectangular-lineage'", 'BEGIN READ ONLY;', 'statement_timeout', 'closed(public, expected)', 'closed(internal, expected)'), 'scripts/test/online-raster-lineage_test.py': ('test_proximity_rectangular_approved_single_source_current_raster_save_and_hidden_public_operation',), 'Makefile': ('test_online_proximity_rectangular_fixture.py', '@$(MAKE) test-raster-proximity-rectangular-online-runner', 'scripts/test/raster-proximity-rectangular-persistence-online_test.py', 'scripts/test/online-raster-proximity-rectangular-fixture_test.py', 'scripts/test/online-hosted-raster-proximity-rectangular-gate_test.py', 'test_proximity_rectangular_requires_exact_cog_matrix_budget_and_regressions'), 'business/scripts/online-raster-zonal-percentile-fixture.py': ('previous archive was deleted', 'previous archived bytes changed')}

def validate_raster_proximity_rectangular(repository: Path, registered: set[str]) -> None:
    suite = 'raster-proximity-rectangular-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-proximity-rectangular-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-proximity-rectangular-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-proximity-rectangular-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal proximity_rectangular requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_proximity_rectangular_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('focal proximity_rectangular Native omitted from standard recipe')
    for relative, fragments in PROXIMITY_RECTANGULAR_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('focal proximity_rectangular strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-proximity-rectangular-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'wide.cog.tif': ('wide.tif', 2, [1]), 'tall.cog.tif': ('tall.tif', 2, [1]), 'fractional.cog.tif': ('fractional.tif', 2, [1]), 'foot.cog.tif': ('foot.tif', 2, [1]), 'survey.cog.tif': ('survey.tif', 2, [1]), 'second-band.cog.tif': ('second-band.tif', 2, [1]), 'nodata.cog.tif': ('nodata.tif', 2, [1]), 'mask.cog.tif': ('mask.tif', 2, [1]), 'alpha.cog.tif': ('alpha.tif', 2, [1]), 'zero-negative.cog.tif': ('zero-negative.tif', 2, [0, -3]), 'uint32.cog.tif': ('uint32.tif', 2, [0, 4294967295]), 'cross-block.cog.tif': ('cross-block.tif', 2, [1]), 'row.cog.tif': ('row.tif', 2, [1]), 'column.cog.tif': ('column.tif', 2, [1]), 'square-metre.cog.tif': ('square-metre.tif', 2, [1]), 'square-foot.cog.tif': ('square-foot.tif', 2, [1])}
    expected_failures = ('band_zero', 'band_high', 'band_boolean', 'alpha_band', 'float', 'complex', 'int64', 'uint64', 'targets_empty', 'targets_duplicate', 'targets_boolean', 'targets_fraction', 'targets_object', 'targets_range', 'targets_nonlist', 'no_target', 'invalid_targets', 'geographic', 'compound', 'missing_crs', 'missing_grid', 'unknown_unit', 'rotated_x', 'rotated_y', 'orientation', 'singular')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('focal proximity_rectangular fixed 16/52 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('focal proximity_rectangular oracle must remain independent')


PROXIMITY_ORTHOGONAL_REQUIRED = {'scripts/test/online-hosted-raster-proximity-orthogonal-gate.sh': ('start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'business/scripts/online-raster-proximity-orthogonal-fixture.py': ('NFSExport', 'owned_root', 'Decimal(exact.numerator)', 'context.prec=200', 'from fractions import Fraction', 'np.minimum', 'full_pixel_and_mask_verification', 'full_source_pixels_verified', 'verify_sources(root)', 'runtime_workspaces_removed', 'archive_files', 'oracle cache hash changed', 'source file changed', 'proximity_orthogonal-cogs', 'proximity_orthogonal-expectations', 'validate_expected_export', 'allow_pickle=False', 'cleanup_oracle', 'full_check=True', 'nearest_overviews', 'private_control', 'distance_unit', 'actual_metre_nearest_verified', 'square_results_preserved'), 'scripts/test/raster-proximity-orthogonal-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges', 'readonly.no_publication', 'query language rewrote stored failure', 'previous original COG changed', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1251', 'physical_calls != 79', 'len(cases) != 18', 'len(failures) != 60', 'exact_cause', 'raster.strict_sizes', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', 'overview_count(name)'), 'engines/geopython-workflow/test_online_proximity_orthogonal_fixture.py': ('test_proximity_orthogonal_hosted_eighteen_real_cogs_full_pixels_masks_units_and_raw_archive', 'test_proximity_orthogonal_hosted_sixty_exact_submitted_failures_without_publication', 'test_proximity_orthogonal_hosted_real_physical_corruption_is_rejected', 'one-ulp', 'test_proximity_orthogonal_hosted_original_cog_archive_never_repairs_or_overwrites', 'test_proximity_orthogonal_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change', 'test_proximity_orthogonal_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup', 'test_proximity_orthogonal_hosted_frozen_expected_archive_rejects_tampering', 'test_proximity_orthogonal_hosted_independent_oracle_rejects_wrong_semantics', 'test_proximity_orthogonal_hosted_world_centres_independently_audit_every_case', 'test_proximity_orthogonal_hosted_tiff_roundtrip_exact_dot_and_three_units'), 'scripts/test/raster-proximity-orthogonal-persistence-online_test.py': ('test_full_matrix_18_success_60_failures_1251_checks_and_cleanup', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_actual_unit_and_conditional_overview_proof_cannot_weaken', 'test_source_and_all_old_target_meta_change_is_rejected', 'test_failures_preserve_primary_and_cleanup_error', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced'), 'scripts/utils/online_raster_lineage.py': ("'raster-proximity-orthogonal-persistence': 'proximity_orthogonal-lineage'", 'BEGIN READ ONLY;', 'statement_timeout', 'closed(public, expected)', 'closed(internal, expected)'), 'scripts/test/online-raster-lineage_test.py': ('test_proximity_orthogonal_approved_single_source_current_raster_save_and_hidden_public_operation',), 'Makefile': ('test_online_proximity_orthogonal_fixture.py', '@$(MAKE) test-raster-proximity-orthogonal-online-runner', 'scripts/test/raster-proximity-orthogonal-persistence-online_test.py', 'scripts/test/online-raster-proximity-orthogonal-fixture_test.py', 'scripts/test/online-hosted-raster-proximity-orthogonal-gate_test.py', 'test_proximity_orthogonal_requires_exact_cog_matrix_budget_and_regressions'), 'business/scripts/online-raster-zonal-percentile-fixture.py': ('previous archive was deleted', 'previous archived bytes changed')}

def validate_raster_proximity_orthogonal(repository: Path, registered: set[str]) -> None:
    suite = 'raster-proximity-orthogonal-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-proximity-orthogonal-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-proximity-orthogonal-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-proximity-orthogonal-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal proximity_orthogonal requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_proximity_orthogonal_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('focal proximity_orthogonal Native omitted from standard recipe')
    for relative, fragments in PROXIMITY_ORTHOGONAL_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('focal proximity_orthogonal strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-proximity-orthogonal-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'wide.cog.tif': ('wide.tif', 2, [1]), 'tall.cog.tif': ('tall.tif', 2, [1]), 'foot.cog.tif': ('foot.tif', 2, [1]), 'survey.cog.tif': ('survey.tif', 2, [1]), 'second-band.cog.tif': ('second-band.tif', 2, [1]), 'nodata.cog.tif': ('nodata.tif', 2, [1]), 'mask.cog.tif': ('mask.tif', 2, [1]), 'alpha.cog.tif': ('alpha.tif', 2, [1]), 'zero-negative.cog.tif': ('zero-negative.tif', 2, [0, -3]), 'cross-block.cog.tif': ('cross-block.tif', 2, [1]), 'square-metre.cog.tif': ('square-metre.tif', 2, [1]), 'square-foot.cog.tif': ('square-foot.tif', 2, [1]), 'square-survey.cog.tif': ('square-survey.tif', 2, [1]), 'rotate90.cog.tif': ('rotate90.tif', 2, [1]), 'rotate180.cog.tif': ('rotate180.tif', 2, [1]), 'rotate270.cog.tif': ('rotate270.tif', 2, [1]), 'oblique-square.cog.tif': ('oblique-square.tif', 2, [1]), 'oblique-wide.cog.tif': ('oblique-wide.tif', 2, [1])}
    expected_failures = ('band_zero', 'band_high', 'band_boolean', 'alpha_band', 'float', 'complex', 'int64', 'uint64', 'targets_empty', 'targets_duplicate', 'targets_boolean', 'targets_fraction', 'targets_object', 'targets_range', 'targets_nonlist', 'no_target', 'invalid_targets', 'geographic', 'compound', 'missing_crs', 'missing_grid', 'unknown_unit', 'rotated_x', 'rotated_y', 'orientation', 'singular', 'tiny_shear', 'rounded_dot', 'south_up', 'rotated_mirror')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('focal proximity_orthogonal fixed 18/60 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('focal proximity_orthogonal oracle must remain independent')



PROTECTED_SIEVE_REQUIRED = {
    'scripts/test/online-hosted-raster-sieve-protected-gate.sh': (
        'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
        'online_raster_lineage.py prepare', 'online-raster-sieve-protected-fixture.py stop',
        'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
    ),
    'business/scripts/online-raster-sieve-protected-fixture.py': (
        'NFSExport', 'owned_root', 'from collections import deque', 'category not in (CASES[name][3] or ())',
        'len(components[other][1])>=minimum', 'case_shape(name)', 'verify_sources(root)', 'np.array_equal',
        'GDAL_TIFF_INTERNAL_MASK', 'full_source_pixels_verified', 'runtime_workspaces_removed', 'archive_files',
        'oracle cache hash changed', 'source file changed', 'sieve-protected-cogs', 'archive history missing',
        'sieve-protected-expectations', 'validate_expected_export', 'archive_expectations',
        'allow_pickle=False', 'def cleanup_oracle', 'full_check=True', 'nearest_overviews', 'private_control',
    ),
    'scripts/test/raster-sieve-protected-persistence-online.py': (
        'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()',
        'validate_success', 'lineage.collect', 'lineage.validate(facts,wanted,public=True)',
        'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges',
        'readonly.no_publication', 'query language rewrote stored failure', 'previous original COG changed',
        'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 968',
        'frozen independent array archive changed', 'archived_expectations',
        'physical_calls != 69', 'len(cases) != 16', 'len(failures) != 52', 'exact_cause',
        'Monitor current output lineage differs', 'raster.strict_sizes', 'fixture.expected_cache_bytes()',
    ),
    'engines/geopython-workflow/test_online_sieve_protected_fixture.py': (
        'test_protected_sieve_hosted_sixteen_real_cogs_complete_oracle_protected_controls_and_original_bytes',
        'test_protected_sieve_hosted_fifty_two_exact_submitted_failures_no_output_and_clean_workspace',
        'test_protected_sieve_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected', 'one-ulp',
        'test_protected_sieve_hosted_original_cog_archive_never_repairs_or_overwrites',
        'test_protected_sieve_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change',
        'test_protected_sieve_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup',
        'test_protected_sieve_hosted_frozen_expected_archive_rejects_tampering',
        'test_protected_sieve_native_only_integral_float_rejection',
        'test_protected_sieve_hosted_independent_oracle_rejects_wrong_protection_and_old_semantics', 'small_protected_donor', 'large_protected_refused',
    ),
    'scripts/test/raster-sieve-protected-persistence-online_test.py': (
        'test_full_matrix_16_success_52_failures_968_checks_and_cleanup',
        'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
        'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken',
        'test_failures_preserve_primary_and_cleanup_error',
        'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced',
    ),
    'business/scripts/online-raster-zonal-percentile-fixture.py': (
        'previous archive was deleted', 'previous archived bytes changed',
    ),
    'scripts/utils/online_raster_lineage.py': (
        "'raster-sieve-protected-persistence': 'sieve-protected-lineage'", 'BEGIN READ ONLY;', 'statement_timeout',
        'workflow_matches', 'validate_container', 'closed(public, expected)', 'closed(internal, expected)',
    ),
    'scripts/test/online-raster-lineage_test.py': (
        'test_protected_sieve_approved_single_source_current_raster_save_and_hidden_public_operation',
    ),
    'Makefile': (
        'test_online_sieve_protected_fixture.py', '@$(MAKE) test-raster-sieve-protected-online-runner',
        'scripts/test/raster-sieve-protected-persistence-online_test.py',
        'scripts/test/online-raster-sieve-protected-fixture_test.py',
        'scripts/test/online-hosted-raster-sieve-protected-gate_test.py',
        'test_protected_sieve_requires_exact_cog_matrix_budget_and_regressions',
    ),
}

def validate_raster_sieve_protected(repository: Path, registered: set[str]) -> None:
    suite = 'raster-sieve-protected-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-sieve-protected-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-sieve-protected-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-sieve-protected-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('protected sieve requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_sieve_protected_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('protected sieve Native omitted from standard recipe')
    for relative, fragments in PROTECTED_SIEVE_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('protected sieve strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-sieve-protected-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'protected-four.cog.tif': ('classes.tif', 5, 4, (9,), 'direct'), 'protected-eight.cog.tif': ('classes.tif', 5, 8, (9,), 'direct'), 'reclassified.cog.tif': ('classes.tif', 5, 4, (-9,), 'reclassify'), 'zero-negative.cog.tif': ('classes.tif', 5, 4, (0, -5), 'direct'), 'donor-eligibility.cog.tif': ('donors.tif', 5, 4, (7, 9), 'direct'), 'omitted.cog.tif': ('classes.tif', 5, 4, None, 'direct'), 'empty.cog.tif': ('classes.tif', 5, 4, (), 'direct'), 'unmatched.cog.tif': ('classes.tif', 5, 4, (123456,), 'direct'), 'ordered.cog.tif': ('classes.tif', 5, 4, (0, 9), 'direct'), 'reversed.cog.tif': ('classes.tif', 5, 4, (9, 0), 'direct'), 'all-protected.cog.tif': ('classes.tif', 5, 4, (-9007199254740991, -5, -2, 0, 1, 2, 3, 7, 8, 9, 9007199254740991), 'direct'), 'identity.cog.tif': ('classes.tif', 1, 4, (9,), 'direct'), 'unlocated.cog.tif': ('unlocated.tif', 5, 4, (0,), 'direct'), 'invalid.cog.tif': ('invalid.tif', 5, 8, (7,), 'direct'), 'int64.cog.tif': ('safe-int64.tif', 5, 4, (0, 9), 'direct'), 'uint64.cog.tif': ('safe-uint64.tif', 5, 8, (0, 9), 'direct')}
    expected_failures = ('band', 'threshold_zero', 'threshold_negative', 'threshold_fractional', 'threshold_boolean', 'connectivity_value', 'connectivity_fractional', 'connectivity_boolean', 'fractional', 'unsafe', 'int64', 'uint64', 'alpha', 'complex', 'protected_null', 'protected_boolean', 'protected_scalar', 'protected_string', 'protected_object', 'protected_item_boolean', 'protected_item_object', 'protected_item_fractional', 'protected_positive_unsafe', 'protected_negative_unsafe', 'protected_duplicate', 'protected_nested')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('protected sieve fixed 16/52 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('protected sieve oracle must remain independent')

AREA_SIEVE_REQUIRED = {
    'scripts/test/online-hosted-raster-sieve-area-gate.sh': (
        'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
        'online_raster_lineage.py prepare', 'online-raster-sieve-area-fixture.py stop',
        'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
    ),
    'business/scripts/online-raster-sieve-area-fixture.py': (
        'NFSExport', 'owned_root', 'from collections import deque', 'category not in (CASES[name][3] or ())',
        'len(components[other][1])*cell_area>=minimum', 'case_shape(name)', 'from decimal import Decimal', 'context.prec=2000', 'len(pixels)*cell_area<minimum', 'source_reference(name)', 'threshold_contract(name)', 'area_comparison_verified', 'linear_units_verified', 'verify_sources(root)', 'np.array_equal',
        'GDAL_TIFF_INTERNAL_MASK', 'full_source_pixels_verified', 'runtime_workspaces_removed', 'archive_files',
        'oracle cache hash changed', 'source file changed', 'sieve-area-cogs', 'archive history missing',
        'sieve-area-expectations', 'validate_expected_export', 'archive_expectations',
        'allow_pickle=False', 'def cleanup_oracle', 'full_check=True', 'nearest_overviews', 'private_control',
    ),
    'scripts/test/raster-sieve-area-persistence-online.py': (
        'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()',
        'validate_success', 'lineage.collect', 'lineage.validate(facts,wanted,public=True)',
        'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges',
        'readonly.no_publication', 'query language rewrote stored failure', 'previous original COG changed',
        'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1160',
        'frozen independent array archive changed', 'archived_expectations',
        "fact.get('threshold_contract')!=fixture.threshold_contract(name)", "resource_type='file'", 'physical_calls != 81', 'len(cases) != 16', 'len(failures) != 64', 'exact_cause',
        'Monitor current output lineage differs', 'raster.strict_sizes', 'fixture.expected_cache_bytes()',
    ),
    'engines/geopython-workflow/test_online_sieve_area_fixture.py': (
        'test_area_sieve_hosted_sixteen_real_cogs_complete_oracle_protected_controls_and_original_bytes',
        'test_area_sieve_hosted_sixty_four_exact_submitted_failures_no_output_and_clean_workspace',
        'test_area_sieve_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected', 'one-ulp',
        'test_area_sieve_hosted_original_cog_archive_never_repairs_or_overwrites',
        'test_area_sieve_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change',
        'test_area_sieve_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup',
        'test_area_sieve_hosted_frozen_expected_archive_rejects_tampering',
        'test_area_sieve_native_only_integral_float_rejection', 'test_area_sieve_source_crs_roundtrip_and_complete_native_source_facts',
        'test_area_sieve_hosted_independent_oracle_rejects_area_protection_and_original_semantic_tampering', 'small_protected_donor', 'large_protected_refused',
    ),
    'scripts/test/raster-sieve-area-persistence-online_test.py': (
        'test_full_matrix_16_success_64_failures_1160_checks_and_cleanup',
        'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
        'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken',
        'test_failures_preserve_primary_and_cleanup_error',
        'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced',
    ),
    'business/scripts/online-raster-zonal-percentile-fixture.py': (
        'previous archive was deleted', 'previous archived bytes changed',
    ),
    'scripts/utils/online_raster_lineage.py': (
        "'raster-sieve-area-persistence': 'sieve-area-lineage'", 'BEGIN READ ONLY;', 'statement_timeout',
        'workflow_matches', 'validate_container', 'closed(public, expected)', 'closed(internal, expected)',
    ),
    'scripts/test/online-raster-lineage_test.py': (
        'test_area_sieve_approved_single_source_current_raster_save_and_hidden_public_operation',
    ),
    'Makefile': (
        'test_online_sieve_area_fixture.py', '@$(MAKE) test-raster-sieve-area-online-runner',
        'scripts/test/raster-sieve-area-persistence-online_test.py',
        'scripts/test/online-raster-sieve-area-fixture_test.py',
        'scripts/test/online-hosted-raster-sieve-area-gate_test.py',
        'test_area_sieve_requires_exact_cog_matrix_budget_and_regressions',
    ),
}

def validate_raster_sieve_area(repository: Path, registered: set[str]) -> None:
    suite = 'raster-sieve-area-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-sieve-area-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-sieve-area-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-sieve-area-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('area sieve requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_sieve_area_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('area sieve Native omitted from standard recipe')
    for relative, fragments in AREA_SIEVE_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('area sieve strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-sieve-area-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'area-four.cog.tif': ('classes.tif', 30.0, 4, (7, 9), 'direct'), 'area-eight.cog.tif': ('classes.tif', 30.0, 8, (7, 9), 'direct'), 'reclassified.cog.tif': ('classes.tif', 30.0, 4, (-7, -9), 'reclassify'), 'feet.cog.tif': ('feet.tif', 1.1148364800000001, 4, (7, 9), 'direct'), 'survey-feet.cog.tif': ('survey-feet.tif', 1.1148409393592984, 4, (7, 9), 'direct'), 'equal.cog.tif': ('classes.tif', 12.0, 4, (7, 9), 'direct'), 'below.cog.tif': ('classes.tif', 11.999999999999998, 4, (7, 9), 'direct'), 'above.cog.tif': ('classes.tif', 12.000000000000002, 4, (7, 9), 'direct'), 'pixel-control.cog.tif': ('classes.tif', 5, 4, (7, 9), 'pixels'), 'omitted.cog.tif': ('classes.tif', 30.0, 4, None, 'direct'), 'empty.cog.tif': ('classes.tif', 30.0, 4, (), 'direct'), 'int64.cog.tif': ('safe-int64.tif', 30.0, 4, (0, 9), 'direct'), 'uint64.cog.tif': ('safe-uint64.tif', 30.0, 8, (0, 9), 'direct'), 'resolution-10.cog.tif': ('resolution-10.tif', 1000.0, 4, (7, 9), 'direct'), 'resolution-30.cog.tif': ('resolution-30.tif', 1000.0, 4, (7, 9), 'direct'), 'invalid.cog.tif': ('invalid.tif', 30.0, 8, (7, 9), 'direct')}
    expected_failures = ('threshold_missing', 'threshold_both', 'threshold_pixel_null', 'threshold_area_null_with_pixel', 'area_null', 'area_boolean', 'area_zero', 'area_negative', 'area_string', 'area_array', 'area_object', 'grid_geographic', 'grid_no_crs', 'grid_no_transform', 'grid_rotated_x', 'grid_rotated_y', 'grid_unknown_unit', 'grid_compound', 'band', 'alpha', 'complex', 'connectivity_value', 'connectivity_fractional', 'connectivity_boolean', 'fractional', 'unsafe', 'int64', 'uint64', 'protected_null', 'protected_item_object', 'protected_duplicate', 'protected_positive_unsafe')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('area sieve fixed 16/64 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('area sieve oracle must remain independent')

def validate_vector_zonal_percentile(repository: Path, registered: set[str]) -> None:
    suite = 'vector-zonal-percentile-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  vector-zonal-percentile-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-zonal-percentile-persistence'" not in job.group('body')
        or "&& inputs.suite != 'vector-zonal-percentile-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('zonal percentile requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_vector_zonal_percentile_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('zonal percentile Native omitted from standard recipe')
    for relative, fragments in VECTOR_ZONAL_PERCENTILE_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('zonal percentile strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-vector-zonal-percentile-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'original.gpkg': ('values.tif', 'zones.geojson', None, False, 'zonal_'), 'p0.gpkg': ('values.tif', 'zones.geojson', 0, False, 'zonal_'), 'p100.gpkg': ('values.tif', 'zones.geojson', 100, False, 'zonal_'), 'p50-odd.gpkg': ('odd.tif', 'small.geojson', 50, False, 'zonal_'), 'p50-even.gpkg': ('even.tif', 'small.geojson', 50, False, 'zonal_'), 'p25.gpkg': ('values.tif', 'zones.geojson', 25, False, 'zonal_'), 'p75.gpkg': ('values.tif', 'zones.geojson', 75, False, 'zonal_'), 'p33-center.gpkg': ('values.tif', 'zones.geojson', 33.3, False, 'zonal_'), 'p33-touched.gpkg': ('values.tif', 'zones.geojson', 33.3, True, 'zonal_'), 'custom.gpkg': ('values.tif', 'zones.geojson', 25, False, 'custom_'), 'empty-prefix.gpkg': ('values.tif', 'zones.geojson', 75, False, ''), 'classified.gpkg': ('values.tif', None, 90, False, 'zonal_'), 'int64.gpkg': ('int64.tif', 'small.geojson', 50, False, 'zonal_'), 'uint64.gpkg': ('uint64.tif', 'small.geojson', 50, False, 'zonal_'), 'invalid-values.gpkg': ('invalid.tif', 'small.geojson', 50, False, 'zonal_'), 'empty-zones.gpkg': ('even.tif', 'empty.gpkg', 50, False, 'zonal_')}
    expected_failures = ('percentile_null', 'percentile_boolean', 'percentile_string', 'percentile_array', 'percentile_object', 'percentile_low', 'percentile_high', 'band_zero', 'band_high', 'band_boolean', 'band_string', 'complex', 'alpha', 'touched_numeric', 'touched_null', 'prefix_null', 'prefix_boolean', 'collision_stats', 'collision_percentile', 'collision_value', 'different_crs', 'missing_crs', 'missing_grid', 'singular_grid')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('zonal percentile fixed 16/48 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('zonal percentile oracle must remain independent')


VECTOR_ZONAL_PERCENTILES_REQUIRED = {'scripts/test/online-hosted-vector-zonal-percentiles-gate.sh': ('start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare', 'online-vector-zonal-percentiles-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'business/scripts/online-vector-zonal-percentiles-fixture.py': ('NFSExport', 'owned_root', 'from fractions import Fraction', 'WIDTH,HEIGHT=519,515', 'position=(count-1)*Fraction(float(p))/100', 'sorted(float(v)', 'shapely.contains', 'shapely.intersection', 'np.array_equal', 'GDAL_TIFF_INTERNAL_MASK', 'full_gpkg_checks', 'full_source_pixels_verified', 'full_source_vectors_verified', 'runtime_workspaces_removed', 'archive_files', 'oracle cache hash changed', 'source file changed', 'percentile changed original eight fields', 'vector-zonal-percentiles-gpkgs', 'actual batch and single percentile GeoPackages differ', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'GetNoDataValueAsInt64', 'GetNoDataValueAsUInt64', "with state.open('xb')"), 'scripts/test/vector-zonal-percentiles-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()', 'validate_preview', 'legacy.validate_success', 'lineage.collect', 'lineage.validate(facts, wanted, public=True)', 'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges', 'readonly.no_publication', 'query language rewrote stored failure', 'previous complete GeoPackage changed', 'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1416', 'physical_calls != 97', 'len(cases) != 16', 'len(failures) != 80', 'exact_cause', 'Monitor current output lineage differs', 'vector_persistence.validate_item', 'archive_response', 'archived_oracle', 'actual_single_controls_verified', 'target_metadata_after!=target_metadata_before', 'source_metadata_after!=before'), 'engines/geopython-workflow/test_online_vector_zonal_percentiles_fixture.py': ('test_vector_zonal_percentiles_sixteen_real_gpkgs_complete_independent_oracle_and_raw_archive', 'test_vector_zonal_percentiles_eighty_exact_submitted_failures_and_no_output', 'test_vector_zonal_percentiles_real_physical_corruption_is_rejected', 'one-ulp', 'test_vector_zonal_percentiles_gpkg_archive_never_repairs_or_overwrites', 'test_vector_zonal_percentiles_worker_stdin_frozen_oracle_poison_and_cleanup', 'test_vector_zonal_percentiles_schema_change_has_no_live_feature', 'test_vector_zonal_percentiles_oracle_binds_exact_source_script_and_run', 'test_vector_zonal_percentiles_oracle_independent_of_product_readers_and_rank_helpers', 'later-value', 'timeout=90'), 'scripts/test/vector-zonal-percentiles-persistence-online_test.py': ('test_full_matrix_16_success_80_failures_1416_checks_and_cleanup', 'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_failures_preserve_primary_and_cleanup_error', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_exactly_192_raw_terminal_responses_and_metadata_snapshots', 'test_source_and_prior_target_metadata_cannot_change', 'test_fixed_case_dags_all_conflicts_and_actual_controls'), 'business/scripts/online-raster-zonal-percentile-fixture.py': ('previous archive was deleted', 'previous archived bytes changed'), 'scripts/utils/online_raster_lineage.py': ("'vector-zonal-percentiles-persistence': 'vector-zonal-percentiles-lineage'",), 'Makefile': ('test_online_vector_zonal_percentiles_fixture.py', '@$(MAKE) test-vector-zonal-percentiles-online-runner', 'scripts/test/vector-zonal-percentiles-persistence-online_test.py', 'scripts/test/online-vector-zonal-percentiles-fixture_test.py', 'scripts/test/online-hosted-vector-zonal-percentiles-gate_test.py', 'test_vector_zonal_percentiles_requires_exact_gpkg_matrix_budget_and_regressions'), 'scripts/test/online-vector-zonal-percentiles-fixture_test.py': ('test_main_archives_frozen_oracle_sources_and_sixteen_gpkg_original_bytes',)}

def validate_vector_zonal_percentiles(repository: Path, registered: set[str]) -> None:
    suite = 'vector-zonal-percentiles-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  vector-zonal-percentiles-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-zonal-percentiles-persistence'" not in job.group('body')
        or "&& inputs.suite != 'vector-zonal-percentiles-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('vector zonal percentiles requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_vector_zonal_percentiles_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('vector zonal percentiles Native omitted from standard recipe')
    for relative, fragments in VECTOR_ZONAL_PERCENTILES_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('vector zonal percentiles strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-vector-zonal-percentiles-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES','PERCENTILES')}
    expected_cases = {'original.gpkg': ('values.tif', 'zones.geojson', None, False, 'zonal_'), 'batch.gpkg': ('values.tif', 'zones.geojson', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), False, 'zonal_'), 'p0.gpkg': ('values.tif', 'zones.geojson', (0.0,), False, 'zonal_'), 'p25.gpkg': ('values.tif', 'zones.geojson', (25.0,), False, 'zonal_'), 'p33.gpkg': ('values.tif', 'zones.geojson', (33.3,), False, 'zonal_'), 'p50.gpkg': ('values.tif', 'zones.geojson', (50.0,), False, 'zonal_'), 'p75.gpkg': ('values.tif', 'zones.geojson', (75.0,), False, 'zonal_'), 'p100.gpkg': ('values.tif', 'zones.geojson', (100.0,), False, 'zonal_'), 'touched.gpkg': ('values.tif', 'zones.geojson', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), True, 'zonal_'), 'custom.gpkg': ('values.tif', 'zones.geojson', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), False, 'custom_'), 'empty-prefix.gpkg': ('values.tif', 'zones.geojson', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), False, ''), 'classified.gpkg': ('values.tif', None, (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), False, 'zonal_'), 'int64.gpkg': ('int64.tif', 'small.geojson', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), False, 'zonal_'), 'uint64.gpkg': ('uint64.tif', 'small.geojson', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), False, 'zonal_'), 'invalid-values.gpkg': ('invalid.tif', 'small.geojson', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), False, 'zonal_'), 'empty-zones.gpkg': ('values.tif', 'empty.gpkg', (0.0, 25.0, 33.3, 50.0, 75.0, 100.0), False, 'zonal_')}
    expected_failures = ('list_null', 'list_empty', 'scalar_number', 'scalar_boolean', 'scalar_string', 'scalar_object', 'item_null', 'item_boolean', 'item_string', 'item_list', 'item_object', 'item_low', 'item_high', 'duplicate', 'descending', 'band_zero', 'band_high', 'band_boolean', 'band_string', 'complex', 'alpha', 'touched_numeric', 'touched_null', 'prefix_null', 'prefix_boolean', 'collision_stats', 'collision_percentile', 'collision_value', 'collision_later_percentile', 'collision_later_value', 'different_crs', 'missing_crs', 'missing_grid', 'singular_grid', 'weights_conflict', 'weighted_stddev_conflict', 'area_conflict', 'area_stddev_conflict', 'frequency_conflict', 'frequency_null_conflict')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures) or ast.literal_eval(actual['PERCENTILES'][0]) != (0.0, 25.0, 33.3, 50.0, 75.0, 100.0)): raise RegistrationError('vector zonal percentiles fixed 16/80 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('vector zonal percentiles oracle must remain independent')


SAMPLE_POINTS_REQUIRED = {'business/scripts/online-raster-sample-points-fixture.py': ('NFSExport', 'source_arrays', 'Decimal.from_float', 'context.prec=5000', 'def in_cell', 'cross==0 and i in (1,2)', 'def oracle_scope', 'source_files', 'fixture_sha256', 'GetNoDataValueAsInt64', 'GetNoDataValueAsUInt64', 'verify_vector', 'shape.equals_exact(wanted_shape,0)', 'validate_properties', 'np.array_equal(band.ReadAsArray(),values,equal_nan=True)', 'archive_files', 'archived_oracle', 'oracle cache became writable', 'runtime_workspaces_removed', "with state.open('xb')"), 'scripts/test/raster-sample-points-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'full_checks != 712', 'len(cases) != 16', 'len(failures) != 36', 'physical_calls != 53', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'vector_persistence.validate_item', 'validate_preview', 'target_metadata_after != target_metadata_before','source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_gpkgs'), 'scripts/test/online-hosted-raster-sample-points-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-sample-points-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'engines/geopython-workflow/test_online_sample_points_fixture.py': ('test_sample_hosted_sixteen_complete_gpkgs_and_raw_archive', 'test_sample_hosted_thirty_six_exact_failures_no_output_or_workspace', 'test_sample_hosted_real_corruption_rejected', 'test_sample_hosted_archive_never_repairs', 'test_sample_hosted_oracle_does_not_call_product_inverse_or_reader', 'test_sample_hosted_worker_stdin_scope_poison_and_cleanup'), 'scripts/test/raster-sample-points-persistence-online_test.py': ('test_full_matrix_16_success_36_failures_712_checks_and_cleanup', 'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced'), 'Makefile': ('test_online_sample_points_fixture.py', 'test-raster-sample-points-online-runner', 'scripts/test/raster-sample-points-persistence-online_test.py', 'test_sample_points_requires_exact_gpkg_matrix_budget_and_regressions')}

def validate_sample_points(repository: Path, registered: set[str]) -> None:
    suite = 'raster-sample-points-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-sample-points-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-sample-points-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-sample-points-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('point sampling requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_sample_points_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('point sampling Native omitted from standard recipe')
    for relative, fragments in SAMPLE_POINTS_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('point sampling strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-sample-points-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'base.gpkg': ('values.tif', 'points.gpkg', 'direct'), 'boundaries.gpkg': ('values.tif', 'edges.gpkg', 'direct'), 'rotated.gpkg': ('rotated.tif', 'rotated-points.gpkg', 'direct'), 'geographic.gpkg': ('geographic.tif', 'geographic-points.gpkg', 'direct'), 'west-south.gpkg': ('west-south.tif', 'west-south-points.gpkg', 'direct'), 'band-two.gpkg': ('values.tif', 'band-points.gpkg', 'direct'), 'coverage.gpkg': ('coverage.tif', 'coverage-points.gpkg', 'direct'), 'int64.gpkg': ('int64.tif', 'integer-points.gpkg', 'direct'), 'uint64.gpkg': ('uint64.tif', 'integer-points.gpkg', 'direct'), 'duplicates.gpkg': ('values.tif', 'duplicate-points.gpkg', 'direct'), 'cross-block.gpkg': ('large.tif', 'large-points.gpkg', 'direct'), 'classified.gpkg': ('values.tif', 'points.gpkg', 'classify'), 'calculated.gpkg': ('values.tif', 'points.gpkg', 'calculate'), 'all-nodata.gpkg': ('invalid.tif', 'inside-points.gpkg', 'direct'), 'all-outside.gpkg': ('values.tif', 'outside-points.gpkg', 'direct'), 'empty.gpkg': ('values.tif', 'empty-points.gpkg', 'direct')}
    expected_failures = ('band_zero', 'band_negative', 'band_high', 'band_boolean', 'band_string', 'band_null', 'band_list', 'band_object', 'different_crs', 'multipoint', 'line', 'polygon', 'collision_value', 'collision_status', 'alpha', 'inexact_int64', 'inexact_uint64', 'missing_grid')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('point sampling fixed 16/36 matrix changed')
    if any(word in source for word in ('operators.raster_compute', '_sample_point_groups', 'gdal.InvGeoTransform', 'raster_sample_points(')):
        raise RegistrationError('point sampling oracle must remain independent')


SAMPLE_POINTS_BILINEAR_REQUIRED = {'business/scripts/online-raster-sample-points-bilinear-fixture.py': ('NFSExport', 'source_arrays', 'Decimal.from_float', 'context.prec=5000', 'def in_cell', 'def centre_support', 'Fraction(cross(east,south))', 'if wx*wy>0', "if mode=='combined'", 'def oracle_scope', 'source_files', 'fixture_sha256', 'GetNoDataValueAsInt64', 'GetNoDataValueAsUInt64', 'verify_vector', 'shape.equals_exact(wanted_shape,0)', 'validate_properties', 'np.array_equal(band.ReadAsArray(),values,equal_nan=True)', 'archive_files', 'archived_oracle', 'oracle cache became writable', 'runtime_workspaces_removed', "with state.open('xb')"), 'scripts/test/raster-sample-points-bilinear-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'full_checks != 968', 'len(cases) != 16', 'len(failures) != 52', 'physical_calls != 69', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'vector_persistence.validate_item', 'validate_preview', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_gpkgs'), 'scripts/test/online-hosted-raster-sample-points-bilinear-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-sample-points-bilinear-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'engines/geopython-workflow/test_online_sample_points_bilinear_fixture.py': ('test_sample_bilinear_hosted_sixteen_complete_gpkgs_and_raw_archive', 'test_sample_bilinear_hosted_fifty_two_exact_failures_no_output_or_workspace', 'test_sample_bilinear_hosted_real_corruption_rejected', 'test_sample_bilinear_hosted_archive_never_repairs', 'test_sample_bilinear_hosted_oracle_does_not_call_product_inverse_or_reader', 'test_sample_bilinear_hosted_worker_stdin_scope_poison_and_cleanup', 'test_sample_bilinear_hosted_new_semantics_cannot_weaken_frozen_oracle'), 'scripts/test/raster-sample-points-bilinear-persistence-online_test.py': ('test_full_matrix_16_success_52_failures_968_checks_and_cleanup', 'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_exact_twenty_six_failure_parameters_and_combined_original_sources'), 'Makefile': ('test_online_sample_points_bilinear_fixture.py', 'test-raster-sample-points-bilinear-online-runner', 'scripts/test/raster-sample-points-bilinear-persistence-online_test.py', 'test_sample_points_bilinear_requires_exact_gpkg_matrix_budget_and_regressions')}

def validate_sample_points_bilinear(repository: Path, registered: set[str]) -> None:
    suite = 'raster-sample-points-bilinear-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-sample-points-bilinear-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-sample-points-bilinear-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-sample-points-bilinear-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('point sampling requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_sample_points_bilinear_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('point sampling Native omitted from standard recipe')
    for relative, fragments in SAMPLE_POINTS_BILINEAR_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('point sampling strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-raster-sample-points-bilinear-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'cell-default.gpkg': ('values.tif', 'points.gpkg', 'direct', None), 'cell-explicit.gpkg': ('values.tif', 'points.gpkg', 'direct', 'cell'), 'bilinear-support.gpkg': ('support.tif', 'support-points.gpkg', 'direct', 'bilinear'), 'center-ulp.gpkg': ('centers.tif', 'center-points.gpkg', 'direct', 'bilinear'), 'boundaries.gpkg': ('values.tif', 'edges.gpkg', 'direct', 'bilinear'), 'rotated.gpkg': ('rotated.tif', 'rotated-points.gpkg', 'direct', 'bilinear'), 'geographic.gpkg': ('geographic.tif', 'geographic-points.gpkg', 'direct', 'bilinear'), 'west-south.gpkg': ('west-south.tif', 'west-south-points.gpkg', 'direct', 'bilinear'), 'coverage.gpkg': ('coverage.tif', 'coverage-points.gpkg', 'direct', 'bilinear'), 'int64.gpkg': ('int64.tif', 'integer-points.gpkg', 'direct', 'bilinear'), 'uint64.gpkg': ('uint64.tif', 'integer-points.gpkg', 'direct', 'bilinear'), 'cross-block.gpkg': ('large.tif', 'large-points.gpkg', 'direct', 'bilinear'), 'combined.gpkg': ('values.tif', 'points.gpkg', 'combined', 'bilinear'), 'all-nodata.gpkg': ('invalid.tif', 'inside-points.gpkg', 'direct', 'bilinear'), 'all-outside.gpkg': ('values.tif', 'outside-points.gpkg', 'direct', 'bilinear'), 'empty.gpkg': ('values.tif', 'empty-points.gpkg', 'direct', 'bilinear')}
    expected_failures = ('band_zero', 'band_negative', 'band_high', 'band_boolean', 'band_string', 'band_null', 'band_list', 'band_object', 'different_crs', 'multipoint', 'line', 'polygon', 'collision_value', 'collision_status', 'alpha', 'inexact_int64', 'inexact_uint64', 'missing_grid', 'method_null', 'method_boolean', 'method_number', 'method_empty', 'method_unknown', 'method_case', 'method_list', 'method_object')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('point sampling fixed 16/52 matrix changed')
    if any(word in source for word in ('operators.raster_compute', '_sample_point_groups', 'gdal.InvGeoTransform', 'raster_sample_points(')):
        raise RegistrationError('point sampling oracle must remain independent')




VECTOR_ZONAL_WEIGHTED_REQUIRED = {'scripts/test/online-hosted-vector-zonal-weighted-gate.sh': ('start_online_geopython_runtime',
                                                              '--mount',
                                                              '--suite raster-workflow',
                                                              '--wait-protection-owners develop,manager',
                                                              'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
                                                              'online_raster_lineage.py prepare',
                                                              'online-vector-zonal-weighted-fixture.py stop',
                                                              'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
 'business/scripts/online-vector-zonal-weighted-fixture.py': ('NFSExport',
                                                              'owned_root',
                                                              'WIDTH,HEIGHT=519,515',
                                                              'context.prec=2000',
                                                              'Decimal.from_float(float(v))',
                                                              'weighted=sum((v*w for v,w in pairs)',
                                                              'shapely.contains',
                                                              'shapely.intersection',
                                                              'classified_rows',
                                                              'np.array_equal',
                                                              'GDAL_TIFF_INTERNAL_MASK',
                                                              'full_gpkg_checks',
                                                              'full_source_pixels_verified',
                                                              'full_source_vectors_verified',
                                                              'runtime_workspaces_removed',
                                                              'archive_files',
                                                              'oracle cache hash changed',
                                                              'source file changed',
                                                              'weights changed original eight fields',
                                                              'all-one weights changed original sum or mean',
                                                              'absolute scale or mean invariance changed',
                                                              'exact weighted statistic differs',
                                                              'vector-zonal-weighted-gpkgs'),
 'scripts/test/vector-zonal-weighted-persistence-online.py': ('raster.validate_identity',
                                                              'raster.EDITOR_PERMISSIONS',
                                                              'budget != 600',
                                                              'BudgetClient',
                                                              'remaining()',
                                                              'validate_preview',
                                                              'legacy.validate_success',
                                                              'lineage.collect',
                                                              'lineage.validate(facts, wanted, public=True)',
                                                              'lineage.verify_archive',
                                                              'base.validate_scan',
                                                              'raster.wait_lineage',
                                                              'source_edges',
                                                              'readonly.no_publication',
                                                              'query language rewrote stored failure',
                                                              'previous complete GeoPackage changed',
                                                              'fixture_call',
                                                              'cleanup-oracle',
                                                              'oracle cleanup also failed',
                                                              'full_checks != 1512',
                                                              'physical_calls != 103',
                                                              'len(cases) != 16',
                                                              'len(failures) != 86',
                                                              'exact_cause',
                                                              'Monitor current output lineage differs',
                                                              'vector_persistence.validate_item'),
 'engines/geopython-workflow/test_online_vector_zonal_weighted_fixture.py': ('test_vector_zonal_weighted_sixteen_real_gpkgs_complete_independent_oracle_and_raw_archive',
                                                                             'test_vector_zonal_weighted_eighty_six_exact_submitted_failures_and_no_output',
                                                                             'test_vector_zonal_weighted_real_physical_corruption_is_rejected',
                                                                             'one-ulp',
                                                                             'test_vector_zonal_weighted_gpkg_archive_never_repairs_or_overwrites',
                                                                             'test_vector_zonal_weighted_worker_stdin_frozen_oracle_poison_and_cleanup',
                                                                             'test_vector_zonal_weighted_independent_oracle_rejects_wrong_semantics_and_links',
                                                                             'normalize-sum',
                                                                             'drop-scale',
                                                                             'ones-mean',
                                                                             'directory-symlink',
                                                                             'ones-and-original-mean',
                                                                             'source-vector'),
 'scripts/test/vector-zonal-weighted-persistence-online_test.py': ('test_full_matrix_16_success_86_failures_1512_checks_and_cleanup',
                                                                   'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection',
                                                                   'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken',
                                                                   'test_failures_preserve_primary_and_cleanup_error',
                                                                   'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced'),
 'scripts/utils/online_raster_lineage.py': ("'vector-zonal-weighted-persistence': "
                                            "'vector-zonal-weighted-lineage'",),
 'scripts/test/online-raster-lineage_test.py': ('test_vector_zonal_weighted_approved_three_role_gpkg_facts_preserve_internal_and_public_boundaries',),
 'Makefile': ('test_online_vector_zonal_weighted_fixture.py',
              '@$(MAKE) test-vector-zonal-weighted-online-runner',
              'scripts/test/vector-zonal-weighted-persistence-online_test.py',
              'scripts/test/online-vector-zonal-weighted-fixture_test.py',
              'scripts/test/online-hosted-vector-zonal-weighted-gate_test.py',
              'test_vector_zonal_weighted_requires_exact_gpkg_matrix_budget_and_regressions'),
 'business/scripts/online-raster-zonal-percentile-fixture.py': ('previous archive was deleted',
                                                                'previous archived bytes changed')}


def validate_vector_zonal_weighted(repository: Path, registered: set[str]) -> None:
    suite = 'vector-zonal-weighted-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  vector-zonal-weighted-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-zonal-weighted-persistence'" not in job.group('body')
        or "&& inputs.suite != 'vector-zonal-weighted-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('zonal weighted requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_vector_zonal_weighted_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('zonal weighted Native omitted from standard recipe')
    for relative, fragments in VECTOR_ZONAL_WEIGHTED_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('zonal weighted strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-vector-zonal-weighted-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'weighted-center.gpkg': ('values.tif', 'zones.geojson', 'weights.tif', False, 'zonal_'), 'weighted-touched.gpkg': ('values.tif', 'zones.geojson', 'weights.tif', True, 'zonal_'), 'scaled.gpkg': ('values.tif', 'zones.geojson', 'scaled-weights.tif', False, 'zonal_'), 'ones.gpkg': ('values.tif', 'zones.geojson', 'ones-weights.tif', False, 'zonal_'), 'control-center.gpkg': ('values.tif', 'zones.geojson', None, False, 'zonal_'), 'control-touched.gpkg': ('values.tif', 'zones.geojson', None, True, 'zonal_'), 'weights-reclassified.gpkg': ('values.tif', 'zones.geojson', 'weight-classes.tif', False, 'zonal_'), 'values-reclassified.gpkg': ('values.tif', 'zones.geojson', 'weights.tif', False, 'zonal_'), 'classified.gpkg': ('values.tif', None, 'weights.tif', False, 'zonal_'), 'custom.gpkg': ('values.tif', 'zones.geojson', 'weights.tif', False, 'custom_'), 'empty-prefix.gpkg': ('values.tif', 'zones.geojson', 'weights.tif', False, ''), 'zero.gpkg': ('values.tif', 'zones.geojson', 'zero-weights.tif', False, 'zonal_'), 'invalid-values.gpkg': ('invalid.tif', 'small.geojson', 'small-weights.tif', False, 'zonal_'), 'empty-zones.gpkg': ('even.tif', 'empty.gpkg', 'small-weights.tif', False, 'zonal_'), 'int64.gpkg': ('int64.tif', 'small.geojson', 'int64-weights.tif', False, 'zonal_'), 'uint64.gpkg': ('uint64.tif', 'small.geojson', 'uint64-weights.tif', False, 'zonal_')}
    expected_failures = ('only_raster', 'only_band', 'raster_null', 'band_null', 'both_null', 'percentile_conflict', 'negative_weight', 'nodata_weight', 'mask_weight', 'alpha_weight', 'nan_weight', 'infinite_weight', 'values_band_zero', 'values_band_high', 'values_band_boolean', 'values_band_string', 'values_alpha', 'values_complex', 'weights_band_zero', 'weights_band_high', 'weights_band_boolean', 'weights_band_string', 'weights_alpha', 'weights_complex', 'weights_size', 'weights_crs', 'weights_shifted', 'weights_rotated', 'weights_missing_crs', 'weights_missing_grid', 'weights_singular', 'different_crs', 'missing_crs', 'missing_grid', 'singular_grid', 'touched_numeric', 'touched_null', 'prefix_null', 'prefix_boolean', 'collision_stats', 'collision_mass', 'collision_sum', 'collision_mean')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('zonal weighted fixed 16/86 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('zonal weighted oracle must remain independent')


VECTOR_ZONAL_WEIGHTED_STDDEV_REQUIRED = {'scripts/test/online-hosted-vector-zonal-weighted-stddev-gate.sh': ('start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare', 'online-vector-zonal-weighted-stddev-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'business/scripts/online-vector-zonal-weighted-stddev-fixture.py': ('NFSExport', 'owned_root', 'WIDTH,HEIGHT=519,515', 'context.prec=2000', 'Decimal.from_float(v)', 'weighted=sum((v*w for v,w in pairs)', 'shapely.contains', 'shapely.intersection', 'classified_rows', 'np.array_equal', 'GDAL_TIFF_INTERNAL_MASK', 'full_gpkg_checks', 'full_source_pixels_verified', 'full_source_vectors_verified', 'runtime_workspaces_removed', 'archive_files', 'oracle cache hash changed', 'source file changed', 'weights changed original eight fields', 'all-one weights changed original sum mean or stddev', 'absolute scale or mean invariance changed', 'exact weighted statistic differs', 'vector-zonal-weighted-stddev-gpkgs', 'Counter', 'center=weighted/mass', 'w*(v-center)**2', 'disabled flag changed original weighted fields', 'sd_weighted_stddev', 'stat_columns(name)'), 'scripts/test/vector-zonal-weighted-stddev-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()', 'validate_preview', 'legacy.validate_success', 'lineage.collect', 'lineage.validate(facts, wanted, public=True)', 'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges', 'readonly.no_publication', 'query language rewrote stored failure', 'previous complete GeoPackage changed', 'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1832', 'physical_calls != 123', 'len(cases) != 16', 'len(failures) != 106', 'exact_cause', 'Monitor current output lineage differs', 'vector_persistence.validate_item', 'include_weighted_stddev', 'flag_fraction', 'enabled_without_weights', 'collision_stddev'), 'engines/geopython-workflow/test_online_vector_zonal_weighted_stddev_fixture.py': ('test_vector_zonal_weighted_stddev_sixteen_real_gpkgs_complete_independent_oracle_and_raw_archive', 'test_vector_zonal_weighted_stddev_one_hundred_six_exact_submitted_failures_and_no_output', 'test_vector_zonal_weighted_stddev_real_physical_corruption_is_rejected', 'one-ulp', 'test_vector_zonal_weighted_stddev_gpkg_archive_never_repairs_or_overwrites', 'test_vector_zonal_weighted_stddev_worker_stdin_frozen_oracle_poison_and_cleanup', 'test_vector_zonal_weighted_stddev_independent_oracle_rejects_wrong_semantics_and_links', 'normalize-sum', 'drop-scale', 'ones-mean', 'directory-symlink', 'ones-and-original-mean', 'source-vector', 'test_vector_zonal_weighted_stddev_deviation_semantics_and_disabled_fields_cannot_weaken', 'sample-denominator', 'one-ulp-stddev', 'ones-both-stddev'), 'scripts/test/vector-zonal-weighted-stddev-persistence-online_test.py': ('test_full_matrix_16_success_106_failures_1832_checks_and_cleanup', 'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_failures_preserve_primary_and_cleanup_error', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_stddev_flag_and_three_schemas_are_exact'), 'scripts/utils/online_raster_lineage.py': ("'vector-zonal-weighted-stddev-persistence': 'vector-zonal-weighted-stddev-lineage'",), 'scripts/test/online-raster-lineage_test.py': ('test_vector_zonal_weighted_stddev_approved_three_role_gpkg_facts_preserve_internal_and_public_boundaries',), 'Makefile': ('test_online_vector_zonal_weighted_stddev_fixture.py', '@$(MAKE) test-vector-zonal-weighted-stddev-online-runner', 'scripts/test/vector-zonal-weighted-stddev-persistence-online_test.py', 'scripts/test/online-vector-zonal-weighted-stddev-fixture_test.py', 'scripts/test/online-hosted-vector-zonal-weighted-stddev-gate_test.py', 'test_vector_zonal_weighted_stddev_requires_exact_gpkg_matrix_budget_and_regressions'), 'business/scripts/online-raster-zonal-percentile-fixture.py': ('previous archive was deleted', 'previous archived bytes changed')}

def validate_vector_zonal_weighted_stddev(repository: Path, registered: set[str]) -> None:
    suite = 'vector-zonal-weighted-stddev-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  vector-zonal-weighted-stddev-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-zonal-weighted-stddev-persistence'" not in job.group('body')
        or "&& inputs.suite != 'vector-zonal-weighted-stddev-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('zonal weighted stddev requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_vector_zonal_weighted_stddev_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('zonal weighted stddev Native omitted from standard recipe')
    for relative, fragments in VECTOR_ZONAL_WEIGHTED_STDDEV_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('zonal weighted stddev strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-vector-zonal-weighted-stddev-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'weighted-center.gpkg': ('values.tif', 'zones.geojson', 'weights.tif', False, 'zonal_'), 'weighted-touched.gpkg': ('values.tif', 'zones.geojson', 'weights.tif', True, 'zonal_'), 'scaled.gpkg': ('values.tif', 'zones.geojson', 'scaled-weights.tif', False, 'zonal_'), 'ones.gpkg': ('values.tif', 'zones.geojson', 'ones-weights.tif', False, 'zonal_'), 'control-default.gpkg': ('values.tif', 'zones.geojson', 'weights.tif', False, 'zonal_'), 'control-false.gpkg': ('values.tif', 'zones.geojson', 'weights.tif', True, 'sd_'), 'control-original.gpkg': ('values.tif', 'zones.geojson', None, False, 'zonal_'), 'weights-reclassified.gpkg': ('values.tif', 'zones.geojson', 'weight-classes.tif', False, 'custom_'), 'values-reclassified.gpkg': ('values.tif', 'zones.geojson', 'weights.tif', False, 'zonal_'), 'classified.gpkg': ('values.tif', None, 'weights.tif', False, 'zonal_'), 'empty-prefix.gpkg': ('values.tif', 'zones.geojson', 'weights.tif', False, ''), 'zero.gpkg': ('values.tif', 'zones.geojson', 'zero-weights.tif', False, 'zonal_'), 'invalid-values.gpkg': ('invalid.tif', 'small.geojson', 'small-weights.tif', False, 'zonal_'), 'empty-zones.gpkg': ('even.tif', 'empty.gpkg', 'small-weights.tif', False, 'zonal_'), 'int64.gpkg': ('int64.tif', 'small.geojson', 'int64-weights.tif', False, 'zonal_'), 'uint64.gpkg': ('uint64.tif', 'small.geojson', 'uint64-weights.tif', False, 'zonal_')}
    expected_failures = ('only_raster', 'only_band', 'raster_null', 'band_null', 'both_null', 'percentile_conflict', 'negative_weight', 'nodata_weight', 'mask_weight', 'alpha_weight', 'nan_weight', 'infinite_weight', 'values_band_zero', 'values_band_high', 'values_band_boolean', 'values_band_string', 'values_alpha', 'values_complex', 'weights_band_zero', 'weights_band_high', 'weights_band_boolean', 'weights_band_string', 'weights_alpha', 'weights_complex', 'weights_size', 'weights_crs', 'weights_shifted', 'weights_rotated', 'weights_missing_crs', 'weights_missing_grid', 'weights_singular', 'different_crs', 'missing_crs', 'missing_grid', 'singular_grid', 'touched_numeric', 'touched_null', 'prefix_null', 'prefix_boolean', 'collision_stats', 'collision_mass', 'collision_sum', 'collision_mean', 'flag_null', 'flag_zero', 'flag_one', 'flag_fraction', 'flag_string_true', 'flag_string_false', 'flag_list', 'flag_object', 'enabled_without_weights', 'collision_stddev')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('zonal weighted stddev fixed 16/106 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize')):
        raise RegistrationError('zonal weighted stddev oracle must remain independent')


VECTOR_ZONAL_AREA_WEIGHTED_REQUIRED = {'scripts/test/online-hosted-vector-zonal-area-weighted-gate.sh': ('start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare', 'online-vector-zonal-area-weighted-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'business/scripts/online-vector-zonal-area-weighted-fixture.py': ('NFSExport', 'owned_root', 'WIDTH,HEIGHT=519,515', 'context.prec=2000', 'Decimal.from_float', 'Counter', 'EPSG:2222', 'EPSG:2277', 'clipped_triangle', 'analytic_coverage', 'rectangle_area', 'numerator/mass', 'abs(Fraction(grid[1])*Fraction(grid[5])-Fraction(grid[2])*Fraction(grid[4]))', 'classified_rows', 'np.array_equal', 'GDAL_TIFF_INTERNAL_MASK', 'full_gpkg_checks', 'full_source_pixels_verified', 'full_source_vectors_verified', 'runtime_workspaces_removed', 'archive_files', 'oracle cache hash changed', 'source file changed', 'area mode changed original eight fields', 'area statistics changed with all_touched', 'exact area statistic differs', 'vector-zonal-area-weighted-gpkgs', 'kept_valid_area_m2', 'stat_columns(name)'), 'scripts/test/vector-zonal-area-weighted-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()', 'validate_preview', 'legacy.validate_success', 'lineage.collect', 'lineage.validate(facts, wanted, public=True)', 'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges', 'readonly.no_publication', 'query language rewrote stored failure', 'previous complete GeoPackage changed', 'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1352', 'physical_calls != 93', 'len(cases) != 16', 'len(failures) != 76', 'exact_cause', 'Monitor current output lineage differs', 'vector_persistence.validate_item', 'flag_fraction', 'include_area_weighted', 'fixture.case_crs(name)', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', 'target_metadata_before[name] = copy.deepcopy(item)', 'vector projection CRS differs', 'collision_area_mean', 'weight_pair'), 'engines/geopython-workflow/test_online_vector_zonal_area_weighted_fixture.py': ('test_vector_zonal_area_weighted_sixteen_real_gpkgs_complete_independent_oracle_and_raw_archive', 'test_vector_zonal_area_weighted_seventy_six_exact_submitted_failures_and_no_output', 'test_vector_zonal_area_weighted_real_physical_corruption_is_rejected', 'one-ulp', 'test_vector_zonal_area_weighted_gpkg_archive_never_repairs_or_overwrites', 'test_vector_zonal_area_weighted_worker_stdin_frozen_oracle_poison_and_cleanup', 'test_vector_zonal_area_weighted_independent_oracle_rejects_wrong_semantics_and_links', 'covered-as-valid', 'unit-factor', 'ignore-fractions', 'directory-symlink', 'disabled-original-property', 'source-vector', 'test_vector_zonal_area_weighted_closed_form_oracle_does_not_call_geos_intersection_or_product'), 'scripts/test/vector-zonal-area-weighted-persistence-online_test.py': ('test_full_matrix_16_success_76_failures_1352_checks_and_cleanup', 'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_failures_preserve_primary_and_cleanup_error', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_area_flag_eleven_or_eight_stat_columns_and_exact_conflicts', 'test_preview_crs_cannot_be_missing_or_foreign', 'test_all_source_and_target_meta_snapshots_are_archived_and_changed_old_meta_fails'), 'scripts/utils/online_raster_lineage.py': ("'vector-zonal-area-weighted-persistence': 'vector-zonal-area-weighted-lineage'",), 'scripts/test/online-raster-lineage_test.py': ('test_vector_zonal_area_weighted_approved_two_role_gpkg_facts_preserve_internal_and_public_boundaries',), 'Makefile': ('test_online_vector_zonal_area_weighted_fixture.py', '@$(MAKE) test-vector-zonal-area-weighted-online-runner', 'scripts/test/vector-zonal-area-weighted-persistence-online_test.py', 'scripts/test/online-vector-zonal-area-weighted-fixture_test.py', 'scripts/test/online-hosted-vector-zonal-area-weighted-gate_test.py', 'test_vector_zonal_area_weighted_requires_exact_gpkg_matrix_budget_and_regressions'), 'business/scripts/online-raster-zonal-percentile-fixture.py': ('previous archive was deleted', 'previous archived bytes changed')}

def validate_vector_zonal_area_weighted(repository: Path, registered: set[str]) -> None:
    suite = 'vector-zonal-area-weighted-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  vector-zonal-area-weighted-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-zonal-area-weighted-persistence'" not in job.group('body')
        or "&& inputs.suite != 'vector-zonal-area-weighted-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('zonal area weighted requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_vector_zonal_area_weighted_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('zonal area weighted Native omitted from standard recipe')
    for relative, fragments in VECTOR_ZONAL_AREA_WEIGHTED_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('zonal area weighted strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-vector-zonal-area-weighted-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'area-center.gpkg': ('values.tif', 'zones.geojson', False, 'zonal_'), 'area-touched.gpkg': ('values.tif', 'zones.geojson', True, 'zonal_'), 'control-default.gpkg': ('values.tif', 'zones.geojson', False, 'zonal_'), 'control-false.gpkg': ('values.tif', 'zones.geojson', True, 'kept_'), 'foot.gpkg': ('foot.tif', 'foot.gpkg', False, 'zonal_'), 'survey.gpkg': ('survey.tif', 'survey.gpkg', False, 'zonal_'), 'rotated.gpkg': ('rotated.tif', 'rotated.geojson', False, 'zonal_'), 'reversed.gpkg': ('reversed.tif', 'reversed.geojson', False, 'zonal_'), 'values-reclassified.gpkg': ('values.tif', 'zones.geojson', False, 'custom_'), 'classified.gpkg': ('values.tif', None, False, 'zonal_'), 'empty-prefix.gpkg': ('values.tif', 'zones.geojson', False, ''), 'zero-negative.gpkg': ('zero-negative.tif', 'zones.geojson', False, 'zonal_'), 'invalid-values.gpkg': ('invalid.tif', 'small.geojson', False, 'zonal_'), 'empty-zones.gpkg': ('small.tif', 'empty.gpkg', False, 'zonal_'), 'int64.gpkg': ('int64.tif', 'small.geojson', False, 'zonal_'), 'uint64.gpkg': ('uint64.tif', 'small.geojson', False, 'zonal_')}
    expected_failures = ('flag_null', 'flag_zero', 'flag_one', 'flag_fraction', 'flag_string_true', 'flag_string_false', 'flag_list', 'flag_object', 'percentile_null', 'percentile_value', 'raster_null', 'raster_reference', 'band_null', 'band_value', 'weight_pair', 'stddev_enabled', 'values_band_zero', 'values_band_high', 'values_band_boolean', 'values_band_string', 'values_alpha', 'values_complex', 'different_crs', 'geographic', 'compound', 'missing_crs', 'missing_grid', 'singular_grid', 'collision_covered', 'collision_valid', 'collision_area_mean', 'collision_stats', 'invalid_face', 'nonpolygon', 'touched_numeric', 'touched_null', 'prefix_null', 'prefix_boolean')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('zonal area weighted fixed 16/76 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize', 'shapely.intersection', 'shapely.contains')):
        raise RegistrationError('zonal area weighted oracle must remain independent')


VECTOR_ZONAL_AREA_WEIGHTED_STDDEV_REQUIRED = {'scripts/test/online-hosted-vector-zonal-area-weighted-stddev-gate.sh': ('start_online_geopython_runtime', '--mount', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online_raster_lineage.py prepare', 'online-vector-zonal-area-weighted-stddev-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'business/scripts/online-vector-zonal-area-weighted-stddev-fixture.py': ('NFSExport', 'owned_root', 'WIDTH,HEIGHT=519,515', 'context.prec=2000', 'Decimal.from_float', 'Counter', 'EPSG:2222', 'EPSG:2277', 'clipped_triangle', 'analytic_coverage', 'rectangle_area', 'numerator/mass', 'abs(Fraction(grid[1])*Fraction(grid[5])-Fraction(grid[2])*Fraction(grid[4]))', 'classified_rows', 'np.array_equal', 'GDAL_TIFF_INTERNAL_MASK', 'full_gpkg_checks', 'full_source_pixels_verified', 'full_source_vectors_verified', 'runtime_workspaces_removed', 'archive_files', 'oracle cache hash changed', 'source file changed', 'area mode changed original eight fields', 'area statistics changed with all_touched', 'exact area statistic differs', 'vector-zonal-area-weighted-stddev-gpkgs', 'kept_valid_area_m2', 'stat_columns(name)', 'enabled_stddev', 'area_weighted_stddev', 'variance.sqrt()', 'stddev_enabled(name)', 'area standard deviation changed original eleven fields', 'sd_area_weighted_stddev'), 'scripts/test/vector-zonal-area-weighted-stddev-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'budget != 600', 'BudgetClient', 'remaining()', 'validate_preview', 'legacy.validate_success', 'lineage.collect', 'lineage.validate(facts, wanted, public=True)', 'lineage.verify_archive', 'base.validate_scan', 'raster.wait_lineage', 'source_edges', 'readonly.no_publication', 'query language rewrote stored failure', 'previous complete GeoPackage changed', 'fixture_call', 'cleanup-oracle', 'oracle cleanup also failed', 'full_checks != 1736', 'physical_calls != 117', 'len(cases) != 16', 'len(failures) != 100', 'exact_cause', 'Monitor current output lineage differs', 'vector_persistence.validate_item', 'flag_fraction', 'include_area_weighted', 'fixture.case_crs(name)', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', 'target_metadata_before[name] = copy.deepcopy(item)', 'vector projection CRS differs', 'collision_area_mean', 'weight_pair', 'include_area_weighted_stddev', 'sd_area_missing', 'sd_collision_empty'), 'engines/geopython-workflow/test_online_vector_zonal_area_weighted_stddev_fixture.py': ('test_vector_zonal_area_weighted_stddev_sixteen_real_gpkgs_complete_independent_oracle_and_raw_archive', 'test_vector_zonal_area_weighted_stddev_one_hundred_exact_submitted_failures_and_no_output', 'test_vector_zonal_area_weighted_stddev_real_physical_corruption_is_rejected', 'one-ulp', 'test_vector_zonal_area_weighted_stddev_gpkg_archive_never_repairs_or_overwrites', 'test_vector_zonal_area_weighted_stddev_worker_stdin_frozen_oracle_poison_and_cleanup', 'test_vector_zonal_area_weighted_stddev_independent_oracle_rejects_wrong_semantics_and_links', 'covered-as-valid', 'unit-factor', 'ignore-fractions', 'directory-symlink', 'disabled-original-property', 'source-vector', 'test_vector_zonal_area_weighted_stddev_closed_form_oracle_does_not_call_geos_intersection_or_product', 'test_vector_zonal_area_weighted_stddev_centered_population_semantic_mutations', "'rms'", "'unweighted'", "'constant-nonzero'"), 'scripts/test/vector-zonal-area-weighted-stddev-persistence-online_test.py': ('test_full_matrix_16_success_100_failures_1736_checks_and_cleanup', 'test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_failures_preserve_primary_and_cleanup_error', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_area_stddev_three_schemas_and_exact_conflicts', 'test_preview_crs_cannot_be_missing_or_foreign', 'test_all_source_and_target_meta_snapshots_are_archived_and_changed_old_meta_fails', 'test_fifty_failure_classes_include_strict_new_flag_pair_and_field_collisions'), 'scripts/utils/online_raster_lineage.py': ("'vector-zonal-area-weighted-stddev-persistence': 'vector-zonal-area-weighted-stddev-lineage'",), 'scripts/test/online-raster-lineage_test.py': ('test_vector_zonal_area_weighted_stddev_approved_two_role_gpkg_facts_preserve_internal_and_public_boundaries',), 'Makefile': ('test_online_vector_zonal_area_weighted_stddev_fixture.py', '@$(MAKE) test-vector-zonal-area-weighted-stddev-online-runner', 'scripts/test/vector-zonal-area-weighted-stddev-persistence-online_test.py', 'scripts/test/online-vector-zonal-area-weighted-stddev-fixture_test.py', 'scripts/test/online-hosted-vector-zonal-area-weighted-stddev-gate_test.py', 'test_vector_zonal_area_weighted_stddev_requires_exact_gpkg_matrix_budget_and_regressions'), 'business/scripts/online-raster-zonal-percentile-fixture.py': ('previous archive was deleted', 'previous archived bytes changed')}

def validate_vector_zonal_area_weighted_stddev(repository: Path, registered: set[str]) -> None:
    suite = 'vector-zonal-area-weighted-stddev-persistence'
    if suite not in registered: return
    workflow = (repository/'.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  vector-zonal-area-weighted-stddev-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-zonal-area-weighted-stddev-persistence'" not in job.group('body')
        or "&& inputs.suite != 'vector-zonal-area-weighted-stddev-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('zonal area weighted requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_vector_zonal_area_weighted_stddev_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('zonal area weighted Native omitted from standard recipe')
    for relative, fragments in VECTOR_ZONAL_AREA_WEIGHTED_STDDEV_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('zonal area weighted strict contract incomplete: '+relative)
    source = (repository/'business/scripts/online-vector-zonal-area-weighted-stddev-fixture.py').read_text()
    tree = ast.parse(source)
    actual = {key: [node.value for node in tree.body if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == key for target in node.targets)] for key in ('CASES','FAILURES')}
    expected_cases = {'area-center.gpkg': ('values.tif', 'zones.geojson', False, 'zonal_'), 'area-touched.gpkg': ('values.tif', 'zones.geojson', True, 'zonal_'), 'control-default.gpkg': ('values.tif', 'zones.geojson', False, 'zonal_'), 'control-false.gpkg': ('values.tif', 'zones.geojson', False, 'sd_'), 'foot.gpkg': ('foot.tif', 'foot.gpkg', False, 'zonal_'), 'survey.gpkg': ('survey.tif', 'survey.gpkg', False, 'zonal_'), 'rotated.gpkg': ('rotated.tif', 'rotated.geojson', False, 'zonal_'), 'reversed.gpkg': ('reversed.tif', 'reversed.geojson', False, 'zonal_'), 'values-reclassified.gpkg': ('values.tif', 'zones.geojson', False, 'custom_'), 'classified.gpkg': ('values.tif', None, False, 'zonal_'), 'empty-prefix.gpkg': ('values.tif', 'zones.geojson', False, ''), 'zero-negative.gpkg': ('zero-negative.tif', 'zones.geojson', False, 'zonal_'), 'invalid-values.gpkg': ('invalid.tif', 'small.geojson', False, 'zonal_'), 'empty-zones.gpkg': ('small.tif', 'empty.gpkg', False, 'zonal_'), 'constant.gpkg': ('constant.tif', 'small.geojson', False, 'zonal_'), 'sliver.gpkg': ('small.tif', 'sliver.geojson', False, 'zonal_')}
    expected_failures = ('flag_null', 'flag_zero', 'flag_one', 'flag_fraction', 'flag_string_true', 'flag_string_false', 'flag_list', 'flag_object', 'percentile_null', 'percentile_value', 'raster_null', 'raster_reference', 'band_null', 'band_value', 'weight_pair', 'stddev_enabled', 'values_band_zero', 'values_band_high', 'values_band_boolean', 'values_band_string', 'values_alpha', 'values_complex', 'different_crs', 'geographic', 'compound', 'missing_crs', 'missing_grid', 'singular_grid', 'collision_covered', 'collision_valid', 'collision_area_mean', 'collision_stats', 'invalid_face', 'nonpolygon', 'touched_numeric', 'touched_null', 'prefix_null', 'prefix_boolean', 'sd_flag_null', 'sd_flag_zero', 'sd_flag_one', 'sd_flag_fraction', 'sd_flag_string_true', 'sd_flag_string_false', 'sd_flag_list', 'sd_flag_object', 'sd_area_missing', 'sd_area_false', 'sd_collision_default', 'sd_collision_empty')
    if (any(len(nodes) != 1 for nodes in actual.values()) or repr(ast.literal_eval(actual['CASES'][0])) != repr(expected_cases)
        or repr(ast.literal_eval(actual['FAILURES'][0])) != repr(expected_failures)): raise RegistrationError('zonal area weighted fixed 16/100 matrix changed')
    if any(word in source for word in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'gdal.RasterizeLayer', 'gdal.Polygonize', 'shapely.intersection', 'shapely.contains')):
        raise RegistrationError('zonal area weighted oracle must remain independent')






STATISTICS_PERCENTILE_REQUIRED = {
    'scripts/test/online-hosted-raster-statistics-percentile-gate.sh': (
        'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
        'online-raster-statistics-percentile-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
    'business/scripts/online-raster-statistics-percentile-fixture.py': (
        'NFSExport', 'owned_root', 'from fractions import Fraction', 'return sorted(samples)',
        'position = (count-1)*Fraction(p)/100', 'GDAL_TIFF_INTERNAL_MASK', 'np.array_equal',
        'runtime_workspaces_removed', 'archive_sources', 'statistics-sources-manifest.json',
        'previous archived source changed', 'source file changed', "'percentile_object'"),
    'scripts/test/raster-statistics-percentile-workflow-online.py': (
        'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'raster.transient_result',
        'readonly.execution_snapshot', 'readonly.no_publication', 'readonly.graph',
        'BudgetClient', 'budget != 600', 'physical_runner(repository, env, \'oracle\', remaining())',
        'physical_runner(repository, env, \'verify\', remaining())',
        'current_execution', 'SubmittedLanguageClient', 'invalid_diagnostic',
        'query language rewrote stored failure', 'previous execution result changed',
        'scan_ids(client) != scans', 'statistics-responses-manifest.json', 'response.raw',
        'previous original response bytes changed', 'rel_tol=4e-15, abs_tol=0.',
        'all 40 calls, 41 physical checks and 504 previous-result checks are required'),
    'engines/geopython-workflow/test_online_statistics_percentile_fixture.py': (
        'test_statistics_percentile_sixteen_complete_objects_real_gdal_and_immutable_sources',
        'test_statistics_percentile_twenty_four_exact_submitted_language_failures',
        'test_statistics_percentile_full_source_and_archive_tampering_is_rejected',
        'test_statistics_percentile_object_oracle_rejects_wrong_semantics',
        'test_statistics_percentile_worker_is_repository_independent_and_full_arrays_are_rechecked',
        'test_statistics_percentile_complex_alpha_remains_separate_native_mixed_mem_evidence'),
    'Makefile': ('test_online_statistics_percentile_fixture.py', '@$(MAKE) test-raster-statistics-percentile-online-runner',
        'scripts/test/raster-statistics-percentile-workflow-online_test.py',
        'scripts/test/online-raster-statistics-percentile-fixture_test.py',
        'scripts/test/online-hosted-raster-statistics-percentile-gate_test.py',
        'test_statistics_percentile_requires_exact_readonly_matrix_budget_and_regressions'),
}


def validate_raster_statistics_percentile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-statistics-percentile-workflow'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-statistics-percentile-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-statistics-percentile-workflow'" not in job.group('body')
        or "&& inputs.suite != 'raster-statistics-percentile-workflow'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('statistics percentile requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_statistics_percentile_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('statistics percentile Native fixture omitted from standard recipe')
    for relative, fragments in STATISTICS_PERCENTILE_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('statistics percentile strict contract incomplete: '+relative)
    tree = ast.parse((repository/'business/scripts/online-raster-statistics-percentile-fixture.py').read_text())
    actual = {name: [node.value for node in tree.body if isinstance(node,ast.Assign)
                    and any(isinstance(target,ast.Name) and target.id==name for target in node.targets)]
              for name in ('CASES','FAILURES')}
    expected_cases = {'original': {'source': 'values.tif', 'band': 2}, 'p0': {'source': 'values.tif', 'band': 2, 'percentiles': [0]}, 'p100': {'source': 'values.tif', 'band': 2, 'percentiles': [100]}, 'p50-even': {'source': 'even.tif', 'band': 2, 'percentiles': [50]}, 'p50-odd': {'source': 'odd.tif', 'band': 2, 'percentiles': [50]}, 'p25': {'source': 'values.tif', 'band': 2, 'percentiles': [25]}, 'p75': {'source': 'values.tif', 'band': 2, 'percentiles': [75]}, 'p33': {'source': 'values.tif', 'band': 2, 'percentiles': [33.3]}, 'p33-reversed': {'source': 'reversed.tif', 'band': 2, 'percentiles': [33.3]}, 'reclassified': {'source': 'values.tif', 'band': 2, 'percentiles': [90], 'stage': 'reclassify'}, 'int64': {'source': 'int64.tif', 'band': 2, 'percentiles': [50]}, 'uint64': {'source': 'uint64.tif', 'band': 2, 'percentiles': [50]}, 'coverage': {'source': 'coverage.tif', 'band': 2, 'percentiles': [50]}, 'single': {'source': 'single.tif', 'band': 2, 'percentiles': [33.3]}, 'invalid': {'source': 'invalid.tif', 'band': 2, 'percentiles': [50]}, 'unlocated': {'source': 'unlocated.tif', 'band': 2, 'percentiles': [25]}}
    expected_failures = ('percentile_null', 'percentile_boolean', 'percentile_string', 'percentile_array', 'percentile_object', 'percentile_low', 'percentile_high', 'band_zero', 'band_high', 'band_boolean', 'band_string', 'complex')
    if (any(len(values)!=1 for values in actual.values()) or ast.literal_eval(actual['CASES'][0]) != expected_cases
        or ast.literal_eval(actual['FAILURES'][0]) != expected_failures):
        raise RegistrationError('statistics percentile fixed sixteen-success/twenty-four-failure matrix changed')
    if any(word in (repository/'business/scripts/online-raster-statistics-percentile-fixture.py').read_text()
           for word in ('operators.raster_compute', 'np.percentile', 'np.quantile')):
        raise RegistrationError('statistics oracle must remain independent of product/NumPy percentile')


STATISTICS_PERCENTILES_REQUIRED = {
    'scripts/test/online-hosted-raster-statistics-percentiles-gate.sh': (
        'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
        '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
        'online-raster-statistics-percentiles-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
    'business/scripts/online-raster-statistics-percentiles-fixture.py': (
        'NFSExport', 'owned_root', 'source_fixture.expected_population', 'source_fixture.expected_result',
        'source_fixture.verify', 'source_fixture.seed', 'source_fixture.archive_sources',
        'worker_source', 'types.ModuleType', 'fixture_sources_sha256', 'hashlib.sha256',
        'source file or fixture source changed', 'Worker source binding differs',
        'runtime_workspaces_removed', "'constant': True"),
    'scripts/test/raster-statistics-percentiles-workflow-online.py': (
        'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'raster.transient_result',
        'readonly.no_publication', 'readonly.graph',
        'BudgetClient', 'budget != 600', 'physical_runner(repository, env, \'oracle\', remaining())',
        'physical_runner(repository, env, \'verify\', remaining())',
        'current_execution', 'SubmittedLanguageClient', 'invalid_diagnostic',
        'query language rewrote stored failure', 'previous execution result changed',
        'scan_ids(client) != scans', 'statistics-responses-manifest.json', 'archive_response = base.encoded, base.immutable, base.archive_response',
        'previous original response bytes changed', 'current_execution, compare = base.current_execution, base.compare', 'fixture.fixture_sources()',
        'same-source singleton P50 and batch differ', 'source ordering changed the complete percentile list',
        'constant zero batch lost requested positions', 'previous_config',
        'all 56 calls, 57 physical checks and 760 previous-result checks are required'),
    'scripts/test/raster-statistics-percentile-workflow-online.py': (
        'readonly.execution_snapshot', 'response.raw', 'for actual_item, expected_item in zip(value, target)',
        'rel_tol=4e-15, abs_tol=0.', 'archive_response', 'no_publication'),
    'engines/geopython-workflow/test_online_statistics_percentiles_fixture.py': (
        'test_statistics_percentiles_sixteen_complete_objects_and_original_suite_unchanged',
        'test_statistics_percentiles_forty_exact_analysis_failures_preserve_sources',
        'test_statistics_percentiles_full_ordered_oracle_rejects_every_item_tampering',
        'test_statistics_percentiles_full_source_and_archive_tampering_fails',
        'test_statistics_percentiles_standalone_worker_binds_both_sources_and_rechecks_all_arrays'),
    'Makefile': ('test_online_statistics_percentiles_fixture.py', '@$(MAKE) test-raster-statistics-percentiles-online-runner',
        'scripts/test/raster-statistics-percentiles-workflow-online_test.py',
        'scripts/test/online-raster-statistics-percentiles-fixture_test.py',
        'scripts/test/online-hosted-raster-statistics-percentiles-gate_test.py',
        'test_statistics_percentiles_requires_exact_readonly_matrix_budget_and_regressions'),
}


def validate_raster_statistics_percentiles(repository: Path, registered: set[str]) -> None:
    suite = 'raster-statistics-percentiles-workflow'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-statistics-percentiles-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-statistics-percentiles-workflow'" not in job.group('body')
        or "&& inputs.suite != 'raster-statistics-percentiles-workflow'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body') or 'cancel-in-progress: false' not in job.group('body')
        or 'if: always()' not in job.group('body') or 'logs/' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('statistics percentile requires an independent manual Hosted NFS job')
    make = (repository/'Makefile').read_text()
    if 'test_online_statistics_percentiles_fixture.py' not in make.split('test-geopython-workflow:',1)[1].split('\n.PHONY:',1)[0]:
        raise RegistrationError('statistics percentile Native fixture omitted from standard recipe')
    for relative, fragments in STATISTICS_PERCENTILES_REQUIRED.items():
        path = repository/relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('statistics percentile strict contract incomplete: '+relative)
    tree = ast.parse((repository/'business/scripts/online-raster-statistics-percentiles-fixture.py').read_text())
    actual = {name: [node.value for node in tree.body if isinstance(node,ast.Assign)
                    and any(isinstance(target,ast.Name) and target.id==name for target in node.targets)]
              for name in ('CASES','FAILURES')}
    expected_cases = {'original': {'source': 'values.tif', 'band': 2}, 'endpoints': {'source': 'values.tif', 'band': 2, 'percentiles': [0, 100]}, 'quartiles': {'source': 'values.tif', 'band': 2, 'percentiles': [0, 25, 50, 75, 100]}, 'p50-control': {'source': 'values.tif', 'band': 2, 'percentiles': [50]}, 'fractional': {'source': 'values.tif', 'band': 2, 'percentiles': [10, 33.3, 90]}, 'reversed': {'source': 'reversed.tif', 'band': 2, 'percentiles': [10, 33.3, 90]}, 'odd': {'source': 'odd.tif', 'band': 2, 'percentiles': [0, 25, 50, 75, 100]}, 'even': {'source': 'even.tif', 'band': 2, 'percentiles': [0, 50, 100]}, 'constant': {'source': 'values.tif', 'band': 2, 'percentiles': [0, 25, 50, 75, 100], 'stage': 'reclassify', 'constant': True}, 'reclassified': {'source': 'values.tif', 'band': 2, 'percentiles': [0, 25, 50, 75, 100], 'stage': 'reclassify'}, 'int64': {'source': 'int64.tif', 'band': 2, 'percentiles': [0, 25, 50, 75, 100]}, 'uint64': {'source': 'uint64.tif', 'band': 2, 'percentiles': [0, 25, 50, 75, 100]}, 'coverage': {'source': 'coverage.tif', 'band': 2, 'percentiles': [0, 25, 50, 100]}, 'single': {'source': 'single.tif', 'band': 2, 'percentiles': [0, 33.3, 50, 100]}, 'invalid': {'source': 'invalid.tif', 'band': 2, 'percentiles': [0, 33.3, 50, 100]}, 'unlocated': {'source': 'unlocated.tif', 'band': 2, 'percentiles': [0, 25, 50, 100]}}
    expected_failures = ('list_null', 'list_boolean', 'list_number', 'list_string', 'list_object', 'list_empty', 'item_boolean', 'item_string', 'item_null', 'item_list', 'item_object', 'item_low', 'item_high', 'duplicate', 'descending', 'band_zero', 'band_high', 'band_boolean', 'band_string', 'complex')
    if (any(len(values)!=1 for values in actual.values()) or ast.literal_eval(actual['CASES'][0]) != expected_cases
        or ast.literal_eval(actual['FAILURES'][0]) != expected_failures):
        raise RegistrationError('statistics percentile fixed sixteen-success/forty-failure matrix changed')
    if any(word in (repository/'business/scripts/online-raster-statistics-percentiles-fixture.py').read_text()
           for word in ('operators.raster_compute', 'np.percentile', 'np.quantile')):
        raise RegistrationError('statistics oracle must remain independent of product/NumPy percentile')


def validate_raster_band_recommendation_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-band-recommendation-workflow'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-band-recommendation-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-band-recommendation-workflow'" not in job.group('body')
        or "&& inputs.suite != 'raster-band-recommendation-workflow'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('band-recommendation requires a manual isolated Hosted NFS job')
    required = {'scripts/test/online-hosted-raster-band-recommendation-gate.sh': ('source '
                                                                   '"$ROOT_DIR/scripts/utils/hosted-online.sh"',
                                                                   'start_online_geopython_runtime',
                                                                   '--mount',
                                                                   '--suite raster-workflow',
                                                                   '--wait-protection-owners develop,manager',
                                                                   'online-raster-band-recommendation-fixture.py '
                                                                   'stop',
                                                                   'unset '
                                                                   'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
                                                                   'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
 'scripts/test/raster-band-recommendation-workflow-online.py': ('raster.validate_identity',
                                                                'raster.EDITOR_PERMISSIONS',
                                                                'raster.transient_result',
                                                                'execution_snapshot',
                                                                'no_publication',
                                                                'actor',
                                                                'scan_ids(client) != baseline_scans',
                                                                'complete_object_verified'),
 'business/scripts/online-raster-band-recommendation-fixture.py': ('NFSExport',
                                                                   'owned_root',
                                                                   'expected_result',
                                                                   'np.corrcoef',
                                                                   'np.ascontiguousarray',
                                                                   'no_outputs',
                                                                   'runtime_workspaces_removed',
                                                                   'full_pixels_and_mask_verified'),
 'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
 'engines/geopython-workflow/test_online_band_recommendation_fixture.py': ('test_band_recommendation_fixture_complete_sources_and_full_product_object',
                                                                           'test_band_recommendation_fixture_object_comparison_is_complete_and_strict'),
 'Makefile': ('@$(MAKE) test-raster-band-recommendation-online-runner',
              'test_online_band_recommendation_fixture.py',
              'scripts/test/raster-band-recommendation-workflow-online_test.py',
              'scripts/test/online-raster-band-recommendation-fixture_test.py',
              'scripts/test/online-hosted-raster-band-recommendation-gate_test.py',
              'test_band_recommendation_requires_manual_nfs_readonly_oif_and_regressions')}
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'band-recommendation persistence contract is incomplete: {relative}')


def validate_raster_nibble_capacity_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-nibble-capacity'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-nibble-capacity-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-nibble-capacity'" not in job.group('body')
        or "&& inputs.suite != 'raster-nibble-capacity'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or 'timeout-minutes: 240' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('nibble capacity requires an independent manual Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-nibble-capacity-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite raster-nibble-capacity',
            '--wait-protection-owners develop,manager', '--cpus 4 --memory 2g --memory-swap 2g',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=10800', 'TMPDIR=', '--start-runtime',
            'online-raster-nibble-capacity-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
        'scripts/test/raster-nibble-capacity-online.py': (
            'raster.EDITOR_PERMISSIONS', 'capacity.cgroup_directory', 'capacity.validate_measurement',
            'fixture.require_disk', 'validate_disk', 'raster.strict_sizes', 'raster.wait_lineage',
            'triggered_by', 'scan_ids(client)', 'policy_restored', 'previous_artifacts_unchanged',
            'private_allocated_peak_bytes', 'runtime_container_id'),
        'system/backend/cmd/online-test-fixture/main.go': (
            'case "raster-workflow", "raster-nibble-capacity", "raster-slope-orthogonal-persistence", "raster-sight-orthogonal-persistence":',
            'suite == "raster-workflow-capacity" || suite == "raster-nibble-capacity"'),
        'system/backend/cmd/online-test-fixture/main_test.go': (
            'TestCapacityFixtureSessionLastsBeyondTheThreeHourBudget',
            'TestNibbleCapacityRetainsExactlyTheOrdinaryRasterPermissions'),
        'business/scripts/online-raster-nibble-capacity-fixture.py': (
            'NFSExport', 'SIZES = (4096, 10980)', "SPACING = {'sparse': 128, 'dense': 2}",
            'expected_block', 'running*size**2*64', 'full_check=True',
            'capacity complete pixel values differ', 'capacity complete mask differs',
            'previous capacity artifact changed', 'capacity source file changed'),
        'engines/geopython-workflow/test_online_nibble_capacity_fixture.py': (
            'test_capacity_analytical_oracle_matches_original_donor_bruteforce',
            'test_nibble_capacity_real_gdal_complete_pixels_and_previous_immutable',
            'test_nibble_capacity_oracle_rejects_corrupt_or_previous_output'),
        'Makefile': ('@$(MAKE) test-raster-nibble-capacity-online-runner',
            'test_online_nibble_capacity_fixture.py', 'scripts/test/raster-nibble-capacity-online_test.py',
            'scripts/test/online-raster-nibble-capacity-fixture_test.py',
            'scripts/test/online-hosted-raster-nibble-capacity-gate_test.py',
            'test_nibble_capacity_requires_manual_limits_disk_and_full_oracle')}
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'nibble capacity contract incomplete: {relative}')


def validate_raster_disk_pressure_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-disk-pressure'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-disk-pressure-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "inputs.suite == 'raster-disk-pressure'" not in job.group('body')
        or "inputs.suite != 'raster-disk-pressure'" not in workflow
        or 'nfs-kernel-server nfs-common e2fsprogs util-linux' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('disk pressure requires manual isolated Hosted Linux ext4/NFS')
    required = {
        'scripts/test/online-hosted-raster-disk-pressure-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime',
            '--cpus 4 --memory 2g --memory-swap 2g', 'TMPDIR=/raster-pressure-tmp',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-disk-pressure-fixture.py stop'),
        'business/scripts/online-raster-disk-pressure-fixture.py': (
            'NFSExport', 'posix_fallocate', 'mkfs.ext4', 'nodiscard', 'losetup', 'findmnt', 'umount',
            'refusing foreign loop cleanup', 'loop_detached', 'image_removed',
            'np.array_equal(band.ReadAsArray(), expected, equal_nan=True)', 'full_check=True'),
        'scripts/test/raster-disk-pressure-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'verify_failure',
            'SubmittedLanguageClient', 'Accept-Language', 'capacity.validate_scan', 'raster.strict_sizes',
            'raster.wait_lineage', 'source_edges(client', 'source metadata changed',
            'product Runtime or master/worker restarted', 'pending_bytes', 'safety_bytes', 'workflow_sha256',
            'exact rejected workflow/target'),
        'engines/geopython-workflow/test_online_disk_pressure_fixture.py': (
            'test_pressure_native_exact_phase_rejection_and_same_process_recovery',
            'test_pressure_oracle_rejects_corrupt_or_extra_publications'),
        'Makefile': ('test_online_disk_pressure_fixture.py', '@$(MAKE) test-raster-disk-pressure-online-runner',
            'scripts/test/raster-disk-pressure-online_test.py', 'scripts/test/online-raster-disk-pressure-fixture_test.py',
            'scripts/test/online-hosted-raster-disk-pressure-gate_test.py')}
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('disk pressure strict contract incomplete: '+relative)


def validate_raster_nibble_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-nibble-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-nibble-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-nibble-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-nibble-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('nibble requires a manual isolated Hosted NFS job')
    required = {'scripts/test/online-hosted-raster-nibble-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
                                                      'start_online_geopython_runtime',
                                                      '--mount',
                                                      '--suite raster-workflow',
                                                      '--wait-protection-owners develop,manager',
                                                      'online-raster-nibble-fixture.py stop',
                                                      'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
                                                      'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
 'scripts/test/raster-nibble-persistence-online.py': ('raster.validate_identity',
                                                      'raster.EDITOR_PERMISSIONS',
                                                      'fixture.SOURCES',
                                                      'triggered_by',
                                                      'raster.strict_sizes',
                                                      'raster.validate_target_item',
                                                      'raster.wait_lineage',
                                                      'scan_ids(client)!=before_scans',
                                                      'rasterize.check_physical(native,previous)',
                                                      "for problem in ('band','unsafe','no-donor')",
                                                      'full_pixel_and_mask_verification',
                                                      'exact_integer_pixels_verified'),
 'business/scripts/online-raster-nibble-fixture.py': ('NFSExport',
                                                      'owned_root',
                                                      'expected_pixels',
                                                      'squared<best',
                                                      'np.array_equal(actual,expected,equal_nan=True)',
                                                      'GDAL_TIFF_INTERNAL_MASK',
                                                      'runtime_workspaces_removed',
                                                      'source file changed',
                                                      'full_check=True'),
 'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
 'engines/geopython-workflow/test_online_nibble_fixture.py': ('test_nibble_persistence_real_gdal_three_cog_chains_and_complete_pixels',
                                                              'test_nibble_persistence_oracle_rejects_corruption'),
 'Makefile': ('@$(MAKE) test-raster-nibble-online-runner',
              'test_online_nibble_fixture.py',
              'scripts/test/raster-nibble-persistence-online_test.py',
              'scripts/test/online-raster-nibble-fixture_test.py',
              'scripts/test/online-hosted-raster-nibble-gate_test.py',
              'test_nibble_requires_manual_nfs_exact_donors_and_regressions')}
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'nibble persistence contract is incomplete: {relative}')


def validate_raster_focal_statistics_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-statistics-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-focal-statistics-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-focal-statistics-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-focal-statistics-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal statistics requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-focal-statistics-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-focal-statistics-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-focal-statistics-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'triggered_by',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', "for problem in ('band','window')", 'full_pixel_and_mask_verification'),
        'business/scripts/online-raster-focal-statistics-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'sliding_window_view', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_raster_fixture.py': (
            'test_focal_persistence_real_gdal_six_cog_chains_and_complete_pixels',
            'test_focal_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-focal-statistics-online-runner',
            'scripts/test/raster-focal-statistics-persistence-online_test.py',
            'scripts/test/online-raster-focal-statistics-fixture_test.py',
            'scripts/test/online-hosted-raster-focal-statistics-gate_test.py',
            'test_focal_requires_manual_nfs_identity_strict_sizes_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'focal statistics persistence contract is incomplete: {relative}')


def validate_raster_zonal_statistics_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-zonal-statistics-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-zonal-statistics-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-zonal-statistics-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-zonal-statistics-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('zonal statistics requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-zonal-statistics-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-zonal-statistics-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-zonal-statistics-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'triggered_by',
            'validate_preview', 'validate_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', "for problem in ('band','grid')"),
        'business/scripts/online-raster-zonal-statistics-fixture.py': (
            'NFSExport', 'owned_root', 'expected_rows', 'validate_records', 'csv.DictReader',
            'GDAL_TIFF_INTERNAL_MASK', 'runtime_workspaces_removed', 'source file changed'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_raster_fixture.py': (
            'test_zonal_persistence_real_gdal_four_csv_chains_and_independent_full_tables',
            'test_zonal_persistence_oracle_rejects_csv_corruption'),
        'Makefile': ('@$(MAKE) test-raster-zonal-statistics-online-runner',
            'scripts/test/raster-zonal-statistics-persistence-online_test.py',
            'scripts/test/online-raster-zonal-statistics-fixture_test.py',
            'scripts/test/online-hosted-raster-zonal-statistics-gate_test.py',
            'test_zonal_requires_manual_nfs_identity_complete_csv_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'zonal statistics persistence contract is incomplete: {relative}')


def validate_raster_capacity_profile(repository: Path, registered: set[str]) -> None:
    if "raster-workflow-capacity" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  raster-capacity-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "runs-on: ubuntu-24.04" not in job.group("body") or (
        "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-workflow-capacity'\n" not in job.group("body")
    ) or "&& inputs.suite != 'raster-workflow-capacity'" not in workflow:
        raise RegistrationError("raster capacity requires its own manual Ubuntu Hosted job")
    if load_suite_registry(repository)["raster-workflow-capacity"].nightly:
        raise RegistrationError("raster capacity requires real evidence before schedule registration")
    required = {
        "scripts/test/raster-workflow-online.py": (
            "def strict_sizes(", "type(value) is not int", "len(set(sizes)) != 1", "size_evidence",
        ),
        "scripts/test/online-hosted-raster-capacity-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite raster-workflow-capacity',
            '--cpus 4 --memory 2g --memory-swap 2g', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=10800',
            'online-raster-capacity-fixture.py stop', 'online-raster-capacity-fixture.py seed',
            'scripts/test/online-engine-registration.py', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'env -u POSTGRES_PASSWORD make test-online', 'launch_online_geopython_runtime',
            'for start_target in -security -develop -manager -monitor;', 'export SECURITY_URL=',
        ),
        "scripts/utils/hosted-product-runtime.sh": (
            'com.addp.online-run-id', 'Runtime default entry mismatch', 'Runtime image identity mismatch',
        ),
        "scripts/test/raster-workflow-capacity-online.py": (
            'CACHE_VALUES=(64,128,256)', 'REPETITIONS=2', 'validate_measurement', 'wait_applied',
            'def run_directories(', 'def directory_matrix(', 'manager.derived_artifact.create',
            'class CapacityMetadataGrants:', 'class MosaicScopeFixture:', 'def verify_basic_rescans(',
            "layouts=('single','multi')", 'self.grants.metadata_facts(',
            'on_evidence=self.capture_facts', 'capacity-failure.json', 'def capacity_error_message(',
            'authorization.cleanup()', 'expires_at=self.expires_at', 'def produced_item(',
            'def execution(self, observation, identity, module)', 'BEGIN READ ONLY;',
            'class SnapshotQueryError(', 'def capacity_error_evidence(', 'capacity-snapshots.json',
            'VERBOSITY=sqlstate', 'stdout_sha256', 'stdout_rows', 'json_type',
            "operation='execution'", "operation='produced_item'",
            's.parent_execution_id=e.execution_id', 'basic_rescans=8',
            'manager.derived_artifact.read', 'mosaic_groups', 'submitted_tasks=1', 'automatic_scans=28',
            "physical('verify-mosaic',[name])", "physical('purge-mosaic',[name])",
            "validate_monitor_lineage(monitor,facts)",
            'from scripts.utils import capacity_resources', 'capacity_resources.cgroup_directory',
            'capacity_resources.read_metrics', 'scene.strict_sizes(artifact,actual,item)',
            "physical('purge',names)", 'capacity-progress.json', 'wait_lineage', "'runtime_container_id'",
        ),
        "scripts/utils/capacity_resources.py": (
            'def cgroup_directory(', 'def read_metrics(', 'memory.peak', 'oom_kill',
            'memory.swap.max', 'quota/period!=4', 'maximum!=2*(1<<30)',
        ),
        "business/scripts/online-raster-capacity-fixture.py": (
            'S2A_17SPV_20220304_0_L2A', 'SIZE = 10980', 'data_directory=root/',
            'public asset checksum changed', 'full_check=True', 'expected_rows',
            'capacity source object bytes changed', 'capacity target purge has residuals',
            'def verify_mosaic_directory(', 'def verify_mosaic_raster(', 'def mosaic_names_checked(',
            'capacity mosaic exact seven-object inventory differs', 'capacity mosaic source bytes changed',
        ),
        "system/backend/cmd/online-test-fixture/main.go": (
            'case "raster-workflow-capacity":',
            'return append(append([]string{}, rasterWorkflowPermissions...), "manager.derived_artifact.create", "manager.derived_artifact.read"), nil',
        ),
        "engines/geopython-workflow/test_online_raster_fixture.py": (
            'test_capacity_directory_all_bands_global_base_grid_and_full_pyramids',
            'test_capacity_directory_oracle_rejects_corrupt_components_and_scope',
        ),
        "scripts/test/online-engine-registration.py": (
            "class ExactFileMetadataGrants(_ExactGrantLifecycle):", "class _ExactGrantLifecycle:", "expires_at=None", "remaining <= 10800",
            'class MetadataGrantError(', 'def metadata_facts_evidence(', 'def metadata_error_evidence(',
            'def metadata_facts(', 'layout_not_allowed', 'basis_not_complete',
            "body.update(expiry_mode='at_time', expires_at=expires_at)",
        ),
        "scripts/test/raster-workflow-capacity-online_test.py": (
            'test_only_fixed_precise_sources_and_results_gain_expiring_metadata',
            'test_capacity_grants_observed_multi_tiff_and_archives_authorization_facts',
            'test_second_fact_read_detects_layout_drift_without_granting_or_rewriting',
            'test_transport_and_unknown_write_errors_are_safe_and_cleanup_failure_is_separate',
            'test_cleanup_runs_for_success_scope_rejection_and_failed_business',
            'test_private_snapshots_bind_public_user_parent_scan_item_and_preserve_public_projection',
            'test_sqlstate_response_shapes_and_sensitive_output_are_classified',
            'test_snapshot_failure_is_retained_before_cleanup_and_cli_uses_same_stage',
            'test_execution_query_runs_on_real_postgres', 'test_produced_item_query_runs_on_real_postgres',
            'test_whole_state_and_seed_queries_run_on_real_postgres',
        ),
        "scripts/test/common-postgres-gate.sh": (
            'online-capacity-snapshots', 'ADDP_CAPACITY_SNAPSHOT_POSTGRES_DSN="$execution_dsn"',
            'scripts.test.raster-workflow-capacity-online_test --postgres',
            'scripts/test/raster-workflow-capacity-online.py scripts/test/raster-workflow-capacity-online_test.py',
        ),
        "Makefile": ('scripts/test/raster-workflow-capacity-online_test.py',
                     'scripts/test/online-raster-capacity-fixture_test.py',
                     'scripts/test/online-hosted-raster-capacity-gate_test.py'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f"raster capacity owner contract is incomplete: {relative}")


def validate_sql_pool_profile(repository: Path, registry: dict[str, object]) -> None:
    suite = "sql-pool-consumer-flow"
    if suite not in registry:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  sql-pool-hosted-t4:\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    required = (
        f"if: github.event_name == 'workflow_dispatch' && inputs.suite == '{suite}'",
        "runs-on: ubuntu-24.04", "online-hosted-sql-pool-gate.sh --check-only",
        "run: bash scripts/test/online-hosted-sql-pool-gate.sh", "git merge-base --is-ancestor",
    )
    if (job is None or any(fragment not in job[1] for fragment in required)
            or f"&& inputs.suite != '{suite}'" not in workflow or registry[suite].nightly):
        raise RegistrationError("SQL pool requires its fixed-source manual Hosted job")
    if not make_target_commands((repository / "Makefile").read_text(), "test-sql-pool-online-runner"):
        raise RegistrationError("SQL pool deterministic gate is missing")
    lifecycle = (repository / "scripts/test/online-hosted-sql-pool-gate.sh").read_text()
    if "for target in -system -security -meta -manager -develop; do" not in lifecycle:
        raise RegistrationError("SQL pool page acceptance requires the System frontend lifecycle")


def validate_hdfs_spark_profile(repository: Path, registered: set[str]) -> None:
    if "hdfs-spark-consumer-flow" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  hdfs-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "runs-on: ubuntu-24.04" not in job.group("body") or (
        "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'hdfs-spark-consumer-flow'\n" not in job.group("body")
    ) or "actions/setup-java@" not in job.group("body") or "java-version: '11'" not in job.group("body"):
        raise RegistrationError("HDFS requires a manual Ubuntu Hosted job with Java 11")
    if "&& inputs.suite != 'hdfs-spark-consumer-flow'" not in workflow:
        raise RegistrationError("HDFS must not dispatch on self-hosted")
    if '      - name: Verify HDFS owner runner contract\n        run: make test-hdfs-online-runner\n' not in job.group("body"):
        raise RegistrationError("HDFS Hosted must pass the same owner runner contract before deployment")
    suite = load_suite_registry(repository)["hdfs-spark-consumer-flow"]
    if suite.nightly:
        raise RegistrationError("HDFS requires real evidence before schedule registration")
    if ("security", "SECURITY_URL") not in suite.services:
        raise RegistrationError("HDFS requires Security in the same-source service preflight")
    required = {
        "scripts/test/online-hosted-hdfs-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite hdfs-spark-consumer-flow',
            'start_online_spark_runtime online-hdfs hdfs', 'RUNTIME_IMAGE', 'RUNTIME_CONTAINER',
            'scripts/test/online-engine-registration.py',
            '-security', '--wait-protection-owners develop,manager', 'SECURITY_URL=http://127.0.0.1:8194',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'SPARK_MODE override is forbidden',
            'run_logged make test-online', 'online-hdfs-spark-fixture.sh stop', 'ADDP_ONLINE_MINIO_ENGINE_ID',
            'ADDP_ONLINE_MYSQL_ENGINE_ID', 'ADDP_ONLINE_DORIS_ENGINE_ID', 'ADDP_ONLINE_TEST_RUN_ID',
        ),
        "scripts/utils/hosted-online.sh": (
            "make build-images", 'source "$ROOT_DIR/scripts/utils/hosted-product-runtime.sh"',
            "${RUNTIME_REPORT}-runtime.log",
            "com.addp.online-runtime", "stop_online_product_runtime || status=1",
        ),
        "scripts/utils/hosted-product-runtime.sh": (
            "Runtime image identity mismatch", "Runtime default entry mismatch", "${RUNTIME_REPORT}-runtime-build.txt",
        ),
        "business/scripts/online-hdfs-spark-fixture.sh": (
            'com.addp.online-fixture', '--network host', '--tmpfs /data', '/addp/hdfs/init.py',
            'refusing to delete a foreign container', 'fixture containers remain', 'addp-hdfs-online-minio', 'minio-engine.json',
            'addp-hdfs-online-mysql', 'addp-hdfs-online-doris', 'addp-hdfs-online-master-b', 'addp-hdfs-online-worker-b', '18084', 'spark-b-engine.json', 'common-spark-table-gate.sh', '/addp/doris/init.sh',
        ),
        "scripts/test/spark-online-evidence.py": (
            'Finished task', 'ADDP-Workflow-Engine-', 'com.addp.online-fixture',
            '/api/executions/', 'range(8)', 'all_results',
        ),
        "scripts/test/hdfs-spark-consumer-flow-online.py": (
            '/api/v1/develop/executions', 'spark_cluster_id', 'SPARK.worker_evidence', 'SPARK.runtime_status_evidence',
            'e2e/online/hdfs-spark-consumer-flow.spec.js', 'evidence != expected', 'minio_physical', 'minio_second',
            'TABLES.prepare', 'TABLES.run', 'TABLES.cleanup', "expected['tables']", 'run_multicluster_browser', 'validate_multicluster_receipt', 'spark-dual-physical.json',
        ),
        "scripts/test/spark-table-consumer-online.py": (
            'physical_before', 'physical_after', 'validate_fields', 'validate_rows', 'runtime_status_evidence',
            'meta_scan_runs', 'spark-table-cleanup.json', 'table domain cleanup unverified',
        ),
        "business/scripts/online-spark-table-oracle.py": (
            'physical oracle refuses a foreign container', 'INNODB_TABLES', "SHOW PROC '/dbs'", 'target_absent',
            'staging evidence', 'ddl_sha256', 'MYSQL_PWD',
        ),
        "console/frontend/e2e/online/hdfs-spark-consumer-flow.spec.js": (
            'login(', 'auth.principalID', 'meta/scan/run/manual', '.workflow-final-result-json',
            "getByRole('dialog'", 'Confirm and start scan', 'postDataJSON()', "catalog_paths: ['samples']",
            'The selected scope has been scanned',
            'facts.physical_after.rows', "['created', 'overwrite', 'reuse']", "getAttribute('class')",
        ),
        "console/frontend/e2e/online/spark-multicluster.spec.js": (
            'login(', 'query-preflight', 'postDataJSON()', 'source_task_id', 'task-definitions',
            "batch('workflow', 'failure-isolation'", "batch('query', 'failure-isolation'", 'overlap',
            'before:', 'applications', 'ADDP_ONLINE_SPARK_DUAL_BROWSER_REPORT',
        ),
        "Makefile": ('$(MAKE) test-hdfs-online-runner', 'scripts/test/hdfs-spark-consumer-flow-online_test.py',
                     'scripts/test/online-hdfs-spark-fixture_test.py', 'scripts/test/online-hosted-hdfs-gate_test.py',
                     'scripts/test/spark-table-consumer-online_test.py', 'console/frontend/e2e/online/spark-multicluster.spec.js'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f"HDFS owner contract is incomplete: {relative}")


def validate_elasticsearch_spark_profile(repository: Path, registered: set[str]) -> None:
    if 'elasticsearch-consumer-flow' not in registered:
        return
    required = {
        'scripts/test/online-hosted-elasticsearch-gate.sh': (
            'start_online_spark_runtime online-elasticsearch elasticsearch', 'ADDP_ONLINE_SPARK_ENGINE_ID', 'SPARK_MODE override is forbidden',
            'start_online_spark_image_registry', 'stop_online_spark_image_registry',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
        ),
        'scripts/utils/hosted-online.sh': (
            'make build-images', '--services spark-workflow-engine --tag $tag --verify --jobs 1',
            '--network host', '-e WORKFLOW_BIND_HOST=127.0.0.1',
            'source "$ROOT_DIR/scripts/utils/hosted-product-runtime.sh"', '${RUNTIME_REPORT}-runtime.log',
            'com.addp.online-runtime', 'stop_online_product_runtime || status=1',
        ),
        'scripts/utils/hosted-product-runtime.sh': (
            'Runtime image identity mismatch', 'Runtime default entry mismatch', '${RUNTIME_REPORT}-runtime-build.txt',
        ),
        'scripts/test/elasticsearch-consumer-flow-online.py': (
            'SPARK.worker_evidence', 'SPARK.runtime_status_evidence', 'spark_cluster_id', 'validate_workflow_nodes',
        ),
        'scripts/test/spark-online-evidence.py': ('Finished task', 'range(8)', 'all_results'),
        'console/frontend/e2e/online/elasticsearch-consumer-flow.spec.js': (
            '.workflow-final-result-json', 'expected.spark.execution_id', 'spark_workflow_result',
        ),
        'Makefile': ('$(MAKE) test-elasticsearch-online-runner', 'scripts/test/elasticsearch-consumer-flow-online_test.py',
                     'scripts/test/online-hosted-elasticsearch-gate_test.py', 'scripts/test/online-elasticsearch-consumer-fixture_test.py'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f'ES Spark owner contract is incomplete: {relative}')


def validate_public_origin_browser_profile(repository: Path, registered: set[str]) -> None:
    if "compose-public-origin" not in registered:
        return
    required = {
        "scripts/test/online-hosted-public-origin-gate.sh": (
            "npm --prefix console/frontend ci",
            "playwright install --with-deps chromium",
            "up -d --no-deps --wait security-backend",
            "up -d --no-deps --wait workbench-backend asset-backend",
            "up -d --no-deps --wait portal-backend",
            "up -d --no-deps --wait workbench-frontend portal-frontend",
            "protection_owners=manager,transfer",
            '--wait-protection-owners "$2"',
            "online-metric-postgres-fixture.sh start",
            "online-metric-postgres-fixture.sh stop",
            'run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        ),
        "scripts/test/compose-public-origin-online.py": (
            "e2e/online/compose-public-origin.spec.js", "--config=playwright.online.config.js", "browser = run_browser()",
            "login_reports", "cross_module_session_reused", "browser = run_published_workbench_browser()",
        ),
        "console/frontend/e2e/online/compose-public-origin.spec.js": (
            "ADDP_ONLINE_READ_USER_PASSWORD", "action: 'hold-lock'", "action: 'queue-refresh'",
            "requests.length - initialRefreshes", "ADDP_ONLINE_PUBLIC_ORIGIN_BROWSER_REPORT",
            "auth/mfa-verifications", "auth/context-selections", "ADDP_ONLINE_PUBLIC_MFA_TOTP", "cross_module_session_reused",
            "workbench_published_revision", "workbench_table_values",
        ),
        "system/backend/cmd/online-test-fixture/main.go": (
            'values["ADDP_ONLINE_READ_USER_USERNAME"]', 'values["ADDP_ONLINE_READ_USER_PASSWORD"]',
            'values["ADDP_ONLINE_PUBLIC_MFA_TOTP"]', 'values["ADDP_ONLINE_PUBLIC_MFA_ALTERNATE_MEMBERSHIP_ID"]',
        ),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f"compose-public-origin browser contract is incomplete: {relative}")


def validate_pg_authorization_profile(repository: Path, registered: set[str]) -> None:
    if "pg-source-authorization" not in registered:
        return
    suite = load_suite_registry(repository)["pg-source-authorization"]
    if suite.nightly or {name for name, _ in suite.services} != {"gateway", "system", "meta", "security", "manager", "catalog", "transfer"}:
        raise RegistrationError("PG authorization requires its owners including Security and manual first acceptance")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  pg-authorization-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "runs-on: ubuntu-24.04" not in job.group("body") or (
        "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'pg-source-authorization'\n" not in job.group("body")
    ) or "&& inputs.suite != 'pg-source-authorization'" not in workflow:
        raise RegistrationError("PG authorization must use its single manual Hosted job")
    if '      pg_source_commit:\n' not in workflow or any(not source_contract_contains(job.group('body'), fragment, repository) for fragment in (
        'name: Validate requested PG source commit', 'name: Verify PG source ancestry',
        'PG_SOURCE_COMMIT: ${{ inputs.pg_source_commit }}', 'PG_DISPATCH_COMMIT: ${{ github.sha }}',
        '^[0-9a-f]{40}$', 'ref: ${{ inputs.pg_source_commit || github.sha }}',
        '[ "$(git rev-parse HEAD)" = "$selected" ]',
        'git merge-base --is-ancestor "$selected" "$PG_DISPATCH_COMMIT"',
    )):
        raise RegistrationError("PG fixed source must validate exact SHA, checkout and dispatch ancestry")
    if '      pg_cpu_comparison:\n' not in workflow or any(not source_contract_contains(job.group('body'), fragment, repository) for fragment in (
        "if: inputs.pg_cpu_comparison",
        "ADDP_ONLINE_PG_CPU_ARM: ${{ inputs.pg_cpu_comparison && '4' || '' }}",
        "ADDP_ONLINE_PG_CPU_ARM: '3'",
        'git checkout --detach "$selected"',
        'ADDP_ONLINE_ARTIFACT_DIR: ${{ runner.temp }}/addp-online-${{ github.run_id }}-cpu3',
        'ADDP_ONLINE_SECRET_DIR: ${{ runner.temp }}/addp-online-secret-${{ github.run_id }}-cpu3',
        '${{ runner.temp }}/addp-online-${{ github.run_id }}-cpu3/',
        '${{ runner.temp }}/addp-pg-cpu3-${{ github.run_id }}/logs/',
    )):
        raise RegistrationError("PG CPU comparison must pair the fixed source and archive the isolated three-vCPU arm")
    required = {
        "scripts/test/online-hosted-pg-authorization-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite pg-source-authorization',
            'playwright install --with-deps chromium', 'export CONSOLE_URL=',
            'export CATALOG_URL=', 'export CATALOG_SERVICE_CLIENT_SECRET=', 'export SECURITY_URL=',
            'for start_target in -system -gateway', '--exact-process "$start_target"',
            'for start_target in -meta -security -transfer -manager -catalog -system',
            'export ADDP_ONLINE_PG_SOURCE_PREFIX=', 'PGAPPNAME=',
            'export TRANSFER_SERVICE_CLIENT_SECRET=', 'export META_SERVICE_CLIENT_SECRET=',
            'online-metric-postgres-fixture.sh stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
        "scripts/test/pg-source-authorization-online.py": (
            "'scan_depth': 'basic'", "'force': True", 'lifecycle.cleanup()', 'self.expiry_grant',
            "state='expired'", "self.reader.token == reader_before", 'with BrowserObservation(', 'browser.finish()',
            'pg-source-authorization.spec.js', 'self.meta_recovery_flow(', "with DependencyOutage('meta')",
            'original_grant_recovers_without_rescan', "'authorization_service_unavailable'",
            'lifecycle.catalog_flow(', 'catalog_confirmation_alone_denied',
            'catalog_replay_does_not_restore_revoked_grant', "with DependencyOutage('catalog')",
            'catalog_offline_explicit_direct_grant_and_revocation', 'catalog_recovery_preserves_original_rules',
            'self.content_change_flow(', 'lifecycle.node_scope_flow(', 'node_future_item_covered_without_leaf_grant',
            'mutate_source_content(', 'existing_grant_reads_reset_content',
            'content_change_and_replay_do_not_restore_withdrawn_grant',
            'lifecycle.workspace_flow(', 'with WorkspaceObservation(', 'pg-authorization-workspace.json',
            'browser withdrawal did not preserve its exact Grant history',
            'lifecycle.production_flow(', "'auto_scan_metadata': False", 'self.collect_production_grants()',
            'new_pg_output_initial_grant_issued', 'output_survives_source_withdrawal',
            'output_withdrawal_denies_read', 'def source_connection_summary(',
            "environment['PGAPPNAME'] = source_application_name(self.module)",
            "evidence['connection_observation'] = barrier.connection_observation(",
            "'pg_source_private/statements.json'"),
        "business/scripts/online-metric-postgres-fixture.sh": (
            'content-change|content-reset)', 'pg-source-authorization', 'container ownership mismatch',
            'LOCK TABLE public.metric_people', 'pg-content:changed', 'pg-content:baseline',
            'log_directory=pg_source_private', 'log_destination=jsonlog',
            'ALTER ROLE metric_reader SET log_min_duration_statement = 0;'),
        "console/frontend/e2e/online/pg-source-authorization.spec.js": (
            'sessionSnapshot', 'same_iframe_without_relogin', 'denied_preview_clears_old_rows',
            'isAnonymousRefreshResponse', 'beforeCookie.value !== afterCookie.value',
            'exclusive_workspace_tabs', 'integrated_create_picker_without_write',
            "getByTestId('approval-resource-tree')",
            'table_text_and_tree_filters', 'recipient_type_and_account_filters',
            'browser_revocation_updates_current_and_history', 'browser_revocation_denies_manager_read',
            'expect(writes).toEqual([base +', 'reader.fingerprint'),
        "Makefile": ('test-pg-authorization-online-runner', 'pg-source-authorization-online_test.py',
                     'online-hosted-pg-authorization-gate_test.py', '@$(MAKE) test-pg-authorization-online-runner',
                     'scripts/test/online-hosted-pg-authorization-gate.sh business/scripts/online-metric-postgres-fixture.sh',
                     '@node --check --input-type=module < console/frontend/e2e/online/pg-source-authorization.spec.js'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError(f"PG authorization owner contract is incomplete: {relative}")


def validate_highgo_profile(repository: Path, registered: set[str]) -> None:
    if "highgo-consumer-flow" not in registered:
        return
    requirements = {
        "scripts/test/online-hosted-highgo-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
            'business/scripts/online-highgo-consumer-fixture.py start',
            'business/scripts/online-highgo-consumer-fixture.py verify',
            'business/scripts/online-highgo-consumer-fixture.py stop',
            '--suite highgo-consumer-flow', 'scripts/test/online-engine-registration.py',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        ),
        "business/scripts/online-highgo-consumer-fixture.py": (
            "'GITHUB_ACTIONS': 'true'", "'ADDP_ONLINE_HOSTED': '1'",
            "highgo_image()", "/addp/highgo/entrypoint.sh", "business/highgo/init/init.sql",
            "'127.0.0.1::5866'", "'com.addp.online-fixture'", "'rm', '-fv'",
            "'volume', 'inspect'", "source == target", "'SENTINEL'",
        ),
        "scripts/test/highgo-consumer-flow-online.py": (
            'initialize_exact_record_read_grants', 'API.validate_user_identity',
            "actual == PERMISSIONS", "'replace_sentinel', 'repeat_replace'",
            'FLOW.assert_transfer_counts', 'SUPPORT.cleanup_tasks', 'run_browser(report)',
        ),
        "console/frontend/e2e/online/highgo-consumer-flow.spec.js": (
            "frameLocator('iframe[data-testid=\"module-iframe\"]')",
            'current.principalID', 'attrs.capabilities.constraints.constraints',
            "['updated_at', 'id']", 'highgo-replace-console.png',
        ),
        "Makefile": ('test-highgo-online-runner:', '$(MAKE) test-highgo-online-runner'),
        "scripts/test/highgo-consumer-flow-online_test.py": (),
        "scripts/test/online-highgo-consumer-fixture_test.py": (),
        "scripts/test/online-hosted-highgo-gate_test.py": (),
    }
    for relative, fragments in requirements.items():
        path = repository / relative
        if not path.is_file():
            raise RegistrationError(f"highgo-consumer-flow requires {relative}")
        text = path.read_text(encoding="utf-8")
        for fragment in fragments:
            if not source_contract_contains(text, fragment, repository):
                raise RegistrationError(f"highgo-consumer-flow {relative} is missing {fragment}")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
    job = re.search(r"(?ms)^  highgo-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if not job or 'runs-on: ubuntu-24.04' not in job['body'] or "github.event_name == 'workflow_dispatch' && inputs.suite == 'highgo-consumer-flow'" not in job['body']:
        raise RegistrationError("highgo-consumer-flow requires a manual Linux x86_64 Hosted job")
    if "inputs.suite != 'highgo-consumer-flow'" not in workflow:
        raise RegistrationError("highgo-consumer-flow must not also run on self-hosted")


def validate_local_execution_trace_profile(repository: Path) -> None:
    """The formal-identity local technical gate is never a GitHub dispatch."""
    path=repository / "scripts/test/monitor-traces-local-gate.py"
    make=(repository / "Makefile").read_text() if (repository / "Makefile").is_file() else ""
    if not path.exists() and not source_contract_contains(make, "test-monitor-traces-local:", repository):
        return
    required=("test-monitor-traces-local:", "@python3 scripts/test/monitor-traces-local-gate.py",
              "test-monitor-traces-local-runner:", "@$(MAKE) test-monitor-traces-local-runner",
              "scripts/test/monitor-traces-local-gate_test.py", "scripts/test/monitor-traces-local-online_test.py")
    if not path.is_file() or any(not source_contract_contains(make, value, repository) for value in required):
        raise RegistrationError("local execution Trace requires its manual entry and existing lifecycle runner")
    for relative in ("scripts/test/monitor-traces-local-online.py", "console/frontend/e2e/online/execution-trace-local.spec.js",
                     "system/backend/cmd/online-test-fixture/execution_trace_local.go"):
        if not (repository / relative).is_file():
            raise RegistrationError("local execution Trace is missing its formal-identity owner inputs")
    for workflow in (repository / ".github/workflows").glob("*.y*ml"):
        if re.search(r"(?:make\s+test-monitor-traces-local(?![A-Za-z0-9_-])|python3?\s+scripts/test/monitor-traces-local-gate.py)", workflow.read_text()):
            raise RegistrationError("local execution Trace must not execute in GitHub Actions")


def validate_local_http_quality_profile(repository: Path) -> None:
    """Run mocks in normal lifecycle CI, never the real manual product profile."""
    make=(repository / "Makefile").read_text() if (repository / "Makefile").is_file() else ""
    entry="scripts/test/monitor-http-local-gate.py"
    if not (repository / entry).exists() and not source_contract_contains(make, "test-monitor-http-local:", repository):
        return
    if '--case=$(or $(MONITOR_HTTP_LOCAL_CASE),full)' not in make_target_commands(make,"test-monitor-http-local"):
        raise RegistrationError("local HTTP quality requires bounded case forwarding through its original entry")
    for marker in ("test-monitor-http-local:", "@python3 "+entry,
                   "test-monitor-http-local-runner:", "@$(MAKE) test-monitor-http-local-runner",
                   "scripts/test/monitor-http-local-gate_test.py", "scripts/test/monitor-http-local-online_test.py"):
        if not source_contract_contains(make, marker, repository):
            raise RegistrationError("local HTTP quality requires manual entry and deterministic lifecycle registration")
    commands=make_target_commands(make,"test-monitor-http-local-runner")
    if "node --test scripts/test/http-quality-local-browser_test.mjs" not in [line.strip().removeprefix("@") for line in commands.splitlines()]:
        raise RegistrationError("local HTTP browser logout requires its deterministic Node regression")
    probe=commands.find("command -v openssl")
    tests=commands.find("python3 -m unittest")
    workflow_path=repository / ".github/workflows/platform-ci.yml"
    workflow=workflow_path.read_text() if workflow_path.is_file() else ""
    job=re.search(r"(?ms)^  platform-consistency:\n(?P<body>.*?)(?=^  [a-zA-Z0-9_-]+:\n|\Z)",workflow)
    body=job.group("body") if job else ""
    install=re.search(r"(?m)^\s*sudo apt-get install[^\n]*\bopenssl\b[^\n]*$",body)
    gate=body.find("make test-platform")
    verify=body.find("command -v openssl")
    if not (0<=probe<tests and install and install.end()<verify<gate):
        raise RegistrationError("local HTTP TLS requires OpenSSL admission and CI preparation before the gate")
    for relative in (entry, "scripts/test/http-quality-local-browser_test.mjs", "scripts/test/monitor-http-local-online.py",
                     "console/frontend/e2e/online/http-quality-local.spec.js",
                     "console/frontend/e2e/http-quality-page-support.mjs",
                     "console/frontend/e2e/online/local-identity-support.mjs",
                     "system/backend/cmd/online-test-fixture/http_quality_local.go"):
        if not (repository / relative).is_file():
            raise RegistrationError("local HTTP quality formal product input missing")
    for workflow in (repository / ".github/workflows").glob("*.y*ml"):
        if re.search(r"(?:make\s+test-monitor-http-local(?![A-Za-z0-9_-])|python3?\s+scripts/test/monitor-http-local-gate.py)", workflow.read_text()):
            raise RegistrationError("local HTTP quality must not execute in GitHub Actions")


def validate_local_catalog_browser_profile(repository: Path) -> None:
    make_path = repository / "Makefile"
    make = make_path.read_text() if make_path.is_file() else ""
    path = repository / "scripts/test/catalog-browser-local-gate.py"
    if not path.exists() and "test-catalog-browser-local:" not in make:
        return
    for fragment in ("test-catalog-browser-local:", "clean-catalog-browser-local:", "finish-catalog-browser-local:",
                     "test-catalog-browser-local-runner:", "@$(MAKE) test-catalog-browser-local-runner"):
        if not source_contract_contains(make, fragment, repository):
            raise RegistrationError("local Catalog browser lifecycle/runner entry missing")
    for relative in ("scripts/test/catalog-browser-local-gate.py", "scripts/test/catalog-browser-local-gate_test.py",
                     "scripts/test/catalog-browser-local-fixture.py", "scripts/test/catalog-browser-local-fixture_test.py",
                     "scripts/utils/disposable-compose.py", "system/backend/cmd/online-test-fixture/catalog_browser_local.go",
                     "system/backend/cmd/online-test-fixture/catalog_browser_local_postgres_test.go"):
        if not (repository / relative).is_file():
            raise RegistrationError("local Catalog browser owner input missing")
    for workflow in (repository / ".github/workflows").glob("*.y*ml"):
        if re.search(r"(?:make\s+test-catalog-browser-local(?![A-Za-z0-9_-])|python3?\s+scripts/test/catalog-browser-local-gate.py)", workflow.read_text()):
            raise RegistrationError("local Opera acceptance must not execute in GitHub Actions")


def validate_local_sgm_profile(repository: Path) -> None:
    """The approved technical page gate must never become a public T4 choice."""
    make_path = repository / "Makefile"
    make = make_path.read_text() if make_path.is_file() else ""
    script = repository / "scripts/test/manager-sgm-local-gate.py"
    if "test-manager-sgm-local:" not in make and not script.exists():
        return
    if not script.is_file() or "@python3 scripts/test/manager-sgm-local-gate.py" not in make:
        raise RegistrationError("local SGM requires its manual Make entry")
    if ("@python3 -m unittest scripts/test/manager-sgm-local-gate_test.py" not in make
            or not source_contract_contains(make, "@$(MAKE) test-manager-sgm-local-runner", repository)):
        raise RegistrationError("local SGM lifecycle tests must join the existing Manager runner")
    text = script.read_text()
    for fragment in ('"local-isolated-technical-page-acceptance"', '"formal_t4": False',
                     '"com.addp.sgm-local"', '"--no-hardlinks"', 'life.cleanup()', 'preflight()',
                     'MANAGER.cleanup_managed_artifact(', '"--env-file", "/dev/null"'):
        if not source_contract_contains(text, fragment, repository):
            raise RegistrationError("local SGM lifecycle is missing " + fragment)
    for workflow in (repository / ".github/workflows").glob("*.y*ml"):
        if re.search(r"(?:make\s+test-manager-sgm-local(?![A-Za-z0-9_-])|python3?\s+scripts/test/manager-sgm-local-gate.py)", workflow.read_text()):
            raise RegistrationError("private local SGM must not execute in GitHub Actions")


def validate_graph_protection_profile(repository: Path, registered: set[str]) -> None:
    if "graph-protection" not in registered:
        return
    required = {
        "scripts/test/online-hosted-security-gate.sh": (
            "# ADDP_ONLINE_SUITES=graph-protection", "ADDP_ONLINE_GRAPH_ENGINE", "graph-control.json", "graph-review.json", "online-graph-fixture.py start",
            "online-graph-fixture.py stop", "develop,graph,manager,service,transfer", "graph-target.json",
            "docker-compose.graph-protection-t4.yml", "compose_graph down --remove-orphans --volumes",
            ',console,graph-frontend,develop-frontend,service-frontend,nginx', 'CONSOLE_URL="$ADDP_PUBLIC_ORIGIN"',
        ),
        "scripts/test/docker-compose.graph-protection-t4.yml": ("services:", "graph-review-extractor:", "graph-review-extractor.py:ro"),
        "scripts/test/graph-review-online.py": ("verify_selection", "graph-review-stable-selection.json", "verify_items", "reader_403=True", "cross_graph_404=True", "domain_resources_remaining", "real_model=False"),
        "scripts/test/graph-review-extractor.py": ("ADDP_ONLINE_HOSTED", "MATERIAL", "bad-native"),
        "console/frontend/e2e/online/graph-review.spec.js": ("addp.graph-review-browser/v2", "browserAPIFromResponse", "login(", "batch_status: 500", "selection_cleared: true", "selection_stable_visible: true", "stableEmptyReviewSelection)", "graph-review-stable-selection.json"),
        "console/frontend/e2e/graph-relationship.fixture.js": ("installGraphRelationshipPaint", "stableRelationshipPaths", "graphRelationshipHits", "graphOverlapPaint", "__graphNodePaintEvents", "node_geometry", "circlePaths", "page.mouse.click", "fixture_id", "prop-row", "getImageData"),
        "console/frontend/e2e/graph-visual.fixture.js": ("stableGraphPixels", "stableExposedGraphPixels", "denseGraphStages", "graphEndpointGeometry", "page.mouse.down", "page.mouse.up", "stableGraphColorPixels", "fillPixelCount", "getImageData", "stableFrames", "graphNodeHits", "exploreGraphLayouts", "clickGraphNode", "graphObservationColor", "stableEmptyReviewSelection", "getAnimations", "emptyBackground", "emptyTick"),
        "graph/frontend/Dockerfile": ("COPY graph/frontend/nginx.conf /etc/nginx/conf.d/default.conf",),
        "graph/frontend/nginx.conf": ("try_files $uri $uri/ /index.html;", "try_files $uri =404;"),
        "scripts/test/graph-protection-online.py": (
            "addp.graph-protection-online/v16", "verify_dense", "verify_relationships", "verify_dense_filters", "verify_node_filters", "node_geometry", "verify_endpoint_geometry", "verify_same_color_geometry", "verify_exposed_frame", "verify_overlap_paint", "graph-same-color-geometry", "run_endpoint_case", "graph-endpoint-geometry", "graph-node-filter-frame.json", "graph-relationship-filter-frame.json", "graph-relationship-hit-frame.json", "relationship_pixels", "run_dense", "graph-dense-stable-frame.json", "verify_search_frame", "verify_exploration", "verify_native_edges", "native_path_edges(reader, target, ids)", "verify_pixel_frame", "graph-exploration-stable-frame.json", "graph-search-stable-frame.json", "run_review", "public_unmanaged", "public_protected",
            "same_engine_control", "separate_engine_control", "target_paths", "private_protected_anonymous", "domain_resources_remaining", "wait_owners(",
            'report["browser"] = run_browser(', 'e2e/online/graph-protection.spec.js',
            'report["develop_unmanaged"] = run_develop_browser(', 'report["develop_protected"] = run_develop_browser(',
            'e2e/online/develop-graph-query.spec.js',
            'report["service_graph_unmanaged"] = run_service_graph_phase(',
            'report["service_graph_protected"] = run_service_graph_phase(',
            'verify_service_graph_denial', 'delete synthetic Service graph definition',
            'verify_service_graph_foreign', 'len(foreign_checks) == 4',
            '"cross_tenant_runtime_verified": True', 'ADDP_ONLINE_GRAPH_SERVICE_FOREIGN_TENANT_ID',
        ),
        "console/frontend/e2e/online/graph-protection.spec.js": (
            "addp.graph-protection-browser/v7", "graph-relationship-hit-frame.json", "graph-relationship-filter-frame.json", "graph-node-filter-frame.json", "installGraphRelationshipPaint", "browserAPIFromResponse", "login(",
            'addp.service-graph-browser/v1', 'api.post(definition.endpoints.execute',
            'dto_address_unchanged: true', 'public_https_verified: false',
            "browse_snapshot: true", "knowledge_search: true", "public_switch_disabled: true", "graph_writes: 0",
            "release_assets: true", "deep_link_reload: true", "theme_canvas_pixels: true",
            "graph-browser-diagnostics.json", "graphBrowserDiagnostic(", "search_stable_visible: true", "search_node_hits: hits.length", "expansion_stable_visible: true", "nativeNodes, expected.native_edges,", "count: found.nodes.length, color, fillColor", "layout_stable_visible: true", "graph-exploration-stable-frame.json", "exploreGraphLayouts(", "graphNodeHits(", "stableGraphPixels", "graph-search-stable-frame.json", "theme_typography: true", "graphDocumentHasThemeTypography)",
        ),
        "console/frontend/e2e/online/develop-graph-query.spec.js": (
            "addp.develop-graph-query-browser/v1", "browserAPIFromResponse", "login(",
            "typed_parameters: true", "graph_table_visible: true", "protected_result_absent: true", "control_readable: true",
            "develop.query.protection_required", "unexpected_writes: 0", "theme_canvas_pixels: true", "graphCanvasHasThemePixels)",
        ),
        "business/scripts/online-graph-fixture.py": (
            "--volumes", "owned_resources", "must not publish host ports", "0o600", "graph_protection_control", "def exploration(", "DENSE_EDGES",
        ),
        "business/docker-compose.online-graph.yml": (
            "../scripts/infra/falkordb.yml", "../scripts/test/docker-compose.neo4j-t2.yml", "ports: !reset []", "name: addp-network", "tmpfs:",
        ),
    }
    for path, fragments in required.items():
        file = repository / path
        if not file.is_file() or any(not source_contract_contains(file.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError("Graph native protection contract is incomplete: " + path)
    # The host preflight must reach every service declared by the suite, even
    # though normal browser traffic only enters through production Nginx.
    lifecycle = (repository / "scripts/test/online-hosted-security-gate.sh").read_text()
    environment = (repository / "scripts/utils/hosted-online.sh").read_text() + "\n" + lifecycle
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    case_selector = re.search(r"(?ms)^      graph_case:\n(?P<body>.*?)(?=^      [a-zA-Z0-9_-]+:\n|\Z)", workflow)
    if (case_selector is None or any(not source_contract_contains(case_selector.group('body'), token, repository) for token in ('type: choice', 'default: full', '- full', '- endpoint-geometry', '- endpoint-same-color'))
            or 'ADDP_ONLINE_GRAPH_CASE: ${{ inputs.graph_case }}' not in workflow
            or any(not source_contract_contains(lifecycle, token, repository) for token in ('ADDP_ONLINE_GRAPH_CASE=${ADDP_ONLINE_GRAPH_CASE-full}', 'full|endpoint-geometry|endpoint-same-color|execution-lifecycle)'))):
        raise RegistrationError('Graph endpoint geometry requires explicit bounded case selection')
    if lifecycle.index('full|endpoint-geometry|endpoint-same-color|execution-lifecycle)') > lifecycle.index('source "$ROOT_DIR/scripts/utils/hosted-online.sh"'):
        raise RegistrationError('Graph case validation must precede Hosted lifecycle')
    overlay = (repository / "scripts/test/docker-compose.graph-protection-t4.yml").read_text()
    suite = load_suite_registry(repository)["graph-protection"]
    for owner, variable in suite.services:
        ports = re.findall(rf"\b{re.escape(variable)}=http://127\.0\.0\.1:(\d+)\b", environment)
        if not ports:
            raise RegistrationError(f"Graph preflight requires an explicit loopback address: {variable}")
        port = ports[-1]
        service = "gateway" if owner == "gateway" else owner + "-backend"
        block = compose_service_block(overlay, service) or ""
        bindings = re.findall(r'''["']([^"']+:\d+:\d+)["']''', block)
        if bindings != [f"127.0.0.1:{port}:{port}"]:
            raise RegistrationError(f"Graph preflight {variable} requires exactly one loopback diagnostic binding on {service}: {port}")
    commands = make_target_commands((repository / "Makefile").read_text(), "test-online-runner")
    for test in ("graph-review-online_test.py", "graph-protection-online_test.py", "online-graph-fixture_test.py"):
        if not source_contract_contains(commands, test, repository):
            raise RegistrationError("Graph protection regression must enter test-online-runner")
    if not source_contract_contains(commands, "console/frontend/e2e/online/graph-review.spec.js", repository):
        raise RegistrationError("Graph review browser syntax must enter test-online-runner")
    if not source_contract_contains(commands, "console/frontend/e2e/online/graph-protection.spec.js", repository):
        raise RegistrationError("Graph Console browser syntax must enter test-online-runner")
    if not source_contract_contains(commands, "console/frontend/e2e/online/develop-graph-query.spec.js", repository):
        raise RegistrationError("Develop native browser syntax must enter test-online-runner")
    if 'if [ "$ONLINE_SUITE" != graph-protection ]; then' in lifecycle or "playwright install --with-deps chromium" not in lifecycle:
        raise RegistrationError("Graph Console requires Chromium in the same Hosted lifecycle")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  security-hosted-t4:\n(?P<body>.*?)(?=^  [a-zA-Z0-9_-]+:\n|\Z)", workflow)
    if job is None or "inputs.suite == 'graph-protection'" not in job.group("body") or "runs-on: ubuntu-24.04" not in job.group("body"):
        raise RegistrationError("Graph protection requires the standard Ubuntu Hosted Security lifecycle")
    expected_group = "group: online-t4-${{ inputs.suite }}${{ inputs.suite == 'graph-protection' && format('-{0}', inputs.graph_engine) || '' }}"
    if re.findall(r"(?m)^      (group: .+)$", job.group("body")) != [expected_group] or not re.search(r"(?m)^      cancel-in-progress: false$", job.group("body")):
        raise RegistrationError("Graph engine runs require distinct engine concurrency groups without cancelling active runs")
    selector = re.search(r"(?ms)^      graph_engine:\n(?P<body>.*?)(?=^      [a-zA-Z0-9_-]+:\n|\Z)", workflow)
    if selector is None or not all(value in selector.group("body") for value in ("type: choice", "- falkordb", "- neo4j")) or "ADDP_ONLINE_GRAPH_ENGINE: ${{ inputs.graph_engine }}" not in job.group("body"):
        raise RegistrationError("Graph protection must explicitly select and propagate its native engine")
    if "&& inputs.suite != 'graph-protection'" not in workflow:
        raise RegistrationError("Graph protection must not also dispatch on self-hosted")


def validate_execution_profiles(repository: Path, registered: set[str]) -> None:
    if "cleanup-execution" not in registered:
        return
    registry = load_suite_registry(repository)
    if registry["cleanup-execution"].nightly:
        raise RegistrationError("Cleanup execution is manual only")
    if set(owner for owner, _ in registry["cleanup-execution"].services) != {
            "gateway", "system", "meta", "manager", "transfer", "develop", "service", "quality", "graph", "asset", "model", "standard"}:
        raise RegistrationError("Cleanup execution must preflight all ten Owners")
    required = {
        "scripts/test/online-gate.py": ("scripts.test.graph-execution-online", "scripts.test.cleanup-execution-online",
            "validate_execution_archive(profile", "online_execution_checks_missing", "online_execution_owner_results_invalid",
            "running_process_crash_without_replay", "physical_effect_acceptance", "blocked_provider_identity",
            "browser_revocation_refresh_pagination_late_response", "cleanup-browser.json"),
        "scripts/test/online-preflight.py": ("--execution-profile", "validate_execution_environment(profile", "graph-peer",
            "independent runtime instance identities", "ADDP_ONLINE_CLEANUP_", "secret.stat().st_uid != os.getuid()"),
        "scripts/test/online-hosted-security-gate.sh": ("# ADDP_ONLINE_SUITES=cleanup-execution", "COMPOSE_PROFILES=execution-lifecycle",
            "docker-compose.cleanup-execution-t4.yml", "GRAPH_BUILD_LEASE_SECONDS=30 GRAPH_BUILD_HEARTBEAT_SECONDS=5",
            "ADDP_ONLINE_GRAPH_PEER_URL=http://127.0.0.1:18186", "before_online_application_stop()",
            "python3 -m scripts.test.execution-online-support resume", "execution-recovery.txt", "graph-backend-peer", ",console,system-frontend,nginx",
            "compose_graph down --remove-orphans --volumes"),
        "scripts/test/docker-compose.graph-protection-t4.yml": ("graph-backend-peer:", "profiles: [execution-lifecycle]",
            "SERVICE_HOST: graph-backend-peer", 'ports: !override ["127.0.0.1:18186:8186"]', 'restart: "no"', 'GRAPH_BUILD_LEASE_SECONDS: "30"',
            'GRAPH_BUILD_HEARTBEAT_SECONDS: "5"', "ADDP_ONLINE_TEST_RUN_ID", "ADDP_ONLINE_GRAPH_CASE"),
        "scripts/test/online-hosted-security-gate_test.py": ("test_execution_graph_starts_peer_and_recovers_before_destruction",
            "test_cleanup_execution_starts_exact_ten_owners_without_peer", "test_execution_recovery_scenario_and_residual_failures_still_destroy_and_fail"),
        "scripts/test/online-gate_test.py": ("test_execution_report_rejects_missing_failed_or_forged_scope",
            "test_execution_archive_is_required_and_must_match_stdout"),
    }
    for path, fragments in required.items():
        file = repository / path
        if not file.is_file() or any(not source_contract_contains(file.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError("Execution CI contract is incomplete: " + path)
    overlay = (repository / "scripts/test/docker-compose.cleanup-execution-t4.yml").read_text()
    if compose_service_block(overlay, "graph-backend-peer"):
        raise RegistrationError("Cleanup requires exactly one Graph Owner instance")
    for owner in ("system", "meta", "manager", "transfer", "develop", "service", "quality", "graph", "asset", "model", "standard"):
        block = compose_service_block(overlay, owner + "-backend") or ""
        if "ADDP_ONLINE_TEST_RUN_ID" not in block or 'ADDP_ONLINE_HOSTED: "1"' not in block or "127.0.0.1:" not in block:
            raise RegistrationError("Cleanup Owner deployment identity/loopback binding missing: " + owner)
    graph_overlay = (repository / "scripts/test/docker-compose.graph-protection-t4.yml").read_text()
    peer = compose_service_block(graph_overlay, "graph-backend-peer") or ""
    if 'restart: "no"' not in peer or 'ports: !override ["127.0.0.1:18186:8186"]' not in peer:
        raise RegistrationError("Graph peer requires no automatic restart and one overridden loopback port")
    lifecycle = (repository / "scripts/test/online-hosted-security-gate.sh").read_text()
    if lifecycle.index("EXECUTION_PROFILE=") > lifecycle.index('source "$ROOT_DIR/scripts/utils/hosted-online.sh"'):
        raise RegistrationError("Execution selection must precede Hosted admission")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    self_hosted = re.search(r"(?ms)^  online-t4:\n(?P<body>.*?)(?=^  [a-zA-Z0-9_-]+:\n|\Z)", workflow)
    if self_hosted is None or "&& inputs.suite != 'cleanup-execution'" not in self_hosted.group("body"):
        raise RegistrationError("Cleanup execution must not also dispatch on self-hosted")
    job = re.search(r"(?ms)^  security-hosted-t4:\n(?P<body>.*?)(?=^  [a-zA-Z0-9_-]+:\n|\Z)", workflow)
    if (job is None or "inputs.suite == 'cleanup-execution'" not in job.group("body")
            or "&& inputs.suite != 'cleanup-execution'" not in workflow
            or "- execution-lifecycle" not in workflow):
        raise RegistrationError("Execution profiles require unique manual Hosted dispatch")
    upload = re.search(r"(?ms)^      - name: Upload Hosted execution evidence\n(?P<body>.*?)(?=^      - name:|\Z)", job.group("body"))
    if upload is None:
        raise RegistrationError("Execution evidence requires a curated upload")
    paths = re.findall(r"(?m)^            \$\{\{ runner.temp \}\}/addp-online-\$\{\{ github.run_id \}\}/(.+)$", upload.group("body"))
    if set(paths) != {"readiness.txt", "summary.txt", "online-report.json", "graph-execution.json",
                      "cleanup-execution.json", "cleanup-browser.json", "execution-recovery.txt"} or len(paths) != 7:
        raise RegistrationError("Execution upload must contain only public reports and lifecycle outcomes")
    if "logs/" in upload.group("body") or "addp-online-secret" in upload.group("body"):
        raise RegistrationError("Execution upload must not archive private controls or raw logs")
    if "if: always() && inputs.suite != 'cleanup-execution' && (inputs.suite != 'graph-protection' || inputs.graph_case != 'execution-lifecycle')" not in job.group("body"):
        raise RegistrationError("Execution profiles must not use the legacy broad artifact upload")
    if not (repository / "console/frontend/e2e/online/cleanup-execution.spec.js").is_file():
        raise RegistrationError("Cleanup browser scenario is missing")
    commands = make_target_commands((repository / "Makefile").read_text(), "test-online-runner")
    if not source_contract_contains(commands, "console/frontend/e2e/online/cleanup-execution.spec.js", repository):
        raise RegistrationError("Cleanup browser syntax must enter the standard Online gate")
    if not source_contract_contains(commands, "scripts/ci/check-online-ci-registration_test.py", repository):
        raise RegistrationError("Execution registration regressions must enter the standard Online gate")


def validate_native_cleanup_profile(repository: Path, registered: set[str]) -> None:
    if 'cleanup-native-effects' not in registered:
        return
    registry = load_suite_registry(repository)
    suite = registry['cleanup-native-effects']
    owners = {'meta', 'manager', 'transfer', 'develop', 'service', 'quality', 'graph', 'asset', 'model', 'standard'}
    if suite.nightly or {owner for owner, _ in suite.services} != owners | {'gateway', 'system', 'security', 'monitor'}:
        raise RegistrationError('Native Cleanup requires manual selection and all ten Owners')
    required = {
        'scripts/test/online-hosted-cleanup-native-gate.sh': ('# ADDP_ONLINE_SUITES=cleanup-native-effects',
            'if [ "$ADDP_ONLINE_NATIVE_CASE" = manager-late-writer ]; then',
            'export MANAGER_EXECUTION_LEASE_DURATION=30s MANAGER_EXECUTION_HEARTBEAT_INTERVAL=5s',
            'case "${ADDP_ONLINE_NATIVE_CASE:-}" in', 'cleanup-native-infra.py prepare', 'cleanup-native-infra.py manifest',
            'cleanup-native-infra.py stop', 'cleanup-native-infra.py recover-writer', 'model3d-workflow-engine', 'transfer-continuous-worker', ' start-obj',
            'execution-online-support resume', 'compose_native down --remove-orphans --volumes',
            'source "$IDENTITY_ENV"', 'run_native_phase scenario make test-online ONLINE_SUITE="$ONLINE_SUITE"',
            'run_native_phase images make build-images IMAGE_BUILD_ARGS="--verify --services $image_services --jobs 1"',
            'native_cleanup_phase=%s state=started', 'native_cleanup_phase=%s state=returned exit=%s'),
        'scripts/test/cleanup-native-infra.py': ('def advertised_config(', 'internal://cleanup-native-dlq-proxy:29092',
            'def verify_pin(', 'def stop(self):', "'native-container-pins.json'", "'--network', 'host'", 'source_minio'),
        'scripts/test/online-gate.py': ('validate_native_cleanup_report', "contract['native_case_checks']", 'cleanup-native-effects.json', 'native_late_writer_scope', 'manager_glb_upload_after_owner_crash'),
        'scripts/test/online-preflight.py': ('cleanup-native-fixture.json', 'ADDP_ONLINE_CLEANUP_NATIVE_', 'ADDP_ONLINE_NATIVE_CASE'),
        'scripts/test/cleanup-native-effects-online.py': ('native_case_checks', 'source_objects_unchanged', 'target_business_rows_preserved'),
        'scripts/test/native-effect-proxy.py': ('--config', '18087', 'blocked_replays'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), fragment, repository) for fragment in fragments):
            raise RegistrationError('Native Cleanup contract incomplete: ' + relative)
    contract = json.loads((repository / 'scripts/test/execution-online-contract.json').read_text())
    cases = set(contract['native_case_checks'])
    lifecycle = (repository / 'scripts/test/online-hosted-cleanup-native-gate.sh').read_text()
    selection = re.search(r'(?m)^  (manager-success[^)]*)\) ;;$', lifecycle)
    tree = ast.parse((repository / 'scripts/test/cleanup-native-infra.py').read_text())
    assignments = [node.value for node in tree.body if isinstance(node, ast.Assign)
                   and any(isinstance(target, ast.Name) and target.id == 'CASES' for target in node.targets)]
    if (selection is None or set(selection.group(1).split('|')) != cases or len(assignments) != 1
            or set(ast.literal_eval(assignments[0])) != cases):
        raise RegistrationError('Native Cleanup case selection must equal the sole Owner contract')
    fixture = (repository / 'business/scripts/online-manager-minio-fixture.sh').read_text()
    manager_selection = re.search(r'case "\$\{ADDP_ONLINE_NATIVE_CASE:-\}" in ([a-z|-]+)\)', fixture)
    if manager_selection is None or set(manager_selection.group(1).split('|')) != {case for case in cases if case.startswith('manager-')}:
        raise RegistrationError('Native Manager source fixture case selection differs')
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  cleanup-native-hosted-t4:\n(?P<body>.*?)(?=^  [a-zA-Z0-9_-]+:\n|\Z)', workflow)
    self_hosted = re.search(r'(?ms)^  online-t4:\n(?P<body>.*?)(?=^  [a-zA-Z0-9_-]+:\n|\Z)', workflow)
    if (job is None or self_hosted is None or "inputs.suite != 'cleanup-native-effects'" not in self_hosted.group('body')
            or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'cleanup-native-effects'" not in job.group('body')
            or 'cancel-in-progress: false' not in job.group('body') or 'runs-on: ubuntu-24.04' not in job.group('body')
            or 'inputs.native_case' not in job.group('body') or 'timeout-minutes: 120' not in job.group('body')):
        raise RegistrationError('Native Cleanup requires a sole manual Hosted job')
    paths = re.findall(r'(?m)^            \$\{\{ runner.temp \}\}/addp-online-\$\{\{ github.run_id \}\}/(.+)$', job.group('body'))
    if len(paths) != 5 or set(paths) != {'readiness.txt', 'summary.txt', 'online-report.json', 'cleanup-native-effects.json', 'execution-recovery.txt'}:
        raise RegistrationError('Native Cleanup must archive only five public reports/outcomes')
    commands = make_target_commands((repository / 'Makefile').read_text(), 'test-online-runner')
    for relative in ('cleanup-native-effects-online_test.py', 'native-effect-proxy_test.py', 'cleanup-native-infra_test.py'):
        if not source_contract_contains(commands, relative, repository):
            raise RegistrationError('Native Cleanup deterministic tests missing from standard Online gate')
    overlay = (repository / 'scripts/test/docker-compose.cleanup-native-t4.yml').read_text()
    if compose_service_block(overlay, 'graph-backend-peer'):
        raise RegistrationError('Native Cleanup requires one Graph Owner')
    for owner in owners | {'system', 'security', 'monitor'}:
        block = compose_service_block(overlay, owner + '-backend') or ''
        if any(not source_contract_contains(block, fragment, repository) for fragment in ('ADDP_ONLINE_TEST_RUN_ID', 'ADDP_ONLINE_HOSTED: "1"', 'ports: !override ["127.0.0.1:')):
            raise RegistrationError('Native Owner identity/loopback boundary missing: ' + owner)
    manager = compose_service_block(overlay, 'manager-backend') or ''
    transfer = compose_service_block(overlay, 'transfer-backend') or ''
    if 'cleanup-native-object-proxy:19000' not in manager or 'cleanup-native-index-proxy:17700' not in manager or any(
            not source_contract_contains(transfer, fragment, repository) for fragment in ('cleanup-native-dlq-proxy:29092', 'cleanup-native-capture-proxy:18083')):
        raise RegistrationError('Native app endpoints must be fixed before startup')


def validate_administrator_governance_profile(repository: Path, registered: set[str]) -> None:
    if "platform-administrator-governance" not in registered:
        return
    required = (
        "scripts/test/platform-administrator-governance-online.py",
        "scripts/test/platform-administrator-governance-online_test.py",
        "scripts/test/online-hosted-administrator-governance-gate_test.py",
        "system/backend/cmd/online-test-fixture/administrator_governance.go",
        "system/backend/cmd/online-test-fixture/administrator_governance_test.go",
        "system/backend/cmd/iam-recovery/main.go",
        "system/backend/cmd/iam-bootstrap/main.go",
        "console/frontend/e2e/online/platform-administrator-governance.spec.js",
    )
    for path in required:
        if not (repository / path).is_file():
            raise RegistrationError("administrator governance requires " + path)
    commands = make_target_commands((repository / "Makefile").read_text(), "test-online-runner")
    for fragment in ("platform-administrator-governance-online_test.py", "online-hosted-administrator-governance-gate_test.py",
                     "platform-administrator-governance.spec.js", "test_administrator_governance_requires_manual_isolated_profile"):
        if not source_contract_contains(commands, fragment, repository):
            raise RegistrationError("administrator governance regression missing: " + fragment)
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  administrator-governance-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'platform-administrator-governance'" not in job.group("body") or "runs-on: ubuntu-24.04" not in job.group("body"):
        raise RegistrationError("administrator governance must use manual Hosted Linux")
    if "inputs.suite != 'platform-administrator-governance'" not in workflow:
        raise RegistrationError("administrator governance must not dispatch to self-hosted")
    gate = (repository / "scripts/test/online-hosted-administrator-governance-gate.sh").read_text()
    for fragment in ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite platform-administrator-governance',
                     'make build-iam-recovery build-iam-bootstrap GOOS=linux GOARCH=amd64 OUT_DIR=dist BUILD_TYPE=release',
                     'playwright install --with-deps chromium', 'make test-online "ONLINE_SUITE=$ONLINE_SUITE"'):
        if not source_contract_contains(gate, fragment, repository):
            raise RegistrationError("administrator governance lifecycle missing: " + fragment)


def validate_catalog_domain_overview_profile(repository: Path, registered: set[str]) -> None:
    if "catalog-domain-overview" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  catalog-domain-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'catalog-domain-overview'" not in job.group("body") or "runs-on: ubuntu-24.04" not in job.group("body"):
        raise RegistrationError("Catalog domain overview requires manual Hosted Linux")
    if "inputs.suite != 'catalog-domain-overview'" not in workflow or "catalog-domain-overview)" in (repository / "scripts/test/online-host-gate.sh").read_text():
        raise RegistrationError("Catalog domain overview must not use self-hosted")
    if "online-hosted-catalog-domain-gate_test.py" not in make_target_commands((repository / "Makefile").read_text(), "test-catalog-domain-online-runner"):
        raise RegistrationError("Catalog Hosted cleanup regression is missing")
    required = {
        "scripts/test/online-hosted-catalog-domain-gate.sh": ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', "--suite catalog-domain-overview", "-model -catalog -quality"),
        "scripts/test/catalog-domain-overview-online.py": ("reader_inventory_denied", "stable_source_identities", "reference_element_unchanged", "catalog_history", "finally:"),
        "scripts/test/catalog-domain-overview-online_test.py": ("test_partial_fixture_failure_cleans_created_domain_and_leaves_reference_read_only", "test_cleanup_attempts_all_exact_resources_and_reports_failure"),
        "console/frontend/e2e/catalog-domain-overview.spec.js": ("catalog-domain-overview", "inventory", "governance"),
        "Makefile": ("test-catalog-domain-online-runner", "$(MAKE) test-catalog-domain-online-runner"),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file():
            raise RegistrationError("Catalog domain overview requires " + relative)
        content = path.read_text(encoding="utf-8")
        if any(not source_contract_contains(content, fragment, repository) for fragment in fragments):
            raise RegistrationError("Catalog domain overview profile is incomplete: " + relative)


def validate_execution_retention_profile(repository: Path, registered: set[str]) -> None:
    if "execution-retention-readonly" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  execution-retention-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
            or "if: github.event_name == 'workflow_dispatch' && (inputs.suite == 'execution-retention-readonly' || inputs.suite == 'execution-events-retention' || inputs.suite == 'execution-retention-hourly')" not in job.group("body")
            or "inputs.suite != 'execution-retention-readonly'" not in workflow):
        raise RegistrationError("Execution retention requires one manual Hosted Linux job")
    gate = (repository / "scripts/test/online-hosted-execution-retention-gate.sh").read_text()
    for fragment in ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'scripts/dev/stop-exact-process.sh -system',
                     'scripts/dev/start.sh --exact-process -system', '-m "$RETENTION_DRIVER" prepare',
                     'run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"'):
        if not source_contract_contains(gate, fragment, repository):
            raise RegistrationError("Execution retention lifecycle missing " + fragment)
    makefile = (repository / "Makefile").read_text()
    if not source_contract_contains(make_target_commands(makefile, "test-online-runner"), "$(MAKE) test-execution-retention-online-runner", repository):
        raise RegistrationError("Execution retention regressions must belong to test-online-runner")
    commands = make_target_commands(makefile, "test-execution-retention-online-runner")
    for name in ("execution-retention-readonly-online_test.py", "online-hosted-execution-retention-gate_test.py"):
        if not source_contract_contains(commands, name, repository) or not (repository / "scripts/test" / name).is_file():
            raise RegistrationError("Execution retention regression missing " + name)

    if "execution-events-retention" in registered:
        if ("ONLINE_SUITE_INPUT: ${{ inputs.suite }}" not in job.group("body")
                or "inputs.suite != 'execution-events-retention'" not in workflow
                or "scripts.test.execution-events-retention-online" not in gate
                or not source_contract_contains(commands, "execution-events-retention-online_test.py", repository)
                or not (repository / "scripts/test/execution-events-retention-online_test.py").is_file()):
            raise RegistrationError("Execution events require a dedicated manual Hosted profile and regressions")

    if "execution-retention-hourly" in registered:
        if ("inputs.suite != 'execution-retention-hourly'" not in workflow
                or "timeout-minutes: ${{ inputs.suite == 'execution-retention-hourly' && 180 || 60 }}" not in job.group("body")
                or "execution-retention-hourly) RETENTION_DRIVER=scripts.test.execution-retention-hourly-online" not in gate
                or "if [ \"$ONLINE_SUITE\" = execution-retention-hourly ]; then export ADDP_ONLINE_TEST_TIMEOUT_SECONDS=8100; fi" not in gate
                or not source_contract_contains(commands, "execution-retention-hourly-online_test.py", repository)
                or not (repository / "scripts/test/execution-retention-hourly-online_test.py").is_file()):
            raise RegistrationError("Hourly retention requires its manual-only long budget and regressions")


def validate_event_pagination_profile(repository: Path, registered: set[str]) -> None:
    if "execution-events-pagination" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  event-pagination-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
            or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'execution-events-pagination'" not in job.group("body")
            or "inputs.suite != 'execution-events-pagination'" not in workflow):
        raise RegistrationError("Event pagination requires its manual Hosted profile")
    gate = (repository / "scripts/test/online-hosted-event-pagination-gate.sh").read_text()
    for part in ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'scripts/dev/start.sh -meta -monitor -console',
                 'playwright install --with-deps chromium', 'run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"'):
        if part not in gate:
            raise RegistrationError("Event pagination lifecycle missing " + part)
    makefile = (repository / "Makefile").read_text()
    if not source_contract_contains(make_target_commands(makefile, "test-online-runner"), "$(MAKE) test-execution-events-pagination-runner", repository):
        raise RegistrationError("Event pagination regressions need the standard Online owner")
    commands = make_target_commands(makefile, "test-execution-events-pagination-runner")
    for name in ("execution-events-pagination-online_test.py", "online-hosted-event-pagination-gate_test.py", "execution-event-pagination-fixture_test.go", "execution-events-pagination.spec.js"):
        if not source_contract_contains(commands, name, repository):
            raise RegistrationError("Event pagination regression missing " + name)
    fixture = make_target_commands(makefile, "test-execution-event-pagination-fixture")
    if not source_contract_contains(fixture, "ADDP_EXECUTION_EVENT_FIXTURE=1", repository) or not source_contract_contains(fixture, "TestHostedExecutionEventPaginationFixture", repository):
        raise RegistrationError("Event pagination fixture requires explicit guarded entry")


def check_registration(repository: Path) -> None:
    registry = load_suite_registry(repository)
    registered = load_registered_suites(registry)
    nightly = load_nightly_suites(registry)
    profiles = load_deployment_profiles(repository)
    workflow = load_workflow_suites(repository, nightly)
    if set(profiles) != registered:
        raise RegistrationError(
            f"Online deployment profiles {sorted(profiles)} do not match registered suites {sorted(registered)}"
        )
    if workflow != registered:
        raise RegistrationError(
            f"Online workflow choices {sorted(workflow)} do not match registered suites {sorted(registered)}"
        )
    validate_execution_retention_profile(repository, registered)
    validate_event_pagination_profile(repository, registered)
    validate_catalog_domain_overview_profile(repository, registered)
    validate_highgo_profile(repository, registered)
    validate_metric_engine_variant(repository, registered)
    validate_pg_authorization_profile(repository, registered)
    validate_public_origin_browser_profile(repository, registered)
    validate_vector_pmtiles_profile(repository, registered)
    validate_raster_workflow_profile(repository, registered)
    validate_raster_capacity_profile(repository, registered)
    validate_raster_contour_profile(repository, registered)
    validate_raster_polygonize_profile(repository, registered)
    validate_vector_rasterize_profile(repository, registered)
    validate_vector_zonal_statistics_profile(repository, registered)
    validate_raster_proximity_profile(repository, registered)
    validate_raster_focal_mode_profile(repository, registered)
    validate_raster_boundary_clean_profile(repository, registered)
    validate_raster_thin_profile(repository, registered)
    validate_raster_fill_depressions_profile(repository, registered)
    validate_raster_flow_direction_d8_profile(repository, registered)
    validate_raster_flow_accumulation_d8_profile(repository, registered)
    validate_raster_drainage_basins_d8_profile(repository, registered)
    validate_raster_flow_length_d8_profile(repository, registered)
    validate_raster_flow_accumulation_area_d8_profile(repository, registered)
    validate_raster_distance_to_stream_d8_profile(repository, registered)
    validate_raster_hand_d8_profile(repository, registered)
    validate_raster_curvature_profile(repository, registered)
    validate_raster_profile_curvature(repository, registered)
    validate_raster_cut_fill(repository, registered)
    validate_raster_surface_area(repository, registered)
    validate_raster_slope_orthogonal(repository, registered)
    validate_raster_sight_orthogonal(repository, registered)
    validate_raster_viewshed(repository, registered)
    validate_raster_line_of_sight(repository, registered)
    validate_raster_tangential_curvature(repository, registered)
    validate_raster_plan_curvature(repository, registered)
    validate_raster_tpi(repository, registered)
    validate_raster_tri(repository, registered)
    validate_raster_roughness(repository, registered)
    validate_raster_twi(repository, registered)
    validate_raster_spi(repository, registered)
    validate_raster_sieve(repository, registered)
    validate_raster_focal_percentile(repository, registered)
    validate_raster_lineage_observer(repository, registered)
    validate_raster_flow_length_upstream_d8_profile(repository, registered)
    validate_raster_stream_order_d8_profile(repository, registered)
    validate_raster_stream_shreve_d8_profile(repository, registered)
    validate_raster_stream_to_vector_d8_profile(repository, registered)
    validate_raster_stream_link_d8_profile(repository, registered)
    validate_raster_flow_accumulation_weighted_d8_profile(repository, registered)
    validate_raster_watershed_d8_profile(repository, registered)
    validate_raster_subcatchments_d8_profile(repository, registered)
    validate_raster_snap_pour_points_profile(repository, registered)
    validate_raster_band_recommendation_profile(repository, registered)
    validate_raster_statistics_percentile(repository, registered)
    validate_raster_statistics_percentiles(repository, registered)
    validate_raster_zonal_percentile(repository, registered)
    validate_raster_zonal_percentiles(repository, registered)
    validate_raster_zonal_class_frequency(repository, registered)
    validate_raster_zonal_weighted(repository, registered)
    validate_raster_zonal_weighted_stddev(repository, registered)
    validate_vector_zonal_percentile(repository, registered)
    validate_vector_zonal_percentiles(repository, registered)
    validate_sample_points(repository, registered)
    validate_sample_points_bilinear(repository, registered)
    validate_raster_sample_lines(repository, registered)
    validate_interpolate_idw(repository, registered)
    validate_interpolate_idw_min_points(repository, registered)
    validate_interpolate_idw_max_points(repository, registered)
    validate_interpolate_idw_power(repository, registered)
    validate_point_density(repository, registered)
    validate_line_density(repository, registered)
    validate_line_density_weighted(repository, registered)
    validate_line_kernel_density(repository, registered)
    validate_point_kernel_density(repository, registered)
    validate_point_density_weighted(repository, registered)
    validate_focal_percentiles(repository, registered)
    validate_focal_square_seven(repository, registered)
    validate_vector_zonal_class_frequency(repository, registered)
    validate_vector_zonal_weighted(repository, registered)
    validate_vector_zonal_weighted_stddev(repository, registered)
    validate_vector_zonal_area_weighted(repository, registered)
    validate_vector_zonal_area_weighted_stddev(repository, registered)
    validate_raster_focal_variety(repository, registered)
    validate_raster_focal_circle(repository, registered)
    validate_raster_focal_circle_orthogonal(repository, registered)
    validate_raster_focal_circle_weighted(repository, registered)
    validate_raster_boundary_clean_two_rounds(repository, registered)
    validate_raster_region_group(repository, registered)
    validate_raster_focal_weighted_mean(repository, registered)
    validate_raster_focal_weighted_stddev(repository, registered)
    validate_raster_focal_weighted_sum(repository, registered)
    validate_raster_proximity_rectangular(repository, registered)
    validate_raster_proximity_orthogonal(repository, registered)
    validate_raster_sieve_protected(repository, registered)
    validate_raster_sieve_area(repository, registered)
    validate_raster_nibble_profile(repository, registered)
    validate_raster_disk_pressure_profile(repository, registered)
    validate_raster_nibble_capacity_profile(repository, registered)
    validate_raster_focal_statistics_profile(repository, registered)
    validate_raster_zonal_statistics_profile(repository, registered)
    validate_vector_topology_profile(repository, registered)
    validate_sql_pool_profile(repository, registry)
    validate_hdfs_spark_profile(repository, registered)
    validate_elasticsearch_spark_profile(repository, registered)
    validate_orchestrator_execution_profile(repository, registered)
    validate_quality_dynamic_binding_profile(repository, registered)
    validate_platform_node_metrics_profile(repository, registered)
    validate_administrator_governance_profile(repository, registered)
    validate_module_registry_process_profile(repository, registered)
    validate_workflow_runtime_health_profile(repository, registered)
    validate_consumer_engine_recovery_profile(repository, registered)
    validate_enterprise_catalog_publishing_profile(repository, registered)
    validate_workbench_service_consumption_profile(repository, registered)
    validate_manager_internal_artifact_lineage_profile(repository, registered)
    validate_local_catalog_browser_profile(repository)
    validate_local_sgm_profile(repository)
    validate_local_execution_trace_profile(repository)
    validate_local_http_quality_profile(repository)
    validate_manager_hybrid_search_profile(repository, registered)
    validate_security_transfer_protection_profile(repository, registered)
    validate_security_plaintext_access_profile(repository, registered)
    validate_security_mysql_owner_protection_profile(repository, registered)
    validate_graph_protection_profile(repository, registered)
    validate_execution_profiles(repository, registered)
    if "cleanup-execution" in registered:
        validate_execution_runner_ci(repository)
    validate_native_cleanup_profile(repository, registered)
    validate_oceanbase_consumer_flow_profile(repository, registered)
    validate_tidb_consumer_flow_profile(repository, registered)
    validate_opengauss_consumer_flow_profile(repository, registered)
    validate_kingbase_consumer_flow_profile(repository, registered)
    validate_transfer_insert_only_mysql_profile(repository, registered)
    validate_transfer_relational_sql_etl_profile(repository, registered)



VECTOR_ZONAL_CLASS_FREQUENCY_REQUIRED = {'business/scripts/online-vector-zonal-class-frequency-fixture.py': ('NFSExport', 'def oracle', 'def verify_vector', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'np.array_equal(band.ReadAsArray(),values,equal_nan=True)', 'storage FID', "with state.open('xb')"), 'scripts/test/vector-zonal-class-frequency-persistence-online.py': ('independent_storage_fid_verified', 'storage_fids', 'storage_fid_column', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 16', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'vector_persistence.validate_item', 'validate_preview', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_gpkgs', 'len(failures) != 64', 'full_checks != 1160', 'physical_calls != 81'), 'scripts/test/online-hosted-vector-zonal-class-frequency-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-vector-zonal-class-frequency-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'engines/geopython-workflow/test_online_vector_zonal_class_frequency_fixture.py': ('test_sixteen_complete_gpkgs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers'), 'scripts/test/vector-zonal-class-frequency-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_all_fixed_failure_dags_have_two_actual_sources'), 'scripts/utils/online_raster_lineage.py': ("'vector-zonal-class-frequency-persistence': 'vector-zonal-class-frequency-lineage'",), 'Makefile': ('test_online_vector_zonal_class_frequency_fixture.py', '@$(MAKE) test-vector-zonal-class-frequency-online-runner', 'scripts/test/vector-zonal-class-frequency-persistence-online_test.py', 'test_vector_zonal_class_frequency_requires_exact_gpkg_matrix_budget_and_regressions')}

def validate_vector_zonal_class_frequency(repository: Path, registered: set[str]) -> None:
    suite = 'vector-zonal-class-frequency-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  vector-zonal-class-frequency-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in VECTOR_ZONAL_CLASS_FREQUENCY_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-vector-zonal-class-frequency-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES')}
    fixed={'CASES': {'direct.gpkg': ('values.tif', 'zones.gpkg', 'frequency', False, 'zonal_'), 'touched.gpkg': ('values.tif', 'zones.gpkg', 'frequency', True, 'zonal_'), 'holes.gpkg': ('values.tif', 'holes.gpkg', 'frequency', False, 'zonal_'), 'overlap.gpkg': ('values.tif', 'overlap.gpkg', 'frequency', False, 'zonal_'), 'multipart.gpkg': ('values.tif', 'multipart.gpkg', 'frequency', False, 'zonal_'), 'rotated.gpkg': ('rotated.tif', 'rotated.gpkg', 'frequency', False, 'zonal_'), 'geographic.gpkg': ('geographic.tif', 'geographic.gpkg', 'frequency', False, 'zonal_'), 'coverage.gpkg': ('coverage.tif', 'zones.gpkg', 'frequency', False, 'zonal_'), 'cross-block.gpkg': ('large.tif', 'large.gpkg', 'frequency', False, 'zonal_'), 'all-invalid.gpkg': ('invalid.tif', 'zones.gpkg', 'frequency', False, 'zonal_'), 'outside.gpkg': ('values.tif', 'outside.gpkg', 'frequency', False, 'zonal_'), 'empty.gpkg': ('values.tif', 'empty.gpkg', 'frequency', False, 'zonal_'), 'business-fid.gpkg': ('values.tif', 'fid.gpkg', 'frequency', False, 'zonal_'), 'business-uppercase-fid.gpkg': ('values.tif', 'upper-fid.gpkg', 'frequency', False, 'zonal_'), 'business-mixed-fid.gpkg': ('values.tif', 'mixed-fid.gpkg', 'frequency', False, 'custom_'), 'summary.gpkg': ('values.tif', 'zones.gpkg', 'summary', False, 'zonal_')}, 'FAILURES': ('method_null', 'method_unknown', 'percentile_null', 'weights_null', 'weights_band', 'stddev', 'area', 'area_stddev', 'flag_string', 'id_missing', 'id_null', 'id_duplicate', 'id_unknown', 'band_zero', 'band_high', 'band_boolean', 'band_string', 'band_null', 'alpha', 'fractional', 'unsafe', 'different_crs', 'prefix_null', 'collision', 'touched_numeric', 'point', 'invalid_polygon', 'z_geometry', 'summary_id', 'prefix_boolean', 'touched_null', 'complex')}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','raster_sample_lines(', 'raster_vector_zonal_statistics(')):raise RegistrationError('expected oracle must be independent')

RASTER_SAMPLE_LINES_REQUIRED = {'business/scripts/online-raster-sample-lines-fixture.py': ('NFSExport', 'def oracle', 'def verify_vector', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'np.array_equal(band.ReadAsArray(),values,equal_nan=True)', 'storage FID', "with state.open('xb')"), 'scripts/test/raster-sample-lines-persistence-online.py': ('independent_storage_fid_verified', 'storage_fids', 'storage_fid_column', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 16', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'vector_persistence.validate_item', 'validate_preview', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_gpkgs', 'len(failures) != 56', 'full_checks != 1032', 'physical_calls != 73'), 'scripts/test/online-hosted-raster-sample-lines-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-sample-lines-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'engines/geopython-workflow/test_online_raster_sample_lines_fixture.py': ('test_sixteen_complete_gpkgs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers'), 'scripts/test/raster-sample-lines-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_all_fixed_failure_dags_have_two_actual_sources'), 'scripts/utils/online_raster_lineage.py': ("'raster-sample-lines-persistence': 'raster-sample-lines-lineage'",), 'Makefile': ('test_online_raster_sample_lines_fixture.py', '@$(MAKE) test-raster-sample-lines-online-runner', 'scripts/test/raster-sample-lines-persistence-online_test.py', 'test_raster_sample_lines_requires_exact_gpkg_matrix_budget_and_regressions')}

def validate_raster_sample_lines(repository: Path, registered: set[str]) -> None:
    suite = 'raster-sample-lines-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-sample-lines-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in RASTER_SAMPLE_LINES_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-raster-sample-lines-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES')}
    fixed={'CASES': {'cell.gpkg': ('values.tif', 'lines.gpkg', 'direct', 'cell', 1.0), 'bilinear.gpkg': ('values.tif', 'lines.gpkg', 'direct', 'bilinear', 1.0), 'feet.gpkg': ('feet.tif', 'feet.gpkg', 'direct', 'cell', 0.5), 'survey-feet.gpkg': ('survey-feet.tif', 'survey-feet.gpkg', 'direct', 'bilinear', 0.5), 'corners.gpkg': ('values.tif', 'corners.gpkg', 'direct', 'cell', 2.0), 'short.gpkg': ('values.tif', 'short.gpkg', 'direct', 'bilinear', 10.0), 'closed.gpkg': ('values.tif', 'closed.gpkg', 'direct', 'cell', 1.0), 'endpoint.gpkg': ('values.tif', 'endpoint.gpkg', 'direct', 'bilinear', 2.0), 'rotated.gpkg': ('rotated.tif', 'rotated.gpkg', 'direct', 'bilinear', 1.0), 'rectangular.gpkg': ('values.tif', 'lines.gpkg', 'direct', 'cell', 0.5), 'outside.gpkg': ('values.tif', 'outside.gpkg', 'direct', 'bilinear', 2.0), 'nodata.gpkg': ('invalid.tif', 'lines.gpkg', 'direct', 'cell', 1.0), 'coverage.gpkg': ('coverage.tif', 'lines.gpkg', 'direct', 'bilinear', 1.0), 'business-fid.gpkg': ('values.tif', 'fid.gpkg', 'direct', 'cell', 1.0), 'business-uppercase-fid.gpkg': ('values.tif', 'upper-fid.gpkg', 'direct', 'bilinear', 1.0), 'business-mixed-fid.gpkg': ('values.tif', 'mixed-fid.gpkg', 'direct', 'bilinear', 1.0)}, 'FAILURES': ('band_zero', 'band_high', 'band_boolean', 'band_string', 'band_null', 'alpha', 'method_null', 'method_unknown', 'spacing_zero', 'spacing_negative', 'spacing_boolean', 'spacing_string', 'spacing_null', 'spacing_list', 'id_geometry', 'id_null', 'id_unknown', 'id_duplicate', 'different_crs', 'geographic', 'point', 'polygon', 'multiline', 'zero_line', 'z_line', 'collision_distance', 'collision_value', 'collision_status')}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','raster_sample_lines(', 'raster_vector_zonal_statistics(')):raise RegistrationError('expected oracle must be independent')

INTERPOLATE_IDW_REQUIRED = {'business/scripts/online-raster-interpolate-idw-fixture.py': ('NFSExport', 'def oracle', 'def verify_sources', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'np.array_equal(band.ReadAsArray(),a,equal_nan=True)', 'nearest_overviews', "with state.open('xb')"), 'scripts/test/raster-interpolate-idw-persistence-online.py': ('template_mask_ignored_verified', 'archive_response', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 16', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.strict_sizes', 'validate_target_item', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_cogs', 'len(failures) != 36', 'full_checks != 712', 'physical_calls != 53'), 'scripts/test/online-hosted-raster-interpolate-idw-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-interpolate-idw-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'engines/geopython-workflow/test_online_interpolate_idw_fixture.py': ('test_sixteen_complete_cogs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers'), 'scripts/test/raster-interpolate-idw-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_all_fixed_failure_dags_have_two_actual_sources'), 'scripts/utils/online_raster_lineage.py': ("'raster-interpolate-idw-persistence': 'raster-interpolate-idw-lineage'",), 'Makefile': ('test_online_interpolate_idw_fixture.py', '@$(MAKE) test-raster-interpolate-idw-online-runner', 'scripts/test/raster-interpolate-idw-persistence-online_test.py', 'test_interpolate_idw_requires_exact_cog_matrix_budget_and_regressions')}

def validate_interpolate_idw(repository: Path, registered: set[str]) -> None:
    suite = 'raster-interpolate-idw-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-interpolate-idw-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in INTERPOLATE_IDW_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-raster-interpolate-idw-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES')}
    fixed={'CASES': {'base.cog.tif': ('template.tif', 'points.gpkg', 4.0), 'reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0), 'coincident.cog.tif': ('template.tif', 'coincident.gpkg', 4.0), 'boundary.cog.tif': ('template.tif', 'boundary.gpkg', 1.0), 'below.cog.tif': ('template.tif', 'boundary.gpkg', 0.9999999999999999), 'above.cog.tif': ('template.tif', 'boundary.gpkg', 1.0000000000000002), 'outside.cog.tif': ('template.tif', 'outside.gpkg', 1.0), 'partial.cog.tif': ('template.tif', 'boundary.gpkg', 0.125), 'empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0), 'invalid-template.cog.tif': ('invalid.tif', 'points.gpkg', 4.0), 'mask-alpha.cog.tif': ('coverage.tif', 'points.gpkg', 4.0), 'rectangular-block.cog.tif': ('rectangular.tif', 'points.gpkg', 4.0), 'rotated.cog.tif': ('rotated.tif', 'points.gpkg', 4.0), 'sheared.cog.tif': ('sheared.tif', 'points.gpkg', 4.0), 'foot.cog.tif': ('foot.tif', 'foot.gpkg', 1.5), 'survey-foot.cog.tif': ('survey.tif', 'survey.gpkg', 1.5)}, 'FAILURES': ('radius_zero', 'radius_negative', 'radius_boolean', 'radius_string', 'field_missing', 'field_geometry', 'value_null', 'value_string', 'value_unsafe', 'duplicate_equal', 'duplicate_different', 'duplicate_outside', 'null_geometry', 'line', 'multipoint', 'z_point', 'different_crs', 'geographic')}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','interpolate_idw(', 'raster_vector_zonal_statistics(')):raise RegistrationError('expected oracle must be independent')

INTERPOLATE_IDW_MIN_POINTS_REQUIRED = {'business/scripts/online-raster-interpolate-idw-min-points-fixture.py': ('NFSExport', 'def oracle', 'def verify_sources', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'np.array_equal(band.ReadAsArray(),a,equal_nan=True)', 'nearest_overviews', "with state.open('xb')", 'minimum_support_verified', 'actual default-one or point-order COGs differ', 'actual_control_pairs_verified', 'len(terms)>=(1 if minimum is None else minimum)'), 'scripts/test/raster-interpolate-idw-min-points-persistence-online.py': ('template_mask_ignored_verified', 'archive_response', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 16', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.strict_sizes', 'validate_target_item', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_cogs', 'len(failures) != 54', 'full_checks != 1000', 'physical_calls != 71', 'actual_control_pairs_verified', 'minimum_support_verified', "params['min_points']=minimum", 'min_points_fraction', 'min_points_null'), 'scripts/test/online-hosted-raster-interpolate-idw-min-points-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-interpolate-idw-min-points-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'engines/geopython-workflow/test_online_interpolate_idw_min_points_fixture.py': ('test_sixteen_complete_cogs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers', 'test_idw_min_points_frozen_matrix_has_real_threshold_coincidence_and_controls', 'test_minimum_support_oracle_rejects_wrong_shortfall_coincidence_and_boundary', 'timeout=90'), 'scripts/test/raster-interpolate-idw-min-points-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_all_fixed_failure_dags_have_two_actual_sources', 'test_actual_default_one_point_order_and_minimum_proofs_cannot_weaken', 'test_all_nine_minimum_failure_values_are_json_exact_and_not_silently_dropped'), 'scripts/utils/online_raster_lineage.py': ("'raster-interpolate-idw-min-points-persistence': 'raster-interpolate-idw-min-points-lineage'",), 'Makefile': ('test_online_interpolate_idw_min_points_fixture.py', '@$(MAKE) test-raster-interpolate-idw-min-points-online-runner', 'scripts/test/raster-interpolate-idw-min-points-persistence-online_test.py', 'test_interpolate_idw_min_points_requires_exact_cog_matrix_budget_and_regressions')}

def validate_interpolate_idw_min_points(repository: Path, registered: set[str]) -> None:
    suite = 'raster-interpolate-idw-min-points-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-interpolate-idw-min-points-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in INTERPOLATE_IDW_MIN_POINTS_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-raster-interpolate-idw-min-points-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES','CONTROLS')}
    fixed={'CASES': {'base.cog.tif': ('template.tif', 'points.gpkg', 4.0, None), 'one.cog.tif': ('template.tif', 'points.gpkg', 4.0, 1), 'threshold.cog.tif': ('template.tif', 'points.gpkg', 4.0, 3), 'shortfall.cog.tif': ('template.tif', 'points.gpkg', 4.0, 4), 'coincident.cog.tif': ('template.tif', 'coincident.gpkg', 4.0, 10), 'no-support.cog.tif': ('template.tif', 'noncoincident.gpkg', 4.0, 2), 'below.cog.tif': ('one.tif', 'boundary.gpkg', 0.9999999999999999, 2), 'boundary.cog.tif': ('one.tif', 'boundary.gpkg', 1.0, 2), 'above.cog.tif': ('one.tif', 'boundary.gpkg', 1.0000000000000002, 2), 'reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0, 3), 'empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 2), 'mask-alpha.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 2), 'rectangular-block.cog.tif': ('rectangular.tif', 'points.gpkg', 4.0, 2), 'rotated-sheared-block.cog.tif': ('sheared.tif', 'points.gpkg', 4.0, 2), 'foot.cog.tif': ('foot.tif', 'foot.gpkg', 1.5, 2), 'survey-foot.cog.tif': ('survey.tif', 'survey.gpkg', 1.5, 2)}, 'CONTROLS': (('base.cog.tif', 'one.cog.tif'), ('threshold.cog.tif', 'reversed.cog.tif')), 'FAILURES': ('radius_zero', 'radius_negative', 'radius_boolean', 'radius_string', 'field_missing', 'field_geometry', 'value_null', 'value_string', 'value_unsafe', 'duplicate_equal', 'duplicate_different', 'duplicate_outside', 'null_geometry', 'line', 'multipoint', 'z_point', 'different_crs', 'geographic', 'min_points_null', 'min_points_true', 'min_points_false', 'min_points_zero', 'min_points_negative', 'min_points_fraction', 'min_points_string', 'min_points_array', 'min_points_object')}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','interpolate_idw(', 'raster_vector_zonal_statistics(')):raise RegistrationError('expected oracle must be independent')

INTERPOLATE_IDW_MAX_POINTS_REQUIRED = {'business/scripts/online-raster-interpolate-idw-max-points-fixture.py': ('NFSExport', 'def oracle', 'def verify_sources', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'np.array_equal(band.ReadAsArray(),a,equal_nan=True)', 'nearest_overviews', "with state.open('xb')", 'minimum_support_verified', 'actual unlimited or point-order COGs differ', 'actual_control_pairs_verified', 'terms=sorted(terms)', 'terms=terms[:maximum]', 'len(terms)>=minimum', 'nearest_support_verified'), 'scripts/test/raster-interpolate-idw-max-points-persistence-online.py': ('template_mask_ignored_verified', 'archive_response', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 16', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.strict_sizes', 'validate_target_item', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_cogs', 'len(failures) != 78', 'full_checks != 1384', 'physical_calls != 95', 'actual_control_pairs_verified', 'minimum_support_verified', "params['min_points']=minimum", 'min_points_fraction', 'min_points_null', 'nearest_support_verified', "params['max_points']=maximum", 'max_points_fraction', 'max_points_null', 'point_counts_empty', 'point_counts_coincident'), 'scripts/test/online-hosted-raster-interpolate-idw-max-points-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-interpolate-idw-max-points-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'engines/geopython-workflow/test_online_interpolate_idw_max_points_fixture.py': ('test_sixteen_complete_cogs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers', 'timeout=90', 'test_idw_max_points_frozen_matrix_has_real_distance_x_y_selection_and_controls', 'test_nearest_support_oracle_rejects_wrong_selection_shortfall_coincidence_and_boundary', 'test_nearest_saved_cog_rejects_specific_wrong_distance_or_tie_choices', "monkeypatch.setattr(heapq,'nsmallest',forbidden)"), 'scripts/test/raster-interpolate-idw-max-points-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_all_fixed_failure_dags_have_two_actual_sources', 'test_actual_unlimited_point_order_and_nearest_proofs_cannot_weaken', 'test_all_nine_minimum_failure_values_are_json_exact_and_not_silently_dropped', 'test_nine_maximum_values_and_three_conflicts_are_json_exact'), 'scripts/utils/online_raster_lineage.py': ("'raster-interpolate-idw-max-points-persistence': 'raster-interpolate-idw-max-points-lineage'",), 'Makefile': ('test_online_interpolate_idw_max_points_fixture.py', '@$(MAKE) test-raster-interpolate-idw-max-points-online-runner', 'scripts/test/raster-interpolate-idw-max-points-persistence-online_test.py', 'test_interpolate_idw_max_points_requires_exact_cog_matrix_budget_and_regressions')}

def validate_interpolate_idw_max_points(repository: Path, registered: set[str]) -> None:
    suite = 'raster-interpolate-idw-max-points-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-interpolate-idw-max-points-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in INTERPOLATE_IDW_MAX_POINTS_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-raster-interpolate-idw-max-points-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES','CONTROLS','RASTERS','VECTORS')}
    fixed={'CASES': {'base.cog.tif': ('template.tif', 'points.gpkg', 4.0, 1, None), 'unlimited.cog.tif': ('template.tif', 'points.gpkg', 4.0, 1, 100), 'nearest-one.cog.tif': ('template.tif', 'ties.gpkg', 4.0, 1, 1), 'nearest-two.cog.tif': ('template.tif', 'ties.gpkg', 4.0, 2, 2), 'reversed.cog.tif': ('template.tif', 'ties-reversed.gpkg', 4.0, 2, 2), 'below.cog.tif': ('one.tif', 'boundary.gpkg', 0.9999999999999999, 2, 2), 'boundary.cog.tif': ('one.tif', 'boundary.gpkg', 1.0, 2, 2), 'above.cog.tif': ('one.tif', 'boundary.gpkg', 1.0000000000000002, 2, 2), 'coincident.cog.tif': ('template.tif', 'coincident.gpkg', 4.0, 10, 10), 'shortfall.cog.tif': ('template.tif', 'noncoincident.gpkg', 4.0, 2, 2), 'empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 1, 3), 'mask-alpha.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 1, 2), 'rectangular-block.cog.tif': ('rectangular.tif', 'points.gpkg', 4.0, 1, 2), 'rotated-sheared-block.cog.tif': ('sheared.tif', 'points.gpkg', 4.0, 1, 2), 'foot.cog.tif': ('foot.tif', 'foot.gpkg', 1.5, 1, 2), 'survey-foot.cog.tif': ('survey.tif', 'survey.gpkg', 1.5, 1, 2)}, 'CONTROLS': (('base.cog.tif', 'unlimited.cog.tif'), ('nearest-two.cog.tif', 'reversed.cog.tif')), 'FAILURES': ('radius_zero', 'radius_negative', 'radius_boolean', 'radius_string', 'field_missing', 'field_geometry', 'value_null', 'value_string', 'value_unsafe', 'duplicate_equal', 'duplicate_different', 'duplicate_outside', 'null_geometry', 'line', 'multipoint', 'z_point', 'different_crs', 'geographic', 'min_points_null', 'min_points_true', 'min_points_false', 'min_points_zero', 'min_points_negative', 'min_points_fraction', 'min_points_string', 'min_points_array', 'min_points_object', 'max_points_null', 'max_points_true', 'max_points_false', 'max_points_zero', 'max_points_negative', 'max_points_fraction', 'max_points_string', 'max_points_array', 'max_points_object', 'point_counts', 'point_counts_empty', 'point_counts_coincident'), 'RASTERS': ('template.tif', 'invalid.tif', 'coverage.tif', 'rectangular.tif', 'rotated.tif', 'sheared.tif', 'foot.tif', 'survey.tif', 'geographic.tif', 'one.tif'), 'VECTORS': ('points.gpkg', 'reversed.gpkg', 'coincident.gpkg', 'boundary.gpkg', 'outside.gpkg', 'empty.gpkg', 'foot.gpkg', 'survey.gpkg', 'value_null.gpkg', 'value_string.gpkg', 'value_unsafe.gpkg', 'duplicate_equal.gpkg', 'duplicate_different.gpkg', 'duplicate_outside.gpkg', 'null_geometry.gpkg', 'line.gpkg', 'multipoint.gpkg', 'z_point.gpkg', 'different_crs.gpkg', 'geographic.gpkg', 'noncoincident.gpkg', 'ties.gpkg', 'ties-reversed.gpkg')}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','interpolate_idw(', 'raster_vector_zonal_statistics(')):raise RegistrationError('expected oracle must be independent')


INTERPOLATE_IDW_POWER_REQUIRED = {'scripts/test/online-hosted-raster-interpolate-idw-power-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-interpolate-idw-power-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'scripts/utils/online_raster_lineage.py': ("'raster-interpolate-idw-power-persistence': 'raster-interpolate-idw-power-lineage'",), 'Makefile': ('test_online_interpolate_idw_power_fixture.py', '@$(MAKE) test-raster-interpolate-idw-power-online-runner', 'scripts/test/raster-interpolate-idw-power-persistence-online_test.py', 'test_interpolate_idw_power_requires_exact_cog_matrix_budget_and_regressions'), 'business/scripts/online-raster-interpolate-idw-power-fixture.py': ('NFSExport', 'def oracle', 'def verify_sources', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'nearest_overviews', "with state.open('xb')", 'actual_control_pairs_verified', 'actual_power_witness_verified', 'actual distance-power analytic witness differs', 'context.prec=2000', 'decimal(dist).sqrt()**power', 'power=2 if power is None else power', 'terms.sort()', 'maximum is not None', 'dist<=limit', 'previous.shape[1]>shape[0]', 'nearest_support_verified', 'minimum_support_verified', 'distance_power_verified', 'power_parameter_omitted'), 'scripts/test/raster-interpolate-idw-power-persistence-online.py': ('template_mask_ignored_verified', 'archive_response', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 24', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.strict_sizes', 'validate_target_item', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_cogs', 'len(failures) != 80', 'full_checks != 2220', 'physical_calls != 105', "'operator':'raster_interpolate_idw'", "'task': 'interpolate'", 'POWER_FAILURES', "params['power']=power", 'actual_power_witness_verified', 'distance_power_verified', 'power_parameter_omitted'), 'engines/geopython-workflow/test_online_interpolate_idw_power_fixture.py': ('test_twenty_four_complete_cogs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers', 'timeout=90', 'test_power_matrix_controls_analytic_values_support_boundaries_units_and_tails', 'test_saved_power_cogs_reject_specific_value_support_mask_and_default_corruption', 'test_input_schema_exact_bad_values_and_radius_witnesses_survive_file_load', 'test_p2_expected_grid_uses_original_exact_fraction_formula', "'_idw_radical_mean'", "'_idw_inverse_root_bounds'"), 'scripts/test/raster-interpolate-idw-power-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_all_fixed_failure_dags_have_two_actual_sources', 'test_actual_unlimited_point_order_and_nearest_proofs_cannot_weaken', '(24,80,2220,105)', "glob('*.json'))),208)", 'test_ten_power_failures_preserve_exact_json_values', 'test_three_powers_keep_limits_conflicts_and_distinct_sources', 'test_default_two_actual_control_differs_only_in_power_and_target_name', 'test_power_and_actual_witness_proofs_cannot_weaken')}

def validate_interpolate_idw_power(repository: Path, registered: set[str]) -> None:
    suite = 'raster-interpolate-idw-power-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-interpolate-idw-power-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in INTERPOLATE_IDW_POWER_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-raster-interpolate-idw-power-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES','CONTROLS','RASTERS','VECTORS')}
    fixed={'CASES': {'p1-base.cog.tif': ('template.tif', 'points.gpkg', 4.0, 1, None, 1), 'p1-reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0, 1, None, 1), 'p1-nearest.cog.tif': ('profile.tif', 'ties.gpkg', 1.0, 2, 2, 1), 'p1-coincident.cog.tif': ('template.tif', 'coincident.gpkg', 4.0, 10, 10, 1), 'p1-empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 1, None, 1), 'p1-masked.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 1, None, 1), 'p1-foot-block.cog.tif': ('rectangular.tif', 'foot.gpkg', 10000.0, 1, 2, 1), 'p1-survey-block.cog.tif': ('sheared.tif', 'survey.gpkg', 10000.0, 1, 2, 1), 'p2-default.cog.tif': ('template.tif', 'points.gpkg', 4.0, 1, None, None), 'p2-explicit.cog.tif': ('template.tif', 'points.gpkg', 4.0, 1, None, 2), 'p2-nearest.cog.tif': ('profile.tif', 'ties.gpkg', 1.0, 2, 2, 2), 'p2-coincident.cog.tif': ('template.tif', 'coincident.gpkg', 4.0, 10, 10, 2), 'p2-empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 1, None, 2), 'p2-masked.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 1, None, 2), 'p2-foot-block.cog.tif': ('rectangular.tif', 'foot.gpkg', 10000.0, 1, 2, 2), 'p2-survey-block.cog.tif': ('sheared.tif', 'survey.gpkg', 10000.0, 1, 2, 2), 'p3-base.cog.tif': ('template.tif', 'points.gpkg', 4.0, 1, None, 3), 'p3-reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0, 1, None, 3), 'p3-nearest.cog.tif': ('profile.tif', 'ties.gpkg', 1.0, 2, 2, 3), 'p3-coincident.cog.tif': ('template.tif', 'coincident.gpkg', 4.0, 10, 10, 3), 'p3-empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 1, None, 3), 'p3-masked.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 1, None, 3), 'p3-foot-block.cog.tif': ('rectangular.tif', 'foot.gpkg', 10000.0, 1, 2, 3), 'p3-survey-block.cog.tif': ('sheared.tif', 'survey.gpkg', 10000.0, 1, 2, 3)}, 'CONTROLS': (('p1-base.cog.tif', 'p1-reversed.cog.tif'), ('p1-base.cog.tif', 'p1-masked.cog.tif'), ('p2-default.cog.tif', 'p2-explicit.cog.tif'), ('p2-default.cog.tif', 'p2-masked.cog.tif'), ('p3-base.cog.tif', 'p3-reversed.cog.tif'), ('p3-base.cog.tif', 'p3-masked.cog.tif')), 'FAILURES': ('power_null', 'power_true', 'power_false', 'power_zero', 'power_negative', 'power_large', 'power_fraction', 'power_string', 'power_array', 'power_object', 'p1_min_zero', 'p1_max_zero', 'p1_counts_regular', 'p1_counts_empty', 'p1_counts_coincident', 'p1_duplicate_outside', 'p1_outside_null', 'p1_unselected_null', 'p1_line', 'p1_different_crs', 'p2_min_zero', 'p2_max_zero', 'p2_counts_regular', 'p2_counts_empty', 'p2_counts_coincident', 'p2_duplicate_outside', 'p2_outside_null', 'p2_unselected_null', 'p2_line', 'p2_different_crs', 'p3_min_zero', 'p3_max_zero', 'p3_counts_regular', 'p3_counts_empty', 'p3_counts_coincident', 'p3_duplicate_outside', 'p3_outside_null', 'p3_unselected_null', 'p3_line', 'p3_different_crs'), 'RASTERS': ('template.tif', 'coverage.tif', 'profile.tif', 'rectangular.tif', 'sheared.tif'), 'VECTORS': ('points.gpkg', 'reversed.gpkg', 'coincident.gpkg', 'empty.gpkg', 'foot.gpkg', 'survey.gpkg', 'ties.gpkg', 'duplicate_outside.gpkg', 'outside_null.gpkg', 'unselected_null.gpkg', 'line.gpkg', 'different_crs.gpkg')}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    runner_tree=ast.parse((repository/'scripts/test/raster-interpolate-idw-power-persistence-online.py').read_text())
    maps=[node.value for node in runner_tree.body if isinstance(node,ast.Assign)and any(isinstance(t,ast.Name)and t.id=='POWER_FAILURES'for t in node.targets)]
    fixed_power={'power_null':None,'power_true':True,'power_false':False,'power_zero':0,'power_negative':-1,'power_large':4,'power_fraction':1.5,'power_string':'2','power_array':[],'power_object':{}}
    if len(maps)!=1 or repr(ast.literal_eval(maps[0]))!=repr(fixed_power):raise RegistrationError('fixed exact power failure values changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','interpolate_idw(', 'raster_vector_zonal_statistics(')):raise RegistrationError('expected oracle must be independent')

POINT_DENSITY_REQUIRED = {'business/scripts/online-raster-point-density-fixture.py': ('NFSExport', 'def oracle', 'def verify_sources', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'np.array_equal(band.ReadAsArray(),a,equal_nan=True)', 'nearest_overviews', "with state.open('xb')", 'point_count_and_full_circle_area_verified', 'unit_points_per_square_metre_verified', 'actual_control_pairs_verified', 'actual_scale_control_verified', 'actual triplicated point density differs', 'Q(math.pi)*limit', 'count=sum(', "band.GetUnitType()!='points/m2'", 'SCALE_CONTROL'), 'scripts/test/raster-point-density-persistence-online.py': ('template_mask_ignored_verified', 'archive_response', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 16', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.strict_sizes', 'validate_target_item', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_cogs', 'len(failures) != 32', 'full_checks != 648', 'physical_calls != 49', 'actual_scale_control_verified', 'unit_points_per_square_metre_verified', 'radius_null', 'radius_object', "'operator':'raster_point_density'", "'task': 'density'"), 'scripts/test/online-hosted-raster-point-density-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-point-density-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'engines/geopython-workflow/test_online_point_density_fixture.py': ('test_sixteen_complete_cogs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers', 'test_density_frozen_matrix_exact_boundaries_duplicates_units_zero_and_actual_controls', 'test_density_oracle_rejects_specific_wrong_count_area_boundary_zero_and_unit', 'timeout=90'), 'scripts/test/raster-point-density-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_all_fixed_failure_dags_have_two_actual_sources', 'test_actual_point_order_template_and_duplicate_controls_cannot_weaken', 'test_all_eight_radius_failure_values_are_json_exact_and_not_silently_dropped', '(16,32,648,49)', "glob('*.json'))),96)"), 'scripts/utils/online_raster_lineage.py': ("'raster-point-density-persistence': 'raster-point-density-lineage'",), 'Makefile': ('test_online_point_density_fixture.py', '@$(MAKE) test-raster-point-density-online-runner', 'scripts/test/raster-point-density-persistence-online_test.py', 'test_point_density_requires_exact_cog_matrix_budget_and_regressions')}

def validate_point_density(repository: Path, registered: set[str]) -> None:
    suite = 'raster-point-density-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-point-density-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in POINT_DENSITY_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-raster-point-density-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES','CONTROLS','SCALE_CONTROL','RASTERS','VECTORS')}
    fixed={'CASES': {'base.cog.tif': ('template.tif', 'points.gpkg', 4.0), 'reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0), 'triplicated.cog.tif': ('template.tif', 'triplicated.gpkg', 4.0), 'coincident.cog.tif': ('template.tif', 'coincident.gpkg', 4.0), 'below.cog.tif': ('one.tif', 'boundary.gpkg', 0.9999999999999999), 'boundary.cog.tif': ('one.tif', 'boundary.gpkg', 1.0), 'above.cog.tif': ('one.tif', 'boundary.gpkg', 1.0000000000000002), 'outside.cog.tif': ('template.tif', 'outside.gpkg', 1.0), 'partial.cog.tif': ('template.tif', 'points.gpkg', 0.5), 'empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0), 'invalid-template.cog.tif': ('invalid.tif', 'points.gpkg', 4.0), 'mask-alpha.cog.tif': ('coverage.tif', 'points.gpkg', 4.0), 'rectangular-block.cog.tif': ('rectangular.tif', 'points.gpkg', 4.0), 'rotated-sheared-block.cog.tif': ('sheared.tif', 'points.gpkg', 4.0), 'foot.cog.tif': ('foot.tif', 'foot.gpkg', 1.5), 'survey-foot.cog.tif': ('survey.tif', 'survey.gpkg', 1.5)}, 'CONTROLS': (('base.cog.tif', 'reversed.cog.tif'), ('base.cog.tif', 'invalid-template.cog.tif'), ('base.cog.tif', 'mask-alpha.cog.tif')), 'SCALE_CONTROL': ('base.cog.tif', 'triplicated.cog.tif', 3), 'FAILURES': ('radius_null', 'radius_true', 'radius_false', 'radius_zero', 'radius_negative', 'radius_string', 'radius_array', 'radius_object', 'null_geometry', 'empty_geometry', 'line', 'multipoint', 'z_point', 'outside_z_point', 'different_crs', 'geographic'), 'RASTERS': ('template.tif', 'invalid.tif', 'coverage.tif', 'rectangular.tif', 'sheared.tif', 'foot.tif', 'survey.tif', 'geographic.tif', 'one.tif'), 'VECTORS': ('points.gpkg', 'reversed.gpkg', 'triplicated.gpkg', 'coincident.gpkg', 'boundary.gpkg', 'outside.gpkg', 'empty.gpkg', 'foot.gpkg', 'survey.gpkg', 'null_geometry.gpkg', 'empty_geometry.gpkg', 'line.gpkg', 'multipoint.gpkg', 'z_point.gpkg', 'outside_z_point.gpkg', 'different_crs.gpkg', 'geographic.gpkg')}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','raster_point_density(', 'STRtree(', '_circle_sites(')):raise RegistrationError('expected oracle must be independent')


LINE_DENSITY_REQUIRED = {'scripts/test/online-hosted-raster-line-density-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-line-density-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'scripts/utils/online_raster_lineage.py': ("'raster-line-density-persistence': 'raster-line-density-lineage'",), 'Makefile': ('test_online_line_density_fixture.py', '@$(MAKE) test-raster-line-density-online-runner', 'scripts/test/raster-line-density-persistence-online_test.py', 'test_line_density_requires_exact_cog_matrix_budget_and_regressions'), 'business/scripts/online-raster-line-density-fixture.py': ('NFSExport', 'def oracle', 'def verify_sources', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'nearest_overviews', "with state.open('xb')", 'line_length_and_full_circle_area_verified', 'unit_metres_per_square_metre_verified', 'actual_control_pairs_verified', 'actual_scale_controls_verified', 'actual doubled or retraced line density differs', 'context.prec=2000', 'delta=b*b-4*a*c', 'total+=(right-left)*a.sqrt()*factor', "band.GetUnitType()!='metre/m2'", 'SCALE_CONTROLS', 'selected=previous if previous.shape[1]>shape[0] else expected', 'bytes(actual.ExportToWkb())!=geometry.wkb'), 'scripts/test/raster-line-density-persistence-online.py': ('template_mask_ignored_verified', 'archive_response', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 16', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.strict_sizes', 'validate_target_item', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_cogs', 'len(failures) != 40', 'full_checks != 776', 'physical_calls != 57', 'actual_scale_controls_verified', 'unit_metres_per_square_metre_verified', 'radius_null', 'radius_object', "'operator':'raster_line_density'", "'task': 'density'"), 'engines/geopython-workflow/test_online_line_density_fixture.py': ('test_sixteen_complete_cogs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers', 'test_line_density_frozen_matrix_exact_boundaries_multiplicities_units_tails_and_controls', 'test_line_density_oracle_rejects_specific_length_circle_boundary_zero_unit_and_tail_errors', 'test_all_abnormal_geometries_and_boundary_wkb_survive_geopackage_without_normalization', 'timeout=90', "'_line_density_float'", "'_line_density_segment'", "'_rational_sqrt_bounds'"), 'scripts/test/raster-line-density-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_all_fixed_failure_dags_have_two_actual_sources', 'test_actual_point_order_template_and_duplicate_controls_cannot_weaken', 'test_all_eight_radius_failure_values_are_json_exact_and_not_silently_dropped', '(16,40,776,57)', "glob('*.json'))),112)", 'test_all_twenty_failure_sources_keep_accurate_geometry_or_crs_roles', 'test_both_doubled_and_retraced_actual_saved_controls_are_required')}

def validate_line_density(repository: Path, registered: set[str]) -> None:
    suite = 'raster-line-density-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-line-density-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in LINE_DENSITY_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-raster-line-density-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES','CONTROLS','SCALE_CONTROLS','RASTERS','VECTORS')}
    fixed={'CASES': {'base.cog.tif': ('template.tif', 'lines.gpkg', 4.0), 'reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0), 'doubled.cog.tif': ('template.tif', 'doubled.gpkg', 4.0), 'retraced.cog.tif': ('template.tif', 'retraced.gpkg', 4.0), 'multipart.cog.tif': ('template.tif', 'multipart.gpkg', 4.0), 'subdivided.cog.tif': ('template.tif', 'subdivided.gpkg', 4.0), 'overlap.cog.tif': ('template.tif', 'overlap.gpkg', 4.0), 'tangent.cog.tif': ('one.tif', 'tangent.gpkg', 1.0), 'inside-tangent.cog.tif': ('one.tif', 'inside-tangent.gpkg', 1.0), 'outside-tangent.cog.tif': ('one.tif', 'outside-tangent.gpkg', 1.0), 'endpoint.cog.tif': ('one.tif', 'endpoint.gpkg', 1.0), 'outside.cog.tif': ('template.tif', 'outside.gpkg', 4.0), 'empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0), 'mask-alpha.cog.tif': ('coverage.tif', 'lines.gpkg', 4.0), 'foot-block.cog.tif': ('rectangular.tif', 'foot.gpkg', 4.0), 'survey-block.cog.tif': ('sheared.tif', 'survey.gpkg', 4.0)}, 'CONTROLS': (('base.cog.tif', 'reversed.cog.tif'), ('base.cog.tif', 'multipart.cog.tif'), ('base.cog.tif', 'subdivided.cog.tif'), ('base.cog.tif', 'mask-alpha.cog.tif'), ('tangent.cog.tif', 'outside-tangent.cog.tif'), ('tangent.cog.tif', 'endpoint.cog.tif'), ('outside.cog.tif', 'empty.cog.tif')), 'SCALE_CONTROLS': (('base.cog.tif', 'doubled.cog.tif', 2), ('base.cog.tif', 'retraced.cog.tif', 2)), 'FAILURES': ('radius_null', 'radius_true', 'radius_false', 'radius_zero', 'radius_negative', 'radius_string', 'radius_array', 'radius_object', 'null_geometry', 'empty_geometry', 'point', 'polygon', 'collection', 'zero_line', 'zero_component', 'z_line', 'outside_z_line', 'outside_zero_line', 'different_crs', 'geographic'), 'RASTERS': ('template.tif', 'coverage.tif', 'rectangular.tif', 'sheared.tif', 'geographic.tif', 'one.tif'), 'VECTORS': ('lines.gpkg', 'reversed.gpkg', 'doubled.gpkg', 'retraced.gpkg', 'multipart.gpkg', 'subdivided.gpkg', 'overlap.gpkg', 'tangent.gpkg', 'inside-tangent.gpkg', 'outside-tangent.gpkg', 'endpoint.gpkg', 'outside.gpkg', 'empty.gpkg', 'foot.gpkg', 'survey.gpkg', 'null_geometry.gpkg', 'empty_geometry.gpkg', 'point.gpkg', 'polygon.gpkg', 'collection.gpkg', 'zero_line.gpkg', 'zero_component.gpkg', 'z_line.gpkg', 'outside_z_line.gpkg', 'outside_zero_line.gpkg', 'different_crs.gpkg', 'geographic.gpkg')}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','raster_line_density(', 'STRtree(', '_circle_sites(', '_line_density_segment(', '_line_density_float(', '_rational_sqrt_bounds(')):raise RegistrationError('expected oracle must be independent')


LINE_DENSITY_WEIGHTED_REQUIRED = {'scripts/test/online-hosted-raster-line-density-weighted-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-line-density-weighted-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'scripts/utils/online_raster_lineage.py': ("'raster-line-density-weighted-persistence': 'raster-line-density-weighted-lineage'",), 'Makefile': ('test_online_line_density_weighted_fixture.py', '@$(MAKE) test-raster-line-density-weighted-online-runner', 'scripts/test/raster-line-density-weighted-persistence-online_test.py', 'test_line_density_weighted_requires_exact_cog_matrix_budget_and_regressions'), 'business/scripts/online-raster-line-density-weighted-fixture.py': ('NFSExport', 'def oracle', 'def verify_sources', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'nearest_overviews', "with state.open('xb')", 'line_length_and_full_circle_area_verified', 'line_density_units_verified', 'actual_control_pairs_verified', 'actual_scale_controls_verified', 'actual scaled or retraced weighted line density differs', 'context.prec=2000', 'delta=b*b-4*a*c', 'if a:segments.append((ax,ay,dx,dy,a,a.sqrt(),mass))', 'root=square_root(delta)', 'total+=mass*(right-left)*length*factor', 'band.GetUnitType()!=output_unit(name)', 'SCALE_CONTROLS', 'selected=previous if previous.shape[1]>shape[0] else expected', 'bytes(actual.ExportToWkb())!=geometry.wkb', 'unnormalized_weights_verified', 'weight_field_parameter_omitted', 'def output_unit', 'def vector_schema', 'decimal(1 if field is None else props[field])'), 'scripts/test/raster-line-density-weighted-persistence-online.py': ('template_mask_ignored_verified', 'archive_response', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 16', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.strict_sizes', 'validate_target_item', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_cogs', 'len(failures) != 40', 'full_checks != 776', 'physical_calls != 57', 'actual_scale_controls_verified', 'line_density_units_verified', 'field_null', 'field_object', "'operator':'raster_line_density'", "'task': 'density'", 'FIELD_FAILURES', "params['weight_field']=FIELD_FAILURES[invalid]", 'weighted field/omission/unit proof differs', "'unnormalized_weights_verified'", 'fixture.output_unit(n)'), 'engines/geopython-workflow/test_online_line_density_weighted_fixture.py': ('test_sixteen_complete_cogs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers', 'test_weighted_line_density_frozen_matrix_units_tails_and_all_actual_controls', 'test_weighted_oracle_rejects_specific_weight_normalization_boundary_unit_and_tail_errors', 'test_all_weight_fields_and_zero_weight_invalid_geometries_survive_real_source_loading', 'timeout=90', "'_line_density_float'", "'_line_density_segment'", "'_rational_sqrt_bounds'", "'_density_weights'", 'loaded.geometry.name', 'band.GetOverview(band.GetOverviewCount()-1)'), 'scripts/test/raster-line-density-weighted-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_all_fixed_failure_dags_have_two_actual_sources', 'test_actual_point_order_template_and_duplicate_controls_cannot_weaken', 'test_all_nine_weight_field_failure_values_are_json_exact_and_not_silently_dropped', '(16,40,776,57)', "glob('*.json'))),112)", 'test_all_twenty_failure_sources_keep_accurate_weight_geometry_and_crs_roles', 'test_both_doubled_and_retraced_actual_saved_controls_are_required', 'test_weight_selector_units_omission_and_unnormalized_proof_cannot_weaken')}

def validate_line_density_weighted(repository: Path, registered: set[str]) -> None:
    suite = 'raster-line-density-weighted-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-line-density-weighted-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in LINE_DENSITY_WEIGHTED_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-raster-line-density-weighted-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES','CONTROLS','SCALE_CONTROLS','RASTERS','VECTORS')}
    fixed={'CASES': {'omitted.cog.tif': ('template.tif', 'lines.gpkg', 4.0, None), 'ones.cog.tif': ('template.tif', 'lines.gpkg', 4.0, 'ones'), 'mixed.cog.tif': ('template.tif', 'lines.gpkg', 4.0, 'mass'), 'reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0, 'mass'), 'integer.cog.tif': ('template.tif', 'integer.gpkg', 4.0, 'mass'), 'replicated.cog.tif': ('template.tif', 'replicated.gpkg', 4.0, None), 'scaled.cog.tif': ('template.tif', 'lines.gpkg', 4.0, 'scaled'), 'retraced.cog.tif': ('template.tif', 'retraced.gpkg', 4.0, 'mass'), 'multipart.cog.tif': ('template.tif', 'multipart.gpkg', 4.0, 'mass'), 'subdivided.cog.tif': ('template.tif', 'subdivided.gpkg', 4.0, 'mass'), 'zero.cog.tif': ('template.tif', 'lines.gpkg', 4.0, 'zero'), 'empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 'mass'), 'mask-alpha.cog.tif': ('coverage.tif', 'lines.gpkg', 4.0, 'mass'), 'boundary-profile.cog.tif': ('profile.tif', 'profile.gpkg', 1.0, 'mass'), 'foot-block.cog.tif': ('rectangular.tif', 'foot.gpkg', 4.0, 'mass'), 'survey-block.cog.tif': ('sheared.tif', 'survey.gpkg', 4.0, 'mass')}, 'CONTROLS': (('omitted.cog.tif', 'ones.cog.tif'), ('mixed.cog.tif', 'reversed.cog.tif'), ('integer.cog.tif', 'replicated.cog.tif'), ('mixed.cog.tif', 'multipart.cog.tif'), ('mixed.cog.tif', 'subdivided.cog.tif'), ('mixed.cog.tif', 'mask-alpha.cog.tif'), ('zero.cog.tif', 'empty.cog.tif')), 'SCALE_CONTROLS': (('mixed.cog.tif', 'scaled.cog.tif', 8), ('mixed.cog.tif', 'retraced.cog.tif', 2)), 'FAILURES': ('field_null', 'field_empty', 'field_blank', 'field_bool', 'field_number', 'field_array', 'field_object', 'field_missing', 'field_geometry', 'missing', 'negative', 'string', 'outside_missing', 'outside_negative', 'zero_point', 'zero_line', 'zero_component', 'zero_outside_z', 'zero_crs', 'zero_radius'), 'RASTERS': ('template.tif', 'coverage.tif', 'profile.tif', 'rectangular.tif', 'sheared.tif'), 'VECTORS': ('lines.gpkg', 'reversed.gpkg', 'integer.gpkg', 'replicated.gpkg', 'retraced.gpkg', 'multipart.gpkg', 'subdivided.gpkg', 'empty.gpkg', 'profile.gpkg', 'foot.gpkg', 'survey.gpkg', 'missing.gpkg', 'negative.gpkg', 'string.gpkg', 'outside_missing.gpkg', 'outside_negative.gpkg', 'zero_point.gpkg', 'zero_line.gpkg', 'zero_component.gpkg', 'zero_outside_z.gpkg', 'zero_crs.gpkg')}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','raster_line_density(', 'STRtree(', '_circle_sites(', '_line_density_segment(', '_line_density_float(', '_rational_sqrt_bounds(', '_density_weights(')):raise RegistrationError('expected oracle must be independent')

    runner_tree=ast.parse((repository/'scripts/test/raster-line-density-weighted-persistence-online.py').read_text())
    fields=[n.value for n in runner_tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id=='FIELD_FAILURES'for t in n.targets)]
    if len(fields)!=1 or repr(ast.literal_eval(fields[0]))!="{'field_null': None, 'field_empty': '', 'field_blank': ' ', 'field_bool': True, 'field_number': 1, 'field_array': [], 'field_object': {}, 'field_missing': 'absent', 'field_geometry': 'shape'}":raise RegistrationError('fixed typed weight-field failures changed')


LINE_KERNEL_DENSITY_REQUIRED = {'scripts/test/online-hosted-raster-line-kernel-density-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-line-kernel-density-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'scripts/utils/online_raster_lineage.py': ("'raster-line-kernel-density-persistence': 'raster-line-kernel-density-lineage'",), 'Makefile': ('test_online_line_kernel_density_fixture.py', '@$(MAKE) test-raster-line-kernel-density-online-runner', 'scripts/test/raster-line-kernel-density-persistence-online_test.py', 'test_line_kernel_density_requires_exact_cog_matrix_budget_and_regressions'), 'business/scripts/online-raster-line-kernel-density-fixture.py': ('NFSExport', 'def oracle', 'def verify_sources', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'nearest_overviews', "with state.open('xb')", 'line_length_and_full_circle_area_verified', 'line_density_units_verified', 'actual_control_pairs_verified', 'actual_scale_controls_verified', 'actual scaled or retraced weighted line density differs', 'context.prec=2000', 'delta=b*b-4*a*(c-r2)', 'total+=mass*length*factor*area', 'band.GetUnitType()!=output_unit(name)', 'SCALE_CONTROLS', 'selected=previous if previous.shape[1]>shape[0] else expected', 'bytes(actual.ExportToWkb())!=geometry.wkb', 'unnormalized_weights_verified', 'weight_field_parameter_omitted', 'def output_unit', 'def vector_schema', 'decimal(1 if field is None else props[field])', 'METHOD_CONTROLS', 'line_density_method_verified', 'method_parameter_omitted', 'actual_method_controls_verified', 'uniform and quartic actual COGs must differ', '@lru_cache(maxsize=4096)', 'area=3*sum(co*(right**i-left**i)', 'from functools import lru_cache'), 'scripts/test/raster-line-kernel-density-persistence-online.py': ('template_mask_ignored_verified', 'archive_response', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 24', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.strict_sizes', 'validate_target_item', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_cogs', 'len(failures) != 60', 'full_checks != 1740', 'physical_calls != 85', 'actual_scale_controls_verified', 'line_density_units_verified', 'field_null', 'field_object', "'operator':'raster_line_density'", "'task': 'density'", 'FIELD_FAILURES', "params['weight_field']=FIELD_FAILURES[invalid]", 'weighted field/omission/unit proof differs', "'unnormalized_weights_verified'", 'fixture.output_unit(n)', 'METHOD_FAILURES', "params['method']=METHOD_FAILURES[invalid]", 'method selector/omission proof differs', 'actual uniform/quartic witness missing', "'line_density_method_verified'"), 'engines/geopython-workflow/test_online_line_kernel_density_fixture.py': ('test_twenty_four_complete_cogs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers', 'test_weighted_line_density_frozen_matrix_units_tails_and_all_actual_controls', 'test_weighted_oracle_rejects_specific_weight_normalization_boundary_unit_and_tail_errors', 'test_all_weight_fields_and_zero_weight_invalid_geometries_survive_real_source_loading', 'timeout=90', "'_line_density_float'", "'_line_density_segment'", "'_rational_sqrt_bounds'", "'_density_weights'", 'loaded.geometry.name', 'band.GetOverview(band.GetOverviewCount()-1)', 'test_kernel_definition_keeps_actual_uniform_defaults_and_explicit_quartic_binding', 'test_kernel_oracle_cannot_accept_uniform_values_as_quartic_results', "expected['diameter.cog.tif'][0][0]", 'f.METHOD_CONTROLS'), 'scripts/test/raster-line-kernel-density-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_all_fixed_failure_dags_have_two_actual_sources', 'test_actual_point_order_template_and_duplicate_controls_cannot_weaken', 'test_all_nine_weight_field_failure_values_are_json_exact_and_not_silently_dropped', '(24,60,1740,85)', "glob('*.json'))),168)", 'test_all_twenty_failure_sources_keep_accurate_weight_geometry_and_crs_roles', 'test_both_doubled_and_retraced_actual_saved_controls_are_required', 'test_weight_selector_units_omission_and_unnormalized_proof_cannot_weaken', 'test_ten_method_failures_are_json_exact_and_not_defaulted', 'test_actual_method_witness_and_selector_omission_cannot_weaken', 'test_method_omission_boolean_proof_rejects_numeric_lookalikes')}

def validate_line_kernel_density(repository: Path, registered: set[str]) -> None:
    suite = 'raster-line-kernel-density-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-line-kernel-density-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in LINE_KERNEL_DENSITY_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-raster-line-kernel-density-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES','CONTROLS','SCALE_CONTROLS','RASTERS','VECTORS','METHOD_CONTROLS')}
    fixed={'CASES': {'uniform-default.cog.tif': ('template.tif', 'lines.gpkg', 4.0, None, None), 'uniform-explicit.cog.tif': ('template.tif', 'lines.gpkg', 4.0, None, 'uniform'), 'uniform-weighted-default.cog.tif': ('template.tif', 'lines.gpkg', 4.0, 'mass', None), 'uniform-weighted-explicit.cog.tif': ('template.tif', 'lines.gpkg', 4.0, 'mass', 'uniform'), 'uniform-zero.cog.tif': ('template.tif', 'lines.gpkg', 4.0, 'zero', 'uniform'), 'uniform-boundary-profile.cog.tif': ('profile.tif', 'profile.gpkg', 1.0, 'mass', 'uniform'), 'omitted.cog.tif': ('template.tif', 'lines.gpkg', 4.0, None, 'quartic'), 'ones.cog.tif': ('template.tif', 'lines.gpkg', 4.0, 'ones', 'quartic'), 'mixed.cog.tif': ('template.tif', 'lines.gpkg', 4.0, 'mass', 'quartic'), 'reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0, 'mass', 'quartic'), 'integer.cog.tif': ('template.tif', 'integer.gpkg', 4.0, 'mass', 'quartic'), 'replicated.cog.tif': ('template.tif', 'replicated.gpkg', 4.0, None, 'quartic'), 'scaled.cog.tif': ('template.tif', 'lines.gpkg', 4.0, 'scaled', 'quartic'), 'retraced.cog.tif': ('template.tif', 'retraced.gpkg', 4.0, 'mass', 'quartic'), 'multipart.cog.tif': ('template.tif', 'multipart.gpkg', 4.0, 'mass', 'quartic'), 'subdivided.cog.tif': ('template.tif', 'subdivided.gpkg', 4.0, 'mass', 'quartic'), 'zero.cog.tif': ('template.tif', 'lines.gpkg', 4.0, 'zero', 'quartic'), 'empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 'mass', 'quartic'), 'mask-alpha.cog.tif': ('coverage.tif', 'lines.gpkg', 4.0, 'mass', 'quartic'), 'boundary-profile.cog.tif': ('profile.tif', 'profile.gpkg', 1.0, 'mass', 'quartic'), 'foot-block.cog.tif': ('rectangular.tif', 'foot.gpkg', 4.0, 'mass', 'quartic'), 'survey-block.cog.tif': ('sheared.tif', 'survey.gpkg', 4.0, 'mass', 'quartic'), 'diameter.cog.tif': ('profile.tif', 'diameter.gpkg', 1.0, 'mass', 'quartic'), 'chord.cog.tif': ('profile.tif', 'chord.gpkg', 1.0, 'mass', 'quartic')}, 'CONTROLS': (('uniform-default.cog.tif', 'uniform-explicit.cog.tif'), ('uniform-weighted-default.cog.tif', 'uniform-weighted-explicit.cog.tif'), ('uniform-zero.cog.tif', 'zero.cog.tif'), ('omitted.cog.tif', 'ones.cog.tif'), ('mixed.cog.tif', 'reversed.cog.tif'), ('integer.cog.tif', 'replicated.cog.tif'), ('mixed.cog.tif', 'multipart.cog.tif'), ('mixed.cog.tif', 'subdivided.cog.tif'), ('mixed.cog.tif', 'mask-alpha.cog.tif'), ('zero.cog.tif', 'empty.cog.tif')), 'SCALE_CONTROLS': (('mixed.cog.tif', 'scaled.cog.tif', 8), ('mixed.cog.tif', 'retraced.cog.tif', 2)), 'FAILURES': ('field_null', 'field_empty', 'field_blank', 'field_bool', 'field_number', 'field_array', 'field_object', 'field_missing', 'field_geometry', 'missing', 'negative', 'string', 'outside_missing', 'outside_negative', 'zero_point', 'zero_line', 'zero_component', 'zero_outside_z', 'zero_crs', 'zero_radius', 'method_null', 'method_true', 'method_false', 'method_number', 'method_list', 'method_object', 'method_empty', 'method_blank', 'method_case', 'method_unknown'), 'RASTERS': ('template.tif', 'coverage.tif', 'profile.tif', 'rectangular.tif', 'sheared.tif'), 'VECTORS': ('lines.gpkg', 'reversed.gpkg', 'integer.gpkg', 'replicated.gpkg', 'retraced.gpkg', 'multipart.gpkg', 'subdivided.gpkg', 'empty.gpkg', 'profile.gpkg', 'foot.gpkg', 'survey.gpkg', 'missing.gpkg', 'negative.gpkg', 'string.gpkg', 'outside_missing.gpkg', 'outside_negative.gpkg', 'zero_point.gpkg', 'zero_line.gpkg', 'zero_component.gpkg', 'zero_outside_z.gpkg', 'zero_crs.gpkg', 'diameter.gpkg', 'chord.gpkg'), 'METHOD_CONTROLS': (('uniform-default.cog.tif', 'omitted.cog.tif'), ('uniform-weighted-default.cog.tif', 'mixed.cog.tif'), ('uniform-boundary-profile.cog.tif', 'boundary-profile.cog.tif'))}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','raster_line_density(', 'STRtree(', '_circle_sites(', '_line_density_segment(', '_line_density_float(', '_rational_sqrt_bounds(', '_density_weights(')):raise RegistrationError('expected oracle must be independent')

    runner_tree=ast.parse((repository/'scripts/test/raster-line-kernel-density-persistence-online.py').read_text())
    fields=[n.value for n in runner_tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id=='FIELD_FAILURES'for t in n.targets)]
    if len(fields)!=1 or repr(ast.literal_eval(fields[0]))!="{'field_null': None, 'field_empty': '', 'field_blank': ' ', 'field_bool': True, 'field_number': 1, 'field_array': [], 'field_object': {}, 'field_missing': 'absent', 'field_geometry': 'shape'}":raise RegistrationError('fixed typed weight-field failures changed')

    methods=[n.value for n in runner_tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id=='METHOD_FAILURES'for t in n.targets)]
    if len(methods)!=1 or repr(ast.literal_eval(methods[0]))!="{'method_null': None, 'method_true': True, 'method_false': False, 'method_number': 1, 'method_list': [], 'method_object': {}, 'method_empty': '', 'method_blank': ' ', 'method_case': 'Quartic', 'method_unknown': 'gaussian'}":raise RegistrationError('fixed typed method failures changed')

POINT_KERNEL_DENSITY_REQUIRED = {'business/scripts/online-raster-point-kernel-density-fixture.py': ('NFSExport', 'def oracle', 'def verify_sources', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'np.array_equal(band.ReadAsArray(),a,equal_nan=True)', 'nearest_overviews', "with state.open('xb')", 'kernel_and_uniform_methods_verified', 'unit_points_per_square_metre_verified', 'actual_control_pairs_verified', 'actual_scale_control_verified', 'actual triplicated point density differs', 'context.prec=2000', 'distances=[', "band.GetUnitType()!='points/m2'", 'SCALE_CONTROL', 'numerator=3*sum', 'Decimal.from_float(radius)**(6 if', "'method':CASES[name][3]or'uniform'", 'default-uniform.cog.tif', 'explicit-uniform.cog.tif'), 'scripts/test/raster-point-kernel-density-persistence-online.py': ('template_mask_ignored_verified', 'archive_response', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 18', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.strict_sizes', 'validate_target_item', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_cogs', 'len(failures) != 52', 'full_checks != 1107', 'physical_calls != 71', 'actual_scale_control_verified', 'unit_points_per_square_metre_verified', 'radius_null', 'radius_object', "'operator':'raster_point_density'", "'task': 'density'", "params['method']=method", 'method_fraction', 'method_null', "fact.get('method')!=(fixture.CASES[n][3]or'uniform')"), 'scripts/test/online-hosted-raster-point-kernel-density-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-point-kernel-density-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'engines/geopython-workflow/test_online_point_kernel_density_fixture.py': ('test_eighteen_complete_cogs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers', 'test_density_frozen_matrix_exact_boundaries_duplicates_units_zero_and_actual_controls', 'test_density_oracle_rejects_specific_wrong_count_area_boundary_zero_and_unit', 'timeout=90', 'test_uniform_saved_controls_use_original_count_formula_and_differ_from_quartic', "'_point_density_mass'", "expected['boundary.cog.tif']==[[0.]]"), 'scripts/test/raster-point-kernel-density-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_all_fixed_failure_dags_have_two_actual_sources', 'test_actual_point_order_template_and_duplicate_controls_cannot_weaken', 'test_all_eight_radius_failure_values_are_json_exact_and_not_silently_dropped', '(18,52,1107,71)', "glob('*.json'))),140)", 'test_all_ten_invalid_methods_have_exact_json_values_and_density_stage', 'test_physical_method_claim_cannot_substitute_uniform_for_quartic'), 'scripts/utils/online_raster_lineage.py': ("'raster-point-kernel-density-persistence': 'raster-point-kernel-density-lineage'",), 'Makefile': ('test_online_point_kernel_density_fixture.py', '@$(MAKE) test-raster-point-kernel-density-online-runner', 'scripts/test/raster-point-kernel-density-persistence-online_test.py', 'test_point_kernel_density_requires_exact_cog_matrix_budget_and_regressions')}

def validate_point_kernel_density(repository: Path, registered: set[str]) -> None:
    suite = 'raster-point-kernel-density-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-point-kernel-density-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in POINT_KERNEL_DENSITY_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-raster-point-kernel-density-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES','CONTROLS','SCALE_CONTROL','RASTERS','VECTORS')}
    fixed={'CASES': {'base.cog.tif': ('template.tif', 'points.gpkg', 4.0, 'quartic'), 'reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0, 'quartic'), 'triplicated.cog.tif': ('template.tif', 'triplicated.gpkg', 4.0, 'quartic'), 'coincident.cog.tif': ('profile.tif', 'coincident.gpkg', 1.0, 'quartic'), 'below.cog.tif': ('one.tif', 'boundary.gpkg', 0.9999999999999999, 'quartic'), 'boundary.cog.tif': ('one.tif', 'boundary.gpkg', 1.0, 'quartic'), 'above.cog.tif': ('one.tif', 'boundary.gpkg', 1.0000000000000002, 'quartic'), 'outside.cog.tif': ('template.tif', 'outside.gpkg', 1.0, 'quartic'), 'partial.cog.tif': ('template.tif', 'points.gpkg', 0.5, 'quartic'), 'empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 'quartic'), 'invalid-template.cog.tif': ('invalid.tif', 'points.gpkg', 4.0, 'quartic'), 'mask-alpha.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 'quartic'), 'rectangular-block.cog.tif': ('rectangular.tif', 'points.gpkg', 4.0, 'quartic'), 'rotated-sheared-block.cog.tif': ('sheared.tif', 'points.gpkg', 4.0, 'quartic'), 'foot.cog.tif': ('foot.tif', 'foot.gpkg', 1.5, 'quartic'), 'survey-foot.cog.tif': ('survey.tif', 'survey.gpkg', 1.5, 'quartic'), 'default-uniform.cog.tif': ('template.tif', 'points.gpkg', 4.0, None), 'explicit-uniform.cog.tif': ('template.tif', 'points.gpkg', 4.0, 'uniform')}, 'CONTROLS': (('base.cog.tif', 'reversed.cog.tif'), ('base.cog.tif', 'invalid-template.cog.tif'), ('base.cog.tif', 'mask-alpha.cog.tif'), ('default-uniform.cog.tif', 'explicit-uniform.cog.tif')), 'SCALE_CONTROL': ('base.cog.tif', 'triplicated.cog.tif', 3), 'FAILURES': ('radius_null', 'radius_true', 'radius_false', 'radius_zero', 'radius_negative', 'radius_string', 'radius_array', 'radius_object', 'null_geometry', 'empty_geometry', 'line', 'multipoint', 'z_point', 'outside_z_point', 'different_crs', 'geographic', 'method_null', 'method_true', 'method_false', 'method_zero', 'method_fraction', 'method_case', 'method_unknown', 'method_empty', 'method_array', 'method_object'), 'RASTERS': ('template.tif', 'invalid.tif', 'coverage.tif', 'rectangular.tif', 'sheared.tif', 'foot.tif', 'survey.tif', 'geographic.tif', 'one.tif', 'profile.tif'), 'VECTORS': ('points.gpkg', 'reversed.gpkg', 'triplicated.gpkg', 'coincident.gpkg', 'boundary.gpkg', 'outside.gpkg', 'empty.gpkg', 'foot.gpkg', 'survey.gpkg', 'null_geometry.gpkg', 'empty_geometry.gpkg', 'line.gpkg', 'multipoint.gpkg', 'z_point.gpkg', 'outside_z_point.gpkg', 'different_crs.gpkg', 'geographic.gpkg')}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','raster_point_density(', 'STRtree(', '_circle_sites(')):raise RegistrationError('expected oracle must be independent')

POINT_DENSITY_WEIGHTED_REQUIRED = {'scripts/test/online-hosted-raster-point-density-weighted-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-point-density-weighted-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'scripts/utils/online_raster_lineage.py': ("'raster-point-density-weighted-persistence': 'raster-point-density-weighted-lineage'",), 'Makefile': ('test_online_point_density_weighted_fixture.py', '@$(MAKE) test-raster-point-density-weighted-online-runner', 'scripts/test/raster-point-density-weighted-persistence-online_test.py', 'test_point_density_weighted_requires_exact_cog_matrix_budget_and_regressions'), 'business/scripts/online-raster-point-density-weighted-fixture.py': ('NFSExport', 'def oracle', 'def verify_sources', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'nearest_overviews', "with state.open('xb')", 'kernel_and_uniform_methods_verified', 'density_units_verified', 'actual_control_pairs_verified', 'actual_scale_controls_verified', 'actual absolute weight scale differs', 'context.prec=2000', 'distances=[', 'band.GetUnitType()!=output_unit(name)', 'SCALE_CONTROLS', 'decimal(weight)', 'Decimal.from_float(radius)**(6 if', "'weight_field':CASES[name][4]", 'weight/m2'), 'scripts/test/raster-point-density-weighted-persistence-online.py': ('template_mask_ignored_verified', 'archive_response', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 24', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.strict_sizes', 'validate_target_item', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_cogs', 'len(failures) != 64', 'full_checks != 1836', 'physical_calls != 89', 'actual_scale_controls_verified', 'density_units_verified', "'operator':'raster_point_density'", "'task': 'density'", "params['weight_field']=field", 'FIELD_FAILURES', "fact.get('weight_field')!=fixture.CASES[n][4]", "fact.get('unit')!=fixture.output_unit(n)"), 'engines/geopython-workflow/test_online_point_density_weighted_fixture.py': ('test_twenty_four_complete_cogs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers', 'timeout=90', 'test_weighted_fixed_matrix_independent_formulas_controls_units_and_tails', 'test_weighted_oracle_rejects_actual_saved_semantic_corruption', 'test_weighted_boundary_and_adjacent_float_witnesses_have_no_epsilon', "'_point_density_mass'", 'test_input_weight_values_schema_and_load_failures_preserve_expected_stage'), 'scripts/test/raster-point-density-weighted-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_all_fixed_failure_dags_have_two_actual_sources', 'test_actual_point_order_template_and_duplicate_controls_cannot_weaken', '(24,64,1836,89)', "glob('*.json'))),176)", 'test_all_nine_field_failure_values_are_json_exact_and_not_dropped', 'test_physical_method_field_and_units_cannot_be_substituted')}

def validate_point_density_weighted(repository: Path, registered: set[str]) -> None:
    suite = 'raster-point-density-weighted-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-point-density-weighted-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in POINT_DENSITY_WEIGHTED_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-raster-point-density-weighted-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES','CONTROLS','SCALE_CONTROLS','RASTERS','VECTORS')}
    fixed={'CASES': {'uniform-omitted.cog.tif': ('template.tif', 'points.gpkg', 4.0, 'uniform', None), 'uniform-ones.cog.tif': ('template.tif', 'points.gpkg', 4.0, 'uniform', 'ones'), 'uniform-mixed.cog.tif': ('template.tif', 'points.gpkg', 4.0, 'uniform', 'mass'), 'uniform-reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0, 'uniform', 'mass'), 'uniform-integer.cog.tif': ('template.tif', 'integer.gpkg', 4.0, 'uniform', 'mass'), 'uniform-replicated.cog.tif': ('template.tif', 'replicated.gpkg', 4.0, 'uniform', None), 'uniform-scaled.cog.tif': ('template.tif', 'points.gpkg', 4.0, 'uniform', 'scaled'), 'uniform-zero.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 'uniform', 'zero'), 'uniform-empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 'uniform', 'mass'), 'uniform-masked.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 'uniform', 'mass'), 'uniform-rectangular.cog.tif': ('rectangular.tif', 'foot.gpkg', 4.0, 'uniform', 'mass'), 'uniform-rotated.cog.tif': ('sheared.tif', 'survey.gpkg', 4.0, 'uniform', 'mass'), 'quartic-omitted.cog.tif': ('template.tif', 'points.gpkg', 4.0, 'quartic', None), 'quartic-ones.cog.tif': ('template.tif', 'points.gpkg', 4.0, 'quartic', 'ones'), 'quartic-mixed.cog.tif': ('template.tif', 'points.gpkg', 4.0, 'quartic', 'mass'), 'quartic-reversed.cog.tif': ('template.tif', 'reversed.gpkg', 4.0, 'quartic', 'mass'), 'quartic-integer.cog.tif': ('template.tif', 'integer.gpkg', 4.0, 'quartic', 'mass'), 'quartic-replicated.cog.tif': ('template.tif', 'replicated.gpkg', 4.0, 'quartic', None), 'quartic-scaled.cog.tif': ('template.tif', 'points.gpkg', 4.0, 'quartic', 'scaled'), 'quartic-zero.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 'quartic', 'zero'), 'quartic-empty.cog.tif': ('template.tif', 'empty.gpkg', 4.0, 'quartic', 'mass'), 'quartic-masked.cog.tif': ('coverage.tif', 'points.gpkg', 4.0, 'quartic', 'mass'), 'quartic-rectangular.cog.tif': ('rectangular.tif', 'foot.gpkg', 4.0, 'quartic', 'mass'), 'quartic-rotated.cog.tif': ('sheared.tif', 'survey.gpkg', 4.0, 'quartic', 'mass')}, 'CONTROLS': (('uniform-omitted.cog.tif', 'uniform-ones.cog.tif'), ('uniform-mixed.cog.tif', 'uniform-reversed.cog.tif'), ('uniform-integer.cog.tif', 'uniform-replicated.cog.tif'), ('uniform-mixed.cog.tif', 'uniform-masked.cog.tif'), ('quartic-omitted.cog.tif', 'quartic-ones.cog.tif'), ('quartic-mixed.cog.tif', 'quartic-reversed.cog.tif'), ('quartic-integer.cog.tif', 'quartic-replicated.cog.tif'), ('quartic-mixed.cog.tif', 'quartic-masked.cog.tif')), 'SCALE_CONTROLS': (('uniform-mixed.cog.tif', 'uniform-scaled.cog.tif', 8), ('quartic-mixed.cog.tif', 'quartic-scaled.cog.tif', 8)), 'FAILURES': ('uniform_field_null', 'uniform_field_empty', 'uniform_field_blank', 'uniform_field_bool', 'uniform_field_number', 'uniform_field_array', 'uniform_field_object', 'uniform_field_missing', 'uniform_field_geometry', 'uniform_missing', 'uniform_negative', 'uniform_string', 'uniform_outside_missing', 'uniform_outside_negative', 'uniform_zero_line', 'uniform_zero_crs', 'quartic_field_null', 'quartic_field_empty', 'quartic_field_blank', 'quartic_field_bool', 'quartic_field_number', 'quartic_field_array', 'quartic_field_object', 'quartic_field_missing', 'quartic_field_geometry', 'quartic_missing', 'quartic_negative', 'quartic_string', 'quartic_outside_missing', 'quartic_outside_negative', 'quartic_zero_line', 'quartic_zero_crs'), 'RASTERS': ('template.tif', 'coverage.tif', 'rectangular.tif', 'sheared.tif'), 'VECTORS': ('points.gpkg', 'reversed.gpkg', 'integer.gpkg', 'replicated.gpkg', 'empty.gpkg', 'foot.gpkg', 'survey.gpkg', 'missing.gpkg', 'negative.gpkg', 'string.gpkg', 'outside_missing.gpkg', 'outside_negative.gpkg', 'zero_line.gpkg', 'zero_crs.gpkg')}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    runner_tree=ast.parse((repository/'scripts/test/raster-point-density-weighted-persistence-online.py').read_text())
    field_maps=[node.value for node in runner_tree.body if isinstance(node,ast.Assign)and any(isinstance(t,ast.Name)and t.id=='FIELD_FAILURES'for t in node.targets)]
    fixed_fields={'field_null':None,'field_empty':'','field_blank':'  ','field_bool':True,'field_number':1,'field_array':[],'field_object':{},'field_missing':'not_present','field_geometry':'geometry'}
    if len(field_maps)!=1 or repr(ast.literal_eval(field_maps[0]))!=repr(fixed_fields):raise RegistrationError('fixed explicit field failure values changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','raster_point_density(', 'STRtree(', '_circle_sites(')):raise RegistrationError('expected oracle must be independent')

FOCAL_PERCENTILES_REQUIRED = {'business/scripts/online-raster-focal-percentiles-fixture.py': ('NFSExport', 'def oracle', 'def verify_sources', 'def oracle_scope', 'source_files', 'fixture_sha256', 'oracle cache became writable', 'np.array_equal(band.ReadAsArray(),wanted,equal_nan=True)', 'nearest_overviews', 'actual batch and single-stack controls differ', 'actual P50 and median controls differ', 'descriptions(name)', "with state.open('xb')"), 'scripts/test/raster-focal-percentiles-persistence-online.py': ('band_order_and_descriptions_verified', 'actual_control_pairs_verified', 'archive_response', 'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'remaining()', 'budget != 600', 'len(cases) != 16', 'current(execution', 'lineage.collect', 'lineage.verify_archive', 'base.validate_scan', 'raster.strict_sizes', 'validate_target_item', 'target_metadata_after != target_metadata_before', 'source_metadata_after != before', "for language in ('zh-cn', 'en')", 'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage', "physical_runner(repository, env, 'cleanup-oracle'", 'archived_cogs', 'len(failures) != 56', 'full_checks != 1032', 'physical_calls != 73', "'operator':'raster_stack'"), 'scripts/test/online-hosted-raster-focal-percentiles-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', 'online_raster_lineage.py prepare', '--suite raster-workflow', '--wait-protection-owners develop,manager', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-focal-percentiles-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'engines/geopython-workflow/test_online_focal_percentiles_fixture.py': ('test_sixteen_complete_cogs_independent_oracle_and_raw_archives', 'test_exact_bilingual_failures_publish_nothing_and_remove_workspaces', 'test_real_physical_corruption_and_source_or_extra_files_are_rejected', 'test_archive_existing_bytes_are_not_repaired_or_overwritten', 'test_worker_stdin_frozen_source_run_scope_poison_and_cleanup', 'test_oracle_is_independent_of_product_computation_and_readers', 'test_focal_percentiles_exact_dyadic_band_rejects_one_ulp'), 'scripts/test/raster-focal-percentiles-persistence-online_test.py': ('test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection', 'test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken', 'test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced', 'test_fixed_case_dags_roles_and_complete_preview', 'test_exactly_144_raw_terminal_responses_are_archived'), 'scripts/utils/online_raster_lineage.py': ("'raster-focal-percentiles-persistence': 'raster-focal-percentiles-lineage'",), 'Makefile': ('test_online_focal_percentiles_fixture.py', '@$(MAKE) test-raster-focal-percentiles-online-runner', 'scripts/test/raster-focal-percentiles-persistence-online_test.py', 'test_focal_percentiles_requires_exact_cog_matrix_budget_and_regressions')}

def validate_focal_percentiles(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-percentiles-persistence'
    if suite not in registered: return
    workflow=(repository/'.github/workflows/online-t4-gates.yml').read_text()
    job=re.search(r'(?ms)^  raster-focal-percentiles-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)',workflow)
    if (job is None or 'runs-on: ubuntu-24.04'not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == '"+suite+"'"not in job.group('body')
        or "&& inputs.suite != '"+suite+"'"not in workflow or 'nfs-kernel-server nfs-common'not in job.group('body')
        or 'cancel-in-progress: false'not in job.group('body')or 'if: always()'not in job.group('body')or 'logs/'not in job.group('body')or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted NFS job required')
    for relative,fragments in FOCAL_PERCENTILES_REQUIRED.items():
        path=repository/relative
        if not path.is_file()or any(not source_contract_contains(path.read_text(), fragment, repository)for fragment in fragments):raise RegistrationError('strict Hosted contract incomplete: '+relative)
    source=(repository/'business/scripts/online-raster-focal-percentiles-fixture.py').read_text();tree=ast.parse(source)
    actual={key:[n.value for n in tree.body if isinstance(n,ast.Assign)and any(isinstance(t,ast.Name)and t.id==key for t in n.targets)]for key in('CASES','FAILURES','PERCENTILES')}
    fixed={'PERCENTILES': [0.0, 25.0, 33.3, 50.0, 75.0, 100.0], 'CASES': {'square3.cog.tif': ('values.tif', 'square_pixels', 3, 'batch'), 'square3-stack.cog.tif': ('values.tif', 'square_pixels', 3, 'singles'), 'square5.cog.tif': ('values.tif', 'square_pixels', 5, 'batch'), 'square5-stack.cog.tif': ('values.tif', 'square_pixels', 5, 'singles'), 'square3-median.cog.tif': ('values.tif', 'square_pixels', 3, 'median'), 'square5-median.cog.tif': ('values.tif', 'square_pixels', 5, 'median'), 'circle.cog.tif': ('metre.tif', 'circle_metres', 3.0, 'batch'), 'circle-stack.cog.tif': ('metre.tif', 'circle_metres', 3.0, 'singles'), 'circle-median.cog.tif': ('metre.tif', 'circle_metres', 3.0, 'median'), 'foot.cog.tif': ('foot.tif', 'circle_metres', 1.5, 'batch'), 'survey-foot.cog.tif': ('survey.tif', 'circle_metres', 1.5, 'batch'), 'reclassified.cog.tif': ('values.tif', 'square_pixels', 3, 'reclassify'), 'int64-unlocated.cog.tif': ('int64.tif', 'square_pixels', 3, 'batch'), 'uint64.cog.tif': ('uint64.tif', 'square_pixels', 5, 'batch'), 'cross-block.cog.tif': ('cross.tif', 'square_pixels', 5, 'batch'), 'invalid.cog.tif': ('invalid.tif', 'square_pixels', 3, 'batch')}, 'FAILURES': ('list_missing', 'list_null', 'list_empty', 'scalar_number', 'scalar_boolean', 'scalar_string', 'scalar_object', 'item_boolean', 'item_string', 'item_null', 'item_list', 'item_object', 'item_low', 'item_high', 'duplicate', 'descending', 'unused', 'unused_null', 'band_zero', 'band_high', 'band_boolean', 'band_alpha', 'square_size', 'square_radius', 'circle_window', 'circle_radius_missing', 'circle_size', 'circle_geographic')}
    if any(len(nodes)!=1 or repr(ast.literal_eval(nodes[0]))!=repr(fixed[key])for key,nodes in actual.items()):raise RegistrationError('fixed success/failure matrix changed')
    if any(word in source for word in('operators.raster_compute','gdal.InvGeoTransform','raster_focal_statistics(', 'np.percentile(', 'np.quantile(', 'raster_vector_zonal_statistics(')):raise RegistrationError('expected oracle must be independent')

SQUARE_SEVEN_REQUIRED = {
    'business/scripts/online-raster-focal-square-seven-fixture.py': (
        'NFSExport', 'full_check=True', 'check_band', 'complete band mask differs', 'complete overview mask differs',
        'actual same-source COG control pixels or masks differ', 'fixture_sha256', 'source_files',
        'oracle cache hash changed', 'allow_pickle=False', 'Runtime execution workspace residual', 'archive_files',
        '(367, 369)'),
    'scripts/test/raster-focal-square-seven-persistence-online.py': (
        'ADDP_ONLINE_FOCAL_SQUARE_SEVEN_STARTED', 'budget != 600', 'raster.EDITOR_PERMISSIONS',
        'lineage.collect', 'lineage.verify_archive', 'raster.strict_sizes', 'base.validate_scan',
        'for language in (\'zh-cn\', \'en\')', 'wrong exact failure task/cause',
        'query language rewrote stored failure', 'failed execution added scan', 'failed execution added derived lineage',
        'target_metadata_after != target_metadata_before', 'source_metadata_after != before',
        'len(cases) != 18', 'len(failures) != 60', 'full_checks != 1251', 'physical_calls != 79',
        "physical_runner(repository, env, 'cleanup-oracle'"),
    'scripts/test/online-hosted-raster-focal-square-seven-gate.sh': (
        'start_online_geopython_runtime', 'ADDP_ONLINE_FOCAL_SQUARE_SEVEN_STARTED', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600',
        '--suite raster-workflow', '--wait-protection-owners develop,manager',
        'online_raster_lineage.py prepare', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
    'scripts/utils/online_raster_lineage.py': (
        "'raster-focal-square-seven-persistence': 'raster-focal-square-seven-lineage'",),
    'engines/geopython-workflow/test_online_focal_square_seven_fixture.py': (
        'test_eighteen_real_cogs_original_archives_and_1251_full_reads', 'test_seam_sensitive_oracle_and_weight_direction',
        'test_physical_corruption_is_rejected', 'test_sixty_accurate_failures_are_at_focal_and_clear_workspace'),
    'scripts/test/raster-focal-square-seven-persistence-online_test.py': (
        'test_exact_18_success_60_failures_1251_full_reads_and_cleanup',
        'test_budget_includes_prior_initialization_and_stops_at_exhaustion'),
    'Makefile': ('test_online_focal_square_seven_fixture.py', '@$(MAKE) test-raster-focal-square-seven-online-runner',
                 'scripts/test/online-hosted-raster-focal-square-seven-gate_test.py'),
}


def validate_focal_square_seven(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-square-seven-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-focal-square-seven-hosted-t4:\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    job_fragments = (
        f"if: github.event_name == 'workflow_dispatch' && inputs.suite == '{suite}'",
        'runs-on: ubuntu-24.04', 'cancel-in-progress: false', 'nfs-kernel-server nfs-common',
        'online-hosted-raster-focal-square-seven-gate.sh --check-only', 'if: always()', 'logs/')
    if (job is None or any(fragment not in job[1] for fragment in job_fragments)
            or f"&& inputs.suite != '{suite}'" not in workflow or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('independent manual Hosted seven-pixel NFS job required')
    for relative, fragments in SQUARE_SEVEN_REQUIRED.items():
        path = repository / relative
        if not path.is_file() or any(not source_contract_contains(path.read_text(), item, repository) for item in fragments):
            raise RegistrationError('strict seven-pixel Hosted contract incomplete: ' + relative)
    fixture = (repository / 'business/scripts/online-raster-focal-square-seven-fixture.py').read_text()
    assignments = {node.targets[0].id: node.value for node in ast.parse(fixture).body
                   if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name)}
    cases = ast.literal_eval(assignments['CASES'])
    failures = ast.literal_eval(assignments['FAILURES'])
    kernels = {method for _, size, method, kernel, _ in cases.values() if size == 7 and kernel == 'asymmetric'}
    uniform = {method for _, size, method, kernel, _ in cases.values() if size == 7 and kernel == 'ones'}
    weighted = {'weighted_sum', 'weighted_mean', 'weighted_stddev'}
    methods = {method for _, size, method, _, _ in cases.values() if size == 7}
    pairs = ast.literal_eval(assignments['COMPARISONS'])
    if (len(cases) != 18 or len(failures) != 30 or len(set(failures)) != 30 or kernels != weighted or uniform != weighted
            or methods != weighted | {'sum', 'mean', 'min', 'max', 'stddev', 'mode', 'median', 'percentile', 'variety'}
            or len(pairs) != 6 or {pair[0] for pair in pairs if pair[0].startswith('ones-')} != {
                'ones-sum.cog.tif', 'ones-mean.cog.tif', 'ones-stddev.cog.tif'}):
        raise RegistrationError('seven-pixel fixed COG matrix or actual comparison coverage changed')
    if any(fragment in fixture for fragment in ('operators.raster_compute', 'raster_focal_statistics(', 'np.percentile(', 'np.quantile(')):
        raise RegistrationError('seven-pixel oracle must be independent')


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, required=True)
    args = parser.parse_args()
    try:
        check_registration(args.repository.resolve())
    except (OSError, RegistrationError) as error:
        print(f"Online CI registration check failed: {error}", file=sys.stderr)
        return 1
    print("Online CI registration is consistent")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
