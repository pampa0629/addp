"""Shared HTTP health and readiness contract for ADDP Python backends."""

from __future__ import annotations

import asyncio
import json
import os
import signal
from datetime import datetime, timezone
from typing import Callable

from .client.module_registry import ModuleRegistration, ModuleRegistryClient


_STARTED_AT = datetime.now(timezone.utc).isoformat()


def _build_identity() -> dict[str, str]:
    return {
        "build_id": os.getenv("ADDP_BUILD_ID", "unknown"),
        "git_commit": os.getenv("ADDP_GIT_COMMIT", "unknown"),
        "source_fingerprint": os.getenv("ADDP_SOURCE_FINGERPRINT", "unknown"),
        "built_at": os.getenv("ADDP_BUILT_AT", "unknown"),
        "started_at": _STARTED_AT,
    }


def live_response(module: str) -> dict[str, object]:
    return {"status": "live", "module": module, **_build_identity()}


def ready_response(
    module: str,
    registration: ModuleRegistration,
    registry_client: ModuleRegistryClient | None,
    *,
    local_ready: bool = True,
) -> tuple[dict[str, object], bool]:
    snapshot = registry_client.snapshot(registration) if registry_client else None
    state = snapshot.state if snapshot else "starting"
    registration_error = snapshot.error_code if snapshot else ""
    ready = local_ready and state == "registered"
    checks = [{
        "name": "local_dependencies",
        "status": "ready" if local_ready else "not_ready",
        **({"error_code": "local_dependencies_unavailable"} if not local_ready else {}),
    }, {
        "name": "system_registration",
        "status": "ready" if state == "registered" else "not_ready",
        **({"error_code": registration_error or "system_registration_unavailable"} if state != "registered" else {}),
    }]
    return ({
        "status": "ready" if ready else "not_ready",
        "module": module,
        "role": registration.role,
        "instance_id": registration.instance_id,
        "registration_state": state,
        "checks": checks,
        **_build_identity(),
    }, ready)


class ModuleReadyMiddleware:
    def __init__(self, app, readiness: Callable[[], tuple[dict[str, object], bool]]) -> None:
        self.app = app
        self.readiness = readiness

    async def __call__(self, scope, receive, send) -> None:
        if scope.get("type") != "http" or scope.get("path") in {
            "/health/live", "/health/ready", "/docs", "/openapi.json", "/redoc",
        }:
            await self.app(scope, receive, send)
            return
        _, ready = self.readiness()
        if ready:
            await self.app(scope, receive, send)
            return
        body = json.dumps(
            {"error": "module is not ready", "error_code": "module_not_ready"},
            separators=(",", ":"),
        ).encode()
        await send({
            "type": "http.response.start",
            "status": 503,
            "headers": [(b"content-type", b"application/json"), (b"content-length", str(len(body)).encode())],
        })
        await send({"type": "http.response.body", "body": body})


async def terminate_process_on_registration_failure(task: asyncio.Task) -> None:
    try:
        await task
    except asyncio.CancelledError:
        raise
    except Exception:
        os.kill(os.getpid(), signal.SIGTERM)


async def register_after_listener(
    registry_client: ModuleRegistryClient,
    registration: ModuleRegistration,
    port: int,
    *,
    host: str = "127.0.0.1",
    retry_interval: float = 0.05,
) -> None:
    """Start the registry loop only after the local HTTP listener is reachable."""
    while True:
        try:
            _, writer = await asyncio.open_connection(host, port)
        except OSError:
            await asyncio.sleep(retry_interval)
            continue
        writer.close()
        await writer.wait_closed()
        await registry_client.run(registration)
        return
