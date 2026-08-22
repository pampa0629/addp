#!/bin/bash
# =============================================================================
# Start Local Docker Registry
# =============================================================================
# Description: Start a local Docker registry for ADDP image builds
# Usage: ./scripts/registry/start.sh
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
REGISTRY_IMAGE="registry:2"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Starting Local Docker Registry${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Check if registry is already running
if docker ps --format '{{.Names}}' | grep -q "^${REGISTRY_NAME}$"; then
    echo -e "${YELLOW}Registry container is already running${NC}"
    echo ""
    echo -e "${GREEN}Testing registry health...${NC}"
    if curl -sf --max-time 3 "http://localhost:${REGISTRY_PORT}/v2/" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ Registry is healthy and accessible${NC}"
        echo ""
        echo -e "${BLUE}Registry URL:${NC} http://localhost:${REGISTRY_PORT}"
        exit 0
    else
        echo -e "${RED}✗ Registry is running but not accessible${NC}"
        echo -e "${YELLOW}Restarting registry...${NC}"
        docker rm -f ${REGISTRY_NAME} > /dev/null 2>&1
    fi
fi

# Check if stopped registry container exists
if docker ps -a --format '{{.Names}}' | grep -q "^${REGISTRY_NAME}$"; then
    echo -e "${YELLOW}Found stopped registry container, removing...${NC}"
    docker rm -f ${REGISTRY_NAME} > /dev/null 2>&1
fi

# Start new registry container
echo -e "${GREEN}Starting new registry container...${NC}"
docker run -d \
    -p ${REGISTRY_PORT}:5000 \
    --restart=always \
    --name ${REGISTRY_NAME} \
    ${REGISTRY_IMAGE}

# Wait for registry to be ready
echo -e "${YELLOW}Waiting for registry to be ready...${NC}"
sleep 3

# Verify registry is accessible
MAX_RETRIES=10
RETRY_COUNT=0

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if curl -sf --max-time 3 "http://localhost:${REGISTRY_PORT}/v2/" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ Registry is up and running!${NC}"
        echo ""
        echo -e "${BLUE}Registry Information:${NC}"
        echo -e "  URL:       http://localhost:${REGISTRY_PORT}"
        echo -e "  Container: ${REGISTRY_NAME}"
        echo -e "  Image:     ${REGISTRY_IMAGE}"
        echo ""
        echo -e "${GREEN}You can now run:${NC}"
        echo -e "  ./scripts/deploy/1-build-images.sh"
        echo ""
        exit 0
    fi

    RETRY_COUNT=$((RETRY_COUNT + 1))
    echo -e "${YELLOW}Retry $RETRY_COUNT/$MAX_RETRIES...${NC}"
    sleep 2
done

# Failed to start
echo -e "${RED}✗ Failed to start registry${NC}"
echo ""
echo -e "${YELLOW}Troubleshooting:${NC}"
echo "  1. Check Docker logs:"
echo "     docker logs ${REGISTRY_NAME}"
echo ""
echo "  2. Check if port ${REGISTRY_PORT} is already in use:"
echo "     lsof -i :${REGISTRY_PORT}"
echo ""
echo "  3. Try manual cleanup:"
echo "     docker rm -f ${REGISTRY_NAME}"
echo "     docker run -d -p ${REGISTRY_PORT}:5000 --restart=always --name ${REGISTRY_NAME} ${REGISTRY_IMAGE}"
echo ""
exit 1
