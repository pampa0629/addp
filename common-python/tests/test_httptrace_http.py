"""Streaming, cancellation and trust boundaries of the explicit HTTP SDK."""
import asyncio
from contextlib import contextmanager
from types import SimpleNamespace
import os
import unittest
from unittest.mock import patch
import uuid

import httpx
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.sdk.trace.sampling import ALWAYS_ON

from addp_common.httptrace import runtime
from addp_common.httptrace.client import trace_sync_transport, trace_transport
from addp_common.httpmetrics.server import HTTPObservationMiddleware


@contextmanager
def tracing(enabled=True):
    exporter = InMemorySpanExporter()
    provider = TracerProvider(sampler=ALWAYS_ON)
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    with patch.object(runtime, "enabled", return_value=enabled), patch.object(runtime, "tracer", return_value=provider.get_tracer("addp.http")):
        try:
            yield exporter
        finally:
            provider.shutdown()


def scope(headers=(), method="GET", path="/private/value"):
    return {"type": "http", "method": method, "path": path, "query_string": b"token=private-query", "headers": list(headers)}


async def receive():
    return {"type": "http.request", "body": b"private-body", "more_body": False}


class AsyncChunks(httpx.AsyncByteStream):
    def __init__(self, error=None):
        self.error, self.reads, self.closed = error, 0, False

    async def __aiter__(self):
        self.reads += 1
        yield b"first"
        if self.error:
            raise self.error
        self.reads += 1
        yield b"second"

    async def aclose(self):
        self.closed = True


class SyncChunks(httpx.SyncByteStream):
    def __init__(self, error=None):
        self.error, self.closed = error, False

    def __iter__(self):
        yield b"first"
        if self.error:
            raise self.error
        yield b"second"

    def close(self):
        self.closed = True


