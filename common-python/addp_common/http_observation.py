"""Pure HTTP transport normalization shared by server observation and clients."""
import asyncio
import uuid

_METHODS = {"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "CONNECT", "TRACE", "QUERY"}
_ROUTINE = {"/health/live", "/health/ready", "/api/v1/system/runtime/modules/heartbeat"}

def _method(value: str) -> str:
    return value if value in _METHODS else "_OTHER"

def _routine(path: str) -> bool:
    return path in _ROUTINE

def _failure_type(error: BaseException) -> str:
    if isinstance(error, (asyncio.CancelledError, GeneratorExit)):
        return "canceled"
    if isinstance(error, TimeoutError):
        return "timeout"
    return "_OTHER"

def _canonical_request_id(value: str) -> bool:
    try:
        parsed = uuid.UUID(value)
        return bool(parsed.int) and str(parsed) == value
    except (ValueError, AttributeError, TypeError):
        return False
