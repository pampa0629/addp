import httpx

from addp_common.client.runtime_registration import register_runtime_engine, retry_runtime_registration
from addp_common.client.service_token import SyncOAuthServiceTokenSource


def test_sync_service_token_source_uses_tenant_client_credentials():
    def handler(request):
        assert request.url.path == "/api/v1/system/oauth/token"
        assert request.headers["Authorization"].startswith("Basic ")
        assert "tenant_id=7" in request.content.decode()
        assert "context_type" not in request.content.decode()
        return httpx.Response(200, json={
            "access_token": "addp_at_sync_tenant_token",
            "token_type": "Bearer",
            "expires_in": 120,
            "scope": "addp.api",
        })

    source = SyncOAuthServiceTokenSource(
        "http://system",
        "addp-geopython",
        "test-service-client-secret-32bytes",
        transport=httpx.MockTransport(handler),
    )
    try:
        assert source.token(7) == "addp_at_sync_tenant_token"
    finally:
        source.close()


def test_register_runtime_engine_uses_platform_bearer(monkeypatch):
    captured = {}

    class FakeTokenSource:
        def __init__(self, system_url, client_id, client_secret, *, timeout):
            captured["token_source"] = (system_url, client_id, client_secret, timeout)

        def platform_token(self):
            return "addp_at_platform_runtime_token"

        def close(self):
            captured["closed"] = True

    class FakeResponse:
        status_code = 202
        text = "accepted"

    class FakeClient:
        def __init__(self, *, timeout, trust_env):
            captured["client"] = (timeout, trust_env)

        def __enter__(self):
            return self

        def __exit__(self, *_args):
            return None

        def post(self, url, *, json, headers):
            captured["request"] = (url, json, headers)
            return FakeResponse()

    monkeypatch.setattr("addp_common.client.runtime_registration.SyncOAuthServiceTokenSource", FakeTokenSource)
    monkeypatch.setattr("addp_common.client.runtime_registration.httpx.Client", FakeClient)

    status, body = register_runtime_engine(
        "http://system",
        "addp-geopython",
        "test-service-client-secret-32bytes",
        {"engine_type": "geopython_workflow"},
    )

    assert (status, body) == (202, "accepted")
    assert captured["request"] == (
        "http://system/api/v1/system/runtime/engines",
        {"engine_type": "geopython_workflow"},
        {"Authorization": "Bearer addp_at_platform_runtime_token"},
    )
    assert captured["closed"] is True


def test_retry_runtime_registration_uses_bounded_backoff_until_success():
    attempts = []
    waits = []

    class Logger:
        def info(self, *_args):
            pass

    def register():
        attempts.append(len(attempts) + 1)
        return len(attempts) == 4

    retry_runtime_registration(
        register,
        "test_runtime",
        Logger(),
        initial_interval=1,
        max_interval=2,
        wait=waits.append,
    )

    assert attempts == [1, 2, 3, 4]
    assert waits == [1, 2, 2]
