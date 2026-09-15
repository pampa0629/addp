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
            ) + "      - if: matrix.playwright == true\n        run: npx playwright install --with-deps chromium\n",
            encoding="utf-8",
        )

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
