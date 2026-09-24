#!/usr/bin/env bash
set -euo pipefail

YELLOW='\033[1;33m'
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${YELLOW}Checking ADDP port allocations...${NC}"

# Preferred values and reserved policy
SYS_MINIO_API=19000
SYS_MINIO_CONSOLE=19001
SYS_PG=15432
SYS_REDIS=16379
SYS_MEILISEARCH=17700

ok=true

# Read business .env if present
if [ -f business/.env ]; then
  set -a; source business/.env; set +a
fi

# Compose preferences; dynamic mappings are persisted separately after startup.
echo "Business (preferred / pinned / actual host ports):"
report_business_port() {
  local name="$1" variable="$2" container="$3" internal="$4" preferred="$5" pinned actual
  pinned=''
  if [ -f business/.business-state/ports.env ]; then
    pinned=$(sed -n "s/^${variable}=//p" business/.business-state/ports.env | head -n 1)
  fi
  actual=$(docker port "$container" "${internal}/tcp" 2>/dev/null | sed -n '1s/.*://p' || true)
  echo "  ${name}: preferred=${preferred} pinned=${pinned:-none} actual=${actual:-not running}"
  if [ -n "$actual" ] && [ -n "$pinned" ] && [ "$actual" != "$pinned" ]; then
    echo -e "  ${RED}✗ ${name} 的实际端口与固定端口不一致${NC}"
    ok=false
  fi
}
report_business_port PostgreSQL POSTGRES_PORT business-postgres 5432 "${POSTGRES_PORT:-5433}"
report_business_port MySQL MYSQL_PORT business-mysql 3306 "${MYSQL_PORT:-3306}"
report_business_port 'MinIO API' MINIO_API_PORT business-minio 9000 "${MINIO_API_PORT:-9002}"
report_business_port 'MinIO Console' MINIO_CONSOLE_PORT business-minio 9001 "${MINIO_CONSOLE_PORT:-9003}"
if [ "${MINIO_API_PORT:-9002}" = 19000 ] || [ "${MINIO_API_PORT:-9002}" = 19001 ] ||
   [ "${MINIO_CONSOLE_PORT:-9003}" = 19000 ] || [ "${MINIO_CONSOLE_PORT:-9003}" = 19001 ]; then
  echo -e "  ${RED}✗ Business MinIO 不得使用 System MinIO 首选端口${NC}"
  ok=false
fi

echo "System (preferred / actual host ports):"
report_system_port() {
  local name="$1" container="$2" internal="$3" preferred="$4" actual
  actual=$(docker port "$container" "${internal}/tcp" 2>/dev/null | sed -n '1s/.*://p' || true)
  echo "  ${name}: preferred=${preferred} actual=${actual:-not running}"
}
report_system_port PostgreSQL addp-postgres 5432 "$SYS_PG"
report_system_port Redis addp-redis 6379 "$SYS_REDIS"
report_system_port FalkorDB addp-falkordb 6379 16479
report_system_port 'MinIO API' addp-minio 9000 "$SYS_MINIO_API"
report_system_port 'MinIO Console' addp-minio 9001 "$SYS_MINIO_CONSOLE"
report_system_port Meilisearch addp-meilisearch 7700 "$SYS_MEILISEARCH"
report_system_port 'Infra Kafka' addp-redpanda 9092 19092
report_system_port 'Kafka Connect' addp-kafka-connect 8083 18083

echo ""
echo "Runtime containers (if any):"
docker ps --format '{{.Names}}\t{{.Ports}}' | grep -E 'minio|postgres' || echo "  (none)"

echo ""
if [ "$ok" = true ]; then
  echo -e "${GREEN}Policy OK. No changes required.${NC}"
  exit 0
else
  echo -e "${RED}Policy mismatch detected. Please update ports as above.${NC}"
  exit 1
fi
