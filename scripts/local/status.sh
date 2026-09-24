#!/bin/bash
# =============================================================================
# ADDP Local Docker Deployment Status
# =============================================================================
# Description: Show status of ADDP services running in Docker Compose
# Usage: ./scripts/local/status.sh
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

cd "$ROOT_DIR"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}📊 ADDP Docker Services Status${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# =============================================================================
# Infrastructure Layer Status
# =============================================================================

echo -e "${CYAN}=== Infrastructure Layer ===${NC}"
echo ""

if docker compose -f docker-compose.infra.yml ps --format json 2>/dev/null | grep -q .; then
    docker compose -f docker-compose.infra.yml ps
else
    echo -e "${YELLOW}No infrastructure services running${NC}"
fi

echo ""

# =============================================================================
# Application Layer Status
# =============================================================================

echo -e "${CYAN}=== Application Layer ===${NC}"
echo ""

if docker compose -f docker-compose.yml ps --format json 2>/dev/null | grep -q .; then
    docker compose -f docker-compose.yml ps
else
    echo -e "${YELLOW}No application services running${NC}"
fi

echo ""

# =============================================================================
# Service URL
# =============================================================================

echo -e "${CYAN}=== Unified Entry ===${NC}"
echo ""
nginx_id="$(docker compose -f docker-compose.yml ps --status running -q nginx 2>/dev/null)"
if [ -n "$nginx_id" ]; then
    published_port="$(docker compose -f docker-compose.yml port nginx 80 | sed 's/.*://')"
    public_origin="${ADDP_PUBLIC_ORIGIN:-}"
    if [ -z "$public_origin" ] && [ -f .env ]; then
        public_origin="$(sed -n 's/^ADDP_PUBLIC_ORIGIN=//p' .env | tail -n 1)"
    fi
    if [ -z "$public_origin" ]; then
        public_origin="http://localhost:${published_port}"
    fi
    echo -e "  ${GREEN}✓${NC} ${public_origin}"
else
    echo -e "  ${RED}✗${NC} Nginx 未运行"
fi
echo ""

# =============================================================================
# Resource Usage (Top 5 containers by memory)
# =============================================================================

echo -e "${CYAN}=== Resource Usage (Top 5 by Memory) ===${NC}"
echo ""

# Get all ADDP containers
CONTAINER_IDS=$(docker ps --filter "name=addp-" --filter "name=business-" --format "{{.ID}}" 2>/dev/null)

if [ -n "$CONTAINER_IDS" ]; then
    docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}" $CONTAINER_IDS | head -6
else
    echo -e "${YELLOW}No containers found${NC}"
fi

echo ""

# =============================================================================
# Management Commands
# =============================================================================

echo -e "${CYAN}=== Management Commands ===${NC}"
echo ""
echo -e "  ${GREEN}Start:${NC}   bash scripts/local/start.sh"
echo -e "  ${GREEN}Stop:${NC}    bash scripts/local/stop.sh"
echo -e "  ${GREEN}Restart:${NC} bash scripts/local/restart.sh"
echo -e "  ${GREEN}Logs:${NC}    docker compose -f docker-compose.yml logs -f [service]"
echo ""
echo -e "${CYAN}Example:${NC}"
echo -e "  docker compose -f docker-compose.yml logs -f system-backend"
echo ""
