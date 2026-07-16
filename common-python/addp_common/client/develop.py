"""Develop 模块客户端"""
import asyncio
from typing import List, Dict, Any, Optional
from .base import BaseClient


class DevelopClient(BaseClient):
    """Develop 模块 API 客户端 - SQL 执行、工作流、算子"""

    async def list_engines(self) -> List[Dict[str, Any]]:
        """获取引擎列表"""
        resp = await self.get("/api/v1/develop/engines")
        if not isinstance(resp, list):
            raise ValueError("develop engines response must be a list")
        return resp

    async def list_workflow_engines(self) -> List[Dict[str, Any]]:
        """获取可用于工作流编排的引擎实例列表"""
        resp = await self.get("/api/v1/develop/workflow-engines")
        if not isinstance(resp, list):
            raise ValueError("develop workflow engines response must be a list")
        return resp

    async def list_operators(self, workflow_engine_id: int) -> List[Dict[str, Any]]:
        """获取指定工作流引擎实例的算子列表"""
        resp = await self.get(f"/api/v1/develop/workflow-engines/{workflow_engine_id}/operators")
        return resp.get("operators", [])

    async def get_operator(self, operator_name: str, workflow_engine_id: int) -> Optional[Dict[str, Any]]:
        """获取指定工作流引擎实例的算子详情"""
        operators = await self.list_operators(workflow_engine_id)
        for op in operators:
            if op.get("name") == operator_name:
                return op
        return None

    async def validate_workflow(
        self,
        workflow: Dict[str, Any],
        workflow_engine_id: int,
    ) -> Dict[str, Any]:
        """按目标运行时的 Public Operator Spec 校验候选工作流。"""
        return await self.post(
            "/api/v1/develop/workflow-validations",
            json={
                "workflow_engine_id": workflow_engine_id,
                "workflow_definition": workflow,
            },
        )

    async def execute_sql(self, sql: str, engine_id: int) -> Dict[str, Any]:
        """执行 SQL 查询"""
        return await self.post("/api/v1/develop/execute", json={
            "content": {
                "query_type": "sql",
                "query": sql,
            },
            "execution_config": {
                "engine_id": engine_id,
            },
        })

    async def run_workflow_content(
        self,
        workflow: Dict[str, Any],
        engine_id: int,
        engine_specific: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        """提交工作流临时执行（不保存为开发项），返回 execution_id"""
        execution_config: Dict[str, Any] = {
            "engine_id": engine_id,
        }
        if engine_specific:
            execution_config["engine_specific"] = engine_specific

        data: Dict[str, Any] = {
            "dev_type": "workflow",
            "trigger_type": "api",
            "content": {
                "workflow_definition": workflow,
                "inputs": {},
            },
            "execution_config": execution_config,
        }
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
