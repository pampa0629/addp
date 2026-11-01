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
    browser_timeout_imports,
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
        run_browser_login(Path(secret), {}, fixture)
    assert str(exc_info.value) == (
        'browser login timed out; browser_fixture_progress={'
        f'"authorization_requests":{request_count},"pending_authorization_codes":{code_count},'
        f'"token_families":{family_count}' + '}; browser_completed_import_us={}'
    )
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
    assert run_browser_login(Path("addp"), env, OAuthFixture()) is result
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
        run_browser_login(Path(secret), {}, OAuthFixture())
    message = str(exc_info.value)
    assert message.endswith('browser_completed_import_us={"addp_common.oauth":400,"keyring":100}')
    assert secret not in message


def test_browser_timeout_import_diagnostic_bounds_input_and_ignores_other_errors():
    output = "import time: 1 | 2 | keyring\n" + "x" * 65536
    error = subprocess.TimeoutExpired(["addp"], 30, stderr=output)
    assert browser_timeout_imports(error) == {}
    assert browser_timeout_imports(None) == {}
    assert browser_timeout_imports(RuntimeError("private")) == {}
    assert browser_timeout_imports(subprocess.TimeoutExpired(["addp"], 30)) == {}
