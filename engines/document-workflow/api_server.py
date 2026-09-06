from __future__ import annotations

import logging
import os
import threading
import time
import uuid
from datetime import datetime
from typing import Any

from flask import Flask, jsonify, request
from flask_cors import CORS

from addp_common.workflow_access import WorkflowAccessError
from addp_common.workflow_runtime import (
    ExecutionRegistry,
    ExecutionSnapshot,
    WorkflowRunner,
    WorkflowValidationError,
    validate_execution_authorization,
    validate_workflow_def,
)
from operators import ConverterError, converter_status, invoke_operator, list_operators


app = Flask(__name__)
CORS(app)

logging.basicConfig(level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s")
logger = logging.getLogger(__name__)

start_time = datetime.now()
executions = ExecutionRegistry()


class ErrorCode:
    INVALID_PARAMS = "INVALID_PARAMS"
    WORKFLOW_INVALID = "WORKFLOW_INVALID"
    EXECUTION_NOT_FOUND = "EXECUTION_NOT_FOUND"
    INTERNAL_ERROR = "INTERNAL_ERROR"


def error_response(error_code: str, message: str, details: str | None = None) -> dict[str, Any]:
    response: dict[str, Any] = {"status": "failed", "error": message, "error_code": error_code}
    if details:
        response["details"] = details
    return response


@app.route("/health", methods=["GET"])
def health():
    converter = converter_status()
    conversion_ready = bool(converter.get("available"))
    return jsonify({
        "status": "healthy" if conversion_ready else "degraded",
        "service": "document-workflow-engine",
        "version": "1.0.0",
        "uptime": int((datetime.now() - start_time).total_seconds()),
        "operators_count": len(list_operators()),
        "conversion_ready": conversion_ready,
        "dependencies": {"libreoffice": converter},
    }), 200


@app.route("/api/operators", methods=["GET"])
def get_operators():
    category = request.args.get("category")
    operators = list_operators()
    if category:
        operators = [operator for operator in operators if operator.get("category") == category]
    return jsonify({"status": "success", "operators": operators, "count": len(operators)}), 200


@app.route("/api/workflow", methods=["POST"])
def execute_workflow():
    started = time.time()
    data = request.get_json(silent=True) or {}
    workflow_def = data.get("workflow_def")
    input_data = data.get("input_data") if isinstance(data.get("input_data"), dict) else {}
    operators = list_operators()
    operator_ids = {operator["id"] for operator in operators if "workflow" in operator.get("execution_modes", [])}
    try:
        validate_workflow_def(workflow_def, operator_ids=operator_ids)
        validate_execution_authorization(
            workflow_def,
            operator_effects={
                operator["id"]: operator["effects"]
                for operator in operators
                if "workflow" in operator.get("execution_modes", [])
            },
            runtime=data.get("runtime"),
        )
    except WorkflowValidationError as exc:
        response = error_response(ErrorCode.WORKFLOW_INVALID, str(exc))
        response["execution_time_ms"] = (time.time() - started) * 1000
        return jsonify(response), 400

    timeout_seconds = _timeout_seconds(data.get("timeout_seconds"))
    runner = WorkflowRunner(
        operator_ids,
        lambda operator, params: invoke_operator(operator, params, timeout_seconds=timeout_seconds),
    )
    snapshot = executions.submit(runner, workflow_def, input_data)
    return jsonify({
        "status": snapshot.status,
        "execution_id": snapshot.execution_id,
        "execution_time_ms": (time.time() - started) * 1000,
    }), 202


@app.route("/api/operators/<name>/invoke", methods=["POST"])
def invoke_single_operator(name: str):
    started = time.time()
    execution_id = str(uuid.uuid4())
    data = request.get_json(silent=True) or {}
    params = data.get("params")
    if not isinstance(params, dict):
        response = error_response(ErrorCode.INVALID_PARAMS, "request body must contain object field 'params'")
        response.update(execution_id=execution_id, execution_time_ms=(time.time() - started) * 1000)
        executions.record(_execution_snapshot(execution_id, "failed", response))
        return jsonify(response), 400

    try:
        result = invoke_operator(name, params, timeout_seconds=_timeout_seconds(data.get("timeout_seconds")))
        execution_time_ms = (time.time() - started) * 1000
        response = {
            "status": "success",
            "execution_id": execution_id,
            "result": result,
            "execution_time_ms": execution_time_ms,
        }
        executions.record(ExecutionSnapshot(
            execution_id=execution_id,
            status="success",
            progress=100,
            result=result,
            started_at=datetime.now().isoformat(),
            execution_time_ms=execution_time_ms,
        ))
        return jsonify(response), 200
    except ConverterError as exc:
        response = error_response(exc.error_code, exc.message, exc.details)
        status_code = exc.http_status
    except WorkflowAccessError as exc:
        response = error_response(ErrorCode.INVALID_PARAMS, str(exc))
        status_code = 400
    except Exception as exc:
        logger.exception("document operator invocation failed")
        response = error_response(ErrorCode.INTERNAL_ERROR, "internal document workflow error", str(exc))
        status_code = 500
    response.update(execution_id=execution_id, execution_time_ms=(time.time() - started) * 1000)
    executions.record(_execution_snapshot(execution_id, "failed", response))
    return jsonify(response), status_code


@app.route("/api/executions/<execution_id>", methods=["GET"])
def get_execution_status(execution_id: str):
    execution = executions.get(execution_id)
    if execution is None:
        return jsonify(error_response(ErrorCode.EXECUTION_NOT_FOUND, "Execution not found")), 404
    return jsonify({
        "status": execution.status,
        "execution_id": execution_id,
        "result": execution.result,
        "all_results": execution.all_results,
        "task_order": execution.task_order,
        "current_task": execution.current_task,
        "error": execution.error,
        "error_code": execution.error_code,
        "details": execution.details,
        "progress": execution.progress,
        "started_at": execution.started_at,
        "execution_time_ms": execution.execution_time_ms,
    }), 200


def register_to_system() -> bool:
    from addp_common.client import register_runtime_engine

    converter = converter_status()
    if not converter.get("available"):
        logger.warning("skip document_workflow registration because LibreOffice is not bound: %s", converter.get("details"))
        return False

    port = int(os.getenv("PORT", 8105))
    runtime_host = os.getenv("RUNTIME_HOST", "localhost").strip()
    connection_info: dict[str, Any] = {"protocol": os.getenv("PROTOCOL", "http"), "port": port}
    if runtime_host:
        connection_info["host"] = runtime_host
    payload = {
        "engine_type": "document_workflow",
        "name": "Document 工作流引擎",
        "description": "文档静态预览产物转换运行时，PPTX 转 PDF 算子同时支持 workflow 与受控 direct 调用",
        "connection_info": connection_info,
        "capabilities": {
            "schema_version": "engine.capabilities/v1",
            "engine_type": "document_workflow",
            "engine_family": "workflow",
            "compute": {"workflow": {
                "supported": True,
                "runtime_api": "addp.workflow/v1",
                "dynamic_operators": True,
            }},
        },
        "is_builtin": True,
    }
    try:
        status_code, body = register_runtime_engine(
            os.getenv("SYSTEM_URL", "http://localhost:8180"),
            "addp-document",
            os.getenv("DOCUMENT_WORKFLOW_SERVICE_CLIENT_SECRET", ""),
            payload,
        )
        if status_code == 202:
            logger.info("document_workflow registered to System")
            return True
        logger.warning("failed to register document_workflow: %s - %s", status_code, body)
        return False
    except Exception as exc:
        logger.warning("failed to register document_workflow: %s", exc)
        return False


def register_to_system_with_retry() -> None:
    converter = converter_status()
    if not converter.get("available"):
        logger.warning("skip document_workflow registration because LibreOffice is not bound: %s", converter.get("details"))
        return
    from addp_common.client import retry_runtime_registration
    retry_runtime_registration(register_to_system, "document_workflow", logger)


def _execution_snapshot(execution_id: str, status: str, response: dict[str, Any]) -> ExecutionSnapshot:
    return ExecutionSnapshot(
        execution_id=execution_id,
        status=status,
        progress=100 if status in {"success", "failed"} else 0,
        result=response.get("result"),
        error=response.get("error") or "",
        error_code=response.get("error_code") or "",
        details=response.get("details") or "",
        started_at=datetime.now().isoformat(),
        execution_time_ms=response.get("execution_time_ms"),
    )


def _timeout_seconds(value: Any) -> int | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, (int, float)) and value > 0:
        return int(value)
    return None


if __name__ == "__main__":
    threading.Thread(target=register_to_system_with_retry, daemon=True).start()
    port = int(os.getenv("PORT", 8105))
    logger.info("Document Workflow Engine listening on http://0.0.0.0:%s", port)
    logger.info("Operators: %s", len(list_operators()))
    app.run(host="0.0.0.0", port=port, debug=False)
