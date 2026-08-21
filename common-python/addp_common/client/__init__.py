"""
ADDP 模块客户端

提供统一的 HTTP 客户端,用于调用各个模块的 API
"""

from .base import BaseClient
from .system import SystemClient
from .meta import MetaClient
from .develop import DevelopClient
from .manager import ManagerClient
from .graph import GraphClient
from .copilot import CopilotClient
from .inference import (
    ChatResponse,
    EmbeddingInput,
    EmbeddingResponse,
    InferenceClient,
    InferenceError,
    Message,
    ResponseSchema,
    RerankDocument,
    RerankResponse,
    ResolveProfileResponse,
    ToolCall,
    ToolDefinition,
)
from .service_token import OAuthServiceTokenSource, ServiceTokenError, SyncOAuthServiceTokenSource
from .module_registry import (
    ConfigurationManagementDeclaration,
    ConfigurationManagementEntry,
    ModuleRegistration,
    ModuleRegistryClient,
)
from .runtime_registration import register_runtime_engine, retry_runtime_registration

__all__ = [
    "BaseClient",
    "SystemClient",
    "MetaClient",
    "DevelopClient",
    "ManagerClient",
    "GraphClient",
    "CopilotClient",
    "OAuthServiceTokenSource",
    "SyncOAuthServiceTokenSource",
    "ServiceTokenError",
    "InferenceClient",
    "InferenceError",
    "Message",
    "ToolCall",
    "ToolDefinition",
    "ResponseSchema",
    "ChatResponse",
    "EmbeddingInput",
    "EmbeddingResponse",
    "RerankDocument",
    "RerankResponse",
    "ResolveProfileResponse",
    "ConfigurationManagementDeclaration",
    "ConfigurationManagementEntry",
    "ModuleRegistration",
    "ModuleRegistryClient",
    "register_runtime_engine",
    "retry_runtime_registration",
]
