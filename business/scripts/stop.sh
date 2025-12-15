#!/bin/bash
# Business Database Stop Script
# 业务库停止脚本

set -e

# 获取脚本所在目录并切换到项目根目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

echo "停止业务库服务..."
docker-compose down

echo "✓ 业务库已停止"
