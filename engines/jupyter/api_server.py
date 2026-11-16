"""
Jupyter Engine API Server
提供 HTTP API 用于执行 Notebook（通过 papermill）
"""

import os
import io
import hashlib
import re
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

# 加载配置
from config import config
from addp_common.infra.objectstore import Ref
from addp_common.infra.objectstore.bootstrap import open_object_area
from addp_common.auth import resolve_authorization_context
from interactive_sessions import (
    InteractiveSessionManager,
    SessionConflictError,
    SessionScopeError,
    SessionNotFoundError,
    SessionValidationError,
)

# 配置日志
logging.basicConfig(
    level=getattr(logging, config.LOG_LEVEL),
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

# 打印配置信息
config.print_config()

# Select the deployment-bound provider once. Storage failure makes this Runtime unready.
try:
    notebook_store = open_object_area("develop")
except Exception:
    logger.exception("Notebook object binding unavailable")
    notebook_store = None

app = Flask(__name__)
interactive_session_manager = InteractiveSessionManager(
    notebook_store,
    config.WORKSPACE_DIR,
    config.SESSION_TTL_SECONDS,
)


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
    """The dedicated Notebook Runtime requires its bound storage to be ready."""
    ready = notebook_store is not None
    if ready:
        try:
            notebook_store.check_ready()
        except Exception as exc:
            logger.warning("Notebook storage readiness failed: %s", type(exc).__name__)
            ready = False
    return jsonify({'status': 'healthy' if ready else 'unhealthy',
                    'service': 'jupyter-engine', 'timestamp': datetime.now().isoformat()}), 200 if ready else 503

@app.route('/api/execute', methods=['POST'])
@require_develop_service
def execute_notebook():
    """
    执行 Notebook

    请求体:
    {
        "tenant_id": 1,                    # 租户 ID（由 Develop 模块传递）
        "notebook_locator": "addp-infra://object/develop/tenant_1/notebooks/test.ipynb?type=object",
        "parameters": {"user_param1": "value1"},
        "kernel": "python3"
    }

    说明：
    - tenant_id 由调用方（Develop 模块）提供
    - notebook_locator 是 develop 区域内、当前租户 Notebook 的逻辑引用
    - 输入文件从 object storage 读取
    - 输出文件保存到 object storage (executions/ 目录)
    - 租户内的用户默认共享所有 Notebook
    """
    try:
        data = request.get_json(silent=True)
        if not isinstance(data, dict):
            return jsonify({'status': 'error', 'message': 'request body must be a JSON object'}), 400

        if 'notebook_path' in data or 'minio_path' in data:
            return jsonify({'status': 'error', 'message': 'legacy Notebook file field rejected'}), 400

        # 1. 获取租户信息（由 Develop 模块传递）
        tenant_id = data.get('tenant_id')
        notebook_locator = data.get('notebook_locator')
        parameters = data.get('parameters', {})
        kernel = data.get('kernel', 'python3')

        # 验证必需参数
        if not isinstance(tenant_id, int) or isinstance(tenant_id, bool) or tenant_id <= 0 or not isinstance(notebook_locator, str) or not notebook_locator:
            return jsonify({
                'status': 'error',
                'message': 'tenant_id and notebook_locator are required'
            }), 400
        if tenant_id != g.authorization_context.tenant_id:
            return jsonify({'status': 'error', 'message': 'tenant context mismatch'}), 403
        if not isinstance(parameters, dict) or any(str(key).startswith('ds_') for key in parameters):
            return jsonify({'status': 'error', 'message': 'data source injection is not supported'}), 400
        content_digest = data.get('notebook_content_sha256')
        if not isinstance(content_digest, str) or re.fullmatch(r'[0-9a-f]{64}', content_digest) is None:
            return jsonify({'status': 'error', 'message': 'notebook_content_sha256 is required'}), 400
        try:
            ref = Ref.parse(notebook_locator)
        except ValueError:
            return jsonify({'status': 'error', 'message': 'invalid notebook_locator'}), 400
        if ref.area != 'develop' or ref.kind != 'object' or not ref.key.startswith(f'tenant_{tenant_id}/notebooks/') or not ref.key.endswith('.ipynb'):
            return jsonify({'status': 'error', 'message': 'object scope forbidden'}), 403
        if kernel != 'python3':
            return jsonify({'status': 'error', 'message': 'unsupported kernel'}), 400

        # 2. 构造 object storage 路径（租户级别隔离）
        input_key = ref.key

        # 生成执行输出路径（使用时间戳避免冲突）
        from datetime import datetime
        timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
        notebook_name = os.path.splitext(os.path.basename(ref.key))[0]
        output_notebook_name = f"{notebook_name}_exec_{timestamp}.ipynb"
        output_key = f"tenant_{tenant_id}/executions/{output_notebook_name}"

        logger.info(f"执行 Notebook: {input_key} -> {output_key}")
        logger.info(f"租户 ID: {tenant_id}")

        # 3. 从 object storage 下载 Notebook 到临时文件
        if notebook_store is None:
            return jsonify({
                'status': 'error',
                'message': 'Notebook storage unavailable'
            }), 500

        input_temp_fd, input_temp_path = tempfile.mkstemp(suffix='.ipynb')
        output_temp_fd, output_temp_path = tempfile.mkstemp(suffix='.ipynb')
        os.close(output_temp_fd)

        try:
            # 下载输入文件
            with os.fdopen(input_temp_fd, 'wb') as f:
                response = notebook_store.open(input_key)
                try:
                    notebook_bytes = response.read()
                finally:
                    response.close()
                if hashlib.sha256(notebook_bytes).hexdigest() != content_digest:
                    return jsonify({'status': 'error', 'error_code': 'notebook_content_changed',
                                    'message': 'Notebook content changed; submit a new execution'}), 409
                f.write(notebook_bytes)

            logger.info(f"已从对象存储下载: {input_key}")

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
                notebook_store.put(
                    output_key,
                    io.BytesIO(data),
                    size=len(data),
                    media_type='application/x-ipynb+json'
                )

            logger.info(f"已上传输出到对象存储: {output_key}")

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
                'output_locator': Ref('develop', output_key).uri(),
                'cell_count': len(output_nb.cells),
                'execution_count': sum(1 for c in output_nb.cells if c.cell_type == 'code'),
                'output_count': output_count,
                'outputs': outputs,
                'variables_exported': {}
            })

        except Exception as e:
            logger.error(f"从 object storage 下载或执行 Notebook 失败: {e}")
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


