from typing import Any, Dict

from .base import BaseClient


class CopilotClient(BaseClient):
    """Copilot 模块公开 API 客户端。"""

    async def generate_workflow(
        self,
        query: str,
        workflow_engine_id: int,
        resources: list[dict[str, Any]],
    ) -> Dict[str, Any]:
        return await self.post(
            "/api/v1/copilot/workflow/generate",
            json={
                "query": query,
                "workflow_engine_id": workflow_engine_id,
                "resources": resources,
            },
        )
