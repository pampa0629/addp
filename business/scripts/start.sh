#!/bin/bash
# Business Database Startup Script
# 业务库启动脚本
#
# 功能: 检查配置、检测架构、启动服务、验证健康、安装 PostGIS/初始化 Oracle 样例
# 服务: PostgreSQL/PostGIS、SuperMap SDX+ for PostgreSQL 专用 PostgreSQL、MinIO 及其他可选业务引擎
# 特性: 幂等执行（可重复运行，已运行的服务会跳过）
#
# 使用方法:
#   bash scripts/start.sh                    # 默认启动 PostgreSQL + MinIO
#   bash scripts/start.sh -all               # 启动所有服务
#   bash scripts/start.sh -postgres          # 只启动 PostgreSQL
#   bash scripts/start.sh -oracle            # 只启动 Oracle Free
#   bash scripts/start.sh -supermap-postgresql # 只启动 SuperMap SDX+ for PostgreSQL 专用实例
#   bash scripts/start.sh -minio             # 只启动 MinIO
#   bash scripts/start.sh -clickhouse        # 只启动 ClickHouse
#   bash scripts/start.sh -mongodb           # 只启动 MongoDB
#   bash scripts/start.sh -doris             # 只启动 Doris
#   bash scripts/start.sh -spark             # 只启动 Spark
#   bash scripts/start.sh -neo4j             # 只启动 Neo4j
#   bash scripts/start.sh -mysql             # 只启动 MySQL
#   bash scripts/start.sh -oceanbase         # 只启动 OceanBase CE
#   bash scripts/start.sh -redpanda          # 只启动业务 Redpanda
#   bash scripts/start.sh -nfs               # 只启动 NFS
#   bash scripts/start.sh -postgres -minio   # 启动 PostgreSQL + MinIO
#   bash scripts/start.sh -clickhouse -mongodb  # 启动 ClickHouse + MongoDB

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
ENABLE_ORACLE=false
ENABLE_SUPERMAP_PG=false
ENABLE_MINIO=false
ENABLE_CLICKHOUSE=false
ENABLE_MONGODB=false
ENABLE_DORIS=false
ENABLE_SPARK=false
ENABLE_NEO4J=false
ENABLE_NFS=false
ENABLE_MYSQL=false
ENABLE_OCEANBASE=false
ENABLE_REDPANDA=false
HAS_ARGS=false

