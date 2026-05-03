"""
Develop API 相关的 LangChain Tools

封装与 Develop 模块的所有 API 交互
"""
import time
from typing import List, Dict, Optional
from langchain.tools import BaseTool

from addp_common.client import DevelopClient, SystemClient
from config import settings


class EngineTool(BaseTool):
    """获取引擎列表 Tool"""
    name: str = "get_engines"
    description: str = """
获取当前租户的所有存储引擎列表。
引擎类型包括：
- 关系数据库：PostgreSQL、MySQL、Doris、ClickHouse
- 对象存储：MinIO、S3、OSS
- 计算引擎：Python Workflow、Spark
使用方式：输入租户 ID（整数），返回引擎列表（JSON）
"""

    async def _arun(self, tenant_id: int) -> List[Dict]:
        """异步执行"""
        print(f"[EngineTool] 获取租户 {tenant_id} 的引擎列表")

        try:
            async with DevelopClient(
                base_url=settings.get_develop_url(),
                internal_api_key=settings.internal_api_key
            ) as client:
                engines = await client.list_engines()
                print(f"[EngineTool] ✅ 成功获取 {len(engines)} 个引擎")

                # 返回简化的引擎信息
                return [
                    {
                        "id": e["id"],
                        "name": e["name"],
                        "type": e["engine_type"],
                        "description": e.get("description", "")
                    }
                    for e in engines
                ]
        except Exception as e:
            print(f"[EngineTool] ❌ 获取引擎失败: {type(e).__name__}: {e}")
            return []

    def _run(self, tenant_id: int) -> List[Dict]:
        """同步执行（不支持）"""
        raise NotImplementedError("EngineTool only supports async execution")


class SchemaTableTool(BaseTool):
    """获取 catalog 命名空间和数据项列表 Tool"""
    name: str = "get_catalog_items"
    description: str = """
获取指定关系数据库引擎的 catalog 命名空间和数据项列表。
仅适用于关系数据库类型（PostgreSQL、MySQL、Doris、ClickHouse）。
使用方式：输入 engine_id（整数），可选 namespace（字符串），返回命名空间和数据项列表
"""

    async def _arun(self, engine_id: int, namespace: Optional[str] = None) -> Dict:
        """异步执行"""
        print(f"[SchemaTableTool] 获取引擎 {engine_id} 的 catalog 列表（namespace={namespace}）")

        try:
            async with DevelopClient(
                base_url=settings.get_develop_url(),
                internal_api_key=settings.internal_api_key
            ) as client:
                result = {"namespaces": [], "items": []}

                namespaces = await client.list_namespaces(engine_id)
                result["namespaces"] = namespaces
                print(f"[SchemaTableTool] ✅ 获取到 {len(namespaces)} 个命名空间")

                target_namespace = namespace or "public"
                items = await client.list_catalog_items(engine_id, target_namespace)
                result["items"] = items
                print(f"[SchemaTableTool] ✅ 获取到 {len(items)} 个数据项")

                return result
        except Exception as e:
            print(f"[SchemaTableTool] ❌ 获取 catalog 列表失败: {type(e).__name__}: {e}")
            return {"namespaces": [], "items": []}

    def _run(self, engine_id: int, namespace: Optional[str] = None) -> Dict:
        """同步执行（不支持）"""
        raise NotImplementedError("SchemaTableTool only supports async execution")


class ObjectStorageTool(BaseTool):
    """获取对象存储 Bucket 和对象列表 Tool"""
    name: str = "get_object_storage"
    description: str = """
获取指定对象存储引擎的 Bucket 和对象列表。
仅适用于对象存储类型（MinIO、S3、OSS）。
使用方式：输入 engine_id（整数），可选 bucket（字符串），返回 Bucket/对象列表
"""

    async def _arun(self, engine_id: int, bucket: Optional[str] = None, prefix: Optional[str] = None) -> Dict:
        """异步执行"""
        print(f"[ObjectStorageTool] 获取引擎 {engine_id} 的对象存储信息（bucket={bucket}, prefix={prefix}）")

        try:
            async with SystemClient(
                base_url=settings.get_system_url(),
                internal_api_key=settings.internal_api_key
            ) as client:
                result = {"buckets": [], "objects": []}

                # 获取 Bucket 列表
                buckets = await client.list_buckets(engine_id)
                result["buckets"] = buckets
                print(f"[ObjectStorageTool] ✅ 获取到 {len(buckets)} 个 Bucket")

                # 如果指定了 bucket，获取对象列表
                if bucket:
                    objects = await client.list_objects(engine_id, bucket, prefix)
                    result["objects"] = objects
                    print(f"[ObjectStorageTool] ✅ 获取到 {len(objects)} 个对象")

                return result
        except Exception as e:
            print(f"[ObjectStorageTool] ❌ 获取对象存储信息失败: {type(e).__name__}: {e}")
            return {"buckets": [], "objects": []}

    def _run(self, engine_id: int, bucket: Optional[str] = None, prefix: Optional[str] = None) -> Dict:
        """同步执行（不支持）"""
        raise NotImplementedError("ObjectStorageTool only supports async execution")


