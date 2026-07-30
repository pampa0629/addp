import argparse
import asyncio
import json
import uuid
from unittest.mock import patch

import pytest

from addp_common import cli
from addp_common.auth import AuthorizationContext
from addp_common.oauth import LoginResult, OAuthLoginError


def _oauth_context():
    return AuthorizationContext(
        principal_id=42,
        context_type="tenant",
        tenant_id=7,
        tenant_membership_id=9,
        client_id="addp-cli",
        scope_mode="restricted",
        scopes=("addp.api",),
        token_type="oauth_access_token",
    )


def test_tools_list_writes_strict_json(capsys):
    code = asyncio.run(cli._run(argparse.Namespace(
        group="tools",
        command="list",
        base_url="http://gateway",
        token=None,
    )))
    output = capsys.readouterr().out
    assert code == cli.EXIT_OK
    assert json.loads(output)[0]["name"] == "engine.list"
    assert "\n" not in output.rstrip("\n")


def test_tool_call_requires_user_token(capsys):
    async def missing_login(_base_url):
        raise OAuthLoginError("authentication_required")

    with patch.object(cli, "refresh_access_token", missing_login):
        code = asyncio.run(cli._run(argparse.Namespace(
            group="tool",
            command="call",
            name="data.search",
            arguments='{"query":"roads"}',
            base_url="http://gateway",
        )))
    payload = json.loads(capsys.readouterr().out)
    assert code == cli.EXIT_USAGE
    assert payload["error"]["code"] == "authentication_required"


def test_tool_call_outputs_result_without_adapter_envelope(capsys):
    async def fake_refresh(base_url):
        assert base_url == "http://gateway"
        return "token"

    class FakeExecutor:
        def __init__(self, base_url, token):
            assert base_url == "http://gateway"
            assert token == "token"

        async def call(self, name, arguments, *, agent_run_id, tool_call_id):
            assert name == "data.search"
            assert arguments == {"query": "roads"}
            assert uuid.UUID(agent_run_id)
            assert uuid.UUID(tool_call_id)
            return {"total": 1, "hits": []}

    with patch.object(cli, "refresh_access_token", fake_refresh), patch.object(cli, "ToolExecutor", FakeExecutor):
        code = asyncio.run(cli._run(argparse.Namespace(
            group="tool",
            command="call",
            name="data.search",
            arguments='{"query":"roads"}',
            base_url="http://gateway",
        )))

    assert code == cli.EXIT_OK
    assert json.loads(capsys.readouterr().out) == {"total": 1, "hits": []}


def test_device_login_keeps_stdout_single_json_and_secrets_off_terminal(capsys):
    async def fake_device_login(base_url, on_device):
        assert base_url == "http://gateway"
        on_device({
            "device_code": "addp_dc_secret",
            "user_code": "ABCD-EFGH",
            "verification_uri": "http://console/oauth/device",
            "verification_uri_complete": "http://console/oauth/device?user_code=ABCD-EFGH",
            "expires_in": 600,
            "interval": 5,
        })
        return LoginResult(client_id="addp-cli", scope="addp.api", access_token="addp_at_secret"), {}

    async def fake_resolve(base_url, access_token):
        assert base_url == "http://gateway"
        assert access_token == "addp_at_secret"
        return _oauth_context()

    with patch.object(cli, "device_login", fake_device_login), patch.object(
        cli, "_resolve_authentication", fake_resolve
    ):
        code = asyncio.run(cli._run(argparse.Namespace(
            group="auth",
            command="login",
            device=True,
            base_url="http://gateway",
        )))

    captured = capsys.readouterr()
    assert code == cli.EXIT_OK
    assert json.loads(captured.out) == {
        "authenticated": True,
        "base_url": "http://gateway",
        "principal": {"type": "user", "id": "42"},
        "context": {"type": "tenant", "tenant_id": "7", "tenant_membership_id": "9"},
        "authentication": {"assurance_level": "aal1"},
        "client": {"client_id": "addp-cli", "scope_mode": "restricted", "scopes": ["addp.api"]},
    }
    assert "ABCD-EFGH" in captured.err
    assert "http://console/oauth/device" in captured.err
    assert "addp_dc_secret" not in captured.out + captured.err
    assert "addp_at_secret" not in captured.out + captured.err


