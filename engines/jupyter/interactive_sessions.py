import atexit
import io
from contextlib import closing
import logging
import os
import re
import secrets
import hmac
import threading
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from execution_container import ExecutionContainer
from urllib.parse import urlsplit
from addp_common.infra.objectstore import Ref

import httpx


logger = logging.getLogger(__name__)


class SessionValidationError(ValueError):
    pass


class SessionScopeError(PermissionError):
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
    notebook_locator: str
    notebook_name: str
    base_path: str
    endpoint: str
    runtime_token: str
    expires_at: datetime
    container: ExecutionContainer
    owner_routes: dict
    capability: str
    lab_token: str

    def response(self):
        return {
            "session_id": self.session_id,
            "endpoint": self.endpoint,
            "runtime_token": self.runtime_token,
            "notebook_name": self.notebook_name,
            "expires_at": self.expires_at.isoformat().replace("+00:00", "Z"),
        }


class InteractiveSessionManager:
    def __init__(self, store, workspace_root, max_ttl_seconds=3600):
        self._store = store
        self._workspace_root = Path(workspace_root)
        self._workspace_root.mkdir(parents=True, exist_ok=True)
        self._max_ttl_seconds = max(60, int(max_ttl_seconds))
        self._sessions = {}
        self._opening = {}
        self._closing = set()
        self._lock = threading.RLock()
        self._stopped = threading.Event()
        self._reaper = threading.Thread(target=self._reap_expired, name="jupyter-session-reaper", daemon=True)
        self._reaper.start()
        atexit.register(self.shutdown)

    def create(self, data, runtime_authority):
        values = self._validate_request(data)
        if self._store is None:
            raise RuntimeError("Notebook object store unavailable")

        key = values["session_id"]
        container = ExecutionContainer(self._workspace_root, key)
        object_name = values["notebook_ref"].key
        name = os.path.basename(object_name)
        token = secrets.token_urlsafe(32)
        lab_token = secrets.token_urlsafe(32)
        routes = {}
        environment = {"ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN": values["owner_capability_token"]}
        for field, url in values.items():
            if field.startswith("owner_") and field.endswith("_endpoint"):
                path = urlsplit(url).path
                routes[path] = url
                variable = "ADDP_NOTEBOOK_" + field.removeprefix("owner_").upper()
                if field == "owner_api_endpoint":
                    variable = "ADDP_NOTEBOOK_OWNER_API_ENDPOINT"
                environment[variable] = "http://nb-control:8097" + path
        # Reserve both identities before launch, including concurrently opening sessions.
        with self._lock:
            if key in self._opening or object_name in self._opening.values() or key in self._sessions or any(
                session.notebook_locator == values["notebook_locator"] for session in self._sessions.values()
            ):
                raise SessionConflictError("notebook already has an active interactive session")
            self._opening[key] = object_name
        prepared = False
        try:
            with closing(self._store.open(object_name)) as stream:
                contents = stream.read()
            container.prepare(name, contents, {"name": name, "base_path": values["base_path"],
                              "runtime_token": lab_token, "session_environment": environment})
            prepared = True
            container.start("lab", values["ttl_seconds"])
            self._wait_until_ready(f"http://nb-{key}:8888", values["base_path"], lab_token, container)
            session = InteractiveSession(key, values["tenant_id"], values["user_id"], values["task_id"],
                values["notebook_locator"], name, values["base_path"], f"http://{runtime_authority}", token,
                datetime.fromtimestamp(time.time() + values["ttl_seconds"], tz=timezone.utc),
                container, routes, values["owner_capability_token"], lab_token)
            with self._lock:
                self._sessions[key] = session
            return session
        except Exception:
            if prepared:
                container.close()
            raise
        finally:
            with self._lock:
                self._opening.pop(key, None)

    def lab_authorization(self, key, authorization):
        with self._lock:
            session = self._sessions.get(key)
            if (session and key not in self._closing and session.expires_at > datetime.now(tz=timezone.utc)
                    and hmac.compare_digest(authorization.encode(), ("token " + session.runtime_token).encode())):
                return "token " + session.lab_token
        return None

    def owner_route(self, key, path, authorization):
        with self._lock:
            session = self._sessions.get(key)
            if (session and key not in self._closing and session.expires_at > datetime.now(tz=timezone.utc)
                    and hmac.compare_digest(authorization.encode(), ("Bearer " + session.capability).encode())):
                return session.owner_routes.get(path)
        return None

    def close(self, session_id, tenant_id=None):
        with self._lock:
            session = self._sessions.get(session_id)
            if session is not None and tenant_id is not None and session.tenant_id != tenant_id:
                session = None
            if session is None or session_id in self._closing:
                raise SessionNotFoundError(session_id)
            self._closing.add(session_id)
        try:
            self._save_and_cleanup(session)
        finally:
            with self._lock:
                self._sessions.pop(session_id, None)
                self._closing.remove(session_id)

    def shutdown(self):
        if self._stopped.is_set():
            return
        self._stopped.set()
        with self._lock:
            sessions = list(self._sessions.values())
        for session in sessions:
            try:
                self.close(session.session_id)
            except SessionNotFoundError:
                continue
            except Exception:
                logger.exception("failed to close interactive notebook session id=%s", session.session_id)

    def _save_and_cleanup(self, session):
        session.container.stop()
        try:
            data = session.container.read(session.notebook_name)
            object_name = Ref.parse(session.notebook_locator).key
            self._store.put(
                object_name,
                io.BytesIO(data),
                size=len(data),
                media_type="application/x-ipynb+json",
            )
            logger.info(
                "saved interactive notebook session id=%s tenant=%s task=%s",
                session.session_id,
                session.tenant_id,
                session.task_id,
            )
        finally:
            session.container.discard_files()
            session.container.http.close()

    def _reap_expired(self):
        while not self._stopped.wait(15):
            now = datetime.now(tz=timezone.utc)
            with self._lock:
                sessions = list(self._sessions.values())
            for session in sessions:
                session_id = session.session_id
                try:
                    if session.expires_at <= now or session.container.poll() is not None:
                        self.close(session_id)
                except SessionNotFoundError:
                    continue
                except Exception:
                    logger.exception("failed to reap interactive notebook session id=%s", session_id)

    def _validate_request(self, data):
        if not isinstance(data, dict):
            raise SessionValidationError("request body must be a JSON object")
        if "notebook_path" in data or "minio_path" in data:
            raise SessionValidationError("legacy Notebook file field rejected")
        session_id = data.get("session_id")
        if not isinstance(session_id, str) or not re.fullmatch(r"[a-f0-9][a-f0-9-]{0,59}", session_id):
            raise SessionValidationError("session_id is invalid")
        positive_ids = {}
        for field in ("tenant_id", "user_id", "task_id"):
            value = data.get(field)
            if not isinstance(value, int) or isinstance(value, bool) or value <= 0:
                raise SessionValidationError(f"{field} must be a positive integer")
            positive_ids[field] = value
        notebook_locator = data.get("notebook_locator")
        try:
            ref = Ref.parse(notebook_locator)
        except (ValueError, TypeError, AttributeError) as exc:
            raise SessionValidationError("invalid notebook_locator") from exc
        if ref.area != "develop" or ref.kind != "object" or not ref.key.startswith(f"tenant_{positive_ids['tenant_id']}/notebooks/") or not ref.key.endswith(".ipynb"):
            raise SessionScopeError("notebook_locator outside session scope")
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
            "notebook_locator": notebook_locator,
            "notebook_ref": ref,
            "kernel": "python3",
            "base_path": base_path,
            "ttl_seconds": min(ttl_seconds, self._max_ttl_seconds),
            "owner_api_endpoint": owner_api_endpoint,
            **owner_endpoints,
            "owner_capability_token": owner_capability_token,
        }

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
