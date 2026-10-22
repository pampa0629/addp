"""Agent installs the shared tracer outside its real readiness/auth stack."""
import asyncio
import unittest
from unittest.mock import AsyncMock, Mock, patch


class AgentTraceLifecycleTests(unittest.IsolatedAsyncioTestCase):
    async def test_database_failure_releases_optional_trace_with_registration_identity(self):
        import main

        close = Mock()
        with (
            patch.object(main.trace_runtime, "start_optional", return_value=close) as start,
            patch.object(main, "init_db", new=AsyncMock(side_effect=RuntimeError("db unavailable"))),
        ):
            with self.assertRaisesRegex(RuntimeError, "db unavailable"):
                await main.startup()
        start.assert_called_once_with(main._registration)
        close.assert_called_once_with()
        self.assertIsNone(main._trace_close)

    async def test_trace_close_runs_even_when_business_shutdown_fails(self):
        import main

        close = Mock()
        with (
            patch.object(main, "_trace_close", close),
            patch.object(main, "_registry_client", None),
            patch.object(main, "_registry_task", None),
            patch.object(main, "_registry_monitor_task", None),
            patch.object(main.AgentInferenceService, "close", new=AsyncMock(side_effect=RuntimeError("close failed"))),
        ):
            with self.assertRaisesRegex(RuntimeError, "close failed"):
                await main.shutdown()
        close.assert_called_once_with()


class AgentTraceHTTPTests(unittest.TestCase):
    def test_real_stack_records_not_ready_auth_stream_and_unhandled_failure(self):
        import main
        from addp_common.auth import AuthorizationContext
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
        old_stack = main.app.middleware_stack
        main.app.router.routes.extend(routes)
        main.app.middleware_stack = None
        try:
            with (
                patch.object(runtime, "enabled", return_value=True),
                patch.object(runtime, "tracer", return_value=provider.get_tracer("agent-test")),
            ):
                client = TestClient(main.app, raise_server_exceptions=False)
                response = client.get("/api/v1/agent/sessions")
                self.assertEqual(response.status_code, 503)
                self.assertEqual(response.json()["error_code"], "module_not_ready")
                self.assertTrue(response.headers.get("x-request-id"))
                with patch.object(main, "ready_response", return_value=({}, True)):
                    denied = client.get("/api/v1/agent/sessions")
                    self.assertEqual(denied.status_code, 401)
                    context = AuthorizationContext(principal_id=12, tenant_id=3, tenant_membership_id=8)
                    with patch("middleware.auth.resolve_authorization_context", new=AsyncMock(return_value=context)):
                        stream_response = client.get("/__trace_stream", headers={"Authorization": "Bearer controlled"})
                        self.assertEqual(stream_response.content, b"firstsecond")
                        failure = client.get("/__trace_fail", headers={"Authorization": "Bearer controlled"})
                        self.assertEqual(failure.status_code, 500)
                self.assertEqual(runtime.correlation(), {})
            spans = exporter.get_finished_spans()
            self.assertEqual(len(spans), 4)
            self.assertEqual([span.attributes["http.response.status_code"] for span in spans], [503, 401, 200, 0])
            self.assertTrue(spans[-1].attributes["addp.http.server_failure"])
            self.assertEqual(spans[-1].attributes["error.type"], "_OTHER")
            self.assertEqual(spans[-1].status.status_code.name, "ERROR")
            self.assertEqual(observed[0], observed[1])
            self.assertTrue(observed[0]["trace_id"])
            self.assertEqual(observed[0]["request_id"], stream_response.headers["x-request-id"])
        finally:
            for route in routes:
                main.app.router.routes.remove(route)
            main.app.middleware_stack = old_stack
            provider.shutdown()


class OwnedTraceFixtureTests(unittest.TestCase):
    def test_sigterm_returns_through_trace_cleanup_and_exits_zero(self):
        import os
        from pathlib import Path
        import subprocess
        import sys
        import tempfile
        import time
        from urllib.request import ProxyHandler, build_opener
        from uuid import UUID

        root = Path(__file__).resolve().parents[3]
        fixture = root / "scripts/test/trace-python-source.py"
        # The receipt proves this real process returned through finally. Actual
        # mTLS batch export remains the standard mixed-language Trace T2's job.
        script = """
import runpy, sys
from pathlib import Path
from addp_common.httptrace import runtime
def start(_registration):
    return lambda: Path(sys.argv[2]).write_text('closed')
runtime.start_optional = start
runpy.run_path(sys.argv[1], run_name='__main__')
"""
        with tempfile.TemporaryDirectory(prefix="addp-python-trace-exit-") as directory:
            address = Path(directory) / "address"
            receipt = Path(directory) / "closed"
            env = dict(os.environ, TRACE_T2_NEXT="http://127.0.0.1:1", TRACE_T2_ADDRESS=str(address))
            env["ADDP_OBSERVABILITY_TRACES_ENABLED"] = "false"
            env["PYTHONPATH"] = str(root / "common-python") + os.pathsep + env.get("PYTHONPATH", "")
            process = subprocess.Popen(
                [sys.executable, "-c", script, str(fixture), str(receipt)],
                env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
            )
            try:
                deadline = time.monotonic() + 10
                ready = False
                opener = build_opener(ProxyHandler({}))
                while time.monotonic() < deadline and process.poll() is None:
                    if address.exists():
                        try:
                            with opener.open(address.read_text() + "/health/ready", timeout=1) as response:
                                self.assertEqual(response.status, 200)
                                self.assertNotEqual(UUID(response.headers["x-request-id"]).int, 0)
                            ready = True
                            break
                        except OSError:
                            pass
                    time.sleep(0.02)
                self.assertTrue(ready, "owned Python fixture did not become ready")
                process.terminate()
                output, _ = process.communicate(timeout=6)
                self.assertEqual(process.returncode, 0, output.decode(errors="replace"))
                self.assertEqual(receipt.read_text(), "closed")
            finally:
                if process.poll() is None:
                    process.kill()
                    process.communicate(timeout=3)
