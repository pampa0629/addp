import importlib.util
import re
import shutil
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-online-ci-registration.py")
SPEC = importlib.util.spec_from_file_location("check_online_ci_registration", SCRIPT)
CHECK = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = CHECK
SPEC.loader.exec_module(CHECK)


class OnlineCIRegistrationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-registration-")
        self.repository = Path(self.temporary.name)
        (self.repository / "scripts/test").mkdir(parents=True)
        (self.repository / ".github/workflows").mkdir(parents=True)
        (self.repository / "scripts/test/online-gate.py").write_text(
            textwrap.dedent(
                """\
                from dataclasses import dataclass
                @dataclass(frozen=True)
                class Suite:
                    command: tuple[str, ...]
                    services: tuple[tuple[str, str], ...]
                SUITES = {
                    "first-suite": Suite(("first",), (("system", "SYSTEM_URL"),)),
                    "second-suite": Suite(("second",), (("gateway", "GATEWAY_URL"),)),
                }
                """
            ),
            encoding="utf-8",
        )
        (self.repository / "scripts/test/online-host-gate.sh").write_text(
            textwrap.dedent(
                """\
                case "$ONLINE_SUITE" in
                  first-suite)
                    START_TARGET=-system
                    ;;
                  second-suite)
                    START_TARGET=-model
                    ;;
                esac
                python3 scripts/test/online-preflight.py --environment-only
                printf 'database=%s\\n' "$POSTGRES_DB"
                run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
                """
            ),
            encoding="utf-8",
        )
        self.workflow = self.repository / ".github/workflows/online-t4-gates.yml"
        self.workflow.write_text(self._workflow(), encoding="utf-8")

    def tearDown(self) -> None:
        self.temporary.cleanup()

    @staticmethod
    def _workflow() -> str:
        return textwrap.dedent(
            """\
            on:
              workflow_dispatch:
                inputs:
                  suite:
                    options:
                      - first-suite
                      - second-suite
            permissions:
              contents: read
            jobs:
              online:
                if: github.event_name == 'workflow_dispatch'
                runs-on:
                  - self-hosted
                  - macOS
                  - addp-online
                environment: addp-online
                env:
                  ADDP_ONLINE_ENV_FILE: ${{ vars.ADDP_ONLINE_ENV_FILE }}
                steps:
                  - env:
                      ADDP_ONLINE_ARTIFACT_DIR: ${{ runner.temp }}/addp-online-${{ github.run_id }}
                    run: bash scripts/test/online-host-gate.sh --check-only
                  - env:
                      ADDP_ONLINE_ARTIFACT_DIR: ${{ runner.temp }}/addp-online-${{ github.run_id }}
                    run: bash scripts/test/online-host-gate.sh
                  - uses: actions/upload-artifact@0123456789abcdef0123456789abcdef01234567
            """
        )

    def test_public_origin_requires_browser_preparation_dispatch_and_source(self) -> None:
        source = Path(__file__).resolve().parents[2]
        paths = (
            "scripts/test/online-hosted-public-origin-gate.sh", "scripts/test/compose-public-origin-online.py",
            "console/frontend/e2e/online/compose-public-origin.spec.js", "system/backend/cmd/online-test-fixture/main.go",
        )
        for relative in paths:
            destination = self.repository / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, destination)
        registered = {"compose-public-origin"}
        CHECK.validate_public_origin_browser_profile(self.repository, registered)
        for relative, fragment in (
            (paths[0], "playwright install --with-deps chromium"), (paths[1], "browser = run_browser()"),
            (paths[2], "ADDP_ONLINE_PUBLIC_ORIGIN_BROWSER_REPORT"), (paths[3], 'values["ADDP_ONLINE_READ_USER_PASSWORD"]'),
        ):
            path = self.repository / relative
            original = path.read_text()
            path.write_text(original.replace(fragment, "removed"))
            with self.subTest(relative=relative), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_public_origin_browser_profile(self.repository, registered)
            path.write_text(original)
        (self.repository / paths[2]).unlink()
        with self.assertRaises(CHECK.RegistrationError):
            CHECK.validate_public_origin_browser_profile(self.repository, registered)

    def test_accepts_one_profile_and_workflow_choice_per_registered_suite(self) -> None:
        CHECK.check_registration(self.repository)

    def test_hdfs_requires_manual_java11_lifecycle_browser_and_owner_tests(self) -> None:
        source = SCRIPT.parents[2]
        paths = (
            "scripts/test/online-hosted-hdfs-gate.sh", "scripts/test/online-gate.py",
            "business/scripts/online-hdfs-spark-fixture.sh", "scripts/test/hdfs-spark-consumer-flow-online.py",
            "console/frontend/e2e/online/hdfs-spark-consumer-flow.spec.js", "Makefile",
            ".github/workflows/online-t4-gates.yml", "scripts/test/spark-online-evidence.py",
        )
        for relative in paths:
            destination = self.repository / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, destination)
        registered = {"hdfs-spark-consumer-flow"}
        CHECK.validate_hdfs_spark_profile(self.repository, registered)
        for relative, fragment in (
            (paths[0], 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
            (paths[0], 'make build-images'), (paths[0], 'hdfs-runtime-build.txt'),
            (paths[0], 'hdfs-runtime.log'), (paths[0], 'com.addp.online-runtime'),
            (paths[2], 'refusing to delete a foreign container'),
            (paths[3], 'SPARK.worker_evidence'), (paths[7], 'Finished task'), (paths[4], 'login('),
            (paths[5], '$(MAKE) test-hdfs-online-runner'),
            (paths[6], "java-version: '11'"),
            (paths[6], "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'hdfs-spark-consumer-flow'\n"),
        ):
            path = self.repository / relative
            original = path.read_text()
            path.write_text(original.replace(fragment, 'removed'))
            with self.subTest(relative=relative, fragment=fragment), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_hdfs_spark_profile(self.repository, registered)
            path.write_text(original)

    def test_elasticsearch_spark_requires_java_worker_runtime_and_console_evidence(self) -> None:
        source = SCRIPT.parents[2]
        paths = ('.github/workflows/online-t4-gates.yml', 'scripts/test/online-hosted-elasticsearch-gate.sh',
                 'scripts/test/elasticsearch-consumer-flow-online.py', 'scripts/test/spark-online-evidence.py',
                 'console/frontend/e2e/online/elasticsearch-consumer-flow.spec.js', 'Makefile')
        for relative in paths:
            destination = self.repository / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source / relative, destination)
        CHECK.validate_elasticsearch_spark_profile(self.repository, {'elasticsearch-consumer-flow'})
        for relative, fragment in ((paths[0], "java-version: '11'"), (paths[1], '-spark-workflow'),
                                   (paths[2], 'validate_workflow_nodes'), (paths[3], 'Finished task'),
                                   (paths[4], '.workflow-final-result-json'), (paths[5], '$(MAKE) test-elasticsearch-online-runner')):
            path = self.repository / relative
            original = path.read_text()
            path.write_text(original.replace(fragment, 'removed'))
            with self.subTest(relative=relative), self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_elasticsearch_spark_profile(self.repository, {'elasticsearch-consumer-flow'})
            path.write_text(original)

    def test_hosted_setup_node_pin_matches_platform_ci(self) -> None:
        repository = SCRIPT.parents[2]
        online = (
            repository / ".github/workflows/online-t4-gates.yml"
        ).read_text(encoding="utf-8")
        platform = (
            repository / ".github/workflows/platform-ci.yml"
        ).read_text(encoding="utf-8")

        def setup_node_pin(text: str) -> str:
            match = re.search(r"actions/setup-node@([0-9a-f]{40})", text)
            self.assertIsNotNone(match)
            return match.group(1)  # type: ignore[union-attr]

        self.assertEqual(setup_node_pin(online), setup_node_pin(platform))

    def test_metric_engine_variant_is_explicitly_wired_to_hosted_job(self) -> None:
        actual = (SCRIPT.parents[2] / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
        self.workflow.write_text(actual, encoding="utf-8")
        registered = {"metric-service-revision-lifecycle"}
        CHECK.validate_metric_engine_variant(self.repository, registered)
        for missing in ("        default: postgresql\n", "          - tidb\n",
                        "      fail-fast: false\n",
                        "        metric_engine: ${{ fromJSON(github.event_name == 'schedule' && '[\"postgresql\",\"tidb\"]' || format('[\"{0}\"]', inputs.metric_engine)) }}\n",
                        "      group: online-t4-metric-service-revision-lifecycle-${{ matrix.metric_engine }}\n",
                        "      ADDP_ONLINE_METRIC_ENGINE_TYPE: ${{ matrix.metric_engine }}\n"):
            with self.subTest(missing=missing):
                self.workflow.write_text(actual.replace(missing, "", 1), encoding="utf-8")
                with self.assertRaises(CHECK.RegistrationError):
                    CHECK.validate_metric_engine_variant(self.repository, registered)

    def test_discovers_hosted_profile_from_metadata(self) -> None:
        hosted = self.repository / "scripts/test/online-hosted-example-gate.sh"
        hosted.write_text(
            "# ADDP_ONLINE_SUITES=hosted-suite\n"
            "# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64\n",
            encoding="utf-8",
        )

        profiles = CHECK.load_deployment_profiles(self.repository)

        self.assertEqual(
            profiles["hosted-suite"], "github-hosted-linux-x86_64"
        )

    def prepare_orchestrator_profile(self):
        root = SCRIPT.parents[2]
        for relative in (
            "scripts/test/online-hosted-orchestrator-gate.sh",
            "scripts/test/orchestrator-execution-online.py",
            "scripts/test/orchestrator-execution-online_test.py",
            "scripts/test/orchestrator-execution-faults.py",
            "scripts/test/orchestrator-execution-faults_test.py",
            "scripts/test/online-hosted-orchestrator-gate_test.py",
            "business/scripts/online-metric-postgres-fixture.sh",
            "Makefile",
            ".github/workflows/online-t4-gates.yml",
        ):
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / relative, target)

    def test_orchestrator_profile_is_hosted_manual_and_uses_real_owners(self):
        self.prepare_orchestrator_profile()
        CHECK.validate_orchestrator_execution_profile(self.repository, {"orchestrator-execution"})
        gate = self.repository / "scripts/test/online-hosted-orchestrator-gate.sh"
        gate.write_text(gate.read_text().replace("-meta -orchestrator -monitor", "-orchestrator"))
        with self.assertRaisesRegex(CHECK.RegistrationError, "-meta -orchestrator -monitor"):
            CHECK.validate_orchestrator_execution_profile(self.repository, {"orchestrator-execution"})

    def test_orchestrator_fault_tests_cannot_be_removed_from_the_owner_entry(self):
        self.prepare_orchestrator_profile()
        makefile = self.repository / "Makefile"
        text = makefile.read_text()
        for missing in ("$(MAKE) test-orchestrator-online-runner", "scripts/test/orchestrator-execution-faults_test.py"):
            with self.subTest(missing=missing):
                makefile.write_text(text.replace(missing, ""))
                with self.assertRaises(CHECK.RegistrationError):
                    CHECK.validate_orchestrator_execution_profile(self.repository, {"orchestrator-execution"})

    def test_orchestrator_cannot_start_nightly_or_also_target_personal_runner(self):
        self.prepare_orchestrator_profile()
        original = self.workflow.read_text()
        manual = "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'orchestrator-execution'"
        self.workflow.write_text(original.replace(manual, "    if: github.event_name == 'schedule'"))
        with self.assertRaisesRegex(CHECK.RegistrationError, "manual Hosted"):
            CHECK.validate_orchestrator_execution_profile(self.repository, {"orchestrator-execution"})
        self.workflow.write_text(original.replace("&& inputs.suite != 'orchestrator-execution'", ""))
        with self.assertRaisesRegex(CHECK.RegistrationError, "self-hosted"):
            CHECK.validate_orchestrator_execution_profile(self.repository, {"orchestrator-execution"})

    def test_discovers_owner_managed_profile_from_metadata(self) -> None:
        owner_managed = self.repository / "scripts/test/online-owner-managed-example-gate.sh"
        owner_managed.write_text(
            "# ADDP_ONLINE_SUITES=licensed-suite\n"
            "# ADDP_ONLINE_RUNNER=owner-managed-linux-x86_64\n",
            encoding="utf-8",
        )

        profiles = CHECK.load_deployment_profiles(self.repository)

        self.assertEqual(profiles["licensed-suite"], "owner-managed-linux-x86_64")

    def test_rejects_hosted_profile_without_runner_metadata(self) -> None:
        hosted = self.repository / "scripts/test/online-hosted-example-gate.sh"
        hosted.write_text(
            "# ADDP_ONLINE_SUITES=hosted-suite\n", encoding="utf-8"
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "Hosted Online metadata"):
            CHECK.load_deployment_profiles(self.repository)

    def test_rejects_missing_deployment_profile(self) -> None:
        script = self.repository / "scripts/test/online-host-gate.sh"
        script.write_text(
            script.read_text(encoding="utf-8").replace(
                "  second-suite)\n    START_TARGET=-model\n    ;;\n", ""
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "do not match"):
            CHECK.check_registration(self.repository)

    def test_rejects_nightly_schedule_before_first_real_run(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "  workflow_dispatch:\n", "  schedule:\n    - cron: '0 1 * * *'\n  workflow_dispatch:\n"
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "must remain manual"):
            CHECK.check_registration(self.repository)

    def test_rejects_self_hosted_job_without_manual_dispatch_guard(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "    if: github.event_name == 'workflow_dispatch'\n", ""
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(
            CHECK.RegistrationError, "workflow_dispatch or a fixed schedule"
        ):
            CHECK.check_registration(self.repository)

    def test_rejects_self_hosted_job_with_repository_secret(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "      ADDP_ONLINE_ENV_FILE: ${{ vars.ADDP_ONLINE_ENV_FILE }}\n",
                "      ADDP_ONLINE_ENV_FILE: ${{ vars.ADDP_ONLINE_ENV_FILE }}\n"
                "      FORBIDDEN_SECRET: ${{ secrets.FORBIDDEN_SECRET }}\n",
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(
            CHECK.RegistrationError, "must not receive repository secrets"
        ):
            CHECK.check_registration(self.repository)

    def test_rejects_self_hosted_job_with_unpinned_action(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "actions/upload-artifact@0123456789abcdef0123456789abcdef01234567",
                "actions/upload-artifact@v7",
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "immutable commit SHA"):
            CHECK.check_registration(self.repository)

    def test_rejects_online_workflow_with_write_token(self) -> None:
        self.workflow.write_text(
            self._workflow().replace("contents: read", "contents: write"),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "permissions"):
            CHECK.check_registration(self.repository)

    def test_rejects_graduated_nightly_suite_without_workflow_schedule(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8")
            .replace(
                "    services: tuple[tuple[str, str], ...]\n",
                "    services: tuple[tuple[str, str], ...]\n"
                "    nightly: bool = False\n",
            )
            .replace(
                '"second-suite": Suite(("second",), (("gateway", "GATEWAY_URL"),)),',
                '"second-suite": Suite('
                '("second",), (("gateway", "GATEWAY_URL"),), nightly=True),',
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "nightly suite"):
            CHECK.check_registration(self.repository)

    def test_rejects_nightly_job_without_fixed_suite_concurrency(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8")
            .replace(
                "    services: tuple[tuple[str, str], ...]\n",
                "    services: tuple[tuple[str, str], ...]\n"
                "    nightly: bool = False\n",
            )
            .replace(
                '"second-suite": Suite(("second",), (("gateway", "GATEWAY_URL"),)),',
                '"second-suite": Suite('
                '("second",), (("gateway", "GATEWAY_URL"),), nightly=True),',
            ),
            encoding="utf-8",
        )
        self.workflow.write_text(
            self._workflow()
            .replace(
                "  workflow_dispatch:\n",
                "  schedule:\n    - cron: '0 1 * * *'\n  workflow_dispatch:\n",
            )
            .replace(
                "  online:\n",
                "  online:\n"
                "    if: github.event_name == 'schedule'\n"
                "    env:\n"
                "      ONLINE_SUITE_INPUT: second-suite\n",
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "concurrency group"):
            CHECK.check_registration(self.repository)

    def test_repository_schedules_graduated_hosted_profiles(self) -> None:
        repository = SCRIPT.parents[2]

        registry = CHECK.load_suite_registry(repository)
        nightly = CHECK.load_nightly_suites(registry)

        self.assertEqual(
            nightly,
            {
                "compose-public-origin",
                "opengauss-consumer-flow",
                "metric-service-revision-lifecycle",
            },
        )
        CHECK.check_registration(repository)

    def test_rejects_workflow_without_readiness_check(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "      - env:\n"
                "          ADDP_ONLINE_ARTIFACT_DIR: "
                "${{ runner.temp }}/addp-online-${{ github.run_id }}\n"
                "        run: bash scripts/test/online-host-gate.sh --check-only\n",
                "",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "--check-only"):
            CHECK.check_registration(self.repository)

    def test_rejects_runner_context_in_job_level_environment(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "    steps:\n",
                "    env:\n"
                "      ADDP_ONLINE_ARTIFACT_DIR: ${{ runner.temp }}/invalid\n"
                "    steps:\n",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "job-level env"):
            CHECK.check_registration(self.repository)

    def test_requires_artifact_directory_on_both_lifecycle_steps(self) -> None:
        self.workflow.write_text(
            self._workflow().replace(
                "      - env:\n"
                "          ADDP_ONLINE_ARTIFACT_DIR: "
                "${{ runner.temp }}/addp-online-${{ github.run_id }}\n"
                "        run: bash scripts/test/online-host-gate.sh\n",
                "      - run: bash scripts/test/online-host-gate.sh\n",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "both lifecycle steps"):
            CHECK.check_registration(self.repository)

    def test_rejects_host_gate_without_shared_environment_preflight(self) -> None:
        script = self.repository / "scripts/test/online-host-gate.sh"
        script.write_text(
            script.read_text(encoding="utf-8").replace(
                "python3 scripts/test/online-preflight.py --environment-only\n", ""
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "environment-only"):
            CHECK.check_registration(self.repository)

    def test_rejects_module_registry_suite_without_formal_process_profile(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8").replace(
                '"first-suite"', '"module-registry-recovery"'
            ),
            encoding="utf-8",
        )
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "first-suite", "module-registry-recovery"
            ),
            encoding="utf-8",
        )
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "first-suite", "module-registry-recovery"
            ),
            encoding="utf-8",
        )

        with self.assertRaisesRegex(CHECK.RegistrationError, "process profile is missing"):
            CHECK.check_registration(self.repository)

    def test_module_profile_requires_force_stage_and_observer_configuration(self):
        source = Path(__file__).resolve().parents[2]
        paths = ("scripts/test/online-host-gate.sh", "scripts/dev/stop-exact-process.sh",
                 "scripts/test/module-lifecycle-process-online.py", "scripts/dev/start.sh")
        for relative in paths:
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text((source / relative).read_text())
        CHECK.validate_module_registry_process_profile(self.repository, {"module-registry-recovery"})
        host = self.repository / paths[0]
        original = host.read_text()
        for fragment in ("bash scripts/dev/stop-exact-process.sh --force -manager",
                         "observe_module_lifecycle manager-restarted",
                         "ADDP_ONLINE_TEST_PLATFORM_ACCESS_TOKEN", "ADDP_HOST_NODE_IPS"):
            with self.subTest(fragment=fragment):
                host.write_text(original.replace(fragment, "REMOVED"))
                with self.assertRaisesRegex(CHECK.RegistrationError, "process profile is missing"):
                    CHECK.validate_module_registry_process_profile(self.repository, {"module-registry-recovery"})
        host.write_text(original)

    def test_requires_consumer_engine_recovery_lifecycle_and_browser_assets(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8").replace(
                '"first-suite"', '"consumer-engine-recovery"'
            ),
            encoding="utf-8",
        )
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "first-suite", "consumer-engine-recovery"
            )
            + "\nbash business/scripts/online-engine-fixture.sh start\n"
            + "bash business/scripts/online-engine-fixture.sh stop\n"
            + "bash scripts/dev/start.sh\n"
            + "playwright install chromium\n"
            + "python3 scripts/test/consumer-process-stability-online.py\n"
            + "python3 scripts/test/consumer-engine-recovery-online.py --restore-only\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "first-suite", "consumer-engine-recovery"
            ),
            encoding="utf-8",
        )
        required = (
            "business/scripts/online-engine-fixture.sh",
            "scripts/test/consumer-engine-recovery-online.py",
            "scripts/test/consumer-process-stability-online.py",
            "console/frontend/playwright.online.config.js",
            "console/frontend/e2e/online/consumer-engine-recovery.spec.js",
        )
        for relative in required:
            path = self.repository / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            content = "ADDP_ONLINE_HOST --env-file /dev/null business-postgres\n" if relative.startswith("business/") else "fixture\n"
            path.write_text(content, encoding="utf-8")

        CHECK.check_registration(self.repository)
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "python3 scripts/test/consumer-engine-recovery-online.py --restore-only\n",
                "",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "process profile is missing"):
            CHECK.check_registration(self.repository)

    def test_requires_workbench_mysql_fixture_and_owner_suite(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8").replace(
                '"first-suite"', '"workbench-service-consumption"'
            ),
            encoding="utf-8",
        )
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "first-suite)\n    START_TARGET=-system",
                "workbench-service-consumption)\n    START_TARGET=-all",
            )
            + "\nSYSTEM_URL GATEWAY_URL SERVICE_URL WORKBENCH_URL CONSOLE_URL\n"
            + "ADDP_ONLINE_TEST_USER_USERNAME ADDP_ONLINE_TEST_USER_PASSWORD\n"
            + "ADDP_ONLINE_TEST_TENANT_ADMIN_ACCESS_TOKEN\n"
            + "ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID\n"
            + "bash business/scripts/online-workbench-mysql-fixture.sh start\n"
            + "bash business/scripts/online-workbench-mysql-fixture.sh stop\n"
            + 'bash scripts/dev/start.sh "$START_TARGET"\n'
            + "playwright install chromium\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "first-suite", "workbench-service-consumption"
            ),
            encoding="utf-8",
        )
        fixture = self.repository / "business/scripts/online-workbench-mysql-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        fixture.write_text(
            "ADDP_ONLINE_HOST --env-file /dev/null business-mysql "
            "REVOKE ALL PRIVILEGES, GRANT OPTION GRANT SELECT ON\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/workbench-service-consumption-online.py"
        owner.write_text("fixture\n", encoding="utf-8")
        for relative in (
            "console/frontend/playwright.online.config.js",
            "console/frontend/e2e/online/workbench-service-consumption.spec.js",
        ):
            path = self.repository / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("fixture\n", encoding="utf-8")

        CHECK.check_registration(self.repository)
        owner.unlink()
        with self.assertRaisesRegex(CHECK.RegistrationError, "requires"):
            CHECK.check_registration(self.repository)

    def test_requires_mysql_four_owner_protection_fixtures_and_contract(self) -> None:
        root = SCRIPT.parents[2]
        for relative in (
            "scripts/test/online-hosted-security-gate.sh", "scripts/test/online-host-gate.sh",
            ".github/workflows/online-t4-gates.yml", "Makefile",
            "business/scripts/online-security-owner-fixture.sh",
            "scripts/test/security-mysql-owner-protection-online.py",
            "scripts/test/security-transfer-protection-online.py",
            "scripts/test/online-gate.py",
            "console/frontend/e2e/online/security-manager-export.spec.js",
        ):
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / relative, target)
        registered = {"security-mysql-owner-protection"}
        CHECK.validate_security_mysql_owner_protection_profile(self.repository, registered)
        for relative, fragment, message in (
            ("scripts/test/security-mysql-owner-protection-online.py", '"rollback_verified"', "owner contract is missing"),
            ("scripts/test/online-hosted-security-gate.sh", "playwright install --with-deps chromium", "Hosted profile is missing"),
            ("scripts/test/online-hosted-security-gate.sh", "-transfer -monitor", "Hosted profile is missing"),
            ("scripts/test/online-gate.py", '("monitor", "MONITOR_URL"),', "must preflight Monitor"),
            ("scripts/test/security-mysql-owner-protection-online.py", "run_export_browser(", "must dispatch and validate"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "execution.triggered_by", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "download.delete()", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "format: 'csv'", "export browser contract is missing"),
            ("console/frontend/e2e/online/security-manager-export.spec.js", "csv.DictReader", "export browser contract is missing"),
            ("business/scripts/online-security-owner-fixture.sh", "container ownership mismatch", "fixture contract is missing"),
            ("scripts/test/online-hosted-security-gate.sh", "ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN", "Hosted profile is missing"),
            (".github/workflows/online-t4-gates.yml", "&& inputs.suite != 'security-mysql-owner-protection'", "must not also dispatch"),
            ("Makefile", "online-hosted-security-gate_test.py", "regression must enter"),
        ):
            with self.subTest(fragment=fragment):
                path = self.repository / relative
                original = path.read_text()
                path.write_text(original.replace(fragment, ""))
                try:
                    with self.assertRaisesRegex(CHECK.RegistrationError, message):
                        CHECK.validate_security_mysql_owner_protection_profile(self.repository, registered)
                finally:
                    path.write_text(original)
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(host.read_text() + "\nsecurity-mysql-owner-protection)\n")
        with self.assertRaisesRegex(CHECK.RegistrationError, "self-hosted route"):
            CHECK.validate_security_mysql_owner_protection_profile(self.repository, registered)

    def test_requires_transfer_field_lineage_services_proofs_and_cleanup(self) -> None:
        root = SCRIPT.parents[2]
        relatives = (
            "scripts/test/online-gate.py", "scripts/test/online-host-gate.sh",
            "scripts/test/online-hosted-transfer-gate.sh", "scripts/test/online-hosted-transfer-gate_test.py",
            ".github/workflows/online-t4-gates.yml", "Makefile",
            "scripts/test/transfer-relational-sql-etl-online.py", "scripts/test/transfer-relational-sql-etl-online_test.py",
            "scripts/test/online-transfer-relational-sql-etl-fixture_test.py",
            "business/scripts/online-transfer-relational-sql-etl-fixture.sh",
            "console/frontend/e2e/online/transfer-relational-sql-etl.spec.js",
        )
        for relative in relatives:
            target = self.repository / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / relative, target)
        registered = {"transfer-relational-sql-etl"}
        CHECK.validate_transfer_relational_sql_etl_profile(self.repository, registered)
        for relative, fragment in (
            ("scripts/test/transfer-relational-sql-etl-online.py", '"meta.lineage.read"'),
            ("scripts/test/transfer-relational-sql-etl-online.py", "validate_mongodb_execution"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "addp.transfer-relational-sql-etl-online/v6"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "validate_multi_source_execution"),
            ("business/scripts/online-transfer-relational-sql-etl-fixture.sh", "multi-source CTE/JOIN/UNION rows differ"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "verify_historical_schema"),
            ("business/scripts/online-transfer-relational-sql-etl-fixture.sh", "TYPE timestamp without time zone USING activity_date::timestamp"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "manager_evolved_schema_verified: true"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "addp.transfer-relational-sql-etl-browser/v5"),
            ("business/scripts/online-transfer-relational-sql-etl-fixture.sh", "--tmpfs /data/db --tmpfs /data/configdb"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "manager_mongodb_field_graph_verified: true"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "expect(mongodbGraphRequests).toBe(1)"),
            ("scripts/test/transfer-relational-sql-etl-online.py", "cleanup_tasks(client"),
            ("business/scripts/online-transfer-relational-sql-etl-fixture.sh", "DROP TABLE IF EXISTS public.${NATIVE_DOWNSTREAM}"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "query_field_lineage_verified: true"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "expect(query.graph.field_lineage_status).toBe('complete')"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "expect(query.focused.edges).toHaveLength(1)"),
            ("console/frontend/e2e/online/transfer-relational-sql-etl.spec.js", "schema_snapshot_hash"),
        ):
            with self.subTest(fragment=fragment):
                path = self.repository / relative
                original = path.read_text(encoding="utf-8")
                path.write_text(original.replace(fragment, ""), encoding="utf-8")
                with self.assertRaisesRegex(CHECK.RegistrationError, "contract is missing"):
                    CHECK.validate_transfer_relational_sql_etl_profile(self.repository, registered)
                path.write_text(original, encoding="utf-8")
        for relative, fragment, message in (
            ("scripts/test/online-hosted-transfer-gate.sh", "CONSOLE_URL=", "Hosted profile is missing"),
            ("scripts/test/online-hosted-transfer-gate.sh", "addp-transfer-mongodb-online-disposable", "Hosted profile is missing"),
            ("scripts/test/online-hosted-transfer-gate.sh", "export ADDP_ONLINE_TEST_MONGODB_ENGINE_ID", "Hosted profile is missing"),
            ("scripts/test/online-hosted-transfer-gate.sh", "unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN", "Hosted profile is missing"),
            (".github/workflows/online-t4-gates.yml", "&& inputs.suite != 'transfer-relational-sql-etl'", "must not also dispatch"),
            (".github/workflows/online-t4-gates.yml", "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'transfer-relational-sql-etl'", "manual Ubuntu"),
            ("Makefile", "scripts/test/online-hosted-transfer-gate_test.py", "regression must enter"),
        ):
            with self.subTest(fragment=fragment):
                path = self.repository / relative
                original = path.read_text()
                path.write_text(original.replace(fragment, ""))
                with self.assertRaisesRegex(CHECK.RegistrationError, message):
                    CHECK.validate_transfer_relational_sql_etl_profile(self.repository, registered)
                path.write_text(original)
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(host.read_text() + "\n# transfer-relational-sql-etl\n")
        with self.assertRaisesRegex(CHECK.RegistrationError, "must not keep a self-hosted"):
            CHECK.validate_transfer_relational_sql_etl_profile(self.repository, registered)

    def test_raster_requires_manual_disposable_profile_and_registered_regressions(self) -> None:
        root = SCRIPT.parents[2]
        files = (
            "scripts/test/online-gate.py", "scripts/test/online-hosted-raster-gate.sh",
            "business/scripts/online-raster-minio-fixture.py", "scripts/test/raster-workflow-online.py",
            "console/frontend/e2e/online/raster-workflow.spec.js", "Makefile",
            ".github/workflows/online-t4-gates.yml",
        )
        for relative in files:
            destination = self.repository / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(root / relative, destination)
        registered = {"raster-workflow"}
        CHECK.validate_raster_workflow_profile(self.repository, registered)
        faults = (
            (".github/workflows/online-t4-gates.yml", "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-workflow'", "if: github.event_name == 'schedule'"),
            (".github/workflows/online-t4-gates.yml", "&& inputs.suite != 'raster-workflow'", ""),
            ("scripts/test/online-hosted-raster-gate.sh", "online-raster-minio-fixture.py stop", "true"),
            ("scripts/test/online-hosted-raster-gate.sh", "unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN", "true"),
            ("Makefile", "scripts/test/online-raster-minio-fixture_test.py", ""),
            ("console/frontend/e2e/online/raster-workflow.spec.js", "auth.principalID", "other"),
            ("scripts/test/raster-workflow-online.py", "spatial_workflow", "incomplete_case"),
            ("business/scripts/online-raster-minio-fixture.py", "verify-mosaic-last", "omitted"),
            ("console/frontend/e2e/online/raster-workflow.spec.js", "evidence.target_name", "omitted"),
            ("scripts/test/raster-workflow-online.py", "validate_analysis", "incomplete_analysis"),
            ("business/scripts/online-raster-minio-fixture.py", "verify-analysis", "omitted_analysis"),
            ("console/frontend/e2e/online/raster-workflow.spec.js", ".workflow-final-result-json", ".omitted-preview"),
        )
        for relative, old, new in faults:
            with self.subTest(relative=relative, fragment=old):
                path = self.repository / relative
                original = path.read_text()
                self.assertIn(old, original)
                path.write_text(original.replace(old, new))
                with self.assertRaises(CHECK.RegistrationError):
                    CHECK.validate_raster_workflow_profile(self.repository, registered)
                path.write_text(original)

    def test_requires_manager_internal_artifact_lineage_fixture_and_browser_suite(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8").replace(
                '"first-suite"', '"manager-internal-artifact-lineage"'
            ),
            encoding="utf-8",
        )
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "  first-suite)\n    START_TARGET=-system\n    ;;\n", ""
            ),
            encoding="utf-8",
        )
        hosted = self.repository / "scripts/test/online-hosted-manager-gate.sh"
        hosted.write_text((SCRIPT.parents[2] / "scripts/test/online-hosted-manager-gate.sh").read_text(), encoding="utf-8")
        workflow = (SCRIPT.parents[2] / ".github/workflows/online-t4-gates.yml").read_text()
        import re
        job = re.search(r"(?ms)^  manager-hosted-t4:\n.*?(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow).group()
        self.workflow.write_text(self.workflow.read_text().replace("first-suite", "manager-internal-artifact-lineage").replace(
            "if: github.event_name == 'workflow_dispatch'", "if: github.event_name == 'workflow_dispatch' && inputs.suite != 'manager-internal-artifact-lineage'")
            + job, encoding="utf-8")
        for name in ("_3dtile", "assimp", "IfcConvert", "docker-converter.sh"):
            wrapper = self.repository / "engines/model3d-workflow/scripts/converters" / name
            wrapper.parent.mkdir(parents=True, exist_ok=True)
            wrapper.write_text("tracked converter wrapper\n")
        fixture = self.repository / "business/scripts/online-manager-minio-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        fixture.write_text(
            "ADDP_ONLINE_HOST --env-file /dev/null business-minio "
            "pdal_las12_format0.las addp_online_preview_fixture.pptx MC_HOST_fixture "
            "dae/model.dae 3ds/model.3ds texture.png\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/manager-internal-artifact-lineage-online.py"
        owner.write_text(
            "addp.lineage-facts/v1 /api/v1/meta/scan/run/manual "
            "/api/v1/monitor/executions/by-execution-id/ "
            "addp-infra://minio/manager/tenant_ /api/v1/manager/point_cloud_copc/ "
            "/api/v1/manager/quick-view/capability /api/v1/manager/quick-view/actions "
            "/api/v1/manager/tasks/{PPTX_TASK_TYPE}/ "
            '"cache_reused": True model_3d_glb_generation /api/v1/manager/model_3d_glb/ '
            'validate_model_glb cleanup_model_glb\n',
            encoding="utf-8",
        )
        browser = self.repository / "console/frontend/e2e/online/manager-internal-artifact-lineage.spec.js"
        browser.parent.mkdir(parents=True, exist_ok=True)
        browser.write_text(
            ".execution-lineage__group .execution-lineage__resource-action "
            "平台内部产物|Platform-internal artifact platform_internal_outputs "
            ".pptx-preview .pdf-preview pptx_page_after_engine_refresh pptx_generation_requests "
            ".model-preview model_loaded content_loaded addp.manager-internal-artifact-lineage-browser/v2\n",
            encoding="utf-8",
        )
        config = self.repository / "console/frontend/playwright.online.config.js"
        config.write_text("--use-gl=angle --use-angle=swiftshader-webgl --enable-unsafe-swiftshader\n", encoding="utf-8")
        CHECK.check_registration(self.repository)
        for file, fragment, reason in (
            (hosted, "-model3d-workflow", "Hosted profile"),
            (fixture, "texture.png", "fixture contract"),
            (config, "--enable-unsafe-swiftshader", "software WebGL"),
            (owner, "validate_model_glb", "owner contract"),
            (browser, ".model-preview", "browser contract"),
        ):
            with self.subTest(fragment=fragment):
                original = file.read_text(encoding="utf-8")
                file.write_text(original.replace(fragment, ""), encoding="utf-8")
                with self.assertRaisesRegex(CHECK.RegistrationError, reason):
                    CHECK.check_registration(self.repository)
                file.write_text(original, encoding="utf-8")
        browser.unlink()
        with self.assertRaisesRegex(CHECK.RegistrationError, "requires"):
            CHECK.check_registration(self.repository)

    def test_requires_manager_hybrid_search_fixture_and_owner_suite(self) -> None:
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8").replace(
                '"first-suite"', '"manager-hybrid-search"'
            ),
            encoding="utf-8",
        )
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "first-suite)\n    START_TARGET=-system",
                "manager-hybrid-search)\n    START_TARGET=-all",
            )
            + "\nSYSTEM_URL GATEWAY_URL META_URL MANAGER_URL INFERENCE_URL\n"
            + "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN ADDP_ONLINE_TEST_TENANT_ID\n"
            + "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID ADDP_ONLINE_MANAGER_MINIO_PORT "
            + "ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY "
            + "ADDP_ONLINE_MANAGER_MINIO_BUCKET "
            + "ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT "
            + "ADDP_ONLINE_MANAGER_EMBEDDING_MODEL_PROFILE_ID\n"
            + "bash business/scripts/online-manager-minio-fixture.sh start\n"
            + "bash business/scripts/online-manager-minio-fixture.sh stop\n"
            + 'bash scripts/dev/start.sh "$START_TARGET"\n',
            encoding="utf-8",
        )
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "first-suite", "manager-hybrid-search"
            ),
            encoding="utf-8",
        )
        fixture = self.repository / "business/scripts/online-manager-minio-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        fixture.write_text(
            "ADDP_ONLINE_HOST --env-file /dev/null business-minio Autocop_4X3.jpg "
            "ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT MC_HOST_fixture\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/manager-hybrid-search-online.py"
        owner.write_text(
            "/api/v1/meta/scan/run/manual /api/v1/manager/embedding_executions "
            "/api/v1/manager/embeddings? /api/v1/manager/search? "
            "expected_methods: list[str] "
            '["keyword", "vector"] ["vector"] vector_hits '
            '"residual_resources": -1\n',
            encoding="utf-8",
        )

        CHECK.check_registration(self.repository)
        owner.unlink()
        with self.assertRaisesRegex(CHECK.RegistrationError, "requires"):
            CHECK.check_registration(self.repository)

    def test_requires_oceanbase_consumer_flow_owner_contract(self) -> None:
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8")
            + "\noceanbase-consumer-flow)\n"
            + "SYSTEM_URL GATEWAY_URL META_URL MANAGER_URL TRANSFER_URL DEVELOP_URL SERVICE_URL\n"
            + "ADDP_ONLINE_OCEANBASE_ENGINE_ID ADDP_ONLINE_OCEANBASE_PORT\n"
            + "ADDP_ONLINE_OCEANBASE_DATABASE ADDP_ONLINE_OCEANBASE_USER ADDP_ONLINE_OCEANBASE_PASSWORD\n"
            + "bash business/scripts/online-oceanbase-consumer-fixture.sh start\n"
            + "bash business/scripts/online-oceanbase-consumer-fixture.sh stop\n"
            + 'bash scripts/dev/start.sh "$START_TARGET"\n',
            encoding="utf-8",
        )
        fixture = self.repository / "business/scripts/online-oceanbase-consumer-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        fixture.write_text(
            "ADDP_ONLINE_HOST --env-file /dev/null oceanbase/oceanbase-ce:4.4.2-lts "
            "business-oceanbase addp_online_consumer_source addp_online_consumer_target "
            "start|advance|stop|status reset_fixture\n",
            encoding="utf-8",
        )
        support = self.repository / "scripts/test/security-transfer-protection-online.py"
        support.write_text(
            "/api/v1/meta/scan/run/manual\n/api/v1/manager/preview\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/relational-consumer-flow-online.py"
        owner.write_text(
            '"verification_owner": "deployment_profile"\n'
            '"type": "watermark"\n"start": "committed"\n'
            '"end": "execution_upper_bound"\n"apply_mode": "upsert"\n'
            '"manager.data_item.read"\n'
            "/api/v1/develop/executions\n/api/query/\nadvance_fixture(profile)\n"
            '"empty_resume"\ncleanup_tasks(client, task_ids)\n'
            'cleanup_service(client, service_id)\n"residual_resources": 0\n',
            encoding="utf-8",
        )
        for relative in (
            "scripts/test/online-oceanbase-consumer-fixture_test.py",
            "scripts/test/relational-consumer-flow-online_test.py",
        ):
            (self.repository / relative).write_text("fixture\n", encoding="utf-8")

        CHECK.validate_oceanbase_consumer_flow_profile(
            self.repository, {"oceanbase-consumer-flow"}
        )
        support_text = support.read_text(encoding="utf-8")
        support.write_text(
            support_text.replace("/api/v1/manager/preview", ""),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "manager/preview"):
            CHECK.validate_oceanbase_consumer_flow_profile(
                self.repository, {"oceanbase-consumer-flow"}
            )
        support.write_text(support_text, encoding="utf-8")
        owner.write_text(
            owner.read_text(encoding="utf-8").replace("advance_fixture(profile)", ""),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "advance_fixture"):
            CHECK.validate_oceanbase_consumer_flow_profile(
                self.repository, {"oceanbase-consumer-flow"}
            )

    def test_requires_tidb_consumer_flow_owner_and_digest_contract(self) -> None:
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8")
            + "\ntidb-consumer-flow)\n"
            + "SYSTEM_URL GATEWAY_URL META_URL MANAGER_URL TRANSFER_URL DEVELOP_URL SERVICE_URL\n"
            + "ADDP_ONLINE_TIDB_ENGINE_ID ADDP_ONLINE_TIDB_PORT\n"
            + "ADDP_ONLINE_TIDB_DATABASE ADDP_ONLINE_TIDB_USER\n"
            + "bash business/scripts/online-tidb-consumer-fixture.sh start\n"
            + "bash business/scripts/online-tidb-consumer-fixture.sh stop\n"
            + 'bash scripts/dev/start.sh "$START_TARGET"\n',
            encoding="utf-8",
        )
        fixture = self.repository / "business/scripts/online-tidb-consumer-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        fixture.write_text(
            "ADDP_ONLINE_HOST docker-compose.tidb-t2.yml addp-online-tidb-consumer "
            "addp_online_consumer_source addp_online_consumer_target "
            "start|advance|stop|status reset_fixture "
            "down --volumes --remove-orphans assert_zero_residue\n",
            encoding="utf-8",
        )
        compose = self.repository / "scripts/test/docker-compose.tidb-t2.yml"
        compose.write_text(
            "services:\n"
            "  tidb-pd:\n"
            "    image: pingcap/pd:v8.5.8@sha256:" + "a" * 64 + "\n"
            "  tidb-tikv:\n"
            "    image: pingcap/tikv:v8.5.8@sha256:" + "b" * 64 + "\n"
            "    ulimits:\n"
            "      nofile:\n"
            "        soft: 1000000\n"
            "        hard: 1000000\n"
            "  tidb:\n"
            "    image: pingcap/tidb:v8.5.8@sha256:" + "c" * 64 + "\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/relational-consumer-flow-online.py"
        owner.write_text(
            '"tidb": ConsumerProfile(\n'
            'namespace_kind="database"\nitem_code_prefix="TIDB"\n'
            'identifier_quote="`"\n'
            'fixture_script="business/scripts/online-tidb-consumer-fixture.sh"\n'
            '"verification_owner": "deployment_profile"\n'
            '"schema_version": "addp.relational-consumer-flow-online/v1"\n'
            '"residual_resources": 0\n',
            encoding="utf-8",
        )
        for relative in (
            "scripts/test/online-tidb-consumer-fixture_test.py",
            "scripts/test/relational-consumer-flow-online_test.py",
        ):
            (self.repository / relative).write_text("fixture\n", encoding="utf-8")

        CHECK.validate_tidb_consumer_flow_profile(
            self.repository, {"tidb-consumer-flow"}
        )
        compose_text = compose.read_text(encoding="utf-8")
        compose.write_text(
            compose_text.replace("hard: 1000000", "hard: 65536"),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "nofile"):
            CHECK.validate_tidb_consumer_flow_profile(
                self.repository, {"tidb-consumer-flow"}
            )
        compose.write_text(compose_text, encoding="utf-8")
        compose.write_text(
            compose.read_text(encoding="utf-8").replace(
                "pingcap/tidb:v8.5.8@sha256:" + "c" * 64,
                "pingcap/tidb:v8.5.8",
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "pin pingcap/tidb"):
            CHECK.validate_tidb_consumer_flow_profile(
                self.repository, {"tidb-consumer-flow"}
            )

    def test_requires_opengauss_hosted_owner_and_identity_contracts(self) -> None:
        shared = self.repository / "scripts/utils/hosted-online.sh"
        shared.parent.mkdir(parents=True, exist_ok=True)
        shared.write_text("# shared test lifecycle\n", encoding="utf-8")
        hosted = self.repository / "scripts/test/online-hosted-opengauss-gate.sh"
        hosted.write_text(
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"\n'
            "# ADDP_ONLINE_SUITES=opengauss-consumer-flow\n"
            "# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64\n"
            "GITHUB_ACTIONS RUNNER_OS Linux x86_64 POSTGRES_DB=addp_online\n"
            "ADDP_ONLINE_SECRET_DIR go run ./cmd/online-test-fixture\n"
            "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN\n"
            "opengauss-official-media.sh refusing to reuse existing image\n"
            "bash business/scripts/online-opengauss-consumer-fixture.sh start\n"
            "python3 scripts/test/online-engine-registration.py\n"
            '--descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"\n'
            "for start_target in -manager -develop -service\n"
            "unset ADDP_TEST_OPENGAUSS_DSN\n"
            'make test-online "ONLINE_SUITE=$ONLINE_SUITE"\n'
            "bash scripts/dev/stop.sh\n"
            "bash scripts/infra/down.sh --volumes --force\n",
            encoding="utf-8",
        )
        fixture = self.repository / "business/scripts/online-opengauss-consumer-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        fixture.write_text(
            "opengauss_ensure_official_image x86_64 refusing to reuse existing image addp-opengauss-online-disposable "
            "addp_opengauss_online addp_online_consumer_source addp_online_consumer_target "
            "MERGE INTO container_exists start|advance|stop|status ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE "
            '"engine_type": "opengauss"\n',
            encoding="utf-8",
        )
        postgres_dockerfile = self.repository / "scripts/infra/Dockerfile.postgres"
        postgres_dockerfile.parent.mkdir(parents=True, exist_ok=True)
        postgres_dockerfile.write_text("FROM postgres:15\n", encoding="utf-8")
        (self.repository / "scripts/infra/up.sh").write_text(
            "BUILD_REPOSITORY_POSTGRES_IMAGE=true\n"
            "docker build --file scripts/infra/Dockerfile.postgres \\\n"
            '  --tag "$POSTGRES_IMAGE" scripts/infra\n',
            encoding="utf-8",
        )
        registration = self.repository / "scripts/test/online-engine-registration.py"
        registration.write_text(
            '/api/v1/system/engines\ntest_result.get("success") is not True\n'
            "ADDP_ONLINE_CONSUMER_ENGINE_ID\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/relational-consumer-flow-online.py"
        owner.write_text(
            '"opengauss": ConsumerProfile(\nnamespace_kind="schema"\n'
            'identifier_quote=\'"\'\n'
            'fixture_script="business/scripts/online-opengauss-consumer-fixture.sh"\n'
            '"verification_owner": "deployment_profile"\n'
            '"schema_version": "addp.relational-consumer-flow-online/v1"\n'
            '"residual_resources": 0\n',
            encoding="utf-8",
        )
        for relative in (
            "scripts/test/online-hosted-opengauss-gate_test.py",
            "scripts/test/online-engine-registration_test.py",
            "scripts/test/online-opengauss-consumer-fixture_test.py",
            "scripts/test/relational-consumer-flow-online_test.py",
            "system/backend/cmd/online-test-fixture/main_test.go",
        ):
            path = self.repository / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("fixture\n", encoding="utf-8")
        identity = self.repository / "system/backend/cmd/online-test-fixture/main.go"
        identity.write_text(
            'engineProvisionerRoleKey = "tenant.infrastructure_administrator"\n'
            'repository.GetActiveBuiltinRoleByKey(ctx, engineProvisionerRoleKey)\n'
            'RoleKey: "online." + strings.ReplaceAll(*suite, "-", "_")\n'
            'permissions, err := suitePermissions(*suite)\n'
            'PermissionKeys: permissions\n'
            '"ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN"\n',
            encoding="utf-8",
        )

        CHECK.validate_opengauss_consumer_flow_profile(
            self.repository, {"opengauss-consumer-flow"}
        )

        owner_text = owner.read_text(encoding="utf-8")
        owner.write_text(owner_text + '"system.engine.read"\n', encoding="utf-8")
        with self.assertRaisesRegex(CHECK.RegistrationError, "consumer must not access"):
            CHECK.validate_opengauss_consumer_flow_profile(
                self.repository, {"opengauss-consumer-flow"}
            )
        owner.write_text(owner_text, encoding="utf-8")

        identity_text = identity.read_text(encoding="utf-8")
        identity.write_text(
            identity_text + '"system.engine.read"\n', encoding="utf-8"
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "consumer role must not"):
            CHECK.validate_opengauss_consumer_flow_profile(
                self.repository, {"opengauss-consumer-flow"}
            )
        identity.write_text(identity_text, encoding="utf-8")

        infra_up = self.repository / "scripts/infra/up.sh"
        infra_up_text = infra_up.read_text(encoding="utf-8")
        infra_up.write_text(
            infra_up_text.replace(
                "docker build --file scripts/infra/Dockerfile.postgres", "docker build"
            ),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "Infra lifecycle"):
            CHECK.validate_opengauss_consumer_flow_profile(
                self.repository, {"opengauss-consumer-flow"}
            )
        infra_up.write_text(infra_up_text, encoding="utf-8")

        fixture.write_text(
            fixture.read_text(encoding="utf-8").replace("MERGE INTO", ""),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "MERGE INTO"):
            CHECK.validate_opengauss_consumer_flow_profile(
                self.repository, {"opengauss-consumer-flow"}
            )


if __name__ == "__main__":
    unittest.main()
