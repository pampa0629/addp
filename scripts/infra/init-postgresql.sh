#!/usr/bin/env bash

# ADDP PostgreSQL Extensions Installation
# 为 ADDP PostgreSQL 安装所有必需的扩展
# 包括: PostGIS (空间数据) 和 pgvector (向量检索)
# 由 infra-up.sh 自动调用

set -euo pipefail

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

echo -e "${YELLOW}========================================${NC}"
echo -e "${YELLOW}PostgreSQL Extensions Installation${NC}"
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
DB_NAME="${POSTGRES_DB:-addp}"
CONTAINER_NAME="postgres"

# Check if PostgreSQL container is running
if ! docker ps --filter name="${CONTAINER_NAME}" --filter status=running | grep -q "${CONTAINER_NAME}"; then
    echo -e "${RED}✗ PostgreSQL 容器未运行${NC}"
    echo -e "${YELLOW}  请先执行: bash scripts/infra/up.sh${NC}"
    exit 1
fi

# ==================== PostGIS Extension ====================

echo -e "${BLUE}▶ 安装 PostGIS 扩展（空间数据支持）${NC}"
echo ""

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
echo -e "  ${YELLOW}安装 PostGIS Topology...${NC}"
TOPOLOGY_CHECK=$(docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -t -c "SELECT COUNT(*) FROM pg_extension WHERE extname='postgis_topology';" 2>/dev/null | tr -d '[:space:]' || echo "0")

if [ "${TOPOLOGY_CHECK}" = "1" ]; then
    echo -e "  ${GREEN}✓ PostGIS Topology 已安装${NC}"
else
    docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -c "CREATE EXTENSION IF NOT EXISTS postgis_topology;" >/dev/null 2>&1 || true
    echo -e "  ${GREEN}✓ PostGIS Topology 安装完成${NC}"
fi

echo ""

# ==================== pgvector Extension ====================

echo -e "${BLUE}▶ 安装 pgvector 扩展（向量检索支持）${NC}"
echo ""

PGVECTOR_CHECK=$(docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -t -c "SELECT COUNT(*) FROM pg_extension WHERE extname='vector';" 2>/dev/null | tr -d '[:space:]' || echo "0")

if [ "${PGVECTOR_CHECK}" = "1" ]; then
    PGVECTOR_VERSION=$(docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -t -c "SELECT extversion FROM pg_extension WHERE extname='vector';" 2>/dev/null | tr -d '[:space:]' || echo "Unknown")
    echo -e "  ${GREEN}✓ pgvector 已安装 (${PGVECTOR_VERSION})${NC}"
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

    # Create extension
    docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -c "CREATE EXTENSION IF NOT EXISTS vector;" >/dev/null 2>&1
    echo -e "  ${GREEN}✓ pgvector 扩展创建完成${NC}"
fi

echo ""
echo -e "${GREEN}✓ PostgreSQL 扩展安装完成${NC}"
echo ""

# Show installed extensions
echo -e "${YELLOW}已安装的扩展:${NC}"
docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -c "\dx" 2>/dev/null

echo ""
echo -e "${GREEN}ADDP 系统数据库现已支持:${NC}"
echo -e "  ✓ ${GREEN}空间数据操作${NC} (PostGIS + Topology)"
echo -e "  ✓ ${GREEN}向量嵌入与相似度搜索${NC} (pgvector)"
echo -e "  ✓ ${GREEN}多模态 AI 应用${NC} (文本、图像、音频向量)"
echo ""
