#!/bin/bash
# =============================================================================
# ADDP Production Stop Script
# =============================================================================
# Description: Stop ADDP production services
# Usage: ./scripts/prod/stop.sh
# =============================================================================

set -e

if [ "$#" -ne 0 ]; then
    echo "错误: stop.sh 不接受参数；停止平台容器时始终保留数据卷" >&2
    exit 2
fi

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$PROJECT_ROOT"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Production Stop${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

if [ ! -f ".env" ]; then
    echo -e "${RED}错误: 根目录 .env 不存在${NC}"
    exit 1
fi

ROOT_DIR="$PROJECT_ROOT"
source scripts/dev/lifecycle-lock.sh
addp_acquire_lifecycle_lock prod-stop "$@"
(
    set -a
    source .env || exit 1
    set +a
    python3 scripts/utils/deployment-inventory.py service-expectations --state planned_stopped --all
) || { echo '无法发布计划停用，尚未停止服务；请修复部署意图目录后重试' >&2; exit 1; }

echo -e "${YELLOW}Stopping ADDP services...${NC}"
docker compose -f docker-compose.runtimes.yml --env-file .env down
docker compose -f docker-compose.yml --env-file .env down

echo ""
echo -e "${GREEN}✓ ADDP services stopped${NC}"
