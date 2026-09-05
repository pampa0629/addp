import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).with_name("manager-internal-artifact-lineage-online.py")
SPEC = importlib.util.spec_from_file_location("manager_internal_artifact_lineage_online", SCRIPT)
SUITE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = SUITE
SPEC.loader.exec_module(SUITE)


def response(status, payload=None, *, raw=b""):
    return SUITE.Response(status, payload if payload is not None else {}, {}, raw)


class FakeGatewayClient:
    def __init__(self) -> None:
        self.task_exists = False
        self.result_exists = False
        self.calls: list[tuple[str, str]] = []
        self.item_id = 91
        self.fingerprint = "a" * 64
        self.item_locator = (
            "addp://engine/27/path/addp-online/pointcloud/"
            "pdal_las12_format0.las?type=object&item_id=91"
        )
        self.output_name = "pdal_las12_format0.copc.laz"
        self.output_locator = (
            "addp-infra://minio/manager/tenant_42/point-cloud-copc/91/"
            "pdal_las12_format0.copc.laz?type=object"
        )

    def lineage_execution(self):
        return {
            "status": "success",
            "metadata": {
                "lineage_facts": {
                    "schema_version": SUITE.LINEAGE_SCHEMA,
                    "inputs": [
                        {
                            "port": "source",
                            "locator": self.item_locator,
                            "item_id": self.item_id,
                            "item_fingerprint": self.fingerprint,
                        }
                    ],
                    "outputs": [{"port": "result", "locator": self.output_locator}],
                    "operations": [
                        {
                            "kind": "derive",
                            "operator": SUITE.TASK_TYPE,
                            "input_ports": ["source"],
                            "output_ports": ["result"],
                        }
                    ],
                }
            },
        }

    def request(self, method, path, expected, body=None, headers=None):
        self.calls.append((method, path))
        result = self._request(method, path, body, headers)
        if result.status not in expected:
            raise AssertionError(
                f"fake response {result.status} is not expected for {method} {path}"
            )
        return result

    def _request(self, method, path, body, headers):
        if path == "/api/v1/system/auth/context":
            return response(
                200,
                {
                    "principal": {"type": "user", "id": "51"},
                    "context": {"type": "tenant", "tenant_id": "42"},
                    "token": {"type": "first_party_access_token"},
                    "authorization": {
                        "role_assignments": [
                            {
                                "role_key": "tenant.data_operator",
                                "permissions": sorted(SUITE.REQUIRED_PERMISSIONS),
                            }
                        ]
                    },
                },
            )
        if path == "/api/v1/meta/scan/run/manual":
            self._assert_scan_request(body)
            return response(201, {"execution_id": "meta-execution-1"})
        if path == "/api/v1/meta/executions/meta-execution-1":
            return response(200, {"status": "success"})
        if path == "/api/v1/meta/engines/27/items":
            return response(
                200,
                [
                    {
                        "id": self.item_id,
                        "full_name": "addp-online/pointcloud/pdal_las12_format0.las",
                        "item_type": "object",
                        "fingerprint": self.fingerprint,
                        "size_bytes": 128,
                    }
                ],
            )
        if path == "/api/v1/manager/point_cloud_copc_tasks?page=1&page_size=100":
            return response(200, {"data": [], "total": 0})
        if path.startswith("/api/v1/manager/point_cloud_copc?item_fingerprint="):
            return response(
                200,
                {"data": [] if not self.result_exists else [{"id": 301}], "total": int(self.result_exists)},
            )
        if path == "/api/v1/manager/point_cloud_copc_tasks" and method == "POST":
            self._assert_task_request(body)
            self.task_exists = True
            return response(201, {"id": 201})
        if path == "/api/v1/manager/tasks/point_cloud_copc_generation/201/execute":
            if not self.task_exists:
                raise AssertionError("task must exist before execution")
            self.result_exists = True
            return response(202, {"execution_id": "manager-execution-1"})
        if path == "/api/v1/manager/executions/manager-execution-1":
            return response(200, self.lineage_execution())
        if path == "/api/v1/monitor/executions/by-execution-id/manager-execution-1":
            return response(200, self.lineage_execution())
        if path == "/api/v1/manager/point_cloud_copc?task_id=201&page=1&page_size=20":
            data = (
                [{"id": 301, "file_name": self.output_name}]
                if self.result_exists
                else []
            )
            return response(200, {"data": data, "total": len(data)})
        if path == "/api/v1/manager/point_cloud_copc/301/content":
            if self.result_exists:
                if headers != {
                    "Accept": "application/octet-stream",
                    "Range": "bytes=0-63",
                }:
                    raise AssertionError(f"unexpected Range headers: {headers!r}")
                return response(206, raw=b"COPC fixture")
            return response(404)
        if path == "/api/v1/manager/point_cloud_copc/301" and method == "DELETE":
            self.result_exists = False
            return response(200)
        if path == "/api/v1/manager/point_cloud_copc_tasks/201" and method == "DELETE":
            self.task_exists = False
            return response(200)
        if path == "/api/v1/manager/point_cloud_copc_tasks/201" and method == "GET":
            return response(200 if self.task_exists else 404)
        raise AssertionError(f"unexpected request {method} {path} body={body!r}")

    @staticmethod
    def _assert_scan_request(body):
        if body != {
            "engine_id": 27,
            "scan_depth": "deep",
            "trigger_type": "manual",
            "force": True,
        }:
            raise AssertionError(f"unexpected Meta scan body: {body!r}")

    def _assert_task_request(self, body):
        source = body["config"]["source"]
        expected = {
            "item_locator": self.item_locator,
            "source_engine_id": 27,
            "item_fingerprint": self.fingerprint,
            "item_id": self.item_id,
            "format": "las",
            "source_size_bytes": 128,
        }
        if source != expected:
            raise AssertionError(f"unexpected PointCloud source: {source!r}")


