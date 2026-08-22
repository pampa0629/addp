#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-python-ci-registration.py")
SPEC = importlib.util.spec_from_file_location("python_ci_registration", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class PythonCIRegistrationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        manifest = self.repository / "sample/backend/requirements.txt"
        manifest.parent.mkdir(parents=True)
        manifest.write_text("-e ../../common-python[dev]\n", encoding="utf-8")
        (self.repository / "Makefile").write_text(
            "test: test-sample\n\ntest-sample:\n\t@true\n", encoding="utf-8"
        )
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.parent.mkdir(parents=True)
        workflow.write_text(
            "jobs:\n"
            "  sample-tests:\n"
            "    steps:\n"
            "      - run: select 'sample/backend/*' 'common-python/*'\n"
            "      - uses: ./.github/actions/prepare-python-gate\n"
            "      - run: make test-sample\n",
            encoding="utf-8",
        )
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def test_accepts_complete_registration(self) -> None:
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_missing_root_and_workflow_registration(self) -> None:
        (self.repository / "Makefile").write_text(
            "test:\n\ntest-sample:\n\t@true\n", encoding="utf-8"
        )
        (self.repository / ".github/workflows/platform-ci.yml").write_text(
            "jobs: {}\n", encoding="utf-8"
        )
        errors = MODULE.validate_registration(self.repository)
        self.assertIn(
            "sample/backend/requirements.txt: root test dependency test-sample is missing",
            errors,
        )
        self.assertIn(
            "sample/backend/requirements.txt: GitHub Actions target test-sample is missing",
            errors,
        )

    def test_rejects_gate_without_standard_environment_setup(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(
            "jobs:\n"
            "  sample-tests:\n"
            "    steps:\n"
            "      - run: select 'sample/backend/*' 'common-python/*'\n"
            "      - run: make test-sample\n",
            encoding="utf-8",
        )
        self.assertIn(
            "sample/backend/requirements.txt: Python gate setup action is missing from test-sample job",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_shared_path_registered_in_another_job(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(
            "jobs:\n"
            "  sample-tests:\n"
            "    steps:\n"
            "      - run: select 'sample/backend/*'\n"
            "      - uses: ./.github/actions/prepare-python-gate\n"
            "      - run: make test-sample\n"
            "  unrelated:\n"
            "    steps:\n"
            "      - run: select 'common-python/*'\n",
            encoding="utf-8",
        )
        self.assertIn(
            "sample/backend/requirements.txt: shared path common-python/* is missing",
            MODULE.validate_registration(self.repository),
        )


if __name__ == "__main__":
    unittest.main()
