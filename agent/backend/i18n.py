"""Agent-owned UI messages; model context is not UI copy."""

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
            "未验证：目标字段类型适配、实际业务值转换及写入可执行性。源结构事实不保证覆盖全部业务值，"
            "配置复核通过不代表任务执行成功。创建仍由 Transfer 校验权限与配置。"
        ),
        "confirm": "确认仅创建任务",
        "cancel": "取消",
    },
    "en": {
        "prompt": (
            "Review the task configuration below. Confirmation only attempts to create a stopped Transfer task; "
            "it does not run the task or write business data.\n\n"
            "Checked: the Tool argument contract and the source schema and target parent facts required by the current "
            "condition adapters. The complete arguments, current platform definition, and schema facts are bound to this review.\n\n"
            "Not verified: target field type compatibility, actual business value conversion, or write executability. "
            "Source schema facts do not guarantee coverage of all business values, and configuration review does not "
            "prove execution success. Transfer still checks permissions and configuration on creation."
        ),
        "confirm": "Confirm task creation only",
        "cancel": "Cancel",
    },
}


def _language(accept_language: str) -> str:
    return "en" if accept_language.lower().startswith("en") else "zh-cn"


def clarification_text_error(reason: str, accept_language: str) -> str:
    return _CLARIFICATION_TEXT_ERRORS[reason][_language(accept_language)]


def transfer_create_review_messages(accept_language: str) -> dict[str, str]:
    return _TRANSFER_CREATE_REVIEW[_language(accept_language)]
