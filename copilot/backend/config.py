"""
Copilot 模块配置管理
"""
from pydantic_settings import BaseSettings
from typing import Optional


class Settings(BaseSettings):
    """应用配置"""

    # 服务配置
    app_name: str = "ADDP Copilot"
    port: int = 8087
    debug: bool = False

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

    # 外部服务 URL
    system_service_url: str = "http://localhost:8080"
    meta_service_url: str = "http://localhost:8082"
    develop_service_url: str = "http://localhost:8084"

    # LLM 默认配置
    default_llm_provider: str = "dashscope"
    default_llm_model: str = "qwen-max"
    dashscope_model: str = "qwen-max"
    dashscope_api_key: Optional[str] = None
    openai_api_key: Optional[str] = None
    claude_api_key: Optional[str] = None
    ollama_base_url: str = "http://localhost:11434"
    ollama_model: str = "qwen2.5:7b"

    # 开发环境选项
    disable_ssl_verify: bool = False  # 禁用 SSL 验证（仅用于开发环境）

    # 加密密钥
    encryption_key: str = "default-encryption-key-change-in-production"

    # Copilot 功能配置
    copilot_enable_streaming: bool = True
    copilot_max_tokens_per_day: int = 100000
    copilot_rate_limit: int = 10
    copilot_score_threshold: float = 0.15
    copilot_max_candidates: int = 10

    # 内部 API Key (用于服务间调用)
    internal_api_key: Optional[str] = None

    class Config:
        env_file = "../../.env"  # 从项目根目录读取 .env
        case_sensitive = False
        extra = "ignore"  # 忽略额外的环境变量


# 全局配置实例
settings = Settings()
