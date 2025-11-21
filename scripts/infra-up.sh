#!/usr/bin/env bash

# ADDP Infrastructure Up Script
# 以容器方式拉起系统库（PostgreSQL、Redis、MinIO、Elasticsearch），并做健康检查

set -euo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Infrastructure Up (Postgres/Redis/MinIO/ES)${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Optional: load env overrides if present
if [ -f ./.env ]; then
  set -a
  # shellcheck disable=SC1091
  source ./.env || true
  set +a
fi

# Ensure DOCKER_DEFAULT_PLATFORM is set (inherited from infra-restart.sh or default)
if [ -z "${DOCKER_DEFAULT_PLATFORM:-}" ]; then
    ARCH=$(uname -m)
    case "${ARCH}" in
        x86_64)
            export DOCKER_DEFAULT_PLATFORM="linux/amd64"
            ;;
        aarch64|arm64)
            export DOCKER_DEFAULT_PLATFORM="linux/arm64"
            ;;
        armv7l)
            export DOCKER_DEFAULT_PLATFORM="linux/arm/v7"
            ;;
        *)
            echo -e "${YELLOW}⚠️  Unknown architecture ${ARCH}, using default platform${NC}"
            ;;
    esac

    if [ -n "${DOCKER_DEFAULT_PLATFORM:-}" ]; then
        echo -e "${YELLOW}🏗️  Using platform: ${DOCKER_DEFAULT_PLATFORM}${NC}"
    fi
fi

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用，请安装 Docker Desktop 或 docker-compose 插件${NC}"
  exit 1
fi

check_port() {
  local port="$1"
  if lsof -nP -i ":${port}" >/dev/null 2>&1; then
    echo "busy"
  else
    echo "free"
  fi
}

echo -e "${YELLOW}▶ 端口占用检查（固定端口，不自动改）${NC}"

# default desired ports
PG_PORT=5432
REDIS_PORT=6379
MINIO_API_PORT=9002
MINIO_CONSOLE_PORT=9003
ES_HTTP_PORT=9200
ES_TRANSPORT_PORT=9300

port_used_by_container() {
  local port="$1"; local name="$2"
  local ports
  ports=$(docker ps --filter name="^/${name}$" --format '{{.Ports}}' 2>/dev/null || true)
  if echo "$ports" | grep -q ":${port}->"; then
    return 0
  fi
  return 1
}

legacy_override="docker-compose.override.autogen.yml"
[ -f "$legacy_override" ] && echo -e "${YELLOW}发现历史覆盖文件 $legacy_override，将忽略该文件（推荐删除）。${NC}"

# PostgreSQL（固定 5432）
if [ "$(check_port "$PG_PORT")" = "busy" ]; then
  if port_used_by_container "$PG_PORT" "addp-postgres"; then
    echo -e "  ${GREEN}PostgreSQL 端口 ${PG_PORT} 已由 addp-postgres 使用，继续...${NC}"
  else
    echo -e "  ${RED}✗ PostgreSQL 端口 ${PG_PORT} 被其他进程占用${NC}"
    echo -e "    建议：使用 lsof -nP -i :${PG_PORT} 查占用并释放，或临时调整 docker-compose.yml 端口映射。"
  fi
else
  echo -e "  ${GREEN}✓ PostgreSQL 端口 ${PG_PORT} 可用${NC}"
fi

# Redis（固定 6379）
if [ "$(check_port "$REDIS_PORT")" = "busy" ]; then
  if port_used_by_container "$REDIS_PORT" "addp-redis"; then
    echo -e "  ${GREEN}Redis 端口 ${REDIS_PORT} 已由 addp-redis 使用，继续...${NC}"
  else
    echo -e "  ${RED}✗ Redis 端口 ${REDIS_PORT} 被其他进程占用${NC}"
    echo -e "    建议：使用 lsof -nP -i :${REDIS_PORT} 查占用并释放，或临时调整 docker-compose.yml 端口映射。"
  fi
