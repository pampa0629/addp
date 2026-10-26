#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import json
import os
import re
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-frontend-ci-registration.py")
SPEC = importlib.util.spec_from_file_location("frontend_ci_registration", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class FrontendCIRegistrationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        frontend = self.repository / "sample/frontend"
        frontend.mkdir(parents=True)
        (frontend / "package.json").write_text(
            '{"scripts":{"build":"vite build","test":"vitest run"},'
            '"dependencies":{"@addp/common-frontend":"file:../../common-frontend"}}\n',
            encoding="utf-8",
        )
        (self.repository / ".github/workflows").mkdir(parents=True)
        (self.repository / "Makefile").write_text(
            "test: test-sample-frontend\n\n"
            "test-sample-frontend:\n\t@cd sample/frontend && npm test\n",
            encoding="utf-8",
        )
        self.workflow = self.repository / ".github/workflows/platform-ci.yml"
        self.workflow.write_text(
            "jobs:\n"
            "  frontend-tests:\n"
            "    matrix:\n"
            "      include:\n"
            "        - module: sample\n"
            "          target: test-sample-frontend\n"
            "    steps:\n"
            "      - run: python3 scripts/ci/select-module-gate.py --module '${{ matrix.module }}'\n"
            "      - uses: ./.github/actions/prepare-frontend-gate\n",
            encoding="utf-8",
        )
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def test_accepts_complete_registration(self) -> None:
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def enable_browser_suite(self) -> None:
        self.write_isolated_fixture()
        path = self.repository / "sample/frontend/package.json"
        package = json.loads(path.read_text())
        package["scripts"]["test:e2e"] = MODULE.BROWSER_LAUNCHER
        path.write_text(json.dumps(package), encoding="utf-8")
        makefile = self.repository / "Makefile"
        makefile.write_text(makefile.read_text() + "\t@cd sample/frontend && npm run test:e2e\n", encoding="utf-8")
        self.workflow.write_text(
            self.workflow.read_text().replace(
                "          target: test-sample-frontend\n",
                "          target: test-sample-frontend\n          playwright: true\n",
            ) + (
                "      - if: matrix.playwright == true\n        run: npx playwright install --with-deps chromium\n"
                "      - name: Run frontend gate\n"
                "        env:\n"
                "          TMPDIR: ${{ runner.temp }}\n"
                "        run: make ${{ matrix.target }}\n"
                "      - name: Upload browser failure evidence\n"
                "        if: failure() && matrix.playwright == true && steps.selection.outputs.run == 'true'\n"
                "        uses: actions/upload-artifact@fixed-test-ref\n"
                "        with:\n"
                "          path: ${{ runner.temp }}/addp-${{ matrix.module }}-playwright-results-*/\n"
            ),
            encoding="utf-8",
        )

    def write_isolated_fixture(self) -> None:
        frontend = self.repository / "sample/frontend"
        self.vite = frontend / "vite.config.js"
        self.vite.write_text(
            "import { withFrontendTestIsolation } from '../../common-frontend/basic/src/utils/viteTestIsolation.mjs'\n"
            "export default defineConfig(withFrontendTestIsolation('sample', {\n"
            "  server: { port: Number(process.env.SAMPLE_FE_PORT || 5199) }\n"
            "}))\n"
        )
        self.playwright = frontend / "playwright.config.js"
        self.playwright.write_text(
            "import { withBrowserTestIsolation } from '../../common-frontend/basic/src/utils/browserTestIsolation.mjs'\n"
            "export default defineConfig(withBrowserTestIsolation('sample', {\n  use: { headless: true },\n}))\n"
        )
        package_path = frontend / "package.json"
        package = json.loads(package_path.read_text())
        package["scripts"]["dev"] = "vite"
        package["addpBrowserTest"] = {"fixtures": [{"module": "sample", "readyPath": "/login", "base": "/"}]}
        package_path.write_text(json.dumps(package))
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)

    def test_public_module_entry_preserves_shared_browser_isolation(self) -> None:
        self.enable_browser_suite()
        self.write_isolated_fixture()
        self.vite.write_text(self.vite.read_text().replace(
            "defineConfig(withFrontendTestIsolation('sample', {",
            "defineConfig(withModuleFrontend('sample', withFrontendTestIsolation('sample', {",
        ).replace("}))", "})))"))
        self.assertEqual([], MODULE.validate_browser_isolation(self.repository))
        self.vite.write_text(self.vite.read_text().replace(
            "withFrontendTestIsolation('sample', {", "{",
        ))
        self.assertTrue(MODULE.validate_browser_isolation(self.repository))

    def test_cross_frontend_fixture_requires_its_own_locked_ci_installation(self) -> None:
        self.enable_browser_suite()
        other = self.repository / "other/frontend"
        other.mkdir(parents=True)
        (other / "package.json").write_text('{"scripts":{"dev":"vite"}}')
        (other / "vite.config.js").write_text(self.vite.read_text().replace("'sample'", "'other'"))
        package_path = self.repository / "sample/frontend/package.json"
        package = json.loads(package_path.read_text())
        package["addpBrowserTest"]["fixtures"].append({"module": "other", "readyPath": "/login"})
        package_path.write_text(json.dumps(package))
        errors = MODULE.validate_registration(self.repository)
        self.assertTrue(any("other/frontend lacks locked CI dependency installation" in error for error in errors), errors)
        install = "      - name: Install iframe owner\n        if: matrix.module == 'sample'\n        working-directory: other/frontend\n        run: npm ci\n"
        original = self.workflow.read_text()
        self.workflow.write_text(original + install)
        self.assertEqual([], MODULE.validate_registration(self.repository))
        self.workflow.write_text(original + install.replace("matrix.module == 'sample'", "matrix.module == 'another'"))
        self.assertTrue(any("other/frontend lacks locked CI dependency installation" in error for error in MODULE.validate_registration(self.repository)))

    def test_browser_failure_evidence_matches_the_gate_and_matrix(self) -> None:
        self.enable_browser_suite()
        original = self.workflow.read_text()
        self.assertEqual([], MODULE.validate_registration(self.repository))
        for old, new, message in [
            ("          TMPDIR: ${{ runner.temp }}\n", "", "TMPDIR must match"),
            ("TMPDIR: ${{ runner.temp }}", "TMPDIR: /tmp", "TMPDIR must match"),
            ("matrix.playwright == true", "matrix.module == 'security'", "cover the Playwright matrix"),
            ("uses: actions/upload-artifact@fixed-test-ref", "run: true", "must upload browser failure evidence"),
        ]:
            with self.subTest(message=message, replacement=new):
                self.workflow.write_text(original.replace(old, new))
                self.assertTrue(any(message in error for error in MODULE.validate_registration(self.repository)))

    def test_worktree_browser_config_is_checked_before_staging(self) -> None:
        self.enable_browser_suite()
        subprocess.run(["git", "reset", "-q", "--", "sample/frontend/playwright.config.js"], cwd=self.repository, check=True)
        self.assertEqual([], MODULE.validate_browser_isolation(self.repository))
        self.playwright.write_text(self.playwright.read_text().replace("headless: true", "headless: true, webServer: {}"))
        self.assertTrue(MODULE.validate_browser_isolation(self.repository))

    def test_rejects_unsafe_browser_isolation(self) -> None:
        cases = [
            ("playwright", "withBrowserTestIsolation('sample', {", "{", "shared withBrowserTestIsolation"),
            ("playwright", "headless: true", "headless: true, baseURL: 'http://127.0.0.1:4199'", "overrides"),
            ("playwright", "headless: true", "headless: true, webServer: {}", "overrides"),
            ("playwright", "headless: true", "headless: true, outputDir: '/tmp/shared'", "overrides"),
            ("vite", "withFrontendTestIsolation('sample', {", "{", "shared withFrontendTestIsolation"),
            ("vite", "server: {", "server: { hmr: isE2E ? false : true,", "test HMR"),
            ("vite", "server: {", "cacheDir: 'node_modules/.vite-e2e', server: {", "module-owned"),
        ]
        for file_name, old, new, message in cases:
            with self.subTest(message=message):
                self.enable_browser_suite()
                path = getattr(self, file_name)
                path.write_text(path.read_text().replace(old, new))
                self.assertTrue(any(message in error for error in MODULE.validate_registration(self.repository)))

    def test_checks_cross_module_fixture_servers(self) -> None:
        self.enable_browser_suite()
        other = self.repository / "other/frontend"
        other.mkdir(parents=True)
        (other / "package.json").write_text('{"scripts":{"dev":"vite"}}')
        (other / "vite.config.js").write_text('export default defineConfig({})')
        package_path = self.repository / "sample/frontend/package.json"
        package = json.loads(package_path.read_text())
        package["addpBrowserTest"]["fixtures"].append({"module": "other", "readyPath": "/"})
        package_path.write_text(json.dumps(package))
        self.assertTrue(any("shared withFrontendTestIsolation" in error
                            for error in MODULE.validate_browser_isolation(self.repository)))

    def test_rejects_legacy_launcher_and_fixed_origins(self) -> None:
        self.enable_browser_suite()
        package_path = self.repository / "sample/frontend/package.json"
        package = json.loads(package_path.read_text())
        package["scripts"]["test:e2e"] = "playwright test"
        package_path.write_text(json.dumps(package))
        self.assertTrue(any("shared frontend-browser-gate launcher" in error for error in MODULE.validate_registration(self.repository)))
        package["scripts"]["test:e2e"] = MODULE.BROWSER_LAUNCHER
        package_path.write_text(json.dumps(package))
        tests = self.repository / "sample/frontend/e2e"
        tests.mkdir()
        (tests / "fixed.spec.js").write_text("page.goto('http://127.0.0.1:4199/login')")
        self.assertTrue(any("fixed browser fixture origins" in error for error in MODULE.validate_registration(self.repository)))

    def test_browser_routes_cannot_use_personal_development_ports(self) -> None:
        self.enable_browser_suite()
        tests = self.repository / "sample/frontend/e2e"
        tests.mkdir()
        path = tests / "route.spec.js"
        for source in ["page.route('http://127.0.0.1:5176/**', handler)",
                       'context.route("http://localhost:5185/**", handler)']:
            path.write_text(source)
            self.assertTrue(any("fixed browser fixture origins" in error
                                for error in MODULE.validate_browser_isolation(self.repository)))
        path.write_text("const endpoint = 'https://127.0.0.1:19091/metrics'; context.route('**/module-ui/sample/**', handler)")
        self.assertEqual([], MODULE.validate_browser_isolation(self.repository))

    def test_rejects_invalid_or_duplicate_fixture_declarations(self) -> None:
        self.enable_browser_suite()
        package_path = self.repository / "sample/frontend/package.json"
        package = json.loads(package_path.read_text())
        for fixtures in [[], [{"module": "sample", "readyPath": "//external.invalid/"}],
                         [{"module": 123, "readyPath": "/"}],
                         [{"module": "sample", "readyPath": "/", "port": 4199}],
                         [{"module": "sample", "readyPath": "/"}] * 2]:
            package["addpBrowserTest"]["fixtures"] = fixtures
            package_path.write_text(json.dumps(package))
            self.assertTrue(MODULE.validate_registration(self.repository), fixtures)

    def test_online_cannot_borrow_the_deterministic_launcher(self) -> None:
        self.enable_browser_suite()
        path = self.repository / 'scripts/test/sample-online.py'
        path.parent.mkdir(parents=True)
        path.write_text('["npm", "run", "test:e2e", "--", "--config=playwright.online.config.js"]')
        self.assertTrue(any('Online must use explicit Playwright' in error for error in MODULE.validate_registration(self.repository)))

    def test_rejects_missing_browser_config(self) -> None:
        self.enable_browser_suite()
        self.playwright.unlink()
        subprocess.run(["git", "rm", "--cached", "sample/frontend/playwright.config.js"],
                       cwd=self.repository, check=True, capture_output=True)
        self.assertIn("sample: deterministic Playwright config is missing",
                      MODULE.validate_registration(self.repository))

    def test_accepts_registered_browser_suite(self) -> None:
        self.enable_browser_suite()
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_browser_suite_missing_from_root_gate(self) -> None:
        self.enable_browser_suite()
        path = self.repository / "Makefile"
        path.write_text(path.read_text().replace(" && npm run test:e2e", " && npm test"))
        self.assertIn("sample: root frontend gate must run a declared Playwright suite", MODULE.validate_registration(self.repository))

    def test_rejects_missing_browser_installation(self) -> None:
        self.enable_browser_suite()
        self.workflow.write_text(self.workflow.read_text().replace("npx playwright install --with-deps chromium", "true"))
        self.assertIn("sample: frontend CI job must install Chromium", MODULE.validate_registration(self.repository))

    def test_rejects_browser_matrix_flag_removed_from_one_module(self) -> None:
        self.enable_browser_suite()
        self.workflow.write_text(self.workflow.read_text().replace("          playwright: true\n", ""))
        self.assertIn("sample: frontend CI matrix must enable Playwright", MODULE.validate_registration(self.repository))

    def test_rejects_missing_workflow_target(self) -> None:
        self.workflow.write_text("jobs: {}\n", encoding="utf-8")
        errors = MODULE.validate_registration(self.repository)
        self.assertIn("sample: GitHub Actions target test-sample-frontend is missing", errors)
        self.assertIn("sample: shared module change selector is missing", errors)

    def test_rejects_missing_root_test_dependency(self) -> None:
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "test: test-sample-frontend", "test:"
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "sample: root test target dependency test-sample-frontend is missing",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_gate_without_standard_environment_setup(self) -> None:
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "      - uses: ./.github/actions/prepare-frontend-gate\n", ""
            ),
            encoding="utf-8",
        )
        self.assertIn(
            "sample: standard frontend gate setup is missing from test-sample-frontend job",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_missing_shared_module_change_selector(self) -> None:
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "      - run: python3 scripts/ci/select-module-gate.py --module '${{ matrix.module }}'\n",
                "      - run: true\n",
            ),
            encoding="utf-8",
        )
        errors = MODULE.validate_registration(self.repository)
        self.assertIn(
            "sample: shared module change selector is missing from test-sample-frontend job",
            errors,
        )

    def test_requires_agent_frontend_directly_in_root_aggregate(self) -> None:
        frontend = self.repository / "agent/frontend"
        frontend.mkdir(parents=True)
        (frontend / "package.json").write_text(
            '{"scripts":{"build":"vite build","test":"vitest run"}}\n',
            encoding="utf-8",
        )
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8").replace(
                "test: test-sample-frontend",
                "test: test-sample-frontend test-agent-eval",
            )
            + "\ntest-agent-eval:\n\t@echo agent\n"
            + "\ntest-agent-frontend:\n\t@cd agent/frontend && npm test\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8")
            .replace(
                "        - module: sample\n",
                "        - module: agent\n"
                "          target: test-agent-frontend\n"
                "        - module: sample\n",
            ),
            encoding="utf-8",
        )
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        self.assertIn(
            "agent: root test target dependency test-agent-frontend is missing",
            MODULE.validate_registration(self.repository),
        )


