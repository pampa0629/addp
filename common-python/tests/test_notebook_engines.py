import httpx
import pytest

from addp_common.notebook import engines


def test_list_uses_only_injected_session_capability(monkeypatch):
    endpoint = "http://develop:8185/api/v1/develop/notebook-kernel-sessions/session-1/engine-descriptors"
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_API_ENDPOINT", endpoint)
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN", "addp_nkc_kernel-secret")
    monkeypatch.setenv("ADDP_TOKEN", "must-not-be-used")

    def handler(request: httpx.Request) -> httpx.Response:
        assert str(request.url) == endpoint
        assert request.headers["Authorization"] == "Bearer addp_nkc_kernel-secret"
        assert request.headers["Cache-Control"] == "no-store"
        assert "must-not-be-used" not in str(request.headers)
        return httpx.Response(200, json=[{"id": 21, "name": "PostgreSQL", "engine_type": "postgresql"}])

    monkeypatch.setattr(engines, "_transport", httpx.MockTransport(handler))
    assert engines.list() == [{"id": 21, "name": "PostgreSQL", "engine_type": "postgresql"}]


@pytest.mark.parametrize(
    ("endpoint", "token"),
    [
        ("", "addp_nkc_kernel-secret"),
        ("http://develop/session", ""),
        ("http://develop/session", "wrong-prefix"),
    ],
)
def test_list_fails_closed_without_valid_session_environment(monkeypatch, endpoint, token):
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_API_ENDPOINT", endpoint)
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN", token)
    with pytest.raises(engines.NotebookSessionUnavailableError):
        engines.list()


def test_list_reports_http_failure_without_exposing_token(monkeypatch):
    secret = "addp_nkc_do-not-leak"
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_API_ENDPOINT", "http://develop/session")
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN", secret)
    monkeypatch.setattr(
        engines,
        "_transport",
        httpx.MockTransport(lambda _request: httpx.Response(401, json={"error": "unauthorized"})),
    )
    with pytest.raises(engines.NotebookEngineDiscoveryError) as error:
        engines.list()
    assert secret not in str(error.value)


def test_list_rejects_non_array_response(monkeypatch):
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_API_ENDPOINT", "http://develop/session")
    monkeypatch.setenv("ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN", "addp_nkc_kernel-secret")
    monkeypatch.setattr(
        engines,
        "_transport",
        httpx.MockTransport(lambda _request: httpx.Response(200, json={"data": []})),
    )
    with pytest.raises(engines.NotebookEngineDiscoveryError):
        engines.list()
