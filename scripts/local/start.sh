#!/bin/bash
# =============================================================================
# ADDP Local Docker Deployment Starter
# =============================================================================
# Description: Start ADDP services using Docker Compose (infrastructure + app)
# Usage: ./scripts/local/start.sh
#
# Features:
#   - Idempotent: Can be run multiple times safely
#   - Checks Docker availability
#   - Validates required images exist
#   - Starts infrastructure layer (PostgreSQL, Redis, MinIO, Meilisearch)
#   - Starts application layer (all backends, frontends, workers, gateway, nginx)
#   - Waits for health checks before proceeding
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration
REGISTRY="${REGISTRY:-localhost:5001}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}🚀 ADDP Local Docker Deployment${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# =============================================================================
# Helper Functions
# =============================================================================

# Check if Docker is running
check_docker() {
    echo -e "${YELLOW}▶️  Checking Docker status...${NC}"

    if ! docker info &> /dev/null; then
        echo -e "${RED}❌ Docker is not running${NC}"
        echo ""
        echo -e "${YELLOW}Please start Docker:${NC}"
        echo "  macOS:  open -a Docker"
        echo "  Linux:  sudo systemctl start docker"
        echo ""
        exit 1
    fi

    echo -e "${GREEN}✓ Docker is running${NC}"
}

# Check if required images exist
check_images() {
    echo -e "${YELLOW}▶️  Checking required images...${NC}"

    local required_images=(
        "${REGISTRY}/addp-system-backend:${IMAGE_TAG}"
        "${REGISTRY}/addp-manager-backend:${IMAGE_TAG}"
        "${REGISTRY}/addp-meta-backend:${IMAGE_TAG}"
        "${REGISTRY}/addp-transfer-backend:${IMAGE_TAG}"
        "${REGISTRY}/addp-orchestrator-backend:${IMAGE_TAG}"
        "${REGISTRY}/addp-develop-backend:${IMAGE_TAG}"
        "${REGISTRY}/addp-copilot-backend:${IMAGE_TAG}"
        "${REGISTRY}/addp-geopython-workflow-engine:${IMAGE_TAG}"
        "${REGISTRY}/addp-model3d-workflow-engine:${IMAGE_TAG}"
        "${REGISTRY}/addp-pointcloud-workflow-engine:${IMAGE_TAG}"
        "${REGISTRY}/addp-document-workflow-engine:${IMAGE_TAG}"
        "${REGISTRY}/addp-supermap-workflow-engine:${IMAGE_TAG}"
        "${REGISTRY}/addp-spark-workflow-engine:${IMAGE_TAG}"
        "${REGISTRY}/addp-jupyter-engine:${IMAGE_TAG}"
        "${REGISTRY}/addp-duckdb-engine:${IMAGE_TAG}"
        "${REGISTRY}/addp-gateway:${IMAGE_TAG}"
        "${REGISTRY}/addp-console:${IMAGE_TAG}"
        "${REGISTRY}/addp-nginx:${IMAGE_TAG}"
    )

    local missing_images=()

    for image in "${required_images[@]}"; do
        if ! docker images --format "{{.Repository}}:{{.Tag}}" | grep -q "^${image}$"; then
            missing_images+=("$image")
        fi
    done

    if [ ${#missing_images[@]} -gt 0 ]; then
        echo -e "${RED}❌ Missing images:${NC}"
        for img in "${missing_images[@]}"; do
            echo -e "  - ${img}"
        done
        echo ""
        echo -e "${YELLOW}Please build images first:${NC}"
        echo "  make build"
        echo "  make build-images"
        echo ""
        exit 1
    fi

    echo -e "${GREEN}✓ All required images found${NC}"
}

# =============================================================================
# Main Deployment
# =============================================================================

cd "$ROOT_DIR"

# Step 1: Check Docker
check_docker

# Step 2: Check images
check_images

# Step 3: Start infrastructure layer (delegate to infra/up.sh)
echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}📦 Starting Infrastructure Layer${NC}"
echo -e "${BLUE}========================================${NC}"

echo -e "${YELLOW}▶️  Delegating to scripts/infra/up.sh...${NC}"
echo ""

# Call the dedicated infrastructure startup script
# This script handles:
# - Port conflict checking
# - Image pulling if needed
# - Idempotent service startup (skips if already running)
# - Database/MinIO/Meilisearch initialization
# - Health checks for all services
if bash "$SCRIPT_DIR/../infra/up.sh"; then
    echo ""
    echo -e "${GREEN}✓ Infrastructure layer ready${NC}"
else
    echo -e "${RED}❌ Infrastructure startup failed${NC}"
    echo -e "${YELLOW}Check logs with: docker compose -f docker-compose.infra.yml logs${NC}"
    exit 1
fi

# Step 4: Start application layer and wait for container health.
echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}🚀 Starting Application Layer${NC}"
echo -e "${BLUE}========================================${NC}"
docker compose -f docker-compose.yml up -d --wait --wait-timeout 180 --remove-orphans

published_port="$(docker compose -f docker-compose.yml port nginx 80 | sed 's/.*://')"
public_origin="${ADDP_PUBLIC_ORIGIN:-$(sed -n 's/^ADDP_PUBLIC_ORIGIN=//p' .env | tail -n 1)}"
if [ -z "$public_origin" ]; then
    public_origin="http://localhost:${published_port}"
fi

echo -e "${GREEN}✓ ADDP 容器服务已就绪${NC}"
echo -e "统一入口: ${public_origin}"
echo ""
docker compose -f docker-compose.yml ps
echo ""
echo -e "${GREEN}Management Commands:${NC}"
echo "  bash scripts/local/status.sh"
echo "  bash scripts/local/stop.sh"
echo "  docker compose -f docker-compose.yml logs -f [service]"
