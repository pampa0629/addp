#!/usr/bin/env python3
"""Verify disposable FalkorDB gate policy without running Docker or Go."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import unittest

SCRIPT = Path(__file__).with_name("ontology-falkor-gate.sh")


class FalkorGateTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-falkor-gate-policy-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        target = self.root / "scripts/test/ontology-falkor-gate.sh"
        target.parent.mkdir(parents=True)
        shutil.copyfile(SCRIPT, target)
        (self.root / "ontology/backend").mkdir(parents=True)
        fake_bin = self.root / "fake-bin"
        fake_bin.mkdir()
        programs = {
            "docker": '''#!/bin/bash
echo "docker $*" >> "$TEST_INVOCATIONS"
case "$*" in
    *"up -d"*) exit "${TEST_START_EXIT:-0}" ;;
    *"port falkordb"*) echo "${TEST_PORT:-127.0.0.1:23456}" ;;
    *"down --volumes --remove-orphans"*) exit "${TEST_CLEANUP_EXIT:-0}" ;;
    "ps -aq"*) [ "${TEST_RESIDUAL:-0}" = 0 ] || echo residual ;;
esac
exit 0
''',
            "go": '''#!/bin/bash
echo "go $*" >> "$TEST_INVOCATIONS"
[ "$ONTOLOGY_FALKOR_TEST_ADDRESS" = '127.0.0.1:23456' ] || exit 2
[ "${#ONTOLOGY_FALKOR_TEST_PASSWORD}" = 32 ] || exit 2
[ "$ADDP_ONTOLOGY_FALKOR_INTEGRATION" = 1 ] || exit 2
if [ "${TEST_MODE:-}" = skip ]; then echo '--- SKIP: fixture'; fi
if [ "${TEST_MODE:-}" = pause ]; then sleep 0.5; fi
exit "${TEST_MAIN_EXIT:-0}"
''',
        }
        for name, content in programs.items():
            path = fake_bin / name
            path.write_text(content)
            path.chmod(0o700)
        self.log = self.root / "invocations.log"
        self.command = ["bash", str(target)]
        self.env = dict(os.environ, PATH=str(fake_bin) + os.pathsep + os.environ["PATH"],
                        TEST_INVOCATIONS=str(self.log),
                        ONTOLOGY_FALKOR_TEST_ADDRESS="production:6379",
                        ONTOLOGY_FALKOR_TEST_PASSWORD="inherited-not-allowed")

    def test_exit_paths_cleanup_and_reject_residuals(self):
        for extra, success in (({}, True), ({"TEST_MAIN_EXIT": "1"}, False),
                               ({"TEST_MODE": "skip"}, False), ({"TEST_START_EXIT": "1"}, False),
                               ({"TEST_CLEANUP_EXIT": "1"}, False), ({"TEST_RESIDUAL": "1"}, False),
                               ({"TEST_PORT": "0.0.0.0:6379"}, False)):
            with self.subTest(extra=extra):
                result = subprocess.run(self.command, env=dict(self.env, **extra), capture_output=True, text=True, timeout=10)
                self.assertEqual(success, result.returncode == 0, result.stdout + result.stderr)
                log = self.log.read_text()
                self.assertIn("down --volumes --remove-orphans", log)
                self.assertIn("--env-file /dev/null", log)
                self.assertNotIn("production", log)
                self.log.unlink()

    def test_term_cleans_owned_project_and_is_not_success(self):
        process = subprocess.Popen(self.command, env=dict(self.env, TEST_MODE="pause"),
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            if self.log.exists() and "go " in self.log.read_text():
                break
            time.sleep(0.01)
        else:
            process.kill()
            process.communicate()
            self.fail("fake test did not start")
        process.terminate()
        process.communicate(timeout=10)
        self.assertEqual(143, process.returncode)
        self.assertIn("down --volumes --remove-orphans", self.log.read_text())


if __name__ == "__main__":
    unittest.main()
