from __future__ import annotations

from typing import Any

from .errors import WorkflowExecutionError, WorkflowValidationError


def collect_references(value: Any) -> list[tuple[str, str | None]]:
    references: list[tuple[str, str | None]] = []
    if isinstance(value, dict):
        if "$ref" in value:
            task_id = value.get("$ref")
            port = value.get("port")
            if not isinstance(task_id, str) or not task_id.strip():
                raise WorkflowValidationError("$ref 必须是非空任务 ID")
            if port is not None and (not isinstance(port, str) or not port.strip()):
                raise WorkflowValidationError("$ref.port 必须是非空字符串")
            references.append((task_id.strip(), port.strip() if isinstance(port, str) else None))
            return references
        for nested in value.values():
            references.extend(collect_references(nested))
    elif isinstance(value, list):
        for nested in value:
            references.extend(collect_references(nested))
    return references


def resolve_references(value: Any, results: dict[str, dict[str, Any]]) -> Any:
    if isinstance(value, dict):
        if "$ref" in value:
            task_id = str(value["$ref"]).strip()
            if task_id not in results:
                raise WorkflowExecutionError(f"引用的任务尚无结果: {task_id}")
            return _select_port(task_id, results[task_id], value.get("port"))
        return {key: resolve_references(nested, results) for key, nested in value.items()}
    if isinstance(value, list):
        return [resolve_references(nested, results) for nested in value]
    return value


def _select_port(task_id: str, ports: dict[str, Any], requested_port: Any) -> Any:
    if requested_port is not None:
        port = str(requested_port).strip()
        if port not in ports:
            raise WorkflowExecutionError(f"任务 {task_id} 不存在输出端口 {port}")
        return ports[port]
    if "default" in ports:
        return ports["default"]
    if "result" in ports:
        return ports["result"]
    if len(ports) == 1:
        return next(iter(ports.values()))
    raise WorkflowExecutionError(f"任务 {task_id} 有多个输出端口，必须显式指定 port")