class OperatorDiscoveryTool(BaseTool):
    """获取算子列表 Tool（带缓存）"""
    name: str = "discover_operators"
    description: str = """
获取所有计算引擎提供的算子简要信息（名称、分类、简要描述）。
包含空间和非空间算子（Buffer、Clip、Union、Intersect 等）。
使用方式：无需参数，返回算子列表（JSON）
"""

    # 类级别的缓存
    _cache: Optional[List[Dict]] = None
    _cache_time: Optional[float] = None
    _cache_ttl: int = 300  # 5 分钟

    async def _arun(self, engine_type: str = "python_workflow") -> List[Dict]:
        """异步执行，获取指定引擎类型的算子"""
        # 检查缓存
        if self._cache and self._cache_time and hasattr(self, '_cache_engine_type'):
            if self._cache_engine_type == engine_type:
                age = time.time() - self._cache_time
                if age < self._cache_ttl:
                    print(f"[OperatorDiscoveryTool] 使用缓存（引擎: {engine_type}, 年龄: {age:.1f}秒）")
                    return self._cache

        # 缓存过期或不存在，从 API 获取
        print(f"[OperatorDiscoveryTool] 缓存过期，从 Develop API 获取（引擎: {engine_type}）")

        try:
            async with DevelopClient(
                base_url=settings.get_develop_url(),
                internal_api_key=settings.internal_api_key
            ) as client:
                operators = await client.list_operators(engine_type)
                print(f"[OperatorDiscoveryTool] ✅ 从 API 获取到 {len(operators)} 个算子（引擎: {engine_type}）")

                # 提取简要信息
                brief_operators = [
                    {
                        "name": op["name"],
                        "brief": op.get("brief_description", op.get("description", "")),
                        "category": op.get("category", "其他")
                    }
                    for op in operators
                ]

                # 更新缓存
                self._cache = brief_operators
                self._cache_time = time.time()
                self._cache_engine_type = engine_type

                return brief_operators
        except Exception as e:
            print(f"[OperatorDiscoveryTool] ❌ 获取算子失败: {type(e).__name__}: {e}")
            # 如果有缓存且引擎类型匹配，即使过期也返回
            if self._cache and hasattr(self, '_cache_engine_type') and self._cache_engine_type == engine_type:
                print(f"[OperatorDiscoveryTool] 使用过期缓存作为降级")
                return self._cache
            return []

    def _run(self) -> List[Dict]:
        """同步执行（不支持）"""
        raise NotImplementedError("OperatorDiscoveryTool only supports async execution")


class OperatorDetailTool(BaseTool):
    """获取算子详情 Tool"""
    name: str = "get_operator_detail"
    description: str = """
获取指定算子的详细信息，包括参数定义、输出定义、工作流示例。
使用方式：输入算子名称（字符串），返回算子详情（JSON）
"""

    async def _arun(self, operator_name: str, engine_type: str = "python_workflow") -> Optional[Dict]:
        """异步执行，获取指定引擎的算子详情"""
        print(f"[OperatorDetailTool] 获取算子 '{operator_name}' 的详细信息（引擎: {engine_type}）")

        try:
            async with DevelopClient(
                base_url=settings.get_develop_url(),
                internal_api_key=settings.internal_api_key
            ) as client:
                operator = await client.get_operator(operator_name, engine_type)
                if operator:
                    print(f"[OperatorDetailTool] ✅ 成功获取算子详情")
                    return operator
                else:
                    print(f"[OperatorDetailTool] ⚠️ 未找到算子 '{operator_name}' (引擎: {engine_type})")
                    return None
        except Exception as e:
            print(f"[OperatorDetailTool] ❌ 获取算子详情失败: {type(e).__name__}: {e}")
            return None

    def _run(self, operator_name: str) -> Optional[Dict]:
        """同步执行（不支持）"""
        raise NotImplementedError("OperatorDetailTool only supports async execution")
