#!/bin/bash

# PostGIS Extension Installation Script for ADDP PostgreSQL
# 为 ADDP PostgreSQL 数据库安装 PostGIS 空间扩展

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

DB_USER=${POSTGRES_USER:-addp}
DB_NAME=${POSTGRES_DB:-addp}

# Check if PostgreSQL container is running
if ! docker ps --filter name=addp-postgres --filter status=running | grep -q addp-postgres; then
    echo -e "${RED}✗ PostgreSQL container is not running!${NC}"
    echo -e "${YELLOW}Please start it first: bash scripts/infra/up.sh${NC}"
    exit 1
fi

echo -e "${YELLOW}Checking PostGIS installation...${NC}"

# Check if PostGIS is already installed
POSTGIS_CHECK=$(docker exec addp-postgres psql -U "$DB_USER" -d "$DB_NAME" -t -c "SELECT COUNT(*) FROM pg_extension WHERE extname='postgis';" 2>/dev/null || echo "0")
POSTGIS_CHECK=$(echo "$POSTGIS_CHECK" | tr -d '[:space:]')

if [ "$POSTGIS_CHECK" = "1" ]; then
    echo -e "${GREEN}✓ PostGIS is already installed${NC}"

    # Show PostGIS version
    POSTGIS_VERSION=$(docker exec addp-postgres psql -U "$DB_USER" -d "$DB_NAME" -t -c "SELECT PostGIS_Version();" 2>/dev/null || echo "Unknown")
    echo -e "${GREEN}  Version: $POSTGIS_VERSION${NC}"
else
    echo -e "${YELLOW}PostGIS not found. Installing packages...${NC}"

    # Check if PostGIS packages are installed in container
    POSTGIS_PACKAGE_CHECK=$(docker exec addp-postgres sh -c 'dpkg -l | grep postgis || echo "not-installed"')

    if echo "$POSTGIS_PACKAGE_CHECK" | grep -q "not-installed"; then
        echo -e "${YELLOW}Installing PostGIS packages in container...${NC}"

        # Install PostGIS packages (Debian-based container)
        docker exec addp-postgres sh -c '
            apt-get update -qq && \
            apt-get install -y -qq --no-install-recommends \
                postgresql-15-postgis-3 \
                postgis > /dev/null 2>&1
        '

        if [ $? -eq 0 ]; then
            echo -e "${GREEN}✓ PostGIS packages installed successfully${NC}"
        else
            echo -e "${RED}✗ Failed to install PostGIS packages${NC}"
            echo -e "${YELLOW}Tip: For AMD64 systems, consider using postgis/postgis:15-3.4 image${NC}"
            echo -e "${YELLOW}     Set POSTGRES_IMAGE=postgis/postgis:15-3.4 in .env${NC}"
            exit 1
        fi
    else
        echo -e "${GREEN}✓ PostGIS packages already installed${NC}"
    fi

    # Now create PostGIS extension in database
    echo -e "${YELLOW}Creating PostGIS extension in database...${NC}"
    docker exec addp-postgres psql -U "$DB_USER" -d "$DB_NAME" -c "CREATE EXTENSION IF NOT EXISTS postgis;" 2>&1

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ PostGIS extension installed successfully${NC}"
    else
        echo -e "${RED}✗ Failed to install PostGIS extension${NC}"
        exit 1
    fi
fi

# Check and install PostGIS Topology extension
echo ""
echo -e "${YELLOW}Checking PostGIS Topology extension...${NC}"

TOPOLOGY_CHECK=$(docker exec addp-postgres psql -U "$DB_USER" -d "$DB_NAME" -t -c "SELECT COUNT(*) FROM pg_extension WHERE extname='postgis_topology';" 2>/dev/null || echo "0")
TOPOLOGY_CHECK=$(echo "$TOPOLOGY_CHECK" | tr -d '[:space:]')

if [ "$TOPOLOGY_CHECK" = "1" ]; then
    echo -e "${GREEN}✓ PostGIS Topology is already installed${NC}"
else
    echo -e "${YELLOW}PostGIS Topology not found. Installing...${NC}"

    docker exec addp-postgres psql -U "$DB_USER" -d "$DB_NAME" -c "CREATE EXTENSION IF NOT EXISTS postgis_topology;" 2>&1

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
docker exec addp-postgres psql -U "$DB_USER" -d "$DB_NAME" -c "\dx" 2>/dev/null

echo ""
echo -e "${GREEN}ADDP system database now supports spatial data.${NC}"
echo ""
