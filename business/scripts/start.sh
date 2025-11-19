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

# Load .env into environment so that variables are visible in this script
if [ -f .env ]; then
    set -a
    # shellcheck disable=SC1091
    source .env
    set +a
fi

# Preflight port checks with auto-fix
echo -e "${YELLOW}Checking port availability...${NC}"

API_PORT=${BUSINESS_MINIO_API_PORT:-9000}
CONSOLE_PORT=${BUSINESS_MINIO_CONSOLE_PORT:-9001}
PG_PORT=${BUSINESS_POSTGRES_PORT:-5433}

check_port() {
    local port="$1"
    if lsof -nP -i ":${port}" >/dev/null 2>&1; then
        echo "busy"
    else
        echo "free"
    fi
}

port_used_by_container() {
    local port="$1"; local name="$2"
    local ports
    ports=$(docker ps --filter name="^/${name}$" --format '{{.Ports}}' 2>/dev/null || true)
    if echo "$ports" | grep -q ":${port}->"; then
        return 0
    fi
    return 1
}

# PostgreSQL port — keep 5433 by default for business
if [ "$(check_port "$PG_PORT")" = "busy" ]; then
    if port_used_by_container "$PG_PORT" "business-postgres"; then
        echo -e "${GREEN}PostgreSQL 端口 ${PG_PORT} 已由 business-postgres 使用，继续...${NC}"
    else
        echo -e "${YELLOW}警告：PostgreSQL 端口 ${PG_PORT} 被其他进程占用，将继续尝试启动（可能失败）。${NC}"
        echo -e "${YELLOW}如需释放：lsof -nP -i :${PG_PORT} 查看占用并处理，或临时修改 business/.env 中 BUSINESS_POSTGRES_PORT。${NC}"
    fi
fi

# Enforce reserved policy: Business MinIO must NOT use 9002/9003 (system reserved)
if [ "${API_PORT}" = "9002" ] || [ "${CONSOLE_PORT}" = "9003" ]; then
    echo -e "${RED}✗ 配置不合法：Business MinIO 端口不能使用 9002/9003（系统保留）。${NC}"
    echo -e "${YELLOW}请在 business/.env 设置 BUSINESS_MINIO_API_PORT=9000 和 BUSINESS_MINIO_CONSOLE_PORT=9001，然后重试。${NC}"
    exit 1
fi

# MinIO ports — fixed (prefer current BUSINESS_* or defaults)
if [ "$(check_port "$API_PORT")" = "busy" ] || [ "$(check_port "$CONSOLE_PORT")" = "busy" ]; then
    if port_used_by_container "$API_PORT" "business-minio" || port_used_by_container "$CONSOLE_PORT" "business-minio"; then
        echo -e "${GREEN}MinIO 端口 ${API_PORT}/${CONSOLE_PORT} 已由 business-minio 使用，继续...${NC}"
    else
        echo -e "${RED}✗ MinIO 端口 ${API_PORT}/${CONSOLE_PORT} 被其他进程占用，启动可能失败。${NC}"
        echo -e "${YELLOW}请释放占用（lsof -nP -i :${API_PORT},${CONSOLE_PORT}），或修改 business/.env 中 BUSINESS_MINIO_*_PORT。${NC}"
    fi
fi

# Start services
echo -e "${GREEN}Starting business infrastructure services...${NC}"

# Auto-detect CPU architecture and set PostgreSQL image
if [ -z "${POSTGRES_IMAGE}" ]; then
    ARCH=$(uname -m)
    echo -e "${YELLOW}🔍 Detecting CPU architecture: ${ARCH}${NC}"
    case "${ARCH}" in
        aarch64|arm64)
            export POSTGRES_IMAGE="imresamu/postgis-arm64:15-3.4"
            export DOCKER_PLATFORM="linux/arm64"
            echo -e "${GREEN}✓ Using ARM64 images${NC}"
            echo -e "  PostgreSQL: ${POSTGRES_IMAGE}"
            echo -e "  Docker Platform: ${DOCKER_PLATFORM}"
            ;;
        x86_64)
            export POSTGRES_IMAGE="postgis/postgis:15-3.4"
            export DOCKER_PLATFORM="linux/amd64"
            echo -e "${GREEN}✓ Using AMD64 images${NC}"
            echo -e "  PostgreSQL: ${POSTGRES_IMAGE}"
            echo -e "  Docker Platform: ${DOCKER_PLATFORM}"
            ;;
        *)
            echo -e "${YELLOW}⚠️  Unknown architecture ${ARCH}, defaulting to ARM64 images${NC}"
            export POSTGRES_IMAGE="imresamu/postgis-arm64:15-3.4"
            export DOCKER_PLATFORM="linux/arm64"
            ;;
    esac
