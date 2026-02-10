#!/bin/bash
set -e

echo "Starting Jupyter Engine..."

# 确保工作目录存在
mkdir -p /workspace/notebooks

# 启动 Flask API Server（后台运行）
echo "Starting API Server on port ${API_PORT:-5000}..."
python3 /app/api_server.py &

# 启动 Jupyter Lab（前台运行）
echo "Starting Jupyter Lab on port ${JUPYTER_PORT:-8088}..."
exec jupyter lab \
    --config=/root/.jupyter/jupyter_notebook_config.py \
    --no-browser \
    --allow-root
