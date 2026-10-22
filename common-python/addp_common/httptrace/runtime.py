"""Optional, bounded HTTP tracing using an explicit OpenTelemetry provider."""
from __future__ import annotations

import contextvars
import json
import logging
import os
from pathlib import Path
import queue
import re
import ssl
import stat
import threading
import time
from uuid import UUID

import httpx
from opentelemetry import trace
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import ExportTraceServiceRequest
from opentelemetry.proto.common.v1.common_pb2 import AnyValue, KeyValue, InstrumentationScope
from opentelemetry.proto.resource.v1.resource_pb2 import Resource as WireResource
from opentelemetry.proto.trace.v1.trace_pb2 import ResourceSpans, ScopeSpans, Span as WireSpan, Status

logger = logging.getLogger("addp.httptrace")
request_id_var: contextvars.ContextVar[str] = contextvars.ContextVar("addp_request_id", default="")
_NAME = re.compile(r"[a-z][a-z0-9_-]{0,63}\Z")
_INSTANCE = re.compile(r"[A-Za-z0-9._:-]{1,100}\Z")
_ROUTE = re.compile(r"/[A-Za-z0-9_./:{}*+-]{0,255}\Z")
_METHODS = {"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE", "QUERY", "_OTHER"}
_ERRORS = {"timeout", "canceled", "_OTHER"} | {str(code) for code in range(500, 600)}
_LIMIT = 256 << 10
_NOOP = trace.NoOpTracerProvider().get_tracer("addp.http")
_lock = threading.RLock()
_current: _Runtime | None = None
_retiring: _Runtime | None = None


def _object(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("trace_deployment_invalid")
        value[key] = item
    return value


def _deployment(raw: str, module: str, role: str):
    if not raw:
        return None
    if len(raw.encode("utf-8")) > _LIMIT:
        raise ValueError("trace_deployment_invalid")
    values = json.loads(raw, object_pairs_hook=_object)
    if not isinstance(values, list) or len(values) > 1000:
        raise ValueError("trace_deployment_invalid")
    selected, seen = None, set()
    for item in values:
        if (not isinstance(item, dict) or set(item) != {"module_name", "role", "endpoint", "tls_dir"}
                or not all(isinstance(v, str) for v in item.values())):
            raise ValueError("trace_deployment_invalid")
        from urllib.parse import urlsplit
        url = urlsplit(item["endpoint"])
        key = (item["module_name"], item["role"])
        if (not _NAME.fullmatch(key[0]) or key[1] not in {"backend", "ingress"} or key in seen
                or not item["endpoint"].startswith("https://")
                or any(ord(char) <= 32 or ord(char) == 127 for char in item["endpoint"])
                or url.scheme != "https" or not url.hostname or url.username is not None
                or url.password is not None or url.path != "/v1/traces" or url.query or url.fragment
                or "?" in item["endpoint"] or "#" in item["endpoint"] or "%" in url.netloc
                or not os.path.isabs(item["tls_dir"])):
            raise ValueError("trace_deployment_invalid")
        # Reading the port also rejects malformed/overflowing explicit ports.
        _ = url.port
        seen.add(key)
        if key == (module, role):
            selected = item
    return selected


def _tls(directory: str) -> ssl.SSLContext:
    directory_path = Path(directory)
    if not directory_path.is_absolute() or os.path.normpath(directory) != directory:
        raise ValueError("trace_deployment_invalid")
    resolved = directory_path.resolve(strict=True)
    repository = Path(__file__).resolve().parents[3]
    if resolved == repository or repository in resolved.parents:
        raise ValueError("trace_deployment_invalid")
    info = directory_path.lstat()
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid()
            or stat.S_IMODE(info.st_mode) & 0o077
            or set(os.listdir(directory)) != {"ca.crt", "client.crt", "client.key"}):
        raise ValueError("trace_deployment_invalid")
    contents = {}
    for name in ("ca.crt", "client.crt", "client.key"):
        file_path = directory_path / name
        descriptor = os.open(file_path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            info = os.fstat(descriptor)
            if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.geteuid()
                    or not 1 <= info.st_size <= _LIMIT
                    or (name == "client.key" and stat.S_IMODE(info.st_mode) != 0o600)):
                raise ValueError("trace_deployment_invalid")
            with os.fdopen(descriptor, "rb", closefd=False) as stream:
                contents[name] = stream.read(_LIMIT + 1)
            if len(contents[name]) > _LIMIT:
                raise ValueError("trace_deployment_invalid")
        finally:
            os.close(descriptor)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_verify_locations(cadata=contents["ca.crt"].decode("ascii"))
    # The private owner-only directory and no-follow checks prevent credential aliases.
    context.load_cert_chain(directory_path / "client.crt", directory_path / "client.key")
    return context


