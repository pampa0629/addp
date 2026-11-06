"""
GeoPython Workflow Flask API Server
Python 数据处理工作流运行时 API 服务
提供 REST API 接口供 Develop 和 Orchestrator 调用
"""


def run_runtime():
    """One HTTP process model for native development and the product image."""
    import os
    import threading
    from dotenv import find_dotenv, load_dotenv
    from gunicorn.app.base import BaseApplication

    load_dotenv(find_dotenv())
    port = int(os.getenv('PORT', '8099'))

    def register_runtime():
        from api_server import register_to_system_with_retry
        from addp_common.client.runtime_registration import register_after_listener

        register_after_listener(register_to_system_with_retry, port)

    def post_worker_init(worker):
        threading.Thread(target=register_runtime,
                         name='geopython-runtime-registration', daemon=True).start()

    class RuntimeApplication(BaseApplication):
        def load_config(self):
            for key, value in {
                'bind': f"{os.getenv('WORKFLOW_BIND_HOST', '0.0.0.0')}:{port}",
                'workers': 1, 'worker_class': 'gthread', 'threads': 4,
                'timeout': int(os.getenv('GEOPYTHON_WORKFLOW_GUNICORN_TIMEOUT', '7200')),
                'preload_app': False, 'post_worker_init': post_worker_init,
            }.items():
                self.cfg.set(key, value)

        def load(self):
            from api_server import app as application

            return application

    RuntimeApplication().run()


if __name__ == '__main__':
    # Native GIS libraries must be imported in the worker, never before fork.
    run_runtime()
    raise SystemExit(0)


import base64
import binascii

from addp_common.workflow_runtime.health import health_dependency, runtime_health

from flask import Flask, request, jsonify
from flask_cors import CORS
from operators.io_operators import STORAGE_ACCESS
import logging
import uuid
from datetime import datetime
import os
import time

# 加载环境变量（从项目根目录的 .env 文件）
from dotenv import load_dotenv, find_dotenv
# 自动向上搜索 .env 文件（类似 common/config/loader.go 的 LoadEnv）
load_dotenv(find_dotenv())

from workflow_engine import GeoPythonWorkflowRunner, execute_single_operator
from addp_common.workflow_runtime import ExecutionRegistry, WorkflowValidationError, validate_execution_authorization
from geometry_batches import (
    GeometryBatchInputError,
    decode_geometry_batch_arrow,
    encode_geometry_batch_arrow,
    parse_geometry_batch_crs,
)
from operators import get_operator, list_operators
from raster_resources import RasterResources, RasterPolicyUnavailable, requires_raster
from addp_common.workflow_runtime.admission import RuntimeBusy

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
executions = ExecutionRegistry()
raster_resources = RasterResources()

_RASTER_MESSAGES = {
    'zh-cn': {'RUNTIME_BUSY': '栅格运行时容量已满，请稍后重试',
              'RUNTIME_POLICY_UNAVAILABLE': '栅格资源策略暂不可用',
              'INVALID_RASTER_TENANT': 'tenant_id 必须是正整数'},
    'en': {'RUNTIME_BUSY': 'Raster runtime capacity is busy; retry later',
           'RUNTIME_POLICY_UNAVAILABLE': 'Raster resource policy is unavailable',
           'INVALID_RASTER_TENANT': 'tenant_id must be a positive integer'},
}


def _request_language():
    return request.accept_languages.best_match(['zh-cn', 'en']) or 'zh-cn'


def _raster_message(code):
    return _RASTER_MESSAGES[_request_language()][code]


def _raster_busy_response(code):
    return jsonify(error_response(code, _raster_message(code))), 503


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


_BINARY_GEOMETRY_MESSAGES = {
    "zh-cn": {
        "envelope": "几何批请求封装无效",
        "transport": "几何批须使用 Arrow 流和标准 base64",
        "geometry_encoding": "binary_payload.metadata.geometry_encoding 必须是 ewkb",
        "arrow_encoding": "Arrow schema geometry encoding 必须是 ewkb",
        "arrow": "Arrow 几何批无效",
        "column": "Arrow 几何批须包含唯一且名称一致的二进制几何列",
        "ewkb": "几何列包含无法解析的 EWKB",
        "crs": "请求中的源或目标 CRS 无效",
        "crs_mismatch": "几何批的源 CRS 与请求参数不一致",
        "execution": "几何批执行失败",
    },
    "en": {
        "envelope": "Invalid geometry batch request envelope",
        "transport": "Geometry batch requires an Arrow stream and standard base64",
        "geometry_encoding": "binary_payload.metadata.geometry_encoding must be ewkb",
        "arrow_encoding": "Arrow schema geometry encoding must be ewkb",
        "arrow": "Invalid Arrow geometry batch",
        "column": "Arrow batch requires one binary geometry column with matching names",
        "ewkb": "Geometry column contains invalid EWKB",
        "crs": "Invalid source or target CRS in the request",
        "crs_mismatch": "Geometry batch source CRS does not match the request parameter",
        "execution": "Geometry batch execution failed",
    },
}


