"""Installed-wheel product E2E for the ADDP CLI OAuth contract."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import shlex
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

import keyring


CLIENT_ID = "addp-cli"
KEYRING_SERVICE = "addp-cli"
DEVICE_GRANT_TYPE = "urn:ietf:params:oauth:grant-type:device_code"
RELEASE_KEYRING_BACKEND = "keyring.backends.macOS"
LOOPBACK_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))
PROXY_ENVIRONMENT_NAMES = frozenset({
    "http_proxy",
    "https_proxy",
    "all_proxy",
    "no_proxy",
})


class OAuthFixture:
    def __init__(self) -> None:
        self.lock = threading.Lock()
        self.base_url = ""
        self.sequence = 0
        self.authorization_requests: dict[str, dict[str, str]] = {}
        self.authorization_codes: dict[str, dict[str, str]] = {}
        self.devices: dict[str, dict[str, Any]] = {}
        self.families: dict[str, dict[str, Any]] = {}
        self.refresh_tokens: dict[str, str] = {}
        self.access_tokens: dict[str, str] = {}
        self.refresh_submissions: list[str] = []
        self.revoked_tokens: list[str] = []
        self.sensitive_values: set[str] = set()

    def secret(self, prefix: str) -> str:
        self.sequence += 1
        value = f"{prefix}{self.sequence:04d}_{uuid.uuid4().hex}"
        self.sensitive_values.add(value)
        return value

    def issue_family(self, context: tuple[str, str]) -> dict[str, Any]:
        family_id = uuid.uuid4().hex
        refresh_token = self.secret("addp_rt_e2e_")
        access_token = self.secret("addp_at_e2e_")
        family = {
            "id": family_id,
            "context": context,
            "refresh_token": refresh_token,
            "access_token": access_token,
            "revoked": False,
        }
        self.families[family_id] = family
        self.refresh_tokens[refresh_token] = family_id
        self.access_tokens[access_token] = family_id
        return family

    def rotate_family(self, refresh_token: str) -> dict[str, Any] | None:
        self.refresh_submissions.append(refresh_token)
        family_id = self.refresh_tokens.get(refresh_token)
        if family_id is None:
            return None
        family = self.families[family_id]
        if family["revoked"] or family["refresh_token"] != refresh_token:
            family["revoked"] = True
            return None
        old_access = family["access_token"]
        self.access_tokens.pop(old_access, None)
        new_refresh = self.secret("addp_rt_e2e_")
        new_access = self.secret("addp_at_e2e_")
        family["refresh_token"] = new_refresh
        family["access_token"] = new_access
        self.refresh_tokens[new_refresh] = family_id
        self.access_tokens[new_access] = family_id
        return family

    def revoke(self, refresh_token: str) -> None:
        self.revoked_tokens.append(refresh_token)
        family_id = self.refresh_tokens.get(refresh_token)
        if family_id is None:
            return
        family = self.families[family_id]
        family["revoked"] = True
        self.access_tokens.pop(family["access_token"], None)


class OAuthHandler(BaseHTTPRequestHandler):
    server: "OAuthServer"

    def do_GET(self) -> None:
        parsed = urllib.parse.urlsplit(self.path)
        if parsed.path == "/oauth/authorize":
            self._authorize_browser(parsed)
            return
        if parsed.path == "/api/v1/system/auth/context":
            self._auth_context()
            return
        self._json(HTTPStatus.NOT_FOUND, {"error": "not_found"})

    def do_POST(self) -> None:
        parsed = urllib.parse.urlsplit(self.path)
        form = self._form()
        if parsed.path == "/api/v1/system/oauth/authorization_requests":
            self._create_authorization_request(form)
            return
        if parsed.path == "/api/v1/system/oauth/device/code":
            self._create_device_code(form)
            return
        if parsed.path == "/api/v1/system/oauth/token":
            self._token(form)
            return
        if parsed.path == "/api/v1/system/oauth/revoke":
            self._revoke(form)
            return
        self._json(HTTPStatus.NOT_FOUND, {"error": "not_found"})

    def _create_authorization_request(self, form: dict[str, str]) -> None:
        redirect = urllib.parse.urlsplit(form.get("redirect_uri", ""))
        if (
            form.get("client_id") != CLIENT_ID
            or form.get("scope") != "addp.api"
            or form.get("code_challenge_method") != "S256"
            or redirect.scheme != "http"
            or redirect.hostname != "127.0.0.1"
            or redirect.path != "/callback"
            or redirect.port is None
        ):
            self._json(HTTPStatus.BAD_REQUEST, {"error": "invalid_request"})
            return
        with self.server.fixture.lock:
            request_id = self.server.fixture.secret("oauth_request_")
            request_secret = self.server.fixture.secret("addp_ars_e2e_")
            self.server.fixture.authorization_requests[request_id] = {
                "redirect_uri": form["redirect_uri"],
                "challenge": form["code_challenge"],
                "request_secret": request_secret,
            }
        self._json(
            HTTPStatus.CREATED,
            {"request_id": request_id, "request_secret": request_secret, "expires_in": 300},
        )

    def _authorize_browser(self, parsed: urllib.parse.SplitResult) -> None:
        request_ids = urllib.parse.parse_qs(parsed.query).get("request_id", [])
        if len(request_ids) != 1:
            self._json(HTTPStatus.BAD_REQUEST, {"error": "invalid_request"})
            return
        with self.server.fixture.lock:
            request = self.server.fixture.authorization_requests.get(request_ids[0])
            if request is None:
                self._json(HTTPStatus.GONE, {"error": "invalid_request"})
                return
            code = self.server.fixture.secret("addp_ac_e2e_")
            self.server.fixture.authorization_codes[code] = {
                **request,
                "context_type": "tenant",
                "tenant_id": "101",
                "tenant_membership_id": "1001",
            }
        location = request["redirect_uri"] + "?" + urllib.parse.urlencode(
            {"code": code, "state": request_ids[0]}
        )
        self.send_response(HTTPStatus.FOUND)
        self.send_header("Location", location)
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", "0")
        self.end_headers()

    def _create_device_code(self, form: dict[str, str]) -> None:
        if form != {"client_id": CLIENT_ID, "scope": "addp.api", "audience": "addp.api"}:
            self._json(HTTPStatus.BAD_REQUEST, {"error": "invalid_request"})
            return
        with self.server.fixture.lock:
            device_code = self.server.fixture.secret("addp_dc_e2e_")
            user_code = "E2E2-CLI2"
            self.server.fixture.devices[device_code] = {"polls": 0, "user_code": user_code}
        self._json(
            HTTPStatus.OK,
            {
                "device_code": device_code,
                "user_code": user_code,
                "verification_uri": self.server.fixture.base_url + "/oauth/device",
                "verification_uri_complete": self.server.fixture.base_url + "/oauth/device?user_code=" + user_code,
                "expires_in": 30,
                "interval": 1,
            },
        )

    def _token(self, form: dict[str, str]) -> None:
        grant_type = form.get("grant_type")
        if form.get("client_id") != CLIENT_ID:
            self._json(HTTPStatus.BAD_REQUEST, {"error": "invalid_client"})
            return
        if grant_type == "authorization_code":
            self._authorization_code_token(form)
            return
        if grant_type == DEVICE_GRANT_TYPE:
            self._device_token(form)
            return
        if grant_type == "refresh_token":
            with self.server.fixture.lock:
                # Keep the first rotation open long enough for an unlocked peer
                # to read and submit the same old Keychain value.
                time.sleep(0.2)
                family = self.server.fixture.rotate_family(form.get("refresh_token", ""))
                if family is None:
                    self._json(HTTPStatus.BAD_REQUEST, {"error": "invalid_grant"})
                    return
                payload = self._token_payload(family)
            self._json(HTTPStatus.OK, payload)
            return
        self._json(HTTPStatus.BAD_REQUEST, {"error": "unsupported_grant_type"})

    def _authorization_code_token(self, form: dict[str, str]) -> None:
        with self.server.fixture.lock:
            request = self.server.fixture.authorization_codes.pop(form.get("code", ""), None)
            verifier = form.get("code_verifier", "")
            if verifier:
                self.server.fixture.sensitive_values.add(verifier)
            challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b"=").decode()
            if request is None or form.get("redirect_uri") != request["redirect_uri"] or challenge != request["challenge"]:
                self._json(HTTPStatus.BAD_REQUEST, {"error": "invalid_grant"})
                return
            family = self.server.fixture.issue_family(
                (request["tenant_id"], request["tenant_membership_id"])
            )
            payload = self._token_payload(family)
        self._json(HTTPStatus.OK, payload)

    def _device_token(self, form: dict[str, str]) -> None:
        with self.server.fixture.lock:
            device = self.server.fixture.devices.get(form.get("device_code", ""))
            if device is None:
                self._json(HTTPStatus.BAD_REQUEST, {"error": "invalid_grant"})
                return
            device["polls"] += 1
            if device["polls"] == 1:
                self._json(HTTPStatus.BAD_REQUEST, {"error": "authorization_pending"})
                return
            self.server.fixture.devices.pop(form["device_code"], None)
            family = self.server.fixture.issue_family(("202", "2002"))
            payload = self._token_payload(family)
        self._json(HTTPStatus.OK, payload)

    def _auth_context(self) -> None:
        authorization = self.headers.get("Authorization", "")
        access_token = authorization.removeprefix("Bearer ") if authorization.startswith("Bearer ") else ""
        with self.server.fixture.lock:
            family_id = self.server.fixture.access_tokens.get(access_token)
            family = self.server.fixture.families.get(family_id or "")
            if family is None or family["revoked"] or family["access_token"] != access_token:
                self._json(HTTPStatus.UNAUTHORIZED, {"error": "invalid_token"})
                return
            tenant_id, membership_id = family["context"]
        self._json(
            HTTPStatus.OK,
            {
                "schema_version": "addp.auth_context/v1",
                "principal": {"type": "user", "id": "42"},
                "context": {
                    "type": "tenant",
                    "tenant_id": tenant_id,
                    "tenant_membership_id": membership_id,
                },
                "authentication": {
                    "methods": ["password"],
                    "assurance_level": "aal1",
                    "authenticated_at": "2026-07-30T00:00:00Z",
                    "step_up_expires_at": None,
                },
                "client": {
                    "client_id": CLIENT_ID,
                    "audiences": ["addp.api"],
                    "scope_mode": "restricted",
                    "scopes": ["addp.api"],
                },
                "organization": {"departments": [], "project_groups": []},
                "authorization": {"authorization_version": "1", "role_assignments": []},
                "token": {
                    "type": "oauth_access_token",
                    "issued_at": "2026-07-30T00:00:00Z",
                    "expires_at": "2026-07-30T00:15:00Z",
                },
                "delegation": None,
            },
        )

    def _revoke(self, form: dict[str, str]) -> None:
        if form.get("client_id") != CLIENT_ID:
            self._json(HTTPStatus.BAD_REQUEST, {"error": "invalid_client"})
            return
        with self.server.fixture.lock:
            self.server.fixture.revoke(form.get("token", ""))
        self._json(HTTPStatus.OK, {})

    @staticmethod
    def _token_payload(family: dict[str, Any]) -> dict[str, Any]:
        return {
            "access_token": family["access_token"],
            "refresh_token": family["refresh_token"],
            "token_type": "Bearer",
            "expires_in": 900,
            "scope": "addp.api",
        }

    def _form(self) -> dict[str, str]:
        length = int(self.headers.get("Content-Length", "0"))
        values = urllib.parse.parse_qs(self.rfile.read(length).decode(), keep_blank_values=True)
        return {key: items[0] for key, items in values.items() if len(items) == 1}

    def _json(self, status: HTTPStatus, payload: dict[str, Any]) -> None:
        body = json.dumps(payload, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, _format: str, *_args: object) -> None:
        return


class OAuthServer(ThreadingHTTPServer):
    def __init__(self, fixture: OAuthFixture) -> None:
        super().__init__(("127.0.0.1", 0), OAuthHandler)
        self.fixture = fixture


def run_browser(url: str) -> int:
    try:
        with LOOPBACK_OPENER.open(url, timeout=10) as response:
            response.read()
    except urllib.error.HTTPError as exc:
        if exc.code >= 400:
            return 1
    except OSError:
        return 1
    return 0


def assert_command_succeeded(result: subprocess.CompletedProcess[str], operation: str) -> None:
    if result.returncode != 0:
        raise AssertionError(f"{operation} failed: exit_code={result.returncode}")


def capture_command(
    command: list[str], env: dict[str, str], operation: str
) -> subprocess.CompletedProcess[str]:
    try:
        return subprocess.run(command, env=env, capture_output=True, text=True, timeout=30)
    except subprocess.TimeoutExpired:
        raise AssertionError(f"{operation} timed out") from None


def run_command(
    command: list[str], env: dict[str, str], operation: str
) -> subprocess.CompletedProcess[str]:
    result = capture_command(command, env, operation)
    assert_command_succeeded(result, operation)
    return result


def collect_process(
    process: subprocess.Popen[str], operation: str
) -> subprocess.CompletedProcess[str]:
    try:
        stdout, stderr = process.communicate(timeout=30)
    except subprocess.TimeoutExpired:
        process.kill()
        process.communicate()
        raise AssertionError(f"{operation} timed out") from None
    return subprocess.CompletedProcess(process.args, process.returncode, stdout, stderr)


def parse_json_output(result: subprocess.CompletedProcess[str]) -> dict[str, Any]:
    lines = result.stdout.splitlines()
    if len(lines) != 1:
        raise AssertionError(f"stdout is not one JSON document: document_count={len(lines)}")
    try:
        payload = json.loads(lines[0])
    except (TypeError, ValueError):
        raise AssertionError("stdout is not valid JSON") from None
    if not isinstance(payload, dict):
        raise AssertionError(f"stdout JSON is not an object: json_type={type(payload).__name__}")
    return payload


def assert_context(payload: dict[str, Any], tenant_id: str, membership_id: str) -> None:
    expected = {"type": "tenant", "tenant_id": tenant_id, "tenant_membership_id": membership_id}
    if payload.get("context") != expected or payload.get("client", {}).get("client_id") != CLIENT_ID:
        raise AssertionError("authoritative context did not match the approved OAuth context")


def assert_manual_token_rejected(result: subprocess.CompletedProcess[str]) -> None:
    if result.returncode != 2:
        raise AssertionError(f"manual access token was not rejected: exit_code={result.returncode}")
    if parse_json_output(result).get("error", {}).get("code") != "invalid_command":
        raise AssertionError("manual access token rejection returned an unexpected error code")


def assert_refresh_process_succeeded(result: subprocess.CompletedProcess[str]) -> None:
    assert_command_succeeded(result, "cross-process refresh")
    if parse_json_output(result) != {"refreshed": True}:
        raise AssertionError("cross-process refresh returned an unexpected result")


def assert_refresh_rotation_serialized(refresh_tokens: list[str]) -> None:
    token_count = len(refresh_tokens)
    distinct_token_count = len(set(refresh_tokens))
    if token_count != 2 or distinct_token_count != 2:
        raise AssertionError(
            "refresh rotation was not serialized across processes: "
            f"submission_count={token_count}, distinct_token_count={distinct_token_count}"
        )


def assert_no_secrets(results: list[subprocess.CompletedProcess[str]], fixture: OAuthFixture) -> None:
    terminal = "".join(result.stdout + result.stderr for result in results)
    leaked_count = sum(1 for value in fixture.sensitive_values if value and value in terminal)
    if leaked_count:
        raise AssertionError(f"OAuth secrets reached terminal output: leaked_secret_count={leaked_count}")


def main(addp: Path) -> int:
    if sys.platform != "darwin":
        raise RuntimeError(f"release E2E currently requires macOS, got {sys.platform}")
    backend = keyring.get_keyring()
    backend_module = type(backend).__module__
    if backend_module != RELEASE_KEYRING_BACKEND or backend.priority <= 0:
        raise RuntimeError(f"release E2E requires macOS Keychain, got {backend_module}.{type(backend).__name__}")

    fixture = OAuthFixture()
    server = OAuthServer(fixture)
    fixture.base_url = f"http://127.0.0.1:{server.server_port}"
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    results: list[subprocess.CompletedProcess[str]] = []
    env = {
        **{
            name: value
            for name, value in os.environ.items()
            if name.lower() not in PROXY_ENVIRONMENT_NAMES
        },
        "ADDP_BASE_URL": fixture.base_url,
        "ADDP_CONSOLE_URL": fixture.base_url,
        "BROWSER": f"{shlex.quote(sys.executable)} {shlex.quote(str(Path(__file__).resolve()))} --browser %s",
    }
    helper = (
        "import asyncio,json,os; "
        "from addp_common.oauth import refresh_access_token; "
        "asyncio.run(refresh_access_token(os.environ['ADDP_BASE_URL'])); "
        "print(json.dumps({'refreshed':True},separators=(',',':')))"
    )
    try:
        try:
            keyring.delete_password(KEYRING_SERVICE, fixture.base_url)
        except keyring.errors.PasswordDeleteError:
            pass

        manual_token = fixture.secret("addp_at_manual_e2e_")
        rejected_manual_token = capture_command(
            [str(addp), "--token", manual_token, "tools", "list"],
            env,
            "manual access token rejection",
        )
        results.append(rejected_manual_token)
        assert_manual_token_rejected(rejected_manual_token)

        browser_login = run_command([str(addp), "auth", "login"], env, "browser login")
        results.append(browser_login)
        assert_context(parse_json_output(browser_login), "101", "1001")
        if not keyring.get_password(KEYRING_SERVICE, fixture.base_url):
            raise AssertionError("browser login did not persist the refresh token in OS Keychain")

        browser_status = run_command([str(addp), "auth", "status"], env, "browser status")
        results.append(browser_status)
        assert_context(parse_json_output(browser_status), "101", "1001")

        before_race = len(fixture.refresh_submissions)
        racers = [
            subprocess.Popen(
                [sys.executable, "-c", helper],
                env=env,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
            for _ in range(2)
        ]
        for racer in racers:
            result = collect_process(racer, "cross-process refresh")
            results.append(result)
            assert_refresh_process_succeeded(result)
        race_tokens = fixture.refresh_submissions[before_race:]
        assert_refresh_rotation_serialized(race_tokens)

        bound_status = run_command([str(addp), "auth", "status"], env, "context-bound status")
        results.append(bound_status)
        assert_context(parse_json_output(bound_status), "101", "1001")

        browser_logout = run_command([str(addp), "auth", "logout"], env, "browser logout")
        results.append(browser_logout)
        if parse_json_output(browser_logout).get("authenticated") is not False:
            raise AssertionError("logout response is invalid")
        if keyring.get_password(KEYRING_SERVICE, fixture.base_url) is not None:
            raise AssertionError("logout did not delete the OS Keychain refresh token")
        if not fixture.revoked_tokens:
            raise AssertionError("logout did not call OAuth revocation")

        logged_out_status = run_command([str(addp), "auth", "status"], env, "logged-out status")
        results.append(logged_out_status)
        if parse_json_output(logged_out_status) != {"authenticated": False, "base_url": fixture.base_url}:
            raise AssertionError("status did not confirm the revoked local session")

        device_login = run_command([str(addp), "auth", "login", "--device"], env, "device login")
        results.append(device_login)
        assert_context(parse_json_output(device_login), "202", "2002")
        if "E2E2-CLI2" not in device_login.stderr or fixture.base_url + "/oauth/device" not in device_login.stderr:
            raise AssertionError("device instructions were not written to stderr")

        device_status = run_command([str(addp), "auth", "status"], env, "device status")
        results.append(device_status)
        assert_context(parse_json_output(device_status), "202", "2002")
        results.append(run_command([str(addp), "auth", "logout"], env, "device logout"))

        assert_no_secrets(results, fixture)
        print(
            json.dumps(
                {
                    "schema_version": "addp.cli-product-e2e/v1",
                    "status": "passed",
                    "keyring_backend": backend_module + "." + type(backend).__name__,
                    "checks": [
                        "manual_access_token_rejected",
                        "browser_loopback_pkce",
                        "device_flow",
                        "authoritative_context_binding",
                        "cross_process_refresh_rotation",
                        "oauth_revocation",
                        "terminal_secret_redaction",
                    ],
                },
                separators=(",", ":"),
            )
        )
        return 0
    finally:
        try:
            keyring.delete_password(KEYRING_SERVICE, fixture.base_url)
        except keyring.errors.PasswordDeleteError:
            pass
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--browser")
    parser.add_argument("--addp", type=Path)
    arguments = parser.parse_args()
    if arguments.browser:
        raise SystemExit(run_browser(arguments.browser))
    if arguments.addp is None:
        parser.error("--addp is required")
    raise SystemExit(main(arguments.addp.resolve()))
