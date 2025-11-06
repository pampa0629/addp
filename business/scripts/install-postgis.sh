#!/bin/bash

# PostGIS Extension Installation Script for Business PostgreSQL
# 为业务 PostgreSQL 数据库安装 PostGIS 空间扩展

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  PostGIS Extension Installation${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# Load environment variables
if [ -f .env ]; then
    set -a
    # shellcheck disable=SC1091
    source .env
    set +a
fi

DB_USER=${BUSINESS_POSTGRES_USER:-business}
DB_NAME=${BUSINESS_POSTGRES_DB:-business}
DB_PORT=${BUSINESS_POSTGRES_PORT:-5433}

# Check if PostgreSQL container is running
if ! docker ps --filter name=business-postgres --filter status=running | grep -q business-postgres; then
    echo -e "${RED}✗ PostgreSQL container is not running!${NC}"
    echo -e "${YELLOW}Please start it first: ./scripts/start.sh${NC}"
    exit 1
fi

echo -e "${YELLOW}Checking PostGIS installation...${NC}"

# Check if PostGIS is already installed
POSTGIS_CHECK=$(docker exec business-postgres psql -U "$DB_USER" -d "$DB_NAME" -t -c "SELECT COUNT(*) FROM pg_extension WHERE extname='postgis';" 2>/dev/null || echo "0")
POSTGIS_CHECK=$(echo "$POSTGIS_CHECK" | tr -d '[:space:]')

if [ "$POSTGIS_CHECK" = "1" ]; then
    echo -e "${GREEN}✓ PostGIS is already installed${NC}"

    # Show PostGIS version
    POSTGIS_VERSION=$(docker exec business-postgres psql -U "$DB_USER" -d "$DB_NAME" -t -c "SELECT PostGIS_Version();" 2>/dev/null || echo "Unknown")
    echo -e "${GREEN}  Version: $POSTGIS_VERSION${NC}"
else
    echo -e "${YELLOW}PostGIS not found. Installing...${NC}"

    # Install PostGIS extension
    docker exec business-postgres psql -U "$DB_USER" -d "$DB_NAME" -c "CREATE EXTENSION IF NOT EXISTS postgis;" 2>&1

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ PostGIS extension installed successfully${NC}"
    else
        echo -e "${RED}✗ Failed to install PostGIS extension${NC}"
        echo -e "${YELLOW}This might be because the PostgreSQL image doesn't include PostGIS.${NC}"
        echo -e "${YELLOW}Ensure you're using postgis/postgis:15-3.4-alpine image.${NC}"
        exit 1
    fi
fi

# Check and install PostGIS Topology extension
echo ""
echo -e "${YELLOW}Checking PostGIS Topology extension...${NC}"

TOPOLOGY_CHECK=$(docker exec business-postgres psql -U "$DB_USER" -d "$DB_NAME" -t -c "SELECT COUNT(*) FROM pg_extension WHERE extname='postgis_topology';" 2>/dev/null || echo "0")
TOPOLOGY_CHECK=$(echo "$TOPOLOGY_CHECK" | tr -d '[:space:]')

if [ "$TOPOLOGY_CHECK" = "1" ]; then
    echo -e "${GREEN}✓ PostGIS Topology is already installed${NC}"
else
    echo -e "${YELLOW}PostGIS Topology not found. Installing...${NC}"

    docker exec business-postgres psql -U "$DB_USER" -d "$DB_NAME" -c "CREATE EXTENSION IF NOT EXISTS postgis_topology;" 2>&1

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ PostGIS Topology extension installed successfully${NC}"
    else
        echo -e "${YELLOW}⚠️  PostGIS Topology installation failed (non-critical)${NC}"
    fi
fi

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  PostGIS Installation Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# Show list of installed extensions
echo "Installed PostgreSQL extensions:"
docker exec business-postgres psql -U "$DB_USER" -d "$DB_NAME" -c "\dx" 2>/dev/null

echo ""
echo -e "${GREEN}You can now import spatial data to the business database.${NC}"
echo -e "${YELLOW}Tip: When using Transfer module, specify target table as:${NC}"
echo -e "${YELLOW}     business_data.your_spatial_table${NC}"
echo ""
