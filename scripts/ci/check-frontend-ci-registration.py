#!/usr/bin/env python3
"""Verify that every tracked ADDP frontend is registered in the CI contract."""

from __future__ import annotations

import argparse
import json
import re
import shlex
import subprocess
import sys
from pathlib import Path
from urllib.parse import urlparse


class RegistrationError(RuntimeError):
    pass


FRONTEND_GATE_ACTION = "uses: ./.github/actions/prepare-frontend-gate"
MODULE_GATE_SELECTOR = "python3 scripts/ci/select-module-gate.py"


def git_files(repository: Path, pattern: str) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", pattern],
        cwd=repository,
        check=True,
        capture_output=True,
        text=True,
    )
    return [line for line in result.stdout.splitlines() if line]


def worktree_files(repository: Path, pattern: str) -> list[str]:
    result = subprocess.run(["git", "ls-files", "-co", "--exclude-standard", "--", pattern], cwd=repository, check=True, capture_output=True, text=True)
    return sorted({line for line in result.stdout.splitlines() if line and (repository / line).is_file()})


def discover_frontends(repository: Path) -> list[str]:
    modules: list[str] = []
    for relative_path in git_files(repository, "*/frontend/package.json"):
        package_path = repository / relative_path
        package = json.loads(package_path.read_text(encoding="utf-8"))
        scripts = package.get("scripts")
        if not isinstance(scripts, dict) or not scripts.get("build"):
            raise RegistrationError(f"{relative_path} must declare scripts.build")
        modules.append(relative_path.split("/", 1)[0])
    if not modules:
        raise RegistrationError("no tracked */frontend/package.json files found")
    return sorted(modules)


def workflow_jobs(repository: Path) -> list[str]:
    jobs: list[str] = []
    workflow_paths = sorted((repository / ".github/workflows").glob("*.yml"))
    workflow_paths.extend(sorted((repository / ".github/workflows").glob("*.yaml")))
    if not workflow_paths:
        raise RegistrationError("no GitHub Actions workflows found")
    for path in workflow_paths:
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


ISOLATION_IMPORT = "../../common-frontend/basic/src/utils/viteTestIsolation.mjs"


