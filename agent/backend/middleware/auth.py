from fastapi import Request, HTTPException, status
from fastapi.responses import JSONResponse
from jose import jwt, JWTError
from config import settings


async def auth_middleware(request: Request, call_next):
    # 健康检查不需要认证
    if request.url.path in ["/health", "/docs", "/openapi.json"]:
        return await call_next(request)

    auth_header = request.headers.get("Authorization")
    if not auth_header or not auth_header.startswith("Bearer "):
        return JSONResponse(
            status_code=status.HTTP_401_UNAUTHORIZED,
            content={"code": 401, "message": "未提供认证 Token"},
        )

    token = auth_header[7:]
    try:
        payload = jwt.decode(token, settings.JWT_SECRET, algorithms=["HS256"])
        request.state.user_id = payload.get("user_id") or payload.get("sub")
        request.state.tenant_id = payload.get("tenant_id", 1)
        request.state.token = token
    except JWTError:
        return JSONResponse(
            status_code=status.HTTP_401_UNAUTHORIZED,
            content={"code": 401, "message": "Token 无效或已过期"},
        )

    return await call_next(request)
