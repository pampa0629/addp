#!/bin/bash
set -e

echo "=== Jupyter Engine 启动 ==="
echo "租户 ID: ${ADDP_TENANT_ID:-未设置}"
echo "API 地址: ${ADDP_API_BASE:-http://host.docker.internal:8085}"
echo ""

# 确保工作目录存在
mkdir -p /workspace/notebooks

# 配置 IPython startup 脚本（数据源自动注入）
echo "配置 IPython startup 脚本..."
mkdir -p /root/.ipython/profile_default/startup

# 复制数据源自动加载脚本
if [ -f /app/ipython_startup_00_addp_datasources.py ]; then
    cp /app/ipython_startup_00_addp_datasources.py /root/.ipython/profile_default/startup/00-addp-datasources.py
    echo "✅ IPython startup 脚本已部署"
else
    echo "⚠️  IPython startup 脚本不存在，跳过"
fi

echo ""

# 1. 启动 Flask API Server（后台运行）
echo "启动 Flask API Server (端口: ${API_PORT:-5000})..."
python3 /app/api_server.py &
API_PID=$!
echo "Flask API Server 已启动 (PID: $API_PID)"

# 2. 启动 Jupyter Lab（后台运行）
echo "启动 Jupyter Lab (端口: ${JUPYTER_PORT:-8088})..."
jupyter lab \
    --config=/root/.jupyter/jupyter_notebook_config.py \
    --no-browser \
    --allow-root &
JUPYTER_PID=$!
echo "Jupyter Lab 已启动 (PID: $JUPYTER_PID)"

# 3. 等待服务就绪
echo "等待服务就绪..."
sleep 10

# 4. 向 System 模块注册引擎（不影响启动）
echo "注册 Jupyter Engine 到 System 模块..."
if [ -f /app/register.py ]; then
    python3 /app/register.py
    if [ $? -eq 0 ]; then
        echo "✅ 注册成功"
    else
        echo "⚠️  注册失败，但引擎仍正常运行"
    fi
else
    echo "⚠️  注册脚本不存在，跳过注册"
fi

# 5. 保持容器运行
echo "=== Jupyter Engine 启动完成 ==="
wait $API_PID $JUPYTER_PID
