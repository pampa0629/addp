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
CORE_SERVICES=(postgres redis falkordb minio meilisearch redpanda redpanda-init kafka-connect)

compose() {
  # Inactive optional profiles are still interpolated by Compose. Their unused
  # ports cannot prevent core or the other capability from starting.
  case "${ADDP_INFRA_PORT_SCOPE:-core}" in
    logs) PROMETHEUS_PORT=0 docker compose "${COMPOSE_FILES[@]}" "$@" ;;
    metrics) LOKI_PORT=0 ALLOY_PORT=0 docker compose "${COMPOSE_FILES[@]}" "$@" ;;
    *) PROMETHEUS_PORT=0 LOKI_PORT=0 ALLOY_PORT=0 docker compose "${COMPOSE_FILES[@]}" "$@" ;;
  esac
}

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Infrastructure Up${NC}"
echo -e "${BLUE}(PostgreSQL/Redis/FalkorDB/MinIO/Meilisearch/Redpanda/Kafka Connect)${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

source "${PROJECT_ROOT}/scripts/utils/observability-env.sh"
if [[ "${ADDP_ONLINE_HOST:-0}" == 1 ]]; then
  : "${ADDP_ONLINE_ENV_FILE:?Online lifecycle requires ADDP_ONLINE_ENV_FILE outside the repository}"
  RUNTIME_ENV_FILE="$ADDP_ONLINE_ENV_FILE"
else
  RUNTIME_ENV_FILE="$PROJECT_ROOT/.env"
fi
addp_prepare_observability_env "$RUNTIME_ENV_FILE"

if [ -z "${INFRA_FALKORDB_PASSWORD:-}" ]; then
  echo -e "${RED}✗ 请设置独立的 INFRA_FALKORDB_PASSWORD${NC}"
  exit 1
fi
if [ "${INFRA_FALKORDB_PASSWORD}" = "${REDIS_PASSWORD:-}" ]; then
  echo -e "${RED}✗ FalkorDB 与 Redis 密码不得复用${NC}"
  exit 1
fi

