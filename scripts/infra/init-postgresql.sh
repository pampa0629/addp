#!/usr/bin/env bash

# ADDP PostgreSQL 一站式初始化脚本
# Unified PostgreSQL Initialization Script
#
# 功能:
# 1. 配置 pg_hba.conf (允许外部连接，解决 DBeaver 等客户端连接问题)
# 2. 安装扩展 (PostGIS 空间数据支持, pgvector 向量检索支持)
# 3. 创建 schema 和表结构 (system, manager, meta, transfer, orchestrator, develop, service)
#
# 调用: 由 scripts/infra/up.sh 自动调用

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
用法: bash scripts/infra/init-postgresql.sh [选项]

选项:
  --drop-schema <schema_name>   删除指定 schema 及其所有表
  --drop-all                    删除所有 ADDP schema（慎用！）
  --skip-extensions             跳过扩展安装
  --skip-pgvector               仅跳过 pgvector 扩展（保留 PostGIS）
  --skip-hba                    跳过 pg_hba.conf 配置

示例:
  bash scripts/infra/init-postgresql.sh                        # 正常初始化
  bash scripts/infra/init-postgresql.sh --drop-schema meta # 重建 meta schema
  bash scripts/infra/init-postgresql.sh --drop-all             # 清空所有数据
EOF
}

echo -e "${YELLOW}========================================${NC}"
echo -e "${YELLOW}PostgreSQL 一站式初始化${NC}"
echo -e "${YELLOW}========================================${NC}"
echo ""

# Load environment variables
if [ -f ./.env ]; then
    set -a
    # shellcheck disable=SC1091
    source ./.env || true
    set +a
fi

DB_USER="${POSTGRES_USER:-addp}"
DB_PASSWORD="${POSTGRES_PASSWORD:-addp_password}"
DB_NAME="${POSTGRES_DB:-addp}"
CONTAINER_NAME="addp-postgres"

SKIP_EXTENSIONS=false
SKIP_HBA=false
SKIP_PGVECTOR="${SKIP_PGVECTOR:-false}"  # 支持通过环境变量跳过 pgvector 编译

# ==================== 参数处理 ====================

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
  echo -e "${YELLOW}  包括: system, manager, meta, transfer, orchestrator, develop, service${NC}"
  read -p "确认删除所有数据？(yes/no): " confirm
  if [[ "$confirm" != "yes" ]]; then
    echo -e "${YELLOW}✗ 操作已取消${NC}"
    exit 0
  fi
  echo -e "${YELLOW}▶ 删除所有 ADDP schemas...${NC}"
  for schema in system manager meta transfer orchestrator develop service; do
    echo -e "${BLUE}  ▸ 删除 schema: ${schema}${NC}"
    docker compose -f docker-compose.infra.yml exec -T postgres env PGPASSWORD="${DB_PASSWORD}" \
      psql -U "${DB_USER}" -d "${DB_NAME}" \
      -c "DROP SCHEMA IF EXISTS ${schema} CASCADE;" 2>/dev/null || true
  done
  echo -e "${GREEN}✓ 所有 schemas 已删除${NC}"
  echo -e "${YELLOW}  重新运行脚本（不带参数）以重建所有 schemas${NC}"
  exit 0
elif [[ "${1:-}" == "--skip-extensions" ]]; then
  SKIP_EXTENSIONS=true
elif [[ "${1:-}" == "--skip-pgvector" ]]; then
  SKIP_PGVECTOR=true
elif [[ "${1:-}" == "--skip-hba" ]]; then
  SKIP_HBA=true
fi

# ==================== 前置检查 ====================

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"
  exit 1
fi

# 确认 PostgreSQL 容器正在运行（精确匹配，避免匹配到 business-postgres）
if ! docker ps --filter name="^${CONTAINER_NAME}$" --filter status=running --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo -e "${RED}✗ PostgreSQL 容器未运行${NC}"
    echo -e "${YELLOW}  请先执行: bash scripts/infra/up.sh${NC}"
    exit 1
fi

echo -e "${GREEN}✓ PostgreSQL 容器运行中${NC}"
echo ""

# ==================== 步骤 1: 配置 pg_hba.conf ====================

