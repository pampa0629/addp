class WorkflowValidationError(ValueError):
    """工作流定义不符合 addp.workflow/v1。"""


class WorkflowExecutionError(RuntimeError):
    """工作流执行阶段失败。"""

    def __init__(self, message, *, error_code="EXECUTION_FAILED", partial_result=None, all_results=None, task_order=None):
        super().__init__(message)
        if error_code not in {"EXECUTION_FAILED", "TARGET_ALREADY_EXISTS"}:
            raise ValueError("unsupported workflow execution error code")
        self.error_code = error_code
        self.partial_result = partial_result
        self.all_results = all_results
        self.task_order = task_order or []
