import pytest
from langchain_core.messages import AIMessage, HumanMessage, SystemMessage, ToolMessage

from addp_common.inference_langchain import _to_inference_message


@pytest.mark.parametrize("message", [
    SystemMessage(content=[{"type": "text", "text": "平台"}, "语义"]),
    HumanMessage(content=["平台", {"type": "text", "text": "语义"}]),
    AIMessage(content=[{"type": "text", "text": "平台语义"}]),
    ToolMessage(content=[{"type": "text", "text": "平台语义"}], tool_call_id="tool-1"),
])
def test_text_blocks_normalize_without_repr(message):
    result = _to_inference_message(message)
    assert result.content == "平台语义"
    if isinstance(message, ToolMessage):
        assert result.tool_call_id == "tool-1"


@pytest.mark.parametrize("block", [
    {"type": "image_url", "image_url": {"url": "https://example.invalid/image"}},
    {"type": "reasoning", "text": "不可当正文"},
    {"text": "没有类型"}, {"type": "text", "text": 42},
])
def test_non_text_blocks_fail_closed(block):
    with pytest.raises(ValueError, match="only supports text"):
        _to_inference_message(HumanMessage(content=[block]))
