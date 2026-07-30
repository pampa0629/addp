import asyncio
import json
import os
import socket
import subprocess
import sys
import threading
import time
import urllib.parse
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest
import httpx

from addp_common import oauth


class FakeResponse:
    status_code = 200

    def json(self):
        return {
            "access_token": "addp_at_new",
            "refresh_token": "addp_rt_new",
            "token_type": "Bearer",
            "expires_in": 900,
            "scope": "addp.api",
        }


class AuthorizationRequestResponse:
    status_code = 201

    def json(self):
        return {
            "request_id": "authorization-request-1",
            "request_secret": "addp_ars_secret",
            "expires_in": 300,
        }


class FakeAsyncClient:
    def __init__(self, *args, **kwargs):
        self.request = None

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        return False

    async def post(self, url, data):
        self.request = (url, data)
        assert data == {
            "grant_type": "refresh_token",
            "client_id": "addp-cli",
            "refresh_token": "addp_rt_old",
        }
        return FakeResponse()


@pytest.mark.asyncio
async def test_device_login_requests_fixed_addp_api_audience(monkeypatch):
    requests = []
    stored = []

    class DeviceResponse:
        status_code = 200

        def json(self):
            return {
                "device_code": "addp_dc_device",
                "user_code": "ABCD-EFGH",
                "interval": 5,
                "expires_in": 600,
            }

    class DeviceAsyncClient:
        def __init__(self, *args, **kwargs):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *args):
            return False

        async def post(self, url, data):
            requests.append((url, data))
            if url.endswith("/device/code"):
                return DeviceResponse()
            return FakeResponse()

    async def no_wait(_seconds):
        return None

    monkeypatch.setattr(oauth.httpx, "AsyncClient", DeviceAsyncClient)
    monkeypatch.setattr(oauth.asyncio, "sleep", no_wait)
    monkeypatch.setattr(oauth, "_store_refresh_token", lambda base_url, token: stored.append((base_url, token)))

    result, device = await oauth.device_login("http://localhost:8000")

    assert requests[0] == (
        "http://localhost:8000/api/v1/system/oauth/device/code",
        {"client_id": "addp-cli", "scope": "addp.api", "audience": "addp.api"},
    )
    assert requests[1][1] == {
        "grant_type": oauth.DEVICE_GRANT_TYPE,
        "client_id": "addp-cli",
        "device_code": "addp_dc_device",
    }
    assert result == oauth.LoginResult(
        client_id="addp-cli",
        scope="addp.api",
        access_token="addp_at_new",
    )
    assert device["user_code"] == "ABCD-EFGH"
    assert stored == [("http://localhost:8000", "addp_rt_new")]


@pytest.mark.asyncio
async def test_browser_login_uses_available_dynamic_loopback_port(monkeypatch):
    blocker = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        blocker.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        blocker.bind(("127.0.0.1", 8765))
        blocker.listen(1)
    except OSError:
        blocker.close()
        blocker = None

    opened_redirect_uri = None
    token_redirect_uri = None
    opened_query = None
    stored = []

    def open_browser(authorization_url):
        nonlocal opened_query
        query = urllib.parse.parse_qs(urllib.parse.urlparse(authorization_url).query)
        opened_query = query
        callback_query = urllib.parse.urlencode({"code": "addp_ac_test", "state": query["request_id"][0]})
        with urllib.request.urlopen(opened_redirect_uri + "?" + callback_query, timeout=2) as response:
            body = response.read().decode()
            assert response.status == 200
            assert response.headers["Cache-Control"] == "no-store"
            assert response.headers["Referrer-Policy"] == "no-referrer"
            assert "ADDP 授权响应已接收" in body
            assert "addp_ac_test" not in body
            assert query["request_id"][0] not in body
        return True

    class BrowserLoginAsyncClient:
        def __init__(self, *args, **kwargs):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *args):
            return False

        async def post(self, _url, data):
            nonlocal opened_redirect_uri, token_redirect_uri
            if _url.endswith("/oauth/authorization_requests"):
                opened_redirect_uri = data["redirect_uri"]
                assert data == {
                    "client_id": "addp-cli",
                    "redirect_uri": opened_redirect_uri,
                    "scope": "addp.api",
                    "code_challenge": data["code_challenge"],
                    "code_challenge_method": "S256",
                }
                return AuthorizationRequestResponse()
            token_redirect_uri = data["redirect_uri"]
            assert data["code"] == "addp_ac_test"
            return FakeResponse()

    monkeypatch.setattr(oauth.webbrowser, "open", open_browser)
    monkeypatch.setattr(oauth.httpx, "AsyncClient", BrowserLoginAsyncClient)
    monkeypatch.setattr(oauth, "_store_refresh_token", lambda base_url, token: stored.append((base_url, token)))

    try:
        result = await oauth.browser_login("http://localhost:8000", "http://localhost:5170")
    finally:
        if blocker is not None:
            blocker.close()

    parsed_redirect = urllib.parse.urlparse(opened_redirect_uri)
    assert parsed_redirect.hostname == "127.0.0.1"
    assert parsed_redirect.port not in (None, 8765)
    assert parsed_redirect.path == "/callback"
    assert opened_query == {"request_id": ["authorization-request-1"]}
    assert token_redirect_uri == opened_redirect_uri
    assert result == oauth.LoginResult(
        client_id="addp-cli",
        scope="addp.api",
        access_token="addp_at_new",
    )
    assert stored == [("http://localhost:8000", "addp_rt_new")]