@app.route('/api/interactive-sessions', methods=['POST'])
@require_develop_service
def create_interactive_session():
    data = request.get_json(silent=True)
    if not isinstance(data, dict):
        return jsonify({'status': 'error', 'message': 'request body must be a JSON object'}), 400
    if data.get('tenant_id') != g.authorization_context.tenant_id:
        return jsonify({'status': 'error', 'message': 'tenant context mismatch'}), 403
    if notebook_store is None:
        return jsonify({'status': 'error', 'message': 'Notebook storage unavailable'}), 503
    runtime_host = request.host.rsplit(':', 1)[0]
    try:
        session = interactive_session_manager.create(data, runtime_host)
    except SessionValidationError as exc:
        return jsonify({'status': 'error', 'message': str(exc)}), 400
    except SessionScopeError:
        return jsonify({'status': 'error', 'message': 'Notebook object scope forbidden'}), 403
    except SessionConflictError as exc:
        return jsonify({'status': 'error', 'message': str(exc)}), 409
    except Exception as exc:
        logger.exception('failed to create interactive notebook session')
        return jsonify({'status': 'error', 'message': str(exc)}), 500
    return jsonify({'status': 'success', **session.response()}), 201


@app.route('/api/interactive-sessions/<session_id>', methods=['DELETE'])
@require_develop_service
def close_interactive_session(session_id):
    try:
        interactive_session_manager.close(session_id, g.authorization_context.tenant_id)
    except SessionNotFoundError:
        return jsonify({'status': 'error', 'message': 'interactive session not found'}), 404
    except Exception as exc:
        logger.exception('failed to close interactive notebook session id=%s', session_id)
        return jsonify({'status': 'error', 'message': str(exc)}), 500
    return '', 204

if __name__ == '__main__':
    logger.info(f"启动 Jupyter Engine API Server (端口: {config.API_PORT})...")
    app.run(host='0.0.0.0', port=config.API_PORT, debug=False)
