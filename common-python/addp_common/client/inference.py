"""Strongly typed client for the ADDP Inference Runtime data plane."""

from __future__ import annotations

from typing import Literal
from urllib.parse import urlsplit

import httpx
from pydantic import BaseModel, ConfigDict, Field

from .service_token import OAuthServiceTokenSource


SCHEMA_VERSION = "addp.inference/v1"


class _ContractModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ToolCall(_ContractModel):
    id: str
    name: str
    arguments: dict[str, object]


class Message(_ContractModel):
    role: Literal["system", "user", "assistant", "tool"]
    content: str = ""
    tool_call_id: str = ""
    tool_calls: list[ToolCall] = Field(default_factory=list)


class ToolDefinition(_ContractModel):
    name: str
    description: str = ""
    parameters: dict[str, object]


class ResponseSchema(_ContractModel):
    name: str
    description: str = ""
    schema_value: dict[str, object] = Field(alias="schema", serialization_alias="schema")
    strict: bool = True


class Usage(_ContractModel):
    input_tokens: int = 0
    output_tokens: int = 0
    total_tokens: int = 0


class ResolveProfileResponse(_ContractModel):
    schema_version: Literal[SCHEMA_VERSION]
    model_profile_id: str
    profile_version: int
    deployment_id: str
    dimension: int


class ChatResponse(_ContractModel):
    schema_version: Literal[SCHEMA_VERSION]
    message: Message
    usage: Usage
    deployment_id: str
    profile_version: int


class EmbeddingInput(_ContractModel):
    modality: Literal["text", "image"]
    text: str = ""
    data: str = ""
    mime_type: str = ""


class EmbeddingResponse(_ContractModel):
    schema_version: Literal[SCHEMA_VERSION]
    vectors: list[list[float]]
    dimension: int
    usage: Usage
    deployment_id: str
    profile_version: int


class RerankDocument(_ContractModel):
    id: str
    text: str


class RerankResult(_ContractModel):
    document_id: str
    index: int
    score: float


class RerankResponse(_ContractModel):
    schema_version: Literal[SCHEMA_VERSION]
    results: list[RerankResult]
    usage: Usage
    deployment_id: str
    profile_version: int


class InferenceError(RuntimeError):
    def __init__(self, error_code: str, message: str = "") -> None:
        super().__init__(f"{error_code}: {message}" if message else error_code)
        self.error_code = error_code


class InferenceClient:
    def __init__(
        self,
        base_url: str,
        token_source: OAuthServiceTokenSource,
        *,
        timeout: float = 120.0,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        base_url = base_url.strip().rstrip("/")
        parsed = urlsplit(base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc or parsed.query or parsed.fragment:
            raise ValueError("inference base URL must be an absolute HTTP(S) URL")
        if token_source is None:
            raise ValueError("inference client requires a service token source")
        self._token_source = token_source
        self._client = httpx.AsyncClient(
            base_url=base_url,
            timeout=timeout,
            transport=transport,
            trust_env=False,
            headers={"Content-Type": "application/json"},
        )

    async def chat(
        self,
        *,
        tenant_id: int,
        model_profile_id: str,
        messages: list[Message],
        tools: list[ToolDefinition] | None = None,
        tool_choice: Literal["auto", "none", "required"] | None = None,
        response_schema: ResponseSchema | None = None,
        temperature: float | None = None,
        max_output_tokens: int = 0,
    ) -> ChatResponse:
        payload: dict[str, object] = {
            "schema_version": SCHEMA_VERSION,
            "tenant_id": self._tenant_id(tenant_id),
            "model_profile_id": self._profile_id(model_profile_id),
            "messages": [message.model_dump(exclude_defaults=True) for message in messages],
        }
        if tools:
            payload["tools"] = [tool.model_dump(exclude_defaults=True) for tool in tools]
        if tool_choice is not None:
            payload["tool_choice"] = tool_choice
        if response_schema is not None:
            payload["response_schema"] = response_schema.model_dump(exclude_defaults=True, by_alias=True)
        if temperature is not None:
            payload["temperature"] = temperature
        if max_output_tokens > 0:
            payload["max_output_tokens"] = max_output_tokens
        response = await self._post("/api/v1/inference/internal/chat", tenant_id, payload)
        return ChatResponse.model_validate(response)

    async def resolve_profile(
        self,
        *,
        tenant_id: int,
        model_profile_id: str,
        operation: Literal["chat", "embedding", "rerank"],
        modality: Literal["text", "image"],
    ) -> ResolveProfileResponse:
        payload = {
            "schema_version": SCHEMA_VERSION,
            "tenant_id": self._tenant_id(tenant_id),
            "model_profile_id": self._profile_id(model_profile_id),
            "operation": operation,
            "modality": modality,
        }
        response = await self._post("/api/v1/inference/internal/profiles/resolve", tenant_id, payload)
        return ResolveProfileResponse.model_validate(response)

    async def embed(
        self,
        *,
        tenant_id: int,
        model_profile_id: str,
        inputs: list[EmbeddingInput],
    ) -> EmbeddingResponse:
        payload = {
            "schema_version": SCHEMA_VERSION,
            "tenant_id": self._tenant_id(tenant_id),
            "model_profile_id": self._profile_id(model_profile_id),
            "inputs": [item.model_dump(exclude_defaults=True) for item in inputs],
        }
        response = await self._post("/api/v1/inference/internal/embeddings", tenant_id, payload)
        return EmbeddingResponse.model_validate(response)

    async def rerank(
        self,
        *,
        tenant_id: int,
        model_profile_id: str,
        query: str,
        documents: list[RerankDocument],
        top_n: int = 0,
    ) -> RerankResponse:
        payload: dict[str, object] = {
            "schema_version": SCHEMA_VERSION,
            "tenant_id": self._tenant_id(tenant_id),
            "model_profile_id": self._profile_id(model_profile_id),
            "query": query,
            "documents": [document.model_dump() for document in documents],
        }
        if top_n > 0:
            payload["top_n"] = top_n
        response = await self._post("/api/v1/inference/internal/rerank", tenant_id, payload)
        return RerankResponse.model_validate(response)

    async def _post(self, path: str, tenant_id: int, payload: dict[str, object]) -> object:
        tenant_id = self._tenant_id(tenant_id)
        for attempt in range(2):
            token = await self._token_source.token(tenant_id)
            try:
                response = await self._client.post(
                    path,
                    json=payload,
                    headers={"Authorization": f"Bearer {token}"},
                )
            except httpx.HTTPError as exc:
                raise InferenceError("inference_unavailable") from exc
            if response.status_code != 401 or attempt == 1:
                break
            self._token_source.invalidate(tenant_id, token)
        if response.status_code < 200 or response.status_code >= 300:
            try:
                failure = response.json()
                error_code = failure.get("error_code") or "inference_upstream_failed"
                message = failure.get("error") or ""
            except (TypeError, ValueError):
                error_code, message = "inference_upstream_failed", ""
            raise InferenceError(str(error_code), str(message))
        return response.json()

    @staticmethod
    def _tenant_id(value: int) -> int:
        if not isinstance(value, int) or isinstance(value, bool) or value <= 0:
            raise ValueError("inference request requires a tenant ID")
        return value

    @staticmethod
    def _profile_id(value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("inference request requires a model profile ID")
        return value

    async def close(self) -> None:
        await self._client.aclose()

    async def __aenter__(self) -> "InferenceClient":
        return self

    async def __aexit__(self, *_args: object) -> None:
        await self.close()
