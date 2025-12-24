"""
ADDP Copilot - FastAPI 应用入口
"""
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from contextlib import asynccontextmanager

from config import settings
from database import init_db


@asynccontextmanager
async def lifespan(app: FastAPI):
    """应用生命周期管理"""
    # 启动时初始化数据库
    await init_db()
    print(f"✅ {settings.app_name} started on port {settings.port}")
    yield
    # 关闭时清理资源
    print(f"👋 {settings.app_name} shutting down")


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
from api import workflow_router, sql_router
app.include_router(workflow_router, prefix="/copilot", tags=["Workflow Agent"])
app.include_router(sql_router, prefix="/copilot", tags=["SQL Agent"])


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
    uvicorn.run(
        "main:app",
        host="0.0.0.0",
        port=settings.port,
        reload=settings.debug
    )
