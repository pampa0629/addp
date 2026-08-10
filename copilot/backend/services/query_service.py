"""查询语言生成服务：基于当前引擎 capability 和已验证资源事实生成候选查询。"""

from __future__ import annotations

import json
import re
from typing import Any

from langchain_core.messages import HumanMessage, SystemMessage
from sqlalchemy.orm import Session

from services.inference_service import CopilotInferenceService


class QueryService:
    """根据当前 Query Engine 生成候选查询文本，不执行查询。"""

    _system_prompt = (
        "你是 ADDP 查询工作台的查询生成助手。根据当前查询引擎、真实 capability、查询语言和已验证资源事实，"
        "生成一个只读候选查询。不得编造表、集合、字段、locator、连接信息、几何列或 CRS；不得把资源 locator 写进查询；"
        "不得假定 PostgreSQL、public、geom、geometry 或任何固定空间函数。只返回结构化 JSON。"
    )

    async def generate(
        self,
        *,
        query: str,
        engine: dict[str, Any],
        query_language: str,
        resources: list[dict[str, Any]],
        tenant_id: int,
        db: Session,
    ) -> dict[str, Any]:
        llm = CopilotInferenceService.chat_model(
            db,
            tenant_id=tenant_id,
            scenario_code="query_generation",
            temperature=0.2,
            max_output_tokens=2500,
        )
        context = json.dumps({
            "engine": {
                "id": engine.get("id"),
                "name": engine.get("name"),
                "engine_type": engine.get("engine_type"),
                "capabilities": engine.get("capabilities"),
            },
            "query_language": query_language,
            "resources": resources,
        }, ensure_ascii=False, default=str)
        response = await llm.ainvoke([
            SystemMessage(content=(
                self._system_prompt
                + '\n输出格式：{"query":"...","explanation":"...","warnings":[]}。'
            )),
            HumanMessage(content=f"当前查询上下文:\n{context}\n\n用户需求:\n{query}"),
        ])
        return self._parse_output(str(getattr(response, "content", response)))

    @staticmethod
    def _parse_output(output: str) -> dict[str, Any]:
        cleaned = output.strip()
        match = re.search(r"```(?:json)?\s*(.*?)\s*```", cleaned, re.DOTALL | re.IGNORECASE)
        if match:
            cleaned = match.group(1).strip()
        parsed = json.loads(cleaned)
        if not isinstance(parsed, dict):
            raise ValueError("query generation response must be an object")
        query_text = parsed.get("query")
        if not isinstance(query_text, str) or not query_text.strip():
            raise ValueError("query generation response must contain a non-empty query")
        query_text = query_text.strip()
        if "addp://" in query_text or re.search(r"\b(connection_info|engine_id)\s*[:=]", query_text, re.IGNORECASE):
            raise ValueError("generated query contains internal resource facts")
        warnings = parsed.get("warnings")
        if not isinstance(warnings, list):
            warnings = []
        return {
            "query": query_text,
            "explanation": str(parsed.get("explanation") or "").strip(),
            "warnings": [str(item).strip() for item in warnings if str(item).strip()],
        }


query_service = QueryService()
