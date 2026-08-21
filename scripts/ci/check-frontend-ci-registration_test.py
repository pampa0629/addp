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
            '{"scripts":{"build":"vite build","test":"vitest run"}}\n',
            encoding="utf-8",
        )
        (self.repository / ".github/workflows").mkdir(parents=True)
        (self.repository / "Makefile").write_text(
            "test-sample-frontend:\n\t@cd sample/frontend && npm test\n",
            encoding="utf-8",
        )
        self.workflow = self.repository / ".github/workflows/platform-ci.yml"
        self.workflow.write_text(
            "matrix:\n  include:\n    - module: sample\n"
            "      target: test-sample-frontend\n",
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


if __name__ == "__main__":
    unittest.main()
