"""
Python Workflow Engine Flask API Server
Python 数据处理工作流引擎 API 服务
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
LOG_LEVEL = os.getenv('LOG_LEVEL', 'INFO').upper()
logging.basicConfig(
    level=getattr(logging, LOG_LEVEL),
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

app = Flask(__name__)
CORS(app)  # 启用跨域

# 启动时间（用于计算 uptime）
start_time = datetime.now()

# 内存存储（生产环境应使用数据库）
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
            "service": "python-workflow-engine",
            "version": "1.0.0",
            "uptime": 3600,
            "operators_count": 42,
            "dependencies": {
                "geopandas": "0.14.1",
                "pandas": "2.1.4"
            }
        }
    """
    try:
        import geopandas as gpd
        import pandas as pd

        uptime = int((datetime.now() - start_time).total_seconds())
        operators = list_operators()

        return jsonify({
            "status": "healthy",
            "service": "python-workflow-engine",
            "version": "1.0.0",
            "uptime": uptime,
            "operators_count": len(operators),
            "dependencies": {
                "geopandas": gpd.__version__,
                "pandas": pd.__version__
            }
        }), 200
    except Exception as e:
        logger.error(f"Health check failed: {e}")
        return jsonify({
            "status": "unhealthy",
            "error": str(e)
        }), 500


# ========================================
# 算子列表（供前端使用）
# ========================================

@app.route('/api/operators', methods=['GET'])
def get_operators():
    """
    获取所有空间算子列表 (Common 模块标准接口)
    供 Develop Backend OperatorDiscoveryService 使用

    Query Parameters:
        category (optional): 按分类过滤

    Returns:
        {
            "status": "success",
            "operators": [...],
            "count": 42
        }
    """
    try:
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
    except Exception as e:
        logger.error(f"Failed to list operators: {e}", exc_info=True)
        return jsonify(error_response(
            ErrorCode.INTERNAL_ERROR,
            f"获取算子列表失败: {str(e)}"
        )), 500


# ========================================
# 即时执行（供 Develop 使用）
# ========================================

@app.route('/api/spatial/workflow', methods=['POST'])
def execute_workflow_endpoint():
    """
    即时执行工作流
    供 Develop 模块即时执行使用

    Request Body:
        {
            "workflow_def": {
                "tasks": [...]
            },
            "input_data": {...}  // 可选
        }

    Response:
        {
            "status": "success",
            "execution_id": "...",
            "final_result": "...",  // GeoJSON
            "all_results": {...},
            "execution_time_ms": 123.45
        }
    """
    start = time.time()

    try:
        data = request.get_json()
        workflow_def = data.get('workflow_def')
        input_data = data.get('input_data', {})

        if not workflow_def:
            return jsonify(error_response(
                ErrorCode.INVALID_PARAMS,
                "请求体缺少 'workflow_def' 字段"
            )), 400

        # 执行工作流
        execution_id = str(uuid.uuid4())
        logger.info(f"Executing workflow {execution_id}")

        result = execute_workflow(workflow_def, input_data)

        execution_time = (time.time() - start) * 1000

        # 存储执行记录
        executions[execution_id] = {
            "execution_id": execution_id,
            "status": result['status'],
            "result": result.get('final_result'),
            "all_results": result.get('all_results'),
            "error": result.get('error'),
            "started_at": datetime.now().isoformat(),
            "execution_time_ms": execution_time
        }

        response = {
            "status": result['status'],
            "execution_id": execution_id,
            "execution_time_ms": execution_time
        }

        if result['status'] == 'success':
            response['final_result'] = result['final_result']
            response['all_results'] = result['all_results']
            logger.info(f"Workflow {execution_id} completed successfully in {execution_time:.2f}ms")
            return jsonify(response), 200
        else:
            response.update(error_response(
                ErrorCode.EXECUTION_FAILED,
                result.get('error', '工作流执行失败')
            ))
            return jsonify(response), 500

    except ValueError as e:
        execution_time = (time.time() - start) * 1000
        logger.error(f"Workflow validation failed: {e}")
        response = error_response(
            ErrorCode.WORKFLOW_INVALID,
            f"工作流定义无效: {str(e)}"
        )
        response["execution_time_ms"] = execution_time
        return jsonify(response), 400

    except Exception as e:
        execution_time = (time.time() - start) * 1000
        logger.error(f"Workflow execution failed: {e}", exc_info=True)
        response = error_response(
            ErrorCode.EXECUTION_FAILED,
            f"工作流执行失败: {str(e)}"
        )
        response["execution_time_ms"] = execution_time
        return jsonify(response), 500


