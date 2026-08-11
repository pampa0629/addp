"""用大模型对已验证的分析资源候选排序并标记推荐项。"""

from __future__ import annotations

import json
from typing import Any

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.output_parsers import PydanticOutputParser
from pydantic import BaseModel, Field


class ResourceRecommendation(BaseModel):
    role: str = Field(min_length=1, description="候选资源对应的业务输入角色")
    ranked_locators: list[str] = Field(
        default_factory=list,
        max_length=20,
        description="按业务相关度排序的候选 locator",
    )
    recommended_locator: str | None = Field(
        default=None,
        description="最推荐的候选 locator；无法明确推荐时为空",
    )
    recommendation_reason: str | None = Field(
        default=None,
        max_length=240,
        description="推荐理由；只说明已验证的名称、路径、引擎、字段或空间事实",
    )


class ResourceRecommendationOutput(BaseModel):
    recommendations: list[ResourceRecommendation] = Field(default_factory=list, max_length=8)


class ResourceRecommendationChain:
    """只排序和推荐已验证候选，不构造 locator，也不删除候选。"""

    def __init__(self, llm: Any):
        self.llm = llm
        self.output_parser = PydanticOutputParser(pydantic_object=ResourceRecommendationOutput)

    async def recommend(
        self,
        candidates: list[dict[str, Any]],
        *,
        query: str | None = None,
        search_queries: list[str] | None = None,
    ) -> dict[str, ResourceRecommendation]:
        grouped: dict[str, list[dict[str, Any]]] = {}
        for candidate in candidates:
            role = str(candidate.get("role") or "").strip()
            locator = str(candidate.get("locator") or "").strip()
            if role and locator:
                grouped.setdefault(role, []).append({
                    "locator": locator,
                    "name": candidate.get("name"),
                    "full_name": candidate.get("full_name"),
                    "path": candidate.get("path"),
                    "asset_type": candidate.get("asset_type"),
                    "engine_id": candidate.get("engine_id"),
                    "engine_name": candidate.get("engine_name"),
                    "score": candidate.get("score"),
                    "fields": candidate.get("fields", [])[:30],
                    "geometry_type": candidate.get("geometry_type"),
                    "crs": candidate.get("crs"),
                })

        if not grouped:
            return {}

        response = await self.llm.ainvoke([
            SystemMessage(content=(
                "你负责为数据分析或查询的已验证输入数据源候选排序和标记推荐项。"
                "必须先理解用户需求，再用候选中的名称、路径、引擎和字段事实比较业务相关度；"
                "role 和 search_queries 只是检索线索，不是资源字段事实。"
                "你只能排序和推荐候选中已有的 locator，绝对不能改写、拼接或编造 locator，"
                "也不能删除候选。每个 role 的 ranked_locators 应包含全部候选；"
                "只有名称、路径、引擎、字段或空间事实足以支持时才设置 recommended_locator，"
                "应优先推荐已验证字段能同时覆盖角色主体、关联关系和结果值的候选。"
                "不得仅凭 object 或 array 容器字段推断未列出的嵌套字段，也不得在字段证据不足时推荐；"
                "否则留空。recommendation_reason 必须简短且只能引用输入中的事实。\n\n"
                + self.output_parser.get_format_instructions()
            )),
            HumanMessage(content=json.dumps({
                "user_query": query,
                "search_queries": search_queries or [],
                "candidates": grouped,
            }, ensure_ascii=False)),
        ])
        content = getattr(response, "content", response)
        parsed = self.output_parser.parse(str(content))
        known = {
            role: [value["locator"] for value in values]
            for role, values in grouped.items()
        }
        recommendations: dict[str, ResourceRecommendation] = {}
        for item in parsed.recommendations:
            role = item.role.strip()
            if role not in known or role in recommendations:
                continue
            known_set = set(known[role])
            ranked: list[str] = []
            for locator in item.ranked_locators:
                normalized = locator.strip()
                if normalized in known_set and normalized not in ranked:
                    ranked.append(normalized)
            ranked.extend(locator for locator in known[role] if locator not in ranked)
            recommended_locator = (
                item.recommended_locator.strip()
                if item.recommended_locator and item.recommended_locator.strip() in known_set
                else None
            )
            recommendations[role] = ResourceRecommendation(
                role=role,
                ranked_locators=ranked,
                recommended_locator=recommended_locator,
                recommendation_reason=(
                    item.recommendation_reason.strip()
                    if recommended_locator and item.recommendation_reason
                    else None
                ),
            )
        return recommendations
