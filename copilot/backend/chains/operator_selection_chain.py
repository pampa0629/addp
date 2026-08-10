"""Select workflow operators from the current runtime's public catalog."""

from __future__ import annotations

import json
from typing import Any

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.output_parsers import PydanticOutputParser
from pydantic import BaseModel, Field

from services.operator_catalog import OperatorCatalogService


class OperatorListOutput(BaseModel):
    operators: list[str] = Field(default_factory=list, max_length=8)
    reasoning: str | None = None


class OperatorSelectionChain:
    """Apply one constrained LLM transformation to an owner-provided operator catalog."""

    def __init__(self, llm: Any, operator_catalog: OperatorCatalogService | None = None) -> None:
        self.llm = llm
        self.operator_catalog = operator_catalog or OperatorCatalogService()
        self.output_parser = PydanticOutputParser(pydantic_object=OperatorListOutput)

    async def select(
        self,
        query: str,
        data_source_info: str | None = None,
        workflow_engine_id: int | None = None,
        tenant_id: int = 0,
    ) -> list[str]:
        if not workflow_engine_id:
            raise ValueError("workflow_engine_id 是算子发现必需上下文")
        operators = await self.operator_catalog.list_operators(workflow_engine_id, tenant_id)
        if not operators:
            return []
        known = {str(operator.get("name")) for operator in operators if operator.get("name")}
        response = await self.llm.ainvoke([
            SystemMessage(content=(
                "你负责从当前 Workflow Runtime 的公开算子目录选择完成需求所需的最小算子集合。"
                "只能返回输入目录中存在的 name，不得构造算子。通常包含必要输入、处理和输出算子，"
                "最多 8 个；只返回指定结构。\n\n" + self.output_parser.get_format_instructions()
            )),
            HumanMessage(content=json.dumps({
                "query": query,
                "resource_facts": data_source_info or "",
                "operators": operators,
            }, ensure_ascii=False)),
        ])
        parsed = self.output_parser.parse(str(getattr(response, "content", response)))
        selected: list[str] = []
        for name in parsed.operators:
            normalized = name.strip()
            if normalized in known and normalized not in selected:
                selected.append(normalized)
        if not selected:
            raise ValueError("模型未选择任何有效工作流算子")
        return selected