@pytest.mark.asyncio
async def test_browser_login_cancels_server_request_when_browser_cannot_open(monkeypatch):
    cancelled = []

    class BrowserUnavailableAsyncClient:
        def __init__(self, *args, **kwargs):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *args):
            return False

        async def post(self, _url, data):
            return AuthorizationRequestResponse()

        async def delete(self, url, headers):
            cancelled.append((url, headers))

    monkeypatch.setattr(oauth.httpx, "AsyncClient", BrowserUnavailableAsyncClient)
    monkeypatch.setattr(oauth.webbrowser, "open", lambda _url: False)

    with pytest.raises(oauth.OAuthLoginError, match="browser_open_failed"):
        await oauth.browser_login("http://localhost:8000", "http://localhost:5170")

    assert cancelled == [(
        "http://localhost:8000/api/v1/system/oauth/authorization_requests/authorization-request-1",
        {"Authorization": "Bearer addp_ars_secret"},
    )]


@pytest.mark.asyncio
async def test_callback_ignores_other_paths_until_valid_callback():
    callback = oauth._CallbackServer("expected-state")
    callback.start()

    with pytest.raises(urllib.error.HTTPError) as not_found:
        urllib.request.urlopen(callback.redirect_uri.replace("/callback", "/favicon.ico"), timeout=2)
    assert not_found.value.code == 404

    callback_url = callback.redirect_uri + "?" + urllib.parse.urlencode({
        "code": "addp_ac_expected",
        "state": "expected-state",
    })
    with urllib.request.urlopen(callback_url, timeout=2) as response:
        assert response.status == 200

    assert await callback.wait_for_code(timeout_seconds=1) == "addp_ac_expected"


@pytest.mark.asyncio
async def test_callback_rejects_state_mismatch_without_reflecting_secrets():
    callback = oauth._CallbackServer("expected-state")
    callback.start()
    callback_url = callback.redirect_uri + "?" + urllib.parse.urlencode({
        "code": "addp_ac_secret",
        "state": "unexpected-state",
    })

    with pytest.raises(urllib.error.HTTPError) as failed:
        urllib.request.urlopen(callback_url, timeout=2)
    body = failed.value.read().decode()
    assert failed.value.code == 400
    assert "addp_ac_secret" not in body
    assert "unexpected-state" not in body

    with pytest.raises(oauth.OAuthLoginError, match="state_mismatch"):
        await callback.wait_for_code(timeout_seconds=1)


@pytest.mark.asyncio
async def test_callback_returns_authorization_error_to_cli_only():
    callback = oauth._CallbackServer("expected-state")
    callback.start()
    callback_url = callback.redirect_uri + "?" + urllib.parse.urlencode({
        "error": "access_denied",
        "state": "expected-state",
    })

    request = urllib.request.Request(callback_url, headers={"Accept-Language": "en"})
    with pytest.raises(urllib.error.HTTPError) as failed:
        urllib.request.urlopen(request, timeout=2)
    body = failed.value.read().decode()
    assert failed.value.code == 400
    assert "Authorization failed" in body
    assert "access_denied" not in body

    with pytest.raises(oauth.OAuthLoginError, match="access_denied"):
        await callback.wait_for_code(timeout_seconds=1)


