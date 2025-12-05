#!/usr/bin/env bash

# ADDP Infrastructure - Pull Correct Images Based on CPU Architecture
# 根据 CPU 架构自动拉取正确的 Docker 镜像

set -euo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Infrastructure - Pull Images${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Load environment variables
if [ -f ./.env ]; then
  set -a
  # shellcheck disable=SC1091
  source ./.env || true
  set +a
fi

# Detect CPU architecture
ARCH=$(uname -m)
echo -e "${YELLOW}🔍 检测到 CPU 架构: ${ARCH}${NC}"

# Select PostgreSQL image based on architecture
case "${ARCH}" in
    x86_64)
        POSTGRES_IMAGE="postgis/postgis:15-3.4"
        ARCH_NAME="AMD64"
        ;;
    aarch64|arm64)
        POSTGRES_IMAGE="imresamu/postgis-arm64:15-3.4"
        ARCH_NAME="ARM64"
        ;;
    armv7l)
        POSTGRES_IMAGE="postgis/postgis:15-3.4"
        ARCH_NAME="ARMv7 (使用 AMD64 镜像)"
        ;;
    *)
        echo -e "${RED}✗ 不支持的架构: ${ARCH}${NC}"
        echo -e "${YELLOW}支持的架构: x86_64 (AMD64), arm64/aarch64 (ARM64)${NC}"
        exit 1
        ;;
esac

echo -e "${GREEN}✓ 架构: ${ARCH_NAME}${NC}"
echo -e "${GREEN}✓ 选择 PostgreSQL 镜像: ${POSTGRES_IMAGE}${NC}"
echo ""

# Export for docker-compose
export POSTGRES_IMAGE

# List of images to pull
echo -e "${YELLOW}▶ 拉取基础设施镜像...${NC}"
echo ""

# 1. PostgreSQL with PostGIS
echo -e "${BLUE}[1/4] PostgreSQL + PostGIS${NC}"
if docker pull "${POSTGRES_IMAGE}"; then
    echo -e "${GREEN}✓ ${POSTGRES_IMAGE} 拉取成功${NC}"
else
    echo -e "${RED}✗ ${POSTGRES_IMAGE} 拉取失败${NC}"
    exit 1
fi
echo ""

# 2. Redis
echo -e "${BLUE}[2/4] Redis${NC}"
REDIS_IMAGE="redis:6.2.19-alpine"
if docker pull "${REDIS_IMAGE}"; then
    echo -e "${GREEN}✓ ${REDIS_IMAGE} 拉取成功${NC}"
else
    echo -e "${RED}✗ ${REDIS_IMAGE} 拉取失败${NC}"
    exit 1
fi
echo ""

# 3. MinIO
echo -e "${BLUE}[3/4] MinIO${NC}"
MINIO_IMAGE="minio/minio:latest"
if docker pull "${MINIO_IMAGE}"; then
    echo -e "${GREEN}✓ ${MINIO_IMAGE} 拉取成功${NC}"
else
    echo -e "${RED}✗ ${MINIO_IMAGE} 拉取失败${NC}"
    exit 1
fi
echo ""

# 4. Meilisearch
echo -e "${BLUE}[4/4] Meilisearch${NC}"
MEILI_IMAGE="getmeili/meilisearch:v1.7"
if docker pull "${MEILI_IMAGE}"; then
    echo -e "${GREEN}✓ ${MEILI_IMAGE} 拉取成功${NC}"
else
    echo -e "${RED}✗ ${MEILI_IMAGE} 拉取失败${NC}"
    exit 1
fi
echo ""

# Summary
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ 所有镜像拉取完成!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "已拉取的镜像:"
echo "  - PostgreSQL: ${POSTGRES_IMAGE}"
echo "  - Redis:      ${REDIS_IMAGE}"
echo "  - MinIO:      ${MINIO_IMAGE}"
echo "  - Meilisearch: ${MEILI_IMAGE}"
echo ""
echo -e "${YELLOW}提示: 使用 'bash scripts/infra/restart.sh' 重启基础设施以应用新镜像${NC}"
