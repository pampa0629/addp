"""
Math Workflow Engine - API 服务

实现 ADDP 工作流计算引擎的 5 个标准接口：
1. GET /health - 健康检查
2. GET /api/operators - 获取算子列表
3. POST /api/workflow - 执行工作流（DAG）
4. POST /api/operators/<name>/invoke - direct 调用单个算子
5. GET /api/executions/<execution_id> - 查询执行状态

注意：引擎层面使用通用 API 路径，不包含领域特定前缀（如 spatial）
"""

from flask import Flask, request, jsonify
from flask_cors import CORS
import uuid
import logging
import time
import os
from datetime import datetime

from operators import list_operators, get_operator_function, OPERATORS
from addp_common.workflow_runtime import ExecutionRegistry, WorkflowRunner, WorkflowValidationError, validate_execution_authorization, validate_workflow_def

# ============ 应用初始化 ============

app = Flask(__name__)
CORS(app)

# 配置日志
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

# 启动时间（用于计算 uptime）
start_time = datetime.now()
executions = ExecutionRegistry()

# ============ 错误码定义 ============

class ErrorCode:
    """标准错误码"""
    OPERATOR_NOT_FOUND = "OPERATOR_NOT_FOUND"
    INVALID_PARAMS = "INVALID_PARAMS"
    EXECUTION_FAILED = "EXECUTION_FAILED"
    WORKFLOW_INVALID = "WORKFLOW_INVALID"
    DIRECT_NOT_SUPPORTED = "DIRECT_NOT_SUPPORTED"
    EXECUTION_NOT_FOUND = "EXECUTION_NOT_FOUND"
    INTERNAL_ERROR = "INTERNAL_ERROR"


def error_response(error_code: str, message: str, details: str = None):
    """构造标准错误响应"""
    response = {
        "status": "failed",
        "error": message,
        "error_code": error_code
    }
    if details:
        response["details"] = details
    return response


# ============ API 端点实现 ============

@app.route('/health', methods=['GET'])
def health():
    """
    健康检查（符合 OpenAPI 规范）

    Returns:
        {
            "status": "healthy",
            "service": "math-workflow-engine",
            "version": "1.0.0",
            "uptime": 3600,
            "operators_count": 5
        }
    """
    uptime = int((datetime.now() - start_time).total_seconds())

    return jsonify({
        "status": "healthy",
        "service": "math-workflow-engine",
        "version": "1.0.0",
        "uptime": uptime,
        "operators_count": len(OPERATORS)
    }), 200


@app.route('/api/operators', methods=['GET'])
def get_operators():
    """
    获取算子列表（符合 OpenAPI 规范）

    Query Parameters:
        category (optional): 按分类过滤

    Returns:
        {
            "status": "success",
            "operators": [...],
            "count": 5
        }
    """
    category = request.args.get('category')

    operators = list_operators()

    # 按分类过滤
    if category:
        operators = [op for op in operators if op.get('category') == category]

    return jsonify({
        "status": "success",
        "operators": operators,
        "count": len(operators)
    }), 200


@app.route('/api/workflow', methods=['POST'])
def execute_workflow():
    """
    执行工作流（关键接口，支持 DAG）

    Request Body:
        {
            "workflow_def": {
                "tasks": [
                    {"id": "task1", "operator": "add", "params": {"a": 10, "b": 20}, "depends_on": []},
                    {"id": "task2", "operator": "multiply", "params": {"a": {"$ref": "task1"}, "b": 2}, "depends_on": ["task1"]}
                ]
            },
            "input_data": {} (可选)
        }

    Returns:
        {
            "status": "success",
            "execution_id": "uuid-1234",
            "final_result": 60,
            "all_results": {"task1": 30, "task2": 60}
        }
    """
    start = time.time()
    try:
        data = request.get_json(silent=True) or {}

        if not data or 'workflow_def' not in data:
            response = error_response(
                ErrorCode.INVALID_PARAMS,
                "请求体缺少 'workflow_def' 字段"
            )
            response["execution_time_ms"] = (time.time() - start) * 1000
            return jsonify(response), 400

        workflow_def = data['workflow_def']
        input_data = data.get('input_data', {})
        if not isinstance(input_data, dict):
            input_data = {}
        operator_ids = set(OPERATORS)
        validate_workflow_def(workflow_def, operator_ids=operator_ids)
        operator_effects = {
            operator["id"]: operator["effects"]
            for operator in list_operators()
            if "workflow" in operator.get("execution_modes", [])
        }
        validate_execution_authorization(
            workflow_def,
            operator_effects=operator_effects,
            runtime=data.get("runtime"),
        )
        runner = WorkflowRunner(operator_ids, lambda operator, params: get_operator_function(operator)(**params))
        snapshot = executions.submit(runner, workflow_def, input_data)
        return jsonify({
            "status": snapshot.status,
            "execution_id": snapshot.execution_id,
            "execution_time_ms": (time.time() - start) * 1000
        }), 202

    except WorkflowValidationError as e:
        logger.error(f"工作流定义无效: {e}")
        execution_time = (time.time() - start) * 1000
        response = error_response(
            ErrorCode.WORKFLOW_INVALID,
            str(e)
        )
        response["execution_time_ms"] = execution_time
        return jsonify(response), 400

    except Exception as e:
        logger.exception(f"工作流执行失败: {e}")
        execution_time = (time.time() - start) * 1000
        response = error_response(
            ErrorCode.EXECUTION_FAILED,
            f"工作流执行失败: {str(e)}"
        )
        response["execution_time_ms"] = execution_time
        return jsonify(response), 500


