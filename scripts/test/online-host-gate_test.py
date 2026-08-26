import os
import shutil
import subprocess
import tempfile
import textwrap
import time
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("online-host-gate.sh")
PREFLIGHT = Path(__file__).with_name("online-preflight.py")
EXACT_STOP_SCRIPT = Path(__file__).parents[1] / "dev" / "stop-exact-process.sh"
LIFECYCLE_LOCK_SCRIPT = Path(__file__).parents[1] / "dev" / "lifecycle-lock.sh"


class OnlineHostGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-host-gate-")
        self.root = Path(self.temporary.name)
        self.repository = self.root / "repository"
        self.external = self.root / "external"
        self.artifacts = self.external / "artifacts"
        self.command_log = self.external / "commands.log"
        (self.repository / "scripts/test").mkdir(parents=True)
        (self.repository / "scripts/infra").mkdir(parents=True)
        (self.repository / "scripts/dev").mkdir(parents=True)
        self.external.mkdir()
        shutil.copy2(SCRIPT, self.repository / "scripts/test/online-host-gate.sh")
        shutil.copy2(PREFLIGHT, self.repository / "scripts/test/online-preflight.py")
        self._write_executable(
            "scripts/infra/up.sh",
            '#!/bin/bash\nprintf "infra-up\\n" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "scripts/dev/start.sh",
            '#!/bin/bash\nprintf "start:%s\\n" "$*" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "scripts/dev/stop-exact-process.sh",
            '#!/bin/bash\nprintf "stop-exact:%s\\n" "$1" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "scripts/dev/stop.sh",
            textwrap.dedent(
                """\
                #!/bin/bash
                printf "stop\n" >> "$ADDP_TEST_COMMAND_LOG"
                [ "${ADDP_TEST_STOP_FAIL:-0}" != "1" ]
                """
            ),
        )
        self._write_executable(
            "make",
            textwrap.dedent(
                """\
                #!/bin/bash
                printf "make:%s:%s\n" "$1" "$2" >> "$ADDP_TEST_COMMAND_LOG"
                printf '{"schema_version":"addp.online-suite/v1"}\n'
                """
            ),
        )
        self._write_executable(
            "uname",
            textwrap.dedent(
                """\
                #!/bin/bash
                printf '%s\n' "${ADDP_TEST_UNAME_S:-Darwin}"
                """
            ),
        )
        for command in ("docker", "go", "node", "npm", "curl", "lsof", "nc"):
            self._write_executable(command, "#!/bin/bash\nexit 0\n")
        self._write_executable(
            "scripts/test/module-lifecycle-process-online.py",
            textwrap.dedent(
                """\
                #!/usr/bin/env python3
                import json
                import os
                import pathlib
                import sys

                phase = sys.argv[sys.argv.index("--phase") + 1]
                output = pathlib.Path(sys.argv[sys.argv.index("--output") + 1])
                report = {
                    "schema_version": "addp.module-lifecycle-process/v1",
                    "phase": phase,
                    "manager": {"instance_id": "manager-online-process"},
                }
                output.write_text(json.dumps(report) + "\\n", encoding="utf-8")
                with open(os.environ["ADDP_TEST_COMMAND_LOG"], "a", encoding="utf-8") as log:
                    log.write(f"observe:{phase}\\n")
                print(json.dumps(report))
                """
            ),
        )
        self.env_file = self.external / "online.env"
        self.env_file.write_text(
            textwrap.dedent(
                """\
                ADDP_ONLINE_TEST=1
                ADDP_ONLINE_TEST_TENANT_ID=42
                POSTGRES_DB=addp_online
                SYSTEM_URL=http://127.0.0.1:8180
                GATEWAY_URL=http://127.0.0.1:8000
                MANAGER_URL=http://127.0.0.1:8081
                STANDARD_URL=http://127.0.0.1:8110
                MODEL_URL=http://127.0.0.1:8181
                MANAGER_SERVICE_CLIENT_SECRET=manager-online-secret-0123456789abcdef
                ADDP_ONLINE_TEST_USER_ACCESS_TOKEN=addp_at_online
                """
            ),
            encoding="utf-8",
        )
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        subprocess.run(["git", "config", "user.email", "online@example.invalid"], cwd=self.repository, check=True)
        subprocess.run(["git", "config", "user.name", "Online Gate Test"], cwd=self.repository, check=True)
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(["git", "commit", "-qm", "fixture"], cwd=self.repository, check=True)

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def _write_executable(self, relative: str, content: str) -> None:
        path = self.repository / relative
        path.write_text(content, encoding="utf-8")
        path.chmod(0o755)

    def _run(
        self, suite: str, *arguments: str, **overrides: str
    ) -> subprocess.CompletedProcess[str]:
        environment = dict(os.environ)
        environment.update(
            {
                "ADDP_ONLINE_HOST": "1",
                "ADDP_ONLINE_ENV_FILE": str(self.env_file),
                "ADDP_ONLINE_ARTIFACT_DIR": str(self.artifacts),
                "ADDP_TEST_COMMAND_LOG": str(self.command_log),
                "ONLINE_SUITE": suite,
                "PATH": str(self.repository) + os.pathsep + environment["PATH"],
            }
        )
        environment.update(overrides)
        return subprocess.run(
            ["bash", "scripts/test/online-host-gate.sh", *arguments],
            cwd=self.repository,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_dispatches_registered_suite_and_always_stops_application(self) -> None:
        result = self._run("module-registry-recovery")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.command_log.read_text(encoding="utf-8").splitlines(),
            [
                "stop",
                "infra-up",
                "start:--exact-process --wait-live -manager",
                "observe:business-before-system",
                "start:--exact-process -system",
                "observe:manager-registered",
                "start:--exact-process -gateway",
                "observe:gateway-established",
                "stop-exact:-system",
                "observe:system-interrupted",
                "start:--exact-process -system",
                "observe:system-recovered",
                "stop-exact:-manager",
                "make:test-online:ONLINE_SUITE=module-registry-recovery",
                "stop",
            ],
        )
        summary = (self.artifacts / "summary.txt").read_text(encoding="utf-8")
        self.assertIn("suite=module-registry-recovery", summary)
        self.assertIn("result=passed", summary)
        self.assertIn("cleanup=passed", summary)
        self.assertIn("process_lifecycle=passed", summary)
        self.assertTrue((self.artifacts / "online-gate.log").is_file())
        self.assertTrue(
            (self.artifacts / "module-lifecycle-system-recovered.json").is_file()
        )

    def test_maps_standard_model_suite_to_model_deployment(self) -> None:
        result = self._run("standard-model-reference-deletion")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("start:-model", self.command_log.read_text(encoding="utf-8"))

    def test_check_only_writes_readiness_without_lifecycle_action(self) -> None:
        result = self._run("module-registry-recovery", "--check-only")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("readiness check passed", result.stdout)
        self.assertFalse(self.command_log.exists())
        readiness = (self.artifacts / "readiness.txt").read_text(encoding="utf-8")
        self.assertIn("schema_version=addp.online-host-readiness/v1", readiness)
        self.assertIn("suite=module-registry-recovery", readiness)
        self.assertIn("repository_clean=true", readiness)
        self.assertIn("database=addp_online", readiness)
        self.assertIn("lifecycle=not-started", readiness)
        self.assertFalse((self.artifacts / "summary.txt").exists())

    def test_caller_control_values_override_stale_env_file_values(self) -> None:
        stale_artifacts = self.external / "stale-artifacts"
        self.env_file.write_text(
            self.env_file.read_text(encoding="utf-8")
            + f"ONLINE_SUITE=standard-model-reference-deletion\n"
            + f"ADDP_ONLINE_ARTIFACT_DIR={stale_artifacts}\n",
            encoding="utf-8",
        )

        result = self._run("module-registry-recovery", "--check-only")

        self.assertEqual(result.returncode, 0, result.stderr)
        readiness = (self.artifacts / "readiness.txt").read_text(encoding="utf-8")
        self.assertIn("suite=module-registry-recovery", readiness)
        self.assertIn("start_target=-system", readiness)
        self.assertFalse(stale_artifacts.exists())

    def test_env_file_cannot_supply_lifecycle_control_values(self) -> None:
        self.env_file.write_text(
            self.env_file.read_text(encoding="utf-8")
            + "ONLINE_SUITE=module-registry-recovery\n"
            + f"ADDP_ONLINE_ARTIFACT_DIR={self.artifacts}\n",
            encoding="utf-8",
        )

        result = self._run(
            "module-registry-recovery",
            "--check-only",
            ONLINE_SUITE="",
            ADDP_ONLINE_ARTIFACT_DIR="",
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ONLINE_SUITE is required from the caller", result.stderr)
        self.assertFalse(self.artifacts.exists())

    def test_rejects_non_dedicated_host_before_any_lifecycle_action(self) -> None:
        result = self._run("module-registry-recovery", ADDP_ONLINE_HOST="0")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ADDP_ONLINE_HOST", result.stderr)
        self.assertFalse(self.command_log.exists())

    def test_rejects_non_macos_host_before_any_lifecycle_action(self) -> None:
        result = self._run(
            "module-registry-recovery", ADDP_TEST_UNAME_S="Linux"
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("dedicated Online Runner must use macOS", result.stderr)
        self.assertFalse(self.command_log.exists())

    def test_rejects_missing_suite_environment_before_lifecycle_action(self) -> None:
        self.env_file.write_text(
            self.env_file.read_text(encoding="utf-8").replace(
                "GATEWAY_URL=http://127.0.0.1:8000\n", ""
            ),
            encoding="utf-8",
        )
        result = self._run("module-registry-recovery")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires GATEWAY_URL", result.stderr)
        self.assertFalse(self.command_log.exists())

    def test_rejects_non_online_database_before_lifecycle_action(self) -> None:
        self.env_file.write_text(
            self.env_file.read_text(encoding="utf-8").replace(
                "POSTGRES_DB=addp_online\n", "POSTGRES_DB=addp\n"
            ),
            encoding="utf-8",
        )
        result = self._run("module-registry-recovery")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("POSTGRES_DB must be exactly addp_online", result.stderr)
        self.assertFalse(self.command_log.exists())

    def test_rejects_repository_env_and_repository_owned_secret_file(self) -> None:
        repository_env = self.repository / ".env"
        repository_env.write_text("ADDP_ONLINE_TEST=1\n", encoding="utf-8")
        result = self._run("module-registry-recovery")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("仓库根 .env", result.stderr)
        repository_env.unlink()

        internal_env = self.repository / "online.env"
        internal_env.write_text("ADDP_ONLINE_TEST=1\n", encoding="utf-8")
        result = self._run(
            "module-registry-recovery", ADDP_ONLINE_ENV_FILE=str(internal_env)
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("仓库外", result.stderr)

    def test_cleanup_failure_fails_gate(self) -> None:
        result = self._run(
            "module-registry-recovery", ADDP_TEST_STOP_FAIL="1"
        )

        self.assertNotEqual(result.returncode, 0)
        summary = (self.artifacts / "summary.txt").read_text(encoding="utf-8")
        self.assertIn("result=failed", summary)
        self.assertIn("cleanup=failed", summary)


class ExactProcessControlTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-exact-process-")
        self.repository = Path(self.temporary.name)
        (self.repository / "scripts/dev").mkdir(parents=True)
        (self.repository / ".dev-bins").mkdir()
        (self.repository / ".dev-pids").mkdir()
        shutil.copy2(
            EXACT_STOP_SCRIPT, self.repository / "scripts/dev/stop-exact-process.sh"
        )
        shutil.copy2(
            LIFECYCLE_LOCK_SCRIPT, self.repository / "scripts/dev/lifecycle-lock.sh"
        )
        self.processes: list[subprocess.Popen[bytes]] = []

    def tearDown(self) -> None:
        for process in self.processes:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=2)
        self.temporary.cleanup()

    def _managed_process(self, module: str) -> subprocess.Popen[bytes]:
        binary = self.repository / f".dev-bins/addp-{module}"
        binary.write_text(
            "#!/usr/bin/env bash\ntrap 'exit 0' TERM INT\nwhile true; do sleep 1; done\n",
            encoding="utf-8",
        )
        binary.chmod(0o755)
        process = subprocess.Popen([str(binary)], cwd=self.repository)
        self.processes.append(process)
        (self.repository / f".dev-pids/{module}.pid").write_text(
            f"{process.pid}\n", encoding="utf-8"
        )
        time.sleep(0.05)
        return process

    def _run(self, selector: str, *, online_host: str = "1") -> subprocess.CompletedProcess[str]:
        environment = dict(os.environ)
        environment["ADDP_ONLINE_HOST"] = online_host
        return subprocess.run(
            ["bash", "scripts/dev/stop-exact-process.sh", selector],
            cwd=self.repository,
            env=environment,
            capture_output=True,
            text=True,
        )

    def test_exact_stop_stops_only_the_selected_managed_binary(self) -> None:
        manager = self._managed_process("manager")
        system = self._managed_process("system")

        result = self._run("-manager")

        self.assertEqual(result.returncode, 0, result.stderr)
        manager.wait(timeout=2)
        self.assertIsNotNone(manager.returncode)
        self.assertIsNone(system.poll())
        self.assertFalse((self.repository / ".dev-pids/manager.pid").exists())
        self.assertTrue((self.repository / ".dev-pids/system.pid").exists())

    def test_exact_stop_rejects_non_online_host_before_stopping(self) -> None:
        manager = self._managed_process("manager")

        result = self._run("-manager", online_host="0")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ADDP_ONLINE_HOST", result.stderr)
        self.assertIsNone(manager.poll())

    def test_exact_stop_rejects_unrelated_pid(self) -> None:
        unrelated = subprocess.Popen(["sleep", "30"])
        self.processes.append(unrelated)
        (self.repository / ".dev-pids/manager.pid").write_text(
            f"{unrelated.pid}\n", encoding="utf-8"
        )

        result = self._run("-manager")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not the managed manager binary", result.stderr)
        self.assertIsNone(unrelated.poll())


if __name__ == "__main__":
    unittest.main()
