"""
Meta API 相关的 LangChain Tools

封装与 Meta 模块的元数据搜索交互
"""
from typing import List, Dict, Optional
from langchain.tools import BaseTool

from addp_common.client import ManagerClient, MetaClient
from config import settings


class MetadataSearchTool(BaseTool):
    """元数据搜索 Tool"""
    name: str = "search_metadata"
    description: str = """
搜索元数据信息，用于模糊匹配数据源。
可以搜索表名、字段名、文件名等元数据。
使用方式：输入查询关键词（字符串），可选类型过滤（table/file），返回匹配结果列表
"""

    async def _arun(
        self,
        query: str,
        metadata_type: Optional[str] = None,
        tenant_id: int = 1,
        limit: int = 10
    ) -> List[Dict]:
        """异步执行元数据搜索"""
        print(f"[MetadataSearchTool] 搜索元数据: query='{query}', type={metadata_type}, tenant_id={tenant_id}")

        async with ManagerClient(
            base_url=settings.get_manager_url(),
            internal_api_key=settings.internal_api_key,
            tenant_id=tenant_id,
        ) as manager_client, MetaClient(
            base_url=settings.get_meta_url(),
            internal_api_key=settings.internal_api_key,
            tenant_id=tenant_id,
        ) as meta_client:
            result = await manager_client.search(
                q=query,
                tenant_id=tenant_id,
                page=1,
                page_size=limit,
            )
            results = result.get("results")
            if not isinstance(results, list):
                raise ValueError("manager search data.results must be an array")

            verified_results = []
            for item in results:
                if not isinstance(item, dict) or not self._matches_type(item, metadata_type):
                    continue
                verified = await self._verify_locator(meta_client, item)
                if verified is not None:
                    verified_results.append(verified)

            print(f"[MetadataSearchTool] ✅ 找到 {len(verified_results)} 个已验证匹配结果")
            return verified_results

    @staticmethod
    def _matches_type(item: Dict, metadata_type: Optional[str]) -> bool:
        if not metadata_type:
            return True
        item_type = str(item.get("asset_type") or item.get("type") or "").lower()
        if metadata_type == "table":
            return item_type in {"table", "view"}
        if metadata_type == "file":
            return item_type in {"file", "object"}
        return item_type == metadata_type

    @staticmethod
    async def _verify_locator(meta_client: MetaClient, item: Dict) -> Optional[Dict]:
        locator = item.get("locator")
        engine_id = item.get("engine_id")
        if not locator or not engine_id:
            return None

        response = await meta_client.get_resource_tree_ancestors(int(engine_id), str(locator))
        ancestors = response.get("ancestors")
        if not isinstance(ancestors, list) or len(ancestors) < 2:
            return None

        parent = ancestors[-2]
        parent_locator = parent.get("locator") if isinstance(parent, dict) else None
        target_locator = response.get("target_locator")
        if not target_locator or not parent_locator:
            return None

        metadata = item.get("metadata") if isinstance(item.get("metadata"), dict) else {}
        return {
            "name": item.get("name") or item.get("file_name") or item.get("title"),
            "type": item.get("asset_type") or item.get("type"),
            "engine_id": int(engine_id),
            "engine_name": item.get("engine_name"),
            "engine_type": item.get("engine_type"),
            "schema": item.get("schema"),
            "bucket": item.get("bucket"),
            "path": item.get("path"),
            "score": item.get("score"),
            "metadata": metadata,
            "locator": str(target_locator),
            "target_parent_locator": str(parent_locator),
        }

    def _run(
        self,
        query: str,
        metadata_type: Optional[str] = None,
        tenant_id: int = 1,
        limit: int = 10
    ) -> List[Dict]:
        """同步执行（不支持）"""
        raise NotImplementedError("MetadataSearchTool only supports async execution")


# 创建全局 Tool 实例（可选）
metadata_search_tool = MetadataSearchTool()
