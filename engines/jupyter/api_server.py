"""
Jupyter Engine API Server
提供 HTTP API 用于执行 Notebook（通过 papermill）
"""

import os
import io
import asyncio
import logging
import traceback
import tempfile
from functools import wraps
from flask import Flask, g, request, jsonify
import httpx
import papermill as pm
import nbformat
from datetime import datetime
from minio import Minio

# 加载配置
from config import config
from addp_common.auth import resolve_authorization_context

# 配置日志
logging.basicConfig(
    level=getattr(logging, config.LOG_LEVEL),
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

# 打印配置信息
config.print_config()

# 初始化 MinIO 客户端
logger.info("初始化 MinIO 客户端...")
try:
    minio_client = Minio(
        config.MINIO_ENDPOINT,
        access_key=config.MINIO_ACCESS_KEY,
        secret_key=config.MINIO_SECRET_KEY,
        secure=config.MINIO_USE_SSL
    )
    # 检查连接
    if not minio_client.bucket_exists(config.MINIO_BUCKET):
        logger.warning(f"MinIO bucket '{config.MINIO_BUCKET}' 不存在，将尝试创建")
        minio_client.make_bucket(config.MINIO_BUCKET)
        logger.info(f"已创建 MinIO bucket: {config.MINIO_BUCKET}")
    else:
        logger.info(f"MinIO 连接成功，bucket: {config.MINIO_BUCKET}")
except Exception as e:
    logger.error(f"MinIO 初始化失败: {e}")
    logger.warning("Jupyter Engine 将以降级模式运行（无 MinIO 支持）")
    minio_client = None

app = Flask(__name__)


def require_develop_service(handler):
    @wraps(handler)
    def wrapped(*args, **kwargs):
        authorization = request.headers.get('Authorization', '')
        if not authorization.startswith('Bearer '):
            return jsonify({'status': 'error', 'message': 'authentication required'}), 401
        token = authorization[7:].strip()
        if not token:
            return jsonify({'status': 'error', 'message': 'authentication required'}), 401
        try:
            context = asyncio.run(resolve_authorization_context(config.SYSTEM_URL, token))
        except httpx.HTTPStatusError as exc:
            status = 401 if exc.response.status_code in {401, 403} else 503
            return jsonify({'status': 'error', 'message': 'invalid credential' if status == 401 else 'authentication service unavailable'}), status
        except (httpx.RequestError, ValueError):
            return jsonify({'status': 'error', 'message': 'authentication service unavailable'}), 503
        if (
            context.principal_type != 'service_principal'
            or context.token_type != 'service_access_token'
            or context.client_id != 'addp-develop'
            or context.context_type != 'tenant'
            or context.tenant_id is None
        ):
            return jsonify({'status': 'error', 'message': 'permission denied'}), 403
        g.authorization_context = context
        return handler(*args, **kwargs)

    return wrapped

@app.route('/health', methods=['GET'])
def health():
    """健康检查"""
    return jsonify({
        'status': 'healthy',
        'service': 'jupyter-engine',
        'timestamp': datetime.now().isoformat()
    })

@app.route('/api/execute', methods=['POST'])
@require_develop_service
def execute_notebook():
    """
    执行 Notebook

    请求体:
    {
        "tenant_id": 1,                    # 租户 ID（由 Develop 模块传递）
        "notebook_path": "test.ipynb",     # Notebook 相对路径
        "parameters": {"user_param1": "value1"},
        "kernel": "python3"
    }

    说明：
    - tenant_id 由调用方（Develop 模块）提供
    - notebook_path 是相对路径，完整路径为: tenant_{tenant_id}/notebooks/{notebook_path}
    - 输入文件从 MinIO 读取
    - 输出文件保存到 MinIO (executions/ 目录)
    - 租户内的用户默认共享所有 Notebook
    """
    try:
        data = request.get_json(silent=True)
        if not isinstance(data, dict):
            return jsonify({'status': 'error', 'message': 'request body must be a JSON object'}), 400

        # 1. 获取租户信息（由 Develop 模块传递）
        tenant_id = data.get('tenant_id')
        notebook_path = data.get('notebook_path')
        parameters = data.get('parameters', {})
        kernel = data.get('kernel', 'python3')

        # 验证必需参数
        if not isinstance(tenant_id, int) or isinstance(tenant_id, bool) or tenant_id <= 0 or not isinstance(notebook_path, str) or not notebook_path:
            return jsonify({
                'status': 'error',
                'message': 'tenant_id and notebook_path are required'
            }), 400
        if tenant_id != g.authorization_context.tenant_id:
            return jsonify({'status': 'error', 'message': 'tenant context mismatch'}), 403
        if not isinstance(parameters, dict) or any(str(key).startswith('ds_') for key in parameters):
            return jsonify({'status': 'error', 'message': 'data source injection is not supported'}), 400
        if kernel != 'python3':
            return jsonify({'status': 'error', 'message': 'unsupported kernel'}), 400

        # 2. 构造 MinIO 路径（租户级别隔离）
        input_minio_path = f"tenant_{tenant_id}/notebooks/{notebook_path}"

        # 生成执行输出路径（使用时间戳避免冲突）
        from datetime import datetime
        timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
        notebook_name = os.path.splitext(os.path.basename(notebook_path))[0]
        output_notebook_name = f"{notebook_name}_exec_{timestamp}.ipynb"
        output_minio_path = f"tenant_{tenant_id}/executions/{output_notebook_name}"

        logger.info(f"执行 Notebook: {input_minio_path} -> {output_minio_path}")
        logger.info(f"租户 ID: {tenant_id}")

        # 3. 从 MinIO 下载 Notebook 到临时文件
        if not minio_client:
            return jsonify({
                'status': 'error',
                'message': 'MinIO client is not initialized'
            }), 500

        input_temp_fd, input_temp_path = tempfile.mkstemp(suffix='.ipynb')
        output_temp_fd, output_temp_path = tempfile.mkstemp(suffix='.ipynb')

        try:
            # 下载输入文件
            with os.fdopen(input_temp_fd, 'wb') as f:
                response = minio_client.get_object(config.MINIO_BUCKET, input_minio_path)
                f.write(response.read())
                response.close()
                response.release_conn()

            logger.info(f"已从 MinIO 下载: {input_minio_path}")

            # 关闭 output_temp_fd，papermill 需要写入
            os.close(output_temp_fd)

            logger.info(f"开始执行 Notebook: {input_temp_path} -> {output_temp_path}")
            start_time = datetime.now()

            # 使用 papermill 执行 Notebook，只传递公开任务参数。
            pm.execute_notebook(
                input_temp_path,
                output_temp_path,
                parameters=parameters,
                kernel_name=kernel,
                progress_bar=False
            )

            end_time = datetime.now()
            execution_time = (end_time - start_time).total_seconds()

            logger.info(f"Notebook 执行完成，耗时 {execution_time:.2f} 秒")

            with open(output_temp_path, 'r', encoding='utf-8') as f:
                output_nb = nbformat.read(f, as_version=4)

            with open(output_temp_path, 'rb') as f:
                data = f.read()
                minio_client.put_object(
                    config.MINIO_BUCKET,
                    output_minio_path,
                    io.BytesIO(data),
                    length=len(data),
                    content_type='application/x-ipynb+json'
                )

            logger.info(f"已上传输出到 MinIO: {output_minio_path}")

            outputs = []
            output_count = 0
            for cell in output_nb.cells:
                if cell.cell_type == 'code':
                    cell_outputs = cell.get('outputs', [])
                    output_count += len(cell_outputs)
                    if len(outputs) < 5:
                        for output in cell_outputs[:5 - len(outputs)]:
                            outputs.append(output)

            return jsonify({
                'status': 'success',
                'message': 'Notebook executed successfully',
                'execution_time_seconds': execution_time,
                'output_path': output_minio_path,
                'cell_count': len(output_nb.cells),
                'execution_count': sum(1 for c in output_nb.cells if c.cell_type == 'code'),
                'output_count': output_count,
                'outputs': outputs,
                'variables_exported': {}
            })

        except Exception as e:
            logger.error(f"从 MinIO 下载或执行 Notebook 失败: {e}")
            raise

        finally:
            # 清理输入/输出临时文件
            if os.path.exists(input_temp_path):
                os.remove(input_temp_path)
                logger.debug(f"已删除临时文件: {input_temp_path}")
            if os.path.exists(output_temp_path):
                os.remove(output_temp_path)
                logger.debug(f"已删除临时文件: {output_temp_path}")

    except Exception as e:
        error_trace = traceback.format_exc()
        logger.error(f"Notebook 执行失败: {str(e)}\n{error_trace}")
        return jsonify({
            'status': 'error',
            'message': str(e),
            'error_message': str(e)
        }), 500

@app.route('/api/kernels', methods=['GET'])
@require_develop_service
def list_kernels():
    """列出可用的 Kernel"""
    try:
        # 简单返回预定义的 Kernel 列表
        kernels = [
            {
                'name': 'python3',
                'display_name': 'Python 3',
                'language': 'python'
            }
        ]
        return jsonify({
            'status': 'success',
            'kernels': kernels
        })
    except Exception as e:
        return jsonify({
            'status': 'error',
            'message': str(e)
        }), 500

if __name__ == '__main__':
    logger.info(f"启动 Jupyter Engine API Server (端口: {config.API_PORT})...")
    app.run(host='0.0.0.0', port=config.API_PORT, debug=False)
