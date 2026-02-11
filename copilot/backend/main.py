"""
ADDP Copilot - FastAPI 应用入口
"""
from contextlib import asynccontextmanager
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from config import settings

# 在模块级别初始化数据库（同步执行，避免异步延迟）
# TODO: Copilot 暂时不需要数据库，注释掉以加快启动速度（避免 60 秒延迟）
# from database import Base, engine
# from models import conversation, message, llm_config  # noqa: F401
# Base.metadata.create_all(bind=engine)


# 应用生命周期管理
@asynccontextmanager
async def lifespan(app: FastAPI):
    """应用启动/关闭时的生命周期管理"""
    # 启动时：注册模块到 System
    print("🚀 Copilot Backend 启动中...")

    from services.module_registry import register_module_on_startup

    # 注册模块（非阻塞，失败不影响启动）
    registry = await register_module_on_startup(
        module_name="copilot",
        module_url=f"http://localhost:{settings.port}",
        route_prefix="/api/copilot",
        health_check_url=f"http://localhost:{settings.port}/health"
    )

    print("✅ Copilot Backend 启动完成")

    yield  # 应用运行中

    # 关闭时：停止心跳
    print("🛑 Copilot Backend 关闭中...")
    if registry:
        registry.stop_heartbeat()
    print("✅ Copilot Backend 已关闭")


# 创建 FastAPI 应用
app = FastAPI(
    title=settings.app_name,
    description="AI 辅助 SQL 和工作流生成",
    version="1.0.0",
    lifespan=lifespan
)

# CORS 配置
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],  # 生产环境需限制
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# 注册路由
from api import workflow_router, sql_router  # noqa: E402
app.include_router(workflow_router, prefix="/api/copilot", tags=["Workflow Agent"])
app.include_router(sql_router, prefix="/api/copilot", tags=["SQL Agent"])


@app.get("/health")
async def health_check():
    """健康检查"""
    return {
        "status": "healthy",
        "service": "copilot",
        "version": "1.0.0"
    }


@app.get("/")
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
