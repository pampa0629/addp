"""The IAM database must have one destructive gate owner across checkouts."""

import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import time
import unittest


# Platform T0 can run alongside builds and browser gates. Process startup is
# bounded, but is not a five-second performance requirement of the lock contract.
PROCESS_TIMEOUT_SECONDS = 30


class SystemIAMGateLockTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.lock = self.root / "iam.lock"
        self.started = self.root / "started"
        self.release = self.root / "release"
        self.processes = []
        source = Path(__file__).with_name("system-iam-postgres-gate.sh").read_text()
        self.scripts = []
        for name in ("first", "second"):
            checkout = self.root / name
            (checkout / "scripts/test").mkdir(parents=True)
            (checkout / "system/backend").mkdir(parents=True)
            script = checkout / "scripts/test/system-iam-postgres-gate.sh"
            script.write_text(source.replace("/tmp/addp-system-iam-postgres-gate.lock", str(self.lock)))
            utility = checkout / "scripts/infra/ports.sh"
            utility.parent.mkdir(parents=True)
            shutil.copyfile(Path(__file__).parents[1] / "infra/ports.sh", utility)
            self.scripts.append(script)
        bin_dir = self.root / "bin"
        bin_dir.mkdir()
        go = bin_dir / "go"
        go.write_text(f'''#!/usr/bin/env python3
import os, sys, time
from pathlib import Path
with open(os.environ["TEST_GO_TRACE"], "a") as trace:
    trace.write(" ".join(sys.argv[1:]) + "\\n")
if os.environ.get("TEST_GO_HOLD") == "1" and "./internal/testsupport" in sys.argv:
    Path(os.environ["TEST_STARTED"]).touch()
    deadline = time.monotonic() + {2 * PROCESS_TIMEOUT_SECONDS}
    while not Path(os.environ["TEST_RELEASE"]).exists():
        if time.monotonic() >= deadline:
            sys.exit("test holder timed out")
        time.sleep(0.02)
sys.exit(int(os.environ.get("TEST_GO_STATUS", "0")))
''')
        go.chmod(0o755)
        self.environment = dict(os.environ, PATH=f"{bin_dir}:{os.environ['PATH']}",
                                ADDP_SYSTEM_POSTGRES_TEST_DSN="postgres://fixture/addp_iam_test",
                                GITHUB_ACTIONS="true",
                                TEST_STARTED=str(self.started), TEST_RELEASE=str(self.release))

    def tearDown(self):
        for process in self.processes:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
            process.communicate(timeout=PROCESS_TIMEOUT_SECONDS)
        self.temporary.cleanup()

    def run_gate(self, index=1, arguments=(), **overrides):
        environment = dict(self.environment, TEST_GO_TRACE=str(self.root / f"trace-{index}"))
        environment.update(overrides)
        return subprocess.run(["bash", str(self.scripts[index]), "--package", "migration", *arguments],
                              env=environment, capture_output=True, text=True, timeout=PROCESS_TIMEOUT_SECONDS)

    def start_holder(self):
        environment = dict(self.environment, TEST_GO_TRACE=str(self.root / "trace-0"), TEST_GO_HOLD="1")
        process = subprocess.Popen(["bash", str(self.scripts[0]), "--package", "iam"],
                                   env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   text=True, start_new_session=True)
        self.processes.append(process)
        deadline = time.monotonic() + PROCESS_TIMEOUT_SECONDS
        while not self.started.exists() and time.monotonic() < deadline and process.poll() is None:
            time.sleep(0.02)
        self.assertTrue(self.started.exists(), "holder did not reach its first database command")
        return process

    def test_concurrent_checkout_is_rejected_before_reset_and_lock_is_reusable(self):
        first = self.start_holder()
        second = self.run_gate()
        self.assertNotEqual(second.returncode, 0)
        self.assertIn("already running", second.stderr)
        self.assertFalse((self.root / "trace-1").exists())
        self.release.touch()
        stdout, stderr = first.communicate(timeout=PROCESS_TIMEOUT_SECONDS)
        self.assertEqual(first.returncode, 0, stdout + stderr)
        self.assertTrue(self.lock.exists())
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_failed_gate_releases_lock(self):
        failed = self.run_gate(TEST_GO_STATUS="1")
        self.assertNotEqual(failed.returncode, 0)
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_coordination_filter_preserves_complete_default_discovery(self):
        result = self.run_gate(arguments=("--test", "engine-access-coordination"))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertIn("./internal/testsupport -run ^TestResetDisposablePostgresForGate$", trace)
        self.assertIn("./internal/migration -run ^Test(FulfillmentOutcome|FulfillmentGrant|FulfillmentGrantRevocation|FulfillmentGrantRevocationExpiry|FulfillmentRecoveryPermission|FulfillmentHandlingPermission|FulfillmentBasis|ApprovalRequirement|SharingExpiry)ForwardMigrationAgainstPostgres$", trace)
        (self.root / "trace-1").unlink()
        result = self.run_gate(arguments=("--package", ""))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertIn("./internal/migration -run AgainstPostgres$", trace)
        self.assertIn("./internal/engineaccess -run AgainstPostgres$", trace)
        self.assertIn("./cmd/online-test-fixture -run AgainstPostgres$", trace)
        result = self.run_gate(arguments=("--package", "iam", "--test", "engine-access-coordination"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package migration", result.stderr)

    def test_terminated_gate_releases_lock(self):
        first = self.start_holder()
        os.killpg(first.pid, signal.SIGTERM)
        first.communicate(timeout=PROCESS_TIMEOUT_SECONDS)
        self.assertNotEqual(first.returncode, 0)
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_credential_context_filter_runs_shared_validation_consumers(self):
        result = self.run_gate(arguments=("--package", "iam", "--test", "credential-context"))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertIn("./internal/iam -run ^Test(DelegationService|ExecutionAuthorizationService|NotebookSessionAuthorizationService|CredentialValidationEvidence|BrowserAuthenticationClock)AgainstPostgres$", trace)
        result = self.run_gate(arguments=("--test", "credential-context"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package iam", result.stderr)

    def test_transfer_create_filter_keeps_default_migration_discovery(self):
        result = self.run_gate(arguments=("--test", "transfer-task-create"))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertIn("./internal/migration -run ^TestTransferTaskCreateDelegationMigrationAgainstPostgres$", trace)
        (self.root / "trace-1").unlink()
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("./internal/migration -run AgainstPostgres$", (self.root / "trace-1").read_text())
        result = self.run_gate(arguments=("--package", "iam", "--test", "transfer-task-create"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package migration", result.stderr)


if __name__ == "__main__":
    unittest.main()
