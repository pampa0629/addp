"""Whole-runtime readiness for addp.workflow/v1; independent of registration."""

from collections.abc import Callable
from typing import Any


def health_dependency(available: bool, *, required: bool = True,
                      version: str = "", details: str = "") -> dict[str, Any]:
    if type(available) is not bool or type(required) is not bool:
        raise ValueError("dependency availability and requirement must be booleans")
    fact: dict[str, Any] = {"available": available, "required": required}
    if version:
        fact["version"] = version
    if details:
        fact["details"] = details
    return fact


def runtime_health(service: str, version: str, uptime: int,
                   probe: Callable[[], dict[str, Any]]) -> tuple[dict[str, Any], int]:
    """Probe local facts and construct the same envelope on success and failure.

    The probe owns actual tool/SDK checks. Exceptions fail closed without exposing
    exception text, paths, credentials or downstream response bodies.
    """
    body: dict[str, Any] = {"service": service, "version": version, "uptime": max(0, uptime)}
    try:
        facts = probe()
        dependencies = facts.get("dependencies", {})
        ready = all(not item["required"] or item["available"] for item in dependencies.values())
        body.update(facts)
        body["status"] = "healthy" if ready else "unhealthy"
        return body, 200 if ready else 503
    except Exception:
        body.update(status="unhealthy", error_code="health_check_failed")
        return body, 503
