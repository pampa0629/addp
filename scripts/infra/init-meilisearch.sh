#!/usr/bin/env bash

# ADDP Infrastructure Meilisearch Initialization
# 初始化 Meilisearch 索引（按模块组织）
# 由 infra-up.sh 自动调用

set -euo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

# Optional: load env overrides if present
if [ -f ./.env ]; then
  set -a
  # shellcheck disable=SC1091
  source ./.env || true
  set +a
fi

MEILI_URL="${MEILISEARCH_URL_LOCAL:-http://localhost:17700}"
MEILI_KEY="${MEILISEARCH_MASTER_KEY}"

if [ -z "${MEILI_KEY}" ]; then
  echo -e "${RED}✗ MEILISEARCH_MASTER_KEY 未设置${NC}"
  echo -e "${YELLOW}  请在 .env 文件中设置 MEILISEARCH_MASTER_KEY${NC}"
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"
  exit 1
fi

# 确认 Meilisearch 容器正在运行
if ! docker compose -f docker-compose.infra.yml ps --status running meilisearch >/dev/null 2>&1; then
  echo -e "${RED}✗ Meilisearch 容器未运行，无法初始化索引${NC}"
  echo -e "${YELLOW}  请先执行: bash scripts/infra/up.sh${NC}"
  exit 1
fi

echo -e "${YELLOW}========================================${NC}"
echo -e "${YELLOW}Meilisearch 索引初始化（模块化组织）${NC}"
echo -e "${YELLOW}========================================${NC}"
echo ""

echo -e "${YELLOW}▶ 校验 Meilisearch API...${NC}"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer ${MEILI_KEY}" \
  "${MEILI_URL}/health")
if [ "$STATUS" != "200" ]; then
  echo -e "  ${RED}✗ Meilisearch API 不可用（HTTP ${STATUS}）${NC}"
  exit 1
fi
echo -e "  ${GREEN}✓ Meilisearch API 可用${NC}"
echo -e "  ${BLUE}各模块在启动时创建并配置自己的 owner 索引；Infra 不预建跨模块共享索引。${NC}"

echo ""
echo -e "${GREEN}✓ Meilisearch 索引初始化完成${NC}"
echo ""
echo -e "${YELLOW}========================================${NC}"
echo -e "${YELLOW}索引访问信息${NC}"
echo -e "${YELLOW}========================================${NC}"
echo "  访问地址: ${MEILI_URL}"
echo "  Master Key: ${MEILI_KEY}"
echo ""
