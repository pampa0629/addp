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
            "test-sample-eval: test-sample-frontend\n\t@true\n"
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
                ("make", "test-sample-postgres"),
            ],
            [step.command for step in steps],
        )
        self.assertEqual((("GOWORK", "off"),), steps[1].environment)

    def test_rejects_unknown_module(self) -> None:
        with self.assertRaisesRegex(MODULE.ModuleGateError, "unknown MODULE"):
            MODULE.plan_module(self.repository, "missing")

    def test_rejects_invalid_module_name(self) -> None:
        with self.assertRaisesRegex(MODULE.ModuleGateError, "lowercase ADDP module name"):
            MODULE.plan_module(self.repository, "sample;echo")


if __name__ == "__main__":
    unittest.main()
