"""
LLM 实例工厂

从 main_agent.py 抽取，供 agents/ 和 graph/ 共享使用，避免循环依赖。
"""
import logging
from langchain_openai import ChatOpenAI
from utils.logging_setup import LLMIOLogger
from config import settings

logger = logging.getLogger("agent.llm")


def get_llm(streaming: bool = False) -> ChatOpenAI:
    """根据配置获取 LLM 实例，注入日志回调"""
    callbacks = [LLMIOLogger()]
    provider = settings.DEFAULT_LLM_PROVIDER.lower()

    if provider == "dashscope":
        if not settings.DASHSCOPE_API_KEY:
            raise ValueError("DASHSCOPE_API_KEY 未配置，请在 .env 中设置")
        return ChatOpenAI(
            api_key=settings.DASHSCOPE_API_KEY,
            model=settings.DEFAULT_LLM_MODEL or "qwen-max",
            base_url="https://dashscope.aliyuncs.com/compatible-mode/v1",
            temperature=0.7,
            streaming=streaming,
            callbacks=callbacks,
        )
    elif provider == "openai":
        return ChatOpenAI(
            api_key=settings.OPENAI_API_KEY,
            model=settings.DEFAULT_LLM_MODEL or "gpt-4o",
            base_url=settings.OPENAI_BASE_URL or None,
            temperature=0.7,
            streaming=streaming,
            callbacks=callbacks,
        )
    else:
        raise ValueError(f"不支持的 LLM Provider: {provider}")
