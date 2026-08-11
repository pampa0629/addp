import asyncio

import pytest
from fastapi import HTTPException
from pydantic import ValidationError

from addp_common.auth import AuthorizationContext, RoleAssignment
from api.navigate_api import NavigateRequest
from api.query_agent_api import QueryGenerationRequest
from dependencies import auth


def test_query_request_rejects_client_supplied_identity():
    with pytest.raises(ValidationError):
        QueryGenerationRequest(
            query="查询城市",
            engine_id=8,
            query_language="sql",
            tenant_id=99,
            user_id=88,
        )


def test_navigate_request_rejects_client_supplied_identity():
    with pytest.raises(ValidationError):
        NavigateRequest(
            query="打开数据资产",
            tenant_id=99,
            user_id=88,
        )


def test_tenant_user_rejects_tenantless_context(monkeypatch):
    async def tenantless_user(*_args, **_kwargs):
        return AuthorizationContext(
            principal_id=1,
            tenant_id=None,
            context_type="platform",
        )

    monkeypatch.setattr(auth, "require_user", tenantless_user)
    with pytest.raises(HTTPException) as exc_info:
        asyncio.run(auth.require_tenant_user(None, "en"))
    assert exc_info.value.status_code == 403
    assert exc_info.value.detail == "This endpoint requires a tenant context"


def test_tenant_permission_dependency_requires_role_permission(monkeypatch):
    dependency = auth.require_tenant_permissions("copilot.sql.execute")

    async def allowed(*_args, **_kwargs):
        return AuthorizationContext(
            principal_id=1,
            tenant_id=3,
            tenant_membership_id=8,
            role_assignments=(
                RoleAssignment(1, "tenant.ai_user", "tenant", ("copilot.sql.execute",)),
            ),
        )

    monkeypatch.setattr(auth, "require_tenant_user", allowed)
    assert asyncio.run(dependency(None, "en")).principal_id == 1

    async def denied(*_args, **_kwargs):
        return AuthorizationContext(principal_id=1, tenant_id=3, tenant_membership_id=8)

    monkeypatch.setattr(auth, "require_tenant_user", denied)
    with pytest.raises(HTTPException) as exc_info:
        asyncio.run(dependency(None, "en"))
    assert exc_info.value.status_code == 403
    assert exc_info.value.detail == "Insufficient permission"


def test_tenant_service_dependency_requires_bound_client_and_permission(monkeypatch):
    dependency = auth.require_tenant_service("addp-graph", "copilot.knowledge_graph.execute")

    async def allowed(*_args, **_kwargs):
        return AuthorizationContext(
            principal_id=11,
            principal_type="service_principal",
            token_type="service_access_token",
            client_id="addp-graph",
            context_type="tenant",
            tenant_id=7,
            tenant_membership_id=9,
            role_assignments=(
                RoleAssignment(4, "tenant.graph_runtime", "tenant", ("copilot.knowledge_graph.execute",)),
            ),
        )

    monkeypatch.setattr(auth, "_resolve_user", allowed)
    assert asyncio.run(dependency(None, "en")).tenant_id == 7

    async def wrong_client(*_args, **_kwargs):
        return AuthorizationContext(
            principal_id=12,
            principal_type="service_principal",
            token_type="service_access_token",
            client_id="addp-copilot",
            context_type="tenant",
            tenant_id=7,
            tenant_membership_id=10,
            role_assignments=(
                RoleAssignment(5, "tenant.copilot_runtime", "tenant", ("copilot.knowledge_graph.execute",)),
            ),
        )

    monkeypatch.setattr(auth, "_resolve_user", wrong_client)
    with pytest.raises(HTTPException) as denied:
        asyncio.run(dependency(None, "en"))
    assert denied.value.status_code == 403
    assert denied.value.detail == "Service token has insufficient permission"


def test_copilot_openapi_declares_authorization_contracts():
    from main import app

    specification = app.openapi()
    paths = specification["paths"]

    assert paths["/query/generate"]["post"]["x-addp-auth-mode"] == "delegated_tool"
    assert paths["/query/generate"]["post"]["x-addp-required-permissions"] == [
        "copilot.sql.execute"
    ]
    assert {"400", "500", "502"}.issubset(paths["/query/generate"]["post"]["responses"])
    assert paths["/notebook/generate"]["post"]["x-addp-auth-mode"] == "delegated_tool"
    assert paths["/notebook/generate"]["post"]["x-addp-required-permissions"] == [
        "copilot.notebook.execute"
    ]
    assert paths["/workflow/generate"]["post"]["x-addp-auth-mode"] == "delegated_tool"
    assert paths["/workflow/generate"]["post"]["x-addp-required-permissions"] == [
        "copilot.workflow.execute"
    ]
    assert paths["/transfer/generate"]["post"]["x-addp-auth-mode"] == "delegated_tool"
    assert paths["/transfer/generate"]["post"]["x-addp-required-permissions"] == [
        "copilot.transfer.execute",
    ]
    assert paths["/kg-build/extract"]["post"]["x-addp-auth-mode"] == "permission"
    assert paths["/kg-build/extract"]["post"]["x-addp-required-permissions"] == [
        "copilot.knowledge_graph.execute"
    ]
    assert paths["/navigate/guide"]["post"]["x-addp-auth-mode"] == "authenticated"
    assert paths["/settings/inference-bindings/{scenario_code}"]["get"]["x-addp-required-permissions"] == [
        "copilot.configuration.read"
    ]
    assert paths["/settings/inference-bindings/{scenario_code}"]["put"]["x-addp-required-permissions"] == [
        "copilot.configuration.update"
    ]
    assert paths["/health"]["get"]["x-addp-auth-mode"] == "public"
    assert paths["/"]["get"]["x-addp-auth-mode"] == "public"

    query_schema = specification["components"]["schemas"]["QueryGenerationRequest"]
    navigate_schema = specification["components"]["schemas"]["NavigateRequest"]
    assert "tenant_id" not in query_schema.get("properties", {})
    assert "user_id" not in query_schema.get("properties", {})
    assert query_schema["properties"]["current_query"]["anyOf"][0]["type"] == "string"
    assert "tenant_id" not in navigate_schema.get("properties", {})
    assert "user_id" not in navigate_schema.get("properties", {})