@app.route('/api/spatial/operators/<operator_name>/execute', methods=['POST'])
def execute_operator_endpoint(operator_name):
    """
    执行单个算子
    供 Develop 模块快速测试使用

    Path Parameters:
        operator_name: 算子名称

    Request Body:
        {
            "params": {...}
        }

    Response:
        {
            "status": "success",
            "execution_id": "uuid-...",
            "result": "...",  // GeoJSON
            "execution_time_ms": 12.34
        }
    """
    start = time.time()

    try:
        # 检查算子是否存在
        operators = list_operators()
        operator_names = [op['name'] for op in operators]

        if operator_name not in operator_names:
            execution_time = (time.time() - start) * 1000
            response = error_response(
                ErrorCode.OPERATOR_NOT_FOUND,
                f"算子 '{operator_name}' 不存在",
                details=f"可用算子: {', '.join(operator_names[:10])}..."
            )
            response["execution_time_ms"] = execution_time
            return jsonify(response), 404

        data = request.get_json()

        if not data or 'params' not in data:
            execution_time = (time.time() - start) * 1000
            response = error_response(
                ErrorCode.INVALID_PARAMS,
                "请求体缺少 'params' 字段"
            )
            response["execution_time_ms"] = execution_time
            return jsonify(response), 400

        params = data['params']
        execution_id = str(uuid.uuid4())

        logger.info(f"Executing operator {operator_name}, execution_id={execution_id}")

        result = execute_single_operator(operator_name, params)

        execution_time = (time.time() - start) * 1000

        if result['status'] == 'success':
            response = {
                "status": "success",
                "execution_id": execution_id,
                "result": result['result'],
                "execution_time_ms": execution_time
            }
            logger.info(f"Operator {operator_name} completed successfully in {execution_time:.2f}ms")
            return jsonify(response), 200
        else:
            response = error_response(
                ErrorCode.EXECUTION_FAILED,
                result.get('error', '算子执行失败')
            )
            response["execution_id"] = execution_id
            response["execution_time_ms"] = execution_time
            return jsonify(response), 500

    except TypeError as e:
        execution_time = (time.time() - start) * 1000
        logger.error(f"Parameter error: {e}")
        response = error_response(
            ErrorCode.INVALID_PARAMS,
            f"参数错误: {str(e)}"
        )
        response["execution_time_ms"] = execution_time
        return jsonify(response), 400

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
# 任务管理（供 Orchestrator 使用）
# ========================================

# 注意：这里的"任务"指的是保存到 develop.spatial_tasks 的 GIS 任务定义
# 实际存储在 Develop Backend 的 PostgreSQL 中
# 这里提供的 API 只是占位符，实际查询会通过 Develop Backend 转发

@app.route('/api/spatial/tasks', methods=['GET'])
def list_tasks():
    """
    列出所有 GIS 任务
    供 Orchestrator 动态发现任务使用

    Query Params:
        - tenant_id: 租户ID
        - page: 页码
        - page_size: 每页数量

    Response:
        {
            "status": "success",
            "tasks": [
                {
                    "id": 1,
                    "name": "北京 POI 缓冲区分析",
                    "description": "...",
                    "workflow_def": {...},
                    "input_schema": {...}
                }
            ],
            "total": 10,
            "page": 1,
            "page_size": 20
        }
    """
    # 注意：实际实现应该查询 develop.spatial_tasks 表
    # 这里返回空列表作为占位符
    tenant_id = request.args.get('tenant_id', type=int)
    page = request.args.get('page', 1, type=int)
    page_size = request.args.get('page_size', 20, type=int)

    logger.info(f"Listing tasks for tenant {tenant_id}, page {page}")

    return jsonify({
        "status": "success",
        "tasks": [],  # 实际应查询数据库
        "total": 0,
        "page": page,
        "page_size": page_size,
        "message": "Task list should be queried from develop.spatial_tasks via Develop Backend"
    }), 200


@app.route('/api/spatial/tasks', methods=['POST'])
def create_task():
    """
    创建 GIS 任务
    供 Develop 模块"保存为任务"功能使用

    Request Body:
        {
            "name": "...",
            "description": "...",
            "workflow_def": {...},
            "input_schema": {...},
            "schedule": "0 2 * * *"  // 可选
        }

    Response:
        {
            "status": "success",
            "task_id": 1
        }
    """
    try:
        data = request.get_json()

        # 验证必填字段
        if not data.get('name') or not data.get('workflow_def'):
            return jsonify({
                "status": "failed",
                "error": "name and workflow_def are required"
            }), 400

        logger.info(f"Creating task: {data.get('name')}")

        # 注意：实际实现应该写入 develop.spatial_tasks 表
        # 这里返回占位符响应
        return jsonify({
            "status": "success",
            "task_id": 1,  # 实际应返回数据库生成的ID
            "message": "Task should be saved to develop.spatial_tasks via Develop Backend"
        }), 201

    except Exception as e:
        logger.error(f"Task creation failed: {e}", exc_info=True)
        return jsonify({
            "status": "failed",
            "error": str(e)
        }), 500


