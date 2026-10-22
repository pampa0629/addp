"""The shared, streaming-preserving ASGI HTTP trace boundary."""
from __future__ import annotations

import asyncio
import uuid

from opentelemetry import trace
from opentelemetry.context import Context
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

from . import runtime


_METHODS = {"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "CONNECT", "TRACE", "QUERY"}
_PROPAGATOR = TraceContextTextMapPropagator()
_ROUTINE = {"/health/live", "/health/ready", "/api/v1/system/runtime/modules/heartbeat"}


def _method(value: str) -> str:
    return value if value in _METHODS else "_OTHER"


def _routine(path: str) -> bool:
    return path in _ROUTINE


def _failure_type(error: BaseException) -> str:
    if isinstance(error, (asyncio.CancelledError, GeneratorExit)):
        return "canceled"
    if isinstance(error, TimeoutError):
        return "timeout"
    return "_OTHER"


def _canonical_request_id(value: str) -> bool:
    try:
        parsed = uuid.UUID(value)
        return bool(parsed.int) and str(parsed) == value
    except (ValueError, AttributeError, TypeError):
        return False


def _request_id(headers) -> str:
    values = [value for key, value in headers if key.lower() == b"x-request-id"]
    if len(values) == 1:
        try:
            value = values[0].decode("ascii")
            if _canonical_request_id(value):
                return value
        except UnicodeDecodeError:
            pass
    return str(uuid.uuid4())


def _parent(headers) -> Context:
    values = [value for key, value in headers if key.lower() == b"traceparent"]
    if len(values) != 1:
        return Context()
    try:
        value = values[0].decode("ascii")
    except UnicodeDecodeError:
        return Context()
    # Extract only this header into an empty context. Neither baggage nor the
    # caller's tracestate (or an unrelated local span) crosses this boundary.
    return _PROPAGATOR.extract({"traceparent": value}, context=Context())


class TraceMiddleware:
    """Observe one HTTP exchange without reading or buffering its body."""

    def __init__(self, app, module: str) -> None:
        self.app = app
        self.module = module

    async def __call__(self, scope, receive, send) -> None:
        if scope.get("type") != "http":
            await self.app(scope, receive, send)
            return

        headers = scope.get("headers", [])
        request_id = _request_id(headers)
        # Work on a new scope so no untrusted state is restored by an outer
        # middleware. The request ID is available even when tracing is off.
        scope = dict(scope)
        scope["headers"] = [
            (key, value) for key, value in headers
            if key.lower() not in {b"baggage", b"tracestate", b"x-request-id"}
        ] + [(b"x-request-id", request_id.encode("ascii"))]
        method = _method(scope.get("method", ""))
        span = (
            runtime.tracer().start_span(
                method,
                context=_parent(headers),
                kind=trace.SpanKind.SERVER,
                attributes={"http.request.method": method},
            )
            if runtime.enabled() else trace.INVALID_SPAN
        )
        token = runtime.request_id_var.set(request_id)
        completed = False
        finished = False
        status = 0
        failure = ""

        def finish() -> None:
            nonlocal finished
            if finished:
                return
            finished = True
            route = getattr(scope.get("route"), "path", "")
            if not isinstance(route, str) or len(route.encode("utf-8")) > 1024 or any(c in route for c in "\x00\r\n?#"):
                route = ""
            error_type = failure or (str(status) if status >= 500 else "")
            span.update_name(method + " " + route)
            span.set_attributes({
                "http.route": route,
                "http.response.status_code": status,
                "addp.http.server_failure": bool(error_type),
                "addp.http.routine": _routine(route),
            })
            if error_type:
                span.set_attribute("error.type", error_type)
                span.set_status(trace.Status(trace.StatusCode.ERROR))
            span.end()

        async def observed_receive():
            nonlocal failure
            message = await receive()
            if message.get("type") == "http.disconnect" and not completed:
                failure = "canceled"
            return message

        async def observed_send(message):
            nonlocal status, completed, failure
            if message.get("type") == "http.response.start":
                candidate = message.get("status", 0)
                status = candidate if isinstance(candidate, int) and 100 <= candidate <= 599 else 0
                message = dict(message)
                message["headers"] = [
                    (key, value) for key, value in message.get("headers", [])
                    if key.lower() != b"x-request-id"
                ] + [(b"x-request-id", request_id.encode("ascii"))]
            try:
                await send(message)
            except BaseException as error:
                if not completed:
                    failure = _failure_type(error)
                    finish()
                raise
            if message.get("type") == "http.response.body" and not message.get("more_body", False):
                completed = True
                finish()

        try:
            with trace.use_span(span, end_on_exit=False, record_exception=False, set_status_on_exception=False):
                try:
                    await self.app(scope, observed_receive, observed_send)
                except BaseException as error:
                    if not completed:
                        failure = _failure_type(error)
                    raise
                finally:
                    if not completed and not failure:
                        failure = "_OTHER"
                    finish()
        finally:
            runtime.request_id_var.reset(token)
