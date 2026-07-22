import asyncio
import base64
import hashlib
import http
import http.server
import secrets
import tempfile
import threading
import urllib.parse
import webbrowser
from dataclasses import dataclass
from pathlib import Path

import httpx
import keyring
from filelock import AsyncFileLock, Timeout


CLIENT_ID = "addp-cli"
KEYRING_SERVICE = "addp-cli"
DEVICE_GRANT_TYPE = "urn:ietf:params:oauth:grant-type:device_code"
AUTHORIZATION_TIMEOUT_SECONDS = 300
CALLBACK_PATH = "/callback"


class OAuthLoginError(RuntimeError):
    pass


@dataclass(frozen=True)
class LoginResult:
    client_id: str
    scope: str


def _token_endpoint(base_url: str) -> str:
    return base_url.rstrip("/") + "/api/v1/system/oauth/token"


def _authorization_requests_endpoint(base_url: str) -> str:
    return base_url.rstrip("/") + "/api/v1/system/oauth/authorization_requests"


def _refresh_lock_path(base_url: str) -> Path:
    normalized_base_url = base_url.rstrip("/")
    digest = hashlib.sha256(normalized_base_url.encode()).hexdigest()
    lock_dir = Path(tempfile.gettempdir()) / "addp-cli"
    lock_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    return lock_dir / f"oauth-refresh-{digest}.lock"


def _refresh_token(base_url: str) -> str | None:
    return keyring.get_password(KEYRING_SERVICE, base_url.rstrip("/"))


def _store_refresh_token(base_url: str, token: str) -> None:
    keyring.set_password(KEYRING_SERVICE, base_url.rstrip("/"), token)


def delete_refresh_token(base_url: str) -> None:
    try:
        keyring.delete_password(KEYRING_SERVICE, base_url.rstrip("/"))
    except keyring.errors.PasswordDeleteError:
        pass


def has_login(base_url: str) -> bool:
    return bool(_refresh_token(base_url))


async def refresh_access_token(base_url: str) -> str:
    try:
        async with AsyncFileLock(_refresh_lock_path(base_url), timeout=30):
            refresh_token = _refresh_token(base_url)
            if not refresh_token:
                raise OAuthLoginError("authentication_required")
            async with httpx.AsyncClient(timeout=15.0) as client:
                response = await client.post(
                    _token_endpoint(base_url),
                    data={
                        "grant_type": "refresh_token",
                        "client_id": CLIENT_ID,
                        "refresh_token": refresh_token,
                    },
                )
            if response.status_code != 200:
                delete_refresh_token(base_url)
                raise OAuthLoginError(response.json().get("error", "invalid_grant"))
            payload = response.json()
            _store_refresh_token(base_url, payload["refresh_token"])
            return payload["access_token"]
    except Timeout as exc:
        raise OAuthLoginError("refresh_lock_timeout") from exc


async def browser_login(base_url: str, console_url: str, scope: str = "addp.api") -> LoginResult:
    verifier = secrets.token_urlsafe(64)
    challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b"=").decode()
    callback = _CallbackServer()
    callback.start()
    redirect_uri = callback.redirect_uri
    authorization_request = None
    try:
        async with httpx.AsyncClient(timeout=15.0) as client:
            response = await client.post(
                _authorization_requests_endpoint(base_url),
                data={
                    "client_id": CLIENT_ID,
                    "redirect_uri": redirect_uri,
                    "scope": scope,
                    "code_challenge": challenge,
                    "code_challenge_method": "S256",
                },
            )
        if response.status_code != 201:
            raise OAuthLoginError(response.json().get("error", "invalid_request"))
        authorization_request = response.json()
        request_id = authorization_request["request_id"]
        callback.expected_state = request_id
        query = urllib.parse.urlencode({"request_id": request_id})
        if not webbrowser.open(console_url.rstrip("/") + "/oauth/authorize?" + query):
            raise OAuthLoginError("browser_open_failed")
        code = await callback.wait_for_code()

        async with httpx.AsyncClient(timeout=15.0) as client:
            response = await client.post(
                _token_endpoint(base_url),
                data={
                    "grant_type": "authorization_code",
                    "client_id": CLIENT_ID,
                    "code": code,
                    "redirect_uri": redirect_uri,
                    "code_verifier": verifier,
                },
            )
        if response.status_code != 200:
            raise OAuthLoginError(response.json().get("error", "invalid_grant"))
        payload = response.json()
        _store_refresh_token(base_url, payload["refresh_token"])
        return LoginResult(client_id=CLIENT_ID, scope=payload.get("scope", scope))
    except BaseException:
        await callback.close()
        if authorization_request is not None:
            await _cancel_authorization_request(
                base_url,
                authorization_request["request_id"],
                authorization_request["request_secret"],
            )
        raise


