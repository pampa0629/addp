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

    _language_instructions = {
        "sql": (
            "SQL 只返回一条只读 SQL 语句；使用已验证资源的原生表/模式名称和字段，"
            "参数使用 :name，不要输出连接信息、代码围栏或解释性前后缀。"
        ),
        "mql": (
            "MQL 必须是单个 JSON command object，整个 query 字符串必须能被 JSON 解析为对象。"
            "使用平台支持的命令对象，例如 {\"find\":\"Persons\",\"filter\":{},\"limit\":10}，"
            "数据库由执行目标选择，find/aggregate/count/distinct 只填写集合名称。"
            "如果提供 current_query，默认保留 current_query 已声明的主 collection；它是编辑器上下文，不是已验证资源事实。"
            "resources 为空时只能复用 current_query 中已经出现的字段，不得新增字段。"
            "禁止输出 Mongo Shell 或 JavaScript 语法（包括 db.getSiblingDB(...）、db.collection.find(...）、"
            "链式 .find/.limit、BSON 构造器和函数调用），禁止把 locator 或 connection_info 写入 query；"
            "参数只能使用 {\"$param\":\"name\"} 结构化值节点。"
        ),
        "cypher": (
            "Cypher 只返回一条只读 Cypher 语句；参数使用 $name，不要输出连接信息、代码围栏或解释性前后缀。"
        ),
    }

    async def generate(
        self,
        *,
        query: str,
        engine: dict[str, Any],
        query_language: str,
        resources: list[dict[str, Any]],
        current_query: str | None,
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
        context_payload = {
            "engine": {
                "id": engine.get("id"),
                "name": engine.get("name"),
                "engine_type": engine.get("engine_type"),
                "capabilities": engine.get("capabilities"),
            },
            "query_language": query_language,
            "resources": resources,
        }
        if current_query and current_query.strip():
            context_payload["current_query"] = current_query.strip()
        context = json.dumps(context_payload, ensure_ascii=False, default=str)
        response = await llm.ainvoke([
            SystemMessage(content=(
                self._system_prompt
                + "\n"
                + self._language_instructions.get(
                    query_language.strip().lower(),
                    "严格遵守当前引擎 capability 声明的查询语言语法。",
                )
                + '\n输出格式：{"query":"...","explanation":"...","warnings":[]}。'
            )),
            HumanMessage(content=f"当前查询上下文:\n{context}\n\n用户需求:\n{query}"),
        ])
        return self._parse_output(
            str(getattr(response, "content", response)),
            query_language=query_language,
        )

    @staticmethod
    def _parse_output(output: str, *, query_language: str | None = None) -> dict[str, Any]:
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
        if query_language and query_language.strip().lower() == "mql":
            try:
                parsed_mql = json.loads(query_text)
            except json.JSONDecodeError as error:
                raise ValueError("generated MQL must be a JSON command object") from error
            if not isinstance(parsed_mql, dict):
                raise ValueError("generated MQL must be a JSON command object")
        warnings = parsed.get("warnings")
        if not isinstance(warnings, list):
            warnings = []
        return {
            "query": query_text,
            "explanation": str(parsed.get("explanation") or "").strip(),
            "warnings": [str(item).strip() for item in warnings if str(item).strip()],
        }


query_service = QueryService()
