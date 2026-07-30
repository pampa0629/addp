from __future__ import annotations

from typing import Any, Collection

from .errors import WorkflowValidationError
from .graph import topological_order
from .references import collect_references


ALLOWED_EXECUTION_EFFECTS = {"read", "write", "ddl", "external_effect"}


def validate_workflow_def(
    workflow_def: Any,
    *,
    operator_ids: Collection[str] | None = None,
) -> list[dict[str, Any]]:
    if not isinstance(workflow_def, dict):
        raise WorkflowValidationError("workflow_def 必须是对象")
    tasks = workflow_def.get("tasks")
    if not isinstance(tasks, list) or not tasks:
        raise WorkflowValidationError("workflow_def.tasks 必须是非空数组")

    normalized: list[dict[str, Any]] = []
    task_ids: set[str] = set()
    allowed_operators = set(operator_ids) if operator_ids is not None else None
    for index, raw_task in enumerate(tasks):
        if not isinstance(raw_task, dict):
            raise WorkflowValidationError(f"任务 {index} 必须是对象")
        task_id = _required_text(raw_task, "id", index)
        operator = _required_text(raw_task, "operator", index)
        if task_id in task_ids:
            raise WorkflowValidationError(f"任务 ID 重复: {task_id}")
        if allowed_operators is not None and operator not in allowed_operators:
            raise WorkflowValidationError(f"算子不存在或不支持 workflow: {operator}")
        params = raw_task.get("params")
        if not isinstance(params, dict):
            raise WorkflowValidationError(f"任务 {task_id} 的 params 必须是对象")
        dependencies = raw_task.get("depends_on")
        if not isinstance(dependencies, list) or not all(isinstance(item, str) and item.strip() for item in dependencies):
            raise WorkflowValidationError(f"任务 {task_id} 的 depends_on 必须是字符串数组")
        dependencies = [item.strip() for item in dependencies]
        if len(dependencies) != len(set(dependencies)):
            raise WorkflowValidationError(f"任务 {task_id} 的 depends_on 包含重复依赖")
        task_ids.add(task_id)
        normalized.append({"id": task_id, "operator": operator, "params": params, "depends_on": dependencies})

    all_ids = {task["id"] for task in normalized}
    for task in normalized:
        task_id = task["id"]
        for dependency in task["depends_on"]:
            if dependency not in all_ids:
                raise WorkflowValidationError(f"任务 {task_id} 依赖不存在的任务 {dependency}")
            if dependency == task_id:
                raise WorkflowValidationError(f"任务 {task_id} 不能依赖自身")
        dependency_set = set(task["depends_on"])
        for referenced_task, _ in collect_references(task["params"]):
            if referenced_task not in all_ids:
                raise WorkflowValidationError(f"任务 {task_id} 引用不存在的任务 {referenced_task}")
            if referenced_task not in dependency_set:
                raise WorkflowValidationError(f"任务 {task_id} 引用了任务 {referenced_task} 但未在 depends_on 中声明")

    topological_order(normalized)
    return normalized


def validate_execution_authorization(
    workflow_def: Any,
    *,
    operator_effects: dict[str, Collection[str]],
    runtime: Any,
) -> list[str]:
    """Validate that runtime authorization covers every operator effect in the DAG."""

    tasks = validate_workflow_def(workflow_def, operator_ids=operator_effects.keys())
    if not isinstance(runtime, dict):
        raise WorkflowValidationError("runtime.execution_authorization 必须由调用方提供")
    authorization = runtime.get("execution_authorization")
    if not isinstance(authorization, dict):
        raise WorkflowValidationError("runtime.execution_authorization 必须是对象")
    authorization_id = authorization.get("id")
    if not isinstance(authorization_id, int) or isinstance(authorization_id, bool) or authorization_id <= 0:
        raise WorkflowValidationError("runtime.execution_authorization.id 必须是正整数")
    authorized_effects = authorization.get("effects")
    if (
        not isinstance(authorized_effects, list)
        or not authorized_effects
        or any(not isinstance(effect, str) or not effect for effect in authorized_effects)
        or len(authorized_effects) != len(set(authorized_effects))
        or not set(authorized_effects).issubset(ALLOWED_EXECUTION_EFFECTS)
    ):
        raise WorkflowValidationError("runtime.execution_authorization.effects 无效")

    required_effects: set[str] = set()
    for task in tasks:
        effects = operator_effects.get(task["operator"])
        if (
            not isinstance(effects, Collection)
            or isinstance(effects, (str, bytes))
            or not effects
            or any(effect not in ALLOWED_EXECUTION_EFFECTS for effect in effects)
        ):
            raise WorkflowValidationError(f"算子 {task['operator']} 未声明有效 effects")
        required_effects.update(effects)
    missing = required_effects - set(authorized_effects)
    if missing:
        raise WorkflowValidationError(
            "Execution Authorization 不包含工作流所需效果: " + ", ".join(sorted(missing))
        )
    return [
        effect
        for effect in ("read", "write", "ddl", "external_effect")
        if effect in required_effects
    ]


def _required_text(task: dict[str, Any], key: str, index: int) -> str:
    value = task.get(key)
    if not isinstance(value, str) or not value.strip():
        raise WorkflowValidationError(f"任务 {index} 的 {key} 必须是非空字符串")
    return value.strip()
