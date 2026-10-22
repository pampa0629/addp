"""Local mTLS/protobuf and deterministic bounds for the shared optional SDK."""
import asyncio
from contextlib import contextmanager
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import os
from pathlib import Path
import shutil
import ssl
import subprocess
import sys
import threading
import time
from types import SimpleNamespace
from uuid import uuid4

import httpx
from opentelemetry import trace
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import ExportTraceServiceRequest
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
import pytest

from addp_common.httptrace import runtime


def _openssl(*args):
    subprocess.run(["openssl", *map(str, args)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


@pytest.fixture(scope="module")
def certificates(tmp_path_factory):
    root = tmp_path_factory.mktemp("trace-certs")
    _openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN=trace-test-ca",
             "-keyout", root / "ca.key", "-out", root / "ca.crt")
    for name, extensions in (("server", "subjectAltName=IP:127.0.0.1,DNS:localhost\nextendedKeyUsage=serverAuth"),
                             ("client", "extendedKeyUsage=clientAuth")):
        _openssl("req", "-new", "-newkey", "rsa:2048", "-nodes", "-subj", f"/CN={name}",
                 "-keyout", root / f"{name}.key", "-out", root / f"{name}.csr")
        ext = root / f"{name}.ext"
        ext.write_text(extensions)
        _openssl("x509", "-req", "-in", root / f"{name}.csr", "-CA", root / "ca.crt", "-CAkey", root / "ca.key",
                 "-CAcreateserial", "-days", "1", "-extfile", ext, "-out", root / f"{name}.crt")
    _openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1", "-subj", "/CN=foreign",
             "-keyout", root / "foreign.key", "-out", root / "foreign.crt")
    return root


@pytest.fixture
def tls_dir(tmp_path, certificates):
    directory = tmp_path / "tls"
    directory.mkdir(mode=0o700)
    for name in ("ca.crt", "client.crt", "client.key"):
        shutil.copyfile(certificates / name, directory / name)
        (directory / name).chmod(0o600)
    return directory


@contextmanager
def _receiver(certificates, *, delay=0, status=200):
    received = []
    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            received.append((self.path, dict(self.headers), self.rfile.read(int(self.headers["Content-Length"]))))
            time.sleep(delay)
            self.send_response(status)
            if status == 302:
                self.send_header("Location", "/redirect-secret")
            self.send_header("Content-Length", "0")
            self.end_headers()
        def log_message(self, *args):
            pass
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(certificates / "server.crt", certificates / "server.key")
    context.load_verify_locations(certificates / "ca.crt")
    context.verify_mode = ssl.CERT_REQUIRED
    server.socket = context.wrap_socket(server.socket, server_side=True)
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    try:
        yield f"https://127.0.0.1:{server.server_port}/v1/traces", received
    finally:
        server.shutdown()
        server.server_close()
        worker.join(1)


@pytest.fixture(autouse=True)
def no_shared_runtime(monkeypatch):
    monkeypatch.delenv("ADDP_OBSERVABILITY_TRACES_ENABLED", raising=False)
    monkeypatch.delenv("ADDP_TRACE_DEPLOYMENTS", raising=False)
    yield
    if runtime._current is not None:
        instance = runtime._current
        runtime._current = None
        instance.provider.shutdown()


def _start(monkeypatch, tls_dir, endpoint):
    registration = SimpleNamespace(module_name="agent", role="backend", instance_id=str(uuid4()))
    monkeypatch.setenv("ADDP_OBSERVABILITY_TRACES_ENABLED", "true")
    monkeypatch.setenv("ADDP_TRACE_DEPLOYMENTS", json.dumps([{
        "module_name": "agent", "role": "backend", "endpoint": endpoint, "tls_dir": str(tls_dir),
    }]))
    return registration, runtime.start_optional(registration)


def _span(provider=None, **attributes):
    tracer = provider.get_tracer("test") if provider else runtime.tracer()
    span = tracer.start_span("SECRET-NAME", kind=trace.SpanKind.SERVER,
                             attributes={"http.request.method": "GET", "http.route": "/safe/{id}", **attributes})
    return span


