import asyncio
from unittest.mock import AsyncMock

import httpx
import pytest
from fastapi import HTTPException
from fastapi.security import HTTPAuthorizationCredentials
from pydantic import ValidationError

from addp_common.auth import AuthorizationContext, RoleAssignment
from api import workflow_agent_api
from api.workflow_agent_api import WorkflowGenerationRequest
from dependencies import auth
from services.resource_discovery import ResourceDiscoveryResult


def test_workflow_request_rejects_client_supplied_identity():
    with pytest.raises(ValidationError):
        WorkflowGenerationRequest(
            query="分析铁路",
            workflow_engine_id=12,
            resources=[],
            tenant_id=99,
            user_id=88,
        )


def test_workflow_request_allows_resource_discovery():
    request = WorkflowGenerationRequest(query="分析铁路与耕地", workflow_engine_id=12)

    assert request.resources == []


def test_user_request_without_resources_returns_verified_candidates(monkeypatch):
    locator = "addp://engine/8/path/public/railway?type=table&item_id=60"

    class Discovery:
        def __init__(self, gateway_url, source_token, **kwargs):
            assert gateway_url == "http://gateway:8000"
            assert source_token == "addp_at_user"
            assert "recommender" in kwargs

        async def discover(self, intents, **_kwargs):
            assert [intent.model_dump() for intent in intents] == [
                {"role": "铁路", "search_queries": ["铁路"]},
            ]
            return ResourceDiscoveryResult(candidates=[{
                "role": "input_1",
                "name": "railway",
                "locator": locator,
                "engine_id": 8,
                "asset_type": "table",
                "data_type": "table",
                "geometry_column": "shape",
                "geometry_type": "LineString",
                "crs": "EPSG:32650",
                "fields": [],
                "ancestors": [],
            }], missing_roles=[])

    class IntentChain:
        def __init__(self, llm):
            assert llm == "resource-intent-llm"

        async def extract(self, query, *, scope=None):
            assert query == "分析铁路"
            from chains.resource_intent_chain import ResourceIntent
            return [ResourceIntent(role="铁路", search_queries=["铁路"])]

    monkeypatch.setattr(workflow_agent_api, "ResourceDiscovery", Discovery)
    monkeypatch.setattr(workflow_agent_api, "ResourceIntentChain", IntentChain)
    monkeypatch.setattr(
        workflow_agent_api.CopilotInferenceService,
        "chat_model",
        lambda *_args, **_kwargs: "resource-intent-llm",
    )
    monkeypatch.setattr(workflow_agent_api.settings, "gateway_url", "http://gateway:8000")
    monkeypatch.setattr(
        workflow_agent_api,
        "get_workflow_service",
        lambda *_args: (_ for _ in ()).throw(AssertionError("service must wait for resource confirmation")),
    )
    user = AuthorizationContext(
        principal_id=7,
        tenant_id=3,
        tenant_membership_id=9,
        token_type="first_party_access_token",
    )

    response = asyncio.run(workflow_agent_api.generate_workflow(
        WorkflowGenerationRequest(query="分析铁路", workflow_engine_id=12),
        user,
        HTTPAuthorizationCredentials(scheme="Bearer", credentials="addp_at_user"),
        None,
    ))

    assert response.status == "need_clarification"
    assert response.clarification_reason == "data_source_confirmation_required"
    assert response.data_source_candidates[0].locator == locator


