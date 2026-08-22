#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
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
            "      - run: select 'common-frontend/*' '.github/actions/prepare-frontend-gate/*'\n"
            "      - uses: ./.github/actions/prepare-frontend-gate\n",
            encoding="utf-8",
        )
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def test_accepts_complete_registration(self) -> None:
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_missing_workflow_target(self) -> None:
        self.workflow.write_text("jobs: {}\n", encoding="utf-8")
        errors = MODULE.validate_registration(self.repository)
        self.assertIn("sample: GitHub Actions target test-sample-frontend is missing", errors)
        self.assertIn("sample: GitHub Actions path registration is missing", errors)

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

    def test_rejects_missing_shared_and_setup_change_paths(self) -> None:
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "      - run: select 'common-frontend/*' '.github/actions/prepare-frontend-gate/*'\n",
                "      - run: select\n",
            ),
            encoding="utf-8",
        )
        errors = MODULE.validate_registration(self.repository)
        self.assertIn(
            "sample: frontend gate setup change path is missing from test-sample-frontend job",
            errors,
        )
        self.assertIn(
            "sample: shared path common-frontend/* is missing from test-sample-frontend job",
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