def _safe_attributes(attributes):
    safe = {}
    for key, value in (attributes or {}).items():
        if key == "http.request.method" and isinstance(value, str) and value in _METHODS:
            safe[key] = value
        elif key == "http.route" and isinstance(value, str) and (value == "" or _ROUTE.fullmatch(value)):
            safe[key] = value
        elif key == "http.response.status_code" and type(value) is int and 100 <= value <= 599:
            safe[key] = value
        elif key in {"addp.http.server_failure", "addp.http.routine"} and type(value) is bool:
            safe[key] = value
        elif key == "error.type" and isinstance(value, str) and value in _ERRORS:
            safe[key] = value
    return safe


def _key_value(key, value):
    if type(value) is bool:
        item = AnyValue(bool_value=value)
    elif type(value) is int:
        item = AnyValue(int_value=value)
    else:
        item = AnyValue(string_value=value)
    return KeyValue(key=key, value=item)


def _wire_span(span):
    if span.kind not in {trace.SpanKind.SERVER, trace.SpanKind.CLIENT}:
        raise ValueError("trace_span_invalid")
    context = span.get_span_context()
    attributes = _safe_attributes(span.attributes)
    method = attributes.get("http.request.method", "_OTHER")
    route = attributes.get("http.route", "") if span.kind == trace.SpanKind.SERVER else ""
    parent = span.parent
    # Never serialize user span names, event text, links, tracestate or descriptions.
    return WireSpan(
        trace_id=context.trace_id.to_bytes(16, "big"),
        span_id=context.span_id.to_bytes(8, "big"),
        parent_span_id=parent.span_id.to_bytes(8, "big") if parent and parent.is_valid else b"",
        name=method + (" " + route if route else ""),
        kind=span.kind.value + 1,
        start_time_unix_nano=span.start_time,
        end_time_unix_nano=span.end_time,
        attributes=[_key_value(k, v) for k, v in attributes.items()],
        status=Status(code=Status.STATUS_CODE_ERROR if span.status.status_code == trace.StatusCode.ERROR else Status.STATUS_CODE_UNSET),
        flags=int(context.trace_flags),
    )


