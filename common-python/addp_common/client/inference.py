"""Strongly typed client for the ADDP Inference Runtime data plane."""

from __future__ import annotations

import json
from collections.abc import AsyncIterator
from typing import Literal
from urllib.parse import urlsplit, urlunsplit

import httpx

from ..httptrace.client import trace_transport
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


class ChatStreamEvent(_ContractModel):
    model_config = ConfigDict(extra="forbid", strict=True)
    schema_version: Literal["addp.inference-stream/v1"]
    type: Literal["start", "text_delta", "completed", "error"]
    sequence: int = Field(ge=1)
    deployment_id: str = ""
    profile_version: int = 0
    delta: str = ""
    usage: Usage | None = None
    error_code: str = ""


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
        system_url: str,
        token_source: OAuthServiceTokenSource,
        *,
        timeout: float = 120.0,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        system_url = system_url.strip().rstrip("/")
        parsed = urlsplit(system_url)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc or parsed.path not in {"", "/"} or parsed.query or parsed.fragment:
            raise ValueError("inference client requires an absolute System HTTP(S) URL")
        if token_source is None:
            raise ValueError("inference client requires a service token source")
        self._system_url = system_url
        self._token_source = token_source
        self._client = httpx.AsyncClient(
            timeout=timeout,
            transport=trace_transport(system_url, transport),
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

    async def chat_stream(
        self, *, tenant_id: int, model_profile_id: str, messages: list[Message],
        temperature: float | None = None, max_output_tokens: int = 0,
    ) -> AsyncIterator[ChatStreamEvent]:
        tenant_id = self._tenant_id(tenant_id)
        payload = {"schema_version": SCHEMA_VERSION, "tenant_id": tenant_id,
                   "model_profile_id": self._profile_id(model_profile_id), "stream": True,
                   "messages": [message.model_dump(exclude_defaults=True) for message in messages]}
        if temperature is not None:
            payload["temperature"] = temperature
        if max_output_tokens > 0:
            payload["max_output_tokens"] = max_output_tokens
        for attempt in range(2):
            token = await self._token_source.token(tenant_id)
            try:
                runtime_url = await self._discover_runtime(token, streaming=True)
                async with self._client.stream("POST", runtime_url + "/api/v1/inference/internal/chat",
                    json=payload, headers={"Authorization": f"Bearer {token}"}) as response:
                    if response.status_code == 401 and attempt == 0:
                        self._token_source.invalidate(tenant_id, token)
                        continue
                    if response.status_code != 200:
                        code = "inference_timeout" if response.status_code == 504 else "inference_upstream_failed"
                        if response.status_code == 401:
                            code = "inference_runtime_unauthorized"
                        raise InferenceError(code)
                    if response.headers.get("content-type", "").split(";", 1)[0] != "text/event-stream":
                        raise InferenceError("inference_invalid_stream")
                    sequence, total, text_bytes, started, completed = 0, 0, 0, False, False
                    buffer = b""
                    async for chunk in response.aiter_bytes():
                        total += len(chunk)
                        if total > 32 * 1024 * 1024:
                            raise InferenceError("inference_invalid_stream")
                        buffer += chunk
                        while True:
                            ends = [(buffer.find(mark), len(mark)) for mark in (b"\n\n", b"\r\n\r\n")]
                            ends = [(at, size) for at, size in ends if at >= 0]
                            if not ends:
                                break
                            at, size = min(ends)
                            frame, buffer = buffer[:at], buffer[at+size:]
                            if len(frame) > 512 * 1024:
                                raise InferenceError("inference_invalid_stream")
                            lines = [line[5:].lstrip(b" ") for line in frame.splitlines() if line.startswith(b"data:")]
                            if not lines:
                                continue
                            try:
                                event = ChatStreamEvent.model_validate_json(b"\n".join(lines))
                            except ValueError as exc:
                                raise InferenceError("inference_invalid_stream") from exc
                            if completed or event.sequence != sequence + 1:
                                raise InferenceError("inference_invalid_stream")
                            sequence = event.sequence
                            if event.type == "error":
                                if not started or event.delta or event.usage is not None or event.deployment_id or event.profile_version or event.error_code not in {"inference_timeout", "inference_upstream_failed", "inference_upstream_unavailable", "inference_operation_unsupported"}:
                                    raise InferenceError("inference_invalid_stream")
                                raise InferenceError(event.error_code)
                            if event.type == "start":
                                if started or not event.deployment_id or event.profile_version < 1 or event.delta or event.usage is not None:
                                    raise InferenceError("inference_invalid_stream")
                                started = True
                            elif not started:
                                raise InferenceError("inference_invalid_stream")
                            elif event.type == "text_delta":
                                if not event.delta or event.usage is not None or event.deployment_id or event.profile_version:
                                    raise InferenceError("inference_invalid_stream")
                                text_bytes += len(event.delta.encode("utf-8"))
                                if text_bytes > 2 * 1024 * 1024:
                                    raise InferenceError("inference_invalid_stream")
                            elif event.type == "completed":
                                if not text_bytes or event.usage is None or event.delta or event.deployment_id or event.profile_version or min(event.usage.input_tokens, event.usage.output_tokens, event.usage.total_tokens) < 0:
                                    raise InferenceError("inference_invalid_stream")
                                completed = True
                            if event.error_code:
                                raise InferenceError("inference_invalid_stream")
                            yield event
                        if len(buffer) > 512 * 1024:
                            raise InferenceError("inference_invalid_stream")
                    if buffer.strip() or not completed:
                        raise InferenceError("inference_incomplete_stream")
                    return
            except _RuntimeDiscoveryUnauthorized as exc:
                if attempt == 0:
                    self._token_source.invalidate(tenant_id, token)
                    continue
                raise InferenceError("inference_runtime_unauthorized") from exc
            except httpx.TimeoutException as exc:
                raise InferenceError("inference_timeout") from exc
            except httpx.HTTPError as exc:
                raise InferenceError("inference_runtime_unavailable") from exc

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
                runtime_url = await self._discover_runtime(token)
            except _RuntimeDiscoveryUnauthorized:
                if attempt == 0:
                    self._token_source.invalidate(tenant_id, token)
                    continue
                raise InferenceError("inference_runtime_unauthorized")
            except httpx.HTTPError as exc:
                raise InferenceError("inference_runtime_unavailable") from exc
            try:
                response = await self._client.post(
                    runtime_url + path,
                    json=payload,
                    headers={"Authorization": f"Bearer {token}"},
                )
            except httpx.TimeoutException as exc:
                raise InferenceError("inference_timeout") from exc
            except httpx.HTTPError as exc:
                raise InferenceError("inference_runtime_unavailable") from exc
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

    async def _discover_runtime(self, token: str, *, streaming: bool = False) -> str:
        response = await self._client.get(
            self._system_url + "/api/v1/system/runtime/engine-descriptors",
            params={"engine_type": "inference_runtime", "page": 1, "page_size": 2},
            headers={"Authorization": f"Bearer {token}"},
        )
        if response.status_code < 200 or response.status_code >= 300:
            if response.status_code == 401:
                raise _RuntimeDiscoveryUnauthorized()
            raise InferenceError("inference_runtime_discovery_failed")
        try:
            payload = response.json()
            descriptors = payload["data"]
            total = payload["total"]
        except (KeyError, TypeError, ValueError) as exc:
            raise InferenceError("inference_runtime_discovery_invalid") from exc
        if not isinstance(descriptors, list) or not isinstance(total, int) or isinstance(total, bool):
            raise InferenceError("inference_runtime_discovery_invalid")
        if total == 0:
            raise InferenceError("inference_runtime_not_found")
        if total != 1 or len(descriptors) != 1:
            raise InferenceError("inference_runtime_ambiguous")
        descriptor = descriptors[0]
        if not isinstance(descriptor, dict):
            raise InferenceError("inference_runtime_discovery_invalid")
        if (
            descriptor.get("engine_type") != "inference_runtime"
            or descriptor.get("is_builtin") is not True
            or descriptor.get("lifecycle_state") != "active"
        ):
            raise InferenceError("inference_runtime_not_found")
        capabilities = descriptor.get("capabilities")
        if isinstance(capabilities, str):
            try:
                capabilities = json.loads(capabilities)
            except (TypeError, ValueError) as exc:
                raise InferenceError("inference_runtime_discovery_invalid") from exc
        if not isinstance(capabilities, dict):
            raise InferenceError("inference_runtime_discovery_invalid")
        inference_capability = ((capabilities.get("compute") or {}).get("inference") or {})
        if capabilities.get("schema_version") != "engine.capabilities/v1" or inference_capability.get("supported") is not True or inference_capability.get("runtime_api") != SCHEMA_VERSION:
            raise InferenceError("inference_runtime_not_found")
        if streaming and inference_capability.get('streaming') is not True:
            raise InferenceError('inference_operation_unsupported')
        endpoint = descriptor.get("runtime_endpoint")
        if not isinstance(endpoint, dict):
            raise InferenceError("inference_runtime_discovery_invalid")
        protocol = endpoint.get("protocol")
        host = endpoint.get("host")
        port = endpoint.get("port")
        if protocol not in {"http", "https"} or not isinstance(host, str) or not host.strip() or not isinstance(port, int) or isinstance(port, bool) or not 0 < port <= 65535:
            raise InferenceError("inference_runtime_discovery_invalid")
        if ":" in host and not host.startswith("["):
            host = f"[{host}]"
        return urlunsplit((protocol, f"{host}:{port}", "", "", ""))

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


class _RuntimeDiscoveryUnauthorized(Exception):
    pass
