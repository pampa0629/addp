import asyncio
import base64
import hashlib
import http.server
import secrets
import threading
import urllib.parse
import webbrowser
from dataclasses import dataclass

import httpx
import keyring


CLIENT_ID = "addp-cli"
REDIRECT_URI = "http://127.0.0.1:8765/callback"
KEYRING_SERVICE = "addp-cli"
DEVICE_GRANT_TYPE = "urn:ietf:params:oauth:grant-type:device_code"


class OAuthLoginError(RuntimeError):
    pass


@dataclass(frozen=True)
class LoginResult:
    client_id: str
    scope: str


def _token_endpoint(base_url: str) -> str:
    return base_url.rstrip("/") + "/api/v1/system/oauth/token"


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


async def browser_login(base_url: str, console_url: str, scope: str = "addp.api") -> LoginResult:
    verifier = secrets.token_urlsafe(64)
    challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b"=").decode()
    state = secrets.token_urlsafe(24)
    callback = _CallbackServer(state)
    callback.start()

    query = urllib.parse.urlencode({
        "response_type": "code",
        "client_id": CLIENT_ID,
        "redirect_uri": REDIRECT_URI,
        "scope": scope,
        "state": state,
        "code_challenge": challenge,
        "code_challenge_method": "S256",
    })
    webbrowser.open(console_url.rstrip("/") + "/oauth/authorize?" + query)
    code = await callback.wait_for_code()

    async with httpx.AsyncClient(timeout=15.0) as client:
        response = await client.post(
            _token_endpoint(base_url),
            data={
                "grant_type": "authorization_code",
                "client_id": CLIENT_ID,
                "code": code,
                "redirect_uri": REDIRECT_URI,
                "code_verifier": verifier,
            },
        )
    if response.status_code != 200:
        raise OAuthLoginError(response.json().get("error", "invalid_grant"))
    payload = response.json()
    _store_refresh_token(base_url, payload["refresh_token"])
    return LoginResult(client_id=CLIENT_ID, scope=payload.get("scope", scope))


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
        query = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
        if query.get("state", [""])[0] != self.server.expected_state:
            self.server.error = "state_mismatch"
        elif query.get("error"):
            self.server.error = query["error"][0]
        else:
            self.server.code = query.get("code", [""])[0]
        self.send_response(204)
        self.end_headers()
        self.server.completed.set()

    def log_message(self, _format, *_args):
        return


class _HTTPCallbackServer(http.server.HTTPServer):
    def __init__(self, expected_state: str):
        super().__init__(("127.0.0.1", 8765), _CallbackHandler)
        self.expected_state = expected_state
        self.code = ""
        self.error = ""
        self.completed = threading.Event()


class _CallbackServer:
    def __init__(self, expected_state: str):
        self.server = _HTTPCallbackServer(expected_state)

    def start(self) -> None:
        threading.Thread(target=self.server.handle_request, daemon=True).start()

    async def wait_for_code(self) -> str:
        completed = await asyncio.to_thread(self.server.completed.wait, 300)
        self.server.server_close()
        if not completed:
            raise OAuthLoginError("authorization_timeout")
        if self.server.error:
            raise OAuthLoginError(self.server.error)
        if not self.server.code:
            raise OAuthLoginError("invalid_authorization_response")
        return self.server.code