def _binary_geometry_error(start, reason, *, execution=False):
    code = ErrorCode.EXECUTION_FAILED if execution else ErrorCode.INVALID_PARAMS
    response = error_response(code, _BINARY_GEOMETRY_MESSAGES[_request_language()][reason])
    response["execution_time_ms"] = (time.time() - start) * 1000
    return jsonify(response), 500 if execution else 400


def _invoke_binary_geometry(operator_func, direct_binary, params, binary_payload, start):
    try:
        if not isinstance(binary_payload, dict) or not isinstance(params, dict) or params.keys() - {"source_crs", "target_crs"}:
            raise GeometryBatchInputError("envelope")
        content_type = direct_binary["content_type"]
        encoding = direct_binary["encoding"]
        data = binary_payload.get("data")
        if (binary_payload.get("content_type") != content_type or binary_payload.get("encoding") != encoding
                or not isinstance(data, str) or not data.strip()):
            raise GeometryBatchInputError("transport")
        metadata = binary_payload.get("metadata")
        geometry_encoding = direct_binary["geometry_encoding"]
        if not isinstance(metadata, dict) or str(metadata.get("geometry_encoding", "")).strip().lower() != geometry_encoding:
            raise GeometryBatchInputError("geometry_encoding")
        try:
            payload = base64.b64decode(data.strip(), validate=True)
        except (binascii.Error, ValueError):
            raise GeometryBatchInputError("transport") from None
        input_gdf = decode_geometry_batch_arrow(payload, expected_encoding=geometry_encoding,
                                                declared_column=metadata.get("geometry_column"))
        source_crs = parse_geometry_batch_crs(params.get("source_crs"))
        parse_geometry_batch_crs(params.get("target_crs"))
        if input_gdf.crs is None and source_crs is None:
            raise GeometryBatchInputError("crs")
        if input_gdf.crs is not None and source_crs is not None and not input_gdf.crs.equals(source_crs):
            raise GeometryBatchInputError("crs_mismatch")
    except GeometryBatchInputError as exc:
        return _binary_geometry_error(start, exc.args[0])
    except Exception as exc:
        logger.error("Binary geometry preparation failed (%s)", type(exc).__name__)
        return _binary_geometry_error(start, "execution", execution=True)

    try:
        result_gdf = operator_func(input_gdf=input_gdf, **params)
        if not isinstance(result_gdf, type(input_gdf)):
            raise TypeError("binary direct operator must return a GeoDataFrame")
        result_crs = str(result_gdf.crs) if result_gdf.crs else ""
        column = result_gdf.geometry.name
        encoded = encode_geometry_batch_arrow(
            result_gdf, geometry_column=column, source_crs=result_crs,
            target_crs=result_crs, geometry_encoding=geometry_encoding,
        )
        return jsonify({
            "status": "success",
            "result": {"row_count": len(result_gdf), "geometry_column": column, "crs": result_crs},
            "binary_payload": {
                "content_type": content_type,
                "encoding": encoding,
                "name": direct_binary["output_name"],
                "data": base64.b64encode(encoded).decode("ascii"),
                "metadata": {"geometry_column": column, "geometry_encoding": geometry_encoding, "crs": result_crs},
            },
            "execution_time_ms": (time.time() - start) * 1000,
        }), 200
    except Exception as exc:
        logger.error("Binary geometry execution failed (%s)", type(exc).__name__)
        return _binary_geometry_error(start, "execution", execution=True)


# ========================================
# 健康检查
# ========================================

