#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-t2-ci-registration.py")
SPEC = importlib.util.spec_from_file_location("t2_ci_registration", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class T2CIRegistrationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        script = self.repository / "scripts/test/sample-postgres-gate.sh"
        script.parent.mkdir(parents=True)
        script.write_text("#!/usr/bin/env bash\n", encoding="utf-8")
        (self.repository / "Makefile").write_text(
            "test-sample-postgres:\n"
            "\t@bash scripts/test/sample-postgres-gate.sh\n",
            encoding="utf-8",
        )
        workflow_directory = self.repository / ".github/workflows"
        workflow_directory.mkdir(parents=True)
        self.workflow = workflow_directory / "release-and-t2-gates.yml"
        self.workflow.write_text(self._workflow_text(), encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def _workflow_text(self) -> str:
        return (
            "jobs:\n"
            "  sample:\n"
            "    services:\n"
            "      postgres:\n"
            "        image: postgres:15@sha256:" + "a" * 64 + "\n"
            "    steps:\n"
            "      - run: |\n"
            "          select-gate 'sample/backend/*' "
            "'scripts/test/sample-postgres-gate.sh'\n"
            "      - run: make test-sample-postgres\n"
        )

    def test_accepts_complete_registration(self) -> None:
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_missing_owner_and_target_registration(self) -> None:
        self.workflow.write_text(
            "jobs:\n  sample:\n    services:\n      postgres:\n"
            "        image: postgres:15@sha256:" + "a" * 64 + "\n",
            encoding="utf-8",
        )
        errors = MODULE.validate_registration(self.repository)
        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: GitHub Actions target "
            "test-sample-postgres is missing",
            errors,
        )
        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: owner path sample/backend/* is missing",
            errors,
        )


if __name__ == "__main__":
    unittest.main()
