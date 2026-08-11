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

    async def get_resource_tree(self, engine_id: int, expand_depth: int = 2) -> Dict[str, Any]:
        """获取指定引擎的标准资源树"""
        return await self.get(
            f"/api/v1/meta/resource-tree/{engine_id}",
            params={"expand_depth": expand_depth},
        )

    async def get_resource_tree_ancestors(self, engine_id: int, locator: str) -> Dict[str, Any]:
        """按当前 Meta 事实校验 locator 并返回根到目标的祖先链"""
        response = await self.get(
            f"/api/v1/meta/resource-tree/{engine_id}/ancestors",
            params={"locator": locator},
        )
        if not isinstance(response, dict):
            raise ValueError("meta resource tree ancestors response must be an object")
        return response

    async def get_resource_tree_node(self, engine_id: int, locator: str) -> Dict[str, Any]:
        """按父 locator 返回标准资源树节点及其直接子资源。"""
        response = await self.get(
            f"/api/v1/meta/resource-tree/{engine_id}/node",
            params={"locator": locator},
        )
        if not isinstance(response, dict):
            raise ValueError("meta resource tree node response must be an object")
        return response
