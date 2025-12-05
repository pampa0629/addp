#!/bin/bash
set -e

echo "等待 PostgreSQL 就绪..."
timeout=90
counter=0
until docker compose -f docker-compose.infra.yml exec -T postgres pg_isready -U addp > /dev/null 2>&1; do
  sleep 2
  counter=$((counter + 2))
  if [ $counter -ge $timeout ]; then
    echo "错误: PostgreSQL 启动超时"
    exit 1
  fi
done
echo "✓ PostgreSQL 已就绪"

echo "等待 Redis 就绪..."
counter=0
until docker compose -f docker-compose.infra.yml exec -T redis redis-cli --raw incr ping > /dev/null 2>&1; do
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
until curl -f http://localhost:9000/minio/health/live > /dev/null 2>&1; do
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
until curl -f http://localhost:7700/health > /dev/null 2>&1; do
  sleep 2
  counter=$((counter + 2))
  if [ $counter -ge $timeout ]; then
    echo "错误: Meilisearch 启动超时"
    exit 1
  fi
done
echo "✓ Meilisearch 已就绪"

echo "所有基础设施服务已就绪！"
