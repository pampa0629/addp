import importlib.util
import sys
import tempfile
import unittest
import urllib.parse
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).with_name("manager-hybrid-search-online.py")
SPEC = importlib.util.spec_from_file_location("manager_hybrid_search_online", SCRIPT)
SUITE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = SUITE
SPEC.loader.exec_module(SUITE)


def response(status=200, payload=None):
    return SUITE.Response(status=status, payload={} if payload is None else payload, headers={})


class FakeGatewayClient:
    def __init__(self) -> None:
        self.item_id = 701
        self.fingerprint = "a" * 64
        self.locator = (
            "addp://engine/27/path/addp-online/hybrid-search/"
            "purple-gaming-light-gun.jpg?type=object&item_id=701"
        )
        self.embedding_exists = False
        self.fail_semantic_search = False
        self.calls = []

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, body))
        result = self._request(method, path, body)
        if result.status not in expected:
            raise SUITE.SuiteError(f"unexpected fake response {result.status} for {method} {path}")
        return result

    def _request(self, method, path, body):
        if path == "/api/v1/system/auth/context":
            return response(payload={
                "principal": {"type": "user", "id": "51"},
                "context": {"type": "tenant", "tenant_id": "42"},
                "token": {"type": "first_party_access_token"},
                "authorization": {"role_assignments": [{
                    "role_key": "tenant.data_steward",
                    "permissions": sorted(SUITE.REQUIRED_PERMISSIONS),
                }]},
            })
        if path == "/api/v1/meta/scan/run/manual":
            self.assert_scan_body(body)
            return response(201, {"execution_id": "meta-execution-1"})
        if path == "/api/v1/meta/executions/meta-execution-1":
            return response(payload={"status": "success"})
        if path == "/api/v1/meta/engines/27/items":
            return response(payload=[{
                "id": self.item_id,
                "full_name": "addp-online/hybrid-search/purple-gaming-light-gun.jpg",
                "item_type": "object",
                "fingerprint": self.fingerprint,
                "size_bytes": 4096,
            }])
        if path.startswith("/api/v1/manager/embeddings?"):
            query = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)
            if query != {"item_id": ["701"], "page": ["1"], "page_size": ["20"]}:
                raise AssertionError(f"unexpected embedding list query: {query!r}")
            rows = []
            if self.embedding_exists:
                rows = [{
                    "id": 801,
                    "item_id": self.item_id,
                    "item_fingerprint": self.fingerprint,
                    "status": "ready",
                    "model_profile_id": "11111111-1111-1111-1111-111111111111",
                }]
            return response(payload={"data": rows, "total": len(rows)})
        if path == "/api/v1/manager/embedding_executions" and method == "POST":
            expected = {
                "scope": "item",
                "target": {
                    "engine_id": 27,
                    "item_id": self.item_id,
                    "item_fingerprint": self.fingerprint,
                    "locator": self.locator,
                },
                "entry": "online_gate",
            }
            if body != expected:
                raise AssertionError(f"unexpected embedding request: {body!r}")
            self.embedding_exists = True
            return response(payload={"execution_id": "manager-execution-1", "status": "pending"})
        if path == "/api/v1/manager/executions/manager-execution-1":
            return response(payload={"status": "success"})
        if path.startswith("/api/v1/manager/search?"):
            query = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)
            text = query["q"][0]
            if text == SUITE.SEMANTIC_QUERY and self.fail_semantic_search:
                raise SUITE.SuiteError("injected semantic search failure")
            if text == SUITE.SEMANTIC_QUERY:
                self.assert_search_query(query, page_size="1")
                methods = ["vector"]
            else:
                self.assert_search_query(query, page_size="10")
                self.assertEqual(text, "purple-gaming-light-gun")
                methods = ["keyword", "vector"]
            return response(payload={"data": {
                "total": 1,
                "page": 1,
                "page_size": int(query["page_size"][0]),
                "results": [{
                    "document_id": self.fingerprint,
                    "locator": self.locator,
                    "score": 0.75,
                    "match_methods": methods,
                }],
            }})
        if path == "/api/v1/manager/embeddings/801" and method == "DELETE":
            self.embedding_exists = False
            return response(payload={"message": "deleted"})
        raise AssertionError(f"unexpected request {method} {path} body={body!r}")

    def assert_scan_body(self, body):
        self.assertEqual(body, {
            "engine_id": 27,
            "scan_depth": "deep",
            "trigger_type": "manual",
            "force": True,
        })

    def assert_search_query(self, query, page_size):
        self.assertEqual(query["engine_id"], ["27"])
        self.assertEqual(query["page"], ["1"])
        self.assertEqual(query["page_size"], [page_size])

    def assertEqual(self, left, right):
        if left != right:
            raise AssertionError(f"{left!r} != {right!r}")


