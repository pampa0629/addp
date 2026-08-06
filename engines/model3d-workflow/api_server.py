from __future__ import annotations

import logging
import json
import os
import time
import uuid
from datetime import datetime
from typing import Any
from urllib import error as urlerror
from urllib import request as urlrequest

from flask import Flask, jsonify, request
from flask_cors import CORS

from addp_common.workflow_access import WorkflowAccessError
from addp_common.workflow_runtime import ExecutionRegistry, ExecutionSnapshot, WorkflowRunner, WorkflowValidationError, validate_execution_authorization, validate_workflow_def
from operators import ConverterError, converter_status, invoke_operator, list_operators


app = Flask(__name__)
CORS(app)

logging.basicConfig(level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s")
logger = logging.getLogger(__name__)

start_time = datetime.now()
executions = ExecutionRegistry()


class ErrorCode:
    OPERATOR_NOT_FOUND = "OPERATOR_NOT_FOUND"
    INVALID_PARAMS = "INVALID_PARAMS"
    WORKFLOW_INVALID = "WORKFLOW_INVALID"
    EXECUTION_FAILED = "EXECUTION_FAILED"
    EXECUTION_NOT_FOUND = "EXECUTION_NOT_FOUND"
    INTERNAL_ERROR = "INTERNAL_ERROR"


def error_response(error_code: str, message: str, details: str | None = None) -> dict[str, Any]:
    response: dict[str, Any] = {
        "status": "failed",
        "error": message,
        "error_code": error_code,
    }
    if details:
        response["details"] = details
    return response


@app.route("/health", methods=["GET"])
def health():
    uptime = int((datetime.now() - start_time).total_seconds())
    converter = converter_status()
    conversion_ready = bool(converter.get("available"))
    return jsonify(
        {
            "status": "healthy" if conversion_ready else "degraded",
            "service": "model3d-workflow-engine",
            "version": "1.0.0",
            "uptime": uptime,
            "operators_count": len(list_operators()),
            "conversion_ready": conversion_ready,
            "dependencies": {
                "converter": converter,
            },
        }
    ), 200


@app.route("/api/operators", methods=["GET"])
def get_operators():
    category = request.args.get("category")
    operators = list_operators()
    if category:
        operators = [operator for operator in operators if operator.get("category") == category]
    return jsonify({"status": "success", "operators": operators, "count": len(operators)}), 200


@app.route("/api/workflow", methods=["POST"])
def execute_workflow():
    start = time.time()
    data = request.get_json(silent=True) or {}
    workflow_def = data.get("workflow_def")
    input_data = data.get("input_data")
    if not isinstance(input_data, dict):
        input_data = {}
    operator_ids = {operator["id"] for operator in list_operators() if "workflow" in operator.get("execution_modes", [])}
    try:
        validate_workflow_def(workflow_def, operator_ids=operator_ids)
        validate_execution_authorization(
            workflow_def,
            operator_effects={
                operator["id"]: operator["effects"]
                for operator in list_operators()
                if "workflow" in operator.get("execution_modes", [])
            },
            runtime=data.get("runtime"),
        )
    except WorkflowValidationError as exc:
        response = error_response(ErrorCode.WORKFLOW_INVALID, str(exc))
        response["execution_time_ms"] = (time.time() - start) * 1000
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
        "execution_time_ms": (time.time() - start) * 1000,
    }), 202


