#!/usr/bin/env python3
"""Keep registered Online suites, deployment profiles, and T4 workflow aligned."""

from __future__ import annotations

import argparse
import importlib.util
import re
import sys
from pathlib import Path


class RegistrationError(RuntimeError):
    pass


def load_registered_suites(repository: Path) -> set[str]:
    path = repository / "scripts/test/online-gate.py"
    spec = importlib.util.spec_from_file_location("addp_online_gate_registration", path)
    if spec is None or spec.loader is None:
        raise RegistrationError(f"cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return set(module.SUITES)


def load_deployment_profiles(repository: Path) -> dict[str, str]:
    path = repository / "scripts/test/online-host-gate.sh"
    if not path.is_file():
        raise RegistrationError(f"{path.relative_to(repository)} is missing")
    text = path.read_text(encoding="utf-8")
    required_fragments = (
        "python3 scripts/test/online-preflight.py --environment-only",
        'run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        "printf 'database=%s\\n' \"$POSTGRES_DB\"",
    )
    missing = [fragment for fragment in required_fragments if fragment not in text]
    if missing:
        raise RegistrationError(
            "Online host gate is missing: " + ", ".join(missing)
        )
    matches = re.findall(
        r"(?m)^  ([a-z][a-z0-9-]*)\)\n    START_TARGET=(-[a-z][a-z0-9-]*)$",
        text,
    )
    profiles = dict(matches)
    if len(profiles) != len(matches):
        raise RegistrationError("online deployment profiles contain duplicate suites")
    for hosted_path in sorted(
        (repository / "scripts/test").glob("online-hosted-*-gate.sh")
    ):
        hosted_text = hosted_path.read_text(encoding="utf-8")
        hosted_suite = re.search(
            r"(?m)^# ADDP_ONLINE_SUITES=([a-z][a-z0-9-]*)$", hosted_text
        )
        hosted_runner = re.search(
            r"(?m)^# ADDP_ONLINE_RUNNER=([a-z][a-z0-9_-]*)$", hosted_text
        )
        if hosted_suite is None or hosted_runner is None:
            raise RegistrationError(
                f"{hosted_path.relative_to(repository)} is missing Hosted Online metadata"
            )
        suite = hosted_suite.group(1)
        if suite in profiles:
            raise RegistrationError(f"Online suite {suite} has multiple deployment profiles")
        profiles[suite] = hosted_runner.group(1)
    return profiles


def validate_module_registry_process_profile(repository: Path, registered: set[str]) -> None:
    if "module-registry-recovery" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "bash scripts/dev/start.sh --exact-process --wait-live -manager",
        "observe_module_lifecycle business-before-system",
        "bash scripts/dev/start.sh --exact-process -system",
        "observe_module_lifecycle manager-registered",
        "bash scripts/dev/start.sh --exact-process -gateway",
        "observe_module_lifecycle gateway-established",
        "bash scripts/dev/stop-exact-process.sh -system",
        "observe_module_lifecycle system-interrupted",
        "observe_module_lifecycle system-recovered",
        "bash scripts/dev/stop-exact-process.sh -manager",
    )
    missing = [fragment for fragment in required_fragments if fragment not in host_gate]
    if missing:
        raise RegistrationError(
            "module-registry-recovery process profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "scripts/dev/stop-exact-process.sh",
        "scripts/test/module-lifecycle-process-online.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"module-registry-recovery process profile requires {relative}")
    start_script = (repository / "scripts/dev/start.sh").read_text(encoding="utf-8")
    for fragment in ("--exact-process", "ADDP_ONLINE_HOST", "--wait-live"):
        if fragment not in start_script:
            raise RegistrationError(
                f"module-registry-recovery exact start contract is missing {fragment}"
            )


def validate_consumer_engine_recovery_profile(repository: Path, registered: set[str]) -> None:
    if "consumer-engine-recovery" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "bash business/scripts/online-engine-fixture.sh start",
        "bash business/scripts/online-engine-fixture.sh stop",
        "bash scripts/dev/start.sh",
        "playwright install chromium",
        "consumer-process-stability-online.py",
        "consumer-engine-recovery-online.py --restore-only",
    )
    missing = [fragment for fragment in required_fragments if fragment not in host_gate]
    if missing:
        raise RegistrationError(
            "consumer-engine-recovery process profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-engine-fixture.sh",
        "scripts/test/consumer-engine-recovery-online.py",
        "scripts/test/consumer-process-stability-online.py",
        "console/frontend/playwright.online.config.js",
        "console/frontend/e2e/online/consumer-engine-recovery.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"consumer-engine-recovery requires {relative}")
    fixture = (repository / "business/scripts/online-engine-fixture.sh").read_text(encoding="utf-8")
    for fragment in ("ADDP_ONLINE_HOST", "--env-file /dev/null", "business-postgres"):
        if fragment not in fixture:
            raise RegistrationError(
                f"consumer-engine-recovery fixture contract is missing {fragment}"
            )


def validate_enterprise_catalog_publishing_profile(repository: Path, registered: set[str]) -> None:
    if "enterprise-catalog-publishing" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "enterprise-catalog-publishing)",
        "START_TARGET=-all",
        "META_URL CATALOG_URL ASSET_URL PORTAL_URL CONSOLE_URL",
        "ADDP_ONLINE_TEST_USER_USERNAME",
        "ADDP_ONLINE_TEST_USER_PASSWORD",
        "ADDP_ONLINE_TEST_CATALOG_DOMAIN_ID",
        "ADDP_ONLINE_TEST_CATALOG_DEPARTMENT_ID",
        "bash business/scripts/online-engine-fixture.sh start",
        'bash scripts/dev/start.sh "$START_TARGET"',
        "playwright install chromium",
    )
    missing = [fragment for fragment in required_fragments if fragment not in host_gate]
    if missing:
        raise RegistrationError(
            "enterprise-catalog-publishing profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-engine-fixture.sh",
        "scripts/test/enterprise-catalog-publishing-online.py",
        "console/frontend/playwright.online.config.js",
        "console/frontend/e2e/online/enterprise-catalog-publishing.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"enterprise-catalog-publishing requires {relative}")
    browser_spec = (
        repository / "console/frontend/e2e/online/enterprise-catalog-publishing.spec.js"
    ).read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_ASSET_CATEGORY_ID",
        "ADDP_ONLINE_ASSET_ID",
        "/portal/categories/",
        "portal_category_assets",
        "data-governance-status",
        "catalog-certify-action",
        "catalog-deprecate-action",
        "catalog-withdraw-curation-action",
        "catalog_lifecycle_actions",
    ):
        if fragment not in browser_spec:
            raise RegistrationError(
                f"enterprise-catalog-publishing browser contract is missing {fragment}"
            )
    fixture = (repository / "business/scripts/online-engine-fixture.sh").read_text(encoding="utf-8")
    for fragment in ("addp_online_catalog_fixture", "CREATE TABLE IF NOT EXISTS", "ON CONFLICT"):
        if fragment not in fixture:
            raise RegistrationError(
                f"enterprise-catalog-publishing fixture contract is missing {fragment}"
            )


