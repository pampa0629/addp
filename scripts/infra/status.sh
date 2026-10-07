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

source "${SCRIPT_DIR}/ports.sh"
ADDP_INFRA_PORT_SCOPE=core addp_infra_read_actual_ports
echo -e "${YELLOW}▶ ADDP Infra 容器状态${NC}"
docker ps -a --filter label=com.docker.compose.project=addp-infra --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'

echo ""
echo -e "${YELLOW}▶ 健康检查${NC}"

# Resolve actual mapped ports
get_port() {
  local svc="$1"; local p="$2"
  docker port "addp-$svc" "$p/tcp" 2>/dev/null | head -n 1 | sed 's/.*://'
}

PG_PORT=$(get_port postgres 5432 || true)
REDIS_PORT=$(get_port redis 6379 || true)
MINIO_API_PORT=$(get_port minio 9000 || true)
MINIO_CONSOLE_PORT=$(get_port minio 9001 || true)
KAFKA_PORT=$(get_port redpanda 9092 || true)
KAFKA_CONNECT_PORT=$(get_port kafka-connect 8083 || true)
FALKORDB_PORT=$(get_port falkordb 6379 || true)

# Postgres
printf "%s" "- PostgreSQL (localhost:${PG_PORT}):  "
if docker exec addp-postgres pg_isready -U addp >/dev/null 2>&1; then
  echo -e "${GREEN}Healthy${NC}"
else
  echo -e "${RED}Unhealthy${NC}"
fi

# Redis
printf "%s" "- Redis (localhost:${REDIS_PORT}):       "
if docker exec addp-redis redis-cli -a "${REDIS_PASSWORD:-addp_redis}" ping 2>/dev/null | grep -q PONG; then
  echo -e "${GREEN}Healthy${NC}"
else
  echo -e "${RED}Unhealthy${NC}"
fi

# FalkorDB
printf "%s" "- FalkorDB (127.0.0.1:${FALKORDB_PORT}):          "
FALKORDB_HEALTH=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}' addp-falkordb 2>/dev/null || true)
if [ "$FALKORDB_HEALTH" = "healthy" ]; then
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
MEILI_PORT=$(get_port meilisearch 7700 || true)
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
  (
    [ -f ./business/.env ] || exit 1
    unset POSTGRES_USER POSTGRES_DB
    # shellcheck disable=SC1091
    source ./business/.env
    : "${POSTGRES_USER:?business/.env 缺少 POSTGRES_USER}"
    : "${POSTGRES_DB:?business/.env 缺少 POSTGRES_DB}"
    docker exec business-postgres psql -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -P pager=off -c "
    SELECT slot_name,
           active,
           pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS retained_wal
    FROM pg_replication_slots
    ORDER BY slot_name;
    "
  ) 2>/dev/null || echo -e "${RED}无法读取本地业务 PostgreSQL replication slot${NC}"
fi

source "$PROJECT_ROOT/scripts/utils/observability-env.sh"
case "${ADDP_OBSERVABILITY_METRICS_ENABLED-false}" in
  false) echo '- Metrics center: Disabled' ;;
  true)
    if ! addp_metrics_preflight >/dev/null 2>&1; then
      echo '- Metrics center: Unconfigured (deployment certificates)'
    elif ! ADDP_INFRA_PORT_SCOPE=metrics addp_infra_read_actual_ports || [[ -z "${PROMETHEUS_PORT:-}" ]]; then
      echo '- Metrics center: Not deployed'
    elif curl -fsS --max-time 3 --cacert "$ADDP_METRICS_TLS_DIR/ca.crt" --cert "$ADDP_METRICS_TLS_DIR/health.crt" --key "$ADDP_METRICS_TLS_DIR/health.key" "https://localhost:${PROMETHEUS_PORT}/-/ready" >/dev/null 2>&1; then
      echo '- Metrics center: Ready (resource coverage requires Monitor verification)'
    else
      echo '- Metrics center: Unavailable'
    fi ;;
  *) echo '- Metrics center: Unconfigured (invalid deployment selection)' ;;
esac

