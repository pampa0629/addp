"""
Math Workflow Engine - API 服务

实现 ADDP 工作流计算引擎的 5 个标准接口：
1. GET /health - 健康检查
2. GET /api/operators - 获取算子列表
3. POST /api/spatial/workflow - 执行工作流（DAG）
4. POST /api/spatial/operators/<name>/execute - 执行单个算子
5. GET /api/spatial/executions/<execution_id> - 查询执行状态（可选）
"""

from flask import Flask, request, jsonify
from flask_cors import CORS
import uuid
import logging
import time
import os
import threading
import requests
from datetime import datetime

from operators import list_operators, get_operator_function, OPERATORS
from workflow_engine import MathWorkflowEngine, WorkflowInvalidError

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

# ============ 错误码定义 ============

class ErrorCode:
    """标准错误码"""
    OPERATOR_NOT_FOUND = "OPERATOR_NOT_FOUND"
    INVALID_PARAMS = "INVALID_PARAMS"
    EXECUTION_FAILED = "EXECUTION_FAILED"
    WORKFLOW_INVALID = "WORKFLOW_INVALID"
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


@app.route('/api/spatial/workflow', methods=['POST'])
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
    try:
        data = request.get_json()

        if not data or 'workflow_def' not in data:
            return jsonify(error_response(
                ErrorCode.INVALID_PARAMS,
                "请求体缺少 'workflow_def' 字段"
            )), 400

        workflow_def = data['workflow_def']
        input_data = data.get('input_data', {})
        execution_id = str(uuid.uuid4())

        logger.info(f"开始执行工作流: execution_id={execution_id}")
        start = time.time()

        # 创建引擎并执行
        engine = MathWorkflowEngine()
        engine.load_workflow(workflow_def)
        results = engine.execute(input_data)

        # 最后一个任务的结果作为最终结果
        final_task_id = list(results.keys())[-1] if results else None
        final_result = results[final_task_id] if final_task_id else None

        execution_time = (time.time() - start) * 1000

        logger.info(f"工作流执行成功: execution_id={execution_id}, 耗时={execution_time:.2f}ms")

        return jsonify({
            "status": "success",
            "execution_id": execution_id,
            "final_result": final_result,
            "all_results": results,
            "execution_time_ms": execution_time
        }), 200

    except WorkflowInvalidError as e:
        logger.error(f"工作流定义无效: {e}")
        return jsonify(error_response(
            ErrorCode.WORKFLOW_INVALID,
            str(e)
        )), 400

    except Exception as e:
        logger.exception(f"工作流执行失败: {e}")
        return jsonify(error_response(
            ErrorCode.EXECUTION_FAILED,
            f"工作流执行失败: {str(e)}"
        )), 500


@app.route('/api/spatial/operators/<name>/execute', methods=['POST'])
def execute_operator(name):
    """
    执行单个算子

    Path Parameters:
        name: 算子名称

    Request Body:
        {
            "params": {"a": 5, "b": 3}
        }

    Returns:
        {
            "status": "success",
            "execution_id": "uuid-5678",
            "result": 8
        }
    """
    try:
        # 检查算子是否存在
        if name not in OPERATORS:
            return jsonify(error_response(
                ErrorCode.OPERATOR_NOT_FOUND,
                f"算子 '{name}' 不存在",
                details=f"可用算子: {', '.join(OPERATORS.keys())}"
            )), 404

        data = request.get_json()

        if not data or 'params' not in data:
            return jsonify(error_response(
                ErrorCode.INVALID_PARAMS,
                "请求体缺少 'params' 字段"
            )), 400

        params = data['params']
        execution_id = str(uuid.uuid4())

        logger.info(f"执行算子 '{name}': execution_id={execution_id}, params={params}")
        start = time.time()

        # 获取算子函数并执行
        operator_func = get_operator_function(name)
        result = operator_func(**params)

        execution_time = (time.time() - start) * 1000

        logger.info(f"算子执行成功: '{name}', result={result}, 耗时={execution_time:.2f}ms")

        return jsonify({
            "status": "success",
            "execution_id": execution_id,
            "result": result,
            "execution_time_ms": execution_time
        }), 200

    except TypeError as e:
        logger.error(f"参数错误: {e}")
        return jsonify(error_response(
            ErrorCode.INVALID_PARAMS,
            f"参数错误: {str(e)}"
        )), 400

    except Exception as e:
        logger.exception(f"算子执行失败: {e}")
        return jsonify(error_response(
            ErrorCode.EXECUTION_FAILED,
            f"算子执行失败: {str(e)}"
        )), 500


