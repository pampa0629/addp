#!/usr/bin/env bash

# ADDP Infrastructure Database Initialization
# 将系统库（system/manager/meta/gateway/transfer 等）表结构初始化逻辑
# 整合到 infrastructure 脚本，用于 infra-up/infra-restart 之后自动执行。

set -euo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

SQL_FILE="${PROJECT_ROOT}/scripts/init-db.sql"

if [ ! -f "${SQL_FILE}" ]; then
  echo -e "${RED}✗ 未找到初始化脚本: ${SQL_FILE}${NC}"
  exit 1
fi

# Optional: load env overrides if present
if [ -f ./.env ]; then
  set -a
  # shellcheck disable=SC1091
  source ./.env || true
  set +a
fi

DB_USER="${POSTGRES_USER:-${DB_USER:-addp}}"
DB_PASSWORD="${POSTGRES_PASSWORD:-${DB_PASSWORD:-addp_password}}"
DB_NAME="${POSTGRES_DB:-${DB_NAME:-addp}}"

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"
  exit 1
fi

# 确认 PostgreSQL 容器正在运行
if ! docker compose ps --status running postgres >/dev/null 2>&1; then
  echo -e "${RED}✗ PostgreSQL 容器未运行，无法初始化数据库${NC}"
  echo -e "${YELLOW}  请先执行: bash scripts/infra-up.sh${NC}"
  exit 1
fi

echo -e "${YELLOW}▶ 初始化 ADDP 系统库表结构（数据库：${DB_NAME}，用户：${DB_USER}）${NC}"

# 使用 ON_ERROR_STOP 防止忽略 SQL 错误
if docker compose exec -T postgres env PGPASSWORD="${DB_PASSWORD}" \
  psql -v "ON_ERROR_STOP=1" -U "${DB_USER}" -d "${DB_NAME}" < "${SQL_FILE}"; then
  echo -e "${GREEN}✓ 数据库初始化完成${NC}"
else
  echo -e "${RED}✗ 数据库初始化失败${NC}"
  exit 1
fi

