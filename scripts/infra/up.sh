#!/usr/bin/env bash

# ADDP Infrastructure Up Script
# 以容器方式拉起 ADDP 基础设施，并做健康检查

set -euo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

COMPOSE_FILES=(-f docker-compose.infra.yml)
BUILD_REPOSITORY_POSTGRES_IMAGE=false

compose() {
  docker compose "${COMPOSE_FILES[@]}" "$@"
}

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Infrastructure Up${NC}"
echo -e "${BLUE}(PostgreSQL/Redis/FalkorDB/MinIO/Meilisearch/Redpanda/Kafka Connect)${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

if [ -f ./.env ]; then
  set -a
  # shellcheck disable=SC1091
  source ./.env || true
  set +a
fi

if [ -z "${INFRA_FALKORDB_PASSWORD:-}" ]; then
  echo -e "${RED}✗ 请在根 .env 设置独立的 INFRA_FALKORDB_PASSWORD${NC}"
  exit 1
fi
if [ "${INFRA_FALKORDB_PASSWORD}" = "${REDIS_PASSWORD:-}" ]; then
  echo -e "${RED}✗ FalkorDB 与 Redis 密码不得复用${NC}"
  exit 1
fi

# Detect CPU architecture and select the single supported PostgreSQL image path.
if [ -z "${POSTGRES_IMAGE:-}" ]; then
    ARCH=$(uname -m)
    case "${ARCH}" in
        x86_64)
            # AMD64: 使用仓库 Dockerfile 构建包含 pgvector 的本地镜像
            export POSTGRES_IMAGE="addp-postgres-pgvector:latest"
            BUILD_REPOSITORY_POSTGRES_IMAGE=true
            echo -e "${YELLOW}🏗️  检测到 AMD64 架构${NC}"
            echo -e "${YELLOW}    使用仓库 PostgreSQL 镜像: ${POSTGRES_IMAGE}${NC}"
            ;;
        aarch64|arm64)
            # ARM64: 使用 Docker Hub 预构建镜像
            export POSTGRES_IMAGE="pampa0629/addp-postgres:15-arm64"
            echo -e "${YELLOW}🏗️  检测到 ARM64 架构，使用预构建镜像: ${POSTGRES_IMAGE}${NC}"
            ;;
        *)
            echo -e "${RED}✗ 不支持的 CPU 架构: ${ARCH}；请显式设置 POSTGRES_IMAGE${NC}"
            exit 1
            ;;
    esac
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

source "${SCRIPT_DIR}/ports.sh"
echo -e "${YELLOW}▶ 解析 ADDP Infra 宿主机端口${NC}"
addp_infra_resolve_ports
export ADDP_INFRA_RESOLVED=1
ALL_SERVICES_RUNNING=false
if addp_infra_ready; then
  ALL_SERVICES_RUNNING=true
fi

echo "  PostgreSQL: ${POSTGRES_PORT}  Redis: ${REDIS_PORT}  FalkorDB: ${FALKORDB_PORT}"
echo "  MinIO: ${MINIO_API_PORT}/${MINIO_CONSOLE_PORT}  Meilisearch: ${MEILISEARCH_PORT}"
echo "  Infra Kafka: ${INFRA_KAFKA_PORT}  Kafka Connect: ${KAFKA_CONNECT_PORT}"
echo ""

echo -e "${YELLOW}▶ 检查 Docker 镜像...${NC}"

if [ "$BUILD_REPOSITORY_POSTGRES_IMAGE" = "true" ] &&
  ! docker image inspect "$POSTGRES_IMAGE" >/dev/null 2>&1; then
  echo -e "  ${BLUE}构建仓库 PostgreSQL 镜像: $POSTGRES_IMAGE${NC}"
  docker build --file scripts/infra/Dockerfile.postgres \
    --tag "$POSTGRES_IMAGE" scripts/infra
fi

# Images to check
COMPOSE_IMAGES=$(compose config --images)
while IFS= read -r image; do
  if ! docker image inspect "$image" >/dev/null 2>&1; then
    echo -e "  ${BLUE}拉取镜像: $image${NC}"
    docker pull "$image"
  else
    echo -e "  ${GREEN}✓ $image 已存在${NC}"
  fi
