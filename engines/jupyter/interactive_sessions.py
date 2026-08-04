import atexit
import io
import logging
import os
import re
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import quote
from urllib.parse import urlsplit

import httpx


logger = logging.getLogger(__name__)


class SessionValidationError(ValueError):
    pass


class SessionConflictError(RuntimeError):
    pass


class SessionNotFoundError(KeyError):
    pass


@dataclass
class InteractiveSession:
    session_id: str
    tenant_id: int
    user_id: int
    task_id: int
    notebook_path: str
    notebook_name: str
    base_path: str
    endpoint: str
    runtime_token: str
    expires_at: datetime
    workspace: str
    notebook_file: str
    process: subprocess.Popen

    def response(self):
        return {
            "session_id": self.session_id,
            "endpoint": self.endpoint,
            "runtime_token": self.runtime_token,
            "notebook_name": self.notebook_name,
            "expires_at": self.expires_at.isoformat().replace("+00:00", "Z"),
        }


class InteractiveSessionManager:
    def __init__(self, minio_client, bucket, workspace_root, max_ttl_seconds=3600):
        self._minio = minio_client
        self._bucket = bucket
        self._workspace_root = Path(workspace_root)
        self._workspace_root.mkdir(parents=True, exist_ok=True)
        self._max_ttl_seconds = max(60, int(max_ttl_seconds))
        self._sessions = {}
        self._lock = threading.RLock()
        self._stopped = threading.Event()
        self._reaper = threading.Thread(target=self._reap_expired, name="jupyter-session-reaper", daemon=True)
        self._reaper.start()
        atexit.register(self.shutdown)

    def create(self, data, runtime_host):
        values = self._validate_request(data)
        if self._minio is None:
            raise RuntimeError("MinIO client is not initialized")

        with self._lock:
            if values["session_id"] in self._sessions:
                raise SessionConflictError("session already exists")
            if any(
                session.tenant_id == values["tenant_id"]
                and session.notebook_path == values["notebook_path"]
                for session in self._sessions.values()
            ):
                raise SessionConflictError("notebook already has an active interactive session")

        workspace = tempfile.mkdtemp(prefix=f"session-{values['session_id']}-", dir=self._workspace_root)
        notebook_name = os.path.basename(values["notebook_path"])
        notebook_file = os.path.join(workspace, notebook_name)
        process = None
        try:
            object_name = f"tenant_{values['tenant_id']}/notebooks/{values['notebook_path']}"
            response = self._minio.get_object(self._bucket, object_name)
            try:
                with open(notebook_file, "wb") as destination:
                    shutil.copyfileobj(response, destination)
            finally:
                response.close()
                response.release_conn()

            port = self._available_port()
            runtime_token = secrets.token_urlsafe(32)
            endpoint = f"http://{runtime_host}:{port}"
            process = self._start_jupyter(
                workspace=workspace,
                base_path=values["base_path"],
                port=port,
                runtime_token=runtime_token,
                owner_api_endpoint=values["owner_api_endpoint"],
                owner_catalog_api_endpoint=values["owner_catalog_api_endpoint"],
                owner_table_scan_api_endpoint=values["owner_table_scan_api_endpoint"],
                owner_record_scan_api_endpoint=values["owner_record_scan_api_endpoint"],
                owner_query_api_endpoint=values["owner_query_api_endpoint"],
                owner_graph_sample_api_endpoint=values["owner_graph_sample_api_endpoint"],
                owner_graph_query_api_endpoint=values["owner_graph_query_api_endpoint"],
                owner_content_read_api_endpoint=values["owner_content_read_api_endpoint"],
                owner_change_stream_api_endpoint=values["owner_change_stream_api_endpoint"],
                owner_capability_token=values["owner_capability_token"],
            )
            self._wait_until_ready(endpoint, values["base_path"], runtime_token, process)
            expires_at = datetime.fromtimestamp(time.time() + values["ttl_seconds"], tz=timezone.utc)
            session = InteractiveSession(
                session_id=values["session_id"],
                tenant_id=values["tenant_id"],
                user_id=values["user_id"],
                task_id=values["task_id"],
                notebook_path=values["notebook_path"],
                notebook_name=notebook_name,
                base_path=values["base_path"],
                endpoint=endpoint,
                runtime_token=runtime_token,
                expires_at=expires_at,
                workspace=workspace,
                notebook_file=notebook_file,
                process=process,
            )
            with self._lock:
                self._sessions[session.session_id] = session
            logger.info(
                "created interactive notebook session id=%s tenant=%s task=%s expires_at=%s",
                session.session_id,
                session.tenant_id,
                session.task_id,
                session.expires_at.isoformat(),
            )
            return session
        except Exception:
            if process is not None:
                self._terminate(process)
            shutil.rmtree(workspace, ignore_errors=True)
            raise

    def close(self, session_id, tenant_id=None):
        with self._lock:
            session = self._sessions.get(session_id)
            if session is not None and tenant_id is not None and session.tenant_id != tenant_id:
                session = None
            if session is not None:
                self._sessions.pop(session_id, None)
        if session is None:
            raise SessionNotFoundError(session_id)
        self._save_and_cleanup(session)

    def shutdown(self):
        if self._stopped.is_set():
            return
        self._stopped.set()
        with self._lock:
            sessions = list(self._sessions.values())
            self._sessions.clear()
        for session in sessions:
            try:
                self._save_and_cleanup(session)
            except Exception:
                logger.exception("failed to close interactive notebook session id=%s", session.session_id)

    def _save_and_cleanup(self, session):
        save_error = None
        try:
            with open(session.notebook_file, "rb") as source:
                data = source.read()
            object_name = f"tenant_{session.tenant_id}/notebooks/{session.notebook_path}"
            self._minio.put_object(
                self._bucket,
                object_name,
                io.BytesIO(data),
                length=len(data),
                content_type="application/x-ipynb+json",
            )
            logger.info(
                "saved interactive notebook session id=%s tenant=%s task=%s",
                session.session_id,
                session.tenant_id,
                session.task_id,
            )
        except Exception as exc:
            save_error = exc
        finally:
            self._terminate(session.process)
            shutil.rmtree(session.workspace, ignore_errors=True)
        if save_error is not None:
            raise save_error

    def _reap_expired(self):
        while not self._stopped.wait(15):
            now = datetime.now(tz=timezone.utc)
            with self._lock:
                expired_ids = [
                    session_id
                    for session_id, session in self._sessions.items()
                    if session.expires_at <= now or session.process.poll() is not None
                ]
            for session_id in expired_ids:
                try:
                    self.close(session_id)
                except SessionNotFoundError:
                    continue
                except Exception:
                    logger.exception("failed to reap interactive notebook session id=%s", session_id)

    def _validate_request(self, data):
        if not isinstance(data, dict):
            raise SessionValidationError("request body must be a JSON object")
        session_id = data.get("session_id")
        if not isinstance(session_id, str) or not session_id or any(
            character not in "0123456789abcdef-" for character in session_id.lower()
        ):
            raise SessionValidationError("session_id is invalid")
        positive_ids = {}
        for field in ("tenant_id", "user_id", "task_id"):
            value = data.get(field)
            if not isinstance(value, int) or isinstance(value, bool) or value <= 0:
                raise SessionValidationError(f"{field} must be a positive integer")
            positive_ids[field] = value
        notebook_path = data.get("notebook_path")
        if (
            not isinstance(notebook_path, str)
            or not notebook_path.endswith(".ipynb")
            or notebook_path.startswith("/")
            or ".." in Path(notebook_path).parts
        ):
            raise SessionValidationError("notebook_path is invalid")
        if data.get("kernel") != "python3":
            raise SessionValidationError("unsupported kernel")
        base_path = data.get("base_path")
        expected_prefix = f"/api/v1/develop/notebook-sessions/{session_id}/"
        if base_path != expected_prefix:
            raise SessionValidationError("base_path does not match session_id")
        ttl_seconds = data.get("ttl_seconds", self._max_ttl_seconds)
        if not isinstance(ttl_seconds, int) or isinstance(ttl_seconds, bool) or ttl_seconds <= 0:
            raise SessionValidationError("ttl_seconds must be a positive integer")
        owner_api_endpoint = data.get("owner_api_endpoint")
        if not isinstance(owner_api_endpoint, str):
            raise SessionValidationError("owner_api_endpoint is invalid")
        owner_url = urlsplit(owner_api_endpoint)
        expected_owner_path = f"/api/v1/develop/notebook-kernel-sessions/{session_id}/engine-descriptors"
        if (
            owner_url.scheme not in {"http", "https"}
            or not owner_url.netloc
            or owner_url.username is not None
            or owner_url.password is not None
            or owner_url.path != expected_owner_path
            or owner_url.query
            or owner_url.fragment
        ):
            raise SessionValidationError("owner_api_endpoint is invalid")
        endpoint_suffixes = {
            "owner_catalog_api_endpoint": "catalog/children",
            "owner_table_scan_api_endpoint": "table-scans",
            "owner_record_scan_api_endpoint": "record-scans",
            "owner_query_api_endpoint": "queries",
            "owner_graph_sample_api_endpoint": "graph-samples",
            "owner_graph_query_api_endpoint": "graph-queries",
            "owner_content_read_api_endpoint": "content-reads",
            "owner_change_stream_api_endpoint": "change-streams",
        }
        owner_endpoints = {}
        for field, suffix in endpoint_suffixes.items():
            endpoint = data.get(field)
            if not isinstance(endpoint, str):
                raise SessionValidationError(f"{field} is invalid")
            endpoint_url = urlsplit(endpoint)
            expected_path = f"/api/v1/develop/notebook-kernel-sessions/{session_id}/{suffix}"
            if (
                endpoint_url.scheme not in {"http", "https"}
                or endpoint_url.scheme != owner_url.scheme
                or endpoint_url.netloc != owner_url.netloc
                or endpoint_url.username is not None
                or endpoint_url.password is not None
                or endpoint_url.path != expected_path
                or endpoint_url.query
                or endpoint_url.fragment
            ):
                raise SessionValidationError(f"{field} is invalid")
            owner_endpoints[field] = endpoint
        owner_capability_token = data.get("owner_capability_token")
        if not isinstance(owner_capability_token, str) or re.fullmatch(
            r"addp_nkc_[A-Za-z0-9_-]{43}", owner_capability_token
        ) is None:
            raise SessionValidationError("owner_capability_token is invalid")
        return {
            "session_id": session_id,
            **positive_ids,
            "notebook_path": notebook_path,
            "kernel": "python3",
            "base_path": base_path,
            "ttl_seconds": min(ttl_seconds, self._max_ttl_seconds),
            "owner_api_endpoint": owner_api_endpoint,
            **owner_endpoints,
            "owner_capability_token": owner_capability_token,
        }

    @staticmethod
    def _available_port():
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            sock.bind(("", 0))
            return sock.getsockname()[1]

    @staticmethod
    def _start_jupyter(
        workspace,
        base_path,
        port,
        runtime_token,
        owner_api_endpoint,
        owner_catalog_api_endpoint,
        owner_table_scan_api_endpoint,
        owner_record_scan_api_endpoint,
        owner_query_api_endpoint,
        owner_graph_sample_api_endpoint,
        owner_graph_query_api_endpoint,
        owner_content_read_api_endpoint,
        owner_change_stream_api_endpoint,
        owner_capability_token,
    ):
        command = [
            sys.executable,
            "-m",
            "jupyterlab",
            "--no-browser",
            "--ServerApp.ip=0.0.0.0",
            f"--ServerApp.port={port}",
            "--ServerApp.port_retries=0",
            f"--ServerApp.base_url={base_path}",
            f"--ServerApp.root_dir={workspace}",
            f"--ServerApp.token={runtime_token}",
            "--ServerApp.password=",
            "--ServerApp.allow_remote_access=True",
            "--ServerApp.trust_xheaders=True",
            "--ServerApp.quit_button=False",
            f"--LabApp.default_url=/lab/tree/{quote(os.path.basename(next(Path(workspace).glob('*.ipynb'))))}",
        ]
        environment = os.environ.copy()
        for key in tuple(environment):
            upper_key = key.upper()
            if upper_key.startswith("MINIO_") or any(
                marker in upper_key
                for marker in ("TOKEN", "SECRET", "PASSWORD", "CREDENTIAL", "API_KEY", "ACCESS_KEY", "PRIVATE_KEY")
            ):
                environment.pop(key, None)
        environment["ADDP_NOTEBOOK_OWNER_API_ENDPOINT"] = owner_api_endpoint
        environment["ADDP_NOTEBOOK_CATALOG_API_ENDPOINT"] = owner_catalog_api_endpoint
        environment["ADDP_NOTEBOOK_TABLE_SCAN_API_ENDPOINT"] = owner_table_scan_api_endpoint
        environment["ADDP_NOTEBOOK_RECORD_SCAN_API_ENDPOINT"] = owner_record_scan_api_endpoint
        environment["ADDP_NOTEBOOK_QUERY_API_ENDPOINT"] = owner_query_api_endpoint
        environment["ADDP_NOTEBOOK_GRAPH_SAMPLE_API_ENDPOINT"] = owner_graph_sample_api_endpoint
        environment["ADDP_NOTEBOOK_GRAPH_QUERY_API_ENDPOINT"] = owner_graph_query_api_endpoint
        environment["ADDP_NOTEBOOK_CONTENT_READ_API_ENDPOINT"] = owner_content_read_api_endpoint
        environment["ADDP_NOTEBOOK_CHANGE_STREAM_API_ENDPOINT"] = owner_change_stream_api_endpoint
        environment["ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN"] = owner_capability_token
        return subprocess.Popen(
            command,
            cwd=workspace,
            env=environment,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            start_new_session=True,
        )

    @staticmethod
    def _wait_until_ready(endpoint, base_path, runtime_token, process):
        status_url = endpoint + base_path + "api/status"
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            if process.poll() is not None:
                raise RuntimeError("JupyterLab process exited during startup")
            try:
                response = httpx.get(
                    status_url,
                    headers={"Authorization": f"token {runtime_token}"},
                    timeout=1,
                )
                if response.status_code == 200:
                    return
            except httpx.RequestError:
                pass
            time.sleep(0.2)
        raise RuntimeError("JupyterLab session startup timed out")

    @staticmethod
    def _terminate(process):
        if process.poll() is not None:
            return
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)
