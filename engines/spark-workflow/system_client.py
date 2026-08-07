import os
from typing import Any, Dict

import requests

from addp_common.client import SyncOAuthServiceTokenSource


_token_source: SyncOAuthServiceTokenSource | None = None


def _service_token_source() -> SyncOAuthServiceTokenSource:
    global _token_source
    if _token_source is None:
        _token_source = SyncOAuthServiceTokenSource(
            os.getenv("SYSTEM_URL", "http://localhost:8180"),
            "addp-spark",
            os.getenv("SPARK_WORKFLOW_SERVICE_CLIENT_SECRET", ""),
        )
    return _token_source


def get_engine(engine_id: int, tenant_id: int) -> Dict[str, Any]:
    """Fetch a tenant engine with the Spark runtime Service Principal."""
    if not isinstance(engine_id, int) or isinstance(engine_id, bool) or engine_id <= 0:
        raise ValueError("engine_id must be a positive integer")
    if not isinstance(tenant_id, int) or isinstance(tenant_id, bool) or tenant_id <= 0:
        raise ValueError("tenant_id must be a positive integer")

    system_url = os.getenv("SYSTEM_URL", "http://localhost:8180").rstrip("/")
    source = _service_token_source()
    for attempt in range(2):
        token = source.token(tenant_id)
        response = requests.get(
            f"{system_url}/api/v1/system/engines/{engine_id}",
            headers={"Authorization": f"Bearer {token}"},
            timeout=10,
        )
        if response.status_code != 401 or attempt == 1:
            break
        source.invalidate(tenant_id, token)
    if response.status_code != 200:
        raise ValueError(f"Failed to get engine {engine_id}: {response.text}")
    return response.json()
