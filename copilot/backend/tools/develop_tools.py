"""
Develop API 相关的 LangChain Tools

封装与 Develop 模块的所有 API 交互
"""
import time
from typing import List, Dict, Optional
from langchain.tools import BaseTool

from addp_common.client import DevelopClient
from config import settings


class EngineTool(BaseTool):
    """获取引擎列表 Tool"""
    name: str = "get_engines"
    description: str = """
	获取当前租户的所有存储引擎列表。
	引擎类型包括：
	- 关系数据库：PostgreSQL、MySQL、Doris、ClickHouse
	- 对象存储：MinIO、S3、OSS
	- 可查询通用引擎：Spark
	不返回工作流运行时实例；工作流运行时由调用方通过 workflow_engine_id 单独选择。
	使用方式：输入租户 ID（整数），返回引擎列表（JSON）
	"""

    async def _arun(self, tenant_id: int) -> List[Dict]:
        """异步执行"""
        print(f"[EngineTool] 获取租户 {tenant_id} 的引擎列表")

        try:
            async with DevelopClient(
                base_url=settings.get_develop_url(),
                internal_api_key=settings.internal_api_key,
                tenant_id=tenant_id,
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
            raise

    def _run(self, tenant_id: int) -> List[Dict]:
        """同步执行（不支持）"""
        raise NotImplementedError("EngineTool only supports async execution")


class OperatorDiscoveryTool(BaseTool):
    """获取算子列表 Tool（带缓存）"""
    name: str = "discover_operators"
    description: str = """
	获取指定工作流运行时提供的算子简要信息（名称、分类、简要描述）。
	包含空间和非空间算子（Buffer、Clip、Union、Intersect 等）。
	使用方式：传入 workflow_engine_id（工作流引擎实例 ID），返回算子列表（JSON）
	"""

    # 类级别的缓存
    _cache: Optional[List[Dict]] = None
    _cache_time: Optional[float] = None
    _cache_ttl: int = 300  # 5 分钟

    async def _arun(self, workflow_engine_id: int, tenant_id: int = 0) -> List[Dict]:
        """异步执行，获取指定工作流引擎实例的算子"""
        if not workflow_engine_id:
            raise ValueError("workflow_engine_id 不能为空")

        # 检查缓存
        if self._cache and self._cache_time and hasattr(self, '_cache_workflow_engine_id'):
            if self._cache_workflow_engine_id == workflow_engine_id and getattr(self, '_cache_tenant_id', None) == tenant_id:
                age = time.time() - self._cache_time
                if age < self._cache_ttl:
                    print(f"[OperatorDiscoveryTool] 使用缓存（工作流引擎 ID: {workflow_engine_id}, 年龄: {age:.1f}秒）")
                    return self._cache

        # 缓存过期或不存在，从 API 获取
        print(f"[OperatorDiscoveryTool] 缓存过期，从 Develop API 获取（工作流引擎 ID: {workflow_engine_id}）")

        try:
            async with DevelopClient(
                base_url=settings.get_develop_url(),
                internal_api_key=settings.internal_api_key,
                tenant_id=tenant_id,
            ) as client:
                operators = await client.list_operators(workflow_engine_id)
                print(f"[OperatorDiscoveryTool] ✅ 从 API 获取到 {len(operators)} 个算子（工作流引擎 ID: {workflow_engine_id}）")

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
                self._cache_workflow_engine_id = workflow_engine_id
                self._cache_tenant_id = tenant_id

                return brief_operators
        except Exception as e:
            print(f"[OperatorDiscoveryTool] ❌ 获取算子失败: {type(e).__name__}: {e}")
            raise

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

    async def _arun(self, operator_name: str, workflow_engine_id: int, tenant_id: int = 0) -> Optional[Dict]:
        """异步执行，获取指定工作流引擎实例的算子详情"""
        if not workflow_engine_id:
            raise ValueError("workflow_engine_id 不能为空")

        print(f"[OperatorDetailTool] 获取算子 '{operator_name}' 的详细信息（工作流引擎 ID: {workflow_engine_id}）")

        try:
            async with DevelopClient(
                base_url=settings.get_develop_url(),
                internal_api_key=settings.internal_api_key,
                tenant_id=tenant_id,
            ) as client:
                operator = await client.get_operator(operator_name, workflow_engine_id)
                if operator:
                    print(f"[OperatorDetailTool] ✅ 成功获取算子详情")
                    return operator
                else:
                    print(f"[OperatorDetailTool] ⚠️ 未找到算子 '{operator_name}' (工作流引擎 ID: {workflow_engine_id})")
                    return None
        except Exception as e:
            print(f"[OperatorDetailTool] ❌ 获取算子详情失败: {type(e).__name__}: {e}")
            raise

    def _run(self, operator_name: str) -> Optional[Dict]:
        """同步执行（不支持）"""
        raise NotImplementedError("OperatorDetailTool only supports async execution")
