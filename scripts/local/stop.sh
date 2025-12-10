#!/bin/bash
# =============================================================================
# ADDP Local Docker Deployment Stopper
# =============================================================================
# Description: Stop ADDP services running in Docker Compose
# Usage: ./scripts/local/stop.sh [OPTIONS]
#
# Options:
#   --all       Stop both application and infrastructure layers
#   --volumes   Remove data volumes (WARNING: deletes all data)
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
REMOVE_VOLUMES=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --all)
            STOP_INFRA=true
            shift
            ;;
        --volumes)
            REMOVE_VOLUMES=true
            shift
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo ""
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  --all       Stop both application and infrastructure layers"
            echo "  --volumes   Remove data volumes (WARNING: deletes all data)"
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

if [ "$REMOVE_VOLUMES" = true ]; then
    docker compose -f docker-compose.yml down -v
    echo -e "${GREEN}✓ Application layer stopped (volumes removed)${NC}"
else
    docker compose -f docker-compose.yml down
    echo -e "${GREEN}✓ Application layer stopped (volumes preserved)${NC}"
fi

# =============================================================================
# Stop Infrastructure Layer (Optional)
# =============================================================================

if [ "$STOP_INFRA" = true ]; then
    echo ""
    echo -e "${YELLOW}▶️  Stopping infrastructure layer...${NC}"

    if [ "$REMOVE_VOLUMES" = true ]; then
        echo -e "${RED}⚠️  WARNING: This will delete all database data!${NC}"
        read -p "Are you sure? (yes/no): " -r
        echo
        if [[ $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
            docker compose -f docker-compose.infra.yml down -v
            echo -e "${GREEN}✓ Infrastructure layer stopped (volumes removed)${NC}"
        else
            docker compose -f docker-compose.infra.yml down
            echo -e "${GREEN}✓ Infrastructure layer stopped (volumes preserved)${NC}"
        fi
    else
        docker compose -f docker-compose.infra.yml down
        echo -e "${GREEN}✓ Infrastructure layer stopped (volumes preserved)${NC}"
    fi
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