class FrontendJobBudgetTest(unittest.TestCase):
    def job(self) -> str:
        return """  frontend-deterministic:
    timeout-minutes: ${{ matrix.timeout_minutes || 20 }}
    strategy:
      matrix:
        include:
          - module: monitor
            target: test-monitor-frontend
            timeout_minutes: 30
          - module: console
            target: test-console-frontend
          - module: sample
            target: test-sample-frontend
    steps:
      - run: make ${{ matrix.target }}
"""

    def test_accepts_only_bounded_monitor_exception_and_console_twenty_minutes(self) -> None:
        self.assertEqual([], MODULE.validate_frontend_job_budgets([self.job()]))
        explicit_console = self.job().replace('target: test-console-frontend',
                                             'target: test-console-frontend\n            timeout_minutes: 20')
        self.assertEqual([], MODULE.validate_frontend_job_budgets([explicit_console]))
        self.assertEqual([], MODULE.validate_frontend_job_budgets(["  unrelated:\n    timeout-minutes: 60\n"]))

    def test_rejects_lost_timeout_and_owner_expansion(self) -> None:
        for before, after in [
            ("${{ matrix.timeout_minutes || 20 }}", "30"),
            ("${{ matrix.timeout_minutes || 20 }}", "${{ matrix.timeout_minutes || 30 }}"),
            ("            timeout_minutes: 30\n", ""),
            ("timeout_minutes: 30", "timeout_minutes: 20"),
            ("timeout_minutes: 30", "timeout_minutes: 60"),
            ("timeout_minutes: 30", "timeout_minutes: invalid"),
            ("timeout_minutes: 30", "timeout_minutes: 30\n            timeout_minutes: 60"),
            ("target: test-sample-frontend", "target: test-sample-frontend\n            timeout_minutes: 30"),
            *[("target: test-console-frontend", f"target: test-console-frontend\n            timeout_minutes: {value}")
              for value in ['30', '60', 'invalid', '20\n            timeout_minutes: 20']],
        ]:
            with self.subTest(after=after):
                self.assertTrue(MODULE.validate_frontend_job_budgets([self.job().replace(before, after)]))

    def test_console_and_monitor_entries_are_required_once(self) -> None:
        for owner in ['monitor', 'console']:
            entry = f"          - module: {owner}\n            target: test-{owner}-frontend\n"
            if owner == 'monitor':
                entry += '            timeout_minutes: 30\n'
            for replacement in ['', entry + entry]:
                with self.subTest(owner=owner, replacement=replacement):
                    self.assertTrue(MODULE.validate_frontend_job_budgets([self.job().replace(entry, replacement)]))


