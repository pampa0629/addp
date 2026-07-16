"""
ADDP 基础 HTTP 客户端

支持两种认证方式:
- internal_api_key: 服务间调用 (X-Internal-API-Key)
- user_token: 用户访问令牌 (Authorization: Bearer)
"""
import httpx
from typing import Any, Dict, Optional


class BaseClient:
    """ADDP 模块 HTTP 客户端基类"""

    def __init__(
        self,
        base_url: str,
        internal_api_key: Optional[str] = None,
        user_token: Optional[str] = None,
        tenant_id: Optional[int] = None,
        timeout: float = 30.0,
    ):
        """
        初始化客户端

        Args:
            base_url: 服务地址,如 http://localhost:8180
            internal_api_key: 内部 API Key (服务间调用)
            user_token: 用户访问令牌 (用户请求)
            timeout: 请求超时时间(秒)
        """
        self.base_url = base_url.rstrip("/")
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
        resp = await self._client.get(path, params=params)
        resp.raise_for_status()
        return resp.json()

    async def post(self, path: str, json: Optional[Dict] = None, **kwargs) -> Any:
        """POST 请求"""
        resp = await self._client.post(path, json=json, **kwargs)
        resp.raise_for_status()
        return resp.json()

    async def put(self, path: str, json: Optional[Dict] = None) -> Any:
        """PUT 请求"""
        resp = await self._client.put(path, json=json)
        resp.raise_for_status()
        return resp.json()

    async def delete(self, path: str) -> Any:
        """DELETE 请求"""
        resp = await self._client.delete(path)
        resp.raise_for_status()
        return resp.json()

    async def close(self):
        """关闭客户端连接"""
        await self._client.aclose()

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        await self.close()
