import io
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from interactive_sessions import InteractiveSessionManager, SessionConflictError, SessionValidationError


class _ObjectResponse(io.BytesIO):
    def release_conn(self):
        return None


class _Minio:
    def __init__(self):
        self.content = b'{"cells": [], "metadata": {}, "nbformat": 4, "nbformat_minor": 5}'
        self.puts = []

    def get_object(self, bucket, object_name):
        return _ObjectResponse(self.content)

    def put_object(self, bucket, object_name, source, length, content_type):
        self.puts.append((bucket, object_name, source.read(), length, content_type))


class _Process:
    def __init__(self):
        self.running = True

    def poll(self):
        return None if self.running else 0

    def terminate(self):
        self.running = False

    def wait(self, timeout=None):
        self.running = False
        return 0

    def kill(self):
        self.running = False


class InteractiveSessionManagerTests(unittest.TestCase):
    def test_create_and_close_saves_same_notebook_object(self):
        minio = _Minio()
        with tempfile.TemporaryDirectory() as workspace:
            manager = InteractiveSessionManager(minio, "develop", workspace, 300)
            process = _Process()
            request = {
                "session_id": "abc-123",
                "tenant_id": 7,
                "user_id": 9,
                "task_id": 11,
                "notebook_path": "analysis.ipynb",
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
            with patch.object(manager, "_available_port", return_value=31000), patch.object(
                manager, "_start_jupyter", return_value=process
            ), patch.object(manager, "_wait_until_ready"):
                session = manager.create(request, "jupyter-engine")

            self.assertEqual(session.endpoint, "http://jupyter-engine:31000")
            self.assertNotIn("owner_api_endpoint", session.response())
            self.assertNotIn("owner_catalog_api_endpoint", session.response())
            self.assertNotIn("owner_capability_token", session.response())
            self.assertTrue(Path(session.notebook_file).exists())
            manager.close("abc-123", tenant_id=7)

            self.assertFalse(Path(session.workspace).exists())
            self.assertFalse(process.running)
            self.assertEqual(minio.puts[0][1], "tenant_7/notebooks/analysis.ipynb")
            manager.shutdown()

    def test_rejects_second_writer_for_same_notebook(self):
        minio = _Minio()
        with tempfile.TemporaryDirectory() as workspace:
            manager = InteractiveSessionManager(minio, "develop", workspace, 300)
            base = {
                "session_id": "abc-123",
                "tenant_id": 7,
                "user_id": 9,
                "task_id": 11,
                "notebook_path": "analysis.ipynb",
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
            with patch.object(manager, "_available_port", return_value=31000), patch.object(
                manager, "_start_jupyter", return_value=_Process()
            ), patch.object(manager, "_wait_until_ready"):
                manager.create(base, "jupyter-engine")
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
            manager.shutdown()

    def test_start_jupyter_injects_only_session_owner_capability(self):
        with tempfile.TemporaryDirectory() as workspace:
            Path(workspace, "analysis.ipynb").write_text("{}", encoding="utf-8")
            with patch("interactive_sessions.subprocess.Popen") as popen:
                InteractiveSessionManager._start_jupyter(
                    workspace,
                    "/api/v1/develop/notebook-sessions/abc-123/",
                    31000,
                    "runtime-secret",
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/engine-descriptors",
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/catalog/children",
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/table-scans",
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/record-scans",
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/queries",
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/graph-samples",
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/graph-queries",
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/content-reads",
                    "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/change-streams",
                    "addp_nkc_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
                )

            environment = popen.call_args.kwargs["env"]
            self.assertEqual(
                environment["ADDP_NOTEBOOK_OWNER_API_ENDPOINT"],
                "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/engine-descriptors",
            )
            self.assertEqual(
                environment["ADDP_NOTEBOOK_CATALOG_API_ENDPOINT"],
                "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/catalog/children",
            )
            self.assertEqual(
                environment["ADDP_NOTEBOOK_TABLE_SCAN_API_ENDPOINT"],
                "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/table-scans",
            )
            self.assertEqual(
                environment["ADDP_NOTEBOOK_QUERY_API_ENDPOINT"],
                "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/queries",
            )
            self.assertEqual(
                environment["ADDP_NOTEBOOK_RECORD_SCAN_API_ENDPOINT"],
                "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/record-scans",
            )
            self.assertEqual(
                environment["ADDP_NOTEBOOK_GRAPH_SAMPLE_API_ENDPOINT"],
                "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/graph-samples",
            )
            self.assertEqual(
                environment["ADDP_NOTEBOOK_GRAPH_QUERY_API_ENDPOINT"],
                "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/graph-queries",
            )
            self.assertEqual(
                environment["ADDP_NOTEBOOK_CONTENT_READ_API_ENDPOINT"],
                "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/content-reads",
            )
            self.assertEqual(
                environment["ADDP_NOTEBOOK_CHANGE_STREAM_API_ENDPOINT"],
                "http://develop:8185/api/v1/develop/notebook-kernel-sessions/abc-123/change-streams",
            )
            self.assertEqual(
                environment["ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN"],
                "addp_nkc_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            )
            self.assertNotIn("ADDP_TOKEN", environment)

    def test_validate_request_rejects_missing_or_invalid_owner_capability(self):
        with tempfile.TemporaryDirectory() as workspace:
            manager = InteractiveSessionManager(_Minio(), "develop", workspace, 300)
            request = {
                "session_id": "abc-123",
                "tenant_id": 7,
                "user_id": 9,
                "task_id": 11,
                "notebook_path": "analysis.ipynb",
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
            manager = InteractiveSessionManager(_Minio(), "develop", workspace, 300)
            request = {
                "session_id": "abc-123",
                "tenant_id": 7,
                "user_id": 9,
                "task_id": 11,
                "notebook_path": "analysis.ipynb",
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
