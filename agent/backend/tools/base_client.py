import httpx
from typing import Any, Dict, Optional


class ADDPBaseClient:
    """ADDP 模块 API 基础客户端"""

    def __init__(self, base_url: str, token: str):
        self.base_url = base_url.rstrip("/")
        self.headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        }

    async def get(self, path: str, params: Optional[Dict] = None) -> Dict[str, Any]:
        async with httpx.AsyncClient(timeout=30.0) as client:
            response = await client.get(
                f"{self.base_url}{path}",
                headers=self.headers,
                params=params,
            )
            response.raise_for_status()
            return response.json()

    async def post(self, path: str, data: Optional[Dict] = None) -> Dict[str, Any]:
        async with httpx.AsyncClient(timeout=30.0) as client:
            response = await client.post(
                f"{self.base_url}{path}",
                headers=self.headers,
                json=data,
            )
            response.raise_for_status()
            return response.json()

    async def post_file(self, path: str, files: Dict) -> Dict[str, Any]:
        headers = {"Authorization": self.headers["Authorization"]}
        async with httpx.AsyncClient(timeout=60.0) as client:
            response = await client.post(
                f"{self.base_url}{path}",
                headers=headers,
                files=files,
            )
            response.raise_for_status()
            return response.json()

    async def delete(self, path: str) -> Dict[str, Any]:
        async with httpx.AsyncClient(timeout=30.0) as client:
            response = await client.delete(
                f"{self.base_url}{path}",
                headers=self.headers,
            )
            response.raise_for_status()
            return response.json()