@app.route('/api/operators/<name>/invoke', methods=['POST'])
def invoke_operator(name):
    """
    direct 调用单个算子。

    仅允许调用 execution_modes 包含 direct 的算子；Math Workflow 内置算子默认只支持 workflow。

    Path Parameters:
        name: 算子名称

    Request Body:
        {
            "params": {"a": 5, "b": 3}
        }

    Returns:
        {
            "status": "success",
            "result": 8
        }
    """
    start = time.time()
    try:
        # 检查算子是否存在
        if name not in OPERATORS:
            response = error_response(
                ErrorCode.OPERATOR_NOT_FOUND,
                f"算子 '{name}' 不存在",
                details=f"可用算子: {', '.join(OPERATORS.keys())}"
            )
            response["execution_time_ms"] = (time.time() - start) * 1000
            return jsonify(response), 404

        operator_meta = OPERATORS[name]["metadata"].model_dump()
        execution_modes = operator_meta["execution_modes"]
        if "direct" not in execution_modes:
            response = error_response(
                ErrorCode.DIRECT_NOT_SUPPORTED,
                f"算子 '{name}' 不支持 direct 调用"
            )
            response["execution_time_ms"] = (time.time() - start) * 1000
            return jsonify(response), 403

        data = request.get_json()

        if not data or 'params' not in data:
            response = error_response(
                ErrorCode.INVALID_PARAMS,
                "请求体缺少 'params' 字段"
            )
            response["execution_time_ms"] = (time.time() - start) * 1000
            return jsonify(response), 400

        params = data['params']

        logger.info(f"direct 调用算子 '{name}': params={params}")

        # 获取算子函数并执行
        operator_func = get_operator_function(name)
        result = operator_func(**params)

        execution_time = (time.time() - start) * 1000

        logger.info(f"算子执行成功: '{name}', result={result}, 耗时={execution_time:.2f}ms")

        return jsonify({
            "status": "success",
            "result": result,
            "execution_time_ms": execution_time
        }), 200

    except TypeError as e:
        logger.error(f"参数错误: {e}")
        response = error_response(
            ErrorCode.INVALID_PARAMS,
            f"参数错误: {str(e)}"
        )
        response["execution_time_ms"] = (time.time() - start) * 1000
        return jsonify(response), 400

    except Exception as e:
        logger.exception(f"算子执行失败: {e}")
        response = error_response(
            ErrorCode.EXECUTION_FAILED,
            f"算子执行失败: {str(e)}"
        )
        response["execution_time_ms"] = (time.time() - start) * 1000
        return jsonify(response), 500


@app.route('/api/executions/<execution_id>', methods=['GET'])
def get_execution_status(execution_id):
    """
    查询执行状态

    Returns:
        {
            "status": "success",
            "execution_id": "uuid-1234",
            "result": 60,
            "progress": 100
        }
    """
    execution = executions.get(execution_id)
    if execution is None:
        return jsonify(error_response(
            ErrorCode.EXECUTION_NOT_FOUND,
            "Execution not found"
        )), 404

    return jsonify({
        "status": execution.status,
        "execution_id": execution_id,
        "result": execution.result,
        "all_results": execution.all_results,
        "task_order": execution.task_order,
        "current_task": execution.current_task,
        "progress": execution.progress,
        "error": execution.error,
        "error_code": execution.error_code,
        "details": execution.details,
        "started_at": execution.started_at,
        "execution_time_ms": execution.execution_time_ms
    }), 200


# ============ 应用启动 ============

if __name__ == '__main__':
    port = int(os.getenv('PORT', 8089))
    logger.info(f"🚀 Math Workflow Engine 启动: http://0.0.0.0:{port}")
    logger.info("   示例服务已启动，但不会自动注册到 System；请在 System 引擎管理中按扩展引擎手动注册")
    logger.info(f"   算子数量: {len(OPERATORS)}")
    logger.info(f"   OpenAPI 文档: ../docs/workflow-engine-api-v1.yaml")

    app.run(host='0.0.0.0', port=port, debug=False)
