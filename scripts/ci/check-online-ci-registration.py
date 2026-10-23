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


def make_target_commands(makefile: str, target: str, visited: set[str] | None = None) -> str:
    """Resolve the same focused Make owners invoked by the Online aggregate."""
    if visited is None:
        visited = set()
    if target in visited:
        return ""
    visited.add(target)
    match = re.search(rf"(?ms)^{re.escape(target)}:[^\n]*\n(?P<body>.*?)(?=^[^\t\n#]|\Z)", makefile)
    if match is None:
        return ""
    body = match.group("body")
    children = re.findall(r"\$\(MAKE\)\s+(test-[a-z0-9-]+)", body)
    return body + "".join(make_target_commands(makefile, child, visited) for child in children)


def compose_service_block(compose: str, service: str) -> str | None:
    match = re.search(
        rf"(?ms)^  {re.escape(service)}:\s*\n(?P<body>.*?)(?=^  [a-zA-Z0-9_-]+:\s*$|\Z)",
        compose,
    )
    return match.group("body") if match else None


def service_has_required_nofile_limit(service_block: str) -> bool:
    soft = re.search(r"(?m)^\s*soft:\s*(\d+)\s*$", service_block)
    hard = re.search(r"(?m)^\s*hard:\s*(\d+)\s*$", service_block)
    return (
        "nofile:" in service_block
        and soft is not None
        and hard is not None
        and int(soft.group(1)) >= 1_000_000
        and int(hard.group(1)) >= 1_000_000
    )


def load_suite_registry(repository: Path) -> dict[str, object]:
    path = repository / "scripts/test/online-gate.py"
    spec = importlib.util.spec_from_file_location("addp_online_gate_registration", path)
    if spec is None or spec.loader is None:
        raise RegistrationError(f"cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return dict(module.SUITES)


def load_registered_suites(registry: dict[str, object]) -> set[str]:
    return set(registry)


def load_nightly_suites(registry: dict[str, object]) -> set[str]:
    nightly: set[str] = set()
    for name, suite in registry.items():
        enabled = getattr(suite, "nightly", False)
        if not isinstance(enabled, bool):
            raise RegistrationError(
                f"Online suite {name} nightly registration must be boolean"
            )
        if enabled:
            nightly.add(name)
    return nightly


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
    external_paths = sorted(
        (repository / "scripts/test").glob("online-hosted-*-gate.sh")
    ) + sorted(
        (repository / "scripts/test").glob("online-owner-managed-*-gate.sh")
    )
    for external_path in external_paths:
        external_text = external_path.read_text(encoding="utf-8")
        external_suites = re.findall(
            r"(?m)^# ADDP_ONLINE_SUITES=([a-z][a-z0-9-]*)$", external_text
        )
        external_runner = re.search(
            r"(?m)^# ADDP_ONLINE_RUNNER=([a-z][a-z0-9_-]*)$", external_text
        )
        if not external_suites or external_runner is None:
            profile_kind = (
                "Hosted"
                if external_path.name.startswith("online-hosted-")
                else "owner-managed"
            )
            raise RegistrationError(
                f"{external_path.relative_to(repository)} is missing {profile_kind} Online metadata"
            )
        for suite in external_suites:
            if suite in profiles:
                raise RegistrationError(f"Online suite {suite} has multiple deployment profiles")
            profiles[suite] = external_runner.group(1)
    return profiles


def validate_workflow_runtime_health_profile(repository: Path, registered: set[str]) -> None:
    if "workflow-runtime-health-recovery" not in registered:
        return
    host = (repository / "scripts/test/online-host-gate.sh").read_text()
    if "workflow-runtime-health-recovery" in host:
        raise RegistrationError("Runtime health must not retain a self-hosted profile")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  workflow-health-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
            or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'workflow-runtime-health-recovery'" not in job.group("body")
            or "inputs.suite != 'workflow-runtime-health-recovery'" not in workflow):
        raise RegistrationError("Runtime health requires one manual Hosted Linux job")
    for filename in ("workflow-runtime-health-recovery-online.py", "workflow-runtime-health-recovery-online_test.py",
                     "workflow-runtime-health-tool.py", "online-hosted-workflow-health-gate.sh", "online-hosted-workflow-health-gate_test.py"):
        if not (repository / "scripts/test" / filename).is_file():
            raise RegistrationError(f"Runtime health requires {filename}")
    makefile = (repository / "Makefile").read_text()
    if "$(MAKE) test-workflow-runtime-health-online-runner" not in make_target_commands(makefile, "test-online-runner"):
        raise RegistrationError("Runtime health regression must belong to test-online-runner")
    commands = make_target_commands(makefile, "test-workflow-runtime-health-online-runner")
    for fragment in ("workflow-runtime-health-recovery-online_test.py", "online-hosted-workflow-health-gate_test.py", "WorkflowRuntimeHealth"):
        if fragment not in commands:
            raise RegistrationError(f"Runtime health regression missing {fragment}")
    gate = (repository / "scripts/test/online-hosted-workflow-health-gate.sh").read_text()
    for fragment in ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
                     'scripts/dev/start.sh --exact-process "$target"', 'for target in -system -gateway',
                     '--suite workflow-runtime-health-recovery', '--services model3d-workflow-engine,pointcloud-workflow-engine,document-workflow-engine',
                     'workflow-runtime-health-recovery-online.py --restore-only', 'docker rm -fv', 'runtime_cleanup=',
                     'workflow-runtime-health-recovery-online.py --assert-empty', 'image-missing',
                     'verify_online_product_identity', 'startup-missing'):
        if fragment not in gate:
            raise RegistrationError(f"Runtime health Hosted lifecycle missing {fragment}")
    scenario = (repository / "scripts/test/workflow-runtime-health-recovery-online.py").read_text()
    tests = (repository / "scripts/test/workflow-runtime-health-recovery-online_test.py").read_text()
    if '"startup_cycles": startup_cycles' not in scenario or "run_startup_recovery(observer)" not in scenario:
        raise RegistrationError("Runtime health must run and report initial unhealthy registration")
    if "test_startup_recovery_uses_only_automatic_registration_and_periodic_observation" not in tests:
        raise RegistrationError("Runtime health startup registration regression is missing")


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
        "bash scripts/dev/stop-exact-process.sh --force -manager",
        "observe_module_lifecycle manager-abnormally-stopped",
        "observe_module_lifecycle manager-restarted",
        "observe_module_lifecycle manager-gracefully-stopped",
        "ADDP_ONLINE_TEST_PLATFORM_ACCESS_TOKEN",
        "ADDP_HOST_NODE_IPS",
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
        "ADDP_ONLINE_TEST_TENANT_ADMIN_ACCESS_TOKEN",
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
    hosted_path = repository / "scripts/test/online-hosted-manager-gate.sh"
    if not hosted_path.is_file():
        raise RegistrationError("manager-internal-artifact-lineage requires Hosted lifecycle")
    hosted = hosted_path.read_text(encoding="utf-8")
    for fragment in (
        'source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
        'MODEL3D_DOCKER_PLATFORM=linux/amd64',
        'start_online_geopython_runtime',
        'online-raster-minio-fixture.py manager-seed',
        'RUNTIME_CONTAINER=addp-manager-raster-runtime',
        'scripts/build/build-images.sh --verify',
        '-meta -manager -monitor -pointcloud-workflow -document-workflow',
        'start_online_model3d_runtime', 'remove_online_product_container addp-manager-model3d-runtime',
        'start_online_model3d_runtime addp-manager-model3d-runtime', 'addp-model3d-workflow-engine:online',
        'ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID',
        'bash business/scripts/online-manager-minio-fixture.sh start',
        'bash business/scripts/online-manager-minio-fixture.sh stop',
        'bash business/scripts/online-manager-nfs-fixture.sh start',
        'bash business/scripts/online-manager-nfs-fixture.sh stop',
        'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID',
        'go run ./cmd/online-test-fixture --suite manager-internal-artifact-lineage',
        'scripts/test/online-engine-registration.py',
        'playwright install --with-deps chromium',
    ):
        if fragment not in hosted:
            raise RegistrationError(f"manager-internal-artifact-lineage Hosted profile is missing {fragment}")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
    job = re.search(r"(?ms)^  manager-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if not job or "runs-on: ubuntu-24.04" not in job.group("body"):
        raise RegistrationError("manager-internal-artifact-lineage requires Ubuntu Hosted job")
    if "nfs-kernel-server" not in job.group("body"):
        raise RegistrationError("Manager content boundary acceptance requires its Hosted NFS package")
    if "inputs.suite != 'manager-internal-artifact-lineage'" not in workflow:
        raise RegistrationError("manager-internal-artifact-lineage must not also dispatch on self-hosted")
    for relative in (
        "business/scripts/online-manager-minio-fixture.sh",
        "business/scripts/online-manager-nfs-fixture.sh",
        "business/scripts/online-content-boundary-fixture.py",
        "scripts/test/manager-internal-artifact-lineage-online.py",
        "scripts/test/manager-img-online.py",
        "scripts/test/manager-img-online_test.py",
        "console/frontend/playwright.online.config.js",
        "console/frontend/e2e/online/manager-internal-artifact-lineage.spec.js",
        "console/frontend/e2e/online/manager-img.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"manager-internal-artifact-lineage requires {relative}")
    img_owner = (repository / 'scripts/test/manager-img-online.py').read_text(encoding='utf-8')
    img_browser = (repository / 'console/frontend/e2e/online/manager-img.spec.js').read_text(encoding='utf-8')
    manager_owner = (repository / 'scripts/test/manager-internal-artifact-lineage-online.py').read_text(encoding='utf-8')
    make = (repository / 'Makefile').read_text(encoding='utf-8')
    img_runner = re.search(r'(?ms)^test-manager-online-runner:[^\n]*\n(?P<body>(?:\t[^\n]*\n)+)', make)
    if ("img_scene.run(repository, environment, sys.modules[__name__])" not in manager_owner
            or not img_runner or '\t@python3 -m unittest scripts/test/manager-img-online_test.py' not in img_runner.group('body')
            or '\t@node --check --input-type=module < console/frontend/e2e/online/manager-img.spec.js' not in img_runner.group('body')
            or any(fragment not in img_owner for fragment in ('manager-img.spec.js', 'generate_raster_cog', 'cleanup_managed_artifact', 'source_format', 'img-source', 'addp.manager-img/v1'))
            or any(fragment not in img_browser for fragment in ('modelPixels', 'range_loaded', 'reused_result', 'generation_unavailable', 'download_sha256', 'addp.manager-img-browser/v1'))):
        raise RegistrationError('Manager IMG Hosted subscene must include real generation, browser, cleanup and standard regressions')
    browser_config = (repository / "console/frontend/playwright.online.config.js").read_text(encoding="utf-8")
    for flag in ("--use-gl=angle", "--use-angle=swiftshader-webgl", "--enable-unsafe-swiftshader"):
        if flag not in browser_config:
            raise RegistrationError("manager-internal-artifact-lineage Hosted browser requires software WebGL")
    fixture = (repository / "business/scripts/online-manager-minio-fixture.sh").read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "business-minio",
        "pdal_las12_format0.las",
        "addp_online_preview_fixture.pptx",
        "MC_HOST_fixture",
        "dae/model.dae",
        "3ds/model.3ds",
        "texture.png",
        "format-samples/gbk-quoted.csv",
        "format-samples/gbk-quoted.tsv",
        "format-samples/format-validation.xlsx",
        "format-samples/document.json",
        "format-samples/records.json",
        "format-samples/features.geojson",
        "format-samples/features.json",
        "format-samples/trajectory.csv",
        "format-samples/trajectory.json",
        "format-samples/trajectory.geojson",
        "format-samples/vector-exchange/sites.kml",
        "format-samples/vector-exchange/sites.gml",
        "format-samples/vector-exchange/projected.topojson",
        "index-source-versions/source.csv",
        "index-source-versions/source.tsv",
        "index-csv-change",
        "index-tsv-restore",
        "index source restoration refused unexpected bytes",
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
        "model_3d_glb_generation",
        "/api/v1/manager/model_3d_glb/",
        "validate_model_glb",
        "cleanup_managed_artifact",
        "generate_raster_cog",
        "raster_to_cog",
        "raster_physical",
        "DELIMITED_SHA256",
        "delimited_sources",
        "WORKBOOK_SHA256",
        "WORKBOOK_SHEETS",
        "workbook_source",
        "STRUCTURED_JSON_SOURCES",
        "structured_json_sources",
        "TRAJECTORY_SOURCES",
        "trajectory_sources",
        "VECTOR_EXCHANGE_SOURCES",
        "vector_exchange_sources",
        "MODEL_FORMATS",
        "MESH_SOURCE_FACTS",
        "FBX_BASIC_SOURCE",
        "prepare_basic_fbx_source",
        "fbx_basic_preview",
        "verify_content_boundaries",
        "boundaries = verify_content_boundaries",
        "boundaries += verify_content_boundaries",
        "content-boundaries.json",
        "addp.content-boundaries/v2",
        "directory-convergence",
        "refs-convergence",
        '"removed_components"',
        '"preserved_initial_items"',
        "force=not converging",
        "ADDP_ONLINE_MANAGER_NFS_ENGINE_ID",
        "verify_mesh_source",
        "validate_model_bundle",
        "download_bundle",
        "addp.manager-internal-artifact-lineage/v15",
        '"header_mode"',
        '"tested_has_header": False',
        "first_record_verified",
        "ordinary_refresh_preserved",
        "versions = verify_index_source_versions",
        "index-source-versions.json",
        "addp.index-source-versions/v2",
        'storage in ("nfs", "minio")',
        "verify_manager_execution_events",
        "addp.manager-execution-events/v1",
        "manager-execution-events.json",
    ):
        if fragment not in owner:
            raise RegistrationError(
                f"manager-internal-artifact-lineage owner contract is missing {fragment}"
            )
    nfs = (repository / "business/scripts/online-manager-nfs-fixture.sh").read_text(encoding="utf-8")
    for fragment in ("GITHUB_ACTIONS", "ADDP_ONLINE_HOSTED", "127.0.0.1(ro,sync", ".addp-online-owner", "online-content-boundary-fixture.py", "NFS export cleanup has residuals"):
        if fragment not in nfs:
            raise RegistrationError(f"Manager NFS lifecycle contract is missing {fragment}")
    generator = (repository / "business/scripts/online-content-boundary-fixture.py").read_text(encoding="utf-8")
    for fragment in ("directory-convergence", "refs-convergence"):
        if fragment not in generator:
            raise RegistrationError(f"Manager boundary fixture contract is missing {fragment}")
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
        ".model-preview",
        "model_loaded",
        "content_loaded",
        ".raster-tiff-quick-view",
        "raster_generation_requests",
        "range_loaded",
        "map_loaded",
        "ADDP_ONLINE_MANAGER_BROWSER_PHASE",
        "generation-entry",
        "cached-preview",
        "generation_entry_visible",
        "source_model_loaded",
        "verifyBasicColoredFBX",
        "basic_preview_source",
        "basic_preview: basicPreviewProof",
        "vertex_colors_verified",
        "model-preview-support.js",
        "model_generation_requests",
        "anonymous_refresh_401",
        "failed_business_responses",
        "addp.manager-internal-artifact-lineage-browser/v15",
        "header_mode:",
        "applyHeaderMode(false)",
        "verifyHeaderlessPage(20, 6)",
        "applyHeaderMode(true)",
        "ADDP_ONLINE_MANAGER_DELIMITED_JSON",
        "verifyDelimitedTable",
        "delimited_tables",
        "encoding_request_verified",
        "download_sha256",
        "ADDP_ONLINE_MANAGER_WORKBOOK_JSON",
        "verifyWorkbook",
        ".container-preview",
        "workbook: workbookEvidence",
        "ADDP_ONLINE_MANAGER_STRUCTURED_JSON",
        "verifyStructuredJSON",
        "structured_json: structuredEvidence",
        "ADDP_ONLINE_MANAGER_TRAJECTORIES_JSON",
        "verifyTrajectoryRange",
        "trajectories: trajectoryEvidence",
        "ADDP_ONLINE_MANAGER_VECTOR_EXCHANGE_JSON",
        "verifyVectorExchange",
        "vector_exchange: vectorEvidence",
        "layer_proofs",
        "temporary_reset_verified",
        "content_verified",
        "table_verified",
        "geometry_verified",
        "source_crs",
        "source_srid",
        "unknown_total_verified",
        "refresh_preserved_page",
        "refresh_preserved_empty_sheet",
    ):
        if fragment not in browser:
            raise RegistrationError(
                f"manager-internal-artifact-lineage browser contract is missing {fragment}"
            )


def validate_manager_hybrid_search_profile(repository: Path, registered: set[str]) -> None:
    if "manager-hybrid-search" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "manager-hybrid-search)",
        "START_TARGET=-all",
        "SYSTEM_URL GATEWAY_URL META_URL MANAGER_URL INFERENCE_URL",
        "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN ADDP_ONLINE_TEST_TENANT_ID",
        "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID",
        "ADDP_ONLINE_MANAGER_MINIO_PORT",
        "ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY",
        "ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY",
        "ADDP_ONLINE_MANAGER_MINIO_BUCKET",
        "ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT",
        "ADDP_ONLINE_MANAGER_EMBEDDING_MODEL_PROFILE_ID",
        "bash business/scripts/online-manager-minio-fixture.sh start",
        "bash business/scripts/online-manager-minio-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
    )
    missing = [fragment for fragment in required_fragments if fragment not in host_gate]
    if missing:
        raise RegistrationError(
            "manager-hybrid-search profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-manager-minio-fixture.sh",
        "scripts/test/manager-hybrid-search-online.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"manager-hybrid-search requires {relative}")
    fixture = (repository / "business/scripts/online-manager-minio-fixture.sh").read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "--env-file /dev/null",
        "business-minio",
        "Autocop_4X3.jpg",
        "ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT",
        "MC_HOST_fixture",
    ):
        if fragment not in fixture:
            raise RegistrationError(
                f"manager-hybrid-search fixture contract is missing {fragment}"
            )
    owner = (repository / "scripts/test/manager-hybrid-search-online.py").read_text(encoding="utf-8")
    for fragment in (
        "/api/v1/meta/scan/run/manual",
        "/api/v1/manager/embedding_executions",
        "/api/v1/manager/embeddings?",
        "/api/v1/manager/search?",
        'expected_methods: list[str]',
        '["keyword", "vector"]',
        '["vector"]',
        "vector_hits",
        '"residual_resources": -1',
    ):
        if fragment not in owner:
            raise RegistrationError(
                f"manager-hybrid-search owner contract is missing {fragment}"
            )


