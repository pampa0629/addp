"""生成 Transfer 任务的受限语义补丁。

Transfer 的 endpoint、运行边界和目标策略属于 Transfer owner。模型只返回
任务名称、描述和基于已验证源字段的映射意图，不能构造 locator 或改变任务边界。
"""

from __future__ import annotations

import json
from typing import Any

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.output_parsers import PydanticOutputParser
from pydantic import BaseModel, Field


class TransferFieldMappingIntent(BaseModel):
    source: str = Field(min_length=1, description="已验证源字段名称")
    target: str = Field(min_length=1, description="目标字段名称")


class TransferGenerationOutput(BaseModel):
    name: str = Field(min_length=1, max_length=255)
    description: str = Field(default="", max_length=2000)
    mappings: list[TransferFieldMappingIntent] = Field(default_factory=list, max_length=200)


class TransferGenerationChain:
    """只生成任务元数据和字段映射，不生成 Transfer endpoint。"""

    def __init__(self, llm: Any):
        self.llm = llm
        self.output_parser = PydanticOutputParser(pydantic_object=TransferGenerationOutput)

    async def generate(
        self,
        query: str,
        *,
        source: dict[str, Any],
        target: dict[str, Any],
        current_task: dict[str, Any],
    ) -> TransferGenerationOutput:
        response = await self.llm.ainvoke([
            SystemMessage(content=(
                "你负责为 ADDP Transfer 任务生成受限的任务描述和字段映射。"
                "只能使用输入中已有的源字段名称；不得编造字段、locator、引擎、格式、运行边界、装载模式、"
                "目标策略或连接信息。不要删除未提及的已有映射。source 必须精确匹配 source.fields 中的 name；"
                "如果用户没有提出字段映射要求，返回空 mappings。name 应简短描述传输任务，description 只概括用户需求。"
                "Transfer endpoint、权限和能力由 owner 与当前向导上下文决定，不能在输出中表达。\n\n"
                + self.output_parser.get_format_instructions()
            )),
            HumanMessage(content=json.dumps({
                "query": query,
                "source": {
                    "locator": source.get("locator"),
                    "data_type": source.get("data_type"),
                    "fields": source.get("fields", [])[:200],
                    "geometry_column": source.get("geometry_column"),
                    "geometry_type": source.get("geometry_type"),
                    "crs": source.get("crs"),
                },
                "target": {
                    "parent_locator": target.get("parent_locator"),
                    "name": target.get("name"),
                    "data_type": target.get("data_type"),
                    "representation": target.get("representation"),
                    "format": target.get("format"),
                    "fields": target.get("fields", [])[:200],
                },
                "current_task": {
                    "name": current_task.get("name"),
                    "description": current_task.get("description"),
                },
            }, ensure_ascii=False)),
        ])
        content = getattr(response, "content", response)
        parsed = self.output_parser.parse(str(content))
        return _normalize_output(parsed, source)


def _normalize_output(
    output: TransferGenerationOutput,
    source: dict[str, Any],
) -> TransferGenerationOutput:
    known_fields = {
        str(field.get("name")).strip()
        for field in source.get("fields", [])
        if isinstance(field, dict) and str(field.get("name") or "").strip()
    }
    mappings: list[TransferFieldMappingIntent] = []
    seen_sources: set[str] = set()
    for mapping in output.mappings:
        source_name = mapping.source.strip()
        target_name = mapping.target.strip()
        if not source_name or not target_name or source_name not in known_fields:
            continue
        if source_name in seen_sources:
            continue
        seen_sources.add(source_name)
        mappings.append(TransferFieldMappingIntent(source=source_name, target=target_name))
    name = output.name.strip()
    return TransferGenerationOutput(
        name=name[:255],
        description=output.description.strip()[:2000],
        mappings=mappings,
    )