def test_real_mtls_official_protobuf_identity_and_safe_projection(monkeypatch, tls_dir, certificates):
    global_provider = trace.get_tracer_provider()
    monkeypatch.setenv("OTEL_RESOURCE_ATTRIBUTES", "secret=DO-NOT-EXPORT")
    monkeypatch.setenv("OTEL_TRACES_SAMPLER", "always_off")
    monkeypatch.setenv("OTEL_SDK_DISABLED", "true")
    monkeypatch.setenv("OTEL_SPAN_ATTRIBUTE_COUNT_LIMIT", "1")
    monkeypatch.setenv("OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT", "1")
    monkeypatch.setenv("OTEL_EVENT_ATTRIBUTE_COUNT_LIMIT", "9999")
    monkeypatch.setenv("HTTPS_PROXY", "https://invalid-proxy.example")
    with _receiver(certificates) as (endpoint, received):
        registration, close = _start(monkeypatch, tls_dir, endpoint)
        assert runtime.enabled()
        instance = runtime._current
        parent = trace.SpanContext(123, 456, True, trace.TraceFlags(0), trace.TraceState([("secret", "DO-NOT-EXPORT")]))
        with trace.use_span(trace.NonRecordingSpan(parent)):
            span = _span(**{"url.full": "SECRET-URL", "error.type": "SECRET-ERROR", "http.response.status_code": 503,
                            "addp.http.server_failure": True, "http.route": "/safe/{id}"})
        span.add_event("SECRET-EVENT", {"secret": "SECRET-VALUE"})
        span.set_status(trace.Status(trace.StatusCode.ERROR, "SECRET-STATUS"))
        span.end()
        assert instance.processor.force_flush(2000)
        close()
        close()
        assert len(received) == 1
        path, headers, body = received[0]
        assert path == "/v1/traces"
        assert headers["Content-Type"] == "application/x-protobuf"
        assert "Content-Encoding" not in headers
        assert b"SECRET" not in body and b"DO-NOT-EXPORT" not in body
        request = ExportTraceServiceRequest.FromString(body)
        resource = request.resource_spans[0]
        assert {v.key: v.value.string_value for v in resource.resource.attributes} == {
            "service.name": "agent", "service.instance.id": registration.instance_id, "addp.role": "backend"}
        wire = resource.scope_spans[0].spans[0]
        assert wire.trace_id == (123).to_bytes(16, "big")
        assert wire.parent_span_id == (456).to_bytes(8, "big")
        assert wire.flags & 1
        assert wire.name == "GET /safe/{id}" and wire.kind == 2
        assert os.environ["OTEL_SDK_DISABLED"] == "true"
        assert len(wire.attributes) >= 4
        assert not wire.events and not wire.links and not wire.trace_state and not wire.status.message
        assert instance.processor.snapshot() == {"queued": 0, "dropped": 0, "export_failed": 0, "exported": 1}
    assert trace.get_tracer_provider() is global_provider
    assert not runtime.enabled()


@pytest.mark.asyncio
async def test_correlation_concurrent_context_and_ended_span(monkeypatch, tls_dir, certificates):
    with _receiver(certificates) as (endpoint, _):
        _, close = _start(monkeypatch, tls_dir, endpoint)
        async def one():
            request_id = str(uuid4())
            token = runtime.request_id_var.set(request_id)
            span = _span()
            try:
                with trace.use_span(span, end_on_exit=False):
                    before = runtime.correlation()
                    await asyncio.sleep(0)
                    assert runtime.correlation() == before
                    assert before["request_id"] == request_id
                    span.end()
                    assert runtime.correlation() == {"request_id": request_id}
                return before
            finally:
                runtime.request_id_var.reset(token)
        values = await asyncio.gather(*(one() for _ in range(20)))
        assert len({v["trace_id"] for v in values}) == 20
        assert len({v["request_id"] for v in values}) == 20
        assert runtime.correlation() == {}
        close()
    for invalid in ("bad", "00000000-0000-0000-0000-000000000000", str(uuid4()).upper()):
        token = runtime.request_id_var.set(invalid)
        assert runtime.correlation() == {}
        runtime.request_id_var.reset(token)


