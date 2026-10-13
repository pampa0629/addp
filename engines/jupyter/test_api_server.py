import hashlib
import io
import importlib
import sys
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch, MagicMock


REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPOSITORY_ROOT / "common-python"))

with patch("minio.Minio") as minio_class:
    minio_class.return_value.bucket_exists.return_value = True
    api_server = importlib.import_module("api_server")


def _context(*, client_id="addp-develop", tenant_id=7):
    return SimpleNamespace(
        principal_type="service_principal",
        token_type="service_access_token",
        client_id=client_id,
        context_type="tenant",
        tenant_id=tenant_id,
    )


class NotebookRuntimeAuthTests(unittest.TestCase):
    def setUp(self):
        self.client = api_server.app.test_client()

    def test_kernels_requires_bearer_token(self):
        response = self.client.get("/api/kernels")
        self.assertEqual(response.status_code, 401)

    def test_kernels_rejects_other_service_client(self):
        async def resolve(*_args):
            return _context(client_id="addp-meta")

        with patch.object(api_server, "resolve_authorization_context", resolve):
            response = self.client.get("/api/kernels", headers={"Authorization": "Bearer token"})

        self.assertEqual(response.status_code, 403)

    def test_kernels_accepts_develop_service(self):
        async def resolve(*_args):
            return _context()

        with patch.object(api_server, "resolve_authorization_context", resolve):
            response = self.client.get("/api/kernels", headers={"Authorization": "Bearer token"})

        self.assertEqual(response.status_code, 200)

    def test_execute_rejects_tenant_mismatch_before_runtime_access(self):
        async def resolve(*_args):
            return _context(tenant_id=7)

        with patch.object(api_server, "resolve_authorization_context", resolve):
            response = self.client.post(
                "/api/execute",
                headers={"Authorization": "Bearer token"},
                json={"tenant_id": 8, "notebook_path": "analysis.ipynb"},
            )

        self.assertEqual(response.status_code, 403)

    def test_execute_rejects_legacy_data_source_injection(self):
        async def resolve(*_args):
            return _context()

        with patch.object(api_server, "resolve_authorization_context", resolve):
            response = self.client.post(
                "/api/execute",
                headers={"Authorization": "Bearer token"},
                json={
                    "tenant_id": 7,
                    "notebook_path": "analysis.ipynb",
                    "parameters": {"ds_2": {"connection_string": "secret"}},
                },
            )

        self.assertEqual(response.status_code, 400)

    def test_create_interactive_session_rejects_tenant_mismatch(self):
        async def resolve(*_args):
            return _context(tenant_id=7)

        with patch.object(api_server, "resolve_authorization_context", resolve), patch.object(
            api_server.interactive_session_manager, "create"
        ) as create:
            response = self.client.post(
                "/api/interactive-sessions",
                headers={"Authorization": "Bearer token"},
                json={"tenant_id": 8},
            )

        self.assertEqual(response.status_code, 403)
        create.assert_not_called()

    def test_create_interactive_session_returns_internal_session_facts(self):
        async def resolve(*_args):
            return _context(tenant_id=7)

        session = SimpleNamespace(
            response=lambda: {
                "session_id": "abc",
                "endpoint": "http://jupyter:31000",
                "runtime_token": "secret",
                "notebook_name": "analysis.ipynb",
                "expires_at": "2026-08-02T00:00:00Z",
            }
        )
        with patch.object(api_server, "resolve_authorization_context", resolve), patch.object(
            api_server.interactive_session_manager, "create", return_value=session
        ):
            response = self.client.post(
                "/api/interactive-sessions",
                headers={"Authorization": "Bearer token"},
                json={"tenant_id": 7},
            )

        self.assertEqual(response.status_code, 201)
        self.assertEqual(response.get_json()["runtime_token"], "secret")

    def test_close_interactive_session_is_tenant_scoped(self):
        async def resolve(*_args):
            return _context(tenant_id=7)

        with patch.object(api_server, "resolve_authorization_context", resolve), patch.object(
            api_server.interactive_session_manager, "close"
        ) as close:
            response = self.client.delete(
                "/api/interactive-sessions/abc",
                headers={"Authorization": "Bearer token"},
            )

        self.assertEqual(response.status_code, 204)
        close.assert_called_once_with("abc", 7)


if __name__ == "__main__":
    unittest.main()


class NotebookContentIdentityTests(unittest.TestCase):
    def setUp(self):
        self.client = api_server.app.test_client()
        self.content = b'{"cells": [], "nbformat": 4, "nbformat_minor": 5}'
        self.digest = hashlib.sha256(self.content).hexdigest()

    def request(self, **values):
        async def resolve(*_args):
            return _context()
        payload = dict(tenant_id=7, notebook_path="analysis.ipynb", notebook_content_sha256=self.digest)
        payload.update(values)
        with patch.object(api_server, "resolve_authorization_context", resolve):
            return self.client.post("/api/execute", headers={"Authorization": "Bearer token"}, json=payload)

    def test_invalid_digest_and_path_rejected_before_storage_access(self):
        for values in [{"notebook_content_sha256": None}, {"notebook_content_sha256": self.digest.upper()},
                       {"notebook_content_sha256": "a" * 63}, {"notebook_path": "../other.ipynb"},
                       {"notebook_path": "a/../other.ipynb"}, {"notebook_path": "/other.ipynb"},
                       {"notebook_path": "a\\other.ipynb"}]:
            with self.subTest(values=values), patch.object(api_server, "minio_client") as store, patch.object(api_server.pm, "execute_notebook") as execute:
                self.assertEqual(self.request(**values).status_code, 400)
                store.get_object.assert_not_called()
                store.put_object.assert_not_called()
                execute.assert_not_called()

    def test_changed_download_never_executes_or_publishes(self):
        with patch.object(api_server, "minio_client") as store, patch.object(api_server.pm, "execute_notebook") as execute:
            download = MagicMock()
            download.read.return_value = self.content + b"\n"
            store.get_object.return_value = download
            response = self.request()
            self.assertEqual(response.status_code, 409)
            self.assertEqual(response.get_json()["error_code"], "notebook_content_changed")
            execute.assert_not_called()
            store.put_object.assert_not_called()
            download.close.assert_called_once()
            download.release_conn.assert_called_once()

    def test_verified_bytes_are_the_bytes_executed(self):
        def execute(input_path, output_path, **_kwargs):
            self.assertEqual(Path(input_path).read_bytes(), self.content)
            Path(output_path).write_bytes(b"{}")
        with patch.object(api_server, "minio_client") as store, patch.object(api_server.pm, "execute_notebook", execute), patch.object(api_server.nbformat, "read", return_value=SimpleNamespace(cells=[])):
            download = MagicMock()
            download.read.return_value = self.content
            store.get_object.return_value = download
            response = self.request()
            self.assertEqual(response.status_code, 200)
            store.get_object.assert_called_once_with(api_server.config.MINIO_BUCKET, "tenant_7/notebooks/analysis.ipynb")
            store.put_object.assert_called_once()
            self.assertEqual(response.get_json()["cell_count"], 0)
