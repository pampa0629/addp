"""发现当前 Notebook 会话可用于查询的 ADDP Engine。"""

from __future__ import annotations

import builtins
import os
from typing import Any
from urllib.parse import urlsplit

import httpx


_ENDPOINT_ENV = "ADDP_NOTEBOOK_OWNER_API_ENDPOINT"
_CAPABILITY_ENV = "ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN"
_CAPABILITY_PREFIX = "addp_nkc_"
_transport: httpx.BaseTransport | None = None


class NotebookSessionUnavailableError(RuntimeError):
    """当前 Python process 不属于可发现 Engine 的受控 Notebook 会话。"""


class NotebookEngineDiscoveryError(RuntimeError):
    """当前 Notebook 会话未能读取可用 Engine 描述。"""


def list(*, timeout: float = 10.0) -> builtins.list[dict[str, Any]]:
    """返回当前会话所属租户中可用的脱敏查询 Engine 描述列表。"""
    endpoint = os.environ.get(_ENDPOINT_ENV, "").strip()
    token = os.environ.get(_CAPABILITY_ENV, "").strip()
    parsed = urlsplit(endpoint)
    if (
        parsed.scheme not in {"http", "https"}
        or not parsed.netloc
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
        or not token.startswith(_CAPABILITY_PREFIX)
    ):
        raise NotebookSessionUnavailableError("当前 Python process 不属于可发现 ADDP Engine 的 Notebook 会话")

    try:
        client_options: dict[str, Any] = {"timeout": timeout, "trust_env": False}
        if _transport is not None:
            client_options["transport"] = _transport
        with httpx.Client(**client_options) as client:
            response = client.get(
                endpoint,
                headers={
                    "Authorization": f"Bearer {token}",
                    "Cache-Control": "no-store",
                },
            )
    except httpx.HTTPError as exc:
        raise NotebookEngineDiscoveryError("无法连接 ADDP Notebook Engine 发现接口") from exc

    if response.status_code != httpx.codes.OK:
        raise NotebookEngineDiscoveryError(f"ADDP Notebook Engine 发现接口返回 HTTP {response.status_code}")
    try:
        payload = response.json()
    except ValueError as exc:
        raise NotebookEngineDiscoveryError("ADDP Notebook Engine 发现接口返回了无效 JSON") from exc
    if not isinstance(payload, builtins.list) or any(not isinstance(item, dict) for item in payload):
        raise NotebookEngineDiscoveryError("ADDP Notebook Engine 发现接口返回了无效描述列表")
    return payload

__all__ = [
    "NotebookEngineDiscoveryError",
    "NotebookSessionUnavailableError",
    "list",
]
