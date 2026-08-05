"""ADDP 基础 HTTP 客户端。"""
from __future__ import annotations

import httpx
from typing import TYPE_CHECKING, Any, Dict, Optional

if TYPE_CHECKING:
    from .service_token import OAuthServiceTokenSource


class BaseClient:
    """ADDP 模块 HTTP 客户端基类"""

    def __init__(
        self,
        base_url: str,
        internal_api_key: Optional[str] = None,
        user_token: Optional[str] = None,
        tenant_id: Optional[int] = None,
        service_token_source: OAuthServiceTokenSource | None = None,
        timeout: float = 30.0,
    ):
        """
        初始化客户端

        Args:
            base_url: 服务地址,如 http://localhost:8180
            internal_api_key: 仅供尚未迁移的内部 Runtime 路径使用
            user_token: 用户访问令牌 (用户请求)
            service_token_source: 模块 Service Principal 的 OAuth Token Source
            timeout: 请求超时时间(秒)
        """
        if service_token_source is not None:
            if internal_api_key or user_token:
                raise ValueError("service token authentication cannot be combined with other credentials")
            if not isinstance(tenant_id, int) or isinstance(tenant_id, bool) or tenant_id <= 0:
                raise ValueError("tenant service client requires a tenant ID")

        self.base_url = base_url.rstrip("/")
        self._service_token_source = service_token_source
        self._tenant_id = tenant_id
        headers = {"Content-Type": "application/json"}

        if internal_api_key:
            headers["X-Internal-API-Key"] = internal_api_key
            if tenant_id is not None:
                headers["X-Tenant-ID"] = str(tenant_id)
        elif user_token:
            headers["Authorization"] = f"Bearer {user_token}"

        self._client = httpx.AsyncClient(
            base_url=self.base_url,
            headers=headers,
            timeout=timeout,
            trust_env=False,
        )

    async def get(self, path: str, params: Optional[Dict] = None) -> Any:
        """GET 请求"""
        resp = await self._request("GET", path, params=params)
        resp.raise_for_status()
        return resp.json()

    async def post(self, path: str, json: Optional[Dict] = None, **kwargs) -> Any:
        """POST 请求"""
        resp = await self._request("POST", path, json=json, **kwargs)
        resp.raise_for_status()
        return resp.json()

    async def put(self, path: str, json: Optional[Dict] = None) -> Any:
        """PUT 请求"""
        resp = await self._request("PUT", path, json=json)
        resp.raise_for_status()
        return resp.json()

    async def delete(self, path: str) -> Any:
        """DELETE 请求"""
        resp = await self._request("DELETE", path)
        resp.raise_for_status()
        return resp.json()

    async def _request(self, method: str, path: str, **kwargs) -> httpx.Response:
        if self._service_token_source is None:
            return await self._client.request(method, path, **kwargs)

        for attempt in range(2):
            token = await self._service_token_source.token(self._tenant_id)
            request_kwargs = dict(kwargs)
            headers = dict(request_kwargs.pop("headers", {}) or {})
            headers["Authorization"] = f"Bearer {token}"
            response = await self._client.request(method, path, headers=headers, **request_kwargs)
            if response.status_code != 401 or attempt == 1:
                return response
            self._service_token_source.invalidate(self._tenant_id, token)
        raise RuntimeError("tenant service request did not produce a response")

    async def close(self):
        """关闭客户端连接"""
        await self._client.aclose()

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        await self.close()
