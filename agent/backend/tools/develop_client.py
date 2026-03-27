import asyncio
from typing import Any, Dict, Optional
from .base_client import ADDPBaseClient


class DevelopClient(ADDPBaseClient):
    """Develop 模块 API 客户端 - SQL 执行、工作流"""

    def __init__(self, token: str, base_url: str = "http://localhost:8000"):
        super().__init__(base_url, token)

    async def execute_sql(self, sql: str, engine_id: int) -> Dict[str, Any]:
        """执行 SQL 查询"""
        return await self.post("/api/develop/execute", json={
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
        return await self.post("/api/develop/executions", json=data)

    async def get_execution(self, execution_id: str) -> Dict[str, Any]:
        """获取执行详情（含状态和结果）"""
        return await self.get(f"/api/develop/executions/{execution_id}")

    async def wait_for_execution(
        self, execution_id: str, timeout_secs: int = 120, poll_interval: float = 2.0
    ) -> Dict[str, Any]:
        """轮询执行结果，直到完成或超时"""
        terminal_statuses = {"success", "failed", "timeout", "cancelled"}
        elapsed = 0.0
        while elapsed < timeout_secs:
            result = await self.get_execution(execution_id)
            status = result.get("status", "")
            if status in terminal_statuses:
                return result
            await asyncio.sleep(poll_interval)
            elapsed += poll_interval
        return {
            "execution_id": execution_id,
            "status": "timeout",
            "message": f"等待超过 {timeout_secs} 秒，执行仍未完成",
        }
