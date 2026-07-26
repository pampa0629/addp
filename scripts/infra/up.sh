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

compose() {
  docker compose "${COMPOSE_FILES[@]}" "$@"
}

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Infrastructure Up${NC}"
echo -e "${BLUE}(PostgreSQL/Redis/MinIO/Meilisearch/Redpanda/Kafka Connect)${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

if [ -f ./.env ]; then
  set -a
  # shellcheck disable=SC1091
  source ./.env || true
  set +a
fi

# Detect CPU architecture and set PostgreSQL image
# Note: docker-compose.infra.yml has default value, only override if needed for compatibility
if [ -z "${POSTGRES_IMAGE:-}" ]; then
    ARCH=$(uname -m)
    case "${ARCH}" in
        x86_64)
            # AMD64: 暂无预构建镜像，需要本地构建
            export POSTGRES_IMAGE="addp-postgres-pgvector:latest"
            echo -e "${YELLOW}🏗️  检测到 AMD64 架构${NC}"
            echo -e "${YELLOW}    如需使用，请先构建本地镜像: docker build -f scripts/infra/Dockerfile.postgres -t addp-postgres-pgvector:latest scripts/infra/${NC}"
            ;;
        aarch64|arm64)
            # ARM64: 使用 Docker Hub 预构建镜像
            export POSTGRES_IMAGE="pampa0629/addp-postgres:15-arm64"
            echo -e "${YELLOW}🏗️  检测到 ARM64 架构，使用预构建镜像: ${POSTGRES_IMAGE}${NC}"
            ;;
        *)
            echo -e "${YELLOW}⚠️  未知架构 ${ARCH}，使用默认镜像${NC}"
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
PG_PORT=15432
REDIS_PORT=16379
MINIO_API_PORT=19000
MINIO_CONSOLE_PORT=19001
MEILISEARCH_PORT=17700
KAFKA_PORT=19092
KAFKA_CONNECT_PORT=18083

# Track if all services are already running (for idempotency)
ALL_SERVICES_RUNNING=true

port_used_by_container() {
  local port="$1"; local name="$2"
  local ports
  ports=$(docker ps --filter name="^/${name}$" --format '{{.Ports}}' 2>/dev/null || true)
  # 支持范围起始 (:9000-> 或 :9000-9001->), 范围结束 (-9001->), 单端口 (:9000->)
  if echo "$ports" | grep -qE "(^|:)${port}(-[0-9]+)?->|[0-9]+-${port}->"; then
    return 0
  fi
  return 1
}

legacy_override="docker-compose.override.autogen.yml"
[ -f "$legacy_override" ] && echo -e "${YELLOW}发现历史覆盖文件 $legacy_override，将忽略该文件（推荐删除）。${NC}"

# PostgreSQL（固定 15432）
if [ "$(check_port "$PG_PORT")" = "busy" ]; then
  if port_used_by_container "$PG_PORT" "addp-postgres"; then
    echo -e "  ${GREEN}✓ PostgreSQL 端口 ${PG_PORT} 已由 addp-postgres 容器使用${NC}"
  else
    echo -e "  ${RED}✗ PostgreSQL 端口 ${PG_PORT} 被其他进程占用${NC}"
    echo -e "    建议：使用 lsof -nP -i :${PG_PORT} 查占用并释放，或临时调整 docker-compose.yml 端口映射。"
    exit 1
  fi
else
  echo -e "  ${GREEN}✓ PostgreSQL 端口 ${PG_PORT} 可用${NC}"
  ALL_SERVICES_RUNNING=false
fi

# Redis（固定 16379）
if [ "$(check_port "$REDIS_PORT")" = "busy" ]; then
  if port_used_by_container "$REDIS_PORT" "addp-redis"; then
    echo -e "  ${GREEN}✓ Redis 端口 ${REDIS_PORT} 已由 addp-redis 容器使用${NC}"
  else
    echo -e "  ${RED}✗ Redis 端口 ${REDIS_PORT} 被其他进程占用${NC}"
    echo -e "    建议：使用 lsof -nP -i :${REDIS_PORT} 查占用并释放，或临时调整 docker-compose.yml 端口映射。"
    exit 1
  fi
else
  echo -e "  ${GREEN}✓ Redis 端口 ${REDIS_PORT} 可用${NC}"
  ALL_SERVICES_RUNNING=false
fi

# MinIO（固定 19000/19001）
if [ "$(check_port "$MINIO_API_PORT")" = "busy" ] || [ "$(check_port "$MINIO_CONSOLE_PORT")" = "busy" ]; then
  if port_used_by_container "$MINIO_API_PORT" "addp-minio" || port_used_by_container "$MINIO_CONSOLE_PORT" "addp-minio"; then
    echo -e "  ${GREEN}✓ MinIO 端口 ${MINIO_API_PORT}/${MINIO_CONSOLE_PORT} 已由 addp-minio 容器使用${NC}"
  elif port_used_by_container "$MINIO_API_PORT" "business-minio" || port_used_by_container "$MINIO_CONSOLE_PORT" "business-minio"; then
    echo -e "  ${RED}✗ 检测到 business-minio 使用了系统保留端口 ${MINIO_API_PORT}/${MINIO_CONSOLE_PORT}${NC}"
    echo -e "    请到 business/.env 将 BUSINESS_MINIO_API_PORT/BUSINESS_MINIO_CONSOLE_PORT 改为 9002/9003，然后重启 business。"
    exit 1
  else
    echo -e "  ${RED}✗ MinIO 端口 ${MINIO_API_PORT}/${MINIO_CONSOLE_PORT} 被其他进程占用${NC}"
    echo -e "    建议：使用 lsof -nP -i :${MINIO_API_PORT},${MINIO_CONSOLE_PORT} 查占用并释放。"
    exit 1
  fi
