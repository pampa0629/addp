"""
Develop API 相关的 LangChain Tools

封装与 Develop 模块的所有 API 交互
"""
import time
import httpx
from typing import List, Dict, Optional
from langchain.tools import BaseTool
from pydantic import Field

from config import settings


class EngineTool(BaseTool):
    """
    获取引擎列表 Tool

    调用 Develop API 获取租户的所有存储引擎（数据库、对象存储、计算引擎等）
    """
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

        # 构建认证头
        headers = {}
        if settings.internal_api_key:
            headers["X-Internal-API-Key"] = settings.internal_api_key

        try:
            async with httpx.AsyncClient(timeout=10.0, trust_env=False) as client:
                url = f"{settings.develop_service_url}/api/develop/engines"
                print(f"[EngineTool] 请求 URL: {url}")
                response = await client.get(url, headers=headers)
                response.raise_for_status()
                data = response.json()

                # Develop API 直接返回数组，不是 {engines: []}
                if isinstance(data, list):
                    engines = data
                else:
                    engines = data.get("engines", [])

                print(f"[EngineTool] ✅ 成功获取 {len(engines)} 个引擎")

                # 打印每个引擎的信息
                for e in engines:
                    print(f"  - ID: {e['id']}, 名称: {e['name']}, 类型: {e['engine_type']}")

                # 返回简化的引擎信息
                result = [
                    {
                        "id": e["id"],
                        "name": e["name"],
                        "type": e["engine_type"],
                        "description": e.get("description", "")
                    }
                    for e in engines
                ]

                return result

        except httpx.HTTPError as e:
            print(f"[EngineTool] ❌ HTTP 请求失败: {e}")
            return []
        except Exception as e:
            print(f"[EngineTool] ❌ 获取引擎失败: {type(e).__name__}: {e}")
            return []

    def _run(self, tenant_id: int) -> List[Dict]:
        """同步执行（不支持）"""
        raise NotImplementedError("EngineTool only supports async execution")


class SchemaTableTool(BaseTool):
    """
    获取 Schema 和 Table 列表 Tool

    调用 Develop API 获取关系数据库的 Schema 和 Table 信息
    """
    name: str = "get_schema_tables"
    description: str = """
获取指定关系数据库引擎的 Schema 和 Table 列表。
仅适用于关系数据库类型（PostgreSQL、MySQL、Doris、ClickHouse）。
使用方式：输入 engine_id（整数），可选 schema（字符串），返回 Schema/Table 列表
"""

    async def _arun(self, engine_id: int, schema: Optional[str] = None) -> Dict:
        """异步执行"""
        print(f"[SchemaTableTool] 获取引擎 {engine_id} 的 Schema/Table 列表（schema={schema}）")

        headers = {}
        if settings.internal_api_key:
            headers["X-Internal-API-Key"] = settings.internal_api_key

        try:
            result = {"schemas": [], "tables": []}

            # 1. 获取 Schema 列表
            async with httpx.AsyncClient(timeout=10.0, trust_env=False) as client:
                schema_resp = await client.get(
                    f"{settings.develop_service_url}/api/develop/engines/{engine_id}/schemas",
                    headers=headers
                )

                if schema_resp.status_code == 200:
                    schema_data = schema_resp.json()
                    result["schemas"] = schema_data.get("schemas", [])
                    print(f"[SchemaTableTool] ✅ 获取到 {len(result['schemas'])} 个 Schema")

                # 2. 获取 Table 列表（如果指定了 schema）
                target_schema = schema or "public"
                table_resp = await client.get(
                    f"{settings.develop_service_url}/api/develop/engines/{engine_id}/tables",
                    params={"schema": target_schema},
                    headers=headers
                )

                if table_resp.status_code == 200:
                    table_data = table_resp.json()
                    result["tables"] = table_data.get("tables", [])
                    print(f"[SchemaTableTool] ✅ 获取到 {len(result['tables'])} 个 Table")

            return result

        except httpx.HTTPError as e:
            print(f"[SchemaTableTool] ❌ HTTP 请求失败: {e}")
            return {"schemas": [], "tables": []}
        except Exception as e:
            print(f"[SchemaTableTool] ❌ 获取 Schema/Table 失败: {type(e).__name__}: {e}")
            return {"schemas": [], "tables": []}

    def _run(self, engine_id: int, schema: Optional[str] = None) -> Dict:
        """同步执行（不支持）"""
        raise NotImplementedError("SchemaTableTool only supports async execution")


