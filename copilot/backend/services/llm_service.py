"""
LLM 服务：支持多 LLM 提供商（通义千问优先）
"""
from typing import Optional
from langchain_openai import ChatOpenAI
from langchain_anthropic import ChatAnthropic
from config import settings
import tiktoken


class LLMService:
    """
    LLM 服务管理器

    支持的提供商：
    - Dashscope (通义千问) - 优先
    - OpenAI
    - Claude (Anthropic)
    - Ollama (本地模型)
    """

    def __init__(self):
        self.default_provider = settings.default_llm_provider
        self.default_model = settings.default_llm_model

    def get_llm(self, tenant_id: Optional[int] = None, provider: Optional[str] = None):
        """
        获取 LLM 实例

        Args:
            tenant_id: 租户 ID（用于获取租户级配置，暂未实现）
            provider: 指定 LLM 提供商，不指定则使用默认值

        Returns:
            LangChain ChatModel 实例
        """
        provider = provider or self.default_provider

        if provider == 'dashscope':
            return self.get_dashscope_llm()
        elif provider == 'openai':
            return self.get_openai_llm()
        elif provider == 'claude':
            return self.get_claude_llm()
        elif provider == 'ollama':
            return self.get_ollama_llm()
        else:
            raise ValueError(f"Unsupported LLM provider: {provider}")

    def get_dashscope_llm(self):
        """
        获取通义千问 LLM（优先）

        配置示例：
        - model: qwen-max (推荐)、qwen-turbo、qwen-plus
        - API Key: 从阿里云 Dashscope 控制台获取

        注意：使用 Dashscope 的 OpenAI 兼容接口
        """
        if not settings.dashscope_api_key:
            raise ValueError("Dashscope API Key not configured. Please set DASHSCOPE_API_KEY in .env")

        return ChatOpenAI(
            model=settings.dashscope_model or self.default_model or "qwen-max",
            api_key=settings.dashscope_api_key,
            base_url="https://dashscope.aliyuncs.com/compatible-mode/v1",
            temperature=0.7,
            max_tokens=2000
        )

    def get_openai_llm(self):
        """获取 OpenAI LLM"""
        if not settings.openai_api_key:
            raise ValueError("OpenAI API Key not configured")

        return ChatOpenAI(
            model=self.default_model if self.default_provider == 'openai' else 'gpt-4o-mini',
            api_key=settings.openai_api_key,
            temperature=0.7,
            max_tokens=2000
        )

    def get_claude_llm(self):
        """获取 Claude LLM"""
        if not settings.claude_api_key:
            raise ValueError("Claude API Key not configured")

        return ChatAnthropic(
            model=self.default_model if self.default_provider == 'claude' else 'claude-3-5-sonnet-20241022',
            api_key=settings.claude_api_key,
            temperature=0.7,
            max_tokens=2000
        )

    def get_ollama_llm(self):
        """获取 Ollama 本地模型"""
        from langchain_community.chat_models import ChatOllama

        return ChatOllama(
            model=settings.ollama_model or 'qwen2.5:7b',
            base_url=settings.ollama_base_url or 'http://localhost:11434',
            temperature=0.7
        )

    def count_tokens(self, text: str, model: str = "gpt-4o-mini") -> int:
        """
        计算文本的 Token 数量

        注意：这是基于 OpenAI tokenizer 的估算，不同模型可能有差异
        """
        try:
            encoding = tiktoken.encoding_for_model(model)
            return len(encoding.encode(text))
        except Exception:
            # 简单估算：中文 ~2 字符/token，英文 ~4 字符/token
            chinese_chars = sum(1 for c in text if '\u4e00' <= c <= '\u9fff')
            other_chars = len(text) - chinese_chars
            return int(chinese_chars / 2 + other_chars / 4)

    def check_quota(self, tenant_id: int, tokens: int) -> bool:
        """
        检查租户的 Token 限额

        Args:
            tenant_id: 租户 ID
            tokens: 请求的 Token 数量

        Returns:
            是否在限额内

        TODO: 实现基于数据库的限额检查
        """
        max_tokens_per_day = settings.copilot_max_tokens_per_day
        # 这里应该查询数据库获取今日已使用的 tokens
        # 暂时简单返回 True
        return True


# 全局单例
llm_service = LLMService()
