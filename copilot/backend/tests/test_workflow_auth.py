import asyncio
from unittest.mock import AsyncMock

import httpx
import pytest
from fastapi import HTTPException
from fastapi.security import HTTPAuthorizationCredentials
from pydantic import ValidationError

from addp_common.auth import AuthorizationContext
from api.workflow_agent_api import WorkflowGenerationRequest
from dependencies import auth


def test_workflow_request_rejects_client_supplied_identity():
    with pytest.raises(ValidationError):
        WorkflowGenerationRequest(
            query="分析铁路",
            workflow_engine_id=12,
            resources=[],
            tenant_id=99,
            user_id=88,
        )


def test_workflow_auth_uses_system_verified_identity(monkeypatch):
    expected = AuthorizationContext(user_id=7, tenant_id=3, username="tester", user_type="user")
    verifier = AsyncMock(return_value=expected)
    monkeypatch.setattr(auth, "resolve_authorization_context", verifier)

    result = asyncio.run(auth.require_user(HTTPAuthorizationCredentials(
        scheme="Bearer",
        credentials="user-token",
    )))

    assert result == expected
    verifier.assert_awaited_once_with(auth.settings.get_system_url(), "user-token")


def test_workflow_auth_rejects_invalid_token(monkeypatch):
    monkeypatch.setattr(
        auth,
        "resolve_authorization_context",
        AsyncMock(side_effect=httpx.HTTPStatusError(
            "unauthorized",
            request=httpx.Request("GET", "http://system/api/v1/system/auth/context"),
            response=httpx.Response(401),
        )),
    )

    with pytest.raises(HTTPException) as exc_info:
        asyncio.run(auth.require_user(HTTPAuthorizationCredentials(
            scheme="Bearer",
            credentials="bad-token",
        )))

    assert exc_info.value.status_code == 401


def test_workflow_tool_auth_requires_delegated_audience_and_scope(monkeypatch):
    dependency = auth.require_tool_user("copilot", "workflow.draft.generate")
    valid = AuthorizationContext(
        user_id=7,
        tenant_id=3,
        username="tester",
        user_type="user",
        auth_type="delegated_access_token",
        audiences=("copilot",),
        scopes=("workflow.draft.generate",),
    )
    monkeypatch.setattr(auth, "resolve_authorization_context", AsyncMock(return_value=valid))
    result = asyncio.run(dependency(HTTPAuthorizationCredentials(scheme="Bearer", credentials="delegated")))
    assert result == valid

    invalid = AuthorizationContext(
        user_id=7,
        tenant_id=3,
        username="tester",
        user_type="user",
        auth_type="delegated_access_token",
        audiences=("develop",),
        scopes=("workflow.draft.generate",),
    )
    monkeypatch.setattr(auth, "resolve_authorization_context", AsyncMock(return_value=invalid))
    with pytest.raises(HTTPException) as exc_info:
        asyncio.run(dependency(HTTPAuthorizationCredentials(scheme="Bearer", credentials="delegated")))
    assert exc_info.value.status_code == 403


def test_workflow_openapi_declares_bearer_auth():
    from main import app

    specification = app.openapi()
    operation = specification["paths"]["/workflow/generate"]["post"]
    assert operation["security"] == [{"BearerAuth": []}]
    request_schema = specification["components"]["schemas"]["WorkflowGenerationRequest"]
    assert "resources" in request_schema["required"]
    assert specification["components"]["securitySchemes"]["BearerAuth"]["scheme"] == "bearer"
