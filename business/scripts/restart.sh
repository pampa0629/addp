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
echo -e "${YELLOW}🧹 Cleaning up old architecture images...${NC}"

# Check if postgis image exists and remove if architecture mismatch
if docker image inspect postgis/postgis:15-3.4 &>/dev/null; then
    OLD_ARCH=$(docker image inspect postgis/postgis:15-3.4 2>/dev/null | grep -m1 "Architecture" | awk -F'"' '{print $4}')
    if [ "${OLD_ARCH}" != "${DOCKER_ARCH##*/}" ]; then
        echo -e "   Removing old PostGIS image (${OLD_ARCH})..."
        docker rmi -f postgis/postgis:15-3.4 || true
    fi
fi

# Check if minio image exists and remove if architecture mismatch (optional, usually fine)
if docker image inspect minio/minio:latest &>/dev/null; then
    OLD_MINIO_ARCH=$(docker image inspect minio/minio:latest 2>/dev/null | grep -m1 "Architecture" | awk -F'"' '{print $4}')
    if [ "${OLD_MINIO_ARCH}" != "${DOCKER_ARCH##*/}" ]; then
        echo -e "   Removing old MinIO image (${OLD_MINIO_ARCH})..."
        docker rmi -f minio/minio:latest || true
    fi
fi

echo -e "${GREEN}✓ Cleanup complete${NC}"
echo ""

# Pull the correct architecture images
echo -e "${YELLOW}📥 Pulling images for ${DISPLAY_ARCH} architecture...${NC}"
export DOCKER_DEFAULT_PLATFORM="${DOCKER_ARCH}"

# Pull PostGIS image for the correct architecture
echo -e "   Pulling postgis/postgis:15-3.4 for ${DOCKER_ARCH}..."
docker pull --platform="${DOCKER_ARCH}" postgis/postgis:15-3.4

# Pull MinIO image (multi-arch, but explicit is safer)
echo -e "   Pulling minio/minio:latest for ${DOCKER_ARCH}..."
docker pull --platform="${DOCKER_ARCH}" minio/minio:latest

echo -e "${GREEN}✓ Images pulled successfully${NC}"
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
echo ""
echo "Verify with:"
echo "  docker image inspect postgis/postgis:15-3.4 | grep Architecture"
echo "  docker inspect business-postgres | grep Architecture"
echo ""