def test_user_request_returns_all_ambiguous_candidates_after_recommendation(monkeypatch):
    locators = [
        "addp://engine/8/path/public/farmland?type=table&item_id=61",
        "addp://engine/9/path/public/farmland?type=table&item_id=71",
    ]

    class Discovery:
        def __init__(self, gateway_url, source_token, **kwargs):
            assert "recommender" in kwargs

        async def discover(self, intents, **_kwargs):
            return ResourceDiscoveryResult(candidates=[{
                "role": "耕地",
                "name": "farmland",
                "locator": locator,
                "engine_id": engine_id,
                "engine_name": f"spatial-{engine_id}",
                "asset_type": "table",
                "data_type": "table",
                "geometry_column": "shape",
                "geometry_type": "Polygon",
                "crs": "EPSG:32650",
                "fields": [],
                "ancestors": [],
                "recommended": index == 0,
                "recommendation_reason": "名称和空间类型匹配" if index == 0 else None,
            } for index, (engine_id, locator) in enumerate(zip((8, 9), locators))], missing_roles=[])

    class IntentChain:
        def __init__(self, llm):
            pass

        async def extract(self, query, *, scope=None):
            from chains.resource_intent_chain import ResourceIntent
            return [ResourceIntent(role="耕地", search_queries=["farmland"])]

    monkeypatch.setattr(workflow_agent_api, "ResourceDiscovery", Discovery)
    monkeypatch.setattr(workflow_agent_api, "ResourceIntentChain", IntentChain)
    monkeypatch.setattr(workflow_agent_api.CopilotInferenceService, "chat_model", lambda *_args, **_kwargs: object())
    user = AuthorizationContext(
        principal_id=7,
        tenant_id=3,
        tenant_membership_id=9,
        token_type="first_party_access_token",
    )

    response = asyncio.run(workflow_agent_api.generate_workflow(
        WorkflowGenerationRequest(query="分析 farmland", workflow_engine_id=12),
        user,
        HTTPAuthorizationCredentials(scheme="Bearer", credentials="addp_at_user"),
        None,
    ))

    assert response.clarification_reason == "data_source_ambiguous"
    assert [candidate.locator for candidate in response.data_source_candidates] == locators
    assert response.data_source_candidates[0].recommended is True
    assert response.data_source_candidates[1].recommended is False


def test_user_request_retries_only_missing_resource_roles(monkeypatch):
    railway_locator = "addp://engine/8/path/public/railway?type=table&item_id=60"
    farmland_locator = "addp://engine/8/path/public/farmland?type=table&item_id=61"

    class Discovery:
        calls = []

        def __init__(self, gateway_url, source_token, **kwargs):
            pass

        async def discover(self, intents, **_kwargs):
            self.calls.append([intent.model_dump() for intent in intents])
            if len(self.calls) == 1:
                return ResourceDiscoveryResult(candidates=[{
                    "role": "铁路",
                    "name": "railway",
                    "locator": railway_locator,
                    "engine_id": 8,
                    "asset_type": "table",
                    "data_type": "table",
                    "geometry_column": "geom",
                    "geometry_type": "LineString",
                    "crs": "EPSG:32650",
                    "fields": [],
                    "ancestors": [],
                }], missing_roles=["耕地"])
            return ResourceDiscoveryResult(candidates=[{
                "role": "耕地",
                "name": "farmland",
                "locator": farmland_locator,
                "engine_id": 8,
                "asset_type": "table",
                "data_type": "table",
                "geometry_column": "geometry",
                "geometry_type": "Polygon",
                "crs": "EPSG:32650",
                "fields": [],
                "ancestors": [],
            }], missing_roles=[])

    class IntentChain:
        def __init__(self, llm):
            pass

        async def extract(self, query, *, scope=None):
            from chains.resource_intent_chain import ResourceIntent
            return [
                ResourceIntent(role="铁路", search_queries=["铁路", "railway"]),
                ResourceIntent(
                    role="耕地",
                    search_queries=["耕地", "cultivated land", "cropland", "agricultural land"],
                ),
            ]

        async def expand_missing(self, query, intents):
            assert [intent.role for intent in intents] == ["耕地"]
            from chains.resource_intent_chain import ResourceIntent
            return [ResourceIntent(role="耕地", search_queries=["farmland"])]

    monkeypatch.setattr(workflow_agent_api, "ResourceDiscovery", Discovery)
    monkeypatch.setattr(workflow_agent_api, "ResourceIntentChain", IntentChain)
    monkeypatch.setattr(workflow_agent_api.CopilotInferenceService, "chat_model", lambda *_args, **_kwargs: object())
    monkeypatch.setattr(
        workflow_agent_api,
        "get_workflow_service",
        lambda *_args: (_ for _ in ()).throw(AssertionError("service must wait for resource confirmation")),
    )
    user = AuthorizationContext(
        principal_id=7,
        tenant_id=3,
        tenant_membership_id=9,
        token_type="first_party_access_token",
    )

    response = asyncio.run(workflow_agent_api.generate_workflow(
        WorkflowGenerationRequest(
            query="计算铁路两边宽度50米所占用的耕地面积",
            workflow_engine_id=12,
        ),
        user,
        HTTPAuthorizationCredentials(scheme="Bearer", credentials="addp_at_user"),
        None,
    ))

    assert response.status == "need_clarification"
    assert response.clarification_reason == "data_source_confirmation_required"
    assert [candidate.locator for candidate in response.data_source_candidates] == [
        railway_locator,
        farmland_locator,
    ]
    assert Discovery.calls == [
        [
            {"role": "铁路", "search_queries": ["铁路", "railway"]},
            {
                "role": "耕地",
                "search_queries": ["耕地", "cultivated land", "cropland", "agricultural land"],
            },
        ],
        [{"role": "耕地", "search_queries": ["farmland"]}],
    ]


