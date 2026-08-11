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

    async def generate_query(
        self,
        query: str,
        engine_id: int,
        query_language: str,
        resources: list[dict[str, Any]],
        engine_context: dict[str, Any],
        current_query: str | None = None,
    ) -> Dict[str, Any]:
        payload: dict[str, Any] = {
            "query": query,
            "engine_id": engine_id,
            "query_language": query_language,
            "resources": resources,
            "engine_context": engine_context,
        }
        if current_query is not None:
            payload["current_query"] = current_query
        return await self.post(
            "/api/v1/copilot/query/generate",
            json=payload,
        )

    async def generate_notebook(
        self,
        query: str,
        kernel: str = "python3",
        candidates: list[dict[str, Any]] | None = None,
        resources: list[dict[str, Any]] | None = None,
    ) -> Dict[str, Any]:
        """理解 Notebook 数据源或基于 Session 已验证事实生成 Python 单元。"""
        return await self.post(
            "/api/v1/copilot/notebook/generate",
            json={
                "query": query,
                "kernel": kernel,
                "candidates": candidates or [],
                "resources": resources or [],
            },
        )

    async def generate_transfer(
        self,
        query: str,
        *,
        resources: list[dict[str, Any]] | None = None,
        task: dict[str, Any] | None = None,
        source_engine_id: int | None = None,
    ) -> Dict[str, Any]:
        """发现 Transfer 单一源资源或生成不带副作用的任务草稿。"""
        payload: dict[str, Any] = {
            "query": query,
            "resources": resources or [],
        }
        if source_engine_id is not None:
            payload["source_engine_id"] = source_engine_id
        if task is not None:
            payload["task"] = task
        return await self.post("/api/v1/copilot/transfer/generate", json=payload)
