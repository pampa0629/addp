#!/usr/bin/env bash

set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}/.."

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Business Infrastructure Restart${NC}"
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

# Check current running containers
if docker ps --filter "name=business-postgres" --format "{{.Image}}" | grep -q "postgis"; then
    CURRENT_IMAGE=$(docker ps --filter "name=business-postgres" --format "{{.Image}}")
    CURRENT_ARCH=$(docker image inspect "${CURRENT_IMAGE}" 2>/dev/null | grep -m1 "Architecture" | awk -F'"' '{print $4}')
    echo -e "${YELLOW}📦 Current PostgreSQL image: ${CURRENT_IMAGE} (${CURRENT_ARCH})${NC}"

    if [ "${CURRENT_ARCH}" != "${DOCKER_ARCH##*/}" ]; then
        echo -e "${YELLOW}⚠️  Architecture mismatch detected!${NC}"
        echo -e "${YELLOW}   Current: ${CURRENT_ARCH}, Target: ${DOCKER_ARCH##*/}${NC}"
        echo -e "${YELLOW}   Will pull and restart with correct architecture...${NC}"
    else
        echo -e "${GREEN}✓ Architecture matches, proceeding with restart...${NC}"
    fi
else
    echo -e "${YELLOW}ℹ️  No business-postgres container found${NC}"
fi
echo ""

# Stop services first
echo -e "${YELLOW}🛑 Stopping services...${NC}"
bash ./scripts/stop.sh || true
echo ""

# Remove old architecture images if they exist
echo -e "${YELLOW}🧹 Cleaning up old images...${NC}"

# Remove PostGIS image (we'll use postgres:15 for ARM64)
if docker image inspect postgis/postgis:15-3.4 &>/dev/null; then
    echo -e "   Removing old PostGIS image..."
    docker rmi -f postgis/postgis:15-3.4 || true
fi

if docker image inspect postgis/postgis:16-3.4 &>/dev/null; then
    echo -e "   Removing PostGIS 16-3.4 image..."
    docker rmi -f postgis/postgis:16-3.4 || true
fi

# Remove postgres:15 if exists (will re-pull with correct arch)
if docker image inspect postgres:15 &>/dev/null; then
    echo -e "   Removing old PostgreSQL 15 image..."
    docker rmi -f postgres:15 || true
fi

# Remove MinIO image (will re-pull with correct arch)
if docker image inspect minio/minio:latest &>/dev/null; then
    echo -e "   Removing old MinIO image..."
    docker rmi -f minio/minio:latest || true
fi

# Prune dangling images
docker image prune -f &>/dev/null || true

echo -e "${GREEN}✓ Cleanup complete${NC}"
echo ""

# Pull the correct architecture images
echo -e "${YELLOW}📥 Pulling images for ${DISPLAY_ARCH} architecture...${NC}"
export DOCKER_DEFAULT_PLATFORM="${DOCKER_ARCH}"

# Use PostGIS official image for all architectures (supports ARM64 and AMD64)
POSTGRES_IMAGE="postgis/postgis:15-3.4"
echo -e "   Using ${POSTGRES_IMAGE} (PostGIS pre-installed, multi-arch support)"

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

# Pull MinIO image (multi-arch, but explicit is safer)
echo -e "   Pulling minio/minio:latest for ${DOCKER_ARCH}..."
docker pull --platform="${DOCKER_ARCH}" minio/minio:latest

# Verify the pulled images have correct architecture
PG_ARCH=$(docker image inspect "${POSTGRES_IMAGE}" --format '{{.Architecture}}' 2>/dev/null || echo "unknown")
MINIO_ARCH=$(docker image inspect minio/minio:latest --format '{{.Architecture}}' 2>/dev/null || echo "unknown")

echo -e "${GREEN}✓ Images pulled successfully${NC}"
echo -e "   PostgreSQL (${POSTGRES_IMAGE}): ${PG_ARCH}"
echo -e "   MinIO: ${MINIO_ARCH}"

if [ "${PG_ARCH}" != "${DOCKER_ARCH##*/}" ]; then
    echo -e "${RED}✗ PostgreSQL architecture mismatch! Expected ${DOCKER_ARCH##*/}, got ${PG_ARCH}${NC}"
    exit 1
fi

echo ""

echo -e "${YELLOW}🚀 Starting services with correct architecture...${NC}"

# Export platform for docker-compose
export DOCKER_DEFAULT_PLATFORM="${DOCKER_ARCH}"

# Start services
bash ./scripts/start.sh

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Restart Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "Architecture: ${GREEN}${DISPLAY_ARCH}${NC} (${DOCKER_ARCH})"
echo -e "PostgreSQL Image: ${GREEN}${POSTGRES_IMAGE}${NC}"
echo ""
echo "Verify with:"
echo "  docker exec business-postgres psql -U business -d business -c 'SELECT PostGIS_Version();'"
echo "  docker inspect business-postgres | grep Architecture"
echo ""