def scenario_environment(artifact_dir: str):
    return {
        "ADDP_ONLINE_ARTIFACT_DIR": artifact_dir,
        "ADDP_ONLINE_TEST_RUN_ID": "run-1",
        "ADDP_ONLINE_TEST_TENANT_ID": "42",
        "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "token",
        "ADDP_ONLINE_POINTCLOUD_MINIO_ENGINE_ID": "27",
        "ADDP_ONLINE_POINTCLOUD_MINIO_BUCKET": "addp-online",
        "ADDP_ONLINE_POINTCLOUD_MINIO_OBJECT": "pointcloud/pdal_las12_format0.las",
        "GATEWAY_URL": "http://127.0.0.1:8000",
    }


class ManagerInternalArtifactLineageOnlineTest(unittest.TestCase):
    def test_runs_complete_owner_route_and_verifies_zero_residual_resources(self) -> None:
        client = FakeGatewayClient()
        browser_calls = []

        def browser(repository, environment, **evidence):
            browser_calls.append((repository, evidence))
            return {
                "schema_version": "addp.manager-internal-artifact-lineage-browser/v1",
                "suite": "manager-internal-artifact-lineage",
                "run_id": environment["ADDP_ONLINE_TEST_RUN_ID"],
                "result": "passed",
                "execution_id": evidence["execution_id"],
                "item_id": evidence["item_id"],
                "output_name": evidence["output_name"],
                "input_resources": 1,
                "output_resources": 1,
                "platform_internal_outputs": 1,
                "browser_warning_errors": 0,
            }

        with tempfile.TemporaryDirectory() as artifact_dir:
            with mock.patch.object(SUITE, "GatewayClient", return_value=client):
                report = SUITE.run_scenario(
                    Path("/repository"), scenario_environment(artifact_dir), browser
                )

        self.assertEqual(
            report["lineage"],
            {
                "schema_version": SUITE.LINEAGE_SCHEMA,
                "inputs": 1,
                "outputs": 1,
                "manager_monitor_equal": True,
            },
        )
        self.assertEqual(
            report["cleanup"],
            {
                "result_deleted": True,
                "task_deleted": True,
                "content_unavailable": True,
                "residual_resources": 0,
            },
        )
        self.assertFalse(client.task_exists)
        self.assertFalse(client.result_exists)
        self.assertEqual(browser_calls[0][1]["source_name"], "pdal_las12_format0.las")
        self.assertLess(
            client.calls.index(("DELETE", "/api/v1/manager/point_cloud_copc/301")),
            client.calls.index(("DELETE", "/api/v1/manager/point_cloud_copc_tasks/201")),
        )

    def test_browser_failure_still_deletes_result_and_task(self) -> None:
        client = FakeGatewayClient()

        def browser(*_args, **_kwargs):
            raise SUITE.SuiteError("injected browser failure")

        with tempfile.TemporaryDirectory() as artifact_dir:
            with mock.patch.object(SUITE, "GatewayClient", return_value=client):
                with self.assertRaisesRegex(SUITE.SuiteError, "injected browser failure"):
                    SUITE.run_scenario(
                        Path("/repository"), scenario_environment(artifact_dir), browser
                    )

        self.assertFalse(client.task_exists)
        self.assertFalse(client.result_exists)

    def execution(self, output_locator: str | None = None):
        return {
            "metadata": {
                "lineage_facts": {
                    "schema_version": SUITE.LINEAGE_SCHEMA,
                    "inputs": [
                        {
                            "port": "source",
                            "locator": "addp://engine/12/path/addp-online/pointcloud/source.las?type=object&item_id=91",
                            "item_id": 91,
                            "item_fingerprint": "sha256:" + "a" * 64,
                        }
                    ],
                    "outputs": [
                        {
                            "port": "result",
                            "locator": output_locator
                            or "addp-infra://minio/manager/tenant_42/point-cloud-copc/source/source.copc.laz?type=object",
                        }
                    ],
                    "operations": [
                        {
                            "kind": "derive",
                            "operator": SUITE.TASK_TYPE,
                            "input_ports": ["source"],
                            "output_ports": ["result"],
                        }
                    ],
                }
            }
        }

    def test_validates_owner_facts_for_business_input_and_infra_output(self) -> None:
        facts = SUITE.validate_lineage(
            self.execution(),
            item_locator="addp://engine/12/path/addp-online/pointcloud/source.las?type=object&item_id=91",
            item_id=91,
            fingerprint="sha256:" + "a" * 64,
            tenant_id=42,
        )

        self.assertEqual(facts["schema_version"], SUITE.LINEAGE_SCHEMA)
        self.assertEqual(facts["inputs"][0]["port"], "source")
        self.assertEqual(facts["outputs"][0]["port"], "result")

    def test_rejects_business_output_instead_of_manager_internal_artifact(self) -> None:
        with self.assertRaisesRegex(SUITE.SuiteError, "not the Manager infra COPC artifact"):
            SUITE.validate_lineage(
                self.execution("addp://engine/12/path/output/source.copc.laz?type=object"),
                item_locator="addp://engine/12/path/addp-online/pointcloud/source.las?type=object&item_id=91",
                item_id=91,
                fingerprint="sha256:" + "a" * 64,
                tenant_id=42,
            )

    def test_builds_canonical_resource_locator_from_meta_item(self) -> None:
        locator = SUITE.build_item_locator(
            12,
            {"id": 91, "full_name": "addp-online/point cloud/source.las"},
        )

        self.assertEqual(
            locator,
            "addp://engine/12/path/addp-online/point%20cloud/source.las?type=object&item_id=91",
        )

    def test_accepts_zero_as_a_canonical_non_negative_count(self) -> None:
        self.assertEqual(SUITE.non_negative_int(0, "total"), 0)

    def test_rejects_non_canonical_non_negative_count(self) -> None:
        with self.assertRaisesRegex(SUITE.SuiteError, "canonical"):
            SUITE.non_negative_int("00", "total")

    def test_validates_browser_report_contract(self) -> None:
        report = {
            "schema_version": "addp.manager-internal-artifact-lineage-browser/v1",
            "suite": "manager-internal-artifact-lineage",
            "run_id": "run-1",
            "result": "passed",
            "execution_id": "execution-1",
            "item_id": 91,
            "output_name": "source.copc.laz",
            "input_resources": 1,
            "output_resources": 1,
            "platform_internal_outputs": 1,
            "browser_warning_errors": 0,
        }

        validated = SUITE.validate_browser_report(
            report,
            run_id="run-1",
            execution_id="execution-1",
            item_id=91,
            output_name="source.copc.laz",
        )

        self.assertEqual(validated, report)

    def test_rejects_browser_report_without_platform_internal_output(self) -> None:
        report = {
            "schema_version": "addp.manager-internal-artifact-lineage-browser/v1",
            "suite": "manager-internal-artifact-lineage",
            "run_id": "run-1",
            "result": "passed",
            "execution_id": "execution-1",
            "item_id": 91,
            "output_name": "source.copc.laz",
            "input_resources": 1,
            "output_resources": 1,
            "platform_internal_outputs": 0,
            "browser_warning_errors": 0,
        }

        with self.assertRaisesRegex(SUITE.SuiteError, "platform_internal_outputs"):
            SUITE.validate_browser_report(
                report,
                run_id="run-1",
                execution_id="execution-1",
                item_id=91,
                output_name="source.copc.laz",
            )


if __name__ == "__main__":
    unittest.main()
