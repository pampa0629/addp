#!/usr/bin/env python3

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("common-oracle-decimal-gate.sh")


class OracleReadOnlyGateTest(unittest.TestCase):
    def run_gate(self, *, missing_password=False, skipped=False, arguments=None):
        with tempfile.TemporaryDirectory(prefix="addp-oracle-readonly-gate-") as directory:
            root = Path(directory)
            commands = root / "commands"
            commands.mkdir()
            go_calls = root / "go.calls"
            docker_calls = root / "docker.calls"
            go = commands / "go"
            go.write_text(
                '#!/bin/sh\nprintf "%s\\n" "$*" > "$GO_CALLS"\n'
                'test "$ADDP_ORACLE_READONLY_INTEGRATION" = 1 || exit 9\n'
                + ('echo "--- SKIP: TestIntegrationOracleReadOnlyCatalog"\n' if skipped else 'echo "PASS"\n')
            )
            go.chmod(0o700)
            docker = commands / "docker"
            docker.write_text('#!/bin/sh\necho invoked > "$DOCKER_CALLS"\nexit 77\n')
            docker.chmod(0o700)
            environment = {
                key: value for key, value in os.environ.items()
                if not key.startswith(("ADDP_TEST_ORACLE_", "ADDP_ORACLE_"))
            }
            environment.update({
                "PATH": str(commands) + os.pathsep + os.environ["PATH"],
                "TMPDIR": str(root),
                "GO_CALLS": str(go_calls),
                "DOCKER_CALLS": str(docker_calls),
                "ADDP_TEST_ORACLE_HOST": "existing.example.invalid",
                "ADDP_TEST_ORACLE_PORT": "1521",
                "ADDP_TEST_ORACLE_SERVICE_NAME": "SERVICE",
                "ADDP_TEST_ORACLE_USER": "reader",
                "ADDP_TEST_ORACLE_PASSWORD": "private-fixture-password",
                # An inherited write opt-in cannot broaden the selected test.
                "ADDP_ORACLE_DECIMAL_INTEGRATION": "1",
            })
            if missing_password:
                environment.pop("ADDP_TEST_ORACLE_PASSWORD")
            result = subprocess.run(
                ["bash", str(SCRIPT), *(arguments or ["--read-only"])],
                env=environment, capture_output=True, text=True, timeout=10,
            )
            self.assertFalse(docker_calls.exists(), result.stdout + result.stderr)
            self.assertFalse(list(root.glob("addp-common-oracle-decimal.*")))
            self.assertNotIn("private-fixture-password", result.stdout + result.stderr)
            return result, go_calls.read_text() if go_calls.exists() else ""

    def test_readonly_mode_selects_only_the_readonly_catalog_test(self):
        result, calls = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(
            calls.strip(),
            "test ./engine/plugins/oracle -run ^TestIntegrationOracleReadOnlyCatalog$ -count=1 -v",
        )

    def test_readonly_mode_requires_explicit_credentials(self):
        result, calls = self.run_gate(missing_password=True)
        self.assertEqual(result.returncode, 2)
        self.assertEqual(calls, "")

    def test_readonly_mode_rejects_skipped_verification(self):
        result, _ = self.run_gate(skipped=True)
        self.assertEqual(result.returncode, 1)

    def test_readonly_mode_rejects_extra_arguments(self):
        result, calls = self.run_gate(arguments=["--read-only", "--write"])
        self.assertEqual(result.returncode, 2)
        self.assertEqual(calls, "")


if __name__ == "__main__":
    unittest.main()
