"""从自然语言分析需求中提取独立的输入数据意图。"""

from __future__ import annotations

import json
from enum import StrEnum
from typing import Any

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.output_parsers import PydanticOutputParser
from pydantic import BaseModel, Field


class ResourceIntent(BaseModel):
    """一个需要独立检索并由用户确认的输入数据项。"""

    role: str = Field(min_length=1, description="数据项在本次分析中的稳定业务用途")
    search_queries: list[str] = Field(
        min_length=1,
        max_length=8,
        description="用于 data.search 的业务资源名或常用技术名",
    )


class ResourceIntentOutput(BaseModel):
    resources: list[ResourceIntent] = Field(default_factory=list, max_length=8)


class ResourceIntentScope(StrEnum):
    INPUTS = "inputs"
    TRANSFER_SOURCE = "transfer_source"


class ResourceIntentChain:
    """只理解输入数据项，不选择具体资源或构造 locator。"""

    def __init__(self, llm: Any):
        self.llm = llm
        self.output_parser = PydanticOutputParser(pydantic_object=ResourceIntentOutput)

    async def extract(
        self,
        query: str,
        *,
        scope: ResourceIntentScope = ResourceIntentScope.INPUTS,
    ) -> list[ResourceIntent]:
        transfer_source_instruction = (
            "这是 Transfer 任务描述，只提取‘从/源’一侧真正要读取的数据资源；"
            "‘到/目标’一侧的引擎、数据库、表名、写入动作和传输动作都不是输入资源，必须忽略。"
            if scope is ResourceIntentScope.TRANSFER_SOURCE else ""
        )
        response = await self.llm.ainvoke([
            SystemMessage(content=(
                "你负责识别数据分析或查询需要读取的输入数据项。"
                "把每个独立输入数据项分别列出；距离、单位、算子、统计指标和输出名称不是数据项。"
                "role 使用简短且稳定的业务用途。search_queries 首项使用用户原始业务资源名；"
                "当用户使用中文或其他自然语言描述资源时，search_queries 必须按高召回方式依次包含：原始业务词、"
                "最常见的英文单词形式、其他常用英文技术同义词或数据库命名形式，最多 8 个，"
                "以覆盖资源实际使用不同英文名称的情况；这些词只能是该业务资源的直接翻译或通用技术称呼，不得编造具体表名。"
                "不得编造字段、locator 或具体数据集。"
                + transfer_source_instruction
                + "\n\n"
                + self.output_parser.get_format_instructions()
            )),
            HumanMessage(content=query),
        ])
        content = getattr(response, "content", response)
        parsed = self.output_parser.parse(str(content))
        return _normalize_intents(parsed.resources)

    async def expand_missing(
        self,
        query: str,
        missing_intents: list[ResourceIntent],
    ) -> list[ResourceIntent]:
        """根据首轮零召回事实，为缺失角色补充尚未尝试的检索词。"""
        if not missing_intents:
            return []

        attempted = {
            intent.role: [value for value in intent.search_queries]
            for intent in missing_intents
        }
        response = await self.llm.ainvoke([
            SystemMessage(content=(
                "你负责补充数据分析或查询的数据源检索词。此前检索词均未召回候选，"
                "请仅为输入中给出的 role 生成尚未尝试的新检索词。优先补充该业务资源最常见的单词型英文名称，"
                "再补充单复数、复合词、数据库标识符形式和其他直接技术同义词；不得改变 role，"
                "不得重复 attempted_queries，不得编造字段、locator 或具体数据集，每个 role 最多 8 个新词。\n\n"
                + self.output_parser.get_format_instructions()
            )),
            HumanMessage(content=json.dumps({
                "query": query,
                "missing_resources": [
                    {
                        "role": intent.role,
                        "attempted_queries": intent.search_queries,
                    }
                    for intent in missing_intents
                ],
            }, ensure_ascii=False)),
        ])
        content = getattr(response, "content", response)
        parsed = self.output_parser.parse(str(content))
        return _new_queries_only(parsed.resources, attempted)


def _normalize_intents(intents: list[ResourceIntent]) -> list[ResourceIntent]:
    merged: dict[str, list[str]] = {}
    for intent in intents:
        role = intent.role.strip()
        if not role:
            continue
        queries = merged.setdefault(role, [])
        for query in intent.search_queries:
            normalized = query.strip()
            if normalized and normalized not in queries:
                queries.append(normalized)

    return [
        ResourceIntent(role=role, search_queries=queries[:8])
        for role, queries in merged.items()
        if queries
    ][:8]


def _new_queries_only(
    intents: list[ResourceIntent],
    attempted: dict[str, list[str]],
) -> list[ResourceIntent]:
    normalized = _normalize_intents(intents)
    expanded: list[ResourceIntent] = []
    for intent in normalized:
        if intent.role not in attempted:
            continue
        known = {value.strip().casefold() for value in attempted[intent.role]}
        queries = [
            value
            for value in intent.search_queries
            if value.casefold() not in known
        ][:8]
        if queries:
            expanded.append(ResourceIntent(role=intent.role, search_queries=queries))
    return expanded
