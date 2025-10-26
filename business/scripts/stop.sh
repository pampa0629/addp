#!/bin/bash

# Business Infrastructure Stop Script
# 业务基础设施停止脚本

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${YELLOW}========================================${NC}"
echo -e "${YELLOW}  Stopping Business Infrastructure${NC}"
echo -e "${YELLOW}========================================${NC}"
echo ""

# Ask for confirmation
read -p "Stop business infrastructure services? (y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Cancelled."
    exit 0
fi

# Stop services
echo -e "${YELLOW}Stopping services...${NC}"
docker-compose down

echo ""
echo -e "${GREEN}✓ Business infrastructure stopped${NC}"
echo ""
echo "Services stopped:"
echo "  - PostgreSQL (business data)"
echo "  - MinIO (business files)"
echo ""
echo "Data volumes are preserved. To remove volumes (⚠️ DELETES ALL DATA):"
echo "  docker-compose down -v"
echo ""