@app.route('/health', methods=['GET'])
def health_check():
    def probe():
        import geopandas as gpd
        import pandas as pd
        from osgeo import gdal

        return {
            "operators_count": len(list_operators()),
            "raster_resources": raster_resources.snapshot(),
            "dependencies": {
                "geopandas": health_dependency(True, version=gpd.__version__),
                "pandas": health_dependency(True, version=pd.__version__),
                "gdal": health_dependency(True, version=gdal.VersionInfo("RELEASE_NAME")),
            },
        }

    body, code = runtime_health(
        "geopython-workflow-engine", "1.0.0", int((datetime.now() - start_time).total_seconds()), probe,
    )
    return jsonify(body), code

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
def execute_workflow_api():
    """Validate authorization, submit asynchronously, then poll the canonical status endpoint."""
    data = request.get_json(silent=True)
    if not isinstance(data, dict) or not isinstance(data.get('input_data', {}), dict):
        return jsonify(error_response(ErrorCode.INVALID_PARAMS, 'Request and input_data must be objects')), 400
    try:
        operators = {op['id']: op['effects'] for op in list_operators() if 'workflow' in op['execution_modes']}
        validate_execution_authorization(data.get('workflow_def'), operator_effects=operators, runtime=data.get('runtime'))
        metadata = {op['id']: op for op in list_operators()}
        raster = any(requires_raster(metadata[task['operator']]) for task in data['workflow_def']['tasks'])
        admission = raster_resources.refresh() if raster else None
        snapshot = executions.submit(GeoPythonWorkflowRunner(language=_request_language()), data['workflow_def'], data.get('input_data'),
                                     admission=admission, tenant_id=data['runtime']['tenant_id'])
        return jsonify(snapshot.to_dict()), 202
    except RasterPolicyUnavailable:
        return _raster_busy_response('RUNTIME_POLICY_UNAVAILABLE')
    except RuntimeBusy:
        return _raster_busy_response('RUNTIME_BUSY')
    except WorkflowValidationError as exc:
        return jsonify(error_response(ErrorCode.WORKFLOW_INVALID, str(exc))), 400


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

        data = request.get_json(silent=True)
        direct_binary = (operator_meta.get("attributes") or {}).get("direct_binary") or {}
        if not isinstance(data, dict) and direct_binary:
            return _binary_geometry_error(start, "envelope")
        data = data or {}
        params = data.get('params')
        binary_payload = data.get('binary_payload')

        logger.info(f"Invoking operator {operator_name} directly")
        if binary_payload is not None:
            if not direct_binary:
                response = error_response(
                    ErrorCode.DIRECT_NOT_SUPPORTED,
                    f"算子 '{operator_name}' 不支持 binary direct 调用",
                )
                response["execution_time_ms"] = (time.time() - start) * 1000
                return jsonify(response), 403
            return _invoke_binary_geometry(get_operator(operator_name), direct_binary,
                                           {} if params is None else params, binary_payload, start)

        params = params or {}
        if not isinstance(params, dict):
            response = error_response(ErrorCode.INVALID_PARAMS, "请求体中的 'params' 必须是对象")
            response["execution_time_ms"] = (time.time() - start) * 1000
            return jsonify(response), 400

        if requires_raster(operator_meta):
            tenant = data.get('tenant_id')
            if type(tenant) is not int or tenant <= 0:
                return jsonify(error_response(ErrorCode.INVALID_PARAMS, _raster_message('INVALID_RASTER_TENANT'))), 400
            with raster_resources.refresh().direct(tenant):
                result = execute_single_operator(operator_name, params)
        else:
            result = execute_single_operator(operator_name, params)

        execution_time = (time.time() - start) * 1000

        if result.get('error_code') == 'ORC_SOURCE_CHANGED':
            return jsonify(error_response('ORC_SOURCE_CHANGED', 'ORC source changed; refresh metadata')), 409
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

    except RasterPolicyUnavailable:
        return _raster_busy_response('RUNTIME_POLICY_UNAVAILABLE')
    except RuntimeBusy:
        return _raster_busy_response('RUNTIME_BUSY')
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
    snapshot = executions.get(execution_id)
    if snapshot is None:
        return jsonify(error_response(ErrorCode.EXECUTION_NOT_FOUND, 'Execution not found')), 404
    return jsonify(snapshot.to_dict()), 200


# ========================================
# System 注册（自动注册到 System Backend）
# ========================================

def register_to_system():
    """
    向 System Backend 自注册（创建或更新引擎记录）
    """
    from addp_common.client import register_runtime_engine, runtime_advertised_port

    system_url = os.getenv('SYSTEM_URL', 'http://localhost:8180')
    client_secret = os.getenv('GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET', '')

    # 读取自身配置
    port = int(os.getenv('PORT', 8099))
    protocol = os.getenv('PROTOCOL', 'http')
    runtime_host = os.getenv('RUNTIME_HOST', 'localhost').strip()
    connection_info = {"protocol": protocol, "port": runtime_advertised_port(port)}
    if runtime_host:
        connection_info["host"] = runtime_host

    # 构建注册请求
    payload = {
        "engine_type": "geopython_workflow",
        "name": "GeoPython Workflow",
        "description": "基于 Python 地理计算生态的工作流引擎，支持 Pandas、GeoPandas、GDAL/OGR 等能力",
        "connection_info": connection_info,
        "capabilities": {
            "schema_version": "engine.capabilities/v1",
            "engine_type": "geopython_workflow",
            "engine_family": "workflow",
            "compute": {
                "workflow": {
                    "supported": True,
                    "runtime_api": "addp.workflow/v1",
                    "dynamic_operators": True,
                    "storage_access": STORAGE_ACCESS
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
    from addp_common.client import retry_runtime_registration

    retry_runtime_registration(register_to_system, "geopython_workflow", logger)
