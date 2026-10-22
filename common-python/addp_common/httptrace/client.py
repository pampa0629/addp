"""Explicit HTTPX transports for registered platform origins only."""
from __future__ import annotations

import threading

import httpx
from opentelemetry import trace
from opentelemetry.context import Context

from . import runtime
from .server import _canonical_request_id, _failure_type, _method, _routine


_HEADERS = ("traceparent", "tracestate", "baggage", "x-request-id")


def _origin(value: str | httpx.URL):
    try:
        url = httpx.URL(value)
    except (httpx.InvalidURL, ValueError, TypeError):
        return None
    if url.scheme not in {"http", "https"} or not url.host or url.userinfo:
        return None
    return url.scheme, url.host, url.port or (443 if url.scheme == "https" else 80)


class _End:
    def __init__(self, span) -> None:
        self.span = span
        self.status = 0
        self.done = False
        self.lock = threading.Lock()

    def __call__(self, error: BaseException | None = None) -> None:
        with self.lock:
            if self.done:
                return
            self.done = True
        category = ("timeout" if isinstance(error, httpx.TimeoutException) else _failure_type(error)) if error is not None else (str(self.status) if self.status >= 500 else "")
        self.span.set_attributes({"http.response.status_code": self.status, "addp.http.server_failure": bool(category)})
        if category:
            self.span.set_attribute("error.type", category)
            self.span.set_status(trace.Status(trace.StatusCode.ERROR))
        self.span.end()


def _prepare(request: httpx.Request, origin):
    headers = request.headers.copy()
    for name in _HEADERS:
        headers.pop(name, None)
    active = None
    if origin is not None and _origin(request.url) == origin:
        parent = trace.get_current_span()
        # ASGI background tasks can inherit a request context after the final
        # body. Do not revive that ended HTTP operation or its correlation ID.
        ended = parent.get_span_context().is_valid and not parent.is_recording()
        if not ended:
            request_id = runtime.request_id_var.get()
            if _canonical_request_id(request_id):
                headers["x-request-id"] = request_id
            if runtime.enabled():
                method = _method(request.method)
                span = runtime.tracer().start_span(
                    method, context=None if parent.is_recording() else Context(),
                    kind=trace.SpanKind.CLIENT,
                    attributes={"http.request.method": method, "addp.http.routine": _routine(request.url.path)},
                )
                context = span.get_span_context()
                headers["traceparent"] = f"00-{context.trace_id:032x}-{context.span_id:016x}-{int(context.trace_flags):02x}"
                active = _End(span)
    copy = httpx.Request(request.method, request.url, headers=headers, stream=request.stream, extensions=request.extensions.copy())
    return copy, active


class _AsyncStream(httpx.AsyncByteStream):
    def __init__(self, stream, end: _End) -> None:
        self.stream, self.end = stream, end

    async def __aiter__(self):
        iterator = self.stream.__aiter__()
        try:
            while True:
                with trace.use_span(self.end.span, end_on_exit=False, record_exception=False, set_status_on_exception=False):
                    chunk = await iterator.__anext__()
                yield chunk
        except StopAsyncIteration:
            self.end()
        except BaseException as error:
            self.end(error)
            raise

    async def aclose(self) -> None:
        try:
            await self.stream.aclose()
        except BaseException as error:
            self.end(error)
            raise
        else:
            self.end()


class _SyncStream(httpx.SyncByteStream):
    def __init__(self, stream, end: _End) -> None:
        self.stream, self.end = stream, end

    def __iter__(self):
        iterator = iter(self.stream)
        try:
            while True:
                with trace.use_span(self.end.span, end_on_exit=False, record_exception=False, set_status_on_exception=False):
                    chunk = next(iterator)
                yield chunk
        except StopIteration:
            self.end()
        except BaseException as error:
            self.end(error)
            raise

    def close(self) -> None:
        try:
            self.stream.close()
        except BaseException as error:
            self.end(error)
            raise
        else:
            self.end()


class _AsyncTransport(httpx.AsyncBaseTransport):
    def __init__(self, origin: str, transport) -> None:
        self.origin = _origin(origin)
        self.transport = transport if transport is not None else httpx.AsyncHTTPTransport(trust_env=False)

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        request, end = _prepare(request, self.origin)
        if end is None:
            return await self.transport.handle_async_request(request)
        try:
            with trace.use_span(end.span, end_on_exit=False, record_exception=False, set_status_on_exception=False):
                response = await self.transport.handle_async_request(request)
            end.status = response.status_code
            if response.is_stream_consumed or response.is_closed:
                end()
            else:
                response.stream = _AsyncStream(response.stream, end)
            return response
        except BaseException as error:
            end(error)
            raise

    async def aclose(self) -> None:
        await self.transport.aclose()


class _SyncTransport(httpx.BaseTransport):
    def __init__(self, origin: str, transport) -> None:
        self.origin = _origin(origin)
        self.transport = transport if transport is not None else httpx.HTTPTransport(trust_env=False)

    def handle_request(self, request: httpx.Request) -> httpx.Response:
        request, end = _prepare(request, self.origin)
        if end is None:
            return self.transport.handle_request(request)
        try:
            with trace.use_span(end.span, end_on_exit=False, record_exception=False, set_status_on_exception=False):
                response = self.transport.handle_request(request)
            end.status = response.status_code
            if response.is_stream_consumed or response.is_closed:
                end()
            else:
                response.stream = _SyncStream(response.stream, end)
            return response
        except BaseException as error:
            end(error)
            raise

    def close(self) -> None:
        self.transport.close()


def trace_transport(origin: str, transport: httpx.AsyncBaseTransport | None = None) -> httpx.AsyncBaseTransport:
    return _AsyncTransport(origin, transport)


def trace_sync_transport(origin: str, transport: httpx.BaseTransport | None = None) -> httpx.BaseTransport:
    return _SyncTransport(origin, transport)
