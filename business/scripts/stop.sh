#!/bin/bash
# 业务库停止脚本
#
# 使用方法:
#   bash scripts/stop.sh              # 停止所有运行中的业务库容器
#   bash scripts/stop.sh -mysql       # 只停止 MySQL
#   bash scripts/stop.sh -postgres    # 只停止 PostgreSQL
#   bash scripts/stop.sh -all         # 停止所有（同无参数）

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_ROOT"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

SERVICES=()
HAS_ARGS=false

for arg in "$@"; do
    HAS_ARGS=true
    key="${arg#-}"
    case $arg in
        -all) HAS_ARGS=false; break ;;
        -h|--help)
            echo "使用方法: bash scripts/stop.sh [-postgres|-minio|-clickhouse|-mongodb|-doris|-spark|-neo4j|-mysql|-redpanda|-nfs|-all]"
            exit 0
            ;;
        -nfs)
            # NFS 是 macOS 系统服务，不通过 compose 管理，跳过
            echo -e "${YELLOW}⚠️  NFS 需手动停止: sudo nfsd stop${NC}"
            ;;
        -*)
            case "$key" in
                postgres|minio|clickhouse|mongodb|neo4j|mysql)
                    SERVICES+=("$key")
                    ;;
                redpanda)
                    SERVICES+=("business-redpanda")
                    ;;
                doris)
                    SERVICES+=("doris-fe")
                    ;;
                spark)
                    SERVICES+=("spark-master" "spark-worker-1" "spark-worker-2")
                    ;;
                *)
                    echo "未知参数: $arg"; exit 1
                    ;;
            esac
            ;;
    esac
done

echo -e "${YELLOW}🛑 停止服务...${NC}"

if [ "$HAS_ARGS" = false ] || [ ${#SERVICES[@]} -eq 0 ]; then
    # 无参数或 -all：停止所有
    docker compose down
    echo -e "${GREEN}✓ 所有业务库服务已停止${NC}"
else
    # 只停止指定服务
    docker compose rm -sf "${SERVICES[@]}"
    echo -e "${GREEN}✓ 已停止: ${SERVICES[*]}${NC}"
fi
