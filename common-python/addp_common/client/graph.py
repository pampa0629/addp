"""
ADDP Graph 模块客户端
提供知识图谱的知识服务 API 访问能力
"""
from typing import Optional
from .base import BaseClient


class GraphClient(BaseClient):
    """Graph 模块 HTTP Client"""

    def __init__(self, base_url: str, user_token: Optional[str] = None,
                 internal_api_key: Optional[str] = None):
        super().__init__(base_url, internal_api_key=internal_api_key, user_token=user_token)

    async def list_graphs(self) -> list:
        """列出所有知识图谱"""
        result = await self.get("/api/v1/graph/graphs")
        # 兼容分页响应和直接数组响应
        if isinstance(result, dict) and "data" in result:
            return result["data"]
        if isinstance(result, list):
            return result
        return result

    async def get_ontology(self, graph_id: int) -> dict:
        """获取图谱本体描述（实体类型 + 关系类型 + 数量统计）"""
        return await self.get(f"/api/v1/graph/kg/{graph_id}/ontology")

    async def search_entities(self, graph_id: int, q: str,
                               entity_type: str = "", page: int = 1,
                               page_size: int = 20) -> dict:
        """全文搜索实体，返回分页结果 {data, total, page, page_size, total_pages}"""
        params = {"q": q, "page": page, "page_size": page_size}
        if entity_type:
            params["type"] = entity_type
        return await self.get(f"/api/v1/graph/kg/{graph_id}/search", params=params)

    async def get_neighbors(self, graph_id: int, node_id: str, limit: int = 100) -> dict:
        """获取节点的所有直接邻居关系"""
        return await self.get(
            f"/api/v1/graph/kg/{graph_id}/nodes/{node_id}/neighbors",
            params={"limit": limit}
        )

    async def get_subgraph(self, graph_id: int, node_id: str,
                            depth: int = 2, limit: int = 50) -> dict:
        """获取节点中心子图（N 跳范围内节点和关系）"""
        return await self.post(
            f"/api/v1/graph/kg/{graph_id}/subgraph",
            json={"node_id": node_id, "depth": depth, "limit": limit}
        )
