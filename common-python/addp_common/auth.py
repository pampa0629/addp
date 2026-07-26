from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any

from .client import SystemClient


AUTH_CONTEXT_SCHEMA_VERSION = "addp.auth_context/v1"


@dataclass(frozen=True)
class RoleAssignment:
    assignment_id: int
    role_key: str
    scope_type: str
    permissions: tuple[str, ...]


@dataclass(frozen=True)
class AuthorizationContext:
    principal_id: int
    principal_type: str = "user"
    context_type: str = "tenant"
    tenant_id: int | None = None
    tenant_membership_id: int | None = None
    authentication_methods: tuple[str, ...] = ("password",)
    assurance_level: str = "aal1"
    authenticated_at: datetime = datetime.min.replace(tzinfo=timezone.utc)
    step_up_expires_at: datetime | None = None
    client_id: str | None = None
    audiences: tuple[str, ...] = ("addp.api",)
    scope_mode: str = "unrestricted"
    scopes: tuple[str, ...] = ()
    authorization_version: int = 1
    role_assignments: tuple[RoleAssignment, ...] = ()
    token_type: str = "first_party_access_token"
    issued_at: datetime = datetime.min.replace(tzinfo=timezone.utc)
    expires_at: datetime = datetime.max.replace(tzinfo=timezone.utc)
    delegated_by_client_id: str | None = None
    agent_run_id: str | None = None
    tool_call_id: str | None = None

    @property
    def permissions(self) -> tuple[str, ...]:
        return tuple(sorted({permission for assignment in self.role_assignments for permission in assignment.permissions}))


def allows_permissions(context: AuthorizationContext, required_permissions: tuple[str, ...]) -> bool:
    granted = set(context.permissions)
    return bool(required_permissions) and all(permission in granted for permission in required_permissions)


def allows_delegated_tool(context: AuthorizationContext, audience: str, required_scopes: tuple[str, ...]) -> bool:
    if context.token_type != "delegated_access_token":
        return True
    return audience in context.audiences and bool(required_scopes) and all(
        scope in context.scopes for scope in required_scopes
    )


