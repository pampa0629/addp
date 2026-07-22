import asyncio
import json
import os
import socket
import subprocess
import sys
import threading
import time
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest

from addp_common import oauth


class FakeResponse:
    status_code = 200

    def json(self):
        return {
            "access_token": "addp_at_new",
            "refresh_token": "addp_rt_new",
            "scope": "addp.api",
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
    stored = []

    def open_browser(authorization_url):
        nonlocal opened_redirect_uri
        query = urllib.parse.parse_qs(urllib.parse.urlparse(authorization_url).query)
        opened_redirect_uri = query["redirect_uri"][0]
        callback_query = urllib.parse.urlencode({"code": "addp_ac_test", "state": query["state"][0]})
        with urllib.request.urlopen(opened_redirect_uri + "?" + callback_query, timeout=2) as response:
            assert response.status == 204
        return True

    class BrowserLoginAsyncClient:
        def __init__(self, *args, **kwargs):
            pass

        async def __aenter__(self):
            return self

        async def __aexit__(self, *args):
            return False

        async def post(self, _url, data):
            nonlocal token_redirect_uri
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
    assert token_redirect_uri == opened_redirect_uri
    assert result == oauth.LoginResult(client_id="addp-cli", scope="addp.api")
    assert stored == [("http://localhost:8000", "addp_rt_new")]


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
                payload = {"access_token": "addp_at_one", "refresh_token": "addp_rt_one"}
            elif submitted_token == "addp_rt_one":
                status_code = 200
                payload = {"access_token": "addp_at_two", "refresh_token": "addp_rt_two"}
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
