import json
import os
import tempfile
import unittest
import uuid
from importlib import import_module
from pathlib import Path
from unittest.mock import patch

FIXTURE = import_module("scripts.test.quality-online-fixture")


class Gateway:
    def __init__(self, fault=None):
        self.fault, self.calls, self.tables, self.physical = fault, [], {}, []
        self.next_id = 10
        self.scan_id = str(uuid.uuid4())

    def request(self, method, path, statuses, body=None):
        self.calls.append((method, path, body))
        if method == "GET":
            payload = {"execution_id": path.rsplit("/", 1)[1], "status": "failed" if self.fault == "failed_execution" else "success"}
        elif path.endswith("/scan/run/manual"):
            payload = {"execution_id": self.scan_id, "status": "pending"}
        elif path.endswith("/fields"):
            self.tables[path.rsplit("/", 1)[0]]["version"] = 2
            payload = {"field": {"id": 99}, "version": 2}
        elif path.endswith("/approve"):
            table = self.tables[path.rsplit("/", 1)[0]]
            assert body == {"version": 2}
            table.update(status="approved", version=3)
            payload = table.copy()
        elif path.endswith("/materialized-target"):
            table = self.tables[path.rsplit("/", 1)[0]]
            assert table["status"] == "approved" and body == {"version": 3}
            self.physical.append(table["materialization"]["target_name"])
            payload = {"execution_id": str(uuid.uuid4()), "status": "pending"}
        else:
            if self.fault == "lost_create":
                raise FIXTURE.API.SuiteError("lost creation response")
            self.next_id += 1
            payload = dict(body, id=self.next_id, tenant_id=2, version=1, status="draft")
            if path == FIXTURE.QUALITY.MODEL:
                self.tables[path + "/" + str(self.next_id)] = payload
        return FIXTURE.API.Response(statuses[0], payload)


class QualityOwnerFixtureTest(unittest.TestCase):
    def test_formal_model_creation_approval_and_materialization_precede_acceptance(self):
        client, report = Gateway(), {}
        fixtures = FIXTURE.prepare(client, 2, 17, report, lambda: None)
        self.assertEqual(client.physical, ["quality_target_a", "quality_target_b"])
        self.assertEqual({f["row_count"] for f in fixtures}, {2})
        self.assertEqual(len(report["materialization_executions"]), 2)
        self.assertEqual(report["result"], "passed")
        self.assertFalse(report["uncertain_mutation"])
        self.assertEqual(client.calls[0][2]["catalog_paths"], ["public"])
        self.assertEqual(sum("/monitor/" in path for _, path, _ in client.calls), 2)
        self.assertFalse(any("task-provider" in path for _, path, _ in client.calls))

    def test_lost_create_response_preserves_uncertainty(self):
        report = {}
        with self.assertRaises(FIXTURE.API.SuiteError):
            FIXTURE.prepare(Gateway("lost_create"), 2, 17, report, lambda: None)
        self.assertTrue(report["uncertain_mutation"])
        self.assertEqual(report["result"], "failed")

    def test_failed_execution_cannot_publish_fixtures(self):
        client, report = Gateway("failed_execution"), {}
        with self.assertRaises(FIXTURE.API.SuiteError):
            FIXTURE.prepare(client, 2, 17, report, lambda: None)
        self.assertFalse(client.physical)
        self.assertNotIn("fixtures", report)

    def environment(self, root):
        return dict(os.environ, GITHUB_ACTIONS="true", RUNNER_OS="Linux", ADDP_ONLINE_HOSTED="1", ADDP_ONLINE_HOST="1",
                    POSTGRES_DB="addp_online", ADDP_ONLINE_TEST_TENANT_ID="2", ADDP_ONLINE_CONSUMER_ENGINE_ID="17",
                    ADDP_ONLINE_FIXTURE_QUALITY_ACCESS_TOKEN="private-preparer", ADDP_ONLINE_TEST_USER_ACCESS_TOKEN="private-consumer",
                    SYSTEM_URL="http://127.0.0.1:8180", GATEWAY_URL="http://127.0.0.1:8000",
                    ADDP_ONLINE_SECRET_DIR=str(root), ADDP_ONLINE_ARTIFACT_DIR=str(root))

    def test_private_environment_and_safe_evidence_use_distinct_users(self):
        with tempfile.TemporaryDirectory(prefix="addp-quality-fixture-") as temporary:
            root, client = Path(temporary), Gateway()
            with patch.dict(os.environ, self.environment(root), clear=True), patch.object(FIXTURE.API, "GatewayClient", return_value=client), \
                    patch.object(FIXTURE.API, "validate_user_identity", side_effect=[{"principal_id": "4"}, {"principal_id": "5"}]):
                FIXTURE.main("prepare")
            destination = root / "quality-fixtures.env"
            self.assertEqual(destination.stat().st_mode & 0o777, 0o600)
            self.assertIn("export ADDP_ONLINE_QUALITY_FIXTURES_JSON=", destination.read_text())
            report = json.loads((root / "quality-fixture.json").read_text())
            self.assertEqual(report["preparation_identity"]["principal_id"], "4")
            self.assertNotIn("private-", json.dumps(report))

    def test_same_user_is_rejected_before_preparation(self):
        with tempfile.TemporaryDirectory(prefix="addp-quality-fixture-") as temporary:
            root = Path(temporary)
            with patch.dict(os.environ, self.environment(root), clear=True), patch.object(FIXTURE.API, "GatewayClient"), \
                    patch.object(FIXTURE.API, "validate_user_identity", return_value={"principal_id": "4"}), self.assertRaises(FIXTURE.API.SuiteError):
                FIXTURE.main("prepare")
            self.assertFalse((root / "quality-fixtures.env").exists())


if __name__ == "__main__":
    unittest.main()