def validate_browser_isolation(repository: Path) -> list[str]:
    """Check literal deterministic fixture recipes without launching Node or services."""
    repository = repository.resolve()
    errors = []
    config_paths = worktree_files(repository, "*/frontend/playwright.config.*")
    for module in discover_frontends(repository):
        package = json.loads((repository / module / "frontend/package.json").read_text())
        if any(name == "test:e2e" and "playwright test" in command
               for name, command in package.get("scripts", {}).items()):
            if not any(path.startswith(f"{module}/frontend/") for path in config_paths):
                errors.append(f"{module}: deterministic Playwright config is missing")
    development_ports = set()
    for path in git_files(repository, "*/frontend/vite.config.*"):
        development_ports.update(int(port) for port in re.findall(
            r"process\.env\.[A-Z_]+_FE_PORT\s*\|\|\s*(\d+)",
            (repository / path).read_text(encoding="utf-8"),
        ))
    for path in config_paths:
        source = (repository / path).read_text(encoding="utf-8")
        servers = list(re.finditer(
            r"command:\s*(['\"])(?P<command>[^\n]*?)\1\s*,(?P<body>.*?)^\s*}",
            source, re.M | re.S,
        ))
        if not servers or len(servers) != len(re.findall(r"\bcommand\s*:", source)):
            errors.append(f"{path}: each deterministic webServer must use a literal npm fixture recipe")
            continue
        ports = set()
        for server in servers:
            label = f"{path}: {server.group('command')}"
            try:
                tokens = shlex.split(server.group('command'))
                npm_index = tokens.index("npm")
                environment = dict(token.split("=", 1) for token in tokens[:npm_index] if "=" in token)
                if environment.get("ADDP_E2E") != "1":
                    raise ValueError("webServer must explicitly set ADDP_E2E=1")
                if any(token in {"&&", ";", "|", "||", "&"} for token in tokens):
                    raise ValueError("webServer must launch only its single Vite fixture")
                arguments = tokens[npm_index + 1:]
                frontend = repository / Path(path).parent
                if arguments[:1] == ["--prefix"]:
                    frontend = (frontend / arguments[1]).resolve()
                    arguments = arguments[2:]
                if arguments[:3] != ["run", "dev", "--"]:
                    raise ValueError("webServer must use npm run dev with explicit Vite CLI arguments")
                if "--strictPort" not in arguments:
                    raise ValueError("webServer must set --strictPort")
                host = arguments[arguments.index("--host") + 1]
                port = int(arguments[arguments.index("--port") + 1])
                if host != "127.0.0.1" or not 1 <= port <= 65535:
                    raise ValueError("webServer must bind an explicit loopback port")
                if port in development_ports or port in ports:
                    raise ValueError("webServer port must be distinct from development and sibling fixtures")
                ports.add(port)
                body = server.group("body")
                url_match = re.search(r"url:\s*['\"]([^'\"]+)['\"]", body)
                if url_match is None:
                    raise ValueError("webServer must declare its readiness URL")
                url = urlparse(url_match.group(1))
                if url.scheme != "http" or url.hostname != host or url.port != port:
                    raise ValueError("webServer URL must match its CLI host and port")
                if not re.search(r"reuseExistingServer:\s*false\b", body):
                    raise ValueError("webServer must not reuse an existing server")
                if not re.search(
                    r"gracefulShutdown:\s*{\s*signal:\s*['\"]SIGTERM['\"],\s*timeout:\s*[1-9]\d*\s*}", body,
                ):
                    raise ValueError("webServer must declare bounded SIGTERM graceful shutdown")
                relative_frontend = frontend.relative_to(repository)
                if len(relative_frontend.parts) != 2 or relative_frontend.parts[1] != "frontend":
                    raise ValueError("webServer must start an ADDP frontend owner")
                package = json.loads((frontend / "package.json").read_text(encoding="utf-8"))
                if not re.match(r"^vite(?:\s|$)", package.get("scripts", {}).get("dev", "")):
                    raise ValueError("fixture dev script must directly launch Vite")
                vite = (frontend / "vite.config.js").read_text(encoding="utf-8")
                module = relative_frontend.parts[0]
                if not re.search(
                    rf"import\s*{{\s*withFrontendTestIsolation\s*}}\s*from\s*['\"]{re.escape(ISOLATION_IMPORT)}['\"]",
                    vite,
                ) or not re.search(
                    rf"export default defineConfig\((?:withModuleFrontend\(['\"]{re.escape(module)}['\"],\s*)?withFrontendTestIsolation\(['\"]{re.escape(module)}['\"],\s*{{",
                    vite,
                ):
                    raise ValueError("Vite config must use the shared withFrontendTestIsolation owner")
                if re.search(r"\bhmr\s*:\s*(?:process\.env\.ADDP_E2E|!?isE2E|!?IS_E2E|!?testing)\b", vite):
                    raise ValueError("module-owned Vite test HMR branches are forbidden")
                if re.search(r"\bcacheDir\s*:", vite):
                    raise ValueError("module-owned Vite cache isolation is forbidden")
            except (ValueError, IndexError, OSError, json.JSONDecodeError) as error:
                errors.append(f"{label}: {error}")
        base_url = re.search(r"baseURL:\s*['\"]([^'\"]+)['\"]", source)
        try:
            url = urlparse(base_url.group(1) if base_url else "")
            if url.scheme != "http" or url.hostname != "127.0.0.1" or url.port not in ports:
                raise ValueError("browser baseURL must use a declared loopback fixture port")
        except ValueError as error:
            errors.append(f"{path}: {error}")
    return errors


