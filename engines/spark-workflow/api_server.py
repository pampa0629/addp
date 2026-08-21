"""
Spark 工作流引擎 Flask API Server
提供 REST API 接口供 Develop 和 Orchestrator 调用
"""

from flask import Flask, request, jsonify
from flask_cors import CORS
import json
import logging
import uuid
from datetime import datetime
import os
import time

# 加载环境变量（从项目根目录的 .env 文件）
from dotenv import load_dotenv, find_dotenv
# 自动向上搜索 .env 文件（类似 common/config/loader.go 的 LoadEnv）
load_dotenv(find_dotenv())

from workflow_engine import execute_workflow, execute_single_operator
from addp_common.workflow_runtime import validate_execution_authorization
from operators import list_operators
from operators import OPERATORS as OPERATOR_REGISTRY

try:
    from pyspark.sql import DataFrame
except Exception:
    DataFrame = None

# 配置日志
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

app = Flask(__name__)
CORS(app)  # 启用跨域

# 启动时间（用于计算 uptime）
start_time = datetime.now()

# 内存存储 (生产环境应使用数据库)
executions = {}  # {execution_id: {status, result, ...}}


# ========================================
# 标准错误码（符合 ADDP 工作流计算引擎接口规范）
# ========================================

class ErrorCode:
    """标准错误码"""
    OPERATOR_NOT_FOUND = "OPERATOR_NOT_FOUND"      # 算子不存在
    INVALID_PARAMS = "INVALID_PARAMS"              # 参数错误
    EXECUTION_FAILED = "EXECUTION_FAILED"          # 执行失败
    WORKFLOW_INVALID = "WORKFLOW_INVALID"          # 工作流定义无效
    DIRECT_NOT_SUPPORTED = "DIRECT_NOT_SUPPORTED"  # 算子不支持 direct 调用
    EXECUTION_NOT_FOUND = "EXECUTION_NOT_FOUND"    # 执行记录不存在
    INTERNAL_ERROR = "INTERNAL_ERROR"              # 内部错误


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