else
    echo -e "${GREEN}✓ Using configured POSTGRES_IMAGE: ${POSTGRES_IMAGE}${NC}"
    # Also detect platform for MinIO even if POSTGRES_IMAGE is set
    ARCH=$(uname -m)
    case "${ARCH}" in
        aarch64|arm64)
            export DOCKER_PLATFORM="linux/arm64"
            ;;
        x86_64)
            export DOCKER_PLATFORM="linux/amd64"
            ;;
        *)
            export DOCKER_PLATFORM="linux/arm64"
            ;;
    esac
fi
echo ""

docker-compose up -d

# Wait for services to be ready
echo ""
echo -e "${YELLOW}Waiting for services to be ready...${NC}"

# Wait for PostgreSQL
echo -n "Checking PostgreSQL... "
for i in {1..30}; do
    if docker-compose exec -T postgres pg_isready -U ${BUSINESS_POSTGRES_USER:-business} > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        break
    fi
    sleep 1
    echo -n "."
done

# Wait for MinIO
echo -n "Checking MinIO... "
for i in {1..30}; do
    if curl -sf http://localhost:${BUSINESS_MINIO_API_PORT:-9000}/minio/health/live > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        break
    fi
    sleep 1
    echo -n "."
done

# Verify PostGIS extension (should be pre-installed in image)
echo ""
echo -e "${YELLOW}Verifying PostGIS extension...${NC}"
PG_EXTENSIONS=$(docker-compose exec -T postgres psql -U ${BUSINESS_POSTGRES_USER:-business} -d ${BUSINESS_POSTGRES_DB:-business} -t -c "\dx postgis" 2>/dev/null || echo "")

if echo "$PG_EXTENSIONS" | grep -q "postgis"; then
    POSTGIS_VERSION=$(docker-compose exec -T postgres psql -U ${BUSINESS_POSTGRES_USER:-business} -d ${BUSINESS_POSTGRES_DB:-business} -t -c "SELECT PostGIS_Version();" 2>/dev/null | tr -d '[:space:]' || echo "Unknown")
    echo -e "${GREEN}✓ PostGIS extension installed (version: ${POSTGIS_VERSION})${NC}"
else
    echo -e "${YELLOW}⚠️  PostGIS extension not found. Creating...${NC}"
    docker-compose exec -T postgres psql -U ${BUSINESS_POSTGRES_USER:-business} -d ${BUSINESS_POSTGRES_DB:-business} -c "CREATE EXTENSION IF NOT EXISTS postgis; CREATE EXTENSION IF NOT EXISTS postgis_topology;" 2>&1 | grep -v "^$" || true
    echo -e "${GREEN}✓ PostGIS extension created${NC}"
fi

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Business Infrastructure Ready!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "Service URLs:"
echo -e "  PostgreSQL:     localhost:${BUSINESS_POSTGRES_PORT:-5433}"
echo -e "  MinIO API:      http://localhost:${BUSINESS_MINIO_API_PORT:-9000}"
echo -e "  MinIO Console:  http://localhost:${BUSINESS_MINIO_CONSOLE_PORT:-9001}"
echo ""
echo "Credentials (default):"
echo -e "  PostgreSQL:  ${BUSINESS_POSTGRES_USER:-business} / ${BUSINESS_POSTGRES_PASSWORD:-business_password}"
echo -e "  MinIO:       ${BUSINESS_MINIO_ROOT_USER:-minioadmin} / ${BUSINESS_MINIO_ROOT_PASSWORD:-minioadmin}"
echo ""
echo -e "${YELLOW}⚠️  Remember to change default passwords in production!${NC}"
echo ""
echo "Next steps:"
echo "  - View logs:      docker-compose logs -f"
echo "  - Check status:   docker-compose ps"
echo "  - Stop services:  ./scripts/stop.sh"
echo ""
