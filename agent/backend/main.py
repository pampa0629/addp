import asyncio
import logging
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
from api.inference_scenario_bindings import router as inference_scenario_bindings_router
from utils.llm import AgentInferenceService
from addp_common.client import (
    ConfigurationManagementDeclaration,
    ConfigurationManagementEntry,
    ModuleRegistration,
    ModuleRegistryClient,
)

# 最先初始化日志（在其他模块 import 之前）
setup_logging()
logger = logging.getLogger(__name__)

_API_PREFIX = "/api/v1/agent"
_bearer_auth = HTTPBearer(
    auto_error=False,
    scheme_name="BearerAuth",
    description="ADDP 用户访问令牌：Authorization: Bearer <token> | ADDP User Access Token",
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
app.include_router(
    inference_scenario_bindings_router,
    prefix=_API_PREFIX,
    dependencies=[Depends(_bearer_auth)],
)


@app.get(
    "/health",
    summary="健康检查 | Health Check",
    openapi_extra={"x-addp-auth-mode": "public"},
)
async def health():
    return {"status": "ok", "module": "agent"}


_registry_client: ModuleRegistryClient | None = None
_registry_task: asyncio.Task | None = None


def _module_registration() -> ModuleRegistration:
    service_url = f"http://{settings.SERVICE_HOST}:{settings.AGENT_BACKEND_PORT}"
    return ModuleRegistration(
        module_name="agent",
        module_url=service_url,
        route_prefix="/agent",
        health_check_url=f"{service_url}/health",
        metadata={"module": "agent", "language": "python"},
        configuration_management=ConfigurationManagementDeclaration(entries=[
            ConfigurationManagementEntry(
                id="agent.configuration",
                owner_module="agent",
                scope_types=["platform_default_with_tenant_override"],
                frontend_route="/configuration/agent",
                read_permission="agent.configuration.read",
                update_permission="agent.configuration.update",
            ),
        ]),
    )


@app.on_event("startup")
async def startup():
    global _registry_client, _registry_task
    await init_db()
    AgentInferenceService.initialize()
    _registry_client = ModuleRegistryClient(settings.get_system_url(), AgentInferenceService.token_source())
    _registry_task = asyncio.create_task(_registry_client.run(_module_registration()))


@app.on_event("shutdown")
async def shutdown():
    global _registry_client, _registry_task
    if _registry_task is not None:
        _registry_task.cancel()
        try:
            await _registry_task
        except asyncio.CancelledError:
            pass
    if _registry_client is not None:
        await _registry_client.close()
    _registry_task = None
    _registry_client = None
    await AgentInferenceService.close()


if __name__ == "__main__":
    # 禁用 reload 避免进程匹配问题及性能开销
    uvicorn.run(
        "main:app",
        host="0.0.0.0",
        port=settings.AGENT_BACKEND_PORT,
        reload=False,
        log_level="info",
    )
