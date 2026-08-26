"""OAuth Client Credentials service token provider."""

from __future__ import annotations

import asyncio
import threading
import time
from dataclasses import dataclass
from urllib.parse import urlsplit

import httpx


class ServiceTokenError(RuntimeError):
    """Raised when a service access token cannot be obtained."""

    def __init__(self, code: str, *, status_code: int = 0, retryable: bool = False) -> None:
        super().__init__(code)
        self.code = code
        self.status_code = status_code
        self.retryable = retryable


@dataclass(frozen=True)
class _CachedToken:
    value: str
    expires_at: float


class OAuthServiceTokenSource:
    """Obtain and cache tenant-scoped Service Access Tokens from System."""

    def __init__(
        self,
        system_url: str,
        client_id: str,
        client_secret: str,
        *,
        timeout: float = 10.0,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        system_url = system_url.strip().rstrip("/")
        client_id = client_id.strip()
        parsed = urlsplit(system_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc or parsed.query or parsed.fragment:
            raise ValueError("service token source System URL must be an absolute HTTP(S) URL")
        if not client_id or len(client_secret) < 32:
            raise ValueError("service token source requires a client ID and a 32-byte client secret")

        self._token_url = system_url + "/api/v1/system/oauth/token"
        self._client_id = client_id
        self._client_secret = client_secret
        self._client = httpx.AsyncClient(timeout=timeout, transport=transport, trust_env=False)
        self._cache: dict[str, _CachedToken] = {}
        self._locks: dict[str, asyncio.Lock] = {}

    async def token(self, tenant_id: int) -> str:
        if not isinstance(tenant_id, int) or isinstance(tenant_id, bool) or tenant_id <= 0:
            raise ValueError("service token requires a tenant ID")
        return await self._token(f"tenant:{tenant_id}", {"tenant_id": str(tenant_id)})

    async def platform_token(self) -> str:
        return await self._token("platform", {"context_type": "platform"})

    async def _token(self, cache_key: str, context_form: dict[str, str]) -> str:
        lock = self._locks.setdefault(cache_key, asyncio.Lock())
        async with lock:
            now = time.time()
            cached = self._cache.get(cache_key)
            if cached is not None and cached.expires_at > now + 30:
                return cached.value

            try:
                response = await self._client.post(
                    self._token_url,
                    data={
                        "grant_type": "client_credentials",
                        "scope": "addp.api",
                        "audience": "addp.api",
                        **context_form,
                    },
                    auth=(self._client_id, self._client_secret),
                    headers={"Content-Type": "application/x-www-form-urlencoded"},
                )
            except httpx.HTTPError as exc:
                raise ServiceTokenError("service_token_unavailable", retryable=True) from exc
            if response.status_code != 200:
                raise ServiceTokenError(
                    f"service_token_http_{response.status_code}",
                    status_code=response.status_code,
                    retryable=response.status_code == 429 or response.status_code >= 500,
                )
            try:
                payload = response.json()
                access_token = payload["access_token"]
                token_type = payload["token_type"]
                expires_in = payload["expires_in"]
                scope = payload.get("scope", "")
            except (KeyError, TypeError, ValueError) as exc:
                raise ServiceTokenError("invalid_service_token_response") from exc
            if (
                not isinstance(access_token, str)
                or not access_token.startswith("addp_at_")
                or not isinstance(token_type, str)
                or token_type.lower() != "bearer"
                or not isinstance(expires_in, int)
                or isinstance(expires_in, bool)
                or expires_in <= 0
                or expires_in > 300
                or scope not in {"", "addp.api"}
            ):
                raise ServiceTokenError("invalid_service_token_response")
            self._cache[cache_key] = _CachedToken(access_token, now + expires_in)
            return access_token

    def invalidate(self, tenant_id: int, rejected_token: str) -> None:
        self._invalidate(f"tenant:{tenant_id}", rejected_token)

    def invalidate_platform(self, rejected_token: str) -> None:
        self._invalidate("platform", rejected_token)

    def _invalidate(self, cache_key: str, rejected_token: str) -> None:
        cached = self._cache.get(cache_key)
        if cached is not None and cached.value == rejected_token:
            self._cache.pop(cache_key, None)

    async def close(self) -> None:
        await self._client.aclose()

    async def __aenter__(self) -> "OAuthServiceTokenSource":
        return self

    async def __aexit__(self, *_args: object) -> None:
        await self.close()


class SyncOAuthServiceTokenSource:
    """Synchronous token source for Flask-based workflow runtimes."""

    def __init__(
        self,
        system_url: str,
        client_id: str,
        client_secret: str,
        *,
        timeout: float = 10.0,
        transport: httpx.BaseTransport | None = None,
    ) -> None:
        system_url = system_url.strip().rstrip("/")
        client_id = client_id.strip()
        parsed = urlsplit(system_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc or parsed.query or parsed.fragment:
            raise ValueError("service token source System URL must be an absolute HTTP(S) URL")
        if not client_id or len(client_secret) < 32:
            raise ValueError("service token source requires a client ID and a 32-byte client secret")
        self._token_url = system_url + "/api/v1/system/oauth/token"
        self._client_id = client_id
        self._client_secret = client_secret
        self._client = httpx.Client(timeout=timeout, transport=transport, trust_env=False)
        self._cache: dict[str, _CachedToken] = {}
        self._lock = threading.Lock()

    def token(self, tenant_id: int) -> str:
        if not isinstance(tenant_id, int) or isinstance(tenant_id, bool) or tenant_id <= 0:
            raise ValueError("service token requires a tenant ID")
        return self._token(f"tenant:{tenant_id}", {"tenant_id": str(tenant_id)})

    def platform_token(self) -> str:
        return self._token("platform", {"context_type": "platform"})

    def _token(self, cache_key: str, context_form: dict[str, str]) -> str:
        with self._lock:
            now = time.time()
            cached = self._cache.get(cache_key)
            if cached is not None and cached.expires_at > now + 30:
                return cached.value
            try:
                response = self._client.post(
                    self._token_url,
                    data={
                        "grant_type": "client_credentials",
                        "scope": "addp.api",
                        "audience": "addp.api",
                        **context_form,
                    },
                    auth=(self._client_id, self._client_secret),
                    headers={"Content-Type": "application/x-www-form-urlencoded"},
                )
            except httpx.HTTPError as exc:
                raise ServiceTokenError("service_token_unavailable", retryable=True) from exc
            if response.status_code != 200:
                raise ServiceTokenError(
                    f"service_token_http_{response.status_code}",
                    status_code=response.status_code,
                    retryable=response.status_code == 429 or response.status_code >= 500,
                )
            try:
                payload = response.json()
                access_token = payload["access_token"]
                token_type = payload["token_type"]
                expires_in = payload["expires_in"]
                scope = payload.get("scope", "")
            except (KeyError, TypeError, ValueError) as exc:
                raise ServiceTokenError("invalid_service_token_response") from exc
            if (
                not isinstance(access_token, str)
                or not access_token.startswith("addp_at_")
                or not isinstance(token_type, str)
                or token_type.lower() != "bearer"
                or not isinstance(expires_in, int)
                or isinstance(expires_in, bool)
                or expires_in <= 0
                or expires_in > 300
                or scope not in {"", "addp.api"}
            ):
                raise ServiceTokenError("invalid_service_token_response")
            self._cache[cache_key] = _CachedToken(access_token, now + expires_in)
            return access_token

    def invalidate(self, tenant_id: int, rejected_token: str) -> None:
        self._invalidate(f"tenant:{tenant_id}", rejected_token)

    def invalidate_platform(self, rejected_token: str) -> None:
        self._invalidate("platform", rejected_token)

    def _invalidate(self, cache_key: str, rejected_token: str) -> None:
        with self._lock:
            cached = self._cache.get(cache_key)
            if cached is not None and cached.value == rejected_token:
                self._cache.pop(cache_key, None)

    def close(self) -> None:
        self._client.close()
