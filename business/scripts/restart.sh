#!/bin/bash
# 业务库重启脚本
#
# 使用方法:
#   bash scripts/restart.sh              # 重启默认服务 (PostgreSQL + MinIO)
#   bash scripts/restart.sh -mysql       # 只重启 MySQL
#   bash scripts/restart.sh -oceanbase   # 只重启 OceanBase CE
#   bash scripts/restart.sh -all         # 重启所有服务

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

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
