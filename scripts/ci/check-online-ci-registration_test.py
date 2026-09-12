import importlib.util
import re
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
            jobs:
              online:
                runs-on:
                  - self-hosted
                  - macOS
                  - addp-online
                environment: addp-online
                steps:
                  - env:
                      ADDP_ONLINE_ARTIFACT_DIR: ${{ runner.temp }}/addp-online-${{ github.run_id }}
                    run: bash scripts/test/online-host-gate.sh --check-only
                  - env:
                      ADDP_ONLINE_ARTIFACT_DIR: ${{ runner.temp }}/addp-online-${{ github.run_id }}
                    run: bash scripts/test/online-host-gate.sh
                  - uses: actions/upload-artifact@pinned
            """
        )

    def test_accepts_one_profile_and_workflow_choice_per_registered_suite(self) -> None:
        CHECK.check_registration(self.repository)

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

    def test_repository_schedules_only_graduated_opengauss_profile(self) -> None:
        repository = SCRIPT.parents[2]

        registry = CHECK.load_suite_registry(repository)
        nightly = CHECK.load_nightly_suites(registry)

        self.assertEqual(nightly, {"opengauss-consumer-flow"})
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
        gate = self.repository / "scripts/test/online-gate.py"
        gate.write_text(
            gate.read_text(encoding="utf-8").replace(
                '"first-suite"', '"security-mysql-owner-protection"'
            ),
            encoding="utf-8",
        )
        host = self.repository / "scripts/test/online-host-gate.sh"
        host.write_text(
            host.read_text(encoding="utf-8").replace(
                "first-suite)\n    START_TARGET=-system",
                "security-mysql-owner-protection)\n    START_TARGET=-all",
            )
            + "\nSYSTEM_URL GATEWAY_URL META_URL SECURITY_URL MANAGER_URL DEVELOP_URL\n"
            + "SERVICE_URL TRANSFER_URL ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID\n"
            + "bash business/scripts/online-engine-fixture.sh start\n"
            + "bash business/scripts/online-engine-fixture.sh stop\n"
            + "bash business/scripts/online-workbench-mysql-fixture.sh start\n"
            + "bash business/scripts/online-workbench-mysql-fixture.sh stop\n"
            + 'bash scripts/dev/start.sh "$START_TARGET"\n',
            encoding="utf-8",
        )
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "first-suite", "security-mysql-owner-protection"
            ),
            encoding="utf-8",
        )
        postgres_fixture = self.repository / "business/scripts/online-engine-fixture.sh"
        postgres_fixture.parent.mkdir(parents=True, exist_ok=True)
        postgres_fixture.write_text(
            "addp_online_security.mysql_email_transfer "
            "DROP TABLE IF EXISTS addp_online_security.mysql_email_transfer\n",
            encoding="utf-8",
        )
        mysql_fixture = (
            self.repository / "business/scripts/online-workbench-mysql-fixture.sh"
        )
        mysql_fixture.write_text("fixture\n", encoding="utf-8")
        owner = (
            self.repository
            / "scripts/test/security-mysql-owner-protection-online.py"
        )
        owner.write_text(
            "/api/v1/meta/scan/run/manual "
            "/api/v1/security/sensitive-data-types "
            "security.sensitive_data_type.create security.sensitive_data_type.delete "
            '"default_protection" "invalid_request_status" '
            '"rollback_verified" "cleanup_verified" '
            "addp.detector.email_metadata/v1 "
            "/api/v1/security/protection-baselines "
            "/api/v1/security/protection-enrollments "
            "/api/v1/develop/executions /api/query/ "
            '"effect": "suppress" "residual_resources": 0\n',
            encoding="utf-8",
        )
        support = (
            self.repository / "scripts/test/security-transfer-protection-online.py"
        )
        support.write_text("/api/v1/transfer/task-definitions\n", encoding="utf-8")

        CHECK.check_registration(self.repository)
        owner.unlink()
        with self.assertRaisesRegex(CHECK.RegistrationError, "requires"):
            CHECK.check_registration(self.repository)

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
                "first-suite)\n    START_TARGET=-system",
                "manager-internal-artifact-lineage)\n    START_TARGET=-all",
            )
            + "\nSYSTEM_URL GATEWAY_URL META_URL MANAGER_URL MONITOR_URL CONSOLE_URL\n"
            + "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID ADDP_ONLINE_MANAGER_MINIO_PORT "
            + "ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY "
            + "ADDP_ONLINE_MANAGER_MINIO_BUCKET ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT "
            + "ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT\n"
            + "bash business/scripts/online-manager-minio-fixture.sh start\n"
            + "bash business/scripts/online-manager-minio-fixture.sh stop\n"
            + 'bash scripts/dev/start.sh "$START_TARGET"\n'
            + "playwright install chromium\n",
            encoding="utf-8",
        )
        self.workflow.write_text(
            self.workflow.read_text(encoding="utf-8").replace(
                "first-suite", "manager-internal-artifact-lineage"
            ),
            encoding="utf-8",
        )
        fixture = self.repository / "business/scripts/online-manager-minio-fixture.sh"
        fixture.parent.mkdir(parents=True, exist_ok=True)
        fixture.write_text(
            "ADDP_ONLINE_HOST --env-file /dev/null business-minio "
            "pdal_las12_format0.las addp_online_preview_fixture.pptx MC_HOST_fixture\n",
            encoding="utf-8",
        )
        owner = self.repository / "scripts/test/manager-internal-artifact-lineage-online.py"
        owner.write_text(
            "addp.lineage-facts/v1 /api/v1/meta/scan/run/manual "
            "/api/v1/monitor/executions/by-execution-id/ "
            "addp-infra://minio/manager/tenant_ /api/v1/manager/point_cloud_copc/ "
            "/api/v1/manager/quick-view/capability /api/v1/manager/quick-view/actions "
            "/api/v1/manager/tasks/{PPTX_TASK_TYPE}/ "
            '"cache_reused": True\n',
            encoding="utf-8",
        )
        browser = self.repository / "console/frontend/e2e/online/manager-internal-artifact-lineage.spec.js"
        browser.parent.mkdir(parents=True, exist_ok=True)
        browser.write_text(
            ".execution-lineage__group .execution-lineage__resource-action "
            "平台内部产物|Platform-internal artifact platform_internal_outputs "
            ".pptx-preview .pdf-preview pptx_page_after_engine_refresh pptx_generation_requests\n",
            encoding="utf-8",
        )
        config = self.repository / "console/frontend/playwright.online.config.js"
        config.write_text("fixture\n", encoding="utf-8")
        start_script = self.repository / "scripts/dev/start.sh"
        start_script.parent.mkdir(parents=True, exist_ok=True)
        start_script.write_text(
            "case $1 in -all) START_POINTCLOUD_WORKFLOW=true START_DOCUMENT_WORKFLOW=true ;; esac\n",
            encoding="utf-8",
        )

        CHECK.check_registration(self.repository)
        browser.unlink()
        with self.assertRaisesRegex(CHECK.RegistrationError, "requires"):
            CHECK.check_registration(self.repository)

        browser.write_text(
            ".execution-lineage__group .execution-lineage__resource-action "
            "平台内部产物|Platform-internal artifact platform_internal_outputs "
            ".pptx-preview .pdf-preview pptx_page_after_engine_refresh pptx_preview_requests\n",
            encoding="utf-8",
        )
        start_script.write_text(
            "case $1 in -all) START_POINTCLOUD_WORKFLOW=false ;; esac\n",
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECK.RegistrationError, "full start contract"):
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
        hosted = self.repository / "scripts/test/online-hosted-opengauss-gate.sh"
        hosted.write_text(
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
            'RoleKey: "online.relational_consumer"\n'
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
