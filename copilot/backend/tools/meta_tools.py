"""
Meta API 相关的 LangChain Tools

封装与 Meta 模块的元数据搜索交互
"""
import httpx
from typing import List, Dict, Optional
from langchain.tools import BaseTool

from config import settings


class MetadataSearchTool(BaseTool):
    """
    元数据搜索 Tool

    封装现有的 MetadataMatcherService，调用 Meta API 进行元数据搜索
    用于数据源模糊匹配、表名自动补全等场景
    """
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
        """
        异步执行元数据搜索

        Args:
            query: 搜索关键词
            metadata_type: 元数据类型过滤（table、file 等）
            tenant_id: 租户 ID
            limit: 返回结果数量限制

        Returns:
            匹配的元数据列表
        """
        print(f"[MetadataSearchTool] 搜索元数据: query='{query}', type={metadata_type}, tenant_id={tenant_id}")

        headers = {}
        if settings.internal_api_key:
            headers["X-Internal-API-Key"] = settings.internal_api_key

        try:
            async with httpx.AsyncClient(timeout=10.0, trust_env=False) as client:
                params = {
                    "query": query,
                    "limit": limit
                }
                if metadata_type:
                    params["type"] = metadata_type

                response = await client.get(
                    f"{settings.meta_service_url}/api/meta/search",
                    params=params,
                    headers=headers
                )
                response.raise_for_status()
                data = response.json()

                results = data.get("results", [])
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

        except httpx.HTTPError as e:
            print(f"[MetadataSearchTool] ❌ HTTP 请求失败: {e}")
            return []
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