@app.route('/api/spatial/executions/<execution_id>', methods=['GET'])
def get_execution_status(execution_id):
    """
    查询执行状态（可选接口，简化版直接返回已完成）

    当前简化版所有执行都是同步的，因此此接口返回固定的"已完成"状态。
    生产环境中应实现异步任务队列（如 Celery）。

    Returns:
        {
            "status": "success",
            "execution_id": "uuid-1234",
            "task_status": "completed",
            "result": null,
            "message": "当前版本所有任务同步执行，请直接使用执行接口的响应结果"
        }
    """
    return jsonify({
        "status": "success",
        "execution_id": execution_id,
        "task_status": "completed",
        "result": None,
        "message": "当前版本所有任务同步执行，请直接使用执行接口的响应结果"
    }), 200


# ============ 自动注册到 System Backend ============

def register_to_system():
    """引擎启动时自动注册到 System Backend"""
    import json

    time.sleep(5)  # 等待引擎完全启动

    system_url = os.getenv('SYSTEM_SERVICE_URL', 'http://localhost:8080')
    internal_api_key = os.getenv('INTERNAL_API_KEY', '')
    port = int(os.getenv('PORT', 8097))
    protocol = os.getenv('PROTOCOL', 'http')

    # 构建引擎 URL（host 将由 System 自动填充）
    engine_url = f"{protocol}://localhost:{port}"

    registration_data = {
        "name": "Math Workflow 计算引擎",
        "engine_type": "math_workflow",
        "is_builtin": True,
        "description": "基于 Python 的数学计算工作流引擎，支持基本数学运算",
        "capabilities": {
            "compute": [{
                "dev_modes": ["workflow"],
                "features": ["math_operations", "workflow_execution"],
                "description": "支持加减乘除等基本数学运算的工作流执行"
            }]
        },
        "connection_info": {
            "protocol": protocol,
            "host": "localhost",
            "port": port,
            "api_url": engine_url
        }
    }

    # 禁用代理，直接连接到 System Backend（避免系统代理干扰）
    proxies = {
        'http': None,
        'https': None
    }

    try:
        logger.info(f"📤 尝试注册到 System Backend: {system_url}")
        logger.info(f"📦 Payload: {json.dumps(registration_data, ensure_ascii=False)}")
        logger.info(f"🔑 API Key length: {len(internal_api_key)}")

        response = requests.post(
            f"{system_url}/internal/registry/capabilities",
            json=registration_data,
            headers={"X-Internal-API-Key": internal_api_key},
            proxies=proxies,
            timeout=10
        )

        logger.info(f"📥 收到响应: status={response.status_code}, body={response.text[:500]}")

        if response.status_code == 200:
            logger.info("✅ Math Workflow Engine 注册成功")
            return True
        else:
            logger.warning(f"⚠️  注册失败: HTTP {response.status_code}, {response.text}")
            return False

    except requests.exceptions.RequestException as e:
        logger.error(f"❌ 网络请求异常: {type(e).__name__}: {e}")
        return False
    except Exception as e:
        logger.error(f"❌ 注册异常: {e}")
        import traceback
        logger.error(f"详细堆栈:\n{traceback.format_exc()}")
        return False


def register_to_system_with_retry():
    """后台线程定期重试注册（最多5次，间隔10秒）"""
    max_retries = 5
    retry_interval = 10

    for attempt in range(1, max_retries + 1):
        logger.info(f"🔄 Attempting to register to System (attempt {attempt}/{max_retries})")

        if register_to_system():
            logger.info(f"✅ Registration successful on attempt {attempt}")
            return

        if attempt < max_retries:
            logger.info(f"⏳ Waiting {retry_interval}s before retry...")
            time.sleep(retry_interval)

    logger.error(f"❌ Registration failed after {max_retries} attempts")



# ============ 应用启动 ============

if __name__ == '__main__':
    # 后台线程自动注册
    threading.Thread(target=register_to_system, daemon=True).start()

    port = int(os.getenv('PORT', 8097))
    logger.info(f"🚀 Math Workflow Engine 启动: http://0.0.0.0:{port}")
    logger.info(f"   算子数量: {len(OPERATORS)}")
    logger.info(f"   OpenAPI 文档: ../docs/workflow-engine-api-v1.yaml")

    app.run(host='0.0.0.0', port=port, debug=False)
