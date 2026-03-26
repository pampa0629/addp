from typing import Any, Dict, Optional
import httpx
from .base_client import ADDPBaseClient


class CopilotClient(ADDPBaseClient):
    """Copilot 模块 API 客户端 - 工作流生成"""

    def __init__(self, token: str, base_url: str = "http://localhost:8087"):
        super().__init__(base_url, token)

    async def generate_workflow(
        self,
        query: str,
        tenant_id: int = 1,
        user_id: int = 1,
        engine_type: str = "python",
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
        async with httpx.AsyncClient(timeout=120.0) as client:
            response = await client.post(
                f"{self.base_url}/api/copilot/workflow/generate",
                headers=self.headers,
                json=data,
            )
            response.raise_for_status()
            return response.json()
