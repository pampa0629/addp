#!/bin/bash
set -e

echo "=== Jupyter Engine 启动 ==="

# 确保工作目录存在
mkdir -p /workspace/notebooks

echo "启动 Notebook Runtime API (端口: ${API_PORT:-8097})..."
cd /app
exec gunicorn \
    --bind "0.0.0.0:${API_PORT:-8097}" \
    --workers "${JUPYTER_GUNICORN_WORKERS:-2}" \
    --timeout "${JUPYTER_GUNICORN_TIMEOUT:-7200}" \
    api_server:app