for arg in "$@"; do
    HAS_ARGS=true
    case $arg in
        -all)
            ENABLE_PG=true
            ENABLE_ORACLE=true
            ENABLE_SUPERMAP_PG=true
            ENABLE_MINIO=true
            ENABLE_CLICKHOUSE=true
            ENABLE_MONGODB=true
            ENABLE_DORIS=true
            ENABLE_SPARK=true
            ENABLE_NEO4J=true
            ENABLE_NFS=true
            ENABLE_MYSQL=true
            ENABLE_OCEANBASE=true
            ENABLE_REDPANDA=true
            ;;
        -postgres)
            ENABLE_PG=true
            ;;
        -oracle)
            ENABLE_ORACLE=true
            ;;
        -supermap-postgresql)
            ENABLE_SUPERMAP_PG=true
            ;;
        -minio)
            ENABLE_MINIO=true
            ;;
        -clickhouse)
            ENABLE_CLICKHOUSE=true
            ;;
        -mongodb)
            ENABLE_MONGODB=true
            ;;
        -doris)
            ENABLE_DORIS=true
            ;;
        -spark)
            ENABLE_SPARK=true
            ;;
        -neo4j)
            ENABLE_NEO4J=true
            ;;
        -mysql)
            ENABLE_MYSQL=true
            ;;
        -oceanbase)
            ENABLE_OCEANBASE=true
            ;;
        -redpanda)
            ENABLE_REDPANDA=true
            ;;
        -nfs)
            ENABLE_NFS=true
            ;;
        -h|--help)
            echo "使用方法:"
            echo "  bash scripts/start.sh                       # 默认启动 PostgreSQL + MinIO"
            echo "  bash scripts/start.sh -all                  # 启动所有服务"
            echo "  bash scripts/start.sh -postgres             # 只启动 PostgreSQL"
            echo "  bash scripts/start.sh -oracle               # 只启动 Oracle Free"
            echo "  bash scripts/start.sh -supermap-postgresql  # 只启动 SuperMap SDX+ for PostgreSQL 专用实例"
            echo "  bash scripts/start.sh -minio                # 只启动 MinIO"
            echo "  bash scripts/start.sh -clickhouse           # 只启动 ClickHouse"
            echo "  bash scripts/start.sh -mongodb              # 只启动 MongoDB"
            echo "  bash scripts/start.sh -doris                # 只启动 Doris"
            echo "  bash scripts/start.sh -spark                # 只启动 Spark"
            echo "  bash scripts/start.sh -neo4j                # 只启动 Neo4j"
            echo "  bash scripts/start.sh -mysql               # 只启动 MySQL"
            echo "  bash scripts/start.sh -oceanbase           # 只启动 OceanBase CE"
            echo "  bash scripts/start.sh -redpanda            # 只启动业务 Redpanda"
            echo "  bash scripts/start.sh -nfs                  # 只启动 NFS"
            echo "  bash scripts/start.sh -postgres -minio      # 启动 PostgreSQL + MinIO"
            echo "  bash scripts/start.sh -clickhouse -mongodb  # 启动 ClickHouse + MongoDB"
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
if [ "$ENABLE_ORACLE" = true ]; then
    echo -e "  Oracle Free: ✓"
else
    echo -e "  Oracle Free: ✗ (使用 -oracle 启用)"
fi
if [ "$ENABLE_SUPERMAP_PG" = true ]; then
    echo -e "  SuperMap SDX+ for PostgreSQL: ✓"
else
    echo -e "  SuperMap SDX+ for PostgreSQL: ✗ (使用 -supermap-postgresql 启用)"
fi
if [ "$ENABLE_MINIO" = true ]; then
    echo -e "  MinIO: ✓"
else
    echo -e "  MinIO: ✗ (使用 -minio 启用)"
fi
if [ "$ENABLE_CLICKHOUSE" = true ]; then
    echo -e "  ClickHouse: ✓"
else
    echo -e "  ClickHouse: ✗ (使用 -clickhouse 启用)"
fi
if [ "$ENABLE_MONGODB" = true ]; then
    echo -e "  MongoDB: ✓"
else
    echo -e "  MongoDB: ✗ (使用 -mongodb 启用)"
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
if [ "$ENABLE_NEO4J" = true ]; then
    echo -e "  Neo4j: ✓"
else
    echo -e "  Neo4j: ✗ (使用 -neo4j 启用)"
fi
if [ "$ENABLE_MYSQL" = true ]; then
    echo -e "  MySQL: ✓"
else
    echo -e "  MySQL: ✗ (使用 -mysql 启用)"
fi
if [ "$ENABLE_OCEANBASE" = true ]; then
    echo -e "  OceanBase CE: ✓"
else
    echo -e "  OceanBase CE: ✗ (使用 -oceanbase 启用)"
fi
if [ "$ENABLE_REDPANDA" = true ]; then
    echo -e "  Redpanda: ✓"
else
    echo -e "  Redpanda: ✗ (使用 -redpanda 启用)"
fi
if [ "$ENABLE_NFS" = true ]; then
    echo -e "  NFS: ✓"
