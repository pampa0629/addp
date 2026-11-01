import json
import subprocess
from pathlib import Path

import pytest

from cli_product_e2e import (
    OAuthFixture,
    assert_command_succeeded,
    assert_context,
    assert_manual_token_rejected,
    assert_no_secrets,
    assert_refresh_process_succeeded,
    assert_refresh_rotation_serialized,
    browser_timeout_import_progress,
    capture_command,
    collect_process,
    parse_json_output,
    run_browser_login,
)


def assert_diagnostic_redacts(secret: str, assertion) -> None:
    with pytest.raises(AssertionError) as exc_info:
        assertion()
    assert secret not in str(exc_info.value)


def test_process_and_json_diagnostics_do_not_disclose_terminal_secrets():
    secret = "addp_rt_regression_process_secret"
    failed = subprocess.CompletedProcess(
        ["addp", "--token", secret],
        9,
        stdout=secret,
        stderr=secret,
    )

    assert_diagnostic_redacts(secret, lambda: assert_command_succeeded(failed, "test operation"))
    assert_diagnostic_redacts(secret, lambda: assert_manual_token_rejected(failed))
    assert_diagnostic_redacts(secret, lambda: assert_refresh_process_succeeded(failed))
    assert_diagnostic_redacts(secret, lambda: parse_json_output(failed))


def test_payload_and_refresh_diagnostics_do_not_disclose_oauth_secrets():
    secret = "addp_at_regression_payload_secret"
    payload = {
        "context": {"type": "tenant", "tenant_id": secret},
        "client": {"client_id": "unexpected"},
    }

    assert_diagnostic_redacts(secret, lambda: assert_context(payload, "101", "1001"))
    assert_diagnostic_redacts(secret, lambda: assert_refresh_rotation_serialized([secret, secret]))


def test_terminal_leak_assertion_reports_only_the_secret_count():
    secret = "addp_rt_regression_terminal_secret"
    fixture = OAuthFixture()
    fixture.sensitive_values.add(secret)
    result = subprocess.CompletedProcess(["addp"], 0, stdout=secret, stderr="")

    assert_diagnostic_redacts(secret, lambda: assert_no_secrets([result], fixture))


def test_command_timeout_diagnostic_does_not_disclose_command_or_output(monkeypatch):
    secret = "addp_at_regression_timeout_secret"

    def time_out(*_args, **_kwargs):
        raise subprocess.TimeoutExpired(["addp", "--token", secret], 30, output=secret, stderr=secret)

    monkeypatch.setattr(subprocess, "run", time_out)
    assert_diagnostic_redacts(
        secret,
        lambda: capture_command(["addp", "--token", secret], {}, "manual token rejection"),
    )


def test_process_timeout_diagnostic_discards_captured_output():
    secret = "addp_rt_regression_timeout_secret"

    class TimedOutProcess:
        args = ["python", secret]
        returncode = None

        def __init__(self):
            self.communicate_count = 0

        def communicate(self, timeout=None):
            self.communicate_count += 1
            if self.communicate_count == 1:
                raise subprocess.TimeoutExpired(self.args, timeout, output=secret, stderr=secret)
            return secret, secret

        def kill(self):
            self.returncode = -9

    process = TimedOutProcess()
    assert_diagnostic_redacts(secret, lambda: collect_process(process, "refresh process"))
    assert process.communicate_count == 2
    assert process.returncode == -9


@pytest.mark.parametrize("request_count,code_count,family_count", [(0, 0, 0), (1, 0, 0), (1, 1, 0), (1, 0, 1)])
def test_browser_timeout_reports_only_fixture_counts(monkeypatch, request_count, code_count, family_count):
    secret = "addp_rt_browser_timeout_secret"
    fixture = OAuthFixture()
    fixture.authorization_requests = {f"{secret}{i}": {"secret": secret} for i in range(request_count)}
    fixture.authorization_codes = {f"{secret}{i}": {"secret": secret} for i in range(code_count)}
    fixture.families = {f"{secret}{i}": {"secret": secret} for i in range(family_count)}

    def time_out(command, **kwargs):
        assert command == [secret, "auth", "login"]
        assert kwargs["timeout"] == 30
        assert kwargs["env"]["PYTHONPROFILEIMPORTTIME"] == "1"
        raise subprocess.TimeoutExpired(command, 30, output=secret, stderr=secret)

    monkeypatch.setattr(subprocess, "run", time_out)
    with pytest.raises(AssertionError, match="browser login timed out") as exc_info:
        run_browser_login(Path(secret), {}, fixture, startup_stderr="")
    assert str(exc_info.value).startswith(
        'browser login timed out; browser_fixture_progress={'
        f'"authorization_requests":{request_count},"pending_authorization_codes":{code_count},'
        f'"token_families":{family_count}' + '}; browser_import_progress='
    )
    progress = json.loads(str(exc_info.value).split("browser_import_progress=", 1)[1])
    assert progress["timeout_context"] is True
    assert progress["completed_import_us"] == {}
    assert progress["profile_rows"] == 0
    assert progress["captured_length"] == len(secret)
    assert secret not in str(exc_info.value)


