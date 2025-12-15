#!/bin/bash
# Business Database Startup Script
# 业务库启动脚本
#
# 功能: 检查配置、检测架构、启动服务、验证健康、安装 PostGIS
# 特性: 幂等执行（可重复运行，已运行的服务会跳过）

set -e

# 获取脚本所在目录并切换到项目根目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Business Database Startup${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# 1. 检查并创建 .env
if [ ! -f .env ]; then
    echo -e "${YELLOW}⚠️  .env 不存在，从模板创建...${NC}"
    cp .env.example .env
    echo -e "${GREEN}✓ 已创建 .env${NC}"
    echo -e "${YELLOW}⚠️  请编辑 .env 修改密码后再启动！${NC}"
    echo ""
    read -p "按 Enter 继续或 Ctrl+C 退出..."
fi

# 2. 加载环境变量
if [ -f .env ]; then
    set -a
    source .env
    set +a
fi

# 3. 检测 CPU 架构
ARCH=$(uname -m)
echo -e "${YELLOW}🔍 检测 CPU 架构: ${ARCH}${NC}"

case "${ARCH}" in
    aarch64|arm64)
        export POSTGRES_IMAGE="imresamu/postgis-arm64:15-3.4"
        echo -e "${GREEN}✓ ARM64 架构，使用: ${POSTGRES_IMAGE}${NC}"
        ;;
    x86_64)
        export POSTGRES_IMAGE="postgis/postgis:15-3.4"
        echo -e "${GREEN}✓ AMD64 架构，使用: ${POSTGRES_IMAGE}${NC}"
        ;;
    *)
        echo -e "${RED}✗ 不支持的架构: ${ARCH}${NC}"
        exit 1
        ;;
esac
echo ""

# 4. 检查端口占用（幂等）
PG_PORT=${POSTGRES_PORT:-5433}
MINIO_API=${MINIO_API_PORT:-9002}
MINIO_CONSOLE=${MINIO_CONSOLE_PORT:-9003}

check_port_used_by_self() {
    local port=$1
    local container=$2
    if docker ps --filter "name=${container}" --format '{{.Ports}}' 2>/dev/null | grep -q ":${port}->"; then
        return 0
    fi
    return 1
}

echo -e "${YELLOW}检查端口...${NC}"
for port in $PG_PORT $MINIO_API $MINIO_CONSOLE; do
    if lsof -nP -i ":${port}" >/dev/null 2>&1; then
        if check_port_used_by_self $port "business-postgres" || \
           check_port_used_by_self $port "business-minio"; then
            echo -e "${GREEN}✓ 端口 ${port} 已被业务库容器使用${NC}"
        else
            echo -e "${YELLOW}⚠️  端口 ${port} 被其他进程占用${NC}"
        fi
    fi
done
echo ""

# 5. 启动服务（幂等）
echo -e "${YELLOW}🚀 启动服务...${NC}"

if docker ps --filter "name=business-postgres" --format '{{.Status}}' | grep -q "Up"; then
    echo -e "${GREEN}✓ business-postgres 已运行，跳过启动${NC}"
else
    docker-compose up -d postgres
    echo -e "${GREEN}✓ PostgreSQL 已启动${NC}"
fi

if docker ps --filter "name=business-minio" --format '{{.Status}}' | grep -q "Up"; then
    echo -e "${GREEN}✓ business-minio 已运行，跳过启动${NC}"
else
    docker-compose up -d minio
    echo -e "${GREEN}✓ MinIO 已启动${NC}"
fi
echo ""

# 6. 等待服务就绪
echo -e "${YELLOW}⏳ 等待服务就绪...${NC}"

for i in {1..30}; do
    if docker exec business-postgres pg_isready -U business >/dev/null 2>&1; then
        echo -e "${GREEN}✓ PostgreSQL 就绪${NC}"
        break
    fi
    sleep 1
done

for i in {1..30}; do
    if curl -sf http://localhost:${MINIO_API}/minio/health/live >/dev/null 2>&1; then
        echo -e "${GREEN}✓ MinIO 就绪${NC}"
        break
    fi
    sleep 1
done
echo ""

# 7. 验证 PostGIS（幂等）
echo -e "${YELLOW}🔍 验证 PostGIS 扩展...${NC}"

POSTGIS_INSTALLED=$(docker exec business-postgres psql -U business -d business -tAc \
    "SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='postgis');" 2>/dev/null || echo "false")

if [ "$POSTGIS_INSTALLED" = "t" ]; then
    echo -e "${GREEN}✓ PostGIS 已安装${NC}"
    VERSION=$(docker exec business-postgres psql -U business -d business -tAc \
        "SELECT PostGIS_Version();" 2>/dev/null || echo "unknown")
    echo -e "${GREEN}  版本: ${VERSION}${NC}"
else
    echo -e "${YELLOW}⚠️  PostGIS 未安装，正在安装...${NC}"
    docker exec business-postgres psql -U business -d business -c \
        "CREATE EXTENSION IF NOT EXISTS postgis;" >/dev/null 2>&1
    docker exec business-postgres psql -U business -d business -c \
        "CREATE EXTENSION IF NOT EXISTS postgis_topology;" >/dev/null 2>&1
    echo -e "${GREEN}✓ PostGIS 已安装${NC}"
fi
echo ""

# 8. 完成
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  ✓ 业务库启动完成！${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "PostgreSQL: localhost:${PG_PORT}"
echo -e "MinIO API: http://localhost:${MINIO_API}"
echo -e "MinIO Console: http://localhost:${MINIO_CONSOLE}"
echo ""
echo -e "查看日志: docker-compose logs -f"
echo -e "停止服务: bash scripts/stop.sh"
