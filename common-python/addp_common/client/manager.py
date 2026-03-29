"""Manager 模块客户端"""
from typing import Dict, Any, Optional
from .base import BaseClient


class ManagerClient(BaseClient):
    """Manager 模块 API 客户端 - 数据管理、预览、搜索"""

    async def get_engines(self) -> Dict[str, Any]:
        """获取存储引擎列表"""
        return await self.get("/api/v1/manager/engines")

    async def get_engine_tree(self, engine_id: int, expand_depth: int = 2) -> Dict[str, Any]:
        """获取引擎的资源树（含 schema、表、bucket 等层级）"""
        return await self.get(f"/api/v1/manager/tree/{engine_id}", params={"expand_depth": expand_depth})

    async def preview_by_locator(self, locator: str, page: int = 1, page_size: int = 20) -> Dict[str, Any]:
        """通过 ResourceLocator URI 预览数据
        locator 格式：addp://engine/{engine_id}/path/{schema}/{table}?type=table
        """
        return await self.get("/api/v1/manager/preview", params={"locator": locator, "page": page, "page_size": page_size})

    async def search(self, q: str, page: int = 1, page_size: int = 10) -> Dict[str, Any]:
        """混合检索（全文检索 + 向量语义检索）"""
        return await self.get("/api/v1/manager/search", params={"q": q, "page": page, "page_size": page_size})