def test_browser_login_returns_original_command_result(monkeypatch):
    result = subprocess.CompletedProcess(["addp", "auth", "login"], 0, stdout="{}", stderr="")
    env = {"NO_PROXY": "127.0.0.1"}

    def succeed(command, **kwargs):
        assert command == result.args
        assert kwargs["timeout"] == 30
        assert kwargs["env"] == {**env, "PYTHONPROFILEIMPORTTIME": "1"}
        return result

    monkeypatch.setattr(subprocess, "run", succeed)
    assert run_browser_login(Path("addp"), env, OAuthFixture(), startup_stderr="private") is result
    assert env == {"NO_PROXY": "127.0.0.1"}


@pytest.mark.parametrize("as_bytes", [False, True])
def test_browser_timeout_import_diagnostic_is_closed_and_redacted(monkeypatch, as_bytes):
    secret = "addp_rt_import_diagnostic_secret"
    stderr = (
        f"{secret}\nimport time: 10 | 100 | keyring\n"
        f"import time: 20 | 200 | {secret}\n"
        f"import time: 30 | 300 | addp_common.cli{secret}\n"
        "import time: 40 | 400 | addp_common.oauth\n"
    )
    if as_bytes:
        stderr = stderr.encode()

    def time_out(command, **kwargs):
        raise subprocess.TimeoutExpired(command, kwargs["timeout"], output=secret, stderr=stderr)

    monkeypatch.setattr(subprocess, "run", time_out)
    with pytest.raises(AssertionError) as exc_info:
        run_browser_login(Path(secret), {}, OAuthFixture(), startup_stderr="")
    message = str(exc_info.value)
    progress = json.loads(message.split("browser_import_progress=", 1)[1])
    assert progress["completed_import_us"] == {"addp_common.oauth": 400, "keyring": 100}
    assert progress["profile_rows"] == 4
    assert progress["stream"] == ("bytes" if as_bytes else "text")
    assert progress["captured_length"] == len(stderr)
    assert progress["examined_length"] == len(stderr)
    assert progress["truncated"] is False
    assert secret not in message


def test_browser_timeout_import_diagnostic_bounds_input_and_ignores_other_errors():
    output = "import time: 1 | 2 | keyring\n" + "x" * 65536 + "\nimport time: 3 | 4 | asyncio\n"
    error = subprocess.TimeoutExpired(["addp"], 30, stderr=output)
    progress = browser_timeout_import_progress(error)
    assert progress["completed_import_us"] == {"keyring": 2, "asyncio": 4}
    assert progress["captured_length"] == len(output)
    assert progress["examined_length"] == 65536
    assert progress["profile_rows"] == 2
    assert progress["truncated"] is True
    for error in [None, RuntimeError("private"), subprocess.TimeoutExpired(["addp"], 30)]:
        progress = browser_timeout_import_progress(error)
        assert progress["timeout_context"] is isinstance(error, subprocess.TimeoutExpired)
        assert progress["stream"] == "none"
        assert progress["captured_length"] == progress["examined_length"] == progress["profile_rows"] == 0
        assert progress["completed_import_us"] == {}


def test_browser_timeout_import_diagnostic_does_not_join_truncated_rows():
    output = "x" * 32755 + "\nimport time:" + "x" * 65536 + " 1 | 2 | keyring\n" + "x" * 32751
    progress = browser_timeout_import_progress(subprocess.TimeoutExpired(["addp"], 30, stderr=output))
    assert progress["examined_length"] == 65536
    assert progress["completed_import_us"] == {}


@pytest.mark.parametrize("as_bytes", [False, True])
def test_browser_timeout_compares_existing_cli_startup_without_disclosing_output(monkeypatch, as_bytes):
    secret = "addp_at_preceding_cli_control_secret"
    startup_stderr = (
        f"{secret}\nimport time: 1 | 12 | keyring\n"
        f"import time: 2 | 34 | {secret}\n"
        "import time: 3 | 56 | addp_common.cli\n"
    )
    if as_bytes:
        startup_stderr = startup_stderr.encode()
    calls = []

    def time_out(command, **kwargs):
        calls.append(command)
        assert kwargs["timeout"] == 30
        assert kwargs["env"]["PYTHONPROFILEIMPORTTIME"] == "1"
        raise subprocess.TimeoutExpired(command, 30)

    monkeypatch.setattr(subprocess, "run", time_out)
    with pytest.raises(AssertionError) as exc_info:
        run_browser_login(Path(secret), {}, OAuthFixture(), startup_stderr=startup_stderr)
    message = str(exc_info.value)
    progress = json.loads(message.split("browser_import_progress=", 1)[1])
    control = progress["preceding_cli_import_progress"]
    assert progress["stream"] == "none"
    assert progress["captured_length"] == 0
    assert control["timeout_context"] is False
    assert control["stream"] == ("bytes" if as_bytes else "text")
    assert control["captured_length"] == control["examined_length"] == len(startup_stderr)
    assert control["profile_rows"] == 3
    assert control["completed_import_us"] == {"keyring": 12, "addp_common.cli": 56}
    assert secret not in message
    assert calls == [[secret, "auth", "login"]]
