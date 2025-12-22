"""
LLM 服务：支持多 LLM 提供商的适配层
"""
from typing import Optional, List
from http import HTTPStatus
from config import settings
import tiktoken


class BaseLLMAdapter:
    """LLM 适配器基类"""

    def invoke(self, messages: List[dict], temperature: float = 0.7, max_tokens: int = 2000) -> str:
        """同步调用 LLM"""
        raise NotImplementedError

    async def ainvoke(self, messages: List[dict], temperature: float = 0.7, max_tokens: int = 2000) -> str:
        """异步调用 LLM"""
        raise NotImplementedError


class DashscopeAdapter(BaseLLMAdapter):
    """通义千问适配器（使用官方 SDK）"""

    def __init__(self, api_key: str, model: str = "qwen-max", disable_ssl_verify: bool = False):
        import dashscope
        dashscope.api_key = api_key
        self.model = model
        self.generation = dashscope.Generation()
        self.disable_ssl_verify = disable_ssl_verify

        # 如果禁用 SSL 验证，设置环境变量
        if disable_ssl_verify:
            import ssl
            import os
            import urllib3

            # 禁用 urllib3 的 SSL 警告
            urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

            # 设置环境变量禁用 SSL 验证
            os.environ['CURL_CA_BUNDLE'] = ''
            os.environ['REQUESTS_CA_BUNDLE'] = ''

            print(f"[Dashscope Adapter] ⚠️ SSL 验证已禁用（仅用于开发环境）")

        print(f"[Dashscope Adapter] 初始化完成 - Model: {model}, API Key: {api_key[:10]}...")

    def _convert_messages(self, messages: List[dict]) -> List[dict]:
        """
        转换消息格式
        LangChain 格式 -> Dashscope 格式
        """
        dashscope_messages = []

        for msg in messages:
            # 处理 LangChain 的消息对象
            if hasattr(msg, '__class__'):
                msg_type = msg.__class__.__name__
                content = msg.content if hasattr(msg, 'content') else str(msg)

                if 'System' in msg_type:
                    role = 'system'
                elif 'Human' in msg_type or 'User' in msg_type:
                    role = 'user'
                elif 'AI' in msg_type or 'Assistant' in msg_type:
                    role = 'assistant'
                else:
                    role = 'user'

                dashscope_messages.append({'role': role, 'content': content})
            # 处理字典格式
            elif isinstance(msg, dict):
                dashscope_messages.append(msg)
            else:
                print(f"[Dashscope Adapter] ⚠️ 未知消息格式: {type(msg)}")

        print(f"[Dashscope Adapter] 转换消息: {len(messages)} -> {len(dashscope_messages)} 条")
        return dashscope_messages

    def invoke(self, messages: List[dict], temperature: float = 0.7, max_tokens: int = 2000) -> str:
        """同步调用"""
        dashscope_messages = self._convert_messages(messages)

        print(f"[Dashscope Adapter] 调用 {self.model}...")
        print(f"[Dashscope Adapter] 参数: temperature={temperature}, max_tokens={max_tokens}")

        try:
            # 如果禁用 SSL，修改 requests 会话
            if self.disable_ssl_verify:
                import requests
                from requests.adapters import HTTPAdapter
                from urllib3.util.retry import Retry

                # 创建一个禁用 SSL 验证的会话
                session = requests.Session()
                session.verify = False

                # 临时替换 dashscope 内部的 session
                import dashscope.api_entities.http_request as http_req
                original_session = getattr(http_req, '_session', None)
                http_req._session = session

            response = self.generation.call(
                model=self.model,
                messages=dashscope_messages,
                temperature=temperature,
                max_tokens=max_tokens,
                result_format='message'
            )

            # 恢复原始 session
            if self.disable_ssl_verify and original_session is not None:
                http_req._session = original_session

            if response.status_code == HTTPStatus.OK:
                content = response['output']['choices'][0]['message']['content']
                print(f"[Dashscope Adapter] ✅ 调用成功，响应长度: {len(content)} 字符")
                return content
            else:
                error_msg = (
                    f"Request id: {response.request_id}, "
                    f"Status code: {response.status_code}, "
                    f"Error code: {response.code}, "
                    f"Error message: {response.message}"
                )
                print(f"[Dashscope Adapter] ❌ 调用失败: {error_msg}")
                raise RuntimeError(f"Dashscope API error: {error_msg}")

        except Exception as e:
            print(f"[Dashscope Adapter] ❌ 异常: {type(e).__name__}: {str(e)}")
            raise

    async def ainvoke(self, messages: List[dict], temperature: float = 0.7, max_tokens: int = 2000) -> str:
        """异步调用（通义千问 SDK 暂时用同步实现）"""
        # Dashscope SDK 目前没有原生异步支持，使用同步调用
        return self.invoke(messages, temperature, max_tokens)


