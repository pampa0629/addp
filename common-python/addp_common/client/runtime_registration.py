"""Bearer-only System registration for built-in workflow runtimes."""

from __future__ import annotations

import threading
from collections.abc import Callable
from typing import Any

import httpx

from .service_token import SyncOAuthServiceTokenSource


def register_runtime_engine(
    system_url: str,
    client_id: str,
    client_secret: str,
    payload: dict[str, Any],
    *,
    timeout: float = 10.0,
) -> tuple[int, str]:
    source = SyncOAuthServiceTokenSource(system_url, client_id, client_secret, timeout=timeout)
    try:
        token = source.platform_token()
        with httpx.Client(timeout=timeout, trust_env=False) as client:
            response = client.post(
                system_url.rstrip("/") + "/api/v1/system/runtime/engines",
                json=payload,
                headers={"Authorization": f"Bearer {token}"},
            )
        return response.status_code, response.text
    finally:
        source.close()


def retry_runtime_registration(
    register: Callable[[], bool],
    runtime_name: str,
    logger: Any,
    *,
    initial_interval: float = 10.0,
    max_interval: float = 60.0,
    wait: Callable[[float], Any] | None = None,
) -> None:
    """Retry one idempotent Runtime registration until success.

    Callers run this helper in a daemon thread so System startup order never blocks
    the Runtime listener. The loop stops only after success or process shutdown.
    """
    if initial_interval <= 0:
        initial_interval = 1.0
    if max_interval < initial_interval:
        max_interval = initial_interval
    wait = wait or threading.Event().wait
    attempt = 1
    interval = initial_interval
    while True:
        logger.info("attempting to register %s to System (attempt %s)", runtime_name, attempt)
        if register():
            logger.info("%s registration succeeded on attempt %s", runtime_name, attempt)
            return
        wait(interval)
        attempt += 1
        interval = min(interval * 2, max_interval)
