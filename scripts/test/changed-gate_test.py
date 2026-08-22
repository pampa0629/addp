#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("changed-gate.py")
SPEC = importlib.util.spec_from_file_location("changed_gate", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class ChangedGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        files = {
            "common/go.mod": "module github.com/addp/common\n",
            "sample/backend/go.mod": (
                "module example.com/sample\nrequire github.com/addp/common v0.0.0\n"
            ),
            "sample/frontend/package.json": (
                '{"dependencies":{"@addp/common-frontend":"file:../../common-frontend"}}\n'
            ),
            "other/frontend/package.json": "{}\n",
            "common-python/pyproject.toml": "[project]\nname='common-python'\n",
            "agent/backend/requirements.txt": "-e ../../common-python\n",
            "agent/frontend/package.json": "{}\n",
            "Makefile": (
                "test-platform:\n\t@true\n"
                "test-sample-frontend:\n\t@true\n"
                "test-other-frontend:\n\t@true\n"
                "test-agent-eval: test-agent-frontend\n\t@true\n"
                "test-agent-frontend:\n\t@true\n"
                "test-common-python:\n\t@true\n"
            ),
        }
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

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def test_common_changes_expand_to_go_consumers(self) -> None:
        self.assertEqual(
            ["common", "sample"],
            MODULE.affected_modules(self.repository, ["common/client/system.go"]),
        )

    def test_common_frontend_changes_expand_only_to_declared_consumers(self) -> None:
        self.assertEqual(
            ["sample"],
            MODULE.affected_modules(self.repository, ["common-frontend/basic/index.js"]),
        )

    def test_common_python_changes_expand_to_registered_consumers(self) -> None:
        self.assertEqual(
            ["agent", "common-python"],
            MODULE.affected_modules(self.repository, ["common-python/addp_common/client.py"]),
        )

    def test_changed_files_include_tracked_and_untracked_worktree_changes(self) -> None:
        (self.repository / "sample/backend/go.mod").write_text("changed\n", encoding="utf-8")
        untracked = self.repository / "other/frontend/new.js"
        untracked.write_text("new\n", encoding="utf-8")
        self.assertEqual(
            ["other/frontend/new.js", "sample/backend/go.mod"],
            MODULE.changed_files(self.repository, None),
        )


if __name__ == "__main__":
    unittest.main()