def validate_workbench_service_consumption_profile(repository: Path, registered: set[str]) -> None:
    if "workbench-service-consumption" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "workbench-service-consumption)",
        "START_TARGET=-all",
        "SYSTEM_URL GATEWAY_URL SERVICE_URL WORKBENCH_URL CONSOLE_URL",
        "ADDP_ONLINE_TEST_USER_USERNAME",
        "ADDP_ONLINE_TEST_USER_PASSWORD",
        "ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID",
        "bash business/scripts/online-workbench-mysql-fixture.sh start",
        "bash business/scripts/online-workbench-mysql-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
        "playwright install chromium",
    )
    missing = [fragment for fragment in required_fragments if fragment not in host_gate]
    if missing:
        raise RegistrationError(
            "workbench-service-consumption profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-workbench-mysql-fixture.sh",
        "scripts/test/workbench-service-consumption-online.py",
        "console/frontend/playwright.online.config.js",
        "console/frontend/e2e/online/workbench-service-consumption.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"workbench-service-consumption requires {relative}")
    fixture = (repository / "business/scripts/online-workbench-mysql-fixture.sh").read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "business-mysql",
        "REVOKE ALL PRIVILEGES, GRANT OPTION",
        "GRANT SELECT ON",
    ):
        if fragment not in fixture:
            raise RegistrationError(
                f"workbench-service-consumption fixture contract is missing {fragment}"
            )


