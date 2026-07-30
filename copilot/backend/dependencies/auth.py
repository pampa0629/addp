import hmac

import httpx
from fastapi import Depends, Header, HTTPException, status
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from addp_common.auth import AuthorizationContext, allows_delegated_tool, allows_permissions, resolve_authorization_context
from config import settings


_bearer_auth = HTTPBearer(
    scheme_name="BearerAuth",
    description="ADDP 用户访问令牌：Authorization: Bearer <token>",
)


def _message(accept_language: str | None, zh_cn: str, en: str) -> str:
    language = accept_language if isinstance(accept_language, str) else ""
    return en if language.lower().startswith("en") else zh_cn


async def _resolve_user(
    credentials: HTTPAuthorizationCredentials = Depends(_bearer_auth),
    accept_language: str | None = Header(default=None, alias="Accept-Language"),
) -> AuthorizationContext:
    try:
        return await resolve_authorization_context(settings.get_system_url(), credentials.credentials)
    except httpx.HTTPStatusError as exc:
        status_code = (
            status.HTTP_401_UNAUTHORIZED
            if exc.response.status_code in {status.HTTP_401_UNAUTHORIZED, status.HTTP_403_FORBIDDEN}
            else status.HTTP_503_SERVICE_UNAVAILABLE
        )
        raise HTTPException(
            status_code=status_code,
            detail=(
                _message(accept_language, "Token 无效或已过期", "Token is invalid or expired")
                if status_code == 401
                else _message(accept_language, "System 认证服务不可用", "System authentication service is unavailable")
            ),
        ) from exc
    except (httpx.RequestError, ValueError) as exc:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail=_message(accept_language, "System 认证服务不可用", "System authentication service is unavailable"),
        ) from exc


async def require_user(
    credentials: HTTPAuthorizationCredentials = Depends(_bearer_auth),
    accept_language: str | None = Header(default=None, alias="Accept-Language"),
) -> AuthorizationContext:
    context = await _resolve_user(credentials, accept_language)
    if context.principal_type != "user" or context.token_type in {"delegated_access_token", "service_access_token"}:
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail=_message(
                accept_language,
                "此接口只接受普通用户令牌",
                "This endpoint only accepts regular user tokens",
            ),
        )
    return context


async def require_tenant_user(
    credentials: HTTPAuthorizationCredentials = Depends(_bearer_auth),
    accept_language: str | None = Header(default=None, alias="Accept-Language"),
) -> AuthorizationContext:
    context = await require_user(credentials, accept_language)
    if context.tenant_id is None:
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail=_message(
                accept_language,
                "此接口需要租户上下文",
                "This endpoint requires a tenant context",
            ),
        )
    return context


def require_tenant_permissions(*required_permissions: str):
    async def dependency(
        credentials: HTTPAuthorizationCredentials = Depends(_bearer_auth),
        accept_language: str | None = Header(default=None, alias="Accept-Language"),
    ) -> AuthorizationContext:
        context = await require_tenant_user(credentials, accept_language)
        if not allows_permissions(context, tuple(required_permissions)):
            raise HTTPException(
                status_code=status.HTTP_403_FORBIDDEN,
                detail=_message(accept_language, "权限不足", "Insufficient permission"),
            )
        return context

    return dependency


def require_tool_user(audience: str, scope: str, *required_permissions: str):
    async def dependency(
        credentials: HTTPAuthorizationCredentials = Depends(_bearer_auth),
        accept_language: str | None = Header(default=None, alias="Accept-Language"),
    ) -> AuthorizationContext:
        context = await _resolve_user(credentials, accept_language)
        if context.tenant_id is None:
            raise HTTPException(
                status_code=status.HTTP_403_FORBIDDEN,
                detail=_message(accept_language, "此接口需要租户上下文", "This endpoint requires a tenant context"),
            )
        if not allows_delegated_tool(context, audience, (scope,)) or not allows_permissions(
            context, tuple(required_permissions)
        ):
            raise HTTPException(
                status_code=status.HTTP_403_FORBIDDEN,
                detail=_message(accept_language, "委托令牌权限不足", "Delegated token has insufficient permission"),
            )
        return context

    return dependency


async def require_internal_api_key(
    internal_api_key: str | None = Header(default=None, alias="X-Internal-API-Key"),
    accept_language: str | None = Header(default=None, alias="Accept-Language"),
) -> None:
    configured_key = settings.internal_api_key
    if not configured_key:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail=_message(
                accept_language,
                "内部认证未配置",
                "Internal authentication is not configured",
            ),
        )
    if not internal_api_key or not hmac.compare_digest(internal_api_key, configured_key):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail=_message(
                accept_language,
                "内部 API Key 无效",
                "Internal API key is invalid",
            ),
        )
