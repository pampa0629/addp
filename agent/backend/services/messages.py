import copy
import json
from typing import Any


MESSAGE_PARTS_MAX_BYTES = 2 * 1024 * 1024


def append_text_delta(parts: list[dict[str, Any]], delta: str) -> None:
    """Merge only adjacent text; never move text across a presentation."""
    if not delta:
        return
    if parts and parts[-1].get("type") == "text":
        parts[-1]["text"] += delta
    else:
        parts.append({"type": "text", "text": delta})


def bounded_message_parts(parts: Any) -> list[dict[str, Any]]:
    if not isinstance(parts, list) or any(not isinstance(part, dict) for part in parts):
        raise ValueError("message parts must be an array of objects")
    encoded = json.dumps(parts, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    if len(encoded) > MESSAGE_PARTS_MAX_BYTES:
        raise ValueError(f"message parts exceed {MESSAGE_PARTS_MAX_BYTES} bytes")
    return copy.deepcopy(parts)