async def _cancel_authorization_request(base_url: str, request_id: str, request_secret: str) -> None:
    try:
        async with httpx.AsyncClient(timeout=5.0) as client:
            await client.delete(
                _authorization_requests_endpoint(base_url) + "/" + urllib.parse.quote(request_id, safe=""),
                headers={"Authorization": "Bearer " + request_secret},
            )
    except Exception:
        return


async def device_login(base_url: str, scope: str = "addp.api", on_device=None) -> tuple[LoginResult, dict]:
    root = base_url.rstrip("/") + "/api/v1/system/oauth"
    async with httpx.AsyncClient(timeout=15.0) as client:
        response = await client.post(root + "/device/code", data={"client_id": CLIENT_ID, "scope": scope})
        if response.status_code != 200:
            raise OAuthLoginError(response.json().get("error", "invalid_request"))
        device = response.json()
        if on_device is not None:
            on_device(device)
        interval = int(device["interval"])
        deadline = asyncio.get_running_loop().time() + int(device["expires_in"])
        while asyncio.get_running_loop().time() < deadline:
            await asyncio.sleep(interval)
            token_response = await client.post(
                root + "/token",
                data={
                    "grant_type": DEVICE_GRANT_TYPE,
                    "client_id": CLIENT_ID,
                    "device_code": device["device_code"],
                },
            )
            payload = token_response.json()
            if token_response.status_code == 200:
                _store_refresh_token(base_url, payload["refresh_token"])
                return LoginResult(client_id=CLIENT_ID, scope=payload.get("scope", scope)), device
            error = payload.get("error")
            if error == "authorization_pending":
                continue
            if error == "slow_down":
                interval += 5
                continue
            raise OAuthLoginError(error or "invalid_grant")
    raise OAuthLoginError("expired_token")


async def logout(base_url: str) -> None:
    refresh_token = _refresh_token(base_url)
    if refresh_token:
        async with httpx.AsyncClient(timeout=15.0) as client:
            await client.post(
                base_url.rstrip("/") + "/api/v1/system/oauth/revoke",
                data={"client_id": CLIENT_ID, "token": refresh_token},
            )
    delete_refresh_token(base_url)


