#!/bin/bash

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}=== 基础设施层健康检查 ===${NC}"

# PostgreSQL
if docker compose -f docker-compose.prod.yml exec -T postgres pg_isready -U addp > /dev/null 2>&1; then
  echo -e "${GREEN}✓ PostgreSQL${NC}"
else
  echo -e "${RED}✗ PostgreSQL${NC}"
fi

# Redis
if docker compose -f docker-compose.prod.yml exec -T redis redis-cli --raw incr ping > /dev/null 2>&1; then
  echo -e "${GREEN}✓ Redis${NC}"
else
  echo -e "${RED}✗ Redis${NC}"
fi

# MinIO
if curl -f http://localhost:9000/minio/health/live > /dev/null 2>&1; then
  echo -e "${GREEN}✓ MinIO${NC}"
else
  echo -e "${RED}✗ MinIO${NC}"
fi

# Meilisearch
if curl -f http://localhost:7700/health > /dev/null 2>&1; then
  echo -e "${GREEN}✓ Meilisearch${NC}"
else
  echo -e "${RED}✗ Meilisearch${NC}"
fi

echo ""
echo -e "${YELLOW}=== 应用服务层健康检查 ===${NC}"

# 检查各个后端服务
for service in system:8080 manager:8081 meta:8082 transfer:8083 orchestrator:8084 develop:8085; do
  name=$(echo $service | cut -d: -f1)
  port=$(echo $service | cut -d: -f2)

  if curl -f http://localhost:$port/health > /dev/null 2>&1; then
    echo -e "${GREEN}✓ ${name}-backend${NC}"
  else
    echo -e "${RED}✗ ${name}-backend${NC}"
  fi
done

# Gateway
if curl -f http://localhost:8000/health > /dev/null 2>&1; then
  echo -e "${GREEN}✓ gateway${NC}"
else
  echo -e "${RED}✗ gateway${NC}"
fi

# Nginx
if curl -f http://localhost:8000/ > /dev/null 2>&1; then
  echo -e "${GREEN}✓ nginx${NC}"
else
  echo -e "${RED}✗ nginx${NC}"
fi