def validate_security_transfer_protection_profile(repository: Path, registered: set[str]) -> None:
    if "security-transfer-protection" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    required_fragments = (
        "security-transfer-protection)",
        "START_TARGET=-all",
        "SYSTEM_URL GATEWAY_URL META_URL SECURITY_URL GRAPH_URL TRANSFER_URL MANAGER_URL",
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
    host = (repository / "scripts/test/online-host-gate.sh").read_text()
    if "security-plaintext-access" in host:
        raise RegistrationError("security-plaintext-access must not keep a self-hosted route")
    requirements = {
        "scripts/test/online-hosted-security-gate.sh": (
            "# ADDP_ONLINE_SUITES=security-plaintext-access", "security-plaintext-access-online.py",
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite "$1"', 'playwright install --with-deps chromium',
            '"scripts/test/$INITIALIZER_SCRIPT" --initialize',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN',
        ),
        "business/scripts/online-security-owner-fixture.sh": ("addp_online_security.exemption_source", "13987654321", "GRANT SELECT ON addp_online_security.exemption_source"),
        "scripts/test/security-plaintext-access-online.py": (
            "SUPPORT.require_hosted_initialization(os.environ)", "initialize_fresh_governance(",
            "initialize_exact_record_read_grants(", "ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN",
            "applicant and approver must be two different Users", "preview_rows = SUPPORT.preview_rows",
            "/api/v1/security/protection-access-request-targets", "review-queue?scope=pending",
            '"scope": "history"', "protected_other_subject", "expired_without_security_refresh",
            "revoked_applicant", "revoked_approval_history", 'addp.security-plaintext-access-online/v6', 'validate_plaintext_browser_report(', 'run_plaintext_browser(', 'report["browser"] = run_plaintext_browser(',
            'outage = exercise_security_unavailability(', 'SUPPORT.require_hosted_security_lifecycle(repository)',
            'SUPPORT.run_security_lifecycle(repository, "stop", deadline)', 'SUPPORT.run_security_lifecycle(repository, "start", deadline)',
            'finally:', 'SUPPORT.security_listener_refused()', 'connection_refusals', 'manager_process_unchanged',
            'current_develop_result(', 'authorized_history_masked', 'current_result_rechecked', 'exercise_develop_scopes(', 'same_history_masked_at_expiry', 'same_history_masked_at_revocation',
            'manager_only_does_not_authorize_develop', 'private_history_excluded', 'manager_grant_independent',
            'validate_develop_scope_report(',
            'develop_history_while_unavailable_verified', 'develop_process_unchanged',
        ),
        "scripts/test/security-transfer-protection-online.py": (
            'def require_hosted_security_lifecycle(', 'def ready_process(', 'def verify_restarted_process(',
            'def run_security_lifecycle(', 'def security_listener_refused(',
            'ADDP_ONLINE_TEST_USER_', 'ADDP_ONLINE_TEST_APPROVER_', 'ADDP_ONLINE_FIXTURE_',
            'a disposable Hosted Linux checkout without .env', 'registration_state', 'source_fingerprint',
        ),
        "system/backend/cmd/online-test-fixture/main.go": ("plaintextApplicantPermissions", "plaintextApproverPermissions", "external-online-plaintext-approver", "ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN", "ADDP_ONLINE_TEST_APPROVER_USERNAME", "ADDP_ONLINE_TEST_APPROVER_PASSWORD"),
        "console/frontend/e2e/online/security-plaintext-access.spec.js": (
            "addp.security-plaintext-access-browser/v2", "browser.newContext(", "browserIdentity(",
            "submitRequest(", "approve(", "history(", "revocation_from_security_ui",
            "target.projection_acknowledged === true", "await synchronized", "await secondSynchronized",
            "Date.now() >= Date.parse(first.requested_expires_at) + 1000", "expired_history_verified",
            "revoked_history_verified", "isAnonymousRefreshResponse", "state.started = true",
            "multi_outlet_request_verified", "['manager','develop']",
            "expect(applicantState.failures.concat(approverState.failures)).toEqual([])",
            "expect(applicantState.errors.concat(approverState.errors)).toEqual([])",
            "screenshot: 'off'", "for (const context of contexts) await context.close()",
        ),
    }
    for relative, fragments in requirements.items():
        path = repository / relative
        if not path.is_file():
            raise RegistrationError(f"security-plaintext-access requires {relative}")
        text = path.read_text()
        for fragment in fragments:
            if fragment not in text:
                raise RegistrationError("security-plaintext-access contract is missing " + fragment)
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  security-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "runs-on: ubuntu-24.04" not in job.group("body") or "inputs.suite == 'security-plaintext-access'" not in job.group("body"):
        raise RegistrationError("security-plaintext-access requires a manual Ubuntu Hosted job")
    if "&& inputs.suite != 'security-plaintext-access'" not in workflow:
        raise RegistrationError("security-plaintext-access must not also dispatch on self-hosted")
    aggregate = make_target_commands((repository / "Makefile").read_text(), "test-online-runner")
    for test in ("security-plaintext-access-online_test.py", "online-hosted-security-gate_test.py"):
        if not aggregate or test not in aggregate:
            raise RegistrationError("security-plaintext-access regression must enter test-online-runner")


def validate_security_mysql_owner_protection_profile(
    repository: Path, registered: set[str]
) -> None:
    if "security-mysql-owner-protection" not in registered:
        return
    suite = load_suite_registry(repository)["security-mysql-owner-protection"]
    if ("monitor", "MONITOR_URL") not in suite.services:
        raise RegistrationError("security-mysql-owner-protection must preflight Monitor")
    hosted_path = repository / "scripts/test/online-hosted-security-gate.sh"
    if not hosted_path.is_file():
        raise RegistrationError("security-mysql-owner-protection requires Hosted lifecycle")
    hosted = hosted_path.read_text(encoding="utf-8")
    for fragment in (
        'source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
        'GOWORK="$ADDP_ONLINE_SECRET_DIR/go.work"', 'go work init "${go_modules[@]}"',
        'GOMODCACHE="$ROOT_DIR/.gomodcache"',
        'cp "$ROOT_DIR/go.work.sum" "${GOWORK}.sum"',
        'for start_target in -meta -security -manager -develop -service -transfer -graph -monitor',
        'playwright install --with-deps chromium',
        'MONITOR_URL=', 'CONSOLE_URL=',
        'bash business/scripts/online-security-owner-fixture.sh start',
        'bash business/scripts/online-security-owner-fixture.sh stop',
        '--suite "$1"', 'scripts/test/online-engine-registration.py',
        'security-mongodb.json', 'ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID', 'addp-security-online-mongodb',
        '"scripts/test/$INITIALIZER_SCRIPT" --initialize',
        'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN',
        'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
    ):
        if fragment not in hosted:
            raise RegistrationError(f"security-mysql-owner-protection Hosted profile is missing {fragment}")
    host = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    if "security-mysql-owner-protection" in host:
        raise RegistrationError("security-mysql-owner-protection must not keep a self-hosted route")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
    job = re.search(r"(?ms)^  security-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
            or "inputs.suite == 'security-mysql-owner-protection'" not in job.group("body")):
        raise RegistrationError("security-mysql-owner-protection requires a manual Ubuntu Hosted job")
    if "&& inputs.suite != 'security-mysql-owner-protection'" not in workflow:
        raise RegistrationError("security-mysql-owner-protection must not also dispatch on self-hosted")
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    aggregate = make_target_commands((repository / "Makefile").read_text(), "test-online-runner")
    for test in ("online-hosted-security-gate_test.py", "online-security-owner-fixture_test.py", "security-mysql-owner-protection-online_test.py"):
        if not aggregate or test not in aggregate:
            raise RegistrationError("security-mysql-owner-protection regression must enter test-online-runner")
    for relative in (
        "business/scripts/online-security-owner-fixture.sh",
        "scripts/test/security-mysql-owner-protection-online.py",
        "scripts/test/security-transfer-protection-online.py",
        "console/frontend/e2e/online/security-manager-export.spec.js",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"security-mysql-owner-protection requires {relative}")
    fixture = (repository / "business/scripts/online-security-owner-fixture.sh").read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOSTED", "GITHUB_ACTIONS", "container ownership mismatch", "container remains after cleanup",
        "bank-schema-add|bank-schema-remove", "_security_drift_probe", "ADD COLUMN", "DROP COLUMN",
        "--tmpfs /var/lib/postgresql/data", "--tmpfs /var/lib/mysql", "CREATE EXTENSION postgis",
        "location_point geometry(Point, 4326)", "addp_online_security.mysql_email_transfer",
        "CREATE TABLE bank_card_accounts", "credit_card_number int NOT NULL", "addp_online_security.bank_card_transfer",
        "addp_online_security.spatial_algorithm_source", "addp_online_security.spatial_algorithm_transfer",
        "business/mysql/test-data.sh", "security_reader", "security_writer",
        "addp-security-online-mongodb", "--tmpfs /data/db", "fixture.Persons.insertMany", "Outdoor",
        'sourceVersion: NumberLong("9007199254740993")',
        "addp_online_security.mongodb_query_transfer", "addp_online_security.mongodb_query_control", "addp_online_security.mongodb_query_denied",
    ):
        if fragment not in fixture:
            raise RegistrationError("security-mysql-owner-protection fixture contract is missing " + fragment)
    owner = (
        repository / "scripts/test/security-mysql-owner-protection-online.py"
    ).read_text(encoding="utf-8")
    owner_contract = owner + (
        repository / "scripts/test/security-transfer-protection-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        "/api/v1/meta/scan/run/manual",
        "/api/v1/security/sensitive-data-types",
        "security.sensitive_data_type.create",
        "security.sensitive_data_type.delete",
        '"default_protection"',
        '"invalid_request_status"',
        '"rollback_verified"',
        '"cleanup_verified"',
        "addp.detector.email_metadata/v1", "addp.detector.phone_metadata/v2",
        "exercise_bank_card_metadata(", "assert_bank_card_rows(", "assert_bank_card_discovery(", "bank_card_projections(",
        "addp.detector.bank_card_number_metadata/v1", '"bank_card_metadata"', '"negative_fields_not_detected"',
        '"temporary_execution_resource_residuals": 0',
        "exercise_mongodb_algorithms(", "assert_mongodb_rows(", '"mongodb_algorithms"', '"sparse_objects_verified"',
        "ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID", "def initialize_postgres_read_grants(",
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

    initializer = re.search(
        r"(?ms)^def initialize_postgres_read_grants\((?P<parameters>.*?)\) -> None:\n(?P<body>.*?)(?=^def |\Z)", owner)
    if (initializer is None
            or any(engine in initializer.group("parameters").lower() for engine in ("mysql", "mongodb"))
            or re.search(r"item_term\s*=\s*['\"]collection['\"]", initializer.group("body"))
            or initializer.group("body").count("REGISTRATION.initialize_exact_record_read_grants(") != 1):
        raise RegistrationError("security-mysql-owner-protection must prepare only PostgreSQL table read Grants")

    for fragment in ("run_export_browser(", "validate_export_browser_report(", "e2e/online/security-manager-export.spec.js",
                     "wait_for_technical_field(", '"technical_field_search"', '"manager.search.execute"',
                     "SUPPORT.require_hosted_security_lifecycle(", "exercise_restart_recovery(", "verify_restarted_process(",
                     '"scripts/dev/restart.sh", "-security", "-manager"', '"verified_before_rescan"',
                     "prepare_mongodb_source(", '"initial_technical_field_search"',
                     "verify_unprotected_mongodb_derived_query(", "verify_mongodb_query_boundaries(", "validate_develop_protection_refusal(",
                     "validate_transfer_protection_refusal(", "validate_service_protection_refusal(",
                     "prepare_mongodb_transfer_targets(", "verify_mongodb_query_transfer(",
                     '"query_transfer"', '"derived_transfer_denied"', '"transfer_during_manager_policy"',
                     '"target_empty_before_and_after"', '"query_output_lineage_unresolved"',
                     "verify_mongodb_raw_export(", '"raw_record_export"', '"export_during_manager_policy"',
                     "canonical_int64_preserved=True",
                     '"develop_during_manager_policy"', '"addp.security-mysql-owner-protection-online/v25"',
                     '"develop_wildcard"', '"SELECT b.* FROM "', '"develop_mixed_wildcard"',
                     "assert_bank_card_mixed_rows(", '"sensitive_copies_verified"', '"develop_page_wildcard"', '"service_sql_page"',
                     "exercise_bank_card_sql_pages(", '"no_duplicate_or_missing_rows"',
                     "exercise_bank_card_computed_outputs(", '"computed_outputs"', '"history_result_absent"', '"unprotected_calculation_rows"',
                     "develop_refusal_with_history(", "exercise_enrolling_refusal(", "exercise_bank_card_refusal_causes(",
                     "def protection_reason_matrix(", "bank_outlet_refusals(", "transfer_refusal_with_history(",
                     "validate_manager_protection_refusal(", '"protection_reason_matrix"', '"protocol_only"', '"coverage_counts"',
                     "run_bank_schema_fixture(", '"protection_not_ready"', '"protection_schema_mismatch"',
                     '"protection_operation_unsupported"', '"refusal_causes"', '"schema_restored"', '"additional_refusals"',
                     '"protection_computed_output_unsupported"', '"protection_lineage_unverified"', '"protection_expired"',
                     '"independent_outlet_fields"', 'expected_by_owner[owner][field]',
                     'outage = exercise_owner_unavailability(', 'case["security_unavailability"] = exercise_owner_unavailability(',
                     'verify_mysql_outage_transfer(', 'SUPPORT.run_security_lifecycle(repository, "stop", deadline)',
                     'SUPPORT.run_security_lifecycle(repository, "start", deadline)', 'finally:',
                     'SUPPORT.security_listener_refused()', 'owner_processes_unchanged', 'target_empty_before',
                     'same_user_verified', 'target_output_verified', 'diagnostics.get("code") != "query_output_lineage_unresolved"',
                     'exercise_explicit_rediscovery(', '"explicit_rediscovery"',
                     '"all_current_revisions_acknowledged"', '"projections_before"', '"projections_after"',
                     'verify_mysql_transfer_output(', '"security.enrollment.update"',
                     'exercise_resource_validity(', 'wait_protection_deadline(', '"resource_validity": resource_validity',
                     '"natural_expiry_verified"', '"permanent_restored"', '"resource_permanent_validity_visible"'):
        if fragment not in owner:
            raise RegistrationError("security-mysql-owner-protection must dispatch and validate export browser: " + fragment)
    browser = (repository / "console/frontend/e2e/online/security-manager-export.spec.js").read_text(encoding="utf-8")
    for fragment in (
        "login(page,", "page.waitForEvent('download'", "source_item_locator",
        "format: 'csv'", "csv.DictReader",
        "transfer_execution_id", "execution.triggered_by", "execution.source_task_id",
        "/api/v1/monitor/executions/by-execution-id/", ".execution-detail-content",
        "addp.security-manager-export-browser/v9", "anonymous_refresh_401", "download.delete()",
        "protected_preview_verified", "technical_field_search_verified", "search_to_preview_verified",
        "verifyResourceCombinationFilters(", "resource_combination_filters_verified", "resource_filters_read_only",
        "verifyDevelopProtectionRefusal(", "develop_refusal_diagnostic_verified", "develop_monitor_same_execution",
        "verifyAdditionalProtectionHistories(", "additional_protection_histories_verified",
        "additional_protection_monitors_verified", "additional_protection_refusal_count",
        "verifyServiceTransferProtectionRefusal(", "service_old_preview_cleared", "transfer_refusal_visible",
        "verifyTransferResultProtection(", "transfer_result_stage", "transfer_result_target_unchanged",
        "transfer_result_baseline_restored", "result_browser_residual_resources", "result_transfer_execution_id",
        "security-transfer-result-refusal.png", "security-transfer-result-monitor.png",
        "output_protection_permanent_restored", "output_browser_residual_resources", "protection_transfer_execution_id",
        "security-service-protection-refusal.png", "security-service-protection-restored.png",
        "security-transfer-protection-refusal.png", "security-transfer-protection-monitor.png",
        "security-develop-protection-refusal.png", "security-develop-protection-monitor.png",
        "browserAPIFromResponse(", "browser_session_rebound_verified",
        "resource_filter_case_count", "security-resource-combination-filters.png", "security-resource-empty-intersection.png",
        "security-resource-filter-reset.png",
        "security-manager-protected-preview.png", "security-manager-field-search.png", ".field-name mark",
        "expect(browserErrors).toEqual([])", "expect(failedResponses).toEqual([])",
    ):
        if fragment not in browser:
            raise RegistrationError("security-mysql-owner-protection export browser contract is missing " + fragment)


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
        '"verification_owner": "deployment_profile"',
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
    if "/api/v1/system/engines" in owner or '"system.engine.read"' in owner:
        raise RegistrationError(
            "oceanbase-consumer-flow consumer must not access the System Engine control plane"
        )


def validate_tidb_consumer_flow_profile(
    repository: Path, registered: set[str]
) -> None:
    if "tidb-consumer-flow" not in registered:
        return
    host_gate = (repository / "scripts/test/online-host-gate.sh").read_text(
        encoding="utf-8"
    )
    required_fragments = (
        "tidb-consumer-flow)",
        "SYSTEM_URL GATEWAY_URL META_URL MANAGER_URL TRANSFER_URL DEVELOP_URL SERVICE_URL",
        "ADDP_ONLINE_TIDB_ENGINE_ID",
        "ADDP_ONLINE_TIDB_PORT",
        "ADDP_ONLINE_TIDB_DATABASE",
        "ADDP_ONLINE_TIDB_USER",
        "bash business/scripts/online-tidb-consumer-fixture.sh start",
        "bash business/scripts/online-tidb-consumer-fixture.sh stop",
        'bash scripts/dev/start.sh "$START_TARGET"',
    )
    missing = [fragment for fragment in required_fragments if fragment not in host_gate]
    if missing:
        raise RegistrationError(
            "tidb-consumer-flow profile is missing: " + ", ".join(missing)
        )
    for relative in (
        "business/scripts/online-tidb-consumer-fixture.sh",
        "scripts/test/docker-compose.tidb-t2.yml",
        "scripts/test/online-tidb-consumer-fixture_test.py",
        "scripts/test/relational-consumer-flow-online.py",
        "scripts/test/relational-consumer-flow-online_test.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"tidb-consumer-flow requires {relative}")
    fixture = (
        repository / "business/scripts/online-tidb-consumer-fixture.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "ADDP_ONLINE_HOST",
        "docker-compose.tidb-t2.yml",
        "addp-online-tidb-consumer",
        "addp_online_consumer_source",
        "addp_online_consumer_target",
        "start|advance|stop|status",
        "reset_fixture",
        "down --volumes --remove-orphans",
        "assert_zero_residue",
    ):
        if fragment not in fixture:
            raise RegistrationError(
                f"tidb-consumer-flow fixture contract is missing {fragment}"
            )
    compose = (
        repository / "scripts/test/docker-compose.tidb-t2.yml"
    ).read_text(encoding="utf-8")
    for image in ("pingcap/pd", "pingcap/tikv", "pingcap/tidb"):
        if re.search(
            rf"(?m)^\s*image:\s*{re.escape(image)}:v8\.5\.8@sha256:[0-9a-f]{{64}}\s*$",
            compose,
        ) is None:
            raise RegistrationError(
                f"tidb-consumer-flow Compose must pin {image}:v8.5.8 by digest"
            )
    tikv_service = compose_service_block(compose, "tidb-tikv")
    if tikv_service is None or not service_has_required_nofile_limit(tikv_service):
        raise RegistrationError(
            "tidb-consumer-flow Compose must set tidb-tikv nofile soft/hard limits to at least 1000000"
        )
    if re.search(r"(?m)^volumes:\s*$", compose):
        raise RegistrationError(
            "tidb-consumer-flow Compose must remain disposable and volume-free"
        )
    owner = (
        repository / "scripts/test/relational-consumer-flow-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        '"tidb": ConsumerProfile(',
        'namespace_kind="database"',
        'item_code_prefix="TIDB"',
        'identifier_quote="`"',
        'fixture_script="business/scripts/online-tidb-consumer-fixture.sh"',
        '"verification_owner": "deployment_profile"',
        '"schema_version": "addp.relational-consumer-flow-online/v1"',
        '"residual_resources": 0',
    ):
        if fragment not in owner:
            raise RegistrationError(
                f"tidb-consumer-flow owner contract is missing {fragment}"
            )
    if "/api/v1/system/engines" in owner or '"system.engine.read"' in owner:
        raise RegistrationError(
            "tidb-consumer-flow consumer must not access the System Engine control plane"
        )


def validate_opengauss_consumer_flow_profile(
    repository: Path, registered: set[str]
) -> None:
    if "opengauss-consumer-flow" not in registered:
        return
    hosted = (repository / "scripts/test/online-hosted-opengauss-gate.sh").read_text(
        encoding="utf-8"
    )
    if 'source "$ROOT_DIR/scripts/utils/hosted-online.sh"' not in hosted:
        raise RegistrationError("opengauss profile must use the shared Hosted lifecycle")
    hosted += (repository / "scripts/utils/hosted-online.sh").read_text(encoding="utf-8")
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
        "for start_target in -security -manager -develop -service -transfer",
        "--wait-protection-owners manager,develop,service,transfer",
        "unset ADDP_TEST_OPENGAUSS_DSN",
        'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        "bash scripts/dev/stop.sh",
        "bash scripts/infra/down.sh --volumes --force",
    ):
        if fragment not in hosted:
            raise RegistrationError(
                f"opengauss-consumer-flow Hosted profile is missing {fragment}"
            )
    infra_up = (repository / "scripts/infra/up.sh").read_text(encoding="utf-8")
    for fragment in (
        "BUILD_REPOSITORY_POSTGRES_IMAGE=true",
        "docker build --file scripts/infra/Dockerfile.postgres",
        '--tag "$POSTGRES_IMAGE" scripts/infra',
    ):
        if fragment not in infra_up:
            raise RegistrationError(
                f"opengauss-consumer-flow Infra lifecycle is missing {fragment}"
            )
    for relative in (
        "business/scripts/online-opengauss-consumer-fixture.sh",
        "scripts/infra/Dockerfile.postgres",
        "scripts/infra/up.sh",
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
        '"verification_owner": "deployment_profile"',
        '"schema_version": "addp.relational-consumer-flow-online/v1"',
        '"residual_resources": 0',
    ):
        if fragment not in owner:
            raise RegistrationError(
                f"opengauss-consumer-flow owner contract is missing {fragment}"
            )
    if "/api/v1/system/engines" in owner or '"system.engine.read"' in owner:
        raise RegistrationError(
            "opengauss-consumer-flow consumer must not access the System Engine control plane"
        )
    identity_fixture = (
        repository / "system/backend/cmd/online-test-fixture/main.go"
    ).read_text(encoding="utf-8")
    for fragment in (
        'engineProvisionerRoleKey = "tenant.infrastructure_administrator"',
        "repository.GetActiveBuiltinRoleByKey(ctx, engineProvisionerRoleKey)",
        'RoleKey: "online." + strings.ReplaceAll(*suite, "-", "_")',
        "permissions, err := suitePermissions(*suite)",
        "PermissionKeys: permissions",
        '"ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN"',
    ):
        if fragment not in identity_fixture:
            raise RegistrationError(
                f"opengauss-consumer-flow identity fixture is missing {fragment}"
            )
    consumer_role = re.search(r'(?ms)^var consumerPermissions = \[\]string\{(?P<body>.*?)^\}', identity_fixture)
    if consumer_role is None or re.search(r'"system\.engine(?:\.|_)[^"]*"', consumer_role.group('body')):
        raise RegistrationError(
            "opengauss-consumer-flow consumer role must not include System Engine permissions"
        )
    if "ADDP_ONLINE_FIXTURE_ADMIN_ACCESS_TOKEN" in identity_fixture:
        raise RegistrationError(
            "opengauss-consumer-flow must not issue an administrator fixture token"
        )


def validate_kingbase_consumer_flow_profile(
    repository: Path, registered: set[str]
) -> None:
    if "kingbase-consumer-flow" not in registered:
        return
    owner_gate = (
        repository / "scripts/test/online-owner-managed-kingbase-gate.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "# ADDP_ONLINE_SUITES=kingbase-consumer-flow",
        "# ADDP_ONLINE_RUNNER=owner-managed-linux-x86_64",
        "ADDP_ONLINE_OWNER_MANAGED",
        "kingbase_load_owner_environment",
        "kingbase_validate_license_input",
        "bash business/scripts/online-kingbase-consumer-fixture.sh start",
        "bash business/scripts/online-kingbase-consumer-fixture.sh stop",
        "scripts/test/online-engine-registration.py",
        'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        "media_sha256=",
        "license_sha256=",
    ):
        if fragment not in owner_gate:
            raise RegistrationError(
                f"kingbase-consumer-flow owner-managed profile is missing {fragment}"
            )
    for relative in (
        "business/scripts/online-kingbase-consumer-fixture.sh",
        "scripts/lib/kingbase-official-media.sh",
        "scripts/test/online-owner-managed-kingbase-gate.sh",
        "scripts/test/online-owner-managed-kingbase-gate_test.py",
        "scripts/test/online-kingbase-consumer-fixture_test.py",
        "scripts/test/online-engine-registration.py",
        "scripts/test/relational-consumer-flow-online.py",
        "scripts/test/relational-consumer-flow-online_test.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"kingbase-consumer-flow requires {relative}")
    fixture = (
        repository / "business/scripts/online-kingbase-consumer-fixture.sh"
    ).read_text(encoding="utf-8")
    for fragment in (
        "kingbase_ensure_official_image",
        "kingbase_validate_license_input",
        "docker create",
        "docker rm --force",
        "docker image rm",
        "addp-kingbase-online-disposable",
        "addp_kingbase_online",
        "addp_online_consumer_source",
        "addp_online_consumer_target",
        "ON CONFLICT (id) DO UPDATE",
        '"engine_type": "kingbase"',
        "start|advance|stop|status",
    ):
        if fragment not in fixture:
            raise RegistrationError(
                f"kingbase-consumer-flow fixture contract is missing {fragment}"
            )
    owner = (
        repository / "scripts/test/relational-consumer-flow-online.py"
    ).read_text(encoding="utf-8")
    for fragment in (
        '"kingbase": ConsumerProfile(',
        'engine_type="kingbase"',
        'namespace_kind="schema"',
        'fixture_script="business/scripts/online-kingbase-consumer-fixture.sh"',
        '"schema_version": "addp.relational-consumer-flow-online/v1"',
        '"residual_resources": 0',
    ):
        if fragment not in owner:
            raise RegistrationError(
                f"kingbase-consumer-flow owner contract is missing {fragment}"
            )
    if "/api/v1/system/engines" in owner:
        raise RegistrationError(
            "kingbase-consumer-flow consumer must not access the System Engine control plane"
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


def validate_transfer_relational_sql_etl_profile(repository: Path, registered: set[str]) -> None:
    if "transfer-relational-sql-etl" not in registered:
        return
    suite = load_suite_registry(repository)["transfer-relational-sql-etl"]
    if not {("manager", "MANAGER_URL"), ("develop", "DEVELOP_URL"), ("orchestrator", "ORCHESTRATOR_URL"), ("monitor", "MONITOR_URL"), ("security", "SECURITY_URL")}.issubset(suite.services):
        raise RegistrationError("transfer-relational-sql-etl must preflight Manager, Develop, Orchestrator, Monitor and Security")
    hosted_path = repository / "scripts/test/online-hosted-transfer-gate.sh"
    if not hosted_path.is_file():
        raise RegistrationError("transfer-relational-sql-etl requires Hosted lifecycle")
    hosted = hosted_path.read_text(encoding="utf-8")
    for fragment in (
        'source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
        'for start_target in -security -transfer -manager -develop -orchestrator -monitor', 'CONSOLE_URL=', 'SECURITY_URL=',
        'bash business/scripts/online-transfer-relational-sql-etl-fixture.sh start',
        'bash business/scripts/online-transfer-relational-sql-etl-fixture.sh stop',
        'bash business/scripts/online-transfer-relational-sql-etl-fixture.sh verify',
        '--suite transfer-relational-sql-etl', 'scripts/test/online-engine-registration.py',
        'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
        'export ADDP_ONLINE_TEST_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"',
        'HOSTED_FIXTURE_CONTAINERS=(addp-transfer-online-disposable addp-transfer-mongodb-online-disposable addp-transfer-doris-online-disposable addp-transfer-clickhouse-online-disposable addp-transfer-upload-online-disposable)',
        'ADDP_ONLINE_FIXTURE_MONGODB_ENGINE_DESCRIPTOR_FILE',
        'export ADDP_ONLINE_TEST_MONGODB_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"',
        'transfer-minio-engine.json',
        'ADDP_ONLINE_TRANSFER_MINIO_ENGINE_ID',
        'playwright install --with-deps chromium',
        'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
    ):
        if fragment not in hosted:
            raise RegistrationError(f"transfer-relational-sql-etl Hosted profile is missing {fragment}")
    host = (repository / "scripts/test/online-host-gate.sh").read_text(encoding="utf-8")
    if "transfer-relational-sql-etl" in host:
        raise RegistrationError("transfer-relational-sql-etl must not keep a self-hosted route")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
    job = re.search(r"(?ms)^  transfer-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
            or "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'transfer-relational-sql-etl'\n" not in job.group("body")
            or "ONLINE_SUITE_INPUT: transfer-relational-sql-etl" not in job.group("body")):
        raise RegistrationError("transfer-relational-sql-etl requires a manual Ubuntu Hosted job")
    if "&& inputs.suite != 'transfer-relational-sql-etl'" not in workflow:
        raise RegistrationError("transfer-relational-sql-etl must not also dispatch on self-hosted")
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    aggregate = re.search(r"(?ms)^test-online-runner:[^\n]*\n(?P<body>.*?)(?=^[^\t\n#]|\Z)", makefile)
    owner_gate = re.search(r"(?ms)^test-transfer-online-runner:[^\n]*\n(?P<body>(?:\t[^\n]*\n)+)", makefile)
    if (aggregate is None or "$(MAKE) test-transfer-online-runner" not in aggregate.group("body")
            or owner_gate is None or "scripts/test/online-hosted-transfer-gate_test.py" not in owner_gate.group("body")):
        raise RegistrationError("transfer-relational-sql-etl Hosted regression must enter test-online-runner")
    contracts = {
        "Makefile": ("go test ./internal/planner -run '^TestHostedWriteFailureTaskContract$$' -count=1",),
        "transfer/backend/internal/planner/online_write_failure_contract_test.go": ("TestHostedWriteFailureTaskContract", "ParseTableExportTaskSpec", "module.write_failure_task"),
        "scripts/test/transfer-relational-sql-etl-online.py": (
            '"manager.data_item.read"', '"manager.content.read"', '"meta.lineage.read"',
            "evolve_fixture", "verify_unproven_schema", "verify_historical_schema", "history_before_write", "history_after_write", "addp.transfer-relational-sql-etl-online/v14",
            "verify_scan_trace_visibility", "record_scan_trace_receipt", "scan-trace-visibility.json", "addp.scan-trace-visibility/v1", "parent_read_does_not_grant_scan_read", "peer_hidden_endpoints", "owner_visible_observations", "peer_denials",
            "run_object_upload", "validate_object_upload_facts", "manager-object-upload.json", "addp.manager-object-upload/v1", "single_source_facts_verified", "multi_physical_size_verified", "ADDP_ONLINE_TRANSFER_MINIO_ENGINE_ID",
            "record_manager_import_receipt", "manager-private-import.json", "addp.manager-private-import/v2", "validate_manager_import_browser", "metadata_wait_verified", "failure_ui_verified", "physical_geometry_verified", "user_inspect_rejected", "run_manager_import", "current_user_origin_verified", "automatic_scan_verified", "non_uploader_rejected", "ADDP_ONLINE_IMPORT_OUTSIDER_ACCESS_TOKEN", '"manager.data_item.create"',
            "run_wide_lineage", "validate_wide_graph", "WIDE_FIELDS", "manager_wide_field_graph_verified", "config_checks",
            "run_write_failures", "validate_write_failure_execution", "validate_write_failure_physical", "write_failure_details_verified", "ADDP_ONLINE_TRANSFER_WRITE_FAILURES",
            "run_multi_source_lineage", "validate_multi_source_execution", "multiple_origins_verified", "cte_join_union_verified",
            "run_orchestrated_lineage", "validate_orchestrated_child", "canonical_table_locator", "validate_query_facts", "wait_resource_chain", "cleanup_definitions", "three_hop_verified",
            "run_mongodb_lineage", "validate_mongodb_execution", "ADDP_ONLINE_TEST_MONGODB_ENGINE_ID", "automatic_collection_verified", "rerun_verified",
            "run_native_lineage", "wait_field_graph", "execution_schema_hashes", "validate_graph_snapshots", "generated_label", '"_replace"', '"_hop"',
            '"residual_resources": 0', "cleanup_tasks(client", "owned_task_names", "schema_snapshot_hash",
            "addp.transfer-relational-sql-etl-browser/v7",
        ),
        "business/scripts/online-transfer-relational-sql-etl-fixture.sh": (
            "object-facts", "start_minio", "addp.manager-object-physical/v1", "addp-transfer-upload-online-disposable",
            "ADDP_ONLINE_HOSTED", "GITHUB_ACTIONS", "NATIVE_TARGET", "NATIVE_DOWNSTREAM", "DROP TABLE IF EXISTS public.${NATIVE_TARGET}", "read-only configuration check created a target table",
            "DROP TABLE IF EXISTS public.${NATIVE_DOWNSTREAM}", "numeric_precision, numeric_scale", "generated_label",
            "WIDE_SOURCE", "WIDE_DOWNSTREAM", "generate_series(0,499)", "EXCEPT ALL", "wide target complete row multiset differs",
            "start_write_failure_targets", "verify_write_failures", "addp-transfer-doris-online-disposable", "addp-transfer-clickhouse-online-disposable", "enable_insert_strict=true", "PARTITION BY RANGE(id)", "CONSTRAINT fixture_label_length",
            "verify_manager_import", "manager-import", "MULTI_TARGET", "multi-source CTE/JOIN/UNION rows differ", "DROP TABLE IF EXISTS public.${MULTI_TARGET}",
            "DIM_TARGET", "DWD_TARGET", "DIM rows differ", "DWD rows differ",
            "RENAME COLUMN person_nickname TO person_display_name", "TYPE timestamp without time zone USING activity_date::timestamp", "evolved DWD columns differ",
            "MONGODB_TARGET", "mongo:7.0", "--tmpfs /data/db --tmpfs /data/configdb", "leader_nickname_snapshot",
            "container remains after cleanup", "container ownership mismatch", "--tmpfs /var/lib/postgresql/data",
        ),
        "console/frontend/e2e/online/transfer-relational-sql-etl.spec.js": (
            "addp.manager-private-import-browser/v1", "browserAPIFromResponse", "manager-import-console-success.png", "manager-import-console-rejection.png", "upload_ui_verified", "metadata_wait_verified", "failure_ui_verified",
            "/manager/data-explorer", "field_ref", "schema_snapshot_hash", "lineage-fields", "lineage-canvas", "lineageFieldConnections",
            "verifyTaskConfigChecks", "taskExecutionIdentities", "/api/v1/transfer/task-config-checks", "json_representation_not_proven", "constant_encoding_invalid", "forbiddenRequests", "config_checks: configChecks",
            "verifyWideLineage", "manager_wide_field_graph_verified: true", "expect(graph.nodes).toHaveLength(1500)", "expect(graph.edges).toHaveLength(1000)", "expect(requests).toBe(1)",
            "manager_evolved_schema_verified: true", "schema_evolution.history_verified", "expect(chain.rounds).toHaveLength(3)",
            "manager_orchestrated_field_graph_verified: true", "ADDP_ONLINE_ORCHESTRATED_FIELD_LINEAGE", "transfer-orchestrated-dwd-field-lineage.png",
            "verifyWriteFailureDetails", "write_failure_details_verified: writeFailureDetailsVerified", "ADDP_ONLINE_TRANSFER_WRITE_FAILURES",
            "manager_mongodb_field_graph_verified: true", "ADDP_ONLINE_TRANSFER_MONGODB_FIELD_LINEAGE", "transfer-mongodb-ods-field-lineage.png", "expect(mongodbGraphRequests).toBe(1)",
            "manager_field_graph_verified: true", "query_field_lineage_verified: true", "expect(query.graph.field_lineage_status).toBe('complete')", "expect(query.focused.edges).toHaveLength(1)",
            "addp.transfer-relational-sql-etl-browser/v7",
        ),
        "scripts/test/transfer-relational-sql-etl-online_test.py": ("test_readonly_config_checks_require_complete_proofs_and_zero_side_effects", "test_multi_source_frozen_evidence_rejects_partial_or_guessed_origins", "test_multi_source_scenario_preserves_inputs_parameters_and_exact_graph_origins", "test_rejects_wrong_fields_versions_and_execution_proofs", "test_mongodb_reruns_one_task_and_requires_all_latest_automatic_proofs", "test_chain_reruns_same_definition_and_only_observes_latest_owner_proofs"),
        "scripts/test/online-hosted-transfer-gate_test.py": ("test_failures_destroy_owned_resources", "ADDP_ONLINE_TRANSFER_CHECK_USER_USERNAME"),
        "system/backend/cmd/online-test-fixture/main.go": ("grantTransferConfigCheckEngineReader", "ADDP_ONLINE_TRANSFER_CHECK_USER_PASSWORD"),
        "system/backend/cmd/online-test-fixture/main_test.go": ("TestTransferPrecheckFixtureKeepsConsumerSeparateAndReissuesSession",),
        "scripts/test/online-transfer-relational-sql-etl-fixture_test.py": ("test_readonly_config_check_target_must_remain_absent", "test_rejects_incorrect_multi_source_query_rows", "test_rejects_native_rows", "test_partial_mongodb_initialization_is_redacted_and_cleans_both_containers"),
    }
    for relative, fragments in contracts.items():
        path = repository / relative
        if not path.is_file():
            raise RegistrationError(f"transfer-relational-sql-etl requires {relative}")
        content = path.read_text(encoding="utf-8")
        for fragment in fragments:
            if fragment not in content:
                raise RegistrationError(f"transfer-relational-sql-etl {relative} contract is missing {fragment}")


def load_workflow_suites(
    repository: Path, nightly_suites: set[str]
) -> set[str]:
    path = repository / ".github/workflows/online-t4-gates.yml"
    if not path.is_file():
        raise RegistrationError(f"{path.relative_to(repository)} is missing")
    text = path.read_text(encoding="utf-8")
    required_fragments = (
        "workflow_dispatch:",
        "permissions:\n  contents: read",
        "- self-hosted",
        "- macOS",
        "- addp-online",
        "environment: addp-online",
        "ADDP_ONLINE_ENV_FILE: ${{ vars.ADDP_ONLINE_ENV_FILE }}",
        "bash scripts/test/online-host-gate.sh --check-only",
        "bash scripts/test/online-host-gate.sh",
        "actions/upload-artifact@",
    )
    missing = [fragment for fragment in required_fragments if fragment not in text]
    if missing:
        raise RegistrationError("Online T4 workflow is missing: " + ", ".join(missing))
    jobs_text = text.split("\njobs:\n", 1)
    if len(jobs_text) != 2:
        raise RegistrationError("Online T4 workflow jobs are missing")
    job_blocks = re.findall(
        r"(?ms)^  ([a-z][a-z0-9-]*):\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)",
        jobs_text[1],
    )
    self_hosted_jobs = [
        (job_name, body)
        for job_name, body in job_blocks
        if re.search(r"(?m)^      - self-hosted\s*$", body)
    ]
    if not self_hosted_jobs:
        raise RegistrationError("Online T4 workflow has no self-hosted job")
    for job_name, body in self_hosted_jobs:
        accepts_manual_dispatch = "github.event_name == 'workflow_dispatch'" in body
        accepts_fixed_schedule = (
            "github.event_name == 'schedule'" in body
            and re.search(
                r"(?m)^      ONLINE_SUITE_INPUT: [a-z][a-z0-9-]*$", body
            )
            is not None
        )
        if not accepts_manual_dispatch and not accepts_fixed_schedule:
            raise RegistrationError(
                f"self-hosted Online T4 job {job_name} must only accept "
                "workflow_dispatch or a fixed schedule"
            )
        if "${{ secrets." in body:
            raise RegistrationError(
                f"self-hosted Online T4 job {job_name} must not receive repository secrets"
            )
        for action in re.findall(r"(?m)^\s*- uses:\s*([^\s]+)\s*$", body):
            if action.startswith("./"):
                continue
            _, separator, revision = action.rpartition("@")
            if not separator or re.fullmatch(r"[0-9a-f]{40}", revision) is None:
                raise RegistrationError(
                    f"self-hosted Online T4 job {job_name} action must use an immutable commit SHA: {action}"
                )
    hosted_gates = sorted(
        (repository / "scripts/test").glob("online-hosted-*-gate.sh")
    )
    owner_managed_gates = sorted(
        (repository / "scripts/test").glob("online-owner-managed-*-gate.sh")
    )
    for external_gate in hosted_gates + owner_managed_gates:
        relative = external_gate.relative_to(repository).as_posix()
        lifecycle_flag = (
            'ADDP_ONLINE_HOSTED: "1"'
            if external_gate in hosted_gates
            else 'ADDP_ONLINE_OWNER_MANAGED: "1"'
        )
        for fragment in (
            f"bash {relative} --check-only",
            f"bash {relative}",
            lifecycle_flag,
            "ADDP_ONLINE_SECRET_DIR:",
        ):
            if fragment not in text:
                raise RegistrationError(
                    f"Online T4 workflow is missing external profile fragment: {fragment}"
                )
    if (hosted_gates or owner_managed_gates) and "node-version-file: .node-version" not in text:
        raise RegistrationError(
            "Online T4 external profiles must use the repository .node-version"
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
    expected_artifact_assignments = 2 * (
        1 + len(hosted_gates) + len(owner_managed_gates)
    )
    if text.count(artifact_assignment) != expected_artifact_assignments:
        raise RegistrationError(
            "Online T4 workflow must configure the Runner temp artifact directory "
            "on both lifecycle steps for every Runner profile"
        )
    has_schedule = re.search(r"(?m)^  schedule:\s*$", text) is not None
    if has_schedule and not nightly_suites:
        raise RegistrationError(
            "Online T4 workflow must remain manual until the first real run passes"
        )
    if nightly_suites and not has_schedule:
        raise RegistrationError(
            "Online nightly suite registration requires a workflow schedule"
        )
    if has_schedule:
        cron_values = re.findall(
            r"(?m)^    - cron: [\"']([^\"']+)[\"']$", text
        )
        if len(cron_values) != 1 or re.fullmatch(
            r"(?:[0-5]?\d) (?:[01]?\d|2[0-3]) \* \* \*", cron_values[0]
        ) is None:
            raise RegistrationError(
                "Online T4 nightly schedule must contain exactly one daily UTC cron"
            )
        scheduled_workflow_suites: set[str] = set()
        for job_name, body in job_blocks:
            if "github.event_name == 'schedule'" in body:
                suite = re.search(
                    r"(?m)^      ONLINE_SUITE_INPUT: ([a-z][a-z0-9-]*)$", body
                )
                if suite is None:
                    raise RegistrationError(
                        f"scheduled Online T4 job {job_name} must set a fixed "
                        "ONLINE_SUITE_INPUT"
                    )
                suite_name = suite.group(1)
                if f"group: online-t4-{suite_name}" not in body:
                    raise RegistrationError(
                        f"scheduled Online T4 job {job_name} must use its fixed "
                        "suite concurrency group"
                    )
                scheduled_workflow_suites.add(suite_name)
            elif "github.event_name == 'workflow_dispatch'" not in body:
                raise RegistrationError(
                    f"Online T4 job {job_name} must explicitly exclude schedule events"
                )
        if scheduled_workflow_suites != nightly_suites:
            raise RegistrationError(
                "Online workflow nightly suites "
                f"{sorted(scheduled_workflow_suites)} do not match registered nightly "
                f"suites {sorted(nightly_suites)}"
            )
    options = re.search(
        r"(?m)^        options:\n(?P<body>(?:          - [a-z][a-z0-9-]*\n)+)",
        text,
    )
    if options is None:
        raise RegistrationError("Online T4 workflow suite choices are missing")
    return set(re.findall(r"(?m)^          - ([a-z][a-z0-9-]*)$", options.group("body")))


def validate_metric_engine_variant(repository: Path, registered: set[str]) -> None:
    if "metric-service-revision-lifecycle" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
    selection = re.search(
        r"(?ms)^      metric_engine:\n(?P<body>.*?)(?=^permissions:|\Z)", workflow
    )
    if selection is None:
        raise RegistrationError("metric Online T4 engine choice is missing")
    choice = selection.group("body")
    options = re.search(r"(?m)^        options:\n(?P<body>(?:          - [a-z]+\n)+)", choice)
    if ("        type: choice\n" not in choice
            or "        required: true\n" not in choice
            or "        default: postgresql\n" not in choice
            or options is None
            or re.findall(r"(?m)^          - ([a-z]+)$", options.group("body")) != ["postgresql", "tidb"]):
        raise RegistrationError("metric Online T4 must select PostgreSQL or TiDB explicitly")
    job = re.search(r"(?ms)^  metric-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None:
        raise RegistrationError("Hosted metric T4 job is missing")
    body = job.group("body")
    required = (
        "    if: github.event_name == 'schedule' || (github.event_name == 'workflow_dispatch' && inputs.suite == 'metric-service-revision-lifecycle')\n",
        "      fail-fast: false\n",
        "        metric_engine: ${{ fromJSON(github.event_name == 'schedule' && '[\"postgresql\",\"tidb\"]' || format('[\"{0}\"]', inputs.metric_engine)) }}\n",
        "      group: online-t4-metric-service-revision-lifecycle-${{ matrix.metric_engine }}\n",
        "      ADDP_ONLINE_METRIC_ENGINE_TYPE: ${{ matrix.metric_engine }}\n",
        "          name: online-t4-metric-service-revision-lifecycle-${{ matrix.metric_engine }}-${{ github.run_id }}\n",
    )
    if any(fragment not in body for fragment in required):
        raise RegistrationError("Hosted metric T4 must schedule both engines and dispatch only the selected engine")


def validate_platform_node_metrics_profile(repository: Path, registered: set[str]) -> None:
    if "platform-node-metrics" not in registered:
        return
    for relative in (
        "scripts/test/platform-node-metrics-online.py", "scripts/test/platform-node-metrics-online_test.py",
        "scripts/test/platform-node-metrics-fixture.py", "scripts/test/platform-node-metrics-fixture_test.py",
        "scripts/test/online-hosted-node-metrics-gate_test.py",
        "system/backend/cmd/online-test-fixture/platform_metrics.go",
        "system/backend/cmd/online-test-fixture/platform_metrics_test.go",
        "scripts/infra/node-metrics.py",
    ):
        if not (repository / relative).is_file():
            raise RegistrationError(f"platform-node-metrics requires {relative}")
    makefile = (repository / "Makefile").read_text()
    owned = re.search(r"(?ms)^test-node-metrics-online-runner:[^\n]*\n(?P<body>.*?)(?=^[^\t\n#]|\Z)", makefile)
    aggregate = re.search(r"(?ms)^test-online-runner:[^\n]*\n(?P<body>.*?)(?=^[^\t\n#]|\Z)", makefile)
    if owned is None or aggregate is None or "$(MAKE) test-node-metrics-online-runner" not in aggregate.group("body"):
        raise RegistrationError("platform-node-metrics requires its owner regression in test-online-runner")
    for filename in ("platform-node-metrics-online_test.py", "platform-node-metrics-fixture_test.py", "online-hosted-node-metrics-gate_test.py"):
        if filename not in owned.group("body"):
            raise RegistrationError(f"platform-node-metrics regression is missing {filename}")
    gate = (repository / "scripts/test/online-hosted-node-metrics-gate.sh").read_text()
    for fragment in ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', "scripts/infra/node-metrics.py up",
                     "scripts/infra/node-metrics.py down", "--suite platform-node-metrics", 'modules=(-monitor)',
                     'if [ "$ADDP_ONLINE_NODE_METRICS_CASE" = host-admission ] || [ "$ADDP_ONLINE_NODE_METRICS_CASE" = engine-admission ]; then modules+=(-system); fi',
                     'scripts/dev/start.sh "${modules[@]}"',
                     'make test-online "ONLINE_SUITE=$ONLINE_SUITE"'):
        if fragment not in gate:
            raise RegistrationError(f"platform-node-metrics lifecycle is missing {fragment}")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  node-metrics-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'platform-node-metrics'\n" not in job.group("body"):
        raise RegistrationError("platform-node-metrics requires a manual Hosted T4 job")
    if "&& inputs.suite != 'platform-node-metrics'" not in workflow:
        raise RegistrationError("platform-node-metrics must not dispatch on the self-hosted deployment")
    selection = re.search(r"(?ms)^      node_metrics_case:\n(?P<body>.*?)(?=^      [a-z_]+:\n|^permissions:|\Z)", workflow)
    choice = selection.group("body") if selection else ""
    options = re.search(r"(?m)^        options:\n(?P<body>(?:          - [a-z-]+\n)+)", choice)
    if ("        type: choice\n" not in choice or "        required: true\n" not in choice
            or "        default: regression\n" not in choice or options is None
            or re.findall(r"(?m)^          - ([a-z-]+)$", options.group("body")) != ["regression", "complete-comparison", "cpu-pressure", "host-admission", "engine-admission", "meta-workers", "transfer-workers", "quality-workers"]
            or "      ADDP_ONLINE_NODE_METRICS_CASE: ${{ inputs.node_metrics_case }}\n" not in job.group("body")
            or "regression|complete-comparison|cpu-pressure|host-admission|engine-admission|meta-workers|transfer-workers|quality-workers)" not in gate):
        raise RegistrationError("platform-node-metrics must select a closed, bounded acceptance case")


def validate_orchestrator_execution_profile(repository: Path, registered: set[str]) -> None:
    if "orchestrator-execution" not in registered:
        return
    gate = repository / "scripts/test/online-hosted-orchestrator-gate.sh"
    required_files = (
        "scripts/test/orchestrator-execution-online.py",
        "scripts/test/orchestrator-execution-online_test.py",
        "scripts/test/orchestrator-execution-faults.py",
        "scripts/test/orchestrator-execution-faults_test.py",
        "scripts/test/online-hosted-orchestrator-gate_test.py",
        "business/scripts/online-metric-postgres-fixture.sh",
    )
    for relative in required_files:
        if not (repository / relative).is_file():
            raise RegistrationError(f"orchestrator-execution requires {relative}")
    makefile = (repository / "Makefile").read_text(encoding="utf-8")
    owned = re.search(r"(?ms)^test-orchestrator-online-runner:[^\n]*\n(?P<body>.*?)(?=^[^\t\n#]|\Z)", makefile)
    aggregate = re.search(r"(?ms)^test-online-runner:[^\n]*\n(?P<body>.*?)(?=^[^\t\n#]|\Z)", makefile)
    if owned is None or aggregate is None or "$(MAKE) test-orchestrator-online-runner" not in aggregate.group("body"):
        raise RegistrationError("orchestrator-execution requires its owner T1 entry in test-online-runner")
    for test in ("orchestrator-execution-online_test.py", "orchestrator-execution-faults_test.py", "online-hosted-orchestrator-gate_test.py"):
        if test not in owned.group("body"):
            raise RegistrationError(f"orchestrator-execution owner T1 entry is missing {test}")
    text = gate.read_text(encoding="utf-8")
    for fragment in (
        'source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
        "for start_target in -meta -orchestrator -monitor",
        "bash business/scripts/online-metric-postgres-fixture.sh start",
        "bash business/scripts/online-metric-postgres-fixture.sh stop",
        "--suite orchestrator-execution",
        "orchestrator-execution-faults.py alias-add",
        "orchestrator-execution-faults.py alias-remove",
        'HTTP_PROXY="$ADDP_ONLINE_ORCHESTRATOR_PROXY_URL"',
        "SERVICE_HOST=addp-orchestrator-meta.test",
        "python3 scripts/test/online-engine-registration.py",
        'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
    ):
        if fragment not in text:
            raise RegistrationError(f"orchestrator-execution Hosted profile is missing {fragment}")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
    job = re.search(r"(?ms)^  orchestrator-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'orchestrator-execution'\n" not in job.group("body"):
        raise RegistrationError("orchestrator-execution must remain a manual Hosted T4 job before its first real pass")
    if "&& inputs.suite != 'orchestrator-execution'" not in workflow:
        raise RegistrationError("orchestrator-execution must not also dispatch on the self-hosted deployment")


def validate_quality_dynamic_binding_profile(repository: Path, registered: set[str]) -> None:
    if "quality-dynamic-binding" not in registered:
        return
    host = (repository / "scripts/test/online-host-gate.sh").read_text()
    if re.search(r"(?m)^  quality-dynamic-binding\)", host):
        raise RegistrationError("Quality must have one Hosted profile, without the removed self-hosted route")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  quality-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "runs-on: ubuntu-24.04" not in job.group("body") or (
        "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'quality-dynamic-binding'\n" not in job.group("body")
    ):
        raise RegistrationError("Quality requires a manual Ubuntu Hosted T4 job")
    if "&& inputs.suite != 'quality-dynamic-binding'" not in workflow:
        raise RegistrationError("Quality must not also dispatch on self-hosted")
    required = {
        "scripts/test/online-hosted-quality-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', "--suite quality-dynamic-binding",
            "-model -meta -quality -orchestrator -monitor", "online-quality-postgres-fixture.py stop",
            "online-quality-postgres-fixture.py seed", "online-quality-postgres-fixture.py verify",
            "-m scripts.test.quality-online-fixture prepare", "-m scripts.test.quality-online-fixture scan",
            "unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN", "unset ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN",
            'make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        ),
        "business/scripts/online-quality-postgres-fixture.py": (
            "container ownership mismatch", "container remains after cleanup", "source data changed",
            "type=tmpfs,destination=/var/lib/postgresql/data",
        ),
        "scripts/test/quality-online-fixture.py": ("/materialized-target", "different Users", "uncertain_mutation"),
        "system/backend/cmd/online-test-fixture/main.go": ('case "quality-dynamic-binding":', 'values["ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN"]'),
        "Makefile": ("$(MAKE) test-quality-online-runner", "scripts/test/quality-online-fixture_test.py",
                     "scripts/test/online-quality-postgres-fixture_test.py", "scripts/test/online-hosted-quality-gate_test.py",
                     "scripts/ci/check-quality-online-ci-registration_test.py"),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f"Quality Hosted owner contract is incomplete: {relative}")


def validate_vector_pmtiles_profile(repository: Path, registered: set[str]) -> None:
    if "vector-pmtiles-workflow" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  vector-pmtiles-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    suite = load_suite_registry(repository)["vector-pmtiles-workflow"]
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-pmtiles-workflow'" not in job.group("body")
        or "&& inputs.suite != 'vector-pmtiles-workflow'" not in workflow or suite.nightly
        or ("security", "SECURITY_URL") not in suite.services):
        raise RegistrationError("PMTiles requires a manual isolated Hosted job and Security preflight")
    required = {
        "scripts/test/online-hosted-vector-pmtiles-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite vector-pmtiles-workflow',
            '--wait-protection-owners develop,manager',
            'start_online_geopython_runtime', 'for start_target in -security -develop -manager -monitor',
            'business.scripts.online-vector-pmtiles-fixture stop', 'scripts/test/online-engine-registration.py',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'env -u POSTGRES_PASSWORD make test-online'),
        "scripts/utils/hosted-online.sh": ('stop_online_product_runtime || status=1',),
        "business/scripts/online-vector-pmtiles-fixture.py": (
            'shared.start_minio(', 'shared.owned(name, SUITE)', "com.addp.online-fixture=", 'validate_records(',
            "'vector_layers'", "header['min_zoom'] != 0", 'reader.get(5, 26, 13)', 'physical_worker('),
        "scripts/test/vector-pmtiles-workflow-online.py": (
            'validate_conflict(', 'before_scans != after_scans', 'before_graph != after_graph',
            "target['fingerprint'] != before_item['fingerprint']", "'meta_scan_runs'", 'wait_current_graph(',
            'validate_item(target, native)', 'browser_runner(repository, env, evidence)'),
        "console/frontend/e2e/online/vector-pmtiles-workflow.spec.js": (
            'login(', 'auth.principalID', "content.metadata.min_zoom).toBe(0)", "['cache-control']).toBe('no-store')",
            "value === 'no-store'", 'evidence.tile_sha256', 'evidence.feature_count', "getImageData("),
        "system/backend/cmd/online-test-fixture/main.go": ('case "vector-pmtiles-workflow":', 'return vectorPMTilesPermissions, nil'),
        "Makefile": ('@$(MAKE) test-vector-pmtiles-online-runner', 'test_online_pmtiles_fixture.py',
            'scripts/test/vector-pmtiles-workflow-online_test.py', 'scripts/test/online-vector-pmtiles-fixture_test.py',
            'scripts/test/online-hosted-vector-pmtiles-gate_test.py', 'test_pmtiles_requires_manual_isolation_content_and_cleanup'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f"PMTiles owner contract is incomplete: {relative}")



def validate_vector_topology_profile(repository: Path, registered: set[str]) -> None:
    if "vector-topology-persistence" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  topology-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-topology-persistence'" not in job.group("body")
        or "&& inputs.suite != 'vector-topology-persistence'" not in workflow
        or "nfs-kernel-server nfs-common" not in job.group("body")
        or load_suite_registry(repository)["vector-topology-persistence"].nightly):
        raise RegistrationError("topology requires a manual isolated Hosted NFS job")
    required = {
        "scripts/test/online-hosted-topology-gate.sh": ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
            'start_online_geopython_runtime', '--mount', '--suite vector-topology-persistence',
            '--wait-protection-owners develop,manager', 'online-vector-topology-fixture.py stop',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'playwright install --with-deps chromium'),
        "scripts/test/vector-topology-persistence-online.py": ('validate_identity', 'PERMISSIONS = raster.PERMISSIONS',
            'validate_success', 'validate_item', 'triggered_by', 'meta_scan_runs', 'browser_runner', 'physical_size_bytes', 'check_geometry', 'repair_geometry', 'after_valid', 'fixture.SOURCES', 'snap_geometry', 'check_polygon_overlap', 'check_polygon_gap', 'check_dangling_points', 'check_line_self_overlap', 'line_self_overlap_expected', 'line_self_overlap_input', 'check_line_self_intersection', 'line_self_intersection_expected', 'line_self_intersection_input', 'check_line_intersection', 'line_intersection_expected', 'line_intersection_input', 'check_line_overlap', 'line_overlap_expected', 'line_overlap_input', 'dangling_expected', 'dangling_input', 'gap_expected', 'gap_coverage', 'overlap_expected', 'post_browser_physical', 'validate_failed_snap', 'collapsed_snap_diagnostic', 'topology-snap-failure.json', 'scan_ids(client)', 'vector.validate_item', 'addp.vector-topology-persistence/v10'),
        "business/scripts/online-vector-topology-fixture.py": ('NFSExport', 'validate_features', 'expected_geometries', 'source file changed', 'validate_repaired', 'repaired.geojson', 'SNAP_EXPECTED', 'validate_snap_features', 'verify_snap', 'snap-input.gpkg', 'snap-reference.gpkg', 'snapped.gpkg', 'archive_physical_evidence', 'verify_overlap', 'overlap_expected_features', 'overlap-input.gpkg', 'overlaps.gpkg', 'verify_gap', 'gap_expected_features', 'gap-input.gpkg', 'gap-coverage.gpkg', 'gaps.gpkg', 'verify_line_self_overlap', 'line_self_overlap_expected_features', 'line-self-overlap-input.gpkg', 'line-self-overlaps.gpkg', 'verify_line_self_intersection', 'line_self_intersection_expected_features', 'line-self-intersection-input.gpkg', 'line-self-intersections.gpkg', 'verify_line_intersection', 'line_intersection_expected_features', 'line-intersection-input.gpkg', 'line-intersections.gpkg', 'verify_line_overlap', 'line_overlap_expected_features', 'line-overlap-input.gpkg', 'line-overlaps.gpkg', 'verify_dangling', 'dangling_expected_features', 'dangling-input.gpkg', 'dangling.gpkg'),
        "scripts/utils/online_nfs_fixture.py": ('root_squash', 'owned_root', 'nfs_export_removed'),
        "console/frontend/e2e/online/vector-topology-persistence.spec.js": ('login(', 'browserAPIFromResponse(', 'principalID: evidence.principal_id', 'previewResponse', 'source_srid', 'getImageData', 'evidence.execution_id', 'topology-browser.json', 'row.geometry_valid', 'row.after_valid', 'MultiPolygon', 'topology-source-lineage-raw.json', 'expect(current).toEqual(source.before)', 'decodePreviewEWKB', 'snapCoordinates', 'source_srid', 'failed_execution_id', 'addp.topology-browser/v10', 'graph, preview', 'line_self_overlap_expected', '[evidence.sources[9].id]', 'line_self_intersection_expected', '[evidence.sources[11].id]', 'line_intersection_expected', '[evidence.sources[10].id]', 'line_overlap_expected', 'overlap_length', '[evidence.sources[8].id]', 'dangling_expected', "source_id:feature.get('source_id')", "geometry.type).toBe('Point')", '[evidence.sources[7].id]', 'gap_expected', 'gapRings', 'gap_area', '[evidence.sources[5].id, evidence.sources[6].id]', 'overlap_expected', 'source_id_a', 'rendered', "getProjection().getCode() !== 'EPSG:3857'", 'toEqual(snapCoordinates.map(line => line.map(webMercatorPreviewCoordinate))'),
        "manager/frontend/e2e/geometry-preview-support.js": ('decodePreviewEWKB', 'zoomOutPreviewMap'),
        "manager/frontend/e2e/geometry-preview-coordinate.js": ('webMercatorPreviewCoordinate', '6378137', 'Math.atan(Math.exp(-y / radius))'),
        "manager/frontend/tests/previewGeometryWire.test.js": ('display projection round trip exactly', 'keeps display axis, displacement and vertex order differences observable'),
        "system/backend/cmd/online-test-fixture/main.go": ('case "vector-topology-persistence":', '"manager.data_item.read"'),
        "engines/geopython-workflow/test_topology_operators.py": ('test_hosted_topology_physical_oracle',),
        "Makefile": ('@$(MAKE) test-vector-topology-online-runner', 'scripts/test/vector-topology-persistence-online_test.py',
            'scripts/test/online-vector-topology-fixture_test.py', 'scripts/test/online-hosted-topology-gate_test.py',
            'test_topology_requires_manual_nfs_identity_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f"topology owner contract is incomplete: {relative}")

def validate_raster_workflow_profile(repository: Path, registered: set[str]) -> None:
    if "raster-workflow" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  raster-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "runs-on: ubuntu-24.04" not in job.group("body") or (
        "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-workflow'\n" not in job.group("body")
    ):
        raise RegistrationError("raster-workflow requires a manual Ubuntu Hosted job")
    if "&& inputs.suite != 'raster-workflow'" not in workflow:
        raise RegistrationError("raster-workflow must not also dispatch on self-hosted")
    if load_suite_registry(repository)["raster-workflow"].nightly:
        raise RegistrationError("raster-workflow requires real evidence before schedule registration")
    required = {
        "scripts/test/online-hosted-raster-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite raster-workflow',
            'scripts/test/online-engine-registration.py', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'env -u POSTGRES_PASSWORD make test-online', 'start_online_geopython_runtime',
            'RUNTIME_CONTAINER=addp-raster-online-runtime',
            'online-raster-minio-fixture.py stop',
        ),
        "scripts/utils/hosted-online.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-product-runtime.sh"',
            'com.addp.online-runtime', 'stop_online_product_runtime || status=1',
        ),
        "scripts/utils/hosted-product-runtime.sh": (
            'start_online_geopython_runtime', 'verify_online_product_runtime',
            '--services geopython-workflow-engine --tag $tag --verify --jobs 1',
            'Runtime image identity mismatch', 'Runtime default entry mismatch',
            'com.addp.online-runtime',
        ),
        "business/scripts/online-raster-minio-fixture.py": (
            "full_check=True", "type=tmpfs,destination=/data", "source_unchanged", "com.addp.online-fixture",
            "verify-mosaic-first", "verify-mosaic-last", "baseline_sha256", "first_sha256",
            "verify-analysis", "analysis_expectations", "last_sha256",
        ),
        "scripts/test/raster-workflow-online.py": (
            "meta_scan_runs", "wait_lineage", "verify-replace", "failed create modified", "browser_runner",
            "spatial_workflow", "raster_reproject", "raster_clip", "raster_mosaic", "spatial_cases",
            "analysis_workflow", "raster_statistics", "raster_histogram", "validate_analysis", "analysis_cases",
        ),
        "console/frontend/e2e/online/raster-workflow.spec.js": (
            "login(", ".execution-lineage", "auth.principalID", "edge.evidence?.execution_id",
            "evidence.source_name", "evidence.target_name", "evidence.case_name",
            "evidence.expected_result", ".workflow-final-result-json", "professional.metadata.result.final_result",
        ),
        "Makefile": ("test-raster-online-runner", "scripts/test/raster-workflow-online_test.py",
                     "scripts/test/online-raster-minio-fixture_test.py", "scripts/test/online-hosted-raster-gate_test.py"),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f"raster-workflow owner contract is incomplete: {relative}")


def validate_raster_vector_profile(repository: Path, registered: set[str], domain: str,
                                   source_name: str, oracle_test: str, physical_fragments: tuple[str, ...]) -> None:
    suite = f"raster-{domain}-persistence"
    if suite not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(rf"(?ms)^  raster-{domain}-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if (job is None or "runs-on: ubuntu-24.04" not in job.group("body")
        or f"if: github.event_name == 'workflow_dispatch' && inputs.suite == '{suite}'" not in job.group("body")
        or f"&& inputs.suite != '{suite}'" not in workflow
        or "nfs-kernel-server nfs-common" not in job.group("body")
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError(f"{domain} persistence requires a manual isolated Hosted NFS job")
    required = {
        f"scripts/test/online-hosted-raster-{domain}-gate.sh": ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
            'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
            '--wait-protection-owners develop,manager', f'online-raster-{domain}-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
        f"scripts/test/raster-{domain}-persistence-online.py": ('run_vector_scenario', f"source_name='{source_name}'"),
        "scripts/utils/online_vector_persistence.py": ('validate_identity', 'validate_success', 'validate_item',
            'wait_lineage', 'triggered_by', 'meta_scan_runs', 'physical_size_bytes'),
        f"business/scripts/online-raster-{domain}-fixture.py": ('owned_root', 'NFSExport', 'IsSame', 'runtime_workspaces_removed', *physical_fragments),
        "scripts/utils/online_nfs_fixture.py": ('root_squash', 'nfs_export_removed', 'owned_root'),
        "engines/geopython-workflow/test_online_raster_fixture.py": (oracle_test, f'test_{domain}_persistence_oracle_rejects_real_file_corruption'),
        "Makefile": (f'scripts/test/raster-{domain}-persistence-online_test.py',
            f'scripts/test/online-raster-{domain}-fixture_test.py', f'scripts/test/online-hosted-raster-{domain}-gate_test.py',
            f'test_{domain}_requires_manual_nfs_identity_and_registered_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f"{domain} persistence contract is incomplete: {relative}")


def validate_raster_contour_profile(repository: Path, registered: set[str]) -> None:
    validate_raster_vector_profile(repository, registered, 'contour', 'dem.tif',
        'test_contour_persistence_real_gdal_independent_plane_and_both_save_modes', ('GetPoint',))


def validate_raster_polygonize_profile(repository: Path, registered: set[str]) -> None:
    validate_raster_vector_profile(repository, registered, 'polygonize', 'classes.tif',
        'test_polygonize_persistence_real_gdal_all_regions_and_three_chains',
        ('expected_regions', 'region.equals', 'OFTInteger64'))


def validate_vector_rasterize_profile(repository: Path, registered: set[str]) -> None:
    suite = 'vector-rasterize-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  vector-rasterize-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-rasterize-persistence'" not in job.group('body')
        or "&& inputs.suite != 'vector-rasterize-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('rasterize requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-vector-rasterize-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
            'start_online_geopython_runtime', '--mount', '--suite raster-workflow',
            '--wait-protection-owners develop,manager', 'online-vector-rasterize-fixture.py stop',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/vector-rasterize-persistence-online.py': ('raster.validate_identity', 'raster.EDITOR_PERMISSIONS',
            'fixture.SOURCES', 'triggered_by', "resource_type='file'", 'raster.strict_sizes', 'raster.wait_lineage',
            'raster.validate_target_item', 'scan_ids(client)!=before_scans', 'check_physical(native,previous)'),
        'business/scripts/online-vector-rasterize-fixture.py': ('NFSExport', 'owned_root', 'IsSame',
            'expected_pixels', 'full_check=True', 'runtime_workspaces_removed', 'source file changed'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_raster_fixture.py': (
            'test_rasterize_persistence_real_gdal_three_cog_chains_and_reference_independence',
            'test_rasterize_persistence_oracle_rejects_real_file_corruption'),
        'Makefile': ('@$(MAKE) test-vector-rasterize-online-runner', 'scripts/test/vector-rasterize-persistence-online_test.py',
            'scripts/test/online-vector-rasterize-fixture_test.py', 'scripts/test/online-hosted-vector-rasterize-gate_test.py',
            'test_rasterize_requires_manual_nfs_identity_strict_sizes_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'rasterize persistence contract is incomplete: {relative}')


def validate_vector_zonal_statistics_profile(repository: Path, registered: set[str]) -> None:
    suite = 'vector-zonal-statistics-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  vector-zonal-statistics-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'vector-zonal-statistics-persistence'" not in job.group('body')
        or "&& inputs.suite != 'vector-zonal-statistics-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('vector-zone statistics requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-vector-zonal-statistics-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-vector-zonal-statistics-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/vector-zonal-statistics-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'triggered_by',
            'validate_item', 'used_sources(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', "for problem in ('band','collision')", 'complete_geometry_attributes_statistics_verified'),
        'business/scripts/online-vector-zonal-statistics-fixture.py': (
            'NFSExport', 'owned_root', 'expected_rows', 'statistics.pstdev', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source files changed', 'shapely.intersection', 'Integer64'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_raster_fixture.py': (
            'test_vector_zonal_persistence_real_gdal_three_gpkg_chains_and_full_content',
            'test_vector_zonal_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-vector-zonal-statistics-online-runner',
            'scripts/test/vector-zonal-statistics-persistence-online_test.py',
            'scripts/test/online-vector-zonal-statistics-fixture_test.py',
            'scripts/test/online-hosted-vector-zonal-statistics-gate_test.py',
            'test_vector_zonal_requires_manual_nfs_complete_gpkg_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'vector-zone statistics persistence contract is incomplete: {relative}')


def validate_raster_proximity_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-proximity-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-proximity-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-proximity-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-proximity-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('proximity requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-proximity-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-proximity-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-proximity-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'triggered_by',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', "for problem in ('band','targets')", 'full_pixel_and_mask_verification'),
        'business/scripts/online-raster-proximity-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'np.hypot', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_raster_fixture.py': (
            'test_proximity_persistence_real_gdal_two_cog_chains_and_complete_distances',
            'test_proximity_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-proximity-online-runner',
            'scripts/test/raster-proximity-persistence-online_test.py',
            'scripts/test/online-raster-proximity-fixture_test.py',
            'scripts/test/online-hosted-raster-proximity-gate_test.py',
            'test_proximity_requires_manual_nfs_identity_exact_distances_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'proximity persistence contract is incomplete: {relative}')


def validate_raster_focal_mode_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-mode-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-focal-mode-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-focal-mode-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-focal-mode-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal mode requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-focal-mode-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-focal-mode-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-focal-mode-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'triggered_by',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', "for problem in ('band','fractional','overflow')", 'full_pixel_and_mask_verification', 'exact_integer_pixels_verified'),
        'business/scripts/online-raster-focal-mode-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'Counter', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_focal_mode_fixture.py': (
            'test_focal_mode_persistence_real_gdal_three_cog_chains_and_complete_pixels',
            'test_focal_mode_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-focal-mode-online-runner',
            'test_online_focal_mode_fixture.py',
            'scripts/test/raster-focal-mode-persistence-online_test.py',
            'scripts/test/online-raster-focal-mode-fixture_test.py',
            'scripts/test/online-hosted-raster-focal-mode-gate_test.py',
            'test_focal_mode_requires_manual_nfs_exact_integer_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'focal mode persistence contract is incomplete: {relative}')


def validate_raster_boundary_clean_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-boundary-clean-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-boundary-clean-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-boundary-clean-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-boundary-clean-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('boundary clean requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-boundary-clean-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-boundary-clean-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-boundary-clean-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in ('band','fractional','overflow')", 'full_pixel_and_mask_verification', 'exact_integer_pixels_verified'),
        'business/scripts/online-raster-boundary-clean-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'Counter', 'empty-clean.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_boundary_clean_fixture.py': (
            'test_boundary_clean_persistence_real_gdal_three_cog_chains_and_complete_pixels',
            'test_boundary_clean_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-boundary-clean-online-runner',
            'test_online_boundary_clean_fixture.py',
            'scripts/test/raster-boundary-clean-persistence-online_test.py',
            'scripts/test/online-raster-boundary-clean-fixture_test.py',
            'scripts/test/online-hosted-raster-boundary-clean-gate_test.py',
            'test_boundary_clean_requires_manual_nfs_exact_integer_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'boundary clean persistence contract is incomplete: {relative}')


def validate_raster_thin_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-thin-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-thin-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-thin-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-thin-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('thinning requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-thin-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-thin-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-thin-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'exact_integer_pixels_verified', 'binary_skeleton_verified', 'invalid_diagnostic', 'foreground_value'),
        'business/scripts/online-raster-thin-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'adjacent_a', 'empty-thin.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_thin_fixture.py': (
            'test_thin_persistence_real_gdal_three_cog_chains_and_complete_pixels',
            'test_thin_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-thin-online-runner',
            'test_online_thin_fixture.py',
            'scripts/test/raster-thin-persistence-online_test.py',
            'scripts/test/online-raster-thin-fixture_test.py',
            'scripts/test/online-hosted-raster-thin-gate_test.py',
            'test_thin_requires_manual_nfs_exact_integer_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'thinning persistence contract is incomplete: {relative}')

def validate_raster_fill_depressions_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-fill-depressions-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-fill-depressions-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-fill-depressions-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-fill-depressions-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('DEM filling requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-fill-depressions-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-fill-depressions-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-fill-depressions-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'unit_metres_verified', 'depression_fill_verified', 'open_outlets_verified', 'never_lowered_verified', 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-fill-depressions-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'raw_filled_pixels', 'empty-filled.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_fill_depressions_fixture.py': (
            'test_fill_depressions_persistence_real_gdal_three_cog_chains_and_complete_pixels',
            'test_fill_depressions_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-fill-depressions-online-runner',
            'test_online_fill_depressions_fixture.py',
            'scripts/test/raster-fill-depressions-persistence-online_test.py',
            'scripts/test/online-raster-fill-depressions-fixture_test.py',
            'scripts/test/online-hosted-raster-fill-depressions-gate_test.py',
            'test_fill_depressions_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'DEM filling persistence contract is incomplete: {relative}')


def validate_raster_flow_direction_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-flow-direction-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-flow-direction-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-flow-direction-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-flow-direction-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('DEM D8 flow direction requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-flow-direction-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-flow-direction-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-flow-direction-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'direction_unit_cleared', 'direction_codes_verified', 'shortest_flat_paths_verified', 'acyclic_verified', 'final_open_outlets_verified', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-flow-direction-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'routing_evidence', 'empty-direction.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_flow_direction_d8_fixture.py': (
            'test_flow_direction_d8_persistence_real_gdal_three_cog_chains_and_complete_pixels',
            'test_flow_direction_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-flow-direction-d8-online-runner',
            'test_online_flow_direction_d8_fixture.py',
            'scripts/test/raster-flow-direction-d8-persistence-online_test.py',
            'scripts/test/online-raster-flow-direction-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-flow-direction-d8-gate_test.py',
            'test_flow_direction_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'DEM D8 flow direction persistence contract is incomplete: {relative}')


def validate_raster_flow_accumulation_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-flow-accumulation-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-flow-accumulation-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-flow-accumulation-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-flow-accumulation-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 flow accumulation requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-flow-accumulation-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-flow-accumulation-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-flow-accumulation-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'count_unit_cleared', 'counts_exclude_self', 'terminal_conservation_verified', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-flow-accumulation-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'conservation_evidence', 'empty-accumulation.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_flow_accumulation_d8_fixture.py': (
            'test_flow_accumulation_d8_persistence_real_gdal_four_cog_chains_and_complete_pixels',
            'test_flow_accumulation_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-flow-accumulation-d8-online-runner',
            'test_online_flow_accumulation_d8_fixture.py',
            'scripts/test/raster-flow-accumulation-d8-persistence-online_test.py',
            'scripts/test/online-raster-flow-accumulation-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-flow-accumulation-d8-gate_test.py',
            'test_flow_accumulation_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'D8 flow accumulation persistence contract is incomplete: {relative}')


def validate_raster_drainage_basins_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-drainage-basins-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-drainage-basins-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-drainage-basins-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-drainage-basins-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 drainage basins requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-drainage-basins-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-drainage-basins-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-drainage-basins-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'basin_conservation_verified', 'basin_unit_cleared', 'unique_membership_verified', 'outlet_order_verified', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-drainage-basins-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'basin_evidence', 'empty-basins.cog.tif', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_drainage_basins_d8_fixture.py': (
            'test_drainage_basins_d8_persistence_real_gdal_four_cog_chains_and_complete_pixels',
            'test_drainage_basins_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-drainage-basins-d8-online-runner',
            'test_online_drainage_basins_d8_fixture.py',
            'scripts/test/raster-drainage-basins-d8-persistence-online_test.py',
            'scripts/test/online-raster-drainage-basins-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-drainage-basins-d8-gate_test.py',
            'test_drainage_basins_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'D8 drainage basins persistence contract is incomplete: {relative}')


def validate_raster_flow_length_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-flow-length-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-flow-length-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-flow-length-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-flow-length-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 downstream flow length requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-flow-length-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-flow-length-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-flow-length-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'metre_unit_verified', 'terminals_zero', 'centre_steps_verified', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-flow-length-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'distance_evidence', 'empty-length.cog.tif', 'feet-length.cog.tif', 'survey-feet-length.cog.tif', 'SOURCE_UNITS', 'np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_flow_length_d8_fixture.py': (
            'test_flow_length_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels',
            'test_flow_length_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-flow-length-d8-online-runner',
            'test_online_flow_length_d8_fixture.py',
            'scripts/test/raster-flow-length-d8-persistence-online_test.py',
            'scripts/test/online-raster-flow-length-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-flow-length-d8-gate_test.py',
            'test_flow_length_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'D8 downstream flow length persistence contract is incomplete: {relative}')


def validate_raster_flow_length_upstream_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-flow-length-upstream-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-flow-length-upstream-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-flow-length-upstream-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-flow-length-upstream-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 longest upstream flow length requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-flow-length-upstream-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-flow-length-upstream-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-flow-length-upstream-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'metre_unit_verified', 'sources_zero', 'longest_path_verified', 'confluence_max_verified', 'outlets_receive_upstream', 'centre_steps_verified', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-flow-length-upstream-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'distance_evidence', 'empty-length.cog.tif', 'feet-length.cog.tif', 'survey-feet-length.cog.tif', 'SOURCE_UNITS', 'np.allclose(actual,expected,rtol=RTOL,atol=ATOL,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_flow_length_upstream_d8_fixture.py': (
            'test_flow_length_upstream_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels',
            'test_flow_length_upstream_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-flow-length-upstream-d8-online-runner',
            'test_online_flow_length_upstream_d8_fixture.py',
            'scripts/test/raster-flow-length-upstream-d8-persistence-online_test.py',
            'scripts/test/online-raster-flow-length-upstream-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-flow-length-upstream-d8-gate_test.py',
            'test_flow_length_upstream_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'D8 longest upstream flow length persistence contract is incomplete: {relative}')

def validate_raster_stream_order_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-stream-order-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-stream-order-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-stream-order-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-stream-order-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 Strahler stream order requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-stream-order-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-stream-order-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-stream-order-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'strahler_verified', 'stream_closure_verified', 'confluence_verified', 'zero_background_verified', 'case_inputs(name)', 'stream_raster', 'stream_band', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-stream-order-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'backward_oracle', 'order_evidence', 'empty-order.cog.tif', 'invalid-order.cog.tif', 'binary_values', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_stream_order_d8_fixture.py': (
            'test_stream_order_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels',
            'test_stream_order_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-stream-order-d8-online-runner',
            'test_online_stream_order_d8_fixture.py',
            'scripts/test/raster-stream-order-d8-persistence-online_test.py',
            'scripts/test/online-raster-stream-order-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-stream-order-d8-gate_test.py',
            'test_stream_order_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'D8 Strahler stream order persistence contract is incomplete: {relative}')


def validate_raster_flow_accumulation_weighted_d8_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-flow-accumulation-weighted-d8-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-flow-accumulation-weighted-d8-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-flow-accumulation-weighted-d8-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-flow-accumulation-weighted-d8-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('D8 weighted flow accumulation requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-flow-accumulation-weighted-d8-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-flow-accumulation-weighted-d8-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-flow-accumulation-weighted-d8-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'scan_identity',
            'raster.strict_sizes', 'raster.validate_target_item', 'fixture.artifact_expectation(name)', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', 'validate_scan(scanned,scan,identifier', 'source_edges(client', 'for name in fixture.TARGETS', "for problem in FAILURES", 'full_pixel_and_mask_verification', 'weighted_sum_verified', 'self_exclusion_verified', 'mass_balance_verified', 'valid_zero_verified', 'weight_unit_verified', 'all_ones_equivalent_verified', 'case_inputs(name)', 'weights_raster', 'weights_band', 'SubmittedLanguageClient', "for language in ('zh-cn','en')", 'invalid_diagnostic', 'elevation_unit'),
        'business/scripts/online-raster-flow-accumulation-weighted-d8-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'walk_oracle', 'weighted_evidence', 'ones.cog.tif', 'invalid-weighted.cog.tif', 'weight_values', 'np.array_equal(actual,expected,equal_nan=True)', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_flow_accumulation_weighted_d8_fixture.py': (
            'test_flow_accumulation_weighted_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels',
            'test_flow_accumulation_weighted_d8_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-flow-accumulation-weighted-d8-online-runner',
            'test_online_flow_accumulation_weighted_d8_fixture.py',
            'scripts/test/raster-flow-accumulation-weighted-d8-persistence-online_test.py',
            'scripts/test/online-raster-flow-accumulation-weighted-d8-fixture_test.py',
            'scripts/test/online-hosted-raster-flow-accumulation-weighted-d8-gate_test.py',
            'test_flow_accumulation_weighted_d8_requires_manual_nfs_full_pixels_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'D8 weighted flow accumulation persistence contract is incomplete: {relative}')




def validate_raster_band_recommendation_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-band-recommendation-workflow'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-band-recommendation-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-band-recommendation-workflow'" not in job.group('body')
        or "&& inputs.suite != 'raster-band-recommendation-workflow'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('band-recommendation requires a manual isolated Hosted NFS job')
    required = {'scripts/test/online-hosted-raster-band-recommendation-gate.sh': ('source '
                                                                   '"$ROOT_DIR/scripts/utils/hosted-online.sh"',
                                                                   'start_online_geopython_runtime',
                                                                   '--mount',
                                                                   '--suite raster-workflow',
                                                                   '--wait-protection-owners develop,manager',
                                                                   'online-raster-band-recommendation-fixture.py '
                                                                   'stop',
                                                                   'unset '
                                                                   'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
                                                                   'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
 'scripts/test/raster-band-recommendation-workflow-online.py': ('raster.validate_identity',
                                                                'raster.EDITOR_PERMISSIONS',
                                                                'raster.transient_result',
                                                                'execution_snapshot',
                                                                'no_publication',
                                                                'actor',
                                                                'scan_ids(client) != baseline_scans',
                                                                'complete_object_verified'),
 'business/scripts/online-raster-band-recommendation-fixture.py': ('NFSExport',
                                                                   'owned_root',
                                                                   'expected_result',
                                                                   'np.corrcoef',
                                                                   'np.ascontiguousarray',
                                                                   'no_outputs',
                                                                   'runtime_workspaces_removed',
                                                                   'full_pixels_and_mask_verified'),
 'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
 'engines/geopython-workflow/test_online_band_recommendation_fixture.py': ('test_band_recommendation_fixture_complete_sources_and_full_product_object',
                                                                           'test_band_recommendation_fixture_object_comparison_is_complete_and_strict'),
 'Makefile': ('@$(MAKE) test-raster-band-recommendation-online-runner',
              'test_online_band_recommendation_fixture.py',
              'scripts/test/raster-band-recommendation-workflow-online_test.py',
              'scripts/test/online-raster-band-recommendation-fixture_test.py',
              'scripts/test/online-hosted-raster-band-recommendation-gate_test.py',
              'test_band_recommendation_requires_manual_nfs_readonly_oif_and_regressions')}
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'band-recommendation persistence contract is incomplete: {relative}')


def validate_raster_nibble_capacity_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-nibble-capacity'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-nibble-capacity-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-nibble-capacity'" not in job.group('body')
        or "&& inputs.suite != 'raster-nibble-capacity'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or 'timeout-minutes: 240' not in job.group('body') or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('nibble capacity requires an independent manual Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-nibble-capacity-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite raster-nibble-capacity',
            '--wait-protection-owners develop,manager', '--cpus 4 --memory 2g --memory-swap 2g',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=10800', 'TMPDIR=', '--start-runtime',
            'online-raster-nibble-capacity-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
        'scripts/test/raster-nibble-capacity-online.py': (
            'raster.EDITOR_PERMISSIONS', 'capacity.cgroup_directory', 'capacity.validate_measurement',
            'fixture.require_disk', 'validate_disk', 'raster.strict_sizes', 'raster.wait_lineage',
            'triggered_by', 'scan_ids(client)', 'policy_restored', 'previous_artifacts_unchanged',
            'private_allocated_peak_bytes', 'runtime_container_id'),
        'system/backend/cmd/online-test-fixture/main.go': (
            'case "raster-workflow", "raster-nibble-capacity":',
            'suite == "raster-workflow-capacity" || suite == "raster-nibble-capacity"'),
        'system/backend/cmd/online-test-fixture/main_test.go': (
            'TestCapacityFixtureSessionLastsBeyondTheThreeHourBudget',
            'TestNibbleCapacityRetainsExactlyTheOrdinaryRasterPermissions'),
        'business/scripts/online-raster-nibble-capacity-fixture.py': (
            'NFSExport', 'SIZES = (4096, 10980)', "SPACING = {'sparse': 128, 'dense': 2}",
            'expected_block', 'running*size**2*64', 'full_check=True',
            'capacity complete pixel values differ', 'capacity complete mask differs',
            'previous capacity artifact changed', 'capacity source file changed'),
        'engines/geopython-workflow/test_online_nibble_capacity_fixture.py': (
            'test_capacity_analytical_oracle_matches_original_donor_bruteforce',
            'test_nibble_capacity_real_gdal_complete_pixels_and_previous_immutable',
            'test_nibble_capacity_oracle_rejects_corrupt_or_previous_output'),
        'Makefile': ('@$(MAKE) test-raster-nibble-capacity-online-runner',
            'test_online_nibble_capacity_fixture.py', 'scripts/test/raster-nibble-capacity-online_test.py',
            'scripts/test/online-raster-nibble-capacity-fixture_test.py',
            'scripts/test/online-hosted-raster-nibble-capacity-gate_test.py',
            'test_nibble_capacity_requires_manual_limits_disk_and_full_oracle')}
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'nibble capacity contract incomplete: {relative}')


def validate_raster_disk_pressure_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-disk-pressure'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-disk-pressure-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "inputs.suite == 'raster-disk-pressure'" not in job.group('body')
        or "inputs.suite != 'raster-disk-pressure'" not in workflow
        or 'nfs-kernel-server nfs-common e2fsprogs util-linux' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('disk pressure requires manual isolated Hosted Linux ext4/NFS')
    required = {
        'scripts/test/online-hosted-raster-disk-pressure-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime',
            '--cpus 4 --memory 2g --memory-swap 2g', 'TMPDIR=/raster-pressure-tmp',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600', 'online-raster-disk-pressure-fixture.py stop'),
        'business/scripts/online-raster-disk-pressure-fixture.py': (
            'NFSExport', 'posix_fallocate', 'mkfs.ext4', 'nodiscard', 'losetup', 'findmnt', 'umount',
            'refusing foreign loop cleanup', 'loop_detached', 'image_removed',
            'np.array_equal(band.ReadAsArray(), expected, equal_nan=True)', 'full_check=True'),
        'scripts/test/raster-disk-pressure-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'verify_failure',
            'SubmittedLanguageClient', 'Accept-Language', 'capacity.validate_scan', 'raster.strict_sizes',
            'raster.wait_lineage', 'source_edges(client', 'source metadata changed',
            'product Runtime or master/worker restarted', 'pending_bytes', 'safety_bytes', 'workflow_sha256',
            'exact rejected workflow/target'),
        'engines/geopython-workflow/test_online_disk_pressure_fixture.py': (
            'test_pressure_native_exact_phase_rejection_and_same_process_recovery',
            'test_pressure_oracle_rejects_corrupt_or_extra_publications'),
        'Makefile': ('test_online_disk_pressure_fixture.py', '@$(MAKE) test-raster-disk-pressure-online-runner',
            'scripts/test/raster-disk-pressure-online_test.py', 'scripts/test/online-raster-disk-pressure-fixture_test.py',
            'scripts/test/online-hosted-raster-disk-pressure-gate_test.py')}
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError('disk pressure strict contract incomplete: '+relative)


def validate_raster_nibble_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-nibble-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-nibble-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-nibble-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-nibble-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('nibble requires a manual isolated Hosted NFS job')
    required = {'scripts/test/online-hosted-raster-nibble-gate.sh': ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
                                                      'start_online_geopython_runtime',
                                                      '--mount',
                                                      '--suite raster-workflow',
                                                      '--wait-protection-owners develop,manager',
                                                      'online-raster-nibble-fixture.py stop',
                                                      'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
                                                      'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
 'scripts/test/raster-nibble-persistence-online.py': ('raster.validate_identity',
                                                      'raster.EDITOR_PERMISSIONS',
                                                      'fixture.SOURCES',
                                                      'triggered_by',
                                                      'raster.strict_sizes',
                                                      'raster.validate_target_item',
                                                      'raster.wait_lineage',
                                                      'scan_ids(client)!=before_scans',
                                                      'rasterize.check_physical(native,previous)',
                                                      "for problem in ('band','unsafe','no-donor')",
                                                      'full_pixel_and_mask_verification',
                                                      'exact_integer_pixels_verified'),
 'business/scripts/online-raster-nibble-fixture.py': ('NFSExport',
                                                      'owned_root',
                                                      'expected_pixels',
                                                      'squared<best',
                                                      'np.array_equal(actual,expected,equal_nan=True)',
                                                      'GDAL_TIFF_INTERNAL_MASK',
                                                      'runtime_workspaces_removed',
                                                      'source file changed',
                                                      'full_check=True'),
 'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
 'engines/geopython-workflow/test_online_nibble_fixture.py': ('test_nibble_persistence_real_gdal_three_cog_chains_and_complete_pixels',
                                                              'test_nibble_persistence_oracle_rejects_corruption'),
 'Makefile': ('@$(MAKE) test-raster-nibble-online-runner',
              'test_online_nibble_fixture.py',
              'scripts/test/raster-nibble-persistence-online_test.py',
              'scripts/test/online-raster-nibble-fixture_test.py',
              'scripts/test/online-hosted-raster-nibble-gate_test.py',
              'test_nibble_requires_manual_nfs_exact_donors_and_regressions')}
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'nibble persistence contract is incomplete: {relative}')


def validate_raster_focal_statistics_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-focal-statistics-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-focal-statistics-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-focal-statistics-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-focal-statistics-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('focal statistics requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-focal-statistics-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-focal-statistics-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-focal-statistics-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'triggered_by',
            'raster.strict_sizes', 'raster.validate_target_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', "for problem in ('band','window')", 'full_pixel_and_mask_verification'),
        'business/scripts/online-raster-focal-statistics-fixture.py': (
            'NFSExport', 'owned_root', 'expected_pixels', 'sliding_window_view', 'GDAL_TIFF_INTERNAL_MASK',
            'runtime_workspaces_removed', 'source file changed', 'full_check=True'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_raster_fixture.py': (
            'test_focal_persistence_real_gdal_six_cog_chains_and_complete_pixels',
            'test_focal_persistence_oracle_rejects_corruption'),
        'Makefile': ('@$(MAKE) test-raster-focal-statistics-online-runner',
            'scripts/test/raster-focal-statistics-persistence-online_test.py',
            'scripts/test/online-raster-focal-statistics-fixture_test.py',
            'scripts/test/online-hosted-raster-focal-statistics-gate_test.py',
            'test_focal_requires_manual_nfs_identity_strict_sizes_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'focal statistics persistence contract is incomplete: {relative}')


def validate_raster_zonal_statistics_profile(repository: Path, registered: set[str]) -> None:
    suite = 'raster-zonal-statistics-persistence'
    if suite not in registered:
        return
    workflow = (repository / '.github/workflows/online-t4-gates.yml').read_text()
    job = re.search(r'(?ms)^  raster-zonal-statistics-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)', workflow)
    if (job is None or 'runs-on: ubuntu-24.04' not in job.group('body')
        or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-zonal-statistics-persistence'" not in job.group('body')
        or "&& inputs.suite != 'raster-zonal-statistics-persistence'" not in workflow
        or 'nfs-kernel-server nfs-common' not in job.group('body')
        or load_suite_registry(repository)[suite].nightly):
        raise RegistrationError('zonal statistics requires a manual isolated Hosted NFS job')
    required = {
        'scripts/test/online-hosted-raster-zonal-statistics-gate.sh': (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', 'start_online_geopython_runtime', '--mount',
            '--suite raster-workflow', '--wait-protection-owners develop,manager',
            'online-raster-zonal-statistics-fixture.py stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=600'),
        'scripts/test/raster-zonal-statistics-persistence-online.py': (
            'raster.validate_identity', 'raster.EDITOR_PERMISSIONS', 'fixture.SOURCES', 'triggered_by',
            'validate_preview', 'validate_item', 'raster.wait_lineage', 'scan_ids(client)!=before_scans',
            'rasterize.check_physical(native,previous)', "for problem in ('band','grid')"),
        'business/scripts/online-raster-zonal-statistics-fixture.py': (
            'NFSExport', 'owned_root', 'expected_rows', 'validate_records', 'csv.DictReader',
            'GDAL_TIFF_INTERNAL_MASK', 'runtime_workspaces_removed', 'source file changed'),
        'scripts/utils/online_nfs_fixture.py': ('root_squash', 'nfs_export_removed', 'owned_root'),
        'engines/geopython-workflow/test_online_raster_fixture.py': (
            'test_zonal_persistence_real_gdal_four_csv_chains_and_independent_full_tables',
            'test_zonal_persistence_oracle_rejects_csv_corruption'),
        'Makefile': ('@$(MAKE) test-raster-zonal-statistics-online-runner',
            'scripts/test/raster-zonal-statistics-persistence-online_test.py',
            'scripts/test/online-raster-zonal-statistics-fixture_test.py',
            'scripts/test/online-hosted-raster-zonal-statistics-gate_test.py',
            'test_zonal_requires_manual_nfs_identity_complete_csv_and_regressions'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'zonal statistics persistence contract is incomplete: {relative}')


def validate_raster_capacity_profile(repository: Path, registered: set[str]) -> None:
    if "raster-workflow-capacity" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  raster-capacity-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "runs-on: ubuntu-24.04" not in job.group("body") or (
        "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'raster-workflow-capacity'\n" not in job.group("body")
    ) or "&& inputs.suite != 'raster-workflow-capacity'" not in workflow:
        raise RegistrationError("raster capacity requires its own manual Ubuntu Hosted job")
    if load_suite_registry(repository)["raster-workflow-capacity"].nightly:
        raise RegistrationError("raster capacity requires real evidence before schedule registration")
    required = {
        "scripts/test/raster-workflow-online.py": (
            "def strict_sizes(", "type(value) is not int", "len(set(sizes)) != 1", "size_evidence",
        ),
        "scripts/test/online-hosted-raster-capacity-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite raster-workflow-capacity',
            '--cpus 4 --memory 2g --memory-swap 2g', 'ADDP_ONLINE_TEST_TIMEOUT_SECONDS=10800',
            'online-raster-capacity-fixture.py stop', 'online-raster-capacity-fixture.py seed',
            'scripts/test/online-engine-registration.py', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'env -u POSTGRES_PASSWORD make test-online', 'launch_online_geopython_runtime',
        ),
        "scripts/utils/hosted-product-runtime.sh": (
            'com.addp.online-run-id', 'Runtime default entry mismatch', 'Runtime image identity mismatch',
        ),
        "scripts/test/raster-workflow-capacity-online.py": (
            'CACHE_VALUES=(64,128,256)', 'REPETITIONS=2', 'validate_measurement', 'wait_applied',
            'def run_directories(', 'def directory_matrix(', 'manager.derived_artifact.create',
            'manager.derived_artifact.read', 'mosaic_groups', 'submitted_tasks=1', 'automatic_scans=28',
            "physical('verify-mosaic',[name])", "physical('purge-mosaic',[name])",
            "validate_monitor_lineage(monitor,facts)",
            'from scripts.utils import capacity_resources', 'capacity_resources.cgroup_directory',
            'capacity_resources.read_metrics', 'scene.strict_sizes(artifact,actual,item)',
            "physical('purge',names)", 'capacity-progress.json', 'wait_lineage', "'runtime_container_id'",
        ),
        "scripts/utils/capacity_resources.py": (
            'def cgroup_directory(', 'def read_metrics(', 'memory.peak', 'oom_kill',
            'memory.swap.max', 'quota/period!=4', 'maximum!=2*(1<<30)',
        ),
        "business/scripts/online-raster-capacity-fixture.py": (
            'S2A_17SPV_20220304_0_L2A', 'SIZE = 10980', 'data_directory=root/',
            'public asset checksum changed', 'full_check=True', 'expected_rows',
            'capacity source object bytes changed', 'capacity target purge has residuals',
            'def verify_mosaic_directory(', 'def verify_mosaic_raster(', 'def mosaic_names_checked(',
            'capacity mosaic exact seven-object inventory differs', 'capacity mosaic source bytes changed',
        ),
        "system/backend/cmd/online-test-fixture/main.go": (
            'case "raster-workflow-capacity":',
            'return append(append([]string{}, rasterWorkflowPermissions...), "manager.derived_artifact.create", "manager.derived_artifact.read"), nil',
        ),
        "engines/geopython-workflow/test_online_raster_fixture.py": (
            'test_capacity_directory_all_bands_global_base_grid_and_full_pyramids',
            'test_capacity_directory_oracle_rejects_corrupt_components_and_scope',
        ),
        "Makefile": ('scripts/test/raster-workflow-capacity-online_test.py',
                     'scripts/test/online-raster-capacity-fixture_test.py',
                     'scripts/test/online-hosted-raster-capacity-gate_test.py'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f"raster capacity owner contract is incomplete: {relative}")


def validate_hdfs_spark_profile(repository: Path, registered: set[str]) -> None:
    if "hdfs-spark-consumer-flow" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  hdfs-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "runs-on: ubuntu-24.04" not in job.group("body") or (
        "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'hdfs-spark-consumer-flow'\n" not in job.group("body")
    ) or "actions/setup-java@" not in job.group("body") or "java-version: '11'" not in job.group("body"):
        raise RegistrationError("HDFS requires a manual Ubuntu Hosted job with Java 11")
    if "&& inputs.suite != 'hdfs-spark-consumer-flow'" not in workflow:
        raise RegistrationError("HDFS must not dispatch on self-hosted")
    suite = load_suite_registry(repository)["hdfs-spark-consumer-flow"]
    if suite.nightly:
        raise RegistrationError("HDFS requires real evidence before schedule registration")
    if ("security", "SECURITY_URL") not in suite.services:
        raise RegistrationError("HDFS requires Security in the same-source service preflight")
    required = {
        "scripts/test/online-hosted-hdfs-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite hdfs-spark-consumer-flow',
            'start_online_spark_runtime online-hdfs hdfs', 'RUNTIME_IMAGE', 'RUNTIME_CONTAINER',
            'scripts/test/online-engine-registration.py',
            '-security', '--wait-protection-owners develop,manager', 'SECURITY_URL=http://127.0.0.1:8194',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN', 'SPARK_MODE override is forbidden',
            'run_logged make test-online', 'online-hdfs-spark-fixture.sh stop', 'ADDP_ONLINE_MINIO_ENGINE_ID',
            'ADDP_ONLINE_MYSQL_ENGINE_ID', 'ADDP_ONLINE_DORIS_ENGINE_ID', 'ADDP_ONLINE_TEST_RUN_ID',
        ),
        "scripts/utils/hosted-online.sh": (
            "make build-images", 'source "$ROOT_DIR/scripts/utils/hosted-product-runtime.sh"',
            "${RUNTIME_REPORT}-runtime.log",
            "com.addp.online-runtime", "stop_online_product_runtime || status=1",
        ),
        "scripts/utils/hosted-product-runtime.sh": (
            "Runtime image identity mismatch", "Runtime default entry mismatch", "${RUNTIME_REPORT}-runtime-build.txt",
        ),
        "business/scripts/online-hdfs-spark-fixture.sh": (
            'com.addp.online-fixture', '--network host', '--tmpfs /data', '/addp/hdfs/init.py',
            'refusing to delete a foreign container', 'fixture containers remain', 'addp-hdfs-online-minio', 'minio-engine.json',
            'addp-hdfs-online-mysql', 'addp-hdfs-online-doris', 'common-spark-table-gate.sh', '/addp/doris/init.sh',
        ),
        "scripts/test/spark-online-evidence.py": (
            'Finished task', 'ADDP-Workflow-Engine-', 'com.addp.online-fixture',
            '/api/executions/', 'range(8)', 'all_results',
        ),
        "scripts/test/hdfs-spark-consumer-flow-online.py": (
            '/api/v1/develop/executions', 'spark_cluster_id', 'SPARK.worker_evidence', 'SPARK.runtime_status_evidence',
            'e2e/online/hdfs-spark-consumer-flow.spec.js', 'evidence != expected', 'minio_physical', 'minio_second',
            'TABLES.prepare', 'TABLES.run', 'TABLES.cleanup', "expected['tables']",
        ),
        "scripts/test/spark-table-consumer-online.py": (
            'physical_before', 'physical_after', 'validate_fields', 'validate_rows', 'runtime_status_evidence',
            'meta_scan_runs', 'spark-table-cleanup.json', 'table domain cleanup unverified',
        ),
        "business/scripts/online-spark-table-oracle.py": (
            'physical oracle refuses a foreign container', 'INNODB_TABLES', "SHOW PROC '/dbs'", 'target_absent',
            'staging evidence', 'ddl_sha256', 'MYSQL_PWD',
        ),
        "console/frontend/e2e/online/hdfs-spark-consumer-flow.spec.js": (
            'login(', 'auth.principalID', 'meta/scan/run/manual', '.workflow-final-result-json',
            "getByRole('dialog'", 'Confirm and start scan', 'postDataJSON()', "catalog_paths: ['samples']",
            'The selected scope has been scanned',
            'facts.physical_after.rows', "['created', 'overwrite', 'reuse']", "getAttribute('class')",
        ),
        "Makefile": ('$(MAKE) test-hdfs-online-runner', 'scripts/test/hdfs-spark-consumer-flow-online_test.py',
                     'scripts/test/online-hdfs-spark-fixture_test.py', 'scripts/test/online-hosted-hdfs-gate_test.py',
                     'scripts/test/spark-table-consumer-online_test.py'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f"HDFS owner contract is incomplete: {relative}")


def validate_elasticsearch_spark_profile(repository: Path, registered: set[str]) -> None:
    if 'elasticsearch-consumer-flow' not in registered:
        return
    required = {
        'scripts/test/online-hosted-elasticsearch-gate.sh': (
            'start_online_spark_runtime online-elasticsearch elasticsearch', 'ADDP_ONLINE_SPARK_ENGINE_ID', 'SPARK_MODE override is forbidden',
            'start_online_spark_image_registry', 'stop_online_spark_image_registry',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
        ),
        'scripts/utils/hosted-online.sh': (
            'make build-images', '--services spark-workflow-engine --tag $tag --verify --jobs 1',
            '--network host', '-e WORKFLOW_BIND_HOST=127.0.0.1',
            'source "$ROOT_DIR/scripts/utils/hosted-product-runtime.sh"', '${RUNTIME_REPORT}-runtime.log',
            'com.addp.online-runtime', 'stop_online_product_runtime || status=1',
        ),
        'scripts/utils/hosted-product-runtime.sh': (
            'Runtime image identity mismatch', 'Runtime default entry mismatch', '${RUNTIME_REPORT}-runtime-build.txt',
        ),
        'scripts/test/elasticsearch-consumer-flow-online.py': (
            'SPARK.worker_evidence', 'SPARK.runtime_status_evidence', 'spark_cluster_id', 'validate_workflow_nodes',
        ),
        'scripts/test/spark-online-evidence.py': ('Finished task', 'range(8)', 'all_results'),
        'console/frontend/e2e/online/elasticsearch-consumer-flow.spec.js': (
            '.workflow-final-result-json', 'expected.spark.execution_id', 'spark_workflow_result',
        ),
        'Makefile': ('$(MAKE) test-elasticsearch-online-runner', 'scripts/test/elasticsearch-consumer-flow-online_test.py',
                     'scripts/test/online-hosted-elasticsearch-gate_test.py', 'scripts/test/online-elasticsearch-consumer-fixture_test.py'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f'ES Spark owner contract is incomplete: {relative}')


def validate_public_origin_browser_profile(repository: Path, registered: set[str]) -> None:
    if "compose-public-origin" not in registered:
        return
    required = {
        "scripts/test/online-hosted-public-origin-gate.sh": (
            "npm --prefix console/frontend ci",
            "playwright install --with-deps chromium",
            "up -d --no-deps --wait security-backend",
            "--wait-protection-owners manager,transfer",
            'run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        ),
        "scripts/test/compose-public-origin-online.py": (
            "e2e/online/compose-public-origin.spec.js", "--config=playwright.online.config.js", "browser = run_browser()",
        ),
        "console/frontend/e2e/online/compose-public-origin.spec.js": (
            "ADDP_ONLINE_READ_USER_PASSWORD", "action: 'hold-lock'", "action: 'queue-refresh'",
            "requests.length - initialRefreshes", "ADDP_ONLINE_PUBLIC_ORIGIN_BROWSER_REPORT",
        ),
        "system/backend/cmd/online-test-fixture/main.go": (
            'values["ADDP_ONLINE_READ_USER_USERNAME"]', 'values["ADDP_ONLINE_READ_USER_PASSWORD"]',
        ),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f"compose-public-origin browser contract is incomplete: {relative}")


def validate_pg_authorization_profile(repository: Path, registered: set[str]) -> None:
    if "pg-source-authorization" not in registered:
        return
    suite = load_suite_registry(repository)["pg-source-authorization"]
    if suite.nightly or {name for name, _ in suite.services} != {"gateway", "system", "meta", "security", "manager", "catalog", "transfer"}:
        raise RegistrationError("PG authorization requires its owners including Security and manual first acceptance")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  pg-authorization-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "runs-on: ubuntu-24.04" not in job.group("body") or (
        "    if: github.event_name == 'workflow_dispatch' && inputs.suite == 'pg-source-authorization'\n" not in job.group("body")
    ) or "&& inputs.suite != 'pg-source-authorization'" not in workflow:
        raise RegistrationError("PG authorization must use its single manual Hosted job")
    required = {
        "scripts/test/online-hosted-pg-authorization-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite pg-source-authorization',
            'playwright install --with-deps chromium', 'export CONSOLE_URL=',
            'export CATALOG_URL=', 'export CATALOG_SERVICE_CLIENT_SECRET=', 'export SECURITY_URL=',
            '-system -meta -security -manager -catalog -transfer',
            'export TRANSFER_SERVICE_CLIENT_SECRET=', 'export META_SERVICE_CLIENT_SECRET=',
            'online-metric-postgres-fixture.sh stop', 'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),
        "scripts/test/pg-source-authorization-online.py": (
            "'scan_depth': 'basic'", "'force': True", 'lifecycle.cleanup()', 'self.expiry_grant',
            "state='expired'", "self.reader.token == reader_before", 'with BrowserObservation(', 'browser.finish()',
            'pg-source-authorization.spec.js', 'self.meta_recovery_flow(', "with DependencyOutage('meta')",
            'deny_survives_meta_recovery_without_rescan', "'authorization_service_unavailable'",
            'lifecycle.catalog_flow(', 'catalog_confirmation_alone_denied',
            'catalog_replay_does_not_restore_revoked_grant', "with DependencyOutage('catalog')",
            'catalog_offline_no_direct_approval_fallback', 'catalog_recovery_preserves_original_rules',
            'self.approval_mode_flow(', 'approval_switch_preserves_business_grant',
            'old_version_new_catalog_request_closed_without_upgrade', 'self.content_change_flow(',
            'mutate_source_content(', 'deny_survives_content_change_and_basic_rescan',
            'content_change_and_replay_do_not_restore_withdrawn_grant',
            'lifecycle.workspace_flow(', 'with WorkspaceObservation(', 'pg-authorization-workspace.json',
            'browser withdrawal did not preserve its exact Grant history',
            'lifecycle.production_flow(', "'auto_scan_metadata': False", 'self.collect_production_grants()',
            'new_pg_output_initial_grant_issued', 'output_survives_source_withdrawal',
            'output_withdrawal_denies_read'),
        "business/scripts/online-metric-postgres-fixture.sh": (
            'content-change|content-reset)', 'pg-source-authorization', 'container ownership mismatch',
            'LOCK TABLE public.metric_people', 'pg-content:changed', 'pg-content:baseline'),
        "console/frontend/e2e/online/pg-source-authorization.spec.js": (
            'sessionSnapshot', 'same_iframe_without_relogin', 'denied_preview_clears_old_rows',
            'isAnonymousRefreshResponse', 'beforeCookie.value !== afterCookie.value',
            'exclusive_workspace_tabs', 'integrated_create_picker_without_write',
            'table_text_and_tree_filters', 'recipient_type_and_account_filters',
            'browser_revocation_updates_current_and_history', 'browser_revocation_denies_manager_read',
            'expect(writes).toEqual([base +', 'reader.fingerprint'),
        "Makefile": ('test-pg-authorization-online-runner', 'pg-source-authorization-online_test.py',
                     'online-hosted-pg-authorization-gate_test.py', '@$(MAKE) test-pg-authorization-online-runner',
                     'scripts/test/online-hosted-pg-authorization-gate.sh business/scripts/online-metric-postgres-fixture.sh',
                     '@node --check --input-type=module < console/frontend/e2e/online/pg-source-authorization.spec.js'),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file() or any(fragment not in path.read_text() for fragment in fragments):
            raise RegistrationError(f"PG authorization owner contract is incomplete: {relative}")


def validate_highgo_profile(repository: Path, registered: set[str]) -> None:
    if "highgo-consumer-flow" not in registered:
        return
    requirements = {
        "scripts/test/online-hosted-highgo-gate.sh": (
            'source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
            'business/scripts/online-highgo-consumer-fixture.py start',
            'business/scripts/online-highgo-consumer-fixture.py verify',
            'business/scripts/online-highgo-consumer-fixture.py stop',
            '--suite highgo-consumer-flow', 'scripts/test/online-engine-registration.py',
            'unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',
            'run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"',
        ),
        "business/scripts/online-highgo-consumer-fixture.py": (
            "'GITHUB_ACTIONS': 'true'", "'ADDP_ONLINE_HOSTED': '1'",
            "highgo_image()", "/addp/highgo/entrypoint.sh", "business/highgo/init/init.sql",
            "'127.0.0.1::5866'", "'com.addp.online-fixture'", "'rm', '-fv'",
            "'volume', 'inspect'", "source == target", "'SENTINEL'",
        ),
        "scripts/test/highgo-consumer-flow-online.py": (
            'initialize_exact_record_read_grants', 'API.validate_user_identity',
            "actual == PERMISSIONS", "'replace_sentinel', 'repeat_replace'",
            'FLOW.assert_transfer_counts', 'SUPPORT.cleanup_tasks', 'run_browser(report)',
        ),
        "console/frontend/e2e/online/highgo-consumer-flow.spec.js": (
            "frameLocator('iframe[data-testid=\"module-iframe\"]')",
            'current.principalID', 'attrs.capabilities.constraints.constraints',
            "['updated_at', 'id']", 'highgo-replace-console.png',
        ),
        "Makefile": ('test-highgo-online-runner:', '$(MAKE) test-highgo-online-runner'),
        "scripts/test/highgo-consumer-flow-online_test.py": (),
        "scripts/test/online-highgo-consumer-fixture_test.py": (),
        "scripts/test/online-hosted-highgo-gate_test.py": (),
    }
    for relative, fragments in requirements.items():
        path = repository / relative
        if not path.is_file():
            raise RegistrationError(f"highgo-consumer-flow requires {relative}")
        text = path.read_text(encoding="utf-8")
        for fragment in fragments:
            if fragment not in text:
                raise RegistrationError(f"highgo-consumer-flow {relative} is missing {fragment}")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text(encoding="utf-8")
    job = re.search(r"(?ms)^  highgo-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if not job or 'runs-on: ubuntu-24.04' not in job['body'] or "github.event_name == 'workflow_dispatch' && inputs.suite == 'highgo-consumer-flow'" not in job['body']:
        raise RegistrationError("highgo-consumer-flow requires a manual Linux x86_64 Hosted job")
    if "inputs.suite != 'highgo-consumer-flow'" not in workflow:
        raise RegistrationError("highgo-consumer-flow must not also run on self-hosted")


def validate_local_execution_trace_profile(repository: Path) -> None:
    """The formal-identity local technical gate is never a GitHub dispatch."""
    path=repository / "scripts/test/monitor-traces-local-gate.py"
    make=(repository / "Makefile").read_text() if (repository / "Makefile").is_file() else ""
    if not path.exists() and "test-monitor-traces-local:" not in make:
        return
    required=("test-monitor-traces-local:", "@python3 scripts/test/monitor-traces-local-gate.py",
              "test-monitor-traces-local-runner:", "@$(MAKE) test-monitor-traces-local-runner",
              "scripts/test/monitor-traces-local-gate_test.py", "scripts/test/monitor-traces-local-online_test.py")
    if not path.is_file() or any(value not in make for value in required):
        raise RegistrationError("local execution Trace requires its manual entry and existing lifecycle runner")
    for relative in ("scripts/test/monitor-traces-local-online.py", "console/frontend/e2e/online/execution-trace-local.spec.js",
                     "system/backend/cmd/online-test-fixture/execution_trace_local.go"):
        if not (repository / relative).is_file():
            raise RegistrationError("local execution Trace is missing its formal-identity owner inputs")
    for workflow in (repository / ".github/workflows").glob("*.y*ml"):
        if re.search(r"(?:make\s+test-monitor-traces-local(?![A-Za-z0-9_-])|python3?\s+scripts/test/monitor-traces-local-gate.py)", workflow.read_text()):
            raise RegistrationError("local execution Trace must not execute in GitHub Actions")


def validate_local_sgm_profile(repository: Path) -> None:
    """The approved technical page gate must never become a public T4 choice."""
    make_path = repository / "Makefile"
    make = make_path.read_text() if make_path.is_file() else ""
    script = repository / "scripts/test/manager-sgm-local-gate.py"
    if "test-manager-sgm-local:" not in make and not script.exists():
        return
    if not script.is_file() or "@python3 scripts/test/manager-sgm-local-gate.py" not in make:
        raise RegistrationError("local SGM requires its manual Make entry")
    if ("@python3 -m unittest scripts/test/manager-sgm-local-gate_test.py" not in make
            or "@$(MAKE) test-manager-sgm-local-runner" not in make):
        raise RegistrationError("local SGM lifecycle tests must join the existing Manager runner")
    text = script.read_text()
    for fragment in ('"local-isolated-technical-page-acceptance"', '"formal_t4": False',
                     '"com.addp.sgm-local"', '"--no-hardlinks"', 'life.cleanup()', 'preflight()',
                     'MANAGER.cleanup_managed_artifact(', '"--env-file", "/dev/null"'):
        if fragment not in text:
            raise RegistrationError("local SGM lifecycle is missing " + fragment)
    for workflow in (repository / ".github/workflows").glob("*.y*ml"):
        if re.search(r"(?:make\s+test-manager-sgm-local(?![A-Za-z0-9_-])|python3?\s+scripts/test/manager-sgm-local-gate.py)", workflow.read_text()):
            raise RegistrationError("private local SGM must not execute in GitHub Actions")


def validate_graph_protection_profile(repository: Path, registered: set[str]) -> None:
    if "graph-protection" not in registered:
        return
    required = {
        "scripts/test/online-hosted-security-gate.sh": (
            "# ADDP_ONLINE_SUITES=graph-protection", "ADDP_ONLINE_GRAPH_ENGINE", "graph-control.json", "graph-review.json", "online-graph-fixture.py start",
            "online-graph-fixture.py stop", "develop,graph,manager,service,transfer", "graph-target.json",
            "docker-compose.graph-protection-t4.yml", "compose_graph down --remove-orphans --volumes",
            ',console,graph-frontend,develop-frontend,service-frontend,nginx', 'CONSOLE_URL="$ADDP_PUBLIC_ORIGIN"',
        ),
        "scripts/test/docker-compose.graph-protection-t4.yml": ("services:", "graph-review-extractor:", "graph-review-extractor.py:ro"),
        "scripts/test/graph-review-online.py": ("verify_selection", "graph-review-stable-selection.json", "verify_items", "reader_403=True", "cross_graph_404=True", "domain_resources_remaining", "real_model=False"),
        "scripts/test/graph-review-extractor.py": ("ADDP_ONLINE_HOSTED", "MATERIAL", "bad-native"),
        "console/frontend/e2e/online/graph-review.spec.js": ("addp.graph-review-browser/v2", "browserAPIFromResponse", "login(", "batch_status: 500", "selection_cleared: true", "selection_stable_visible: true", "stableEmptyReviewSelection)", "graph-review-stable-selection.json"),
        "console/frontend/e2e/graph-visual.fixture.js": ("stableGraphPixels", "denseGraphStages", "stableRelationshipPixels", "fillPixelCount", "getImageData", "stableFrames", "graphNodeHits", "exploreGraphLayouts", "clickGraphNode", "graphObservationColor", "stableEmptyReviewSelection", "getAnimations", "emptyBackground", "emptyTick"),
        "graph/frontend/Dockerfile": ("COPY graph/frontend/nginx.conf /etc/nginx/conf.d/default.conf",),
        "graph/frontend/nginx.conf": ("try_files $uri $uri/ /index.html;", "try_files $uri =404;"),
        "scripts/test/graph-protection-online.py": (
            "addp.graph-protection-online/v11", "verify_dense", "relationship_pixels", "run_dense", "graph-dense-stable-frame.json", "verify_search_frame", "verify_exploration", "verify_native_edges", "native_path_edges(reader, target, ids)", "verify_pixel_frame", "graph-exploration-stable-frame.json", "graph-search-stable-frame.json", "run_review", "public_unmanaged", "public_protected",
            "same_engine_control", "separate_engine_control", "target_paths", "private_protected_anonymous", "domain_resources_remaining", "wait_owners(",
            'report["browser"] = run_browser(', 'e2e/online/graph-protection.spec.js',
            'report["develop_unmanaged"] = run_develop_browser(', 'report["develop_protected"] = run_develop_browser(',
            'e2e/online/develop-graph-query.spec.js',
            'report["service_graph_unmanaged"] = run_service_graph_phase(',
            'report["service_graph_protected"] = run_service_graph_phase(',
            'verify_service_graph_denial', 'delete synthetic Service graph definition',
        ),
        "console/frontend/e2e/online/graph-protection.spec.js": (
            "addp.graph-protection-browser/v7", "browserAPIFromResponse", "login(",
            'addp.service-graph-browser/v1', 'api.post(definition.endpoints.execute',
            'dto_address_unchanged: true', 'public_https_verified: false',
            "browse_snapshot: true", "knowledge_search: true", "public_switch_disabled: true", "graph_writes: 0",
            "release_assets: true", "deep_link_reload: true", "theme_canvas_pixels: true",
            "graph-browser-diagnostics.json", "graphBrowserDiagnostic(", "search_stable_visible: true", "search_node_hits: hits.length", "expansion_stable_visible: true", "nativeNodes, expected.native_edges,", "count: found.nodes.length, color, fillColor", "layout_stable_visible: true", "graph-exploration-stable-frame.json", "exploreGraphLayouts(", "graphNodeHits(", "stableGraphPixels", "graph-search-stable-frame.json", "theme_typography: true", "graphDocumentHasThemeTypography)",
        ),
        "console/frontend/e2e/online/develop-graph-query.spec.js": (
            "addp.develop-graph-query-browser/v1", "browserAPIFromResponse", "login(",
            "typed_parameters: true", "graph_table_visible: true", "protected_result_absent: true", "control_readable: true",
            "develop.query.protection_required", "unexpected_writes: 0", "theme_canvas_pixels: true", "graphCanvasHasThemePixels)",
        ),
        "business/scripts/online-graph-fixture.py": (
            "--volumes", "owned_resources", "must not publish host ports", "0o600", "graph_protection_control", "def exploration(", "DENSE_EDGES",
        ),
        "business/docker-compose.online-graph.yml": (
            "../scripts/infra/falkordb.yml", "../scripts/test/docker-compose.neo4j-t2.yml", "ports: !reset []", "name: addp-network", "tmpfs:",
        ),
    }
    for path, fragments in required.items():
        file = repository / path
        if not file.is_file() or any(fragment not in file.read_text() for fragment in fragments):
            raise RegistrationError("Graph native protection contract is incomplete: " + path)
    # The host preflight must reach every service declared by the suite, even
    # though normal browser traffic only enters through production Nginx.
    lifecycle = (repository / "scripts/test/online-hosted-security-gate.sh").read_text()
    environment = (repository / "scripts/utils/hosted-online.sh").read_text() + "\n" + lifecycle
    overlay = (repository / "scripts/test/docker-compose.graph-protection-t4.yml").read_text()
    suite = load_suite_registry(repository)["graph-protection"]
    for owner, variable in suite.services:
        ports = re.findall(rf"\b{re.escape(variable)}=http://127\.0\.0\.1:(\d+)\b", environment)
        if not ports:
            raise RegistrationError(f"Graph preflight requires an explicit loopback address: {variable}")
        port = ports[-1]
        service = "gateway" if owner == "gateway" else owner + "-backend"
        block = compose_service_block(overlay, service) or ""
        bindings = re.findall(r'''["']([^"']+:\d+:\d+)["']''', block)
        if bindings != [f"127.0.0.1:{port}:{port}"]:
            raise RegistrationError(f"Graph preflight {variable} requires exactly one loopback diagnostic binding on {service}: {port}")
    commands = make_target_commands((repository / "Makefile").read_text(), "test-online-runner")
    for test in ("graph-review-online_test.py", "graph-protection-online_test.py", "online-graph-fixture_test.py"):
        if test not in commands:
            raise RegistrationError("Graph protection regression must enter test-online-runner")
    if "console/frontend/e2e/online/graph-review.spec.js" not in commands:
        raise RegistrationError("Graph review browser syntax must enter test-online-runner")
    if "console/frontend/e2e/online/graph-protection.spec.js" not in commands:
        raise RegistrationError("Graph Console browser syntax must enter test-online-runner")
    if "console/frontend/e2e/online/develop-graph-query.spec.js" not in commands:
        raise RegistrationError("Develop native browser syntax must enter test-online-runner")
    if 'if [ "$ONLINE_SUITE" != graph-protection ]; then' in lifecycle or "playwright install --with-deps chromium" not in lifecycle:
        raise RegistrationError("Graph Console requires Chromium in the same Hosted lifecycle")
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  security-hosted-t4:\n(?P<body>.*?)(?=^  [a-zA-Z0-9_-]+:\n|\Z)", workflow)
    if job is None or "inputs.suite == 'graph-protection'" not in job.group("body") or "runs-on: ubuntu-24.04" not in job.group("body"):
        raise RegistrationError("Graph protection requires the standard Ubuntu Hosted Security lifecycle")
    expected_group = "group: online-t4-${{ inputs.suite }}${{ inputs.suite == 'graph-protection' && format('-{0}', inputs.graph_engine) || '' }}"
    if re.findall(r"(?m)^      (group: .+)$", job.group("body")) != [expected_group] or not re.search(r"(?m)^      cancel-in-progress: false$", job.group("body")):
        raise RegistrationError("Graph engine runs require distinct engine concurrency groups without cancelling active runs")
    selector = re.search(r"(?ms)^      graph_engine:\n(?P<body>.*?)(?=^      [a-zA-Z0-9_-]+:\n|\Z)", workflow)
    if selector is None or not all(value in selector.group("body") for value in ("type: choice", "- falkordb", "- neo4j")) or "ADDP_ONLINE_GRAPH_ENGINE: ${{ inputs.graph_engine }}" not in job.group("body"):
        raise RegistrationError("Graph protection must explicitly select and propagate its native engine")
    if "&& inputs.suite != 'graph-protection'" not in workflow:
        raise RegistrationError("Graph protection must not also dispatch on self-hosted")


def validate_administrator_governance_profile(repository: Path, registered: set[str]) -> None:
    if "platform-administrator-governance" not in registered:
        return
    required = (
        "scripts/test/platform-administrator-governance-online.py",
        "scripts/test/platform-administrator-governance-online_test.py",
        "scripts/test/online-hosted-administrator-governance-gate_test.py",
        "system/backend/cmd/online-test-fixture/administrator_governance.go",
        "system/backend/cmd/online-test-fixture/administrator_governance_test.go",
        "system/backend/cmd/iam-recovery/main.go",
        "system/backend/cmd/iam-bootstrap/main.go",
        "console/frontend/e2e/online/platform-administrator-governance.spec.js",
    )
    for path in required:
        if not (repository / path).is_file():
            raise RegistrationError("administrator governance requires " + path)
    commands = make_target_commands((repository / "Makefile").read_text(), "test-online-runner")
    for fragment in ("platform-administrator-governance-online_test.py", "online-hosted-administrator-governance-gate_test.py",
                     "platform-administrator-governance.spec.js", "test_administrator_governance_requires_manual_isolated_profile"):
        if fragment not in commands:
            raise RegistrationError("administrator governance regression missing: " + fragment)
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  administrator-governance-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'platform-administrator-governance'" not in job.group("body") or "runs-on: ubuntu-24.04" not in job.group("body"):
        raise RegistrationError("administrator governance must use manual Hosted Linux")
    if "inputs.suite != 'platform-administrator-governance'" not in workflow:
        raise RegistrationError("administrator governance must not dispatch to self-hosted")
    gate = (repository / "scripts/test/online-hosted-administrator-governance-gate.sh").read_text()
    for fragment in ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', '--suite platform-administrator-governance',
                     'make build-iam-recovery build-iam-bootstrap GOOS=linux GOARCH=amd64 OUT_DIR=dist BUILD_TYPE=release',
                     'playwright install --with-deps chromium', 'make test-online "ONLINE_SUITE=$ONLINE_SUITE"'):
        if fragment not in gate:
            raise RegistrationError("administrator governance lifecycle missing: " + fragment)


def validate_catalog_domain_overview_profile(repository: Path, registered: set[str]) -> None:
    if "catalog-domain-overview" not in registered:
        return
    workflow = (repository / ".github/workflows/online-t4-gates.yml").read_text()
    job = re.search(r"(?ms)^  catalog-domain-hosted-t4:\n(?P<body>.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow)
    if job is None or "if: github.event_name == 'workflow_dispatch' && inputs.suite == 'catalog-domain-overview'" not in job.group("body") or "runs-on: ubuntu-24.04" not in job.group("body"):
        raise RegistrationError("Catalog domain overview requires manual Hosted Linux")
    if "inputs.suite != 'catalog-domain-overview'" not in workflow or "catalog-domain-overview)" in (repository / "scripts/test/online-host-gate.sh").read_text():
        raise RegistrationError("Catalog domain overview must not use self-hosted")
    if "online-hosted-catalog-domain-gate_test.py" not in make_target_commands((repository / "Makefile").read_text(), "test-catalog-domain-online-runner"):
        raise RegistrationError("Catalog Hosted cleanup regression is missing")
    required = {
        "scripts/test/online-hosted-catalog-domain-gate.sh": ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', "--suite catalog-domain-overview", "-model -catalog -quality"),
        "scripts/test/catalog-domain-overview-online.py": ("reader_inventory_denied", "stable_source_identities", "reference_element_unchanged", "catalog_history", "finally:"),
        "scripts/test/catalog-domain-overview-online_test.py": ("test_partial_fixture_failure_cleans_created_domain_and_leaves_reference_read_only", "test_cleanup_attempts_all_exact_resources_and_reports_failure"),
        "console/frontend/e2e/catalog-domain-overview.spec.js": ("catalog-domain-overview", "inventory", "governance"),
        "Makefile": ("test-catalog-domain-online-runner", "$(MAKE) test-catalog-domain-online-runner"),
    }
    for relative, fragments in required.items():
        path = repository / relative
        if not path.is_file():
            raise RegistrationError("Catalog domain overview requires " + relative)
        content = path.read_text(encoding="utf-8")
        if any(fragment not in content for fragment in fragments):
            raise RegistrationError("Catalog domain overview profile is incomplete: " + relative)


def check_registration(repository: Path) -> None:
    registry = load_suite_registry(repository)
    registered = load_registered_suites(registry)
    nightly = load_nightly_suites(registry)
    profiles = load_deployment_profiles(repository)
    workflow = load_workflow_suites(repository, nightly)
    if set(profiles) != registered:
        raise RegistrationError(
            f"Online deployment profiles {sorted(profiles)} do not match registered suites {sorted(registered)}"
        )
    if workflow != registered:
        raise RegistrationError(
            f"Online workflow choices {sorted(workflow)} do not match registered suites {sorted(registered)}"
        )
    validate_catalog_domain_overview_profile(repository, registered)
    validate_highgo_profile(repository, registered)
    validate_metric_engine_variant(repository, registered)
    validate_pg_authorization_profile(repository, registered)
    validate_public_origin_browser_profile(repository, registered)
    validate_vector_pmtiles_profile(repository, registered)
    validate_raster_workflow_profile(repository, registered)
    validate_raster_capacity_profile(repository, registered)
    validate_raster_contour_profile(repository, registered)
    validate_raster_polygonize_profile(repository, registered)
    validate_vector_rasterize_profile(repository, registered)
    validate_vector_zonal_statistics_profile(repository, registered)
    validate_raster_proximity_profile(repository, registered)
    validate_raster_focal_mode_profile(repository, registered)
    validate_raster_boundary_clean_profile(repository, registered)
    validate_raster_thin_profile(repository, registered)
    validate_raster_fill_depressions_profile(repository, registered)
    validate_raster_flow_direction_d8_profile(repository, registered)
    validate_raster_flow_accumulation_d8_profile(repository, registered)
    validate_raster_drainage_basins_d8_profile(repository, registered)
    validate_raster_flow_length_d8_profile(repository, registered)
    validate_raster_flow_length_upstream_d8_profile(repository, registered)
    validate_raster_stream_order_d8_profile(repository, registered)
    validate_raster_flow_accumulation_weighted_d8_profile(repository, registered)
    validate_raster_band_recommendation_profile(repository, registered)
    validate_raster_nibble_profile(repository, registered)
    validate_raster_disk_pressure_profile(repository, registered)
    validate_raster_nibble_capacity_profile(repository, registered)
    validate_raster_focal_statistics_profile(repository, registered)
    validate_raster_zonal_statistics_profile(repository, registered)
    validate_vector_topology_profile(repository, registered)
    validate_hdfs_spark_profile(repository, registered)
    validate_elasticsearch_spark_profile(repository, registered)
    validate_orchestrator_execution_profile(repository, registered)
    validate_quality_dynamic_binding_profile(repository, registered)
    validate_platform_node_metrics_profile(repository, registered)
    validate_administrator_governance_profile(repository, registered)
    validate_module_registry_process_profile(repository, registered)
    validate_workflow_runtime_health_profile(repository, registered)
    validate_consumer_engine_recovery_profile(repository, registered)
    validate_enterprise_catalog_publishing_profile(repository, registered)
    validate_workbench_service_consumption_profile(repository, registered)
    validate_manager_internal_artifact_lineage_profile(repository, registered)
    validate_local_sgm_profile(repository)
    validate_local_execution_trace_profile(repository)
    validate_manager_hybrid_search_profile(repository, registered)
    validate_security_transfer_protection_profile(repository, registered)
    validate_security_plaintext_access_profile(repository, registered)
    validate_security_mysql_owner_protection_profile(repository, registered)
    validate_graph_protection_profile(repository, registered)
    validate_oceanbase_consumer_flow_profile(repository, registered)
    validate_tidb_consumer_flow_profile(repository, registered)
    validate_opengauss_consumer_flow_profile(repository, registered)
    validate_kingbase_consumer_flow_profile(repository, registered)
    validate_transfer_insert_only_mysql_profile(repository, registered)
    validate_transfer_relational_sql_etl_profile(repository, registered)


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
