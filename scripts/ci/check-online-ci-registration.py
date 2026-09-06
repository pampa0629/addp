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
        "/api/v1/manager/pptx_pdf/preview",
        "/api/v1/manager/pptx_pdf_tasks/",
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
        "pptx_preview_requests",
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
        "SYSTEM_URL GATEWAY_URL META_URL TRANSFER_URL DEVELOP_URL SERVICE_URL",
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
        "scripts/test/oceanbase-consumer-flow-online.py",
        "scripts/test/online-oceanbase-consumer-fixture_test.py",
        "scripts/test/oceanbase-consumer-flow-online_test.py",
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
        repository / "scripts/test/oceanbase-consumer-flow-online.py"
    ).read_text(encoding="utf-8")
    owner_contract = owner + (
        repository / "scripts/test/security-transfer-protection-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        'engine.get("engine_type") != "oceanbase"',
        "/api/v1/meta/scan/run/manual",
        '"type": "watermark"',
        '"start": "committed"',
        '"end": "execution_upper_bound"',
        '"apply_mode": "upsert"',
        "/api/v1/develop/executions",
        "/api/query/",
        "advance_fixture()",
        '"empty_resume"',
        "cleanup_tasks(client, task_ids)",
        "cleanup_service(client, service_id)",
        '"residual_resources": 0',
    ):
        if fragment not in owner_contract:
            raise RegistrationError(
                f"oceanbase-consumer-flow owner contract is missing {fragment}"
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
    if text.count(artifact_assignment) != 2:
        raise RegistrationError(
            "Online T4 workflow must configure the Runner temp artifact directory "
            "on both lifecycle steps"
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
