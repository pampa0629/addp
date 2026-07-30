import subprocess

import pytest

from cli_product_e2e import (
    OAuthFixture,
    assert_command_succeeded,
    assert_context,
    assert_manual_token_rejected,
    assert_no_secrets,
    assert_refresh_process_succeeded,
    assert_refresh_rotation_serialized,
    capture_command,
    collect_process,
    parse_json_output,
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
