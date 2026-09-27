#!/bin/bash
# =============================================================================
# ADDP Local Docker Deployment Stopper
# =============================================================================
# Description: Stop ADDP services running in Docker Compose
# Usage: ./scripts/local/stop.sh [OPTIONS]
#
# Options:
#   --all       Stop both application and infrastructure layers
#
# Default behavior: Stop application layer only (keep infrastructure running)
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
STOP_INFRA=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --all)
            STOP_INFRA=true
            shift
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo ""
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  --all       Stop both application and infrastructure layers"
            exit 1
            ;;
    esac
done

cd "$ROOT_DIR"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}🛑 ADDP Local Docker Stop${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# =============================================================================
# Stop Application Layer
# =============================================================================

echo -e "${YELLOW}▶️  Stopping application layer...${NC}"

docker compose -f docker-compose.runtimes.yml down
docker compose -f docker-compose.yml down
echo -e "${GREEN}✓ Application layer stopped (volumes preserved)${NC}"

# =============================================================================
# Stop Infrastructure Layer (Optional)
# =============================================================================

if [ "$STOP_INFRA" = true ]; then
    echo ""
    echo -e "${YELLOW}▶️  Stopping infrastructure layer...${NC}"

    bash "$SCRIPT_DIR/../infra/down.sh"
    echo -e "${GREEN}✓ Infrastructure layer stopped (volumes preserved)${NC}"
else
    echo ""
    echo -e "${CYAN}ℹ️  Infrastructure layer is still running${NC}"
    echo -e "${CYAN}   (PostgreSQL, Redis, MinIO, Meilisearch)${NC}"
    echo ""
    echo -e "${YELLOW}To stop infrastructure:${NC}"
    echo "  bash scripts/local/stop.sh --all"
fi

# =============================================================================
# Summary
# =============================================================================

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}✅ Stop Complete${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

if [ "$STOP_INFRA" = true ]; then
    echo -e "${GREEN}All services stopped${NC}"
else
    echo -e "${GREEN}Application services stopped${NC}"
    echo -e "${CYAN}Infrastructure services still running${NC}"
fi

echo ""
echo -e "${GREEN}Management Commands:${NC}"
echo -e "  ${CYAN}Start:${NC}   bash scripts/local/start.sh"
echo -e "  ${CYAN}Status:${NC}  bash scripts/local/status.sh"
echo ""
