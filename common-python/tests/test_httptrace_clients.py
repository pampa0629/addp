"""Exercise tracing through the actual shared clients, including auth retries."""
from contextlib import contextmanager
from unittest.mock import patch

import httpx
import pytest
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from addp_common.client import BaseClient, EmbeddingInput, InferenceClient, ModuleRegistration, ModuleRegistryClient, OAuthServiceTokenSource, ServiceConsumerClient
from addp_common.client.service_token import SyncOAuthServiceTokenSource
from addp_common.httptrace import runtime
from addp_common.httptrace.client import trace_transport


@contextmanager
def traced_request():
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    tracer = provider.get_tracer("client-integration-test")
    token = runtime.request_id_var.set("6f4ff34d-583f-42c8-b193-6502161b1534")
    try:
        with patch.object(runtime, "enabled", return_value=True), patch.object(runtime, "tracer", return_value=tracer):
            with tracer.start_as_current_span("incoming") as parent:
                yield parent.get_span_context(), exporter
    finally:
        runtime.request_id_var.reset(token)
        provider.shutdown()


def assert_context(request, parent):
    value = request.headers["traceparent"].split("-")
    assert value[1] == f"{parent.trace_id:032x}"
    assert value[2] != f"{parent.span_id:016x}"
    assert request.headers["x-request-id"] == "6f4ff34d-583f-42c8-b193-6502161b1534"
    assert "baggage" not in request.headers
    assert "tracestate" not in request.headers


@pytest.mark.asyncio
async def test_base_service_retry_and_token_refresh_keep_auth_and_parentage():
    requests = []

    async def token_handler(request):
        requests.append(request)
        return httpx.Response(200, json={"access_token": f"addp_at_token-{len(requests)}", "token_type": "Bearer", "expires_in": 120, "scope": "addp.api"})

    owner_calls = []

    async def owner_handler(request):
        owner_calls.append(request)
        return httpx.Response(401 if len(owner_calls) == 1 else 200, json={"ok": True})

    source = OAuthServiceTokenSource("http://system", "addp-agent", "test-service-client-secret-32bytes", transport=httpx.MockTransport(token_handler))
    with patch("addp_common.client.base.trace_transport", side_effect=lambda origin: trace_transport(origin, httpx.MockTransport(owner_handler))):
        client = BaseClient("http://owner", tenant_id=7, service_token_source=source)
    try:
        with traced_request() as (parent, exporter):
            assert await client.post("/resource", json={"private": "not-a-span-attribute"}) == {"ok": True}
            for request in requests + owner_calls:
                assert_context(request, parent)
            spans = exporter.get_finished_spans()
            assert len(spans) == 4
            assert all(span.parent.span_id == parent.span_id for span in spans)
            assert len({span.context.span_id for span in spans}) == 4
            assert owner_calls[0].headers["Authorization"] != owner_calls[1].headers["Authorization"]
            assert all("private" not in str(dict(span.attributes)) for span in spans)
            assert "traceparent" not in client._client.headers
    finally:
        await client.close()
        await source.close()


@pytest.mark.asyncio
async def test_registry_and_service_consumer_requests_use_configured_origins():
    requests = []

    async def handler(request):
        requests.append(request)
        return httpx.Response(200, json={"data": [], "total": 0, "page": 1, "page_size": 20, "total_pages": 0})

    class TokenSource:
        async def platform_token(self):
            return "addp_at_registry"

    registry = ModuleRegistryClient("http://system", TokenSource(), transport=httpx.MockTransport(handler))
    consumer = ServiceConsumerClient("http://gateway", user_token="addp_at_user", transport=httpx.MockTransport(handler))
    try:
        with traced_request() as (parent, exporter):
            await registry.register(ModuleRegistration(module_name="agent", module_url="http://agent", route_prefix="/agent"))
            page = await consumer.list_services()
            assert page.total == 0
            for request in requests:
                assert_context(request, parent)
            assert [request.url.host for request in requests] == ["system", "gateway"]
            assert [request.headers["Authorization"] for request in requests] == ["Bearer addp_at_registry", "Bearer addp_at_user"]
            assert len(exporter.get_finished_spans()) == 2
    finally:
        # Avoid an additional deregistration operation in this test's assertion.
        registry._active_instances.clear()
        await registry.close()
        await consumer.close()


@pytest.mark.asyncio
async def test_inference_discovery_does_not_expand_origin_trust_to_runtime():
    requests = []

    async def handler(request):
        requests.append(request)
        if request.url.host == "system":
            return httpx.Response(200, json={
                "data": [{"engine_type": "inference_runtime", "is_builtin": True, "lifecycle_state": "active",
                          "capabilities": {"schema_version": "engine.capabilities/v1", "compute": {"inference": {"supported": True, "runtime_api": "addp.inference/v1"}}},
                          "runtime_endpoint": {"protocol": "http", "host": "inference", "port": 8191}}], "total": 1})
        return httpx.Response(200, json={"schema_version": "addp.inference/v1", "vectors": [[0.1]], "dimension": 1,
                                         "deployment_id": "model", "profile_version": 1, "usage": {"input_tokens": 1, "total_tokens": 1}})

    class TokenSource:
        async def token(self, _tenant):
            return "addp_at_inference"

    client = InferenceClient("http://system", TokenSource(), transport=httpx.MockTransport(handler))
    try:
        with traced_request() as (parent, exporter):
            result = await client.embed(tenant_id=7, model_profile_id="model", inputs=[EmbeddingInput(modality="text", text="private text")])
            assert result.dimension == 1
            assert_context(requests[0], parent)
            assert requests[1].url.host == "inference"
            for name in ("traceparent", "tracestate", "baggage", "x-request-id"):
                assert name not in requests[1].headers
            assert requests[1].headers["Authorization"] == "Bearer addp_at_inference"
            assert len(exporter.get_finished_spans()) == 1
    finally:
        await client.close()


def test_sync_token_source_uses_same_context_without_global_instrumentation():
    requests = []

    def handler(request):
        requests.append(request)
        return httpx.Response(200, json={"access_token": "addp_at_sync", "token_type": "Bearer", "expires_in": 120, "scope": "addp.api"})

    source = SyncOAuthServiceTokenSource("http://system", "addp-agent", "test-service-client-secret-32bytes", transport=httpx.MockTransport(handler))
    try:
        with traced_request() as (parent, exporter):
            assert source.platform_token() == "addp_at_sync"
            assert_context(requests[0], parent)
            assert len(exporter.get_finished_spans()) == 1
    finally:
        source.close()