else
  echo -e "  ${GREEN}✓ MinIO 端口 ${MINIO_API_PORT}/${MINIO_CONSOLE_PORT} 可用${NC}"
  ALL_SERVICES_RUNNING=false
fi

# Infra Kafka（固定 19092）
if [ "$(check_port "$KAFKA_PORT")" = "busy" ]; then
  if port_used_by_container "$KAFKA_PORT" "addp-redpanda"; then
    echo -e "  ${GREEN}✓ Infra Kafka 端口 ${KAFKA_PORT} 已由 addp-redpanda 容器使用${NC}"
  else
    echo -e "  ${RED}✗ Infra Kafka 端口 ${KAFKA_PORT} 被其他进程占用${NC}"
    echo -e "    建议：使用 lsof -nP -i :${KAFKA_PORT} 查占用并释放。"
    exit 1
  fi
else
  echo -e "  ${GREEN}✓ Infra Kafka 端口 ${KAFKA_PORT} 可用${NC}"
  ALL_SERVICES_RUNNING=false
fi

# Kafka Connect REST（固定 18083）
if [ "$(check_port "$KAFKA_CONNECT_PORT")" = "busy" ]; then
  if port_used_by_container "$KAFKA_CONNECT_PORT" "addp-kafka-connect"; then
    echo -e "  ${GREEN}✓ Kafka Connect 端口 ${KAFKA_CONNECT_PORT} 已由 addp-kafka-connect 容器使用${NC}"
  else
    echo -e "  ${RED}✗ Kafka Connect 端口 ${KAFKA_CONNECT_PORT} 被其他进程占用${NC}"
    echo -e "    建议：使用 lsof -nP -i :${KAFKA_CONNECT_PORT} 查占用并释放。"
    exit 1
  fi
else
  echo -e "  ${GREEN}✓ Kafka Connect 端口 ${KAFKA_CONNECT_PORT} 可用${NC}"
  ALL_SERVICES_RUNNING=false
fi

echo -e "${YELLOW}▶ 检查 Docker 镜像...${NC}"

# Images to check
IMAGES=(
  "${POSTGRES_IMAGE:-postgis/postgis:15-3.4}"
  "redis:7-alpine"
  "minio/minio:latest"
  "getmeili/meilisearch:v1.7"
  "${REDPANDA_IMAGE:-docker.redpanda.com/redpandadata/redpanda:v24.3.18}"
  "${KAFKA_CONNECT_IMAGE:-quay.io/debezium/connect:3.6.0.Final}"
)

for image in "${IMAGES[@]}"; do
  if ! docker image inspect "$image" >/dev/null 2>&1; then
    echo -e "  ${BLUE}拉取镜像: $image${NC}"
    docker pull "$image"
  else
    echo -e "  ${GREEN}✓ $image 已存在${NC}"
  fi
done

echo ""
echo -e "${YELLOW}▶ 检查服务运行状态...${NC}"

# Check if services are already running
RUNNING_SERVICES=$(compose ps --status running --format "{{.Service}}" 2>/dev/null || true)

if echo "$RUNNING_SERVICES" | grep -qE "postgres|redis|minio|meilisearch|redpanda|kafka-connect"; then
  echo -e "  ${GREEN}检测到部分服务已在运行${NC}"
  echo "  运行中的服务:"
  for svc in postgres redis minio meilisearch redpanda kafka-connect; do
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

# MinIO
printf "%s" "- MinIO      "
for i in $(seq 1 ${max_wait}); do
  if curl -sf http://localhost:19000/minio/health/live >/dev/null 2>&1; then
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
  if curl -sf http://localhost:17700/health >/dev/null 2>&1; then
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
echo "  - PostgreSQL:  localhost:15432  user=addp  password=addp_password  db=addp"
echo "  - Redis:       localhost:16379  password=addp_redis"
echo "  - MinIO API:   http://localhost:19000  user=${MINIO_ROOT_USER:-minioadmin}  password=${MINIO_ROOT_PASSWORD:-minioadmin}"
echo "  - MinIO Console:http://localhost:19001"
echo "  - Meilisearch: http://localhost:17700  master_key=${MEILISEARCH_MASTER_KEY:-未设置}"
echo "  - Infra Kafka: localhost:19092  Redpanda / SASL_PLAINTEXT/SCRAM-SHA-256（内部使用）"
echo "  - Kafka Connect:http://localhost:18083  （内部控制面）"
echo ""
echo -e "${YELLOW}提示：修改默认密码可通过根目录 .env 覆盖相应变量。${NC}"
