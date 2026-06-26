from typing import Any, Dict
from addp_common.client.base import BaseClient


class CopilotClient(BaseClient):
    """Copilot 模块 API 客户端 - 工作流生成"""

    async def generate_workflow(
        self,
        query: str,
        workflow_engine_id: int,
        tenant_id: int = 1,
        user_id: int = 1,
    ) -> Dict[str, Any]:
        """调用 Copilot 将自然语言描述转换为工作流 DAG（LLM 调用，超时 120 秒）"""
        data: Dict[str, Any] = {
            "query": query,
            "tenant_id": tenant_id,
            "user_id": user_id,
            "workflow_engine_id": workflow_engine_id,
        }
        return await self.post("/api/v1/copilot/workflow/generate", json=data)
