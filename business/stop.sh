#!/bin/bash
# Business Database Stop Script
# 业务库停止脚本

set -e

# 获取脚本所在目录并切换
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "停止业务库服务..."
docker-compose down

echo "✓ 业务库已停止"
