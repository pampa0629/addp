#!/usr/bin/env bash

# ADDP Infrastructure Status Script
# 查看 ADDP 基础设施状态并做快速健康检查

set -euo pipefail

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

if [ -f ./.env ]; then
  set -a
  # shellcheck disable=SC1091
  source ./.env || true
  set +a
fi

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"; exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"; exit 1
fi

echo -e "${YELLOW}▶ 容器状态（docker compose ps）${NC}"
docker compose -f docker-compose.infra.yml ps postgres redis minio meilisearch redpanda redpanda-init kafka-connect || true

echo ""
echo -e "${YELLOW}▶ 健康检查${NC}"

# Resolve actual mapped ports
get_port() {
  local svc="$1"; local p="$2"
  docker compose -f docker-compose.infra.yml port "$svc" "$p" 2>/dev/null | sed 's/.*://'
}

PG_PORT=$(get_port postgres 5432 || echo 15432)
REDIS_PORT=$(get_port redis 6379 || echo 16379)
MINIO_API_PORT=$(get_port minio 9000 || echo 19000)
MINIO_CONSOLE_PORT=$(get_port minio 9001 || echo 19001)
KAFKA_PORT=$(get_port redpanda 9092 || echo 19092)
KAFKA_CONNECT_PORT=$(get_port kafka-connect 8083 || echo 18083)

# Postgres
printf "%s" "- PostgreSQL (localhost:${PG_PORT}):  "
if docker compose -f docker-compose.infra.yml exec -T postgres pg_isready -U addp >/dev/null 2>&1; then
  echo -e "${GREEN}Healthy${NC}"
else
  echo -e "${RED}Unhealthy${NC}"
fi

# Redis
printf "%s" "- Redis (localhost:${REDIS_PORT}):       "
if docker compose -f docker-compose.infra.yml exec -T redis redis-cli -a "${REDIS_PASSWORD:-addp_redis}" ping 2>/dev/null | grep -q PONG; then
  echo -e "${GREEN}Healthy${NC}"
else
  echo -e "${RED}Unhealthy${NC}"
fi

# MinIO
printf "%s" "- MinIO (API:${MINIO_API_PORT}/Console:${MINIO_CONSOLE_PORT}):    "
if curl -sf "http://localhost:${MINIO_API_PORT}/minio/health/live" >/dev/null 2>&1; then
  echo -e "${GREEN}Healthy${NC}"
else
  echo -e "${RED}Unhealthy${NC}"
fi

# Meilisearch
MEILI_PORT=$(get_port meilisearch 7700 || echo 17700)
printf "%s" "- Meilisearch (localhost:${MEILI_PORT}):  "
if curl -sf "http://localhost:${MEILI_PORT}/health" >/dev/null 2>&1; then
  echo -e "${GREEN}Healthy${NC}"
else
  echo -e "${RED}Unhealthy${NC}"
fi

# Infra Kafka
printf "%s" "- Infra Kafka / Redpanda (localhost:${KAFKA_PORT}):  "
KAFKA_HEALTH=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' addp-redpanda 2>/dev/null || true)
if [ "$KAFKA_HEALTH" = "healthy" ]; then
  echo -e "${GREEN}Healthy${NC}"
else
  echo -e "${RED}Unhealthy${NC}"
fi

if [ "$KAFKA_HEALTH" = "healthy" ]; then
  KAFKA_DISK_PERCENT=$(docker exec addp-redpanda df -P /var/lib/redpanda/data 2>/dev/null | awk 'NR==2 {gsub(/%/, "", $5); print $5}')
  KAFKA_DISK_DEGRADED=${INFRA_KAFKA_DISK_DEGRADED_PERCENT:-75}
  KAFKA_DISK_CRITICAL=${INFRA_KAFKA_DISK_CRITICAL_PERCENT:-85}
  if [ -n "$KAFKA_DISK_PERCENT" ] && [ "$KAFKA_DISK_PERCENT" -ge "$KAFKA_DISK_CRITICAL" ]; then
    echo -e "  Disk: ${RED}${KAFKA_DISK_PERCENT}% critical${NC}"
  elif [ -n "$KAFKA_DISK_PERCENT" ] && [ "$KAFKA_DISK_PERCENT" -ge "$KAFKA_DISK_DEGRADED" ]; then
    echo -e "  Disk: ${YELLOW}${KAFKA_DISK_PERCENT}% degraded${NC}"
  elif [ -n "$KAFKA_DISK_PERCENT" ]; then
    echo -e "  Disk: ${GREEN}${KAFKA_DISK_PERCENT}% healthy${NC}"
  fi
fi

# Kafka Connect
printf "%s" "- Kafka Connect (localhost:${KAFKA_CONNECT_PORT}): "
if curl -sf "http://localhost:${KAFKA_CONNECT_PORT}/connectors" >/dev/null 2>&1; then
  echo -e "${GREEN}Healthy${NC}"
  CONNECTORS=$(curl -sf "http://localhost:${KAFKA_CONNECT_PORT}/connectors" || echo '[]')
  if command -v jq >/dev/null 2>&1 && [ "$(jq 'length' <<<"$CONNECTORS")" -gt 0 ]; then
    while IFS= read -r connector; do
      status=$(curl -sf "http://localhost:${KAFKA_CONNECT_PORT}/connectors/${connector}/status" || true)
      connector_state=$(jq -r '.connector.state // "unknown"' <<<"$status")
      task_states=$(jq -r '[.tasks[]?.state] | if length == 0 then "none" else join(",") end' <<<"$status")
      echo "  - ${connector}: connector=${connector_state}, tasks=${task_states}"
    done < <(jq -r '.[]' <<<"$CONNECTORS")
  else
    echo "  - connectors: none"
  fi
else
  echo -e "${RED}Unhealthy${NC}"
fi

if docker ps --filter name='^/business-postgres$' --format '{{.Names}}' | grep -q '^business-postgres$'; then
  echo ""
  echo -e "${YELLOW}▶ 本地业务 PostgreSQL logical replication${NC}"
  docker exec business-postgres psql -U "${BUSINESS_PG_USER:-business}" -d "${BUSINESS_PG_DB:-business}" -P pager=off -c "
    SELECT slot_name,
           active,
           pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS retained_wal
    FROM pg_replication_slots
    ORDER BY slot_name;
  " 2>/dev/null || echo -e "${RED}无法读取本地业务 PostgreSQL replication slot${NC}"
fi
