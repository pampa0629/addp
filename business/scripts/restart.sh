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

# Select PostgreSQL image based on architecture
case "${ARCH}" in
    aarch64|arm64)
        POSTGRES_IMAGE="imresamu/postgis-arm64:15-3.4"
        DISPLAY_ARCH="ARM64"
        echo -e "${GREEN}✓ Using ARM64 PostGIS image: ${POSTGRES_IMAGE}${NC}"
        ;;
    x86_64)
        POSTGRES_IMAGE="postgis/postgis:15-3.4"
        DISPLAY_ARCH="AMD64"
        echo -e "${GREEN}✓ Using AMD64 PostGIS image: ${POSTGRES_IMAGE}${NC}"
        ;;
    *)
        echo -e "${RED}✗ Unsupported architecture: ${ARCH}${NC}"
        echo -e "${YELLOW}Supported: ARM64 (aarch64/arm64) or AMD64 (x86_64)${NC}"
        exit 1
        ;;
esac
echo ""

# Check current running containers
if docker ps --filter "name=business-postgres" --format "{{.Image}}" | grep -q "postgis"; then
    CURRENT_IMAGE=$(docker ps --filter "name=business-postgres" --format "{{.Image}}")
    echo -e "${YELLOW}📦 Current PostgreSQL image: ${CURRENT_IMAGE}${NC}"

    if [ "${CURRENT_IMAGE}" != "${POSTGRES_IMAGE}" ]; then
        echo -e "${YELLOW}⚠️  Image change detected!${NC}"
        echo -e "${YELLOW}   Current: ${CURRENT_IMAGE}, Target: ${POSTGRES_IMAGE}${NC}"
        echo -e "${YELLOW}   Will pull and restart with new image...${NC}"
    else
        echo -e "${GREEN}✓ Image matches, proceeding with restart...${NC}"
    fi
else
    echo -e "${YELLOW}ℹ️  No business-postgres container found${NC}"
fi
echo ""

# Stop services first
echo -e "${YELLOW}🛑 Stopping services...${NC}"
bash ./scripts/stop.sh || true
echo ""

# Remove old PostGIS images if they exist
echo -e "${YELLOW}🧹 Cleaning up old images...${NC}"

# Remove official PostGIS images (AMD64 only)
for tag in "15-3.4" "16-3.4" "latest"; do
    if docker image inspect "postgis/postgis:${tag}" &>/dev/null; then
        echo -e "   Removing old PostGIS ${tag} image..."
        docker rmi -f "postgis/postgis:${tag}" || true
    fi
done

# Remove old ARM64 PostGIS images
for tag in "15-3.4" "latest"; do
    if docker image inspect "imresamu/postgis-arm64:${tag}" &>/dev/null; then
        IMAGE_TO_KEEP="${POSTGRES_IMAGE}"
        if [ "imresamu/postgis-arm64:${tag}" != "${IMAGE_TO_KEEP}" ]; then
            echo -e "   Removing old ARM64 PostGIS ${tag} image..."
            docker rmi -f "imresamu/postgis-arm64:${tag}" || true
        fi
    fi
done

# Remove standard postgres:15 if exists
if docker image inspect postgres:15 &>/dev/null; then
    echo -e "   Removing standard PostgreSQL 15 image..."
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

# Pull the correct images
echo -e "${YELLOW}📥 Pulling images for ${DISPLAY_ARCH} architecture...${NC}"

export POSTGRES_IMAGE

# Pull PostgreSQL + PostGIS image with retries
echo -e "   Pulling ${POSTGRES_IMAGE}..."
MAX_RETRIES=3
RETRY_COUNT=0

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if docker pull "${POSTGRES_IMAGE}"; then
        echo -e "${GREEN}✓ PostgreSQL + PostGIS image pulled successfully${NC}"
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

# Pull MinIO image
echo -e "   Pulling minio/minio:latest..."
if docker pull minio/minio:latest; then
    echo -e "${GREEN}✓ MinIO image pulled successfully${NC}"
else
    echo -e "${RED}✗ Failed to pull MinIO image${NC}"
    exit 1
fi

echo ""

echo -e "${YELLOW}🚀 Starting services...${NC}"

# Start services
bash ./scripts/start.sh

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Restart Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "Architecture: ${GREEN}${DISPLAY_ARCH}${NC}"
echo -e "PostgreSQL Image: ${GREEN}${POSTGRES_IMAGE}${NC}"
echo -e "PostGIS: ${GREEN}Built-in (no manual installation needed)${NC}"
echo ""
echo "Verify PostGIS installation:"
echo "  docker exec business-postgres psql -U business -d business -c 'SELECT PostGIS_Version();'"
echo ""