@pytest.mark.parametrize("raw", ['{}', 'null', '[{}]', '[{"module_name":"agent","module_name":"agent"}]',
                                 '[{"module_name":"agent","role":"backend","endpoint":"https://x/v1/traces","tls_dir":"/x","extra":"x"}]',
                                 '[] true', '"' + 'x' * (256 << 10) + '"'])
def test_strict_json_rejected(raw):
    with pytest.raises((ValueError, TypeError)):
        runtime._deployment(raw, "agent", "backend")


@pytest.mark.parametrize("field,value", [("endpoint", "http://localhost/v1/traces"), ("endpoint", "https://user:secret@x/v1/traces"),
    ("endpoint", "https://x/v1/traces?"), ("endpoint", "https://x/v1/traces#"), ("endpoint", "https://x:99999/v1/traces"), ("endpoint", "https://x\n/v1/traces"),
    ("endpoint", "https://x/v1/%74races"), ("tls_dir", "relative"), ("role", "worker"), ("module_name", "Bad")])
def test_deployment_fields_rejected(field, value):
    item = {"module_name": "agent", "role": "backend", "endpoint": "https://x/v1/traces", "tls_dir": "/tmp/tls"}
    item[field] = value
    with pytest.raises(ValueError):
        runtime._deployment(json.dumps([item]), "agent", "backend")


def test_missing_and_duplicate_deployments():
    assert runtime._deployment("", "agent", "backend") is None
    assert runtime._deployment("[]", "agent", "backend") is None
    item = {"module_name": "agent", "role": "backend", "endpoint": "https://x/v1/traces", "tls_dir": "/tmp/tls"}
    with pytest.raises(ValueError):
        runtime._deployment(json.dumps([item, item]), "agent", "backend")


@pytest.mark.parametrize("case", ["directory-mode", "directory-link", "key-mode", "file-link", "hard-link", "extra", "empty", "oversize", "repo"])
def test_tls_files_fail_closed(tls_dir, tmp_path, case):
    target = tls_dir
    if case == "directory-mode": tls_dir.chmod(0o755)
    elif case == "directory-link":
        target = tmp_path / "alias"; target.symlink_to(tls_dir, target_is_directory=True)
    elif case == "key-mode": (tls_dir / "client.key").chmod(0o640)
    elif case == "file-link":
        original = tls_dir / "client.key"; moved = tmp_path / "key"; original.rename(moved); original.symlink_to(moved)
    elif case == "hard-link": os.link(tls_dir / "client.key", tmp_path / "key")
    elif case == "extra": (tls_dir / "extra").write_text("x")
    elif case == "empty": (tls_dir / "ca.crt").write_text("")
    elif case == "oversize": (tls_dir / "ca.crt").write_bytes(b"x" * ((256 << 10) + 1))
    elif case == "repo": target = Path(runtime.__file__).resolve().parent
    with pytest.raises((ValueError, OSError, ssl.SSLError)):
        runtime._tls(str(target))


def test_optional_invalid_configuration_logs_only_safe_code(monkeypatch, caplog):
    registration = SimpleNamespace(module_name="agent", role="backend", instance_id=str(uuid4()))
    runtime.start_optional(registration)()
    assert not runtime.enabled() and not runtime.tracer().start_span("noop").is_recording()
    monkeypatch.setenv("ADDP_OBSERVABILITY_TRACES_ENABLED", "true")
    monkeypatch.setenv("ADDP_TRACE_DEPLOYMENTS", "SECRET-BAD-JSON")
    runtime.start_optional(registration)()
    assert not runtime.enabled()
    assert "trace_deployment_invalid" in caplog.text and "SECRET" not in caplog.text


