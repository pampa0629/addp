"""Meta 模块客户端"""
from typing import List, Dict, Any, Optional
from .base import BaseClient


class MetaClient(BaseClient):
    """Meta 模块 API 客户端 - 元数据扫描、查询"""

    async def get_metadata(self, object_id: int) -> Dict[str, Any]:
        """获取元数据详情"""
        return await self.get(f"/api/v1/meta/metadata/{object_id}")

    async def scan_metadata(self, object_id: int) -> Dict[str, Any]:
        """触发元数据扫描"""
        return await self.post("/api/v1/meta/scan", json={"object_id": object_id})

    async def list_metadata(self, page: int = 1, size: int = 20) -> Dict[str, Any]:
        """列出所有元数据"""
        return await self.get("/api/v1/meta/metadata", params={"page": page, "size": size})
