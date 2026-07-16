import httpx
from fastapi import Depends, HTTPException, status
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer

from addp_common.auth import AuthorizationContext, resolve_authorization_context
from config import settings


_bearer_auth = HTTPBearer(
    scheme_name="BearerAuth",
    description="ADDP 用户访问令牌：Authorization: Bearer <token>",
)


async def require_user(
    credentials: HTTPAuthorizationCredentials = Depends(_bearer_auth),
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
            detail="Token 无效或已过期" if status_code == 401 else "System 认证服务不可用",
        ) from exc
    except (httpx.RequestError, ValueError) as exc:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail="System 认证服务不可用",
        ) from exc
