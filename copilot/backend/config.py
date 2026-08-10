"""
Copilot 模块配置管理
"""
from pydantic import Field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict
from typing import Optional


class Settings(BaseSettings):
    """应用配置"""
    model_config = SettingsConfigDict(
        case_sensitive=False,
        extra="ignore",
        populate_by_name=True,
    )

    # 服务配置
    app_name: str = "ADDP Copilot"
    port: int = 8087
    debug: bool = Field(default=False, alias="copilot_debug")

    @field_validator("debug", mode="before")
    @classmethod
    def parse_debug(cls, value):
        if isinstance(value, str) and value.lower() in {"release", "prod", "production"}:
            return False
        return value

    # 数据库配置（支持环境变量）
    postgres_host: str = "localhost"
    postgres_port: int = 5432
    postgres_user: str = "addp"
    postgres_password: str = "addp_password"
    postgres_db: str = "addp"
    db_schema: str = "copilot"

    @property
    def database_url(self) -> str:
        """动态构建数据库连接字符串"""
        return f"postgresql://{self.postgres_user}:{self.postgres_password}@{self.postgres_host}:{self.postgres_port}/{self.postgres_db}"

    # 服务 host 和端口（从 .env 读取）
    service_host: str = "localhost"
    gateway_port: int = 8000
    system_backend_port: int = 8180
    manager_backend_port: int = 8081
    meta_backend_port: int = 8082
    develop_backend_port: int = 8185

    # 外部服务 URL（优先使用环境变量，否则自动构建）
    gateway_url: Optional[str] = None
    system_url: Optional[str] = None
    manager_url: Optional[str] = None
    meta_url: Optional[str] = None
    develop_url: Optional[str] = None

    def get_gateway_url(self) -> str:
        return self.gateway_url or f"http://{self.service_host}:{self.gateway_port}"

    def get_system_url(self) -> str:
        return self.system_url or f"http://{self.service_host}:{self.system_backend_port}"

    def get_manager_url(self) -> str:
        return self.manager_url or f"http://{self.service_host}:{self.manager_backend_port}"

    def get_meta_url(self) -> str:
        return self.meta_url or f"http://{self.service_host}:{self.meta_backend_port}"

    def get_develop_url(self) -> str:
        return self.develop_url or f"http://{self.service_host}:{self.develop_backend_port}"

    copilot_service_client_secret: str = ""

    # Copilot 功能配置
    copilot_enable_streaming: bool = True
    copilot_max_tokens_per_day: int = 100000
    copilot_rate_limit: int = 10

# 全局配置实例
settings = Settings()
