import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import subprocess


SPEC = importlib.util.spec_from_file_location(
    "workflow_security_gate", Path(__file__).with_name("workflow-security-gate.py")
)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class WorkflowSecurityGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-audit-test-")
        self.addCleanup(self.temporary.cleanup)
        self.repository = Path(self.temporary.name)
        workflow = self.repository / MODULE.WORKFLOW
        workflow.parent.mkdir(parents=True)
        workflow.write_text("jobs: {}\n", encoding="utf-8")

    @patch.object(MODULE.subprocess, "run")
    def test_pins_tool_scope_and_strict_audit_contract(self, run) -> None:
        run.return_value = subprocess.CompletedProcess([], 0)
        self.assertEqual(0, MODULE.audit(self.repository))
        self.assertEqual(3, run.call_count)
        install = run.call_args_list[1].args[0]
        self.assertIn("zizmor==1.28.0", install)
        self.assertIn("--only-binary=:all:", install)
        self.assertIn("--no-deps", install)
        self.assertEqual(
            ["--no-online-audits", "--persona=auditor", "--min-severity=medium",
             "--format=github", "--no-progress", "--strict-collection",
             "--no-config", "--no-ignores", MODULE.WORKFLOW],
            run.call_args.args[0][1:],
        )
        for call in run.call_args_list:
            self.assertEqual(self.repository, call.kwargs["cwd"])
        self.assertFalse(Path(install[0]).parent.parent.parent.exists())

    def test_failures_stop_and_preserve_exit_code_and_cleanup(self) -> None:
        for failed_step in range(3):
            with self.subTest(failed_step=failed_step), patch.object(MODULE.subprocess, "run") as run:
                run.side_effect = [subprocess.CompletedProcess([], 0)] * failed_step + [
                    subprocess.CompletedProcess([], 13)
                ]
                self.assertEqual(13, MODULE.audit(self.repository))
                self.assertEqual(failed_step + 1, run.call_count)
                environment = Path(run.call_args_list[0].args[0][-1])
                self.assertFalse(environment.parent.exists())

    @patch.object(MODULE.subprocess, "run")
    def test_missing_workflow_fails_before_install(self, run) -> None:
        (self.repository / MODULE.WORKFLOW).unlink()
        self.assertEqual(1, MODULE.audit(self.repository))
        run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
