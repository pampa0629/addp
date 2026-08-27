#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("module-gate.py")
SPEC = importlib.util.spec_from_file_location("module_gate", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class ModuleGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        for relative_path in (
            "sample/backend/go.mod",
            "sample/frontend/package.json",
            "scripts/test/sample-postgres-gate.sh",
        ):
            path = self.repository / relative_path
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("{}\n", encoding="utf-8")
        (self.repository / "Makefile").write_text(
            "test-platform:\n\t@true\n"
            "test-sample-eval:\n\t@true\n"
            "test-sample-frontend:\n\t@true\n"
            "test-sample-postgres:\n\t@true\n",
            encoding="utf-8",
        )
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def test_plans_platform_and_discovered_module_gates(self) -> None:
        steps = MODULE.plan_module(self.repository, "sample")
        self.assertEqual(
            [
                ("make", "test-platform"),
                ("go", "test", "./..."),
                ("make", "test-sample-eval"),
                ("make", "test-sample-frontend"),
                ("make", "test-sample-postgres"),
            ],
            [step.command for step in steps],
        )
        self.assertEqual((("GOWORK", "off"),), steps[1].environment)
        self.assertEqual(
            ("*_POSTGRES_TEST_DSN", "ADDP_POSTGRES_INTEGRATION"),
            steps[1].excluded_environment,
        )

    def test_discovers_untracked_module_before_first_commit(self) -> None:
        for relative_path in (
            "fresh/backend/go.mod",
            "fresh/frontend/package.json",
            "scripts/test/fresh-postgres-gate.sh",
        ):
            path = self.repository / relative_path
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("{}\n", encoding="utf-8")
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8")
            + "test-fresh-frontend:\n\t@true\n"
            + "test-fresh-postgres:\n\t@true\n",
            encoding="utf-8",
        )

        steps = MODULE.plan_module(self.repository, "fresh")

        self.assertEqual(
            [
                ("make", "test-platform"),
                ("go", "test", "./..."),
                ("make", "test-fresh-frontend"),
                ("make", "test-fresh-postgres"),
            ],
            [step.command for step in steps],
        )

    def test_go_t1_environment_excludes_postgres_integration_opt_ins(self) -> None:
        step = MODULE.Step(
            "sample Go T1",
            ("go", "test", "./..."),
            self.repository,
            (("GOWORK", "off"),),
            MODULE.GO_T1_EXCLUDED_ENVIRONMENT,
        )

        environment = MODULE.step_environment(
            step,
            {
                "ADDP_SYSTEM_POSTGRES_TEST_DSN": "postgres://system-test",
                "STANDARD_POSTGRES_TEST_DSN": "postgres://standard-test",
                "ADDP_POSTGRES_INTEGRATION": "1",
                "UNRELATED_TEST_FLAG": "kept",
            },
        )

        self.assertNotIn("ADDP_SYSTEM_POSTGRES_TEST_DSN", environment)
        self.assertNotIn("STANDARD_POSTGRES_TEST_DSN", environment)
        self.assertNotIn("ADDP_POSTGRES_INTEGRATION", environment)
        self.assertEqual("kept", environment["UNRELATED_TEST_FLAG"])
        self.assertEqual("off", environment["GOWORK"])

    def test_rejects_unknown_module(self) -> None:
        with self.assertRaisesRegex(MODULE.ModuleGateError, "unknown MODULE"):
            MODULE.plan_module(self.repository, "missing")

    def test_rejects_invalid_module_name(self) -> None:
        with self.assertRaisesRegex(MODULE.ModuleGateError, "lowercase ADDP module name"):
            MODULE.plan_module(self.repository, "sample;echo")

    def test_git_files_preserves_unicode_and_spaces(self) -> None:
        path = self.repository / "sample/frontend/中文 说明.md"
        path.write_text("说明\n", encoding="utf-8")
        subprocess.run(["git", "add", str(path)], cwd=self.repository, check=True)
        self.assertIn(
            "sample/frontend/中文 说明.md",
            MODULE.git_files(self.repository, "*/frontend/*"),
        )


if __name__ == "__main__":
    unittest.main()
