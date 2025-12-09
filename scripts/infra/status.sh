#!/usr/bin/env bash

# ADDP Infrastructure Status Script
# 查看系统库（PostgreSQL、Redis、MinIO）状态并做快速健康检查

set -euo pipefail

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"; exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"; exit 1
fi

echo -e "${YELLOW}▶ 容器状态（docker compose ps）${NC}"
docker compose -f docker-compose.infra.yml ps postgres redis minio meilisearch || true

echo ""
echo -e "${YELLOW}▶ 健康检查${NC}"

# Resolve actual mapped ports
get_port() {
  local svc="$1"; local p="$2"
  docker compose port "$svc" "$p" 2>/dev/null | sed 's/.*://'
}

PG_PORT=$(get_port postgres 5432 || echo 5432)
REDIS_PORT=$(get_port redis 6379 || echo 6379)
MINIO_API_PORT=$(get_port minio 9000 || echo 9000)
MINIO_CONSOLE_PORT=$(get_port minio 9001 || echo 9001)

# Postgres
printf "%s" "- PostgreSQL (localhost:${PG_PORT}):  "
if docker compose -f docker-compose.infra.yml exec -T postgres pg_isready -U addp >/dev/null 2>&1; then
  echo -e "${GREEN}Healthy${NC}"
else
  echo -e "${RED}Unhealthy${NC}"
fi

# Redis
printf "%s" "- Redis (localhost:${REDIS_PORT}):       "
if docker compose -f docker-compose.infra.yml exec -T redis redis-cli -a addp_redis ping 2>/dev/null | grep -q PONG; then
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
MEILI_PORT=7700
printf "%s" "- Meilisearch (localhost:${MEILI_PORT}):  "
if curl -sf "http://localhost:${MEILI_PORT}/health" >/dev/null 2>&1; then
  echo -e "${GREEN}Healthy${NC}"
else
  echo -e "${RED}Unhealthy${NC}"
fi
