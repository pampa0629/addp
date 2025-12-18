#!/bin/bash
# Business Database Startup Script
# 业务库启动脚本
#
# 功能: 检查配置、检测架构、启动服务、验证健康、安装 PostGIS
# 服务: PostgreSQL (必选), MinIO (必选), Doris (可选), Spark (可选)
# 特性: 幂等执行（可重复运行，已运行的服务会跳过）
#
# 使用方法:
#   bash scripts/start.sh                    # 默认启动 PostgreSQL + MinIO
#   bash scripts/start.sh -all               # 启动所有服务
#   bash scripts/start.sh -postgres          # 只启动 PostgreSQL
#   bash scripts/start.sh -minio             # 只启动 MinIO
#   bash scripts/start.sh -doris             # 只启动 Doris
#   bash scripts/start.sh -spark             # 只启动 Spark
#   bash scripts/start.sh -postgres -minio   # 启动 PostgreSQL + MinIO
#   bash scripts/start.sh -doris -spark      # 启动 Doris + Spark

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

# 解析启动参数
ENABLE_PG=false
ENABLE_MINIO=false
ENABLE_DORIS=false
ENABLE_SPARK=false
HAS_ARGS=false

for arg in "$@"; do
    HAS_ARGS=true
    case $arg in
        -all)
            ENABLE_PG=true
            ENABLE_MINIO=true
            ENABLE_DORIS=true
            ENABLE_SPARK=true
            ;;
        -postgres)
            ENABLE_PG=true
            ;;
        -minio)
            ENABLE_MINIO=true
            ;;
        -doris)
            ENABLE_DORIS=true
            ;;
        -spark)
            ENABLE_SPARK=true
            ;;
        -h|--help)
            echo "使用方法:"
            echo "  bash scripts/start.sh                    # 默认启动 PostgreSQL + MinIO"
            echo "  bash scripts/start.sh -all               # 启动所有服务"
            echo "  bash scripts/start.sh -postgres          # 只启动 PostgreSQL"
            echo "  bash scripts/start.sh -minio             # 只启动 MinIO"
            echo "  bash scripts/start.sh -doris             # 只启动 Doris"
            echo "  bash scripts/start.sh -spark             # 只启动 Spark"
            echo "  bash scripts/start.sh -postgres -minio   # 启动 PostgreSQL + MinIO"
            echo "  bash scripts/start.sh -doris -spark      # 启动 Doris + Spark"
            exit 0
            ;;
        *)
            echo -e "${RED}未知参数: $arg${NC}"
            echo "使用 -h 查看帮助"
            exit 1
            ;;
    esac
done

# 如果没有指定任何参数，默认启动 PostgreSQL + MinIO
if [ "$HAS_ARGS" = false ]; then
    ENABLE_PG=true
    ENABLE_MINIO=true
