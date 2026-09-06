"""Agent 日志配置，只记录推理与工具调用的非正文元信息。"""

import json
import logging
from pathlib import Path
from typing import Any, Dict, List, Union

from langchain_core.callbacks import BaseCallbackHandler
from langchain_core.messages import BaseMessage
from langchain_core.outputs import LLMResult


# 计算 logs 目录：utils/ -> backend/ -> agent/ -> 项目根/
_PROJECT_ROOT = Path(__file__).parent.parent.parent.parent
_LOG_DIR = _PROJECT_ROOT / "logs"
_LOG_FILE = _LOG_DIR / "agent-backend.log"

_LOG_FORMAT = "%(asctime)s [%(levelname)s] %(name)s: %(message)s"
_DATE_FORMAT = "%Y-%m-%d %H:%M:%S"


def setup_logging(level: int = logging.INFO) -> None:
    """
    初始化日志配置。在 main.py 启动时调用一次。
    同时输出到文件（logs/agent-backend.log）和 stdout。
    """
    _LOG_DIR.mkdir(parents=True, exist_ok=True)

    root_logger = logging.getLogger()
    # 避免重复添加 handler
    if root_logger.handlers:
        return

    root_logger.setLevel(level)
    formatter = logging.Formatter(_LOG_FORMAT, datefmt=_DATE_FORMAT)

    # 文件 handler
    file_handler = logging.FileHandler(_LOG_FILE, encoding="utf-8")
    file_handler.setFormatter(formatter)
    root_logger.addHandler(file_handler)

    # 控制台 handler
    console_handler = logging.StreamHandler()
    console_handler.setFormatter(formatter)
    root_logger.addHandler(console_handler)


def _payload_size(value: Any) -> int:
    if isinstance(value, bytes):
        return len(value)
    if isinstance(value, str):
        return len(value.encode("utf-8"))
    try:
        return len(json.dumps(value, ensure_ascii=False, default=lambda _: None).encode("utf-8"))
    except (TypeError, ValueError, RecursionError):
        return 0


def _fmt_messages(messages: List[List[BaseMessage]]) -> str:
    """只汇总消息形态，不把提示词、工具结果或用户正文写入日志。"""
    roles: dict[str, int] = {}
    message_count = 0
    content_bytes = 0
    tool_call_count = 0
    for turn in messages:
        for message in turn:
            message_count += 1
            role = type(message).__name__.replace("Message", "").lower() or "unknown"
            roles[role] = roles.get(role, 0) + 1
            content_bytes += _payload_size(message.content)
            tool_call_count += len(getattr(message, "tool_calls", None) or [])
    role_summary = ",".join(f"{role}:{count}" for role, count in sorted(roles.items()))
    return (
        f"turns={len(messages)} messages={message_count} roles={role_summary or 'none'} "
        f"content_bytes={content_bytes} tool_calls={tool_call_count}"
    )


def _fmt_llm_result(response: LLMResult) -> str:
    """只汇总模型响应形态与 token 用量，不记录响应正文。"""
    generation_count = 0
    content_bytes = 0
    tool_call_count = 0
    for gens in response.generations:
        for gen in gens:
            generation_count += 1
            message = getattr(gen, "message", None)
            content = getattr(message, "content", None)
            if content is None:
                content = getattr(gen, "text", "")
            content_bytes += _payload_size(content)
            tool_call_count += len(getattr(message, "tool_calls", None) or [])

    usage_summary: dict[str, int | float] = {}
    if response.llm_output:
        token_usage = response.llm_output.get("token_usage") or response.llm_output.get("usage")
        if isinstance(token_usage, dict):
            for key, value in token_usage.items():
                if isinstance(value, (int, float)) and not isinstance(value, bool):
                    usage_summary[str(key)] = value
    usage = json.dumps(usage_summary, sort_keys=True, separators=(",", ":"))
    return (
        f"generations={generation_count} content_bytes={content_bytes} "
        f"tool_calls={tool_call_count} token_usage={usage}"
    )


class LLMMetadataLogger(BaseCallbackHandler):
    """LangChain 回调：只记录可观测元信息，不复制任何调用正文。"""

    def __init__(self):
        super().__init__()
        self._logger = logging.getLogger("agent.llm")

    def on_chat_model_start(
        self,
        serialized: Dict[str, Any],
        messages: List[List[BaseMessage]],
        **kwargs: Any,
    ) -> None:
        model = serialized.get("kwargs", {}).get("model_name", "unknown")
        self._logger.info("[LLM-IN] model=%s %s", model, _fmt_messages(messages))

    def on_llm_end(self, response: LLMResult, **kwargs: Any) -> None:
        self._logger.info("[LLM-OUT] %s", _fmt_llm_result(response))

    def on_llm_error(
        self, error: Union[Exception, KeyboardInterrupt], **kwargs: Any
    ) -> None:
        self._logger.error("[LLM-ERROR] error_type=%s", type(error).__name__)

    def on_tool_start(
        self,
        serialized: Dict[str, Any],
        input_str: str,
        **kwargs: Any,
    ) -> None:
        name = serialized.get("name", "unknown")
        self._logger.info("[TOOL-CALL] name=%s input_bytes=%d", name, _payload_size(input_str))

    def on_tool_end(self, output: str, **kwargs: Any) -> None:
        self._logger.info("[TOOL-RESULT] output_bytes=%d", _payload_size(output))

    def on_tool_error(
        self, error: Union[Exception, KeyboardInterrupt], **kwargs: Any
    ) -> None:
        self._logger.error("[TOOL-ERROR] error_type=%s", type(error).__name__)
