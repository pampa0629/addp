"""
GeoPandas Engine Flask API Server
提供 REST API 接口供 Develop 和 Orchestrator 调用
"""

from flask import Flask, request, jsonify
from flask_cors import CORS
import logging
import uuid
from datetime import datetime
import os

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

# 内存存储（生产环境应使用数据库）
executions = {}  # {execution_id: {status, result, ...}}


# ========================================
# 健康检查
# ========================================

@app.route('/health', methods=['GET'])
def health_check():
    """健康检查端点"""
    return jsonify({
        "status": "healthy",
        "service": "geopandas-engine",
        "version": "1.0.0"
    }), 200


# ========================================
# 算子列表（供前端使用）
# ========================================

@app.route('/api/spatial/operators', methods=['GET'])
def get_operators():
    """
    获取所有空间算子列表
    供 Develop Frontend 使用
    """
    try:
        operators = list_operators()
        return jsonify({
            "status": "success",
            "operators": operators,
            "count": len(operators)
        }), 200
    except Exception as e:
        logger.error(f"Failed to list operators: {e}")
        return jsonify({
            "status": "failed",
            "error": str(e)
        }), 500


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
            "all_results": {...}
        }
    """
    try:
        data = request.get_json()
        workflow_def = data.get('workflow_def')
        input_data = data.get('input_data', {})

        if not workflow_def:
            return jsonify({
                "status": "failed",
                "error": "workflow_def is required"
            }), 400

        # 执行工作流
        execution_id = str(uuid.uuid4())
        logger.info(f"Executing workflow {execution_id}")

        result = execute_workflow(workflow_def, input_data)

        # 存储执行记录
        executions[execution_id] = {
            "execution_id": execution_id,
            "status": result['status'],
            "result": result.get('final_result'),
            "all_results": result.get('all_results'),
            "error": result.get('error'),
            "started_at": datetime.now().isoformat()
        }

        response = {
            "status": result['status'],
            "execution_id": execution_id
        }

        if result['status'] == 'success':
            response['final_result'] = result['final_result']
            response['all_results'] = result['all_results']
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


@app.route('/api/spatial/operators/<operator_name>/execute', methods=['POST'])
def execute_operator_endpoint(operator_name):
    """
    执行单个算子
    供 Develop 模块快速测试使用

    Request Body:
        {
            "params": {...}
        }

    Response:
        {
            "status": "success",
            "result": "..."  // GeoJSON
        }
    """
    try:
        data = request.get_json()
        params = data.get('params', {})

        logger.info(f"Executing operator {operator_name}")

        result = execute_single_operator(operator_name, params)

        status_code = 200 if result['status'] == 'success' else 500
        return jsonify(result), status_code

    except Exception as e:
        logger.error(f"Operator execution failed: {e}", exc_info=True)
        return jsonify({
            "status": "failed",
            "error": str(e)
        }), 500


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
    启动时自动注册到 System Backend（单次尝试）
    """
    import requests

    system_url = os.getenv('SYSTEM_SERVICE_URL', 'http://localhost:8080')
    internal_api_key = os.getenv('INTERNAL_API_KEY', '')

    registration_data = {
        "unique_identifier": "api.geopandas",
        "name": "geopandas_engine",
        "display_name": "GeoPandas空间计算引擎",
        "resource_type": "api.geopandas",
        "is_builtin": True,
        "capabilities": {
            "compute": [{
                "type": "spatial",
                "dev_modes": ["workflow"],
                "supported_formats": ["geojson", "wkt", "shapely"],
                "features": ["dag", "memory_efficient", "batch"]
            }]
        },
        "task_api_config": {
            "base_url": os.getenv("DEVELOP_SERVICE_URL", "http://localhost:8085"),
            "endpoints": {
                "list": {
                    "method": "GET",
                    "path": "/api/develop/spatial/tasks",
                    "query_params": {
                        "page": "{{.Page}}",
                        "page_size": "{{.PageSize}}"
                    }
                },
                "create": {
                    "method": "POST",
                    "path": "/api/develop/spatial/tasks",
                    "body_template": {
                        "name": "{{.Name}}",
                        "description": "{{.Description}}",
                        "workflow_def": "{{.WorkflowDef}}",
                        "input_schema": "{{.InputSchema}}",
                        "output_schema": "{{.OutputSchema}}",
                        "schedule": "{{.Schedule}}"
                    }
                },
                "execute": {
                    "method": "POST",
                    "path": "/api/develop/spatial/tasks/{{.TaskID}}/execute",
                    "body_template": {
                        "inputs": "{{.Inputs}}"
                    }
                },
                "status": {
                    "method": "GET",
                    "path": "/api/develop/spatial/executions/{{.ExecutionID}}",
                    "response_mapping": {
                        "status_field": "status",
                        "message_field": "error_message",
                        "progress_field": "progress",
                        "task_id_field": "id"
                    }
                }
            },
            "timeout": {
                "create": 30,
                "execute": 600,
                "status": 10
            }
        },
        "health_check_config": {
            "endpoint": "/health",
            "timeout": 5,
            "interval": 60
        }
    }

    try:
        response = requests.post(
            f"{system_url}/internal/registry/capabilities",
            json=registration_data,
            headers={"X-Internal-API-Key": internal_api_key},
            timeout=10
        )

        if response.status_code in [200, 201]:
            logger.info("✅ Successfully registered to System Backend")
            return True
        else:
            logger.warning(f"⚠️  Failed to register to System: {response.status_code} - {response.text}")
            return False

    except Exception as e:
        logger.warning(f"⚠️  Failed to register to System: {e}")
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
    logger.info(f"🚀 Starting GeoPandas Engine on port {port}")
    app.run(host='0.0.0.0', port=port, debug=False)