class OpenAIAdapter(BaseLLMAdapter):
    """OpenAI 适配器"""

    def __init__(self, api_key: str, model: str = "gpt-4o-mini", base_url: Optional[str] = None):
        from openai import OpenAI, AsyncOpenAI
        self.client = OpenAI(api_key=api_key, base_url=base_url)
        self.async_client = AsyncOpenAI(api_key=api_key, base_url=base_url)
        self.model = model
        print(f"[OpenAI Adapter] 初始化完成 - Model: {model}")

    def _convert_messages(self, messages: List[dict]) -> List[dict]:
        """转换消息格式"""
        openai_messages = []
        for msg in messages:
            if hasattr(msg, '__class__'):
                msg_type = msg.__class__.__name__
                content = msg.content if hasattr(msg, 'content') else str(msg)

                if 'System' in msg_type:
                    role = 'system'
                elif 'Human' in msg_type or 'User' in msg_type:
                    role = 'user'
                elif 'AI' in msg_type or 'Assistant' in msg_type:
                    role = 'assistant'
                else:
                    role = 'user'

                openai_messages.append({'role': role, 'content': content})
            elif isinstance(msg, dict):
                openai_messages.append(msg)

        return openai_messages

    def invoke(self, messages: List[dict], temperature: float = 0.7, max_tokens: int = 2000) -> str:
        """同步调用"""
        openai_messages = self._convert_messages(messages)

        response = self.client.chat.completions.create(
            model=self.model,
            messages=openai_messages,
            temperature=temperature,
            max_tokens=max_tokens
        )

        return response.choices[0].message.content

    async def ainvoke(self, messages: List[dict], temperature: float = 0.7, max_tokens: int = 2000) -> str:
        """异步调用"""
        openai_messages = self._convert_messages(messages)

        response = await self.async_client.chat.completions.create(
            model=self.model,
            messages=openai_messages,
            temperature=temperature,
            max_tokens=max_tokens
        )

        return response.choices[0].message.content


class ClaudeAdapter(BaseLLMAdapter):
    """Claude 适配器"""

    def __init__(self, api_key: str, model: str = "claude-3-5-sonnet-20241022"):
        from anthropic import Anthropic, AsyncAnthropic
        self.client = Anthropic(api_key=api_key)
        self.async_client = AsyncAnthropic(api_key=api_key)
        self.model = model
        print(f"[Claude Adapter] 初始化完成 - Model: {model}")

    def _convert_messages(self, messages: List[dict]) -> tuple:
        """转换消息格式，分离 system 和其他消息"""
        system_message = ""
        claude_messages = []

        for msg in messages:
            if hasattr(msg, '__class__'):
                msg_type = msg.__class__.__name__
                content = msg.content if hasattr(msg, 'content') else str(msg)

                if 'System' in msg_type:
                    system_message = content
                elif 'Human' in msg_type or 'User' in msg_type:
                    claude_messages.append({'role': 'user', 'content': content})
                elif 'AI' in msg_type or 'Assistant' in msg_type:
                    claude_messages.append({'role': 'assistant', 'content': content})
            elif isinstance(msg, dict):
                if msg.get('role') == 'system':
                    system_message = msg['content']
                else:
                    claude_messages.append(msg)

        return system_message, claude_messages

    def invoke(self, messages: List[dict], temperature: float = 0.7, max_tokens: int = 2000) -> str:
        """同步调用"""
        system_message, claude_messages = self._convert_messages(messages)

        response = self.client.messages.create(
            model=self.model,
            system=system_message,
            messages=claude_messages,
            temperature=temperature,
            max_tokens=max_tokens
        )

        return response.content[0].text

    async def ainvoke(self, messages: List[dict], temperature: float = 0.7, max_tokens: int = 2000) -> str:
        """异步调用"""
        system_message, claude_messages = self._convert_messages(messages)

        response = await self.async_client.messages.create(
            model=self.model,
            system=system_message,
            messages=claude_messages,
            temperature=temperature,
            max_tokens=max_tokens
        )

        return response.content[0].text


