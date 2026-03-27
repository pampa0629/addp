import httpx
from typing import Any, Dict, Optional


class AddpBaseClient:
    """
    ADDP 模块 HTTP Client 基类

    支持两种认证方式：
    - internal_api_key: 服务间调用（X-Internal-API-Key），agent 调用其他模块时使用
    - user_token: 用户 JWT token（Authorization: Bearer），代理用户请求时使用

    所有子类必须继承此基类，不得各自实现 HTTP 调用逻辑。
    """

    def __init__(
        self,
        base_url: str,
        internal_api_key: Optional[str] = None,
        user_token: Optional[str] = None,
        timeout: float = 30.0,
    ):
        self.base_url = base_url.rstrip("/")
        headers = {"Content-Type": "application/json"}
        if internal_api_key:
            headers["X-Internal-API-Key"] = internal_api_key
        elif user_token:
            headers["Authorization"] = f"Bearer {user_token}"

        self._client = httpx.AsyncClient(
            base_url=self.base_url,
            headers=headers,
            timeout=timeout,
        )

    async def get(self, path: str, params: Optional[Dict] = None) -> Any:
        resp = await self._client.get(path, params=params)
        resp.raise_for_status()
        return resp.json()

    async def post(self, path: str, json: Optional[Dict] = None, **kwargs) -> Any:
        resp = await self._client.post(path, json=json, **kwargs)
        resp.raise_for_status()
        return resp.json()

    async def put(self, path: str, json: Optional[Dict] = None) -> Any:
        resp = await self._client.put(path, json=json)
        resp.raise_for_status()
        return resp.json()

    async def delete(self, path: str) -> Any:
        resp = await self._client.delete(path)
        resp.raise_for_status()
        return resp.json()

    async def post_file(self, path: str, files: Dict) -> Any:
        """上传文件（不设置 Content-Type，由 httpx 自动处理 multipart）"""
        headers = {}
        if "X-Internal-API-Key" in self._client.headers:
            headers["X-Internal-API-Key"] = self._client.headers["X-Internal-API-Key"]
        elif "Authorization" in self._client.headers:
            headers["Authorization"] = self._client.headers["Authorization"]
        resp = await self._client.post(path, files=files, headers=headers)
        resp.raise_for_status()
        return resp.json()

    async def close(self):
        await self._client.aclose()

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        await self.close()


# 向后兼容别名（旧代码使用 ADDPBaseClient）
ADDPBaseClient = AddpBaseClient
