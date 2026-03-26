"""
Agent 模块日志配置
- 配置写入 logs/agent-backend.log（项目根目录下）
- 提供 LangChain 回调类，记录 LLM 的完整输入输出
"""
import logging
import os
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


def _fmt_messages(messages: List[List[BaseMessage]]) -> str:
    """格式化 LangChain messages 列表，用于日志输出"""
    lines = []
    for turn in messages:
        for msg in turn:
            role = type(msg).__name__.replace("Message", "").upper()
            content = str(msg.content)
            # 超长内容截断（避免日志过大）
            if len(content) > 3000:
                content = content[:3000] + f"...[截断，共 {len(content)} 字符]"
            lines.append(f"  [{role}]\n{content}")
    return "\n".join(lines)


def _fmt_llm_result(response: LLMResult) -> str:
    """格式化 LLM 输出结果，用于日志"""
    parts = []
    for gens in response.generations:
        for gen in gens:
            text = getattr(gen, "text", None) or str(getattr(gen, "message", gen))
            if len(text) > 2000:
                text = text[:2000] + f"...[截断，共 {len(text)} 字符]"
            parts.append(text)

    # token 用量（如有）
    usage = ""
    if response.llm_output:
        token_usage = response.llm_output.get("token_usage") or response.llm_output.get("usage")
        if token_usage:
            usage = f"\n  [tokens] {token_usage}"

    return "\n".join(parts) + usage


class LLMIOLogger(BaseCallbackHandler):
    """
    LangChain 回调：记录 LLM 的完整输入和输出到日志文件。
    用于排查 agent 的推理过程和工具调用行为。
    """

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
        self._logger.info(
            "[LLM-IN] model=%s\n%s",
            model,
            _fmt_messages(messages),
        )

    def on_llm_end(self, response: LLMResult, **kwargs: Any) -> None:
        self._logger.info("[LLM-OUT]\n%s", _fmt_llm_result(response))

    def on_llm_error(
        self, error: Union[Exception, KeyboardInterrupt], **kwargs: Any
    ) -> None:
        self._logger.error("[LLM-ERROR] %s", error)

    def on_tool_start(
        self,
        serialized: Dict[str, Any],
        input_str: str,
        **kwargs: Any,
    ) -> None:
        name = serialized.get("name", "unknown")
        self._logger.info("[TOOL-CALL] %s | input: %s", name, input_str)

    def on_tool_end(self, output: str, **kwargs: Any) -> None:
        text = str(output)
        if len(text) > 2000:
            text = text[:2000] + f"...[截断，共 {len(text)} 字符]"
        self._logger.info("[TOOL-RESULT] %s", text)

    def on_tool_error(
        self, error: Union[Exception, KeyboardInterrupt], **kwargs: Any
    ) -> None:
        self._logger.error("[TOOL-ERROR] %s", error)