def test_workflow_auth_uses_system_verified_identity(monkeypatch):
    expected = AuthorizationContext(principal_id=7, tenant_id=3, tenant_membership_id=9)
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


def test_workflow_auth_rejects_service_principal(monkeypatch):
    service_context = AuthorizationContext(
        principal_id=41,
        principal_type="service_principal",
        tenant_id=3,
        tenant_membership_id=8,
        client_id="addp-develop",
        scope_mode="restricted",
        scopes=("addp.api",),
        token_type="service_access_token",
    )
    monkeypatch.setattr(
        auth,
        "resolve_authorization_context",
        AsyncMock(return_value=service_context),
    )

    with pytest.raises(HTTPException) as exc_info:
        asyncio.run(auth.require_user(HTTPAuthorizationCredentials(
            scheme="Bearer",
            credentials="service-token",
        )))

    assert exc_info.value.status_code == 403
    assert exc_info.value.detail == "此接口只接受普通用户令牌"


def test_workflow_tool_auth_requires_delegated_audience_and_scope(monkeypatch):
    dependency = auth.require_tool_user("copilot", "workflow.draft.generate", "copilot.workflow.execute")
    valid = AuthorizationContext(
        principal_id=7,
        tenant_id=3,
        tenant_membership_id=9,
        token_type="delegated_access_token",
        audiences=("copilot",),
        scope_mode="restricted",
        scopes=("workflow.draft.generate",),
        role_assignments=(
            RoleAssignment(1, "tenant.ai_user", "tenant", ("copilot.workflow.execute",)),
        ),
    )
    monkeypatch.setattr(auth, "resolve_authorization_context", AsyncMock(return_value=valid))
    result = asyncio.run(dependency(HTTPAuthorizationCredentials(scheme="Bearer", credentials="delegated")))
    assert result == valid

    invalid = AuthorizationContext(
        principal_id=7,
        tenant_id=3,
        tenant_membership_id=9,
        token_type="delegated_access_token",
        audiences=("develop",),
        scope_mode="restricted",
        scopes=("workflow.draft.generate",),
        role_assignments=(
            RoleAssignment(1, "tenant.ai_user", "tenant", ("copilot.workflow.execute",)),
        ),
    )
    monkeypatch.setattr(auth, "resolve_authorization_context", AsyncMock(return_value=invalid))
    with pytest.raises(HTTPException) as exc_info:
        asyncio.run(dependency(HTTPAuthorizationCredentials(scheme="Bearer", credentials="delegated")))
    assert exc_info.value.status_code == 403

    missing_permission = AuthorizationContext(
        principal_id=7,
        tenant_id=3,
        tenant_membership_id=9,
        token_type="delegated_access_token",
        audiences=("copilot",),
        scope_mode="restricted",
        scopes=("workflow.draft.generate",),
    )
    monkeypatch.setattr(auth, "resolve_authorization_context", AsyncMock(return_value=missing_permission))
    with pytest.raises(HTTPException) as exc_info:
        asyncio.run(dependency(HTTPAuthorizationCredentials(scheme="Bearer", credentials="delegated")))
    assert exc_info.value.status_code == 403


def test_workflow_openapi_declares_bearer_auth():
    from main import app

    specification = app.openapi()
    operation = specification["paths"]["/workflow/generate"]["post"]
    assert operation["security"] == [{"BearerAuth": []}]
    request_schema = specification["components"]["schemas"]["WorkflowGenerationRequest"]
    candidate_schema = specification["components"]["schemas"]["WorkflowResourceCandidate"]
    assert "resources" not in request_schema.get("required", [])
    assert candidate_schema["properties"]["recommended"]["type"] == "boolean"
    assert "recommendation_reason" in candidate_schema["properties"]
    assert specification["components"]["securitySchemes"]["BearerAuth"]["scheme"] == "bearer"