class ObjectStorageTool(BaseTool):
    """
    获取对象存储 Bucket 和对象列表 Tool

    调用 System API 获取对象存储的 Bucket 和对象信息
    """
    name: str = "get_object_storage"
    description: str = """
获取指定对象存储引擎的 Bucket 和对象列表。
仅适用于对象存储类型（MinIO、S3、OSS）。
使用方式：输入 engine_id（整数），可选 bucket（字符串），返回 Bucket/对象列表
"""

    async def _arun(self, engine_id: int, bucket: Optional[str] = None, prefix: Optional[str] = None) -> Dict:
        """异步执行"""
        print(f"[ObjectStorageTool] 获取引擎 {engine_id} 的对象存储信息（bucket={bucket}, prefix={prefix}）")

        headers = {}
        if settings.internal_api_key:
            headers["X-Internal-API-Key"] = settings.internal_api_key

        try:
            result = {"buckets": [], "objects": []}

            async with httpx.AsyncClient(timeout=10.0, trust_env=False) as client:
                # 1. 获取 Bucket 列表
                bucket_resp = await client.get(
                    f"{settings.system_service_url}/api/engines/{engine_id}/buckets",
                    headers=headers
                )

                if bucket_resp.status_code == 200:
                    bucket_data = bucket_resp.json()
                    result["buckets"] = bucket_data.get("buckets", [])
                    print(f"[ObjectStorageTool] ✅ 获取到 {len(result['buckets'])} 个 Bucket")

                # 2. 如果指定了 bucket，获取对象列表
                if bucket:
                    params = {"bucket": bucket}
                    if prefix:
                        params["prefix"] = prefix

                    obj_resp = await client.get(
                        f"{settings.system_service_url}/api/engines/{engine_id}/objects",
                        params=params,
                        headers=headers
                    )

                    if obj_resp.status_code == 200:
                        obj_data = obj_resp.json()
                        result["objects"] = obj_data.get("objects", [])
                        print(f"[ObjectStorageTool] ✅ 获取到 {len(result['objects'])} 个对象")

            return result

        except httpx.HTTPError as e:
            print(f"[ObjectStorageTool] ❌ HTTP 请求失败: {e}")
            return {"buckets": [], "objects": []}
        except Exception as e:
            print(f"[ObjectStorageTool] ❌ 获取对象存储信息失败: {type(e).__name__}: {e}")
            return {"buckets": [], "objects": []}

    def _run(self, engine_id: int, bucket: Optional[str] = None, prefix: Optional[str] = None) -> Dict:
        """同步执行（不支持）"""
        raise NotImplementedError("ObjectStorageTool only supports async execution")


class OperatorDiscoveryTool(BaseTool):
    """
    获取算子列表 Tool（带缓存）

    调用 Develop API 获取所有算子的简要信息，使用 5 分钟 TTL 缓存
    """
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
        """
        异步执行，获取指定引擎类型的算子

        Args:
            engine_type: 工作流引擎类型（python_workflow/spark_workflow/math_workflow），默认 python_workflow
        """
        # 构建缓存键（按引擎类型缓存）
        cache_key = f"{engine_type}"

        # 检查缓存
        if self._cache and self._cache_time and hasattr(self, '_cache_engine_type'):
            if self._cache_engine_type == engine_type:
                age = time.time() - self._cache_time
                if age < self._cache_ttl:
                    print(f"[OperatorDiscoveryTool] 使用缓存（引擎: {engine_type}, 年龄: {age:.1f}秒）")
                    return self._cache

        # 缓存过期或不存在，从 API 获取
        print(f"[OperatorDiscoveryTool] 缓存过期，从 Develop API 获取（引擎: {engine_type}）")

        headers = {}
        if settings.internal_api_key:
            headers["X-Internal-API-Key"] = settings.internal_api_key

        try:
            async with httpx.AsyncClient(timeout=10.0, trust_env=False) as client:
                # 使用按模块筛选的 API
                response = await client.get(
                    f"{settings.develop_service_url}/api/develop/operators/modules/{engine_type}",
                    headers=headers
                )
                response.raise_for_status()
                data = response.json()

                operators = data.get("operators", [])
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

        except httpx.HTTPError as e:
            print(f"[OperatorDiscoveryTool] ❌ HTTP 请求失败: {e}")
            # 如果有缓存且引擎类型匹配，即使过期也返回
            if self._cache and hasattr(self, '_cache_engine_type') and self._cache_engine_type == engine_type:
                print(f"[OperatorDiscoveryTool] 使用过期缓存作为降级")
                return self._cache
            return []
        except Exception as e:
            print(f"[OperatorDiscoveryTool] ❌ 获取算子失败: {type(e).__name__}: {e}")
            if self._cache and hasattr(self, '_cache_engine_type') and self._cache_engine_type == engine_type:
                return self._cache
            return []

    def _run(self) -> List[Dict]:
        """同步执行（不支持）"""
        raise NotImplementedError("OperatorDiscoveryTool only supports async execution")


class OperatorDetailTool(BaseTool):
    """
    获取算子详情 Tool

    调用 Develop API 获取单个算子的详细信息（参数、输出、工作流示例等）
    """
    name: str = "get_operator_detail"
    description: str = """
获取指定算子的详细信息，包括参数定义、输出定义、工作流示例。
使用方式：输入算子名称（字符串），返回算子详情（JSON）
"""

    async def _arun(self, operator_name: str, engine_type: str = "python_workflow") -> Optional[Dict]:
        """
        异步执行，获取指定引擎的算子详情

        Args:
            operator_name: 算子名称
            engine_type: 工作流引擎类型（python_workflow/spark_workflow/math_workflow），默认 python_workflow
        """
        print(f"[OperatorDetailTool] 获取算子 '{operator_name}' 的详细信息（引擎: {engine_type}）")

        headers = {}
        if settings.internal_api_key:
            headers["X-Internal-API-Key"] = settings.internal_api_key

        try:
            async with httpx.AsyncClient(timeout=10.0, trust_env=False) as client:
                # 获取指定引擎模块的所有算子
                response = await client.get(
                    f"{settings.develop_service_url}/api/develop/operators/modules/{engine_type}",
                    headers=headers
                )
                response.raise_for_status()
                data = response.json()

                operators = data.get("operators", [])

                # 从中筛选出匹配名称的算子
                for op in operators:
                    if op.get("name") == operator_name:
                        print(f"[OperatorDetailTool] ✅ 成功获取算子详情")
                        return op

                print(f"[OperatorDetailTool] ⚠️ 未找到算子 '{operator_name}' (引擎: {engine_type})")
                return None

        except httpx.HTTPError as e:
            print(f"[OperatorDetailTool] ❌ HTTP 请求失败: {e}")
            return None
        except Exception as e:
            print(f"[OperatorDetailTool] ❌ 获取算子详情失败: {type(e).__name__}: {e}")
            return None

    def _run(self, operator_name: str) -> Optional[Dict]:
        """同步执行（不支持）"""
        raise NotImplementedError("OperatorDetailTool only supports async execution")
