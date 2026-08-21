"""Manager 模块客户端"""
from typing import Dict, Any, Optional
from .base import BaseClient


class ManagerClient(BaseClient):
    """Manager 模块 API 客户端 - 数据管理、预览、搜索"""

    async def get_engines(self) -> Dict[str, Any]:
        """获取存储引擎列表"""
        return await self.get("/api/v1/manager/engines")

    async def preview_by_locator(self, locator: str, page: int = 1, page_size: int = 20) -> Dict[str, Any]:
        """通过 ResourceLocator URI 预览数据
        locator 格式：addp://engine/{engine_id}/path/{schema}/{table}?type=table&item_id={item_id}
        """
        return await self.get("/api/v1/manager/preview", params={"locator": locator, "page": page, "page_size": page_size})

    async def get_resource_facts(self, locator: str) -> Dict[str, Any]:
        """按 ResourceLocator 获取不含原始数据行的资源与 Schema 事实。"""
        response = await self.get("/api/v1/manager/resource-facts", params={"locator": locator})
        if not isinstance(response, dict):
            raise ValueError("manager resource facts response must be an object")
        return response

    async def search(
        self,
        q: str,
        engine_id: Optional[int] = None,
        tenant_id: Optional[int] = None,
        page: int = 1,
        page_size: int = 10,
    ) -> Dict[str, Any]:
        """混合检索（全文检索 + 向量语义检索）"""
        params = {
            "q": q,
            "page": page,
            "page_size": page_size,
        }
        if tenant_id is not None:
            params["tenant_id"] = tenant_id
        if engine_id is not None:
            params["engine_id"] = engine_id
        response = await self.get("/api/v1/manager/search", params=params)
        data = response.get("data") if isinstance(response, dict) else None
        if not isinstance(data, dict):
            raise ValueError("manager search response must contain data object")
        return data
