import httpx
from fastapi import Depends, Header, HTTPException, status
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from addp_common.auth import AuthorizationContext, allows_delegated_tool, resolve_authorization_context
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
    if context.auth_type == "delegated_access_token":
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail=_message(accept_language, "委托令牌不能访问该接口", "Delegated token cannot access this endpoint"),
        )
    return context


def require_tool_user(audience: str, scope: str):
    async def dependency(
        credentials: HTTPAuthorizationCredentials = Depends(_bearer_auth),
        accept_language: str | None = Header(default=None, alias="Accept-Language"),
    ) -> AuthorizationContext:
        context = await _resolve_user(credentials, accept_language)
        if not allows_delegated_tool(context, audience, (scope,)):
            raise HTTPException(
                status_code=status.HTTP_403_FORBIDDEN,
                detail=_message(accept_language, "委托令牌权限不足", "Delegated token has insufficient permission"),
            )
        return context

    return dependency