@app.route('/api/spatial/tasks/<int:task_id>/execute', methods=['POST'])
def execute_task(task_id):
    """
    执行 GIS 任务
    供 Orchestrator 执行已保存的任务使用

    Request Body:
        {
            "inputs": {
                "poi_location": {...},
                "buffer_distance": 0.001
            }
        }

    Response:
        {
            "status": "success",
            "execution_id": "..."
        }
    """
    try:
        data = request.get_json()
        inputs = data.get('inputs', {})

        logger.info(f"Executing task {task_id} with inputs")

        # 注意：实际实现应该：
        # 1. 从 develop.spatial_tasks 查询任务定义
        # 2. 使用 inputs 填充 workflow_def 中的参数模板
        # 3. 调用 execute_workflow
        # 4. 将结果写入 develop.spatial_execution_results（PostGIS 空间表）

        execution_id = str(uuid.uuid4())

        return jsonify({
            "status": "success",
            "execution_id": execution_id,
            "message": "Task execution should query develop.spatial_tasks and save to PostGIS"
        }), 200

    except Exception as e:
        logger.error(f"Task execution failed: {e}", exc_info=True)
        return jsonify({
            "status": "failed",
            "error": str(e)
        }), 500


# ========================================
# 执行状态查询
# ========================================

@app.route('/api/spatial/executions/<execution_id>', methods=['GET'])
def get_execution_status(execution_id):
    """
    查询执行状态
    供 Orchestrator 轮询使用

    Response:
        {
            "status": "success",
            "execution_id": "...",
            "result": "...",  // GeoJSON
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
        "result": execution.get('result'),
        "error": execution.get('error'),
        "progress": 100 if execution['status'] in ['success', 'failed'] else 50,
        "started_at": execution.get('started_at')
    }), 200


# ========================================
# System 注册（自动注册到 System Backend）
# ========================================

def register_to_system():
    """
    向 System Backend 自注册（创建或更新引擎记录）
    """
    import requests
    import json

    system_url = os.getenv('SYSTEM_SERVICE_URL', 'http://localhost:8080')
    api_key = os.getenv('INTERNAL_API_KEY', '')

    # 读取自身配置
    port = int(os.getenv('PORT', 8099))
    protocol = os.getenv('PROTOCOL', 'http')

    # 生成 capabilities
    capabilities = {
        "compute": [{
            "dev_modes": ["workflow"],
            "supported_formats": ["geojson", "wkt", "csv", "parquet"],
            "features": ["dag", "memory_efficient", "batch", "pandas", "numpy", "scipy"],
            "description": "Python数据处理（Pandas, GeoPandas, NumPy, SciPy）"
        }]
    }

    # 构建注册请求
    payload = {
        "engine_type": "python_workflow",
        "name": "Python 工作流引擎",
        "description": "基于 Python 的工作流执行引擎，支持 GeoPandas、Pandas、NumPy 等数据处理库",
        "connection_info": {
            "protocol": protocol,
            "port": port
            # host 由 System 自动填充
        },
        "capabilities": json.dumps(capabilities)
    }

    headers = {
        "Content-Type": "application/json",
        "X-Internal-API-Key": api_key
    }

    try:
        logger.info(f"📤 发送注册请求到: {system_url}/internal/engines/register")
        logger.info(f"📦 Payload: {payload}")
        logger.info(f"🔑 Headers: Content-Type={headers['Content-Type']}, API Key length={len(api_key)}")

        # 禁用代理，直接连接到 System Backend（避免系统代理干扰）
        proxies = {
            'http': None,
            'https': None
        }

        response = requests.post(
            f"{system_url}/internal/engines/register",
            json=payload,
            headers=headers,
            proxies=proxies,
            timeout=10
        )

        logger.info(f"📥 收到响应: status={response.status_code}, body={response.text[:500]}")

        if response.status_code == 202:
            result = response.json()
            engine_id = result.get('engine_id')
            logger.info(f"✅ Successfully registered to System Backend (Engine ID: {engine_id})")
            return True
        else:
            logger.warning(f"⚠️  Failed to register: {response.status_code} - {response.text}")
            return False

    except requests.exceptions.RequestException as e:
        logger.error(f"❌ 网络请求异常: {type(e).__name__}: {e}")
        import traceback
        logger.error(f"详细堆栈:\n{traceback.format_exc()}")
        return False
    except Exception as e:
        logger.error(f"❌ 注册异常: {type(e).__name__}: {e}")
        import traceback
        logger.error(f"详细堆栈:\n{traceback.format_exc()}")
        return False


def register_to_system_with_retry():
    """
    后台线程定期重试注册（最多5次，间隔10秒）
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
    # 启动后台线程注册到 System（不阻塞应用启动）
    import threading
    registration_thread = threading.Thread(target=register_to_system_with_retry, daemon=True)
    registration_thread.start()

    # 启动 Flask 服务
    port = int(os.getenv('PORT', 8099))
    logger.info(f"🚀 Starting Python Workflow Engine on port {port}")
    app.run(host='0.0.0.0', port=port, debug=False)