class _CallbackHandler(http.server.BaseHTTPRequestHandler):
    server: "_HTTPCallbackServer"

    def do_GET(self):
        request_uri = urllib.parse.urlparse(self.path)
        if request_uri.path != CALLBACK_PATH:
            self._send_empty(http.HTTPStatus.NOT_FOUND)
            return
        if self.server.completed.is_set():
            self._send_empty(http.HTTPStatus.GONE)
            return

        query = urllib.parse.parse_qs(request_uri.query, keep_blank_values=True)
        states = query.get("state", [])
        codes = query.get("code", [])
        errors = query.get("error", [])
        success = False
        if len(states) != 1 or states[0] != self.server.expected_state:
            self.server.error = "state_mismatch"
        elif len(codes) == 1 and codes[0] and not errors:
            self.server.code = codes[0]
            success = True
        elif len(errors) == 1 and errors[0] and not codes:
            self.server.error = errors[0]
        else:
            self.server.error = "invalid_authorization_response"

        self.server.completed.set()
        self._send_result(success)

    def _send_result(self, success: bool) -> None:
        language = "en" if self.headers.get("Accept-Language", "").lower().startswith("en") else "zh-cn"
        if language == "en":
            title = "Authorization response received" if success else "Authorization failed"
            message = (
                "Return to the terminal to view the final login result. You may close this window."
                if success
                else "Return to the terminal for details. You may close this window."
            )
        else:
            title = "ADDP 授权响应已接收" if success else "ADDP 授权失败"
            message = (
                "请返回终端查看最终登录结果，现在可以关闭此窗口。"
                if success
                else "请返回终端查看详情，现在可以关闭此窗口。"
            )
        body = (
            "<!doctype html><html lang=\""
            + language
            + "\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" "
            "content=\"width=device-width,initial-scale=1\"><title>"
            + title
            + "</title><style>body{font-family:system-ui,sans-serif;margin:0;min-height:100vh;display:grid;"
            "place-items:center;background:#f6f7f9;color:#202124}main{max-width:32rem;padding:2rem}"
            "h1{font-size:1.5rem;"
            "margin:0 0 .75rem}p{line-height:1.6;margin:0;color:#5f6368}</style></head><body><main><h1>"
            + title
            + "</h1><p>"
            + message
            + "</p></main></body></html>"
        ).encode("utf-8")
        self.send_response(http.HTTPStatus.OK if success else http.HTTPStatus.BAD_REQUEST)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.send_header(
            "Content-Security-Policy",
            "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'",
        )
        self.send_header("Referrer-Policy", "no-referrer")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.end_headers()
        self.wfile.write(body)

    def _send_empty(self, status: http.HTTPStatus) -> None:
        self.send_response(status)
        self.send_header("Content-Length", "0")
        self.send_header("Cache-Control", "no-store")
        self.end_headers()

    def log_message(self, _format, *_args):
        return


class _HTTPCallbackServer(http.server.HTTPServer):
    def __init__(self, expected_state: str):
        super().__init__(("127.0.0.1", 0), _CallbackHandler)
        self.timeout = 0.1
        self.expected_state = expected_state
        self.code = ""
        self.error = ""
        self.completed = threading.Event()


class _CallbackServer:
    def __init__(self, expected_state: str = ""):
        self.server = _HTTPCallbackServer(expected_state)
        self.stopped = threading.Event()
        self.thread: threading.Thread | None = None

    def start(self) -> None:
        self.thread = threading.Thread(target=self._serve, daemon=True)
        self.thread.start()

    def _serve(self) -> None:
        while not self.server.completed.is_set() and not self.stopped.is_set():
            self.server.handle_request()

    @property
    def redirect_uri(self) -> str:
        return f"http://127.0.0.1:{self.server.server_port}{CALLBACK_PATH}"

    @property
    def expected_state(self) -> str:
        return self.server.expected_state

    @expected_state.setter
    def expected_state(self, value: str) -> None:
        self.server.expected_state = value

    async def close(self) -> None:
        self.stopped.set()
        if self.thread is not None and self.thread.is_alive():
            await asyncio.to_thread(self.thread.join, 1)
        self.server.server_close()

    async def wait_for_code(self, timeout_seconds: float = AUTHORIZATION_TIMEOUT_SECONDS) -> str:
        loop = asyncio.get_running_loop()
        deadline = loop.time() + timeout_seconds
        try:
            while not self.server.completed.is_set() and loop.time() < deadline:
                await asyncio.sleep(min(0.05, max(0, deadline - loop.time())))
            completed = self.server.completed.is_set()
        finally:
            await self.close()
        if not completed:
            raise OAuthLoginError("authorization_timeout")
        if self.server.error:
            raise OAuthLoginError(self.server.error)
        if not self.server.code:
            raise OAuthLoginError("invalid_authorization_response")
        return self.server.code
