#!/bin/bash
set -e

if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

# Always use the actual owned container mappings, including port collision resolution.
source scripts/infra/ports.sh
ADDP_INFRA_PORT_SCOPE=core addp_infra_read_actual_ports
core_compose() { PROMETHEUS_PORT=0 LOKI_PORT=0 ALLOY_PORT=0 docker compose -f docker-compose.infra.yml "$@"; }

POSTGRES_USER="${POSTGRES_USER:-addp}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-addp_password}"
REDIS_PASSWORD="${REDIS_PASSWORD:-addp_redis}"
MINIO_API_PORT="${MINIO_API_PORT:-19000}"
MEILISEARCH_PORT="${MEILISEARCH_PORT:-17700}"

echo "等待 PostgreSQL 就绪..."
timeout=90
counter=0
until core_compose exec -T postgres env PGPASSWORD="${POSTGRES_PASSWORD}" \
  psql -h 127.0.0.1 -U "${POSTGRES_USER}" -d "${POSTGRES_DB:-addp}" -c "select 1" > /dev/null 2>&1; do
  sleep 2
  counter=$((counter + 2))
  if [ $counter -ge $timeout ]; then
    echo "错误: PostgreSQL 启动超时或密码不匹配"
    echo "请确认 .env 中 POSTGRES_PASSWORD 与已有 PostgreSQL 数据卷初始化密码一致。"
    exit 1
  fi
done
echo "✓ PostgreSQL 已就绪"

echo "等待 Redis 就绪..."
counter=0
until core_compose exec -T redis redis-cli -a "${REDIS_PASSWORD}" ping > /dev/null 2>&1; do
  sleep 2
  counter=$((counter + 2))
  if [ $counter -ge $timeout ]; then
    echo "错误: Redis 启动超时"
    exit 1
  fi
done
echo "✓ Redis 已就绪"

echo "等待 FalkorDB 就绪..."
counter=0
until [ "$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}' addp-falkordb 2>/dev/null)" = "healthy" ]; do
  sleep 2
  counter=$((counter + 2))
  if [ $counter -ge $timeout ]; then
    echo "错误: FalkorDB 启动超时或图健康检查失败"
    exit 1
  fi
done
echo "✓ FalkorDB 已就绪"

echo "等待 MinIO 就绪..."
counter=0
until curl -f "http://localhost:${MINIO_API_PORT}/minio/health/live" > /dev/null 2>&1; do
  sleep 2
  counter=$((counter + 2))
  if [ $counter -ge $timeout ]; then
    echo "错误: MinIO 启动超时"
    exit 1
  fi
done
echo "✓ MinIO 已就绪"

echo "等待 Meilisearch 就绪..."
counter=0
until curl -f "http://localhost:${MEILISEARCH_PORT}/health" > /dev/null 2>&1; do
  sleep 2
  counter=$((counter + 2))
  if [ $counter -ge $timeout ]; then
    echo "错误: Meilisearch 启动超时"
    exit 1
  fi
done
echo "✓ Meilisearch 已就绪"

echo "业务基础设施服务已就绪。"
if [[ "${ADDP_OBSERVABILITY_LOGS_ENABLED-true}" != true ]]; then
  echo "集中运行日志未启用或配置无效；业务 Infra 状态独立。"
  exit 0
fi
for component in addp-runtime-log-api addp-alloy addp-runtime-log-pruner; do
  state=$(docker inspect --format '{{.State.Running}}:{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$component" 2>/dev/null || true)
  case "$state" in
    true:healthy|true:) echo "✓ $component 已就绪" ;;
    *) echo "警告: $component 未就绪；日志查询可能不可用，请运行 bash scripts/infra/status.sh 排查。" ;;
  esac
done