done <<< "$COMPOSE_IMAGES"

echo ""
echo -e "${YELLOW}▶ 检查服务运行状态...${NC}"

# Check if services are already running
RUNNING_SERVICES=$(compose ps --status running --format "{{.Service}}" 2>/dev/null || true)

if echo "$RUNNING_SERVICES" | grep -qE "postgres|redis|falkordb|minio|meilisearch|redpanda|kafka-connect"; then
  echo -e "  ${GREEN}检测到部分服务已在运行${NC}"
  echo "  运行中的服务:"
  for svc in postgres redis falkordb minio meilisearch redpanda kafka-connect; do
    if echo "$RUNNING_SERVICES" | grep -q "^${svc}$"; then
      echo -e "    ${GREEN}✓ $svc${NC}"
    fi
  done
  echo ""
  echo -e "  ${YELLOW}将确保所有服务都处于运行状态（docker compose up -d 是幂等操作）${NC}"
fi

echo ""
if [ "$ALL_SERVICES_RUNNING" = "true" ]; then
  echo -e "${GREEN}✓ 基础设施长期服务已在运行，将幂等校验一次性初始化服务${NC}"
fi
echo -e "${YELLOW}▶ 启动或校验基础设施容器${NC}"
compose up -d
addp_infra_read_actual_ports
echo ""
echo -e "${YELLOW}等待服务就绪...${NC}"

max_wait=180

# PostgreSQL
printf "%s" "- PostgreSQL "
for i in $(seq 1 ${max_wait}); do
  if compose exec -T postgres pg_isready -U addp >/dev/null 2>&1; then
    echo -e "${GREEN}✓${NC}"
    break
  fi
  sleep 1; printf "%s" "."
  if [ "$i" -eq "$max_wait" ]; then echo -e "\n${RED}✗ PostgreSQL 等待超时${NC}"; exit 1; fi
done

# Initialize PostgreSQL (pg_hba.conf + extensions + schemas)
if [[ "${SKIP_POSTGRESQL_INIT:-0}" != "1" ]]; then
  bash "${SCRIPT_DIR}/init-postgresql.sh"
else
  echo -e "${YELLOW}▶ 跳过 PostgreSQL 初始化（SKIP_POSTGRESQL_INIT=1）${NC}"
fi

# Redis
printf "%s" "- Redis      "
REDIS_PW="${REDIS_PASSWORD:-addp_redis}"
last_out=""
for i in $(seq 1 ${max_wait}); do
  out=$(compose exec -T redis sh -lc "redis-cli -h 127.0.0.1 -p 6379 -a '${REDIS_PW}' ping" 2>&1 || true)
  if echo "$out" | grep -q "PONG"; then
    echo -e "${GREEN}✓${NC}"
    break
  fi
  last_out="$out"
  sleep 1; printf "%s" "."
  if [ "$i" -eq "$max_wait" ]; then \
    echo -e "\n${RED}✗ Redis 等待超时${NC}"; \
    if [ -n "$last_out" ]; then echo "最近一次输出: $last_out"; fi; \
    echo "请检查: docker compose -f docker-compose.infra.yml logs -f redis"; \
    exit 1; \
  fi
done

# FalkorDB: healthcheck verifies authentication and the graph module budget.
printf "%s" "- FalkorDB   "
for i in $(seq 1 ${max_wait}); do
  health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}' addp-falkordb 2>/dev/null || true)
  if [ "$health" = "healthy" ]; then
    echo -e "${GREEN}✓${NC}"
    break
  fi
  sleep 1; printf "%s" "."
  if [ "$i" -eq "$max_wait" ]; then echo -e "\n${RED}✗ FalkorDB 等待超时${NC}"; exit 1; fi
done

# MinIO
printf "%s" "- MinIO      "
for i in $(seq 1 ${max_wait}); do
  if curl -sf "http://localhost:${MINIO_API_PORT}/minio/health/live" >/dev/null 2>&1; then
    echo -e "${GREEN}✓${NC}"
    break
  fi
  sleep 1; printf "%s" "."
  if [ "$i" -eq "$max_wait" ]; then echo -e "\n${RED}✗ MinIO 等待超时${NC}"; exit 1; fi
done

