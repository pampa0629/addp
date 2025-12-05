#!/bin/bash
# =============================================================================
# Check Local Docker Registry Status
# =============================================================================
# Description: Check the health and status of local Docker registry
# Usage: ./check-registry.sh
# =============================================================================

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

REGISTRY_NAME="registry"
REGISTRY_PORT="5001"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Docker Registry Health Check${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Check if container exists
if ! docker ps -a --format '{{.Names}}' | grep -q "^${REGISTRY_NAME}$"; then
    echo -e "${RED}✗ Registry container does not exist${NC}"
    echo ""
    echo -e "${YELLOW}Start registry with:${NC}"
    echo "  ./scripts/setup/start-registry.sh"
    echo "  OR"
    echo "  make registry-start"
    exit 1
fi

# Check if container is running
if ! docker ps --format '{{.Names}}' | grep -q "^${REGISTRY_NAME}$"; then
    echo -e "${RED}✗ Registry container exists but is not running${NC}"
    echo ""
    echo -e "${YELLOW}Start registry with:${NC}"
    echo "  docker start ${REGISTRY_NAME}"
    echo "  OR"
    echo "  make registry-start"
    exit 1
fi

echo -e "${GREEN}✓ Registry container is running${NC}"

# Check if registry is accessible
echo ""
echo -e "${YELLOW}Testing registry API...${NC}"
if curl -sf --max-time 3 "http://localhost:${REGISTRY_PORT}/v2/" > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Registry API is accessible${NC}"
else
    echo -e "${RED}✗ Registry API is not accessible${NC}"
    echo ""
    echo -e "${YELLOW}Possible issues:${NC}"
    echo "  - Container is starting up (wait a few seconds)"
    echo "  - Container crashed (check logs: docker logs ${REGISTRY_NAME})"
    echo "  - Port conflict (check: lsof -i :${REGISTRY_PORT})"
    exit 1
fi

# List images in registry
echo ""
echo -e "${YELLOW}Fetching registry catalog...${NC}"
CATALOG=$(curl -s "http://localhost:${REGISTRY_PORT}/v2/_catalog" 2>/dev/null)

if [ $? -eq 0 ]; then
    IMAGE_COUNT=$(echo "$CATALOG" | grep -o '"repositories":\[' | wc -l)

    if echo "$CATALOG" | grep -q '"repositories":\[\]'; then
        echo -e "${YELLOW}Registry is empty (no images pushed yet)${NC}"
    else
        echo -e "${GREEN}Images in registry:${NC}"
        echo "$CATALOG" | grep -o '"addp-[^"]*"' | sed 's/"//g' | sed 's/^/  - /' || echo "  (parsing error)"
    fi
else
    echo -e "${YELLOW}Could not fetch registry catalog${NC}"
fi

# Display registry info
echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Registry Information${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "${GREEN}Status:${NC}       ✓ Healthy"
echo -e "${GREEN}URL:${NC}          http://localhost:${REGISTRY_PORT}"
echo -e "${GREEN}Container:${NC}    ${REGISTRY_NAME}"
echo -e "${GREEN}Image:${NC}        $(docker inspect ${REGISTRY_NAME} --format '{{.Config.Image}}')"
echo -e "${GREEN}Uptime:${NC}       $(docker inspect ${REGISTRY_NAME} --format '{{.State.Status}}' | sed 's/running/Running/')"
echo ""
echo -e "${GREEN}✓ Registry is ready for image builds${NC}"
echo ""