def validate_manager_internal_artifact_lineage_profile(repository: Path, registered: set[str]) -> None:
    if "manager-internal-artifact-lineage" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "manager-internal-artifact-lineage)",
        "START_TARGET=-all",
        "SYSTEM_URL GATEWAY_URL META_URL MANAGER_URL MONITOR_URL CONSOLE_URL",
        "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID",
        "ADDP_ONLINE_MANAGER_MINIO_PORT",
        "ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY",
        "ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY",
        "ADDP_ONLINE_MANAGER_MINIO_BUCKET",
        "ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT",
        "ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT",
        "bash business/scripts/online-manager-minio-fixture.sh start",
        "bash business/scripts/online-manager-minio-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
        "playwright install chromium",
    )
    missing = [fragment for fragment in required_fragments if fragment not in host_gate]
    if missing:
        raise RegistrationError(
            "manager-internal-artifact-lineage profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-manager-minio-fixture.sh",
        "scripts/test/manager-internal-artifact-lineage-online.py",
        "console/frontend/playwright.online.config.js",
        "console/frontend/e2e/online/manager-internal-artifact-lineage.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"manager-internal-artifact-lineage requires {relative}")
    start_script_path = repository / "scripts/dev/start.sh"
    if not start_script_path.is_file():
        raise RegistrationError(
            "manager-internal-artifact-lineage requires scripts/dev/start.sh"
        )
    start_script = start_script_path.read_text(encoding="utf-8")
    for fragment in ("-all)", "START_POINTCLOUD_WORKFLOW=true", "START_DOCUMENT_WORKFLOW=true"):
        if fragment not in start_script:
            raise RegistrationError(
                f"manager-internal-artifact-lineage full start contract is missing {fragment}"
            )
    fixture = (repository / "business/scripts/online-manager-minio-fixture.sh").read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "business-minio",
        "pdal_las12_format0.las",
        "addp_online_preview_fixture.pptx",
        "MC_HOST_fixture",
    ):
        if fragment not in fixture:
            raise RegistrationError(
                f"manager-internal-artifact-lineage fixture contract is missing {fragment}"
            )
    owner = (
        repository / "scripts/test/manager-internal-artifact-lineage-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        "addp.lineage-facts/v1",
        "/api/v1/meta/scan/run/manual",
        "/api/v1/monitor/executions/by-execution-id/",
        "addp-infra://minio/manager/tenant_",
        "/api/v1/manager/point_cloud_copc/",
        "/api/v1/manager/quick-view/capability",
        "/api/v1/manager/quick-view/actions",
        "/api/v1/manager/tasks/{PPTX_TASK_TYPE}/",
        '"cache_reused": True',
    ):
        if fragment not in owner:
            raise RegistrationError(
                f"manager-internal-artifact-lineage owner contract is missing {fragment}"
            )
    browser = (
        repository
        / "console/frontend/e2e/online/manager-internal-artifact-lineage.spec.js"
    ).read_text(encoding="utf-8")
    for fragment in (
        ".execution-lineage__group",
        ".execution-lineage__resource-action",
        "平台内部产物|Platform-internal artifact",
        "platform_internal_outputs",
        ".pptx-preview .pdf-preview",
        "pptx_page_after_engine_refresh",
        "pptx_generation_requests",
    ):
        if fragment not in browser:
            raise RegistrationError(
                f"manager-internal-artifact-lineage browser contract is missing {fragment}"
            )