class ConsoleBrowserPreparationTest(unittest.TestCase):
    def job(self) -> str:
        return next(job for job in MODULE.workflow_jobs(SCRIPT.parents[2])
                    if job.startswith("  frontend-deterministic:"))

    def script(self) -> str:
        step = next(step for step in re.split(r"(?m)^      - ", self.job())
                    if step.startswith("name: Configure Console Ubuntu archive\n"))
        return textwrap.dedent(step.split("        run: |\n", 1)[1])

    def test_preserves_full_installation_and_console_only_scope(self) -> None:
        self.assertEqual([], MODULE.validate_console_browser_preparation([self.job()]))
        for before, after in (
            ("name: Configure Console Ubuntu archive", "name: Unregistered archive choice"),
            ("matrix.module == 'console'", "matrix.playwright == true"),
            ("https://archive.ubuntu.com/ubuntu/", "http://azure.archive.ubuntu.com/ubuntu/"),
            ("test -f /etc/apt/apt-mirrors.txt", "true"),
            ("grep -Fxq 'URIs: mirror+file:/etc/apt/apt-mirrors.txt'", "echo"),
            ("playwright install --with-deps chromium", "playwright install chromium"),
        ):
            with self.subTest(after=after):
                self.assertTrue(MODULE.validate_console_browser_preparation([self.job().replace(before, after)]))
        self.assertEqual([], MODULE.validate_console_browser_preparation(["  unrelated:\n"]))

    def run_preparation(self, directory: Path, *, mirror=True, expected_source=True, diagnostics_ok=True):
        apt = directory / "apt"
        (apt / "sources.list.d").mkdir(parents=True)
        original = ("http://azure.archive.ubuntu.com/ubuntu/\tpriority:1\n"
                    "https://archive.ubuntu.com/ubuntu/\tpriority:2\n"
                    "https://security.ubuntu.com/ubuntu/\tpriority:3\n")
        if mirror:
            (apt / "apt-mirrors.txt").write_text(original)
        uri = f"mirror+file:{apt}/apt-mirrors.txt" if expected_source else "https://example.invalid/ubuntu/"
        source = (f"Types: deb\nURIs: {uri}\nSuites: noble noble-updates noble-backports noble-security\n"
                  "Components: main universe restricted multiverse\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n")
        (apt / "sources.list.d/ubuntu.sources").write_text(source)
        other = apt / "sources.list.d/other.list"
        other.write_text("deb [signed-by=/other.gpg] https://example.invalid/other noble main\n")
        binaries = directory / "bin"
        binaries.mkdir()
        for name, body in {"sudo": 'exec "$@"', "apt-config": "exit 0" if diagnostics_ok else "exit 9"}.items():
            command = binaries / name
            command.write_text("#!/bin/sh\n" + body + "\n")
            command.chmod(0o755)
        result = subprocess.run(["bash", "-c", self.script().replace("/etc/apt/", str(apt) + "/")],
                                env={**os.environ, "PATH": str(binaries) + os.pathsep + os.environ["PATH"]},
                                text=True, capture_output=True)
        self.assertEqual(source, (apt / "sources.list.d/ubuntu.sources").read_text())
        self.assertEqual("deb [signed-by=/other.gpg] https://example.invalid/other noble main\n", other.read_text())
        return result, apt / "apt-mirrors.txt", original

    def test_changes_only_mirror_list_preserving_suites_components_and_signature(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            result, mirror, _ = self.run_preparation(Path(directory))
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertEqual("https://archive.ubuntu.com/ubuntu/\n", mirror.read_text())

    def test_rejects_missing_or_unexpected_runner_sources_before_mutation(self) -> None:
        for arguments in ({"mirror": False}, {"expected_source": False}, {"diagnostics_ok": False}):
            with self.subTest(arguments=arguments), tempfile.TemporaryDirectory() as directory:
                result, mirror, original = self.run_preparation(Path(directory), **arguments)
                self.assertNotEqual(0, result.returncode)
                self.assertEqual(original if arguments.get("mirror", True) else None,
                                 mirror.read_text() if mirror.exists() else None)


if __name__ == "__main__":
    unittest.main()
