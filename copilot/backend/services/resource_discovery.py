"""Copilot 的通用资源候选发现。

只通过 common-python ToolExecutor 调用 owner Tool；本模块不直接拼接模块 URL，
也不把未经 Meta/Manager 校验的搜索结果交给生成流水线。
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any
from uuid import uuid4

from addp_common.tools import ToolExecutionError, ToolExecutor, preview_resource_fact
from chains.resource_intent_chain import ResourceIntent
from addp_common.resources import ResourceFact


@dataclass(frozen=True)
class ResourceDiscoveryResult:
    candidates: list[dict[str, Any]]
    missing_roles: list[str]


class ResourceDiscovery:
    """按用户自然语言搜索并校验可作为分析输入的资源。"""

    def __init__(
        self,
        gateway_url: str,
        source_token: str,
        *,
        executor: ToolExecutor | None = None,
        recommender: Any = None,
    ):
        self.executor = executor or ToolExecutor(gateway_url, source_token)
        self.agent_run_id = f"copilot-resource-discovery-{uuid4()}"
        self.recommender = recommender

    async def discover(
        self,
        intents: list[ResourceIntent],
        *,
        query: str | None = None,
        engine_id: int | None = None,
        limit: int = 20,
        allowed_data_types: set[str] | frozenset[str] | None = None,
    ) -> ResourceDiscoveryResult:
        candidates: list[dict[str, Any]] = []
        missing_roles: list[str] = []
        verified: dict[str, tuple[dict[str, Any], dict[str, Any]] | None] = {}

        for intent in intents:
            role_candidates: list[dict[str, Any]] = []
            seen_for_role: set[str] = set()
            verification_errors: list[ToolExecutionError] = []
            eligible_hits = 0

            for search_query in intent.search_queries:
                search_arguments: dict[str, Any] = {"query": search_query, "limit": limit}
                if engine_id is not None:
                    search_arguments["engine_id"] = engine_id
                search = await self.executor.call(
                    "data.search",
                    search_arguments,
                    agent_run_id=self.agent_run_id,
                    tool_call_id=f"data-search-{uuid4()}",
                )
                hits = search.get("results") if isinstance(search, dict) else None
                if not isinstance(hits, list):
                    continue

                for hit in hits[:limit]:
                    if not isinstance(hit, dict):
                        continue
                    locator = str(hit.get("locator") or "").strip()
                    hit_engine_id = hit.get("engine_id")
                    if not locator or not isinstance(hit_engine_id, int) or hit_engine_id <= 0 or locator in seen_for_role:
                        continue
                    if search_arguments.get("engine_id") is not None and hit_engine_id != search_arguments["engine_id"]:
                        continue
                    asset_type = str(hit.get("asset_type") or hit.get("document_type") or "").lower()
                    eligible_hits += 1

                    if locator not in verified:
                        try:
                            ancestors = await self.executor.call(
                                "resource.ancestors.get",
                                {"engine_id": hit_engine_id, "locator": locator},
                                agent_run_id=self.agent_run_id,
                                tool_call_id=f"resource-ancestors-{uuid4()}",
                            )
                            preview = await self.executor.call(
                                "data.preview",
                                {"locator": locator, "limit": 5},
                                agent_run_id=self.agent_run_id,
                                tool_call_id=f"data-preview-{uuid4()}",
                            )
                        except ToolExecutionError as error:
                            verification_errors.append(error)
                            verified[locator] = None
                            continue
                        verified[locator] = (ancestors, preview)

                    verification = verified[locator]
                    if verification is None:
                        continue
                    ancestors, preview = verification
                    if not _ancestors_confirm_locator(ancestors, locator):
                        continue
                    fact = _resource_fact(preview, locator)
                    if not fact:
                        verification_errors.append(ToolExecutionError(
                            "invalid_owner_response",
                            "数据预览未返回规范化资源事实",
                        ))
                        continue
                    if allowed_data_types and fact["data_type"] not in allowed_data_types:
                        continue
                    seen_for_role.add(locator)
                    candidate = {
                        "role": intent.role,
                        "name": hit.get("name") or hit.get("full_name") or locator,
                        "locator": locator,
                        "engine_id": hit_engine_id,
                        "engine_name": hit.get("engine_name"),
                        "asset_type": asset_type,
                        "full_name": hit.get("full_name"),
                        "path": hit.get("path"),
                        "score": hit.get("score"),
                        "ancestors": _ancestor_summary(ancestors),
                        **fact,
                    }
                    representation = _hit_attribute(hit, "representation")
                    if representation:
                        candidate["representation"] = representation
                    format_value = _hit_attribute(hit, "format")
                    if format_value:
                        candidate["format"] = format_value
                    role_candidates.append(candidate)
                    if len(role_candidates) >= limit:
                        break

                if len(role_candidates) >= limit:
                    break

            if not role_candidates:
                if eligible_hits and verification_errors:
                    raise verification_errors[-1]
                missing_roles.append(intent.role)
            await self._rank_role_candidates(
                intent.role,
                role_candidates,
                query=query,
                search_queries=intent.search_queries,
            )

            candidates.extend(role_candidates)

        return ResourceDiscoveryResult(candidates=candidates, missing_roles=missing_roles)

    async def discover_scoped(
        self,
        intents: list[ResourceIntent],
        *,
        query: str | None = None,
        engine_id: int,
        scope_locator: str,
        limit: int = 20,
        allowed_data_types: set[str] | frozenset[str] | None = None,
    ) -> ResourceDiscoveryResult:
        """枚举 Owner 已确认 scope 的直接子资源，并用 preview 收敛候选事实。"""
        scope = await self.executor.call(
            "resource.children.list",
            {"engine_id": engine_id, "parent_locator": scope_locator},
            agent_run_id=self.agent_run_id,
            tool_call_id=f"resource-children-{uuid4()}",
        )
        if not isinstance(scope, dict) or scope.get("locator") != scope_locator:
            raise ToolExecutionError("invalid_owner_response", "Meta 未确认资源发现范围 locator")
        children = scope.get("children")
        if not isinstance(children, list):
            raise ToolExecutionError("invalid_owner_response", "Meta 未返回直接子资源列表")

        parent = {
            key: scope[key]
            for key in ("label", "type", "locator")
            if isinstance(scope.get(key), str) and scope[key]
        }
        verified_children: list[dict[str, Any]] = []
        verification_errors: list[ToolExecutionError] = []
        for child in children[:limit]:
            if not isinstance(child, dict):
                continue
            locator = str(child.get("locator") or "").strip()
            if not locator:
                continue
            try:
                preview = await self.executor.call(
                    "data.preview",
                    {"locator": locator, "limit": 5},
                    agent_run_id=self.agent_run_id,
                    tool_call_id=f"data-preview-{uuid4()}",
                )
            except ToolExecutionError as error:
                verification_errors.append(error)
                continue
            fact = _resource_fact(preview, locator)
            if not fact:
                continue
            if allowed_data_types and fact["data_type"] not in allowed_data_types:
                continue
            verified_children.append({
                "name": child.get("label") or locator,
                "locator": locator,
                "engine_id": engine_id,
                "engine_name": None,
                "asset_type": str(child.get("type") or "").lower(),
                "full_name": child.get("label"),
                "path": None,
                "score": None,
                "ancestors": [parent] if parent else [],
                **fact,
            })

        if not verified_children and verification_errors:
            raise verification_errors[-1]

        candidates: list[dict[str, Any]] = []
        missing_roles: list[str] = []
        for intent in intents:
            role_candidates = [dict(candidate, role=intent.role) for candidate in verified_children]
            if not role_candidates:
                missing_roles.append(intent.role)
            await self._rank_role_candidates(
                intent.role,
                role_candidates,
                query=query,
                search_queries=intent.search_queries,
            )
            candidates.extend(role_candidates)
        return ResourceDiscoveryResult(candidates=candidates, missing_roles=missing_roles)

    async def _rank_role_candidates(
        self,
        role: str,
        candidates: list[dict[str, Any]],
        *,
        query: str | None = None,
        search_queries: list[str] | None = None,
    ) -> None:
        recommendation = None
        if candidates and self.recommender is not None:
            recommendations = await self.recommender.recommend(
                candidates,
                query=query,
                search_queries=search_queries,
            )
            recommendation = recommendations.get(role) if isinstance(recommendations, dict) else None
        if recommendation is not None:
            rank = {
                locator: index
                for index, locator in enumerate(recommendation.ranked_locators)
            }
            candidates.sort(key=lambda candidate: rank.get(candidate["locator"], len(rank)))
        for candidate in candidates:
            is_recommended = bool(
                recommendation is not None
                and candidate["locator"] == recommendation.recommended_locator
            )
            candidate["recommended"] = is_recommended
            candidate["recommendation_reason"] = (
                recommendation.recommendation_reason if is_recommended else None
            )

    async def verify(
        self,
        resources: list[ResourceFact],
        *,
        engine_id: int | None = None,
        allowed_data_types: set[str] | frozenset[str] | None = None,
    ) -> list[ResourceFact]:
        """重新通过 owner Tool 校验用户确认的资源，并收敛最新字段事实。"""
        verified: list[ResourceFact] = []
        for resource in resources:
            if engine_id is not None and resource.engine_id != engine_id:
                raise ToolExecutionError(
                    "invalid_arguments",
                    "资源所属引擎必须与当前查询引擎一致",
                )
            resolved_engine_id = engine_id or resource.engine_id
            if resolved_engine_id is None or resolved_engine_id <= 0:
                raise ToolExecutionError("invalid_arguments", "资源事实缺少有效 engine_id")
            ancestors = await self.executor.call(
                "resource.ancestors.get",
                {"engine_id": resolved_engine_id, "locator": resource.locator},
                agent_run_id=self.agent_run_id,
                tool_call_id=f"resource-ancestors-{uuid4()}",
            )
            preview = await self.executor.call(
                "data.preview",
                {"locator": resource.locator, "limit": 5},
                agent_run_id=self.agent_run_id,
                tool_call_id=f"data-preview-{uuid4()}",
            )
            if not _ancestors_confirm_locator(ancestors, resource.locator):
                raise ToolExecutionError("invalid_owner_response", "Meta 未确认资源 locator")
            preview_fact = preview_resource_fact(preview)
            if (
                preview_fact is not None
                and preview_fact.get("locator") == resource.locator
                and not preview_fact.get("data_type")
            ):
                raise ToolExecutionError("invalid_arguments", "请选择具体可查询的数据项，不能使用数据库或目录容器")
            fact = _resource_fact(preview, resource.locator)
            if not fact:
                raise ToolExecutionError("invalid_owner_response", "数据预览未返回可用资源事实")
            verified_type = fact["data_type"]
            if allowed_data_types and verified_type not in allowed_data_types:
                raise ToolExecutionError("invalid_arguments", "资源类型不符合当前场景约束")
            verified.append(ResourceFact(
                role=resource.role,
                engine_id=resolved_engine_id,
                locator=resource.locator,
                data_type=verified_type,
                geometry_column=fact.get("geometry_column") or resource.geometry_column,
                geometry_type=fact.get("geometry_type") or resource.geometry_type,
                crs=fact.get("crs") or resource.crs,
                fields=fact.get("fields") or resource.fields,
            ))
        return verified


def _resource_fact(preview: dict[str, Any], locator: str) -> dict[str, Any] | None:
    preview_fact = preview_resource_fact(preview)
    if preview_fact is None or preview_fact["locator"] != locator or not preview_fact.get("data_type"):
        return None
    return {
        "data_type": preview_fact["data_type"],
        "geometry_column": preview_fact.get("geometry_column"),
        "geometry_type": preview_fact.get("geometry_type"),
        "crs": preview_fact.get("source_crs"),
        "fields": preview_fact.get("fields", [])[:200],
    }


def _hit_attribute(hit: dict[str, Any], key: str) -> str | None:
    """读取搜索结果已声明的标准 item 属性，不推断引擎或格式。"""
    direct = hit.get(key)
    if isinstance(direct, str) and direct.strip():
        return direct.strip()
    attributes = hit.get("attributes")
    if not isinstance(attributes, dict):
        return None
    for section in ("item", "format_info"):
        value = attributes.get(section)
        if isinstance(value, dict) and isinstance(value.get(key), str) and value[key].strip():
            return value[key].strip()
    return None


def _ancestors_confirm_locator(ancestors: dict[str, Any], locator: str) -> bool:
    return isinstance(ancestors, dict) and ancestors.get("target_locator") == locator


def _ancestor_summary(ancestors: dict[str, Any]) -> list[dict[str, Any]]:
    values = ancestors.get("ancestors") if isinstance(ancestors, dict) else None
    if not isinstance(values, list):
        return []
    return [
        {key: item[key] for key in ("label", "type", "locator") if key in item}
        for item in values
        if isinstance(item, dict) and item.get("locator")
    ]
