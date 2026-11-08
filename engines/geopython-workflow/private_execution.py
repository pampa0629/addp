"""A private-file DAG uses the ordinary Runner in an owned child process."""
from __future__ import annotations

import json
import multiprocessing
import os
import signal
import threading
import time
from pathlib import Path


def _project_private_result(value):
    import geopandas as gpd
    import pandas as pd

    if isinstance(value, pd.DataFrame):
        return {"runtime_type": "geodataframe" if isinstance(value, gpd.GeoDataFrame) else "dataframe",
                "row_count": len(value)}
    return {"runtime_type": "value"}


def _run_private_dag(directory: str, workflow: dict, language: str, deadline: float) -> None:
    # Only private local files are passed in. Source access and final publication
    # belong to Develop, and neither is performed by this worker.
    # The POSIX child also owns its deadline: loss of the HTTP parent must not
    # leave native computation running without the supervisor's timer.
    remaining = deadline - time.time()
    if remaining <= 0:
        return
    signal.signal(signal.SIGALRM, signal.SIG_DFL)
    signal.setitimer(signal.ITIMER_REAL, remaining)
    from addp_common.workflow_runtime import WorkflowExecutionError, WorkflowValidationError
    from workflow_engine import GeoPythonWorkflowRunner

    os.umask(0o077)
    try:
        result = GeoPythonWorkflowRunner(language=language, result_projector=_project_private_result).execute(workflow)
        report = {"status": "success", "result": result.final_result,
                  "all_results": result.all_results, "task_order": result.task_order}
    except WorkflowExecutionError as exc:
        # The diagnostic is the existing controlled projection. Native exception
        # text can contain local paths and is not a private-file API response.
        report = {"status": "failed", "error_code": exc.error_code,
                  "all_results": exc.all_results, "task_order": exc.task_order,
                  "diagnostic": exc.diagnostic}
    except WorkflowValidationError:
        report = {"status": "failed", "error_code": "WORKFLOW_INVALID"}
    except Exception:
        report = {"status": "failed", "error_code": "EXECUTION_FAILED"}
    root = Path(directory)
    # A result file avoids a pipe buffer making child exit depend on the parent
    # draining a potentially large result before join.
    with (root / "result.pending").open("x", encoding="utf-8") as stream:
        json.dump(report, stream, ensure_ascii=False, allow_nan=False)
    (root / "result.pending").replace(root / "result.json")


class PrivateWorkflowProcess:
    """Supervise one already-bound private DAG; never restart its computation.

    This object does not own business resources, authorization, or NFS cleanup.
    Its caller owns the private directory until this process has actually exited.
    """

    def __init__(self, directory: Path, workflow: dict, language: str, deadline: float):
        self.directory = directory
        self.workflow = workflow
        self.language = language
        self.deadline = deadline
        self._lock = threading.RLock()
        self._process = None
        self._monitor = None
        self._terminal = threading.Event()
        self._stop_reason = None
        self._report = {"status": "pending"}

    def start(self) -> None:
        with self._lock:
            if self._process is not None or self._terminal.is_set():
                raise RuntimeError("private workflow has already been dispatched")
            if time.time() >= self.deadline:
                self._report = {"status": "failed", "error_code": "EXECUTION_EXPIRED"}
                self._terminal.set()
                return
            process = multiprocessing.get_context("spawn").Process(
                target=_run_private_dag,
                args=(str(self.directory), self.workflow, self.language, self.deadline),
            )
            self._process = process
            try:
                process.start()
                self._monitor = threading.Thread(target=self._observe, daemon=True)
                self._monitor.start()
            except Exception:
                self._report = {"status": "failed", "error_code": "EXECUTION_FAILED"}
                if process.is_alive():
                    self._stop(process)
                    self._report["cleanup_pending"] = process.is_alive()
                self._terminal.set()
                raise
            self._report = {"status": "running"}

    def _observe(self) -> None:
        process = self._process
        process.join(max(0, self.deadline - time.time()))
        with self._lock:
            if process.is_alive():
                self._stop_reason = self._stop_reason or "EXECUTION_EXPIRED"
                self._stop(process)
            if process.is_alive():
                self._report = {"status": "failed", "error_code": self._stop_reason,
                                "cleanup_pending": True}
            elif self._stop_reason is not None or time.time() >= self.deadline:
                self._report = {"status": "failed", "error_code": self._stop_reason or "EXECUTION_EXPIRED"}
            elif process.exitcode != 0:
                self._report = {"status": "failed", "error_code": "EXECUTION_FAILED"}
            else:
                try:
                    self._report = json.loads((self.directory / "result.json").read_text(encoding="utf-8"))
                except (OSError, ValueError):
                    self._report = {"status": "failed", "error_code": "EXECUTION_FAILED"}
            self._terminal.set()

    @staticmethod
    def _stop(process) -> None:
        process.terminate()
        process.join(1)
        if process.is_alive():
            process.kill()
            process.join(1)

    def cancel(self) -> None:
        with self._lock:
            if self._terminal.is_set():
                return
            self._stop_reason = "EXECUTION_CANCELLED"
            if self._process is None:
                self._report = {"status": "failed", "error_code": self._stop_reason}
                self._terminal.set()
            elif self._process.is_alive():
                self._process.terminate()

    def wait(self, timeout: float | None = None) -> bool:
        return self._terminal.wait(timeout)

    def snapshot(self) -> dict:
        with self._lock:
            # API callers cannot mutate the process's terminal observation.
            return json.loads(json.dumps(self._report))

    def stopped(self) -> bool:
        with self._lock:
            return self._terminal.is_set() and (self._process is None or not self._process.is_alive())
