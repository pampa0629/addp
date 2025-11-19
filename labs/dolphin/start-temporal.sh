#!/bin/bash
# Temporal 快速启动脚本

set -e

echo "=============================================="
echo "🚀 Temporal + GeoPandas 快速启动"
echo "=============================================="

# 检查 Docker
if ! command -v docker &> /dev/null; then
    echo "❌ Docker 未安装，请先安装 Docker"
    exit 1
fi

# 检查 docker-compose
if ! command -v docker-compose &> /dev/null; then
    echo "❌ docker-compose 未安装，请先安装 docker-compose"
    exit 1
fi

echo ""
echo "📋 步骤 1: 启动 Temporal Server"
echo "----------------------------------------------"
docker-compose -f docker-compose-temporal.yml up -d

echo ""
echo "⏳ 等待 Temporal Server 启动 (30 秒)..."
sleep 30

echo ""
echo "✅ Temporal Server 已启动"
echo "   - Temporal UI: http://localhost:8080"
echo "   - gRPC API: localhost:7233"

echo ""
echo "📋 步骤 2: 安装 Python 依赖"
echo "----------------------------------------------"
cd backend/temporal

if [ ! -d "venv" ]; then
    echo "   创建虚拟环境..."
    python3 -m venv venv
fi

echo "   激活虚拟环境..."
source venv/bin/activate

echo "   安装依赖..."
pip install -q -r requirements.txt

echo "✅ Python 依赖已安装"

echo ""
echo "=============================================="
echo "🎉 启动完成！"
echo "=============================================="
echo ""
echo "下一步:"
echo "  1. 启动 Worker:"
echo "     cd backend/temporal"
echo "     source venv/bin/activate"
echo "     python worker.py"
echo ""
echo "  2. 运行示例 (新终端):"
echo "     cd backend/temporal"
echo "     source venv/bin/activate"
echo "     python examples/run_buffer_workflow.py \\"
echo "       --input ../../data/sample.geojson \\"
echo "       --output ../../output/buffer_result.geojson \\"
echo "       --distance 100"
echo ""
echo "  3. 访问 Temporal UI:"
echo "     open http://localhost:8080"
echo ""
echo "=============================================="
