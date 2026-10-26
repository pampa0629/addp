"""Copilot shares HTTP tracing without gating registration or changing auth."""
import asyncio
from unittest.mock import AsyncMock, Mock, patch

import pytest


@pytest.mark.asyncio
async def test_database_failure_releases_trace_using_same_registration():
    import main

    close = Mock()
    with (
        patch.object(main.trace_runtime, "start_optional", return_value=close) as start,
        patch("database.init_db", new=AsyncMock(side_effect=RuntimeError("db unavailable"))),
    ):
        with pytest.raises(RuntimeError, match="db unavailable"):
            async with main.lifespan(main.app):
                pytest.fail("startup should fail")
    start.assert_called_once_with(main._registration)
    close.assert_called_once_with()


@pytest.mark.asyncio
async def test_lifespan_failure_still_releases_registry_and_trace():
    import main

    close = Mock()
    registry = Mock(close=AsyncMock())
    with (
        patch.object(main.trace_runtime, "start_optional", return_value=close) as start,
        patch("database.init_db", new=AsyncMock()),
        patch("services.inference_service.CopilotInferenceService.initialize"),
        patch("services.inference_service.CopilotInferenceService.token_source", return_value=object()),
        patch("services.inference_service.CopilotInferenceService.close", new=AsyncMock()),
        patch.object(main, "ModuleRegistryClient", return_value=registry),
        patch.object(main, "register_after_listener", new=AsyncMock()),
        patch.object(main, "terminate_process_on_registration_failure", new=AsyncMock()),
    ):
        with pytest.raises(RuntimeError, match="controlled lifespan error"):
            async with main.lifespan(main.app):
                raise RuntimeError("controlled lifespan error")
    start.assert_called_once_with(main._registration)
    registry.close.assert_awaited_once_with()
    close.assert_called_once_with()
    assert main._registry_client is None


def test_real_stack_records_ready_rejection_auth_stream_and_failure():
    import main
    from addp_common.httptrace import runtime
    from fastapi.testclient import TestClient
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import SimpleSpanProcessor
    from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
    from starlette.responses import StreamingResponse
    from starlette.routing import Route

    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    observed = []

    async def stream(_request):
        async def chunks():
            observed.append(runtime.correlation())
            yield b"first"
            await asyncio.sleep(0)
            observed.append(runtime.correlation())
            yield b"second"
        return StreamingResponse(chunks())

    async def fail(_request):
        raise RuntimeError("controlled failure")

    routes = [Route("/__trace_stream", stream), Route("/__trace_fail", fail)]
    from addp_common.httpmetrics import process
    metrics = process()
    before = metrics.total[0], metrics.failures, metrics.client_errors
    old_stack = main.app.middleware_stack
    main.app.router.routes.extend(routes)
    main.app.middleware_stack = None
    try:
        with (
            patch.object(runtime, "enabled", return_value=True),
            patch.object(runtime, "tracer", return_value=provider.get_tracer("copilot-test")),
        ):
            client = TestClient(main.app, raise_server_exceptions=False)
            response = client.get("/api/v1/copilot/settings/inference-bindings/sql")
            assert response.status_code == 503
            assert response.json()["error_code"] == "module_not_ready"
            assert response.headers.get("x-request-id")
            with patch.object(main, "ready_response", return_value=({}, True)):
                denied = client.get("/api/v1/copilot/settings/inference-bindings/sql")
                assert denied.status_code == 401
                streamed = client.get("/__trace_stream")
                assert streamed.content == b"firstsecond"
                failure = client.get("/__trace_fail")
                assert failure.status_code == 500
            assert runtime.correlation() == {}
        spans = exporter.get_finished_spans()
        assert len(spans) == 4
        assert (metrics.total[0]-before[0],metrics.failures-before[1],metrics.client_errors-before[2]) == (4,2,1)
        assert sum(metrics.active.values()) == 0
        assert [span.attributes["http.response.status_code"] for span in spans] == [503, 401, 200, 0]
        assert spans[-1].attributes["addp.http.server_failure"] is True
        assert spans[-1].attributes["error.type"] == "_OTHER"
        assert spans[-1].status.status_code.name == "ERROR"
        assert observed[0] == observed[1]
        assert observed[0]["trace_id"]
        assert observed[0]["request_id"] == streamed.headers["x-request-id"]
    finally:
        for route in routes:
            main.app.router.routes.remove(route)
        main.app.middleware_stack = old_stack
        provider.shutdown()


@pytest.mark.asyncio
async def test_http_source_publishes_private_declaration_and_closes_on_database_failure():
    import main
    from addp_common.client.module_registry import ProcessMetricsDeclaration
    source=Mock(declaration=ProcessMetricsDeclaration(endpoint='https://localhost:19445/metrics'))
    with patch.object(main,'start_http_metrics',return_value=source) as start,patch('database.init_db',new=AsyncMock(side_effect=RuntimeError('controlled database failure'))),patch.object(main.trace_runtime,'start_optional',return_value=Mock()),patch.object(main._registration,'process_metrics',None):
        with pytest.raises(RuntimeError):
            async with main.lifespan(main.app):pass
        start.assert_called_once_with(main._registration)
        assert main._registration.process_metrics is source.declaration
    source.close.assert_called_once_with()
