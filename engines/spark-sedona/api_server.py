"""
Spark Sedona Engine Flask API Server
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

# 内存存储 (生产环境应使用数据库)
executions = {}  # {execution_id: {status, result, ...}}


# ========================================
# 健康检查
# ========================================

@app.route('/health', methods=['GET'])
def health_check():
    """健康检查端点"""
    return jsonify({
        "status": "healthy",
        "service": "spark-sedona-engine",
        "version": "1.0.0"
    }), 200


# ========================================
# 算子列表 (供前端使用)
# ========================================

@app.route('/api/operators', methods=['GET'])
def get_operators():
    """
    获取所有空间算子列表 (Common 模块标准接口)
    供 Develop Backend OperatorDiscoveryService 使用

    Response:
        {
            "status": "success",
            "operators": [
                {
                    "id": "load",
                    "name": "load",
                    "display_name": "数据加载",
                    "module": "spark",
                    "category": "数据I/O",
                    "description": "从数据库、文件、湖仓加载数据",
                    "parameters": [...],
                    "output_ports": [...]
                },
                ...
            ],
            "count": 35
        }
    """
    try:
        # 导入算子元数据定义
        from operator_metadata import get_operator_metadata

        operators = get_operator_metadata()

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
# 即时执行 (供 Develop 使用)
# ========================================

@app.route('/api/spatial/workflow', methods=['POST'])
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


@app.route('/api/spatial/operators/<operator_name>/execute', methods=['POST'])
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

@app.route('/api/spatial/executions/<execution_id>', methods=['GET'])
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
    启动时自动注册到 System Backend (单次尝试)
    """
    import requests

    system_url = os.getenv('SYSTEM_SERVICE_URL', 'http://localhost:8080')
    internal_api_key = os.getenv('INTERNAL_API_KEY', '')

    registration_data = {
        "unique_identifier": "api.spark_sedona",
        "name": "spark_sedona_engine",
        "display_name": "Spark Sedona 空间计算引擎",
        "engine_type": "api.spark_sedona",
        "is_builtin": True,
        "capabilities": {
            "compute": [{
                "dev_modes": ["workflow"],
                "supported_formats": ["dataframe", "sql"],
                "features": ["dag", "distributed", "lakehouse", "large_scale"],
                "description": "基于 Spark Sedona 的分布式空间计算引擎"
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
    logger.info(f"🚀 Starting Spark Sedona Engine on port {port}")
    app.run(host='0.0.0.0', port=port, debug=False)