def serialize_json_value(value):
    """Recursively reduce runtime values to JSON-safe primitives."""
    if isinstance(value, dict):
        return {key: serialize_json_value(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [serialize_json_value(item) for item in value]
    if isinstance(value, bytes):
        return value.hex()
    try:
        json.dumps(value)
        return value
    except (TypeError, ValueError):
        return str(value)


def serialize_workflow_value(value, preview_limit: int = 5):
    """把 Spark 运行时对象转换为可 JSON 化的轻量结果摘要。"""
    if DataFrame is not None and isinstance(value, DataFrame):
        schema = [
            {"name": field.name, "type": field.dataType.simpleString()}
            for field in value.schema.fields
        ]
        rows = serialize_json_value([
            row.asDict(recursive=True)
            for row in value.limit(preview_limit).collect()
        ])
        return {
            "type": "spark_dataframe",
            "schema": schema,
            "preview_rows": rows,
            "preview_limit": preview_limit
        }

    if isinstance(value, dict):
        return {key: serialize_workflow_value(item, preview_limit) for key, item in value.items()}
    if isinstance(value, list):
        return [serialize_workflow_value(item, preview_limit) for item in value]
    return serialize_json_value(value)


def runtime_tenant_id(data):
    """Return the tenant context derived by the owner module."""
    runtime = data.get("runtime")
    if not isinstance(runtime, dict):
        raise ValueError("runtime.tenant_id 必须由调用方提供")
    tenant_id = runtime.get("tenant_id")
    if not isinstance(tenant_id, int) or isinstance(tenant_id, bool) or tenant_id <= 0:
        raise ValueError("runtime.tenant_id 必须是正整数")
    return tenant_id


# ========================================
# 健康检查
# ========================================

@app.route('/health', methods=['GET'])
def health_check():
    """
    健康检查端点（符合 ADDP 工作流计算引擎接口规范）

    Returns:
        {
            "status": "healthy",
            "service": "spark-workflow-engine",
            "version": "1.0.0",
            "uptime": 3600,
            "operators_count": 35,
            "dependencies": {
                "pyspark": "3.5.0",
                "sedona": "1.5.1"
            }
        }
    """
    try:
        from pyspark import __version__ as spark_version
        from importlib.metadata import version as get_version

        uptime = int((datetime.now() - start_time).total_seconds())

        # 获取算子数量
        from operator_metadata import get_operator_metadata
        operators = get_operator_metadata()

        # 获取 Sedona 版本（使用 importlib.metadata）
        try:
            sedona_version = get_version('apache-sedona')
        except Exception:
            sedona_version = "unknown"

        return jsonify({
            "status": "healthy",
            "service": "spark-workflow-engine",
            "version": "1.0.0",
            "uptime": uptime,
            "operators_count": len(operators),
            "dependencies": {
                "pyspark": spark_version,
                "sedona": sedona_version
            }
        }), 200
    except Exception as e:
        logger.error(f"Health check failed: {e}")
        return jsonify({
            "status": "unhealthy",
            "error": str(e)
        }), 500


# ========================================
# 算子列表 (供前端使用)
# ========================================

@app.route('/api/operators', methods=['GET'])
def get_operators():
    """
    获取所有空间算子列表 (Common 模块标准接口)
    供 Develop Backend OperatorDiscoveryService 使用

    Query Parameters:
        category (optional): 按分类过滤

    Response:
        {
            "status": "success",
            "operators": [...],
            "count": 35
        }
    """
    try:
        category = request.args.get('category')

        # 导入算子元数据定义
        from operator_metadata import get_operator_metadata

        operators = get_operator_metadata()

        # 按分类过滤
        if category:
            operators = [op for op in operators if op.get('category') == category]

        return jsonify({
            "status": "success",
            "operators": operators,
            "count": len(operators)
        }), 200

    except Exception as e:
        logger.error(f"Failed to list operators: {e}", exc_info=True)
        return jsonify(error_response(
            ErrorCode.INTERNAL_ERROR,
            f"获取算子列表失败: {str(e)}"
        )), 500


# ========================================
# 即时执行 (供 Develop 使用)
# ========================================

@app.route('/api/workflow', methods=['POST'])
def execute_workflow_endpoint():
    """
    即时执行工作流
    供 Develop 模块即时执行使用

    Request Body:
        {
            "engine_id": 34,           # Spark引擎ID
            "workflow_def": {
                "tasks": [...]
            },
            "input_data": {...}  # 可选
        }

    Response:
        {
            "status": "success",
            "execution_id": "...",
            "message": "...",
            "task_order": [...]
        }
    """
    start = time.time()
    execution_id = None
    try:
        data = request.get_json(silent=True) or {}
        engine_id = data.get('engine_id')
        workflow_def = data.get('workflow_def')
        input_data = data.get('input_data', {})

        if not engine_id:
            response = error_response(
                ErrorCode.INVALID_PARAMS,
                "请求体缺少 'engine_id' 字段"
            )
            response["execution_time_ms"] = (time.time() - start) * 1000
            return jsonify(response), 400

        if not workflow_def:
            response = error_response(
                ErrorCode.INVALID_PARAMS,
                "请求体缺少 'workflow_def' 字段"
            )
            response["execution_time_ms"] = (time.time() - start) * 1000
            return jsonify(response), 400

        from operator_metadata import get_operator_metadata
        operators = {
            op["id"]: op["effects"]
            for op in get_operator_metadata()
            if "workflow" in op.get("execution_modes", [])
        }
        validate_execution_authorization(
            workflow_def,
            operator_effects=operators,
            runtime=data.get("runtime"),
        )
        tenant_id = runtime_tenant_id(data)

        # 执行工作流
        execution_id = str(uuid.uuid4())
        logger.info(f"Executing workflow {execution_id} on Spark engine {engine_id}")

        result = execute_workflow(engine_id, tenant_id, workflow_def, input_data)
        execution_time = (time.time() - start) * 1000
        final_result = serialize_workflow_value(result.get('final_result'))
        all_results = serialize_workflow_value(result.get('all_results', {}))

        # 存储执行记录
        executions[execution_id] = {
            "execution_id": execution_id,
            "engine_id": engine_id,
            "status": result['status'],
            "result": final_result,
            "all_results": all_results,
            "task_order": result.get('task_order'),
            "error": result.get('error'),
            "error_code": result.get('error_code'),
            "details": result.get('details'),
            "message": result.get('message'),
            "started_at": datetime.now().isoformat(),
            "execution_time_ms": execution_time
        }

        response = {
            "status": result['status'],
            "execution_id": execution_id,
            "execution_time_ms": execution_time
        }

        if result['status'] == 'success':
            response['final_result'] = final_result
            response['all_results'] = all_results
            response['message'] = result.get('message')
            response['task_order'] = result.get('task_order')
        else:
            error_code = result.get('error_code')
            if error_code == ErrorCode.WORKFLOW_INVALID:
                response.update(error_response(
                    ErrorCode.WORKFLOW_INVALID,
                    result.get('error', '工作流定义无效')
                ))
                return jsonify(response), 400
            response.update(error_response(
                ErrorCode.EXECUTION_FAILED,
                result.get('error', '工作流执行失败')
            ))

        status_code = 200 if result['status'] == 'success' else 500
        return jsonify(response), status_code

    except Exception as e:
        execution_time = (time.time() - start) * 1000 if 'start' in locals() else 0
        logger.error(f"Workflow execution failed: {e}", exc_info=True)
        if execution_id:
            executions[execution_id] = {
                "execution_id": execution_id,
                "engine_id": data.get('engine_id') if 'data' in locals() else None,
                "status": "failed",
                "result": None,
                "all_results": None,
                "task_order": None,
                "error": f"工作流执行失败: {str(e)}",
                "error_code": ErrorCode.EXECUTION_FAILED,
                "details": str(e),
                "message": "工作流执行失败",
                "started_at": datetime.now().isoformat(),
                "execution_time_ms": execution_time
            }
        response = error_response(
            ErrorCode.EXECUTION_FAILED,
            f"工作流执行失败: {str(e)}"
        )
        if execution_id:
            response["execution_id"] = execution_id
        response["execution_time_ms"] = execution_time
        return jsonify(response), 500


@app.route('/api/operators/<operator_name>/invoke', methods=['POST'])
def invoke_operator_endpoint(operator_name):
    """
    direct 调用单个算子。

    Spark Workflow direct 调用仍必须携带顶层 engine_id，该 ID 指向真实
    engine_type=spark 的通用引擎资源；当前内置 Spark 算子默认只支持 workflow。

    Request Body:
        {
            "engine_id": 34,
            "params": {...}
        }

    Response:
        {
            "status": "success",
            "result": ...
        }
    """
    start = time.time()
    try:
        if operator_name not in OPERATOR_REGISTRY:
            response = error_response(
                ErrorCode.OPERATOR_NOT_FOUND,
                f"算子 '{operator_name}' 不存在",
                details=f"可用算子: {', '.join(OPERATOR_REGISTRY.keys())}"
            )
            response["execution_time_ms"] = (time.time() - start) * 1000
            return jsonify(response), 404

        from operator_metadata import get_operator_metadata
        operator_meta = next(
            (op for op in get_operator_metadata() if op.get('name') == operator_name),
            None,
        )
        execution_modes = operator_meta['execution_modes']
        if 'direct' not in execution_modes:
            response = error_response(
                ErrorCode.DIRECT_NOT_SUPPORTED,
                f"算子 '{operator_name}' 不支持 direct 调用"
            )
            response["execution_time_ms"] = (time.time() - start) * 1000
            return jsonify(response), 403

        data = request.get_json(silent=True) or {}
        engine_id = data.get('engine_id')
        params = data.get('params', {})
        tenant_id = runtime_tenant_id(data)

        if not engine_id:
            response = error_response(
                ErrorCode.INVALID_PARAMS,
                "请求体缺少 'engine_id' 字段"
            )
            response["execution_time_ms"] = (time.time() - start) * 1000
            return jsonify(response), 400

        logger.info(f"Invoking operator {operator_name} directly on Spark engine {engine_id}")

        result = execute_single_operator(engine_id, tenant_id, operator_name, params)
        execution_time = (time.time() - start) * 1000

        if result['status'] == 'success':
            result.setdefault("execution_time_ms", execution_time)
            return jsonify(result), 200

        response = error_response(
            ErrorCode.EXECUTION_FAILED,
            result.get('error', '算子执行失败')
        )
        response["execution_time_ms"] = execution_time
        return jsonify(response), 500

    except Exception as e:
        execution_time = (time.time() - start) * 1000
        logger.error(f"Operator execution failed: {e}", exc_info=True)
        response = error_response(
            ErrorCode.EXECUTION_FAILED,
            f"算子执行失败: {str(e)}"
        )
        response["execution_time_ms"] = execution_time
        return jsonify(response), 500


# ========================================
# 执行状态查询
# ========================================

@app.route('/api/executions/<execution_id>', methods=['GET'])
def get_execution_status(execution_id):
    """
    查询执行状态
    供 Orchestrator 轮询使用

    Response:
        {
            "status": "success",
            "execution_id": "...",
            "message": "...",
            "task_order": [...],
            "progress": 100
        }
    """
    if execution_id not in executions:
        return jsonify(error_response(
            ErrorCode.EXECUTION_NOT_FOUND,
            "Execution not found"
        )), 404

    execution = executions[execution_id]

    return jsonify({
        "status": execution['status'],
        "execution_id": execution_id,
        "result": execution.get('result'),
        "all_results": execution.get('all_results'),
        "message": execution.get('message'),
        "task_order": execution.get('task_order'),
        "error": execution.get('error'),
        "error_code": execution.get('error_code'),
        "details": execution.get('details'),
        "progress": 100 if execution['status'] in ['success', 'failed'] else 50,
        "started_at": execution.get('started_at'),
        "execution_time_ms": execution.get('execution_time_ms')
    }), 200


# ========================================
# System 注册 (自动注册到 System Backend)
# ========================================

def register_to_system():
    """
    向 System Backend 自注册（创建或更新引擎记录）
    """
    from addp_common.client import register_runtime_engine

    system_url = os.getenv('SYSTEM_URL', 'http://localhost:8180')
    client_secret = os.getenv('SPARK_WORKFLOW_SERVICE_CLIENT_SECRET', '')

    # 读取自身配置
    port = int(os.getenv('PORT', 8098))
    protocol = os.getenv('PROTOCOL', 'http')
    runtime_host = os.getenv('RUNTIME_HOST', 'localhost').strip()
    connection_info = {"protocol": protocol, "port": port}
    if runtime_host:
        connection_info["host"] = runtime_host

    # 构建注册请求
    payload = {
        "engine_type": "spark_workflow",
        "name": "Spark 工作流引擎",
        "description": "基于 Apache Spark 的分布式工作流执行引擎",
        "connection_info": connection_info,
        "capabilities": {
            "schema_version": "engine.capabilities/v1",
            "engine_type": "spark_workflow",
            "engine_family": "workflow",
            "compute": {
                "workflow": {
                    "supported": True,
                    "runtime_api": "addp.workflow/v1",
                    "dynamic_operators": True
                }
            }
        },
        "is_builtin": True  # 内置引擎，对所有租户可见
    }

    try:
        status_code, body = register_runtime_engine(
            system_url, "addp-spark", client_secret, payload
        )
        if status_code == 202:
            logger.info("✅ Successfully registered to System Backend")
            return True
        else:
            logger.warning(f"⚠️  Failed to register: {status_code} - {body}")
            return False

    except Exception as e:
        logger.warning(f"⚠️  Failed to register to System: {e}")
        return False


def register_to_system_with_retry():
    from addp_common.client import retry_runtime_registration

    retry_runtime_registration(register_to_system, "spark_workflow", logger)


# ========================================
# 主入口
# ========================================

if __name__ == '__main__':
    # 启动后台线程注册到 System (不阻塞应用启动)
    import threading
    registration_thread = threading.Thread(target=register_to_system_with_retry, daemon=True)
    registration_thread.start()

    # 启动 Flask 服务
    port = int(os.getenv('PORT', 8098))
    logger.info(f"🚀 Starting Spark 工作流引擎 on port {port}")
    app.run(host='0.0.0.0', port=port, debug=False)
