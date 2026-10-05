import httpx
import pytest

from addp_common.client.raster_policy import RasterPolicyClient


@pytest.mark.parametrize('statuses,expected_tokens', [([200], ['first']), ([401, 200], ['first', 'second']), ([503], ['first']), ([401, 401], ['first', 'second'])])
def test_authoritative_policy_client_retries_only_one_expired_token(monkeypatch, statuses, expected_tokens):
    import addp_common.client.raster_policy as module
    calls, invalidated = [], []
    class Tokens:
        def __init__(self, url, client, secret, **kwargs):
            assert client == 'addp-geopython' and secret == 'credential'
        def platform_token(self):
            return 'second' if invalidated else 'first'
        def invalidate_platform(self, value):
            invalidated.append(value)
        def close(self):
            pass
    def handle(request):
        import json
        calls.append(request.headers['authorization'].removeprefix('Bearer '))
        assert str(request.url) == 'http://system/api/v1/system/runtime/engine-raster-policy'
        assert json.loads(request.content) == {'connection_info': {'protocol': 'http', 'host': 'runtime', 'port': 8099}}
        assert b'credential' not in request.content
        return httpx.Response(statuses[len(calls) - 1], json={'policy': {'version': 1}, 'quotas': []})
    factory = httpx.Client
    monkeypatch.setattr(module, 'SyncOAuthServiceTokenSource', Tokens)
    monkeypatch.setattr(module.httpx, 'Client', lambda **kwargs: factory(transport=httpx.MockTransport(handle), **kwargs))
    client = RasterPolicyClient('http://system/', 'credential', {'protocol': 'http', 'host': 'runtime', 'port': 8099})
    if statuses[-1] == 200:
        assert client.fetch()['policy']['version'] == 1
    else:
        with pytest.raises(httpx.HTTPStatusError):
            client.fetch()
    assert calls == expected_tokens
    assert invalidated == (['first'] if statuses[0] == 401 else [])
