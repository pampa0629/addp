"""Agent-owned UI messages; model context is not UI copy."""

import json

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

_TRANSFER_CREATE_REVIEW = {
    "zh-cn": {
        "prompt": (
            "请复核以下任务配置。确认后仅尝试创建未启动的 Transfer 任务，不执行、不写入业务数据。\n\n"
            "已检查：Tool 参数契约及当前条件适配器要求的源结构和目标父节点事实；完整参数、当前平台定义及结构事实已绑定本次复核。\n\n"
            "Transfer 已完成正式只读配置预检，检查范围与剩余要求见下方报告。"
            "预检只使用已登记结构事实，不读取业务行，不保证实时结构、实际值转换、Security 或运行授权成功。"
            "创建前会重新预检；稳定事实或诊断变化后必须重新确认。创建仍由 Transfer 校验权限与配置。"
        ),
        "confirm": "确认仅创建任务",
        "cancel": "取消",
        "unknown": "预检结果为无法确定（unknown），并非通过。请核对未检查项和运行时核验要求；确认表示知情接受这些限制，仅保存未运行任务。",
        "unknown_confirm": "知悉未核验项，仅创建任务",
        "supported": "预检结果为已检查部分支持（supported），不代表执行必然成功。",
        "failed": "只读配置预检失败，未创建任务",
    },
    "en": {
        "prompt": (
            "Review the task configuration below. Confirmation only attempts to create a stopped Transfer task; "
            "it does not run the task or write business data.\n\n"
            "Checked: the Tool argument contract and the source schema and target parent facts required by the current "
            "condition adapters. The complete arguments, current platform definition, and schema facts are bound to this review.\n\n"
            "Transfer performed the formal read-only configuration check; its scope and remaining requirements appear below. "
            "It uses persisted schema facts, not business rows, and does not guarantee live schema, value conversion, "
            "Security or runtime authorization. A fresh check precedes creation; changed stable facts or diagnostics "
            "require a new confirmation. Transfer still checks permissions and configuration on creation."
        ),
        "confirm": "Confirm task creation only",
        "cancel": "Cancel",
        "unknown": "The check is unknown, not passed. Review the unchecked items and required runtime checks; confirmation knowingly accepts these limitations and only saves a stopped task.",
        "unknown_confirm": "Accept unchecked items; create only",
        "supported": "The checked definitions are supported; this does not guarantee execution success.",
        "failed": "Read-only configuration check failed; no task was created",
    },
}


def _language(accept_language: str) -> str:
    return "en" if accept_language.lower().startswith("en") else "zh-cn"


def clarification_text_error(reason: str, accept_language: str) -> str:
    return _CLARIFICATION_TEXT_ERRORS[reason][_language(accept_language)]


def transfer_create_review_messages(accept_language: str, report: dict) -> dict[str, str]:
    messages = dict(_TRANSFER_CREATE_REVIEW[_language(accept_language)])
    status = report["status"]
    if status not in {"supported", "unknown"}:
        raise ValueError("config_check_unsupported")
    details = {key: report[key] for key in ("status", "checked", "not_checked", "runtime_checks_required", "diagnostics")}
    messages["prompt"] += "\n\n" + messages[status] + "\n\n```json\n" + json.dumps(details, ensure_ascii=False, indent=2) + "\n```\n"
    if status == "unknown":
        messages["confirm"] = messages["unknown_confirm"]
    return messages


def transfer_config_check_error(accept_language: str) -> str:
    return _TRANSFER_CREATE_REVIEW[_language(accept_language)]["failed"]
