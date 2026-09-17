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
        (self.root / "ontology/backend").mkdir(parents=True)
        fake_bin = self.root / "fake-bin"
        fake_bin.mkdir()
        fake_go = fake_bin / "go"
        fake_go.write_text('''#!/bin/bash
echo "$*" >> "$TEST_INVOCATIONS"
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
                        ONTOLOGY_POSTGRES_TEST_DSN="postgres://fixture@127.0.0.1/addp_test")

    def run_gate(self, **extra):
        return subprocess.run(self.command, env=dict(self.env, **extra), capture_output=True, text=True, timeout=10)

    def assert_cleanup(self):
        self.assertIn("TestPostgresGateCleanup", self.log.read_text())

    def test_missing_dsn_stops_before_go(self):
        result = self.run_gate(ONTOLOGY_POSTGRES_TEST_DSN="")
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
