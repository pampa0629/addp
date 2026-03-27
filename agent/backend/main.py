import asyncio
import logging
import httpx
import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from starlette.middleware.base import BaseHTTPMiddleware
from utils.logging_setup import setup_logging
from config import settings
from database import init_db
from middleware.auth import auth_middleware
from api.sessions import router as sessions_router
from api.chat import router as chat_router

# 最先初始化日志（在其他模块 import 之前）
setup_logging()
logger = logging.getLogger(__name__)

app = FastAPI(
    title="ADDP Agent Service",
    description="ADDP 平台智能体服务",
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
app.include_router(sessions_router)
app.include_router(chat_router)


@app.get("/health")
async def health():
    return {"status": "ok", "module": "agent"}


async def _register_module():
    """向 System 模块注册，并保持心跳"""
    if not settings.INTERNAL_API_KEY:
        logger.warning("INTERNAL_API_KEY 未配置，跳过模块注册")
        return

    headers = {
        "X-Internal-API-Key": settings.INTERNAL_API_KEY,
        "Content-Type": "application/json",
    }
    service_url = f"http://localhost:{settings.AGENT_BACKEND_PORT}"
    register_url = f"{settings.SYSTEM_URL}/api/v1/internal/modules/register"
    heartbeat_url = f"{settings.SYSTEM_URL}/api/v1/internal/modules/heartbeat"

    async with httpx.AsyncClient(timeout=5.0) as client:
        # 注册，最多重试 3 次
        for attempt in range(1, 4):
            try:
                resp = await client.post(register_url, headers=headers, json={
                    "module_name": "agent",
                    "module_url": service_url,
                    "route_prefix": "/agent",
                    "health_check_url": f"{service_url}/health",
                    "metadata": {"module": "agent", "language": "python"},
                })
                if resp.status_code < 300:
                    logger.info("✅ Agent 模块注册成功: %s", service_url)
                    break
                logger.warning("⚠️  Agent 模块注册失败 (尝试 %d/3): %s", attempt, resp.text)
            except Exception as e:
                logger.warning("⚠️  Agent 模块注册失败 (尝试 %d/3): %s", attempt, e)
            await asyncio.sleep(attempt * 5)

    # 心跳循环
    while True:
        await asyncio.sleep(10)
        try:
            async with httpx.AsyncClient(timeout=5.0) as client:
                await client.post(heartbeat_url, headers=headers, json={"module_name": "agent"})
        except Exception as e:
            logger.debug("Agent 心跳失败: %s", e)


@app.on_event("startup")
async def startup():
    await init_db()
    asyncio.create_task(_register_module())


if __name__ == "__main__":
    uvicorn.run(
        "main:app",
        host="0.0.0.0",
        port=settings.AGENT_BACKEND_PORT,
        reload=True,
        log_level="info",
    )
