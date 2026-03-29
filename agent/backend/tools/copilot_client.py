from typing import Any, Dict, Optional
from addp_common.client.base import BaseClient


class CopilotClient(BaseClient):
    """Copilot 模块 API 客户端 - 工作流生成"""

    async def generate_workflow(
        self,
        query: str,
        tenant_id: int = 1,
        user_id: int = 1,
        engine_type: str = "python_workflow",
        workflow_engine_id: Optional[int] = None,
    ) -> Dict[str, Any]:
        """调用 Copilot 将自然语言描述转换为工作流 DAG（LLM 调用，超时 120 秒）"""
        data: Dict[str, Any] = {
            "query": query,
            "tenant_id": tenant_id,
            "user_id": user_id,
            "engine_type": engine_type,
        }
        if workflow_engine_id is not None:
            data["workflow_engine_id"] = workflow_engine_id
        return await self.post("/copilot/workflow/generate", json=data)