class HTTPTraceTests(unittest.IsolatedAsyncioTestCase):
    async def test_default_transports_do_not_reenable_environment_ca_overrides(self):
        # A nonexistent environment CA used to fail in SSLContext construction,
        # even though all shared HTTP clients explicitly set trust_env=False.
        with patch.dict(os.environ, {"SSL_CERT_FILE": "/nonexistent/addp-trace-ca.pem", "SSL_CERT_DIR": "/nonexistent/addp-trace-ca-dir"}):
            asynchronous = trace_transport("https://system")
            synchronous = trace_sync_transport("https://system")
            await asynchronous.aclose()
            synchronous.close()

    async def test_asgi_parent_stream_lifetime_and_safe_projection(self):
        request_id = str(uuid.uuid4())
        partial = asyncio.Event()
        resume = asyncio.Event()
        seen, sent = {}, []

        async def app(current, _receive, send):
            seen["headers"] = current["headers"]
            seen["request_id"] = runtime.request_id_var.get()
            seen["context"] = trace.get_current_span().get_span_context()
            current["route"] = SimpleNamespace(path="/items/{item_id}")
            await send({"type": "http.response.start", "status": 200, "headers": []})
            await send({"type": "http.response.body", "body": b"first", "more_body": True})
            partial.set()
            await resume.wait()
            await send({"type": "http.response.body", "body": b"second"})
            seen["ended_in_background"] = not trace.get_current_span().is_recording()

        async def send(message):
            sent.append(message)

        with tracing() as exporter:
            task = asyncio.create_task(HTTPObservationMiddleware(app, "agent")(
                scope([(b"traceparent", b"00-" + b"1" * 32 + b"-" + b"2" * 16 + b"-00"),
                       (b"tracestate", b"private=secret"), (b"baggage", b"user=secret"),
                       (b"x-request-id", request_id.encode())]), receive, send))
            await partial.wait()
            self.assertEqual(exporter.get_finished_spans(), ())
            self.assertEqual(sent[1]["body"], b"first")
            resume.set()
            await task
            spans = exporter.get_finished_spans()
        self.assertEqual(len(spans), 1)
        span = spans[0]
        self.assertEqual(span.context.trace_id, int("1" * 32, 16))
        self.assertEqual(span.parent.span_id, int("2" * 16, 16))
        self.assertEqual(span.name, "GET /items/{item_id}")
        self.assertEqual(span.attributes["http.route"], "/items/{item_id}")
        self.assertFalse(span.attributes["addp.http.server_failure"])
        self.assertTrue(seen["ended_in_background"])
        self.assertEqual(seen["request_id"], request_id)
        self.assertEqual(dict(sent[0]["headers"])[b"x-request-id"].decode(), request_id)
        self.assertNotIn(b"baggage", dict(seen["headers"]))
        self.assertNotIn(b"tracestate", dict(seen["headers"]))
        self.assertNotIn("private", str(span.attributes))
        self.assertEqual(span.events, ())
        self.assertEqual(runtime.request_id_var.get(), "")

    async def test_asgi_rejections_failures_cancel_and_send_errors(self):
        for status, error, send_error, category in [
            (403, None, None, None), (503, None, None, "503"),
            (200, TimeoutError("private"), None, "timeout"),
            (200, asyncio.CancelledError("private"), None, "canceled"),
            (200, None, OSError("private"), "_OTHER"),
        ]:
            with self.subTest(status=status, error=type(error), send_error=send_error), tracing() as exporter:
                async def app(current, _receive, send):
                    current["route"] = SimpleNamespace(path="/health/ready")
                    await send({"type": "http.response.start", "status": status})
                    if error is not None:
                        raise error
                    await send({"type": "http.response.body", "body": b"private"})

                async def send(message):
                    if send_error and message["type"] == "http.response.body":
                        raise send_error

                if error is not None or send_error is not None:
                    with self.assertRaises(type(error if error is not None else send_error)):
                        await HTTPObservationMiddleware(app, "copilot")(scope(), receive, send)
                else:
                    await HTTPObservationMiddleware(app, "copilot")(scope(), receive, send)
                span, = exporter.get_finished_spans()
                self.assertEqual(span.attributes["addp.http.server_failure"], category is not None)
                self.assertEqual(span.attributes.get("error.type"), category)
                self.assertTrue(span.attributes["addp.http.routine"])
                self.assertNotIn("private", str(span.attributes))
                self.assertEqual(span.events, ())
                self.assertEqual(runtime.request_id_var.get(), "")

    async def test_asgi_parallel_contexts_and_invalid_parents_do_not_leak(self):
        barrier = asyncio.Event()
        contexts = []

        async def app(current, _receive, send):
            own = runtime.request_id_var.get(), trace.get_current_span().get_span_context().trace_id
            contexts.append(own)
            if len(contexts) == 2:
                barrier.set()
            await barrier.wait()
            self.assertEqual((runtime.request_id_var.get(), trace.get_current_span().get_span_context().trace_id), own)
            await send({"type": "http.response.start", "status": 404})
            await send({"type": "http.response.body"})

        async def send(_message):
            pass

        with tracing() as exporter:
            await asyncio.gather(*[
                HTTPObservationMiddleware(app, "agent")(scope([(b"traceparent", b"00-" + b"0" * 32 + b"-" + b"2" * 16 + b"-01"),
                                                     (b"x-request-id", b"private-injection\r\n")], method="PRIVATE"), receive, send)
                for _ in range(2)
            ])
            spans = exporter.get_finished_spans()
        self.assertEqual(len({value[0] for value in contexts}), 2)
        self.assertEqual(len({value[1] for value in contexts}), 2)
        for span in spans:
            self.assertIsNone(span.parent)
            self.assertEqual(span.attributes["http.request.method"], "_OTHER")
            self.assertEqual(span.attributes["http.route"], "")

    async def test_disabled_sdk_still_keeps_request_id_and_non_http_is_untouched(self):
        messages = []

        async def app(current, _receive, send):
            if current["type"] == "websocket":
                self.assertIs(current, websocket)
                return
            await send({"type": "http.response.start", "status": 503})
            await send({"type": "http.response.body", "body": b"not ready"})

        async def send(message):
            messages.append(message)

        websocket = {"type": "websocket"}
        with tracing(False) as exporter:
            wrapper = HTTPObservationMiddleware(app, "agent")
            await wrapper(scope(), receive, send)
            await wrapper(websocket, receive, send)
            self.assertEqual(exporter.get_finished_spans(), ())
        self.assertEqual(messages[-1]["body"], b"not ready")
        self.assertNotEqual(uuid.UUID(dict(messages[0]["headers"])[b"x-request-id"].decode()).int, 0)

    async def test_canceling_running_asgi_task_finishes_without_fabricating_status(self):
        started = asyncio.Event()
        async def app(_scope, _receive, _send):
            started.set()
            await asyncio.Event().wait()
        async def send(_message):
            self.fail("instrumentation must not manufacture a response")
        with tracing() as exporter:
            task = asyncio.create_task(HTTPObservationMiddleware(app, "agent")(scope(), receive, send))
            await started.wait()
            task.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await task
            span, = exporter.get_finished_spans()
        self.assertEqual(span.attributes["error.type"], "canceled")
        self.assertEqual(span.attributes["http.response.status_code"], 0)
        self.assertEqual(runtime.request_id_var.get(), "")

    async def test_async_client_does_not_prefetch_stream_and_close_ends_once(self):
        source = AsyncChunks()
        requests = []

        async def handler(request):
            requests.append(request)
            return httpx.Response(200, stream=source)

        with tracing() as exporter:
            transport = trace_transport("https://system:443", httpx.MockTransport(handler))
            request = httpx.Request("GET", "https://system/private?token=secret", headers={"baggage": "secret", "tracestate": "secret", "x-request-id": "forged"}, extensions={"timeout": {"read": 7}})
            response = await transport.handle_async_request(request)
            self.assertEqual(source.reads, 0)
            self.assertEqual(exporter.get_finished_spans(), ())
            self.assertEqual([chunk async for chunk in response.stream], [b"first", b"second"])
            await response.aclose()
            await transport.aclose()
            span, = exporter.get_finished_spans()
        self.assertTrue(source.closed)
        self.assertEqual(request.headers["baggage"], "secret")
        self.assertEqual(requests[0].extensions["timeout"], {"read": 7})
        self.assertIn("traceparent", requests[0].headers)
        self.assertNotIn("baggage", requests[0].headers)
        self.assertNotIn("private", str(span.attributes))
        self.assertNotIn("secret", str(span.attributes))

    async def test_async_client_cancel_read_and_transport_timeout_have_fixed_categories(self):
        for error, category in [(asyncio.CancelledError("secret"), "canceled"), (OSError("secret"), "_OTHER")]:
            with self.subTest(error=type(error)), tracing() as exporter:
                source = AsyncChunks(error)
                transport = trace_transport("https://system", httpx.MockTransport(lambda _request: httpx.Response(200, stream=source)))
                response = await transport.handle_async_request(httpx.Request("GET", "https://system"))
                with self.assertRaises(type(error)):
                    async for _ in response.stream:
                        pass
                await response.aclose()
                span, = exporter.get_finished_spans()
                self.assertEqual(span.attributes["error.type"], category)
                self.assertEqual(span.events, ())
        async def timeout(_request):
            raise httpx.ReadTimeout("private secret")
        with tracing() as exporter:
            transport = trace_transport("https://system", httpx.MockTransport(timeout))
            with self.assertRaises(httpx.ReadTimeout):
                await transport.handle_async_request(httpx.Request("GET", "https://system"))
            span, = exporter.get_finished_spans()
        self.assertEqual(span.attributes["error.type"], "timeout")
        self.assertNotIn("private", str(span.attributes))

    async def test_client_close_without_read_does_not_drain_and_disabled_only_sends_canonical_request_id(self):
        source = AsyncChunks()
        with tracing() as exporter:
            transport = trace_transport("https://system", httpx.MockTransport(lambda _request: httpx.Response(200, stream=source)))
            response = await transport.handle_async_request(httpx.Request("GET", "https://system"))
            await response.aclose()
            self.assertEqual(source.reads, 0)
            self.assertTrue(source.closed)
            self.assertEqual(len(exporter.get_finished_spans()), 1)
        for candidate in (str(uuid.uuid4()), "private-invalid-id"):
            seen = []
            with self.subTest(request_id=candidate), tracing(False) as exporter:
                token = runtime.request_id_var.set(candidate)
                try:
                    transport = trace_transport("https://system", httpx.MockTransport(lambda request: seen.append(request) or httpx.Response(200)))
                    response = await transport.handle_async_request(httpx.Request("GET", "https://system", headers={"traceparent": "forged"}))
                    await response.aclose()
                finally:
                    runtime.request_id_var.reset(token)
                self.assertEqual(exporter.get_finished_spans(), ())
            self.assertNotIn("traceparent", seen[0].headers)
            self.assertEqual(seen[0].headers.get("x-request-id"), candidate if candidate != "private-invalid-id" else None)

    async def test_redirect_each_origin_strips_all_platform_headers(self):
        seen = []
        async def handler(request):
            seen.append(request)
            if request.url.host == "system":
                return httpx.Response(302, headers={"location": "https://external/private"})
            return httpx.Response(200, content=b"ok")
        with tracing() as exporter:
            token = runtime.request_id_var.set(str(uuid.uuid4()))
            try:
                async with httpx.AsyncClient(transport=trace_transport("https://system", httpx.MockTransport(handler)), follow_redirects=True) as client:
                    await client.get("https://system", headers={"traceparent": "forged", "baggage": "secret", "tracestate": "secret"})
            finally:
                runtime.request_id_var.reset(token)
            self.assertEqual(len(exporter.get_finished_spans()), 1)
        self.assertIn("traceparent", seen[0].headers)
        self.assertIn("x-request-id", seen[0].headers)
        for name in ("traceparent", "tracestate", "baggage", "x-request-id"):
            self.assertNotIn(name, seen[1].headers)

    async def test_ended_server_context_is_not_revived_for_background_http(self):
        requests = []
        transport = trace_transport("https://system", httpx.MockTransport(lambda request: requests.append(request) or httpx.Response(200)))
        async def app(_scope, _receive, send):
            await send({"type": "http.response.start", "status": 200})
            await send({"type": "http.response.body"})
            response = await transport.handle_async_request(httpx.Request("GET", "https://system"))
            await response.aclose()
        async def send(_message):
            pass
        with tracing() as exporter:
            await HTTPObservationMiddleware(app, "agent")(scope(), receive, send)
            spans = exporter.get_finished_spans()
        self.assertEqual(len(spans), 1)
        self.assertEqual(spans[0].kind, trace.SpanKind.SERVER)
        self.assertNotIn("traceparent", requests[0].headers)
        self.assertNotIn("x-request-id", requests[0].headers)


