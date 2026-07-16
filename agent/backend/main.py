import asyncio
import logging
import httpx
import uvicorn
from fastapi import Depends, FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.security import HTTPBearer
from starlette.middleware.base import BaseHTTPMiddleware
from utils.logging_setup import setup_logging
from config import settings
from database import init_db
from middleware.auth import auth_middleware
from api.sessions import router as sessions_router
from api.chat import router as chat_router
from api.runs import router as runs_router

# 最先初始化日志（在其他模块 import 之前）
setup_logging()
logger = logging.getLogger(__name__)

_API_PREFIX = "/api/v1/agent"
_bearer_auth = HTTPBearer(
    auto_error=False,
    scheme_name="BearerAuth",
    description="ADDP JWT：Authorization: Bearer <token>",
)

app = FastAPI(
    title="ADDP Agent Service",
    description="ADDP 平台智能体服务 | ADDP Platform Agent Service",
    version="1.0.0",
)

# CORS 配置
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# 认证中间件
app.add_middleware(BaseHTTPMiddleware, dispatch=auth_middleware)

# 注册路由
app.include_router(
    sessions_router,
    prefix=_API_PREFIX,
    dependencies=[Depends(_bearer_auth)],
)
app.include_router(
    chat_router,
    prefix=_API_PREFIX,
    dependencies=[Depends(_bearer_auth)],
)
app.include_router(
    runs_router,
    prefix=_API_PREFIX,
    dependencies=[Depends(_bearer_auth)],
)


@app.get("/health", summary="健康检查 | Health Check")
async def health():
    return {"status": "ok", "module": "agent"}


async def _register_module():
    """向 System 模块注册，并保持心跳；注册失败会在心跳阶段自动重试"""
    if not settings.INTERNAL_API_KEY:
        logger.warning("INTERNAL_API_KEY 未配置，跳过模块注册")
        return

    headers = {
        "X-Internal-API-Key": settings.INTERNAL_API_KEY,
        "Content-Type": "application/json",
    }
    service_url = f"http://{settings.SERVICE_HOST}:{settings.AGENT_BACKEND_PORT}"
    register_url = f"{settings.get_system_url()}/api/v1/internal/modules/register"
    heartbeat_url = f"{settings.get_system_url()}/api/v1/internal/modules/heartbeat"
    register_payload = {
        "module_name": "agent",
        "module_url": service_url,
        "route_prefix": "/agent",
        "health_check_url": f"{service_url}/health",
        "metadata": {"module": "agent", "language": "python"},
    }

    # 禁用系统代理，避免 httpx 自动拾取 macOS 网络代理导致内部请求失败
    _no_proxy_transport = httpx.AsyncHTTPTransport()

    async def _try_register() -> bool:
        """尝试注册一次，成功返回 True"""
        try:
            async with httpx.AsyncClient(timeout=5.0, transport=_no_proxy_transport) as client:
                resp = await client.post(register_url, headers=headers, json=register_payload)
                if resp.status_code < 300:
                    logger.info("✅ Agent 模块注册成功: %s", service_url)
                    return True
                logger.warning("⚠️  Agent 模块注册失败: %s", resp.text)
        except Exception as e:
            logger.warning("⚠️  Agent 模块注册失败: %s", e)
        return False

    # 初始注册，最多重试 3 次
    registered = False
    for attempt in range(1, 4):
        if await _try_register():
            registered = True
            break
        logger.warning("  (尝试 %d/3)", attempt)
        await asyncio.sleep(attempt * 5)

    # 心跳循环：连续失败时自动重新注册
    consecutive_failures = 0
    while True:
        await asyncio.sleep(10)
        try:
            async with httpx.AsyncClient(timeout=5.0, transport=_no_proxy_transport) as client:
                resp = await client.post(heartbeat_url, headers=headers, json={"module_name": "agent"})
                if resp.status_code < 300:
                    consecutive_failures = 0
                    if not registered:
                        logger.info("✅ Agent 心跳恢复正常")
                        registered = True
                else:
                    consecutive_failures += 1
        except Exception as e:
            consecutive_failures += 1
            logger.debug("Agent 心跳失败: %s", e)

        # 连续失败 3 次，尝试重新注册
        if consecutive_failures >= 3:
            logger.warning("⚠️  心跳连续失败 %d 次，尝试重新注册...", consecutive_failures)
            if await _try_register():
                registered = True
                consecutive_failures = 0
            else:
                await asyncio.sleep(20)


@app.on_event("startup")
async def startup():
    await init_db()
    asyncio.create_task(_register_module())


if __name__ == "__main__":
    # 禁用 reload 避免进程匹配问题及性能开销
    uvicorn.run(
        "main:app",
        host="0.0.0.0",
        port=settings.AGENT_BACKEND_PORT,
        reload=False,
        log_level="info",
    )
