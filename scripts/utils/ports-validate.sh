#!/usr/bin/env bash
set -euo pipefail

YELLOW='\033[1;33m'
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${YELLOW}Checking ADDP port allocations...${NC}"

# Expected policy
SYS_MINIO_API=19000
SYS_MINIO_CONSOLE=19001
BIZ_MINIO_API=9002
BIZ_MINIO_CONSOLE=9003
SYS_PG=15432
BIZ_PG=5433
SYS_REDIS=16379
SYS_MEILISEARCH=17700

ok=true

report_port() {
  local name="$1" p="$2" expect="$3"
  if [ "$p" = "$expect" ]; then
    echo -e "  ${GREEN}✓ ${name}: ${p}${NC}"
  else
    echo -e "  ${RED}✗ ${name}: ${p} (expect ${expect})${NC}"
    ok=false
  fi
}

# Read business .env if present
if [ -f business/.env ]; then
  set -a; source business/.env; set +a
fi

# Compose defaults if env missing
BUSINESS_MINIO_API_PORT=${BUSINESS_MINIO_API_PORT:-9002}
BUSINESS_MINIO_CONSOLE_PORT=${BUSINESS_MINIO_CONSOLE_PORT:-9003}
BUSINESS_POSTGRES_PORT=${BUSINESS_POSTGRES_PORT:-5433}

echo "Business (.env or defaults):"
report_port "BUSINESS_MINIO_API_PORT" "$BUSINESS_MINIO_API_PORT" "$BIZ_MINIO_API"
report_port "BUSINESS_MINIO_CONSOLE_PORT" "$BUSINESS_MINIO_CONSOLE_PORT" "$BIZ_MINIO_CONSOLE"
report_port "BUSINESS_POSTGRES_PORT" "$BUSINESS_POSTGRES_PORT" "$BIZ_PG"

echo "System (compose defaults):"
report_port "SYSTEM_POSTGRES_PORT" "$SYS_PG" "$SYS_PG"
report_port "SYSTEM_REDIS_PORT" "$SYS_REDIS" "$SYS_REDIS"
report_port "SYSTEM_MINIO_API_PORT" "$SYS_MINIO_API" "$SYS_MINIO_API"
report_port "SYSTEM_MINIO_CONSOLE_PORT" "$SYS_MINIO_CONSOLE" "$SYS_MINIO_CONSOLE"
report_port "SYSTEM_MEILISEARCH_PORT" "$SYS_MEILISEARCH" "$SYS_MEILISEARCH"

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