else
  echo -e "  ${GREEN}✓ Redis 端口 ${REDIS_PORT} 可用${NC}"
fi

# MinIO（固定 9002/9003）
if [ "$(check_port "$MINIO_API_PORT")" = "busy" ] || [ "$(check_port "$MINIO_CONSOLE_PORT")" = "busy" ]; then
  if port_used_by_container "$MINIO_API_PORT" "addp-minio" || port_used_by_container "$MINIO_CONSOLE_PORT" "addp-minio"; then
    echo -e "  ${GREEN}MinIO 端口 ${MINIO_API_PORT}/${MINIO_CONSOLE_PORT} 已由 addp-minio 使用，继续...${NC}"
  elif port_used_by_container "$MINIO_API_PORT" "business-minio" || port_used_by_container "$MINIO_CONSOLE_PORT" "business-minio"; then
    echo -e "  ${RED}✗ 检测到 business-minio 使用了系统保留端口 ${MINIO_API_PORT}/${MINIO_CONSOLE_PORT}${NC}"
    echo -e "    请到 business/.env 将 BUSINESS_MINIO_API_PORT/BUSINESS_MINIO_CONSOLE_PORT 改为 9000/9001，然后重启 business。"
    exit 1
  else
    echo -e "  ${RED}✗ MinIO 端口 ${MINIO_API_PORT}/${MINIO_CONSOLE_PORT} 被其他进程占用${NC}"
    echo -e "    建议：使用 lsof -nP -i :${MINIO_API_PORT},${MINIO_CONSOLE_PORT} 查占用并释放。"
    exit 1
  fi
else
  echo -e "  ${GREEN}✓ MinIO 端口 ${MINIO_API_PORT}/${MINIO_CONSOLE_PORT} 可用${NC}"
fi

# Elasticsearch（固定 9200/9300）
if [ "$(check_port "$ES_HTTP_PORT")" = "busy" ] || [ "$(check_port "$ES_TRANSPORT_PORT")" = "busy" ]; then
  if port_used_by_container "$ES_HTTP_PORT" "addp-elasticsearch" || port_used_by_container "$ES_TRANSPORT_PORT" "addp-elasticsearch"; then
    echo -e "  ${GREEN}Elasticsearch 端口 ${ES_HTTP_PORT}/${ES_TRANSPORT_PORT} 已由 addp-elasticsearch 使用，继续...${NC}"
  else
    echo -e "  ${RED}✗ Elasticsearch 端口 ${ES_HTTP_PORT}/${ES_TRANSPORT_PORT} 被其他进程占用${NC}"
    echo -e "    建议：使用 lsof -nP -i :${ES_HTTP_PORT},${ES_TRANSPORT_PORT} 查占用并释放。"
  fi
else
  echo -e "  ${GREEN}✓ Elasticsearch 端口 ${ES_HTTP_PORT}/${ES_TRANSPORT_PORT} 可用${NC}"
fi

echo -e "${YELLOW}▶ 启动基础设施容器: postgres, redis, minio, elasticsearch${NC}"
docker compose up -d postgres redis minio elasticsearch

echo ""
echo -e "${YELLOW}等待服务就绪...${NC}"

max_wait=90

# PostgreSQL
printf "%s" "- PostgreSQL "
for i in $(seq 1 ${max_wait}); do
  if docker compose exec -T postgres pg_isready -U addp >/dev/null 2>&1; then
    echo -e "${GREEN}✓${NC}"
    break
  fi
  sleep 1; printf "%s" "."
  if [ "$i" -eq "$max_wait" ]; then echo -e "\n${RED}✗ PostgreSQL 等待超时${NC}"; exit 1; fi
done

if [[ "${SKIP_INFRA_DB_INIT:-0}" != "1" ]]; then
  bash "${SCRIPT_DIR}/infra-init-db.sh"
else
  echo -e "${YELLOW}▶ 跳过系统库初始化（SKIP_INFRA_DB_INIT=1）${NC}"
