from dataclasses import dataclass
from datetime import datetime

from .client import SystemClient


@dataclass(frozen=True)
class AuthorizationContext:
    user_id: int
    tenant_id: int | None
    username: str
    user_type: str
    subject_type: str = "user"
    auth_type: str = "first_party_access_token"
    client_id: str | None = None
    audiences: tuple[str, ...] = ()
    scopes: tuple[str, ...] = ()
    delegated_by: str | None = None
    agent_run_id: str | None = None
    tool_call_id: str | None = None
    issued_at: datetime | None = None
    expires_at: datetime | None = None


def _parse_time(value: object, field_name: str) -> datetime:
    if not isinstance(value, str) or not value:
        raise ValueError(f"authorization context {field_name} must be an ISO 8601 string")
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


async def resolve_authorization_context(system_url: str, token: str) -> AuthorizationContext:
    """Resolve a user access token through the canonical System AuthContext API."""
    async with SystemClient(base_url=system_url, user_token=token) as client:
        data = await client.get_authorization_context()

    if data.get("subject_type") != "user":
        raise ValueError("authorization context subject_type must be user")
    user_id = int(data["user_id"])
    raw_tenant_id = data.get("tenant_id")
    tenant_id = int(raw_tenant_id) if raw_tenant_id is not None else None
    if user_id <= 0:
        raise ValueError("authorization context user_id must be positive")
    user_type = str(data.get("user_type") or "")
    if user_type not in {"super_admin", "tenant_admin", "user"}:
        raise ValueError("authorization context user_type is invalid")
    if user_type == "super_admin" and tenant_id is not None:
        raise ValueError("super admin authorization context must be tenantless")
    if user_type != "super_admin" and (tenant_id is None or tenant_id <= 0):
        raise ValueError("tenant user authorization context must contain tenant_id")

    return AuthorizationContext(
        user_id=user_id,
        tenant_id=tenant_id,
        username=str(data.get("username") or ""),
        user_type=user_type,
        subject_type="user",
        auth_type=str(data.get("auth_type") or ""),
        client_id=data.get("client_id"),
        audiences=tuple(str(value) for value in data.get("audiences") or []),
        scopes=tuple(str(value) for value in data.get("scopes") or []),
        delegated_by=data.get("delegated_by"),
        agent_run_id=data.get("agent_run_id"),
        tool_call_id=data.get("tool_call_id"),
        issued_at=_parse_time(data.get("issued_at"), "issued_at"),
        expires_at=_parse_time(data.get("expires_at"), "expires_at"),
    )
