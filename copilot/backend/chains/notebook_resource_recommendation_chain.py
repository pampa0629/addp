"""对 Notebook Session 已授权的 Catalog 候选进行语义排序。"""

from __future__ import annotations

import json
from typing import Any

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.output_parsers import PydanticOutputParser
from pydantic import BaseModel, Field


MAX_RANKING_CANDIDATES_PER_ROLE = 32
DERIVED_RESOURCE_MARKERS = (
    "buffer", "area", "sum", "avg", "mean", "stats", "occupied",
    "result", "output", "analysis", "calculation",
)


class NotebookRoleRecommendation(BaseModel):
    role: str = Field(min_length=1)
    ranked_candidate_ids: list[str] = Field(default_factory=list, max_length=40)
    recommended_candidate_id: str | None = None
    recommendation_reason: str | None = Field(default=None, max_length=240)


class NotebookRecommendationOutput(BaseModel):
    recommendations: list[NotebookRoleRecommendation] = Field(default_factory=list, max_length=8)


class NotebookResourceRecommendationChain:
    """只排序 Develop 提供的候选，不构造资源路径。"""

    def __init__(self, llm: Any):
        self.llm = llm
        self.output_parser = PydanticOutputParser(pydantic_object=NotebookRecommendationOutput)

    async def recommend(self, query: str, candidates: list[dict[str, Any]]) -> dict[str, NotebookRoleRecommendation]:
        grouped: dict[str, list[dict[str, Any]]] = {}
        for candidate in candidates:
            role = str(candidate.get("role") or "").strip()
            candidate_id = str(candidate.get("candidate_id") or "").strip()
            if role and candidate_id:
                grouped.setdefault(role, []).append({
                    "candidate_id": candidate_id,
                    "name": candidate.get("name"),
                    "engine_name": candidate.get("engine_name"),
                    "engine_type": candidate.get("engine_type"),
                    "path_names": candidate.get("path_names", []),
                    "term": candidate.get("term"),
                    "kind": candidate.get("kind"),
                })
        if not grouped:
            return {}

        # 候选全集仍由调用方返回给用户；只将每个角色的有限短名单交给模型，
        # 避免大量同名/同类资源耗尽推理上下文。短名单保持 Catalog 粗筛后的稳定顺序。
        ranking_grouped = {
            role: items[:MAX_RANKING_CANDIDATES_PER_ROLE]
            for role, items in grouped.items()
        }

        response = await self.llm.ainvoke([
            SystemMessage(content=(
                "你负责对当前 Notebook Session 已授权的数据源候选进行业务语义排序。"
                "只能使用输入中已有的 candidate_id，不得构造、改写或删除候选。"
                "每个 role 的 ranked_candidate_ids 只返回最相关的前 8 个候选即可，不要重复列出其余候选；"
                "服务端会把未列出的已授权候选补回列表，用户仍可二次选择。"
                "推荐用于分析的原始输入数据，不要推荐名称明显表示缓冲区、面积、汇总、统计、结果或输出的派生资源。"
                "只有名称、原生路径、引擎类型足以判断时才给出 recommended_candidate_id，否则留空；"
                "推荐理由必须说明候选为何更像原始输入数据，并且只能使用候选中已有事实。"
                "推荐只是提示，最终由用户确认。\n\n" + self.output_parser.get_format_instructions()
            )),
            HumanMessage(content=json.dumps({"query": query, "candidates": ranking_grouped}, ensure_ascii=False)),
        ])
        parsed = self.output_parser.parse(str(getattr(response, "content", response)))
        known_values = ranking_grouped
        known = {role: [item["candidate_id"] for item in items] for role, items in known_values.items()}
        result: dict[str, NotebookRoleRecommendation] = {}
        for item in parsed.recommendations:
            role = item.role.strip()
            if role not in known or role in result:
                continue
            ranked = []
            for candidate_id in item.ranked_candidate_ids:
                normalized = candidate_id.strip()
                if normalized in known[role] and normalized not in ranked:
                    ranked.append(normalized)
            ranked.extend(candidate_id for candidate_id in known[role] if candidate_id not in ranked)
            recommended = (
                item.recommended_candidate_id.strip()
                if item.recommended_candidate_id and item.recommended_candidate_id.strip() in known[role]
                else None
            )
            fallback = _fallback_recommendation(known_values[role], ranked, recommended)
            if fallback is not None:
                recommended, fallback_reason = fallback
            else:
                fallback_reason = None
            result[role] = NotebookRoleRecommendation(
                role=role,
                ranked_candidate_ids=ranked,
                recommended_candidate_id=recommended,
                recommendation_reason=(
                    fallback_reason
                    or (item.recommendation_reason.strip() if recommended and item.recommendation_reason else None)
                ),
            )
        for role, candidate_ids in known.items():
            if role in result:
                continue
            fallback = _fallback_recommendation(known_values[role], candidate_ids, None)
            result[role] = NotebookRoleRecommendation(
                role=role,
                ranked_candidate_ids=list(candidate_ids),
                recommended_candidate_id=fallback[0] if fallback else None,
                recommendation_reason=fallback[1] if fallback else None,
            )
        return result


def _fallback_recommendation(
    candidates: list[dict[str, Any]],
    ranked_ids: list[str],
    llm_recommended_id: str | None,
) -> tuple[str, str] | None:
    """当模型漏选或选中明显派生结果时，选择名称最接近原始资源的已授权候选。"""
    by_id = {str(candidate.get("candidate_id")): candidate for candidate in candidates}
    if llm_recommended_id and not _looks_derived(by_id.get(llm_recommended_id, {})):
        return None
    ordered = [by_id[candidate_id] for candidate_id in ranked_ids if candidate_id in by_id]
    ordered.extend(candidate for candidate in candidates if candidate not in ordered)
    base_candidates = [candidate for candidate in ordered if not _looks_derived(candidate)]
    if not base_candidates:
        return None
    best = min(
        base_candidates,
        key=lambda candidate: len(_normalize_name(candidate.get("name"))),
    )
    if llm_recommended_id == best.get("candidate_id"):
        return None
    return (
        str(best["candidate_id"]),
        "候选名称更接近原始输入数据，未显示缓冲区、面积或统计结果等派生标记",
    )


def _looks_derived(candidate: dict[str, Any]) -> bool:
    name = _normalize_name(candidate.get("name"))
    return any(marker in name for marker in DERIVED_RESOURCE_MARKERS)


def _normalize_name(value: Any) -> str:
    return "".join(char.lower() for char in str(value or "") if char.isalnum())
