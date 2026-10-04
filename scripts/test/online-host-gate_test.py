import os
import signal
import shutil
import subprocess
import tempfile
import textwrap
import time
import unittest
from pathlib import Path
from unittest.mock import patch


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
        self.background_pids = self.external / "background-pids"
        (self.repository / "scripts/test").mkdir(parents=True)
        (self.repository / "scripts/infra").mkdir(parents=True)
        (self.repository / "scripts/dev").mkdir(parents=True)
        (self.repository / "business/scripts").mkdir(parents=True)
        self.external.mkdir()
        shutil.copy2(SCRIPT, self.repository / "scripts/test/online-host-gate.sh")
        shutil.copy2(PREFLIGHT, self.repository / "scripts/test/online-preflight.py")
        self._write_executable(
            "scripts/infra/up.sh",
            '#!/bin/bash\nprintf "infra-up\\n" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "scripts/dev/start.sh",
            textwrap.dedent(
                """\
                #!/bin/bash
                printf "start:%s\\n" "$*" >> "$ADDP_TEST_COMMAND_LOG"
                if [ "${ADDP_TEST_START_RETAINS_OUTPUT:-0}" = "1" ]; then
                  sleep 30 &
                  printf "%s\\n" "$!" >> "$ADDP_TEST_BACKGROUND_PIDS"
                  printf "started:%s\\n" "$*"
                fi
                """
            ),
        )
        self._write_executable(
            "scripts/dev/stop-exact-process.sh",
            '#!/bin/bash\nprintf "stop-exact:%s\\n" "$*" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "scripts/dev/stop.sh",
            textwrap.dedent(
                """\
                #!/bin/bash
                if [ -f "${ADDP_TEST_BACKGROUND_PIDS:-}" ]; then
                  while IFS= read -r pid; do
                    kill "$pid" 2>/dev/null || true
                  done < "$ADDP_TEST_BACKGROUND_PIDS"
                fi
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
        for command in ("docker", "go", "node", "curl", "lsof", "nc"):
            self._write_executable(command, "#!/bin/bash\nexit 0\n")
        self._write_executable(
            "npm",
            '#!/bin/bash\nprintf "npm:%s\\n" "$*" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "business/scripts/online-engine-fixture.sh",
            '#!/bin/bash\nprintf "fixture:%s\\n" "$1" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "business/scripts/online-workbench-mysql-fixture.sh",
            '#!/bin/bash\nprintf "mysql-fixture:%s\\n" "$1" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "business/scripts/online-oceanbase-consumer-fixture.sh",
            '#!/bin/bash\nprintf "oceanbase-fixture:%s\\n" "$1" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "business/scripts/online-tidb-consumer-fixture.sh",
            '#!/bin/bash\nprintf "tidb-fixture:%s\\n" "$1" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "business/scripts/online-transfer-insert-only-fixture.sh",
            '#!/bin/bash\nprintf "transfer-insert-only-fixture:%s\\n" "$1" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "business/scripts/online-manager-minio-fixture.sh",
            '#!/bin/bash\nprintf "manager-fixture:%s\\n" "$1" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "business/scripts/online-security-transfer-fixture.sh",
            '#!/bin/bash\nprintf "security-transfer-fixture:%s\\n" "$1" >> "$ADDP_TEST_COMMAND_LOG"\n',
        )
        self._write_executable(
            "scripts/test/consumer-engine-recovery-online.py",
            textwrap.dedent(
                """\
                #!/usr/bin/env python3
                import json
                import os
                import sys
                if "--restore-only" in sys.argv:
                    with open(os.environ["ADDP_TEST_COMMAND_LOG"], "a", encoding="utf-8") as log:
                        log.write("restore-engine\\n")
                print(json.dumps({"schema_version": "addp.consumer-engine-recovery/v1"}))
                """
            ),
        )
        self._write_executable(
            "scripts/test/consumer-process-stability-online.py",
            textwrap.dedent(
                """\
                #!/usr/bin/env python3
                import os
                import sys
                mode = "capture" if "--capture" in sys.argv else "verify"
                with open(os.environ["ADDP_TEST_COMMAND_LOG"], "a", encoding="utf-8") as log:
                    log.write(f"stability:{mode}\\n")
                print('{"schema_version":"addp.consumer-process-stability/v1"}')
                """
            ),
        )
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
                    "manager": {"instance_id": "manager-restarted-process" if phase in
                                {"manager-restarted", "manager-gracefully-stopped"} else "manager-online-process"},
                }
                if os.environ.get("ADDP_TEST_FAIL_PHASE") == phase:
                    raise SystemExit(1)
                if phase in {"system-recovered", "manager-abnormally-stopped", "manager-restarted", "manager-gracefully-stopped"}:
                    baseline = pathlib.Path(sys.argv[sys.argv.index("--baseline") + 1])
                    assert baseline.is_file()
                if phase == "manager-gracefully-stopped":
                    assert sys.argv[sys.argv.index("--expected-instance-id") + 1] == "manager-restarted-process"
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
                POSTGRES_HOST=127.0.0.1
                POSTGRES_PORT=25432
                POSTGRES_USER=online
                POSTGRES_PASSWORD=online-only-fixture-password
                SYSTEM_URL=http://127.0.0.1:8180
                GATEWAY_URL=http://127.0.0.1:8000
                MANAGER_URL=http://127.0.0.1:8081
                INFERENCE_URL=http://127.0.0.1:8094
                SERVICE_URL=http://127.0.0.1:8085
                WORKBENCH_URL=http://127.0.0.1:8095
                MONITOR_URL=http://127.0.0.1:8100
                CONSOLE_URL=http://127.0.0.1:5170
                STANDARD_URL=http://127.0.0.1:8110
                MODEL_URL=http://127.0.0.1:8181
                QUALITY_URL=http://127.0.0.1:8182
                ORCHESTRATOR_URL=http://127.0.0.1:8084
                ADDP_ONLINE_QUALITY_FIXTURES_JSON='[{"logical_table_id":10,"locator":"addp://engine/2/path/test/a?type=table","row_count":2},{"logical_table_id":11,"locator":"addp://engine/2/path/test/b?type=table","row_count":3}]'
                META_URL=http://127.0.0.1:8082
                SECURITY_URL=http://127.0.0.1:8194
                DEVELOP_URL=http://127.0.0.1:8084
                TRANSFER_URL=http://127.0.0.1:8083
                CATALOG_URL=http://127.0.0.1:8192
                ASSET_URL=http://127.0.0.1:8086
                PORTAL_URL=http://127.0.0.1:8088
                MANAGER_SERVICE_CLIENT_SECRET=manager-online-secret-0123456789abcdef
                SYSTEM_SERVICE_CLIENT_SECRET=system-online-secret-0123456789abcdef
                ADDP_HOST_NODE_IPS=192.0.2.10,2001:db8::10
                ADDP_ONLINE_TEST_PLATFORM_ACCESS_TOKEN=addp_at_online_platform
                ADDP_ONLINE_TEST_USER_ACCESS_TOKEN=addp_at_online
                ADDP_ONLINE_TEST_TENANT_ADMIN_ACCESS_TOKEN=addp_at_online_tenant_admin
                ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN=addp_at_online_approver
                ADDP_ONLINE_TEST_USER_USERNAME=online-user
                ADDP_ONLINE_TEST_USER_PASSWORD=online-password
                ADDP_ONLINE_TEST_ENGINE_ID=7
                ADDP_ONLINE_TEST_ENGINE_NAME='Online PostgreSQL Fixture'
                ADDP_ONLINE_TEST_ENGINE_PORT=55433
                ADDP_ONLINE_TEST_ENGINE_USER=online_engine
                ADDP_ONLINE_TEST_ENGINE_PASSWORD=online-engine-password
                ADDP_ONLINE_TEST_ENGINE_DATABASE=online_engine
                ADDP_ONLINE_TEST_CATALOG_DOMAIN_ID=31
                ADDP_ONLINE_TEST_CATALOG_DEPARTMENT_ID=41
                ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID=17
                ADDP_ONLINE_WORKBENCH_MYSQL_PORT=53306
                ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE=commerce_fixture
                ADDP_ONLINE_WORKBENCH_MYSQL_USER=workbench_reader
                ADDP_ONLINE_WORKBENCH_MYSQL_PASSWORD=reader-password-1234
                ADDP_ONLINE_WORKBENCH_MYSQL_ROOT_PASSWORD=root-password-1234
                ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_ID=57
                ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_NAME='Online Transfer MySQL Fixture'
                ADDP_ONLINE_TRANSFER_MYSQL_PORT=53307
                ADDP_ONLINE_TRANSFER_MYSQL_DATABASE=transfer_fixture
                ADDP_ONLINE_TRANSFER_MYSQL_USER=transfer_writer
                ADDP_ONLINE_TRANSFER_MYSQL_PASSWORD=writer-password-1234
                ADDP_ONLINE_TRANSFER_MYSQL_ROOT_PASSWORD=transfer-root-password-1234
                ADDP_ONLINE_OCEANBASE_ENGINE_ID=47
                ADDP_ONLINE_OCEANBASE_PORT=52881
                ADDP_ONLINE_OCEANBASE_DATABASE=oceanbase_fixture
                ADDP_ONLINE_OCEANBASE_USER=root@test
                ADDP_ONLINE_OCEANBASE_PASSWORD=oceanbase-password-1234
                ADDP_ONLINE_TIDB_ENGINE_ID=67
                ADDP_ONLINE_TIDB_PORT=54000
                ADDP_ONLINE_TIDB_DATABASE=tidb_fixture
                ADDP_ONLINE_TIDB_USER=root
                ADDP_ONLINE_TIDB_PASSWORD=
                ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID=27
                ADDP_ONLINE_MANAGER_MINIO_PORT=59002
                ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY=online-manager
                ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY=manager-secret-1234
                ADDP_ONLINE_MANAGER_MINIO_BUCKET=addp-online
                ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT=pointcloud/pdal_las12_format0.las
                ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT=document/addp_online_preview_fixture.pptx
                ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT=hybrid-search/purple-gaming-light-gun.jpg
                ADDP_ONLINE_MANAGER_EMBEDDING_MODEL_PROFILE_ID=11111111-1111-1111-1111-111111111111
                ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID=37
                ADDP_ONLINE_SECURITY_MONGODB_PORT=57017
                ADDP_ONLINE_SECURITY_MONGODB_DATABASE=security_online
                ADDP_ONLINE_SECURITY_MONGODB_USER=security_reader
                ADDP_ONLINE_SECURITY_MONGODB_PASSWORD=security-reader-1234
                ADDP_ONLINE_SECURITY_MONGODB_ROOT_USER=online_root
                ADDP_ONLINE_SECURITY_MONGODB_ROOT_PASSWORD=security-root-1234
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
        if self.background_pids.exists():
            for value in self.background_pids.read_text(
                encoding="utf-8"
            ).splitlines():
                try:
                    os.kill(int(value), signal.SIGTERM)
                except (ProcessLookupError, ValueError):
                    pass
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
                "ADDP_TEST_BACKGROUND_PIDS": str(self.background_pids),
                "ONLINE_SUITE": suite,
                "PATH": str(self.repository) + os.pathsep + environment["PATH"],
            }
        )
        environment.update(overrides)
        try:
            return subprocess.run(
                ["bash", "scripts/test/online-host-gate.sh", *arguments],
                cwd=self.repository,
                env=environment,
                capture_output=True,
                text=True,
                timeout=5,
            )
        except subprocess.TimeoutExpired as error:
            commands = (
                self.command_log.read_text(encoding="utf-8").splitlines()
                if self.command_log.exists() else []
            )
            readiness = "present" if (self.artifacts / "readiness.txt").exists() else "absent"
            cleanup = "present" if (self.artifacts / "summary.txt").exists() else "absent"
            error.add_note(
                f"Online host fixture: suite={suite}; "
                f"last_fixture_command={commands[-1] if commands else 'not-recorded'}; "
                f"readiness_report={readiness}; cleanup_report={cleanup}"
            )
            raise

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
                "stop-exact:--force -manager",
                "observe:manager-abnormally-stopped",
                "start:--exact-process -manager",
                "observe:manager-restarted",
                "stop-exact:-manager",
                "observe:manager-gracefully-stopped",
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

    def test_fault_phase_failure_still_cleans_up_and_fails_report(self):
        result = self._run("module-registry-recovery", ADDP_TEST_FAIL_PHASE="manager-abnormally-stopped")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.command_log.read_text().splitlines()[-1], "stop")
        summary = (self.artifacts / "summary.txt").read_text()
        self.assertIn("result=failed", summary)
        self.assertIn("cleanup=passed", summary)
        self.assertNotIn("process_lifecycle=passed", summary)

    def test_module_observer_configuration_required_before_lifecycle(self):
        original = self.env_file.read_text()
        for variable in ("ADDP_HOST_NODE_IPS", "ADDP_ONLINE_TEST_PLATFORM_ACCESS_TOKEN"):
            with self.subTest(variable=variable):
                self.env_file.write_text("\n".join(line for line in original.splitlines()
                                                     if not line.startswith(variable + "=")) + "\n")
                result = self._run("module-registry-recovery")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(variable, result.stderr)
                self.assertFalse(self.command_log.exists())

    def test_metric_lifecycle_has_no_self_hosted_route(self) -> None:
        result = self._run("metric-service-revision-lifecycle", "--check-only")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no dedicated deployment profile", result.stderr)
        self.assertFalse(self.command_log.exists())

    def test_maps_standard_model_suite_to_model_deployment(self) -> None:
        result = self._run("standard-model-reference-deletion")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("start:-model", self.command_log.read_text(encoding="utf-8"))

    def test_maps_quality_suite_to_full_deployment_and_stops_application(self) -> None:
        result = self._run("quality-dynamic-binding")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.command_log.read_text(encoding="utf-8").splitlines(),
            ["stop", "infra-up", "start:-all",
             "make:test-online:ONLINE_SUITE=quality-dynamic-binding", "stop"],
        )

    def test_quality_fixture_is_required_before_lifecycle_action(self) -> None:
        self.env_file.write_text(
            "\n".join(line for line in self.env_file.read_text(encoding="utf-8").splitlines()
                      if not line.startswith("ADDP_ONLINE_QUALITY_FIXTURES_JSON=")) + "\n",
            encoding="utf-8",
        )
        result = self._run("quality-dynamic-binding")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires ADDP_ONLINE_QUALITY_FIXTURES_JSON", result.stderr)
        self.assertFalse(self.command_log.exists())

    def test_runs_consumer_engine_fixture_and_verifies_process_stability(self) -> None:
        result = self._run("consumer-engine-recovery")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.command_log.read_text(encoding="utf-8").splitlines(),
            [
                "stop",
                "infra-up",
                "fixture:stop",
                "fixture:start",
                "start:",
                "npm:--prefix console/frontend exec -- playwright install chromium",
                "stability:capture",
                "make:test-online:ONLINE_SUITE=consumer-engine-recovery",
                "stability:verify",
                "fixture:start",
                "restore-engine",
                "fixture:stop",
                "stop",
            ],
        )
        summary = (self.artifacts / "summary.txt").read_text(encoding="utf-8")
        self.assertIn("process_lifecycle=passed", summary)

    def test_timeout_keeps_original_failure_and_fixture_progress(self) -> None:
        self.command_log.write_text("start:\nstability:capture\n", encoding="utf-8")
        self.artifacts.mkdir()
        (self.artifacts / "readiness.txt").write_text("result=passed\n", encoding="utf-8")
        failure = subprocess.TimeoutExpired("online-host-gate", 5)

        with patch("subprocess.run", side_effect=failure) as run:
            with self.assertRaises(subprocess.TimeoutExpired) as raised:
                self._run("consumer-engine-recovery")

        self.assertIs(raised.exception, failure)
        run.assert_called_once()
        diagnostic = "\n".join(raised.exception.__notes__)
        self.assertIn("suite=consumer-engine-recovery", diagnostic)
        self.assertIn("last_fixture_command=stability:capture", diagnostic)
        self.assertIn("readiness_report=present", diagnostic)
        self.assertIn("cleanup_report=absent", diagnostic)

    def test_timeout_before_lifecycle_reports_missing_progress(self) -> None:
        failure = subprocess.TimeoutExpired("online-host-gate", 5)
        with patch("subprocess.run", side_effect=failure):
            with self.assertRaises(subprocess.TimeoutExpired) as raised:
                self._run("enterprise-catalog-publishing")

        self.assertIs(raised.exception, failure)
        diagnostic = "\n".join(raised.exception.__notes__)
        self.assertIn("last_fixture_command=not-recorded", diagnostic)
        self.assertIn("readiness_report=absent", diagnostic)
        self.assertIn("cleanup_report=absent", diagnostic)

    def test_runs_enterprise_catalog_suite_with_seeded_engine_fixture(self) -> None:
        result = self._run("enterprise-catalog-publishing")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.command_log.read_text(encoding="utf-8").splitlines(),
            [
                "stop",
                "infra-up",
                "fixture:stop",
                "fixture:start",
                "start:-all",
                "npm:--prefix console/frontend exec -- playwright install chromium",
                "make:test-online:ONLINE_SUITE=enterprise-catalog-publishing",
                "fixture:stop",
                "stop",
            ],
        )

    def test_catalog_missing_observer_database_input_fails_before_lifecycle(self) -> None:
        original = self.env_file.read_text(encoding="utf-8")
        self.env_file.write_text(original.replace("POSTGRES_HOST=127.0.0.1", "POSTGRES_HOST="), encoding="utf-8")
        result = self._run("enterprise-catalog-publishing")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("POSTGRES_HOST", result.stderr)
        self.assertFalse(self.command_log.exists())

    def test_catalog_nonlocal_or_invalid_database_input_fails_before_lifecycle(self) -> None:
        original = self.env_file.read_text(encoding="utf-8")
        for source, target in (("POSTGRES_HOST=127.0.0.1", "POSTGRES_HOST=remote.example"),
                               ("POSTGRES_PORT=25432", "POSTGRES_PORT=0")):
            with self.subTest(target=target):
                self.env_file.write_text(original.replace(source, target), encoding="utf-8")
                result = self._run("enterprise-catalog-publishing")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("issuance observation requires", result.stderr)
                self.assertFalse(self.command_log.exists())

    def test_catalog_missing_basis_secret_fails_before_lifecycle(self) -> None:
        contents = self.env_file.read_text(encoding="utf-8")
        self.env_file.write_text(contents.replace(
            "SYSTEM_SERVICE_CLIENT_SECRET=system-online-secret-0123456789abcdef",
            "SYSTEM_SERVICE_CLIENT_SECRET="), encoding="utf-8")
        result = self._run("enterprise-catalog-publishing")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SYSTEM_SERVICE_CLIENT_SECRET", result.stderr)
        self.assertFalse(self.command_log.exists())

    def test_runs_workbench_suite_with_read_only_mysql_fixture(self) -> None:
        result = self._run("workbench-service-consumption")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.command_log.read_text(encoding="utf-8").splitlines(),
            [
                "stop",
                "infra-up",
                "mysql-fixture:stop",
                "mysql-fixture:start",
                "start:-all",
                "npm:--prefix console/frontend exec -- playwright install chromium",
                "make:test-online:ONLINE_SUITE=workbench-service-consumption",
                "mysql-fixture:stop",
                "stop",
            ],
        )

    def test_runs_transfer_insert_only_suite_with_owned_browser_fixture(self) -> None:
        result = self._run("transfer-insert-only-mysql")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.command_log.read_text(encoding="utf-8").splitlines(),
            [
                "stop",
                "infra-up",
                "transfer-insert-only-fixture:stop",
                "transfer-insert-only-fixture:start",
                "start:-all",
                "npm:--prefix console/frontend exec -- playwright install chromium",
                "make:test-online:ONLINE_SUITE=transfer-insert-only-mysql",
                "transfer-insert-only-fixture:stop",
                "stop",
            ],
        )

    def test_transfer_relational_sql_etl_has_no_self_hosted_route(self) -> None:
        result = self._run("transfer-relational-sql-etl", "--check-only")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.command_log.exists())

    def test_runs_oceanbase_consumer_flow_with_owned_fixture(self) -> None:
        result = self._run("oceanbase-consumer-flow")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.command_log.read_text(encoding="utf-8").splitlines(),
            [
                "stop",
                "infra-up",
                "oceanbase-fixture:stop",
                "oceanbase-fixture:start",
                "start:-all",
                "make:test-online:ONLINE_SUITE=oceanbase-consumer-flow",
                "stop",
                "oceanbase-fixture:stop",
            ],
        )

    def test_runs_tidb_consumer_flow_with_owned_fixture(self) -> None:
        result = self._run("tidb-consumer-flow")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.command_log.read_text(encoding="utf-8").splitlines(),
            [
                "stop",
                "infra-up",
                "tidb-fixture:stop",
                "tidb-fixture:start",
                "start:-all",
                "make:test-online:ONLINE_SUITE=tidb-consumer-flow",
                "stop",
                "tidb-fixture:stop",
            ],
        )

    def test_daemon_launcher_does_not_keep_gate_log_pipe_open(self) -> None:
        result = self._run(
            "standard-model-reference-deletion",
            ADDP_TEST_START_RETAINS_OUTPUT="1",
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("started:-model", result.stdout)
        summary = (self.artifacts / "summary.txt").read_text(encoding="utf-8")
        self.assertIn("result=passed", summary)
        self.assertIn("cleanup=passed", summary)

    def test_manager_lineage_rejects_removed_self_hosted_profile_before_mutation(self) -> None:
        result = self._run("manager-internal-artifact-lineage")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.command_log.exists())

    def test_runs_manager_hybrid_search_suite_with_manager_minio_fixture(self) -> None:
        result = self._run("manager-hybrid-search")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.command_log.read_text(encoding="utf-8").splitlines(),
            [
                "stop",
                "infra-up",
                "manager-fixture:stop",
                "manager-fixture:start",
                "start:-all",
                "make:test-online:ONLINE_SUITE=manager-hybrid-search",
                "manager-fixture:stop",
                "stop",
            ],
        )

    def test_runs_security_transfer_suite_with_composite_fixture(self) -> None:
        result = self._run("security-transfer-protection")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.command_log.read_text(encoding="utf-8").splitlines(),
            [
                "stop",
                "infra-up",
                "security-transfer-fixture:stop",
                "security-transfer-fixture:start",
                "start:-all",
                "make:test-online:ONLINE_SUITE=security-transfer-protection",
                "security-transfer-fixture:stop",
                "stop",
            ],
        )

    def test_runs_security_exemption_suite_with_composite_fixture(self) -> None:
        result = self._run("security-plaintext-access")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.command_log.read_text(encoding="utf-8").splitlines(),
            [
                "stop",
                "infra-up",
                "security-transfer-fixture:stop",
                "security-transfer-fixture:start",
                "start:-all",
                "make:test-online:ONLINE_SUITE=security-plaintext-access",
                "security-transfer-fixture:stop",
                "stop",
            ],
        )

    def test_rejects_security_hosted_suite_before_lifecycle(self) -> None:
        result = self._run("security-mysql-owner-protection")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.command_log.exists())

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

    def _managed_process(self, module: str, *, cwd=None) -> subprocess.Popen[bytes]:
        binary = self.repository / f".dev-bins/addp-{module}"
        binary.write_text(
            f"#!/usr/bin/env bash\ntrap 'touch {self.repository}/term-observed; exit 0' TERM INT\nwhile true; do sleep 1; done\n",
            encoding="utf-8",
        )
        binary.chmod(0o755)
        process = subprocess.Popen([str(binary)], cwd=cwd or self.repository)
        self.processes.append(process)
        (self.repository / f".dev-pids/{module}.pid").write_text(
            f"{process.pid}\n", encoding="utf-8"
        )
        time.sleep(0.05)
        return process

    def _run(self, selector: str, *, online_host: str = "1", force: bool = False) -> subprocess.CompletedProcess[str]:
        environment = dict(os.environ)
        environment["ADDP_ONLINE_HOST"] = online_host
        return subprocess.run(
            ["bash", "scripts/dev/stop-exact-process.sh", *(["--force"] if force else []), selector],
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

    def test_force_stop_skips_term_and_keeps_other_process_alive(self):
        manager = self._managed_process("manager")
        gateway = self._managed_process("gateway")
        result = self._run("-manager", force=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(manager.wait(timeout=2), -9)
        self.assertFalse((self.repository / "term-observed").exists())
        self.assertFalse((self.repository / ".dev-pids/manager.pid").exists())
        self.assertIsNone(gateway.poll())

    def test_force_stop_rejects_managed_name_from_foreign_checkout(self):
        manager = self._managed_process("manager", cwd=self.repository.parent)
        result = self._run("-manager", force=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not belong to this checkout", result.stderr)
        self.assertIsNone(manager.poll())

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