class _Processor:
    """One batch worker and at most one in-flight network daemon; never retry."""

    def __init__(self, client, endpoint, identity):
        self.client, self.endpoint, self.identity = client, endpoint, identity
        self.pending = queue.Queue(maxsize=2048)
        self.condition = threading.Lock()
        self.closed = False
        self.deadline = None
        self.dropped = self.export_failed = self.exported = self.outstanding = 0
        self.network = None
        self.network_done = None
        self.dispose_client = False
        self.wake = threading.Event()
        self.worker = threading.Thread(target=self._run, name="addp-trace-batch", daemon=True)
        self.worker.start()

    def on_start(self, span, parent_context=None):
        pass

    def _on_ending(self, span):
        # SDK 1.45.1 calls this non-exporting pre-end hook on every processor.
        pass

    def on_end(self, span):
        with self.condition:
            if self.closed:
                self.dropped += 1
                return
            try:
                self.pending.put_nowait(_wire_span(span))
                self.outstanding += 1
            except Exception:
                # Telemetry projection, including malformed/custom SDK spans,
                # cannot change the business operation ending this span.
                self.dropped += 1
        if self.pending.qsize() >= 256:
            self.wake.set()

    def snapshot(self):
        with self.condition:
            return {"queued": self.pending.qsize(), "dropped": self.dropped,
                    "export_failed": self.export_failed, "exported": self.exported}

    def _network(self, payload, done, result):
        try:
            with self.client.stream("POST", self.endpoint, content=payload,
                                    headers={"Content-Type": "application/x-protobuf", "Accept-Encoding": "identity"}) as response:
                result.append(response.status_code == 200)
        except Exception:
            result.append(False)
        finally:
            done.set()
            if self.dispose_client:
                self.client.close()

    def _send(self, batch):
        try:
            payload = ExportTraceServiceRequest(resource_spans=[ResourceSpans(
                resource=WireResource(attributes=[_key_value(k, v) for k, v in self.identity.items()]),
                scope_spans=[ScopeSpans(scope=InstrumentationScope(name="addp.http"), spans=batch)],
            )]).SerializeToString()
        except Exception:
            with self.condition:
                self.export_failed += len(batch)
                self.outstanding -= len(batch)
            return True
        done, result = threading.Event(), []
        network = threading.Thread(target=self._network, args=(payload, done, result), name="addp-trace-network", daemon=True)
        self.network = network
        self.network_done = done
        network.start()
        timeout = 2.0 if self.deadline is None else max(0.0, min(2.0, self.deadline - time.monotonic()))
        success = done.wait(timeout) and bool(result and result[0])
        with self.condition:
            if success:
                self.exported += len(batch)
            else:
                self.export_failed += len(batch)
            self.outstanding -= len(batch)
        # A late result never becomes success. Do not create another request while
        # this one is alive, even if a peer drips headers beyond HTTPX's read timeout.
        while not done.is_set():
            if self.deadline is not None and time.monotonic() >= self.deadline:
                return False
            done.wait(0.02)
        return True

    def _run(self):
        try:
            while True:
                self.wake.wait(1.0)
                self.wake.clear()
                while not self.pending.empty():
                    if self.deadline is not None and time.monotonic() >= self.deadline:
                        return
                    batch = []
                    for _ in range(256):
                        try:
                            batch.append(self.pending.get_nowait())
                        except queue.Empty:
                            break
                    if batch and not self._send(batch):
                        return
                    if not self.closed and self.pending.qsize() < 256:
                        break
                if self.closed and self.pending.empty():
                    return
        finally:
            with self.condition:
                while not self.pending.empty():
                    self.pending.get_nowait()
                    self.dropped += 1
                    self.outstanding -= 1
            # HTTPX does not expose an active socket cancellation API. Only close
            # when there is no in-flight network operation; its own timeout remains.
            self.dispose_client = True
            if self.network_done is None or self.network_done.is_set():
                self.client.close()

    def shutdown(self):
        with self.condition:
            if not self.closed:
                self.closed = True
                self.deadline = time.monotonic() + 3.0
        self.wake.set()
        self.worker.join(max(0.0, self.deadline - time.monotonic()))

    def force_flush(self, timeout_millis=30000):
        self.wake.set()
        until = time.monotonic() + min(timeout_millis / 1000, 3.0)
        while time.monotonic() < until:
            with self.condition:
                if self.outstanding == 0:
                    return True
            time.sleep(0.005)
        with self.condition:
            return self.outstanding == 0


