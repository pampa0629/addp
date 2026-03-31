"""
Meta API 相关的 LangChain Tools

封装与 Meta 模块的元数据搜索交互
"""
from typing import List, Dict, Optional
from langchain.tools import BaseTool

from addp_common.client import ManagerClient
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

        try:
            async with ManagerClient(
                base_url=settings.get_manager_url(),
                internal_api_key=settings.internal_api_key
            ) as client:
                result = await client.search(q=query, page=1, page_size=limit)
                results = result.get("results", [])
                print(f"[MetadataSearchTool] ✅ 找到 {len(results)} 个匹配结果")

                # 简化返回结果
                return [
                    {
                        "id": r.get("id"),
                        "name": r.get("name"),
                        "type": r.get("type"),
                        "engine_id": r.get("engine_id"),
                        "schema": r.get("schema"),
                        "table": r.get("table"),
                        "path": r.get("path"),
                        "score": r.get("score", 0),
                        "metadata": r.get("metadata", {})
                    }
                    for r in results
                ]
        except Exception as e:
            print(f"[MetadataSearchTool] ❌ 元数据搜索失败: {type(e).__name__}: {e}")
            return []

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
