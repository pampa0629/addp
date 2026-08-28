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
            "alias/frontend/package.json": "{}\n",
            "alias/frontend/vite.config.js": (
                "resolve(__dirname, '../../common-frontend/basic/src')\n"
            ),
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

    def test_common_frontend_changes_expand_to_package_and_alias_consumers(self) -> None:
        self.assertEqual(
            ["alias", "sample"],
            MODULE.affected_modules(self.repository, ["common-frontend/basic/index.js"]),
        )

    def test_consumer_scan_skips_tracked_files_deleted_from_worktree(self) -> None:
        (self.repository / "sample/frontend/package.json").unlink()
        self.assertEqual(
            ["alias"],
            MODULE.affected_modules(self.repository, ["common-frontend/basic/index.js"]),
        )

    def test_common_python_changes_expand_to_registered_consumers(self) -> None:
        self.assertEqual(
            ["agent", "common-python"],
            MODULE.affected_modules(self.repository, ["common-python/addp_common/client.py"]),
        )

    def test_evaluation_scenarios_and_gate_scripts_map_to_owner(self) -> None:
        self.assertEqual(
            ["agent"],
            MODULE.affected_modules(
                self.repository,
                ["evals/agent-scenarios/registry.json"],
            ),
        )

        gate = self.repository / "scripts/test/sample-postgres-gate.sh"
        gate.parent.mkdir(parents=True, exist_ok=True)
        gate.write_text("#!/usr/bin/env bash\n", encoding="utf-8")
        subprocess.run(["git", "add", str(gate)], cwd=self.repository, check=True)
        self.assertEqual(
            ["sample"],
            MODULE.affected_modules(
                self.repository,
                ["scripts/test/sample-postgres-gate.sh"],
            ),
        )

    def test_gate_control_changes_select_all_registered_modules(self) -> None:
        self.assertEqual(
            ["agent", "alias", "common", "common-python", "other", "sample"],
            MODULE.affected_modules(self.repository, [".github/workflows/platform-ci.yml"]),
        )

    def test_changed_files_between_uses_merge_base_range(self) -> None:
        base = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            cwd=self.repository,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
        (self.repository / "sample/new.go").write_text("package sample\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(
            ["git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "change"],
            cwd=self.repository,
            check=True,
        )
        self.assertEqual(
            ["sample/new.go"],
            MODULE.changed_files_between(self.repository, base, "HEAD"),
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
