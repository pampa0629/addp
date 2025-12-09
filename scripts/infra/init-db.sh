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
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

usage() {
  cat <<EOF
用法: bash scripts/infra/init-db.sh [选项]

选项:
  --drop-schema <schema_name>   删除指定 schema 及其所有表
  --drop-all                    删除所有 ADDP schema（慎用！）

示例:
  bash scripts/infra/init-db.sh                        # 正常初始化
  bash scripts/infra/init-db.sh --drop-schema metadata # 重建 metadata schema
  bash scripts/infra/init-db.sh --drop-all             # 清空所有数据
EOF
}

# 定义需要执行的 SQL 初始化文件（按顺序）
SQL_FILES=(
  "${SCRIPT_DIR}/init-db.sql"
)

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
if ! docker compose -f docker-compose.infra.yml ps --status running postgres >/dev/null 2>&1; then
  echo -e "${RED}✗ PostgreSQL 容器未运行，无法初始化数据库${NC}"
  echo -e "${YELLOW}  请先执行: bash scripts/infra/up.sh${NC}"
  exit 1
fi

# 处理参数
if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
elif [[ "${1:-}" == "--drop-schema" ]]; then
  if [[ -z "${2:-}" ]]; then
    echo -e "${RED}✗ 请指定要删除的 schema 名称${NC}"
    usage
    exit 1
  fi
  SCHEMA_NAME="${2}"
  echo -e "${YELLOW}⚠️  警告：即将删除 schema '${SCHEMA_NAME}' 及其所有数据${NC}"
  read -p "确认删除？(yes/no): " confirm
  if [[ "$confirm" != "yes" ]]; then
    echo -e "${YELLOW}✗ 操作已取消${NC}"
    exit 0
  fi
  echo -e "${YELLOW}▶ 删除 schema: ${SCHEMA_NAME}${NC}"
  docker compose -f docker-compose.infra.yml exec -T postgres env PGPASSWORD="${DB_PASSWORD}" \
    psql -U "${DB_USER}" -d "${DB_NAME}" \
    -c "DROP SCHEMA IF EXISTS ${SCHEMA_NAME} CASCADE;"
  echo -e "${GREEN}✓ Schema ${SCHEMA_NAME} 已删除${NC}"
  echo -e "${YELLOW}  重新运行脚本（不带参数）以重建该 schema${NC}"
  exit 0
elif [[ "${1:-}" == "--drop-all" ]]; then
  echo -e "${RED}⚠️  警告：即将删除所有 ADDP schema 及其数据！${NC}"
  echo -e "${YELLOW}  包括: system, manager, metadata, transfer, orchestrator, develop${NC}"
  read -p "确认删除所有数据？(yes/no): " confirm
  if [[ "$confirm" != "yes" ]]; then
    echo -e "${YELLOW}✗ 操作已取消${NC}"
    exit 0
  fi
  echo -e "${YELLOW}▶ 删除所有 ADDP schemas...${NC}"
  for schema in system manager metadata transfer orchestrator develop; do
    echo -e "${BLUE}  ▸ 删除 schema: ${schema}${NC}"
    docker compose -f docker-compose.infra.yml exec -T postgres env PGPASSWORD="${DB_PASSWORD}" \
      psql -U "${DB_USER}" -d "${DB_NAME}" \
      -c "DROP SCHEMA IF EXISTS ${schema} CASCADE;" 2>/dev/null || true
  done
  echo -e "${GREEN}✓ 所有 schemas 已删除${NC}"
  echo -e "${YELLOW}  重新运行脚本（不带参数）以重建所有 schemas${NC}"
  exit 0
fi

# 检查所有 SQL 文件是否存在
for sql_file in "${SQL_FILES[@]}"; do
  if [ ! -f "${sql_file}" ]; then
    echo -e "${RED}✗ 未找到初始化脚本: ${sql_file}${NC}"
    exit 1
  fi
done

echo -e "${YELLOW}▶ 初始化 ADDP 系统库表结构（数据库：${DB_NAME}，用户：${DB_USER}）${NC}"

# 按顺序执行所有 SQL 文件
for sql_file in "${SQL_FILES[@]}"; do
  echo -e "${BLUE}  ▸ 执行: $(basename "${sql_file}")${NC}"

  # 使用 ON_ERROR_STOP 防止忽略 SQL 错误
  if docker compose -f docker-compose.infra.yml exec -T postgres env PGPASSWORD="${DB_PASSWORD}" \
    psql -v "ON_ERROR_STOP=1" -U "${DB_USER}" -d "${DB_NAME}" < "${sql_file}"; then
    echo -e "${GREEN}  ✓ $(basename "${sql_file}") 执行成功${NC}"
  else
    echo -e "${RED}✗ $(basename "${sql_file}") 执行失败${NC}"
    exit 1
  fi
done

echo -e "${GREEN}✓ 数据库初始化完成${NC}"