fi

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Business Database Startup${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${YELLOW}启动模式:${NC}"
if [ "$ENABLE_PG" = true ]; then
    echo -e "  PostgreSQL: ✓"
else
    echo -e "  PostgreSQL: ✗ (使用 -postgres 启用)"
fi
if [ "$ENABLE_MINIO" = true ]; then
    echo -e "  MinIO: ✓"
else
    echo -e "  MinIO: ✗ (使用 -minio 启用)"
fi
if [ "$ENABLE_DORIS" = true ]; then
    echo -e "  Doris: ✓"
else
    echo -e "  Doris: ✗ (使用 -doris 启用)"
fi
if [ "$ENABLE_SPARK" = true ]; then
    echo -e "  Spark: ✓"
else
    echo -e "  Spark: ✗ (使用 -spark 启用)"
fi
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
DORIS_FE_PORT=${DORIS_FE_PORT:-9030}
DORIS_FE_HTTP_PORT=${DORIS_FE_HTTP_PORT:-8030}
SPARK_MASTER_PORT=${SPARK_MASTER_PORT:-7077}
SPARK_MASTER_UI=${SPARK_MASTER_UI:-8088}
SPARK_THRIFT_PORT=${SPARK_THRIFT_PORT:-10000}

check_port_used_by_self() {
    local port=$1
    local container=$2
    if docker ps --filter "name=${container}" --format '{{.Ports}}' 2>/dev/null | grep -q ":${port}->"; then
        return 0
    fi
    return 1
}

echo -e "${YELLOW}检查端口...${NC}"
PORTS_TO_CHECK=""
[ "$ENABLE_PG" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $PG_PORT"
[ "$ENABLE_MINIO" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $MINIO_API $MINIO_CONSOLE"
[ "$ENABLE_DORIS" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $DORIS_FE_PORT $DORIS_FE_HTTP_PORT"
[ "$ENABLE_SPARK" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $SPARK_MASTER_PORT $SPARK_MASTER_UI $SPARK_THRIFT_PORT"

for port in $PORTS_TO_CHECK; do
    if lsof -nP -i ":${port}" >/dev/null 2>&1; then
        if check_port_used_by_self $port "business-postgres" || \
           check_port_used_by_self $port "business-minio" || \
           check_port_used_by_self $port "business-doris-fe" || \
           check_port_used_by_self $port "business-spark-master"; then
            echo -e "${GREEN}✓ 端口 ${port} 已被业务库容器使用${NC}"
        else
            echo -e "${YELLOW}⚠️  端口 ${port} 被其他进程占用${NC}"
        fi
    fi
done
echo ""

# 5. 启动服务（幂等）
echo -e "${YELLOW}🚀 启动服务...${NC}"

# PostgreSQL
if [ "$ENABLE_PG" = true ]; then
    if docker ps --filter "name=business-postgres" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ PostgreSQL 已运行，跳过启动${NC}"
    else
        docker-compose up -d postgres
        echo -e "${GREEN}✓ PostgreSQL 已启动${NC}"
    fi
fi

# MinIO
if [ "$ENABLE_MINIO" = true ]; then
    if docker ps --filter "name=business-minio" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ MinIO 已运行，跳过启动${NC}"
    else
        docker-compose up -d minio
        echo -e "${GREEN}✓ MinIO 已启动${NC}"
    fi
fi

# Doris
if [ "$ENABLE_DORIS" = true ]; then
    if docker ps --filter "name=business-doris-fe" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ Doris 已运行，跳过启动${NC}"
    else
        docker-compose up -d doris-fe
        echo -e "${GREEN}✓ Doris 已启动${NC}"
    fi
fi

# Spark
if [ "$ENABLE_SPARK" = true ]; then
    if docker ps --filter "name=business-spark-master" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ Spark Master 已运行，跳过启动${NC}"
    else
        docker-compose up -d spark-master
        echo -e "${GREEN}✓ Spark Master 已启动${NC}"
    fi

    if docker ps --filter "name=business-spark-worker-1" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ Spark Worker 1 已运行，跳过启动${NC}"
    else
        docker-compose up -d spark-worker-1
        echo -e "${GREEN}✓ Spark Worker 1 已启动${NC}"
    fi
fi
echo ""

# 6. 等待服务就绪
echo -e "${YELLOW}⏳ 等待服务就绪...${NC}"

if [ "$ENABLE_PG" = true ]; then
    for i in {1..30}; do
        if docker exec business-postgres pg_isready -U business >/dev/null 2>&1; then
            echo -e "${GREEN}✓ PostgreSQL 就绪${NC}"
            break
        fi
        sleep 1
    done
fi

if [ "$ENABLE_MINIO" = true ]; then
    for i in {1..30}; do
        if curl -sf http://localhost:${MINIO_API}/minio/health/live >/dev/null 2>&1; then
            echo -e "${GREEN}✓ MinIO 就绪${NC}"
            break
        fi
        sleep 1
    done
fi

if [ "$ENABLE_DORIS" = true ]; then
    echo -e "${YELLOW}等待 Doris 启动 (可能需要 60-90 秒)...${NC}"
    for i in {1..90}; do
        if docker exec business-doris-fe mysql -h127.0.0.1 -P9030 -uroot --connect-timeout=5 -e 'SHOW FRONTENDS;' >/dev/null 2>&1; then
            echo -e "${GREEN}✓ Doris 就绪${NC}"
            break
        fi
        sleep 1
    done
fi

if [ "$ENABLE_SPARK" = true ]; then
    echo -e "${YELLOW}等待 Spark 启动 (可能需要 60-90 秒)...${NC}"
    for i in {1..90}; do
        if curl -sf http://localhost:${SPARK_MASTER_UI} >/dev/null 2>&1; then
            echo -e "${GREEN}✓ Spark Master 就绪${NC}"
            break
        fi
        sleep 1
    done
fi
echo ""

# 7. 验证 PostGIS（幂等，仅当 PostgreSQL 启用时）
if [ "$ENABLE_PG" = true ]; then
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
fi

# 8. 完成
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  ✓ 业务库启动完成！${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# 动态显示已启动的服务访问信息
if [ "$ENABLE_PG" = true ]; then
    echo -e "PostgreSQL: localhost:${PG_PORT}"
fi
if [ "$ENABLE_MINIO" = true ]; then
    echo -e "MinIO API: http://localhost:${MINIO_API}"
    echo -e "MinIO Console: http://localhost:${MINIO_CONSOLE}"
fi
if [ "$ENABLE_DORIS" = true ]; then
    echo -e "Doris FE (MySQL): localhost:${DORIS_FE_PORT}"
    echo -e "Doris FE (HTTP): http://localhost:${DORIS_FE_HTTP_PORT}"
fi
if [ "$ENABLE_SPARK" = true ]; then
    echo -e "Spark Master: spark://localhost:${SPARK_MASTER_PORT}"
    echo -e "Spark Master UI: http://localhost:${SPARK_MASTER_UI}"
    echo -e "Spark Thrift Server: localhost:${SPARK_THRIFT_PORT}"
fi

echo ""
echo -e "查看日志: docker-compose logs -f"
echo -e "停止服务: bash scripts/stop.sh"
