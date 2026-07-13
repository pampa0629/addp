from __future__ import annotations

from collections import deque
from typing import Any

from .errors import WorkflowValidationError


def topological_order(tasks: list[dict[str, Any]]) -> list[str]:
    by_id = {task["id"]: task for task in tasks}
    indegree = {task_id: 0 for task_id in by_id}
    dependents: dict[str, list[str]] = {task_id: [] for task_id in by_id}
    for task in tasks:
        task_id = task["id"]
        for dependency in task["depends_on"]:
            indegree[task_id] += 1
            dependents[dependency].append(task_id)

    queue = deque(task["id"] for task in tasks if indegree[task["id"]] == 0)
    ordered: list[str] = []
    while queue:
        task_id = queue.popleft()
        ordered.append(task_id)
        for dependent in dependents[task_id]:
            indegree[dependent] -= 1
            if indegree[dependent] == 0:
                queue.append(dependent)
    if len(ordered) != len(tasks):
        raise WorkflowValidationError("工作流依赖包含环")
    return ordered