def validate_security_transfer_protection_profile(repository: Path, registered: set[str]) -> None:
    if "security-transfer-protection" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "security-transfer-protection)",
        "START_TARGET=-all",
        "SYSTEM_URL GATEWAY_URL META_URL SECURITY_URL TRANSFER_URL MANAGER_URL",
        "ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID",
        "ADDP_ONLINE_SECURITY_MONGODB_ROOT_PASSWORD",
        "bash business/scripts/online-security-transfer-fixture.sh start",
        "bash business/scripts/online-security-transfer-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
    )
    missing = [fragment for fragment in required_fragments if fragment not in host_gate]
    if missing:
        raise RegistrationError(
            "security-transfer-protection profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-security-transfer-fixture.sh",
        "scripts/test/security-transfer-protection-online.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"security-transfer-protection requires {relative}")
    fixture = (repository / "business/scripts/online-security-transfer-fixture.sh").read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "business-mongodb",
        "online-engine-fixture.sh",
        'role: "read"',
        "addp_online_security.transfer_masked",
    ):
        if fragment not in fixture:
            raise RegistrationError(
                f"security-transfer-protection fixture contract is missing {fragment}"
            )
    owner = (repository / "scripts/test/security-transfer-protection-online.py").read_text(encoding="utf-8")
    for fragment in (
        "/api/v1/meta/scan/run/manual",
        "/api/v1/security/protection-enrollments",
        '("export", "mask")',
        "/api/v1/transfer/task-definitions",
        "/api/v1/manager/preview",
        "cleanup_tasks(client, tasks)",
        '"residual_resources": 0',
    ):
        if fragment not in owner:
            raise RegistrationError(
                f"security-transfer-protection owner contract is missing {fragment}"
            )