def scenario_environment(artifact_dir: str):
    return {
        "ADDP_ONLINE_ARTIFACT_DIR": artifact_dir,
        "ADDP_ONLINE_TEST_RUN_ID": "run-1",
        "ADDP_ONLINE_TEST_TENANT_ID": "42",
        "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "token",
        "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID": "27",
        "ADDP_ONLINE_MANAGER_MINIO_BUCKET": "addp-online",
        "ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT": (
            "hybrid-search/purple-gaming-light-gun.jpg"
        ),
        "ADDP_ONLINE_MANAGER_EMBEDDING_MODEL_PROFILE_ID": (
            "11111111-1111-1111-1111-111111111111"
        ),
        "GATEWAY_URL": "http://127.0.0.1:8000",
    }


class ManagerHybridSearchOnlineTest(unittest.TestCase):
    def test_runs_real_contract_and_deletes_embedding(self) -> None:
        client = FakeGatewayClient()
        with tempfile.TemporaryDirectory() as artifact_dir:
            with mock.patch.object(SUITE, "GatewayClient", return_value=client):
                report = SUITE.run_scenario(scenario_environment(artifact_dir))

        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["retrieval"]["hybrid_match_methods"], ["keyword", "vector"])
        self.assertEqual(report["retrieval"]["semantic_match_methods"], ["vector"])
        self.assertEqual(report["retrieval"]["semantic_page_size"], 1)
        self.assertTrue(report["retrieval"]["legacy_vector_hits_absent"])
        self.assertEqual(report["cleanup"], {"embedding_deleted": True, "residual_resources": 0})
        self.assertFalse(client.embedding_exists)

    def test_search_failure_still_deletes_embedding(self) -> None:
        client = FakeGatewayClient()
        client.fail_semantic_search = True
        with tempfile.TemporaryDirectory() as artifact_dir:
            with mock.patch.object(SUITE, "GatewayClient", return_value=client):
                with self.assertRaisesRegex(SUITE.SuiteError, "injected semantic search failure"):
                    SUITE.run_scenario(scenario_environment(artifact_dir))

        self.assertFalse(client.embedding_exists)
        self.assertIn(
            ("DELETE", "/api/v1/manager/embeddings/801", None),
            client.calls,
        )

    def test_rejects_stale_embedding_before_creating_execution(self) -> None:
        client = FakeGatewayClient()
        client.embedding_exists = True
        with tempfile.TemporaryDirectory() as artifact_dir:
            with mock.patch.object(SUITE, "GatewayClient", return_value=client):
                with self.assertRaisesRegex(SUITE.SuiteError, "stale embedding"):
                    SUITE.run_scenario(scenario_environment(artifact_dir))

        self.assertNotIn(
            ("POST", "/api/v1/manager/embedding_executions", mock.ANY),
            client.calls,
        )

    def test_rejects_non_uuid_profile_before_transport(self) -> None:
        environment = scenario_environment("/tmp/online-artifacts")
        environment["ADDP_ONLINE_MANAGER_EMBEDDING_MODEL_PROFILE_ID"] = "not-a-uuid"
        with mock.patch.object(SUITE, "GatewayClient") as client:
            with self.assertRaisesRegex(SUITE.SuiteError, "must be a UUID"):
                SUITE.run_scenario(environment)
        client.assert_not_called()


if __name__ == "__main__":
    unittest.main()
