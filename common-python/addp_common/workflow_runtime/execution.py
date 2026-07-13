from __future__ import annotations

import threading
import time
import uuid
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from typing import Any, Callable

from .errors import WorkflowExecutionError, WorkflowValidationError
from .runner import WorkflowRunner


@dataclass
class ExecutionSnapshot:
    execution_id: str
    status: str
    progress: int = 0
    result: Any = None
    all_results: dict[str, Any] | None = None
    task_order: list[str] = field(default_factory=list)
    current_task: str = ""
    error: str = ""
    error_code: str = ""
    details: str = ""
    started_at: str = ""
    execution_time_ms: float | None = None

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


class ExecutionRegistry:
    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._executions: dict[str, ExecutionSnapshot] = {}

    def clear(self) -> None:
        with self._lock:
            self._executions.clear()

    def get(self, execution_id: str) -> ExecutionSnapshot | None:
        with self._lock:
            snapshot = self._executions.get(execution_id)
            return ExecutionSnapshot(**snapshot.to_dict()) if snapshot else None

    def record(self, snapshot: ExecutionSnapshot) -> None:
        with self._lock:
            self._executions[snapshot.execution_id] = ExecutionSnapshot(**snapshot.to_dict())

    def submit(
        self,
        runner: WorkflowRunner,
        workflow_def: dict[str, Any],
        input_data: dict[str, Any] | None = None,
        *,
        thread_factory: Callable[..., threading.Thread] = threading.Thread,
    ) -> ExecutionSnapshot:
        execution_id = str(uuid.uuid4())
        snapshot = ExecutionSnapshot(
            execution_id=execution_id,
            status="pending",
            started_at=datetime.now(timezone.utc).isoformat(),
        )
        with self._lock:
            self._executions[execution_id] = snapshot
        accepted = ExecutionSnapshot(**snapshot.to_dict())
        thread = thread_factory(
            target=self._run,
            args=(execution_id, runner, workflow_def, input_data or {}),
            daemon=True,
        )
        thread.start()
        return accepted

    def _run(
        self,
        execution_id: str,
        runner: WorkflowRunner,
        workflow_def: dict[str, Any],
        input_data: dict[str, Any],
    ) -> None:
        started = time.monotonic()
        self._update(execution_id, status="running", progress=0)
        try:
            result = runner.execute(
                workflow_def,
                input_data,
                progress=lambda status, progress, task: self._update(
                    execution_id,
                    status=status,
                    progress=progress,
                    current_task=task,
                ),
            )
            self._update(
                execution_id,
                status="success",
                progress=100,
                current_task="",
                result=result.final_result,
                all_results=result.all_results,
                task_order=result.task_order,
                execution_time_ms=(time.monotonic() - started) * 1000,
            )
        except WorkflowValidationError as exc:
            self._fail(execution_id, "WORKFLOW_INVALID", str(exc), started)
        except WorkflowExecutionError as exc:
            self._fail(execution_id, "EXECUTION_FAILED", str(exc), started)
        except Exception as exc:
            self._fail(execution_id, "INTERNAL_ERROR", "工作流运行时内部错误", started, details=str(exc))

    def _fail(self, execution_id: str, code: str, error: str, started: float, *, details: str = "") -> None:
        self._update(
            execution_id,
            status="failed",
            current_task="",
            error=error,
            error_code=code,
            details=details,
            execution_time_ms=(time.monotonic() - started) * 1000,
        )

    def _update(self, execution_id: str, **changes: Any) -> None:
        with self._lock:
            snapshot = self._executions[execution_id]
            for key, value in changes.items():
                setattr(snapshot, key, value)
