"""
Jupyter Engine API Server
提供 HTTP API 用于执行 Notebook（通过 papermill）
"""

import os
import json
import logging
import traceback
from flask import Flask, request, jsonify
from flask_cors import CORS
import papermill as pm
from datetime import datetime

# 配置日志
LOG_LEVEL = os.getenv('LOG_LEVEL', 'INFO').upper()
logging.basicConfig(
    level=getattr(logging, LOG_LEVEL),
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

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
    执行 Notebook

    请求体:
    {
        "input_path": "/workspace/notebooks/1/10/abc.ipynb",
        "output_path": "/workspace/notebooks/1/10/exec-123.ipynb",
        "parameters": {"param1": "value1"},
        "kernel": "python3"
    }
    """
    try:
        data = request.json

        input_path = data.get('input_path')
        output_path = data.get('output_path')
        parameters = data.get('parameters', {})
        kernel = data.get('kernel', 'python3')

        # 验证必需参数
        if not input_path or not output_path:
            return jsonify({
                'status': 'error',
                'message': 'input_path and output_path are required'
            }), 400

        # 检查输入文件是否存在
        if not os.path.exists(input_path):
            return jsonify({
                'status': 'error',
                'message': f'Input notebook not found: {input_path}'
            }), 404

        # 确保输出目录存在
        output_dir = os.path.dirname(output_path)
        os.makedirs(output_dir, exist_ok=True)

        # 执行 Notebook
        start_time = datetime.now()

        pm.execute_notebook(
            input_path,
            output_path,
            parameters=parameters,
            kernel_name=kernel,
            progress_bar=False
        )

        end_time = datetime.now()
        execution_time = (end_time - start_time).total_seconds()

        # 读取执行后的 Notebook，提取输出
        with open(output_path, 'r', encoding='utf-8') as f:
            notebook_data = json.load(f)

        # 提取所有单元格的输出
        outputs = []
        for cell in notebook_data.get('cells', []):
            if cell.get('cell_type') == 'code':
                cell_outputs = cell.get('outputs', [])
                for output in cell_outputs:
                    outputs.append(output)

        return jsonify({
            'status': 'success',
            'message': 'Notebook executed successfully',
            'execution_time_seconds': execution_time,
            'output_path': output_path,
            'output_count': len(outputs),
            'outputs': outputs
        })

    except Exception as e:
        error_trace = traceback.format_exc()
        return jsonify({
            'status': 'error',
            'message': str(e),
            'traceback': error_trace
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
    port = int(os.environ.get('API_PORT', 5000))
    app.run(host='0.0.0.0', port=port, debug=False)
