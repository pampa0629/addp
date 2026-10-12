#!/usr/bin/env python3
"""Verify owner gate exit/cleanup policy without contacting PostgreSQL."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import unittest

SCRIPT = Path(__file__).with_name("ontology-postgres-gate.sh")


class OntologyGateTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-ontology-gate-policy-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        target = self.root / "scripts/test/ontology-postgres-gate.sh"
        target.parent.mkdir(parents=True)
        shutil.copyfile(SCRIPT, target)
        utility = self.root / "scripts/infra/ports.sh"
        utility.parent.mkdir(parents=True)
        shutil.copyfile(SCRIPT.parents[1] / "infra/ports.sh", utility)
        log_utility = self.root / "scripts/utils/observability-env.sh"
        log_utility.parent.mkdir(parents=True)
        shutil.copyfile(SCRIPT.parents[1] / "utils/observability-env.sh", log_utility)
        (self.root / "ontology/backend").mkdir(parents=True)
        fake_bin = self.root / "fake-bin"
        fake_bin.mkdir()
        fake_go = fake_bin / "go"
        fake_go.write_text('''#!/bin/bash
echo "$* run=$ONTOLOGY_POSTGRES_TEST_RUN_ID" >> "$TEST_INVOCATIONS"
if [[ "$*" == *TestPostgresGateCleanup* ]]; then
    exit "${TEST_CLEANUP_EXIT:-0}"
fi
if [ "${TEST_MODE:-}" = skip ]; then echo '--- SKIP: fixture'; fi
if [ "${TEST_MODE:-}" = pause ]; then sleep 0.5; fi
exit "${TEST_MAIN_EXIT:-0}"
''')
        fake_go.chmod(0o700)
        self.log = self.root / "invocations.log"
        self.command = ["bash", str(target)]
        self.env = dict(os.environ, PATH=str(fake_bin) + os.pathsep + os.environ["PATH"],
                        TEST_INVOCATIONS=str(self.log),
                        ONTOLOGY_POSTGRES_TEST_DSN="postgres://fixture@127.0.0.1/addp_test",
                        GITHUB_ACTIONS="true")

    def run_gate(self, args=(), **extra):
        return subprocess.run(self.command + list(args), env=dict(self.env, **extra), capture_output=True, text=True, timeout=10)

    def assert_cleanup(self):
        self.assertIn("TestPostgresGateCleanup", self.log.read_text())

    def test_missing_dsn_stops_before_go(self):
        result = self.run_gate(ONTOLOGY_POSTGRES_TEST_DSN="")
        self.assertNotEqual(0, result.returncode)
        self.assertFalse(self.log.exists())

    def test_cleanup_requires_exact_explicit_run(self):
        run_id = "301447df-31e5-441b-ad38-b7ce28f445a0"
        for args in (("--cleanup-run",), ("--cleanup-run", ""), ("--cleanup-run", "invalid"),
                     ("--cleanup-run", run_id.upper()), ("--cleanup-run", run_id, "extra"),
                     ("--unknown", run_id)):
            with self.subTest(args=args):
                result = self.run_gate(args=args)
                self.assertNotEqual(0, result.returncode)
                self.assertFalse(self.log.exists(), "invalid arguments reached Go")

    def test_cleanup_only_runs_owner_cleanup_once(self):
        run_id = "301447df-31e5-441b-ad38-b7ce28f445a0"
        for code in ("0", "1"):
            with self.subTest(code=code):
                result = self.run_gate(args=("--cleanup-run", run_id), TEST_CLEANUP_EXIT=code)
                self.assertEqual(int(code), result.returncode, result.stdout + result.stderr)
                calls = self.log.read_text().splitlines()
                self.assertEqual(1, len(calls))
                self.assertIn("TestPostgresGateCleanup", calls[0])
                self.assertIn("run=" + run_id, calls[0])
                self.log.unlink()

    def test_normal_gate_never_adopts_inherited_run(self):
        run_id = "301447df-31e5-441b-ad38-b7ce28f445a0"
        result = self.run_gate(ONTOLOGY_POSTGRES_TEST_RUN_ID=run_id)
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        self.assertNotIn("run=" + run_id, self.log.read_text())

    def test_cleanup_rejects_missing_dsn_and_unsafe_target(self):
        args = ("--cleanup-run", "301447df-31e5-441b-ad38-b7ce28f445a0")
        for dsn in ("", "postgres://fixture@127.0.0.1/addp"):
            with self.subTest(dsn=dsn):
                result = self.run_gate(args=args, ONTOLOGY_POSTGRES_TEST_DSN=dsn)
                self.assertNotEqual(0, result.returncode)
                self.assertFalse(self.log.exists())

    def test_success_failure_and_skip_always_cleanup(self):
        for extra, success in (({}, True), ({"TEST_MAIN_EXIT": "1"}, False),
                               ({"TEST_MODE": "skip"}, False), ({"TEST_CLEANUP_EXIT": "1"}, False)):
            with self.subTest(extra=extra):
                result = self.run_gate(**extra)
                self.assertEqual(success, result.returncode == 0, result.stdout + result.stderr)
                self.assert_cleanup()
                self.log.unlink()

    def test_term_runs_cleanup_and_fails(self):
        process = subprocess.Popen(self.command, env=dict(self.env, TEST_MODE="pause"),
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        deadline = time.monotonic() + 5
        while not self.log.exists() and time.monotonic() < deadline:
            time.sleep(0.01)
        if not self.log.exists():
            process.kill()
            process.communicate()
            self.fail("fake test did not start")
        process.terminate()
        process.communicate(timeout=10)
        self.assertEqual(143, process.returncode)
        self.assert_cleanup()


if __name__ == "__main__":
    unittest.main()
