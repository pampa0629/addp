import os
from typing import Optional
from pydantic_settings import BaseSettings

class Settings(BaseSettings):
    # Agent 模块配置
    AGENT_BACKEND_PORT: int = 8190
    AGENT_DB_SCHEMA: str = "agent"

    # LLM 配置（复用 .env 中的全局配置）
    DEFAULT_LLM_PROVIDER: str = "dashscope"
    DEFAULT_LLM_MODEL: str = "qwen-max"
    DASHSCOPE_API_KEY: Optional[str] = None

    # 可选的其他 LLM
    OPENAI_API_KEY: Optional[str] = None
    OPENAI_BASE_URL: str = "https://api.openai.com/v1"

    # ADDP 服务配置
    GATEWAY_URL: str = "http://localhost:8000"
    SYSTEM_URL: str = "http://localhost:8180"
    MANAGER_URL: str = "http://localhost:8181"
    META_URL: str = "http://localhost:8182"
    DEVELOP_URL: str = "http://localhost:8184"
    COPILOT_URL: str = "http://localhost:8189"
    INTERNAL_API_KEY: str = ""
    ENABLE_SERVICE_INTEGRATION: bool = True

    # JWT 配置
    JWT_SECRET: str = ""

    # PostgreSQL 配置（与 .env 的 POSTGRES_* 变量对应）
    POSTGRES_HOST: str = "localhost"
    POSTGRES_PORT: int = 15432
    POSTGRES_USER: str = "addp"
    POSTGRES_PASSWORD: str = ""
    POSTGRES_DB: str = "addp"

    # Redis 配置
    REDIS_HOST: str = "localhost"
    REDIS_PORT: int = 16379
    REDIS_PASSWORD: str = ""
    REDIS_DB: int = 0

    class Config:
        env_file = "../../.env"
        case_sensitive = True
        extra = "ignore"

settings = Settings()
