"""Copilot Scenario Binding resolution and Inference Runtime access."""

from __future__ import annotations

from typing import ClassVar
from uuid import UUID

from addp_common.client import InferenceClient, OAuthServiceTokenSource
from addp_common.inference_langchain import InferenceChatModel as CopilotInferenceChatModel
from sqlalchemy.orm import Session

from config import settings
from models.inference_scenario_binding import InferenceScenarioBinding
from repositories.inference_scenario_binding_repository import InferenceScenarioBindingRepository


SCENARIOS: tuple[str, ...] = (
    "nl2sql",
    "nl2dag",
    "navigation_guide",
    "knowledge_graph_extraction",
)


class InferenceScenarioNotConfigured(RuntimeError):
    pass


class InferenceClientNotInitialized(RuntimeError):
    pass


class CopilotInferenceService:
    _token_source: ClassVar[OAuthServiceTokenSource | None] = None
    _client: ClassVar[InferenceClient | None] = None

    @classmethod
    def initialize(cls) -> None:
        if cls._client is not None:
            return
        cls._token_source = OAuthServiceTokenSource(
            settings.get_system_url(),
            "addp-copilot",
            settings.copilot_service_client_secret,
        )
        cls._client = InferenceClient(settings.inference_url, cls._token_source)

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
        db: Session,
        *,
        tenant_id: int,
        scenario_code: str,
        temperature: float | None = None,
        max_output_tokens: int = 0,
    ) -> CopilotInferenceChatModel:
        if scenario_code not in SCENARIOS:
            raise ValueError("unsupported Copilot inference scenario")
        binding = InferenceScenarioBindingRepository(db).resolve(tenant_id, scenario_code)
        if binding is None:
            raise InferenceScenarioNotConfigured("inference_scenario_not_configured")
        if cls._client is None:
            raise InferenceClientNotInitialized("inference_client_not_initialized")
        return CopilotInferenceChatModel(
            client=cls._client,
            tenant_id=tenant_id,
            model_profile_id=binding.model_profile_id,
            temperature=temperature,
            max_output_tokens=max_output_tokens,
        )


def validate_profile_id(value: str) -> str:
    return str(UUID(value.strip()))


def binding_to_dict(binding: InferenceScenarioBinding, *, effective: bool) -> dict[str, object]:
    return {
        "scenario_code": binding.scenario_code,
        "scope_type": binding.scope_type,
        "tenant_id": binding.tenant_id,
        "model_profile_id": binding.model_profile_id,
        "version": binding.version,
        "updated_by": binding.updated_by,
        "updated_at": binding.updated_at,
        "effective": effective,
    }