class _Runtime:
    def __init__(self, registration, deployment):
        # The SDK evaluates some OTEL_* variables while importing. Delay that
        # optional dependency until inside StartOptional's safe failure boundary:
        # invalid SDK environment must never break a disabled application's import.
        from opentelemetry.sdk.resources import Resource
        from opentelemetry.sdk.trace import SpanLimits, TracerProvider
        from opentelemetry.sdk.trace.sampling import ALWAYS_ON

        identity = {"service.name": registration.module_name, "service.instance.id": registration.instance_id,
                    "addp.role": registration.role}
        provider = client = processor = None
        try:
            # Provider/tracer setup can reject SDK configuration. Complete it
            # before creating a client or starting an owned processor thread.
            provider = TracerProvider(sampler=ALWAYS_ON, resource=Resource(identity), shutdown_on_exit=False,
                                      span_limits=SpanLimits(max_attributes=32, max_span_attributes=32,
                                                             max_attribute_length=256, max_span_attribute_length=256,
                                                             max_events=0, max_event_attributes=0,
                                                             max_links=0, max_link_attributes=0))
            # Pinned SDK 1.45.1 reads OTEL_SDK_DISABLED unconditionally in its
            # constructor. Override this explicit instance, never global state.
            provider._disabled = False
            tracer = provider.get_tracer("addp.http")
            client = httpx.Client(verify=_tls(deployment["tls_dir"]), trust_env=False, follow_redirects=False,
                                  timeout=2.0, limits=httpx.Limits(max_connections=1, max_keepalive_connections=0))
            processor = _Processor(client, deployment["endpoint"], identity)
            provider.add_span_processor(processor)
        except Exception:
            # Shutdown every successfully-created resource even when a later
            # initialization step fails. No cleanup exception escapes the soft path.
            for resource in (processor, provider):
                if resource is not None:
                    try:
                        resource.shutdown()
                    except Exception:
                        pass
            if client is not None:
                try:
                    client.close()
                except Exception:
                    pass
            raise
        self.provider, self.processor, self.tracer = provider, processor, tracer


def start_optional(registration):
    """Start with the actual registration identity; configuration is a soft dependency."""
    global _current, _retiring
    if os.environ.get("ADDP_OBSERVABILITY_TRACES_ENABLED") != "true":
        return lambda: None
    try:
        if (not _NAME.fullmatch(registration.module_name) or registration.role != "backend"
                or not _INSTANCE.fullmatch(registration.instance_id)):
            raise ValueError("trace_deployment_invalid")
        deployment = _deployment(os.environ.get("ADDP_TRACE_DEPLOYMENTS", ""), registration.module_name, registration.role)
        if deployment is None:
            return lambda: None
        with _lock:
            if _retiring is not None:
                processor = _retiring.processor
                if processor.worker.is_alive() or (processor.network is not None and processor.network.is_alive()):
                    logger.warning("trace_already_started")
                    return lambda: None
                _retiring = None
            if _current is not None:
                logger.warning("trace_already_started")
                return lambda: None
            runtime = _Runtime(registration, deployment)
            _current = runtime
    except Exception:
        logger.warning("trace_deployment_invalid")
        return lambda: None
    close_lock = threading.Lock()
    closed = False

    def close():
        nonlocal closed
        global _current, _retiring
        with close_lock:
            if closed:
                return
            closed = True
        with _lock:
            if _current is runtime:
                _current = None
                _retiring = runtime
        try:
            runtime.provider.shutdown()
        except Exception:
            logger.warning("trace_shutdown_failed")
    return close


def enabled() -> bool:
    return _current is not None


def tracer():
    runtime = _current
    return runtime.tracer if runtime is not None else _NOOP


def snapshot() -> dict[str, int]:
    runtime = _current
    return runtime.processor.snapshot() if runtime is not None else {"queued": 0, "dropped": 0, "export_failed": 0, "exported": 0}


def correlation() -> dict[str, str]:
    values = {}
    request_id = request_id_var.get()
    try:
        parsed = UUID(request_id)
        if parsed.int and str(parsed) == request_id:
            values["request_id"] = request_id
    except (ValueError, AttributeError, TypeError):
        pass
    span = trace.get_current_span()
    context = span.get_span_context()
    if span.is_recording() and context.is_valid:
        values["trace_id"] = f"{context.trace_id:032x}"
        values["span_id"] = f"{context.span_id:016x}"
    return values
