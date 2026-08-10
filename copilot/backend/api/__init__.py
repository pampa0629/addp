"""
Copilot API 路由模块
"""
from .workflow_agent_api import router as workflow_router
from .query_agent_api import router as query_router
from .notebook_agent_api import router as notebook_router
from .transfer_agent_api import router as transfer_router
from .navigate_api import router as navigate_router
from .inference_scenario_binding_api import router as inference_scenario_binding_router

__all__ = ['workflow_router', 'query_router', 'notebook_router', 'transfer_router', 'navigate_router', 'inference_scenario_binding_router']
