#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("select-module-gate.py")
SPEC = importlib.util.spec_from_file_location("select_module_gate", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class SelectModuleGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        files = {
            "sample/backend/go.mod": "module example.com/sample\n",
            "other/frontend/package.json": '{"scripts":{"build":"vite build"}}\n',
            "Makefile": "test-sample:\n\t@true\ntest-other-frontend:\n\t@true\n",
        }
        source_scripts = Path(__file__).parents[1] / "test"
        for script_name in ("changed-gate.py", "module-gate.py"):
            files[f"scripts/test/{script_name}"] = (source_scripts / script_name).read_text(
                encoding="utf-8"
            )
        for relative_path, content in files.items():
            path = self.repository / relative_path
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content, encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(
            ["git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "fixture"],
            cwd=self.repository,
            check=True,
        )
        self.base = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            cwd=self.repository,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def commit(self, relative_path: str) -> dict[str, str]:
        path = self.repository / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("change\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(
            ["git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "change"],
            cwd=self.repository,
            check=True,
        )
        return {
            "ADDP_CI_EVENT": "pull_request",
            "ADDP_CI_HEAD": "HEAD",
            "ADDP_CI_PR_BASE": self.base,
        }

    def test_selects_only_affected_module(self) -> None:
        environment = self.commit("sample/backend/main.go")
        self.assertEqual(
            (True, "shared matrix selected sample from 1 changed files"),
            MODULE.select_module(self.repository, "sample", environment),
        )
        self.assertEqual(
            (False, "shared matrix did not select other from 1 changed files"),
            MODULE.select_module(self.repository, "other", environment),
        )

    def test_gate_control_change_selects_every_module(self) -> None:
        environment = self.commit(".github/workflows/platform-ci.yml")
        self.assertTrue(MODULE.select_module(self.repository, "sample", environment)[0])
        self.assertTrue(MODULE.select_module(self.repository, "other", environment)[0])

    def test_manual_event_and_force_select_without_diff(self) -> None:
        self.assertEqual(
            (True, "workflow_dispatch event"),
            MODULE.select_module(
                self.repository,
                "sample",
                {"ADDP_CI_EVENT": "workflow_dispatch"},
            ),
        )
        self.assertEqual(
            (True, "forced by caller"),
            MODULE.select_module(
                self.repository,
                "sample",
                {"ADDP_CI_FORCE": "true"},
            ),
        )


if __name__ == "__main__":
    unittest.main()
