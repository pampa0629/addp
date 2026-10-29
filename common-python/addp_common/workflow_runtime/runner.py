from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Callable

from ..workflow_access import WorkflowTargetExistsError
from .errors import OperatorInputError, WorkflowExecutionError
from .graph import topological_order
from .references import resolve_references
from .validation import validate_workflow_def


OperatorExecutor = Callable[[str, dict[str, Any]], Any]
ProgressCallback = Callable[[str, int, str], None]


@dataclass(frozen=True)
class WorkflowRunResult:
    final_result: Any
    all_results: dict[str, Any]
    task_order: list[str]


class WorkflowRunner:
    def __init__(self, operator_ids: set[str], executor: OperatorExecutor, *, result_projector=None) -> None:
        self._operator_ids = set(operator_ids)
        self._executor = executor
        self._result_projector = result_projector

    def _project_result(self, result):
        return self._result_projector(result) if self._result_projector is not None else result

    def execute(
        self,
        workflow_def: dict[str, Any],
        input_data: dict[str, Any] | None = None,
        progress: ProgressCallback | None = None,
    ) -> WorkflowRunResult:
        tasks = validate_workflow_def(workflow_def, operator_ids=self._operator_ids)
        by_id = {task["id"]: task for task in tasks}
        order = topological_order(tasks)
        port_results: dict[str, dict[str, Any]] = {}
        public_results: dict[str, Any] = {}
        total = len(order)
        for index, task_id in enumerate(order):
            task = by_id[task_id]
            if progress:
                progress("running", int(index * 100 / total), task_id)
            try:
                params = resolve_references(task["params"], port_results)
                params = _resolve_input_data(params, input_data or {})
                raw_result = self._executor(task["operator"], params)
                ports = _normalize_ports(raw_result)
                public_results[task_id] = self._project_result(_public_result(ports))
                port_results[task_id] = ports
            except Exception as exc:
                diagnostic = (exc.diagnostic(task["operator"], tasks.index(task) + 1)
                              if isinstance(exc, OperatorInputError) else None)
                code = ("INVALID_PARAMS" if diagnostic is not None else exc.error_code if isinstance(exc, WorkflowExecutionError) else
                        "TARGET_ALREADY_EXISTS" if isinstance(exc, WorkflowTargetExistsError) else "EXECUTION_FAILED")
                partial = exc.partial_result if isinstance(exc, WorkflowExecutionError) else None
                if self._result_projector is not None or partial is not None or diagnostic is not None:
                    failed_results = dict(public_results)
                    if partial is not None:
                        failed_results[task_id] = self._project_result(partial)
                    raise WorkflowExecutionError(
                        f"任务 {task_id} 执行失败: {exc}",
                        error_code=code, diagnostic=diagnostic,
                        all_results=failed_results,
                        task_order=order[:index + 1],
                    ) from exc
                raise WorkflowExecutionError(f"任务 {task_id} 执行失败: {exc}", error_code=code, diagnostic=diagnostic) from exc
            if progress:
                progress("running", int((index + 1) * 100 / total), task_id)
        final_result = public_results[order[-1]] if order else None
        return WorkflowRunResult(final_result=final_result, all_results=public_results, task_order=order)


def _normalize_ports(result: Any) -> dict[str, Any]:
    if isinstance(result, dict) and "__ports__" in result:
        ports = result["__ports__"]
        if not isinstance(ports, dict) or not ports:
            raise WorkflowExecutionError("算子 __ports__ 必须是非空对象")
        return ports
    return {"result": result}


def _public_result(ports: dict[str, Any]) -> Any:
    if len(ports) == 1:
        return next(iter(ports.values()))
    return ports


def _resolve_input_data(value: Any, input_data: dict[str, Any]) -> Any:
    if isinstance(value, dict):
        if set(value) == {"$input"}:
            name = value["$input"]
            if not isinstance(name, str) or name not in input_data:
                raise WorkflowExecutionError(f"工作流输入不存在: {name}")
            return input_data[name]
        return {key: _resolve_input_data(nested, input_data) for key, nested in value.items()}
    if isinstance(value, list):
        return [_resolve_input_data(nested, input_data) for nested in value]
    return value
