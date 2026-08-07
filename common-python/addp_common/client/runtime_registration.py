"""Bearer-only System registration for built-in workflow runtimes."""

from __future__ import annotations

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