class SyncHTTPTraceTests(unittest.TestCase):
    def test_sync_stream_close_and_read_failure_preserve_response(self):
        for error, status in [(None, 403), (OSError("private"), 200), (None, 503)]:
            with self.subTest(error=error, status=status), tracing() as exporter:
                source = SyncChunks(error)
                transport = trace_sync_transport("https://system", httpx.MockTransport(lambda _request: httpx.Response(status, stream=source)))
                response = transport.handle_request(httpx.Request("GET", "https://system"))
                self.assertEqual(exporter.get_finished_spans(), ())
                if error is None:
                    self.assertEqual(list(response.stream), [b"first", b"second"])
                else:
                    with self.assertRaises(OSError):
                        list(response.stream)
                response.close()
                transport.close()
                span, = exporter.get_finished_spans()
                self.assertEqual(span.attributes["addp.http.server_failure"], error is not None or status >= 500)
                self.assertTrue(source.closed)

    def test_invalid_origin_and_port_change_cannot_receive_platform_headers(self):
        for origin, target in [("https://system", "https://system:444"), ("http://system", "https://system"), ("https://user:secret@system", "https://system")]:
            seen = []
            with self.subTest(origin=origin, target=target), tracing() as exporter:
                transport = trace_sync_transport(origin, httpx.MockTransport(lambda request: seen.append(request) or httpx.Response(200)))
                response = transport.handle_request(httpx.Request("GET", target, headers={name: "private" for name in ("traceparent", "tracestate", "baggage", "x-request-id")}))
                response.close()
                self.assertEqual(exporter.get_finished_spans(), ())
            for name in ("traceparent", "tracestate", "baggage", "x-request-id"):
                self.assertNotIn(name, seen[0].headers)