else
    echo -e "  NFS: ✗ (使用 -nfs 启用)"
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
ORACLE_PORT_VAL=${ORACLE_PORT:-15210}
SUPERMAP_PG_PORT=${SUPERMAP_POSTGRESQL_PORT:-5434}
MINIO_API=${MINIO_API_PORT:-9002}
MINIO_CONSOLE=${MINIO_CONSOLE_PORT:-9003}
CLICKHOUSE_PORT=${CLICKHOUSE_PORT:-9000}
CLICKHOUSE_HTTP_PORT=${CLICKHOUSE_HTTP_PORT:-8123}
MONGO_PORT=${MONGO_PORT:-27017}
DORIS_FE_PORT=${DORIS_FE_PORT:-9030}
DORIS_FE_HTTP_PORT=${DORIS_FE_HTTP_PORT:-8030}
SPARK_MASTER_PORT=${SPARK_MASTER_PORT:-7077}
SPARK_MASTER_UI=${SPARK_MASTER_UI:-18088}
SPARK_THRIFT_PORT=${SPARK_THRIFT_PORT:-11000}

NEO4J_HTTP_PORT_VAL=${NEO4J_HTTP_PORT:-7474}
NEO4J_BOLT_PORT_VAL=${NEO4J_BOLT_PORT:-7687}
MYSQL_PORT_VAL=${MYSQL_PORT:-3306}
OCEANBASE_PORT_VAL=${OCEANBASE_PORT:-2881}
BUSINESS_KAFKA_PORT_VAL=${BUSINESS_KAFKA_PORT:-29092}

if [ "$ENABLE_OCEANBASE" = true ]; then
    case "${OCEANBASE_DATABASE:-business}" in
        ""|*[!A-Za-z0-9_]*)
            echo -e "${RED}✗ OCEANBASE_DATABASE 只允许字母、数字和下划线${NC}"
            exit 1
            ;;
    esac