def validate_registration(repository: Path) -> list[str]:
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    logical_makefile = re.sub(r"\\\n\s*", " ", makefile)
    jobs = workflow_jobs(repository)
    errors: list[str] = []
    modules = discover_frontends(repository)
    for module in modules:
        target = f"test-{module}-frontend"
        if not re.search(rf"(?m)^{re.escape(target)}\s*:", makefile):
            errors.append(f"{module}: Makefile target {target} is missing")
        target_job = next(
            (
                job
                for job in jobs
                if re.search(rf"(?m)(?:target:\s*|make\s+){re.escape(target)}\s*$", job)
            ),
            None,
        )
        if target_job is None:
            errors.append(f"{module}: GitHub Actions target {target} is missing")
            errors.append(f"{module}: shared module change selector is missing")
            continue
        if FRONTEND_GATE_ACTION not in target_job:
            errors.append(f"{module}: standard frontend gate setup is missing from {target} job")
        direct_selector = re.search(
            rf"{re.escape(MODULE_GATE_SELECTOR)}\s+--module\s+['\"]?{re.escape(module)}['\"]?",
            target_job,
        )
        matrix_selector = (
            MODULE_GATE_SELECTOR in target_job
            and "--module '${{ matrix.module }}'" in target_job
            and module in re.findall(
                r"(?m)^\s*- module:\s*([a-z][a-z0-9-]*)\s*$", target_job
            )
        )
        if direct_selector is None and not matrix_selector:
            errors.append(f"{module}: shared module change selector is missing from {target} job")
        package = json.loads((repository / module / "frontend/package.json").read_text(encoding="utf-8"))
        browser_scripts = [
            name for name, command in package.get("scripts", {}).items()
            if name.startswith("test:e2e") and "playwright test" in command
        ]
        if browser_scripts:
            recipe = re.search(rf"(?m)^{re.escape(target)}:[^\n]*\n(?P<body>(?:[\t ][^\n]*\n|\n)*)", makefile)
            if not recipe or not any(
                re.search(rf"npm run {re.escape(name)}(?:\s|$)", recipe.group("body"))
                for name in browser_scripts
            ):
                errors.append(f"{module}: root frontend gate must run a declared Playwright suite")
            if not re.search(r"playwright install (?:--with-deps )?chromium", target_job):
                errors.append(f"{module}: frontend CI job must install Chromium")
            if "matrix.playwright" in target_job:
                entry = re.search(
                    rf"(?m)^\s*- module:\s*{re.escape(module)}\s*\n(?P<body>(?:(?!\s*- module:|\s*steps:)[^\n]*\n)*)",
                    target_job,
                )
                if not entry or not re.search(r"(?m)^\s*playwright:\s*true\s*$", entry.group("body")):
                    errors.append(f"{module}: frontend CI matrix must enable Playwright")
            steps = re.split(r"(?m)^      - ", target_job)
            artifact_steps = [step for step in steps if "actions/upload-artifact@" in step
                              and "playwright-results/" in step]
            if matrix_selector and not artifact_steps:
                errors.append(f"{module}: frontend Playwright matrix must upload browser failure evidence")
            for artifact_step in artifact_steps:
                if matrix_selector and not re.search(
                    r"(?m)^\s*if:\s*failure\(\)\s*&&\s*matrix\.playwright\s*==\s*true\s*&&",
                    artifact_step,
                ):
                    errors.append(f"{module}: browser failure evidence must cover the Playwright matrix")
                if "runner.temp" in artifact_step:
                    gate_steps = [step for step in steps if re.search(
                        rf"(?m)^\s*run:\s*make\s+(?:{re.escape(target)}|\$\{{\{{\s*matrix\.target\s*\}}\}})\s*$",
                        step,
                    )]
                    if not gate_steps or any(not re.search(
                        r"(?m)^\s*TMPDIR:\s*\$\{\{\s*runner.temp\s*\}\}\s*$", step
                    ) for step in gate_steps):
                        errors.append(f"{module}: browser gate TMPDIR must match the artifact runner.temp directory")
        test_target = re.search(r"(?m)^test\s*:(?P<dependencies>[^\n]*)", logical_makefile)
        if (
            test_target is None
            or target not in test_target.group("dependencies").split()
        ):
            errors.append(
                f"{module}: root test target dependency {target} is missing"
            )
    errors.extend(validate_browser_isolation(repository))
    return errors


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, default=Path.cwd())
    return parser.parse_args()


def main() -> int:
    repository = parse_args().repository.resolve()
    try:
        errors = validate_registration(repository)
    except (RegistrationError, json.JSONDecodeError, subprocess.CalledProcessError) as error:
        print(f"Frontend CI registration check failed: {error}", file=sys.stderr)
        return 1
    if errors:
        for error in errors:
            print(f"Frontend CI registration check failed: {error}", file=sys.stderr)
        return 1
    count = len(discover_frontends(repository))
    print(f"Frontend CI registration check passed: {count} tracked frontends are registered.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