@pytest.mark.asyncio
async def test_callback_timeout_closes_loopback_listener():
    callback = oauth._CallbackServer("expected-state")
    callback.start()
    callback_address = ("127.0.0.1", callback.server.server_port)

    with pytest.raises(oauth.OAuthLoginError, match="authorization_timeout"):
        await callback.wait_for_code(timeout_seconds=0.01)

    with pytest.raises(OSError):
        socket.create_connection(callback_address, timeout=0.2)


@pytest.mark.asyncio
async def test_refresh_access_token_rotates_keyring_value(monkeypatch):
    stored = []
    monkeypatch.setattr(oauth, "_refresh_token", lambda _base_url: "addp_rt_old")
    monkeypatch.setattr(oauth, "_store_refresh_token", lambda base_url, token: stored.append((base_url, token)))
    monkeypatch.setattr(oauth.httpx, "AsyncClient", FakeAsyncClient)

    access_token = await oauth.refresh_access_token("http://localhost:8000")

    assert access_token == "addp_at_new"
    assert stored == [("http://localhost:8000", "addp_rt_new")]


@pytest.mark.asyncio
async def test_refresh_access_token_deletes_keyring_value_only_for_invalid_grant(monkeypatch):
    deleted = []

    class InvalidGrantResponse:
        status_code = 400

        def json(self):
            return {"error": "invalid_grant"}

    class InvalidGrantAsyncClient:
        def __init__(self, *args, **kwargs):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *args):
            return False

        async def post(self, _url, data):
            return InvalidGrantResponse()

    monkeypatch.setattr(oauth, "_refresh_token", lambda _base_url: "addp_rt_old")
    monkeypatch.setattr(oauth, "delete_refresh_token", lambda base_url: deleted.append(base_url))
    monkeypatch.setattr(oauth.httpx, "AsyncClient", InvalidGrantAsyncClient)

    with pytest.raises(oauth.OAuthLoginError, match="invalid_grant"):
        await oauth.refresh_access_token("http://localhost:8000")

    assert deleted == ["http://localhost:8000"]


@pytest.mark.asyncio
@pytest.mark.parametrize("status_code", [429, 503])
async def test_refresh_access_token_preserves_keyring_value_for_temporary_failure(monkeypatch, status_code):
    deleted = []

    class TemporaryFailureResponse:
        def __init__(self):
            self.status_code = status_code

        def json(self):
            return {"error": "temporarily_unavailable"}

    class TemporaryFailureAsyncClient:
        def __init__(self, *args, **kwargs):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *args):
            return False

        async def post(self, _url, data):
            return TemporaryFailureResponse()

    monkeypatch.setattr(oauth, "_refresh_token", lambda _base_url: "addp_rt_old")
    monkeypatch.setattr(oauth, "delete_refresh_token", lambda base_url: deleted.append(base_url))
    monkeypatch.setattr(oauth.httpx, "AsyncClient", TemporaryFailureAsyncClient)

    with pytest.raises(oauth.OAuthLoginError, match="temporarily_unavailable"):
        await oauth.refresh_access_token("http://localhost:8000")

    assert deleted == []


@pytest.mark.asyncio
async def test_refresh_access_token_preserves_keyring_value_for_non_json_failure(monkeypatch):
    deleted = []

    class NonJSONFailureResponse:
        status_code = 502

        def json(self):
            raise ValueError("not JSON")

    class NonJSONFailureAsyncClient:
        def __init__(self, *args, **kwargs):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *args):
            return False

        async def post(self, _url, data):
            return NonJSONFailureResponse()

    monkeypatch.setattr(oauth, "_refresh_token", lambda _base_url: "addp_rt_old")
    monkeypatch.setattr(oauth, "delete_refresh_token", lambda base_url: deleted.append(base_url))
    monkeypatch.setattr(oauth.httpx, "AsyncClient", NonJSONFailureAsyncClient)

    with pytest.raises(oauth.OAuthLoginError, match="oauth_request_failed"):
        await oauth.refresh_access_token("http://localhost:8000")

    assert deleted == []


def test_oauth_error_does_not_expose_unrecognized_server_value():
    secret = "addp_rt_server_echo_must_not_reach_terminal"
    response = httpx.Response(400, json={"error": secret})

    error = oauth._oauth_error(response)

    assert error == "oauth_request_failed"
    assert secret not in error


@pytest.mark.asyncio
async def test_refresh_access_token_maps_network_failure(monkeypatch):
    class NetworkFailureAsyncClient:
        def __init__(self, *args, **kwargs):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *args):
            return False

        async def post(self, url, data):
            raise httpx.ConnectError("connection refused", request=httpx.Request("POST", url))

    monkeypatch.setattr(oauth, "_refresh_token", lambda _base_url: "addp_rt_old")
    monkeypatch.setattr(oauth.httpx, "AsyncClient", NetworkFailureAsyncClient)

    with pytest.raises(oauth.OAuthLoginError, match="oauth_service_unavailable"):
        await oauth.refresh_access_token("http://localhost:8000")


