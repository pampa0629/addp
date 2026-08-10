"""Agent-owned Inference Scenario resolution and LangChain model factory."""

from __future__ import annotations

import asyncio
from typing import ClassVar

from addp_common.client import InferenceClient, OAuthServiceTokenSource
from addp_common.inference_langchain import InferenceChatModel

from config import settings
from database import AsyncSessionLocal
from repositories.inference_scenario_binding_repository import InferenceScenarioBindingRepository
from utils.logging_setup import LLMIOLogger


SCENARIOS: tuple[str, ...] = ("reasoning", "general-chat")


class InferenceScenarioNotConfigured(RuntimeError):
    pass


class InferenceClientNotInitialized(RuntimeError):
    pass


class _BindingSnapshot:
    """Resolve an owner binding once and keep it stable for one model instance."""

    def __init__(self, tenant_id: int, scenario_code: str) -> None:
        self._tenant_id = tenant_id
        self._scenario_code = scenario_code
        self._model_profile_id = ""
        self._lock = asyncio.Lock()

    async def resolve(self) -> str:
        if self._model_profile_id:
            return self._model_profile_id
        async with self._lock:
            if self._model_profile_id:
                return self._model_profile_id
            async with AsyncSessionLocal() as db:
                binding = await InferenceScenarioBindingRepository(db).resolve(
                    self._tenant_id,
                    self._scenario_code,
                )
            if binding is None:
                raise InferenceScenarioNotConfigured("inference_scenario_not_configured")
            self._model_profile_id = binding.model_profile_id
            return self._model_profile_id


class AgentInferenceService:
    _token_source: ClassVar[OAuthServiceTokenSource | None] = None
    _client: ClassVar[InferenceClient | None] = None

    @classmethod
    def initialize(cls) -> None:
        if cls._client is not None:
            return
        cls._token_source = OAuthServiceTokenSource(
            settings.get_system_url(),
            "addp-agent",
            settings.AGENT_SERVICE_CLIENT_SECRET,
        )
        cls._client = InferenceClient(settings.get_system_url(), cls._token_source)

    @classmethod
    async def close(cls) -> None:
        if cls._client is not None:
            await cls._client.close()
        if cls._token_source is not None:
            await cls._token_source.close()
        cls._client = None
        cls._token_source = None

    @classmethod
    def token_source(cls) -> OAuthServiceTokenSource:
        if cls._token_source is None:
            raise InferenceClientNotInitialized("inference_client_not_initialized")
        return cls._token_source

    @classmethod
    def chat_model(
        cls,
        *,
        tenant_id: int,
        scenario_code: str,
        temperature: float | None = None,
        max_output_tokens: int = 0,
    ) -> InferenceChatModel:
        if scenario_code not in SCENARIOS:
            raise ValueError("unsupported Agent inference scenario")
        if cls._client is None:
            raise InferenceClientNotInitialized("inference_client_not_initialized")
        snapshot = _BindingSnapshot(tenant_id, scenario_code)
        return InferenceChatModel(
            client=cls._client,
            tenant_id=tenant_id,
            profile_resolver=snapshot.resolve,
            temperature=temperature,
            max_output_tokens=max_output_tokens,
            callbacks=[LLMIOLogger()],
        )


def get_llm(
    tenant_id: int,
    scenario_code: str,
    *,
    temperature: float | None = 0.7,
    max_output_tokens: int = 0,
) -> InferenceChatModel:
    return AgentInferenceService.chat_model(
        tenant_id=tenant_id,
        scenario_code=scenario_code,
        temperature=temperature,
        max_output_tokens=max_output_tokens,
    )