@pytest.mark.parametrize("foreign_identity", [False, True])
def test_foreign_ca_and_absent_client_rejected(monkeypatch, tls_dir, certificates, foreign_identity):
    with _receiver(certificates) as (endpoint, received):
        context = ssl.create_default_context(cafile=str(certificates / "ca.crt"))
        with httpx.Client(verify=context, trust_env=False, timeout=1) as client:
            with pytest.raises(httpx.HTTPError):
                client.post(endpoint, content=b"x")
        if foreign_identity:
            shutil.copyfile(certificates / "foreign.crt", tls_dir / "client.crt")
            shutil.copyfile(certificates / "foreign.key", tls_dir / "client.key")
        else:
            shutil.copyfile(certificates / "foreign.crt", tls_dir / "ca.crt")
        _, close = _start(monkeypatch, tls_dir, endpoint)
        instance = runtime._current
        _span().end()
        assert instance.processor.force_flush(2000)
        assert instance.processor.snapshot()["export_failed"] == 1
        close()
        assert received == []


@pytest.mark.parametrize("status,delay", [(302, 0), (503, 0), (200, 2.3)])
def test_no_redirect_retry_and_real_timeout(monkeypatch, tls_dir, certificates, status, delay):
    with _receiver(certificates, delay=delay, status=status) as (endpoint, received):
        _, close = _start(monkeypatch, tls_dir, endpoint)
        instance = runtime._current
        _span().end()
        started = time.monotonic()
        assert instance.processor.force_flush(2500)
        assert time.monotonic() - started < 2.4
        assert instance.processor.snapshot()["export_failed"] == 1
        close()
        assert len(received) == 1


class _BlockedClient:
    def __init__(self):
        self.started = threading.Event(); self.release = threading.Event(); self.calls = 0; self.closed = False
    @contextmanager
    def stream(self, *args, **kwargs):
        self.calls += 1; self.started.set(); self.release.wait(10)
        yield SimpleNamespace(status_code=200)
    def close(self): self.closed = True


def test_nonblocking_queue_and_bounded_shutdown_no_late_success():
    client = _BlockedClient()
    processor = runtime._Processor(client, "https://unused/v1/traces", {"service.name": "agent"})
    provider = TracerProvider(resource=Resource({}), shutdown_on_exit=False)
    provider.add_span_processor(processor)
    for _ in range(256): _span(provider).end()
    assert client.started.wait(1)
    started = time.monotonic()
    for _ in range(3000): _span(provider).end()
    assert time.monotonic() - started < 1
    assert processor.snapshot()["queued"] == 2048
    assert processor.snapshot()["dropped"] == 952
    deadline = time.monotonic() + 2.2
    while processor.snapshot()["export_failed"] == 0 and time.monotonic() < deadline:
        time.sleep(0.01)
    assert processor.snapshot()["export_failed"] == 256
    assert client.calls == 1
    started = time.monotonic()
    provider.shutdown()
    assert time.monotonic() - started < 3.15
    processor.worker.join(0.2)
    assert not processor.worker.is_alive()
    assert processor.snapshot()["queued"] == 0
    assert processor.snapshot()["dropped"] == 3000
    assert processor.snapshot()["exported"] == 0
    assert client.calls == 1
    assert processor.network.is_alive()
    client.release.set()
    processor.network.join(1)
    assert client.closed and processor.snapshot()["exported"] == 0


def test_safe_query_method_fixed_5xx_and_rejected_values():
    assert runtime._safe_attributes({"http.request.method": "QUERY", "error.type": "599"}) == {
        "http.request.method": "QUERY", "error.type": "599"}
    assert runtime._safe_attributes({"http.request.method": "SECRET", "http.route": "/path?secret=x",
        "http.response.status_code": True, "error.type": "499", "addp.http.routine": "true",
        "user.id": "SECRET", "http.route.bad": "SECRET"}) == {}


