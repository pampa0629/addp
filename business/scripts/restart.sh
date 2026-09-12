#!/bin/bash
# 业务库重启脚本
#
# 使用方法:
#   bash scripts/restart.sh              # 重启默认服务 (PostgreSQL + MinIO)
#   bash scripts/restart.sh -mysql       # 只重启 MySQL
#   bash scripts/restart.sh -oceanbase   # 只重启 OceanBase CE
#   bash scripts/restart.sh -tidb        # 只重启 TiDB 8.5.8 三组件
#   bash scripts/restart.sh -opengauss   # 只重启 openGauss
#   bash scripts/restart.sh -kingbase    # 只重启 KingbaseES（必须独立使用）
#   bash scripts/restart.sh -all         # 重启所有服务

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
    [ "$#" -eq 1 ] || { echo "帮助参数必须独立使用" >&2; exit 1; }
    echo "使用方法: bash scripts/restart.sh [start.sh 支持的单个或组合选项]"
    echo "  -kingbase 必须独立使用，且不属于 -all"
    exit 0
fi

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Business Database Restart${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

echo -e "${YELLOW}🛑 停止服务...${NC}"
"${SCRIPT_DIR}/stop.sh" "$@"
echo ""

echo -e "${YELLOW}🚀 启动服务...${NC}"
"${SCRIPT_DIR}/start.sh" "$@"
