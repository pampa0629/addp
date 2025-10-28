#!/bin/bash

# Business Infrastructure Startup Script
# 业务基础设施启动脚本

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  ADDP Business Infrastructure Startup${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# Check if .env exists
if [ ! -f .env ]; then
    echo -e "${YELLOW}⚠️  .env file not found. Creating from template...${NC}"
    cp .env.example .env
    echo -e "${GREEN}✓ Created .env file${NC}"
    echo -e "${YELLOW}⚠️  Please edit .env to configure passwords before starting!${NC}"
    echo ""
    read -p "Press Enter to continue or Ctrl+C to exit..."
fi

# Start services
echo -e "${GREEN}Starting business infrastructure services...${NC}"
docker-compose up -d

# Wait for services to be ready
echo ""
echo -e "${YELLOW}Waiting for services to be ready...${NC}"

# Wait for PostgreSQL
echo -n "Checking PostgreSQL... "
for i in {1..30}; do
    if docker-compose exec -T postgres pg_isready -U ${POSTGRES_USER:-business} > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        break
    fi
    sleep 1
    echo -n "."
done

# Wait for MinIO
echo -n "Checking MinIO... "
for i in {1..30}; do
    if curl -sf http://localhost:${MINIO_API_PORT:-9000}/minio/health/live > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        break
    fi
    sleep 1
    echo -n "."
done

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Business Infrastructure Ready!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "Service URLs:"
echo -e "  PostgreSQL:     localhost:${POSTGRES_PORT:-5433}"
echo -e "  MinIO API:      http://localhost:${MINIO_API_PORT:-9000}"
echo -e "  MinIO Console:  http://localhost:${MINIO_CONSOLE_PORT:-9001}"
echo ""
echo "Credentials (default):"
echo -e "  PostgreSQL:  ${POSTGRES_USER:-business} / ${POSTGRES_PASSWORD:-business_password}"
echo -e "  MinIO:       ${MINIO_ROOT_USER:-minioadmin} / ${MINIO_ROOT_PASSWORD:-minioadmin}"
echo ""
echo -e "${YELLOW}⚠️  Remember to change default passwords in production!${NC}"
echo ""
echo "Next steps:"
echo "  - View logs:      docker-compose logs -f"
echo "  - Check status:   docker-compose ps"
echo "  - Stop services:  ./scripts/stop.sh"
echo ""