# Initialize MinIO buckets
if [[ "${SKIP_MINIO_INIT:-0}" != "1" ]]; then
  bash "${SCRIPT_DIR}/init-minio.sh"
else
  echo -e "${YELLOW}▶ 跳过 MinIO buckets 初始化（SKIP_MINIO_INIT=1）${NC}"
fi

# Meilisearch
printf "%s" "- Meilisearch "
for i in $(seq 1 ${max_wait}); do
  if curl -sf "http://localhost:${MEILISEARCH_PORT}/health" >/dev/null 2>&1; then
    echo -e "${GREEN}✓${NC}"
    break
  fi
  sleep 1; printf "%s" "."
  if [ "$i" -eq "$max_wait" ]; then echo -e "\n${RED}✗ Meilisearch 等待超时${NC}"; exit 1; fi
done

# Initialize Meilisearch indexes
if [[ "${SKIP_MEILISEARCH_INIT:-0}" != "1" ]]; then
  bash "${SCRIPT_DIR}/init-meilisearch.sh"
else
  echo -e "${YELLOW}▶ 跳过 Meilisearch 索引初始化（SKIP_MEILISEARCH_INIT=1）${NC}"
fi

# Infra Kafka
printf "%s" "- Infra Kafka "
for i in $(seq 1 ${max_wait}); do
  kafka_health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' addp-redpanda 2>/dev/null || true)
  if [ "$kafka_health" = "healthy" ]; then
    echo -e "${GREEN}✓${NC}"
    break
  fi
  sleep 1; printf "%s" "."
  if [ "$i" -eq "$max_wait" ]; then
    echo -e "\n${RED}✗ Infra Kafka 等待超时${NC}"
    compose logs --tail=100 redpanda || true
    exit 1
  fi
done

# Kafka internal topics and ACLs
printf "%s" "- Redpanda Init "
for i in $(seq 1 ${max_wait}); do
  init_status=$(docker inspect --format '{{.State.Status}}:{{.State.ExitCode}}' addp-redpanda-init 2>/dev/null || true)
  if [ "$init_status" = "exited:0" ]; then
    echo -e "${GREEN}✓${NC}"
    break
  fi
  if [[ "$init_status" == exited:* && "$init_status" != "exited:0" ]]; then
    echo -e "${RED}✗${NC}"
    compose logs --tail=150 redpanda-init || true
    exit 1
  fi
  sleep 1; printf "%s" "."
  if [ "$i" -eq "$max_wait" ]; then
    echo -e "\n${RED}✗ Redpanda Init 等待超时${NC}"
    compose logs --tail=150 redpanda-init || true
    exit 1
  fi
done

# Kafka Connect
printf "%s" "- Kafka Connect "
for i in $(seq 1 ${max_wait}); do
  if curl -sf "http://localhost:${KAFKA_CONNECT_PORT}/connectors" >/dev/null 2>&1; then
    echo -e "${GREEN}✓${NC}"
    break
  fi
  sleep 1; printf "%s" "."
  if [ "$i" -eq "$max_wait" ]; then
    echo -e "\n${RED}✗ Kafka Connect 等待超时${NC}"
    compose logs --tail=150 kafka-connect || true
    exit 1
  fi
done

echo ""
echo -e "${GREEN}基础设施就绪！${NC}"
echo ""
echo "访问地址与默认凭据："
echo "  - PostgreSQL:  localhost:${POSTGRES_PORT}  db=${POSTGRES_DB:-addp}"
echo "  - Redis:       localhost:${REDIS_PORT}"
echo "  - FalkorDB:    127.0.0.1:${FALKORDB_PORT}  Ontology 私有 Infra"
echo "  - MinIO API:   http://localhost:${MINIO_API_PORT}"
echo "  - MinIO Console:http://localhost:${MINIO_CONSOLE_PORT}"
echo "  - Meilisearch: http://localhost:${MEILISEARCH_PORT}"
echo "  - Infra Kafka: localhost:${INFRA_KAFKA_PORT}  Redpanda / SASL_PLAINTEXT/SCRAM-SHA-256"
echo "  - Kafka Connect:http://localhost:${KAFKA_CONNECT_PORT}  内部控制面"
echo ""
echo -e "${YELLOW}提示：修改默认密码可通过根目录 .env 覆盖相应变量。${NC}"
