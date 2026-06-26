import os
from typing import Any, Dict

import requests


def get_engine(engine_id: int) -> Dict[str, Any]:
    """Fetch a System engine through the internal API."""
    system_url = os.getenv("SYSTEM_URL", "http://localhost:8180").rstrip("/")
    api_key = os.getenv("INTERNAL_API_KEY", "")
    headers = {"X-Internal-API-Key": api_key} if api_key else {}

    response = requests.get(
        f"{system_url}/api/v1/internal/engines/{engine_id}",
        headers=headers,
        timeout=10,
    )
    if response.status_code != 200:
        raise ValueError(f"Failed to get engine {engine_id}: {response.text}")
    return response.json()

