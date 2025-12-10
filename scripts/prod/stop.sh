#!/bin/bash
# =============================================================================
# ADDP Production Stop Script
# =============================================================================
# Description: Stop ADDP production services
# Usage: ./scripts/prod/stop.sh
# =============================================================================

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$PROJECT_ROOT"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Production Stop${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

if [ ! -f ".env.prod" ]; then
    echo -e "${YELLOW}Warning: .env.prod not found${NC}"
    echo "Stopping services without env file..."
    docker compose -f docker-compose.yml down
else
    echo -e "${YELLOW}Stopping ADDP services...${NC}"
    docker compose -f docker-compose.yml --env-file .env.prod down
fi

echo ""
echo -e "${GREEN}✓ ADDP services stopped${NC}"
