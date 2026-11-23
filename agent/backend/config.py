from typing import Literal, Optional
from pydantic_settings import BaseSettings, SettingsConfigDict

class Settings(BaseSettings):
    model_config = SettingsConfigDict(case_sensitive=True, extra="ignore")

    # Agent 模块配置
    AGENT_BACKEND_PORT: int = 8190
    AGENT_DB_SCHEMA: Literal["agent"] = "agent"

    AGENT_SERVICE_CLIENT_SECRET: str = ""

    # 服务 host 和端口（从 .env 读取）
    SERVICE_HOST: str = "localhost"
    GATEWAY_PORT: int = 8000
    SYSTEM_BACKEND_PORT: int = 8180

    # 服务 URL（优先使用环境变量，否则自动构建）
    GATEWAY_URL: Optional[str] = None
    SYSTEM_URL: Optional[str] = None

    def get_gateway_url(self) -> str:
        return self.GATEWAY_URL or f"http://{self.SERVICE_HOST}:{self.GATEWAY_PORT}"

    def get_system_url(self) -> str:
        return self.SYSTEM_URL or f"http://{self.SERVICE_HOST}:{self.SYSTEM_BACKEND_PORT}"

    # Redis 配置
    REDIS_HOST: str = "localhost"
    REDIS_PORT: int = 16379
    REDIS_PASSWORD: str = ""
    REDIS_DB: int = 0

settings = Settings()
