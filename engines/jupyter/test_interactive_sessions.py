import io
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from execution_container import ExecutionContainer

from interactive_sessions import InteractiveSessionManager, SessionConflictError, SessionValidationError


class _ObjectResponse(io.BytesIO):
    def release_conn(self):
        return None


class _Minio:
    def __init__(self):
        self.content = b'{"cells": [], "metadata": {}, "nbformat": 4, "nbformat_minor": 5}'
        self.puts = []

    def open(self, object_name):
        return _ObjectResponse(self.content)

    def put(self, object_name, source, size, media_type):
        self.puts.append(("develop", object_name, source.read(), size, media_type))


class InteractiveSessionManagerTests(unittest.TestCase):
    def test_create_and_close_saves_same_notebook_object(self):
        minio = _Minio()
        with tempfile.TemporaryDirectory() as workspace:
            manager = InteractiveSessionManager(minio, workspace, 300)
            request = {
                "session_id": "abc-123",
                "tenant_id": 7,
                "user_id": 9,
                "task_id": 11,
                "notebook_locator": "addp-infra://object/develop/tenant_7/notebooks/analysis.ipynb?type=object",
                "kernel": "python3",
                "base_path": "/api/v1/develop/notebook-sessions/abc-123/",
                "ttl_seconds": 120,
                "owner_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/engine-descriptors",
                "owner_catalog_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/catalog/children",
                "owner_table_scan_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/table-scans",
                "owner_record_scan_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/record-scans",
                "owner_query_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/queries",
                "owner_graph_sample_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/graph-samples",
                "owner_graph_query_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/graph-queries",
                "owner_content_read_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/content-reads",
                "owner_change_stream_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/change-streams",
                "owner_capability_token": "addp_nkc_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            }
            with patch.dict(os.environ, {"ADDP_SYNTHETIC_MARKER": "api-only"}), patch.object(
                ExecutionContainer, "start"
            ) as start, patch.object(manager, "_wait_until_ready"):
                session = manager.create(request, "jupyter-engine:8097")
            start.assert_called_once_with("lab", 120)
            self.assertEqual(session.endpoint, "http://jupyter-engine:8097")
            job = json.loads((session.container.workspace / "job.json").read_text())
            environment = job["session_environment"]
            self.assertNotIn("ADDP_SYNTHETIC_MARKER", environment)
            for key, value in request.items():
                if key.startswith("owner_") and key.endswith("_endpoint"):
                    variable = "ADDP_NOTEBOOK_" + key.removeprefix("owner_").upper()
                    if key == "owner_api_endpoint":
                        variable = "ADDP_NOTEBOOK_OWNER_API_ENDPOINT"
                    self.assertEqual(environment[variable], value.replace("http://develop:8185", "http://nb-control:8097"))
            self.assertEqual(environment["ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN"], request["owner_capability_token"])
            self.assertNotIn("owner_api_endpoint", session.response())
            self.assertNotIn("owner_catalog_api_endpoint", session.response())
            self.assertNotIn("owner_capability_token", session.response())
            self.assertTrue((session.container.workspace / "files/analysis.ipynb").exists())
            with patch.object(session.container.http, "delete"):
                manager.close("abc-123", tenant_id=7)
            self.assertFalse(session.container.workspace.exists())
            self.assertEqual(minio.puts[0][1], "tenant_7/notebooks/analysis.ipynb")
            manager.shutdown()

    def test_rejects_second_writer_for_same_notebook(self):
        minio = _Minio()
        with tempfile.TemporaryDirectory() as workspace:
            manager = InteractiveSessionManager(minio, workspace, 300)
            base = {
                "session_id": "abc-123",
                "tenant_id": 7,
                "user_id": 9,
                "task_id": 11,
                "notebook_locator": "addp-infra://object/develop/tenant_7/notebooks/analysis.ipynb?type=object",
                "kernel": "python3",
                "base_path": "/api/v1/develop/notebook-sessions/abc-123/",
                "ttl_seconds": 120,
                "owner_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/engine-descriptors",
                "owner_catalog_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/catalog/children",
                "owner_table_scan_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/table-scans",
                "owner_record_scan_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/record-scans",
                "owner_query_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/queries",
                "owner_graph_sample_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/graph-samples",
                "owner_graph_query_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/graph-queries",
                "owner_content_read_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/content-reads",
                "owner_change_stream_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/change-streams",
                "owner_capability_token": "addp_nkc_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            }
            with patch.object(ExecutionContainer, "start"), patch.object(manager, "_wait_until_ready"):
                session = manager.create(base, "jupyter-engine:8097")
                second = dict(base)
                second["session_id"] = "def-456"
                second["base_path"] = "/api/v1/develop/notebook-sessions/def-456/"
                second["owner_api_endpoint"] = (
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/def-456/engine-descriptors"
                )
                second["owner_catalog_api_endpoint"] = (
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/def-456/catalog/children"
                )
                second["owner_table_scan_api_endpoint"] = (
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/def-456/table-scans"
                )
                second["owner_record_scan_api_endpoint"] = (
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/def-456/record-scans"
                )
                second["owner_query_api_endpoint"] = (
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/def-456/queries"
                )
                second["owner_graph_sample_api_endpoint"] = (
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/def-456/graph-samples"
                )
                second["owner_graph_query_api_endpoint"] = (
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/def-456/graph-queries"
                )
                second["owner_content_read_api_endpoint"] = (
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/def-456/content-reads"
                )
                second["owner_change_stream_api_endpoint"] = (
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/def-456/change-streams"
                )
                with self.assertRaises(SessionConflictError):
                    manager.create(second, "jupyter-engine")
            with patch.object(session.container.http, "delete"):
                manager.shutdown()

    def test_validate_request_rejects_missing_or_invalid_owner_capability(self):
        with tempfile.TemporaryDirectory() as workspace:
            manager = InteractiveSessionManager(_Minio(), workspace, 300)
            request = {
                "session_id": "abc-123",
                "tenant_id": 7,
                "user_id": 9,
                "task_id": 11,
                "notebook_locator": "addp-infra://object/develop/tenant_7/notebooks/analysis.ipynb?type=object",
                "kernel": "python3",
                "base_path": "/api/v1/develop/notebook-sessions/abc-123/",
                "ttl_seconds": 120,
                "owner_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/engine-descriptors",
                "owner_catalog_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/catalog/children",
                "owner_table_scan_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/table-scans",
                "owner_record_scan_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/record-scans",
                "owner_query_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/queries",
                "owner_graph_sample_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/graph-samples",
                "owner_graph_query_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/graph-queries",
                "owner_content_read_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/content-reads",
                "owner_change_stream_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/change-streams",
            }
            with self.assertRaisesRegex(Exception, "owner_capability_token"):
                manager._validate_request(request)
            request["owner_capability_token"] = "wrong-prefix"
            with self.assertRaisesRegex(Exception, "owner_capability_token"):
                manager._validate_request(request)
            manager.shutdown()

    def test_validate_request_rejects_cross_scheme_data_endpoint(self):
        with tempfile.TemporaryDirectory() as workspace:
            manager = InteractiveSessionManager(_Minio(), workspace, 300)
            request = {
                "session_id": "abc-123",
                "tenant_id": 7,
                "user_id": 9,
                "task_id": 11,
                "notebook_locator": "addp-infra://object/develop/tenant_7/notebooks/analysis.ipynb?type=object",
                "kernel": "python3",
                "base_path": "/api/v1/develop/notebook-sessions/abc-123/",
                "ttl_seconds": 120,
                "owner_api_endpoint": "https://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/engine-descriptors",
                "owner_catalog_api_endpoint": "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/catalog/children",
                "owner_table_scan_api_endpoint": "https://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/table-scans",
                "owner_record_scan_api_endpoint": "https://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/record-scans",
                "owner_query_api_endpoint": "https://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/queries",
                "owner_graph_sample_api_endpoint": "https://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/graph-samples",
                "owner_graph_query_api_endpoint": "https://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/graph-queries",
                "owner_content_read_api_endpoint": "https://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/content-reads",
                "owner_change_stream_api_endpoint": "https://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/change-streams",
                "owner_capability_token": "addp_nkc_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            }

            with self.assertRaisesRegex(SessionValidationError, "owner_catalog_api_endpoint"):
                manager._validate_request(request)
            manager.shutdown()


if __name__ == "__main__":
    unittest.main()