def _require_object(value: object, field_name: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError(f"authorization context {field_name} must be an object")
    return value


def _require_fields(value: dict[str, Any], field_name: str, expected: set[str]) -> None:
    actual = set(value)
    if actual != expected:
        raise ValueError(f"authorization context {field_name} fields are invalid")


def _parse_id(value: object, field_name: str) -> int:
    if not isinstance(value, str) or not value.isdigit() or value.startswith("0"):
        raise ValueError(f"authorization context {field_name} must be a positive decimal string")
    parsed = int(value)
    if parsed <= 0:
        raise ValueError(f"authorization context {field_name} must be positive")
    return parsed


def _parse_time(value: object, field_name: str) -> datetime:
    if not isinstance(value, str) or not value:
        raise ValueError(f"authorization context {field_name} must be an ISO 8601 string")
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def _parse_optional_time(value: object, field_name: str) -> datetime | None:
    return None if value is None else _parse_time(value, field_name)


def _parse_string_tuple(value: object, field_name: str, *, nonempty: bool = False) -> tuple[str, ...]:
    if not isinstance(value, list) or any(not isinstance(item, str) or not item for item in value):
        raise ValueError(f"authorization context {field_name} must be a string array")
    parsed = tuple(value)
    if len(set(parsed)) != len(parsed) or (nonempty and not parsed):
        raise ValueError(f"authorization context {field_name} is invalid")
    return parsed


def _parse_role_assignments(value: object) -> tuple[RoleAssignment, ...]:
    if not isinstance(value, list):
        raise ValueError("authorization context authorization.role_assignments must be an array")
    assignments = []
    for index, raw in enumerate(value):
        field = f"authorization.role_assignments[{index}]"
        assignment = _require_object(raw, field)
        _require_fields(
            assignment,
            field,
            {"assignment_id", "role_key", "scope", "permissions", "source_type", "valid_from", "valid_until"},
        )
        role_key = assignment["role_key"]
        if not isinstance(role_key, str) or not role_key:
            raise ValueError(f"authorization context {field}.role_key is invalid")
        scope = _require_object(assignment["scope"], f"{field}.scope")
        scope_type = scope.get("type")
        if scope_type not in {"platform", "tenant", "department", "project_group"}:
            raise ValueError(f"authorization context {field}.scope.type is invalid")
        _parse_time(assignment["valid_from"], f"{field}.valid_from")
        _parse_optional_time(assignment["valid_until"], f"{field}.valid_until")
        assignments.append(
            RoleAssignment(
                assignment_id=_parse_id(assignment["assignment_id"], f"{field}.assignment_id"),
                role_key=role_key,
                scope_type=scope_type,
                permissions=_parse_string_tuple(assignment["permissions"], f"{field}.permissions", nonempty=True),
            )
        )
    return tuple(assignments)


async def resolve_authorization_context(system_url: str, token: str) -> AuthorizationContext:
    """Resolve a user access token through the canonical System AuthContext v1 API."""
    async with SystemClient(base_url=system_url, user_token=token) as client:
        data = await client.get_authorization_context()

    root = _require_object(data, "root")
    _require_fields(
        root,
        "root",
        {"schema_version", "principal", "context", "authentication", "client", "organization", "authorization", "token", "delegation"},
    )
    if root["schema_version"] != AUTH_CONTEXT_SCHEMA_VERSION:
        raise ValueError(f"authorization context schema_version must be {AUTH_CONTEXT_SCHEMA_VERSION}")

    principal = _require_object(root["principal"], "principal")
    _require_fields(principal, "principal", {"type", "id"})
    if principal["type"] != "user":
        raise ValueError("authorization context principal.type must be user")

    session_context = _require_object(root["context"], "context")
    context_type = session_context.get("type")
    if context_type == "platform":
        _require_fields(session_context, "context", {"type"})
        tenant_id = None
        tenant_membership_id = None
    elif context_type == "tenant":
        _require_fields(session_context, "context", {"type", "tenant_id", "tenant_membership_id"})
        tenant_id = _parse_id(session_context["tenant_id"], "context.tenant_id")
        tenant_membership_id = _parse_id(session_context["tenant_membership_id"], "context.tenant_membership_id")
    else:
        raise ValueError("authorization context context.type is invalid")

    authentication = _require_object(root["authentication"], "authentication")
    _require_fields(authentication, "authentication", {"methods", "assurance_level", "authenticated_at", "step_up_expires_at"})
    assurance_level = authentication["assurance_level"]
    if assurance_level not in {"aal1", "aal2", "aal3", "not_applicable"}:
        raise ValueError("authorization context authentication.assurance_level is invalid")

    client = _require_object(root["client"], "client")
    _require_fields(client, "client", {"client_id", "audiences", "scope_mode", "scopes"})
    if client["client_id"] is not None and (not isinstance(client["client_id"], str) or not client["client_id"]):
        raise ValueError("authorization context client.client_id is invalid")
    if client["scope_mode"] not in {"unrestricted", "restricted"}:
        raise ValueError("authorization context client.scope_mode is invalid")

    authorization = _require_object(root["authorization"], "authorization")
    _require_fields(authorization, "authorization", {"authorization_version", "role_assignments"})
    token_facts = _require_object(root["token"], "token")
    _require_fields(token_facts, "token", {"type", "issued_at", "expires_at"})
    token_type = token_facts["type"]
    if token_type not in {"first_party_access_token", "oauth_access_token", "delegated_access_token"}:
        raise ValueError("authorization context token.type is not a user access token")

    delegation = root["delegation"]
    delegated_by_client_id = agent_run_id = tool_call_id = None
    if delegation is not None:
        delegation = _require_object(delegation, "delegation")
        _require_fields(delegation, "delegation", {"delegated_by_client_id", "agent_run_id", "tool_call_id"})
        delegated_by_client_id = delegation["delegated_by_client_id"]
        agent_run_id = delegation["agent_run_id"]
        tool_call_id = delegation["tool_call_id"]
        if any(not isinstance(value, str) or not value for value in (delegated_by_client_id, agent_run_id, tool_call_id)):
            raise ValueError("authorization context delegation is invalid")
    if (token_type == "delegated_access_token") != (delegation is not None):
        raise ValueError("authorization context delegation does not match token.type")

    context = AuthorizationContext(
        principal_id=_parse_id(principal["id"], "principal.id"),
        principal_type="user",
        context_type=context_type,
        tenant_id=tenant_id,
        tenant_membership_id=tenant_membership_id,
        authentication_methods=_parse_string_tuple(authentication["methods"], "authentication.methods", nonempty=True),
        assurance_level=assurance_level,
        authenticated_at=_parse_time(authentication["authenticated_at"], "authentication.authenticated_at"),
        step_up_expires_at=_parse_optional_time(authentication["step_up_expires_at"], "authentication.step_up_expires_at"),
        client_id=client["client_id"],
        audiences=_parse_string_tuple(client["audiences"], "client.audiences", nonempty=True),
        scope_mode=client["scope_mode"],
        scopes=_parse_string_tuple(client["scopes"], "client.scopes"),
        authorization_version=_parse_id(authorization["authorization_version"], "authorization.authorization_version"),
        role_assignments=_parse_role_assignments(authorization["role_assignments"]),
        token_type=token_type,
        issued_at=_parse_time(token_facts["issued_at"], "token.issued_at"),
        expires_at=_parse_time(token_facts["expires_at"], "token.expires_at"),
        delegated_by_client_id=delegated_by_client_id,
        agent_run_id=agent_run_id,
        tool_call_id=tool_call_id,
    )
    if context.scope_mode == "unrestricted" and context.scopes:
        raise ValueError("authorization context unrestricted client must not contain scopes")
    if context.scope_mode == "restricted" and not context.scopes:
        raise ValueError("authorization context restricted client must contain scopes")
    if context.token_type == "delegated_access_token" and (len(context.audiences) != 1 or not context.scopes):
        raise ValueError("delegated authorization context must contain one audience and non-empty scopes")
    return context
