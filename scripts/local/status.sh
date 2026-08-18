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
# Service URLs
# =============================================================================

echo -e "${CYAN}=== Service URLs ===${NC}"
echo ""

# Check if key services are running
SYSTEM_RUNNING=$(docker compose -f docker-compose.yml ps system-backend --format json 2>/dev/null | grep -c '"State":"running"' || echo "0")
GATEWAY_RUNNING=$(docker compose -f docker-compose.yml ps gateway --format json 2>/dev/null | grep -c '"State":"running"' || echo "0")
NGINX_RUNNING=$(docker compose -f docker-compose.yml ps nginx --format json 2>/dev/null | grep -c '"State":"running"' || echo "0")
POSTGRES_RUNNING=$(docker compose -f docker-compose.infra.yml ps postgres --format json 2>/dev/null | grep -c '"State":"running"' || echo "0")

if [ "$NGINX_RUNNING" -gt 0 ]; then
    echo -e "  ${GREEN}✓${NC} Console (Recommended):  http://localhost:80"
else
    echo -e "  ${RED}✗${NC} Console:               http://localhost:80 ${YELLOW}(not running)${NC}"
fi

if [ "$GATEWAY_RUNNING" -gt 0 ]; then
    echo -e "  ${GREEN}✓${NC} Gateway:               http://localhost:8000"
else
    echo -e "  ${RED}✗${NC} Gateway:               http://localhost:8000 ${YELLOW}(not running)${NC}"
fi

if [ "$SYSTEM_RUNNING" -gt 0 ]; then
    echo -e "  ${GREEN}✓${NC} System Backend:        http://localhost:8180"
else
    echo -e "  ${RED}✗${NC} System Backend:        http://localhost:8180 ${YELLOW}(not running)${NC}"
fi

GEOPYTHON_WORKFLOW_RUNNING=$(docker compose -f docker-compose.yml ps geopython-workflow-engine --format json 2>/dev/null | grep -c '"State":"running"' || echo "0")
MODEL3D_WORKFLOW_RUNNING=$(docker compose -f docker-compose.yml ps model3d-workflow-engine --format json 2>/dev/null | grep -c '"State":"running"' || echo "0")
POINTCLOUD_WORKFLOW_RUNNING=$(docker compose -f docker-compose.yml ps pointcloud-workflow-engine --format json 2>/dev/null | grep -c '"State":"running"' || echo "0")
SUPERMAP_WORKFLOW_RUNNING=$(docker compose -f docker-compose.yml ps supermap-workflow-engine --format json 2>/dev/null | grep -c '"State":"running"' || echo "0")

echo ""
echo -e "${CYAN}Engines:${NC}"

if [ "$GEOPYTHON_WORKFLOW_RUNNING" -gt 0 ]; then
    echo -e "  ${GREEN}✓${NC} GeoPython Workflow:      http://localhost:8099"
else
    echo -e "  ${RED}✗${NC} GeoPython Workflow:      http://localhost:8099 ${YELLOW}(not running)${NC}"
fi

if [ "$MODEL3D_WORKFLOW_RUNNING" -gt 0 ]; then
    echo -e "  ${GREEN}✓${NC} Model3D Workflow Engine:     http://localhost:8101"
else
    echo -e "  ${RED}✗${NC} Model3D Workflow Engine:     http://localhost:8101 ${YELLOW}(not running)${NC}"
fi

if [ "$POINTCLOUD_WORKFLOW_RUNNING" -gt 0 ]; then
    echo -e "  ${GREEN}✓${NC} PointCloud Workflow Engine:  http://localhost:8102"
else
    echo -e "  ${RED}✗${NC} PointCloud Workflow Engine:  http://localhost:8102 ${YELLOW}(not running)${NC}"
fi

if [ "$SUPERMAP_WORKFLOW_RUNNING" -gt 0 ]; then
    echo -e "  ${GREEN}✓${NC} SuperMap Workflow Engine:    http://localhost:8103"
else
    echo -e "  ${RED}✗${NC} SuperMap Workflow Engine:    http://localhost:8103 ${YELLOW}(not running)${NC}"
fi

echo ""
echo -e "${CYAN}Infrastructure:${NC}"

if [ "$POSTGRES_RUNNING" -gt 0 ]; then
    echo -e "  ${GREEN}✓${NC} PostgreSQL:            localhost:5433"
else
    echo -e "  ${RED}✗${NC} PostgreSQL:            localhost:5433 ${YELLOW}(not running)${NC}"
fi

REDIS_RUNNING=$(docker compose -f docker-compose.infra.yml ps redis --format json 2>/dev/null | grep -c '"State":"running"' || echo "0")
if [ "$REDIS_RUNNING" -gt 0 ]; then
    echo -e "  ${GREEN}✓${NC} Redis:                 localhost:6379"
else
    echo -e "  ${RED}✗${NC} Redis:                 localhost:6379 ${YELLOW}(not running)${NC}"
fi

MINIO_RUNNING=$(docker compose -f docker-compose.infra.yml ps minio --format json 2>/dev/null | grep -c '"State":"running"' || echo "0")
if [ "$MINIO_RUNNING" -gt 0 ]; then
    echo -e "  ${GREEN}✓${NC} MinIO Console:         http://localhost:9001"
else
    echo -e "  ${RED}✗${NC} MinIO Console:         http://localhost:9001 ${YELLOW}(not running)${NC}"
fi

MEILISEARCH_RUNNING=$(docker compose -f docker-compose.infra.yml ps meilisearch --format json 2>/dev/null | grep -c '"State":"running"' || echo "0")
if [ "$MEILISEARCH_RUNNING" -gt 0 ]; then
    echo -e "  ${GREEN}✓${NC} Meilisearch:           http://localhost:7700"
else
    echo -e "  ${RED}✗${NC} Meilisearch:           http://localhost:7700 ${YELLOW}(not running)${NC}"
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