fi

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
[ "$ENABLE_ORACLE" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $ORACLE_PORT_VAL"
[ "$ENABLE_SUPERMAP_PG" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $SUPERMAP_PG_PORT"
[ "$ENABLE_MINIO" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $MINIO_API $MINIO_CONSOLE"
[ "$ENABLE_CLICKHOUSE" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $CLICKHOUSE_PORT $CLICKHOUSE_HTTP_PORT"
[ "$ENABLE_MONGODB" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $MONGO_PORT"
[ "$ENABLE_DORIS" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $DORIS_FE_PORT $DORIS_FE_HTTP_PORT"
[ "$ENABLE_SPARK" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $SPARK_MASTER_PORT $SPARK_MASTER_UI $SPARK_THRIFT_PORT"
[ "$ENABLE_NEO4J" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $NEO4J_HTTP_PORT_VAL $NEO4J_BOLT_PORT_VAL"
[ "$ENABLE_MYSQL" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $MYSQL_PORT_VAL"
[ "$ENABLE_OCEANBASE" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $OCEANBASE_PORT_VAL"
[ "$ENABLE_REDPANDA" = true ] && PORTS_TO_CHECK="$PORTS_TO_CHECK $BUSINESS_KAFKA_PORT_VAL"

for port in $PORTS_TO_CHECK; do
    if lsof -nP -i ":${port}" >/dev/null 2>&1; then
        if check_port_used_by_self $port "business-postgres" || \
           check_port_used_by_self $port "business-oracle" || \
           check_port_used_by_self $port "business-supermap-postgresql" || \
           check_port_used_by_self $port "business-minio" || \
           check_port_used_by_self $port "business-clickhouse" || \
           check_port_used_by_self $port "business-mongodb" || \
           check_port_used_by_self $port "business-doris-fe" || \
           check_port_used_by_self $port "business-spark-master" || \
           check_port_used_by_self $port "business-neo4j" || \
           check_port_used_by_self $port "business-mysql" || \
           check_port_used_by_self $port "business-oceanbase" || \
           check_port_used_by_self $port "business-redpanda"; then
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
        docker compose up -d postgres
        echo -e "${GREEN}✓ PostgreSQL 已启动${NC}"
    fi
fi

# Oracle Free
if [ "$ENABLE_ORACLE" = true ]; then
    if docker ps --filter "name=business-oracle" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ Oracle 已运行，跳过启动${NC}"
    else
        docker compose up -d oracle
        echo -e "${GREEN}✓ Oracle 已启动${NC}"
    fi
fi

# SuperMap SDX+ for PostgreSQL 专用实例
if [ "$ENABLE_SUPERMAP_PG" = true ]; then
    if docker ps --filter "name=business-supermap-postgresql" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ SuperMap PostgreSQL 已运行，跳过启动${NC}"
    else
        docker compose up -d supermap-postgresql
        echo -e "${GREEN}✓ SuperMap PostgreSQL 已启动${NC}"
    fi
fi

# MinIO
if [ "$ENABLE_MINIO" = true ]; then
    if docker ps --filter "name=business-minio" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ MinIO 已运行，跳过启动${NC}"
    else
        docker compose up -d minio
        echo -e "${GREEN}✓ MinIO 已启动${NC}"
    fi
fi

# ClickHouse
if [ "$ENABLE_CLICKHOUSE" = true ]; then
    if docker ps --filter "name=business-clickhouse" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ ClickHouse 已运行，跳过启动${NC}"
    else
        docker compose up -d clickhouse
        echo -e "${GREEN}✓ ClickHouse 已启动${NC}"
    fi
fi

# MongoDB
if [ "$ENABLE_MONGODB" = true ]; then
    if docker ps --filter "name=business-mongodb" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ MongoDB 已运行，跳过启动${NC}"
    else
        docker compose up -d mongodb
        echo -e "${GREEN}✓ MongoDB 已启动${NC}"
    fi
fi

# Doris
if [ "$ENABLE_DORIS" = true ]; then
    if docker ps --filter "name=business-doris-fe" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ Doris 已运行，跳过启动${NC}"
    else
        docker compose up -d doris-fe
        echo -e "${GREEN}✓ Doris 已启动${NC}"
    fi
fi

# Spark
if [ "$ENABLE_SPARK" = true ]; then
    if docker ps --filter "name=business-spark-master" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ Spark Master 已运行，跳过启动${NC}"
    else
        docker compose up -d spark-master
        echo -e "${GREEN}✓ Spark Master 已启动${NC}"
    fi

    if docker ps --filter "name=business-spark-worker-1" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ Spark Worker 1 已运行，跳过启动${NC}"
    else
        docker compose up -d spark-worker-1
        echo -e "${GREEN}✓ Spark Worker 1 已启动${NC}"
    fi
fi

# Neo4j
if [ "$ENABLE_NEO4J" = true ]; then
    mkdir -p "${PROJECT_ROOT}/neo4j/plugins"

    # 自动下载 Neo4j 插件（GDS + Spatial）
    # 插件版本需与 docker-compose.yml 中的 neo4j 镜像版本匹配
    NEO4J_VERSION="5.26.0"
    GDS_VERSION="2.13.9"
    GDS_JAR="neo4j-graph-data-science-${GDS_VERSION}.jar"
    SPATIAL_JAR="neo4j-spatial-${NEO4J_VERSION}-server-plugin.jar"
    PLUGINS_DIR="${PROJECT_ROOT}/neo4j/plugins"

    download_plugin() {
        local jar_name="$1"
        local url="$2"
        local dest="${PLUGINS_DIR}/${jar_name}"
        if [ -f "$dest" ]; then
            echo -e "${GREEN}✓ ${jar_name} 已存在，跳过下载${NC}"
            return 0
        fi
        echo -e "${YELLOW}⬇️  下载 ${jar_name}...${NC}"
        python3 -c "
import urllib.request, sys
url = '${url}'
dest = '${dest}'
try:
    urllib.request.urlretrieve(url, dest)
    import os
    size = os.path.getsize(dest)
    print(f'  已下载: {size:,} bytes')
except Exception as e:
    print(f'  下载失败: {e}', file=sys.stderr)
    sys.exit(1)
"
        if [ $? -eq 0 ]; then
            echo -e "${GREEN}✓ ${jar_name} 下载完成${NC}"
        else
            echo -e "${RED}✗ ${jar_name} 下载失败，请手动下载到 ${PLUGINS_DIR}/${NC}"
        fi
    }

    echo -e "${YELLOW}🔍 检查 Neo4j 插件...${NC}"
    download_plugin "$GDS_JAR" \
        "https://github.com/neo4j/graph-data-science/releases/download/${GDS_VERSION}/neo4j-graph-data-science-${GDS_VERSION}.jar"
    download_plugin "$SPATIAL_JAR" \
        "https://github.com/neo4j-contrib/spatial/releases/download/${NEO4J_VERSION}/neo4j-spatial-${NEO4J_VERSION}-server-plugin.jar"
    echo ""

    if docker ps --filter "name=business-neo4j" --format '{{.Status}}' | grep -q "Up"; then
        echo -e "${GREEN}✓ Neo4j 已运行，跳过启动${NC}"
    else
        docker compose up -d neo4j
        echo -e "${GREEN}✓ Neo4j 已启动${NC}"
    fi
fi

# MySQL
if [ "$ENABLE_MYSQL" = true ]; then
    docker compose up -d mysql
    echo -e "${GREEN}✓ MySQL 配置已同步${NC}"
fi

# OceanBase CE
if [ "$ENABLE_OCEANBASE" = true ]; then
    docker compose up -d oceanbase
    echo -e "${GREEN}✓ OceanBase CE 配置已同步${NC}"
fi

# Redpanda
if [ "$ENABLE_REDPANDA" = true ]; then
    docker compose up -d business-redpanda
    echo -e "${GREEN}✓ Redpanda 配置已同步${NC}"
fi

# NFS（macOS 内置 NFS）
if [ "$ENABLE_NFS" = true ]; then
    NFS_EXPORT_PATH="${PROJECT_ROOT}/nfs/data"
    mkdir -p "${NFS_EXPORT_PATH}/gis-data"

    if [ "$(uname -s)" != "Darwin" ]; then
        echo -e "${RED}✗ -nfs 当前仅支持 macOS 内置 NFS${NC}"
        exit 1
    fi

    if [ "$EUID" -ne 0 ]; then
        echo -e "${RED}✗ 配置 macOS NFS 需要 sudo 权限${NC}"
        echo -e "${YELLOW}请使用: sudo bash scripts/start.sh -nfs${NC}"
        exit 1
    fi

    EXPORTS_FILE="/etc/exports"
    OLD_EXPORT="${PROJECT_ROOT}/nas-data"
    NEW_EXPORT="${NFS_EXPORT_PATH}"

    if [ -f "${EXPORTS_FILE}" ]; then
        sed -i '' "s|${OLD_EXPORT}|${NEW_EXPORT}|g" "${EXPORTS_FILE}"
    fi

    if ! grep -Fq "${NEW_EXPORT} -alldirs -mapall=501 -noresvport" "${EXPORTS_FILE}" 2>/dev/null; then
        echo "${NEW_EXPORT} -alldirs -mapall=501 -noresvport" >> "${EXPORTS_FILE}"
    fi

    nfsd restart >/dev/null 2>&1 || nfsd enable >/dev/null 2>&1 || true
    nfsd checkexports >/dev/null 2>&1 || true

    echo -e "${GREEN}✓ macOS NFS 导出已配置并重载${NC}"
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

if [ "$ENABLE_ORACLE" = true ]; then
    ORACLE_READY=false
    for i in {1..120}; do
        if docker inspect --format '{{.State.Health.Status}}' business-oracle 2>/dev/null | grep -q '^healthy$'; then
            echo -e "${GREEN}✓ Oracle 就绪${NC}"
            ORACLE_READY=true
            break
        fi
        sleep 2
    done
    if [ "$ORACLE_READY" != true ]; then
        echo -e "${RED}✗ Oracle 未在 240 秒内就绪${NC}"
        exit 1
    fi
    bash "${PROJECT_ROOT}/oracle/test-data.sh"
fi

if [ "$ENABLE_SUPERMAP_PG" = true ]; then
    SUPERMAP_PG_READY=false
    for i in {1..30}; do
        if docker exec business-supermap-postgresql pg_isready \
            -U "${SUPERMAP_POSTGRESQL_USER:-supermap}" \
            -d "${SUPERMAP_POSTGRESQL_DB:-supermap}" >/dev/null 2>&1; then
            echo -e "${GREEN}✓ SuperMap PostgreSQL 就绪${NC}"
            SUPERMAP_PG_READY=true
            break
        fi
        sleep 1
    done
    if [ "$SUPERMAP_PG_READY" != true ]; then
        echo -e "${RED}✗ SuperMap PostgreSQL 未在 30 秒内就绪${NC}"
        exit 1
    fi
    if docker exec business-supermap-postgresql psql \
        -U "${SUPERMAP_POSTGRESQL_USER:-supermap}" \
        -d "${SUPERMAP_POSTGRESQL_DB:-supermap}" -tAc \
        "SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='postgis');" | grep -qx 't'; then
        echo -e "${RED}✗ SuperMap SDX+ for PostgreSQL 专用实例禁止安装 PostGIS${NC}"
        exit 1
    fi
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

if [ "$ENABLE_CLICKHOUSE" = true ]; then
    for i in {1..30}; do
        if docker exec business-clickhouse clickhouse-client --query 'SELECT 1' >/dev/null 2>&1; then
            echo -e "${GREEN}✓ ClickHouse 就绪${NC}"
            break
        fi
        sleep 1
    done
fi

if [ "$ENABLE_MONGODB" = true ]; then
    for i in {1..30}; do
        if docker exec business-mongodb mongosh --eval "db.adminCommand('ping')" >/dev/null 2>&1; then
            echo -e "${GREEN}✓ MongoDB 就绪${NC}"
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
    if ! bash "${PROJECT_ROOT}/spark/init-test-data.sh"; then
        echo -e "${RED}✗ Spark 样例数据初始化失败${NC}"
        docker exec business-spark-master cat /tmp/addp-spark-sample-init.log 2>/dev/null || true
        exit 1
    fi
    echo -e "${GREEN}✓ Spark 样例数据已真实查询验证${NC}"
fi

if [ "$ENABLE_NEO4J" = true ]; then
    echo -e "${YELLOW}等待 Neo4j 启动 (可能需要 30-60 秒)...${NC}"
    for i in {1..60}; do
        if curl -sf http://localhost:${NEO4J_HTTP_PORT_VAL} >/dev/null 2>&1; then
            echo -e "${GREEN}✓ Neo4j 就绪${NC}"
            break
        fi
        sleep 1
    done
fi

if [ "$ENABLE_NFS" = true ]; then
    NFS_EXPORT_PATH="${PROJECT_ROOT}/nfs/data"
    if [ -d "${NFS_EXPORT_PATH}" ]; then
        echo -e "${GREEN}✓ NFS 就绪${NC}"
    else
        echo -e "${YELLOW}⚠️  NFS 导出目录不存在: ${NFS_EXPORT_PATH}${NC}"
    fi
fi

if [ "$ENABLE_MYSQL" = true ]; then
    MYSQL_READY=false
    for i in {1..30}; do
        if docker exec business-mysql mysql -h127.0.0.1 -u root -p"${MYSQL_ROOT_PASSWORD:-password}" --connect-timeout=5 -e "SELECT 1" >/dev/null 2>&1; then
            echo -e "${GREEN}✓ MySQL 就绪${NC}"
            MYSQL_READY=true
            break
        fi
        sleep 1
    done
    if [ "$MYSQL_READY" != true ]; then
        echo -e "${RED}✗ MySQL 未在 30 秒内就绪${NC}"
        exit 1
    fi

    bash mysql/init-cdc.sh
fi

if [ "$ENABLE_OCEANBASE" = true ]; then
    OCEANBASE_READY=false
    echo -e "${YELLOW}等待 OceanBase CE 启动 (首次可能需要数分钟)...${NC}"
    for i in {1..180}; do
        if docker exec business-oceanbase obclient \
            -h127.0.0.1 -P2881 \
            -u"root@${OCEANBASE_TENANT_NAME:-test}" \
            --password="${OCEANBASE_PASSWORD:-business_oceanbase_password}" \
            --default-character-set=utf8mb4 \
            -e "SELECT 1" >/dev/null 2>&1; then
            docker exec business-oceanbase obclient \
                -h127.0.0.1 -P2881 \
                -u"root@${OCEANBASE_TENANT_NAME:-test}" \
                --password="${OCEANBASE_PASSWORD:-business_oceanbase_password}" \
                --default-character-set=utf8mb4 \
                -e "CREATE DATABASE IF NOT EXISTS \`${OCEANBASE_DATABASE:-business}\`"
            docker exec -i business-oceanbase obclient \
                -h127.0.0.1 -P2881 \
                -u"root@${OCEANBASE_TENANT_NAME:-test}" \
                --password="${OCEANBASE_PASSWORD:-business_oceanbase_password}" \
                --default-character-set=utf8mb4 \
                -D"${OCEANBASE_DATABASE:-business}" < oceanbase/init.sql
            docker exec business-oceanbase obclient \
                -h127.0.0.1 -P2881 \
                -u"root@${OCEANBASE_TENANT_NAME:-test}" \
                --password="${OCEANBASE_PASSWORD:-business_oceanbase_password}" \
                --default-character-set=utf8mb4 \
                -D"${OCEANBASE_DATABASE:-business}" \
                -e "SELECT COUNT(*) FROM addp_engine_probe" >/dev/null
            echo -e "${GREEN}✓ OceanBase CE 就绪且样例数据可查询${NC}"
            OCEANBASE_READY=true
            break
        fi
        sleep 2
    done
    if [ "$OCEANBASE_READY" != true ]; then
        echo -e "${RED}✗ OceanBase CE 未在 360 秒内完成初始化${NC}"
        exit 1
    fi
fi

if [ "$ENABLE_ORACLE" = true ]; then
    ORACLE_READY=false
    for i in {1..120}; do
        if docker exec business-oracle healthcheck.sh >/dev/null 2>&1; then
            echo -e "${GREEN}✓ Oracle 就绪${NC}"
            ORACLE_READY=true
            break
        fi
        sleep 2
    done
    if [ "$ORACLE_READY" != true ]; then
        echo -e "${RED}✗ Oracle 未在 240 秒内就绪${NC}"
        exit 1
    fi

    bash oracle/init-cdc.sh
    bash oracle/test-data.sh
fi

if [ "$ENABLE_REDPANDA" = true ]; then
    REDPANDA_READY=false
    for i in {1..60}; do
        if docker inspect --format '{{.State.Health.Status}}' business-redpanda 2>/dev/null | grep -q '^healthy$'; then
            echo -e "${GREEN}✓ Redpanda 就绪${NC}"
            REDPANDA_READY=true
            break
        fi
        sleep 1
    done
    if [ "$REDPANDA_READY" != true ]; then
        echo -e "${RED}✗ Redpanda 未在 60 秒内就绪${NC}"
        exit 1
    fi

    ADMIN_ARGS=(
        -X brokers=localhost:29092
        -X "user=${BUSINESS_KAFKA_ADMIN_USERNAME:-admin}"
        -X "pass=${BUSINESS_KAFKA_ADMIN_PASSWORD:-addp_business_kafka_admin}"
        -X sasl.mechanism=SCRAM-SHA-256
    )
    READER_USERNAME="${BUSINESS_KAFKA_READER_USERNAME:-addp_transfer}"
    READER_PASSWORD="${BUSINESS_KAFKA_READER_PASSWORD:-addp_business_kafka_reader}"
    EXISTING_USERS="$(docker exec business-redpanda rpk security user list "${ADMIN_ARGS[@]}")"
    if awk 'NR > 1 {print $1}' <<<"${EXISTING_USERS}" | grep -Fxq "${READER_USERNAME}"; then
        docker exec business-redpanda rpk security user update "${READER_USERNAME}" \
            --new-password "${READER_PASSWORD}" --mechanism SCRAM-SHA-256 "${ADMIN_ARGS[@]}" >/dev/null
    else
        docker exec business-redpanda rpk security user create "${READER_USERNAME}" \
            --password "${READER_PASSWORD}" --mechanism SCRAM-SHA-256 "${ADMIN_ARGS[@]}" >/dev/null
    fi
    docker exec business-redpanda rpk acl create --allow-principal "User:${READER_USERNAME}" \
        --topic '*' --operation read,describe "${ADMIN_ARGS[@]}" >/dev/null
    docker exec business-redpanda rpk acl create --allow-principal "User:${READER_USERNAME}" \
        --group '*' --operation read,describe "${ADMIN_ARGS[@]}" >/dev/null
    docker exec business-redpanda rpk acl create --allow-principal "User:${READER_USERNAME}" \
        --cluster --operation describe "${ADMIN_ARGS[@]}" >/dev/null
    echo -e "${GREEN}✓ Redpanda 只读 Engine 账号已同步${NC}"
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
if [ "$ENABLE_ORACLE" = true ]; then
    echo -e "Oracle: localhost:${ORACLE_PORT_VAL} (service: ${ORACLE_SERVICE_NAME:-FREEPDB1}, CDC 用户: ${ORACLE_CDC_USER:-C##ADDP_CDC})"
fi
if [ "$ENABLE_SUPERMAP_PG" = true ]; then
    echo -e "SuperMap SDX+ for PostgreSQL: localhost:${SUPERMAP_PG_PORT}"
fi
if [ "$ENABLE_MINIO" = true ]; then
    echo -e "MinIO API: http://localhost:${MINIO_API}"
    echo -e "MinIO Console: http://localhost:${MINIO_CONSOLE}"
fi
if [ "$ENABLE_CLICKHOUSE" = true ]; then
    echo -e "ClickHouse (Native): localhost:${CLICKHOUSE_PORT}"
    echo -e "ClickHouse (HTTP): http://localhost:${CLICKHOUSE_HTTP_PORT}"
fi
if [ "$ENABLE_MONGODB" = true ]; then
    echo -e "MongoDB: localhost:${MONGO_PORT}"
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
if [ "$ENABLE_NEO4J" = true ]; then
    echo -e "Neo4j Browser: http://localhost:${NEO4J_HTTP_PORT_VAL}"
    echo -e "Neo4j Bolt: bolt://localhost:${NEO4J_BOLT_PORT_VAL}"
fi
if [ "$ENABLE_MYSQL" = true ]; then
    echo -e "MySQL: localhost:${MYSQL_PORT_VAL:-3306}  (CDC 用户: ${MYSQL_CDC_USER:-addp_cdc})"
fi
if [ "$ENABLE_OCEANBASE" = true ]; then
    echo -e "OceanBase CE: localhost:${OCEANBASE_PORT_VAL} (database: ${OCEANBASE_DATABASE:-business}, user: root@${OCEANBASE_TENANT_NAME:-test})"
fi
if [ "$ENABLE_REDPANDA" = true ]; then
    echo -e "Kafka API: localhost:${BUSINESS_KAFKA_PORT_VAL}  (Engine 用户: ${BUSINESS_KAFKA_READER_USERNAME:-addp_transfer})"
fi
if [ "$ENABLE_NFS" = true ]; then
    echo -e "NFS Server: localhost:2049 (export: ${PROJECT_ROOT}/nfs/data)"
fi

echo ""
echo -e "查看日志: docker-compose logs -f"
echo -e "停止服务: bash scripts/stop.sh"
