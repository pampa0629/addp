class WorkflowValidationError(ValueError):
    """工作流定义不符合 addp.workflow/v1。"""


class WorkflowExecutionError(RuntimeError):
    """工作流执行阶段失败。"""
