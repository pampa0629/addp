from typing import Any


HISTORY_MESSAGE_LIMIT = 20
MESSAGE_CHAR_LIMIT = 6000
HISTORY_CHAR_LIMIT = 24000
SUMMARY_CHAR_LIMIT = 2000


def _truncate(value: str, limit: int) -> tuple[str, bool]:
    if len(value) <= limit:
        return value, False
    marker = "\n...[上下文已截断]"
    if limit <= len(marker):
        return marker[:limit], True
    return value[: limit - len(marker)] + marker, True


def build_context_window(
    messages: list[dict[str, Any]],
    session_summary: str | None,
    *,
    input_message_count: int | None = None,
) -> tuple[list[dict[str, str]], str | None, dict[str, int]]:
    candidates = messages[-HISTORY_MESSAGE_LIMIT:]
    selected_reversed: list[dict[str, str]] = []
    remaining = HISTORY_CHAR_LIMIT
    truncated_messages = 0

    for message in reversed(candidates):
        if remaining <= 0:
            break
        content = str(message.get("content") or "")
        content, truncated = _truncate(content, min(MESSAGE_CHAR_LIMIT, remaining))
        if truncated:
            truncated_messages += 1
        selected_reversed.append({"role": str(message.get("role") or ""), "content": content})
        remaining -= len(content)

    selected = list(reversed(selected_reversed))
    summary = None
    if session_summary:
        summary, _ = _truncate(session_summary, SUMMARY_CHAR_LIMIT)
    total_messages = max(len(messages), int(input_message_count or 0))
    metrics = {
        "input_message_count": total_messages,
        "selected_message_count": len(selected),
        "omitted_message_count": total_messages - len(selected),
        "truncated_message_count": truncated_messages,
        "message_char_count": sum(len(message["content"]) for message in selected),
        "summary_char_count": len(summary or ""),
        "message_limit": HISTORY_MESSAGE_LIMIT,
        "message_char_limit": MESSAGE_CHAR_LIMIT,
        "history_char_limit": HISTORY_CHAR_LIMIT,
        "summary_char_limit": SUMMARY_CHAR_LIMIT,
    }
    return selected, summary, metrics
