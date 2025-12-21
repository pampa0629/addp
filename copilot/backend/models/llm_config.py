"""
LLM 配置模型
"""
from sqlalchemy import Column, Integer, String, Text, Boolean, DateTime
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.sql import func
from database import Base


class LLMConfig(Base):
    """LLM 配置表（支持租户级配置）"""
    __tablename__ = 'llm_configs'
    __table_args__ = {'schema': 'copilot'}

    id = Column(Integer, primary_key=True, autoincrement=True)
    tenant_id = Column(Integer, index=True)  # NULL 表示全局配置
    provider = Column(String(50), nullable=False)  # 'openai', 'claude', 'ollama', 'dashscope'
    model = Column(String(100), nullable=False)
    api_key = Column(Text)  # AES 加密存储
    base_url = Column(String(512))
    config = Column(JSONB)  # 额外配置（temperature, max_tokens 等）
    is_default = Column(Boolean, default=False)
    is_active = Column(Boolean, default=True)
    created_at = Column(DateTime(timezone=True), server_default=func.now())

    def __repr__(self):
        return f"<LLMConfig(id={self.id}, provider={self.provider}, model={self.model})>"