@app.route("/api/operators/<name>/invoke", methods=["POST"])
def invoke_single_operator(name: str):
    started = time.time()
    execution_id = str(uuid.uuid4())
    data = request.get_json(silent=True) or {}
    params = data.get("params")
    if not isinstance(params, dict):
        response = error_response(ErrorCode.INVALID_PARAMS, "request body must contain object field 'params'")
        response["execution_id"] = execution_id
        response["execution_time_ms"] = (time.time() - started) * 1000
        executions.record(_execution_snapshot(execution_id, "failed", response))
        return jsonify(response), 400

    timeout_seconds = _timeout_seconds(data.get("timeout_seconds"))

    try:
        result = invoke_operator(name, params, timeout_seconds=timeout_seconds)
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
        execution_time_ms = (time.time() - started) * 1000
        response = error_response(exc.error_code, exc.message, exc.details)
        response["execution_id"] = execution_id
        response["execution_time_ms"] = execution_time_ms
        executions.record(_execution_snapshot(execution_id, "failed", response))
        return jsonify(response), exc.http_status
    except WorkflowAccessError as exc:
        execution_time_ms = (time.time() - started) * 1000
        response = error_response(ErrorCode.INVALID_PARAMS, str(exc))
        response["execution_id"] = execution_id
        response["execution_time_ms"] = execution_time_ms
        executions.record(_execution_snapshot(execution_id, "failed", response))
        return jsonify(response), 400
    except Exception as exc:
        logger.exception("model3d operator invocation failed")
        execution_time_ms = (time.time() - started) * 1000
        response = error_response(ErrorCode.INTERNAL_ERROR, "internal model3d workflow error", str(exc))
        response["execution_id"] = execution_id
        response["execution_time_ms"] = execution_time_ms
        executions.record(_execution_snapshot(execution_id, "failed", response))
        return jsonify(response), 500


@app.route("/api/executions/<execution_id>", methods=["GET"])
def get_execution_status(execution_id: str):
    execution = executions.get(execution_id)
    if execution is None:
        return jsonify(error_response(ErrorCode.EXECUTION_NOT_FOUND, "Execution not found")), 404
    return jsonify(
        {
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
        }
    ), 200


def register_to_system() -> bool:
    converter = converter_status()
    if not converter.get("available"):
        logger.warning("skip model3d_workflow registration because converter is not bound: %s", converter.get("details"))
        return False

    system_url = os.getenv("SYSTEM_URL", "http://localhost:8180")
    api_key = os.getenv("INTERNAL_API_KEY", "")
    port = int(os.getenv("PORT", 8101))
    protocol = os.getenv("PROTOCOL", "http")

    payload = {
        "engine_type": "model3d_workflow",
        "name": "Model3D 工作流引擎",
        "description": "三维模型与 Gaussian Splat 持久化转换工作流运行时，算子同时支持 workflow 与受控 direct 调用",
        "connection_info": {
            "protocol": protocol,
            "port": port,
        },
        "capabilities": {
            "schema_version": "engine.capabilities/v1",
            "engine_type": "model3d_workflow",
            "engine_family": "workflow",
            "compute": {
                "workflow": {
                    "supported": True,
                    "runtime_api": "addp.workflow/v1",
                    "dynamic_operators": True,
                }
            },
        },
        "is_builtin": True,
    }

    headers = {
        "Content-Type": "application/json",
        "X-Internal-API-Key": api_key,
    }

    request_body = json.dumps(payload).encode("utf-8")
    register_url = f"{system_url}/api/v1/internal/engines/register"
    req = urlrequest.Request(register_url, data=request_body, headers=headers, method="POST")

    try:
        with urlrequest.urlopen(req, timeout=10) as response:
            body = response.read().decode("utf-8", errors="replace")
            status_code = response.status
        if status_code == 202:
            logger.info("model3d_workflow registered to System")
            return True
        logger.warning("failed to register model3d_workflow: %s - %s", status_code, body)
        return False
    except urlerror.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        logger.warning("failed to register model3d_workflow: %s - %s", exc.code, body)
        return False
    except Exception as exc:
        logger.warning("failed to register model3d_workflow: %s", exc)
        return False


def register_to_system_with_retry() -> None:
    import threading

    max_retries = 5
    retry_interval = 10

    for attempt in range(1, max_retries + 1):
        logger.info("attempting to register model3d_workflow to System (%s/%s)", attempt, max_retries)
        if register_to_system():
            logger.info("model3d_workflow registration succeeded on attempt %s", attempt)
            return
        if attempt < max_retries:
            threading.Event().wait(retry_interval)

    logger.error("model3d_workflow registration failed after %s attempts", max_retries)


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
    import threading

    registration_thread = threading.Thread(target=register_to_system_with_retry, daemon=True)
    registration_thread.start()

    port = int(os.getenv("PORT", 8101))
    logger.info("Model3D Workflow Engine listening on http://0.0.0.0:%s", port)
    logger.info("Operators: %s", len(list_operators()))
    app.run(host="0.0.0.0", port=port, debug=False)
