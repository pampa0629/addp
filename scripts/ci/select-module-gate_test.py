#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import os
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
            "common/go.mod": "module github.com/addp/common\n",
            "sample/backend/go.mod": "module example.com/sample\nrequire github.com/addp/common v0.0.0\n",
            "agent/backend/requirements.txt": "PyYAML\n",
            "other/frontend/package.json": '{"scripts":{"build":"vite build"}}\n',
            "Makefile": "test-sample:\n\t@true\ntest-other-frontend:\n\t@true\ntest-agent-eval:\n\t@true\n",
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
            "ADDP_CI_HEAD": subprocess.run(
                ["git", "rev-parse", "HEAD"], cwd=self.repository,
                check=True, capture_output=True, text=True,
            ).stdout.strip(),
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

    def push(self, relative_path: str) -> dict[str, str]:
        before = subprocess.run(
            ["git", "rev-parse", "HEAD"], cwd=self.repository,
            check=True, capture_output=True, text=True,
        ).stdout.strip()
        return {
            **self.commit(relative_path),
            "ADDP_CI_EVENT": "push",
            "ADDP_CI_BEFORE": before,
        }

    def test_running_and_queued_code_pushes_retain_selection_after_docs_push(self) -> None:
        running = self.push("sample/backend/first.go")
        queued = self.push("sample/backend/second.go")
        docs = self.push("docs/acceptance.md")
        # Use each immutable event range after the branch advances. A skipped
        # docs run is valid only while both earlier runs retain their evidence.
        for event, selected in ((running, True), (queued, True), (docs, False)):
            with self.subTest(head=event["ADDP_CI_HEAD"]):
                self.assertEqual(selected, MODULE.select_module(self.repository, "sample", event)[0])
                self.assertFalse(MODULE.select_module(self.repository, "other", event)[0])

    def test_shared_dependency_push_selects_consumer_after_docs_push(self) -> None:
        shared = self.push("common/client/meta.go")
        docs = self.push("docs/shared.md")
        for owner in ("common", "sample"):
            with self.subTest(owner=owner):
                self.assertTrue(MODULE.select_module(self.repository, owner, shared)[0])
                self.assertFalse(MODULE.select_module(self.repository, owner, docs)[0])
        self.assertFalse(MODULE.select_module(self.repository, "other", shared)[0])

    def test_push_uses_full_event_range_for_multiple_commits(self) -> None:
        self.commit("sample/backend/main.go")
        event = self.push("docs/latest.md")
        self.assertFalse(MODULE.select_module(self.repository, "sample", event)[0])
        event["ADDP_CI_BEFORE"] = self.base
        self.assertTrue(MODULE.select_module(self.repository, "sample", event)[0])

    def test_cli_code_pushes_retain_selection_after_docs_push(self) -> None:
        running = self.push("common-python/first.py")
        queued = self.push("common-python/second.py")
        docs = self.push("docs/cli.md")
        selector = SCRIPT.with_name("select-gate-by-paths.sh")
        output = self.repository / "selection-output"
        for event, selected in ((running, True), (queued, True), (docs, False)):
            with self.subTest(head=event["ADDP_CI_HEAD"]):
                output.write_text("", encoding="utf-8")
                result = subprocess.run(
                    ["bash", str(selector), "CLI product", "common-python/*"],
                    cwd=self.repository,
                    env={**os.environ, **event, "GITHUB_OUTPUT": str(output)},
                    capture_output=True, text=True,
                )
                self.assertEqual(0, result.returncode, result.stderr)
                self.assertEqual(f"run={str(selected).lower()}\n", output.read_text(encoding="utf-8"))

    def test_reporter_refuses_missing_or_unsuccessful_selected_verification(self) -> None:
        reporter = SCRIPT.with_name("report-selected-gate.sh")
        cases = [
            ("true", "success", "success", 0),
            ("false", "success", "skipped", 0),
            ("true", "success", "failure", 1),
            ("true", "success", "cancelled", 1),
            ("true", "success", "skipped", 1),
            ("true", "success", "", 1),
            ("false", "failure", "skipped", 1),
            ("false", "cancelled", "skipped", 1),
            ("false", "success", "success", 1),
            ("", "success", "skipped", 1),
        ]
        for selected, selection, verification, expected in cases:
            with self.subTest(selected=selected, selection=selection, verification=verification):
                result = subprocess.run(
                    ["bash", str(reporter), "gate", selected, selection, verification],
                    capture_output=True, text=True,
                )
                self.assertEqual(expected, result.returncode, result.stdout + result.stderr)

    def test_skill_change_selects_agent_for_pull_request_and_push(self) -> None:
        environment = self.commit("skills/workflow-analysis/SKILL.md")
        for event in ("pull_request", "push"):
            with self.subTest(event=event):
                event_environment = {
                    **environment,
                    "ADDP_CI_EVENT": event,
                    "ADDP_CI_BEFORE": self.base,
                }
                self.assertTrue(
                    MODULE.select_module(self.repository, "agent", event_environment)[0]
                )
                self.assertFalse(
                    MODULE.select_module(self.repository, "other", event_environment)[0]
                )

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
