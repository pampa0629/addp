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
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  ADDP Infrastructure Restart${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Detect CPU architecture
ARCH=$(uname -m)
echo -e "${YELLOW}🔍 Detecting CPU architecture: ${ARCH}${NC}"

# Select PostgreSQL + PostGIS image based on architecture
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

# Stop services
echo -e "${YELLOW}🛑 Stopping infrastructure services...${NC}"
bash "${SCRIPT_DIR}/down.sh" || true
echo ""

# Clean up old images
echo -e "${YELLOW}🧹 Cleaning up old images...${NC}"

# Remove official PostGIS images (AMD64 only)
for tag in "15-3.4" "16-3.4" "latest"; do
    if docker image inspect "postgis/postgis:${tag}" &>/dev/null; then
        if [ "${tag}" != "15-3.4" ] || [ "${POSTGRES_IMAGE}" != "postgis/postgis:15-3.4" ]; then
            echo -e "   Removing old PostGIS ${tag} image..."
            docker rmi -f "postgis/postgis:${tag}" || true
        fi
    fi
done

# Remove old ARM64 PostGIS images
for tag in "15-3.4" "latest"; do
    if docker image inspect "imresamu/postgis-arm64:${tag}" &>/dev/null; then
        if [ "imresamu/postgis-arm64:${tag}" != "${POSTGRES_IMAGE}" ]; then
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

# Prune dangling images
docker image prune -f &>/dev/null || true

echo -e "${GREEN}✓ Cleanup complete${NC}"
echo ""

# Pull the PostgreSQL + PostGIS image
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

echo ""

# Start infrastructure
echo -e "${YELLOW}🚀 Starting infrastructure services...${NC}"

bash "${SCRIPT_DIR}/up.sh"

# Install pgvector extension for vector embeddings support
echo ""
echo -e "${YELLOW}📦 Installing pgvector extension...${NC}"
bash "${SCRIPT_DIR}/init-pgvector.sh"

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Restart Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "Architecture: ${GREEN}${DISPLAY_ARCH}${NC}"
echo -e "PostgreSQL Image: ${GREEN}${POSTGRES_IMAGE}${NC}"
echo -e "PostGIS: ${GREEN}Built-in${NC}"
echo -e "pgvector: ${GREEN}Installed${NC}"
echo ""
echo "Verify extensions:"
echo "  docker exec addp-postgres psql -U addp -d addp -c '\\dx'"
echo "  docker exec addp-postgres psql -U addp -d addp -c 'SELECT PostGIS_Version();'"
echo "  docker exec addp-postgres psql -U addp -d addp -c 'SELECT extversion FROM pg_extension WHERE extname = '\"'\"'vector'\"'\"';'"
echo ""

