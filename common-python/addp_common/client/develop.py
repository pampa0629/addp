"""Develop 模块客户端"""
import asyncio
from typing import List, Dict, Any, Optional
from .base import BaseClient


class DevelopClient(BaseClient):
    """Develop 模块 API 客户端 - SQL 执行、工作流、算子"""

    async def list_engines(self) -> List[Dict[str, Any]]:
        """获取引擎列表"""
        resp = await self.get("/api/v1/develop/engines")
        return resp if isinstance(resp, list) else resp.get("engines", [])

    async def list_namespaces(self, engine_id: int) -> List[Dict[str, Any]]:
        """获取 catalog 命名空间列表"""
        resp = await self.get(f"/api/v1/develop/engines/{engine_id}/namespaces")
        return resp.get("namespaces", [])

    async def list_catalog_items(self, engine_id: int, namespace: str = "") -> List[Dict[str, Any]]:
        """获取 catalog 数据项列表"""
        resp = await self.get(
            f"/api/v1/develop/engines/{engine_id}/items",
            params={"namespace": namespace}
        )
        return resp.get("items", [])

    async def list_operators(self, engine_type: str = "python_workflow") -> List[Dict[str, Any]]:
        """获取算子列表"""
        resp = await self.get(f"/api/v1/develop/operators/modules/{engine_type}")
        return resp.get("operators", [])

    async def get_operator(self, operator_name: str, engine_type: str = "python_workflow") -> Optional[Dict[str, Any]]:
        """获取算子详情"""
        operators = await self.list_operators(engine_type)
        for op in operators:
            if op.get("name") == operator_name:
                return op
        return None

    async def execute_sql(self, sql: str, engine_id: int) -> Dict[str, Any]:
        """执行 SQL 查询"""
        return await self.post("/api/v1/develop/execute", json={
            "query": sql,
            "engine_id": engine_id,
        })

    async def run_workflow_content(
        self, workflow: Dict[str, Any], engine_id: Optional[int] = None
    ) -> Dict[str, Any]:
        """提交工作流临时执行（不保存为开发项），返回 execution_id"""
        data: Dict[str, Any] = {
            "dev_type": "workflow",
            "trigger_type": "api",
            "content": {
                "workflow_definition": workflow,
                "inputs": {},
            },
        }
        if engine_id is not None:
            data["engine_id"] = engine_id
        return await self.post("/api/v1/develop/executions", json=data)

    async def get_execution(self, execution_id: str) -> Dict[str, Any]:
        """获取执行详情（含状态和结果）"""
        return await self.get(f"/api/v1/develop/executions/{execution_id}")

    async def wait_for_execution(
        self, execution_id: str, timeout_secs: int = 120, poll_interval: float = 2.0
    ) -> Dict[str, Any]:
        """轮询执行结果，直到完成或超时"""
        terminal_statuses = {"success", "failed", "timeout", "cancelled"}
        elapsed = 0.0
        while elapsed < timeout_secs:
            result = await self.get_execution(execution_id)
            if result.get("status", "") in terminal_statuses:
                return result
            await asyncio.sleep(poll_interval)
            elapsed += poll_interval
        return {
            "execution_id": execution_id,
            "status": "timeout",
            "message": f"等待超过 {timeout_secs} 秒，执行仍未完成",
        }