@pytest.mark.asyncio
async def test_refresh_access_token_maps_invalid_success_response(monkeypatch):
    class InvalidSuccessResponse:
        status_code = 200

        def json(self):
            raise ValueError("not JSON")

    class InvalidSuccessAsyncClient:
        def __init__(self, *args, **kwargs):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *args):
            return False

        async def post(self, _url, data):
            return InvalidSuccessResponse()

    monkeypatch.setattr(oauth, "_refresh_token", lambda _base_url: "addp_rt_old")
    monkeypatch.setattr(oauth.httpx, "AsyncClient", InvalidSuccessAsyncClient)

    with pytest.raises(oauth.OAuthLoginError, match="invalid_oauth_response"):
        await oauth.refresh_access_token("http://localhost:8000")


def test_base_url_normalization_is_shared_by_keyring_and_refresh_lock(monkeypatch):
    accounts = []
    monkeypatch.setattr(oauth.keyring, "set_password", lambda service, account, token: accounts.append(account))

    oauth._store_refresh_token("HTTP://LOCALHOST:80/", "addp_rt_value")

    assert accounts == ["http://localhost"]
    assert oauth._refresh_lock_path("HTTP://LOCALHOST:80/") == oauth._refresh_lock_path("http://localhost")


@pytest.mark.parametrize("base_url", [
    "localhost:8000",
    "ftp://localhost",
    "http://user:password@localhost",
    "http://localhost?tenant=one",
    "http://localhost#fragment",
])
def test_base_url_normalization_rejects_ambiguous_or_unsafe_values(base_url):
    with pytest.raises(oauth.OAuthLoginError, match="invalid_base_url"):
        oauth._normalize_base_url(base_url)


@pytest.mark.asyncio
async def test_browser_login_rejects_unsafe_console_url_before_opening_listener(monkeypatch):
    monkeypatch.setattr(oauth.webbrowser, "open", lambda _url: pytest.fail("browser must not open"))

    with pytest.raises(oauth.OAuthLoginError, match="invalid_base_url"):
        await oauth.browser_login("http://localhost:8000", "javascript:alert(1)")


@pytest.mark.asyncio
async def test_logout_deletes_keyring_value_only_after_server_accepts_revocation(monkeypatch):
    deleted = []

    class RevocationResponse:
        status_code = 200

    class RevocationClient:
        def __init__(self, *args, **kwargs):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *args):
            return False

        async def post(self, url, data):
            assert url.endswith("/api/v1/system/oauth/revoke")
            assert data == {"client_id": "addp-cli", "token": "addp_rt_current"}
            return RevocationResponse()

    monkeypatch.setattr(oauth, "_refresh_token", lambda _base_url: "addp_rt_current")
    monkeypatch.setattr(oauth, "delete_refresh_token", lambda base_url: deleted.append(base_url))
    monkeypatch.setattr(oauth.httpx, "AsyncClient", RevocationClient)

    await oauth.logout("http://localhost:8000/")

    assert deleted == ["http://localhost:8000"]


@pytest.mark.asyncio
async def test_logout_preserves_keyring_value_when_revocation_fails(monkeypatch):
    deleted = []

    class RevocationFailureResponse:
        status_code = 503

        def json(self):
            return {"error": "temporarily_unavailable"}

    class RevocationFailureClient:
        def __init__(self, *args, **kwargs):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *args):
            return False

        async def post(self, _url, data):
            return RevocationFailureResponse()

    monkeypatch.setattr(oauth, "_refresh_token", lambda _base_url: "addp_rt_current")
    monkeypatch.setattr(oauth, "delete_refresh_token", lambda base_url: deleted.append(base_url))
    monkeypatch.setattr(oauth.httpx, "AsyncClient", RevocationFailureClient)

    with pytest.raises(oauth.OAuthLoginError, match="temporarily_unavailable"):
        await oauth.logout("http://localhost:8000")

    assert deleted == []


