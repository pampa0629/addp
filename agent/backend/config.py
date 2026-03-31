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

    # 服务 host 和端口（从 .env 读取）
    SERVICE_HOST: str = "localhost"
    GATEWAY_PORT: int = 8000
    SYSTEM_BACKEND_PORT: int = 8180
    MANAGER_BACKEND_PORT: int = 8081
    META_BACKEND_PORT: int = 8082
    DEVELOP_BACKEND_PORT: int = 8185
    COPILOT_BACKEND_PORT: int = 8087

    # 服务 URL（优先使用环境变量，否则自动构建）
    GATEWAY_URL: Optional[str] = None
    SYSTEM_SERVICE_URL: Optional[str] = None
    MANAGER_SERVICE_URL: Optional[str] = None
    META_SERVICE_URL: Optional[str] = None
    DEVELOP_SERVICE_URL: Optional[str] = None
    COPILOT_SERVICE_URL: Optional[str] = None

    def get_gateway_url(self) -> str:
        return self.GATEWAY_URL or f"http://{self.SERVICE_HOST}:{self.GATEWAY_PORT}"

    def get_system_url(self) -> str:
        return self.SYSTEM_SERVICE_URL or f"http://{self.SERVICE_HOST}:{self.SYSTEM_BACKEND_PORT}"

    def get_manager_url(self) -> str:
        return self.MANAGER_SERVICE_URL or f"http://{self.SERVICE_HOST}:{self.MANAGER_BACKEND_PORT}"

    def get_meta_url(self) -> str:
        return self.META_SERVICE_URL or f"http://{self.SERVICE_HOST}:{self.META_BACKEND_PORT}"

    def get_develop_url(self) -> str:
        return self.DEVELOP_SERVICE_URL or f"http://{self.SERVICE_HOST}:{self.DEVELOP_BACKEND_PORT}"

    def get_copilot_url(self) -> str:
        return self.COPILOT_SERVICE_URL or f"http://{self.SERVICE_HOST}:{self.COPILOT_BACKEND_PORT}"

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
