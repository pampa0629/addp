#!/usr/bin/env bash

# ADDP Infrastructure Restart Script with Architecture Detection
# 自动检测 CPU 架构并使用最优 PostgreSQL/PostGIS 镜像

set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  ADDP Infrastructure Restart${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Detect CPU architecture
ARCH=$(uname -m)
echo -e "${YELLOW}🔍 Detecting CPU architecture: ${ARCH}${NC}"

# Map architecture names
case "${ARCH}" in
    x86_64)
        DOCKER_ARCH="linux/amd64"
        DISPLAY_ARCH="AMD64"
        ;;
    aarch64|arm64)
        DOCKER_ARCH="linux/arm64"
        DISPLAY_ARCH="ARM64"
        ;;
    armv7l)
        DOCKER_ARCH="linux/arm/v7"
        DISPLAY_ARCH="ARMv7"
        ;;
    *)
        echo -e "${RED}✗ Unsupported architecture: ${ARCH}${NC}"
        exit 1
        ;;
esac

echo -e "${GREEN}✓ Target platform: ${DISPLAY_ARCH} (${DOCKER_ARCH})${NC}"
echo ""

# Stop services
echo -e "${YELLOW}🛑 Stopping infrastructure services...${NC}"
bash scripts/infra-down.sh || true
echo ""

# Clean up old images
echo -e "${YELLOW}🧹 Cleaning up old images...${NC}"

# Remove PostGIS images
if docker image inspect postgis/postgis:15-3.4 &>/dev/null; then
    echo -e "   Removing old PostGIS image..."
    docker rmi -f postgis/postgis:15-3.4 || true
fi

# Remove postgres:15 if exists (will re-pull with correct arch)
if docker image inspect postgres:15 &>/dev/null; then
    echo -e "   Removing old PostgreSQL 15 image..."
    docker rmi -f postgres:15 || true
fi

# Prune dangling images
docker image prune -f &>/dev/null || true

echo -e "${GREEN}✓ Cleanup complete${NC}"
echo ""

# Pull the correct architecture images
echo -e "${YELLOW}📥 Pulling images for ${DISPLAY_ARCH} architecture...${NC}"
export DOCKER_DEFAULT_PLATFORM="${DOCKER_ARCH}"

# ARM64: Use standard PostgreSQL + PostGIS extension (installed via script)
# AMD64: Use postgis/postgis:15-3.4 directly
if [ "${DOCKER_ARCH}" = "linux/arm64" ]; then
    POSTGRES_IMAGE="postgres:15"
    echo -e "   ARM64 detected: Using ${POSTGRES_IMAGE} (PostGIS will be installed as extension)"
else
    POSTGRES_IMAGE="postgis/postgis:15-3.4"
    echo -e "   AMD64 detected: Using ${POSTGRES_IMAGE} (PostGIS pre-installed)"
fi

export POSTGRES_IMAGE

# Pull PostgreSQL image for the correct architecture with retries
echo -e "   Pulling ${POSTGRES_IMAGE} for ${DOCKER_ARCH}..."
MAX_RETRIES=3
RETRY_COUNT=0

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if docker pull --platform="${DOCKER_ARCH}" "${POSTGRES_IMAGE}"; then
        break
    else
        RETRY_COUNT=$((RETRY_COUNT + 1))
        if [ $RETRY_COUNT -lt $MAX_RETRIES ]; then
            echo -e "${YELLOW}   Retry $RETRY_COUNT/$MAX_RETRIES...${NC}"
            sleep 2
        else
            echo -e "${RED}✗ Failed to pull PostgreSQL image after $MAX_RETRIES attempts${NC}"
            exit 1
        fi
    fi
done

# Verify the pulled image has correct architecture
PG_ARCH=$(docker image inspect "${POSTGRES_IMAGE}" --format '{{.Architecture}}' 2>/dev/null || echo "unknown")

echo -e "${GREEN}✓ Image pulled successfully${NC}"
echo -e "   PostgreSQL (${POSTGRES_IMAGE}): ${PG_ARCH}"

if [ "${PG_ARCH}" != "${DOCKER_ARCH##*/}" ]; then
    echo -e "${RED}✗ PostgreSQL architecture mismatch! Expected ${DOCKER_ARCH##*/}, got ${PG_ARCH}${NC}"
    exit 1
fi

echo ""

# Start infrastructure
echo -e "${YELLOW}🚀 Starting infrastructure with correct architecture...${NC}"

# Export platform for docker-compose
export DOCKER_DEFAULT_PLATFORM="${DOCKER_ARCH}"

bash scripts/infra-up.sh

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Restart Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "Architecture: ${GREEN}${DISPLAY_ARCH}${NC} (${DOCKER_ARCH})"
echo -e "PostgreSQL Image: ${GREEN}${POSTGRES_IMAGE}${NC}"
echo ""
echo "Verify with:"
echo "  docker image inspect ${POSTGRES_IMAGE} | grep Architecture"
echo "  docker inspect addp-postgres | grep Architecture"
echo ""
if [ "${DOCKER_ARCH}" = "linux/arm64" ]; then
    echo -e "${YELLOW}Note: PostGIS extension will be installed via infra-init-postgis.sh script${NC}"
    echo ""
fi