@pytest.mark.asyncio
async def test_refresh_access_token_serializes_rotation_per_base_url(monkeypatch):
    current_token = "addp_rt_old"
    submitted_tokens = []
    first_request_started = asyncio.Event()
    release_first_request = asyncio.Event()

    def read_token(_base_url):
        return current_token

    def store_token(_base_url, token):
        nonlocal current_token
        current_token = token

    class RotatingResponse:
        status_code = 200

        def __init__(self, submitted_token):
            self.submitted_token = submitted_token

        def json(self):
            suffix = "one" if self.submitted_token == "addp_rt_old" else "two"
            return {
                "access_token": f"addp_at_{suffix}",
                "refresh_token": f"addp_rt_{suffix}",
                "token_type": "Bearer",
                "expires_in": 900,
                "scope": "addp.api",
            }

    class ConcurrentAsyncClient:
        def __init__(self, *args, **kwargs):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *args):
            return False

        async def post(self, _url, data):
            submitted_tokens.append(data["refresh_token"])
            if len(submitted_tokens) == 1:
                first_request_started.set()
                await release_first_request.wait()
            return RotatingResponse(data["refresh_token"])

    monkeypatch.setattr(oauth, "_refresh_token", read_token)
    monkeypatch.setattr(oauth, "_store_refresh_token", store_token)
    monkeypatch.setattr(oauth.httpx, "AsyncClient", ConcurrentAsyncClient)

    first = asyncio.create_task(oauth.refresh_access_token("http://localhost:8000/"))
    await first_request_started.wait()
    second = asyncio.create_task(oauth.refresh_access_token("http://localhost:8000"))
    await asyncio.sleep(0.05)
    release_first_request.set()

    assert await asyncio.gather(first, second) == ["addp_at_one", "addp_at_two"]
    assert submitted_tokens == ["addp_rt_old", "addp_rt_one"]
    assert current_token == "addp_rt_two"


def test_refresh_access_token_serializes_rotation_across_processes(tmp_path):
    submitted_tokens = []
    requests_lock = threading.Lock()

    class TokenHandler(BaseHTTPRequestHandler):
        def do_POST(self):
            content_length = int(self.headers["Content-Length"])
            form = urllib.parse.parse_qs(self.rfile.read(content_length).decode())
            submitted_token = form["refresh_token"][0]
            with requests_lock:
                submitted_tokens.append(submitted_token)
            if submitted_token == "addp_rt_old":
                time.sleep(0.2)
                status_code = 200
                payload = {
                    "access_token": "addp_at_one",
                    "refresh_token": "addp_rt_one",
                    "token_type": "Bearer",
                    "expires_in": 900,
                }
            elif submitted_token == "addp_rt_one":
                status_code = 200
                payload = {
                    "access_token": "addp_at_two",
                    "refresh_token": "addp_rt_two",
                    "token_type": "Bearer",
                    "expires_in": 900,
                }
            else:
                status_code = 400
                payload = {"error": "invalid_grant"}
            body = json.dumps(payload).encode()
            self.send_response(status_code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, _format, *_args):
            return

    server = ThreadingHTTPServer(("127.0.0.1", 0), TokenHandler)
    server_thread = threading.Thread(target=server.serve_forever, daemon=True)
    server_thread.start()
    token_file = tmp_path / "refresh-token"
    token_file.write_text("addp_rt_old")
    base_url = f"http://127.0.0.1:{server.server_port}"
    helper = """
import asyncio
import os
from pathlib import Path
from addp_common import oauth

token_file = Path(os.environ["ADDP_TEST_TOKEN_FILE"])
oauth._refresh_token = lambda _base_url: token_file.read_text()
oauth._store_refresh_token = lambda _base_url, token: token_file.write_text(token)
print(asyncio.run(oauth.refresh_access_token(os.environ["ADDP_TEST_BASE_URL"])))
"""
    env = {
        **os.environ,
        "ADDP_TEST_BASE_URL": base_url,
        "ADDP_TEST_TOKEN_FILE": str(token_file),
    }

    try:
        processes = [
            subprocess.Popen(
                [sys.executable, "-c", helper],
                cwd=os.fspath(tmp_path),
                env=env,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
            for _ in range(2)
        ]
        results = [process.communicate(timeout=10) for process in processes]
    finally:
        server.shutdown()
        server.server_close()

    assert [process.returncode for process in processes] == [0, 0]
    assert {stdout.strip() for stdout, _stderr in results} == {"addp_at_one", "addp_at_two"}
    assert [stderr for _stdout, stderr in results] == ["", ""]
    assert submitted_tokens == ["addp_rt_old", "addp_rt_one"]
    assert token_file.read_text() == "addp_rt_two"
