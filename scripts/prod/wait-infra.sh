#!/bin/bash
set -e

if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

POSTGRES_USER="${POSTGRES_USER:-addp}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-addp_password}"
REDIS_PASSWORD="${REDIS_PASSWORD:-addp_redis}"
MINIO_API_PORT="${MINIO_API_PORT:-19000}"
MEILISEARCH_PORT="${MEILISEARCH_PORT:-17700}"

echo "等待 PostgreSQL 就绪..."
timeout=90
counter=0
until docker compose -f docker-compose.infra.yml exec -T postgres env PGPASSWORD="${POSTGRES_PASSWORD}" \
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
until docker compose -f docker-compose.infra.yml exec -T redis redis-cli -a "${REDIS_PASSWORD}" ping > /dev/null 2>&1; do
  sleep 2
  counter=$((counter + 2))
  if [ $counter -ge $timeout ]; then
    echo "错误: Redis 启动超时"
    exit 1
  fi
done
echo "✓ Redis 已就绪"

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

echo "所有基础设施服务已就绪！"
