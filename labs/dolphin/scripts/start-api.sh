#!/bin/bash

# 启动空间算子 API 服务（本地开发模式）

set -e

echo "=========================================="
echo "启动空间算子 API 服务"
echo "=========================================="
echo ""

# 检查是否在 backend 目录
if [ ! -f "api_server.py" ]; then
    if [ -f "backend/api_server.py" ]; then
        cd backend
    else
        echo "❌ 错误: 找不到 api_server.py"
        echo "   请在项目根目录或 backend 目录运行此脚本"
        exit 1
    fi
fi

# 检查 Python
if ! command -v python3 &> /dev/null; then
    echo "❌ 错误: Python 3 未安装"
    exit 1
fi

echo "✅ Python 版本: $(python3 --version)"
echo ""

# 检查依赖
echo "📦 检查依赖..."
if ! python3 -c "import flask" 2>/dev/null; then
    echo "⚠️  Flask 未安装，正在安装..."
    pip3 install flask
fi

if ! python3 -c "import shapely" 2>/dev/null; then
    echo "⚠️  Shapely 未安装，正在安装..."
    pip3 install shapely==2.0.2 'numpy<2'
fi

echo "✅ 所有依赖已安装"
echo ""

# 启动服务
echo "🚀 启动 API 服务..."
echo "   访问地址: http://localhost:5001"
echo "   健康检查: http://localhost:5001/health"
echo "   算子列表: http://localhost:5001/operators"
echo "   (注: 默认使用 5001 端口，避免 macOS AirPlay 占用 5000)"
echo ""
echo "按 Ctrl+C 停止服务"
echo ""

python3 api_server.py
