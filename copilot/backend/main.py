"""
ADDP Copilot - FastAPI 应用入口
"""
import asyncio
from contextlib import asynccontextmanager
from addp_common.client import (
    ConfigurationManagementDeclaration,
    ConfigurationManagementEntry,
    ModuleRegistration,
    ModuleRegistryClient,
)
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.openapi.utils import get_openapi

from config import settings

# 应用生命周期管理
@asynccontextmanager
async def lifespan(app: FastAPI):
    """应用启动/关闭时的生命周期管理"""
    from database import init_db
    from services.inference_service import CopilotInferenceService

    print("Copilot Backend 启动中...")
    await init_db()
    CopilotInferenceService.initialize()

    public_base_url = f"http://{settings.service_host}:{settings.port}"
    registry_client = ModuleRegistryClient(settings.get_system_url(), CopilotInferenceService.token_source())
    registration = ModuleRegistration(
        module_name="copilot",
        module_url=public_base_url,
        route_prefix="/copilot",
        health_check_url=f"{public_base_url}/health",
        metadata={"module": "copilot", "language": "python"},
        configuration_management=ConfigurationManagementDeclaration(entries=[
            ConfigurationManagementEntry(
                id="copilot.inference_bindings",
                owner_module="copilot",
                scope_types=["platform_default_with_tenant_override"],
                frontend_route="/configuration/copilot/inference",
                read_permission="copilot.configuration.read",
                update_permission="copilot.configuration.update",
            ),
        ]),
    )
    registry_task = asyncio.create_task(registry_client.run(registration))

    print("Copilot Backend 启动完成")

    yield  # 应用运行中

    # 关闭时：停止心跳
    print("🛑 Copilot Backend 关闭中...")
    registry_task.cancel()
    try:
        await registry_task
    except asyncio.CancelledError:
        pass
    await registry_client.close()
    await CopilotInferenceService.close()
    print("Copilot Backend 已关闭")


# 创建 FastAPI 应用
app = FastAPI(
    title=settings.app_name,
    description="AI 辅助 SQL 和工作流生成 | AI-assisted SQL and workflow generation",
    version="1.0.0",
    lifespan=lifespan
)

_API_PREFIX = "/api/v1/copilot"

# CORS 配置
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],  # 生产环境需限制
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# 注册路由
from api import inference_scenario_binding_router, navigate_router, sql_router, workflow_router  # noqa: E402
from api.kg_extract_api import router as kg_extract_router  # noqa: E402
app.include_router(workflow_router, prefix=_API_PREFIX, tags=["工作流智能体 | Workflow Agent"])
app.include_router(sql_router, prefix=_API_PREFIX, tags=["SQL 智能体 | SQL Agent"])
app.include_router(kg_extract_router, prefix=_API_PREFIX, tags=["图谱构建 | KG Build"])
app.include_router(navigate_router, prefix=_API_PREFIX, tags=["导航引导 | Navigation Guide"])
app.include_router(
    inference_scenario_binding_router,
    prefix=_API_PREFIX,
    tags=["配置管理 | Configuration Management"],
)


def custom_openapi():
    if app.openapi_schema:
        return app.openapi_schema
    schema = get_openapi(
        title=app.title,
        version=app.version,
        description=app.description,
        routes=app.routes,
    )
    # 剥离路径前缀，与 Go 模块 BasePath 风格一致
    new_paths = {}
    for path, item in schema.get("paths", {}).items():
        short = path[len(_API_PREFIX):] if path.startswith(_API_PREFIX) else path
        new_paths[short or "/"] = item
    schema["paths"] = new_paths
    schema["servers"] = [{"url": _API_PREFIX}]
    # 清理 info 字段，只保留 title 和 version
    schema["info"] = {
        "title": schema["info"]["title"],
        "version": schema["info"]["version"]
    }
    app.openapi_schema = schema
    return schema


app.openapi = custom_openapi


@app.get(
    "/health",
    summary="健康检查 | Health Check",
    openapi_extra={"x-addp-auth-mode": "public"},
)
async def health_check():
    """健康检查"""
    return {
        "status": "healthy",
        "service": "copilot",
        "version": "1.0.0"
    }


@app.get(
    "/",
    summary="根路径 | Root",
    openapi_extra={"x-addp-auth-mode": "public"},
)
async def root():
    """根路径"""
    return {
        "message": "ADDP Copilot API",
        "docs": "/docs"
    }


if __name__ == "__main__":
    import uvicorn
    # 修复：直接传递 app 对象而不是字符串，避免重新导入
    # 禁用 reload 避免性能问题
    uvicorn.run(
        app,  # 直接使用 app 对象
        host="0.0.0.0",
        port=settings.port,
        log_level="info"
    )
