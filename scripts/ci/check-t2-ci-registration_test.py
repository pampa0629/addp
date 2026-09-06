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
            "test-integration:\n"
            "\t@$(MAKE) test-sample-postgres\n\n"
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
            "      - name: Select sample gate\n"
            "        id: sample\n"
            "        run: python3 scripts/ci/select-module-gate.py --module sample\n"
            "      - name: Run sample gate\n"
            "        run: make test-sample-postgres\n"
        )

    def _add_mysql_gate(self, image: str) -> None:
        script = self.repository / "scripts/test/common-mysql-data-protection-gate.sh"
        script.write_text("#!/usr/bin/env bash\n", encoding="utf-8")
        (self.repository / "Makefile").write_text(
            "test-integration:\n"
            "\t@$(MAKE) test-sample-postgres\n"
            "\t@$(MAKE) test-common-mysql-data-protection\n\n"
            "test-sample-postgres:\n"
            "\t@bash scripts/test/sample-postgres-gate.sh\n\n"
            "test-common-mysql-data-protection:\n"
            "\t@bash scripts/test/common-mysql-data-protection-gate.sh\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            self._workflow_text()
            + "  common-mysql-data-protection:\n"
            + "    services:\n"
            + "      mysql:\n"
            + f"        image: {image}\n"
            + "    steps:\n"
            + "      - name: Select common gate\n"
            + "        id: common\n"
            + "        run: python3 scripts/ci/select-module-gate.py --module common\n"
            + "      - name: Run MySQL gate\n"
            + "        run: make test-common-mysql-data-protection\n",
            encoding="utf-8",
        )

    def test_accepts_complete_registration(self) -> None:
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_accepts_pinned_mysql_gate(self) -> None:
        self._add_mysql_gate("mysql:8.0@sha256:" + "b" * 64)
        self.assertEqual([], MODULE.validate_registration(self.repository))

    def test_rejects_unpinned_mysql_gate(self) -> None:
        self._add_mysql_gate("mysql:8.0")
        self.assertIn(
            "scripts/test/common-mysql-data-protection-gate.sh: MySQL 8 service image "
            "is not pinned in test-common-mysql-data-protection job",
            MODULE.validate_registration(self.repository),
        )

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
            "scripts/test/sample-postgres-gate.sh: shared module change selector is missing",
            errors,
        )

    def test_rejects_selector_registered_in_another_selection_step(self) -> None:
        self.workflow.write_text(
            self._workflow_text().replace(
                "        run: python3 scripts/ci/select-module-gate.py --module sample\n",
                "        run: true\n"
                "      - name: Select unrelated gate\n"
                "        id: unrelated\n"
                "        run: python3 scripts/ci/select-module-gate.py --module sample\n",
            ),
            encoding="utf-8",
        )
        errors = MODULE.validate_registration(self.repository)
        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: shared module change selector is missing",
            errors,
        )

    def test_rejects_unpinned_postgres_in_target_job(self) -> None:
        self.workflow.write_text(
            self._workflow_text().replace("postgres:15@sha256:" + "a" * 64, "postgres:15"),
            encoding="utf-8",
        )
        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: PostgreSQL 15 service image is not pinned "
            "in test-sample-postgres job",
            MODULE.validate_registration(self.repository),
        )

    def test_rejects_gate_missing_from_integration_aggregate(self) -> None:
        (self.repository / "Makefile").write_text(
            "test-integration:\n\n"
            "test-sample-postgres:\n"
            "\t@bash scripts/test/sample-postgres-gate.sh\n",
            encoding="utf-8",
        )
        self.assertIn(
            "scripts/test/sample-postgres-gate.sh: root test-integration does not invoke "
            "test-sample-postgres sequentially",
            MODULE.validate_registration(self.repository),
        )

    def test_detects_untracked_gate_before_commit(self) -> None:
        script = self.repository / "scripts/test/new-postgres-gate.sh"
        script.write_text("#!/usr/bin/env bash\n", encoding="utf-8")
        errors = MODULE.validate_registration(self.repository)
        self.assertIn(
            "scripts/test/new-postgres-gate.sh: Makefile target test-new-postgres is missing",
            errors,
        )


if __name__ == "__main__":
    unittest.main()
