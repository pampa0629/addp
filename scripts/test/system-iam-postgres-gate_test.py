"""The IAM database must have one destructive gate owner across checkouts."""

import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import unittest


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
            self.scripts.append(script)
        bin_dir = self.root / "bin"
        bin_dir.mkdir()
        go = bin_dir / "go"
        go.write_text('''#!/usr/bin/env python3
import os, sys, time
from pathlib import Path
with open(os.environ["TEST_GO_TRACE"], "a") as trace:
    trace.write(" ".join(sys.argv[1:]) + "\\n")
if os.environ.get("TEST_GO_HOLD") == "1" and "./internal/testsupport" in sys.argv:
    Path(os.environ["TEST_STARTED"]).touch()
    deadline = time.monotonic() + 10
    while not Path(os.environ["TEST_RELEASE"]).exists():
        if time.monotonic() >= deadline:
            sys.exit("test holder timed out")
        time.sleep(0.02)
sys.exit(int(os.environ.get("TEST_GO_STATUS", "0")))
''')
        go.chmod(0o755)
        self.environment = dict(os.environ, PATH=f"{bin_dir}:{os.environ['PATH']}",
                                ADDP_SYSTEM_POSTGRES_TEST_DSN="postgres://fixture/addp_iam_test",
                                TEST_STARTED=str(self.started), TEST_RELEASE=str(self.release))

    def tearDown(self):
        for process in self.processes:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
            process.communicate(timeout=5)
        self.temporary.cleanup()

    def run_gate(self, index=1, **overrides):
        environment = dict(self.environment, TEST_GO_TRACE=str(self.root / f"trace-{index}"))
        environment.update(overrides)
        return subprocess.run(["bash", str(self.scripts[index]), "--package", "migration"],
                              env=environment, capture_output=True, text=True, timeout=5)

    def start_holder(self):
        environment = dict(self.environment, TEST_GO_TRACE=str(self.root / "trace-0"), TEST_GO_HOLD="1")
        process = subprocess.Popen(["bash", str(self.scripts[0]), "--package", "iam"],
                                   env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   text=True, start_new_session=True)
        self.processes.append(process)
        deadline = time.monotonic() + 5
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
        stdout, stderr = first.communicate(timeout=5)
        self.assertEqual(first.returncode, 0, stdout + stderr)
        self.assertTrue(self.lock.exists())
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_failed_gate_releases_lock(self):
        failed = self.run_gate(TEST_GO_STATUS="1")
        self.assertNotEqual(failed.returncode, 0)
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_terminated_gate_releases_lock(self):
        first = self.start_holder()
        os.killpg(first.pid, signal.SIGTERM)
        first.communicate(timeout=5)
        self.assertNotEqual(first.returncode, 0)
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