fi

# Install PostGIS extension (for ARM64 with postgres:15 image)
if [[ "${SKIP_POSTGIS_INSTALL:-0}" != "1" ]]; then
  # Check if using standard postgres image (needs PostGIS installation)
  CURRENT_IMAGE=$(docker inspect addp-postgres --format '{{.Config.Image}}' 2>/dev/null || echo "")
  if [[ "$CURRENT_IMAGE" == *"postgres:15"* ]] || [[ "$CURRENT_IMAGE" == "postgres:15" ]]; then
    echo -e "${YELLOW}▶ Installing PostGIS extension...${NC}"
    bash "${SCRIPT_DIR}/infra-init-postgis.sh"
  else
    echo -e "${YELLOW}▶ Skipping PostGIS installation (using PostGIS pre-installed image)${NC}"
  fi
else
  echo -e "${YELLOW}▶ 跳过 PostGIS 安装（SKIP_POSTGIS_INSTALL=1）${NC}"
fi

# Redis
printf "%s" "- Redis      "
REDIS_PW="${REDIS_PASSWORD:-addp_redis}"
last_out=""
for i in $(seq 1 ${max_wait}); do
  out=$(docker compose exec -T redis sh -lc "redis-cli -h 127.0.0.1 -p 6379 -a '${REDIS_PW}' ping" 2>&1 || true)
  if echo "$out" | grep -q "PONG"; then
    echo -e "${GREEN}✓${NC}"
    break
  fi
  last_out="$out"
  sleep 1; printf "%s" "."
  if [ "$i" -eq "$max_wait" ]; then \
    echo -e "\n${RED}✗ Redis 等待超时${NC}"; \
    if [ -n "$last_out" ]; then echo "最近一次输出: $last_out"; fi; \
    echo "请检查: docker compose logs -f redis"; \
    exit 1; \
  fi
done

# MinIO
printf "%s" "- MinIO      "
for i in $(seq 1 ${max_wait}); do
  if curl -sf http://localhost:9002/minio/health/live >/dev/null 2>&1; then
    echo -e "${GREEN}✓${NC}"
    break
  fi
  sleep 1; printf "%s" "."
  if [ "$i" -eq "$max_wait" ]; then echo -e "\n${RED}✗ MinIO 等待超时${NC}"; exit 1; fi
done

# Initialize MinIO buckets
if [[ "${SKIP_MINIO_INIT:-0}" != "1" ]]; then
  bash "${SCRIPT_DIR}/infra-init-minio.sh"
else
  echo -e "${YELLOW}▶ 跳过 MinIO buckets 初始化（SKIP_MINIO_INIT=1）${NC}"
fi

# Elasticsearch
printf "%s" "- Elastic    "
for i in $(seq 1 ${max_wait}); do
  if curl -fs "http://localhost:9200/_cluster/health?wait_for_status=yellow&timeout=2s" >/dev/null 2>&1; then
    echo -e "${GREEN}✓${NC}"
    break
  fi
  sleep 1; printf "%s" "."
  if [ "$i" -eq "$max_wait" ]; then echo -e "\n${RED}✗ Elasticsearch 等待超时${NC}"; exit 1; fi
done

echo ""
echo -e "${GREEN}基础设施就绪！${NC}"
echo ""
echo "访问地址与默认凭据："
echo "  - PostgreSQL:  localhost:5432  user=addp  password=addp_password  db=addp"
echo "  - Redis:       localhost:6379  password=addp_redis"
echo "  - MinIO API:   http://localhost:9002  user=${MINIO_ROOT_USER:-minioadmin}  password=${MINIO_ROOT_PASSWORD:-minioadmin}"
echo "  - MinIO Console:http://localhost:9003"
echo "  - Elasticsearch:http://localhost:9200 (no auth)"
echo ""
echo -e "${YELLOW}提示：修改默认密码可通过根目录 .env 覆盖相应变量。${NC}"
