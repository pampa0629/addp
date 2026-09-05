#!/usr/bin/env python3
"""Accept Business MinIO -> Manager infra artifact -> Monitor lineage through real services."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Iterable, Mapping


TERMINAL_STATUSES = {"success", "failed", "timeout", "cancelled"}
TASK_TYPE = "point_cloud_copc_generation"
LINEAGE_SCHEMA = "addp.lineage-facts/v1"
REQUIRED_PERMISSIONS = {
    "manager.derived_artifact.create",
    "manager.derived_artifact.delete",
    "manager.derived_artifact.read",
    "meta.catalog.read",
    "meta.scan_task.execute",
    "meta.scan_task.read",
    "monitor.execution.read",
}
FORBIDDEN_ADMIN_ROLES = {
    "platform.audit_administrator",
    "platform.security_administrator",
    "platform.system_administrator",
    "tenant.administrator",
}


class SuiteError(RuntimeError):
    pass


@dataclass
class Response:
    status: int
    payload: Any
    headers: Mapping[str, str]
    raw: bytes = b""


class GatewayClient:
    def __init__(self, base_url: str, token: str, timeout: float) -> None:
        parsed = urllib.parse.urlsplit(base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise SuiteError("GATEWAY_URL must be an absolute HTTP(S) URL")
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.timeout = timeout

    def request(
        self,
        method: str,
        path: str,
        expected: Iterable[int],
        body: dict[str, object] | None = None,
        headers: Mapping[str, str] | None = None,
    ) -> Response:
        data = None if body is None else json.dumps(body).encode()
        request_headers = {
            "Accept": "application/json",
            "Content-Type": "application/json",
            "Authorization": f"Bearer {self.token}",
        }
        request_headers.update(headers or {})
        request = urllib.request.Request(
            self.base_url + path,
            data=data,
            method=method,
            headers=request_headers,
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                status = response.status
                raw = response.read()
                response_headers = dict(response.headers.items())
        except urllib.error.HTTPError as error:
            status = error.code
            raw = error.read()
            response_headers = dict(error.headers.items()) if error.headers else {}
        except (urllib.error.URLError, TimeoutError) as error:
            raise SuiteError(f"{method} {path} transport failed: {error}") from error
        content_type = next(
            (value for key, value in response_headers.items() if key.lower() == "content-type"),
            "",
        ).lower()
        payload: Any = {}
        if raw and ("json" in content_type or status >= 400):
            try:
                payload = json.loads(raw)
            except json.JSONDecodeError as error:
                raise SuiteError(f"{method} {path} returned invalid JSON") from error
        if status not in set(expected):
            code = payload.get("error_code", "unknown") if isinstance(payload, dict) else "unknown"
            raise SuiteError(f"{method} {path} returned HTTP {status} ({code})")
        return Response(status=status, payload=payload, headers=response_headers, raw=raw)


def _object(value: Any, resource: str) -> dict[str, object]:
    if not isinstance(value, dict):
        raise SuiteError(f"{resource} must be an object")
    return value


def _array(value: Any, resource: str) -> list[object]:
    if not isinstance(value, list):
        raise SuiteError(f"{resource} must be an array")
    return value


def positive_int(value: object, field: str) -> int:
    if isinstance(value, bool):
        raise SuiteError(f"{field} must be a positive integer")
    try:
        parsed = int(value)  # type: ignore[arg-type]
    except (TypeError, ValueError) as error:
        raise SuiteError(f"{field} must be a positive integer") from error
    if parsed <= 0 or str(parsed) != str(value):
        raise SuiteError(f"{field} must be a canonical positive integer")
    return parsed


def non_negative_int(value: object, field: str) -> int:
    if isinstance(value, bool):
        raise SuiteError(f"{field} must be a non-negative integer")
    try:
        parsed = int(value)  # type: ignore[arg-type]
    except (TypeError, ValueError) as error:
        raise SuiteError(f"{field} must be a non-negative integer") from error
    if parsed < 0 or str(parsed) != str(value):
        raise SuiteError(f"{field} must be a canonical non-negative integer")
    return parsed


def validate_user_identity(client: GatewayClient, tenant_id: int) -> dict[str, object]:
    context = _object(
        client.request("GET", "/api/v1/system/auth/context", (200,)).payload,
        "AuthContext",
    )
    principal = _object(context.get("principal"), "AuthContext principal")
    tenant = _object(context.get("context"), "AuthContext context")
    token = _object(context.get("token"), "AuthContext token")
    authorization = _object(context.get("authorization"), "AuthContext authorization")
    if principal.get("type") != "user":
        raise SuiteError("Online Manager lineage token must belong to a User")
    principal_id = positive_int(principal.get("id"), "AuthContext principal.id")
    if tenant.get("type") != "tenant" or tenant.get("tenant_id") != str(tenant_id):
        raise SuiteError("Online Manager lineage token must use the configured Tenant Context")
    if token.get("type") not in {"first_party_access_token", "oauth_access_token"}:
        raise SuiteError("Online Manager lineage token must be a User Access Token")
    roles: set[str] = set()
    permissions: set[str] = set()
    for assignment in _array(authorization.get("role_assignments"), "AuthContext role_assignments"):
        item = _object(assignment, "AuthContext role assignment")
        role = item.get("role_key")
        granted = item.get("permissions")
        if not isinstance(role, str) or not isinstance(granted, list) or not all(isinstance(key, str) for key in granted):
            raise SuiteError("AuthContext role assignment is incomplete")
        roles.add(role)
        permissions.update(granted)
    forbidden = roles & FORBIDDEN_ADMIN_ROLES
    if forbidden:
        raise SuiteError("Online Manager lineage token must not use administrator roles: " + ", ".join(sorted(forbidden)))
    missing = REQUIRED_PERMISSIONS - permissions
    if missing:
        raise SuiteError("Online Manager lineage token is missing required permissions: " + ", ".join(sorted(missing)))
    return {
        "principal_id": str(principal_id),
        "principal_type": "user",
        "tenant_id": str(tenant_id),
        "roles": sorted(roles),
        "permissions_verified": sorted(REQUIRED_PERMISSIONS),
    }


def wait_for_meta_scan(client: GatewayClient, engine_id: int, deadline: float) -> str:
    run = _object(
        client.request(
            "POST",
            "/api/v1/meta/scan/run/manual",
            (201,),
            {"engine_id": engine_id, "scan_depth": "deep", "trigger_type": "manual", "force": True},
        ).payload,
        "Meta scan execution",
    )
    execution_id = run.get("execution_id")
    if not isinstance(execution_id, str) or not execution_id:
        raise SuiteError("Meta scan execution_id is missing")
    while time.monotonic() < deadline:
        execution = _object(
            client.request("GET", f"/api/v1/meta/executions/{urllib.parse.quote(execution_id)}", (200,)).payload,
            "Meta execution",
        )
        status = execution.get("status")
        if status == "success":
            return execution_id
        if status in TERMINAL_STATUSES:
            raise SuiteError(f"Meta scan ended with status {status}")
        time.sleep(1)
    raise SuiteError("Meta scan did not finish before the convergence timeout")


def find_fixture_item(client: GatewayClient, engine_id: int, full_name: str) -> dict[str, object]:
    items = _array(
        client.request("GET", f"/api/v1/meta/engines/{engine_id}/items", (200,)).payload,
        "Meta engine items",
    )
    matches = [item for item in items if isinstance(item, dict) and item.get("full_name") == full_name]
    if len(matches) != 1:
        raise SuiteError(f"Meta must return exactly one point-cloud fixture {full_name}")
    item = matches[0]
    if item.get("item_type") not in {"object", "file"}:
        raise SuiteError("point-cloud fixture must be an object or file DataItem")
    fingerprint = item.get("fingerprint")
    if not isinstance(fingerprint, str) or not fingerprint:
        raise SuiteError("point-cloud fixture fingerprint is missing")
    positive_int(item.get("id"), "point-cloud fixture id")
    size = item.get("size_bytes")
    if size is not None and positive_int(size, "point-cloud fixture size_bytes") <= 0:
        raise SuiteError("point-cloud fixture size_bytes must be positive")
    return item


def build_item_locator(engine_id: int, item: Mapping[str, object]) -> str:
    item_id = positive_int(item.get("id"), "point-cloud fixture id")
    full_name = item.get("full_name")
    if not isinstance(full_name, str) or not full_name.strip():
        raise SuiteError("point-cloud fixture full_name is missing")
    path = urllib.parse.quote(full_name.strip("/"), safe="/")
    return f"addp://engine/{engine_id}/path/{path}?type=object&item_id={item_id}"


def assert_no_existing_resources(client: GatewayClient, fingerprint: str) -> None:
    tasks = _object(
        client.request("GET", "/api/v1/manager/point_cloud_copc_tasks?page=1&page_size=100", (200,)).payload,
        "PointCloud task list",
    )
    for task in _array(tasks.get("data"), "PointCloud task list data"):
        if not isinstance(task, dict):
            continue
        config = task.get("config")
        source = config.get("source") if isinstance(config, dict) else None
        if isinstance(source, dict) and source.get("item_fingerprint") == fingerprint:
            raise SuiteError("a stale PointCloud task exists for the dedicated fixture")
    query = urllib.parse.urlencode({"item_fingerprint": fingerprint, "page": 1, "page_size": 100})
    results = _object(
        client.request("GET", f"/api/v1/manager/point_cloud_copc?{query}", (200,)).payload,
        "PointCloud result list",
    )
    if non_negative_int(results.get("total"), "PointCloud result total") != 0:
        raise SuiteError("a stale PointCloud result exists for the dedicated fixture")


def wait_for_manager_execution(client: GatewayClient, execution_id: str, deadline: float) -> dict[str, object]:
    while time.monotonic() < deadline:
        execution = _object(
            client.request("GET", f"/api/v1/manager/executions/{urllib.parse.quote(execution_id)}", (200,)).payload,
            "Manager execution",
        )
        status = execution.get("status")
        if status == "success":
            return execution
        if status in TERMINAL_STATUSES:
            details = execution.get("error_details") or execution.get("metadata")
            raise SuiteError(f"Manager PointCloud execution ended with status {status}: {details!r}")
        time.sleep(1)
    raise SuiteError("Manager PointCloud execution did not finish before the convergence timeout")


def validate_lineage(
    execution: Mapping[str, object],
    *,
    item_locator: str,
    item_id: int,
    fingerprint: str,
    tenant_id: int,
) -> dict[str, object]:
    metadata = _object(execution.get("metadata"), "execution metadata")
    facts = _object(metadata.get("lineage_facts"), "execution lineage_facts")
    if facts.get("schema_version") != LINEAGE_SCHEMA:
        raise SuiteError("execution lineage_facts schema_version is invalid")
    inputs = _array(facts.get("inputs"), "execution lineage inputs")
    outputs = _array(facts.get("outputs"), "execution lineage outputs")
    operations = _array(facts.get("operations"), "execution lineage operations")
    if len(inputs) != 1 or len(outputs) != 1 or len(operations) != 1:
        raise SuiteError("execution lineage must contain exactly one input, output and operation")
    input_ref = _object(inputs[0], "execution lineage input")
    expected_input = {
        "port": "source",
        "locator": item_locator,
        "item_id": item_id,
        "item_fingerprint": fingerprint,
    }
    for key, value in expected_input.items():
        if input_ref.get(key) != value:
            raise SuiteError(f"execution lineage input {key} is invalid")
    output_ref = _object(outputs[0], "execution lineage output")
    output_locator = output_ref.get("locator")
    expected_prefix = f"addp-infra://minio/manager/tenant_{tenant_id}/point-cloud-copc/"
    if output_ref.get("port") != "result" or not isinstance(output_locator, str) or not output_locator.startswith(expected_prefix):
        raise SuiteError("execution lineage output is not the Manager infra COPC artifact")
    parsed_output = urllib.parse.urlsplit(output_locator)
    if urllib.parse.parse_qs(parsed_output.query) != {"type": ["object"]} or not parsed_output.path.endswith(".copc.laz"):
        raise SuiteError("execution lineage output must be a COPC object locator")
    operation = _object(operations[0], "execution lineage operation")
    expected_operation = {
        "kind": "derive",
        "operator": TASK_TYPE,
        "input_ports": ["source"],
        "output_ports": ["result"],
    }
    for key, value in expected_operation.items():
        if operation.get(key) != value:
            raise SuiteError(f"execution lineage operation {key} is invalid")
    return facts


def validate_browser_report(
    report: object,
    *,
    run_id: str,
    execution_id: str,
    item_id: int,
    output_name: str,
) -> dict[str, object]:
    payload = _object(report, "Manager lineage browser report")
    expected = {
        "schema_version": "addp.manager-internal-artifact-lineage-browser/v1",
        "suite": "manager-internal-artifact-lineage",
        "run_id": run_id,
        "result": "passed",
        "execution_id": execution_id,
        "item_id": item_id,
        "output_name": output_name,
        "input_resources": 1,
        "output_resources": 1,
        "platform_internal_outputs": 1,
        "browser_warning_errors": 0,
    }
    mismatches = [key for key, value in expected.items() if payload.get(key) != value]
    if mismatches:
        raise SuiteError("Manager lineage browser report contract mismatch: " + ", ".join(mismatches))
    return payload


def run_browser(
    repository: Path,
    environment: dict[str, str],
    *,
    execution_id: str,
    item_id: int,
    source_name: str,
    output_name: str,
) -> dict[str, object]:
    artifact_dir = Path(environment["ADDP_ONLINE_ARTIFACT_DIR"])
    report_path = artifact_dir / "manager-internal-artifact-lineage-browser.json"
    report_path.unlink(missing_ok=True)
    browser_environment = dict(environment)
    browser_environment.update(
        {
            "ADDP_ONLINE_MANAGER_LINEAGE_EXECUTION_ID": execution_id,
            "ADDP_ONLINE_MANAGER_LINEAGE_ITEM_ID": str(item_id),
            "ADDP_ONLINE_MANAGER_LINEAGE_SOURCE_NAME": source_name,
            "ADDP_ONLINE_MANAGER_LINEAGE_OUTPUT_NAME": output_name,
        }
    )
    result = subprocess.run(
        [
            "npm",
            "--prefix",
            "console/frontend",
            "exec",
            "--",
            "playwright",
            "test",
            "e2e/online/manager-internal-artifact-lineage.spec.js",
            "--config=playwright.online.config.js",
        ],
        cwd=repository,
        env=browser_environment,
        check=False,
    )
    if result.returncode != 0:
        raise SuiteError(f"Manager lineage browser acceptance exited with status {result.returncode}")
    if not report_path.is_file():
        raise SuiteError("Playwright did not write manager-internal-artifact-lineage-browser.json")
    return validate_browser_report(
        json.loads(report_path.read_text(encoding="utf-8")),
        run_id=environment["ADDP_ONLINE_TEST_RUN_ID"],
        execution_id=execution_id,
        item_id=item_id,
        output_name=output_name,
    )


def run_scenario(
    repository: Path,
    environment: dict[str, str],
    browser_runner: Callable[..., dict[str, object]] | None = None,
) -> dict[str, object]:
    tenant_id = positive_int(environment["ADDP_ONLINE_TEST_TENANT_ID"], "ADDP_ONLINE_TEST_TENANT_ID")
    engine_id = positive_int(environment["ADDP_ONLINE_POINTCLOUD_MINIO_ENGINE_ID"], "ADDP_ONLINE_POINTCLOUD_MINIO_ENGINE_ID")
    timeout = float(environment.get("ADDP_ONLINE_MANAGER_LINEAGE_CONVERGENCE_TIMEOUT_SECONDS", "180"))
    if timeout <= 0:
        raise SuiteError("ADDP_ONLINE_MANAGER_LINEAGE_CONVERGENCE_TIMEOUT_SECONDS must be positive")
    client = GatewayClient(environment["GATEWAY_URL"], environment["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"], min(timeout, 30))
    identity = validate_user_identity(client, tenant_id)
    fixture_full_name = f"{environment['ADDP_ONLINE_POINTCLOUD_MINIO_BUCKET'].strip('/')}/{environment['ADDP_ONLINE_POINTCLOUD_MINIO_OBJECT'].strip('/')}"
    deadline = time.monotonic() + timeout
    scan_execution_id = wait_for_meta_scan(client, engine_id, deadline)
    item = find_fixture_item(client, engine_id, fixture_full_name)
    item_id = positive_int(item.get("id"), "point-cloud fixture id")
    fingerprint = str(item["fingerprint"])
    size_bytes = positive_int(item.get("size_bytes"), "point-cloud fixture size_bytes")
    item_locator = build_item_locator(engine_id, item)
    assert_no_existing_resources(client, fingerprint)

    task_id: int | None = None
    result_id: int | None = None
    execution_id = ""
    cleanup = {"result_deleted": False, "task_deleted": False, "content_unavailable": False, "residual_resources": -1}
    try:
        task = _object(
            client.request(
                "POST",
                "/api/v1/manager/point_cloud_copc_tasks",
                (201,),
                {
                    "name": f"Online Manager lineage {environment['ADDP_ONLINE_TEST_RUN_ID']}",
                    "description": "Dedicated T4 Business MinIO to Manager infra lineage acceptance",
                    "config": {
                        "source": {
                            "item_locator": item_locator,
                            "source_engine_id": engine_id,
                            "item_fingerprint": fingerprint,
                            "item_id": item_id,
                            "format": "las",
                            "source_size_bytes": size_bytes,
                        }
                    },
                },
            ).payload,
            "created PointCloud task",
        )
        task_id = positive_int(task.get("id"), "created PointCloud task id")
        started = _object(
            client.request(
                "POST",
                f"/api/v1/manager/tasks/{TASK_TYPE}/{task_id}/execute",
                (202,),
                {"trigger_type": "manual", "source": "manager"},
            ).payload,
            "PointCloud execution start",
        )
        execution_id_value = started.get("execution_id")
        if not isinstance(execution_id_value, str) or not execution_id_value:
            raise SuiteError("PointCloud execution_id is missing")
        execution_id = execution_id_value
        manager_execution = wait_for_manager_execution(client, execution_id, deadline)
        manager_facts = validate_lineage(
            manager_execution,
            item_locator=item_locator,
            item_id=item_id,
            fingerprint=fingerprint,
            tenant_id=tenant_id,
        )

        monitor_execution = _object(
            client.request(
                "GET",
                f"/api/v1/monitor/executions/by-execution-id/{urllib.parse.quote(execution_id)}",
                (200,),
            ).payload,
            "Monitor execution",
        )
        monitor_facts = validate_lineage(
            monitor_execution,
            item_locator=item_locator,
            item_id=item_id,
            fingerprint=fingerprint,
            tenant_id=tenant_id,
        )
        if monitor_facts != manager_facts:
            raise SuiteError("Monitor lineage facts differ from the Manager owner facts")

        query = urllib.parse.urlencode({"task_id": task_id, "page": 1, "page_size": 20})
        results = _object(
            client.request("GET", f"/api/v1/manager/point_cloud_copc?{query}", (200,)).payload,
            "PointCloud result list",
        )
        result_rows = _array(results.get("data"), "PointCloud result list data")
        if len(result_rows) != 1:
            raise SuiteError("PointCloud execution must create exactly one result")
        result = _object(result_rows[0], "PointCloud result")
        result_id = positive_int(result.get("id"), "PointCloud result id")
        output_name = result.get("file_name")
        if not isinstance(output_name, str) or not output_name.endswith(".copc.laz"):
            raise SuiteError("PointCloud result file_name must end with .copc.laz")
        content = client.request(
            "GET",
            f"/api/v1/manager/point_cloud_copc/{result_id}/content",
            (206,),
            headers={"Accept": "application/octet-stream", "Range": "bytes=0-63"},
        )
        if not content.raw or len(content.raw) > 64:
            raise SuiteError("PointCloud COPC Range response is empty or unbounded")

        browser_evidence: dict[str, object] = {}
        if browser_runner is not None:
            browser_evidence = browser_runner(
                repository,
                environment,
                execution_id=execution_id,
                item_id=item_id,
                source_name=Path(environment["ADDP_ONLINE_POINTCLOUD_MINIO_OBJECT"]).name,
                output_name=output_name,
            )
        return {
            "schema_version": "addp.manager-internal-artifact-lineage/v1",
            "suite": "manager-internal-artifact-lineage",
            "scenario": "business-object-to-manager-infra-lineage",
            "run_id": environment["ADDP_ONLINE_TEST_RUN_ID"],
            "result": "passed",
            "tenant_id": str(tenant_id),
            "identity": identity,
            "engine_id": engine_id,
            "meta_scan_execution_id": scan_execution_id,
            "source": {"item_id": item_id, "fingerprint": fingerprint, "locator": item_locator},
            "created": {"task_id": task_id, "execution_id": execution_id, "result_id": result_id},
            "lineage": {"schema_version": LINEAGE_SCHEMA, "inputs": 1, "outputs": 1, "manager_monitor_equal": True},
            "artifact": {"name": output_name, "range_bytes": len(content.raw), "storage_domain": "addp-infra"},
            "browser": browser_evidence,
            "cleanup": cleanup,
        }
    finally:
        cleanup_errors: list[str] = []
        if result_id is not None:
            try:
                client.request("DELETE", f"/api/v1/manager/point_cloud_copc/{result_id}", (200,))
                cleanup["result_deleted"] = True
                client.request("GET", f"/api/v1/manager/point_cloud_copc/{result_id}/content", (404,))
                cleanup["content_unavailable"] = True
            except SuiteError as error:
                cleanup_errors.append(str(error))
        if task_id is not None:
            try:
                client.request("DELETE", f"/api/v1/manager/point_cloud_copc_tasks/{task_id}", (200,))
                client.request("GET", f"/api/v1/manager/point_cloud_copc_tasks/{task_id}", (404,))
                cleanup["task_deleted"] = True
            except SuiteError as error:
                cleanup_errors.append(str(error))
        if task_id is not None or result_id is not None:
            try:
                query = urllib.parse.urlencode({"item_fingerprint": fingerprint, "page": 1, "page_size": 100})
                results = _object(
                    client.request("GET", f"/api/v1/manager/point_cloud_copc?{query}", (200,)).payload,
                    "PointCloud residual result list",
                )
                residual_results = non_negative_int(
                    results.get("total"), "PointCloud residual result total"
                )
                residual_tasks = 0 if cleanup["task_deleted"] else 1
                cleanup["residual_resources"] = residual_results + residual_tasks
                if residual_results != 0:
                    cleanup_errors.append(
                        f"PointCloud residual result count is {residual_results}"
                    )
                if residual_tasks != 0:
                    cleanup_errors.append("PointCloud task deletion was not verified")
            except SuiteError as error:
                cleanup_errors.append(str(error))
        if cleanup_errors:
            raise SuiteError("Manager lineage cleanup failed: " + "; ".join(cleanup_errors))


def required_environment() -> dict[str, str]:
    names = (
        "ADDP_ONLINE_ARTIFACT_DIR",
        "ADDP_ONLINE_TEST_RUN_ID",
        "ADDP_ONLINE_TEST_TENANT_ID",
        "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN",
        "ADDP_ONLINE_TEST_USER_USERNAME",
        "ADDP_ONLINE_TEST_USER_PASSWORD",
        "ADDP_ONLINE_POINTCLOUD_MINIO_ENGINE_ID",
        "ADDP_ONLINE_POINTCLOUD_MINIO_BUCKET",
        "ADDP_ONLINE_POINTCLOUD_MINIO_OBJECT",
        "CONSOLE_URL",
        "GATEWAY_URL",
    )
    missing = [name for name in names if not os.environ.get(name)]
    if missing:
        raise SuiteError("missing Online environment: " + ", ".join(missing))
    return dict(os.environ)


def main() -> int:
    try:
        environment = required_environment()
        repository = Path(__file__).resolve().parents[2]
        report = run_scenario(repository, environment, run_browser)
    except (OSError, ValueError, SuiteError, subprocess.SubprocessError) as error:
        print(f"Manager internal artifact lineage Online acceptance failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
