"""Owned Python middle process for the standard mixed-language Trace T2."""
import logging
import os
import signal
import socket
from contextlib import asynccontextmanager
from pathlib import Path

import httpx
import uvicorn
from fastapi import FastAPI
from starlette.responses import Response

from addp_common.client.module_registry import ModuleRegistration
from addp_common.httptrace.client import trace_transport
from addp_common.httptrace.runtime import snapshot, start_optional
from addp_common.httptrace.server import TraceMiddleware
from addp_common.runtime_logging import setup_runtime_logging


def main():
    setup_runtime_logging()
    target = os.environ["TRACE_T2_NEXT"]
    registration = ModuleRegistration(
        module_name="copilot", instance_id="t2-copilot", module_url="http://127.0.0.1:1",
        route_prefix="/copilot", health_check_url="http://127.0.0.1:1/health/ready",
    )
    close_trace = start_optional(registration)
    client = httpx.AsyncClient(transport=trace_transport(target), timeout=5, trust_env=False)

    @asynccontextmanager
    async def lifespan(app):
        try:
            yield
        finally:
            await client.aclose()

    app = FastAPI(lifespan=lifespan)
    app.add_middleware(TraceMiddleware, module="copilot")

    @app.get("/health/ready")
    async def ready():
        return snapshot()

    @app.get("/items/{item_id}")
    async def item(item_id: str):
        logging.getLogger("trace.source").info("trace fixture request")
        response = await client.get(target + "/items/" + item_id)
        return Response(response.content, status_code=response.status_code, media_type="application/json")

    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    listener.listen(128)
    listener.setblocking(False)
    Path(os.environ["TRACE_T2_ADDRESS"]).write_text(f"http://127.0.0.1:{listener.getsockname()[1]}")
    server = uvicorn.Server(uvicorn.Config(app, log_config=None, access_log=False))
    # Uvicorn restores and replays captured signals after graceful shutdown.
    # Keep this owned fixture's handler in place so SIGTERM returns through
    # Python's finally block, flushing telemetry before the child exits zero.
    def request_shutdown(_signal, _frame):
        server.should_exit = True
    previous_handler = signal.signal(signal.SIGTERM, request_shutdown)
    try:
        server.run(sockets=[listener])
    finally:
        try:
            close_trace()
        finally:
            listener.close()
            signal.signal(signal.SIGTERM, previous_handler)


if __name__ == "__main__":
    main()
