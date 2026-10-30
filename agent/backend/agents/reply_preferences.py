"""Expression preferences never change Tool, routing or approval contracts."""
REPLY_DETAILS = frozenset({"standard", "concise", "detailed"})


def reply_detail_prompt(value: str) -> str:
    if not isinstance(value, str) or value not in REPLY_DETAILS:
        raise ValueError("invalid_reply_detail")
    if value == "standard":
        return ""
    preference = "简洁，先给结论，省略非必要展开" if value == "concise" else "详细，给出必要的解释、依据和步骤"
    return ("\n\n用户表达偏好：" + preference + "。仅调整回复表达；本轮用户明确要求优先。"
            "不改变任务目标、路由、工具、参数、权限、确认或正式报告，不删减必要限制和风险说明。")
