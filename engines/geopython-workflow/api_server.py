"""
GeoPython Workflow Flask API Server
Python 数据处理工作流运行时 API 服务
提供 REST API 接口供 Develop 和 Orchestrator 调用
"""

import base64

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
from addp_common.workflow_runtime import validate_execution_authorization
from geometry_batches import (
    GEOMETRY_BATCH_METADATA_PREFIX,
    decode_geometry_batch_arrow,
    encode_geometry_batch_arrow,
    geometry_batch_arrow_metadata,
)
from operators import get_operator, list_operators

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
            "service": "geopython-workflow-engine",
            "version": "1.0.0",
            "uptime": 3600,
            "operators_count": len(list_operators()),
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
            "service": "geopython-workflow-engine",
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
            "count": len(list_operators())
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

@app.route('/api/workflow', methods=['POST'])
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
    execution_id = None

    try:
        data = request.get_json(silent=True) or {}
        workflow_def = data.get('workflow_def')
        input_data = data.get('input_data', {})

        if not workflow_def:
            response = error_response(
                ErrorCode.INVALID_PARAMS,
                "请求体缺少 'workflow_def' 字段"
            )
            response["execution_time_ms"] = (time.time() - start) * 1000
            return jsonify(response), 400

        operators = {
            op["id"]: op["effects"]
            for op in list_operators()
            if "workflow" in op.get("execution_modes", [])
        }
        validate_execution_authorization(
            workflow_def,
            operator_effects=operators,
            runtime=data.get("runtime"),
        )

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
            "error_code": result.get('error_code'),
            "details": result.get('details') or result.get('traceback'),
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
            response['logs'] = result.get('logs', [])
            logger.info(f"Workflow {execution_id} completed successfully in {execution_time:.2f}ms")
            return jsonify(response), 200
        else:
            error_code = result.get('error_code')
            if error_code == ErrorCode.WORKFLOW_INVALID:
                response.update(error_response(
                    ErrorCode.WORKFLOW_INVALID,
                    result.get('error', '工作流定义无效')
                ))
                response['logs'] = result.get('logs', [])
                response['traceback'] = result.get('traceback', '')
                return jsonify(response), 400

            response.update(error_response(
                ErrorCode.EXECUTION_FAILED,
                result.get('error', '工作流执行失败')
            ))
            response['logs'] = result.get('logs', [])
            response['traceback'] = result.get('traceback', '')
            return jsonify(response), 500

    except ValueError as e:
        execution_time = (time.time() - start) * 1000
        logger.error(f"Workflow validation failed: {e}")
        if execution_id:
            executions[execution_id] = {
                "execution_id": execution_id,
                "status": "failed",
                "result": None,
                "all_results": None,
                "error": f"工作流定义无效: {str(e)}",
                "error_code": ErrorCode.WORKFLOW_INVALID,
                "details": str(e),
                "started_at": datetime.now().isoformat(),
                "execution_time_ms": execution_time
            }
        response = error_response(
            ErrorCode.WORKFLOW_INVALID,
            f"工作流定义无效: {str(e)}"
        )
        if execution_id:
            response["execution_id"] = execution_id
        response["execution_time_ms"] = execution_time
        return jsonify(response), 400

    except Exception as e:
        execution_time = (time.time() - start) * 1000
        logger.error(f"Workflow execution failed: {e}", exc_info=True)
        if execution_id:
            executions[execution_id] = {
                "execution_id": execution_id,
                "status": "failed",
                "result": None,
                "all_results": None,
                "error": f"工作流执行失败: {str(e)}",
                "error_code": ErrorCode.EXECUTION_FAILED,
                "details": str(e),
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

    仅允许调用 execution_modes 包含 direct 的算子。direct 是业务模块受控能力调用，
    不创建 Develop/Orchestrator/Monitor 任务。

    Path Parameters:
        operator_name: 算子名称

    Request Body:
        {
            "params": {...}
        }

    Response:
        {
            "status": "success",
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

        operator_meta = next(op for op in operators if op['name'] == operator_name)
        execution_modes = operator_meta['execution_modes']
        if 'direct' not in execution_modes:
            execution_time = (time.time() - start) * 1000
            response = error_response(
                ErrorCode.DIRECT_NOT_SUPPORTED,
                f"算子 '{operator_name}' 不支持 direct 调用"
            )
            response["execution_time_ms"] = execution_time
            return jsonify(response), 403

        data = request.get_json(silent=True) or {}
        params = data.get('params') or {}
        binary_payload = data.get('binary_payload')

        if not isinstance(params, dict):
            execution_time = (time.time() - start) * 1000
            response = error_response(
                ErrorCode.INVALID_PARAMS,
                "请求体中的 'params' 必须是对象"
            )
            response["execution_time_ms"] = execution_time
            return jsonify(response), 400

        logger.info(f"Invoking operator {operator_name} directly")
        operator_func = get_operator(operator_name)

        if binary_payload is not None:
            direct_binary = (operator_meta.get("attributes") or {}).get("direct_binary") or {}
            if not direct_binary:
                execution_time = (time.time() - start) * 1000
                response = error_response(
                    ErrorCode.DIRECT_NOT_SUPPORTED,
                    f"算子 '{operator_name}' 不支持 binary direct 调用",
                )
                response["execution_time_ms"] = execution_time
                return jsonify(response), 403
            content_type = str(binary_payload.get("content_type") or "").strip()
            if content_type and direct_binary.get("content_type") and content_type != direct_binary.get("content_type"):
                execution_time = (time.time() - start) * 1000
                response = error_response(
                    ErrorCode.INVALID_PARAMS,
                    f"算子 '{operator_name}' 的 binary_payload.content_type 不匹配",
                )
                response["execution_time_ms"] = execution_time
                return jsonify(response), 400

            payload_metadata = binary_payload.get("metadata")
            if not isinstance(payload_metadata, dict):
                payload_metadata = {}
            if "geometry_encoding" not in payload_metadata:
                execution_time = (time.time() - start) * 1000
                response = error_response(
                    ErrorCode.INVALID_PARAMS,
                    f"算子 '{operator_name}' 缺少 binary_payload.metadata.geometry_encoding",
                )
                response["execution_time_ms"] = execution_time
                return jsonify(response), 400
            geometry_encoding = str(
                payload_metadata.get("geometry_encoding")
            ).strip().lower()
            expected_geometry_encoding = str(direct_binary.get("geometry_encoding") or "ewkb").strip().lower()
            if geometry_encoding != expected_geometry_encoding:
                execution_time = (time.time() - start) * 1000
                response = error_response(
                    ErrorCode.INVALID_PARAMS,
                    f"算子 '{operator_name}' 的 binary_payload.metadata.geometry_encoding 必须是 {expected_geometry_encoding}",
                )
                response["execution_time_ms"] = execution_time
                return jsonify(response), 400

            payload_data = str(binary_payload.get("data") or "").strip()
            if not payload_data:
                execution_time = (time.time() - start) * 1000
                response = error_response(
                    ErrorCode.INVALID_PARAMS,
                    "binary_payload.data is required",
                )
                response["execution_time_ms"] = execution_time
                return jsonify(response), 400

            try:
                binary_bytes = base64.b64decode(payload_data)
            except Exception as exc:
                execution_time = (time.time() - start) * 1000
                response = error_response(
                    ErrorCode.INVALID_PARAMS,
                    "binary_payload.data 不是合法的 base64",
                    details=str(exc),
                )
                response["execution_time_ms"] = execution_time
                return jsonify(response), 400

            try:
                arrow_metadata = geometry_batch_arrow_metadata(binary_bytes)
                arrow_geometry_encoding = str(
                    arrow_metadata.get(f"{GEOMETRY_BATCH_METADATA_PREFIX}encoding") or ""
                ).strip().lower()
            except Exception as exc:
                execution_time = (time.time() - start) * 1000
                response = error_response(
                    ErrorCode.INVALID_PARAMS,
                    "binary_payload.data 不是合法的 Arrow geometry batch",
                    details=str(exc),
                )
                response["execution_time_ms"] = execution_time
                return jsonify(response), 400
            if arrow_geometry_encoding != expected_geometry_encoding:
                execution_time = (time.time() - start) * 1000
                response = error_response(
                    ErrorCode.INVALID_PARAMS,
                    f"算子 '{operator_name}' 的 Arrow schema geometry encoding 必须是 {expected_geometry_encoding}",
                )
                response["execution_time_ms"] = execution_time
                return jsonify(response), 400

            try:
                input_gdf = decode_geometry_batch_arrow(binary_bytes)
                if input_gdf is None:
                    raise ValueError("decoded geometry batch is empty")
                source_crs = str(input_gdf.crs) if input_gdf.crs else str(params.get("source_crs") or "")
                call_params = dict(params)
                call_params["input_gdf"] = input_gdf
                result_gdf = operator_func(**call_params)
                if not isinstance(result_gdf, type(input_gdf)):
                    raise ValueError("binary direct operator must return a GeoDataFrame")
                result_crs = str(result_gdf.crs) if result_gdf.crs else ""
                encoded = encode_geometry_batch_arrow(
                    result_gdf,
                    geometry_column=result_gdf.geometry.name,
                    source_crs=result_crs,
                    target_crs=result_crs,
                    geometry_encoding=geometry_encoding,
                )
                execution_time = (time.time() - start) * 1000
                response = {
                    "status": "success",
                    "result": {
                        "row_count": len(result_gdf),
                        "geometry_column": result_gdf.geometry.name,
                        "crs": str(result_gdf.crs) if result_gdf.crs else "",
                    },
                    "binary_payload": {
                        "content_type": direct_binary.get("content_type") or "application/vnd.apache.arrow.stream",
                        "encoding": direct_binary.get("encoding") or "arrow",
                        "name": direct_binary.get("output_name") or "geometry_batch",
                        "data": base64.b64encode(encoded).decode("ascii"),
                        "metadata": {
                            "geometry_column": result_gdf.geometry.name,
                            "geometry_encoding": geometry_encoding,
                            "crs": result_crs,
                        },
                    },
                    "execution_time_ms": execution_time,
                }
                logger.info(f"Operator {operator_name} completed successfully in {execution_time:.2f}ms")
                return jsonify(response), 200
            except Exception as exc:
                execution_time = (time.time() - start) * 1000
                response = error_response(
                    ErrorCode.EXECUTION_FAILED,
                    f"算子 '{operator_name}' 执行失败: {str(exc)}",
                    details=str(exc),
                )
                response["execution_time_ms"] = execution_time
                return jsonify(response), 500

        result = execute_single_operator(operator_name, params)

        execution_time = (time.time() - start) * 1000

        if result['status'] == 'success':
            response = {
                "status": "success",
                "result": result['result'],
                "execution_time_ms": execution_time
            }
            logger.info(f"Operator {operator_name} completed successfully in {execution_time:.2f}ms")
            return jsonify(response), 200
        else:
            response = error_response(
                ErrorCode.EXECUTION_FAILED,
                result.get('error', '算子执行失败'),
                details=result.get('traceback', '')
            )
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
            "result": "...",  // GeoJSON
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
        "error": execution.get('error'),
        "error_code": execution.get('error_code'),
        "details": execution.get('details'),
        "progress": 100 if execution['status'] in ['success', 'failed'] else 50,
        "started_at": execution.get('started_at'),
        "execution_time_ms": execution.get('execution_time_ms')
    }), 200


