"""Develop Public Operator Spec access for workflow generation."""

from __future__ import annotations

import time
from typing import Any

from addp_common.client import DevelopClient

from config import settings
from services.inference_service import CopilotInferenceService


class OperatorCatalogService:
    """Read operator facts through the shared DevelopClient and tenant service identity."""

    _cache_ttl = 300.0

    def __init__(self) -> None:
        self._cache: dict[tuple[int, int], tuple[float, list[dict[str, Any]]]] = {}

    async def list_operators(self, workflow_engine_id: int, tenant_id: int) -> list[dict[str, Any]]:
        if workflow_engine_id <= 0:
            raise ValueError("workflow_engine_id 不能为空")
        key = (tenant_id, workflow_engine_id)
        cached = self._cache.get(key)
        if cached and time.monotonic() - cached[0] < self._cache_ttl:
            return cached[1]
        async with DevelopClient(
            base_url=settings.get_develop_url(),
            tenant_id=tenant_id,
            service_token_source=CopilotInferenceService.token_source(),
        ) as client:
            operators = await client.list_operators(workflow_engine_id)
        values = [
            {
                "name": operator["name"],
                "brief": operator.get("brief_description", operator.get("description", "")),
                "category": operator.get("category", "其他"),
            }
            for operator in operators
        ]
        self._cache[key] = (time.monotonic(), values)
        return values

    async def get_operator(
        self,
        operator_name: str,
        workflow_engine_id: int,
        tenant_id: int,
    ) -> dict[str, Any] | None:
        if workflow_engine_id <= 0:
            raise ValueError("workflow_engine_id 不能为空")
        async with DevelopClient(
            base_url=settings.get_develop_url(),
            tenant_id=tenant_id,
            service_token_source=CopilotInferenceService.token_source(),
        ) as client:
            return await client.get_operator(operator_name, workflow_engine_id)
