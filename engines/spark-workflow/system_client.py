import os
from datetime import datetime, timezone
from uuid import UUID
from typing import Any, Dict

import requests

from addp_common.client import SyncOAuthServiceTokenSource


_token_source: SyncOAuthServiceTokenSource | None = None


class ExecutionAccessError(ValueError):
    """Closed HTTP category; no raw System response is exposed to callers."""

    def __init__(self, message, status_code):
        super().__init__(message)
        self.status_code = status_code


def get_execution_engine_access(authorization_id: str, execution_id: str,
                                engine_id: int, tenant_id: int, *, timeout=10) -> Dict[str, Any]:
    """Consume one exact read-only Spark query grant as the existing service identity.

    tenant_id must come from the authenticated caller context. No User Token,
    caller-supplied connection, engine-list lookup or engine-wide fallback is used.
    """
    if (not isinstance(authorization_id, str) or not authorization_id.isascii()
            or not authorization_id.isdecimal() or authorization_id.startswith('0')):
        raise ValueError('authorization_id must be a canonical positive ID')
    try:
        parsed = UUID(execution_id)
        if not parsed.int or str(parsed) != execution_id:
            raise ValueError()
    except (ValueError, TypeError, AttributeError):
        raise ValueError('execution_id must be a canonical UUID') from None
    for value in (engine_id, tenant_id):
        if not isinstance(value, int) or isinstance(value, bool) or value <= 0:
            raise ValueError('engine_id and tenant_id must be positive integers')
    source = _service_token_source()
    system_url = os.getenv('SYSTEM_URL', 'http://localhost:8180').rstrip('/')
    for attempt in range(2):
        token = source.token(tenant_id)
        response = requests.post(
            f'{system_url}/api/v1/system/execution-authorizations/{authorization_id}/engine-accesses',
            headers={'Authorization': f'Bearer {token}'},
            json={'execution_id': execution_id, 'engine_id': str(engine_id), 'required_effects': ['read']},
            timeout=timeout,
        )
        if response.status_code != 401 or attempt == 1:
            break
        source.invalidate(tenant_id, token)
    if response.status_code != 200:
        raise ExecutionAccessError('Spark execution engine access denied or unavailable',
                                   403 if response.status_code in {400, 401, 403, 404} else 503)
    try:
        access = response.json()
        engine = access['engine']
        expires = datetime.fromisoformat(access['expires_at'].replace('Z', '+00:00'))
        valid = (
            access['authorization_id'] == authorization_id and access['execution_id'] == execution_id
            and access['engine_id'] == str(engine_id) and access['audience'] == 'spark'
            and access['effects'] == ['read'] and type(engine['id']) is int and engine['id'] == engine_id
            and type(engine['tenant_id']) is int and engine['tenant_id'] == tenant_id
            and expires.tzinfo is not None and expires > datetime.now(timezone.utc)
        )
    except (KeyError, TypeError, ValueError, AttributeError):
        raise ExecutionAccessError('System returned an invalid Spark execution engine access', 503) from None
    if not valid:
        raise ExecutionAccessError('System returned an invalid Spark execution engine access', 503)
    return access


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
