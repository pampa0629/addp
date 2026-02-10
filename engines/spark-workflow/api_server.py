"""
Spark 工作流引擎 Flask API Server
提供 REST API 接口供 Develop 和 Orchestrator 调用
"""

from flask import Flask, request, jsonify
from flask_cors import CORS
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
from operators import list_operators

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
    try:
        data = request.get_json()
        engine_id = data.get('engine_id')
        workflow_def = data.get('workflow_def')
        input_data = data.get('input_data', {})

        if not engine_id:
            return jsonify({
                "status": "failed",
                "error": "engine_id is required"
            }), 400

        if not workflow_def:
            return jsonify({
                "status": "failed",
                "error": "workflow_def is required"
            }), 400

        # 执行工作流
        execution_id = str(uuid.uuid4())
        logger.info(f"Executing workflow {execution_id} on Spark engine {engine_id}")

        result = execute_workflow(engine_id, workflow_def, input_data)

        # 存储执行记录
        executions[execution_id] = {
            "execution_id": execution_id,
            "engine_id": engine_id,
            "status": result['status'],
            "result": result.get('final_result'),
            "task_order": result.get('task_order'),
            "error": result.get('error'),
            "message": result.get('message'),
            "started_at": datetime.now().isoformat()
        }

        response = {
            "status": result['status'],
            "execution_id": execution_id
        }

        if result['status'] == 'success':
            response['message'] = result.get('message')
            response['task_order'] = result.get('task_order')
        else:
            response['error'] = result['error']

        status_code = 200 if result['status'] == 'success' else 500
        return jsonify(response), status_code

    except Exception as e:
        logger.error(f"Workflow execution failed: {e}", exc_info=True)
        return jsonify({
            "status": "failed",
            "error": str(e)
        }), 500


@app.route('/api/operators/<operator_name>/execute', methods=['POST'])
def execute_operator_endpoint(operator_name):
    """
    执行单个算子
    供 Develop 模块快速测试使用

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
    try:
        data = request.get_json()
        engine_id = data.get('engine_id')
        params = data.get('params', {})

        if not engine_id:
            return jsonify({
                "status": "failed",
                "error": "engine_id is required"
            }), 400

        logger.info(f"Executing operator {operator_name} on Spark engine {engine_id}")

        result = execute_single_operator(engine_id, operator_name, params)

        status_code = 200 if result['status'] == 'success' else 500
        return jsonify(result), status_code

    except Exception as e:
        logger.error(f"Operator execution failed: {e}", exc_info=True)
        return jsonify({
            "status": "failed",
            "error": str(e)
        }), 500


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
        return jsonify({
            "status": "failed",
            "error": "Execution not found"
        }), 404

    execution = executions[execution_id]

    return jsonify({
        "status": execution['status'],
        "execution_id": execution_id,
        "message": execution.get('message'),
        "task_order": execution.get('task_order'),
        "error": execution.get('error'),
        "progress": 100 if execution['status'] in ['success', 'failed'] else 50,
        "started_at": execution.get('started_at')
    }), 200


# ========================================
# System 注册 (自动注册到 System Backend)
# ========================================

def register_to_system():
    """
    向 System Backend 自注册（创建或更新引擎记录）
    """
    import requests
    import json

    system_url = os.getenv('SYSTEM_SERVICE_URL', 'http://localhost:8180')
    api_key = os.getenv('INTERNAL_API_KEY', '')

    # 读取自身配置
    port = int(os.getenv('PORT', 8098))
    protocol = os.getenv('PROTOCOL', 'http')

    # 生成 capabilities
    capabilities = {
        "compute": [{
            "dev_modes": ["workflow"],
            "api_endpoints": {
                "operators": "/api/operators",
                "execute": "/api/operators/:name/execute",
                "workflow": "/api/workflow",
                "executions": "/api/executions/:id"
            },
            "engine": "spark",
            "scale": "distributed",
            "features": ["big_data", "distributed"],
            "description": "分布式空间分析"
        }]
    }

    # 构建注册请求
    payload = {
        "engine_type": "spark_workflow",
        "name": "Spark 工作流引擎",
        "description": "基于 Apache Spark 的分布式工作流执行引擎",
        "connection_info": {
            "protocol": protocol,
            "port": port
            # host 由 System 自动填充
        },
        "capabilities": json.dumps(capabilities),
        "is_builtin": True  # 内置引擎，对所有租户可见
    }

    headers = {
        "Content-Type": "application/json",
        "X-Internal-API-Key": api_key
    }

    try:
        # 禁用代理，直接连接到 System Backend（避免系统代理干扰）
        proxies = {
            'http': None,
            'https': None
        }

        response = requests.post(
            f"{system_url}/api/internal/engines/register",
            json=payload,
            headers=headers,
            proxies=proxies,
            timeout=10
        )

        if response.status_code == 202:
            result = response.json()
            engine_id = result.get('engine_id')
            logger.info(f"✅ Successfully registered to System Backend (Engine ID: {engine_id})")
            return True
        else:
            logger.warning(f"⚠️  Failed to register: {response.status_code} - {response.text}")
            return False

    except Exception as e:
        logger.warning(f"⚠️  Failed to register to System: {e}")
        return False


def register_to_system_with_retry():
    """
    后台线程定期重试注册 (最多5次,间隔10秒)
    """
    import threading
    import time

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
