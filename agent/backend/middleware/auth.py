import httpx
from fastapi import HTTPException, Request, status
from fastapi.responses import JSONResponse

from addp_common.auth import allows_permissions, resolve_authorization_context
from config import settings


async def auth_middleware(request: Request, call_next):
    # 健康检查不需要认证
    if request.url.path in ["/health/live", "/health/ready", "/docs", "/openapi.json"]:
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

    if authorization_context.principal_type != "user":
        return JSONResponse(
            status_code=status.HTTP_403_FORBIDDEN,
            content={"error": "此接口只接受用户身份"},
        )

    request.state.authorization_context = authorization_context
    request.state.principal_id = authorization_context.principal_id
    request.state.tenant_id = authorization_context.tenant_id
    request.state.token = token

    return await call_next(request)


def require_permissions(*required_permissions: str):
    async def dependency(request: Request) -> None:
        context = request.state.authorization_context
        if context.context_type != "tenant" or context.tenant_id is None:
            raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="此接口需要租户上下文")
        if context.token_type == "delegated_access_token":
            raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="委托令牌不能访问该接口")
        if not allows_permissions(context, tuple(required_permissions)):
            raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="权限不足")

    return dependency


def require_context_permissions(*required_permissions: str):
    async def dependency(request: Request) -> None:
        context = request.state.authorization_context
        if context.context_type not in {"platform", "tenant"}:
            raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="不支持的授权上下文")
        if context.context_type == "tenant" and context.tenant_id is None:
            raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="Tenant 上下文无效")
        if context.token_type == "delegated_access_token":
            raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="委托令牌不能访问该接口")
        if not allows_permissions(context, tuple(required_permissions)):
            raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="权限不足")

    return dependency