def test_unsupported_and_malformed_span_projection_is_soft():
    client = _BlockedClient(); client.release.set()
    processor = runtime._Processor(client, "https://unused/v1/traces", {"service.name": "agent"})
    provider = TracerProvider(resource=Resource({}), shutdown_on_exit=False)
    provider.add_span_processor(processor)
    provider.get_tracer("test").start_span("SQL SECRET", kind=trace.SpanKind.INTERNAL).end()
    processor.on_end(SimpleNamespace(kind=trace.SpanKind.SERVER))
    assert processor.snapshot()["dropped"] == 2
    assert processor.snapshot()["queued"] == 0
    provider.shutdown()
    assert client.calls == 0


def test_serialization_failure_counted_and_worker_recovers(monkeypatch):
    client = _BlockedClient(); client.release.set()
    processor = runtime._Processor(client, "https://unused/v1/traces", {"service.name": "agent"})
    provider = TracerProvider(resource=Resource({}), shutdown_on_exit=False)
    provider.add_span_processor(processor)
    original = runtime.ExportTraceServiceRequest
    def fail_once(**kwargs):
        monkeypatch.setattr(runtime, "ExportTraceServiceRequest", original)
        raise ValueError("SECRET-PROTO-ERROR")
    monkeypatch.setattr(runtime, "ExportTraceServiceRequest", fail_once)
    _span(provider).end()
    assert processor.force_flush(1000)
    assert processor.snapshot()["export_failed"] == 1
    _span(provider).end()
    assert processor.force_flush(1000)
    provider.shutdown()
    assert processor.snapshot()["exported"] == 1 and client.calls == 1


@pytest.mark.parametrize("enabled", [False, True])
def test_invalid_sdk_environment_never_breaks_optional_import(enabled):
    script = """
import logging, threading
from types import SimpleNamespace
from addp_common.httptrace import runtime
before = {thread.ident for thread in threading.enumerate()}
registration = SimpleNamespace(module_name="agent", role="backend", instance_id="runtime-test")
close = runtime.start_optional(registration)
assert not runtime.enabled()
assert not runtime.tracer().start_span("noop").is_recording()
close()
assert {thread.ident for thread in threading.enumerate()} == before
print("optional_ready")
"""
    env = dict(os.environ)
    env["OTEL_SPAN_ATTRIBUTE_COUNT_LIMIT"] = "SECRET-NOT-AN-INTEGER"
    env["ADDP_OBSERVABILITY_TRACES_ENABLED"] = "true" if enabled else "false"
    env["ADDP_TRACE_DEPLOYMENTS"] = json.dumps([{
        "module_name": "agent", "role": "backend", "endpoint": "https://localhost/v1/traces", "tls_dir": "/tmp/not-used",
    }])
    result = subprocess.run([sys.executable, "-c", script], env=env, capture_output=True, text=True, timeout=10)
    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == "optional_ready"
    assert "SECRET" not in result.stderr and "Traceback" not in result.stderr
    assert result.stderr.strip() == ("trace_deployment_invalid" if enabled else "")


def test_late_initialization_failure_closes_client_and_worker(monkeypatch, tls_dir, certificates, caplog):
    import opentelemetry.sdk.trace as sdk
    original = sdk.TracerProvider
    clients = []
    factory = runtime.httpx.Client
    def create_client(*args, **kwargs):
        client = factory(*args, **kwargs)
        clients.append(client)
        return client
    class BrokenProvider(original):
        def add_span_processor(self, processor):
            super().add_span_processor(processor)
            raise ValueError("SECRET-AFTER-WORKER-START")
    monkeypatch.setattr(runtime.httpx, "Client", create_client)
    monkeypatch.setattr(sdk, "TracerProvider", BrokenProvider)
    before = {thread.ident for thread in threading.enumerate() if thread.name.startswith("addp-trace-")}
    with _receiver(certificates) as (endpoint, received):
        _, close = _start(monkeypatch, tls_dir, endpoint)
        close()
        assert not runtime.enabled()
        assert len(clients) == 1 and clients[0].is_closed
        assert {thread.ident for thread in threading.enumerate() if thread.name.startswith("addp-trace-")} == before
        assert received == []
    assert "trace_deployment_invalid" in caplog.text and "SECRET" not in caplog.text
