"""
ADDP Common Python Module

Python 共享模块,提供统一的客户端和工具函数
"""

__version__ = "0.1.14"
from .module_lifecycle import (
    ModuleReadyMiddleware,
    live_response,
    register_after_listener,
    ready_response,
    terminate_process_on_registration_failure,
)

__all__ = [
    "ModuleReadyMiddleware",
    "live_response",
    "register_after_listener",
    "ready_response",
    "terminate_process_on_registration_failure",
]
