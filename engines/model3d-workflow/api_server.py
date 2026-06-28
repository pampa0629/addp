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

from operators import ConverterError, converter_status, invoke_operator, list_operators


app = Flask(__name__)
CORS(app)

logging.basicConfig(level=logging.INFO, format="%(asctime)s - %(name)s - %(levelname)s - %(message)s")
logger = logging.getLogger(__name__)

start_time = datetime.now()
executions: dict[str, dict[str, Any]] = {}


class ErrorCode:
    OPERATOR_NOT_FOUND = "OPERATOR_NOT_FOUND"
    INVALID_PARAMS = "INVALID_PARAMS"
    WORKFLOW_NOT_SUPPORTED = "WORKFLOW_NOT_SUPPORTED"
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
    response = error_response(
        ErrorCode.WORKFLOW_NOT_SUPPORTED,
        "model3d_workflow first phase supports direct operator invocation only",
    )
    response["execution_time_ms"] = (time.time() - start) * 1000
    return jsonify(response), 400


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
        executions[execution_id] = _execution_record("failed", response=response)
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
        executions[execution_id] = {
            "status": "success",
            "result": result,
            "started_at": datetime.now().isoformat(),
            "execution_time_ms": execution_time_ms,
            "message": "direct operator invocation completed",
        }
        return jsonify(response), 200
    except ConverterError as exc:
        execution_time_ms = (time.time() - started) * 1000
        response = error_response(exc.error_code, exc.message, exc.details)
        response["execution_id"] = execution_id
        response["execution_time_ms"] = execution_time_ms
        executions[execution_id] = _execution_record("failed", response=response)
        return jsonify(response), exc.http_status
    except Exception as exc:
        logger.exception("model3d operator invocation failed")
        execution_time_ms = (time.time() - started) * 1000
        response = error_response(ErrorCode.INTERNAL_ERROR, "internal model3d workflow error", str(exc))
        response["execution_id"] = execution_id
        response["execution_time_ms"] = execution_time_ms
        executions[execution_id] = _execution_record("failed", response=response)
        return jsonify(response), 500


@app.route("/api/executions/<execution_id>", methods=["GET"])
def get_execution_status(execution_id: str):
    execution = executions.get(execution_id)
    if execution is None:
        return jsonify(error_response(ErrorCode.EXECUTION_NOT_FOUND, "Execution not found")), 404
    return jsonify(
        {
            "status": execution["status"],
            "execution_id": execution_id,
            "result": execution.get("result"),
            "all_results": execution.get("all_results"),
            "error": execution.get("error"),
            "error_code": execution.get("error_code"),
            "details": execution.get("details"),
            "progress": 100 if execution["status"] in {"success", "failed"} else 50,
            "started_at": execution.get("started_at"),
            "execution_time_ms": execution.get("execution_time_ms"),
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
        "description": "三维模型转换专用工作流运行时，提供 OSGB 快显和 OSGB Scene 转 3D Tiles direct 算子",
        "connection_info": {
            "protocol": protocol,
            "port": port,
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


def _execution_record(status: str, *, response: dict[str, Any]) -> dict[str, Any]:
    return {
        "status": status,
        "result": response.get("result"),
        "error": response.get("error"),
        "error_code": response.get("error_code"),
        "details": response.get("details"),
        "started_at": datetime.now().isoformat(),
        "execution_time_ms": response.get("execution_time_ms"),
        "message": response.get("error") if status == "failed" else "completed",
    }


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
