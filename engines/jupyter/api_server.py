"""
Jupyter Engine API Server
提供 HTTP API 用于执行 Notebook（通过 papermill）
"""

import os
import io
import json
import logging
import traceback
import tempfile
from flask import Flask, request, jsonify
from flask_cors import CORS
import papermill as pm
import nbformat
from datetime import datetime
from minio import Minio

# 加载配置
from config import config

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
CORS(app)  # 允许跨域请求

@app.route('/health', methods=['GET'])
def health():
    """健康检查"""
    return jsonify({
        'status': 'healthy',
        'service': 'jupyter-engine',
        'timestamp': datetime.now().isoformat()
    })

@app.route('/api/execute', methods=['POST'])
def execute_notebook():
    """
    执行 Notebook（支持自动注入数据源连接）

    请求体:
    {
        "tenant_id": 1,                    # 租户 ID（由 Develop 模块传递）
        "notebook_path": "test.ipynb",     # Notebook 相对路径
        "parameters": {
            "user_param1": "value1",
            "ds_5": {"type": "postgresql", "connection_string": "..."}
        },
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
        data = request.json

        # 1. 获取租户信息（由 Develop 模块传递）
        tenant_id = data.get('tenant_id')
        notebook_path = data.get('notebook_path')
        parameters = data.get('parameters', {})
        kernel = data.get('kernel', 'python3')

        # 验证必需参数
        if not tenant_id or not notebook_path:
            return jsonify({
                'status': 'error',
                'message': 'tenant_id and notebook_path are required'
            }), 400

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

            # 4. 分离数据源连接配置和用户参数
            connections = {}
            user_params = {}
            for key, value in parameters.items():
                if key.startswith('ds_'):
                    connections[key] = value
                else:
                    user_params[key] = value

            # 5. 读取原始 Notebook
            with open(input_temp_path, 'r', encoding='utf-8') as f:
                nb = nbformat.read(f, as_version=4)

            # 6. 如果有连接配置，注入到 Notebook
            injected_cell_index = None
            if connections:
                logger.info(f"注入 {len(connections)} 个数据源连接配置")

                # 生成连接配置的 Python 代码
                connection_code_lines = ["# 自动注入的数据源连接配置 (由 ADDP 平台管理)"]
                for var_name, conn_info in connections.items():
                    # 将连接信息转换为 Python 字典字面量
                    # JSON 的 null 需要转换为 Python 的 None
                    conn_str = json.dumps(conn_info, indent=2).replace('null', 'None')
                    connection_code_lines.append(f"{var_name} = {conn_str}")

                connection_code = "\n".join(connection_code_lines)

                # 创建新的 Code Cell
                injected_cell = nbformat.v4.new_code_cell(source=connection_code)
                injected_cell.metadata['tags'] = ['injected', 'parameters']

                # 插入到第一个 Cell 之前
                nb.cells.insert(0, injected_cell)
                injected_cell_index = 0

                logger.info(f"已注入连接配置到第 0 个 Cell")

            # 将注入后的 Notebook 写到临时文件（供 papermill 执行）
            temp_nb_fd, temp_nb_path = tempfile.mkstemp(suffix='.ipynb')
            try:
                with os.fdopen(temp_nb_fd, 'w', encoding='utf-8') as f:
                    nbformat.write(nb, f)

                logger.info(f"开始执行 Notebook: {temp_nb_path} -> {output_temp_path}")
                start_time = datetime.now()

                # 7. 使用 papermill 执行 Notebook（仅传递用户参数）
                pm.execute_notebook(
                    temp_nb_path,
                    output_temp_path,
                    parameters=user_params,
                    kernel_name=kernel,
                    progress_bar=False
                )

                end_time = datetime.now()
                execution_time = (end_time - start_time).total_seconds()

                logger.info(f"Notebook 执行完成，耗时 {execution_time:.2f} 秒")

                # 8. 读取执行后的 Notebook
                with open(output_temp_path, 'r', encoding='utf-8') as f:
                    output_nb = nbformat.read(f, as_version=4)

                # 9. 从输出中删除注入的 Cell（标记为 'injected' 的 Cell）
                if injected_cell_index is not None:
                    filtered_cells = []
                    for cell in output_nb.cells:
                        tags = cell.metadata.get('tags', [])
                        if 'injected' not in tags:
                            filtered_cells.append(cell)

                    removed_count = len(output_nb.cells) - len(filtered_cells)
                    output_nb.cells = filtered_cells

                    if removed_count > 0:
                        logger.info(f"已从输出中删除 {removed_count} 个注入的 Cell")

                        # 保存清理后的 Notebook
                        with open(output_temp_path, 'w', encoding='utf-8') as f:
                            nbformat.write(output_nb, f)

                # 10. 上传输出 Notebook 到 MinIO
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

                # 11. 提取所有单元格的输出
                outputs = []
                output_count = 0
                for cell in output_nb.cells:
                    if cell.cell_type == 'code':
                        cell_outputs = cell.get('outputs', [])
                        output_count += len(cell_outputs)
                        # 只返回前 5 个输出的预览
                        if len(outputs) < 5:
                            for output in cell_outputs[:5 - len(outputs)]:
                                outputs.append(output)

                # 提取导出的变量（TODO: 实现变量提取逻辑）
                variables_exported = {}

                return jsonify({
                    'status': 'success',
                    'message': 'Notebook executed successfully',
                    'execution_time_seconds': execution_time,
                    'output_path': output_minio_path,
                    'cell_count': len(output_nb.cells),
                    'execution_count': sum(1 for c in output_nb.cells if c.cell_type == 'code'),
                    'output_count': output_count,
                    'outputs': outputs,
                    'variables_exported': variables_exported
                })

            finally:
                # 清理临时文件
                if os.path.exists(temp_nb_path):
                    os.remove(temp_nb_path)
                    logger.debug(f"已删除临时文件: {temp_nb_path}")

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
            'traceback': error_trace,
            'error_message': str(e)
        }), 500

@app.route('/api/kernels', methods=['GET'])
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
