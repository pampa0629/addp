#!/bin/bash
#
# Spark 工作流引擎 启动脚本
# 功能: 启动 Spark 工作流引擎 (Flask API Server)
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENGINE_DIR="$SCRIPT_DIR"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "🔧 [Spark Engine] 准备启动 Spark 工作流引擎..."

# 加载环境变量
if [ -f "$ROOT_DIR/.env" ]; then
    echo "📋 [Spark Engine] 加载环境变量: $ROOT_DIR/.env"
    export $(grep -v '^#' "$ROOT_DIR/.env" | xargs)
fi

export PORT="${SPARK_WORKFLOW_PORT:-${PORT:-8098}}"

# 激活虚拟环境 (如果存在)
if [ -d "$ENGINE_DIR/venv" ]; then
    echo "🐍 [Spark Engine] 激活虚拟环境..."
    source "$ENGINE_DIR/venv/bin/activate"
else
    echo "⚠️  [Spark Engine] 虚拟环境不存在，使用系统Python"
    echo "💡 建议运行: python3 -m venv $ENGINE_DIR/venv"
fi

# 检查依赖
echo "📦 [Spark Engine] 检查依赖..."
if ! python3 -c "import pyspark" 2>/dev/null; then
    echo "❌ [Spark Engine] PySpark 未安装"
    echo "💡 运行安装命令: pip install -r $ENGINE_DIR/requirements.txt"
    exit 1
fi

# 启动 Flask API Server
echo "🚀 [Spark Engine] 启动 Spark 工作流引擎..."
cd "$ENGINE_DIR"
python3 api_server.py
