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
            "      - run: python3 scripts/ci/select-module-gate.py --module sample\n"
            "      - uses: ./.github/actions/prepare-python-gate\n"
            "      - run: make test-sample\n",
            encoding="utf-8",
        )
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def test_accepts_complete_registration(self) -> None:
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_registered_engine_target_requires_root_and_own_ci_job(self) -> None:
        manifest = self.repository / "engines/model3d-workflow/requirements.txt"
        manifest.parent.mkdir(parents=True)
        manifest.write_text("Pillow==12.3.0\n", encoding="utf-8")
        subprocess.run(["git", "add", str(manifest.relative_to(self.repository))], cwd=self.repository, check=True)
        makefile = self.repository / "Makefile"
        makefile.write_text(makefile.read_text() + "\ntest-model3d-workflow:\n\t@true\n")
        errors = MODULE.validate_registration(self.repository)
        self.assertIn("engines/model3d-workflow/requirements.txt: root test dependency test-model3d-workflow is missing", errors)
        self.assertIn("engines/model3d-workflow/requirements.txt: GitHub Actions target test-model3d-workflow is missing", errors)
        makefile.write_text(makefile.read_text().replace("test: test-sample", "test: test-sample test-model3d-workflow"))
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(workflow.read_text() + "  model3d-tests:\n    steps:\n      - run: python3 scripts/ci/select-module-gate.py --module engines\n      - uses: ./.github/actions/prepare-python-gate\n      - run: make test-model3d-workflow\n")
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
            "      - run: python3 scripts/ci/select-module-gate.py --module sample\n"
            "      - run: make test-sample\n",
            encoding="utf-8",
        )
        self.assertIn(
            "sample/backend/requirements.txt: Python gate setup action is missing from test-sample job",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_shared_selector_registered_in_another_job(self) -> None:
        workflow = self.repository / ".github/workflows/platform-ci.yml"
        workflow.write_text(
            "jobs:\n"
            "  sample-tests:\n"
            "    steps:\n"
            "      - run: true\n"
            "      - uses: ./.github/actions/prepare-python-gate\n"
            "      - run: make test-sample\n"
            "  unrelated:\n"
            "    steps:\n"
            "      - run: python3 scripts/ci/select-module-gate.py --module sample\n",
            encoding="utf-8",
        )
        self.assertIn(
            "sample/backend/requirements.txt: shared module change selector is missing",
            MODULE.validate_registration(self.repository),
        )


if __name__ == "__main__":
    unittest.main()
