from typing import Any, Dict
from .base_client import ADDPBaseClient


class ManagerClient(ADDPBaseClient):
    """Manager 模块 API 客户端 - 数据管理、目录、预览"""

    def __init__(self, token: str, base_url: str = "http://localhost:8000"):
        super().__init__(base_url, token)

    async def get_engine_tree(self, engine_id: int, expand_depth: int = 2) -> Dict[str, Any]:
        """获取引擎的资源树（含 schema、表、bucket 等层级）"""
        params = {"expand_depth": expand_depth}
        return await self.get(f"/api/manager/tree/{engine_id}", params=params)

    async def preview_by_locator(self, locator: str, page: int = 1, page_size: int = 20) -> Dict[str, Any]:
        """通过 ResourceLocator URI 预览数据
        locator 格式：addp://engine/{engine_id}/path/{schema}/{table}?type=table
        """
        params = {"locator": locator, "page": page, "page_size": page_size}
        return await self.get("/api/manager/preview", params=params)

    async def get_engines(self) -> Dict[str, Any]:
        """获取存储引擎列表"""
        return await self.get("/api/manager/engines")
