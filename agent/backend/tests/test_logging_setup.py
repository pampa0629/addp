import logging

from langchain_core.messages import HumanMessage, ToolMessage
from langchain_core.outputs import ChatGeneration, LLMResult

from utils.logging_setup import LLMMetadataLogger, _fmt_llm_result, _fmt_messages


SECRET = "13661384499-secret-payload"


def test_message_summary_never_contains_message_content():
    summary = _fmt_messages(
        [[HumanMessage(content=SECRET), ToolMessage(content=SECRET, tool_call_id="call-1")]]
    )

    assert SECRET not in summary
    assert "messages=2" in summary
    assert f"content_bytes={len(SECRET.encode('utf-8')) * 2}" in summary


def test_result_summary_never_contains_model_content():
    result = LLMResult(
        generations=[[ChatGeneration(message=HumanMessage(content=SECRET))]],
        llm_output={"token_usage": {"prompt_tokens": 12, "detail": SECRET}},
    )

    summary = _fmt_llm_result(result)

    assert SECRET not in summary
    assert "generations=1" in summary
    assert '"prompt_tokens":12' in summary
    assert "detail" not in summary


def test_callbacks_log_only_metadata(caplog):
    logger = LLMMetadataLogger()
    with caplog.at_level(logging.INFO, logger="agent.llm"):
        logger.on_chat_model_start({}, [[HumanMessage(content=SECRET)]])
        logger.on_tool_start({"name": "data.preview"}, SECRET)
        logger.on_tool_end(SECRET)
        logger.on_llm_error(ValueError(SECRET))
        logger.on_tool_error(RuntimeError(SECRET))

    output = caplog.text
    assert SECRET not in output
    assert "content_bytes=" in output
    assert "input_bytes=" in output
    assert "output_bytes=" in output
    assert "error_type=ValueError" in output
    assert "error_type=RuntimeError" in output