def validate_security_plaintext_access_profile(repository: Path, registered: set[str]) -> None:
    if "security-plaintext-access" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "security-plaintext-access)",
        "SYSTEM_URL GATEWAY_URL META_URL SECURITY_URL MANAGER_URL",
        "ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN",
        "bash business/scripts/online-security-transfer-fixture.sh start",
        "bash business/scripts/online-security-transfer-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
    )
    missing = [fragment for fragment in required_fragments if fragment not in host_gate]
    if missing:
        raise RegistrationError(
            "security-plaintext-access profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-security-transfer-fixture.sh",
        "scripts/test/security-plaintext-access-online.py",
        "scripts/test/security-transfer-protection-online.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"security-plaintext-access requires {relative}")
    fixture = (repository / "business/scripts/online-security-transfer-fixture.sh").read_text(encoding="utf-8")
    for fragment in (
        "addp_online_security.exemption_source",
        "addp_online_security.exemption_transfer",
        "13812345678",
    ):
        if fragment not in fixture:
            raise RegistrationError(
                f"security-plaintext-access fixture contract is missing {fragment}"
            )
    owner = (repository / "scripts/test/security-plaintext-access-online.py").read_text(encoding="utf-8")
    for fragment in (
        "/api/v1/security/protection-access-request-targets",
        "/api/v1/security/protection-access-requests",
        "/api/v1/security/protection-access-requests/review-queue",
        "/api/v1/security/protection-exemptions",
        "preview_rows = SUPPORT.preview_rows",
        "applicant and approver must be two different Users",
        "protected_other_subject",
        "expired_without_security_refresh",
        '"residual_resources": 0',
    ):
        if fragment not in owner:
            raise RegistrationError(
                f"security-plaintext-access owner contract is missing {fragment}"
            )
    support = (repository / "scripts/test/security-transfer-protection-online.py").read_text(
        encoding="utf-8"
    )
    if "/api/v1/manager/preview" not in support:
        raise RegistrationError(
            "security-plaintext-access preview support is missing /api/v1/manager/preview"
        )


def validate_security_mysql_owner_protection_profile(
    repository: Path, registered: set[str]
) -> None:
    if "security-mysql-owner-protection" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(
        encoding="utf-8"
    )
    required_fragments = (
        "security-mysql-owner-protection)",
        "SYSTEM_URL GATEWAY_URL META_URL SECURITY_URL MANAGER_URL DEVELOP_URL",
        "SERVICE_URL TRANSFER_URL",
        "ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID",
        "bash business/scripts/online-engine-fixture.sh start",
        "bash business/scripts/online-engine-fixture.sh stop",
        "bash business/scripts/online-workbench-mysql-fixture.sh start",
        "bash business/scripts/online-workbench-mysql-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
    )
    missing = [fragment for fragment in required_fragments if fragment not in host_gate]
    if missing:
        raise RegistrationError(
            "security-mysql-owner-protection profile is missing: "
            + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-engine-fixture.sh",
        "business/scripts/online-workbench-mysql-fixture.sh",
        "scripts/test/security-mysql-owner-protection-online.py",
        "scripts/test/security-transfer-protection-online.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(
                f"security-mysql-owner-protection requires {relative}"
            )
    postgres_fixture = (
        repository / "business/scripts/online-engine-fixture.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "addp_online_security.mysql_email_transfer",
        "DROP TABLE IF EXISTS addp_online_security.mysql_email_transfer",
    ):
        if fragment not in postgres_fixture:
            raise RegistrationError(
                "security-mysql-owner-protection PostgreSQL fixture is missing "
                + fragment
            )
    owner = (
        repository / "scripts/test/security-mysql-owner-protection-online.py"
    ).read_text(encoding="utf-8")
    owner_contract = owner + (
        repository / "scripts/test/security-transfer-protection-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        "/api/v1/meta/scan/run/manual",
        "/api/v1/security/sensitive-data-types",
        "addp.detector.email_metadata/v1",
        "/api/v1/security/protection-baselines",
        "/api/v1/security/protection-enrollments",
        "/api/v1/develop/executions",
        "/api/query/",
        "/api/v1/transfer/task-definitions",
        '"effect": "suppress"',
        '"residual_resources": 0',
    ):
        if fragment not in owner_contract:
            raise RegistrationError(
                "security-mysql-owner-protection owner contract is missing "
                + fragment
            )


def validate_oceanbase_consumer_flow_profile(
    repository: Path, registered: set[str]
) -> None:
    if "oceanbase-consumer-flow" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(
        encoding="utf-8"
    )
    required_fragments = (
        "oceanbase-consumer-flow)",
        "SYSTEM_URL GATEWAY_URL META_URL MANAGER_URL TRANSFER_URL DEVELOP_URL SERVICE_URL",
        "ADDP_ONLINE_OCEANBASE_ENGINE_ID",
        "ADDP_ONLINE_OCEANBASE_PORT",
        "ADDP_ONLINE_OCEANBASE_DATABASE",
        "ADDP_ONLINE_OCEANBASE_USER",
        "ADDP_ONLINE_OCEANBASE_PASSWORD",
        "bash business/scripts/online-oceanbase-consumer-fixture.sh start",
        "bash business/scripts/online-oceanbase-consumer-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
    )
    missing = [fragment for fragment in required_fragments if fragment not in host_gate]
    if missing:
        raise RegistrationError(
            "oceanbase-consumer-flow profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-oceanbase-consumer-fixture.sh",
        "scripts/test/relational-consumer-flow-online.py",
        "scripts/test/online-oceanbase-consumer-fixture_test.py",
        "scripts/test/relational-consumer-flow-online_test.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"oceanbase-consumer-flow requires {relative}")
    fixture = (
        repository / "business/scripts/online-oceanbase-consumer-fixture.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "oceanbase/oceanbase-ce:4.4.2-lts",
        "business-oceanbase",
        "addp_online_consumer_source",
        "addp_online_consumer_target",
        "start|advance|stop|status",
        "reset_fixture",
    ):
        if fragment not in fixture:
            raise RegistrationError(
                f"oceanbase-consumer-flow fixture contract is missing {fragment}"
            )
    owner = (
        repository / "scripts/test/relational-consumer-flow-online.py"
    ).read_text(encoding="utf-8")
    owner_contract = owner + (
        repository / "scripts/test/security-transfer-protection-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        'engine.get("engine_type") != profile.engine_type',
        "/api/v1/meta/scan/run/manual",
        '"type": "watermark"',
        '"start": "committed"',
        '"end": "execution_upper_bound"',
        '"apply_mode": "upsert"',
        '"manager.data_item.read"',
        "/api/v1/manager/preview",
        "/api/v1/develop/executions",
        "/api/query/",
        "advance_fixture(profile)",
        '"empty_resume"',
        "cleanup_tasks(client, task_ids)",
        "cleanup_service(client, service_id)",
        '"residual_resources": 0',
    ):
        if fragment not in owner_contract:
            raise RegistrationError(
                f"oceanbase-consumer-flow owner contract is missing {fragment}"
            )


def validate_opengauss_consumer_flow_profile(
    repository: Path, registered: set[str]
) -> None:
    if "opengauss-consumer-flow" not in registered:
        return
    hosted = (repository / "scripts/test/online-hosted-opengauss-gate.sh").read_text(
        encoding="utf-8"
    )
    for fragment in (
        "# ADDP_ONLINE_SUITES=opengauss-consumer-flow",
        "GITHUB_ACTIONS",
        "RUNNER_OS",
        "Linux",
        "x86_64",
        "POSTGRES_DB=addp_online",
        "ADDP_ONLINE_SECRET_DIR",
        "go run ./cmd/online-test-fixture",
        "ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN",
        "opengauss-official-media.sh",
        "refusing to reuse existing image",
        "bash business/scripts/online-opengauss-consumer-fixture.sh start",
        "python3 scripts/test/online-engine-registration.py",
        "--descriptor \"$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE\"",
        "for start_target in -manager -develop -service",
        "unset ADDP_TEST_OPENGAUSS_DSN",
        'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        "bash scripts/dev/stop.sh",
        "bash scripts/infra/down.sh --volumes --force",
    ):
        if fragment not in hosted:
            raise RegistrationError(
                f"opengauss-consumer-flow Hosted profile is missing {fragment}"
            )
    for relative in (
        "business/scripts/online-opengauss-consumer-fixture.sh",
        "scripts/test/online-hosted-opengauss-gate.sh",
        "scripts/test/online-hosted-opengauss-gate_test.py",
        "scripts/test/online-engine-registration.py",
        "scripts/test/online-engine-registration_test.py",
        "scripts/test/online-opengauss-consumer-fixture_test.py",
        "scripts/test/relational-consumer-flow-online.py",
        "scripts/test/relational-consumer-flow-online_test.py",
        "system/backend/cmd/online-test-fixture/main.go",
        "system/backend/cmd/online-test-fixture/main_test.go",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"opengauss-consumer-flow requires {relative}")
    fixture = (
        repository / "business/scripts/online-opengauss-consumer-fixture.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "opengauss_ensure_official_image x86_64",
        "refusing to reuse existing image",
        "addp-opengauss-online-disposable",
        "addp_opengauss_online",
        "addp_online_consumer_source",
        "addp_online_consumer_target",
        "MERGE INTO",
        "container_exists",
        "start|advance|stop|status",
        "ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE",
        '"engine_type": "opengauss"',
    ):
        if fragment not in fixture:
            raise RegistrationError(
                f"opengauss-consumer-flow fixture contract is missing {fragment}"
            )
    if "/api/v1/system/engines" in fixture:
        raise RegistrationError(
            "opengauss-consumer-flow Business fixture must not own System Engine registration"
        )
    registration = (
        repository / "scripts/test/online-engine-registration.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        "/api/v1/system/engines",
        'test_result.get("success") is not True',
        "ADDP_ONLINE_CONSUMER_ENGINE_ID",
    ):
        if fragment not in registration:
            raise RegistrationError(
                f"opengauss-consumer-flow registration helper is missing {fragment}"
            )
    owner = (
        repository / "scripts/test/relational-consumer-flow-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        '"opengauss": ConsumerProfile(',
        'namespace_kind="schema"',
        'identifier_quote=\'"\'',
        'fixture_script="business/scripts/online-opengauss-consumer-fixture.sh"',
        '"schema_version": "addp.relational-consumer-flow-online/v1"',
        '"residual_resources": 0',
    ):
        if fragment not in owner:
            raise RegistrationError(
                f"opengauss-consumer-flow owner contract is missing {fragment}"
            )
    identity_fixture = (
        repository / "system/backend/cmd/online-test-fixture/main.go"
    ).read_text(encoding="utf-8")
    for fragment in (
        'RoleKey: "online.engine_provisioner"',
        '"system.engine.create"',
        '"system.engine.execute"',
        'RoleKey: "online.relational_consumer"',
        '"ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN"',
    ):
        if fragment not in identity_fixture:
            raise RegistrationError(
                f"opengauss-consumer-flow identity fixture is missing {fragment}"
            )
    if "ADDP_ONLINE_FIXTURE_ADMIN_ACCESS_TOKEN" in identity_fixture:
        raise RegistrationError(
            "opengauss-consumer-flow must not issue an administrator fixture token"
        )


def validate_transfer_insert_only_mysql_profile(
    repository: Path, registered: set[str]
) -> None:
    if "transfer-insert-only-mysql" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(
        encoding="utf-8"
    )
    required_fragments = (
        "transfer-insert-only-mysql)",
        "SYSTEM_URL GATEWAY_URL META_URL TRANSFER_URL CONSOLE_URL",
        "ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_ID",
        "ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_NAME",
        "bash business/scripts/online-transfer-insert-only-fixture.sh start",
        "bash business/scripts/online-transfer-insert-only-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
        "playwright install chromium",
    )
    missing = [fragment for fragment in required_fragments if fragment not in host_gate]
    if missing:
        raise RegistrationError(
            "transfer-insert-only-mysql profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-transfer-insert-only-fixture.sh",
        "scripts/test/transfer-insert-only-mysql-online.py",
        "scripts/test/online-transfer-insert-only-fixture_test.py",
        "scripts/test/transfer-insert-only-mysql-online_test.py",
        "console/frontend/playwright.online.config.js",
        "console/frontend/e2e/online/transfer-insert-only-mysql.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"transfer-insert-only-mysql requires {relative}")
    environment_example = (repository / ".env.example").read_text(encoding="utf-8")
    for variable in (
        "ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_ID",
        "ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_NAME",
        "ADDP_ONLINE_TRANSFER_MYSQL_PORT",
        "ADDP_ONLINE_TRANSFER_MYSQL_DATABASE",
        "ADDP_ONLINE_TRANSFER_MYSQL_USER",
        "ADDP_ONLINE_TRANSFER_MYSQL_PASSWORD",
        "ADDP_ONLINE_TRANSFER_MYSQL_ROOT_PASSWORD",
    ):
        if variable not in environment_example:
            raise RegistrationError(
                f"transfer-insert-only-mysql environment template is missing {variable}"
            )
    fixture = (
        repository / "business/scripts/online-transfer-insert-only-fixture.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "online-engine-fixture.sh",
        "business-mysql",
        "addp_online_transfer_insert_only_source",
        "addp_online_transfer_insert_only_target",
        "start|advance|verify|stop|status",
        "REVOKE ALL PRIVILEGES, GRANT OPTION",
        "GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, INDEX",
        "target rows do not prove insert-only semantics",
        "DECIMAL(6,2)",
    ):
        if fragment not in fixture:
            raise RegistrationError(
                f"transfer-insert-only-mysql fixture contract is missing {fragment}"
            )
    owner = (
        repository / "scripts/test/transfer-insert-only-mysql-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        "/api/v1/meta/scan/run/manual",
        "transfer-insert-only-mysql.spec.js",
        "initial_records_written",
        "incremental_records_written",
        "old_update_ignored",
        "cleanup_tasks(client",
        '"residual_resources": 0',
    ):
        owner_contract = owner + (
            repository / "scripts/test/security-transfer-protection-online.py"
        ).read_text(encoding="utf-8")
        if fragment not in owner_contract:
            raise RegistrationError(
                f"transfer-insert-only-mysql owner contract is missing {fragment}"
            )
    browser = (
        repository / "console/frontend/e2e/online/transfer-insert-only-mysql.spec.js"
    ).read_text(encoding="utf-8")
    for fragment in (
        "task-recommend-decimal-definitions",
        "input[value=\"insert_only\"]",
        "change_detection?.tie_breaker).toEqual([])",
        "initial_records_written: 6",
        "incremental_records_written: 1",
        "old_update_ignored: true",
        "controlFixture(repository, 'verify')",
        "task_deleted: true",
    ):
        if fragment not in browser:
            raise RegistrationError(
                f"transfer-insert-only-mysql browser contract is missing {fragment}"
            )


def load_workflow_suites(repository: Path) -> set[str]:
    path = repository / ".github/workflows/online-t4-gates.yml"
    if not path.is_file():
        raise RegistrationError(f"{path.relative_to(repository)} is missing")
    text = path.read_text(encoding="utf-8")
    required_fragments = (
        "workflow_dispatch:",
        "- self-hosted",
        "- macOS",
        "- addp-online",
        "environment: addp-online",
        "bash scripts/test/online-host-gate.sh --check-only",
        "bash scripts/test/online-host-gate.sh",
        "actions/upload-artifact@",
    )
    missing = [fragment for fragment in required_fragments if fragment not in text]
    if missing:
        raise RegistrationError("Online T4 workflow is missing: " + ", ".join(missing))
    hosted_gates = sorted(
        (repository / "scripts/test").glob("online-hosted-*-gate.sh")
    )
    for hosted_gate in hosted_gates:
        relative = hosted_gate.relative_to(repository).as_posix()
        for fragment in (
            f"bash {relative} --check-only",
            f"bash {relative}",
            "ADDP_ONLINE_HOSTED: \"1\"",
            "ADDP_ONLINE_SECRET_DIR:",
        ):
            if fragment not in text:
                raise RegistrationError(
                    f"Online T4 workflow is missing Hosted profile fragment: {fragment}"
                )
    if hosted_gates and "node-version-file: .node-version" not in text:
        raise RegistrationError(
            "Online T4 Hosted profile must use the repository .node-version"
        )
    job_environment_blocks = re.findall(
        r"(?ms)^    env:\n(?P<body>(?:      [^\n]*\n)+)",
        text,
    )
    if any("${{ runner." in block for block in job_environment_blocks):
        raise RegistrationError(
            "Online T4 workflow must not use the step-only runner context in job-level env"
        )
    artifact_assignment = (
        "ADDP_ONLINE_ARTIFACT_DIR: "
        "${{ runner.temp }}/addp-online-${{ github.run_id }}"
    )
    expected_artifact_assignments = 2 * (1 + len(hosted_gates))
    if text.count(artifact_assignment) != expected_artifact_assignments:
        raise RegistrationError(
            "Online T4 workflow must configure the Runner temp artifact directory "
            "on both lifecycle steps for every Runner profile"
        )
    if re.search(r"(?m)^  schedule:\s*$", text):
        raise RegistrationError("Online T4 workflow must remain manual until the first real run passes")
    options = re.search(
        r"(?m)^        options:\n(?P<body>(?:          - [a-z][a-z0-9-]*\n)+)",
        text,
    )
    if options is None:
        raise RegistrationError("Online T4 workflow suite choices are missing")
    return set(re.findall(r"(?m)^          - ([a-z][a-z0-9-]*)$", options.group("body")))


def check_registration(repository: Path) -> None:
    registered = load_registered_suites(repository)
    profiles = load_deployment_profiles(repository)
    workflow = load_workflow_suites(repository)
    if set(profiles) != registered:
        raise RegistrationError(
            f"Online deployment profiles {sorted(profiles)} do not match registered suites {sorted(registered)}"
        )
    if workflow != registered:
        raise RegistrationError(
            f"Online workflow choices {sorted(workflow)} do not match registered suites {sorted(registered)}"
        )
    validate_module_registry_process_profile(repository, registered)
    validate_consumer_engine_recovery_profile(repository, registered)
    validate_enterprise_catalog_publishing_profile(repository, registered)
    validate_workbench_service_consumption_profile(repository, registered)
    validate_manager_internal_artifact_lineage_profile(repository, registered)
    validate_security_transfer_protection_profile(repository, registered)
    validate_security_plaintext_access_profile(repository, registered)
    validate_security_mysql_owner_protection_profile(repository, registered)
    validate_oceanbase_consumer_flow_profile(repository, registered)
    validate_opengauss_consumer_flow_profile(repository, registered)
    validate_transfer_insert_only_mysql_profile(repository, registered)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repository", type=Path, required=True)
    args = parser.parse_args()
    try:
        check_registration(args.repository.resolve())
    except (OSError, RegistrationError) as error:
        print(f"Online CI registration check failed: {error}", file=sys.stderr)
        return 1
    print("Online CI registration is consistent")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
