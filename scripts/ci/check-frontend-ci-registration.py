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


BROWSER_LAUNCHER = "node ../../scripts/test/frontend-browser-gate.mjs"
BROWSER_IMPORT = "../../common-frontend/basic/src/utils/browserTestIsolation.mjs"


def browser_fixtures(repository: Path, module: str) -> list[dict]:
    package = json.loads((repository / module / "frontend/package.json").read_text())
    declaration = package.get("addpBrowserTest")
    fixtures = declaration.get("fixtures", []) if isinstance(declaration, dict) else []
    return fixtures if isinstance(fixtures, list) else []


def validate_browser_isolation(repository: Path) -> list[str]:
    """Check the single shared browser path and every declared Vite owner."""
    repository = repository.resolve()
    errors = []
    config_paths = worktree_files(repository, "*/frontend/playwright.config.*")
    for module in discover_frontends(repository):
        package = json.loads((repository / module / "frontend/package.json").read_text())
        scripts = {name: command for name, command in package.get("scripts", {}).items() if name.startswith("test:e2e")}
        if not scripts:
            continue
        if any(not command.startswith(BROWSER_LAUNCHER) or any(token in {"&&", ";", "|", "||", "&"} for token in shlex.split(command)) for command in scripts.values()):
            errors.append(f"{module}: browser scripts must use the shared frontend-browser-gate launcher")
        configs = [path for path in config_paths if path.startswith(f"{module}/frontend/")]
        if not configs:
            errors.append(f"{module}: deterministic Playwright config is missing")
        for path in configs:
            source = (repository / path).read_text()
            if not re.search(rf"import\s*{{\s*withBrowserTestIsolation\s*}}\s*from\s*['\"]{re.escape(BROWSER_IMPORT)}['\"]", source) or not re.search(rf"export default defineConfig\(withBrowserTestIsolation\(['\"]{re.escape(module)}['\"],\s*{{", source):
                errors.append(f"{path}: must use shared withBrowserTestIsolation")
            if re.search(r"\b(?:webServer|baseURL|outputDir|projects)\s*:", source):
                errors.append(f"{path}: browser isolation overrides are forbidden")
        fixtures = browser_fixtures(repository, module)
        if not isinstance(fixtures, list) or not fixtures:
            errors.append(f"{module}: browser fixtures must be declared")
            continue
        owners = set()
        for fixture in fixtures:
            if not isinstance(fixture, dict):
                errors.append(f"{module}: invalid browser fixture")
                continue
            owner = fixture.get("module", "")
            ready = fixture.get("readyPath", "")
            base = fixture.get("base", "/")
            if not isinstance(owner, str) or not re.fullmatch(r"[a-z][a-z0-9-]*", owner) or owner in owners or set(fixture) - {"module", "base", "readyPath"} or not isinstance(ready, str) or not ready.startswith("/") or ready.startswith("//") or not isinstance(base, str) or not re.fullmatch(r"/[a-z0-9/-]*", base):
                errors.append(f"{module}: invalid or duplicate browser fixture")
                continue
            owners.add(owner)
            frontend = repository / owner / "frontend"
            try:
                owner_package = json.loads((frontend / "package.json").read_text())
                if not re.match(r"^vite(?:\s|$)", owner_package.get("scripts", {}).get("dev", "")):
                    raise ValueError("fixture dev script must directly launch Vite")
                vite = (frontend / "vite.config.js").read_text()
                if not re.search(rf"import\s*{{\s*withFrontendTestIsolation\s*}}\s*from\s*['\"]{re.escape(ISOLATION_IMPORT)}['\"]", vite) or not re.search(rf"export default defineConfig\((?:withModuleFrontend\(['\"]{re.escape(owner)}['\"],\s*)?withFrontendTestIsolation\(['\"]{re.escape(owner)}['\"],\s*{{", vite):
                    raise ValueError("Vite config must use the shared withFrontendTestIsolation owner")
                if re.search(r"\bhmr\s*:\s*(?:process\.env\.ADDP_E2E|!?isE2E|!?IS_E2E|!?testing)\b", vite):
                    raise ValueError("module-owned Vite test HMR branches are forbidden")
                if re.search(r"\bcacheDir\s*:", vite):
                    raise ValueError("module-owned Vite cache isolation is forbidden")
            except (ValueError, OSError, json.JSONDecodeError) as error:
                errors.append(f"{module}: fixture {owner}: {error}")
        if module not in owners:
            errors.append(f"{module}: browser fixtures must include their owner")
        for path in (repository / module / "frontend/e2e").rglob("*"):
            if path.is_file() and "online" not in path.parts and path.suffix in {".js", ".mjs"} and re.search(r"(?:127\.0\.0\.1|localhost):41\d{2}\b", path.read_text()):
                errors.append(f"{path.relative_to(repository)}: fixed browser fixture origins are forbidden")
    for path in (repository / 'scripts/test').glob('*online.py'):
        if re.search(r'"npm",\s*"run",\s*"test:e2e"', path.read_text()):
            errors.append(f"{path.relative_to(repository)}: Online must use explicit Playwright, not the deterministic browser launcher")
    return errors


def validate_registration(repository: Path) -> list[str]:
    repository = repository.resolve()
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
            if name.startswith("test:e2e")
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
            # A registered owner does not install the other frontends started by
            # its real iframe fixtures. Verify their locked CI dependencies too.
            for fixture in browser_fixtures(repository, module):
                if not isinstance(fixture, dict):
                    continue
                owner = fixture.get("module", "")
                if owner == module or not isinstance(owner, str) or not re.fullmatch(r"[a-z][a-z0-9-]*", owner):
                    continue
                relative = f"{owner}/frontend"
                matching = [step for step in steps if re.search(
                    rf"(?m)^\s*working-directory:\s*{re.escape(relative)}\s*$", step
                ) and re.search(r"(?m)^\s*run:\s*npm ci\s*$", step)]
                if matrix_selector:
                    matching = [step for step in matching if re.search(
                        rf"matrix\.module\s*==\s*['\"]{re.escape(module)}['\"]", step
                    )]
                if not matching:
                    errors.append(f"{module}: browser fixture {relative} lacks locked CI dependency installation")
            artifact_steps = [step for step in steps if "actions/upload-artifact@" in step
                              and "playwright-results-*/" in step]
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
