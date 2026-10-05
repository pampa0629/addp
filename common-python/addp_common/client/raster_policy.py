"""Authoritative System resource policy client; only Platform Service OAuth."""
import httpx
from .service_token import SyncOAuthServiceTokenSource


class RasterPolicyClient:
    def __init__(self, system_url, client_secret, connection_info, *, timeout=5):
        self._url = system_url.rstrip('/') + '/api/v1/system/runtime/engine-raster-policy'
        self._identity, self._timeout = dict(connection_info), timeout
        self._tokens = SyncOAuthServiceTokenSource(system_url, 'addp-geopython', client_secret, timeout=timeout)

    def fetch(self):
        token = self._tokens.platform_token()
        with httpx.Client(timeout=self._timeout, trust_env=False) as client:
            response = client.post(self._url, json={'connection_info': self._identity}, headers={'Authorization': 'Bearer ' + token})
            if response.status_code == 401:
                self._tokens.invalidate_platform(token)
                token = self._tokens.platform_token()
                response = client.post(self._url, json={'connection_info': self._identity}, headers={'Authorization': 'Bearer ' + token})
            response.raise_for_status()
            return response.json()

    def close(self):
        self._tokens.close()
