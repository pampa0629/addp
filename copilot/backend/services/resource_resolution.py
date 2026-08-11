"""统一输入资源解析与确认编排。

该服务只负责把自然语言输入收敛为 owner 已验证的 ResourceFact。领域生成器
（查询、工作流、Notebook、Transfer）不再各自复制意图提取、候选重试或确认规则。
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from addp_common.resources import ResourceFact

from chains.notebook_resource_recommendation_chain import NotebookResourceRecommendationChain
from chains.resource_intent_chain import ResourceIntent, ResourceIntentChain, ResourceIntentScope
from chains.resource_recommendation_chain import ResourceRecommendationChain
from services.resource_discovery import ResourceDiscovery, ResourceDiscoveryResult


QUERY_DATA_TYPES = frozenset({"table", "graph"})


@dataclass(frozen=True)
class ResourceResolutionPolicy:
    """Explicit scope and cardinality constraints for one domain entry point."""

    name: str
    intent_scope: ResourceIntentScope = ResourceIntentScope.INPUTS
    engine_id: int | None = None
    max_inputs: int = 8
    max_candidates: int = 20
    allowed_data_types: frozenset[str] | None = None
    session_catalog: bool = False

    @classmethod
    def query(cls, engine_id: int) -> "ResourceResolutionPolicy":
        return cls(
            name="query",
            engine_id=engine_id,
            max_inputs=8,
            allowed_data_types=QUERY_DATA_TYPES,
        )

    @classmethod
    def workflow(cls) -> "ResourceResolutionPolicy":
        return cls(name="workflow", max_inputs=8)

    @classmethod
    def notebook(cls) -> "ResourceResolutionPolicy":
        return cls(name="notebook", max_inputs=8, session_catalog=True)

    @classmethod
    def transfer(cls, source_engine_id: int | None = None) -> "ResourceResolutionPolicy":
        return cls(
            name="transfer",
            intent_scope=ResourceIntentScope.TRANSFER_SOURCE,
            engine_id=source_engine_id,
            max_inputs=1,
        )


@dataclass(frozen=True)
class ResourceResolutionResult:
    intents: list[ResourceIntent]
    candidates: list[dict[str, Any]]
    missing_roles: list[str]


class ResourceResolutionService:
    """共享资源确认流程；策略只表达场景限制，不改变主流程。"""

    def __init__(
        self,
        *,
        discovery: ResourceDiscovery | None = None,
        intent_chain: ResourceIntentChain,
        recommender: ResourceRecommendationChain | None = None,
        notebook_recommender: NotebookResourceRecommendationChain | None = None,
    ) -> None:
        self.discovery = discovery
        self.intent_chain = intent_chain
        self.recommender = recommender
        self.notebook_recommender = notebook_recommender

    async def extract(self, query: str, policy: ResourceResolutionPolicy) -> list[ResourceIntent]:
        intents = await self.intent_chain.extract(query, scope=policy.intent_scope)
        self._validate_intent_count(intents, policy)
        return intents

    async def expand_missing(
        self,
        query: str,
        missing_intents: list[ResourceIntent],
    ) -> list[ResourceIntent]:
        return await self.intent_chain.expand_missing(query, missing_intents)

    async def discover(
        self,
        query: str,
        policy: ResourceResolutionPolicy,
    ) -> ResourceResolutionResult:
        if self.discovery is None or policy.session_catalog:
            raise ValueError(f"{policy.name} 场景不支持 Tool 资源搜索")
        intents = await self.extract(query, policy)
        if not intents:
            return ResourceResolutionResult(intents=[], candidates=[], missing_roles=[])
        if policy.name == "transfer" and len(intents) != 1:
            return ResourceResolutionResult(intents=intents, candidates=[], missing_roles=[])

        result = await self._discover_once(intents, policy)
        if result.missing_roles:
            missing = [item for item in intents if item.role in set(result.missing_roles)]
            expanded = await self.expand_missing(query, missing)
            if expanded:
                retry = await self._discover_once(expanded, policy)
                result = ResourceDiscoveryResult(
                    candidates=[*result.candidates, *retry.candidates],
                    missing_roles=[role for role in result.missing_roles if role in retry.missing_roles],
                )
        return ResourceResolutionResult(
            intents=intents,
            candidates=result.candidates,
            missing_roles=result.missing_roles,
        )

    async def rank_session_candidates(
        self,
        query: str,
        candidates: list[dict[str, Any]],
        policy: ResourceResolutionPolicy,
    ) -> dict[str, Any]:
        if not policy.session_catalog:
            raise ValueError(f"{policy.name} 场景不支持 Session Catalog 候选")
        if self.notebook_recommender is None:
            return {}
        return await self.notebook_recommender.recommend(query, candidates)

    async def verify(
        self,
        resources: list[ResourceFact],
        policy: ResourceResolutionPolicy,
    ) -> list[ResourceFact]:
        if self.discovery is None:
            raise ValueError("当前资源确认策略没有 owner 校验器")
        if not resources or len(resources) > policy.max_inputs:
            raise ValueError(f"{policy.name} 输入资源数量必须为 1 到 {policy.max_inputs}")
        if policy.name == "transfer" and len(resources) != 1:
            raise ValueError("Transfer 只允许一个源资源")
        return await self.discovery.verify(
            resources,
            engine_id=policy.engine_id,
            allowed_data_types=policy.allowed_data_types,
        )

    async def _discover_once(
        self,
        intents: list[ResourceIntent],
        policy: ResourceResolutionPolicy,
    ) -> ResourceDiscoveryResult:
        return await self.discovery.discover(
            intents,
            engine_id=policy.engine_id,
            limit=policy.max_candidates,
            allowed_data_types=policy.allowed_data_types,
        )

    @staticmethod
    def _validate_intent_count(intents: list[ResourceIntent], policy: ResourceResolutionPolicy) -> None:
        if policy.name == "transfer":
            return
        if len(intents) > policy.max_inputs:
            raise ValueError(f"{policy.name} 输入数据项超过上限")
