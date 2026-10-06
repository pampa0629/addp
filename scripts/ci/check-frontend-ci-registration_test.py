#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import json
import subprocess
import tempfile
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
        package["scripts"]["test:e2e"] = "playwright test"
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
                "          path: ${{ runner.temp }}/addp-${{ matrix.module }}-playwright-results/\n"
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
            "export default defineConfig({\n  use: { baseURL: 'http://127.0.0.1:4199' },\n  webServer: {\n"
            "    command: 'ADDP_E2E=1 npm run dev -- --host 127.0.0.1 --port 4199 --strictPort',\n"
            "    url: 'http://127.0.0.1:4199/login',\n"
            "    reuseExistingServer: false,\n    gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 },\n  }\n})\n"
        )
        package_path = frontend / "package.json"
        package = json.loads(package_path.read_text())
        package["scripts"]["dev"] = "vite"
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
        for prefix in ("../../other/frontend", "'../../other/frontend'"):
            self.playwright.write_text("export default defineConfig({\n  use: { baseURL: 'http://127.0.0.1:4198' },\n  webServer: {\n"
                + f"    command: \"ADDP_E2E=1 npm --prefix {prefix} run dev -- --host 127.0.0.1 --port 4198 --strictPort\",\n"
                + "    url: 'http://127.0.0.1:4198/login',\n    reuseExistingServer: false,\n"
                + "    gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 },\n  }\n})\n")
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
        self.playwright.write_text(self.playwright.read_text().replace("reuseExistingServer: false", "reuseExistingServer: true"))
        self.assertTrue(MODULE.validate_browser_isolation(self.repository))

    def test_rejects_unsafe_browser_isolation(self) -> None:
        cases = [
            ("playwright", "ADDP_E2E=1 ", "", "ADDP_E2E=1"),
            ("playwright", "ADDP_E2E=1 ", "ADDP_E2E=1 ADDP_E2E=0 ", "ADDP_E2E=1"),
            ("playwright", "--strictPort'", "--strictPort && npm run dev'", "single Vite fixture"),
            ("playwright", " --strictPort", "", "--strictPort"),
            ("playwright", "reuseExistingServer: false", "reuseExistingServer: true", "must not reuse"),
            ("playwright", "gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 },", "", "bounded SIGTERM"),
            ("playwright", "--port 4199", "--port 5199", "distinct from development"),
            ("playwright", "127.0.0.1:4199", "127.0.0.1:4200", "URL must match"),
            ("playwright", "--host 127.0.0.1", "--host 0.0.0.0", "loopback"),
            ("playwright", "baseURL: 'http://127.0.0.1:4199'", "baseURL: 'http://127.0.0.1:5199'", "browser baseURL"),
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
        self.playwright.write_text(self.playwright.read_text().replace(
            "ADDP_E2E=1 npm run dev", "ADDP_E2E=1 npm --prefix ../../other/frontend run dev"
        ))
        self.assertTrue(any("shared withFrontendTestIsolation" in error
                            for error in MODULE.validate_browser_isolation(self.repository)))

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


if __name__ == "__main__":
    unittest.main()