class OllamaAdapter(BaseLLMAdapter):
    """Ollama 本地模型适配器"""

    def __init__(self, model: str = "qwen2.5:7b", base_url: str = "http://localhost:11434"):
        import httpx
        self.model = model
        self.base_url = base_url
        self.client = httpx.Client(base_url=base_url, timeout=60.0)
        self.async_client = httpx.AsyncClient(base_url=base_url, timeout=60.0)
        print(f"[Ollama Adapter] 初始化完成 - Model: {model}, Base URL: {base_url}")

    def _convert_messages(self, messages: List[dict]) -> List[dict]:
        """转换消息格式"""
        ollama_messages = []
        for msg in messages:
            if hasattr(msg, '__class__'):
                msg_type = msg.__class__.__name__
                content = msg.content if hasattr(msg, 'content') else str(msg)

                if 'System' in msg_type:
                    role = 'system'
                elif 'Human' in msg_type or 'User' in msg_type:
                    role = 'user'
                elif 'AI' in msg_type or 'Assistant' in msg_type:
                    role = 'assistant'
                else:
                    role = 'user'

                ollama_messages.append({'role': role, 'content': content})
            elif isinstance(msg, dict):
                ollama_messages.append(msg)

        return ollama_messages

    def invoke(self, messages: List[dict], temperature: float = 0.7, max_tokens: int = 2000) -> str:
        """同步调用"""
        ollama_messages = self._convert_messages(messages)

        response = self.client.post(
            "/api/chat",
            json={
                "model": self.model,
                "messages": ollama_messages,
                "stream": False,
                "options": {
                    "temperature": temperature,
                    "num_predict": max_tokens
                }
            }
        )

        response.raise_for_status()
        return response.json()["message"]["content"]

    async def ainvoke(self, messages: List[dict], temperature: float = 0.7, max_tokens: int = 2000) -> str:
        """异步调用"""
        ollama_messages = self._convert_messages(messages)

        response = await self.async_client.post(
            "/api/chat",
            json={
                "model": self.model,
                "messages": ollama_messages,
                "stream": False,
                "options": {
                    "temperature": temperature,
                    "num_predict": max_tokens
                }
            }
        )

        response.raise_for_status()
        return response.json()["message"]["content"]


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

    def get_llm(self, tenant_id: Optional[int] = None, provider: Optional[str] = None) -> BaseLLMAdapter:
        """
        获取 LLM 适配器实例

        Args:
            tenant_id: 租户 ID（用于获取租户级配置，暂未实现）
            provider: 指定 LLM 提供商，不指定则使用默认值

        Returns:
            LLM 适配器实例
        """
        provider = provider or self.default_provider
        print(f"[LLM Service] 获取 LLM 实例 - Provider: {provider}, Tenant: {tenant_id}")

        if provider == 'dashscope':
            return self.get_dashscope_adapter()
        elif provider == 'openai':
            return self.get_openai_adapter()
        elif provider == 'claude':
            return self.get_claude_adapter()
        elif provider == 'ollama':
            return self.get_ollama_adapter()
        else:
            raise ValueError(f"Unsupported LLM provider: {provider}")

    def get_dashscope_adapter(self) -> DashscopeAdapter:
        """获取通义千问适配器"""
        if not settings.dashscope_api_key:
            print("[LLM Service] ❌ Dashscope API Key 未配置")
            raise ValueError("Dashscope API Key not configured. Please set DASHSCOPE_API_KEY in .env")

        model = settings.dashscope_model or self.default_model or "qwen-max"
        print(f"[LLM Service] 创建 Dashscope Adapter - Model: {model}")

        if settings.disable_ssl_verify:
            print(f"[LLM Service] ⚠️ SSL 验证已禁用（开发环境配置）")

        return DashscopeAdapter(
            api_key=settings.dashscope_api_key,
            model=model,
            disable_ssl_verify=settings.disable_ssl_verify
        )

    def get_openai_adapter(self) -> OpenAIAdapter:
        """获取 OpenAI 适配器"""
        if not settings.openai_api_key:
            raise ValueError("OpenAI API Key not configured")

        model = self.default_model if self.default_provider == 'openai' else 'gpt-4o-mini'
        return OpenAIAdapter(api_key=settings.openai_api_key, model=model)

    def get_claude_adapter(self) -> ClaudeAdapter:
        """获取 Claude 适配器"""
        if not settings.claude_api_key:
            raise ValueError("Claude API Key not configured")

        model = self.default_model if self.default_provider == 'claude' else 'claude-3-5-sonnet-20241022'
        return ClaudeAdapter(api_key=settings.claude_api_key, model=model)

    def get_ollama_adapter(self) -> OllamaAdapter:
        """获取 Ollama 适配器"""
        return OllamaAdapter(
            model=settings.ollama_model or 'qwen2.5:7b',
            base_url=settings.ollama_base_url or 'http://localhost:11434'
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