# ========================================
# System 注册（自动注册到 System Backend）
# ========================================

def register_to_system():
    """
    向 System Backend 自注册（创建或更新引擎记录）
    """
    from addp_common.client import register_runtime_engine

    system_url = os.getenv('SYSTEM_URL', 'http://localhost:8180')
    client_secret = os.getenv('GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET', '')

    # 读取自身配置
    port = int(os.getenv('PORT', 8099))
    protocol = os.getenv('PROTOCOL', 'http')

    # 构建注册请求
    payload = {
        "engine_type": "geopython_workflow",
        "name": "GeoPython Workflow",
        "description": "基于 Python 地理计算生态的工作流引擎，支持 Pandas、GeoPandas、GDAL/OGR 等能力",
        "connection_info": {
            "protocol": protocol,
            "port": port
            # host 由 System 自动填充
        },
        "capabilities": {
            "schema_version": "engine.capabilities/v1",
            "engine_type": "geopython_workflow",
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
        logger.info(f"📤 发送注册请求到: {system_url}/api/v1/system/runtime/engines")
        logger.info(f"📦 Payload: {payload}")
        status_code, body = register_runtime_engine(
            system_url, "addp-geopython", client_secret, payload
        )
        logger.info(f"📥 收到响应: status={status_code}, body={body[:500]}")
        if status_code == 202:
            logger.info("✅ Successfully registered to System Backend")
            return True
        else:
            logger.warning(f"⚠️  Failed to register: {status_code} - {body}")
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
    logger.info(f"🚀 Starting GeoPython Workflow on port {port}")
    app.run(host='0.0.0.0', port=port, debug=False)
