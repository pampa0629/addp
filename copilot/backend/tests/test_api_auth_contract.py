import asyncio

import pytest
from fastapi import HTTPException
from pydantic import ValidationError

from addp_common.auth import AuthorizationContext, RoleAssignment
from api.navigate_api import NavigateRequest
from api.sql_agent_api import SQLGenerationRequest
from dependencies import auth


def test_sql_request_rejects_client_supplied_identity():
    with pytest.raises(ValidationError):
        SQLGenerationRequest(
            query="查询城市",
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


def test_internal_api_key_dependency(monkeypatch):
    monkeypatch.setattr(auth.settings, "internal_api_key", "shared-secret")
    assert asyncio.run(auth.require_internal_api_key("shared-secret", "zh-cn")) is None

    with pytest.raises(HTTPException) as invalid:
        asyncio.run(auth.require_internal_api_key("wrong", "en"))
    assert invalid.value.status_code == 401
    assert invalid.value.detail == "Internal API key is invalid"

    monkeypatch.setattr(auth.settings, "internal_api_key", None)
    with pytest.raises(HTTPException) as unavailable:
        asyncio.run(auth.require_internal_api_key("shared-secret", "en"))
    assert unavailable.value.status_code == 503
    assert unavailable.value.detail == "Internal authentication is not configured"


def test_copilot_openapi_declares_authorization_contracts():
    from main import app

    specification = app.openapi()
    paths = specification["paths"]

    assert paths["/sql/generate"]["post"]["x-addp-auth-mode"] == "permission"
    assert paths["/sql/generate"]["post"]["x-addp-required-permissions"] == [
        "copilot.sql.execute"
    ]
    assert paths["/workflow/generate"]["post"]["x-addp-auth-mode"] == "delegated_tool"
    assert paths["/workflow/generate"]["post"]["x-addp-required-permissions"] == [
        "copilot.workflow.execute"
    ]
    assert paths["/kg-build/extract"]["post"]["x-addp-auth-mode"] == "internal"
    assert "x-addp-required-permissions" not in paths["/kg-build/extract"]["post"]
    assert paths["/navigate/guide"]["post"]["x-addp-auth-mode"] == "authenticated"
    assert paths["/health"]["get"]["x-addp-auth-mode"] == "public"
    assert paths["/"]["get"]["x-addp-auth-mode"] == "public"

    sql_schema = specification["components"]["schemas"]["SQLGenerationRequest"]
    navigate_schema = specification["components"]["schemas"]["NavigateRequest"]
    assert "tenant_id" not in sql_schema.get("properties", {})
    assert "user_id" not in sql_schema.get("properties", {})
    assert "tenant_id" not in navigate_schema.get("properties", {})
    assert "user_id" not in navigate_schema.get("properties", {})