if [ "$SKIP_HBA" = false ]; then
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}步骤 1/3: 配置客户端认证 (pg_hba.conf)${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo ""

    HBA_CONFIG_FILE="${SCRIPT_DIR}/init-postgresql-hba.conf"

    if [ -f "${HBA_CONFIG_FILE}" ]; then
        echo -e "${YELLOW}▶ 更新 pg_hba.conf 配置...${NC}"

        # 备份原配置
        docker exec "${CONTAINER_NAME}" cp /var/lib/postgresql/data/pgdata/pg_hba.conf /var/lib/postgresql/data/pgdata/pg_hba.conf.bak 2>/dev/null || true

        # 复制新配置
        cat "${HBA_CONFIG_FILE}" | docker exec -i "${CONTAINER_NAME}" bash -c "cat > /var/lib/postgresql/data/pgdata/pg_hba.conf && chmod 600 /var/lib/postgresql/data/pgdata/pg_hba.conf"

        # 重新加载配置
        docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -c "SELECT pg_reload_conf();" >/dev/null 2>&1

        echo -e "${GREEN}✓ pg_hba.conf 配置完成${NC}"
        echo -e "${GREEN}  现在支持 DBeaver/pgAdmin 等外部工具连接${NC}"
    else
        echo -e "${YELLOW}⚠  未找到 ${HBA_CONFIG_FILE}，跳过 pg_hba.conf 配置${NC}"
    fi

    echo ""
else
    echo -e "${YELLOW}⚠  跳过 pg_hba.conf 配置 (--skip-hba)${NC}"
    echo ""
fi

# ==================== 步骤 2: 安装扩展 ====================

if [ "$SKIP_EXTENSIONS" = false ]; then
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}步骤 2/3: 安装 PostgreSQL 扩展${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo ""

    # ==================== PostGIS Extension ====================

    echo -e "${YELLOW}▶ 安装 PostGIS 扩展（空间数据支持）${NC}"

    POSTGIS_CHECK=$(docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -t -c "SELECT COUNT(*) FROM pg_extension WHERE extname='postgis';" 2>/dev/null | tr -d '[:space:]' || echo "0")

    if [ "${POSTGIS_CHECK}" = "1" ]; then
        POSTGIS_VERSION=$(docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -t -c "SELECT PostGIS_Version();" 2>/dev/null | head -n1 | xargs || echo "Unknown")
        echo -e "  ${GREEN}✓ PostGIS 已安装 (${POSTGIS_VERSION})${NC}"
    else
        echo -e "  ${YELLOW}PostGIS 未安装，开始安装...${NC}"

        # Check if PostGIS packages are installed
        POSTGIS_PACKAGE_CHECK=$(docker exec "${CONTAINER_NAME}" sh -c 'dpkg -l | grep postgis || echo "not-installed"')

        if echo "${POSTGIS_PACKAGE_CHECK}" | grep -q "not-installed"; then
            echo -e "  ${BLUE}安装 PostGIS 包...${NC}"
            docker exec "${CONTAINER_NAME}" sh -c '
                apt-get update -qq && \
                apt-get install -y -qq --no-install-recommends \
                    postgresql-15-postgis-3 \
                    postgis > /dev/null 2>&1
            '
            echo -e "  ${GREEN}✓ PostGIS 包安装完成${NC}"
        fi

        # Create extension
        docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -c "CREATE EXTENSION IF NOT EXISTS postgis;" >/dev/null 2>&1
        echo -e "  ${GREEN}✓ PostGIS 扩展创建完成${NC}"
    fi

    # Install PostGIS Topology extension
    TOPOLOGY_CHECK=$(docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -t -c "SELECT COUNT(*) FROM pg_extension WHERE extname='postgis_topology';" 2>/dev/null | tr -d '[:space:]' || echo "0")

    if [ "${TOPOLOGY_CHECK}" = "1" ]; then
        echo -e "  ${GREEN}✓ PostGIS Topology 已安装${NC}"
    else
        docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -c "CREATE EXTENSION IF NOT EXISTS postgis_topology;" >/dev/null 2>&1 || true
        echo -e "  ${GREEN}✓ PostGIS Topology 安装完成${NC}"
    fi

    echo ""

    # ==================== pgvector Extension ====================

    if [ "$SKIP_PGVECTOR" = false ]; then
        echo -e "${YELLOW}▶ 安装 pgvector 扩展（向量检索支持）${NC}"

        # Check if pgvector extension exists in manager schema
        PGVECTOR_CHECK=$(docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -t -c "SELECT COUNT(*) FROM pg_extension e JOIN pg_namespace n ON e.extnamespace = n.oid WHERE e.extname='vector' AND n.nspname='manager';" 2>/dev/null | tr -d '[:space:]' || echo "0")

        if [ "${PGVECTOR_CHECK}" = "1" ]; then
            PGVECTOR_VERSION=$(docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -t -c "SELECT extversion FROM pg_extension WHERE extname='vector';" 2>/dev/null | tr -d '[:space:]' || echo "Unknown")
            echo -e "  ${GREEN}✓ pgvector 已安装在 manager schema (${PGVECTOR_VERSION})${NC}"
        else
            echo -e "  ${YELLOW}pgvector 未安装，开始安装...${NC}"

            # Check if pgvector packages are installed
            PGVECTOR_PACKAGE_CHECK=$(docker exec "${CONTAINER_NAME}" sh -c 'dpkg -l | grep pgvector || echo "not-installed"')

            if echo "${PGVECTOR_PACKAGE_CHECK}" | grep -q "not-installed"; then
                echo -e "  ${BLUE}编译安装 pgvector (v0.7.0)...${NC}"
                docker exec "${CONTAINER_NAME}" sh -c '
                    set -e
                    apt-get update -qq
                    apt-get install -y -qq --no-install-recommends \
                        build-essential \
                        git \
                        postgresql-server-dev-15 \
                        ca-certificates > /dev/null 2>&1

                    cd /tmp
                    rm -rf pgvector || true
                    git clone --branch v0.7.0 https://github.com/pgvector/pgvector.git > /dev/null 2>&1
                    cd pgvector
                    make clean > /dev/null 2>&1 || true
                    make -j$(nproc) > /dev/null 2>&1
                    make install > /dev/null 2>&1

                    cd /tmp
                    rm -rf pgvector
                    apt-get remove -y --purge build-essential git > /dev/null 2>&1
                    apt-get autoremove -y > /dev/null 2>&1
                    apt-get clean > /dev/null 2>&1
                    rm -rf /var/lib/apt/lists/*
                ' >/dev/null 2>&1
                echo -e "  ${GREEN}✓ pgvector 包安装完成${NC}"
            fi

            # Create extension in manager schema (not in public schema)
            # Note: The extension is created in init-postgresql.sql with schema specification
            echo -e "  ${GREEN}✓ pgvector 包安装完成（扩展将由 init-postgresql.sql 创建）${NC}"
        fi
    else
        echo -e "${YELLOW}⚠  跳过 pgvector 扩展安装 (SKIP_PGVECTOR=true)${NC}"
    fi

    echo ""
    echo -e "${GREEN}✓ PostgreSQL 扩展安装完成${NC}"
    echo ""

    # Show installed extensions
    echo -e "${YELLOW}已安装的扩展:${NC}"
    docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -c "\dx" 2>/dev/null

    echo ""
else
    echo -e "${YELLOW}⚠  跳过扩展安装 (--skip-extensions)${NC}"
    echo ""
fi

# ==================== 步骤 3: 创建 Schema 和表结构 ====================

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}步骤 3/3: 创建 Schema 和表结构${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

SQL_FILE="${SCRIPT_DIR}/init-postgresql.sql"

if [ ! -f "${SQL_FILE}" ]; then
    echo -e "${RED}✗ 未找到初始化脚本: ${SQL_FILE}${NC}"
    exit 1
fi

echo -e "${YELLOW}▶ 初始化 ADDP 系统库表结构（数据库：${DB_NAME}，用户：${DB_USER}）${NC}"
echo -e "${BLUE}  ▸ 执行: $(basename "${SQL_FILE}")${NC}"

# 使用 ON_ERROR_STOP 防止忽略 SQL 错误
if docker compose -f docker-compose.infra.yml exec -T postgres env PGPASSWORD="${DB_PASSWORD}" \
  psql -v "ON_ERROR_STOP=1" -U "${DB_USER}" -d "${DB_NAME}" < "${SQL_FILE}"; then
  echo -e "${GREEN}  ✓ $(basename "${SQL_FILE}") 执行成功${NC}"
else
  echo -e "${RED}✗ $(basename "${SQL_FILE}") 执行失败${NC}"
  exit 1
fi

echo ""
echo -e "${GREEN}✓ 数据库表结构初始化完成${NC}"
echo ""

# ==================== 完成总结 ====================

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}PostgreSQL 初始化完成 ✓${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${GREEN}ADDP 系统数据库现已支持:${NC}"
echo -e "  ✓ ${GREEN}外部客户端连接${NC} (DBeaver, pgAdmin, DataGrip)"
echo -e "  ✓ ${GREEN}空间数据操作${NC} (PostGIS + Topology)"
echo -e "  ✓ ${GREEN}向量嵌入与相似度搜索${NC} (pgvector)"
echo -e "  ✓ ${GREEN}完整的模块 Schema${NC} (system, manager, meta, transfer, orchestrator, develop, service)"
echo ""
echo -e "${YELLOW}连接信息:${NC}"
echo -e "  Host: localhost"
echo -e "  Port: 5432"
echo -e "  Database: ${DB_NAME}"
echo -e "  Username: ${DB_USER}"
echo -e "  Password: ${DB_PASSWORD}"
echo ""