def test_auth_status_refreshes_and_resolves_authoritative_context(capsys):
    async def fake_refresh(base_url):
        assert base_url == "http://gateway"
        return "addp_at_status"

    async def fake_resolve(base_url, access_token):
        assert base_url == "http://gateway"
        assert access_token == "addp_at_status"
        return _oauth_context()

    with patch.object(cli, "refresh_access_token", fake_refresh), patch.object(
        cli, "_resolve_authentication", fake_resolve
    ):
        code = asyncio.run(cli._run(argparse.Namespace(
            group="auth",
            command="status",
            base_url="http://gateway",
        )))

    payload = json.loads(capsys.readouterr().out)
    assert code == cli.EXIT_OK
    assert payload["authenticated"] is True
    assert payload["context"] == {
        "type": "tenant",
        "tenant_id": "7",
        "tenant_membership_id": "9",
    }


def test_auth_status_distinguishes_missing_login_from_service_failure(capsys):
    async def missing_login(_base_url):
        raise OAuthLoginError("authentication_required")

    with patch.object(cli, "refresh_access_token", missing_login):
        code = asyncio.run(cli._run(argparse.Namespace(
            group="auth",
            command="status",
            base_url="http://gateway",
        )))

    assert code == cli.EXIT_OK
    assert json.loads(capsys.readouterr().out) == {
        "authenticated": False,
        "base_url": "http://gateway",
    }

    async def unavailable(_base_url):
        raise OAuthLoginError("temporarily_unavailable")

    with patch.object(cli, "refresh_access_token", unavailable):
        code = asyncio.run(cli._run(argparse.Namespace(
            group="auth",
            command="status",
            base_url="http://gateway",
        )))

    assert code == cli.EXIT_EXECUTION_FAILED
    assert json.loads(capsys.readouterr().out)["error"]["code"] == "authentication_unavailable"


def test_version_is_one_json_document(capsys):
    with pytest.raises(SystemExit) as exited:
        cli._parser().parse_args(["--version"])

    assert exited.value.code == cli.EXIT_OK
    assert json.loads(capsys.readouterr().out) == {
        "name": "addp",
        "version": cli.PACKAGE_VERSION,
    }


def test_cli_rejects_manual_access_token_inputs(monkeypatch, capsys):
    manual_token = "addp_at_manual_must_not_be_logged"
    with pytest.raises(SystemExit) as exited:
        cli._parser().parse_args(["--token", manual_token, "tools", "list"])

    assert exited.value.code == cli.EXIT_USAGE
    output = capsys.readouterr().out
    assert json.loads(output)["error"]["code"] == "invalid_command"
    assert manual_token not in output

    monkeypatch.setenv("ADDP_TOKEN", "addp_at_environment")
    parsed = cli._parser().parse_args(["tool", "call", "data.search"])
    assert not hasattr(parsed, "token")


def test_tool_call_validates_json_before_refreshing_login(capsys):
    async def unexpected_refresh(_base_url):
        raise AssertionError("refresh must not run for invalid local arguments")

    with patch.object(cli, "refresh_access_token", unexpected_refresh):
        code = asyncio.run(cli._run(argparse.Namespace(
            group="tool",
            command="call",
            name="data.search",
            arguments="not-json",
            base_url="http://gateway",
        )))

    assert code == cli.EXIT_USAGE
    assert json.loads(capsys.readouterr().out)["error"]["code"] == "invalid_json"


def test_tool_call_reports_temporary_refresh_failure_without_requesting_login(capsys):
    async def unavailable_refresh(_base_url):
        raise OAuthLoginError("temporarily_unavailable")

    with patch.object(cli, "refresh_access_token", unavailable_refresh):
        code = asyncio.run(cli._run(argparse.Namespace(
            group="tool",
            command="call",
            name="data.search",
            arguments="{}",
            base_url="http://gateway",
        )))

    assert code == cli.EXIT_EXECUTION_FAILED
    assert json.loads(capsys.readouterr().out)["error"] == {
        "code": "authentication_unavailable",
        "message": "无法刷新 ADDP 登录会话: temporarily_unavailable",
    }


def test_tool_call_internal_error_does_not_log_exception_message(capsys):
    async def fake_refresh(_base_url):
        return "addp_at_input"

    class FailingExecutor:
        def __init__(self, _base_url, _token):
            pass

        async def call(self, *_args, **_kwargs):
            raise RuntimeError("addp_rt_must_not_reach_terminal")

    with patch.object(cli, "refresh_access_token", fake_refresh), patch.object(cli, "ToolExecutor", FailingExecutor):
        code = asyncio.run(cli._run(argparse.Namespace(
            group="tool",
            command="call",
            name="data.search",
            arguments="{}",
            base_url="http://gateway",
            agent_run_id=None,
            tool_call_id=None,
        )))

    captured = capsys.readouterr()
    assert code == cli.EXIT_EXECUTION_FAILED
    assert json.loads(captured.out)["error"]["code"] == "internal_error"
    assert "RuntimeError" in captured.err
    assert "addp_rt_must_not_reach_terminal" not in captured.out + captured.err
