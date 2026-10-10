"""Agent-owned HTTP validation messages; model context is not UI copy."""

_CLARIFICATION_TEXT_ERRORS = {
    "invalid_shape": {
        "zh-cn": "文字回答只能包含 text 字符串，不能携带候选或批准字段",
        "en": "A text answer must contain only a text string, without candidate or approval fields",
    },
    "invalid_length": {
        "zh-cn": "澄清文字回答不能为空，且不能超过 2000 字符",
        "en": "A clarification text answer must be non-empty and at most 2000 characters",
    },
}


def clarification_text_error(reason: str, accept_language: str) -> str:
    language = "en" if accept_language.lower().startswith("en") else "zh-cn"
    return _CLARIFICATION_TEXT_ERRORS[reason][language]
