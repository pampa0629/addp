from typing import Any, Dict, Optional
from .base_client import ADDPBaseClient


class MetaClient(ADDPBaseClient):
    """Meta 模块 API 客户端 - 元数据扫描、查询"""

    def __init__(self, token: str, base_url: str = "http://localhost:8000"):
        super().__init__(base_url, token)

    async def scan_metadata(self, object_id: int) -> Dict[str, Any]:
        """触发元数据扫描"""
        return await self.post("/api/meta/scan", data={"object_id": object_id})

    async def get_metadata(self, object_id: int) -> Dict[str, Any]:
        """获取元数据信息"""
        return await self.get(f"/api/meta/metadata/{object_id}")

    async def search_metadata(self, keyword: str, limit: int = 10) -> Dict[str, Any]:
        """搜索元数据"""
        return await self.get("/api/meta/search", params={"q": keyword, "limit": limit})

    async def list_metadata(self, page: int = 1, size: int = 20) -> Dict[str, Any]:
        """列出所有元数据"""
        return await self.get("/api/meta/metadata", params={"page": page, "size": size})
