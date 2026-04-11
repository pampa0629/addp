"""
Copilot API 路由模块
"""
from .workflow_agent_api import router as workflow_router
from .sql_agent_api import router as sql_router
from .navigate_api import router as navigate_router

__all__ = ['workflow_router', 'sql_router', 'navigate_router']