# Docker must not create host bind-mount parents as root. Do not chown or
# recursively relax existing private directories belonging to another owner.
export ADDP_RUNTIME_LOG_ROOT="${ADDP_RUNTIME_LOG_ROOT:-$PROJECT_ROOT/logs/runtime}"
case "$ADDP_RUNTIME_LOG_ROOT" in /*) ;; *) export ADDP_RUNTIME_LOG_ROOT="$PROJECT_ROOT/$ADDP_RUNTIME_LOG_ROOT" ;; esac
python3 - "$PROJECT_ROOT/logs" "$ADDP_RUNTIME_LOG_ROOT" "$ADDP_RUNTIME_LOG_OWNER" <<'PYLOGDIR'
from pathlib import Path
import os, sys
for name in sys.argv[1:3]:
    path = Path(name)
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    if not os.access(path, os.R_OK | os.W_OK | os.X_OK):
        raise SystemExit('Runtime log directory is not accessible to lifecycle owner: ' + name)
owner = Path(sys.argv[2]).stat()
if sys.argv[3] != f'{owner.st_uid}:{owner.st_gid}':
    raise SystemExit('ADDP_RUNTIME_LOG_OWNER must match source directory ownership')
PYLOGDIR

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
ADDP_INFRA_PORT_SCOPE=core addp_infra_resolve_ports
export ADDP_INFRA_RESOLVED=1
ALL_SERVICES_RUNNING=false
if addp_infra_ready core; then
  ALL_SERVICES_RUNNING=true
fi

echo "  PostgreSQL: ${POSTGRES_PORT}  Redis: ${REDIS_PORT}  FalkorDB: ${FALKORDB_PORT}"
echo "  MinIO: ${MINIO_API_PORT}/${MINIO_CONSOLE_PORT}  Meilisearch: ${MEILISEARCH_PORT}"
echo "  Infra Kafka: ${INFRA_KAFKA_PORT}  Kafka Connect: ${KAFKA_CONNECT_PORT}"
echo ""

echo -e "${YELLOW}▶ 检查 Docker 镜像...${NC}"

# Compose reconciliation is not a database migration. Keep the old container
# and its volume untouched until an explicit, separately verified cutover.
existing_meilisearch=$(docker ps -aq --filter 'name=^/addp-meilisearch$')
if [ -n "$existing_meilisearch" ]; then
  addp_infra_verify_container meilisearch addp-meilisearch
  selected_meilisearch=$(compose config --images meilisearch)
  selected_meilisearch_id=$(docker image inspect --format '{{.Id}}' "$selected_meilisearch" 2>/dev/null) || {
    echo '✗ 无法核实 Meilisearch 镜像身份；已有数据库禁止自动拉取并替换镜像，请先完成显式迁移。' >&2
    exit 1
  }
  existing_meilisearch_id=$(docker inspect --format '{{.Image}}' addp-meilisearch)
  if ! [[ "$selected_meilisearch_id" =~ ^sha256:[0-9a-f]{64}$ && "$existing_meilisearch_id" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo '✗ 无法核实 Meilisearch 镜像身份；保留原容器与旧卷，请先完成显式迁移。' >&2
    exit 1
  fi
  if [ "$existing_meilisearch_id" != "$selected_meilisearch_id" ]; then
    echo '✗ Meilisearch 镜像身份不一致；拒绝普通启动替换已有数据库。请先按 scripts/infra/README.md 完成独立新卷迁移与切换，原容器和旧卷保持不动。' >&2
    exit 1
  fi
fi

if [ "$BUILD_REPOSITORY_POSTGRES_IMAGE" = "true" ] &&
  ! docker image inspect "$POSTGRES_IMAGE" >/dev/null 2>&1; then
  echo -e "  ${BLUE}构建仓库 PostgreSQL 镜像: $POSTGRES_IMAGE${NC}"
  docker build --file scripts/infra/Dockerfile.postgres \
    --tag "$POSTGRES_IMAGE" scripts/infra
fi

# Build every repository-owned service before checking remotely published images.
# Compose-generated image names are local build artifacts, never registry pulls.
compose build "${CORE_SERVICES[@]}"
# Images to check
COMPOSE_IMAGES=$(compose config --images "${CORE_SERVICES[@]}")
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
  for svc in postgres redis falkordb minio meilisearch redpanda kafka-connect loki alloy runtime-log-api runtime-log-pruner runtime-log-observer; do
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
compose up -d "${CORE_SERVICES[@]}"
ADDP_INFRA_PORT_SCOPE=core addp_infra_read_actual_ports
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

# Bounded local source retention is independent of central collection.
optional_result=0
if compose build runtime-log-pruner && compose up -d runtime-log-pruner; then
  echo 'Bounded local runtime output housekeeping started'
else
  echo 'Runtime output housekeeping unavailable; core services remain running' >&2
  optional_result=1
fi

start_central_logs() {
  local ADDP_INFRA_PORT_SCOPE=logs
  addp_runtime_log_preflight || return 1
  ADDP_INFRA_PORT_SCOPE=logs addp_infra_resolve_ports || return 1
  compose build runtime-log-observer || return 1
  local image images
  images=$(compose config --images loki alloy runtime-log-api runtime-log-observer runtime-log-store-init) || return 1
  while IFS= read -r image; do
    if ! docker image inspect "$image" >/dev/null 2>&1; then docker pull "$image" || return 1; fi
  done <<< "$images"
  compose up -d loki alloy runtime-log-api runtime-log-observer runtime-log-store-init || return 1
  ADDP_INFRA_PORT_SCOPE=logs addp_infra_read_actual_ports || return 1
  curl -fsS --max-time 5 --retry 30 --retry-delay 2 --retry-connrefused "http://127.0.0.1:${LOKI_PORT}/ready" >/dev/null || return 1
  curl -fsS --max-time 5 --retry 30 --retry-delay 2 --retry-connrefused "http://127.0.0.1:${ALLOY_PORT}/-/ready" >/dev/null || return 1
  echo 'Runtime log query and collector ready; delivery completeness requires a probe'
}

if addp_runtime_logs_enabled; then
  if ! start_central_logs; then
    echo 'Central runtime logs unavailable; core services remain running' >&2
    optional_result=1
  fi
elif [[ "${ADDP_OBSERVABILITY_LOGS_ENABLED-true}" == false ]]; then
  # Verify ownership before stopping only this workspace's unselected collectors.
  for service in runtime-log-observer alloy runtime-log-api loki runtime-log-store-init; do
    container=$(compose ps -aq "$service") || { optional_result=1; continue; }
    if [[ -n "$container" ]]; then
      if addp_infra_verify_container "$service" "$container"; then
        compose stop "$service" || optional_result=1
      else
        optional_result=1
      fi
    fi
  done
  echo 'Central runtime logs disabled; retained data volumes preserved'
else
  echo 'Central runtime logs unconfigured: ADDP_OBSERVABILITY_LOGS_ENABLED must be true or false' >&2
  optional_result=1
fi
start_metrics() {
  local ADDP_INFRA_PORT_SCOPE=metrics
  addp_metrics_preflight || return 1
  addp_infra_resolve_ports || return 1
  local image
  image=$(compose config --images prometheus) || return 1
  if ! docker image inspect "$image" >/dev/null 2>&1; then docker pull "$image" || return 1; fi
  compose run --rm --no-deps --entrypoint /bin/promtool prometheus check config /etc/prometheus/addp.yml || return 1
  compose run --rm --no-deps --entrypoint /bin/promtool prometheus check web-config /etc/prometheus/web.yml || return 1
  compose up -d --wait --wait-timeout 90 prometheus || return 1
  addp_infra_read_actual_ports || return 1
  [[ -n "${PROMETHEUS_PORT:-}" ]] || return 1
  curl -fsS --max-time 5 --cacert "$ADDP_METRICS_TLS_DIR/ca.crt" --cert "$ADDP_METRICS_TLS_DIR/health.crt" --key "$ADDP_METRICS_TLS_DIR/health.key" "https://localhost:${PROMETHEUS_PORT}/-/ready" >/dev/null || return 1
  echo "Prometheus center Ready (localhost:${PROMETHEUS_PORT}); business resource targets not yet connected"
}

if addp_metrics_enabled; then
  if ! start_metrics; then
    echo 'Metrics center unavailable; core services remain running' >&2
    optional_result=1
  fi
elif [[ "${ADDP_OBSERVABILITY_METRICS_ENABLED-false}" == false ]]; then
  container=$(compose ps -aq prometheus) || { optional_result=1; container=''; }
  if [[ -n "$container" ]]; then
    if addp_infra_verify_container prometheus "$container"; then
      compose stop prometheus || optional_result=1
    else
      optional_result=1
    fi
  fi
  echo 'Metrics center disabled; retained data volume preserved'
else
  echo 'Metrics center unconfigured: ADDP_OBSERVABILITY_METRICS_ENABLED must be true or false' >&2
  optional_result=1
fi
exit "$optional_result"
