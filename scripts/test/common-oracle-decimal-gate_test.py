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


class OracleDisposableGateTest(unittest.TestCase):
    def run_gate(self, *, spatial=False, skipped=False, failed=False, extra=False):
        with tempfile.TemporaryDirectory(prefix="addp-oracle-profile-gate-") as directory:
            root = Path(directory)
            commands = root / "commands"
            commands.mkdir()
            docker = commands / "docker"
            docker.write_text("""#!/bin/sh
printf '%s\\n' "$*" >> "$DOCKER_CALLS"
case "$*" in
  'container inspect '*) test -f "$OWNED_CONTAINER" ;;
  'run '*) touch "$OWNED_CONTAINER"; echo fixture-container ;;
  'exec '*) exit 0 ;;
  'port '*) echo 127.0.0.1:45678 ;;
  'rm '*) rm "$OWNED_CONTAINER" ;;
  *) exit 99 ;;
esac
""")
            docker.chmod(0o700)
            go = commands / "go"
            go.write_text(
                '#!/bin/sh\nprintf "%s\\n" "$*" > "$GO_CALLS"\n'
                'printf "%s %s\\n" "$ADDP_ORACLE_SPATIAL_INTEGRATION" "$ADDP_TEST_ORACLE_PORT" > "$GO_ENV"\n'
                + ('echo "--- SKIP: TestIntegrationOracleSpatialDelete"\n' if skipped else 'echo PASS\n')
                + ('exit 17\n' if failed else '')
            )
            go.chmod(0o700)
            environment = {
                key: value for key, value in os.environ.items()
                if not key.startswith(("ADDP_TEST_ORACLE_", "ADDP_ORACLE_"))
            }
            environment.update({
                "PATH": str(commands) + os.pathsep + os.environ["PATH"],
                "TMPDIR": str(root),
                "DOCKER_CALLS": str(root / "docker.calls"),
                "GO_CALLS": str(root / "go.calls"),
                "GO_ENV": str(root / "go.env"),
                "OWNED_CONTAINER": str(root / "owned.container"),
                # Caller environment must not activate Spatial in the Slim profile.
                "ADDP_ORACLE_SPATIAL_INTEGRATION": "1",
            })
            arguments = ["--spatial"] if spatial else []
            if extra:
                arguments.append("--read-only")
            result = subprocess.run(["bash", str(SCRIPT), *arguments], env=environment,
                                    capture_output=True, text=True, timeout=10)
            self.assertFalse((root / "owned.container").exists(), result.stdout + result.stderr)
            self.assertFalse(list(root.glob("addp-common-oracle-decimal.*")))
            read = lambda name: (root / name).read_text() if (root / name).exists() else ""
            return result, read("docker.calls"), read("go.calls"), read("go.env")

    def test_native_spatial_profile_selects_full_image_and_native_test(self):
        result, docker, go, env = self.run_gate(spatial=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("gvenzl/oracle-free:23@sha256:", docker)
        self.assertNotIn("23-slim", docker)
        self.assertIn("|SpatialDelete)$", go)
        self.assertEqual(env.strip(), "1 45678")
        self.assertIn("rm --force --volumes addp-oracle-decimal-disposable", docker)

    def test_slim_profile_cannot_inherit_spatial_opt_in(self):
        result, docker, go, env = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("gvenzl/oracle-free:23-slim@sha256:", docker)
        self.assertNotIn("SpatialDelete", go)
        self.assertEqual(env.strip(), "0 45678")

    def test_native_spatial_profile_cleans_up_after_failure(self):
        for kwargs in ({"skipped": True}, {"failed": True}):
            with self.subTest(kwargs=kwargs):
                result, docker, _, _ = self.run_gate(spatial=True, **kwargs)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("rm --force --volumes", docker)

    def test_native_spatial_profile_rejects_readonly_combination_before_docker(self):
        result, docker, go, _ = self.run_gate(spatial=True, extra=True)
        self.assertEqual(result.returncode, 2)
        self.assertEqual(docker, "")
        self.assertEqual(go, "")


if __name__ == "__main__":
    unittest.main()
