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
async def test_refresh_access_token_rotates_keyring_value(monkeypatch):
    stored = []
    monkeypatch.setattr(oauth, "_refresh_token", lambda _base_url: "addp_rt_old")
    monkeypatch.setattr(oauth, "_store_refresh_token", lambda base_url, token: stored.append((base_url, token)))
    monkeypatch.setattr(oauth.httpx, "AsyncClient", FakeAsyncClient)

    access_token = await oauth.refresh_access_token("http://localhost:8000")

    assert access_token == "addp_at_new"
    assert stored == [("http://localhost:8000", "addp_rt_new")]