if ! addp_metrics_enabled || [[ "${ADDP_METRICS_CONTROL_ENABLED-false}" == false ]]; then
  echo '- Metrics control TLS: Disabled'
elif [[ "${ADDP_METRICS_CONTROL_ENABLED-false}" != true ]]; then
  echo '- Metrics control TLS: Unconfigured (invalid selection)'
else
  control_container=$(docker compose -f docker-compose.infra.yml ps -aq metrics-control 2>/dev/null || true)
  if [[ -z "$control_container" ]]; then
    echo '- Metrics control TLS: Not deployed'
  elif ! addp_infra_verify_container metrics-control "$control_container" >/dev/null 2>&1; then
    echo '- Metrics control TLS: Unavailable (ownership)'
  elif [[ "$(docker inspect --format '{{.State.Running}}' "$control_container")" == true ]]; then
    echo '- Metrics control TLS: Running (API forwarding not yet verified)'
  else
    echo '- Metrics control TLS: Unavailable'
  fi
fi

if ! addp_runtime_logs_enabled; then
  case "${ADDP_OBSERVABILITY_LOGS_ENABLED-true}" in
    false) echo '- Central runtime logs: Disabled' ;;
    *) echo '- Central runtime logs: Unconfigured (invalid deployment selection)' ;;
  esac
  exit 0
fi
LOKI_PORT=$(get_port runtime-log-api 3100 || true)
ALLOY_PORT=$(get_port alloy 12345 || true)
printf "%s" "- Runtime log query (localhost:${LOKI_PORT}): "
if [ -z "$LOKI_PORT" ]; then echo "Not deployed"; elif curl -fsS --max-time 3 "http://127.0.0.1:${LOKI_PORT}/ready" >/dev/null; then echo "Ready"; else echo "Unavailable"; fi
printf "%s" "- Alloy (localhost:${ALLOY_PORT}): "
if [ -z "$ALLOY_PORT" ]; then echo "Not deployed"; elif curl -fsS --max-time 3 "http://127.0.0.1:${ALLOY_PORT}/-/ready" >/dev/null; then echo "Ready (delivery completeness unconfirmed)"; else echo "Unavailable"; fi

# A successful probe proves this node's current path, never historical completeness.
if [ -n "${LOKI_PORT:-}" ] && [ -n "${ALLOY_PORT:-}" ]; then
  export LOKI_URL=http://runtime-log-api:3100
  docker exec -e LOKI_URL -e LOKI_READ_TOKEN addp-runtime-log-pruner runtime-log probe || true
fi

# Print counters only; never emit arbitrary labels, event bodies or tokens.
if [ -n "${ALLOY_PORT:-}" ]; then
  curl -fsS --max-time 3 "http://127.0.0.1:${ALLOY_PORT}/metrics" 2>/dev/null | python3 -c '
import re,sys
values={}
for line in sys.stdin:
 match=re.match(r"^(loki_write_(?:batch_retries|dropped_entries|sent_entries)_total)(?:\{.*\})? ([0-9.eE+]+)$",line.strip())
 if match: values[match[1]]=values.get(match[1],0)+float(match[2])
print("- Alloy sending counters:",values if values else "unavailable; no completeness proof")
' || true
fi
python3 - <<'PYSOURCECOUNTERS'
from pathlib import Path
import json,os
root=Path(os.environ.get('ADDP_RUNTIME_LOG_ROOT','./logs/runtime'))
names=('received','written','parse_failures','truncated','dropped','write_failures','source_files_cleaned')
total={name:0 for name in names}
count=0
for file in root.glob('*/*/status.json'):
 try:
  value=json.loads(file.read_text());count+=1
  for name in names:total[name]+=int(value.get(name,0))
 except (OSError,ValueError,TypeError):pass
print('- Runtime source retained receiver counters:',count,total,'(retained files only; not a complete history)')
try:
 value=json.loads((root/'housekeeping-status.json').read_text())
 print('- Runtime source last housekeeping:', {key:value.get(key) for key in ('observed_at','source_files_cleaned','quota_exhausted')})
except (OSError,ValueError):
 print('- Runtime source housekeeping: unavailable')
PYSOURCECOUNTERS
