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

set -e

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
        "${REGISTRY}/addp-python-workflow-engine:${IMAGE_TAG}"
        "${REGISTRY}/addp-spark-workflow-engine:${IMAGE_TAG}"
        "${REGISTRY}/addp-jupyter-engine:${IMAGE_TAG}"
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
        echo "  bash scripts/build/compile.sh"
        echo "  bash scripts/build/build-images.sh"
        echo ""
        exit 1
    fi

    echo -e "${GREEN}✓ All required images found${NC}"
}

# Wait for service to be healthy
wait_for_health() {
    local url=$1
    local service_name=$2
    local timeout=${3:-120}
    local elapsed=0

    echo -e "${YELLOW}⏳ Waiting for ${service_name} to be healthy...${NC}"

    while [ $elapsed -lt $timeout ]; do
        if curl -sf "$url" > /dev/null 2>&1; then
            echo -e "${GREEN}✓ ${service_name} is healthy${NC}"
            return 0
        fi
        sleep 2
        elapsed=$((elapsed + 2))
    done

    echo -e "${RED}⚠️  ${service_name} health check timeout (${timeout}s)${NC}"
    echo -e "${YELLOW}Note: Service may still be starting. Check logs with:${NC}"
    echo "  docker compose -f docker-compose.yml logs ${service_name}"
    return 1
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

# Step 4: Start application layer
echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}🚀 Starting Application Layer${NC}"
echo -e "${BLUE}========================================${NC}"

echo -e "${YELLOW}▶️  Starting application services...${NC}"
docker compose -f docker-compose.yml up -d

echo -e "${CYAN}Application services:${NC}"
docker compose -f docker-compose.yml ps

# Wait for key services to be healthy
echo ""
echo -e "${YELLOW}⏳ Waiting for key services to be healthy...${NC}"

# Wait for System Backend (critical)
if docker compose -f docker-compose.yml ps system-backend | grep -q "Up"; then
    wait_for_health "http://localhost:8180/health" "System Backend" 120
fi

# Wait for Gateway
if docker compose -f docker-compose.yml ps gateway | grep -q "Up"; then
    wait_for_health "http://localhost:8000/health" "Gateway" 60
fi

# Wait for Nginx
if docker compose -f docker-compose.yml ps nginx | grep -q "Up"; then
    wait_for_health "http://localhost:80/health" "Nginx" 30
fi

# Wait for Python Workflow Engine
if docker compose -f docker-compose.yml ps python-workflow-engine | grep -q "Up"; then
    wait_for_health "http://localhost:8099/health" "Python Workflow Engine" 60
fi

# =============================================================================
# Summary
# =============================================================================

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}✅ ADDP Services Started${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

echo -e "${GREEN}Access URLs:${NC}"
echo -e "  ${CYAN}Console (Recommended):${NC} http://localhost:80"
echo -e "  ${CYAN}Gateway:${NC}              http://localhost:8000"
echo -e "  ${CYAN}System Backend:${NC}       http://localhost:8180"
echo ""

echo -e "${GREEN}Infrastructure:${NC}"
echo -e "  ${CYAN}PostgreSQL:${NC}           localhost:5433"
echo -e "  ${CYAN}Redis:${NC}                localhost:6379"
echo -e "  ${CYAN}MinIO Console:${NC}        http://localhost:9001"
echo -e "  ${CYAN}Meilisearch:${NC}          http://localhost:7700"
echo ""

echo -e "${GREEN}Engines:${NC}"
echo -e "  ${CYAN}Python Workflow Engine:${NC}     http://localhost:8099"
echo ""

echo -e "${GREEN}Management Commands:${NC}"
echo -e "  ${CYAN}Status:${NC}   bash scripts/local/status.sh"
echo -e "  ${CYAN}Stop:${NC}     bash scripts/local/stop.sh"
echo -e "  ${CYAN}Restart:${NC}  bash scripts/local/restart.sh"
echo -e "  ${CYAN}Logs:${NC}     docker compose -f docker-compose.yml logs -f [service]"
echo ""

echo -e "${YELLOW}Note:${NC} Some services may take additional time to fully initialize."
echo -e "      Use 'status.sh' to check detailed service status."
echo ""
