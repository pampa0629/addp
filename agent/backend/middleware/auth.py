import httpx
from fastapi import Request, status
from fastapi.responses import JSONResponse

from addp_common.auth import resolve_authorization_context
from config import settings


async def auth_middleware(request: Request, call_next):
    # 健康检查不需要认证
    if request.url.path in ["/health", "/docs", "/openapi.json"]:
        return await call_next(request)

    auth_header = request.headers.get("Authorization")
    if not auth_header or not auth_header.startswith("Bearer "):
        return JSONResponse(
            status_code=status.HTTP_401_UNAUTHORIZED,
            content={"error": "未提供认证 Token"},
        )

    token = auth_header[7:].strip()
    if not token:
        return JSONResponse(
            status_code=status.HTTP_401_UNAUTHORIZED,
            content={"error": "未提供认证 Token"},
        )
    try:
        authorization_context = await resolve_authorization_context(settings.get_system_url(), token)
    except httpx.HTTPStatusError as exc:
        status_code = (
            status.HTTP_401_UNAUTHORIZED
            if exc.response.status_code in {status.HTTP_401_UNAUTHORIZED, status.HTTP_403_FORBIDDEN}
            else status.HTTP_503_SERVICE_UNAVAILABLE
        )
        return JSONResponse(
            status_code=status_code,
            content={"error": "Token 无效或已过期" if status_code == 401 else "System 认证服务不可用"},
        )
    except (httpx.RequestError, ValueError):
        return JSONResponse(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            content={"error": "System 认证服务不可用"},
        )

    request.state.authorization_context = authorization_context
    request.state.user_id = authorization_context.user_id
    request.state.tenant_id = authorization_context.tenant_id or 0
    request.state.token = token

    return await call_next(request)
