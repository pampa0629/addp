#!/usr/bin/env bash

# pgvector Extension Installation Script for ADDP System PostgreSQL
# 为 ADDP 系统 PostgreSQL 数据库安装 pgvector 向量扩展
# 支持多模态向量检索、相似度搜索、最近邻查询等场景

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  pgvector Extension Installation${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

DB_USER="addp"
DB_NAME="addp"
CONTAINER_NAME="addp-postgres"

# Check if PostgreSQL container is running
if ! docker ps --filter name="${CONTAINER_NAME}" --filter status=running | grep -q "${CONTAINER_NAME}"; then
    echo -e "${RED}✗ PostgreSQL container is not running!${NC}"
    echo -e "${YELLOW}Please start it first: make up-infra${NC}"
    exit 1
fi

echo -e "${YELLOW}Checking pgvector installation...${NC}"

# Check if pgvector extension is already enabled in database
PGVECTOR_CHECK=$(docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -t -c "SELECT COUNT(*) FROM pg_extension WHERE extname='vector';" 2>/dev/null | tr -d '[:space:]' || echo "0")

if [ "${PGVECTOR_CHECK}" = "1" ]; then
    echo -e "${GREEN}✓ pgvector extension is already enabled${NC}"

    # Show pgvector version
    PGVECTOR_VERSION=$(docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -t -c "SELECT extversion FROM pg_extension WHERE extname='vector';" 2>/dev/null | tr -d '[:space:]' || echo "Unknown")
    echo -e "${GREEN}  Version: ${PGVECTOR_VERSION}${NC}"
else
    echo -e "${YELLOW}pgvector extension not found. Installing...${NC}"

    # Check if pgvector packages are installed in container
    PGVECTOR_PACKAGE_CHECK=$(docker exec "${CONTAINER_NAME}" sh -c 'dpkg -l | grep pgvector || echo "not-installed"')

    if echo "${PGVECTOR_PACKAGE_CHECK}" | grep -q "not-installed"; then
        echo -e "${YELLOW}Installing pgvector packages in container...${NC}"

        # Install build dependencies and pgvector from source
        docker exec "${CONTAINER_NAME}" sh -c '
            set -e
            echo "📦 Installing build dependencies..."
            apt-get update -qq
            apt-get install -y -qq --no-install-recommends \
                build-essential \
                git \
                postgresql-server-dev-15 \
                ca-certificates > /dev/null 2>&1

            echo "📥 Cloning pgvector repository..."
            cd /tmp
            if [ -d "pgvector" ]; then
                rm -rf pgvector
            fi
            git clone --branch v0.7.0 https://github.com/pgvector/pgvector.git > /dev/null 2>&1

            echo "🔨 Building pgvector..."
            cd pgvector
            make clean > /dev/null 2>&1 || true
            make -j$(nproc) > /dev/null 2>&1

            echo "📦 Installing pgvector..."
            make install > /dev/null 2>&1

            echo "🧹 Cleaning up..."
            cd /tmp
            rm -rf pgvector
            apt-get remove -y --purge build-essential git > /dev/null 2>&1
            apt-get autoremove -y > /dev/null 2>&1
            apt-get clean > /dev/null 2>&1
            rm -rf /var/lib/apt/lists/*

            echo "✅ pgvector built and installed successfully"
        '

        if [ $? -eq 0 ]; then
            echo -e "${GREEN}✓ pgvector packages installed successfully${NC}"
        else
            echo -e "${RED}✗ Failed to install pgvector packages${NC}"
            exit 1
        fi
    else
        echo -e "${GREEN}✓ pgvector packages already installed${NC}"
    fi

    # Create pgvector extension in database
    echo -e "${YELLOW}Creating pgvector extension in database...${NC}"
    docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -c "CREATE EXTENSION IF NOT EXISTS vector;" 2>&1 | grep -v "^$"

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ pgvector extension created successfully${NC}"
    else
        echo -e "${RED}✗ Failed to create pgvector extension${NC}"
        exit 1
    fi
fi

# Verify PostGIS extension is also available
echo ""
echo -e "${YELLOW}Verifying PostGIS extension...${NC}"

POSTGIS_CHECK=$(docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -t -c "SELECT COUNT(*) FROM pg_extension WHERE extname='postgis';" 2>/dev/null | tr -d '[:space:]' || echo "0")

if [ "${POSTGIS_CHECK}" = "1" ]; then
    POSTGIS_VERSION=$(docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -t -c "SELECT PostGIS_Version();" 2>/dev/null | tr -d '[:space:]' || echo "Unknown")
    echo -e "${GREEN}✓ PostGIS extension installed (version: ${POSTGIS_VERSION})${NC}"
else
    echo -e "${YELLOW}⚠️  PostGIS extension not found. Creating...${NC}"
    docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -c "CREATE EXTENSION IF NOT EXISTS postgis; CREATE EXTENSION IF NOT EXISTS postgis_topology;" 2>&1 | grep -v "^$" || true
fi

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Extensions Installation Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# Show list of installed extensions
echo "Installed PostgreSQL extensions:"
docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}" -c "\dx" 2>/dev/null

echo ""
echo -e "${GREEN}✅ ADDP system database is now ready for:${NC}"
echo -e "   - ${GREEN}Spatial data operations${NC} (PostGIS)"
echo -e "   - ${GREEN}Vector embeddings & similarity search${NC} (pgvector)"
echo -e "   - ${GREEN}Multi-modal AI applications${NC} (text, image, audio embeddings)"
echo ""
echo -e "${YELLOW}Example: Create a vector column (1536 dimensions for OpenAI embeddings)${NC}"
echo "  ALTER TABLE your_table ADD COLUMN embedding vector(1536);"
echo ""
echo -e "${YELLOW}Example: Find similar vectors (cosine distance)${NC}"
echo "  SELECT * FROM your_table ORDER BY embedding <=> '[0.1,0.2,...]'::vector LIMIT 5;"
echo ""
echo -e "${YELLOW}Example: Create vector index for faster search${NC}"
echo "  CREATE INDEX ON your_table USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);"
echo ""
